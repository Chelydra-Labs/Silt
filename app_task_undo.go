package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"silt/backend/parser"
	"silt/backend/plugins"
)

// taskUndoJournalCap bounds the session undo journal. The ring is deliberately
// shallow: undo is an "oops" affordance for the current working session, not a
// durable history (page history owns that). 100 mutations comfortably covers a
// board drag session or a metadata-editing pass while keeping memory bounded
// (each entry holds two ParsedBlock snapshots per touched task).
const taskUndoJournalCap = 100

// taskUndoBlock captures one block's full parsed state on both sides of a
// successful write. A whole snapshot (rather than just CleanText) is required
// because the task setters mutate typed fields — status, due date, owner,
// order — that live outside the prose. Restoring only the prose would make
// status/due-date undo a no-op on disk.
type taskUndoBlock struct {
	BlockID string
	Before  parser.ParsedBlock
	After   parser.ParsedBlock
}

// taskUndoEntry is one successful page write. A batch reorder touches several
// blocks in a single file write, so entries carry a slice; single-block
// mutations carry exactly one. SpawnedID is set only when a recurring task's
// TODO/DOING → DONE transition minted a sibling in the same write — undoing
// that transition must remove the sibling too.
type taskUndoEntry struct {
	Source    string
	Notebook  string
	Section   string
	Page      string
	Blocks    []taskUndoBlock
	SpawnedID string
	// ChangedFields is derived at capture time so the frontend can describe
	// what an undo restored without re-deriving it from before/after state.
	ChangedFields []string
}

// taskUndoJournal is the in-memory, session-scoped undo stack. The mutex is
// held across a whole undo replay so a concurrent capture cannot reorder the
// stack between the peek and the pop.
type taskUndoJournal struct {
	mu      sync.Mutex
	entries []taskUndoEntry
}

func (j *taskUndoJournal) record(entry taskUndoEntry) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.entries = append(j.entries, entry)
	if len(j.entries) > taskUndoJournalCap {
		// Drop the oldest entry: the cap bounds session memory, and the
		// dropped mutation simply stops being undoable.
		j.entries = append([]taskUndoEntry(nil), j.entries[len(j.entries)-taskUndoJournalCap:]...)
	}
}

func (j *taskUndoJournal) clear() {
	j.mu.Lock()
	j.entries = nil
	j.mu.Unlock()
}

// recordTaskUndo captures a successful write for undo. Callers invoke this
// only after trackedPageWrite returned nil — focus-lock refusals, validation
// failures, and write errors must never become undoable.
func (a *App) recordTaskUndo(entry taskUndoEntry) {
	entry.ChangedFields = changedTaskFields(entry.Blocks)
	a.taskUndo.record(entry)
}

// changedTaskFields returns the frontend-facing names of the task fields that
// differ across the entry's blocks. ModifiedAt is intentionally excluded: every
// write (and the undo itself) re-stamps it, so it carries no user intent.
func changedTaskFields(blocks []taskUndoBlock) []string {
	var fields []string
	seen := make(map[string]bool)
	add := func(name string) {
		if !seen[name] {
			seen[name] = true
			fields = append(fields, name)
		}
	}
	for _, b := range blocks {
		before, after := b.Before, b.After
		if before.CleanText != after.CleanText {
			add("text")
		}
		if before.Status != after.Status {
			add("status")
		}
		if before.CompletedAt != after.CompletedAt {
			add("completed")
		}
		if before.Owner != after.Owner {
			add("owner")
		}
		if before.StartDate != after.StartDate {
			add("startDate")
		}
		if before.DueDate != after.DueDate {
			add("dueDate")
		}
		if before.Priority != after.Priority {
			add("priority")
		}
		if !equalBoolPtr(before.Pinned, after.Pinned) {
			add("pinned")
		}
		if before.Progress != after.Progress {
			add("progress")
		}
		if before.Recurrence != after.Recurrence {
			add("recurrence")
		}
		if !taskUndoStringSlicesEqual(before.BlockedBy, after.BlockedBy) {
			add("blockedBy")
		}
		if before.Estimate != after.Estimate {
			add("estimate")
		}
		if before.ManualOrder != after.ManualOrder {
			add("order")
		}
	}
	return fields
}

func equalBoolPtr(a, b *bool) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func taskUndoStringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TaskUndoResult is the IPC shape returned by UndoLastTaskMutation. Undone is
// false for a clean no-op (empty journal) and for a skipped entry whose target
// blocks vanished; SkippedBlockIDs names those blocks. ChangedFields lists the
// task attributes the undo restored ("status", "dueDate", …).
type TaskUndoResult struct {
	Undone           bool     `json:"undone"`
	Skipped          bool     `json:"skipped"`
	BlockIDs         []string `json:"blockIds"`
	SkippedBlockIDs  []string `json:"skippedBlockIds,omitempty"`
	Notebook         string   `json:"notebook,omitempty"`
	Section          string   `json:"section,omitempty"`
	Page             string   `json:"page,omitempty"`
	RemovedSpawnedID string   `json:"removedSpawnedId,omitempty"`
	ChangedFields    []string `json:"changedFields,omitempty"`
	Remaining        int      `json:"remaining"`
}

// UndoLastTaskMutation restores the newest journaled task mutation. The replay
// is block-addressed: it re-parses the recorded page inside the same
// lock/focus-gate path as every writer, restores each target block's typed
// fields from the before snapshot, removes the recurrence sibling when the
// undone transition minted one, then renders/writes/re-indexes once. Adjacent
// blocks are copied through the renderer untouched.
//
// The undo write itself is deliberately NOT journaled (there is no redo); the
// journal mutex stays held across the replay so a concurrent mutation cannot
// slip between the peek and the pop. On a write refusal (focus lock, disk
// error) the entry stays journaled; a vanished target is popped and reported
// as Skipped so a stale UI invocation doesn't error-storm.
func (a *App) UndoLastTaskMutation() (TaskUndoResult, error) {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.db == nil {
		return TaskUndoResult{}, fmt.Errorf("vault database not loaded")
	}
	a.wg.Add(1)
	defer a.wg.Done()

	a.taskUndo.mu.Lock()
	defer a.taskUndo.mu.Unlock()
	n := len(a.taskUndo.entries)
	if n == 0 {
		return TaskUndoResult{Remaining: 0}, nil
	}
	entry := a.taskUndo.entries[n-1]
	result, err := a.replayTaskUndoEntry(entry)
	if err != nil {
		return TaskUndoResult{Remaining: n}, err
	}
	a.taskUndo.entries = a.taskUndo.entries[:n-1]
	result.Remaining = n - 1
	return result, nil
}

// PluginUndoLastTaskMutation is the plugin-SDK wrapper for
// UndoLastTaskMutation, gated by the standard capability + session checks.
// Undo mutates page files, so it needs the same content-mutate grant as the
// setters whose mutations it reverses.
func (a *App) PluginUndoLastTaskMutation(pluginID, sessionToken string) (TaskUndoResult, error) {
	if err := a.validatePluginSession(pluginID, sessionToken); err != nil {
		return TaskUndoResult{}, err
	}
	if err := a.requireGrant(pluginID, plugins.CapContentMutate); err != nil {
		return TaskUndoResult{}, err
	}
	return a.UndoLastTaskMutation()
}

// replayTaskUndoEntry performs the canonical lock → focus-gate → parse →
// restore → render → history capture → tracked write → re-index → emit chain
// for one journal entry. It returns a Skipped result (nil error) when none of
// the entry's blocks still exist on disk.
func (a *App) replayTaskUndoEntry(entry taskUndoEntry) (TaskUndoResult, error) {
	result := TaskUndoResult{
		Notebook:      entry.Notebook,
		Section:       entry.Section,
		Page:          entry.Page,
		ChangedFields: entry.ChangedFields,
	}
	if len(entry.Blocks) == 0 {
		result.Skipped = true
		return result, nil
	}

	safeNotebook := sanitizePathSegment(entry.Notebook)
	safeSection, secErr := historySection(entry.Section)
	if secErr != nil {
		return result, invalidNavigationPath(secErr)
	}
	safePage := sanitizePathSegment(entry.Page)
	if safeNotebook == "" || safePage == "" {
		return result, fmt.Errorf("task undo: invalid recorded locator %s/%s/%s", entry.Notebook, entry.Section, entry.Page)
	}
	notebookDir, err := a.resolveNotebookDir(safeNotebook, entry.Source)
	if err != nil {
		return result, fmt.Errorf("task undo: resolve notebook dir: %w", err)
	}
	filePath := filepath.Join(notebookDir, safeSection, safePage+".md")
	if !isPathWithinRoot(filePath, notebookDir) {
		return result, fmt.Errorf("task undo: resolved file %q escapes notebook root", filePath)
	}

	primary := entry.Blocks[0].BlockID
	var writeErr error
	var restoredIDs, missingIDs []string
	var removedSpawned string
	didWrite := false

	a.coordinator.LockBlockWrite(primary, func() {
		a.coordinator.LockFileWrite(filePath, func() {
			// Same focus gate as every task setter: never clobber a file the
			// editor currently owns. The entry stays journaled for a later retry.
			if a.watcher != nil && a.watcher.IsFocusLocked(filePath) {
				writeErr = blockBeingEditedError()
				return
			}
			contentBytes, err := os.ReadFile(filePath)
			if err != nil {
				if os.IsNotExist(err) {
					// The page (and therefore every recorded block) is gone.
					return
				}
				writeErr = err
				return
			}
			fileDate := fileOrDefaultDate(filePath)
			parsedBlocks, meta, _, _, parseErr := parser.ParseFileContent(string(contentBytes), safeNotebook, safeSection, safePage, fileDate, a.spacesPerTab)
			if parseErr != nil {
				writeErr = fmt.Errorf("task undo: parse %s: %w", filePath, parseErr)
				return
			}
			indexByID := make(map[string]int, len(parsedBlocks))
			for i := range parsedBlocks {
				indexByID[parsedBlocks[i].ID] = i
			}
			for _, undo := range entry.Blocks {
				i, ok := indexByID[undo.BlockID]
				if !ok {
					missingIDs = append(missingIDs, undo.BlockID)
					continue
				}
				applyTaskUndoSnapshot(&parsedBlocks[i], undo.Before)
				// Undo is a real mutation; re-stamping [modified::] is accepted
				// (suppressing it would fork the write chain's stamp rule).
				parsedBlocks[i].ModifiedAt = time.Now().Format("2006-01-02T15:04:05")
				restoredIDs = append(restoredIDs, undo.BlockID)
			}
			if len(restoredIDs) == 0 {
				return // every target vanished → skip without a write
			}
			if entry.SpawnedID != "" {
				if i, ok := indexByID[entry.SpawnedID]; ok {
					parsedBlocks = append(parsedBlocks[:i], parsedBlocks[i+1:]...)
					removedSpawned = entry.SpawnedID
				}
			}

			frontmatter, body := parser.SplitFrontmatter(string(contentBytes))
			if frontmatter == "" {
				fmDate := meta.Date
				if fmDate == "" {
					fmDate = fileDate
				}
				frontmatter = fmt.Sprintf("---\nnotebook: %s\nsection: %s\npage: %s\ndate: %s\ntags: []\n---\n", strconv.Quote(safeNotebook), strconv.Quote(safeSection), strconv.Quote(safePage), strconv.Quote(fmDate))
			}
			newContent := parser.RenderFileContent(parsedBlocks, body, frontmatter, a.spacesPerTab)
			a.maybeCapturePageVersion(historyLoc(entry.Source, safeNotebook, safeSection, safePage), contentBytes, []byte(newContent), historyReasonUndo)
			if err := a.trackedPageWrite(filePath, []byte(newContent)); err != nil {
				writeErr = err
				return
			}
			didWrite = true

			blocks, remeta, _, _, err := parser.ParseFileContent(newContent, meta.Notebook, meta.Section, meta.Page, meta.Date, a.spacesPerTab)
			if err == nil {
				if idxErr := a.indexFile(entry.Source, remeta.Notebook, remeta.Section, remeta.Page, blocks, remeta, remeta.Warnings...); idxErr != nil {
					log.Printf("task undo: indexFile failed for %s/%s/%s: %v", remeta.Notebook, remeta.Section, remeta.Page, idxErr)
				}
			} else {
				log.Printf("task undo: re-parse failed (file written, index stale until next scan): %v", err)
			}
		})
	})
	if writeErr != nil {
		return result, writeErr
	}
	if !didWrite {
		result.Skipped = true
		if len(missingIDs) == 0 {
			for _, b := range entry.Blocks {
				missingIDs = append(missingIDs, b.BlockID)
			}
		}
		result.SkippedBlockIDs = missingIDs
		return result, nil
	}

	result.Undone = true
	result.BlockIDs = restoredIDs
	result.SkippedBlockIDs = missingIDs
	result.RemovedSpawnedID = removedSpawned
	for _, id := range restoredIDs {
		a.emitBlockChanged(id, safeNotebook, safeSection, safePage, "")
	}
	if removedSpawned != "" {
		a.emitBlockChanged(removedSpawned, safeNotebook, safeSection, safePage, "")
	}
	return result, nil
}

// applyTaskUndoSnapshot copies the task-mutable fields from a before snapshot
// onto the freshly parsed target. Identity/structure fields (ID, ParentID,
// Depth, LineNumber, FileDate) are deliberately left as parsed so undo cannot
// relocate a block or break nesting.
func applyTaskUndoSnapshot(dst *parser.ParsedBlock, snap parser.ParsedBlock) {
	dst.CleanText = snap.CleanText
	dst.Status = snap.Status
	dst.Owner = snap.Owner
	dst.StartDate = snap.StartDate
	dst.DueDate = snap.DueDate
	dst.Priority = snap.Priority
	dst.Pinned = cloneBoolPtr(snap.Pinned)
	dst.Progress = snap.Progress
	dst.Recurrence = snap.Recurrence
	dst.BlockedBy = append([]string(nil), snap.BlockedBy...)
	dst.Estimate = snap.Estimate
	dst.CompletedAt = snap.CompletedAt
	dst.ManualOrder = snap.ManualOrder
}

func cloneBoolPtr(p *bool) *bool {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}
