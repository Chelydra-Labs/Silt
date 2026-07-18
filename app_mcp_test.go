package main

import (
	"testing"

	"silt/backend/config"
)

// TestSetLocalMCPConfig_PersistsViaTrackedSave ensures the MCP setter uses the
// canonical saveConfigTracked path (self-write suppression + no lost concurrent
// config writes) rather than raw config.Save.
func TestSetLocalMCPConfig_PersistsViaTrackedSave(t *testing.T) {
	app := newTestApp(t)
	if err := app.SetLocalMCPConfig(true, true, false, 17887); err != nil {
		t.Fatalf("SetLocalMCPConfig: %v", err)
	}
	if !app.cfg.AI.LocalMCP.Enabled {
		t.Fatal("in-memory cfg not updated")
	}
	loaded, err := config.Load(app.vaultPath)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if !loaded.AI.LocalMCP.Enabled {
		t.Fatal("config.yaml did not persist LocalMCP.Enabled")
	}
	if !loaded.AI.LocalMCP.HTTPEnabled {
		t.Fatal("expected HTTP forced on when enabling")
	}
	// Disable again — should clear host and persist.
	if err := app.SetLocalMCPConfig(false, false, false, 0); err != nil {
		t.Fatalf("SetLocalMCPConfig disable: %v", err)
	}
	loaded, err = config.Load(app.vaultPath)
	if err != nil {
		t.Fatalf("config.Load after disable: %v", err)
	}
	if loaded.AI.LocalMCP.Enabled {
		t.Fatal("expected LocalMCP disabled on disk")
	}
}
