package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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
