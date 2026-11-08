package main

import (
	"errors"
	"os"
	"strings"
	"testing"

	"silt/backend/plugins"
)

func taskUndoLen(app *App) int {
	app.taskUndo.mu.Lock()
	defer app.taskUndo.mu.Unlock()
	return len(app.taskUndo.entries)
}

// assertOnlyTargetLineChanged pins the adjacency guarantee: undo must not
// re-render unrelated blocks. The target line may differ (the accepted
// [modified::] re-stamp), every other line must be byte-identical.
func assertOnlyTargetLineChanged(t *testing.T, before, after, targetID string) {
	t.Helper()
	beforeLines := strings.Split(before, "\n")
	afterLines := strings.Split(after, "\n")
	if len(beforeLines) != len(afterLines) {
		t.Fatalf("undo changed the line count: %d -> %d\nbefore:\n%s\nafter:\n%s",
			len(beforeLines), len(afterLines), before, after)
	}
	changed := 0
	for i := range beforeLines {
		if beforeLines[i] == afterLines[i] {
			continue
		}
		changed++
		if !strings.Contains(beforeLines[i], "<!-- id: "+targetID) &&
			!strings.Contains(afterLines[i], "<!-- id: "+targetID) {
			t.Errorf("undo changed a non-target line %d: %q -> %q",
				i, beforeLines[i], afterLines[i])
		}
	}
	if changed != 1 {
		t.Errorf("expected exactly one changed line, got %d\nbefore:\n%s\nafter:\n%s",
			changed, before, after)
	}
}

func TestUndoLastTaskMutation_RestoresBoardStatus(t *testing.T) {
	app := newTestApp(t)
	const id = "a11d0001-1111-1111-1111-111111111111"
	content := "- [ ] drag me <!-- id: " + id + " -->\n"
	filePath := indexTestFile(t, app, "W", "S", "UndoStatus", "2026-07-01", content)

	if _, err := app.UpdateBlockState(id, "DOING"); err != nil {
		t.Fatalf("UpdateBlockState DOING: %v", err)
	}
	if got := taskUndoLen(app); got != 1 {
		t.Fatalf("journal after status write = %d, want 1", got)
	}

	result, err := app.UndoLastTaskMutation()
	if err != nil {
		t.Fatalf("UndoLastTaskMutation: %v", err)
	}
	if !result.Undone || result.Skipped || result.Remaining != 0 {
		t.Fatalf("undo result = %+v, want undone with empty journal", result)
	}
	if len(result.BlockIDs) != 1 || result.BlockIDs[0] != id {
		t.Fatalf("restored ids = %v, want [%s]", result.BlockIDs, id)
	}
	if !taskUndoContains(result.ChangedFields, "status") {
		t.Fatalf("changed fields = %v, want status", result.ChangedFields)
	}

	updated, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	line := taskLineForID(string(updated), id)
	if !strings.HasPrefix(line, "- [ ] ") {
		t.Errorf("disk status not restored to TODO: %q", line)
	}
	if strings.Contains(line, "[completed::") {
		t.Errorf("undo left a completion stamp: %q", line)
	}
}

func TestUndoLastTaskMutation_ClearsDueDate(t *testing.T) {
	app := newTestApp(t)
	const id = "a11d0002-1111-1111-1111-111111111111"
	content := "- [ ] schedule me <!-- id: " + id + " -->\n"
	filePath := indexTestFile(t, app, "W", "S", "UndoDue", "2026-07-01", content)

	if err := app.SetTaskDueDate(id, "2026-08-01"); err != nil {
		t.Fatalf("SetTaskDueDate: %v", err)
	}
	result, err := app.UndoLastTaskMutation()
	if err != nil {
		t.Fatalf("UndoLastTaskMutation: %v", err)
	}
	if !result.Undone || !taskUndoContains(result.ChangedFields, "dueDate") {
		t.Fatalf("undo result = %+v, want dueDate restored", result)
	}

	updated, _ := os.ReadFile(filePath)
	line := taskLineForID(string(updated), id)
	if strings.Contains(line, "[due::") {
		t.Errorf("undo left the due token on disk: %q", line)
	}
}

// A mutation that changed only the due date must undo only the due date: an
// editor/external prose rewrite that landed after the mutation (not journaled)
// has to survive the undo instead of being reverted to the stale snapshot.
func TestUndoLastTaskMutation_DueDateUndoPreservesLaterTextEdit(t *testing.T) {
	app := newTestApp(t)
	const id = "a11d0016-1111-1111-1111-111111111111"
	content := "- [ ] original title <!-- id: " + id + " -->\n"
	filePath := indexTestFile(t, app, "W", "S", "UndoStaleText", "2026-07-01", content)

	if err := app.SetTaskDueDate(id, "2026-08-01"); err != nil {
		t.Fatalf("SetTaskDueDate: %v", err)
	}
	// Simulate a later non-journaled write (editor save / external app): the
	// prose changes but the due date the mutation set stays.
	writeFile(t, filePath, "- [ ] rewritten by editor [due:: 2026-08-01] <!-- id: "+id+" -->\n")

	result, err := app.UndoLastTaskMutation()
	if err != nil {
		t.Fatalf("UndoLastTaskMutation: %v", err)
	}
	if !result.Undone || !taskUndoContains(result.ChangedFields, "dueDate") {
		t.Fatalf("undo result = %+v, want dueDate restored", result)
	}
	restored, _ := os.ReadFile(filePath)
	line := taskLineForID(string(restored), id)
	if !strings.Contains(line, "rewritten by editor") {
		t.Errorf("undo reverted a later non-journaled text edit: %q", line)
	}
	if strings.Contains(line, "[due::") {
		t.Errorf("undo did not clear the journaled due token: %q", line)
	}
}

func TestUndoLastTaskMutation_OnlyTargetLineChanges(t *testing.T) {
	app := newTestApp(t)
	const (
		idA = "a11d0003-1111-1111-1111-111111111111"
		idB = "a11d0004-1111-1111-1111-111111111111"
		idC = "a11d0005-1111-1111-1111-111111111111"
	)
	content := "- [ ] first <!-- id: " + idA + " -->\n" +
		"- [ ] middle <!-- id: " + idB + " -->\n" +
		"- [ ] last <!-- id: " + idC + " -->\n"
	filePath := indexTestFile(t, app, "W", "S", "UndoAdjacency", "2026-07-01", content)

	if err := app.SetTaskDueDate(idB, "2026-09-15"); err != nil {
		t.Fatalf("SetTaskDueDate: %v", err)
	}
	// Compare the canonical post-setter file with the post-undo file: the
	// first mutation also lands the page frontmatter and block dates, which
	// are outside undo's scope.
	beforeUndo, _ := os.ReadFile(filePath)
	if _, err := app.UndoLastTaskMutation(); err != nil {
		t.Fatalf("UndoLastTaskMutation: %v", err)
	}

	updated, _ := os.ReadFile(filePath)
	assertOnlyTargetLineChanged(t, string(beforeUndo), string(updated), idB)
}

func TestUndoLastTaskMutation_RecurrenceRemovesSpawnedSibling(t *testing.T) {
	app := newTestApp(t)
	const id = "a11d0006-1111-1111-1111-111111111111"
	content := "# Today <!-- id: a11d0007-1111-1111-1111-111111111111 -->\n" +
		"\n" +
		"- [ ] Water plants [due:: 2026-07-01] [recur:: every week] <!-- id: " + id + " -->\n" +
		"- [ ] other task <!-- id: a11d0008-1111-1111-1111-111111111111 -->\n"
	filePath := indexTestFile(t, app, "W", "S", "UndoRecur", "2026-07-01", content)

	spawnedID, err := app.UpdateBlockState(id, "DONE")
	if err != nil {
		t.Fatalf("UpdateBlockState DONE: %v", err)
	}
	if spawnedID == "" {
		t.Fatal("expected a spawned recurrence instance")
	}
	afterDone, _ := os.ReadFile(filePath)
	if got := strings.Count(string(afterDone), "- ["); got != 3 {
		t.Fatalf("after DONE expected 3 task lines, got %d:\n%s", got, afterDone)
	}

	result, err := app.UndoLastTaskMutation()
	if err != nil {
		t.Fatalf("UndoLastTaskMutation: %v", err)
	}
	if !result.Undone {
		t.Fatalf("undo result = %+v, want undone", result)
	}
	if result.RemovedSpawnedID != spawnedID {
		t.Errorf("removed spawned id = %q, want %q", result.RemovedSpawnedID, spawnedID)
	}

	restored, _ := os.ReadFile(filePath)
	restoredStr := string(restored)
	if got := strings.Count(restoredStr, "- ["); got != 2 {
		t.Fatalf("undo must remove the spawned sibling; got %d task lines:\n%s", got, restoredStr)
	}
	if strings.Contains(restoredStr, "<!-- id: "+spawnedID) {
		t.Errorf("spawned block still on disk after undo:\n%s", restoredStr)
	}
	line := taskLineForID(restoredStr, id)
	if !strings.HasPrefix(line, "- [ ] ") || !strings.Contains(line, "[recur:: every week]") {
		t.Errorf("original task not restored to TODO with its recurrence rule: %q", line)
	}
	if _, err := app.db.GetBlockLocation(spawnedID); err == nil {
		t.Error("spawned block still indexed after undo")
	}
}

func TestUndoLastTaskMutation_RestoresBlockBodyText(t *testing.T) {
	app := newTestApp(t)
	const id = "a11d000e-1111-1111-1111-111111111111"
	content := "- original note <!-- id: " + id + " -->\n"
	filePath := indexTestFile(t, app, "W", "S", "UndoBody", "2026-07-01", content)

	if err := app.MutateBlock(id, "rewritten note"); err != nil {
		t.Fatalf("MutateBlock: %v", err)
	}
	result, err := app.UndoLastTaskMutation()
	if err != nil {
		t.Fatalf("UndoLastTaskMutation: %v", err)
	}
	if !result.Undone || !taskUndoContains(result.ChangedFields, "text") {
		t.Fatalf("undo result = %+v, want body text restored", result)
	}
	restored, _ := os.ReadFile(filePath)
	if line := taskLineForID(string(restored), id); !strings.Contains(line, "original note") {
		t.Errorf("block body not restored: %q", line)
	}
}

func TestUndoLastTaskMutation_BatchOrderRestoresAll(t *testing.T) {
	app := newTestApp(t)
	const (
		idA = "a11d0009-1111-1111-1111-111111111111"
		idB = "a11d000a-1111-1111-1111-111111111111"
	)
	content := "- [ ] first <!-- id: " + idA + " -->\n" +
		"- [ ] second <!-- id: " + idB + " -->\n"
	filePath := indexTestFile(t, app, "W", "S", "UndoOrders", "2026-07-01", content)

	if err := app.SetTaskOrders([]string{idA, idB}, []int{1, 2}); err != nil {
		t.Fatalf("SetTaskOrders: %v", err)
	}
	if got := taskUndoLen(app); got != 1 {
		t.Fatalf("batch write journaled %d entries, want one per file-write", got)
	}
	ordered, _ := os.ReadFile(filePath)
	if !strings.Contains(taskLineForID(string(ordered), idA), "[order:: 1]") ||
		!strings.Contains(taskLineForID(string(ordered), idB), "[order:: 2]") {
		t.Fatalf("order tokens not written:\n%s", ordered)
	}

	result, err := app.UndoLastTaskMutation()
	if err != nil {
		t.Fatalf("UndoLastTaskMutation: %v", err)
	}
	if !result.Undone || len(result.BlockIDs) != 2 {
		t.Fatalf("undo result = %+v, want both blocks restored", result)
	}
	restored, _ := os.ReadFile(filePath)
	for _, id := range []string{idA, idB} {
		if line := taskLineForID(string(restored), id); strings.Contains(line, "[order::") {
			t.Errorf("undo left an order token on %s: %q", id, line)
		}
	}
}

func TestUndoLastTaskMutation_FocusLockRefusalDoesNotJournal(t *testing.T) {
	app := newTestApp(t)
	withFocusLockWatcher(t, app)
	const id = "a11d000b-1111-1111-1111-111111111111"
	content := "- [ ] ship <!-- id: " + id + " -->\n"
	indexTestFile(t, app, "W", "S", "UndoFocus", "2026-07-01", content)

	if err := app.AcquireFocusLock("W", "S", "UndoFocus"); err != nil {
		t.Fatalf("AcquireFocusLock: %v", err)
	}
	if err := app.SetTaskOwner(id, "Alice"); !errors.Is(err, errBlockBeingEdited) {
		t.Fatalf("expected errBlockBeingEdited, got %v", err)
	}
	if got := taskUndoLen(app); got != 0 {
		t.Fatalf("refused write journaled %d entries, want 0", got)
	}

	result, err := app.UndoLastTaskMutation()
	if err != nil {
		t.Fatalf("undo with empty journal: %v", err)
	}
	if result.Undone || result.Skipped || result.Remaining != 0 {
		t.Fatalf("empty-journal undo result = %+v, want clean no-op", result)
	}
}

func TestUndoLastTaskMutation_EmptyJournalIsCleanNoOp(t *testing.T) {
	app := newTestApp(t)
	result, err := app.UndoLastTaskMutation()
	if err != nil {
		t.Fatalf("empty journal must not error: %v", err)
	}
	if result.Undone || result.Skipped || result.Remaining != 0 {
		t.Fatalf("result = %+v, want clean no-op", result)
	}
}

func TestTeardownClearsTaskUndoJournal(t *testing.T) {
	app := newTestApp(t)
	const id = "a11d000c-1111-1111-1111-111111111111"
	content := "- [ ] ship <!-- id: " + id + " -->\n"
	indexTestFile(t, app, "W", "S", "UndoTeardown", "2026-07-01", content)

	if err := app.SetTaskOwner(id, "Alice"); err != nil {
		t.Fatalf("SetTaskOwner: %v", err)
	}
	if got := taskUndoLen(app); got != 1 {
		t.Fatalf("journal = %d, want 1", got)
	}

	app.teardownVaultServices()

	if got := taskUndoLen(app); got != 0 {
		t.Fatalf("teardown left %d journal entries, want 0", got)
	}
	// Post-close undo is a clean refusal, not a replay into a torn-down vault.
	if _, err := app.UndoLastTaskMutation(); err == nil {
		t.Fatal("undo after vault close must refuse")
	}
}

func TestTaskUndoJournal_CapEvictsOldest(t *testing.T) {
	app := newTestApp(t)
	for i := 0; i < taskUndoJournalCap+5; i++ {
		app.taskUndo.record(taskUndoEntry{
			Blocks: []taskUndoBlock{{BlockID: strings.Repeat("x", i+1)}},
		})
	}
	app.taskUndo.mu.Lock()
	got := len(app.taskUndo.entries)
	oldest := ""
	if got > 0 {
		oldest = app.taskUndo.entries[0].Blocks[0].BlockID
	}
	app.taskUndo.mu.Unlock()

	if got != taskUndoJournalCap {
		t.Fatalf("journal length = %d, want cap %d", got, taskUndoJournalCap)
	}
	if oldest != strings.Repeat("x", 6) {
		t.Errorf("oldest surviving entry = %q, want the 6th recorded entry", oldest)
	}
}

func TestPluginUndoLastTaskMutation_GatedAndUndoes(t *testing.T) {
	app := newTestApp(t)
	const (
		pluginID = "third-party"
		id       = "a11d000d-1111-1111-1111-111111111111"
	)
	content := "- [ ] ship <!-- id: " + id + " -->\n"
	indexTestFile(t, app, "W", "S", "UndoPlugin", "2026-07-01", content)
	token := registerTestSession(t, app, pluginID)

	if _, err := app.PluginUndoLastTaskMutation(pluginID, token); err == nil {
		t.Fatal("undo must be denied without content-mutate")
	}
	if err := app.RequestCapability(pluginID, string(plugins.CapContentMutate), ""); err != nil {
		t.Fatalf("RequestCapability: %v", err)
	}

	if err := app.SetTaskOwner(id, "Alice"); err != nil {
		t.Fatalf("SetTaskOwner: %v", err)
	}
	if _, err := app.PluginUndoLastTaskMutation(pluginID, "wrong-token"); err == nil {
		t.Fatal("undo must reject a mismatched session token")
	}
	result, err := app.PluginUndoLastTaskMutation(pluginID, token)
	if err != nil {
		t.Fatalf("PluginUndoLastTaskMutation: %v", err)
	}
	if !result.Undone || !taskUndoContains(result.ChangedFields, "owner") {
		t.Fatalf("plugin undo result = %+v, want owner restored", result)
	}
}

func taskUndoContains(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}
