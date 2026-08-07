package main

import (
	"log"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"silt/backend/db"
	"silt/backend/parser"
)

// reprojectionAllMarker is the sentinel pending-map key the worker uses to
// record "next iteration reprojects every typed page regardless of which
// individual IDs are also pending". A non-empty string never collides with
// a real type id (which is lowercase [a-z0-9_-]).
const reprojectionAllMarker = "\x00all"

// projectionReprojectWorker is the vault-scoped coalescing worker that
// replaces the prior synchronous reprojectAllTypedPages calls from
// SaveType / DeleteType / ReloadTypes / the type watcher / RestoreExampleTypes.
//
// Design (Phase 5 / #866):
//   - Vault-local. One worker per open vault; started in
//     initializeVaultServices and stopped in stopWatchersOutsideLock so its
//     lifecycle is bound to the vault and runs OUTSIDE vaultMu.
//   - Coalescing. enqueue() adds type IDs to a pending set and wakes the
//     goroutine via a non-blocking signal. Multiple enqueues between
//     iterations collapse into one batch, so rapid saves produce one disk
//     pass, not N.
//   - Re-fetches schema per iteration. projectPageType / computePageProjection
//     use the mtime-aware types cache; on each iteration the cache reflects
//     the most recent on-disk state. A second enqueue landing while a batch
//     is in flight produces a follow-up iteration that re-reads the (now
//     updated) schema, converging to the final state without generation
//     counters.
//   - Out-of-lock disk + DB. The worker holds vaultMu only long enough to
//     snapshot the vaultPath / db handle / spacesPerTab, then releases it
//     before reading files or calling projectPageType. A close racing
//     mid-batch is detected via the liveness re-check before each locator.
//   - No generic scheduler. The pending map, wake/stop/done channels, and
//     epoch/processed counters are the entire state machine — there is no
//     pluggable Work interface, no priority queue, no generation framework.
type projectionReprojectWorker struct {
	app *App

	wake chan struct{} // buffered=1; non-blocking signal
	stop chan struct{} // closed to request exit; any pending enqueue is dropped (the next vault open's backfill covers it)
	done chan struct{} // closed when goroutine has exited

	// startOnce / stopOnce make start and stopAndJoin idempotent. stop on a
	// never-started worker is a no-op (stopOnce runs, done is already closed
	// by the deferred close in a noop wrapper). A double-stop does not panic
	// on close(stop) because stopOnce gates it.
	startOnce sync.Once
	stopOnce  sync.Once
	stopped   atomic.Bool // set by stopAndJoin so enqueue can short-circuit

	mu      sync.Mutex
	pending map[string]struct{}
	allMode bool

	epoch     atomic.Uint64
	processed atomic.Uint64

	// progress is the atomic snapshot backing both the live progress event
	// emits AND the GetTypesReprojectionStatus cold-state read. Swapped as a
	// single pointer so the read observes a consistent (total, processed)
	// pair — never a torn read across two independent atomics. Set at the
	// start of a non-empty batch (runOneBatch), advanced per locator, and
	// cleared to nil on completion / mid-batch abandon. nil is the idle
	// signal: GetTypesReprojectionStatus reports active=false, and the
	// dashboard hides the progress region. One atomic load on the read path.
	progress atomic.Pointer[reprojectionProgress]
}

// reprojectionProgress is the value type for projectionReprojectWorker.progress.
// Page counts always fit comfortably in uint64; a zero struct is unreachable
// (idle is represented by a nil pointer, not a zeroed value).
type reprojectionProgress struct {
	total     uint64
	processed uint64
}

// newProjectionReprojectWorker constructs an idle worker. done is pre-closed
// so stopAndJoin on a never-started worker returns immediately.
func newProjectionReprojectWorker(app *App) *projectionReprojectWorker {
	w := &projectionReprojectWorker{
		app:     app,
		pending: make(map[string]struct{}),
		wake:    make(chan struct{}, 1),
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
	}
	// Pre-close done so stopAndJoin on a never-started worker doesn't hang.
	// start() replaces done with a fresh open channel before launching run().
	close(w.done)
	return w
}

// enqueueReprojection is the App-level entry point every former
// reprojectAllTypedPages caller routes through. It is a no-op when no worker
// is running (no vault open / closing) — mirroring the prior behavior where
// reprojectAllTypedPages without a db handle silently dropped the work.
//
// allMode=true covers callers that cannot attribute the change to a single
// type id (ReloadTypes, the type watcher). Otherwise ids lists the affected
// type ids: SaveType passes the saved id; DeleteType passes the deleted id;
// a rename (DeleteType + SaveType) enqueues both.
func (a *App) enqueueReprojection(allMode bool, ids ...string) {
	w := a.reprojectWorker
	if w == nil {
		return
	}
	w.enqueue(allMode, ids...)
}

// start launches the worker goroutine. Idempotent via startOnce: a second
// call is a no-op. Replaces the pre-closed done channel with a fresh one so
// run's defer close(w.done) signals stopAndJoin correctly.
func (w *projectionReprojectWorker) start() {
	w.startOnce.Do(func() {
		w.done = make(chan struct{})
		go w.run()
	})
}

// stopAndJoin signals the worker to exit and blocks until done. Idempotent
// via stopOnce: a second call is a no-op. Safe on a never-started worker
// (done was pre-closed in the constructor). Safe concurrently with enqueue
// (stopped flag short-circuits future enqueues).
func (w *projectionReprojectWorker) stopAndJoin() {
	w.stopOnce.Do(func() {
		w.stopped.Store(true)
		close(w.stop)
	})
	<-w.done
}

// enqueue schedules a reprojection pass. If allMode is true, the next
// iteration reprojects every typed page regardless of which IDs had been
// pending (used by ReloadTypes + the type watcher, which cannot attribute
// the change to a specific type). Otherwise ids name the affected type IDs
// (used by SaveType for the saved id and by DeleteType for the deleted id).
//
// Rapid enqueues coalesce: a save + save + save produces one disk pass that
// reflects the final schema, not three. A late enqueue landing while a
// batch is in flight produces a follow-up iteration that re-reads the now-
// updated schema.
//
// Safe under any lock — only touches the worker's own mu.
func (w *projectionReprojectWorker) enqueue(allMode bool, ids ...string) {
	if w.stopped.Load() {
		return
	}
	w.mu.Lock()
	if allMode {
		w.allMode = true
		w.pending[reprojectionAllMarker] = struct{}{}
	}
	for _, id := range ids {
		if id != "" {
			w.pending[id] = struct{}{}
		}
	}
	w.mu.Unlock()
	w.epoch.Add(1)
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

// flushForTest blocks until the worker has processed every enqueue made
// before this call, or timeout elapses. Returns true if the worker caught
// up, false on timeout. Production code never calls this — it exists for
// deterministic test synchronization without sleeps.
func (w *projectionReprojectWorker) flushForTest(timeout time.Duration) bool {
	target := w.epoch.Load()
	deadline := time.Now().Add(timeout)
	for {
		if w.processed.Load() >= target {
			// One extra wake + drain cycle ensures the post-processing
			// idle state is observable. The worker has either finished
			// this batch or skipped it (because pending was empty after
			// a prior drain); either way processed >= target is the
			// authoritative signal.
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// run is the worker loop. Exits when stop is closed.
func (w *projectionReprojectWorker) run() {
	defer close(w.done)
	for {
		select {
		case <-w.stop:
			return
		case <-w.wake:
			target := w.epoch.Load()
			w.runOneBatch()
			w.processed.Store(target)
		}
	}
}

// runOneBatch drains the pending set under the worker's lock, then processes
// every drained locator outside vaultMu. Disk reads, schema fetches, and DB
// writes all happen here, unreferenced by the lifecycle lock — only brief
// RLock snapshots are taken and the worker re-checks liveness between
// locators so a mid-batch close abandons the remainder cleanly.
func (w *projectionReprojectWorker) runOneBatch() {
	w.mu.Lock()
	allMode := w.allMode
	ids := make([]string, 0, len(w.pending))
	for id := range w.pending {
		if id == reprojectionAllMarker {
			continue
		}
		ids = append(ids, id)
	}
	w.pending = make(map[string]struct{})
	w.allMode = false
	w.mu.Unlock()

	// Liveness snapshot: hold vaultMu only long enough to copy the handles.
	// All subsequent disk reads + DB writes happen WITHOUT the lifecycle
	// lock so concurrent IPC readers are not blocked by a large vault's
	// reprojection pass.
	w.app.vaultMu.RLock()
	if w.app.vaultPath == "" || w.app.db == nil {
		w.app.vaultMu.RUnlock()
		return
	}
	vaultPath := w.app.vaultPath
	dbMgr := w.app.db
	spacesPerTab := w.app.spacesPerTab
	w.app.vaultMu.RUnlock()

	// Resolve the locator set. The choice between scoped (by id) and full
	// (all typed pages) is the #866 scaling win: a single type's schema
	// edit touches only its own pages, not every typed page in the vault.
	var (
		locators []db.TypedPageLocator
		err      error
	)
	if allMode {
		locators, err = dbMgr.GetAllTypedPageLocators()
	} else {
		locators, err = dbMgr.GetTypedPageLocatorsByIDs(ids)
	}
	if err != nil {
		log.Printf("types: reprojection worker locators query failed (allMode=%v, ids=%v): %v", allMode, ids, err)
		w.app.emit(EventTypesProjectionError, map[string]string{"source": "", "page": ""})
		return
	}

	// Progress setup. A no-op batch (no locators) emits nothing — churning
	// start/done for an empty set would flicker the dashboard progress region
	// on every coalesced no-op wake. The progress pointer is already nil
	// (the prior batch's completion or mid-batch abandon cleared it), so the
	// cold-state read continues to report idle.
	total := uint64(len(locators))
	if total == 0 {
		return
	}
	w.progress.Store(&reprojectionProgress{total: total, processed: 0})
	w.app.emit(EventTypesReprojectionProgress, map[string]any{
		"state":     "running",
		"processed": uint64(0),
		"total":     total,
	})
	// Throttle intermediate emits so a 10k-page vault does not fire an event
	// per page. ~20 updates over the run (or every 25 for small batches) is
	// smooth without flooding the IPC channel.
	step := total / 20
	if step < 25 {
		step = 25
	}

	for i, loc := range locators {
		// Re-check liveness before each locator so a vault close racing
		// mid-batch abandons the remainder cleanly — the db handle may
		// already be nil by the time we reach a later locator.
		w.app.vaultMu.RLock()
		closed := w.app.vaultPath == "" || w.app.db == nil
		w.app.vaultMu.RUnlock()
		if closed {
			// Reset progress without a "done" emit — the batch was abandoned,
			// not completed. The next open starts fresh (nil = idle).
			w.progress.Store(nil)
			return
		}
		w.reprojectOneLocator(dbMgr, vaultPath, spacesPerTab, loc)
		done := uint64(i + 1)
		w.progress.Store(&reprojectionProgress{total: total, processed: done})
		if done == total || (step > 0 && done%step == 0) {
			w.app.emit(EventTypesReprojectionProgress, map[string]any{
				"state":     "running",
				"processed": done,
				"total":     total,
			})
		}
	}

	w.app.emit(EventTypesReprojectionProgress, map[string]any{
		"state":     "done",
		"processed": total,
		"total":     total,
	})
	w.progress.Store(nil)
}

// reprojectOneLocator is the per-page step shared with the prior
// reprojectAllTypedPages body. It reads the page's file, re-parses the
// frontmatter against the current (mtime-cached) type schema, and writes
// the projection (or clears it when the page lost its type / vanished).
//
// All disk + DB work happens WITHOUT vaultMu; the worker has already
// snapshotted what it needs. types:projection-error is emitted on every
// failure path so the user can see something went wrong.
func (w *projectionReprojectWorker) reprojectOneLocator(dbMgr *db.DatabaseManager, vaultPath string, spacesPerTab int, loc db.TypedPageLocator) {
	safeNotebook := sanitizePathSegment(loc.Notebook)
	// validateSectionPath (not sanitizePathSegment) so a multi-segment
	// section like "Projects/Active" survives — sanitizePathSegment strips
	// the "/", flattening it to "ProjectsActive" and ENOENT'ing the file.
	safeSection, sectionErr := validateSectionPath(loc.Section, true)
	safePage := sanitizePathSegment(loc.Page)
	if sectionErr != nil || safeNotebook == "" || safePage == "" {
		w.app.emit(EventTypesProjectionError, map[string]string{"source": loc.Source, "page": loc.Page})
		return
	}
	notebookDir, err := w.app.resolveNotebookDir(safeNotebook, loc.Source)
	if err != nil {
		w.app.emit(EventTypesProjectionError, map[string]string{"source": loc.Source, "page": loc.Page})
		return
	}
	filePath := filepath.Join(notebookDir, safeSection, safePage+".md")
	if !isPathWithinRoot(filePath, notebookDir) {
		w.app.emit(EventTypesProjectionError, map[string]string{"source": loc.Source, "page": loc.Page})
		return
	}
	// Capture mtime+size at read time to detect a concurrent file
	// modification between read and projection write. Same strategy as
	// IsFileUnchanged (files-table warm-start gate): mtime+size on the
	// target filesystem is the architecture's existing staleness signal.
	readInfo, statErr := os.Stat(filePath)
	if statErr != nil {
		if os.IsNotExist(statErr) {
			if cerr := dbMgr.ClearPageProjection(loc.Source, loc.Notebook, loc.Section, loc.Page); cerr != nil {
				log.Printf("types: reprojection ClearPageProjection(%s/%s/%s/%s) after missing file: %v", loc.Source, loc.Notebook, loc.Section, loc.Page, cerr)
			}
		} else {
			log.Printf("types: reprojection stat %s failed (projection kept): %v", filePath, statErr)
		}
		w.app.emit(EventTypesProjectionError, map[string]string{"source": loc.Source, "page": loc.Page})
		return
	}
	contentBytes, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			if cerr := dbMgr.ClearPageProjection(loc.Source, loc.Notebook, loc.Section, loc.Page); cerr != nil {
				log.Printf("types: reprojection ClearPageProjection(%s/%s/%s/%s) after missing file: %v", loc.Source, loc.Notebook, loc.Section, loc.Page, cerr)
			}
		} else {
			log.Printf("types: reprojection read %s failed (projection kept): %v", filePath, err)
		}
		w.app.emit(EventTypesProjectionError, map[string]string{"source": loc.Source, "page": loc.Page})
		return
	}
	_, meta, _, _, perr := parser.ParseFileContent(string(contentBytes), safeNotebook, safeSection, safePage, fileOrDefaultDate(filePath), spacesPerTab)
	if perr != nil {
		// Keep the prior projection on parse failure — the file still
		// exists and may become readable again.
		log.Printf("types: reprojection parse %s failed (projection kept): %v", filePath, perr)
		w.app.emit(EventTypesProjectionError, map[string]string{"source": loc.Source, "page": loc.Page})
		return
	}
	if meta.Type == "" {
		// Page lost its type externally; drop the stale projection row.
		if err := dbMgr.ClearPageProjection(loc.Source, loc.Notebook, loc.Section, loc.Page); err != nil {
			log.Printf("types: reprojection ClearPageProjection(%s/%s/%s/%s) failed: %v", loc.Source, loc.Notebook, loc.Section, loc.Page, err)
			w.app.emit(EventTypesProjectionError, map[string]string{"source": loc.Source, "page": loc.Page})
		}
		return
	}
	// Guard against a page-deletion race: the locator list was snapshotted
	// at the start of this batch, so a concurrent DeletePage /
	// ClearFileBlocks may have removed the page's blocks AND projection
	// between snapshot and this write. Without this check projectPageType
	// would write a fresh page_types/page_properties row — resurrecting the
	// projection for a page the user just deleted. PageExists is a single
	// EXISTS probe on a covered index, cheap relative to the disk read we
	// already paid.
	stillExists, existsErr := dbMgr.PageExists(loc.Source, loc.Notebook, loc.Section, loc.Page)
	if existsErr != nil {
		// DB error — can't prove the page is gone, so leave the prior
		// projection intact rather than risk a false clear.
		log.Printf("types: reprojection PageExists check failed for %s/%s/%s/%s (projection kept): %v", loc.Source, loc.Notebook, loc.Section, loc.Page, existsErr)
		w.app.emit(EventTypesProjectionError, map[string]string{"source": loc.Source, "page": loc.Page})
		return
	}
	if !stillExists {
		return
	}
	// TOCTOU guard: re-stat the file. If mtime or size changed since our
	// initial read, a concurrent writer rewrote the file between our read
	// and this write. Our parsed meta is stale; skip so the concurrent
	// writer's own atomic publish is the authoritative projection.
	curInfo, statErr := os.Stat(filePath)
	if statErr != nil || !curInfo.ModTime().Equal(readInfo.ModTime()) || curInfo.Size() != readInfo.Size() {
		return
	}
	if err := w.app.projectPageType(loc.Source, meta); err != nil {
		w.app.emit(EventTypesProjectionError, map[string]string{"source": loc.Source, "page": loc.Page})
	}
}
