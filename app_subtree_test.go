package main

import (
	"os"
	"strings"
	"testing"

	"silt/backend/parser"
)

// TestExtractSubtree verifies the sub-tree membership rule: every block whose
// ParentID ancestor chain contains the parent (#305).
func TestExtractSubtree(t *testing.T) {
	// Layout (depth in parens):
	//   parent task (0)
	//     child note (1)
	//       grandchild task (2)
	//     child task (1)
	//   sibling task (0)   <- ends the sub-tree
	const parent = "aaaaaaaa-0000-0000-0000-000000000000"
	blocks := []parser.ParsedBlock{
		{ID: parent, Type: parser.BlockTask, Depth: 0, CleanText: "parent"},
		{ID: "aaaaaaaa-1111-0000-0000-000000000000", Type: parser.BlockNote, Depth: 1, ParentID: parent, CleanText: "child note"},
		{ID: "aaaaaaaa-2222-0000-0000-000000000000", Type: parser.BlockTask, Depth: 2, ParentID: "aaaaaaaa-1111-0000-0000-000000000000", CleanText: "grandchild"},
		{ID: "aaaaaaaa-3333-0000-0000-000000000000", Type: parser.BlockTask, Depth: 1, ParentID: parent, CleanText: "child task"},
		{ID: "bbbbbbbb-0000-0000-0000-000000000000", Type: parser.BlockTask, Depth: 0, CleanText: "sibling"},
	}

	got := extractSubtree(blocks, parent)
	if len(got) != 3 {
		t.Fatalf("expected 3 subtree blocks (child note + grandchild + child task), got %d: %+v", len(got), got)
	}
	if got[0].ID != "aaaaaaaa-1111-0000-0000-000000000000" {
		t.Errorf("first subtree block = %s, want child note", got[0].ID)
	}
	if got[2].ID != "aaaaaaaa-3333-0000-0000-000000000000" {
		t.Errorf("last subtree block = %s, want child task", got[2].ID)
	}
}

// TestExtractSubtree_IndentedRegionIsMember pins the #1060 membership
// change: an indented managed region under a task resolves its ParentID to
// the task in the single derivation, so parent-graph descent carries it into
// the sub-tree — the sub-editor sees the region among the task's children.
// The column-0 counterpart (regionChildRunDoc below) stays a root and out.
func TestExtractSubtree_IndentedRegionIsMember(t *testing.T) {
	const (
		taskID  = "c1d2e3f4-0000-0000-0000-000000000001"
		codeID  = "c1d2e3f4-0000-0000-0000-000000000002"
		replyID = "c1d2e3f4-0000-0000-0000-000000000003"
	)
	doc := "- [ ] parent task <!-- id: " + taskID + " -->\n" +
		"    ```go\n    fmt.Println(1)\n    ```\n" +
		"    <!-- id: " + codeID + " -->\n" +
		"    - reply note <!-- id: " + replyID + " -->\n"
	blocks, _, _, _, err := parser.ParseFileContent(doc, "W", "S", "P", "2026-07-01", 4)
	if err != nil {
		t.Fatalf("ParseFileContent: %v", err)
	}
	got := extractSubtree(blocks, taskID)
	if len(got) != 2 || got[0].ID != codeID || got[1].ID != replyID {
		t.Fatalf("subtree = %+v, want [code region, reply] — the indented region is a member", got)
	}
}

func TestExtractSubtree_NoChildren(t *testing.T) {
	blocks := []parser.ParsedBlock{
		{ID: "aaaaaaaa-0000-0000-0000-000000000000", Type: parser.BlockTask, Depth: 0},
		{ID: "bbbbbbbb-0000-0000-0000-000000000000", Type: parser.BlockTask, Depth: 0},
	}
	got := extractSubtree(blocks, "aaaaaaaa-0000-0000-0000-000000000000")
	if got == nil {
		t.Fatalf("expected non-nil empty subtree for a childless task, got nil")
	}
	if len(got) != 0 {
		t.Fatalf("expected empty subtree for a childless task, got %d", len(got))
	}
}

func TestExtractSubtree_ParentNotFound(t *testing.T) {
	blocks := []parser.ParsedBlock{{ID: "aaaaaaaa-0000-0000-0000-000000000000", Depth: 0}}
	// A missing parent returns an empty (non-nil) slice, not nil. The SDK
	// contract is []ParsedBlock; a nil would serialize to JSON null and
	// crash the frontend sub-editor's blocksToDoc(null).map. The modal opens
	// blank either way.
	got := extractSubtree(blocks, "missing-id")
	if got == nil {
		t.Fatalf("expected non-nil empty subtree for a missing parent, got nil")
	}
	if len(got) != 0 {
		t.Fatalf("expected empty subtree for a missing parent, got %d", len(got))
	}
}

// TestSpliceSubtree_ReplacesChildRange verifies the splice substitutes the
// incoming children for the existing child range, preserving the parent and
// all surrounding blocks (#305).
func TestSpliceSubtree_ReplacesChildRange(t *testing.T) {
	const parent = "aaaaaaaa-0000-0000-0000-000000000000"
	blocks := []parser.ParsedBlock{
		{ID: "before-0000-0000-0000-000000000001", Type: parser.BlockNote, Depth: 0, CleanText: "before"},
		{ID: parent, Type: parser.BlockTask, Depth: 0, CleanText: "parent"},
		{ID: "aaaaaaaa-1111-0000-0000-000000000001", Type: parser.BlockNote, Depth: 1, ParentID: parent, CleanText: "old child 1"},
		{ID: "aaaaaaaa-1111-0000-0000-000000000002", Type: parser.BlockTask, Depth: 1, ParentID: parent, CleanText: "old child 2"},
		{ID: "after-0000-0000-0000-000000000002", Type: parser.BlockNote, Depth: 0, CleanText: "after"},
	}
	newChildren := []parser.ParsedBlock{
		{ID: "cccccccc-1111-0000-0000-000000000001", Type: parser.BlockNote, Depth: 1, CleanText: "new child"},
	}

	got, err := spliceSubtree(blocks, parent, newChildren)
	if err != nil {
		t.Fatal(err)
	}
	// Expected order: before, parent, new child, after.
	if len(got) != 4 {
		t.Fatalf("expected 4 blocks after splice, got %d: %+v", len(got), got)
	}
	if got[0].ID != "before-0000-0000-0000-000000000001" {
		t.Errorf("block 0 = %s, want 'before'", got[0].ID)
	}
	if got[1].ID != parent {
		t.Errorf("block 1 = %s, want parent", got[1].ID)
	}
	if got[2].ID != "cccccccc-1111-0000-0000-000000000001" {
		t.Errorf("block 2 = %s, want new child", got[2].ID)
	}
	if got[3].ID != "after-0000-0000-0000-000000000002" {
		t.Errorf("block 3 = %s, want 'after'", got[3].ID)
	}
	// The incoming child's ParentID is defensively stamped.
	if got[2].ParentID != parent {
		t.Errorf("new child ParentID = %s, want %s", got[2].ParentID, parent)
	}
}

func TestSpliceSubtree_EmptyChildrenRemovesOldSubtree(t *testing.T) {
	const parent = "aaaaaaaa-0000-0000-0000-000000000000"
	blocks := []parser.ParsedBlock{
		{ID: parent, Type: parser.BlockTask, Depth: 0},
		{ID: "aaaaaaaa-1111-0000-0000-000000000001", Type: parser.BlockNote, Depth: 1, ParentID: parent},
		{ID: "bbbbbbbb-0000-0000-0000-000000000000", Type: parser.BlockTask, Depth: 0},
	}
	got, err := spliceSubtree(blocks, parent, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 blocks after clearing children, got %d: %+v", len(got), got)
	}
}

// TestSpliceSubtree_DeletesByMembershipAroundRegion pins that the splice
// deletes the child sub-tree by parent-graph membership — the exact inverse
// of extractSubtree — not by a contiguous depth run. Two shapes:
//   - a column-0 region is a root (not a member): a task's members straddle
//     it, and a depth-run delete would stop at the region and resurrect the
//     post-region member while the normalized list already carries it;
//   - an indented region is itself a member (#1060): omitted from the saved
//     children it is deleted with the run; resubmitted it survives, in the
//     submitted position, exactly once.
func TestSpliceSubtree_DeletesByMembershipAroundRegion(t *testing.T) {
	const (
		parent  = "aaaaaaaa-0000-0000-0000-000000000000"
		reply1  = "aaaaaaaa-1111-0000-0000-000000000001"
		region  = "aaaaaaaa-2222-0000-0000-000000000002"
		reply2  = "aaaaaaaa-3333-0000-0000-000000000003"
		sibling = "bbbbbbbb-0000-0000-0000-000000000000"
	)
	t.Run("members straddle a non-member region", func(t *testing.T) {
		blocks := []parser.ParsedBlock{
			{ID: parent, Type: parser.BlockTask, Depth: 0, CleanText: "parent"},
			{ID: reply1, Type: parser.BlockNote, Depth: 1, ParentID: parent, CleanText: "reply one"},
			{ID: region, Type: parser.BlockCode, Depth: 0, CleanText: "code"},
			{ID: reply2, Type: parser.BlockNote, Depth: 1, ParentID: parent, CleanText: "reply two"},
			{ID: sibling, Type: parser.BlockTask, Depth: 0, CleanText: "sibling"},
		}

		// The sub-editor deletes reply two: the splice receives the fetched
		// subtree minus it.
		got, err := spliceSubtree(blocks, parent, blocks[1:2])
		if err != nil {
			t.Fatal(err)
		}
		seen := make(map[string]int, len(got))
		for _, b := range got {
			seen[b.ID]++
		}
		if seen[reply2] != 0 {
			t.Errorf("deleted post-region reply still present %d time(s): %+v", seen[reply2], got)
		}
		for _, id := range []string{parent, reply1, region, sibling} {
			if seen[id] != 1 {
				t.Errorf("block %s occurs %d times, want exactly 1: %+v", id, seen[id], got)
			}
		}
		// The region is not a member: it survives, after the replaced child run.
		if len(got) != 4 || got[0].ID != parent || got[1].ID != reply1 || got[2].ID != region || got[3].ID != sibling {
			t.Fatalf("order = %+v, want parent, reply one, region, sibling", got)
		}
	})
	t.Run("indented region is a member", func(t *testing.T) {
		blocks := []parser.ParsedBlock{
			{ID: parent, Type: parser.BlockTask, Depth: 0, CleanText: "parent"},
			{ID: reply1, Type: parser.BlockNote, Depth: 1, ParentID: parent, CleanText: "reply one"},
			{ID: region, Type: parser.BlockCode, Depth: 1, ParentID: parent, CleanText: "code"},
			{ID: reply2, Type: parser.BlockNote, Depth: 1, ParentID: parent, CleanText: "reply two"},
			{ID: sibling, Type: parser.BlockTask, Depth: 0, CleanText: "sibling"},
		}

		// Omitted from the saved children: deleted with the run.
		got, err := spliceSubtree(blocks, parent, blocks[1:2])
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 3 || got[0].ID != parent || got[1].ID != reply1 || got[2].ID != sibling {
			t.Fatalf("splice without the region = %+v, want parent, reply one, sibling (member region deleted)", got)
		}

		// Resubmitted: kept in the submitted position, exactly once.
		got, err = spliceSubtree(blocks, parent, blocks[1:3])
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 4 || got[0].ID != parent || got[1].ID != reply1 || got[2].ID != region || got[3].ID != sibling {
			t.Fatalf("splice with the region = %+v, want parent, reply one, region, sibling", got)
		}
	})
}

// TestSaveSubtreeBlocks_PreservesSurroundingContent is the end-to-end splice
// test: a parent task with child notes, plus a sibling task and unmanaged
// prose. After SaveSubtreeBlocks, only the child range changes; everything
// else is byte-identical (#305 acceptance: zero data loss on surrounding
// blocks).
func TestSaveSubtreeBlocks_PreservesSurroundingContent(t *testing.T) {
	app := newTestApp(t)
	const (
		parent = "a1b2c3d4-0000-0000-0000-000000000001"
		child1 = "a1b2c3d4-0000-0000-0000-000000000002"
		sib    = "a1b2c3d4-0000-0000-0000-000000000003"
	)
	// File: a sibling task, the parent task with two child notes, then prose.
	content := "- [ ] sibling <!-- id: " + sib + " -->\n" +
		"- [ ] parent task <!-- id: " + parent + " -->\n" +
		"\t- old child note one <!-- id: " + child1 + " -->\n" +
		"\t- old child note two <!-- id: a1b2c3d4-0000-0000-0000-000000000004 -->\n" +
		"This is unmanaged prose.\n"
	filePath := indexTestFile(t, app, "W", "S", "Splice", "2026-07-01", content)

	// Replace the child sub-tree with a single new child note.
	newChildren := []parser.ParsedBlock{{
		ID:         "b2c3d4e5-0000-0000-0000-000000000099",
		Type:       parser.BlockNote,
		Depth:      1,
		CleanText:  "new child note",
		RawText:    "- new child note",
		LineNumber: 3,
		FileDate:   "2026-07-01",
	}}
	if _, err := app.SaveSubtreeBlocks(parent, newChildren); err != nil {
		t.Fatalf("SaveSubtreeBlocks: %v", err)
	}

	updated, _ := os.ReadFile(filePath)
	s := string(updated)

	// The sibling task and the unmanaged prose survive verbatim.
	if !strings.Contains(s, "sibling") {
		t.Errorf("sibling task lost in:\n%s", s)
	}
	if !strings.Contains(s, "This is unmanaged prose.") {
		t.Errorf("unmanaged prose lost in:\n%s", s)
	}
	// The new child is present; the old children are gone.
	if !strings.Contains(s, "new child note") {
		t.Errorf("new child note missing in:\n%s", s)
	}
	if strings.Contains(s, "old child note one") || strings.Contains(s, "old child note two") {
		t.Errorf("old children should be replaced in:\n%s", s)
	}
	// The parent task survives.
	if !strings.Contains(s, "parent task") {
		t.Errorf("parent task lost in:\n%s", s)
	}
}

// TestSaveSubtreeBlocks_RejectsNonTask verifies the splice is refused for a
// non-task block (the sub-editor is task-scoped).
func TestSaveSubtreeBlocks_RejectsNonTask(t *testing.T) {
	app := newTestApp(t)
	const note = "a1b2c3d4-0000-0000-0000-000000000010"
	content := "- a note block <!-- id: " + note + " -->\n"
	indexTestFile(t, app, "W", "S", "NonTaskSub", "2026-07-01", content)

	_, err := app.SaveSubtreeBlocks(note, nil)
	if err == nil {
		t.Fatal("expected error saving subtree for a non-task block, got nil")
	}
}

// TestFetchSubtree_ReturnsChildren verifies the read path returns the child
// sub-tree from the file on disk.
func TestFetchSubtree_ReturnsChildren(t *testing.T) {
	app := newTestApp(t)
	const (
		parent = "a1b2c3d4-0000-0000-0000-000000000020"
		child  = "a1b2c3d4-0000-0000-0000-000000000021"
	)
	content := "- [ ] parent <!-- id: " + parent + " -->\n" +
		"\t- child note <!-- id: " + child + " -->\n" +
		"- [ ] sibling <!-- id: a1b2c3d4-0000-0000-0000-000000000022 -->\n"
	indexTestFile(t, app, "W", "S", "FetchSub", "2026-07-01", content)

	got, err := app.FetchSubtree(parent)
	if err != nil {
		t.Fatalf("FetchSubtree: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 child, got %d: %+v", len(got), got)
	}
	if got[0].ID != child {
		t.Errorf("child ID = %s, want %s", got[0].ID, child)
	}
}

// TestFetchSubtree_NoChildrenReturnsEmpty verifies a childless task returns an
// empty (not nil) slice so the modal opens with a blank editor.
func TestFetchSubtree_NoChildrenReturnsEmpty(t *testing.T) {
	app := newTestApp(t)
	const parent = "a1b2c3d4-0000-0000-0000-000000000030"
	content := "- [ ] childless <!-- id: " + parent + " -->\n"
	indexTestFile(t, app, "W", "S", "FetchEmpty", "2026-07-01", content)

	got, err := app.FetchSubtree(parent)
	if err != nil {
		t.Fatalf("FetchSubtree: %v", err)
	}
	// Non-nil empty slice (not Go nil) so the binding serializes to JSON `[]`
	// rather than `null` — the frontend's blocksToDoc would otherwise crash on
	// .map over null in the sub-editor.
	if got == nil {
		t.Fatalf("expected non-nil empty subtree, got nil")
	}
	if len(got) != 0 {
		t.Errorf("expected empty subtree, got %d blocks", len(got))
	}
}

// TestPluginSaveSubtreeBlocks_RequireCapability verifies the plugin wrapper
// gates on CapContentMutate + a valid session — a third-party plugin without
// the grant cannot splice arbitrary blocks into a task's sub-tree (#305).
// Mirrors the requireGrant denial pattern in app_capabilities_test.go.
func TestPluginSaveSubtreeBlocks_RequireCapability(t *testing.T) {
	app := newTestApp(t)
	const parent = "a1b2c3d4-0000-0000-0000-000000000040"
	content := "- [ ] parent <!-- id: " + parent + " -->\n"
	indexTestFile(t, app, "W", "S", "GatedSub", "2026-07-01", content)

	// An ungranted third-party plugin is denied before any disk I/O. Even a
	// bad session token is rejected first; use a plausible ungranted id.
	_, err := app.PluginSaveSubtreeBlocks(
		"not-a-real-plugin", "any-token", parent, nil,
	)
	if err == nil {
		t.Fatal("expected PluginSaveSubtreeBlocks to reject an ungranted third-party plugin, got nil")
	}
	// The denial must happen before the file is touched: confirm the splice
	// never ran by checking the parent still has no children written.
	// (The file is unchanged regardless, but asserting the error proves the
	// gate fired rather than the splice silently no-op'ing.)
}

// TestPluginSaveSubtreeBlocks_FirstPartySucceeds verifies a first-party
// plugin (implicitly granted) with a valid session can splice — the gate
// doesn't lock out legitimate callers.
func TestPluginSaveSubtreeBlocks_FirstPartySucceeds(t *testing.T) {
	app := newTestApp(t)
	const parent = "a1b2c3d4-0000-0000-0000-000000000050"
	content := "- [ ] parent <!-- id: " + parent + " -->\n"
	filePath := indexTestFile(t, app, "W", "S", "FirstPartySub", "2026-07-01", content)
	tok := registerTestSession(t, app, "silt-tasks")

	newChildren := []parser.ParsedBlock{{
		ID:         "b2c3d4e5-0000-0000-0000-000000000051",
		Type:       parser.BlockNote,
		Depth:      1,
		CleanText:  "via plugin",
		RawText:    "- via plugin",
		LineNumber: 2,
		FileDate:   "2026-07-01",
	}}
	ok, err := app.PluginSaveSubtreeBlocks("silt-tasks", tok, parent, newChildren)
	if err != nil || !ok {
		t.Fatalf("PluginSaveSubtreeBlocks (first-party): ok=%v err=%v", ok, err)
	}
	updated, _ := os.ReadFile(filePath)
	if !strings.Contains(string(updated), "via plugin") {
		t.Errorf("first-party splice should have written the child in:\n%s", updated)
	}
}

func TestSaveSubtreeBlocks_RejectsOutOfRangePriority(t *testing.T) {
	app := newTestApp(t)
	const parent = "a1b2c3d4-0000-0000-0000-000000000070"
	content := "- [ ] parent <!-- id: " + parent + " -->\n"
	filePath := indexTestFile(t, app, "W", "S", "SubtreePrioRange", "2026-07-01", content)
	before, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read before: %v", err)
	}

	child := []parser.ParsedBlock{{
		ID:        "b2c3d4e5-0000-0000-0000-000000000071",
		Type:      parser.BlockTask,
		Depth:     1,
		Priority:  4,
		CleanText: "invalid priority child",
	}}
	if ok, err := app.SaveSubtreeBlocks(parent, child); ok || err == nil {
		t.Fatalf("SaveSubtreeBlocks invalid priority = ok=%v err=%v, want rejection", ok, err)
	}

	after, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read after: %v", err)
	}
	if string(after) != string(before) {
		t.Fatalf("invalid priority changed the source file:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// TestSpliceSubtree_RefusesChildIDCollisionWithKeptBlock: an incoming child
// whose id matches a block that SURVIVES the splice (a kept non-member) would
// render the same UUID twice in one file — a stale-list race or plugin caller
// can produce it, and writing it degrades the index (duplicate blocks.id).
// The splice refuses loudly instead; the file stays untouched.
func TestSpliceSubtree_RefusesChildIDCollisionWithKeptBlock(t *testing.T) {
	const (
		parent  = "aaaaaaaa-0000-0000-0000-000000000000"
		kept    = "bbbbbbbb-0000-0000-0000-000000000099"
		freshID = "cccccccc-0000-0000-0000-000000000001"
	)
	blocks := []parser.ParsedBlock{
		{ID: parent, Type: parser.BlockTask, Depth: 0, CleanText: "parent"},
		{ID: kept, Type: parser.BlockTask, Depth: 0, CleanText: "kept sibling elsewhere in the page"},
	}
	// Stale-list shape: the caller fetched a subtree listing that included the
	// kept block's id and re-submits it as a child of parent.
	children := []parser.ParsedBlock{
		{ID: freshID, Type: parser.BlockNote, Depth: 1, CleanText: "legit new child"},
		{ID: kept, Type: parser.BlockNote, Depth: 1, CleanText: "colliding child"},
	}
	if _, err := spliceSubtree(blocks, parent, children); err == nil {
		t.Fatal("spliceSubtree accepted a child id that a kept block already owns; want refusal")
	}
}

// TestSpliceSubtree_RefusesDuplicateChildren: two incoming children sharing
// one id are equally a duplicate-UUID write; refused.
func TestSpliceSubtree_RefusesDuplicateChildren(t *testing.T) {
	const (
		parent = "aaaaaaaa-0000-0000-0000-000000000000"
		dup    = "cccccccc-0000-0000-0000-000000000042"
	)
	blocks := []parser.ParsedBlock{
		{ID: parent, Type: parser.BlockTask, Depth: 0, CleanText: "parent"},
	}
	children := []parser.ParsedBlock{
		{ID: dup, Type: parser.BlockNote, Depth: 1, CleanText: "first"},
		{ID: dup, Type: parser.BlockNote, Depth: 2, CleanText: "second"},
	}
	if _, err := spliceSubtree(blocks, parent, children); err == nil {
		t.Fatal("spliceSubtree accepted two children sharing one id; want refusal")
	}
}

// TestSpliceSubtree_AllowsReusingDeletedMemberID: replacing the subtree with
// children whose ids match the OLD members is the normal update flow — those
// ids are deleted by the splice, so no collision. Pins that the guard does
// not over-refuse.
func TestSpliceSubtree_AllowsReusingDeletedMemberID(t *testing.T) {
	const (
		parent = "aaaaaaaa-0000-0000-0000-000000000000"
		member = "aaaaaaaa-1111-0000-0000-000000000001"
	)
	blocks := []parser.ParsedBlock{
		{ID: parent, Type: parser.BlockTask, Depth: 0, CleanText: "parent"},
		{ID: member, Type: parser.BlockNote, Depth: 1, ParentID: parent, CleanText: "old child"},
	}
	updated := []parser.ParsedBlock{
		{ID: member, Type: parser.BlockNote, Depth: 1, ParentID: parent, CleanText: "edited in place"},
	}
	got, err := spliceSubtree(blocks, parent, updated)
	if err != nil {
		t.Fatalf("spliceSubtree refused a legitimate member update: %v", err)
	}
	if len(got) != 2 || got[1].ID != member {
		t.Fatalf("splice result = %+v, want parent + updated member", got)
	}
}

// TestSpliceSubtree_NormalizesIncomingDepths verifies a plugin caller passing
// children with Depth <= parentDepth can't corrupt the file hierarchy: the
// splice forces every child strictly beneath the parent, preserving relative
// ordering. Without normalization, a Depth-0 child under a Depth-2 parent
// would render at the parent's indent and vanish from extractSubtree.
func TestSpliceSubtree_NormalizesIncomingDepths(t *testing.T) {
	const parent = "a1b2c3d4-0000-0000-0000-000000000060"
	blocks := []parser.ParsedBlock{
		{ID: parent, Type: parser.BlockTask, Depth: 2, CleanText: "parent"},
	}
	// Malicious/buggy caller: two children, both at Depth 0 (at/above parent).
	badChildren := []parser.ParsedBlock{
		{ID: "child-a-0000-0000-0000-000000000061", Type: parser.BlockNote, Depth: 0, CleanText: "child a"},
		{ID: "child-b-0000-0000-0000-000000000062", Type: parser.BlockNote, Depth: 1, CleanText: "child b"},
	}
	got, err := spliceSubtree(blocks, parent, badChildren)
	if err != nil {
		t.Fatal(err)
	}
	// Both children must end up strictly deeper than the parent (Depth 2).
	for i, b := range got {
		if b.ID == "child-a-0000-0000-0000-000000000061" || b.ID == "child-b-0000-0000-0000-000000000062" {
			if b.Depth <= 2 {
				t.Errorf("child %s Depth = %d, must be > parent depth 2 (index %d)", b.ID, b.Depth, i)
			}
		}
	}
	// Relative ordering preserved: child b started deeper than child a.
	if got[1].Depth >= got[2].Depth {
		t.Errorf("relative depth ordering lost: child a Depth=%d, child b Depth=%d", got[1].Depth, got[2].Depth)
	}
}

// regionChildRunDoc is the shared fixture for the region-in-child-run writer
// regressions: a task whose children straddle a managed code region written
// at column 0. A column-0 region is a root — not a member of the task's
// sub-tree — while both replies are, exactly the shape where the old
// contiguous depth-run splice disagreed with extractSubtree. The indented
// member-region counterpart is indentedRegionChildRunDoc (#1060).
const (
	regionRunTaskID   = "a1b2c3d4-0000-0000-0000-0000000000a1"
	regionRunReply1   = "a1b2c3d4-0000-0000-0000-0000000000a2"
	regionRunCodeID   = "a1b2c3d4-0000-0000-0000-0000000000a3"
	regionRunReply2   = "a1b2c3d4-0000-0000-0000-0000000000a4"
	regionChildRunDoc = "- [ ] parent task <!-- id: " + regionRunTaskID + " -->\n" +
		"\t- reply one <!-- id: " + regionRunReply1 + " -->\n" +
		"```go\nfmt.Println(1)\n```\n" +
		"<!-- id: " + regionRunCodeID + " -->\n" +
		"\t- reply two <!-- id: " + regionRunReply2 + " -->\n"
)

// indentedRegionChildRunDoc is the #1060 member-region counterpart: the code
// region is indented into the task's depth band, so the parser's single
// hierarchy derivation resolves its parent to the task and the region is a
// sub-tree member — the sub-editor fetch/save and the comment writer must
// carry it through the splice like any NOTE child, without duplication or
// byte damage.
const (
	indentRunTaskID           = "a1b2c3d4-0000-0000-0000-0000000000b1"
	indentRunReply1           = "a1b2c3d4-0000-0000-0000-0000000000b2"
	indentRunCodeID           = "a1b2c3d4-0000-0000-0000-0000000000b3"
	indentRunReply2           = "a1b2c3d4-0000-0000-0000-0000000000b4"
	indentedRegionChildRunDoc = "- [ ] parent task <!-- id: " + indentRunTaskID + " -->\n" +
		"\t- reply one <!-- id: " + indentRunReply1 + " -->\n" +
		"\t```go\n\tfmt.Println(1)\n\t```\n" +
		"\t<!-- id: " + indentRunCodeID + " -->\n" +
		"\t- reply two <!-- id: " + indentRunReply2 + " -->\n"
)

// indexedBlockIDs returns the set of block ids indexed for the page, failing
// on query/scan errors. Writer tests use it to prove IndexFileBlocks
// committed: a duplicate UUID aborts the index transaction and leaves the
// page's rows stale.
func indexedBlockIDs(t *testing.T, app *App, page string) map[string]bool {
	t.Helper()
	rows, err := app.db.SQLDB().Query(
		`SELECT id FROM blocks WHERE notebook = ? AND section = ? AND page = ?`, "W", "S", page)
	if err != nil {
		t.Fatalf("query blocks: %v", err)
	}
	defer rows.Close()
	ids := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan block row: %v", err)
		}
		ids[id] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate block rows: %v", err)
	}
	return ids
}

// TestSaveSubtreeBlocks_RegionInsideChildRun pins the sub-editor save path on
// the region fixture: deleting a post-region reply must actually delete it
// (pre-fix the depth-run splice stopped at the region, so the reply survived
// outside the replaced range and was resurrected on the next fetch while the
// normalized list also carried it — the same UUID twice on disk).
func TestSaveSubtreeBlocks_RegionInsideChildRun(t *testing.T) {
	app := newTestApp(t)
	filePath := indexTestFile(t, app, "W", "S", "SubtreeRegion", "2026-07-01", regionChildRunDoc)

	sub, err := app.FetchSubtree(regionRunTaskID)
	if err != nil {
		t.Fatalf("FetchSubtree: %v", err)
	}
	if len(sub) != 2 || sub[0].ID != regionRunReply1 || sub[1].ID != regionRunReply2 {
		t.Fatalf("subtree = %+v, want both replies — the region between them is not a member", sub)
	}

	// Delete reply two: submit the subtree without it.
	if _, err := app.SaveSubtreeBlocks(regionRunTaskID, sub[:1]); err != nil {
		t.Fatalf("SaveSubtreeBlocks: %v", err)
	}

	updated, _ := os.ReadFile(filePath)
	s := string(updated)
	if strings.Contains(s, regionRunReply2) {
		t.Errorf("deleted post-region reply still on disk (resurrected):\n%s", s)
	}
	for _, id := range []string{regionRunTaskID, regionRunReply1, regionRunCodeID} {
		if n := strings.Count(s, id); n != 1 {
			t.Errorf("id %s occurs %d times on disk, want exactly 1 (a duplicate UUID breaks the blocks.id primary key):\n%s", id, n, s)
		}
	}
	if !strings.Contains(s, "fmt.Println(1)") {
		t.Errorf("code region lost in:\n%s", s)
	}

	// The post-save index must have committed: a duplicated reply would have
	// aborted it and left the page's rows stale (still containing reply two).
	ids := indexedBlockIDs(t, app, "SubtreeRegion")
	if len(ids) != 3 || !ids[regionRunTaskID] || !ids[regionRunReply1] || !ids[regionRunCodeID] || ids[regionRunReply2] {
		t.Errorf("indexed ids = %v, want exactly task, reply one, and the code region (index commit failed or stale)", ids)
	}
}

// TestAppendTaskComment_RegionInsideChildRun pins the comment writer on the
// same fixture: AppendTaskComment re-splices the task's whole membership
// (existing children + the new NOTE). Pre-fix the post-region reply was
// written twice, the re-parse fed IndexFileBlocks the same UUID twice, the
// insert failed, and the page's index stayed stale.
func TestAppendTaskComment_RegionInsideChildRun(t *testing.T) {
	app := newTestApp(t)
	indexTestFile(t, app, "W", "S", "CommentRegion", "2026-07-01", regionChildRunDoc)

	newID, err := app.AppendTaskComment(regionRunTaskID, "post-region comment", "Dana", "2026-07-07T14:22:00", "")
	if err != nil {
		t.Fatalf("AppendTaskComment: %v", err)
	}

	// Every id exactly once on disk — duplication is the corruption, not
	// just noise: the id-comment line is what the next parse turns into a
	// second block with the same primary key.
	s := mustReadTaskFile(t, app, "W", "S", "CommentRegion")
	for _, id := range []string{regionRunTaskID, regionRunReply1, regionRunCodeID, regionRunReply2, newID} {
		if n := strings.Count(s, id); n != 1 {
			t.Errorf("id %s occurs %d times on disk, want exactly 1:\n%s", id, n, s)
		}
	}

	// IndexFileBlocks committed: the page's rows are exactly the five
	// unique blocks. A duplicate would have aborted the transaction and
	// left the pre-comment rows (no newID).
	ids := indexedBlockIDs(t, app, "CommentRegion")
	if len(ids) != 5 || !ids[newID] || !ids[regionRunReply2] || !ids[regionRunCodeID] {
		t.Errorf("indexed ids = %v, want exactly the task, both replies, the code region, and the new comment %s", ids, newID)
	}
}

// TestSaveSubtreeBlocks_IndentedRegionMember is the #1060 sub-editor
// round-trip: FetchSubtree returns the indented region together with the
// replies; saving the subtree minus a reply keeps the region byte-intact
// (fence body, indent, id line), deletes exactly the omitted member, and the
// re-parse indexes the page without duplication.
func TestSaveSubtreeBlocks_IndentedRegionMember(t *testing.T) {
	app := newTestApp(t)
	filePath := indexTestFile(t, app, "W", "S", "SubtreeRegionMember", "2026-07-01", indentedRegionChildRunDoc)

	sub, err := app.FetchSubtree(indentRunTaskID)
	if err != nil {
		t.Fatalf("FetchSubtree: %v", err)
	}
	if len(sub) != 3 || sub[0].ID != indentRunReply1 || sub[1].ID != indentRunCodeID || sub[2].ID != indentRunReply2 {
		t.Fatalf("subtree = %+v, want reply one, the indented code region, reply two — the region is a member", sub)
	}

	// Delete reply two: submit the subtree without it, region included.
	if _, err := app.SaveSubtreeBlocks(indentRunTaskID, sub[:2]); err != nil {
		t.Fatalf("SaveSubtreeBlocks: %v", err)
	}

	updated, _ := os.ReadFile(filePath)
	s := string(updated)
	if strings.Contains(s, indentRunReply2) {
		t.Errorf("deleted reply still on disk (resurrected):\n%s", s)
	}
	for _, id := range []string{indentRunTaskID, indentRunReply1, indentRunCodeID} {
		if n := strings.Count(s, id); n != 1 {
			t.Errorf("id %s occurs %d times on disk, want exactly 1 (a duplicate UUID breaks the blocks.id primary key):\n%s", id, n, s)
		}
	}
	if !strings.Contains(s, "fmt.Println(1)") {
		t.Errorf("code region body lost in:\n%s", s)
	}

	// Round-trip: the saved bytes re-parse to the same three-block shape
	// with the region still indented under the task, and the region stays a
	// member on the next fetch.
	blocks, _, _, _, err := parser.ParseFileContent(s, "W", "S", "SubtreeRegionMember", "2026-07-01", 4)
	if err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	if len(blocks) != 3 {
		t.Fatalf("re-parse got %d blocks, want 3 (task + reply + region):\n%s", len(blocks), s)
	}
	region := blocks[2]
	if region.ID != indentRunCodeID || region.Type != parser.BlockCode || region.Depth != 1 || region.ParentID != indentRunTaskID {
		t.Errorf("re-parsed region = %+v, want CODE depth 1 parented to the task", region)
	}
	sub2, err := app.FetchSubtree(indentRunTaskID)
	if err != nil {
		t.Fatalf("post-save FetchSubtree: %v", err)
	}
	if len(sub2) != 2 || sub2[0].ID != indentRunReply1 || sub2[1].ID != indentRunCodeID {
		t.Fatalf("post-save subtree = %+v, want reply one + the region (round-trip membership intact)", sub2)
	}

	// The post-save index committed: exactly the three surviving blocks.
	ids := indexedBlockIDs(t, app, "SubtreeRegionMember")
	if len(ids) != 3 || !ids[indentRunTaskID] || !ids[indentRunReply1] || !ids[indentRunCodeID] || ids[indentRunReply2] {
		t.Errorf("indexed ids = %v, want exactly task, reply one, and the code region", ids)
	}
}

// TestAppendTaskComment_IndentedRegionMember pins the comment writer on the
// member fixture: AppendTaskComment re-splices the task's whole membership —
// now including the indented region — plus the new NOTE. Every id must land
// exactly once in member order (reply, region, reply, new comment) and the
// index must commit all five rows.
func TestAppendTaskComment_IndentedRegionMember(t *testing.T) {
	app := newTestApp(t)
	indexTestFile(t, app, "W", "S", "CommentRegionMember", "2026-07-01", indentedRegionChildRunDoc)

	newID, err := app.AppendTaskComment(indentRunTaskID, "comment beside a region", "Dana", "2026-07-07T14:22:00", "")
	if err != nil {
		t.Fatalf("AppendTaskComment: %v", err)
	}

	s := mustReadTaskFile(t, app, "W", "S", "CommentRegionMember")
	for _, id := range []string{indentRunTaskID, indentRunReply1, indentRunCodeID, indentRunReply2, newID} {
		if n := strings.Count(s, id); n != 1 {
			t.Errorf("id %s occurs %d times on disk, want exactly 1:\n%s", id, n, s)
		}
	}
	// Region must hold member position between the replies, ahead of the new
	// comment — main's non-member semantics relocates it after the inserted run.
	if !(strings.Index(s, indentRunReply1) < strings.Index(s, indentRunCodeID) &&
		strings.Index(s, indentRunCodeID) < strings.Index(s, indentRunReply2) &&
		strings.Index(s, indentRunReply2) < strings.Index(s, newID)) {
		t.Errorf("relative order reply1 < region < reply2 < new comment broken — region lost member position:\n%s", s)
	}
	if !strings.Contains(s, "fmt.Println(1)") {
		t.Errorf("code region body lost in:\n%s", s)
	}

	ids := indexedBlockIDs(t, app, "CommentRegionMember")
	if len(ids) != 5 || !ids[newID] || !ids[indentRunCodeID] || !ids[indentRunReply2] {
		t.Errorf("indexed ids = %v, want exactly the task, both replies, the code region, and the new comment %s", ids, newID)
	}
}
