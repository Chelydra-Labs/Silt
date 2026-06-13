package core

import (
	"database/sql"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func newTestCoordinator(t *testing.T) *ExecutionCoordinator {
	t.Helper()
	sqlDB, err := sql.Open("sqlite", "file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return NewExecutionCoordinator(sqlDB)
}

func TestExecutionCoordinator_DBSerializesWrites(t *testing.T) {
	ec := newTestCoordinator(t)

	const goroutines = 8
	var inFlight int32
	var maxInFlight int32
	var wg sync.WaitGroup

	ec.WithDBWrite(func() {
		// Hold the write lock while we measure concurrency.
	})

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ec.WithDBWrite(func() {
				cur := atomic.AddInt32(&inFlight, 1)
				for {
					m := atomic.LoadInt32(&maxInFlight)
					if cur <= m || atomic.CompareAndSwapInt32(&maxInFlight, m, cur) {
						break
					}
				}
				time.Sleep(5 * time.Millisecond)
				atomic.AddInt32(&inFlight, -1)
			})
		}()
	}
	wg.Wait()

	if got := atomic.LoadInt32(&maxInFlight); got != 1 {
		t.Errorf("expected write lock to serialize, observed max in-flight = %d", got)
	}
}

func TestExecutionCoordinator_DBReadAllowsConcurrency(t *testing.T) {
	ec := newTestCoordinator(t)

	const goroutines = 8
	var inFlight int32
	var maxInFlight int32
	var wg sync.WaitGroup

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ec.WithDBRead(func() {
				cur := atomic.AddInt32(&inFlight, 1)
				for {
					m := atomic.LoadInt32(&maxInFlight)
					if cur <= m || atomic.CompareAndSwapInt32(&maxInFlight, m, cur) {
						break
					}
				}
				time.Sleep(5 * time.Millisecond)
				atomic.AddInt32(&inFlight, -1)
			})
		}()
	}
	wg.Wait()

	if got := atomic.LoadInt32(&maxInFlight); got < 2 {
		t.Errorf("expected read lock to allow concurrency, observed max in-flight = %d", got)
	}
}

func TestExecutionCoordinator_FileWriteLocksArePerFile(t *testing.T) {
	ec := newTestCoordinator(t)
	fileA := filepath.Join(t.TempDir(), "a.md")
	fileB := filepath.Join(t.TempDir(), "b.md")

	var overlap int32
	var maxOverlap int32
	var wg sync.WaitGroup
	start := make(chan struct{})

	hold := func() {
		<-start
		cur := atomic.AddInt32(&overlap, 1)
		for {
			m := atomic.LoadInt32(&maxOverlap)
			if cur <= m || atomic.CompareAndSwapInt32(&maxOverlap, m, cur) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		atomic.AddInt32(&overlap, -1)
	}

	wg.Add(2)
	go func() {
		defer wg.Done()
		ec.LockFileWrite(fileA, hold)
	}()
	go func() {
		defer wg.Done()
		ec.LockFileWrite(fileB, hold)
	}()

	close(start)
	wg.Wait()

	// Different files should run in parallel; if the locks were shared, the
	// max in-flight would be 1.
	if got := atomic.LoadInt32(&maxOverlap); got < 2 {
		t.Errorf("expected per-file locks to allow concurrency, observed max overlap = %d", got)
	}
}

func TestExecutionCoordinator_SameFileWritesAreSerialized(t *testing.T) {
	ec := newTestCoordinator(t)
	file := filepath.Join(t.TempDir(), "shared.md")

	var overlap int32
	var maxOverlap int32
	var wg sync.WaitGroup
	start := make(chan struct{})

	hold := func() {
		<-start
		cur := atomic.AddInt32(&overlap, 1)
		for {
			m := atomic.LoadInt32(&maxOverlap)
			if cur <= m || atomic.CompareAndSwapInt32(&maxOverlap, m, cur) {
				break
			}
		}
		time.Sleep(5 * time.Millisecond)
		atomic.AddInt32(&overlap, -1)
	}

	const goroutines = 4
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ec.LockFileWrite(file, hold)
		}()
	}
	close(start)
	wg.Wait()

	if got := atomic.LoadInt32(&maxOverlap); got != 1 {
		t.Errorf("expected same-file writes to be serialized, observed max overlap = %d", got)
	}
}

func TestExecutionCoordinator_WithDBReadResultReturnsError(t *testing.T) {
	ec := newTestCoordinator(t)

	sentinel := errSentinel("boom")
	got := ec.WithDBReadResult(func() error {
		return sentinel
	})
	if got != sentinel {
		t.Errorf("expected sentinel error to propagate, got %v", got)
	}

	if err := ec.WithDBReadResult(func() error { return nil }); err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
}

type errSentinel string

func (e errSentinel) Error() string { return string(e) }
