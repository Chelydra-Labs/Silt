// Streaming chat completions (#226 / Sprint 22).
//
// OpenAI-compatible providers (Ollama /v1, OpenRouter, OpenAI, LM Studio, …)
// return Server-Sent Events when stream=true. This file parses that SSE body
// and delivers content deltas via a callback so the App layer can push them
// over Wails events without buffering the full response first.
//
// Native Google/Anthropic streaming is intentionally out of scope for v1 —
// CompleteStream returns ErrBadRequest for those provider types so callers
// fall back to non-stream Complete or surface a clear error.

package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// StreamDeltaFn is invoked once per content delta. Returning a non-nil error
// aborts the stream (the HTTP body is closed and CompleteStream returns that
// error wrapped as AIError when appropriate). Used for backpressure: the App
// layer returns an error when its outbound buffer is full.
type StreamDeltaFn func(delta string) error

// ToolCallDelta is one streamed fragment of a tool call (#595). OpenAI-compat
// providers split a single tool_call across chunks: the first carries id+name,
// later chunks append to arguments. Index identifies which call in the
// parallel-call set this fragment belongs to.
type ToolCallDelta struct {
	Index             int    `json:"index"`
	ID                string `json:"id,omitempty"`
	Name              string `json:"name,omitempty"`
	ArgumentsFragment string `json:"arguments_fragment,omitempty"`
}

// StreamToolDeltaFn is invoked once per tool-call fragment. Returning a non-nil
// error aborts the stream, mirroring StreamDeltaFn. May be nil when the caller
// does not surface live tool-call progress (the calls are still aggregated onto
// the final CompleteResult.ToolCalls).
type StreamToolDeltaFn func(delta ToolCallDelta) error

// MaxStreamBytes bounds the total accumulated content of a single streamed
// completion. Mirrors MaxResponseBytes intent for chat (much smaller than
// embedding batches) while still allowing long answers.
const MaxStreamBytes = 10 * 1024 * 1024 // 10 MB

// streamChatChunk is one OpenAI-compatible SSE data payload.
type streamChatChunk struct {
	Choices []struct {
		Delta struct {
			Content   string                `json:"content"`
			ToolCalls []openaiToolCallDelta `json:"tool_calls,omitempty"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Model string `json:"model"`
	Usage *struct {
		PromptTokens     *int `json:"prompt_tokens"`
		CompletionTokens *int `json:"completion_tokens"`
		TotalTokens      *int `json:"total_tokens"`
	} `json:"usage,omitempty"`
}

// openaiToolCallDelta is one streamed fragment of a tool call. Arguments
// arrive as a partial JSON string that must be concatenated across chunks.
type openaiToolCallDelta struct {
	Index    int                      `json:"index"`
	ID       string                   `json:"id,omitempty"`
	Type     string                   `json:"type,omitempty"`
	Function *openaiToolCallDeltaFunc `json:"function,omitempty"`
}

type openaiToolCallDeltaFunc struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

// CompleteStream performs a streaming chat completion. onDelta is called for
// each content token/chunk; onToolDelta (optional) is called for each tool-call
// fragment. The returned CompleteResult.Content is the full concatenated text
// (pre-reasoning-strip; the SDK strips on the frontend) and ToolCalls holds the
// reassembled tool invocations (#595).
//
// Supported for ProviderLocal and ProviderOpenAICompatible only. Native
// providers return ErrBadRequest so the UI can fall back or prompt the user.
func CompleteStream(ctx context.Context, req CompleteRequest, onDelta StreamDeltaFn, onToolDelta StreamToolDeltaFn) (CompleteResult, error) {
	if len(req.Messages) == 0 {
		return CompleteResult{}, &AIError{Kind: ErrBadRequest, Message: "messages must not be empty"}
	}
	if onDelta == nil {
		return CompleteResult{}, &AIError{Kind: ErrBadRequest, Message: "stream callback is required"}
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
	case ProviderGoogle, ProviderAnthropic:
		return CompleteResult{}, &AIError{
			Kind:    ErrBadRequest,
			Message: "streaming is not supported for native " + req.Provider.ProviderType + " providers; use an OpenAI-compatible or local endpoint, or call complete without stream",
		}
	default:
		return streamOpenAI(ctx, req, model, baseURL, onDelta, onToolDelta)
	}
}

// streamOpenAI issues stream=true against /v1/chat/completions and parses SSE.
func streamOpenAI(ctx context.Context, req CompleteRequest, model, baseURL string, onDelta StreamDeltaFn, onToolDelta StreamToolDeltaFn) (CompleteResult, error) {
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
		Stream:          true,
		Tools:           openaiTools(req.Tools),
		ToolChoice:      openaiToolChoice(req.ToolChoice),
	})
	if err != nil {
		return CompleteResult{}, &AIError{Kind: ErrUnknown, Message: fmt.Sprintf("marshal request: %v", err)}
	}
	if len(body) > MaxRequestBytes {
		return CompleteResult{}, &AIError{Kind: ErrBadRequest, Message: fmt.Sprintf("request body exceeds %d-byte cap", MaxRequestBytes)}
	}

	timeout := DefaultTimeout
	if req.Provider.TimeoutMs != nil && *req.Provider.TimeoutMs > 0 {
		timeout = time.Duration(*req.Provider.TimeoutMs) * time.Millisecond
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return CompleteResult{}, &AIError{Kind: ErrUnreachable, Message: fmt.Sprintf("build request: %v", err)}
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if req.Provider.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+req.Provider.APIKey)
	}

	resp, err := httpClient.Do(httpReq)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return CompleteResult{}, &AIError{Kind: ErrTimeout, Message: fmt.Sprintf("request timed out after %s: %v", timeout, err)}
		}
		if ctx.Err() == context.Canceled {
			return CompleteResult{}, &AIError{Kind: ErrTimeout, Message: fmt.Sprintf("stream cancelled: %v", err)}
		}
		return CompleteResult{}, &AIError{Kind: ErrUnreachable, Message: err.Error()}
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		msg := strings.TrimSpace(string(raw))
		if len(msg) > 500 {
			msg = msg[:500] + "…"
		}
		return CompleteResult{}, &AIError{Kind: classifyStatus(resp.StatusCode), Status: resp.StatusCode, Message: msg}
	}

	return parseOpenAISSE(resp.Body, model, onDelta, onToolDelta)
}

// parseOpenAISSE reads an OpenAI-compatible SSE body and invokes onDelta for
// each content delta and onToolDelta for each tool-call fragment. Tool-call
// fragments are accumulated by index and reassembled onto the final result's
// ToolCalls (OpenAI splits one call across many chunks).
func parseOpenAISSE(r io.Reader, fallbackModel string, onDelta StreamDeltaFn, onToolDelta StreamToolDeltaFn) (CompleteResult, error) {
	scanner := bufio.NewScanner(r)
	// Default scanner buffer is 64KB; raise for large SSE lines (rare but real).
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var (
		content strings.Builder
		model   = fallbackModel
		usage   *AIUsage
	)
	// toolAccum reassembles a single tool call from streamed fragments.
	type toolAccum struct {
		id   string
		name string
		args strings.Builder
	}
	accum := map[int]*toolAccum{}
	var accumOrder []int

	for scanner.Scan() {
		line := scanner.Text()
		// SSE comment / heartbeat lines start with ':' — ignore.
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			// Ignore event: / id: / retry: fields; we only need data payloads.
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" {
			continue
		}
		if payload == "[DONE]" {
			break
		}
		var chunk streamChatChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			// Skip malformed chunks rather than aborting a mostly-good stream;
			// a single bad frame is not fatal for chat UX.
			continue
		}
		if chunk.Model != "" {
			model = chunk.Model
		}
		if chunk.Usage != nil {
			usage = &AIUsage{
				PromptTokens:     chunk.Usage.PromptTokens,
				CompletionTokens: chunk.Usage.CompletionTokens,
				TotalTokens:      chunk.Usage.TotalTokens,
			}
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		choice := chunk.Choices[0]
		// Content delta.
		if choice.Delta.Content != "" {
			if content.Len()+len(choice.Delta.Content) > MaxStreamBytes {
				return CompleteResult{}, &AIError{Kind: ErrServer, Message: fmt.Sprintf("stream content exceeds %d-byte cap", MaxStreamBytes)}
			}
			content.WriteString(choice.Delta.Content)
			if err := onDelta(choice.Delta.Content); err != nil {
				return CompleteResult{}, &AIError{Kind: ErrUnknown, Message: fmt.Sprintf("stream consumer aborted: %v", err)}
			}
		}
		// Tool-call fragments: accumulate by index and forward to onToolDelta.
		for _, d := range choice.Delta.ToolCalls {
			a, ok := accum[d.Index]
			if !ok {
				a = &toolAccum{}
				accum[d.Index] = a
				accumOrder = append(accumOrder, d.Index)
			}
			frag := ToolCallDelta{Index: d.Index}
			if d.ID != "" {
				a.id = d.ID
				frag.ID = d.ID
			}
			if d.Function != nil {
				if d.Function.Name != "" {
					a.name = d.Function.Name
					frag.Name = d.Function.Name
				}
				if d.Function.Arguments != "" {
					a.args.WriteString(d.Function.Arguments)
					frag.ArgumentsFragment = d.Function.Arguments
				}
			}
			if onToolDelta != nil {
				if err := onToolDelta(frag); err != nil {
					return CompleteResult{}, &AIError{Kind: ErrUnknown, Message: fmt.Sprintf("stream tool consumer aborted: %v", err)}
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return CompleteResult{}, &AIError{Kind: ErrUnknown, Message: fmt.Sprintf("read stream: %v", err)}
	}

	// Reassemble accumulated tool calls in arrival order.
	var toolCalls []ToolCall
	for _, idx := range accumOrder {
		a := accum[idx]
		toolCalls = append(toolCalls, ToolCall{
			ID:        a.id,
			Name:      a.name,
			Arguments: openaiArgsToRaw(a.args.String()),
		})
	}

	return CompleteResult{
		Content:   content.String(),
		Model:     model,
		Usage:     usage,
		ToolCalls: toolCalls,
	}, nil
}
