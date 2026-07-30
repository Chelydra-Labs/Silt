package db

import (
	"database/sql"
	"encoding/base64"
	"fmt"
	"regexp"
	"sort"
	"strconv"
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
// candidate window beyond this cursor page. Truncated is pool-level: more FTS
// candidates exist beyond the current window (past unlinkedScanCap for this
// batch). ScanCursor is the opaque keyset to fetch the next FTS batch when
// Truncated is true (empty otherwise). Residual cursor/HasMore are orthogonal
// to Truncated/ScanCursor — see scanUnlinkedCandidateBlocks.
type UnlinkedMentionsResult struct {
	Results    []UnlinkedMention `json:"results"`
	Cursor     string            `json:"cursor"`
	HasMore    bool              `json:"has_more"`
	Truncated  bool              `json:"truncated"`
	ScanCursor string            `json:"scan_cursor"`
}

// unlinkedScanCap bounds each FTS candidate batch. A title that appears in more
// than this many blocks is almost certainly a common word; unbounded scans would
// dominate IPC cost. Callers may request further batches via ScanCursor (user-
// gated "Scan more") — each round is still capped. When a batch fills the cap
// with more matches beyond it, Truncated is true. Mirrors searchFlatCap's
// rationale. Cost per call is O(window) join/fetch for the batch, not O(all
// FTS matches in the vault).
const unlinkedScanCap = 500

// scanCursorPrefix versions the opaque FTS batch keyset (decimal rowid after
// the last included candidate). Invalid/unknown tokens soft-reset to 0.
const scanCursorPrefix = "u1:"

// GetUnlinkedMentionsPaged returns source pages whose clean_content has a
// residual plain (non-[[…]]) whole-word mention of the active page's title.
// Blocks that already contain a [[…]] to the page still appear when plain
// residual text remains; fully-linked-only blocks are excluded. Matches are
// case-insensitive whole-word(s); multi-word titles are matched as a phrase.
// Results are deduped by source page and cursor-paged by the same keyset
// pattern as GetBacklinksPaged.
//
// scanCursor selects the FTS candidate batch: empty starts at the lowest
// matching rowid; a prior result's ScanCursor continues after that batch's
// last rowid. Residual cursor pages only within the current batch window.
// Client-accumulated batches: the UI appends unique residual pages across
// Scan more rounds (see ARCHITECTURE §4.3 / §5.4).
//
// The title is the active page's leaf `page` name; queries with a blank or
// sub-2-rune title return an empty result (short names yield too many false
// hits and are not actionable). Ambiguous titles (same leaf name on more than
// one page) are still surfaced — with Ambiguous=true and Candidates populated —
// so the UI can offer one-click disambiguation chips.
func (dm *DatabaseManager) GetUnlinkedMentionsPaged(source, notebook, section, page, cursor, scanCursor string, limit int) (UnlinkedMentionsResult, error) {
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

	afterRowid := decodeUnlinkedScanCursor(scanCursor)
	candidates, scanTruncated, lastRowid, err := dm.scanUnlinkedCandidateBlocks(db, title, source, notebook, section, page, afterRowid)
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

	outScanCursor := ""
	if scanTruncated {
		outScanCursor = encodeUnlinkedScanCursor(lastRowid)
	}

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
			// Pool-level Truncated / ScanCursor still apply (scan already ran).
			return UnlinkedMentionsResult{
				Results:    []UnlinkedMention{},
				Truncated:  scanTruncated,
				ScanCursor: outScanCursor,
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
		Results:    pageItems,
		Cursor:     nextCursor,
		HasMore:    hasMore,
		Truncated:  scanTruncated,
		ScanCursor: outScanCursor,
	}, nil
}

type unlinkedBlock struct {
	rowid                                      int64
	id, source, notebook, section, page, clean string
}

// scanUnlinkedCandidateBlocks runs one bounded FTS5 phrase batch over
// clean_content and returns raw candidate blocks plus whether more matches
// exist beyond this batch.
//
// WHY FTS rowid subquery (not path ORDER BY on the join): EXPLAIN shows
// path-ordered plans do MATCH → join → TEMP B-TREE sort on path columns before
// LIMIT, so common titles still materialize the full match set. A nested
// `SELECT rowid FROM blocks_fts … ORDER BY rowid LIMIT N` uses FTS index 64
// (rowid-ordered) and stops at N before joining blocks — cost O(cap) per call.
// CODE / self-page filters run in Go after the join so they cannot push work
// past the FTS LIMIT (filters after LIMIT would under-fill and skip rowids).
// Residual pages are still path-sorted in Go; scan order only defines the batch.
//
// afterRowid is an exclusive lower bound (0 = from the start). LIMIT is
// unlinkedScanCap+1 so Truncated is true only when a row exists beyond the cap
// (same exact-boundary rule as PluginRawQuery). lastRowid is the last FTS
// rowid in the kept window (keyset for the next batch), including rows later
// dropped as CODE/self so continuation does not skip past them incorrectly.
func (dm *DatabaseManager) scanUnlinkedCandidateBlocks(db *sql.DB, title, source, notebook, section, page string, afterRowid int64) ([]unlinkedBlock, bool, int64, error) {
	phrase := buildUnlinkedFTSPhrase(title)
	if phrase == "" {
		return nil, false, 0, nil
	}
	if afterRowid < 0 {
		afterRowid = 0
	}
	// FTS-first keyset: bound MATCH work, then PK-join blocks (no path TEMP sort).
	q := `SELECT b.rowid, b.id, b.source, b.notebook, b.section, b.page, b.type, COALESCE(b.clean_content,'')
		FROM (
			SELECT rowid AS rid FROM blocks_fts
			WHERE blocks_fts MATCH ?
			  AND rowid > ?
			ORDER BY rowid
			LIMIT ?
		) AS f
		JOIN blocks b ON b.rowid = f.rid`
	rows, err := db.Query(q, phrase, afterRowid, unlinkedScanCap+1)
	if err != nil {
		return nil, false, 0, fmt.Errorf("unlinked mentions: fts scan: %w", err)
	}
	defer rows.Close()
	type rawRow struct {
		unlinkedBlock
		blockType string
	}
	var raw []rawRow
	for rows.Next() {
		var r rawRow
		if err := rows.Scan(&r.rowid, &r.id, &r.source, &r.notebook, &r.section, &r.page, &r.blockType, &r.clean); err != nil {
			return nil, false, 0, fmt.Errorf("unlinked mentions: scan: %w", err)
		}
		raw = append(raw, r)
	}
	if err := rows.Err(); err != nil {
		return nil, false, 0, err
	}
	truncated := len(raw) > unlinkedScanCap
	if truncated {
		raw = raw[:unlinkedScanCap]
	}
	var last int64
	if len(raw) > 0 {
		last = raw[len(raw)-1].rowid
	}
	out := make([]unlinkedBlock, 0, len(raw))
	for _, r := range raw {
		if r.blockType == "CODE" {
			continue
		}
		if r.source == source && r.notebook == notebook && r.section == section && r.page == page {
			continue
		}
		out = append(out, r.unlinkedBlock)
	}
	return out, truncated, last, nil
}

// encodeUnlinkedScanCursor builds the opaque next-batch keyset from the last
// included candidate rowid (next fetch uses rowid > last).
func encodeUnlinkedScanCursor(lastRowid int64) string {
	if lastRowid <= 0 {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString([]byte(scanCursorPrefix + strconv.FormatInt(lastRowid, 10)))
}

// decodeUnlinkedScanCursor returns the exclusive lower-bound rowid for the next
// FTS batch. Empty or invalid tokens soft-reset to 0 (first batch), matching
// residual cursor tolerance.
func decodeUnlinkedScanCursor(scanCursor string) int64 {
	if scanCursor == "" {
		return 0
	}
	raw, err := base64.RawURLEncoding.DecodeString(scanCursor)
	if err != nil {
		return 0
	}
	s := string(raw)
	if !strings.HasPrefix(s, scanCursorPrefix) {
		return 0
	}
	n, err := strconv.ParseInt(s[len(scanCursorPrefix):], 10, 64)
	if err != nil || n < 0 {
		return 0
	}
	return n
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
