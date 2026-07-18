package main

import (
	"errors"
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

// TestRenameSection_SerializesAgainstSavePageMarkdown covers Source-mode save
// vs section rename (#691 race matrix).
func TestRenameSection_SerializesAgainstSavePageMarkdown(t *testing.T) {
	app := newTestApp(t)
	notebook, section, page := "MdRaceNB", "OldSec", "Page1"
	blockID := "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	_ = seedRacePage(t, app, notebook, section, page, blockID, "original md")

	marker := "MD_SAVE_" + time.Now().Format("150405.000")
	body := marker + "\n\n- [ ] task line\n"

	start := make(chan struct{})
	var wg sync.WaitGroup
	var saveErr, renameErr error

	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		_, saveErr = app.SavePageMarkdown(notebook, section, page, body)
	}()
	go func() {
		defer wg.Done()
		<-start
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
		t.Fatalf("ghost at old path")
	}
	content, err := os.ReadFile(newPath)
	if err != nil {
		t.Fatalf("new path: %v", err)
	}
	if saveErr == nil && !strings.Contains(string(content), marker) {
		t.Fatalf("successful markdown save lost\n%s", content)
	}
	if saveErr != nil && !errors.Is(saveErr, ErrPageMovedOrDeleted) && !strings.Contains(saveErr.Error(), "page_moved") {
		t.Logf("save erred (acceptable if fail-loud): %v", saveErr)
	}
}

// TestRenameSection_SerializesAgainstMutateBlock covers single-block mutate
// vs section rename (#691).
func TestRenameSection_SerializesAgainstMutateBlock(t *testing.T) {
	app := newTestApp(t)
	notebook, section, page := "MutRaceNB", "OldSec", "Page1"
	blockID := "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
	_ = seedRacePage(t, app, notebook, section, page, blockID, "mutate original")

	marker := "MUT_" + time.Now().Format("150405.000")
	start := make(chan struct{})
	var wg sync.WaitGroup
	var mutErr, renameErr error

	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		mutErr = app.MutateBlock(blockID, marker)
	}()
	go func() {
		defer wg.Done()
		<-start
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
		t.Fatalf("ghost at old path")
	}
	content, err := os.ReadFile(newPath)
	if err != nil {
		t.Fatalf("new path: %v", err)
	}
	// Mutate either landed before rename (marker present) or failed after move.
	if mutErr == nil && !strings.Contains(string(content), marker) {
		t.Fatalf("successful mutate lost\n%s", content)
	}
	if mutErr != nil && !errors.Is(mutErr, ErrPageMovedOrDeleted) &&
		!strings.Contains(mutErr.Error(), "page_moved") &&
		!strings.Contains(mutErr.Error(), "no such file") {
		t.Logf("mutate erred (acceptable if fail-loud): %v", mutErr)
	}
	if _, err := app.FetchPageBlocks(notebook, "NewSec", page); err != nil {
		t.Fatalf("index: %v", err)
	}
}

// TestDeleteSection_SerializesAgainstSaveFileBlocks ensures tree delete multi-
// locks descendants so a concurrent save cannot recreate a deleted path.
func TestDeleteSection_SerializesAgainstSaveFileBlocks(t *testing.T) {
	app := newTestApp(t)
	notebook, section, page := "DelRaceNB", "Doomed", "Page1"
	blockID := "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"
	blocks := seedRacePage(t, app, notebook, section, page, blockID, "to delete")
	blocks[0].RawText = "AFTER_DELETE_ATTEMPT <!-- id: " + blockID + " -->"
	blocks[0].CleanText = "AFTER_DELETE_ATTEMPT"

	start := make(chan struct{})
	var wg sync.WaitGroup
	var saveErr, delErr error

	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		saveErr = app.SaveFileBlocks(notebook, section, page, blocks)
	}()
	go func() {
		defer wg.Done()
		<-start
		delErr = app.DeleteSection(notebook, section)
	}()
	close(start)
	wg.Wait()

	if delErr != nil {
		t.Fatalf("DeleteSection: %v", delErr)
	}
	// Section path should be gone (trashed or removed).
	secPath := filepath.Join(app.vaultPath, notebook, section)
	if st, err := os.Stat(secPath); err == nil && st.IsDir() {
		// May still exist empty in some trash flows — page file must not be live content.
		pagePath := filepath.Join(secPath, page+".md")
		if _, err := os.Stat(pagePath); err == nil {
			// If save won first then delete, file is gone; if both raced, no live page.
			t.Logf("page still at %s after delete (checking trash)", pagePath)
		}
	}
	if saveErr == nil {
		// Save succeeded before delete — delete should still have removed the tree.
		if _, err := os.Stat(filepath.Join(app.vaultPath, notebook, section, page+".md")); err == nil {
			t.Fatal("live page remained after DeleteSection completed")
		}
	}
}
