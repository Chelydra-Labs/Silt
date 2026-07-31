package db

import "time"

// unlinkedScanCacheMaxEntries bounds process-local FTS window cache size.
// The backlinks panel typically holds one active title; a small LRU-ish cap
// covers multi-window Scan more without retaining large vault scans.
const unlinkedScanCacheMaxEntries = 16

// unlinkedScanCacheTTL is defense-in-depth against missed invalidation.
// Primary freshness is unlinkedScanCacheGen (bumped on blocks mutation).
const unlinkedScanCacheTTL = 30 * time.Second

// unlinkedScanCalls counts scanUnlinkedCandidateBlocks invocations (tests).
// Not atomic: tests run serially against a single dm.
var unlinkedScanCalls int

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
	// Touch for crude LRU: re-insert moves to "newest" only if we tracked order;
	// with a small map, TTL + gen invalidation is enough. Copy blocks slice header
	// so callers cannot mutate the cached backing array via append.
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
	if dm.unlinkedScanCache == nil {
		dm.unlinkedScanCache = make(map[unlinkedScanCacheKey]unlinkedScanCacheEntry, unlinkedScanCacheMaxEntries)
	}
	// Evict arbitrary entries when over cap (gen/TTL keep correctness).
	for len(dm.unlinkedScanCache) >= unlinkedScanCacheMaxEntries {
		for k := range dm.unlinkedScanCache {
			delete(dm.unlinkedScanCache, k)
			break
		}
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
