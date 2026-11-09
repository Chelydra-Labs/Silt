package db

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"
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
//
// The task-attribute filters (Owner/Status/DueBefore/DueAfter/PriorityMin/
// Overdue) are task-scoped: setting any of them restricts results to TASK
// blocks whose tasks projection row satisfies every predicate, and suppresses
// the page-title result leg (a page-level row carries no task attributes).
// Dates are YYYY-MM-DD strings compared lexicographically (= chronological).
type SearchFilters struct {
	Notebook  string `json:"notebook"`
	Section   string `json:"section"`
	Tag       string `json:"tag"`
	Type      string `json:"type"`      // TASK / NOTE / HEADER / CODE / TABLE / DETAILS / CALLOUT
	Sort      string `json:"sort"`      // "" | "relevance" | "recency"
	VaultOnly bool   `json:"vaultOnly"` // exclude linked-notebook sources (source != 'vault')

	// Owner matches the [owner::] token exactly, case-insensitively.
	Owner string `json:"owner"`
	// Status matches the checkbox-driven status: TODO, DOING, or DONE
	// (normalized to upper case server-side).
	Status string `json:"status"`
	// DueBefore / DueAfter bound due_date inclusively (YYYY-MM-DD). A task
	// with no due date never matches a date bound.
	DueBefore string `json:"dueBefore"`
	DueAfter  string `json:"dueAfter"`
	// PriorityMin is an inclusive urgency floor, where 1 is the highest
	// priority and 3 the lowest: a floor of N selects priorities N and
	// everything more urgent. Rows with no recorded priority (NULL column;
	// the COALESCE-0 shape in the result projection) never match — "no
	// priority recorded" is not a priority. The parser's absent-token
	// default (LegacyMissingTaskPriority, stored as 3) is recorded data and
	// is admitted only by the floor of 3, like an explicit P3.
	PriorityMin int `json:"priorityMin"`
	// Overdue selects tasks whose due_date is before today (local) and whose
	// status is not DONE.
	Overdue bool `json:"overdue"`

	// ExcludePageTitles suppresses the page-title leg so every row in the
	// envelope is a block. Block-scoped consumers (the replace preview, the
	// block picker) need this: their offset/limit/total arithmetic assumes a
	// row is a block, and a page row consuming a slot would silently shrink
	// the block budget — for the picker's fixed 50-row window, a vault whose
	// page names share a common fragment can crowd every block out.
	ExcludePageTitles bool `json:"excludePageTitles"`
}

// hasTaskFilters reports whether any task-attribute filter is active — i.e.
// whether the query is restricted to tasks carrying those attributes (which
// also makes page-level title hits inapplicable). Values are tested trimmed
// because the predicates bind trimmed values: an all-whitespace Owner must not
// activate a filter that can then never match.
func (f SearchFilters) hasTaskFilters() bool {
	return strings.TrimSpace(f.Owner) != "" || strings.TrimSpace(f.Status) != "" ||
		strings.TrimSpace(f.DueBefore) != "" || strings.TrimSpace(f.DueAfter) != "" ||
		f.PriorityMin > 0 || f.Overdue
}

// validateTaskFilters rejects malformed date bounds at the boundary. The bounds
// are compared as strings in SQL, so a value like "2020-1-1" would order
// against stored dates lexicographically and return a plausible-looking but
// wrong set — indistinguishable from a legitimate empty match. Parsing keeps
// the YYYY-MM-DD contract enforced by the caller instead of hiding a bad value
// in a silent result.
func (f SearchFilters) validateTaskFilters() error {
	for _, bound := range []struct{ name, value string }{
		{"dueBefore", f.DueBefore},
		{"dueAfter", f.DueAfter},
	} {
		v := strings.TrimSpace(bound.value)
		if v == "" {
			continue
		}
		if _, err := time.Parse("2006-01-02", v); err != nil {
			return fmt.Errorf("invalid %s %q: want YYYY-MM-DD", bound.name, v)
		}
	}
	return nil
}

// appendTaskFilterPredicates is the SINGLE source of the tasks-JOIN WHERE
// clauses for the task-attribute filters. It appends predicates (referencing
// the `tasks t` alias of SearchBlocksPaged's LEFT JOIN) and their bind args,
// so no second SQL path for these filters can grow alongside it. Zero-value
// fields append nothing — the unfiltered query shape is byte-identical to
// the pre-filter SQL. All values are parameterized; only the filter MODE is
// interpolated (fixed strings).
//
// Semantics (documented for the ARCHITECTURE contract and mirrored by tests):
//   - Owner: exact match, case-insensitive for ASCII (both sides lowered in
//     SQL; SQLite's LOWER folds ASCII only, so non-ASCII matches exactly-cased).
//   - Status: exact match after upper-casing the caller's input (TODO/DOING/
//     DONE are stored upper-case by the indexer).
//   - DueBefore/DueAfter: inclusive bounds on due_date; NULL never satisfies
//     a comparison, so a task without a due date is excluded by either bound.
//   - PriorityMin: an urgency floor — 1 is the highest priority, so a floor
//     of N selects priorities N and everything MORE urgent (recorded values
//     1..N; P3/low is admitted only by a floor of 3). `t.priority IS NOT
//     NULL` excludes the no-priority-recorded shapes (NULL column; the
//     COALESCE(t.priority, 0) sentinel the result projection surfaces for
//     non-task rows) — "no priority recorded" is not a priority and must
//     not match any floor. The parser's absent-token default
//     (LegacyMissingTaskPriority, stored as 3) is recorded data,
//     indistinguishable from an explicit P3, and is admitted only by the
//     floor of 3 accordingly.
//   - Overdue: due_date < today (local date, computed by the caller and
//     passed as now) AND status != DONE.
func appendTaskFilterPredicates(where []string, args []any, filters SearchFilters, now time.Time) ([]string, []any) {
	// Trim once and gate on the trimmed value: hasTaskFilters makes the same
	// decision, so a whitespace-only field can neither activate the filter nor
	// bind an empty needle that matches nothing.
	owner := strings.TrimSpace(filters.Owner)
	status := strings.ToUpper(strings.TrimSpace(filters.Status))
	dueBefore := strings.TrimSpace(filters.DueBefore)
	dueAfter := strings.TrimSpace(filters.DueAfter)
	if owner != "" {
		// Lower BOTH sides in SQL: the caller's raw text may carry non-ASCII
		// upper case that Go's ToLower would fold but a Go-lowered needle vs a
		// SQL-lowered haystack could never compare. SQLite's LOWER is ASCII-only,
		// so exact-case non-ASCII now matches; non-ASCII case variants beyond
		// that remain an approximation (see searchPageTitlesOn).
		where = append(where, "LOWER(t.owner) = LOWER(?)")
		args = append(args, owner)
	}
	if status != "" {
		where = append(where, "t.status = ?")
		args = append(args, status)
	}
	if dueBefore != "" {
		where = append(where, "t.due_date IS NOT NULL AND t.due_date <= ?")
		args = append(args, dueBefore)
	}
	if dueAfter != "" {
		where = append(where, "t.due_date IS NOT NULL AND t.due_date >= ?")
		args = append(args, dueAfter)
	}
	if filters.PriorityMin > 0 {
		where = append(where, "t.priority IS NOT NULL AND t.priority <= ?")
		args = append(args, filters.PriorityMin)
	}
	if filters.Overdue {
		where = append(where, "t.due_date IS NOT NULL AND t.due_date < ? AND t.status != 'DONE'")
		args = append(args, now.Format("2006-01-02"))
	}
	return where, args
}

// searchTerms extracts the usable search terms from a free-text query for the
// FTS body leg: Unicode letters and digits only (mirroring the unicode61
// tokenizer), terms under 2 bytes dropped. The page-title leg uses titleTerms,
// which splits on separators instead of squashing them — the body query must
// answer to FTS tokenization, a page name must answer to what the user typed.
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

// titleTerms extracts the page-title leg's terms: the query split on every rune
// that is not a letter or digit. The per-word squash searchTerms applies is
// wrong for this leg — a page NAMED "2026-06-13" or "sprint_17" would never be a
// LIKE candidate for the squashed "20260613"/"sprint17", so typing a page's own
// name could not find it and the exact-name rank tier was unreachable. Splitting
// keeps every part of a separator-bearing name, so each one is found in the
// name (unicode61 tokenizes on exactly those separators for the body leg).
func titleTerms(query string) []string {
	terms := strings.FieldsFunc(query, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	out := make([]string, 0, len(terms))
	for _, t := range terms {
		if len(t) < 2 {
			continue
		}
		out = append(out, t)
	}
	return out
}

// buildFTSQuery turns a free-text user query into a safe FTS5 MATCH
// expression. The index uses tokenize='unicode61', so non-ASCII content
// (CJK, accented Latin, Cyrillic, …) IS tokenized and searchable — the query
// builder must not strip those code points. We keep Unicode letters and
// digits (covering every script unicode61 would treat as word characters)
// and drop everything else, which removes ALL FTS5 query-syntax characters
// (`"`, `*`, `(`, `)`, `:`, `^`, `-` are ASCII punctuation, never letters or
// digits). Each surviving term is quoted before its trailing `*` for prefix
// matching (closer to the old LIKE %term% feel than bare-token exact
// matching). The quotes matter beyond hygiene: FTS5 parses bare uppercase
// OR/AND/NOT as boolean operators, so a page named "OR" — or any query with
// one of those as a term — was a syntax error that aborted the whole search
// before the title leg could find it. Terms are space-joined → FTS5 implicit
// AND. Returns "" when no usable terms survive, which the caller treats as
// "no search".
func buildFTSQuery(query string) string {
	terms := searchTerms(query)
	if len(terms) == 0 {
		return ""
	}
	parts := make([]string, len(terms))
	for i, t := range terms {
		parts[i] = `"` + t + `"*`
	}
	return strings.Join(parts, " ")
}

// SearchBlocks searches indexed blocks via the FTS5 virtual table, ranked by
// bm25 relevance, with highlighted snippets and per-page grouping. It is a
// thin wrapper over SearchBlocksPaged returning the first page (offset 0,
// limit 50) for backwards compatibility with the original single-shot binding.
//
// The contract is BLOCK hits only, and page rows are excluded at the source
// (ExcludePageTitles) so the 50-row window belongs entirely to blocks. Dropping
// them after paging would let page-title hits consume the block budget — with
// enough title hits the picker would come back empty while body matches exist.
// The ID check below stays as defense for consumers that key by block id.
func (dm *DatabaseManager) SearchBlocks(query string) ([]parser.TaskResult, error) {
	res, err := dm.SearchBlocksPaged(query, 0, 50, SearchFilters{ExcludePageTitles: true})
	if err != nil {
		return nil, err
	}
	out := make([]parser.TaskResult, 0, len(res.Results))
	for _, r := range res.Results {
		if r.ID != "" {
			out = append(out, r)
		}
	}
	return out, nil
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
	if err := filters.validateTaskFilters(); err != nil {
		return parser.SearchResult{}, err
	}
	// The body leg is an FTS5 query, so a query with no tokenizable term (e.g.
	// "C++" squashes to a single character) has nothing to match here — but a
	// page may still be NAMED exactly that, so the page-title leg runs either
	// way. Only a totally blank query short-circuits above.
	fts := buildFTSQuery(query)

	var flat []parser.TaskResult
	if fts != "" {
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
		where, args = appendTaskFilterPredicates(where, args, filters, time.Now())
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
			       COALESCE(t.due_date, ''), COALESCE(t.priority, 0), t.shortlist_rank,
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

		for rows.Next() {
			var r parser.TaskResult
			var parentID sql.NullString
			var status, owner, start, due string
			var priority int
			var shortlistRank sql.NullInt64
			var rank float64
			if err := rows.Scan(
				&r.ID, &parentID, &r.Source, &r.Notebook, &r.Section, &r.Page, &r.FileDate, &r.Depth,
				&r.RawContent, &r.CleanContent, &r.LineNumber,
				&status, &owner, &start, &due, &priority, &shortlistRank,
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
			// Shortlist (#1092): NULL stays nil (unranked); 0 is a real
			// top rank (badge parity with the tasks query paths).
			if shortlistRank.Valid {
				sr := int(shortlistRank.Int64)
				r.ShortlistRank = &sr
			}
			flat = append(flat, r)
		}
		if err := rows.Err(); err != nil {
			return parser.SearchResult{}, fmt.Errorf("failed iterating search results: %w", err)
		}
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
	// UNION would degrade both legs. Block-scoped filters (type, tag),
	// task-attribute filters, and the block-scoped ExcludePageTitles switch
	// suppress the page leg — a page-level row carries neither a block type
	// nor task attributes, and a block-scoped consumer budgets rows as blocks.
	var titleHits []parser.TaskResult
	if !filters.ExcludePageTitles && filters.Type == "" && filters.Tag == "" && !filters.hasTaskFilters() {
		var err error
		titleHits, err = searchPageTitlesOn(db, query, filters)
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
// contains every title term, case-insensitive, AND semantics. Terms are the
// query split on separators (see titleTerms), so a page named "2026-06-13" or
// "sprint_17" is found by typing its own name; the squashed body terms would
// match neither the name nor (via FTS) the body. Quotes/`%`/`_` cannot occur
// inside a term, so the patterns need no escaping.
//
// A query whose every part is a single character ("R&D", "Q&A", "C++") yields
// no terms, yet a page may be named exactly that. For those the leg falls back
// to the raw query as a single escaped pattern: typing a page's own name must
// find it. Queries shorter than 2 bytes — a single ASCII character — stay a
// no-op (one keystroke is not a name search); the gate is byte length, so a
// single 2-byte non-ASCII letter (e.g. "Ü") is still searched. The raw query is
// escaped because it carries the user's punctuation, which LIKE would otherwise
// treat as wildcards.
//
// Both sides are lowered in SQL: the comparison is ASCII case-insensitive and
// an exactly-cased non-ASCII name still matches (query "MÜNCHEN" → page
// "MÜNCHEN"). SQLite's LOWER folds ASCII only, so a non-exact-case non-ASCII
// variant ("münchen") does not match — the same approximation the rename
// lookup (page_links) relies on; full Unicode fold parity with SearchPages is
// out of scope here.
// Notebook/section/scope filters narrow the page leg exactly as they narrow
// the body leg. Results carry no block identity (ID stays empty — the marker
// the frontend uses to render a page-navigation row), the page name as
// `clean_content`, and a `<mark>`-highlighted title snippet. Consumers must
// HTML-escape the snippet and then re-allow only `<mark>`: the page name is
// concatenated in raw. Bounded by searchFlatCap and ordered by match quality
// first — exact name, then name prefix, then substring, with the alphabetical
// (notebook, section, page) flow and source as the final tiebreak — so an
// exactly-named page cannot be buried by alphabetically-earlier substring
// matches or pushed past the cap by them. source is the tail tiebreak because
// a vault page and a linked-notebook page can share the exact display path
// (page_core's PK): a tie there would make paging over title hits skip or
// duplicate rows.
func searchPageTitlesOn(db *sql.DB, query string, filters SearchFilters) ([]parser.TaskResult, error) {
	terms := titleTerms(query)
	var where []string
	var args []any
	snippetTerms := terms
	if len(terms) == 0 {
		// See the doc comment: an all-single-char-part name has no term to
		// anchor, so the raw query is the only pattern that can find it.
		if len(query) < 2 {
			return nil, nil
		}
		where = append(where, `LOWER(page) LIKE LOWER(?) ESCAPE '\'`)
		args = append(args, "%"+escapeLikePattern(query)+"%")
		snippetTerms = []string{query}
	}
	for _, t := range terms {
		where = append(where, "LOWER(page) LIKE LOWER(?)")
		args = append(args, "%"+t+"%")
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
	// The rank binds are the raw query, so LIKE metacharacters a user typed
	// must be escaped to stay literal (the page-name patterns above come from
	// titleTerms, which strips punctuation, or from the escaped raw fallback).
	escQuery := escapeLikePattern(query)
	args = append(args, query, escQuery+"%", searchFlatCap)
	rows, err := db.Query(`
		SELECT COALESCE(source, 'vault'), notebook, section, page, date
		FROM page_core
		WHERE `+strings.Join(where, " AND ")+`
		ORDER BY CASE
			WHEN LOWER(page) = LOWER(?) THEN 0
			WHEN LOWER(page) LIKE LOWER(?) ESCAPE '\' THEN 1
			ELSE 2
		END, notebook, section, page, source
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
		r.Snippet = titleSnippet(r.Page, snippetTerms)
		out = append(out, r)
	}
	return out, rows.Err()
}

// escapeLikePattern makes a raw user query literal inside a LIKE pattern (the
// pattern is always used with ESCAPE '\').
func escapeLikePattern(query string) string {
	return strings.NewReplacer(`\`, `\\`, `_`, `\_`, `%`, `\%`).Replace(query)
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
