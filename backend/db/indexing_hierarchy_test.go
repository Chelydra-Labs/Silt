package db

import (
	"testing"

	"silt/backend/parser"
)

// The counts half of the hierarchy reader-agreement contract. The root
// package's TestSubtreeReader_IndentedNoteReply pins extractSubtree on the
// same fixture bytes; both tests must hold together so the sub-editor and the
// comment counts can never disagree about a task reply again.
const (
	hierarchyTaskID  = "5f0c0a11-0000-4000-8000-000000000001"
	hierarchyReplyID = "5f0c0a11-0000-4000-8000-000000000002"

	indentedReplyDoc = "- [ ] root task <!-- id: 5f0c0a11-0000-4000-8000-000000000001 -->\n" +
		"    - reply note <!-- id: 5f0c0a11-0000-4000-8000-000000000002 -->\n"

	depthJumpDoc = "- [ ] root task <!-- id: 5f0c0a11-0000-4000-8000-000000000001 -->\n" +
		"                - orphan reply <!-- id: 5f0c0a11-0000-4000-8000-000000000002 -->\n"
)

// TestTaskDerivedCounts_IndentedNoteReply asserts the derived comment count
// for a TASK with an indented NOTE reply is exactly one. The depth-jump
// variant only holds because the parser's single hierarchy derivation repairs
// the orphan under the task — an unrepaired (empty) ParentID would count zero
// comments while the subtree reader still shows the reply.
func TestTaskDerivedCounts_IndentedNoteReply(t *testing.T) {
	t.Run("well-formed reply", func(t *testing.T) {
		blocks, _, _, _, err := parser.ParseFileContent(indentedReplyDoc, "nb", "sec", "pg", "2026-01-01", 4)
		if err != nil {
			t.Fatalf("ParseFileContent failed: %v", err)
		}
		comments, _, _ := taskDerivedCounts(blocks)
		if got := comments[hierarchyTaskID]; got != 1 {
			t.Errorf("comments[%s] = %d, want 1 (the indented NOTE reply)", hierarchyTaskID, got)
		}
	})
	t.Run("depth-jump reply repaired under the task", func(t *testing.T) {
		blocks, _, _, _, err := parser.ParseFileContent(depthJumpDoc, "nb", "sec", "pg", "2026-01-01", 4)
		if err != nil {
			t.Fatalf("ParseFileContent failed: %v", err)
		}
		comments, _, _ := taskDerivedCounts(blocks)
		if got := comments[hierarchyTaskID]; got != 1 {
			t.Errorf("comments[%s] = %d, want 1 (orphan reply must resolve to the task), reply ParentID = %q",
				hierarchyTaskID, got, blockParentID(blocks, hierarchyReplyID))
		}
	})
}

func blockParentID(blocks []parser.ParsedBlock, id string) string {
	for _, b := range blocks {
		if b.ID == id {
			return b.ParentID
		}
	}
	return ""
}

// TestTaskDerivedCounts_RegionUnderTaskUnaffected pins the counts half of the
// #1060 contract: an indented region under a task now resolves its ParentID
// to the task (subtree membership), but taskDerivedCounts counts NOTE and
// TASK blocks only — a CODE region is neither, so comment and subtask counts
// stay exactly what they were when regions were parentless.
func TestTaskDerivedCounts_RegionUnderTaskUnaffected(t *testing.T) {
	const regionID = "5f0c0a11-0000-4000-8000-000000000003"
	doc := "- [ ] root task <!-- id: " + hierarchyTaskID + " -->\n" +
		"    ```go\n    fmt.Println(1)\n    ```\n" +
		"    <!-- id: " + regionID + " -->\n" +
		"    - reply note <!-- id: " + hierarchyReplyID + " -->\n"
	blocks, _, _, _, err := parser.ParseFileContent(doc, "nb", "sec", "pg", "2026-01-01", 4)
	if err != nil {
		t.Fatalf("ParseFileContent failed: %v", err)
	}
	if got := blockParentID(blocks, regionID); got != hierarchyTaskID {
		t.Fatalf("region ParentID = %q, want the task %q (the region is in the task's sub-tree)", got, hierarchyTaskID)
	}
	comments, subtaskTotal, _ := taskDerivedCounts(blocks)
	if got := comments[hierarchyTaskID]; got != 1 {
		t.Errorf("comments[%s] = %d, want 1 (the NOTE reply only — the CODE region is not a comment)", hierarchyTaskID, got)
	}
	if got := subtaskTotal[hierarchyTaskID]; got != 0 {
		t.Errorf("subtaskTotal[%s] = %d, want 0 (the region is not a subtask)", hierarchyTaskID, got)
	}
}
