package db

import (
	"database/sql"
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

const backlinkSnippetRunes = 120

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
// use parameterized LIKE candidates safely (UUIDs contain no LIKE-special
// chars). Results are deduped and stably sorted by (source_notebook,
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

	// 3. Legs 2+3: block-refs and embeds via parameterized LIKE.
	blockRefs, embeds, err := dm.legBlockRefsAndEmbeds(db, targetBlockIDs)
	if err != nil {
		return nil, err
	}

	// 4. Merge, dedupe, stable sort.
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
	sortBacklinks(out)
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
// LinkTargetRawCandidates → ListPageLinksByTargetRaws → ResolvePageLinkAgainst
// to gate on nonambiguous canonical targets. Batch-fetches block info for
// source discriminator and clean_content snippet.
func (dm *DatabaseManager) legPageLinks(db *sql.DB, source, notebook, section, page string) ([]Backlink, error) {
	candidates := LinkTargetRawCandidates([]struct{ Notebook, Section, Page string }{
		{notebook, section, page},
	})
	rows, err := dm.ListPageLinksByTargetRaws(candidates)
	if err != nil {
		return nil, fmt.Errorf("get backlinks: page links: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}

	// Resolve-gate: only include rows whose target_raw nonambiguously resolves
	// to the requested target page. Same contract as the rename-rewrite path.
	pages, err := dm.ListDistinctPages()
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
		out = append(out, Backlink{
			Kind:           BacklinkPageLink,
			Source:         info.linkSource,
			SourceNotebook: r.SourceNotebook,
			SourceSection:  r.SourceSection,
			SourcePage:     r.SourcePage,
			SourceBlockID:  r.SourceBlockID,
			Snippet:        snippet(info.cleanText),
		})
	}
	return out, nil
}

// legBlockRefsAndEmbeds finds blocks whose raw_content contains ((targetID))
// or {{embed:targetID}} for any of the target page's block IDs. Returns two
// separate slices (block-refs vs embeds). Each UUID is 36 hex chars with no
// LIKE-special characters, so the pattern construction is safe without escaping.
func (dm *DatabaseManager) legBlockRefsAndEmbeds(db *sql.DB, targetBlockIDs []string) ([]Backlink, []Backlink, error) {
	if len(targetBlockIDs) == 0 {
		return nil, nil, nil
	}

	// Batch OR-clause LIKE conditions to stay under SQLite's 999 variable limit.
	// Each UUID contributes 2 bind args (one per leg).
	const batchSize = 400

	type rawHit struct {
		id, src, nb, sec, pg, cleanContent, rawContent string
	}

	var hits []rawHit
	for start := 0; start < len(targetBlockIDs); start += batchSize {
		end := start + batchSize
		if end > len(targetBlockIDs) {
			end = len(targetBlockIDs)
		}
		batch := targetBlockIDs[start:end]

		orParts := make([]string, 0, len(batch)*2)
		args := make([]any, 0, len(batch)*2)
		for _, uuid := range batch {
			orParts = append(orParts, "raw_content LIKE ?")
			args = append(args, "%(("+uuid+"))%")
			orParts = append(orParts, "raw_content LIKE ?")
			args = append(args, "%{{embed:"+uuid+"}}%")
		}
		q := "SELECT id, COALESCE(source,'vault'), notebook, section, page, " +
			"COALESCE(clean_content,''), raw_content FROM blocks WHERE " +
			strings.Join(orParts, " OR ")

		rows, err := db.Query(q, args...)
		if err != nil {
			return nil, nil, fmt.Errorf("get backlinks: block refs/embeds: %w", err)
		}
		for rows.Next() {
			var h rawHit
			if err := rows.Scan(&h.id, &h.src, &h.nb, &h.sec, &h.pg, &h.cleanContent, &h.rawContent); err != nil {
				rows.Close()
				return nil, nil, fmt.Errorf("get backlinks: scan block ref: %w", err)
			}
			hits = append(hits, h)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, nil, fmt.Errorf("get backlinks: iterate block refs: %w", err)
		}
		rows.Close()
	}

	// Classify each hit against the exact target tokens. A block that
	// matched LIKE for one target UUID must be checked against ALL target
	// tokens to avoid false positives (e.g. a row matched for {{embed:uuidA}}
	// may also contain ((uuidB)) for an unrelated block-ref).
	targetRefTokens := make(map[string]bool, len(targetBlockIDs))
	targetEmbedTokens := make(map[string]bool, len(targetBlockIDs))
	for _, id := range targetBlockIDs {
		targetRefTokens["(("+id+"))"] = true
		targetEmbedTokens["{{embed:"+id+"}}"] = true
	}

	var blockRefs, embeds []Backlink
	for _, h := range hits {
		hasRef := false
		hasEmbed := false
		for token := range targetRefTokens {
			if strings.Contains(h.rawContent, token) {
				hasRef = true
				break
			}
		}
		for token := range targetEmbedTokens {
			if strings.Contains(h.rawContent, token) {
				hasEmbed = true
				break
			}
		}
		if hasRef {
			blockRefs = append(blockRefs, Backlink{
				Kind:           BacklinkBlockRef,
				Source:         h.src,
				SourceNotebook: h.nb,
				SourceSection:  h.sec,
				SourcePage:     h.pg,
				SourceBlockID:  h.id,
				Snippet:        snippet(h.cleanContent),
			})
		}
		if hasEmbed {
			embeds = append(embeds, Backlink{
				Kind:           BacklinkEmbed,
				Source:         h.src,
				SourceNotebook: h.nb,
				SourceSection:  h.sec,
				SourcePage:     h.pg,
				SourceBlockID:  h.id,
				Snippet:        snippet(h.cleanContent),
			})
		}
	}
	return blockRefs, embeds, nil
}

// snippet returns a 120-rune prefix of text for the backlinks panel preview.
// Uses clean_content (not raw) so markdown syntax doesn't clutter the snippet.
func snippet(text string) string {
	if text == "" {
		return ""
	}
	if utf8.RuneCountInString(text) <= backlinkSnippetRunes {
		return text
	}
	runes := []rune(text)
	return string(runes[:backlinkSnippetRunes]) + "…"
}

// sortBacklinks stably sorts backlinks by (source_notebook, source_section,
// source_page, kind, source_block_id) for deterministic panel rendering.
func sortBacklinks(bl []Backlink) {
	sort.SliceStable(bl, func(i, j int) bool {
		a, b := bl[i], bl[j]
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
