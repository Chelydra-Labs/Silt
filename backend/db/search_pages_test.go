package db

import (
	"fmt"
	"strings"
	"testing"

	"silt/backend/parser"
)

// seedSearchPages indexes two task pages plus one zero-block page so the
// page-title and task-attribute search tests share a controlled fixture.
//
//	Work/Projects/Db Migrations: Bob's "Postgres cutover" task (overdue TODO),
//	  Alice's "SQLite backup" task (future due, DOING), a plain note.
//	Work/Projects/Meeting Notes: one note body mentioning "agenda".
//	Work/Projects/Quiet Empty Page: ZERO blocks (indexed via the projection
//	  path) — must stay findable by title.
func seedSearchPages(t *testing.T) *DatabaseManager {
	t.Helper()
	dm := newTestDB(t)

	migrations := []parser.ParsedBlock{
		{
			ID: "aaaa1111-0000-4000-8000-000000000001", Type: parser.BlockTask,
			RawText:   "- [ ] Postgres cutover [owner:: Bob] [due:: 2020-01-01] [priority:: 1]",
			CleanText: "Postgres cutover", Status: "TODO", Owner: "Bob",
			DueDate: "2020-01-01", Priority: 1, LineNumber: 1,
		},
		{
			ID: "aaaa1111-0000-4000-8000-000000000002", Type: parser.BlockTask,
			RawText:   "- [/] Postgres backup script [owner:: Alice] [due:: 2030-01-01] [priority:: 2]",
			CleanText: "Postgres backup script", Status: "DOING", Owner: "Alice",
			DueDate: "2030-01-01", Priority: 2, LineNumber: 2,
		},
		{
			ID: "aaaa1111-0000-4000-8000-000000000007", Type: parser.BlockTask,
			RawText:   "- [x] Postgres retro writeup [owner:: Carol] [due:: 2020-02-01] [priority:: 2]",
			CleanText: "Postgres retro writeup", Status: "DONE", Owner: "Carol",
			DueDate: "2020-02-01", Priority: 2, LineNumber: 3,
		},
		{
			ID: "aaaa1111-0000-4000-8000-000000000003", Type: parser.BlockNote,
			RawText: "a plain note about gardening", CleanText: "a plain note about gardening",
			LineNumber: 4,
		},
	}
	if err := dm.IndexFileBlocks("vault", "Work", "Projects", "Db Migrations", migrations, nil); err != nil {
		t.Fatalf("index migrations page: %v", err)
	}

	meeting := []parser.ParsedBlock{
		{
			ID: "aaaa1111-0000-4000-8000-000000000004", Type: parser.BlockNote,
			RawText: "weekly agenda draft", CleanText: "weekly agenda draft", LineNumber: 1,
		},
	}
	if err := dm.IndexFileBlocks("vault", "Work", "Projects", "Meeting Notes", meeting, nil); err != nil {
		t.Fatalf("index meeting page: %v", err)
	}

	// A page NAMED for a term that also occurs in block bodies: the ranking
	// test pins that its page-level title hit outranks the body hits.
	agenda := []parser.ParsedBlock{
		{
			ID: "aaaa1111-0000-4000-8000-000000000006", Type: parser.BlockNote,
			RawText: "agenda topics galore", CleanText: "agenda topics galore", LineNumber: 1,
		},
	}
	if err := dm.IndexFileBlocks("vault", "Work", "Projects", "Agenda Page", agenda, nil); err != nil {
		t.Fatalf("index agenda page: %v", err)
	}

	// Zero-block page: a real .md file whose body has no managed blocks. Only
	// the projection path can register it — this is the findability contract.
	if err := dm.IndexFileWithProjection("vault", "Work", "Projects", "Quiet Empty Page",
		nil, nil, "", nil, PageCoreFields{}); err != nil {
		t.Fatalf("index empty page: %v", err)
	}
	return dm
}

func TestSearch_TitleOnlyMatchNoBodyHit(t *testing.T) {
	dm := seedSearchPages(t)

	// "quiet empty" appears in no block body — only the page NAME carries it.
	res, err := dm.SearchBlocksPaged("quiet empty", 0, 50, SearchFilters{})
	if err != nil {
		t.Fatalf("SearchBlocksPaged: %v", err)
	}
	if res.Total != 1 {
		t.Fatalf("expected the zero-block page to be findable by title, got total=%d: %+v", res.Total, res.Results)
	}
	hit := res.Results[0]
	if hit.ID != "" {
		t.Errorf("page-level result should carry no block id, got %q", hit.ID)
	}
	if hit.Notebook != "Work" || hit.Section != "Projects" || hit.Page != "Quiet Empty Page" {
		t.Errorf("page-level result locator = %+v", hit)
	}
	if !strings.Contains(hit.Snippet, "<mark>Quiet</mark>") || !strings.Contains(hit.Snippet, "<mark>Empty</mark>") {
		t.Errorf("snippet should highlight the matched title terms, got %q", hit.Snippet)
	}
}

func TestSearch_EmptyPageFindableByTitle(t *testing.T) {
	dm := seedSearchPages(t)

	// Single-term query against the empty page's name.
	res, err := dm.SearchBlocksPaged("quiet", 0, 50, SearchFilters{})
	if err != nil {
		t.Fatalf("SearchBlocksPaged: %v", err)
	}
	if res.Total != 1 || res.Results[0].Page != "Quiet Empty Page" {
		t.Fatalf("empty page not findable by single title term: total=%d %+v", res.Total, res.Results)
	}

	// The page inventory must list it too (wiki-link resolution, SearchPages).
	pages, err := dm.ListDistinctPages()
	if err != nil {
		t.Fatalf("ListDistinctPages: %v", err)
	}
	found := false
	for _, p := range pages {
		if p.Page == "Quiet Empty Page" && p.Notebook == "Work" {
			found = true
		}
	}
	if !found {
		t.Errorf("page inventory missing zero-block page: %+v", pages)
	}
}

func TestSearch_TitleMatchRequiresAllTerms(t *testing.T) {
	dm := seedSearchPages(t)

	// "quiet gardening": the empty page matches "quiet" but not "gardening"
	// (AND semantics — a partial title match must not surface the page).
	res, err := dm.SearchBlocksPaged("quiet gardening", 0, 50, SearchFilters{})
	if err != nil {
		t.Fatalf("SearchBlocksPaged: %v", err)
	}
	for _, r := range res.Results {
		if r.ID == "" {
			t.Errorf("page-level row surfaced for a partial title match: %+v", r)
		}
	}
}

func TestSearch_TitleHitRanksAboveBodyHits(t *testing.T) {
	dm := seedSearchPages(t)

	// "agenda" matches the Agenda Page by TITLE and two block BODIES by FTS
	// ("agenda topics galore", "weekly agenda draft"). The page named for the
	// query must come first.
	res, err := dm.SearchBlocksPaged("agenda", 0, 50, SearchFilters{})
	if err != nil {
		t.Fatalf("SearchBlocksPaged: %v", err)
	}
	if res.Total != 3 {
		t.Fatalf("expected 1 title + 2 body hits, got total=%d: %+v", res.Total, res.Results)
	}
	if res.Results[0].ID != "" || res.Results[0].Page != "Agenda Page" {
		t.Errorf("expected page-level title hit first, got %+v", res.Results[0])
	}
}

func TestSearch_TitleHitsSuppressUnderBlockScopedFilters(t *testing.T) {
	dm := seedSearchPages(t)

	// Type filter makes the query block-scoped; the "Meeting Notes" TITLE hit
	// must not leak a page-level row into a TASK-only search.
	res, err := dm.SearchBlocksPaged("notes", 0, 50, SearchFilters{Type: "TASK"})
	if err != nil {
		t.Fatalf("SearchBlocksPaged: %v", err)
	}
	for _, r := range res.Results {
		if r.ID == "" {
			t.Errorf("page-level row leaked under type filter: %+v", r)
		}
	}

	// The unfiltered query DOES surface the title hit — proves the filter,
	// not the fixture, suppressed it.
	plain, err := dm.SearchBlocksPaged("notes", 0, 50, SearchFilters{})
	if err != nil {
		t.Fatalf("SearchBlocksPaged: %v", err)
	}
	if plain.Total == 0 || plain.Results[0].Page != "Meeting Notes" {
		t.Errorf("expected Meeting Notes title hit unfiltered, got %+v", plain.Results)
	}
}

func TestSearch_TitleHitsRespectScopeFilters(t *testing.T) {
	dm := seedSearchPages(t)

	// A same-named page in a linked source: scope filters must narrow the
	// page leg exactly like the body leg.
	linked := []parser.ParsedBlock{
		{
			ID: "aaaa1111-0000-4000-8000-000000000005", Type: parser.BlockNote,
			RawText: "linked noise", CleanText: "linked noise", LineNumber: 1,
		},
	}
	if err := dm.IndexFileBlocks("linked:x", "Linked", "", "Quiet Empty Page", linked, nil); err != nil {
		t.Fatalf("index linked page: %v", err)
	}

	all, err := dm.SearchBlocksPaged("quiet", 0, 50, SearchFilters{})
	if err != nil {
		t.Fatalf("SearchBlocksPaged: %v", err)
	}
	pageLocs := map[string]bool{}
	for _, r := range all.Results {
		if r.ID == "" {
			pageLocs[r.Source] = true
		}
	}
	if !pageLocs["vault"] || !pageLocs["linked:x"] {
		t.Errorf("expected title hits from both sources, got %+v", all.Results)
	}

	vaultOnly, err := dm.SearchBlocksPaged("quiet", 0, 50, SearchFilters{VaultOnly: true})
	if err != nil {
		t.Fatalf("SearchBlocksPaged: %v", err)
	}
	for _, r := range vaultOnly.Results {
		if r.ID == "" && r.Source != "vault" {
			t.Errorf("VaultOnly leaked a linked title hit: %+v", r)
		}
	}
}

func TestSearch_TitleHitsRespectPagingEnvelope(t *testing.T) {
	dm := seedSearchPages(t)

	// "agenda" yields one title hit + two body hits; page size 1 must page
	// over the MERGED list with correct envelope math.
	page1, err := dm.SearchBlocksPaged("agenda", 0, 1, SearchFilters{})
	if err != nil {
		t.Fatalf("page1: %v", err)
	}
	if len(page1.Results) != 1 || page1.Results[0].ID != "" {
		t.Fatalf("page1 should be the title hit, got %+v", page1.Results)
	}
	if page1.Total != 3 || !page1.HasMore {
		t.Fatalf("envelope: total=%d hasMore=%v", page1.Total, page1.HasMore)
	}
	page2, err := dm.SearchBlocksPaged("agenda", 1, 1, SearchFilters{})
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	if len(page2.Results) != 1 || page2.Results[0].ID == "" {
		t.Fatalf("page2 should be a body hit, got %+v", page2.Results)
	}
	if !page2.HasMore {
		t.Error("page2 HasMore should be true (one body hit remains)")
	}
	page3, err := dm.SearchBlocksPaged("agenda", 2, 1, SearchFilters{})
	if err != nil {
		t.Fatalf("page3: %v", err)
	}
	if len(page3.Results) != 1 || page3.Results[0].ID == "" || page3.HasMore {
		t.Fatalf("page3 should be the final body hit without HasMore, got %+v hasMore=%v", page3.Results, page3.HasMore)
	}
}

// ownerOf returns the distinct owners in a result set (for terse assertions).
func ownersOf(results []parser.TaskResult) map[string]bool {
	out := map[string]bool{}
	for _, r := range results {
		out[r.Owner] = true
	}
	return out
}

func TestSearch_OwnerFilterCombinedWithText(t *testing.T) {
	dm := seedSearchPages(t)

	// Text alone: all three Postgres tasks match.
	plain, err := dm.SearchBlocksPaged("postgres", 0, 50, SearchFilters{})
	if err != nil {
		t.Fatalf("SearchBlocksPaged: %v", err)
	}
	if plain.Total != 3 {
		t.Fatalf("unfiltered: expected 3 Postgres tasks, got %d", plain.Total)
	}

	// Owner + text: only Bob's task. Case-insensitive in both directions.
	for _, owner := range []string{"bob", "BOB", "Bob"} {
		res, err := dm.SearchBlocksPaged("postgres", 0, 50, SearchFilters{Owner: owner})
		if err != nil {
			t.Fatalf("SearchBlocksPaged(owner=%q): %v", owner, err)
		}
		if res.Total != 1 || res.Results[0].Owner != "Bob" {
			t.Errorf("owner=%q: expected exactly Bob's task, got %+v", owner, res.Results)
		}
	}

	// A non-matching owner narrows to nothing.
	none, err := dm.SearchBlocksPaged("postgres", 0, 50, SearchFilters{Owner: "Zed"})
	if err != nil {
		t.Fatalf("SearchBlocksPaged: %v", err)
	}
	if none.Total != 0 {
		t.Errorf("owner=Zed: expected 0, got %d", none.Total)
	}
}

func TestSearch_OverdueFilterCombinedWithText(t *testing.T) {
	dm := seedSearchPages(t)

	// Overdue + "postgres": Bob's TODO (due 2020) only — Carol's task is
	// equally past-due but DONE, Alice's is future-dated.
	res, err := dm.SearchBlocksPaged("postgres", 0, 50, SearchFilters{Overdue: true})
	if err != nil {
		t.Fatalf("SearchBlocksPaged: %v", err)
	}
	if res.Total != 1 || res.Results[0].Owner != "Bob" {
		t.Fatalf("overdue+text: expected only Bob's overdue TODO, got %+v", res.Results)
	}

	// A task with NO due date must never match the overdue filter.
	undated := []parser.ParsedBlock{
		{
			ID: "aaaa1111-0000-4000-8000-000000000008", Type: parser.BlockTask,
			RawText:   "- [ ] Postgres undated chore [owner:: Bob]",
			CleanText: "Postgres undated chore", Status: "TODO", Owner: "Bob",
			LineNumber: 1,
		},
	}
	if err := dm.IndexFileBlocks("vault", "Work", "Projects", "Chores", undated, nil); err != nil {
		t.Fatalf("index chores: %v", err)
	}
	res, err = dm.SearchBlocksPaged("undated", 0, 50, SearchFilters{Overdue: true})
	if err != nil {
		t.Fatalf("SearchBlocksPaged: %v", err)
	}
	if res.Total != 0 {
		t.Errorf("undated task must not match overdue, got %+v", res.Results)
	}
}

func TestSearch_StatusFilterCombinedWithText(t *testing.T) {
	dm := seedSearchPages(t)

	res, err := dm.SearchBlocksPaged("postgres", 0, 50, SearchFilters{Status: "doing"})
	if err != nil {
		t.Fatalf("SearchBlocksPaged: %v", err)
	}
	if res.Total != 1 || res.Results[0].Owner != "Alice" {
		t.Fatalf("status=doing (lowercase): expected Alice's DOING task, got %+v", res.Results)
	}

	done, err := dm.SearchBlocksPaged("postgres", 0, 50, SearchFilters{Status: "DONE"})
	if err != nil {
		t.Fatalf("SearchBlocksPaged: %v", err)
	}
	if done.Total != 1 || done.Results[0].Owner != "Carol" {
		t.Fatalf("status=DONE: expected Carol's task, got %+v", done.Results)
	}
}

func TestSearch_DueBoundsCombinedWithText(t *testing.T) {
	dm := seedSearchPages(t)

	after, err := dm.SearchBlocksPaged("postgres", 0, 50, SearchFilters{DueAfter: "2029-01-01"})
	if err != nil {
		t.Fatalf("SearchBlocksPaged: %v", err)
	}
	if after.Total != 1 || after.Results[0].Owner != "Alice" {
		t.Fatalf("dueAfter=2029: expected only Alice's 2030 task, got %+v", after.Results)
	}

	// Inclusive upper bound: Carol's task is due exactly ON the bound date.
	before, err := dm.SearchBlocksPaged("postgres", 0, 50, SearchFilters{DueBefore: "2020-02-01"})
	if err != nil {
		t.Fatalf("SearchBlocksPaged: %v", err)
	}
	got := ownersOf(before.Results)
	if before.Total != 2 || !got["Bob"] || !got["Carol"] {
		t.Fatalf("dueBefore=2020-02-01 (inclusive): expected Bob+Carol, got %+v", before.Results)
	}
}

func TestSearch_PriorityMinFloor(t *testing.T) {
	dm := seedSearchPages(t)

	// Floor 1 (highest only): Bob's P1.
	p1, err := dm.SearchBlocksPaged("postgres", 0, 50, SearchFilters{PriorityMin: 1})
	if err != nil {
		t.Fatalf("SearchBlocksPaged: %v", err)
	}
	if p1.Total != 1 || p1.Results[0].Owner != "Bob" {
		t.Fatalf("priorityMin=1: expected only Bob's P1, got %+v", p1.Results)
	}

	// Floor 2 (P1+P2): everyone.
	p2, err := dm.SearchBlocksPaged("postgres", 0, 50, SearchFilters{PriorityMin: 2})
	if err != nil {
		t.Fatalf("SearchBlocksPaged: %v", err)
	}
	if p2.Total != 3 {
		t.Fatalf("priorityMin=2: expected all three (P1+P2), got %+v", p2.Results)
	}

	// A NULL priority row (no priority recorded) must never match a floor,
	// not even floor 3 — "no priority recorded" is not a priority.
	if _, err := dm.SQLDB().Exec(`UPDATE tasks SET priority = NULL WHERE block_id = ?`,
		"aaaa1111-0000-4000-8000-000000000002"); err != nil {
		t.Fatalf("null out priority: %v", err)
	}
	anyFloor, err := dm.SearchBlocksPaged("backup", 0, 50, SearchFilters{PriorityMin: 3})
	if err != nil {
		t.Fatalf("SearchBlocksPaged: %v", err)
	}
	if anyFloor.Total != 0 {
		t.Fatalf("NULL priority must not match priorityMin=3, got %+v", anyFloor.Results)
	}
}

func TestSearch_TaskFiltersSuppressTitleHits(t *testing.T) {
	dm := seedSearchPages(t)

	// "meeting" matches the Meeting Notes page by TITLE only. Under any
	// task-attribute filter the page leg must vanish — a page row has no
	// owner — and the body leg matches nothing, so the result is empty.
	filters := []SearchFilters{
		{Owner: "Bob"},
		{Status: "TODO"},
		{DueBefore: "2030-01-01"},
		{DueAfter: "2000-01-01"},
		{PriorityMin: 1},
		{Overdue: true},
	}
	for _, f := range filters {
		res, err := dm.SearchBlocksPaged("meeting", 0, 50, f)
		if err != nil {
			t.Fatalf("SearchBlocksPaged(%+v): %v", f, err)
		}
		if res.Total != 0 {
			t.Errorf("task filter %+v: expected 0 results for title-only term, got %+v", f, res.Results)
		}
	}
}

func TestSearch_ZeroValueFiltersAreCompleteNoOps(t *testing.T) {
	dm := seedSearchPages(t)

	// A zero-value SearchFilters must reproduce the exact pre-filter result
	// set: every block matching the text — including NOTE blocks that any
	// task predicate would silently drop — plus the page-title leg.
	res, err := dm.SearchBlocksPaged("postgres", 0, 50, SearchFilters{})
	if err != nil {
		t.Fatalf("SearchBlocksPaged: %v", err)
	}
	if res.Total != 3 {
		t.Fatalf("zero-value filters changed the result set: total=%d (want 3 tasks)", res.Total)
	}
	for _, r := range res.Results {
		if r.ID == "" {
			t.Errorf("no page-level row expected for a body-only term: %+v", r)
		}
		if r.Status == "" {
			// Task results keep their projected attributes.
			t.Errorf("result lost task projection fields: %+v", r)
		}
	}
}

func TestSearch_RecencySortKeepsTitleHitsFirst(t *testing.T) {
	dm := seedSearchPages(t)

	// Recency mode reorders BODY hits (file_date DESC) but page-title hits
	// still lead the merged list — the page named for the query is the best
	// match regardless of sort mode.
	res, err := dm.SearchBlocksPaged("agenda", 0, 50, SearchFilters{Sort: "recency"})
	if err != nil {
		t.Fatalf("SearchBlocksPaged: %v", err)
	}
	if res.Total != 3 {
		t.Fatalf("expected 3 results, got %d", res.Total)
	}
	if res.Results[0].ID != "" || res.Results[0].Page != "Agenda Page" {
		t.Errorf("recency sort: title hit must still rank first, got %+v", res.Results[0])
	}
}

func TestSearch_SearchBlocksReturnsOnlyBlockHits(t *testing.T) {
	dm := seedSearchPages(t)

	// A title-only term produces exactly one page-level row in the paged
	// envelope...
	paged, err := dm.SearchBlocksPaged("quiet empty", 0, 50, SearchFilters{})
	if err != nil {
		t.Fatalf("SearchBlocksPaged: %v", err)
	}
	if paged.Total != 1 || paged.Results[0].ID != "" {
		t.Fatalf("fixture: expected a single page-level title hit, got %+v", paged.Results)
	}

	// ...but the legacy block-only binding must not expose it: its consumers
	// (editor block-ref picker, plugin SDK) key and navigate by block id, and
	// an empty id breaks the picker.
	blocks, err := dm.SearchBlocks("quiet empty")
	if err != nil {
		t.Fatalf("SearchBlocks: %v", err)
	}
	if len(blocks) != 0 {
		t.Fatalf("SearchBlocks leaked a page-level row: %+v", blocks)
	}

	// A mixed query (one title hit + two body hits) filters the page row and
	// keeps every block hit.
	blocks, err = dm.SearchBlocks("agenda")
	if err != nil {
		t.Fatalf("SearchBlocks: %v", err)
	}
	if len(blocks) != 2 {
		t.Fatalf("expected the 2 body hits only, got %+v", blocks)
	}
	for _, r := range blocks {
		if r.ID == "" {
			t.Errorf("SearchBlocks returned a page-level row: %+v", r)
		}
	}
}

func TestSearch_UnicodeOwnerAndTitleExactCase(t *testing.T) {
	dm := newTestDB(t)

	release := []parser.ParsedBlock{
		{
			ID: "aaaa1111-0000-4000-8000-000000000009", Type: parser.BlockTask,
			RawText:   "- [ ] Release checklist [owner:: Ünal]",
			CleanText: "Release checklist", Status: "TODO", Owner: "Ünal", LineNumber: 1,
		},
	}
	if err := dm.IndexFileBlocks("vault", "Work", "Projects", "Release Notes", release, nil); err != nil {
		t.Fatalf("index release page: %v", err)
	}
	if err := dm.IndexFileWithProjection("vault", "Work", "Projects", "MÜNCHEN",
		nil, nil, "", nil, PageCoreFields{}); err != nil {
		t.Fatalf("index accented page: %v", err)
	}

	// Owner typed exactly as stored: the needle used to be Go-lowered while the
	// haystack was SQLite-lowered, so Ü matched only Ü by luck of no folding —
	// and "Ünal" vs "ünal" could never compare.
	res, err := dm.SearchBlocksPaged("release", 0, 50, SearchFilters{Owner: "Ünal"})
	if err != nil {
		t.Fatalf("SearchBlocksPaged(owner): %v", err)
	}
	if res.Total != 1 || res.Results[0].Owner != "Ünal" {
		t.Fatalf("exact-case non-ASCII owner did not match: %+v", res.Results)
	}

	// Page title typed exactly as stored.
	title, err := dm.SearchBlocksPaged("MÜNCHEN", 0, 50, SearchFilters{})
	if err != nil {
		t.Fatalf("SearchBlocksPaged(title): %v", err)
	}
	if title.Total != 1 || title.Results[0].ID != "" || title.Results[0].Page != "MÜNCHEN" {
		t.Fatalf("exact-case non-ASCII page title did not match: %+v", title.Results)
	}
	if !strings.Contains(title.Results[0].Snippet, "<mark>MÜNCHEN</mark>") {
		t.Errorf("snippet should highlight the accented title, got %q", title.Results[0].Snippet)
	}
}

func TestSearch_TitleHitsSuppressUnderTagFilter(t *testing.T) {
	dm := seedSearchPages(t)

	// "notes" matches the Meeting Notes page by TITLE only. A tag filter makes
	// the query block-scoped (page rows carry no tags), so the page leg must
	// vanish — the counterpart to the type-filter test.
	res, err := dm.SearchBlocksPaged("notes", 0, 50, SearchFilters{Tag: "work"})
	if err != nil {
		t.Fatalf("SearchBlocksPaged(tag): %v", err)
	}
	if res.Total != 0 {
		t.Fatalf("tag filter must suppress the page-title leg, got %+v", res.Results)
	}

	// Unfiltered, the same term surfaces the page — proves the filter, not the
	// fixture, suppressed it.
	plain, err := dm.SearchBlocksPaged("notes", 0, 50, SearchFilters{})
	if err != nil {
		t.Fatalf("SearchBlocksPaged: %v", err)
	}
	if plain.Total != 1 || plain.Results[0].Page != "Meeting Notes" {
		t.Fatalf("expected the Meeting Notes title hit unfiltered, got %+v", plain.Results)
	}
}

func TestSearch_TitleHitsBoundedByFlatCap(t *testing.T) {

	dm := newTestDB(t)

	// searchFlatCap+10 zero-block pages whose names all share a term: the
	// title leg must stop at the flat cap, and the envelope must report the
	// truncation via Total.
	total := searchFlatCap + 10
	results := make([]parser.ScanResult, 0, total)
	projections := make([]ScanProjection, 0, total)
	for i := 0; i < total; i++ {
		results = append(results, parser.ScanResult{
			Path:     fmt.Sprintf("NB/p%04d.md", i),
			Notebook: "NB", Section: "", Page: fmt.Sprintf("Kilroy Page %04d", i),
		})
		projections = append(projections, ScanProjection{})
	}
	if _, _, err := dm.IndexScanResultsWithProjection(results, projections); err != nil {
		t.Fatalf("batch index: %v", err)
	}

	res, err := dm.SearchBlocksPaged("kilroy", 0, 10, SearchFilters{})
	if err != nil {
		t.Fatalf("SearchBlocksPaged: %v", err)
	}
	if res.Total != searchFlatCap {
		t.Fatalf("title leg must cap at searchFlatCap=%d, got total=%d", searchFlatCap, res.Total)
	}
	if !res.HasMore {
		t.Error("HasMore expected true: 10 returned of 500 capped total")
	}
}
