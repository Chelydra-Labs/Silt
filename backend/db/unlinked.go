package db

import (
	"database/sql"
	"encoding/base64"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"silt/backend/parser"
)

// UnlinkedMention is one source page that mentions the active page's title in
// prose WITHOUT a [[…]] link to it. Surfaced by the backlinks panel so an author
// can promote a plain-text mention into a real page-link. MatchCount and
// SourceBlockIDs aggregate every matching block on that source page; Ambiguous
// is true when the title resolves to more than one page (Link stays disabled —
// promote is rejected server-side to avoid wiring a link to the wrong page).
type UnlinkedMention struct {
	Source         string            `json:"source"`
	SourceNotebook string            `json:"source_notebook"`
	SourceSection  string            `json:"source_section"`
	SourcePage     string            `json:"source_page"`
	SourceBlockIDs []string          `json:"source_block_ids"`
	MatchCount     int               `json:"match_count"`
	Title          string            `json:"title"`
	Ambiguous      bool              `json:"ambiguous"`
	Candidates     []parser.PagePath `json:"candidates"`
}

// UnlinkedMentionsResult is the cursor-paged envelope returned by
// GetUnlinkedMentionsPaged, mirroring BacklinksResult.
type UnlinkedMentionsResult struct {
	Results []UnlinkedMention `json:"results"`
	Cursor  string            `json:"cursor"`
	HasMore bool              `json:"has_more"`
}

// unlinkedScanCap bounds the FTS candidate fetch. A title that appears in more
// than this many blocks is almost certainly a common word the author does not
// want to mass-link; the top results by source-page order are enough to surface
// the feature. Mirrors searchFlatCap's rationale.
const unlinkedScanCap = 500

// GetUnlinkedMentionsPaged returns source pages whose clean_content mentions
// the active page's title as plain prose, excluding pages that already link to
// it via [[…]]. Matches are case-insensitive whole-word(s); multi-word titles
// are matched as a phrase. Results are deduped by source page and cursor-paged
// by the same keyset pattern as GetBacklinksPaged.
//
// The title is the active page's leaf `page` name; queries with a blank or
// sub-2-rune title return an empty result (short names yield too many false
// hits and are not actionable). Ambiguous titles (same leaf name on more than
// one page) are still surfaced — with Ambiguous=true and Candidates populated —
// but the promote IPC rejects them, so the UI disables the Link action.
func (dm *DatabaseManager) GetUnlinkedMentionsPaged(source, notebook, section, page, cursor string, limit int) (UnlinkedMentionsResult, error) {
	title := strings.TrimSpace(page)
	if title == "" || len([]rune(title)) < 2 {
		return UnlinkedMentionsResult{Results: []UnlinkedMention{}}, nil
	}
	if source == "" {
		source = "vault"
	}
	if limit <= 0 {
		limit = BacklinksDefaultLimit
	}
	if limit > BacklinksMaxLimit {
		limit = BacklinksMaxLimit
	}

	db, release, err := dm.handle()
	if err != nil {
		return UnlinkedMentionsResult{}, ErrDBClosed
	}
	defer release()

	// Ambiguity of the leaf title across the whole page inventory (all sources).
	// Promote uses the same check to reject ambiguous targets.
	pages, err := listDistinctPages(db)
	if err != nil {
		return UnlinkedMentionsResult{}, fmt.Errorf("unlinked mentions: list pages: %w", err)
	}
	titleRef := ResolvePageLinkAgainst(title, pages)

	// Already-linked source blocks: any block carrying a [[…]] that resolves to
	// this page. Mirrors legPageLinks' resolve-gate (LinkTargetRawCandidates →
	// reverse index → ResolvePageLinkAgainst). Those blocks are NOT unlinked.
	linkedBlocks, err := dm.blocksAlreadyLinkedTo(db, source, notebook, section, page)
	if err != nil {
		return UnlinkedMentionsResult{}, err
	}

	candidates, err := dm.scanUnlinkedCandidateBlocks(db, title, source, notebook, section, page)
	if err != nil {
		return UnlinkedMentionsResult{}, err
	}

	// Dedupe by source page, dropping already-linked blocks and confirming each
	// block's clean_content actually contains the title as whole word(s). The
	// FTS phrase matches any indexed column (notebook/section included), so the
	// Go-side word-boundary check is the authority for "this block's body
	// mentions the title".
	titleRE := wordBoundaryRE(title)
	type pageKey struct {
		src, nb, sec, pg string
	}
	order := []pageKey{}
	byKey := map[pageKey]*UnlinkedMention{}
	for _, c := range candidates {
		if linkedBlocks[c.id] {
			continue
		}
		if !titleRE.MatchString(c.clean) {
			continue
		}
		k := pageKey{c.source, c.notebook, c.section, c.page}
		m := byKey[k]
		if m == nil {
			m = &UnlinkedMention{
				Source:         c.source,
				SourceNotebook: c.notebook,
				SourceSection:  c.section,
				SourcePage:     c.page,
				Title:          title,
				Ambiguous:      titleRef.Ambiguous,
				Candidates:     titleRef.Candidates,
			}
			byKey[k] = m
			order = append(order, k)
		}
		m.SourceBlockIDs = append(m.SourceBlockIDs, c.id)
		m.MatchCount++
	}

	all := make([]UnlinkedMention, 0, len(order))
	for _, k := range order {
		all = append(all, *byKey[k])
	}
	sortUnlinkedMentions(all)

	startIdx := 0
	if cursorKey, ok := decodeUnlinkedCursor(cursor); ok {
		found := false
		for i, m := range all {
			if unlinkedCursorKey(m) > cursorKey {
				startIdx = i
				found = true
				break
			}
		}
		if !found {
			// Cursor sorts at or past every item → empty page.
			return UnlinkedMentionsResult{Results: []UnlinkedMention{}}, nil
		}
	}

	endIdx := startIdx + limit
	hasMore := endIdx < len(all)
	if endIdx > len(all) {
		endIdx = len(all)
	}
	pageItems := all[startIdx:endIdx]
	if pageItems == nil {
		pageItems = []UnlinkedMention{}
	}

	nextCursor := ""
	if hasMore {
		nextCursor = encodeUnlinkedCursor(pageItems[len(pageItems)-1])
	}
	return UnlinkedMentionsResult{
		Results: pageItems,
		Cursor:  nextCursor,
		HasMore: hasMore,
	}, nil
}

type unlinkedBlock struct {
	id, source, notebook, section, page, clean string
}

// scanUnlinkedCandidateBlocks runs the FTS5 phrase match over clean_content and
// returns the raw candidate blocks (caller drops already-linked blocks and
// confirms the word-boundary match in Go). Excludes the active page itself and
// CODE blocks (code mentions are not actionable prose to promote).
func (dm *DatabaseManager) scanUnlinkedCandidateBlocks(db *sql.DB, title, source, notebook, section, page string) ([]unlinkedBlock, error) {
	phrase := buildUnlinkedFTSPhrase(title)
	if phrase == "" {
		return nil, nil
	}
	q := `SELECT b.id, COALESCE(b.source,'vault'), b.notebook, b.section, b.page, COALESCE(b.clean_content,'')
		FROM blocks_fts
		JOIN blocks b ON b.rowid = blocks_fts.rowid
		WHERE blocks_fts MATCH ?
		  AND b.type <> 'CODE'
		  AND NOT (COALESCE(b.source,'vault') = ? AND b.notebook = ? AND b.section = ? AND b.page = ?)
		LIMIT ?`
	rows, err := db.Query(q, phrase, source, notebook, section, page, unlinkedScanCap)
	if err != nil {
		return nil, fmt.Errorf("unlinked mentions: fts scan: %w", err)
	}
	defer rows.Close()
	var out []unlinkedBlock
	for rows.Next() {
		var c unlinkedBlock
		if err := rows.Scan(&c.id, &c.source, &c.notebook, &c.section, &c.page, &c.clean); err != nil {
			return nil, fmt.Errorf("unlinked mentions: scan: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// buildUnlinkedFTSPhrase turns a page title into an FTS5 phrase MATCH expression
// over clean_content, e.g. "onboarding friction". Double-quotes inside the title
// are stripped (they are illegal in page names) so the phrase is always
// well-formed. Returns "" when the title has no usable tokens.
//
// WHY FTS5 phrase instead of a LIKE word-boundary query: the unicode61 tokenizer
// already defines word boundaries consistently for both the index and the
// phrase, so a phrase match IS a case-insensitive whole-word(s) match for free,
// and it rides the existing blocks_fts index (cost ∝ match count, not a full
// scan). A LIKE approach would need escaped boundaries + a full table scan. The
// caller still confirms the match in Go (titleRE) because FTS phrase matches any
// indexed column, not strictly clean_content.
func buildUnlinkedFTSPhrase(title string) string {
	cleaned := strings.Map(func(r rune) rune {
		if r == '"' {
			return -1
		}
		return r
	}, title)
	cleaned = strings.TrimSpace(cleaned)
	if cleaned == "" {
		return ""
	}
	return `clean_content : "` + cleaned + `"`
}

// wordBoundaryRE compiles a case-insensitive, word-boundary regex for title.
func wordBoundaryRE(title string) *regexp.Regexp {
	return regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(title) + `\b`)
}

// blocksAlreadyLinkedTo returns the set of source_block_ids whose [[…]] resolves
// non-ambiguously to (source, notebook, section, page). Mirrors legPageLinks.
func (dm *DatabaseManager) blocksAlreadyLinkedTo(db *sql.DB, source, notebook, section, page string) (map[string]bool, error) {
	candidates := LinkTargetRawCandidates([]LinkTargetSpec{
		{Source: source, Notebook: notebook, Section: section, Page: page},
	})
	rows, err := listPageLinksByTargetRaws(db, candidates)
	if err != nil {
		return nil, fmt.Errorf("unlinked mentions: page links: %w", err)
	}
	if len(rows) == 0 {
		return map[string]bool{}, nil
	}
	pages, err := listDistinctPages(db)
	if err != nil {
		return nil, fmt.Errorf("unlinked mentions: list pages for resolve: %w", err)
	}
	linked := make(map[string]bool, len(rows))
	for _, r := range rows {
		ref := ResolvePageLinkAgainst(r.TargetRaw, pages)
		if !ref.Exists || ref.Ambiguous {
			continue
		}
		if ref.Source != source || ref.Notebook != notebook || ref.Section != section || ref.Page != page {
			continue
		}
		linked[r.SourceBlockID] = true
	}
	return linked, nil
}

// sortUnlinkedMentions stably sorts by (source, source_notebook, source_section,
// source_page) — the dedupe key — for deterministic cursor-paged rendering.
func sortUnlinkedMentions(ms []UnlinkedMention) {
	sort.SliceStable(ms, func(i, j int) bool {
		a, b := ms[i], ms[j]
		if a.Source != b.Source {
			return a.Source < b.Source
		}
		if a.SourceNotebook != b.SourceNotebook {
			return a.SourceNotebook < b.SourceNotebook
		}
		if a.SourceSection != b.SourceSection {
			return a.SourceSection < b.SourceSection
		}
		return a.SourcePage < b.SourcePage
	})
}

// unlinkedCursorKey is the keyset tuple for an UnlinkedMention cursor.
func unlinkedCursorKey(m UnlinkedMention) string {
	return m.Source + cursorSep +
		m.SourceNotebook + cursorSep +
		m.SourceSection + cursorSep +
		m.SourcePage
}

func encodeUnlinkedCursor(m UnlinkedMention) string {
	return base64.RawURLEncoding.EncodeToString([]byte(unlinkedCursorKey(m)))
}

func decodeUnlinkedCursor(cursor string) (string, bool) {
	if cursor == "" {
		return "", false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return "", false
	}
	return string(decoded), true
}
