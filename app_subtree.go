package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"

	"silt/backend/db"
	"silt/backend/parser"
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
	if parentIdx < 0 {
		return nil
	}
	var subtree []parser.ParsedBlock
	for i := parentIdx + 1; i < len(blocks); i++ {
		if blocks[i].Depth <= parentDepth {
			break
		}
		subtree = append(subtree, blocks[i])
	}
	return subtree
}

// SaveSubtreeBlocks splices an edited child sub-tree back into the parent
// task's block (#305). It re-parses the whole file, replaces the contiguous
// child range (depth > parent depth) with the incoming children, then runs the
// canonical write chain: RenderFileContent → WriteFileAtomic → re-parse →
// IndexFileBlocks → emit block:changed. The parent task and all surrounding
// content (unmanaged prose, sibling blocks, other tasks) are preserved verbatim
// because RenderFileContent re-serializes the full slice. Holds both
// LockBlockWrite(parentID) and LockFileWrite(filePath) so the splice is atomic
// and races no concurrent writer. Returns true when a write occurred.
func (a *App) SaveSubtreeBlocks(blockID string, children []parser.ParsedBlock) (bool, error) {
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
	// resolves the hierarchy even if the caller omitted it. Depth is left as
	// the editor set it (the serializer indents by Depth * spacesPerTab).
	normalized := make([]parser.ParsedBlock, len(children))
	for i, c := range children {
		c.ParentID = parentID
		normalized[i] = c
	}

	out := make([]parser.ParsedBlock, 0, len(blocks)-(childEnd-(parentIdx+1))+len(normalized))
	out = append(out, blocks[:parentIdx+1]...)
	out = append(out, normalized...)
	out = append(out, blocks[childEnd:]...)
	return out, true
}
