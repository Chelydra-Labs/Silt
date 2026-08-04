package types

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"silt/backend/parser"
)

const validUserType = `name: Demo
description: A demo type
properties:
  - name: title
    type: text
    required: true
  - name: count
    type: number
`

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}

// TestTypeWatcher_AddModifyDelete drives a real fsnotify instance (the
// recommended approach for fsnotify tests: "drive a real fsnotify instance
// in tests rather than mocking") and verifies the onChange callback fires
// on external add/modify/delete of a type file.
func TestTypeWatcher_AddModifyDelete(t *testing.T) {
	dir := t.TempDir()
	typesDir := filepath.Join(dir, "types")
	// Pre-create the dir so the watcher observes it directly.
	writeTypeFile(t, typesDir, ".gitkeep", "")

	var mu sync.Mutex
	var callCount int
	changed := make(chan struct{}, 16)

	w, err := NewTypeWatcher(typesDir, func() {
		mu.Lock()
		callCount++
		mu.Unlock()
		select {
		case changed <- struct{}{}:
		default:
		}
	})
	if err != nil {
		t.Fatalf("NewTypeWatcher: %v", err)
	}
	w.Start()
	defer w.Close()
	time.Sleep(100 * time.Millisecond)

	waitForTypeChange(t, changed, "add file", func() {
		writeTypeFile(t, typesDir, "demo.yaml", validUserType)
	})
	waitForTypeChange(t, changed, "modify file", func() {
		if err := writeFile(filepath.Join(typesDir, "demo.yaml"), validUserType+"\n# extra\n"); err != nil {
			t.Fatalf("modify: %v", err)
		}
	})
	waitForTypeChange(t, changed, "delete file", func() {
		_ = os.Remove(filepath.Join(typesDir, "demo.yaml"))
	})

	mu.Lock()
	if callCount < 3 {
		t.Errorf("expected at least 3 onChange calls, got %d", callCount)
	}
	mu.Unlock()
}

// TestTypeWatcher_SelfWriteSuppressed arms a self-write with the EXACT bytes
// SaveType will write, then runs SaveType (the production atomic-write path
// via parser.WriteFileAtomic: temp + fsync + rename). The post-write event
// burst must be recognized as a confirmed self-write and suppressed — no
// onChange, no feedback loop. This implicitly covers atomic-rename event
// coalescing: the temp Create/Remove + final Create/Write events collapse to
// one debounced comparison against the settled bytes.
func TestTypeWatcher_SelfWriteSuppressed(t *testing.T) {
	dir := t.TempDir()
	typesDir := filepath.Join(dir, "types")
	td := &TypeDef{ID: "demo", Name: "Demo", Properties: []PropertyDef{{Name: "title", Type: PropText}}}
	expected := SerializeType(td)

	changed := make(chan struct{}, 8)
	w, err := NewTypeWatcher(typesDir, func() {
		select {
		case changed <- struct{}{}:
		default:
		}
	})
	if err != nil {
		t.Fatalf("NewTypeWatcher: %v", err)
	}
	w.Start()
	defer w.Close()
	time.Sleep(100 * time.Millisecond)

	demoPath := filepath.Join(typesDir, "demo.yaml")
	// Arm with the exact canonical bytes SaveType will write.
	w.RegisterSelfWrite(demoPath, expected)
	if err := SaveType(typesDir, td); err != nil {
		t.Fatalf("SaveType: %v", err)
	}
	select {
	case <-changed:
		t.Error("confirmed self-write should be suppressed, but onChange fired")
	case <-time.After(SelfWriteSuppressionTimeout):
		// Good — no callback within the suppression window.
	}

	// The arm must have been consumed so a later external edit is not
	// silently dropped by a stale arm.
	if _, armed := armedPathForTest(t, w, demoPath); armed {
		t.Error("matched self-write arm should be consumed after revalidation")
	}
}

// TestTypeWatcher_SelfWriteViaAtomicRename_MultipleEventsCoalesced writes
// via parser.WriteFileAtomic directly (the exact code path SaveType uses)
// and asserts the resulting multi-event burst collapses to one
// content-equality decision that suppresses. Pins the atomic-rename path.
func TestTypeWatcher_SelfWriteViaAtomicRename_MultipleEventsCoalesced(t *testing.T) {
	dir := t.TempDir()
	typesDir := filepath.Join(dir, "types")
	writeTypeFile(t, typesDir, ".gitkeep", "")

	changed := make(chan struct{}, 8)
	w, err := NewTypeWatcher(typesDir, func() {
		select {
		case changed <- struct{}{}:
		default:
		}
	})
	if err != nil {
		t.Fatalf("NewTypeWatcher: %v", err)
	}
	w.Start()
	defer w.Close()
	time.Sleep(100 * time.Millisecond)

	demoPath := filepath.Join(typesDir, "demo.yaml")
	payload := []byte("name: Demo\nproperties:\n  - name: title\n    type: text\n")
	w.RegisterSelfWrite(demoPath, payload)
	if err := parser.WriteFileAtomic(demoPath, payload); err != nil {
		t.Fatalf("WriteFileAtomic: %v", err)
	}
	select {
	case <-changed:
		t.Error("atomic-rename self-write should be suppressed after coalescing")
	case <-time.After(SelfWriteSuppressionTimeout):
		// Good — single coalesced decision matched and suppressed.
	}
}

// TestTypeWatcher_ExternalEditDuringSuppressionFiresReload is the Phase 3/#872
// core fix. The old path+time-window suppression dropped ANY event for the
// armed path inside the window, so a coincident external/sync edit to the
// SAME file was silently lost. Content-identity revalidation fixes this: the
// post-write bytes are read once after the burst settles, and if they differ
// from what we armed (an external editor overwrote our save), onChange must
// fire so the new content is picked up.
func TestTypeWatcher_ExternalEditDuringSuppressionFiresReload(t *testing.T) {
	dir := t.TempDir()
	typesDir := filepath.Join(dir, "types")
	td := &TypeDef{ID: "demo", Name: "Demo", Properties: []PropertyDef{{Name: "title", Type: PropText}}}
	armed := SerializeType(td)

	changed := make(chan struct{}, 8)
	w, err := NewTypeWatcher(typesDir, func() {
		select {
		case changed <- struct{}{}:
		default:
		}
	})
	if err != nil {
		t.Fatalf("NewTypeWatcher: %v", err)
	}
	w.Start()
	defer w.Close()
	time.Sleep(100 * time.Millisecond)

	demoPath := filepath.Join(typesDir, "demo.yaml")
	// Arm with our intended bytes, then complete our save (writes `armed`).
	w.RegisterSelfWrite(demoPath, armed)
	if err := SaveType(typesDir, td); err != nil {
		t.Fatalf("SaveType: %v", err)
	}
	// Immediately overwrite with DIFFERENT bytes — an external editor or a
	// sync client racing in during the suppression window.
	external := []byte("name: External\nproperties:\n  - name: x\n    type: text\n")
	if err := os.WriteFile(demoPath, external, 0o644); err != nil {
		t.Fatalf("external overwrite: %v", err)
	}

	select {
	case <-changed:
		// expected — settled bytes differ from armed, so reload fires.
	case <-time.After(2 * time.Second):
		t.Fatal("external edit to an armed path was silently suppressed — content-identity revalidation did not fire reload")
	}

	// The stale arm is cleared either way (matched-then-overwritten path or
	// mismatch path), so a later edit still lands.
	if _, stillArmed := armedPathForTest(t, w, demoPath); stillArmed {
		t.Error("stale arm should be cleared after revalidation fired")
	}
}

// TestTypeWatcher_SelfWritePathScoped pins cross-file isolation: a self-write
// arm on type A must NOT suppress an external edit to type B arriving inside
// the same window. Whole-directory suppression dropped coincident
// sync/external edits — content-identity is path-scoped to prevent that.
func TestTypeWatcher_SelfWritePathScoped(t *testing.T) {
	dir := t.TempDir()
	typesDir := filepath.Join(dir, "types")
	writeTypeFile(t, typesDir, "a.yaml", validUserType)
	writeTypeFile(t, typesDir, "b.yaml", validUserType)

	changed := make(chan struct{}, 8)
	w, err := NewTypeWatcher(typesDir, func() {
		select {
		case changed <- struct{}{}:
		default:
		}
	})
	if err != nil {
		t.Fatalf("NewTypeWatcher: %v", err)
	}
	w.Start()
	defer w.Close()
	time.Sleep(100 * time.Millisecond)

	// Arm suppression for A only, then write B externally — B must fire.
	w.RegisterSelfWrite(filepath.Join(typesDir, "a.yaml"), []byte(validUserType+"\n# self-a\n"))
	if err := writeFile(filepath.Join(typesDir, "b.yaml"), validUserType+"\n# external-b\n"); err != nil {
		t.Fatalf("external write B: %v", err)
	}
	select {
	case <-changed:
		// expected — B is not under the self-write arm.
	case <-time.After(SelfWriteSuppressionTimeout):
		t.Fatal("external edit to type B was suppressed by a self-write arm on type A")
	}
}

// TestTypeWatcher_UnregisterSelfWrite_ClearsWindow verifies that a save that
// fails after arming clears every arm (failed-write cleanup), so an external
// edit landing right after the failed save is NOT silently dropped.
func TestTypeWatcher_UnregisterSelfWrite_ClearsWindow(t *testing.T) {
	dir := t.TempDir()
	typesDir := filepath.Join(dir, "types")
	writeTypeFile(t, typesDir, "demo.yaml", validUserType)

	changed := make(chan struct{}, 8)
	w, err := NewTypeWatcher(typesDir, func() {
		select {
		case changed <- struct{}{}:
		default:
		}
	})
	if err != nil {
		t.Fatalf("NewTypeWatcher: %v", err)
	}
	w.Start()
	defer w.Close()

	// Arm then clear (a failed save's cleanup), then an external edit must land.
	w.RegisterSelfWrite(filepath.Join(typesDir, "demo.yaml"), []byte(validUserType+"\n# self\n"))
	w.UnregisterSelfWrite()
	if err := writeFile(filepath.Join(typesDir, "demo.yaml"), validUserType+"\n# external\n"); err != nil {
		t.Fatalf("external write: %v", err)
	}
	select {
	case <-changed:
		// expected
	case <-time.After(SelfWriteSuppressionTimeout):
		t.Fatalf("external edit was suppressed — UnregisterSelfWrite did not clear the arm")
	}
}

// TestTypeWatcher_SelfDeleteSuppressed arms a delete (nil expected = file
// should not exist after) then removes the file. The settled state (file
// absent) matches the armed expectation, so onChange must NOT fire — the
// in-app DeleteType path does its own InvalidateTypesCache + reproject +
// emit, and the watcher echo would be redundant.
func TestTypeWatcher_SelfDeleteSuppressed(t *testing.T) {
	dir := t.TempDir()
	typesDir := filepath.Join(dir, "types")
	td := &TypeDef{ID: "demo", Name: "Demo", Properties: []PropertyDef{{Name: "title", Type: PropText}}}
	writeTypeFile(t, typesDir, "demo.yaml", string(SerializeType(td)))

	changed := make(chan struct{}, 8)
	w, err := NewTypeWatcher(typesDir, func() {
		select {
		case changed <- struct{}{}:
		default:
		}
	})
	if err != nil {
		t.Fatalf("NewTypeWatcher: %v", err)
	}
	w.Start()
	defer w.Close()
	time.Sleep(100 * time.Millisecond)

	demoPath := filepath.Join(typesDir, "demo.yaml")
	// nil expected arms a delete: file should be absent after the burst.
	w.RegisterSelfWrite(demoPath, nil)
	if err := DeleteType(typesDir, "demo"); err != nil {
		t.Fatalf("DeleteType: %v", err)
	}
	select {
	case <-changed:
		t.Error("confirmed delete (file absent as armed) should be suppressed")
	case <-time.After(SelfWriteSuppressionTimeout):
		// Good — settled state matches armed (nil), no reload.
	}
}

// TestTypeWatcher_DeleteThenRecreateFiresReload arms a delete, removes the
// file, then immediately recreates it with different bytes (a sync client
// restoring the file, or an editor racing). The settled state (file exists)
// differs from the armed expectation (absent), so onChange must fire — the
// recreate is a real external change and must not be hidden by the delete arm.
func TestTypeWatcher_DeleteThenRecreateFiresReload(t *testing.T) {
	dir := t.TempDir()
	typesDir := filepath.Join(dir, "types")
	writeTypeFile(t, typesDir, "demo.yaml", validUserType)

	changed := make(chan struct{}, 8)
	w, err := NewTypeWatcher(typesDir, func() {
		select {
		case changed <- struct{}{}:
		default:
		}
	})
	if err != nil {
		t.Fatalf("NewTypeWatcher: %v", err)
	}
	w.Start()
	defer w.Close()
	time.Sleep(100 * time.Millisecond)

	demoPath := filepath.Join(typesDir, "demo.yaml")
	// Arm delete, then remove + recreate externally in the same burst.
	w.RegisterSelfWrite(demoPath, nil)
	if err := os.Remove(demoPath); err != nil {
		t.Fatalf("remove: %v", err)
	}
	recreate := []byte("name: Recreated\nproperties:\n  - name: x\n    type: text\n")
	if err := os.WriteFile(demoPath, recreate, 0o644); err != nil {
		t.Fatalf("recreate: %v", err)
	}

	select {
	case <-changed:
		// expected — settled state (file exists) differs from armed (absent).
	case <-time.After(2 * time.Second):
		t.Fatal("delete-then-recreate was suppressed — nil-expected delete arm did not detect the recreate")
	}
}

// TestTypeWatcher_ExpiredArmNotSuppressed pins the safety net: if fsnotify
// drops the post-write event entirely (e.g. inotify watch limit reached), the
// arm must not live forever silently suppressing a later real external edit.
// After selfWriteWindow the arm is pruned and a subsequent edit fires reload.
func TestTypeWatcher_ExpiredArmNotSuppressed(t *testing.T) {
	dir := t.TempDir()
	typesDir := filepath.Join(dir, "types")
	writeTypeFile(t, typesDir, "demo.yaml", validUserType)

	changed := make(chan struct{}, 8)
	w, err := NewTypeWatcher(typesDir, func() {
		select {
		case changed <- struct{}{}:
		default:
		}
	})
	if err != nil {
		t.Fatalf("NewTypeWatcher: %v", err)
	}
	w.Start()
	defer w.Close()

	demoPath := filepath.Join(typesDir, "demo.yaml")
	// Arm with an artificially old armedAt so the arm is already expired
	// when the event arrives — no real sleep needed.
	w.selfMu.Lock()
	w.selfArmed[filepath.Clean(demoPath)] = selfEntry{
		expected: []byte(validUserType + "\n# self\n"),
		armedAt:  time.Now().Add(-2 * selfWriteWindow),
	}
	w.selfMu.Unlock()

	if err := writeFile(demoPath, validUserType+"\n# external\n"); err != nil {
		t.Fatalf("external write: %v", err)
	}
	select {
	case <-changed:
		// expected — the arm was expired, so the edit landed.
	case <-time.After(SelfWriteSuppressionTimeout):
		t.Fatal("expired arm suppressed an external edit — safety-net pruning failed")
	}
}

func TestTypeWatcher_IgnoresNonYaml(t *testing.T) {
	dir := t.TempDir()
	typesDir := filepath.Join(dir, "types")
	writeTypeFile(t, typesDir, ".gitkeep", "")

	changed := make(chan struct{}, 8)
	w, err := NewTypeWatcher(typesDir, func() {
		select {
		case changed <- struct{}{}:
		default:
		}
	})
	if err != nil {
		t.Fatalf("NewTypeWatcher: %v", err)
	}
	w.Start()
	defer w.Close()
	time.Sleep(100 * time.Millisecond)

	// A .txt file must NOT trigger; a .yml file MUST.
	writeTypeFile(t, typesDir, "readme.txt", "not a type")
	select {
	case <-changed:
		t.Error("non-yaml file should not trigger callback")
	case <-time.After(500 * time.Millisecond):
		// Good.
	}

	waitForTypeChange(t, changed, "add .yml file", func() {
		writeTypeFile(t, typesDir, "alt.yml", validUserType)
	})
}

func TestTypeWatcher_Close(t *testing.T) {
	dir := t.TempDir()
	typesDir := filepath.Join(dir, "types")
	w, err := NewTypeWatcher(typesDir, func() {})
	if err != nil {
		t.Fatalf("NewTypeWatcher: %v", err)
	}
	w.Start()
	if err := w.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	_ = w.Close() // double-close is safe
}

// TestTypeWatcher_CloseMidBurst_NoDeadlock pins Close/shutdown safety: closing
// the watcher while a burst is mid-debounce must return without hanging. The
// loop goroutine sees the closed Events channel (or stopCh) and exits;
// wg.Wait in Close blocks until it does. Calling Close mid-burst is the
// realistic shutdown race (user closes the vault while a sync edit is
// landing), so this guards against the MB-1-class regression in isolation
// from the app-level CloseVault test.
func TestTypeWatcher_CloseMidBurst_NoDeadlock(t *testing.T) {
	dir := t.TempDir()
	typesDir := filepath.Join(dir, "types")
	writeTypeFile(t, typesDir, ".gitkeep", "")

	w, err := NewTypeWatcher(typesDir, func() {})
	if err != nil {
		t.Fatalf("NewTypeWatcher: %v", err)
	}
	w.Start()
	time.Sleep(100 * time.Millisecond)

	// Stagger several writes so events are in-flight when Close runs, then
	// close immediately. Close must drain (wg.Wait) and return.
	for i := 0; i < 5; i++ {
		_ = os.WriteFile(filepath.Join(typesDir, "burst.yaml"),
			[]byte("name: B\nproperties:\n  - name: x\n    type: text\n"), 0o644)
	}
	time.Sleep(reloadDebounce / 2)
	done := make(chan struct{})
	go func() {
		_ = w.Close()
		close(done)
	}()
	select {
	case <-done:
		// Good — Close returned despite the in-flight burst.
	case <-time.After(5 * time.Second):
		t.Fatal("Close deadlocked with an in-flight event burst")
	}
	_ = w.Close() // double-close is still safe.
}

func TestIsTypeFile(t *testing.T) {
	w := &TypeWatcher{typesDir: filepath.Join("vault", ".system", "types")}
	cases := []struct {
		path string
		want bool
	}{
		{filepath.Join("vault", ".system", "types", "book.yaml"), true},
		{filepath.Join("vault", ".system", "types", "book.YML"), true},
		{filepath.Join("vault", ".system", "types", "readme.txt"), false},
		{filepath.Join("vault", ".system", "types", "sub", "x.yaml"), false}, // not directly in dir
	}
	for _, c := range cases {
		if got := w.isTypeFile(c.path); got != c.want {
			t.Errorf("isTypeFile(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

// --- Deterministic unit tests (no fsnotify) for the revalidation core. ---
// These exercise the content-identity decision directly so the suppression
// logic has coverage that does not depend on fsnotify delivery timing.

// TestContentMatches covers the four quadrants of the (expected, exists)
// decision: save-vs-existing, save-vs-missing, delete-vs-missing,
// delete-vs-existing. Reading the file and bytes.Equal is the entire
// suppression oracle; this pins all four outcomes.
func TestContentMatches(t *testing.T) {
	dir := t.TempDir()
	exists := filepath.Join(dir, "exists.yaml")
	if err := os.WriteFile(exists, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "missing.yaml")

	cases := []struct {
		name     string
		path     string
		expected []byte
		want     bool
	}{
		{"save matches existing", exists, []byte("payload"), true},
		{"save mismatch on existing", exists, []byte("other"), false},
		{"delete of missing matches", missing, nil, true},
		{"delete of existing mismatches", exists, nil, false},
		{"save of missing mismatches", missing, []byte("x"), false},
		{"empty expected matches missing", missing, []byte{}, true},
		{"empty expected mismatches existing", exists, []byte{}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := contentMatches(c.path, c.expected); got != c.want {
				t.Errorf("contentMatches(%q, %q) = %v, want %v", c.path, c.expected, got, c.want)
			}
		})
	}
}

// TestContentMatches_ReadErrorFailsLoud pins the fail-loud contract for a
// non-NotExist read error: a directory in place of the file (which yields
// "read ...: Incorrect function." on Windows or "is a directory" on POSIX —
// neither is IsNotExist) must mismatch even for a delete arm, so onChange
// fires rather than silently suppressing a state the watcher cannot verify.
// Permission-denied would exercise the same path but is flaky on Windows
// where chmod 0 does not prevent reads; a directory is a deterministic
// cross-platform stand-in.
func TestContentMatches_ReadErrorFailsLoud(t *testing.T) {
	dir := t.TempDir()
	// A path that IS a directory: ReadFile returns a non-NotExist error.
	dirAsFile := filepath.Join(dir, "subdir")
	if err := os.MkdirAll(dirAsFile, 0o755); err != nil {
		t.Fatal(err)
	}
	// Sanity-check the fixture so a future Go/OS change that turns this into
	// an IsNotExist surfaces here rather than as a misleading pass below.
	if _, err := os.ReadFile(dirAsFile); err == nil {
		t.Fatal("fixture regressed: ReadFile on a directory succeeded; pick a different non-NotExist trigger")
	} else if os.IsNotExist(err) {
		t.Fatalf("fixture regressed: ReadFile on a directory returned IsNotExist (%v); test would not exercise the intended path", err)
	}

	if contentMatches(dirAsFile, nil) {
		t.Error("delete arm should mismatch on a non-NotExist read error; reload must fire (fail loud)")
	}
	if contentMatches(dirAsFile, []byte("x")) {
		t.Error("save arm should mismatch on a non-NotExist read error; reload must fire (fail loud)")
	}
}

// TestLookupEntry_CaseInsensitive pins path normalization: case-insensitive
// filesystems (Windows, macOS) can report the same path with different
// casing depending on the source. The arm must still match via EqualFold.
func TestLookupEntry_CaseInsensitive(t *testing.T) {
	armed := filepath.Join("vault", ".system", "types", "Book.yaml")
	w := &TypeWatcher{selfArmed: map[string]selfEntry{
		armed: {expected: []byte("x"), armedAt: time.Now()},
	}}
	query := strings.ToLower(armed)
	if _, _, ok := w.lookupEntry(query, time.Now()); !ok {
		t.Errorf("EqualFold lookup failed: armed=%q query=%q", armed, query)
	}
}

// TestLookupEntry_ExpiresOldArms pins the safety net for dropped fsnotify
// events: an arm older than selfWriteWindow is pruned on lookup and treated
// as not-armed, so a later real external edit is not silently suppressed.
func TestLookupEntry_ExpiresOldArms(t *testing.T) {
	w := &TypeWatcher{selfArmed: map[string]selfEntry{
		"old.yaml": {expected: []byte("old"), armedAt: time.Now().Add(-2 * selfWriteWindow)},
		"new.yaml": {expected: []byte("new"), armedAt: time.Now()},
	}}
	now := time.Now()
	if _, _, ok := w.lookupEntry("old.yaml", now); ok {
		t.Error("expired arm should not be returned")
	}
	if _, ok := w.selfArmed["old.yaml"]; ok {
		t.Error("expired arm should be pruned from the map")
	}
	if _, _, ok := w.lookupEntry("new.yaml", now); !ok {
		t.Error("live arm should still be returned")
	}
}

// TestRegisterSelfWrite_NormalizesPath pins path normalization: RegisterSelfWrite
// stores filepath.Clean(path) so a path with internal "."/".." segments still
// matches the cleaned event path the loop records.
func TestRegisterSelfWrite_NormalizesPath(t *testing.T) {
	w := &TypeWatcher{selfArmed: map[string]selfEntry{}}
	raw := filepath.Join("vault", ".system", "types", "sub", "..", "book.yaml")
	w.RegisterSelfWrite(raw, []byte("x"))
	want := filepath.Clean(raw)
	if _, ok := w.selfArmed[want]; !ok {
		t.Errorf("expected arm under cleaned path %q; got keys %v", want, mapKeys(w.selfArmed))
	}
}

func mapKeys(m map[string]selfEntry) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestFlushPending_SelfWriteMatchSuppresses drives the debounced decision
// directly (no fsnotify): an armed path whose on-disk bytes equal the armed
// expectation is a confirmed self-write → suppress + consume the arm.
func TestFlushPending_SelfWriteMatchSuppresses(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "book.yaml")
	content := []byte("name: Book\n")
	if err := os.WriteFile(p, content, 0o644); err != nil {
		t.Fatal(err)
	}

	fired := false
	w := &TypeWatcher{
		onChange:  func() { fired = true },
		selfArmed: map[string]selfEntry{p: {expected: content, armedAt: time.Now()}},
		pending:   map[string]bool{p: true},
	}
	w.flushPending()
	if fired {
		t.Error("matched self-write should suppress onChange")
	}
	if _, ok := w.selfArmed[p]; ok {
		t.Error("matched arm should be consumed")
	}
}

// TestFlushPending_ExternalEditFiresOnChange: armed path whose on-disk bytes
// DIFFER from the armed expectation (an external edit landed) → fire onChange
// and clear the stale arm. This is the deterministic core of #872.
func TestFlushPending_ExternalEditFiresOnChange(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "book.yaml")
	if err := os.WriteFile(p, []byte("external"), 0o644); err != nil {
		t.Fatal(err)
	}

	fired := false
	w := &TypeWatcher{
		onChange:  func() { fired = true },
		selfArmed: map[string]selfEntry{p: {expected: []byte("our-save"), armedAt: time.Now()}},
		pending:   map[string]bool{p: true},
	}
	w.flushPending()
	if !fired {
		t.Error("mismatched arm should fire onChange (external edit)")
	}
	if _, ok := w.selfArmed[p]; ok {
		t.Error("mismatched arm should be cleared")
	}
}

// TestFlushPending_UnarmedPathFiresOnChange: a pending path with no arm is a
// plain external event → always fire onChange (cross-file / unrelated edit).
func TestFlushPending_UnarmedPathFiresOnChange(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "book.yaml")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	fired := false
	w := &TypeWatcher{
		onChange:  func() { fired = true },
		selfArmed: map[string]selfEntry{},
		pending:   map[string]bool{p: true},
	}
	w.flushPending()
	if !fired {
		t.Error("unarmed pending path should fire onChange")
	}
}

// TestFlushPending_DeleteMatchSuppresses: armed delete (nil expected) on a
// now-missing file → confirmed delete → suppress.
func TestFlushPending_DeleteMatchSuppresses(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "book.yaml") // intentionally not created

	fired := false
	w := &TypeWatcher{
		onChange:  func() { fired = true },
		selfArmed: map[string]selfEntry{p: {expected: nil, armedAt: time.Now()}},
		pending:   map[string]bool{p: true},
	}
	w.flushPending()
	if fired {
		t.Error("confirmed delete (file absent) should suppress onChange")
	}
}

// TestFlushPending_DeleteArmReadErrorFiresReload: a delete arm whose path
// yields a non-NotExist read error (directory-in-place-of-file here;
// permission-denied in real failures) must NOT suppress — the watcher cannot
// confirm the delete, so onChange fires (fail loud). Pins #872's correction.
func TestFlushPending_DeleteArmReadErrorFiresReload(t *testing.T) {
	dir := t.TempDir()
	// A directory where the type file should be — ReadFile fails non-NotExist.
	p := filepath.Join(dir, "book.yaml")
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}

	fired := false
	w := &TypeWatcher{
		onChange:  func() { fired = true },
		selfArmed: map[string]selfEntry{p: {expected: nil, armedAt: time.Now()}},
		pending:   map[string]bool{p: true},
	}
	w.flushPending()
	if !fired {
		t.Error("delete arm with a non-NotExist read error should fire onChange (fail loud), not suppress")
	}
	if _, ok := w.selfArmed[p]; ok {
		t.Error("unconfirmable delete arm should be cleared after firing reload")
	}
}

// TestFlushPending_MultiplePathsOneReload: a burst spanning an armed
// confirmed self-write AND an unrelated external edit fires onChange exactly
// once. Pins the at-most-one-callback-per-burst coalescing contract.
func TestFlushPending_MultiplePathsOneReload(t *testing.T) {
	dir := t.TempDir()
	self := filepath.Join(dir, "self.yaml")
	ext := filepath.Join(dir, "ext.yaml")
	content := []byte("name: Self\n")
	if err := os.WriteFile(self, content, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ext, []byte("name: Ext\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	calls := 0
	w := &TypeWatcher{
		onChange:  func() { calls++ },
		selfArmed: map[string]selfEntry{self: {expected: content, armedAt: time.Now()}},
		pending:   map[string]bool{self: true, ext: true},
	}
	w.flushPending()
	if calls != 1 {
		t.Errorf("expected exactly one onChange for a mixed burst, got %d", calls)
	}
	// Self confirmed → arm consumed; ext was never armed.
	if _, ok := w.selfArmed[self]; ok {
		t.Error("matched self arm should be consumed after the burst")
	}
}

// waitForTypeChange performs an action that triggers a file-system change, then
// waits up to 2s for the watcher's onChange callback to fire. Mirrors
// templates.waitFor.
func waitForTypeChange(t *testing.T, ch <-chan struct{}, label string, action func()) {
	t.Helper()
	action()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatalf("type watcher did not fire onChange for: %s", label)
	}
}

// armedPathForTest reports whether path has a live arm, for assertions. Reaches
// into the watcher's internal map without exposing it on the public API.
func armedPathForTest(t *testing.T, w *TypeWatcher, path string) (selfEntry, bool) {
	t.Helper()
	w.selfMu.Lock()
	defer w.selfMu.Unlock()
	e, _, ok := w.lookupEntry(filepath.Clean(path), time.Now())
	return e, ok
}
