package monitor

import (
	"sync"
	"time"
)

const (
	defaultCooldown    = 300 * time.Millisecond
	defaultSweepMargin = 2 // evict entries older than cooldown * margin
)

// Option configures a WriteTracker.
type Option func(*WriteTracker)

// WithCooldown overrides how long RegisterWrite → IsSelfGenerated
// fingerprints are considered self-generated (default 300ms).
func WithCooldown(d time.Duration) Option {
	return func(wt *WriteTracker) {
		if d > 0 {
			wt.cooldown = d
		}
	}
}

// WithSweepMargin sets the multiplier used for background pruning.
// Entries older than cooldown * margin are evicted (default 2, so
// entries older than 600ms get pruned).
func WithSweepMargin(m int) Option {
	return func(wt *WriteTracker) {
		if m > 0 {
			wt.sweepMargin = m
		}
	}
}

type WriteTracker struct {
	cooldown    time.Duration
	sweepMargin int

	mu           sync.Mutex
	activeWrites map[string]time.Time

	stopOnce sync.Once
	stopCh   chan struct{}
}

func NewWriteTracker(opts ...Option) *WriteTracker {
	wt := &WriteTracker{
		cooldown:     defaultCooldown,
		sweepMargin:  defaultSweepMargin,
		activeWrites: make(map[string]time.Time),
		stopCh:       make(chan struct{}),
	}
	for _, o := range opts {
		o(wt)
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
	if time.Since(t) < wt.cooldown {
		delete(wt.activeWrites, filepath)
		return true
	}
	delete(wt.activeWrites, filepath)
	return false
}

// PruneExpired removes entries older than cooldown * sweepMargin. Safe
// to call concurrently with RegisterWrite / IsSelfGenerated.
func (wt *WriteTracker) PruneExpired() int {
	cutoff := time.Now().Add(-wt.cooldown * time.Duration(wt.sweepMargin))
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
	period := wt.cooldown * time.Duration(wt.sweepMargin)
	ticker := time.NewTicker(period)
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
