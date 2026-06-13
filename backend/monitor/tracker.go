package monitor

import (
	"sync"
	"time"
)

const (
	writeTrackerCooldown    = 300 * time.Millisecond
	writeTrackerSweepMargin = 2 // evict entries older than cooldown * this factor
)

type WriteTracker struct {
	mu           sync.Mutex
	activeWrites map[string]time.Time

	stopOnce sync.Once
	stopCh   chan struct{}
}

func NewWriteTracker() *WriteTracker {
	wt := &WriteTracker{
		activeWrites: make(map[string]time.Time),
		stopCh:       make(chan struct{}),
	}
	go wt.runSweeper()
	return wt
}

func (wt *WriteTracker) RegisterWrite(filepath string) {
	wt.mu.Lock()
	defer wt.mu.Unlock()
	wt.activeWrites[filepath] = time.Now()
}

func (wt *WriteTracker) IsSelfGenerated(filepath string) bool {
	wt.mu.Lock()
	defer wt.mu.Unlock()
	t, exists := wt.activeWrites[filepath]
	if !exists {
		return false
	}
	// If write event fired within 300ms of our atomic write, ignore it
	if time.Since(t) < writeTrackerCooldown {
		delete(wt.activeWrites, filepath)
		return true
	}
	// Expired entry: drop it so the map doesn't grow unbounded for files
	// that are written but never re-checked within the cooldown window.
	delete(wt.activeWrites, filepath)
	return false
}

// PruneExpired removes entries older than writeTrackerCooldown *
// writeTrackerSweepMargin. It is safe to call concurrently with
// RegisterWrite / IsSelfGenerated.
func (wt *WriteTracker) PruneExpired() int {
	cutoff := time.Now().Add(-writeTrackerCooldown * writeTrackerSweepMargin)
	wt.mu.Lock()
	defer wt.mu.Unlock()
	pruned := 0
	for path, t := range wt.activeWrites {
		if t.Before(cutoff) {
			delete(wt.activeWrites, path)
			pruned++
		}
	}
	return pruned
}

// Stop terminates the background sweeper. Safe to call multiple times.
func (wt *WriteTracker) Stop() {
	wt.stopOnce.Do(func() { close(wt.stopCh) })
}

func (wt *WriteTracker) runSweeper() {
	ticker := time.NewTicker(writeTrackerCooldown * writeTrackerSweepMargin)
	defer ticker.Stop()
	for {
		select {
		case <-wt.stopCh:
			return
		case <-ticker.C:
			wt.PruneExpired()
		}
	}
}
