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
