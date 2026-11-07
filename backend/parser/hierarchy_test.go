package parser

import (
	"strings"
	"testing"
)

// TestDeriveHierarchy_ConsistentParentEveryType is the regression net for the
// invariant "depth > 0 implies a consistent parent": for every line-managed
// block type, a block at depth 1 parents to the preceding depth-0 block and a
// block at depth 2 parents to the preceding depth-1 block, with no
// violations. Region types (CODE/TABLE/DETAILS/CALLOUT) are excluded — their
// parentage resolution is pinned separately (see
// TestDeriveHierarchy_RegionParentage).
func TestDeriveHierarchy_ConsistentParentEveryType(t *testing.T) {
	types := []BlockType{BlockTask, BlockNote, BlockHeader}
	for _, bt := range types {
		t.Run(string(bt), func(t *testing.T) {
			blocks := []ParsedBlock{
				{ID: "11111111-1111-4111-8111-111111111111", Type: bt, Depth: 0},
				{ID: "22222222-2222-4222-8222-222222222222", Type: bt, Depth: 1},
				{ID: "33333333-3333-4333-8333-333333333333", Type: bt, Depth: 2},
			}
			violations := DeriveHierarchy(blocks)
			if len(violations) != 0 {
				t.Fatalf("DeriveHierarchy reported violations for a well-formed %s chain: %+v", bt, violations)
			}
			if blocks[1].ParentID != blocks[0].ID {
				t.Errorf("depth-1 %s ParentID = %q, want %q", bt, blocks[1].ParentID, blocks[0].ID)
			}
			if blocks[2].ParentID != blocks[1].ID {
				t.Errorf("depth-2 %s ParentID = %q, want %q", bt, blocks[2].ParentID, blocks[1].ID)
			}
		})
	}
}

// TestDeriveHierarchy_RegionParentage pins the region-parentage contract
// (#1060): a region resolves its ParentID to the nearest shallower non-region
// ancestor (same scan as prose blocks), but never enters the ancestry stack —
// nothing parents to a region, and a block following one still resolves to
// its pre-region ancestor. Resolution never reports violations and never
// rewrites depth: a region's Depth is serialization-only, so an indentation
// jump on it is a shape, not corruption.
func TestDeriveHierarchy_RegionParentage(t *testing.T) {
	regionTypes := []BlockType{BlockCode, BlockTable, BlockDetails, BlockCallout}
	const (
		taskID   = "11111111-1111-4111-8111-111111111111"
		regionID = "22222222-2222-4222-8222-222222222222"
		replyID  = "33333333-3333-4333-8333-333333333333"
		deepID   = "44444444-4444-4444-8444-444444444444"
	)
	for _, rt := range regionTypes {
		t.Run(string(rt), func(t *testing.T) {
			blocks := []ParsedBlock{
				{ID: taskID, Type: BlockTask, Depth: 0},
				{ID: regionID, Type: rt, Depth: 1},
				{ID: replyID, Type: BlockNote, Depth: 1},
				{ID: deepID, Type: BlockNote, Depth: 2},
			}
			violations := DeriveHierarchy(blocks)
			if len(violations) != 0 {
				t.Fatalf("%s fixture produced violations: %+v", rt, violations)
			}
			if blocks[1].ParentID != taskID {
				t.Errorf("indented %s ParentID = %q, want the task %q (nearest shallower non-region ancestor)", rt, blocks[1].ParentID, taskID)
			}
			if blocks[2].ParentID != taskID {
				t.Errorf("reply ParentID = %q, want the task %q (region must not steal it)", blocks[2].ParentID, taskID)
			}
			if blocks[3].ParentID != replyID {
				t.Errorf("depth-2 block ParentID = %q, want the reply %q", blocks[3].ParentID, replyID)
			}
		})
	}
	t.Run("region at depth 0 is a root", func(t *testing.T) {
		blocks := []ParsedBlock{
			{ID: taskID, Type: BlockTask, Depth: 0},
			{ID: regionID, Type: BlockCode, Depth: 0},
			{ID: replyID, Type: BlockNote, Depth: 1},
		}
		if violations := DeriveHierarchy(blocks); len(violations) != 0 {
			t.Fatalf("violations = %+v, want none", violations)
		}
		if blocks[1].ParentID != "" {
			t.Errorf("depth-0 region ParentID = %q, want empty (root)", blocks[1].ParentID)
		}
		if blocks[2].ParentID != taskID {
			t.Errorf("reply ParentID = %q, want the task %q", blocks[2].ParentID, taskID)
		}
	})
	t.Run("deeper block after a region parents to the pre-region ancestor", func(t *testing.T) {
		// The stack must never hold a region id: the depth-3 block finds
		// nothing at depth 2 (the region was not pushed) and repairs to the
		// pre-region note — parenting it to the region is exactly the bug
		// the old transparency rule prevented, and the new one keeps.
		blocks := []ParsedBlock{
			{ID: taskID, Type: BlockTask, Depth: 0},
			{ID: replyID, Type: BlockNote, Depth: 1},
			{ID: regionID, Type: BlockCode, Depth: 2},
			{ID: deepID, Type: BlockNote, Depth: 3},
		}
		violations := DeriveHierarchy(blocks)
		if len(violations) != 1 || violations[0].BlockID != deepID {
			t.Fatalf("violations = %+v, want exactly the depth-3 block's jump repair (the region itself never reports)", violations)
		}
		if blocks[2].ParentID != replyID {
			t.Errorf("region ParentID = %q, want the note %q", blocks[2].ParentID, replyID)
		}
		if blocks[3].ParentID != replyID {
			t.Errorf("depth-3 block ParentID = %q, want the pre-region note %q (a region must never be an ancestor)", blocks[3].ParentID, replyID)
		}
	})
	t.Run("region depth jump resolves silently", func(t *testing.T) {
		blocks := []ParsedBlock{
			{ID: taskID, Type: BlockTask, Depth: 0},
			{ID: regionID, Type: BlockTable, Depth: 4},
		}
		if violations := DeriveHierarchy(blocks); len(violations) != 0 {
			t.Fatalf("violations = %+v, want none for a region depth jump (resolution, not repair)", violations)
		}
		if blocks[1].ParentID != taskID {
			t.Errorf("depth-4 region ParentID = %q, want the task %q", blocks[1].ParentID, taskID)
		}
	})
}

// TestParseFileContent_RegionParentage pins the same contract end to end
// through a real parse: an indented fenced code block under a task resolves
// its own parent to the task without re-parenting the following indented
// reply, and the file parses warning-free — a region sitting in a task's
// depth band is the sub-editor's normal shape, not a hierarchy violation.
func TestParseFileContent_RegionParentage(t *testing.T) {
	const (
		taskID  = "5f0c0a11-0000-4000-8000-000000000001"
		codeID  = "5f0c0a11-0000-4000-8000-000000000003"
		replyID = "5f0c0a11-0000-4000-8000-000000000002"
	)
	src := "- [ ] root task <!-- id: 5f0c0a11-0000-4000-8000-000000000001 -->\n" +
		"    ```go\n    fmt.Println(1)\n    ```\n" +
		"    <!-- id: 5f0c0a11-0000-4000-8000-000000000003 -->\n" +
		"    - reply note <!-- id: 5f0c0a11-0000-4000-8000-000000000002 -->\n"

	blocks, meta, _, _, err := ParseFileContent(src, "nb", "sec", "pg", "2026-01-01", 4)
	if err != nil {
		t.Fatalf("ParseFileContent failed: %v", err)
	}
	if len(blocks) != 3 || blocks[1].ID != codeID || blocks[2].ID != replyID {
		t.Fatalf("blocks = %+v, want task, code region, reply", blocks)
	}
	if blocks[1].Type != BlockCode || blocks[1].Depth != 1 || blocks[1].ParentID != taskID {
		t.Errorf("code region (type %s depth %d) ParentID = %q, want the task %q", blocks[1].Type, blocks[1].Depth, blocks[1].ParentID, taskID)
	}
	if blocks[2].ParentID != taskID {
		t.Errorf("reply ParentID = %q, want the task %q (region must stay non-ancestor)", blocks[2].ParentID, taskID)
	}
	if hierarchyWarningPresent(meta.Warnings) {
		t.Errorf("meta.Warnings = %v, want no hierarchy violations for an indented region", meta.Warnings)
	}
}

func TestDeriveHierarchy_RepairsOrphanDepthJump(t *testing.T) {
	const (
		rootID   = "11111111-1111-4111-8111-111111111111"
		childID  = "22222222-2222-4222-8222-222222222222"
		orphanID = "33333333-3333-4333-8333-333333333333"
	)
	t.Run("jump over empty depths attaches to nearest resolvable parent", func(t *testing.T) {
		blocks := []ParsedBlock{
			{ID: rootID, Type: BlockTask, Depth: 0},
			{ID: childID, Type: BlockNote, Depth: 1},
			{ID: orphanID, Type: BlockNote, Depth: 4},
		}
		violations := DeriveHierarchy(blocks)
		if blocks[2].ParentID != childID {
			t.Errorf("orphan ParentID = %q, want nearest resolvable parent %q", blocks[2].ParentID, childID)
		}
		if len(violations) != 1 || violations[0].BlockID != orphanID {
			t.Fatalf("violations = %+v, want exactly one for the depth-4 orphan", violations)
		}
		if violations[0].ParentID != childID || violations[0].ParentDepth != 1 {
			t.Errorf("violation repair = (%q, depth %d), want (%q, depth 1)", violations[0].ParentID, violations[0].ParentDepth, childID)
		}
	})
	t.Run("no ancestor at all leaves the block parentless and reported", func(t *testing.T) {
		blocks := []ParsedBlock{
			{ID: orphanID, Type: BlockNote, Depth: 2},
		}
		violations := DeriveHierarchy(blocks)
		if blocks[0].ParentID != "" {
			t.Errorf("parentless root-orphan ParentID = %q, want empty", blocks[0].ParentID)
		}
		if len(violations) != 1 || violations[0].BlockID != orphanID || violations[0].ParentID != "" {
			t.Fatalf("violations = %+v, want one unresolvable entry for %s", violations, orphanID)
		}
	})
	t.Run("block after a repair still resolves its own parent", func(t *testing.T) {
		blocks := []ParsedBlock{
			{ID: rootID, Type: BlockTask, Depth: 0},
			{ID: orphanID, Type: BlockNote, Depth: 4},
			{ID: "44444444-4444-4444-8444-444444444444", Type: BlockNote, Depth: 5},
		}
		violations := DeriveHierarchy(blocks)
		if blocks[2].ParentID != orphanID {
			t.Errorf("depth-5 block ParentID = %q, want the repaired depth-4 block %q", blocks[2].ParentID, orphanID)
		}
		if len(violations) != 1 {
			t.Fatalf("violations = %+v, want only the depth-4 jump (the depth-5 parent resolves)", violations)
		}
	})
	// The two sequences below pin the stack bookkeeping around repairs: a
	// block at depth d evicts every deeper slot (stack[d+1:] = ""), so a
	// repaired deep block must never survive as a stale ancestor for later
	// jumps. Dropping that eviction would silently re-parent deeper blocks
	// to a block the shallower return already displaced.
	t.Run("shallower block after a repair invalidates deeper slots", func(t *testing.T) {
		const (
			midID  = "44444444-4444-4444-8444-444444444444"
			deepID = "55555555-5555-4555-8555-555555555555"
		)
		blocks := []ParsedBlock{
			{ID: rootID, Type: BlockTask, Depth: 0},
			{ID: orphanID, Type: BlockNote, Depth: 4}, // repaired → rootID
			{ID: midID, Type: BlockNote, Depth: 1},    // must evict stack[2..4]
			{ID: deepID, Type: BlockNote, Depth: 5},
		}
		violations := DeriveHierarchy(blocks)
		if blocks[3].ParentID != midID {
			t.Errorf("depth-5 block ParentID = %q, want the depth-1 block %q (the repaired depth-4 entry was not evicted)", blocks[3].ParentID, midID)
		}
		if len(violations) != 2 || violations[0].BlockID != orphanID || violations[1].BlockID != deepID {
			t.Fatalf("violations = %+v, want repairs for the depth-4 and depth-5 jumps only", violations)
		}
	})
	t.Run("repair of a shallower block evicts stale deeper ancestors", func(t *testing.T) {
		const (
			midID  = "44444444-4444-4444-8444-444444444444"
			deepID = "55555555-5555-4555-8555-555555555555"
		)
		blocks := []ParsedBlock{
			{ID: rootID, Type: BlockTask, Depth: 0},
			{ID: orphanID, Type: BlockNote, Depth: 4}, // repaired → rootID, sits at stack[4]
			{ID: midID, Type: BlockNote, Depth: 2},    // repaired → rootID, must evict stack[4]
			{ID: deepID, Type: BlockNote, Depth: 5},
		}
		violations := DeriveHierarchy(blocks)
		if blocks[3].ParentID != midID {
			t.Errorf("depth-5 block ParentID = %q, want the depth-2 block %q (the depth-4 entry survived the shallower repair)", blocks[3].ParentID, midID)
		}
		if len(violations) != 3 || violations[0].BlockID != orphanID || violations[1].BlockID != midID || violations[2].BlockID != deepID {
			t.Fatalf("violations = %+v, want repairs for the depth-4, depth-2, and depth-5 jumps", violations)
		}
	})
}

// TestDeriveHierarchy_HeaderLevelJumpsAreNotViolations pins the header
// exemption: a HEADER's Depth is its heading level, so a file-opening H1
// (depth 1, nothing above it) and a level jump (`#` straight to `###`) are
// idiomatic markdown. Parentage is still assigned where resolvable; no
// violation is reported.
func TestDeriveHierarchy_HeaderLevelJumpsAreNotViolations(t *testing.T) {
	blocks := []ParsedBlock{
		{ID: "11111111-1111-4111-8111-111111111111", Type: BlockHeader, Depth: 1},
		{ID: "22222222-2222-4222-8222-222222222222", Type: BlockHeader, Depth: 3},
	}
	violations := DeriveHierarchy(blocks)
	if len(violations) != 0 {
		t.Fatalf("header level jumps reported as violations: %+v", violations)
	}
	if blocks[1].ParentID != blocks[0].ID {
		t.Errorf("H3 ParentID = %q, want the preceding H1 %q (nearest resolvable)", blocks[1].ParentID, blocks[0].ID)
	}
}

// TestParseFileContent_HierarchyRepairReportedAndStable pins the load/save
// contract end to end on the depth-jump fixture: the parse repairs the orphan
// under the nearest resolvable parent, reports a hierarchy warning, and the
// repair is stable — rendering the repaired blocks and re-parsing yields the
// same parentage, the same warning, and byte-identical content (the repair
// never rewrites the file).
func TestParseFileContent_HierarchyRepairReportedAndStable(t *testing.T) {
	const (
		taskID  = "5f0c0a11-0000-4000-8000-000000000001"
		replyID = "5f0c0a11-0000-4000-8000-000000000002"
	)
	src := "- [ ] root task <!-- id: 5f0c0a11-0000-4000-8000-000000000001 -->\n" +
		"                - orphan reply <!-- id: 5f0c0a11-0000-4000-8000-000000000002 -->\n"

	blocks, meta, _, _, err := ParseFileContent(src, "nb", "sec", "pg", "2026-01-01", 4)
	if err != nil {
		t.Fatalf("ParseFileContent failed: %v", err)
	}
	if len(blocks) != 2 || blocks[1].ID != replyID {
		t.Fatalf("blocks = %+v, want the task and the orphan reply", blocks)
	}
	if blocks[1].ParentID != taskID {
		t.Fatalf("orphan reply ParentID = %q, want repair to the task %q", blocks[1].ParentID, taskID)
	}
	if !hierarchyWarningPresent(meta.Warnings) {
		t.Fatalf("meta.Warnings = %v, want a hierarchy violation entry", meta.Warnings)
	}

	// Save side: the same parse runs on rendered content, so the warning is
	// reported again and the repair must be idempotent.
	rendered := RenderFileContent(blocks, "", "", 4)
	blocks2, meta2, _, _, err := ParseFileContent(rendered, "nb", "sec", "pg", "2026-01-01", 4)
	if err != nil {
		t.Fatalf("re-parse failed: %v", err)
	}
	if len(blocks2) != 2 || blocks2[1].ParentID != taskID {
		t.Fatalf("re-parse blocks = %+v, want the same repaired parentage", blocks2)
	}
	if !hierarchyWarningPresent(meta2.Warnings) {
		t.Fatalf("re-parse meta.Warnings = %v, want the hierarchy violation reported on save too", meta2.Warnings)
	}
	if rendered2 := RenderFileContent(blocks2, "", "", 4); rendered2 != rendered {
		t.Errorf("repair is not stable across render→parse:\nfirst:  %q\nsecond: %q", rendered, rendered2)
	}
}

func hierarchyWarningPresent(warnings []string) bool {
	for _, w := range warnings {
		if strings.Contains(w, "hierarchy violation") {
			return true
		}
	}
	return false
}
