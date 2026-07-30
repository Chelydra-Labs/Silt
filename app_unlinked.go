package main

import (
	"fmt"
	"regexp"
	"strings"

	"silt/backend/db"
	"silt/backend/parser"
)

// GetUnlinkedMentionsPaged returns source pages that mention the active page's
// title in prose without linking it, so the backlinks panel can offer a "Link"
// action. Source-aware (notebook → source resolved server-side). Cursor/limit
// mirror GetBacklinksPaged. See db.GetUnlinkedMentionsPaged for matching rules.
func (a *App) GetUnlinkedMentionsPaged(notebook, section, page, cursor string, limit int) (db.UnlinkedMentionsResult, error) {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.db == nil {
		return db.UnlinkedMentionsResult{}, fmt.Errorf("vault database not loaded")
	}
	a.wg.Add(1)
	defer a.wg.Done()

	source := a.resolveSourceByName(notebook)
	var res db.UnlinkedMentionsResult
	err := a.coordinator.WithDBReadResult(func() error {
		got, err := a.db.GetUnlinkedMentionsPaged(source, notebook, section, page, cursor, limit)
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
// block:changed). Ambiguous targets (leaf name on >1 page) are rejected with an
// *IPCError carrying CodeAmbiguousTarget — the UI must surface the candidates
// and let the author disambiguate manually rather than silently wiring the link.
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
		return NewIPCError(CodeAmbiguousTarget, "cannot promote an empty page title")
	}

	pages, err := a.db.ListDistinctPages()
	if err != nil {
		return fmt.Errorf("promote unlinked mention: list pages: %w", err)
	}
	// Ambiguity uses the same leaf-name resolution as the unlinked query, so a
	// row marked Ambiguous=true can never be promoted.
	if ref := db.ResolvePageLinkAgainst(title, pages); ref.Ambiguous {
		cands := make([]string, 0, len(ref.Candidates))
		for _, c := range ref.Candidates {
			cands = append(cands, c.Notebook+"/"+c.Section+"/"+c.Page)
		}
		return NewIPCError(CodeAmbiguousTarget,
			fmt.Sprintf("page title %q is ambiguous (matches: %s)", title, strings.Join(cands, ", ")))
	}

	targetSource := a.resolveSourceByName(targetNotebook)
	loc := db.PageLoc{Source: targetSource, Notebook: targetNotebook, Section: targetSection, Page: targetPage}
	shortest := db.ShortestUniquePath(loc, pages)

	titleRE := db.WordBoundaryTitleRE(title)
	return a.writeBlockText(sourceBlockID, func(currentClean string) (string, error) {
		newText, ok := wrapFirstUnlinkedOccurrence(currentClean, titleRE, shortest)
		if !ok {
			return "", fmt.Errorf("title %q no longer appears as plain text in the block", title)
		}
		return newText, nil
	})
}

// wrapFirstUnlinkedOccurrence wraps the first title match in clean (per titleRE)
// that is NOT already inside a [[…]] span, producing before+"[[linkTarget]]"+after.
// Returns (clean, false) when no promotable occurrence exists. Already-linked
// spans are located via parser.PageLinkRegex so heading/alias variants are
// covered by the same grammar that indexed them.
func wrapFirstUnlinkedOccurrence(clean string, titleRE *regexp.Regexp, linkTarget string) (string, bool) {
	type span struct{ start, end int }
	var linked []span
	for _, idx := range parser.PageLinkRegex.FindAllStringSubmatchIndex(clean, -1) {
		if len(idx) >= 2 {
			linked = append(linked, span{idx[0], idx[1]})
		}
	}
	inLinked := func(s, e int) bool {
		for _, sp := range linked {
			if s < sp.end && e > sp.start { // overlap
				return true
			}
		}
		return false
	}
	for _, m := range titleRE.FindAllStringSubmatchIndex(clean, -1) {
		// WordBoundaryTitleRE captures the title as group 1 (m[2]:m[3]); the
		// leading/trailing boundary char (m[0]:m[1]) is not part of the title.
		if len(m) < 4 {
			continue
		}
		if inLinked(m[2], m[3]) {
			continue
		}
		return clean[:m[2]] + "[[" + linkTarget + "]]" + clean[m[3]:], true
	}
	return clean, false
}
