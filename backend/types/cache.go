package types

import (
	"os"
	"sync"
	"time"
)

// cacheTTL bounds how long a cached ListTypes result is considered fresh even
// when the dir-mtime check has not fired. The mtime check is the primary
// freshness gate; this TTL is defense-in-depth against filesystems with coarse
// mtime resolution. Mirrors templates.cacheTTL.
const cacheTTL = 5 * time.Minute

// typeCache is a process-local, dir-mtime-aware cache of the full ListTypes
// result per types directory. The type manager calls ListTypes whenever it
// needs the schema set (assigning a type, validating a property, rendering the
// dashboard), and re-reading + re-parsing every .yaml file each time is
// wasteful. Types are few, so the granularity is the whole directory: any
// add/modify/delete bumps the directory mtime and invalidates the entry.
//
// In-process only. InvalidateTypesCache is called after SaveType/DeleteType
// (so the next read reflects the new file) and by the TypeWatcher (so an
// external edit is picked up). We deliberately do not hook fsnotify inside the
// cache — the dedicated TypeWatcher (watcher.go) owns observation and calls
// InvalidateTypesCache on change. Mirrors templates.templateCache.
type typeCache struct {
	mu      sync.RWMutex
	entries map[string]typeCacheEntry
}

type typeCacheEntry struct {
	result     *ListTypesResult
	loadedAt   time.Time
	dirModTime time.Time // on-disk dir modtime at load; mismatch → reload
}

var globalTypeCache = &typeCache{
	entries: map[string]typeCacheEntry{},
}

// CachedListTypes returns the parsed type set for typesDir, using the cache when
// the directory is unchanged. A missing directory bypasses the cache and
// returns the empty result (fresh vault). A genuine I/O error propagates.
// Mirrors templates.CachedGetTemplate's freshness gate, adapted to whole-dir
// granularity.
//
// The returned *ListTypesResult is shared across callers — treat it as
// read-only (see the ListTypesResult immutability contract).
func CachedListTypes(typesDir string) (*ListTypesResult, error) {
	if typesDir == "" {
		return ListTypes(typesDir)
	}
	info, statErr := os.Stat(typesDir)
	if statErr != nil {
		// Missing dir: no cache, just the empty result.
		return ListTypes(typesDir)
	}
	dirMod := info.ModTime()
	now := time.Now()

	globalTypeCache.mu.RLock()
	entry, ok := globalTypeCache.entries[typesDir]
	globalTypeCache.mu.RUnlock()
	if ok && entry.result != nil && entry.dirModTime.Equal(dirMod) && now.Sub(entry.loadedAt) < cacheTTL {
		return entry.result, nil
	}

	res, err := ListTypes(typesDir)
	if err != nil {
		return nil, err
	}
	globalTypeCache.mu.Lock()
	globalTypeCache.entries[typesDir] = typeCacheEntry{
		result:     res,
		loadedAt:   now,
		dirModTime: dirMod,
	}
	globalTypeCache.mu.Unlock()
	return res, nil
}

// InvalidateTypesCache drops all cached entries. Called by the App after
// SaveType/DeleteType (so the next read re-reads the new file) and by the
// TypeWatcher (so an external edit is picked up). Mirrors
// templates.InvalidateTemplateCache.
func InvalidateTypesCache() {
	globalTypeCache.mu.Lock()
	defer globalTypeCache.mu.Unlock()
	globalTypeCache.entries = map[string]typeCacheEntry{}
}

// ResetCacheForTests clears the entire cache. Test-only (exported so test files
// in the same package can call it from setup); not used by production code.
// Mirrors templates.ResetCacheForTests.
func ResetCacheForTests() {
	globalTypeCache.mu.Lock()
	defer globalTypeCache.mu.Unlock()
	globalTypeCache.entries = map[string]typeCacheEntry{}
}
