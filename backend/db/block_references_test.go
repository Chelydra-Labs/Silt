package db

import (
	"database/sql"
	"fmt"
	"testing"

	"silt/backend/parser"
)

// countBlockReferences returns the total row count of block_references.
func countBlockReferences(t *testing.T, dm *DatabaseManager) int {
	t.Helper()
	var n int
	if err := dm.SQLDB().QueryRow("SELECT COUNT(*) FROM block_references").Scan(&n); err != nil {
		t.Fatalf("count block_references: %v", err)
	}
	return n
}

// migrationMarkerApplied reports whether schema_migrations has the named row.
func migrationMarkerApplied(t *testing.T, dm *DatabaseManager, name string) bool {
	t.Helper()
	var appliedAt int64
	err := dm.SQLDB().QueryRow("SELECT applied_at FROM schema_migrations WHERE name = ?", name).Scan(&appliedAt)
	if err == sql.ErrNoRows {
		return false
	}
	if err != nil {
		t.Fatalf("probe marker %s: %v", name, err)
	}
	return true
}

// legacyBlocksSchema is the pre-block_references schema seed: blocks table
// without block_references / schema_migrations. We can't easily simulate a
// true legacy vault (the schema init creates block_references eagerly), so
// the backfill tests instead drop block_references + the marker, seed blocks,
// then re-run the backfill against the seeded blocks.
func seedLegacyBlocks(t *testing.T, dm *DatabaseManager, blocks []parser.ParsedBlock) {
	t.Helper()
	for _, b := range blocks {
		var parentID interface{}
		if b.ParentID != "" {
			parentID = b.ParentID
		}
		_, err := dm.SQLDB().Exec(
			"INSERT INTO blocks (id, parent_id, source, notebook, section, page, file_date, depth, type, raw_content, clean_content, line_number) VALUES (?, ?, 'vault', 'NB', 'Sec', 'Pg', '2026-07-20', 0, ?, ?, ?, 1)",
			b.ID, parentID, string(b.Type), b.RawText, b.CleanText,
		)
		if err != nil {
			t.Fatalf("seed block %s: %v", b.ID, err)
		}
	}
}

// resetBlockReferencesForBackfillTest wipes block_references + the marker so
// the backfill can be exercised against pre-existing blocks rows. Mirrors a
// vault that was upgraded but whose migration crashed before commit.
func resetBlockReferencesForBackfillTest(t *testing.T, dm *DatabaseManager) {
	t.Helper()
	if _, err := dm.SQLDB().Exec("DELETE FROM block_references"); err != nil {
		t.Fatalf("clear block_references: %v", err)
	}
	if _, err := dm.SQLDB().Exec(fmt.Sprintf("DELETE FROM schema_migrations WHERE name = '%s'", blockReferencesBackfillMarker)); err != nil {
		t.Fatalf("clear marker: %v", err)
	}
}

// TestBlockReferences_FreshSchemaHasEmptyTable verifies a brand-new in-memory
// DB creates block_references, the reverse index, and the schema_migrations
// ledger with no rows and the backfill marker already applied (no-op since
// there were no pre-existing blocks).
func TestBlockReferences_FreshSchemaHasEmptyTable(t *testing.T) {
	dm := newTestDB(t)
	if got := countBlockReferences(t, dm); got != 0 {
		t.Fatalf("fresh block_references should be empty, got %d rows", got)
	}
	if !migrationMarkerApplied(t, dm, blockReferencesBackfillMarker) {
		t.Fatalf("backfill marker %q should be applied on fresh schema", blockReferencesBackfillMarker)
	}
	// Index and ledger must exist.
	var idxExists, ledgerExists int
	if err := dm.SQLDB().QueryRow("SELECT count(*) FROM sqlite_master WHERE type='index' AND name='idx_block_references_target'").Scan(&idxExists); err != nil {
		t.Fatalf("probe index: %v", err)
	}
	if idxExists != 1 {
		t.Errorf("expected idx_block_references_target to exist, got count=%d", idxExists)
	}
	if err := dm.SQLDB().QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='schema_migrations'").Scan(&ledgerExists); err != nil {
		t.Fatalf("probe ledger: %v", err)
	}
	if ledgerExists != 1 {
		t.Errorf("expected schema_migrations table to exist, got count=%d", ledgerExists)
	}
}

// TestBlockReferences_BackfillPopulatesFromLegacyBlocks verifies that
// pre-existing blocks.raw_content tokens are extracted into block_references
// by the migration's backfill pass. Mirrors an upgrade from a pre-#704 vault.
func TestBlockReferences_BackfillPopulatesFromLegacyBlocks(t *testing.T) {
	dm := newTestDB(t)
	seedLegacyBlocks(t, dm, []parser.ParsedBlock{
		noteBlock(uuidA, "ref to target "+("(("+uuidB+"))")+" and embed "+"{{embed:"+uuidC+"}}"),
		noteBlock(uuidB, "target block"),
		noteBlock(uuidC, "another target"),
		noteBlock(uuidD, "no refs here"),
	})
	resetBlockReferencesForBackfillTest(t, dm)

	if err := backfillBlockReferences(dm.SQLDB()); err != nil {
		t.Fatalf("backfillBlockReferences: %v", err)
	}

	// Expected edges: A->B (block-ref), A->C (embed). Both targets exist,
	// neither is dangling. D contributes nothing.
	if got := countBlockReferences(t, dm); got != 2 {
		t.Fatalf("expected 2 edges after backfill, got %d", got)
	}
	assertEdgeExists(t, dm, uuidA, uuidB, "block-ref")
	assertEdgeExists(t, dm, uuidA, uuidC, "embed")
	if !migrationMarkerApplied(t, dm, blockReferencesBackfillMarker) {
		t.Errorf("backfill marker should be applied after successful backfill")
	}
}

// TestBlockReferences_BackfillCollapsesDuplicateTokens verifies that the
// source-block PK collapses same-kind duplicate tokens in one block: a block
// with two ((uuid)) refs to the SAME target produces exactly one edge.
func TestBlockReferences_BackfillCollapsesDuplicateTokens(t *testing.T) {
	dm := newTestDB(t)
	seedLegacyBlocks(t, dm, []parser.ParsedBlock{
		// Two block-ref tokens to uuidA in one block + one embed to uuidA.
		noteBlock(uuidB, "(("+uuidA+")) then (("+uuidA+")) again and {{embed:"+uuidA+"}}"),
		noteBlock(uuidA, "target"),
	})
	resetBlockReferencesForBackfillTest(t, dm)

	if err := backfillBlockReferences(dm.SQLDB()); err != nil {
		t.Fatalf("backfill: %v", err)
	}

	// One block-ref edge (deduped) + one embed edge.
	if got := countBlockReferences(t, dm); got != 2 {
		t.Fatalf("expected 2 edges after dedupe, got %d", got)
	}
	assertEdgeExists(t, dm, uuidB, uuidA, "block-ref")
	assertEdgeExists(t, dm, uuidB, uuidA, "embed")
}

// TestBlockReferences_BackfillIgnoresUnrelatedFKOrphans verifies that the
// scoped integrity assertion does NOT brick the backfill when an unrelated
// FK'd table (e.g. task_dependencies) carries a pre-existing orphan row.
// Regression for the original unscoped pragma_foreign_key_check that would
// have made NewDatabaseManager fail on any vault with a stray orphan in any
// FK'd table.
func TestBlockReferences_BackfillIgnoresUnrelatedFKOrphans(t *testing.T) {
	dm := newTestDB(t)
	seedLegacyBlocks(t, dm, []parser.ParsedBlock{
		noteBlock(uuidA, "ref "+("(("+uuidB+"))")),
		noteBlock(uuidB, "target"),
	})
	// Inject an orphan row into task_dependencies by temporarily disabling
	// FK enforcement on the connection (orphan cannot be inserted under
	// FK=ON). This mirrors a vault that accumulated the orphan under a
	// pre-FK enforcement era or via an external tool.
	db := dm.SQLDB()
	if _, err := db.Exec("PRAGMA foreign_keys = OFF"); err != nil {
		t.Fatalf("disable FKs: %v", err)
	}
	if _, err := db.Exec(
		"INSERT INTO task_dependencies (block_id, blocked_by_id) VALUES (?, ?)",
		uuidC, uuidD, // both absent from blocks → orphan
	); err != nil {
		t.Fatalf("seed task_dependencies orphan: %v", err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		t.Fatalf("re-enable FKs: %v", err)
	}

	resetBlockReferencesForBackfillTest(t, dm)
	// The scoped FK check inspects only block_references.source_block_id, so
	// the unrelated task_dependencies orphan must NOT abort the backfill.
	if err := backfillBlockReferences(db); err != nil {
		t.Fatalf("backfill must succeed despite unrelated FK orphan: %v", err)
	}
	assertEdgeExists(t, dm, uuidA, uuidB, "block-ref")
	if !migrationMarkerApplied(t, dm, blockReferencesBackfillMarker) {
		t.Errorf("marker should be applied after successful backfill")
	}
}

// TestBlockReferences_BackfillRetainsDanglingTargetEdges verifies the
// source-only-FK design: an edge to a target block ID that does NOT exist in
// `blocks` is still retained. The backlink re-resolves when the target is
// later indexed without requiring source re-indexing.
func TestBlockReferences_BackfillRetainsDanglingTargetEdges(t *testing.T) {
	dm := newTestDB(t)
	// uuidA references uuidB which is NOT in the blocks table.
	seedLegacyBlocks(t, dm, []parser.ParsedBlock{
		noteBlock(uuidA, "dangling ref "+("(("+uuidB+"))")),
	})
	resetBlockReferencesForBackfillTest(t, dm)

	if err := backfillBlockReferences(dm.SQLDB()); err != nil {
		t.Fatalf("backfill: %v", err)
	}

	// The dangling edge must survive (source-only FK; no target FK).
	assertEdgeExists(t, dm, uuidA, uuidB, "block-ref")
	if got := countBlockReferences(t, dm); got != 1 {
		t.Fatalf("expected 1 dangling edge retained, got %d", got)
	}
}

// TestBlockReferences_BackfillIdempotentReopen verifies that calling
// backfillBlockReferences a second time (warm reopen) is a no-op when the
// marker is present: no duplicate edges, no spurious work.
func TestBlockReferences_BackfillIdempotentReopen(t *testing.T) {
	dm := newTestDB(t)
	seedLegacyBlocks(t, dm, []parser.ParsedBlock{
		noteBlock(uuidA, "ref "+("(("+uuidB+"))")),
		noteBlock(uuidB, "target"),
	})
	resetBlockReferencesForBackfillTest(t, dm)

	if err := backfillBlockReferences(dm.SQLDB()); err != nil {
		t.Fatalf("first backfill: %v", err)
	}
	firstCount := countBlockReferences(t, dm)

	// Simulate a warm reopen by calling backfill again — must short-circuit.
	if err := backfillBlockReferences(dm.SQLDB()); err != nil {
		t.Fatalf("second backfill (warm reopen): %v", err)
	}
	secondCount := countBlockReferences(t, dm)
	if firstCount != secondCount {
		t.Fatalf("warm reopen changed row count: %d -> %d", firstCount, secondCount)
	}
}

// TestBlockReferences_BackfillRedoneAfterInterruptedMigration verifies
// restart-safety: if the marker is missing (crash before commit), the next
// backfill call redoes the work. Combined with the idempotent test above
// this proves the marker is the sole gate.
func TestBlockReferences_BackfillRedoneAfterInterruptedMigration(t *testing.T) {
	dm := newTestDB(t)
	seedLegacyBlocks(t, dm, []parser.ParsedBlock{
		noteBlock(uuidA, "ref "+("(("+uuidB+"))")+" and "+"{{embed:"+uuidC+"}}"),
		noteBlock(uuidB, "target b"),
		noteBlock(uuidC, "target c"),
	})

	// Simulate a crash mid-migration: wipe both the rows and the marker, then
	// run the backfill. It must rebuild the full edge set.
	resetBlockReferencesForBackfillTest(t, dm)
	if err := backfillBlockReferences(dm.SQLDB()); err != nil {
		t.Fatalf("backfill after simulated crash: %v", err)
	}
	if got := countBlockReferences(t, dm); got != 2 {
		t.Fatalf("after crash recovery expected 2 edges, got %d", got)
	}
	if !migrationMarkerApplied(t, dm, blockReferencesBackfillMarker) {
		t.Errorf("marker must be set after crash-recovery backfill")
	}

	// Now simulate ANOTHER crash (drop marker only, keep edges) and rerun.
	// The backfill must remain idempotent at the row level — INSERT OR IGNORE
	// against the existing PK prevents duplicates.
	if _, err := dm.SQLDB().Exec(fmt.Sprintf("DELETE FROM schema_migrations WHERE name = '%s'", blockReferencesBackfillMarker)); err != nil {
		t.Fatalf("clear marker for second crash: %v", err)
	}
	if err := backfillBlockReferences(dm.SQLDB()); err != nil {
		t.Fatalf("backfill after second simulated crash: %v", err)
	}
	if got := countBlockReferences(t, dm); got != 2 {
		t.Fatalf("after second crash expected 2 edges (no dupes), got %d", got)
	}
}

// TestBlockReferences_WarmReopenPersistsEdges verifies the on-disk lifecycle:
// the backfilled block_references rows + the schema_migrations marker
// persist across a close/reopen (the backfill marker prevents re-running on
// the warm path).
func TestBlockReferences_WarmReopenPersistsEdges(t *testing.T) {
	dm, dbPath := newOnDiskDB(t)
	// Seed pre-existing blocks (mirrors a legacy vault pre-#704 upgrade),
	// drop the marker so the backfill runs against them, then close+reopen.
	seedLegacyBlocks(t, dm, []parser.ParsedBlock{
		noteBlock(uuidA, "ref "+("(("+uuidB+"))")+" and embed "+"{{embed:"+uuidC+"}}"),
		noteBlock(uuidB, "target b"),
		noteBlock(uuidC, "target c"),
	})
	resetBlockReferencesForBackfillTest(t, dm)
	if err := backfillBlockReferences(dm.SQLDB()); err != nil {
		t.Fatalf("backfill before reopen: %v", err)
	}
	before := countBlockReferences(t, dm)
	if before != 2 {
		t.Fatalf("setup: expected 2 backfilled edges, got %d", before)
	}
	markerBefore := migrationMarkerApplied(t, dm, blockReferencesBackfillMarker)

	if err := dm.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	dm2, err := NewDatabaseManager(dbPath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer dm2.Close()

	after := countBlockReferences(t, dm2)
	markerAfter := migrationMarkerApplied(t, dm2, blockReferencesBackfillMarker)
	if before != after {
		t.Errorf("row count drifted across reopen: %d -> %d", before, after)
	}
	if !markerAfter {
		t.Errorf("marker must remain applied after warm reopen")
	}
	if markerBefore != markerAfter {
		t.Errorf("marker state drifted across reopen: %v -> %v", markerBefore, markerAfter)
	}
	// Edges themselves must persist.
	assertEdgeExists(t, dm2, uuidA, uuidB, "block-ref")
	assertEdgeExists(t, dm2, uuidA, uuidC, "embed")
}

// TestBlockReferences_FKCascadeOnSourceDelete verifies the source FK cascade:
// deleting a source block from `blocks` removes its block_references rows.
// (Target deletion does NOT cascade — source-only FK design.)
func TestBlockReferences_FKCascadeOnSourceDelete(t *testing.T) {
	dm := newTestDB(t)
	seedLegacyBlocks(t, dm, []parser.ParsedBlock{
		noteBlock(uuidA, "ref "+("(("+uuidB+"))")),
		noteBlock(uuidB, "target"),
	})
	resetBlockReferencesForBackfillTest(t, dm)
	if err := backfillBlockReferences(dm.SQLDB()); err != nil {
		t.Fatalf("backfill: %v", err)
	}
	assertEdgeExists(t, dm, uuidA, uuidB, "block-ref")

	// Delete the source block — cascade must remove the edge.
	if _, err := dm.SQLDB().Exec("DELETE FROM blocks WHERE id = ?", uuidA); err != nil {
		t.Fatalf("delete source block: %v", err)
	}
	if got := countBlockReferences(t, dm); got != 0 {
		t.Fatalf("source cascade should have removed the edge, got %d rows", got)
	}
}

// TestBlockReferences_TargetDeletionKeepsEdge verifies the asymmetry: a target
// block deletion does NOT cascade through block_references (no target FK).
// The edge survives as a derived source projection of markdown intent.
func TestBlockReferences_TargetDeletionKeepsEdge(t *testing.T) {
	dm := newTestDB(t)
	seedLegacyBlocks(t, dm, []parser.ParsedBlock{
		noteBlock(uuidA, "ref "+("(("+uuidB+"))")),
		noteBlock(uuidB, "target"),
	})
	resetBlockReferencesForBackfillTest(t, dm)
	if err := backfillBlockReferences(dm.SQLDB()); err != nil {
		t.Fatalf("backfill: %v", err)
	}

	// Delete the TARGET block — the edge must remain (dangling by design).
	if _, err := dm.SQLDB().Exec("DELETE FROM blocks WHERE id = ?", uuidB); err != nil {
		t.Fatalf("delete target block: %v", err)
	}
	assertEdgeExists(t, dm, uuidA, uuidB, "block-ref")
}

// TestBlockReferences_ExtractionIncludesCodeBlocks verifies that the backfill
// walks every block row regardless of type, mirroring the indexed extractor
// that reads raw_content of CODE blocks too. Page-link CODE exclusion is a
// different indexer path and does not apply here.
func TestBlockReferences_ExtractionIncludesCodeBlocks(t *testing.T) {
	dm := newTestDB(t)
	seedLegacyBlocks(t, dm, []parser.ParsedBlock{
		{ID: uuidA, Type: parser.BlockCode, RawText: "code with ((" + uuidB + ")) inline", CleanText: "code", LineNumber: 1},
		noteBlock(uuidB, "target"),
	})
	resetBlockReferencesForBackfillTest(t, dm)
	if err := backfillBlockReferences(dm.SQLDB()); err != nil {
		t.Fatalf("backfill: %v", err)
	}
	assertEdgeExists(t, dm, uuidA, uuidB, "block-ref")
}

// assertEdgeExists fails the test if the (source, target, kind) edge is not
// present in block_references.
func assertEdgeExists(t *testing.T, dm *DatabaseManager, sourceID, targetID, kind string) {
	t.Helper()
	var n int
	err := dm.SQLDB().QueryRow(
		"SELECT COUNT(*) FROM block_references WHERE source_block_id = ? AND target_block_id = ? AND kind = ?",
		sourceID, targetID, kind,
	).Scan(&n)
	if err != nil {
		t.Fatalf("probe edge %s->%s (%s): %v", sourceID, targetID, kind, err)
	}
	if n != 1 {
		t.Fatalf("expected 1 edge %s->%s (%s), got %d", sourceID, targetID, kind, n)
	}
}

// TestBlockReferences_KindValuesMatchBacklinkConstants guards against the
// stored kind values drifting from the BacklinkKind string values — the
// indexed lookup maps the column directly to the result kind.
func TestBlockReferences_KindValuesMatchBacklinkConstants(t *testing.T) {
	dm := newTestDB(t)
	// One source block carrying both a block-ref and an embed to distinct
	// targets, so both kind values are exercised in a single DB.
	seedLegacyBlocks(t, dm, []parser.ParsedBlock{
		noteBlock(uuidB, "block with "+("(("+uuidA+"))")+" and "+"{{embed:"+uuidC+"}}"),
		noteBlock(uuidA, "target a"),
		noteBlock(uuidC, "target c"),
	})
	resetBlockReferencesForBackfillTest(t, dm)
	if err := backfillBlockReferences(dm.SQLDB()); err != nil {
		t.Fatalf("backfill: %v", err)
	}

	cases := []struct {
		target string
		kind   BacklinkKind
	}{
		{uuidA, BacklinkBlockRef},
		{uuidC, BacklinkEmbed},
	}
	for _, c := range cases {
		var stored string
		err := dm.SQLDB().QueryRow(
			"SELECT kind FROM block_references WHERE source_block_id = ? AND target_block_id = ?",
			uuidB, c.target,
		).Scan(&stored)
		if err != nil {
			t.Fatalf("select kind for target=%s: %v", c.target, err)
		}
		if stored != string(c.kind) {
			t.Errorf("kind mismatch: stored %q, expected %q", stored, c.kind)
		}
	}
}

// --- Phase 3 indexer wiring (#704) -----------------------------------------
//
// These tests assert block_references row state after IndexFileBlocks and
// IndexScanResults run. They pin the storage contract directly so a
// regression in the extraction helper or cascade surfaces independently
// of the backlinks query path.

// countEdgesFromSource returns the number of block_references rows for a
// given source_block_id (across all kinds and targets).
func countEdgesFromSource(t *testing.T, dm *DatabaseManager, sourceID string) int {
	t.Helper()
	var n int
	if err := dm.SQLDB().QueryRow("SELECT COUNT(*) FROM block_references WHERE source_block_id = ?", sourceID).Scan(&n); err != nil {
		t.Fatalf("count edges from %s: %v", sourceID, err)
	}
	return n
}

// TestBlockReferences_IndexerPopulatesBothKinds asserts IndexFileBlocks
// emits one row per distinct (target, kind) edge extracted from RawText,
// covering block-ref + embed coexistence.
func TestBlockReferences_IndexerPopulatesBothKinds(t *testing.T) {
	dm := newTestDB(t)
	idx(t, dm, "vault", "NB", "Sec", "Source", []parser.ParsedBlock{
		noteBlock(uuidA, "ref "+("(("+uuidB+"))")+" plus embed "+"{{embed:"+uuidC+"}}"),
	})

	assertEdgeExists(t, dm, uuidA, uuidB, "block-ref")
	assertEdgeExists(t, dm, uuidA, uuidC, "embed")
	if got := countEdgesFromSource(t, dm, uuidA); got != 2 {
		t.Errorf("expected 2 edges from %s, got %d", uuidA, got)
	}
}

// TestBlockReferences_IndexerCollapsesDuplicateSameKind asserts that two
// tokens of the same kind to the SAME target in one block produce one edge
// (PK dedupe via INSERT OR IGNORE).
func TestBlockReferences_IndexerCollapsesDuplicateSameKind(t *testing.T) {
	dm := newTestDB(t)
	idx(t, dm, "vault", "NB", "Sec", "Source", []parser.ParsedBlock{
		noteBlock(uuidA, "(("+uuidB+")) then (("+uuidB+")) again"),
	})
	if got := countEdgesFromSource(t, dm, uuidA); got != 1 {
		t.Fatalf("expected 1 deduped block-ref edge, got %d", got)
	}
	assertEdgeExists(t, dm, uuidA, uuidB, "block-ref")
}

// TestBlockReferences_IndexerIncludesCodeBlocks asserts that CODE blocks
// contribute edges — the extractor walks RawText of every block row,
// diverging from page_links which skips CODE. Pins the all-type scan
// contract that the backlinks panel relies on.
func TestBlockReferences_IndexerIncludesCodeBlocks(t *testing.T) {
	dm := newTestDB(t)
	idx(t, dm, "vault", "NB", "Sec", "Source", []parser.ParsedBlock{
		{ID: uuidA, Type: parser.BlockCode, RawText: "code ((" + uuidB + ")) {{embed:" + uuidC + "}} end", CleanText: "code", LineNumber: 1},
	})
	assertEdgeExists(t, dm, uuidA, uuidB, "block-ref")
	assertEdgeExists(t, dm, uuidA, uuidC, "embed")
}

// TestBlockReferences_IndexerRetainsDanglingTarget asserts that indexing a
// source block referencing a not-yet-indexed target still records the edge.
// The backlink resolves when the target is later indexed (Phase 4 lookup).
func TestBlockReferences_IndexerRetainsDanglingTarget(t *testing.T) {
	dm := newTestDB(t)
	// uuidB is NOT indexed as a block — pure dangling reference.
	idx(t, dm, "vault", "NB", "Sec", "Source", []parser.ParsedBlock{
		noteBlock(uuidA, "dangling "+("(("+uuidB+"))")),
	})
	assertEdgeExists(t, dm, uuidA, uuidB, "block-ref")
}

// TestBlockReferences_IndexerSourceReindexClearsStaleEdges asserts that
// re-indexing a source page after a block-ref was edited away drops the
// prior edge. The cascade through DELETE FROM blocks by ID must remove the
// stale rows before the new (smaller) edge set is inserted.
func TestBlockReferences_IndexerSourceReindexClearsStaleEdges(t *testing.T) {
	dm := newTestDB(t)
	idx(t, dm, "vault", "NB", "Sec", "Source", []parser.ParsedBlock{
		noteBlock(uuidA, "ref "+("(("+uuidB+"))")+" and embed "+"{{embed:"+uuidC+"}}"),
	})
	if got := countEdgesFromSource(t, dm, uuidA); got != 2 {
		t.Fatalf("baseline: expected 2 edges, got %d", got)
	}

	// Re-index Source with the block-ref removed (same block ID, new raw).
	idx(t, dm, "vault", "NB", "Sec", "Source", []parser.ParsedBlock{
		noteBlock(uuidA, "ref is gone, only embed "+"{{embed:"+uuidC+"}}"),
	})
	// Stale block-ref edge must be cleared by the cascade; embed survives.
	if got := countEdgesFromSource(t, dm, uuidA); got != 1 {
		t.Fatalf("expected 1 edge after re-index (embed only), got %d", got)
	}
	assertEdgeExists(t, dm, uuidA, uuidC, "embed")
}

// TestBlockReferences_IndexerClearFileBlocksCascades asserts that
// ClearFileBlocks (the page-scoped clear path used by SaveFileBlocks and
// the watcher) cascades through the source FK and removes all of the
// page's source edges.
func TestBlockReferences_IndexerClearFileBlocksCascades(t *testing.T) {
	dm := newTestDB(t)
	idx(t, dm, "vault", "NB", "Sec", "Source", []parser.ParsedBlock{
		noteBlock(uuidA, "ref "+("(("+uuidB+"))")),
		noteBlock(uuidC, "embed "+"{{embed:"+uuidB+"}}"),
	})
	if got := countBlockReferences(t, dm); got != 2 {
		t.Fatalf("baseline: expected 2 edges, got %d", got)
	}

	if err := dm.ClearFileBlocks(nil, "vault", "NB", "Sec", "Source"); err != nil {
		t.Fatalf("ClearFileBlocks: %v", err)
	}
	if got := countBlockReferences(t, dm); got != 0 {
		t.Fatalf("ClearFileBlocks should have cascaded all source edges, got %d", got)
	}
}

// TestBlockReferences_IndexerDeleteBlockFromPageCascades asserts the
// single-block delete path also cascades through the source FK.
func TestBlockReferences_IndexerDeleteBlockFromPageCascades(t *testing.T) {
	dm := newTestDB(t)
	idx(t, dm, "vault", "NB", "Sec", "Source", []parser.ParsedBlock{
		noteBlock(uuidA, "ref "+("(("+uuidB+"))")),
		noteBlock(uuidC, "embed "+"{{embed:"+uuidB+"}}"),
	})
	if err := dm.DeleteBlockFromPage(uuidA, "vault", "NB", "Sec", "Source"); err != nil {
		t.Fatalf("DeleteBlockFromPage: %v", err)
	}
	// Only uuidA's edge should be cleared; uuidC's embed survives.
	if got := countEdgesFromSource(t, dm, uuidA); got != 0 {
		t.Errorf("uuidA edges should be cleared, got %d", got)
	}
	assertEdgeExists(t, dm, uuidC, uuidB, "embed")
}

// TestBlockReferences_IndexerClearSourceBlocksCascades asserts the
// source-scoped clear path (UnlinkNotebook) cascades through the source FK.
func TestBlockReferences_IndexerClearSourceBlocksCascades(t *testing.T) {
	dm := newTestDB(t)
	idx(t, dm, "linked:ext", "NB", "Sec", "Source", []parser.ParsedBlock{
		noteBlock(uuidA, "ref "+("(("+uuidB+"))")),
	})
	// A vault source block also referencing uuidB should survive.
	idx(t, dm, "vault", "NB", "Sec", "VaultSrc", []parser.ParsedBlock{
		noteBlock(uuidC, "vault ref "+("(("+uuidB+"))")),
	})
	if got := countBlockReferences(t, dm); got != 2 {
		t.Fatalf("baseline: expected 2 edges across sources, got %d", got)
	}

	if err := dm.ClearSourceBlocks("linked:ext"); err != nil {
		t.Fatalf("ClearSourceBlocks: %v", err)
	}
	// Only the linked:ext edge should be cleared; vault edge survives.
	assertEdgeExists(t, dm, uuidC, uuidB, "block-ref")
	if got := countEdgesFromSource(t, dm, uuidA); got != 0 {
		t.Errorf("linked:ext source edge should be cleared, got %d", got)
	}
}

// TestClearSourceBlocks_ClearsTypedProjection pins the transactional cleanup
// contract for UnlinkNotebook: ClearSourceBlocks must drop not only the source's
// blocks rows but also its page_types/page_properties projection rows in the
// SAME transaction, so an unlinked notebook's pages cannot linger as ghosts in
// the type dashboards (QueryPagesByType does not JOIN blocks). The block_references
// cascade is covered by TestBlockReferences_IndexerClearSourceBlocksCascades; this
// test asserts the typed-projection tables specifically.
func TestClearSourceBlocks_ClearsTypedProjection(t *testing.T) {
	dm := newTestDB(t)

	// Seed one block for the linked source and project a typed page for it.
	idx(t, dm, "linked:ext", "Work", "Sprint", "Board", []parser.ParsedBlock{
		noteBlock(uuidA, "linked content"),
	})
	if err := dm.IndexPageProjection("linked:ext", "Work", "Sprint", "Board", "task",
		[]ProjectedProperty{{Property: "owner", ValueText: "Alice", ValueSort: "Alice", ValueType: "text"}},
	); err != nil {
		t.Fatalf("IndexPageProjection(linked): %v", err)
	}
	// A vault-source page of the same type must survive the linked clear.
	if err := dm.IndexPageProjection("vault", "Work", "Notes", "Daily", "task",
		[]ProjectedProperty{{Property: "owner", ValueText: "Bob", ValueSort: "Bob", ValueType: "text"}},
	); err != nil {
		t.Fatalf("IndexPageProjection(vault): %v", err)
	}

	// Sanity-check the seed: each of the three tables has the linked row.
	for _, table := range []string{"blocks", "page_types", "page_properties"} {
		var c int
		if err := dm.SQLDB().QueryRow("SELECT COUNT(*) FROM "+table+" WHERE source = ?", "linked:ext").Scan(&c); err != nil {
			t.Fatalf("seed count %s: %v", table, err)
		}
		if c == 0 {
			t.Fatalf("seed failed: %s has 0 rows for linked:ext", table)
		}
	}

	if err := dm.ClearSourceBlocks("linked:ext"); err != nil {
		t.Fatalf("ClearSourceBlocks: %v", err)
	}

	// All three tables must be empty for the cleared source.
	for _, table := range []string{"blocks", "page_types", "page_properties"} {
		var c int
		if err := dm.SQLDB().QueryRow("SELECT COUNT(*) FROM "+table+" WHERE source = ?", "linked:ext").Scan(&c); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if c != 0 {
			t.Errorf("expected 0 rows in %s for linked:ext after ClearSourceBlocks, got %d", table, c)
		}
	}

	// The vault projection survives — only the linked source was cleared.
	pages, err := dm.QueryPagesByType("task")
	if err != nil {
		t.Fatalf("QueryPagesByType: %v", err)
	}
	if len(pages) != 1 {
		t.Fatalf("expected only the vault page to remain, got %d pages", len(pages))
	}
	if pages[0].Source != "vault" {
		t.Errorf("remaining page source = %q, want vault", pages[0].Source)
	}

	// ClearSourceBlocks("") is a documented no-op (never clears the vault).
	if err := dm.ClearSourceBlocks(""); err != nil {
		t.Errorf("ClearSourceBlocks(\"\") should be a no-op: %v", err)
	}
	if pages, err := dm.QueryPagesByType("task"); err != nil || len(pages) != 1 {
		t.Errorf("empty-source clear should not wipe the vault projection: pages=%v err=%v", pages, err)
	}
}

// TestBlockReferences_IndexerTargetDeletionKeepsEdge asserts the
// source-only-FK asymmetry from the indexer side: deleting the target
// block from `blocks` does NOT cascade through block_references. The edge
// survives and re-resolves if the target is re-indexed later.
func TestBlockReferences_IndexerTargetDeletionKeepsEdge(t *testing.T) {
	dm := newTestDB(t)
	idx(t, dm, "vault", "NB", "Sec", "Source", []parser.ParsedBlock{
		noteBlock(uuidA, "ref "+("(("+uuidB+"))")),
	})
	idx(t, dm, "vault", "NB", "Sec", "Target", []parser.ParsedBlock{
		noteBlock(uuidB, "target"),
	})

	if _, err := dm.SQLDB().Exec("DELETE FROM blocks WHERE id = ?", uuidB); err != nil {
		t.Fatalf("delete target block: %v", err)
	}
	// Edge must survive — target FK is intentionally absent.
	assertEdgeExists(t, dm, uuidA, uuidB, "block-ref")
}

// TestBlockReferences_IndexerScanResultsParityDirect asserts that
// IndexScanResults (the batched cold-start path) populates block_references
// identically to IndexFileBlocks for the same fixture. Direct table
// assertion — the backlinks-output parity test in Phase 1 covers the
// end-to-end contract.
func TestBlockReferences_IndexerScanResultsParityDirect(t *testing.T) {
	target := noteBlock(uuidA, "target")
	srcBlockRef := noteBlock(uuidB, "ref "+("(("+uuidA+"))"))
	srcEmbed := noteBlock(uuidC, "embed "+"{{embed:"+uuidA+"}}")

	dmFile := newTestDB(t)
	idx(t, dmFile, "vault", "NB", "Sec", "Target", []parser.ParsedBlock{target})
	idx(t, dmFile, "vault", "NB", "Sec", "BR", []parser.ParsedBlock{srcBlockRef})
	idx(t, dmFile, "vault", "NB", "Sec", "EM", []parser.ParsedBlock{srcEmbed})

	dmScan := newTestDB(t)
	results := []parser.ScanResult{
		{Path: "/v/NB/Sec/Target.md", Notebook: "NB", Section: "Sec", Page: "Target", Blocks: []parser.ParsedBlock{target}},
		{Path: "/v/NB/Sec/BR.md", Notebook: "NB", Section: "Sec", Page: "BR", Blocks: []parser.ParsedBlock{srcBlockRef}},
		{Path: "/v/NB/Sec/EM.md", Notebook: "NB", Section: "Sec", Page: "EM", Blocks: []parser.ParsedBlock{srcEmbed}},
	}
	if _, _, err := dmScan.IndexScanResults(results); err != nil {
		t.Fatalf("IndexScanResults: %v", err)
	}

	if fileCount, scanCount := countBlockReferences(t, dmFile), countBlockReferences(t, dmScan); fileCount != scanCount {
		t.Fatalf("edge count mismatch: file=%d scan=%d", fileCount, scanCount)
	}
	// Both must have produced exactly the two expected edges (block-ref + embed).
	assertEdgeExists(t, dmScan, uuidB, uuidA, "block-ref")
	assertEdgeExists(t, dmScan, uuidC, uuidA, "embed")
}

// TestBlockReferences_IndexerChangedSourceContentReplacesEdges asserts the
// full delete-then-insert replacement flow: a block whose raw_content
// changes from {A,B} edges to {C,D} ends up with exactly {C,D} — no stale
// leftovers and no missing new edges.
func TestBlockReferences_IndexerChangedSourceContentReplacesEdges(t *testing.T) {
	dm := newTestDB(t)
	idx(t, dm, "vault", "NB", "Sec", "Source", []parser.ParsedBlock{
		noteBlock(uuidA, "(("+uuidB+")) and (("+uuidC+"))"),
	})
	if got := countEdgesFromSource(t, dm, uuidA); got != 2 {
		t.Fatalf("baseline: expected 2 edges, got %d", got)
	}

	// Re-index with a completely different edge set.
	idx(t, dm, "vault", "NB", "Sec", "Source", []parser.ParsedBlock{
		noteBlock(uuidA, "(("+uuidD+")) and {{embed:"+uuidE+"}}"),
	})
	if got := countEdgesFromSource(t, dm, uuidA); got != 2 {
		t.Fatalf("after re-index: expected 2 edges, got %d", got)
	}
	assertEdgeExists(t, dm, uuidA, uuidD, "block-ref")
	assertEdgeExists(t, dm, uuidA, uuidE, "embed")
	// Stale edges to uuidB and uuidC must be gone.
	if got := countEdgesFromSource(t, dm, uuidA); got != 2 {
		t.Errorf("stale edges leaked: %d", got)
	}
	var staleCount int
	if err := dm.SQLDB().QueryRow("SELECT COUNT(*) FROM block_references WHERE target_block_id IN (?, ?)", uuidB, uuidC).Scan(&staleCount); err != nil {
		t.Fatalf("count stale: %v", err)
	}
	if staleCount != 0 {
		t.Errorf("stale edges to uuidB/uuidC must be cleared, got %d", staleCount)
	}
}

// --- Shared extraction helper (#704 review) --------------------------------

// TestExtractBlockRefEdges pins the pure extraction contract shared by
// indexBlockReferences (live indexer) and backfillBlockReferences (one-shot
// migration). Both callers depend on this returning exactly one (target, kind)
// tuple per distinct regex match; a drift here would desynchronize the two
// paths silently.
func TestExtractBlockRefEdges(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want []blockRefEdge
	}{
		{
			name: "empty",
			raw:  "",
			want: nil,
		},
		{
			name: "no tokens",
			raw:  "plain text with no refs",
			want: nil,
		},
		{
			name: "single block-ref",
			raw:  "see ((" + uuidA + ")) here",
			want: []blockRefEdge{{uuidA, BacklinkBlockRef}},
		},
		{
			name: "single embed",
			raw:  "{{embed:" + uuidA + "}}",
			want: []blockRefEdge{{uuidA, BacklinkEmbed}},
		},
		{
			name: "block-ref + embed coexist",
			raw:  "((" + uuidA + ")) and {{embed:" + uuidB + "}}",
			want: []blockRefEdge{
				{uuidA, BacklinkBlockRef},
				{uuidB, BacklinkEmbed},
			},
		},
		{
			name: "duplicate same-kind tokens preserved (PK dedupe is DB-side)",
			raw:  "((" + uuidA + ")) then ((" + uuidA + ")) again",
			want: []blockRefEdge{
				{uuidA, BacklinkBlockRef},
				{uuidA, BacklinkBlockRef},
			},
		},
		{
			name: "bare UUID without delimiters is ignored",
			raw:  "the id is " + uuidA + " but not wrapped",
			want: nil,
		},
		{
			name: "non-embed {{other:uuid}} syntax is ignored",
			raw:  "{{other:" + uuidA + "}}",
			want: nil,
		},
		{
			name: "multiple block-refs to different targets",
			raw:  "((" + uuidA + ")) ((" + uuidB + ")) ((" + uuidC + "))",
			want: []blockRefEdge{
				{uuidA, BacklinkBlockRef},
				{uuidB, BacklinkBlockRef},
				{uuidC, BacklinkBlockRef},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractBlockRefEdges(tc.raw)
			if len(got) != len(tc.want) {
				t.Fatalf("edge count: got %d, want %d (%+v)", len(got), len(tc.want), got)
			}
			for i, e := range got {
				if e != tc.want[i] {
					t.Errorf("edge[%d]: got {%s, %s}, want {%s, %s}",
						i, e.targetID, e.kind, tc.want[i].targetID, tc.want[i].kind)
				}
			}
		})
	}
}
