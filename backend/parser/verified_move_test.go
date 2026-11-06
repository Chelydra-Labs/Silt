package parser

// Verified-move primitive tests (#1021): MoveFileVerified removes the source
// only after its copy is verified durable, and every copy-side failure leaves
// the source intact with the failure typed as VerifiedMoveError. Torn copies
// ride the same VerifiedWriteAfterRenameHook seam as the write tests.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMoveFileVerified_Success(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "old.md")
	dst := filepath.Join(dir, "sub", "new.md")
	backup := filepath.Join(dir, ".wb", "sub", "new.md")
	content := []byte("---\npage: old\n---\nbody\n")
	if err := os.WriteFile(src, content, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := MoveFileVerified(src, dst, backup); err != nil {
		t.Fatalf("MoveFileVerified: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read moved file: %v", err)
	}
	if string(got) != string(content) {
		t.Fatalf("moved bytes = %q, want %q", got, content)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Errorf("source still exists after verified move (stat err = %v)", err)
	}
}

func TestCopyFileVerified_NeverTouchesSource(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.md")
	dst := filepath.Join(dir, "dst.md")
	backup := filepath.Join(dir, ".wb", "dst.md")
	content := []byte("stable source bytes\n")
	if err := os.WriteFile(src, content, 0o644); err != nil {
		t.Fatal(err)
	}

	// Every rename on the destination tears: the copy ladder exhausts and the
	// restore removes the torn destination — the source must be untouched.
	swapAfterRenameHook(t, 3)
	err := CopyFileVerified(src, dst, backup)
	if !IsVerifiedWriteError(err) {
		t.Fatalf("want verified-write failure, got %v", err)
	}
	got, rerr := os.ReadFile(src)
	if rerr != nil {
		t.Fatalf("source vanished after failed copy: %v", rerr)
	}
	if string(got) != string(content) {
		t.Fatalf("source bytes altered by a failed copy: %q", got)
	}
	if _, serr := os.Stat(dst); !os.IsNotExist(serr) {
		t.Errorf("destination holds a file after failed copy (stat err = %v)", serr)
	}
}

func TestMoveFileVerified_TornCopyKeepsSourceAndTypesError(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "old.md")
	dst := filepath.Join(dir, "new.md")
	backup := filepath.Join(dir, ".wb", "new.md")
	content := []byte("precious move body\n")
	if err := os.WriteFile(src, content, 0o644); err != nil {
		t.Fatal(err)
	}

	// write + retry torn; restore clean.
	swapAfterRenameHook(t, 2)
	err := MoveFileVerified(src, dst, backup)
	var vme *VerifiedMoveError
	if !errors.As(err, &vme) {
		t.Fatalf("want *VerifiedMoveError, got %v", err)
	}
	if vme.OldPath != src {
		t.Errorf("OldPath = %q, want %q", vme.OldPath, src)
	}
	if !IsVerifiedWriteError(err) {
		t.Errorf("VerifiedMoveError must stay a verified-write failure for existing consumers")
	}
	if !strings.Contains(err.Error(), "is still at") || !strings.Contains(err.Error(), src) {
		t.Errorf("error must name the intact original: %q", err.Error())
	}

	got, rerr := os.ReadFile(src)
	if rerr != nil {
		t.Fatalf("source vanished after torn move: %v", rerr)
	}
	if string(got) != string(content) {
		t.Fatalf("source bytes altered by a torn move: %q", got)
	}
	if _, serr := os.Stat(dst); !os.IsNotExist(serr) {
		t.Errorf("destination holds a torn partial (stat err = %v)", serr)
	}
	// The pre-move bytes are retained beside the destination's backup slot.
	pending, perr := os.ReadFile(backup + ".pending")
	if perr != nil || string(pending) != string(content) {
		t.Errorf("pending copy = %q (err = %v), want the pre-move bytes", pending, perr)
	}
}
