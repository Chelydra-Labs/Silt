package main

import (
	"testing"
	"time"

	"silt/backend/config"
	"silt/backend/mcp"
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

// TestSyncMCPHostLocked_NoDeadlockUnderExclusiveVaultMu reproduces the
// initializeVaultServices path: exclusive vaultMu.Lock + MCP enabled must
// complete without bridge.VaultPath re-entering vaultMu.RLock.
func TestSyncMCPHostLocked_NoDeadlockUnderExclusiveVaultMu(t *testing.T) {
	app := newTestApp(t)
	app.cfg.AI.LocalMCP = mcp.NormalizeConfig(mcp.Config{
		Enabled:     true,
		HTTPEnabled: true,
		HTTPPort:    0, // will normalize; Start may fail bind — must not hang
	})
	// Force a free port via config after normalize in Start.
	app.cfg.AI.LocalMCP.HTTPPort = 0
	app.cfg.AI.LocalMCP.HTTPEnabled = true
	app.cfg.AI.LocalMCP.Enabled = true

	done := make(chan struct{})
	go func() {
		app.vaultMu.Lock()
		app.syncMCPHostLocked()
		app.vaultMu.Unlock()
		close(done)
	}()
	select {
	case <-done:
		// success — no deadlock
	case <-time.After(3 * time.Second):
		t.Fatal("syncMCPHostLocked deadlocked under vaultMu.Lock (bridge.VaultPath RLock)")
	}
	// Cleanup host if it started.
	app.stopMCPHost()
}
