package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"silt/backend/keyring"
)

// withFakeKeyring arms the app with an in-memory keyring so the keyring code
// paths are exercised (NewApp wires the real OS store; the test harness leaves
// the field nil). Returns the fake so a test can assert against it.
func withFakeKeyring(t *testing.T, app *App) *keyring.Fake {
	t.Helper()
	fake := keyring.NewFake()
	app.keyringStore = fake
	return fake
}

func TestSetAIAPIKey_KeyringStoresOffConfig(t *testing.T) {
	app := newTestApp(t)
	fake := withFakeKeyring(t, app)
	if err := app.SetAIAPIKey("chat", "keyring-secret"); err != nil {
		t.Fatalf("SetAIAPIKey: %v", err)
	}
	// The key lives in the keyring, NOT plaintext config.
	got, err := fake.Get(keyringService, app.aiKeyringUser("chat"))
	if err != nil {
		t.Fatalf("fake Get: %v", err)
	}
	if got != "keyring-secret" {
		t.Errorf("keyring held %q, want keyring-secret", got)
	}
	app.configMu.RLock()
	cfgKey := app.cfg.AI.Chat.APIKey
	app.configMu.RUnlock()
	if cfgKey != "" {
		t.Errorf("config.yaml should be blanked when the key lives in the keyring, got %q", cfgKey)
	}
}

func TestSetAIAPIKey_FallsBackToConfigWhenKeyringUnavailable(t *testing.T) {
	app := newTestApp(t)
	app.keyringStore = keyring.UnavailableFake{} // headless Linux / locked session
	if err := app.SetAIAPIKey("embedding", "fallback-secret"); err != nil {
		t.Fatalf("SetAIAPIKey: %v", err)
	}
	// Keyring unusable → the key is kept in config so the feature still works.
	app.configMu.RLock()
	cfgKey := app.cfg.AI.Embedding.APIKey
	app.configMu.RUnlock()
	if cfgKey != "fallback-secret" {
		t.Errorf("unavailable keyring should fall back to config, got %q", cfgKey)
	}
}

func TestSetAIAPIKey_ConfigPathWhenToggleOff(t *testing.T) {
	app := newTestApp(t)
	fake := withFakeKeyring(t, app)
	// Turn the keyring toggle OFF — even with a working keyring, the key must
	// land in config (the user explicitly opted out of OS-keyring storage).
	if err := app.SetUseKeyring(false); err != nil {
		t.Fatalf("SetUseKeyring: %v", err)
	}
	if err := app.SetAIAPIKey("chat", "config-secret"); err != nil {
		t.Fatalf("SetAIAPIKey: %v", err)
	}
	app.configMu.RLock()
	cfgKey := app.cfg.AI.Chat.APIKey
	app.configMu.RUnlock()
	if cfgKey != "config-secret" {
		t.Errorf("toggle off should store in config, got %q", cfgKey)
	}
	if _, err := fake.Get(keyringService, app.aiKeyringUser("chat")); err == nil {
		t.Errorf("toggle off should NOT touch the keyring")
	}
}

func TestClearAIAPIKey_RemovesFromBothStores(t *testing.T) {
	app := newTestApp(t)
	fake := withFakeKeyring(t, app)
	if err := app.SetAIAPIKey("chat", "both"); err != nil {
		t.Fatalf("SetAIAPIKey: %v", err)
	}
	// Also plant a stale config value so we prove Clear blanks it too.
	app.configMu.Lock()
	app.cfg.AI.Chat.APIKey = "stale-in-config"
	app.configMu.Unlock()
	if err := app.ClearAIAPIKey("chat"); err != nil {
		t.Fatalf("ClearAIAPIKey: %v", err)
	}
	if _, err := fake.Get(keyringService, app.aiKeyringUser("chat")); err == nil {
		t.Errorf("keyring entry should be deleted")
	}
	app.configMu.RLock()
	cfgKey := app.cfg.AI.Chat.APIKey
	app.configMu.RUnlock()
	if cfgKey != "" {
		t.Errorf("config should be blanked on clear, got %q", cfgKey)
	}
}

func TestClearAIAPIKey_IdempotentWhenNothingStored(t *testing.T) {
	app := newTestApp(t)
	withFakeKeyring(t, app)
	// Nothing stored anywhere yet — clearing must not error.
	if err := app.ClearAIAPIKey("embedding"); err != nil {
		t.Errorf("ClearAIAPIKey on empty state should be a no-op, got %v", err)
	}
}

func TestResolveAIKey_KeyringFirstConfigFallback(t *testing.T) {
	app := newTestApp(t)
	fake := withFakeKeyring(t, app)
	// Put a DIFFERENT value in each store; the keyring one must win.
	_ = fake.Set(keyringService, app.aiKeyringUser("chat"), "from-keyring")
	app.configMu.Lock()
	app.cfg.AI.Chat.APIKey = "from-config"
	app.configMu.Unlock()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Echo the Authorization header so the test can see which key was used.
		auth := r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"m","choices":[{"message":{"content":"` + auth + `"}}]}`))
	}))
	defer srv.Close()
	app.configMu.Lock()
	app.cfg.AI.Chat.BaseURL = srv.URL
	app.cfg.AI.Chat.Model = "m"
	app.configMu.Unlock()

	tok, _ := app.RegisterPluginSession("silt-tasks")
	res, err := app.PluginAIComplete("silt-tasks", tok, PluginAICompleteInput{
		Messages: []PluginAIChatMessage{{Role: "user", Content: "x"}},
	})
	if err != nil {
		t.Fatalf("PluginAIComplete: %v", err)
	}
	if res.Content != "Bearer from-keyring" {
		t.Errorf("expected the keyring key to be used, got content=%q", res.Content)
	}
}

// TestInitializeVaultServices_MigrateUnderExclusiveLock pins the #654
// deadlock fix: initializeVaultServices runs under vaultMu.Lock and must call
// migrateAIKeysToKeyringLocked (not the public self-locking migrate). With a
// non-nil keyring store + plaintext config key, the old public path RLock'd
// under exclusive Lock and hung forever.
func TestInitializeVaultServices_MigrateUnderExclusiveLock(t *testing.T) {
	app := newTestApp(t)
	fake := withFakeKeyring(t, app)

	const secret = "legacy-under-init"
	app.configMu.Lock()
	app.cfg.AI.Chat.APIKey = secret
	if err := app.saveConfigTracked(app.cfg); err != nil {
		app.configMu.Unlock()
		t.Fatalf("saveConfigTracked: %v", err)
	}
	app.configMu.Unlock()

	vaultPath := app.vaultPath
	done := make(chan error, 1)
	go func() {
		app.vaultMu.Lock()
		defer app.vaultMu.Unlock()
		// Match production cutover: exclusive lock → teardown → reinit.
		app.teardownVaultServices()
		done <- app.initializeVaultServices(vaultPath)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("initializeVaultServices under exclusive Lock: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("initializeVaultServices deadlocked under exclusive vaultMu.Lock with keyring store — migrate must use locked variant (#654)")
	}

	app.vaultMu.RLock()
	chatUser := app.aiKeyringUser("chat")
	finalPath := app.vaultPath
	app.vaultMu.RUnlock()
	if finalPath != vaultPath {
		t.Fatalf("vaultPath after init = %q, want %q", finalPath, vaultPath)
	}
	got, err := fake.Get(keyringService, chatUser)
	if err != nil || got != secret {
		t.Fatalf("keyring after init migrate: got %q err=%v, want %q", got, err, secret)
	}
	app.configMu.RLock()
	cfgKey := app.cfg.AI.Chat.APIKey
	app.configMu.RUnlock()
	if cfgKey != "" {
		t.Fatalf("config chat key should be blanked after migrate, got %q", cfgKey)
	}
	t.Cleanup(func() { _ = app.CloseVault() })
}

// TestMigrateAIKeysToKeyringLocked_UnderExclusiveLock is a narrow pin: the
// locked migrate body must complete while the caller holds vaultMu.Lock
// (public migrateAIKeysToKeyring would deadlock on RLock).
func TestMigrateAIKeysToKeyringLocked_UnderExclusiveLock(t *testing.T) {
	app := newTestApp(t)
	fake := withFakeKeyring(t, app)
	const secret = "locked-migrate-secret"
	app.configMu.Lock()
	app.cfg.AI.Chat.APIKey = secret
	app.configMu.Unlock()

	done := make(chan struct{})
	go func() {
		app.vaultMu.Lock()
		defer app.vaultMu.Unlock()
		app.migrateAIKeysToKeyringLocked()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("migrateAIKeysToKeyringLocked deadlocked under exclusive vaultMu.Lock")
	}

	got, err := fake.Get(keyringService, app.aiKeyringUser("chat"))
	if err != nil || got != secret {
		t.Fatalf("keyring after locked migrate: got %q err=%v, want %q", got, err, secret)
	}
	app.configMu.RLock()
	if app.cfg.AI.Chat.APIKey != "" {
		t.Fatalf("config key not blanked: %q", app.cfg.AI.Chat.APIKey)
	}
	app.configMu.RUnlock()
}

func TestMigrateAIKeysToKeyring_MovesPlaintext(t *testing.T) {
	app := newTestApp(t)
	fake := withFakeKeyring(t, app)
	// Seed plaintext keys in config (the pre-upgrade state).
	app.configMu.Lock()
	app.cfg.AI.Chat.APIKey = "legacy-chat"
	app.cfg.AI.Embedding.APIKey = "legacy-emb"
	app.configMu.Unlock()

	app.migrateAIKeysToKeyring()

	// Keys moved to the keyring …
	chat, err := fake.Get(keyringService, app.aiKeyringUser("chat"))
	if err != nil || chat != "legacy-chat" {
		t.Errorf("chat should have migrated to keyring, got %q err=%v", chat, err)
	}
	emb, err := fake.Get(keyringService, app.aiKeyringUser("embedding"))
	if err != nil || emb != "legacy-emb" {
		t.Errorf("embedding should have migrated to keyring, got %q err=%v", emb, err)
	}
	// … and blanked from config.
	app.configMu.RLock()
	if app.cfg.AI.Chat.APIKey != "" || app.cfg.AI.Embedding.APIKey != "" {
		t.Errorf("plaintext keys should be blanked post-migration: chat=%q emb=%q", app.cfg.AI.Chat.APIKey, app.cfg.AI.Embedding.APIKey)
	}
	app.configMu.RUnlock()
}

func TestMigrateAIKeysToKeyring_Idempotent(t *testing.T) {
	app := newTestApp(t)
	fake := withFakeKeyring(t, app)
	app.configMu.Lock()
	app.cfg.AI.Chat.APIKey = "once"
	app.configMu.Unlock()
	app.migrateAIKeysToKeyring()
	// Second run finds nothing in config → no-op, no error.
	app.migrateAIKeysToKeyring()
	chat, _ := fake.Get(keyringService, app.aiKeyringUser("chat"))
	if chat != "once" {
		t.Errorf("idempotent re-run should not lose the key, got %q", chat)
	}
}

func TestMigrateAIKeysToKeyring_NoopWhenKeyringUnavailable(t *testing.T) {
	app := newTestApp(t)
	app.keyringStore = keyring.UnavailableFake{}
	app.configMu.Lock()
	app.cfg.AI.Chat.APIKey = "stays-plaintext"
	app.configMu.Unlock()
	app.migrateAIKeysToKeyring()
	// Unavailable keyring → the plaintext key MUST stay in config.
	app.configMu.RLock()
	k := app.cfg.AI.Chat.APIKey
	app.configMu.RUnlock()
	if k != "stays-plaintext" {
		t.Errorf("unavailable keyring should leave config key intact, got %q", k)
	}
}

func TestMigrateAIKeysToKeyring_NoopWhenToggleOff(t *testing.T) {
	app := newTestApp(t)
	fake := withFakeKeyring(t, app)
	if err := app.SetUseKeyring(false); err != nil {
		t.Fatalf("SetUseKeyring: %v", err)
	}
	app.configMu.Lock()
	app.cfg.AI.Chat.APIKey = "opted-out"
	app.configMu.Unlock()
	app.migrateAIKeysToKeyring()
	app.configMu.RLock()
	k := app.cfg.AI.Chat.APIKey
	app.configMu.RUnlock()
	if k != "opted-out" {
		t.Errorf("toggle off should skip migration, got %q", k)
	}
	if _, err := fake.Get(keyringService, app.aiKeyringUser("chat")); err == nil {
		t.Errorf("toggle off should not write the keyring")
	}
}

func TestGetAIProviderConfig_HasKeySpansKeyringAndConfig(t *testing.T) {
	app := newTestApp(t)
	fake := withFakeKeyring(t, app)
	_ = fake.Set(keyringService, app.aiKeyringUser("chat"), "in-keyring")
	// Embedding key lives in config.
	app.configMu.Lock()
	app.cfg.AI.Embedding.APIKey = "in-config"
	app.configMu.Unlock()
	pub, err := app.GetAIProviderConfig()
	if err != nil {
		t.Fatalf("GetAIProviderConfig: %v", err)
	}
	if !pub.Chat.HasKey {
		t.Errorf("Chat.HasKey should be true (key in keyring)")
	}
	if !pub.Embedding.HasKey {
		t.Errorf("Embedding.HasKey should be true (key in config)")
	}
	if pub.KeyringUnusableFor != nil {
		t.Errorf("expected no unusability warning with a working keyring, got %v", pub.KeyringUnusableFor)
	}
	if !pub.KeyringAvailable {
		t.Errorf("working fake keyring should report KeyringAvailable=true")
	}
}

func TestGetAIProviderConfig_FlagsKeyringUnusable(t *testing.T) {
	app := newTestApp(t)
	app.keyringStore = keyring.UnavailableFake{}
	// A key sits in config (the fallback).
	app.configMu.Lock()
	app.cfg.AI.Chat.APIKey = "fallback"
	app.configMu.Unlock()
	pub, err := app.GetAIProviderConfig()
	if err != nil {
		t.Fatalf("GetAIProviderConfig: %v", err)
	}
	if !pub.Chat.HasKey {
		t.Errorf("Chat.HasKey should still be true via config fallback")
	}
	if pub.KeyringAvailable {
		t.Errorf("unavailable keyring should report KeyringAvailable=false")
	}
	// The page uses this to show the "keyring unavailable" warning banner.
	found := false
	for _, w := range pub.KeyringUnusableFor {
		if w == "chat" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected chat in KeyringUnusableFor, got %v", pub.KeyringUnusableFor)
	}
}
