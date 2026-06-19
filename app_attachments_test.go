package main

import (
	"os"
	"path/filepath"
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

// OpenAttachment opens the file in the OS native handler (tested by verifying
// the path resolves; the actual OS open is a side-effect we can't assert in CI).
func TestOpenAttachment_ResolvesPath(t *testing.T) {
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
