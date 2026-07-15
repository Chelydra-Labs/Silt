// OpenAI-compatible provider: /v1/chat/completions + /v1/embeddings. This is the
// universal default/fallback spoken by Ollama, llama-server, OpenRouter, LM
// Studio, OpenAI, and Google's compat shim. Auth is Authorization: Bearer when a
// key is present (keyless local endpoints stay anonymous). Extracted from the
// original single-shape service so native providers can share the transport.

package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// chatRequest is the OpenAI-compatible /v1/chat/completions request body.
type chatRequest struct {
	Model           string          `json:"model"`
	Messages        []openaiMessage `json:"messages"`
	Temperature     *float64        `json:"temperature,omitempty"`
	MaxTokens       *int            `json:"max_tokens,omitempty"`
	ReasoningEffort *string         `json:"reasoning_effort,omitempty"`
	Stream          bool            `json:"stream,omitempty"`
	Tools           []openaiTool    `json:"tools,omitempty"`
	ToolChoice      any             `json:"tool_choice,omitempty"`
}

// openaiMessage is the wire shape for one message. Assistant turns that
// requested tools carry tool_calls; tool results carry tool_call_id. Plain
// text turns carry only Role + Content (the historical shape).
type openaiMessage struct {
	Role       string           `json:"role"`
	Content    string           `json:"content,omitempty"`
	ToolCalls  []openaiToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
}

// openaiTool is one entry in the request tools[] array: a function wrapper.
type openaiTool struct {
	Type     string        `json:"type"` // always "function"
	Function openaiToolDef `json:"function"`
}

// openaiToolDef is the function definition inside a tool entry.
type openaiToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// openaiToolCall is one fully-formed tool invocation in an assistant message
// (history replay). Arguments is a JSON-stringified object on the OpenAI wire.
type openaiToolCall struct {
	ID       string             `json:"id"`
	Type     string             `json:"type"` // always "function"
	Function openaiToolCallFunc `json:"function"`
}

type openaiToolCallFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// openaiMessages converts the unified ChatMessage slice into the OpenAI wire
// shape. Tool-call turns and tool-result turns are encoded per OpenAI's
// chat-completions contract; plain text turns pass through unchanged.
func openaiMessages(msgs []ChatMessage) []openaiMessage {
	out := make([]openaiMessage, 0, len(msgs))
	for _, m := range msgs {
		om := openaiMessage{Role: m.Role, Content: m.Content, ToolCallID: m.ToolCallID}
		if len(m.ToolCalls) > 0 {
			om.ToolCalls = make([]openaiToolCall, len(m.ToolCalls))
			for i, tc := range m.ToolCalls {
				om.ToolCalls[i] = openaiToolCall{
					ID:       tc.ID,
					Type:     "function",
					Function: openaiToolCallFunc{Name: tc.Name, Arguments: rawToOpenAIArgs(tc.Arguments)},
				}
			}
		}
		out = append(out, om)
	}
	return out
}

// openaiTools encodes the unified ToolDef slice as OpenAI function tools. An
// empty Parameters schema is normalized to an empty object schema so the
// provider doesn't reject the tool.
func openaiTools(tools []ToolDef) []openaiTool {
	if len(tools) == 0 {
		return nil
	}
	out := make([]openaiTool, len(tools))
	for i, t := range tools {
		params := normalizeSchemaObject(t.Parameters)
		out[i] = openaiTool{
			Type: "function",
			Function: openaiToolDef{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  params,
			},
		}
	}
	return out
}

// openaiToolChoice encodes the unified ToolChoice as the OpenAI tool_choice
// field: a bare keyword ("auto"/"required"/"none") or a function-name object
// for force. nil returns nil (field omitted).
func openaiToolChoice(tc *ToolChoice) any {
	if tc == nil {
		return nil
	}
	switch tc.Mode {
	case ToolChoiceAuto, ToolChoiceNone, ToolChoiceRequired:
		return tc.Mode
	case ToolChoiceForce:
		if tc.ToolName == "" {
			return ToolChoiceRequired
		}
		return map[string]any{
			"type":     "function",
			"function": map[string]any{"name": tc.ToolName},
		}
	}
	return nil
}

// rawToOpenAIArgs renders a JSON RawMessage (object) as the stringified JSON
// OpenAI expects in a tool-call's arguments field. Empty → "{}".
func rawToOpenAIArgs(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "{}"
	}
	return string(raw)
}

// openaiArgsToRaw turns OpenAI's stringified-JSON arguments into the raw JSON
// object bytes the unified ToolCall carries. Non-JSON strings are wrapped as a
// JSON string so they round-trip safely.
func openaiArgsToRaw(s string) json.RawMessage {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return nil
	}
	if json.Valid([]byte(trimmed)) {
		return json.RawMessage(trimmed)
	}
	b, _ := json.Marshal(s)
	return b
}

// normalizeSchemaObject returns a non-empty JSON object schema, defaulting to
// {"type":"object","properties":{}} when raw is empty so providers don't reject
// a parameterless tool.
func normalizeSchemaObject(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(`{"type":"object","properties":{}}`)
	}
	return raw
}

// chatResponse is the OpenAI-compatible /v1/chat/completions response body.
type chatResponse struct {
	Choices []struct {
		Message struct {
			Content   string `json:"content"`
			ToolCalls []struct {
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
	Model string `json:"model"`
	Usage *struct {
		PromptTokens     *int `json:"prompt_tokens"`
		CompletionTokens *int `json:"completion_tokens"`
		TotalTokens      *int `json:"total_tokens"`
	} `json:"usage,omitempty"`
}

// embedRequest is the OpenAI-compatible /v1/embeddings request body.
type embedRequest struct {
	Model      string   `json:"model"`
	Input      []string `json:"input"`
	Dimensions *int     `json:"dimensions,omitempty"`
}

// embedResponse is the OpenAI-compatible /v1/embeddings response body.
type embedResponse struct {
	Data []struct {
		Embedding []float64 `json:"embedding"`
	} `json:"data"`
	Model string `json:"model"`
	Usage *struct {
		PromptTokens *int `json:"prompt_tokens"`
		TotalTokens  *int `json:"total_tokens"`
	} `json:"usage,omitempty"`
}

// completeOpenAI performs a chat completion against /v1/chat/completions.
func completeOpenAI(ctx context.Context, req CompleteRequest, model, baseURL string) (CompleteResult, error) {
	reasoning := req.ReasoningEffort
	if reasoning == nil {
		reasoning = req.Provider.ReasoningEffort
	}
	body, err := json.Marshal(chatRequest{
		Model:           model,
		Messages:        openaiMessages(req.Messages),
		Temperature:     req.Temperature,
		MaxTokens:       req.MaxTokens,
		ReasoningEffort: reasoning,
		Stream:          false, // buffered path; streaming uses streamOpenAI
		Tools:           openaiTools(req.Tools),
		ToolChoice:      openaiToolChoice(req.ToolChoice),
	})
	if err != nil {
		return CompleteResult{}, &AIError{Kind: ErrUnknown, Message: fmt.Sprintf("marshal request: %v", err)}
	}
	pr := providerRequest{
		method: "POST",
		url:    baseURL + "/v1/chat/completions",
		body:   body,
		setHeaders: func(r *http.Request) {
			// Ollama's OpenAI-compatible routes accept any non-empty Bearer
			// token; cloud providers require a real key. Sending an empty
			// bearer would 401 on cloud, so only attach when present.
			if req.Provider.APIKey != "" {
				r.Header.Set("Authorization", "Bearer "+req.Provider.APIKey)
			}
		},
	}
	raw, _, aiErr := sendWithRetry(ctx, pr, req.Provider.TimeoutMs)
	if aiErr != nil {
		return CompleteResult{}, aiErr
	}
	var resp chatResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return CompleteResult{}, &AIError{Kind: ErrUnknown, Message: fmt.Sprintf("parse response: %v", err)}
	}
	if len(resp.Choices) == 0 {
		return CompleteResult{}, &AIError{Kind: ErrUnknown, Message: "provider returned no choices"}
	}
	msg := resp.Choices[0].Message
	var toolCalls []ToolCall
	for _, tc := range msg.ToolCalls {
		toolCalls = append(toolCalls, ToolCall{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: openaiArgsToRaw(tc.Function.Arguments),
		})
	}
	out := CompleteResult{Content: msg.Content, Model: resp.Model, ToolCalls: toolCalls}
	if resp.Usage != nil {
		out.Usage = &AIUsage{
			PromptTokens:     resp.Usage.PromptTokens,
			CompletionTokens: resp.Usage.CompletionTokens,
			TotalTokens:      resp.Usage.TotalTokens,
		}
	}
	return out, nil
}

// embedOpenAI computes embeddings via /v1/embeddings.
func embedOpenAI(ctx context.Context, req EmbedRequest, model, baseURL string) (EmbedResult, error) {
	body, err := json.Marshal(embedRequest{Model: model, Input: req.Texts, Dimensions: req.Dimensions})
	if err != nil {
		return EmbedResult{}, &AIError{Kind: ErrUnknown, Message: fmt.Sprintf("marshal request: %v", err)}
	}
	pr := providerRequest{
		method: "POST",
		url:    baseURL + "/v1/embeddings",
		body:   body,
		setHeaders: func(r *http.Request) {
			if req.Provider.APIKey != "" {
				r.Header.Set("Authorization", "Bearer "+req.Provider.APIKey)
			}
		},
	}
	raw, _, aiErr := sendWithRetry(ctx, pr, req.Provider.TimeoutMs)
	if aiErr != nil {
		return EmbedResult{}, aiErr
	}
	var resp embedResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return EmbedResult{}, &AIError{Kind: ErrUnknown, Message: fmt.Sprintf("parse response: %v", err)}
	}
	if len(resp.Data) == 0 {
		return EmbedResult{}, &AIError{Kind: ErrUnknown, Message: "provider returned no embeddings"}
	}
	// Embeddings[i] ↔ Texts[i] is the contract; a partial response would
	// silently misalign vectors with inputs, so any count mismatch is fatal.
	if len(resp.Data) != len(req.Texts) {
		return EmbedResult{}, &AIError{Kind: ErrUnknown, Message: fmt.Sprintf("provider returned %d embeddings for %d texts", len(resp.Data), len(req.Texts))}
	}
	out := EmbedResult{
		Embeddings: make([][]float64, len(resp.Data)),
		Model:      resp.Model,
		Dimensions: len(resp.Data[0].Embedding),
	}
	for i, d := range resp.Data {
		out.Embeddings[i] = d.Embedding
	}
	if resp.Usage != nil {
		out.Usage = &AIUsage{PromptTokens: resp.Usage.PromptTokens, TotalTokens: resp.Usage.TotalTokens}
	}
	return out, nil
}

// listModelsOpenAI polls GET <baseURL>/v1/models (served by Ollama, OpenRouter,
// LM Studio, OpenAI, Google's compat shim, …). Returns model ids; DisplayName
// equals ID since the compat shape carries no human-readable name.
func listModelsOpenAI(ctx context.Context, p AIProvider, baseURL string) ([]AIModel, error) {
	pr := providerRequest{
		method: "GET",
		url:    baseURL + "/v1/models",
		setHeaders: func(r *http.Request) {
			if p.APIKey != "" {
				r.Header.Set("Authorization", "Bearer "+p.APIKey)
			}
		},
	}
	raw, _, aiErr := sendWithRetry(ctx, pr, p.TimeoutMs)
	if aiErr != nil {
		return nil, aiErr
	}
	var resp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, &AIError{Kind: ErrUnknown, Message: fmt.Sprintf("parse models response: %v", err)}
	}
	out := make([]AIModel, 0, len(resp.Data))
	for _, m := range resp.Data {
		if id := strings.TrimSpace(m.ID); id != "" {
			out = append(out, AIModel{ID: id, DisplayName: id})
		}
	}
	return out, nil
}
