package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"silt/backend/parser"
)

// ---------------------------------------------------------------------------
// AppendTaskComment (#456)
//
// Closes the concurrent-post race: the previous SDK two-step
// (FetchSubtree → PluginSaveSubtreeBlocks) was last-write-wins on overlapping
// posts. AppendTaskComment does read-modify-write under one
// LockBlockWrite+LockFileWrite hold so both concurrent comments land.
// ---------------------------------------------------------------------------

func TestAppendTaskComment_AppendsNoteChild(t *testing.T) {
	app := newTestApp(t)
	const taskID = "ac1aaaaa-4444-1111-1111-111111111111"
	content := "- [ ] ship the feature <!-- id: " + taskID + " -->\n"
	indexTestFile(t, app, "W", "S", "CommentBasic", "2026-07-01", content)

	newID, err := app.AppendTaskComment(taskID, "looks good to me", "Dana", "2026-07-07T14:22:00")
	if err != nil {
		t.Fatalf("AppendTaskComment: %v", err)
	}
	if newID == "" {
		t.Fatal("expected a non-empty new comment id")
	}

	// The new NOTE must appear in the task's sub-tree with the right text +
	// attribution tokens ([author:: Dana] / [ts:: ...]).
	sub, err := app.FetchSubtree(taskID)
	if err != nil {
		t.Fatalf("FetchSubtree: %v", err)
	}
	var found *parser.ParsedBlock
	for i := range sub {
		if sub[i].ID == newID {
			found = &sub[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("new comment %s not in sub-tree: %+v", newID, sub)
	}
	if found.Type != parser.BlockNote {
		t.Errorf("expected NOTE type, got %s", found.Type)
	}
	if found.CleanText != "looks good to me" {
		t.Errorf("expected comment text, got %q", found.CleanText)
	}
	if found.Author != "Dana" {
		t.Errorf("expected author=Dana, got %q", found.Author)
	}
	if found.Timestamp != "2026-07-07T14:22:00" {
		t.Errorf("expected ts, got %q", found.Timestamp)
	}
	if found.ParentID != taskID {
		t.Errorf("expected ParentID=%s, got %q", taskID, found.ParentID)
	}

	// The on-disk file carries the attribution tokens (round-trip).
	blocks, _, _, _, perr := parser.ParseFileContent(mustReadTaskFile(t, app, "W", "S", "CommentBasic"), "W", "S", "CommentBasic", "2026-07-01", app.spacesPerTab)
	if perr != nil {
		t.Fatalf("re-parse: %v", perr)
	}
	var onDisk *parser.ParsedBlock
	for i := range blocks {
		if blocks[i].ID == newID {
			onDisk = &blocks[i]
		}
	}
	if onDisk == nil {
		t.Fatal("new comment not found on disk after re-parse")
	}
	if onDisk.Author != "Dana" || onDisk.Timestamp != "2026-07-07T14:22:00" {
		t.Errorf("on-disk attribution drifted: author=%q ts=%q", onDisk.Author, onDisk.Timestamp)
	}
}

func TestAppendTaskComment_ConcurrentPostsBothLand(t *testing.T) {
	app := newTestApp(t)
	const taskID = "ac1bbbbb-4444-1111-1111-111111111111"
	content := "- [ ] review PR <!-- id: " + taskID + " -->\n"
	indexTestFile(t, app, "W", "S", "CommentRace", "2026-07-01", content)

	// Channel barrier: both goroutines block until close(barrier), then race to
	// append. Pre-fix the unlocked two-step SDK would drop one of these; the
	// atomic binding serializes both writes so both NOTE children land.
	barrier := make(chan struct{})
	var wg sync.WaitGroup
	var errA, errB error
	var idA, idB string
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-barrier
		idA, errA = app.AppendTaskComment(taskID, "comment A", "Alice", "2026-07-07T10:00:00")
	}()
	go func() {
		defer wg.Done()
		<-barrier
		idB, errB = app.AppendTaskComment(taskID, "comment B", "Bob", "2026-07-07T10:00:01")
	}()
	close(barrier)
	wg.Wait()

	if errA != nil {
		t.Fatalf("post A: %v", errA)
	}
	if errB != nil {
		t.Fatalf("post B: %v", errB)
	}
	if idA == "" || idB == "" {
		t.Fatalf("expected non-empty ids: A=%q B=%q", idA, idB)
	}
	if idA == idB {
		t.Fatalf("expected distinct UUIDs, both %q", idA)
	}

	sub, err := app.FetchSubtree(taskID)
	if err != nil {
		t.Fatalf("FetchSubtree: %v", err)
	}
	if len(sub) != 2 {
		t.Fatalf("expected 2 comments after concurrent posts, got %d: %+v", len(sub), sub)
	}
	// Both texts must be present (neither post was dropped).
	texts := map[string]bool{sub[0].CleanText: true, sub[1].CleanText: true}
	if !texts["comment A"] || !texts["comment B"] {
		t.Errorf("expected both comments present, got %v", texts)
	}
	// Both ids must be in the sub-tree (the atomic write can't have lost one).
	ids := map[string]bool{sub[0].ID: true, sub[1].ID: true}
	if !ids[idA] || !ids[idB] {
		t.Errorf("expected both ids in sub-tree: want %q,%q got %v", idA, idB, ids)
	}
}

func TestAppendTaskComment_RefusedWhileFocusLocked(t *testing.T) {
	app := newTestApp(t)
	withFocusLockWatcher(t, app)
	const taskID = "ac1ccccc-4444-1111-1111-111111111111"
	content := "- [ ] ship <!-- id: " + taskID + " -->\n"
	indexTestFile(t, app, "W", "S", "CommentFocus", "2026-07-01", content)

	if err := app.AcquireFocusLock("W", "S", "CommentFocus"); err != nil {
		t.Fatalf("AcquireFocusLock: %v", err)
	}
	// The append path mirrors every other task setter's focus-lock guard (#444).
	if _, err := app.AppendTaskComment(taskID, "x", "Dana", "2026-07-07T14:22:00"); !errors.Is(err, errBlockBeingEdited) {
		t.Fatalf("expected errBlockBeingEdited while focus-locked, got %v", err)
	}
	if err := app.ReleaseFocusLock("W", "S", "CommentFocus"); err != nil {
		t.Fatalf("ReleaseFocusLock: %v", err)
	}
	if _, err := app.AppendTaskComment(taskID, "x", "Dana", "2026-07-07T14:22:00"); err != nil {
		t.Fatalf("AppendTaskComment after release: %v", err)
	}
}

func TestAppendTaskComment_RejectsNonTask(t *testing.T) {
	app := newTestApp(t)
	const noteID = "ac1ddddd-4444-1111-1111-111111111111"
	content := "- a plain note <!-- id: " + noteID + " -->\n"
	indexTestFile(t, app, "W", "S", "CommentNonTask", "2026-07-01", content)

	if _, err := app.AppendTaskComment(noteID, "x", "Dana", "2026-07-07T14:22:00"); err == nil {
		t.Fatal("expected error appending comment to a non-task block, got nil")
	}
}

// mustReadTaskFile reads a (notebook/section/page).md from the test vault and
// fails the test on error. Kept local so the comment tests are self-contained.
func mustReadTaskFile(t *testing.T, app *App, notebook, section, page string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(app.vaultPath, notebook, section, page+".md"))
	if err != nil {
		t.Fatalf("read file %s/%s/%s: %v", notebook, section, page, err)
	}
	return string(b)
}

// strings is referenced by sub-tree text assertions above; keep the import
// honest if a future edit removes the only call site.
var _ = strings.Contains
