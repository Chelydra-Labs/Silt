package db

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"sync"
	"sync/atomic"

	_ "modernc.org/sqlite"
	// sqlite-vec: registers vec0 virtual tables + vec_distance_* on every
	// connection opened by the modernc driver (inert on the core index —
	// no schema change; available to per-plugin DBs, #213). Pure-Go via
	// ccgo transpilation; no CGo.
	_ "modernc.org/sqlite/vec"
)

// ErrNetworkFilesystem is returned when the vault index path is detected to be
// on a network filesystem (NFS/SMB/CIFS/…). WAL mode requires shared memory
// which network mounts cannot provide, so the index would fail with an opaque
// SQLite error. This sentinel lets the UI surface a clear, actionable message
// instead (#79).
var ErrNetworkFilesystem = errors.New("network filesystem detected")

// IsNetworkFS reports whether path lives on a network filesystem that cannot
// support SQLite WAL (NFS/SMB/CIFS/…). Returns nil for local filesystems and
// on platforms without a detector. Exported so the vault mover (#141) can
// reject a network destination with the same clear message the index-opener
// surfaces, without re-implementing the per-platform detection.
func IsNetworkFS(path string) error {
	return detectNetworkFilesystem(path)
}

// ErrWALRejected labels a structural WAL rejection: the database is on-disk
// but SQLite did not accept WAL mode (the PRAGMA returned a different journal
// mode, or the mount silently downgraded). It is wrapped into the cause carried
// by applyJournalMode's degraded-mode warning (not surfaced as a hard error);
// classifyWALFallback uses it to mark the silent-downgrade case as structural
// rather than transient.
var ErrWALRejected = errors.New("WAL mode rejected by the filesystem")

// ErrDBClosed is returned by DatabaseManager methods after Close. Callers
// (including vault-switch IPC that races teardown) get a loud sentinel instead
// of a nil-deref panic (#517).
var ErrDBClosed = errors.New("database manager is closed")

type DatabaseManager struct {
	// dbMu serializes Close against in-flight ops that hold a read lease via
	// withDB (#517). The handle pointer itself is also published through
	// atomic ops so a bare field read cannot tear; withDB is the contract
	// for package methods.
	dbMu sync.RWMutex
	// db is the live handle. nil after Close. Use withDB / handle() rather
	// than reading this field directly from new code.
	db       atomic.Pointer[sql.DB]
	path     string   // "" for the in-memory shared-cache DB; otherwise the on-disk file path
	warnings []string // soft caveats from initSchema (e.g. WAL fell back to TRUNCATE)

	// unlinkedScanCache holds short-lived FTS candidate windows so residual
	// Load more does not re-pay loop-fill. Generation bumps on any blocks
	// mutation / Close so stale windows miss without sweeping under readers.
	unlinkedScanCacheMu  sync.Mutex
	unlinkedScanCacheGen uint64
	unlinkedScanCache    map[unlinkedScanCacheKey]unlinkedScanCacheEntry

	// indexerTestingHook is a deterministic test seam: nil in production
	// (the hot path is one atomic load + nil-check); tests install a hook
	// via setIndexerTestingHook to force mid-transaction failures so they
	// can prove atomic visibility and rollback. See index_hook.go.
	indexerTestingHook indexerTestingHook
}

// FileStat records the last-seen filesystem attributes of an indexed file, used
// to skip unchanged files on a warm restart (#29). MTime is Unix nanoseconds
// so it round-trips losslessly across SQLite's INTEGER storage.
type FileStat struct {
	MTime     int64
	Size      int64
	IndexedAt int64
}

// NewDatabaseManager opens the Silt index. Pass the on-disk path (resolved by
// paths.LocalIndexPath for production — a per-user local DataDir, no longer
// inside the synced vault) for the production persistent WAL database, or ""
// for an ephemeral in-memory shared-cache DB (used by tests and before a vault
// is open).
//
// On-disk databases run in WAL mode (journal_mode is persistent in the file
// header, so it is set once and inherited by every subsequent connection,
// including the plugin read-only handle). The remaining pragmas
// (synchronous=NORMAL, temp_store=MEMORY, mmap_size, busy_timeout, cache_size,
// foreign_keys) are per-connection and are re-applied on every open. On an
// in-memory DB, `journal_mode=WAL` is a safe no-op (SQLite keeps "memory").
func NewDatabaseManager(dbPath string) (*DatabaseManager, error) {
	// Pre-open guard (#79): detect network filesystems before sql.Open so the
	// user gets a clear "move to a local folder" message instead of an opaque
	// SQLite shared-memory error. Only check for on-disk paths.
	if dbPath != "" {
		if err := detectNetworkFilesystem(filepath.Dir(dbPath)); err != nil {
			return nil, err
		}
	}

	dsn := dbPath
	if dsn == "" {
		// cache=shared lets a second connection (pluginRawQuery's read-only
		// handle) attach to the same ephemeral DB during tests.
		dsn = "file::memory:?cache=shared"
	}
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open SQLite: %w", err)
	}

	// Cap the connection pool at one. The ExecutionCoordinator already
	// serializes all DB access at the Go level; a larger pool would only
	// obscure the locking story without yielding usable concurrency. WAL
	// still helps (OS-level sync blocking moves to the WAL append path).
	sqlDB.SetMaxOpenConns(1)

	dm := &DatabaseManager{path: dbPath}
	dm.db.Store(sqlDB)
	if err := dm.initSchema(); err != nil {
		sqlDB.Close()
		dm.db.Store(nil)
		return nil, err
	}

	return dm, nil
}

// handle returns the live *sql.DB under a read lease so Close waits for the
// caller to finish. The returned release func MUST be deferred on the success
// path (not called when err != nil). Nested calls that would re-enter handle
// while the lease is held deadlock — pass *sql.DB into helpers, or when a
// *sql.Tx is already open skip handle and use the tx only (#517).
func (dm *DatabaseManager) handle() (db *sql.DB, release func(), err error) {
	dm.dbMu.RLock()
	db = dm.db.Load()
	if db == nil {
		dm.dbMu.RUnlock()
		return nil, func() {}, ErrDBClosed
	}
	return db, dm.dbMu.RUnlock, nil
}

// withDB runs fn while holding a read lock so Close (write lock) waits for
// in-flight work. Post-close calls return ErrDBClosed (#517).
func (dm *DatabaseManager) withDB(fn func(db *sql.DB) error) error {
	db, release, err := dm.handle()
	if err != nil {
		return err
	}
	defer release()
	return fn(db)
}

// SQLDB exposes the underlying *sql.DB handle. Callers MUST serialize access
// through core.ExecutionCoordinator (e.g. WithDBRead/WithDBWrite) to avoid
// race conditions on the shared database.
//
// Returns nil after Close. Prefer DatabaseManager methods (withDB) over
// long-lived use of this pointer across vault teardown (#517).
func (dm *DatabaseManager) SQLDB() *sql.DB {
	return dm.db.Load()
}

// Path returns the on-disk index path ("" for the in-memory DB). Used by the
// watcher/app to open the plugin read-only handle against the same file.
func (dm *DatabaseManager) Path() string {
	return dm.path
}

// IsOnDisk reports whether the index is a persistent on-disk database (true)
// or an ephemeral in-memory one (false).
func (dm *DatabaseManager) IsOnDisk() bool {
	return dm.path != ""
}

// Warnings returns soft caveats produced while opening the index (e.g. WAL was
// unavailable and the index opened in degraded TRUNCATE mode). Empty when the
// index opened cleanly. Populated by initSchema; safe to read after
// NewDatabaseManager returns. Callers surface these as non-blocking warnings
// (e.g. the vault:init-warnings channel) rather than errors.
func (dm *DatabaseManager) Warnings() []string {
	if len(dm.warnings) == 0 {
		return nil
	}
	out := make([]string, len(dm.warnings))
	copy(out, dm.warnings)
	return out
}

func (dm *DatabaseManager) Close() error {
	// Write lock waits for in-flight withDB readers, then swaps the handle
	// to nil so new callers see ErrDBClosed / nil SQLDB (#517).
	dm.dbMu.Lock()
	defer dm.dbMu.Unlock()
	dm.invalidateUnlinkedScanCache()
	db := dm.db.Swap(nil)
	if db == nil {
		return nil // already closed (idempotent)
	}
	// Merge any pending WAL frames into the main file on a clean close so the
	// WAL does not grow unbounded across sessions. On in-memory databases this
	// is a no-op. A checkpoint failure is logged but not surfaced: SQLite
	// auto-checkpoints anyway and recovers on next open.
	if _, err := db.Exec("PRAGMA wal_checkpoint(TRUNCATE);"); err != nil {
		log.Printf("db.Close: wal_checkpoint failed: %v", err)
	}
	// sql.DB.Close waits for queries that have already started, then
	// rejects new ones — covers callers that used handle()/SQLDB() without
	// withDB for a single op.
	return db.Close()
}

// Checkpoint forces a WAL checkpoint (TRUNCATE). Called on shutdown and after
// the startup reindex pass to keep the WAL file bounded. No-op on in-memory.
func (dm *DatabaseManager) Checkpoint() error {
	return dm.withDB(func(db *sql.DB) error {
		_, err := db.Exec("PRAGMA wal_checkpoint(TRUNCATE);")
		return err
	})
}
