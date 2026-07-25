package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/rand/v2"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// Resilient journal-mode selection for cloud-synced vaults.
//
// When the index lived inside the synced vault, a sync engine or antivirus
// could briefly lock the freshly-created -wal/-shm sidecars at the moment
// SQLite switched into WAL mode, surfacing as SQLITE_IOERR_FSTAT (1802) and a
// hard vault-open failure. The helpers here retry the WAL PRAGMA with bounded
// jittered backoff on transient IOERR/BUSY, fall back to a rollback journal
// (TRUNCATE) when WAL stays unavailable, and memoize the per-directory
// decision so a known-bad mount is not re-probed every open.

// Retry tuning. A cloud-sync engine or AV typically releases a sidecar lock
// within a few hundred milliseconds; three jittered attempts span roughly
// ~300ms worst case (two inter-attempt sleeps: ~50ms then ~100ms, each with
// decorrelated jitter up to 2x), well under the 5s busy_timeout and SQLite's
// own internal retries. base is the first sleep, doubled per attempt up to cap.
const (
	walRetryMaxAttempts = 3
	walRetryBaseDelay   = 50 * time.Millisecond
	walRetryCapDelay    = 250 * time.Millisecond
)

// isTransientResultCode reports whether a SQLite result code is a transient
// failure worth retrying the WAL PRAGMA against: the IOERR family (transient
// fstat/read/write/shm failures from a sync engine or AV briefly locking the
// sidecars) plus BUSY and LOCKED (contended byte-range locks). Non-transient
// codes (CORRUPT, CANTOPEN on a genuinely missing dir, CONSTRAINT, …) return
// false so the caller falls back immediately rather than stalling.
//
// modernc.org/sqlite enables extended result codes on every connection open,
// so a driver error's Code() is the extended value (e.g. 1802 for FSTAT).
// Extended IOERR codes carry the primary code (10) in their low byte, so
// masking the low byte collapses the whole family to a single match — robust
// whether or not extended codes happen to be enabled.
func isTransientResultCode(code int) bool {
	switch code & 0xff {
	case sqlite3.SQLITE_IOERR, sqlite3.SQLITE_BUSY, sqlite3.SQLITE_LOCKED:
		return true
	}
	return false
}

// isTransientWALErr reports whether err is a transient SQLite failure worth
// retrying the WAL PRAGMA against. Non-SQLite errors return false.
func isTransientWALErr(err error) bool {
	var sqliteErr *sqlite.Error
	if !errors.As(err, &sqliteErr) {
		return false
	}
	return isTransientResultCode(sqliteErr.Code())
}

// withBoundedRetry calls op up to maxAttempts times. It retries only while
// retryable(err) is true, sleeping a decorrelated-jitter exponential backoff
// between attempts (base*2^attempt, capped at cap, randomized within
// [delay, 2*delay) so concurrent retries spread). ctx cancellation interrupts
// the sleep and returns ctx.Err(). The retryable predicate is injected so the
// loop's timing/attempt behavior is unit-testable without a live database.
//
// Precondition: base and capDelay must be > 0 — the jitter uses math/rand/v2,
// whose Int64N panics on a zero-sized range.
func withBoundedRetry(ctx context.Context, maxAttempts int, base, capDelay time.Duration, retryable func(error) bool, op func() error) error {
	var err error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		err = op()
		if err == nil {
			return nil
		}
		if !retryable(err) || attempt+1 >= maxAttempts {
			return err
		}
		delay := base << uint(attempt) // base * 2^attempt
		if delay > capDelay {
			delay = capDelay
		}
		// Defensive clamp: a misconfigured base/cap must not crash the
		// vault-open path via rand.Int64N(0).
		if delay < 1 {
			delay = 1
		}
		// Decorrelated jitter: randomize within [delay, 2*delay) so retries
		// spread and a sustained lock gets distinct chances to clear.
		delay += time.Duration(rand.Int64N(int64(delay)))
		t := time.NewTimer(delay)
		select {
		case <-t.C:
		case <-ctx.Done():
			t.Stop()
			return ctx.Err()
		}
	}
	return err
}

// journalModeCache memoizes the per-directory journal-mode decision so a
// filesystem known to reject WAL is not re-probed (with its retry budget) on
// every open within a session. Keyed by the index's parent directory absolute
// path. Process-scoped only — a fresh launch re-probes once, which is cheap
// when WAL succeeds on the first try (the common case). Mirrors the
// productionhardening.org JOURNAL_FALLBACK guidance: cache the decision rather
// than probing on every open.
var (
	journalModeCacheMu sync.Mutex
	journalModeCache   = map[string]string{}
)

func cachedJournalMode(dir string) (mode string, ok bool) {
	journalModeCacheMu.Lock()
	defer journalModeCacheMu.Unlock()
	mode, ok = journalModeCache[dir]
	return
}

func rememberJournalMode(dir, mode string) {
	journalModeCacheMu.Lock()
	defer journalModeCacheMu.Unlock()
	journalModeCache[dir] = mode
}

// resetJournalModeCacheForTest clears the process cache. Tests only.
func resetJournalModeCacheForTest() {
	journalModeCacheMu.Lock()
	defer journalModeCacheMu.Unlock()
	journalModeCache = map[string]string{}
}

// applyJournalMode selects and applies the SQLite journal mode for the index,
// resilient to transient locks on cloud-synced/network folders.
//
// Strategy (productionhardening.org JOURNAL_FALLBACK tier):
//  1. In-memory DB (dm.path == ""): WAL PRAGMA is a no-op; mode stays "memory".
//  2. If the parent dir is already known (cached) to structurally reject WAL,
//     set TRUNCATE directly, skipping the retry budget.
//  3. Otherwise retry `PRAGMA journal_mode = WAL` with bounded jittered backoff
//     on transient IOERR/BUSY.
//  4. After a successful PRAGMA, belt-and-suspenders re-read the mode (#79):
//     some mounts silently downgrade away from WAL.
//  5. On failure, fall back to TRUNCATE (a rollback journal needs no shared
//     memory, so it survives on a synced/network mount).
//
// A *transient* failure that exhausted the retry budget (a fleeting AV/sync
// lock) is NOT cached and yields a "restart may help" warning — a later open
// re-probes WAL so the user is not pinned to degraded mode for a blip. Only a
// *structural* rejection (silent downgrade or non-transient error) caches the
// directory as truncate-only.
//
// Returns the chosen mode, a non-empty warning string when the vault opened in
// degraded (non-WAL) mode, and a hard error only when even TRUNCATE fails (a
// genuinely unusable index location). Called from initSchema under its
// handle() lease, so it operates on the passed *sql.DB without re-entering
// handle().
func (dm *DatabaseManager) applyJournalMode(ctx context.Context, db *sql.DB) (mode, warning string, err error) {
	if dm.path == "" {
		// In-memory shared-cache DB: WAL is a safe no-op (mode stays "memory").
		_, _ = db.Exec("PRAGMA journal_mode = WAL;")
		return "memory", "", nil
	}

	dir := filepath.Dir(dm.path)

	// Known structurally-bad mount: skip the probe and the retry budget.
	if cached, ok := cachedJournalMode(dir); ok && cached == "truncate" {
		if _, ferr := db.Exec("PRAGMA journal_mode = TRUNCATE;"); ferr != nil {
			return "truncate", "", fmt.Errorf("set journal mode TRUNCATE on degraded location %s: %w", dir, ferr)
		}
		return "truncate", "", nil
	}

	// Try WAL with bounded retry on transient sync/AV locks.
	walErr := withBoundedRetry(ctx, walRetryMaxAttempts, walRetryBaseDelay, walRetryCapDelay, isTransientWALErr, func() error {
		_, err := db.Exec("PRAGMA journal_mode = WAL;")
		return err
	})

	// structural is true unless the failure was a transient retry exhaustion.
	structural := true
	if walErr == nil {
		reported, qerr := readJournalMode(db)
		if qerr == nil && strings.EqualFold(reported, "wal") {
			rememberJournalMode(dir, "wal")
			return "wal", "", nil
		}
		if qerr != nil {
			// A read flake is transient — don't pin the directory for it.
			walErr = qerr
			structural = false
		} else {
			// Silent downgrade (returned a non-wal mode) is structural.
			walErr = fmt.Errorf("%w: PRAGMA journal_mode returned %q instead of %q", ErrWALRejected, reported, "wal")
			structural = true
		}
	} else if isTransientWALErr(walErr) {
		structural = false
	}

	// Fallback: TRUNCATE. No shared memory required.
	if _, ferr := db.Exec("PRAGMA journal_mode = TRUNCATE;"); ferr != nil {
		// Even the rollback journal fails — the location is unusable. Fail
		// loudly rather than open a silently-broken index.
		return "", "", fmt.Errorf("journal mode unavailable: WAL failed (%v) and TRUNCATE fallback also failed: %w", walErr, ferr)
	}
	if structural {
		rememberJournalMode(dir, "truncate")
		return "truncate", fmt.Sprintf(
			"WAL journal mode is not supported at %s (%v); using TRUNCATE (rollback journal). The vault works, but this index location cannot use WAL — for best performance move the vault to a purely local folder.",
			dir, walErr), nil
	}
	// Transient exhaustion: do NOT cache — a later open re-probes WAL.
	return "truncate", fmt.Sprintf(
		"WAL setup failed after retries at %s (%v), likely a transient sync or antivirus lock; using TRUNCATE for this session. Restarting Silt may restore full performance.",
		dir, walErr), nil
}

// readJournalMode reads PRAGMA journal_mode, retrying once on a transient
// SQLite error so a one-off read flake is not mistaken for a structural WAL
// rejection (which would pin the directory to TRUNCATE for the whole session).
func readJournalMode(db *sql.DB) (string, error) {
	var reported string
	var err error
	for attempt := 0; attempt < 2; attempt++ {
		err = db.QueryRow("PRAGMA journal_mode;").Scan(&reported)
		if err == nil {
			return reported, nil
		}
		if !isTransientWALErr(err) {
			return "", err
		}
	}
	return "", err
}
