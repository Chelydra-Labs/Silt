package spellcheck

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
)

// dictMigratedSentinel is written to the new cache directory only after a full
// successful copy — the commit point so a crash mid-copy re-runs cleanly.
const dictMigratedSentinel = ".migrated"

// migrateCacheOnce ensures the one-time dictionary-cache relocation runs at
// most once per process (kicked off at app startup, or lazily on the first
// CacheRoot() call as a fallback).
var migrateCacheOnce sync.Once

// migrateDictionaryCacheOnce performs a one-time relocation of the dictionary
// cache from <UserConfigDir>/silt/dictionaries to <UserCacheDir>/silt/dictionaries
// (dictionaries are downloadable/regenerable, so the OS cache dir is the right
// home). Best-effort: a copy or remove failure is logged and leaves the legacy
// cache in place — the cache re-downloads on demand. Guarded by a process-wide
// Once so it runs at most once regardless of how many times CacheRoot is called.
func migrateDictionaryCacheOnce() {
	migrateCacheOnce.Do(func() {
		cfgDir, err := os.UserConfigDir()
		if err != nil {
			log.Printf("spellcheck: dictionary cache migration skipped: %v", err)
			return
		}
		cacheDir, err := os.UserCacheDir()
		if err != nil {
			log.Printf("spellcheck: dictionary cache migration skipped: %v", err)
			return
		}
		migrateDictionaryCacheDirs(
			filepath.Join(cfgDir, "silt", "dictionaries"),
			filepath.Join(cacheDir, "silt", "dictionaries"),
		)
	})
}

// MigrateDictionaryCache triggers the one-time dictionary-cache relocation. It
// is safe to call repeatedly (sync.Once-guarded). Intended to be kicked off at
// app startup so the copy does not block the first user-facing spellcheck IPC;
// CacheRoot also calls it lazily as a fallback.
func MigrateDictionaryCache() {
	migrateDictionaryCacheOnce()
}

// migrateDictionaryCacheDirs copies the legacy dictionary cache at old into new
// and removes old on success. Crash-safe and idempotent: a `.migrated` sentinel
// in new is the commit point — its presence means a prior copy completed. If
// the sentinel is absent but old exists, the copy re-runs (copyTree overwrites
// per-file, so a half-copied new from a prior crash is completed, not reset).
// If old is absent, nothing is migrated (fresh install). A copy failure aborts
// and leaves old intact for safety; the cache re-downloads on demand.
func migrateDictionaryCacheDirs(old, new string) {
	sentinel := filepath.Join(new, dictMigratedSentinel)
	if _, err := os.Stat(sentinel); err == nil {
		return // a prior migration completed
	}
	entries, err := os.ReadDir(old)
	if err != nil {
		return // old absent (fresh install) or unreadable: nothing to migrate
	}
	if err := os.MkdirAll(new, 0o755); err != nil {
		log.Printf("spellcheck: dictionary cache migration: mkdir %s: %v", new, err)
		return
	}
	for _, e := range entries {
		if err := copyTree(filepath.Join(old, e.Name()), filepath.Join(new, e.Name())); err != nil {
			log.Printf("spellcheck: dictionary cache migration aborted at %s: %v (legacy cache left in place)", e.Name(), err)
			return
		}
	}
	// Commit point: write the sentinel only after the full copy succeeds, so a
	// crash mid-copy re-runs cleanly on the next launch.
	if err := os.WriteFile(sentinel, []byte("migrated\n"), 0o644); err != nil {
		log.Printf("spellcheck: dictionary cache migration: could not write marker: %v", err)
		return
	}
	if err := os.RemoveAll(old); err != nil {
		log.Printf("spellcheck: dictionary cache migration: could not remove legacy %s: %v", old, err)
	}
}

// copyTree recursively copies the file or directory tree at src into dst.
func copyTree(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		return copyFile(path, target)
	})
}

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
	if err := os.MkdirAll(filepath.Dir(dst), info.Mode()); err != nil {
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
	// Durably flush before close so a crash does not leave a half-written pack.
	if err := out.Sync(); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
