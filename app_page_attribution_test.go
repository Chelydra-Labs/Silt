package main

// Page-save comment attribution (#1160)
//
// The whole-page editor's schema carries no comment attribution (page fetch →
// TipTap doc → docToBlocks emits neither [author::] nor [ts::]), so its save
// payload rewrites every comment as unattributed prose. The fix is a
// server-side by-id merge on the editor funnel (D2) — not new node attrs —
// since a page save is the whole page's source of truth and the on-disk block
// with the same id is the only proof that a payload block IS that comment.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"silt/backend/parser"
)

const (
	pageAttrNotebook = "W"
	pageAttrSection  = "S"
)

// pageEditorPayload mirrors what the page editor actually sends: every block
// on the page with comment attribution zeroed. Neither the DB projection this
// fetch uses nor the editor schema round-trips the tokens, so the explicit
// clear keeps the fixture honest if the projection ever starts hydrating them.
func pageEditorPayload(t *testing.T, app *App, page string) []parser.ParsedBlock {
	t.Helper()
	blocks, err := app.FetchPageBlocks(pageAttrNotebook, pageAttrSection, page)
	if err != nil {
		t.Fatalf("FetchPageBlocks %s: %v", page, err)
	}
	for i := range blocks {
		blocks[i].Author = ""
		blocks[i].Timestamp = ""
	}
	return blocks
}

// parseSavedPage re-parses the page file as written, independent of the index.
func parseSavedPage(t *testing.T, app *App, page, fileDate string) []parser.ParsedBlock {
	t.Helper()
	content := mustReadTaskFile(t, app, pageAttrNotebook, pageAttrSection, page)
	blocks, _, _, _, err := parser.ParseFileContent(content, pageAttrNotebook, pageAttrSection, page, fileDate, app.spacesPerTab)
	if err != nil {
		t.Fatalf("re-parse %s: %v", page, err)
	}
	return blocks
}

// savedBlock returns the re-parsed on-disk block with id, or nil.
func savedBlock(t *testing.T, app *App, page, fileDate, id string) *parser.ParsedBlock {
	t.Helper()
	blocks := parseSavedPage(t, app, page, fileDate)
	for i := range blocks {
		if blocks[i].ID == id {
			return &blocks[i]
		}
	}
	return nil
}

func TestSaveFileBlocks_PreservesCommentAttributionOnUnrelatedEdit(t *testing.T) {
	app := newTestApp(t)
	const (
		taskID    = "ae100001-6666-1111-1111-111111111111"
		commentID = "ae100002-6666-1111-1111-111111111111"
		noteID    = "ae100003-6666-1111-1111-111111111111"
		page      = "PageAttributionUnrelated"
		fileDate  = "2026-07-01"
	)
	content := "- [ ] parent task <!-- id: " + taskID + " -->\n" +
		"\t- looks good [author:: Dana] [ts:: 2026-07-07T10:00:00] <!-- id: " + commentID + " -->\n"
	indexTestFile(t, app, pageAttrNotebook, pageAttrSection, page, fileDate, content)

	payload := pageEditorPayload(t, app, page)
	payload = append(payload, parser.ParsedBlock{
		ID: noteID, Type: parser.BlockNote, Depth: 0, CleanText: "an unrelated paragraph",
	})
	if err := app.SaveFileBlocks(pageAttrNotebook, pageAttrSection, page, payload); err != nil {
		t.Fatalf("SaveFileBlocks: %v", err)
	}

	saved := mustReadTaskFile(t, app, pageAttrNotebook, pageAttrSection, page)
	for _, token := range []string{"[author:: Dana]", "[ts:: 2026-07-07T10:00:00]"} {
		if !strings.Contains(saved, token) {
			t.Errorf("page save dropped %q from the comment:\n%s", token, saved)
		}
	}
	if !strings.Contains(saved, "an unrelated paragraph") {
		t.Errorf("the unrelated edit did not land:\n%s", saved)
	}

	sub, err := app.FetchSubtree(taskID)
	if err != nil {
		t.Fatalf("FetchSubtree: %v", err)
	}
	comment := findSubtreeBlock(t, sub, commentID)
	if comment == nil {
		t.Fatalf("comment missing from the sub-tree after a page save: %+v", sub)
	}
	if comment.Author != "Dana" || comment.Timestamp != "2026-07-07T10:00:00" {
		t.Errorf("comment attribution drifted on FetchSubtree: author=%q ts=%q",
			comment.Author, comment.Timestamp)
	}
	if got := taskCommentsCount(t, app, taskID); got != 1 {
		t.Errorf("comments_count = %d, want 1", got)
	}
}

func TestSaveFileBlocks_EditedCommentBodyKeepsAttribution(t *testing.T) {
	app := newTestApp(t)
	const (
		taskID    = "ae110001-6666-1111-1111-111111111111"
		commentID = "ae110002-6666-1111-1111-111111111111"
		page      = "PageAttributionBodyEdit"
		fileDate  = "2026-07-01"
	)
	content := "- [ ] parent task <!-- id: " + taskID + " -->\n" +
		"\t- original text [author:: Dana] [ts:: 2026-07-07T10:00:00] <!-- id: " + commentID + " -->\n"
	indexTestFile(t, app, pageAttrNotebook, pageAttrSection, page, fileDate, content)

	payload := pageEditorPayload(t, app, page)
	edited := false
	for i := range payload {
		if payload[i].ID == commentID {
			payload[i].CleanText = "edited through the page editor"
			edited = true
		}
	}
	if !edited {
		t.Fatal("comment block missing from the page payload fixture")
	}
	if err := app.SaveFileBlocks(pageAttrNotebook, pageAttrSection, page, payload); err != nil {
		t.Fatalf("SaveFileBlocks: %v", err)
	}

	block := savedBlock(t, app, page, fileDate, commentID)
	if block == nil {
		t.Fatal("edited comment missing from disk")
	}
	if block.CleanText != "edited through the page editor" {
		t.Errorf("body edit lost: %q", block.CleanText)
	}
	if block.Author != "Dana" || block.Timestamp != "2026-07-07T10:00:00" {
		t.Errorf("body edit dropped attribution: author=%q ts=%q", block.Author, block.Timestamp)
	}

	sub, err := app.FetchSubtree(taskID)
	if err != nil {
		t.Fatalf("FetchSubtree: %v", err)
	}
	comment := findSubtreeBlock(t, sub, commentID)
	if comment == nil {
		t.Fatal("edited comment missing from the sub-tree")
	}
	if comment.CleanText != "edited through the page editor" ||
		comment.Author != "Dana" || comment.Timestamp != "2026-07-07T10:00:00" {
		t.Errorf("sub-tree view drifted: text=%q author=%q ts=%q",
			comment.CleanText, comment.Author, comment.Timestamp)
	}
}

func TestSaveFileBlocks_NewNoteStaysUnattributed(t *testing.T) {
	app := newTestApp(t)
	const (
		taskID    = "ae120001-6666-1111-1111-111111111111"
		commentID = "ae120002-6666-1111-1111-111111111111"
		newNoteID = "ae120003-6666-1111-1111-111111111111"
		page      = "PageAttributionNewNote"
		fileDate  = "2026-07-01"
	)
	content := "- [ ] parent task <!-- id: " + taskID + " -->\n" +
		"\t- existing comment [author:: Dana] [ts:: 2026-07-07T10:00:00] <!-- id: " + commentID + " -->\n"
	indexTestFile(t, app, pageAttrNotebook, pageAttrSection, page, fileDate, content)

	payload := pageEditorPayload(t, app, page)
	payload = append(payload, parser.ParsedBlock{
		ID: newNoteID, Type: parser.BlockNote, Depth: 1, CleanText: "typed into the page",
	})
	if err := app.SaveFileBlocks(pageAttrNotebook, pageAttrSection, page, payload); err != nil {
		t.Fatalf("SaveFileBlocks: %v", err)
	}

	fresh := savedBlock(t, app, page, fileDate, newNoteID)
	if fresh == nil {
		t.Fatal("new note missing from disk")
	}
	if fresh.Author != "" || fresh.Timestamp != "" {
		t.Errorf("new note grew attribution: author=%q ts=%q", fresh.Author, fresh.Timestamp)
	}
	// The pre-existing comment is unaffected by the new sibling.
	if old := savedBlock(t, app, page, fileDate, commentID); old == nil ||
		old.Author != "Dana" || old.Timestamp != "2026-07-07T10:00:00" {
		t.Errorf("existing comment attribution drifted: %+v", old)
	}
	// The typed paragraph is a prose sub-note, so the count stays at the one
	// attributed comment (#1159).
	if got := taskCommentsCount(t, app, taskID); got != 1 {
		t.Errorf("comments_count = %d, want 1 (the existing comment; the new prose note is a sub-note)", got)
	}
}

// TestSaveFileBlocks_OmittingCommentDeletesIt pins the page-path semantics: the
// payload is the whole page, so a block absent from it is a deletion and the
// attribution merge must not resurrect it. The sub-notes editor is the
// opposite contract — its payload is only the non-comment blocks, so its save
// re-inserts comments (preserveTaskComments); both halves are asserted here.
func TestSaveFileBlocks_OmittingCommentDeletesIt(t *testing.T) {
	app := newTestApp(t)
	const (
		taskID    = "ae130001-6666-1111-1111-111111111111"
		proseID   = "ae130002-6666-1111-1111-111111111111"
		commentID = "ae130003-6666-1111-1111-111111111111"
		page      = "PageAttributionDelete"
		fileDate  = "2026-07-01"
	)
	content := "- [ ] parent task <!-- id: " + taskID + " -->\n" +
		"\t- prose note <!-- id: " + proseID + " -->\n" +
		"\t- comment to delete [author:: Dana] [ts:: 2026-07-07T10:00:00] <!-- id: " + commentID + " -->\n"
	indexTestFile(t, app, pageAttrNotebook, pageAttrSection, page, fileDate, content)

	payload := pageEditorPayload(t, app, page)
	kept := payload[:0]
	for _, b := range payload {
		if b.ID != commentID {
			kept = append(kept, b)
		}
	}
	if err := app.SaveFileBlocks(pageAttrNotebook, pageAttrSection, page, kept); err != nil {
		t.Fatalf("SaveFileBlocks: %v", err)
	}

	saved := mustReadTaskFile(t, app, pageAttrNotebook, pageAttrSection, page)
	if strings.Contains(saved, commentID) || strings.Contains(saved, "[author:: Dana]") {
		t.Errorf("page payload omitting the comment resurrected it:\n%s", saved)
	}
	sub, err := app.FetchSubtree(taskID)
	if err != nil {
		t.Fatalf("FetchSubtree: %v", err)
	}
	if findSubtreeBlock(t, sub, commentID) != nil {
		t.Errorf("deleted comment still in the sub-tree: %+v", sub)
	}
	// The prose note remains on disk but is not a comment, so the count is 0.
	if got := taskCommentsCount(t, app, taskID); got != 0 {
		t.Errorf("comments_count = %d, want 0 (the remaining note is prose, not a comment)", got)
	}

	// Contrast — the sub-notes editor sends only its non-comment blocks, so
	// the same omission there preserves an on-disk comment.
	newID, err := app.AppendTaskComment(taskID, "comment added later", "Dana", "2026-07-07T11:00:00", "")
	if err != nil {
		t.Fatalf("AppendTaskComment: %v", err)
	}
	if _, err := app.SaveSubtreeBlocks(taskID, []parser.ParsedBlock{
		{ID: proseID, Type: parser.BlockNote, Depth: 0, CleanText: "prose note"},
	}); err != nil {
		t.Fatalf("SaveSubtreeBlocks: %v", err)
	}
	if block := savedBlock(t, app, page, fileDate, newID); block == nil ||
		block.Author != "Dana" || block.Timestamp != "2026-07-07T11:00:00" {
		t.Errorf("sub-editor save dropped an omitted comment (contrast broken): %+v", block)
	}
}

func TestSaveFileBlocks_SuppliedAttributionWins(t *testing.T) {
	app := newTestApp(t)
	const (
		taskID    = "ae140001-6666-1111-1111-111111111111"
		commentID = "ae140002-6666-1111-1111-111111111111"
		page      = "PageAttributionSupplied"
		fileDate  = "2026-07-01"
	)
	content := "- [ ] parent task <!-- id: " + taskID + " -->\n" +
		"\t- comment [author:: Dana] [ts:: 2026-07-07T10:00:00] <!-- id: " + commentID + " -->\n"
	indexTestFile(t, app, pageAttrNotebook, pageAttrSection, page, fileDate, content)

	// A caller that carries attribution (plugin block ops build their payload
	// from on-disk rows) is authoritative — the merge only fills empty fields.
	payload := pageEditorPayload(t, app, page)
	supplied := false
	for i := range payload {
		if payload[i].ID == commentID {
			payload[i].Author = "Zoe"
			payload[i].Timestamp = "2026-08-01T09:00:00"
			supplied = true
		}
	}
	if !supplied {
		t.Fatal("comment block missing from the page payload fixture")
	}
	if err := app.SaveFileBlocks(pageAttrNotebook, pageAttrSection, page, payload); err != nil {
		t.Fatalf("SaveFileBlocks: %v", err)
	}

	saved := mustReadTaskFile(t, app, pageAttrNotebook, pageAttrSection, page)
	if !strings.Contains(saved, "[author:: Zoe]") || !strings.Contains(saved, "[ts:: 2026-08-01T09:00:00]") {
		t.Errorf("supplied attribution not written:\n%s", saved)
	}
	if strings.Contains(saved, "Dana") || strings.Contains(saved, "2026-07-07T10:00:00") {
		t.Errorf("merge overwrote caller-supplied attribution:\n%s", saved)
	}
	block := savedBlock(t, app, page, fileDate, commentID)
	if block == nil || block.Author != "Zoe" || block.Timestamp != "2026-08-01T09:00:00" {
		t.Errorf("on-disk attribution = %+v, want Zoe / 2026-08-01T09:00:00", block)
	}
}

func TestSaveFileBlocks_ConsecutiveSavesAreStable(t *testing.T) {
	app := newTestApp(t)
	const (
		taskID    = "ae150001-6666-1111-1111-111111111111"
		commentID = "ae150002-6666-1111-1111-111111111111"
		noteID    = "ae150003-6666-1111-1111-111111111111"
		page      = "PageAttributionStable"
		fileDate  = "2026-07-01"
	)
	content := "- [ ] parent task <!-- id: " + taskID + " -->\n" +
		"\t- comment [author:: Dana] [ts:: 2026-07-07T10:00:00] <!-- id: " + commentID + " -->\n"
	indexTestFile(t, app, pageAttrNotebook, pageAttrSection, page, fileDate, content)

	save := func() string {
		t.Helper()
		payload := pageEditorPayload(t, app, page)
		// First save adds the paragraph; the second one reloads the page, so
		// the paragraph is already in the payload — the same save, twice.
		hasNote := false
		for i := range payload {
			if payload[i].ID == noteID {
				hasNote = true
			}
		}
		if !hasNote {
			payload = append(payload, parser.ParsedBlock{
				ID: noteID, Type: parser.BlockNote, Depth: 0, CleanText: "trailing prose", FileDate: fileDate,
			})
		}
		if err := app.SaveFileBlocks(pageAttrNotebook, pageAttrSection, page, payload); err != nil {
			t.Fatalf("SaveFileBlocks: %v", err)
		}
		return mustReadTaskFile(t, app, pageAttrNotebook, pageAttrSection, page)
	}

	first := save()
	second := save()
	if first != second {
		t.Fatalf("second identical save changed the file:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
	if n := strings.Count(second, "[author:: Dana]"); n != 1 {
		t.Errorf("[author::] token appears %d times, want 1:\n%s", n, second)
	}
	if n := strings.Count(second, "[ts:: 2026-07-07T10:00:00]"); n != 1 {
		t.Errorf("[ts::] token appears %d times, want 1:\n%s", n, second)
	}

	// Ids and order are untouched by the merge.
	ids := func() []string {
		var out []string
		for _, b := range parseSavedPage(t, app, page, fileDate) {
			out = append(out, b.ID)
		}
		return out
	}
	got := ids()
	want := []string{taskID, commentID, noteID}
	if len(got) != len(want) {
		t.Fatalf("block ids = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("block order drifted: %v, want %v", got, want)
		}
	}
}

// TestSaveFileBlocks_RefusedSaveResolvePreservesCommentAttribution drives the
// real refusal path: the page is loaded, an external write lands, the editor's
// whole-page save is refused (payload stashed), and the resolution replays it.
// The replay skips saveFileBlocksWithGate's merge, so the resolver must merge
// from the on-disk version itself (#1160).
func TestSaveFileBlocks_RefusedSaveResolvePreservesCommentAttribution(t *testing.T) {
	app := newTestApp(t)
	const (
		taskID    = "ae160001-6666-1111-1111-111111111111"
		commentID = "ae160002-6666-1111-1111-111111111111"
		extID     = "ae160003-6666-1111-1111-111111111111"
		page      = "PageAttributionResolve"
		fileDate  = "2026-07-01"
	)
	content := "- [ ] parent task <!-- id: " + taskID + " -->\n" +
		"\t- looks good [author:: Dana] [ts:: 2026-07-07T10:00:00] <!-- id: " + commentID + " -->\n"
	indexTestFile(t, app, pageAttrNotebook, pageAttrSection, page, fileDate, content)

	// Fetching the page captures the load baseline the gate compares against.
	payload := pageEditorPayload(t, app, page)

	// An external tool appends a line while the editor holds its buffer.
	external := content + "- external line <!-- id: " + extID + " -->\n"
	if err := os.WriteFile(conflictPagePath(app, pageAttrNotebook, pageAttrSection, page),
		[]byte(external), 0o644); err != nil {
		t.Fatalf("external write: %v", err)
	}
	requireConflict(t, app.SaveFileBlocks(pageAttrNotebook, pageAttrSection, page, payload))

	copyPage, err := app.ResolveExternalConflict(pageAttrNotebook, pageAttrSection, page, conflictResolutionKeepMine)
	if err != nil {
		t.Fatalf("ResolveExternalConflict(keep_mine): %v", err)
	}
	if copyPage == "" {
		t.Fatal("resolution must report the conflict copy it created")
	}

	saved := mustReadTaskFile(t, app, pageAttrNotebook, pageAttrSection, page)
	for _, token := range []string{"[author:: Dana]", "[ts:: 2026-07-07T10:00:00]"} {
		if !strings.Contains(saved, token) {
			t.Errorf("conflict replay dropped %q from the comment:\n%s", token, saved)
		}
	}
	if strings.Contains(saved, extID) {
		t.Errorf("keep_mine must land the editor payload, not the external line:\n%s", saved)
	}
	// The external version survives in the conflict copy.
	sibling := findConflictSibling(t, app, pageAttrNotebook, pageAttrSection, page)
	sibBytes, err := os.ReadFile(sibling)
	if err != nil {
		t.Fatalf("read sibling: %v", err)
	}
	if !strings.Contains(string(sibBytes), extID) {
		t.Errorf("conflict copy must carry the external version:\n%s", sibBytes)
	}

	sub, err := app.FetchSubtree(taskID)
	if err != nil {
		t.Fatalf("FetchSubtree: %v", err)
	}
	comment := findSubtreeBlock(t, sub, commentID)
	if comment == nil {
		t.Fatalf("comment missing from the sub-tree after the resolution: %+v", sub)
	}
	if comment.Author != "Dana" || comment.Timestamp != "2026-07-07T10:00:00" {
		t.Errorf("comment attribution drifted on FetchSubtree: author=%q ts=%q",
			comment.Author, comment.Timestamp)
	}
	if got := taskCommentsCount(t, app, taskID); got != 1 {
		t.Errorf("comments_count = %d, want 1", got)
	}
}

// TestSaveFileBlocks_RefusedSaveResolveKeepTheirsCopiesMine covers the other
// consumer of the resolved payload: keep_theirs settles theirs on the original
// path and writes my stashed buffer to the conflict copy. That copy is the
// user's surviving version, so it must carry the attribution the replay
// reconstructed from disk (#1160).
func TestSaveFileBlocks_RefusedSaveResolveKeepTheirsCopiesMine(t *testing.T) {
	app := newTestApp(t)
	const (
		taskID    = "ae180001-6666-1111-1111-111111111111"
		commentID = "ae180002-6666-1111-1111-111111111111"
		extID     = "ae180003-6666-1111-1111-111111111111"
		page      = "PageAttributionKeepTheirs"
		fileDate  = "2026-07-01"
	)
	content := "- [ ] parent task <!-- id: " + taskID + " -->\n" +
		"\t- looks good [author:: Dana] [ts:: 2026-07-07T10:00:00] <!-- id: " + commentID + " -->\n"
	indexTestFile(t, app, pageAttrNotebook, pageAttrSection, page, fileDate, content)

	// Fetching the page captures the load baseline the gate compares against.
	payload := pageEditorPayload(t, app, page)
	// My buffer's edit makes the two versions distinguishable: the copy must
	// carry mine, the original path must still carry theirs.
	edited := false
	for i := range payload {
		if payload[i].ID == commentID {
			payload[i].CleanText = "edited through the page editor"
			edited = true
		}
	}
	if !edited {
		t.Fatal("comment block missing from the page payload fixture")
	}

	// An external tool appends a line while the editor holds its buffer.
	external := content + "- external line <!-- id: " + extID + " -->\n"
	if err := os.WriteFile(conflictPagePath(app, pageAttrNotebook, pageAttrSection, page),
		[]byte(external), 0o644); err != nil {
		t.Fatalf("external write: %v", err)
	}
	requireConflict(t, app.SaveFileBlocks(pageAttrNotebook, pageAttrSection, page, payload))

	copyPage, err := app.ResolveExternalConflict(pageAttrNotebook, pageAttrSection, page, conflictResolutionKeepTheirs)
	if err != nil {
		t.Fatalf("ResolveExternalConflict(keep_theirs): %v", err)
	}
	sibling := findConflictSibling(t, app, pageAttrNotebook, pageAttrSection, page)
	if want := copyPage + ".md"; filepath.Base(sibling) != want {
		t.Fatalf("resolution reported copy %q, sibling on disk is %q", copyPage, sibling)
	}

	// The original path keeps theirs, tokens included.
	original := mustReadTaskFile(t, app, pageAttrNotebook, pageAttrSection, page)
	if !strings.Contains(original, extID) || !strings.Contains(original, "[author:: Dana]") {
		t.Errorf("keep_theirs must leave the external version on the original path:\n%s", original)
	}
	if strings.Contains(original, "edited through the page editor") {
		t.Errorf("keep_theirs wrote my buffer over theirs:\n%s", original)
	}

	// The copy carries my body edit plus the merged attribution.
	sibBytes, err := os.ReadFile(sibling)
	if err != nil {
		t.Fatalf("read sibling: %v", err)
	}
	sib := string(sibBytes)
	for _, want := range []string{
		"edited through the page editor", "[author:: Dana]", "[ts:: 2026-07-07T10:00:00]",
	} {
		if !strings.Contains(sib, want) {
			t.Errorf("conflict copy is missing %q:\n%s", want, sib)
		}
	}
	if strings.Contains(sib, extID) {
		t.Errorf("conflict copy must hold my buffer, not the external line:\n%s", sib)
	}

	// The copy is a real page on disk. The block rows for these ids re-home to
	// the original when theirs settles last, so re-parsing the copy's bytes is
	// the assertion of record for what the user's version contains.
	comment := savedBlock(t, app, copyPage, fileDate, commentID)
	if comment == nil {
		t.Fatal("comment missing from the conflict copy")
	}
	if comment.CleanText != "edited through the page editor" {
		t.Errorf("conflict copy body = %q, want the editor's edit", comment.CleanText)
	}
	if comment.Author != "Dana" || comment.Timestamp != "2026-07-07T10:00:00" {
		t.Errorf("conflict copy dropped attribution: author=%q ts=%q", comment.Author, comment.Timestamp)
	}
	if task := savedBlock(t, app, copyPage, fileDate, taskID); task == nil {
		t.Error("conflict copy lost the parent task block")
	}
}

// ---------------------------------------------------------------------------
// Global Replace (#1167): the modal fetches through the programmatic funnel,
// mutates the fetched rows in place and writes the same rows back, so the
// fetch itself must carry the comment metadata the shared SELECT never
// hydrates — and its undo snapshot replays those same rows.
// ---------------------------------------------------------------------------

// programmaticPayload mirrors what Global Replace persists: the programmatic
// fetch, unmodified. Kept as a helper so apply and undo share one shape.
func programmaticPayload(t *testing.T, app *App, page string) []parser.ParsedBlock {
	t.Helper()
	blocks, err := app.FetchPageBlocksProgrammatic(pageAttrNotebook, pageAttrSection, page)
	if err != nil {
		t.Fatalf("FetchPageBlocksProgrammatic %s: %v", page, err)
	}
	return blocks
}

// TestFetchPageBlocksProgrammatic_HydratesCommentMetadata pins the hydration
// unit assertion: the programmatic payload carries marker + attribution while
// the tokens stay out of the displayed body.
func TestFetchPageBlocksProgrammatic_HydratesCommentMetadata(t *testing.T) {
	app := newTestApp(t)
	const (
		taskID    = "ae190001-6666-1111-1111-111111111111"
		commentID = "ae190002-6666-1111-1111-111111111111"
		page      = "PageAttributionGlobalReplace"
		fileDate  = "2026-07-01"
	)
	content := "- [ ] parent task <!-- id: " + taskID + " -->\n" +
		"\t- looks good [comment::] [author:: Dana] [ts:: 2026-07-07T10:00:00] <!-- id: " + commentID + " -->\n"
	indexTestFile(t, app, pageAttrNotebook, pageAttrSection, page, fileDate, content)

	blocks := programmaticPayload(t, app, page)
	var comment *parser.ParsedBlock
	for i := range blocks {
		if blocks[i].ID == commentID {
			comment = &blocks[i]
		}
	}
	if comment == nil {
		t.Fatalf("comment missing from the programmatic payload: %+v", blocks)
	}
	if !comment.IsComment || comment.Author != "Dana" || comment.Timestamp != "2026-07-07T10:00:00" {
		t.Errorf("programmatic fetch dropped comment metadata: is_comment=%v author=%q ts=%q",
			comment.IsComment, comment.Author, comment.Timestamp)
	}
	if comment.CleanText != "looks good" {
		t.Errorf("tokens leaked into the displayed body: %q", comment.CleanText)
	}
}

// TestSaveFileBlocksProgrammatic_ApplyPreservesCommentAttribution drives the
// Global Replace apply shape end to end: mutate only the fetched comment's
// body, write it back, and the marker + attribution must survive on disk, in
// the sub-tree and in comments_count.
func TestSaveFileBlocksProgrammatic_ApplyPreservesCommentAttribution(t *testing.T) {
	app := newTestApp(t)
	const (
		taskID    = "ae200001-6666-1111-1111-111111111111"
		commentID = "ae200002-6666-1111-1111-111111111111"
		page      = "PageAttributionReplaceApply"
		fileDate  = "2026-07-01"
	)
	content := "- [ ] parent task <!-- id: " + taskID + " -->\n" +
		"\t- looks good [comment::] [author:: Dana] [ts:: 2026-07-07T10:00:00] <!-- id: " + commentID + " -->\n"
	indexTestFile(t, app, pageAttrNotebook, pageAttrSection, page, fileDate, content)

	payload := programmaticPayload(t, app, page)
	edited := false
	for i := range payload {
		if payload[i].ID == commentID {
			payload[i].CleanText = "looks great" // the modal's only mutation
			edited = true
		}
	}
	if !edited {
		t.Fatal("comment block missing from the programmatic payload fixture")
	}
	if err := app.SaveFileBlocksProgrammatic(pageAttrNotebook, pageAttrSection, page, payload); err != nil {
		t.Fatalf("SaveFileBlocksProgrammatic: %v", err)
	}

	saved := mustReadTaskFile(t, app, pageAttrNotebook, pageAttrSection, page)
	if !strings.Contains(saved, "looks great") {
		t.Errorf("the replacement did not land:\n%s", saved)
	}
	for _, token := range []string{"[comment::]", "[author:: Dana]", "[ts:: 2026-07-07T10:00:00]"} {
		if n := strings.Count(saved, token); n != 1 {
			t.Errorf("%q appears %d times on disk, want 1:\n%s", token, n, saved)
		}
	}

	sub, err := app.FetchSubtree(taskID)
	if err != nil {
		t.Fatalf("FetchSubtree: %v", err)
	}
	comment := findSubtreeBlock(t, sub, commentID)
	if comment == nil {
		t.Fatalf("comment missing from the sub-tree after the replace: %+v", sub)
	}
	if comment.CleanText != "looks great" || !comment.IsComment ||
		comment.Author != "Dana" || comment.Timestamp != "2026-07-07T10:00:00" {
		t.Errorf("sub-tree view drifted: text=%q is_comment=%v author=%q ts=%q",
			comment.CleanText, comment.IsComment, comment.Author, comment.Timestamp)
	}
	if got := taskCommentsCount(t, app, taskID); got != 1 {
		t.Errorf("comments_count = %d, want 1", got)
	}
}

// TestSaveFileBlocksProgrammatic_UndoKeepsCommentAttribution replays the
// pre-mutation snapshot through the same funnel, which is exactly what the
// modal's Undo does — the metadata must survive the round trip.
func TestSaveFileBlocksProgrammatic_UndoKeepsCommentAttribution(t *testing.T) {
	app := newTestApp(t)
	const (
		taskID    = "ae210001-6666-1111-1111-111111111111"
		commentID = "ae210002-6666-1111-1111-111111111111"
		page      = "PageAttributionReplaceUndo"
		fileDate  = "2026-07-01"
	)
	content := "- [ ] parent task <!-- id: " + taskID + " -->\n" +
		"\t- original text [comment::] [author:: Dana] [ts:: 2026-07-07T10:00:00] <!-- id: " + commentID + " -->\n"
	indexTestFile(t, app, pageAttrNotebook, pageAttrSection, page, fileDate, content)

	payload := programmaticPayload(t, app, page)
	// The modal snapshots the original rows once before mutating them.
	snapshot := make([]parser.ParsedBlock, len(payload))
	copy(snapshot, payload)
	for i := range payload {
		if payload[i].ID == commentID {
			payload[i].CleanText = "replaced text"
		}
	}
	if err := app.SaveFileBlocksProgrammatic(pageAttrNotebook, pageAttrSection, page, payload); err != nil {
		t.Fatalf("apply save: %v", err)
	}
	if err := app.SaveFileBlocksProgrammatic(pageAttrNotebook, pageAttrSection, page, snapshot); err != nil {
		t.Fatalf("undo save: %v", err)
	}

	saved := mustReadTaskFile(t, app, pageAttrNotebook, pageAttrSection, page)
	if !strings.Contains(saved, "original text") || strings.Contains(saved, "replaced text") {
		t.Errorf("undo did not restore the original body:\n%s", saved)
	}
	for _, token := range []string{"[comment::]", "[author:: Dana]", "[ts:: 2026-07-07T10:00:00]"} {
		if n := strings.Count(saved, token); n != 1 {
			t.Errorf("%q appears %d times after undo, want 1:\n%s", token, n, saved)
		}
	}

	sub, err := app.FetchSubtree(taskID)
	if err != nil {
		t.Fatalf("FetchSubtree: %v", err)
	}
	comment := findSubtreeBlock(t, sub, commentID)
	if comment == nil {
		t.Fatalf("comment missing from the sub-tree after undo: %+v", sub)
	}
	if !comment.IsComment || comment.Author != "Dana" || comment.Timestamp != "2026-07-07T10:00:00" {
		t.Errorf("undo dropped comment metadata: is_comment=%v author=%q ts=%q",
			comment.IsComment, comment.Author, comment.Timestamp)
	}
	if got := taskCommentsCount(t, app, taskID); got != 1 {
		t.Errorf("comments_count = %d after undo, want 1", got)
	}
}

// TestSaveFileBlocksProgrammatic_NewNoteStaysUnattributed guards the other side
// of the hydration: Global Replace never creates blocks, so an id with no
// on-disk match must not borrow metadata from the payload around it.
func TestSaveFileBlocksProgrammatic_NewNoteStaysUnattributed(t *testing.T) {
	app := newTestApp(t)
	const (
		taskID    = "ae220001-6666-1111-1111-111111111111"
		commentID = "ae220002-6666-1111-1111-111111111111"
		newNoteID = "ae220003-6666-1111-1111-111111111111"
		page      = "PageAttributionReplaceNewNote"
		fileDate  = "2026-07-01"
	)
	content := "- [ ] parent task <!-- id: " + taskID + " -->\n" +
		"\t- existing comment [comment::] [author:: Dana] [ts:: 2026-07-07T10:00:00] <!-- id: " + commentID + " -->\n"
	indexTestFile(t, app, pageAttrNotebook, pageAttrSection, page, fileDate, content)

	payload := programmaticPayload(t, app, page)
	payload = append(payload, parser.ParsedBlock{
		ID: newNoteID, Type: parser.BlockNote, Depth: 1, CleanText: "no on-disk match",
	})
	if err := app.SaveFileBlocksProgrammatic(pageAttrNotebook, pageAttrSection, page, payload); err != nil {
		t.Fatalf("SaveFileBlocksProgrammatic: %v", err)
	}

	fresh := savedBlock(t, app, page, fileDate, newNoteID)
	if fresh == nil {
		t.Fatal("new note missing from disk")
	}
	if fresh.IsComment || fresh.Author != "" || fresh.Timestamp != "" {
		t.Errorf("funnel invented metadata for a new block: is_comment=%v author=%q ts=%q",
			fresh.IsComment, fresh.Author, fresh.Timestamp)
	}
	if old := savedBlock(t, app, page, fileDate, commentID); old == nil ||
		!old.IsComment || old.Author != "Dana" || old.Timestamp != "2026-07-07T10:00:00" {
		t.Errorf("existing comment metadata drifted: %+v", old)
	}
}

// TestMCPFetchPageBlocks_KeepsCommentMetadataBare is the contract guard: the
// MCP read path fetches the same rows through the unhydrated funnel, so its
// JSON contract must not grow marker/attribution fields (#1167 / D1).
func TestMCPFetchPageBlocks_KeepsCommentMetadataBare(t *testing.T) {
	app := newTestApp(t)
	const (
		taskID    = "ae230001-6666-1111-1111-111111111111"
		commentID = "ae230002-6666-1111-1111-111111111111"
		page      = "PageAttributionMCPGuard"
		fileDate  = "2026-07-01"
	)
	content := "- [ ] parent task <!-- id: " + taskID + " -->\n" +
		"\t- looks good [comment::] [author:: Dana] [ts:: 2026-07-07T10:00:00] <!-- id: " + commentID + " -->\n"
	indexTestFile(t, app, pageAttrNotebook, pageAttrSection, page, fileDate, content)

	bridge := mcpBridge{app: app, vaultPath: app.vaultPath}
	rows, _, err := bridge.FetchPageBlocks(context.Background(), pageAttrNotebook, pageAttrSection, page)
	if err != nil {
		t.Fatalf("MCP FetchPageBlocks: %v", err)
	}
	comment := findSubtreeBlock(t, rows, commentID)
	if comment == nil {
		t.Fatalf("MCP read dropped the comment row: %+v", rows)
	}
	if comment.IsComment || comment.Author != "" || comment.Timestamp != "" {
		t.Errorf("hydration leaked into the MCP contract: is_comment=%v author=%q ts=%q",
			comment.IsComment, comment.Author, comment.Timestamp)
	}
}

// TestSaveFileBlocks_RefusedSaveResolveKeepsProgrammaticAttributionClear pins
// the funnel boundary: a programmatic payload's deliberate attribution clear is
// authoritative, so its replay must write exactly what was stashed (D2).
func TestSaveFileBlocks_RefusedSaveResolveKeepsProgrammaticAttributionClear(t *testing.T) {
	app := newTestApp(t)
	const (
		taskID    = "ae170001-6666-1111-1111-111111111111"
		commentID = "ae170002-6666-1111-1111-111111111111"
		extID     = "ae170003-6666-1111-1111-111111111111"
		page      = "PageAttributionProgrammatic"
		fileDate  = "2026-07-01"
	)
	content := "- [ ] parent task <!-- id: " + taskID + " -->\n" +
		"\t- looks good [author:: Dana] [ts:: 2026-07-07T10:00:00] <!-- id: " + commentID + " -->\n"
	indexTestFile(t, app, pageAttrNotebook, pageAttrSection, page, fileDate, content)

	payload := pageEditorPayload(t, app, page)
	external := content + "- external line <!-- id: " + extID + " -->\n"
	if err := os.WriteFile(conflictPagePath(app, pageAttrNotebook, pageAttrSection, page),
		[]byte(external), 0o644); err != nil {
		t.Fatalf("external write: %v", err)
	}
	requireConflict(t, app.SaveFileBlocksProgrammatic(pageAttrNotebook, pageAttrSection, page, payload))

	if _, err := app.ResolveExternalConflict(pageAttrNotebook, pageAttrSection, page, conflictResolutionKeepMine); err != nil {
		t.Fatalf("ResolveExternalConflict(keep_mine): %v", err)
	}

	saved := mustReadTaskFile(t, app, pageAttrNotebook, pageAttrSection, page)
	if strings.Contains(saved, "[author:: Dana]") || strings.Contains(saved, "[ts:: 2026-07-07T10:00:00]") {
		t.Errorf("programmatic replay re-attributed comments the payload cleared:\n%s", saved)
	}
	if !strings.Contains(saved, "looks good") {
		t.Errorf("programmatic replay dropped the comment block:\n%s", saved)
	}
}
