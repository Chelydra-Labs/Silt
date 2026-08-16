package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// errRenameNoOp signals that a rename op is a no-op (old name == new name).
// The cores translate it to a nil return so the IPC binding observes success.
var errRenameNoOp = errors.New("rename: no-op")

// --- Core 1: single-file rename (RenamePage + MovePage) -------------------
//
// RenamePage and MovePage share the same concurrency shape: lock [oldFile,
// newFile] + inbound sources, inbound-TOCTOU retry, rename-first-with-no-
// rollback, plain reindexFile, and reconcile OUTSIDE the lock. The op-specific
// differences (MkdirAll, which frontmatter field, log-vs-return on write
// failure, from/to locators) are captured as closures in the strategy.

type renameSingleFileStrategy struct {
	oldFile string
	newFile string
	targets []renameTarget

	// renameStep is the under-lock read + rename + frontmatter-write step.
	// Rename-first semantics: if os.Rename fails, nothing was modified and the
	// core returns the error without rollback.
	renameStep func() error
	// rewriteInbound is the under-lock inbound wiki-link rewrite. The core
	// passes the current held-path set (grown lockPaths normalized).
	rewriteInbound func(heldPaths map[string]bool)
	// clearAndReindex is the under-lock ClearFileBlocks + ForgetFile +
	// reindexFile sequence at the new path.
	clearAndReindex func()
	// staleSweep is the post-unlock residual TOCTOU sweep.
	staleSweep func()
	// reconcile is the post-unlock navigation reconcile.
	reconcile func() error
	// relocateHistory moves path-keyed page history after a successful rename.
	// Fail-open: a relocate error must not fail the rename.
	relocateHistory func()
	label           string
}

// renameSingleFile is the shared core for RenamePage and MovePage. It acquires
// vaultMu.RLock, runs prepare (validation + path resolution under the read
// lock), then drives the inbound-TOCTOU retry loop with LockPathsWrite.
// Reconcile runs OUTSIDE the lock (after the loop succeeds).
func (a *App) renameSingleFile(prepare func() (renameSingleFileStrategy, error)) error {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.db == nil {
		return fmt.Errorf("vault database not loaded")
	}
	s, err := prepare()
	if err != nil {
		if errors.Is(err, errRenameNoOp) {
			return nil
		}
		return err
	}

	a.wg.Add(1)
	defer a.wg.Done()

	inbound, err := a.collectInboundSourcePaths(s.targets)
	if err != nil {
		return fmt.Errorf("collect inbound sources: %w", err)
	}
	lockPaths := append([]string{s.oldFile, s.newFile}, inbound...)
	if a.renameHooks != nil && a.renameHooks.afterPreLockInbound != nil {
		a.renameHooks.afterPreLockInbound()
	}

	// Retry when inbound sources appear after pre-lock collect (TOCTOU).
	var runErr error
	for attempt := 0; attempt < 8; attempt++ {
		var needRetry []string
		runErr = nil
		// Lock page + inbound sources so concurrent saves cannot interleave.
		a.coordinator.LockPathsWrite(lockPaths, func() {
			missing, mErr := a.missingInboundPaths(s.targets, lockPathSet(lockPaths))
			if mErr != nil {
				runErr = mErr
				return
			}
			if len(missing) > 0 {
				needRetry = missing
				return
			}
			if err := s.renameStep(); err != nil {
				runErr = err
				return
			}
			s.rewriteInbound(lockPathSet(lockPaths))
			s.clearAndReindex()
		})
		if len(needRetry) > 0 {
			lockPaths = unionLockPaths(lockPaths, needRetry)
			continue
		}
		if runErr != nil {
			return runErr
		}
		// Sweep residual inbound links that landed after the under-lock
		// re-collect but before rewrite (narrow residual TOCTOU window).
		s.staleSweep()
		if err := s.reconcile(); err != nil {
			return err
		}
		if s.relocateHistory != nil {
			s.relocateHistory()
		}
		return nil
	}
	return fmt.Errorf("%s: inbound lock set did not stabilize after concurrent link creates", s.label)
}

// --- Core 2: tree rename (RenameSection + RenameNotebook) -----------------
//
// RenameSection and RenameNotebook share the same concurrency shape: lock
// every descendant .md + inbound sources, tree-TOCTOU retry, full rollback on
// error (forward write uses renameWriteFileAtomic, rollback uses plain
// WriteFileAtomic — that asymmetry is load-bearing), reindexFileStrict with
// rename hook, and reconcile INSIDE the lock. The op-specific differences
// (which frontmatter field, section/notebook derivation, literal vs resolved
// source, reconcile hook name) are captured as closures in the strategy.

type renameTreeStrategy struct {
	oldDir        string
	newDir        string
	notebook      string // renameTargetsFromMarkdownUnder notebook arg
	sectionPrefix string // renameTargetsFromMarkdownUnder sectionPrefix arg

	// buildRenameTargets derives the pre-lock inbound-collect targets from the
	// initial descendant .md seed paths (op-specific section-prefix logic).
	buildRenameTargets func(seedPaths []string) []renameTarget
	// walkAndSnapshot reads every descendant .md under oldDir and produces the
	// updated-content snapshots (op-specific frontmatter field + section
	// derivation).
	walkAndSnapshot func() ([]renameFileSnapshot, error)
	// rewriteInboundFile is the per-file under-lock inbound rewrite. The core
	// passes the current held-path set and the per-attempt link journal.
	rewriteInboundFile func(file renameFileSnapshot, heldPaths map[string]bool, linkJournal map[string]renameLinkJournalEntry)
	// clearAndReindexFile is the per-file under-lock ClearFileBlocks +
	// ForgetFile + reindexFileStrict. A non-nil return triggers rollback.
	clearAndReindexFile func(file renameFileSnapshot) error
	// reconcile is the under-lock navigation reconcile (runs before the
	// rollback check so a reconcile failure rolls back the whole tree).
	reconcile func() error
	// staleSweepFile is the post-unlock residual TOCTOU sweep per file.
	staleSweepFile func(file renameFileSnapshot)
	// relocateHistory moves path-keyed page history after a successful tree rename.
	relocateHistory func(files []renameFileSnapshot)
	// rollbackSource is the source arg to rollbackRename (resolved source for
	// Section; literal "vault" for Notebook).
	rollbackSource string
	// rollbackReconcileHookEnabled evaluates the configAttempted-and-hook
	// expression that controls whether rollback restores the config snapshot.
	rollbackReconcileHookEnabled func(configAttempted bool) bool
	label                        string
}

// renameTree is the shared core for RenameSection and RenameNotebook.
func (a *App) renameTree(prepare func() (renameTreeStrategy, error)) error {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.db == nil {
		return fmt.Errorf("vault database not loaded")
	}
	s, err := prepare()
	if err != nil {
		if errors.Is(err, errRenameNoOp) {
			return nil
		}
		return err
	}

	a.wg.Add(1)
	defer a.wg.Done()

	seedPaths, err := collectMarkdownFilePaths(s.oldDir)
	if err != nil {
		return err
	}
	renameTargets := s.buildRenameTargets(seedPaths)
	inbound, err := a.collectInboundSourcePaths(renameTargets)
	if err != nil {
		return fmt.Errorf("collect inbound sources: %w", err)
	}
	lockPaths := append(append([]string{}, seedPaths...), inbound...)
	if a.renameHooks != nil && a.renameHooks.afterPreLockInbound != nil {
		a.renameHooks.afterPreLockInbound()
	}

	configSnapshot := a.snapshotConfig()
	var files []renameFileSnapshot
	var runErr error
	// Retry if a .md appears under oldDir or inbound sources grow after the
	// pre-lock collect (TOCTOU).
	for attempt := 0; attempt < 8; attempt++ {
		var needRetry []string
		files = nil
		runErr = nil
		configAttempted := false
		linkJournal := make(map[string]renameLinkJournalEntry)
		// Lock every descendant page path + inbound sources so concurrent
		// saves cannot interleave with the directory rename.
		a.coordinator.LockPathsWrite(lockPaths, func() {
			missing, mErr := missingMarkdownUnder(s.oldDir, lockPathSet(lockPaths))
			if mErr != nil {
				runErr = mErr
				return
			}
			if len(missing) > 0 {
				needRetry = missing
				return
			}
			// Re-check inbound under lock so late wiki-links are locked too.
			lockedTargets := renameTargetsFromMarkdownUnder(s.oldDir, s.notebook, s.sectionPrefix, lockPaths)
			if len(lockedTargets) == 0 {
				lockedTargets = renameTargets
			}
			inMissing, iErr := a.missingInboundPaths(lockedTargets, lockPathSet(lockPaths))
			if iErr != nil {
				runErr = iErr
				return
			}
			if len(inMissing) > 0 {
				needRetry = inMissing
				return
			}
			walked, walkErr := s.walkAndSnapshot()
			if walkErr != nil {
				runErr = walkErr
				return
			}
			files = walked
			a.tracker.RegisterWrite(s.oldDir)
			a.tracker.RegisterWrite(s.newDir)
			if err := os.Rename(s.oldDir, s.newDir); err != nil {
				runErr = err
				return
			}
			for _, file := range files {
				newPath := filepath.Join(s.newDir, file.relPath)
				a.tracker.RegisterWrite(newPath)
				if err := a.renameWriteFileAtomic(newPath, file.updatedContent); err != nil {
					runErr = fmt.Errorf("%s: write %s: %w", s.label, file.relPath, err)
					if rollbackErr := a.rollbackRename(s.oldDir, s.newDir, files, s.rollbackSource, linkJournal, configSnapshot, false); rollbackErr != nil {
						runErr = fmt.Errorf("%w (rollback failed: %v)", runErr, rollbackErr)
					}
					return
				}
			}
			heldPaths := lockPathSet(lockPaths)
			for _, file := range files {
				s.rewriteInboundFile(file, heldPaths, linkJournal)
			}
			for _, file := range files {
				if err := s.clearAndReindexFile(file); err != nil {
					runErr = err
					break
				}
			}
			if runErr == nil {
				configAttempted = true
				if err := s.reconcile(); err != nil {
					runErr = err
				}
			}
			if runErr != nil {
				if rollbackErr := a.rollbackRename(s.oldDir, s.newDir, files, s.rollbackSource, linkJournal, configSnapshot, s.rollbackReconcileHookEnabled(configAttempted)); rollbackErr != nil {
					runErr = fmt.Errorf("%w (rollback failed: %v)", runErr, rollbackErr)
				}
			}
		})
		if len(needRetry) > 0 {
			lockPaths = unionLockPaths(lockPaths, needRetry)
			continue
		}
		if runErr != nil {
			return runErr
		}
		// Residual inbound sweep for each renamed page (post-unlock TOCTOU).
		for _, file := range files {
			s.staleSweepFile(file)
		}
		if s.relocateHistory != nil {
			s.relocateHistory(files)
		}
		return nil
	}
	return fmt.Errorf("%s: tree lock did not stabilize after concurrent creates", s.label)
}
