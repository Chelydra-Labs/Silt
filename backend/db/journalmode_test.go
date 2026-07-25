package db

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

func TestIsTransientResultCode(t *testing.T) {
	cases := []struct {
		name string
		code int
		want bool
	}{
		{"IOERR primary", sqlite3.SQLITE_IOERR, true},
		{"IOERR_FSTAT extended (1802)", sqlite3.SQLITE_IOERR | (7 << 8), true},
		{"IOERR_WRITE extended", sqlite3.SQLITE_IOERR | (3 << 8), true},
		{"IOERR_SHMOPEN extended", sqlite3.SQLITE_IOERR | (18 << 8), true},
		{"BUSY", sqlite3.SQLITE_BUSY, true},
		{"LOCKED", sqlite3.SQLITE_LOCKED, true},
		{"OK", 0, false},
		{"CORRUPT", sqlite3.SQLITE_CORRUPT, false},
		{"CANTOPEN", sqlite3.SQLITE_CANTOPEN, false},
		{"CONSTRAINT", sqlite3.SQLITE_CONSTRAINT, false},
		{"ERROR", sqlite3.SQLITE_ERROR, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isTransientResultCode(c.code); got != c.want {
				t.Errorf("isTransientResultCode(%d) = %v, want %v", c.code, got, c.want)
			}
		})
	}
}

func TestIsTransientWALErr(t *testing.T) {
	t.Run("nil", func(t *testing.T) {
		if isTransientWALErr(nil) {
			t.Error("nil error should not be transient")
		}
	})
	t.Run("non-sqlite error", func(t *testing.T) {
		if isTransientWALErr(errors.New("plain")) {
			t.Error("plain error should not be transient")
		}
	})
	t.Run("real driver error is recognized", func(t *testing.T) {
		// A UNIQUE constraint violation yields a real *sqlite.Error from the
		// driver, proving errors.As + Code() extraction works end-to-end. It is
		// non-transient, so the classifier must reject it (no retry).
		err := realConstraintErr(t)
		var sqliteErr *sqlite.Error
		if !errors.As(err, &sqliteErr) {
			t.Fatalf("expected *sqlite.Error, got %T: %v", err, err)
		}
		// A UNIQUE violation reports an EXTENDED constraint code (e.g. 2067 =
		// SQLITE_CONSTRAINT | (8<<8)), proving modernc enables extended result
		// codes. Mask to the primary to assert the family.
		if sqliteErr.Code()&0xff != sqlite3.SQLITE_CONSTRAINT {
			t.Fatalf("expected CONSTRAINT primary code, got %d", sqliteErr.Code())
		}
		if isTransientWALErr(err) {
			t.Error("CONSTRAINT error should not be transient")
		}
	})
}

// realConstraintErr triggers a UNIQUE constraint violation on an in-memory DB
// and returns the resulting driver error.
func realConstraintErr(t *testing.T) error {
	t.Helper()
	dm := newTestDB(t)
	conn := dm.SQLDB()
	if _, err := conn.Exec("CREATE TABLE u(x TEXT UNIQUE)"); err != nil {
		t.Fatalf("create table: %v", err)
	}
	if _, err := conn.Exec("INSERT INTO u VALUES('a')"); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	_, realErr := conn.Exec("INSERT INTO u VALUES('a')") // duplicate
	if realErr == nil {
		t.Fatal("expected a constraint error, got nil")
	}
	return realErr
}

func TestWithBoundedRetry(t *testing.T) {
	// Use a tiny base so retry tests stay fast while still exercising a real
	// sleep + jitter path (ctx cancellation depends on it).
	const base = time.Millisecond
	const cap = 5 * time.Millisecond
	retryAny := func(error) bool { return true }
	retryNone := func(error) bool { return false }
	sentinel := errors.New("boom")

	t.Run("success on first attempt", func(t *testing.T) {
		var calls int32
		err := withBoundedRetry(context.Background(), 3, base, cap, retryAny, func() error {
			atomic.AddInt32(&calls, 1)
			return nil
		})
		if err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		if got := atomic.LoadInt32(&calls); got != 1 {
			t.Errorf("calls = %d, want 1", got)
		}
	})

	t.Run("non-retryable fails fast", func(t *testing.T) {
		var calls int32
		err := withBoundedRetry(context.Background(), 3, base, cap, retryNone, func() error {
			atomic.AddInt32(&calls, 1)
			return sentinel
		})
		if !errors.Is(err, sentinel) {
			t.Fatalf("err = %v, want sentinel", err)
		}
		if got := atomic.LoadInt32(&calls); got != 1 {
			t.Errorf("calls = %d, want 1 (no retry on non-retryable)", got)
		}
	})

	t.Run("retryable then succeeds", func(t *testing.T) {
		var calls int32
		err := withBoundedRetry(context.Background(), 3, base, cap, retryAny, func() error {
			n := atomic.AddInt32(&calls, 1)
			if n < 3 {
				return errors.New("transient")
			}
			return nil
		})
		if err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		if got := atomic.LoadInt32(&calls); got != 3 {
			t.Errorf("calls = %d, want 3", got)
		}
	})

	t.Run("retryable exhausts attempts", func(t *testing.T) {
		var calls int32
		err := withBoundedRetry(context.Background(), 3, base, cap, retryAny, func() error {
			atomic.AddInt32(&calls, 1)
			return sentinel
		})
		if !errors.Is(err, sentinel) {
			t.Fatalf("err = %v, want sentinel", err)
		}
		if got := atomic.LoadInt32(&calls); got != 3 {
			t.Errorf("calls = %d, want 3 (maxAttempts)", got)
		}
	})

	t.Run("context cancelled aborts", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		var calls int32
		// Cancel during the first sleep (before the 2nd attempt).
		go func() {
			time.Sleep(base / 2)
			cancel()
		}()
		err := withBoundedRetry(ctx, 5, base, cap, retryAny, func() error {
			atomic.AddInt32(&calls, 1)
			return sentinel
		})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
		if got := atomic.LoadInt32(&calls); got != 1 {
			t.Errorf("calls = %d, want 1 (cancelled before 2nd attempt)", got)
		}
	})
}

func TestJournalModeCache(t *testing.T) {
	resetJournalModeCacheForTest()
	t.Cleanup(resetJournalModeCacheForTest)

	if _, ok := cachedJournalMode("/never/seen"); ok {
		t.Fatal("expected cache miss on fresh cache")
	}
	rememberJournalMode("/d/wal", "wal")
	if mode, ok := cachedJournalMode("/d/wal"); !ok || mode != "wal" {
		t.Errorf("cached(/d/wal) = %q,%t, want wal,true", mode, ok)
	}
	rememberJournalMode("/d/wal", "truncate") // overwrite
	if mode, ok := cachedJournalMode("/d/wal"); !ok || mode != "truncate" {
		t.Errorf("cached(/d/wal) after overwrite = %q,%t, want truncate,true", mode, ok)
	}
	resetJournalModeCacheForTest()
	if _, ok := cachedJournalMode("/d/wal"); ok {
		t.Error("expected cache miss after reset")
	}
}

func TestApplyJournalMode_InMemoryNoWarning(t *testing.T) {
	dm := newTestDB(t) // in-memory (path == "")
	mode, warn, err := dm.applyJournalMode(context.Background(), dm.SQLDB())
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if mode != "memory" {
		t.Errorf("mode = %q, want memory", mode)
	}
	if warn != "" {
		t.Errorf("warn = %q, want empty for in-memory", warn)
	}
	if w := dm.Warnings(); len(w) != 0 {
		t.Errorf("Warnings() = %v, want empty", w)
	}
}

func TestApplyJournalMode_OnDiskWAL(t *testing.T) {
	resetJournalModeCacheForTest()
	t.Cleanup(resetJournalModeCacheForTest)

	dm, _ := newOnDiskDB(t) // real on-disk WAL DB on local temp dir
	mode, warn, err := dm.applyJournalMode(context.Background(), dm.SQLDB())
	if err != nil {
		t.Fatalf("err = %v, want nil (local disk supports WAL)", err)
	}
	if mode != "wal" {
		t.Errorf("mode = %q, want wal", mode)
	}
	if warn != "" {
		t.Errorf("warn = %q, want empty on clean WAL", warn)
	}
	// The decision should be cached for the dir.
	dir := filepath.Dir(dm.Path())
	if m, ok := cachedJournalMode(dir); !ok || m != "wal" {
		t.Errorf("cache = %q,%t, want wal,true", m, ok)
	}
}
