package monitor

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"notes-sharp/backend/core"
	"notes-sharp/backend/db"
)

func TestDirectoryWatcher_ReindexFileHoldsFileLock(t *testing.T) {
	// Verifies the fix for the lost-update race: reindexFile must hold
	// the per-file IO lock for the duration of read+parse+write+index
	// so a concurrent UpdateBlockState cannot land between the watcher's
	// read and the watcher's eventual write.
	vaultPath := t.TempDir()

	dm, err := db.NewDatabaseManager()
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
			"- [ ] TODO TASK x <!-- id: bbbb2222-bbbb-bbbb-bbbb-bbbbbbbbbbbb -->\n",
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

	dm, err := db.NewDatabaseManager()
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

	filePath := filepath.Join(vaultPath, "Work", "Journal", "2026-06-13.md")
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filePath, []byte(
		"# Today <!-- id: 11111111-1111-1111-1111-111111111111 -->\n"+
			"\n"+
			"- [ ] TODO TASK sample <!-- id: 22222222-2222-2222-2222-222222222222 -->\n",
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
