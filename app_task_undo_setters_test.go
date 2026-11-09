package main

import (
	"reflect"
	"strings"
	"testing"

	"silt/backend/parser"
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
	if line := requireTaskLine(t, readTaskPage(t, filePath), id); !strings.Contains(line, "[recur:: every week]") {
		t.Fatalf("mutation did not land on disk: %q", line)
	}
	result, err := app.UndoLastTaskMutation()
	if err != nil {
		t.Fatalf("UndoLastTaskMutation: %v", err)
	}
	if !result.Undone || !taskUndoContains(result.ChangedFields, "recurrence") {
		t.Fatalf("undo result = %+v, want recurrence restored", result)
	}
	line := requireTaskLine(t, readTaskPage(t, filePath), id)
	if strings.Contains(line, "[recur::") {
		t.Errorf("undo left the recurrence token on disk: %q", line)
	}
	if !strings.Contains(line, "[due:: 2026-07-01]") {
		t.Errorf("undo disturbed an untouched field: %q", line)
	}
	requireTaskProjection(t, app, id, func(t *testing.T, task parser.TaskResult) {
		if task.Recurrence != "" {
			t.Errorf("index recurrence after undo = %q, want empty", task.Recurrence)
		}
	})
}

func TestUndoLastTaskMutation_RestoresRecurrenceClear(t *testing.T) {
	app := newTestApp(t)
	const id = "a11d0011-1111-1111-1111-111111111111"
	content := "- [ ] Water plants [due:: 2026-07-01] [recur:: every week] <!-- id: " + id + " -->\n"
	filePath := indexTestFile(t, app, "W", "S", "UndoRecurClear", "2026-07-01", content)

	if err := app.SetTaskRecurrence(id, ""); err != nil {
		t.Fatalf("SetTaskRecurrence clear: %v", err)
	}
	if line := requireTaskLine(t, readTaskPage(t, filePath), id); strings.Contains(line, "[recur::") {
		t.Fatalf("clear mutation did not land on disk: %q", line)
	}
	result, err := app.UndoLastTaskMutation()
	if err != nil {
		t.Fatalf("UndoLastTaskMutation: %v", err)
	}
	if !result.Undone || !taskUndoContains(result.ChangedFields, "recurrence") {
		t.Fatalf("undo result = %+v, want recurrence restored", result)
	}
	if line := requireTaskLine(t, readTaskPage(t, filePath), id); !strings.Contains(line, "[recur:: every week]") {
		t.Errorf("undo did not restore the cleared recurrence rule: %q", line)
	}
	requireTaskProjection(t, app, id, func(t *testing.T, task parser.TaskResult) {
		if task.Recurrence != "every week" {
			t.Errorf("index recurrence after undo = %q, want %q", task.Recurrence, "every week")
		}
	})
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
	if line := requireTaskLine(t, readTaskPage(t, filePath), idA); !strings.Contains(line, "[blocked_by::") {
		t.Fatalf("mutation did not land on disk: %q", line)
	}
	result, err := app.UndoLastTaskMutation()
	if err != nil {
		t.Fatalf("UndoLastTaskMutation: %v", err)
	}
	if !result.Undone || !taskUndoContains(result.ChangedFields, "blockedBy") {
		t.Fatalf("undo result = %+v, want blockedBy restored", result)
	}
	if line := requireTaskLine(t, readTaskPage(t, filePath), idA); strings.Contains(line, "[blocked_by::") {
		t.Errorf("undo left the dependency token on disk: %q", line)
	}
	requireTaskProjection(t, app, idA, func(t *testing.T, task parser.TaskResult) {
		if len(task.BlockedBy) != 0 {
			t.Errorf("index blocked_by after undo = %v, want empty", task.BlockedBy)
		}
	})
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
	if line := requireTaskLine(t, readTaskPage(t, filePath), idA); strings.Contains(line, "[blocked_by::") {
		t.Fatalf("clear mutation did not land on disk: %q", line)
	}
	result, err := app.UndoLastTaskMutation()
	if err != nil {
		t.Fatalf("UndoLastTaskMutation: %v", err)
	}
	if !result.Undone || !taskUndoContains(result.ChangedFields, "blockedBy") {
		t.Fatalf("undo result = %+v, want blockedBy restored", result)
	}
	line := requireTaskLine(t, readTaskPage(t, filePath), idA)
	if !strings.Contains(line, "(( "+idB+" ))") && !strings.Contains(line, "(("+idB+"))") {
		t.Errorf("undo did not restore the cleared dependency: %q", line)
	}
	requireTaskProjection(t, app, idA, func(t *testing.T, task parser.TaskResult) {
		if !reflect.DeepEqual(task.BlockedBy, []string{idB}) {
			t.Errorf("index blocked_by after undo = %v, want [%s]", task.BlockedBy, idB)
		}
	})
}

func TestUndoLastTaskMutation_RestoresShortlistSet(t *testing.T) {
	app := newTestApp(t)
	const id = "a11d0016-1111-1111-1111-111111111111"
	content := "- [ ] ship [priority:: 1] <!-- id: " + id + " -->\n"
	filePath := indexTestFile(t, app, "W", "S", "UndoShortSet", "2026-07-01", content)

	// nil → &0: 0 is a real rank, not a clear, so this must journal a change
	// (nil and &0 are different journal values).
	if err := app.SetTaskShortlistRank(id, 0); err != nil {
		t.Fatalf("SetTaskShortlistRank: %v", err)
	}
	if got := taskUndoLen(app); got != 1 {
		t.Fatalf("shortlist write journaled %d entries, want 1", got)
	}
	if line := requireTaskLine(t, readTaskPage(t, filePath), id); !strings.Contains(line, "[shortlist:: 0]") {
		t.Fatalf("mutation did not land on disk: %q", line)
	}
	result, err := app.UndoLastTaskMutation()
	if err != nil {
		t.Fatalf("UndoLastTaskMutation: %v", err)
	}
	if !result.Undone || !taskUndoContains(result.ChangedFields, "shortlist") {
		t.Fatalf("undo result = %+v, want shortlist restored", result)
	}
	line := requireTaskLine(t, readTaskPage(t, filePath), id)
	if strings.Contains(line, "[shortlist::") {
		t.Errorf("undo left the shortlist token on disk: %q", line)
	}
	if !strings.Contains(line, "[priority:: 1]") {
		t.Errorf("undo disturbed an untouched field: %q", line)
	}
	requireTaskProjection(t, app, id, func(t *testing.T, task parser.TaskResult) {
		if task.ShortlistRank != nil {
			t.Errorf("index shortlist rank after undo = &%d, want nil", *task.ShortlistRank)
		}
	})
}

func TestUndoLastTaskMutation_RestoresShortlistClear(t *testing.T) {
	app := newTestApp(t)
	const id = "a11d0017-1111-1111-1111-111111111111"
	content := "- [ ] ship [shortlist:: 2] <!-- id: " + id + " -->\n"
	filePath := indexTestFile(t, app, "W", "S", "UndoShortClear", "2026-07-01", content)

	// &2 → nil: undo must restore the pointer, not just a value.
	if err := app.SetTaskShortlistRank(id, -1); err != nil {
		t.Fatalf("SetTaskShortlistRank clear: %v", err)
	}
	if line := requireTaskLine(t, readTaskPage(t, filePath), id); strings.Contains(line, "[shortlist::") {
		t.Fatalf("clear mutation did not land on disk: %q", line)
	}
	result, err := app.UndoLastTaskMutation()
	if err != nil {
		t.Fatalf("UndoLastTaskMutation: %v", err)
	}
	if !result.Undone || !taskUndoContains(result.ChangedFields, "shortlist") {
		t.Fatalf("undo result = %+v, want shortlist restored", result)
	}
	if line := requireTaskLine(t, readTaskPage(t, filePath), id); !strings.Contains(line, "[shortlist:: 2]") {
		t.Errorf("undo did not restore the cleared rank: %q", line)
	}
	requireTaskProjection(t, app, id, func(t *testing.T, task parser.TaskResult) {
		if task.ShortlistRank == nil || *task.ShortlistRank != 2 {
			t.Errorf("index shortlist rank after undo = %v, want &2", task.ShortlistRank)
		}
	})
}

func TestUndoLastTaskMutation_RestoresShortlistRankChange(t *testing.T) {
	app := newTestApp(t)
	const id = "a11d0018-1111-1111-1111-111111111111"
	content := "- [ ] ship [shortlist:: 1] <!-- id: " + id + " -->\n"
	filePath := indexTestFile(t, app, "W", "S", "UndoShortChange", "2026-07-01", content)

	// &1 → &2.
	if err := app.SetTaskShortlistRank(id, 2); err != nil {
		t.Fatalf("SetTaskShortlistRank: %v", err)
	}
	if line := requireTaskLine(t, readTaskPage(t, filePath), id); !strings.Contains(line, "[shortlist:: 2]") {
		t.Fatalf("mutation did not land on disk: %q", line)
	}
	result, err := app.UndoLastTaskMutation()
	if err != nil {
		t.Fatalf("UndoLastTaskMutation: %v", err)
	}
	if !result.Undone || !taskUndoContains(result.ChangedFields, "shortlist") {
		t.Fatalf("undo result = %+v, want shortlist restored", result)
	}
	if line := requireTaskLine(t, readTaskPage(t, filePath), id); !strings.Contains(line, "[shortlist:: 1]") {
		t.Errorf("undo did not restore the previous rank: %q", line)
	}
	requireTaskProjection(t, app, id, func(t *testing.T, task parser.TaskResult) {
		if task.ShortlistRank == nil || *task.ShortlistRank != 1 {
			t.Errorf("index shortlist rank after undo = %v, want &1", task.ShortlistRank)
		}
	})

	// There is no redo; re-issuing the write is itself journaled.
	if err := app.SetTaskShortlistRank(id, 2); err != nil {
		t.Fatalf("SetTaskShortlistRank re-set: %v", err)
	}
	if got := taskUndoLen(app); got != 1 {
		t.Fatalf("re-set journaled %d entries, want 1", got)
	}
	if line := requireTaskLine(t, readTaskPage(t, filePath), id); !strings.Contains(line, "[shortlist:: 2]") {
		t.Errorf("re-set did not land on disk: %q", line)
	}
}

// Undo of a shortlist write restores the task's semantic state — the rank
// pointer — not the bytes of a hand-authored invalid token. The stale
// [shortlist:: 9] the setter dropped stays dropped, and the clear-only
// variant (token present, rank already nil) journals nothing at all
// because no tracked field changed. Pinned deliberately: byte fidelity
// across edits is page history's job, not the undo journal's.
func TestUndoLastTaskMutation_ShortlistStaleTokenStaysDropped(t *testing.T) {
	app := newTestApp(t)
	const id = "a11d0030-1111-1111-1111-111111111111"
	content := "- [ ] ship [shortlist:: 9] <!-- id: " + id + " -->\n"
	filePath := indexTestFile(t, app, "W", "S", "UndoShortStale", "2026-07-01", content)

	if err := app.SetTaskShortlistRank(id, 2); err != nil {
		t.Fatalf("SetTaskShortlistRank: %v", err)
	}
	if line := requireTaskLine(t, readTaskPage(t, filePath), id); !strings.Contains(line, "[shortlist:: 2]") {
		t.Fatalf("mutation did not land on disk: %q", line)
	}
	result, err := app.UndoLastTaskMutation()
	if err != nil {
		t.Fatalf("UndoLastTaskMutation: %v", err)
	}
	if !result.Undone || !taskUndoContains(result.ChangedFields, "shortlist") {
		t.Fatalf("undo result = %+v, want shortlist restored", result)
	}
	// Before the write the task was unranked (nil renders no token), and the
	// invalid extra token is not resurrected: the line ends token-less.
	line := requireTaskLine(t, readTaskPage(t, filePath), id)
	if strings.Contains(strings.ToLower(line), "[shortlist::") {
		t.Errorf("undo resurrected the dropped invalid shortlist token: %q", line)
	}
	requireTaskProjection(t, app, id, func(t *testing.T, task parser.TaskResult) {
		if task.ShortlistRank != nil {
			t.Errorf("index shortlist rank after undo = &%d, want nil", *task.ShortlistRank)
		}
	})

	// Clear-only variant: rank nil → nil changes no tracked field, so the
	// token drop is not journaled and there is nothing to undo.
	const id2 = "a11d0031-1111-1111-1111-111111111111"
	content2 := "- [ ] ship [Shortlist:: 9] <!-- id: " + id2 + " -->\n"
	filePath2 := indexTestFile(t, app, "W", "S", "UndoShortStaleClear", "2026-07-01", content2)
	if err := app.SetTaskShortlistRank(id2, -1); err != nil {
		t.Fatalf("SetTaskShortlistRank clear: %v", err)
	}
	if line := requireTaskLine(t, readTaskPage(t, filePath2), id2); strings.Contains(strings.ToLower(line), "[shortlist::") {
		t.Errorf("clear-only drop: stale token must be gone: %q", line)
	}
	if got := taskUndoLen(app); got != 0 {
		t.Fatalf("clear-only stale drop journaled %d entries, want 0", got)
	}
}

func TestSetTaskShortlistRank_SameRankIsNotJournaled(t *testing.T) {
	// &2 → &2 changes no user-visible field, so the write never reaches the
	// journal (recordTaskUndo drops field-identical entries). The pointer-
	// aware nil-vs-&0 distinction is covered by the rank-0 tests above.
	app := newTestApp(t)
	const id = "a11d0019-1111-1111-1111-111111111111"
	content := "- [ ] ship [shortlist:: 2] <!-- id: " + id + " -->\n"
	indexTestFile(t, app, "W", "S", "UndoShortNoop", "2026-07-01", content)

	if err := app.SetTaskShortlistRank(id, 2); err != nil {
		t.Fatalf("SetTaskShortlistRank same-rank: %v", err)
	}
	if got := taskUndoLen(app); got != 0 {
		t.Fatalf("re-sending the current rank journaled %d entries, want 0", got)
	}
}
