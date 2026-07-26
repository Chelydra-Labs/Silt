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
// half-written final index.
const migrateTmpSuffix = ".migrating"

// migratedSentinel is an empty-ish marker written to the index directory ONLY
// after the full rename set succeeds. It is the commit point: its presence
// means a migration completed atomically-as-a-set, so the idempotency gate can
// trust it. Without it, a present newPath could be a half-committed set from a
// prior crash and must be re-migrated. (Losing the sentinel is safe: a later
// open re-evaluates and either re-copies from the still-present legacy or, if
// the legacy is gone, opens the existing newPath as a normal warm start.)
const migratedSentinel = ".migrated"

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
// per-user local location newPath. Crash-safe and idempotent: a `.migrated`
// sentinel in the index directory is the commit point — its presence means a
// prior migration completed the full rename set, so the gate trusts it. If the
// sentinel is absent but the legacy index exists, any partial artifacts from a
// prior crashed attempt are cleared and the migration re-runs cleanly. If the
// legacy index is absent, nothing is migrated (fresh install or already built
// locally). On any copy/verify failure (source locked or corrupt) the copy is
// skipped and a warning is returned — the index rebuilds from markdown on first
// open (the core index is reproducible working memory). The legacy copy is
// removed only after the new copy is verified to open and the sentinel is
// written, so a crash mid-migration never loses data and never leaves a
// half-committed set.
func migrateIndex(legacy, newPath string) []string {
	sentinel := migratedSentinelPath(newPath)

	// Committed gate: the sentinel is written only after the full rename set
	// succeeds, so its presence means a complete migration.
	if fileExists(sentinel) {
		// Best-effort sweep of a legacy trio orphaned by a prior failed removal
		// (a sentinel that committed but a legacy delete that failed or crashed
		// between the two). Mirrors the dictionary migration's sentinel-hit
		// sweep; without it the legacy trio would linger in the vault forever
		// since the sentinel short-circuits every future run.
		removeQuiet(legacy, legacy+"-wal", legacy+"-shm")
		removeQuiet(newPath+migrateTmpSuffix, newPath+migrateTmpSuffix+"-wal", newPath+migrateTmpSuffix+"-shm")
		return nil
	}
	if !fileExists(legacy) {
		// Fresh install, or already built locally without a legacy. Nothing to
		// migrate; no sentinel is needed (there was no copy to commit).
		return nil
	}

	// Legacy present, not committed: clear any partial artifacts from a prior
	// crashed attempt (a renamed-but-uncommitted main, sidecars, and temps)
	// before re-copying from the still-present legacy source.
	removeQuiet(newPath, newPath+"-wal", newPath+"-shm",
		newPath+migrateTmpSuffix, newPath+migrateTmpSuffix+"-wal", newPath+migrateTmpSuffix+"-shm")

	tmpMain := newPath + migrateTmpSuffix
	if w := copySQLiteSet(legacy, tmpMain); w != "" {
		removeQuiet(tmpMain, tmpMain+"-wal", tmpMain+"-shm")
		return []string{w}
	}

	// Verify the temp copy opens and passes integrity_check before committing.
	if vErr := indexOpens(tmpMain); vErr != nil {
		removeQuiet(tmpMain, tmpMain+"-wal", tmpMain+"-shm")
		return []string{fmt.Sprintf("index migration: the copied vault index could not be verified (%v); a fresh index will be rebuilt from your notes on the next launch (this may take a moment for a large vault)", vErr)}
	}

	// Commit: rename temp set to final names, then write the sentinel. The
	// sentinel is the commit point — until it exists, a crash triggers a clean
	// re-migration above. Per-file atomic on the same FS.
	var warnings []string
	for _, pair := range []struct{ tmp, final string }{
		{tmpMain, newPath},
		{tmpMain + "-wal", newPath + "-wal"},
		{tmpMain + "-shm", newPath + "-shm"},
	} {
		if fileExists(pair.tmp) {
			if rErr := os.Rename(pair.tmp, pair.final); rErr != nil {
				warnings = append(warnings, fmt.Sprintf("index migration: rename to %s: %v", pair.final, rErr))
				if pair.final == newPath {
					// The main index file did not land — the index is unusable
					// without it. Clean up the temps and do NOT commit the
					// sentinel, so the next launch re-migrates from the
					// still-present legacy (a transparent retry) instead of
					// pinning the user to a forced full reindex.
					removeQuiet(tmpMain, tmpMain+"-wal", tmpMain+"-shm")
					return warnings
				}
			}
		}
	}
	if w := writeSentinel(sentinel); w != "" {
		warnings = append(warnings, w)
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

// migratedSentinelPath returns the path of the completion marker for a migrated
// index (sibling of the index file in its per-vault directory).
func migratedSentinelPath(newPath string) string {
	return filepath.Join(filepath.Dir(newPath), migratedSentinel)
}

// writeSentinel writes the migration completion marker. Returns "" on success
// or a warning string. Not fsynced: a lost sentinel is safe (see migrateIndex).
func writeSentinel(path string) string {
	if err := os.WriteFile(path, []byte("migrated\n"), 0o644); err != nil {
		return fmt.Sprintf("index migration: could not write completion marker %s: %v", path, err)
	}
	return ""
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
	// Durably flush the copied bytes before a rename so a power-loss window
	// cannot leave a renamed file with zero-filled pages. (Directory fsync
	// after rename is not portable on Windows and is omitted; the file-level
	// sync is the material win for a regenerable index.)
	if err := out.Sync(); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// indexOpens opens the SQLite file and confirms it passes integrity_check. Used
// to validate a migrated copy before deleting the legacy in-vault original. It
// opens read-write (no portable read-only URI over modernc without path-
// escaping concerns), which may run WAL recovery/checkpoint on the temp — that
// is benign: the post-recovery bytes are what get renamed to the final path.
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
