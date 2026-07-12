package ai

import (
	"context"
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
	})
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
		})
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

func TestCompleteStream_NativeProviderRejected(t *testing.T) {
	_, err := CompleteStream(context.Background(), CompleteRequest{
		Provider: AIProvider{BaseURL: "https://generativelanguage.googleapis.com", Model: "g", ProviderType: ProviderGoogle},
		Messages: []ChatMessage{{Role: "user", Content: "hi"}},
	}, func(string) error { return nil })
	if err == nil {
		t.Fatal("expected error for native google streaming")
	}
	ae, ok := err.(*AIError)
	if !ok || ae.Kind != ErrBadRequest {
		t.Fatalf("want ErrBadRequest, got %v", err)
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
	res, err := parseOpenAISSE(body, "fallback", func(string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if res.Content != "ok" {
		t.Errorf("content = %q", res.Content)
	}
}
