package main

import (
	"fmt"
	"regexp"
	"strings"

	"silt/backend/db"
)

// GetUnlinkedMentionsPaged returns source pages that mention the active page's
// title in prose without linking it, so the backlinks panel can offer a "Link"
// action. Source-aware (notebook → source resolved server-side). Cursor/limit
// mirror GetBacklinksPaged. scanCursor continues a capped FTS batch when the
// prior response set truncated + scan_cursor (empty starts the first batch).
// See db.GetUnlinkedMentionsPaged for matching rules.
func (a *App) GetUnlinkedMentionsPaged(notebook, section, page, cursor, scanCursor string, limit int) (db.UnlinkedMentionsResult, error) {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.db == nil {
		return db.UnlinkedMentionsResult{}, fmt.Errorf("vault database not loaded")
	}
	a.wg.Add(1)
	defer a.wg.Done()

	// Trim before source resolve and DB self-filter so padded linked-notebook
	// names still map correctly and the active page is excluded.
	notebook = strings.TrimSpace(notebook)
	section = strings.TrimSpace(section)
	page = strings.TrimSpace(page)

	source := a.resolveSourceByName(notebook)
	var res db.UnlinkedMentionsResult
	err := a.coordinator.WithDBReadResult(func() error {
		got, err := a.db.GetUnlinkedMentionsPaged(source, notebook, section, page, cursor, scanCursor, limit)
		if err != nil {
			return err
		}
		res = got
		return nil
	})
	if err != nil {
		return db.UnlinkedMentionsResult{}, err
	}
	return res, nil
}

// PromoteUnlinkedMention wraps the first plain-text occurrence of the target
// page's title in sourceBlockID's body with a [[shortest]] link, writing through
// the same atomic chain as MutateBlock (so embeds/backlinks refresh on the next
// block:changed).
//
// Target resolution prefers an explicit (notebook, section, page) path when that
// location exists in the page inventory — so the UI can promote ambiguous leaf
// titles via a candidate chip without guessing. When the triple is not in the
// inventory, resolution falls back to leaf-name lookup and rejects ambiguous
// leaves with CodeAmbiguousTarget.
func (a *App) PromoteUnlinkedMention(sourceBlockID, targetNotebook, targetSection, targetPage string) error {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.db == nil {
		return fmt.Errorf("vault database not loaded")
	}
	a.wg.Add(1)
	defer a.wg.Done()

	title := strings.TrimSpace(targetPage)
	if title == "" {
		// Plain error — not CodeAmbiguousTarget (that maps to "pick a candidate").
		return fmt.Errorf("cannot promote an empty page title")
	}

	var pages []db.PageLoc
	err := a.coordinator.WithDBReadResult(func() error {
		got, err := a.db.ListDistinctPages()
		if err != nil {
			return err
		}
		pages = got
		return nil
	})
	if err != nil {
		return fmt.Errorf("promote unlinked mention: list pages: %w", err)
	}

	targetSource := a.resolveSourceByName(targetNotebook)
	loc := db.PageLoc{Source: targetSource, Notebook: targetNotebook, Section: targetSection, Page: title}
	chosen, ok := findExactPageLoc(pages, loc)
	if !ok {
		// Explicit path missing — fall back to leaf resolution (unique only).
		ref := db.ResolvePageLinkAgainst(title, pages)
		if ref.Ambiguous {
			cands := make([]string, 0, len(ref.Candidates))
			for _, c := range ref.Candidates {
				cands = append(cands, c.Notebook+"/"+c.Section+"/"+c.Page)
			}
			return NewIPCError(CodeAmbiguousTarget,
				fmt.Sprintf("page title %q is ambiguous (matches: %s)", title, strings.Join(cands, ", ")))
		}
		if !ref.Exists {
			return fmt.Errorf("page %q not found in inventory", title)
		}
		chosen = db.PageLoc{
			Source:   ref.Source,
			Notebook: ref.Notebook,
			Section:  ref.Section,
			Page:     ref.Page,
		}
	}
	shortest := db.ShortestUniquePath(chosen, pages)

	titleRE := db.WordBoundaryTitleRE(title)
	return a.writeBlockText(sourceBlockID, func(currentClean string) (string, error) {
		newText, ok := wrapFirstUnlinkedOccurrence(currentClean, titleRE, shortest)
		if !ok {
			return "", fmt.Errorf("title %q no longer appears as plain text in the block", title)
		}
		return newText, nil
	})
}

// findExactPageLoc returns the inventory page matching source+notebook+section+page.
func findExactPageLoc(pages []db.PageLoc, want db.PageLoc) (db.PageLoc, bool) {
	for _, p := range pages {
		if p.Source == want.Source &&
			p.Notebook == want.Notebook &&
			p.Section == want.Section &&
			p.Page == want.Page {
			return p, true
		}
	}
	return db.PageLoc{}, false
}

// wrapFirstUnlinkedOccurrence wraps the first residual plain title match in
// clean (per titleRE / db.FirstPlainTitleOccurrence), producing
// before+"[[linkTarget]]"+after. Returns (clean, false) when no promotable
// occurrence exists. List and promote share FirstPlainTitleOccurrence so a
// block that already links the page once can still promote a remaining plain hit.
func wrapFirstUnlinkedOccurrence(clean string, titleRE *regexp.Regexp, linkTarget string) (string, bool) {
	start, end, ok := db.FirstPlainTitleOccurrence(clean, titleRE)
	if !ok {
		return clean, false
	}
	return clean[:start] + "[[" + linkTarget + "]]" + clean[end:], true
}
