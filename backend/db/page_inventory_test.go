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
	if err := dm.IndexFileWithProjection("", "vault", "Work", "Ops", "Scratchpad",
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

// TestListDistinctPages_IdentityChangeDropsAbandonedLoc pins the identity-
// change self-heal. Blocks carry their own stable IDs and re-home on a
// frontmatter identity edit, but page_core and the page-scoped projections are
// keyed by loc and have no ID to move: a re-index must clear the loc the page
// left, or the inventory keeps a twin that resolves wiki links to a page the
// file no longer carries.
func TestListDistinctPages_IdentityChangeDropsAbandonedLoc(t *testing.T) {
	dm := newTestDB(t)

	blocks := []parser.ParsedBlock{
		{ID: "ffff1111-0000-4000-8000-000000000001", Type: parser.BlockNote,
			RawText: "retitled body", CleanText: "retitled body", LineNumber: 1},
	}
	props := []ProjectedProperty{
		{Property: "status", ValueText: "active", ValueSort: "active", ValueType: "text"},
	}
	if err := dm.IndexFileWithProjection("", "vault", "NB", "OldSec", "OldPage",
		blocks, nil, "meeting", props, PageCoreFields{Type: "meeting", Date: "2026-01-02"}); err != nil {
		t.Fatalf("index at old identity: %v", err)
	}

	// Same block (stable ID), new frontmatter identity: what an in-place
	// `page:`/`section:` edit produces on the reindex path.
	if err := dm.IndexFileWithProjection("", "vault", "NB", "NewSec", "NewPage",
		blocks, nil, "", nil, PageCoreFields{}); err != nil {
		t.Fatalf("reindex at new identity: %v", err)
	}

	pages, err := dm.ListDistinctPages()
	if err != nil {
		t.Fatalf("ListDistinctPages: %v", err)
	}
	var haveOld, haveNew bool
	for _, p := range pages {
		switch p.Page {
		case "OldPage":
			haveOld = true
		case "NewPage":
			haveNew = true
		}
	}
	if !haveNew {
		t.Fatalf("new identity missing from inventory: %+v", pages)
	}
	if haveOld {
		t.Errorf("abandoned identity still in inventory: %+v", pages)
	}
	for _, q := range []string{
		`SELECT COUNT(*) FROM blocks WHERE page = 'OldPage'`,
		`SELECT COUNT(*) FROM page_types WHERE page = 'OldPage'`,
		`SELECT COUNT(*) FROM page_properties WHERE page = 'OldPage'`,
		`SELECT COUNT(*) FROM page_core WHERE page = 'OldPage'`,
	} {
		var n int
		if err := dm.SQLDB().QueryRow(q).Scan(&n); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		if n != 0 {
			t.Errorf("abandoned loc rows left behind (%s): %d", q, n)
		}
	}
	if old, err := dm.GetPageCoreProjection("vault", "NB", "OldSec", "OldPage"); err != nil || old != nil {
		t.Errorf("GetPageCoreProjection(old) = %+v, %v (want nil, nil)", old, err)
	}
}

// TestListDistinctPages_ZeroBlockIdentityChangeDropsAbandonedLoc pins the
// residual the block-evidence reconcile cannot see: a file that carried no
// blocks at its last index leaves no block IDs to follow, so the loc recorded
// for its path is the only evidence that the old identity belonged to that
// file. Without it the abandoned identity stays in the inventory forever and
// keeps resolving wiki links to a page the file no longer carries.
func TestListDistinctPages_ZeroBlockIdentityChangeDropsAbandonedLoc(t *testing.T) {
	dm := newTestDB(t)
	const path = "/vault/NB/OldSec/OldPage.md"

	if err := dm.IndexFileWithProjection(path, "vault", "NB", "OldSec", "OldPage",
		nil, nil, "", nil, PageCoreFields{}); err != nil {
		t.Fatalf("index empty page at old identity: %v", err)
	}
	// Same file, frontmatter identity edited while it still has no content
	// blocks — the external-edit reindex path, which carries no block IDs.
	if err := dm.IndexFileWithProjection(path, "vault", "NB", "NewSec", "NewPage",
		nil, nil, "", nil, PageCoreFields{}); err != nil {
		t.Fatalf("reindex empty page at new identity: %v", err)
	}

	assertInventoryHasOnly(t, dm, "NewPage", "OldPage")
	assertNoPageLocRows(t, dm, "OldPage")
}

// TestListDistinctPages_ZeroBlockOriginToBlocksDropsAbandonedLoc covers the
// crossed variant: the old loc had no blocks, the new identity gains them. The
// incoming block IDs are new, so they prove nothing about the old loc — only
// the path-keyed record does.
func TestListDistinctPages_ZeroBlockOriginToBlocksDropsAbandonedLoc(t *testing.T) {
	dm := newTestDB(t)
	const path = "/vault/NB/OldSec/OldPage.md"

	if err := dm.IndexFileWithProjection(path, "vault", "NB", "OldSec", "OldPage",
		nil, nil, "", nil, PageCoreFields{}); err != nil {
		t.Fatalf("index empty page at old identity: %v", err)
	}
	blocks := []parser.ParsedBlock{
		{ID: "ffff4444-0000-4000-8000-000000000001", Type: parser.BlockNote,
			RawText: "gained a body", CleanText: "gained a body", LineNumber: 1},
	}
	if err := dm.IndexFileWithProjection(path, "vault", "NB", "NewSec", "NewPage",
		blocks, nil, "meeting", nil, PageCoreFields{Type: "meeting"}); err != nil {
		t.Fatalf("reindex with blocks at new identity: %v", err)
	}

	assertInventoryHasOnly(t, dm, "NewPage", "OldPage")
	assertNoPageLocRows(t, dm, "OldPage")
}

// TestListDistinctPages_ZeroBlockMoveKeepsSharedLoc guards the clear's safety
// condition: two files can resolve to one identity through frontmatter name
// overrides, so a loc another indexed path still claims must survive.
func TestListDistinctPages_ZeroBlockMoveKeepsSharedLoc(t *testing.T) {
	dm := newTestDB(t)
	const movedPath = "/vault/NB/Sec/Alpha.md"
	const stayPath = "/vault/NB/Sec/Beta.md"

	for _, p := range []string{movedPath, stayPath} {
		if err := dm.IndexFileWithProjection(p, "vault", "NB", "Sec", "Shared",
			nil, nil, "", nil, PageCoreFields{}); err != nil {
			t.Fatalf("index %s at shared identity: %v", p, err)
		}
	}
	if err := dm.IndexFileWithProjection(movedPath, "vault", "NB", "Sec", "Elsewhere",
		nil, nil, "", nil, PageCoreFields{}); err != nil {
		t.Fatalf("reindex moved file: %v", err)
	}

	pages, err := dm.ListDistinctPages()
	if err != nil {
		t.Fatalf("ListDistinctPages: %v", err)
	}
	var haveShared, haveElsewhere bool
	for _, p := range pages {
		switch p.Page {
		case "Shared":
			haveShared = true
		case "Elsewhere":
			haveElsewhere = true
		}
	}
	if !haveShared || !haveElsewhere {
		t.Fatalf("shared=%v elsewhere=%v inventory=%+v (the surviving owner's loc must not be cleared)", haveShared, haveElsewhere, pages)
	}
}

// TestIndexScanResults_ZeroBlockIdentityChangeDropsAbandonedLoc covers the
// batched cold-start path: an identity edited while Silt was closed is
// re-indexed by the scan, whose batch transaction must run the same
// path-keyed reconcile.
func TestIndexScanResults_ZeroBlockIdentityChangeDropsAbandonedLoc(t *testing.T) {
	dm := newTestDB(t)
	const path = "/vault/NB/OldSec/OldPage.md"

	if err := dm.IndexFileWithProjection(path, "vault", "NB", "OldSec", "OldPage",
		nil, nil, "", nil, PageCoreFields{}); err != nil {
		t.Fatalf("index empty page at old identity: %v", err)
	}
	results := []parser.ScanResult{{Path: path, Notebook: "NB", Section: "NewSec", Page: "NewPage"}}
	if _, _, err := dm.IndexScanResultsWithProjection(results, []ScanProjection{{Core: PageCoreFields{}}}); err != nil {
		t.Fatalf("IndexScanResultsWithProjection: %v", err)
	}

	assertInventoryHasOnly(t, dm, "NewPage", "OldPage")
	assertNoPageLocRows(t, dm, "OldPage")
}

// assertInventoryHasOnly fails unless every want page is in the inventory and
// no unwanted page is.
func assertInventoryHasOnly(t *testing.T, dm *DatabaseManager, want, unwanted string) {
	t.Helper()
	pages, err := dm.ListDistinctPages()
	if err != nil {
		t.Fatalf("ListDistinctPages: %v", err)
	}
	var haveWant, haveUnwanted bool
	for _, p := range pages {
		switch p.Page {
		case want:
			haveWant = true
		case unwanted:
			haveUnwanted = true
		}
	}
	if !haveWant {
		t.Fatalf("%q missing from inventory: %+v", want, pages)
	}
	if haveUnwanted {
		t.Errorf("abandoned identity %q still in inventory: %+v", unwanted, pages)
	}
}

// assertNoPageLocRows fails when any loc-keyed table still carries rows for a
// page name, i.e. the abandoned identity was not fully cleared.
func assertNoPageLocRows(t *testing.T, dm *DatabaseManager, page string) {
	t.Helper()
	for _, table := range []string{"blocks", "page_types", "page_properties", "page_core"} {
		var n int
		q := "SELECT COUNT(*) FROM " + table + " WHERE page = ?"
		if err := dm.SQLDB().QueryRow(q, page).Scan(&n); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		if n != 0 {
			t.Errorf("%s still has %d row(s) for abandoned page %q", table, n, page)
		}
	}
}

// TestListDistinctPages_TransplantedBlockKeepsSourcePage guards the containment
// rule: a single block moving to another page must not take its source page's
// inventory row with it, because that page still owns other blocks.
func TestListDistinctPages_TransplantedBlockKeepsSourcePage(t *testing.T) {
	dm := newTestDB(t)

	source := []parser.ParsedBlock{
		{ID: "ffff2222-0000-4000-8000-000000000001", Type: parser.BlockNote,
			RawText: "stays home", CleanText: "stays home", LineNumber: 1},
		{ID: "ffff2222-0000-4000-8000-000000000002", Type: parser.BlockNote,
			RawText: "moves away", CleanText: "moves away", LineNumber: 2},
	}
	if err := dm.IndexFileBlocks("vault", "NB", "", "Source Page", source, nil); err != nil {
		t.Fatalf("index source: %v", err)
	}
	target := []parser.ParsedBlock{
		{ID: "ffff2222-0000-4000-8000-000000000003", Type: parser.BlockNote,
			RawText: "target body", CleanText: "target body", LineNumber: 1},
		source[1], // transplanted block, same stable ID
	}
	if err := dm.IndexFileBlocks("vault", "NB", "", "Target Page", target, nil); err != nil {
		t.Fatalf("index target: %v", err)
	}

	pages, err := dm.ListDistinctPages()
	if err != nil {
		t.Fatalf("ListDistinctPages: %v", err)
	}
	var haveSource bool
	for _, p := range pages {
		if p.Page == "Source Page" {
			haveSource = true
		}
	}
	if !haveSource {
		t.Fatalf("source page lost its inventory row after a single-block move: %+v", pages)
	}
	var remaining int
	if err := dm.SQLDB().QueryRow(
		`SELECT COUNT(*) FROM blocks WHERE page = 'Source Page'`).Scan(&remaining); err != nil {
		t.Fatalf("count source blocks: %v", err)
	}
	if remaining != 1 {
		t.Fatalf("source page blocks = %d, want the one that stayed", remaining)
	}
}

// TestIndexScanResults_IdentityChangeDropsAbandonedLoc covers the cold-start
// path: an identity edited while Silt was closed is re-indexed by the batched
// scan, which must reconcile the abandoned loc in its batch transaction.
func TestIndexScanResults_IdentityChangeDropsAbandonedLoc(t *testing.T) {
	dm := newTestDB(t)

	block := parser.ParsedBlock{ID: "ffff3333-0000-4000-8000-000000000001", Type: parser.BlockNote,
		RawText: "scanned body", CleanText: "scanned body", LineNumber: 1}
	if err := dm.IndexFileBlocks("vault", "NB", "", "Before Scan", []parser.ParsedBlock{block}, nil); err != nil {
		t.Fatalf("index before scan: %v", err)
	}

	results := []parser.ScanResult{{
		Path:     "NB/After Scan.md",
		Notebook: "NB",
		Page:     "After Scan",
		Blocks:   []parser.ParsedBlock{block},
	}}
	projections := []ScanProjection{{Core: PageCoreFields{}}}
	if _, _, err := dm.IndexScanResultsWithProjection(results, projections); err != nil {
		t.Fatalf("IndexScanResultsWithProjection: %v", err)
	}

	pages, err := dm.ListDistinctPages()
	if err != nil {
		t.Fatalf("ListDistinctPages: %v", err)
	}
	var haveBefore, haveAfter bool
	for _, p := range pages {
		switch p.Page {
		case "Before Scan":
			haveBefore = true
		case "After Scan":
			haveAfter = true
		}
	}
	if !haveAfter || haveBefore {
		t.Fatalf("scan identity reconciliation: before=%v after=%v inventory=%+v", haveBefore, haveAfter, pages)
	}
}

// TestListDistinctPages_CrossPageMoveKeepsEmptiedSourcePage reproduces the
// editor's cross-page move at the index level: the TARGET page is published
// first (applyBlocksOps saves it, so the moved block is indexed there while the
// source still holds it) and the moved block is then removed from the SOURCE
// with a targeted delete that deliberately does not re-index. When the move
// empties the source — trivially true for a single-block page — containment
// alone cannot distinguish that from an identity change, and treating it as one
// would delete a live page's blocks, projections, and page_core for the rest of
// the session (the source file still exists; only a reopen would heal it).
func TestListDistinctPages_CrossPageMoveKeepsEmptiedSourcePage(t *testing.T) {
	dm := newTestDB(t)
	const sourcePath = "/vault/NB/Ops/Stub.md"
	const targetPath = "/vault/NB/Ops/Target.md"

	moved := parser.ParsedBlock{ID: "ffff5555-0000-4000-8000-000000000001", Type: parser.BlockTask,
		RawText: "- [ ] moved", CleanText: "moved", LineNumber: 1}
	props := []ProjectedProperty{
		{Property: "status", ValueText: "active", ValueSort: "active", ValueType: "text"},
	}
	if err := dm.IndexFileWithProjection(sourcePath, "vault", "NB", "Ops", "Stub",
		[]parser.ParsedBlock{moved}, nil, "meeting", props,
		PageCoreFields{Type: "meeting", Date: "2026-05-01"}); err != nil {
		t.Fatalf("index source page: %v", err)
	}

	// Target first: the moved block is now indexed at the target while its
	// source row still exists. This is the publish that used to clear the
	// source loc.
	stay := parser.ParsedBlock{ID: "ffff5555-0000-4000-8000-000000000002", Type: parser.BlockNote,
		RawText: "target body", CleanText: "target body", LineNumber: 1}
	if err := dm.IndexFileWithProjection(targetPath, "vault", "NB", "Ops", "Target",
		[]parser.ParsedBlock{stay, moved}, nil, "meeting", props,
		PageCoreFields{Type: "meeting", Date: "2026-05-01"}); err != nil {
		t.Fatalf("index target page: %v", err)
	}

	// Source half: targeted delete only, no re-index (the concurrency rule that
	// makes a full re-index unsafe for a concurrent mover).
	if err := dm.DeleteBlockFromPage(moved.ID, "vault", "NB", "Ops", "Stub"); err != nil {
		t.Fatalf("remove moved block from source: %v", err)
	}

	pages, err := dm.ListDistinctPages()
	if err != nil {
		t.Fatalf("ListDistinctPages: %v", err)
	}
	var haveStub, haveTarget bool
	for _, p := range pages {
		switch p.Page {
		case "Stub":
			haveStub = true
		case "Target":
			haveTarget = true
		}
	}
	if !haveStub {
		t.Errorf("emptied source page lost its inventory row after a cross-page move: %+v", pages)
	}
	if !haveTarget {
		t.Errorf("target page missing from inventory: %+v", pages)
	}

	// The whole projection must survive, not just page_core: a typed dashboard
	// and the properties panel read the other two.
	core, err := dm.GetPageCoreProjection("vault", "NB", "Ops", "Stub")
	if err != nil {
		t.Fatalf("GetPageCoreProjection(source): %v", err)
	}
	if core == nil || core.Type != "meeting" || core.Date != "2026-05-01" {
		t.Errorf("source page core metadata lost: %+v", core)
	}
	var pageTypes, pageProps, remaining int
	for _, c := range []struct {
		q   string
		out *int
	}{
		{`SELECT COUNT(*) FROM page_types WHERE page = 'Stub'`, &pageTypes},
		{`SELECT COUNT(*) FROM page_properties WHERE page = 'Stub'`, &pageProps},
		{`SELECT COUNT(*) FROM blocks WHERE page = 'Stub'`, &remaining},
	} {
		if err := dm.SQLDB().QueryRow(c.q).Scan(c.out); err != nil {
			t.Fatalf("%s: %v", c.q, err)
		}
	}
	if pageTypes != 1 || pageProps != 1 {
		t.Errorf("source projection lost: page_types=%d page_properties=%d", pageTypes, pageProps)
	}
	if remaining != 0 {
		t.Errorf("source page still indexes %d moved block(s)", remaining)
	}
}

// TestListDistinctPages_DuplicateIDCarrierKeepsBothPages pins the other shape
// containment cannot tell from an identity change: two files carrying the same
// inline block ID (a copy, or the sync-conflict carriers ScanDuplicateIDs
// exists to warn about). Whichever file is indexed second re-homes the block; it
// must not wipe the first carrier's page, or that page disappears from the
// inventory permanently (its files row is untouched, so the next warm scan skips
// it).
func TestListDistinctPages_DuplicateIDCarrierKeepsBothPages(t *testing.T) {
	dm := newTestDB(t)
	const firstPath = "/vault/NB/Ops/Original.md"
	const copyPath = "/vault/NB/Ops/Original Copy.md"

	shared := parser.ParsedBlock{ID: "ffff6666-0000-4000-8000-000000000001", Type: parser.BlockNote,
		RawText: "copied body", CleanText: "copied body", LineNumber: 1}
	props := []ProjectedProperty{
		{Property: "status", ValueText: "active", ValueSort: "active", ValueType: "text"},
	}
	if err := dm.IndexFileWithProjection(firstPath, "vault", "NB", "Ops", "Original",
		[]parser.ParsedBlock{shared}, nil, "meeting", props,
		PageCoreFields{Type: "meeting", Date: "2026-05-02"}); err != nil {
		t.Fatalf("index original: %v", err)
	}
	// The copy carries the same ID: the per-file reindex path (watcher picks the
	// new file up) indexes it exactly like a target publish.
	if err := dm.IndexFileWithProjection(copyPath, "vault", "NB", "Ops", "Original Copy",
		[]parser.ParsedBlock{shared}, nil, "meeting", props,
		PageCoreFields{Type: "meeting", Date: "2026-05-02"}); err != nil {
		t.Fatalf("index copy: %v", err)
	}

	pages, err := dm.ListDistinctPages()
	if err != nil {
		t.Fatalf("ListDistinctPages: %v", err)
	}
	have := map[string]bool{}
	for _, p := range pages {
		have[p.Page] = true
	}
	if !have["Original"] || !have["Original Copy"] {
		t.Fatalf("duplicate-ID carrier wiped a live page: inventory=%+v (want both Original and Original Copy)", pages)
	}
	core, err := dm.GetPageCoreProjection("vault", "NB", "Ops", "Original")
	if err != nil {
		t.Fatalf("GetPageCoreProjection(original): %v", err)
	}
	if core == nil || core.Type != "meeting" {
		t.Errorf("first carrier's core metadata lost: %+v", core)
	}
}

// TestListDistinctPages_IdentityChangeWithPathRecordStillDropsAbandonedLoc
// guards the abandonment guard's other side: once the moving file's path record
// points at its new loc, the old loc has no owner and must still be cleared.
// Without this, requiring an owner would silently disable the identity-change
// reconcile.
func TestListDistinctPages_IdentityChangeWithPathRecordStillDropsAbandonedLoc(t *testing.T) {
	dm := newTestDB(t)
	const path = "/vault/NB/OldSec/OldPage.md"

	blocks := []parser.ParsedBlock{
		{ID: "ffff7777-0000-4000-8000-000000000001", Type: parser.BlockNote,
			RawText: "retitled body", CleanText: "retitled body", LineNumber: 1},
	}
	props := []ProjectedProperty{
		{Property: "status", ValueText: "active", ValueSort: "active", ValueType: "text"},
	}
	if err := dm.IndexFileWithProjection(path, "vault", "NB", "OldSec", "OldPage",
		blocks, nil, "meeting", props, PageCoreFields{Type: "meeting"}); err != nil {
		t.Fatalf("index at old identity: %v", err)
	}
	// Same file, new frontmatter identity: the path record moves with it, so the
	// old loc now has no owner.
	if err := dm.IndexFileWithProjection(path, "vault", "NB", "NewSec", "NewPage",
		blocks, nil, "", nil, PageCoreFields{}); err != nil {
		t.Fatalf("reindex at new identity: %v", err)
	}

	assertInventoryHasOnly(t, dm, "NewPage", "OldPage")
	assertNoPageLocRows(t, dm, "OldPage")
}

// TestIndexScanResults_DuplicateIDCarriersKeepBothPages is the cold-start
// variant of the carrier case: a copy made while Silt was closed means the batch
// sees two files with one block ID. The scan must re-home the block without
// wiping either page — the scan marks every result's files row, so a wiped page
// would be warm-skipped forever after.
func TestIndexScanResults_DuplicateIDCarriersKeepBothPages(t *testing.T) {
	dm := newTestDB(t)

	shared := parser.ParsedBlock{ID: "ffff8888-0000-4000-8000-000000000001", Type: parser.BlockNote,
		RawText: "copied body", CleanText: "copied body", LineNumber: 1}
	results := []parser.ScanResult{
		{Path: "/vault/NB/Ops/Original.md", Notebook: "NB", Section: "Ops", Page: "Original",
			Blocks: []parser.ParsedBlock{shared}},
		{Path: "/vault/NB/Ops/Original Copy.md", Notebook: "NB", Section: "Ops", Page: "Original Copy",
			Blocks: []parser.ParsedBlock{shared}},
	}
	projections := []ScanProjection{
		{TypeID: "meeting", Props: []ProjectedProperty{
			{Property: "status", ValueText: "active", ValueSort: "active", ValueType: "text"},
		}},
		{TypeID: "meeting", Props: []ProjectedProperty{
			{Property: "status", ValueText: "active", ValueSort: "active", ValueType: "text"},
		}},
	}
	if _, _, err := dm.IndexScanResultsWithProjection(results, projections); err != nil {
		t.Fatalf("IndexScanResultsWithProjection: %v", err)
	}

	for _, page := range []string{"Original", "Original Copy"} {
		var core, types int
		if err := dm.SQLDB().QueryRow(`SELECT COUNT(*) FROM page_core WHERE page = ?`, page).Scan(&core); err != nil {
			t.Fatalf("count page_core(%s): %v", page, err)
		}
		if err := dm.SQLDB().QueryRow(`SELECT COUNT(*) FROM page_types WHERE page = ?`, page).Scan(&types); err != nil {
			t.Fatalf("count page_types(%s): %v", page, err)
		}
		if core != 1 || types != 1 {
			t.Errorf("%s lost rows to the duplicate ID: page_core=%d page_types=%d", page, core, types)
		}
	}
}

// TestReconcilePageInventory_KeepsUnreadableFileLoc pins the scan's blind spot:
// a file the scan cannot read or resolve this pass has no known loc, but it is
// still on disk. Its recorded loc must stay live, or a transient permission /
// decode failure would drop a live page from the inventory.
func TestReconcilePageInventory_KeepsUnreadableFileLoc(t *testing.T) {
	dm := newTestDB(t)
	const path = "/vault/NB/Sec/Unreadable.md"

	blocks := []parser.ParsedBlock{
		{ID: "ffff9999-0000-4000-8000-000000000001", Type: parser.BlockNote,
			RawText: "body", CleanText: "body", LineNumber: 1},
	}
	props := []ProjectedProperty{
		{Property: "status", ValueText: "active", ValueSort: "active", ValueType: "text"},
	}
	if err := dm.IndexFileWithProjection(path, "vault", "NB", "Sec", "Unreadable",
		blocks, nil, "meeting", props, PageCoreFields{Type: "meeting"}); err != nil {
		t.Fatalf("index: %v", err)
	}

	removed, err := dm.ReconcilePageInventory("vault", []PageInventoryScan{{Path: path, Unresolved: true}})
	if err != nil {
		t.Fatalf("ReconcilePageInventory(unresolved): %v", err)
	}
	if removed != 0 {
		t.Errorf("removed = %d, want 0: an unreadable file is still on disk", removed)
	}
	core, err := dm.GetPageCoreProjection("vault", "NB", "Sec", "Unreadable")
	if err != nil || core == nil || core.Type != "meeting" {
		t.Fatalf("unreadable file's inventory row was dropped: %+v, %v", core, err)
	}

	// Control: with no scan entry at all the loc really is stale.
	removed, err = dm.ReconcilePageInventory("vault", nil)
	if err != nil {
		t.Fatalf("ReconcilePageInventory(empty): %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d, want 1 (the loc no file publishes)", removed)
	}
	for _, table := range []string{"page_core", "page_types", "page_properties"} {
		var n int
		q := "SELECT COUNT(*) FROM " + table + " WHERE page = 'Unreadable'"
		if err := dm.SQLDB().QueryRow(q).Scan(&n); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		if n != 0 {
			t.Errorf("%s still has %d row(s) for the stale loc", table, n)
		}
	}
}

// TestReconcilePageInventory_HealsPreInventoryGhosts covers the upgrade path:
// page_core predates its use as the inventory, so a vault that ever had a
// frontmatter identity change carries rows at locs no file publishes any more.
// The startup scan is the authoritative inventory and must drop them — while
// preserving the core fields of live pages, recording warm-skipped files' locs,
// and leaving other sources alone.
func TestReconcilePageInventory_HealsPreInventoryGhosts(t *testing.T) {
	dm := newTestDB(t)
	const livePath = "/vault/NB/Sec/Live.md"
	const linkGhostPath = "/linked/Sec/Linked.md"

	// A live page with real core metadata (as a frontmatter publish leaves it).
	if err := dm.IndexFileWithProjection(livePath, "vault", "NB", "Sec", "Live",
		nil, nil, "", nil, PageCoreFields{Type: "meeting", Date: "2026-04-01"}); err != nil {
		t.Fatalf("index live page: %v", err)
	}
	// Ghost rows exactly as an older build left them: page_core at an abandoned
	// loc, plus a page_types row whose core row a failed publish already dropped.
	for _, q := range []string{
		`INSERT INTO page_core (source, notebook, section, page, type, date, aliases, created)
		 VALUES ('vault', 'NB', 'OldSec', 'Ghost', 'meeting', '2026-01-01', '[]', '')`,
		`INSERT INTO page_types (source, notebook, section, page, type_name)
		 VALUES ('vault', 'NB', 'OldSec', 'Orphan', 'meeting')`,
		`INSERT INTO page_core (source, notebook, section, page, type, date, aliases, created)
		 VALUES ('linked:test', 'Sec', '', 'Linked', 'meeting', '2026-01-01', '[]', '')`,
	} {
		if _, err := dm.SQLDB().Exec(q); err != nil {
			t.Fatalf("seed ghost (%s): %v", q, err)
		}
	}

	removed, err := dm.ReconcilePageInventory("vault", []PageInventoryScan{
		{Path: livePath, Loc: PageLoc{Source: "vault", Notebook: "NB", Section: "Sec", Page: "Live"}},
		// A warm-skipped file: never re-indexed this session, still on disk.
		{Path: "/vault/NB/Sec/Warm.md", Loc: PageLoc{Source: "vault", Notebook: "NB", Section: "Sec", Page: "Warm"}},
	})
	if err != nil {
		t.Fatalf("ReconcilePageInventory: %v", err)
	}
	if removed != 2 {
		t.Errorf("removed = %d ghost loc(s), want 2 (the page_core ghost and the orphaned page_types row)", removed)
	}

	// Live page keeps its published core fields; the warm-skipped file becomes
	// an inventory row.
	core, err := dm.GetPageCoreProjection("vault", "NB", "Sec", "Live")
	if err != nil {
		t.Fatalf("GetPageCoreProjection(live): %v", err)
	}
	if core == nil || core.Type != "meeting" || core.Date != "2026-04-01" {
		t.Errorf("live page core metadata clobbered: %+v", core)
	}
	if warm, _ := dm.GetPageCoreProjection("vault", "NB", "Sec", "Warm"); warm == nil {
		t.Error("warm-skipped file did not get an inventory row")
	}
	// Warm-skipped file's loc is now owned, so a later move out of it cannot look
	// abandoned.
	var owned int
	if err := dm.SQLDB().QueryRow(
		`SELECT COUNT(*) FROM file_page_locs WHERE path = '/vault/NB/Sec/Warm.md'`).Scan(&owned); err != nil {
		t.Fatalf("count file_page_locs: %v", err)
	}
	if owned != 1 {
		t.Error("warm-skipped file's page loc was not recorded")
	}

	// Ghosts are gone from every loc-keyed table; the other source is untouched.
	assertNoPageLocRows(t, dm, "Ghost")
	assertNoPageLocRows(t, dm, "Orphan")
	var linked int
	if err := dm.SQLDB().QueryRow(
		`SELECT COUNT(*) FROM page_core WHERE source = 'linked:test' AND page = 'Linked'`).Scan(&linked); err != nil {
		t.Fatalf("count linked page_core: %v", err)
	}
	if linked != 1 {
		t.Error("vault-scoped reconcile deleted another source's inventory row")
	}
}

// TestPageCoreBackfill_PreservesExistingRows drives the backfill through the
// mixed state a warm upgrade actually produces: the marker is absent (so the
// guarded INSERT runs) while some identity rows are already present with real
// frontmatter-derived core fields. The NOT EXISTS guard is load-bearing here —
// without it the INSERT would either duplicate a row (PK conflict) or clobber
// the published core fields.
func TestPageCoreBackfill_PreservesExistingRows(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "index.sqlite")

	dm1, err := NewDatabaseManager(dbPath)
	if err != nil {
		t.Fatalf("open1: %v", err)
	}
	index := func(id, source, page string) {
		blocks := []parser.ParsedBlock{
			{ID: id, Type: parser.BlockNote,
				RawText: page + " body", CleanText: page + " body", LineNumber: 1},
		}
		if err := dm1.IndexFileBlocks(source, "NB", "", page, blocks, nil); err != nil {
			t.Fatalf("index %s/%s: %v", source, page, err)
		}
	}
	index("eeee1111-0000-4000-8000-000000000001", "vault", "Alpha")
	index("eeee1111-0000-4000-8000-000000000002", "vault", "Beta")
	index("eeee1111-0000-4000-8000-000000000003", "linked:x", "Shared")

	// Existing row written by a frontmatter-affecting publish before the
	// upgrade: its core fields must survive the backfill untouched.
	if _, err := dm1.SQLDB().Exec(
		`UPDATE page_core SET type = 'meeting', date = '2026-01-02', created = '2026-01-01T09:00:00Z'
		 WHERE source = 'vault' AND page = 'Alpha'`); err != nil {
		t.Fatalf("seed core fields: %v", err)
	}
	// Mixed state: one identity row missing, marker absent.
	if _, err := dm1.SQLDB().Exec(`DELETE FROM page_core WHERE source = 'vault' AND page = 'Beta'`); err != nil {
		t.Fatalf("drop Beta row: %v", err)
	}
	if _, err := dm1.SQLDB().Exec(`DELETE FROM schema_migrations WHERE name = ?`, pageCoreBlocksBackfillMarker); err != nil {
		t.Fatalf("clear marker: %v", err)
	}
	if err := dm1.Close(); err != nil {
		t.Fatalf("close1: %v", err)
	}

	// Reopen: the guarded INSERT backfills Beta alone and leaves Alpha alone.
	dm2, err := NewDatabaseManager(dbPath)
	if err != nil {
		t.Fatalf("open2: %v", err)
	}
	t.Cleanup(func() { _ = dm2.Close() })

	if !migrationMarkerApplied(t, dm2, pageCoreBlocksBackfillMarker) {
		t.Fatal("backfill marker missing after reopen")
	}
	var rows int
	if err := dm2.SQLDB().QueryRow(`SELECT COUNT(*) FROM page_core`).Scan(&rows); err != nil {
		t.Fatalf("count: %v", err)
	}
	if rows != 3 {
		t.Fatalf("backfill duplicated rows: %d (want Alpha, Beta, Shared)", rows)
	}

	alpha, err := dm2.GetPageCoreProjection("vault", "NB", "", "Alpha")
	if err != nil || alpha == nil {
		t.Fatalf("GetPageCoreProjection(Alpha): %v %v", alpha, err)
	}
	if alpha.Type != "meeting" || alpha.Date != "2026-01-02" || alpha.Created != "2026-01-01T09:00:00Z" {
		t.Errorf("backfill clobbered existing core fields: %+v", alpha)
	}

	beta, err := dm2.GetPageCoreProjection("vault", "NB", "", "Beta")
	if err != nil || beta == nil {
		t.Fatalf("GetPageCoreProjection(Beta): %v %v", beta, err)
	}
	if beta.Type != "" || beta.Date != "" || beta.Created != "" {
		t.Errorf("backfilled row should be identity-only, got %+v", beta)
	}

	// Linked sources keep their discriminator: the backfill must not collapse
	// them into the vault namespace.
	shared, err := dm2.GetPageCoreProjection("linked:x", "NB", "", "Shared")
	if err != nil || shared == nil {
		t.Fatalf("linked source row missing after backfill: %v %v", shared, err)
	}
	if shared.Type != "" || shared.Created != "" {
		t.Errorf("linked backfilled row should be identity-only, got %+v", shared)
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
