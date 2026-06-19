package main

import (
	"os"
	"path/filepath"
	"testing"

	"silt/backend/config"
	"silt/backend/parser"
	"silt/backend/vault"
)

// blockPresent reports whether any block in the slice carries the given id.
func blockPresent(blocks []parser.ParsedBlock, id string) bool {
	for _, b := range blocks {
		if b.ID == id {
			return true
		}
	}
	return false
}

// newMoveTestApp scaffolds a real vault at a temp dir, writes one indexed
// page, persists settings.json pointing at it (via a controlled OS config
// dir), and initializes real on-disk services (DB + watcher) against it. The
// returned app is a faithful miniature of a running Silt, so MoveVault's
// teardown/reinit path is exercised end-to-end. ctx is left nil so the
// (guarded) event emission is skipped in tests.
func newMoveTestApp(t *testing.T) (*App, string) {
	t.Helper()

	// Control where settings.json lands so we can assert it after a move.
	settingsDir := t.TempDir()
	t.Setenv("APPDATA", settingsDir)
	t.Setenv("XDG_CONFIG_HOME", settingsDir)

	src := t.TempDir()
	if err := vault.ScaffoldVault(src); err != nil {
		t.Fatalf("ScaffoldVault: %v", err)
	}
	// A page with one task block whose path resolves to notebook=Work,
	// section="", page=Inbox (the scanner derives these from the path).
	writeFile(t, filepath.Join(src, "Work", "Inbox.md"),
		"# Inbox\n"+
			"- [ ] do a thing [owner:: Chris] <!-- id: 22222222-2222-2222-2222-222222222222 -->\n")

	if err := vault.SaveSettings(&vault.AppSettings{
		VaultPath:   src,
		ActiveTheme: "cyber_forest",
		ThemeMode:   "dark",
	}); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}

	app := &App{spacesPerTab: 4}
	if err := app.initializeVaultServices(src); err != nil {
		t.Fatalf("initializeVaultServices: %v", err)
	}
	t.Cleanup(func() { _ = app.CloseVault() })
	return app, src
}

func TestMoveVault_HappyPath(t *testing.T) {
	app, src := newMoveTestApp(t)

	blocksBefore, err := app.FetchPageBlocks("Work", "", "Inbox")
	if err != nil {
		t.Fatalf("pre-move FetchPageBlocks: %v", err)
	}
	// The fixture has a header + one task; locate the task by its stable id.
	taskID := "22222222-2222-2222-2222-222222222222"
	if !blockPresent(blocksBefore, taskID) {
		t.Fatalf("pre-move: expected task %s in indexed blocks %v", taskID, blocksBefore)
	}

	dest := filepath.Join(t.TempDir(), "moved")
	res, err := app.MoveVault(dest, false)
	if err != nil {
		t.Fatalf("MoveVault: %v", err)
	}
	if res.From != src {
		t.Errorf("From = %q, want %q", res.From, src)
	}
	if res.To != dest {
		t.Errorf("To = %q, want %q", res.To, dest)
	}
	if res.FilesCopied == 0 {
		t.Error("FilesCopied should be > 0")
	}
	if !res.SkippedIndex {
		t.Error("SkippedIndex should be true (index artifacts were present at source)")
	}
	if res.RemoveOldErr != "" {
		t.Errorf("RemoveOldErr should be empty for removeOld=false, got %q", res.RemoveOldErr)
	}

	// settings.json now points at dest, theme/mode preserved.
	s, err := vault.LoadSettings()
	if err != nil {
		t.Fatalf("post-move LoadSettings: %v", err)
	}
	if s.VaultPath != dest {
		t.Errorf("settings vault_path = %q, want %q", s.VaultPath, dest)
	}
	if s.ActiveTheme != "cyber_forest" || s.ThemeMode != "dark" {
		t.Errorf("theme/mode not preserved: theme=%q mode=%q", s.ActiveTheme, s.ThemeMode)
	}

	// App now serves from dest.
	if app.vaultPath != dest {
		t.Errorf("app.vaultPath = %q, want %q", app.vaultPath, dest)
	}
	if !app.IsVaultInitialized() {
		t.Error("IsVaultInitialized should be true after a successful move")
	}

	// Same content served from the new location, block identity preserved.
	blocksAfter, err := app.FetchPageBlocks("Work", "", "Inbox")
	if err != nil {
		t.Fatalf("post-move FetchPageBlocks: %v", err)
	}
	if !blockPresent(blocksAfter, taskID) {
		t.Errorf("post-move: task %s missing from blocks %v", taskID, blocksAfter)
	}

	// A fresh index was built at dest (not copied from src).
	if _, err := os.Stat(filepath.Join(dest, ".system", "index.sqlite")); err != nil {
		t.Errorf("dest should have a freshly-built index.sqlite: %v", err)
	}
	// The original is untouched because removeOld=false.
	if _, err := os.Stat(filepath.Join(src, ".system", "index.sqlite")); err != nil {
		t.Errorf("src index.sqlite should still exist (removeOld=false): %v", err)
	}

	// dest config.yaml notebooks.path was rewritten to the new location so the
	// Settings → General workspace row shows it.
	destCfg, err := config.Load(dest)
	if err != nil {
		t.Fatalf("config.Load(dest): %v", err)
	}
	if destCfg.Notebooks.Path != filepath.ToSlash(dest) {
		t.Errorf("dest config notebooks.path = %q, want %q", destCfg.Notebooks.Path, filepath.ToSlash(dest))
	}
}

func TestMoveVault_RemoveOldDeletesSource(t *testing.T) {
	app, src := newMoveTestApp(t)

	dest := filepath.Join(t.TempDir(), "moved")
	res, err := app.MoveVault(dest, true)
	if err != nil {
		t.Fatalf("MoveVault(removeOld=true): %v", err)
	}
	if res.RemoveOldErr != "" {
		t.Errorf("RemoveOldErr should be empty on a successful delete, got %q", res.RemoveOldErr)
	}
	// Original vault folder is gone; the active vault is the new one.
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Errorf("expected src to be removed, stat err = %v", err)
	}
	if app.vaultPath != dest {
		t.Errorf("app.vaultPath = %q, want %q", app.vaultPath, dest)
	}
}

func TestMoveVault_RejectsExistingVaultDestination(t *testing.T) {
	app, src := newMoveTestApp(t)

	// Destination already looks like a vault (has a .system).
	dest := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dest, ".system"), 0755); err != nil {
		t.Fatal(err)
	}

	_, err := app.MoveVault(dest, false)
	if err == nil {
		t.Fatal("expected MoveVault to reject a destination that is already a vault, got nil")
	}

	// Active vault is untouched: services still up, settings unchanged, content
	// still served from src.
	if app.vaultPath != src {
		t.Errorf("app.vaultPath = %q, want %q (should be unchanged)", app.vaultPath, src)
	}
	s, _ := vault.LoadSettings()
	if s.VaultPath != src {
		t.Errorf("settings vault_path = %q, want %q (should be unchanged)", s.VaultPath, src)
	}
	if _, ferr := app.FetchPageBlocks("Work", "", "Inbox"); ferr != nil {
		t.Errorf("FetchPageBlocks should still work after a rejected move, got %v", ferr)
	}
}

func TestMoveVault_RejectsWhenNoVaultOpen(t *testing.T) {
	app := &App{spacesPerTab: 4} // no vault initialized
	if _, err := app.MoveVault("/tmp/whatever", false); err == nil {
		t.Fatal("expected error when no vault is open, got nil")
	}
}

func TestCopyVault_LeavesActiveVaultUntouched(t *testing.T) {
	app, src := newMoveTestApp(t)

	dest := filepath.Join(t.TempDir(), "copy")
	res, err := app.CopyVault(dest)
	if err != nil {
		t.Fatalf("CopyVault: %v", err)
	}
	if res.FilesCopied == 0 {
		t.Error("FilesCopied should be > 0")
	}
	if !res.SkippedIndex {
		t.Error("SkippedIndex should be true")
	}

	// Active vault is unchanged.
	if app.vaultPath != src {
		t.Errorf("app.vaultPath = %q, want %q (copy must not switch the active vault)", app.vaultPath, src)
	}
	s, _ := vault.LoadSettings()
	if s.VaultPath != src {
		t.Errorf("settings vault_path = %q, want %q (copy must not change settings)", s.VaultPath, src)
	}

	// Content was duplicated at dest (index excluded — rebuilt on first open).
	if _, err := os.Stat(filepath.Join(dest, "Work", "Inbox.md")); err != nil {
		t.Errorf("copied page missing at dest: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, ".system", "config.yaml")); err != nil {
		t.Errorf("copied config.yaml missing at dest: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, ".system", "index.sqlite")); err == nil {
		t.Error("dest should NOT have an index.sqlite (it is rebuilt on first open)")
	}

	// The active vault still serves its content.
	if _, ferr := app.FetchPageBlocks("Work", "", "Inbox"); ferr != nil {
		t.Errorf("FetchPageBlocks should still work after a copy, got %v", ferr)
	}
}

func TestCopyVault_RejectsWhenNoVaultOpen(t *testing.T) {
	app := &App{spacesPerTab: 4}
	if _, err := app.CopyVault("/tmp/whatever"); err == nil {
		t.Fatal("expected error when no vault is open, got nil")
	}
}
