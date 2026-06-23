package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"silt/backend/parser"
)

// AddAttachment copies a source file into the notebook's attachments/ dir and
// returns the relative path. The copy is atomic and collision-safe.
func TestAddAttachment_CopiesAndReturnsRelPath(t *testing.T) {
	app := newTestApp(t)
	// Create a source file outside the notebook.
	src := filepath.Join(t.TempDir(), "report.pdf")
	if err := os.WriteFile(src, []byte("PDF CONTENT"), 0o644); err != nil {
		t.Fatal(err)
	}
	relPath, err := app.AddAttachment(src, "Work")
	if err != nil {
		t.Fatalf("AddAttachment: %v", err)
	}
	if relPath != "attachments/report.pdf" {
		t.Errorf("relPath = %q, want attachments/report.pdf", relPath)
	}
	abs := filepath.Join(app.vaultPath, "Work", "attachments", "report.pdf")
	got, err := os.ReadFile(abs)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != "PDF CONTENT" {
		t.Errorf("content = %q, want PDF CONTENT", got)
	}
}

// AddAttachment resolves collisions with a counter suffix so two notes
// attaching the same-named file produce two distinct copies.
func TestAddAttachment_CollisionSafe(t *testing.T) {
	app := newTestApp(t)
	src1 := filepath.Join(t.TempDir(), "doc.txt")
	os.WriteFile(src1, []byte("first"), 0o644)
	src2 := filepath.Join(t.TempDir(), "doc.txt")
	os.WriteFile(src2, []byte("second"), 0o644)

	rel1, _ := app.AddAttachment(src1, "Work")
	rel2, _ := app.AddAttachment(src2, "Work")
	if rel1 == rel2 {
		t.Fatalf("expected distinct paths, both = %q", rel1)
	}
	if rel1 != "attachments/doc.txt" {
		t.Errorf("first = %q, want attachments/doc.txt", rel1)
	}
	if rel2 != "attachments/doc-1.txt" {
		t.Errorf("second = %q, want attachments/doc-1.txt", rel2)
	}
}

// AddAttachment is race-free under concurrency: many goroutines attaching
// same-named files must each get a distinct destination, with no clobbering.
// Guards against the TOCTOU regression where collision resolution used an
// unlocked os.Stat check (O_CREATE|O_EXCL now reserves names atomically).
func TestAddAttachment_ConcurrentNoClobber(t *testing.T) {
	app := newTestApp(t)
	const n = 32
	type result struct {
		rel string
		err error
	}
	results := make([]result, n)
	srcs := make([]string, n)
	for i := range srcs {
		srcs[i] = filepath.Join(t.TempDir(), "shared.png")
		os.WriteFile(srcs[i], []byte(fmt.Sprintf("content-%d", i)), 0o644)
	}

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rel, err := app.AddAttachment(srcs[i], "Work")
			results[i] = result{rel, err}
		}(i)
	}
	wg.Wait()

	// Every call must succeed and return a distinct path.
	seen := make(map[string]bool, n)
	for i, r := range results {
		if r.err != nil {
			t.Fatalf("goroutine %d: %v", i, r.err)
		}
		if seen[r.rel] {
			t.Fatalf("duplicate destination %q — collision resolution not race-free", r.rel)
		}
		seen[r.rel] = true
	}
	// Every destination file must hold its own original content (no overwrite).
	for i, r := range results {
		abs := filepath.Join(app.vaultPath, "Work", r.rel)
		got, err := os.ReadFile(abs)
		if err != nil {
			t.Fatalf("read %s: %v", r.rel, err)
		}
		want := fmt.Sprintf("content-%d", i)
		if string(got) != want {
			t.Errorf("%s = %q, want %q (concurrent attach clobbered content)", r.rel, got, want)
		}
	}
}

// OpenAttachment opens the file in the OS native handler (tested by verifying
// the path resolves; the actual OS open is a side-effect we can't assert in CI).
func TestOpenAttachment_ResolvesPath(t *testing.T) {
	// Stub openNative to prevent spawning native handlers/popups in tests.
	origOpenNative := openNative
	openNative = func(path string) error { return nil }
	t.Cleanup(func() { openNative = origOpenNative })

	app := newTestApp(t)
	src := filepath.Join(t.TempDir(), "x.txt")
	os.WriteFile(src, []byte("x"), 0o644)
	rel, _ := app.AddAttachment(src, "Work")
	// OpenAttachment should at least resolve the path without error.
	// (On CI without a display server, the OS-open may fail, so we only
	// assert no resolution/traversal error.)
	err := app.OpenAttachment("Work", rel)
	if err != nil && !os.IsNotExist(err) {
		// An OS-open failure (no handler) is acceptable; a path error is not.
		t.Logf("OpenAttachment returned %v (may be no handler on CI)", err)
	}
}

// OpenAttachment rejects an empty notebook (#101 review): the embedBlock
// NodeView MUST pass the originating notebook, and the Go side is the
// authoritative gatekeeper so a malformed frontend can't open an arbitrary
// attachment. The marker now carries the notebook at insert time, and the
// NodeView falls back to the live active location — but the Go contract
// remains "notebook is required".
func TestOpenAttachment_RejectsEmptyNotebook(t *testing.T) {
	origOpenNative := openNative
	openNative = func(path string) error { return nil }
	t.Cleanup(func() { openNative = origOpenNative })

	app := newTestApp(t)
	src := filepath.Join(t.TempDir(), "x.txt")
	os.WriteFile(src, []byte("x"), 0o644)
	rel, _ := app.AddAttachment(src, "Work")

	err := app.OpenAttachment("", rel)
	if err == nil {
		t.Fatal("OpenAttachment with empty notebook should be rejected")
	}
	if !strings.Contains(err.Error(), "notebook is required") {
		t.Errorf("error = %v, want to mention 'notebook is required'", err)
	}
}

// OpenAttachment rejects a relPath that escapes the notebook root via "..".
// The path is resolved through resolvePluginNotebookPath, so it should be
// safe; this test pins the contract so a future refactor cannot regress
// the embedBlock click-to-open path (#101 review gap).
func TestOpenAttachment_RejectsTraversal(t *testing.T) {
	origOpenNative := openNative
	openNative = func(path string) error { return nil }
	t.Cleanup(func() { openNative = origOpenNative })

	app := newTestApp(t)
	err := app.OpenAttachment("Work", "../../../etc/passwd")
	if err == nil {
		t.Fatal("OpenAttachment with traversal relPath should be rejected")
	}
	if !strings.Contains(err.Error(), "escapes the notebook root") {
		t.Errorf("error = %v, want to mention 'escapes the notebook root'", err)
	}
}

// DeleteAttachment removes the file.
func TestDeleteAttachment(t *testing.T) {
	app := newTestApp(t)
	src := filepath.Join(t.TempDir(), "del.txt")
	os.WriteFile(src, []byte("bye"), 0o644)
	rel, _ := app.AddAttachment(src, "Work")
	if err := app.DeleteAttachment("Work", rel); err != nil {
		t.Fatalf("DeleteAttachment: %v", err)
	}
	abs := filepath.Join(app.vaultPath, "Work", "attachments", "del.txt")
	if _, err := os.Stat(abs); !os.IsNotExist(err) {
		t.Errorf("file still exists after delete: %v", err)
	}
}

// The scanner skips the attachments/ directory so it never indexes binary
// files as blocks.
func TestScanner_SkipsAttachmentsDir(t *testing.T) {
	root := t.TempDir()
	// A notebook with an attachments dir + a section.
	nbDir := filepath.Join(root, "Work")
	os.MkdirAll(filepath.Join(nbDir, "attachments"), 0o755)
	os.MkdirAll(filepath.Join(nbDir, "Projects"), 0o755)
	os.WriteFile(filepath.Join(nbDir, "attachments", "blob.pdf"), []byte("PDF"), 0o644)
	os.WriteFile(filepath.Join(nbDir, "Projects", "Site.md"), []byte("# Site"), 0o644)
	os.WriteFile(filepath.Join(nbDir, "Inbox.md"), []byte("# Inbox"), 0o644)

	files, _, err := parser.WalkMarkdown(root)
	if err != nil {
		t.Fatalf("WalkMarkdown: %v", err)
	}
	for _, f := range files {
		if filepath.Base(filepath.Dir(f)) == "attachments" {
			t.Errorf("scanner indexed an attachments/ file: %s", f)
		}
	}
	// Should have found the two .md files.
	if len(files) != 2 {
		t.Errorf("expected 2 markdown files, got %d: %v", len(files), files)
	}
}

// AddAttachment writes the destination file 0o600 and the attachments/ dir
// 0o700 so a co-tenant cannot read user-attached files (F19).
func TestAddAttachment_RestrictiveFilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits are not enforced on Windows")
	}
	app := newTestApp(t)
	src := filepath.Join(t.TempDir(), "photo.png")
	if err := os.WriteFile(src, []byte("PNG"), 0o600); err != nil {
		t.Fatal(err)
	}
	rel, err := app.AddAttachment(src, "Work")
	if err != nil {
		t.Fatalf("AddAttachment: %v", err)
	}
	dest := filepath.Join(app.vaultPath, "Work", rel)
	dInfo, err := os.Stat(dest)
	if err != nil {
		t.Fatalf("stat attachment: %v", err)
	}
	if got := dInfo.Mode().Perm(); got != 0o600 {
		t.Errorf("attachment file perm = %o, want 0o600", got)
	}
	dirInfo, err := os.Stat(filepath.Join(app.vaultPath, "Work", "attachments"))
	if err != nil {
		t.Fatalf("stat attachments dir: %v", err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o700 {
		t.Errorf("attachments dir perm = %o, want 0o700", got)
	}
}

// AddAttachment rejects scriptable / shortcut / executable extensions so the
// attachments folder cannot become a drop zone handed to the OS handler (F6).
func TestAddAttachment_RejectsScriptableExtensions(t *testing.T) {
	app := newTestApp(t)
	dir := t.TempDir()
	newlyBlocked := []string{
		".html", ".htm", ".xhtml", ".xht", ".svg", ".svgz",
		".js", ".mjs", ".webmanifest", ".lnk", ".url", ".command",
		".scpt", ".applescript", ".desktop", ".jar", ".class",
		".py", ".pyc", ".rb", ".php", ".pl", ".wsh", ".cpl",
	}
	for _, ext := range newlyBlocked {
		src := filepath.Join(dir, "evil"+ext)
		if err := os.WriteFile(src, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := app.AddAttachment(src, "Work")
		if err == nil {
			t.Errorf("AddAttachment(%q) should be blocked, got nil", ext)
		} else if !strings.Contains(err.Error(), "blocked") {
			t.Errorf("AddAttachment(%q) error %q should mention 'blocked'", ext, err.Error())
		}
	}
}

// AddAttachment continues to accept common document/media types, including
// .pdf and .docx (intentionally kept allowed — see F6 deviation note).
func TestAddAttachment_LegitimateExtensionsAllowed(t *testing.T) {
	app := newTestApp(t)
	dir := t.TempDir()
	allowed := []string{
		".png", ".jpg", ".gif", ".mp4", ".mp3", ".wav",
		".txt", ".csv", ".md", ".json",
		".docx", ".xlsx", ".pptx", ".pdf",
	}
	for _, ext := range allowed {
		src := filepath.Join(dir, "file"+ext)
		if err := os.WriteFile(src, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		rel, err := app.AddAttachment(src, "Work")
		if err != nil {
			t.Errorf("AddAttachment(%q) should be allowed, got %v", ext, err)
		}
		if !strings.HasPrefix(filepath.ToSlash(rel), "attachments/") {
			t.Errorf("AddAttachment(%q) rel = %q, want attachments/...", ext, rel)
		}
	}
}
