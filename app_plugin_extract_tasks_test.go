package main

// End-to-end seam test for #1008: extract_and_save's commit path writes
// action items through PluginApplyBlocks with type TASK and marker-free
// text. This pins the real backend contract the tool depends on — the
// create path accepts TASK ops, the TASK renderer owns the single `- [ ] `
// marker emission, and the re-index lands the due date in the tasks table
// (what the task hub queries), not as inert NOTE text.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"silt/backend/parser"
)

func TestPluginApplyBlocks_ActionItemTaskCreate(t *testing.T) {
	app := newTestApp(t)
	notebook, section, page := "ExtractNB", "", "ActionItems"

	// Empty seeded target page, exactly like the extractor's createPage step.
	seedWriterFamilyPage(t, app, notebook, section, page, "eeee1008-1111-4111-8111-111111111111", "")

	// Op shape mirroring commitExtractAndSave's action-items mode: TASK type,
	// marker-free body, [due::] token, citation trailer on its own paragraph.
	err := app.applyBlocksOps([]PluginCreateBlockOp{{
		Kind:     "create",
		Type:     "TASK",
		Text:     "Set up Postgres replication [due:: 2026-08-01]\n\n<!-- src: eeee1008-1111-4111-8111-111111111111 mode:action_items -->",
		Notebook: notebook,
		Section:  section,
		Page:     page,
	}}, historyReasonPlugin)
	if err != nil {
		t.Fatalf("applyBlocksOps: %v", err)
	}

	path := filepath.Join(app.vaultPath, notebook, section, page+".md")
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read page: %v", err)
	}
	body := string(got)
	if n := strings.Count(body, "- [ ]"); n != 1 {
		t.Fatalf("expected exactly one task marker on disk, got %d:\n%s", n, body)
	}
	if !strings.Contains(body, "Set up Postgres replication") {
		t.Fatalf("task body missing on disk:\n%s", body)
	}
	if !strings.Contains(body, "[due:: 2026-08-01]") {
		t.Fatalf("due token missing on disk:\n%s", body)
	}
	// The citation trailer must survive the write (source traceability).
	if !strings.Contains(body, "<!-- src:") {
		t.Fatalf("citation comment missing on disk:\n%s", body)
	}

	// Re-parse the written file: the item is a TASK with a live due date.
	blocks, _, _, _, err := parser.ParseFileContent(body, notebook, section, page, "2026-01-01", app.spacesPerTab)
	if err != nil {
		t.Fatalf("ParseFileContent: %v", err)
	}
	var task *parser.ParsedBlock
	for i := range blocks {
		if blocks[i].Type == parser.BlockTask {
			task = &blocks[i]
			break
		}
	}
	if task == nil {
		t.Fatalf("no TASK block in written page:\n%s", body)
	}
	if task.DueDate != "2026-08-01" {
		t.Errorf("re-parsed task lost the due date, got %q", task.DueDate)
	}
	if strings.Contains(task.CleanText, "[due::") {
		t.Errorf("due token must live in the DueDate field, not CleanText: %q", task.CleanText)
	}

	// Hub visibility: the re-indexed tasks table (INNER JOIN on blocks) must
	// return the item with its due date — a NOTE write never lands here.
	results, err := app.db.QueryTasksWithFilters(parser.TaskQueryFilter{})
	if err != nil {
		t.Fatalf("QueryTasksWithFilters: %v", err)
	}
	found := false
	for _, r := range results {
		if r.Notebook == notebook && r.Page == page && r.DueDate == "2026-08-01" {
			found = true
		}
	}
	if !found {
		t.Fatalf("extracted action item not visible in the task index with its due date; got %+v", results)
	}
}
