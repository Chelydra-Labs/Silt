package db

import (
	"log"

	"silt/backend/parser"
)

// Duplicate block-id detection.
//
// A block id is a global primary key (blocks.id): every ((uuid)) reference
// and {{embed:uuid}} resolves through it, so two pages carrying the same id
// make the index silently ambiguous — links resolve to whichever page
// indexed last, and neither file is touched to say otherwise. These types
// carry one such collision to the user as a structured warning; the files
// themselves are never rewritten (report-only posture).

// DuplicateIDPage names one page (index coordinates) carrying a duplicate
// block id.
type DuplicateIDPage struct {
	Source   string `json:"source"`
	Notebook string `json:"notebook"`
	Section  string `json:"section"`
	Page     string `json:"page"`
}

// DuplicateIDWarning describes one block id carried by more than one page
// (Pages has 2+ entries), twice within one page, or both at once.
type DuplicateIDWarning struct {
	BlockID string            `json:"block_id"`
	Pages   []DuplicateIDPage `json:"pages"`
	// WithinPage is true when at least one carrier page holds the id more
	// than once — a hand-edited or plugin-raced file, possibly on top of a
	// cross-file collision. The pure within-page case is a single-entry
	// Pages, not this flag alone.
	WithinPage bool `json:"within_page"`
}

// SetDuplicateIDWarningHandler registers the sink for duplicate-id warnings
// detected inside an indexing transaction (within-file duplicates, where the
// blocks.id constraint fires mid-insert). The handler is called after the
// transaction commits, never from inside it, and must not touch the
// database. Pass nil to unregister.
func (dm *DatabaseManager) SetDuplicateIDWarningHandler(fn func(DuplicateIDWarning)) {
	dm.duplicateIDHandlerMu.Lock()
	defer dm.duplicateIDHandlerMu.Unlock()
	dm.duplicateIDHandler = fn
}

// dispatchDuplicateIDWarnings delivers collected warnings to the registered
// handler. Called post-commit only: a rolled-back transaction indexed
// nothing, so warning about it would describe a state that never existed.
func (dm *DatabaseManager) dispatchDuplicateIDWarnings(warnings []DuplicateIDWarning) {
	if len(warnings) == 0 {
		return
	}
	dm.duplicateIDHandlerMu.Lock()
	fn := dm.duplicateIDHandler
	dm.duplicateIDHandlerMu.Unlock()
	if fn == nil {
		// A silent drop here left startup warnings invisible until someone
		// traced the wiring — always leave a trace when there is no sink.
		log.Printf("db: dropping %d duplicate-id warning(s): no handler registered", len(warnings))
		return
	}
	for _, w := range warnings {
		fn(w)
	}
}

// ScanDuplicateIDs reports cross-file duplicate block ids across one scan's
// parsed results: every id carried by two or more resolvable results becomes
// one warning naming each carrier page. Pure — no database access — because
// the scan has every file's parse in hand, which is exactly what makes the
// check exact: a block that MOVED between two files shows up in the new
// file's parse only (the old file's parse no longer has it), while a
// duplicate shows up in both. The database cannot make that distinction
// (a re-index legitimately re-homes ids on moves and renames), so this runs
// at the scan layer, grouping parsed content.
//
// Within-file-only duplicates are deliberately not reported here — the
// indexing transaction catches those at the constraint and owns their
// warning, so a re-indexed file is not warned twice for one defect.
func ScanDuplicateIDs(results []parser.ScanResult) []DuplicateIDWarning {
	type carrier struct {
		page  DuplicateIDPage
		count int
	}
	owners := make(map[string][]carrier)
	for _, res := range results {
		// Unreadable or unresolvable files have no trustworthy coordinates.
		if res.Err != nil || res.Notebook == "" {
			continue
		}
		source := res.Source
		if source == "" {
			source = "vault"
		}
		for _, b := range res.Blocks {
			page := DuplicateIDPage{Source: source, Notebook: res.Notebook, Section: res.Section, Page: res.Page}
			cs := owners[b.ID]
			if n := len(cs); n > 0 && cs[n-1].page == page {
				cs[n-1].count++
			} else {
				cs = append(cs, carrier{page: page, count: 1})
			}
			owners[b.ID] = cs
		}
	}

	var warnings []DuplicateIDWarning
	for id, cs := range owners {
		if len(cs) < 2 {
			continue
		}
		w := DuplicateIDWarning{BlockID: id}
		for _, c := range cs {
			if c.count > 1 {
				// One of the carrier pages holds the id more than once on
				// top of the cross-file collision — say so in the payload.
				w.WithinPage = true
			}
			w.Pages = append(w.Pages, c.page)
		}
		warnings = append(warnings, w)
	}
	return warnings
}

// duplicateIDCollector accumulates the warnings one indexing transaction
// detects (within a single goroutine — the tx scope), so the entry point can
// dispatch them after commit.
type duplicateIDCollector struct {
	warnings []DuplicateIDWarning
}

func (c *duplicateIDCollector) add(w DuplicateIDWarning) {
	c.warnings = append(c.warnings, w)
}

func (c *duplicateIDCollector) drain() []DuplicateIDWarning {
	ws := c.warnings
	c.warnings = nil
	return ws
}
