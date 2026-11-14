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

// TestTaskDerivedCounts_SubtaskOwnsItsComments pins the ownership rule the
// comment thread now mirrors (D3 of the #1163 series): each NOTE counts toward
// its nearest TASK ancestor, so a commented sub-task never inflates the
// parent's count and the thread cannot show more comments than the badge.
func TestTaskDerivedCounts_SubtaskOwnsItsComments(t *testing.T) {
	const (
		parentTaskID  = "5f0c0a11-0000-4000-8000-000000000010"
		subTaskID     = "5f0c0a11-0000-4000-8000-000000000011"
		parentNoteID  = "5f0c0a11-0000-4000-8000-000000000012"
		parentReplyID = "5f0c0a11-0000-4000-8000-000000000013"
		subNoteID     = "5f0c0a11-0000-4000-8000-000000000014"
	)
	doc := "- [ ] parent task <!-- id: " + parentTaskID + " -->\n" +
		"    - parent comment <!-- id: " + parentNoteID + " -->\n" +
		"        - parent reply <!-- id: " + parentReplyID + " -->\n" +
		"    - [ ] sub task <!-- id: " + subTaskID + " -->\n" +
		"        - sub comment <!-- id: " + subNoteID + " -->\n"
	blocks, _, _, _, err := parser.ParseFileContent(doc, "nb", "sec", "pg", "2026-01-01", 4)
	if err != nil {
		t.Fatalf("ParseFileContent failed: %v", err)
	}
	comments, _, _ := taskDerivedCounts(blocks)
	if got := comments[parentTaskID]; got != 2 {
		t.Errorf("comments[%s] = %d, want 2 (its own comment + reply, not the sub-task's)", parentTaskID, got)
	}
	if got := comments[subTaskID]; got != 1 {
		t.Errorf("comments[%s] = %d, want 1 (the sub-task owns its own comment)", subTaskID, got)
	}
	// Every NOTE has exactly one owner: no double count, none dropped.
	total := 0
	for _, n := range comments {
		total += n
	}
	if total != 3 {
		t.Errorf("total comments across tasks = %d, want 3", total)
	}
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
