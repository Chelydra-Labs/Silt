package db

import (
	"path/filepath"
	"testing"

	"silt/backend/parser"
)

// The page-inventory projection: page_core is the one-row-per-indexed-file
// answer to "which pages exist", including zero-block files. These tests pin
// the write path guarantees, the ListDistinctPages swap, and the warm-reopen
// backfill that upgrades vaults indexed before every path guaranteed a row.

func TestListDistinctPages_ProjectionEquivalence(t *testing.T) {
	dm := newTestDB(t)

	// A block-carrying page (block-only index path — identity upsert must
	// still register it), an empty page (projection path only), and a linked
	// source page.
	blocks := []parser.ParsedBlock{
		{ID: "bbbb1111-0000-4000-8000-000000000001", Type: parser.BlockNote,
			RawText: "body", CleanText: "body", LineNumber: 1},
	}
	if err := dm.IndexFileBlocks("vault", "Work", "Ops", "Runbook", blocks, nil); err != nil {
		t.Fatalf("index runbook: %v", err)
	}
	if err := dm.IndexFileWithProjection("vault", "Work", "Ops", "Scratchpad",
		nil, nil, "", nil, PageCoreFields{}); err != nil {
		t.Fatalf("index scratchpad: %v", err)
	}
	linked := []parser.ParsedBlock{
		{ID: "bbbb1111-0000-4000-8000-000000000002", Type: parser.BlockNote,
			RawText: "linked body", CleanText: "linked body", LineNumber: 1},
	}
	if err := dm.IndexFileBlocks("linked:x", "Work", "", "Shared Doc", linked, nil); err != nil {
		t.Fatalf("index linked: %v", err)
	}

	pages, err := dm.ListDistinctPages()
	if err != nil {
		t.Fatalf("ListDistinctPages: %v", err)
	}
	want := map[string]bool{
		"vault|Work|Ops|Runbook":    true,
		"vault|Work|Ops|Scratchpad": true,
		"linked:x|Work||Shared Doc": true,
	}
	got := map[string]bool{}
	for _, p := range pages {
		got[p.Source+"|"+p.Notebook+"|"+p.Section+"|"+p.Page] = true
	}
	for k := range want {
		if !got[k] {
			t.Errorf("inventory missing %q: got %v", k, got)
		}
	}
	if len(got) != len(want) {
		t.Errorf("inventory has unexpected extra entries: %v", got)
	}

	// Ordering contract: (notebook, section, page) — the shape the previous
	// SELECT DISTINCT ... FROM blocks produced and every caller sorts against.
	for i := 1; i < len(pages); i++ {
		a, b := pages[i-1], pages[i]
		if a.Notebook > b.Notebook ||
			(a.Notebook == b.Notebook && a.Section > b.Section) ||
			(a.Notebook == b.Notebook && a.Section == b.Section && a.Page > b.Page) {
			t.Errorf("inventory not ordered by (notebook, section, page): %+v before %+v", a, b)
		}
	}

	// The delete path drops the identity with everything else — a deleted
	// page must stop resolving.
	if err := dm.ClearFileBlocks(nil, "vault", "Work", "Ops", "Runbook"); err != nil {
		t.Fatalf("ClearFileBlocks: %v", err)
	}
	pages, err = dm.ListDistinctPages()
	if err != nil {
		t.Fatalf("ListDistinctPages: %v", err)
	}
	for _, p := range pages {
		if p.Page == "Runbook" {
			t.Errorf("deleted page still in inventory: %+v", p)
		}
	}

	// Source unlink drops every page of the source, zero-block ones included.
	if err := dm.ClearSourceBlocks("linked:x"); err != nil {
		t.Fatalf("ClearSourceBlocks: %v", err)
	}
	pages, err = dm.ListDistinctPages()
	if err != nil {
		t.Fatalf("ListDistinctPages: %v", err)
	}
	for _, p := range pages {
		if p.Source == "linked:x" {
			t.Errorf("unlinked source still in inventory: %+v", p)
		}
	}
}

func TestListDistinctPages_EmptyPageSurvivesBlockOnlyReindex(t *testing.T) {
	dm := newTestDB(t)

	// A page that HAS blocks loses them all on reindex (the file's content
	// became unmanaged-only). The reindex path (ClearFileBlocks with tx !=
	// nil) must NOT drop the identity row — the file still exists on disk.
	blocks := []parser.ParsedBlock{
		{ID: "cccc1111-0000-4000-8000-000000000001", Type: parser.BlockNote,
			RawText: "vanishing", CleanText: "vanishing", LineNumber: 1},
	}
	if err := dm.IndexFileBlocks("vault", "NB", "", "Fading", blocks, nil); err != nil {
		t.Fatalf("index: %v", err)
	}
	if err := dm.IndexFileBlocks("vault", "NB", "", "Fading", nil, nil); err != nil {
		t.Fatalf("reindex empty: %v", err)
	}
	pages, err := dm.ListDistinctPages()
	if err != nil {
		t.Fatalf("ListDistinctPages: %v", err)
	}
	found := false
	for _, p := range pages {
		if p.Page == "Fading" {
			found = true
		}
	}
	if !found {
		t.Fatal("page lost from inventory after a zero-block reindex")
	}
}

func TestPageCoreBackfill_ReopenPopulatesInventory(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "index.sqlite")

	// Open 1: index pages the way a pre-projection vault would have — via
	// the block-only path — then strip the identity rows and the marker to
	// simulate the upgrade state.
	dm1, err := NewDatabaseManager(dbPath)
	if err != nil {
		t.Fatalf("open1: %v", err)
	}
	for _, page := range []string{"Alpha", "Beta"} {
		blocks := []parser.ParsedBlock{
			{ID: "dddd" + page + "-0000-4000-8000-000000000001", Type: parser.BlockNote,
				RawText: page + " body", CleanText: page + " body", LineNumber: 1},
		}
		if err := dm1.IndexFileBlocks("vault", "NB", "Sec", page, blocks, nil); err != nil {
			t.Fatalf("index %s: %v", page, err)
		}
	}
	if _, err := dm1.SQLDB().Exec(`DELETE FROM page_core`); err != nil {
		t.Fatalf("strip page_core: %v", err)
	}
	if _, err := dm1.SQLDB().Exec(`DELETE FROM schema_migrations WHERE name = ?`, pageCoreBlocksBackfillMarker); err != nil {
		t.Fatalf("clear marker: %v", err)
	}
	if err := dm1.Close(); err != nil {
		t.Fatalf("close1: %v", err)
	}

	// Open 2: initSchema backfills identity rows from blocks in one tx.
	dm2, err := NewDatabaseManager(dbPath)
	if err != nil {
		t.Fatalf("open2: %v", err)
	}
	t.Cleanup(func() { _ = dm2.Close() })

	if !migrationMarkerApplied(t, dm2, pageCoreBlocksBackfillMarker) {
		t.Fatal("backfill marker missing after reopen")
	}
	pages, err := dm2.ListDistinctPages()
	if err != nil {
		t.Fatalf("ListDistinctPages: %v", err)
	}
	if len(pages) != 2 || pages[0].Page != "Alpha" || pages[1].Page != "Beta" {
		t.Fatalf("inventory after backfill = %+v", pages)
	}

	// Backfilled rows are identity-only: frontmatter-derived fields stay
	// empty until the file's next frontmatter-affecting index republishes
	// them (they are re-derivable caches, cardinal rule 4).
	core, err := dm2.GetPageCoreProjection("vault", "NB", "Sec", "Alpha")
	if err != nil || core == nil {
		t.Fatalf("GetPageCoreProjection after backfill: %v, %v", core, err)
	}
	if core.Type != "" || core.Date != "" || core.Created != "" {
		t.Errorf("backfilled row should be identity-only, got %+v", core)
	}
}

func TestPageCoreBackfill_IdempotentReopen(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "index.sqlite")

	dm1, err := NewDatabaseManager(dbPath)
	if err != nil {
		t.Fatalf("open1: %v", err)
	}
	blocks := []parser.ParsedBlock{
		{ID: "eeee1111-0000-4000-8000-000000000001", Type: parser.BlockNote,
			RawText: "solo body", CleanText: "solo body", LineNumber: 1},
	}
	if err := dm1.IndexFileBlocks("vault", "NB", "", "Solo", blocks, nil); err != nil {
		t.Fatalf("index: %v", err)
	}
	// Preserve the CORE fields an existing row may carry: the backfill must
	// never overwrite a present row (NOT EXISTS guard).
	if _, err := dm1.SQLDB().Exec(
		`UPDATE page_core SET type = 'meeting', date = '2026-01-02' WHERE page = 'Solo'`); err != nil {
		t.Fatalf("seed core fields: %v", err)
	}
	if err := dm1.Close(); err != nil {
		t.Fatalf("close1: %v", err)
	}

	dm2, err := NewDatabaseManager(dbPath)
	if err != nil {
		t.Fatalf("open2: %v", err)
	}
	t.Cleanup(func() { _ = dm2.Close() })

	var rows int
	if err := dm2.SQLDB().QueryRow(`SELECT COUNT(*) FROM page_core`).Scan(&rows); err != nil {
		t.Fatalf("count: %v", err)
	}
	if rows != 1 {
		t.Fatalf("backfill duplicated rows: %d", rows)
	}
	core, err := dm2.GetPageCoreProjection("vault", "NB", "", "Solo")
	if err != nil || core == nil {
		t.Fatalf("GetPageCoreProjection: %v %v", core, err)
	}
	if core.Type != "meeting" || core.Date != "2026-01-02" {
		t.Errorf("backfill clobbered existing core fields: %+v", core)
	}
}

// TestPageCoreBackfill_FTSTriggerSafety pins the migration-safety property:
// the backfill only INSERTs into page_core — it never UPDATEs blocks — so
// the external-content FTS sync triggers and the FTS index itself must be
// intact after a reopen that runs the backfill (no trigger drop/restore is
// needed, unlike the page_fold backfill).
func TestPageCoreBackfill_FTSTriggerSafety(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "index.sqlite")

	dm1, err := NewDatabaseManager(dbPath)
	if err != nil {
		t.Fatalf("open1: %v", err)
	}
	needle := "quokka42 haystack content"
	blocks := []parser.ParsedBlock{
		{ID: "ffff1111-0000-4000-8000-000000000001", Type: parser.BlockNote,
			RawText: needle, CleanText: needle, LineNumber: 1},
	}
	if err := dm1.IndexFileBlocks("vault", "NB", "", "FTSProbe", blocks, nil); err != nil {
		t.Fatalf("index: %v", err)
	}
	// Re-arm the backfill for open 2.
	if _, err := dm1.SQLDB().Exec(`DELETE FROM page_core`); err != nil {
		t.Fatalf("strip page_core: %v", err)
	}
	if _, err := dm1.SQLDB().Exec(`DELETE FROM schema_migrations WHERE name = ?`, pageCoreBlocksBackfillMarker); err != nil {
		t.Fatalf("clear marker: %v", err)
	}
	if err := dm1.Close(); err != nil {
		t.Fatalf("close1: %v", err)
	}

	dm2, err := NewDatabaseManager(dbPath)
	if err != nil {
		t.Fatalf("open2: %v", err)
	}
	t.Cleanup(func() { _ = dm2.Close() })

	// All three sync triggers present.
	var triggers int
	if err := dm2.SQLDB().QueryRow(
		`SELECT count(*) FROM sqlite_master WHERE type='trigger' AND name IN ('blocks_fts_ai','blocks_fts_ad','blocks_fts_au')`,
	).Scan(&triggers); err != nil || triggers != 3 {
		t.Fatalf("expected 3 FTS sync triggers, got %d err=%v", triggers, err)
	}

	// FTS still answers without a rebuild.
	res, err := dm2.SearchBlocksPaged("quokka42", 0, 10, SearchFilters{})
	if err != nil {
		t.Fatalf("SearchBlocksPaged: %v", err)
	}
	if res.Total != 1 {
		t.Fatalf("FTS lost content across the backfill reopen: total=%d", res.Total)
	}

	// And a fresh block insert still syncs (triggers live, not rebuilt).
	more := []parser.ParsedBlock{
		{ID: "ffff1111-0000-4000-8000-000000000002", Type: parser.BlockNote,
			RawText: "postreopen token", CleanText: "postreopen token", LineNumber: 2},
	}
	if err := dm2.IndexFileBlocks("vault", "NB", "", "FTSProbe", more, nil); err != nil {
		t.Fatalf("reindex: %v", err)
	}
	res, err = dm2.SearchBlocksPaged("postreopen", 0, 10, SearchFilters{})
	if err != nil {
		t.Fatalf("SearchBlocksPaged: %v", err)
	}
	if res.Total != 1 {
		t.Fatalf("trigger sync broken after reopen: total=%d", res.Total)
	}
}
