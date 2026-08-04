package db

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"silt/backend/parser"
)

// sentinelHookErr is the failure tests inject via the seam. A private
// sentinel makes errors.Is identify the seam as the rollback source,
// distinct from any real DB error.
var sentinelHookErr = errors.New("indexer testing hook forced failure")

// TestIndexerTestingHook_IndexFileBlocks_RollsBackAndPreservesState is the
// load-bearing invariant: when a hook forces a failure inside an
// IndexFileBlocks transaction (after the prior rows are cleared and the new
// rows are staged but before commit), the deferred rollback must restore
// the prior committed state — no cleared blocks, no half-inserted new
// blocks, no orphaned projection.
func TestIndexerTestingHook_IndexFileBlocks_RollsBackAndPreservesState(t *testing.T) {
	dm := newTestDB(t)

	const (
		notebook = "Work"
		section  = "Journal"
		page     = "Daily"
		source   = "vault"
	)
	oldID := "11111111-1111-1111-1111-111111111111"
	newID := "22222222-2222-2222-2222-222222222222"

	// Seed the prior committed state: one block + its projection.
	if err := dm.IndexFileBlocks(source, notebook, section, page,
		[]parser.ParsedBlock{sampleNoteBlock(oldID, 1)}, nil); err != nil {
		t.Fatalf("seed IndexFileBlocks: %v", err)
	}
	if err := dm.IndexPageProjection(source, notebook, section, page, "task",
		[]ProjectedProperty{{Property: "owner", ValueText: "Alice", ValueSort: "Alice", ValueType: "text"}}); err != nil {
		t.Fatalf("seed IndexPageProjection: %v", err)
	}

	// Fail only at the IndexFileBlocks pre-commit seam for the target page.
	dm.setIndexerTestingHook(func(ctx indexerHookContext) error {
		if ctx.Phase == indexerHookIndexFileBlocksPreCommit &&
			ctx.Notebook == notebook && ctx.Section == section && ctx.Page == page {
			return sentinelHookErr
		}
		return nil
	})
	defer dm.setIndexerTestingHook(nil)

	// Drive a re-index that would replace oldID with newID. The hook must
	// abort with sentinelHookErr wrapped.
	err := dm.IndexFileBlocks(source, notebook, section, page,
		[]parser.ParsedBlock{sampleNoteBlock(newID, 1)}, nil)
	if !errors.Is(err, sentinelHookErr) {
		t.Fatalf("expected sentinel wrap, got %v", err)
	}

	// Prior committed state must survive: old block visible, new block
	// absent. The clears inside the rolled-back tx were undone.
	assertCount := func(table, id string, want int) {
		t.Helper()
		var n int
		if err := dm.SQLDB().QueryRow("SELECT COUNT(*) FROM "+table+" WHERE id = ?", id).Scan(&n); err != nil {
			t.Fatalf("count %s %s: %v", table, id, err)
		}
		if n != want {
			t.Errorf("%s %s: got %d, want %d", table, id, n, want)
		}
	}
	assertCount("blocks", oldID, 1)
	assertCount("blocks", newID, 0)

	// Projection must be untouched — it lives in a separate transaction
	// today; the rollback inside IndexFileBlocks must not leak across.
	row, err := dm.GetPageProjection(source, notebook, section, page)
	if err != nil {
		t.Fatalf("GetPageProjection after rollback: %v", err)
	}
	if row == nil || row.TypeName != "task" || len(row.Properties) != 1 {
		t.Fatalf("projection drifted after rollback: %+v", row)
	}

	// A clean re-index after clearing the hook must succeed — the seam
	// must not leave the indexer wedged.
	dm.setIndexerTestingHook(nil)
	if err := dm.IndexFileBlocks(source, notebook, section, page,
		[]parser.ParsedBlock{sampleNoteBlock(newID, 1)}, nil); err != nil {
		t.Fatalf("post-hook IndexFileBlocks: %v", err)
	}
	assertCount("blocks", newID, 1)
}

// TestIndexerTestingHook_IndexScanResults_RollsBackBatch verifies the
// batched indexer's pre-commit seam: a forced failure rolls back every
// result in the batch — no partial index survives — and prior state is
// preserved. Coordinates in the hook context are zero for the batched path
// (no single page identifies it).
func TestIndexerTestingHook_IndexScanResults_RollsBackBatch(t *testing.T) {
	dm := newTestDB(t)

	const (
		notebook = "Work"
		section  = "Journal"
		page     = "Daily"
	)
	oldID := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	newID := "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"

	// Seed prior state via the batched path.
	seed := []parser.ScanResult{{
		Notebook: notebook, Section: section, Page: page,
		Blocks: []parser.ParsedBlock{sampleNoteBlock(oldID, 1)},
	}}
	if _, _, err := dm.IndexScanResults(seed); err != nil {
		t.Fatalf("seed IndexScanResults: %v", err)
	}

	// Hook fires for the batched pre-commit seam only, with zero
	// coordinates.
	var batchCtx indexerHookContext
	dm.setIndexerTestingHook(func(ctx indexerHookContext) error {
		if ctx.Phase == indexerHookIndexScanResultsPreCommit {
			batchCtx = ctx
			return sentinelHookErr
		}
		return nil
	})
	defer dm.setIndexerTestingHook(nil)

	repl := []parser.ScanResult{{
		Notebook: notebook, Section: section, Page: page,
		Blocks: []parser.ParsedBlock{sampleNoteBlock(newID, 1)},
	}}
	if _, _, err := dm.IndexScanResults(repl); !errors.Is(err, sentinelHookErr) {
		t.Fatalf("expected sentinel wrap, got %v", err)
	}

	if batchCtx.Source != "" || batchCtx.Notebook != "" || batchCtx.Section != "" || batchCtx.Page != "" {
		t.Errorf("batched hook context must have zero coordinates, got %+v", batchCtx)
	}

	// Prior block survives; new block never committed.
	var n int
	if err := dm.SQLDB().QueryRow("SELECT COUNT(*) FROM blocks WHERE id = ?", oldID).Scan(&n); err != nil {
		t.Fatalf("count old: %v", err)
	}
	if n != 1 {
		t.Errorf("old block missing after rollback: %d", n)
	}
	if err := dm.SQLDB().QueryRow("SELECT COUNT(*) FROM blocks WHERE id = ?", newID).Scan(&n); err != nil {
		t.Fatalf("count new: %v", err)
	}
	if n != 0 {
		t.Errorf("new block committed despite hook abort: %d", n)
	}
}

// TestIndexerTestingHook_IndexPageProjection_RollsBack verifies the
// projection indexer's pre-commit seam: a forced failure rolls back the
// clear+insert and leaves the prior projection row intact, with no
// half-state in either projection table.
func TestIndexerTestingHook_IndexPageProjection_RollsBack(t *testing.T) {
	dm := newTestDB(t)

	const (
		notebook = "Work"
		section  = "Sprint"
		page     = "Board"
		source   = "vault"
	)
	oldProps := []ProjectedProperty{
		{Property: "owner", ValueText: "Alice", ValueSort: "Alice", ValueType: "text"},
	}
	newProps := []ProjectedProperty{
		{Property: "status", ValueText: "done", ValueSort: "done", ValueType: "select"},
	}
	if err := dm.IndexPageProjection(source, notebook, section, page, "task", oldProps); err != nil {
		t.Fatalf("seed: %v", err)
	}

	dm.setIndexerTestingHook(func(ctx indexerHookContext) error {
		if ctx.Phase == indexerHookIndexPageProjectionPreCommit &&
			ctx.Notebook == notebook && ctx.Section == section && ctx.Page == page {
			return sentinelHookErr
		}
		return nil
	})
	defer dm.setIndexerTestingHook(nil)

	if err := dm.IndexPageProjection(source, notebook, section, page, "task", newProps); !errors.Is(err, sentinelHookErr) {
		t.Fatalf("expected sentinel wrap, got %v", err)
	}

	// Prior projection survives intact; new value not committed.
	row, err := dm.GetPageProjection(source, notebook, section, page)
	if err != nil {
		t.Fatalf("GetPageProjection: %v", err)
	}
	if row == nil {
		t.Fatal("projection row vanished after rollback")
	}
	if row.TypeName != "task" {
		t.Errorf("type_name drift: %q", row.TypeName)
	}
	if len(row.Properties) != 1 || row.Properties[0].Property != "owner" ||
		row.Properties[0].ValueText != "Alice" {
		t.Errorf("prior property did not survive rollback: %+v", row.Properties)
	}

	// Both projection tables must carry exactly one row for the page —
	// the rollback must not leave the cleared state behind.
	countRow := func(table string) int {
		var n int
		if err := dm.SQLDB().QueryRow(
			"SELECT COUNT(*) FROM "+table+" WHERE source = ? AND notebook = ? AND section = ? AND page = ?",
			source, notebook, section, page,
		).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		return n
	}
	if n := countRow("page_types"); n != 1 {
		t.Errorf("page_types rows = %d, want 1 (no half-state)", n)
	}
	if n := countRow("page_properties"); n != 1 {
		t.Errorf("page_properties rows = %d, want 1 (no half-state)", n)
	}
}

// TestIndexerTestingHook_PhaseScoped confirms a hook installed for one
// indexer phase does not fire for the others. Without this, a hook that
// injects a failure at IndexFileBlocks could ghost-fire during a follow-up
// IndexPageProjection call (or vice versa) and produce baffling cascades.
// Covers all five phases including the unified atomic paths.
func TestIndexerTestingHook_PhaseScoped(t *testing.T) {
	for _, tc := range []struct {
		name   string
		phase  indexerHookPhase
		driver func(dm *DatabaseManager) error
	}{
		{
			name:  "IndexFileBlocks",
			phase: indexerHookIndexFileBlocksPreCommit,
			driver: func(dm *DatabaseManager) error {
				return dm.IndexFileBlocks("vault", "NB", "S", "P",
					[]parser.ParsedBlock{sampleNoteBlock("11111111-1111-1111-1111-111111111111", 1)}, nil)
			},
		},
		{
			name:  "IndexScanResults",
			phase: indexerHookIndexScanResultsPreCommit,
			driver: func(dm *DatabaseManager) error {
				_, _, err := dm.IndexScanResults([]parser.ScanResult{{
					Notebook: "NB", Section: "S", Page: "P",
					Blocks: []parser.ParsedBlock{sampleNoteBlock("22222222-2222-2222-2222-222222222222", 1)},
				}})
				return err
			},
		},
		{
			name:  "IndexPageProjection",
			phase: indexerHookIndexPageProjectionPreCommit,
			driver: func(dm *DatabaseManager) error {
				return dm.IndexPageProjection("vault", "NB", "S", "P", "task", nil)
			},
		},
		{
			name:  "IndexFileWithProjection",
			phase: indexerHookIndexFileWithProjectionPreCommit,
			driver: func(dm *DatabaseManager) error {
				return dm.IndexFileWithProjection("vault", "NB", "S", "P",
					[]parser.ParsedBlock{sampleNoteBlock("33333333-3333-3333-3333-333333333333", 1)},
					nil, "task", nil)
			},
		},
		{
			name:  "IndexScanResultsWithProjection",
			phase: indexerHookIndexScanResultsWithProjectionPreCommit,
			driver: func(dm *DatabaseManager) error {
				results := []parser.ScanResult{{
					Notebook: "NB", Section: "S", Page: "P",
					Blocks: []parser.ParsedBlock{sampleNoteBlock("44444444-4444-4444-4444-444444444444", 1)},
				}}
				_, _, err := dm.IndexScanResultsWithProjection(results, []ScanProjection{{TypeID: "task"}})
				return err
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dm := newTestDB(t)
			var fired []indexerHookPhase
			dm.setIndexerTestingHook(func(ctx indexerHookContext) error {
				if ctx.Phase == tc.phase {
					fired = append(fired, ctx.Phase)
					return sentinelHookErr
				}
				return nil
			})
			defer dm.setIndexerTestingHook(nil)

			if err := tc.driver(dm); !errors.Is(err, sentinelHookErr) {
				t.Fatalf("%s: expected sentinel, got %v", tc.name, err)
			}
			if len(fired) != 1 || fired[0] != tc.phase {
				t.Errorf("%s: expected exactly one fire of its own phase, got %+v", tc.name, fired)
			}
		})
	}
}

// TestIndexerTestingHook_AppliesAcrossWriters exercises the seam under the
// Go memory model: a hook installed in one goroutine is observable by a
// concurrent writer goroutine. The atomic.Pointer field is the
// synchronization primitive that makes this safe; a plain field would be a
// data race. Run with -race to catch a regression.
func TestIndexerTestingHook_AppliesAcrossWriters(t *testing.T) {
	dm := newTestDB(t)

	var success atomic.Int64
	var aborted atomic.Int64
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			err := dm.IndexFileBlocks("vault", "NB", "S", "P",
				[]parser.ParsedBlock{sampleNoteBlock(nextUUID(i), 1)}, nil)
			switch {
			case err == nil:
				success.Add(1)
			case errors.Is(err, sentinelHookErr):
				aborted.Add(1)
			}
		}
	}()

	// Let a few successes through first so the writer is provably running.
	for success.Load() < 2 {
	}

	dm.setIndexerTestingHook(func(indexerHookContext) error { return sentinelHookErr })

	// Every subsequent writer call from THIS goroutine must abort.
	for i := 0; i < 50; i++ {
		err := dm.IndexFileBlocks("vault", "NB", "S", "P",
			[]parser.ParsedBlock{sampleNoteBlock(nextUUID(1000+i), 1)}, nil)
		if !errors.Is(err, sentinelHookErr) {
			close(stop)
			wg.Wait()
			t.Fatalf("post-install writer: expected sentinel, got %v", err)
		}
	}
	close(stop)
	wg.Wait()

	if aborted.Load() == 0 {
		t.Errorf("expected the racing writer to have observed the hook at least once")
	}

	// Clear and confirm the production fast path returns.
	dm.setIndexerTestingHook(nil)
	if err := dm.IndexFileBlocks("vault", "NB", "S", "P",
		[]parser.ParsedBlock{sampleNoteBlock(nextUUID(2000), 1)}, nil); err != nil {
		t.Fatalf("post-clear writer: %v", err)
	}
}

// nextUUID formats i into a deterministic UUID-shaped string. The racing
// writer loop needs distinct ids per call; the canonical shape is just so
// any debug printout stays scannable. Not a real RFC 4122 generator.
func nextUUID(i int) string {
	return fmt.Sprintf("%08x-1111-1111-1111-%012x", i, i)
}
