package db

import (
	"database/sql"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"silt/backend/dependencies"
	"silt/backend/parser"
)

// tagRegex matches inline tags starting with # followed by a letter.
// Package-level var so the regex is compiled once, not per ExtractTags call.
var tagRegex = regexp.MustCompile(`\B#([a-zA-Z][a-zA-Z0-9_/-]*)`)

// nullIfEmpty converts an empty string to a SQL NULL (nil interface), so the
// block_meta projection stores absent [author::]/[ts::] values as NULL rather
// than empty strings — mirroring the nullable-cache convention used for task
// owner/dates/etc. A non-empty string passes through unchanged.
func nullIfEmpty(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

// taskDerivedCounts precomputes per-task derived cache columns for one file's
// block slice (#434 / #439 / comments descendants):
//   - comments: ALL NOTE descendants under a task (walk ParentID until TASK)
//   - subtaskTotal / subtaskDone: direct TASK children only
func taskDerivedCounts(blocks []parser.ParsedBlock) (comments, subtaskTotal, subtaskDone map[string]int) {
	parentOf := make(map[string]string, len(blocks))
	typeOf := make(map[string]parser.BlockType, len(blocks))
	for _, b := range blocks {
		parentOf[b.ID] = b.ParentID
		typeOf[b.ID] = b.Type
	}
	comments = make(map[string]int)
	subtaskTotal = make(map[string]int)
	subtaskDone = make(map[string]int)
	for _, b := range blocks {
		switch b.Type {
		case parser.BlockNote:
			// Nested replies: walk up until a TASK ancestor (or root).
			// Depth-capped to match CommentThread's frontend guard and to
			// survive a cyclic ParentID graph from SaveSubtreeBlocks /
			// hand-edited markdown (self-loop alone is not enough for A→B→A).
			pid := b.ParentID
			for steps := 0; pid != "" && steps < 64; steps++ {
				if typeOf[pid] == parser.BlockTask {
					comments[pid]++
					break
				}
				next := parentOf[pid]
				if next == "" || next == pid {
					break
				}
				pid = next
			}
		case parser.BlockTask:
			if b.ParentID != "" && typeOf[b.ParentID] == parser.BlockTask {
				subtaskTotal[b.ParentID]++
				if b.Status == "DONE" {
					subtaskDone[b.ParentID]++
				}
			}
		}
	}
	return comments, subtaskTotal, subtaskDone
}

// taskEstimateMinutes returns the nullable estimate_minutes projection for a
// task block: ParseEstimateMinutes ok → minutes, else NULL (invalid/empty raw
// still lives only in markdown).
func taskEstimateMinutes(raw string) interface{} {
	if mins, ok := parser.ParseEstimateMinutes(raw); ok {
		return mins
	}
	return nil
}

// blockRefEdge is one extracted (target, kind) tuple from a block's RawText.
// Shared by the live indexer and the warm-upgrade backfill so the extraction
// contract has one source of truth (#704).
type blockRefEdge struct {
	targetID string
	kind     BacklinkKind
}

// extractBlockRefEdges extracts ((uuid)) block-ref and {{embed:uuid}} embed
// tokens from raw and returns one (targetID, kind) tuple per distinct match.
// Pure function — no DB access, no side effects. Shared by indexBlockReferences
// (live indexer path) and backfillBlockReferences (one-shot migration) so the
// regex extraction cannot drift between the two callers.
//
// Walks raw regardless of block type; callers pass RawText for every block
// row (including CODE blocks — the backlinks contract requires all-type
// extraction; page_links CODE exclusion is unrelated and lives in the
// indexer loop).
func extractBlockRefEdges(raw string) []blockRefEdge {
	var edges []blockRefEdge
	for _, m := range parser.BlockRefRegex.FindAllStringSubmatch(raw, -1) {
		if len(m) >= 2 && m[1] != "" {
			edges = append(edges, blockRefEdge{targetID: m[1], kind: BacklinkBlockRef})
		}
	}
	for _, m := range parser.EmbedRegex.FindAllStringSubmatch(raw, -1) {
		if len(m) >= 2 && m[1] != "" {
			edges = append(edges, blockRefEdge{targetID: m[1], kind: BacklinkEmbed})
		}
	}
	return edges
}

// indexBlockReferences extracts ((uuid)) block-ref and {{embed:uuid}} embed
// tokens from raw via the shared extractBlockRefEdges helper and inserts each
// as a distinct edge into block_references via the caller's prepared
// statement. Shared between IndexFileBlocks and IndexScanResults so the two
// indexers cannot drift (#704). The cascade through the per-block DELETE FROM
// blocks at the top of each indexer already removed the prior edge set, so
// each insert here is additive; INSERT OR IGNORE keeps the (source_block_id,
// target_block_id, kind) PK unique when the same token appears twice in one
// block.
//
// Includes CODE blocks: the indexed lookup must preserve the contract that
// block-ref/embed tokens in fenced code ARE backlinks (diverging from
// page_links, which excludes CODE blocks — literal [[…]] inside fenced code
// is not a link and must not be rewritten on rename).
//
// Source-only FK by design: target IDs that do not (yet) exist in `blocks`
// are retained as edges — markdown may reference an ID before the target is
// indexed, after it was deleted, or in a file indexed later. The backlink
// re-resolves automatically when the target subsequently appears.
func indexBlockReferences(stmt *sql.Stmt, sourceBlockID, raw string) error {
	for _, e := range extractBlockRefEdges(raw) {
		if _, err := stmt.Exec(sourceBlockID, e.targetID, string(e.kind)); err != nil {
			return fmt.Errorf("insert %s edge %s -> %s: %w", e.kind, sourceBlockID, e.targetID, err)
		}
	}
	return nil
}

// warnOnDependencyCycle builds the [blocked_by::] edge map for a set of blocks
// and logs a warning when it contains a cycle (#301). The IPC setter prevents
// cycles at write time, but a hand-edited or externally-synced file (Obsidian,
// Dropbox) can still introduce one; this guard surfaces it so the user knows
// the dependency graph is inconsistent rather than silently caching a loop.
// The check is best-effort — it never blocks indexing or returns an error.
//
// Used by the single-file indexer (IndexFileBlocks), which can only see one
// file's edges. The batched indexer (IndexScanResults) inlines its own
// cross-file check instead — cycles can span files and are invisible to a
// per-file pass (see TestIndexScanResults_CrossFileDependencyCycle).
func warnOnDependencyCycle(blocks []parser.ParsedBlock) {
	edges := make(map[string][]string)
	for _, b := range blocks {
		if b.Type == parser.BlockTask && len(b.BlockedBy) > 0 {
			edges[b.ID] = append(edges[b.ID], b.BlockedBy...)
		}
	}
	if len(edges) == 0 {
		return
	}
	if dependencies.DetectsCycle(edges) {
		log.Printf("db: task_dependencies cycle detected in indexed blocks — a hand-edited or externally-synced file introduced a circular dependency; the setter normally prevents this")
	}
}

// --- Shared block-index engine --------------------------------------------
//
// IndexFileBlocks (single-file) and IndexScanResults (batched startup/linked-
// notebook scan) previously carried two ~95%-duplicated ~300-LOC bodies that
// had drifted across bug-fixes. The prepared statements, the per-block insert
// loop, and the [blocked_by::] second pass are now shared; each indexer keeps
// only its distinct orchestration (transaction scope, per-result skip
// handling, and — load-bearing — the cycle-check's edge-set scope).

// blockIndexStmts holds the byte-identical prepared statements both indexers
// share. Prepared once per transaction and reused across files/blocks.
type blockIndexStmts struct {
	block           *sql.Stmt
	task            *sql.Stmt
	taskDep         *sql.Stmt
	tag             *sql.Stmt
	pageLink        *sql.Stmt
	blockMetaUpsert *sql.Stmt
	blockMetaClear  *sql.Stmt
	blockRef        *sql.Stmt
}

// prepareBlockIndexStmts prepares the shared statement set on tx. The SQL is
// identical across both indexers (the prior duplication was a maintenance
// hazard where fixes landed in one path but not the other).
func prepareBlockIndexStmts(tx *sql.Tx) (*blockIndexStmts, error) {
	s := &blockIndexStmts{}
	var err error
	if s.block, err = tx.Prepare("INSERT INTO blocks (id, parent_id, source, notebook, section, page, file_date, depth, type, raw_content, clean_content, line_number) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)"); err != nil {
		return nil, err
	}
	if s.task, err = tx.Prepare("INSERT INTO tasks (block_id, status, owner, start_date, due_date, priority, pinned, progress, recur, comments_count, links_count, created_at, completed_at, manual_order, modified_at, estimate_minutes, subtask_total, subtask_done) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)"); err != nil {
		return nil, err
	}
	// task_dependencies edges: one row per [blocked_by:: ((uuid))] ref on a
	// task line. The cascade clear already removed the block's prior edges
	// (both FKs ON DELETE CASCADE), so each insert here is additive.
	if s.taskDep, err = tx.Prepare("INSERT OR IGNORE INTO task_dependencies (block_id, blocked_by_id) VALUES (?, ?)"); err != nil {
		return nil, err
	}
	if s.tag, err = tx.Prepare("INSERT INTO tags (block_id, raw_path, level_0, level_1, level_2) VALUES (?, ?, ?, ?, ?)"); err != nil {
		return nil, err
	}
	// page_links reverse index (#545): one row per [[target]] occurrence in a
	// block body. The per-block blocks-row DELETE cascades to page_links via
	// FK ON DELETE CASCADE, so each insert here is additive. target_* are left
	// NULL — resolution happens on demand (ResolvePageLink) so re-indexing
	// never needs the full pages list. `source` carries the root discriminator
	// so same-named notebooks across roots produce distinct rows. INSERT OR
	// IGNORE keeps the PK unique when the same target appears twice in one block.
	if s.pageLink, err = tx.Prepare("INSERT OR IGNORE INTO page_links (source, source_notebook, source_section, source_page, source_block_id, target_raw, target_notebook, target_section, target_page, heading, alias) VALUES (?, ?, ?, ?, ?, ?, NULL, NULL, NULL, ?, ?)"); err != nil {
		return nil, fmt.Errorf("failed to prepare page_links insert: %w", err)
	}
	// block_meta upsert (#418): sparse projection — one row per NOTE block
	// carrying [author::] and/or [ts::]. UPSERT by block_id so re-indexing the
	// same block overwrites stale values. The per-block DELETE cascades to
	// block_meta, so a block whose tokens were REMOVED no longer has a row
	// after re-index — handled by the delete-then-insert flow plus the
	// explicit DELETE for the "tokens cleared" case in the loop.
	if s.blockMetaUpsert, err = tx.Prepare("INSERT INTO block_meta (block_id, author, timestamp) VALUES (?, ?, ?) ON CONFLICT(block_id) DO UPDATE SET author=excluded.author, timestamp=excluded.timestamp"); err != nil {
		return nil, fmt.Errorf("failed to prepare block_meta upsert: %w", err)
	}
	if s.blockMetaClear, err = tx.Prepare("DELETE FROM block_meta WHERE block_id = ?"); err != nil {
		return nil, fmt.Errorf("failed to prepare block_meta clear: %w", err)
	}
	// block_references reverse index (#704): one row per distinct
	// (source_block_id, target_block_id, kind) edge extracted from RawText.
	// The per-block DELETE cascades to block_references, so each insert is
	// additive. INSERT OR IGNORE keeps the PK unique. Includes CODE blocks.
	if s.blockRef, err = tx.Prepare("INSERT OR IGNORE INTO block_references (source_block_id, target_block_id, kind) VALUES (?, ?, ?)"); err != nil {
		return nil, fmt.Errorf("failed to prepare block_references insert: %w", err)
	}
	return s, nil
}

// close releases every prepared statement. Statements are tx-scoped; closing
// before commit/rollback is the established hygiene (the prior inline code
// deferred each Close).
func (s *blockIndexStmts) close() {
	if s.block != nil {
		s.block.Close()
	}
	if s.task != nil {
		s.task.Close()
	}
	if s.taskDep != nil {
		s.taskDep.Close()
	}
	if s.tag != nil {
		s.tag.Close()
	}
	if s.pageLink != nil {
		s.pageLink.Close()
	}
	if s.blockMetaUpsert != nil {
		s.blockMetaUpsert.Close()
	}
	if s.blockMetaClear != nil {
		s.blockMetaClear.Close()
	}
	if s.blockRef != nil {
		s.blockRef.Close()
	}
}

// indexBlocks runs the shared per-block insert loop for one file's blocks:
// blocks, tasks (+ derived caches), tags (with frontmatter attach to the first
// block), block_references, page_links (CODE excluded), and block_meta. The
// caller has already cleared prior rows for these IDs and resolved
// source/notebook/section/page. fileTags are the file-level frontmatter tags
// (attached to the first parsed block); logTag prefixes diagnostic logs so
// failures stay attributable to the originating indexer.
func (s *blockIndexStmts) indexBlocks(source, notebook, section, page string, blocks []parser.ParsedBlock, fileTags []string, logTag string) error {
	// Derived task caches: descendant NOTE comments, direct subtask rollups
	// (#434), plus modified_at / estimate_minutes projections (#439/#440).
	commentsByTask, subtaskTotalByTask, subtaskDoneByTask := taskDerivedCounts(blocks)

	for blockIdx, block := range blocks {
		// 1. Insert into blocks — each block carries its own file_date.
		var parentID interface{}
		if block.ParentID != "" {
			parentID = block.ParentID
		}
		fileDate := block.FileDate
		if fileDate == "" {
			fileDate = time.Now().Format("2006-01-02")
		}
		if _, err := s.block.Exec(block.ID, parentID, source, notebook, section, page, fileDate, block.Depth, string(block.Type), block.RawText, block.CleanText, block.LineNumber); err != nil {
			return fmt.Errorf("failed to insert block %s: %w", block.ID, err)
		}

		// 2. Insert task metadata if it's a task.
		if block.Type == parser.BlockTask {
			var owner, startDate, dueDate interface{}
			if block.Owner != "" {
				owner = block.Owner
			}
			if block.StartDate != "" {
				startDate = block.StartDate
			}
			if block.DueDate != "" {
				dueDate = block.DueDate
			}
			// recur is a nullable string: NULL for one-off tasks (no
			// [recur::] token), the canonical rule otherwise (#296).
			var recurVal interface{}
			if block.Recurrence != "" {
				recurVal = block.Recurrence
			}
			// Lifecycle timestamps + manual order (#417): nullable caches
			// re-derivable from the [created::], [completed::], [order::]
			// tokens. Empty/0 → NULL (token absent). The dates are stored
			// verbatim (ISO 8601 local, no timezone normalization) so the
			// value round-trips byte-for-byte through markdown.
			var createdAtVal, completedAtVal interface{}
			if block.CreatedAt != "" {
				createdAtVal = block.CreatedAt
			}
			if block.CompletedAt != "" {
				completedAtVal = block.CompletedAt
			}
			var manualOrderVal interface{}
			if block.ManualOrder > 0 {
				manualOrderVal = block.ManualOrder
			}
			// [modified::] / [estimate::] projections (#439/#440): empty or
			// unparseable → NULL. Raw estimate stays in markdown only.
			var modifiedAtVal interface{}
			if block.ModifiedAt != "" {
				modifiedAtVal = block.ModifiedAt
			}
			estimateMinsVal := taskEstimateMinutes(block.Estimate)
			// Pin projection (#135): the column accepts NULL/0/1 so the
			// cache can represent the parser's tri-state — NULL when no
			// [pin::] token is present (nil), 0 for an explicit [pin::
			// false] (&false), 1 for [pin:: true] (&true). The column is
			// reproducible cache; the markdown is the source of truth.
			var pinnedVal sql.NullInt64
			if block.Pinned != nil {
				pinnedVal = sql.NullInt64{Int64: 0, Valid: true}
				if *block.Pinned {
					pinnedVal = sql.NullInt64{Int64: 1, Valid: true}
				}
			}
			linksCount := len(parser.BlockRefRegex.FindAllString(block.RawText, -1))
			if _, err := s.task.Exec(block.ID, block.Status, owner, startDate, dueDate, block.Priority, pinnedVal, block.Progress, recurVal, commentsByTask[block.ID], linksCount, createdAtVal, completedAtVal, manualOrderVal, modifiedAtVal, estimateMinsVal, subtaskTotalByTask[block.ID], subtaskDoneByTask[block.ID]); err != nil {
				return fmt.Errorf("failed to insert task for block %s: %w", block.ID, err)
			}
		}

		// 3. Extract and insert tags for this block.
		tags := ExtractTags(block.RawText)
		// Attach file-level frontmatter tags to the first parsed block. The
		// previous implementation checked block.LineNumber == 1, which is
		// never true when the file has YAML frontmatter (the first block
		// sits after the closing `---`).
		if blockIdx == 0 {
			for _, ft := range fileTags {
				trimmedFT := strings.TrimPrefix(ft, "#")
				found := false
				for _, t := range tags {
					if t == trimmedFT {
						found = true
						break
					}
				}
				if !found && trimmedFT != "" {
					tags = append(tags, trimmedFT)
				}
			}
		}

		for _, tagPath := range tags {
			parts := strings.Split(tagPath, "/")
			var level0, level1, level2 interface{}
			if len(parts) > 0 {
				level0 = parts[0]
			}
			if len(parts) > 1 {
				level1 = parts[1]
			}
			if len(parts) > 2 {
				level2 = parts[2]
			}
			if _, err := s.tag.Exec(block.ID, tagPath, level0, level1, level2); err != nil {
				// PRIMARY KEY is (block_id, raw_path) so most collisions
				// are just duplicate tags, but we log so a real DB error
				// (constraint violation from a schema change) stays visible.
				log.Printf("db.%s: tag insert error for block %s tag %q: %v", logTag, block.ID, tagPath, err)
				continue
			}
		}

		// block_references reverse index (#704): extract every ((uuid)) and
		// {{embed:uuid}} token from RawText. Includes CODE blocks (diverges
		// from page_links below, which excludes CODE); the cascade through
		// the per-block DELETE cleared the prior edge set. Dangling target
		// IDs are retained by design (source-only FK).
		if err := indexBlockReferences(s.blockRef, block.ID, block.RawText); err != nil {
			return fmt.Errorf("failed to index block_references for block %s: %w", block.ID, err)
		}

		// page_links reverse index (#545): extract every [[target]] /
		// [[target#heading]] / [[target|alias]] from the block body. CODE
		// blocks are excluded — literal [[…]] inside fenced code is not a
		// link and must not be rewritten on rename.
		if block.Type == parser.BlockCode {
			continue
		}
		for _, pl := range parser.PageLinkRegex.FindAllStringSubmatch(block.CleanText, -1) {
			target := pl[1]
			if target == "" {
				continue
			}
			var heading, alias interface{}
			if pl[2] != "" {
				heading = pl[2]
			}
			if pl[3] != "" {
				alias = pl[3]
			}
			if _, err := s.pageLink.Exec(source, notebook, section, page, block.ID, target, heading, alias); err != nil {
				log.Printf("db.%s: page_link insert error for block %s target %q: %v", logTag, block.ID, target, err)
				continue
			}
		}

		// 4. block_meta projection (#418): NOTE blocks carrying [author::]
		// and/or [ts::] get a sparse projection row. NOTE-only by
		// construction (scanTaskTokens has no author/ts cases). A NOTE block
		// whose tokens were cleared since the last index gets its stale row
		// deleted so the table stays sparse and matches the markdown source.
		if block.Type == parser.BlockNote {
			if block.Author != "" || block.Timestamp != "" {
				if _, err := s.blockMetaUpsert.Exec(block.ID, nullIfEmpty(block.Author), nullIfEmpty(block.Timestamp)); err != nil {
					return fmt.Errorf("failed to upsert block_meta for block %s: %w", block.ID, err)
				}
			} else if _, err := s.blockMetaClear.Exec(block.ID); err != nil {
				return fmt.Errorf("failed to clear block_meta for block %s: %w", block.ID, err)
			}
		}
	}
	return nil
}

// indexTaskDependencies runs the [blocked_by:: ((uuid))] second pass: probe the
// just-inserted blocks for each dependency target's existence, then insert the
// edge. Runs AFTER every block row is inserted (#301) because a task may
// reference a block that appears later in the file (or in a different file for
// the batched path, which passes every result's blocks flattened).
//
// The existence probe is required because INSERT OR IGNORE does NOT suppress FK
// violations (SQLite conflict resolution excludes foreign keys): an absent
// target would raise SQLITE_CONSTRAINT ForeignKey and abort the whole re-index.
// Absent refs are skipped with a warning; the markdown round-trips the token so
// the edge re-materializes if the target is ever indexed.
func indexTaskDependencies(tx *sql.Tx, stmtTaskDep *sql.Stmt, blocks []parser.ParsedBlock, logTag string) error {
	stmtDepExists, err := tx.Prepare("SELECT 1 FROM blocks WHERE id = ? LIMIT 1")
	if err != nil {
		return fmt.Errorf("failed to prepare dep-existence probe: %w", err)
	}
	defer stmtDepExists.Close()
	for _, block := range blocks {
		if block.Type != parser.BlockTask {
			continue
		}
		for _, depID := range block.BlockedBy {
			var ok int
			if err := stmtDepExists.QueryRow(depID).Scan(&ok); err != nil {
				// Target block doesn't exist (sql.ErrNoRows) — skip rather
				// than trip the FK. The markdown round-trips the token, so
				// the edge re-materializes if the target is ever indexed.
				log.Printf("db.%s: skipping dependency %s -> %s: target block not indexed", logTag, block.ID, depID)
				continue
			}
			if _, err := stmtTaskDep.Exec(block.ID, depID); err != nil {
				return fmt.Errorf("failed to insert task_dependency for block %s: %w", block.ID, err)
			}
		}
	}
	return nil
}

// ExtractTags finds inline tags starting with # followed by a letter, ignoring numeric priorities.
// Tag names may contain letters, digits, underscores, hyphens, and slashes
// (so #work/project/milestone-one is captured in full).
func ExtractTags(text string) []string {
	matches := tagRegex.FindAllStringSubmatch(text, -1)
	var tags []string
	seen := make(map[string]bool)
	for _, match := range matches {
		if len(match) > 1 {
			// Trim a trailing slash or hyphen so "#work-" doesn't store "work-".
			t := strings.TrimRight(match[1], "/-")
			if t == "" || seen[t] {
				continue
			}
			seen[t] = true
			tags = append(tags, t)
		}
	}
	return tags
}

// ClearFileBlocks deletes all blocks, tasks, and tags associated with a
// specific page on a given day, scoped to the notebook's source so a linked
// notebook sharing a display name with a vault notebook cannot clear the
// vault's rows (#100).
func (dm *DatabaseManager) ClearFileBlocks(tx *sql.Tx, source, notebook, section, page string) error {
	if source == "" {
		source = "vault"
	}
	query := "DELETE FROM blocks WHERE source = ? AND notebook = ? AND section = ? AND page = ?"
	// When the caller already holds a transaction (and typically a handle()
	// lease), use the tx only — re-entering handle() would deadlock on dbMu (#517).
	if tx != nil {
		_, err := tx.Exec(query, source, notebook, section, page)
		return err
	}
	db, release, err := dm.handle()
	if err != nil {
		return ErrDBClosed
	}
	defer release()
	_, err = db.Exec(query, source, notebook, section, page)
	return err
}

// DeleteBlockFromPage removes a single block by ID, but ONLY if it is at the
// specified source/notebook/section/page. This page-scoping is critical for
// the cross-page-move source-removal path: the block has already been indexed
// at the TARGET page by the first pass. A non-scoped delete would remove it
// from the target too (#104 concurrency fix).
func (dm *DatabaseManager) DeleteBlockFromPage(blockID, source, notebook, section, page string) error {
	db, release, err := dm.handle()
	if err != nil {
		return ErrDBClosed
	}
	defer release()
	if source == "" {
		source = "vault"
	}
	_, err = db.Exec(
		"DELETE FROM blocks WHERE id = ? AND source = ? AND notebook = ? AND section = ? AND page = ?",
		blockID, source, notebook, section, page)
	return err
}

// BlockIDsForPage returns the IDs of every block currently indexed for a page,
// without materializing the full ParsedBlock rows. Used by the eviction paths
// (DeletePage, watcher Remove/Rename, SaveFileBlocks replacement) to release the
// per-block mutex entries (#122) for blocks that no longer exist. Scoped by
// source (#100).
func (dm *DatabaseManager) BlockIDsForPage(source, notebook, section, page string) ([]string, error) {
	db, release, err := dm.handle()
	if err != nil {
		return nil, ErrDBClosed
	}
	defer release()
	if source == "" {
		source = "vault"
	}
	rows, err := db.Query(
		"SELECT id FROM blocks WHERE source = ? AND notebook = ? AND section = ? AND page = ?",
		source, notebook, section, page,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// CountBlocksForPage returns the number of managed blocks currently indexed for
// one (source, notebook, section, page) tuple. It mirrors BlockIDsForPage's
// WHERE shape but returns a COUNT so the watcher can read the PRIOR block count
// before a re-parse replaces the rows — the input to the mass-re-mint heuristic
// (#443). The count is derived working memory (ARCHITECTURE.md §0 rule 4): no
// schema change, re-derivable from the blocks table on demand.
func (dm *DatabaseManager) CountBlocksForPage(source, notebook, section, page string) (int, error) {
	db, release, err := dm.handle()
	if err != nil {
		return 0, ErrDBClosed
	}
	defer release()
	if source == "" {
		source = "vault"
	}
	var n int
	err = db.QueryRow(
		"SELECT COUNT(*) FROM blocks WHERE source = ? AND notebook = ? AND section = ? AND page = ?",
		source, notebook, section, page,
	).Scan(&n)
	if err != nil {
		return 0, err
	}
	return n, nil
}

// IndexFileBlocks updates the index with one file's blocks in a single
// transaction (the per-file / watcher / editor-save path).
//
// fileWarnings is an optional slice of non-fatal diagnostics from the parser
// (e.g. malformed YAML frontmatter). They are logged at warn level so a
// maintainer can grep the output without changing the call signature.
func (dm *DatabaseManager) IndexFileBlocks(source, notebook, section, page string, blocks []parser.ParsedBlock, fileTags []string, fileWarnings ...string) error {
	db, release, err := dm.handle()
	if err != nil {
		return ErrDBClosed
	}
	defer release()
	if source == "" {
		source = "vault"
	}
	for _, w := range fileWarnings {
		log.Printf("db.IndexFileBlocks(%s/%s/%s/%s): %s", source, notebook, section, page, w)
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Delete any pre-existing rows for the block IDs we're about to (re)insert.
	// Block IDs are stable across re-parses. Cascading FKs clean up their
	// related tasks and tags.
	if len(blocks) > 0 {
		placeholders := make([]string, len(blocks))
		args := make([]interface{}, len(blocks))
		for i, b := range blocks {
			placeholders[i] = "?"
			args[i] = b.ID
		}
		query := "DELETE FROM blocks WHERE id IN (" + strings.Join(placeholders, ",") + ")"
		if _, err := tx.Exec(query, args...); err != nil {
			return fmt.Errorf("failed to clear blocks by id: %w", err)
		}
	}

	// Also clear by metadata to catch blocks that the user removed from the
	// file (their IDs are no longer in the new parse output). Scope by source
	// so a linked notebook sharing a display name with a vault notebook cannot
	// clear the vault's rows (#100).
	if err := dm.ClearFileBlocks(tx, source, notebook, section, page); err != nil {
		return fmt.Errorf("failed to clear old blocks: %w", err)
	}

	if len(blocks) == 0 {
		return tx.Commit()
	}

	stmts, err := prepareBlockIndexStmts(tx)
	if err != nil {
		return err
	}
	defer stmts.close()

	if err := stmts.indexBlocks(source, notebook, section, page, blocks, fileTags, "IndexFileBlocks"); err != nil {
		return err
	}

	// Per-file cycle guard: only one file's edges are visible here, so this
	// detects within-file cycles only. The batched indexer's cross-file check
	// catches cycles that span files.
	warnOnDependencyCycle(blocks)
	if err := indexTaskDependencies(tx, stmts.taskDep, blocks, "IndexFileBlocks"); err != nil {
		return err
	}

	return tx.Commit()
}

// IndexScanResults inserts multiple scan results into the database in a single
// transaction (the batched vault-startup / linked-notebook path). It returns
// the count of files that were successfully indexed, plus a slice describing
// files that were skipped because the scanner reported a per-file error.
// Callers should surface the skipped set so users can distinguish a fully-loaded
// vault from one with unreadable files.
func (dm *DatabaseManager) IndexScanResults(results []parser.ScanResult) (int, []string, error) {
	db, release, err := dm.handle()
	if err != nil {
		return 0, nil, ErrDBClosed
	}
	defer release()
	tx, err := db.Begin()
	if err != nil {
		return 0, nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	stmts, err := prepareBlockIndexStmts(tx)
	if err != nil {
		return 0, nil, err
	}
	defer stmts.close()

	indexedCount := 0
	var skipped []string

	for _, res := range results {
		if res.Err != nil {
			skipped = append(skipped, fmt.Sprintf("%s: %v", res.Path, res.Err))
			continue
		}

		// Files that did not resolve to a notebook/section/page (e.g. live
		// too shallow in the vault) arrive with a warning and no notebook.
		// Surface them as skipped rather than indexing under empty strings.
		if res.Notebook == "" {
			for _, w := range res.Warnings {
				skipped = append(skipped, fmt.Sprintf("%s: %s", res.Path, w))
			}
			if len(res.Warnings) == 0 {
				skipped = append(skipped, fmt.Sprintf("%s: missing notebook/section/page", res.Path))
			}
			continue
		}

		// Clear any pre-existing rows for these block IDs (handles the case
		// where frontmatter metadata changed since the previous index, since
		// block IDs are stable but (notebook, section, date) is denormalized).
		if len(res.Blocks) > 0 {
			placeholders := make([]string, len(res.Blocks))
			args := make([]interface{}, len(res.Blocks))
			for i, b := range res.Blocks {
				placeholders[i] = "?"
				args[i] = b.ID
			}
			query := "DELETE FROM blocks WHERE id IN (" + strings.Join(placeholders, ",") + ")"
			if _, err := tx.Exec(query, args...); err != nil {
				return 0, skipped, fmt.Errorf("failed to clear blocks by id for %s: %w", res.Path, err)
			}
		}

		// Also clear by metadata to catch blocks the user removed from the file.
		// Source comes from the ScanResult so the batched linked-tree scan
		// (#134) scopes its own rows; the vault startup scan leaves Source
		// empty, defaulting to 'vault' (the historical behavior).
		source := res.Source
		if source == "" {
			source = "vault"
		}
		if err := dm.ClearFileBlocks(tx, source, res.Notebook, res.Section, res.Page); err != nil {
			return 0, skipped, fmt.Errorf("failed to clear blocks for %s: %w", res.Path, err)
		}

		if err := stmts.indexBlocks(source, res.Notebook, res.Section, res.Page, res.Blocks, res.Tags, "IndexScanResults"); err != nil {
			return 0, skipped, err
		}

		indexedCount++
	}

	// Cross-file cycle guard — the load-bearing divergence from
	// IndexFileBlocks' per-file warnOnDependencyCycle. Cycles can span files
	// (task in file A blocked-by task in file B blocked-by task in file A),
	// so the check must run over the union of every result's edges, AFTER the
	// whole results loop. A per-file check sees neither file as cyclic on its
	// own and would silently miss it. See TestIndexScanResults_CrossFileDependencyCycle.
	allEdges := make(map[string][]string)
	for _, res := range results {
		for _, b := range res.Blocks {
			if b.Type == parser.BlockTask && len(b.BlockedBy) > 0 {
				allEdges[b.ID] = append(allEdges[b.ID], b.BlockedBy...)
			}
		}
	}
	if len(allEdges) > 0 && dependencies.DetectsCycle(allEdges) {
		log.Printf("db: task_dependencies cycle detected across indexed files — a hand-edited or externally-synced file introduced a circular dependency; the setter normally prevents this")
	}

	// Dependency second pass over EVERY indexed file's blocks (flattened),
	// AFTER all block rows are inserted. Edges may cross file boundaries (#301).
	var allBlocks []parser.ParsedBlock
	for _, res := range results {
		allBlocks = append(allBlocks, res.Blocks...)
	}
	if err := indexTaskDependencies(tx, stmts.taskDep, allBlocks, "IndexScanResults"); err != nil {
		return 0, skipped, err
	}

	if err := tx.Commit(); err != nil {
		return 0, skipped, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return indexedCount, skipped, nil
}

// ClearSourceBlocks deletes every block (and, via CASCADE, its tasks/tags)
// for a given source. Used by UnlinkNotebook to drop a linked notebook's
// local index rows without touching the external files (#100).
func (dm *DatabaseManager) ClearSourceBlocks(source string) error {
	db, release, err := dm.handle()
	if err != nil {
		return ErrDBClosed
	}
	defer release()
	if source == "" {
		return nil
	}
	_, err = db.Exec("DELETE FROM blocks WHERE source = ?", source)
	return err
}
