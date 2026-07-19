package main

import (
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"silt/backend/parser"
	"strconv"
	"strings"
	"time"

	"silt/backend/config"
	"silt/backend/core"
)

// --- Rename / Delete lifecycle (#62, #83) ---------------------------------

// collectMarkdownFilePaths walks root and returns every .md file path under it.
// Used so structural rename/delete can LockPathsWrite every descendant page
// before os.Rename — directory lock keys alone do not exclude page saves (#691).
func collectMarkdownFilePaths(root string) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".md") {
			return nil
		}
		paths = append(paths, path)
		return nil
	})
	return paths, err
}

// lockPathSet is a normalized set for comparing pre-lock vs in-lock walks.
func lockPathSet(paths []string) map[string]bool {
	m := make(map[string]bool, len(paths))
	for _, p := range paths {
		if n := core.NormalizeFileLockPath(p); n != "" {
			m[n] = true
		}
	}
	return m
}

// missingMarkdownUnder returns fresh .md paths under root that are not already
// in locked (normalized). Used to detect walk-then-lock TOCTOU and retry.
func missingMarkdownUnder(root string, locked map[string]bool) ([]string, error) {
	fresh, err := collectMarkdownFilePaths(root)
	if err != nil {
		return nil, err
	}
	var missing []string
	for _, p := range fresh {
		n := core.NormalizeFileLockPath(p)
		if n != "" && !locked[n] {
			missing = append(missing, p)
		}
	}
	return missing, nil
}

// renameTargetsFromMarkdownUnder builds renameTarget rows for .md paths under
// rootDir that appear in lockPaths (normalized). sectionPrefix is the section
// path of rootDir within the notebook (empty for notebook-root renames).
func renameTargetsFromMarkdownUnder(rootDir, notebook, sectionPrefix string, lockPaths []string) []renameTarget {
	var out []renameTarget
	for _, p := range lockPaths {
		if !strings.EqualFold(filepath.Ext(p), ".md") {
			continue
		}
		rel, relErr := filepath.Rel(rootDir, p)
		if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		relSlash := filepath.ToSlash(rel)
		parts := strings.Split(relSlash, "/")
		page := strings.TrimSuffix(parts[len(parts)-1], filepath.Ext(parts[len(parts)-1]))
		if page == "" {
			continue
		}
		sec := sectionPrefix
		if len(parts) > 1 {
			nested := strings.Join(parts[:len(parts)-1], "/")
			if sec != "" {
				sec += "/" + nested
			} else {
				sec = nested
			}
		}
		out = append(out, renameTarget{notebook, sec, page})
	}
	return out
}

type renameHooks struct {
	writeFileAtomic   func(string, []byte) error
	indexFile         func(*App, string, string, string, string, []parser.ParsedBlock, []string, ...string) error
	reconcileSection  func(*App, string, string, string, bool) error
	reconcileNotebook func(*App, string, string, bool) error
}

type renameFileSnapshot struct {
	oldPath, relPath string
	oldContent       []byte
	updatedContent   []byte
	oldNotebook      string
	newNotebook      string
	oldSection       string
	newSection       string
	page             string
}

type renameLinkJournalEntry struct {
	content  []byte
	source   string
	notebook string
	section  string
	page     string
}

func (a *App) renameWriteFileAtomic(path string, content []byte) error {
	if a.renameHooks != nil && a.renameHooks.writeFileAtomic != nil {
		return a.renameHooks.writeFileAtomic(path, content)
	}
	return parser.WriteFileAtomic(path, content)
}

func (a *App) renameIndexFile(source, notebook, section, page string, blocks []parser.ParsedBlock, tags []string, warnings ...string) error {
	if a.renameHooks != nil && a.renameHooks.indexFile != nil {
		return a.renameHooks.indexFile(a, source, notebook, section, page, blocks, tags, warnings...)
	}
	return a.indexFile(source, notebook, section, page, blocks, tags, warnings...)
}

func (a *App) indexFile(source, notebook, section, page string, blocks []parser.ParsedBlock, tags []string, warnings ...string) error {
	var err error
	a.coordinator.WithDBWrite(func() {
		err = a.db.IndexFileBlocks(source, notebook, section, page, blocks, tags, warnings...)
	})
	return err
}

func (a *App) reindexFileContent(filePath, source, notebook, section, page string, content []byte, useRenameHook bool) error {
	blocks, meta, _, _, parseErr := parser.ParseFileContent(
		string(content), notebook, section, page,
		fileOrDefaultDate(filePath), a.spacesPerTab,
	)
	if parseErr != nil {
		return fmt.Errorf("parse %s: %w", filePath, parseErr)
	}
	index := a.indexFile
	if useRenameHook {
		index = a.renameIndexFile
	}
	if err := index(source, meta.Notebook, meta.Section, meta.Page, blocks, meta.Tags, meta.Warnings...); err != nil {
		return fmt.Errorf("index %s: %w", filePath, err)
	}
	stat, statErr := os.Stat(filePath)
	if statErr != nil {
		return fmt.Errorf("stat indexed file %s: %w", filePath, statErr)
	}
	var markErr error
	a.coordinator.WithDBWrite(func() {
		markErr = a.db.MarkFileIndexed(nil, filePath, stat.ModTime().UnixNano(), stat.Size())
	})
	if markErr != nil {
		return fmt.Errorf("record indexed file %s: %w", filePath, markErr)
	}
	if useRenameHook {
		for _, b := range blocks {
			if b.ID != "" {
				a.emitBlockChanged(b.ID, meta.Notebook, meta.Section, meta.Page, b.FileDate)
			}
		}
	}
	return nil
}

func (a *App) reindexFileStrict(filePath, source, notebook, section, page string, useRenameHook bool) error {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}
	return a.reindexFileContent(filePath, source, notebook, section, page, content, useRenameHook)
}

func (a *App) snapshotConfig() config.SystemConfig {
	a.configMu.RLock()
	defer a.configMu.RUnlock()
	return config.Clone(a.cfg)
}

func (a *App) restoreConfig(snapshot config.SystemConfig) error {
	a.configMu.Lock()
	defer a.configMu.Unlock()
	if err := a.saveConfigTracked(snapshot); err != nil {
		return err
	}
	a.cfg = config.Clone(snapshot)
	return nil
}

func (a *App) renameReconcileSection(notebook, oldPath, newPath string, remove bool) error {
	if a.renameHooks != nil && a.renameHooks.reconcileSection != nil {
		return a.renameHooks.reconcileSection(a, notebook, oldPath, newPath, remove)
	}
	return a.reconcileNavigationSection(notebook, oldPath, newPath, remove)
}

func (a *App) renameReconcileNotebook(oldName, newName string, remove bool) error {
	if a.renameHooks != nil && a.renameHooks.reconcileNotebook != nil {
		return a.renameHooks.reconcileNotebook(a, oldName, newName, remove)
	}
	return a.reconcileNavigationNotebook(oldName, newName, remove)
}

func (a *App) rollbackRename(
	oldDir, newDir string,
	files []renameFileSnapshot,
	source string,
	linkJournal map[string]renameLinkJournalEntry,
	configSnapshot config.SystemConfig,
	restoreConfigSnapshot bool,
) error {
	var rollbackErrs []error

	// Restore inbound-link sources while the renamed tree is still at newDir.
	// Caller already holds LockPathsWrite on these paths (and descendants); do
	// not re-enter LockFileWrite (#691 non-reentrant).
	for path, entry := range linkJournal {
		a.tracker.RegisterWrite(path)
		if err := parser.WriteFileAtomic(path, entry.content); err != nil {
			rollbackErrs = append(rollbackErrs, fmt.Errorf("restore inbound links %s: %w", path, err))
			continue
		}
		if err := a.reindexFileContent(path, entry.source, entry.notebook, entry.section, entry.page, entry.content, false); err != nil {
			rollbackErrs = append(rollbackErrs, fmt.Errorf("restore inbound index %s: %w", path, err))
		}
	}

	// Restore every descendant's original bytes before moving the directory
	// back. The rollback path deliberately bypasses the forward-write hook.
	// Caller holds structural file locks for the forward op (#691).
	for _, file := range files {
		path := filepath.Join(newDir, file.relPath)
		a.tracker.RegisterWrite(path)
		if err := parser.WriteFileAtomic(path, file.oldContent); err != nil {
			rollbackErrs = append(rollbackErrs, fmt.Errorf("restore %s: %w", file.relPath, err))
		}
	}
	if err := os.Rename(newDir, oldDir); err != nil {
		rollbackErrs = append(rollbackErrs, fmt.Errorf("restore renamed directory: %w", err))
	}

	// Rebuild the old index from the captured source bytes. This clears both
	// possible locations first, so a partial forward index cannot survive.
	for _, file := range files {
		a.coordinator.WithDBWrite(func() {
			if err := a.db.ClearFileBlocks(nil, source, file.newNotebook, file.newSection, file.page); err != nil {
				rollbackErrs = append(rollbackErrs, fmt.Errorf("clear new index %s: %w", file.relPath, err))
			}
			if err := a.db.ClearFileBlocks(nil, source, file.oldNotebook, file.oldSection, file.page); err != nil {
				rollbackErrs = append(rollbackErrs, fmt.Errorf("clear old index %s: %w", file.relPath, err))
			}
			if err := a.db.ForgetFile(filepath.Join(newDir, file.relPath)); err != nil {
				rollbackErrs = append(rollbackErrs, fmt.Errorf("forget new index path %s: %w", file.relPath, err))
			}
			if err := a.db.ForgetFile(file.oldPath); err != nil {
				rollbackErrs = append(rollbackErrs, fmt.Errorf("forget old index path %s: %w", file.relPath, err))
			}
		})
		if err := a.reindexFileContent(file.oldPath, source, file.oldNotebook, file.oldSection, file.page, file.oldContent, false); err != nil {
			rollbackErrs = append(rollbackErrs, fmt.Errorf("restore index %s: %w", file.relPath, err))
		}
	}
	if restoreConfigSnapshot {
		if err := a.restoreConfig(configSnapshot); err != nil {
			rollbackErrs = append(rollbackErrs, fmt.Errorf("restore navigation config: %w", err))
		}
	}
	return errors.Join(rollbackErrs...)
}

// trashBase returns the .system/trash directory path.
func (a *App) trashBase() string {
	return filepath.Join(a.vaultPath, ".system", "trash")
}

// moveToTrash moves a file or directory to .system/trash/<timestamp>/<relPath>,
// preserving the relative structure so the user can recover it. Returns the
// trash destination path. The caller MUST guard with isPathWithinRoot.
func (a *App) moveToTrash(source string) (string, error) {
	rel, err := filepath.Rel(a.vaultPath, source)
	if err != nil {
		return "", fmt.Errorf("cannot compute relative path: %w", err)
	}
	ts := time.Now().Format("20060102-150405")
	dest := filepath.Join(a.trashBase(), ts, rel)
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return "", fmt.Errorf("failed to create trash directory: %w", err)
	}
	if err := os.Rename(source, dest); err != nil {
		return "", fmt.Errorf("failed to move to trash: %w", err)
	}
	return dest, nil
}

// reindexFile reads, parses, and indexes a single .md file at the given path.
// Used by rename operations where the file content changed (frontmatter) or
// the path changed (folder rename). The caller MUST hold the file lock.
func (a *App) reindexFile(filePath, notebook, section, page string) {
	contentBytes, err := os.ReadFile(filePath)
	if err != nil {
		log.Printf("reindexFile: failed to read %s: %v", filePath, err)
		return
	}
	content := string(contentBytes)
	blocks, meta, _, _, parseErr := parser.ParseFileContent(
		content, notebook, section, page,
		fileOrDefaultDate(filePath), a.spacesPerTab,
	)
	if parseErr != nil {
		log.Printf("reindexFile: parse failed for %s: %v", filePath, parseErr)
		return
	}
	var idxErr error
	reidxSource := a.resolveSourceByName(notebook)
	a.coordinator.WithDBWrite(func() {
		idxErr = a.db.IndexFileBlocks(reidxSource, meta.Notebook, meta.Section, meta.Page, blocks, meta.Tags, meta.Warnings...)
	})
	if idxErr != nil {
		log.Printf("reindexFile: index failed for %s/%s/%s: %v", meta.Notebook, meta.Section, meta.Page, idxErr)
	}
	// Emit block:changed so live embeds/references refresh.
	for _, b := range blocks {
		if b.ID != "" {
			a.emitBlockChanged(b.ID, meta.Notebook, meta.Section, meta.Page, b.FileDate)
		}
	}
}

// updateFrontmatterField rewrites a single YAML key in the frontmatter block.
// It performs a simple line-based replacement of `key: "old"` → `key: "new"`.
// The caller MUST hold the file lock and call tracker.RegisterWrite.
func updateFrontmatterField(content, key, newVal string) string {
	lines := strings.Split(content, "\n")
	inFM := false
	closeIdx := -1
	found := false
	for i, line := range lines {
		if strings.TrimSpace(line) == "---" {
			if !inFM {
				inFM = true
				continue
			}
			closeIdx = i
			break // closing ---
		}
		if inFM {
			prefix := key + ":"
			if strings.HasPrefix(strings.TrimSpace(line), prefix) {
				lines[i] = fmt.Sprintf("%s: %s", key, strconv.Quote(newVal))
				found = true
				break
			}
		}
	}
	// If the frontmatter exists but the key was absent, insert it before
	// the closing --- so externally-authored files (external editors) that
	// lack the key gain it on rename rather than silently no-oping.
	if inFM && !found && closeIdx >= 0 {
		newLine := fmt.Sprintf("%s: %s", key, strconv.Quote(newVal))
		lines = append(lines[:closeIdx], append([]string{newLine}, lines[closeIdx:]...)...)
	}
	return strings.Join(lines, "\n")
}

// RenamePage renames a single page file. Updates the page: frontmatter value,
// moves the file, and re-indexes. Block UUIDs are preserved so references
// and embeds keep resolving (#62, #83).
func (a *App) RenamePage(notebook, section, oldName, newName string) error {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.db == nil {
		return fmt.Errorf("vault database not loaded")
	}
	safeNotebook := sanitizePathSegment(notebook)
	safeSection, sectionErr := validateSectionPath(section, true)
	safeOldPage := sanitizePathSegment(oldName)
	safeNewPage := sanitizePathSegment(newName)
	if sectionErr != nil {
		return invalidNavigationPath(sectionErr)
	}
	if safeNotebook == "" || safeOldPage == "" || safeNewPage == "" {
		return fmt.Errorf("notebook and page names are required")
	}
	if safeOldPage == safeNewPage {
		return nil
	}

	source := a.resolveSourceByName(safeNotebook)
	notebookDir, err := a.resolveNotebookDir(safeNotebook, source)
	if err != nil {
		return err
	}
	oldFile := filepath.Join(notebookDir, safeSection, safeOldPage+".md")
	newFile := filepath.Join(notebookDir, safeSection, safeNewPage+".md")
	if !isPathWithinRoot(oldFile, notebookDir) || !isCreationPathWithinRoot(newFile, notebookDir) {
		return fmt.Errorf("path escapes notebook root")
	}
	if _, err := os.Stat(newFile); err == nil {
		return fmt.Errorf("a page named %q already exists", safeNewPage)
	}

	a.wg.Add(1)
	defer a.wg.Done()

	targets := []renameTarget{{safeNotebook, safeSection, safeOldPage}}
	inbound, err := a.collectInboundSourcePaths(targets)
	if err != nil {
		return fmt.Errorf("collect inbound sources: %w", err)
	}
	lockPaths := append([]string{oldFile, newFile}, inbound...)

	// Retry when inbound sources appear after pre-lock collect (TOCTOU).
	var runErr error
	for attempt := 0; attempt < 8; attempt++ {
		var needRetry []string
		runErr = nil
		// Lock page + inbound sources so concurrent saves cannot interleave.
		a.coordinator.LockPathsWrite(lockPaths, func() {
			missing, mErr := a.missingInboundPaths(targets, lockPathSet(lockPaths))
			if mErr != nil {
				runErr = mErr
				return
			}
			if len(missing) > 0 {
				needRetry = missing
				return
			}

			// 1. Read the file content before renaming.
			contentBytes, err := os.ReadFile(oldFile)
			if err != nil {
				runErr = err
				return
			}

			// 2. Rename old → new FIRST. If this fails, nothing was modified
			// (clean state). This avoids the stale-frontmatter-at-old-path
			// inconsistency that would occur if we wrote frontmatter first.
			a.tracker.RegisterWrite(oldFile)
			a.tracker.RegisterWrite(newFile)
			if err := os.Rename(oldFile, newFile); err != nil {
				runErr = err
				return
			}

			// 3. Update frontmatter at the new path. If this fails, the file
			// is at the correct new path with stale frontmatter — the scanner
			// will use the path-derived page name, which matches the sidebar.
			content := updateFrontmatterField(string(contentBytes), "page", safeNewPage)
			a.tracker.RegisterWrite(newFile)
			if err := parser.WriteFileAtomic(newFile, []byte(content)); err != nil {
				runErr = err
				return
			}

			// 4. Rewrite inbound [[…]] wiki-links BEFORE clearing the old index,
			// so the resolution-based rewrite sees the pre-rename page inventory.
			a.rewriteInboundPageLinksWithJournal(safeNotebook, safeSection, safeOldPage, safeNotebook, safeSection, safeNewPage, nil, lockPathSet(lockPaths))

			// 5. Clear old index entries + re-index at new path.
			a.coordinator.WithDBWrite(func() {
				_ = a.db.ClearFileBlocks(nil, source, safeNotebook, safeSection, safeOldPage)
			})
			a.coordinator.WithDBWrite(func() {
				_ = a.db.ForgetFile(oldFile)
			})
			a.reindexFile(newFile, safeNotebook, safeSection, safeNewPage)
		})
		if len(needRetry) > 0 {
			lockPaths = unionLockPaths(lockPaths, needRetry)
			continue
		}
		if runErr != nil {
			return runErr
		}
		return a.reconcileNavigationPage(safeNotebook, safeSection, safeOldPage, safeNewPage, false)
	}
	return fmt.Errorf("RenamePage: inbound lock set did not stabilize after concurrent link creates")
}

// MovePage moves a page from one section to another (or to the notebook root
// when toSection == "") within the same notebook (#177). The .md file is
// renamed on disk, its `section:` frontmatter is rewritten, the block index
// is rebuilt at the new path, and nav_order is adjusted for both the source
// and target sectionKeys. Returns an error on name collision. Cross-notebook
// moves are out of scope — the page stays within `notebook`. Block UUIDs are
// preserved so references and embeds keep resolving.
func (a *App) MovePage(notebook, fromSection, toSection, page string) error {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.db == nil {
		return fmt.Errorf("vault database not loaded")
	}
	safeNotebook := sanitizePathSegment(notebook)
	safeFrom, fromErr := validateSectionPath(fromSection, true)
	safeTo, toErr := validateSectionPath(toSection, true)
	safePage := sanitizePathSegment(page)
	if fromErr != nil {
		return invalidNavigationPath(fromErr)
	}
	if toErr != nil {
		return invalidNavigationPath(toErr)
	}
	if safeNotebook == "" || safePage == "" {
		return fmt.Errorf("notebook and page names are required")
	}
	if safeFrom == safeTo {
		return nil // already in the target section
	}

	source := a.resolveSourceByName(safeNotebook)
	notebookDir, err := a.resolveNotebookDir(safeNotebook, source)
	if err != nil {
		return err
	}
	oldFile := filepath.Join(notebookDir, safeFrom, safePage+".md")
	newFile := filepath.Join(notebookDir, safeTo, safePage+".md")
	if !isPathWithinRoot(oldFile, notebookDir) || !isCreationPathWithinRoot(newFile, notebookDir) {
		return fmt.Errorf("path escapes notebook root")
	}
	if _, err := os.Stat(oldFile); os.IsNotExist(err) {
		return fmt.Errorf("page %q not found in section %q", safePage, safeFrom)
	}
	if _, err := os.Stat(newFile); err == nil {
		return fmt.Errorf("a page named %q already exists in that section", safePage)
	}

	a.wg.Add(1)
	defer a.wg.Done()

	targets := []renameTarget{{safeNotebook, safeFrom, safePage}}
	inbound, err := a.collectInboundSourcePaths(targets)
	if err != nil {
		return fmt.Errorf("collect inbound sources: %w", err)
	}
	lockPaths := append([]string{oldFile, newFile}, inbound...)

	var runErr error
	for attempt := 0; attempt < 8; attempt++ {
		var needRetry []string
		runErr = nil
		// Lock page + inbound sources so concurrent saves cannot interleave.
		a.coordinator.LockPathsWrite(lockPaths, func() {
			missing, mErr := a.missingInboundPaths(targets, lockPathSet(lockPaths))
			if mErr != nil {
				runErr = mErr
				return
			}
			if len(missing) > 0 {
				needRetry = missing
				return
			}

			// 1. Ensure the target section directory exists (handles nested
			// sections like "Projects/Active" and the section-less root, which
			// is the notebook dir itself).
			targetDir := filepath.Dir(newFile)
			if err := os.MkdirAll(targetDir, 0o755); err != nil {
				runErr = fmt.Errorf("failed to create target section directory: %w", err)
				return
			}

			// 2. Read the file content before moving.
			contentBytes, err := os.ReadFile(oldFile)
			if err != nil {
				runErr = err
				return
			}

			// 3. Rename old → new. If this fails, nothing was modified.
			a.tracker.RegisterWrite(oldFile)
			a.tracker.RegisterWrite(newFile)
			if err := os.Rename(oldFile, newFile); err != nil {
				runErr = err
				return
			}

			// 4. Rewrite the section: frontmatter at the new path. An empty
			// safeTo produces `section: ""` (section-less), matching the
			// parser's section-less convention. If this fails, the file is at
			// the new path with stale frontmatter — we log the error and
			// continue through the index cleanup (step 5) so the index doesn't
			// dangle at the old path. The scanner will reconcile on next pass.
			content := updateFrontmatterField(string(contentBytes), "section", safeTo)
			a.tracker.RegisterWrite(newFile)
			if err := parser.WriteFileAtomic(newFile, []byte(content)); err != nil {
				log.Printf("MovePage: WriteFileAtomic failed at %s (file already moved): %v", newFile, err)
			}

			// 5. Rewrite inbound [[…]] BEFORE clearing the old index, so the
			// resolution-based rewrite sees the pre-move page inventory (#545).
			a.rewriteInboundPageLinksWithJournal(safeNotebook, safeFrom, safePage, safeNotebook, safeTo, safePage, nil, lockPathSet(lockPaths))

			// 6. Clear old index entries + re-index at the new path. These run
			// unconditionally — even if the frontmatter write failed, the file
			// has already moved and the old index entries must be cleaned up.
			a.coordinator.WithDBWrite(func() {
				_ = a.db.ClearFileBlocks(nil, source, safeNotebook, safeFrom, safePage)
			})
			a.coordinator.WithDBWrite(func() {
				_ = a.db.ForgetFile(oldFile)
			})
			a.reindexFile(newFile, safeNotebook, safeTo, safePage)
			// If frontmatter write failed, the file has already moved — do not
			// surface the error to the user. The scanner reconciles stale
			// frontmatter on the next pass.
		})
		if len(needRetry) > 0 {
			lockPaths = unionLockPaths(lockPaths, needRetry)
			continue
		}
		if runErr != nil {
			return runErr
		}

		// Update nav_order outside the multi-path lock. File move already
		// succeeded; config persist failure is logged only.
		if err := a.reconcileNavigationMove(safeNotebook, safeFrom, safeTo, safePage); err != nil {
			log.Printf("MovePage: nav_order persist failed (file move succeeded): %v", err)
		}
		return nil
	}
	return fmt.Errorf("MovePage: inbound lock set did not stabilize after concurrent link creates")
}

// RenameSection renames a section folder and updates the section: frontmatter
// in every .md file it contains. All affected blocks are re-indexed (#62).
func (a *App) RenameSection(notebook, oldName, newName string) error {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.db == nil {
		return fmt.Errorf("vault database not loaded")
	}
	safeNotebook := sanitizePathSegment(notebook)
	safeOldSection, oldErr := validateSectionPath(oldName, false)
	safeNewSection, newErr := validateSectionPath(newName, false)
	if oldErr != nil {
		return invalidNavigationPath(oldErr)
	}
	if newErr != nil {
		return invalidNavigationPath(newErr)
	}
	if safeNotebook == "" {
		return fmt.Errorf("notebook and section names are required")
	}
	if safeOldSection == safeNewSection {
		return nil
	}

	source := a.resolveSourceByName(safeNotebook)
	notebookDir, err := a.resolveNotebookDir(safeNotebook, source)
	if err != nil {
		return err
	}
	oldDir := filepath.Join(notebookDir, safeOldSection)
	newDir := filepath.Join(notebookDir, safeNewSection)
	if !isPathWithinRoot(oldDir, notebookDir) || !isCreationPathWithinRoot(newDir, notebookDir) {
		return fmt.Errorf("path escapes notebook root")
	}
	if _, err := os.Stat(newDir); err == nil {
		return fmt.Errorf("a section named %q already exists", safeNewSection)
	}

	a.wg.Add(1)
	defer a.wg.Done()

	// Build rename targets for inbound-source lock collection (pre-lock).
	seedPaths, err := collectMarkdownFilePaths(oldDir)
	if err != nil {
		return err
	}
	var renameTargets []renameTarget
	for _, p := range seedPaths {
		rel, relErr := filepath.Rel(oldDir, p)
		if relErr != nil {
			continue
		}
		relSlash := filepath.ToSlash(rel)
		parts := strings.Split(relSlash, "/")
		page := strings.TrimSuffix(parts[len(parts)-1], filepath.Ext(parts[len(parts)-1]))
		oldSection := safeOldSection
		if len(parts) > 1 {
			oldSection += "/" + strings.Join(parts[:len(parts)-1], "/")
		}
		renameTargets = append(renameTargets, renameTarget{safeNotebook, oldSection, page})
	}
	inbound, err := a.collectInboundSourcePaths(renameTargets)
	if err != nil {
		return fmt.Errorf("collect inbound sources: %w", err)
	}
	lockPaths := append(append([]string{}, seedPaths...), inbound...)

	var files []renameFileSnapshot
	configSnapshot := a.snapshotConfig()
	linkJournal := make(map[string]renameLinkJournalEntry)
	configAttempted := false
	var runErr error
	// Retry if a .md appears under oldDir or inbound sources grow after collect (TOCTOU).
	for attempt := 0; attempt < 8; attempt++ {
		var needRetry []string
		files = nil
		runErr = nil
		configAttempted = false
		linkJournal = make(map[string]renameLinkJournalEntry)
		// Lock every descendant page path + inbound sources so concurrent saves
		// cannot interleave with the directory rename.
		a.coordinator.LockPathsWrite(lockPaths, func() {
			missing, mErr := missingMarkdownUnder(oldDir, lockPathSet(lockPaths))
			if mErr != nil {
				runErr = mErr
				return
			}
			if len(missing) > 0 {
				needRetry = missing
				return
			}
			// Re-check inbound under lock so late wiki-links are locked too.
			lockedTargets := renameTargetsFromMarkdownUnder(oldDir, safeNotebook, safeOldSection, lockPaths)
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
			_ = filepath.WalkDir(oldDir, func(path string, entry fs.DirEntry, walkErr error) error {
				if walkErr != nil {
					runErr = walkErr
					return walkErr
				}
				if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".md") {
					return nil
				}
				content, readErr := os.ReadFile(path)
				if readErr != nil {
					runErr = readErr
					return readErr
				}
				rel, relErr := filepath.Rel(oldDir, path)
				if relErr != nil {
					runErr = relErr
					return relErr
				}
				relSlash := filepath.ToSlash(rel)
				parts := strings.Split(relSlash, "/")
				page := strings.TrimSuffix(parts[len(parts)-1], filepath.Ext(parts[len(parts)-1]))
				oldSection := safeOldSection
				if len(parts) > 1 {
					oldSection += "/" + strings.Join(parts[:len(parts)-1], "/")
				}
				newSection := safeNewSection
				if idx := strings.LastIndex(filepath.ToSlash(rel), "/"); idx >= 0 {
					newSection += "/" + filepath.ToSlash(rel)[:idx]
				}
				updated := updateFrontmatterField(string(content), "section", newSection)
				files = append(files, renameFileSnapshot{
					oldPath: path, relPath: rel, oldContent: content, updatedContent: []byte(updated),
					oldNotebook: safeNotebook, newNotebook: safeNotebook,
					oldSection: oldSection, newSection: newSection, page: page,
				})
				return nil
			})
			if runErr != nil {
				return
			}
			a.tracker.RegisterWrite(oldDir)
			a.tracker.RegisterWrite(newDir)
			if err := os.Rename(oldDir, newDir); err != nil {
				runErr = err
				return
			}
			for _, file := range files {
				newPath := filepath.Join(newDir, file.relPath)
				a.tracker.RegisterWrite(newPath)
				if err := a.renameWriteFileAtomic(newPath, file.updatedContent); err != nil {
					runErr = fmt.Errorf("RenameSection: write %s: %w", file.relPath, err)
					if rollbackErr := a.rollbackRename(oldDir, newDir, files, source, linkJournal, configSnapshot, false); rollbackErr != nil {
						runErr = fmt.Errorf("%w (rollback failed: %v)", runErr, rollbackErr)
					}
					return
				}
			}
			for _, file := range files {
				a.rewriteInboundPageLinksWithJournal(safeNotebook, file.oldSection, file.page, safeNotebook, file.newSection, file.page, linkJournal, lockPathSet(lockPaths))
			}
			for _, file := range files {
				a.coordinator.WithDBWrite(func() {
					if err := a.db.ClearFileBlocks(nil, source, safeNotebook, file.oldSection, file.page); err != nil && runErr == nil {
						runErr = fmt.Errorf("RenameSection: clear index %s: %w", file.relPath, err)
					}
					if err := a.db.ForgetFile(file.oldPath); err != nil && runErr == nil {
						runErr = fmt.Errorf("RenameSection: forget index %s: %w", file.relPath, err)
					}
				})
				if runErr != nil {
					break
				}
				if err := a.reindexFileStrict(filepath.Join(newDir, file.relPath), source, safeNotebook, file.newSection, file.page, true); err != nil {
					runErr = fmt.Errorf("RenameSection: reindex %s: %w", file.relPath, err)
					break
				}
			}
			if runErr == nil {
				configAttempted = true
				if err := a.renameReconcileSection(safeNotebook, safeOldSection, safeNewSection, false); err != nil {
					runErr = err
				}
			}
			if runErr != nil {
				if rollbackErr := a.rollbackRename(oldDir, newDir, files, source, linkJournal, configSnapshot, configAttempted && a.renameHooks != nil && a.renameHooks.reconcileSection != nil); rollbackErr != nil {
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
		return nil
	}
	return fmt.Errorf("RenameSection: tree lock did not stabilize after concurrent creates")
}

// RenameNotebook renames a notebook folder and updates the notebook: frontmatter
// in every .md file it contains. All affected blocks are re-indexed (#62).
func (a *App) RenameNotebook(oldName, newName string) error {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.db == nil {
		return fmt.Errorf("vault database not loaded")
	}
	safeOldNotebook := sanitizePathSegment(oldName)
	safeNewNotebook := sanitizePathSegment(newName)
	if safeOldNotebook == "" || safeNewNotebook == "" {
		return fmt.Errorf("notebook names are required")
	}
	if safeOldNotebook == safeNewNotebook {
		return nil
	}

	// A linked notebook's name is its external folder basename + registry
	// identity; renaming it is unlink + re-link, not a folder rename on the
	// external source of truth. Refuse here so the vault-only folder rename
	// below never misroutes (#100).
	if src := a.resolveSourceByName(safeOldNotebook); strings.HasPrefix(src, "linked:") {
		return fmt.Errorf("linked notebooks cannot be renamed in place — unlink and re-link the folder under the new name")
	}

	oldDir := filepath.Join(a.vaultPath, safeOldNotebook)
	newDir := filepath.Join(a.vaultPath, safeNewNotebook)
	if !isPathWithinRoot(oldDir, a.vaultPath) || !isCreationPathWithinRoot(newDir, a.vaultPath) {
		return fmt.Errorf("path escapes vault")
	}
	if _, err := os.Stat(newDir); err == nil {
		return fmt.Errorf("a notebook named %q already exists", safeNewNotebook)
	}
	if a.nameCollidesWithLink(safeNewNotebook, "") {
		return fmt.Errorf("a linked notebook named %q already exists; unlink or rename it first", safeNewNotebook)
	}

	a.wg.Add(1)
	defer a.wg.Done()

	lockPaths, err := collectMarkdownFilePaths(oldDir)
	if err != nil {
		return err
	}
	var renameTargets []renameTarget
	for _, p := range lockPaths {
		rel, relErr := filepath.Rel(oldDir, p)
		if relErr != nil {
			continue
		}
		relSlash := filepath.ToSlash(rel)
		parts := strings.Split(relSlash, "/")
		var section, page string
		if len(parts) == 1 {
			page = strings.TrimSuffix(parts[0], ".md")
		} else {
			section = strings.Join(parts[:len(parts)-1], "/")
			page = strings.TrimSuffix(parts[len(parts)-1], ".md")
		}
		renameTargets = append(renameTargets, renameTarget{safeOldNotebook, section, page})
	}
	inbound, err := a.collectInboundSourcePaths(renameTargets)
	if err != nil {
		return fmt.Errorf("collect inbound sources: %w", err)
	}
	lockPaths = append(lockPaths, inbound...)

	var runErr error
	configSnapshot := a.snapshotConfig()
	linkJournal := make(map[string]renameLinkJournalEntry)
	configAttempted := false
	// Retry if a .md appears under oldDir or inbound sources grow after collect (TOCTOU).
	var files []renameFileSnapshot
	for attempt := 0; attempt < 8; attempt++ {
		var needRetry []string
		runErr = nil
		configAttempted = false
		linkJournal = make(map[string]renameLinkJournalEntry)
		files = nil
		// Lock every descendant page path + inbound sources before renaming.
		a.coordinator.LockPathsWrite(lockPaths, func() {
			missing, mErr := missingMarkdownUnder(oldDir, lockPathSet(lockPaths))
			if mErr != nil {
				runErr = mErr
				return
			}
			if len(missing) > 0 {
				needRetry = missing
				return
			}
			lockedTargets := renameTargetsFromMarkdownUnder(oldDir, safeOldNotebook, "", lockPaths)
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
			// 1. Walk all .md files under the old notebook recursively and
			// read their content BEFORE renaming.
			_ = filepath.WalkDir(oldDir, func(path string, d fs.DirEntry, err error) error {
				if err != nil {
					return nil
				}
				if !d.IsDir() && strings.HasSuffix(path, ".md") {
					b, readErr := os.ReadFile(path)
					if readErr != nil {
						runErr = fmt.Errorf("RenameNotebook: read %s: %w", path, readErr)
						return filepath.SkipDir
					}
					rel, _ := filepath.Rel(oldDir, path)
					relSlash := filepath.ToSlash(rel)
					parts := strings.Split(relSlash, "/")
					var section, page string
					if len(parts) == 1 {
						page = strings.TrimSuffix(parts[0], ".md")
					} else {
						section = strings.Join(parts[:len(parts)-1], "/")
						page = strings.TrimSuffix(parts[len(parts)-1], ".md")
					}
					updated := updateFrontmatterField(string(b), "notebook", safeNewNotebook)
					files = append(files, renameFileSnapshot{
						oldPath: path, relPath: rel, oldContent: b, updatedContent: []byte(updated),
						oldNotebook: safeOldNotebook, newNotebook: safeNewNotebook,
						oldSection: section, newSection: section, page: page,
					})
				}
				return nil
			})
			if runErr != nil {
				return
			}

			// 2. Rename the notebook folder FIRST. If this fails, nothing
			// was modified (clean state).
			a.tracker.RegisterWrite(oldDir)
			a.tracker.RegisterWrite(newDir)
			if err := os.Rename(oldDir, newDir); err != nil {
				runErr = err
				return
			}

			// 3. Update notebook: frontmatter in each file at the new path.
			for _, fc := range files {
				newMdPath := filepath.Join(newDir, fc.relPath)
				a.tracker.RegisterWrite(newMdPath)
				if err := a.renameWriteFileAtomic(newMdPath, fc.updatedContent); err != nil {
					runErr = fmt.Errorf("RenameNotebook: write %s: %w", fc.relPath, err)
					if rollbackErr := a.rollbackRename(oldDir, newDir, files, "vault", linkJournal, configSnapshot, false); rollbackErr != nil {
						runErr = fmt.Errorf("%w (rollback failed: %v)", runErr, rollbackErr)
					}
					return
				}
			}

			// 4. Clear old index entries and re-index all files at new paths.
			for _, fc := range files {
				a.coordinator.WithDBWrite(func() {
					if err := a.db.ClearFileBlocks(nil, "vault", safeOldNotebook, fc.oldSection, fc.page); err != nil && runErr == nil {
						runErr = fmt.Errorf("RenameNotebook: clear index %s: %w", fc.relPath, err)
					}
					if err := a.db.ForgetFile(fc.oldPath); err != nil && runErr == nil {
						runErr = fmt.Errorf("RenameNotebook: forget index %s: %w", fc.relPath, err)
					}
				})
				if runErr != nil {
					break
				}
				newMdPath := filepath.Join(newDir, fc.relPath)
				if err := a.reindexFileStrict(newMdPath, "vault", safeNewNotebook, fc.newSection, fc.page, true); err != nil {
					runErr = fmt.Errorf("RenameNotebook: reindex %s: %w", fc.relPath, err)
					break
				}
			}
			if runErr == nil {
				configAttempted = true
				if err := a.renameReconcileNotebook(safeOldNotebook, safeNewNotebook, false); err != nil {
					runErr = err
				}
			}
			if runErr != nil {
				if rollbackErr := a.rollbackRename(oldDir, newDir, files, "vault", linkJournal, configSnapshot, configAttempted && a.renameHooks != nil && a.renameHooks.reconcileNotebook != nil); rollbackErr != nil {
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
		return nil
	}
	return fmt.Errorf("RenameNotebook: tree lock did not stabilize after concurrent creates")
}

// DeletePage moves a single page file to .system/trash/ and clears its index
// entries. The file is recoverable from the trash folder (#62).
func (a *App) DeletePage(notebook, section, page string) error {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.db == nil {
		return fmt.Errorf("vault database not loaded")
	}
	safeNotebook := sanitizePathSegment(notebook)
	safeSection, sectionErr := validateSectionPath(section, true)
	safePage := sanitizePathSegment(page)
	if sectionErr != nil {
		return invalidNavigationPath(sectionErr)
	}
	if safeNotebook == "" || safePage == "" {
		return fmt.Errorf("notebook and page names are required")
	}

	source := a.resolveSourceByName(safeNotebook)
	notebookDir, err := a.resolveNotebookDir(safeNotebook, source)
	if err != nil {
		return err
	}
	filePath := filepath.Join(notebookDir, safeSection, safePage+".md")
	if !isPathWithinRoot(filePath, notebookDir) {
		return fmt.Errorf("path escapes notebook root")
	}
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return fmt.Errorf("page %q not found", safePage)
	}

	a.wg.Add(1)
	defer a.wg.Done()

	linked := strings.HasPrefix(source, "linked:")
	var runErr error
	a.coordinator.LockFileWrite(filePath, func() {
		a.tracker.RegisterWrite(filePath)
		if linked {
			// External folder is the source of truth — delete in place. Silt
			// never copies linked content into the vault trash (#100).
			if err := os.Remove(filePath); err != nil {
				runErr = err
				return
			}
		} else {
			if _, err := a.moveToTrash(filePath); err != nil {
				runErr = err
				return
			}
		}
		var blockIDs []string
		a.coordinator.WithDBWrite(func() {
			blockIDs, _ = a.db.BlockIDsForPage(source, safeNotebook, safeSection, safePage)
			_ = a.db.ClearFileBlocks(nil, source, safeNotebook, safeSection, safePage)
			_ = a.db.ForgetFile(filePath)
		})
		// Release the deleted blocks' per-block mutex entries (#122).
		a.coordinator.ReleaseBlockMutexes(blockIDs)
	})

	if runErr != nil {
		return runErr
	}
	return a.reconcileNavigationPage(safeNotebook, safeSection, safePage, safePage, true)
}

// DeleteSection moves a section folder (all pages) to .system/trash/ and clears
// their index entries (#62).
func (a *App) DeleteSection(notebook, section string) error {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.db == nil {
		return fmt.Errorf("vault database not loaded")
	}
	safeNotebook := sanitizePathSegment(notebook)
	safeSection, sectionErr := validateSectionPath(section, false)
	if sectionErr != nil {
		return invalidNavigationPath(sectionErr)
	}
	if safeNotebook == "" {
		return fmt.Errorf("notebook and section names are required")
	}

	source := a.resolveSourceByName(safeNotebook)
	notebookDir, err := a.resolveNotebookDir(safeNotebook, source)
	if err != nil {
		return err
	}
	secPath := filepath.Join(notebookDir, safeSection)
	if !isPathWithinRoot(secPath, notebookDir) {
		return fmt.Errorf("path escapes notebook root")
	}
	if _, err := os.Stat(secPath); os.IsNotExist(err) {
		return fmt.Errorf("section %q not found", safeSection)
	}

	a.wg.Add(1)
	defer a.wg.Done()

	linked := strings.HasPrefix(source, "linked:")
	lockPaths, err := collectMarkdownFilePaths(secPath)
	if err != nil {
		return err
	}
	var runErr error
	type deletedPage struct{ path, section, page string }
	var pages []deletedPage
	// Retry if a .md appears under secPath after the pre-lock walk (TOCTOU).
	for attempt := 0; attempt < 8; attempt++ {
		var needRetry []string
		pages = nil
		runErr = nil
		// Lock every page under the section before trashing the tree (#691).
		a.coordinator.LockPathsWrite(lockPaths, func() {
			missing, mErr := missingMarkdownUnder(secPath, lockPathSet(lockPaths))
			if mErr != nil {
				runErr = mErr
				return
			}
			if len(missing) > 0 {
				needRetry = missing
				return
			}
			_ = filepath.WalkDir(secPath, func(path string, entry fs.DirEntry, walkErr error) error {
				if walkErr != nil {
					runErr = walkErr
					return walkErr
				}
				if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".md") {
					return nil
				}
				rel, relErr := filepath.Rel(notebookDir, path)
				if relErr != nil {
					runErr = relErr
					return relErr
				}
				parts := strings.Split(filepath.ToSlash(rel), "/")
				pages = append(pages, deletedPage{path: path, section: strings.Join(parts[:len(parts)-1], "/"), page: strings.TrimSuffix(parts[len(parts)-1], filepath.Ext(parts[len(parts)-1]))})
				return nil
			})
			if runErr != nil {
				return
			}

			a.tracker.RegisterWrite(secPath)
			if linked {
				// External folder is the source of truth — remove in place (#100).
				if err := os.RemoveAll(secPath); err != nil {
					runErr = err
					return
				}
			} else {
				if _, err := a.moveToTrash(secPath); err != nil {
					runErr = err
					return
				}
			}

			a.coordinator.WithDBWrite(func() {
				for _, pg := range pages {
					_ = a.db.ClearFileBlocks(nil, source, safeNotebook, pg.section, pg.page)
					_ = a.db.ForgetFile(pg.path)
				}
			})
		})
		if len(needRetry) > 0 {
			lockPaths = append(lockPaths, needRetry...)
			continue
		}
		if runErr != nil {
			return runErr
		}
		return a.reconcileNavigationSection(safeNotebook, safeSection, "", true)
	}
	return fmt.Errorf("DeleteSection: tree lock did not stabilize after concurrent creates")
}

// DeleteNotebook moves a notebook folder (all sections + pages) to
// .system/trash/ and clears their index entries (#62).
func (a *App) DeleteNotebook(notebook string) error {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.db == nil {
		return fmt.Errorf("vault database not loaded")
	}
	safeNotebook := sanitizePathSegment(notebook)
	if safeNotebook == "" {
		return fmt.Errorf("notebook name is required")
	}

	nbPath := filepath.Join(a.vaultPath, safeNotebook)
	if !isPathWithinRoot(nbPath, a.vaultPath) {
		return fmt.Errorf("path escapes vault")
	}
	if _, err := os.Stat(nbPath); os.IsNotExist(err) {
		return fmt.Errorf("notebook %q not found", safeNotebook)
	}

	a.wg.Add(1)
	defer a.wg.Done()

	lockPaths, err := collectMarkdownFilePaths(nbPath)
	if err != nil {
		return err
	}
	var runErr error
	type pageInfo struct {
		path    string
		section string
		page    string
	}
	// Retry if a .md appears under nbPath after the pre-lock walk (TOCTOU).
	for attempt := 0; attempt < 8; attempt++ {
		var needRetry []string
		runErr = nil
		// Lock every page under the notebook before trashing the tree (#691).
		a.coordinator.LockPathsWrite(lockPaths, func() {
			missing, mErr := missingMarkdownUnder(nbPath, lockPathSet(lockPaths))
			if mErr != nil {
				runErr = mErr
				return
			}
			if len(missing) > 0 {
				needRetry = missing
				return
			}
			// Walk the subtree BEFORE trashing to collect file paths and their
			// (section, page) for per-page index cleanup via the typed API.
			var pages []pageInfo
			_ = filepath.WalkDir(nbPath, func(path string, d fs.DirEntry, err error) error {
				if err != nil {
					return nil
				}
				if !d.IsDir() && strings.HasSuffix(path, ".md") {
					rel, _ := filepath.Rel(nbPath, path)
					relParts := strings.Split(filepath.ToSlash(rel), "/")
					var section, page string
					if len(relParts) == 1 {
						page = strings.TrimSuffix(relParts[0], ".md")
					} else {
						section = strings.Join(relParts[:len(relParts)-1], "/")
						page = strings.TrimSuffix(relParts[len(relParts)-1], ".md")
					}
					pages = append(pages, pageInfo{path: path, section: section, page: page})
				}
				return nil
			})

			a.tracker.RegisterWrite(nbPath)
			if _, err := a.moveToTrash(nbPath); err != nil {
				runErr = err
				return
			}
			// Clear blocks + files-cache entries per page via the typed API.
			for _, pg := range pages {
				a.coordinator.WithDBWrite(func() {
					_ = a.db.ClearFileBlocks(nil, "vault", safeNotebook, pg.section, pg.page)
					_ = a.db.ForgetFile(pg.path)
				})
			}
		})
		if len(needRetry) > 0 {
			lockPaths = append(lockPaths, needRetry...)
			continue
		}
		if runErr != nil {
			return runErr
		}
		return a.reconcileNavigationNotebook(safeNotebook, "", true)
	}
	return fmt.Errorf("DeleteNotebook: tree lock did not stabilize after concurrent creates")
}
