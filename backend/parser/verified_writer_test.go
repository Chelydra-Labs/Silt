package parser

// Verified staged-writer tests (#1009): backup + read-back verify, the retry
// ladder, restore-from-backup, and retained recovery copies. Torn
// destinations are simulated through the VerifiedWriteAfterRenameHook seam (a CI
// temp dir cannot reproduce a non-atomic rename for real).

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// swapAfterRenameHook installs a counting hook that corrupts the target after
// the first `corruptCalls` renames inside the verified writer (write, retry,
// restore order), simulating torn results.
func swapAfterRenameHook(t *testing.T, corruptCalls int) *int {
	t.Helper()
	orig := VerifiedWriteAfterRenameHook
	calls := 0
	VerifiedWriteAfterRenameHook = func(path string) error {
		calls++
		if calls <= corruptCalls {
			if err := os.WriteFile(path, []byte("TORN!"), 0o644); err != nil {
				return err
			}
		}
		return nil
	}
	t.Cleanup(func() { VerifiedWriteAfterRenameHook = orig })
	return &calls
}

func readOrEmpty(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

// Happy path: target replaced, pre-write backup retained as the single
// crash-recovery generation, and a stale .pending copy swept on success.
func TestWriteFileAtomicVerified_Success(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "nb", "page.md")
	backup := filepath.Join(dir, ".system", "writebackups", "nb", "page.md")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(backup), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backup+".pending", []byte("stale pending"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := WriteFileAtomicVerified(target, backup, []byte("new content")); err != nil {
		t.Fatalf("WriteFileAtomicVerified: %v", err)
	}
	if got := readOrEmpty(t, target); got != "new content" {
		t.Errorf("target = %q, want new content", got)
	}
	if got := readOrEmpty(t, backup); got != "old" {
		t.Errorf("backup = %q, want the pre-write bytes %q", got, "old")
	}
	if got := readOrEmpty(t, backup+".pending"); got != "" {
		t.Errorf("stale .pending survived a successful write: %q", got)
	}
}

// A torn first pass recovers via the single retry: second pass lands clean,
// no error, backup still retained.
func TestWriteFileAtomicVerified_TornOnceRetries(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "page.md")
	backup := filepath.Join(dir, ".system", "writebackups", "page.md")
	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	calls := swapAfterRenameHook(t, 1) // corrupt the write pass only

	if err := WriteFileAtomicVerified(target, backup, []byte("new")); err != nil {
		t.Fatalf("expected retry to recover, got %v", err)
	}
	if *calls != 2 {
		t.Errorf("hook calls = %d, want 2 (write + retry)", *calls)
	}
	if got := readOrEmpty(t, target); got != "new" {
		t.Errorf("target = %q, want %q", got, "new")
	}
	if got := readOrEmpty(t, backup); got != "old" {
		t.Errorf("backup = %q, want %q", got, "old")
	}
}

// Torn write + torn retry → restore from the pre-write bytes; the incoming
// bytes are retained next to the backup; the typed error says both survived.
func TestWriteFileAtomicVerified_TornTwiceRestores(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "nb", "sec", "page.md")
	backup := filepath.Join(dir, ".system", "writebackups", "nb", "sec", "page.md")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	swapAfterRenameHook(t, 2) // write + retry torn; restore lands clean

	err := WriteFileAtomicVerified(target, backup, []byte("user's new text"))
	var vwe *VerifiedWriteError
	if !errors.As(err, &vwe) {
		t.Fatalf("want *VerifiedWriteError, got %v", err)
	}
	if !vwe.Restored {
		t.Errorf("Restored = false, want true (original restored)")
	}
	if got := readOrEmpty(t, target); got != "original" {
		t.Errorf("target = %q, want the original restored", got)
	}
	if got := readOrEmpty(t, backup); got != "original" {
		t.Errorf("backup = %q, want the pre-write bytes", got)
	}
	if got := readOrEmpty(t, backup+".pending"); got != "user's new text" {
		t.Errorf("pending = %q, want the incoming bytes retained", got)
	}
	if !strings.Contains(vwe.Error(), backup) || !strings.Contains(vwe.Error(), backup+".pending") {
		t.Errorf("error must name the retained copies: %v", vwe)
	}
}

// New page torn on both passes: the pre-write state is "absent", so the torn
// target is removed (never a partial first page) and the text is retained.
func TestWriteFileAtomicVerified_TornNewPageRemoved(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "page.md")
	backup := filepath.Join(dir, ".system", "writebackups", "page.md")
	swapAfterRenameHook(t, 2)

	err := WriteFileAtomicVerified(target, backup, []byte("first ever"))
	var vwe *VerifiedWriteError
	if !errors.As(err, &vwe) {
		t.Fatalf("want *VerifiedWriteError, got %v", err)
	}
	if !vwe.Restored {
		t.Errorf("Restored = false, want true (absent pre-write state restored)")
	}
	if _, statErr := os.Stat(target); !os.IsNotExist(statErr) {
		t.Errorf("torn new page still present: %v", statErr)
	}
	if got := readOrEmpty(t, backup+".pending"); got != "first ever" {
		t.Errorf("pending = %q, want the incoming bytes retained", got)
	}
}

// Even the restore lands torn → hard failure, Restored=false, backup intact.
func TestWriteFileAtomicVerified_HardFailureRetainsBackup(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "page.md")
	backup := filepath.Join(dir, ".system", "writebackups", "page.md")
	if err := os.WriteFile(target, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	swapAfterRenameHook(t, 3) // write + retry + restore all torn

	err := WriteFileAtomicVerified(target, backup, []byte("new"))
	var vwe *VerifiedWriteError
	if !errors.As(err, &vwe) {
		t.Fatalf("want *VerifiedWriteError, got %v", err)
	}
	if vwe.Restored {
		t.Errorf("Restored = true, want false (restore itself was torn)")
	}
	if got := readOrEmpty(t, backup); got != "original" {
		t.Errorf("backup = %q, want the retained pre-write bytes", got)
	}
	if got := readOrEmpty(t, backup+".pending"); got != "new" {
		t.Errorf("pending = %q, want the incoming bytes retained", got)
	}
}

// One generation: the next write's backup copy sweeps the previous
// generation — after write 2 the backup holds write 1's result, not write 0's.
func TestWriteFileAtomicVerified_BackupOneGenerationSweptByNextWrite(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "page.md")
	backup := filepath.Join(dir, ".system", "writebackups", "page.md")
	if err := os.WriteFile(target, []byte("gen0"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, content := range []string{"gen1", "gen2"} {
		if err := WriteFileAtomicVerified(target, backup, []byte(content)); err != nil {
			t.Fatalf("write %s: %v", content, err)
		}
	}
	if got := readOrEmpty(t, backup); got != "gen1" {
		t.Errorf("backup = %q, want gen1 (gen0 swept by write 2)", got)
	}
	if got := readOrEmpty(t, target); got != "gen2" {
		t.Errorf("target = %q, want gen2", got)
	}
}

// Empty backupPath keeps verification + in-memory restore but writes no
// recovery copies (caller without a backup location).
func TestWriteFileAtomicVerified_EmptyBackupPathStillVerifies(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "page.md")
	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	swapAfterRenameHook(t, 2)

	err := WriteFileAtomicVerified(target, "", []byte("new"))
	var vwe *VerifiedWriteError
	if !errors.As(err, &vwe) {
		t.Fatalf("want *VerifiedWriteError, got %v", err)
	}
	if !vwe.Restored {
		t.Errorf("Restored = false, want true")
	}
	if got := readOrEmpty(t, target); got != "old" {
		t.Errorf("target = %q, want restored %q", got, "old")
	}
	if vwe.Pending != "" {
		t.Errorf("Pending = %q, want empty for backupless mode", vwe.Pending)
	}
}
