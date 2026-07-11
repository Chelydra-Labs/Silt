package db

import (
	"database/sql"
	"testing"

	"silt/backend/parser"
)

// TestIndexFileBlocks_LifecycleProjection verifies the [created::],
// [completed::], and [order::] task lifecycle tokens are projected into the
// nullable SQLite cache columns (#417): empty/0 → NULL, set values → stored
// verbatim. Both IndexFileBlocks and IndexScanResults must agree.
func TestIndexFileBlocks_LifecycleProjection(t *testing.T) {
	dm := newTestDB(t)

	mk := func(id, created, completed string, order int) parser.ParsedBlock {
		b := sampleTaskBlock(id, 1)
		b.CreatedAt = created
		b.CompletedAt = completed
		b.ManualOrder = order
		return b
	}
	blocks := []parser.ParsedBlock{
		mk("aaaaaaaa-1111-1111-1111-111111111111", "2026-07-01T09:00:00", "2026-07-06T14:00:00", 1),
		mk("bbbbbbbb-2222-2222-2222-222222222222", "2026-07-02T10:00:00", "", 2),
		mk("cccccccc-3333-3333-3333-333333333333", "", "", 0), // no lifecycle tokens → all NULL
	}
	if err := dm.IndexFileBlocks("vault", "Work", "Journal", "Daily", blocks, nil); err != nil {
		t.Fatalf("IndexFileBlocks: %v", err)
	}

	cases := []struct {
		id             string
		createdValid   bool
		createdVal     string
		completedValid bool
		completedVal   string
		orderValid     bool
		orderVal       int64
	}{
		{"aaaaaaaa-1111-1111-1111-111111111111", true, "2026-07-01T09:00:00", true, "2026-07-06T14:00:00", true, 1},
		{"bbbbbbbb-2222-2222-2222-222222222222", true, "2026-07-02T10:00:00", false, "", true, 2},
		{"cccccccc-3333-3333-3333-333333333333", false, "", false, "", false, 0},
	}
	for _, c := range cases {
		var created, completed sql.NullString
		var order sql.NullInt64
		if err := dm.SQLDB().QueryRow(
			"SELECT created_at, completed_at, manual_order FROM tasks WHERE block_id = ?", c.id,
		).Scan(&created, &completed, &order); err != nil {
			t.Fatalf("select lifecycle for %s: %v", c.id, err)
		}
		if created.Valid != c.createdValid {
			t.Errorf("%s: created_at valid=%v want %v", c.id, created.Valid, c.createdValid)
		} else if created.Valid && created.String != c.createdVal {
			t.Errorf("%s: created_at=%q want %q", c.id, created.String, c.createdVal)
		}
		if completed.Valid != c.completedValid {
			t.Errorf("%s: completed_at valid=%v want %v", c.id, completed.Valid, c.completedValid)
		} else if completed.Valid && completed.String != c.completedVal {
			t.Errorf("%s: completed_at=%q want %q", c.id, completed.String, c.completedVal)
		}
		if order.Valid != c.orderValid {
			t.Errorf("%s: manual_order valid=%v want %v", c.id, order.Valid, c.orderValid)
		} else if order.Valid && order.Int64 != c.orderVal {
			t.Errorf("%s: manual_order=%d want %d", c.id, order.Int64, c.orderVal)
		}
	}
}

// TestIndex_LifecycleRecoveryFromMarkdown verifies rule #4 (SQLite is
// reproducible working memory): a task carrying the lifecycle tokens can be
// wiped from the index and re-indexed from the markdown, and the lifecycle
// values are reproduced exactly. This is the "delete the index → rebuild"
// invariant that makes the cache disposable.
func TestIndex_LifecycleRecoveryFromMarkdown(t *testing.T) {
	dm := newTestDB(t)

	const (
		notebook = "Work"
		section  = "Journal"
		page     = "Daily"
		source   = "vault"
		taskID   = "deaddead-1111-1111-1111-111111111111"
	)
	// A markdown source with lifecycle tokens — exactly what the renderer
	// would emit for a created + completed task.
	md := "---\nnotebook: \"Work\"\nsection: \"Journal\"\npage: \"Daily\"\ndate: \"2026-07-06\"\ntags: []\n---\n" +
		"- [x] ship it [created:: 2026-07-01T09:00:00] [completed:: 2026-07-06T14:00:00] [order:: 5] <!-- id: " + taskID + " -->\n"

	// First index from the parsed markdown.
	blocks, meta, _, _, err := parser.ParseFileContent(md, notebook, section, page, "2026-07-06", 4)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0].CreatedAt != "2026-07-01T09:00:00" || blocks[0].CompletedAt != "2026-07-06T14:00:00" || blocks[0].ManualOrder != 5 {
		t.Fatalf("parser did not surface lifecycle values: %+v", blocks[0])
	}
	if err := dm.IndexFileBlocks(source, meta.Notebook, meta.Section, meta.Page, blocks, meta.Tags); err != nil {
		t.Fatalf("first index: %v", err)
	}

	// Wipe the index rows entirely (simulating an index deletion / corruption).
	if _, err := dm.SQLDB().Exec("DELETE FROM blocks WHERE id = ?", taskID); err != nil {
		t.Fatalf("wipe: %v", err)
	}

	// Re-index from the SAME markdown (the source of truth) and assert the
	// lifecycle values are reproduced exactly.
	if err := dm.IndexFileBlocks(source, meta.Notebook, meta.Section, meta.Page, blocks, meta.Tags); err != nil {
		t.Fatalf("re-index: %v", err)
	}
	var created, completed string
	var order int
	if err := dm.SQLDB().QueryRow(
		"SELECT created_at, completed_at, manual_order FROM tasks WHERE block_id = ?", taskID,
	).Scan(&created, &completed, &order); err != nil {
		// Columns are nullable; a bare Scan into non-nullable types fails on
		// NULL. They should NOT be NULL here (the tokens were present), so a
		// failure is a real regression.
		t.Fatalf("re-index select: %v", err)
	}
	if created != "2026-07-01T09:00:00" {
		t.Errorf("re-index created_at drift: got %q", created)
	}
	if completed != "2026-07-06T14:00:00" {
		t.Errorf("re-index completed_at drift: got %q", completed)
	}
	if order != 5 {
		t.Errorf("re-index manual_order drift: got %d", order)
	}

	// The QueryTasksWithFilters path must also hydrate the TaskResult fields.
	results, err := dm.QueryTasksWithFilters(parser.TaskQueryFilter{BlockIDs: []string{taskID}})
	if err != nil {
		t.Fatalf("QueryTasksWithFilters: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	r := results[0]
	if r.CreatedAt != "2026-07-01T09:00:00" {
		t.Errorf("TaskResult.CreatedAt drift: got %q", r.CreatedAt)
	}
	if r.CompletedAt != "2026-07-06T14:00:00" {
		t.Errorf("TaskResult.CompletedAt drift: got %q", r.CompletedAt)
	}
	if r.ManualOrder != 5 {
		t.Errorf("TaskResult.ManualOrder drift: got %d", r.ManualOrder)
	}
}

// TestIndexFileBlocks_ModifiedEstimateSubtasks verifies [modified::],
// [estimate::] minutes projection, descendant comments_count, and direct
// subtask_total/subtask_done rollups (#434/#439/#440).
func TestIndexFileBlocks_ModifiedEstimateSubtasks(t *testing.T) {
	dm := newTestDB(t)

	parentID := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	childDone := "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
	childTodo := "cccccccc-cccc-cccc-cccc-cccccccccccc"
	noteDirect := "dddddddd-dddd-dddd-dddd-dddddddddddd"
	noteNested := "eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee"
	noteOnNote := "ffffffff-ffff-ffff-ffff-ffffffffffff"

	parent := sampleTaskBlock(parentID, 1)
	parent.ModifiedAt = "2026-07-06T10:00:00"
	parent.Estimate = "2h"

	subDone := sampleTaskBlock(childDone, 2)
	subDone.ParentID = parentID
	subDone.Status = "DONE"
	subDone.Depth = 1

	subTodo := sampleTaskBlock(childTodo, 3)
	subTodo.ParentID = parentID
	subTodo.Status = "TODO"
	subTodo.Depth = 1

	// Direct NOTE under parent.
	n1 := sampleNoteBlock(noteDirect, 4)
	n1.ParentID = parentID
	n1.Depth = 1

	// Nested reply: NOTE under NOTE under parent — still counts as comment.
	n2 := sampleNoteBlock(noteNested, 5)
	n2.ParentID = noteDirect
	n2.Depth = 2

	// Another nested level under n2.
	n3 := sampleNoteBlock(noteOnNote, 6)
	n3.ParentID = noteNested
	n3.Depth = 3

	// Sibling task with invalid estimate → NULL minutes.
	lonely := sampleTaskBlock("11111111-1111-1111-1111-111111111111", 7)
	lonely.Estimate = "not-a-duration"
	lonely.ModifiedAt = ""

	blocks := []parser.ParsedBlock{parent, subDone, subTodo, n1, n2, n3, lonely}
	if err := dm.IndexFileBlocks("vault", "Work", "Journal", "Daily", blocks, nil); err != nil {
		t.Fatalf("IndexFileBlocks: %v", err)
	}

	var modified sql.NullString
	var estMins sql.NullInt64
	var comments, subTotal, subDoneCount int
	if err := dm.SQLDB().QueryRow(
		`SELECT modified_at, estimate_minutes, comments_count, subtask_total, subtask_done
		 FROM tasks WHERE block_id = ?`, parentID,
	).Scan(&modified, &estMins, &comments, &subTotal, &subDoneCount); err != nil {
		t.Fatalf("select parent: %v", err)
	}
	if !modified.Valid || modified.String != "2026-07-06T10:00:00" {
		t.Errorf("modified_at=%v want 2026-07-06T10:00:00", modified)
	}
	if !estMins.Valid || estMins.Int64 != 120 {
		t.Errorf("estimate_minutes=%v want 120", estMins)
	}
	if comments != 3 {
		t.Errorf("comments_count=%d want 3 (descendant NOTES)", comments)
	}
	if subTotal != 2 {
		t.Errorf("subtask_total=%d want 2", subTotal)
	}
	if subDoneCount != 1 {
		t.Errorf("subtask_done=%d want 1", subDoneCount)
	}

	// Invalid estimate → NULL minutes; empty modified → NULL.
	var lonelyMod sql.NullString
	var lonelyEst sql.NullInt64
	if err := dm.SQLDB().QueryRow(
		`SELECT modified_at, estimate_minutes FROM tasks WHERE block_id = ?`,
		"11111111-1111-1111-1111-111111111111",
	).Scan(&lonelyMod, &lonelyEst); err != nil {
		t.Fatalf("select lonely: %v", err)
	}
	if lonelyMod.Valid {
		t.Errorf("expected NULL modified_at, got %q", lonelyMod.String)
	}
	if lonelyEst.Valid {
		t.Errorf("expected NULL estimate_minutes for invalid raw, got %d", lonelyEst.Int64)
	}

	// Query path hydrates TaskResult fields.
	results, err := dm.QueryTasksWithFilters(parser.TaskQueryFilter{BlockIDs: []string{parentID}})
	if err != nil {
		t.Fatalf("QueryTasksWithFilters: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	r := results[0]
	if r.ModifiedAt != "2026-07-06T10:00:00" {
		t.Errorf("TaskResult.ModifiedAt=%q", r.ModifiedAt)
	}
	if r.EstimateMinutes != 120 {
		t.Errorf("TaskResult.EstimateMinutes=%d want 120", r.EstimateMinutes)
	}
	if r.SubtaskTotal != 2 || r.SubtaskDone != 1 {
		t.Errorf("TaskResult subtasks total=%d done=%d", r.SubtaskTotal, r.SubtaskDone)
	}
}
