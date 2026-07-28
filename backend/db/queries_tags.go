package db

// Tag hierarchy and tag-scoped block read queries. Split from queries.go so
// the db package keeps one file per read domain. All methods stay on
// *DatabaseManager.

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"silt/backend/parser"
)

// QueryTagHierarchy returns the hierarchical tag tree with per-node distinct
// block counts. A node's count is the number of distinct blocks that are
// tagged at or beneath that path, so clicking #work surfaces every block
// reachable via #work or any of its descendants — without double-counting a
// block that happens to carry several nested tags (e.g. #work and
// #work/project/milestone-one).
func (dm *DatabaseManager) QueryTagHierarchy() ([]parser.TagNode, error) {
	db, release, err := dm.handle()
	if err != nil {
		return nil, ErrDBClosed
	}
	defer release()
	rows, err := db.Query("SELECT raw_path, block_id FROM tags")
	if err != nil {
		return nil, fmt.Errorf("failed to query tag hierarchy: %w", err)
	}
	defer rows.Close()

	// direct maps each exact raw_path to the set of block_ids tagged with it.
	// Keeping the set (rather than just a count) is what lets us compute a
	// node's count as the *distinct* number of blocks at-or-beneath it via
	// a bottom-up union pass over the trie.
	direct := map[string]map[string]struct{}{}
	for rows.Next() {
		var p, id string
		if err := rows.Scan(&p, &id); err != nil {
			return nil, err
		}
		if direct[p] == nil {
			direct[p] = make(map[string]struct{})
		}
		direct[p][id] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed iterating tag hierarchy: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	// Build a trie of every segment across all paths.
	type node struct {
		name     string
		path     string
		children map[string]*node
		// blocks is the union of (a) blocks tagged exactly at this path and
		// (b) blocks tagged at any descendant path. Populated bottom-up by
		// aggregate below.
		blocks map[string]struct{}
	}
	root := &node{name: "", path: "", children: map[string]*node{}}
	for p := range direct {
		segs := strings.Split(p, "/")
		cur := root
		acc := ""
		for i, seg := range segs {
			if i > 0 {
				acc += "/"
			}
			acc += seg
			child, ok := cur.children[seg]
			if !ok {
				child = &node{name: seg, path: acc, children: map[string]*node{}}
				cur.children[seg] = child
			}
			cur = child
		}
	}

	// Bottom-up pass: each node's blocks-set starts with the blocks tagged
	// exactly at that path, then absorbs the union of its children's sets.
	// The size of the resulting set is the count we surface to the UI.
	var aggregate func(n *node) map[string]struct{}
	aggregate = func(n *node) map[string]struct{} {
		merged := make(map[string]struct{}, len(direct[n.path]))
		for id := range direct[n.path] {
			merged[id] = struct{}{}
		}
		for _, child := range n.children {
			for id := range aggregate(child) {
				merged[id] = struct{}{}
			}
		}
		n.blocks = merged
		return merged
	}
	aggregate(root)

	var build func(parent *node) []parser.TagNode
	build = func(parent *node) []parser.TagNode {
		// Non-nil empty slice so leaf nodes serialize as JSON [] (not null);
		// the frontend dereferences node.children.length unconditionally.
		kids := make([]parser.TagNode, 0, len(parent.children))
		// Sort the children map by name for deterministic output independent
		// of Go's randomized map iteration order.
		names := make([]string, 0, len(parent.children))
		for name := range parent.children {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			child := parent.children[name]
			node := parser.TagNode{
				Name:  child.name,
				Path:  child.path,
				Count: len(child.blocks),
			}
			node.Children = build(child)
			kids = append(kids, node)
		}
		return kids
	}

	return build(root), nil
}

// QueryBlocksByTag returns blocks whose tag path equals tagPath or is nested
// beneath it (prefix semantics, so #work matches #work/project/milestone-one).
func (dm *DatabaseManager) QueryBlocksByTag(tagPath string) ([]parser.TaskResult, error) {
	db, release, err := dm.handle()
	if err != nil {
		return nil, ErrDBClosed
	}
	defer release()
	tagPath = strings.TrimSpace(strings.TrimPrefix(tagPath, "#"))
	if tagPath == "" {
		return []parser.TaskResult{}, nil
	}
	query := `
		SELECT b.id, b.parent_id, b.source, b.notebook, b.section, b.page, b.file_date, b.depth, b.raw_content, b.clean_content, b.line_number,
		       COALESCE(t.status, ''), COALESCE(t.owner, ''), COALESCE(t.start_date, ''), COALESCE(t.due_date, ''), COALESCE(t.priority, 0),
		       t.created_at, t.completed_at, t.manual_order, t.modified_at, t.estimate_minutes, t.subtask_total, t.subtask_done
		FROM blocks b
		LEFT JOIN tasks t ON b.id = t.block_id
		WHERE b.id IN (SELECT block_id FROM tags WHERE raw_path = ? OR raw_path LIKE ?)
		ORDER BY b.notebook, b.section, b.page, b.file_date DESC, b.line_number ASC
		LIMIT 500
	`
	rows, err := db.Query(query, tagPath, tagPath+"/%")
	if err != nil {
		return nil, fmt.Errorf("failed to query blocks by tag: %w", err)
	}
	defer rows.Close()

	var results []parser.TaskResult
	for rows.Next() {
		var r parser.TaskResult
		var parentID sql.NullString
		var status, owner, start, due string
		var priority int
		var createdAt, completedAt, modifiedAt sql.NullString
		var manualOrder, estimateMinutes, subtaskTotal, subtaskDone sql.NullInt64
		if err := rows.Scan(
			&r.ID, &parentID, &r.Source, &r.Notebook, &r.Section, &r.Page, &r.FileDate, &r.Depth, &r.RawContent, &r.CleanContent, &r.LineNumber,
			&status, &owner, &start, &due, &priority, &createdAt, &completedAt, &manualOrder, &modifiedAt, &estimateMinutes, &subtaskTotal, &subtaskDone,
		); err != nil {
			return nil, err
		}
		if parentID.Valid {
			r.ParentID = parentID.String
		}
		r.Status = status
		r.Owner = owner
		r.StartDate = start
		r.DueDate = due
		r.Priority = priority
		if createdAt.Valid {
			r.CreatedAt = createdAt.String
		}
		if completedAt.Valid {
			r.CompletedAt = completedAt.String
		}
		if manualOrder.Valid {
			r.ManualOrder = int(manualOrder.Int64)
		}
		if modifiedAt.Valid {
			r.ModifiedAt = modifiedAt.String
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
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed iterating blocks by tag: %w", err)
	}
	return results, nil
}
