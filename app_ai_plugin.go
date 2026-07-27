package main

// =========================================================================
// Plugin AI gateway (#216, #226)
// =========================================================================
//
// Plugin-facing AI bindings: chat completion, structured audit events, and
// embeddings. Streaming runtime (startAIStream, cancel/ready, session type)
// lives in app_ai_stream.go (#762). All are gated exactly like PluginFetch:
// session token → requireGrant(CapAI) → rate limiter → size cap → service
// call → audit. Plugins NEVER receive credentials; the provider config +
// resolved key are snapshotted server-side (withAIPreflight) and handed to
// the service as a value.
//
// Streaming (#226): PluginAIComplete(stream=true) returns immediately with a
// stream_id and pushes deltas via owner-scoped Wails events
// ("ai:complete:delta:<pluginID>" etc.) so concurrent plugin streams do not
// share a global bus (#635). Cancel with PluginAICancelStream; the frontend
// acks listener attach via PluginAIStreamReady.
//
// Lock ordering follows the app.go invariant (vaultMu before configMu). The
// plugin bindings snapshot the provider config under the locks, RELEASE them,
// and only then perform the (potentially long) HTTP call so an LLM completion
// cannot hold vaultMu for 60s. The snapshot is a value copy, so no lock is
// needed during the call.

import (
	"encoding/json"
	"fmt"
	"silt/backend/ai"
	"silt/backend/config"
	"silt/backend/plugins"
	"strings"
)

// PluginAIChatMessage is one chat message crossing the plugin→service boundary.
// For multi-turn tool use (#595): assistant turns may carry tool_calls, and a
// tool result turn (role "tool") carries tool_call_id correlating it.
type PluginAIChatMessage struct {
	Role       string             `json:"role"`
	Content    string             `json:"content"`
	ToolCalls  []PluginAIToolCall `json:"tool_calls,omitempty"`
	ToolCallID string             `json:"tool_call_id,omitempty"`
}

// PluginAIToolDef declares one tool a plugin exposes to the model (#595).
// Parameters is a raw JSON Schema object.
type PluginAIToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// PluginAIToolCall is one tool invocation the model requested (#595). Arguments
// is the raw JSON object bytes (unwrapped from OpenAI's stringified form).
type PluginAIToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// PluginAIToolChoice constrains tool selection (#595).
type PluginAIToolChoice struct {
	Mode     string `json:"mode"`                // auto|required|none|force
	ToolName string `json:"tool_name,omitempty"` // set when Mode == "force"
}

// PluginAICompleteInput is the plugin-side request envelope for a chat
// completion. When Stream is true, PluginAIComplete starts an async SSE stream
// and returns immediately with stream_id set; deltas arrive as Wails events
// (#226). When false (default), the buffered Sprint 20 path is used.
type PluginAICompleteInput struct {
	Messages        []PluginAIChatMessage `json:"messages"`
	Model           string                `json:"model,omitempty"`
	Temperature     *float64              `json:"temperature,omitempty"`
	MaxTokens       *int                  `json:"max_tokens,omitempty"`
	ReasoningEffort *string               `json:"reasoning_effort,omitempty"`
	Stream          bool                  `json:"stream,omitempty"`
	ResponseSchema  json.RawMessage       `json:"response_schema,omitempty"`
	Tools           []PluginAIToolDef     `json:"tools,omitempty"`
	ToolChoice      *PluginAIToolChoice   `json:"tool_choice,omitempty"`
}

// PluginAIEmbedInput is the plugin-side request envelope for an embedding batch.
type PluginAIEmbedInput struct {
	Texts      []string `json:"texts"`
	Model      string   `json:"model,omitempty"`
	Dimensions *int     `json:"dimensions,omitempty"`
	// TaskType is Google-specific (RETRIEVAL_DOCUMENT / RETRIEVAL_QUERY); empty omits.
	TaskType string `json:"task_type,omitempty"`
}

// aiChatKind / aiEmbedKind are the audit "kind" tags written to the AI audit log.
const (
	aiChatKind = "chat"
	aiEmbKind  = "embed"
)

// maxPluginAIAuditEventJSONBytes rejects oversized event payloads before
// json.Unmarshal so CapAI plugins cannot force multi-megabyte allocations
// (the on-disk Detail cap is only 2 KiB after filtering).
const maxPluginAIAuditEventJSONBytes = 8 * 1024

// maxPluginAIAuditKindLen bounds the kind string stored on each log row.
const maxPluginAIAuditKindLen = 64

// withAIPreflight runs the plugin-side gates (session, capability, rate limit)
// and returns the resolved provider snapshot, the effective model, and a done
// func the caller MUST defer. Locks are released before the keyring lookup and
// before the caller does any HTTP.
//
// Drain contract (#473): the closing-check + vaultClosingWG.Add(1) below share
// ONE RLock hold, so they are atomic w.r.t. CloseVault/SwitchVault's
// closing=true + Wait (taken under the write lock) — no TOCTOU window where a
// call slips through after the drain returns (#452). Returning the balancing
// Done() as part of the same result as the Add makes the contract STRUCTURAL:
// a caller that uses this wrapper cannot forget the Add (it's bundled in) and
// cannot forget the Done (the returned func must be deferred or the vet/linter
// flags the unused result). On error, done is nil — no Add ran, nothing to
// balance. The Done() is safe to call at most once (sync.WaitGroup semantics).
//
// Nothing after the Add may return an error, or it would leak an unbalanced
// increment; the remaining steps (keyring resolve + struct return) cannot fail
// (the key resolve ignores its error and the return is a value copy), so the
// returned done is always balanced by exactly one caller defer.
func (a *App) withAIPreflight(pluginID, sessionToken, which string) (ai.AIProvider, string, func(), error) {
	a.vaultMu.RLock()
	// Reject new AI calls once a vault close/switch has begun. The check and
	// the vaultClosingWG.Add below share this RLock hold, so they are atomic
	// w.r.t. CloseVault/SwitchVault's closing=true + Wait (taken under the
	// write lock) — no TOCTOU window where a call slips through after the
	// drain returns (#452).
	if a.closing {
		a.vaultMu.RUnlock()
		return ai.AIProvider{}, "", nil, vaultClosingError()
	}
	if err := a.validatePluginSession(pluginID, sessionToken); err != nil {
		a.vaultMu.RUnlock()
		return ai.AIProvider{}, "", nil, err
	}
	if err := a.requireGrant(pluginID, plugins.CapAI); err != nil {
		a.vaultMu.RUnlock()
		return ai.AIProvider{}, "", nil, err
	}
	if a.rateLimiter != nil && !a.rateLimiter.allow(a.vaultPath, pluginID) {
		a.vaultMu.RUnlock()
		a.recordRateLimited(pluginID)
		rps, burst := resolvePluginRatelimit(a.vaultPath, pluginID)
		return ai.AIProvider{}, "", nil, fmt.Errorf("plugin %q AI rate limit exceeded (max %.1f rps, burst %d); retry after a short delay", pluginID, rps, burst)
	}
	a.configMu.RLock()
	p := aiConfigBlock(a.cfg.AI, which)
	useKeyring := a.aiUseKeyringLocked()
	user := a.aiKeyringUser(which)
	a.configMu.RUnlock()
	// All gates passed: this call will issue HTTP and outlive the RLock. Add
	// to the vault-close drain so CloseVault/SwitchVault wait for it before
	// teardown (#452). The returned done func balances this Add; the caller
	// defers it on the success path.
	a.vaultClosingWG.Add(1)
	a.vaultMu.RUnlock()
	key, _ := a.resolveAIKeyUnlocked(user, useKeyring, p.APIKey)
	provider := ai.AIProvider{
		ProviderType:    p.ProviderType,
		BaseURL:         p.BaseURL,
		APIKey:          key,
		Model:           p.Model,
		ReasoningEffort: p.ReasoningEffort,
		TimeoutMs:       p.TimeoutMs,
	}
	return provider, p.Model, func() { a.vaultClosingWG.Done() }, nil
}

// PluginAIComplete performs a chat completion on behalf of a plugin. Gated by
// the ai capability; rate-limited and audit-logged exactly like PluginFetch.
// Credentials are read server-side and never returned to the caller. (#216.)
//
// When input.Stream is true (#226), the call returns immediately with
// stream_id set and pushes deltas via ai:complete:delta / done / error events.
// Cancel with PluginAICancelStream. Audit records one start+final status row
// (not per-token).
func (a *App) PluginAIComplete(pluginID, sessionToken string, input PluginAICompleteInput) (ai.CompleteResult, error) {
	// Tracked by a.wg so shutdown's a.wg.Wait() drains in-flight AI calls
	// before teardownVaultServices clears the audit state. Unlike PluginFetch
	// (which holds vaultMu.RLock for its whole body), PluginAIComplete releases
	// vaultMu after withAIPreflight so a long LLM call doesn't hold the vault
	// lock — which means vaultMu alone can't serialize the call against a close.
	a.wg.Add(1)
	// Validate reasoning_effort BEFORE withAIPreflight so an invalid call
	// doesn't consume a rate-limit slot or snapshot the provider config.
	if input.ReasoningEffort != nil {
		if re := strings.TrimSpace(*input.ReasoningEffort); re != "" && !config.IsValidAIReasoningEffort(re) {
			a.wg.Done()
			return ai.CompleteResult{}, &ai.AIError{Kind: ai.ErrBadRequest, Message: fmt.Sprintf("invalid reasoning_effort %q: must be one of none, minimal, low, medium, high, xhigh, max", *input.ReasoningEffort)}
		}
	}
	// Validate tools + tool_choice at the boundary so a malformed request
	// fails fast (bad-request) before snapshotting config or rate-limiting.
	// Providers normalize inconsistently; this keeps one source of truth.
	if verr := validateAITools(input.Tools, input.ToolChoice); verr != nil {
		a.wg.Done()
		return ai.CompleteResult{}, &ai.AIError{Kind: ai.ErrBadRequest, Message: verr.Error()}
	}
	provider, configuredModel, drainDone, err := a.withAIPreflight(pluginID, sessionToken, "chat")
	if err != nil {
		a.wg.Done()
		return ai.CompleteResult{}, err
	}
	// Effective model for auditing: the per-call override, else the configured one.
	effectiveModel := input.Model
	if effectiveModel == "" {
		effectiveModel = configuredModel
	}
	messages := make([]ai.ChatMessage, len(input.Messages))
	for i, m := range input.Messages {
		messages[i] = ai.ChatMessage{
			Role:       m.Role,
			Content:    m.Content,
			ToolCalls:  toAIToolCalls(m.ToolCalls),
			ToolCallID: m.ToolCallID,
		}
	}
	req := ai.CompleteRequest{
		Provider:        provider,
		Messages:        messages,
		Model:           input.Model,
		Temperature:     input.Temperature,
		MaxTokens:       input.MaxTokens,
		ReasoningEffort: input.ReasoningEffort,
		ResponseSchema:  input.ResponseSchema,
		Tools:           toAIToolDefs(input.Tools),
		ToolChoice:      toAIToolChoice(input.ToolChoice),
	}

	if input.Stream {
		return a.startAIStream(pluginID, provider, effectiveModel, req, drainDone)
	}

	defer a.wg.Done()
	// Preflight registered this call with the vault-close drain (the Add ran
	// inside withAIPreflight on this success path). Balance it now that HTTP +
	// audit will run, so CloseVault/SwitchVault's drain can't under-count.
	defer drainDone()
	result, callErr := ai.Complete(a.aiContext(), req)
	status := "ok"
	if callErr != nil {
		status = aiErrKind(callErr)
	}
	a.auditAI(pluginID, aiChatKind, provider.BaseURL, effectiveModel, status, result.Usage)
	if callErr != nil {
		return ai.CompleteResult{}, callErr
	}
	return result, nil
}

// requirePluginSession validates the session token maps to pluginID. Shared by
// stream cancel and other AI bindings that need identity without full preflight.
func (a *App) requirePluginSession(pluginID, sessionToken string) error {
	a.pluginSessionsMu.RLock()
	owner, ok := a.pluginSessions[sessionToken]
	a.pluginSessionsMu.RUnlock()
	if !ok || owner != pluginID {
		return fmt.Errorf("invalid plugin session")
	}
	return nil
}

// PluginAIAuditEvent appends a structured agent/tool/staging audit row to the
// AI audit log (#630). Gated by CapAI + session. eventJSON is a JSON object;
// only allowlisted metadata keys are retained (never freeform note bodies).
func (a *App) PluginAIAuditEvent(pluginID, sessionToken, eventJSON string) error {
	if err := a.requirePluginSession(pluginID, sessionToken); err != nil {
		return err
	}
	if err := a.requireGrant(pluginID, plugins.CapAI); err != nil {
		return err
	}
	eventJSON = strings.TrimSpace(eventJSON)
	if eventJSON == "" {
		return &ai.AIError{Kind: ai.ErrBadRequest, Message: "event JSON is required"}
	}
	if len(eventJSON) > maxPluginAIAuditEventJSONBytes {
		return &ai.AIError{
			Kind:    ai.ErrBadRequest,
			Message: fmt.Sprintf("event JSON exceeds %d-byte cap", maxPluginAIAuditEventJSONBytes),
		}
	}
	var fields map[string]any
	if err := json.Unmarshal([]byte(eventJSON), &fields); err != nil {
		return &ai.AIError{Kind: ai.ErrBadRequest, Message: fmt.Sprintf("invalid event JSON: %v", err)}
	}
	kind, _ := fields["kind"].(string)
	kind = strings.TrimSpace(kind)
	if kind == "" {
		kind = "event"
	}
	if len(kind) > maxPluginAIAuditKindLen {
		return &ai.AIError{
			Kind:    ai.ErrBadRequest,
			Message: fmt.Sprintf("event kind exceeds %d characters", maxPluginAIAuditKindLen),
		}
	}
	// Drop the kind key from detail fields; it is stored on the entry.
	delete(fields, "kind")
	a.auditAIEvent(pluginID, kind, fields)
	return nil
}

// PluginAIEmbed computes embeddings for a batch of texts on behalf of a plugin.
// Gated by the ai capability; rate-limited and audit-logged exactly like
// PluginFetch. Credentials are read server-side and never returned to the
// caller. (#216.)
func (a *App) PluginAIEmbed(pluginID, sessionToken string, input PluginAIEmbedInput) (ai.EmbedResult, error) {
	// Tracked by a.wg — same rationale as PluginAIComplete (see above).
	a.wg.Add(1)
	defer a.wg.Done()
	provider, configuredModel, drainDone, err := a.withAIPreflight(pluginID, sessionToken, "embedding")
	if err != nil {
		return ai.EmbedResult{}, err
	}
	// Preflight registered this call with the vault-close drain (the Add ran
	// inside withAIPreflight on this success path); balance it via the returned
	// done func. See PluginAIComplete.
	defer drainDone()
	effectiveModel := input.Model
	if effectiveModel == "" {
		effectiveModel = configuredModel
	}
	result, callErr := ai.Embed(a.aiContext(), ai.EmbedRequest{
		Provider:   provider,
		Texts:      input.Texts,
		Model:      input.Model,
		Dimensions: input.Dimensions,
		TaskType:   input.TaskType,
	})
	status := "ok"
	if callErr != nil {
		status = aiErrKind(callErr)
	}
	a.auditAI(pluginID, aiEmbKind, provider.BaseURL, effectiveModel, status, result.Usage)
	if callErr != nil {
		return ai.EmbedResult{}, callErr
	}
	return result, nil
}

// aiErrKind reduces an error from the service to its audit "kind" tag, falling
// back to a generic "error" for non-AIError failures.
func aiErrKind(err error) string {
	if e, ok := err.(*ai.AIError); ok {
		return string(e.Kind)
	}
	return "error"
}

// toAIToolDefs maps the plugin-facing tool defs into the ai package's
// normalized ToolDef. The shapes are identical; this keeps the conversion in
// one place so future divergence is intentional, not accidental.
func toAIToolDefs(in []PluginAIToolDef) []ai.ToolDef {
	if len(in) == 0 {
		return nil
	}
	out := make([]ai.ToolDef, len(in))
	for i, t := range in {
		out[i] = ai.ToolDef{Name: t.Name, Description: t.Description, Parameters: t.Parameters}
	}
	return out
}

// toAIToolCalls maps plugin-facing tool calls into the ai package's ToolCall.
func toAIToolCalls(in []PluginAIToolCall) []ai.ToolCall {
	if len(in) == 0 {
		return nil
	}
	out := make([]ai.ToolCall, len(in))
	for i, c := range in {
		out[i] = ai.ToolCall{ID: c.ID, Name: c.Name, Arguments: c.Arguments}
	}
	return out
}

// toAIToolChoice maps the plugin-facing tool choice into the ai package's
// pointer-based ToolChoice (nil stays nil → provider default).
func toAIToolChoice(in *PluginAIToolChoice) *ai.ToolChoice {
	if in == nil {
		return nil
	}
	return &ai.ToolChoice{Mode: in.Mode, ToolName: in.ToolName}
}

// validateAITools rejects malformed tool definitions and tool_choice at the
// plugin boundary. Tool names must be non-empty; tool_choice.mode must be a
// known value; a "force" choice must name a declared tool. Providers each
// normalize tool_choice in their own shape and only partially coerce bad
// input, so this keeps a single source of truth before rate-limiting or HTTP.
func validateAITools(tools []PluginAIToolDef, choice *PluginAIToolChoice) error {
	seen := make(map[string]struct{}, len(tools))
	for _, t := range tools {
		if strings.TrimSpace(t.Name) == "" {
			return fmt.Errorf("tool definitions must each carry a non-empty name")
		}
		if _, dup := seen[t.Name]; dup {
			return fmt.Errorf("duplicate tool name %q", t.Name)
		}
		seen[t.Name] = struct{}{}
		// parameters must be a JSON Schema object. Reject missing, scalar,
		// and array shapes that providers would forward verbatim and then
		// reject or misparse.
		if len(t.Parameters) == 0 {
			return fmt.Errorf("tool %q parameters must be a JSON Schema object", t.Name)
		}
		var params map[string]any
		if err := json.Unmarshal(t.Parameters, &params); err != nil {
			return fmt.Errorf("tool %q parameters must be a JSON object: %w", t.Name, err)
		}
		if pt, ok := params["type"].(string); ok && pt != "object" {
			return fmt.Errorf("tool %q parameters must be type \"object\", got %q", t.Name, pt)
		}
	}
	if choice == nil {
		return nil
	}
	switch choice.Mode {
	case ai.ToolChoiceAuto, ai.ToolChoiceRequired, ai.ToolChoiceNone, ai.ToolChoiceForce:
	default:
		return fmt.Errorf("tool_choice.mode %q must be one of auto, required, none, force", choice.Mode)
	}
	if choice.Mode == ai.ToolChoiceForce {
		if strings.TrimSpace(choice.ToolName) == "" {
			return fmt.Errorf(`tool_choice.mode "force" requires tool_name`)
		}
		for _, t := range tools {
			if t.Name == choice.ToolName {
				return nil
			}
		}
		return fmt.Errorf("tool_choice forces unknown tool %q", choice.ToolName)
	}
	return nil
}
