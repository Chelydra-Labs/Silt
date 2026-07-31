package db

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"silt/backend/parser"
)

// TestPageFold_BackfillFillsNullAndEnablesLeafLookup mirrors a warm vault
// upgraded with NULL page_fold rows before the one-shot migration (#844).
func TestPageFold_BackfillFillsNullAndEnablesLeafLookup(t *testing.T) {
	dm := newTestDB(t)
	_, err := dm.SQLDB().Exec(
		`INSERT INTO blocks (id, parent_id, source, notebook, section, page, file_date, depth, type, raw_content, clean_content, line_number)
		 VALUES ('fold-bf-1', NULL, 'vault', 'NB', 'Sec', 'Café', '2026-07-20', 0, 'NOTE', 'body', 'body', 1)`,
	)
	if err != nil {
		t.Fatalf("seed block: %v", err)
	}
	if _, err := dm.SQLDB().Exec("UPDATE blocks SET page_fold = NULL"); err != nil {
		t.Fatalf("null page_fold: %v", err)
	}
	if _, err := dm.SQLDB().Exec("DELETE FROM schema_migrations WHERE name = ?", pageFoldBackfillMarker); err != nil {
		t.Fatalf("clear marker: %v", err)
	}
	if err := backfillPageFold(dm.SQLDB()); err != nil {
		t.Fatalf("backfillPageFold: %v", err)
	}
	if !migrationMarkerApplied(t, dm, pageFoldBackfillMarker) {
		t.Fatal("expected page_fold_backfill marker")
	}
	var fold sql.NullString
	if err := dm.SQLDB().QueryRow(`SELECT page_fold FROM blocks WHERE id = 'fold-bf-1'`).Scan(&fold); err != nil {
		t.Fatalf("read fold: %v", err)
	}
	want := pageFoldKey("Café")
	if !fold.Valid || fold.String != want {
		t.Fatalf("page_fold = %v, want %q", fold, want)
	}
	pages, err := dm.ListPagesByLeaf("CAFÉ")
	if err != nil {
		t.Fatalf("ListPagesByLeaf: %v", err)
	}
	if len(pages) != 1 || pages[0].Page != "Café" {
		t.Fatalf("leaf after backfill: %+v", pages)
	}
	if err := backfillPageFold(dm.SQLDB()); err != nil {
		t.Fatalf("second backfill: %v", err)
	}
}

// TestPageFold_HealsNullAfterMarker ensures residual NULL folds are repaired
// even when the one-shot migration marker is already present.
func TestPageFold_HealsNullAfterMarker(t *testing.T) {
	dm := newTestDB(t)
	if !migrationMarkerApplied(t, dm, pageFoldBackfillMarker) {
		t.Fatal("fresh DB should already have page_fold marker")
	}
	_, err := dm.SQLDB().Exec(
		`INSERT INTO blocks (id, parent_id, source, notebook, section, page, file_date, depth, type, raw_content, clean_content, line_number)
		 VALUES ('fold-heal-1', NULL, 'vault', 'NB', 'Sec', 'Healed', '2026-07-20', 0, 'NOTE', 'body', 'body', 1)`,
	)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := dm.SQLDB().Exec(`UPDATE blocks SET page_fold = NULL WHERE id = 'fold-heal-1'`); err != nil {
		t.Fatalf("null fold: %v", err)
	}
	if err := backfillPageFold(dm.SQLDB()); err != nil {
		t.Fatalf("heal: %v", err)
	}
	var fold sql.NullString
	if err := dm.SQLDB().QueryRow(`SELECT page_fold FROM blocks WHERE id = 'fold-heal-1'`).Scan(&fold); err != nil {
		t.Fatalf("read: %v", err)
	}
	if !fold.Valid || fold.String != pageFoldKey("Healed") {
		t.Fatalf("page_fold = %v, want %q", fold, pageFoldKey("Healed"))
	}
	pages, err := dm.ListPagesByLeaf("healed")
	if err != nil || len(pages) != 1 {
		t.Fatalf("leaf after heal: pages=%+v err=%v", pages, err)
	}
}

// TestPageFold_WarmReopenWithFTSDoesNotBreakSearch exercises the upgrade path
// where FTS triggers already exist on disk before page_fold backfill runs.
func TestPageFold_WarmReopenWithFTSDoesNotBreakSearch(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "index.sqlite")

	// First open: create schema + FTS + seed content with NULL page_fold.
	dm1, err := NewDatabaseManager(dbPath)
	if err != nil {
		t.Fatalf("open1: %v", err)
	}
	id := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	if err := dm1.IndexFileBlocks("vault", "NB", "Sec", "SearchMe", []parser.ParsedBlock{
		{
			ID: id, Type: parser.BlockNote, Depth: 0,
			RawText: "unique fts token xyzzy", CleanText: "unique fts token xyzzy",
		},
	}, nil); err != nil {
		t.Fatalf("index: %v", err)
	}
	// Force NULL folds + clear marker to simulate pre-#844 on-disk state.
	if _, err := dm1.SQLDB().Exec(`UPDATE blocks SET page_fold = NULL`); err != nil {
		t.Fatalf("null folds: %v", err)
	}
	if _, err := dm1.SQLDB().Exec(`DELETE FROM schema_migrations WHERE name = ?`, pageFoldBackfillMarker); err != nil {
		t.Fatalf("clear marker: %v", err)
	}
	// Confirm FTS triggers exist (warm vault).
	var trig int
	if err := dm1.SQLDB().QueryRow(
		`SELECT count(*) FROM sqlite_master WHERE type='trigger' AND name='blocks_fts_au'`,
	).Scan(&trig); err != nil || trig != 1 {
		t.Fatalf("expected blocks_fts_au trigger, got %d err=%v", trig, err)
	}
	if err := dm1.Close(); err != nil {
		t.Fatalf("close1: %v", err)
	}

	// Second open runs initSchema → backfillPageFold with live FTS triggers.
	dm2, err := NewDatabaseManager(dbPath)
	if err != nil {
		t.Fatalf("open2: %v", err)
	}
	t.Cleanup(func() { _ = dm2.Close() })

	var nulls int
	if err := dm2.SQLDB().QueryRow(`SELECT COUNT(*) FROM blocks WHERE page_fold IS NULL`).Scan(&nulls); err != nil {
		t.Fatalf("count nulls: %v", err)
	}
	if nulls != 0 {
		t.Fatalf("after reopen backfill: %d null page_fold rows", nulls)
	}
	if !migrationMarkerApplied(t, dm2, pageFoldBackfillMarker) {
		t.Fatal("marker missing after reopen")
	}
	// FTS triggers restored.
	if err := dm2.SQLDB().QueryRow(
		`SELECT count(*) FROM sqlite_master WHERE type='trigger' AND name='blocks_fts_au'`,
	).Scan(&trig); err != nil || trig != 1 {
		t.Fatalf("blocks_fts_au should be restored, got %d err=%v", trig, err)
	}
	// Leaf lookup works.
	pages, err := dm2.ListPagesByLeaf("searchme")
	if err != nil || len(pages) != 1 {
		t.Fatalf("leaf: %+v err=%v", pages, err)
	}
	// FTS search still finds content.
	hits, err := dm2.SearchBlocks("xyzzy")
	if err != nil {
		t.Fatalf("SearchBlocks: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("FTS search returned no hits after page_fold backfill")
	}
	// On-disk file still present (sanity).
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("db file: %v", err)
	}
}
