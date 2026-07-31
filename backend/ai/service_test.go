package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// echoChatServer returns an httptest server that asserts the OpenAI-compatible
// chat request shape and replies with a canned completion. If withAuth is true,
// it requires an Authorization: Bearer header (rejecting 401 otherwise).
func echoChatServer(t *testing.T, withAuth bool, key string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("chat path = %q, want /v1/chat/completions", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("chat method = %q, want POST", r.Method)
		}
		if withAuth && r.Header.Get("Authorization") != "Bearer "+key {
			http.Error(w, "missing/invalid auth", http.StatusUnauthorized)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var req chatRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("parse chat request: %v", err)
		}
		resp := map[string]any{
			"model": req.Model,
			"choices": []map[string]any{
				{"message": map[string]any{"content": "hello back"}, "finish_reason": "stop"},
			},
			"usage": map[string]any{"prompt_tokens": 5, "completion_tokens": 2, "total_tokens": 7},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

// echoEmbedServer returns an httptest server replying with canned embeddings,
// one per input text, each of the given dimension.
func echoEmbedServer(t *testing.T, dim int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" {
			t.Errorf("embed path = %q, want /v1/embeddings", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		var req embedRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("parse embed request: %v", err)
		}
		data := make([]map[string]any, len(req.Input))
		for i := range req.Input {
			vec := make([]float64, dim)
			for j := range vec {
				vec[j] = float64(i + j)
			}
			data[i] = map[string]any{"embedding": vec, "index": i}
		}
		resp := map[string]any{"model": req.Model, "data": data, "usage": map[string]any{"prompt_tokens": 3, "total_tokens": 3}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

func TestComplete_Success(t *testing.T) {
	srv := echoChatServer(t, false, "")
	defer srv.Close()
	res, err := Complete(context.Background(), CompleteRequest{
		Provider: AIProvider{BaseURL: srv.URL, Model: "test-model"},
		Messages: []ChatMessage{{Role: "user", Content: "ping"}},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if res.Content != "hello back" {
		t.Errorf("content = %q, want %q", res.Content, "hello back")
	}
	if res.Model != "test-model" {
		t.Errorf("model = %q, want test-model", res.Model)
	}
	if res.Usage == nil || res.Usage.TotalTokens == nil || *res.Usage.TotalTokens != 7 {
		t.Errorf("usage total not propagated, got %+v", res.Usage)
	}
}

func TestComplete_SendsBearerWhenKeyed(t *testing.T) {
	srv := echoChatServer(t, true, "secret-key")
	defer srv.Close()
	_, err := Complete(context.Background(), CompleteRequest{
		Provider: AIProvider{BaseURL: srv.URL, APIKey: "secret-key", Model: "m"},
		Messages: []ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Complete with key: %v", err)
	}
}

func TestComplete_UnauthorizedIsTyped(t *testing.T) {
	srv := echoChatServer(t, true, "expected")
	defer srv.Close()
	_, err := Complete(context.Background(), CompleteRequest{
		Provider: AIProvider{BaseURL: srv.URL, APIKey: "wrong-key", Model: "m"},
		Messages: []ChatMessage{{Role: "user", Content: "hi"}},
	})
	e, ok := err.(*AIError)
	if !ok {
		t.Fatalf("want *AIError, got %T (%v)", err, err)
	}
	if e.Kind != ErrUnauthorized {
		t.Errorf("kind = %q, want %q", e.Kind, ErrUnauthorized)
	}
	if e.Status != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", e.Status)
	}
}

func TestComplete_StatusClassification(t *testing.T) {
	// Transient statuses (429/500/502) now retry, so shrink the backoff or
	// this table test pays ~2s per retryable case.
	withFastRetry(t)
	cases := []struct {
		status int
		want   AIErrorKind
	}{
		{http.StatusForbidden, ErrForbidden},
		{http.StatusTooManyRequests, ErrRateLimited},
		{http.StatusNotFound, ErrModelMissing},
		{http.StatusBadRequest, ErrBadRequest},
		{http.StatusInternalServerError, ErrServer},
		{502, ErrServer},
	}
	for _, c := range cases {
		t.Run(c.want.String(), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "boom", c.status)
			}))
			defer srv.Close()
			_, err := Complete(context.Background(), CompleteRequest{
				Provider: AIProvider{BaseURL: srv.URL, Model: "m"},
				Messages: []ChatMessage{{Role: "user", Content: "x"}},
			})
			e, ok := err.(*AIError)
			if !ok {
				t.Fatalf("want *AIError, got %T", err)
			}
			if e.Kind != c.want {
				t.Errorf("status %d: kind = %q, want %q", c.status, e.Kind, c.want)
			}
			if e.Status != c.status {
				t.Errorf("status = %d, want %d", e.Status, c.status)
			}
		})
	}
}

func TestComplete_UnreachableIsTyped(t *testing.T) {
	// Closed port on loopback: connection refused → ErrUnreachable (not timeout).
	_, err := Complete(context.Background(), CompleteRequest{
		Provider: AIProvider{BaseURL: "http://127.0.0.1:1", Model: "m", TimeoutMs: intPtr(500)},
		Messages: []ChatMessage{{Role: "user", Content: "x"}},
	})
	e, ok := err.(*AIError)
	if !ok {
		t.Fatalf("want *AIError, got %T (%v)", err, err)
	}
	if e.Kind != ErrUnreachable {
		t.Errorf("kind = %q, want %q", e.Kind, ErrUnreachable)
	}
}

func TestComplete_TimeoutIsTyped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
	}))
	defer srv.Close()
	_, err := Complete(context.Background(), CompleteRequest{
		Provider: AIProvider{BaseURL: srv.URL, Model: "m", TimeoutMs: intPtr(50)},
		Messages: []ChatMessage{{Role: "user", Content: "x"}},
	})
	e, ok := err.(*AIError)
	if !ok {
		t.Fatalf("want *AIError, got %T (%v)", err, err)
	}
	if e.Kind != ErrTimeout {
		t.Errorf("kind = %q, want %q", e.Kind, ErrTimeout)
	}
}

func TestComplete_Validation(t *testing.T) {
	// No messages.
	_, err := Complete(context.Background(), CompleteRequest{
		Provider: AIProvider{BaseURL: "http://x", Model: "m"},
	})
	if e, ok := err.(*AIError); !ok || e.Kind != ErrBadRequest {
		t.Errorf("empty messages: want ErrBadRequest, got %v", err)
	}
	// No model and no provider model.
	_, err = Complete(context.Background(), CompleteRequest{
		Provider: AIProvider{BaseURL: "http://x"},
		Messages: []ChatMessage{{Role: "user", Content: "x"}},
	})
	if e, ok := err.(*AIError); !ok || e.Kind != ErrBadRequest {
		t.Errorf("no model: want ErrBadRequest, got %v", err)
	}
	// No base URL.
	_, err = Complete(context.Background(), CompleteRequest{
		Provider: AIProvider{Model: "m"},
		Messages: []ChatMessage{{Role: "user", Content: "x"}},
	})
	if e, ok := err.(*AIError); !ok || e.Kind != ErrUnreachable {
		t.Errorf("no base URL: want ErrUnreachable, got %v", err)
	}
}

func TestComplete_RequestTooLarge(t *testing.T) {
	srv := echoChatServer(t, false, "")
	defer srv.Close()
	huge := strings.Repeat("x", MaxRequestBytes+1)
	_, err := Complete(context.Background(), CompleteRequest{
		Provider: AIProvider{BaseURL: srv.URL, Model: "m"},
		Messages: []ChatMessage{{Role: "user", Content: huge}},
	})
	if e, ok := err.(*AIError); !ok || e.Kind != ErrBadRequest {
		t.Errorf("oversized request: want ErrBadRequest, got %v", err)
	}
}

func TestEmbed_SuccessBatch(t *testing.T) {
	srv := echoEmbedServer(t, 8)
	defer srv.Close()
	res, err := Embed(context.Background(), EmbedRequest{
		Provider: AIProvider{BaseURL: srv.URL, Model: "emb"},
		Texts:    []string{"one", "two", "three"},
	})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(res.Embeddings) != 3 {
		t.Fatalf("got %d embeddings, want 3", len(res.Embeddings))
	}
	if res.Dimensions != 8 {
		t.Errorf("dimensions = %d, want 8", res.Dimensions)
	}
	if len(res.Embeddings[0]) != 8 {
		t.Errorf("vec0 len = %d, want 8", len(res.Embeddings[0]))
	}
	if res.Usage == nil || res.Usage.TotalTokens == nil {
		t.Errorf("embed usage not propagated")
	}
}

func TestEmbed_Validation(t *testing.T) {
	_, err := Embed(context.Background(), EmbedRequest{
		Provider: AIProvider{BaseURL: "http://x", Model: "m"},
	})
	if e, ok := err.(*AIError); !ok || e.Kind != ErrBadRequest {
		t.Errorf("empty texts: want ErrBadRequest, got %v", err)
	}
}

func TestProbe_ChatSuccessEmbedSuccess(t *testing.T) {
	chatSrv := echoChatServer(t, false, "")
	defer chatSrv.Close()
	if err := Probe(context.Background(), AIProvider{BaseURL: chatSrv.URL, Model: "m"}, true); err != nil {
		t.Errorf("Probe chat: %v", err)
	}
	embSrv := echoEmbedServer(t, 4)
	defer embSrv.Close()
	if err := Probe(context.Background(), AIProvider{BaseURL: embSrv.URL, Model: "m"}, false); err != nil {
		t.Errorf("Probe embed: %v", err)
	}
}

func TestProbe_PropagatesTypedError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no such model", http.StatusNotFound)
	}))
	defer srv.Close()
	err := Probe(context.Background(), AIProvider{BaseURL: srv.URL, Model: "missing"}, true)
	e, ok := err.(*AIError)
	if !ok {
		t.Fatalf("want *AIError, got %T", err)
	}
	if e.Kind != ErrModelMissing {
		t.Errorf("kind = %q, want %q", e.Kind, ErrModelMissing)
	}
}

// String lets AIErrorKind be used as a subtest name.
func (k AIErrorKind) String() string { return string(k) }

// intPtr local helper (the config-package helper isn't visible here).
func intPtr(i int) *int { return &i }

func TestComplete_RejectsNonHTTPBaseURL(t *testing.T) {
	_, err := Complete(context.Background(), CompleteRequest{
		Provider: AIProvider{BaseURL: "file:///etc/passwd", Model: "m"},
		Messages: []ChatMessage{{Role: "user", Content: "hi"}},
	})
	e, ok := err.(*AIError)
	if !ok || e.Kind != ErrBadRequest {
		t.Errorf("want ErrBadRequest for file:// URL, got %v", err)
	}
}

func TestEmbed_RejectsNonHTTPBaseURL(t *testing.T) {
	_, err := Embed(context.Background(), EmbedRequest{
		Provider: AIProvider{BaseURL: "ftp://evil.example.com", Model: "m"},
		Texts:    []string{"hi"},
	})
	e, ok := err.(*AIError)
	if !ok || e.Kind != ErrBadRequest {
		t.Errorf("want ErrBadRequest for ftp:// URL, got %v", err)
	}
}

func TestComplete_CrossHostRedirectRejected(t *testing.T) {
	// The first server redirects to a second server on a different host:port.
	// The redirect guard must block it so the Authorization header (if any)
	// is not sent to the redirect target.
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("redirect target should never be reached")
	}))
	defer target.Close()

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer redirector.Close()

	_, err := Complete(context.Background(), CompleteRequest{
		Provider: AIProvider{BaseURL: redirector.URL, Model: "m", APIKey: "secret"},
		Messages: []ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected cross-host redirect to be rejected, got nil")
	}
}

func TestRedirectGuard_RejectsSchemeDowngradeAndCrossHost(t *testing.T) {
	// Drive CheckRedirect directly with constructed requests so the policy is
	// locked independently of any live HTTP hop. Each case's "prev" is the last
	// entry in `via`; `next` is the candidate redirect target.
	cases := []struct {
		name    string
		prev    *url.URL
		next    *url.URL
		wantErr bool
	}{
		{
			name:    "same-host same-scheme https allowed",
			prev:    &url.URL{Scheme: "https", Host: "a.example"},
			next:    &url.URL{Scheme: "https", Host: "a.example"},
			wantErr: false,
		},
		{
			name:    "cross-host rejected",
			prev:    &url.URL{Scheme: "https", Host: "a.example"},
			next:    &url.URL{Scheme: "https", Host: "b.example"},
			wantErr: true,
		},
		{
			name:    "https-to-http same-host rejected",
			prev:    &url.URL{Scheme: "https", Host: "a.example"},
			next:    &url.URL{Scheme: "http", Host: "a.example"},
			wantErr: true,
		},
		{
			name:    "http-to-https same-host allowed",
			prev:    &url.URL{Scheme: "http", Host: "a.example"},
			next:    &url.URL{Scheme: "https", Host: "a.example"},
			wantErr: false,
		},
		{
			name:    "http-to-http same-host allowed",
			prev:    &url.URL{Scheme: "http", Host: "a.example"},
			next:    &url.URL{Scheme: "http", Host: "a.example"},
			wantErr: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := &http.Request{URL: c.next}
			via := []*http.Request{{URL: c.prev}}
			err := httpClient.CheckRedirect(req, via)
			if c.wantErr && err == nil {
				t.Fatalf("expected rejection, got nil")
			}
			if !c.wantErr && err != nil {
				t.Fatalf("expected allow, got error: %v", err)
			}
		})
	}
}

func TestEmbed_CountMismatch(t *testing.T) {
	// Provider returns a single embedding for a three-text batch — the kind of
	// partial response that would silently misalign vectors with inputs if the
	// service did not check the count.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req embedRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("parse embed request: %v", err)
		}
		// Respond with exactly one entry regardless of input length.
		vec := []float64{0.1, 0.2, 0.3}
		resp := map[string]any{
			"model": req.Model,
			"data":  []map[string]any{{"embedding": vec, "index": 0}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	_, err := Embed(context.Background(), EmbedRequest{
		Provider: AIProvider{BaseURL: srv.URL, Model: "emb"},
		Texts:    []string{"one", "two", "three"},
	})
	e, ok := err.(*AIError)
	if !ok {
		t.Fatalf("want *AIError, got %T (%v)", err, err)
	}
	if e.Kind != ErrUnknown {
		t.Errorf("kind = %q, want %q", e.Kind, ErrUnknown)
	}
}

func TestProbe_ChatOmitsMaxTokens(t *testing.T) {
	// Lock the Fix E behavior: the chat probe must not clamp max_tokens, since
	// some reasoning endpoints reject a tiny cap. The handler asserts the field
	// is absent and replies with a valid completion.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req chatRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("parse chat request: %v", err)
		}
		if req.MaxTokens != nil {
			t.Errorf("probe must not set max_tokens, got %d", *req.MaxTokens)
		}
		resp := map[string]any{
			"model": req.Model,
			"choices": []map[string]any{
				{"message": map[string]any{"content": "pong"}, "finish_reason": "stop"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	if err := Probe(context.Background(), AIProvider{BaseURL: srv.URL, Model: "m"}, true); err != nil {
		t.Errorf("Probe chat: %v", err)
	}
}

// withFastRetry shrinks the retry backoff to zero for the duration of the test
// so retry-exercising cases don't pay real wall-clock waits. Restored on
// cleanup so it can't leak into other tests.
func withFastRetry(t *testing.T) {
	t.Helper()
	saved := retryBackoff
	retryBackoff = []time.Duration{0, 0}
	t.Cleanup(func() { retryBackoff = saved })
}

func TestComplete_RetriesTransientThenSucceeds(t *testing.T) {
	// A transient 5xx on the first attempt must be retried; the second attempt
	// succeeds. The provider's OpenAI-compat shim (Google's) intermittently
	// returns INTERNAL 500s, which is exactly the case this absorbs.
	withFastRetry(t)
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) == 1 {
			http.Error(w, "transient", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model": "m",
			"choices": []map[string]any{
				{"message": map[string]any{"content": "ok"}, "finish_reason": "stop"},
			},
		})
	}))
	defer srv.Close()

	res, err := Complete(context.Background(), CompleteRequest{
		Provider: AIProvider{BaseURL: srv.URL, Model: "m"},
		Messages: []ChatMessage{{Role: "user", Content: "x"}},
	})
	if err != nil {
		t.Fatalf("Complete after retry: %v", err)
	}
	if res.Content != "ok" {
		t.Errorf("content = %q, want ok", res.Content)
	}
	if got := hits.Load(); got != 2 {
		t.Errorf("hits = %d, want 2 (1 failed + 1 succeeded)", got)
	}
}

func TestComplete_RetriesTransientExhausted(t *testing.T) {
	// A persistent 5xx must exhaust the retry budget and surface the typed
	// server error (not a bare transport string), having tried the initial
	// attempt plus one per backoff entry.
	withFastRetry(t)
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := Complete(context.Background(), CompleteRequest{
		Provider: AIProvider{BaseURL: srv.URL, Model: "m"},
		Messages: []ChatMessage{{Role: "user", Content: "x"}},
	})
	e, ok := err.(*AIError)
	if !ok {
		t.Fatalf("want *AIError, got %T (%v)", err, err)
	}
	if e.Kind != ErrServer {
		t.Errorf("kind = %q, want server", e.Kind)
	}
	if e.Status != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", e.Status)
	}
	wantHits := int32(1 + len(retryBackoff)) // initial + one retry per backoff entry
	if got := hits.Load(); got != wantHits {
		t.Errorf("hits = %d, want %d (initial + %d retries)", got, wantHits, len(retryBackoff))
	}
}

func TestComplete_DoesNotRetryClientError(t *testing.T) {
	// 4xx is deterministic — retrying an identical request must not happen.
	withFastRetry(t)
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.Error(w, "bad", http.StatusBadRequest)
	}))
	defer srv.Close()

	_, err := Complete(context.Background(), CompleteRequest{
		Provider: AIProvider{BaseURL: srv.URL, Model: "m"},
		Messages: []ChatMessage{{Role: "user", Content: "x"}},
	})
	e, ok := err.(*AIError)
	if !ok {
		t.Fatalf("want *AIError, got %T (%v)", err, err)
	}
	if e.Kind != ErrBadRequest {
		t.Errorf("kind = %q, want bad-request", e.Kind)
	}
	if got := hits.Load(); got != 1 {
		t.Errorf("hits = %d, want 1 (4xx must not retry)", got)
	}
}

func TestComplete_RetryAfterHonored(t *testing.T) {
	// A 429 with Retry-After must wait at least that long (capped) before the
	// next attempt. Use a single non-zero backoff slot so the schedule is
	// Retry-After-dominated rather than the default 500ms/1.5s ladder.
	saved := retryBackoff
	retryBackoff = []time.Duration{10 * time.Millisecond}
	t.Cleanup(func() { retryBackoff = saved })

	var hits atomic.Int32
	var firstAt atomic.Int64
	var secondAt atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := hits.Add(1)
		if n == 1 {
			firstAt.Store(time.Now().UnixNano())
			w.Header().Set("Retry-After", "1")
			http.Error(w, "slow down", http.StatusTooManyRequests)
			return
		}
		secondAt.Store(time.Now().UnixNano())
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model": "m",
			"choices": []map[string]any{
				{"message": map[string]any{"content": "ok"}, "finish_reason": "stop"},
			},
		})
	}))
	defer srv.Close()

	start := time.Now()
	res, err := Complete(context.Background(), CompleteRequest{
		Provider: AIProvider{BaseURL: srv.URL, Model: "m"},
		Messages: []ChatMessage{{Role: "user", Content: "x"}},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if res.Content != "ok" {
		t.Errorf("content = %q, want ok", res.Content)
	}
	// Retry-After=1s is a floor (not shortened by jitter). Allow small slack.
	elapsed := time.Since(start)
	if elapsed < 950*time.Millisecond {
		t.Errorf("elapsed %v, want >= ~1s (Retry-After floor)", elapsed)
	}
	if hits.Load() != 2 {
		t.Errorf("hits = %d, want 2", hits.Load())
	}
	if firstAt.Load() == 0 || secondAt.Load() == 0 {
		t.Fatal("missing attempt timestamps")
	}
	gap := time.Duration(secondAt.Load() - firstAt.Load())
	if gap < 950*time.Millisecond {
		t.Errorf("inter-attempt gap %v, want >= ~1s", gap)
	}
}

func TestComplete_CancelDuringBackoff_IsCanceled(t *testing.T) {
	// Cancel during a long backoff must surface ErrCanceled, not timeout.
	saved := retryBackoff
	retryBackoff = []time.Duration{5 * time.Second}
	t.Cleanup(func() { retryBackoff = saved })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_, err := Complete(ctx, CompleteRequest{
		Provider: AIProvider{BaseURL: srv.URL, Model: "m"},
		Messages: []ChatMessage{{Role: "user", Content: "x"}},
	})
	e, ok := err.(*AIError)
	if !ok {
		t.Fatalf("want *AIError, got %T (%v)", err, err)
	}
	if e.Kind != ErrCanceled {
		t.Errorf("kind = %q, want canceled", e.Kind)
	}
}

func TestParseRetryAfter(t *testing.T) {
	if d := parseRetryAfter("2"); d != 2*time.Second {
		t.Errorf("seconds: got %v", d)
	}
	if d := parseRetryAfter("999"); d != maxRetryAfter {
		t.Errorf("cap: got %v, want %v", d, maxRetryAfter)
	}
	if d := parseRetryAfter(""); d != 0 {
		t.Errorf("empty: got %v", d)
	}
	// HTTP-date a few seconds in the future.
	future := time.Now().Add(3 * time.Second).UTC().Format(http.TimeFormat)
	if d := parseRetryAfter(future); d < 1*time.Second || d > maxRetryAfter {
		t.Errorf("http-date: got %v", d)
	}
}

func TestParseRetryDelayBody_GoogleRetryInfo(t *testing.T) {
	body := []byte(`{
		"error": {
			"code": 429,
			"message": "You exceeded your current quota. Please retry in 53.016342224s.",
			"status": "RESOURCE_EXHAUSTED",
			"details": [
				{
					"@type": "type.googleapis.com/google.rpc.RetryInfo",
					"retryDelay": "53s"
				}
			]
		}
	}`)
	if d := parseRetryDelayBody(body); d != maxRetryAfter {
		t.Errorf("53s should cap at maxRetryAfter: got %v", d)
	}
	bodyShort := []byte(`{
		"error": {
			"message": "rate limited",
			"status": "RESOURCE_EXHAUSTED",
			"details": [{"@type": "type.googleapis.com/google.rpc.RetryInfo", "retryDelay": "2.5s"}]
		}
	}`)
	if d := parseRetryDelayBody(bodyShort); d != 2500*time.Millisecond {
		t.Errorf("2.5s: got %v", d)
	}
	if d := parseRetryDelayBody([]byte(`{"error":{}}`)); d != 0 {
		t.Errorf("empty details: got %v", d)
	}
}

func TestResolveRetryAfter_PrefersLarger(t *testing.T) {
	body := []byte(`{"error":{"details":[{"@type":"type.googleapis.com/google.rpc.RetryInfo","retryDelay":"5s"}]}}`)
	if d := resolveRetryAfter("2", body); d != 5*time.Second {
		t.Errorf("body wins: got %v", d)
	}
	if d := resolveRetryAfter("10", body); d != 10*time.Second {
		t.Errorf("header wins: got %v", d)
	}
}

func TestJitterDuration_ZeroStaysZero(t *testing.T) {
	if d := jitterDuration(0); d != 0 {
		t.Errorf("got %v, want 0", d)
	}
}

func TestOverallSendTimeout_UsesTimeoutMs(t *testing.T) {
	ms := 1000
	d := overallSendTimeout(&ms)
	// 1000ms * (1+2) + 500ms + 1500ms + 30s*2 + margin ≈ large but finite.
	if d < 3*time.Second {
		t.Errorf("overall too small: %v", d)
	}
	if d > 2*time.Minute {
		t.Errorf("overall too large: %v", d)
	}
}

// TestComplete_OversizedSuccessIsNotRetried: a 2xx response whose body exceeds
// the cap is deterministic (the provider returns the same body every time), so
// it must not burn the retry budget — one hit, non-transient kind (#628).
func TestComplete_OversizedSuccessIsNotRetried(t *testing.T) {
	withFastRetry(t)
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		// One byte over the cap so the read-path size guard fires on a 2xx.
		_, _ = w.Write(bytes.Repeat([]byte("a"), MaxResponseBytes+1))
	}))
	defer srv.Close()

	_, err := Complete(context.Background(), CompleteRequest{
		Provider: AIProvider{BaseURL: srv.URL, Model: "m"},
		Messages: []ChatMessage{{Role: "user", Content: "x"}},
	})
	e, ok := err.(*AIError)
	if !ok {
		t.Fatalf("want *AIError, got %T (%v)", err, err)
	}
	if e.Kind == ErrServer || e.Kind == ErrRateLimited {
		t.Errorf("kind = %q must be non-transient (not server/rate-limited) so it is not retried", e.Kind)
	}
	if got := hits.Load(); got != 1 {
		t.Errorf("hits = %d, want 1 (oversized 2xx must not be retried)", got)
	}
}
