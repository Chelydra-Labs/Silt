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
// "user" or "assistant"; content is the simple string form (the API also accepts
// a content-blocks array, but the string form is the universal denominator).
type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
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
	// pass through verbatim.
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
		role := m.Role
		if role != "assistant" {
			role = "user"
		}
		msgs = append(msgs, anthropicMessage{Role: role, Content: m.Content})
	}
	// max_tokens is mandatory for Anthropic. Default when the caller omits it.
	maxTokens := anthropicDefaultMaxTokens
	if req.MaxTokens != nil && *req.MaxTokens > 0 {
		maxTokens = *req.MaxTokens
	}
	body, err := json.Marshal(anthropicRequest{
		Model:       model,
		MaxTokens:   maxTokens,
		System:      system.String(),
		Messages:    msgs,
		Temperature: req.Temperature,
		Tools:       anthropicStructuredTools(req.ResponseSchema),
		ToolChoice:  anthropicStructuredChoice(req.ResponseSchema),
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
	// Concatenate all text blocks. When forced tool-use is active, extract the
	// structured output from the tool_use block's input (JSON-stringified so
	// the existing string-based contract holds). A text block alongside a
	// tool_use block (thinking text) is ignored for the structured result.
	var sb strings.Builder
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
		if b.Type == "text" {
			sb.WriteString(b.Text)
		}
	}
	if sb.Len() == 0 {
		return CompleteResult{}, &AIError{Kind: ErrUnknown, Message: "provider returned no text content"}
	}
	out := CompleteResult{Content: sb.String(), Model: resp.Model}
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

// anthropicStructuredTools returns a single forced tool whose input_schema is
// the given JSON Schema, or nil when no schema is set (no structured output).
func anthropicStructuredTools(schema json.RawMessage) []anthropicTool {
	if len(schema) == 0 {
		return nil
	}
	return []anthropicTool{{
		Name:        anthropicStructuredToolName,
		Description: "Return the structured result",
		InputSchema: schema,
	}}
}

// anthropicStructuredChoice forces the model to call the structured-output tool
// when a schema is set, or nil when no schema is set.
func anthropicStructuredChoice(schema json.RawMessage) any {
	if len(schema) == 0 {
		return nil
	}
	return map[string]string{"type": "tool", "name": anthropicStructuredToolName}
}
