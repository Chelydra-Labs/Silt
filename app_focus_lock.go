package main

import (
	"fmt"
	"path/filepath"
)

// focusFilePath resolves the on-disk page file for a focus-lease operation,
// routing to the correct root via the notebook's source (#100). Shared by
// Acquire/Release/RefreshFocusLock so the lease key always matches the file
// the watcher sees — including linked notebooks.
func (a *App) focusFilePath(notebook, section, page string) (string, error) {
	safeNotebook := sanitizePathSegment(notebook)
	safeSection := sanitizePathSegment(section)
	safePage := sanitizePathSegment(page)
	if safeNotebook == "" || safePage == "" {
		return "", fmt.Errorf("invalid path metadata")
	}
	notebookDir, err := a.resolveNotebookDir(safeNotebook, a.resolveSourceByName(safeNotebook))
	if err != nil {
		return "", err
	}
	fp := filepath.Join(notebookDir, safeSection, safePage+".md")
	if !isPathWithinRoot(fp, notebookDir) {
		return "", fmt.Errorf("path escapes notebook root")
	}
	return fp, nil
}

// AcquireFocusLock registers a focus lock on a page file to ignore fsnotify updates.
func (a *App) AcquireFocusLock(notebook, section, page string) error {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.watcher == nil {
		return fmt.Errorf("watcher not running")
	}
	a.wg.Add(1)
	defer a.wg.Done()
	fp, err := a.focusFilePath(notebook, section, page)
	if err != nil {
		return err
	}
	a.watcher.LockFocus(fp)
	return nil
}

// ReleaseFocusLock removes a focus lock from a page file.
func (a *App) ReleaseFocusLock(notebook, section, page string) error {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.watcher == nil {
		return fmt.Errorf("watcher not running")
	}
	a.wg.Add(1)
	defer a.wg.Done()
	fp, err := a.focusFilePath(notebook, section, page)
	if err != nil {
		return err
	}
	a.watcher.UnlockFocus(fp)
	return nil
}

// RefreshFocusLock extends an existing focus lease for a page file. Called by the
// Svelte editor's heartbeat while it stays focused (#38); a no-op if the
// lease already expired (the editor must re-acquire).
func (a *App) RefreshFocusLock(notebook, section, page string) error {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.watcher == nil {
		return fmt.Errorf("watcher not running")
	}
	a.wg.Add(1)
	defer a.wg.Done()
	fp, err := a.focusFilePath(notebook, section, page)
	if err != nil {
		return err
	}
	a.watcher.RefreshFocus(fp)
	return nil
}
