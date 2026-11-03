package parser

// Verified staged-writer tests (#1009): backup + read-back verify, the retry
// ladder, restore-from-backup, and retained recovery copies. Torn
// destinations are simulated through the VerifiedWriteAfterRenameHook seam (a CI
// temp dir cannot reproduce a non-atomic rename for real).

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
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

// pseudoRandomBytes generates deterministic non-repetitive content — hashing
// must distinguish every region, so a run-length or patterned filler would
// make a digest bug (e.g. hashing only a prefix) invisible.
func pseudoRandomBytes(n int) []byte {
	b := make([]byte, n)
	state := uint32(0x12345678)
	for i := range b {
		state = state*1664525 + 1013904223
		b[i] = byte(state >> 16)
	}
	return b
}

// #1019: with a backup location, the pre-write bytes must NOT be buffered in
// memory — only their digest is kept (the restore reads the durable copy back
// on the rare failure path). The old implementation returned the full slice,
// which tripled resident memory on plugin-scale writes.
func TestBackupTarget_WithBackupLocationDoesNotBufferPrev(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "page.md")
	backup := filepath.Join(dir, ".system", "writebackups", "page.md")
	content := pseudoRandomBytes(256 * 1024)
	if err := os.WriteFile(target, content, 0o644); err != nil {
		t.Fatal(err)
	}

	b, err := backupTarget(target, backup)
	if err != nil {
		t.Fatalf("backupTarget: %v", err)
	}
	if !b.hadPrev {
		t.Fatal("hadPrev = false, want true (target existed)")
	}
	if b.prev != nil {
		t.Errorf("prev bytes buffered in memory (len=%d) — the backup path must stream, keeping only the digest", len(b.prev))
	}
	if b.digest.size != int64(len(content)) {
		t.Errorf("digest.size = %d, want %d", b.digest.size, len(content))
	}
	if b.digest.sum != digestOf(content).sum {
		t.Error("digest does not match the pre-write bytes")
	}
	got, err := os.ReadFile(backup)
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Error("backup file content does not match the pre-write bytes")
	}
}

// #1019: end-to-end over the streaming paths with plugin-scale-ish content:
// the digest-based read-back must verify multi-hundred-KB writes, and the
// recovery ladder must restore the original from the on-disk backup (never
// from resident pre-write bytes — there are none).
func TestWriteFileAtomicVerified_LargeContentStreamsAndRestores(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "nb", "big.md")
	backup := filepath.Join(dir, ".system", "writebackups", "nb", "big.md")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	original := pseudoRandomBytes(3 * 1024 * 1024)
	if err := os.WriteFile(target, original, 0o644); err != nil {
		t.Fatal(err)
	}

	// Success path: streaming read-back verifies a large replacement.
	replacement := pseudoRandomBytes(3*1024*1024 + 7)
	if err := WriteFileAtomicVerified(target, backup, replacement); err != nil {
		t.Fatalf("verified write of large content: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, replacement) {
		t.Error("target does not hold the large replacement bytes")
	}

	// Failure path: torn twice → restore must come from the on-disk backup
	// (the pre-write bytes of THIS write — replacement — were never held in
	// memory; the original generation was swept by this write's step 1).
	swapAfterRenameHook(t, 2)
	err = WriteFileAtomicVerified(target, backup, pseudoRandomBytes(1024*1024))
	var vwe *VerifiedWriteError
	if !errors.As(err, &vwe) {
		t.Fatalf("want *VerifiedWriteError, got %v", err)
	}
	if !vwe.Restored {
		t.Fatalf("Restored = false, want true: %v", vwe)
	}
	got, err = os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, replacement) {
		t.Error("restore did not reproduce the pre-write bytes from the backup")
	}
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

// Pre-write backup failure: nothing was written and no copy exists — the
// error must say the file was not modified and must NOT name a phantom
// backup/pending path (the user would be sent to files that do not exist).
func TestWriteFileAtomicVerified_BackupFailureNamesNoPhantomCopies(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "page.md")
	backup := filepath.Join(dir, ".system", "writebackups", "page.md")
	if err := os.WriteFile(target, []byte("untouched"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A regular FILE where the backup directory must go fails the backup
	// copy (MkdirAll cannot traverse it) without touching the target.
	if err := os.MkdirAll(filepath.Join(dir, ".system"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".system", "writebackups"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := WriteFileAtomicVerified(target, backup, []byte("new text"))
	var vwe *VerifiedWriteError
	if !errors.As(err, &vwe) {
		t.Fatalf("want *VerifiedWriteError, got %v", err)
	}
	if !vwe.PreWriteFailed {
		t.Errorf("PreWriteFailed = false, want true (the backup step failed)")
	}
	if vwe.Backup != "" || vwe.Pending != "" {
		t.Errorf("phantom copy paths (Backup=%q, Pending=%q): the prose must not name files that do not exist", vwe.Backup, vwe.Pending)
	}
	if got := readOrEmpty(t, target); got != "untouched" {
		t.Errorf("target = %q, want the untouched original", got)
	}
	msg := vwe.Error()
	if !strings.Contains(msg, "was not modified") || !strings.Contains(msg, "Neither a backup nor an unsaved copy") {
		t.Errorf("prose must state the file was not modified and no copy exists: %q", msg)
	}
	if strings.Contains(msg, backup) {
		t.Errorf("prose names the never-written backup path: %q", msg)
	}
}

// Retention failure: the pending copy is NOT named — the prose points only
// at the backup that did land and says the unsaved text was not retained.
// This is the branch that makes the RetentionErr truth reachable.
func TestWriteFileAtomicVerified_RetentionFailureNamesNoPhantomPending(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "page.md")
	backup := filepath.Join(dir, "b.md")
	if err := os.WriteFile(target, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A DIRECTORY at the pending path fails the retention copy (open for
	// write on a directory) while the backup copy lands normally.
	if err := os.MkdirAll(backup+".pending", 0o700); err != nil {
		t.Fatal(err)
	}
	swapAfterRenameHook(t, 2) // write + retry torn; restore lands clean

	err := WriteFileAtomicVerified(target, backup, []byte("new text"))
	var vwe *VerifiedWriteError
	if !errors.As(err, &vwe) {
		t.Fatalf("want *VerifiedWriteError, got %v", err)
	}
	if !vwe.Restored {
		t.Errorf("Restored = false, want true (retention failure must not block the restore)")
	}
	if vwe.Pending != "" {
		t.Errorf("Pending = %q, want empty (retention failed)", vwe.Pending)
	}
	if vwe.RetentionErr == nil {
		t.Errorf("RetentionErr = nil, want the retention failure recorded")
	}
	msg := vwe.Error()
	if !strings.Contains(msg, backup) {
		t.Errorf("prose must name the backup that did land: %q", msg)
	}
	// The OS cause may echo the failed path, but the prose must never CLAIM
	// the unsaved text was kept there.
	if strings.Contains(msg, "unsaved text is kept at") {
		t.Errorf("prose claims retention at the never-written pending path: %q", msg)
	}
	if !strings.Contains(msg, "could not be retained on disk") {
		t.Errorf("prose must say the unsaved text was not retained: %q", msg)
	}
	if got := readOrEmpty(t, target); got != "original" {
		t.Errorf("target = %q, want the original restored", got)
	}
}

// New-page failure: no pre-write backup exists (the page had no previous
// version), so the prose must not offer one.
func TestWriteFileAtomicVerified_NewPageFailureNamesNoPhantomBackup(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "page.md")
	backup := filepath.Join(dir, "b.md")
	swapAfterRenameHook(t, 2) // write + retry torn; new-page "restore" removes

	err := WriteFileAtomicVerified(target, backup, []byte("first"))
	var vwe *VerifiedWriteError
	if !errors.As(err, &vwe) {
		t.Fatalf("want *VerifiedWriteError, got %v", err)
	}
	if vwe.Backup != "" {
		t.Errorf("Backup = %q, want empty (no previous version existed to back up)", vwe.Backup)
	}
	if vwe.Pending == "" {
		t.Errorf("Pending empty, want the incoming bytes retained")
	}
	msg := vwe.Error()
	if strings.Contains(msg, backup) && !strings.Contains(msg, backup+".pending") {
		t.Errorf("prose names the phantom backup path: %q", msg)
	}
	// A new page has no previous version: the restored state is "absent", so
	// the lead must say the torn partial file was removed, not point the user
	// at a pre-existing version that never existed.
	if !strings.Contains(msg, "the partial file was removed") {
		t.Errorf("new-page lead must say the partial file was removed: %q", msg)
	}
	if strings.Contains(msg, "previous version was restored") {
		t.Errorf("new-page lead claims a previous version existed: %q", msg)
	}
}

// New-page failure with failed retention: neither copy exists, so the default
// prose branch runs — it must carry the retention cause (the OS error exists
// nowhere else for the user; Unwrap exposes only the step error).
func TestWriteFileAtomicVerified_NewPageRetentionFailureNamesCause(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "page.md")
	backup := filepath.Join(dir, "b.md")
	// A DIRECTORY at the pending path fails the retention copy while the
	// new-page removal still lands (absent state restored).
	if err := os.MkdirAll(backup+".pending", 0o700); err != nil {
		t.Fatal(err)
	}
	swapAfterRenameHook(t, 2) // write + retry torn

	err := WriteFileAtomicVerified(target, backup, []byte("first"))
	var vwe *VerifiedWriteError
	if !errors.As(err, &vwe) {
		t.Fatalf("want *VerifiedWriteError, got %v", err)
	}
	if !vwe.Restored {
		t.Errorf("Restored = false, want true (the torn new page was removed)")
	}
	if vwe.RetentionErr == nil {
		t.Fatalf("RetentionErr = nil, want the retention failure recorded")
	}
	msg := vwe.Error()
	if !strings.Contains(msg, "Neither a backup nor an unsaved copy could be retained on disk") {
		t.Errorf("prose must state no copy exists: %q", msg)
	}
	if !strings.Contains(msg, vwe.RetentionErr.Error()) {
		t.Errorf("default branch must carry the retention cause (else it is server-log-only): %q", msg)
	}
	if got := readOrEmpty(t, target); got != "" {
		t.Errorf("target = %q, want the torn file removed", got)
	}
}

// New-page failure where even the torn-file removal fails (a wedged
// non-atomic mount): no previous version ever existed, so the default-branch
// lead must not claim one — it must say the torn partial file is still at the
// target, where the next startup scan will index it.
func TestWriteFileAtomicVerified_NewPageRemovalFailedNamesTornRemains(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory mode bits do not gate removal on Windows")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "page.md")
	// Keep the backup outside dir's mode change so .pending retention still
	// lands and the prose names it (the recovery path must survive this corner).
	backup := filepath.Join(dir, "backups", "b.md")
	if err := os.MkdirAll(filepath.Dir(backup), 0o700); err != nil {
		t.Fatal(err)
	}
	orig := VerifiedWriteAfterRenameHook
	calls := 0
	VerifiedWriteAfterRenameHook = func(path string) error {
		calls++
		if err := os.WriteFile(path, []byte("TORN!"), 0o644); err != nil {
			return err
		}
		if calls == 2 {
			// Write and retry are torn; revoke write on the target's
			// directory so the recovery os.Remove fails with EACCES.
			return os.Chmod(dir, 0o500)
		}
		return nil
	}
	t.Cleanup(func() {
		VerifiedWriteAfterRenameHook = orig
		_ = os.Chmod(dir, 0o755)
	})

	err := WriteFileAtomicVerified(target, backup, []byte("first"))
	var vwe *VerifiedWriteError
	if !errors.As(err, &vwe) {
		t.Fatalf("want *VerifiedWriteError, got %v", err)
	}
	if vwe.Restored {
		t.Skip("removal succeeded despite the read-only directory (running privileged?)")
	}
	msg := vwe.Error()
	if !strings.Contains(msg, "torn partial file remains at") {
		t.Errorf("new-page removal-failed lead must say the torn file remains: %q", msg)
	}
	if !strings.Contains(msg, target) {
		t.Errorf("lead must name the target path the torn file remains at: %q", msg)
	}
	if strings.Contains(msg, "previous version") {
		t.Errorf("new page had no previous version to claim: %q", msg)
	}
	if !strings.Contains(msg, backup+".pending") {
		t.Errorf("prose must still name the retained .pending copy: %q", msg)
	}
	// The prose's claim is true: the torn bytes are still at the target.
	if got := readOrEmpty(t, target); got != "TORN!" {
		t.Errorf("target = %q, want the torn bytes still present", got)
	}
}
