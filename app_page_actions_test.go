package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"silt/backend/config"
	"silt/backend/parser"
)

const duplicatePageContent = `---
notebook: "Work"
section: "Projects/Active"
page: "Original"
tags: [copy-test]
---
- [ ] First {{embed:11111111-1111-4111-8111-111111111111}} <!-- id: 11111111-1111-4111-8111-111111111111 -->
- [ ] Second [blocked_by:: ((11111111-1111-4111-8111-111111111111))] <!-- id: 22222222-2222-4222-8222-222222222222 -->
`

func seedIndexedPage(t *testing.T, app *App, path, notebook, section, page, content string) {
	seedIndexedPageSource(t, app, "vault", path, notebook, section, page, content)
}

func seedIndexedPageSource(t *testing.T, app *App, source, path, notebook, section, page, content string) {
	t.Helper()
	writeFile(t, path, content)
	blocks, meta, _, _, err := parser.ParseFileContent(content, notebook, section, page, "2026-01-01", app.spacesPerTab)
	if err != nil {
		t.Fatalf("ParseFileContent: %v", err)
	}
	app.coordinator.WithDBWrite(func() {
		if err := app.db.IndexFileBlocks(source, meta.Notebook, meta.Section, meta.Page, blocks, meta.Tags, meta.Warnings...); err != nil {
			t.Fatalf("IndexFileBlocks: %v", err)
		}
	})
}

func TestDuplicatePage_TypedPageIsProjected(t *testing.T) {
	// MB2: duplicating a typed page must project the copy into page_types so
	// dashboards include it without a restart.
	app := newTestApp(t)
	typesDir := filepath.Join(app.vaultPath, ".system", "types")
	if err := os.MkdirAll(typesDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(typesDir, "meeting.yaml"), []byte(
		"name: Meeting\nproperties:\n  - name: status\n    type: text\n",
	), 0o600); err != nil {
		t.Fatal(err)
	}
	content := `---
notebook: "Work"
section: "Projects/Active"
page: "Standup"
type: "meeting"
status: "open"
---
- [ ] Agenda <!-- id: aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa -->
`
	sourcePath := filepath.Join(app.vaultPath, "Work", "Projects", "Active", "Standup.md")
	seedIndexedPage(t, app, sourcePath, "Work", "Projects/Active", "Standup", content)
	// Seed the source projection the way a normal typed write would.
	blocks, meta, _, _, err := parser.ParseFileContent(content, "Work", "Projects/Active", "Standup", "2026-01-01", app.spacesPerTab)
	if err != nil {
		t.Fatal(err)
	}
	_ = blocks
	app.projectPageType("vault", meta)

	if err := app.DuplicatePage("Work", "Projects/Active", "Standup", "Standup Copy"); err != nil {
		t.Fatalf("DuplicatePage: %v", err)
	}
	rows, err := app.QueryPagesByType("meeting", nil, "", false)
	if err != nil {
		t.Fatalf("QueryPagesByType: %v", err)
	}
	found := false
	for _, r := range rows {
		if r.Page == "Standup Copy" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("duplicated typed page missing from QueryPagesByType; rows=%+v", rows)
	}
}

func TestDuplicatePage_VaultFreshIDsFrontmatterAndIndex(t *testing.T) {
	app := newTestApp(t)
	sourcePath := filepath.Join(app.vaultPath, "Work", "Projects", "Active", "Original.md")
	seedIndexedPage(t, app, sourcePath, "Work", "Projects/Active", "Original", duplicatePageContent)

	if err := app.DuplicatePage("Work", "Projects/Active", "Original", "Copy"); err != nil {
		t.Fatalf("DuplicatePage: %v", err)
	}
	targetPath := filepath.Join(app.vaultPath, "Work", "Projects", "Active", "Copy.md")
	content, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("read duplicate: %v", err)
	}
	text := string(content)
	if !strings.Contains(text, `page: "Copy"`) || !strings.Contains(text, `section: "Projects/Active"`) {
		t.Fatalf("duplicate frontmatter has wrong location: %s", text)
	}
	if strings.Contains(text, "11111111-1111-4111-8111-111111111111") || strings.Contains(text, "22222222-2222-4222-8222-222222222222") {
		t.Fatalf("duplicate retained a source block UUID: %s", text)
	}

	blocks, _, _, _, err := parser.ParseFileContent(text, "Work", "Projects/Active", "Copy", "2026-01-01", app.spacesPerTab)
	if err != nil || len(blocks) != 2 {
		t.Fatalf("parse duplicate blocks: count=%d err=%v", len(blocks), err)
	}
	ids, err := app.db.BlockIDsForPage("vault", "Work", "Projects/Active", "Copy")
	if err != nil || len(ids) != 2 {
		t.Fatalf("duplicate was not indexed: ids=%v err=%v", ids, err)
	}
	if err := app.DuplicatePage("Work", "Projects/Active", "Original", "Copy"); err == nil {
		t.Fatal("duplicate collision should fail")
	} else {
		var ipc *IPCError
		if !errors.As(err, &ipc) || ipc.Code != CodeNavigationConflict {
			t.Fatalf("collision error = %T %v, want navigation conflict", err, err)
		}
	}
}

func TestDuplicatePage_LinkedStaysInLinkedRoot(t *testing.T) {
	app := newTestApp(t)
	ext := t.TempDir()
	ln, err := app.LinkNotebook(ext)
	if err != nil {
		t.Fatalf("LinkNotebook: %v", err)
	}
	content := strings.ReplaceAll(duplicatePageContent, `notebook: "Work"`, `notebook: "`+ln.DisplayName+`"`)
	content = strings.ReplaceAll(content, `page: "Original"`, `page: "Original"`)
	seedIndexedPageSource(t, app, ln.Source(), filepath.Join(ext, "Projects", "Active", "Original.md"), ln.DisplayName, "Projects/Active", "Original", content)
	if err := app.CreateSection(ln.DisplayName, "Projects/Active", "Child"); err != nil {
		t.Fatalf("linked CreateSection: %v", err)
	}
	if _, err := os.Stat(filepath.Join(ext, "Projects", "Active", "Child")); err != nil {
		t.Fatalf("linked child section missing: %v", err)
	}

	if err := app.DuplicatePage(ln.DisplayName, "Projects/Active", "Original", "LinkedCopy"); err != nil {
		t.Fatalf("linked DuplicatePage: %v", err)
	}
	if _, err := os.Stat(filepath.Join(ext, "Projects", "Active", "LinkedCopy.md")); err != nil {
		t.Fatalf("linked duplicate missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(app.vaultPath, ln.DisplayName, "Projects", "Active", "LinkedCopy.md")); err == nil {
		t.Fatal("linked duplicate was copied into the vault")
	}
}

func TestDuplicatePage_PostWriteFailuresRollbackAndRetry(t *testing.T) {
	app := newTestApp(t)
	seedIndexedPage(t, app, filepath.Join(app.vaultPath, "Work", "Original.md"), "Work", "", "Original", duplicatePageContent)

	originalParse := duplicatePagePostWriteParse
	duplicatePagePostWriteParse = func(string, string, string, string, string, int) ([]parser.ParsedBlock, parser.FileMetadata, string, bool, error) {
		return nil, parser.FileMetadata{}, "", false, errors.New("injected reparse failure")
	}
	t.Cleanup(func() { duplicatePagePostWriteParse = originalParse })
	if err := app.DuplicatePage("Work", "", "Original", "RetryParse"); err == nil {
		t.Fatal("injected reparse failure should be returned")
	}
	parseRetryPath := filepath.Join(app.vaultPath, "Work", "RetryParse.md")
	if _, err := os.Stat(parseRetryPath); !os.IsNotExist(err) {
		t.Fatalf("failed reparse left target behind: stat err=%v", err)
	}
	if ids, err := app.db.BlockIDsForPage("vault", "Work", "", "RetryParse"); err != nil || len(ids) != 0 {
		t.Fatalf("failed reparse left index rows: ids=%v err=%v", ids, err)
	}
	duplicatePagePostWriteParse = originalParse
	if err := app.DuplicatePage("Work", "", "Original", "RetryParse"); err != nil {
		t.Fatalf("retry after reparse rollback: %v", err)
	}

	originalIndex := duplicatePageIndex
	duplicatePageIndex = func(*App, string, string, string, string, []parser.ParsedBlock, []string, ...string) error {
		return errors.New("injected index failure")
	}
	t.Cleanup(func() { duplicatePageIndex = originalIndex })
	if err := app.DuplicatePage("Work", "", "Original", "RetryIndex"); err == nil {
		t.Fatal("injected index failure should be returned")
	}
	indexRetryPath := filepath.Join(app.vaultPath, "Work", "RetryIndex.md")
	if _, err := os.Stat(indexRetryPath); !os.IsNotExist(err) {
		t.Fatalf("failed index left target behind: stat err=%v", err)
	}
	if ids, err := app.db.BlockIDsForPage("vault", "Work", "", "RetryIndex"); err != nil || len(ids) != 0 {
		t.Fatalf("failed index left index rows: ids=%v err=%v", ids, err)
	}
	duplicatePageIndex = originalIndex
	if err := app.DuplicatePage("Work", "", "Original", "RetryIndex"); err != nil {
		t.Fatalf("retry after index rollback: %v", err)
	}
}

func TestDuplicatePage_AndCreatePageSerializeOnExactTarget(t *testing.T) {
	app := newTestApp(t)
	seedIndexedPage(t, app, filepath.Join(app.vaultPath, "Work", "Original.md"), "Work", "", "Original", duplicatePageContent)

	entered := make(chan struct{})
	release := make(chan struct{})
	originalBeforeWrite := duplicatePageBeforeWrite
	duplicatePageBeforeWrite = func() {
		close(entered)
		<-release
	}
	t.Cleanup(func() { duplicatePageBeforeWrite = originalBeforeWrite })

	duplicateDone := make(chan error, 1)
	go func() { duplicateDone <- app.DuplicatePage("Work", "", "Original", "Copy") }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("duplicate did not reach its target write hook")
	}

	createDone := make(chan error, 1)
	var createDate string
	go func() {
		date, err := app.CreatePage("Work", "", "Copy", "2026-06-13")
		createDate = date
		createDone <- err
	}()
	select {
	case err := <-createDone:
		t.Fatalf("CreatePage completed before DuplicatePage released the target lock: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	close(release)
	if err := <-duplicateDone; err != nil {
		t.Fatalf("DuplicatePage: %v", err)
	}
	if err := <-createDone; err != nil {
		t.Fatalf("CreatePage: %v", err)
	}
	if createDate != "2026-06-13" {
		t.Fatalf("CreatePage returned date %q, want existing-page date", createDate)
	}

	content, err := os.ReadFile(filepath.Join(app.vaultPath, "Work", "Copy.md"))
	if err != nil {
		t.Fatalf("read serialized target: %v", err)
	}
	if !strings.Contains(string(content), "First") || strings.Contains(string(content), "date: \"2026-06-13\"") {
		t.Fatalf("CreatePage clobbered the duplicate target: %s", content)
	}
}

func TestDuplicatePage_RejectsInvalidAndMissingLocators(t *testing.T) {
	app := newTestApp(t)
	cases := []struct {
		name   string
		nb     string
		sec    string
		page   string
		target string
		code   IPCErrorCode
	}{
		{"section traversal", "Work", "../escape", "Original", "Copy", CodeInvalidNavigationPath},
		{"target traversal", "Work", "", "Original", "../Copy", CodeInvalidNavigationPath},
		{"missing source", "Work", "", "Missing", "Copy", CodeNavigationNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := app.DuplicatePage(tc.nb, tc.sec, tc.page, tc.target)
			var ipc *IPCError
			if !errors.As(err, &ipc) || ipc.Code != tc.code {
				t.Fatalf("error = %T %v, want code %q", err, err, tc.code)
			}
		})
	}
}

func TestRevealPageInOS_VaultLinkedAndFailures(t *testing.T) {
	app := newTestApp(t)
	orig := openNative
	var opened []string
	openNative = func(path string) error { opened = append(opened, path); return nil }
	t.Cleanup(func() { openNative = orig })

	vaultPath := filepath.Join(app.vaultPath, "Work", "Projects", "Note.md")
	seedIndexedPage(t, app, vaultPath, "Work", "Projects", "Note", "---\nnotebook: Work\nsection: Projects\npage: Note\n---\n# Note\n")
	if err := app.RevealPageInOS("Work", "Projects", "Note"); err != nil {
		t.Fatalf("vault reveal: %v", err)
	}
	if len(opened) != 1 || opened[0] != vaultPath {
		t.Fatalf("opened %v, want %q", opened, vaultPath)
	}

	ext := t.TempDir()
	ln, err := app.LinkNotebook(ext)
	if err != nil {
		t.Fatalf("LinkNotebook: %v", err)
	}
	linkedPath := filepath.Join(ext, "Linked.md")
	seedIndexedPageSource(t, app, ln.Source(), linkedPath, ln.DisplayName, "", "Linked", "---\nnotebook: "+ln.DisplayName+"\nsection: \"\"\npage: Linked\n---\n# Linked\n")
	opened = nil
	if err := app.RevealPageInOS(ln.DisplayName, "", "Linked"); err != nil {
		t.Fatalf("linked reveal: %v", err)
	}
	if len(opened) != 1 || opened[0] != linkedPath {
		t.Fatalf("linked opened %v, want %q", opened, linkedPath)
	}

	err = app.RevealPageInOS("Work", "", "Missing")
	var ipc *IPCError
	if !errors.As(err, &ipc) || ipc.Code != CodeNavigationNotFound {
		t.Fatalf("missing reveal error = %T %v, want navigation not found", err, err)
	}
	if err := app.RevealPageInOS("Work", "../escape", "Note"); err == nil {
		t.Fatal("traversal reveal should fail")
	}
}

func TestRevealPageInOS_LinkedOfflineIsTyped(t *testing.T) {
	app := newTestApp(t)
	app.configMu.Lock()
	app.cfg.LinkedNotebooks = []config.LinkedNotebook{{ID: "offline", DisplayName: "Offline", RootPath: filepath.Join(t.TempDir(), "missing")}}
	app.configMu.Unlock()
	err := app.RevealPageInOS("Offline", "", "Page")
	var ipc *IPCError
	if !errors.As(err, &ipc) || ipc.Code != CodeNavigationUnavailable {
		t.Fatalf("offline reveal error = %T %v, want navigation unavailable", err, err)
	}
}
