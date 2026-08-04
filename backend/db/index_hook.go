package db

import "sync/atomic"

// indexerHookPhase identifies one pre-commit point inside an indexing
// transaction so a test can target a single indexer when injecting a
// failure (a hook for IndexFileBlocks must not ghost-fire for the others).
type indexerHookPhase int

const (
	indexerHookIndexFileBlocksPreCommit     indexerHookPhase = iota // after clears+inserts, before IndexFileBlocks commit
	indexerHookIndexScanResultsPreCommit                            // after the batch loop, before IndexScanResults commit
	indexerHookIndexPageProjectionPreCommit                         // after projection clear+insert, before IndexPageProjection commit
)

// indexerHookContext carries call-site coordinates so a hook can decide
// whether to fail without touching the live transaction. IndexScanResults
// leaves them zero — no single page identifies the batch.
type indexerHookContext struct {
	Phase    indexerHookPhase
	Source   string
	Notebook string
	Section  string
	Page     string
}

// indexerTestingHookFunc is invoked at pre-commit points inside indexing
// transactions. A non-nil return aborts via the caller's deferred
// tx.Rollback, so the prior committed state stays visible and no partial
// clear/insert leaks. The hook never receives the *sql.Tx — tests assert
// post-rollback invariants by querying the DatabaseManager after the
// indexer call returns the wrapped error.
type indexerTestingHookFunc func(ctx indexerHookContext) error

// indexerTestingHook is the per-DatabaseManager test seam: nil in
// production (one atomic load + nil-check per indexer call), installed by
// tests via setIndexerTestingHook. The atomic pointer is the
// synchronization primitive that lets a test install a hook from one
// goroutine while a writer runs in another. Kept internal so the public
// App/IPC surface stays unchanged.
type indexerTestingHook = atomic.Pointer[indexerTestingHookFunc]

// setIndexerTestingHook installs (or clears with nil) the test hook.
// Same-package seam; call from test setup before driving writers.
func (dm *DatabaseManager) setIndexerTestingHook(fn indexerTestingHookFunc) {
	if fn == nil {
		dm.indexerTestingHook.Store(nil)
		return
	}
	dm.indexerTestingHook.Store(&fn)
}

// runIndexerTestingHook is the single funnel every indexer call site uses.
// Returns nil on the production fast path (no hook) and when the hook
// reports no failure; a non-nil return aborts the caller's transaction.
func (dm *DatabaseManager) runIndexerTestingHook(ctx indexerHookContext) error {
	fnPtr := dm.indexerTestingHook.Load()
	if fnPtr == nil {
		return nil
	}
	return (*fnPtr)(ctx)
}
