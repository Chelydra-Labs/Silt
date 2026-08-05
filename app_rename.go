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
	indexFile         func(*App, string, string, string, string, []parser.ParsedBlock, parser.FileMetadata, ...string) error
	reconcileSection  func(*App, string, string, string, bool) error
	reconcileNotebook func(*App, string, string, bool) error
	// afterPreLockInbound runs after the initial inbound collect and before
	// LockPathsWrite (tests only) so TOCTOU inject can add a late wiki-link.
	afterPreLockInbound func()
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

func (a *App) renameIndexFile(source, notebook, section, page string, blocks []parser.ParsedBlock, meta parser.FileMetadata, warnings ...string) error {
	if a.renameHooks != nil && a.renameHooks.indexFile != nil {
		return a.renameHooks.indexFile(a, source, notebook, section, page, blocks, meta, warnings...)
	}
	return a.indexFile(source, notebook, section, page, blocks, meta, warnings...)
}

// indexFile is the canonical atomic block+projection publish for single-file
// write paths (save / create / duplicate / rename / source-mode edit). It
// computes the page's projection payload from meta against the live schema,
// then opens one DB transaction that replaces blocks AND projection together
// via IndexFileWithProjection. A reader can never observe a freshly-saved
// typed page without its projection (or vice versa).
//
// renameHooks.indexFile (when installed) intercepts this call for the rename
// rollback tests; the hook sees the same atomic shape.
func (a *App) indexFile(source, notebook, section, page string, blocks []parser.ParsedBlock, meta parser.FileMetadata, warnings ...string) error {
	typeID, props := a.computePageProjection(meta)
	core := a.computePageCore(meta)
	// Capture the file's mtime/size BEFORE the index write so the files-table
	// row records the mtime of the content being indexed — not whatever mtime a
	// concurrent external edit (Obsidian/Dropbox/second Silt window) leaves on
	// disk between the index commit and a post-commit stat. With a pre-commit
	// snapshot, an external edit landing in the index window leaves the recorded
	// mtime stale relative to the file's real (post-edit) mtime, so the warm-
	// restart IsFileUnchanged check treats the file as "changed" and re-parses
	// it — instead of silently skipping and persisting the stale content. The
	// window is not eliminated entirely (an edit between the caller's write/parse
	// and this stat can still mismatch), but the dangerous [index-commit, stat]
	// window is gone. Best-effort: a stat/mark failure only leaves `modified`
	// stale until the next external-triggered scan — it is not a write failure.
	var filePath string
	var fileMtime, fileSize int64
	hasStat := false
	if dir, derr := a.resolveNotebookDir(notebook, source); derr == nil {
		filePath = filepath.Join(dir, section, page+".md")
		if stat, se := os.Stat(filePath); se == nil {
			fileMtime = stat.ModTime().UnixNano()
			fileSize = stat.Size()
			hasStat = true
		}
	}
	var err error
	a.coordinator.WithDBWrite(func() {
		err = a.db.IndexFileWithProjection(source, notebook, section, page, blocks, meta.Tags, typeID, props, core, warnings...)
	})
	if err != nil {
		log.Printf("indexFile: IndexFileWithProjection failed for %s/%s/%s: %v", notebook, section, page, err)
		a.emit(EventTypesProjectionError, map[string]string{"source": source, "page": page})
		return err
	}
	// Record the pre-index mtime snapshot so the Core panel's `modified` stays
	// current after an in-app write (the fsnotify watcher ignores self-writes).
	// Every write path funnels through indexFile, so this single call covers
	// them all.
	if hasStat {
		a.coordinator.WithDBWrite(func() {
			if me := a.db.MarkFileIndexed(nil, filePath, fileMtime, fileSize); me != nil {
				log.Printf("indexFile: MarkFileIndexed(%s) failed (modified may be stale): %v", filePath, me)
			}
		})
	}
	return nil
}

// markFileIndexedBestEffort records the caller-captured pre-index mtime/size
// snapshot on the files row after a block-only write (task status/owner/
// priority, dependency, recurrence, subtree edits) that calls IndexFileBlocks
// directly, bypassing indexFile. Without it the Core panel's read-only
// `modified` stays at the pre-write value all session — the fsnotify watcher
// ignores self-writes, so nothing else refreshes the row.
//
// The caller MUST stat the file immediately after WriteFileAtomic (BEFORE
// IndexFileBlocks commits) and hand that os.FileInfo in — mirroring indexFile
// and watcher.reindexFile's pre-index snapshot. A stat taken HERE (after the
// index commit) would reintroduce the [index-commit, stat] window the
// dcd2a6cd fix closed for indexFile: an external edit (Obsidian/Dropbox/second
// Silt window) landing between the app's write and this mark would get its
// mtime recorded against the pre-edit indexed content, and a warm restart's
// IsFileUnchanged would match and silently persist the stale content. With the
// caller's snapshot, such an edit leaves the recorded mtime stale relative to
// the file's real mtime, so the warm restart re-parses. Best-effort: a nil stat
// (the caller's stat failed) or a mark failure only leaves `modified` stale
// until the next external-triggered scan; it is never a write failure.
func (a *App) markFileIndexedBestEffort(filePath string, stat os.FileInfo) {
	if stat == nil {
		return
	}
	a.coordinator.WithDBWrite(func() {
		if me := a.db.MarkFileIndexed(nil, filePath, stat.ModTime().UnixNano(), stat.Size()); me != nil {
			log.Printf("markFileIndexedBestEffort(%s): %v", filePath, me)
		}
	})
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
	// Capture the mtime/size BEFORE the index write so the files-table row
	// records the mtime of the content being indexed — not a mtime a concurrent
	// external edit leaves between the index commit and a post-commit stat
	// (same rationale as indexFile's internal pre-index snapshot). indexFile
	// captures its own snapshot too; this mark is the source of truth for the
	// renameIndexFile path and a redundant idempotent write for indexFile.
	stat, statErr := os.Stat(filePath)
	if statErr != nil {
		return fmt.Errorf("stat indexed file %s: %w", filePath, statErr)
	}
	if err := index(source, meta.Notebook, meta.Section, meta.Page, blocks, meta, meta.Warnings...); err != nil {
		return fmt.Errorf("index %s: %w", filePath, err)
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
	// Projection is published atomically with the block write inside indexFile
	// (IndexFileWithProjection), so no separate projectPageType call remains.
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
	// Atomic block+projection publish (see indexFile). The rename
	// reconciliation paths re-parse the whole file when frontmatter shifted,
	// so blocks and projection must move together.
	reidxSource := a.resolveSourceByName(notebook)
	if err := a.indexFile(reidxSource, meta.Notebook, meta.Section, meta.Page, blocks, meta, meta.Warnings...); err != nil {
		log.Printf("reindexFile: index failed for %s/%s/%s: %v", meta.Notebook, meta.Section, meta.Page, err)
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
	return a.renameSingleFile(func() (renameSingleFileStrategy, error) {
		safeNotebook := sanitizePathSegment(notebook)
		safeSection, sectionErr := validateSectionPath(section, true)
		safeOldPage := sanitizePathSegment(oldName)
		safeNewPage := sanitizePathSegment(newName)
		if sectionErr != nil {
			return renameSingleFileStrategy{}, invalidNavigationPath(sectionErr)
		}
		if safeNotebook == "" || safeOldPage == "" || safeNewPage == "" {
			return renameSingleFileStrategy{}, fmt.Errorf("notebook and page names are required")
		}
		if safeOldPage == safeNewPage {
			return renameSingleFileStrategy{}, errRenameNoOp
		}

		source := a.resolveSourceByName(safeNotebook)
		notebookDir, err := a.resolveNotebookDir(safeNotebook, source)
		if err != nil {
			return renameSingleFileStrategy{}, err
		}
		oldFile := filepath.Join(notebookDir, safeSection, safeOldPage+".md")
		newFile := filepath.Join(notebookDir, safeSection, safeNewPage+".md")
		if !isPathWithinRoot(oldFile, notebookDir) || !isCreationPathWithinRoot(newFile, notebookDir) {
			return renameSingleFileStrategy{}, fmt.Errorf("path escapes notebook root")
		}
		if _, err := os.Stat(newFile); err == nil {
			return renameSingleFileStrategy{}, fmt.Errorf("a page named %q already exists", safeNewPage)
		}

		return renameSingleFileStrategy{
			oldFile: oldFile,
			newFile: newFile,
			targets: []renameTarget{{safeNotebook, safeSection, safeOldPage}},
			label:   "RenamePage",
			renameStep: func() error {
				// Read before renaming so a read failure leaves clean state.
				contentBytes, err := os.ReadFile(oldFile)
				if err != nil {
					return err
				}
				// Rename old → new FIRST: if this fails nothing was modified,
				// avoiding stale-frontmatter-at-old-path inconsistency.
				a.tracker.RegisterWrite(oldFile)
				a.tracker.RegisterWrite(newFile)
				if err := os.Rename(oldFile, newFile); err != nil {
					return err
				}
				content := updateFrontmatterField(string(contentBytes), "page", safeNewPage)
				a.tracker.RegisterWrite(newFile)
				return parser.WriteFileAtomic(newFile, []byte(content))
			},
			rewriteInbound: func(heldPaths map[string]bool) {
				a.rewriteInboundPageLinksWithJournal(source, safeNotebook, safeSection, safeOldPage, safeNotebook, safeSection, safeNewPage, nil, heldPaths)
			},
			clearAndReindex: func() {
				a.coordinator.WithDBWrite(func() {
					_ = a.db.ClearFileBlocks(nil, source, safeNotebook, safeSection, safeOldPage)
				})
				a.coordinator.WithDBWrite(func() {
					_ = a.db.ForgetFile(oldFile)
				})
				a.reindexFile(newFile, safeNotebook, safeSection, safeNewPage)
			},
			staleSweep: func() {
				a.rewriteStaleInboundAfterRename(safeNotebook, safeSection, safeOldPage, safeNotebook, safeSection, safeNewPage)
			},
			reconcile: func() error {
				return a.reconcileNavigationPage(safeNotebook, safeSection, safeOldPage, safeNewPage, false)
			},
		}, nil
	})
}

// MovePage moves a page from one section to another (or to the notebook root
// when toSection == "") within the same notebook (#177). The .md file is
// renamed on disk, its `section:` frontmatter is rewritten, the block index
// is rebuilt at the new path, and nav_order is adjusted for both the source
// and target sectionKeys. Returns an error on name collision. Cross-notebook
// moves are out of scope — the page stays within `notebook`. Block UUIDs are
// preserved so references and embeds keep resolving.
func (a *App) MovePage(notebook, fromSection, toSection, page string) error {
	return a.renameSingleFile(func() (renameSingleFileStrategy, error) {
		safeNotebook := sanitizePathSegment(notebook)
		safeFrom, fromErr := validateSectionPath(fromSection, true)
		safeTo, toErr := validateSectionPath(toSection, true)
		safePage := sanitizePathSegment(page)
		if fromErr != nil {
			return renameSingleFileStrategy{}, invalidNavigationPath(fromErr)
		}
		if toErr != nil {
			return renameSingleFileStrategy{}, invalidNavigationPath(toErr)
		}
		if safeNotebook == "" || safePage == "" {
			return renameSingleFileStrategy{}, fmt.Errorf("notebook and page names are required")
		}
		if safeFrom == safeTo {
			return renameSingleFileStrategy{}, errRenameNoOp
		}

		source := a.resolveSourceByName(safeNotebook)
		notebookDir, err := a.resolveNotebookDir(safeNotebook, source)
		if err != nil {
			return renameSingleFileStrategy{}, err
		}
		oldFile := filepath.Join(notebookDir, safeFrom, safePage+".md")
		newFile := filepath.Join(notebookDir, safeTo, safePage+".md")
		if !isPathWithinRoot(oldFile, notebookDir) || !isCreationPathWithinRoot(newFile, notebookDir) {
			return renameSingleFileStrategy{}, fmt.Errorf("path escapes notebook root")
		}
		if _, err := os.Stat(oldFile); os.IsNotExist(err) {
			return renameSingleFileStrategy{}, fmt.Errorf("page %q not found in section %q", safePage, safeFrom)
		}
		if _, err := os.Stat(newFile); err == nil {
			return renameSingleFileStrategy{}, fmt.Errorf("a page named %q already exists in that section", safePage)
		}

		return renameSingleFileStrategy{
			oldFile: oldFile,
			newFile: newFile,
			targets: []renameTarget{{safeNotebook, safeFrom, safePage}},
			label:   "MovePage",
			renameStep: func() error {
				targetDir := filepath.Dir(newFile)
				if err := os.MkdirAll(targetDir, 0o755); err != nil {
					return fmt.Errorf("failed to create target section directory: %w", err)
				}
				contentBytes, err := os.ReadFile(oldFile)
				if err != nil {
					return err
				}
				a.tracker.RegisterWrite(oldFile)
				a.tracker.RegisterWrite(newFile)
				if err := os.Rename(oldFile, newFile); err != nil {
					return err
				}
				// section: frontmatter rewrite: log on failure (file already
				// moved) and fall through so the old index entries are cleaned.
				content := updateFrontmatterField(string(contentBytes), "section", safeTo)
				a.tracker.RegisterWrite(newFile)
				if err := parser.WriteFileAtomic(newFile, []byte(content)); err != nil {
					log.Printf("MovePage: WriteFileAtomic failed at %s (file already moved): %v", newFile, err)
				}
				return nil
			},
			rewriteInbound: func(heldPaths map[string]bool) {
				a.rewriteInboundPageLinksWithJournal(source, safeNotebook, safeFrom, safePage, safeNotebook, safeTo, safePage, nil, heldPaths)
			},
			clearAndReindex: func() {
				a.coordinator.WithDBWrite(func() {
					_ = a.db.ClearFileBlocks(nil, source, safeNotebook, safeFrom, safePage)
				})
				a.coordinator.WithDBWrite(func() {
					_ = a.db.ForgetFile(oldFile)
				})
				a.reindexFile(newFile, safeNotebook, safeTo, safePage)
			},
			staleSweep: func() {
				a.rewriteStaleInboundAfterRename(safeNotebook, safeFrom, safePage, safeNotebook, safeTo, safePage)
			},
			reconcile: func() error {
				if err := a.reconcileNavigationMove(safeNotebook, safeFrom, safeTo, safePage); err != nil {
					log.Printf("MovePage: nav_order persist failed (file move succeeded): %v", err)
				}
				return nil
			},
		}, nil
	})
}

// RenameSection renames a section folder and updates the section: frontmatter
// in every .md file it contains. All affected blocks are re-indexed (#62).
func (a *App) RenameSection(notebook, oldName, newName string) error {
	return a.renameTree(func() (renameTreeStrategy, error) {
		safeNotebook := sanitizePathSegment(notebook)
		safeOldSection, oldErr := validateSectionPath(oldName, false)
		safeNewSection, newErr := validateSectionPath(newName, false)
		if oldErr != nil {
			return renameTreeStrategy{}, invalidNavigationPath(oldErr)
		}
		if newErr != nil {
			return renameTreeStrategy{}, invalidNavigationPath(newErr)
		}
		if safeNotebook == "" {
			return renameTreeStrategy{}, fmt.Errorf("notebook and section names are required")
		}
		if safeOldSection == safeNewSection {
			return renameTreeStrategy{}, errRenameNoOp
		}

		source := a.resolveSourceByName(safeNotebook)
		notebookDir, err := a.resolveNotebookDir(safeNotebook, source)
		if err != nil {
			return renameTreeStrategy{}, err
		}
		oldDir := filepath.Join(notebookDir, safeOldSection)
		newDir := filepath.Join(notebookDir, safeNewSection)
		if !isPathWithinRoot(oldDir, notebookDir) || !isCreationPathWithinRoot(newDir, notebookDir) {
			return renameTreeStrategy{}, fmt.Errorf("path escapes notebook root")
		}
		if _, err := os.Stat(newDir); err == nil {
			return renameTreeStrategy{}, fmt.Errorf("a section named %q already exists", safeNewSection)
		}

		return renameTreeStrategy{
			oldDir:        oldDir,
			newDir:        newDir,
			notebook:      safeNotebook,
			sectionPrefix: safeOldSection,
			label:         "RenameSection",
			buildRenameTargets: func(seedPaths []string) []renameTarget {
				var targets []renameTarget
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
					targets = append(targets, renameTarget{safeNotebook, oldSection, page})
				}
				return targets
			},
			walkAndSnapshot: func() ([]renameFileSnapshot, error) {
				var snapped []renameFileSnapshot
				var walkErr error
				_ = filepath.WalkDir(oldDir, func(path string, entry fs.DirEntry, err error) error {
					if err != nil {
						walkErr = err
						return err
					}
					if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".md") {
						return nil
					}
					content, readErr := os.ReadFile(path)
					if readErr != nil {
						walkErr = readErr
						return readErr
					}
					rel, relErr := filepath.Rel(oldDir, path)
					if relErr != nil {
						walkErr = relErr
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
					if idx := strings.LastIndex(relSlash, "/"); idx >= 0 {
						newSection += "/" + relSlash[:idx]
					}
					updated := updateFrontmatterField(string(content), "section", newSection)
					snapped = append(snapped, renameFileSnapshot{
						oldPath: path, relPath: rel, oldContent: content, updatedContent: []byte(updated),
						oldNotebook: safeNotebook, newNotebook: safeNotebook,
						oldSection: oldSection, newSection: newSection, page: page,
					})
					return nil
				})
				return snapped, walkErr
			},
			rewriteInboundFile: func(file renameFileSnapshot, heldPaths map[string]bool, linkJournal map[string]renameLinkJournalEntry) {
				a.rewriteInboundPageLinksWithJournal(source, safeNotebook, file.oldSection, file.page, safeNotebook, file.newSection, file.page, linkJournal, heldPaths)
			},
			clearAndReindexFile: func(file renameFileSnapshot) error {
				var clearErr error
				a.coordinator.WithDBWrite(func() {
					if err := a.db.ClearFileBlocks(nil, source, safeNotebook, file.oldSection, file.page); err != nil && clearErr == nil {
						clearErr = fmt.Errorf("RenameSection: clear index %s: %w", file.relPath, err)
					}
					if err := a.db.ForgetFile(file.oldPath); err != nil && clearErr == nil {
						clearErr = fmt.Errorf("RenameSection: forget index %s: %w", file.relPath, err)
					}
				})
				if clearErr != nil {
					return clearErr
				}
				if err := a.reindexFileStrict(filepath.Join(newDir, file.relPath), source, safeNotebook, file.newSection, file.page, true); err != nil {
					return fmt.Errorf("RenameSection: reindex %s: %w", file.relPath, err)
				}
				return nil
			},
			reconcile: func() error {
				return a.renameReconcileSection(safeNotebook, safeOldSection, safeNewSection, false)
			},
			staleSweepFile: func(file renameFileSnapshot) {
				a.rewriteStaleInboundAfterRename(safeNotebook, file.oldSection, file.page, safeNotebook, file.newSection, file.page)
			},
			rollbackSource: source,
			rollbackReconcileHookEnabled: func(configAttempted bool) bool {
				return configAttempted && a.renameHooks != nil && a.renameHooks.reconcileSection != nil
			},
		}, nil
	})
}

// RenameNotebook renames a notebook folder and updates the notebook: frontmatter
// in every .md file it contains. All affected blocks are re-indexed (#62).
func (a *App) RenameNotebook(oldName, newName string) error {
	return a.renameTree(func() (renameTreeStrategy, error) {
		safeOldNotebook := sanitizePathSegment(oldName)
		safeNewNotebook := sanitizePathSegment(newName)
		if safeOldNotebook == "" || safeNewNotebook == "" {
			return renameTreeStrategy{}, fmt.Errorf("notebook names are required")
		}
		if safeOldNotebook == safeNewNotebook {
			return renameTreeStrategy{}, errRenameNoOp
		}

		// A linked notebook's name is its external folder basename + registry
		// identity; renaming it is unlink + re-link, not a folder rename on the
		// external source of truth. Refuse here so the vault-only folder rename
		// below never misroutes (#100).
		if src := a.resolveSourceByName(safeOldNotebook); strings.HasPrefix(src, "linked:") {
			return renameTreeStrategy{}, fmt.Errorf("linked notebooks cannot be renamed in place — unlink and re-link the folder under the new name")
		}

		oldDir := filepath.Join(a.vaultPath, safeOldNotebook)
		newDir := filepath.Join(a.vaultPath, safeNewNotebook)
		if !isPathWithinRoot(oldDir, a.vaultPath) || !isCreationPathWithinRoot(newDir, a.vaultPath) {
			return renameTreeStrategy{}, fmt.Errorf("path escapes vault")
		}
		if _, err := os.Stat(newDir); err == nil {
			return renameTreeStrategy{}, fmt.Errorf("a notebook named %q already exists", safeNewNotebook)
		}
		if a.nameCollidesWithLink(safeNewNotebook, "") {
			return renameTreeStrategy{}, fmt.Errorf("a linked notebook named %q already exists; unlink or rename it first", safeNewNotebook)
		}

		return renameTreeStrategy{
			oldDir:        oldDir,
			newDir:        newDir,
			notebook:      safeOldNotebook,
			sectionPrefix: "",
			label:         "RenameNotebook",
			buildRenameTargets: func(seedPaths []string) []renameTarget {
				var targets []renameTarget
				for _, p := range seedPaths {
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
					targets = append(targets, renameTarget{safeOldNotebook, section, page})
				}
				return targets
			},
			walkAndSnapshot: func() ([]renameFileSnapshot, error) {
				var snapped []renameFileSnapshot
				var walkErr error
				_ = filepath.WalkDir(oldDir, func(path string, d fs.DirEntry, err error) error {
					if err != nil {
						return nil
					}
					if !d.IsDir() && strings.HasSuffix(path, ".md") {
						b, readErr := os.ReadFile(path)
						if readErr != nil {
							walkErr = fmt.Errorf("RenameNotebook: read %s: %w", path, readErr)
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
						snapped = append(snapped, renameFileSnapshot{
							oldPath: path, relPath: rel, oldContent: b, updatedContent: []byte(updated),
							oldNotebook: safeOldNotebook, newNotebook: safeNewNotebook,
							oldSection: section, newSection: section, page: page,
						})
					}
					return nil
				})
				return snapped, walkErr
			},
			rewriteInboundFile: func(file renameFileSnapshot, heldPaths map[string]bool, linkJournal map[string]renameLinkJournalEntry) {
				a.rewriteInboundPageLinksWithJournal("vault", safeOldNotebook, file.oldSection, file.page, safeNewNotebook, file.newSection, file.page, linkJournal, heldPaths)
			},
			clearAndReindexFile: func(file renameFileSnapshot) error {
				var clearErr error
				a.coordinator.WithDBWrite(func() {
					if err := a.db.ClearFileBlocks(nil, "vault", safeOldNotebook, file.oldSection, file.page); err != nil && clearErr == nil {
						clearErr = fmt.Errorf("RenameNotebook: clear index %s: %w", file.relPath, err)
					}
					if err := a.db.ForgetFile(file.oldPath); err != nil && clearErr == nil {
						clearErr = fmt.Errorf("RenameNotebook: forget index %s: %w", file.relPath, err)
					}
				})
				if clearErr != nil {
					return clearErr
				}
				newMdPath := filepath.Join(newDir, file.relPath)
				if err := a.reindexFileStrict(newMdPath, "vault", safeNewNotebook, file.newSection, file.page, true); err != nil {
					return fmt.Errorf("RenameNotebook: reindex %s: %w", file.relPath, err)
				}
				return nil
			},
			reconcile: func() error {
				return a.renameReconcileNotebook(safeOldNotebook, safeNewNotebook, false)
			},
			staleSweepFile: func(file renameFileSnapshot) {
				a.rewriteStaleInboundAfterRename(safeOldNotebook, file.oldSection, file.page, safeNewNotebook, file.newSection, file.page)
			},
			rollbackSource: "vault",
			rollbackReconcileHookEnabled: func(configAttempted bool) bool {
				return configAttempted && a.renameHooks != nil && a.renameHooks.reconcileNotebook != nil
			},
		}, nil
	})
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
	// Notify plugins (AI vector index) so orphan embeddings are dropped (#850).
	a.emitBlockChanged("", safeNotebook, safeSection, safePage, "")
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
		// Notify plugins per deleted page so vector rows are dropped (#850).
		for _, pg := range pages {
			a.emitBlockChanged("", safeNotebook, pg.section, pg.page, "")
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
		var pages []pageInfo
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
			pages = nil
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
		// Notify plugins per deleted page so vector rows are dropped (#850).
		for _, pg := range pages {
			a.emitBlockChanged("", safeNotebook, pg.section, pg.page, "")
		}
		return a.reconcileNavigationNotebook(safeNotebook, "", true)
	}
	return fmt.Errorf("DeleteNotebook: tree lock did not stabilize after concurrent creates")
}
