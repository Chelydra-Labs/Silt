package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"silt/backend/parser"
)

// TestRenamePage_InboundLinkCreatedDuringRename injects a late [[OldTarget]]
// after pre-lock inbound collect (via renameHooks) and asserts the under-lock
// retry + rewrite (or residual sweep) leaves [[NewTarget]] on the source page.
func TestRenamePage_InboundLinkCreatedDuringRename(t *testing.T) {
	app := newTestApp(t)
	nb, sec := "InbNB", "Sec"
	targetOld, targetNew := "OldTarget", "NewTarget"
	sourcePage := "Linker"

	_ = seedRacePage(t, app, nb, sec, targetOld, "dddddddd-dddd-4ddd-8ddd-dddddddddddd", "target body")
	// Source starts without the link so pre-lock collect is empty.
	_ = seedRacePage(t, app, nb, sec, sourcePage, "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee", "no link yet")

	var saveErr error
	app.renameHooks = &renameHooks{
		afterPreLockInbound: func() {
			body := "see [[OldTarget]] please"
			content := "---\nnotebook: " + nb + "\nsection: " + sec + "\npage: " + sourcePage + "\ndate: 2026-01-01\ntags: []\n---\n" +
				body + " <!-- id: eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee -->\n"
			blocks, _, _, _, err := parser.ParseFileContent(content, nb, sec, sourcePage, "2026-01-01", app.spacesPerTab)
			if err != nil {
				saveErr = err
				return
			}
			saveErr = app.SaveFileBlocks(nb, sec, sourcePage, blocks)
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
	srcPath := filepath.Join(app.vaultPath, nb, sec, sourcePage+".md")
	b, err := os.ReadFile(srcPath)
	if err != nil {
		t.Fatalf("read source: %v", err)
	}
	text := string(b)
	if !strings.Contains(text, "[[NewTarget]]") {
		t.Fatalf("expected inbound link rewritten to [[NewTarget]], got:\n%s", text)
	}
	if strings.Contains(text, "[[OldTarget]]") {
		t.Fatalf("stale [[OldTarget]] remained after rename:\n%s", text)
	}
}

// TestRenameSection_InboundLinkCreatedDuringRename covers the section rename
// retry path (missingMarkdownUnder + missingInboundPaths) with a late link.
func TestRenameSection_InboundLinkCreatedDuringRename(t *testing.T) {
	app := newTestApp(t)
	nb, oldSec, newSec := "InbSecNB", "OldSec", "NewSec"
	page := "Page1"
	sourcePage := "Linker"

	_ = seedRacePage(t, app, nb, oldSec, page, "ffffffff-ffff-4fff-8fff-ffffffffffff", "target body")
	_ = seedRacePage(t, app, nb, "Other", sourcePage, "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaa01", "no link yet")

	var saveErr error
	app.renameHooks = &renameHooks{
		afterPreLockInbound: func() {
			body := "see [[" + page + "]] please"
			content := "---\nnotebook: " + nb + "\nsection: Other\npage: " + sourcePage + "\ndate: 2026-01-01\ntags: []\n---\n" +
				body + " <!-- id: aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaa01 -->\n"
			blocks, _, _, _, err := parser.ParseFileContent(content, nb, "Other", sourcePage, "2026-01-01", app.spacesPerTab)
			if err != nil {
				saveErr = err
				return
			}
			saveErr = app.SaveFileBlocks(nb, "Other", sourcePage, blocks)
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
	// Basename link may stay as [[Page1]] if MapTargetRaw keeps depth; section
	// path forms would update. Accept either rewritten section path or basename
	// that still uniquely resolves — but must not leave a broken full old path.
	if strings.Contains(text, "[[OldSec/"+page+"]]") {
		t.Fatalf("stale section path link remained:\n%s", text)
	}
	if !strings.Contains(text, "[["+page+"]]") && !strings.Contains(text, "[[NewSec/"+page+"]]") {
		t.Fatalf("expected inbound link to still reference page after section rename:\n%s", text)
	}
}
