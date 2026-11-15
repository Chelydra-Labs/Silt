package main

// Explicit, reversible NOTE classification (#1159 conversion)
//
// PluginSetBlockCommentClassification is the one canonical mutation behind
// both conversion affordances: prose sub-note → comment (marker only) and
// comment → sub-note (marker + attribution cleared, because [author::]/[ts::]
// is a permanent comment fallback — D4/D7).

import (
	"os"
	"strings"
	"testing"
	"time"

	"silt/backend/parser"
	"silt/backend/plugins"
)

const (
	classifyNotebook = "W"
	classifySection  = "S"
	classifyFileDate = "2026-07-01"

	classTaskID   = "ba100001-1111-4111-8111-111111111111"
	classNoteID   = "ba100002-1111-4111-8111-111111111111"
	classMarkedID = "ba100003-1111-4111-8111-111111111111"
	classLegacyID = "ba100004-1111-4111-8111-111111111111"
)

// classificationPage writes one page holding every shape the mutation has to
// tell apart: a TASK, a prose NOTE with an unrelated extra token, a NOTE with
// the explicit marker + attribution, and a legacy attribution-only comment.
// Returns the file path.
func classificationPage(t *testing.T, app *App, page string) string {
	t.Helper()
	content := "- [ ] parent task <!-- id: " + classTaskID + " -->\n" +
		"\t- prose body [project:: alpha] <!-- id: " + classNoteID + " -->\n" +
		"\t- marked body [comment::] [author:: Dana] [ts:: 2026-07-07T10:00:00] <!-- id: " + classMarkedID + " -->\n" +
		"\t- legacy body [author:: Ash] [ts:: 2026-07-06T09:00:00] <!-- id: " + classLegacyID + " -->\n"
	return indexTestFile(t, app, classifyNotebook, classifySection, page, classifyFileDate, content)
}

func classificationSession(t *testing.T, app *App) string {
	t.Helper()
	tok := registerTestSession(t, app, "silt-tasks")
	if err := app.RequestCapability("silt-tasks", string(plugins.CapContentMutate), ""); err != nil {
		t.Fatalf("grant content-mutate: %v", err)
	}
	return tok
}

func savedPageIDs(t *testing.T, app *App, page string) []string {
	t.Helper()
	var ids []string
	for _, b := range parseSavedPage(t, app, page, classifyFileDate) {
		ids = append(ids, b.ID)
	}
	return ids
}

func wantPageOrder(t *testing.T, app *App, page string) {
	t.Helper()
	got := savedPageIDs(t, app, page)
	want := []string{classTaskID, classNoteID, classMarkedID, classLegacyID}
	if len(got) != len(want) {
		t.Fatalf("page block order changed: got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("page block order changed: got %v, want %v", got, want)
		}
	}
}

// Prose NOTE → comment: only the marker is added. Body, extra tokens,
// neighbours and block order are untouched, and the shared predicate now
// classifies it as a comment.
func TestSetBlockCommentClassification_ProseNoteBecomesComment(t *testing.T) {
	app := newTestApp(t)
	page := "ClassifyToComment"
	classificationPage(t, app, page)
	tok := classificationSession(t, app)

	// The fixture's two attributed comments count; the prose note does not.
	if got := taskCommentsCount(t, app, classTaskID); got != 2 {
		t.Fatalf("pre-conversion comments_count = %d, want 2 (the marked + legacy comments, not the prose note)", got)
	}
	// The conversion emits block:changed, which is what makes the host
	// re-query comments_count and refresh the badge without a reopen.
	emitted := 0
	app.eventEmit = func(name string, data ...any) {
		if name == string(EventBlockChanged) {
			emitted++
		}
	}

	changed, err := app.PluginSetBlockCommentClassification("silt-tasks", tok, classNoteID, true)
	if err != nil {
		t.Fatalf("PluginSetBlockCommentClassification: %v", err)
	}
	if !changed {
		t.Fatal("expected changed=true for a prose note converted to a comment")
	}
	if emitted != 1 {
		t.Errorf("conversion emitted %d block:changed events, want 1 (the badge refresh path)", emitted)
	}
	if got := taskCommentsCount(t, app, classTaskID); got != 3 {
		t.Errorf("post-conversion comments_count = %d, want 3 (the converted note now counts)", got)
	}

	disk := savedBlock(t, app, page, classifyFileDate, classNoteID)
	if disk == nil {
		t.Fatal("converted block missing from disk")
	}
	if !disk.IsComment {
		t.Error("expected the [comment::] marker on disk")
	}
	if disk.Author != "" || disk.Timestamp != "" {
		t.Errorf("conversion must not fabricate attribution: author=%q ts=%q", disk.Author, disk.Timestamp)
	}
	if disk.CleanText != "prose body [project:: alpha]" {
		// NOTE blocks keep unknown [key:: value] text inline (no ExtraTokens
		// path), so "body preserved" includes that token.
		t.Errorf("body changed: %q", disk.CleanText)
	}
	if !parser.IsCommentBlock(*disk) {
		t.Error("converted note does not classify as a comment")
	}

	// The other comment is untouched in both directions of the fixture.
	legacy := savedBlock(t, app, page, classifyFileDate, classLegacyID)
	if legacy == nil || legacy.Author != "Ash" || legacy.Timestamp != "2026-07-06T09:00:00" {
		t.Errorf("neighbouring comment changed: %+v", legacy)
	}
	wantPageOrder(t, app, page)

	sub, err := app.FetchSubtree(classTaskID)
	if err != nil {
		t.Fatalf("FetchSubtree: %v", err)
	}
	fetched := findSubtreeBlock(t, sub, classNoteID)
	if fetched == nil || !fetched.IsComment {
		t.Fatalf("FetchSubtree does not reflect the conversion: %+v", fetched)
	}
}

// Comment → sub-note: the marker AND both attribution tokens are cleared
// (D7). The body and the page order survive; neighbours keep their tokens.
func TestSetBlockCommentClassification_CommentBecomesProse(t *testing.T) {
	app := newTestApp(t)
	page := "ClassifyToProse"
	classificationPage(t, app, page)
	tok := classificationSession(t, app)

	// Both attributed comments count before the reverse conversion.
	if got := taskCommentsCount(t, app, classTaskID); got != 2 {
		t.Fatalf("pre-conversion comments_count = %d, want 2 (the marked + legacy comments)", got)
	}

	changed, err := app.PluginSetBlockCommentClassification("silt-tasks", tok, classMarkedID, false)
	if err != nil {
		t.Fatalf("PluginSetBlockCommentClassification: %v", err)
	}
	if !changed {
		t.Fatal("expected changed=true for a comment converted to a sub-note")
	}
	if got := taskCommentsCount(t, app, classTaskID); got != 1 {
		t.Errorf("post-conversion comments_count = %d, want 1 (the converted comment no longer counts)", got)
	}

	disk := savedBlock(t, app, page, classifyFileDate, classMarkedID)
	if disk == nil {
		t.Fatal("converted block missing from disk")
	}
	if disk.IsComment || disk.Author != "" || disk.Timestamp != "" {
		t.Errorf("reverse conversion must discard marker and attribution: is_comment=%v author=%q ts=%q",
			disk.IsComment, disk.Author, disk.Timestamp)
	}
	if disk.CleanText != "marked body" {
		t.Errorf("body changed: %q", disk.CleanText)
	}
	if parser.IsCommentBlock(*disk) {
		t.Error("converted comment still classifies as a comment")
	}

	saved := mustReadTaskFile(t, app, classifyNotebook, classifySection, page)
	if strings.Contains(saved, "[comment::] [author:: Dana]") {
		t.Errorf("marker/attribution still on disk:\n%s", saved)
	}
	if !strings.Contains(saved, "[author:: Ash] [ts:: 2026-07-06T09:00:00]") {
		t.Errorf("neighbouring legacy comment lost its attribution:\n%s", saved)
	}
	wantPageOrder(t, app, page)

	sub, err := app.FetchSubtree(classTaskID)
	if err != nil {
		t.Fatalf("FetchSubtree: %v", err)
	}
	fetched := findSubtreeBlock(t, sub, classMarkedID)
	if fetched == nil || fetched.IsComment || fetched.Author != "" || fetched.Timestamp != "" {
		t.Fatalf("FetchSubtree does not reflect the reverse conversion: %+v", fetched)
	}
}

// Legacy comments carry no marker; the reverse conversion must still strip
// attribution so the block stops classifying as a comment.
func TestSetBlockCommentClassification_AttributionOnlyCommentBecomesProse(t *testing.T) {
	app := newTestApp(t)
	page := "ClassifyLegacyToProse"
	classificationPage(t, app, page)
	tok := classificationSession(t, app)

	changed, err := app.PluginSetBlockCommentClassification("silt-tasks", tok, classLegacyID, false)
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}

	disk := savedBlock(t, app, page, classifyFileDate, classLegacyID)
	if disk == nil {
		t.Fatal("converted block missing from disk")
	}
	if disk.Author != "" || disk.Timestamp != "" || disk.IsComment {
		t.Errorf("attribution survived: %+v", disk)
	}
	if parser.IsCommentBlock(*disk) {
		t.Error("attribution-only comment still classifies as a comment after conversion")
	}
	// The marked comment is now the only counted one.
	if got := taskCommentsCount(t, app, classTaskID); got != 1 {
		t.Errorf("comments_count = %d, want 1 (only the marked comment remains)", got)
	}
}

// Attribution-only comment + asComment=true is the one shape where the marker
// field and the shared IsCommentBlock predicate disagree: the block already
// classifies as a comment, but the explicit [comment::] marker is still
// missing, so this must be a real write that adds the marker and keeps the
// attribution — not a no-op. Pinned because switching the idempotency check to
// the predicate would silently drop the write (and with it the sub-editor's
// "the block leaves the doc" guarantee).
func TestSetBlockCommentClassification_AttributionOnlyCommentGainsMarker(t *testing.T) {
	app := newTestApp(t)
	page := "ClassifyLegacyToComment"
	path := classificationPage(t, app, page)
	tok := classificationSession(t, app)

	changed, err := app.PluginSetBlockCommentClassification("silt-tasks", tok, classLegacyID, true)
	if err != nil || !changed {
		t.Fatalf("attribution-only + asComment=true: changed=%v err=%v (want true, nil)", changed, err)
	}

	disk := savedBlock(t, app, page, classifyFileDate, classLegacyID)
	if disk == nil {
		t.Fatal("converted block missing from disk")
	}
	if !disk.IsComment {
		t.Error("expected the [comment::] marker on disk")
	}
	if disk.Author != "Ash" || disk.Timestamp != "2026-07-06T09:00:00" {
		t.Errorf("attribution changed: author=%q ts=%q", disk.Author, disk.Timestamp)
	}
	if disk.CleanText != "legacy body" {
		t.Errorf("body changed: %q", disk.CleanText)
	}
	saved := mustReadTaskFile(t, app, classifyNotebook, classifySection, page)
	if !strings.Contains(saved, "[comment::] [author:: Ash] [ts:: 2026-07-06T09:00:00]") {
		t.Errorf("marker not written alongside the surviving attribution:\n%s", saved)
	}
	wantPageOrder(t, app, page)

	// It was a comment by attribution before and after, so the count is stable.
	if got := taskCommentsCount(t, app, classTaskID); got != 2 {
		t.Errorf("comments_count = %d, want 2 (the block already counted; the prose note does not)", got)
	}

	// A repeat is a no-op: the marker is explicit now, so nothing is rewritten.
	sentinel := time.Now().Add(-2 * time.Hour).Truncate(time.Second)
	if err := os.Chtimes(path, sentinel, sentinel); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}
	changed, err = app.PluginSetBlockCommentClassification("silt-tasks", tok, classLegacyID, true)
	if err != nil || changed {
		t.Fatalf("repeat: changed=%v err=%v (want false, nil)", changed, err)
	}
	if after := mustReadTaskFile(t, app, classifyNotebook, classifySection, page); after != saved {
		t.Errorf("repeat rewrote the file:\nbefore:\n%s\nafter:\n%s", saved, after)
	}
	if stat, err := os.Stat(path); err != nil || !stat.ModTime().Equal(sentinel) {
		t.Errorf("repeat touched the file: mtime=%v err=%v want %v", stat, err, sentinel)
	}
}

// Marker-only comment + asComment=true is already a comment by both readings:
// (false, nil) and no write.
func TestSetBlockCommentClassification_MarkedCommentRepeatIsNoOp(t *testing.T) {
	app := newTestApp(t)
	page := "ClassifyMarkedRepeat"
	path := classificationPage(t, app, page)
	tok := classificationSession(t, app)

	sentinel := time.Now().Add(-2 * time.Hour).Truncate(time.Second)
	if err := os.Chtimes(path, sentinel, sentinel); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}
	before := mustReadTaskFile(t, app, classifyNotebook, classifySection, page)

	changed, err := app.PluginSetBlockCommentClassification("silt-tasks", tok, classMarkedID, true)
	if err != nil || changed {
		t.Fatalf("marked comment + asComment=true: changed=%v err=%v (want false, nil)", changed, err)
	}
	if after := mustReadTaskFile(t, app, classifyNotebook, classifySection, page); after != before {
		t.Errorf("no-op rewrote the file:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	if stat, err := os.Stat(path); err != nil || !stat.ModTime().Equal(sentinel) {
		t.Errorf("no-op touched the file: mtime=%v err=%v want %v", stat, err, sentinel)
	}
}

// A repeat is a no-op: (false, nil) and the file is not rewritten (mtime is
// left at the sentinel we set, so a rewrite cannot hide behind coarse
// filesystem timestamps).
func TestSetBlockCommentClassification_IdempotentRepeatDoesNotRewrite(t *testing.T) {
	app := newTestApp(t)
	page := "ClassifyIdempotent"
	path := classificationPage(t, app, page)
	tok := classificationSession(t, app)

	if changed, err := app.PluginSetBlockCommentClassification("silt-tasks", tok, classNoteID, true); err != nil || !changed {
		t.Fatalf("first conversion: changed=%v err=%v", changed, err)
	}
	sentinel := time.Now().Add(-2 * time.Hour).Truncate(time.Second)
	if err := os.Chtimes(path, sentinel, sentinel); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}
	before := mustReadTaskFile(t, app, classifyNotebook, classifySection, page)

	changed, err := app.PluginSetBlockCommentClassification("silt-tasks", tok, classNoteID, true)
	if err != nil || changed {
		t.Fatalf("repeat conversion: changed=%v err=%v (want false, nil)", changed, err)
	}
	if after := mustReadTaskFile(t, app, classifyNotebook, classifySection, page); after != before {
		t.Errorf("idempotent repeat rewrote the file:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	stat, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if !stat.ModTime().Equal(sentinel) {
		t.Errorf("idempotent repeat touched the file: mtime=%v want %v", stat.ModTime(), sentinel)
	}

	// Reverse direction repeats the same way.
	if changed, err := app.PluginSetBlockCommentClassification("silt-tasks", tok, classNoteID, false); err != nil || !changed {
		t.Fatalf("reverse conversion: changed=%v err=%v", changed, err)
	}
	if err := os.Chtimes(path, sentinel, sentinel); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}
	if changed, err := app.PluginSetBlockCommentClassification("silt-tasks", tok, classNoteID, false); err != nil || changed {
		t.Fatalf("reverse repeat: changed=%v err=%v (want false, nil)", changed, err)
	}
	if stat, err := os.Stat(path); err != nil || !stat.ModTime().Equal(sentinel) {
		t.Errorf("reverse repeat touched the file: mtime=%v err=%v want %v", stat.ModTime(), err, sentinel)
	}
}

// taskHasSubnotes reads the derived has_subnotes cache for a task row (the
// presence half of the comment/sub-note derivation).
func taskHasSubnotes(t *testing.T, app *App, taskID string) bool {
	t.Helper()
	var n int
	if err := app.db.SQLDB().QueryRow(
		`SELECT has_subnotes FROM tasks WHERE block_id = ?`, taskID,
	).Scan(&n); err != nil {
		t.Fatalf("query has_subnotes for %s: %v", taskID, err)
	}
	return n != 0
}

// A conversion is a classification move, so the two derived caches must flip in
// opposite directions together: they are the two halves of one predicate read
// in one index pass (AC4). The explicit re-index at the end pins that both
// columns are a pure function of the classification, not of the write path.
func TestSetBlockCommentClassification_FlipsDerivedCachesTogether(t *testing.T) {
	app := newTestApp(t)
	page := "ClassifyFlipBoth"
	classificationPage(t, app, page)
	tok := classificationSession(t, app)

	// Fixture: two comments and one prose note under the task.
	if got, sub := taskCommentsCount(t, app, classTaskID), taskHasSubnotes(t, app, classTaskID); got != 2 || !sub {
		t.Fatalf("pre-conversion comments_count=%d has_subnotes=%v, want 2/true", got, sub)
	}

	// Prose → comment: the block leaves the sub-notes column and joins the
	// comment count.
	if changed, err := app.PluginSetBlockCommentClassification("silt-tasks", tok, classNoteID, true); err != nil || !changed {
		t.Fatalf("convert to comment: changed=%v err=%v", changed, err)
	}
	if got, sub := taskCommentsCount(t, app, classTaskID), taskHasSubnotes(t, app, classTaskID); got != 3 || sub {
		t.Errorf("after convert-to-comment: comments_count=%d has_subnotes=%v, want 3/false", got, sub)
	}

	// Comment → sub-note: the inverse, with neither column left stale.
	if changed, err := app.PluginSetBlockCommentClassification("silt-tasks", tok, classNoteID, false); err != nil || !changed {
		t.Fatalf("convert back to sub-note: changed=%v err=%v", changed, err)
	}
	if got, sub := taskCommentsCount(t, app, classTaskID), taskHasSubnotes(t, app, classTaskID); got != 2 || !sub {
		t.Errorf("after convert-to-sub-note: comments_count=%d has_subnotes=%v, want 2/true", got, sub)
	}

	// Re-index the page as a rescan would: the caches agree with the markdown
	// source of truth rather than with whatever the last write left behind.
	content := mustReadTaskFile(t, app, classifyNotebook, classifySection, page)
	blocks, meta, _, _, err := parser.ParseFileContent(content, classifyNotebook, classifySection, page, classifyFileDate, app.spacesPerTab)
	if err != nil {
		t.Fatalf("ParseFileContent: %v", err)
	}
	if err := app.db.IndexFileBlocks("vault", meta.Notebook, meta.Section, meta.Page, blocks, meta.Tags); err != nil {
		t.Fatalf("IndexFileBlocks: %v", err)
	}
	if got, sub := taskCommentsCount(t, app, classTaskID), taskHasSubnotes(t, app, classTaskID); got != 2 || !sub {
		t.Errorf("after re-index: comments_count=%d has_subnotes=%v, want 2/true", got, sub)
	}
}

func TestSetBlockCommentClassification_RejectsNonNote(t *testing.T) {
	app := newTestApp(t)
	page := "ClassifyNonNote"
	path := classificationPage(t, app, page)
	tok := classificationSession(t, app)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	changed, err := app.PluginSetBlockCommentClassification("silt-tasks", tok, classTaskID, true)
	if err == nil {
		t.Fatal("expected an error for a TASK target")
	}
	if changed {
		t.Error("a rejected target must report changed=false")
	}
	if after, _ := os.ReadFile(path); string(after) != string(before) {
		t.Error("a rejected target still rewrote the file")
	}
}

func TestSetBlockCommentClassification_UnknownID(t *testing.T) {
	app := newTestApp(t)
	classificationPage(t, app, "ClassifyUnknownID")
	tok := classificationSession(t, app)

	changed, err := app.PluginSetBlockCommentClassification("silt-tasks", tok, "00000000-0000-4000-8000-000000000000", true)
	if err == nil {
		t.Fatal("expected an error for an unknown block id")
	}
	if changed {
		t.Error("an unknown id must report changed=false")
	}
}

func TestSetBlockCommentClassification_RequiresContentMutate(t *testing.T) {
	app := newTestApp(t)
	page := "ClassifyNoGrant"
	path := classificationPage(t, app, page)
	tok := registerTestSession(t, app, "third-party")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if changed, err := app.PluginSetBlockCommentClassification("third-party", tok, classNoteID, true); err == nil || changed {
		t.Fatalf("expected capability denial, got changed=%v err=%v", changed, err)
	}
	if after, _ := os.ReadFile(path); string(after) != string(before) {
		t.Error("a denied conversion still rewrote the file")
	}
}

// The marker written by the conversion must survive a following page-editor
// save — the payload carries no comment metadata, so preservation is the only
// thing keeping the block in the thread.
func TestSetBlockCommentClassification_MarkerSurvivesFollowingPageSave(t *testing.T) {
	app := newTestApp(t)
	page := "ClassifySurvivesSave"
	classificationPage(t, app, page)
	tok := classificationSession(t, app)

	if changed, err := app.PluginSetBlockCommentClassification("silt-tasks", tok, classNoteID, true); err != nil || !changed {
		t.Fatalf("conversion: changed=%v err=%v", changed, err)
	}

	payload := pageEditorPayloadNoCommentMetadata(t, app, page)
	payload = append(payload, parser.ParsedBlock{
		ID:   "ba100099-1111-4111-8111-111111111111",
		Type: parser.BlockNote, Depth: 0, CleanText: "an unrelated paragraph",
	})
	if err := app.SaveFileBlocks(classifyNotebook, classifySection, page, payload); err != nil {
		t.Fatalf("SaveFileBlocks: %v", err)
	}

	saved := mustReadTaskFile(t, app, classifyNotebook, classifySection, page)
	if !strings.Contains(saved, "[comment::]") {
		t.Errorf("page save dropped the conversion's marker:\n%s", saved)
	}
	if !strings.Contains(saved, "an unrelated paragraph") {
		t.Errorf("the unrelated edit did not land:\n%s", saved)
	}
	disk := savedBlock(t, app, page, classifyFileDate, classNoteID)
	if disk == nil || !disk.IsComment {
		t.Errorf("converted block lost its classification: %+v", disk)
	}
}
