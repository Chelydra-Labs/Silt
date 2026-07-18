package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"silt/backend/parser"
)

func seedRacePage(t *testing.T, app *App, notebook, section, page, blockID, body string) []parser.ParsedBlock {
	t.Helper()
	dir := filepath.Join(app.vaultPath, notebook, section)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	filePath := filepath.Join(dir, page+".md")
	content := "---\nnotebook: " + notebook + "\nsection: " + section + "\npage: " + page + "\ndate: 2026-01-01\ntags: []\n---\n" +
		body + " <!-- id: " + blockID + " -->\n"
	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	blocks, _, _, _, err := parser.ParseFileContent(content, notebook, section, page, "2026-01-01", app.spacesPerTab)
	if err != nil {
		t.Fatalf("ParseFileContent: %v", err)
	}
	if err := app.SaveFileBlocks(notebook, section, page, blocks); err != nil {
		t.Fatalf("SaveFileBlocks seed: %v", err)
	}
	return blocks
}

// TestRenameSection_SerializesAgainstSaveFileBlocks proves concurrent
// SaveFileBlocks cannot interleave with RenameSection such that a successful
// save is lost or a ghost file is left at the pre-rename path (#691).
func TestRenameSection_SerializesAgainstSaveFileBlocks(t *testing.T) {
	app := newTestApp(t)
	notebook, section, page := "RaceNB", "OldSec", "Page1"
	blockID := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	blocks := seedRacePage(t, app, notebook, section, page, blockID, "original line")

	marker := "SAVED_CONTENT_" + time.Now().Format("150405.000")
	blocks[0].RawText = marker + " <!-- id: " + blockID + " -->"
	blocks[0].CleanText = marker

	start := make(chan struct{})
	var wg sync.WaitGroup
	var saveErr, renameErr error

	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		// Hold the page lock briefly so rename is likely to wait, then save.
		app.coordinator.LockFileWrite(filepath.Join(app.vaultPath, notebook, section, page+".md"), func() {
			time.Sleep(20 * time.Millisecond)
		})
		saveErr = app.SaveFileBlocks(notebook, section, page, blocks)
	}()
	go func() {
		defer wg.Done()
		<-start
		time.Sleep(5 * time.Millisecond)
		renameErr = app.RenameSection(notebook, section, "NewSec")
	}()
	close(start)
	wg.Wait()

	if renameErr != nil {
		t.Fatalf("RenameSection: %v", renameErr)
	}

	oldPath := filepath.Join(app.vaultPath, notebook, section, page+".md")
	newPath := filepath.Join(app.vaultPath, notebook, "NewSec", page+".md")

	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("ghost file must not remain at pre-rename path %s", oldPath)
	}
	content, err := os.ReadFile(newPath)
	if err != nil {
		t.Fatalf("expected page at new path: %v", err)
	}

	hasMarker := strings.Contains(string(content), marker)
	if saveErr == nil && !hasMarker {
		t.Fatalf("successful save lost: new file missing marker %q\ncontent:\n%s", marker, content)
	}
	if saveErr != nil {
		t.Logf("save erred after/during rename (acceptable if fail-loud): %v", saveErr)
	}

	idxBlocks, err := app.FetchPageBlocks(notebook, "NewSec", page)
	if err != nil {
		t.Fatalf("FetchPageBlocks new location: %v", err)
	}
	if len(idxBlocks) == 0 {
		t.Fatal("index empty at new section")
	}
}

// TestRenameNotebook_SerializesAgainstSaveFileBlocks is the notebook-level
// counterpart of the section race (#691).
func TestRenameNotebook_SerializesAgainstSaveFileBlocks(t *testing.T) {
	app := newTestApp(t)
	oldNB, newNB := "OldRaceNB", "NewRaceNB"
	section, page := "Sec", "P"
	blockID := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	blocks := seedRacePage(t, app, oldNB, section, page, blockID, "nb original")

	marker := "NB_SAVE_" + time.Now().Format("150405.000")
	blocks[0].RawText = marker + " <!-- id: " + blockID + " -->"
	blocks[0].CleanText = marker

	start := make(chan struct{})
	var wg sync.WaitGroup
	var saveErr, renameErr error

	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		saveErr = app.SaveFileBlocks(oldNB, section, page, blocks)
	}()
	go func() {
		defer wg.Done()
		<-start
		renameErr = app.RenameNotebook(oldNB, newNB)
	}()
	close(start)
	wg.Wait()

	if renameErr != nil {
		t.Fatalf("RenameNotebook: %v", renameErr)
	}

	oldPath := filepath.Join(app.vaultPath, oldNB, section, page+".md")
	newPath := filepath.Join(app.vaultPath, newNB, section, page+".md")
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("ghost at old notebook path")
	}
	content, err := os.ReadFile(newPath)
	if err != nil {
		t.Fatalf("new path missing: %v", err)
	}
	if saveErr == nil && !strings.Contains(string(content), marker) {
		t.Fatalf("successful save lost after notebook rename\ncontent:\n%s", content)
	}
	if _, err := app.FetchPageBlocks(newNB, section, page); err != nil {
		t.Fatalf("index at new notebook: %v", err)
	}
}
