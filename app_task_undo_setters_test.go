package main

import (
	"os"
	"strings"
	"testing"
)

func TestUndoLastTaskMutation_RestoresRecurrenceSet(t *testing.T) {
	app := newTestApp(t)
	const id = "a11d0010-1111-1111-1111-111111111111"
	content := "- [ ] Water plants [due:: 2026-07-01] <!-- id: " + id + " -->\n"
	filePath := indexTestFile(t, app, "W", "S", "UndoRecurSet", "2026-07-01", content)

	if err := app.SetTaskRecurrence(id, "every week"); err != nil {
		t.Fatalf("SetTaskRecurrence: %v", err)
	}
	if got := taskUndoLen(app); got != 1 {
		t.Fatalf("recurrence write journaled %d entries, want 1", got)
	}
	result, err := app.UndoLastTaskMutation()
	if err != nil {
		t.Fatalf("UndoLastTaskMutation: %v", err)
	}
	if !result.Undone || !taskUndoContains(result.ChangedFields, "recurrence") {
		t.Fatalf("undo result = %+v, want recurrence restored", result)
	}
	restored, _ := os.ReadFile(filePath)
	if line := taskLineForID(string(restored), id); strings.Contains(line, "[recur::") {
		t.Errorf("undo left the recurrence token on disk: %q", line)
	}
}

func TestUndoLastTaskMutation_RestoresRecurrenceClear(t *testing.T) {
	app := newTestApp(t)
	const id = "a11d0011-1111-1111-1111-111111111111"
	content := "- [ ] Water plants [due:: 2026-07-01] [recur:: every week] <!-- id: " + id + " -->\n"
	filePath := indexTestFile(t, app, "W", "S", "UndoRecurClear", "2026-07-01", content)

	if err := app.SetTaskRecurrence(id, ""); err != nil {
		t.Fatalf("SetTaskRecurrence clear: %v", err)
	}
	result, err := app.UndoLastTaskMutation()
	if err != nil {
		t.Fatalf("UndoLastTaskMutation: %v", err)
	}
	if !result.Undone || !taskUndoContains(result.ChangedFields, "recurrence") {
		t.Fatalf("undo result = %+v, want recurrence restored", result)
	}
	restored, _ := os.ReadFile(filePath)
	if line := taskLineForID(string(restored), id); !strings.Contains(line, "[recur:: every week]") {
		t.Errorf("undo did not restore the cleared recurrence rule: %q", line)
	}
}

func TestUndoLastTaskMutation_RestoresDependencyAdd(t *testing.T) {
	app := newTestApp(t)
	const (
		idA = "a11d0012-1111-1111-1111-111111111111"
		idB = "a11d0013-1111-1111-1111-111111111111"
	)
	content := "- [ ] dependent <!-- id: " + idA + " -->\n" +
		"- [ ] prerequisite <!-- id: " + idB + " -->\n"
	filePath := indexTestFile(t, app, "W", "S", "UndoDepAdd", "2026-07-01", content)

	if err := app.SetTaskBlockedBy(idA, []string{idB}); err != nil {
		t.Fatalf("SetTaskBlockedBy: %v", err)
	}
	if got := taskUndoLen(app); got != 1 {
		t.Fatalf("dependency write journaled %d entries, want 1", got)
	}
	result, err := app.UndoLastTaskMutation()
	if err != nil {
		t.Fatalf("UndoLastTaskMutation: %v", err)
	}
	if !result.Undone || !taskUndoContains(result.ChangedFields, "blockedBy") {
		t.Fatalf("undo result = %+v, want blockedBy restored", result)
	}
	restored, _ := os.ReadFile(filePath)
	if line := taskLineForID(string(restored), idA); strings.Contains(line, "[blocked_by::") {
		t.Errorf("undo left the dependency token on disk: %q", line)
	}
}

func TestUndoLastTaskMutation_RestoresDependencyRemove(t *testing.T) {
	app := newTestApp(t)
	const (
		idA = "a11d0014-1111-1111-1111-111111111111"
		idB = "a11d0015-1111-1111-1111-111111111111"
	)
	content := "- [ ] dependent [blocked_by:: ((" + idB + "))] <!-- id: " + idA + " -->\n" +
		"- [ ] prerequisite <!-- id: " + idB + " -->\n"
	filePath := indexTestFile(t, app, "W", "S", "UndoDepRemove", "2026-07-01", content)

	if err := app.SetTaskBlockedBy(idA, nil); err != nil {
		t.Fatalf("SetTaskBlockedBy clear: %v", err)
	}
	result, err := app.UndoLastTaskMutation()
	if err != nil {
		t.Fatalf("UndoLastTaskMutation: %v", err)
	}
	if !result.Undone || !taskUndoContains(result.ChangedFields, "blockedBy") {
		t.Fatalf("undo result = %+v, want blockedBy restored", result)
	}
	restored, _ := os.ReadFile(filePath)
	if line := taskLineForID(string(restored), idA); !strings.Contains(line, "(( "+idB+" ))") &&
		!strings.Contains(line, "(("+idB+"))") {
		t.Errorf("undo did not restore the cleared dependency: %q", line)
	}
}
