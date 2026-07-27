// Package ai is Silt's core AI service: a thin, stdlib-only proxy that turns
// plugin chat-completion and embedding requests into provider-specific HTTP
// calls and normalizes the failure modes into a typed result.
//
// Design notes (Sprint 20 / #216, native providers / #479):
//
//   - Complete/Embed are the stable entry points. They validate the shared
//     inputs (messages, model, base URL) then dispatch on ProviderType to a
//     per-provider encoder/decoder. The OpenAI-compatible path is the universal
//     default/fallback; native google + anthropic paths bypass the OpenAI
//     request shape and speak the provider's first-party API.
//   - The service holds NO credentials. The caller (the App binding layer)
//     resolves the API key (config.yaml or OS-keyring-first) and passes it via
//     CompleteRequest / EmbedRequest, so this package never imports the keyring
//     or config packages.
//   - Errors are normalized into AIError so plugin JS gets an actionable Kind
//     ("unauthorized", "rate-limited", "model-missing", …) instead of a raw
//     status code or transport string.
//   - Request and response bodies are size-capped so a runaway plugin cannot
//     drive unbounded allocation (defense in depth, mirroring maxPluginFetchBytes).
//   - All providers share one transport: timeout, redirect guard, size caps,
//     retry-with-backoff on transient 5xx/429, and HTTP-status classification.
//     Per-provider code owns only URL construction, auth headers, request-body
//     encoding, response-body decoding, and (optionally) structured-error
//     classification.
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// MaxRequestBytes bounds the JSON body sent to a provider (the serialized
// messages / input texts). Mirrors maxPluginFetchRequestBytes: generous for real
// prompts, bounded so a plugin cannot stream a multi-hundred-MB body through
// the host.
const MaxRequestBytes = 10 * 1024 * 1024 // 10 MB

// MaxResponseBytes bounds a provider response body before it is parsed. Chat
// completions are small; embedding batches dominate (e.g. 1024 dims × 100 texts
// ≈ 800 KB). 50 MB covers large embedding batches while still bounding memory.
const MaxResponseBytes = 50 * 1024 * 1024 // 50 MB

// DefaultTimeout is applied when AIProviderConfig.TimeoutMs is unset. LLM
// completions are slow, so this is generous, but bounded so a dead endpoint
// cannot hang a plugin call indefinitely. Mirrors config.DefaultAITimeoutMs.
const DefaultTimeout = 60 * time.Second

// AIProviderType is the canonical provider-type discriminator. This package
// owns the single source of truth; config.AIProviderConfig.ProviderType and the
// App binding layer's public/patch structs carry this typed value, so the App
// copies the configured value verbatim with no string-literal duplication and
// no drift. (config imports ai for the type — acyclic: ai imports no Silt
// packages, keeping it unit-testable with httptest and no vault.)
type AIProviderType string

const (
	ProviderLocal            AIProviderType = "local"
	ProviderOpenAICompatible AIProviderType = "openai-compatible"
	ProviderGoogle           AIProviderType = "google"
	ProviderAnthropic        AIProviderType = "anthropic"
)

// httpClient is the dedicated client for all AI provider calls. It carries a
// CheckRedirect that rejects cross-host redirects AND same-host scheme
// downgrades (https→http), so a compromised or misconfigured endpoint cannot
// redirect the request (bearing an auth header) to a different host or carry
// it over plaintext where it would leak. Same-host same-scheme redirects (and
// http→https upgrades) are allowed (load balancers, path normalization). A
// dedicated client (not http.DefaultClient) also isolates AI calls from any
// global transport changes.
var httpClient = &http.Client{
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) == 0 {
			return nil
		}
		prev := via[len(via)-1].URL
		if req.URL.Host != prev.Host {
			return fmt.Errorf("ai: refused cross-host redirect from %s to %s", prev.Host, req.URL.Host)
		}
		// Same host, but never follow an https→http downgrade: the auth token
		// must not cross a plaintext hop even if the attacker controls only the
		// redirect response (e.g. a stripped TLS redirect on a hijacked path).
		if prev.Scheme == "https" && req.URL.Scheme == "http" {
			return fmt.Errorf("ai: refused scheme downgrade from https to http on host %s", prev.Host)
		}
		return nil
	},
}

// validateBaseURL checks the resolved endpoint for a valid http(s) scheme.
// Called by Complete and Embed as defense in depth (the binding layer also
// validates before persisting).
func validateBaseURL(baseURL string) error {
	if !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") {
		return fmt.Errorf("base URL must start with http:// or https://")
	}
	return nil
}

// Role constants for ChatMessage.Role. The OpenAI roles system/user/assistant
// are the universal input shape; RoleTool carries a tool result back to the
// model in a multi-turn agent loop (#595). Each provider encodes tool turns in
// its own wire shape (OpenAI role:"tool", Anthropic user tool_result blocks,
// Google user functionResponse parts).
const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleTool      = "tool"
)

// ToolDef describes one tool the model may call (#595). Parameters is a JSON
// Schema (raw JSON object, lowercase type strings) describing the arguments;
// each provider encodes it in its own wire shape.
type ToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// ToolCall is one invocation the model wants performed (#595). ID correlates
// the call with the tool-result message in the next turn (OpenAI/Anthropic and
// Google when the provider supplies an opaque id); Google falls back to the
// function name when no id is present. Arguments is the raw JSON object bytes
// (unwrapped from OpenAI's stringified form).
type ToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// normalizeToolArguments enforces the unified ToolCall contract at every
// provider boundary: arguments are always a JSON object. Providers sometimes
// return malformed JSON or a scalar/array; coercing those values to {} avoids
// leaking an invalid shape into the agent loop while preserving valid objects.
func normalizeToolArguments(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(`{}`)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err == nil && object != nil {
		return raw
	}
	return json.RawMessage(`{}`)
}

// ToolChoice constrains which tool (if any) the model must call (#595).
type ToolChoice struct {
	Mode     string `json:"mode"`                // auto|required|none|force
	ToolName string `json:"tool_name,omitempty"` // set when Mode == "force"
}

// ToolChoice mode values. Each provider maps these onto its own tool-selection
// syntax (OpenAI keyword/object, Anthropic tool_choice type enum, Google
// function_calling_config mode).
const (
	ToolChoiceAuto     = "auto"
	ToolChoiceRequired = "required"
	ToolChoiceNone     = "none"
	ToolChoiceForce    = "force"
)

// ChatMessage is one message in a chat-completion conversation. The OpenAI
// roles "system" / "user" / "assistant" are the universal input shape; native
// providers translate as needed (e.g. system → top-level field). For multi-turn
// tool use (#595): an assistant turn may carry ToolCalls, and a tool result
// turn (Role == RoleTool) carries ToolCallID correlating it to the prior call.
type ChatMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// CompleteRequest is the input to Complete. Messages is required and non-empty;
// the rest override the provider's configured defaults for this call only.
type CompleteRequest struct {
	Provider        AIProvider    // resolved endpoint + key + model + advanced knobs
	Messages        []ChatMessage `json:"messages"`
	Model           string        `json:"model,omitempty"`            // override Provider.Model for this call
	Temperature     *float64      `json:"temperature,omitempty"`      // override Provider.Temperature
	MaxTokens       *int          `json:"max_tokens,omitempty"`       // override Provider.MaxTokens
	ReasoningEffort *string       `json:"reasoning_effort,omitempty"` // override Provider.ReasoningEffort ("none"|"minimal"|"low"|"medium"|"high"|"xhigh"|"max")
	Stream          bool          `json:"stream,omitempty"`           // signature present now; Sprint 22 delivers streaming
	// ResponseSchema, when set, asks native providers (google, anthropic) to
	// return a JSON object conforming to the given JSON Schema. The openai-
	// compat path ignores it (prompt-only JSON stays the universal denominator,
	// per the D1 design decision). The schema is a raw JSON Schema object
	// (lowercase type strings); each native encoder converts to its own format.
	ResponseSchema json.RawMessage `json:"response_schema,omitempty"`
	// Tools declares tools the model may call (#595). Each provider encodes
	// them in its own wire shape. For Anthropic, real caller tools are additive
	// to the structured_output tool when ResponseSchema is also set.
	Tools []ToolDef `json:"tools,omitempty"`
	// ToolChoice constrains tool selection. nil lets the provider default
	// apply (auto for OpenAI/Google; omitted for Anthropic). Ignored on the
	// Anthropic structured-output path, which always forces structured_output.
	ToolChoice *ToolChoice `json:"tool_choice,omitempty"`
}

// CompleteResult is the output of a successful completion. Usage is optional
// because not every provider returns token counts. StreamID is set only when
// PluginAIComplete starts an async stream (#226): Content is empty on that
// start response; deltas arrive via Wails events until done/error. ToolCalls
// holds the tool invocations the model requested (#595); Content may be empty
// when the model only emitted tool calls.
type CompleteResult struct {
	Content   string     `json:"content"`
	Model     string     `json:"model"`
	Usage     *AIUsage   `json:"usage,omitempty"`
	StreamID  string     `json:"stream_id,omitempty"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

// EmbedRequest is the input to Embed. Texts is required and non-empty; the
// whole batch is sent in a single request.
type EmbedRequest struct {
	Provider   AIProvider `json:"-"`
	Texts      []string   `json:"input"`
	Model      string     `json:"model,omitempty"`      // override Provider.Model
	Dimensions *int       `json:"dimensions,omitempty"` // override Provider.Dimensions (truncation)
	// TaskType is Google-specific (RETRIEVAL_DOCUMENT / RETRIEVAL_QUERY).
	// Empty string omits the field; other providers ignore it.
	TaskType string `json:"task_type,omitempty"`
}

// EmbedResult is the output of a successful embedding batch. Embeddings[i] is
// the vector for Texts[i]. Dimensions is the per-vector length actually returned
// (the provider's native length, or the requested truncation length).
type EmbedResult struct {
	Embeddings [][]float64 `json:"embeddings"`
	Model      string      `json:"model"`
	Dimensions int         `json:"dimensions"`
	Usage      *AIUsage    `json:"usage,omitempty"`
}

// AIUsage mirrors the OpenAI usage object subset every compatible provider
// returns (some omit it entirely, hence pointers).
type AIUsage struct {
	PromptTokens     *int `json:"prompt_tokens,omitempty"`
	CompletionTokens *int `json:"completion_tokens,omitempty"`
	TotalTokens      *int `json:"total_tokens,omitempty"`
}

// AIModel is one entry in a ListModels poll. ID is the value the user selects
// (and that gets stored as AIProviderConfig.Model); DisplayName is the
// human-readable label for the dropdown (falls back to ID when the provider's
// list endpoint carries no name field).
type AIModel struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
}

// AIProvider is the resolved endpoint configuration handed to the service. It is
// the AIProviderConfig from backend/config with the API key already resolved by
// the caller (config.yaml or OS keyring-first). The App binding layer constructs
// one of these directly from a config.AIProviderConfig before calling
// Complete/Embed/Probe — keeping this package free of any import on config (or
// the keyring) so the service is unit-testable with httptest and no vault.
type AIProvider struct {
	ProviderType    AIProviderType // ProviderLocal | ProviderOpenAICompatible | ProviderGoogle | ProviderAnthropic
	BaseURL         string         // e.g. http://localhost:11434
	APIKey          string         // resolved by caller; "" for a keyless local endpoint
	Model           string
	ReasoningEffort *string // "none"|"minimal"|"low"|"medium"|"high"|"xhigh"|"max"; nil = omit (OpenAI-compat only)
	TimeoutMs       *int
}

// AIErrorKind enumerates the normalized failure categories a plugin can act on.
type AIErrorKind string

const (
	// ErrUnreachable — DNS resolution or TCP connection failed (provider down,
	// wrong host, offline).
	ErrUnreachable AIErrorKind = "unreachable"
	// ErrTimeout — the call exceeded its deadline (context deadline exceeded).
	// Distinct from unreachable so the UI can suggest "slow model" vs "wrong
	// endpoint". User/app cancellation uses ErrCanceled instead (#628).
	ErrTimeout AIErrorKind = "timeout"
	// ErrCanceled — the caller's context was canceled (Stop, vault close) before
	// a deadline. Distinct from timeout so the UI does not suggest raising the
	// timeout for an intentional abort (#628).
	ErrCanceled AIErrorKind = "canceled"
	// ErrUnauthorized — 401. The configured key is missing, wrong, or expired.
	ErrUnauthorized AIErrorKind = "unauthorized"
	// ErrForbidden — 403. The key is valid but lacks permission for the model.
	ErrForbidden AIErrorKind = "forbidden"
	// ErrRateLimited — 429. The provider throttled the call; retry with backoff.
	ErrRateLimited AIErrorKind = "rate-limited"
	// ErrModelMissing — 404. The model name is not available at this endpoint
	// (typo, not pulled, wrong provider).
	ErrModelMissing AIErrorKind = "model-missing"
	// ErrBadRequest — 400. The provider rejected the payload (bad dimensions,
	// unsupported param, malformed messages).
	ErrBadRequest AIErrorKind = "bad-request"
	// ErrServer — 5xx. The provider errored server-side; transient.
	ErrServer AIErrorKind = "server"
	// ErrUnknown — anything not classified above.
	ErrUnknown AIErrorKind = "unknown"
)

// AIError is the structured error returned by the service. It is JSON-
// serializable so the frontend SDK surfaces an actionable Kind + Message rather
// than a raw status code. Kind serializes as "code" to match the plugin SDK
// contract (rejections are keyed by `code`); the Go field name is unchanged.
// Status is 0 for transport-level failures.
type AIError struct {
	Kind    AIErrorKind `json:"code"`
	Status  int         `json:"status"`
	Message string      `json:"message"`
}

// Error implements error.
func (e *AIError) Error() string {
	if e.Status > 0 {
		return fmt.Sprintf("ai: %s (HTTP %d): %s", e.Kind, e.Status, e.Message)
	}
	return fmt.Sprintf("ai: %s: %s", e.Kind, e.Message)
}

// providerRequest is the per-provider output handed to the shared transport: the
// HTTP method, URL, serialized body, an optional header-setter (auth + provider
// specifics), and an optional error classifier. The transport builds a fresh
// *http.Request from these per attempt (so the body reader is never reused),
// applies the timeout + redirect guard + size caps, and retries transient
// failures. classifyErr, when set, parses a structured error body for richer
// fidelity than bare HTTP-status classification; returning nil falls back to the
// default status-based classification + trimmed body message.
type providerRequest struct {
	method      string
	url         string
	body        []byte
	setHeaders  func(req *http.Request)
	classifyErr func(raw []byte, status int) *AIError
}

// maxRetryAfter caps how long we honor a provider Retry-After header so a
// hostile or misconfigured endpoint cannot stall the call for minutes (#628).
const maxRetryAfter = 30 * time.Second

// overallTimeoutMargin is added to the computed overall deadline envelope so
// scheduling jitter and timer resolution do not race the last attempt (#628).
const overallTimeoutMargin = 250 * time.Millisecond

// sendOnce builds and sends one attempt. It is the per-attempt half of
// sendWithRetry: timeout + request build + send + response size cap + status
// classification. On transient HTTP errors the Retry-After header (when present)
// is returned so sendWithRetry can honor it. A fresh request per attempt is the
// caller's responsibility in sendWithRetry.
func sendOnce(ctx context.Context, pr providerRequest, timeoutMs *int) (raw []byte, status int, retryAfter time.Duration, aiErr *AIError) {
	timeout := DefaultTimeout
	if timeoutMs != nil && *timeoutMs > 0 {
		timeout = time.Duration(*timeoutMs) * time.Millisecond
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, pr.method, pr.url, bytes.NewReader(pr.body))
	if err != nil {
		// Malformed URL — surface as unreachable; the caller's base URL is bad.
		return nil, 0, 0, &AIError{Kind: ErrUnreachable, Message: fmt.Sprintf("build request: %v", err)}
	}
	req.Header.Set("Content-Type", "application/json")
	if pr.setHeaders != nil {
		pr.setHeaders(req)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		// Distinguish cancel / deadline / generic transport so the UI can hint
		// "stopped" vs "slow model / raise timeout" vs "endpoint unreachable".
		if errors.Is(ctx.Err(), context.Canceled) {
			return nil, 0, 0, &AIError{Kind: ErrCanceled, Message: fmt.Sprintf("request canceled: %v", err)}
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, 0, 0, &AIError{Kind: ErrTimeout, Message: fmt.Sprintf("request timed out after %s: %v", timeout, err)}
		}
		return nil, 0, 0, &AIError{Kind: ErrUnreachable, Message: err.Error()}
	}
	defer resp.Body.Close()

	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, MaxResponseBytes+1))
	if readErr != nil {
		return nil, resp.StatusCode, 0, &AIError{Kind: classifyStatus(resp.StatusCode), Status: resp.StatusCode, Message: fmt.Sprintf("read response: %v", readErr)}
	}
	if int64(len(raw)) > MaxResponseBytes {
		// A success (2xx) body that exceeds the cap is deterministic, not a
		// transient 5xx — the provider will return the same oversized body on
		// every retry, so classify by status (2xx → ErrUnknown, non-transient)
		// rather than hard-coding ErrServer and burning the retry budget (#628).
		return nil, resp.StatusCode, 0, &AIError{Kind: classifyStatus(resp.StatusCode), Status: resp.StatusCode, Message: fmt.Sprintf("response body exceeds %d-byte cap", MaxResponseBytes)}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Let the provider parse its structured error body for better fidelity;
		// fall back to status-based classification + trimmed body.
		var e *AIError
		if pr.classifyErr != nil {
			e = pr.classifyErr(raw, resp.StatusCode)
		}
		if e == nil {
			msg := strings.TrimSpace(string(raw))
			if len(msg) > 500 {
				msg = msg[:500] + "…"
			}
			e = &AIError{Kind: classifyStatus(resp.StatusCode), Status: resp.StatusCode, Message: msg}
		}
		ra := time.Duration(0)
		if isTransient(e) {
			ra = parseRetryAfter(resp.Header.Get("Retry-After"))
		}
		return nil, resp.StatusCode, ra, e
	}
	return raw, resp.StatusCode, 0, nil
}

// parseRetryAfter interprets an HTTP Retry-After value as either delta-seconds
// or an HTTP-date. Returns 0 when missing/unparseable. Caps at maxRetryAfter.
func parseRetryAfter(v string) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil {
		if secs <= 0 {
			return 0
		}
		d := time.Duration(secs) * time.Second
		if d > maxRetryAfter {
			return maxRetryAfter
		}
		return d
	}
	if t, err := http.ParseTime(v); err == nil {
		d := time.Until(t)
		if d <= 0 {
			return 0
		}
		if d > maxRetryAfter {
			return maxRetryAfter
		}
		return d
	}
	return 0
}

// jitterDuration applies ±25% randomized jitter to a backoff delay (#628).
// Zero and negative inputs stay zero so withFastRetry tests remain instant.
func jitterDuration(d time.Duration) time.Duration {
	if d <= 0 {
		return 0
	}
	// Factor in [0.75, 1.25].
	factor := 0.75 + rand.Float64()*0.5
	return time.Duration(float64(d) * factor)
}

// overallSendTimeout is the wall-clock envelope for the full retry sequence:
// per-attempt budget × (1 + retries) + sum(backoff) + margin (#628).
func overallSendTimeout(timeoutMs *int) time.Duration {
	perAttempt := DefaultTimeout
	if timeoutMs != nil && *timeoutMs > 0 {
		perAttempt = time.Duration(*timeoutMs) * time.Millisecond
	}
	var backoffSum time.Duration
	for _, d := range retryBackoff {
		backoffSum += d
	}
	// Cap Retry-After waits at maxRetryAfter per gap so the envelope still
	// bounds a hostile Retry-After path.
	retryGaps := len(retryBackoff)
	return perAttempt*time.Duration(1+retryGaps) + backoffSum + maxRetryAfter*time.Duration(retryGaps) + overallTimeoutMargin
}

// aiErrorFromContext maps a parent context error during retry backoff to a
// typed AIError: cancel stays cancel, deadline stays timeout (#628).
func aiErrorFromContext(err error) *AIError {
	if err == nil {
		return &AIError{Kind: ErrTimeout, Message: "aborted during retry backoff"}
	}
	if errors.Is(err, context.Canceled) {
		return &AIError{Kind: ErrCanceled, Message: fmt.Sprintf("aborted during retry backoff: %v", err)}
	}
	return &AIError{Kind: ErrTimeout, Message: fmt.Sprintf("aborted during retry backoff: %v", err)}
}

// classifyStatus maps an HTTP status to a normalized AIErrorKind. Centralized so
// every provider agrees on the taxonomy.
func classifyStatus(status int) AIErrorKind {
	switch {
	case status == 401:
		return ErrUnauthorized
	case status == 403:
		return ErrForbidden
	case status == 429:
		return ErrRateLimited
	case status == 404:
		return ErrModelMissing
	case status >= 400 && status < 500:
		return ErrBadRequest
	case status >= 500:
		return ErrServer
	default:
		return ErrUnknown
	}
}

// isTransient reports whether a provider error is worth retrying. Only
// server-side failures (5xx) and rate-limiting (429) qualify: a 5xx is the
// provider's own "try again" signal, and a 429 explicitly invites a backoff.
// 4xx errors (bad request, unauthorized, forbidden, model-missing) are
// deterministic — retrying an identical request only wastes the user's time.
// Transport errors (unreachable/timeout) are excluded too: unreachable
// usually means a misconfigured endpoint, and a timeout has already consumed
// the full per-call budget, so a retry would double an already-long wait.
func isTransient(aiErr *AIError) bool {
	return aiErr != nil && (aiErr.Kind == ErrServer || aiErr.Kind == ErrRateLimited)
}

// retryBackoff is the wait before each retry attempt; its length also caps the
// retry count (len entries → len retries on top of the initial attempt). The
// schedule is modest because 5xx/429 responses arrive immediately, so the
// waits — not request latency — dominate the cost of a persistent failure.
// ~2s of total backoff is a cheap price for absorbing a provider's transient
// blip (e.g. Google's OpenAI-compatible shim intermittently returns INTERNAL
// 500s). A package var (not a const) so tests can shrink it to keep the suite
// fast.
var retryBackoff = []time.Duration{
	500 * time.Millisecond,
	1500 * time.Millisecond,
}

// sendWithRetry wraps sendOnce with bounded retry on transient provider errors
// (5xx / 429). Each attempt gets its own per-call timeout — sendOnce derives a
// fresh context.WithTimeout every call — so one slow response does not eat the
// retry budget. An overall deadline envelope bounds the full sequence (attempts
// + backoff + Retry-After). Cancellation during backoff surfaces as
// ErrCanceled; overall/per-attempt deadline as ErrTimeout (#628).
func sendWithRetry(ctx context.Context, pr providerRequest, timeoutMs *int) ([]byte, int, *AIError) {
	if len(pr.body) > MaxRequestBytes {
		return nil, 0, &AIError{Kind: ErrBadRequest, Message: fmt.Sprintf("request body exceeds %d-byte cap", MaxRequestBytes)}
	}
	// Overall envelope so retries cannot run unbounded wall-clock time.
	ctx, cancel := context.WithTimeout(ctx, overallSendTimeout(timeoutMs))
	defer cancel()

	var last *AIError
	var lastStatus int
	for attempt := 0; attempt <= len(retryBackoff); attempt++ {
		raw, status, retryAfter, aiErr := sendOnce(ctx, pr, timeoutMs)
		if aiErr == nil {
			return raw, status, nil
		}
		last, lastStatus = aiErr, status
		// Non-transient errors (4xx, transport, cancel) return immediately.
		if !isTransient(aiErr) {
			return raw, status, aiErr
		}
		// Transient: wait before the next attempt, unless this was the last try.
		if attempt < len(retryBackoff) {
			// Jitter the local ladder only; Retry-After is a floor that must
			// not be shortened by negative jitter (#628).
			wait := jitterDuration(retryBackoff[attempt])
			if retryAfter > wait {
				wait = retryAfter
			}
			if wait <= 0 {
				// Zero backoff (tests): still honor cancel between attempts.
				if err := ctx.Err(); err != nil {
					return nil, lastStatus, aiErrorFromContext(err)
				}
				continue
			}
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, lastStatus, aiErrorFromContext(ctx.Err())
			case <-timer.C:
			}
		}
	}
	return nil, lastStatus, last
}

// Complete performs a buffered (non-streaming) chat completion against the
// configured provider. It validates the shared inputs then dispatches on
// ProviderType to the matching native encoder. For token-by-token delivery use
// CompleteStream (#226); the Stream field on CompleteRequest is ignored here
// and the provider is always asked for a full buffered body.
func Complete(ctx context.Context, req CompleteRequest) (CompleteResult, error) {
	if len(req.Messages) == 0 {
		return CompleteResult{}, &AIError{Kind: ErrBadRequest, Message: "messages must not be empty"}
	}
	model := req.Model
	if model == "" {
		model = req.Provider.Model
	}
	if model == "" {
		return CompleteResult{}, &AIError{Kind: ErrBadRequest, Message: "no model configured"}
	}
	baseURL := strings.TrimRight(req.Provider.BaseURL, "/")
	if baseURL == "" {
		return CompleteResult{}, &AIError{Kind: ErrUnreachable, Message: "no base URL configured"}
	}
	if err := validateBaseURL(baseURL); err != nil {
		return CompleteResult{}, &AIError{Kind: ErrBadRequest, Message: err.Error()}
	}
	switch req.Provider.ProviderType {
	case ProviderGoogle:
		return completeGoogle(ctx, req, model, baseURL)
	case ProviderAnthropic:
		return completeAnthropic(ctx, req, model, baseURL)
	default:
		// ProviderLocal and ProviderOpenAICompatible (and any unknown value)
		// all use the OpenAI-compatible shape — it is the universal default.
		return completeOpenAI(ctx, req, model, baseURL)
	}
}

// Embed computes embeddings for a batch of texts against the configured
// provider. Google uses native batchEmbedContents; the OpenAI-compatible path
// uses /v1/embeddings; Anthropic has no embeddings endpoint and returns a clear
// error. The whole batch is sent in a single request; embeddings[i]
// corresponds to texts[i].
func Embed(ctx context.Context, req EmbedRequest) (EmbedResult, error) {
	if len(req.Texts) == 0 {
		return EmbedResult{}, &AIError{Kind: ErrBadRequest, Message: "texts must not be empty"}
	}
	model := req.Model
	if model == "" {
		model = req.Provider.Model
	}
	if model == "" {
		return EmbedResult{}, &AIError{Kind: ErrBadRequest, Message: "no model configured"}
	}
	baseURL := strings.TrimRight(req.Provider.BaseURL, "/")
	if baseURL == "" {
		return EmbedResult{}, &AIError{Kind: ErrUnreachable, Message: "no base URL configured"}
	}
	if err := validateBaseURL(baseURL); err != nil {
		return EmbedResult{}, &AIError{Kind: ErrBadRequest, Message: err.Error()}
	}
	switch req.Provider.ProviderType {
	case ProviderGoogle:
		return embedGoogle(ctx, req, model, baseURL)
	case ProviderAnthropic:
		return EmbedResult{}, &AIError{Kind: ErrBadRequest, Message: "anthropic provider does not support embeddings; use an OpenAI-compatible or local embedding endpoint"}
	default:
		return embedOpenAI(ctx, req, model, baseURL)
	}
}

// Probe performs the minimal call the AI Provider page's "Test connection" uses:
// a 1-token chat completion (chat) or a single short embed (embedding). It
// returns nil on success and an *AIError (normalized) on failure. isChat selects
// the probe kind so one binding serves both provider blocks. Dispatches through
// Complete/Embed, so every native provider is probed via its own API shape.
func Probe(ctx context.Context, p AIProvider, isChat bool) error {
	if isChat {
		// No MaxTokens cap: some reasoning endpoints reject an extremely low
		// token budget (treating it as an impossible request) and would fail
		// "Test connection" for a config that actually works in normal use.
		// The provider timeout already bounds the probe's wall-clock cost.
		_, err := Complete(ctx, CompleteRequest{
			Provider: p,
			Messages: []ChatMessage{{Role: "user", Content: "ping"}},
		})
		return err
	}
	_, err := Embed(ctx, EmbedRequest{Provider: p, Texts: []string{"ping"}})
	return err
}

// ListModels polls the configured provider's model-list endpoint and returns the
// available models. which ("chat" | "embedding") lets the Google path filter to
// generateContent- vs embedContent-supporting models. A failed/empty poll
// returns nil + a typed error; the caller (the Settings UI) falls back to a
// free-text model field rather than dead-ending. Dispatches on ProviderType
// exactly like Complete/Embed.
func ListModels(ctx context.Context, p AIProvider, which string) ([]AIModel, error) {
	baseURL := strings.TrimRight(p.BaseURL, "/")
	if baseURL == "" {
		return nil, &AIError{Kind: ErrUnreachable, Message: "no base URL configured"}
	}
	if err := validateBaseURL(baseURL); err != nil {
		return nil, &AIError{Kind: ErrBadRequest, Message: err.Error()}
	}
	switch p.ProviderType {
	case ProviderGoogle:
		return listModelsGoogle(ctx, p, baseURL, which)
	case ProviderAnthropic:
		return listModelsAnthropic(ctx, p, baseURL)
	default:
		return listModelsOpenAI(ctx, p, baseURL)
	}
}
