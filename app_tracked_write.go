package main

import (
	"silt/backend/core"
)

// trackedPageWrite is the single write path for every Silt-owned page-file
// write performed under a file write lock (#1005). It owns the three steps
// every writer family owes the external-edit machinery, so a family cannot
// forget one of them at its nth call site:
//
//  1. tracker.RegisterWrite — the watcher must suppress the self-write event
//     (a watcher re-index racing the writer's own re-index).
//  2. the atomic write itself (atomicPageWrite, the package's test seam).
//  3. refreshFingerprintAfterWrite — a Silt write is never an external edit;
//     re-baselining in the same lock keeps an editor autosave landing in the
//     write→signal→recapture window from false-conflicting against Silt's own
//     bytes — which, under the stash-authoritative gate, would mint a phantom
//     refused-write stash demanding manual resolution.
//
// Direct atomic-write call sites that remain are page writers that cannot use
// this shape (each carries a one-line justification) or non-page control
// files the fingerprint machinery never keys.
func (a *App) trackedPageWrite(filePath string, content []byte) error {
	a.tracker.RegisterWrite(filePath)
	if err := atomicPageWrite(filePath, content); err != nil {
		return err
	}
	a.refreshFingerprintAfterWrite(filePath, content)
	return nil
}

// migrateConflictStateAcrossRename moves a path's pending refused-write stash
// to its new path and drops the old path's load baseline after a successful
// rename/move. Without this the stash strands at the old path — resolution
// dead-ends (nothing can settle a key the page no longer lives at) and any
// page later created at the old path inherits a foreign conflict (#1005).
// The baseline is not migrated: it snapshots the bytes an editor buffer held
// for the OLD path; the renamed page's next editor load recaptures fresh.
// A stash or baseline keyed at the NEW path is dropped when the stash moves
// in: rename refuses an existing target page, so any target-keyed stash
// belongs to a page that no longer exists and replaying it would plant a
// foreign document. The target baseline is deliberately left alone — an
// editor still holding a deleted target document must refuse (via stat/hash
// drift) rather than clobber the renamed page.
func (a *App) migrateConflictStateAcrossRename(oldPath, newPath string) {
	if oldPath == "" || newPath == "" || oldPath == newPath {
		return
	}
	oldKey := core.NormalizeFileLockPath(oldPath)
	newKey := core.NormalizeFileLockPath(newPath)
	a.refusedWritesMu.Lock()
	if rw, ok := a.refusedWrites[oldKey]; ok {
		delete(a.refusedWrites, oldKey)
		delete(a.refusedWrites, newKey)
		if a.refusedWrites == nil {
			a.refusedWrites = make(map[string]refusedPageWrite)
		}
		a.refusedWrites[newKey] = rw
	}
	a.refusedWritesMu.Unlock()
	a.pageLoadFingerprintsMu.Lock()
	if a.pageLoadFingerprints != nil {
		delete(a.pageLoadFingerprints, oldKey)
	}
	a.pageLoadFingerprintsMu.Unlock()
}

// dropConflictStateForPath discards a path's pending refused-write stash and
// load baseline after the page at that path is deleted. A stash for a deleted
// page can never resolve (its "current version" read fails on the absent
// file for every resolution) and would phantom-refuse whatever page is later
// created at the path; the baseline is equally dead — the save funnels refuse
// a missing file via the stat check long before consulting it (#1005).
func (a *App) dropConflictStateForPath(filePath string) {
	if filePath == "" {
		return
	}
	key := core.NormalizeFileLockPath(filePath)
	a.refusedWritesMu.Lock()
	if a.refusedWrites != nil {
		delete(a.refusedWrites, key)
	}
	a.refusedWritesMu.Unlock()
	a.pageLoadFingerprintsMu.Lock()
	if a.pageLoadFingerprints != nil {
		delete(a.pageLoadFingerprints, key)
	}
	a.pageLoadFingerprintsMu.Unlock()
}

// clearConflictState drops every stash and baseline (vault teardown). Both
// maps are vault-scoped session state: a stash keyed by absolute path into a
// closed vault is unresolvable and would hijack a same-named page if the
// vault is reopened, and the baselines reference files the next vault's
// editor never loaded.
func (a *App) clearConflictState() {
	a.refusedWritesMu.Lock()
	a.refusedWrites = nil
	a.refusedWritesMu.Unlock()
	a.pageLoadFingerprintsMu.Lock()
	a.pageLoadFingerprints = nil
	a.pageLoadFingerprintsMu.Unlock()
}
