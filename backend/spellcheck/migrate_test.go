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
	// Old is removed after a successful migration.
	if _, err := os.Stat(old); err == nil {
		t.Error("legacy cache should be removed after migration")
	}
}

func TestMigrateDictionaryCacheDirs_NewPopulatedIsNoOp(t *testing.T) {
	old := t.TempDir()
	new := t.TempDir()
	writeDictFile(t, filepath.Join(old, "domains", "legacy", "words.txt"), "legacy")
	writeDictFile(t, filepath.Join(new, "domains", "existing", "words.txt"), "existing")

	migrateDictionaryCacheDirs(old, new)

	// New is untouched (it already held content).
	entries, _ := os.ReadDir(new)
	if got := countFiles(new); got != 1 {
		t.Errorf("new should be unchanged (1 file), got %d", got)
	}
	// Old is left in place (migration skipped).
	if _, err := os.Stat(filepath.Join(old, "domains", "legacy", "words.txt")); err != nil {
		t.Error("legacy cache should remain when new is already populated")
	}
	_ = entries
}

func TestMigrateDictionaryCacheDirs_FreshInstallEmptyOld(t *testing.T) {
	old := t.TempDir() // exists but empty (no legacy cache)
	new := t.TempDir()
	migrateDictionaryCacheDirs(old, new)
	// Nothing to migrate → new stays empty; old stays empty.
	if got := countFiles(new); got != 0 {
		t.Errorf("new should be empty on fresh install, got %d files", got)
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
