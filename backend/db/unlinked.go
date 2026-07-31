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
	SourceSnippets []string `json:"source_snippets"`
	MatchCount     int      `json:"match_count"`
	Title          string   `json:"title"`
	Ambiguous      bool     `json:"ambiguous"`
	// Candidates is a capped, stable-sorted promote-target list when Ambiguous.
	Candidates []parser.PagePath `json:"candidates"`
	// CandidatesTruncated is true when more leaf collisions exist than were
	// returned in Candidates (UI can show "and N more").
	CandidatesTruncated bool `json:"candidates_truncated"`
	// CandidatesTotal is the full leaf-collision count before the wire cap
	// (0 when not ambiguous).
	CandidatesTotal int `json:"candidates_total"`
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

// unlinkedScanCap bounds each FTS candidate batch (post CODE/self filter). A
// title that appears in more than this many blocks is almost certainly a common
// word; unbounded scans would dominate IPC cost. Callers may request further
// batches via ScanCursor (user-gated "Scan more") — each round is still capped.
// When a batch fills the cap with more FTS matches beyond it, Truncated is true.
// Mirrors searchFlatCap's rationale.
const unlinkedScanCap = 500

// unlinkedScanFillRounds caps how many FTS keyset probes one call may run when
// CODE/self filters under-fill the candidate window. Each probe is still
// LIMIT unlinkedScanCap+1, so worst-case FTS rows examined ≈ rounds×cap — still
// bounded, never O(all vault matches).
const unlinkedScanFillRounds = 4

// unlinkedAmbiguousCandidateCap bounds Candidates embedded on each residual
// row when a leaf title collides across many pages (#839).
const unlinkedAmbiguousCandidateCap = 32

// scanCursorPrefixV3 versions the opaque FTS batch keyset as the exclusive
// lower-bound rowid observed at scan time (immutable bound). A trailing block
// id is stored for diagnostics only — never re-resolved to a live rowid (that
// skips unread rows when the anchor is re-indexed to a higher rowid).
// Legacy u2:uuid soft-resets (client dedups); u1:rowid still decodes.
const scanCursorPrefixV3 = "u3:"
const scanCursorPrefixV2 = "u2:"
const scanCursorPrefixV1 = "u1:"

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
// last examined rowid (immutable bound from scan time). Residual cursor pages
// only within the current batch window. Client-accumulated batches: the UI
// merges residual pages across Scan more rounds (see ARCHITECTURE §4.3 / §5.4).
//
// The title is the active page's leaf `page` name; queries with a blank or
// sub-2-rune title return an empty result (short names yield too many false
// hits and are not actionable). Ambiguous titles (same leaf name on more than
// one page) are still surfaced — with Ambiguous=true and Candidates populated —
// so the UI can offer one-click disambiguation chips.
func (dm *DatabaseManager) GetUnlinkedMentionsPaged(source, notebook, section, page, cursor, scanCursor string, limit int) (UnlinkedMentionsResult, error) {
	// Normalize path once so title MATCH and self-page exclusion agree (padded
	// IPC page values must not surface the active page as its own unlinked hit).
	page = strings.TrimSpace(page)
	notebook = strings.TrimSpace(notebook)
	section = strings.TrimSpace(section)
	title := page
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

	// Leaf-bounded ambiguity (#839): only pages sharing this leaf name, not
	// the full vault inventory. Cap candidates for IPC/UI.
	titleAmb, err := resolveUnlinkedTitleAmbiguity(db, title, notebook, section)
	if err != nil {
		return UnlinkedMentionsResult{}, fmt.Errorf("unlinked mentions: resolve title: %w", err)
	}

	cacheKey := dm.unlinkedScanCacheKeyNow(source, notebook, section, title, scanCursor)
	var candidates []unlinkedBlock
	var scanTruncated bool
	var lastRowid int64
	var lastID string
	if ent, ok := dm.unlinkedScanCacheGet(cacheKey); ok {
		candidates = ent.blocks
		scanTruncated = ent.truncated
		lastRowid = ent.lastRowid
		lastID = ent.lastID
	} else {
		afterRowid := resolveUnlinkedScanCursor(scanCursor)
		var scanErr error
		candidates, scanTruncated, lastRowid, lastID, scanErr = dm.scanUnlinkedCandidateBlocks(db, title, source, notebook, section, page, afterRowid)
		if scanErr != nil {
			return UnlinkedMentionsResult{}, scanErr
		}
		dm.unlinkedScanCachePut(cacheKey, unlinkedScanCacheEntry{
			blocks:    candidates,
			truncated: scanTruncated,
			lastRowid: lastRowid,
			lastID:    lastID,
		})
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
				Source:              c.source,
				SourceNotebook:      c.notebook,
				SourceSection:       c.section,
				SourcePage:          c.page,
				Title:               title,
				Ambiguous:           titleAmb.ambiguous,
				Candidates:          copyPagePaths(titleAmb.candidates),
				CandidatesTruncated: titleAmb.truncated,
				CandidatesTotal:     titleAmb.total,
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
		outScanCursor = encodeUnlinkedScanCursor(lastRowid, lastID)
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

// unlinkedTitleAmbiguity is the leaf-resolve result for residual rows.
type unlinkedTitleAmbiguity struct {
	ambiguous  bool
	candidates []parser.PagePath
	truncated  bool
	total      int
}

// resolveUnlinkedTitleAmbiguity resolves the active page leaf against only
// pages that share that leaf name (indexed page= lookup), then caps Candidates
// for the residual wire shape. Active notebook/section are preferred in sort
// order so promote chips surface nearby paths first.
func resolveUnlinkedTitleAmbiguity(db *sql.DB, title, activeNotebook, activeSection string) (unlinkedTitleAmbiguity, error) {
	pages, err := listPagesByLeaf(db, title)
	if err != nil {
		return unlinkedTitleAmbiguity{}, err
	}
	ref := ResolvePageLinkAgainst(title, pages)
	if !ref.Ambiguous || len(ref.Candidates) == 0 {
		return unlinkedTitleAmbiguity{ambiguous: ref.Ambiguous, candidates: ref.Candidates}, nil
	}
	sort.SliceStable(ref.Candidates, func(i, j int) bool {
		a, b := ref.Candidates[i], ref.Candidates[j]
		aNear := a.Notebook == activeNotebook && a.Section == activeSection
		bNear := b.Notebook == activeNotebook && b.Section == activeSection
		if aNear != bNear {
			return aNear
		}
		if a.Source != b.Source {
			return a.Source < b.Source
		}
		if a.Notebook != b.Notebook {
			return a.Notebook < b.Notebook
		}
		if a.Section != b.Section {
			return a.Section < b.Section
		}
		return a.Page < b.Page
	})
	total := len(ref.Candidates)
	truncated := total > unlinkedAmbiguousCandidateCap
	cands := ref.Candidates
	if truncated {
		cands = append([]parser.PagePath(nil), ref.Candidates[:unlinkedAmbiguousCandidateCap]...)
	} else {
		cands = append([]parser.PagePath(nil), ref.Candidates...)
	}
	return unlinkedTitleAmbiguity{
		ambiguous:  true,
		candidates: cands,
		truncated:  truncated,
		total:      total,
	}, nil
}

// copyPagePaths returns a shallow copy so residual rows do not share a slice header.
func copyPagePaths(in []parser.PagePath) []parser.PagePath {
	if len(in) == 0 {
		return nil
	}
	out := make([]parser.PagePath, len(in))
	copy(out, in)
	return out
}

// scanUnlinkedCandidateBlocks runs a bounded FTS5 phrase window over
// clean_content and returns candidate blocks (post CODE/self filter) plus
// whether more FTS matches exist beyond the window and the last examined
// (rowid, block id) for scan_cursor.
//
// WHY FTS rowid subquery (not path ORDER BY on the join): EXPLAIN shows
// path-ordered plans do MATCH → join → TEMP B-TREE sort on path columns before
// LIMIT, so common titles still materialize the full match set. A nested
// `SELECT rowid FROM blocks_fts … ORDER BY rowid LIMIT N` uses FTS index 64
// (rowid-ordered) and stops at N before joining blocks.
//
// CODE / self-page filters run in Go after each probe. A single probe can
// under-fill when many hits are CODE/self, so we loop-fill up to
// unlinkedScanFillRounds probes until we have unlinkedScanCap keepers or FTS
// is exhausted — still O(rounds×cap), never O(all vault matches). lastRowid is
// the last FTS hit examined (including filtered rows) so scan_cursor does not
// skip past dropped hits. Residual plain filtering happens in the caller.
//
// afterRowid is an exclusive lower bound (0 = from the start).
func (dm *DatabaseManager) scanUnlinkedCandidateBlocks(db *sql.DB, title, source, notebook, section, page string, afterRowid int64) ([]unlinkedBlock, bool, int64, string, error) {
	unlinkedScanCalls++
	phrase := buildUnlinkedFTSPhrase(title)
	if phrase == "" {
		return nil, false, 0, "", nil
	}
	if afterRowid < 0 {
		afterRowid = 0
	}

	// FTS-first keyset: bound MATCH work, then PK-join blocks (no path TEMP sort).
	// Outer ORDER BY f.rid keeps iteration order identical to the keyset so
	// lastRowid/lastID always reflect the highest examined rid in the probe.
	const q = `SELECT b.rowid, b.id, b.source, b.notebook, b.section, b.page, b.type, COALESCE(b.clean_content,'')
		FROM (
			SELECT rowid AS rid FROM blocks_fts
			WHERE blocks_fts MATCH ?
			  AND rowid > ?
			ORDER BY rowid
			LIMIT ?
		) AS f
		JOIN blocks b ON b.rowid = f.rid
		ORDER BY f.rid`

	type rawRow struct {
		unlinkedBlock
		blockType string
	}
	keep := func(r rawRow) bool {
		if r.blockType == "CODE" {
			return false
		}
		if r.source == source && r.notebook == notebook && r.section == section && r.page == page {
			return false
		}
		return true
	}

	out := make([]unlinkedBlock, 0, unlinkedScanCap)
	after := afterRowid
	var lastRowid int64
	var lastID string
	moreFTS := false

	for round := 0; round < unlinkedScanFillRounds && len(out) < unlinkedScanCap; round++ {
		rows, err := db.Query(q, phrase, after, unlinkedScanCap+1)
		if err != nil {
			return nil, false, 0, "", fmt.Errorf("unlinked mentions: fts scan: %w", err)
		}
		var raw []rawRow
		for rows.Next() {
			var r rawRow
			if err := rows.Scan(&r.rowid, &r.id, &r.source, &r.notebook, &r.section, &r.page, &r.blockType, &r.clean); err != nil {
				rows.Close()
				return nil, false, 0, "", fmt.Errorf("unlinked mentions: scan: %w", err)
			}
			raw = append(raw, r)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, false, 0, "", err
		}
		if len(raw) == 0 {
			moreFTS = false
			break
		}

		probeMore := len(raw) > unlinkedScanCap
		if probeMore {
			raw = raw[:unlinkedScanCap]
		}

		filled := false
		for i, r := range raw {
			lastRowid = r.rowid
			lastID = r.id
			if !keep(r) {
				continue
			}
			out = append(out, r.unlinkedBlock)
			if len(out) >= unlinkedScanCap {
				// Cap reached mid-probe: more FTS exists if unread probe rows remain
				// or the limit+1 probe saw another hit.
				moreFTS = (i+1 < len(raw)) || probeMore
				filled = true
				break
			}
		}
		if filled {
			break
		}
		after = lastRowid
		moreFTS = probeMore
		if !probeMore {
			break
		}
		// Round budget exhausted with probeMore still true: moreFTS stays true
		// and lastRowid anchors scan_cursor so Scan more can continue.
	}

	return out, moreFTS, lastRowid, lastID, nil
}

// encodeUnlinkedScanCursor builds the opaque next-batch keyset from the last
// examined FTS rowid (immutable exclusive bound) plus block id (diagnostic).
//
// WHY store scan-time rowid (not live UUID→rowid): re-resolving the anchor to
// its current rowid skips unread matches when that block is re-indexed to a
// higher rowid. Implicit SQLite rowids on `blocks` are monotonic in practice
// for this workload (no AUTOINCREMENT; reuse only if the highest rowid is
// deleted then re-inserted, or on overflow). Rare reuse is bounded by the
// truncated surface and client-side block-id merge/dedup — do not "fix" by
// re-resolving to a live rowid.
func encodeUnlinkedScanCursor(lastRowid int64, lastBlockID string) string {
	if lastRowid <= 0 {
		return ""
	}
	id := strings.TrimSpace(lastBlockID)
	payload := scanCursorPrefixV3 + strconv.FormatInt(lastRowid, 10)
	if id != "" {
		payload += ":" + id
	}
	return base64.RawURLEncoding.EncodeToString([]byte(payload))
}

// resolveUnlinkedScanCursor maps an opaque scan_cursor to an exclusive lower-
// bound rowid for the next FTS probe. Invalid/legacy tokens soft-reset to 0.
//
// u3:<rowid>[:<block-id>] (current): stored scan-time rowid (immutable bound).
// u2:<block-id> (legacy): soft-reset (live UUID→rowid was a skip hazard).
// u1:<rowid> (legacy): accept raw rowid.
func resolveUnlinkedScanCursor(scanCursor string) int64 {
	if scanCursor == "" {
		return 0
	}
	raw, err := base64.RawURLEncoding.DecodeString(scanCursor)
	if err != nil {
		return 0
	}
	s := string(raw)
	switch {
	case strings.HasPrefix(s, scanCursorPrefixV3):
		rest := s[len(scanCursorPrefixV3):]
		rowPart := rest
		if i := strings.IndexByte(rest, ':'); i >= 0 {
			rowPart = rest[:i]
		}
		n, err := strconv.ParseInt(rowPart, 10, 64)
		if err != nil || n < 0 {
			return 0
		}
		return n
	case strings.HasPrefix(s, scanCursorPrefixV2):
		return 0
	case strings.HasPrefix(s, scanCursorPrefixV1):
		n, err := strconv.ParseInt(s[len(scanCursorPrefixV1):], 10, 64)
		if err != nil || n < 0 {
			return 0
		}
		return n
	default:
		return 0
	}
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
