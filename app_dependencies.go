package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"silt/backend/db"
	"silt/backend/dependencies"
	"silt/backend/parser"
	"silt/backend/plugins"
)

// ErrTaskCycle is returned by SetTaskBlockedBy when adding the proposed edge
// would close a circular dependency (A→B→A). The frontend surfaces it inline
// on the dependency picker rather than as a generic error toast (#303).
var ErrTaskCycle = errors.New("adding this dependency would create a circular dependency")

// ErrUnknownDependency is returned when a proposed prerequisite is not an
// indexed TASK block (deleted, typo'd UUID, or a non-task block). The frontend
// surfaces it inline on the dependency picker.
var ErrUnknownDependency = errors.New("dependency target is not an indexed task block")

// SetTaskBlockedBy rewrites the [blocked_by:: ((uuid))...] token on a task
// block, establishing its prerequisite set (#301/#303). An empty slice clears
// the token (the task becomes self-actionable); a non-empty slice lists the
// task IDs this task is blocked by. Cycle prevention runs before the write so
// a hand-fed cycle is impossible to persist.
//
// Follows the canonical write chain (same as SetTaskRecurrence):
// LockBlockWrite -> LockFileWrite -> ReadFile -> ParseFileContent -> mutate
// block.BlockedBy -> RenderFileContent -> WriteFileAtomic -> re-parse ->
// IndexFileBlocks -> emit block:changed.
func (a *App) SetTaskBlockedBy(blockID string, depIDs []string) error {
	return a.setTaskBlockedBy(blockID, depIDs)
}

// PluginSetTaskBlockedBy is the plugin-SDK wrapper for SetTaskBlockedBy,
// gated by the standard capability + session checks (SPECS §8.3 — plugins go
// through PluginContext, never direct wailsjs bindings). Mirrors
// PluginSetTaskRecurrence.
func (a *App) PluginSetTaskBlockedBy(pluginID, sessionToken, blockID string, depIDs []string) (bool, error) {
	if err := a.validatePluginSession(pluginID, sessionToken); err != nil {
		return false, err
	}
	if err := a.requireGrant(pluginID, plugins.CapContentMutate); err != nil {
		return false, err
	}
	if err := a.setTaskBlockedBy(blockID, depIDs); err != nil {
		return false, err
	}
	return true, nil
}

// setTaskBlockedBy is the shared core for the app-level and plugin-level entry
// points. It validates + normalizes the dependency list, runs the cycle check
// against the existing graph, then rewrites the token atomically.
func (a *App) setTaskBlockedBy(blockID string, depIDs []string) error {
	// Normalize: drop empties, de-duplicate preserving order. The parser's
	// ExtractRefs would do the same on the next re-index; doing it here keeps
	// the cycle check and the rendered token in lockstep with what the user
	// asked for.
	seen := make(map[string]struct{}, len(depIDs))
	normalized := make([]string, 0, len(depIDs))
	for _, id := range depIDs {
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		normalized = append(normalized, id)
	}
	// A self-dependency is a trivial cycle; surface it as ErrTaskCycle.
	for _, id := range normalized {
		if id == blockID {
			return ErrTaskCycle
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

	// Validate every proposed prerequisite exists as a TASK block. The cycle
	// check (below) only walks IDs it finds in the edge set, so an unknown or
	// non-task UUID would sail through, get written to markdown, then trip the
	// FK-existence probe at index time (the index skips it, but the user would
	// carry a permanently-broken token). Reject early with a clear error.
	if len(normalized) > 0 {
		var valid map[string]bool
		a.coordinator.WithDBRead(func() {
			valid, err = a.db.ValidTaskBlockIDs(normalized)
		})
		if err != nil {
			return fmt.Errorf("failed to validate dependency targets: %w", err)
		}
		for _, id := range normalized {
			if !valid[id] {
				return fmt.Errorf("%w: %s", ErrUnknownDependency, id)
			}
		}
	}

	// Cycle check: build the existing graph from the task_dependencies cache,
	// then ask whether adding each proposed edge would close a loop. We seed
	// the edge set with the proposed edges' endpoints so transitive cycles
	// through brand-new dependencies are caught too.
	edgeSeeds := append([]string{blockID}, normalized...)
	var edges map[string][]string
	a.coordinator.WithDBRead(func() {
		edges, err = a.db.DependencyEdges(edgeSeeds)
	})
	if err != nil {
		return fmt.Errorf("failed to load dependency graph for cycle check: %w", err)
	}
	// Apply the proposed additions to a working copy and reject if any single
	// edge would create a cycle. Checking each edge against the graph *with the
	// prior proposed edges already applied* catches A->B->C->A when all three
	// are submitted in one call.
	working := make(map[string][]string, len(edges))
	for k, v := range edges {
		cp := make([]string, len(v))
		copy(cp, v)
		working[k] = cp
	}
	for _, dep := range normalized {
		if dependencies.WouldCreateCycle(working, blockID, dep) {
			return ErrTaskCycle
		}
		working[blockID] = append(working[blockID], dep)
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
			// blocked-by edge write can't clobber an in-flight editor edit (#444).
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
				writeErr = fmt.Errorf("failed to parse file for dependency update: %w", parseErr)
				return
			}
			found := false
			for i := range parsedBlocks {
				if parsedBlocks[i].ID == blockID && parsedBlocks[i].Type == parser.BlockTask {
					parsedBlocks[i].BlockedBy = normalized
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
					log.Printf("SetTaskBlockedBy: IndexFileBlocks failed: %v", idxErr)
				}
				for _, b := range blocks {
					if b.ID == blockID {
						emitFileDate = b.FileDate
					}
				}
			} else {
				log.Printf("SetTaskBlockedBy: re-parse of rendered content failed (file written, index stale until next scan): %v", err)
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

// GetTaskBlockers returns the open (non-DONE) prerequisites of a task (#302).
// The frontend calls this before the DONE transition to decide whether to show
// the "complete anyway?" confirmation. Keeping it a separate read (rather than
// baking a guard into UpdateBlockState) preserves the existing IPC contract
// and lets the dialog list *which* tasks are blocking. An empty slice means
// the task is actionable.
func (a *App) GetTaskBlockers(blockID string) ([]parser.TaskResult, error) {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.db == nil {
		return nil, fmt.Errorf("vault database not loaded")
	}
	a.wg.Add(1)
	defer a.wg.Done()

	var ids []string
	var err error
	a.coordinator.WithDBRead(func() {
		ids, err = a.db.OpenBlockers(blockID)
	})
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return []parser.TaskResult{}, nil
	}
	// Reuse QueryTasksWithFilters with an ID-bounded filter so each blocker
	// comes back with full metadata (owner, due date, breadcrumb) for the
	// confirm dialog. The filter carries no owner/date constraints.
	return a.db.QueryTasksWithFilters(parser.TaskQueryFilter{BlockIDs: ids})
}
