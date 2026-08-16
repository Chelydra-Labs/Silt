package main

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"strconv"

	"silt/backend/config"
	"silt/backend/history"
	"silt/backend/parser"
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

// PageVersionInfo is the IPC list row for one retained snapshot.
type PageVersionInfo struct {
	ID        string `json:"id"`
	Timestamp string `json:"timestamp"`
	Source    string `json:"source"`
	Bytes     int    `json:"bytes"`
}

func (a *App) resolvePageHistory(notebook, section, page string) (loc history.Locator, root, filePath, source, safeNotebook, safeSection, safePage string, err error) {
	if a.vaultPath == "" || a.db == nil {
		return loc, "", "", "", "", "", "", fmt.Errorf("vault not loaded")
	}
	safeNotebook = sanitizePathSegment(notebook)
	safeSection = sanitizePathSegment(section)
	safePage = sanitizePathSegment(page)
	if safeNotebook == "" || safePage == "" {
		return loc, "", "", "", "", "", "", NewIPCError(CodeInvalidNavigationPath, "invalid path metadata")
	}
	source = a.resolveSourceByName(safeNotebook)
	notebookDir, err := a.resolveNotebookDir(safeNotebook, source)
	if err != nil {
		return loc, "", "", "", "", "", "", err
	}
	filePath = filepath.Join(notebookDir, safeSection, safePage+".md")
	if !isPathWithinRoot(filePath, notebookDir) {
		return loc, "", "", "", "", "", "", fmt.Errorf("path escapes notebook root")
	}
	loc = historyLoc(source, safeNotebook, safeSection, safePage)
	root = a.historyRoot(source)
	return loc, root, filePath, source, safeNotebook, safeSection, safePage, nil
}

// ListPageVersions returns retained snapshots for a page, newest first.
func (a *App) ListPageVersions(notebook, section, page string) ([]PageVersionInfo, error) {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	loc, root, _, _, _, _, _, err := a.resolvePageHistory(notebook, section, page)
	if err != nil {
		return nil, err
	}
	if root == "" {
		return []PageVersionInfo{}, nil
	}
	entries, err := history.List(root, loc)
	if err != nil {
		return nil, err
	}
	out := make([]PageVersionInfo, 0, len(entries))
	for _, e := range entries {
		out = append(out, PageVersionInfo{
			ID:        e.ID,
			Timestamp: e.Time.UTC().Format(time.RFC3339),
			Source:    e.Source,
			Bytes:     e.Bytes,
		})
	}
	return out, nil
}

// GetPageVersion returns the markdown body of a stored version (no frontmatter).
func (a *App) GetPageVersion(notebook, section, page, versionID string) (string, error) {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	loc, root, _, _, _, _, _, err := a.resolvePageHistory(notebook, section, page)
	if err != nil {
		return "", err
	}
	if root == "" || strings.TrimSpace(versionID) == "" {
		return "", NewIPCError(CodeNavigationNotFound, "page version not found")
	}
	raw, err := history.Read(root, loc, versionID)
	if err != nil {
		if errors.Is(err, history.ErrNotFound) {
			return "", NewIPCError(CodeNavigationNotFound, "page version not found")
		}
		return "", err
	}
	_, body := parser.SplitFrontmatter(string(raw))
	return body, nil
}

// RestorePageVersion replaces the live page body with a stored version.
// Current frontmatter is preserved. The pre-restore bytes are captured first.
func (a *App) RestorePageVersion(notebook, section, page, versionID string) error {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	loc, root, filePath, source, safeNotebook, safeSection, safePage, err := a.resolvePageHistory(notebook, section, page)
	if err != nil {
		return err
	}
	if root == "" || strings.TrimSpace(versionID) == "" {
		return NewIPCError(CodeNavigationNotFound, "page version not found")
	}
	snapshot, err := history.Read(root, loc, versionID)
	if err != nil {
		if errors.Is(err, history.ErrNotFound) {
			return NewIPCError(CodeNavigationNotFound, "page version not found")
		}
		return err
	}
	_, restoreBody := parser.SplitFrontmatter(string(snapshot))

	a.wg.Add(1)
	defer a.wg.Done()

	var beforeIDs []string
	a.coordinator.WithDBRead(func() {
		beforeIDs, _ = a.db.BlockIDsForPage(source, safeNotebook, safeSection, safePage)
	})

	var result []parser.ParsedBlock
	var writeErr error
	a.coordinator.LockBlocksWrite(beforeIDs, func() {
		a.coordinator.LockFileWrite(filePath, func() {
			if _, err := os.Stat(filePath); err != nil {
				if os.IsNotExist(err) {
					writeErr = NewIPCError(CodeNavigationNotFound, "page not found")
				} else {
					writeErr = err
				}
				return
			}
			contentBytes, err := os.ReadFile(filePath)
			if err != nil {
				writeErr = fmt.Errorf("failed to read existing file: %w", err)
				return
			}
			result, writeErr = a.writePageMarkdownLocked(filePath, source, safeNotebook, safeSection, safePage, notebook, section, page, contentBytes, restoreBody, historyReasonRestore)
		})
	})
	if writeErr != nil {
		return writeErr
	}

	newIDSet := make(map[string]bool, len(result))
	for _, b := range result {
		if b.ID != "" {
			newIDSet[b.ID] = true
		}
	}
	var removed []string
	for _, id := range beforeIDs {
		if id != "" && !newIDSet[id] {
			removed = append(removed, id)
		}
	}
	a.coordinator.ReleaseBlockMutexes(removed)
	for _, b := range result {
		if b.ID != "" {
			a.emitBlockChanged(b.ID, safeNotebook, safeSection, safePage, b.FileDate)
		}
	}
	return nil
}

// writePageMarkdownLocked replaces a page body while preserving current
// frontmatter. The caller MUST already hold LockFileWrite for filePath.
func (a *App) writePageMarkdownLocked(filePath, source, notebook, section, page, displayNotebook, displaySection, displayPage string, contentBytes []byte, markdown, reason string) ([]parser.ParsedBlock, error) {
	frontmatter, _ := parser.SplitFrontmatter(string(contentBytes))
	if frontmatter == "" {
		today := time.Now().Format("2006-01-02")
		frontmatter = fmt.Sprintf("---\nnotebook: %s\nsection: %s\npage: %s\ndate: %s\ntags: []\n---\n",
			strconv.Quote(displayNotebook), strconv.Quote(displaySection), strconv.Quote(displayPage), strconv.Quote(today))
	}
	body := markdown
	if body != "" && !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	newContent := frontmatter
	if !strings.HasSuffix(newContent, "\n") {
		newContent += "\n"
	}
	newContent += body

	a.maybeCapturePageVersion(historyLoc(source, notebook, section, page), contentBytes, []byte(newContent), reason)
	a.tracker.RegisterWrite(filePath)
	if err := parser.WriteFileAtomic(filePath, []byte(newContent)); err != nil {
		return nil, err
	}
	parsedBlocks, meta, _, _, parseErr := parser.ParseFileContent(
		newContent, notebook, section, page, fileOrDefaultDate(filePath), a.spacesPerTab,
	)
	if parseErr != nil {
		return nil, fmt.Errorf("parse after source save: %w", parseErr)
	}
	if idxErr := a.indexFile(source, meta.Notebook, meta.Section, meta.Page, parsedBlocks, meta, meta.Warnings...); idxErr != nil {
		return nil, fmt.Errorf("re-index after source save failed: %w", idxErr)
	}
	return parsedBlocks, nil
}

func (a *App) relocatePageHistory(source, oldNotebook, oldSection, oldPage, newNotebook, newSection, newPage string) {
	root := a.historyRoot(source)
	if root == "" {
		return
	}
	oldLoc := historyLoc(source, oldNotebook, oldSection, oldPage)
	newLoc := historyLoc(source, newNotebook, newSection, newPage)
	if err := history.Relocate(root, oldLoc, newLoc); err != nil {
		log.Printf("page history: relocate failed: %v", err)
	}
}
