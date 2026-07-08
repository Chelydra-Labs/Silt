package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"silt/backend/db"
	"silt/backend/parser"
	"silt/backend/plugins"

	"github.com/google/uuid"
)

// FetchSubtree returns a task block's child sub-tree — the contiguous run of
// indented blocks beneath the parent task (#305). The sub-tree is every block
// whose Depth is strictly greater than the parent's, up to (but not including)
// the next block at or above the parent's depth, in file/slice order. An empty
// slice means the task has no children (the modal opens with an empty editor so
// the user can add sub-notes inline). Read-only: no locks beyond vaultMu.
func (a *App) FetchSubtree(blockID string) ([]parser.ParsedBlock, error) {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.db == nil {
		return nil, fmt.Errorf("vault database not loaded")
	}
	a.wg.Add(1)
	defer a.wg.Done()

	var loc db.BlockLocation
	err := a.coordinator.WithDBReadResult(func() error {
		var e error
		loc, e = a.db.GetBlockLocation(blockID)
		return e
	})
	if err != nil {
		return nil, fmt.Errorf("block %s not found in SQLite: %w", blockID, err)
	}

	safeNotebook := sanitizePathSegment(loc.Notebook)
	safeSection := sanitizePathSegment(loc.Section)
	safePage := sanitizePathSegment(loc.Page)
	if safeNotebook == "" || safePage == "" {
		return nil, fmt.Errorf("invalid file metadata for block %s", blockID)
	}
	notebookDir, err := a.resolveNotebookDir(safeNotebook, loc.Source)
	if err != nil {
		return nil, fmt.Errorf("resolve notebook dir for block %s: %w", blockID, err)
	}
	filePath := filepath.Join(notebookDir, safeSection, safePage+".md")
	if !isPathWithinRoot(filePath, notebookDir) {
		return nil, fmt.Errorf("resolved file path %q escapes notebook root %q", filePath, notebookDir)
	}

	contentBytes, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("read file for block %s: %w", blockID, err)
	}
	fileDate := fileOrDefaultDate(filePath)
	parsedBlocks, _, _, _, parseErr := parser.ParseFileContent(string(contentBytes), safeNotebook, safeSection, safePage, fileDate, a.spacesPerTab)
	if parseErr != nil {
		return nil, fmt.Errorf("parse file for subtree fetch: %w", parseErr)
	}

	return extractSubtree(parsedBlocks, blockID), nil
}

// extractSubtree walks the parsed-block slice in order and collects the child
// sub-tree of the parent block: the parent itself is NOT included; every
// following block at Depth > parent.Depth is, until the first block at or
// below the parent's depth ends the run. This mirrors the parser's activeIDs
// stack-clearing rule (parser.go:841-857): once indentation returns to the
// parent's level (or above), the sub-tree is closed.
func extractSubtree(blocks []parser.ParsedBlock, parentID string) []parser.ParsedBlock {
	parentIdx := -1
	parentDepth := 0
	for i, b := range blocks {
		if b.ID == parentID {
			parentIdx = i
			parentDepth = b.Depth
			break
		}
	}
	// Return an empty (non-nil) slice when there are no children so the
	// binding serializes to JSON `[]`, not `null`. The SDK contract is
	// []ParsedBlock; a nil here reaches the frontend as null and crashes
	// blocksToDoc(null).map in the sub-editor.
	subtree := []parser.ParsedBlock{}
	if parentIdx < 0 {
		return subtree
	}
	for i := parentIdx + 1; i < len(blocks); i++ {
		if blocks[i].Depth <= parentDepth {
			break
		}
		subtree = append(subtree, blocks[i])
	}
	return subtree
}

// SaveSubtreeBlocks is the app-level entry point for the sub-editor splice
// (#305). First-party callers (the editor host) use this directly; plugins go
// through the PluginSaveSubtreeBlocks wrapper, which gates on
// CapContentMutate (SPECS §8.3 — plugins never call App methods directly).
func (a *App) SaveSubtreeBlocks(blockID string, children []parser.ParsedBlock) (bool, error) {
	return a.saveSubtreeBlocks(blockID, children)
}

// PluginSaveSubtreeBlocks is the plugin-SDK wrapper for SaveSubtreeBlocks,
// gated by the standard capability + session checks. Mirrors
// PluginSetTaskBlockedBy — a third-party plugin without the CapContentMutate
// grant must not be able to splice arbitrary blocks into a task's sub-tree.
func (a *App) PluginSaveSubtreeBlocks(pluginID, sessionToken, blockID string, children []parser.ParsedBlock) (bool, error) {
	if err := a.validatePluginSession(pluginID, sessionToken); err != nil {
		return false, err
	}
	if err := a.requireGrant(pluginID, plugins.CapContentMutate); err != nil {
		return false, err
	}
	return a.saveSubtreeBlocks(blockID, children)
}

// saveSubtreeBlocks is the shared core for the app-level and plugin-level entry
// points. It re-parses the whole file, replaces the contiguous
// child range (depth > parent depth) with the incoming children, then runs the
// canonical write chain: RenderFileContent → WriteFileAtomic → re-parse →
// IndexFileBlocks → emit block:changed. The parent task and all surrounding
// content (unmanaged prose, sibling blocks, other tasks) are preserved verbatim
// because RenderFileContent re-serializes the full slice. Holds both
// LockBlockWrite(parentID) and LockFileWrite(filePath) so the splice is atomic
// and races no concurrent writer. Returns true when a write occurred.
func (a *App) saveSubtreeBlocks(blockID string, children []parser.ParsedBlock) (bool, error) {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.db == nil {
		return false, fmt.Errorf("vault database not loaded")
	}
	a.wg.Add(1)
	defer a.wg.Done()

	var loc db.BlockLocation
	err := a.coordinator.WithDBReadResult(func() error {
		var e error
		loc, e = a.db.GetBlockLocation(blockID)
		return e
	})
	if err != nil {
		return false, fmt.Errorf("block %s not found in SQLite: %w", blockID, err)
	}
	if loc.BlockType != string(parser.BlockTask) {
		return false, fmt.Errorf("block %s is not a task", blockID)
	}
	safeNotebook := sanitizePathSegment(loc.Notebook)
	safeSection := sanitizePathSegment(loc.Section)
	safePage := sanitizePathSegment(loc.Page)
	if safeNotebook == "" || safePage == "" {
		return false, fmt.Errorf("invalid file metadata for block %s", blockID)
	}
	notebookDir, err := a.resolveNotebookDir(safeNotebook, loc.Source)
	if err != nil {
		return false, fmt.Errorf("resolve notebook dir for block %s: %w", blockID, err)
	}
	filePath := filepath.Join(notebookDir, safeSection, safePage+".md")
	if !isPathWithinRoot(filePath, notebookDir) {
		return false, fmt.Errorf("resolved file path %q escapes notebook root %q", filePath, notebookDir)
	}

	var writeErr error
	var emitFileDate string
	didWrite := false
	a.coordinator.LockBlockWrite(blockID, func() {
		a.coordinator.LockFileWrite(filePath, func() {
			// Focus-lock guard mirrors every other task setter (#444): refuse
			// rather than clobber an in-flight editor edit on the same file.
			if a.watcher != nil && a.watcher.IsFocusLocked(filePath) {
				writeErr = errBlockBeingEdited
				return
			}
			contentBytes, err := os.ReadFile(filePath)
			if err != nil {
				writeErr = err
				return
			}
			fileDate := fileOrDefaultDate(filePath)
			parsedBlocks, meta, _, _, parseErr := parser.ParseFileContent(string(contentBytes), safeNotebook, safeSection, safePage, fileDate, a.spacesPerTab)
			if parseErr != nil {
				writeErr = fmt.Errorf("parse file for subtree save: %w", parseErr)
				return
			}

			merged, ok := spliceSubtree(parsedBlocks, blockID, children)
			if !ok {
				writeErr = fmt.Errorf("parent block %s not found in file %s", blockID, filePath)
				return
			}

			frontmatter, body := parser.SplitFrontmatter(string(contentBytes))
			if frontmatter == "" {
				fmDate := meta.Date
				if fmDate == "" {
					fmDate = fileDate
				}
				frontmatter = fmt.Sprintf("---\nnotebook: %s\nsection: %s\npage: %s\ndate: %s\ntags: []\n---\n", strconv.Quote(safeNotebook), strconv.Quote(safeSection), strconv.Quote(safePage), strconv.Quote(fmDate))
				body = string(contentBytes)
			}
			newContent := parser.RenderFileContent(merged, body, frontmatter, a.spacesPerTab)
			a.tracker.RegisterWrite(filePath)
			if err := parser.WriteFileAtomic(filePath, []byte(newContent)); err != nil {
				writeErr = err
				return
			}
			didWrite = true

			blocks, remeta, _, _, err := parser.ParseFileContent(newContent, meta.Notebook, meta.Section, meta.Page, meta.Date, a.spacesPerTab)
			if err == nil {
				var idxErr error
				a.coordinator.WithDBWrite(func() {
					idxErr = a.db.IndexFileBlocks(loc.Source, remeta.Notebook, remeta.Section, remeta.Page, blocks, remeta.Tags, remeta.Warnings...)
				})
				if idxErr != nil {
					log.Printf("SaveSubtreeBlocks: IndexFileBlocks failed: %v", idxErr)
				}
				for _, b := range blocks {
					if b.ID == blockID {
						emitFileDate = b.FileDate
					}
				}
			} else {
				log.Printf("SaveSubtreeBlocks: re-parse failed (file written, index stale until next scan): %v", err)
			}
			if emitFileDate == "" {
				emitFileDate = fileDate
			}
		})
	}) // LockBlockWrite
	if writeErr != nil {
		return false, writeErr
	}
	if didWrite {
		a.emitBlockChanged(blockID, safeNotebook, safeSection, safePage, emitFileDate)
	}
	return didWrite, nil
}

// AppendTaskComment atomically appends a NOTE comment to a task's child
// sub-tree (#456). It closes the concurrent-post race that the previous
// two-step SDK path (FetchSubtree → PluginSaveSubtreeBlocks) had: the fetch
// was unlocked, so two surfaces posting a comment on the same task at once
// were last-write-wins and one comment was silently dropped. This binding
// performs the read-modify-write under a single LockBlockWrite+LockFileWrite
// hold, so both concurrent posts land.
//
// Returns the freshly-minted block id of the new NOTE comment. `author` and
// `ts` are the NOTE-block comment-attribution tokens ([author:: NAME] /
// [ts:: YYYY-MM-DDTHH:MM:SS]); pass empty to omit either (nullable per
// ARCHITECTURE.md §2.2). The SDK generates `ts` client-side and passes it
// through; the binding is a pure transport so the caller controls attribution.
func (a *App) AppendTaskComment(taskID, text, author, ts string) (string, error) {
	return a.appendTaskComment(taskID, text, author, ts)
}

// PluginAppendTaskComment is the plugin-SDK wrapper for AppendTaskComment,
// gated by the standard capability + session checks so a third-party plugin
// without the CapContentMutate grant can't splice NOTE blocks into a task's
// sub-tree. Mirrors PluginSaveSubtreeBlocks.
func (a *App) PluginAppendTaskComment(pluginID, sessionToken, taskID, text, author, ts string) (string, error) {
	if err := a.validatePluginSession(pluginID, sessionToken); err != nil {
		return "", err
	}
	if err := a.requireGrant(pluginID, plugins.CapContentMutate); err != nil {
		return "", err
	}
	return a.appendTaskComment(taskID, text, author, ts)
}

// appendTaskComment is the shared core for the app-level and plugin-level
// entry points. Read-modify-write under one LockBlockWrite+LockFileWrite hold:
// parse the file, collect the existing child sub-tree, build a new NOTE block
// with a fresh UUID + the author/ts attribution tokens, splice [existing...,
// comment] back through spliceSubtree (the same helper SaveSubtreeBlocks uses),
// then run the canonical write chain. The append is atomic with respect to
// other comment posts and other task setters because the per-file write lock
// serializes the whole read-modify-write.
func (a *App) appendTaskComment(taskID, text, author, ts string) (string, error) {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.db == nil {
		return "", fmt.Errorf("vault database not loaded")
	}
	// Reject empty/whitespace-only text up front: a bare NOTE bullet with no
	// body is never a useful comment, and failing here (before lock
	// acquisition + the read-modify-write) keeps the guard cheap and the
	// intent obvious. Mirrors setTaskTitle's empty-input contract guard.
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("comment text must not be empty")
	}
	a.wg.Add(1)
	defer a.wg.Done()

	var loc db.BlockLocation
	err := a.coordinator.WithDBReadResult(func() error {
		var e error
		loc, e = a.db.GetBlockLocation(taskID)
		return e
	})
	if err != nil {
		return "", fmt.Errorf("block %s not found in SQLite: %w", taskID, err)
	}
	if loc.BlockType != string(parser.BlockTask) {
		return "", fmt.Errorf("block %s is not a task", taskID)
	}
	safeNotebook := sanitizePathSegment(loc.Notebook)
	safeSection := sanitizePathSegment(loc.Section)
	safePage := sanitizePathSegment(loc.Page)
	if safeNotebook == "" || safePage == "" {
		return "", fmt.Errorf("invalid file metadata for block %s", taskID)
	}
	notebookDir, err := a.resolveNotebookDir(safeNotebook, loc.Source)
	if err != nil {
		return "", fmt.Errorf("resolve notebook dir for block %s: %w", taskID, err)
	}
	filePath := filepath.Join(notebookDir, safeSection, safePage+".md")
	if !isPathWithinRoot(filePath, notebookDir) {
		return "", fmt.Errorf("resolved file path %q escapes notebook root %q", filePath, notebookDir)
	}

	newID := uuid.New().String()
	var writeErr error
	var emitFileDate string
	didWrite := false
	a.coordinator.LockBlockWrite(taskID, func() {
		a.coordinator.LockFileWrite(filePath, func() {
			// Focus-lock guard mirrors every other task setter (#444): refuse
			// rather than clobber an in-flight editor edit on the same file.
			if a.watcher != nil && a.watcher.IsFocusLocked(filePath) {
				writeErr = errBlockBeingEdited
				return
			}
			contentBytes, err := os.ReadFile(filePath)
			if err != nil {
				writeErr = err
				return
			}
			fileDate := fileOrDefaultDate(filePath)
			parsedBlocks, meta, _, _, parseErr := parser.ParseFileContent(string(contentBytes), safeNotebook, safeSection, safePage, fileDate, a.spacesPerTab)
			if parseErr != nil {
				writeErr = fmt.Errorf("parse file for append comment: %w", parseErr)
				return
			}

			// Find the parent's depth so the new NOTE sits at parentDepth+1
			// (spliceSubtree would otherwise anchor minDepth+delta and could
			// re-indent existing children if we passed a shallow placeholder).
			parentDepth := 0
			parentFound := false
			for _, b := range parsedBlocks {
				if b.ID == taskID {
					parentDepth = b.Depth
					parentFound = true
					break
				}
			}
			if !parentFound {
				writeErr = fmt.Errorf("parent block %s not found in file %s", taskID, filePath)
				return
			}

			existing := extractSubtree(parsedBlocks, taskID)
			comment := parser.ParsedBlock{
				ID:        newID,
				ParentID:  taskID,
				Type:      parser.BlockNote,
				Depth:     parentDepth + 1,
				CleanText: text,
				Author:    author,
				Timestamp: ts,
			}
			merged, ok := spliceSubtree(parsedBlocks, taskID, append(existing, comment))
			if !ok {
				writeErr = fmt.Errorf("parent block %s not found in file %s", taskID, filePath)
				return
			}

			frontmatter, body := parser.SplitFrontmatter(string(contentBytes))
			if frontmatter == "" {
				fmDate := meta.Date
				if fmDate == "" {
					fmDate = fileDate
				}
				frontmatter = fmt.Sprintf("---\nnotebook: %s\nsection: %s\npage: %s\ndate: %s\ntags: []\n---\n", strconv.Quote(safeNotebook), strconv.Quote(safeSection), strconv.Quote(safePage), strconv.Quote(fmDate))
				body = string(contentBytes)
			}
			newContent := parser.RenderFileContent(merged, body, frontmatter, a.spacesPerTab)
			a.tracker.RegisterWrite(filePath)
			if err := parser.WriteFileAtomic(filePath, []byte(newContent)); err != nil {
				writeErr = err
				return
			}
			didWrite = true

			blocks, remeta, _, _, err := parser.ParseFileContent(newContent, meta.Notebook, meta.Section, meta.Page, meta.Date, a.spacesPerTab)
			if err == nil {
				var idxErr error
				a.coordinator.WithDBWrite(func() {
					idxErr = a.db.IndexFileBlocks(loc.Source, remeta.Notebook, remeta.Section, remeta.Page, blocks, remeta.Tags, remeta.Warnings...)
				})
				if idxErr != nil {
					log.Printf("AppendTaskComment: IndexFileBlocks failed: %v", idxErr)
				}
				for _, b := range blocks {
					if b.ID == taskID {
						emitFileDate = b.FileDate
					}
				}
			} else {
				log.Printf("AppendTaskComment: re-parse failed (file written, index stale until next scan): %v", err)
			}
			if emitFileDate == "" {
				emitFileDate = fileDate
			}
		})
	}) // LockBlockWrite
	if writeErr != nil {
		return "", writeErr
	}
	if didWrite {
		a.emitBlockChanged(taskID, safeNotebook, safeSection, safePage, emitFileDate)
	}
	return newID, nil
}

// spliceSubtree replaces the child sub-tree of parentID in blocks with the
// incoming children. It locates the parent, identifies the contiguous child
// range (depth > parent depth, per extractSubtree's rule), and substitutes it.
// The incoming children keep their own depth (the serializer re-indents), and
// their ParentID is defensively set to parentID so the re-parse resolves the
// hierarchy correctly. Returns ok=false if the parent wasn't found.
func spliceSubtree(blocks []parser.ParsedBlock, parentID string, children []parser.ParsedBlock) ([]parser.ParsedBlock, bool) {
	parentIdx := -1
	parentDepth := 0
	for i, b := range blocks {
		if b.ID == parentID {
			parentIdx = i
			parentDepth = b.Depth
			break
		}
	}
	if parentIdx < 0 {
		return nil, false
	}
	// Find the end of the existing child run.
	childEnd := parentIdx + 1
	for childEnd < len(blocks) && blocks[childEnd].Depth > parentDepth {
		childEnd++
	}

	// Defensive: stamp ParentID on every incoming child so the re-parse
	// resolves the hierarchy even if the caller omitted it. Normalize depths
	// so every child lives strictly beneath the parent: a plugin caller
	// (buggy or malicious) passing Depth <= parentDepth would otherwise render
	// at or above the parent's indent, corrupting the markdown nesting and
	// causing extractSubtree to silently drop the block on the next fetch.
	// Preserve relative ordering among siblings but anchor the shallowest to
	// parentDepth+1.
	normalized := make([]parser.ParsedBlock, len(children))
	if len(children) > 0 {
		minDepth := children[0].Depth
		for _, c := range children[1:] {
			if c.Depth < minDepth {
				minDepth = c.Depth
			}
		}
		delta := (parentDepth + 1) - minDepth
		for i, c := range children {
			c.ParentID = parentID
			c.Depth += delta
			// Clamp: even after the shift, nothing sits at or above the parent.
			if c.Depth <= parentDepth {
				c.Depth = parentDepth + 1
			}
			normalized[i] = c
		}
	}

	out := make([]parser.ParsedBlock, 0, len(blocks)-(childEnd-(parentIdx+1))+len(normalized))
	out = append(out, blocks[:parentIdx+1]...)
	out = append(out, normalized...)
	out = append(out, blocks[childEnd:]...)
	return out, true
}
