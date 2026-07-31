package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"silt/backend/db"
	"silt/backend/parser"
)

// idxUApp indexes blocks for the app-level unlinked-mentions IPC tests.
func idxUApp(t *testing.T, app *App, source, nb, sec, pg string, blocks []parser.ParsedBlock) {
	t.Helper()
	if err := app.db.IndexFileBlocks(source, nb, sec, pg, blocks, nil); err != nil {
		t.Fatalf("index %s/%s/%s/%s: %v", source, nb, sec, pg, err)
	}
}

// TestGetUnlinkedMentionsPaged_SourceResolution verifies the IPC wrapper
// resolves the notebook's source and returns matching unlinked mentions.
func TestGetUnlinkedMentionsPaged_SourceResolution(t *testing.T) {
	app := newTestApp(t)
	idxUApp(t, app, "vault", "Work", "Sec", "Onboarding", []parser.ParsedBlock{
		{ID: "uuuuuuuu-uuuu-4uuu-8uuu-uuuuuuuuuuuu", Type: parser.BlockNote, RawText: "Onboarding", CleanText: "Onboarding", LineNumber: 1},
	})
	idxUApp(t, app, "vault", "Work", "Sec", "Notes", []parser.ParsedBlock{
		{ID: "vvvvvvvv-vvvv-4vvv-8vvv-vvvvvvvvvvvv", Type: parser.BlockNote, RawText: "review Onboarding soon", CleanText: "review Onboarding soon", LineNumber: 1},
	})

	res, err := app.GetUnlinkedMentionsPaged("Work", "Sec", "Onboarding", "", "", 50)
	if err != nil {
		t.Fatalf("GetUnlinkedMentionsPaged: %v", err)
	}
	if len(res.Results) != 1 {
		t.Fatalf("expected 1 mention, got %d: %+v", len(res.Results), res.Results)
	}
	if res.Results[0].Source != "vault" || res.Results[0].SourcePage != "Notes" {
		t.Errorf("unexpected mention source: %+v", res.Results[0])
	}
}

// TestGetUnlinkedMentionsPaged_PaddedPathSelfFilter: wrapper trims notebook/
// section/page before source resolve and DB self-exclusion so padded IPC
// values do not list the active page as its own unlinked mention.
func TestGetUnlinkedMentionsPaged_PaddedPathSelfFilter(t *testing.T) {
	app := newTestApp(t)
	idxUApp(t, app, "vault", "Work", "Sec", "Onboarding", []parser.ParsedBlock{
		{ID: "uuuuuuuu-uuuu-4uuu-8uuu-uuuuuuuuuuuu", Type: parser.BlockNote, RawText: "self Onboarding", CleanText: "self Onboarding", LineNumber: 1},
	})
	idxUApp(t, app, "vault", "Work", "Sec", "Notes", []parser.ParsedBlock{
		{ID: "vvvvvvvv-vvvv-4vvv-8vvv-vvvvvvvvvvvv", Type: parser.BlockNote, RawText: "review Onboarding soon", CleanText: "review Onboarding soon", LineNumber: 1},
	})

	res, err := app.GetUnlinkedMentionsPaged("  Work  ", "  Sec  ", "  Onboarding  ", "", "", 50)
	if err != nil {
		t.Fatalf("padded GetUnlinkedMentionsPaged: %v", err)
	}
	for _, m := range res.Results {
		if m.SourcePage == "Onboarding" {
			t.Fatalf("active page must not appear as unlinked mention: %+v", res.Results)
		}
	}
	if len(res.Results) != 1 || res.Results[0].SourcePage != "Notes" {
		t.Fatalf("want only Notes, got %+v", res.Results)
	}
}

// TestGetUnlinkedMentionsPaged_DBClosed verifies the DB-closed error path: after
// closing the underlying manager, the wrapper surfaces an error rather than
// panicking or returning stale data.
func TestGetUnlinkedMentionsPaged_DBClosed(t *testing.T) {
	app := newTestApp(t)
	if err := app.db.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}
	_, err := app.GetUnlinkedMentionsPaged("Work", "Sec", "Onboarding", "", "", 50)
	if err == nil {
		t.Fatal("expected an error when the database is closed, got nil")
	}
}

// writeNotePageWithMention writes a page file to disk with a NOTE block carrying
// noteID and cleanText, then parses + indexes it. Mirrors writeSamplePage but
// for note blocks (the promote write chain reads the real file).
func writeNotePageWithMention(t *testing.T, app *App, notebook, section, page, fileDate, noteID, cleanText string) {
	t.Helper()
	filePath := filepath.Join(app.vaultPath, notebook, section, page+".md")
	content := "# Title <!-- id: 11111111-1111-1111-1111-111111111111 -->\n\n" +
		cleanText + " <!-- id: " + noteID + " -->\n"
	writeFile(t, filePath, content)
	blocks, meta, _, _, err := parser.ParseFileContent(content, notebook, section, page, fileDate, app.spacesPerTab)
	if err != nil {
		t.Fatalf("ParseFileContent: %v", err)
	}
	if err := app.db.IndexFileBlocks("vault", meta.Notebook, meta.Section, meta.Page, blocks, meta.Tags); err != nil {
		t.Fatalf("IndexFileBlocks: %v", err)
	}
}

// TestPromoteUnlinkedMention_Rewrite verifies the link action wraps the first
// plain-text occurrence in [[shortest]] through the atomic write chain, and the
// surrounding prose is preserved.
func TestPromoteUnlinkedMention_Rewrite(t *testing.T) {
	app := newTestApp(t)
	const srcBlock = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	// Target page (must exist as a distinct page for resolution).
	writeNotePageWithMention(t, app, "Work", "Sec", "Onboarding", "2026-06-13",
		"uuuuuuuu-uuuu-4uuu-8uuu-uuuuuuuuuuuu", "the onboarding guide")
	// Source page whose block mentions Onboarding in prose.
	writeNotePageWithMention(t, app, "Work", "Sec", "Notes", "2026-06-13",
		srcBlock, "review Onboarding before launch")

	if err := app.PromoteUnlinkedMention(srcBlock, "Work", "Sec", "Onboarding"); err != nil {
		t.Fatalf("PromoteUnlinkedMention: %v", err)
	}

	// Re-read the block's clean content and verify the wrap + prose preservation.
	var clean string
	_ = app.db.SQLDB().QueryRow("SELECT clean_content FROM blocks WHERE id = ?", srcBlock).Scan(&clean)
	if !strings.Contains(clean, "[[Onboarding]]") {
		t.Errorf("expected [[Onboarding]] in %q", clean)
	}
	// Surrounding prose preserved.
	if !strings.HasPrefix(clean, "review ") || !strings.HasSuffix(clean, " before launch") {
		t.Errorf("prose not preserved: %q", clean)
	}

	// The mention migrates: unlinked drops to 0, backlinks gains the page-link.
	res, _ := app.GetUnlinkedMentionsPaged("Work", "Sec", "Onboarding", "", "", 50)
	if len(res.Results) != 0 {
		t.Errorf("post-promote unlinked: expected 0, got %d: %+v", len(res.Results), res.Results)
	}
	bl, _ := app.db.GetBacklinks("vault", "Work", "Sec", "Onboarding")
	if len(bl) != 1 || bl[0].Kind != "page" {
		t.Errorf("post-promote backlinks: expected 1 page-link, got %+v", bl)
	}
	// Silence unused os import if QueryRow path changes; keep file-read sanity.
	if _, err := os.Stat(filepath.Join(app.vaultPath, "Work", "Sec", "Notes.md")); err != nil {
		t.Errorf("source file should still exist: %v", err)
	}
}

// TestPromoteUnlinkedMention_ExplicitPathDisambiguates verifies that when a leaf
// title is ambiguous, supplying the full (notebook, section, page) of one
// candidate promotes to that page rather than rejecting.
func TestPromoteUnlinkedMention_ExplicitPathDisambiguates(t *testing.T) {
	app := newTestApp(t)
	const srcBlock = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	// Two pages share the leaf title "Standup".
	writeNotePageWithMention(t, app, "Work", "Journal", "Standup", "2026-06-13",
		"uuuuuuuu-uuuu-4uuu-8uuu-uuuuuuuuuuuu", "journal entry")
	writeNotePageWithMention(t, app, "Work", "Log", "Standup", "2026-06-13",
		"vvvvvvvv-vvvv-4vvv-8vvv-vvvvvvvvvvvv", "log entry")
	// Source page mentions Standup in prose.
	writeNotePageWithMention(t, app, "Work", "Sec", "Notes", "2026-06-13",
		srcBlock, "Standup today")

	if err := app.PromoteUnlinkedMention(srcBlock, "Work", "Journal", "Standup"); err != nil {
		t.Fatalf("explicit-path promote: %v", err)
	}

	var clean string
	_ = app.db.SQLDB().QueryRow("SELECT clean_content FROM blocks WHERE id = ?", srcBlock).Scan(&clean)
	// Shortest unique path for Work/Journal/Standup among two Standups should
	// include enough path to disambiguate (section or more).
	if !strings.Contains(clean, "[[") || !strings.Contains(clean, "Standup]]") {
		t.Errorf("expected wiki-link wrap in %q", clean)
	}
	// Must not still be plain "Standup today" without brackets around the title.
	if strings.Contains(clean, "Standup today") && !strings.Contains(clean, "[[") {
		t.Errorf("title was not wrapped: %q", clean)
	}

	// Migrates into backlinks for the Journal Standup specifically.
	bl, _ := app.db.GetBacklinks("vault", "Work", "Journal", "Standup")
	if len(bl) != 1 || bl[0].Kind != "page" {
		t.Errorf("post-promote backlinks for Journal/Standup: got %+v", bl)
	}
	blLog, _ := app.db.GetBacklinks("vault", "Work", "Log", "Standup")
	if len(blLog) != 0 {
		t.Errorf("Log/Standup should not gain a backlink, got %+v", blLog)
	}
}

// TestPromoteUnlinkedMention_AmbiguousRejected verifies that without a matching
// explicit path, an ambiguous leaf title is still rejected with CodeAmbiguousTarget.
func TestPromoteUnlinkedMention_AmbiguousRejected(t *testing.T) {
	app := newTestApp(t)
	const srcBlock = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	idxUApp(t, app, "vault", "Work", "Journal", "Standup", []parser.ParsedBlock{
		{ID: "uuuuuuuu-uuuu-4uuu-8uuu-uuuuuuuuuuuu", Type: parser.BlockNote, RawText: "journal entry", CleanText: "journal entry", LineNumber: 1},
	})
	idxUApp(t, app, "vault", "Work", "Log", "Standup", []parser.ParsedBlock{
		{ID: "vvvvvvvv-vvvv-4vvv-8vvv-vvvvvvvvvvvv", Type: parser.BlockNote, RawText: "log entry", CleanText: "log entry", LineNumber: 1},
	})
	idxUApp(t, app, "vault", "Work", "Sec", "Notes", []parser.ParsedBlock{
		{ID: srcBlock, Type: parser.BlockNote, RawText: "Standup today", CleanText: "Standup today", LineNumber: 1},
	})

	// Path not in inventory → leaf fallback → ambiguous reject.
	err := app.PromoteUnlinkedMention(srcBlock, "Work", "Missing", "Standup")
	if err == nil {
		t.Fatal("expected ambiguous rejection, got nil")
	}
	var ipc *IPCError
	if !errors.As(err, &ipc) {
		t.Fatalf("expected *IPCError, got %T: %v", err, err)
	}
	if ipc.Code != CodeAmbiguousTarget {
		t.Errorf("expected code %q, got %q", CodeAmbiguousTarget, ipc.Code)
	}
}

// TestPromoteUnlinkedMention_MixedResidual verifies the full promote path on a
// block that already links the target once and still has a plain residual hit:
// wrap the residual, drop from unlinked list, gain a backlink edge.
func TestPromoteUnlinkedMention_MixedResidual(t *testing.T) {
	app := newTestApp(t)
	const srcBlock = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	writeNotePageWithMention(t, app, "Work", "Sec", "Onboarding", "2026-06-13",
		"uuuuuuuu-uuuu-4uuu-8uuu-uuuuuuuuuuuu", "the onboarding guide")
	writeNotePageWithMention(t, app, "Work", "Sec", "Notes", "2026-06-13",
		srcBlock, "see [[Onboarding]] and Onboarding too")

	// Pre: residual plain surfaces in unlinked.
	pre, err := app.GetUnlinkedMentionsPaged("Work", "Sec", "Onboarding", "", "", 50)
	if err != nil {
		t.Fatalf("pre GetUnlinkedMentionsPaged: %v", err)
	}
	if len(pre.Results) != 1 {
		t.Fatalf("pre unlinked: expected 1, got %d: %+v", len(pre.Results), pre.Results)
	}

	if err := app.PromoteUnlinkedMention(srcBlock, "Work", "Sec", "Onboarding"); err != nil {
		t.Fatalf("PromoteUnlinkedMention: %v", err)
	}

	var clean string
	if err := app.db.SQLDB().QueryRow("SELECT clean_content FROM blocks WHERE id = ?", srcBlock).Scan(&clean); err != nil {
		t.Fatalf("read clean: %v", err)
	}
	if clean != "see [[Onboarding]] and [[Onboarding]] too" {
		t.Errorf("post-promote body: got %q", clean)
	}

	post, _ := app.GetUnlinkedMentionsPaged("Work", "Sec", "Onboarding", "", "", 50)
	if len(post.Results) != 0 {
		t.Errorf("post-promote unlinked: expected 0, got %d: %+v", len(post.Results), post.Results)
	}
	bl, _ := app.db.GetBacklinks("vault", "Work", "Sec", "Onboarding")
	if len(bl) == 0 {
		t.Errorf("post-promote backlinks: expected >=1 page-link, got %+v", bl)
	}
}

// TestWrapFirstUnlinkedOccurrence verifies the plain-text→[[shortest]] rewrite
// helper directly: surrounding prose preserved, only the first promotable
// occurrence wrapped, and occurrences already inside [[…]] are skipped.
func TestWrapFirstUnlinkedOccurrence(t *testing.T) {
	titleRE := db.WordBoundaryTitleRE("Onboarding")
	got, ok := wrapFirstUnlinkedOccurrence("see Onboarding soon", titleRE, "Onboarding")
	if !ok || got != "see [[Onboarding]] soon" {
		t.Errorf("plain wrap: ok=%v got=%q", ok, got)
	}
	// Already-linked occurrence skipped, second plain one wrapped.
	got, ok = wrapFirstUnlinkedOccurrence("see [[Onboarding]] and Onboarding too", titleRE, "Onboarding")
	if !ok || got != "see [[Onboarding]] and [[Onboarding]] too" {
		t.Errorf("skip-linked: ok=%v got=%q", ok, got)
	}
	// No occurrence at all.
	got, ok = wrapFirstUnlinkedOccurrence("nothing here", titleRE, "Onboarding")
	if ok || got != "nothing here" {
		t.Errorf("no-match: ok=%v got=%q", ok, got)
	}
	// Non-ASCII titles: RE2's \b is ASCII-only, so accented titles need the
	// Unicode-aware boundaries in db.WordBoundaryTitleRE to match and wrap.
	accentRE := db.WordBoundaryTitleRE("Café")
	got, ok = wrapFirstUnlinkedOccurrence("le Café ouvert", accentRE, "Café")
	if !ok || got != "le [[Café]] ouvert" {
		t.Errorf("accented wrap: ok=%v got=%q", ok, got)
	}
	// A title that is a substring of a larger word must not match (boundary).
	got, ok = wrapFirstUnlinkedOccurrence("les Cafés sont là", accentRE, "Café")
	if ok || got != "les Cafés sont là" {
		t.Errorf("accented substring should not match: ok=%v got=%q", ok, got)
	}
}
