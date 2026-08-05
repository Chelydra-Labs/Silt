package main

import (
	"os"
	"path/filepath"
	"testing"

	"silt/backend/db"
	"silt/backend/paths"
	"silt/backend/vault"
)

// TestColdStart_AvoidsDoubleProjection proves the fix for the review finding:
// on a cold start, IndexScanResultsWithProjection atomically publishes blocks
// AND projection for every file. The standalone backfill loop must NOT
// re-project those files. We verify via backfillProjectionCount: cold start
// has zero warm-skipped files → the backfill loop iterates nothing → counter
// stays 0. The projection is present (atomic batch wrote it).
func TestColdStart_AvoidsDoubleProjection(t *testing.T) {
	t.Setenv("SILT_DATA_DIR", t.TempDir())
	hostConfigDir := t.TempDir()
	t.Setenv("APPDATA", hostConfigDir)
	t.Setenv("XDG_CONFIG_HOME", hostConfigDir)

	vaultPath := t.TempDir()
	if err := vault.ScaffoldVault(vaultPath); err != nil {
		t.Fatalf("ScaffoldVault: %v", err)
	}
	bookPath := filepath.Join(vaultPath, "Books", "Dune.md")
	writeFile(t, bookPath, "---\n"+
		"notebook: \"Books\"\n"+
		"section: \"\"\n"+
		"page: \"Dune\"\n"+
		"date: \"2026-08-01\"\n"+
		"tags: []\n"+
		"type: \"book\"\n"+
		"title: \"Dune\"\n"+
		"author: \"Frank Herbert\"\n"+
		"status: \"done\"\n"+
		"---\n# Dune\n\nBody.\n")

	app := &App{spacesPerTab: 4}
	if err := app.initializeVaultServices(vaultPath); err != nil {
		t.Fatalf("initializeVaultServices: %v", err)
	}
	defer func() { _ = app.CloseVault() }()

	// Projection is present — proves the atomic batch wrote it.
	rows, err := app.QueryPagesByType("book", nil, "", false)
	if err != nil {
		t.Fatalf("QueryPagesByType: %v", err)
	}
	if len(rows) != 1 || rows[0].Page != "Dune" {
		t.Fatalf("expected 1 book page (Dune), got %d: %+v", len(rows), rows)
	}

	// The backfill loop made ZERO standalone projectPageType calls — every
	// file was "changed" on a cold start, so the atomic batch handled them
	// all and warmSkipped was empty.
	if app.backfillProjectionCount != 0 {
		t.Errorf("cold start: backfill loop made %d standalone projection calls, want 0 (atomic batch already projected)", app.backfillProjectionCount)
	}
}

// TestWarmUpgrade_BackfillProjectsWarmSkippedFiles proves the warm-upgrade
// path still works: when the files table is populated (IsFileUnchanged skips
// every file) but the page_projection_backfill marker is NOT set, the
// standalone backfill loop must project every warm-skipped file for the first
// time. The atomic batch did nothing (changed was empty).
func TestWarmUpgrade_BackfillProjectsWarmSkippedFiles(t *testing.T) {
	t.Setenv("SILT_DATA_DIR", t.TempDir())
	hostConfigDir := t.TempDir()
	t.Setenv("APPDATA", hostConfigDir)
	t.Setenv("XDG_CONFIG_HOME", hostConfigDir)

	vaultPath := t.TempDir()
	if err := vault.ScaffoldVault(vaultPath); err != nil {
		t.Fatalf("ScaffoldVault: %v", err)
	}
	// Pre-inject the block ID so the first scan does NOT rewrite the file
	// (WriteFileAtomic changes mtime → IsFileUnchanged would fail on the
	// second open, preventing the warm-skipped state this test needs).
	bookPath := filepath.Join(vaultPath, "Books", "Dune.md")
	writeFile(t, bookPath, "---\n"+
		"notebook: \"Books\"\n"+
		"section: \"\"\n"+
		"page: \"Dune\"\n"+
		"date: \"2026-08-01\"\n"+
		"tags: []\n"+
		"type: \"book\"\n"+
		"title: \"Dune\"\n"+
		"author: \"Frank Herbert\"\n"+
		"status: \"done\"\n"+
		"---\n# Dune <!-- id: aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa -->\n\nBody. <!-- id: bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb -->\n")

	// First open: cold start → populates the files table + projections +
	// records the backfill marker. Then close (keeping the index on disk).
	app1 := &App{spacesPerTab: 4}
	if err := app1.initializeVaultServices(vaultPath); err != nil {
		t.Fatalf("first initializeVaultServices: %v", err)
	}
	if err := app1.CloseVault(); err != nil {
		t.Fatalf("CloseVault: %v", err)
	}

	// Delete the projection rows but KEEP the files-table rows so
	// IsFileUnchanged returns true (warm-skipped). Also clear the backfill
	// marker so the next open thinks it needs a first-time backfill.
	indexPath, err := paths.LocalIndexPath(vaultPath)
	if err != nil {
		t.Fatalf("LocalIndexPath: %v", err)
	}
	// Use DatabaseManager (not raw sql.Open) so the WAL is checkpointed
	// on close — otherwise the next initializeVaultServices reopen may not
	// observe our manual row deletions.
	dm, err := db.NewDatabaseManager(indexPath)
	if err != nil {
		t.Fatalf("open index: %v", err)
	}
	// Clear projection rows + backfill marker to simulate the warm-upgrade
	// state: files table intact, projections absent, marker absent.
	for _, q := range []string{
		"DELETE FROM page_types",
		"DELETE FROM page_properties",
		"DELETE FROM schema_migrations WHERE name = 'page_projection_backfill'",
	} {
		if _, err := dm.SQLDB().Exec(q); err != nil {
			t.Fatalf("exec %q: %v", q, err)
		}
	}
	if err := dm.Close(); err != nil {
		t.Fatalf("close index after clearing projections: %v", err)
	}

	// Second open: warm upgrade. Every file is warm-skipped (files table
	// matches mtime+size) → changed is empty → atomic batch does nothing.
	// The backfill loop must project the warm-skipped Dune page.
	app2 := &App{spacesPerTab: 4}
	if err := app2.initializeVaultServices(vaultPath); err != nil {
		t.Fatalf("second initializeVaultServices: %v", err)
	}
	defer func() { _ = app2.CloseVault() }()

	// Projection is present — the backfill loop wrote it.
	rows, err := app2.QueryPagesByType("book", nil, "", false)
	if err != nil {
		t.Fatalf("QueryPagesByType: %v", err)
	}
	if len(rows) != 1 || rows[0].Page != "Dune" {
		t.Fatalf("warm-upgrade backfill did not project Dune: got %d rows: %+v", len(rows), rows)
	}

	// The backfill loop made ≥1 standalone projection call — proving it
	// ran for the warm-skipped file.
	if app2.backfillProjectionCount == 0 {
		t.Error("warm upgrade: backfill loop made 0 standalone projection calls — warm-skipped files were not projected")
	}
}

// TestWarmRestart_BackfillSkippedAfterMarker proves that when the backfill
// marker IS set (warm restart), the standalone pass does not re-project
// files that the atomic batch already handled.
func TestWarmRestart_BackfillSkippedAfterMarker(t *testing.T) {
	t.Setenv("SILT_DATA_DIR", t.TempDir())
	hostConfigDir := t.TempDir()
	t.Setenv("APPDATA", hostConfigDir)
	t.Setenv("XDG_CONFIG_HOME", hostConfigDir)

	vaultPath := t.TempDir()
	if err := vault.ScaffoldVault(vaultPath); err != nil {
		t.Fatalf("ScaffoldVault: %v", err)
	}
	bookPath := filepath.Join(vaultPath, "Books", "Dune.md")
	writeFile(t, bookPath, "---\n"+
		"notebook: \"Books\"\n"+
		"section: \"\"\n"+
		"page: \"Dune\"\n"+
		"date: \"2026-08-01\"\n"+
		"tags: []\n"+
		"type: \"book\"\n"+
		"title: \"Dune\"\n"+
		"---\n# Dune\n\nBody.\n")

	// First open: cold start → records the backfill marker.
	app1 := &App{spacesPerTab: 4}
	if err := app1.initializeVaultServices(vaultPath); err != nil {
		t.Fatalf("first initializeVaultServices: %v", err)
	}
	if err := app1.CloseVault(); err != nil {
		t.Fatalf("CloseVault: %v", err)
	}

	// Second open: warm restart. Marker is set → backfill pass is skipped.
	// Touch the file so it appears in `changed` (proving the atomic batch
	// handles it, not the backfill loop).
	newContent, _ := os.ReadFile(bookPath)
	if err := os.WriteFile(bookPath, append(newContent, []byte("\n\nExtra.\n")...), 0o644); err != nil {
		t.Fatalf("touch book: %v", err)
	}

	app2 := &App{spacesPerTab: 4}
	if err := app2.initializeVaultServices(vaultPath); err != nil {
		t.Fatalf("second initializeVaultServices: %v", err)
	}
	defer func() { _ = app2.CloseVault() }()

	// Projection is present (atomic batch handled the modified file).
	rows, err := app2.QueryPagesByType("book", nil, "", false)
	if err != nil {
		t.Fatalf("QueryPagesByType: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 book page, got %d", len(rows))
	}

	// Backfill loop was skipped entirely (marker was set).
	if app2.backfillProjectionCount != 0 {
		t.Errorf("warm restart: backfill loop made %d calls, want 0 (marker set → atomic batch handles everything)", app2.backfillProjectionCount)
	}
}
