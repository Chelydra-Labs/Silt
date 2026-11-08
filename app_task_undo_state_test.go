package main

import (
	"os"
	"testing"
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
