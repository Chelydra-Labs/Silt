package db

// Files-table mtime/size cache for incremental re-indexing. Relocated from
// indexing.go so the indexers hold only block-projection logic; the files cache
// is a distinct concern (the WAL-relocated working-memory tier, ARCHITECTURE.md
// §0 rule 4). All methods stay on *DatabaseManager.

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// FileMtime returns the last-seen mtime (Unix nanoseconds) the index recorded
// for the file at path, or (0, sql.ErrNoRows) when the file has no row (never
// indexed, or the row was pruned). Used by GetPageCoreMetadata to surface a
// READ-ONLY `modified` value that stays fresh across block-only writes — a
// task-status edit bumps the file mtime and MarkFileIndexed refreshes this
// cache, so the panel reads the new value without re-parsing the frontmatter.
func (dm *DatabaseManager) FileMtime(path string) (int64, error) {
	db, release, err := dm.handle()
	if err != nil {
		return 0, ErrDBClosed
	}
	defer release()
	var mt int64
	err = db.QueryRow("SELECT mtime FROM files WHERE path = ?", path).Scan(&mt)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("failed to query files table mtime: %w", err)
	}
	return mt, nil
}

// IsFileUnchanged reports whether the file at `path` was previously indexed
// with the exact same mtime (Unix nanoseconds) and size. A warm restart uses
// this to skip re-parsing files the user has not touched since the last index.
func (dm *DatabaseManager) IsFileUnchanged(path string, mtime, size int64) (bool, error) {
	db, release, err := dm.handle()
	if err != nil {
		return false, ErrDBClosed
	}
	defer release()
	var fmtime, fsize int64
	err = db.QueryRow("SELECT mtime, size FROM files WHERE path = ?", path).Scan(&fmtime, &fsize)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to query files table: %w", err)
	}
	return fmtime == mtime && fsize == size, nil
}

// MarkFileIndexed records that the file at `path` was fully indexed with the
// given mtime/size. If tx is non-nil the upsert joins the caller's transaction
// (used by the bulk startup reindex so all per-file rows commit atomically);
// otherwise it runs against the shared connection.
func (dm *DatabaseManager) MarkFileIndexed(tx *sql.Tx, path string, mtime, size int64) error {
	now := time.Now().UnixNano()
	const q = "INSERT INTO files (path, mtime, size, indexed_at) VALUES (?, ?, ?, ?) " +
		"ON CONFLICT(path) DO UPDATE SET mtime=excluded.mtime, size=excluded.size, indexed_at=excluded.indexed_at"
	// Same lease rule as ClearFileBlocks: tx path must not re-enter handle().
	if tx != nil {
		_, err := tx.Exec(q, path, mtime, size, now)
		return err
	}
	db, release, err := dm.handle()
	if err != nil {
		return ErrDBClosed
	}
	defer release()
	_, err = db.Exec(q, path, mtime, size, now)
	return err
}

// FileIndexStat is one path's mtime/size for MarkFilesIndexed.
type FileIndexStat struct {
	Path  string
	MTime int64 // Unix nanoseconds
	Size  int64
}

// MarkFilesIndexed upserts many files-table rows in one transaction under a
// single read lease so App IPC does not call SQLDB().Begin across vault
// teardown. Empty input is a no-op. Nested MarkFileIndexed uses the tx path
// (must not re-enter handle).
func (dm *DatabaseManager) MarkFilesIndexed(files []FileIndexStat) error {
	if len(files) == 0 {
		return nil
	}
	db, release, err := dm.handle()
	if err != nil {
		return ErrDBClosed
	}
	defer release()
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin files-tx: %w", err)
	}
	defer tx.Rollback()
	for _, f := range files {
		if f.Path == "" {
			continue
		}
		if err := dm.MarkFileIndexed(tx, f.Path, f.MTime, f.Size); err != nil {
			return fmt.Errorf("MarkFileIndexed(%s): %w", f.Path, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit files-tx: %w", err)
	}
	return nil
}

// PruneStaleFiles deletes `files` rows for paths that are no longer present on
// disk (the file was deleted, moved, or renamed). `seenPaths` is the complete
// set of file paths the latest vault scan observed. Returns the pruned paths so
// callers can surface them as one-time init warnings (a renamed file shows up
// as "pruned old path + indexed new path").
func (dm *DatabaseManager) PruneStaleFiles(seenPaths []string) ([]string, error) {
	db, release, err := dm.handle()
	if err != nil {
		return nil, ErrDBClosed
	}
	defer release()
	// Build the parameter list for the "NOT IN (...)" clause. A single
	// round-trip DELETE keeps this cheap even for thousands of files.
	if len(seenPaths) == 0 {
		// No files on disk at all: drop every recorded row.
		_, err = db.Exec("DELETE FROM files")
		return nil, err
	}
	placeholders := make([]string, len(seenPaths))
	args := make([]interface{}, len(seenPaths))
	for i, p := range seenPaths {
		placeholders[i] = "?"
		args[i] = p
	}

	// Collect the about-to-be-pruned paths first so we can report them.
	rows, err := db.Query(
		"SELECT path FROM files WHERE path NOT IN ("+strings.Join(placeholders, ",")+")", args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query stale files: %w", err)
	}
	var pruned []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			rows.Close()
			return nil, err
		}
		pruned = append(pruned, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed iterating stale files: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	if len(pruned) > 0 {
		if _, err := db.Exec(
			"DELETE FROM files WHERE path NOT IN ("+strings.Join(placeholders, ",")+")", args...); err != nil {
			return nil, fmt.Errorf("failed to prune stale files: %w", err)
		}
	}
	return pruned, nil
}

// ForgetFile deletes the files-table row for a single path. Called by the
// watcher when a file is removed or renamed so the next startup scan does not
// treat the path as "unchanged" and skip re-indexing the new occupant.
func (dm *DatabaseManager) ForgetFile(path string) error {
	db, release, err := dm.handle()
	if err != nil {
		return ErrDBClosed
	}
	defer release()
	_, err = db.Exec("DELETE FROM files WHERE path = ?", path)
	return err
}

// KnownFiles returns the full path→FileStat map currently recorded in the
// index. Used for diagnostics (e.g. surfacing how many files are tracked).
func (dm *DatabaseManager) KnownFiles() (map[string]FileStat, error) {
	db, release, err := dm.handle()
	if err != nil {
		return nil, ErrDBClosed
	}
	defer release()
	rows, err := db.Query("SELECT path, mtime, size, indexed_at FROM files")
	if err != nil {
		return nil, fmt.Errorf("failed to query known files: %w", err)
	}
	defer rows.Close()
	out := make(map[string]FileStat)
	for rows.Next() {
		var path string
		var fs FileStat
		if err := rows.Scan(&path, &fs.MTime, &fs.Size, &fs.IndexedAt); err != nil {
			return nil, err
		}
		out[path] = fs
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed iterating known files: %w", err)
	}
	return out, nil
}
