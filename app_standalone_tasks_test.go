package main

import (
	"database/sql"
	"os"
	"path/filepath"
	"silt/backend/parser"
	"strings"
	"testing"
)

// resetStandaloneTasks wipes any standalone-task rows left in the shared
// in-memory test DB by earlier tests. newTestApp uses
// db.NewDatabaseManager("") → "file::memory:?cache=shared", so every test in
// package main shares ONE database. Production is unaffected (each vault has
// its own on-disk DB), but tests must isolate the ".silt/tasks" page so block
// counts are deterministic. The on-disk file is already per-test (unique
// t.TempDir vault), so only the shared DB rows need clearing.
func resetStandaloneTasks(t *testing.T, app *App) {
	t.Helper()
	if _, err := app.db.SQLDB().Exec(
		"DELETE FROM blocks WHERE notebook = ?", ".silt",
	); err != nil {
		t.Fatalf("reset standalone tasks: %v", err)
	}
}

// TestPluginCreateTask_CreatesFileAndIndexesBlock verifies PluginCreateTask
// creates the standalone-tasks file when absent, writes a queryable block, and
// returns the new id (#368).
func TestPluginCreateTask_CreatesFileAndIndexesBlock(t *testing.T) {
	app := newTestApp(t)
	resetStandaloneTasks(t, app)
	tok := registerTestSession(t, app, "silt-tasks")

	// File does not exist yet.
	tasksFile := filepath.Join(app.vaultPath, ".silt", "tasks.md")
	if _, err := os.Stat(tasksFile); !os.IsNotExist(err) {
		t.Fatalf("tasks file should not exist before first create: %v", err)
	}

	id, err := app.PluginCreateTask("silt-tasks", tok, "Write the brief", "2026-07-15", "TODO")
	if err != nil || id == "" {
		t.Fatalf("PluginCreateTask: id=%q err=%v", id, err)
	}

	// File was created.
	if _, err := os.Stat(tasksFile); err != nil {
		t.Fatalf("tasks file should exist after create: %v", err)
	}

	// Block is queryable in the index.
	var status, due string
	err = app.db.SQLDB().QueryRow(
		"SELECT t.status, t.due_date FROM tasks t WHERE t.block_id = ?", id,
	).Scan(&status, &due)
	if err != nil {
		t.Fatalf("task row not found in index: %v", err)
	}
	if status != "TODO" {
		t.Errorf("expected status TODO, got %q", status)
	}
	if due != "2026-07-15" {
		t.Errorf("expected due 2026-07-15, got %q", due)
	}

	// Block is a TASK block under the synthetic notebook.
	var notebook, page, btype string
	err = app.db.SQLDB().QueryRow(
		"SELECT notebook, page, type FROM blocks WHERE id = ?", id,
	).Scan(&notebook, &page, &btype)
	if err != nil {
		t.Fatalf("block row not found: %v", err)
	}
	if notebook != ".silt" || page != "tasks" || btype != "TASK" {
		t.Errorf("expected notebook=.silt page=tasks type=TASK, got notebook=%q page=%q type=%q", notebook, page, btype)
	}
}

// TestPluginCreateTask_AppendsMultipleTasks verifies a second create appends
// rather than overwriting.
func TestPluginCreateTask_AppendsMultipleTasks(t *testing.T) {
	app := newTestApp(t)
	resetStandaloneTasks(t, app)
	tok := registerTestSession(t, app, "silt-tasks")

	id1, err := app.PluginCreateTask("silt-tasks", tok, "First task", "", "")
	if err != nil {
		t.Fatalf("create #1: %v", err)
	}
	id2, err := app.PluginCreateTask("silt-tasks", tok, "Second task", "", "")
	if err != nil {
		t.Fatalf("create #2: %v", err)
	}
	if id1 == id2 {
		t.Fatalf("expected distinct ids, got %q twice", id1)
	}

	var n int
	if err := app.db.SQLDB().QueryRow(
		"SELECT COUNT(*) FROM blocks WHERE notebook = ? AND page = ?", ".silt", "tasks",
	).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 2 {
		t.Errorf("expected 2 standalone task blocks, got %d", n)
	}
}

// TestPluginCreateTask_DefaultsAndValidation verifies title is required, status
// defaults to TODO, and an invalid status is rejected.
func TestPluginCreateTask_DefaultsAndValidation(t *testing.T) {
	app := newTestApp(t)
	resetStandaloneTasks(t, app)
	tok := registerTestSession(t, app, "silt-tasks")

	if _, err := app.PluginCreateTask("silt-tasks", tok, "   ", "", ""); err == nil {
		t.Fatal("expected error for empty title")
	}
	if _, err := app.PluginCreateTask("silt-tasks", tok, "ok", "", "BOGUS"); err == nil {
		t.Fatal("expected error for invalid status")
	}
	id, err := app.PluginCreateTask("silt-tasks", tok, "default status", "", "")
	if err != nil {
		t.Fatalf("create with empty status: %v", err)
	}
	var status string
	_ = app.db.SQLDB().QueryRow("SELECT status FROM tasks WHERE block_id = ?", id).Scan(&status)
	if status != "TODO" {
		t.Errorf("expected default status TODO, got %q", status)
	}
}

// TestPluginCreateTask_FileContentHasGFMSyntax verifies the on-disk file holds
// a real GFM checkbox + due token, so deleting the index and re-indexing
// restores the task (markdown-source-of-truth invariant).
func TestPluginCreateTask_FileContentHasGFMSyntax(t *testing.T) {
	app := newTestApp(t)
	resetStandaloneTasks(t, app)
	tok := registerTestSession(t, app, "silt-tasks")

	id, err := app.PluginCreateTask("silt-tasks", tok, "Ship the release", "2026-08-01", "DOING")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	tasksFile := filepath.Join(app.vaultPath, ".silt", "tasks.md")
	content, err := os.ReadFile(tasksFile)
	if err != nil {
		t.Fatalf("read tasks file: %v", err)
	}
	s := string(content)
	if !strings.Contains(s, "- [/] Ship the release") {
		t.Errorf("expected GFM DOING checkbox, file content:\n%s", s)
	}
	if !strings.Contains(s, "[due:: 2026-08-01]") {
		t.Errorf("expected [due:: 2026-08-01] token, file content:\n%s", s)
	}
	if !strings.Contains(s, id) {
		t.Errorf("expected block id %s in file, content:\n%s", id, s)
	}
}

// TestScanStandaloneTasks_IndexesExistingFile verifies the targeted scan picks
// up the standalone-tasks file (which WalkMarkdown skips as a dot-dir) and
// indexes it under the synthetic notebook. Simulates a cold-start re-index.
func TestScanStandaloneTasks_IndexesExistingFile(t *testing.T) {
	app := newTestApp(t)
	resetStandaloneTasks(t, app)
	tok := registerTestSession(t, app, "silt-tasks")

	id, err := app.PluginCreateTask("silt-tasks", tok, "Survives reindex", "", "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Simulate a cold-start scan: wipe the index rows for the standalone file
	// and re-scan via ScanStandaloneTasks.
	if _, err := app.db.SQLDB().Exec(
		"DELETE FROM blocks WHERE notebook = ? AND page = ?", ".silt", "tasks",
	); err != nil {
		t.Fatalf("delete standalone rows: %v", err)
	}
	results := parser.ScanStandaloneTasks(app.vaultPath, app.spacesPerTab)
	if len(results) != 1 {
		t.Fatalf("expected 1 scan result, got %d", len(results))
	}
	res := results[0]
	if res.Notebook != ".silt" || res.Page != "tasks" {
		t.Errorf("expected notebook=.silt page=tasks, got notebook=%q page=%q", res.Notebook, res.Page)
	}
	if len(res.Blocks) != 1 || res.Blocks[0].ID != id {
		t.Errorf("expected the created block to survive re-scan, got %d blocks", len(res.Blocks))
	}
}

// TestScanStandaloneTasks_AbsentFileReturnsNil verifies the scan returns nil
// (no error) when no standalone tasks have been created yet.
func TestScanStandaloneTasks_AbsentFileReturnsNil(t *testing.T) {
	app := newTestApp(t)
	results := parser.ScanStandaloneTasks(app.vaultPath, app.spacesPerTab)
	if results != nil {
		t.Errorf("expected nil results for absent file, got %d", len(results))
	}
}

// TestPluginSetTaskDueDate_SetsReplacesClears verifies set/replace/clear of the
// [due::] token for a note-embedded task (#293).
func TestPluginSetTaskDueDate_SetsReplacesClears(t *testing.T) {
	app := newTestApp(t)
	tok := registerTestSession(t, app, "silt-tasks")
	taskID := "66666666-6666-6666-6666-666666666666"
	writeSamplePage(t, app, "Work", "Journal", "Daily", "2026-06-13", taskID, "reschedule me")

	set := func(date string) {
		t.Helper()
		ok, err := app.PluginSetTaskDueDate("silt-tasks", tok, taskID, date)
		if err != nil || !ok {
			t.Fatalf("PluginSetTaskDueDate(%q): ok=%v err=%v", date, ok, err)
		}
	}
	assertDue := func(want string) {
		t.Helper()
		var got sql.NullString
		if err := app.db.SQLDB().QueryRow(
			"SELECT due_date FROM tasks WHERE block_id = ?", taskID,
		).Scan(&got); err != nil {
			t.Fatalf("query due: %v", err)
		}
		if want == "" {
			if got.Valid && got.String != "" {
				t.Errorf("expected cleared due (NULL), got %q", got.String)
			}
		} else if !got.Valid || got.String != want {
			t.Errorf("expected due %q, got %+v", want, got)
		}
	}

	set("2026-07-20")
	assertDue("2026-07-20")
	set("2026-07-25") // replace
	assertDue("2026-07-25")
	set("") // clear
	assertDue("")
}

// TestPluginSetTaskDueDate_RejectsInvalidDate verifies a malformed date never
// reaches disk.
func TestPluginSetTaskDueDate_RejectsInvalidDate(t *testing.T) {
	app := newTestApp(t)
	tok := registerTestSession(t, app, "silt-tasks")
	taskID := "77777777-7777-7777-7777-777777777777"
	writeSamplePage(t, app, "Work", "Journal", "Daily", "2026-06-13", taskID, "guard me")

	if _, err := app.PluginSetTaskDueDate("silt-tasks", tok, taskID, "not-a-date"); err == nil {
		t.Fatal("expected error for malformed date")
	}
	// A near-miss format must also be rejected.
	if _, err := app.PluginSetTaskDueDate("silt-tasks", tok, taskID, "2026/07/20"); err == nil {
		t.Fatal("expected error for slash-separated date")
	}
}

// TestPluginSetTaskDueDate_RejectsNonTask verifies a non-task block cannot get
// a due date.
func TestPluginSetTaskDueDate_RejectsNonTask(t *testing.T) {
	app := newTestApp(t)
	tok := registerTestSession(t, app, "silt-tasks")
	// writeSamplePage's "# Title" header carries id 11111111...
	if _, err := app.PluginSetTaskDueDate("silt-tasks", tok, "11111111-1111-1111-1111-111111111111", "2026-07-20"); err == nil {
		t.Fatal("expected error for non-task block")
	}
}

// TestPluginSetTaskDueDate_StandaloneTask verifies the synthetic .silt
// notebook resolves correctly through the due-date mutation path — the path
// calendar drag-and-drop uses for a standalone task created from a day cell
// (#368 + #293). A regression in resolveNotebookDir(".silt", "vault") would
// otherwise silently break rescheduling every standalone task.
func TestPluginSetTaskDueDate_StandaloneTask(t *testing.T) {
	app := newTestApp(t)
	resetStandaloneTasks(t, app)
	tok := registerTestSession(t, app, "silt-tasks")

	id, err := app.PluginCreateTask("silt-tasks", tok, "Drag-drop me", "2026-07-10", "TODO")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	ok, err := app.PluginSetTaskDueDate("silt-tasks", tok, id, "2026-08-01")
	if err != nil || !ok {
		t.Fatalf("PluginSetTaskDueDate on standalone task: ok=%v err=%v", ok, err)
	}

	// The on-disk file's [due::] token changed (markdown-source-of-truth).
	tasksFile := filepath.Join(app.vaultPath, ".silt", "tasks.md")
	content, err := os.ReadFile(tasksFile)
	if err != nil {
		t.Fatalf("read tasks file: %v", err)
	}
	if !strings.Contains(string(content), "[due:: 2026-08-01]") {
		t.Errorf("expected [due:: 2026-08-01] in file, got:\n%s", content)
	}
	if strings.Contains(string(content), "[due:: 2026-07-10]") {
		t.Errorf("stale [due:: 2026-07-10] still in file:\n%s", content)
	}

	// The index row reflects the new due date.
	var got sql.NullString
	if err := app.db.SQLDB().QueryRow(
		"SELECT due_date FROM tasks WHERE block_id = ?", id,
	).Scan(&got); err != nil {
		t.Fatalf("query: %v", err)
	}
	if !got.Valid || got.String != "2026-08-01" {
		t.Errorf("expected indexed due 2026-08-01, got %+v", got)
	}
}

// TestListNavigation_ExcludesStandaloneTasks verifies the .silt synthetic
// notebook never appears in the page browser (#368 AC).
func TestListNavigation_ExcludesStandaloneTasks(t *testing.T) {
	app := newTestApp(t)
	resetStandaloneTasks(t, app)
	tok := registerTestSession(t, app, "silt-tasks")
	if _, err := app.PluginCreateTask("silt-tasks", tok, "hidden from nav", "", ""); err != nil {
		t.Fatalf("create: %v", err)
	}
	tree, err := app.ListNavigation()
	if err != nil {
		t.Fatalf("ListNavigation: %v", err)
	}
	for _, nb := range tree.Notebooks {
		if nb.Name == ".silt" {
			t.Errorf("standalone notebook .silt must not appear in navigation; got notebooks: %+v", tree.Notebooks)
		}
	}
}
