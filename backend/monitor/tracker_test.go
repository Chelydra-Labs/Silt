package monitor

import (
	"testing"
	"time"
)

func TestWriteTracker_ImmediateCheck(t *testing.T) {
	wt := NewWriteTracker()
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
