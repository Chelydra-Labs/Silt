// Native Anthropic (Claude) provider. Speaks the Messages API against
// api.anthropic.com/v1/messages. Auth is the x-api-key header plus the mandatory
// anthropic-version header (never Authorization: Bearer). Anthropic has no
// native embeddings endpoint — Embed returns a clear error pointing to the
// OpenAI-compatible or local path.

package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// anthropicVersion is the single stable API version string required on every
// Anthropic request. Documented at platform.claude.com/docs/en/api/getting-started.
const anthropicVersion = "2023-06-01"

// anthropicDefaultMaxTokens is the max_tokens default when the caller omits it.
// Anthropic's max_tokens is MANDATORY (unlike OpenAI's optional field); sending
// 0/nil yields a 400. 4096 is a generous round number that covers any reasonable
// summary/extraction without being so large the provider rejects it.
const anthropicDefaultMaxTokens = 4096

// anthropicMessage is one message in the Anthropic messages[] array. Role is
// "user" or "assistant". Content is raw JSON: a JSON string for plain text
// turns (the universal denominator) or an array of content blocks for
// tool-bearing turns (assistant tool_use, user tool_result).
type anthropicMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// anthropicRequest is the /v1/messages request body.
type anthropicRequest struct {
	Model       string             `json:"model"`
	MaxTokens   int                `json:"max_tokens"`
	System      string             `json:"system,omitempty"`
	Messages    []anthropicMessage `json:"messages"`
	Temperature *float64           `json:"temperature,omitempty"`
	// Forced tool-use for structured output (D1 revisit). When Tools is set,
	// ToolChoice forces the model to emit a tool_use block whose input conforms
	// to the schema. The decoder extracts the input as JSON.
	Tools      []anthropicTool `json:"tools,omitempty"`
	ToolChoice any             `json:"tool_choice,omitempty"`
}

// anthropicTool defines a single tool the model may call. For structured output
// we define exactly one tool whose input_schema is the desired JSON Schema and
// force the model to call it.
type anthropicTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// Structured-output tool name. The decoder looks for a tool_use block with
// this name and extracts its input as the JSON result.
const anthropicStructuredToolName = "structured_output"

// anthropicResponse is the /v1/messages response body.
type anthropicResponse struct {
	Content []struct {
		Type  string          `json:"type"`
		ID    string          `json:"id"`
		Text  string          `json:"text"`
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input"`
	} `json:"content"`
	Model      string `json:"model"`
	StopReason string `json:"stop_reason"`
	Usage      *struct {
		InputTokens  *int `json:"input_tokens"`
		OutputTokens *int `json:"output_tokens"`
	} `json:"usage,omitempty"`
}

// anthropicError is the structured Anthropic error body:
// {type: "error", error: {type, message}}.
type anthropicError struct {
	Type  string `json:"type"`
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// anthropicClassifyError maps an Anthropic structured error body to an AIError.
// Returns nil when the body isn't the expected shape so the default status-based
// path runs.
func anthropicClassifyError(raw []byte, status int) *AIError {
	var ae anthropicError
	if err := json.Unmarshal(raw, &ae); err != nil || ae.Error.Message == "" {
		return nil
	}
	kind := classifyStatus(status)
	switch ae.Error.Type {
	case "invalid_request_error":
		kind = ErrBadRequest
	case "authentication_error":
		kind = ErrUnauthorized
	case "permission_error":
		kind = ErrForbidden
	case "not_found_error":
		kind = ErrModelMissing
	case "rate_limit_error":
		kind = ErrRateLimited
	case "api_error", "overloaded_error":
		kind = ErrServer
	}
	msg := strings.TrimSpace(ae.Error.Message)
	if len(msg) > 500 {
		msg = msg[:500] + "…"
	}
	return &AIError{Kind: kind, Status: status, Message: msg}
}

// completeAnthropic performs a chat completion via /v1/messages.
func completeAnthropic(ctx context.Context, req CompleteRequest, model, baseURL string) (CompleteResult, error) {
	// Split system messages into the top-level system field (Anthropic does not
	// accept role:"system" in messages[]). Remaining user/assistant messages
	// pass through, with tool-bearing turns encoded as content blocks.
	var system strings.Builder
	var msgs []anthropicMessage
	for _, m := range req.Messages {
		if m.Role == "system" {
			if system.Len() > 0 {
				system.WriteString("\n\n")
			}
			system.WriteString(m.Content)
			continue
		}
		// Tool result → user turn carrying a tool_result content block keyed
		// to the prior tool_use id (Anthropic's tool-result wire shape).
		if m.Role == RoleTool {
			block := map[string]any{
				"type":        "tool_result",
				"tool_use_id": m.ToolCallID,
				"content":     m.Content,
			}
			msgs = append(msgs, anthropicMessage{
				Role:    RoleUser,
				Content: marshalContentBlocks([]map[string]any{block}),
			})
			continue
		}
		// Assistant turn that requested tools → assistant turn with tool_use
		// blocks (plus an optional leading text block when Content is set).
		if m.Role == RoleAssistant && len(m.ToolCalls) > 0 {
			blocks := make([]map[string]any, 0, 1+len(m.ToolCalls))
			if m.Content != "" {
				blocks = append(blocks, map[string]any{"type": "text", "text": m.Content})
			}
			for _, tc := range m.ToolCalls {
				blocks = append(blocks, map[string]any{
					"type":  "tool_use",
					"id":    tc.ID,
					"name":  tc.Name,
					"input": anthropicInputFromRaw(tc.Arguments),
				})
			}
			msgs = append(msgs, anthropicMessage{
				Role:    RoleAssistant,
				Content: marshalContentBlocks(blocks),
			})
			continue
		}
		role := m.Role
		if role != RoleAssistant {
			role = RoleUser
		}
		msgs = append(msgs, anthropicMessage{Role: role, Content: jsonString(m.Content)})
	}
	// max_tokens is mandatory for Anthropic. Default when the caller omits it.
	maxTokens := anthropicDefaultMaxTokens
	if req.MaxTokens != nil && *req.MaxTokens > 0 {
		maxTokens = *req.MaxTokens
	}
	tools, toolChoice := anthropicBuildTools(req)
	body, err := json.Marshal(anthropicRequest{
		Model:       model,
		MaxTokens:   maxTokens,
		System:      system.String(),
		Messages:    msgs,
		Temperature: req.Temperature,
		Tools:       tools,
		ToolChoice:  toolChoice,
	})
	if err != nil {
		return CompleteResult{}, &AIError{Kind: ErrUnknown, Message: fmt.Sprintf("marshal request: %v", err)}
	}
	pr := providerRequest{
		method: "POST",
		url:    baseURL + "/v1/messages",
		body:   body,
		setHeaders: func(r *http.Request) {
			if req.Provider.APIKey != "" {
				r.Header.Set("x-api-key", req.Provider.APIKey)
			}
			r.Header.Set("anthropic-version", anthropicVersion)
		},
		classifyErr: anthropicClassifyError,
	}
	raw, _, aiErr := sendWithRetry(ctx, pr, req.Provider.TimeoutMs)
	if aiErr != nil {
		return CompleteResult{}, aiErr
	}
	var resp anthropicResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return CompleteResult{}, &AIError{Kind: ErrUnknown, Message: fmt.Sprintf("parse response: %v", err)}
	}
	// Concatenate text blocks and collect real tool_use blocks. When forced
	// tool-use for structured output is active, extract that tool's input as
	// JSON-stringified content (existing contract). Any other tool_use block
	// becomes a ToolCall on the result.
	var sb strings.Builder
	var toolCalls []ToolCall
	for _, b := range resp.Content {
		if b.Type == "tool_use" && b.Name == anthropicStructuredToolName && len(b.Input) > 0 {
			// A truncated tool_use (stop_reason=max_tokens) produces incomplete
			// JSON that would fail silently in the parse-fallback. Surface a
			// clear error so the caller can retry with a higher token budget.
			if resp.StopReason == "max_tokens" {
				return CompleteResult{}, &AIError{Kind: ErrUnknown, Message: "structured output was truncated (stop_reason: max_tokens); increase max_tokens and retry"}
			}
			return CompleteResult{Content: string(b.Input), Model: resp.Model}, nil
		}
		if b.Type == "tool_use" {
			toolCalls = append(toolCalls, ToolCall{
				ID:        b.ID,
				Name:      b.Name,
				Arguments: anthropicInputFromRaw(b.Input),
			})
			continue
		}
		if b.Type == "text" {
			sb.WriteString(b.Text)
		}
	}
	if sb.Len() == 0 && len(toolCalls) == 0 {
		return CompleteResult{}, &AIError{Kind: ErrUnknown, Message: "provider returned no text content"}
	}
	out := CompleteResult{Content: sb.String(), Model: resp.Model, ToolCalls: toolCalls}
	if resp.Usage != nil {
		out.Usage = &AIUsage{
			PromptTokens:     resp.Usage.InputTokens,
			CompletionTokens: resp.Usage.OutputTokens,
		}
		if resp.Usage.InputTokens != nil && resp.Usage.OutputTokens != nil {
			total := *resp.Usage.InputTokens + *resp.Usage.OutputTokens
			out.Usage.TotalTokens = &total
		}
	}
	return out, nil
}

// listModelsAnthropic polls GET <baseURL>/v1/models. Carries display_name as the
// dropdown label. Follows cursor-based pagination (after_id + has_more) so the
// dropdown isn't silently truncated.
func listModelsAnthropic(ctx context.Context, p AIProvider, baseURL string) ([]AIModel, error) {
	var out []AIModel
	afterID := ""
	// Cap iterations as a safety valve against a misbehaving endpoint.
	for page := 0; page < 20; page++ {
		url := baseURL + "/v1/models?limit=100"
		if afterID != "" {
			url += "&after_id=" + afterID
		}
		pr := providerRequest{
			method: "GET",
			url:    url,
			setHeaders: func(r *http.Request) {
				if p.APIKey != "" {
					r.Header.Set("x-api-key", p.APIKey)
				}
				r.Header.Set("anthropic-version", anthropicVersion)
			},
			classifyErr: anthropicClassifyError,
		}
		raw, _, aiErr := sendWithRetry(ctx, pr, p.TimeoutMs)
		if aiErr != nil {
			return nil, aiErr
		}
		var resp struct {
			Data []struct {
				ID          string `json:"id"`
				DisplayName string `json:"display_name"`
			} `json:"data"`
			HasMore bool   `json:"has_more"`
			LastID  string `json:"last_id"`
		}
		if err := json.Unmarshal(raw, &resp); err != nil {
			return nil, &AIError{Kind: ErrUnknown, Message: fmt.Sprintf("parse models response: %v", err)}
		}
		for _, m := range resp.Data {
			id := strings.TrimSpace(m.ID)
			if id == "" {
				continue
			}
			display := strings.TrimSpace(m.DisplayName)
			if display == "" {
				display = id
			}
			out = append(out, AIModel{ID: id, DisplayName: display})
		}
		if !resp.HasMore || resp.LastID == "" {
			break
		}
		afterID = resp.LastID
	}
	return out, nil
}

// anthropicBuildTools assembles the tools array + tool_choice for a request,
// merging caller-provided real tools with the structured_output tool (#595).
//
// When ResponseSchema is set, the structured_output tool is appended AND its
// selection is forced — preserving the existing structured-output contract
// verbatim (a caller ToolChoice is ignored in that case). When only caller
// tools are present, the caller's ToolChoice maps onto Anthropic's tool_choice
// (nil → provider default / auto).
func anthropicBuildTools(req CompleteRequest) ([]anthropicTool, any) {
	var tools []anthropicTool
	for _, t := range req.Tools {
		tools = append(tools, anthropicTool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: normalizeSchemaObject(t.Parameters),
		})
	}
	if len(req.ResponseSchema) > 0 {
		tools = append(tools, anthropicTool{
			Name:        anthropicStructuredToolName,
			Description: "Return the structured result",
			InputSchema: req.ResponseSchema,
		})
		return tools, map[string]string{"type": "tool", "name": anthropicStructuredToolName}
	}
	return tools, anthropicToolChoiceFromRequest(req.ToolChoice)
}

// anthropicToolChoiceFromRequest maps the unified ToolChoice onto Anthropic's
// tool_choice shape: auto→auto, required→any, none→none, force→specific tool.
func anthropicToolChoiceFromRequest(tc *ToolChoice) any {
	if tc == nil {
		return nil
	}
	switch tc.Mode {
	case ToolChoiceAuto:
		return map[string]string{"type": "auto"}
	case ToolChoiceRequired:
		return map[string]string{"type": "any"}
	case ToolChoiceNone:
		return map[string]string{"type": "none"}
	case ToolChoiceForce:
		if tc.ToolName == "" {
			return map[string]string{"type": "any"}
		}
		return map[string]string{"type": "tool", "name": tc.ToolName}
	}
	return nil
}

// anthropicInputFromRaw normalizes a tool-call arguments RawMessage into a
// valid JSON object for Anthropic's tool_use input field (defaults to {} when
// empty or non-object).
func anthropicInputFromRaw(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(`{}`)
	}
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return json.RawMessage(`{}`)
	}
	return raw
}

// jsonString wraps a plain string as a JSON-encoded RawMessage.
func jsonString(s string) json.RawMessage {
	b, _ := json.Marshal(s)
	return b
}

// marshalContentBlocks serializes an Anthropic content-block array. The blocks
// are pre-built maps; this is a thin marshal helper that keeps callers terse.
func marshalContentBlocks(blocks []map[string]any) json.RawMessage {
	b, _ := json.Marshal(blocks)
	return b
}
