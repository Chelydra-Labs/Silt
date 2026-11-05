package main

// End-to-end seam tests for #1008: extract_and_save's commit path writes
// action items through PluginApplyBlocks with type TASK and marker-free
// text. This pins the real backend contract the tool depends on — the
// create path accepts TASK ops, the TASK renderer owns the single `- [ ] `
// marker emission, and the re-index lands the due date in the tasks table
// (what the task hub queries), not as inert NOTE text.
//
// The op shape mirrors commitExtractAndSave after the #1039 harden pass:
// TASK bodies carry no citation comment — attribution rides as its own
// trailing NOTE op — and TASK creates are canonicalized through the parser
// so the first write is byte-stable.

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

	// Op shape mirroring commitExtractAndSave's action-items mode: TASK ops
	// with marker-free body + [due::] token and NO citation, followed by one
	// NOTE op carrying the citation (#1039 D1) so the task's clean_content —
	// what the hub renders verbatim into titles and aria-labels — stays free
	// of the HTML comment.
	err := app.applyBlocksOps([]PluginCreateBlockOp{
		{
			Kind:     "create",
			Type:     "TASK",
			Text:     "Set up Postgres replication [due:: 2026-08-01]",
			Notebook: notebook,
			Section:  section,
			Page:     page,
		},
		{
			Kind:     "create",
			Type:     "NOTE",
			Text:     "<!-- src: eeee1008-1111-4111-8111-111111111111 mode:action_items -->",
			Notebook: notebook,
			Section:  section,
			Page:     page,
		},
	}, historyReasonPlugin)
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
	// The citation trailer must survive the write (source traceability) —
	// on the attribution NOTE, not inside the task line.
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
	// D1 pin (b): the indexed task body must be display-clean — the hub
	// renders clean_content verbatim (titles, aria-labels), so no "<!--".
	if strings.Contains(task.CleanText, "<!--") {
		t.Errorf("task CleanText carries an HTML comment (citation pollution): %q", task.CleanText)
	}

	// D2 pin (a): the created line is canonical — a second render (what the
	// next full-page save does) is byte-identical, so the user never sees a
	// rewrite of a line they did not touch.
	var taskLine string
	for _, line := range strings.Split(body, "\n") {
		if strings.Contains(line, "- [ ] Set up Postgres replication") {
			taskLine = line
			break
		}
	}
	if taskLine == "" {
		t.Fatalf("task line not found on disk:\n%s", body)
	}
	rendered := parser.RenderFileContent([]parser.ParsedBlock{*task}, "", "", app.spacesPerTab)
	if rendered != taskLine {
		t.Errorf("created task line is not byte-stable on re-render\n disk: %q\nagain: %q", taskLine, rendered)
	}

	// Hub visibility: the re-indexed tasks table (INNER JOIN on blocks) must
	// return the item with its due date — a NOTE write never lands here —
	// and the row's clean_content must stay citation-free (D1 pin b, at the
	// exact row the hub renders).
	results, err := app.db.QueryTasksWithFilters(parser.TaskQueryFilter{})
	if err != nil {
		t.Fatalf("QueryTasksWithFilters: %v", err)
	}
	found := false
	for _, r := range results {
		if r.Notebook == notebook && r.Page == page && r.DueDate == "2026-08-01" {
			found = true
			if strings.Contains(r.CleanContent, "<!--") {
				t.Errorf("indexed task clean_content carries an HTML comment: %q", r.CleanContent)
			}
			if r.CleanContent != "Set up Postgres replication" {
				t.Errorf("indexed clean_content = %q, want the bare title", r.CleanContent)
			}
		}
	}
	if !found {
		t.Fatalf("extracted action item not visible in the task index with its due date; got %+v", results)
	}
}

// TestPluginApplyBlocks_TaskCreateEchoedTokens pins the #1039 D2 watchout:
// because the create path canonicalizes TASK ops through ParseLine, a
// model-echoed [priority::]/[owner::]/[due::] token in the op text becomes
// HONORED metadata — parsed into fields, stripped from the title, rendered
// in canonical token order — not inert description text.
func TestPluginApplyBlocks_TaskCreateEchoedTokens(t *testing.T) {
	app := newTestApp(t)
	notebook, section, page := "ExtractNB", "", "EchoedTokens"

	seedWriterFamilyPage(t, app, notebook, section, page, "eeee1008-2222-4222-8222-222222222222", "")

	err := app.applyBlocksOps([]PluginCreateBlockOp{{
		Kind:     "create",
		Type:     "TASK",
		Text:     "Ship the migration [priority:: 1] [owner:: alice] [due:: 2026-09-15]",
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
	if task.CleanText != "Ship the migration" {
		t.Errorf("echoed tokens must be stripped from the title, got CleanText %q", task.CleanText)
	}
	if task.Priority != 1 {
		t.Errorf("echoed [priority:: 1] not honored, got %d", task.Priority)
	}
	if task.Owner != "alice" {
		t.Errorf("echoed [owner:: alice] not honored, got %q", task.Owner)
	}
	if task.DueDate != "2026-09-15" {
		t.Errorf("echoed [due:: 2026-09-15] not honored, got %q", task.DueDate)
	}

	// First write is already canonical: token order fixed (priority before
	// due before owner) and byte-stable on the second render.
	var taskLine string
	for _, line := range strings.Split(body, "\n") {
		if strings.Contains(line, "Ship the migration") {
			taskLine = line
			break
		}
	}
	if taskLine == "" {
		t.Fatalf("task line not found on disk:\n%s", body)
	}
	if !strings.Contains(taskLine, "[priority:: 1] [due:: 2026-09-15] [owner:: alice]") {
		t.Errorf("canonical token order missing on first write: %q", taskLine)
	}
	rendered := parser.RenderFileContent([]parser.ParsedBlock{*task}, "", "", app.spacesPerTab)
	if rendered != taskLine {
		t.Errorf("created task line is not byte-stable on re-render\n disk: %q\nagain: %q", taskLine, rendered)
	}
}

// TestPluginApplyBlocks_TaskCreateMultilineTitle pins the #1039 iter-1 fix:
// an LLM title with an internal newline must still parse as a TASK (the
// checkbox regex is line-anchored), so the item reaches the hub instead of
// silently indexing as a NOTE.
func TestPluginApplyBlocks_TaskCreateMultilineTitle(t *testing.T) {
	app := newTestApp(t)
	notebook, section, page := "ExtractNB", "", "MultilineAction"

	seedWriterFamilyPage(t, app, notebook, section, page, "eeee1008-2222-4222-8222-222222222222", "")

	err := app.applyBlocksOps([]PluginCreateBlockOp{{
		Kind:     "create",
		Type:     "TASK",
		Text:     "Migrate the DB\nand verify replicas [due:: 2026-08-01]",
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
	if strings.Contains(body, "\n") && strings.Count(body, "- [ ]") != 1 {
		t.Fatalf("expected exactly one task marker on disk:\n%s", body)
	}

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
		t.Fatalf("multi-line title mis-typed as NOTE — no TASK block in written page:\n%s", body)
	}
	if task.DueDate != "2026-08-01" {
		t.Errorf("due date lost, got %q", task.DueDate)
	}
	if strings.Contains(task.CleanText, "\n") {
		t.Errorf("newline must be collapsed to a space in CleanText: %q", task.CleanText)
	}
}
