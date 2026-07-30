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

// UnlinkedMention is one source page with at least one residual plain (non-[[…]])
// whole-word mention of the active page's title. Blocks that already [[link]]
// the page still appear when plain residual text remains. Surfaced by the
// backlinks panel so an author can promote that plain hit. MatchCount,
// SourceBlockIDs, and SourceSnippets aggregate every matching block on that
// source page (snippets are parallel to block IDs — one contextual excerpt per
// match, centered on the residual plain span). Ambiguous is true when the title
// resolves to more than one page; the UI offers candidate chips and
// PromoteUnlinkedMention accepts an explicit path so the author can
// disambiguate without guessing.
type UnlinkedMention struct {
	Source         string   `json:"source"`
	SourceNotebook string   `json:"source_notebook"`
	SourceSection  string   `json:"source_section"`
	SourcePage     string   `json:"source_page"`
	SourceBlockIDs []string `json:"source_block_ids"`
	// SourceSnippets is parallel to SourceBlockIDs: a 120-rune contextual
	// excerpt of clean_content centered on the residual plain title span.
	SourceSnippets []string          `json:"source_snippets"`
	MatchCount     int               `json:"match_count"`
	Title          string            `json:"title"`
	Ambiguous      bool              `json:"ambiguous"`
	Candidates     []parser.PagePath `json:"candidates"`
}

// UnlinkedMentionsResult is the cursor-paged envelope returned by
// GetUnlinkedMentionsPaged, mirroring BacklinksResult.
//
// HasMore is page-level: more residual source pages exist in the current FTS
// candidate pool beyond this cursor page. Truncated is pool-level: the FTS
// candidate fetch hit unlinkedScanCap, so residual mentions beyond the ordered
// top-N candidates may be missing. The two flags are independent.
type UnlinkedMentionsResult struct {
	Results   []UnlinkedMention `json:"results"`
	Cursor    string            `json:"cursor"`
	HasMore   bool              `json:"has_more"`
	Truncated bool              `json:"truncated"`
}

// unlinkedScanCap bounds the FTS candidate fetch. A title that appears in more
// than this many blocks is almost certainly a common word; unbounded scans would
// dominate IPC cost. The ordered top-N candidates are enough to surface the
// feature. When the cap binds, Truncated is true so the UI can warn that results
// may be incomplete — distinct from page-level HasMore. Mirrors searchFlatCap's
// rationale.
const unlinkedScanCap = 500

// GetUnlinkedMentionsPaged returns source pages whose clean_content has a
// residual plain (non-[[…]]) whole-word mention of the active page's title.
// Blocks that already contain a [[…]] to the page still appear when plain
// residual text remains; fully-linked-only blocks are excluded. Matches are
// case-insensitive whole-word(s); multi-word titles are matched as a phrase.
// Results are deduped by source page and cursor-paged by the same keyset
// pattern as GetBacklinksPaged.
//
// The title is the active page's leaf `page` name; queries with a blank or
// sub-2-rune title return an empty result (short names yield too many false
// hits and are not actionable). Ambiguous titles (same leaf name on more than
// one page) are still surfaced — with Ambiguous=true and Candidates populated —
// so the UI can offer one-click disambiguation chips.
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

	candidates, scanTruncated, err := dm.scanUnlinkedCandidateBlocks(db, title, source, notebook, section, page)
	if err != nil {
		return UnlinkedMentionsResult{}, err
	}

	// Dedupe by source page. Include a block only when clean_content has a
	// residual plain (non-[[…]]) whole-word title occurrence — same rule as
	// PromoteUnlinkedMention / FirstPlainTitleOccurrence. Blocks that only
	// mention the title inside an already-linked span stay out; mixed
	// linked+plain blocks surface so the residual plain hit can be promoted.
	// FTS may match notebook/section columns, so the Go-side residual check is
	// the authority for "this block's body has a promotable plain mention".
	titleRE := WordBoundaryTitleRE(title)
	type pageKey struct {
		src, nb, sec, pg string
	}
	order := []pageKey{}
	byKey := map[pageKey]*UnlinkedMention{}
	for _, c := range candidates {
		start, end, ok := FirstPlainTitleOccurrence(c.clean, titleRE)
		if !ok {
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
		// Center the snippet on the residual plain span (not the first raw
		// title substring, which may sit inside an earlier [[…]]).
		m.SourceSnippets = append(m.SourceSnippets, snippetAround(c.clean, start, end))
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
			// Pool-level Truncated still applies (scan already ran).
			return UnlinkedMentionsResult{
				Results:   []UnlinkedMention{},
				Truncated: scanTruncated,
			}, nil
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
		Results:   pageItems,
		Cursor:    nextCursor,
		HasMore:   hasMore,
		Truncated: scanTruncated,
	}, nil
}

type unlinkedBlock struct {
	id, source, notebook, section, page, clean string
}

// scanUnlinkedCandidateBlocks runs the FTS5 phrase match over clean_content and
// returns raw candidate blocks plus whether the ordered fetch hit unlinkedScanCap.
// The caller keeps only blocks with a residual plain title occurrence
// (FirstPlainTitleOccurrence). Excludes the active page itself and CODE blocks
// (code mentions are not actionable prose to promote).
//
// ORDER BY matches the residual page sort key so the capped set is stable.
// LIMIT is unlinkedScanCap+1 so Truncated is true only when a row exists beyond
// the cap (same exact-boundary rule as PluginRawQuery).
func (dm *DatabaseManager) scanUnlinkedCandidateBlocks(db *sql.DB, title, source, notebook, section, page string) ([]unlinkedBlock, bool, error) {
	phrase := buildUnlinkedFTSPhrase(title)
	if phrase == "" {
		return nil, false, nil
	}
	q := `SELECT b.id, COALESCE(b.source,'vault'), b.notebook, b.section, b.page, COALESCE(b.clean_content,'')
		FROM blocks_fts
		JOIN blocks b ON b.rowid = blocks_fts.rowid
		WHERE blocks_fts MATCH ?
		  AND b.type <> 'CODE'
		  AND NOT (COALESCE(b.source,'vault') = ? AND b.notebook = ? AND b.section = ? AND b.page = ?)
		ORDER BY COALESCE(b.source,'vault'), b.notebook, b.section, b.page, b.id
		LIMIT ?`
	rows, err := db.Query(q, phrase, source, notebook, section, page, unlinkedScanCap+1)
	if err != nil {
		return nil, false, fmt.Errorf("unlinked mentions: fts scan: %w", err)
	}
	defer rows.Close()
	var out []unlinkedBlock
	for rows.Next() {
		var c unlinkedBlock
		if err := rows.Scan(&c.id, &c.source, &c.notebook, &c.section, &c.page, &c.clean); err != nil {
			return nil, false, fmt.Errorf("unlinked mentions: scan: %w", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	if len(out) > unlinkedScanCap {
		return out[:unlinkedScanCap], true, nil
	}
	return out, false, nil
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

// WordBoundaryTitleRE compiles a case-insensitive, Unicode-aware word-boundary
// regex for title, capturing the title itself as group 1. RE2's \b is
// ASCII-only (a [0-9A-Za-z_] boundary), so it silently fails for accented or
// CJK titles (e.g. "Café", "会议") — the explicit [^\p{L}\p{N}_] boundaries are
// Unicode-correct. Callers use MatchString for an existence check and group 1's
// indices (SubmatchIndex[2]:[3]) to wrap exactly the title text.
func WordBoundaryTitleRE(title string) *regexp.Regexp {
	return regexp.MustCompile(`(?i)(?:^|[^\p{L}\p{N}_])(` + regexp.QuoteMeta(title) + `)(?:$|[^\p{L}\p{N}_])`)
}

// FirstPlainTitleOccurrence returns the byte range [start,end) of the first
// whole-word title match in clean that does NOT overlap a [[…]] page-link span.
// Matches inside wiki links are skipped so list and promote share one rule:
// residual plain text remains promotable even when the block already links the
// same page once. ok is false when no residual plain occurrence exists.
func FirstPlainTitleOccurrence(clean string, titleRE *regexp.Regexp) (start, end int, ok bool) {
	if titleRE == nil || clean == "" {
		return 0, 0, false
	}
	type span struct{ start, end int }
	var linked []span
	for _, idx := range parser.PageLinkRegex.FindAllStringSubmatchIndex(clean, -1) {
		if len(idx) >= 2 {
			linked = append(linked, span{idx[0], idx[1]})
		}
	}
	inLinked := func(s, e int) bool {
		for _, sp := range linked {
			if s < sp.end && e > sp.start {
				return true
			}
		}
		return false
	}
	for _, m := range titleRE.FindAllStringSubmatchIndex(clean, -1) {
		// WordBoundaryTitleRE captures the title as group 1 (m[2]:m[3]).
		if len(m) < 4 {
			continue
		}
		if inLinked(m[2], m[3]) {
			continue
		}
		return m[2], m[3], true
	}
	return 0, 0, false
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
