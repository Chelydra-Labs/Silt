package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"silt/backend/db"
	"silt/backend/parser"
	"silt/backend/plugins"

	"github.com/google/uuid"
)

// FetchSubtree returns a task block's child sub-tree: every block whose
// validated ParentID ancestor chain contains the task (#305). The hierarchy
// comes from the parser's single derivation, so this is a pure graph
// membership check — no depth-run recomputation. An empty slice means the
// task has no children (the modal opens with an empty editor so the user can
// add sub-notes inline). Read-only: no locks beyond vaultMu.
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
	safeSection, secErr := historySection(loc.Section)
	if secErr != nil {
		return nil, invalidNavigationPath(secErr)
	}
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

// extractSubtree collects the child sub-tree of parentID by parent-graph
// membership: every block after the parent whose ParentID ancestor chain
// contains the parent's id. The parent itself is NOT included. Parentage is
// whatever the parser's single validated derivation (parser.DeriveHierarchy)
// produced, so the sub-editor and the derived task counts can never disagree
// about a block's membership.
func extractSubtree(blocks []parser.ParsedBlock, parentID string) []parser.ParsedBlock {
	byID := make(map[string]parser.ParsedBlock, len(blocks))
	parentIdx := -1
	for i, b := range blocks {
		byID[b.ID] = b
		if b.ID == parentID {
			parentIdx = i
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
	for _, b := range blocks[parentIdx+1:] {
		if hasAncestor(byID, b, parentID) {
			subtree = append(subtree, b)
		}
	}
	return subtree
}

// hasAncestor reports whether b's ParentID chain contains ancestorID. The
// step cap survives a cyclic ParentID graph, which the parser's derivation
// cannot produce but a defensive splice (SaveSubtreeBlocks stamps ParentID
// directly) could — a step cap, like taskDerivedCounts' fixed 64-step guard
// (backend/db/indexing.go).
func hasAncestor(byID map[string]parser.ParsedBlock, b parser.ParsedBlock, ancestorID string) bool {
	pid := b.ParentID
	for steps := 0; pid != "" && steps <= len(byID); steps++ {
		if pid == ancestorID {
			return true
		}
		parent, ok := byID[pid]
		if !ok || parent.ParentID == pid {
			return false
		}
		pid = parent.ParentID
	}
	return false
}

// SaveSubtreeBlocks is the app-level entry point for the sub-editor splice
// (#305). First-party callers (the editor host) use this directly; plugins go
// through the PluginSaveSubtreeBlocks wrapper, which gates on
// CapContentMutate (SPECS §8.3 — plugins never call App methods directly).
func (a *App) SaveSubtreeBlocks(blockID string, children []parser.ParsedBlock) (bool, error) {
	return a.saveSubtreeBlocks(blockID, children, historyReasonEditor)
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
	return a.saveSubtreeBlocks(blockID, children, historyReasonPlugin)
}

// saveSubtreeBlocks is the shared core for the app-level and plugin-level entry
// points. It re-parses the whole file, replaces the parent's child sub-tree
// (parent-graph membership — the exact set extractSubtree computes) with the
// incoming children, then runs the canonical write chain: RenderFileContent → WriteFileAtomic → re-parse →
// IndexFileBlocks → emit block:changed. The parent task and all surrounding
// content (unmanaged prose, sibling blocks, other tasks) are preserved verbatim
// because RenderFileContent re-serializes the full slice. Holds both
// LockBlockWrite(parentID) and LockFileWrite(filePath) so the splice is atomic
// and races no concurrent writer. Returns true when a write occurred.
//
// Comments are not part of the sub-editor surface, so before splicing this
// re-reads every comment block owned by the saved task or by a task the
// incoming payload keeps (nearest TASK ancestor — see preserveTaskComments)
// and re-inserts each at its owner's position. A notes save therefore never
// rewrites their text or attribution, reorders them relative to each other, or
// deletes them through the editor — including a comment appended while the
// modal was open, since the fetch happens under the same write lock as the
// splice. Comment deletion has its own path (deleteBlock).
func (a *App) saveSubtreeBlocks(blockID string, children []parser.ParsedBlock, reason string) (bool, error) {
	if err := validateTaskBlockPriorities(children); err != nil {
		return false, err
	}
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
	safeSection, secErr := historySection(loc.Section)
	if secErr != nil {
		return false, invalidNavigationPath(secErr)
	}
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
				writeErr = blockBeingEditedError()
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

			merged, err := spliceSubtree(parsedBlocks, blockID, preserveTaskComments(parsedBlocks, blockID, children))
			if err != nil {
				writeErr = fmt.Errorf("splice sub-tree under %s in %s: %w", blockID, filePath, err)
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
			a.maybeCapturePageVersion(historyLoc(loc.Source, safeNotebook, safeSection, safePage), contentBytes, []byte(newContent), reason)
			if err := a.trackedPageWrite(filePath, []byte(newContent)); err != nil {
				writeErr = err
				return
			}
			didWrite = true
			// Snapshot the mtime/size BEFORE IndexFileBlocks commits so the
			// files row records the content just written, not whatever mtime a
			// concurrent external edit lands between the index commit and the
			// post-commit mark (same window indexFile closes). See
			// markFileIndexedBestEffort.
			var fileStat os.FileInfo
			if s, se := os.Stat(filePath); se == nil {
				fileStat = s
			}

			blocks, remeta, _, _, err := parser.ParseFileContent(newContent, meta.Notebook, meta.Section, meta.Page, meta.Date, a.spacesPerTab)
			if err == nil {
				var idxErr error
				a.coordinator.WithDBWrite(func() {
					idxErr = a.db.IndexFileBlocks(loc.Source, remeta.Notebook, remeta.Section, remeta.Page, blocks, remeta.Tags, remeta.Warnings...)
				})
				if idxErr != nil {
					log.Printf("SaveSubtreeBlocks: IndexFileBlocks failed: %v", idxErr)
				} else {
					a.markFileIndexedBestEffort(filePath, fileStat)
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
// [ts:: YYYY-MM-DDTHH:MM:SS]); `author` may be empty (nullable per
// ARCHITECTURE.md §2.2). The SDK generates `ts` client-side and passes it
// through; when it is empty the binding stamps the current local time so every
// app-written comment stays classifiable by parser.IsCommentBlock.
//
// parentCommentID nests the new NOTE under an existing comment (#438). Empty
// keeps the historical top-level behavior (direct child of the task). When
// set, the parent must be a NOTE already in the task's sub-tree; the reply
// is spliced immediately after that parent and its existing descendants.
func (a *App) AppendTaskComment(taskID, text, author, ts, parentCommentID string) (string, error) {
	return a.appendTaskComment(taskID, text, author, ts, parentCommentID, historyReasonEditor)
}

// PluginAppendTaskComment is the plugin-SDK wrapper for AppendTaskComment,
// gated by the standard capability + session checks so a third-party plugin
// without the CapContentMutate grant can't splice NOTE blocks into a task's
// sub-tree. Mirrors PluginSaveSubtreeBlocks.
func (a *App) PluginAppendTaskComment(pluginID, sessionToken, taskID, text, author, ts, parentCommentID string) (string, error) {
	if err := a.validatePluginSession(pluginID, sessionToken); err != nil {
		return "", err
	}
	if err := a.requireGrant(pluginID, plugins.CapContentMutate); err != nil {
		return "", err
	}
	return a.appendTaskComment(taskID, text, author, ts, parentCommentID, historyReasonPlugin)
}

// appendTaskComment is the shared core for the app-level and plugin-level
// entry points. Read-modify-write under one LockBlockWrite+LockFileWrite hold:
// parse the file, collect the existing child sub-tree, build a new NOTE block
// with a fresh UUID + the author/ts attribution tokens, splice the updated
// sub-tree back through spliceSubtree (the same helper SaveSubtreeBlocks uses),
// then run the canonical write chain. The append is atomic with respect to
// other comment posts and other task setters because the per-file write lock
// serializes the whole read-modify-write.
//
// parentCommentID empty → top-level NOTE child of the task (depth taskDepth+1).
// parentCommentID set → nested reply under that NOTE (depth parentDepth+1),
// inserted after the parent and its existing descendants inside the sub-tree.
func (a *App) appendTaskComment(taskID, text, author, ts, parentCommentID, reason string) (string, error) {
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
	// An app-written comment must always be classifiable by the attribution
	// predicate (backend/parser.IsCommentBlock), never mistaken for editable
	// prose: the SDK supplies ts, but stamp one here when a caller omits it.
	if strings.TrimSpace(ts) == "" {
		ts = time.Now().Format("2006-01-02T15:04:05")
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
	safeSection, secErr := historySection(loc.Section)
	if secErr != nil {
		return "", invalidNavigationPath(secErr)
	}
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
				writeErr = blockBeingEditedError()
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

			// Find the task so the new top-level NOTE sits at taskDepth+1
			// (spliceSubtree would otherwise anchor minDepth+delta and could
			// re-indent existing children if we passed a shallow placeholder).
			taskDepth := 0
			taskFound := false
			for _, b := range parsedBlocks {
				if b.ID == taskID {
					taskDepth = b.Depth
					taskFound = true
					break
				}
			}
			if !taskFound {
				writeErr = fmt.Errorf("parent block %s not found in file %s", taskID, filePath)
				return
			}

			existing := extractSubtree(parsedBlocks, taskID)
			comment := parser.ParsedBlock{
				ID:        newID,
				Type:      parser.BlockNote,
				CleanText: text,
				Author:    author,
				Timestamp: ts,
			}

			var newSubtree []parser.ParsedBlock
			if parentCommentID == "" {
				// Top-level: direct child of the task, append at end.
				comment.ParentID = taskID
				comment.Depth = taskDepth + 1
				newSubtree = append(existing, comment)
			} else {
				// Nested reply (#438): parent must be a NOTE already in this
				// task's sub-tree. Insert immediately after the parent and its
				// existing descendants so sibling order stays stable.
				parentIdx := -1
				parentDepth := 0
				for i, b := range existing {
					if b.ID == parentCommentID {
						if b.Type != parser.BlockNote {
							writeErr = fmt.Errorf("parent comment %s is not a NOTE", parentCommentID)
							return
						}
						parentIdx = i
						parentDepth = b.Depth
						break
					}
				}
				if parentIdx < 0 {
					// Distinguish "not a NOTE / missing entirely" from "exists
					// but outside this task" for a clearer error.
					for _, b := range parsedBlocks {
						if b.ID == parentCommentID {
							if b.Type != parser.BlockNote {
								writeErr = fmt.Errorf("parent comment %s is not a NOTE", parentCommentID)
								return
							}
							writeErr = fmt.Errorf("parent comment %s is not under task %s", parentCommentID, taskID)
							return
						}
					}
					writeErr = fmt.Errorf("parent comment %s not found", parentCommentID)
					return
				}
				comment.ParentID = parentCommentID
				comment.Depth = parentDepth + 1
				// Walk past the parent's existing descendants (depth > parent).
				insertAt := parentIdx + 1
				for insertAt < len(existing) && existing[insertAt].Depth > parentDepth {
					insertAt++
				}
				newSubtree = make([]parser.ParsedBlock, 0, len(existing)+1)
				newSubtree = append(newSubtree, existing[:insertAt]...)
				newSubtree = append(newSubtree, comment)
				newSubtree = append(newSubtree, existing[insertAt:]...)
			}

			merged, err := spliceSubtree(parsedBlocks, taskID, newSubtree)
			if err != nil {
				writeErr = fmt.Errorf("splice sub-tree under %s in %s: %w", taskID, filePath, err)
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
			a.maybeCapturePageVersion(historyLoc(loc.Source, safeNotebook, safeSection, safePage), contentBytes, []byte(newContent), reason)
			if err := a.trackedPageWrite(filePath, []byte(newContent)); err != nil {
				writeErr = err
				return
			}
			didWrite = true
			// Snapshot the mtime/size BEFORE IndexFileBlocks commits so the
			// files row records the content just written, not whatever mtime a
			// concurrent external edit lands between the index commit and the
			// post-commit mark (same window indexFile closes). See
			// markFileIndexedBestEffort.
			var fileStat os.FileInfo
			if s, se := os.Stat(filePath); se == nil {
				fileStat = s
			}

			blocks, remeta, _, _, err := parser.ParseFileContent(newContent, meta.Notebook, meta.Section, meta.Page, meta.Date, a.spacesPerTab)
			if err == nil {
				var idxErr error
				a.coordinator.WithDBWrite(func() {
					idxErr = a.db.IndexFileBlocks(loc.Source, remeta.Notebook, remeta.Section, remeta.Page, blocks, remeta.Tags, remeta.Warnings...)
				})
				if idxErr != nil {
					log.Printf("AppendTaskComment: IndexFileBlocks failed: %v", idxErr)
				} else {
					a.markFileIndexedBestEffort(filePath, fileStat)
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
// incoming children. It locates the parent, deletes every block after it by
// parent-graph membership (hasAncestor — the exact inverse of extractSubtree),
// and inserts the normalized children at parentIdx+1. The delete is
// deliberately non-contiguous: a column-0 region between members is a root,
// not a member, so a parent's children can straddle it — a depth-run delete
// would stop at the region and leave a post-region child on disk while the
// normalized list already carries it, rendering the same UUID twice and
// breaking IndexFileBlocks on the blocks.id primary key. An indented region
// is itself a member and is deleted or kept with the run like any child.
// The incoming children keep their own depth (the serializer re-indents), and
// their ParentID is defensively set to parentID so the re-parse resolves the
// hierarchy correctly. Returns an error when the parent wasn't found, or when
// an incoming child id collides with a kept block (or another child) — a
// splice that would write one UUID twice must be refused, not written.
func spliceSubtree(blocks []parser.ParsedBlock, parentID string, children []parser.ParsedBlock) ([]parser.ParsedBlock, error) {
	byID := make(map[string]parser.ParsedBlock, len(blocks))
	parentIdx := -1
	parentDepth := 0
	for i, b := range blocks {
		byID[b.ID] = b
		if b.ID == parentID {
			parentIdx = i
			parentDepth = b.Depth
		}
	}
	if parentIdx < 0 {
		return nil, fmt.Errorf("parent block %s not found", parentID)
	}

	if collidingID := spliceChildIDCollision(blocks, parentIdx, byID, parentID, children); collidingID != "" {
		return nil, fmt.Errorf("subtree save would write block id %s twice — the page already carries it outside this sub-tree; re-fetch the sub-tree and retry", collidingID)
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

	out := make([]parser.ParsedBlock, 0, len(blocks)+len(normalized))
	out = append(out, blocks[:parentIdx+1]...)
	out = append(out, normalized...)
	// Non-members keep their file order — a region between two members
	// stays on the page, now after the replaced child run.
	for _, b := range blocks[parentIdx+1:] {
		if !hasAncestor(byID, b, parentID) {
			out = append(out, b)
		}
	}
	return out, nil
}

// preserveTaskComments returns children with every comment block that belongs
// to the saved task or to a task the payload keeps re-inserted at its owner's
// position, so the sub-editor splice can never rewrite, reorder or drop them.
// Ownership is the nearest TASK ancestor (the same attribution the derived
// comments_count uses): the saved root always owns its comments, a kept
// sub-task owns its own, and a comment whose nearest TASK ancestor is absent
// from the payload is deleted with that sub-task instead of being resurrected
// onto a sibling.
//
// Placement is anchored, not tail-appended: a preserved block goes immediately
// after its nearest placed ancestor's run (its original parent when the payload
// carries it or this pass already placed it), so a reply stays under its
// comment rather than behind an unrelated sibling — spliceSubtree re-derives
// parentage from depth, and a tail-appended reply would re-parent to whatever
// block happened to precede it. When no ancestor survived, the anchor is the
// owner itself: the block re-anchors as a direct child of the payload base
// instead of keeping an indentation that would now attach it to a sibling.
// Relative order within an anchor is file order. Depths are rebased by the
// anchor's output offset so reply nesting survives while the payload's own
// depths stay untouched: spliceSubtree derives one delta from the shallowest
// block, which must remain the payload's base.
func preserveTaskComments(blocks []parser.ParsedBlock, taskID string, children []parser.ParsedBlock) []parser.ParsedBlock {
	current := extractSubtree(blocks, taskID)
	if len(current) == 0 {
		return children
	}
	byID := make(map[string]parser.ParsedBlock, len(blocks))
	for _, b := range blocks {
		byID[b.ID] = b
	}
	// Caller-supplied content wins: preserving a comment the payload already
	// carries would trip spliceChildIDCollision on this rule's own injection,
	// and the caller's text/author/ts are what the round-trip meant to save.
	incoming := make(map[string]bool, len(children))
	incomingTasks := make(map[string]bool, len(children))
	for _, c := range children {
		incoming[c.ID] = true
		if c.Type == parser.BlockTask {
			incomingTasks[c.ID] = true
		}
	}
	preserved := make([]parser.ParsedBlock, 0)
	for _, b := range current {
		if !incoming[b.ID] && isTaskComment(byID, b, taskID, incomingTasks) {
			preserved = append(preserved, b)
		}
	}
	if len(preserved) == 0 {
		return children
	}

	// The incoming children's shallowest depth is the base spliceSubtree
	// normalizes against; preserved depths are expressed relative to it.
	base := 0
	if len(children) > 0 {
		base = children[0].Depth
		for _, c := range children[1:] {
			if c.Depth < base {
				base = c.Depth
			}
		}
	}

	out := make([]parser.ParsedBlock, len(children))
	copy(out, children)
	pos := make(map[string]int, len(children)+len(preserved))
	for i, c := range out {
		pos[c.ID] = i
	}
	// runEnd is the last index of a placed block's sub-tree run: depths are
	// consistent (payload as given, preserved as rebased), so a run ends at the
	// first following block that is not strictly deeper.
	runEnd := func(i int) int {
		end := i
		for j := i + 1; j < len(out); j++ {
			if out[j].Depth > out[i].Depth {
				end = j
			} else {
				break
			}
		}
		return end
	}

	// current is in file order (parents before children), so every anchor is
	// already placed by the time its reply is handled.
	for _, b := range preserved {
		// Root anchor: no placed ancestor, so the owner itself (the saved task)
		// is the anchor and the block lands at the tail of the payload run.
		at := len(out)
		rootAnchor := true
		anchorDepth := base
		var origAnchorDepth int
		for pid, steps := b.ParentID, 0; pid != "" && pid != taskID && steps <= len(byID); steps++ {
			if idx, ok := pos[pid]; ok {
				at = runEnd(idx) + 1
				rootAnchor = false
				anchorDepth = out[idx].Depth
				if orig, ok := byID[pid]; ok {
					origAnchorDepth = orig.Depth
				} else {
					origAnchorDepth = anchorDepth
				}
				break
			}
			parent, ok := byID[pid]
			if !ok || parent.ParentID == pid {
				break
			}
			pid = parent.ParentID
		}
		if rootAnchor {
			// The anchor is the owner, so the block is a direct child of the
			// payload base. Reusing its original indentation would land it one
			// level deeper than the payload's shallowest block whenever its
			// parent is gone but was not the shallowest sibling (a reply to a
			// deleted prose NOTE, or gap-indented legacy data); the tail
			// position would then attach it to the last payload sibling and
			// move comments_count to that sibling's task.
			b.Depth = anchorDepth
		} else {
			b.Depth = anchorDepth + (b.Depth - origAnchorDepth)
			// Defensive: a preserved block under a placed anchor stays strictly
			// deeper than it, or the re-parse would re-parent it to the anchor's
			// own sibling.
			if b.Depth <= anchorDepth {
				b.Depth = anchorDepth + 1
			}
		}
		out = append(out, parser.ParsedBlock{})
		copy(out[at+1:], out[at:])
		out[at] = b
		for id, p := range pos {
			if p >= at {
				pos[id] = p + 1
			}
		}
		pos[b.ID] = at
	}
	return out
}

// isTaskComment reports whether b is a comment owned by the saved task or by a
// task the payload keeps: b carries attribution and the nearest TASK ancestor
// of b's ParentID chain is taskID or a task in incomingTasks. A nearer TASK
// ancestor outside the payload means the comment belongs to a sub-task the
// editor deleted, so it goes away with it. The step cap mirrors hasAncestor's
// cyclic-graph guard.
func isTaskComment(byID map[string]parser.ParsedBlock, b parser.ParsedBlock, taskID string, incomingTasks map[string]bool) bool {
	if !parser.IsCommentBlock(b) {
		return false
	}
	pid := b.ParentID
	for steps := 0; pid != "" && steps <= len(byID); steps++ {
		if pid == taskID {
			return true
		}
		parent, ok := byID[pid]
		if !ok || parent.ParentID == pid {
			return false
		}
		if parent.Type == parser.BlockTask {
			return incomingTasks[parent.ID]
		}
		pid = parent.ParentID
	}
	return false
}

// spliceChildIDCollision reports an incoming child id that would render twice
// in the spliced page: it matches a block that SURVIVES the splice (the
// parent, anything before it, or any non-member after it), or another
// incoming child. Ids matching the DELETED old members are fine — replacing
// a child with an edited copy that keeps its id is the normal update flow.
// Only collisions the splice itself introduces count, so a page that already
// carries a duplicate elsewhere does not lock its sub-trees against edits.
func spliceChildIDCollision(blocks []parser.ParsedBlock, parentIdx int, byID map[string]parser.ParsedBlock, parentID string, children []parser.ParsedBlock) string {
	keptIDs := make(map[string]bool, len(blocks))
	for i, b := range blocks {
		if i <= parentIdx || !hasAncestor(byID, b, parentID) {
			keptIDs[b.ID] = true
		}
	}
	seenChildren := make(map[string]bool, len(children))
	for _, c := range children {
		if c.ID == "" {
			continue
		}
		if keptIDs[c.ID] || seenChildren[c.ID] {
			return c.ID
		}
		seenChildren[c.ID] = true
	}
	return ""
}
