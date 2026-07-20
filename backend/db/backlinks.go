package db

import (
	"database/sql"
	"encoding/base64"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

// BacklinkKind discriminates the three legs of the backlinks query.
type BacklinkKind string

const (
	BacklinkPageLink BacklinkKind = "page"
	BacklinkBlockRef BacklinkKind = "block-ref"
	BacklinkEmbed    BacklinkKind = "embed"
)

// Backlink is one inbound reference to a page, surfaced by the backlinks panel.
// Source identifies the root ('vault' | 'linked:<id>') the linking page belongs to.
type Backlink struct {
	Kind           BacklinkKind `json:"linkKind"`
	Source         string       `json:"source"`
	SourceNotebook string       `json:"source_notebook"`
	SourceSection  string       `json:"source_section"`
	SourcePage     string       `json:"source_page"`
	SourceBlockID  string       `json:"source_block_id"`
	Snippet        string       `json:"snippet"`
}

// BacklinksResult is the cursor-paged envelope returned by GetBacklinksPaged.
// Cursor is an opaque base64-encoded sort-key position; empty when no more
// results exist. HasMore is true when additional pages can be fetched.
type BacklinksResult struct {
	Results []Backlink `json:"results"`
	Cursor  string     `json:"cursor"`
	HasMore bool       `json:"has_more"`
}

// BacklinksDefaultLimit is the page size when the caller passes limit=0.
const BacklinksDefaultLimit = 50

// BacklinksMaxLimit is the hard cap on a single page. Requesting more is
// silently clamped.
const BacklinksMaxLimit = 500

// cursorSep separates fields inside a backlink cursor. Must be a byte that
// cannot appear in any of the source/notebook/section/page/kind/block_id
// fields. NUL is safe because SQLite identifiers never contain NUL.
const cursorSep = "\x00"

const backlinkSnippetRunes = 120

// snippetEllipsis is the single Unicode ellipsis character used as a truncation
// marker in backlink snippets.
const snippetEllipsis = "…"

// backlinkKey is the dedupe key for a single backlink result.
type backlinkKey struct {
	kind    BacklinkKind
	src     string
	nb      string
	sec     string
	page    string
	blockID string
}

// GetBacklinks returns every inbound reference to the given target page across
// three legs: page-links ([[…]]), block-refs ((uuid)), and embeds
// ({{embed:uuid}}). Source-aware: the caller passes the resolved source so
// target-page blocks are scoped correctly.
//
// Page-links use LinkTargetRawCandidates + ListPageLinksByTargetRaws +
// ListDistinctPages + ResolvePageLinkAgainst for exact nonambiguous canonical
// target resolution (matching the rename-rewrite path). Block-refs and embeds
// use the derived block_references reverse index (#704): an indexed SEARCH
// against idx_block_references_target, parameterized by the target page's
// block IDs. Results are deduped and stably sorted by (source, source_notebook,
// source_section, source_page, kind, source_block_id).
func (dm *DatabaseManager) GetBacklinks(source, notebook, section, page string) ([]Backlink, error) {
	if page == "" {
		return nil, nil
	}
	if source == "" {
		source = "vault"
	}

	db, release, err := dm.handle()
	if err != nil {
		return nil, ErrDBClosed
	}
	defer release()

	all, err := dm.collectBacklinks(db, source, notebook, section, page)
	if err != nil {
		return nil, err
	}
	sortBacklinks(all)
	return all, nil
}

// GetBacklinksPaged returns a cursor-paged slice of inbound references.
// Cursor is an opaque base64 token from a previous call's Cursor field (empty
// for the first page). Limit is clamped to [1, BacklinksMaxLimit]; passing 0
// uses BacklinksDefaultLimit.
//
// The cursor is a keyset cursor over the deterministic sort order
// (source, source_notebook, source_section, source_page, kind, source_block_id).
// Collecting the full result set is required because the three query legs
// (indexed page-links + indexed block-refs + indexed embeds) must merge and
// dedupe in Go; the cursor slices into this sorted set rather than re-querying
// SQL. Each leg is now an indexed lookup against its derived reverse table
// (page_links / block_references), so collection cost is proportional to
// inbound edge count rather than total block count (#704).
func (dm *DatabaseManager) GetBacklinksPaged(source, notebook, section, page string, cursor string, limit int) (BacklinksResult, error) {
	if page == "" {
		return BacklinksResult{Results: []Backlink{}}, nil
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
		return BacklinksResult{}, ErrDBClosed
	}
	defer release()

	all, err := dm.collectBacklinks(db, source, notebook, section, page)
	if err != nil {
		return BacklinksResult{}, err
	}
	sortBacklinks(all)

	startIdx := 0
	if cursorKey, ok := decodeBacklinkCursor(cursor); ok {
		for i, b := range all {
			if backlinkCursorKey(b) > cursorKey {
				startIdx = i
				break
			}
			// cursor points at the last item of the previous page; skip it.
			if i == len(all)-1 {
				// cursor key >= all items → no more results
				return BacklinksResult{Results: []Backlink{}}, nil
			}
		}
	}

	endIdx := startIdx + limit
	hasMore := endIdx < len(all)
	if endIdx > len(all) {
		endIdx = len(all)
	}
	pageItems := all[startIdx:endIdx]
	if pageItems == nil {
		pageItems = []Backlink{}
	}

	nextCursor := ""
	if hasMore {
		nextCursor = encodeBacklinkCursor(pageItems[len(pageItems)-1])
	}
	return BacklinksResult{
		Results: pageItems,
		Cursor:  nextCursor,
		HasMore: hasMore,
	}, nil
}

// collectBacklinks gathers all inbound references across the three legs
// (page-links, block-refs, embeds), deduplicates, and returns the unsorted
// collection. Callers that need ordering or pagination call sortBacklinks
// and slice themselves.
func (dm *DatabaseManager) collectBacklinks(db *sql.DB, source, notebook, section, page string) ([]Backlink, error) {

	// 1. Collect the target page's block IDs (needed for block-ref/embed legs).
	targetBlockIDs, err := dm.blockIDsForPage(db, source, notebook, section, page)
	if err != nil {
		return nil, err
	}

	// 2. Leg 1: page-links via the indexed reverse lookup.
	pageLinks, err := dm.legPageLinks(db, source, notebook, section, page)
	if err != nil {
		return nil, err
	}

	// 3. Legs 2+3: block-refs and embeds via the indexed block_references
	// reverse lookup (#704).
	blockRefs, embeds, err := dm.legBlockRefsAndEmbeds(db, targetBlockIDs)
	if err != nil {
		return nil, err
	}

	// 4. Merge, dedupe (no sort — caller decides).
	seen := make(map[backlinkKey]bool)
	var out []Backlink
	add := func(b Backlink) {
		k := backlinkKey{b.Kind, b.Source, b.SourceNotebook, b.SourceSection, b.SourcePage, b.SourceBlockID}
		if seen[k] {
			return
		}
		seen[k] = true
		out = append(out, b)
	}
	for _, b := range pageLinks {
		add(b)
	}
	for _, b := range blockRefs {
		add(b)
	}
	for _, b := range embeds {
		add(b)
	}
	return out, nil
}

// blockIDsForPage collects every block ID for a (source, notebook, section, page).
// Done under the caller's handle lease so it never re-enters handle().
func (dm *DatabaseManager) blockIDsForPage(db *sql.DB, source, notebook, section, page string) ([]string, error) {
	rows, err := db.Query(
		"SELECT id FROM blocks WHERE source = ? AND notebook = ? AND section = ? AND page = ?",
		source, notebook, section, page,
	)
	if err != nil {
		return nil, fmt.Errorf("get backlinks: target block ids: %w", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("get backlinks: scan block id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// legPageLinks resolves page-link backlinks using the indexed candidates path:
// LinkTargetRawCandidates → listPageLinksByTargetRaws → ResolvePageLinkAgainst
// to gate on nonambiguous canonical targets. Batch-fetches block info for
// source discriminator and clean_content snippet. Runs under the caller's
// handle lease so it never re-enters handle().
func (dm *DatabaseManager) legPageLinks(db *sql.DB, source, notebook, section, page string) ([]Backlink, error) {
	candidates := LinkTargetRawCandidates([]LinkTargetSpec{
		{Source: source, Notebook: notebook, Section: section, Page: page},
	})
	rows, err := listPageLinksByTargetRaws(db, candidates)
	if err != nil {
		return nil, fmt.Errorf("get backlinks: page links: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}

	// Resolve-gate: only include rows whose target_raw nonambiguously resolves
	// to the requested target page. Same contract as the rename-rewrite path.
	pages, err := listDistinctPages(db)
	if err != nil {
		return nil, fmt.Errorf("get backlinks: list pages: %w", err)
	}

	// Batch-fetch block info for all distinct source_block_ids.
	type blockInfo struct {
		linkSource string
		cleanText  string
	}
	blockIDSet := make(map[string]bool, len(rows))
	blockIDList := make([]string, 0, len(rows))
	for _, r := range rows {
		if !blockIDSet[r.SourceBlockID] {
			blockIDSet[r.SourceBlockID] = true
			blockIDList = append(blockIDList, r.SourceBlockID)
		}
	}
	blocksByID := make(map[string]blockInfo, len(blockIDList))
	if len(blockIDList) > 0 {
		placeholders := make([]string, len(blockIDList))
		args := make([]any, len(blockIDList))
		for i, id := range blockIDList {
			placeholders[i] = "?"
			args[i] = id
		}
		q := "SELECT id, COALESCE(source,'vault'), COALESCE(clean_content,'') FROM blocks WHERE id IN (" +
			strings.Join(placeholders, ",") + ")"
		bRows, err := db.Query(q, args...)
		if err != nil {
			return nil, fmt.Errorf("get backlinks: block info: %w", err)
		}
		for bRows.Next() {
			var id, src, clean string
			if err := bRows.Scan(&id, &src, &clean); err != nil {
				bRows.Close()
				return nil, fmt.Errorf("get backlinks: scan block info: %w", err)
			}
			blocksByID[id] = blockInfo{src, clean}
		}
		bRows.Close()
		if err := bRows.Err(); err != nil {
			return nil, fmt.Errorf("get backlinks: iterate block info: %w", err)
		}
	}

	var out []Backlink
	for _, r := range rows {
		ref := ResolvePageLinkAgainst(r.TargetRaw, pages)
		if !ref.Exists || ref.Ambiguous {
			continue
		}
		if ref.Source != source || ref.Notebook != notebook || ref.Section != section || ref.Page != page {
			continue
		}
		info := blocksByID[r.SourceBlockID]
		// Contextual snippet: center on the [[target]] occurrence in clean_content.
		linkToken := "[[" + r.TargetRaw
		out = append(out, Backlink{
			Kind:           BacklinkPageLink,
			Source:         info.linkSource,
			SourceNotebook: r.SourceNotebook,
			SourceSection:  r.SourceSection,
			SourcePage:     r.SourcePage,
			SourceBlockID:  r.SourceBlockID,
			Snippet:        snippet(info.cleanText, linkToken),
		})
	}
	return out, nil
}

// legBlockRefsAndEmbeds finds source blocks whose RawText references any of
// the target page's block IDs via ((uuid)) or {{embed:uuid}}. Returns two
// separate slices (block-refs vs embeds). The lookup goes through the
// derived block_references reverse index (#704): an indexed SEARCH against
// idx_block_references_target replaces the prior leading-wildcard
// raw_content LIKE scan, so cost is proportional to inbound edges of the
// target page's blocks, not total block count.
//
// Each edge row is created by an exact regex match in the indexer or
// backfill, so substring false positives are structurally impossible — no
// per-row token rescanning is needed (the prior LIKE path did, because one
// OR-clause hit could return a row that incidentally contained unrelated
// tokens). The snippet token is reconstructed from target_block_id + kind
// so the existing snippet() helper keeps its current contextual behavior.
//
// Batched to stay well under SQLite's bind limit (each UUID is one bind
// arg). The join against blocks is structural — FK ON for the source side
// guarantees every edge resolves to a source row.
func (dm *DatabaseManager) legBlockRefsAndEmbeds(db *sql.DB, targetBlockIDs []string) ([]Backlink, []Backlink, error) {
	if len(targetBlockIDs) == 0 {
		return nil, nil, nil
	}

	// Batch the IN (?, ?, ...) clause to stay under SQLite's variable limit.
	// Each UUID contributes 1 bind arg, so 500 is comfortably under any
	// modernc.org/sqlite build (default 32766; legacy 999).
	const batchSize = 500

	type edgeRow struct {
		sourceID, targetID, kind, src, nb, sec, pg, clean string
	}
	var edges []edgeRow
	for start := 0; start < len(targetBlockIDs); start += batchSize {
		end := start + batchSize
		if end > len(targetBlockIDs) {
			end = len(targetBlockIDs)
		}
		batch := targetBlockIDs[start:end]

		placeholders := make([]string, len(batch))
		args := make([]any, len(batch))
		for i, id := range batch {
			placeholders[i] = "?"
			args[i] = id
		}
		q := "SELECT br.source_block_id, br.target_block_id, br.kind, " +
			"COALESCE(b.source,'vault'), b.notebook, b.section, b.page, " +
			"COALESCE(b.clean_content,'') " +
			"FROM block_references br " +
			"JOIN blocks b ON b.id = br.source_block_id " +
			"WHERE br.target_block_id IN (" + strings.Join(placeholders, ",") + ")"

		rows, err := db.Query(q, args...)
		if err != nil {
			return nil, nil, fmt.Errorf("get backlinks: block refs/embeds: %w", err)
		}
		for rows.Next() {
			var e edgeRow
			if err := rows.Scan(&e.sourceID, &e.targetID, &e.kind, &e.src, &e.nb, &e.sec, &e.pg, &e.clean); err != nil {
				rows.Close()
				return nil, nil, fmt.Errorf("get backlinks: scan block ref: %w", err)
			}
			edges = append(edges, e)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, nil, fmt.Errorf("get backlinks: iterate block refs: %w", err)
		}
		rows.Close()
	}

	var blockRefs, embeds []Backlink
	for _, e := range edges {
		switch BacklinkKind(e.kind) {
		case BacklinkBlockRef:
			blockRefs = append(blockRefs, Backlink{
				Kind:           BacklinkBlockRef,
				Source:         e.src,
				SourceNotebook: e.nb,
				SourceSection:  e.sec,
				SourcePage:     e.pg,
				SourceBlockID:  e.sourceID,
				Snippet:        snippet(e.clean, "(("+e.targetID+"))"),
			})
		case BacklinkEmbed:
			embeds = append(embeds, Backlink{
				Kind:           BacklinkEmbed,
				Source:         e.src,
				SourceNotebook: e.nb,
				SourceSection:  e.sec,
				SourcePage:     e.pg,
				SourceBlockID:  e.sourceID,
				Snippet:        snippet(e.clean, "{{embed:"+e.targetID+"}}"),
			})
		}
	}
	return blockRefs, embeds, nil
}

// snippet returns a contextual 120-rune excerpt of text centered on the first
// occurrence of token (the page-link / block-ref / embed syntax). When token
// is absent from text, falls back to a plain prefix. The text argument is
// always the source block's clean_content — the ((uuid)) / {{embed:uuid}}
// syntax survives cleaning (verified at parser.go:1048 BlockRefRegex against
// CleanText), and clean_content is what users see in the rendered panel, so
// the snippet matches the visible context.
//
// For page-link tokens (starting with [[), the snippet boundary is extended to
// include the closing ]] so the link is never sliced mid-syntax. If the full
// [[...]] token exceeds the display budget, the token is replaced with a
// safe elided representation "[[…]]" and context is extracted around that.
// The total output never exceeds backlinkSnippetRunes (120) runes including
// ellipsis markers.
func snippet(text, token string) string {
	if text == "" {
		return ""
	}
	budget := backlinkSnippetRunes
	if utf8.RuneCountInString(text) <= budget {
		return text
	}
	runes := []rune(text)

	// Try to find the token and extract context around it.
	if token != "" {
		tokenRunes := []rune(token)
		tokenLower := strings.ToLower(token)
		textLower := strings.ToLower(text)
		runesLower := []rune(textLower)
		idx := runeIndex(runesLower, []rune(tokenLower))
		if idx >= 0 {
			effectiveLen := len(tokenRunes)

			// For page-link tokens ([[...), extend to include the closing ]]
			// so the link is never sliced mid-syntax.
			if strings.HasPrefix(token, "[[") {
				closeIdx := runeIndex(runes[idx+effectiveLen:], []rune("]]"))
				if closeIdx >= 0 {
					effectiveLen += closeIdx + 2 // +2 for ]]
				}
			}

			// Determine whether we need ellipsis markers.
			wantPrefix := idx > 0
			wantSuffix := idx+effectiveLen < len(runes)
			markerSlots := 0
			if wantPrefix {
				markerSlots++
			}
			if wantSuffix {
				markerSlots++
			}
			contentBudget := budget - markerSlots
			if contentBudget < 0 {
				contentBudget = 0
			}

			// Oversized wiki-link token: replace with elided form "[[…]]"
			// and extract context around the elided placeholder.
			if strings.HasPrefix(token, "[[") && effectiveLen > contentBudget {
				elided := "[[" + snippetEllipsis + "]]" // 4 runes
				elidedRunes := []rune(elided)
				// Build new text: before + elided + after-token text.
				replacement := make([]rune, 0, len(runes)-effectiveLen+4)
				replacement = append(replacement, runes[:idx]...)
				replacement = append(replacement, elidedRunes...)
				replacement = append(replacement, runes[idx+effectiveLen:]...)
				return snippet(string(replacement), elided)
			}

			// Distribute padding around the token within contentBudget.
			pad := contentBudget - effectiveLen
			if pad < 0 {
				pad = 0
			}
			before := pad / 2
			after := pad - before
			start := idx - before
			if start < 0 {
				start = 0
				after += before
			}
			end := idx + effectiveLen + after
			if end > len(runes) {
				end = len(runes)
			}
			// Final cap: content must fit within budget minus markers.
			if end-start > contentBudget {
				excess := (end - start) - contentBudget
				end -= excess
			}

			prefix := ""
			suffix := ""
			if start > 0 {
				prefix = snippetEllipsis
			}
			if end < len(runes) {
				suffix = snippetEllipsis
			}
			return prefix + string(runes[start:end]) + suffix
		}
	}

	// Fallback: plain prefix, reserving one rune for the ellipsis marker.
	return string(runes[:budget-1]) + snippetEllipsis
}

// runeIndex returns the index of the first occurrence of needle in haystack,
// or -1 if not found.
func runeIndex(haystack, needle []rune) int {
	if len(needle) == 0 {
		return 0
	}
	if len(needle) > len(haystack) {
		return -1
	}
	for i := 0; i <= len(haystack)-len(needle); i++ {
		match := true
		for j := 0; j < len(needle); j++ {
			if haystack[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// sortBacklinks stably sorts backlinks by (source, source_notebook, source_section,
// source_page, kind, source_block_id) for deterministic cursor-paged rendering.
// Source is included so cursor positions are unique across linked notebooks.
func sortBacklinks(bl []Backlink) {
	sort.SliceStable(bl, func(i, j int) bool {
		a, b := bl[i], bl[j]
		if a.Source != b.Source {
			return a.Source < b.Source
		}
		if a.SourceNotebook != b.SourceNotebook {
			return a.SourceNotebook < b.SourceNotebook
		}
		if a.SourceSection != b.SourceSection {
			return a.SourceSection < b.SourceSection
		}
		if a.SourcePage != b.SourcePage {
			return a.SourcePage < b.SourcePage
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		return a.SourceBlockID < b.SourceBlockID
	})
}

// backlinkCursorKey extracts the sort-key tuple from a Backlink for cursor
// encoding/positioning.
func backlinkCursorKey(b Backlink) string {
	return b.Source + cursorSep +
		b.SourceNotebook + cursorSep +
		b.SourceSection + cursorSep +
		b.SourcePage + cursorSep +
		string(b.Kind) + cursorSep +
		b.SourceBlockID
}

// encodeBacklinkCursor encodes a backlink's sort key as an opaque base64
// string suitable for returning to the caller and feeding back as the cursor
// argument on the next page request.
func encodeBacklinkCursor(b Backlink) string {
	return base64.RawURLEncoding.EncodeToString([]byte(backlinkCursorKey(b)))
}

// decodeBacklinkCursor decodes an opaque cursor string into its sort-key
// tuple. Returns ("", false) for empty/invalid cursors so the caller treats
// them as "start from the beginning".
func decodeBacklinkCursor(cursor string) (string, bool) {
	if cursor == "" {
		return "", false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return "", false
	}
	return string(decoded), true
}
