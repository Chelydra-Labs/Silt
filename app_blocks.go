package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"silt/backend/db"
	"silt/backend/parser"
	"silt/backend/recurrence"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// errBlockBeingEdited is the sentinel returned by MutateBlock when the target
// file is focus-locked (a user is editing it in another view). It is kept as
// the errors.Is target so the 18+ existing test assertions keep working after
// the #478 migration: the return sites now hand back a blockBeingEditedError()
// (an *IPCError carrying CodeBlockBeingEdited) that wraps this sentinel, so
// errors.Is(err, errBlockBeingEdited) still returns true while the frontend
// additionally receives a stable code across the IPC boundary.
var errBlockBeingEdited = fmt.Errorf("block is being edited in another view")

// blockBeingEditedError returns the focus-lock rejection as an *IPCError that
// carries the stable CodeBlockBeingEdited (#478) AND satisfies
// errors.Is(err, errBlockBeingEdited) via the wrapped sentinel. Use this at
// every return site instead of the bare sentinel so the frontend can map on
// the code.
func blockBeingEditedError() *IPCError {
	return wrapSentinelAsIPCError(CodeBlockBeingEdited, errBlockBeingEdited.Error(), errBlockBeingEdited)
}

// FetchPageBlocks returns a flat list of all blocks for a page, ordered by
// line_number. A page is a single file; each block carries its own file_date.
// The notebook's source is resolved server-side from its (globally-unique)
// name so a linked notebook sharing a display name with a vault notebook
// returns its own page (#100).
func (a *App) FetchPageBlocks(notebook, section, page string) ([]parser.ParsedBlock, error) {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.db == nil {
		return nil, fmt.Errorf("vault database not loaded")
	}

	a.wg.Add(1)
	defer a.wg.Done()

	source := a.resolveSourceByName(sanitizePathSegment(notebook))
	var res []parser.ParsedBlock
	var err error
	a.coordinator.WithDBRead(func() {
		res, err = a.db.FetchPageBlocks(source, notebook, section, page)
	})

	return res, err
}

// DistinctOwners returns the sorted, de-duplicated set of task owners in the
// vault, optionally narrowed by an ASCII-case-insensitive prefix — the source
// for the mention typeahead (#184, #332). Read-only projection of the tasks
// index; no mention state is persisted to SQLite. An empty prefix returns the
// full set so the editor's focus-load can seed the cache.
func (a *App) DistinctOwners(prefix string) ([]string, error) {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.db == nil {
		return nil, fmt.Errorf("vault database not loaded")
	}

	a.wg.Add(1)
	defer a.wg.Done()

	var res []string
	var err error
	a.coordinator.WithDBRead(func() {
		res, err = a.db.DistinctOwners(prefix)
	})

	return res, err
}

// UpdateBlockState changes task status and updates the file and cache.
//
// To avoid TOCTOU races between the DB read and the file write, we look up the
// block's UUID, file metadata, and the lock by file path, then re-locate the
// target line inside the file write lock by scanning for the UUID comment. The
// UUID is the source of truth for the target line, not the cached line number.
//
// The first return value is the spawned recurrence instance's UUID when a
// recurring task transitions TODO/DOING → DONE (the next instance is spliced
// below the completed line in the same atomic write); it is empty for every
// other transition (non-recurring, TODO/DOING, or an already-DONE no-op). The
// frontend can chain off it directly instead of re-querying for the sibling.
func (a *App) UpdateBlockState(blockID string, newState string) (string, error) {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	// Guard against a meaningless no-op that the frontend might interpret
	// as an error. The only valid task status values are TODO, DOING, DONE.
	switch newState {
	case "TODO", "DOING", "DONE":
	default:
		return "", fmt.Errorf("invalid target status: %s (valid: TODO, DOING, DONE)", newState)
	}

	if a.db == nil {
		return "", fmt.Errorf("vault database not loaded")
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
		return "", fmt.Errorf("block %s not found in SQLite: %w", blockID, err)
	}
	notebook, section, page, blockType := loc.Notebook, loc.Section, loc.Page, loc.BlockType

	if blockType != string(parser.BlockTask) {
		return "", fmt.Errorf("block %s is not a task", blockID)
	}

	// Defense-in-depth against path traversal: notebook/section/page originate
	// from user-editable YAML frontmatter. Section may be empty (a page living
	// directly under its notebook).
	safeNotebook := sanitizePathSegment(notebook)
	safeSection := sanitizePathSegment(section)
	safePage := sanitizePathSegment(page)
	if safeNotebook == "" || safePage == "" {
		return "", fmt.Errorf("invalid file metadata for block %s: notebook=%q section=%q page=%q", blockID, notebook, section, page)
	}
	// Resolve the notebook content dir from the block's source (#100): vault
	// blocks live under <vault>/<notebook>, linked blocks under their root.
	notebookDir, err := a.resolveNotebookDir(safeNotebook, loc.Source)
	if err != nil {
		return "", fmt.Errorf("resolve notebook dir for block %s: %w", blockID, err)
	}
	filePath := filepath.Join(notebookDir, safeSection, safePage+".md")
	if !isPathWithinRoot(filePath, notebookDir) {
		return "", fmt.Errorf("resolved file path %q escapes notebook root %q", filePath, notebookDir)
	}

	var writeErr error
	var spawnedID string // UUID minted by the recurrence spawn inside the lock
	a.coordinator.LockBlockWrite(blockID, func() {
		a.coordinator.LockFileWrite(filePath, func() {
			// Don't clobber a file the user is actively editing (the editor
			// holds a focus lock on the file while focused). Mirrors MutateBlock
			// + mutateTaskBlock so every task-status write refuses consistently
			// (#444). The editor itself saves via SaveFileBlocks (whole-file),
			// not this per-block path, so this never blocks the editor's own
			// writes.
			if a.watcher != nil && a.watcher.IsFocusLocked(filePath) {
				writeErr = blockBeingEditedError()
				return
			}
			contentBytes, err := os.ReadFile(filePath)
			if err != nil {
				writeErr = err
				return
			}

			// Parse the whole file, flip the target task's status in the parsed
			// slice, then re-render through the single serializer. This keeps
			// UpdateBlockState on the same write path as every other writer
			// (one on-disk format definition) and preserves unmanaged lines via
			// the original body.
			// Use the file's modification time as the default date for blocks
			// whose comment lacks a @ date suffix — matches the scanner's behavior.
			// Using time.Now() here would silently shift old blocks' dates to today.
			fileDate := fileOrDefaultDate(filePath)
			parsedBlocks, meta, _, _, parseErr := parser.ParseFileContent(string(contentBytes), safeNotebook, safeSection, safePage, fileDate, a.spacesPerTab)
			if parseErr != nil {
				writeErr = fmt.Errorf("failed to parse file for state update: %w", parseErr)
				return
			}
			found := false
			for i := range parsedBlocks {
				if parsedBlocks[i].ID == blockID {
					if parsedBlocks[i].Type != parser.BlockTask {
						writeErr = fmt.Errorf("block %s is not a task", blockID)
						return
					}
					wasDone := parsedBlocks[i].Status == "DONE"
					parsedBlocks[i].Status = newState
					// [modified::] stamp (#440): status change is a task-line touch.
					parsedBlocks[i].ModifiedAt = time.Now().Format("2006-01-02T15:04:05")
					// [completed::] lifecycle stamp (#417): set when entering
					// DONE from a non-DONE state, clear when leaving DONE
					// (reopen), overwrite on re-complete. The token carries
					// the time of the MOST RECENT DONE transition, matching
					// the recurrence-spawn path below (which also lands a
					// fresh [completed::] on the just-completed original).
					if newState == "DONE" && !wasDone {
						parsedBlocks[i].CompletedAt = time.Now().Format("2006-01-02T15:04:05")
					} else if newState != "DONE" && wasDone {
						parsedBlocks[i].CompletedAt = ""
					}
					// Recurring-task auto-recreation (#296): when a task with a
					// [recur::] token transitions TO DONE (not already DONE),
					// spawn the next incomplete instance directly below it in
					// the same parsed slice. The existing render → write →
					// re-index chain then persists both the completion and the
					// new instance atomically. The new block gets a fresh UUID
					// and an advanced [due::] date. The completed line's recur
					// token is stripped so: (a) re-marking DONE is idempotent,
					// (b) the badge doesn't render on completed history items,
					// and (c) the forward rule lives only on the active TODO.
					// A malformed recurrence rule is a no-op + log so the DONE
					// transition never fails because of recurrence.
					if newState == "DONE" && !wasDone && parsedBlocks[i].Recurrence != "" {
						if nb, ok := buildNextRecurrence(parsedBlocks[i], parsedBlocks, i); ok {
							parsedBlocks[i].Recurrence = ""
							parsedBlocks = insertBlockAfter(parsedBlocks, i, nb)
							spawnedID = nb.ID // surface the minted id to the caller (#812)
						}
					}
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
				today := time.Now().Format("2006-01-02")
				frontmatter = fmt.Sprintf("---\nnotebook: %s\nsection: %s\npage: %s\ndate: %s\ntags: []\n---\n", strconv.Quote(safeNotebook), strconv.Quote(safeSection), strconv.Quote(safePage), strconv.Quote(today))
				body = string(contentBytes)
			}

			newContent := parser.RenderFileContent(parsedBlocks, body, frontmatter, a.spacesPerTab)

			a.tracker.RegisterWrite(filePath)

			if err := parser.WriteFileAtomic(filePath, []byte(newContent)); err != nil {
				writeErr = err
				return
			}

			// Re-parse with the sanitized metadata so the re-indexed row
			// uses the same cleaned values that went into the file path.
			blocks, remeta, _, _, err := parser.ParseFileContent(newContent, meta.Notebook, meta.Section, meta.Page, meta.Date, a.spacesPerTab)
			if err == nil {
				var idxErr error
				a.coordinator.WithDBWrite(func() {
					idxErr = a.db.IndexFileBlocks(loc.Source, remeta.Notebook, remeta.Section, remeta.Page, blocks, remeta.Tags, remeta.Warnings...)
				})
				if idxErr != nil {
					log.Printf("UpdateBlockState: IndexFileBlocks failed for %s/%s/%s/%s: %v", remeta.Notebook, remeta.Section, remeta.Page, remeta.Date, idxErr)
				}
			}
		})
	}) // LockBlockWrite

	if writeErr != nil {
		// A write failure means the spawn (if any) never persisted, so do not
		// surface a spawned id for a non-durable block.
		return "", writeErr
	}
	a.emitBlockChanged(blockID, safeNotebook, safeSection, safePage, "")

	// Task dependency fan-out (#301): when a block transitions to DONE, every
	// task blocked-by it may flip its derived "blocked" state (the Kanban/Agenda
	// badge and the DONE-guard consult the live blocker statuses). Re-broadcast
	// block:changed for each dependent so their views re-query. No file write
	// happens for the dependents — only their cached derived state changes,
	// which the badge recomputes at query time. Only a DONE transition can
	// unblock a dependent; TODO/DOING transitions can't clear a blocker, so we
	// skip the fan-out for them.
	if newState == "DONE" {
		var dependents []string
		a.coordinator.WithDBRead(func() {
			dependents, _ = a.db.DependentsOf(blockID)
		})
		// Look up each dependent's location once so the event carries the
		// breadcrumb the frontend expects. A missing location (block deleted
		// between the write and this lookup, or a stale FK) is skipped —
		// broadcasting with an empty breadcrumb would hand the frontend a
		// malformed event it can't route, so wait for the next re-index to
		// reconcile.
		for _, depID := range dependents {
			if depID == blockID {
				continue
			}
			var depLoc db.BlockLocation
			if err := a.coordinator.WithDBReadResult(func() error {
				var e error
				depLoc, e = a.db.GetBlockLocation(depID)
				return e
			}); err != nil || depLoc.Notebook == "" {
				continue
			}
			a.emitBlockChanged(depID, sanitizePathSegment(depLoc.Notebook), sanitizePathSegment(depLoc.Section), sanitizePathSegment(depLoc.Page), "")
		}
	}
	return spawnedID, nil
}

// buildNextRecurrence computes the next instance of a recurring task from the
// just-completed block (#296). It parses the [recur::] rule, anchors on the
// block's DueDate (falling back to today when the due date is absent), and
// advances to the next strictly-future occurrence using skip-missed semantics
// (PLAN.md §1 — skip-missed, avoiding catch-up hell). The returned
// block is a fresh TODO copy with a new UUID and the advanced due date; it
// carries the same recurrence rule so the cycle continues. A malformed rule
// or unparseable due date returns ok=false so the caller can no-op + log
// without blocking the DONE transition.
//
// `all` and `completedIdx` are the full parsed slice and the completed block's
// index within it, used to compute the spawned block's ManualOrder (#417): its
// 1-based position among all TASK blocks (counting the about-to-be-inserted
// spawn). CreatedAt is stamped to now since the spawn is a genuinely-new task.
func buildNextRecurrence(completed parser.ParsedBlock, all []parser.ParsedBlock, completedIdx int) (parser.ParsedBlock, bool) {
	rule, err := recurrence.ParseRule(completed.Recurrence)
	if err != nil {
		log.Printf("recurrence: skipping auto-recreation for block %s: %v", completed.ID, err)
		return parser.ParsedBlock{}, false
	}
	base := time.Now()
	if completed.DueDate != "" {
		if d, err := time.Parse("2006-01-02", completed.DueDate); err == nil {
			base = d
		} else {
			log.Printf("recurrence: unparseable due date %q on block %s, falling back to today", completed.DueDate, completed.ID)
		}
	}
	next := rule.NextFutureInstance(base, time.Now())
	now := time.Now()
	today := now.Format("2006-01-02")
	// ManualOrder for the spawned block: count TASK blocks at or before the
	// insertion point (completedIdx+1 after insertBlockAfter lands the spawn
	// directly below the completed block). This is the spawn's 1-based
	// position among all TASK blocks in the file (#417).
	taskPos := 0
	for j := 0; j <= completedIdx; j++ {
		if all[j].Type == parser.BlockTask {
			taskPos++
		}
	}
	taskPos++ // the spawned block itself
	return parser.ParsedBlock{
		ID:          uuid.New().String(),
		ParentID:    completed.ParentID,
		Type:        parser.BlockTask,
		Depth:       completed.Depth,
		CleanText:   completed.CleanText,
		Status:      "TODO",
		Owner:       completed.Owner,
		StartDate:   completed.StartDate,
		DueDate:     recurrence.FormatDate(next),
		Priority:    completed.Priority,
		Pinned:      completed.Pinned,
		Progress:    0, // new instance starts fresh
		Recurrence:  completed.Recurrence,
		CreatedAt:   now.Format("2006-01-02T15:04:05"),
		ManualOrder: taskPos,
		LineNumber:  completed.LineNumber + 1, // rendered directly below
		FileDate:    today,
	}, true
}

// insertBlockAfter splices nb into blocks immediately after position i and
// returns the resulting slice. Used by the recurrence hook to land the new
// task instance on the line directly below the completed block so it renders
// in the right position via RenderFileContent (which serializes in slice
// order). Line numbers are assigned by the subsequent re-parse, so no manual
// bumping is needed here.
func insertBlockAfter(blocks []parser.ParsedBlock, i int, nb parser.ParsedBlock) []parser.ParsedBlock {
	out := make([]parser.ParsedBlock, 0, len(blocks)+1)
	out = append(out, blocks[:i+1]...)
	out = append(out, nb)
	out = append(out, blocks[i+1:]...)
	return out
}

// QueryTasks retrieves indexed items matching the active filters.
func (a *App) QueryTasks(filter parser.TaskQueryFilter) ([]parser.TaskResult, error) {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.db == nil {
		return nil, fmt.Errorf("vault database not loaded")
	}

	a.wg.Add(1)
	defer a.wg.Done()

	var res []parser.TaskResult
	var err error
	a.coordinator.WithDBRead(func() {
		res, err = a.db.QueryTasksWithFilters(filter)
	})

	return res, err
}

// emitBlockChanged broadcasts a block:changed event so live embeds/references
// refresh whenever a block is mutated through any code path.
func (a *App) emitBlockChanged(id, notebook, section, page, fileDate string) {
	a.emit(EventBlockChanged, parser.BlockChangedEvent{
		ID: id, Notebook: notebook, Section: section, Page: page, FileDate: fileDate,
	})
}

// ResolveBlockReference looks up a ((uuid)) reference, returning its content
// and location for hover previews and scroll-to-source navigation. Missing
// UUIDs return Exists=false (no error) so the UI can render a broken-link chip.
func (a *App) ResolveBlockReference(blockID string) (parser.BlockReference, error) {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	ref := parser.BlockReference{ID: blockID}
	if a.db == nil {
		return ref, fmt.Errorf("vault database not loaded")
	}
	a.wg.Add(1)
	defer a.wg.Done()

	var out parser.BlockReference
	err := a.coordinator.WithDBReadResult(func() error {
		// Lease-aware typed lookup — no raw SQLDB() across vault teardown.
		got, err := a.db.GetBlockReference(blockID)
		if err != nil {
			return err
		}
		out = got
		return nil
	})
	if err != nil {
		return ref, err
	}
	return out, nil
}

// MutateBlock rewrites the body text of a block (identified by UUID) in its
// source file, preserving the leading task/header/bullet syntax and the
// trailing <!-- id --> comment. It re-indexes the file and emits block:changed
// so live embeds/references stay in sync.
func (a *App) MutateBlock(blockID, newText string) error {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.db == nil {
		return fmt.Errorf("vault database not loaded")
	}
	// Block text is single-line; collapse any newlines to spaces.
	cleanText := strings.ReplaceAll(newText, "\n", " ")

	a.wg.Add(1)
	defer a.wg.Done()

	return a.writeBlockText(blockID, func(_ string) (string, error) {
		return cleanText, nil
	})
}

// writeBlockText rewrites a block's clean text through the canonical
// lock→parse→render→atomic-write→reindex→emit chain shared by every programmatic
// block-body edit. transform receives the block's current CleanText as parsed
// from disk INSIDE the write locks (so it cannot race a concurrent writer) and
// returns the new CleanText. The caller MUST hold vaultMu (at least RLock) and
// have incremented the waitgroup, matching MutateBlock's entry contract.
//
// Extracted from MutateBlock so PromoteUnlinkedMention edits a source block on
// the identical write path as a user edit (one on-disk format definition, one
// reindex path, one block:changed emission). MutateBlock passes a constant
// transform; PromoteUnlinkedMention passes the link-wrapping transform.
func (a *App) writeBlockText(blockID string, transform func(currentClean string) (string, error)) error {
	var loc db.BlockLocation
	err := a.coordinator.WithDBReadResult(func() error {
		var e error
		loc, e = a.db.GetBlockLocation(blockID)
		return e
	})
	if err != nil {
		return fmt.Errorf("block %s not found in SQLite: %w", blockID, err)
	}
	notebook, section, page := loc.Notebook, loc.Section, loc.Page

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
	a.coordinator.LockBlockWrite(blockID, func() {
		a.coordinator.LockFileWrite(filePath, func() {
			// Don't clobber a block the user is actively editing in another view
			// (the timeline editor holds a focus lock on the file while focused).
			// Refuse rather than silently overwrite; callers (e.g. EmbedPortal)
			// retry once the editor releases the lock.
			if a.watcher != nil && a.watcher.IsFocusLocked(filePath) {
				writeErr = blockBeingEditedError()
				return
			}
			// After waiting for structural locks, refuse a renamed-away path
			// with the same typed sentinel as SaveFileBlocks (#691). Fail closed
			// on any Stat error — do not write blindly.
			if _, err := os.Stat(filePath); err != nil {
				if os.IsNotExist(err) {
					writeErr = errPageMovedOrDeleted(filePath)
				} else {
					writeErr = err
				}
				return
			}
			contentBytes, err := os.ReadFile(filePath)
			if err != nil {
				writeErr = err
				return
			}

			// Parse the whole file, mutate the target block in the slice, then
			// re-render through the single serializer (RenderFileContent). This
			// preserves unmanaged lines (code fences, prose) via the original
			// body and keeps every writer on the same write path, so there is
			// one on-disk format definition. Use the file's modification time as
			// the default date for blocks whose comment lacks a @ date suffix —
			// matches the scanner's behavior.
			fileDate := fileOrDefaultDate(filePath)
			parsedBlocks, meta, _, _, parseErr := parser.ParseFileContent(string(contentBytes), safeNotebook, safeSection, safePage, fileDate, a.spacesPerTab)
			if parseErr != nil {
				writeErr = fmt.Errorf("failed to parse file for mutation: %w", parseErr)
				return
			}
			found := false
			for i := range parsedBlocks {
				if parsedBlocks[i].ID == blockID {
					newClean, terr := transform(parsedBlocks[i].CleanText)
					if terr != nil {
						writeErr = terr
						return
					}
					parsedBlocks[i].CleanText = newClean
					// [modified::] stamp (#440): body edit on a TASK line.
					if parsedBlocks[i].Type == parser.BlockTask {
						parsedBlocks[i].ModifiedAt = time.Now().Format("2006-01-02T15:04:05")
					}
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
				frontmatter = fmt.Sprintf("---\nnotebook: %s\nsection: %s\npage: %s\ndate: %s\ntags: []\n---\n", strconv.Quote(safeNotebook), strconv.Quote(safeSection), strconv.Quote(safePage), strconv.Quote(time.Now().Format("2006-01-02")))
			}

			newContent := parser.RenderFileContent(parsedBlocks, body, frontmatter, a.spacesPerTab)

			a.tracker.RegisterWrite(filePath)
			if err := parser.WriteFileAtomic(filePath, []byte(newContent)); err != nil {
				writeErr = err
				return
			}

			// Re-parse the rendered output and reindex so the cache reflects the
			// canonical on-disk state (RenderFileContent may have normalized the
			// mutated line's format).
			reblocks, remeta, _, _, err := parser.ParseFileContent(newContent, meta.Notebook, meta.Section, meta.Page, meta.Date, a.spacesPerTab)
			if err == nil {
				var idxErr error
				a.coordinator.WithDBWrite(func() {
					idxErr = a.db.IndexFileBlocks(loc.Source, remeta.Notebook, remeta.Section, remeta.Page, reblocks, remeta.Tags, remeta.Warnings...)
				})
				if idxErr != nil {
					log.Printf("writeBlockText: IndexFileBlocks failed for %s/%s/%s/%s: %v", remeta.Notebook, remeta.Section, remeta.Page, remeta.Date, idxErr)
				}
			}
		})
	}) // LockBlockWrite
	if writeErr != nil {
		return writeErr
	}

	a.emitBlockChanged(blockID, safeNotebook, safeSection, safePage, "")
	return nil
}

// ErrPageMovedOrDeleted is returned when a save acquires the file lock but the
// path no longer exists (rename/delete won the race). Prefix is stable for FE.
var ErrPageMovedOrDeleted = errors.New("page_moved")

func errPageMovedOrDeleted(filePath string) error {
	return fmt.Errorf("%w: page file no longer exists (moved or deleted): %s", ErrPageMovedOrDeleted, filePath)
}

// SaveFileBlocks writes the updated list of blocks back to the page file.
// With the per-day file model removed, a page is a single file. Each block
// carries its own file_date. The notebook's source is resolved server-side
// from its (globally-unique) name (#100).
// writePageFileLocked reads the existing file content, renders the new block
// list through the single serializer (preserving unmanaged lines), writes
// atomically, and re-indexes in SQLite. The caller MUST already hold
// LockFileWrite for filePath — this method does NOT acquire the per-file lock
// (it would deadlock against a re-entrant LockFileWrite on the same path).
// Extracted from SaveFileBlocks so the cross-page source-removal path in
// applyBlocksOps can do an atomic read-parse-filter-write under a single
// LockFileWrite scope (#104 TOCTOU fix).
func (a *App) writePageFileLocked(filePath, source, notebook, section, page string, blocks []parser.ParsedBlock) error {
	contentBytes, err := os.ReadFile(filePath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to read existing file: %w", err)
	}

	frontmatter, body := parser.SplitFrontmatter(string(contentBytes))

	if frontmatter == "" {
		today := time.Now().Format("2006-01-02")
		frontmatter = fmt.Sprintf("---\nnotebook: %s\nsection: %s\npage: %s\ndate: %s\ntags: []\n---\n", strconv.Quote(notebook), strconv.Quote(section), strconv.Quote(page), strconv.Quote(today))
		body = string(contentBytes)
	}

	newContent := parser.RenderFileContent(blocks, body, frontmatter, a.spacesPerTab)

	a.tracker.RegisterWrite(filePath)

	if err := parser.WriteFileAtomic(filePath, []byte(newContent)); err != nil {
		return err
	}

	parsedBlocks, meta, _, _, err := parser.ParseFileContent(newContent, notebook, section, page, fileOrDefaultDate(filePath), a.spacesPerTab)
	if err == nil {
		var idxErr error
		a.coordinator.WithDBWrite(func() {
			idxErr = a.db.IndexFileBlocks(source, meta.Notebook, meta.Section, meta.Page, parsedBlocks, meta.Tags, meta.Warnings...)
		})
		if idxErr != nil {
			log.Printf("writePageFileLocked: IndexFileBlocks failed for %s/%s/%s: %v", meta.Notebook, meta.Section, meta.Page, idxErr)
		}
	}
	return nil
}
func (a *App) SaveFileBlocks(notebook, section, page string, blocks []parser.ParsedBlock) error {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.db == nil {
		return fmt.Errorf("vault database not loaded")
	}

	safeNotebook := sanitizePathSegment(notebook)
	safeSection := sanitizePathSegment(section)
	safePage := sanitizePathSegment(page)
	if safeNotebook == "" || safePage == "" {
		return fmt.Errorf("invalid path metadata")
	}

	source := a.resolveSourceByName(safeNotebook)
	notebookDir, err := a.resolveNotebookDir(safeNotebook, source)
	if err != nil {
		return fmt.Errorf("resolve notebook dir: %w", err)
	}
	filePath := filepath.Join(notebookDir, safeSection, safePage+".md")
	if !isPathWithinRoot(filePath, notebookDir) {
		return fmt.Errorf("path escapes notebook root")
	}

	a.wg.Add(1)
	defer a.wg.Done()

	// Extract block IDs for per-block write-intent locking (#64). This
	// serializes the full-page save against any concurrent MutateBlock for
	// the same block, preventing last-writer-wins clobbering.
	blockIDs := make([]string, 0, len(blocks))
	for _, b := range blocks {
		if b.ID != "" {
			blockIDs = append(blockIDs, b.ID)
		}
	}

	// Fetch the page's current block IDs so that, after the save, we can
	// release the per-block mutex entries for blocks that were dropped or
	// replaced (#122). Block IDs are page-scoped, so any ID present before
	// but absent from the new set no longer exists and will never be mutated
	// again.
	var beforeIDs []string
	a.coordinator.WithDBRead(func() {
		beforeIDs, _ = a.db.BlockIDsForPage(source, safeNotebook, safeSection, safePage)
	})

	var writeErr error
	a.coordinator.LockBlocksWrite(blockIDs, func() {
		a.coordinator.LockFileWrite(filePath, func() {
			// After waiting for structural locks, refuse to recreate a path
			// that rename/delete already moved away (#691). Fail closed on any
			// Stat error (permission/transient) — do not write blindly.
			if _, err := os.Stat(filePath); err != nil {
				if os.IsNotExist(err) {
					writeErr = errPageMovedOrDeleted(filePath)
				} else {
					writeErr = err
				}
				return
			}
			writeErr = a.writePageFileLocked(filePath, source, safeNotebook, safeSection, safePage, blocks)
		})
	}) // LockBlocksWrite

	if writeErr != nil {
		return writeErr
	}
	// Release the per-block mutex entries for blocks that were present before
	// but are absent from the saved set — they were deleted/replaced and will
	// never be mutated again. Bounds blockMu growth (#122).
	newIDSet := make(map[string]bool, len(blockIDs))
	for _, id := range blockIDs {
		newIDSet[id] = true
	}
	var removed []string
	for _, id := range beforeIDs {
		if id != "" && !newIDSet[id] {
			removed = append(removed, id)
		}
	}
	a.coordinator.ReleaseBlockMutexes(removed)
	// Notify live embeds/references that the saved blocks changed.
	for _, b := range blocks {
		if b.ID != "" {
			a.emitBlockChanged(b.ID, safeNotebook, safeSection, safePage, b.FileDate)
		}
	}
	return nil
}

// FetchPageMarkdown returns the on-disk markdown *body* for a page (no YAML
// frontmatter). Used to seed editable Source mode so multi-line regions and
// unmanaged prose match the file, not a reconstruct-from-blocks projection.
func (a *App) FetchPageMarkdown(notebook, section, page string) (string, error) {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.db == nil {
		return "", fmt.Errorf("vault database not loaded")
	}
	safeNotebook := sanitizePathSegment(notebook)
	safeSection := sanitizePathSegment(section)
	safePage := sanitizePathSegment(page)
	if safeNotebook == "" || safePage == "" {
		return "", fmt.Errorf("invalid path metadata")
	}
	source := a.resolveSourceByName(safeNotebook)
	notebookDir, err := a.resolveNotebookDir(safeNotebook, source)
	if err != nil {
		return "", fmt.Errorf("resolve notebook dir: %w", err)
	}
	filePath := filepath.Join(notebookDir, safeSection, safePage+".md")
	if !isPathWithinRoot(filePath, notebookDir) {
		return "", fmt.Errorf("path escapes notebook root")
	}
	contentBytes, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("read page markdown: %w", err)
	}
	_, body := parser.SplitFrontmatter(string(contentBytes))
	return body, nil
}

// SavePageMarkdown writes raw markdown body for a page (editable Source mode
// #660). Preserves YAML frontmatter, uses the atomic write chain (same as
// SaveFileBlocks), re-indexes, and returns the re-parsed block list so the
// frontend can refresh without a second round-trip.
//
// Per-block write-intent locking mirrors SaveFileBlocks: we lock every block
// currently on the page so a concurrent MutateBlock cannot interleave mid-
// rewrite (no torn file / partial index). The body argument is authoritative —
// SavePageMarkdown does not merge concurrent MutateBlock text into the buffer;
// Source-mode conflict UI + focus lease own that product decision.
func (a *App) SavePageMarkdown(notebook, section, page, markdown string) ([]parser.ParsedBlock, error) {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.db == nil {
		return nil, fmt.Errorf("vault database not loaded")
	}

	safeNotebook := sanitizePathSegment(notebook)
	safeSection := sanitizePathSegment(section)
	safePage := sanitizePathSegment(page)
	if safeNotebook == "" || safePage == "" {
		return nil, fmt.Errorf("invalid path metadata")
	}

	source := a.resolveSourceByName(safeNotebook)
	notebookDir, err := a.resolveNotebookDir(safeNotebook, source)
	if err != nil {
		return nil, fmt.Errorf("resolve notebook dir: %w", err)
	}
	filePath := filepath.Join(notebookDir, safeSection, safePage+".md")
	if !isPathWithinRoot(filePath, notebookDir) {
		return nil, fmt.Errorf("path escapes notebook root")
	}

	a.wg.Add(1)
	defer a.wg.Done()

	// Before-set: serialize against MutateBlock on any existing page block.
	var beforeIDs []string
	a.coordinator.WithDBRead(func() {
		beforeIDs, _ = a.db.BlockIDsForPage(source, safeNotebook, safeSection, safePage)
	})

	var result []parser.ParsedBlock
	var writeErr error
	a.coordinator.LockBlocksWrite(beforeIDs, func() {
		a.coordinator.LockFileWrite(filePath, func() {
			// After waiting for structural locks, refuse to recreate a path
			// that rename/delete already moved away (#691). Fail closed on any
			// Stat error (permission/transient) — do not write blindly.
			if _, err := os.Stat(filePath); err != nil {
				if os.IsNotExist(err) {
					writeErr = errPageMovedOrDeleted(filePath)
				} else {
					writeErr = err
				}
				return
			}
			contentBytes, err := os.ReadFile(filePath)
			if err != nil && !os.IsNotExist(err) {
				writeErr = fmt.Errorf("failed to read existing file: %w", err)
				return
			}
			frontmatter, _ := parser.SplitFrontmatter(string(contentBytes))
			if frontmatter == "" {
				// Match writePageFileLocked: quote the display names (not only
				// sanitized path segments) so frontmatter stays user-facing.
				today := time.Now().Format("2006-01-02")
				frontmatter = fmt.Sprintf("---\nnotebook: %s\nsection: %s\npage: %s\ndate: %s\ntags: []\n---\n",
					strconv.Quote(notebook), strconv.Quote(section), strconv.Quote(page), strconv.Quote(today))
			}

			// Body is the user-edited source. Normalize to end with a single newline.
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
			if err := parser.WriteFileAtomic(filePath, []byte(newContent)); err != nil {
				writeErr = err
				return
			}

			parsedBlocks, meta, _, _, parseErr := parser.ParseFileContent(
				newContent, safeNotebook, safeSection, safePage, fileOrDefaultDate(filePath), a.spacesPerTab,
			)
			if parseErr != nil {
				writeErr = fmt.Errorf("parse after source save: %w", parseErr)
				return
			}
			var idxErr error
			a.coordinator.WithDBWrite(func() {
				idxErr = a.db.IndexFileBlocks(source, meta.Notebook, meta.Section, meta.Page, parsedBlocks, meta.Tags, meta.Warnings...)
			})
			if idxErr != nil {
				// Fail loud: disk write already landed, but search/graph would lag.
				// Surface so the UI does not claim a fully successful save.
				writeErr = fmt.Errorf("re-index after source save failed: %w", idxErr)
				return
			}
			result = parsedBlocks
		})
	}) // LockBlocksWrite

	if writeErr != nil {
		return nil, writeErr
	}

	// Release mutexes for blocks dropped by the source rewrite (#122).
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

	// Emit after locks release so subscribers (embeds) re-fetch without re-entry
	// into the file write lock (matches SaveFileBlocks).
	for _, b := range result {
		if b.ID != "" {
			a.emitBlockChanged(b.ID, safeNotebook, safeSection, safePage, b.FileDate)
		}
	}
	return result, nil
}
