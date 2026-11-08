package main

import (
	"errors"
	"os"
	"path/filepath"
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
		name    string
		mutate  func(t *testing.T, app *App, id string)
		field   string
		gone    string
		present string
	}{
		{
			name: "priority",
			mutate: func(t *testing.T, app *App, id string) {
				if err := app.SetTaskPriority(id, 1); err != nil {
					t.Fatalf("SetTaskPriority: %v", err)
				}
			},
			field: "priority",
			gone:  "[priority::",
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
			field: "startDate",
			gone:  "[start::",
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
			field: "estimate",
			gone:  "[estimate::",
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
			field: "pinned",
			gone:  "[pin::",
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
			field: "progress",
			gone:  "[progress::",
		},
		{
			name: "completed",
			mutate: func(t *testing.T, app *App, id string) {
				if _, err := app.UpdateBlockState(id, "DONE"); err != nil {
					t.Fatalf("UpdateBlockState DONE: %v", err)
				}
			},
			field:   "completed",
			gone:    "[completed::",
			present: "- [ ] ",
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
			result, err := app.UndoLastTaskMutation()
			if err != nil {
				t.Fatalf("UndoLastTaskMutation: %v", err)
			}
			if !result.Undone || !taskUndoContains(result.ChangedFields, tc.field) {
				t.Fatalf("undo result = %+v, want %q restored", result, tc.field)
			}
			restored, _ := os.ReadFile(filePath)
			line := taskLineForID(string(restored), id)
			if strings.Contains(line, tc.gone) {
				t.Errorf("undo left %q on disk: %q", tc.gone, line)
			}
			if tc.present != "" && !strings.HasPrefix(line, tc.present) {
				t.Errorf("line = %q, want prefix %q", line, tc.present)
			}
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
	if _, err := app.UndoLastTaskMutation(); !errors.Is(err, errBlockBeingEdited) {
		t.Fatalf("expected errBlockBeingEdited, got %v", err)
	}
	if got := taskUndoLen(app); got != 1 {
		t.Fatalf("refused undo popped the entry: journal = %d, want 1", got)
	}
	locked, _ := os.ReadFile(filePath)
	if !strings.Contains(taskLineForID(string(locked), id), "[owner:: Alice]") {
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
	restored, _ := os.ReadFile(filePath)
	if line := taskLineForID(string(restored), id); strings.Contains(line, "[owner::") {
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
	result, err := app.UndoLastTaskMutation()
	if err != nil {
		t.Fatalf("UndoLastTaskMutation: %v", err)
	}
	if !result.Undone {
		t.Fatalf("linked undo result = %+v, want undone", result)
	}
	restored, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read linked page: %v", err)
	}
	if line := taskLineForID(string(restored), id); strings.Contains(line, "[owner::") {
		t.Errorf("linked undo did not restore the owner: %q", line)
	}
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
		content, _ := os.ReadFile(tf.path)
		if line := taskLineForID(string(content), tf.id); strings.Contains(line, "[order::") {
			t.Errorf("undo left an order token on %s: %q", tf.id, line)
		}
	}
}
