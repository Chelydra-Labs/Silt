package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"silt/backend/ai"
	"silt/backend/config"
	"silt/backend/plugins"
)

// --- Config page bindings ------------------------------------------------

func TestGetAIProviderConfig_StripsKeyShowsHasKey(t *testing.T) {
	app := newTestApp(t)
	if err := app.SetAIAPIKey("chat", "chat-secret"); err != nil {
		t.Fatalf("SetAIAPIKey: %v", err)
	}
	pub, err := app.GetAIProviderConfig()
	if err != nil {
		t.Fatalf("GetAIProviderConfig: %v", err)
	}
	// The raw JSON over IPC must never contain the secret.
	b, _ := json.Marshal(pub)
	if string(b) != "" && containsStr(string(b), "chat-secret") {
		t.Errorf("API key leaked through GetAIProviderConfig:\n%s", b)
	}
	if !pub.Chat.HasKey {
		t.Errorf("Chat.HasKey should be true after SetAIAPIKey")
	}
	if pub.Embedding.HasKey {
		t.Errorf("Embedding.HasKey should be false when no key set")
	}
}

func TestUpdateAIProviderConfig_PreservesKeyAndNormalizes(t *testing.T) {
	app := newTestApp(t)
	if err := app.SetAIAPIKey("embedding", "emb-secret"); err != nil {
		t.Fatalf("SetAIAPIKey: %v", err)
	}
	// Apply a patch that sets a model + bogus provider type. The patch carries
	// no key (AIProviderPatch has none); the live key must survive.
	if err := app.UpdateAIProviderConfig("embedding", AIProviderPatch{
		ProviderType: "bogus",
		Model:        "nomic-embed-text",
		Dimensions:   intPtrAI(768),
	}); err != nil {
		t.Fatalf("UpdateAIProviderConfig: %v", err)
	}
	app.configMu.RLock()
	emb := app.cfg.AI.Embedding
	app.configMu.RUnlock()
	if emb.APIKey != "emb-secret" {
		t.Errorf("embedding key not preserved: got %q", emb.APIKey)
	}
	if emb.Model != "nomic-embed-text" {
		t.Errorf("model not applied: got %q", emb.Model)
	}
	if emb.ProviderType != "local" {
		t.Errorf("bogus provider_type should normalize to local, got %q", emb.ProviderType)
	}
	if emb.Dimensions == nil || *emb.Dimensions != 768 {
		t.Errorf("dimensions not applied: %+v", emb.Dimensions)
	}
}

func TestSetAndClearAIAPIKey(t *testing.T) {
	app := newTestApp(t)
	if err := app.SetAIAPIKey("chat", "  trimmed  "); err != nil {
		t.Fatalf("SetAIAPIKey: %v", err)
	}
	app.configMu.RLock()
	k := app.cfg.AI.Chat.APIKey
	app.configMu.RUnlock()
	if k != "trimmed" {
		t.Errorf("key should be trimmed, got %q", k)
	}
	if err := app.ClearAIAPIKey("chat"); err != nil {
		t.Fatalf("ClearAIAPIKey: %v", err)
	}
	app.configMu.RLock()
	k = app.cfg.AI.Chat.APIKey
	app.configMu.RUnlock()
	if k != "" {
		t.Errorf("key should be cleared, got %q", k)
	}
}

func TestUpdateAIProviderConfig_RejectsBadWhich(t *testing.T) {
	app := newTestApp(t)
	if err := app.UpdateAIProviderConfig("bogus", AIProviderPatch{}); err == nil {
		t.Error("UpdateAIProviderConfig should reject an unknown which")
	}
}

// SaveSystemConfig preserves AI keys across a full-config round-trip: the
// frontend never carries the key (json:"-"), so saving an unrelated section
// must NOT blank a configured provider key.
func TestSaveSystemConfig_PreservesAIKeys(t *testing.T) {
	app := newTestApp(t)
	if err := app.SetAIAPIKey("chat", "keep-me"); err != nil {
		t.Fatalf("SetAIAPIKey: %v", err)
	}
	// Simulate the IPC round-trip: GetSystemConfig returns the Go struct (key
	// present at the Go level), but Wails marshals it over IPC with json:"-",
	// so the frontend receives an empty key. Reproduce that here by marshaling
	// to JSON and back — the key must be gone — then send it back through
	// SaveSystemConfig with an unrelated editor change.
	cfg, err := app.GetSystemConfig()
	if err != nil {
		t.Fatalf("GetSystemConfig: %v", err)
	}
	b, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var roundTripped struct {
		AI struct {
			Chat struct {
				APIKey string `json:"-"`
			} `json:"chat"`
		} `json:"ai"`
	}
	if err := json.Unmarshal(b, &roundTripped); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if roundTripped.AI.Chat.APIKey != "" {
		t.Fatalf("precondition: json round-trip should strip key, got %q", roundTripped.AI.Chat.APIKey)
	}
	// Rebuild a SystemConfig as the frontend would: same struct, empty key,
	// plus an unrelated editor change. json.Unmarshal into the full struct
	// reproduces the frontend's SaveSystemConfig payload exactly.
	var fe config.SystemConfig
	if err := json.Unmarshal(b, &fe); err != nil {
		t.Fatalf("unmarshal full: %v", err)
	}
	fe.Editor.FontSizePx = 20
	if err := app.SaveSystemConfig(fe); err != nil {
		t.Fatalf("SaveSystemConfig: %v", err)
	}
	app.configMu.RLock()
	k := app.cfg.AI.Chat.APIKey
	app.configMu.RUnlock()
	if k != "keep-me" {
		t.Errorf("SaveSystemConfig should preserve the live AI key, got %q", k)
	}
}

// --- Capability gate -----------------------------------------------------

func TestPluginAIComplete_DeniesUngantedThirdParty(t *testing.T) {
	app := newTestApp(t)
	tok, err := app.RegisterPluginSession("third-party-plugin")
	if err != nil {
		t.Fatalf("RegisterPluginSession: %v", err)
	}
	_, err = app.PluginAIComplete("third-party-plugin", tok, PluginAICompleteInput{
		Messages: []PluginAIChatMessage{{Role: "user", Content: "x"}},
	})
	if err == nil {
		t.Fatal("unganted third-party PluginAIComplete should be denied")
	}
	if _, ok := err.(*plugins.CapabilityDeniedError); !ok {
		t.Errorf("want *CapabilityDeniedError, got %T (%v)", err, err)
	}
}

func TestPluginAIComplete_DeniesBadSession(t *testing.T) {
	app := newTestApp(t)
	// First-party id but a session token that was never registered.
	_, err := app.PluginAIComplete("silt-kanban", "not-a-real-token", PluginAICompleteInput{
		Messages: []PluginAIChatMessage{{Role: "user", Content: "x"}},
	})
	if err == nil {
		t.Fatal("PluginAIComplete with an invalid session token should be denied")
	}
}

// --- Success paths via httptest -----------------------------------------

// pointAIProviderAt sets the chat (or embedding) provider config to target the
// test server. Holds configMu itself.
func pointAIProviderAt(t *testing.T, app *App, which, url, model string) {
	t.Helper()
	app.configMu.Lock()
	defer app.configMu.Unlock()
	if which == "embedding" {
		app.cfg.AI.Embedding.BaseURL = url
		app.cfg.AI.Embedding.Model = model
	} else {
		app.cfg.AI.Chat.BaseURL = url
		app.cfg.AI.Chat.Model = model
	}
}

func TestPluginAIComplete_SuccessFirstParty(t *testing.T) {
	app := newTestApp(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model": "test",
			"choices": []map[string]any{
				{"message": map[string]any{"content": "pong"}},
			},
		})
	}))
	defer srv.Close()
	pointAIProviderAt(t, app, "chat", srv.URL, "test")

	// The AI audit log is a package-global slice shared across tests, so reset
	// it before asserting on counts.
	_ = app.ClearAIAudit()

	tok, err := app.RegisterPluginSession("silt-kanban")
	if err != nil {
		t.Fatalf("RegisterPluginSession: %v", err)
	}
	res, err := app.PluginAIComplete("silt-kanban", tok, PluginAICompleteInput{
		Messages: []PluginAIChatMessage{{Role: "user", Content: "ping"}},
	})
	if err != nil {
		t.Fatalf("PluginAIComplete: %v", err)
	}
	if res.Content != "pong" {
		t.Errorf("content = %q, want pong", res.Content)
	}
	// The success should be recorded in the AI audit log.
	entries, _ := app.GetAIAudit()
	if len(entries) != 1 || entries[0].Status != "ok" || entries[0].Kind != "chat" {
		t.Errorf("audit not recorded correctly: %+v", entries)
	}
}

func TestPluginAIEmbed_SuccessFirstParty(t *testing.T) {
	app := newTestApp(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model": "emb",
			"data":  []map[string]any{{"embedding": []float64{0.1, 0.2, 0.3}}},
		})
	}))
	defer srv.Close()
	pointAIProviderAt(t, app, "embedding", srv.URL, "emb")

	tok, err := app.RegisterPluginSession("silt-kanban")
	if err != nil {
		t.Fatalf("RegisterPluginSession: %v", err)
	}
	res, err := app.PluginAIEmbed("silt-kanban", tok, PluginAIEmbedInput{
		Texts: []string{"hello"},
	})
	if err != nil {
		t.Fatalf("PluginAIEmbed: %v", err)
	}
	if len(res.Embeddings) != 1 || len(res.Embeddings[0]) != 3 {
		t.Errorf("embeddings shape wrong: %+v", res.Embeddings)
	}
	if res.Dimensions != 3 {
		t.Errorf("dimensions = %d, want 3", res.Dimensions)
	}
}

func TestPluginAIComplete_AuditsNormalizedError(t *testing.T) {
	app := newTestApp(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no such model", http.StatusNotFound)
	}))
	defer srv.Close()
	pointAIProviderAt(t, app, "chat", srv.URL, "missing")

	_ = app.ClearAIAudit() // reset the shared global audit log

	tok, _ := app.RegisterPluginSession("silt-kanban")
	_, err := app.PluginAIComplete("silt-kanban", tok, PluginAICompleteInput{
		Messages: []PluginAIChatMessage{{Role: "user", Content: "x"}},
	})
	if err == nil {
		t.Fatal("expected error from missing model")
	}
	if _, ok := err.(*ai.AIError); !ok {
		t.Errorf("want *ai.AIError, got %T", err)
	}
	entries, _ := app.GetAIAudit()
	if len(entries) != 1 {
		t.Fatalf("audit entries = %d, want 1", len(entries))
	}
	if entries[0].Status != string(ai.ErrModelMissing) {
		t.Errorf("audit status = %q, want %q", entries[0].Status, ai.ErrModelMissing)
	}
}

// --- Test Connection (core, no capability gate) --------------------------

func TestTestAIConnection_SuccessAndFailure(t *testing.T) {
	app := newTestApp(t)
	okSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"model": "m", "choices": []map[string]any{{"message": map[string]any{"content": "x"}}}})
	}))
	defer okSrv.Close()
	pointAIProviderAt(t, app, "chat", okSrv.URL, "m")
	res, err := app.TestAIConnection("chat")
	if err != nil {
		t.Fatalf("TestAIConnection success path returned err: %v", err)
	}
	if !res.OK {
		t.Errorf("expected OK, got %+v", res)
	}

	// Now point at a server returning 401 → typed failure, NOT a Go error
	// (the binding converts AIError into the result envelope so the page can
	// show the kind).
	badSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad key", http.StatusUnauthorized)
	}))
	defer badSrv.Close()
	pointAIProviderAt(t, app, "chat", badSrv.URL, "m")
	res, err = app.TestAIConnection("chat")
	if err != nil {
		t.Fatalf("failure path should not return a Go error: %v", err)
	}
	if res.OK {
		t.Errorf("expected OK=false")
	}
	if res.Kind != string(ai.ErrUnauthorized) {
		t.Errorf("kind = %q, want %q", res.Kind, ai.ErrUnauthorized)
	}
}

func TestTestAIConnection_RejectsBadWhich(t *testing.T) {
	app := newTestApp(t)
	if _, err := app.TestAIConnection("bogus"); err == nil {
		t.Error("TestAIConnection should reject an unknown which")
	}
}

// intPtrAI avoids shadowing the config-package intPtr inside the main package
// tests.
func intPtrAI(i int) *int { return &i }

// containsStr is a tiny local helper (the test imports no strings package).
func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// Keep context imported (used by future streaming tests; harmless now).
var _ = context.Background

func TestUpdateAIProviderConfig_RejectsNonHTTPBaseURL(t *testing.T) {
	app := newTestApp(t)
	err := app.UpdateAIProviderConfig("chat", AIProviderPatch{
		ProviderType: "openai-compatible",
		BaseURL:      "file:///etc/passwd",
		Model:        "m",
	})
	if err == nil || !containsStr(err.Error(), "http://") {
		t.Errorf("expected scheme-rejection error, got %v", err)
	}
}
