package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"silt/backend/parser"
)

// journalLinkedTask links an external root and journals an owner mutation for a
// task living there, returning the block ID, the page file, and the root.
func journalLinkedTask(t *testing.T, app *App) (id, pagePath, root string) {
	t.Helper()
	root = filepath.Join(t.TempDir(), "linked")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir linked root: %v", err)
	}
	ln, err := app.LinkNotebook(root)
	if err != nil {
		t.Fatalf("LinkNotebook: %v", err)
	}
	id = "a11d0050-1111-1111-1111-111111111111"
	content := "- [ ] linked task <!-- id: " + id + " -->\n"
	pagePath = filepath.Join(root, "Plan.md")
	writeFile(t, pagePath, content)
	blocks, meta, _, _, err := parser.ParseFileContent(content, ln.DisplayName, "", "Plan", "2026-07-01", app.spacesPerTab)
	if err != nil {
		t.Fatalf("ParseFileContent: %v", err)
	}
	if err := app.db.IndexFileBlocks("linked:"+ln.ID, meta.Notebook, meta.Section, meta.Page, blocks, meta.Tags); err != nil {
		t.Fatalf("IndexFileBlocks: %v", err)
	}
	if err := app.SetTaskOwner(id, "Alice"); err != nil {
		t.Fatalf("SetTaskOwner: %v", err)
	}
	return id, pagePath, root
}

// A page path that can never be replayed again — a directory sitting at the
// .md path, or a section directory that forbids the write — must be popped as
// skipped on the first undo. Retaining it would re-fail identically on every
// click and hide the older mutation for the rest of the session.
func TestUndoLastTaskMutation_SkipsPermanentlyUnreadablePagePath(t *testing.T) {
	cases := []struct {
		name      string
		breakPage func(t *testing.T, pagePath string)
	}{
		{
			name: "directory-at-page-path",
			breakPage: func(t *testing.T, pagePath string) {
				t.Helper()
				if err := os.Remove(pagePath); err != nil {
					t.Fatalf("remove page: %v", err)
				}
				if err := os.Mkdir(pagePath, 0o755); err != nil {
					t.Fatalf("mkdir at page path: %v", err)
				}
			},
		},
		{
			name: "read-only-section-directory",
			breakPage: func(t *testing.T, pagePath string) {
				t.Helper()
				dir := filepath.Dir(pagePath)
				skipUnlessPermissionsEnforced(t, func() error {
					if err := os.Chmod(dir, 0o500); err != nil {
						t.Fatalf("chmod section dir: %v", err)
					}
					return os.WriteFile(filepath.Join(dir, "probe.tmp"), []byte("probe"), 0o644)
				})
				t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := newTestApp(t)
			const (
				olderID = "a11d0051-1111-1111-1111-111111111111"
				deadID  = "a11d0052-1111-1111-1111-111111111111"
			)
			olderPath := indexTestFile(t, app, "W", "S", "UndoOlderEntry", "2026-07-01",
				"- [ ] older <!-- id: "+olderID+" -->\n")
			deadPath := indexTestFile(t, app, "W", "Dead", "UndoDeadEntry", "2026-07-01",
				"- [ ] newer <!-- id: "+deadID+" -->\n")
			if err := app.SetTaskOwner(olderID, "Alice"); err != nil {
				t.Fatalf("SetTaskOwner older: %v", err)
			}
			if err := app.SetTaskOwner(deadID, "Bob"); err != nil {
				t.Fatalf("SetTaskOwner dead: %v", err)
			}
			if got := taskUndoLen(app); got != 2 {
				t.Fatalf("journal after two mutations = %d, want 2", got)
			}

			tc.breakPage(t, deadPath)

			result, err := app.UndoLastTaskMutation()
			if err != nil {
				t.Fatalf("a permanently unreadable page path must drain, not error: %v", err)
			}
			if !result.Skipped || result.Undone {
				t.Fatalf("result = %+v, want a skipped entry", result)
			}
			if !reflect.DeepEqual(result.SkippedBlockIDs, []string{deadID}) {
				t.Errorf("skipped block ids = %v, want [%s]", result.SkippedBlockIDs, deadID)
			}
			if len(result.ChangedFields) != 0 {
				t.Errorf("skipped result advertised restored fields: %v", result.ChangedFields)
			}
			if result.Remaining != 1 {
				t.Errorf("remaining = %d, want 1", result.Remaining)
			}
			if got := taskUndoLen(app); got != 1 {
				t.Fatalf("dead entry was not popped: journal = %d, want 1", got)
			}

			// The older mutation, on a healthy page, is still reachable.
			second, err := app.UndoLastTaskMutation()
			if err != nil {
				t.Fatalf("second UndoLastTaskMutation: %v", err)
			}
			if !second.Undone || second.Remaining != 0 {
				t.Fatalf("older undo result = %+v, want it reversed", second)
			}
			if line := requireTaskLine(t, readTaskPage(t, olderPath), olderID); strings.Contains(line, "[owner::") {
				t.Errorf("older mutation was not reversed: %q", line)
			}
		})
	}
}

// An unmounted linked root is an outage, not a dead link: the entry must stay
// journaled so the undo still works once the root is back.
func TestUndoLastTaskMutation_TransientLinkedRootOutageRetainsEntry(t *testing.T) {
	app := newTestApp(t)
	id, pagePath, root := journalLinkedTask(t, app)
	if got := taskUndoLen(app); got != 1 {
		t.Fatalf("journal after mutation = %d, want 1", got)
	}

	offline := root + "-offline"
	if err := os.Rename(root, offline); err != nil {
		t.Fatalf("rename linked root offline: %v", err)
	}
	t.Cleanup(func() { _ = os.Rename(offline, root) })

	if _, err := app.UndoLastTaskMutation(); err == nil {
		t.Fatal("an unmounted root must surface the failure, not skip the entry")
	}
	if got := taskUndoLen(app); got != 1 {
		t.Fatalf("transient root outage dropped the entry: journal = %d, want 1", got)
	}

	if err := os.Rename(offline, root); err != nil {
		t.Fatalf("restore linked root: %v", err)
	}
	result, err := app.UndoLastTaskMutation()
	if err != nil {
		t.Fatalf("undo after the root returned: %v", err)
	}
	if !result.Undone || result.Skipped {
		t.Fatalf("result = %+v, want the retained entry reversed", result)
	}
	if line := requireTaskLine(t, readTaskPage(t, pagePath), id); strings.Contains(line, "[owner::") {
		t.Errorf("undo did not clear the owner: %q", line)
	}
}

// A root that never comes back must not wedge the journal: the entry drains
// after the bounded number of failed attempts, and the older mutation becomes
// reachable again.
func TestUndoLastTaskMutation_PersistentRootOutageDrainsAfterFailureLimit(t *testing.T) {
	app := newTestApp(t)
	const vaultID = "a11d0053-1111-1111-1111-111111111111"
	vaultPath := indexTestFile(t, app, "W", "S", "UndoBehindOutage", "2026-07-01",
		"- [ ] vault task <!-- id: "+vaultID+" -->\n")
	if err := app.SetTaskOwner(vaultID, "Alice"); err != nil {
		t.Fatalf("SetTaskOwner vault: %v", err)
	}
	linkedID, _, root := journalLinkedTask(t, app)
	if got := taskUndoLen(app); got != 2 {
		t.Fatalf("journal after two mutations = %d, want 2", got)
	}

	offline := root + "-offline"
	if err := os.Rename(root, offline); err != nil {
		t.Fatalf("rename linked root offline: %v", err)
	}
	t.Cleanup(func() { _ = os.Rename(offline, root) })

	for attempt := 1; attempt < taskUndoEntryFailureLimit; attempt++ {
		if _, err := app.UndoLastTaskMutation(); err == nil {
			t.Fatalf("attempt %d: expected the outage error", attempt)
		}
		if got := taskUndoLen(app); got != 2 {
			t.Fatalf("attempt %d dropped the entry early: journal = %d, want 2", attempt, got)
		}
	}

	result, err := app.UndoLastTaskMutation()
	if err != nil {
		t.Fatalf("the drain attempt must report a skip, not error: %v", err)
	}
	if !result.Skipped || result.Undone {
		t.Fatalf("result = %+v, want a skipped entry", result)
	}
	if !reflect.DeepEqual(result.SkippedBlockIDs, []string{linkedID}) {
		t.Errorf("skipped block ids = %v, want [%s]", result.SkippedBlockIDs, linkedID)
	}
	if result.Remaining != 1 {
		t.Errorf("remaining = %d, want 1", result.Remaining)
	}
	if got := taskUndoLen(app); got != 1 {
		t.Fatalf("outage entry was not drained: journal = %d, want 1", got)
	}

	// The older mutation on the healthy vault page is reachable again.
	if _, err := app.UndoLastTaskMutation(); err != nil {
		t.Fatalf("undo behind the outage: %v", err)
	}
	if line := requireTaskLine(t, readTaskPage(t, vaultPath), vaultID); strings.Contains(line, "[owner::") {
		t.Errorf("older mutation was not reversed: %q", line)
	}
}
