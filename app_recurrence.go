package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"silt/backend/db"
	"silt/backend/parser"
	"silt/backend/recurrence"
	"strconv"
	"time"
)

// SetTaskRecurrence rewrites the [recur::] token on a task block (#297).
// An empty recurrence string clears the token (the "stop recurring" path);
// a non-empty value must parse as a valid recurrence rule and the block must
// have a [due::] anchor — recurrence without a due date is meaningless and
// rejected up front so the resolver never anchors on an implicit today at
// completion time (PLAN.md §1 validation decision).
//
// The method follows the same canonical write chain as PluginSetTaskDueDate:
// LockBlockWrite -> LockFileWrite -> ReadFile -> ParseFileContent -> mutate
// block.Recurrence -> RenderFileContent -> WriteFileAtomic -> re-parse ->
// IndexFileBlocks -> emit block:changed.
func (a *App) SetTaskRecurrence(blockID, recurrenceRule string) error {
	return a.setTaskRecurrence(blockID, recurrenceRule)
}

// PluginSetTaskRecurrence is the plugin-SDK wrapper for SetTaskRecurrence,
// gated by the standard capability + session checks (#297, SPECS §8.3 —
// plugins go through PluginContext, never direct wailsjs bindings).
func (a *App) PluginSetTaskRecurrence(pluginID, sessionToken, blockID, recurrenceRule string) (bool, error) {
	return a.wrapPluginMutate(pluginID, sessionToken, func() error {
		return a.setTaskRecurrence(blockID, recurrenceRule)
	})
}

// setTaskRecurrence is the shared core for the app-level and plugin-level
// entry points. It is the direct analog of PluginSetTaskDueDate's body but
// for the Recurrence field, with added grammar + due-date validation.
func (a *App) setTaskRecurrence(blockID, recurrenceRule string) error {
	// "" clears the token; a non-empty value must be valid recurrence grammar.
	if recurrenceRule != "" {
		if !recurrence.IsValid(recurrenceRule) {
			return fmt.Errorf("invalid recurrence rule %q (supported: every day|weekday|week|month|year, every N days|weeks|months|years)", recurrenceRule)
		}
	}

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
			// Focus-lock guard — mirrors mutateTaskBlock/MutateBlock so a
			// recurrence-rule write can't clobber an in-flight editor edit (#444).
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
				writeErr = fmt.Errorf("failed to parse file for recurrence update: %w", parseErr)
				return
			}
			found := false
			for i := range parsedBlocks {
				if parsedBlocks[i].ID == blockID && parsedBlocks[i].Type == parser.BlockTask {
					// Reject setting a recurrence rule on a task with no due
					// date — the resolver anchors on the due date and a
					// missing anchor would silently use today, producing
					// surprising next-instance dates (#297 validation).
					if recurrenceRule != "" && parsedBlocks[i].DueDate == "" {
						writeErr = fmt.Errorf("cannot set recurrence on block %s: task has no [due::] date (set a due date first)", blockID)
						return
					}
					// Defense-in-depth: reject a recurrence set on a task with
					// an unparseable due date (shouldn't happen — normalizeDate
					// cleans it — but guards against direct markdown edits).
					if recurrenceRule != "" && parsedBlocks[i].DueDate != "" {
						if _, derr := time.Parse("2006-01-02", parsedBlocks[i].DueDate); derr != nil {
							writeErr = fmt.Errorf("cannot set recurrence on block %s: due date %q is not a valid YYYY-MM-DD", blockID, parsedBlocks[i].DueDate)
							return
						}
					}
					parsedBlocks[i].Recurrence = recurrenceRule
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

			blocks, remeta, _, _, err := parser.ParseFileContent(newContent, meta.Notebook, meta.Section, meta.Page, meta.Date, a.spacesPerTab)
			if err == nil {
				var idxErr error
				a.coordinator.WithDBWrite(func() {
					idxErr = a.db.IndexFileBlocks(loc.Source, remeta.Notebook, remeta.Section, remeta.Page, blocks, remeta.Tags, remeta.Warnings...)
				})
				if idxErr != nil {
					log.Printf("SetTaskRecurrence: IndexFileBlocks failed: %v", idxErr)
				}
				a.markFileIndexedBestEffort(filePath)
				for _, b := range blocks {
					if b.ID == blockID {
						emitFileDate = b.FileDate
					}
				}
			} else {
				log.Printf("SetTaskRecurrence: re-parse of rendered content failed (file written, index stale until next scan): %v", err)
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
