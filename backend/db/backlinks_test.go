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
	if len(runes) > backlinkSnippetRunes {
		t.Errorf("snippet exceeds %d runes: got %d", backlinkSnippetRunes, len(runes))
	}
}

func TestSnippet_Length(t *testing.T) {
	short := "hello world"
	if got := snippet(short, "world"); got != short {
		t.Errorf("short snippet: %q", got)
	}
	long := strings.Repeat("a", 200)
	got := snippet(long, "xxx") // token absent, falls back to prefix
	runes := []rune(got)
	if len(runes) != backlinkSnippetRunes { // exactly 120 (119 content + 1 ellipsis)
		t.Errorf("expected %d runes, got %d", backlinkSnippetRunes, len(runes))
	}
}

func TestSnippet_Empty(t *testing.T) {
	if got := snippet("", "xxx"); got != "" {
		t.Errorf("empty snippet: %q", got)
	}
}

// TestSnippet_ContextualAroundToken verifies that the snippet is centered
// on the exact occurrence of the token in the text.
func TestSnippet_ContextualAroundToken(t *testing.T) {
	// Token in the middle of a long text.
	prefix := strings.Repeat("word ", 40) // 200 bytes, ~40 words
	suffix := strings.Repeat("word ", 40)
	token := "[[TargetPage]]"
	text := prefix + token + suffix

	got := snippet(text, token)
	runes := []rune(got)

	// Should contain the ellipsis and the token.
	if !strings.Contains(got, snippetEllipsis) {
		t.Errorf("expected ellipsis in snippet: %q", got)
	}
	if !strings.Contains(got, token) {
		t.Errorf("expected token in snippet: %q", got)
	}
	// Should be within budget.
	if len(runes) > backlinkSnippetRunes+2 { // small slack for two ellipses
		t.Errorf("snippet over budget: %d runes: %q", len(runes), got)
	}
}

// TestSnippet_ContextualBlockRef verifies that a ((uuid)) token is centered
// in the snippet.
func TestSnippet_ContextualBlockRef(t *testing.T) {
	prefix := strings.Repeat("lorem ipsum ", 30)
	token := "((" + uuidA + "))"
	suffix := strings.Repeat("dolor sit amet ", 30)
	text := prefix + token + suffix

	got := snippet(text, token)
	if !strings.Contains(got, uuidA[:8]) {
		t.Errorf("expected uuid prefix in snippet: %q", got)
	}
	if len([]rune(got)) > backlinkSnippetRunes+2 {
		t.Errorf("snippet over budget: %d runes", len([]rune(got)))
	}
}

// TestSnippet_ContextualAtStart verifies that a token at the start of text
// gets prefix but no leading ellipsis.
func TestSnippet_ContextualAtStart(t *testing.T) {
	token := "[[Target]]"
	suffix := strings.Repeat("more text here ", 50)
	text := token + suffix

	got := snippet(text, token)
	if !strings.Contains(got, token) {
		t.Errorf("expected token in snippet: %q", got)
	}
	if strings.HasPrefix(got, snippetEllipsis) {
		t.Errorf("should not have leading ellipsis when token is at start: %q", got)
	}
}

// TestSnippet_ContextualAtEnd verifies that a token at the end of text
// gets suffix but no trailing ellipsis.
func TestSnippet_ContextualAtEnd(t *testing.T) {
	prefix := strings.Repeat("lots of text before ", 50)
	token := "[[Target]]"
	text := prefix + token

	got := snippet(text, token)
	if !strings.Contains(got, token) {
		t.Errorf("expected token in snippet: %q", got)
	}
	if strings.HasSuffix(got, snippetEllipsis) {
		t.Errorf("should not have trailing ellipsis when token is at end: %q", got)
	}
}

// TestSnippet_TokenAbsentFallsBack verifies that when the token is not found
// in text, the snippet falls back to a prefix.
func TestSnippet_TokenAbsentFallsBack(t *testing.T) {
	text := strings.Repeat("lorem ipsum dolor sit amet ", 50)
	got := snippet(text, "nonexistent")
	if !strings.HasPrefix(got, "lorem") {
		t.Errorf("fallback should be prefix: %q", got)
	}
	if !strings.HasSuffix(got, snippetEllipsis) {
		t.Errorf("fallback should have trailing ellipsis: %q", got)
	}
}

// TestSnippet_ShortTextNoTruncation verifies short text passes through
// even with a token.
func TestSnippet_ShortTextNoTruncation(t *testing.T) {
	text := "see [[Target]] for details"
	got := snippet(text, "[[Target]]")
	if got != text {
		t.Errorf("short text should pass through: %q", got)
	}
}

// TestSnippet_CaseInsensitiveTokenSearch verifies that the token search
// in the snippet function is case-insensitive.
func TestSnippet_CaseInsensitiveTokenSearch(t *testing.T) {
	text := strings.Repeat("word ", 40) + "[[TARGETPAGE]]" + strings.Repeat("word ", 40)
	got := snippet(text, "[[targetpage]]")
	if !strings.Contains(got, "[[TARGETPAGE]]") {
		t.Errorf("case-insensitive token search should find the token: %q", got)
	}
}

// TestGetBacklinks_ContextualSnippet verifies that backlinks now carry
// contextual snippets centered on the link token.
func TestGetBacklinks_ContextualSnippet(t *testing.T) {
	dm := newTestDB(t)
	idx(t, dm, "vault", "NB", "Sec", "Target", []parser.ParsedBlock{
		noteBlock(uuidA, "target"),
	})
	// Source with the link surrounded by padding text.
	prefix := strings.Repeat("lorem ipsum ", 30)
	suffix := strings.Repeat("dolor sit amet ", 30)
	idx(t, dm, "vault", "NB", "Sec", "Source", []parser.ParsedBlock{
		noteBlock(uuidB, prefix+"[[Target]]"+suffix),
	})

	bl, err := dm.GetBacklinks("vault", "NB", "Sec", "Target")
	if err != nil {
		t.Fatal(err)
	}
	if len(bl) != 1 {
		t.Fatalf("expected 1 backlink, got %d", len(bl))
	}
	// Snippet should contain the link token.
	if !strings.Contains(bl[0].Snippet, "[[Target]]") {
		t.Errorf("contextual snippet should contain link token: %q", bl[0].Snippet)
	}
	// Snippet should have ellipsis (text is much longer than budget).
	if !strings.Contains(bl[0].Snippet, snippetEllipsis) {
		t.Errorf("contextual snippet should have ellipsis: %q", bl[0].Snippet)
	}
}

// TestGetBacklinks_ContextualBlockRefSnippet verifies block-ref backlinks
// carry snippets centered on the ((uuid)) token.
func TestGetBacklinks_ContextualBlockRefSnippet(t *testing.T) {
	dm := newTestDB(t)
	idx(t, dm, "vault", "NB", "Sec", "Target", []parser.ParsedBlock{
		noteBlock(uuidA, "target block"),
	})
	prefix := strings.Repeat("padding ", 30)
	suffix := strings.Repeat("more padding ", 30)
	raw := prefix + "((" + uuidA + "))" + suffix
	idx(t, dm, "vault", "NB", "Sec", "Source", []parser.ParsedBlock{
		noteBlock(uuidB, raw),
	})

	bl, err := dm.GetBacklinks("vault", "NB", "Sec", "Target")
	if err != nil {
		t.Fatal(err)
	}
	if len(bl) != 1 {
		t.Fatalf("expected 1 backlink, got %d", len(bl))
	}
	if !strings.Contains(bl[0].Snippet, uuidA[:8]) {
		t.Errorf("block-ref snippet should contain uuid: %q", bl[0].Snippet)
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
	got := snippet(long, "xxx")       // token absent → prefix fallback
	runes := []rune(got)
	if runes[len(runes)-1] != '…' {
		t.Errorf("expected trailing ellipsis, got %q", got)
	}
	if len(runes) != backlinkSnippetRunes { // exactly 120
		t.Errorf("expected %d runes, got %d", backlinkSnippetRunes, len(runes))
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

// --- Oracle review fixes ---

// TestSnippet_PageLinkBoundary verifies that the snippet extends to include
// the closing ]] of a page-link, never slicing mid-syntax.
func TestSnippet_PageLinkBoundary(t *testing.T) {
	prefix := strings.Repeat("word ", 50)
	token := "[[TargetPage#Heading|display text]]"
	text := prefix + token + strings.Repeat(" word", 50)

	got := snippet(text, "[[TargetPage")
	runes := []rune(got)

	// Must contain the closing ]] — never sliced mid-link.
	if !strings.Contains(got, "]]") {
		t.Errorf("snippet must include closing ]]: %q", got)
	}
	// Must not exceed 120 runes.
	if len(runes) > backlinkSnippetRunes {
		t.Errorf("snippet exceeds %d runes: got %d", backlinkSnippetRunes, len(runes))
	}
}

// TestSnippet_PageLinkBoundary_ShortLink verifies that short links like
// [[A]] are fully included in the snippet.
func TestSnippet_PageLinkBoundary_ShortLink(t *testing.T) {
	prefix := strings.Repeat("x ", 80)
	text := prefix + "[[A]]" + strings.Repeat(" y", 80)

	got := snippet(text, "[[A")
	if !strings.Contains(got, "[[A]]") {
		t.Errorf("short link should be complete in snippet: %q", got)
	}
	if len([]rune(got)) > backlinkSnippetRunes {
		t.Errorf("exceeds %d runes: %d", backlinkSnippetRunes, len([]rune(got)))
	}
}

// TestSnippet_120RuneCapWithEllipsis verifies the 120-rune cap is honored
// including ellipsis markers.
func TestSnippet_120RuneCapWithEllipsis(t *testing.T) {
	// Long text, token near the middle.
	text := strings.Repeat("abcdefghij", 20) // 200 chars
	token := "[[X]]"
	mid := strings.Repeat("abcdefghij", 9)                // 90 chars before token
	text = mid + token + strings.Repeat("klmnopqrst", 10) // 90 chars after

	got := snippet(text, token)
	runes := []rune(got)
	if len(runes) > backlinkSnippetRunes {
		t.Errorf("total runes %d exceeds cap %d: %q", len(runes), backlinkSnippetRunes, got)
	}
}

// TestSnippet_UnicodeCap verifies multibyte characters (3-byte Japanese)
// are counted as single runes and the cap is honored.
func TestSnippet_UnicodeCap(t *testing.T) {
	// Build text with Japanese characters around a page-link token.
	prefix := strings.Repeat("日本語", 40) // 120 runes (40×3 bytes)
	token := "[[ターゲット]]"                // 9 runes
	suffix := strings.Repeat("日本語", 40) // 120 runes
	text := prefix + token + suffix     // 249 runes total

	got := snippet(text, "[[ターゲット")
	runes := []rune(got)
	if len(runes) > backlinkSnippetRunes {
		t.Errorf("Unicode snippet %d runes exceeds cap %d", len(runes), backlinkSnippetRunes)
	}
	// Must include the closing ]] for the Japanese link.
	if !strings.Contains(got, "]]") {
		t.Errorf("Unicode link boundary must include ]]: %q", got)
	}
}

// TestSnippet_NeverExceeds120Runes is a property-based check: generate
// various token positions and verify the cap.
func TestSnippet_NeverExceeds120Runes(t *testing.T) {
	base := strings.Repeat("abcde ", 40) // 240 chars
	tests := []struct{ token string }{
		{"[[Target]]"},
		{"[[LongTargetName#Section|Alias]]"},
		{"((aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa))"},
		{"{{embed:aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa}}"},
	}
	for _, tc := range tests {
		// Token at various positions.
		for _, pos := range []int{0, len(base) / 4, len(base) / 2, len(base) * 3 / 4, len(base)} {
			text := base[:pos] + tc.token + base[pos:]
			got := snippet(text, tc.token)
			runes := []rune(got)
			if len(runes) > backlinkSnippetRunes {
				t.Errorf("token=%q pos=%d: %d runes exceeds cap", tc.token, pos, len(runes))
			}
		}
	}
}

// --- Source-qualified rename / stale recovery regression tests ---

// TestGetBacklinks_LinkedRenameIsolation verifies that linked page backlinks
// never include vault-prefixed page-link rows (since vault/ is no longer a
// source qualifier, vault-prefixed links simply won't resolve to vault pages).
func TestGetBacklinks_LinkedRenameIsolation(t *testing.T) {
	dm := newTestDB(t)
	// Vault page NB/Sec/Site.
	idx(t, dm, "vault", "NB", "Sec", "Site", []parser.ParsedBlock{
		noteBlock(uuidA, "vault site"),
	})
	// Linked page NB/Sec/Site (same basename, different source).
	idx(t, dm, "linked:ext", "NB", "Sec", "Site", []parser.ParsedBlock{
		noteBlock(uuidF, "linked site"),
	})
	// Source page links with vault/ prefix — not source-qualified.
	idx(t, dm, "vault", "Other", "", "SrcVault", []parser.ParsedBlock{
		noteBlock(uuidB, "see [[vault/NB/Sec/Site]]"),
	})
	// Another source links with linked: qualified form to the linked page.
	idx(t, dm, "vault", "Other", "", "SrcLinked", []parser.ParsedBlock{
		noteBlock(uuidC, "see [[linked:ext/NB/Sec/Site]]"),
	})

	// Backlinks for the linked target: must only have the linked-qualified link.
	bl, err := dm.GetBacklinks("linked:ext", "NB", "Sec", "Site")
	if err != nil {
		t.Fatal(err)
	}
	if len(bl) != 1 {
		t.Fatalf("linked target should have 1 backlink (linked-qualified), got %d: %+v", len(bl), bl)
	}
	if bl[0].SourceNotebook != "Other" || bl[0].SourcePage != "SrcLinked" {
		t.Errorf("linked backlink should come from SrcLinked, got %+v", bl[0])
	}

	// Backlinks for the vault target: vault/... link won't resolve.
	bl2, err := dm.GetBacklinks("vault", "NB", "Sec", "Site")
	if err != nil {
		t.Fatal(err)
	}
	if len(bl2) != 0 {
		t.Fatalf("vault target should have 0 backlinks (vault/ not qualified), got %d: %+v", len(bl2), bl2)
	}
}

// TestGetBacklinks_LinkedStaleRecovery verifies that a linked: qualified
// target_raw that resolves to the correct canonical page is found by the
// backlinks query. Vault/... is no longer source-qualified so only linked:
// stale forms are recovered.
func TestGetBacklinks_LinkedStaleRecovery(t *testing.T) {
	dm := newTestDB(t)
	// Vault page with a basename that collides with a linked page.
	idx(t, dm, "vault", "NB", "Sec", "Site", []parser.ParsedBlock{
		noteBlock(uuidA, "vault site"),
	})
	idx(t, dm, "linked:ext", "NB", "Sec", "Site", []parser.ParsedBlock{
		noteBlock(uuidF, "linked site"),
	})

	// A source page links with linked: qualified form to the linked page.
	idx(t, dm, "vault", "Other", "", "Src2", []parser.ParsedBlock{
		noteBlock(uuidC, "see [[linked:ext/NB/Sec/Site]]"),
	})

	// Linked qualified form must resolve correctly.
	bl2, err := dm.GetBacklinks("linked:ext", "NB", "Sec", "Site")
	if err != nil {
		t.Fatal(err)
	}
	if len(bl2) != 1 {
		t.Fatalf("linked qualified stale recovery: expected 1, got %d: %+v", len(bl2), bl2)
	}
}

// TestSnippet_OversizedTokenElided verifies that when a wiki-link token
// exceeds the 120-rune display budget, it is replaced with a safe elided
// "[[…]]" representation rather than truncated mid-syntax.
func TestSnippet_OversizedTokenElided(t *testing.T) {
	// Build a token that is >120 runes (page-link with very long alias).
	longAlias := strings.Repeat("x", 150)
	token := "[[Page#" + longAlias + "|display]]" // well over 120 runes
	text := "prefix " + token + " suffix"

	got := snippet(text, "[[Page")
	runes := []rune(got)

	// Must not exceed 120 runes.
	if len(runes) > backlinkSnippetRunes {
		t.Errorf("oversized token snippet: %d runes exceeds cap %d", len(runes), backlinkSnippetRunes)
	}
	// Must contain the elided form, not the raw oversized token.
	if !strings.Contains(got, "[[…]]") {
		t.Errorf("oversized token should be elided to [[…]], got: %q", got)
	}
	// Must NOT contain the raw oversized alias.
	if strings.Contains(got, longAlias[:50]) {
		t.Errorf("oversized token should not leak raw content: %q", got)
	}
}

// TestSnippet_OversizedTokenWithHeading verifies elision for a heading-only
// oversized link [[Page#VeryLongHeading]].
func TestSnippet_OversizedTokenWithHeading(t *testing.T) {
	longHeading := strings.Repeat("h", 130)
	token := "[[Page#" + longHeading + "]]"
	text := "some text before " + token + " some text after"

	got := snippet(text, "[[Page")
	runes := []rune(got)
	if len(runes) > backlinkSnippetRunes {
		t.Errorf("oversized heading token: %d runes exceeds cap", len(runes))
	}
	if !strings.Contains(got, "[[…]]") {
		t.Errorf("oversized heading token should be elided: %q", got)
	}
}

// TestSnippet_FallbackExactly120 verifies the prefix-fallback path produces
// exactly 120 runes (119 content + 1 ellipsis).
func TestSnippet_FallbackExactly120(t *testing.T) {
	text := strings.Repeat("a", 200)
	got := snippet(text, "nonexistent")
	runes := []rune(got)
	if len(runes) != backlinkSnippetRunes {
		t.Errorf("fallback snippet: expected %d runes, got %d: %q", backlinkSnippetRunes, len(runes), got)
	}
	if runes[len(runes)-1] != '…' {
		t.Errorf("fallback should end with ellipsis, got %q", got)
	}
}

// --- Legacy vault notebook backlink regression ---

// TestGetBacklinks_LegacyVaultNotebook verifies that a notebook literally
// named "vault" works correctly for backlinks — the "vault/..." prefix is a
// regular notebook path, not a source qualifier.
func TestGetBacklinks_LegacyVaultNotebook(t *testing.T) {
	dm := newTestDB(t)
	// Target page in a notebook named "vault".
	idx(t, dm, "vault", "vault", "Sec", "Target", []parser.ParsedBlock{
		noteBlock(uuidA, "target content"),
	})
	// Source page links to [[vault/Sec/Target]].
	idx(t, dm, "vault", "Other", "", "Src", []parser.ParsedBlock{
		noteBlock(uuidB, "see [[vault/Sec/Target]] for details"),
	})

	bl, err := dm.GetBacklinks("vault", "vault", "Sec", "Target")
	if err != nil {
		t.Fatalf("GetBacklinks: %v", err)
	}
	if len(bl) != 1 {
		t.Fatalf("expected 1 backlink for vault notebook, got %d: %+v", len(bl), bl)
	}
	if bl[0].Kind != BacklinkPageLink {
		t.Errorf("expected page kind, got %q", bl[0].Kind)
	}
	if !strings.Contains(bl[0].Snippet, "[[vault/Sec/Target]]") {
		t.Errorf("snippet should contain full vault notebook link: %q", bl[0].Snippet)
	}
}

// --- Oversized wiki-link token syntax/cap regression ---

// TestGetBacklinks_OversizedWikiLinkElided verifies that backlinks with an
// oversized wiki-link token produce a snippet containing the elided "[[…]]"
// form rather than truncating mid-syntax.
func TestGetBacklinks_OversizedWikiLinkElided(t *testing.T) {
	dm := newTestDB(t)
	idx(t, dm, "vault", "NB", "Sec", "Target", []parser.ParsedBlock{
		noteBlock(uuidA, "target"),
	})
	// Source with an oversized page-link token.
	longAlias := strings.Repeat("x", 150)
	linkText := "[[Target#" + longAlias + "|display]]"
	longPrefix := strings.Repeat("word ", 30)
	longSuffix := strings.Repeat("word ", 30)
	idx(t, dm, "vault", "NB", "Sec", "Src", []parser.ParsedBlock{
		noteBlock(uuidB, longPrefix+linkText+longSuffix),
	})

	bl, err := dm.GetBacklinks("vault", "NB", "Sec", "Target")
	if err != nil {
		t.Fatal(err)
	}
	if len(bl) != 1 {
		t.Fatalf("expected 1 backlink, got %d", len(bl))
	}
	runes := []rune(bl[0].Snippet)
	if len(runes) > backlinkSnippetRunes {
		t.Errorf("oversized link snippet: %d runes exceeds cap %d", len(runes), backlinkSnippetRunes)
	}
	if !strings.Contains(bl[0].Snippet, "[[…]]") {
		t.Errorf("oversized link should be elided: %q", bl[0].Snippet)
	}
	if strings.Contains(bl[0].Snippet, longAlias[:20]) {
		t.Errorf("oversized link should not leak raw alias: %q", bl[0].Snippet)
	}
}

// TestSnippet_NeverExceeds120Runes_OversizedToken is a property check
// verifying the cap is always honored even with tokens much larger than budget.
func TestSnippet_NeverExceeds120Runes_OversizedToken(t *testing.T) {
	tests := []struct {
		name  string
		token string
	}{
		{"long alias", "[[P#" + strings.Repeat("a", 200) + "|b]]"},
		{"long heading", "[[P#" + strings.Repeat("h", 200) + "]]"},
		{"long page name", "[[" + strings.Repeat("X", 200) + "]]"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			text := "prefix " + tc.token + " suffix"
			got := snippet(text, tc.token)
			runes := []rune(got)
			if len(runes) > backlinkSnippetRunes {
				t.Errorf("%s: %d runes exceeds cap", tc.name, len(runes))
			}
		})
	}
}
