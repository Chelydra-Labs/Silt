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
