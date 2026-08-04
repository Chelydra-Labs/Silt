package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"silt/backend/db"
	"silt/backend/paths"
	"silt/backend/vault"
)

// TestAC5_ColdStartRebuildsTypeProjections is the app-layer evidence for AC5:
// "delete the index + relaunch rebuilds all type projections." The DB-layer
// byte-identical evidence is TestProjection_ReproducibleAcrossReopen in
// backend/db; this test exercises the real scan→IndexScanResults→projectPageType
// loop in initializeVaultServices (the path every cold vault open takes), which
// no other test exercises end-to-end (newTestApp bypasses it with an in-memory
// DB + a direct db.NewDatabaseManager handle).
//
// Scope: the test drives the real startup path against a scaffolded vault that
// has a typed page on disk and a matching book.yaml type def under .system/types.
// It then deletes the relocated index, re-runs initializeVaultServices, and
// asserts QueryPagesByType returns the projection repopulated purely from the
// on-disk frontmatter (cardinal rule 4: the projection is disposable).
func TestAC5_ColdStartRebuildsTypeProjections(t *testing.T) {
	// Isolate the relocated (out-of-vault) index DataDir so each test gets a
	// throwaway path — the same isolation newTestApp applies.
	t.Setenv("SILT_DATA_DIR", t.TempDir())
	// Isolate the per-host grants/settings files so the test never touches the
	// developer's real config dir (mirrors newTestApp's host-config isolation).
	hostConfigDir := t.TempDir()
	t.Setenv("APPDATA", hostConfigDir)
	t.Setenv("XDG_CONFIG_HOME", hostConfigDir)

	vaultPath := t.TempDir()
	if err := vault.ScaffoldVault(vaultPath); err != nil {
		t.Fatalf("ScaffoldVault: %v", err)
	}
	// ScaffoldVault seeds .system/types/book.yaml (status select, rating
	// number, etc.); write one typed Book page that sets status and rating.
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
		"rating: 5\n"+
		"---\n# Dune\n\nBody.\n")

	// First cold start: drives ScanWorkspace → IndexScanResults → the
	// post-index projectPageType loop in initializeVaultServices.
	app := &App{spacesPerTab: 4}
	if err := app.initializeVaultServices(vaultPath); err != nil {
		t.Fatalf("first initializeVaultServices: %v", err)
	}
	defer func() { _ = app.CloseVault() }()

	assertBookProjection := func(a *App, label string) {
		t.Helper()
		rows, err := a.QueryPagesByType("book", nil, "", false)
		if err != nil {
			t.Fatalf("%s: QueryPagesByType: %v", label, err)
		}
		if len(rows) != 1 {
			t.Fatalf("%s: expected 1 book page after cold start, got %d (%+v)", label, len(rows), rows)
		}
		row := rows[0]
		if row.Page != "Dune" || row.Notebook != "Books" {
			t.Errorf("%s: page = %s/%s, want Books/Dune", label, row.Notebook, row.Page)
		}
		// Values must be the projected coercion of the on-disk frontmatter.
		propVals := map[string]string{}
		for _, p := range row.Properties {
			propVals[p.Name] = p.ValueText
		}
		if propVals["status"] != "done" {
			t.Errorf("%s: status = %q, want done", label, propVals["status"])
		}
		if propVals["rating"] != "5" {
			t.Errorf("%s: rating = %q, want 5", label, propVals["rating"])
		}
		if propVals["author"] != "Frank Herbert" {
			t.Errorf("%s: author = %q, want Frank Herbert", label, propVals["author"])
		}
	}
	assertBookProjection(app, "after first cold start")

	// Close + delete every sidecar the WAL connection may have created so the
	// reopen is a true cold rebuild, not a warm restart. The relocated index
	// lives outside the vault (paths.LocalIndexPath resolves against
	// SILT_DATA_DIR), so deleting it mirrors a user wiping the index file.
	indexPath, err := paths.LocalIndexPath(vaultPath)
	if err != nil {
		t.Fatalf("LocalIndexPath: %v", err)
	}
	if err := app.CloseVault(); err != nil {
		t.Fatalf("CloseVault: %v", err)
	}
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		if err := os.Remove(indexPath + suffix); err != nil && !os.IsNotExist(err) {
			t.Fatalf("remove %s: %v", indexPath+suffix, err)
		}
	}

	// Second cold start with an empty index: initializeVaultServices must
	// re-derive the page_types/page_properties projection purely from the
	// on-disk frontmatter + the book.yaml schema. This is the AC5 contract.
	app2 := &App{spacesPerTab: 4}
	if err := app2.initializeVaultServices(vaultPath); err != nil {
		t.Fatalf("second initializeVaultServices: %v", err)
	}
	defer func() { _ = app2.CloseVault() }()
	assertBookProjection(app2, "after cold rebuild from empty index")

	// Belt-and-braces: the raw page_types/page_properties rows exist on disk.
	for _, table := range []string{"page_types", "page_properties"} {
		var n int
		if err := app2.db.SQLDB().QueryRow(
			"SELECT COUNT(*) FROM "+table+" WHERE source = ? AND notebook = ? AND section = ? AND page = ?",
			"vault", "Books", "", "Dune",
		).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if n == 0 {
			t.Errorf("expected %s rows for Books/Dune after cold rebuild, got 0", table)
		}
	}
}

// TestAC5_ColdStartClearsTypeWhenFrontmatterDropped pins the symmetric half of
// the cold-start reprojection contract: a vault reopened after the user removed
// a `type:` line from a page's frontmatter must NOT carry a stale projection
// for that page. The cold-start loop calls projectPageType for every scanned
// file, and projectPageType clears the projection when meta.Type is empty — so
// an external edit that strips a type is reflected without a restart or watcher.
func TestAC5_ColdStartClearsTypeWhenFrontmatterDropped(t *testing.T) {
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

	app := &App{spacesPerTab: 4}
	if err := app.initializeVaultServices(vaultPath); err != nil {
		t.Fatalf("first initializeVaultServices: %v", err)
	}
	if rows, _ := app.QueryPagesByType("book", nil, "", false); len(rows) != 1 {
		t.Fatalf("expected 1 book page after first cold start, got %d", len(rows))
	}
	if err := app.CloseVault(); err != nil {
		t.Fatalf("CloseVault: %v", err)
	}

	// Strip the type line and re-open cold. projectPageType must clear the
	// projection so the page drops off the Book dashboard immediately.
	raw, err := os.ReadFile(bookPath)
	if err != nil {
		t.Fatalf("read book: %v", err)
	}
	raw = []byte(strings.ReplaceAll(string(raw), "type: \"book\"\n", ""))
	if err := os.WriteFile(bookPath, raw, 0o644); err != nil {
		t.Fatalf("rewrite book: %v", err)
	}

	app2 := &App{spacesPerTab: 4}
	if err := app2.initializeVaultServices(vaultPath); err != nil {
		t.Fatalf("second initializeVaultServices: %v", err)
	}
	defer func() { _ = app2.CloseVault() }()
	rows, err := app2.QueryPagesByType("book", nil, "", false)
	if err != nil {
		t.Fatalf("QueryPagesByType: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("expected 0 book pages after type stripped from frontmatter, got %d (%+v)", len(rows), rows)
	}
}

// TestWarmUpgrade_ProjectsPreExistingTypedPages pins the 0.3.x → 0.4.0 path:
// the files table already has mtime+size rows (so IsFileUnchanged skips every
// page) while page_types/page_properties are empty and the
// page_projection_backfill marker is absent. initializeVaultServices must
// still project hand-authored `type:` pages on first miss of the marker.
func TestWarmUpgrade_ProjectsPreExistingTypedPages(t *testing.T) {
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
		"rating: 5\n"+
		"---\n# Dune\n\nBody.\n")

	app := &App{spacesPerTab: 4}
	if err := app.initializeVaultServices(vaultPath); err != nil {
		t.Fatalf("first initializeVaultServices: %v", err)
	}
	if rows, _ := app.QueryPagesByType("book", nil, "", false); len(rows) != 1 {
		t.Fatalf("expected 1 book page after first open, got %d", len(rows))
	}
	// Confirm the one-shot marker was recorded on the cold path too.
	applied, err := app.db.SchemaMigrationApplied(db.PageProjectionBackfillMarker)
	if err != nil || !applied {
		t.Fatalf("expected page_projection_backfill marker after first open (applied=%v err=%v)", applied, err)
	}
	if err := app.CloseVault(); err != nil {
		t.Fatalf("CloseVault: %v", err)
	}

	// Simulate a warm 0.3→0.4 index: files table intact (unchanged gate will
	// skip the page), projection tables empty, backfill marker absent.
	app2 := &App{spacesPerTab: 4}
	if err := app2.initializeVaultServices(vaultPath); err != nil {
		t.Fatalf("warm reopen before wipe: %v", err)
	}
	if _, err := app2.db.SQLDB().Exec(
		"DELETE FROM schema_migrations WHERE name = ?", db.PageProjectionBackfillMarker,
	); err != nil {
		t.Fatalf("drop backfill marker: %v", err)
	}
	for _, table := range []string{"page_types", "page_properties"} {
		if _, err := app2.db.SQLDB().Exec("DELETE FROM " + table); err != nil {
			t.Fatalf("clear %s: %v", table, err)
		}
	}
	if err := app2.CloseVault(); err != nil {
		t.Fatalf("CloseVault after wipe: %v", err)
	}

	// Warm open: files table still has the page row → changed is empty for
	// Dune.md, but missing marker must force a full-results projection.
	app3 := &App{spacesPerTab: 4}
	if err := app3.initializeVaultServices(vaultPath); err != nil {
		t.Fatalf("warm upgrade initializeVaultServices: %v", err)
	}
	defer func() { _ = app3.CloseVault() }()

	rows, err := app3.QueryPagesByType("book", nil, "", false)
	if err != nil {
		t.Fatalf("QueryPagesByType after warm upgrade: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("warm upgrade should project pre-existing typed page, got %d rows (%+v)", len(rows), rows)
	}
	if rows[0].Page != "Dune" {
		t.Errorf("page = %q, want Dune", rows[0].Page)
	}
	applied, err = app3.db.SchemaMigrationApplied(db.PageProjectionBackfillMarker)
	if err != nil || !applied {
		t.Fatalf("expected page_projection_backfill marker after warm upgrade (applied=%v err=%v)", applied, err)
	}
}
