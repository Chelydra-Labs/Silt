package db

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"silt/backend/parser"
)

// --- Helpers ---

const (
	uuidA = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	uuidB = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	uuidC = "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	uuidD = "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
	uuidE = "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"
	uuidF = "ffffffff-ffff-4fff-8fff-ffffffffffff"
)

func idx(t *testing.T, dm *DatabaseManager, source, nb, sec, pg string, blocks []parser.ParsedBlock) {
	t.Helper()
	if err := dm.IndexFileBlocks(source, nb, sec, pg, blocks, nil); err != nil {
		t.Fatalf("index %s/%s/%s/%s: %v", source, nb, sec, pg, err)
	}
}

func noteBlock(id, clean string) parser.ParsedBlock {
	return parser.ParsedBlock{
		ID: id, Type: parser.BlockNote, RawText: clean, CleanText: clean, LineNumber: 1,
	}
}

func taskBlock(id, clean string) parser.ParsedBlock {
	return parser.ParsedBlock{
		ID: id, Type: parser.BlockTask, Status: "TODO", RawText: clean, CleanText: clean, LineNumber: 1,
	}
}

// --- Core Tests ---

func TestGetBacklinks_PageLinkOnly(t *testing.T) {
	dm := newTestDB(t)
	// Target page.
	idx(t, dm, "vault", "NB", "Sec", "Target", []parser.ParsedBlock{
		noteBlock(uuidA, "target content"),
	})
	// Source page with a [[Target]] link.
	idx(t, dm, "vault", "NB", "Sec", "Source", []parser.ParsedBlock{
		noteBlock(uuidB, "see [[Target]] for details"),
	})

	bl, err := dm.GetBacklinks("vault", "NB", "Sec", "Target")
	if err != nil {
		t.Fatalf("GetBacklinks: %v", err)
	}
	if len(bl) != 1 {
		t.Fatalf("expected 1 backlink, got %d: %+v", len(bl), bl)
	}
	if bl[0].Kind != BacklinkPageLink {
		t.Errorf("expected page kind, got %q", bl[0].Kind)
	}
	if bl[0].SourceNotebook != "NB" || bl[0].SourcePage != "Source" {
		t.Errorf("source: %+v", bl[0])
	}
	if bl[0].Source != "vault" {
		t.Errorf("expected source vault, got %q", bl[0].Source)
	}
}

func TestGetBacklinks_BlockRefOnly(t *testing.T) {
	dm := newTestDB(t)
	// Target page with a block.
	idx(t, dm, "vault", "NB", "Sec", "Target", []parser.ParsedBlock{
		noteBlock(uuidA, "target block"),
	})
	// Source page referencing target's block.
	idx(t, dm, "vault", "NB", "Sec", "Source", []parser.ParsedBlock{
		noteBlock(uuidB, "see this ref "+("(("+uuidA+"))")+" here"),
	})

	bl, err := dm.GetBacklinks("vault", "NB", "Sec", "Target")
	if err != nil {
		t.Fatalf("GetBacklinks: %v", err)
	}
	if len(bl) != 1 {
		t.Fatalf("expected 1 backlink, got %d: %+v", len(bl), bl)
	}
	if bl[0].Kind != BacklinkBlockRef {
		t.Errorf("expected block-ref kind, got %q", bl[0].Kind)
	}
}

func TestGetBacklinks_EmbedOnly(t *testing.T) {
	dm := newTestDB(t)
	idx(t, dm, "vault", "NB", "Sec", "Target", []parser.ParsedBlock{
		noteBlock(uuidA, "target block"),
	})
	idx(t, dm, "vault", "NB", "Sec", "Source", []parser.ParsedBlock{
		noteBlock(uuidB, "embed here: "+"{{embed:"+uuidA+"}}"),
	})

	bl, err := dm.GetBacklinks("vault", "NB", "Sec", "Target")
	if err != nil {
		t.Fatalf("GetBacklinks: %v", err)
	}
	if len(bl) != 1 || bl[0].Kind != BacklinkEmbed {
		t.Fatalf("expected 1 embed backlink, got %d: %+v", len(bl), bl)
	}
}

func TestGetBacklinks_MixedLegs(t *testing.T) {
	dm := newTestDB(t)
	idx(t, dm, "vault", "NB", "Sec", "Target", []parser.ParsedBlock{
		noteBlock(uuidA, "target content"),
	})
	// Page-link source.
	idx(t, dm, "vault", "NB", "Sec", "SrcPL", []parser.ParsedBlock{
		noteBlock(uuidB, "see [[Target]]"),
	})
	// Block-ref source.
	idx(t, dm, "vault", "NB", "Sec", "SrcBR", []parser.ParsedBlock{
		noteBlock(uuidC, "ref to "+("(("+uuidA+"))")),
	})
	// Embed source.
	idx(t, dm, "vault", "NB", "Sec", "SrcEM", []parser.ParsedBlock{
		noteBlock(uuidD, "embed "+"{{embed:"+uuidA+"}}"),
	})

	bl, err := dm.GetBacklinks("vault", "NB", "Sec", "Target")
	if err != nil {
		t.Fatalf("GetBacklinks: %v", err)
	}
	if len(bl) != 3 {
		t.Fatalf("expected 3 backlinks (one per leg), got %d: %+v", len(bl), bl)
	}
	kinds := map[BacklinkKind]bool{}
	for _, b := range bl {
		kinds[b.Kind] = true
	}
	for _, k := range []BacklinkKind{BacklinkPageLink, BacklinkBlockRef, BacklinkEmbed} {
		if !kinds[k] {
			t.Errorf("missing kind %q", k)
		}
	}
}

func TestGetBacklinks_Empty(t *testing.T) {
	dm := newTestDB(t)
	idx(t, dm, "vault", "NB", "Sec", "Target", []parser.ParsedBlock{
		noteBlock(uuidA, "orphan page"),
	})

	bl, err := dm.GetBacklinks("vault", "NB", "Sec", "Target")
	if err != nil {
		t.Fatalf("GetBacklinks: %v", err)
	}
	if len(bl) != 0 {
		t.Fatalf("expected 0 backlinks, got %d", len(bl))
	}
}

func TestGetBacklinks_EmptyPage(t *testing.T) {
	dm := newTestDB(t)
	bl, err := dm.GetBacklinks("vault", "NB", "Sec", "")
	if err != nil {
		t.Fatalf("GetBacklinks empty page: %v", err)
	}
	if bl != nil {
		t.Errorf("expected nil for empty page, got %v", bl)
	}
}

// TestGetBacklinks_AmbiguousTargetExcluded verifies that page-links whose
// target_raw is ambiguous (same basename in multiple pages) are NOT returned.
// The resolve-gate matches the rename-rewrite path's contract.
func TestGetBacklinks_AmbiguousTargetExcluded(t *testing.T) {
	dm := newTestDB(t)
	// Two pages with the same basename "Daily" under different sections.
	idx(t, dm, "vault", "NB", "Journal", "Daily", []parser.ParsedBlock{
		noteBlock(uuidA, "journal daily"),
	})
	idx(t, dm, "vault", "NB", "Log", "Daily", []parser.ParsedBlock{
		noteBlock(uuidB, "log daily"),
	})
	// A page that links to "Daily" — this is ambiguous, should NOT appear as
	// a backlink for either target.
	idx(t, dm, "vault", "NB", "Sec", "Source", []parser.ParsedBlock{
		noteBlock(uuidC, "see [[Daily]] here"),
	})

	bl, err := dm.GetBacklinks("vault", "NB", "Journal", "Daily")
	if err != nil {
		t.Fatalf("GetBacklinks: %v", err)
	}
	if len(bl) != 0 {
		t.Fatalf("ambiguous [[Daily]] should be excluded, got %d: %+v", len(bl), bl)
	}

	bl2, err := dm.GetBacklinks("vault", "NB", "Log", "Daily")
	if err != nil {
		t.Fatalf("GetBacklinks log: %v", err)
	}
	if len(bl2) != 0 {
		t.Fatalf("ambiguous [[Daily]] should be excluded for Log too, got %d", len(bl2))
	}
}

// TestGetBacklinks_ExactPageLinkIncluded verifies that disambiguated links
// (e.g. [[Journal/Daily]]) DO appear.
func TestGetBacklinks_ExactPageLinkIncluded(t *testing.T) {
	dm := newTestDB(t)
	idx(t, dm, "vault", "NB", "Journal", "Daily", []parser.ParsedBlock{
		noteBlock(uuidA, "journal daily"),
	})
	idx(t, dm, "vault", "NB", "Log", "Daily", []parser.ParsedBlock{
		noteBlock(uuidB, "log daily"),
	})
	// Disambiguated link to Journal/Daily.
	idx(t, dm, "vault", "NB", "Sec", "Source", []parser.ParsedBlock{
		noteBlock(uuidC, "see [[Journal/Daily]] here"),
	})

	bl, err := dm.GetBacklinks("vault", "NB", "Journal", "Daily")
	if err != nil {
		t.Fatalf("GetBacklinks: %v", err)
	}
	if len(bl) != 1 {
		t.Fatalf("disambiguated [[Journal/Daily]] should be included, got %d", len(bl))
	}
}

// TestGetBacklinks_BrokenLinkExcluded verifies that page-links whose target_raw
// resolves to nothing (broken link) are excluded from backlinks.
func TestGetBacklinks_BrokenLinkExcluded(t *testing.T) {
	dm := newTestDB(t)
	idx(t, dm, "vault", "NB", "Sec", "Target", []parser.ParsedBlock{
		noteBlock(uuidA, "target content"),
	})
	// A page linking to a nonexistent page.
	idx(t, dm, "vault", "NB", "Sec", "Source", []parser.ParsedBlock{
		noteBlock(uuidB, "see [[NoSuchPage]]"),
	})

	bl, err := dm.GetBacklinks("vault", "NB", "Sec", "Target")
	if err != nil {
		t.Fatalf("GetBacklinks: %v", err)
	}
	if len(bl) != 0 {
		t.Fatalf("broken link should not be a backlink to Target, got %d", len(bl))
	}
}

// TestGetBacklinks_IntermediatePathForm verifies that [[Sec/Target]] matches
// NB/Sec/Target when the full path is NB/Sec/Target (intermediate suffix).
func TestGetBacklinks_IntermediatePathForm(t *testing.T) {
	dm := newTestDB(t)
	idx(t, dm, "vault", "NB", "Projects", "Site", []parser.ParsedBlock{
		noteBlock(uuidA, "site content"),
	})
	// Link using intermediate path form.
	idx(t, dm, "vault", "NB", "Sec", "Source", []parser.ParsedBlock{
		noteBlock(uuidB, "see [[Projects/Site]]"),
	})

	bl, err := dm.GetBacklinks("vault", "NB", "Projects", "Site")
	if err != nil {
		t.Fatalf("GetBacklinks: %v", err)
	}
	if len(bl) != 1 {
		t.Fatalf("intermediate [[Projects/Site]] should match, got %d", len(bl))
	}
}

// TestGetBacklinks_CrossSource verifies that a linked-notebook page's backlinks
// are source-scoped: a block-ref from a linked notebook IS returned, but a
// page-link that resolves to a vault page is NOT returned for a linked target.
func TestGetBacklinks_CrossSource(t *testing.T) {
	dm := newTestDB(t)
	// Vault target page.
	idx(t, dm, "vault", "NB", "Sec", "Target", []parser.ParsedBlock{
		noteBlock(uuidA, "vault target block"),
	})
	// Linked source page with a block-ref into the vault target.
	idx(t, dm, "linked:ext", "Ext", "Sec", "Src", []parser.ParsedBlock{
		noteBlock(uuidB, "cross-source ref "+("(("+uuidA+"))")),
	})
	// Another vault source page with a page-link to Target.
	idx(t, dm, "vault", "NB", "Sec", "Src2", []parser.ParsedBlock{
		noteBlock(uuidC, "see [[Target]] here"),
	})

	// Backlinks for the vault target.
	bl, err := dm.GetBacklinks("vault", "NB", "Sec", "Target")
	if err != nil {
		t.Fatalf("GetBacklinks: %v", err)
	}
	if len(bl) != 2 {
		t.Fatalf("expected 2 backlinks (block-ref from linked + page-link from vault), got %d: %+v", len(bl), bl)
	}

	kinds := map[BacklinkKind]string{}
	for _, b := range bl {
		kinds[b.Kind] = b.Source
	}
	if kinds[BacklinkBlockRef] != "linked:ext" {
		t.Errorf("block-ref should come from linked:ext, got %q", kinds[BacklinkBlockRef])
	}
	if kinds[BacklinkPageLink] != "vault" {
		t.Errorf("page-link should come from vault, got %q", kinds[BacklinkPageLink])
	}
}

// TestGetBacklinks_CrossSourceTargetScoping verifies that block-refs to a
// vault page are NOT returned when querying for a linked page with the same
// name.
func TestGetBacklinks_CrossSourceTargetScoping(t *testing.T) {
	dm := newTestDB(t)
	// Vault page NB/Sec/Target.
	idx(t, dm, "vault", "NB", "Sec", "Target", []parser.ParsedBlock{
		noteBlock(uuidA, "vault target"),
	})
	// Linked page Ext/Sec/Target (same notebook name, different source).
	idx(t, dm, "linked:ext", "NB", "Sec", "Target", []parser.ParsedBlock{
		noteBlock(uuidB, "linked target"),
	})
	// Block-ref to the vault page's block.
	idx(t, dm, "vault", "NB", "Sec", "Src", []parser.ParsedBlock{
		noteBlock(uuidC, "ref to vault "+("(("+uuidA+"))")),
	})

	// Backlinks for the LINKED target should NOT include the ref to vault block.
	bl, err := dm.GetBacklinks("linked:ext", "NB", "Sec", "Target")
	if err != nil {
		t.Fatalf("GetBacklinks: %v", err)
	}
	if len(bl) != 0 {
		t.Fatalf("linked target should have no backlinks from vault block, got %d: %+v", len(bl), bl)
	}

	// Backlinks for the VAULT target SHOULD include the ref.
	bl2, err := dm.GetBacklinks("vault", "NB", "Sec", "Target")
	if err != nil {
		t.Fatalf("GetBacklinks vault: %v", err)
	}
	if len(bl2) != 1 {
		t.Fatalf("vault target should have 1 backlink, got %d", len(bl2))
	}
}

// TestGetBacklinks_SnippetTruncation verifies the 120-rune snippet cap.
func TestGetBacklinks_SnippetTruncation(t *testing.T) {
	dm := newTestDB(t)
	idx(t, dm, "vault", "NB", "Sec", "Target", []parser.ParsedBlock{
		noteBlock(uuidA, "target"),
	})
	longText := strings.Repeat("word ", 100) // 500 bytes, ~100 words
	idx(t, dm, "vault", "NB", "Sec", "Source", []parser.ParsedBlock{
		noteBlock(uuidB, "[[Target]] "+longText),
	})

	bl, err := dm.GetBacklinks("vault", "NB", "Sec", "Target")
	if err != nil {
		t.Fatalf("GetBacklinks: %v", err)
	}
	if len(bl) != 1 {
		t.Fatalf("expected 1 backlink, got %d", len(bl))
	}
	runes := []rune(bl[0].Snippet)
	if len(runes) > backlinkSnippetRunes+1 {
		t.Errorf("snippet exceeds %d runes: got %d", backlinkSnippetRunes+1, len(runes))
	}
}

func TestSnippet_Length(t *testing.T) {
	short := "hello world"
	if got := snippet(short); got != short {
		t.Errorf("short snippet: %q", got)
	}
	long := strings.Repeat("a", 200)
	got := snippet(long)
	// 120 runes + "…"
	if len(got) != 121+len("…") {
		// rune count check
	}
	runes := []rune(got)
	if len(runes) != backlinkSnippetRunes+1 { // 120 + ellipsis
		t.Errorf("expected %d runes, got %d", backlinkSnippetRunes+1, len(runes))
	}
}

func TestSnippet_Empty(t *testing.T) {
	if got := snippet(""); got != "" {
		t.Errorf("empty snippet: %q", got)
	}
}

// TestGetBacklinks_Dedupe verifies that a block that contains both a
// ((uuid)) and {{embed:uuid}} to the same target block produces two separate
// backlink entries (one per kind).
func TestGetBacklinks_Dedupe(t *testing.T) {
	dm := newTestDB(t)
	idx(t, dm, "vault", "NB", "Sec", "Target", []parser.ParsedBlock{
		noteBlock(uuidA, "target"),
	})
	// Source block with both a block-ref and embed to the same target.
	idx(t, dm, "vault", "NB", "Sec", "Source", []parser.ParsedBlock{
		noteBlock(uuidB, "ref "+("(("+uuidA+"))")+" and embed "+"{{embed:"+uuidA+"}}"),
	})

	bl, err := dm.GetBacklinks("vault", "NB", "Sec", "Target")
	if err != nil {
		t.Fatalf("GetBacklinks: %v", err)
	}
	if len(bl) != 2 {
		t.Fatalf("expected 2 backlinks (block-ref + embed), got %d: %+v", len(bl), bl)
	}
	kinds := map[BacklinkKind]bool{}
	for _, b := range bl {
		kinds[b.Kind] = true
	}
	if !kinds[BacklinkBlockRef] || !kinds[BacklinkEmbed] {
		t.Errorf("expected both kinds, got %v", kinds)
	}
}

// TestGetBacklinks_SameKindDedup verifies that two [[Target]] links in the
// same block (INSERT OR IGNORE on page_links PK) produce exactly one backlink.
func TestGetBacklinks_SameKindDedup(t *testing.T) {
	dm := newTestDB(t)
	idx(t, dm, "vault", "NB", "Sec", "Target", []parser.ParsedBlock{
		noteBlock(uuidA, "target"),
	})
	// Block with two [[Target]] links.
	idx(t, dm, "vault", "NB", "Sec", "Source", []parser.ParsedBlock{
		noteBlock(uuidB, "[[Target]] and [[Target]] again"),
	})

	bl, err := dm.GetBacklinks("vault", "NB", "Sec", "Target")
	if err != nil {
		t.Fatalf("GetBacklinks: %v", err)
	}
	if len(bl) != 1 {
		t.Fatalf("expected 1 deduped page-link, got %d", len(bl))
	}
}

// TestGetBacklinks_Sort verifies stable sort order: (notebook, section, page,
// kind, block_id).
func TestGetBacklinks_Sort(t *testing.T) {
	dm := newTestDB(t)
	idx(t, dm, "vault", "NB", "Sec", "Target", []parser.ParsedBlock{
		noteBlock(uuidA, "t"),
	})
	idx(t, dm, "vault", "AA", "Sec", "S1", []parser.ParsedBlock{
		noteBlock(uuidB, "[[Target]]"),
	})
	idx(t, dm, "vault", "BB", "Sec", "S2", []parser.ParsedBlock{
		noteBlock(uuidC, "[[Target]]"),
	})
	idx(t, dm, "vault", "AA", "Sec", "S2", []parser.ParsedBlock{
		noteBlock(uuidD, "(("+uuidA+"))"),
	})

	bl, err := dm.GetBacklinks("vault", "NB", "Sec", "Target")
	if err != nil {
		t.Fatalf("GetBacklinks: %v", err)
	}
	if len(bl) != 3 {
		t.Fatalf("expected 3, got %d: %+v", len(bl), bl)
	}
	// AA/S1 page-link, AA/S2 block-ref, BB/S2 page-link
	if bl[0].SourcePage != "S1" {
		t.Errorf("first should be AA/S1, got %s/%s", bl[0].SourceNotebook, bl[0].SourcePage)
	}
	if bl[1].SourcePage != "S2" || bl[1].Kind != BacklinkBlockRef {
		t.Errorf("second should be AA/S2 block-ref, got %+v", bl[1])
	}
	if bl[2].SourceNotebook != "BB" {
		t.Errorf("third should be BB/S2, got %+v", bl[2])
	}
}

// TestGetBacklinks_FalsePositiveBlockRef verifies that a block whose
// clean_content contains a UUID-like substring in prose (NOT wrapped in ((…)))
// is NOT returned as a block-ref backlink. The LIKE pattern requires the
// exact ((uuid)) delimiters.
func TestGetBacklinks_FalsePositiveBlockRef(t *testing.T) {
	dm := newTestDB(t)
	idx(t, dm, "vault", "NB", "Sec", "Target", []parser.ParsedBlock{
		noteBlock(uuidA, "target"),
	})
	// Source block that mentions the UUID in prose without ((…)) delimiters.
	idx(t, dm, "vault", "NB", "Sec", "Source", []parser.ParsedBlock{
		noteBlock(uuidB, "the id is "+uuidA+" but not wrapped in parens"),
	})

	bl, err := dm.GetBacklinks("vault", "NB", "Sec", "Target")
	if err != nil {
		t.Fatalf("GetBacklinks: %v", err)
	}
	if len(bl) != 0 {
		t.Fatalf("bare UUID mention should not be a backlink, got %d: %+v", len(bl), bl)
	}
}

// TestGetBacklinks_FalsePositiveEmbed verifies that {{embed:uuid}} only
// matches the exact {{embed:…}} syntax, not {{other:uuid}}.
func TestGetBacklinks_FalsePositiveEmbed(t *testing.T) {
	dm := newTestDB(t)
	idx(t, dm, "vault", "NB", "Sec", "Target", []parser.ParsedBlock{
		noteBlock(uuidA, "target"),
	})
	idx(t, dm, "vault", "NB", "Sec", "Source", []parser.ParsedBlock{
		noteBlock(uuidB, "{{other:"+uuidA+"}}"),
	})

	bl, err := dm.GetBacklinks("vault", "NB", "Sec", "Target")
	if err != nil {
		t.Fatalf("GetBacklinks: %v", err)
	}
	// The raw_content contains uuidA but NOT inside {{embed:…}} or ((…)).
	// The LIKE for (( is `%((uuidA))%` which won't match {{other:…}}.
	// The LIKE for embed is `%{{embed:uuidA}}%` which won't match {{other:…}}.
	if len(bl) != 0 {
		t.Fatalf("non-embed syntax should not match, got %d: %+v", len(bl), bl)
	}
}

// TestGetBacklinks_MultipleTargetBlocks verifies that when the target page has
// multiple blocks, refs to ANY of them are found.
func TestGetBacklinks_MultipleTargetBlocks(t *testing.T) {
	dm := newTestDB(t)
	idx(t, dm, "vault", "NB", "Sec", "Target", []parser.ParsedBlock{
		noteBlock(uuidA, "block a"),
		noteBlock(uuidB, "block b"),
	})
	// Source referencing both target blocks.
	idx(t, dm, "vault", "NB", "Sec", "Source", []parser.ParsedBlock{
		noteBlock(uuidC, "(("+uuidA+")) and "+("(("+uuidB+"))")),
	})

	bl, err := dm.GetBacklinks("vault", "NB", "Sec", "Target")
	if err != nil {
		t.Fatalf("GetBacklinks: %v", err)
	}
	if len(bl) != 1 {
		t.Fatalf("expected 1 block-ref (deduped by block_id), got %d: %+v", len(bl), bl)
	}
}

// TestGetBacklinks_PageLinkCaseInsensitive verifies that [[target]] and
// [[TARGET]] both match the page "Target" (matching resolution semantics).
func TestGetBacklinks_PageLinkCaseInsensitive(t *testing.T) {
	dm := newTestDB(t)
	idx(t, dm, "vault", "NB", "Sec", "Target", []parser.ParsedBlock{
		noteBlock(uuidA, "target"),
	})
	idx(t, dm, "vault", "NB", "Sec", "Source1", []parser.ParsedBlock{
		noteBlock(uuidB, "[[Target]]"),
	})
	idx(t, dm, "vault", "NB", "Sec", "Source2", []parser.ParsedBlock{
		noteBlock(uuidC, "[[target]]"),
	})

	bl, err := dm.GetBacklinks("vault", "NB", "Sec", "Target")
	if err != nil {
		t.Fatalf("GetBacklinks: %v", err)
	}
	if len(bl) != 2 {
		t.Fatalf("expected 2 case-insensitive page-link backlinks, got %d: %+v", len(bl), bl)
	}
}

// TestGetBacklinks_IndexedQueryPlan asserts that the page-link leg uses the
// idx_page_links_raw_lower index (no full scan).
func TestGetBacklinks_IndexedQueryPlan(t *testing.T) {
	dm := newTestDB(t)
	idx(t, dm, "vault", "NB", "Sec", "Target", []parser.ParsedBlock{
		noteBlock(uuidA, "target"),
	})
	idx(t, dm, "vault", "NB", "Sec", "Source", []parser.ParsedBlock{
		noteBlock(uuidB, "[[Target]]"),
	})

	// EXPLAIN QUERY PLAN for the page-links leg query.
	// SQLite EXPLAIN QUERY PLAN returns 4 columns: selectid, order, from, detail.
	rows, err := dm.SQLDB().Query(
		"EXPLAIN QUERY PLAN SELECT source_notebook, source_section, source_page, source_block_id, "+
			"target_raw, COALESCE(heading,''), COALESCE(alias,'') "+
			"FROM page_links WHERE lower(target_raw) IN (SELECT lower(?))",
		"Target",
	)
	if err != nil {
		t.Fatalf("EXPLAIN: %v", err)
	}
	defer rows.Close()
	var plans []string
	for rows.Next() {
		var selectid, order, from, detail string
		if err := rows.Scan(&selectid, &order, &from, &detail); err != nil {
			t.Fatalf("scan: %v", err)
		}
		plans = append(plans, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows err: %v", err)
	}

	// Assert the plan does NOT do a full table scan (SCAN TABLE page_links).
	for _, p := range plans {
		if strings.Contains(p, "SCAN TABLE page_links") {
			t.Errorf("query plan should use idx_page_links_raw_lower, not full scan: %s", p)
		}
	}
	// Should use SEARCH TABLE via the index (or at least not a full SCAN).
	found := false
	for _, p := range plans {
		if strings.Contains(p, "SEARCH") || strings.Contains(p, "idx_page_links_raw_lower") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected index usage in query plan, got: %v", plans)
	}
}

// TestGetBacklinks_HeadingAndAliasIgnored verifies that the resolve-gate
// ignores heading (#…) and alias (|…) in the page-link target — only the base
// target matters for matching the destination page.
func TestGetBacklinks_HeadingAndAliasIgnored(t *testing.T) {
	dm := newTestDB(t)
	idx(t, dm, "vault", "NB", "Sec", "Target", []parser.ParsedBlock{
		noteBlock(uuidA, "target content"),
	})
	idx(t, dm, "vault", "NB", "Sec", "Source", []parser.ParsedBlock{
		noteBlock(uuidB, "see [[Target#Intro|Link Text]]"),
	})

	bl, err := dm.GetBacklinks("vault", "NB", "Sec", "Target")
	if err != nil {
		t.Fatalf("GetBacklinks: %v", err)
	}
	if len(bl) != 1 {
		t.Fatalf("heading/alias link should still match, got %d", len(bl))
	}
}

// TestGetBacklinks_LargeBatchBlockRefs verifies that block-ref/embed LIKE
// queries batch correctly when the target page has many blocks (> batch size).
func TestGetBacklinks_LargeBatchBlockRefs(t *testing.T) {
	dm := newTestDB(t)
	// Target page with enough blocks to force >1 batch.
	targetBlocks := make([]parser.ParsedBlock, 500)
	blockIDs := make([]string, 500)
	for i := range targetBlocks {
		id := fmt.Sprintf("aaaa%03d-aaaa-4aaa-8aaa-%028d", i%10, i)
		blockIDs[i] = id
		targetBlocks[i] = noteBlock(id, fmt.Sprintf("block %d", i))
	}
	idx(t, dm, "vault", "NB", "Sec", "Target", targetBlocks)

	// Source referencing the last block.
	srcBlock := noteBlock(uuidE, "ref to last "+("(("+blockIDs[499]+"))"))
	idx(t, dm, "vault", "NB", "Sec", "Source", []parser.ParsedBlock{srcBlock})

	bl, err := dm.GetBacklinks("vault", "NB", "Sec", "Target")
	if err != nil {
		t.Fatalf("GetBacklinks: %v", err)
	}
	if len(bl) != 1 {
		t.Fatalf("expected 1 backlink across batches, got %d", len(bl))
	}
	if bl[0].SourceBlockID != uuidE {
		t.Errorf("wrong source block: %s", bl[0].SourceBlockID)
	}
}

// TestGetBacklinks_SourceDefaultsVault verifies that passing empty source
// defaults to "vault".
func TestGetBacklinks_SourceDefaultsVault(t *testing.T) {
	dm := newTestDB(t)
	idx(t, dm, "vault", "NB", "Sec", "Target", []parser.ParsedBlock{
		noteBlock(uuidA, "target"),
	})
	idx(t, dm, "vault", "NB", "Sec", "Source", []parser.ParsedBlock{
		noteBlock(uuidB, "[[Target]]"),
	})

	bl, err := dm.GetBacklinks("", "NB", "Sec", "Target")
	if err != nil {
		t.Fatalf("GetBacklinks: %v", err)
	}
	if len(bl) != 1 {
		t.Fatalf("expected 1 backlink with empty source defaulting to vault, got %d", len(bl))
	}
}

// TestGetBacklinks_CodeBlockSkipped verifies that [[…]] inside CODE blocks
// are NOT indexed as page_links (the indexer skips CODE type), so they don't
// appear as backlinks.
func TestGetBacklinks_CodeBlockSkipped(t *testing.T) {
	dm := newTestDB(t)
	idx(t, dm, "vault", "NB", "Sec", "Target", []parser.ParsedBlock{
		noteBlock(uuidA, "target"),
	})
	idx(t, dm, "vault", "NB", "Sec", "Source", []parser.ParsedBlock{
		{ID: uuidB, Type: parser.BlockCode, CleanText: "[[Target]] in code", LineNumber: 1},
	})

	bl, err := dm.GetBacklinks("vault", "NB", "Sec", "Target")
	if err != nil {
		t.Fatalf("GetBacklinks: %v", err)
	}
	if len(bl) != 0 {
		t.Fatalf("code-block link should not be a backlink, got %d", len(bl))
	}
}

// TestGetBacklinks_NestedSectionPath verifies that deeply nested section paths
// (e.g. NB/A/B/C/Target) work correctly for all three legs.
func TestGetBacklinks_NestedSectionPath(t *testing.T) {
	dm := newTestDB(t)
	idx(t, dm, "vault", "NB", "A/B/C", "Target", []parser.ParsedBlock{
		noteBlock(uuidA, "deep target"),
	})
	idx(t, dm, "vault", "NB", "Sec", "Source", []parser.ParsedBlock{
		noteBlock(uuidB, "[[C/Target]] and "+("(("+uuidA+"))")),
	})

	bl, err := dm.GetBacklinks("vault", "NB", "A/B/C", "Target")
	if err != nil {
		t.Fatalf("GetBacklinks: %v", err)
	}
	if len(bl) != 2 {
		t.Fatalf("expected 2 backlinks (page-link + block-ref), got %d: %+v", len(bl), bl)
	}
}

// TestSnippet_LongerThan120Runes verifies multibyte content truncation.
func TestSnippet_LongerThan120Runes(t *testing.T) {
	// Each Japanese character is 3 bytes but 1 rune.
	long := strings.Repeat("日本語", 50) // 50 runes
	got := snippet(long)
	runes := []rune(got)
	if runes[len(runes)-1] != '…' {
		t.Errorf("expected trailing ellipsis, got %q", got)
	}
	if len(runes) != backlinkSnippetRunes+1 {
		t.Errorf("expected %d runes, got %d", backlinkSnippetRunes+1, len(runes))
	}
}

// TestGetBacklinks_EmptySnippet verifies that a block with empty clean_content
// produces an empty snippet. Uses a block-ref (which searches raw_content)
// with empty clean_text.
func TestGetBacklinks_EmptySnippet(t *testing.T) {
	dm := newTestDB(t)
	idx(t, dm, "vault", "NB", "Sec", "Target", []parser.ParsedBlock{
		noteBlock(uuidA, ""),
	})
	// Source block: raw_text has a block-ref, clean_text is empty.
	idx(t, dm, "vault", "NB", "Sec", "Source", []parser.ParsedBlock{
		{ID: uuidB, Type: parser.BlockNote, RawText: "((" + uuidA + "))", CleanText: "", LineNumber: 1},
	})

	bl, err := dm.GetBacklinks("vault", "NB", "Sec", "Target")
	if err != nil {
		t.Fatalf("GetBacklinks: %v", err)
	}
	if len(bl) != 1 {
		t.Fatalf("expected 1, got %d", len(bl))
	}
	if bl[0].Snippet != "" {
		t.Errorf("expected empty snippet, got %q", bl[0].Snippet)
	}
}

// TestGetBacklinks_DbClosed returns ErrDBClosed.
func TestGetBacklinks_DbClosed(t *testing.T) {
	dm := newTestDB(t)
	_ = dm.Close()
	_, err := dm.GetBacklinks("vault", "NB", "Sec", "Target")
	if err != ErrDBClosed {
		t.Errorf("expected ErrDBClosed, got %v", err)
	}
}

// TestSortBacklinks_Stable verifies that sortBacklinks is a stable sort:
// equal elements preserve their original order.
func TestSortBacklinks_Stable(t *testing.T) {
	original := []Backlink{
		{Kind: BacklinkBlockRef, SourceNotebook: "AA", SourceSection: "S", SourcePage: "P", SourceBlockID: uuidA},
		{Kind: BacklinkPageLink, SourceNotebook: "AA", SourceSection: "S", SourcePage: "P", SourceBlockID: uuidB},
	}
	sorted := make([]Backlink, len(original))
	copy(sorted, original)
	sortBacklinks(sorted)

	// block-ref < embed < page alphabetically, so block-ref comes first.
	if sorted[0].Kind != BacklinkBlockRef {
		t.Errorf("block-ref should come before page-link, got %+v", sorted)
	}
	// Both have same (nb, sec, page), so kind order determines position.
	if sorted[1].Kind != BacklinkPageLink {
		t.Errorf("page-link should come second, got %+v", sorted)
	}
}

// TestGetBacklinks_UnrelatedTokenFalsePositive verifies that a block matched
// by LIKE for a target embed {{embed:uuidA}} that ALSO contains an unrelated
// ((uuidX)) block-ref token (not a target block) is classified ONLY as an embed,
// NOT as a block-ref. The old strings.Contains("((") approach would
// incorrectly mark it as both.
func TestGetBacklinks_UnrelatedTokenFalsePositive(t *testing.T) {
	dm := newTestDB(t)
	// Target page: only uuidA.
	idx(t, dm, "vault", "NB", "Sec", "Target", []parser.ParsedBlock{
		noteBlock(uuidA, "target content"),
	})
	// Source block: embeds target uuidA, but also references some unrelated uuidD
	// that does NOT belong to any target page block.
	raw := "see this embed {{embed:" + uuidA + "}} and unrelated ref ((" + uuidD + "))"
	idx(t, dm, "vault", "NB", "Sec", "Source", []parser.ParsedBlock{
		noteBlock(uuidB, raw),
	})

	bl, err := dm.GetBacklinks("vault", "NB", "Sec", "Target")
	if err != nil {
		t.Fatalf("GetBacklinks: %v", err)
	}
	if len(bl) != 1 {
		t.Fatalf("expected 1 backlink (embed only), got %d: %+v", len(bl), bl)
	}
	if bl[0].Kind != BacklinkEmbed {
		t.Errorf("expected embed, got %q (block-ref from unrelated token leaked)", bl[0].Kind)
	}
}

// TestGetBacklinks_UnrelatedEmbedFalsePositive verifies the reverse: a block
// matched for target block-ref ((uuidA)) that also contains an unrelated
// {{embed:uuidX}} is classified ONLY as a block-ref.
func TestGetBacklinks_UnrelatedEmbedFalsePositive(t *testing.T) {
	dm := newTestDB(t)
	idx(t, dm, "vault", "NB", "Sec", "Target", []parser.ParsedBlock{
		noteBlock(uuidA, "target content"),
	})
	// Source: block-ref to target, plus unrelated embed.
	idx(t, dm, "vault", "NB", "Sec", "Source", []parser.ParsedBlock{
		noteBlock(uuidB, "ref "+("(("+uuidA+"))")+" and unrelated "+"{{embed:"+uuidD+"}}"),
	})

	bl, err := dm.GetBacklinks("vault", "NB", "Sec", "Target")
	if err != nil {
		t.Fatalf("GetBacklinks: %v", err)
	}
	if len(bl) != 1 {
		t.Fatalf("expected 1 backlink (block-ref only), got %d: %+v", len(bl), bl)
	}
	if bl[0].Kind != BacklinkBlockRef {
		t.Errorf("expected block-ref, got %q (embed from unrelated token leaked)", bl[0].Kind)
	}
}

// TestGetBacklinks_CrossSourceDedup verifies that identically named blocks in
// different sources (vault vs linked) are NOT collapsed by the dedupe key.
func TestGetBacklinks_CrossSourceDedup(t *testing.T) {
	dm := newTestDB(t)
	idx(t, dm, "vault", "NB", "Sec", "Target", []parser.ParsedBlock{
		noteBlock(uuidA, "vault target"),
	})
	// Two source blocks with same notebook/section/page/block-id but different
	// sources — the shared block ID is valid because each source is a separate
	// notebook tree.
	vaultRaw := "vault ref ((" + uuidA + "))"
	idx(t, dm, "vault", "NB", "Sec", "Source", []parser.ParsedBlock{
		noteBlock(uuidB, vaultRaw),
	})
	linkedRaw := "linked ref ((" + uuidA + "))"
	idx(t, dm, "linked:ext", "NB", "Sec", "Source", []parser.ParsedBlock{
		noteBlock(uuidC, linkedRaw),
	})

	bl, err := dm.GetBacklinks("vault", "NB", "Sec", "Target")
	if err != nil {
		t.Fatalf("GetBacklinks: %v", err)
	}
	if len(bl) != 2 {
		t.Fatalf("expected 2 backlinks (vault + linked), dedup collapsed across source, got %d: %+v", len(bl), bl)
	}
	sources := map[string]bool{}
	for _, b := range bl {
		sources[b.Source] = true
	}
	if !sources["vault"] || !sources["linked:ext"] {
		t.Errorf("expected both sources, got %v", sources)
	}
}

// TestBacklink_JSONShape verifies that the Backlink struct serializes with
// the frontend-expected field names (linkKind, not kind).
func TestBacklink_JSONShape(t *testing.T) {
	bl := Backlink{
		Kind:           BacklinkPageLink,
		Source:         "vault",
		SourceNotebook: "NB",
		SourceSection:  "Sec",
		SourcePage:     "Page",
		SourceBlockID:  uuidA,
		Snippet:        "hello world",
	}
	data, err := json.Marshal(bl)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	s := string(data)
	if strings.Contains(s, `"kind":`) {
		t.Errorf("JSON should use linkKind, not kind: %s", s)
	}
	if !strings.Contains(s, `"linkKind":`) {
		t.Errorf("JSON missing linkKind field: %s", s)
	}
	if !strings.Contains(s, `"source_block_id":`) {
		t.Errorf("JSON missing source_block_id: %s", s)
	}
}
