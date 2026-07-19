package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"silt/backend/parser"
)

// injectInboundWikiLink saves sourcePage with body containing a wiki-link.
// Used from afterPreLockInbound so the link appears after pre-lock collect.
func injectInboundWikiLink(app *App, nb, sec, page, blockID, body string) error {
	content := "---\nnotebook: " + nb + "\nsection: " + sec + "\npage: " + page + "\ndate: 2026-01-01\ntags: []\n---\n" +
		body + " <!-- id: " + blockID + " -->\n"
	blocks, _, _, _, err := parser.ParseFileContent(content, nb, sec, page, "2026-01-01", app.spacesPerTab)
	if err != nil {
		return err
	}
	return app.SaveFileBlocks(nb, sec, page, blocks)
}

// TestRenamePage_InboundLinkCreatedDuringRename injects a late [[OldTarget]]
// after pre-lock inbound collect (via renameHooks) and asserts the under-lock
// retry + rewrite (or residual sweep) leaves [[NewTarget]] on the source page.
func TestRenamePage_InboundLinkCreatedDuringRename(t *testing.T) {
	app := newTestApp(t)
	nb, sec := "InbNB", "Sec"
	targetOld, targetNew := "OldTarget", "NewTarget"
	sourcePage := "Linker"
	srcID := "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"

	_ = seedRacePage(t, app, nb, sec, targetOld, "dddddddd-dddd-4ddd-8ddd-dddddddddddd", "target body")
	_ = seedRacePage(t, app, nb, sec, sourcePage, srcID, "no link yet")

	var saveErr error
	app.renameHooks = &renameHooks{
		afterPreLockInbound: func() {
			saveErr = injectInboundWikiLink(app, nb, sec, sourcePage, srcID, "see [[OldTarget]] please")
		},
	}
	t.Cleanup(func() { app.renameHooks = nil })

	if err := app.RenamePage(nb, sec, targetOld, targetNew); err != nil {
		t.Fatalf("RenamePage: %v", err)
	}
	if saveErr != nil {
		t.Fatalf("injected inbound save: %v", saveErr)
	}

	newPath := filepath.Join(app.vaultPath, nb, sec, targetNew+".md")
	if _, err := os.Stat(newPath); err != nil {
		t.Fatalf("renamed target missing: %v", err)
	}
	assertInboundRewritten(t, filepath.Join(app.vaultPath, nb, sec, sourcePage+".md"), "[[OldTarget]]", "[[NewTarget]]")
}

// TestMovePage_InboundLinkCreatedDuringRename covers MovePage TOCTOU retry.
func TestMovePage_InboundLinkCreatedDuringRename(t *testing.T) {
	app := newTestApp(t)
	nb, fromSec, toSec := "InbMoveNB", "FromSec", "ToSec"
	page := "Moved"
	sourcePage := "Linker"
	srcID := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbb01"

	_ = seedRacePage(t, app, nb, fromSec, page, "cccccccc-cccc-4ccc-8ccc-cccccccccc01", "target body")
	_ = seedRacePage(t, app, nb, "Other", sourcePage, srcID, "no link yet")

	var saveErr error
	app.renameHooks = &renameHooks{
		afterPreLockInbound: func() {
			// Section-qualified form must rewrite when the page moves sections.
			saveErr = injectInboundWikiLink(app, nb, "Other", sourcePage, srcID, "see [[FromSec/Moved]] please")
		},
	}
	t.Cleanup(func() { app.renameHooks = nil })

	if err := app.MovePage(nb, fromSec, toSec, page); err != nil {
		t.Fatalf("MovePage: %v", err)
	}
	if saveErr != nil {
		t.Fatalf("injected inbound save: %v", saveErr)
	}

	if _, err := os.Stat(filepath.Join(app.vaultPath, nb, toSec, page+".md")); err != nil {
		t.Fatalf("moved target missing: %v", err)
	}
	assertInboundRewritten(t, filepath.Join(app.vaultPath, nb, "Other", sourcePage+".md"), "[[FromSec/Moved]]", "[[ToSec/Moved]]")
}

// TestRenameSection_InboundLinkCreatedDuringRename covers the section rename
// retry path with a late section-qualified link that must be rewritten.
func TestRenameSection_InboundLinkCreatedDuringRename(t *testing.T) {
	app := newTestApp(t)
	nb, oldSec, newSec := "InbSecNB", "OldSec", "NewSec"
	page := "Page1"
	sourcePage := "Linker"
	srcID := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaa01"

	_ = seedRacePage(t, app, nb, oldSec, page, "ffffffff-ffff-4fff-8fff-ffffffffffff", "target body")
	_ = seedRacePage(t, app, nb, "Other", sourcePage, srcID, "no link yet")

	var saveErr error
	app.renameHooks = &renameHooks{
		afterPreLockInbound: func() {
			// Section path form MUST change; basename alone would still resolve.
			saveErr = injectInboundWikiLink(app, nb, "Other", sourcePage, srcID, "see [[OldSec/Page1]] please")
		},
	}
	t.Cleanup(func() { app.renameHooks = nil })

	if err := app.RenameSection(nb, oldSec, newSec); err != nil {
		t.Fatalf("RenameSection: %v", err)
	}
	if saveErr != nil {
		t.Fatalf("injected inbound save: %v", saveErr)
	}

	srcPath := filepath.Join(app.vaultPath, nb, "Other", sourcePage+".md")
	b, err := os.ReadFile(srcPath)
	if err != nil {
		t.Fatalf("read source: %v", err)
	}
	text := string(b)
	if strings.Contains(text, "[[OldSec/Page1]]") {
		t.Fatalf("stale [[OldSec/Page1]] remained after section rename:\n%s", text)
	}
	if !strings.Contains(text, "[[NewSec/Page1]]") && !strings.Contains(text, "[[Page1]]") {
		t.Fatalf("expected rewritten inbound link after section rename:\n%s", text)
	}
}

// TestRenameNotebook_InboundLinkCreatedDuringRename covers notebook rename
// TOCTOU retry + inbound rewrite for a notebook-qualified link.
func TestRenameNotebook_InboundLinkCreatedDuringRename(t *testing.T) {
	app := newTestApp(t)
	oldNB, newNB := "OldInbNB", "NewInbNB"
	sec, page := "Sec", "Page1"
	// Linker lives in a different notebook so it is not under the rename tree.
	linkNB, sourcePage := "OtherNB", "Linker"
	srcID := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaa02"

	_ = seedRacePage(t, app, oldNB, sec, page, "dddddddd-dddd-4ddd-8ddd-dddddddddd02", "target body")
	_ = seedRacePage(t, app, linkNB, sec, sourcePage, srcID, "no link yet")

	var saveErr error
	app.renameHooks = &renameHooks{
		afterPreLockInbound: func() {
			saveErr = injectInboundWikiLink(app, linkNB, sec, sourcePage, srcID, "see [[OldInbNB/Sec/Page1]] please")
		},
	}
	t.Cleanup(func() { app.renameHooks = nil })

	if err := app.RenameNotebook(oldNB, newNB); err != nil {
		t.Fatalf("RenameNotebook: %v", err)
	}
	if saveErr != nil {
		t.Fatalf("injected inbound save: %v", saveErr)
	}

	if _, err := os.Stat(filepath.Join(app.vaultPath, newNB, sec, page+".md")); err != nil {
		t.Fatalf("renamed notebook page missing: %v", err)
	}
	assertInboundRewritten(t,
		filepath.Join(app.vaultPath, linkNB, sec, sourcePage+".md"),
		"[[OldInbNB/Sec/Page1]]",
		"[[NewInbNB/Sec/Page1]]",
	)
}

func assertInboundRewritten(t *testing.T, srcPath, stale, want string) {
	t.Helper()
	b, err := os.ReadFile(srcPath)
	if err != nil {
		t.Fatalf("read source: %v", err)
	}
	text := string(b)
	if strings.Contains(text, stale) {
		t.Fatalf("stale %s remained after rename:\n%s", stale, text)
	}
	if !strings.Contains(text, want) {
		t.Fatalf("expected inbound link rewritten to %s, got:\n%s", want, text)
	}
}
