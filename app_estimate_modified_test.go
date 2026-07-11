package main

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestPluginSetTaskEstimate_SetsClearsAndStampsModified verifies estimate
// set/clear plus the [modified::] stamp from mutateTaskBlock (#439/#440).
func TestPluginSetTaskEstimate_SetsClearsAndStampsModified(t *testing.T) {
	app := newTestApp(t)
	tok := registerTestSession(t, app, "silt-tasks")
	taskID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	writeSamplePage(t, app, "Work", "Journal", "Daily", "2026-06-13", taskID, "estimate me")

	before := time.Now().Add(-2 * time.Second)

	ok, err := app.PluginSetTaskEstimate("silt-tasks", tok, taskID, "2h")
	if err != nil || !ok {
		t.Fatalf("PluginSetTaskEstimate(2h): ok=%v err=%v", ok, err)
	}

	var estMins sql.NullInt64
	var modified sql.NullString
	if err := app.db.SQLDB().QueryRow(
		"SELECT estimate_minutes, modified_at FROM tasks WHERE block_id = ?", taskID,
	).Scan(&estMins, &modified); err != nil {
		t.Fatalf("query: %v", err)
	}
	if !estMins.Valid || estMins.Int64 != 120 {
		t.Errorf("estimate_minutes=%v want 120", estMins)
	}
	if !modified.Valid || modified.String == "" {
		t.Fatal("expected modified_at stamped after estimate set")
	}
	modTime, err := time.ParseInLocation("2006-01-02T15:04:05", modified.String, time.Local)
	if err != nil {
		t.Fatalf("parse modified_at %q: %v", modified.String, err)
	}
	if modTime.Before(before) {
		t.Errorf("modified_at %v is before write started %v", modTime, before)
	}

	// On-disk token present.
	pagePath := filepath.Join(app.vaultPath, "Work", "Journal", "Daily.md")
	content, err := os.ReadFile(pagePath)
	if err != nil {
		t.Fatalf("read page: %v", err)
	}
	if !strings.Contains(string(content), "[estimate:: 2h]") {
		t.Errorf("file missing [estimate:: 2h]:\n%s", content)
	}
	if !strings.Contains(string(content), "[modified::") {
		t.Errorf("file missing [modified::]:\n%s", content)
	}

	// Clear estimate.
	ok, err = app.PluginSetTaskEstimate("silt-tasks", tok, taskID, "")
	if err != nil || !ok {
		t.Fatalf("PluginSetTaskEstimate(clear): ok=%v err=%v", ok, err)
	}
	if err := app.db.SQLDB().QueryRow(
		"SELECT estimate_minutes FROM tasks WHERE block_id = ?", taskID,
	).Scan(&estMins); err != nil {
		t.Fatalf("query after clear: %v", err)
	}
	if estMins.Valid {
		t.Errorf("expected NULL estimate_minutes after clear, got %d", estMins.Int64)
	}
	content, _ = os.ReadFile(pagePath)
	if strings.Contains(string(content), "[estimate::") {
		t.Errorf("file still has estimate after clear:\n%s", content)
	}
}

// TestPluginSetTaskEstimate_RejectsInvalid verifies malformed durations never
// reach disk.
func TestPluginSetTaskEstimate_RejectsInvalid(t *testing.T) {
	app := newTestApp(t)
	tok := registerTestSession(t, app, "silt-tasks")
	taskID := "bbbbbbbb-cccc-dddd-eeee-ffffffffffff"
	writeSamplePage(t, app, "Work", "Journal", "Daily", "2026-06-13", taskID, "guard estimate")

	if _, err := app.PluginSetTaskEstimate("silt-tasks", tok, taskID, "not-a-duration"); err == nil {
		t.Fatal("expected error for invalid estimate")
	}
	if _, err := app.PluginSetTaskEstimate("silt-tasks", tok, taskID, "30"); err == nil {
		t.Fatal("expected error for bare number")
	}
}

// TestUpdateBlockState_StampsModified verifies status changes stamp [modified::].
func TestUpdateBlockState_StampsModified(t *testing.T) {
	app := newTestApp(t)
	taskID := "cccccccc-dddd-eeee-ffff-000000000000"
	writeSamplePage(t, app, "Work", "Journal", "Daily", "2026-06-13", taskID, "status stamp")

	if err := app.UpdateBlockState(taskID, "DOING"); err != nil {
		t.Fatalf("UpdateBlockState: %v", err)
	}
	var modified sql.NullString
	if err := app.db.SQLDB().QueryRow(
		"SELECT modified_at FROM tasks WHERE block_id = ?", taskID,
	).Scan(&modified); err != nil {
		t.Fatalf("query: %v", err)
	}
	if !modified.Valid || modified.String == "" {
		t.Fatal("expected modified_at after status change")
	}
	pagePath := filepath.Join(app.vaultPath, "Work", "Journal", "Daily.md")
	content, err := os.ReadFile(pagePath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(content), "[modified::") {
		t.Errorf("file missing [modified::]:\n%s", content)
	}
}
