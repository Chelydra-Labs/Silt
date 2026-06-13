package monitor

import (
	"sync"
	"testing"
	"time"
)

func TestWriteTracker_ImmediateCheck(t *testing.T) {
	wt := NewWriteTracker()
	defer wt.Stop()
	filePath := "test_file.md"

	// Check on non-existent file
	if wt.IsSelfGenerated(filePath) {
		t.Errorf("Expected false for unregistered file write")
	}

	// Register write
	wt.RegisterWrite(filePath)

	// Immediate check should be true
	if !wt.IsSelfGenerated(filePath) {
		t.Errorf("Expected true for immediate check")
	}

	// Second check immediately after should be false (since it deletes on match)
	if wt.IsSelfGenerated(filePath) {
		t.Errorf("Expected false for second immediate check")
	}
}

func TestWriteTracker_CooldownTimeout(t *testing.T) {
	wt := NewWriteTracker()
	defer wt.Stop()
	filePath := "test_file.md"

	wt.RegisterWrite(filePath)

	// Wait 350ms (longer than the 300ms cooldown)
	time.Sleep(350 * time.Millisecond)

	// Check after timeout should be false
	if wt.IsSelfGenerated(filePath) {
		t.Errorf("Expected false after 350ms cooldown timeout")
	}
}

func TestWriteTracker_DropsExpiredEntry(t *testing.T) {
	// After the cooldown elapses, the entry must be removed from the map
	// to prevent unbounded memory growth across many file writes.
	wt := NewWriteTracker()
	defer wt.Stop()
	filePath := "leak_check.md"

	wt.RegisterWrite(filePath)
	if len(wt.activeWrites) != 1 {
		t.Fatalf("expected 1 entry after RegisterWrite, got %d", len(wt.activeWrites))
	}

	// First check during the cooldown returns true and clears the entry.
	time.Sleep(50 * time.Millisecond)
	if !wt.IsSelfGenerated(filePath) {
		t.Errorf("expected true during cooldown")
	}
	if len(wt.activeWrites) != 0 {
		t.Errorf("expected entry to be cleared during-cooldown check, got %d", len(wt.activeWrites))
	}

	// Now exercise the post-cooldown path. Register again and wait past the
	// 300ms window before checking.
	wt.RegisterWrite(filePath)
	time.Sleep(350 * time.Millisecond)
	if wt.IsSelfGenerated(filePath) {
		t.Errorf("expected false after cooldown")
	}
	if len(wt.activeWrites) != 0 {
		t.Errorf("expected expired entry to be removed, got %d entries", len(wt.activeWrites))
	}
}

func TestWriteTracker_PruneExpired(t *testing.T) {
	// PruneExpired should remove entries older than cooldown * margin and
	// leave recent entries untouched. This covers the case where
	// RegisterWrite is called but IsSelfGenerated never fires for a path
	// (e.g., the file was deleted before fsnotify delivered an event).
	wt := NewWriteTracker()
	defer wt.Stop()

	// Old entry (manually backdate past the sweep cutoff).
	wt.activeWrites["old.md"] = time.Now().Add(-time.Second)
	// Recent entry within the cutoff.
	wt.activeWrites["fresh.md"] = time.Now()

	pruned := wt.PruneExpired()
	if pruned != 1 {
		t.Errorf("expected 1 pruned entry, got %d", pruned)
	}
	if _, ok := wt.activeWrites["old.md"]; ok {
		t.Errorf("old entry should have been pruned")
	}
	if _, ok := wt.activeWrites["fresh.md"]; !ok {
		t.Errorf("fresh entry should have been retained")
	}
}

func TestWriteTracker_BackgroundSweeper(t *testing.T) {
	// Insert a stale entry, then wait long enough for the background
	// sweeper (period = cooldown * margin = 600ms) to evict it.
	wt := NewWriteTracker()
	defer wt.Stop()

	wt.RegisterWrite("ghost.md")
	// Backdate the entry past the sweep cutoff so the next sweeper tick
	// removes it.
	wt.mu.Lock()
	wt.activeWrites["ghost.md"] = time.Now().Add(-time.Second)
	wt.mu.Unlock()

	// Give the sweeper at least one tick to run, polling under the same
	// mutex the sweeper uses to avoid a data race.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		wt.mu.Lock()
		_, present := wt.activeWrites["ghost.md"]
		wt.mu.Unlock()
		if !present {
			return // success
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Errorf("background sweeper did not evict stale entry within 2s")
}

func TestWriteTracker_StopIsIdempotent(t *testing.T) {
	wt := NewWriteTracker()
	wt.Stop()
	wt.Stop() // must not panic
}

// Sanity guard: PruneExpired must be safe to call concurrently with the
// other public methods.
func TestWriteTracker_PruneConcurrent(t *testing.T) {
	wt := NewWriteTracker()
	defer wt.Stop()
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(3)
		go func() { defer wg.Done(); wt.RegisterWrite("concurrent.md") }()
		go func() { defer wg.Done(); wt.IsSelfGenerated("concurrent.md") }()
		go func() { defer wg.Done(); wt.PruneExpired() }()
	}
	wg.Wait()
}
