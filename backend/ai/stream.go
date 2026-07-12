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

// MaxStreamBytes bounds the total accumulated content of a single streamed
// completion. Mirrors MaxResponseBytes intent for chat (much smaller than
// embedding batches) while still allowing long answers.
const MaxStreamBytes = 10 * 1024 * 1024 // 10 MB

// streamChatChunk is one OpenAI-compatible SSE data payload.
type streamChatChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
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

// CompleteStream performs a streaming chat completion. onDelta is called for
// each content token/chunk. The returned CompleteResult.Content is the full
// concatenated text (pre-reasoning-strip; the SDK strips on the frontend).
//
// Supported for ProviderLocal and ProviderOpenAICompatible only. Native
// providers return ErrBadRequest so the UI can fall back or prompt the user.
func CompleteStream(ctx context.Context, req CompleteRequest, onDelta StreamDeltaFn) (CompleteResult, error) {
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
		return streamOpenAI(ctx, req, model, baseURL, onDelta)
	}
}

// streamOpenAI issues stream=true against /v1/chat/completions and parses SSE.
func streamOpenAI(ctx context.Context, req CompleteRequest, model, baseURL string, onDelta StreamDeltaFn) (CompleteResult, error) {
	reasoning := req.ReasoningEffort
	if reasoning == nil {
		reasoning = req.Provider.ReasoningEffort
	}
	body, err := json.Marshal(chatRequest{
		Model:           model,
		Messages:        req.Messages,
		Temperature:     req.Temperature,
		MaxTokens:       req.MaxTokens,
		ReasoningEffort: reasoning,
		Stream:          true,
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

	return parseOpenAISSE(resp.Body, model, onDelta)
}

// parseOpenAISSE reads an OpenAI-compatible SSE body and invokes onDelta for
// each content delta. Accumulates full content for the final CompleteResult.
func parseOpenAISSE(r io.Reader, fallbackModel string, onDelta StreamDeltaFn) (CompleteResult, error) {
	scanner := bufio.NewScanner(r)
	// Default scanner buffer is 64KB; raise for large SSE lines (rare but real).
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var (
		content strings.Builder
		model   = fallbackModel
		usage   *AIUsage
	)

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
		delta := chunk.Choices[0].Delta.Content
		if delta == "" {
			continue
		}
		if content.Len()+len(delta) > MaxStreamBytes {
			return CompleteResult{}, &AIError{Kind: ErrServer, Message: fmt.Sprintf("stream content exceeds %d-byte cap", MaxStreamBytes)}
		}
		content.WriteString(delta)
		if err := onDelta(delta); err != nil {
			return CompleteResult{}, &AIError{Kind: ErrUnknown, Message: fmt.Sprintf("stream consumer aborted: %v", err)}
		}
	}
	if err := scanner.Err(); err != nil {
		return CompleteResult{}, &AIError{Kind: ErrUnknown, Message: fmt.Sprintf("read stream: %v", err)}
	}

	return CompleteResult{
		Content: content.String(),
		Model:   model,
		Usage:   usage,
	}, nil
}
