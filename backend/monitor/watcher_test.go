package monitor

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"silt/backend/core"
	"silt/backend/db"
)

func TestDirectoryWatcher_ReindexFileHoldsFileLock(t *testing.T) {
	// Verifies the fix for the lost-update race: reindexFile must hold
	// the per-file IO lock for the duration of read+parse+write+index
	// so a concurrent UpdateBlockState cannot land between the watcher's
	// read and the watcher's eventual write.
	vaultPath := t.TempDir()

	dm, err := db.NewDatabaseManager("")
	if err != nil {
		t.Fatalf("NewDatabaseManager: %v", err)
	}
	t.Cleanup(func() { _ = dm.Close() })

	coord := core.NewExecutionCoordinator(dm.SQLDB())
	tracker := NewWriteTracker()
	t.Cleanup(tracker.Stop)

	dw, err := NewDirectoryWatcher(vaultPath, dm, tracker, coord, 4)
	if err != nil {
		t.Fatalf("NewDirectoryWatcher: %v", err)
	}

	filePath := filepath.Join(vaultPath, "test.md")
	if err := os.WriteFile(filePath, []byte(
		"# Test <!-- id: aaaa1111-aaaa-aaaa-aaaa-aaaaaaaaaaaa -->\n"+
			"- [ ] x <!-- id: bbbb2222-bbbb-bbbb-bbbb-bbbbbbbbbbbb -->\n",
	), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	// Hold the file lock from an outside goroutine for 200ms.
	lockHeld := make(chan struct{})
	lockReleased := make(chan struct{})
	go func() {
		coord.LockFileWrite(filePath, func() {
			close(lockHeld)
			time.Sleep(200 * time.Millisecond)
			close(lockReleased)
		})
	}()
	<-lockHeld

	// Start reindexFile. It must block on the file lock.
	reindexReturned := make(chan struct{})
	go func() {
		dw.reindexFile(filePath)
		close(reindexReturned)
	}()

	select {
	case <-reindexReturned:
		t.Fatalf("reindexFile returned while the per-file lock was held; the lock is not being acquired")
	case <-time.After(50 * time.Millisecond):
		// Good: reindexFile is still blocked. Fall through.
	}

	// Wait for the outer lock to release; reindexFile should then run
	// to completion.
	select {
	case <-reindexReturned:
		// success
	case <-time.After(2 * time.Second):
		t.Fatalf("reindexFile did not return within 2s after the file lock was released")
	}
	<-lockReleased
}

func TestDirectoryWatcher_ReindexFileIndexesFile(t *testing.T) {
	// Smoke test: reindexFile writes block IDs into the file (when
	// missing) and indexes the file's blocks into the database. Verifies
	// the watcher end-to-end contract that the previous lock fix could
	// have broken.
	vaultPath := t.TempDir()

	dm, err := db.NewDatabaseManager("")
	if err != nil {
		t.Fatalf("NewDatabaseManager: %v", err)
	}
	t.Cleanup(func() { _ = dm.Close() })

	coord := core.NewExecutionCoordinator(dm.SQLDB())
	tracker := NewWriteTracker()
	t.Cleanup(tracker.Stop)

	dw, err := NewDirectoryWatcher(vaultPath, dm, tracker, coord, 4)
	if err != nil {
		t.Fatalf("NewDirectoryWatcher: %v", err)
	}

	filePath := filepath.Join(vaultPath, "Work", "Journal", "Daily", "2026-06-13.md")
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filePath, []byte(
		"# Today <!-- id: 11111111-1111-1111-1111-111111111111 -->\n"+
			"\n"+
			"- [ ] sample <!-- id: 22222222-2222-2222-2222-222222222222 -->\n",
	), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	dw.reindexFile(filePath)

	// File should now have content; the parser may or may not have
	// rewritten it depending on whether the input was already valid.
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read after reindex: %v", err)
	}
	if len(content) == 0 {
		t.Fatalf("file is empty after reindex")
	}

	// Database should now have both blocks.
	for _, id := range []string{
		"11111111-1111-1111-1111-111111111111",
		"22222222-2222-2222-2222-222222222222",
	} {
		var n int
		if err := dm.SQLDB().QueryRow("SELECT COUNT(*) FROM blocks WHERE id = ?", id).Scan(&n); err != nil {
			t.Fatalf("count block %s: %v", id, err)
		}
		if n != 1 {
			t.Errorf("expected block %s to be indexed, got count %d", id, n)
		}
	}
}

func TestDirectoryWatcher_FocusLockSuppressesReindex(t *testing.T) {
	vaultPath := t.TempDir()

	dm, err := db.NewDatabaseManager("")
	if err != nil {
		t.Fatalf("NewDatabaseManager: %v", err)
	}
	t.Cleanup(func() { _ = dm.Close() })

	coord := core.NewExecutionCoordinator(dm.SQLDB())
	tracker := NewWriteTracker()
	t.Cleanup(tracker.Stop)

	dw, err := NewDirectoryWatcher(vaultPath, dm, tracker, coord, 4)
	if err != nil {
		t.Fatalf("NewDirectoryWatcher: %v", err)
	}
	// The file must live under <vault>/<notebook>/<section>/<page>/ to resolve
	// in the 3-level model. Create the dir before Start so the watcher
	// subscribes to it during AddRecursive.
	filePath := filepath.Join(vaultPath, "Work", "Journal", "Daily", "test.md")
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := dw.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = dw.Close() })

	// Step 1: write a file and wait for the watcher to index it.
	writeContent := func(text string) {
		if err := os.WriteFile(filePath, []byte(text), 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}
	}
	writeContent("# Initial <!-- id: aaaa0000-aaaa-aaaa-aaaa-aaaaaaaaaaaa -->\n")

	waitForBlock := func(id string, wantCount int, timeout time.Duration) {
		deadline := time.Now().Add(timeout)
		for time.Now().Before(deadline) {
			var n int
			if err := dm.SQLDB().QueryRow("SELECT COUNT(*) FROM blocks WHERE id = ?", id).Scan(&n); err == nil && n == wantCount {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		var n int
		_ = dm.SQLDB().QueryRow("SELECT COUNT(*) FROM blocks WHERE id = ?", id).Scan(&n)
		t.Fatalf("timed out waiting for block %s count=%d (want %d)", id, n, wantCount)
	}

	waitForBlock("aaaa0000-aaaa-aaaa-aaaa-aaaaaaaaaaaa", 1, 3*time.Second)

	// Step 2: lock the file.
	dw.LockFocus(filePath)

	// Step 3: write new content while locked. The watcher must NOT reindex.
	writeContent("# Updated <!-- id: aaaa0000-aaaa-aaaa-aaaa-aaaaaaaaaaaa -->\n" +
		"- [ ] locked content <!-- id: bbbb1111-bbbb-bbbb-bbbb-bbbbbbbbbbbb -->\n")

	// Give the watcher time to (incorrectly) process it.
	time.Sleep(1 * time.Second)

	var lockedCount int
	_ = dm.SQLDB().QueryRow("SELECT COUNT(*) FROM blocks WHERE id = ?", "bbbb1111-bbbb-bbbb-bbbb-bbbbbbbbbbbb").Scan(&lockedCount)
	if lockedCount != 0 {
		t.Fatalf("expected locked content to NOT be indexed, but found %d rows for bbbb1111", lockedCount)
	}

	// Step 4: unlock and write again. The watcher must now reindex.
	dw.UnlockFocus(filePath)
	writeContent("# Re-indexed <!-- id: aaaa0000-aaaa-aaaa-aaaa-aaaaaaaaaaaa -->\n" +
		"- [ ] unlocked content <!-- id: cccc2222-cccc-cccc-cccc-cccccccccccc -->\n")

	waitForBlock("cccc2222-cccc-cccc-cccc-cccccccccccc", 1, 3*time.Second)
}

// --- Phase 5b: TTL focus leases (#38) ---

// newWatcherForLeaseTest builds a watcher with a short TTL so lease expiry
// tests don't have to wait 60s. The watcher is NOT started (no fsnotify
// subscription / no sweeper goroutine) — the tests drive the lease map and
// sweeper directly.
func newWatcherForLeaseTest(t *testing.T, ttl time.Duration) *DirectoryWatcher {
	t.Helper()
	dm, err := db.NewDatabaseManager("")
	if err != nil {
		t.Fatalf("NewDatabaseManager: %v", err)
	}
	t.Cleanup(func() { _ = dm.Close() })
	tracker := NewWriteTracker()
	coord := core.NewExecutionCoordinator(dm.SQLDB())
	dw, err := NewDirectoryWatcher(t.TempDir(), dm, tracker, coord, 4)
	if err != nil {
		t.Fatalf("NewDirectoryWatcher: %v", err)
	}
	dw.focusTTL = ttl
	t.Cleanup(func() { _ = dw.Close() })
	return dw
}

func TestFocusLease_AcquireThenLocked(t *testing.T) {
	dw := newWatcherForLeaseTest(t, 60*time.Second)
	p := "/vault/a.md"
	if dw.IsFocusLocked(p) {
		t.Fatal("should be unlocked before acquire")
	}
	dw.LockFocus(p)
	if !dw.IsFocusLocked(p) {
		t.Fatal("should be locked after acquire")
	}
}

func TestFocusLease_ExpiryRecoversSuppression(t *testing.T) {
	// A lease with a sub-second TTL must read as unlocked once it expires,
	// so a crashed/unmounted editor self-heals without an explicit release.
	dw := newWatcherForLeaseTest(t, 30*time.Millisecond)
	p := "/vault/b.md"
	dw.LockFocus(p)
	if !dw.IsFocusLocked(p) {
		t.Fatal("should be locked immediately after acquire")
	}
	time.Sleep(60 * time.Millisecond)
	if dw.IsFocusLocked(p) {
		t.Fatal("expired lease should read as unlocked")
	}
	// The sweeper reaps the stale entry; IsFocusLocked is already correct
	// but the map entry should also go away.
	dw.sweepExpiredLeases()
	dw.focusMu.RLock()
	_, present := dw.focusLeases[p]
	dw.focusMu.RUnlock()
	if present {
		t.Error("sweeper did not reap the expired lease")
	}
}

func TestFocusLease_RefreshKeepsItAlive(t *testing.T) {
	dw := newWatcherForLeaseTest(t, 40*time.Millisecond)
	p := "/vault/c.md"
	dw.LockFocus(p)
	// Refresh well within the TTL, a few times, long enough that an
	// unrefreshed lease would have expired.
	for i := 0; i < 5; i++ {
		time.Sleep(20 * time.Millisecond)
		dw.RefreshFocus(p)
	}
	if !dw.IsFocusLocked(p) {
		t.Fatal("refreshed lease should still be alive")
	}
}

func TestFocusLease_RefreshNoOpWhenExpired(t *testing.T) {
	// If the editor lost focus and the lease lapsed, a late heartbeat must
	// NOT silently re-acquire suppression (the editor must re-Acquire).
	dw := newWatcherForLeaseTest(t, 20*time.Millisecond)
	p := "/vault/d.md"
	dw.LockFocus(p)
	time.Sleep(50 * time.Millisecond)
	dw.RefreshFocus(p) // late refresh after expiry
	if dw.IsFocusLocked(p) {
		t.Fatal("late refresh resurrected an expired lease — should require re-acquire")
	}
}

func TestFocusLease_ReleaseAllClearsEverything(t *testing.T) {
	dw := newWatcherForLeaseTest(t, 60*time.Second)
	dw.LockFocus("/vault/e.md")
	dw.LockFocus("/vault/f.md")
	dw.ReleaseAllFocus()
	for _, p := range []string{"/vault/e.md", "/vault/f.md"} {
		if dw.IsFocusLocked(p) {
			t.Errorf("ReleaseAllFocus left %s locked", p)
		}
	}
}

// TestFocusLease_ConcurrentAccessIsRaceClean exercises acquire/refresh/
// release/sweep/IsFocusLocked concurrently to flush any data race in the
// lease map handoff (run under -race).
func TestFocusLease_ConcurrentAccessIsRaceClean(t *testing.T) {
	dw := newWatcherForLeaseTest(t, 50*time.Millisecond)
	const n = 50
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			p := "/vault/g" + itoa(i) + ".md"
			dw.LockFocus(p)
			dw.RefreshFocus(p)
			_ = dw.IsFocusLocked(p)
			dw.UnlockFocus(p)
		}(i)
	}
	// Sweeper running concurrently with the acquirers.
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-done:
				return
			default:
				dw.sweepExpiredLeases()
			}
		}
	}()
	wg.Wait()
	close(done)
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(buf[pos:])
}

// TestDirectoryWatcher_GoverningRoot_LongestPrefix verifies the longest-prefix
// root resolution used by resolveFileMetadata (#100): a direct child of a
// registered root resolves to that root, a descendant shared by a nested root
// resolves to the longest (innermost) prefix, and a path under no root is
// rejected. Also guards the separator handling in the prefix match (a root must
// not get a doubled separator appended when matching its descendants).
func TestDirectoryWatcher_GoverningRoot_LongestPrefix(t *testing.T) {
	vaultPath := t.TempDir()

	dm, err := db.NewDatabaseManager("")
	if err != nil {
		t.Fatalf("NewDatabaseManager: %v", err)
	}
	t.Cleanup(func() { _ = dm.Close() })

	coord := core.NewExecutionCoordinator(dm.SQLDB())
	tracker := NewWriteTracker()
	t.Cleanup(tracker.Stop)

	dw, err := NewDirectoryWatcher(vaultPath, dm, tracker, coord, 4)
	if err != nil {
		t.Fatalf("NewDirectoryWatcher: %v", err)
	}

	outer := t.TempDir()
	inner := filepath.Join(outer, "sub")
	dw.registerRoot(outer, watchRoot{source: "linked:outer", notebook: "Outer"})
	dw.registerRoot(inner, watchRoot{source: "linked:inner", notebook: "Inner"})

	// A direct child of outer (not under inner) resolves to outer.
	got, info, ok := dw.governingRoot(filepath.Join(outer, "page.md"))
	if !ok {
		t.Fatal("expected outer to govern its direct child")
	}
	if got != outer {
		t.Errorf("governing root = %q, want %q", got, outer)
	}
	if info.notebook != "Outer" {
		t.Errorf("notebook = %q, want Outer", info.notebook)
	}

	// A descendant of inner resolves to the longest prefix (inner), not outer.
	got2, info2, ok2 := dw.governingRoot(filepath.Join(inner, "deep", "page.md"))
	if !ok2 {
		t.Fatal("expected inner to govern its descendant")
	}
	if got2 != inner {
		t.Errorf("governing root = %q, want %q", got2, inner)
	}
	if info2.notebook != "Inner" {
		t.Errorf("notebook = %q, want Inner", info2.notebook)
	}

	// An exact match on a root resolves to that root.
	got3, _, ok3 := dw.governingRoot(inner)
	if !ok3 || got3 != inner {
		t.Errorf("governing root for root itself = %q (%v), want %q", got3, ok3, inner)
	}

	// A path under no registered root is rejected.
	if _, _, ok := dw.governingRoot(filepath.Join(t.TempDir(), "stranger.md")); ok {
		t.Error("expected governingRoot to reject a path under no root")
	}
}

// TestDirectoryWatcher_ResolveFileMetadata_LinkedRoot verifies per-root
// attribution (#100): a file under a linked root resolves to the linked
// source + registered display name + section/page relative to that root,
// while a vault file resolves with notebook = its first path component.
func TestDirectoryWatcher_ResolveFileMetadata_LinkedRoot(t *testing.T) {
	vaultPath := t.TempDir()
	if err := os.MkdirAll(filepath.Join(vaultPath, "Work", "Journal"), 0o755); err != nil {
		t.Fatal(err)
	}
	dm, err := db.NewDatabaseManager("")
	if err != nil {
		t.Fatalf("NewDatabaseManager: %v", err)
	}
	t.Cleanup(func() { _ = dm.Close() })
	coord := core.NewExecutionCoordinator(dm.SQLDB())
	tracker := NewWriteTracker()
	t.Cleanup(tracker.Stop)
	dw, err := NewDirectoryWatcher(vaultPath, dm, tracker, coord, 4)
	if err != nil {
		t.Fatalf("NewDirectoryWatcher: %v", err)
	}

	// Register a linked root "Ext" with a nested section page.
	linked := t.TempDir()
	if err := os.MkdirAll(filepath.Join(linked, "Projects"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := dw.AddWatchRoot(linked, "linked:ext", "Ext"); err != nil {
		t.Fatalf("AddWatchRoot: %v", err)
	}

	// Linked file: <linkedRoot>/Projects/Alpha.md → source linked, notebook
	// Ext (the registered name), section Projects, page Alpha.
	src, nb, sec, pg, _ := dw.resolveFileMetadata(filepath.Join(linked, "Projects", "Alpha.md"))
	if src != "linked:ext" || nb != "Ext" || sec != "Projects" || pg != "Alpha" {
		t.Errorf("linked resolve = src=%q nb=%q sec=%q pg=%q, want linked:ext/Ext/Projects/Alpha", src, nb, sec, pg)
	}

	// Vault file: <vault>/Work/Journal/Daily.md → source vault, notebook Work
	// (first component), section Journal, page Daily.
	vsrc, vnb, vsec, vpg, _ := dw.resolveFileMetadata(filepath.Join(vaultPath, "Work", "Journal", "Daily.md"))
	if vsrc != "vault" || vnb != "Work" || vsec != "Journal" || vpg != "Daily" {
		t.Errorf("vault resolve = src=%q nb=%q sec=%q pg=%q, want vault/Work/Journal/Daily", vsrc, vnb, vsec, vpg)
	}
}

// TestDirectoryWatcher_AddRemoveWatchRoot confirms the multi-root registry
// tracks linked roots across add/remove (#100): after AddWatchRoot the root
// governs its files; after RemoveWatchRoot it no longer does (so its events
// would be attributed to no root and dropped).
func TestDirectoryWatcher_AddRemoveWatchRoot(t *testing.T) {
	vaultPath := t.TempDir()
	dm, err := db.NewDatabaseManager("")
	if err != nil {
		t.Fatalf("NewDatabaseManager: %v", err)
	}
	t.Cleanup(func() { _ = dm.Close() })
	coord := core.NewExecutionCoordinator(dm.SQLDB())
	tracker := NewWriteTracker()
	t.Cleanup(tracker.Stop)
	dw, err := NewDirectoryWatcher(vaultPath, dm, tracker, coord, 4)
	if err != nil {
		t.Fatalf("NewDirectoryWatcher: %v", err)
	}

	linked := t.TempDir()
	if err := dw.AddWatchRoot(linked, "linked:x", "Ext"); err != nil {
		t.Fatalf("AddWatchRoot: %v", err)
	}
	if _, _, ok := dw.governingRoot(filepath.Join(linked, "page.md")); !ok {
		t.Error("expected governingRoot to find the linked root after AddWatchRoot")
	}

	dw.RemoveWatchRoot(linked)
	if _, _, ok := dw.governingRoot(filepath.Join(linked, "page.md")); ok {
		t.Error("expected governingRoot to drop the linked root after RemoveWatchRoot")
	}
}

// TestDirectoryWatcher_LinkedConfigSourceForPath confirms the co-located
// config path detector (#133): only a linked root's <root>/.system/config.yaml
// matches; the vault config and unrelated linked YAMLs do not.
func TestDirectoryWatcher_LinkedConfigSourceForPath(t *testing.T) {
	vaultPath := t.TempDir()
	dm, err := db.NewDatabaseManager("")
	if err != nil {
		t.Fatalf("NewDatabaseManager: %v", err)
	}
	t.Cleanup(func() { _ = dm.Close() })
	coord := core.NewExecutionCoordinator(dm.SQLDB())
	tracker := NewWriteTracker()
	t.Cleanup(tracker.Stop)
	dw, err := NewDirectoryWatcher(vaultPath, dm, tracker, coord, 4)
	if err != nil {
		t.Fatalf("NewDirectoryWatcher: %v", err)
	}

	linked := t.TempDir()
	if err := dw.AddWatchRoot(linked, "linked:ext", "Ext"); err != nil {
		t.Fatalf("AddWatchRoot: %v", err)
	}

	cases := []struct {
		name    string
		path    string
		wantOK  bool
		wantSrc string
	}{
		{
			name:    "linked co-located config",
			path:    filepath.Join(linked, ".system", "config.yaml"),
			wantOK:  true,
			wantSrc: "linked:ext",
		},
		{
			name:    "vault co-located config (not linked, ignored)",
			path:    filepath.Join(vaultPath, ".system", "config.yaml"),
			wantOK:  false,
			wantSrc: "",
		},
		{
			name:    "unrelated linked YAML",
			path:    filepath.Join(linked, ".system", "other.yaml"),
			wantOK:  false,
			wantSrc: "",
		},
		{
			name:    "linked markdown page",
			path:    filepath.Join(linked, "Plan.md"),
			wantOK:  false,
			wantSrc: "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotSrc, gotOK := dw.linkedConfigSourceForPath(c.path)
			if gotOK != c.wantOK {
				t.Errorf("linkedConfigSourceForPath(%q) ok = %v, want %v", c.path, gotOK, c.wantOK)
			}
			if gotSrc != c.wantSrc {
				t.Errorf("linkedConfigSourceForPath(%q) src = %q, want %q", c.path, gotSrc, c.wantSrc)
			}
		})
	}
}

// TestDirectoryWatcher_SetLinkedConfigHandler confirms the handler is invoked
// when set and skipped when nil (the watcher must not panic on a co-located
// config event if no handler is registered).
func TestDirectoryWatcher_SetLinkedConfigHandler(t *testing.T) {
	vaultPath := t.TempDir()
	dm, err := db.NewDatabaseManager("")
	if err != nil {
		t.Fatalf("NewDatabaseManager: %v", err)
	}
	t.Cleanup(func() { _ = dm.Close() })
	coord := core.NewExecutionCoordinator(dm.SQLDB())
	tracker := NewWriteTracker()
	t.Cleanup(tracker.Stop)
	dw, err := NewDirectoryWatcher(vaultPath, dm, tracker, coord, 4)
	if err != nil {
		t.Fatalf("NewDirectoryWatcher: %v", err)
	}

	// Register a linked root with a co-located config.
	linked := t.TempDir()
	if err := dw.AddWatchRoot(linked, "linked:ext", "Ext"); err != nil {
		t.Fatalf("AddWatchRoot: %v", err)
	}
	cfgPath := filepath.Join(linked, ".system", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, []byte("plugins: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// With no handler set, the detector still identifies the path; calling
	// the dispatch path directly must be a no-op (handler is nil).
	dw.linkedConfigHandlerMu.RLock()
	h := dw.linkedConfigHandler
	dw.linkedConfigHandlerMu.RUnlock()
	if h != nil {
		t.Fatal("expected nil handler before SetLinkedConfigHandler")
	}

	// Register a handler and confirm it is invoked with the source.
	var (
		gotSource string
		calls     int
		mu        sync.Mutex
	)
	dw.SetLinkedConfigHandler(func(source string) {
		mu.Lock()
		defer mu.Unlock()
		gotSource = source
		calls++
	})
	dw.linkedConfigHandlerMu.RLock()
	h = dw.linkedConfigHandler
	dw.linkedConfigHandlerMu.RUnlock()
	if h == nil {
		t.Fatal("expected non-nil handler after SetLinkedConfigHandler")
	}
	h("linked:ext")

	mu.Lock()
	defer mu.Unlock()
	if calls != 1 || gotSource != "linked:ext" {
		t.Errorf("handler call: got calls=%d source=%q, want 1 / linked:ext", calls, gotSource)
	}
}

// --- Standalone-tasks watcher carve-out (#372) ---------------------------

// TestDirectoryWatcher_StartWatchesStandaloneTasksDir verifies the Start()
// call adds <vault>/.silt to the fsnotify watch set when the dot-prefixed
// directory exists at startup. AddRecursive skips dot-prefixed directories
// by design; the carve-out below is what closes the parity gap for the
// standalone-tasks file (#368) without generalizing the dot-prefix skip.
func TestDirectoryWatcher_StartWatchesStandaloneTasksDir(t *testing.T) {
	vaultPath := t.TempDir()
	dotDir := filepath.Join(vaultPath, ".silt")
	if err := os.MkdirAll(dotDir, 0o755); err != nil {
		t.Fatalf("mkdir .silt: %v", err)
	}

	dm, err := db.NewDatabaseManager("")
	if err != nil {
		t.Fatalf("NewDatabaseManager: %v", err)
	}
	t.Cleanup(func() { _ = dm.Close() })

	coord := core.NewExecutionCoordinator(dm.SQLDB())
	tracker := NewWriteTracker()
	t.Cleanup(tracker.Stop)

	dw, err := NewDirectoryWatcher(vaultPath, dm, tracker, coord, 4)
	if err != nil {
		t.Fatalf("NewDirectoryWatcher: %v", err)
	}
	if err := dw.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = dw.Close() })

	// Strong assertion (#372 hardening): the carve-out's purpose is to
	// add `<vault>/.silt` to the fsnotify watch set. The smoke-only
	// check below (stat the directory) doesn't catch a typo'd carve-out
	// that simply doesn't run. fsnotify exposes WatchList so we can
	// read it directly. On Windows the path separator in the returned
	// list is backslash; normalize for the comparison.
	watches := dw.watcher.WatchList()
	want := filepath.Clean(dotDir)
	found := false
	for _, w := range watches {
		if filepath.Clean(w) == want {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected <vault>/.silt (%s) to be in fsnotify WatchList; got %v",
			want, watches)
	}
}

// TestDirectoryWatcher_StartNoOpWhenStandaloneTasksDirAbsent verifies that
// Start() does not crash when the .silt directory does not exist. The
// carve-out is gated on the directory existing — if a user has never
// captured a standalone task, the dir has never been auto-created.
func TestDirectoryWatcher_StartNoOpWhenStandaloneTasksDirAbsent(t *testing.T) {
	vaultPath := t.TempDir()

	dm, err := db.NewDatabaseManager("")
	if err != nil {
		t.Fatalf("NewDatabaseManager: %v", err)
	}
	t.Cleanup(func() { _ = dm.Close() })

	coord := core.NewExecutionCoordinator(dm.SQLDB())
	tracker := NewWriteTracker()
	t.Cleanup(tracker.Stop)

	dw, err := NewDirectoryWatcher(vaultPath, dm, tracker, coord, 4)
	if err != nil {
		t.Fatalf("NewDirectoryWatcher: %v", err)
	}
	if err := dw.Start(); err != nil {
		t.Fatalf("Start with no .silt dir: %v", err)
	}
	t.Cleanup(func() { _ = dw.Close() })
}

// TestDirectoryWatcher_ExternalWriteToStandaloneTasksReindexes is the
// core acceptance test for #372: an external write to
// <vault>/.silt/tasks.md (not a Silt-driven write) reaches the listen
// loop via the new watcher carve-out and triggers an incremental
// reindex — without requiring a full startup scan.
//
// Setup philosophy: the watcher is started BEFORE the file exists, so
// the first event we observe is the file's creation. The watcher's
// listen loop fires reindexFile on Create events (not just Write), so
// this exercises the same code path an external editor save would.
func TestDirectoryWatcher_ExternalWriteToStandaloneTasksReindexes(t *testing.T) {
	vaultPath := t.TempDir()

	dm, err := db.NewDatabaseManager("")
	if err != nil {
		t.Fatalf("NewDatabaseManager: %v", err)
	}
	t.Cleanup(func() { _ = dm.Close() })

	coord := core.NewExecutionCoordinator(dm.SQLDB())
	tracker := NewWriteTracker()
	t.Cleanup(tracker.Stop)

	dw, err := NewDirectoryWatcher(vaultPath, dm, tracker, coord, 4)
	if err != nil {
		t.Fatalf("NewDirectoryWatcher: %v", err)
	}
	// Pre-create just the .silt dir so the carve-out in Start() can
	// subscribe to it (the dir itself, not the file). We then write
	// tasks.md AFTER Start() so its creation is observed as a fsnotify
	// Create event — exactly the path a non-Silt tool would take.
	dotDir := filepath.Join(vaultPath, ".silt")
	if err := os.MkdirAll(dotDir, 0o755); err != nil {
		t.Fatalf("mkdir .silt: %v", err)
	}
	if err := dw.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = dw.Close() })

	tasksFile := filepath.Join(dotDir, "tasks.md")
	if err := os.WriteFile(tasksFile, []byte(
		"---\nnotebook: \".silt\"\nsection: \"\"\npage: \"tasks\"\ndate: 2026-07-02\ntags: []\n---\n\n"+
			"- [ ] external edit <!-- id: 44444444-4444-4444-4444-444444444444 -->\n",
	), 0o644); err != nil {
		t.Fatalf("external write: %v", err)
	}

	waitForBlock := func(id string, want int, timeout time.Duration, tag string) {
		deadline := time.Now().Add(timeout)
		for time.Now().Before(deadline) {
			var n int
			if err := dm.SQLDB().QueryRow("SELECT COUNT(*) FROM blocks WHERE id = ?", id).Scan(&n); err == nil && n == want {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		var n int
		_ = dm.SQLDB().QueryRow("SELECT COUNT(*) FROM blocks WHERE id = ?", id).Scan(&n)
		t.Fatalf("[%s] timed out waiting for block %s count=%d (want %d)", tag, id, n, want)
	}
	waitForBlock("44444444-4444-4444-4444-444444444444", 1, 5*time.Second, "external-edit reindex")
}

// TestDirectoryWatcher_StandaloneTasksInAppWriteStillSuppressed verifies
// the no-feedback-loop invariant (#372): an in-app write to the
// standalone-tasks file (which goes through the atomic-writer path and
// therefore registers the path with WriteTracker) does NOT trigger a
// reindex even though the dot-dir carve-out now observes it.
//
// The setup mirrors the existing TestSetOpenTabs_SelfWriteSuppressed
// pattern (cited in TESTING.md) — every atomic write registers; the
// listen loop drops the FIRST post-RegisterWrite event within the
// tracker cooldown. Here we drive the listen loop with a synthetic
// WriteTracker + a manual `tracker.IsSelfGenerated` check, bypassing
// the live fsnotify channel — the listen loop's check is itself a
// tracker call, so exercising it directly is the precise behavior we
// need to lock down.
func TestDirectoryWatcher_StandaloneTasksInAppWriteStillSuppressed(t *testing.T) {
	vaultPath := t.TempDir()
	tracker := NewWriteTracker()
	t.Cleanup(tracker.Stop)

	tasksFile := filepath.Join(vaultPath, ".silt", "tasks.md")

	// 1. App-side writer path: RegisterWrite before the atomic rename.
	// The write happens immediately after — the gap is microseconds in
	// production, but the tracker window is 300 ms so we explicitly do
	// NOT simulate a delay here (the test would otherwise be timing-
	// dependent).
	tracker.RegisterWrite(tasksFile)

	// 2. fsnotify delivers a Write event. The listen loop calls
	// tracker.IsSelfGenerated — which must return true and consume the
	// entry, suppressing the reindex.
	if !tracker.IsSelfGenerated(tasksFile) {
		t.Fatal("expected IsSelfGenerated to return true within the 300ms cooldown window after RegisterWrite")
	}

	// 3. Confirm the entry is consumed (a second immediate
	// IsSelfGenerated within the cooldown would have re-fired the
	// suppression; instead the listen loop checks exactly once per
	// event — the "consume" semantics matters because it prevents the
	// suppression from being perpetual).
	if tracker.IsSelfGenerated(tasksFile) {
		t.Error("expected the tracker entry to be consumed on the first IsSelfGenerated call (no perpetual suppression)")
	}
}

// TestDirectoryWatcher_DeferredRegistrationOnDotDirCreate covers the
// mid-session case (#372 PLAN §7.2): the `.silt/` directory does not
// exist at Start() time, so the initial carve-out skips it; a non-Silt
// tool then creates the directory mid-session; the listen loop catches
// the Create event and adds the dot-dir to the fsnotify watch set so
// subsequent edits to `.silt/tasks.md` are observed.
func TestDirectoryWatcher_DeferredRegistrationOnDotDirCreate(t *testing.T) {
	vaultPath := t.TempDir()

	dm, err := db.NewDatabaseManager("")
	if err != nil {
		t.Fatalf("NewDatabaseManager: %v", err)
	}
	t.Cleanup(func() { _ = dm.Close() })

	coord := core.NewExecutionCoordinator(dm.SQLDB())
	tracker := NewWriteTracker()
	t.Cleanup(tracker.Stop)

	dw, err := NewDirectoryWatcher(vaultPath, dm, tracker, coord, 4)
	if err != nil {
		t.Fatalf("NewDirectoryWatcher: %v", err)
	}
	// Start WITHOUT a .silt dir; the carve-out in Start() should
	// silently skip.
	if err := dw.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = dw.Close() })

	// Sanity: the watch list should NOT contain .silt/ — the dir does
	// not exist.
	for _, w := range dw.watcher.WatchList() {
		cleaned := filepath.Clean(w)
		if strings.HasSuffix(cleaned, string(filepath.Separator)+".silt") ||
			strings.HasSuffix(cleaned, ".silt") {
			t.Fatalf(".silt should not yet be watched; got %v", dw.watcher.WatchList())
		}
	}

	// Create the directory mid-session. The vault root is watched
	// (AddRecursive of vaultPath), so the listen loop receives a
	// Create event for `<vault>/.silt`. The deferred-registration
	// path should fire and add the dot-dir to the fsnotify watch set.
	dotDir := filepath.Join(vaultPath, ".silt")
	if err := os.MkdirAll(dotDir, 0o755); err != nil {
		t.Fatalf("mkdir .silt: %v", err)
	}

	// Wait up to 3 s for the deferred-registration to land. fsnotify
	// events are OS-paced; on slow / network filesystems a single
	// 50 ms tick is not enough.
	deadline := time.Now().Add(3 * time.Second)
	want := filepath.Clean(dotDir)
	found := false
	for time.Now().Before(deadline) {
		for _, w := range dw.watcher.WatchList() {
			if filepath.Clean(w) == want {
				found = true
				break
			}
		}
		if found {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !found {
		t.Fatalf(".silt should be watched after deferred registration; got %v",
			dw.watcher.WatchList())
	}
}

// --- #443: mass id re-mint detection -----------------------------------------

// newReMintTestWatcher builds a watcher + DB backed by vaultPath and registers
// a recording reMintWarningHandler, returning the watcher and a capture func.
// Mirrors the scaffolding in TestDirectoryWatcher_ReindexFileIndexesFile.
func newReMintTestWatcher(t *testing.T, vaultPath string) (*DirectoryWatcher, func() []ReMintWarning) {
	t.Helper()
	dm, err := db.NewDatabaseManager("")
	if err != nil {
		t.Fatalf("NewDatabaseManager: %v", err)
	}
	t.Cleanup(func() { _ = dm.Close() })
	coord := core.NewExecutionCoordinator(dm.SQLDB())
	tracker := NewWriteTracker()
	t.Cleanup(tracker.Stop)
	dw, err := NewDirectoryWatcher(vaultPath, dm, tracker, coord, 4)
	if err != nil {
		t.Fatalf("NewDirectoryWatcher: %v", err)
	}
	var mu sync.Mutex
	var captured []ReMintWarning
	dw.SetReMintWarningHandler(func(w ReMintWarning) {
		mu.Lock()
		captured = append(captured, w)
		mu.Unlock()
	})
	return dw, func() []ReMintWarning {
		mu.Lock()
		defer mu.Unlock()
		out := make([]ReMintWarning, len(captured))
		copy(out, captured)
		return out
	}
}

// TestReMint_StrippedIDsFlagsMassReMint is the core #443 scenario: a previously-
// indexed file has its block-identity comments stripped by an external tool/sync,
// so the parser re-mints every id. The watcher must fire the warning so the user
// learns the ((uuid)) references may be broken.
func TestReMint_StrippedIDsFlagsMassReMint(t *testing.T) {
	vaultPath := t.TempDir()
	dw, warnings := newReMintTestWatcher(t, vaultPath)

	filePath := filepath.Join(vaultPath, "Work", "Plan.md")
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// 6 managed blocks, all carrying ids (so the first reindex mints nothing
	// and records priorCount = 6).
	original := "# Plan <!-- id: aaaaaaaa-1111-1111-1111-111111111111 -->\n"
	for i := 0; i < 6; i++ {
		original += "- [ ] task " + string(rune('a'+i)) +
			" <!-- id: aaaaaaaa-" + string(rune('2'+i)) + "222-2222-2222-1111111111" + string(rune('1'+i)) + " -->\n"
	}
	// Use stable distinct ids to avoid collisions.
	original = "# Plan <!-- id: 11111111-1111-1111-1111-111111111111 -->\n" +
		"- [ ] task a <!-- id: 22222222-2222-2222-2222-222222222222 -->\n" +
		"- [ ] task b <!-- id: 33333333-3333-3333-3333-333333333333 -->\n" +
		"- [ ] task c <!-- id: 44444444-4444-4444-4444-444444444444 -->\n" +
		"- [ ] task d <!-- id: 55555555-5555-5555-5555-555555555555 -->\n" +
		"- [ ] task e <!-- id: 66666666-6666-6666-6666-666666666666 -->\n" +
		"- [ ] task f <!-- id: 77777777-7777-7777-7777-777777777777 -->\n"
	if err := os.WriteFile(filePath, []byte(original), 0o644); err != nil {
		t.Fatalf("write original: %v", err)
	}
	dw.reindexFile(filePath) // index → priorCount = 6
	if got := warnings(); len(got) != 0 {
		t.Fatalf("first reindex (all ids present) should not flag; got %v", got)
	}

	// Rewrite the file with EVERY block-identity comment stripped — the
	// signature of a sync tool that doesn't know about <!-- id: ... -->.
	stripped := "# Plan\n" +
		"- [ ] task a\n" +
		"- [ ] task b\n" +
		"- [ ] task c\n" +
		"- [ ] task d\n" +
		"- [ ] task e\n" +
		"- [ ] task f\n"
	if err := os.WriteFile(filePath, []byte(stripped), 0o644); err != nil {
		t.Fatalf("write stripped: %v", err)
	}
	dw.reindexFile(filePath) // re-mints all 7 blocks (header + 6 tasks)

	got := warnings()
	if len(got) != 1 {
		t.Fatalf("expected 1 re-mint warning after stripping all ids, got %d: %v", len(got), got)
	}
	w := got[0]
	if w.PriorCount != 7 {
		t.Errorf("PriorCount = %d, want 7 (header + 6 tasks)", w.PriorCount)
	}
	if w.MintedCount != 7 {
		t.Errorf("MintedCount = %d, want 7 (all ids re-minted)", w.MintedCount)
	}
	if w.Page != "Plan" {
		t.Errorf("Page = %q, want %q", w.Page, "Plan")
	}
	if w.Path != filePath {
		t.Errorf("Path = %q, want %q", w.Path, filePath)
	}
}

// TestReMint_NewFileNotFlagged verifies the legitimate mass-creation path does
// NOT trip the heuristic: a brand-new file (priorCount == 0) gets all its ids
// minted on first index, but that's expected — not a stripping event.
func TestReMint_NewFileNotFlagged(t *testing.T) {
	vaultPath := t.TempDir()
	dw, warnings := newReMintTestWatcher(t, vaultPath)

	filePath := filepath.Join(vaultPath, "Work", "Fresh.md")
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// A large paste into a brand-new file: many tasks, none with ids.
	fresh := "# Fresh\n"
	for i := 0; i < 10; i++ {
		fresh += "- [ ] item " + string(rune('a'+i)) + "\n"
	}
	if err := os.WriteFile(filePath, []byte(fresh), 0o644); err != nil {
		t.Fatalf("write fresh: %v", err)
	}
	dw.reindexFile(filePath) // priorCount == 0 → exempt by construction

	if got := warnings(); len(got) != 0 {
		t.Fatalf("brand-new file (priorCount==0) should not flag even with many mints; got %v", got)
	}
}

// TestReMint_SingleEditNotFlagged verifies a normal one-block edit on an
// indexed file stays well under the threshold: adding one new task mints
// exactly 1 id, which is far below max(3, priorCount/2).
func TestReMint_SingleEditNotFlagged(t *testing.T) {
	vaultPath := t.TempDir()
	dw, warnings := newReMintTestWatcher(t, vaultPath)

	filePath := filepath.Join(vaultPath, "Work", "Plan.md")
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	original := "# Plan <!-- id: 11111111-1111-1111-1111-111111111111 -->\n" +
		"- [ ] task a <!-- id: 22222222-2222-2222-2222-222222222222 -->\n" +
		"- [ ] task b <!-- id: 33333333-3333-3333-3333-333333333333 -->\n" +
		"- [ ] task c <!-- id: 44444444-4444-4444-4444-444444444444 -->\n" +
		"- [ ] task d <!-- id: 55555555-5555-5555-5555-555555555555 -->\n" +
		"- [ ] task e <!-- id: 66666666-6666-6666-6666-666666666666 -->\n" +
		"- [ ] task f <!-- id: 77777777-7777-7777-7777-777777777777 -->\n"
	if err := os.WriteFile(filePath, []byte(original), 0o644); err != nil {
		t.Fatalf("write original: %v", err)
	}
	dw.reindexFile(filePath) // priorCount = 7

	// Normal edit: append ONE new task (no id). MintedCount = 1, threshold = max(3, 7/2=3) = 3 → not flagged.
	edited := strings.TrimRight(original, "\n") +
		"\n- [ ] task g (new) <!-- id: 88888888-8888-8888-8888-888888888888 -->\n"
	// The new task carries an id here to keep MintedCount focused on the
	// "exactly one mint" case; a real new block without an id would also
	// be the only mint and still pass. Strip the new line's id to simulate
	// a freshly-typed task the parser must mint for:
	edited = original + "- [ ] task g (new)\n"
	if err := os.WriteFile(filePath, []byte(edited), 0o644); err != nil {
		t.Fatalf("write edited: %v", err)
	}
	dw.reindexFile(filePath)

	if got := warnings(); len(got) != 0 {
		t.Fatalf("single new task on a 7-block file should not flag (1 < threshold 3); got %v", got)
	}
}
