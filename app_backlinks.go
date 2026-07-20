package main

import (
	"fmt"

	"silt/backend/db"
)

// GetBacklinks returns every inbound reference to the given page across three
// legs: [[…]] page-links, ((uuid)) block-refs, and {{embed:uuid}} embeds.
// Source-aware: the notebook's source is resolved server-side. Uses the
// indexed page_links reverse index (no full-table scan) for page-link lookups,
// and parameterized LIKE for block-refs/embeds.
//
// Constraint: block-ref/embed legs use raw LIKE %token% scans proportional
// to total block count (no FTS index on raw_content tokens).
func (a *App) GetBacklinks(notebook, section, page string) ([]db.Backlink, error) {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.db == nil {
		return nil, fmt.Errorf("vault database not loaded")
	}
	a.wg.Add(1)
	defer a.wg.Done()

	source := a.resolveSourceByName(notebook)
	var out []db.Backlink
	err := a.coordinator.WithDBReadResult(func() error {
		got, err := a.db.GetBacklinks(source, notebook, section, page)
		if err != nil {
			return err
		}
		out = got
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// GetBacklinksPaged returns a cursor-paged envelope of inbound references.
// Cursor is the opaque token from a previous call's Cursor field (empty for
// the first page). Limit is clamped to [1, db.BacklinksMaxLimit]; 0 uses
// db.BacklinksDefaultLimit. Results are deterministically ordered by
// (source, source_notebook, source_section, source_page, kind, source_block_id).
//
// Remaining constraint: block-ref/embed legs still use parameterized LIKE scans
// (full-table proportional to total block count). Pagination is a post-collection
// keyset cursor over the sorted/deduped result set — no new storage tier.
func (a *App) GetBacklinksPaged(notebook, section, page, cursor string, limit int) (db.BacklinksResult, error) {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.db == nil {
		return db.BacklinksResult{}, fmt.Errorf("vault database not loaded")
	}
	a.wg.Add(1)
	defer a.wg.Done()

	source := a.resolveSourceByName(notebook)
	var res db.BacklinksResult
	err := a.coordinator.WithDBReadResult(func() error {
		got, err := a.db.GetBacklinksPaged(source, notebook, section, page, cursor, limit)
		if err != nil {
			return err
		}
		res = got
		return nil
	})
	if err != nil {
		return db.BacklinksResult{}, err
	}
	return res, nil
}
