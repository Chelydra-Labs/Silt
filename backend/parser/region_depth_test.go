package parser

import (
	"strings"
	"testing"
)

// Region-block depth round-trips: a region's Depth (the opener's leading
// indent) must survive parse → render so an indented code fence or callout
// under a task stays inside the task's depth band on disk. Regions remain
// parentless (DeriveHierarchy transparency) — the depth is
// serialization-only.

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
	for _, b := range blocks {
		if b.ParentID != "" && b.ID != regionDepthReplyID {
			t.Errorf("block %v unexpectedly parented to %s", b.Type, b.ParentID)
		}
	}
	// The reply after the regions still belongs to the task.
	if blocks[5].ParentID != regionDepthTaskID {
		t.Errorf("reply parent = %s, want the task", blocks[5].ParentID)
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
