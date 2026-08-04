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
	stop chan struct{} // closed to request drain + exit
	done chan struct{} // closed when goroutine has exited

	// pending + allMode are guarded by mu. The worker drains both under one
	// hold so an enqueue arriving during drain is observed in the NEXT
	// iteration, not the current one (correct coalescing semantics).
	mu      sync.Mutex
	pending map[string]struct{}
	allMode bool

	// epoch / processed give tests a deterministic way to wait for the
	// worker to finish a specific enqueue. epoch is bumped on every enqueue;
	// processed is bumped after a batch finishes. flushForTest polls until
	// processed >= target. Production never reads either.
	epoch     atomic.Uint64
	processed atomic.Uint64
}

// newProjectionReprojectWorker constructs an idle worker. Caller MUST follow
// with start() once the vault's db / vaultPath are populated.
func newProjectionReprojectWorker(app *App) *projectionReprojectWorker {
	return &projectionReprojectWorker{
		app:     app,
		pending: make(map[string]struct{}),
		wake:    make(chan struct{}, 1),
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
	}
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

// start launches the worker goroutine. Idempotent — a second call while the
// goroutine is running is a no-op (the new wake/stop channels would be
// orphaned). Caller does NOT need to hold vaultMu.
func (w *projectionReprojectWorker) start() {
	go w.run()
}

// stopAndJoin signals the worker to exit and blocks until the goroutine is
// done. Idempotent. Safe to call concurrently with an in-flight enqueue (the
// pending set is dropped on exit — work not yet processed is abandoned,
// which is correct for a vault close since the db handle is going away).
// Caller does NOT need to hold vaultMu; this is invoked from
// stopWatchersOutsideLock specifically so the worker can drain without
// deadlocking against the teardown Lock.
func (w *projectionReprojectWorker) stopAndJoin() {
	close(w.stop)
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

	for _, loc := range locators {
		// Re-check liveness before each locator so a vault close racing
		// mid-batch abandons the remainder cleanly — the db handle may
		// already be nil by the time we reach a later locator.
		w.app.vaultMu.RLock()
		closed := w.app.vaultPath == "" || w.app.db == nil
		w.app.vaultMu.RUnlock()
		if closed {
			return
		}
		w.reprojectOneLocator(dbMgr, vaultPath, spacesPerTab, loc)
	}
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
		return
	}
	// resolveNotebookDir consults the linked-notebook registry when source
	// is "linked:<id>", so the worker reaches linked typed pages too. It
	// touches configMu (the linked-notebook registry), NOT vaultMu.
	notebookDir, err := w.app.resolveNotebookDir(safeNotebook, loc.Source)
	if err != nil {
		return
	}
	filePath := filepath.Join(notebookDir, safeSection, safePage+".md")
	if !isPathWithinRoot(filePath, notebookDir) {
		return
	}
	contentBytes, err := os.ReadFile(filePath)
	if err != nil {
		// Only drop the projection when the file is confirmed gone. A
		// transient lock/IO error during sync must not erase the locator
		// (the next schema edit re-enqueues it).
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
	if err := w.app.projectPageType(loc.Source, meta); err != nil {
		w.app.emit(EventTypesProjectionError, map[string]string{"source": loc.Source, "page": loc.Page})
	}
}
