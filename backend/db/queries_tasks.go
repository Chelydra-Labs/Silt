package db

// Task filter and dependency-graph read queries. One file per read domain in
// package db. Dependency helpers live here because they are task-graph reads
// used by the DONE guard and blocked_by setter. All methods stay on
// *DatabaseManager.

import (
	"database/sql"
	"fmt"
	"strings"

	"silt/backend/parser"
)

// QueryTasksWithFilters fetches task results matching the provided query filters.
func (dm *DatabaseManager) QueryTasksWithFilters(filter parser.TaskQueryFilter) ([]parser.TaskResult, error) {
	db, release, err := dm.handle()
	if err != nil {
		return nil, ErrDBClosed
	}
	defer release()
	baseQuery := `
		SELECT b.id, b.parent_id, b.source, b.notebook, b.section, b.page, b.file_date, b.depth, b.raw_content, b.clean_content, b.line_number,
		       t.status, t.owner, t.start_date, t.due_date, t.priority, t.pinned, t.recur, t.created_at, t.completed_at, t.manual_order,
		       t.modified_at, t.estimate_minutes, t.subtask_total, t.subtask_done
		FROM blocks b
		INNER JOIN tasks t ON b.id = t.block_id
		WHERE 1=1
	`

	var args []interface{}

	if filter.Owner != "" {
		baseQuery += " AND t.owner = ?"
		args = append(args, filter.Owner)
	}

	if filter.Priority > 0 {
		baseQuery += " AND t.priority = ?"
		args = append(args, filter.Priority)
	}

	if filter.StartDate != "" {
		baseQuery += " AND (t.start_date >= ? OR t.due_date >= ?)"
		args = append(args, filter.StartDate, filter.StartDate)
	}

	if filter.EndDate != "" {
		baseQuery += " AND (t.due_date <= ? OR t.start_date <= ?)"
		args = append(args, filter.EndDate, filter.EndDate)
	}

	if len(filter.Tags) > 0 {
		var tagConditions []string
		for _, tag := range filter.Tags {
			trimmedTag := strings.TrimPrefix(tag, "#")
			if trimmedTag != "" {
				tagConditions = append(tagConditions, "b.id IN (SELECT block_id FROM tags WHERE raw_path = ? OR raw_path LIKE ?)")
				args = append(args, trimmedTag, trimmedTag+"/%")
			}
		}
		if len(tagConditions) > 0 {
			baseQuery += " AND (" + strings.Join(tagConditions, " OR ") + ")"
		}
	}

	if len(filter.BlockIDs) > 0 {
		// Bound the query to an explicit ID set (GetTaskBlockers, #302).
		placeholders := make([]string, len(filter.BlockIDs))
		for i, id := range filter.BlockIDs {
			placeholders[i] = "?"
			args = append(args, id)
		}
		baseQuery += " AND b.id IN (" + strings.Join(placeholders, ",") + ")"
	}

	baseQuery += " ORDER BY b.file_date DESC, b.line_number ASC"

	rows, err := db.Query(baseQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query tasks: %w", err)
	}
	defer rows.Close()

	// Non-nil empty so Wails marshals JSON `[]` (not `null`) on zero matches —
	// same IPC contract as DistinctOwners.
	results := make([]parser.TaskResult, 0)
	var blockIDs []interface{}
	for rows.Next() {
		var r parser.TaskResult
		var parentID sql.NullString
		var status, owner, start, due, recur interface{}
		var priority int
		var pinned sql.NullInt64
		var createdAt, completedAt, modifiedAt interface{}
		var manualOrder, estimateMinutes, subtaskTotal, subtaskDone sql.NullInt64

		err := rows.Scan(
			&r.ID, &parentID, &r.Source, &r.Notebook, &r.Section, &r.Page, &r.FileDate, &r.Depth, &r.RawContent, &r.CleanContent, &r.LineNumber,
			&status, &owner, &start, &due, &priority, &pinned, &recur, &createdAt, &completedAt, &manualOrder,
			&modifiedAt, &estimateMinutes, &subtaskTotal, &subtaskDone,
		)
		if err != nil {
			return nil, err
		}
		if parentID.Valid {
			r.ParentID = parentID.String
		}

		if statusStr, ok := status.(string); ok {
			r.Status = statusStr
		}
		if ownerStr, ok := owner.(string); ok {
			r.Owner = ownerStr
		}
		if startStr, ok := start.(string); ok {
			r.StartDate = startStr
		}
		if dueStr, ok := due.(string); ok {
			r.DueDate = dueStr
		}
		r.Priority = priority
		// Hydrate the tri-state pin from the cache column (#135): NULL
		// stays nil (no [pin::] token), 0 -> &false ([pin:: false]), 1 ->
		// &true ([pin:: true]). Mirrors the IndexFileBlocks projection.
		if pinned.Valid {
			b := pinned.Int64 != 0
			r.Pinned = &b
		}
		if recurStr, ok := recur.(string); ok {
			r.Recurrence = recurStr
		}
		// Lifecycle metadata (#417): the nullable TEXT/INTEGER projections
		// of [created::], [completed::], [order::]. NULL stays empty/0.
		if s, ok := createdAt.(string); ok {
			r.CreatedAt = s
		}
		if s, ok := completedAt.(string); ok {
			r.CompletedAt = s
		}
		if manualOrder.Valid {
			r.ManualOrder = int(manualOrder.Int64)
		}
		// [modified::] / estimate_minutes / subtask rollups (#439/#440/#434).
		if s, ok := modifiedAt.(string); ok {
			r.ModifiedAt = s
		}
		if estimateMinutes.Valid {
			r.EstimateMinutes = int(estimateMinutes.Int64)
			r.Estimate = parser.FormatEstimateMinutes(r.EstimateMinutes)
		}
		if subtaskTotal.Valid {
			r.SubtaskTotal = int(subtaskTotal.Int64)
		}
		if subtaskDone.Valid {
			r.SubtaskDone = int(subtaskDone.Int64)
		}

		results = append(results, r)
		blockIDs = append(blockIDs, r.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed iterating task results: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	if len(results) == 0 {
		return results, nil
	}

	// Fetch all tags for the returned blocks in a single secondary query to
	// avoid the N+1 pattern of one SELECT per block.
	tagPlaceholders := make([]string, len(blockIDs))
	for i := range tagPlaceholders {
		tagPlaceholders[i] = "?"
	}
	tagQuery := "SELECT block_id, raw_path FROM tags WHERE block_id IN (" + strings.Join(tagPlaceholders, ",") + ") ORDER BY block_id, raw_path"
	tagRows, err := db.Query(tagQuery, blockIDs...)
	if err != nil {
		return nil, fmt.Errorf("failed to query task tags: %w", err)
	}
	defer tagRows.Close()

	tagIndex := make(map[string][]string, len(results))
	for tagRows.Next() {
		var blockID, tag string
		if err := tagRows.Scan(&blockID, &tag); err != nil {
			return nil, err
		}
		tagIndex[blockID] = append(tagIndex[blockID], tag)
	}
	if err := tagRows.Err(); err != nil {
		return nil, fmt.Errorf("failed iterating task tags: %w", err)
	}
	if err := tagRows.Close(); err != nil {
		return nil, err
	}

	for i := range results {
		if tags, ok := tagIndex[results[i].ID]; ok {
			results[i].Tags = tags
		}
	}

	// Hydrate BlockedBy from the task_dependencies join table via a single
	// secondary query (same N+1-avoidance pattern as tags above, #301). Each
	// row is one edge "block_id is blocked by blocked_by_id"; group them by
	// block_id. The Kanban/Agenda badge and the dependency picker read this
	// list without re-parsing markdown.
	depQuery := "SELECT block_id, blocked_by_id FROM task_dependencies WHERE block_id IN (" + strings.Join(tagPlaceholders, ",") + ") ORDER BY block_id, blocked_by_id"
	depRows, err := db.Query(depQuery, blockIDs...)
	if err != nil {
		return nil, fmt.Errorf("failed to query task dependencies: %w", err)
	}
	depIndex := make(map[string][]string, len(results))
	for depRows.Next() {
		var blockID, depID string
		if err := depRows.Scan(&blockID, &depID); err != nil {
			depRows.Close()
			return nil, err
		}
		depIndex[blockID] = append(depIndex[blockID], depID)
	}
	if err := depRows.Err(); err != nil {
		depRows.Close()
		return nil, fmt.Errorf("failed iterating task dependencies: %w", err)
	}
	if err := depRows.Close(); err != nil {
		return nil, err
	}
	for i := range results {
		if deps, ok := depIndex[results[i].ID]; ok {
			results[i].BlockedBy = deps
		}
	}

	return results, nil
}

// DistinctOwners returns the sorted, de-duplicated set of non-empty task owners
// across the whole vault, optionally filtered by a prefix. This is the
// read-only projection the @-mention typeahead (#184) offers: typing `@`
// surfaces every owner already assigned to a task. SQLite stays working memory
// — no mention state is stored here; the `@[name]` token round-trips through
// markdown as the source of truth.
//
// A non-empty prefix bounds the result server-side (#332): `LIKE 'prefix%'`
// narrows the scan so a vault with thousands of owners never ships an unbounded
// payload over IPC per keystroke. An empty prefix returns every owner (LIKE
// '%%') for the cache-seed path. SQLite LIKE is ASCII-case-insensitive, which
// matches the typeahead's case-insensitive client-side filter; ORDER BY uses the
// binary collation by default (uppercase before lowercase), so casing only
// affects ordering, not selection.
func (dm *DatabaseManager) DistinctOwners(prefix string) ([]string, error) {
	db, release, err := dm.handle()
	if err != nil {
		return nil, ErrDBClosed
	}
	defer release()
	rows, err := db.Query(
		"SELECT DISTINCT owner FROM tasks WHERE owner != '' AND owner LIKE ? ORDER BY owner",
		prefix+"%",
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query distinct owners: %w", err)
	}
	defer rows.Close()

	// Non-nil empty slice so the Wails binding marshals to JSON `[]` (not
	// `null`) for an empty vault — callers can `.filter`/iterate without a
	// null-guard.
	owners := make([]string, 0)
	for rows.Next() {
		var o string
		if err := rows.Scan(&o); err != nil {
			return nil, err
		}
		owners = append(owners, o)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed iterating distinct owners: %w", err)
	}
	return owners, nil
}

// OpenBlockers returns the IDs of a task's prerequisites that are not yet
// DONE — i.e. the unfinished blockers standing between this task and
// completion (#302). Used by the DONE-transition guard to decide whether to
// prompt the user, and by the badge to decide "blocked" state. An empty slice
// means the task is actionable (no open prerequisites). The reverse index
// idx_task_deps_blocked_by serves this lookup without a scan.
func (dm *DatabaseManager) OpenBlockers(blockID string) ([]string, error) {
	db, release, err := dm.handle()
	if err != nil {
		return nil, ErrDBClosed
	}
	defer release()
	rows, err := db.Query(
		`SELECT d.blocked_by_id FROM task_dependencies d
		 JOIN tasks t ON t.block_id = d.blocked_by_id
		 WHERE d.block_id = ? AND t.status != 'DONE'
		 ORDER BY d.blocked_by_id`,
		blockID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query open blockers for %s: %w", blockID, err)
	}
	defer rows.Close()
	out := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed iterating open blockers: %w", err)
	}
	return out, nil
}

// DependentsOf returns the IDs of tasks that are blocked by blockID — i.e. the
// tasks whose "blocked" state may flip when blockID is completed (#301
// reactive fan-out). The reverse index serves this lookup. An empty slice
// means nothing depends on blockID.
func (dm *DatabaseManager) DependentsOf(blockID string) ([]string, error) {
	db, release, err := dm.handle()
	if err != nil {
		return nil, ErrDBClosed
	}
	defer release()
	rows, err := db.Query(
		"SELECT block_id FROM task_dependencies WHERE blocked_by_id = ? ORDER BY block_id",
		blockID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query dependents of %s: %w", blockID, err)
	}
	defer rows.Close()
	out := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed iterating dependents: %w", err)
	}
	return out, nil
}

// DependencyEdges returns the full blocked-by edge set keyed by dependent task
// ID, restricted to the tasks whose IDs appear in `blockIDs`. Used by the IPC
// setter's cycle check to build the existing graph before proposing a new edge.
// Edges outside the requested set are irrelevant to a local cycle check and
// omitted to keep the payload bounded.
func (dm *DatabaseManager) DependencyEdges(blockIDs []string) (map[string][]string, error) {
	db, release, err := dm.handle()
	if err != nil {
		return nil, ErrDBClosed
	}
	defer release()
	if len(blockIDs) == 0 {
		return map[string][]string{}, nil
	}
	placeholders := make([]string, len(blockIDs))
	args := make([]interface{}, len(blockIDs))
	for i, id := range blockIDs {
		placeholders[i] = "?"
		args[i] = id
	}
	// Pull edges where either endpoint is in the set: a cycle through the
	// proposed edge may traverse nodes already known to the dependent side.
	query := "SELECT block_id, blocked_by_id FROM task_dependencies WHERE block_id IN (" + strings.Join(placeholders, ",") + ") OR blocked_by_id IN (" + strings.Join(placeholders, ",") + ")"
	// Duplicate args for the second IN clause.
	fullArgs := append(args, args...)
	rows, err := db.Query(query, fullArgs...)
	if err != nil {
		return nil, fmt.Errorf("failed to query dependency edges: %w", err)
	}
	defer rows.Close()
	out := make(map[string][]string)
	for rows.Next() {
		var from, to string
		if err := rows.Scan(&from, &to); err != nil {
			return nil, err
		}
		out[from] = append(out[from], to)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed iterating dependency edges: %w", err)
	}
	return out, nil
}

// ValidTaskBlockIDs returns the subset of `ids` that exist as TASK blocks in
// the index. Used by the dependency setter to reject non-existent or non-task
// prerequisites before writing the [blocked_by::] token — a stale/typo'd UUID
// or a non-task block would otherwise persist as a broken edge the index can't
// resolve (OpenBlockers JOINs tasks, so a non-task blocker never surfaces).
func (dm *DatabaseManager) ValidTaskBlockIDs(ids []string) (map[string]bool, error) {
	db, release, err := dm.handle()
	if err != nil {
		return nil, ErrDBClosed
	}
	defer release()
	out := make(map[string]bool, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	query := "SELECT id FROM blocks WHERE type = 'TASK' AND id IN (" + strings.Join(placeholders, ",") + ")"
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query valid task block ids: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}
