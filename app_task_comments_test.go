package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"silt/backend/parser"
	"silt/backend/plugins"
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

	newID, err := app.AppendTaskComment(taskID, "looks good to me", "Dana", "2026-07-07T14:22:00", "")
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
		idA, errA = app.AppendTaskComment(taskID, "comment A", "Alice", "2026-07-07T10:00:00", "")
	}()
	go func() {
		defer wg.Done()
		<-barrier
		idB, errB = app.AppendTaskComment(taskID, "comment B", "Bob", "2026-07-07T10:00:01", "")
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
	if _, err := app.AppendTaskComment(taskID, "x", "Dana", "2026-07-07T14:22:00", ""); !errors.Is(err, errBlockBeingEdited) {
		t.Fatalf("expected errBlockBeingEdited while focus-locked, got %v", err)
	}
	if err := app.ReleaseFocusLock("W", "S", "CommentFocus"); err != nil {
		t.Fatalf("ReleaseFocusLock: %v", err)
	}
	if _, err := app.AppendTaskComment(taskID, "x", "Dana", "2026-07-07T14:22:00", ""); err != nil {
		t.Fatalf("AppendTaskComment after release: %v", err)
	}
}

func TestAppendTaskComment_RejectsNonTask(t *testing.T) {
	app := newTestApp(t)
	const noteID = "ac1ddddd-4444-1111-1111-111111111111"
	content := "- a plain note <!-- id: " + noteID + " -->\n"
	indexTestFile(t, app, "W", "S", "CommentNonTask", "2026-07-01", content)

	if _, err := app.AppendTaskComment(noteID, "x", "Dana", "2026-07-07T14:22:00", ""); err == nil {
		t.Fatal("expected error appending comment to a non-task block, got nil")
	}
}

// TestSaveSubtreeBlocks_RefusedWhileFocusLocked pins the focus-lock guard on
// saveSubtreeBlocks — the shared core of the sub-editor splice and the only
// task-block writer that was missing the #444 guard. It does a full-file
// read-modify-write, so without the guard a sub-editor save would clobber an
// in-flight editor edit on the same file.
func TestSaveSubtreeBlocks_RefusedWhileFocusLocked(t *testing.T) {
	app := newTestApp(t)
	withFocusLockWatcher(t, app)
	const taskID = "ac1eeeee-4444-1111-1111-111111111111"
	content := "- [ ] ship <!-- id: " + taskID + " -->\n"
	indexTestFile(t, app, "W", "S", "SubtreeFocus", "2026-07-01", content)

	if err := app.AcquireFocusLock("W", "S", "SubtreeFocus"); err != nil {
		t.Fatalf("AcquireFocusLock: %v", err)
	}
	children := []parser.ParsedBlock{
		{ID: "ac1c1c1c-4444-1111-1111-111111111111", ParentID: taskID, Type: parser.BlockNote, Depth: 1, CleanText: "a child note"},
	}
	if _, err := app.SaveSubtreeBlocks(taskID, children); !errors.Is(err, errBlockBeingEdited) {
		t.Fatalf("expected errBlockBeingEdited while focus-locked, got %v", err)
	}
	if err := app.ReleaseFocusLock("W", "S", "SubtreeFocus"); err != nil {
		t.Fatalf("ReleaseFocusLock: %v", err)
	}
	if _, err := app.SaveSubtreeBlocks(taskID, children); err != nil {
		t.Fatalf("SaveSubtreeBlocks after release: %v", err)
	}
}

// TestAppendTaskComment_RejectsEmptyText pins the contract guard: empty or
// whitespace-only comment text is rejected before lock acquisition. A bare
// NOTE bullet with no body is never a useful comment.
func TestAppendTaskComment_RejectsEmptyText(t *testing.T) {
	app := newTestApp(t)
	const taskID = "ac1fffff-4444-1111-1111-111111111111"
	content := "- [ ] ship <!-- id: " + taskID + " -->\n"
	indexTestFile(t, app, "W", "S", "CommentEmpty", "2026-07-01", content)

	for _, text := range []string{"", "   ", "\t\n  "} {
		if _, err := app.AppendTaskComment(taskID, text, "Dana", "2026-07-07T14:22:00", ""); err == nil {
			t.Fatalf("expected error for empty/whitespace comment text %q, got nil", text)
		}
	}
}

// TestPluginAppendTaskComment_GatedByCapability mirrors the content-mutate gate
// pattern the other Plugin* task setters use: a third-party plugin without the
// grant is denied (no splice, no id); the same plugin with content-mutate
// succeeds and lands the NOTE child. Pins #456's plugin-wrapper parity.
func TestPluginAppendTaskComment_GatedByCapability(t *testing.T) {
	app := newTestApp(t)
	const taskID = "ac1eeeee-4444-1111-1111-111111111111"
	content := "- [ ] gated comment target <!-- id: " + taskID + " -->\n"
	indexTestFile(t, app, "W", "S", "CommentGated", "2026-07-01", content)

	tok := registerTestSession(t, app, "third-party")
	// Without the content-mutate grant: rejected, no comment lands.
	if _, err := app.PluginAppendTaskComment("third-party", tok, taskID, "x", "Dana", "2026-07-07T14:22:00", ""); err == nil {
		t.Fatal("expected capability denial without content-mutate grant")
	}
	sub, _ := app.FetchSubtree(taskID)
	if len(sub) != 0 {
		t.Errorf("no comment should land on a denied call, got %d children", len(sub))
	}

	// Grant content-mutate; the same call now succeeds and lands the NOTE.
	if err := app.RequestCapability("third-party", string(plugins.CapContentMutate), ""); err != nil {
		t.Fatalf("grant: %v", err)
	}
	id, err := app.PluginAppendTaskComment("third-party", tok, taskID, "looks good", "Dana", "2026-07-07T14:22:00", "")
	if err != nil {
		t.Fatalf("PluginAppendTaskComment with grant: %v", err)
	}
	if id == "" {
		t.Fatal("expected a non-empty new comment id from granted call")
	}
	sub, _ = app.FetchSubtree(taskID)
	if len(sub) != 1 || sub[0].ID != id {
		t.Errorf("granted call should have landed exactly one NOTE child id=%q: %+v", id, sub)
	}
}

// TestAppendTaskComment_NestedReply nests a reply under an existing NOTE with
// deeper indent (#438). ParentID points at the parent comment (not the task),
// and Depth is parentDepth+1 so the on-disk indent reflects the thread.
func TestAppendTaskComment_NestedReply(t *testing.T) {
	app := newTestApp(t)
	const taskID = "ac1aeee0-4444-1111-1111-111111111111"
	content := "- [ ] discuss design <!-- id: " + taskID + " -->\n"
	indexTestFile(t, app, "W", "S", "CommentNest", "2026-07-01", content)

	parentID, err := app.AppendTaskComment(taskID, "top-level thought", "Alice", "2026-07-07T10:00:00", "")
	if err != nil {
		t.Fatalf("parent comment: %v", err)
	}

	// A sibling top-level comment after the parent ensures the reply is
	// spliced after the parent's descendants, not merely appended at the end.
	siblingID, err := app.AppendTaskComment(taskID, "another top-level", "Carol", "2026-07-07T10:01:00", "")
	if err != nil {
		t.Fatalf("sibling comment: %v", err)
	}

	replyID, err := app.AppendTaskComment(taskID, "reply to Alice", "Bob", "2026-07-07T10:02:00", parentID)
	if err != nil {
		t.Fatalf("nested reply: %v", err)
	}
	if replyID == "" {
		t.Fatal("expected a non-empty reply id")
	}

	sub, err := app.FetchSubtree(taskID)
	if err != nil {
		t.Fatalf("FetchSubtree: %v", err)
	}
	if len(sub) != 3 {
		t.Fatalf("expected 3 notes in sub-tree, got %d: %+v", len(sub), sub)
	}

	var parent, reply, sibling *parser.ParsedBlock
	for i := range sub {
		switch sub[i].ID {
		case parentID:
			parent = &sub[i]
		case replyID:
			reply = &sub[i]
		case siblingID:
			sibling = &sub[i]
		}
	}
	if parent == nil || reply == nil || sibling == nil {
		t.Fatalf("missing blocks in sub-tree: parent=%v reply=%v sibling=%v ids=%v",
			parent != nil, reply != nil, sibling != nil,
			[]string{sub[0].ID, sub[1].ID, sub[2].ID})
	}
	if reply.ParentID != parentID {
		t.Errorf("reply ParentID: want %s, got %q", parentID, reply.ParentID)
	}
	if reply.Depth != parent.Depth+1 {
		t.Errorf("reply Depth: want %d (parent+1), got %d (parent=%d)", parent.Depth+1, reply.Depth, parent.Depth)
	}
	if reply.Type != parser.BlockNote {
		t.Errorf("reply type: want NOTE, got %s", reply.Type)
	}
	if reply.CleanText != "reply to Alice" {
		t.Errorf("reply text: got %q", reply.CleanText)
	}
	if sibling.ParentID != taskID {
		t.Errorf("sibling should stay top-level under task, ParentID=%q", sibling.ParentID)
	}

	// File order: parent, reply (nested under parent), sibling — reply must
	// sit between parent and sibling, not after sibling.
	var order []string
	for _, b := range sub {
		order = append(order, b.ID)
	}
	if !(order[0] == parentID && order[1] == replyID && order[2] == siblingID) {
		t.Errorf("expected order [parent, reply, sibling], got %v", order)
	}

	// Reject nesting under a missing / non-NOTE / out-of-subtree parent.
	if _, err := app.AppendTaskComment(taskID, "x", "Dana", "2026-07-07T11:00:00", "no-such-comment"); err == nil {
		t.Fatal("expected error for missing parent comment")
	}
	if _, err := app.AppendTaskComment(taskID, "x", "Dana", "2026-07-07T11:00:00", taskID); err == nil {
		t.Fatal("expected error nesting under the task id (not a NOTE)")
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

// ---------------------------------------------------------------------------
// Comment preservation across a sub-notes save (#1151)
//
// Comments are child NOTE blocks with attribution; the sub-editor hydrates and
// saves only the non-comment blocks. saveSubtreeBlocks re-reads the task's own
// comments under the write lock and re-appends them as a contiguous tail, so a
// notes save can neither rewrite their attribution nor drop them.
// ---------------------------------------------------------------------------

// taskCommentsCount reads the derived comments_count cache for a task row.
func taskCommentsCount(t *testing.T, app *App, taskID string) int {
	t.Helper()
	var n int
	if err := app.db.SQLDB().QueryRow(
		`SELECT comments_count FROM tasks WHERE block_id = ?`, taskID,
	).Scan(&n); err != nil {
		t.Fatalf("query comments_count for %s: %v", taskID, err)
	}
	return n
}

// findSubtreeBlock returns the block with id from a fetched sub-tree.
func findSubtreeBlock(t *testing.T, sub []parser.ParsedBlock, id string) *parser.ParsedBlock {
	t.Helper()
	for i := range sub {
		if sub[i].ID == id {
			return &sub[i]
		}
	}
	return nil
}

func TestSaveSubtreeBlocks_PreservesTaskComments(t *testing.T) {
	app := newTestApp(t)
	const (
		taskID    = "ad000001-5555-1111-1111-111111111111"
		commentID = "ad000002-5555-1111-1111-111111111111"
		replyID   = "ad000003-5555-1111-1111-111111111111"
		proseID   = "ad000004-5555-1111-1111-111111111111"
		subTaskID = "ad000005-5555-1111-1111-111111111111"
	)
	content := "- [ ] parent task <!-- id: " + taskID + " -->\n" +
		"\t- top level comment [author:: Dana] [ts:: 2026-07-07T10:00:00] <!-- id: " + commentID + " -->\n" +
		"\t\t- reply to Dana [author:: Bob] [ts:: 2026-07-07T10:01:00] <!-- id: " + replyID + " -->\n" +
		"\t- prose note <!-- id: " + proseID + " -->\n" +
		"\t- [ ] sub-task <!-- id: " + subTaskID + " -->\n"
	indexTestFile(t, app, "W", "S", "SubtreeKeepsComments", "2026-07-01", content)

	before := taskCommentsCount(t, app, taskID)
	if before != 3 {
		t.Fatalf("pre-save comments_count = %d, want 3 (comment + reply + prose NOTE descendant)", before)
	}

	// What the sub-editor sends: only non-comment blocks, depths rebased to 0.
	children := []parser.ParsedBlock{
		{ID: proseID, Type: parser.BlockNote, Depth: 0, CleanText: "prose note"},
		{ID: subTaskID, Type: parser.BlockTask, Depth: 0, Status: "TODO", CleanText: "sub-task"},
	}
	if ok, err := app.SaveSubtreeBlocks(taskID, children); err != nil || !ok {
		t.Fatalf("SaveSubtreeBlocks: ok=%v err=%v", ok, err)
	}

	sub, err := app.FetchSubtree(taskID)
	if err != nil {
		t.Fatalf("FetchSubtree: %v", err)
	}
	var order []string
	for _, b := range sub {
		order = append(order, b.ID)
	}
	want := []string{proseID, subTaskID, commentID, replyID}
	if len(order) != len(want) {
		t.Fatalf("subtree = %v, want %v (comments as a tail, in file order)", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("subtree order = %v, want %v", order, want)
		}
	}

	comment := findSubtreeBlock(t, sub, commentID)
	if comment == nil {
		t.Fatal("preserved comment missing from the sub-tree")
	}
	if comment.CleanText != "top level comment" || comment.Author != "Dana" || comment.Timestamp != "2026-07-07T10:00:00" {
		t.Errorf("comment attribution drifted: text=%q author=%q ts=%q",
			comment.CleanText, comment.Author, comment.Timestamp)
	}
	if comment.Depth != 1 || comment.ParentID != taskID {
		t.Errorf("comment nesting drifted: depth=%d parent=%q, want depth 1 parented to the task", comment.Depth, comment.ParentID)
	}

	reply := findSubtreeBlock(t, sub, replyID)
	if reply == nil {
		t.Fatal("preserved reply missing from the sub-tree")
	}
	if reply.Author != "Bob" || reply.Timestamp != "2026-07-07T10:01:00" {
		t.Errorf("reply attribution drifted: author=%q ts=%q", reply.Author, reply.Timestamp)
	}
	if reply.Depth != comment.Depth+1 || reply.ParentID != commentID {
		t.Errorf("reply nesting drifted: depth=%d parent=%q, want depth %d under %s",
			reply.Depth, reply.ParentID, comment.Depth+1, commentID)
	}

	// The derived count is unchanged by the notes save.
	if after := taskCommentsCount(t, app, taskID); after != before {
		t.Errorf("comments_count changed across a notes save: %d → %d", before, after)
	}

	// The tokens survive on disk (round-trip, not just the in-memory projection).
	s := mustReadTaskFile(t, app, "W", "S", "SubtreeKeepsComments")
	for _, token := range []string{"[author:: Dana]", "[ts:: 2026-07-07T10:00:00]", "[author:: Bob]"} {
		if !strings.Contains(s, token) {
			t.Errorf("on-disk comment lost %q:\n%s", token, s)
		}
	}
}

// TestSaveSubtreeBlocks_PreservesCommentAppendedWhileEditing is the
// concurrency proof: a comment posted after the editor built its payload is
// present when the save re-reads the file under the write lock, so it survives
// the stale payload instead of being spliced away.
func TestSaveSubtreeBlocks_PreservesCommentAppendedWhileEditing(t *testing.T) {
	app := newTestApp(t)
	const (
		taskID  = "ad000011-5555-1111-1111-111111111111"
		proseID = "ad000012-5555-1111-1111-111111111111"
	)
	content := "- [ ] parent task <!-- id: " + taskID + " -->\n" +
		"\t- prose note <!-- id: " + proseID + " -->\n"
	indexTestFile(t, app, "W", "S", "SubtreeStalePayload", "2026-07-01", content)

	// The editor opened, saw the prose, and built this payload.
	stale := []parser.ParsedBlock{
		{ID: proseID, Type: parser.BlockNote, Depth: 0, CleanText: "prose note"},
	}

	// Meanwhile a comment lands through the thread's own atomic path.
	commentID, err := app.AppendTaskComment(taskID, "posted while open", "Dana", "2026-07-07T10:00:00", "")
	if err != nil {
		t.Fatalf("AppendTaskComment: %v", err)
	}

	// The stale payload is saved afterwards — it does not know the comment.
	before := taskCommentsCount(t, app, taskID)
	if _, err := app.SaveSubtreeBlocks(taskID, stale); err != nil {
		t.Fatalf("SaveSubtreeBlocks: %v", err)
	}

	sub, err := app.FetchSubtree(taskID)
	if err != nil {
		t.Fatalf("FetchSubtree: %v", err)
	}
	comment := findSubtreeBlock(t, sub, commentID)
	if comment == nil {
		t.Fatalf("comment appended mid-edit was lost: %+v", sub)
	}
	if comment.CleanText != "posted while open" || comment.Author != "Dana" || comment.Timestamp != "2026-07-07T10:00:00" {
		t.Errorf("surviving comment drifted: text=%q author=%q ts=%q",
			comment.CleanText, comment.Author, comment.Timestamp)
	}
	if got := taskCommentsCount(t, app, taskID); got != before {
		t.Errorf("comments_count changed across the stale save: %d → %d", before, got)
	}
}

// TestSaveSubtreeBlocks_DeletedSubTaskDropsOnlyItsComments pins D3: a comment
// whose ancestor chain crosses a TASK belongs to that sub-task, so deleting the
// sub-task in the editor takes its comments with it (no resurrection), while
// the parent task's own comments survive.
func TestSaveSubtreeBlocks_DeletedSubTaskDropsOnlyItsComments(t *testing.T) {
	app := newTestApp(t)
	const (
		taskID     = "ad000021-5555-1111-1111-111111111111"
		commentID  = "ad000022-5555-1111-1111-111111111111"
		subTaskID  = "ad000023-5555-1111-1111-111111111111"
		subComment = "ad000024-5555-1111-1111-111111111111"
	)
	content := "- [ ] parent task <!-- id: " + taskID + " -->\n" +
		"\t- task comment [author:: Dana] [ts:: 2026-07-07T10:00:00] <!-- id: " + commentID + " -->\n" +
		"\t- [ ] sub-task <!-- id: " + subTaskID + " -->\n" +
		"\t\t- sub-task comment [author:: Eve] [ts:: 2026-07-07T10:05:00] <!-- id: " + subComment + " -->\n"
	indexTestFile(t, app, "W", "S", "SubtreeDropSubComments", "2026-07-01", content)

	// Delete the sub-task: the editor payload is empty (the sub-task and its
	// comments are not editor content).
	if _, err := app.SaveSubtreeBlocks(taskID, nil); err != nil {
		t.Fatalf("SaveSubtreeBlocks: %v", err)
	}

	sub, err := app.FetchSubtree(taskID)
	if err != nil {
		t.Fatalf("FetchSubtree: %v", err)
	}
	if len(sub) != 1 || sub[0].ID != commentID {
		t.Fatalf("subtree after deleting the sub-task = %+v, want only the task's own comment", sub)
	}
	if strings.Contains(mustReadTaskFile(t, app, "W", "S", "SubtreeDropSubComments"), subComment) {
		t.Error("sub-task's own comment survived its sub-task (resurrected orphan)")
	}
	if got := taskCommentsCount(t, app, taskID); got != 1 {
		t.Errorf("comments_count = %d, want 1 (only the surviving task comment)", got)
	}
}

// TestSaveSubtreeBlocks_CommentTailIsIdempotent pins D4: a second identical
// save neither duplicates comments nor drifts their order.
func TestSaveSubtreeBlocks_CommentTailIsIdempotent(t *testing.T) {
	app := newTestApp(t)
	const (
		taskID   = "ad000031-5555-1111-1111-111111111111"
		comment1 = "ad000032-5555-1111-1111-111111111111"
		replyID  = "ad000033-5555-1111-1111-111111111111"
		comment2 = "ad000034-5555-1111-1111-111111111111"
		proseID  = "ad000035-5555-1111-1111-111111111111"
	)
	content := "- [ ] parent task <!-- id: " + taskID + " -->\n" +
		"\t- first comment [author:: Dana] [ts:: 2026-07-07T10:00:00] <!-- id: " + comment1 + " -->\n" +
		"\t\t- reply [author:: Bob] [ts:: 2026-07-07T10:01:00] <!-- id: " + replyID + " -->\n" +
		"\t- prose note <!-- id: " + proseID + " -->\n" +
		"\t- second comment [author:: Eve] [ts:: 2026-07-07T10:02:00] <!-- id: " + comment2 + " -->\n"
	indexTestFile(t, app, "W", "S", "SubtreeIdempotent", "2026-07-01", content)

	children := []parser.ParsedBlock{
		{ID: proseID, Type: parser.BlockNote, Depth: 0, CleanText: "prose note"},
	}
	if _, err := app.SaveSubtreeBlocks(taskID, children); err != nil {
		t.Fatalf("first SaveSubtreeBlocks: %v", err)
	}
	first, err := app.FetchSubtree(taskID)
	if err != nil {
		t.Fatalf("FetchSubtree after first save: %v", err)
	}
	var firstOrder []string
	for _, b := range first {
		firstOrder = append(firstOrder, b.ID)
	}

	// The same payload again (the editor re-opened and saved without edits).
	if _, err := app.SaveSubtreeBlocks(taskID, children); err != nil {
		t.Fatalf("second SaveSubtreeBlocks: %v", err)
	}
	second, err := app.FetchSubtree(taskID)
	if err != nil {
		t.Fatalf("FetchSubtree after second save: %v", err)
	}
	var secondOrder []string
	for _, b := range second {
		secondOrder = append(secondOrder, b.ID)
	}
	if len(firstOrder) != 4 || len(secondOrder) != 4 {
		t.Fatalf("subtree sizes after saves = %d / %d, want 4 each: %v / %v",
			len(firstOrder), len(secondOrder), firstOrder, secondOrder)
	}
	for i := range firstOrder {
		if firstOrder[i] != secondOrder[i] {
			t.Fatalf("comment tail drifted across identical saves: %v → %v", firstOrder, secondOrder)
		}
	}

	// No duplicate UUIDs on disk (a duplicated id breaks the blocks PK).
	s := mustReadTaskFile(t, app, "W", "S", "SubtreeIdempotent")
	for _, id := range []string{taskID, comment1, replyID, comment2, proseID} {
		if n := strings.Count(s, id); n != 1 {
			t.Errorf("id %s occurs %d times on disk, want exactly 1:\n%s", id, n, s)
		}
	}
}

// TestSaveSubtreeBlocks_CallerSuppliedCommentIsNotDuplicated pins the
// documented SDK round-trip (fetchSubtree → saveSubtreeBlocks): a payload that
// still carries this task's comment block must save successfully, with
// preservation re-adding only what the payload omitted. Otherwise the caller's
// own block collides with this rule's injection and the whole save is refused.
func TestSaveSubtreeBlocks_CallerSuppliedCommentIsNotDuplicated(t *testing.T) {
	app := newTestApp(t)
	const (
		taskID    = "ad000061-5555-1111-1111-111111111111"
		commentID = "ad000062-5555-1111-1111-111111111111"
		replyID   = "ad000063-5555-1111-1111-111111111111"
		proseID   = "ad000064-5555-1111-1111-111111111111"
		subTaskID = "ad000065-5555-1111-1111-111111111111"
	)
	content := "- [ ] parent task <!-- id: " + taskID + " -->\n" +
		"\t- task comment [author:: Dana] [ts:: 2026-07-07T10:00:00] <!-- id: " + commentID + " -->\n" +
		"\t\t- reply to Dana [author:: Bob] [ts:: 2026-07-07T10:01:00] <!-- id: " + replyID + " -->\n" +
		"\t- prose note <!-- id: " + proseID + " -->\n" +
		"\t- [ ] sub-task <!-- id: " + subTaskID + " -->\n"
	indexTestFile(t, app, "W", "S", "SubtreeCallerComment", "2026-07-01", content)

	// Round-trip payload: the caller serialized the comment block it hydrated
	// back into the document, but dropped the reply.
	children := []parser.ParsedBlock{
		{ID: proseID, Type: parser.BlockNote, Depth: 0, CleanText: "prose note"},
		{ID: commentID, Type: parser.BlockNote, Depth: 0, CleanText: "task comment", Author: "Dana", Timestamp: "2026-07-07T10:00:00"},
		{ID: subTaskID, Type: parser.BlockTask, Depth: 0, Status: "TODO", CleanText: "sub-task"},
	}
	if ok, err := app.SaveSubtreeBlocks(taskID, children); err != nil || !ok {
		t.Fatalf("SaveSubtreeBlocks with a caller-supplied comment: ok=%v err=%v", ok, err)
	}

	sub, err := app.FetchSubtree(taskID)
	if err != nil {
		t.Fatalf("FetchSubtree: %v", err)
	}
	var order []string
	for _, b := range sub {
		order = append(order, b.ID)
	}
	want := []string{proseID, commentID, subTaskID, replyID}
	if len(order) != len(want) {
		t.Fatalf("subtree = %v, want %v (caller order, then the omitted reply)", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("subtree order = %v, want %v", order, want)
		}
	}

	comment := findSubtreeBlock(t, sub, commentID)
	if comment == nil {
		t.Fatal("caller-supplied comment missing from the sub-tree")
	}
	if comment.CleanText != "task comment" || comment.Author != "Dana" || comment.Timestamp != "2026-07-07T10:00:00" {
		t.Errorf("comment attribution drifted: text=%q author=%q ts=%q",
			comment.CleanText, comment.Author, comment.Timestamp)
	}

	// No duplicate UUIDs on disk: the caller's block is written once, and the
	// omitted reply is still preserved.
	s := mustReadTaskFile(t, app, "W", "S", "SubtreeCallerComment")
	for _, id := range []string{taskID, commentID, replyID, proseID, subTaskID} {
		if n := strings.Count(s, id); n != 1 {
			t.Errorf("id %s occurs %d times on disk, want exactly 1:\n%s", id, n, s)
		}
	}
	for _, token := range []string{"[author:: Dana]", "[ts:: 2026-07-07T10:00:00]", "[author:: Bob]"} {
		if !strings.Contains(s, token) {
			t.Errorf("on-disk comment lost %q:\n%s", token, s)
		}
	}
	if got := taskCommentsCount(t, app, taskID); got != 2 {
		t.Errorf("comments_count = %d, want 2 (comment + reply)", got)
	}
}

// TestSaveSubtreeBlocks_ProseNotesStayEditable pins that the preserve rule is
// attribution-based: unattributed NOTE prose is still saved, edited and deleted
// normally, while a comment beside it survives.
func TestSaveSubtreeBlocks_ProseNotesStayEditable(t *testing.T) {
	app := newTestApp(t)
	const (
		taskID    = "ad000041-5555-1111-1111-111111111111"
		proseID   = "ad000042-5555-1111-1111-111111111111"
		commentID = "ad000043-5555-1111-1111-111111111111"
	)
	content := "- [ ] parent task <!-- id: " + taskID + " -->\n" +
		"\t- prose note <!-- id: " + proseID + " -->\n" +
		"\t- comment [author:: Dana] [ts:: 2026-07-07T10:00:00] <!-- id: " + commentID + " -->\n"
	indexTestFile(t, app, "W", "S", "SubtreeProseEdit", "2026-07-01", content)

	edited := []parser.ParsedBlock{
		{ID: proseID, Type: parser.BlockNote, Depth: 0, CleanText: "edited prose"},
	}
	if _, err := app.SaveSubtreeBlocks(taskID, edited); err != nil {
		t.Fatalf("SaveSubtreeBlocks (edit): %v", err)
	}
	sub, err := app.FetchSubtree(taskID)
	if err != nil {
		t.Fatalf("FetchSubtree after edit: %v", err)
	}
	prose := findSubtreeBlock(t, sub, proseID)
	if prose == nil || prose.CleanText != "edited prose" {
		t.Fatalf("prose note not edited: %+v", prose)
	}
	if findSubtreeBlock(t, sub, commentID) == nil {
		t.Fatal("comment dropped while editing prose")
	}

	// Deleting the prose block must not delete the comment.
	if _, err := app.SaveSubtreeBlocks(taskID, nil); err != nil {
		t.Fatalf("SaveSubtreeBlocks (delete): %v", err)
	}
	sub, err = app.FetchSubtree(taskID)
	if err != nil {
		t.Fatalf("FetchSubtree after delete: %v", err)
	}
	if len(sub) != 1 || sub[0].ID != commentID {
		t.Fatalf("subtree after deleting prose = %+v, want only the comment", sub)
	}
}

// TestAppendTaskComment_StampsTimestampWhenOmitted pins the server-side
// guarantee for the attribution predicate: an app-written comment is always
// classifiable, so a caller that omits ts still produces a comment rather than
// something indistinguishable from editable prose.
func TestAppendTaskComment_StampsTimestampWhenOmitted(t *testing.T) {
	app := newTestApp(t)
	const taskID = "ad000051-5555-1111-1111-111111111111"
	content := "- [ ] parent task <!-- id: " + taskID + " -->\n"
	indexTestFile(t, app, "W", "S", "CommentNoTS", "2026-07-01", content)

	id, err := app.AppendTaskComment(taskID, "no ts from the caller", "Dana", "", "")
	if err != nil {
		t.Fatalf("AppendTaskComment: %v", err)
	}
	sub, err := app.FetchSubtree(taskID)
	if err != nil {
		t.Fatalf("FetchSubtree: %v", err)
	}
	comment := findSubtreeBlock(t, sub, id)
	if comment == nil {
		t.Fatalf("comment %s missing from sub-tree: %+v", id, sub)
	}
	if strings.TrimSpace(comment.Timestamp) == "" {
		t.Fatal("expected a server-stamped timestamp, got empty")
	}
	if !parser.IsCommentBlock(*comment) {
		t.Errorf("app-written comment is not classifiable by IsCommentBlock: %+v", *comment)
	}
}
