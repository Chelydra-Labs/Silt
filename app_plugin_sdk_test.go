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
