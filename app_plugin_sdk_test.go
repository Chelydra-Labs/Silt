package main

import (
	"testing"

	"silt/backend/config"
)

// TestSetOpenDevtoolsOnStartup covers the #363 atomic Dev Mode toggle: the
// setter round-trips the bool through config.yaml and a fresh Load sees the
// persisted value, mirroring the atomic config-RMW path the other single-field
// setters (SetShowFormatToolbar / SetFocusMode / SetTypewriterMode) use.
func TestSetOpenDevtoolsOnStartup(t *testing.T) {
	app := newTestApp(t)

	// Default is normalized to a non-nil false (config.normalize).
	cfg, err := config.Load(app.vaultPath)
	if err != nil {
		t.Fatalf("initial config.Load: %v", err)
	}
	if cfg.UI.OpenDevtoolsOnStartup == nil || *cfg.UI.OpenDevtoolsOnStartup {
		t.Errorf("expected OpenDevtoolsOnStartup default false, got %v", cfg.UI.OpenDevtoolsOnStartup)
	}

	// Flip to true.
	if err := app.SetOpenDevtoolsOnStartup(true); err != nil {
		t.Fatalf("SetOpenDevtoolsOnStartup(true): %v", err)
	}
	loaded, err := config.Load(app.vaultPath)
	if err != nil {
		t.Fatalf("config.Load after true: %v", err)
	}
	if loaded.UI.OpenDevtoolsOnStartup == nil || !*loaded.UI.OpenDevtoolsOnStartup {
		t.Errorf("expected persisted OpenDevtoolsOnStartup true, got %v", loaded.UI.OpenDevtoolsOnStartup)
	}

	// Flip back to false.
	if err := app.SetOpenDevtoolsOnStartup(false); err != nil {
		t.Fatalf("SetOpenDevtoolsOnStartup(false): %v", err)
	}
	loaded, err = config.Load(app.vaultPath)
	if err != nil {
		t.Fatalf("config.Load after false: %v", err)
	}
	if loaded.UI.OpenDevtoolsOnStartup == nil || *loaded.UI.OpenDevtoolsOnStartup {
		t.Errorf("expected persisted OpenDevtoolsOnStartup false, got %v", loaded.UI.OpenDevtoolsOnStartup)
	}
}

// TestOpenDevTools_GatedOnDevMode covers #679: OpenDevTools is a no-op when
// Dev Mode is off, and does not panic when mainWindow is nil (unit harness).
func TestOpenDevTools_GatedOnDevMode(t *testing.T) {
	app := newTestApp(t)

	// Flag off (default) → no error, no panic with nil mainWindow.
	if err := app.OpenDevTools(); err != nil {
		t.Fatalf("OpenDevTools with flag off: %v", err)
	}
	if app.devToolsRuntimeEnabled() {
		t.Fatal("devToolsRuntimeEnabled should be false with flag off")
	}

	if err := app.SetOpenDevtoolsOnStartup(true); err != nil {
		t.Fatalf("SetOpenDevtoolsOnStartup(true): %v", err)
	}
	if !app.devToolsRuntimeEnabled() {
		t.Fatal("devToolsRuntimeEnabled should be true with Dev Mode on")
	}
	// Flag on, mainWindow still nil → still no panic.
	if err := app.OpenDevTools(); err != nil {
		t.Fatalf("OpenDevTools with flag on, nil window: %v", err)
	}
}

// TestOpenDevTools_SILT_DEBUG covers runtime parity with launch-time
// shouldOpenDevtools: SILT_DEBUG=1 enables OpenDevTools without the vault flag.
func TestOpenDevTools_SILT_DEBUG(t *testing.T) {
	app := newTestApp(t)
	t.Setenv("SILT_DEBUG", "1")
	if !app.devToolsRuntimeEnabled() {
		t.Fatal("SILT_DEBUG=1 should enable runtime DevTools even with flag off")
	}
	if err := app.OpenDevTools(); err != nil {
		t.Fatalf("OpenDevTools with SILT_DEBUG: %v", err)
	}
}

// TestOpenDevTools_NoVault is a silent no-op (not an error) when no vault is open.
func TestOpenDevTools_NoVault(t *testing.T) {
	app := newTestApp(t)
	app.vaultMu.Lock()
	app.vaultPath = ""
	app.vaultMu.Unlock()
	if err := app.OpenDevTools(); err != nil {
		t.Fatalf("OpenDevTools with empty vaultPath: %v", err)
	}
}
