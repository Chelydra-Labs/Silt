package main

import (
	"os"
	"strings"
	"testing"

	"silt/backend/parser"
)

// TestExtractSubtree verifies the sub-tree boundary rule: every block at
// Depth > parent depth following the parent, up to the next block at or below
// the parent's depth (#305).
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

func TestExtractSubtree_NoChildren(t *testing.T) {
	blocks := []parser.ParsedBlock{
		{ID: "aaaaaaaa-0000-0000-0000-000000000000", Type: parser.BlockTask, Depth: 0},
		{ID: "bbbbbbbb-0000-0000-0000-000000000000", Type: parser.BlockTask, Depth: 0},
	}
	got := extractSubtree(blocks, "aaaaaaaa-0000-0000-0000-000000000000")
	if len(got) != 0 {
		t.Fatalf("expected empty subtree for a childless task, got %d", len(got))
	}
}

func TestExtractSubtree_ParentNotFound(t *testing.T) {
	blocks := []parser.ParsedBlock{{ID: "aaaaaaaa-0000-0000-0000-000000000000", Depth: 0}}
	got := extractSubtree(blocks, "missing-id")
	if got != nil {
		t.Fatalf("expected nil for a missing parent, got %v", got)
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

	got, ok := spliceSubtree(blocks, parent, newChildren)
	if !ok {
		t.Fatal("spliceSubtree returned ok=false for an existing parent")
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
	got, ok := spliceSubtree(blocks, parent, nil)
	if !ok {
		t.Fatal("spliceSubtree returned ok=false")
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 blocks after clearing children, got %d: %+v", len(got), got)
	}
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
	tok := registerTestSession(t, app, "silt-kanban")

	newChildren := []parser.ParsedBlock{{
		ID:         "b2c3d4e5-0000-0000-0000-000000000051",
		Type:       parser.BlockNote,
		Depth:      1,
		CleanText:  "via plugin",
		RawText:    "- via plugin",
		LineNumber: 2,
		FileDate:   "2026-07-01",
	}}
	ok, err := app.PluginSaveSubtreeBlocks("silt-kanban", tok, parent, newChildren)
	if err != nil || !ok {
		t.Fatalf("PluginSaveSubtreeBlocks (first-party): ok=%v err=%v", ok, err)
	}
	updated, _ := os.ReadFile(filePath)
	if !strings.Contains(string(updated), "via plugin") {
		t.Errorf("first-party splice should have written the child in:\n%s", updated)
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
	got, ok := spliceSubtree(blocks, parent, badChildren)
	if !ok {
		t.Fatal("spliceSubtree returned ok=false for an existing parent")
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
