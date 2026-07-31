// Streaming chat completions (#226 / Sprint 22).
//
// OpenAI-compatible providers (Ollama /v1, OpenRouter, OpenAI, LM Studio, …)
// return Server-Sent Events when stream=true. This file parses that SSE body
// and delivers content deltas via a callback so the App layer can push them
// over Wails events without buffering the full response first.
//
// Native Google/Anthropic have no SSE streaming implementation in v1. Rather
// than reject (which broke the agent loop, which always requests stream=true),
// CompleteStream falls back to a buffered non-stream Complete and emits the
// full content as one delta so the stream contract holds transparently.

package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
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

// MaxStreamBytes bounds the total accumulated content and each tool-call's
// arguments in a streamed completion. Mirrors MaxResponseBytes intent for chat
// (much smaller than embedding batches) while still allowing long answers.
const MaxStreamBytes = 10 * 1024 * 1024 // 10 MB

// MaxStreamToolCalls bounds the number of distinct tool calls a single stream
// may accumulate, so a misbehaving endpoint cannot allocate unbounded entries.
const MaxStreamToolCalls = 128

// maxMalformedSSEFrames allows an isolated provider hiccup without making a
// stream unusable, but prevents a mostly-invalid response from being reported
// as a successful completion.
const maxMalformedSSEFrames = 3

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
	switch providerCategory(req.Provider.ProviderType) {
	case categoryGoogle, categoryAnthropic:
		// No SSE streaming for native providers in v1. Fall back to a buffered
		// non-stream completion and emit the full content as one delta so the
		// stream contract (deltas + final result) holds transparently for
		// callers (the agent loop) that always request stream=true.
		result, err := Complete(ctx, req)
		if err != nil {
			return CompleteResult{}, err
		}
		if result.Content != "" {
			if err := onDelta(result.Content); err != nil {
				return result, err
			}
		}
		return result, nil
	default:
		return streamOpenAI(ctx, req, model, baseURL, onDelta, onToolDelta)
	}
}

// streamOpenAI issues stream=true against /v1/chat/completions and parses SSE.
// Transient 5xx/429 on the connection/status phase are retried with the same
// budget as buffered sendWithRetry (#640). Once a 2xx body is obtained, SSE
// parsing is never retried (partial tool-call state is unsafe to replay).
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

	timeoutMs := req.Provider.TimeoutMs
	// Overall envelope bounds the full retry sequence (attempts + backoff +
	// Retry-After). Each connect attempt also nests a per-attempt timeout that
	// bounds that attempt's Do + (on success) SSE body read — same single-shot
	// stream budget as pre-#640, with room for retries in the parent envelope.
	ctx, cancel := context.WithTimeout(ctx, overallSendTimeout(timeoutMs))
	defer cancel()

	resp, err := streamOpenAIConnect(ctx, req, baseURL, body, timeoutMs)
	if err != nil {
		return CompleteResult{}, err
	}
	defer resp.Body.Close()

	return parseOpenAISSE(resp.Body, model, onDelta, onToolDelta)
}

// streamOpenAIConnect establishes a 2xx streaming response, retrying only
// transient pre-byte failures (5xx/429 status before any SSE is handed to the
// consumer). Each attempt gets a fresh per-call timeout under the parent
// overall envelope. On success the caller owns resp.Body and must close it.
func streamOpenAIConnect(ctx context.Context, req CompleteRequest, baseURL string, body []byte, timeoutMs *int) (*http.Response, error) {
	perAttempt := DefaultTimeout
	if timeoutMs != nil && *timeoutMs > 0 {
		perAttempt = time.Duration(*timeoutMs) * time.Millisecond
	}

	var last *AIError
	for attempt := 0; attempt <= len(retryBackoff); attempt++ {
		resp, retryAfter, aiErr := streamOpenAIConnectOnce(ctx, req, baseURL, body, perAttempt)
		if aiErr == nil {
			return resp, nil
		}
		last = aiErr
		if !isTransient(aiErr) {
			return nil, aiErr
		}
		if attempt >= len(retryBackoff) {
			break
		}
		wait := jitterDuration(retryBackoff[attempt])
		if retryAfter > wait {
			wait = retryAfter
		}
		if wait <= 0 {
			if err := ctx.Err(); err != nil {
				return nil, aiErrorFromContext(err)
			}
			continue
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, aiErrorFromContext(ctx.Err())
		case <-timer.C:
		}
	}
	if last == nil {
		last = &AIError{Kind: ErrServer, Message: "stream connect retries exhausted"}
	}
	return nil, last
}

// streamOpenAIConnectOnce performs one connect+status attempt. On non-2xx the
// body is drained/closed and never returned. On 2xx the live body is returned
// for SSE parsing (caller closes).
func streamOpenAIConnectOnce(ctx context.Context, req CompleteRequest, baseURL string, body []byte, perAttempt time.Duration) (*http.Response, time.Duration, *AIError) {
	// Request is bound to attemptCtx (not the parent overall ctx alone): the
	// per-attempt deadline covers connect + the committed SSE body for this
	// attempt. cancelOnCloseBody defers cancel() until Body.Close so we do not
	// kill the stream immediately on Do success; it does not extend the deadline.
	attemptCtx, cancel := context.WithTimeout(ctx, perAttempt)
	httpReq, err := http.NewRequestWithContext(attemptCtx, http.MethodPost, baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		cancel()
		return nil, 0, &AIError{Kind: ErrUnreachable, Message: fmt.Sprintf("build request: %v", err)}
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if req.Provider.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+req.Provider.APIKey)
	}

	resp, err := httpClient.Do(httpReq)
	if err != nil {
		// Capture cause BEFORE cancel() — cancel forces Canceled and would
		// misclassify dial/TLS/refused as "stream canceled" (mirrors sendOnce).
		cerr := attemptCtx.Err()
		parentErr := ctx.Err()
		cancel()
		switch {
		case errors.Is(cerr, context.Canceled) || errors.Is(parentErr, context.Canceled):
			return nil, 0, &AIError{Kind: ErrCanceled, Message: fmt.Sprintf("stream canceled: %v", err)}
		case errors.Is(cerr, context.DeadlineExceeded):
			return nil, 0, &AIError{Kind: ErrTimeout, Message: fmt.Sprintf("request timed out after %s: %v", perAttempt, err)}
		default:
			return nil, 0, &AIError{Kind: ErrUnreachable, Message: err.Error()}
		}
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()
		cancel()
		msg := strings.TrimSpace(string(raw))
		if len(msg) > 500 {
			msg = msg[:500] + "…"
		}
		aiErr := &AIError{Kind: classifyStatus(resp.StatusCode), Status: resp.StatusCode, Message: msg}
		ra := time.Duration(0)
		if isTransient(aiErr) {
			ra = resolveRetryAfter(resp.Header.Get("Retry-After"), raw)
		}
		return nil, ra, aiErr
	}

	// Commit: hand body to SSE parse. Do not cancel() here — that would abort
	// the body mid-stream. cancelOnCloseBody runs cancel when the caller closes.
	resp.Body = &cancelOnCloseBody{ReadCloser: resp.Body, cancel: cancel}
	return resp, 0, nil
}

// cancelOnCloseBody cancels the per-attempt context when the stream body is
// closed so the WithTimeout resources are released after SSE finishes.
type cancelOnCloseBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (b *cancelOnCloseBody) Close() error {
	err := b.ReadCloser.Close()
	if b.cancel != nil {
		b.cancel()
		b.cancel = nil
	}
	return err
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
		content         strings.Builder
		model           = fallbackModel
		usage           *AIUsage
		malformedFrames int
		sawDone         bool
		sawFinishReason bool
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
			sawDone = true
			break
		}
		var chunk streamChatChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			malformedFrames++
			// A single malformed frame is not fatal for chat UX, but a stream
			// that is mostly malformed must not be reported as a success.
			if malformedFrames >= maxMalformedSSEFrames {
				return CompleteResult{}, &AIError{Kind: ErrServer, Message: fmt.Sprintf("stream contains too many malformed SSE frames (at least %d)", maxMalformedSSEFrames)}
			}
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
		if choice.FinishReason != nil && *choice.FinishReason != "" {
			sawFinishReason = true
		}
		// Content delta.
		if choice.Delta.Content != "" {
			if content.Len()+len(choice.Delta.Content) > MaxStreamBytes {
				return CompleteResult{}, &AIError{Kind: ErrServer, Message: fmt.Sprintf("stream content exceeds %d-byte cap", MaxStreamBytes)}
			}
			content.WriteString(choice.Delta.Content)
			if err := onDelta(choice.Delta.Content); err != nil {
				// A consumer cancel (Stop / vault close) returns context.Canceled;
				// classify it as such so the audit/UX shows "canceled", not a
				// generic abort or timeout (#628 cancel contract).
				if errors.Is(err, context.Canceled) {
					return CompleteResult{}, &AIError{Kind: ErrCanceled, Message: fmt.Sprintf("stream canceled: %v", err)}
				}
				return CompleteResult{}, &AIError{Kind: ErrUnknown, Message: fmt.Sprintf("stream consumer aborted: %v", err)}
			}
		}
		// Tool-call fragments: accumulate by index and forward to onToolDelta.
		for _, d := range choice.Delta.ToolCalls {
			a, ok := accum[d.Index]
			if !ok {
				// Bound the number of distinct tool calls so a misbehaving
				// endpoint cannot drive unbounded allocation with many indices
				// (each call's args are already capped by MaxStreamBytes).
				if len(accum) >= MaxStreamToolCalls {
					return CompleteResult{}, &AIError{Kind: ErrServer, Message: fmt.Sprintf("stream exceeded %d distinct tool-call limit", MaxStreamToolCalls)}
				}
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
					if a.args.Len()+len(d.Function.Arguments) > MaxStreamBytes {
						return CompleteResult{}, &AIError{Kind: ErrServer, Message: fmt.Sprintf("stream tool arguments for call %d exceed %d-byte cap", d.Index, MaxStreamBytes)}
					}
					a.args.WriteString(d.Function.Arguments)
					frag.ArgumentsFragment = d.Function.Arguments
				}
			}
			if onToolDelta != nil {
				if err := onToolDelta(frag); err != nil {
					if errors.Is(err, context.Canceled) {
						return CompleteResult{}, &AIError{Kind: ErrCanceled, Message: fmt.Sprintf("stream tool canceled: %v", err)}
					}
					return CompleteResult{}, &AIError{Kind: ErrUnknown, Message: fmt.Sprintf("stream tool consumer aborted: %v", err)}
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		if errors.Is(err, context.Canceled) {
			return CompleteResult{}, &AIError{Kind: ErrCanceled, Message: fmt.Sprintf("stream canceled: %v", err)}
		}
		return CompleteResult{}, &AIError{Kind: ErrUnknown, Message: fmt.Sprintf("read stream: %v", err)}
	}
	if !sawDone && !sawFinishReason {
		// Several providers legitimately close an SSE response without [DONE].
		// Keep that tolerant behavior when the ending is ambiguous, but a partially
		// assembled tool argument is clear evidence that the response was truncated.
		for _, idx := range accumOrder {
			a := accum[idx]
			if a.args.Len() > 0 && !json.Valid([]byte(a.args.String())) {
				return CompleteResult{}, &AIError{Kind: ErrServer, Message: fmt.Sprintf("stream ended before tool arguments for call %d were complete", idx)}
			}
		}
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
