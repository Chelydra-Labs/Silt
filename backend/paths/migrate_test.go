package paths

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// writeWALLegacyIndex creates a real WAL-mode index at the legacy in-vault
// location with one committed-but-uncheckpointed row, so a migration must copy
// the main file AND its -wal/-shm sidecars to preserve warm-start data.
func writeWALLegacyIndex(t *testing.T, vaultPath string) {
	t.Helper()
	legacy := LegacyIndexPath(vaultPath)
	if err := os.MkdirAll(filepath.Dir(legacy), 0o700); err != nil {
		t.Fatalf("mkdir legacy .system: %v", err)
	}
	db, err := sql.Open("sqlite", legacy)
	if err != nil {
		t.Fatalf("open legacy: %v", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("PRAGMA journal_mode = WAL;"); err != nil {
		t.Fatalf("set WAL: %v", err)
	}
	// Disable auto-checkpoint so the row stays in the -wal after close, proving
	// the sidecar copy path.
	if _, err := db.Exec("PRAGMA wal_autocheckpoint = 0;"); err != nil {
		t.Fatalf("disable autocheckpoint: %v", err)
	}
	if _, err := db.Exec("CREATE TABLE marker(v TEXT); INSERT INTO marker VALUES('warm-start-payload');"); err != nil {
		t.Fatalf("seed legacy: %v", err)
	}
	if _, err := os.Stat(legacy + "-wal"); err != nil {
		t.Fatalf("legacy -wal not present after WAL write: %v", err)
	}
}

func TestMigrateIndex_HappyPathCopiesSidecars(t *testing.T) {
	t.Setenv("SILT_DATA_DIR", t.TempDir())
	vault := t.TempDir()
	writeWALLegacyIndex(t, vault)
	legacy := LegacyIndexPath(vault)

	newPath, warns, err := ResolveAndMigrateIndexPath(vault)
	if err != nil {
		t.Fatalf("ResolveAndMigrateIndexPath: %v", err)
	}
	if len(warns) != 0 {
		t.Errorf("expected no warnings on happy path, got %v", warns)
	}
	if newPath == legacy {
		t.Error("newPath should differ from the legacy in-vault path")
	}

	// The migrated index exists and carries the warm-start payload — proving
	// the -wal sidecar was copied (the row lives there until checkpoint).
	if _, err := os.Stat(newPath); err != nil {
		t.Fatalf("migrated index missing: %v", err)
	}
	got, err := sql.Open("sqlite", newPath)
	if err != nil {
		t.Fatalf("open migrated index: %v", err)
	}
	defer got.Close()
	got.SetMaxOpenConns(1)
	var v string
	if err := got.QueryRow("SELECT v FROM marker").Scan(&v); err != nil {
		t.Fatalf("read marker from migrated index (sidecar copy failed?): %v", err)
	}
	if v != "warm-start-payload" {
		t.Errorf("marker = %q, want warm-start-payload", v)
	}

	// The legacy in-vault index trio is removed after a verified migration.
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if _, err := os.Stat(legacy + suffix); err == nil {
			t.Errorf("legacy %s should be removed after migration", legacy+suffix)
		}
	}
}

func TestMigrateIndex_IdempotentRerun(t *testing.T) {
	t.Setenv("SILT_DATA_DIR", t.TempDir())
	vault := t.TempDir()
	writeWALLegacyIndex(t, vault)
	legacy := LegacyIndexPath(vault)

	first, warns1, err := ResolveAndMigrateIndexPath(vault)
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	if len(warns1) != 0 {
		t.Errorf("first run warnings: %v", warns1)
	}
	if _, err := os.Stat(legacy); err == nil {
		t.Error("legacy should be removed after the first migration")
	}

	// Second run: the new index already exists, so this is a no-op.
	second, warns2, err := ResolveAndMigrateIndexPath(vault)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if len(warns2) != 0 {
		t.Errorf("second run warnings: %v", warns2)
	}
	if second != first {
		t.Errorf("paths differ across runs: %q vs %q", second, first)
	}
}

func TestMigrateIndex_FreshInstallHasNoLegacy(t *testing.T) {
	t.Setenv("SILT_DATA_DIR", t.TempDir())
	vault := t.TempDir()
	// No legacy index in the vault — fresh install.

	newPath, warns, err := ResolveAndMigrateIndexPath(vault)
	if err != nil {
		t.Fatalf("ResolveAndMigrateIndexPath: %v", err)
	}
	if len(warns) != 0 {
		t.Errorf("expected no warnings on fresh install, got %v", warns)
	}
	// The index file is not created by migration — it is built on first open.
	if _, err := os.Stat(newPath); err == nil {
		t.Error("newPath file should not exist when there is no legacy to migrate")
	}
	// But the parent directory is created by LocalIndexPath.
	if _, err := os.Stat(filepath.Dir(newPath)); err != nil {
		t.Errorf("index dir should be created: %v", err)
	}
}

func TestMigrateIndex_CorruptSourceFallsBack(t *testing.T) {
	t.Setenv("SILT_DATA_DIR", t.TempDir())
	vault := t.TempDir()
	legacy := LegacyIndexPath(vault)
	if err := os.MkdirAll(filepath.Dir(legacy), 0o700); err != nil {
		t.Fatalf("mkdir legacy .system: %v", err)
	}
	// A garbage file that is not a valid SQLite database (also stands in for a
	// locked source — both fail at copy/verify and hit the same rebuild path).
	if err := os.WriteFile(legacy, []byte("not a sqlite database"), 0o644); err != nil {
		t.Fatalf("write corrupt legacy: %v", err)
	}

	newPath, warns, err := ResolveAndMigrateIndexPath(vault)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(warns) == 0 {
		t.Error("expected a fallback warning for a corrupt source")
	}
	// The corrupt source is left in place (verify failed → not deleted).
	if _, err := os.Stat(legacy); err != nil {
		t.Error("legacy should remain when migration cannot verify it")
	}
	// The new index file is not created — it rebuilds from markdown on first
	// open instead.
	if _, err := os.Stat(newPath); err == nil {
		t.Error("newPath should not exist after a failed migration")
	}
}

// TestMigrateIndex_RecoversFromPartialCrash exercises the crash-recovery
// branch: a prior migration crashed after renaming a partial/garbage main file
// but before writing the commit sentinel, so the sentinel is absent while a
// partial newPath and the legacy source both exist. The next run must clear the
// partial newPath and re-copy cleanly from the legacy source — the sentinel's
// whole purpose.
func TestMigrateIndex_RecoversFromPartialCrash(t *testing.T) {
	t.Setenv("SILT_DATA_DIR", t.TempDir())
	vault := t.TempDir()
	writeWALLegacyIndex(t, vault) // valid legacy (legacy + -wal carrying the marker)
	legacy := LegacyIndexPath(vault)

	newPath, err := LocalIndexPath(vault)
	if err != nil {
		t.Fatalf("LocalIndexPath: %v", err)
	}
	// Simulate a crashed prior run: a garbage newPath with no commit sentinel,
	// and the legacy source still present.
	if err := os.WriteFile(newPath, []byte("partial garbage from a crashed rename"), 0o644); err != nil {
		t.Fatalf("seed partial newPath: %v", err)
	}

	_, warnings, err := ResolveAndMigrateIndexPath(vault)
	if err != nil {
		t.Fatalf("ResolveAndMigrateIndexPath: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("expected a clean re-migration, got warnings: %v", warnings)
	}

	// The partial newPath was cleared and replaced by the valid legacy copy.
	got, err := sql.Open("sqlite", newPath)
	if err != nil {
		t.Fatalf("open migrated index: %v", err)
	}
	defer got.Close()
	got.SetMaxOpenConns(1)
	var v string
	if err := got.QueryRow("SELECT v FROM marker").Scan(&v); err != nil {
		t.Fatalf("marker should survive the re-copy from legacy: %v", err)
	}
	if v != "warm-start-payload" {
		t.Errorf("marker = %q, want warm-start-payload", v)
	}
	// The commit sentinel is written (crash recovery completed).
	if _, err := os.Stat(migratedSentinelPath(newPath)); err != nil {
		t.Error("sentinel should be written after crash recovery")
	}
	// The legacy source is removed after the successful re-migration.
	if _, err := os.Stat(legacy); err == nil {
		t.Error("legacy should be removed after crash recovery")
	}
}

// TestMigrateIndex_SentinelSweepsOrphanedLegacy verifies that a second open,
// finding the commit sentinel already present, sweeps a legacy trio left
// behind when a prior migration's sentinel write succeeded but its legacy
// removal failed (a locked source, or a crash between the two). Without the
// sweep the legacy trio would linger in the synced vault forever.
func TestMigrateIndex_SentinelSweepsOrphanedLegacy(t *testing.T) {
	t.Setenv("SILT_DATA_DIR", t.TempDir())
	vault := t.TempDir()
	legacy := LegacyIndexPath(vault)
	writeWALLegacyIndex(t, vault) // a valid legacy trio (legacy + -wal + -shm)
	newPath, err := LocalIndexPath(vault)
	if err != nil {
		t.Fatalf("LocalIndexPath: %v", err)
	}
	// Simulate a completed migration whose legacy removal failed: the sentinel
	// is present but the legacy trio was never deleted.
	if err := os.WriteFile(migratedSentinelPath(newPath), []byte("migrated\n"), 0o644); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}

	_, warnings, err := ResolveAndMigrateIndexPath(vault)
	if err != nil {
		t.Fatalf("ResolveAndMigrateIndexPath: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("expected a clean no-op, got warnings: %v", warnings)
	}
	// The orphaned legacy trio is swept on the sentinel-hit path.
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if _, err := os.Stat(legacy + suffix); err == nil {
			t.Errorf("orphaned legacy %s should be swept when the sentinel is present", legacy+suffix)
		}
	}
}
