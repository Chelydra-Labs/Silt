package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"silt/backend/parser"
	"silt/backend/plugins"
)

// A page rename after the mutation changes the file path recorded in the
// journal; the replay must re-resolve the block's current location instead of
// popping the entry as skipped.
func TestUndoLastTaskMutation_SurvivesPageRename(t *testing.T) {
	app := newTestApp(t)
	const id = "a11d0022-1111-1111-1111-111111111111"
	content := "- [ ] ship <!-- id: " + id + " -->\n"
	indexTestFile(t, app, "W", "S", "BeforeRename", "2026-07-01", content)

	if err := app.SetTaskOwner(id, "Alice"); err != nil {
		t.Fatalf("SetTaskOwner: %v", err)
	}
	if err := app.RenamePage("W", "S", "BeforeRename", "AfterRename"); err != nil {
		t.Fatalf("RenamePage: %v", err)
	}

	result, err := app.UndoLastTaskMutation()
	if err != nil {
		t.Fatalf("UndoLastTaskMutation: %v", err)
	}
	if !result.Undone || result.Skipped {
		t.Fatalf("undo after rename = %+v, want restored", result)
	}
	if result.Notebook != "W" || result.Section != "S" || result.Page != "AfterRename" {
		t.Errorf("result locator = %+v, want the renamed page", result)
	}

	restored, err := os.ReadFile(filepath.Join(app.vaultPath, "W", "S", "AfterRename.md"))
	if err != nil {
		t.Fatalf("read renamed page: %v", err)
	}
	if line := taskLineForID(string(restored), id); strings.Contains(line, "[owner::") {
		t.Errorf("undo did not restore the owner on the renamed page: %q", line)
	}
}

// A block moved to another page after the mutation leaves the recorded page
// still present (so the file read succeeds) but the target block absent there.
// The replay must find it at its new location.
func TestUndoLastTaskMutation_SurvivesBlockMoveToAnotherPage(t *testing.T) {
	app := newTestApp(t)
	const (
		id       = "a11d0023-1111-1111-1111-111111111111"
		anchorID = "a11d0024-1111-1111-1111-111111111111"
	)
	indexTestFile(t, app, "W", "S", "MoveSource", "2026-07-01",
		"- [ ] ship <!-- id: "+id+" -->\n")
	targetPath := indexTestFile(t, app, "W", "S", "MoveTarget", "2026-07-01",
		"- destination <!-- id: "+anchorID+" -->\n")

	if err := app.SetTaskOwner(id, "Alice"); err != nil {
		t.Fatalf("SetTaskOwner: %v", err)
	}

	const pluginID = "mover"
	token := registerTestSession(t, app, pluginID)
	if err := app.RequestCapability(pluginID, string(plugins.CapContentMutate), ""); err != nil {
		t.Fatalf("RequestCapability: %v", err)
	}
	if err := app.PluginMoveBlock(pluginID, token, id, "", "W", "S", "MoveTarget"); err != nil {
		t.Fatalf("PluginMoveBlock: %v", err)
	}
	loc, err := app.db.GetBlockLocation(id)
	if err != nil || loc.Page != "MoveTarget" {
		t.Fatalf("block did not move: loc=%+v err=%v", loc, err)
	}

	result, err := app.UndoLastTaskMutation()
	if err != nil {
		t.Fatalf("UndoLastTaskMutation: %v", err)
	}
	if !result.Undone || result.Skipped {
		t.Fatalf("undo after move = %+v, want restored", result)
	}
	if result.Page != "MoveTarget" {
		t.Errorf("result page = %q, want MoveTarget", result.Page)
	}

	restored, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("read target page: %v", err)
	}
	if line := taskLineForID(string(restored), id); strings.Contains(line, "[owner::") {
		t.Errorf("undo did not restore the owner at the block's new location: %q", line)
	}
}

// A cross-source move (vault page → linked root) after the mutation must
// publish the undo's index row and history snapshot under the source that
// holds the written file. Publishing under the recorded source would leave the
// linked row stale and invent a phantom row under vault for a page that does
// not exist there.
func TestUndoLastTaskMutation_SurvivesCrossSourceMove(t *testing.T) {
	app := newTestApp(t)
	enablePageHistory(t, app, 50, 0)

	ext := t.TempDir()
	ln, err := app.LinkNotebook(ext)
	if err != nil {
		t.Fatalf("LinkNotebook: %v", err)
	}
	linkedSource := "linked:" + ln.ID
	const (
		id       = "a11d0025-1111-1111-1111-111111111111"
		anchorID = "a11d0026-1111-1111-1111-111111111111"
	)
	indexTestFile(t, app, "W", "S", "CrossSource", "2026-07-01",
		"- [ ] ship <!-- id: "+id+" -->\n")

	// Seed the linked root's target page in the index exactly as the scanner
	// would: the move resolves its target source from the notebook name and
	// reads the source page's indexed location.
	targetContent := "- destination <!-- id: " + anchorID + " -->\n"
	writeFile(t, filepath.Join(ext, "Plan.md"), targetContent)
	targetBlocks, targetMeta, _, _, err := parser.ParseFileContent(targetContent, ln.DisplayName, "", "Plan", "2026-07-01", app.spacesPerTab)
	if err != nil {
		t.Fatalf("ParseFileContent target: %v", err)
	}
	if err := app.db.IndexFileBlocks(linkedSource, targetMeta.Notebook, targetMeta.Section, targetMeta.Page, targetBlocks, targetMeta.Tags); err != nil {
		t.Fatalf("IndexFileBlocks target: %v", err)
	}

	if err := app.SetTaskOwner(id, "Alice"); err != nil {
		t.Fatalf("SetTaskOwner: %v", err)
	}

	const pluginID = "cross-source-mover"
	token := registerTestSession(t, app, pluginID)
	if err := app.RequestCapability(pluginID, string(plugins.CapContentMutate), ""); err != nil {
		t.Fatalf("RequestCapability: %v", err)
	}
	if err := app.PluginMoveBlock(pluginID, token, id, "", ln.DisplayName, "", "Plan"); err != nil {
		t.Fatalf("PluginMoveBlock: %v", err)
	}
	loc, err := app.db.GetBlockLocation(id)
	if err != nil {
		t.Fatalf("GetBlockLocation after move: %v", err)
	}
	if loc.Source != linkedSource || loc.Page != "Plan" {
		t.Fatalf("block did not move across sources: loc=%+v", loc)
	}

	result, err := app.UndoLastTaskMutation()
	if err != nil {
		t.Fatalf("UndoLastTaskMutation: %v", err)
	}
	if !result.Undone || result.Skipped {
		t.Fatalf("cross-source undo = %+v, want restored", result)
	}

	// The written file is the linked one, so the linked row must carry the
	// restored state...
	linkedBlocks, err := app.db.FetchPageBlocks(linkedSource, targetMeta.Notebook, targetMeta.Section, "Plan")
	if err != nil {
		t.Fatalf("FetchPageBlocks linked: %v", err)
	}
	found := false
	for _, b := range linkedBlocks {
		if b.ID != id {
			continue
		}
		found = true
		if b.Owner != "" {
			t.Errorf("linked index row still reports owner %q after undo", b.Owner)
		}
	}
	if !found {
		t.Fatalf("undo left no linked index row for %s", id)
	}
	// ...and the source the block left must not gain a row for the new page.
	phantom, err := app.db.FetchPageBlocks("vault", targetMeta.Notebook, targetMeta.Section, "Plan")
	if err != nil {
		t.Fatalf("FetchPageBlocks vault: %v", err)
	}
	if len(phantom) != 0 {
		t.Errorf("undo published %d block(s) under vault for the linked page", len(phantom))
	}

	// History attribution follows the same source: the linked page owns the
	// undo snapshot.
	versions, err := app.ListPageVersions(targetMeta.Notebook, targetMeta.Section, "Plan")
	if err != nil {
		t.Fatalf("ListPageVersions: %v", err)
	}
	undoVersion := false
	for _, v := range versions {
		if v.Source == historyReasonUndo {
			undoVersion = true
		}
	}
	if !undoVersion {
		t.Errorf("linked page history has no undo snapshot (versions: %+v)", versions)
	}
}
