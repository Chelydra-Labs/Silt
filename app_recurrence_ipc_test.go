package main

import (
	"os"
	"strings"
	"testing"

	"silt/backend/parser"
)

// TestSetTaskRecurrence_SetChangeClear covers the full lifecycle of setting,
// changing, and clearing a recurrence rule via the IPC layer.
func TestSetTaskRecurrence_SetChangeClear(t *testing.T) {
	app := newTestApp(t)
	const taskID = "a1b2c3d4-1111-1111-1111-111111111111"
	content := "- [ ] task [due:: 2026-07-15] <!-- id: " + taskID + " -->\n"
	filePath := indexTestFile(t, app, "W", "S", "SetRecur", "2026-07-01", content)

	// Set recurrence.
	if err := app.SetTaskRecurrence(taskID, "every week"); err != nil {
		t.Fatalf("SetTaskRecurrence (set): %v", err)
	}
	updated, _ := os.ReadFile(filePath)
	if !strings.Contains(string(updated), "[recur:: every week]") {
		t.Errorf("set: recurrence token missing in:\n%s", updated)
	}

	// Change recurrence.
	if err := app.SetTaskRecurrence(taskID, "every month"); err != nil {
		t.Fatalf("SetTaskRecurrence (change): %v", err)
	}
	updated, _ = os.ReadFile(filePath)
	if !strings.Contains(string(updated), "[recur:: every month]") {
		t.Errorf("change: new recurrence token missing in:\n%s", updated)
	}
	if strings.Contains(string(updated), "every week") {
		t.Errorf("change: old recurrence token still present in:\n%s", updated)
	}

	// Clear recurrence (stop recurring).
	if err := app.SetTaskRecurrence(taskID, ""); err != nil {
		t.Fatalf("SetTaskRecurrence (clear): %v", err)
	}
	updated, _ = os.ReadFile(filePath)
	if strings.Contains(string(updated), "[recur::") {
		t.Errorf("clear: recurrence token should be gone in:\n%s", updated)
	}
}

// TestSetTaskRecurrence_RejectsMalformed verifies invalid grammar is rejected
// before any disk write.
func TestSetTaskRecurrence_RejectsMalformed(t *testing.T) {
	app := newTestApp(t)
	const taskID = "b2c3d4e5-1111-1111-1111-111111111111"
	content := "- [ ] task [due:: 2026-07-15] <!-- id: " + taskID + " -->\n"
	filePath := indexTestFile(t, app, "W", "S", "BadRecur", "2026-07-01", content)

	err := app.SetTaskRecurrence(taskID, "fortnightly-ish")
	if err == nil {
		t.Fatal("expected error for malformed recurrence, got nil")
	}
	updated, _ := os.ReadFile(filePath)
	if strings.Contains(string(updated), "fortnightly") {
		t.Errorf("malformed rule must not reach disk in:\n%s", updated)
	}
}

// TestSetTaskRecurrence_RejectsWithoutDueDate verifies that setting a
// recurrence on a task with no [due::] token is rejected (PLAN.md §1
// validation: recurrence without an anchor is meaningless).
func TestSetTaskRecurrence_RejectsWithoutDueDate(t *testing.T) {
	app := newTestApp(t)
	const taskID = "c3d4e5f6-1111-1111-1111-111111111111"
	content := "- [ ] undated task <!-- id: " + taskID + " -->\n"
	filePath := indexTestFile(t, app, "W", "S", "NoDue", "2026-07-01", content)

	err := app.SetTaskRecurrence(taskID, "every week")
	if err == nil {
		t.Fatal("expected error for recurrence without due date, got nil")
	}
	updated, _ := os.ReadFile(filePath)
	if strings.Contains(string(updated), "[recur::") {
		t.Errorf("recurrence must not reach disk when due date is absent in:\n%s", updated)
	}
}

// TestSetTaskRecurrence_IndexReflectsChange verifies the SQLite index is
// updated so QueryTasksWithFilters surfaces the new recurrence value.
func TestSetTaskRecurrence_IndexReflectsChange(t *testing.T) {
	app := newTestApp(t)
	const (
		taskID = "d4e5f6a7-1111-1111-1111-111111111111"
		marker = "index-uniq-9f3a"
	)
	content := "- [ ] " + marker + " [due:: 2026-07-15] <!-- id: " + taskID + " -->\n"
	indexTestFile(t, app, "W", "S", "IdxRecur", "2026-07-01", content)

	if err := app.SetTaskRecurrence(taskID, "every 2 weeks"); err != nil {
		t.Fatalf("SetTaskRecurrence: %v", err)
	}

	tasks, err := app.db.QueryTasksWithFilters(parser.TaskQueryFilter{})
	if err != nil {
		t.Fatalf("QueryTasks: %v", err)
	}
	var found bool
	for _, tk := range tasks {
		if tk.ID == taskID && tk.Recurrence == "every 2 weeks" {
			found = true
		}
	}
	if !found {
		t.Errorf("index does not reflect recurrence='every 2 weeks' for block %s", taskID)
	}
}

// TestSetTaskRecurrence_RejectsNonTask verifies the method refuses blocks
// that are not tasks (e.g. NOTE blocks).
func TestSetTaskRecurrence_RejectsNonTask(t *testing.T) {
	app := newTestApp(t)
	const noteID = "e5f6a7b8-1111-1111-1111-111111111111"
	content := "- note block <!-- id: " + noteID + " -->\n"
	indexTestFile(t, app, "W", "S", "NoteBlock", "2026-07-01", content)

	err := app.SetTaskRecurrence(noteID, "every week")
	if err == nil {
		t.Fatal("expected error for non-task block, got nil")
	}
}
