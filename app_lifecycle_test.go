package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"silt/backend/parser"
)

// TestUpdateBlockState_CompletedAtLifecycle covers the [completed::] token
// behaviour on DONE transitions (#417):
//   - TODO → DONE stamps [completed:: now]
//   - DONE → TODO (reopen) clears it
//   - TODO → DONE again (re-complete) overwrites with a fresh timestamp
//
// The token carries the time of the MOST RECENT DONE transition. The test
// uses Contains (not exact-match) for the timestamp since it depends on
// time.Now(), but asserts the token is present/absent as required.
func TestUpdateBlockState_CompletedAtLifecycle(t *testing.T) {
	app := newTestApp(t)
	const (
		notebook = "Work"
		section  = "Journal"
		page     = "Lifecycle"
		fileDate = "2026-07-06"
		taskID   = "beefbeef-1111-1111-1111-111111111111"
	)
	content := "- [ ] complete me [due:: 2026-07-06] <!-- id: " + taskID + " -->\n"
	filePath := indexTestFile(t, app, notebook, section, page, fileDate, content)

	// TODO → DONE: [completed:: now] must appear.
	if _, err := app.UpdateBlockState(taskID, "DONE"); err != nil {
		t.Fatalf("TODO→DONE: %v", err)
	}
	updated, _ := os.ReadFile(filePath)
	firstDone := string(updated)
	if !strings.Contains(firstDone, "[completed:: ") {
		t.Errorf("expected [completed:: ...] after DONE, got:\n%s", firstDone)
	}
	// Extract the first completed timestamp to compare against the re-complete.
	firstCompleted := extractToken(firstDone, "[completed:: ")

	// DONE → TODO (reopen): [completed::] must be cleared.
	if _, err := app.UpdateBlockState(taskID, "TODO"); err != nil {
		t.Fatalf("DONE→TODO: %v", err)
	}
	updated, _ = os.ReadFile(filePath)
	reopened := string(updated)
	if strings.Contains(reopened, "[completed::") {
		t.Errorf("expected [completed::] cleared after reopen, got:\n%s", reopened)
	}

	// TODO → DONE again: a fresh [completed::] overwrites with the new time.
	// Sleep briefly so the timestamp (second-granularity) is guaranteed to
	// differ when re-completing — otherwise the overwrite assertion is vacuous.
	time.Sleep(1100 * time.Millisecond)
	if _, err := app.UpdateBlockState(taskID, "DONE"); err != nil {
		t.Fatalf("re-DONE: %v", err)
	}
	updated, _ = os.ReadFile(filePath)
	reDone := string(updated)
	if !strings.Contains(reDone, "[completed:: ") {
		t.Errorf("expected [completed:: ...] after re-complete, got:\n%s", reDone)
	}
	reCompleted := extractToken(reDone, "[completed:: ")
	if reCompleted == "" {
		t.Fatalf("could not extract [completed::] from re-done content")
	}
	if reCompleted == firstCompleted {
		t.Errorf("expected re-complete to overwrite the timestamp, both were %q", firstCompleted)
	}
}

// extractToken returns the value inside the first [key:: value] occurrence of
// the given prefix (e.g. "[completed:: "), including the closing bracket.
func extractToken(s, prefix string) string {
	idx := strings.Index(s, prefix)
	if idx < 0 {
		return ""
	}
	rest := s[idx:]
	end := strings.Index(rest, "]")
	if end < 0 {
		return ""
	}
	return rest[:end+1]
}

// TestUpdateBlockState_RecurrenceSpawnHasCreatedAt verifies the spawned
// recurrence instance carries [created::] and [order::] (#417): the spawn is
// a genuinely-new task, so it gets the lifecycle tokens minted by
// buildNextRecurrence. The completed original gets [completed::].
func TestUpdateBlockState_RecurrenceSpawnHasCreatedAt(t *testing.T) {
	app := newTestApp(t)
	const taskID = "feedfeed-1111-1111-1111-111111111111"
	content := "- [ ] water plants [due:: 2026-07-01] [recur:: every week] <!-- id: " + taskID + " -->\n"
	filePath := indexTestFile(t, app, "W", "S", "RecurSpawn", "2026-07-01", content)

	if _, err := app.UpdateBlockState(taskID, "DONE"); err != nil {
		t.Fatalf("UpdateBlockState DONE: %v", err)
	}
	updated, _ := os.ReadFile(filePath)
	s := string(updated)

	// Completed original: [completed::] stamped, [recur::] stripped.
	if !strings.Contains(s, "[completed:: ") {
		t.Errorf("completed original missing [completed:: ...]:\n%s", s)
	}

	// Spawned instance: [created::] and [order::] minted.
	lines := strings.Split(s, "\n")
	var spawned string
	for _, ln := range lines {
		if strings.Contains(ln, "- [ ] water plants") && strings.Contains(ln, "[recur:: every week]") {
			spawned = ln
			break
		}
	}
	if spawned == "" {
		t.Fatalf("spawned instance not found in:\n%s", s)
	}
	if !strings.Contains(spawned, "[created:: ") {
		t.Errorf("spawned instance missing [created:: ...]:\n%s", spawned)
	}
	if !strings.Contains(spawned, "[order:: ") {
		t.Errorf("spawned instance missing [order:: ...]:\n%s", spawned)
	}

	// The index must carry the lifecycle values for the spawned task.
	tasks, err := app.db.QueryTasksWithFilters(parser.TaskQueryFilter{})
	if err != nil {
		t.Fatalf("QueryTasks: %v", err)
	}
	var foundCreated, foundOrder, foundCompleted bool
	for _, tk := range tasks {
		if strings.Contains(tk.CleanContent, "water plants") {
			if tk.Status == "TODO" && tk.CreatedAt != "" && tk.ManualOrder > 0 {
				foundCreated = true
			}
			if tk.Status == "DONE" && tk.CompletedAt != "" {
				foundCompleted = true
			}
		}
		if tk.ManualOrder > 0 {
			foundOrder = true
		}
	}
	if !foundCreated {
		t.Errorf("expected spawned TODO to have CreatedAt + ManualOrder in index")
	}
	if !foundCompleted {
		t.Errorf("expected completed original to have CompletedAt in index")
	}
	if !foundOrder {
		t.Errorf("expected at least one task with ManualOrder in index")
	}
}

// TestCreateStandaloneTask_MintsCreatedAtAndOrder verifies the Go SDK create
// path (CreateStandaloneTask) mints [created::] and [order::] on the new
// block, since it mints the id itself and bypasses the parser's minting
// branch on re-parse (#417).
func TestCreateStandaloneTask_MintsCreatedAtAndOrder(t *testing.T) {
	app := newTestApp(t)
	resetStandaloneTasks(t, app)

	id, err := app.CreateStandaloneTask("quick add task", "2026-07-15", "TODO")
	if err != nil {
		t.Fatalf("CreateStandaloneTask: %v", err)
	}

	tasksFile := filepath.Join(app.vaultPath, ".silt", "tasks.md")
	content, err := os.ReadFile(tasksFile)
	if err != nil {
		t.Fatalf("read tasks file: %v", err)
	}
	s := string(content)
	if !strings.Contains(s, "[created:: ") {
		t.Errorf("expected [created:: ...] in file, got:\n%s", s)
	}
	if !strings.Contains(s, "[order:: 1]") {
		t.Errorf("expected [order:: 1] for first task, got:\n%s", s)
	}
	if !strings.Contains(s, id) {
		t.Errorf("expected block id %s in file", id)
	}

	// Index must carry the lifecycle values.
	var createdAt string
	var manualOrder interface{}
	if err := app.db.SQLDB().QueryRow(
		"SELECT created_at, manual_order FROM tasks WHERE block_id = ?", id,
	).Scan(&createdAt, &manualOrder); err != nil {
		t.Fatalf("query lifecycle: %v", err)
	}
	if createdAt == "" {
		t.Errorf("expected non-empty created_at in index")
	}
	if manualOrder == nil {
		t.Errorf("expected non-null manual_order in index")
	}

	// A second task gets ManualOrder=2.
	id2, err := app.CreateStandaloneTask("second task", "", "TODO")
	if err != nil {
		t.Fatalf("second CreateStandaloneTask: %v", err)
	}
	if id2 == "" || id2 == id {
		t.Fatalf("expected a distinct non-empty id for the second task, got %q", id2)
	}
	content2, _ := os.ReadFile(tasksFile)
	if !strings.Contains(string(content2), "[order:: 2]") {
		t.Errorf("expected [order:: 2] for second task, got:\n%s", content2)
	}
}

// TestCreateStandaloneTask_DoneStampsCompleted is the F1 asymmetry fix: a
// standalone task created with status "DONE" must mint [completed::] (and
// [created::]/[order::]) just like the inline-editor path where - [x] typed
// fresh gets stamped (TestMinting_CreatedAlreadyDoneStampsCompleted).
// CreateStandaloneTask bypasses parser minting (it assigns the id itself), so
// CompletedAt must be stamped explicitly.
func TestCreateStandaloneTask_DoneStampsCompleted(t *testing.T) {
	app := newTestApp(t)
	resetStandaloneTasks(t, app)

	id, err := app.CreateStandaloneTask("already done task", "", "DONE")
	if err != nil {
		t.Fatalf("CreateStandaloneTask DONE: %v", err)
	}

	tasksFile := filepath.Join(app.vaultPath, ".silt", "tasks.md")
	content, err := os.ReadFile(tasksFile)
	if err != nil {
		t.Fatalf("read tasks file: %v", err)
	}
	s := string(content)
	for _, want := range []string{"[created:: ", "[completed:: ", "[order:: 1]"} {
		if !strings.Contains(s, want) {
			t.Errorf("expected %q in file, got:\n%s", want, s)
		}
	}

	// Index must carry both lifecycle timestamps.
	var createdAt, completedAt string
	var manualOrder interface{}
	if err := app.db.SQLDB().QueryRow(
		"SELECT created_at, completed_at, manual_order FROM tasks WHERE block_id = ?", id,
	).Scan(&createdAt, &completedAt, &manualOrder); err != nil {
		t.Fatalf("query lifecycle: %v", err)
	}
	if createdAt == "" {
		t.Errorf("expected non-empty created_at in index")
	}
	if completedAt == "" {
		t.Errorf("expected non-empty completed_at in index for a DONE task")
	}
	if manualOrder == nil {
		t.Errorf("expected non-null manual_order in index")
	}
}
