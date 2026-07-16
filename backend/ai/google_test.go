package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// googleChatServer asserts the native generateContent request shape and replies
// with a canned completion.
func googleChatServer(t *testing.T, key string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Auth header must be x-goog-api-key, NOT Authorization: Bearer.
		if key != "" && r.Header.Get("x-goog-api-key") != key {
			http.Error(w, "missing/invalid x-goog-api-key", http.StatusUnauthorized)
			return
		}
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("google: Authorization header should be absent, got %q", got)
		}
		body, _ := io.ReadAll(r.Body)
		var req googleGenerateRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("parse google request: %v", err)
		}
		// Verify system messages were extracted to systemInstruction.
		if req.SystemInstruction == nil {
			t.Error("google: systemInstruction should be set when a system message is present")
		}
		// Verify the assistant role was mapped to "model".
		for _, c := range req.Contents {
			if c.Role != "user" && c.Role != "model" {
				t.Errorf("google: unexpected role %q (want user or model)", c.Role)
			}
		}
		resp := map[string]any{
			"candidates": []map[string]any{
				{"content": map[string]any{"parts": []map[string]any{{"text": "google reply"}}}},
			},
			"usageMetadata": map[string]any{
				"promptTokenCount":     10,
				"candidatesTokenCount": 5,
				"totalTokenCount":      15,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

func TestCompleteGoogle_Success(t *testing.T) {
	srv := googleChatServer(t, "google-key")
	defer srv.Close()
	res, err := Complete(context.Background(), CompleteRequest{
		Provider: AIProvider{ProviderType: ProviderGoogle, BaseURL: srv.URL, APIKey: "google-key", Model: "gemini-2.0-flash"},
		Messages: []ChatMessage{
			{Role: "system", Content: "You are helpful."},
			{Role: "user", Content: "hi"},
			{Role: "assistant", Content: "hello"},
			{Role: "user", Content: "bye"},
		},
	})
	if err != nil {
		t.Fatalf("Complete google: %v", err)
	}
	if res.Content != "google reply" {
		t.Errorf("content = %q, want %q", res.Content, "google reply")
	}
	if res.Usage == nil || res.Usage.PromptTokens == nil || *res.Usage.PromptTokens != 10 {
		t.Errorf("usage prompt tokens not propagated, got %+v", res.Usage)
	}
	if res.Usage == nil || res.Usage.CompletionTokens == nil || *res.Usage.CompletionTokens != 5 {
		t.Errorf("usage completion tokens not propagated, got %+v", res.Usage)
	}
}

// TestCompleteGoogle_ResolvesToolResultNameFromIndex: a tool result that
// carries only an opaque call id must resolve its function name from the prior
// assistant tool_call via the single-pass id→name index, emitting
// functionResponse{name=resolved, id=call_id} (#637).
func TestCompleteGoogle_ResolvesToolResultNameFromIndex(t *testing.T) {
	var captured googleGenerateRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"candidates": []map[string]any{
				{"content": map[string]any{"parts": []map[string]any{{"text": "done"}}}},
			},
		})
	}))
	defer srv.Close()

	_, err := Complete(context.Background(), CompleteRequest{
		Provider: AIProvider{ProviderType: ProviderGoogle, BaseURL: srv.URL, APIKey: "k", Model: "gemini-2.0-flash"},
		Messages: []ChatMessage{
			{Role: "user", Content: "find notes"},
			{Role: RoleAssistant, ToolCalls: []ToolCall{
				{ID: "call_99", Name: "search_notes", Arguments: json.RawMessage(`{"q":"x"}`)},
			}},
			{Role: RoleTool, ToolCallID: "call_99", Content: "found it"},
			{Role: "user", Content: "thanks"},
		},
	})
	if err != nil {
		t.Fatalf("Complete google: %v", err)
	}

	var fr *googleFunctionResp
	for _, c := range captured.Contents {
		for _, p := range c.Parts {
			if p.FunctionResponse != nil {
				fr = p.FunctionResponse
			}
		}
	}
	if fr == nil {
		t.Fatal("google: request had no functionResponse part")
	}
	if fr.Name != "search_notes" {
		t.Errorf("functionResponse.name = %q, want search_notes (resolved from call_99 via the index)", fr.Name)
	}
	if fr.ID != "call_99" {
		t.Errorf("functionResponse.id = %q, want call_99", fr.ID)
	}
}

func TestCompleteGoogle_ModelsPathPrefix(t *testing.T) {
	var capturedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"candidates": []map[string]any{
				{"content": map[string]any{"parts": []map[string]any{{"text": "ok"}}}},
			},
		})
	}))
	defer srv.Close()
	_, err := Complete(context.Background(), CompleteRequest{
		Provider: AIProvider{ProviderType: ProviderGoogle, BaseURL: srv.URL, Model: "gemini-2.0-flash"},
		Messages: []ChatMessage{{Role: "user", Content: "x"}},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	// The model id must be prefixed with models/ in the path.
	if capturedPath == "" {
		t.Fatal("no request captured")
	}
	// Path should contain models/gemini-2.0-flash:generateContent
	if !contains(capturedPath, "models/gemini-2.0-flash:generateContent") {
		t.Errorf("path = %q, want it to contain models/gemini-2.0-flash:generateContent", capturedPath)
	}
}

func TestCompleteGoogle_UsesApiKeyHeader(t *testing.T) {
	var capturedKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedKey = r.Header.Get("x-goog-api-key")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"candidates": []map[string]any{
				{"content": map[string]any{"parts": []map[string]any{{"text": "ok"}}}},
			},
		})
	}))
	defer srv.Close()
	_, err := Complete(context.Background(), CompleteRequest{
		Provider: AIProvider{ProviderType: ProviderGoogle, BaseURL: srv.URL, APIKey: "my-google-key", Model: "gemini-2.0-flash"},
		Messages: []ChatMessage{{Role: "user", Content: "x"}},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if capturedKey != "my-google-key" {
		t.Errorf("x-goog-api-key = %q, want %q", capturedKey, "my-google-key")
	}
}

func TestEmbedGoogle_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Path must end with :batchEmbedContents
		if !contains(r.URL.Path, ":batchEmbedContents") {
			t.Errorf("google embed path = %q, want it to contain :batchEmbedContents", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		var req googleEmbedRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("parse google embed request: %v", err)
		}
		if len(req.Requests) != 3 {
			t.Errorf("google embed requests len = %d, want 3", len(req.Requests))
		}
		embeddings := make([]map[string]any, len(req.Requests))
		for i := range req.Requests {
			vec := make([]float64, 4)
			for j := range vec {
				vec[j] = float64(i + j)
			}
			embeddings[i] = map[string]any{"values": vec}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"embeddings": embeddings})
	}))
	defer srv.Close()
	res, err := Embed(context.Background(), EmbedRequest{
		Provider: AIProvider{ProviderType: ProviderGoogle, BaseURL: srv.URL, Model: "text-embedding-004"},
		Texts:    []string{"one", "two", "three"},
	})
	if err != nil {
		t.Fatalf("Embed google: %v", err)
	}
	if len(res.Embeddings) != 3 {
		t.Fatalf("got %d embeddings, want 3", len(res.Embeddings))
	}
	if res.Dimensions != 4 {
		t.Errorf("dimensions = %d, want 4", res.Dimensions)
	}
}

func TestEmbedGoogle_TaskTypeInBody(t *testing.T) {
	cases := []struct {
		name     string
		taskType string
		wantKey  bool
	}{
		{name: "document", taskType: "RETRIEVAL_DOCUMENT", wantKey: true},
		{name: "query", taskType: "RETRIEVAL_QUERY", wantKey: true},
		{name: "empty omits", taskType: "", wantKey: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var raw map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				if err := json.Unmarshal(body, &raw); err != nil {
					t.Errorf("parse body: %v", err)
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"embeddings": []map[string]any{{"values": []float64{0.1, 0.2}}},
				})
			}))
			defer srv.Close()
			_, err := Embed(context.Background(), EmbedRequest{
				Provider: AIProvider{ProviderType: ProviderGoogle, BaseURL: srv.URL, Model: "text-embedding-004"},
				Texts:    []string{"hello"},
				TaskType: tc.taskType,
			})
			if err != nil {
				t.Fatalf("Embed: %v", err)
			}
			reqs, _ := raw["requests"].([]any)
			if len(reqs) != 1 {
				t.Fatalf("requests len = %d, want 1", len(reqs))
			}
			item, _ := reqs[0].(map[string]any)
			got, has := item["taskType"]
			if has != tc.wantKey {
				t.Fatalf("taskType present = %v, want %v (body item=%v)", has, tc.wantKey, item)
			}
			if tc.wantKey && got != tc.taskType {
				t.Errorf("taskType = %v, want %q", got, tc.taskType)
			}
		})
	}
}

func TestCompleteGoogle_ErrorClassification(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"code":    429,
				"message": "Quota exceeded",
				"status":  "RESOURCE_EXHAUSTED",
			},
		})
	}))
	defer srv.Close()
	// Shrink retry so the rate-limited response doesn't pay real backoff.
	withFastRetry(t)
	_, err := Complete(context.Background(), CompleteRequest{
		Provider: AIProvider{ProviderType: ProviderGoogle, BaseURL: srv.URL, Model: "gemini-2.0-flash"},
		Messages: []ChatMessage{{Role: "user", Content: "x"}},
	})
	e, ok := err.(*AIError)
	if !ok {
		t.Fatalf("want *AIError, got %T (%v)", err, err)
	}
	if e.Kind != ErrRateLimited {
		t.Errorf("kind = %q, want %q", e.Kind, ErrRateLimited)
	}
	if e.Status != http.StatusTooManyRequests {
		t.Errorf("status = %d, want 429", e.Status)
	}
	if e.Message != "Quota exceeded" {
		t.Errorf("message = %q, want %q", e.Message, "Quota exceeded")
	}
}

func TestCompleteGoogle_NotFoundIsModelMissing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"code":    404,
				"message": "models/bogus is not found",
				"status":  "NOT_FOUND",
			},
		})
	}))
	defer srv.Close()
	_, err := Complete(context.Background(), CompleteRequest{
		Provider: AIProvider{ProviderType: ProviderGoogle, BaseURL: srv.URL, Model: "bogus"},
		Messages: []ChatMessage{{Role: "user", Content: "x"}},
	})
	e, ok := err.(*AIError)
	if !ok {
		t.Fatalf("want *AIError, got %T", err)
	}
	if e.Kind != ErrModelMissing {
		t.Errorf("kind = %q, want %q", e.Kind, ErrModelMissing)
	}
}

func TestCompleteGoogle_FallsBackToStatusClassification(t *testing.T) {
	// When the error body isn't the expected {error: {...}} shape, the default
	// status-based classification must still produce the right Kind.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "plain text error", http.StatusBadRequest)
	}))
	defer srv.Close()
	_, err := Complete(context.Background(), CompleteRequest{
		Provider: AIProvider{ProviderType: ProviderGoogle, BaseURL: srv.URL, Model: "m"},
		Messages: []ChatMessage{{Role: "user", Content: "x"}},
	})
	e, ok := err.(*AIError)
	if !ok || e.Kind != ErrBadRequest {
		t.Errorf("want ErrBadRequest for plain 400, got %v", err)
	}
}

func TestCompleteGoogle_StructuredOutput(t *testing.T) {
	var capturedBody googleGenerateRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &capturedBody)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"candidates": []map[string]any{
				{"content": map[string]any{"parts": []map[string]any{{"text": `{"summary":"test","tasks":[],"risks":[],"decisions":[]}`}}}},
			},
		})
	}))
	defer srv.Close()
	schema := json.RawMessage(`{"type":"object","properties":{"summary":{"type":"string"},"tasks":{"type":"array","items":{"type":"string"}}},"required":["summary","tasks"]}`)
	_, err := Complete(context.Background(), CompleteRequest{
		Provider:       AIProvider{ProviderType: ProviderGoogle, BaseURL: srv.URL, Model: "gemini-2.0-flash"},
		Messages:       []ChatMessage{{Role: "user", Content: "summarize"}},
		ResponseSchema: schema,
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if capturedBody.GenerationConfig == nil {
		t.Fatal("generationConfig should be set when ResponseSchema is provided")
	}
	if capturedBody.GenerationConfig.ResponseMimeType != "application/json" {
		t.Errorf("responseMimeType = %q, want application/json", capturedBody.GenerationConfig.ResponseMimeType)
	}
	// The schema types should be UPPERCASE (Google's enum).
	var raw map[string]any
	_ = json.Unmarshal(capturedBody.GenerationConfig.ResponseSchema, &raw)
	if raw["type"] != "OBJECT" {
		t.Errorf("schema type = %v, want OBJECT (uppercase)", raw["type"])
	}
	props, _ := raw["properties"].(map[string]any)
	summary, _ := props["summary"].(map[string]any)
	if summary["type"] != "STRING" {
		t.Errorf("summary type = %v, want STRING", summary["type"])
	}
}

// contains is a local string-contains helper (avoids importing strings in test).
func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestCompleteGoogle_FiltersThoughtParts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Gemini 2.5+ thinking models emit reasoning in parts with thought:true.
		// The content extraction must skip those and keep only the answer.
		resp := map[string]any{
			"candidates": []map[string]any{
				{
					"content": map[string]any{
						"parts": []map[string]any{
							{"text": "Let me analyze the passages step by step.", "thought": true},
							{"text": "The passages do not mention page counts.", "thought": true},
							{"text": "I don't have information about your total page count."},
						},
					},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()
	res, err := Complete(context.Background(), CompleteRequest{
		Provider: AIProvider{ProviderType: ProviderGoogle, BaseURL: srv.URL, Model: "gemini-2.5-flash"},
		Messages: []ChatMessage{{Role: "user", Content: "How many pages?"}},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if contains(res.Content, "step by step") {
		t.Errorf("thought text leaked into content: %q", res.Content)
	}
	if contains(res.Content, "passages do not") {
		t.Errorf("thought text leaked into content: %q", res.Content)
	}
	want := "I don't have information about your total page count."
	if res.Content != want {
		t.Errorf("content = %q, want %q", res.Content, want)
	}
}
