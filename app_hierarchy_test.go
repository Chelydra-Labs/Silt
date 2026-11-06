package main

import (
	"strings"
	"testing"

	"silt/backend/parser"
)

// Hierarchy-contract fixtures shared with backend/db's
// indexing_hierarchy_test.go (identical bytes, duplicated per package so each
// half of the contract is pinned in the package that owns the reader).
const (
	hierarchyTaskID  = "5f0c0a11-0000-4000-8000-000000000001"
	hierarchyReplyID = "5f0c0a11-0000-4000-8000-000000000002"

	// indentedReplyDoc: a TASK with a well-formed indented NOTE reply at
	// depth 1 — the acceptance fixture for "subtree reader and task counts
	// agree".
	indentedReplyDoc = "- [ ] root task <!-- id: 5f0c0a11-0000-4000-8000-000000000001 -->\n" +
		"    - reply note <!-- id: 5f0c0a11-0000-4000-8000-000000000002 -->\n"

	// depthJumpDoc: the same reply indented straight to depth 4 with no
	// intermediate block — a depth > 0 block whose implied parent cannot be
	// resolved from the file.
	depthJumpDoc = "- [ ] root task <!-- id: 5f0c0a11-0000-4000-8000-000000000001 -->\n" +
		"                - orphan reply <!-- id: 5f0c0a11-0000-4000-8000-000000000002 -->\n"
)

// TestParseFileContent_ReportsDepthJumpOrphan pins the unenforced invariant:
// a depth > 0 block whose parent cannot be resolved must not be silently
// accepted. It must be repaired to the nearest resolvable parent (so subtree
// and counts readers see one consistent tree) and a warning must be reported
// on the per-file warnings channel.
func TestParseFileContent_ReportsDepthJumpOrphan(t *testing.T) {
	blocks, meta, _, _, err := parser.ParseFileContent(depthJumpDoc, "nb", "sec", "pg", "2026-01-01", 4)
	if err != nil {
		t.Fatalf("ParseFileContent failed: %v", err)
	}
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(blocks))
	}
	if blocks[1].ID != hierarchyReplyID {
		t.Fatalf("block 1 id = %s, want the orphan reply", blocks[1].ID)
	}
	if blocks[1].ParentID != hierarchyTaskID {
		t.Errorf("orphan reply ParentID = %q, want repair to the nearest resolvable parent %s", blocks[1].ParentID, hierarchyTaskID)
	}
	if len(meta.Warnings) == 0 {
		t.Errorf("depth jump with no resolvable parent was accepted silently: meta.Warnings is empty")
	} else {
		found := false
		for _, w := range meta.Warnings {
			if strings.Contains(w, "hierarchy") {
				found = true
			}
		}
		if !found {
			t.Errorf("no hierarchy warning in meta.Warnings: %v", meta.Warnings)
		}
	}
}

// TestSubtreeReader_IndentedNoteReply is the subtree half of the reader-agreement
// contract: for a TASK with an indented NOTE reply, extractSubtree must return
// the reply — both for the well-formed fixture and for the depth-jump fixture
// (where the reply is only in the subtree because the parse repaired it under
// the task). backend/db's TestTaskDerivedCounts_IndentedNoteReply pins the
// counts half on identical fixture bytes; the two tests must hold together.
func TestSubtreeReader_IndentedNoteReply(t *testing.T) {
	t.Run("well-formed reply", func(t *testing.T) {
		blocks, _, _, _, err := parser.ParseFileContent(indentedReplyDoc, "nb", "sec", "pg", "2026-01-01", 4)
		if err != nil {
			t.Fatalf("ParseFileContent failed: %v", err)
		}
		sub := extractSubtree(blocks, hierarchyTaskID)
		if len(sub) != 1 || sub[0].ID != hierarchyReplyID || sub[0].Type != parser.BlockNote {
			t.Fatalf("extractSubtree = %+v, want exactly the NOTE reply %s", sub, hierarchyReplyID)
		}
	})
	t.Run("depth-jump reply", func(t *testing.T) {
		blocks, _, _, _, err := parser.ParseFileContent(depthJumpDoc, "nb", "sec", "pg", "2026-01-01", 4)
		if err != nil {
			t.Fatalf("ParseFileContent failed: %v", err)
		}
		sub := extractSubtree(blocks, hierarchyTaskID)
		if len(sub) != 1 || sub[0].ID != hierarchyReplyID {
			t.Fatalf("extractSubtree = %+v, want the repaired orphan reply %s", sub, hierarchyReplyID)
		}
	})
}
