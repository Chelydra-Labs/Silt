package main

import (
	"fmt"
	"sort"
	"strings"

	"silt/backend/config"
	"silt/backend/db"
	"silt/backend/parser"
)

// QueryTagHierarchy returns the hierarchical tag tree for the Tags Explorer.
func (a *App) QueryTagHierarchy() ([]parser.TagNode, error) {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.db == nil {
		return nil, fmt.Errorf("vault database not loaded")
	}
	a.wg.Add(1)
	defer a.wg.Done()
	var res []parser.TagNode
	var err error
	a.coordinator.WithDBRead(func() { res, err = a.db.QueryTagHierarchy() })
	return res, err
}

// QueryBlocksByTag returns blocks tagged at or beneath tagPath (prefix match).
func (a *App) QueryBlocksByTag(tagPath string) ([]parser.TaskResult, error) {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.db == nil {
		return nil, fmt.Errorf("vault database not loaded")
	}
	tagPath = strings.TrimSpace(strings.TrimPrefix(tagPath, "#"))
	if tagPath == "" || !config.IsValidTagPath(tagPath) {
		return []parser.TaskResult{}, nil
	}
	a.wg.Add(1)
	defer a.wg.Done()
	var res []parser.TaskResult
	var err error
	a.coordinator.WithDBRead(func() { res, err = a.db.QueryBlocksByTag(tagPath) })
	return res, err
}

// SearchBlocks fuzzy searches blocks and headings matching the query. Returns
// the first page (offset 0, limit 50) of FTS5-ranked results for backwards
// compatibility with the original binding; the Svelte search modal that needs
// pagination/snippets calls SearchBlocksPaged instead.
func (a *App) SearchBlocks(query string) ([]parser.TaskResult, error) {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.db == nil {
		return nil, fmt.Errorf("vault database not loaded")
	}

	a.wg.Add(1)
	defer a.wg.Done()

	var res []parser.TaskResult
	var err error
	a.coordinator.WithDBRead(func() {
		res, err = a.db.SearchBlocks(query)
	})

	return res, err
}

// SearchBlocksPaged runs the FTS5 search and returns a ranked, paginated
// envelope with highlighted snippets, the total match count, and a HasMore
// flag. offset/limit control the page (defaults applied by the caller).
// SearchBlocksPaged runs the FTS5 global search with optional filters (#186
// global enhancements: notebook/section/tag/type/sort/scope). The frontend
// SearchModal drives this; an empty SearchFilters reproduces the original
// unfiltered behavior (whole vault + linked notebooks, bm25 relevance).
func (a *App) SearchBlocksPaged(query string, offset, limit int, filters db.SearchFilters) (parser.SearchResult, error) {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.db == nil {
		return parser.SearchResult{}, fmt.Errorf("vault database not loaded")
	}

	a.wg.Add(1)
	defer a.wg.Done()

	var res parser.SearchResult
	var err error
	a.coordinator.WithDBRead(func() {
		res, err = a.db.SearchBlocksPaged(query, offset, limit, filters)
	})

	return res, err
}

// searchPagesMinLen is the minimum number of non-space characters a query
// must contain before SearchPages will enumerate the page catalog. Shorter
// queries (including empty) return immediately with no results so that
// rapid typeahead keystrokes don't thrash the DB.
const searchPagesMinLen = 2

// searchPagesMax is the hard cap on results SearchPages returns. The caller
// may request fewer but never more; the server always ranks and truncates
// at this bound so the typeahead picker stays responsive.
const searchPagesMax = 50

// SearchPages returns pages whose notebook/section/page path matches the
// query using a ranked contract:
//
//	Rank 0 (best):   exact page-name match (case-insensitive)
//	Rank 1:          page-name prefix match
//	Rank 2:          full display-path prefix match
//	Rank 3:          substring match anywhere in the display path
//
// Within the same rank tier results are ordered alphabetically by
// (notebook, section, page) for deterministic output. The returned slice is
// bounded to min(limit, searchPagesMax). Queries with fewer than
// searchPagesMinLen non-space characters return no results immediately
// (no DB round-trip).
func (a *App) SearchPages(query string, limit int) ([]parser.PageSummary, error) {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.db == nil {
		return nil, fmt.Errorf("vault database not loaded")
	}

	a.wg.Add(1)
	defer a.wg.Done()

	if limit <= 0 || limit > searchPagesMax {
		limit = searchPagesMax
	}

	// Reject queries that are too short to be meaningful.
	if nonSpaceLen(query) < searchPagesMinLen {
		return []parser.PageSummary{}, nil
	}

	var pages []db.PageLoc
	var err error
	a.coordinator.WithDBRead(func() { pages, err = a.db.ListDistinctPages() })
	if err != nil {
		return nil, err
	}

	q := strings.ToLower(query)

	// Score and collect matching pages.
	type scored struct {
		loc  db.PageLoc
		rank int
		path string // lowercase display path for matching
	}
	var hits []scored
	for _, loc := range pages {
		path := displayPath(loc)
		lp := strings.ToLower(path)
		page := strings.ToLower(loc.Page)

		var rank int = -1
		switch {
		case page == q:
			rank = 0 // exact page-name match
		case strings.HasPrefix(page, q):
			rank = 1 // page-name prefix
		case strings.HasPrefix(lp, q):
			rank = 2 // full display-path prefix
		case strings.Contains(lp, q):
			rank = 3 // substring anywhere in path
		}
		if rank < 0 {
			continue
		}
		hits = append(hits, scored{loc: loc, rank: rank, path: path})
	}

	// Sort by rank tier ascending, then alphabetical (notebook, section, page).
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].rank != hits[j].rank {
			return hits[i].rank < hits[j].rank
		}
		ai, aj := hits[i].loc, hits[j].loc
		if ai.Notebook != aj.Notebook {
			return ai.Notebook < aj.Notebook
		}
		if ai.Section != aj.Section {
			return ai.Section < aj.Section
		}
		return ai.Page < aj.Page
	})

	// Truncate to limit.
	if len(hits) > limit {
		hits = hits[:limit]
	}
	out := make([]parser.PageSummary, len(hits))
	for i, h := range hits {
		out[i] = parser.PageSummary{
			Source:   h.loc.Source,
			Notebook: h.loc.Notebook,
			Section:  h.loc.Section,
			Page:     h.loc.Page,
		}
	}
	return out, nil
}

// displayPath builds the ShortestUniquePath convention string:
// "nb/sec/page" or "nb/page" when section is empty.
func displayPath(loc db.PageLoc) string {
	if loc.Section != "" {
		return loc.Notebook + "/" + loc.Section + "/" + loc.Page
	}
	return loc.Notebook + "/" + loc.Page
}

// nonSpaceLen returns the number of non-space Unicode characters in s.
func nonSpaceLen(s string) int {
	n := 0
	for _, r := range s {
		if r != ' ' && r != '\t' && r != '\n' && r != '\r' {
			n++
		}
	}
	return n
}
