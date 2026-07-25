package spellcheck

import (
	"os"
	"path/filepath"
	"testing"
)

func writeDictFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestMigrateDictionaryCacheDirs_HappyPath(t *testing.T) {
	old := t.TempDir()
	new := t.TempDir()
	writeDictFile(t, filepath.Join(old, "languages", "en-US", "index.aff"), "AFF")
	writeDictFile(t, filepath.Join(old, "languages", "en-US", "index.dic"), "DIC")
	writeDictFile(t, filepath.Join(old, "manifest.json"), "{}")

	migrateDictionaryCacheDirs(old, new)

	if _, err := os.Stat(filepath.Join(new, "languages", "en-US", "index.aff")); err != nil {
		t.Errorf("new missing index.aff: %v", err)
	}
	if _, err := os.Stat(filepath.Join(new, "languages", "en-US", "index.dic")); err != nil {
		t.Errorf("new missing index.dic: %v", err)
	}
	if _, err := os.Stat(filepath.Join(new, "manifest.json")); err != nil {
		t.Errorf("new missing manifest.json: %v", err)
	}
	// The commit sentinel is written (the crash-safety commit point).
	if _, err := os.Stat(filepath.Join(new, dictMigratedSentinel)); err != nil {
		t.Error("commit sentinel should be written after a successful migration")
	}
	// Old is removed after a successful migration.
	if _, err := os.Stat(old); err == nil {
		t.Error("legacy cache should be removed after migration")
	}
}

func TestMigrateDictionaryCacheDirs_AlreadyMigratedIsNoOp(t *testing.T) {
	old := t.TempDir()
	new := t.TempDir()
	writeDictFile(t, filepath.Join(old, "domains", "legacy", "words.txt"), "legacy")
	writeDictFile(t, filepath.Join(new, "domains", "existing", "words.txt"), "existing")
	// Mark new as already migrated (sentinel present).
	if err := os.WriteFile(filepath.Join(new, dictMigratedSentinel), []byte("migrated\n"), 0o644); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}

	migrateDictionaryCacheDirs(old, new)

	// Sentinel short-circuits: new is untouched and the legacy cache is left.
	if _, err := os.Stat(filepath.Join(new, "domains", "existing", "words.txt")); err != nil {
		t.Error("existing new file should remain (migration is a no-op)")
	}
	if _, err := os.Stat(filepath.Join(old, "domains", "legacy", "words.txt")); err != nil {
		t.Error("legacy cache should remain when new is already migrated")
	}
}

func TestMigrateDictionaryCacheDirs_EmptyLegacyDir(t *testing.T) {
	// An empty legacy dir has nothing to copy, but the migration still commits
	// (writes the sentinel) and removes the empty legacy location.
	old := t.TempDir()
	new := t.TempDir()
	migrateDictionaryCacheDirs(old, new)
	if _, err := os.Stat(filepath.Join(new, dictMigratedSentinel)); err != nil {
		t.Error("sentinel should be written even for an empty legacy cache")
	}
	if _, err := os.Stat(old); err == nil {
		t.Error("empty legacy dir should be removed")
	}
}

func TestMigrateDictionaryCacheDirs_OldAbsent(t *testing.T) {
	old := filepath.Join(t.TempDir(), "does-not-exist")
	new := t.TempDir()
	// Must not panic or create anything when the legacy cache is absent.
	migrateDictionaryCacheDirs(old, new)
	if got := countFiles(new); got != 0 {
		t.Errorf("new should be empty when old is absent, got %d files", got)
	}
}

// countFiles walks dir and returns the number of regular files.
func countFiles(dir string) int {
	n := 0
	_ = filepath.Walk(dir, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			n++
		}
		return nil
	})
	return n
}
