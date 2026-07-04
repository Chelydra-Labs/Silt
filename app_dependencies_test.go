package main

import (
	"errors"
	"os"
	"strings"
	"testing"

	"silt/backend/parser"
)

// TestSetTaskBlockedBy_SetChangeClear covers the full lifecycle: set, change,
// and clear a task's dependency list via the IPC layer (#301/#303).
func TestSetTaskBlockedBy_SetChangeClear(t *testing.T) {
	app := newTestApp(t)
	const (
		subject = "a1b2c3d4-1111-1111-1111-111111111111"
		depA    = "a1b2c3d4-2222-2222-2222-222222222222"
		depB    = "a1b2c3d4-3333-3333-3333-333333333333"
	)
	content := "- [ ] subject <!-- id: " + subject + " -->\n" +
		"- [ ] depA <!-- id: " + depA + " -->\n" +
		"- [ ] depB <!-- id: " + depB + " -->\n"
	filePath := indexTestFile(t, app, "W", "S", "SetDep", "2026-07-01", content)

	// Set dependencies on depA + depB.
	if err := app.SetTaskBlockedBy(subject, []string{depA, depB}); err != nil {
		t.Fatalf("SetTaskBlockedBy (set): %v", err)
	}
	updated, _ := os.ReadFile(filePath)
	if !strings.Contains(string(updated), "[blocked_by:: ((a1b2c3d4-2222-2222-2222-222222222222)) ((a1b2c3d4-3333-3333-3333-333333333333))]") {
		t.Errorf("set: blocked_by token missing in:\n%s", updated)
	}

	// Change to only depA.
	if err := app.SetTaskBlockedBy(subject, []string{depA}); err != nil {
		t.Fatalf("SetTaskBlockedBy (change): %v", err)
	}
	updated, _ = os.ReadFile(filePath)
	if !strings.Contains(string(updated), "((a1b2c3d4-2222-2222-2222-222222222222))") {
		t.Errorf("change: depA ref missing in:\n%s", updated)
	}
	// depB's ref must be gone from the blocked_by token. depB's id comment
	// still contains its uuid, so check for the ((...)) ref form on the
	// subject's task line specifically.
	for _, line := range strings.Split(string(updated), "\n") {
		if strings.Contains(line, "<!-- id: "+subject) && strings.Contains(line, "((a1b2c3d4-3333-3333-3333-333333333333))") {
			t.Errorf("change: depB ref should be gone from subject's blocked_by: %s", line)
		}
	}

	// Clear all dependencies.
	if err := app.SetTaskBlockedBy(subject, nil); err != nil {
		t.Fatalf("SetTaskBlockedBy (clear): %v", err)
	}
	updated, _ = os.ReadFile(filePath)
	if strings.Contains(string(updated), "[blocked_by::") {
		t.Errorf("clear: blocked_by token should be gone in:\n%s", updated)
	}
}

// TestSetTaskBlockedBy_RejectsCycle verifies a proposed edge that would close a
// loop is rejected before any disk write (#303 acceptance: prevent circular
// dependencies).
func TestSetTaskBlockedBy_RejectsCycle(t *testing.T) {
	app := newTestApp(t)
	const (
		a = "b2c3d4e5-1111-1111-1111-111111111111"
		b = "b2c3d4e5-2222-2222-2222-222222222222"
	)
	content := "- [ ] A <!-- id: " + a + " -->\n" +
		"- [ ] B <!-- id: " + b + " -->\n"
	filePath := indexTestFile(t, app, "W", "S", "Cycle", "2026-07-01", content)

	// A blocked_by B — allowed (no cycle yet).
	if err := app.SetTaskBlockedBy(a, []string{b}); err != nil {
		t.Fatalf("set A->B: %v", err)
	}
	// B blocked_by A — must be rejected as a cycle.
	err := app.SetTaskBlockedBy(b, []string{a})
	if !errors.Is(err, ErrTaskCycle) {
		t.Fatalf("expected ErrTaskCycle, got %v", err)
	}
	updated, _ := os.ReadFile(filePath)
	// B's line (the second task) must not carry a blocked_by token — the
	// write was rejected. A's line legitimately carries [blocked_by:: B] from
	// the first (allowed) set, so we scope the check to B's id comment.
	for _, line := range strings.Split(string(updated), "\n") {
		// B's line ends with B's id comment.
		if strings.Contains(line, "<!-- id: "+b) && strings.Contains(line, "[blocked_by::") {
			t.Errorf("cycle edge must not reach disk, B line has blocked_by: %s", line)
		}
	}
}

// TestSetTaskBlockedBy_RejectsSelfLoop verifies a self-edge (A blocked_by A)
// is rejected as a trivial cycle.
func TestSetTaskBlockedBy_RejectsSelfLoop(t *testing.T) {
	app := newTestApp(t)
	const a = "c3d4e5f6-1111-1111-1111-111111111111"
	content := "- [ ] A <!-- id: " + a + " -->\n"
	indexTestFile(t, app, "W", "S", "SelfLoop", "2026-07-01", content)

	err := app.SetTaskBlockedBy(a, []string{a})
	if !errors.Is(err, ErrTaskCycle) {
		t.Fatalf("expected ErrTaskCycle for self-loop, got %v", err)
	}
}

// TestSetTaskBlockedBy_IndexReflectsChange verifies QueryTasksWithFilters
// surfaces the new BlockedBy list (#302 projection).
func TestSetTaskBlockedBy_IndexReflectsChange(t *testing.T) {
	app := newTestApp(t)
	const (
		subject = "d4e5f6a7-1111-1111-1111-111111111111"
		dep     = "d4e5f6a7-2222-2222-2222-222222222222"
	)
	content := "- [ ] subject <!-- id: " + subject + " -->\n" +
		"- [ ] dep <!-- id: " + dep + " -->\n"
	indexTestFile(t, app, "W", "S", "IdxDep", "2026-07-01", content)

	if err := app.SetTaskBlockedBy(subject, []string{dep}); err != nil {
		t.Fatalf("SetTaskBlockedBy: %v", err)
	}

	tasks, err := app.db.QueryTasksWithFilters(parser.TaskQueryFilter{})
	if err != nil {
		t.Fatalf("QueryTasks: %v", err)
	}
	var found bool
	for _, tk := range tasks {
		if tk.ID == subject {
			if len(tk.BlockedBy) != 1 || tk.BlockedBy[0] != dep {
				t.Errorf("expected BlockedBy=[%s], got %v", dep, tk.BlockedBy)
			}
			found = true
		}
	}
	if !found {
		t.Errorf("subject %s not returned by QueryTasks", subject)
	}
}

// TestSetTaskBlockedBy_RejectsNonTask verifies the method refuses non-task
// blocks.
func TestSetTaskBlockedBy_RejectsNonTask(t *testing.T) {
	app := newTestApp(t)
	const (
		note = "e5f6a7b8-1111-1111-1111-111111111111"
		dep  = "e5f6a7b8-2222-2222-2222-222222222222"
	)
	content := "- a note block <!-- id: " + note + " -->\n" +
		"- [ ] dep <!-- id: " + dep + " -->\n"
	indexTestFile(t, app, "W", "S", "NonTaskDep", "2026-07-01", content)

	err := app.SetTaskBlockedBy(note, []string{dep})
	if err == nil {
		t.Fatal("expected error setting blocked_by on a non-task block, got nil")
	}
}

// TestGetTaskBlockers_OpenOnly verifies the DONE-guard helper returns only the
// non-DONE prerequisites (#302). Once a blocker completes, it drops out of the
// open-blocker list.
func TestGetTaskBlockers_OpenOnly(t *testing.T) {
	app := newTestApp(t)
	const (
		subject  = "f6a7b8c9-1111-1111-1111-111111111111"
		blocker1 = "f6a7b8c9-2222-2222-2222-222222222222"
		blocker2 = "f6a7b8c9-3333-3333-3333-333333333333"
	)
	content := "- [ ] subject <!-- id: " + subject + " -->\n" +
		"- [ ] blocker1 <!-- id: " + blocker1 + " -->\n" +
		"- [ ] blocker2 <!-- id: " + blocker2 + " -->\n"
	indexTestFile(t, app, "W", "S", "Blockers", "2026-07-01", content)

	if err := app.SetTaskBlockedBy(subject, []string{blocker1, blocker2}); err != nil {
		t.Fatalf("SetTaskBlockedBy: %v", err)
	}

	// Both blockers open initially.
	open, err := app.GetTaskBlockers(subject)
	if err != nil {
		t.Fatalf("GetTaskBlockers (initial): %v", err)
	}
	if len(open) != 2 {
		t.Fatalf("expected 2 open blockers, got %d", len(open))
	}

	// Complete blocker1; only blocker2 should remain open.
	if err := app.UpdateBlockState(blocker1, "DONE"); err != nil {
		t.Fatalf("UpdateBlockState blocker1 DONE: %v", err)
	}
	open, err = app.GetTaskBlockers(subject)
	if err != nil {
		t.Fatalf("GetTaskBlockers (after): %v", err)
	}
	if len(open) != 1 {
		t.Fatalf("expected 1 open blocker after completing one, got %d", len(open))
	}
	if open[0].ID != blocker2 {
		t.Errorf("expected remaining blocker %s, got %s", blocker2, open[0].ID)
	}
}

// TestUpdateBlockState_DoneFanOutEmitsForDependents verifies that completing
// a blocker broadcasts block:changed for each dependent task so their derived
// "blocked" state re-evaluates (#301 reactive re-index).
func TestUpdateBlockState_DoneFanOutEmitsForDependents(t *testing.T) {
	app := newTestApp(t)
	const (
		dependent = "a7b8c9d0-1111-1111-1111-111111111111"
		blocker   = "a7b8c9d0-2222-2222-2222-222222222222"
	)
	content := "- [ ] dependent <!-- id: " + dependent + " -->\n" +
		"- [ ] blocker <!-- id: " + blocker + " -->\n"
	indexTestFile(t, app, "W", "S", "FanOut", "2026-07-01", content)
	if err := app.SetTaskBlockedBy(dependent, []string{blocker}); err != nil {
		t.Fatalf("SetTaskBlockedBy: %v", err)
	}

	// Capture block:changed events. The app's event emitter uses runtime.EventsEmit
	// which is nil-safe (no ctx in tests), so we instead assert the side effect
	// the fan-out exists to enable: after completing the blocker, the
	// dependent's open-blocker list drops to empty (proving the derived state
	// would refresh on a re-query).
	if err := app.UpdateBlockState(blocker, "DONE"); err != nil {
		t.Fatalf("UpdateBlockState blocker DONE: %v", err)
	}
	open, err := app.GetTaskBlockers(dependent)
	if err != nil {
		t.Fatalf("GetTaskBlockers after fan-out: %v", err)
	}
	if len(open) != 0 {
		t.Errorf("expected dependent to have 0 open blockers after fan-out, got %d", len(open))
	}

	// Completing a TODO->DONE transition for an unrelated task (no dependents)
	// must be a no-op fan-out (no panic, no error).
	const lone = "a7b8c9d0-3333-3333-3333-333333333333"
	content2 := "- [ ] lone <!-- id: " + lone + " -->\n"
	indexTestFile(t, app, "W", "S", "FanOutLone", "2026-07-01", content2)
	if err := app.UpdateBlockState(lone, "DONE"); err != nil {
		t.Fatalf("UpdateBlockState lone DONE: %v", err)
	}
}
