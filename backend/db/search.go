package db

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"silt/backend/parser"
)

// searchMaxPerGroup caps how many hits SearchBlocks returns from any single
// page (notebook/section/page) before moving on, so one verbose page can't
// monopolize the result list. Tunable; 3 keeps the modal diverse.
const searchMaxPerGroup = 3

// SearchFilters narrows a SearchBlocksPaged query (#186 global enhancements).
// Empty fields mean "no filter on this dimension". Sort is "relevance" (bm25,
// the default) or "recency" (file_date DESC, a per-block date proxy — blocks
// carry their own date in the identity comment). VaultOnly scopes the search
// to in-vault blocks (excludes linked-notebook sources) — the scope control
// on the global search.
type SearchFilters struct {
	Notebook  string `json:"notebook"`
	Section   string `json:"section"`
	Tag       string `json:"tag"`
	Type      string `json:"type"`      // TASK / NOTE / HEADER / CODE / TABLE / DETAILS / CALLOUT
	Sort      string `json:"sort"`      // "" | "relevance" | "recency"
	VaultOnly bool   `json:"vaultOnly"` // exclude linked-notebook sources (source != 'vault')
}

// searchTerms extracts the usable search terms from a free-text query:
// Unicode letters and digits only (mirroring the unicode61 tokenizer), terms
// under 2 bytes dropped. Shared by the FTS body query (buildFTSQuery) and
// the page-title matcher (searchPageTitlesOn) so the two paths cannot drift
// on what counts as a term — a title match must imply the same AND-of-terms
// contract a body match satisfies.
func searchTerms(query string) []string {
	var terms []string
	for _, w := range strings.Fields(query) {
		clean := strings.Map(func(r rune) rune {
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				return r
			}
			return -1
		}, w)
		if len(clean) < 2 {
			continue
		}
		terms = append(terms, clean)
	}
	return terms
}

// buildFTSQuery turns a free-text user query into a safe FTS5 MATCH
// expression. The index uses tokenize='unicode61', so non-ASCII content
// (CJK, accented Latin, Cyrillic, …) IS tokenized and searchable — the query
// builder must not strip those code points. We keep Unicode letters and
// digits (covering every script unicode61 would treat as word characters)
// and drop everything else, which removes ALL FTS5 query-syntax characters
// (`"`, `*`, `(`, `)`, `:`, `^`, `-` are ASCII punctuation, never letters or
// digits). Each surviving term gets a trailing `*` for prefix matching
// (closer to the old LIKE %term% feel than bare-token exact matching). Terms
// are space-joined → FTS5 implicit AND. Returns "" when no usable terms
// survive, which the caller treats as "no search".
func buildFTSQuery(query string) string {
	terms := searchTerms(query)
	if len(terms) == 0 {
		return ""
	}
	parts := make([]string, len(terms))
	for i, t := range terms {
		parts[i] = t + "*"
	}
	return strings.Join(parts, " ")
}

// SearchBlocks searches indexed blocks via the FTS5 virtual table, ranked by
// bm25 relevance, with highlighted snippets and per-page grouping. It is a
// thin wrapper over SearchBlocksPaged returning the first page (offset 0,
// limit 50) for backwards compatibility with the original single-shot binding.
func (dm *DatabaseManager) SearchBlocks(query string) ([]parser.TaskResult, error) {
	res, err := dm.SearchBlocksPaged(query, 0, 50, SearchFilters{})
	if err != nil {
		return nil, err
	}
	return res.Results, nil
}

// searchFlatCap bounds the flat ranked fetch that feeds the Go-side per-page
// grouping. A common-term query on a large vault can match many blocks; this
// cap keeps the query fast and memory bounded while still returning far more
// than any reasonable modal needs to display. (FTS5 helper functions like
// snippet()/bm25() only work when the FTS table is in the direct FROM clause,
// so per-page grouping via a window-function subquery is not possible — we
// group in Go instead.)
const searchFlatCap = 500

// SearchBlocksPaged runs the FTS5 search and returns a ranked, paginated
// envelope with highlighted snippets, the total match count, and a HasMore
// flag so the frontend knows whether to fetch the next page.
//
// The query selects flat bm25-ranked matches with snippet() highlights (capped
// at searchFlatCap rows). Per-page grouping (top searchMaxPerGroup hits per
// notebook/section/page) is applied in Go because FTS5 helper functions
// cannot survive a window-function subquery wrap. Tag hydration is a single
// secondary SELECT (no N+1).
func (dm *DatabaseManager) SearchBlocksPaged(query string, offset, limit int, filters SearchFilters) (parser.SearchResult, error) {
	db, release, err := dm.handle()
	if err != nil {
		return parser.SearchResult{}, ErrDBClosed
	}
	defer release()
	query = strings.TrimSpace(query)
	if query == "" || offset < 0 || limit <= 0 {
		return parser.SearchResult{Results: []parser.TaskResult{}, Total: 0, Offset: offset, Limit: limit}, nil
	}
	terms := searchTerms(query)
	if len(terms) == 0 {
		return parser.SearchResult{Results: []parser.TaskResult{}, Total: 0, Offset: offset, Limit: limit}, nil
	}
	fts := buildFTSQuery(query)

	// Build the dynamic WHERE + ORDER BY from the filters. All user input is
	// parameterized (never string-interpolated) — a synced vault's tag/notebook
	// name can't inject SQL.
	var where []string
	var args []any
	where = append(where, "blocks_fts MATCH ?")
	args = append(args, fts)
	if filters.Notebook != "" {
		where = append(where, "b.notebook = ?")
		args = append(args, filters.Notebook)
	}
	if filters.Section != "" {
		where = append(where, "b.section = ?")
		args = append(args, filters.Section)
	}
	if filters.Type != "" {
		where = append(where, "b.type = ?")
		args = append(args, filters.Type)
	}
	if filters.Tag != "" {
		// Match the exact tag OR any descendant (hierarchical: "work" matches
		// "work/project"). IN-subquery keeps it one clause. Escape LIKE
		// wildcards (_ and %) in the tag so literal underscores in tag names
		// don't over-match (e.g. "sprint_17" wouldn't match "sprintX17").
		escTag := strings.NewReplacer(`\`, `\\`, `_`, `\_`, `%`, `\%`).Replace(filters.Tag)
		where = append(where, "b.id IN (SELECT block_id FROM tags WHERE raw_path = ? OR raw_path LIKE ? ESCAPE '\\')")
		args = append(args, filters.Tag, escTag+"/%")
	}
	if filters.VaultOnly {
		// Scope: in-vault blocks only (exclude linked-notebook sources).
		where = append(where, "b.source = 'vault'")
	}
	// The SELECT always includes bm25 for scoring; only the ORDER BY varies.
	// (DESC belongs in ORDER BY, never in SELECT.)
	order := "rank"
	if filters.Sort == "recency" {
		order = "b.file_date DESC, rank"
	}

	pageQuery := `
		SELECT b.id, b.parent_id, b.source, b.notebook, b.section, b.page, b.file_date, b.depth,
		       b.raw_content, b.clean_content, b.line_number,
		       COALESCE(t.status, ''), COALESCE(t.owner, ''), COALESCE(t.start_date, ''),
		       COALESCE(t.due_date, ''), COALESCE(t.priority, 0),
		       snippet(blocks_fts, 0, '<mark>', '</mark>', '...', 20),
		       bm25(blocks_fts) AS rank
		FROM blocks_fts
		JOIN blocks b ON b.rowid = blocks_fts.rowid
		LEFT JOIN tasks t ON b.id = t.block_id
		WHERE ` + strings.Join(where, " AND ") + `
		ORDER BY ` + order + `
		LIMIT ?`
	args = append(args, searchFlatCap)
	rows, err := db.Query(pageQuery, args...)
	if err != nil {
		return parser.SearchResult{}, fmt.Errorf("failed to search blocks: %w", err)
	}

	var flat []parser.TaskResult
	for rows.Next() {
		var r parser.TaskResult
		var parentID sql.NullString
		var status, owner, start, due string
		var priority int
		var rank float64
		if err := rows.Scan(
			&r.ID, &parentID, &r.Source, &r.Notebook, &r.Section, &r.Page, &r.FileDate, &r.Depth,
			&r.RawContent, &r.CleanContent, &r.LineNumber,
			&status, &owner, &start, &due, &priority,
			&r.Snippet, &rank,
		); err != nil {
			rows.Close()
			return parser.SearchResult{}, err
		}
		if parentID.Valid {
			r.ParentID = parentID.String
		}
		r.Status = status
		r.Owner = owner
		r.StartDate = start
		r.DueDate = due
		r.Priority = priority
		flat = append(flat, r)
	}
	if err := rows.Err(); err != nil {
		return parser.SearchResult{}, fmt.Errorf("failed iterating search results: %w", err)
	}
	if err := rows.Close(); err != nil {
		return parser.SearchResult{}, err
	}

	// Per-page grouping: keep at most searchMaxPerGroup hits per
	// (notebook, section, page), preserving the bm25 rank order from SQL.
	grouped := make([]parser.TaskResult, 0, len(flat))
	perPage := make(map[string]int, len(flat))
	for _, r := range flat {
		key := r.Notebook + "\x00" + r.Section + "\x00" + r.Page
		if perPage[key] >= searchMaxPerGroup {
			continue
		}
		perPage[key]++
		grouped = append(grouped, r)
	}
	// Page-title matches: a page NAMED for the query is the strongest
	// relevance signal, so matching pages surface as page-level rows ranked
	// above every body hit — including when no block body matches at all,
	// which is what keeps an empty (block-less) page findable by name.
	// Merged in Go, not SQL: the body leg is an FTS5 query whose helper
	// functions (snippet/bm25) only survive in the direct FROM clause, so a
	// UNION would degrade both legs. Block-scoped filters (type, tag)
	// suppress the page leg — a page-level row cannot satisfy them.
	var titleHits []parser.TaskResult
	if filters.Type == "" && filters.Tag == "" {
		var err error
		titleHits, err = searchPageTitlesOn(db, terms, filters)
		if err != nil {
			return parser.SearchResult{}, err
		}
	}
	grouped = append(titleHits, grouped...)
	total := len(grouped)

	end := offset + limit
	if end > total {
		end = total
	}
	var page []parser.TaskResult
	if offset < total {
		page = grouped[offset:end]
	}
	if page == nil {
		page = []parser.TaskResult{}
	}

	// Tag hydration for just this page (single secondary SELECT). Page-level
	// title rows carry no block id and therefore no tags.
	if len(page) > 0 {
		blockIDs := make([]interface{}, 0, len(page))
		for _, r := range page {
			if r.ID != "" {
				blockIDs = append(blockIDs, r.ID)
			}
		}
		if len(blockIDs) > 0 {
			placeholderCount := len(blockIDs)
			placeholders := make([]string, placeholderCount)
			for i := range placeholders {
				placeholders[i] = "?"
			}
			tagQuery := "SELECT block_id, raw_path FROM tags WHERE block_id IN (" + strings.Join(placeholders, ",") + ") ORDER BY block_id, raw_path"
			tagRows, err := db.Query(tagQuery, blockIDs...)
			if err != nil {
				return parser.SearchResult{}, fmt.Errorf("failed to query search tags: %w", err)
			}
			tagIndex := make(map[string][]string, len(page))
			for tagRows.Next() {
				var blockID, tag string
				if err := tagRows.Scan(&blockID, &tag); err != nil {
					tagRows.Close()
					return parser.SearchResult{}, err
				}
				tagIndex[blockID] = append(tagIndex[blockID], tag)
			}
			if err := tagRows.Close(); err != nil {
				return parser.SearchResult{}, err
			}
			for i := range page {
				if tags, ok := tagIndex[page[i].ID]; ok {
					page[i].Tags = tags
				}
			}
		}
	}

	return parser.SearchResult{
		Results: page,
		Total:   total,
		Offset:  offset,
		Limit:   limit,
		HasMore: end < total,
	}, nil
}

// searchPageTitlesOn returns one page-level result per page whose NAME
// contains every term, case-insensitive, AND semantics — the same term
// extraction and combination contract the FTS body query applies, so a title
// hit and a body hit always answer the same question. Terms are
// letters/digits only, so LIKE wildcards cannot occur inside them and the
// pattern needs no escaping. SQLite's LOWER is ASCII-only — the same
// case-folding approximation the rename lookup (page_links) already relies
// on; CJK and already-lowercase names match on literal code points.
// Notebook/section/scope filters narrow the page leg exactly as they narrow
// the body leg. Results carry no block identity (ID stays empty — the marker
// the frontend uses to render a page-navigation row), the page name as
// content, and a highlighted-title snippet. Bounded by searchFlatCap and
// ordered (notebook, section, page) so paging over title hits is
// deterministic.
func searchPageTitlesOn(db *sql.DB, terms []string, filters SearchFilters) ([]parser.TaskResult, error) {
	var where []string
	var args []any
	for _, t := range terms {
		where = append(where, "LOWER(page) LIKE ?")
		args = append(args, "%"+strings.ToLower(t)+"%")
	}
	if filters.Notebook != "" {
		where = append(where, "notebook = ?")
		args = append(args, filters.Notebook)
	}
	if filters.Section != "" {
		where = append(where, "section = ?")
		args = append(args, filters.Section)
	}
	if filters.VaultOnly {
		where = append(where, "source = 'vault'")
	}
	args = append(args, searchFlatCap)
	rows, err := db.Query(`
		SELECT COALESCE(source, 'vault'), notebook, section, page, date
		FROM page_core
		WHERE `+strings.Join(where, " AND ")+`
		ORDER BY notebook, section, page
		LIMIT ?`, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to search page titles: %w", err)
	}
	defer rows.Close()
	var out []parser.TaskResult
	for rows.Next() {
		var r parser.TaskResult
		if err := rows.Scan(&r.Source, &r.Notebook, &r.Section, &r.Page, &r.FileDate); err != nil {
			return nil, err
		}
		r.CleanContent = r.Page
		r.Snippet = titleSnippet(r.Page, terms)
		out = append(out, r)
	}
	return out, rows.Err()
}

// titleSnippet renders a page name as a search snippet with <mark> ranges
// around term occurrences (longest-first so one term cannot chop a longer
// term that starts at the same rune). Matching runs per-rune so byte
// indices of the original title stay valid even when full-string lowering
// would change its length (e.g. 'İ'); display keeps the original casing.
func titleSnippet(title string, terms []string) string {
	termRunes := make([][]rune, 0, len(terms))
	for _, t := range terms {
		termRunes = append(termRunes, []rune(strings.ToLower(t)))
	}
	sort.Slice(termRunes, func(i, j int) bool { return len(termRunes[i]) > len(termRunes[j]) })

	orig := []rune(title)
	lower := make([]rune, len(orig))
	for i, r := range orig {
		lower[i] = unicode.ToLower(r)
	}
	var b strings.Builder
	i := 0
	for i < len(orig) {
		m := 0
		for _, tr := range termRunes {
			if runeHasPrefix(lower[i:], tr) {
				m = len(tr)
				break
			}
		}
		if m > 0 {
			b.WriteString("<mark>")
			b.WriteString(string(orig[i : i+m]))
			b.WriteString("</mark>")
			i += m
			continue
		}
		b.WriteRune(orig[i])
		i++
	}
	return b.String()
}

func runeHasPrefix(s, p []rune) bool {
	if len(p) > len(s) {
		return false
	}
	for i, r := range p {
		if s[i] != r {
			return false
		}
	}
	return true
}
