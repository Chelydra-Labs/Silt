package main

import (
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"silt/backend/parser"
	"silt/backend/plugins"
)

// Table-driven coverage for every restorable task field that is not already
// pinned by a dedicated undo test (status/dueDate/text/order/recurrence/
// blockedBy/owner have their own tests). Each case mutates through the real
// write chain, undoes, and asserts the field is gone from disk again.
func TestUndoLastTaskMutation_RestoresEveryField(t *testing.T) {
	cases := []struct {
		name      string
		mutate    func(t *testing.T, app *App, id string)
		fields    []string
		mutated   string
		gone      string
		present   string
		projected func(t *testing.T, task parser.TaskResult)
	}{
		{
			name: "priority",
			mutate: func(t *testing.T, app *App, id string) {
				if err := app.SetTaskPriority(id, 1); err != nil {
					t.Fatalf("SetTaskPriority: %v", err)
				}
			},
			fields:  []string{"priority"},
			mutated: "[priority:: 1]",
			gone:    "[priority::",
			projected: func(t *testing.T, task parser.TaskResult) {
				if task.Priority != parser.LegacyMissingTaskPriority {
					t.Errorf("index priority after undo = %d, want the token-absent default %d",
						task.Priority, parser.LegacyMissingTaskPriority)
				}
			},
		},
		{
			name: "startDate",
			mutate: func(t *testing.T, app *App, id string) {
				const pluginID = "start-plugin"
				token := registerTestSession(t, app, pluginID)
				if err := app.RequestCapability(pluginID, string(plugins.CapContentMutate), ""); err != nil {
					t.Fatalf("RequestCapability: %v", err)
				}
				if _, err := app.PluginSetTaskStartDate(pluginID, token, id, "2026-08-01"); err != nil {
					t.Fatalf("PluginSetTaskStartDate: %v", err)
				}
			},
			fields:  []string{"startDate"},
			mutated: "[start:: 2026-08-01]",
			gone:    "[start::",
			projected: func(t *testing.T, task parser.TaskResult) {
				if task.StartDate != "" {
					t.Errorf("index start date after undo = %q, want empty", task.StartDate)
				}
			},
		},
		{
			name: "estimate",
			mutate: func(t *testing.T, app *App, id string) {
				const pluginID = "estimate-plugin"
				token := registerTestSession(t, app, pluginID)
				if err := app.RequestCapability(pluginID, string(plugins.CapContentMutate), ""); err != nil {
					t.Fatalf("RequestCapability: %v", err)
				}
				if _, err := app.PluginSetTaskEstimate(pluginID, token, id, "2h"); err != nil {
					t.Fatalf("PluginSetTaskEstimate: %v", err)
				}
			},
			fields:  []string{"estimate"},
			mutated: "[estimate::",
			gone:    "[estimate::",
			projected: func(t *testing.T, task parser.TaskResult) {
				if task.EstimateMinutes != 0 {
					t.Errorf("index estimate minutes after undo = %d, want 0", task.EstimateMinutes)
				}
			},
		},
		{
			name: "pinned",
			mutate: func(t *testing.T, app *App, id string) {
				const pluginID = "pin-plugin"
				token := registerTestSession(t, app, pluginID)
				if err := app.RequestCapability(pluginID, string(plugins.CapContentMutate), ""); err != nil {
					t.Fatalf("RequestCapability: %v", err)
				}
				if _, err := app.PluginUpdateTaskMeta(pluginID, token, id, 1, -1); err != nil {
					t.Fatalf("PluginUpdateTaskMeta pin: %v", err)
				}
			},
			fields:  []string{"pinned"},
			mutated: "[pin:: true]",
			gone:    "[pin::",
			projected: func(t *testing.T, task parser.TaskResult) {
				if task.Pinned != nil {
					t.Errorf("index pinned after undo = %v, want nil", task.Pinned)
				}
			},
		},
		{
			name: "progress",
			mutate: func(t *testing.T, app *App, id string) {
				const pluginID = "progress-plugin"
				token := registerTestSession(t, app, pluginID)
				if err := app.RequestCapability(pluginID, string(plugins.CapContentMutate), ""); err != nil {
					t.Fatalf("RequestCapability: %v", err)
				}
				if _, err := app.PluginUpdateTaskMeta(pluginID, token, id, -1, 50); err != nil {
					t.Fatalf("PluginUpdateTaskMeta progress: %v", err)
				}
			},
			fields:  []string{"progress"},
			mutated: "[progress:: 50]",
			gone:    "[progress::",
			projected: func(t *testing.T, task parser.TaskResult) {
				if task.Progress != 0 {
					t.Errorf("index progress after undo = %d, want 0", task.Progress)
				}
			},
		},
		{
			name: "completed",
			mutate: func(t *testing.T, app *App, id string) {
				if _, err := app.UpdateBlockState(id, "DONE"); err != nil {
					t.Fatalf("UpdateBlockState DONE: %v", err)
				}
			},
			fields:  []string{"status", "completed"},
			mutated: "- [x] ",
			gone:    "[completed::",
			present: "- [ ] ",
			projected: func(t *testing.T, task parser.TaskResult) {
				if task.Status != "TODO" || task.CompletedAt != "" {
					t.Errorf("index row after undo = status %q completed %q, want TODO/empty", task.Status, task.CompletedAt)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := newTestApp(t)
			const id = "a11d0030-1111-1111-1111-111111111111"
			content := "- [ ] task <!-- id: " + id + " -->\n"
			filePath := indexTestFile(t, app, "W", "S", "UndoField", "2026-07-01", content)

			tc.mutate(t, app, id)
			if got := taskUndoLen(app); got != 1 {
				t.Fatalf("mutation journaled %d entries, want 1", got)
			}
			if line := requireTaskLine(t, readTaskPage(t, filePath), id); !strings.Contains(line, tc.mutated) {
				t.Fatalf("mutation did not land on disk (want %q): %q", tc.mutated, line)
			}
			result, err := app.UndoLastTaskMutation()
			if err != nil {
				t.Fatalf("UndoLastTaskMutation: %v", err)
			}
			if !result.Undone || !taskUndoContains(result.ChangedFields, tc.fields[0]) {
				t.Fatalf("undo result = %+v, want %q restored", result, tc.fields[0])
			}
			if !reflect.DeepEqual(result.ChangedFields, tc.fields) {
				t.Errorf("undo changed fields = %v, want exactly %v", result.ChangedFields, tc.fields)
			}
			line := requireTaskLine(t, readTaskPage(t, filePath), id)
			if strings.Contains(line, tc.gone) {
				t.Errorf("undo left %q on disk: %q", tc.gone, line)
			}
			if tc.present != "" && !strings.HasPrefix(line, tc.present) {
				t.Errorf("line = %q, want prefix %q", line, tc.present)
			}
			requireTaskProjection(t, app, id, tc.projected)
		})
	}
}

// A focus-lock refusal during the undo itself must leave the entry journaled
// so a retry after the editor blurs still reverses the mutation.
func TestUndoLastTaskMutation_FocusLockRefusalRetainsEntry(t *testing.T) {
	app := newTestApp(t)
	withFocusLockWatcher(t, app)
	const id = "a11d0031-1111-1111-1111-111111111111"
	content := "- [ ] ship <!-- id: " + id + " -->\n"
	filePath := indexTestFile(t, app, "W", "S", "UndoFocusRefusal", "2026-07-01", content)

	if err := app.SetTaskOwner(id, "Alice"); err != nil {
		t.Fatalf("SetTaskOwner: %v", err)
	}
	if err := app.AcquireFocusLock("W", "S", "UndoFocusRefusal"); err != nil {
		t.Fatalf("AcquireFocusLock: %v", err)
	}
	// A refusal is not a failure of the entry, so even more refusals than the
	// drain cap allows must leave both the entry and its failure count intact.
	for attempt := 1; attempt <= taskUndoEntryFailureLimit+1; attempt++ {
		if _, err := app.UndoLastTaskMutation(); !errors.Is(err, errBlockBeingEdited) {
			t.Fatalf("attempt %d: expected errBlockBeingEdited, got %v", attempt, err)
		}
		if got := taskUndoLen(app); got != 1 {
			t.Fatalf("attempt %d: refused undo popped the entry: journal = %d, want 1", attempt, got)
		}
		if got := taskUndoNewestFailures(app); got != 0 {
			t.Fatalf("attempt %d: focus refusal counted as a replay failure: failures = %d, want 0", attempt, got)
		}
	}
	locked := readTaskPage(t, filePath)
	if !strings.Contains(requireTaskLine(t, locked, id), "[owner:: Alice]") {
		t.Fatalf("refused undo changed the file: %q", locked)
	}

	if err := app.ReleaseFocusLock("W", "S", "UndoFocusRefusal"); err != nil {
		t.Fatalf("ReleaseFocusLock: %v", err)
	}
	result, err := app.UndoLastTaskMutation()
	if err != nil {
		t.Fatalf("retry undo: %v", err)
	}
	if !result.Undone {
		t.Fatalf("retry result = %+v, want undone", result)
	}
	restored := readTaskPage(t, filePath)
	if line := requireTaskLine(t, restored, id); strings.Contains(line, "[owner::") {
		t.Errorf("retry did not restore the owner: %q", line)
	}
}

func TestUndoLastTaskMutation_LinkedRoot(t *testing.T) {
	app := newTestApp(t)
	ext := t.TempDir()
	ln, err := app.LinkNotebook(ext)
	if err != nil {
		t.Fatalf("LinkNotebook: %v", err)
	}
	const id = "a11d0032-1111-1111-1111-111111111111"
	content := "- [ ] linked task <!-- id: " + id + " -->\n"
	filePath := filepath.Join(ext, "Plan.md")
	writeFile(t, filePath, content)
	source := "linked:" + ln.ID
	blocks, meta, _, _, err := parser.ParseFileContent(content, ln.DisplayName, "", "Plan", "2026-07-01", app.spacesPerTab)
	if err != nil {
		t.Fatalf("ParseFileContent: %v", err)
	}
	if err := app.db.IndexFileBlocks(source, meta.Notebook, meta.Section, meta.Page, blocks, meta.Tags); err != nil {
		t.Fatalf("IndexFileBlocks: %v", err)
	}

	if err := app.SetTaskOwner(id, "Alice"); err != nil {
		t.Fatalf("SetTaskOwner: %v", err)
	}
	if got := taskUndoLen(app); got != 1 {
		t.Fatalf("linked write journaled %d entries, want 1", got)
	}
	if line := requireTaskLine(t, readTaskPage(t, filePath), id); !strings.Contains(line, "[owner:: Alice]") {
		t.Fatalf("linked mutation did not land on disk: %q", line)
	}
	result, err := app.UndoLastTaskMutation()
	if err != nil {
		t.Fatalf("UndoLastTaskMutation: %v", err)
	}
	if !result.Undone {
		t.Fatalf("linked undo result = %+v, want undone", result)
	}
	if line := requireTaskLine(t, readTaskPage(t, filePath), id); strings.Contains(line, "[owner::") {
		t.Errorf("linked undo did not restore the owner: %q", line)
	}
	requireTaskProjection(t, app, id, func(t *testing.T, task parser.TaskResult) {
		if task.Owner != "" {
			t.Errorf("linked index owner after undo = %q, want empty", task.Owner)
		}
		if task.Source != source {
			t.Errorf("linked index source after undo = %q, want %q", task.Source, source)
		}
	})
}

// A batch reorder spanning two files journals one entry per file-write, so it
// takes two undos to reverse both writes.
func TestUndoLastTaskMutation_MultiFileBatchOrderNeedsTwoUndos(t *testing.T) {
	app := newTestApp(t)
	const (
		idA = "a11d0033-1111-1111-1111-111111111111"
		idB = "a11d0034-1111-1111-1111-111111111111"
	)
	fileA := indexTestFile(t, app, "W", "S", "BatchA", "2026-07-01",
		"- [ ] first <!-- id: "+idA+" -->\n")
	fileB := indexTestFile(t, app, "W", "S", "BatchB", "2026-07-01",
		"- [ ] second <!-- id: "+idB+" -->\n")

	if err := app.SetTaskOrders([]string{idA, idB}, []int{1, 2}); err != nil {
		t.Fatalf("SetTaskOrders: %v", err)
	}
	if got := taskUndoLen(app); got != 2 {
		t.Fatalf("multi-file batch journaled %d entries, want one per file", got)
	}
	for _, tf := range []struct {
		path  string
		id    string
		order string
	}{{fileA, idA, "[order:: 1]"}, {fileB, idB, "[order:: 2]"}} {
		if line := requireTaskLine(t, readTaskPage(t, tf.path), tf.id); !strings.Contains(line, tf.order) {
			t.Fatalf("mutation did not land on disk for %s: %q", tf.id, line)
		}
	}

	first, err := app.UndoLastTaskMutation()
	if err != nil {
		t.Fatalf("first undo: %v", err)
	}
	if !first.Undone || first.Remaining != 1 {
		t.Fatalf("first undo result = %+v, want one entry left", first)
	}
	second, err := app.UndoLastTaskMutation()
	if err != nil {
		t.Fatalf("second undo: %v", err)
	}
	if !second.Undone || second.Remaining != 0 {
		t.Fatalf("second undo result = %+v, want empty journal", second)
	}

	for _, tf := range []struct {
		path string
		id   string
	}{{fileA, idA}, {fileB, idB}} {
		line := requireTaskLine(t, readTaskPage(t, tf.path), tf.id)
		if strings.Contains(line, "[order::") {
			t.Errorf("undo left an order token on %s: %q", tf.id, line)
		}
		// The cross-file batch is the canonical stale-row case: both rows must
		// follow the restored files, not keep the order the drag wrote.
		requireTaskProjection(t, app, tf.id, func(t *testing.T, task parser.TaskResult) {
			if task.ManualOrder != 0 {
				t.Errorf("index order after undo for %s = %d, want 0", task.ID, task.ManualOrder)
			}
		})
	}
}
