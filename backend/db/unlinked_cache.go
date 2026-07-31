package db

import (
	"sync/atomic"
	"time"
)

// unlinkedScanCacheMaxEntries bounds process-local FTS window cache size.
// The backlinks panel typically holds one active title; a small cap covers
// multi-window Scan more without retaining large vault scans.
const unlinkedScanCacheMaxEntries = 16

// unlinkedScanCacheTTL is defense-in-depth against missed invalidation.
// Primary freshness is unlinkedScanCacheGen (bumped on blocks mutation).
const unlinkedScanCacheTTL = 30 * time.Second

// unlinkedScanCalls counts scanUnlinkedCandidateBlocks invocations (tests +
// concurrent production readers via WithDBReadResult).
var unlinkedScanCalls atomic.Uint64

type unlinkedScanCacheKey struct {
	gen                                  uint64
	source, notebook, section, pageTitle string
	scanCursor                           string
}

type unlinkedScanCacheEntry struct {
	blocks    []unlinkedBlock
	truncated bool
	lastRowid int64
	lastID    string
	storedAt  time.Time
}

func (dm *DatabaseManager) invalidateUnlinkedScanCache() {
	dm.unlinkedScanCacheMu.Lock()
	defer dm.unlinkedScanCacheMu.Unlock()
	dm.unlinkedScanCacheGen++
	dm.unlinkedScanCache = nil
	dm.nonASCIILeaves = nil
	dm.nonASCIILeavesOK = false
}

func (dm *DatabaseManager) unlinkedScanCacheGet(key unlinkedScanCacheKey) (unlinkedScanCacheEntry, bool) {
	dm.unlinkedScanCacheMu.Lock()
	defer dm.unlinkedScanCacheMu.Unlock()
	if dm.unlinkedScanCache == nil {
		return unlinkedScanCacheEntry{}, false
	}
	ent, ok := dm.unlinkedScanCache[key]
	if !ok {
		return unlinkedScanCacheEntry{}, false
	}
	if time.Since(ent.storedAt) > unlinkedScanCacheTTL {
		delete(dm.unlinkedScanCache, key)
		return unlinkedScanCacheEntry{}, false
	}
	// Copy blocks so callers cannot mutate the cached backing array via append.
	out := ent
	if len(ent.blocks) > 0 {
		cp := make([]unlinkedBlock, len(ent.blocks))
		copy(cp, ent.blocks)
		out.blocks = cp
	}
	return out, true
}

func (dm *DatabaseManager) unlinkedScanCachePut(key unlinkedScanCacheKey, ent unlinkedScanCacheEntry) {
	dm.unlinkedScanCacheMu.Lock()
	defer dm.unlinkedScanCacheMu.Unlock()
	// Drop puts from a scan that started under a superseded generation so a
	// concurrent reindex cannot re-insert a stale window under an old gen key
	// (orphans) or race with a fresh gen that happens to reuse the same fields.
	if key.gen != dm.unlinkedScanCacheGen {
		return
	}
	if dm.unlinkedScanCache == nil {
		dm.unlinkedScanCache = make(map[unlinkedScanCacheKey]unlinkedScanCacheEntry, unlinkedScanCacheMaxEntries)
	}
	// Evict oldest storedAt when over cap.
	for len(dm.unlinkedScanCache) >= unlinkedScanCacheMaxEntries {
		var oldestKey unlinkedScanCacheKey
		var oldestAt time.Time
		first := true
		for k, v := range dm.unlinkedScanCache {
			if first || v.storedAt.Before(oldestAt) {
				oldestKey = k
				oldestAt = v.storedAt
				first = false
			}
		}
		if first {
			break
		}
		delete(dm.unlinkedScanCache, oldestKey)
	}
	ent.storedAt = time.Now()
	if len(ent.blocks) > 0 {
		cp := make([]unlinkedBlock, len(ent.blocks))
		copy(cp, ent.blocks)
		ent.blocks = cp
	}
	dm.unlinkedScanCache[key] = ent
}

func (dm *DatabaseManager) unlinkedScanCacheKeyNow(source, notebook, section, pageTitle, scanCursor string) unlinkedScanCacheKey {
	dm.unlinkedScanCacheMu.Lock()
	gen := dm.unlinkedScanCacheGen
	dm.unlinkedScanCacheMu.Unlock()
	return unlinkedScanCacheKey{
		gen:        gen,
		source:     source,
		notebook:   notebook,
		section:    section,
		pageTitle:  pageTitle,
		scanCursor: scanCursor,
	}
}
