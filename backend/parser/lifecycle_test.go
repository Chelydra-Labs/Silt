package parser

import (
	"strings"
	"testing"
)

// TestParseLine_LifecycleTokens covers the [created::], [completed::], and
// [order::] Dataview inline metadata tokens (#417): parsing into the
// ParsedBlock fields, CleanText stripping, that none are echoed into
// ExtraTokens, and byte-for-byte round-trip stability through render.
//
// These are file-resident lifecycle tokens (ARCHITECTURE §0): created is
// minted once at task creation, completed tracks the most recent DONE
// transition, order is a 1-based sort position. Markdown is the source of
// truth; the renderer must omit each when at its zero value so existing
// tasks without the tokens stay exactly as they were (no backfill).
func TestParseLine_LifecycleTokens(t *testing.T) {
	t.Run("created/completed/order parsed into fields", func(t *testing.T) {
		line := "- [x] Ship it [created:: 2026-07-06T15:30:00] [completed:: 2026-07-06T16:00:00] [order:: 3] <!-- id: 11111111-1111-1111-1111-111111111111 -->"
		block, _, _ := ParseLine(line, 1, 4)
		if block.CreatedAt != "2026-07-06T15:30:00" {
			t.Errorf("expected CreatedAt='2026-07-06T15:30:00', got %q", block.CreatedAt)
		}
		if block.CompletedAt != "2026-07-06T16:00:00" {
			t.Errorf("expected CompletedAt='2026-07-06T16:00:00', got %q", block.CompletedAt)
		}
		if block.ManualOrder != 3 {
			t.Errorf("expected ManualOrder=3, got %d", block.ManualOrder)
		}
		if block.CleanText != "Ship it" {
			t.Errorf("expected CleanText='Ship it', got %q", block.CleanText)
		}
	})

	t.Run("tokens not duplicated into ExtraTokens", func(t *testing.T) {
		line := "- [ ] Task [created:: 2026-07-06T15:30:00] [order:: 1] [due:: 2026-08-01] <!-- id: 22222222-2222-2222-2222-222222222222 -->"
		block, _, _ := ParseLine(line, 1, 4)
		if block.CreatedAt == "" || block.ManualOrder == 0 {
			t.Fatalf("expected fields populated, got CreatedAt=%q ManualOrder=%d", block.CreatedAt, block.ManualOrder)
		}
		for _, tok := range block.ExtraTokens {
			lower := strings.ToLower(tok)
			if strings.Contains(lower, "created") || strings.Contains(lower, "order") || strings.Contains(lower, "completed") {
				t.Errorf("lifecycle token must not appear in ExtraTokens, found %q", tok)
			}
		}
	})

	t.Run("empty [created::]/[completed::] yield empty string, not ExtraTokens", func(t *testing.T) {
		line := "- [ ] Task [created:: ] [completed:: ] <!-- id: 33333333-3333-3333-3333-333333333333 -->"
		block, _, _ := ParseLine(line, 1, 4)
		if block.CreatedAt != "" {
			t.Errorf("expected empty CreatedAt, got %q", block.CreatedAt)
		}
		if block.CompletedAt != "" {
			t.Errorf("expected empty CompletedAt, got %q", block.CompletedAt)
		}
		if len(block.ExtraTokens) != 0 {
			t.Errorf("expected no ExtraTokens, got %v", block.ExtraTokens)
		}
	})

	t.Run("non-integer [order::] leaves ManualOrder at 0", func(t *testing.T) {
		line := "- [ ] Task [order:: abc] <!-- id: 44444444-4444-4444-4444-444444444444 -->"
		block, _, _ := ParseLine(line, 1, 4)
		if block.ManualOrder != 0 {
			t.Errorf("expected ManualOrder=0 for non-integer value, got %d", block.ManualOrder)
		}
	})
}

// TestRenderBlock_LifecycleOmitWhenDefault verifies the renderer omits each
// lifecycle token when its field is at the zero value — the no-backfill
// invariant. A task with no created/completed/order must render identically
// to pre-#417 output.
func TestRenderBlock_LifecycleOmitWhenDefault(t *testing.T) {
	block := ParsedBlock{
		ID:        "55555555-5555-5555-5555-555555555555",
		Type:      BlockTask,
		Status:    "TODO",
		CleanText: "legacy task",
		FileDate:  "2026-07-06",
	}
	out := renderBlock(block, 4)
	if strings.Contains(out, "[created::") {
		t.Errorf("expected no [created::] token, got: %s", out)
	}
	if strings.Contains(out, "[completed::") {
		t.Errorf("expected no [completed::] token, got: %s", out)
	}
	if strings.Contains(out, "[order::") {
		t.Errorf("expected no [order::] token, got: %s", out)
	}
}

// TestLifecycleTokens_RoundTripByteForByte is the critical rule-#1 invariant:
// parse a TASK with the 3 tokens → render → the rendered line contains the
// tokens verbatim, and parse → render → parse yields identical field values.
// A drift here means the markdown source of truth is being silently mutated.
func TestLifecycleTokens_RoundTripByteForByte(t *testing.T) {
	line := "- [x] Ship release [created:: 2026-07-06T15:30:00] [completed:: 2026-07-06T16:00:00] [order:: 7] <!-- id: 66666666-6666-6666-6666-666666666666 -->"

	parsed, _, _ := ParseLine(line, 1, 4)
	if parsed.CreatedAt != "2026-07-06T15:30:00" || parsed.CompletedAt != "2026-07-06T16:00:00" || parsed.ManualOrder != 7 {
		t.Fatalf("initial parse wrong: created=%q completed=%q order=%d",
			parsed.CreatedAt, parsed.CompletedAt, parsed.ManualOrder)
	}

	rendered := renderBlock(parsed, 4)
	// The three tokens must appear verbatim in the rendered line.
	for _, want := range []string{
		"[created:: 2026-07-06T15:30:00]",
		"[completed:: 2026-07-06T16:00:00]",
		"[order:: 7]",
	} {
		if !strings.Contains(rendered, want) {
			t.Errorf("rendered line missing %q:\n%s", want, rendered)
		}
	}

	// Re-parse and assert field-level identity.
	reparsed, _, _ := ParseLine(rendered, 1, 4)
	if reparsed.CreatedAt != parsed.CreatedAt {
		t.Errorf("CreatedAt drifted: %q → %q", parsed.CreatedAt, reparsed.CreatedAt)
	}
	if reparsed.CompletedAt != parsed.CompletedAt {
		t.Errorf("CompletedAt drifted: %q → %q", parsed.CompletedAt, reparsed.CompletedAt)
	}
	if reparsed.ManualOrder != parsed.ManualOrder {
		t.Errorf("ManualOrder drifted: %d → %d", parsed.ManualOrder, reparsed.ManualOrder)
	}

	// Second render must be byte-identical to the first (canonical form).
	rendered2 := renderBlock(reparsed, 4)
	if rendered != rendered2 {
		t.Errorf("render is not byte-stable across two passes\n--- pass1 ---\n%s\n--- pass2 ---\n%s", rendered, rendered2)
	}
}

// TestLifecycleTokens_FileLevelRoundTrip exercises the full
// ParseFileContent → RenderFileContent → ParseFileContent pipeline with
// lifecycle tokens present, asserting byte-stability of the rendered output
// across two passes (the rule-#1 invariant at the file level).
func TestLifecycleTokens_FileLevelRoundTrip(t *testing.T) {
	src := "---\nnotebook: \"work\"\nsection: \"\"\npage: \"plan\"\ndate: \"2026-07-06\"\ntags: []\n---\n" +
		"# Plan <!-- id: aaaaaaaa-1111-1111-1111-111111111111 -->\n" +
		"- [x] Done item [created:: 2026-07-01T09:00:00] [completed:: 2026-07-06T14:00:00] [order:: 1] <!-- id: aaaaaaaa-2222-2222-2222-111111111111 -->\n" +
		"- [ ] Open item [created:: 2026-07-02T10:00:00] [order:: 2] <!-- id: aaaaaaaa-3333-3333-3333-111111111111 -->\n"

	first, meta, _, _, err := ParseFileContent(src, "work", "", "plan", "2026-07-06", 4)
	if err != nil {
		t.Fatalf("first parse: %v", err)
	}
	if len(first) != 3 {
		t.Fatalf("expected 3 blocks, got %d", len(first))
	}
	if first[1].CreatedAt != "2026-07-01T09:00:00" || first[1].ManualOrder != 1 {
		t.Errorf("block 1 lifecycle wrong: created=%q order=%d", first[1].CreatedAt, first[1].ManualOrder)
	}

	fm, _ := SplitFrontmatter(src)
	rendered := RenderFileContent(first, "", fm, 4)
	rendered2 := RenderFileContent(first, "", fm, 4)
	if rendered != rendered2 {
		t.Errorf("file-level render not byte-stable:\n--- pass1 ---\n%s\n--- pass2 ---\n%s", rendered, rendered2)
	}
	// Tokens preserved verbatim in output.
	for _, want := range []string{
		"[created:: 2026-07-01T09:00:00]",
		"[completed:: 2026-07-06T14:00:00]",
		"[order:: 1]",
		"[order:: 2]",
	} {
		if !strings.Contains(rendered, want) {
			t.Errorf("rendered output missing %q:\n%s", want, rendered)
		}
	}
	// Re-parse from the rendered output and check the values survived.
	second, _, _, _, err := ParseFileContent(rendered, meta.Notebook, meta.Section, meta.Page, meta.Date, 4)
	if err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	if len(second) != 3 {
		t.Fatalf("expected 3 blocks after re-parse, got %d", len(second))
	}
	if second[1].CreatedAt != "2026-07-01T09:00:00" {
		t.Errorf("re-parse CreatedAt drift: %q", second[1].CreatedAt)
	}
	if second[1].CompletedAt != "2026-07-06T14:00:00" {
		t.Errorf("re-parse CompletedAt drift: %q", second[1].CompletedAt)
	}
	if second[1].ManualOrder != 1 || second[2].ManualOrder != 2 {
		t.Errorf("re-parse ManualOrder drift: %d, %d", second[1].ManualOrder, second[2].ManualOrder)
	}
}

// TestMinting_NoBackfillForExistingTasks verifies the no-backfill cardinal
// rule (#417): an existing TASK block that already has a block-identity id
// comment but LACKS the lifecycle tokens must NOT get CreatedAt/ManualOrder
// stamped on parse. Only genuinely-new tasks (id-less on first save) get the
// minted values.
func TestMinting_NoBackfillForExistingTasks(t *testing.T) {
	// Existing task: has an id, no lifecycle tokens.
	existing := "- [ ] legacy task [due:: 2026-07-01] <!-- id: 77777777-7777-7777-7777-777777777777 -->\n"
	src := "---\nnotebook: \"nb\"\nsection: \"\"\npage: \"pg\"\ndate: \"2026-07-06\"\ntags: []\n---\n" + existing
	blocks, _, _, modified, err := ParseFileContent(src, "nb", "", "pg", "2026-07-06", 4)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if modified {
		t.Errorf("expected no modification — existing task already has an id")
	}
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0].CreatedAt != "" {
		t.Errorf("NO BACKFAIL: existing task got CreatedAt=%q stamped", blocks[0].CreatedAt)
	}
	if blocks[0].ManualOrder != 0 {
		t.Errorf("NO BACKFILL: existing task got ManualOrder=%d stamped", blocks[0].ManualOrder)
	}
}

// TestMinting_NewTaskGetsCreatedAtAndOrder verifies a genuinely-new TASK
// block (no id comment on first parse) gets CreatedAt minted and a ManualOrder
// assigned, plus the id is minted. The minted values then survive a re-parse
// from the rendered output (the token round-trips).
func TestMinting_NewTaskGetsCreatedAtAndOrder(t *testing.T) {
	// New task: NO id comment. The parser must mint id + CreatedAt + ManualOrder.
	src := "---\nnotebook: \"nb\"\nsection: \"\"\npage: \"pg\"\ndate: \"2026-07-06\"\ntags: []\n---\n" +
		"# Header\n" +
		"- [ ] brand new task\n"
	blocks, _, newContent, modified, err := ParseFileContent(src, "nb", "", "pg", "2026-07-06", 4)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !modified {
		t.Errorf("expected modification (id + lifecycle tokens minted)")
	}
	var task *ParsedBlock
	for i := range blocks {
		if blocks[i].Type == BlockTask {
			task = &blocks[i]
		}
	}
	if task == nil {
		t.Fatalf("no TASK block found in %d blocks", len(blocks))
	}
	if task.CreatedAt == "" {
		t.Errorf("expected minted CreatedAt for new task, got empty")
	}
	// This is the first (and only) task in the file → ManualOrder=1.
	if task.ManualOrder != 1 {
		t.Errorf("expected ManualOrder=1 for the sole task, got %d", task.ManualOrder)
	}
	if task.ID == "" {
		t.Errorf("expected a minted id for the new task")
	}

	// The minted tokens must appear in the rewritten content.
	if !strings.Contains(newContent, "[created:: ") {
		t.Errorf("expected [created:: ...] in rewritten content:\n%s", newContent)
	}
	if !strings.Contains(newContent, "[order:: 1]") {
		t.Errorf("expected [order:: 1] in rewritten content:\n%s", newContent)
	}

	// Re-parse the rewritten content: the id is now present, so the parser
	// must NOT re-mint (no double-stamp). The values come from the tokens.
	reblocks, _, _, reModified, err := ParseFileContent(newContent, "nb", "", "pg", "2026-07-06", 4)
	if err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	if reModified {
		t.Errorf("expected no re-modification on second parse (id already present)")
	}
	var retask *ParsedBlock
	for i := range reblocks {
		if reblocks[i].Type == BlockTask {
			retask = &reblocks[i]
		}
	}
	if retask == nil {
		t.Fatalf("no TASK block after re-parse")
	}
	if retask.CreatedAt != task.CreatedAt {
		t.Errorf("CreatedAt did not round-trip through render: %q → %q", task.CreatedAt, retask.CreatedAt)
	}
	if retask.ManualOrder != task.ManualOrder {
		t.Errorf("ManualOrder did not round-trip through render: %d → %d", task.ManualOrder, retask.ManualOrder)
	}
}

// TestMinting_ManualOrderPositionSemantics verifies ManualOrder reflects the
// 1-based position of the new task AMONG ALL TASK BLOCKS in the file — so a
// new task inserted as the 2nd of 3 tasks gets ManualOrder=2, while the
// existing tasks (which already have ids) stay at 0 (no backfill).
func TestMinting_ManualOrderPositionSemantics(t *testing.T) {
	// Three tasks: the 1st and 3rd already have ids; the 2nd is new.
	src := "---\nnotebook: \"nb\"\nsection: \"\"\npage: \"pg\"\ndate: \"2026-07-06\"\ntags: []\n---\n" +
		"- [ ] first (has id) <!-- id: aaaaaaaa-1111-1111-1111-111111111111 -->\n" +
		"- [ ] second (NEW, no id)\n" +
		"- [ ] third (has id) <!-- id: aaaaaaaa-3333-3333-3333-111111111111 -->\n"
	blocks, _, _, _, err := ParseFileContent(src, "nb", "", "pg", "2026-07-06", 4)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(blocks) != 3 {
		t.Fatalf("expected 3 blocks, got %d", len(blocks))
	}
	// 1st task: existing → no backfill.
	if blocks[0].ManualOrder != 0 {
		t.Errorf("task 1 (existing) should NOT be backfilled, got ManualOrder=%d", blocks[0].ManualOrder)
	}
	// 2nd task: new → minted position 2 (it's the 2nd TASK block in the file).
	if blocks[1].ManualOrder != 2 {
		t.Errorf("task 2 (new) expected ManualOrder=2, got %d", blocks[1].ManualOrder)
	}
	if blocks[1].CreatedAt == "" {
		t.Errorf("task 2 (new) expected minted CreatedAt, got empty")
	}
	// 3rd task: existing → no backfill.
	if blocks[2].ManualOrder != 0 {
		t.Errorf("task 3 (existing) should NOT be backfilled, got ManualOrder=%d", blocks[2].ManualOrder)
	}
}
