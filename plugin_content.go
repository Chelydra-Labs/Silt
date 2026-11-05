package main

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"silt/backend/core"
	"silt/backend/parser"
	"silt/backend/plugins"
	"sort"
	"strings"
	"time"
)

// =========================================================================
// Expanded content API (#104)
// =========================================================================

// PluginCreateBlockOp describes a single create/delete/move for the bulk op.
type PluginCreateBlockOp struct {
	Kind     string `json:"kind"` // "create" | "delete" | "move"
	AfterID  string `json:"afterId,omitempty"`
	Type     string `json:"type,omitempty"`     // for create: TASK | NOTE | HEADER
	Text     string `json:"text,omitempty"`     // for create: block body
	BlockID  string `json:"blockId,omitempty"`  // for delete/move
	Notebook string `json:"notebook,omitempty"` // target page for create/move
	Section  string `json:"section,omitempty"`
	Page     string `json:"page,omitempty"`
	NewID    string `json:"newId,omitempty"` // for create: pre-minted UUID (caller captures it)
}

// CreateBlock creates a block after afterID (empty = page end). No plugin session.
// type must be TASK, NOTE, or HEADER. New TASK blocks receive Normal priority.
// Shared by PluginCreateBlock and Local MCP (ungated App core).
func (a *App) CreateBlock(afterID, notebook, section, page, blockType, text string) (string, error) {
	return a.createBlockWithReason(afterID, notebook, section, page, blockType, text, historyReasonPlugin)
}

func (a *App) createBlockWithReason(afterID, notebook, section, page, blockType, text, reason string) (string, error) {
	if a.db == nil {
		return "", fmt.Errorf("vault database not loaded")
	}
	if text == "" {
		return "", fmt.Errorf("text is required")
	}
	bt := parser.BlockType(strings.ToUpper(blockType))
	if bt != parser.BlockTask && bt != parser.BlockNote && bt != parser.BlockHeader {
		return "", fmt.Errorf("invalid block type %q (want TASK, NOTE, or HEADER)", blockType)
	}

	newID := newUUID()
	op := PluginCreateBlockOp{
		Kind:     "create",
		AfterID:  afterID,
		Type:     string(bt),
		Text:     text,
		Notebook: notebook,
		Section:  section,
		Page:     page,
		NewID:    newID,
	}
	if err := a.applyBlocksOps([]PluginCreateBlockOp{op}, reason); err != nil {
		return "", err
	}
	return newID, nil
}

// PluginCreateBlock creates a single block after `afterID` (or at the end of the
// page identified by notebook/section/page when afterID is empty).
// Returns the new block's UUID.
// Gated by content-mutate (#156). Session-token verified (#151).
func (a *App) PluginCreateBlock(pluginID, sessionToken, afterID, notebook, section, page, blockType, text string) (string, error) {
	if err := a.validatePluginSession(pluginID, sessionToken); err != nil {
		return "", err
	}
	if err := a.requireGrant(pluginID, plugins.CapContentMutate); err != nil {
		return "", err
	}
	return a.CreateBlock(afterID, notebook, section, page, blockType, text)
}

// PluginDeleteBlock removes a block by UUID from its source file and re-indexes.
// Gated by content-mutate (#156). Session-token verified (#151).
func (a *App) PluginDeleteBlock(pluginID, sessionToken, blockID string) error {
	if err := a.validatePluginSession(pluginID, sessionToken); err != nil {
		return err
	}
	if err := a.requireGrant(pluginID, plugins.CapContentMutate); err != nil {
		return err
	}
	if a.db == nil {
		return fmt.Errorf("vault database not loaded")
	}
	if blockID == "" {
		return fmt.Errorf("blockId is required")
	}
	return a.applyBlocksOps([]PluginCreateBlockOp{{Kind: "delete", BlockID: blockID}}, historyReasonPlugin)
}

// PluginMoveBlock moves a block within its page (after afterID) or to another
// page (notebook/section/page). When afterID is empty the block goes to the end
// of the target page.
// Gated by content-mutate (#156). Session-token verified (#151).
func (a *App) PluginMoveBlock(pluginID, sessionToken, blockID, afterID, notebook, section, page string) error {
	if err := a.validatePluginSession(pluginID, sessionToken); err != nil {
		return err
	}
	if err := a.requireGrant(pluginID, plugins.CapContentMutate); err != nil {
		return err
	}
	if a.db == nil {
		return fmt.Errorf("vault database not loaded")
	}
	if blockID == "" {
		return fmt.Errorf("blockId is required")
	}
	return a.applyBlocksOps([]PluginCreateBlockOp{{
		Kind:     "move",
		BlockID:  blockID,
		AfterID:  afterID,
		Notebook: notebook,
		Section:  section,
		Page:     page,
	}}, historyReasonPlugin)
}

// PluginApplyBlocks applies a batch of create/delete/move ops, coalescing per-
// page writes into a single SaveFileBlocks + re-index pass so a bulk op does
// not thrash the WAL with one rewrite per block (#104).
// Gated by content-mutate (#156). Session-token verified (#151).
func (a *App) PluginApplyBlocks(pluginID, sessionToken string, ops []PluginCreateBlockOp) error {
	if err := a.validatePluginSession(pluginID, sessionToken); err != nil {
		return err
	}
	if err := a.requireGrant(pluginID, plugins.CapContentMutate); err != nil {
		return err
	}
	if a.db == nil {
		return fmt.Errorf("vault database not loaded")
	}
	if len(ops) == 0 {
		return nil
	}
	return a.applyBlocksOps(ops, historyReasonPlugin)
}

// resolvedBlockOp is one PluginApplyBlocks op resolved to a concrete
// (source, notebook, section, page) target — plus, for cross-page moves, the
// block's original location before the target overwrite (kept so the
// pre-flight stash check and the post-target source rewrite address the same
// file, and so the first pass can fetch the block's content for insertion
// into the target).
type resolvedBlockOp struct {
	op       PluginCreateBlockOp
	source   string
	notebook string
	section  string
	page     string

	origSource   string
	origNotebook string
	origSection  string
	origPage     string
	newID        string
	blockType    parser.BlockType
	text         string
}

// applyBlocksOps is the shared engine for create/delete/move (single + bulk).
// It groups ops by target page and writes each affected page exactly once.
// The rendered base is reconstructed from CURRENT disk bytes inside the file
// lock (see saveFileBlocksWithGate's rebaseOnDisk) — the DB snapshot is only
// the lock/validation input, never the rendered content.
func (a *App) applyBlocksOps(ops []PluginCreateBlockOp, reason string) error {
	if reason == "" {
		reason = historyReasonPlugin
	}
	a.wg.Add(1)
	defer a.wg.Done()

	// 1. Resolve every op to a concrete (source, notebook, section, page).
	var resolvedOps []resolvedBlockOp

	// Resolve block locations for afterID / blockID lookups.
	locOf := func(id string) (source, nb, sec, pg string, ok bool) {
		if a.db == nil {
			return "", "", "", "", false
		}
		var loc struct{ s, n, se, p string }
		var e error
		a.coordinator.WithDBReadResult(func() error {
			bl, err := a.db.GetBlockLocation(id)
			if err == nil {
				loc.s, loc.n, loc.se, loc.p = bl.Source, bl.Notebook, bl.Section, bl.Page
			}
			e = err
			return nil
		})
		if e != nil {
			return "", "", "", "", false
		}
		return loc.s, loc.n, loc.se, loc.p, true
	}

	for i, op := range ops {
		r := resolvedBlockOp{op: op}
		switch op.Kind {
		case "create":
			r.blockType = parser.BlockType(strings.ToUpper(op.Type))
			if r.blockType != parser.BlockTask && r.blockType != parser.BlockNote && r.blockType != parser.BlockHeader {
				return fmt.Errorf("op %d: invalid block type %q", i, op.Type)
			}
			r.text = strings.ReplaceAll(op.Text, "\n", " ")
			// Use the caller's pre-minted ID if provided (so PluginCreateBlock
			// returns the exact UUID that lands in the file); mint otherwise.
			r.newID = op.NewID
			if r.newID == "" {
				r.newID = newUUID()
			}
			// Target page: from afterID if given, else explicit notebook/section/page.
			if op.AfterID != "" {
				s, n, se, p, ok := locOf(op.AfterID)
				if !ok {
					return fmt.Errorf("op %d: after block %s not found", i, op.AfterID)
				}
				r.source, r.notebook, r.section, r.page = s, n, se, p
			} else {
				sn := sanitizePathSegment(op.Notebook)
				sp := sanitizePathSegment(op.Page)
				if sn == "" || sp == "" {
					return fmt.Errorf("op %d: create without afterId needs notebook + page", i)
				}
				sec, secErr := validateSectionPath(op.Section, true)
				if secErr != nil {
					return fmt.Errorf("op %d: invalid section: %w", i, secErr)
				}
				r.notebook, r.section, r.page = sn, sec, sp
				r.source = a.resolveSourceByName(r.notebook)
			}
		case "delete", "move":
			s, n, se, p, ok := locOf(op.BlockID)
			if !ok {
				return fmt.Errorf("op %d: block %s not found", i, op.BlockID)
			}
			r.source, r.notebook, r.section, r.page = s, n, se, p
			r.origSource, r.origNotebook, r.origSection, r.origPage = s, n, se, p
			if op.Kind == "move" && (op.Notebook != "" || op.Section != "" || op.Page != "") {
				// Cross-page move: target is the explicit page.
				tn := sanitizePathSegment(op.Notebook)
				tp := sanitizePathSegment(op.Page)
				if tn != "" && tp != "" {
					tsec, secErr := validateSectionPath(op.Section, true)
					if secErr != nil {
						return fmt.Errorf("op %d: invalid section: %w", i, secErr)
					}
					r.notebook, r.section, r.page = tn, tsec, tp
					r.source = a.resolveSourceByName(r.notebook)
				}
			}
		default:
			return fmt.Errorf("op %d: unknown kind %q", i, op.Kind)
		}
		resolvedOps = append(resolvedOps, r)
	}

	// 2. Group by target page (same-page ops coalesce into one write).
	pagesByKey := map[string][]resolvedBlockOp{}
	for _, r := range resolvedOps {
		key := fmt.Sprintf("%s|%s/%s/%s", r.source, r.notebook, r.section, r.page)
		pagesByKey[key] = append(pagesByKey[key], r)
	}

	// Sort the page keys for deterministic cross-page ordering (#104 hardening).
	// Without this, Go map iteration randomizes the order, and locOf (which
	// reads a mutating DB) returns inconsistent results across runs.
	pageKeys := make([]string, 0, len(pagesByKey))
	for k := range pagesByKey {
		pageKeys = append(pageKeys, k)
	}
	sort.Strings(pageKeys)

	// 3. Apply per page (deterministic order).
	for _, pk := range pageKeys {
		pageOps := pagesByKey[pk]
		// Sort within-page ops by kind (delete → move → create) so the
		// mutation order is deterministic regardless of input order. This
		// prevents a footgun where [create-after-A, move-A, delete-A] would
		// behave differently under re-ordering (#104 hardening).
		opOrder := map[string]int{"delete": 0, "move": 1, "create": 2}
		sort.SliceStable(pageOps, func(i, j int) bool {
			return opOrder[pageOps[i].op.Kind] < opOrder[pageOps[j].op.Kind]
		})
		first := pageOps[0]
		blocks, err := a.fetchPageBlocks(first.notebook, first.section, first.page, captureNone)
		if err != nil {
			return fmt.Errorf("fetch page %s/%s/%s: %w", first.notebook, first.section, first.page, err)
		}
		var createdIDs []string
		// Pre-flight the cross-page moves' SOURCE stash gate before the
		// target half commits (#1005): a pending refused-write stash on a
		// source page refuses its rewrite outright (see
		// removeBlockFromSourcePage), and refusing only AFTER the target
		// save leaves the block duplicated on both pages — with a retry
		// degrading to a same-page no-op that reports success while the
		// stale copy survives. Refusing up front fails the whole op
		// atomically; the in-lock check remains as the TOCTOU backstop.
		// This runs BEFORE the payload resolution below so a pending
		// conflict surfaces as the conflict refusal, not as a side effect
		// of reading a stash-holder's rewritten source file.
		for _, r := range pageOps {
			if r.op.Kind != "move" || r.origNotebook == "" {
				continue
			}
			if r.origNotebook == r.notebook &&
				r.origSection == r.section &&
				r.origPage == r.page {
				continue
			}
			origPath, sn, ss, sp, err := a.crossPageMoveSourcePath(r)
			if err != nil {
				return err
			}
			if err := a.externalEditStashRefusalKey(core.NormalizeFileLockPath(origPath), origPath); err != nil {
				return fmt.Errorf("cross-page move: save source %s/%s/%s: %w", sn, ss, sp, err)
			}
		}
		// Pre-resolve cross-page move payloads (the moved block's content
		// from its source page) BEFORE the save takes coordinator locks: the
		// ops re-apply inside LockFileWrite for the disk rebase, and a DB
		// fetch under those locks would invert the vaultMu→coordinator lock
		// order every other save path observes. The payload itself is read
		// from the source file's CURRENT disk bytes (see
		// crossPageMoveSourceBlocks) so an external edit to the moved block
		// survives the move; a block absent from disk fails loudly instead
		// of resurrecting a stale DB row an external delete removed, and a
		// block that drifts between this read and the in-lock source
		// removal fails the move loudly too (removeBlockFromSourcePage).
		movePayloads := map[string]crossPageMovePayload{}
		for _, r := range pageOps {
			if r.op.Kind != "move" {
				continue
			}
			isCrossPage := r.origNotebook != "" &&
				(r.origNotebook != r.notebook ||
					r.origSection != r.section ||
					r.origPage != r.page)
			if !isCrossPage {
				continue
			}
			srcBlocks, diskAnchored, srcErr := a.crossPageMoveSourceBlocks(r)
			if srcErr != nil {
				return fmt.Errorf("cross-page move: fetch source page for block %s: %w", r.op.BlockID, srcErr)
			}
			found := false
			for _, b := range srcBlocks {
				if b.ID == r.op.BlockID {
					movePayloads[r.op.BlockID] = crossPageMovePayload{block: b, diskAnchored: diskAnchored}
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("cross-page move: block %s not found in source page %s/%s/%s", r.op.BlockID, r.origNotebook, r.origSection, r.origPage)
			}
		}
		// applyPageOps re-applies the page's ops to a block base: once to
		// the DB snapshot (lock/validation input for the save) and again,
		// inside LockFileWrite, to the CURRENT disk bytes — the base that
		// renders is always disk, so stale DB rows can never revert an
		// external edit the watcher has not re-indexed (#1005).
		applyPageOps := func(base []parser.ParsedBlock) []parser.ParsedBlock {
			mutated := base
			for _, r := range pageOps {
				switch r.op.Kind {
				case "create":
					var nb parser.ParsedBlock
					if r.blockType == parser.BlockTask {
						// Canonicalize TASK creates through the parser (#1039 D2):
						// the FIRST disk write matches every later render (no
						// silent reorder/rewrite on the next full-page save), and
						// tokens the model echoed into the title ([due::],
						// [priority::], [owner::]…) become honored metadata
						// instead of inert description text.
						// Collapse internal newlines first: TaskCheckboxRegex is
						// line-anchored, so a multi-line title would mis-parse
						// as a NOTE and the task never reaches the hub (#1039).
						flat := strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(r.text)
						nb, _, _ = parser.ParseLine("- [ ] "+flat, 0, a.spacesPerTab)
						// The parse minted its own id/date for an id-less line;
						// the pre-minted r.newID is what callers were promised.
						nb.ID = r.newID
						nb.RawText = ""
						nb.FileDate = time.Now().Format("2006-01-02")
						// `- [ ] ` pins Status=TODO; callers needing DOING/DONE
						// use UpdateBlockState after mint. Same for priority: an
						// explicitly echoed value survives the parse, otherwise
						// apply the app-wide created-task default. ([priority:: 3]
						// is indistinguishable from "no token" — the legacy
						// missing sentinel — and defaults like a missing token.)
						if nb.Priority == parser.LegacyMissingTaskPriority {
							nb.Priority = parser.DefaultTaskPriority
						}
					} else {
						nb = parser.ParsedBlock{
							ID:        r.newID,
							Type:      r.blockType,
							CleanText: r.text,
							FileDate:  time.Now().Format("2006-01-02"),
						}
					}
					mutated = insertAfter(mutated, r.op.AfterID, nb)
					createdIDs = append(createdIDs, r.newID)
				case "delete":
					mutated = removeByID(mutated, r.op.BlockID)
				case "move":
					// Same-page move: the block is already in `mutated`, just reorder.
					// Cross-page move: the block lives in the SOURCE page, not in
					// `mutated` (the target page); insert the pre-fetched payload.
					isCrossPage := r.origNotebook != "" &&
						(r.origNotebook != r.notebook ||
							r.origSection != r.section ||
							r.origPage != r.page)
					if isCrossPage {
						if p, ok := movePayloads[r.op.BlockID]; ok {
							mutated = insertAfter(mutated, r.op.AfterID, p.block)
						}
					} else if r.op.AfterID != "" || r.op.BlockID != "" {
						mutated = moveWithin(mutated, r.op.BlockID, r.op.AfterID)
					}
				}
			}
			return mutated
		}
		if err := a.saveFileBlocksWithGate(first.notebook, first.section, first.page, applyPageOps(blocks), reason, gateStashOnly, funnelProgrammatic, applyPageOps, 0); err != nil {
			return fmt.Errorf("save page %s/%s/%s: %w", first.notebook, first.section, first.page, err)
		}
		// For cross-page moves, remove the block from its source page.
		//
		// The source rewrite is a read-modify-write anchored on CURRENT disk
		// bytes inside LockFileWrite (see removeBlockFromSourcePage): an
		// external edit the watcher has not re-indexed yet survives it, and a
		// pending refused-write stash refuses it outright. LockFileWrite
		// serializes concurrent source-page writes so the file never has a
		// torn write. A failure here is NOT swallowed — the target half
		// already committed, so reconcileFailedCrossPageRemoval either rolls
		// that half back (retry-safe) or reports exactly where the block now
		// lives; without that, a retry would resolve through locOf to the
		// TARGET page and report success as a same-page no-op while the
		// stale source copy survives.
		for _, r := range pageOps {
			if r.op.Kind == "move" && r.origNotebook != "" &&
				(r.origNotebook != r.notebook ||
					r.origSection != r.section ||
					r.origPage != r.page) {
				origPath, sn, ss, sp, err := a.crossPageMoveSourcePath(r)
				if err != nil {
					return err
				}
				// Remove the moved block from the source page. We do a TARGETED
				// delete (DELETE WHERE id = ?) instead of re-indexing the whole
				// page via IndexFileBlocks. This is critical for concurrency:
				// IndexFileBlocks does "DELETE FROM blocks WHERE id IN (...)"
				// for every block in the filtered list, which would delete blocks
				// that a concurrent goroutine already moved to ANOTHER page.
				// The targeted delete only removes the single block being moved.
				blockID := r.op.BlockID
				payload := movePayloads[blockID]
				var writeErr error
				a.coordinator.LockFileWrite(origPath, func() {
					writeErr = a.removeBlockFromSourcePage(origPath, r.origSource, sn, ss, sp, blockID, reason, payload)
				})
				if writeErr != nil {
					return a.reconcileFailedCrossPageRemoval(first, blockID, payload, origPath, r.origSource, sn, ss, sp, writeErr)
				}
				// Whole-page signal for the rewritten source page (outside the
				// file lock, like every emit here). The rewrite refreshed the
				// fingerprint, so an editor still open on the source page must
				// recapture buffer + baseline via its block:changed reload —
				// otherwise its stale buffer's next autosave passes the gate
				// and silently re-inserts the moved block (#1005; same
				// contract as the rename link fanout).
				a.emitBlockChanged("", sn, ss, sp, "")
			}
		}
		for _, id := range createdIDs {
			a.emitBlockChanged(id, first.notebook, first.section, first.page, "")
		}
	}

	return nil
}

// crossPageMoveSourcePath resolves a cross-page move's source page to its
// file path plus sanitized locator. Shared by the pre-flight stash check and
// the post-target source rewrite so the two can never disagree about which
// file a move's source half addresses.
func (a *App) crossPageMoveSourcePath(r resolvedBlockOp) (origPath, sn, ss, sp string, err error) {
	sn = sanitizePathSegment(r.origNotebook)
	var secErr error
	ss, secErr = validateSectionPath(r.origSection, true)
	if secErr != nil {
		return "", "", "", "", fmt.Errorf("cross-page move: invalid source section: %w", secErr)
	}
	sp = sanitizePathSegment(r.origPage)
	origDir, dirErr := a.resolveNotebookDir(sn, r.origSource)
	if dirErr != nil {
		return "", "", "", "", fmt.Errorf("cross-page move: resolve source dir %s: %w", sn, dirErr)
	}
	origPath = filepath.Join(origDir, ss, sp+".md")
	if !isPathWithinRoot(origPath, origDir) {
		return "", "", "", "", fmt.Errorf("cross-page move: source path escapes notebook root")
	}
	return origPath, sn, ss, sp, nil
}

// reconcileFailedCrossPageRemoval settles a source-removal failure that
// struck AFTER the target half of the move committed (#1005). The raw
// failure alone leaves an un-retryable wedge: the committed target write
// re-homed the block's DB row, so a retry resolves the op via locOf to the
// TARGET page and degrades to a same-page no-op reporting success while the
// stale source copy survives. Two honest outcomes, decided by whether the
// source copy provably survived:
//
//   - The block is still on the source file → the move never happened on
//     either side semantically: roll the target half back (remove the
//     transplanted copy + its re-homed row) AND re-index the source page
//     from the probe's parse so the block's row lives where the file copy
//     lives — "the move can be retried" is then index-true too, not just
//     file-true (the target's index did a global delete-by-id, so without
//     this the block is invisible to every DB-backed query until an
//     unrelated re-index heals it).
//   - The block is gone from the source file → the source half landed and
//     only a durability/index error remains: the block now lives on the
//     target; say so instead of inviting a lossy rollback.
//   - The source file cannot be read or parsed → the probe could not
//     decide; say THAT instead of guessing "gone" (the block may exist on
//     both pages — both page names are in the message for manual cleanup).
//
// The probe and the source re-index run under the source file's write lock
// so a concurrent writer cannot flip the answer mid-decision or invalidate
// the parse between probe and index.
func (a *App) reconcileFailedCrossPageRemoval(first resolvedBlockOp, blockID string, payload crossPageMovePayload, origPath, origSource, sn, ss, sp string, cause error) error {
	const (
		probePresent = iota
		probeGone
		probeUnreadable
	)
	probe := probeUnreadable
	var probeIndexErr error
	a.coordinator.LockFileWrite(origPath, func() {
		contentBytes, err := os.ReadFile(origPath)
		if err != nil {
			return
		}
		blocks, meta, _, _, perr := parser.ParseFileContent(string(contentBytes),
			sn, ss, sp, fileOrDefaultDate(origPath), a.spacesPerTab)
		if perr != nil {
			return
		}
		for _, b := range blocks {
			if b.ID == blockID {
				probe = probePresent
				break
			}
		}
		if probe != probePresent {
			probe = probeGone
			return
		}
		// Restore the source row while still holding this lock, from the
		// parse that proved the copy present (see the doc comment): the
		// retry path resolves the block via the index, and the target
		// half's global delete-by-id left it with no row anywhere.
		if ierr := a.indexFile(origSource, sn, ss, sp, blocks, meta, meta.Warnings...); ierr != nil {
			// Keep the rollback going — the file copy is safe; only the
			// index restore failed. Surfaced in the final error below.
			probeIndexErr = ierr
		}
	})
	if probe == probeUnreadable {
		return fmt.Errorf("cross-page move: source %s/%s/%s could not be read or parsed while deciding the rollback, so block %s may exist on BOTH it and %s/%s/%s — check both pages: %w",
			sn, ss, sp, blockID, first.notebook, first.section, first.page, cause)
	}
	if probe == probeGone {
		return fmt.Errorf("cross-page move: block %s now lives on %s/%s/%s, but removing it from source %s/%s/%s reported: %w",
			blockID, first.notebook, first.section, first.page, sn, ss, sp, cause)
	}
	dir, err := a.resolveNotebookDir(first.notebook, first.source)
	if err != nil {
		return fmt.Errorf("cross-page move: block %s remains duplicated (source %s/%s/%s, target %s/%s/%s) after: %w; rollback could not resolve the target dir: %v",
			blockID, sn, ss, sp, first.notebook, first.section, first.page, cause, err)
	}
	targetPath := filepath.Join(dir, first.section, first.page+".md")
	// diskAnchored=false: the rollback's job is to undo the transplant this
	// batch just made, not to gate the target's own bytes — and the source
	// copy is proven present, so removing the duplicate can never lose the
	// block's only copy.
	var rmErr error
	a.coordinator.LockFileWrite(targetPath, func() {
		rmErr = a.removeBlockFromSourcePage(targetPath, first.source,
			first.notebook, first.section, first.page, blockID,
			historyReasonPlugin, crossPageMovePayload{block: payload.block})
	})
	if rmErr != nil {
		return fmt.Errorf("cross-page move: block %s remains duplicated (source %s/%s/%s, target %s/%s/%s) after: %w; rollback failed: %v — remove the stale copy manually",
			blockID, sn, ss, sp, first.notebook, first.section, first.page, cause, rmErr)
	}
	a.emitBlockChanged("", first.notebook, first.section, first.page, "")
	if probeIndexErr != nil {
		return fmt.Errorf("cross-page move: block %s stayed on source %s/%s/%s and the target half on %s/%s/%s was rolled back, but restoring the source's index row failed: %v — the block is on disk but missing from search until re-indexed: %w",
			blockID, sn, ss, sp, first.notebook, first.section, first.page, probeIndexErr, cause)
	}
	return fmt.Errorf("cross-page move: block %s stayed on source %s/%s/%s; the target half on %s/%s/%s was rolled back and the move can be retried: %w",
		blockID, sn, ss, sp, first.notebook, first.section, first.page, cause)
}

// crossPageMovePayload is the pre-lock basis for one cross-page move's
// transplanted block: the block as parsed from the source file BEFORE any
// coordinator lock is taken, plus whether those bytes came from disk at all
// (a missing source file falls back to DB rows, which carry no external
// bytes that could drift).
type crossPageMovePayload struct {
	block        parser.ParsedBlock
	diskAnchored bool
}

// crossPageMoveBasisDrifted reports whether the same block parsed from two
// reads of the source file differs in any content-bearing field. RawText,
// LineNumber, FileDate and ParentID are excluded: Silt's own rewrites
// re-normalize the id comment (`<!-- id: … @ date -->`) and can shift line
// numbers (a concurrent tracked rewrite removing ANOTHER block does both, and
// that edit survives the disk-anchored rewrite), the file-date day can roll
// between the reads, and ParentID — like LineNumber — is position-derived at
// parse time from the preceding-ID stack and never serialized: a same-batch
// move of a parent rewrites the source BEFORE the nested child's removal
// re-reads it, so the child deterministically re-parses with ParentID="" while
// its transplanted content is identical. Everything the transplant carries —
// body text and every metadata token — is still compared, so an external edit
// to the moved block itself cannot slip through.
func crossPageMoveBasisDrifted(basis, current parser.ParsedBlock) bool {
	basis.RawText, basis.LineNumber, basis.FileDate, basis.ParentID = "", 0, "", ""
	current.RawText, current.LineNumber, current.FileDate, current.ParentID = "", 0, "", ""
	return !reflect.DeepEqual(basis, current)
}

// crossPageMoveSourceBlocks returns a cross-page move's source-page blocks
// anchored on CURRENT disk bytes: the transplanted payload must carry an
// external edit to the moved block the watcher has not re-indexed, or the
// disk-anchored source rewrite (removeBlockFromSourcePage) removes that edit
// from the source while the target receives the stale DB row — the edit is
// then on neither page. This is the same anchoring invariant
// removeBlockFromSourcePage documents. Only a source file that no longer
// exists on disk falls back to DB rows (no external bytes can be lost from a
// missing file; the rows are the only record of the block being moved). The
// second return reports whether the payload basis is disk-anchored; the
// in-lock drift gate in removeBlockFromSourcePage only applies to it.
//
// Runs in the pre-lock pass, so no coordinator locks are held: the raw file
// read does not touch the vaultMu→coordinator ordering the lock-order comment
// at the payload build guards, and Silt's atomic-rename writers make torn
// reads impossible.
func (a *App) crossPageMoveSourceBlocks(r resolvedBlockOp) (blocks []parser.ParsedBlock, diskAnchored bool, err error) {
	origPath, sn, ss, sp, err := a.crossPageMoveSourcePath(r)
	if err != nil {
		return nil, false, err
	}
	contentBytes, err := os.ReadFile(origPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, false, fmt.Errorf("read source file: %w", err)
		}
		blocks, dbErr := a.fetchPageBlocks(r.origNotebook, r.origSection, r.origPage, captureNone)
		return blocks, false, dbErr
	}
	fileDate := fileOrDefaultDate(origPath)
	srcBlocks, _, _, _, parseErr := parser.ParseFileContent(string(contentBytes),
		sn, ss, sp, fileDate, a.spacesPerTab)
	if parseErr != nil {
		return nil, false, fmt.Errorf("parse source %s/%s/%s: %w", sn, ss, sp, parseErr)
	}
	return srcBlocks, true, nil
}

// removeBlockFromSourcePage removes a single block from the source page FILE
// and does a TARGETED DB delete (DELETE WHERE id = ?). This is used by the
// second pass of a cross-page move. Critically, it does NOT call
// IndexFileBlocks — that function deletes ALL passed-in block IDs from the
// entire table, which would clobber blocks that a concurrent goroutine already
// moved to another page. The targeted delete only removes the single block
// being moved, so concurrent cross-page moves from the same source page are
// safe.
//
// The caller MUST hold LockFileWrite for filePath. The file rewrite is
// anchored on the CURRENT disk bytes read inside that lock (the same anchor
// MutateBlock uses): rendering from the DB snapshot would revert an external
// edit the watcher has not re-indexed yet — the exact loss class #1005
// exists to prevent — and the pending-stash gate below refuses the rewrite
// outright while a refused save awaits resolution. The in-lock parse is also
// checked against the pre-lock payload basis: a moved block whose bytes
// changed between the two reads fails loudly rather than dropping the edit
// from both pages (see the drift gate below).
func (a *App) removeBlockFromSourcePage(filePath, source, notebook, section, page, blockID, reason string, payload crossPageMovePayload) error {
	// A pending refused-write stash IS the conflict state: rewriting under it
	// would revert external bytes, and a later keep_mine would replay the
	// stashed pre-move buffer, resurrecting the moved block on both pages
	// (#1005). Same refusal every gated funnel applies.
	if err := a.externalEditStashRefusalKey(core.NormalizeFileLockPath(filePath), filePath); err != nil {
		return err
	}
	// NOTE: the file write and DB delete are not atomic together — the file is
	// rewritten first, then the DB row is deleted. If the process crashes
	// between, the file won't have the block but the DB will still list it.
	// This is acceptable: the DB is a re-derivable cache (§0 rule 4), so the
	// next index rebuild (or the next SaveFileBlocks for this page) reconciles
	// the file↔DB state automatically.
	contentBytes, err := os.ReadFile(filePath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read source file: %w", err)
	}
	fileDate := fileOrDefaultDate(filePath)
	srcBlocks, _, _, _, parseErr := parser.ParseFileContent(string(contentBytes),
		notebook, section, page, fileDate, a.spacesPerTab)
	if parseErr != nil {
		return fmt.Errorf("parse source %s/%s/%s: %w", notebook, section, page, parseErr)
	}
	// Drift gate (#1005): the payload basis was parsed from the source file
	// BEFORE any lock was taken. An external write to the moved block landing
	// in that window would otherwise be silently dropped from BOTH pages —
	// the target receives the pre-edit body while this rewrite deletes the
	// post-edit one. Fail the whole move loudly instead: the target half has
	// already committed, so the failure is reconciled (rolled back or
	// reported as half-applied) by reconcileFailedCrossPageRemoval — never a
	// silent loss. Absence and drift are distinct refusals: a concurrent
	// mover taking the block first is a correct end state misreported as an
	// external edit, so it gets its own message. DB-fallback bases carry no
	// external bytes, so only disk-anchored payloads are gated.
	if payload.diskAnchored {
		var current *parser.ParsedBlock
		for i := range srcBlocks {
			if srcBlocks[i].ID == blockID {
				current = &srcBlocks[i]
				break
			}
		}
		if current == nil {
			return fmt.Errorf("cross-page move: block %s no longer present on source page %s/%s/%s (moved or deleted concurrently); refusing to transplant the stale copy",
				blockID, notebook, section, page)
		}
		if crossPageMoveBasisDrifted(payload.block, *current) {
			return fmt.Errorf("cross-page move: block %s changed on disk in %s/%s/%s while the move was in flight; refusing to drop the external edit",
				blockID, notebook, section, page)
		}
	}
	filtered := removeByID(srcBlocks, blockID)

	// Rewrite the file with the remaining blocks (no re-index).
	frontmatter, body := parser.SplitFrontmatter(string(contentBytes))
	if frontmatter == "" {
		frontmatter = fmt.Sprintf("---\nnotebook: %q\nsection: %q\npage: %q\ndate: %q\ntags: []\n---\n",
			notebook, section, page, time.Now().Format("2006-01-02"))
		body = string(contentBytes)
	}
	newContent := parser.RenderFileContent(filtered, body, frontmatter, a.spacesPerTab)
	if reason == "" {
		reason = historyReasonPlugin
	}
	a.maybeCapturePageVersion(historyLoc(source, notebook, section, page), contentBytes, []byte(newContent), reason)
	if err := a.trackedPageWrite(filePath, []byte(newContent)); err != nil {
		return fmt.Errorf("write source file: %w", err)
	}

	// Targeted DB delete: remove ONLY the moved block from the SOURCE page.
	// Page-scoped so it doesn't delete the block from the TARGET page where
	// the first pass already indexed it.
	a.coordinator.WithDBWrite(func() {
		err = a.db.DeleteBlockFromPage(blockID, source, notebook, section, page)
	})
	if err != nil {
		return fmt.Errorf("delete moved block %s from index: %w", blockID, err)
	}
	return nil
}

func insertAfter(blocks []parser.ParsedBlock, afterID string, nb parser.ParsedBlock) []parser.ParsedBlock {
	if afterID == "" {
		return append(blocks, nb)
	}
	for i, b := range blocks {
		if b.ID == afterID {
			out := make([]parser.ParsedBlock, 0, len(blocks)+1)
			out = append(out, blocks[:i+1]...)
			out = append(out, nb)
			out = append(out, blocks[i+1:]...)
			return out
		}
	}
	return append(blocks, nb)
}

func removeByID(blocks []parser.ParsedBlock, id string) []parser.ParsedBlock {
	out := make([]parser.ParsedBlock, 0, len(blocks))
	for _, b := range blocks {
		if b.ID != id {
			out = append(out, b)
		}
	}
	return out
}

func moveWithin(blocks []parser.ParsedBlock, id, afterID string) []parser.ParsedBlock {
	var moved *parser.ParsedBlock
	filtered := make([]parser.ParsedBlock, 0, len(blocks))
	for i := range blocks {
		if blocks[i].ID == id {
			moved = &blocks[i]
		} else {
			filtered = append(filtered, blocks[i])
		}
	}
	if moved == nil {
		return blocks
	}
	return insertAfter(filtered, afterID, *moved)
}

// PluginCreatePage wraps the core CreatePage for the SDK (sandboxed to the
// declared notebook scope). Returns the resolved date string.
// Session-token verified (#236).
func (a *App) PluginCreatePage(pluginID, sessionToken, notebook, section, page, dateStr string) (string, error) {
	if err := a.validatePluginSession(pluginID, sessionToken); err != nil {
		return "", err
	}
	if err := a.requireGrant(pluginID, plugins.CapContentMutate); err != nil {
		return "", err
	}
	return a.CreatePage(notebook, section, page, dateStr)
}

// PluginRegisterSurface is the Go-side capability gate for plugin UI surface
// registration (#154). A plugin without the ui-surface grant gets a
// CapabilityDeniedError here, before the frontend registry adds the surface.
// The frontend registerSurface SDK method calls this first; only on success
// does it add the surface to the frontend surfaces map.
// Session-token verified (#236).
func (a *App) PluginRegisterSurface(pluginID, sessionToken, surfaceID, kind, label string) error {
	if err := a.validatePluginSession(pluginID, sessionToken); err != nil {
		return err
	}
	return a.requireGrant(pluginID, plugins.CapUISurface)
}

// PluginCreateSection wraps the core CreateSection for the SDK.
// Session-token verified (#236).
func (a *App) PluginCreateSection(pluginID, sessionToken, notebook, section string) error {
	if err := a.validatePluginSession(pluginID, sessionToken); err != nil {
		return err
	}
	if err := a.requireGrant(pluginID, plugins.CapContentMutate); err != nil {
		return err
	}
	return a.CreateSection(notebook, "", section)
}

// PluginCreateNotebook wraps the core CreateNotebook for the SDK.
// Session-token verified (#236).
func (a *App) PluginCreateNotebook(pluginID, sessionToken, name string) error {
	if err := a.validatePluginSession(pluginID, sessionToken); err != nil {
		return err
	}
	if err := a.requireGrant(pluginID, plugins.CapContentMutate); err != nil {
		return err
	}
	return a.CreateNotebook(name)
}

// PluginDeletePage wraps the core DeletePage for the SDK.
// Session-token verified (#236).
func (a *App) PluginDeletePage(pluginID, sessionToken, notebook, section, page string) error {
	if err := a.validatePluginSession(pluginID, sessionToken); err != nil {
		return err
	}
	if err := a.requireGrant(pluginID, plugins.CapContentMutate); err != nil {
		return err
	}
	return a.DeletePage(notebook, section, page)
}

// PluginRenamePage wraps the core RenamePage for the SDK.
// Session-token verified (#236).
func (a *App) PluginRenamePage(pluginID, sessionToken, notebook, section, oldName, newName string) error {
	if err := a.validatePluginSession(pluginID, sessionToken); err != nil {
		return err
	}
	if err := a.requireGrant(pluginID, plugins.CapContentMutate); err != nil {
		return err
	}
	return a.RenamePage(notebook, section, oldName, newName)
}
