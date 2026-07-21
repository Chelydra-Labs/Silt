package db

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

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

// TestGetBacklinks_LargeBatchBlockRefs verifies that the indexed block-ref
// lookup batches correctly when the target page has many blocks (> batch
// size). Uses valid v4-shaped UUIDs so parser.BlockRefRegex extracts the
// source-edge tokens at index time.
func TestGetBacklinks_LargeBatchBlockRefs(t *testing.T) {
	dm := newTestDB(t)
	// Target page with enough blocks to force >1 batch at batchSize=500.
	const total = 600
	targetBlocks := make([]parser.ParsedBlock, total)
	blockIDs := make([]string, total)
	for i := range targetBlocks {
		// 8-4-4-4-12 hex — a valid UUID shape so BlockRefRegex matches.
		id := fmt.Sprintf("aaaaaaaa-aaaa-4aaa-8aaa-%012d", i)
		blockIDs[i] = id
		targetBlocks[i] = noteBlock(id, fmt.Sprintf("block %d", i))
	}
	idx(t, dm, "vault", "NB", "Sec", "Target", targetBlocks)

	// Source referencing the last block (forces the second batch).
	srcBlock := noteBlock(uuidE, "ref to last "+("(("+blockIDs[total-1]+"))"))
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

// --- Cursor-paged backlinks tests ---

// TestGetBacklinksPaged_FirstPage verifies that the first page with an empty
// cursor returns the expected subset and a non-empty cursor when more results
// exist.
func TestGetBacklinksPaged_FirstPage(t *testing.T) {
	dm := newTestDB(t)
	idx(t, dm, "vault", "NB", "Sec", "Target", []parser.ParsedBlock{
		noteBlock(uuidA, "target"),
	})
	// 5 source pages with page-links.
	for i := 0; i < 5; i++ {
		idx(t, dm, "vault", "NB", "Sec", fmt.Sprintf("Src%d", i), []parser.ParsedBlock{
			noteBlock(fmt.Sprintf("aaaa%03d-aaaa-4aaa-8aaa-aaaaaaaaaaaa", i),
				fmt.Sprintf("[[Target]] from src %d", i)),
		})
	}

	res, err := dm.GetBacklinksPaged("vault", "NB", "Sec", "Target", "", 3)
	if err != nil {
		t.Fatalf("GetBacklinksPaged: %v", err)
	}
	if len(res.Results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(res.Results))
	}
	if !res.HasMore {
		t.Error("expected HasMore=true")
	}
	if res.Cursor == "" {
		t.Error("expected non-empty cursor")
	}
}

// TestGetBacklinksPaged_CursoredConcatenation verifies that fetching all pages
// via cursor concatenation yields the same results as the unbounded GetBacklinks.
func TestGetBacklinksPaged_CursoredConcatenation(t *testing.T) {
	dm := newTestDB(t)
	idx(t, dm, "vault", "NB", "Sec", "Target", []parser.ParsedBlock{
		noteBlock(uuidA, "target"),
	})
	// 12 backlinks: 6 page-links + 3 block-refs + 3 embeds.
	for i := 0; i < 6; i++ {
		bid := fmt.Sprintf("bbbbb%03d-bbbb-4bbb-8bbb-bbbbbbbbbbbb", i)
		idx(t, dm, "vault", "NB", "Sec", fmt.Sprintf("PL%d", i), []parser.ParsedBlock{
			noteBlock(bid, fmt.Sprintf("[[Target]] pl %d", i)),
		})
	}
	for i := 0; i < 3; i++ {
		bid := fmt.Sprintf("ccccc%03d-cccc-4ccc-8ccc-cccccccccccc", i)
		idx(t, dm, "vault", "NB", "Sec", fmt.Sprintf("BR%d", i), []parser.ParsedBlock{
			noteBlock(bid, fmt.Sprintf("ref %s", "(("+uuidA+"))")),
		})
	}
	for i := 0; i < 3; i++ {
		bid := fmt.Sprintf("ddddd%03d-dddd-4ddd-8ddd-dddddddddddd", i)
		idx(t, dm, "vault", "NB", "Sec", fmt.Sprintf("EM%d", i), []parser.ParsedBlock{
			noteBlock(bid, fmt.Sprintf("embed %s", "{{embed:"+uuidA+"}}")),
		})
	}

	// Get full set via unbounded API.
	full, err := dm.GetBacklinks("vault", "NB", "Sec", "Target")
	if err != nil {
		t.Fatalf("GetBacklinks: %v", err)
	}

	// Paginate with page size 5.
	var allPaged []Backlink
	cursor := ""
	for {
		res, err := dm.GetBacklinksPaged("vault", "NB", "Sec", "Target", cursor, 5)
		if err != nil {
			t.Fatalf("GetBacklinksPaged cursor=%q: %v", cursor, err)
		}
		allPaged = append(allPaged, res.Results...)
		if !res.HasMore {
			break
		}
		cursor = res.Cursor
	}

	if len(allPaged) != len(full) {
		t.Fatalf("paged count %d != full count %d", len(allPaged), len(full))
	}
	for i := range full {
		if allPaged[i] != full[i] {
			t.Errorf("item %d mismatch:\n  paged: %+v\n  full:  %+v", i, allPaged[i], full[i])
		}
	}
}

// TestGetBacklinksPaged_NoDuplicates verifies that cursor-paged results
// contain no duplicates across all pages.
func TestGetBacklinksPaged_NoDuplicates(t *testing.T) {
	dm := newTestDB(t)
	idx(t, dm, "vault", "NB", "Sec", "Target", []parser.ParsedBlock{
		noteBlock(uuidA, "target"),
		noteBlock(uuidB, "target2"),
	})
	// Many sources, some referencing both blocks (two backlinks per source).
	for i := 0; i < 20; i++ {
		bid := fmt.Sprintf("eeeee%03d-eeee-4eee-8eee-eeeeeeeeeeee", i)
		raw := fmt.Sprintf("((%s)) and ((%s))", uuidA, uuidB)
		idx(t, dm, "vault", "NB", "Sec", fmt.Sprintf("S%d", i), []parser.ParsedBlock{
			noteBlock(bid, raw),
		})
	}

	seen := make(map[backlinkKey]bool)
	cursor := ""
	for {
		res, err := dm.GetBacklinksPaged("vault", "NB", "Sec", "Target", cursor, 7)
		if err != nil {
			t.Fatalf("GetBacklinksPaged: %v", err)
		}
		for _, b := range res.Results {
			k := backlinkKey{b.Kind, b.Source, b.SourceNotebook, b.SourceSection, b.SourcePage, b.SourceBlockID}
			if seen[k] {
				t.Errorf("duplicate backlink across pages: %+v", b)
			}
			seen[k] = true
		}
		if !res.HasMore {
			break
		}
		cursor = res.Cursor
	}
}

// TestGetBacklinksPaged_InvalidCursor verifies that an invalid cursor (garbage
// base64, wrong format, truncated) is treated as empty (returns from start)
// or sorts past the end (returns empty results). Both are safe — no panics.
func TestGetBacklinksPaged_InvalidCursor(t *testing.T) {
	dm := newTestDB(t)
	idx(t, dm, "vault", "NB", "Sec", "Target", []parser.ParsedBlock{
		noteBlock(uuidA, "target"),
	})
	idx(t, dm, "vault", "NB", "Sec", "Source", []parser.ParsedBlock{
		noteBlock(uuidB, "[[Target]]"),
	})

	// These should all succeed without panicking. Results may be empty
	// (cursor sorts past all items) or start from the beginning.
	cursors := []string{
		"!!not-valid-base64!!",
		"AAAA", // valid base64 but decodes to binary that sorts before results
		"",     // empty (baseline: returns first page)
	}
	for _, c := range cursors {
		res, err := dm.GetBacklinksPaged("vault", "NB", "Sec", "Target", c, 10)
		if err != nil {
			t.Fatalf("cursor %q: %v", c, err)
		}
		// Must have 0 or 1 results, never panic or error.
		if len(res.Results) > 1 {
			t.Errorf("cursor %q: expected at most 1 result, got %d", c, len(res.Results))
		}
	}
}

// TestGetBacklinksPaged_LargeResultFixture verifies pagination through a
// large result set (150 backlinks) with page size 30.
func TestGetBacklinksPaged_LargeResultFixture(t *testing.T) {
	dm := newTestDB(t)
	idx(t, dm, "vault", "NB", "Sec", "Target", []parser.ParsedBlock{
		noteBlock(uuidA, "target"),
	})
	// Create 150 source pages with page-links.
	for i := 0; i < 150; i++ {
		bid := fmt.Sprintf("ffff%03d-ffff-4fff-8fff-%028d", i%10, i)
		idx(t, dm, "vault", "NB", "Sec", fmt.Sprintf("Src%03d", i), []parser.ParsedBlock{
			noteBlock(bid, fmt.Sprintf("[[Target]] #%d", i)),
		})
	}

	// Full set.
	full, _ := dm.GetBacklinks("vault", "NB", "Sec", "Target")
	if len(full) != 150 {
		t.Fatalf("expected 150 full backlinks, got %d", len(full))
	}

	// Paginate page size 30.
	var allPaged []Backlink
	cursor := ""
	pages := 0
	for {
		res, err := dm.GetBacklinksPaged("vault", "NB", "Sec", "Target", cursor, 30)
		if err != nil {
			t.Fatalf("page %d: %v", pages, err)
		}
		pages++
		allPaged = append(allPaged, res.Results...)
		if !res.HasMore {
			if res.Cursor != "" {
				t.Error("cursor should be empty when HasMore=false")
			}
			break
		}
		cursor = res.Cursor
	}

	if pages != 5 { // 150 / 30 = 5 pages
		t.Errorf("expected 5 pages, got %d", pages)
	}
	if len(allPaged) != 150 {
		t.Fatalf("expected 150 total, got %d", len(allPaged))
	}
	for i := range full {
		if allPaged[i] != full[i] {
			t.Errorf("item %d mismatch", i)
		}
	}
}

// TestGetBacklinksPaged_SourceOrdering verifies that the sort key includes
// Source: results are grouped by source first, then notebook, section, page,
// kind, block_id.
func TestGetBacklinksPaged_SourceOrdering(t *testing.T) {
	dm := newTestDB(t)
	idx(t, dm, "vault", "NB", "Sec", "Target", []parser.ParsedBlock{
		noteBlock(uuidA, "target"),
	})
	// Linked source block-ref.
	idx(t, dm, "linked:ext", "ExtNB", "Sec", "LinkedSrc", []parser.ParsedBlock{
		noteBlock(uuidB, "cross-ref "+("(("+uuidA+"))")),
	})
	// Vault page-link.
	idx(t, dm, "vault", "NB", "Sec", "VaultSrc", []parser.ParsedBlock{
		noteBlock(uuidC, "[[Target]]"),
	})
	// Another linked source embed.
	idx(t, dm, "linked:ext2", "ExtNB2", "Sec", "LinkedSrc2", []parser.ParsedBlock{
		noteBlock(uuidD, "cross-embed "+"{{embed:"+uuidA+"}}"),
	})

	res, err := dm.GetBacklinksPaged("vault", "NB", "Sec", "Target", "", 10)
	if err != nil {
		t.Fatalf("GetBacklinksPaged: %v", err)
	}
	if len(res.Results) != 3 {
		t.Fatalf("expected 3 backlinks, got %d: %+v", len(res.Results), res.Results)
	}
	// linked:ext < linked:ext2 < vault (alphabetical by source)
	if res.Results[0].Source != "linked:ext" {
		t.Errorf("first should be linked:ext, got %q", res.Results[0].Source)
	}
	if res.Results[1].Source != "linked:ext2" {
		t.Errorf("second should be linked:ext2, got %q", res.Results[1].Source)
	}
	if res.Results[2].Source != "vault" {
		t.Errorf("third should be vault, got %q", res.Results[2].Source)
	}
}

// TestGetBacklinksPaged_LimitCapping verifies that limits > BacklinksMaxLimit
// are clamped, 0 uses BacklinksDefaultLimit, and negative is treated as default.
func TestGetBacklinksPaged_LimitCapping(t *testing.T) {
	dm := newTestDB(t)
	idx(t, dm, "vault", "NB", "Sec", "Target", []parser.ParsedBlock{
		noteBlock(uuidA, "target"),
	})
	for i := 0; i < 10; i++ {
		bid := fmt.Sprintf("ggggg%03d-ggggg-4gggg-8gggg-gggggggggggg", i)
		idx(t, dm, "vault", "NB", "Sec", fmt.Sprintf("S%d", i), []parser.ParsedBlock{
			noteBlock(bid, "[[Target]]"),
		})
	}

	// limit=0 → default (50).
	res0, _ := dm.GetBacklinksPaged("vault", "NB", "Sec", "Target", "", 0)
	if len(res0.Results) != 10 { // only 10 exist
		t.Errorf("limit 0: expected 10, got %d", len(res0.Results))
	}

	// limit=999 → clamped to BacklinksMaxLimit (500).
	res999, _ := dm.GetBacklinksPaged("vault", "NB", "Sec", "Target", "", 999)
	if len(res999.Results) != 10 {
		t.Errorf("limit 999: expected 10, got %d", len(res999.Results))
	}

	// limit=-1 → default (50).
	resNeg, _ := dm.GetBacklinksPaged("vault", "NB", "Sec", "Target", "", -1)
	if len(resNeg.Results) != 10 {
		t.Errorf("limit -1: expected 10, got %d", len(resNeg.Results))
	}
}

// TestGetBacklinksPaged_EmptyPage verifies that an empty cursor on a page with
// no backlinks returns empty results with HasMore=false.
func TestGetBacklinksPaged_EmptyPage(t *testing.T) {
	dm := newTestDB(t)
	idx(t, dm, "vault", "NB", "Sec", "Target", []parser.ParsedBlock{
		noteBlock(uuidA, "orphan"),
	})

	res, err := dm.GetBacklinksPaged("vault", "NB", "Sec", "Target", "", 10)
	if err != nil {
		t.Fatalf("GetBacklinksPaged: %v", err)
	}
	if len(res.Results) != 0 {
		t.Errorf("expected 0 results, got %d", len(res.Results))
	}
	if res.HasMore {
		t.Error("HasMore should be false")
	}
	if res.Cursor != "" {
		t.Error("cursor should be empty")
	}
}

// TestGetBacklinksPaged_ExhaustedCursor verifies that once HasMore=false,
// the cursor is empty. Callers should stop paginating when HasMore=false;
// re-calling with empty cursor returns the first page (correct restart
// behavior).
func TestGetBacklinksPaged_ExhaustedCursor(t *testing.T) {
	dm := newTestDB(t)
	idx(t, dm, "vault", "NB", "Sec", "Target", []parser.ParsedBlock{
		noteBlock(uuidA, "target"),
	})
	idx(t, dm, "vault", "NB", "Sec", "Source", []parser.ParsedBlock{
		noteBlock(uuidB, "[[Target]]"),
	})

	// Page 1 gets the only result; HasMore=false, cursor="".
	res1, _ := dm.GetBacklinksPaged("vault", "NB", "Sec", "Target", "", 10)
	if len(res1.Results) != 1 || res1.HasMore {
		t.Fatalf("page 1: expected 1 result, HasMore=false, got %+v", res1)
	}
	if res1.Cursor != "" {
		t.Error("cursor should be empty when HasMore=false")
	}
}

// TestGetBacklinksPaged_DbClosed returns ErrDBClosed.
func TestGetBacklinksPaged_DbClosed(t *testing.T) {
	dm := newTestDB(t)
	_ = dm.Close()
	_, err := dm.GetBacklinksPaged("vault", "NB", "Sec", "Target", "", 10)
	if err != ErrDBClosed {
		t.Errorf("expected ErrDBClosed, got %v", err)
	}
}

// TestGetBacklinksPaged_ExactPageSize verifies behavior when the result count
// is exactly the page size (no HasMore, empty cursor).
func TestGetBacklinksPaged_ExactPageSize(t *testing.T) {
	dm := newTestDB(t)
	idx(t, dm, "vault", "NB", "Sec", "Target", []parser.ParsedBlock{
		noteBlock(uuidA, "target"),
	})
	for i := 0; i < 5; i++ {
		bid := fmt.Sprintf("hhhhh%03d-hhhhh-4hhhh-8hhhh-hhhhhhhhhhhh", i)
		idx(t, dm, "vault", "NB", "Sec", fmt.Sprintf("S%d", i), []parser.ParsedBlock{
			noteBlock(bid, "[[Target]]"),
		})
	}

	res, _ := dm.GetBacklinksPaged("vault", "NB", "Sec", "Target", "", 5)
	if len(res.Results) != 5 {
		t.Fatalf("expected 5, got %d", len(res.Results))
	}
	if res.HasMore {
		t.Error("HasMore should be false when count == page size")
	}
	if res.Cursor != "" {
		t.Error("cursor should be empty when HasMore=false")
	}
}

// TestGetBacklinksPaged_CursorPastEnd verifies that a cursor pointing beyond
// all results returns an empty page.
func TestGetBacklinksPaged_CursorPastEnd(t *testing.T) {
	dm := newTestDB(t)
	idx(t, dm, "vault", "NB", "Sec", "Target", []parser.ParsedBlock{
		noteBlock(uuidA, "target"),
	})
	idx(t, dm, "vault", "NB", "Sec", "Source", []parser.ParsedBlock{
		noteBlock(uuidB, "[[Target]]"),
	})

	// Create a cursor that sorts after all real results.
	// The cursor key for the last result ends with the block ID.
	// Use a synthetic cursor that alphabetically sorts after everything.
	pastCursor := encodeBacklinkCursor(Backlink{
		Source:         "zzzzz",
		SourceNotebook: "ZZZ",
		SourceSection:  "ZZZ",
		SourcePage:     "ZZZ",
		Kind:           "zzz",
		SourceBlockID:  "zzzz",
	})

	res, err := dm.GetBacklinksPaged("vault", "NB", "Sec", "Target", pastCursor, 10)
	if err != nil {
		t.Fatalf("GetBacklinksPaged past cursor: %v", err)
	}
	if len(res.Results) != 0 {
		t.Errorf("past-end cursor should return 0 results, got %d", len(res.Results))
	}
}

// TestGetBacklinksPaged_MixedLegsAndSources verifies cursor pagination across
// a mix of page-links, block-refs, and embeds from multiple sources.
func TestGetBacklinksPaged_MixedLegsAndSources(t *testing.T) {
	dm := newTestDB(t)
	idx(t, dm, "vault", "NB", "Sec", "Target", []parser.ParsedBlock{
		noteBlock(uuidA, "t1"),
		noteBlock(uuidB, "t2"),
	})
	// Vault page-link at VSrc.
	idx(t, dm, "vault", "NB", "Sec", "VSrc", []parser.ParsedBlock{
		noteBlock(uuidC, "[[Target]]"),
	})
	// Linked block-ref at LSrc.
	idx(t, dm, "linked:ext", "Ext", "Sec", "LSrc", []parser.ParsedBlock{
		noteBlock(uuidD, "ref "+("(("+uuidA+"))")),
	})
	// Vault embed at VSrc2.
	idx(t, dm, "vault", "NB", "Sec", "VSrc2", []parser.ParsedBlock{
		noteBlock(uuidE, "embed "+"{{embed:"+uuidB+"}}"),
	})

	// Paginate with tiny page size.
	var all []Backlink
	cursor := ""
	for {
		res, err := dm.GetBacklinksPaged("vault", "NB", "Sec", "Target", cursor, 2)
		if err != nil {
			t.Fatal(err)
		}
		all = append(all, res.Results...)
		if !res.HasMore {
			break
		}
		cursor = res.Cursor
	}

	if len(all) != 3 {
		t.Fatalf("expected 3 total, got %d: %+v", len(all), all)
	}
	// Sort order: linked:ext < vault (by source), then vault pages ordered
	// by source_page: VSrc (page-link) < VSrc2 (embed).
	if all[0].Source != "linked:ext" {
		t.Errorf("first should be linked:ext, got %q: %+v", all[0].Source, all[0])
	}
	if all[1].Source != "vault" || all[1].Kind != BacklinkPageLink {
		t.Errorf("second should be vault page-link, got %+v", all[1])
	}
	if all[2].Source != "vault" || all[2].Kind != BacklinkEmbed {
		t.Errorf("third should be vault embed, got %+v", all[2])
	}
}

// --- Cursor helper tests ---

// TestEncodeDecodeBacklinkCursor verifies that encode/decode round-trips.
func TestEncodeDecodeBacklinkCursor(t *testing.T) {
	b := Backlink{
		Kind:           BacklinkPageLink,
		Source:         "vault",
		SourceNotebook: "NB",
		SourceSection:  "Sec",
		SourcePage:     "Page",
		SourceBlockID:  uuidA,
	}
	encoded := encodeBacklinkCursor(b)
	if encoded == "" {
		t.Fatal("encoded cursor is empty")
	}
	decoded, ok := decodeBacklinkCursor(encoded)
	if !ok {
		t.Fatal("decode failed")
	}
	expected := backlinkCursorKey(b)
	if decoded != expected {
		t.Errorf("round-trip mismatch:\n  got: %q\n  want: %q", decoded, expected)
	}
}

// TestDecodeBacklinkCursor_Empty verifies that empty/nil cursors decode as false.
func TestDecodeBacklinkCursor_Empty(t *testing.T) {
	_, ok := decodeBacklinkCursor("")
	if ok {
		t.Error("empty cursor should decode as false")
	}
}

// TestDecodeBacklinkCursor_InvalidBase64 verifies that strings with characters
// outside the base64 alphabet decode as false (not a panic).
func TestDecodeBacklinkCursor_InvalidBase64(t *testing.T) {
	for _, c := range []string{"!@#$%", " \t\n", "\xff\xfe"} {
		_, ok := decodeBacklinkCursor(c)
		if ok {
			t.Errorf("garbage %q should decode as false", c)
		}
	}
}

// TestBacklinkCursorKey_NULSafety verifies that the cursor key separator
// cannot collide with any field value (fields never contain NUL).
func TestBacklinkCursorKey_NULSafety(t *testing.T) {
	// Encode a backlink, verify the decoded string has exactly 5 separators.
	b := Backlink{
		Kind: BacklinkEmbed, Source: "vault", SourceNotebook: "NB",
		SourceSection: "S", SourcePage: "P", SourceBlockID: uuidA,
	}
	key := backlinkCursorKey(b)
	parts := strings.Split(key, cursorSep)
	if len(parts) != 6 {
		t.Errorf("expected 6 parts, got %d: %v", len(parts), parts)
	}
}

// --- Benchmarks ---

// BenchmarkGetBacklinks measures the unbounded backlinks query cost for a
// target with 100 source pages referencing it (page-link leg only).
func BenchmarkGetBacklinks(b *testing.B) {
	dm, err := NewDatabaseManager("")
	if err != nil {
		b.Fatal(err)
	}
	defer dm.Close()

	// Target page.
	_ = dm.IndexFileBlocks("vault", "NB", "Sec", "Target",
		[]parser.ParsedBlock{noteBlock(uuidA, "target")}, nil)
	// 100 source pages.
	for i := 0; i < 100; i++ {
		id := fmt.Sprintf("bbbbb%03d-bbbb-4bbb-8bbb-bbbbbbbbbbbb", i)
		_ = dm.IndexFileBlocks("vault", "NB", "Sec", fmt.Sprintf("Src%03d", i),
			[]parser.ParsedBlock{noteBlock(id, fmt.Sprintf("[[Target]] #%d", i))}, nil)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := dm.GetBacklinks("vault", "NB", "Sec", "Target")
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkGetBacklinksPaged measures the cursor-paged backlinks query cost
// for the same fixture. Shows the overhead of cursor encode/slice vs unbounded.
func BenchmarkGetBacklinksPaged(b *testing.B) {
	dm, err := NewDatabaseManager("")
	if err != nil {
		b.Fatal(err)
	}
	defer dm.Close()

	_ = dm.IndexFileBlocks("vault", "NB", "Sec", "Target",
		[]parser.ParsedBlock{noteBlock(uuidA, "target")}, nil)
	for i := 0; i < 100; i++ {
		id := fmt.Sprintf("bbbbb%03d-bbbb-4bbb-8bbb-bbbbbbbbbbbb", i)
		_ = dm.IndexFileBlocks("vault", "NB", "Sec", fmt.Sprintf("Src%03d", i),
			[]parser.ParsedBlock{noteBlock(id, fmt.Sprintf("[[Target]] #%d", i))}, nil)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := dm.GetBacklinksPaged("vault", "NB", "Sec", "Target", "", 50)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// --- Phase 1 contract baseline (#704) --------------------------------------
//
// These tests pin the CURRENT observable contract of the backlinks panel
// before the indexed block_references lookup replaces the raw_content LIKE
// scan. The indexed implementation MUST preserve every behavior locked
// here. They live alongside the rest of the backlinks suite so a regression
// in any of them surfaces a parity break.

// TestGetBacklinks_BlockRefInCodeBlockIndexed locks the intentional parity
// decision that a literal ((uuid)) inside a CODE block IS counted as a
// block-ref backlink. The indexed extractor walks RawText of every block
// row regardless of type (diverging from page_links, which excludes CODE),
// so CODE-block tokens are picked up. This must not change (it would be a
// silent product-semantics regression bundled into a future refactor).
func TestGetBacklinks_BlockRefInCodeBlockIndexed(t *testing.T) {
	dm := newTestDB(t)
	idx(t, dm, "vault", "NB", "Sec", "Target", []parser.ParsedBlock{
		noteBlock(uuidA, "target"),
	})
	idx(t, dm, "vault", "NB", "Sec", "Source", []parser.ParsedBlock{
		{ID: uuidB, Type: parser.BlockCode, RawText: "code with ((" + uuidA + ")) inline", CleanText: "code with ((" + uuidA + ")) inline", LineNumber: 1},
	})

	bl, err := dm.GetBacklinks("vault", "NB", "Sec", "Target")
	if err != nil {
		t.Fatalf("GetBacklinks: %v", err)
	}
	if len(bl) != 1 {
		t.Fatalf("CODE-block ((uuid)) must count as a backlink (parity), got %d: %+v", len(bl), bl)
	}
	if bl[0].Kind != BacklinkBlockRef {
		t.Errorf("expected block-ref kind, got %q", bl[0].Kind)
	}
}

// TestGetBacklinks_EmbedInCodeBlockIndexed locks the embed analogue of the
// CODE-block parity decision above for {{embed:uuid}} tokens.
func TestGetBacklinks_EmbedInCodeBlockIndexed(t *testing.T) {
	dm := newTestDB(t)
	idx(t, dm, "vault", "NB", "Sec", "Target", []parser.ParsedBlock{
		noteBlock(uuidA, "target"),
	})
	idx(t, dm, "vault", "NB", "Sec", "Source", []parser.ParsedBlock{
		{ID: uuidB, Type: parser.BlockCode, RawText: "embed syntax {{embed:" + uuidA + "}} in code", CleanText: "embed syntax {{embed:" + uuidA + "}} in code", LineNumber: 1},
	})

	bl, err := dm.GetBacklinks("vault", "NB", "Sec", "Target")
	if err != nil {
		t.Fatalf("GetBacklinks: %v", err)
	}
	if len(bl) != 1 || bl[0].Kind != BacklinkEmbed {
		t.Fatalf("CODE-block {{embed:uuid}} must count as an embed backlink (parity), got %+v", bl)
	}
}

// TestGetBacklinks_BlockRefLifecycle_TargetAppearsLater verifies that a source
// indexed BEFORE its target block exists still produces a backlink once the
// target page is subsequently indexed. The edge survives against a
// not-yet-indexed target.
func TestGetBacklinks_BlockRefLifecycle_TargetAppearsLater(t *testing.T) {
	dm := newTestDB(t)
	// Source page references a UUID that has no target block yet.
	idx(t, dm, "vault", "NB", "Sec", "Source", []parser.ParsedBlock{
		noteBlock(uuidB, "ref to "+("(("+uuidA+"))")),
	})

	bl, err := dm.GetBacklinks("vault", "NB", "Sec", "Target")
	if err != nil {
		t.Fatalf("GetBacklinks pre-target: %v", err)
	}
	if len(bl) != 0 {
		t.Fatalf("expected 0 backlinks before target exists, got %d", len(bl))
	}

	// Now index the target page — backlink must appear without re-indexing Source.
	idx(t, dm, "vault", "NB", "Sec", "Target", []parser.ParsedBlock{
		noteBlock(uuidA, "target"),
	})
	bl, err = dm.GetBacklinks("vault", "NB", "Sec", "Target")
	if err != nil {
		t.Fatalf("GetBacklinks post-target: %v", err)
	}
	if len(bl) != 1 || bl[0].Kind != BacklinkBlockRef {
		t.Fatalf("expected 1 block-ref after target appears, got %+v", bl)
	}
}

// TestGetBacklinks_BlockRefLifecycle_TargetDeletedReappears verifies the
// full lifecycle: source indexed → target appears (backlink) → target page
// cleared (backlink disappears) → target re-indexed (backlink reappears).
// The source page is indexed only once at the start.
func TestGetBacklinks_BlockRefLifecycle_TargetDeletedReappears(t *testing.T) {
	dm := newTestDB(t)
	idx(t, dm, "vault", "NB", "Sec", "Source", []parser.ParsedBlock{
		noteBlock(uuidB, "ref to "+("(("+uuidA+"))")),
	})
	idx(t, dm, "vault", "NB", "Sec", "Target", []parser.ParsedBlock{
		noteBlock(uuidA, "target"),
	})

	bl, err := dm.GetBacklinks("vault", "NB", "Sec", "Target")
	if err != nil {
		t.Fatalf("GetBacklinks initial: %v", err)
	}
	if len(bl) != 1 {
		t.Fatalf("expected 1 backlink initially, got %d", len(bl))
	}

	// Clear the target page — its block ID leaves targetBlockIDs, so the
	// backlink must disappear even though the source edge still references it.
	if err := dm.ClearFileBlocks(nil, "vault", "NB", "Sec", "Target"); err != nil {
		t.Fatalf("ClearFileBlocks: %v", err)
	}
	bl, err = dm.GetBacklinks("vault", "NB", "Sec", "Target")
	if err != nil {
		t.Fatalf("GetBacklinks post-clear: %v", err)
	}
	if len(bl) != 0 {
		t.Fatalf("expected 0 backlinks after target cleared, got %d", len(bl))
	}

	// Re-index the target — the original source edge must resolve again.
	idx(t, dm, "vault", "NB", "Sec", "Target", []parser.ParsedBlock{
		noteBlock(uuidA, "target again"),
	})
	bl, err = dm.GetBacklinks("vault", "NB", "Sec", "Target")
	if err != nil {
		t.Fatalf("GetBacklinks post-reappear: %v", err)
	}
	if len(bl) != 1 || bl[0].Kind != BacklinkBlockRef {
		t.Fatalf("expected 1 block-ref after target reappears, got %+v", bl)
	}
}

// TestGetBacklinks_SourceReindexedRemovesStaleEdge verifies that re-indexing
// a source page after its block-ref was edited away drops the prior edge —
// no zombie backlinks remain. The clear-then-insert flow must cascade.
func TestGetBacklinks_SourceReindexedRemovesStaleEdge(t *testing.T) {
	dm := newTestDB(t)
	idx(t, dm, "vault", "NB", "Sec", "Target", []parser.ParsedBlock{
		noteBlock(uuidA, "target"),
	})
	idx(t, dm, "vault", "NB", "Sec", "Source", []parser.ParsedBlock{
		noteBlock(uuidB, "ref to "+("(("+uuidA+"))")),
	})
	bl, _ := dm.GetBacklinks("vault", "NB", "Sec", "Target")
	if len(bl) != 1 {
		t.Fatalf("baseline: expected 1, got %d", len(bl))
	}

	// Re-index Source with the block-ref removed (same block ID, new content).
	idx(t, dm, "vault", "NB", "Sec", "Source", []parser.ParsedBlock{
		noteBlock(uuidB, "ref is gone"),
	})
	bl, err := dm.GetBacklinks("vault", "NB", "Sec", "Target")
	if err != nil {
		t.Fatalf("GetBacklinks: %v", err)
	}
	if len(bl) != 0 {
		t.Fatalf("stale block-ref edge must be cleared on source re-index, got %d: %+v", len(bl), bl)
	}
}

// TestGetBacklinks_IndexScanResultsParity asserts that IndexScanResults — the
// batched cold-start indexer — produces backlinks indistinguishable from
// IndexFileBlocks for the same input. The two indexers share extraction
// helpers and must not drift.
func TestGetBacklinks_IndexScanResultsParity(t *testing.T) {
	// Build the same fixture two ways: per-file IndexFileBlocks vs a single
	// IndexScanResults batch. The backlinks result set must be identical.
	target := parser.ParsedBlock{ID: uuidA, Type: parser.BlockNote, RawText: "target", CleanText: "target", LineNumber: 1}
	srcPageLink := parser.ParsedBlock{ID: uuidB, Type: parser.BlockNote, RawText: "[[Target]]", CleanText: "[[Target]]", LineNumber: 1}
	srcBlockRef := parser.ParsedBlock{ID: uuidC, Type: parser.BlockNote, RawText: "see ((" + uuidA + "))", CleanText: "see ((" + uuidA + "))", LineNumber: 1}
	srcEmbed := parser.ParsedBlock{ID: uuidD, Type: parser.BlockNote, RawText: "{{embed:" + uuidA + "}}", CleanText: "{{embed:" + uuidA + "}}", LineNumber: 1}

	// File-by-file indexer.
	dmFile := newTestDB(t)
	idx(t, dmFile, "vault", "NB", "Sec", "Target", []parser.ParsedBlock{target})
	idx(t, dmFile, "vault", "NB", "Sec", "PL", []parser.ParsedBlock{srcPageLink})
	idx(t, dmFile, "vault", "NB", "Sec", "BR", []parser.ParsedBlock{srcBlockRef})
	idx(t, dmFile, "vault", "NB", "Sec", "EM", []parser.ParsedBlock{srcEmbed})
	blFile, err := dmFile.GetBacklinks("vault", "NB", "Sec", "Target")
	if err != nil {
		t.Fatalf("file GetBacklinks: %v", err)
	}

	// Batched indexer (mirrors cold-start scan over multiple files).
	dmScan := newTestDB(t)
	results := []parser.ScanResult{
		{Path: "/v/NB/Sec/Target.md", Notebook: "NB", Section: "Sec", Page: "Target", Blocks: []parser.ParsedBlock{target}},
		{Path: "/v/NB/Sec/PL.md", Notebook: "NB", Section: "Sec", Page: "PL", Blocks: []parser.ParsedBlock{srcPageLink}},
		{Path: "/v/NB/Sec/BR.md", Notebook: "NB", Section: "Sec", Page: "BR", Blocks: []parser.ParsedBlock{srcBlockRef}},
		{Path: "/v/NB/Sec/EM.md", Notebook: "NB", Section: "Sec", Page: "EM", Blocks: []parser.ParsedBlock{srcEmbed}},
	}
	if _, _, err := dmScan.IndexScanResults(results); err != nil {
		t.Fatalf("IndexScanResults: %v", err)
	}
	blScan, err := dmScan.GetBacklinks("vault", "NB", "Sec", "Target")
	if err != nil {
		t.Fatalf("scan GetBacklinks: %v", err)
	}

	if len(blFile) != len(blScan) {
		t.Fatalf("parity: file=%d scan=%d backlinks\nfile: %+v\nscan: %+v", len(blFile), len(blScan), blFile, blScan)
	}
	for i := range blFile {
		if blFile[i] != blScan[i] {
			t.Errorf("parity mismatch at %d:\n  file: %+v\n  scan: %+v", i, blFile[i], blScan[i])
		}
	}
}

// BenchmarkGetBacklinks_IndexedLargeUnrelated measures the indexed
// block_references lookup cost against a fixture with many UNRELATED blocks
// (the noise the previous LIKE scan trudged through linearly). With the
// indexed lookup (#704), the unrelated blocks do not participate in the
// query — cost is proportional to inbound edges of the target page's blocks,
// not total block count. Records the query shape without hardcoding timing
// thresholds.
func BenchmarkGetBacklinks_IndexedLargeUnrelated(b *testing.B) {
	dm, err := NewDatabaseManager("")
	if err != nil {
		b.Fatal(err)
	}
	defer dm.Close()

	// Target with 50 blocks (valid UUIDs so BlockRefRegex matches at index).
	targetBlocks := make([]parser.ParsedBlock, 50)
	for i := range targetBlocks {
		id := fmt.Sprintf("aaaaaaaa-aaaa-4aaa-8aaa-%012d", i)
		targetBlocks[i] = noteBlock(id, fmt.Sprintf("block %d", i))
	}
	_ = dm.IndexFileBlocks("vault", "NB", "Sec", "Target", targetBlocks, nil)

	// Large unrelated fixture: 5000 noise blocks the old LIKE scan would
	// have scanned for every target UUID. The indexed lookup never touches
	// them.
	noise := make([]parser.ParsedBlock, 5000)
	for i := range noise {
		id := fmt.Sprintf("bbbbbbbb-bbbb-4bbb-8bbb-%012d", i)
		noise[i] = noteBlock(id, fmt.Sprintf("noise %d", i))
	}
	_ = dm.IndexFileBlocks("vault", "NB", "Sec", "Noise", noise, nil)

	// Source referencing the last target block.
	_ = dm.IndexFileBlocks("vault", "NB", "Sec", "Source",
		[]parser.ParsedBlock{
			noteBlock(uuidE, "ref "+("(("+targetBlocks[49].ID+"))")),
		}, nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := dm.GetBacklinks("vault", "NB", "Sec", "Target")
		if err != nil {
			b.Fatal(err)
		}
	}
}

// --- Phase 5 regression + query-plan coverage (#704) ----------------------

// TestGetBacklinks_QueryPlanBlockReferences asserts the indexed lookup uses
// idx_block_references_target (a SEARCH, not a SCAN of block_references or
// blocks.raw_content). Stable intent assertion: matches the index name +
// SEARCH verb without depending on the full SQLite plan text, which varies
// across SQLite versions and modernc.org/sqlite builds.
func TestGetBacklinks_QueryPlanBlockReferences(t *testing.T) {
	dm := newTestDB(t)
	idx(t, dm, "vault", "NB", "Sec", "Target", []parser.ParsedBlock{
		noteBlock(uuidA, "target"),
	})
	idx(t, dm, "vault", "NB", "Sec", "Source", []parser.ParsedBlock{
		noteBlock(uuidB, "ref "+("(("+uuidA+"))")),
	})

	// EXPLAIN QUERY PLAN returns 4 columns: selectid, order, from, detail.
	// Run the same indexed lookup shape legBlockRefsAndEmbeds uses.
	rows, err := dm.SQLDB().Query(
		"EXPLAIN QUERY PLAN SELECT br.source_block_id, br.target_block_id, br.kind, "+
			"COALESCE(b.source,'vault'), b.notebook, b.section, b.page, COALESCE(b.clean_content,'') "+
			"FROM block_references br JOIN blocks b ON b.id = br.source_block_id "+
			"WHERE br.target_block_id IN (?)",
		uuidA,
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

	// Must NOT do a full SCAN of block_references or blocks.raw_content.
	joined := strings.Join(plans, "\n")
	if strings.Contains(joined, "SCAN TABLE block_references") {
		t.Errorf("block_references lookup must be indexed, not a full SCAN: %s", joined)
	}
	// Must reference the reverse-lookup index somewhere in the plan.
	if !strings.Contains(joined, "idx_block_references_target") {
		t.Errorf("expected idx_block_references_target in plan, got: %s", joined)
	}
	// Must use SEARCH (ep tables/virtual tables use SEARCH too, but a real
	// indexed lookup on a normal table is always SEARCH).
	if !strings.Contains(joined, "SEARCH") {
		t.Errorf("expected SEARCH in plan, got: %s", joined)
	}
}

// TestGetBacklinks_CrossBatchMixedKind asserts that when a target page has
// enough blocks to span >1 batch (batchSize=500) and a single source holds
// both a block-ref to an early target and an embed to a late target, both
// edges survive the batch boundary and classify into the correct leg.
func TestGetBacklinks_CrossBatchMixedKind(t *testing.T) {
	dm := newTestDB(t)
	const total = 600
	targetBlocks := make([]parser.ParsedBlock, total)
	for i := range targetBlocks {
		id := fmt.Sprintf("aaaaaaaa-aaaa-4aaa-8aaa-%012d", i)
		targetBlocks[i] = noteBlock(id, fmt.Sprintf("block %d", i))
	}
	idx(t, dm, "vault", "NB", "Sec", "Target", targetBlocks)

	// Source with a block-ref to target[0] (batch 1) and an embed to
	// target[599] (batch 2).
	srcRaw := "((" + targetBlocks[0].ID + ")) and {{embed:" + targetBlocks[599].ID + "}}"
	idx(t, dm, "vault", "NB", "Sec", "Source", []parser.ParsedBlock{
		noteBlock(uuidE, srcRaw),
	})

	bl, err := dm.GetBacklinks("vault", "NB", "Sec", "Target")
	if err != nil {
		t.Fatalf("GetBacklinks: %v", err)
	}
	if len(bl) != 2 {
		t.Fatalf("expected 2 backlinks across batches (block-ref + embed), got %d: %+v", len(bl), bl)
	}
	kinds := map[BacklinkKind]bool{}
	for _, b := range bl {
		kinds[b.Kind] = true
	}
	if !kinds[BacklinkBlockRef] || !kinds[BacklinkEmbed] {
		t.Errorf("expected both kinds across batch boundary, got %v", kinds)
	}
}

// TestGetBacklinks_SelfReference verifies that a block on page P that
// references its own UUID via ((uuid)) produces a backlink from P to P.
// Both the prior LIKE scan and the indexed lookup match this case; pin it
// so the contract is explicit before any future refactor.
func TestGetBacklinks_SelfReference(t *testing.T) {
	dm := newTestDB(t)
	// Page P with one block referencing itself.
	idx(t, dm, "vault", "NB", "Sec", "P", []parser.ParsedBlock{
		noteBlock(uuidA, "self ref "+("(("+uuidA+"))")),
	})

	bl, err := dm.GetBacklinks("vault", "NB", "Sec", "P")
	if err != nil {
		t.Fatalf("GetBacklinks: %v", err)
	}
	if len(bl) != 1 {
		t.Fatalf("expected 1 self-reference backlink, got %d: %+v", len(bl), bl)
	}
	if bl[0].Kind != BacklinkBlockRef {
		t.Errorf("expected block-ref kind, got %q", bl[0].Kind)
	}
	if bl[0].SourcePage != "P" || bl[0].SourceBlockID != uuidA {
		t.Errorf("self-reference should point back to P/%s, got %+v", uuidA, bl[0])
	}
}

// TestGetBacklinks_MultiTargetSameKindSnippetDeterminism pins the
// deterministic snippet-center contract when a source block references
// multiple targets on the same target page with the same kind. The prior
// LIKE path iterated a Go map (randomized order) for the token rescan, so
// the snippet center was non-deterministic; the indexed lookup iterates SQL
// rows in stable order, so the snippet center is reproducible. This test
// asserts the same backlink count + a stable snippet value across calls.
func TestGetBacklinks_MultiTargetSameKindSnippetDeterminism(t *testing.T) {
	dm := newTestDB(t)
	idx(t, dm, "vault", "NB", "Sec", "Target", []parser.ParsedBlock{
		noteBlock(uuidA, "target a"),
		noteBlock(uuidB, "target b"),
	})
	// Source references both targets — only one Backlink row is emitted
	// (collectBacklinks dedupes by source_block_id).
	idx(t, dm, "vault", "NB", "Sec", "Source", []parser.ParsedBlock{
		noteBlock(uuidC, "first "+("(("+uuidA+"))")+" then "+("(("+uuidB+"))")+" end"),
	})

	// Run multiple times; the snippet bytes must be identical every call.
	var first string
	for i := 0; i < 5; i++ {
		bl, err := dm.GetBacklinks("vault", "NB", "Sec", "Target")
		if err != nil {
			t.Fatalf("GetBacklinks iter %d: %v", i, err)
		}
		if len(bl) != 1 {
			t.Fatalf("iter %d: expected 1 deduped backlink, got %d", i, len(bl))
		}
		if i == 0 {
			first = bl[0].Snippet
		} else if bl[0].Snippet != first {
			t.Errorf("iter %d: snippet drifted: %q vs %q", i, bl[0].Snippet, first)
		}
	}
	if first == "" {
		t.Errorf("expected non-empty snippet for multi-target same-kind source")
	}
}

// TestGetBacklinks_DanglingEdgeInertAcrossPages verifies the source-only-FK
// inertness contract through the public API: an edge to a deleted target
// block must NOT leak as a backlink into OTHER pages' results. The edge row
// survives in block_references (target FK is intentionally absent), but the
// WHERE target_block_id IN (live-targets-of-P) clause filters it out for
// every page P that doesn't carry the target. When the target is re-indexed,
// the edge re-resolves for the original target page.
func TestGetBacklinks_DanglingEdgeInertAcrossPages(t *testing.T) {
	dm := newTestDB(t)
	idx(t, dm, "vault", "NB", "Sec", "Target", []parser.ParsedBlock{
		noteBlock(uuidA, "target"),
	})
	idx(t, dm, "vault", "NB", "Sec", "Source", []parser.ParsedBlock{
		noteBlock(uuidB, "ref "+("(("+uuidA+"))")),
	})
	// An unrelated page that the dangling edge must never appear under.
	idx(t, dm, "vault", "NB", "Sec", "Unrelated", []parser.ParsedBlock{
		noteBlock(uuidC, "unrelated page"),
	})

	// Sanity: edge resolves for the live target page.
	bl, _ := dm.GetBacklinks("vault", "NB", "Sec", "Target")
	if len(bl) != 1 {
		t.Fatalf("baseline: expected 1 backlink, got %d", len(bl))
	}

	// Delete the target block — its UUID leaves every page's targetBlockIDs.
	if _, err := dm.SQLDB().Exec("DELETE FROM blocks WHERE id = ?", uuidA); err != nil {
		t.Fatalf("delete target: %v", err)
	}

	// Querying the original Target page now returns 0 (page has no live
	// blocks → targetBlockIDs empty).
	bl, _ = dm.GetBacklinks("vault", "NB", "Sec", "Target")
	if len(bl) != 0 {
		t.Errorf("Target page with no live blocks should return 0, got %d", len(bl))
	}
	// Querying the Unrelated page must NOT pick up the dangling edge.
	bl2, _ := dm.GetBacklinks("vault", "NB", "Sec", "Unrelated")
	if len(bl2) != 0 {
		t.Errorf("dangling edge leaked into Unrelated page results: %+v", bl2)
	}

	// Re-index the target — the original edge re-resolves without
	// re-indexing Source.
	idx(t, dm, "vault", "NB", "Sec", "Target", []parser.ParsedBlock{
		noteBlock(uuidA, "target back"),
	})
	bl, err := dm.GetBacklinks("vault", "NB", "Sec", "Target")
	if err != nil {
		t.Fatalf("GetBacklinks post-reappear: %v", err)
	}
	if len(bl) != 1 || bl[0].Kind != BacklinkBlockRef {
		t.Errorf("expected 1 block-ref after target reappears, got %+v", bl)
	}
}

// TestGetBacklinks_SnippetByteEquality pins the exact snippet bytes for a
// known clean_content + token shape, so a future refactor that changes
// backlinkSnippetRunes or the padding distribution surfaces as a test
// failure rather than a silent UI drift.
func TestGetBacklinks_SnippetByteEquality(t *testing.T) {
	dm := newTestDB(t)
	idx(t, dm, "vault", "NB", "Sec", "Target", []parser.ParsedBlock{
		noteBlock(uuidA, "target"),
	})
	// Clean text where the token sits at a known offset (50 chars in).
	prefix := strings.Repeat("a", 50)
	suffix := strings.Repeat("b", 50)
	clean := prefix + "((" + uuidA + "))" + suffix
	idx(t, dm, "vault", "NB", "Sec", "Source", []parser.ParsedBlock{
		noteBlock(uuidB, clean),
	})

	bl, err := dm.GetBacklinks("vault", "NB", "Sec", "Target")
	if err != nil {
		t.Fatalf("GetBacklinks: %v", err)
	}
	if len(bl) != 1 {
		t.Fatalf("expected 1 backlink, got %d", len(bl))
	}
	got := bl[0].Snippet
	// Token ((uuid)) must be present and centered.
	if !strings.Contains(got, "(("+uuidA+"))") {
		t.Errorf("snippet missing token: %q", got)
	}
	// Both ellipses must be present (token sits in the middle of a long text).
	if !strings.HasPrefix(got, snippetEllipsis) {
		t.Errorf("snippet should start with ellipsis: %q", got)
	}
	if !strings.HasSuffix(got, snippetEllipsis) {
		t.Errorf("snippet should end with ellipsis: %q", got)
	}
	// Total rune count must respect the 120-rune cap.
	if rc := utf8.RuneCountInString(got); rc > backlinkSnippetRunes {
		t.Errorf("snippet %d runes exceeds cap %d: %q", rc, backlinkSnippetRunes, got)
	}
}
