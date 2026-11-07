package parser

import (
	"strings"
	"testing"
)

// Region-block depth round-trips: a region's Depth (the opener's leading
// indent) must survive parse → render so an indented code fence or callout
// under a task stays inside the task's depth band on disk. DeriveHierarchy
// resolves a region's parent from that depth (nearest shallower non-region
// ancestor) but never lets a region act as an ancestor — see
// hierarchy_test.go's TestDeriveHierarchy_RegionParentage.

const (
	regionDepthTaskID   = "11111111-1111-1111-1111-111111111111"
	regionDepthCodeID   = "22222222-2222-2222-2222-222222222222"
	regionDepthCallID   = "33333333-3333-3333-3333-333333333333"
	regionDepthReplyID  = "44444444-4444-4444-4444-444444444444"
	regionDepthTableID  = "55555555-5555-5555-5555-555555555555"
	regionDepthDetailID = "66666666-6666-6666-6666-666666666666"
)

// All four region types at column 0 — the historical default. Rendered bytes
// must not change: the region indent is empty, so every branch is a no-op.
func TestRenderFileContent_RegionDepthZeroByteIdentical(t *testing.T) {
	src := "- [ ] task <!-- id: " + regionDepthTaskID + " @ 2026-01-01 -->\n" +
		"```js\n    kept verbatim\n```\n" +
		"<!-- id: " + regionDepthCodeID + " @ 2026-01-01 -->\n" +
		"> [!note] T\n> body\n" +
		"<!-- id: " + regionDepthCallID + " @ 2026-01-01 -->\n" +
		"| a | b |\n| --- | --- |\n| 1 | 2 |\n" +
		"<!-- id: " + regionDepthTableID + " @ 2026-01-01 -->\n" +
		"<details>\n<summary>S</summary>\nline\n</details>\n" +
		"<!-- id: " + regionDepthDetailID + " @ 2026-01-01 -->\n"

	blocks, _, _, _, err := ParseFileContent(src, "", "", "", "2026-01-01", 4)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(blocks) != 5 {
		t.Fatalf("got %d blocks, want 5 (task + 4 regions)", len(blocks))
	}
	for _, b := range blocks[1:] {
		if b.Depth != 0 {
			t.Errorf("%v region parsed at depth %d, want 0", b.Type, b.Depth)
		}
	}
	rendered := RenderFileContent(blocks, src, "", 4)
	if rendered != src {
		t.Fatalf("depth-0 regions not byte-identical:\n%q\n%q", src, rendered)
	}
	// And stable across a second cycle.
	blocks2, _, _, _, err := ParseFileContent(rendered, "", "", "", "2026-01-01", 4)
	if err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	if got := RenderFileContent(blocks2, rendered, "", 4); got != rendered {
		t.Fatalf("depth-0 second render drifted:\n%q\n%q", rendered, got)
	}
}

// All four region types indented under a task, in canonical form. Parse must
// record depth 1; render must re-emit every region indented, byte-identical;
// the reply after the task keeps its parent (ownership does not shift).
func TestRenderFileContent_RegionDepthUnderTaskPreserved(t *testing.T) {
	src := "- [ ] task <!-- id: " + regionDepthTaskID + " @ 2026-01-01 -->\n" +
		"    ```js\n    kept verbatim\n    ```\n" +
		"    <!-- id: " + regionDepthCodeID + " @ 2026-01-01 -->\n" +
		"    > [!note] T\n    > body\n" +
		"    <!-- id: " + regionDepthCallID + " @ 2026-01-01 -->\n" +
		"    | a | b |\n    | --- | --- |\n    | 1 | 2 |\n" +
		"    <!-- id: " + regionDepthTableID + " @ 2026-01-01 -->\n" +
		"    <details>\n    <summary>S</summary>\n    line\n    </details>\n" +
		"    <!-- id: " + regionDepthDetailID + " @ 2026-01-01 -->\n" +
		"    - reply under task <!-- id: " + regionDepthReplyID + " @ 2026-01-01 -->\n"

	blocks, _, _, _, err := ParseFileContent(src, "", "", "", "2026-01-01", 4)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	wantDepths := []int{0, 1, 1, 1, 1, 1}
	for i, want := range wantDepths {
		if blocks[i].Depth != want {
			t.Errorf("block %d (%v) depth = %d, want %d", i, blocks[i].Type, blocks[i].Depth, want)
		}
	}
	// Every region and the reply resolve to the task — regions parent to
	// their nearest shallower non-region ancestor, the reply's parent is
	// unchanged by the regions between them.
	for _, b := range blocks[1:] {
		if b.ParentID != regionDepthTaskID {
			t.Errorf("block %v ParentID = %s, want the task", b.Type, b.ParentID)
		}
	}

	rendered := RenderFileContent(blocks, src, "", 4)
	if rendered != src {
		t.Fatalf("indented regions not byte-identical in canonical form:\n%q\n%q", src, rendered)
	}
}

// A hand-authored (Obsidian-style) indented region whose id line sits at
// column 0 and whose code content is less indented than its fences: the first
// render normalizes the id line into the region indent, and from then on the
// output is byte-stable with the same depths and parentage.
func TestRenderFileContent_RegionDepthStableFromNonCanonical(t *testing.T) {
	src := "- [ ] task <!-- id: " + regionDepthTaskID + " @ 2026-01-01 -->\n" +
		"    ```py\nprint(\"hi\")\n    ```\n" +
		"<!-- id: " + regionDepthCodeID + " @ 2026-01-01 -->\n" +
		"    - reply under task <!-- id: " + regionDepthReplyID + " @ 2026-01-01 -->\n"

	blocks, _, _, _, err := ParseFileContent(src, "", "", "", "2026-01-01", 4)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if blocks[1].Type != BlockCode || blocks[1].Depth != 1 {
		t.Fatalf("code region depth = %d, want 1", blocks[1].Depth)
	}
	if blocks[2].ParentID != regionDepthTaskID {
		t.Fatalf("reply parent = %s, want the task", blocks[2].ParentID)
	}

	rendered := RenderFileContent(blocks, src, "", 4)
	if !strings.HasPrefix(rendered, src[:strings.Index(src, "```")]) {
		t.Fatalf("task line mangled: %q", rendered)
	}
	// The id line normalized INTO the indent (this byte change is the fix);
	// everything else verbatim.
	want := "- [ ] task <!-- id: " + regionDepthTaskID + " @ 2026-01-01 -->\n" +
		"    ```py\nprint(\"hi\")\n    ```\n" +
		"    <!-- id: " + regionDepthCodeID + " @ 2026-01-01 -->\n" +
		"    - reply under task <!-- id: " + regionDepthReplyID + " @ 2026-01-01 -->\n"
	if rendered != want {
		t.Fatalf("first render not canonical:\n%q\n%q", want, rendered)
	}

	// Second cycle: identical bytes, depths, parentage.
	blocks2, _, _, _, err := ParseFileContent(rendered, "", "", "", "2026-01-01", 4)
	if err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	if blocks2[1].Depth != 1 || blocks2[1].CleanText != blocks[1].CleanText {
		t.Fatalf("re-parse drifted: depth=%d clean=%q", blocks2[1].Depth, blocks2[1].CleanText)
	}
	if blocks2[2].ParentID != regionDepthTaskID {
		t.Fatalf("reply parent drifted to %s", blocks2[2].ParentID)
	}
	if got := RenderFileContent(blocks2, rendered, "", 4); got != rendered {
		t.Fatalf("second render not byte-stable:\n%q\n%q", rendered, got)
	}
}

// Externally-authored files (no Silt id comments — the Obsidian/vault import
// case) trigger the watcher's normalization write: ids are minted and the
// scanner writes newContent back to disk. The normalization emit must carry
// the region indent exactly like renderBlock does, or the mint-write dedents
// indented regions on first ingest — destroying the depth before any editor
// save ever runs.
func TestParseFileContent_NormalizationEmitKeepsRegionDepth(t *testing.T) {
	src := "- [ ] task\n" +
		"    > [!note] T\n    > body\n" +
		"    | a | b |\n    | --- | --- |\n    | 1 | 2 |\n" +
		"    <details>\n    <summary>S</summary>\n    line\n    </details>\n" +
		"    ```js\n    kept verbatim\n    ```\n"

	blocks, _, newContent, modified, err := ParseFileContent(src, "", "", "", "2026-01-01", 4)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !modified {
		t.Fatalf("minted ids must mark the file modified for the watcher to write it")
	}
	if len(blocks) != 5 {
		t.Fatalf("got %d blocks, want 5 (task + 4 regions)", len(blocks))
	}
	for _, b := range blocks[1:] {
		if b.Depth != 1 {
			t.Errorf("%v region depth = %d, want 1", b.Type, b.Depth)
		}
	}
	// Every region byte in the normalized content keeps the 4-space band —
	// content lines AND the minted id lines. Line 0 is the task (depth 0;
	// its minted lifecycle tokens are time-dependent, so only the region
	// lines are asserted here).
	for i, ln := range strings.Split(newContent, "\n") {
		if i == 0 || ln == "" {
			continue
		}
		if !strings.HasPrefix(ln, "    ") {
			t.Errorf("normalization line %d lost the region indent: %q", i+1, ln)
		}
	}
	// Second pass: ids exist now, nothing re-mints — the normalized bytes
	// are stable, so the watcher rewrites exactly once.
	_, _, newContent2, modified2, err := ParseFileContent(newContent, "", "", "", "2026-01-01", 4)
	if err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	if modified2 {
		t.Fatalf("second pass still modified — normalization not idempotent")
	}
	if newContent2 != newContent {
		t.Fatalf("normalized bytes drifted on second parse:\n%q\n%q", newContent, newContent2)
	}
}

// A depth>0 code region whose body contains an INDENTED ``` line: the fence
// growth check must count indented fences (isClosingFence TrimSpaces, so an
// indented 3-fence closes a 3-backtick opener on re-parse). A too-short
// outer fence silently splits the region mid-body across saves.
func TestRenderFileContent_IndentedInnerFenceRoundTrip(t *testing.T) {
	src := "- [ ] task <!-- id: " + regionDepthTaskID + " @ 2026-01-01 -->\n" +
		"    ````markdown\n" +
		"    ```\n" +
		"    ```js\n" +
		"    x\n" +
		"    ```\n" +
		"    ````\n" +
		"    <!-- id: " + regionDepthCodeID + " @ 2026-01-01 -->\n" +
		"    - reply <!-- id: " + regionDepthReplyID + " @ 2026-01-01 -->\n"

	blocks, _, _, _, err := ParseFileContent(src, "", "", "", "2026-01-01", 4)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(blocks) != 3 {
		t.Fatalf("got %d blocks, want 3 (task + code region + reply)", len(blocks))
	}
	if blocks[1].Type != BlockCode || blocks[1].Depth != 1 {
		t.Fatalf("code region = %v depth %d, want CODE depth 1", blocks[1].Type, blocks[1].Depth)
	}
	rendered := RenderFileContent(blocks, src, "", 4)
	if rendered != src {
		t.Fatalf("indented inner fence not byte-identical:\n%q\n%q", src, rendered)
	}
	blocks2, _, _, _, err := ParseFileContent(rendered, "", "", "", "2026-01-01", 4)
	if err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	if len(blocks2) != 3 {
		t.Fatalf("re-parse scrambled the region: got %d blocks, want 3", len(blocks2))
	}
	if got := RenderFileContent(blocks2, rendered, "", 4); got != rendered {
		t.Fatalf("second render drifted:\n%q\n%q", rendered, got)
	}
}

// Blank interior lines in a depth>0 region body stay bare on render (no
// trailing whitespace) — details bodies absorb blank lines between the open
// and close tags, and callouts use bare `>` lines as paragraph breaks. The
// depth-1 table pins the same indent loop for pipe rows (parse cannot put a
// blank line inside a table — every row needs its leading `|`).
func TestRenderFileContent_BlankInteriorLinesStayBareAtDepth(t *testing.T) {
	src := "- [ ] task <!-- id: " + regionDepthTaskID + " @ 2026-01-01 -->\n" +
		"    <details>\n    <summary>S</summary>\n\n    line\n    </details>\n" +
		"    <!-- id: " + regionDepthDetailID + " @ 2026-01-01 -->\n" +
		"    > [!note] T\n    >\n    > after break\n" +
		"    <!-- id: " + regionDepthCallID + " @ 2026-01-01 -->\n" +
		"    | a | b |\n    | --- | --- |\n    | 1 | 2 |\n" +
		"    <!-- id: " + regionDepthTableID + " @ 2026-01-01 -->\n"

	blocks, _, _, _, err := ParseFileContent(src, "", "", "", "2026-01-01", 4)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(blocks) != 4 {
		t.Fatalf("got %d blocks, want 4 (task + details + callout + table)", len(blocks))
	}
	for _, b := range blocks[1:] {
		if b.Depth != 1 {
			t.Errorf("%v region depth = %d, want 1", b.Type, b.Depth)
		}
	}
	rendered := RenderFileContent(blocks, src, "", 4)
	if rendered != src {
		t.Fatalf("blank interior lines not byte-identical:\n%q\n%q", src, rendered)
	}
	if strings.Contains(rendered, " \n") {
		t.Fatalf("trailing whitespace leaked into a rendered line: %q", rendered)
	}
}

// renderBlock is also fed caller-constructed blocks straight from save (IPC,
// plugin SDK), where a TABLE or CALLOUT clean_text can carry a genuinely
// blank interior line — parse can never produce one (a blank line ends the
// region). The bare-blank branch must keep those lines empty instead of
// emitting indent-only trailing whitespace (#1060 AC).
func TestRenderBlock_BlankInteriorLineStaysBareAtDepth(t *testing.T) {
	cases := []struct {
		kind BlockType
		body string
	}{
		{BlockTable, "| a | b |\n| --- | --- |\n\n| 1 | 2 |"},
		{BlockDetails, "<details>\n<summary>S</summary>\n\n</details>"},
		{BlockCallout, "> [!note] T\n\n> after the break"},
	}
	for _, tc := range cases {
		out := renderBlock(ParsedBlock{
			ID:        "77777777-7777-4777-8777-777777777777",
			Type:      tc.kind,
			Depth:     1,
			CleanText: tc.body,
		}, 4)
		blankSeen := false
		for i, ln := range strings.Split(out, "\n") {
			if ln == "" {
				blankSeen = true
				continue
			}
			if !strings.HasPrefix(ln, "    ") {
				t.Errorf("%s render line %d = %q, want the 4-space region indent", tc.kind, i, ln)
			}
		}
		if !blankSeen {
			t.Errorf("%s render lost its bare blank interior line (indent-only line or collapsed): %q", tc.kind, out)
		}
	}
}

// renderBlock is fed caller-constructed blocks straight from save (IPC,
// plugin SDK), not only parse output — a negative depth must floor to 0
// instead of panicking strings.Repeat. Mirrors DeriveHierarchy's floor.
func TestRenderBlock_NegativeDepthFloorsToZero(t *testing.T) {
	out := renderBlock(ParsedBlock{
		ID:        "11111111-1111-1111-8111-111111111111",
		Type:      BlockNote,
		Depth:     -1,
		CleanText: "poisoned",
	}, 4)
	if strings.HasPrefix(out, " ") {
		t.Fatalf("floored block still indented: %q", out)
	}
	if !strings.Contains(out, "poisoned") {
		t.Fatalf("floored block lost its text: %q", out)
	}
}
