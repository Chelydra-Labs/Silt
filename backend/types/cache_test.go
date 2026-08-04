package types

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCachedListTypes_PopulatesAndServes(t *testing.T) {
	ResetCacheForTests()
	dir := t.TempDir()
	writeTypeFile(t, dir, "book.yaml", bookYAML)

	res, err := CachedListTypes(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Types) != 1 {
		t.Fatalf("expected 1 type, got %d", len(res.Types))
	}

	// White-box: the cache should now hold an entry for this dir.
	globalTypeCache.mu.RLock()
	entry, ok := globalTypeCache.entries[dir]
	globalTypeCache.mu.RUnlock()
	if !ok {
		t.Fatal("expected cache entry after CachedListTypes")
	}
	if entry.result == nil || len(entry.result.Types) != 1 {
		t.Errorf("cached entry has wrong content: %+v", entry.result)
	}

	// A second call serves from cache but returns a deep copy.
	res2, err := CachedListTypes(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(res2.Types) != 1 || res2.Types[0].ID != "book" {
		t.Errorf("second call returned %v", res2.Types)
	}
	// Mutation of the returned result must not corrupt the cache.
	res2.Types[0].Name = "MUTATED"
	res3, err := CachedListTypes(dir)
	if err != nil {
		t.Fatal(err)
	}
	if res3.Types[0].Name == "MUTATED" {
		t.Fatal("caller mutation leaked into the shared cache")
	}
}

func TestCachedListTypes_MissingDir(t *testing.T) {
	ResetCacheForTests()
	dir := filepath.Join(t.TempDir(), "missing")
	res, err := CachedListTypes(dir)
	if err != nil {
		t.Fatalf("missing dir should not error: %v", err)
	}
	if len(res.Types) != 0 {
		t.Errorf("expected empty result for missing dir, got %d types", len(res.Types))
	}
}

func TestInvalidateTypesCache(t *testing.T) {
	ResetCacheForTests()
	dir := t.TempDir()
	writeTypeFile(t, dir, "book.yaml", bookYAML)
	if _, err := CachedListTypes(dir); err != nil {
		t.Fatal(err)
	}
	globalTypeCache.mu.RLock()
	_, ok := globalTypeCache.entries[dir]
	globalTypeCache.mu.RUnlock()
	if !ok {
		t.Fatal("expected entry before invalidate")
	}
	InvalidateTypesCache()
	globalTypeCache.mu.RLock()
	l := len(globalTypeCache.entries)
	globalTypeCache.mu.RUnlock()
	if l != 0 {
		t.Errorf("expected empty cache after invalidate, got %d entries", l)
	}
}

func TestResetCacheForTests(t *testing.T) {
	ResetCacheForTests()
	dir := t.TempDir()
	writeTypeFile(t, dir, "book.yaml", bookYAML)
	if _, err := CachedListTypes(dir); err != nil {
		t.Fatal(err)
	}
	ResetCacheForTests()
	globalTypeCache.mu.RLock()
	l := len(globalTypeCache.entries)
	globalTypeCache.mu.RUnlock()
	if l != 0 {
		t.Errorf("ResetCacheForTests left %d entries", l)
	}
}

func TestCachedListTypes_InPlaceEditBumpsContentStamp(t *testing.T) {
	// In-place content overwrite can bump file mtime without the parent dir
	// mtime changing; the content stamp must still force a reload.
	ResetCacheForTests()
	dir := t.TempDir()
	path := filepath.Join(dir, "book.yaml")
	writeTypeFile(t, dir, "book.yaml", bookYAML)
	// Pin an old mtime so the subsequent rewrite is guaranteed newer even on
	// coarse-resolution filesystems (no sleep).
	old := time.Now().Add(-2 * time.Second)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}

	res1, err := CachedListTypes(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(res1.Types) != 1 || res1.Types[0].Name != "Book" {
		t.Fatalf("first load = %+v", res1.Types)
	}

	updated := "name: Novel\nproperties:\n  - name: title\n    type: text\n"
	if err := os.WriteFile(path, []byte(updated), 0o600); err != nil {
		t.Fatal(err)
	}
	newer := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(path, newer, newer); err != nil {
		t.Fatal(err)
	}

	res2, err := CachedListTypes(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(res2.Types) != 1 || res2.Types[0].Name != "Novel" {
		t.Fatalf("after in-place edit expected Novel, got %+v", res2.Types)
	}
}
