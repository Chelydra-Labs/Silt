package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListModels_OpenAICompat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Errorf("path = %q, want /v1/models", r.URL.Path)
		}
		if r.Method != http.MethodGet {
			t.Errorf("method = %q, want GET", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("auth = %q, want Bearer test-key", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"id": "gpt-4o"},
				{"id": "gpt-4o-mini"},
			},
		})
	}))
	defer srv.Close()
	models, err := ListModels(context.Background(), AIProvider{
		ProviderType: ProviderOpenAICompatible,
		BaseURL:      srv.URL,
		APIKey:       "test-key",
	}, "chat")
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("got %d models, want 2", len(models))
	}
	if models[0].ID != "gpt-4o" || models[0].DisplayName != "gpt-4o" {
		t.Errorf("model[0] = %+v, want ID=DisplayName=gpt-4o", models[0])
	}
}

func TestListModels_Google(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1beta/models" {
			t.Errorf("path = %q, want /v1beta/models", r.URL.Path)
		}
		if r.Header.Get("x-goog-api-key") != "google-key" {
			t.Errorf("x-goog-api-key = %q, want google-key", r.Header.Get("x-goog-api-key"))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"models": []map[string]any{
				{
					"name":                       "models/gemini-2.0-flash",
					"displayName":                "Gemini 2.0 Flash",
					"supportedGenerationMethods": []string{"generateContent", "embedContent"},
				},
				{
					"name":                       "models/embedding-001",
					"displayName":                "Text Embedding 001",
					"supportedGenerationMethods": []string{"embedContent"},
				},
			},
		})
	}))
	defer srv.Close()
	// Chat filter: should only return generateContent-supporting models.
	chatModels, err := ListModels(context.Background(), AIProvider{
		ProviderType: ProviderGoogle,
		BaseURL:      srv.URL,
		APIKey:       "google-key",
	}, "chat")
	if err != nil {
		t.Fatalf("ListModels chat: %v", err)
	}
	if len(chatModels) != 1 {
		t.Fatalf("got %d chat models, want 1 (only generateContent)", len(chatModels))
	}
	if chatModels[0].ID != "gemini-2.0-flash" {
		t.Errorf("ID = %q, want gemini-2.0-flash (models/ prefix stripped)", chatModels[0].ID)
	}
	if chatModels[0].DisplayName != "Gemini 2.0 Flash" {
		t.Errorf("DisplayName = %q, want Gemini 2.0 Flash", chatModels[0].DisplayName)
	}
	// Embedding filter: should only return embedContent-supporting models.
	embedModels, err := ListModels(context.Background(), AIProvider{
		ProviderType: ProviderGoogle,
		BaseURL:      srv.URL,
		APIKey:       "google-key",
	}, "embedding")
	if err != nil {
		t.Fatalf("ListModels embedding: %v", err)
	}
	if len(embedModels) != 2 {
		t.Errorf("got %d embedding models, want 2 (both support embedContent)", len(embedModels))
	}
}

func TestListModels_Anthropic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Errorf("path = %q, want /v1/models", r.URL.Path)
		}
		if r.Header.Get("x-api-key") != "anthropic-key" {
			t.Errorf("x-api-key = %q, want anthropic-key", r.Header.Get("x-api-key"))
		}
		if r.Header.Get("anthropic-version") != "2023-06-01" {
			t.Errorf("anthropic-version = %q, want 2023-06-01", r.Header.Get("anthropic-version"))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"id": "claude-sonnet-5", "display_name": "Claude Sonnet 5"},
				{"id": "claude-opus-4-5-20251101", "display_name": "Claude Opus 4.5"},
			},
		})
	}))
	defer srv.Close()
	models, err := ListModels(context.Background(), AIProvider{
		ProviderType: ProviderAnthropic,
		BaseURL:      srv.URL,
		APIKey:       "anthropic-key",
	}, "chat")
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("got %d models, want 2", len(models))
	}
	if models[0].ID != "claude-sonnet-5" {
		t.Errorf("ID = %q, want claude-sonnet-5", models[0].ID)
	}
	if models[0].DisplayName != "Claude Sonnet 5" {
		t.Errorf("DisplayName = %q, want Claude Sonnet 5", models[0].DisplayName)
	}
}

func TestListModels_EmptyResponse(t *testing.T) {
	// A provider returning an empty model list should yield an empty slice
	// (not an error) so the UI falls back to free-text.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{}})
	}))
	defer srv.Close()
	models, err := ListModels(context.Background(), AIProvider{
		ProviderType: ProviderOpenAICompatible,
		BaseURL:      srv.URL,
	}, "chat")
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if len(models) != 0 {
		t.Errorf("got %d models, want 0", len(models))
	}
}

func TestListModels_ErrorReturnsTyped(t *testing.T) {
	// A failing endpoint returns a typed error so the UI can show a message.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()
	_, err := ListModels(context.Background(), AIProvider{
		ProviderType: ProviderOpenAICompatible,
		BaseURL:      srv.URL,
	}, "chat")
	e, ok := err.(*AIError)
	if !ok {
		t.Fatalf("want *AIError, got %T (%v)", err, err)
	}
	if e.Kind != ErrUnauthorized {
		t.Errorf("kind = %q, want %q", e.Kind, ErrUnauthorized)
	}
}

func TestListModels_NoBaseURL(t *testing.T) {
	_, err := ListModels(context.Background(), AIProvider{
		ProviderType: ProviderOpenAICompatible,
	}, "chat")
	if e, ok := err.(*AIError); !ok || e.Kind != ErrUnreachable {
		t.Errorf("want ErrUnreachable for empty base URL, got %v", err)
	}
}
