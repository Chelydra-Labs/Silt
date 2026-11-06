package parser

import (
	"strings"
	"testing"
)

// TestDeriveHierarchy_ConsistentParentEveryType is the regression net for the
// invariant "depth > 0 implies a consistent parent": for every block type, a
// block at depth 1 parents to the preceding depth-0 block and a block at
// depth 2 parents to the preceding depth-1 block, with no violations.
func TestDeriveHierarchy_ConsistentParentEveryType(t *testing.T) {
	types := []BlockType{BlockTask, BlockNote, BlockHeader, BlockCode, BlockTable, BlockDetails, BlockCallout}
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
