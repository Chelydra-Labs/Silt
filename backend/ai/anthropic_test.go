package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCompleteAnthropic_Success(t *testing.T) {
	var capturedHeaders http.Header
	var capturedBody anthropicRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header.Clone()
		if r.URL.Path != "/v1/messages" {
			t.Errorf("anthropic path = %q, want /v1/messages", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &capturedBody)
		resp := map[string]any{
			"id":   "msg_01",
			"type": "message",
			"role": "assistant",
			"content": []map[string]any{
				{"type": "text", "text": "claude reply"},
			},
			"model":       "claude-sonnet-5",
			"stop_reason": "end_turn",
			"usage": map[string]any{
				"input_tokens":  12,
				"output_tokens": 7,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()
	res, err := Complete(context.Background(), CompleteRequest{
		Provider: AIProvider{ProviderType: ProviderAnthropic, BaseURL: srv.URL, APIKey: "anthropic-key", Model: "claude-sonnet-5"},
		Messages: []ChatMessage{
			{Role: "system", Content: "You are helpful."},
			{Role: "user", Content: "hi"},
		},
	})
	if err != nil {
		t.Fatalf("Complete anthropic: %v", err)
	}
	if res.Content != "claude reply" {
		t.Errorf("content = %q, want %q", res.Content, "claude reply")
	}
	// Verify auth headers.
	if got := capturedHeaders.Get("x-api-key"); got != "anthropic-key" {
		t.Errorf("x-api-key = %q, want %q", got, "anthropic-key")
	}
	if got := capturedHeaders.Get("anthropic-version"); got != "2023-06-01" {
		t.Errorf("anthropic-version = %q, want 2023-06-01", got)
	}
	if got := capturedHeaders.Get("Authorization"); got != "" {
		t.Errorf("Authorization should be absent for anthropic, got %q", got)
	}
	// System message should be extracted to the top-level system field.
	if capturedBody.System != "You are helpful." {
		t.Errorf("system = %q, want %q", capturedBody.System, "You are helpful.")
	}
	// Messages should NOT include the system message.
	if len(capturedBody.Messages) != 1 || capturedBody.Messages[0].Role != "user" {
		t.Errorf("messages = %+v, want 1 user message", capturedBody.Messages)
	}
	// Usage mapping.
	if res.Usage == nil || res.Usage.PromptTokens == nil || *res.Usage.PromptTokens != 12 {
		t.Errorf("usage prompt tokens not propagated, got %+v", res.Usage)
	}
	if res.Usage == nil || res.Usage.CompletionTokens == nil || *res.Usage.CompletionTokens != 7 {
		t.Errorf("usage completion tokens not propagated, got %+v", res.Usage)
	}
	if res.Usage == nil || res.Usage.TotalTokens == nil || *res.Usage.TotalTokens != 19 {
		t.Errorf("usage total = %v, want 19", res.Usage.TotalTokens)
	}
}

func TestCompleteAnthropic_MaxTokensDefault(t *testing.T) {
	// Anthropic requires max_tokens; when the caller omits it, the service
	// must supply a default (never send 0/nil → 400).
	var capturedMaxTokens int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req anthropicRequest
		_ = json.Unmarshal(body, &req)
		capturedMaxTokens = req.MaxTokens
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]any{{"type": "text", "text": "ok"}},
			"model":   "claude-sonnet-5",
		})
	}))
	defer srv.Close()
	_, err := Complete(context.Background(), CompleteRequest{
		Provider: AIProvider{ProviderType: ProviderAnthropic, BaseURL: srv.URL, Model: "claude-sonnet-5"},
		Messages: []ChatMessage{{Role: "user", Content: "x"}},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if capturedMaxTokens != anthropicDefaultMaxTokens {
		t.Errorf("max_tokens = %d, want default %d", capturedMaxTokens, anthropicDefaultMaxTokens)
	}
}

func TestCompleteAnthropic_MaxTokensOverride(t *testing.T) {
	var capturedMaxTokens int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req anthropicRequest
		_ = json.Unmarshal(body, &req)
		capturedMaxTokens = req.MaxTokens
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]any{{"type": "text", "text": "ok"}},
			"model":   "claude-sonnet-5",
		})
	}))
	defer srv.Close()
	custom := 1024
	_, err := Complete(context.Background(), CompleteRequest{
		Provider:  AIProvider{ProviderType: ProviderAnthropic, BaseURL: srv.URL, Model: "claude-sonnet-5"},
		Messages:  []ChatMessage{{Role: "user", Content: "x"}},
		MaxTokens: &custom,
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if capturedMaxTokens != 1024 {
		t.Errorf("max_tokens = %d, want 1024 (caller override)", capturedMaxTokens)
	}
}

func TestEmbedAnthropic_Unsupported(t *testing.T) {
	_, err := Embed(context.Background(), EmbedRequest{
		Provider: AIProvider{ProviderType: ProviderAnthropic, BaseURL: "https://api.anthropic.com", Model: "claude-sonnet-5"},
		Texts:    []string{"hi"},
	})
	e, ok := err.(*AIError)
	if !ok {
		t.Fatalf("want *AIError, got %T (%v)", err, err)
	}
	if e.Kind != ErrBadRequest {
		t.Errorf("kind = %q, want %q", e.Kind, ErrBadRequest)
	}
	// The message should mention that embeddings aren't supported.
	if e.Message == "" {
		t.Error("expected a non-empty unsupported-embeddings message")
	}
}

func TestCompleteAnthropic_ErrorClassification(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"type": "error",
			"error": map[string]any{
				"type":    "rate_limit_error",
				"message": "Rate limit exceeded",
			},
		})
	}))
	defer srv.Close()
	withFastRetry(t)
	_, err := Complete(context.Background(), CompleteRequest{
		Provider: AIProvider{ProviderType: ProviderAnthropic, BaseURL: srv.URL, Model: "claude-sonnet-5"},
		Messages: []ChatMessage{{Role: "user", Content: "x"}},
	})
	e, ok := err.(*AIError)
	if !ok {
		t.Fatalf("want *AIError, got %T (%v)", err, err)
	}
	if e.Kind != ErrRateLimited {
		t.Errorf("kind = %q, want %q", e.Kind, ErrRateLimited)
	}
	if e.Message != "Rate limit exceeded" {
		t.Errorf("message = %q, want %q", e.Message, "Rate limit exceeded")
	}
}

func TestCompleteAnthropic_AuthErrorClassification(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"type": "error",
			"error": map[string]any{
				"type":    "authentication_error",
				"message": "invalid x-api-key",
			},
		})
	}))
	defer srv.Close()
	_, err := Complete(context.Background(), CompleteRequest{
		Provider: AIProvider{ProviderType: ProviderAnthropic, BaseURL: srv.URL, APIKey: "bad", Model: "claude-sonnet-5"},
		Messages: []ChatMessage{{Role: "user", Content: "x"}},
	})
	e, ok := err.(*AIError)
	if !ok {
		t.Fatalf("want *AIError, got %T", err)
	}
	if e.Kind != ErrUnauthorized {
		t.Errorf("kind = %q, want %q", e.Kind, ErrUnauthorized)
	}
}

func TestCompleteAnthropic_FallsBackToStatusClassification(t *testing.T) {
	// Non-JSON error body → default status-based classification.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "plain error", http.StatusInternalServerError)
	}))
	defer srv.Close()
	withFastRetry(t)
	_, err := Complete(context.Background(), CompleteRequest{
		Provider: AIProvider{ProviderType: ProviderAnthropic, BaseURL: srv.URL, Model: "m"},
		Messages: []ChatMessage{{Role: "user", Content: "x"}},
	})
	e, ok := err.(*AIError)
	if !ok || e.Kind != ErrServer {
		t.Errorf("want ErrServer for plain 500, got %v", err)
	}
}

func TestCompleteAnthropic_NoContent(t *testing.T) {
	// A response with no text content blocks should surface a clear error.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]any{},
			"model":   "claude-sonnet-5",
		})
	}))
	defer srv.Close()
	_, err := Complete(context.Background(), CompleteRequest{
		Provider: AIProvider{ProviderType: ProviderAnthropic, BaseURL: srv.URL, Model: "m"},
		Messages: []ChatMessage{{Role: "user", Content: "x"}},
	})
	if err == nil {
		t.Fatal("expected error for empty content, got nil")
	}
}

func TestCompleteAnthropic_StructuredOutput(t *testing.T) {
	var capturedBody anthropicRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &capturedBody)
		// Respond with a tool_use block (forced tool-use response).
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]any{
				{
					"type": "tool_use",
					"id":   "toolu_01",
					"name": "structured_output",
					"input": map[string]any{
						"summary":   "Meeting notes",
						"tasks":     []any{"Follow up"},
						"risks":     []any{},
						"decisions": []any{},
					},
				},
			},
			"model": "claude-sonnet-5",
			"usage": map[string]any{"input_tokens": 10, "output_tokens": 20},
		})
	}))
	defer srv.Close()
	schema := json.RawMessage(`{"type":"object","properties":{"summary":{"type":"string"},"tasks":{"type":"array","items":{"type":"string"}}},"required":["summary","tasks"]}`)
	res, err := Complete(context.Background(), CompleteRequest{
		Provider:       AIProvider{ProviderType: ProviderAnthropic, BaseURL: srv.URL, Model: "claude-sonnet-5"},
		Messages:       []ChatMessage{{Role: "user", Content: "summarize"}},
		ResponseSchema: schema,
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	// The response content should be the JSON-stringified tool input.
	if !contains(res.Content, `"summary":"Meeting notes"`) {
		t.Errorf("content = %q, want JSON with summary field", res.Content)
	}
	// Verify the request included the forced tool.
	if len(capturedBody.Tools) != 1 {
		t.Fatalf("tools len = %d, want 1", len(capturedBody.Tools))
	}
	if capturedBody.Tools[0].Name != "structured_output" {
		t.Errorf("tool name = %q, want structured_output", capturedBody.Tools[0].Name)
	}
	// ToolChoice should force the tool.
	tc, ok := capturedBody.ToolChoice.(map[string]any)
	if !ok || tc["type"] != "tool" || tc["name"] != "structured_output" {
		t.Errorf("tool_choice = %+v, want {type:tool, name:structured_output}", capturedBody.ToolChoice)
	}
}
