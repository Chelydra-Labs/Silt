package main

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
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
	historyReasonRename  = "rename"
)

// maybeCapturePageVersion snapshots prev (the bytes being replaced) when
// page history is enabled. Callers must hold LockFileWrite. Prefer calling
// after a successful write so a failed write cannot evict a snapshot.
// Errors are logged and never returned — history I/O must not fail a page save.
func (a *App) maybeCapturePageVersion(loc history.Locator, prev, incoming []byte, reason string) {
	if a == nil || a.vaultPath == "" {
		return
	}
	enabled, max, interval := a.pageHistorySettings()
	if reason == "" {
		reason = historyReasonEditor
	}
	// Restore must stay reversible even when auto-capture is off.
	if !enabled && reason != historyReasonRestore {
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
	if reason == historyReasonEditor || reason == historyReasonSource {
		if interval > 0 {
			last, ok, err := history.Last(root, loc)
			if err != nil {
				log.Printf("page history: last lookup failed: %v", err)
			} else if ok && editorSourceIntervalApplies(last.Source) &&
				time.Since(last.Time) < time.Duration(interval)*time.Second {
				return
			}
		}
	}
	skip, err := history.Capture(root, loc, prev, reason, time.Now().UTC(), history.Options{MaxVersions: max})
	if err != nil {
		log.Printf("page history: capture failed: %v", err)
	} else if skip == history.SkipTooLarge {
		log.Printf("page history: skipped %s/%s/%s: page exceeds 1 MiB", loc.Notebook, loc.Section, loc.Page)
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

// sanitizeDeletedLocator rejects on-disk locators that would not pass
// resolvePageHistory (encoded traversal, control chars, absolute segments).
func sanitizeDeletedLocator(loc history.Locator) (history.Locator, bool) {
	nb := sanitizePathSegment(loc.Notebook)
	pg := sanitizePathSegment(loc.Page)
	if nb == "" || pg == "" || nb != loc.Notebook || pg != loc.Page {
		return history.Locator{}, false
	}
	sec, err := validateSectionPath(loc.Section, true)
	if err != nil || sec != loc.Section {
		return history.Locator{}, false
	}
	return history.Locator{Source: loc.Source, Notebook: nb, Section: sec, Page: pg}, true
}

// Swappable so restore-as tests can fail the dest write after Relocate.
var atomicPageWrite = parser.WriteFileAtomic

func liveStatError(err error) error {
	if err == nil {
		return nil
	}
	if os.IsNotExist(err) {
		return NewIPCError(CodeNavigationNotFound, "page not found")
	}
	log.Printf("page history: live page stat failed: %v", err)
	return NewIPCError(CodeNavigationUnavailable, "could not check whether that page still exists")
}

func historyWriteError(err error) error {
	if err == nil {
		return nil
	}
	if os.IsNotExist(err) {
		return NewIPCError(CodeNavigationNotFound, "page not found")
	}
	log.Printf("page history: page write failed: %v", err)
	return NewIPCError(CodeNavigationUnavailable, "could not write the page")
}

func historyFileError(err error) error {
	if err == nil {
		return nil
	}
	if os.IsNotExist(err) {
		return NewIPCError(CodeNavigationNotFound, "page not found")
	}
	return historyReadError(err)
}

func historyReadError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, history.ErrNotFound) {
		return NewIPCError(CodeNavigationNotFound, "page version not found")
	}
	log.Printf("page history: snapshot store read failed: %v", err)
	return NewIPCError(CodeNavigationUnavailable, "snapshot store read failed")
}

func editorSourceIntervalApplies(source string) bool {
	return source == historyReasonEditor || source == historyReasonSource
}

func historySection(section string) (string, error) {
	return validateSectionPath(section, true)
}

func historyLoc(source, notebook, section, page string) history.Locator {
	sec, err := historySection(section)
	if err != nil {
		// Do not alias reserved/traversal sections onto notebook-root history.
		return history.Locator{Source: source, Notebook: notebook, Section: section, Page: page}
	}
	return history.Locator{Source: source, Notebook: notebook, Section: sec, Page: page}
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
	var secErr error
	safeSection, secErr = validateSectionPath(section, true)
	if secErr != nil {
		return loc, "", "", "", "", "", "", invalidNavigationPath(secErr)
	}
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
		return nil, historyReadError(err)
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
		return "", historyReadError(err)
	}
	_, body := parser.SplitFrontmatter(string(raw))
	return body, nil
}

// RestorePageVersion replaces the live page body with a stored version.
// Current frontmatter is preserved. The pre-restore bytes are captured
// after the write succeeds so a failed restore cannot evict a snapshot.
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
					writeErr = historyFileError(err)
				}
				return
			}
			snapshot, err := history.Read(root, loc, versionID)
			if err != nil {
				writeErr = historyReadError(err)
				return
			}
			_, restoreBody := parser.SplitFrontmatter(string(snapshot))
			contentBytes, err := os.ReadFile(filePath)
			if err != nil {
				writeErr = historyFileError(err)
				return
			}
			result, writeErr = a.writePageMarkdownLocked(filePath, source, safeNotebook, safeSection, safePage, notebook, section, page, contentBytes, restoreBody, historyReasonRestore, false)
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
	// Arm focused editors before block:changed so MCP/plugin restores
	// cannot be undone by the next autosave.
	a.emit(EventPageExternalReload, parser.BlockChangedEvent{
		Notebook: safeNotebook, Section: safeSection, Page: safePage,
	})
	// Page-scoped emit so Edit-mode TipTap reloads even when IDs are unchanged
	// or the restored body has no block IDs (empty / first version).
	a.emitBlockChanged("", safeNotebook, safeSection, safePage, "")
	for _, b := range result {
		if b.ID != "" {
			a.emitBlockChanged(b.ID, safeNotebook, safeSection, safePage, b.FileDate)
		}
	}
	return nil
}

// writePageMarkdownLocked replaces a page body while preserving current
// frontmatter. The caller MUST already hold LockFileWrite for filePath.
func (a *App) writePageMarkdownLocked(filePath, source, notebook, section, page, displayNotebook, displaySection, displayPage string, contentBytes []byte, markdown, reason string, skipCapture bool) ([]parser.ParsedBlock, error) {
	frontmatter, _ := parser.SplitFrontmatter(string(contentBytes))
	if frontmatter == "" {
		today := time.Now().Format("2006-01-02")
		createdStr := time.Now().Format("2006-01-02T15:04:05")
		frontmatter = fmt.Sprintf("---\nnotebook: %s\nsection: %s\npage: %s\ndate: %s\ncreated: %s\ntags: []\n---\n",
			strconv.Quote(displayNotebook), strconv.Quote(displaySection), strconv.Quote(displayPage), strconv.Quote(today), strconv.Quote(createdStr))
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

	a.tracker.RegisterWrite(filePath)
	if err := atomicPageWrite(filePath, []byte(newContent)); err != nil {
		log.Printf("page write: atomic write failed for %s/%s/%s: %v", notebook, section, page, err)
		return nil, historyWriteError(err)
	}
	if !skipCapture {
		a.maybeCapturePageVersion(historyLoc(source, notebook, section, page), contentBytes, []byte(newContent), reason)
	}
	parsedBlocks, meta, _, _, parseErr := parser.ParseFileContent(
		newContent, notebook, section, page, fileOrDefaultDate(filePath), a.spacesPerTab,
	)
	if parseErr != nil {
		// File is already the source of truth. Returning an error here would
		// make Restore/Save look failed while disk holds the new body, and
		// skip block:changed — Edit mode would keep the old buffer.
		log.Printf("page write: parse after save failed for %s/%s/%s (file saved; index will refresh on next scan): %v", notebook, section, page, parseErr)
		return nil, nil
	}
	if idxErr := a.indexFile(source, meta.Notebook, meta.Section, meta.Page, parsedBlocks, meta, meta.Warnings...); idxErr != nil {
		log.Printf("page write: re-index after save failed for %s/%s/%s (file saved; index will refresh on next scan): %v", notebook, section, page, idxErr)
		return parsedBlocks, nil
	}
	return parsedBlocks, nil
}

// DeletedPageHistory is one orphan locator that still has retained snapshots.
type DeletedPageHistory struct {
	Notebook        string `json:"notebook"`
	Section         string `json:"section"`
	Page            string `json:"page"`
	Source          string `json:"source"`
	VersionCount    int    `json:"versionCount"`
	LatestTimestamp string `json:"latestTimestamp"`
	LatestBytes     int    `json:"latestBytes"`
}

const maxDeletedPageHistory = 500

// ListDeletedPageHistory returns leftover snapshots whose live .md is gone.
func (a *App) ListDeletedPageHistory() ([]DeletedPageHistory, error) {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.vaultPath == "" || a.db == nil {
		return nil, fmt.Errorf("vault not loaded")
	}
	var out []DeletedPageHistory
	vaultRows, err := a.collectDeletedHistory(a.vaultPath, "vault", func(loc history.Locator) string {
		return filepath.Join(a.vaultPath, loc.Notebook, loc.Section, loc.Page+".md")
	})
	if err != nil {
		return nil, historyReadError(err)
	}
	out = append(out, vaultRows...)

	a.configMu.RLock()
	linked := append([]config.LinkedNotebook(nil), a.cfg.LinkedNotebooks...)
	a.configMu.RUnlock()
	for _, ln := range linked {
		if strings.TrimSpace(ln.RootPath) == "" {
			continue
		}
		root := ln.RootPath
		rows, lerr := a.collectDeletedHistory(root, "linked", func(loc history.Locator) string {
			return filepath.Join(root, loc.Section, loc.Page+".md")
		})
		if lerr != nil {
			log.Printf("page history: list linked manifests failed: %v", lerr)
			continue
		}
		out = append(out, rows...)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].LatestTimestamp == out[j].LatestTimestamp {
			return out[i].Notebook+out[i].Section+out[i].Page < out[j].Notebook+out[j].Section+out[j].Page
		}
		return out[i].LatestTimestamp > out[j].LatestTimestamp
	})
	if len(out) > maxDeletedPageHistory {
		out = out[:maxDeletedPageHistory]
	}
	if out == nil {
		out = []DeletedPageHistory{}
	}
	return out, nil
}

func (a *App) collectDeletedHistory(root, wantSource string, livePath func(history.Locator) string) ([]DeletedPageHistory, error) {
	locs, err := history.ListManifests(root)
	if err != nil {
		return nil, err
	}
	var out []DeletedPageHistory
	for _, loc := range locs {
		if loc.Source != wantSource {
			continue
		}
		safe, ok := sanitizeDeletedLocator(loc)
		if !ok {
			log.Printf("page history: skip leftover with invalid locator %q/%q/%q", loc.Notebook, loc.Section, loc.Page)
			continue
		}
		_, resolvedRoot, liveFile, _, _, _, _, rerr := a.resolvePageHistory(safe.Notebook, safe.Section, safe.Page)
		if rerr == nil && resolvedRoot != "" && resolvedRoot != root {
			log.Printf("page history: skip leftover %q/%q/%q — name now resolves to a different store", safe.Notebook, safe.Section, safe.Page)
			continue
		}
		p := livePath(safe)
		if rerr == nil && liveFile != "" {
			p = liveFile
		}
		if !isPathWithinRoot(p, root) {
			log.Printf("page history: skip leftover outside history root %q/%q/%q", safe.Notebook, safe.Section, safe.Page)
			continue
		}
		if _, err := os.Stat(p); err == nil {
			continue
		} else if err != nil && !os.IsNotExist(err) {
			log.Printf("page history: skip leftover, live stat indeterminate %s: %v", p, err)
			continue
		}
		entries, err := history.List(root, loc)
		if err != nil {
			log.Printf("page history: list leftover %s/%s/%s: %v", loc.Notebook, loc.Section, loc.Page, err)
			continue
		}
		if len(entries) == 0 {
			continue
		}
		latest := entries[0]
		out = append(out, DeletedPageHistory{
			Notebook:        safe.Notebook,
			Section:         safe.Section,
			Page:            safe.Page,
			Source:          safe.Source,
			VersionCount:    len(entries),
			LatestTimestamp: latest.Time.UTC().Format(time.RFC3339),
			LatestBytes:     latest.Bytes,
		})
	}
	return out, nil
}

// RestoreDeletedPageVersion recreates a missing page from a retained snapshot.
// Empty destNotebook+destPage restore at the original locator. If the source
// live file or dest file already exists the call fails without writing.
func (a *App) RestoreDeletedPageVersion(notebook, section, page, versionID, destNotebook, destSection, destPage string) error {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.vaultPath == "" || a.db == nil {
		return fmt.Errorf("vault not loaded")
	}
	destNBIn := strings.TrimSpace(destNotebook)
	destPageIn := strings.TrimSpace(destPage)
	if destNBIn == "" && destPageIn == "" {
		destNotebook, destSection, destPage = notebook, section, page
	} else if destNBIn == "" || destPageIn == "" {
		return NewIPCError(CodeInvalidNavigationPath, "restore destination needs both a notebook and a page name")
	}
	origLoc, origRoot, origFile, _, _, _, _, err := a.resolvePageHistory(notebook, section, page)
	if err != nil {
		return err
	}
	if origRoot == "" || strings.TrimSpace(versionID) == "" {
		return NewIPCError(CodeNavigationNotFound, "page version not found")
	}
	destLoc, destRoot, destFile, destSource, destNB, destSec, destPg, err := a.resolvePageHistory(destNotebook, destSection, destPage)
	if err != nil {
		return err
	}
	if destRoot == "" {
		return NewIPCError(CodeNavigationNotFound, "page not found")
	}
	if destRoot != origRoot {
		return NewIPCError(CodeInvalidNavigationPath, "restore that snapshot in the same vault or linked notebook it came from")
	}
	destNotebookDir, err := a.resolveNotebookDir(destNB, destSource)
	if err != nil {
		return err
	}
	if !isCreationPathWithinRoot(destFile, destNotebookDir) {
		return fmt.Errorf("path escapes notebook root")
	}
	if _, err := os.Stat(origFile); err == nil {
		return NewIPCError(CodePageStillExists, "that page still exists; restore it from page history instead")
	} else if !os.IsNotExist(err) {
		return liveStatError(err)
	}

	snapshot, err := history.Read(origRoot, origLoc, versionID)
	if err != nil {
		return historyReadError(err)
	}
	remapped, err := remapSnapshotIdentity(snapshot, destNB, destSec, destPg)
	if err != nil {
		return err
	}
	_, restoreBody := parser.SplitFrontmatter(string(remapped))

	occupiedMsg := "restoring here would overwrite an existing page; choose a different location"
	leftoverOccupiedMsg := "that name still has leftover page history; restore it in place or choose a different location"
	needRelocate := destLoc != origLoc && !history.SameStore(origRoot, origLoc, destLoc)
	if _, err := os.Stat(destFile); err == nil {
		return NewIPCError(CodePageExists, occupiedMsg)
	} else if !os.IsNotExist(err) {
		return historyWriteError(err)
	}
	if needRelocate && destLeftoverOccupied(destRoot, destLoc) {
		return NewIPCError(CodePageHistoryExists, leftoverOccupiedMsg)
	}

	a.wg.Add(1)
	defer a.wg.Done()

	var result []parser.ParsedBlock
	var writeErr error
	a.coordinator.LockPathsWrite([]string{origFile, destFile}, func() {
		if _, err := os.Stat(origFile); err == nil {
			writeErr = NewIPCError(CodePageStillExists, "that page still exists; restore it from page history instead")
			return
		} else if !os.IsNotExist(err) {
			writeErr = liveStatError(err)
			return
		}
		if _, err := os.Stat(destFile); err == nil {
			writeErr = NewIPCError(CodePageExists, occupiedMsg)
			return
		} else if !os.IsNotExist(err) {
			writeErr = historyWriteError(err)
			return
		}
		if _, rerr := history.Read(origRoot, origLoc, versionID); rerr != nil {
			writeErr = historyReadError(rerr)
			return
		}
		if err := os.MkdirAll(filepath.Dir(destFile), 0755); err != nil {
			writeErr = historyWriteError(err)
			return
		}
		needRelocate := destLoc != origLoc && !history.SameStore(origRoot, origLoc, destLoc)
		relocated := false
		if needRelocate {
			if destLeftoverOccupied(destRoot, destLoc) {
				writeErr = NewIPCError(CodePageHistoryExists, leftoverOccupiedMsg)
				return
			}
			leftover, lerr := history.List(origRoot, origLoc)
			if lerr != nil {
				writeErr = historyReadError(lerr)
				return
			}
			if len(leftover) == 0 {
				writeErr = NewIPCError(CodeNavigationNotFound, "those snapshots were already restored")
				return
			}
			writeErr = a.movePageHistory(destSource, origLoc.Notebook, origLoc.Section, origLoc.Page, destNB, destSec, destPg, false)
			if writeErr != nil {
				return
			}
			relocated = true
		}
		// Remapped snapshot supplies frontmatter (type/tags/created/…). Skip
		// capture: there is no live file, and a synthetic prev would evict a
		// real version at the cap.
		result, writeErr = a.writePageMarkdownLocked(
			destFile, destSource, destNB, destSec, destPg,
			destNB, destSec, destPg,
			remapped, restoreBody, historyReasonRestore, true,
		)
		if writeErr != nil && relocated {
			if backErr := a.movePageHistory(destSource, destNB, destSec, destPg, origLoc.Notebook, origLoc.Section, origLoc.Page, false); backErr != nil {
				log.Printf("page history: relocate-back after write failure: %v", backErr)
			}
			return
		}
		if relocated {
			a.prunePageHistory(destSource, destNB, destSec, destPg)
		}
	})
	if writeErr != nil {
		return writeErr
	}

	_ = result
	a.emitBlockChanged("", destNB, destSec, destPg, "")
	if err := a.reconcileNavigationPage(destNB, destSec, destPg, destPg, false); err != nil {
		log.Printf("page history: restore reconcile failed for %s/%s/%s: %v", destNB, destSec, destPg, err)
	}
	return nil
}

// remapSnapshotIdentity keeps snapshot frontmatter (type, tags, created, …)
// and rewrites only the path identity to the sanitized dest. Snapshots with
// no frontmatter are left unchanged so writePageMarkdownLocked can mint.
func remapSnapshotIdentity(snapshot []byte, notebook, section, page string) ([]byte, error) {
	content := string(snapshot)
	fm, _ := parser.SplitFrontmatter(content)
	if fm == "" {
		return snapshot, nil
	}
	var err error
	content, err = parser.SetFrontmatterField(content, "notebook", notebook)
	if err != nil {
		return nil, err
	}
	content, err = parser.SetFrontmatterField(content, "section", section)
	if err != nil {
		return nil, err
	}
	content, err = parser.SetFrontmatterField(content, "page", page)
	if err != nil {
		return nil, err
	}
	return []byte(content), nil
}

func destLeftoverOccupied(root string, loc history.Locator) bool {
	entries, err := history.List(root, loc)
	return err == nil && len(entries) > 0
}

func (a *App) relocatePageHistory(source, oldNotebook, oldSection, oldPage, newNotebook, newSection, newPage string) error {
	return a.movePageHistory(source, oldNotebook, oldSection, oldPage, newNotebook, newSection, newPage, true)
}

func (a *App) movePageHistory(source, oldNotebook, oldSection, oldPage, newNotebook, newSection, newPage string, prune bool) error {
	root := a.historyRoot(source)
	if root == "" {
		return nil
	}
	oldLoc := historyLoc(source, oldNotebook, oldSection, oldPage)
	newLoc := historyLoc(source, newNotebook, newSection, newPage)
	if err := history.Relocate(root, oldLoc, newLoc); err != nil {
		log.Printf("page history: relocate failed: %v", err)
		return NewIPCError(CodeNavigationUnavailable, "could not move the snapshot history")
	}
	if prune {
		a.prunePageHistory(source, newNotebook, newSection, newPage)
	}
	return nil
}

func (a *App) prunePageHistory(source, notebook, section, page string) {
	root := a.historyRoot(source)
	if root == "" {
		return
	}
	_, max, _ := a.pageHistorySettings()
	if max <= 0 {
		return
	}
	if err := history.Prune(root, historyLoc(source, notebook, section, page), max); err != nil {
		log.Printf("page history: prune after relocate failed: %v", err)
	}
}
