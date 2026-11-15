package main

// Explicit comment marker preservation (#1159)
//
// The [comment::] token classifies a NOTE as a comment without relying on
// [author::]/[ts::]. The page editor's schema cannot carry it (D3), so like
// #1160's attribution merge the marker must survive the editor funnel and the
// conflict replay by id. Behaviour-neutral here: no count or thread rule moves.

import (
	"os"
	"strings"
	"testing"

	"silt/backend/parser"
)

// pageEditorPayloadNoCommentMetadata mirrors what the page editor actually
// sends for a comment: no marker and no attribution. The explicit clear keeps
// the fixture honest if the DB projection ever starts hydrating them.
func pageEditorPayloadNoCommentMetadata(t *testing.T, app *App, page string) []parser.ParsedBlock {
	t.Helper()
	blocks, err := app.FetchPageBlocks(pageAttrNotebook, pageAttrSection, page)
	if err != nil {
		t.Fatalf("FetchPageBlocks %s: %v", page, err)
	}
	for i := range blocks {
		blocks[i].IsComment = false
		blocks[i].Author = ""
		blocks[i].Timestamp = ""
	}
	return blocks
}

func TestAppendTaskComment_WritesExplicitCommentMarker(t *testing.T) {
	app := newTestApp(t)
	const taskID = "ac1c0001-4444-1111-1111-111111111111"
	content := "- [ ] ship the marker <!-- id: " + taskID + " -->\n"
	indexTestFile(t, app, "W", "S", "CommentMarkerAppend", "2026-07-01", content)

	newID, err := app.AppendTaskComment(taskID, "looks good to me", "Dana", "2026-07-07T14:22:00", "")
	if err != nil {
		t.Fatalf("AppendTaskComment: %v", err)
	}

	saved := mustReadTaskFile(t, app, "W", "S", "CommentMarkerAppend")
	marker := strings.Index(saved, "[comment::]")
	author := strings.Index(saved, "[author:: Dana]")
	ts := strings.Index(saved, "[ts:: 2026-07-07T14:22:00]")
	if marker < 0 {
		t.Fatalf("AppendTaskComment did not write [comment::]:\n%s", saved)
	}
	if author < 0 || ts < 0 {
		t.Fatalf("attribution tokens missing from the rendered comment:\n%s", saved)
	}
	if !(marker < author && author < ts) {
		t.Errorf("expected canonical order [comment::] [author::] [ts::], got:\n%s", saved)
	}

	sub, err := app.FetchSubtree(taskID)
	if err != nil {
		t.Fatalf("FetchSubtree: %v", err)
	}
	comment := findSubtreeBlock(t, sub, newID)
	if comment == nil {
		t.Fatalf("new comment %s not in sub-tree: %+v", newID, sub)
	}
	if !comment.IsComment {
		t.Error("expected IsComment=true on the freshly appended comment")
	}
	if !parser.IsCommentBlock(*comment) {
		t.Error("expected the appended comment to classify as a comment")
	}
}

func TestSaveFileBlocks_PreservesCommentMarkerOnUnrelatedEdit(t *testing.T) {
	app := newTestApp(t)
	const (
		taskID    = "ac1d0001-4444-1111-1111-111111111111"
		commentID = "ac1d0002-4444-1111-1111-111111111111"
		noteID    = "ac1d0003-4444-1111-1111-111111111111"
		page      = "CommentMarkerPageSave"
		fileDate  = "2026-07-01"
	)
	content := "- [ ] parent task <!-- id: " + taskID + " -->\n" +
		"\t- marked comment [comment::] [author:: Dana] [ts:: 2026-07-07T10:00:00] <!-- id: " + commentID + " -->\n"
	indexTestFile(t, app, pageAttrNotebook, pageAttrSection, page, fileDate, content)

	payload := pageEditorPayloadNoCommentMetadata(t, app, page)
	// The payload genuinely carries no marker; the merge must restore it.
	for i := range payload {
		if payload[i].ID == commentID && payload[i].IsComment {
			t.Fatal("fixture assumption broken: page fetch already hydrated is_comment")
		}
	}
	payload = append(payload, parser.ParsedBlock{
		ID: noteID, Type: parser.BlockNote, Depth: 0, CleanText: "an unrelated paragraph",
	})
	if err := app.SaveFileBlocks(pageAttrNotebook, pageAttrSection, page, payload); err != nil {
		t.Fatalf("SaveFileBlocks: %v", err)
	}

	saved := mustReadTaskFile(t, app, pageAttrNotebook, pageAttrSection, page)
	for _, token := range []string{"[comment::]", "[author:: Dana]", "[ts:: 2026-07-07T10:00:00]"} {
		if !strings.Contains(saved, token) {
			t.Errorf("page save dropped %q from the comment:\n%s", token, saved)
		}
	}
	if !strings.Contains(saved, "an unrelated paragraph") {
		t.Errorf("the unrelated edit did not land:\n%s", saved)
	}
	disk := savedBlock(t, app, page, fileDate, commentID)
	if disk == nil {
		t.Fatal("comment missing from disk after the page save")
	}
	if !disk.IsComment {
		t.Error("expected IsComment=true on disk after the page save")
	}

	sub, err := app.FetchSubtree(taskID)
	if err != nil {
		t.Fatalf("FetchSubtree: %v", err)
	}
	comment := findSubtreeBlock(t, sub, commentID)
	if comment == nil {
		t.Fatalf("comment missing from the sub-tree after a page save: %+v", sub)
	}
	if !comment.IsComment {
		t.Error("expected IsComment=true in FetchSubtree after the page save")
	}
	if got := taskCommentsCount(t, app, taskID); got != 1 {
		t.Errorf("comments_count = %d, want 1 (the rule must not move in this PR)", got)
	}
}

// TestSaveFileBlocks_RefusedSaveResolvePreservesCommentMarker drives the real
// refusal path: the page is loaded, an external write lands, the editor's save
// is refused (payload stashed), and the resolution replays it. The replay skips
// saveFileBlocksWithGate's merge, so the resolver must carry the marker itself.
func TestSaveFileBlocks_RefusedSaveResolvePreservesCommentMarker(t *testing.T) {
	app := newTestApp(t)
	const (
		taskID    = "ac1e0001-4444-1111-1111-111111111111"
		commentID = "ac1e0002-4444-1111-1111-111111111111"
		extID     = "ac1e0003-4444-1111-1111-111111111111"
		page      = "CommentMarkerResolve"
		fileDate  = "2026-07-01"
	)
	content := "- [ ] parent task <!-- id: " + taskID + " -->\n" +
		"\t- marked comment [comment::] [author:: Dana] [ts:: 2026-07-07T10:00:00] <!-- id: " + commentID + " -->\n"
	indexTestFile(t, app, pageAttrNotebook, pageAttrSection, page, fileDate, content)

	payload := pageEditorPayloadNoCommentMetadata(t, app, page)

	external := content + "- external line <!-- id: " + extID + " -->\n"
	if err := os.WriteFile(conflictPagePath(app, pageAttrNotebook, pageAttrSection, page),
		[]byte(external), 0o644); err != nil {
		t.Fatalf("external write: %v", err)
	}
	requireConflict(t, app.SaveFileBlocks(pageAttrNotebook, pageAttrSection, page, payload))

	if _, err := app.ResolveExternalConflict(pageAttrNotebook, pageAttrSection, page, conflictResolutionKeepMine); err != nil {
		t.Fatalf("ResolveExternalConflict(keep_mine): %v", err)
	}

	saved := mustReadTaskFile(t, app, pageAttrNotebook, pageAttrSection, page)
	for _, token := range []string{"[comment::]", "[author:: Dana]", "[ts:: 2026-07-07T10:00:00]"} {
		if !strings.Contains(saved, token) {
			t.Errorf("conflict replay dropped %q from the comment:\n%s", token, saved)
		}
	}

	sub, err := app.FetchSubtree(taskID)
	if err != nil {
		t.Fatalf("FetchSubtree: %v", err)
	}
	comment := findSubtreeBlock(t, sub, commentID)
	if comment == nil {
		t.Fatalf("comment missing from the sub-tree after the resolution: %+v", sub)
	}
	if !comment.IsComment {
		t.Error("expected IsComment=true after the conflict replay")
	}
	if got := taskCommentsCount(t, app, taskID); got != 1 {
		t.Errorf("comments_count = %d, want 1 (the rule must not move in this PR)", got)
	}
}

// markerOnlyContent is the comment shape a hand-authored (or PR-4) file
// produces: the marker alone classifies the NOTE, with no attribution to fall
// back on. Indexer rows cannot carry it, so both save funnels must restore it
// from disk bytes.
func markerOnlyContent(taskID, commentID string) string {
	return "- [ ] parent task <!-- id: " + taskID + " -->\n" +
		"\t- marked comment [comment::] <!-- id: " + commentID + " -->\n"
}

// assertMarkerOnlyComment checks every surface the marker-only comment must
// keep after a save: rendered bytes, on-disk parse, and the sub-tree fetch.
func assertMarkerOnlyComment(t *testing.T, app *App, taskID, commentID, page, fileDate, saved string) {
	t.Helper()
	if got := strings.Count(saved, "[comment::]"); got != 1 {
		t.Errorf("[comment::] count = %d, want 1:\n%s", got, saved)
	}
	// Marker-only must stay marker-only: nothing may invent attribution.
	for _, token := range []string{"[author::", "[ts::"} {
		if strings.Contains(saved, token) {
			t.Errorf("save invented %q on a marker-only comment:\n%s", token, saved)
		}
	}

	disk := savedBlock(t, app, page, fileDate, commentID)
	if disk == nil {
		t.Fatal("comment missing from disk after the save")
	}
	if !disk.IsComment {
		t.Error("expected IsComment=true on disk after the save")
	}
	if disk.Author != "" || disk.Timestamp != "" {
		t.Errorf("attribution drifted on a marker-only comment: author=%q ts=%q", disk.Author, disk.Timestamp)
	}
	if !parser.IsCommentBlock(*disk) {
		t.Error("expected the on-disk block to classify as a comment")
	}

	sub, err := app.FetchSubtree(taskID)
	if err != nil {
		t.Fatalf("FetchSubtree: %v", err)
	}
	comment := findSubtreeBlock(t, sub, commentID)
	if comment == nil {
		t.Fatalf("comment missing from the sub-tree after the save: %+v", sub)
	}
	if !comment.IsComment {
		t.Error("expected IsComment=true in FetchSubtree after the save")
	}
	if !parser.IsCommentBlock(*comment) {
		t.Error("expected the sub-tree block to classify as a comment")
	}
	if got := taskCommentsCount(t, app, taskID); got != 1 {
		t.Errorf("comments_count = %d, want 1 (the rule must not move in this PR)", got)
	}
}

func TestSaveFileBlocks_PreservesMarkerOnlyCommentOnUnrelatedEdit(t *testing.T) {
	app := newTestApp(t)
	const (
		taskID    = "ac1f0001-4444-1111-1111-111111111111"
		commentID = "ac1f0002-4444-1111-1111-111111111111"
		noteID    = "ac1f0003-4444-1111-1111-111111111111"
		page      = "CommentMarkerOnlyPageSave"
		fileDate  = "2026-07-01"
	)
	indexTestFile(t, app, pageAttrNotebook, pageAttrSection, page, fileDate, markerOnlyContent(taskID, commentID))

	payload := pageEditorPayloadNoCommentMetadata(t, app, page)
	payload = append(payload, parser.ParsedBlock{
		ID: noteID, Type: parser.BlockNote, Depth: 0, CleanText: "an unrelated paragraph",
	})
	if err := app.SaveFileBlocks(pageAttrNotebook, pageAttrSection, page, payload); err != nil {
		t.Fatalf("SaveFileBlocks: %v", err)
	}

	saved := mustReadTaskFile(t, app, pageAttrNotebook, pageAttrSection, page)
	if !strings.Contains(saved, "an unrelated paragraph") {
		t.Errorf("the unrelated edit did not land:\n%s", saved)
	}
	assertMarkerOnlyComment(t, app, taskID, commentID, page, fileDate, saved)
}

// TestSaveFileBlocks_RefusedSaveResolvePreservesMarkerOnlyComment is the
// marker-only twin of the refusal-replay test: the replay skips the save
// funnel's merge, so the resolver alone carries a comment that has no
// attribution to fall back on.
func TestSaveFileBlocks_RefusedSaveResolvePreservesMarkerOnlyComment(t *testing.T) {
	app := newTestApp(t)
	const (
		taskID    = "ac200001-4444-1111-1111-111111111111"
		commentID = "ac200002-4444-1111-1111-111111111111"
		extID     = "ac200003-4444-1111-1111-111111111111"
		page      = "CommentMarkerOnlyResolve"
		fileDate  = "2026-07-01"
	)
	content := markerOnlyContent(taskID, commentID)
	indexTestFile(t, app, pageAttrNotebook, pageAttrSection, page, fileDate, content)

	payload := pageEditorPayloadNoCommentMetadata(t, app, page)

	external := content + "- external line <!-- id: " + extID + " -->\n"
	if err := os.WriteFile(conflictPagePath(app, pageAttrNotebook, pageAttrSection, page),
		[]byte(external), 0o644); err != nil {
		t.Fatalf("external write: %v", err)
	}
	requireConflict(t, app.SaveFileBlocks(pageAttrNotebook, pageAttrSection, page, payload))

	if _, err := app.ResolveExternalConflict(pageAttrNotebook, pageAttrSection, page, conflictResolutionKeepMine); err != nil {
		t.Fatalf("ResolveExternalConflict(keep_mine): %v", err)
	}

	saved := mustReadTaskFile(t, app, pageAttrNotebook, pageAttrSection, page)
	assertMarkerOnlyComment(t, app, taskID, commentID, page, fileDate, saved)
}
