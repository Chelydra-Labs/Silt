package db

import (
	"errors"
	"sync/atomic"
	"testing"

	"silt/backend/parser"
)

// TestIndexFileWithProjection_AtomicReplace proves the unified atomic path:
// in one transaction, prior blocks AND prior projection are cleared and the
// new blocks AND new projection are inserted. A reader querying after Commit
// sees both halves together; there is no intermediate state.
func TestIndexFileWithProjection_AtomicReplace(t *testing.T) {
	dm := newTestDB(t)

	const (
		notebook = "Work"
		section  = "Sprint"
		page     = "Board"
		source   = "vault"
	)
	oldBlock := sampleNoteBlock("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", 1)
	newBlock := sampleNoteBlock("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb", 1)

	// Seed prior state directly via the standalone paths.
	if err := dm.IndexFileBlocks(source, notebook, section, page,
		[]parser.ParsedBlock{oldBlock}, nil); err != nil {
		t.Fatalf("seed blocks: %v", err)
	}
	if err := dm.IndexPageProjection(source, notebook, section, page, "task",
		[]ProjectedProperty{{Property: "owner", ValueText: "Alice", ValueSort: "Alice", ValueType: "text"}}); err != nil {
		t.Fatalf("seed projection: %v", err)
	}

	// Atomic replace: new block + new projection in one tx.
	newProps := []ProjectedProperty{
		{Property: "status", ValueText: "done", ValueSort: "done", ValueType: "select"},
	}
	if err := dm.IndexFileWithProjection(source, notebook, section, page,
		[]parser.ParsedBlock{newBlock}, nil, "meeting", newProps); err != nil {
		t.Fatalf("IndexFileWithProjection: %v", err)
	}

	// Both halves reflect the new state.
	var newBlockN, oldBlockN int
	dm.SQLDB().QueryRow("SELECT COUNT(*) FROM blocks WHERE id = ?", newBlock.ID).Scan(&newBlockN)
	dm.SQLDB().QueryRow("SELECT COUNT(*) FROM blocks WHERE id = ?", oldBlock.ID).Scan(&oldBlockN)
	if newBlockN != 1 || oldBlockN != 0 {
		t.Errorf("blocks: new=%d old=%d, want new=1 old=0", newBlockN, oldBlockN)
	}

	row, err := dm.GetPageProjection(source, notebook, section, page)
	if err != nil {
		t.Fatalf("GetPageProjection: %v", err)
	}
	if row == nil || row.TypeName != "meeting" {
		t.Fatalf("projection: %+v, want type=meeting", row)
	}
	if len(row.Properties) != 1 || row.Properties[0].Property != "status" {
		t.Errorf("properties = %+v, want [status]", row.Properties)
	}
}

// TestIndexFileWithProjection_UntypedClearsProjection proves the symmetric
// half: when the caller passes typeID="" (frontmatter lost its `type:` line),
// the projection is cleared atomically with the block rewrite. Without this,
// a page that loses its type would linger on the dashboard until a separate
// ClearPageProjection call catches up.
func TestIndexFileWithProjection_UntypedClearsProjection(t *testing.T) {
	dm := newTestDB(t)

	const (
		notebook = "Work"
		section  = "Sprint"
		page     = "Board"
		source   = "vault"
	)
	// Seed: typed page with one block.
	if err := dm.IndexFileWithProjection(source, notebook, section, page,
		[]parser.ParsedBlock{sampleNoteBlock("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", 1)},
		nil, "task",
		[]ProjectedProperty{{Property: "owner", ValueText: "Alice", ValueSort: "Alice", ValueType: "text"}}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Atomic transition to untyped: same page, typeID="".
	if err := dm.IndexFileWithProjection(source, notebook, section, page,
		[]parser.ParsedBlock{sampleNoteBlock("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb", 1)},
		nil, "", nil); err != nil {
		t.Fatalf("untyped reindex: %v", err)
	}

	// Projection is gone; new block present.
	row, err := dm.GetPageProjection(source, notebook, section, page)
	if err != nil {
		t.Fatalf("GetPageProjection: %v", err)
	}
	if row != nil {
		t.Errorf("projection must be cleared on untyped reindex, got %+v", row)
	}
	for _, table := range []string{"page_types", "page_properties"} {
		var n int
		if err := dm.SQLDB().QueryRow(
			"SELECT COUNT(*) FROM "+table+" WHERE source = ? AND notebook = ? AND section = ? AND page = ?",
			source, notebook, section, page,
		).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if n != 0 {
			t.Errorf("%s rows = %d after untyped reindex, want 0", table, n)
		}
	}
	var blockN int
	dm.SQLDB().QueryRow("SELECT COUNT(*) FROM blocks WHERE id = ?", "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb").Scan(&blockN)
	if blockN != 1 {
		t.Errorf("new block missing: got %d, want 1", blockN)
	}
}

// TestIndexFileWithProjection_PreservesOldStateOnHookFailure is the
// load-bearing atomic rollback evidence: a hook that forces a failure after
// both halves are staged but before commit must roll back BOTH halves. The
// prior committed block AND projection remain visible; no new partial state
// leaks. This is the property #865 was filed for.
func TestIndexFileWithProjection_PreservesOldStateOnHookFailure(t *testing.T) {
	dm := newTestDB(t)

	const (
		notebook = "Work"
		section  = "Sprint"
		page     = "Board"
		source   = "vault"
	)
	oldID := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	newID := "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"

	// Seed prior committed state via the atomic path itself.
	if err := dm.IndexFileWithProjection(source, notebook, section, page,
		[]parser.ParsedBlock{sampleNoteBlock(oldID, 1)}, nil, "task",
		[]ProjectedProperty{{Property: "owner", ValueText: "Alice", ValueSort: "Alice", ValueType: "text"}}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Inject failure at the unified pre-commit seam.
	dm.setIndexerTestingHook(func(ctx indexerHookContext) error {
		if ctx.Phase == indexerHookIndexFileWithProjectionPreCommit &&
			ctx.Notebook == notebook && ctx.Section == section && ctx.Page == page {
			return sentinelHookErr
		}
		return nil
	})
	defer dm.setIndexerTestingHook(nil)

	err := dm.IndexFileWithProjection(source, notebook, section, page,
		[]parser.ParsedBlock{sampleNoteBlock(newID, 1)}, nil, "meeting",
		[]ProjectedProperty{{Property: "status", ValueText: "done", ValueSort: "done", ValueType: "select"}})
	if !errors.Is(err, sentinelHookErr) {
		t.Fatalf("expected sentinel wrap, got %v", err)
	}

	// Both halves rolled back: old block visible, new block absent.
	var oldN, newN int
	dm.SQLDB().QueryRow("SELECT COUNT(*) FROM blocks WHERE id = ?", oldID).Scan(&oldN)
	dm.SQLDB().QueryRow("SELECT COUNT(*) FROM blocks WHERE id = ?", newID).Scan(&newN)
	if oldN != 1 {
		t.Errorf("old block missing after rollback: %d", oldN)
	}
	if newN != 0 {
		t.Errorf("new block committed despite hook abort: %d", newN)
	}

	// Prior projection (task/Alice) survives intact.
	row, err := dm.GetPageProjection(source, notebook, section, page)
	if err != nil {
		t.Fatalf("GetPageProjection: %v", err)
	}
	if row == nil || row.TypeName != "task" {
		t.Fatalf("projection drifted after rollback: %+v", row)
	}
	if len(row.Properties) != 1 || row.Properties[0].Property != "owner" ||
		row.Properties[0].ValueText != "Alice" {
		t.Errorf("prior projection property did not survive: %+v", row.Properties)
	}
}

// TestIndexFileWithProjection_ReaderNeverSeesHalfState is the concurrency
// visibility evidence. The hook pauses the writer AFTER both halves are
// staged but BEFORE commit; a SEPARATE read-only WAL connection (mirrors the
// plugin SDK's openPluginRODB) samples blocks AND projection during the
// pause. It must observe ONLY the prior committed state — never
// new-blocks-without-new-projection or vice versa. WAL readers see the last
// committed snapshot and do not block on the in-flight tx, which is exactly
// the production property that makes the atomic publish valuable.
//
// Uses a counter gate (not a sleep) so the test is deterministic and
// flake-free under -race.
func TestIndexFileWithProjection_ReaderNeverSeesHalfState(t *testing.T) {
	dm, dbPath := newOnDiskDB(t)

	const (
		notebook = "Work"
		section  = "Sprint"
		page     = "Board"
		source   = "vault"
	)
	// Prior committed state: task page, one block.
	if err := dm.IndexFileWithProjection(source, notebook, section, page,
		[]parser.ParsedBlock{sampleNoteBlock("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", 1)},
		nil, "task",
		[]ProjectedProperty{{Property: "owner", ValueText: "Alice", ValueSort: "Alice", ValueType: "text"}}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Open a second read-only connection the way the plugin SDK does. WAL
	// readers see the last committed state; they do not block on the
	// writer's in-flight transaction.
	ro, err := openRawReadonly(t, dbPath)
	if err != nil {
		t.Fatalf("open reader: %v", err)
	}
	defer ro.Close()

	// Counter gate: the writer's hook waits until the reader has finished
	// its pause-loop observations, then releases the commit.
	var readerDone atomic.Int32
	hookEntered := make(chan struct{})
	dm.setIndexerTestingHook(func(ctx indexerHookContext) error {
		if ctx.Phase != indexerHookIndexFileWithProjectionPreCommit {
			return nil
		}
		select {
		case hookEntered <- struct{}{}:
		default:
		}
		// Spin until the reader signals it has finished sampling. A sleep
		// would make this flaky; the gate is deterministic.
		for readerDone.Load() == 0 {
		}
		return nil
	})
	defer dm.setIndexerTestingHook(nil)

	// Writer goroutine: stages new block + new projection, pauses at the
	// hook, then commits.
	writerDone := make(chan error, 1)
	go func() {
		writerDone <- dm.IndexFileWithProjection(source, notebook, section, page,
			[]parser.ParsedBlock{sampleNoteBlock("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb", 1)},
			nil, "meeting",
			[]ProjectedProperty{{Property: "status", ValueText: "done", ValueSort: "done", ValueType: "select"}})
	}()

	// Wait for the writer to reach the hook (blocks+projection staged,
	// pre-commit).
	<-hookEntered

	// Sample blocks+projection repeatedly via the WAL reader while the
	// writer is paused. Each sample must reflect the PRIOR committed state —
	// never a mix of new-blocks/old-projection or vice versa.
	for i := 0; i < 20; i++ {
		var newBlockN int
		if err := ro.QueryRow("SELECT COUNT(*) FROM blocks WHERE id = ?", "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb").Scan(&newBlockN); err != nil {
			t.Fatalf("sample %d: reader query blocks: %v", i, err)
		}
		var typeName string
		if err := ro.QueryRow(
			"SELECT type_name FROM page_types WHERE source = ? AND notebook = ? AND section = ? AND page = ?",
			source, notebook, section, page,
		).Scan(&typeName); err != nil {
			t.Fatalf("sample %d: reader query projection: %v", i, err)
		}
		// Pre-commit invariant: new block invisible, prior projection intact.
		if newBlockN != 0 {
			t.Fatalf("sample %d: reader saw new block pre-commit (%d) — tx is leaking uncommitted state", i, newBlockN)
		}
		if typeName != "task" {
			t.Fatalf("sample %d: reader saw projection drift pre-commit: %q (want task)", i, typeName)
		}
	}

	// Release the writer.
	readerDone.Store(1)
	if err := <-writerDone; err != nil {
		t.Fatalf("writer commit: %v", err)
	}

	// Post-commit: the WAL reader observes the new state atomically. Both
	// halves appear together.
	var newBlockN int
	if err := ro.QueryRow("SELECT COUNT(*) FROM blocks WHERE id = ?", "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb").Scan(&newBlockN); err != nil {
		t.Fatalf("post-commit reader blocks: %v", err)
	}
	if newBlockN != 1 {
		t.Errorf("post-commit: reader saw new block count %d, want 1", newBlockN)
	}
	var typeName string
	if err := ro.QueryRow(
		"SELECT type_name FROM page_types WHERE source = ? AND notebook = ? AND section = ? AND page = ?",
		source, notebook, section, page,
	).Scan(&typeName); err != nil {
		t.Fatalf("post-commit reader projection: %v", err)
	}
	if typeName != "meeting" {
		t.Errorf("post-commit: reader saw projection type %q, want meeting", typeName)
	}
}

// TestIndexScanResultsWithProjection_AtomicBatch proves the batched atomic
// path: every result's blocks AND projection land in one transaction. A
// failure for any one result rolls back the whole batch — no partial state.
func TestIndexScanResultsWithProjection_AtomicBatch(t *testing.T) {
	dm := newTestDB(t)

	// Two typed pages, batched atomically.
	results := []parser.ScanResult{{
		Notebook: "Work", Section: "Sprint", Page: "Board1",
		Blocks: []parser.ParsedBlock{sampleNoteBlock("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", 1)},
	}, {
		Notebook: "Work", Section: "Sprint", Page: "Board2",
		Blocks: []parser.ParsedBlock{sampleNoteBlock("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb", 1)},
	}}
	projections := []ScanProjection{
		{TypeID: "task", Props: []ProjectedProperty{{Property: "owner", ValueText: "Alice", ValueSort: "Alice", ValueType: "text"}}},
		{TypeID: "task", Props: []ProjectedProperty{{Property: "owner", ValueText: "Bob", ValueSort: "Bob", ValueType: "text"}}},
	}
	count, _, err := dm.IndexScanResultsWithProjection(results, projections)
	if err != nil {
		t.Fatalf("IndexScanResultsWithProjection: %v", err)
	}
	if count != 2 {
		t.Errorf("indexedCount = %d, want 2", count)
	}

	// Both pages have their projection written atomically with the blocks.
	for _, p := range []struct{ page, owner string }{{"Board1", "Alice"}, {"Board2", "Bob"}} {
		row, err := dm.GetPageProjection("vault", "Work", "Sprint", p.page)
		if err != nil {
			t.Fatalf("GetPageProjection %s: %v", p.page, err)
		}
		if row == nil || row.TypeName != "task" || len(row.Properties) != 1 || row.Properties[0].ValueText != p.owner {
			t.Errorf("page %s projection: %+v, want task/%s", p.page, row, p.owner)
		}
	}
}

// TestIndexScanResultsWithProjection_RollsBackBatchAndProjection proves a
// forced mid-batch failure rolls back blocks AND projection for every result
// processed so far in the batch — no partial state survives.
func TestIndexScanResultsWithProjection_RollsBackBatchAndProjection(t *testing.T) {
	dm := newTestDB(t)

	// Seed prior state for one page.
	seedResults := []parser.ScanResult{{
		Notebook: "Work", Section: "Sprint", Page: "Board",
		Blocks: []parser.ParsedBlock{sampleNoteBlock("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", 1)},
	}}
	seedProj := []ScanProjection{
		{TypeID: "task", Props: []ProjectedProperty{{Property: "owner", ValueText: "Alice", ValueSort: "Alice", ValueType: "text"}}},
	}
	if _, _, err := dm.IndexScanResultsWithProjection(seedResults, seedProj); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Inject failure at the batched pre-commit seam.
	dm.setIndexerTestingHook(func(ctx indexerHookContext) error {
		if ctx.Phase == indexerHookIndexScanResultsWithProjectionPreCommit {
			return sentinelHookErr
		}
		return nil
	})
	defer dm.setIndexerTestingHook(nil)

	// Re-batch with a new block + new projection. The hook must abort.
	replResults := []parser.ScanResult{{
		Notebook: "Work", Section: "Sprint", Page: "Board",
		Blocks: []parser.ParsedBlock{sampleNoteBlock("cccccccc-cccc-cccc-cccc-cccccccccccc", 1)},
	}}
	replProj := []ScanProjection{
		{TypeID: "meeting", Props: []ProjectedProperty{{Property: "status", ValueText: "done", ValueSort: "done", ValueType: "select"}}},
	}
	if _, _, err := dm.IndexScanResultsWithProjection(replResults, replProj); !errors.Is(err, sentinelHookErr) {
		t.Fatalf("expected sentinel wrap, got %v", err)
	}

	// Prior block + projection survive intact.
	var oldN, newN int
	dm.SQLDB().QueryRow("SELECT COUNT(*) FROM blocks WHERE id = ?", "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa").Scan(&oldN)
	dm.SQLDB().QueryRow("SELECT COUNT(*) FROM blocks WHERE id = ?", "cccccccc-cccc-cccc-cccc-cccccccccccc").Scan(&newN)
	if oldN != 1 {
		t.Errorf("old block missing after rollback: %d", oldN)
	}
	if newN != 0 {
		t.Errorf("new block committed despite hook abort: %d", newN)
	}
	row, err := dm.GetPageProjection("vault", "Work", "Sprint", "Board")
	if err != nil {
		t.Fatalf("GetPageProjection: %v", err)
	}
	if row == nil || row.TypeName != "task" || len(row.Properties) != 1 ||
		row.Properties[0].Property != "owner" || row.Properties[0].ValueText != "Alice" {
		t.Errorf("prior projection did not survive batch rollback: %+v", row)
	}
}

// TestIndexScanResultsWithProjection_LengthMismatch guards the input
// contract: projections must be the same length as results. A mismatch is a
// programmer error and must fail loudly BEFORE opening a transaction
// (otherwise a partial batch could commit before the length mismatch is
// noticed).
func TestIndexScanResultsWithProjection_LengthMismatch(t *testing.T) {
	dm := newTestDB(t)
	results := []parser.ScanResult{{
		Notebook: "Work", Section: "Sprint", Page: "Board",
		Blocks: []parser.ParsedBlock{sampleNoteBlock("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", 1)},
	}}
	// Wrong length: 0 vs 1.
	if _, _, err := dm.IndexScanResultsWithProjection(results, nil); err == nil {
		t.Fatal("expected length-mismatch error, got nil")
	}
}

// TestIndexFileWithProjection_PreservesIndexFileBlocksBehavior pins the
// contract that the block-only path (IndexFileBlocks) is unchanged by the
// refactor: it preserves an existing projection rather than clearing it.
// Block-only callers (task status / deps / recurrence / subtree edits)
// depend on this so a routine task update does not wipe the page's dashboard
// entry.
func TestIndexFileWithProjection_PreservesIndexFileBlocksBehavior(t *testing.T) {
	dm := newTestDB(t)

	const (
		notebook = "Work"
		section  = "Sprint"
		page     = "Board"
		source   = "vault"
	)
	// Seed via the atomic path: typed page.
	if err := dm.IndexFileWithProjection(source, notebook, section, page,
		[]parser.ParsedBlock{sampleTaskBlock("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", 1)},
		nil, "task",
		[]ProjectedProperty{{Property: "owner", ValueText: "Alice", ValueSort: "Alice", ValueType: "text"}}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Block-only reindex (e.g. a task-status edit) via IndexFileBlocks must
	// NOT touch the projection.
	if err := dm.IndexFileBlocks(source, notebook, section, page,
		[]parser.ParsedBlock{sampleTaskBlock("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", 1)}, nil); err != nil {
		t.Fatalf("IndexFileBlocks reindex: %v", err)
	}
	row, err := dm.GetPageProjection(source, notebook, section, page)
	if err != nil {
		t.Fatalf("GetPageProjection: %v", err)
	}
	if row == nil || row.TypeName != "task" || len(row.Properties) != 1 {
		t.Errorf("projection did not survive block-only reindex: %+v", row)
	}
}

// (No package-level guards: the atomic helpers participate in the same
// DatabaseManager synchronization story as the rest of the indexer methods.)
