package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
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
