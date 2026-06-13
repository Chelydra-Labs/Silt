package db

import (
	"testing"

	"notes-sharp/backend/parser"
)

func newTestDB(t *testing.T) *DatabaseManager {
	t.Helper()
	dm, err := NewDatabaseManager()
	if err != nil {
		t.Fatalf("failed to create DatabaseManager: %v", err)
	}
	t.Cleanup(func() {
		_ = dm.Close()
	})
	return dm
}

func sampleTaskBlock(id string, line int) parser.ParsedBlock {
	return parser.ParsedBlock{
		ID:         id,
		Type:       parser.BlockTask,
		Depth:      0,
		RawText:    "- [ ] TODO TASK [Alice] sample task <!-- id: " + id + " -->",
		CleanText:  "sample task",
		Status:     "TODO",
		Owner:      "Alice",
		StartDate:  "2026-06-01",
		DueDate:    "2026-06-15",
		Priority:   2,
		LineNumber: line,
	}
}

func sampleNoteBlock(id string, line int) parser.ParsedBlock {
	return parser.ParsedBlock{
		ID:         id,
		Type:       parser.BlockNote,
		Depth:      0,
		RawText:    "a note <!-- id: " + id + " -->",
		CleanText:  "a note",
		LineNumber: line,
	}
}

func TestIndexFileBlocks_InsertsBlocksTasksAndTags(t *testing.T) {
	dm := newTestDB(t)

	blocks := []parser.ParsedBlock{
		sampleTaskBlock("11111111-1111-1111-1111-111111111111", 1),
		sampleNoteBlock("22222222-2222-2222-2222-222222222222", 2),
	}
	if err := dm.IndexFileBlocks("Work", "Journal", "2026-06-13", blocks, []string{"work/sogav"}); err != nil {
		t.Fatalf("IndexFileBlocks failed: %v", err)
	}

	var blockCount int
	if err := dm.db.QueryRow("SELECT COUNT(*) FROM blocks").Scan(&blockCount); err != nil {
		t.Fatalf("count blocks: %v", err)
	}
	if blockCount != 2 {
		t.Errorf("expected 2 blocks, got %d", blockCount)
	}

	var taskCount int
	if err := dm.db.QueryRow("SELECT COUNT(*) FROM tasks").Scan(&taskCount); err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	if taskCount != 1 {
		t.Errorf("expected 1 task row, got %d", taskCount)
	}

	var tagCount int
	if err := dm.db.QueryRow("SELECT COUNT(*) FROM tags").Scan(&tagCount); err != nil {
		t.Fatalf("count tags: %v", err)
	}
	// The task raw text has no inline #tag, so only the frontmatter tag is indexed.
	if tagCount != 1 {
		t.Errorf("expected 1 tag row (frontmatter only), got %d", tagCount)
	}

	// Inline tags in the raw text should also be indexed.
	blocksWithInlineTag := []parser.ParsedBlock{
		{
			ID:         "33333333-3333-3333-3333-333333333333",
			Type:       parser.BlockNote,
			RawText:    "remember to follow up on #work/sogav and #systems/specs <!-- id: 33333333-3333-3333-3333-333333333333 -->",
			CleanText:  "remember to follow up on",
			LineNumber: 1,
		},
	}
	if err := dm.IndexFileBlocks("Work", "Journal", "2026-06-14", blocksWithInlineTag, nil); err != nil {
		t.Fatalf("index inline tags: %v", err)
	}
	if err := dm.db.QueryRow("SELECT COUNT(*) FROM tags WHERE block_id = ?", "33333333-3333-3333-3333-333333333333").Scan(&tagCount); err != nil {
		t.Fatalf("count inline tags: %v", err)
	}
	if tagCount != 2 {
		t.Errorf("expected 2 inline tag rows, got %d", tagCount)
	}
}

func TestIndexFileBlocks_ReplacesExistingRows(t *testing.T) {
	dm := newTestDB(t)

	first := []parser.ParsedBlock{sampleTaskBlock("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", 1)}
	if err := dm.IndexFileBlocks("Work", "Journal", "2026-06-13", first, nil); err != nil {
		t.Fatalf("first IndexFileBlocks: %v", err)
	}

	second := []parser.ParsedBlock{
		sampleTaskBlock("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb", 1),
		sampleNoteBlock("cccccccc-cccc-cccc-cccc-cccccccccccc", 2),
	}
	if err := dm.IndexFileBlocks("Work", "Journal", "2026-06-13", second, nil); err != nil {
		t.Fatalf("second IndexFileBlocks: %v", err)
	}

	var count int
	if err := dm.db.QueryRow("SELECT COUNT(*) FROM blocks").Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 2 {
		t.Errorf("expected blocks to be replaced (2 rows), got %d", count)
	}

	// Old task row should be gone (CASCADE).
	var oldTasks int
	if err := dm.db.QueryRow("SELECT COUNT(*) FROM tasks WHERE block_id = ?", "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa").Scan(&oldTasks); err != nil {
		t.Fatalf("old task count: %v", err)
	}
	if oldTasks != 0 {
		t.Errorf("expected old task to be removed, got %d rows", oldTasks)
	}
}

func TestIndexFileBlocks_EmptyBlocksCommits(t *testing.T) {
	dm := newTestDB(t)

	// Seed with a block first.
	if err := dm.IndexFileBlocks("Work", "Journal", "2026-06-13", []parser.ParsedBlock{sampleTaskBlock("dddddddd-dddd-dddd-dddd-dddddddddddd", 1)}, nil); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Re-index with empty blocks should clear and commit successfully.
	if err := dm.IndexFileBlocks("Work", "Journal", "2026-06-13", nil, nil); err != nil {
		t.Fatalf("empty re-index: %v", err)
	}

	var count int
	if err := dm.db.QueryRow("SELECT COUNT(*) FROM blocks").Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Errorf("expected blocks to be cleared, got %d", count)
	}
}

func TestClearFileBlocks_CascadesToTasksAndTags(t *testing.T) {
	dm := newTestDB(t)

	blocks := []parser.ParsedBlock{sampleTaskBlock("eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee", 1)}
	if err := dm.IndexFileBlocks("Work", "Journal", "2026-06-13", blocks, []string{"cascade-tag"}); err != nil {
		t.Fatalf("index: %v", err)
	}

	if err := dm.ClearFileBlocks(nil, "Work", "Journal", "2026-06-13"); err != nil {
		t.Fatalf("clear: %v", err)
	}

	for _, table := range []string{"blocks", "tasks", "tags"} {
		var c int
		if err := dm.db.QueryRow("SELECT COUNT(*) FROM "+table).Scan(&c); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if c != 0 {
			t.Errorf("expected 0 rows in %s after cascade, got %d", table, c)
		}
	}
}

func TestQueryTasksWithFilters_FilterCombinations(t *testing.T) {
	dm := newTestDB(t)

	blocks := []parser.ParsedBlock{
		{
			ID:        "11111111-1111-1111-1111-111111111111",
			Type:      parser.BlockTask,
			RawText:   "- [x] DONE TASK [Alice]#1 ship <!-- id: 11111111-1111-1111-1111-111111111111 -->",
			CleanText: "ship",
			Status:    "DONE",
			Owner:     "Alice",
			Priority:  1,
			LineNumber: 1,
		},
		{
			ID:        "22222222-2222-2222-2222-222222222222",
			Type:      parser.BlockTask,
			RawText:   "- [/] DOING TASK [Bob]#2 fix <!-- id: 22222222-2222-2222-2222-222222222222 -->",
			CleanText: "fix",
			Status:    "DOING",
			Owner:     "Bob",
			Priority:  2,
			LineNumber: 1,
		},
		{
			ID:        "33333333-3333-3333-3333-333333333333",
			Type:      parser.BlockTask,
			RawText:   "- [ ] TODO TASK [Alice]#3 research <!-- id: 33333333-3333-3333-3333-333333333333 -->",
			CleanText: "research",
			Status:    "TODO",
			Owner:     "Alice",
			Priority:  3,
			LineNumber: 1,
		},
	}
	if err := dm.IndexFileBlocks("Work", "Journal", "2026-06-13", blocks, []string{"work/sogav"}); err != nil {
		t.Fatalf("index: %v", err)
	}

	tests := []struct {
		name     string
		filter   parser.TaskQueryFilter
		expected int
	}{
		{
			name:     "no filters returns all",
			filter:   parser.TaskQueryFilter{},
			expected: 3,
		},
		{
			name:     "filter by owner Alice",
			filter:   parser.TaskQueryFilter{Owner: "Alice"},
			expected: 2,
		},
		{
			name:     "filter by priority 2",
			filter:   parser.TaskQueryFilter{Priority: 2},
			expected: 1,
		},
		{
			name:     "filter by owner and priority",
			filter:   parser.TaskQueryFilter{Owner: "Alice", Priority: 1},
			expected: 1,
		},
		{
			name:     "filter by tag prefix",
			filter:   parser.TaskQueryFilter{Tags: []string{"work/sogav"}},
			expected: 3,
		},
		{
			name:     "filter excludes non-matching owner",
			filter:   parser.TaskQueryFilter{Owner: "nobody"},
			expected: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			results, err := dm.QueryTasksWithFilters(tc.filter)
			if err != nil {
				t.Fatalf("query: %v", err)
			}
			if len(results) != tc.expected {
				t.Errorf("expected %d results, got %d", tc.expected, len(results))
			}
		})
	}
}

func TestFetchTimelineDays_GroupsByDateAndOrdersDesc(t *testing.T) {
	dm := newTestDB(t)

	// Two days with multiple blocks each, plus an unrelated section to verify filtering.
	if err := dm.IndexFileBlocks("Work", "Journal", "2026-06-13", []parser.ParsedBlock{
		sampleTaskBlock("11111111-1111-1111-1111-111111111111", 1),
		sampleNoteBlock("22222222-2222-2222-2222-222222222222", 2),
	}, nil); err != nil {
		t.Fatalf("index day1: %v", err)
	}
	if err := dm.IndexFileBlocks("Work", "Journal", "2026-06-12", []parser.ParsedBlock{
		sampleNoteBlock("33333333-3333-3333-3333-333333333333", 1),
	}, nil); err != nil {
		t.Fatalf("index day2: %v", err)
	}
	if err := dm.IndexFileBlocks("Work", "Other", "2026-06-13", []parser.ParsedBlock{
		sampleTaskBlock("44444444-4444-4444-4444-444444444444", 1),
	}, nil); err != nil {
		t.Fatalf("index other section: %v", err)
	}

	groups, err := dm.FetchTimelineDays("Work", "Journal", 10, 0)
	if err != nil {
		t.Fatalf("FetchTimelineDays: %v", err)
	}
	if len(groups) != 2 {
		t.Fatalf("expected 2 day groups, got %d", len(groups))
	}
	if groups[0].Date != "2026-06-13" {
		t.Errorf("expected most recent date first, got %q", groups[0].Date)
	}
	if groups[1].Date != "2026-06-12" {
		t.Errorf("expected second date 2026-06-12, got %q", groups[1].Date)
	}
	if len(groups[0].Blocks) != 2 {
		t.Errorf("expected 2 blocks on 2026-06-13, got %d", len(groups[0].Blocks))
	}
	if len(groups[1].Blocks) != 1 {
		t.Errorf("expected 1 block on 2026-06-12, got %d", len(groups[1].Blocks))
	}
	if groups[0].FormattedDate == "" {
		t.Errorf("expected formatted date to be populated")
	}
}

func TestFetchTimelineDays_PaginationAndEmpty(t *testing.T) {
	dm := newTestDB(t)

	// Empty case.
	groups, err := dm.FetchTimelineDays("Work", "Journal", 10, 0)
	if err != nil {
		t.Fatalf("empty FetchTimelineDays: %v", err)
	}
	if len(groups) != 0 {
		t.Errorf("expected 0 groups for empty section, got %d", len(groups))
	}

	// Seed 3 distinct dates.
	for i, d := range []string{"2026-06-13", "2026-06-12", "2026-06-11"} {
		block := sampleNoteBlock("00000000-0000-0000-0000-00000000000"+string(rune('1'+i)), i+1)
		if err := dm.IndexFileBlocks("Work", "Journal", d, []parser.ParsedBlock{block}, nil); err != nil {
			t.Fatalf("index %s: %v", d, err)
		}
	}

	// First page: limit 2, offset 0.
	first, err := dm.FetchTimelineDays("Work", "Journal", 2, 0)
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	if len(first) != 2 {
		t.Fatalf("expected 2 groups on first page, got %d", len(first))
	}
	if first[0].Date != "2026-06-13" || first[1].Date != "2026-06-12" {
		t.Errorf("unexpected date order on first page: %s, %s", first[0].Date, first[1].Date)
	}

	// Second page: limit 2, offset 2.
	second, err := dm.FetchTimelineDays("Work", "Journal", 2, 2)
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	if len(second) != 1 {
		t.Fatalf("expected 1 group on second page, got %d", len(second))
	}
	if second[0].Date != "2026-06-11" {
		t.Errorf("expected third page date 2026-06-11, got %q", second[0].Date)
	}
}

func TestExtractTags_DeduplicatesAndIgnoresNumeric(t *testing.T) {
	text := "Plan #work/sogav with #work/sogav and #1 priority"
	tags := ExtractTags(text)
	if len(tags) != 1 || tags[0] != "work/sogav" {
		t.Errorf("expected single deduped tag [work/sogav], got %v", tags)
	}
}

func TestSQLDB_ExposesUnderlyingHandle(t *testing.T) {
	dm := newTestDB(t)
	if dm.SQLDB() == nil {
		t.Fatalf("expected SQLDB to return non-nil handle")
	}
}
