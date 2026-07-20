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

// warnOnDependencyCycle builds the [blocked_by::] edge map for a set of blocks
// and logs a warning when it contains a cycle (#301). The IPC setter prevents
// cycles at write time, but a hand-edited or externally-synced file (Obsidian,
// Dropbox) can still introduce one; this guard surfaces it so the user knows
// the dependency graph is inconsistent rather than silently caching a loop.
// The check is best-effort — it never blocks indexing or returns an error.
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

// IsFileUnchanged reports whether the file at `path` was previously indexed
// with the exact same mtime (Unix nanoseconds) and size. A warm restart uses
// this to skip re-parsing files the user has not touched since the last index.
func (dm *DatabaseManager) IsFileUnchanged(path string, mtime, size int64) (bool, error) {
	db, release, err := dm.handle()
	if err != nil {
		return false, ErrDBClosed
	}
	defer release()
	var fmtime, fsize int64
	err = db.QueryRow("SELECT mtime, size FROM files WHERE path = ?", path).Scan(&fmtime, &fsize)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to query files table: %w", err)
	}
	return fmtime == mtime && fsize == size, nil
}

// MarkFileIndexed records that the file at `path` was fully indexed with the
// given mtime/size. If tx is non-nil the upsert joins the caller's transaction
// (used by the bulk startup reindex so all per-file rows commit atomically);
// otherwise it runs against the shared connection.
func (dm *DatabaseManager) MarkFileIndexed(tx *sql.Tx, path string, mtime, size int64) error {
	now := time.Now().UnixNano()
	const q = "INSERT INTO files (path, mtime, size, indexed_at) VALUES (?, ?, ?, ?) " +
		"ON CONFLICT(path) DO UPDATE SET mtime=excluded.mtime, size=excluded.size, indexed_at=excluded.indexed_at"
	// Same lease rule as ClearFileBlocks: tx path must not re-enter handle().
	if tx != nil {
		_, err := tx.Exec(q, path, mtime, size, now)
		return err
	}
	db, release, err := dm.handle()
	if err != nil {
		return ErrDBClosed
	}
	defer release()
	_, err = db.Exec(q, path, mtime, size, now)
	return err
}

// FileIndexStat is one path's mtime/size for MarkFilesIndexed.
type FileIndexStat struct {
	Path  string
	MTime int64 // Unix nanoseconds
	Size  int64
}

// MarkFilesIndexed upserts many files-table rows in one transaction under a
// single read lease so App IPC does not call SQLDB().Begin across vault
// teardown. Empty input is a no-op. Nested MarkFileIndexed uses the tx path
// (must not re-enter handle).
func (dm *DatabaseManager) MarkFilesIndexed(files []FileIndexStat) error {
	if len(files) == 0 {
		return nil
	}
	db, release, err := dm.handle()
	if err != nil {
		return ErrDBClosed
	}
	defer release()
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin files-tx: %w", err)
	}
	defer tx.Rollback()
	for _, f := range files {
		if f.Path == "" {
			continue
		}
		if err := dm.MarkFileIndexed(tx, f.Path, f.MTime, f.Size); err != nil {
			return fmt.Errorf("MarkFileIndexed(%s): %w", f.Path, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit files-tx: %w", err)
	}
	return nil
}

// PruneStaleFiles deletes `files` rows for paths that are no longer present on
// disk (the file was deleted, moved, or renamed). `seenPaths` is the complete
// set of file paths the latest vault scan observed. Returns the pruned paths so
// callers can surface them as one-time init warnings (a renamed file shows up
// as "pruned old path + indexed new path").
func (dm *DatabaseManager) PruneStaleFiles(seenPaths []string) ([]string, error) {
	db, release, err := dm.handle()
	if err != nil {
		return nil, ErrDBClosed
	}
	defer release()
	// Build the parameter list for the "NOT IN (...)" clause. A single
	// round-trip DELETE keeps this cheap even for thousands of files.
	if len(seenPaths) == 0 {
		// No files on disk at all: drop every recorded row.
		_, err = db.Exec("DELETE FROM files")
		return nil, err
	}
	placeholders := make([]string, len(seenPaths))
	args := make([]interface{}, len(seenPaths))
	for i, p := range seenPaths {
		placeholders[i] = "?"
		args[i] = p
	}

	// Collect the about-to-be-pruned paths first so we can report them.
	rows, err := db.Query(
		"SELECT path FROM files WHERE path NOT IN ("+strings.Join(placeholders, ",")+")", args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query stale files: %w", err)
	}
	var pruned []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			rows.Close()
			return nil, err
		}
		pruned = append(pruned, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed iterating stale files: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	if len(pruned) > 0 {
		if _, err := db.Exec(
			"DELETE FROM files WHERE path NOT IN ("+strings.Join(placeholders, ",")+")", args...); err != nil {
			return nil, fmt.Errorf("failed to prune stale files: %w", err)
		}
	}
	return pruned, nil
}

// ForgetFile deletes the files-table row for a single path. Called by the
// watcher when a file is removed or renamed so the next startup scan does not
// treat the path as "unchanged" and skip re-indexing the new occupant.
func (dm *DatabaseManager) ForgetFile(path string) error {
	db, release, err := dm.handle()
	if err != nil {
		return ErrDBClosed
	}
	defer release()
	_, err = db.Exec("DELETE FROM files WHERE path = ?", path)
	return err
}

// KnownFiles returns the full path→FileStat map currently recorded in the
// index. Used for diagnostics (e.g. surfacing how many files are tracked).
func (dm *DatabaseManager) KnownFiles() (map[string]FileStat, error) {
	db, release, err := dm.handle()
	if err != nil {
		return nil, ErrDBClosed
	}
	defer release()
	rows, err := db.Query("SELECT path, mtime, size, indexed_at FROM files")
	if err != nil {
		return nil, fmt.Errorf("failed to query known files: %w", err)
	}
	defer rows.Close()
	out := make(map[string]FileStat)
	for rows.Next() {
		var path string
		var fs FileStat
		if err := rows.Scan(&path, &fs.MTime, &fs.Size, &fs.IndexedAt); err != nil {
			return nil, err
		}
		out[path] = fs
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed iterating known files: %w", err)
	}
	return out, nil
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

// IndexFileBlocks updates the index with a set of blocks in a single transaction.
//
// fileWarnings is an optional slice of non-fatal diagnostics from the parser
// (e.g. malformed YAML frontmatter). They are logged at warn level so a
// maintainer can grep the output without changing the call signature or
// the public API.
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

	stmtBlock, err := tx.Prepare("INSERT INTO blocks (id, parent_id, source, notebook, section, page, file_date, depth, type, raw_content, clean_content, line_number) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)")
	if err != nil {
		return err
	}
	defer stmtBlock.Close()

	stmtTask, err := tx.Prepare("INSERT INTO tasks (block_id, status, owner, start_date, due_date, priority, pinned, progress, recur, comments_count, links_count, created_at, completed_at, manual_order, modified_at, estimate_minutes, subtask_total, subtask_done) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)")
	if err != nil {
		return err
	}
	defer stmtTask.Close()

	// task_dependencies edges: one row per [blocked_by:: ((uuid))] ref on a
	// task line. The cascade clear above already removed the block's prior
	// edges (both FKs ON DELETE CASCADE), so each insert here is additive.
	stmtTaskDep, err := tx.Prepare("INSERT OR IGNORE INTO task_dependencies (block_id, blocked_by_id) VALUES (?, ?)")
	if err != nil {
		return err
	}
	defer stmtTaskDep.Close()

	stmtTag, err := tx.Prepare("INSERT INTO tags (block_id, raw_path, level_0, level_1, level_2) VALUES (?, ?, ?, ?, ?)")
	if err != nil {
		return err
	}
	defer stmtTag.Close()

	// page_links reverse index (#545): one row per [[target]] occurrence in a
	// block body. The per-block blocks-row DELETE above cascades to page_links
	// via FK ON DELETE CASCADE, so each insert here is additive. target_* are
	// left NULL — resolution happens on demand (ResolvePageLink) so re-indexing
	// never needs the full pages list. `source` carries the root discriminator
	// so same-named notebooks across roots produce distinct rows. INSERT OR
	// IGNORE keeps the PK unique when the same target appears twice in one block.
	stmtPageLink, err := tx.Prepare("INSERT OR IGNORE INTO page_links (source, source_notebook, source_section, source_page, source_block_id, target_raw, target_notebook, target_section, target_page, heading, alias) VALUES (?, ?, ?, ?, ?, ?, NULL, NULL, NULL, ?, ?)")
	if err != nil {
		return fmt.Errorf("failed to prepare page_links insert: %w", err)
	}
	defer stmtPageLink.Close()

	// block_meta upsert (#418): sparse projection — one row per NOTE block
	// carrying [author::] and/or [ts::]. UPSERT (ON CONFLICT replace) by
	// block_id so re-indexing the same block overwrites stale values rather
	// than accumulating duplicates. The per-block DELETE FROM blocks at the
	// top of this function cascades to block_meta via FK ON DELETE CASCADE,
	// so a block whose tokens were REMOVED in the new parse no longer has a
	// row here after the re-index — handled by the delete-then-insert flow
	// plus the explicit DELETE for the "tokens cleared" case below.
	stmtBlockMetaUpsert, err := tx.Prepare("INSERT INTO block_meta (block_id, author, timestamp) VALUES (?, ?, ?) ON CONFLICT(block_id) DO UPDATE SET author=excluded.author, timestamp=excluded.timestamp")
	if err != nil {
		return fmt.Errorf("failed to prepare block_meta upsert: %w", err)
	}
	defer stmtBlockMetaUpsert.Close()
	stmtBlockMetaClear, err := tx.Prepare("DELETE FROM block_meta WHERE block_id = ?")
	if err != nil {
		return fmt.Errorf("failed to prepare block_meta clear: %w", err)
	}
	defer stmtBlockMetaClear.Close()

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
		_, err = stmtBlock.Exec(block.ID, parentID, source, notebook, section, page, fileDate, block.Depth, string(block.Type), block.RawText, block.CleanText, block.LineNumber)
		if err != nil {
			return fmt.Errorf("failed to insert block %s: %w", block.ID, err)
		}

		// 2. Insert task metadata if it's a task
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
			_, err = stmtTask.Exec(block.ID, block.Status, owner, startDate, dueDate, block.Priority, pinnedVal, block.Progress, recurVal, commentsByTask[block.ID], linksCount, createdAtVal, completedAtVal, manualOrderVal, modifiedAtVal, estimateMinsVal, subtaskTotalByTask[block.ID], subtaskDoneByTask[block.ID])
			if err != nil {
				return fmt.Errorf("failed to insert task for block %s: %w", block.ID, err)
			}
		}

		// 3. Extract and insert tags for this block
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
			_, err = stmtTag.Exec(block.ID, tagPath, level0, level1, level2)
			if err != nil {
				// PRIMARY KEY is (block_id, raw_path) so most collisions
				// are just duplicate tags, but we log to stderr so a
				// real DB error (constraint violations from a schema
				// change, for example) is still visible during dev.
				log.Printf("db.IndexFileBlocks: tag insert error for block %s tag %q: %v", block.ID, tagPath, err)
				continue
			}
		}

		// page_links reverse index (#545): extract every [[target]] /
		// [[target#heading]] / [[target|alias]] occurrence from the block
		// body. CODE blocks are excluded — literal [[…]] inside fenced code
		// is not a link and must not be rewritten on rename.
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
			if _, err := stmtPageLink.Exec(source, notebook, section, page, block.ID, target, heading, alias); err != nil {
				log.Printf("db.IndexFileBlocks: page_link insert error for block %s target %q: %v", block.ID, target, err)
				continue
			}
		}

		// 4. block_meta projection (#418): NOTE blocks carrying
		// [author::] and/or [ts::] get a sparse projection row. NOTE-only
		// by construction (scanTaskTokens has no author/ts cases). A NOTE
		// block whose tokens were cleared since the last index (both now
		// empty) gets its stale row deleted so the table stays sparse and
		// matches the markdown source of truth. Non-NOTE blocks never
		// carry these fields.
		if block.Type == parser.BlockNote {
			if block.Author != "" || block.Timestamp != "" {
				if _, err := stmtBlockMetaUpsert.Exec(block.ID, nullIfEmpty(block.Author), nullIfEmpty(block.Timestamp)); err != nil {
					return fmt.Errorf("failed to upsert block_meta for block %s: %w", block.ID, err)
				}
			} else if _, err := stmtBlockMetaClear.Exec(block.ID); err != nil {
				return fmt.Errorf("failed to clear block_meta for block %s: %w", block.ID, err)
			}
		}
	}

	// Cache [blocked_by:: ((uuid))] edges in a second pass, AFTER every block
	// row is inserted (#301). A task may reference a block that appears later
	// in the file, so edges can't be inserted inline during the first pass
	// without tripping the blocks(id) foreign key. The per-block clear at the
	// top of this function (cascade on block-id delete) already removed the
	// prior edge set, so each insert here is additive. INSERT OR IGNORE keeps
	// the (block_id, blocked_by_id) PRIMARY KEY unique even if the parser
	// handed back a duplicate.
	warnOnDependencyCycle(blocks)
	// Existence probe for dependency targets. A [blocked_by::] ref may point
	// at a block that was deleted, a typo'd/hand-edited UUID, or a cross-file
	// ref to a page not yet indexed. INSERT OR IGNORE does NOT suppress FK
	// violations (SQLite conflict resolution excludes foreign keys), so an
	// absent target would raise SQLITE_CONSTRAINT ForeignKey and abort the
	// whole re-index. Probe within the transaction (just-inserted blocks are
	// visible) and skip absent refs with a warning, mirroring the cycle guard.
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
				log.Printf("db.IndexFileBlocks: skipping dependency %s -> %s: target block not indexed", block.ID, depID)
				continue
			}
			if _, err := stmtTaskDep.Exec(block.ID, depID); err != nil {
				return fmt.Errorf("failed to insert task_dependency for block %s: %w", block.ID, err)
			}
		}
	}

	return tx.Commit()
}

// IndexScanResults inserts multiple scan results into the database in a single
// transaction. It returns the count of files that were successfully indexed,
// plus a slice describing files that were skipped because the scanner
// reported a per-file error. Callers should surface the skipped set so
// users can distinguish a fully-loaded vault from one with unreadable files.
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

	stmtBlock, err := tx.Prepare("INSERT INTO blocks (id, parent_id, source, notebook, section, page, file_date, depth, type, raw_content, clean_content, line_number) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)")
	if err != nil {
		return 0, nil, err
	}
	defer stmtBlock.Close()

	stmtTask, err := tx.Prepare("INSERT INTO tasks (block_id, status, owner, start_date, due_date, priority, pinned, progress, recur, comments_count, links_count, created_at, completed_at, manual_order, modified_at, estimate_minutes, subtask_total, subtask_done) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)")
	if err != nil {
		return 0, nil, err
	}
	defer stmtTask.Close()

	// task_dependencies edges — mirror IndexFileBlocks (see comment there).
	stmtTaskDep, err := tx.Prepare("INSERT OR IGNORE INTO task_dependencies (block_id, blocked_by_id) VALUES (?, ?)")
	if err != nil {
		return 0, nil, err
	}
	defer stmtTaskDep.Close()

	stmtTag, err := tx.Prepare("INSERT INTO tags (block_id, raw_path, level_0, level_1, level_2) VALUES (?, ?, ?, ?, ?)")
	if err != nil {
		return 0, nil, err
	}
	defer stmtTag.Close()

	// page_links reverse index (#545) — mirror IndexFileBlocks (see comment
	// there). The block-row clear above cascades to page_links via FK.
	stmtPageLink, err := tx.Prepare("INSERT OR IGNORE INTO page_links (source, source_notebook, source_section, source_page, source_block_id, target_raw, target_notebook, target_section, target_page, heading, alias) VALUES (?, ?, ?, ?, ?, ?, NULL, NULL, NULL, ?, ?)")
	if err != nil {
		return 0, nil, fmt.Errorf("failed to prepare page_links insert: %w", err)
	}
	defer stmtPageLink.Close()

	// block_meta upsert/clear (#418) — mirror IndexFileBlocks (see comment
	// there). Sparse projection for NOTE blocks with [author::]/[ts::].
	stmtBlockMetaUpsert, err := tx.Prepare("INSERT INTO block_meta (block_id, author, timestamp) VALUES (?, ?, ?) ON CONFLICT(block_id) DO UPDATE SET author=excluded.author, timestamp=excluded.timestamp")
	if err != nil {
		return 0, nil, fmt.Errorf("failed to prepare block_meta upsert: %w", err)
	}
	defer stmtBlockMetaUpsert.Close()
	stmtBlockMetaClear, err := tx.Prepare("DELETE FROM block_meta WHERE block_id = ?")
	if err != nil {
		return 0, nil, fmt.Errorf("failed to prepare block_meta clear: %w", err)
	}
	defer stmtBlockMetaClear.Close()

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

		// Per-file derived caches — same as IndexFileBlocks (descendant
		// comments + direct subtask rollups + modified/estimate).
		commentsByTask, subtaskTotalByTask, subtaskDoneByTask := taskDerivedCounts(res.Blocks)

		for blockIdx, block := range res.Blocks {
			var parentID interface{}
			if block.ParentID != "" {
				parentID = block.ParentID
			}
			// Per-block file_date: the parser fills FileDate from the comment
			// or meta.Date before blocks reach either indexer. This fallback
			// is a last resort — kept consistent with IndexFileBlocks.
			fileDate := block.FileDate
			if fileDate == "" {
				fileDate = time.Now().Format("2006-01-02")
			}
			_, err = stmtBlock.Exec(block.ID, parentID, source, res.Notebook, res.Section, res.Page, fileDate, block.Depth, string(block.Type), block.RawText, block.CleanText, block.LineNumber)
			if err != nil {
				return 0, skipped, fmt.Errorf("failed to insert block %s: %w", block.ID, err)
			}

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
				var recurVal interface{}
				if block.Recurrence != "" {
					recurVal = block.Recurrence
				}
				// Lifecycle timestamps + manual order (#417) — mirror
				// IndexFileBlocks: empty/0 → NULL (token absent).
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
				var modifiedAtVal interface{}
				if block.ModifiedAt != "" {
					modifiedAtVal = block.ModifiedAt
				}
				estimateMinsVal := taskEstimateMinutes(block.Estimate)
				// Pin projection (#135): tri-state NULL/0/1 mirroring
				// IndexFileBlocks — NULL=absent, 0=[pin:: false], 1=[pin::
				// true]. Reproducible cache; markdown is source of truth.
				var pinnedVal sql.NullInt64
				if block.Pinned != nil {
					pinnedVal = sql.NullInt64{Int64: 0, Valid: true}
					if *block.Pinned {
						pinnedVal = sql.NullInt64{Int64: 1, Valid: true}
					}
				}
				linksCount := len(parser.BlockRefRegex.FindAllString(block.RawText, -1))
				_, err = stmtTask.Exec(block.ID, block.Status, owner, startDate, dueDate, block.Priority, pinnedVal, block.Progress, recurVal, commentsByTask[block.ID], linksCount, createdAtVal, completedAtVal, manualOrderVal, modifiedAtVal, estimateMinsVal, subtaskTotalByTask[block.ID], subtaskDoneByTask[block.ID])
				if err != nil {
					return 0, skipped, fmt.Errorf("failed to insert task for block %s: %w", block.ID, err)
				}
			}

			tags := ExtractTags(block.RawText)
			// Associate file frontmatter tags to the first parsed block.
			// The previous implementation checked block.LineNumber == 1,
			// which is never true when the file has YAML frontmatter.
			if blockIdx == 0 {
				for _, ft := range res.Tags {
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
				_, err = stmtTag.Exec(block.ID, tagPath, level0, level1, level2)
				if err != nil {
					log.Printf("db.IndexScanResults: tag insert error for block %s tag %q: %v", block.ID, tagPath, err)
					continue
				}
			}

			// page_links reverse index (#545) — mirror IndexFileBlocks.
			// CODE blocks are excluded (literal [[…]] in code is not a link).
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
				if _, err := stmtPageLink.Exec(source, res.Notebook, res.Section, res.Page, block.ID, target, heading, alias); err != nil {
					log.Printf("db.IndexScanResults: page_link insert error for block %s target %q: %v", block.ID, target, err)
					continue
				}
			}

			// block_meta projection (#418) — mirror IndexFileBlocks: NOTE
			// blocks with [author::]/[ts::] get a sparse row; a NOTE whose
			// tokens were cleared has its stale row deleted. NOTE-only.
			if block.Type == parser.BlockNote {
				if block.Author != "" || block.Timestamp != "" {
					if _, err := stmtBlockMetaUpsert.Exec(block.ID, nullIfEmpty(block.Author), nullIfEmpty(block.Timestamp)); err != nil {
						return 0, skipped, fmt.Errorf("failed to upsert block_meta for block %s: %w", block.ID, err)
					}
				} else if _, err := stmtBlockMetaClear.Exec(block.ID); err != nil {
					return 0, skipped, fmt.Errorf("failed to clear block_meta for block %s: %w", block.ID, err)
				}
			}
		}

		indexedCount++
	}

	// Cache [blocked_by:: ((uuid))] edges in a second pass over every indexed
	// file's blocks, AFTER all block rows are inserted (#301). Edges may cross
	// file boundaries (a task blocked-by a task in a different page), so this
	// pass runs after the whole results loop, not per-file. Mirrors the
	// IndexFileBlocks second pass; see comment there.
	//
	// Cycle guard: a hand-edited or externally-synced file can introduce a
	// cycle the setter would have refused. Check the accumulated edge set
	// across all results (cycles can span files) and log a warning. This
	// never blocks the batch — the index is best-effort consistent.
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
	// Existence probe for dependency targets — see IndexFileBlocks comment.
	// INSERT OR IGNORE doesn't suppress FK violations, and a single stale ref
	// anywhere in the vault would otherwise abort the whole cold scan.
	stmtDepExists, err := tx.Prepare("SELECT 1 FROM blocks WHERE id = ? LIMIT 1")
	if err != nil {
		return 0, skipped, fmt.Errorf("failed to prepare dep-existence probe: %w", err)
	}
	defer stmtDepExists.Close()
	for _, res := range results {
		for _, block := range res.Blocks {
			if block.Type != parser.BlockTask {
				continue
			}
			for _, depID := range block.BlockedBy {
				var ok int
				if err := stmtDepExists.QueryRow(depID).Scan(&ok); err != nil {
					log.Printf("db.IndexScanResults: skipping dependency %s -> %s: target block not indexed", block.ID, depID)
					continue
				}
				if _, err := stmtTaskDep.Exec(block.ID, depID); err != nil {
					return 0, skipped, fmt.Errorf("failed to insert task_dependency for block %s: %w", block.ID, err)
				}
			}
		}
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
