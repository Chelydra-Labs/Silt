package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// sseChatServer emits a few OpenAI-compatible SSE chunks then [DONE].
func sseChatServer(t *testing.T, chunks []string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path = %q", r.URL.Path)
		}
		// Confirm stream flag was requested.
		body := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)
		if !strings.Contains(string(body), `"stream":true`) {
			t.Errorf("expected stream:true in body, got %s", body)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		for _, c := range chunks {
			fmt.Fprintf(w, "data: %s\n\n", c)
			if flusher != nil {
				flusher.Flush()
			}
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		if flusher != nil {
			flusher.Flush()
		}
	}))
}

func TestCompleteStream_AggregatesDeltas(t *testing.T) {
	chunks := []string{
		`{"model":"m","choices":[{"delta":{"content":"Hel"}}]}`,
		`{"model":"m","choices":[{"delta":{"content":"lo"}}]}`,
		`{"model":"m","choices":[{"delta":{"content":"!"}}],"usage":{"prompt_tokens":1,"completion_tokens":3,"total_tokens":4}}`,
	}
	srv := sseChatServer(t, chunks)
	defer srv.Close()

	var got []string
	res, err := CompleteStream(context.Background(), CompleteRequest{
		Provider: AIProvider{BaseURL: srv.URL, Model: "m", ProviderType: ProviderLocal},
		Messages: []ChatMessage{{Role: "user", Content: "hi"}},
	}, func(delta string) error {
		got = append(got, delta)
		return nil
	}, nil)
	if err != nil {
		t.Fatalf("CompleteStream: %v", err)
	}
	if res.Content != "Hello!" {
		t.Errorf("content = %q, want Hello!", res.Content)
	}
	if res.Model != "m" {
		t.Errorf("model = %q", res.Model)
	}
	if res.Usage == nil || res.Usage.TotalTokens == nil || *res.Usage.TotalTokens != 4 {
		t.Errorf("usage = %+v", res.Usage)
	}
	if strings.Join(got, "") != "Hello!" {
		t.Errorf("deltas = %v", got)
	}
}

func TestCompleteStream_CancelMidStream(t *testing.T) {
	// Slow stream: hang after first delta so cancel can win.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"x"}}]}`+"\n\n")
		if flusher != nil {
			flusher.Flush()
		}
		// Block until client disconnects.
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	var deltas atomic.Int32
	errCh := make(chan error, 1)
	go func() {
		_, err := CompleteStream(ctx, CompleteRequest{
			Provider: AIProvider{BaseURL: srv.URL, Model: "m", ProviderType: ProviderOpenAICompatible},
			Messages: []ChatMessage{{Role: "user", Content: "hi"}},
		}, func(delta string) error {
			deltas.Add(1)
			cancel() // cancel after first delta
			return nil
		}, nil)
		errCh <- err
	}()

	select {
	case err := <-errCh:
		// Cancelled streams surface as an error (timeout/cancel/read).
		if err == nil {
			t.Fatal("expected error on cancelled stream")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("stream did not return after cancel")
	}
	if deltas.Load() < 1 {
		t.Error("expected at least one delta before cancel")
	}
}

func TestCompleteStream_NativeProviderBufferedFallback(t *testing.T) {
	// Native providers have no SSE streaming; CompleteStream must fall back to
	// a buffered non-stream Complete and emit the content as one delta.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("anthropic path = %q, want /v1/messages", r.URL.Path)
		}
		resp := map[string]any{
			"id":   "msg_01",
			"type": "message",
			"role": "assistant",
			"content": []map[string]any{
				{"type": "text", "text": "native reply"},
			},
			"model":       "claude-sonnet-5",
			"stop_reason": "end_turn",
			"usage":       map[string]any{"input_tokens": 5, "output_tokens": 3},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	var got []string
	res, err := CompleteStream(context.Background(), CompleteRequest{
		Provider: AIProvider{ProviderType: ProviderAnthropic, BaseURL: srv.URL, APIKey: "k", Model: "claude-sonnet-5"},
		Messages: []ChatMessage{{Role: "user", Content: "hi"}},
	}, func(delta string) error {
		got = append(got, delta)
		return nil
	}, nil)
	if err != nil {
		t.Fatalf("CompleteStream: %v", err)
	}
	if res.Content != "native reply" {
		t.Errorf("content = %q, want native reply", res.Content)
	}
	if len(got) != 1 || got[0] != "native reply" {
		t.Errorf("deltas = %v, want [native reply]", got)
	}
}

func TestComplete_NonStreamStillWorks(t *testing.T) {
	// Regression: buffered path must ignore Stream flag and return full body.
	srv := echoChatServer(t, false, "")
	defer srv.Close()
	res, err := Complete(context.Background(), CompleteRequest{
		Provider: AIProvider{BaseURL: srv.URL, Model: "test-model"},
		Messages: []ChatMessage{{Role: "user", Content: "hi"}},
		Stream:   true, // must not break buffered Complete
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if res.Content != "hello back" {
		t.Errorf("content = %q", res.Content)
	}
}

func TestParseOpenAISSE_SkipsMalformed(t *testing.T) {
	body := strings.NewReader(strings.Join([]string{
		"data: not-json",
		`data: {"choices":[{"delta":{"content":"ok"}}]}`,
		"data: [DONE]",
		"",
	}, "\n"))
	res, err := parseOpenAISSE(body, "fallback", func(string) error { return nil }, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Content != "ok" {
		t.Errorf("content = %q", res.Content)
	}
}

// TestCompleteStream_AccumulatesToolCallDeltas verifies streamed tool_call
// fragments (OpenAI splits one call across chunks: first carries id+name, later
// chunks append to the JSON arguments string) are reassembled onto the final
// result AND forwarded via the onToolDelta callback.
func TestCompleteStream_AccumulatesToolCallDeltas(t *testing.T) {
	chunks := []string{
		`{"model":"m","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"search_notes","arguments":"{\"q\":"}}]}}]}`,
		`{"model":"m","choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"meetings\"}"}}]}}]}`,
		`{"model":"m","choices":[{"delta":{"content":"done"}}]}`,
	}
	srv := sseChatServer(t, chunks)
	defer srv.Close()

	var toolFrags []ToolCallDelta
	res, err := CompleteStream(context.Background(), CompleteRequest{
		Provider: AIProvider{BaseURL: srv.URL, Model: "m", ProviderType: ProviderLocal},
		Messages: []ChatMessage{{Role: "user", Content: "hi"}},
		Tools:    []ToolDef{{Name: "search_notes", Parameters: dummySchema}},
	}, func(string) error { return nil }, func(d ToolCallDelta) error {
		toolFrags = append(toolFrags, d)
		return nil
	})
	if err != nil {
		t.Fatalf("CompleteStream: %v", err)
	}
	if res.Content != "done" {
		t.Errorf("content = %q, want done", res.Content)
	}
	if len(res.ToolCalls) != 1 {
		t.Fatalf("tool_calls = %d, want 1", len(res.ToolCalls))
	}
	tc := res.ToolCalls[0]
	if tc.ID != "call_1" || tc.Name != "search_notes" {
		t.Errorf("reassembled tool_call = %+v", tc)
	}
	// Concatenated arguments string unwrapped to a raw JSON object.
	var args map[string]any
	if err := json.Unmarshal(tc.Arguments, &args); err != nil {
		t.Fatalf("arguments not object: %v (raw=%s)", err, tc.Arguments)
	}
	if args["q"] != "meetings" {
		t.Errorf("args.q = %v, want meetings", args["q"])
	}
	// Each fragment forwarded (at least one with id+name, one with arguments).
	if len(toolFrags) < 2 {
		t.Fatalf("tool deltas = %d, want >= 2", len(toolFrags))
	}
	if toolFrags[0].ID != "call_1" || toolFrags[0].Name != "search_notes" {
		t.Errorf("first delta = %+v, want id+name", toolFrags[0])
	}
}

func TestParseOpenAISSE_RejectsOversizedToolArguments(t *testing.T) {
	var body bytes.Buffer
	fragment := strings.Repeat("x", 512*1024)
	for total := 0; total <= MaxStreamBytes; total += len(fragment) {
		payload, err := json.Marshal(map[string]any{
			"choices": []map[string]any{{"delta": map[string]any{
				"tool_calls": []map[string]any{{"index": 0, "function": map[string]any{"arguments": fragment}}},
			}}},
		})
		if err != nil {
			t.Fatalf("marshal chunk: %v", err)
		}
		fmt.Fprintf(&body, "data: %s\n\n", payload)
	}

	_, err := parseOpenAISSE(&body, "m", func(string) error { return nil }, nil)
	if err == nil {
		t.Fatal("expected oversized tool arguments to fail")
	}
	ae, ok := err.(*AIError)
	if !ok || ae.Kind != ErrServer {
		t.Fatalf("want ErrServer, got %v", err)
	}
	if !strings.Contains(ae.Message, "tool arguments") {
		t.Errorf("error = %q, want tool-argument cap message", ae.Message)
	}
}

func TestParseOpenAISSE_RejectsTooManyMalformedFrames(t *testing.T) {
	body := strings.NewReader(strings.Join([]string{
		"data: not-json",
		"data: still-not-json",
		"data: definitely-not-json",
		"data: [DONE]",
		"",
	}, "\n"))
	_, err := parseOpenAISSE(body, "m", func(string) error { return nil }, nil)
	if err == nil {
		t.Fatal("expected too many malformed SSE frames to fail")
	}
	ae, ok := err.(*AIError)
	if !ok || ae.Kind != ErrServer {
		t.Fatalf("want ErrServer, got %v", err)
	}
}

func TestParseOpenAISSE_MissingDoneIsToleratedWhenProviderFinished(t *testing.T) {
	body := strings.NewReader(`data: {"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}` + "\n")
	res, err := parseOpenAISSE(body, "m", func(string) error { return nil }, nil)
	if err != nil {
		t.Fatalf("parseOpenAISSE: %v", err)
	}
	if res.Content != "ok" {
		t.Errorf("content = %q, want ok", res.Content)
	}
}

func TestParseOpenAISSE_IncompleteToolArgumentsWithoutDoneFail(t *testing.T) {
	body := strings.NewReader(`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"q\":"}}]}}]}` + "\n")
	_, err := parseOpenAISSE(body, "m", func(string) error { return nil }, nil)
	if err == nil {
		t.Fatal("expected incomplete tool arguments to fail")
	}
	ae, ok := err.(*AIError)
	if !ok || ae.Kind != ErrServer {
		t.Fatalf("want ErrServer, got %v", err)
	}
}

// TestCompleteStream_CancelBeforeConnect_IsCanceled: a context already
// canceled when the call starts must surface ErrCanceled (the Stop UX),
// never ErrTimeout — the buffered path already gets this right (#628).
func TestCompleteStream_CancelBeforeConnect_IsCanceled(t *testing.T) {
	srv := sseChatServer(t, []string{`{"choices":[{"delta":{"content":"x"}}]}`})
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // canceled before the upstream request is made
	_, err := CompleteStream(ctx, CompleteRequest{
		Provider: AIProvider{BaseURL: srv.URL, Model: "m", ProviderType: ProviderOpenAICompatible},
		Messages: []ChatMessage{{Role: "user", Content: "hi"}},
	}, func(string) error { return nil }, nil)
	e, ok := err.(*AIError)
	if !ok {
		t.Fatalf("want *AIError, got %T (%v)", err, err)
	}
	if e.Kind != ErrCanceled {
		t.Errorf("kind = %q, want canceled", e.Kind)
	}
}

// TestCompleteStream_ConsumerCancel_IsCanceled: when the delta consumer
// aborts with context.Canceled (the app_ai backpressure path when Stop is
// pressed mid-answer), the stream must classify as ErrCanceled, not a generic
// abort or timeout (#628 cancel contract on the streaming path).
func TestCompleteStream_ConsumerCancel_IsCanceled(t *testing.T) {
	srv := sseChatServer(t, []string{`{"choices":[{"delta":{"content":"x"}}]}`})
	defer srv.Close()

	_, err := CompleteStream(context.Background(), CompleteRequest{
		Provider: AIProvider{BaseURL: srv.URL, Model: "m", ProviderType: ProviderOpenAICompatible},
		Messages: []ChatMessage{{Role: "user", Content: "hi"}},
	}, func(string) error {
		return context.Canceled
	}, nil)
	e, ok := err.(*AIError)
	if !ok {
		t.Fatalf("want *AIError, got %T (%v)", err, err)
	}
	if e.Kind != ErrCanceled {
		t.Errorf("kind = %q, want canceled", e.Kind)
	}
}

// TestCompleteStream_ToolConsumerCancel_IsCanceled: the tool-delta consumer
// path mirrors the content-delta path.
func TestCompleteStream_ToolConsumerCancel_IsCanceled(t *testing.T) {
	srv := sseChatServer(t, []string{
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","function":{"name":"search_notes","arguments":"{}"}}]}}]}`,
	})
	defer srv.Close()

	_, err := CompleteStream(context.Background(), CompleteRequest{
		Provider: AIProvider{BaseURL: srv.URL, Model: "m", ProviderType: ProviderOpenAICompatible},
		Messages: []ChatMessage{{Role: "user", Content: "hi"}},
	}, func(string) error { return nil }, func(ToolCallDelta) error {
		return context.Canceled
	})
	e, ok := err.(*AIError)
	if !ok {
		t.Fatalf("want *AIError, got %T (%v)", err, err)
	}
	if e.Kind != ErrCanceled {
		t.Errorf("kind = %q, want canceled", e.Kind)
	}
}
