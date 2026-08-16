package main

import (
	"crypto/sha256"
	"log"
	"strings"
	"time"

	"silt/backend/config"
	"silt/backend/history"
)

const (
	historyReasonEditor  = "editor"
	historyReasonSource  = "source"
	historyReasonMCP     = "mcp"
	historyReasonPlugin  = "plugin"
	historyReasonRestore = "restore"
)

// maybeCapturePageVersion snapshots prev (the on-disk bytes about to be
// overwritten) when page history is enabled. It must be called under
// LockFileWrite, before WriteFileAtomic. Errors are logged and never
// returned — history I/O must not fail a page save.
func (a *App) maybeCapturePageVersion(loc history.Locator, prev, incoming []byte, reason string) {
	if a == nil || a.vaultPath == "" {
		return
	}
	enabled, max, interval := a.pageHistorySettings()
	if !enabled {
		return
	}
	if len(prev) == 0 {
		return
	}
	if sha256.Sum256(prev) == sha256.Sum256(incoming) {
		return
	}
	root := a.historyRoot(loc.Source)
	if root == "" {
		return
	}
	if reason == "" {
		reason = historyReasonEditor
	}
	if reason == historyReasonEditor || reason == historyReasonSource {
		if interval > 0 {
			last, ok, err := history.Last(root, loc)
			if err != nil {
				log.Printf("page history: last lookup failed: %v", err)
			} else if ok && time.Since(last.Time) < time.Duration(interval)*time.Second {
				return
			}
		}
	}
	if _, err := history.Capture(root, loc, prev, reason, time.Now().UTC(), history.Options{MaxVersions: max}); err != nil {
		log.Printf("page history: capture failed: %v", err)
	}
}

func (a *App) pageHistorySettings() (enabled bool, max, interval int) {
	a.configMu.RLock()
	defer a.configMu.RUnlock()
	if a.cfg.Editor.AutoVersioningEnabled != nil {
		enabled = *a.cfg.Editor.AutoVersioningEnabled
	}
	max = a.cfg.Editor.MaxVersionsPerPage
	interval = a.cfg.Editor.AutoVersioningMinIntervalSec
	return
}

func (a *App) historyRoot(source string) string {
	if source == "" || source == config.LinkedNotebooksVaultSource {
		return a.vaultPath
	}
	if !strings.HasPrefix(source, "linked:") {
		return ""
	}
	id := strings.TrimPrefix(source, "linked:")
	a.configMu.RLock()
	defer a.configMu.RUnlock()
	for _, ln := range a.cfg.LinkedNotebooks {
		if ln.ID == id {
			return ln.RootPath
		}
	}
	return ""
}

func historyLoc(source, notebook, section, page string) history.Locator {
	return history.Locator{Source: source, Notebook: notebook, Section: section, Page: page}
}
