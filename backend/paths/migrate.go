package paths

import (
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// migrateTmpSuffix is appended to the in-progress copy of the index during a
// one-time relocation, so a crash leaves a *.migrating file rather than a
// half-written final index. The idempotency gate keys off the final name.
const migrateTmpSuffix = ".migrating"

// LegacyIndexPath returns the pre-relocation in-vault index location, used by
// the one-time migration to find and move a legacy index out of the vault.
func LegacyIndexPath(vaultPath string) string {
	return filepath.Join(vaultPath, ".system", "index.sqlite")
}

// ResolveAndMigrateIndexPath resolves the relocated index path for a vault and
// performs a one-time copy of the legacy in-vault index (with its -wal/-shm
// sidecars) into the per-user local DataDir, preserving warm-start
// performance. See migrateIndex for the full contract.
//
// Returns the resolved new path, soft warnings, and a hard error only when the
// local data dir cannot be resolved/created (a genuinely unusable environment).
func ResolveAndMigrateIndexPath(vaultPath string) (newPath string, warnings []string, err error) {
	newPath, err = LocalIndexPath(vaultPath)
	if err != nil {
		return "", nil, err
	}
	warnings = migrateIndex(LegacyIndexPath(vaultPath), newPath)
	return newPath, warnings, nil
}

// migrateIndex copies a legacy SQLite index (+ sidecars) from legacy into the
// per-user local location newPath. Idempotent: if newPath already exists,
// migration is a no-op (and a stale temp from a prior crashed copy is cleared).
// If legacy is absent, nothing is migrated (fresh install). On any copy/verify
// failure (source locked or corrupt) the copy is skipped and a warning is
// returned — the index rebuilds from markdown on first open (the core index is
// reproducible working memory). The legacy copy is removed only after the new
// copy is verified to open, so a crash mid-migration never loses data.
func migrateIndex(legacy, newPath string) []string {
	// Idempotency gate: a present new index means migration already completed.
	if fileExists(newPath) {
		removeQuiet(newPath+migrateTmpSuffix, newPath+migrateTmpSuffix+"-wal", newPath+migrateTmpSuffix+"-shm")
		return nil
	}
	if !fileExists(legacy) {
		return nil // fresh install; nothing to migrate.
	}

	tmpMain := newPath + migrateTmpSuffix
	if w := copySQLiteSet(legacy, tmpMain); w != "" {
		removeQuiet(tmpMain, tmpMain+"-wal", tmpMain+"-shm")
		return []string{w}
	}

	// Verify the temp copy opens and passes integrity_check before committing.
	if vErr := indexOpens(tmpMain); vErr != nil {
		removeQuiet(tmpMain, tmpMain+"-wal", tmpMain+"-shm")
		return []string{fmt.Sprintf("index migration: the copied vault index could not be verified (%v); rebuilding it locally instead", vErr)}
	}

	// Commit: rename temp set to final names. Per-file atomic on the same FS;
	// a crash in this microsecond window can lose uncheckpointed WAL frames,
	// which a re-index rebuilds from markdown on the next launch.
	var warnings []string
	for _, pair := range []struct{ tmp, final string }{
		{tmpMain, newPath},
		{tmpMain + "-wal", newPath + "-wal"},
		{tmpMain + "-shm", newPath + "-shm"},
	} {
		if fileExists(pair.tmp) {
			if rErr := os.Rename(pair.tmp, pair.final); rErr != nil {
				warnings = append(warnings, fmt.Sprintf("index migration: rename to %s: %v", pair.final, rErr))
			}
		}
	}

	// Remove the legacy in-vault index trio. Best-effort: a locked source leaves
	// an orphan that is harmless (and swept by a future cleanup pass).
	for _, p := range []string{legacy, legacy + "-wal", legacy + "-shm"} {
		if fileExists(p) {
			if rErr := os.Remove(p); rErr != nil {
				warnings = append(warnings, fmt.Sprintf("index migration: could not remove legacy %s (%v); it can be deleted manually", p, rErr))
			}
		}
	}
	return warnings
}

// copySQLiteSet copies a main SQLite file and its present -wal/-shm sidecars
// from src to dst (dst for the main, dst+"-wal"/dst+"-shm" for sidecars).
// Returns "" on success or a warning string describing the failure.
func copySQLiteSet(src, dst string) string {
	if err := copyFile(src, dst); err != nil {
		return fmt.Sprintf("index migration: copy %s: %v", src, err)
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if fileExists(src + suffix) {
			if err := copyFile(src+suffix, dst+suffix); err != nil {
				return fmt.Sprintf("index migration: copy %s: %v", src+suffix, err)
			}
		}
	}
	return ""
}

// copyFile copies src to dst preserving the file mode.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// indexOpens opens the SQLite file and confirms it passes integrity_check. Used
// to validate a migrated copy before deleting the legacy in-vault original.
func indexOpens(path string) error {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	var result string
	if err := db.QueryRow("PRAGMA integrity_check;").Scan(&result); err != nil {
		return err
	}
	if !strings.EqualFold(result, "ok") {
		return fmt.Errorf("integrity_check returned %q", result)
	}
	return nil
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func removeQuiet(paths ...string) {
	for _, p := range paths {
		_ = os.Remove(p)
	}
}
