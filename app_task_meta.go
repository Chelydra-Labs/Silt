package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"silt/backend/db"
	"silt/backend/parser"
)

// SetTaskOwner rewrites the [owner:: NAME] inline token on a task block,
// the assignee field surfaced by the unified TaskEditDrawer (#412). Pass an
// empty string to clear the token. The renderer re-emits the token from
// ParsedBlock.Owner (writer.go ~:1210, omit-when-empty), so a parse → render
// round trip is byte-stable and never produces two competing tokens.
//
// Follows the canonical write chain (same as SetTaskRecurrence):
// LockBlockWrite -> LockFileWrite -> ReadFile -> ParseFileContent -> mutate
// block.Owner -> RenderFileContent -> WriteFileAtomic -> re-parse ->
// IndexFileBlocks -> emit block:changed.
func (a *App) SetTaskOwner(blockID, owner string) error {
	return a.setTaskOwner(blockID, owner)
}

// PluginSetTaskOwner is the plugin-SDK wrapper for SetTaskOwner, gated by the
// standard capability + session checks (SPECS §8.3 — plugins go through
// PluginContext, never direct wailsjs bindings). Mirrors PluginSetTaskDueDate.
func (a *App) PluginSetTaskOwner(pluginID, sessionToken, blockID, owner string) (bool, error) {
	return a.wrapPluginMutate(pluginID, sessionToken, func() error {
		return a.setTaskOwner(blockID, owner)
	})
}

// mutateTaskBlock is the canonical task write-chain shared by the four
// single-field setters (SetTaskOwner/Priority/Tags/Title): locate the block,
// sanity-check its file metadata, then under LockBlockWrite+LockFileWrite
// read -> parse -> apply `mutate` -> render -> WriteFileAtomic -> re-parse ->
// IndexFileBlocks -> emit block:changed. `label` prefixes diagnostic log and
// parse-error messages so failures stay attributable. Callers retain any
// input validation (empty-title guard, tag dedupe) BEFORE calling; this
// helper owns only the shared write path so a future cross-cutting guard
// (e.g. focus-lock, #444) lands in exactly one place.
func (a *App) mutateTaskBlock(blockID, label string, mutate func(*parser.ParsedBlock)) error {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.db == nil {
		return fmt.Errorf("vault database not loaded")
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
		return fmt.Errorf("block %s not found in SQLite: %w", blockID, err)
	}
	notebook, section, page, blockType := loc.Notebook, loc.Section, loc.Page, loc.BlockType
	if blockType != string(parser.BlockTask) {
		return fmt.Errorf("block %s is not a task", blockID)
	}

	safeNotebook := sanitizePathSegment(notebook)
	safeSection := sanitizePathSegment(section)
	safePage := sanitizePathSegment(page)
	if safeNotebook == "" || safePage == "" {
		return fmt.Errorf("invalid file metadata for block %s", blockID)
	}
	notebookDir, err := a.resolveNotebookDir(safeNotebook, loc.Source)
	if err != nil {
		return fmt.Errorf("resolve notebook dir for block %s: %w", blockID, err)
	}
	filePath := filepath.Join(notebookDir, safeSection, safePage+".md")
	if !isPathWithinRoot(filePath, notebookDir) {
		return fmt.Errorf("resolved file path %q escapes notebook root %q", filePath, notebookDir)
	}

	var writeErr error
	var emitFileDate string
	didWrite := false
	a.coordinator.LockBlockWrite(blockID, func() {
		a.coordinator.LockFileWrite(filePath, func() {
			// Don't clobber a file the user is actively editing (the editor
			// holds a focus lock on the file while focused). Mirrors MutateBlock
			// so every single-field task-setter refuses consistently; the
			// frontend surfaces the error via the shared ErrorBanner (#444).
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
				writeErr = fmt.Errorf("failed to parse file for %s: %w", label, parseErr)
				return
			}
			found := false
			for i := range parsedBlocks {
				if parsedBlocks[i].ID == blockID && parsedBlocks[i].Type == parser.BlockTask {
					mutate(&parsedBlocks[i])
					// [modified::] stamp (#440): every single-field task write
					// touches the task line; stamp local ISO time after mutate.
					parsedBlocks[i].ModifiedAt = time.Now().Format("2006-01-02T15:04:05")
					found = true
					break
				}
			}
			if !found {
				writeErr = fmt.Errorf("block %s not found in file %s", blockID, filePath)
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
			newContent := parser.RenderFileContent(parsedBlocks, body, frontmatter, a.spacesPerTab)
			a.tracker.RegisterWrite(filePath)
			if err := parser.WriteFileAtomic(filePath, []byte(newContent)); err != nil {
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
					log.Printf("%s: IndexFileBlocks failed: %v", label, idxErr)
				} else {
					a.markFileIndexedBestEffort(filePath, fileStat)
				}
				for _, b := range blocks {
					if b.ID == blockID {
						emitFileDate = b.FileDate
					}
				}
			} else {
				log.Printf("%s: re-parse of rendered content failed (file written, index stale until next scan): %v", label, err)
			}
			if emitFileDate == "" {
				emitFileDate = fileDate
			}
		})
	}) // LockBlockWrite
	if writeErr != nil {
		return writeErr
	}
	if didWrite {
		a.emitBlockChanged(blockID, safeNotebook, safeSection, safePage, emitFileDate)
	}
	return nil
}

// setTaskOwner is the shared core for the app-level and plugin-level entry
// points. Delegates the shared write-chain to mutateTaskBlock; see that
// method for the chain.
func (a *App) setTaskOwner(blockID, owner string) error {
	return a.mutateTaskBlock(blockID, "SetTaskOwner", func(b *parser.ParsedBlock) { b.Owner = owner })
}

// SetTaskOrder rewrites the [order:: N] inline token on a task block (#426).
// Pass 0 to clear the token (the renderer omits it when ManualOrder == 0,
// writer.go ~:1258). A negative value is rejected up front so a UI glitch
// can't stamp an off-by-one into the file. Follows the canonical write
// chain (same as SetTaskOwner); the row mapper caches the new value into
// tasks.manual_order so the next query sees it without re-parsing markdown.
func (a *App) SetTaskOrder(blockID string, order int) error {
	return a.setTaskOrder(blockID, order)
}

// PluginSetTaskOrder is the plugin-SDK wrapper for SetTaskOrder, gated by
// the standard capability + session checks. Mirrors PluginSetTaskOwner.
func (a *App) PluginSetTaskOrder(pluginID, sessionToken, blockID string, order int) (bool, error) {
	return a.wrapPluginMutate(pluginID, sessionToken, func() error {
		return a.setTaskOrder(blockID, order)
	})
}

// setTaskOrder is the shared core for the app-level and plugin-level entry
// points. Validates the input (negative order is a contract violation —
// the token is 1-based; 0 is the "unset" sentinel and clears the token)
// BEFORE entering the write chain so a rejection leaves the file untouched.
func (a *App) setTaskOrder(blockID string, order int) error {
	if order < 0 {
		return fmt.Errorf("task order must be >= 0 (got %d)", order)
	}
	if order > 1_000_000 {
		return fmt.Errorf("task order must be <= 1,000,000 (got %d)", order)
	}
	return a.mutateTaskBlock(blockID, "SetTaskOrder", func(b *parser.ParsedBlock) { b.ManualOrder = order })
}

// SetTaskOrders batch-renumbers [order:: N] tokens across one or more task
// blocks in a single atomic write PER FILE (#426). Use this instead of N
// individual SetTaskOrder calls when a drag-reorder shifts multiple tasks in
// the same file: one read-parse-render-write-reindex cycle per file, so a
// mid-batch IPC failure leaves every file in the batch unchanged on disk (vs.
// N individual calls where a mid-loop failure produces a half-renumbered
// sequence).
//
// ids and orders are parallel slices; ids[i] gets orders[i]. A negative or
// >1,000,000 order is rejected up front for every entry before touching disk.
func (a *App) SetTaskOrders(ids []string, orders []int) error {
	return a.setTaskOrders(ids, orders)
}

// PluginSetTaskOrders is the plugin-SDK wrapper for SetTaskOrders, gated by
// the standard capability + session checks. Mirrors PluginSetTaskOrder.
func (a *App) PluginSetTaskOrders(pluginID, sessionToken string, ids []string, orders []int) (bool, error) {
	return a.wrapPluginMutate(pluginID, sessionToken, func() error {
		return a.setTaskOrders(ids, orders)
	})
}

// setTaskOrders is the batch core. Groups the (id, order) pairs by file, then
// performs one canonical write cycle per file (read -> parse -> mutate ALL
// blocks for that file -> render -> WriteFileAtomic -> re-parse -> reindex).
// Each file's write is atomic; a failure on file N does NOT roll back files
// 1..N-1 (they're independent files in independent sections/notebooks).
func (a *App) setTaskOrders(ids []string, orders []int) error {
	if len(ids) != len(orders) {
		return fmt.Errorf("ids and orders must have the same length (got %d and %d)", len(ids), len(orders))
	}
	if len(ids) == 0 {
		return nil
	}
	for _, o := range orders {
		if o < 0 {
			return fmt.Errorf("task order must be >= 0 (got %d)", o)
		}
		if o > 1_000_000 {
			return fmt.Errorf("task order must be <= 1,000,000 (got %d)", o)
		}
	}

	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.db == nil {
		return fmt.Errorf("vault database not loaded")
	}
	a.wg.Add(1)
	defer a.wg.Done()

	// Resolve every block's file location + validate it's a task.
	type pending struct {
		blockID string
		order   int
		loc     db.BlockLocation
	}
	all := make([]pending, 0, len(ids))
	for i, blockID := range ids {
		var loc db.BlockLocation
		err := a.coordinator.WithDBReadResult(func() error {
			var e error
			loc, e = a.db.GetBlockLocation(blockID)
			return e
		})
		if err != nil {
			return fmt.Errorf("block %s not found in SQLite: %w", blockID, err)
		}
		if loc.BlockType != string(parser.BlockTask) {
			return fmt.Errorf("block %s is not a task", blockID)
		}
		all = append(all, pending{blockID, orders[i], loc})
	}

	// Group by file (source + notebook + section + page = unique file).
	fileKey := func(loc db.BlockLocation) string {
		return loc.Source + "\x00" + loc.Notebook + "\x00" + loc.Section + "\x00" + loc.Page
	}
	groups := make(map[string][]pending)
	for _, p := range all {
		groups[fileKey(p.loc)] = append(groups[fileKey(p.loc)], p)
	}

	// One write cycle per file. Each group shares the notebook/section/page
	// of its members (they're in the same file by construction).
	//
	// Map iteration order is non-deterministic in Go; sort the keys first so
	// a partial-failure across files produces reproducible behavior.
	fileKeys := make([]string, 0, len(groups))
	for k := range groups {
		fileKeys = append(fileKeys, k)
	}
	sort.Strings(fileKeys)
	// Pre-scan: refuse up front if ANY target file is focus-locked. Without
	// this, a batch reorder spanning multiple files could write the first
	// file(s) before a later locked file returns errBlockBeingEdited, leaving
	// the reorder half-applied. The per-file check inside the write loop
	// (below) still guards the narrow race where a file becomes locked mid-batch.
	for _, fk := range fileKeys {
		first := groups[fk][0]
		notebookDir, err := a.resolveNotebookDir(sanitizePathSegment(first.loc.Notebook), first.loc.Source)
		if err != nil {
			return fmt.Errorf("resolve notebook dir for block %s: %w", first.blockID, err)
		}
		filePath := filepath.Join(notebookDir, sanitizePathSegment(first.loc.Section), sanitizePathSegment(first.loc.Page)+".md")
		if a.watcher != nil && a.watcher.IsFocusLocked(filePath) {
			return blockBeingEditedError()
		}
	}
	for _, fk := range fileKeys {
		group := groups[fk]
		first := group[0]
		safeNotebook := sanitizePathSegment(first.loc.Notebook)
		safeSection := sanitizePathSegment(first.loc.Section)
		safePage := sanitizePathSegment(first.loc.Page)
		if safeNotebook == "" || safePage == "" {
			return fmt.Errorf("invalid file metadata for block %s", first.blockID)
		}
		notebookDir, err := a.resolveNotebookDir(safeNotebook, first.loc.Source)
		if err != nil {
			return fmt.Errorf("resolve notebook dir for block %s: %w", first.blockID, err)
		}
		filePath := filepath.Join(notebookDir, safeSection, safePage+".md")
		if !isPathWithinRoot(filePath, notebookDir) {
			return fmt.Errorf("resolved file path %q escapes notebook root %q", filePath, notebookDir)
		}

		// Build a lookup so the parse loop can apply the right order per block.
		orderByID := make(map[string]int, len(group))
		for _, p := range group {
			orderByID[p.blockID] = p.order
		}

		var writeErr error
		var emitBlocks []parser.ParsedBlock
		a.coordinator.LockBlockWrite(first.blockID, func() {
			a.coordinator.LockFileWrite(filePath, func() {
				// Don't clobber a file the user is actively editing — a DnD
				// reorder on a focused file is the highest-risk clobber (#444).
				// Mirrors mutateTaskBlock / MutateBlock; the frontend surfaces
				// the error via the shared ErrorBanner.
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
					writeErr = fmt.Errorf("failed to parse file for SetTaskOrders: %w", parseErr)
					return
				}
				found := 0
				for i := range parsedBlocks {
					if parsedBlocks[i].Type == parser.BlockTask {
						if newOrder, ok := orderByID[parsedBlocks[i].ID]; ok {
							parsedBlocks[i].ManualOrder = newOrder
							parsedBlocks[i].ModifiedAt = time.Now().Format("2006-01-02T15:04:05")
							found++
						}
					}
				}
				if found != len(orderByID) {
					// Compare against unique-id count, not len(group): a duplicate
					// id in `ids` produces multiple `pending` entries but only one
					// block to find, so the old check reported a phantom shortfall.
					writeErr = fmt.Errorf("SetTaskOrders: expected %d unique task blocks in %s, found %d", len(orderByID), filePath, found)
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
				newContent := parser.RenderFileContent(parsedBlocks, body, frontmatter, a.spacesPerTab)
				a.tracker.RegisterWrite(filePath)
				if err := parser.WriteFileAtomic(filePath, []byte(newContent)); err != nil {
					writeErr = err
					return
				}
				// Snapshot the mtime/size BEFORE IndexFileBlocks commits so the
				// files row records the content just written, not whatever mtime
				// a concurrent external edit lands between the index commit and
				// the post-commit mark (same window indexFile closes). See
				// markFileIndexedBestEffort.
				var fileStat os.FileInfo
				if s, se := os.Stat(filePath); se == nil {
					fileStat = s
				}

				blocks, remeta, _, _, err := parser.ParseFileContent(newContent, meta.Notebook, meta.Section, meta.Page, meta.Date, a.spacesPerTab)
				if err == nil {
					var idxErr error
					a.coordinator.WithDBWrite(func() {
						idxErr = a.db.IndexFileBlocks(first.loc.Source, remeta.Notebook, remeta.Section, remeta.Page, blocks, remeta.Tags, remeta.Warnings...)
					})
					if idxErr != nil {
						log.Printf("SetTaskOrders: IndexFileBlocks failed: %v", idxErr)
					} else {
						a.markFileIndexedBestEffort(filePath, fileStat)
					}
					// Collect the re-parsed blocks for emit. Map IDs once so
					// this is O(moved) rather than O(moved × blocksInFile).
					blocksByID := make(map[string]parser.ParsedBlock, len(blocks))
					for _, b := range blocks {
						blocksByID[b.ID] = b
					}
					for _, p := range group {
						if b, ok := blocksByID[p.blockID]; ok {
							emitBlocks = append(emitBlocks, b)
						}
					}
				} else {
					log.Printf("SetTaskOrders: re-parse failed (file written, index stale until next scan): %v", err)
					// Mirror mutateTaskBlock: emit even on re-parse failure so the
					// UI gets a refresh signal. Use the pre-write fileDate as
					// fallback (emitBlockChanged only reads ID + FileDate).
					for _, p := range group {
						emitBlocks = append(emitBlocks, parser.ParsedBlock{ID: p.blockID, FileDate: fileDate})
					}
				}
			})
		})
		if writeErr != nil {
			return writeErr
		}
		// Emit block:changed for each task that was moved.
		for _, b := range emitBlocks {
			a.emitBlockChanged(b.ID, safeNotebook, safeSection, safePage, b.FileDate)
		}
	}
	return nil
}

// SetTaskPriority rewrites the [priority:: N] inline token on a task block
// (#412). Pass 0 (or the legacy omitted-token value 3) to omit the token.
// Positive non-default priorities round-trip through the canonical renderer.
//
// Follows the canonical write chain (same as SetTaskOwner).
func (a *App) SetTaskPriority(blockID string, priority int) error {
	return a.setTaskPriority(blockID, priority)
}

// PluginSetTaskPriority is the plugin-SDK wrapper for SetTaskPriority, gated
// by the standard capability + session checks. Mirrors PluginSetTaskDueDate.
func (a *App) PluginSetTaskPriority(pluginID, sessionToken, blockID string, priority int) (bool, error) {
	return a.wrapPluginMutate(pluginID, sessionToken, func() error {
		return a.setTaskPriority(blockID, priority)
	})
}

// setTaskPriority is the shared core for the app-level and plugin-level entry
// points. Delegates the shared write-chain to mutateTaskBlock; see that
// method for the chain.
func (a *App) setTaskPriority(blockID string, priority int) error {
	return a.mutateTaskBlock(blockID, "SetTaskPriority", func(b *parser.ParsedBlock) { b.Priority = priority })
}

// SetTaskTags rewrites the hashtag set on a task's CleanText (the prose),
// the tag field surfaced by the unified TaskEditDrawer (#412). Tags are
// #namespace/path hashtags living in the prose, NOT [key::] tokens — the
// two live in disjoint parts of the block (prose vs. re-emitted tokens) so
// tag surgery never touches the [key::] tokens, the checkbox marker, or the
// identity comment.
//
// Byte-preservation invariant: prose words, ((uuid)) block-refs, and tags
// that remain in the set stay byte-for-byte. Only added/removed hashtags
// change. The diff is computed against db.ExtractTags (the canonical
// indexer derivation), so the canonical names always match what the index
// sees:
//   - toRemove = current − new: each removed #tag substring is stripped from
//     CleanText with careful whitespace handling (one adjacent space is
//     removed along with the tag so no double/trailing/leading space is left).
//   - toAdd = new − current: appended as " #tag" at the end of the prose.
//
// After re-render + re-index, db.ExtractTags re-derives the new set into
// the tags table. Pass an empty slice (or nil) to clear all hashtags.
//
// Follows the canonical write chain (same as SetTaskOwner).
func (a *App) SetTaskTags(blockID string, tags []string) error {
	return a.setTaskTags(blockID, tags)
}

// PluginSetTaskTags is the plugin-SDK wrapper for SetTaskTags, gated by the
// standard capability + session checks. Mirrors PluginSetTaskDueDate.
func (a *App) PluginSetTaskTags(pluginID, sessionToken, blockID string, tags []string) (bool, error) {
	return a.wrapPluginMutate(pluginID, sessionToken, func() error {
		return a.setTaskTags(blockID, tags)
	})
}

// setTaskTags is the shared core for the app-level and plugin-level entry
// points. Delegates the shared write-chain to mutateTaskBlock; see that
// method for the chain. The byte-surgery helpers (stripTagsFromCleanText,
// appendTagsToCleanText) preserve every other byte of CleanText.
func (a *App) setTaskTags(blockID string, tags []string) error {
	// Normalize (drop empties, de-dupe, strip leading #) before the write chain.
	newTags := dedupeTags(tags)
	return a.mutateTaskBlock(blockID, "SetTaskTags", func(b *parser.ParsedBlock) {
		b.CleanText = rebuildTagSet(b.CleanText, newTags)
	})
}

// SetTaskTitle rewrites the prose portion of a task's CleanText, the "title"
// field surfaced by the unified TaskEditDrawer (#412). The prose is what the
// user reads as the task description; hashtags (#namespace/path) and
// ((uuid)) block-refs live alongside the prose in CleanText and MUST be
// preserved across the rewrite.
//
// Byte-preservation invariant: every hashtag and every ((uuid)) ref in the
// original CleanText is preserved verbatim in the result; only the prose
// portion changes. The checkbox marker, all [key::] tokens, and the
// block-identity comment are NOT in CleanText (the renderer re-emits them
// from ParsedBlock fields), so they are safe by construction.
//
// Follows the canonical write chain (same as SetTaskOwner).
func (a *App) SetTaskTitle(blockID, title string) error {
	return a.setTaskTitle(blockID, title)
}

// PluginSetTaskTitle is the plugin-SDK wrapper for SetTaskTitle, gated by the
// standard capability + session checks. Mirrors PluginSetTaskDueDate.
func (a *App) PluginSetTaskTitle(pluginID, sessionToken, blockID, title string) (bool, error) {
	return a.wrapPluginMutate(pluginID, sessionToken, func() error {
		return a.setTaskTitle(blockID, title)
	})
}

// setTaskTitle is the shared core for the app-level and plugin-level entry
// points. Delegates the shared write-chain to mutateTaskBlock; see that
// method for the chain. The byte-surgery helper (replaceTitleInCleanText)
// tokenizes CleanText into hashtags, block-refs, and prose, then reassembles
// the new title + preserved tokens.
func (a *App) setTaskTitle(blockID, title string) error {
	// SDK contract guard: an empty/whitespace title would silently strip all
	// prose from the task. Reject before touching disk.
	if strings.TrimSpace(title) == "" {
		return fmt.Errorf("task title must not be empty")
	}
	return a.mutateTaskBlock(blockID, "SetTaskTitle", func(b *parser.ParsedBlock) {
		b.CleanText = replaceTitleInCleanText(b.CleanText, title)
	})
}
