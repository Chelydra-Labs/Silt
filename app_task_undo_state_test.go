package main

import (
	"os"
	"testing"

	"silt/backend/plugins"
)

func TestGetTaskUndoState_PeeksWithoutPopping(t *testing.T) {
	app := newTestApp(t)
	const id = "a11d0020-1111-1111-1111-111111111111"
	content := "- [ ] ship <!-- id: " + id + " -->\n"
	indexTestFile(t, app, "W", "S", "UndoState", "2026-07-01", content)

	state := app.GetTaskUndoState()
	if state.CanUndo || state.Remaining != 0 {
		t.Fatalf("empty journal state = %+v, want canUndo=false remaining=0", state)
	}
	if state.ChangedFields == nil || state.BlockIDs == nil {
		t.Errorf("empty state must expose empty (non-nil) slices: %+v", state)
	}

	if err := app.SetTaskOwner(id, "Alice"); err != nil {
		t.Fatalf("SetTaskOwner: %v", err)
	}
	state = app.GetTaskUndoState()
	if !state.CanUndo || state.Remaining != 1 {
		t.Fatalf("state after mutation = %+v, want canUndo with one entry", state)
	}
	if !taskUndoContains(state.ChangedFields, "owner") {
		t.Errorf("changed fields = %v, want owner", state.ChangedFields)
	}
	if len(state.BlockIDs) != 1 || state.BlockIDs[0] != id {
		t.Errorf("block ids = %v, want [%s]", state.BlockIDs, id)
	}
	if state.Notebook != "W" || state.Section != "S" || state.Page != "UndoState" || state.Source != "vault" {
		t.Errorf("state locator = %+v, want W/S/UndoState/vault", state)
	}

	// The peek is non-destructive: a second read still sees the entry.
	if again := app.GetTaskUndoState(); again.Remaining != 1 {
		t.Fatalf("peek popped the entry: remaining = %d, want 1", again.Remaining)
	}

	if _, err := app.UndoLastTaskMutation(); err != nil {
		t.Fatalf("UndoLastTaskMutation: %v", err)
	}
	if after := app.GetTaskUndoState(); after.CanUndo || after.Remaining != 0 {
		t.Fatalf("state after undo = %+v, want clean empty state", after)
	}
}

// The peek must disclose the page undo would actually write, not the page
// recorded before the block moved: a surface renders "what will be undone"
// from this state.
func TestGetTaskUndoState_FollowsBlockMove(t *testing.T) {
	app := newTestApp(t)
	const (
		id       = "a11d0027-1111-1111-1111-111111111111"
		anchorID = "a11d0028-1111-1111-1111-111111111111"
	)
	indexTestFile(t, app, "W", "S", "StateMoveSource", "2026-07-01",
		"- [ ] ship <!-- id: "+id+" -->\n")
	indexTestFile(t, app, "W", "S", "StateMoveTarget", "2026-07-01",
		"- destination <!-- id: "+anchorID+" -->\n")

	if err := app.SetTaskOwner(id, "Alice"); err != nil {
		t.Fatalf("SetTaskOwner: %v", err)
	}
	if state := app.GetTaskUndoState(); state.Page != "StateMoveSource" {
		t.Fatalf("state before move = %+v, want the recorded page", state)
	}

	const pluginID = "state-mover"
	token := registerTestSession(t, app, pluginID)
	if err := app.RequestCapability(pluginID, string(plugins.CapContentMutate), ""); err != nil {
		t.Fatalf("RequestCapability: %v", err)
	}
	if err := app.PluginMoveBlock(pluginID, token, id, "", "W", "S", "StateMoveTarget"); err != nil {
		t.Fatalf("PluginMoveBlock: %v", err)
	}

	state := app.GetTaskUndoState()
	if state.Notebook != "W" || state.Section != "S" || state.Page != "StateMoveTarget" {
		t.Fatalf("state after move = %+v, want the block's current page", state)
	}
	result, err := app.UndoLastTaskMutation()
	if err != nil {
		t.Fatalf("UndoLastTaskMutation: %v", err)
	}
	if result.Notebook != state.Notebook || result.Section != state.Section || result.Page != state.Page {
		t.Errorf("disclosed locator %s/%s/%s disagrees with the undo target %s/%s/%s",
			state.Notebook, state.Section, state.Page, result.Notebook, result.Section, result.Page)
	}
}

func TestPluginGetTaskUndoState_NeedsSessionNotGrant(t *testing.T) {
	app := newTestApp(t)
	const pluginID = "third-party"
	token := registerTestSession(t, app, pluginID)

	if _, err := app.PluginGetTaskUndoState(pluginID, "wrong-token"); err == nil {
		t.Fatal("read-only state must reject a mismatched session token")
	}
	// No content-mutate grant is requested: a peek writes nothing.
	state, err := app.PluginGetTaskUndoState(pluginID, token)
	if err != nil {
		t.Fatalf("PluginGetTaskUndoState: %v", err)
	}
	if state.CanUndo || state.Remaining != 0 {
		t.Fatalf("state = %+v, want an empty journal", state)
	}
}

func TestUndoLastTaskMutation_SkippedResultHasNoChangedFields(t *testing.T) {
	app := newTestApp(t)
	const id = "a11d0021-1111-1111-1111-111111111111"
	content := "- [ ] ship <!-- id: " + id + " -->\n"
	filePath := indexTestFile(t, app, "W", "S", "UndoSkipFields", "2026-07-01", content)

	if err := app.SetTaskOwner(id, "Alice"); err != nil {
		t.Fatalf("SetTaskOwner: %v", err)
	}
	// Remove the page behind the index's back: the replay finds the recorded
	// path but no file, so it skips without restoring anything.
	if err := os.Remove(filePath); err != nil {
		t.Fatalf("remove page: %v", err)
	}
	result, err := app.UndoLastTaskMutation()
	if err != nil {
		t.Fatalf("UndoLastTaskMutation: %v", err)
	}
	if !result.Skipped || result.Undone {
		t.Fatalf("result = %+v, want skipped", result)
	}
	if len(result.ChangedFields) != 0 {
		t.Errorf("skipped result advertised restored fields: %v", result.ChangedFields)
	}
	if taskUndoLen(app) != 0 {
		t.Error("skipped entry must be popped")
	}
}
