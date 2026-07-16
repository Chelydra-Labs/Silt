package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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

func TestCopyAIAPIKey_PropagatesSourceKeyServerSide(t *testing.T) {
	// The "Sync providers" toggle promises to share chat's existing key with
	// embedding without re-entry. CopyAIAPIKey does this server-side so the
	// secret never reaches the renderer (which can only see HasKey).
	app := newTestApp(t)
	if err := app.SetAIAPIKey("chat", "chat-secret"); err != nil {
		t.Fatalf("SetAIAPIKey: %v", err)
	}
	if err := app.CopyAIAPIKey("chat", "embedding"); err != nil {
		t.Fatalf("CopyAIAPIKey: %v", err)
	}
	app.configMu.RLock()
	emb := app.cfg.AI.Embedding.APIKey
	app.configMu.RUnlock()
	if emb != "chat-secret" {
		t.Errorf("embedding key should be copied from chat, got %q", emb)
	}
	// And the public view reflects it.
	pub, err := app.GetAIProviderConfig()
	if err != nil {
		t.Fatalf("GetAIProviderConfig: %v", err)
	}
	if !pub.Embedding.HasKey {
		t.Error("Embedding.HasKey should be true after the copy")
	}
}

func TestCopyAIAPIKey_NoopWhenSourceHasNoKey(t *testing.T) {
	// Toggling sync for a keyless provider must not error or clobber an
	// existing destination key.
	app := newTestApp(t)
	if err := app.SetAIAPIKey("embedding", "emb-secret"); err != nil {
		t.Fatalf("SetAIAPIKey: %v", err)
	}
	if err := app.CopyAIAPIKey("chat", "embedding"); err != nil {
		t.Fatalf("CopyAIAPIKey from keyless chat: %v", err)
	}
	app.configMu.RLock()
	emb := app.cfg.AI.Embedding.APIKey
	app.configMu.RUnlock()
	if emb != "emb-secret" {
		t.Errorf("destination key should be untouched, got %q", emb)
	}
}

func TestCopyAIAPIKey_SameRoleIsNoop(t *testing.T) {
	app := newTestApp(t)
	if err := app.SetAIAPIKey("chat", "chat-secret"); err != nil {
		t.Fatalf("SetAIAPIKey: %v", err)
	}
	if err := app.CopyAIAPIKey("chat", "chat"); err != nil {
		t.Fatalf("CopyAIAPIKey same-role: %v", err)
	}
}

func TestCopyAIAPIKey_RejectsBadWhich(t *testing.T) {
	app := newTestApp(t)
	if err := app.CopyAIAPIKey("bogus", "embedding"); err == nil {
		t.Error("CopyAIAPIKey should reject an unknown source role")
	}
	if err := app.CopyAIAPIKey("chat", "bogus"); err == nil {
		t.Error("CopyAIAPIKey should reject an unknown destination role")
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
	_, err := app.PluginAIComplete("silt-tasks", "not-a-real-token", PluginAICompleteInput{
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

	tok, err := app.RegisterPluginSession("silt-tasks")
	if err != nil {
		t.Fatalf("RegisterPluginSession: %v", err)
	}
	res, err := app.PluginAIComplete("silt-tasks", tok, PluginAICompleteInput{
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

	tok, err := app.RegisterPluginSession("silt-tasks")
	if err != nil {
		t.Fatalf("RegisterPluginSession: %v", err)
	}
	res, err := app.PluginAIEmbed("silt-tasks", tok, PluginAIEmbedInput{
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

// TestPluginAIEmbed_TaskTypeThreadsToGoogle asserts that the optional TaskType
// field on PluginAIEmbedInput flows through the app binding into the Google
// batchEmbedContents request body — the end-to-end #610 contract.
func TestPluginAIEmbed_TaskTypeThreadsToGoogle(t *testing.T) {
	app := newTestApp(t)
	var capturedBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &capturedBody)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"embeddings": []map[string]any{
				{"values": []float64{0.1, 0.2}},
			},
		})
	}))
	defer srv.Close()

	app.configMu.Lock()
	app.cfg.AI.Embedding.BaseURL = srv.URL
	app.cfg.AI.Embedding.Model = "text-embedding-004"
	app.cfg.AI.Embedding.ProviderType = ai.ProviderGoogle
	app.configMu.Unlock()

	tok, err := app.RegisterPluginSession("silt-tasks")
	if err != nil {
		t.Fatalf("RegisterPluginSession: %v", err)
	}
	_, err = app.PluginAIEmbed("silt-tasks", tok, PluginAIEmbedInput{
		Texts:    []string{"hello"},
		TaskType: "RETRIEVAL_DOCUMENT",
	})
	if err != nil {
		t.Fatalf("PluginAIEmbed: %v", err)
	}
	reqs, ok := capturedBody["requests"].([]any)
	if !ok || len(reqs) != 1 {
		t.Fatalf("expected 1 request in body, got %v", capturedBody)
	}
	item, _ := reqs[0].(map[string]any)
	if item["taskType"] != "RETRIEVAL_DOCUMENT" {
		t.Errorf("taskType = %v, want RETRIEVAL_DOCUMENT", item["taskType"])
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

	tok, _ := app.RegisterPluginSession("silt-tasks")
	_, err := app.PluginAIComplete("silt-tasks", tok, PluginAICompleteInput{
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

// TestProviderTypeLiteralsMatchConfig guards against silent dispatch drift:
// the ai package cannot import config (layering rule), so the provider-type
// string constants are duplicated. If one side ever changes a literal, the
// dispatcher would silently route through the wrong provider shape. This test
// asserts both packages agree on every literal value.
func TestProviderTypeLiteralsMatchConfig(t *testing.T) {
	pairs := []struct{ aiVal, cfgVal string }{
		{ai.ProviderLocal, config.AIProviderLocal},
		{ai.ProviderOpenAICompatible, config.AIProviderOpenAICompatible},
		{ai.ProviderGoogle, config.AIProviderGoogle},
		{ai.ProviderAnthropic, config.AIProviderAnthropic},
	}
	for _, p := range pairs {
		if p.aiVal != p.cfgVal {
			t.Fatalf("provider literal drift: ai=%q config=%q", p.aiVal, p.cfgVal)
		}
	}
}

// --- ListModels + model cache ---------------------------------------------

func TestListModels_ColdStartReturnsEmpty(t *testing.T) {
	// force=false with no cache returns empty (no surprise network call).
	app := newTestApp(t)
	models, err := app.ListModels("chat", false)
	if err != nil {
		t.Fatalf("ListModels cold start: %v", err)
	}
	if len(models) != 0 {
		t.Errorf("cold-start ListModels should return empty, got %d", len(models))
	}
}

func TestListModels_ForcesPollAndCaches(t *testing.T) {
	app := newTestApp(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"id": "gpt-4o"},
				{"id": "gpt-4o-mini"},
			},
		})
	}))
	defer srv.Close()
	pointAIProviderAt(t, app, "chat", srv.URL, "gpt-4o")

	// force=true polls and caches.
	models, err := app.ListModels("chat", true)
	if err != nil {
		t.Fatalf("ListModels force: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("got %d models, want 2", len(models))
	}

	// force=false now serves from cache (the test server is still up, but the
	// cache path is exercised — the key point is no error + same data).
	cached, err := app.ListModels("chat", false)
	if err != nil {
		t.Fatalf("ListModels cached: %v", err)
	}
	if len(cached) != 2 {
		t.Errorf("cached models = %d, want 2", len(cached))
	}
}

func TestListModels_CacheInvalidatedOnProviderConfigChange(t *testing.T) {
	app := newTestApp(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"id": "model-a"}},
		})
	}))
	defer srv.Close()
	pointAIProviderAt(t, app, "chat", srv.URL, "model-a")

	// Populate the cache.
	_, err := app.ListModels("chat", true)
	if err != nil {
		t.Fatalf("ListModels force: %v", err)
	}

	// Changing the provider config invalidates the cache.
	if err := app.UpdateAIProviderConfig("chat", AIProviderPatch{
		ProviderType: config.AIProviderOpenAICompatible,
		BaseURL:      srv.URL,
		Model:        "model-b",
	}); err != nil {
		t.Fatalf("UpdateAIProviderConfig: %v", err)
	}

	// force=false now returns empty (cache was invalidated).
	models, err := app.ListModels("chat", false)
	if err != nil {
		t.Fatalf("ListModels after invalidation: %v", err)
	}
	if len(models) != 0 {
		t.Errorf("cache should be invalidated after provider config change, got %d models", len(models))
	}
}

func TestListModels_CacheInvalidatedOnKeyChange(t *testing.T) {
	app := newTestApp(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"id": "model-a"}},
		})
	}))
	defer srv.Close()
	pointAIProviderAt(t, app, "chat", srv.URL, "model-a")

	// Populate the cache.
	_, err := app.ListModels("chat", true)
	if err != nil {
		t.Fatalf("ListModels force: %v", err)
	}

	// Setting a key invalidates the cache.
	if err := app.SetAIAPIKey("chat", "new-key"); err != nil {
		t.Fatalf("SetAIAPIKey: %v", err)
	}

	// force=false returns empty (cache was invalidated by the key change).
	models, _ := app.ListModels("chat", false)
	if len(models) != 0 {
		t.Errorf("cache should be invalidated after key change, got %d models", len(models))
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

// --- Tool-calling threading (#595) ---------------------------------------

// TestPluginAIComplete_ThreadsToolsAndToolCalls verifies the app binding maps
// the plugin-facing Tools/ToolChoice into CompleteRequest and returns the
// decoded ToolCalls from CompleteResult. Uses an OpenAI-compat mock that
// echoes the request it received and replies with a tool_call.
func TestPluginAIComplete_ThreadsToolsAndToolCalls(t *testing.T) {
	app := newTestApp(t)
	var capturedBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &capturedBody)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model": "m",
			"choices": []map[string]any{
				{"message": map[string]any{
					"role": "assistant",
					"tool_calls": []map[string]any{
						{"id": "call_1", "type": "function", "function": map[string]any{
							"name":      "search_notes",
							"arguments": `{"q":"x"}`,
						}},
					},
				}},
			},
		})
	}))
	defer srv.Close()
	pointAIProviderAt(t, app, "chat", srv.URL, "m")

	tok, err := app.RegisterPluginSession("silt-tasks")
	if err != nil {
		t.Fatalf("RegisterPluginSession: %v", err)
	}
	res, err := app.PluginAIComplete("silt-tasks", tok, PluginAICompleteInput{
		Messages: []PluginAIChatMessage{{Role: "user", Content: "find x"}},
		Tools: []PluginAIToolDef{{
			Name:       "search_notes",
			Parameters: json.RawMessage(`{"type":"object"}`),
		}},
		ToolChoice: &PluginAIToolChoice{Mode: "auto"},
	})
	if err != nil {
		t.Fatalf("PluginAIComplete: %v", err)
	}
	// Tools + tool_choice threaded into the provider request.
	if rawTools, _ := capturedBody["tools"].([]any); len(rawTools) != 1 {
		t.Errorf("provider request tools = %v, want 1", rawTools)
	}
	if capturedBody["tool_choice"] != "auto" {
		t.Errorf("provider request tool_choice = %v, want auto", capturedBody["tool_choice"])
	}
	// ToolCalls flowed back on the result.
	if len(res.ToolCalls) != 1 {
		t.Fatalf("tool_calls = %d, want 1", len(res.ToolCalls))
	}
	if res.ToolCalls[0].Name != "search_notes" || res.ToolCalls[0].ID != "call_1" {
		t.Errorf("tool_call = %+v", res.ToolCalls[0])
	}
}

// TestPluginAIComplete_ToolMessageThreadsToProvider verifies a tool-role
// message in the input maps onto the provider request (OpenAI role:tool +
// tool_call_id) so the agent loop can replay multi-turn history.
func TestPluginAIComplete_ToolMessageThreadsToProvider(t *testing.T) {
	app := newTestApp(t)
	var capturedBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &capturedBody)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model":   "m",
			"choices": []map[string]any{{"message": map[string]any{"content": "ok"}}},
		})
	}))
	defer srv.Close()
	pointAIProviderAt(t, app, "chat", srv.URL, "m")

	tok, _ := app.RegisterPluginSession("silt-tasks")
	_, err := app.PluginAIComplete("silt-tasks", tok, PluginAICompleteInput{
		Messages: []PluginAIChatMessage{
			{Role: "user", Content: "find x"},
			{Role: "assistant", ToolCalls: []PluginAIToolCall{
				{ID: "call_1", Name: "search_notes", Arguments: json.RawMessage(`{"q":"x"}`)},
			}},
			{Role: "tool", ToolCallID: "call_1", Content: "found"},
		},
	})
	if err != nil {
		t.Fatalf("PluginAIComplete: %v", err)
	}
	msgs, _ := capturedBody["messages"].([]any)
	if len(msgs) != 3 {
		t.Fatalf("messages = %d, want 3", len(msgs))
	}
	tool, _ := msgs[2].(map[string]any)
	if tool["role"] != "tool" || tool["tool_call_id"] != "call_1" || tool["content"] != "found" {
		t.Errorf("tool message not threaded: %+v", tool)
	}
}

// --- Tool/tool_choice boundary validation (#595 hardening) ---------------

// TestPluginAIComplete_RejectsMalformedTools verifies tools + tool_choice are
// validated at the plugin boundary (bad-request) before rate-limiting or HTTP.
func TestPluginAIComplete_RejectsMalformedTools(t *testing.T) {
	app := newTestApp(t)
	tok, _ := app.RegisterPluginSession("silt-tasks")

	cases := []struct {
		name       string
		tools      []PluginAIToolDef
		choice     *PluginAIToolChoice
		wantSubstr string
	}{
		{
			name:       "empty tool name",
			tools:      []PluginAIToolDef{{Name: "  ", Parameters: json.RawMessage(`{"type":"object"}`)}},
			wantSubstr: "non-empty name",
		},
		{
			name:       "unknown tool_choice mode",
			tools:      []PluginAIToolDef{{Name: "search_notes", Parameters: json.RawMessage(`{"type":"object"}`)}},
			choice:     &PluginAIToolChoice{Mode: "bogus"},
			wantSubstr: "tool_choice.mode",
		},
		{
			name:       "force references unknown tool",
			tools:      []PluginAIToolDef{{Name: "search_notes", Parameters: json.RawMessage(`{"type":"object"}`)}},
			choice:     &PluginAIToolChoice{Mode: "force", ToolName: "ghost"},
			wantSubstr: "unknown tool",
		},
		{
			name:       "force without tool_name",
			tools:      []PluginAIToolDef{{Name: "search_notes", Parameters: json.RawMessage(`{"type":"object"}`)}},
			choice:     &PluginAIToolChoice{Mode: "force"},
			wantSubstr: "force",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := app.PluginAIComplete("silt-tasks", tok, PluginAICompleteInput{
				Messages:   []PluginAIChatMessage{{Role: "user", Content: "x"}},
				Tools:      tc.tools,
				ToolChoice: tc.choice,
			})
			if err == nil {
				t.Fatalf("expected bad-request rejection, got nil")
			}
			aiErr, ok := err.(*ai.AIError)
			if !ok || aiErr.Kind != ai.ErrBadRequest {
				t.Fatalf("expected bad-request AIError, got %v", err)
			}
			if !containsStr(aiErr.Message, tc.wantSubstr) {
				t.Fatalf("error %q does not contain %q", aiErr.Message, tc.wantSubstr)
			}
		})
	}
}

// TestPluginAIComplete_AcceptsForceOnKnownTool verifies a "force" tool_choice
// naming a declared tool passes the boundary gate and reaches the provider.
func TestPluginAIComplete_AcceptsForceOnKnownTool(t *testing.T) {
	app := newTestApp(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model":   "m",
			"choices": []map[string]any{{"message": map[string]any{"content": "ok"}}},
		})
	}))
	defer srv.Close()
	pointAIProviderAt(t, app, "chat", srv.URL, "m")
	tok, _ := app.RegisterPluginSession("silt-tasks")

	_, err := app.PluginAIComplete("silt-tasks", tok, PluginAICompleteInput{
		Messages:   []PluginAIChatMessage{{Role: "user", Content: "x"}},
		Tools:      []PluginAIToolDef{{Name: "search_notes", Parameters: json.RawMessage(`{"type":"object"}`)}},
		ToolChoice: &PluginAIToolChoice{Mode: "force", ToolName: "search_notes"},
	})
	if err != nil {
		t.Fatalf("expected accepted force choice, got %v", err)
	}
}

// --- Reasoning-effort gate (Fix D) ---------------------------------------

func TestUpdateAIProviderConfig_RejectsInvalidReasoningEffort(t *testing.T) {
	app := newTestApp(t)
	bogus := "ultra"
	err := app.UpdateAIProviderConfig("chat", AIProviderPatch{
		ProviderType:    "openai-compatible",
		Model:           "m",
		ReasoningEffort: stringPtrAI(bogus),
	})
	if err == nil || !containsStr(err.Error(), "reasoning_effort") {
		t.Errorf("expected reasoning_effort rejection error, got %v", err)
	}
	// The invalid value must not have persisted: the live block has no
	// reasoning_effort set (the default) and the model is still unset too,
	// proving validation aborted before any mutation.
	app.configMu.RLock()
	chat := app.cfg.AI.Chat
	app.configMu.RUnlock()
	if chat.ReasoningEffort != nil {
		t.Errorf("invalid reasoning_effort leaked into config: %q", *chat.ReasoningEffort)
	}
	if chat.Model != "" {
		t.Errorf("invalid patch should not partially apply; model = %q", chat.Model)
	}
}

func TestUpdateAIProviderConfig_AcceptsValidReasoningEffort(t *testing.T) {
	app := newTestApp(t)
	if err := app.UpdateAIProviderConfig("chat", AIProviderPatch{
		ProviderType:    "openai-compatible",
		Model:           "m",
		ReasoningEffort: stringPtrAI("high"),
	}); err != nil {
		t.Fatalf("valid reasoning_effort should be accepted, got %v", err)
	}
	app.configMu.RLock()
	chat := app.cfg.AI.Chat
	app.configMu.RUnlock()
	if chat.ReasoningEffort == nil || *chat.ReasoningEffort != "high" {
		t.Errorf("reasoning_effort not applied: %+v", chat.ReasoningEffort)
	}
}

// TestPluginAIComplete_RejectsInvalidReasoningEffort verifies the gate fires
// BEFORE withAIPreflight — the call returns a typed bad-request error without
// needing a registered session, provider config, or HTTP server. If the
// validation ran after preflight this would surface a session-denial error
// instead.
func TestPluginAIComplete_RejectsInvalidReasoningEffort(t *testing.T) {
	app := newTestApp(t)
	bogus := "turbo"
	_, err := app.PluginAIComplete("silt-tasks", "never-registered", PluginAICompleteInput{
		Messages:        []PluginAIChatMessage{{Role: "user", Content: "x"}},
		ReasoningEffort: stringPtrAI(bogus),
	})
	if err == nil {
		t.Fatal("invalid reasoning_effort should be rejected")
	}
	e, ok := err.(*ai.AIError)
	if !ok {
		t.Fatalf("want *ai.AIError for invalid reasoning_effort, got %T (%v)", err, err)
	}
	if e.Kind != ai.ErrBadRequest {
		t.Errorf("Kind = %q, want %q", e.Kind, ai.ErrBadRequest)
	}
	if !containsStr(e.Message, "reasoning_effort") {
		t.Errorf("error message should mention reasoning_effort, got %q", e.Message)
	}
}

// stringPtrAI is the *string helper local to these tests (mirrors intPtrAI).
func stringPtrAI(s string) *string { return &s }

// TestPluginAIComplete_TrackedByWaitGroup verifies that an in-flight
// PluginAIComplete is tracked by a.wg so shutdown's a.wg.Wait() drains it
// before teardownVaultServices clears the audit state. Without the tracking, a
// call that completes after teardown repopulates the package-level aiAudit slice
// with a stale entry, blocking the next vault's seed and/or writing to the wrong
// vault's ai.log.
func TestPluginAIComplete_TrackedByWaitGroup(t *testing.T) {
	app := newTestApp(t)
	resetAIAuditState(t)

	// Chat server that signals when the request arrives, then blocks until
	// released — simulating a slow LLM call that outlives a vault close.
	requestReceived := make(chan struct{})
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(requestReceived)
		<-release
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model":   "test",
			"choices": []map[string]any{{"message": map[string]any{"content": "pong"}}},
		})
	}))
	defer srv.Close()
	pointAIProviderAt(t, app, "chat", srv.URL, "test")

	tok, err := app.RegisterPluginSession("silt-tasks")
	if err != nil {
		t.Fatalf("RegisterPluginSession: %v", err)
	}

	// Kick off PluginAIComplete — it will block at the HTTP call.
	type callResult struct {
		res ai.CompleteResult
		err error
	}
	callDone := make(chan callResult, 1)
	go func() {
		res, err := app.PluginAIComplete("silt-tasks", tok, PluginAICompleteInput{
			Messages: []PluginAIChatMessage{{Role: "user", Content: "ping"}},
		})
		callDone <- callResult{res, err}
	}()

	// Wait until the call has passed withAIPreflight (released vaultMu) and
	// reached the blocking HTTP server — deterministic, no timing assumption.
	select {
	case <-requestReceived:
	case <-time.After(5 * time.Second):
		t.Fatal("PluginAIComplete did not reach the HTTP server in time")
	}

	// a.wg.Wait() must block while PluginAIComplete is in flight (it's tracked).
	// If the tracking were missing, Wait() would return immediately.
	wgReturned := make(chan struct{})
	go func() { app.wg.Wait(); close(wgReturned) }()
	select {
	case <-wgReturned:
		t.Fatal("a.wg.Wait() returned while PluginAIComplete is still in flight — call is not tracked")
	case <-time.After(200 * time.Millisecond):
		// Expected: Wait() is still blocking.
	}

	// Release the HTTP server so the call can complete.
	close(release)

	// Now a.wg.Wait() should return once PluginAIComplete finishes.
	select {
	case <-wgReturned:
	case <-time.After(10 * time.Second):
		t.Fatal("a.wg.Wait() did not return after PluginAIComplete completed")
	}

	// The call should have succeeded.
	r := <-callDone
	if r.err != nil {
		t.Fatalf("PluginAIComplete: %v", r.err)
	}
	if r.res.Content != "pong" {
		t.Errorf("content = %q, want pong", r.res.Content)
	}
}

func TestValidateAITools_RejectsDuplicateNames(t *testing.T) {
	tools := []PluginAIToolDef{
		{Name: "search", Parameters: json.RawMessage(`{"type":"object","properties":{}}`)},
		{Name: "search", Parameters: json.RawMessage(`{"type":"object","properties":{}}`)},
	}
	if err := validateAITools(tools, nil); err == nil {
		t.Fatal("expected error for duplicate tool names")
	}
}

func TestValidateAITools_RejectsBadParameters(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{"missing", ""},
		{"scalar", `"string"`},
		{"number", `42`},
		{"array", `[]`},
		{"wrong-type", `{"type":"string"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tools := []PluginAIToolDef{{
				Name:       "t",
				Parameters: json.RawMessage(tc.raw),
			}}
			if err := validateAITools(tools, nil); err == nil {
				t.Fatalf("expected error for %s parameters", tc.name)
			}
		})
	}
}

func TestValidateAITools_AcceptsValidSchema(t *testing.T) {
	tools := []PluginAIToolDef{
		{Name: "search", Parameters: json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}}}`)},
		{Name: "read", Parameters: json.RawMessage(`{"properties":{"id":{"type":"string"}}}`)},
	}
	if err := validateAITools(tools, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
