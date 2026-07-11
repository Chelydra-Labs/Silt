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
