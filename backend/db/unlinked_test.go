package db

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"silt/backend/parser"
)

// uuidU/V/W/X/Y/Z for unlinked-mentions fixtures (kept distinct from the
// backlinks uuidA..F consts so the suites don't share mutable state).
const (
	uuidU = "uuuuuuuu-uuuu-4uuu-8uuu-uuuuuuuuuuuu"
	uuidV = "vvvvvvvv-vvvv-4vvv-8vvv-vvvvvvvvvvvv"
	uuidW = "wwwwwwww-wwww-4www-8www-wwwwwwwwwwww"
	uuidX = "xxxxxxxx-xxxx-4xxx-8xxx-xxxxxxxxxxxx"
	uuidY = "yyyyyyyy-yyyy-4yyy-8yyy-yyyyyyyyyyyy"
	uuidZ = "zzzzzzzz-zzzz-4zzz-8zzz-zzzzzzzzzzzz"
)

func idxU(t *testing.T, dm *DatabaseManager, source, nb, sec, pg string, blocks []parser.ParsedBlock) {
	t.Helper()
	if err := dm.IndexFileBlocks(source, nb, sec, pg, blocks, nil); err != nil {
		t.Fatalf("index %s/%s/%s/%s: %v", source, nb, sec, pg, err)
	}
}

// TestUnlinked_SingleWordTitle verifies a one-word page title is surfaced when
// mentioned in plain prose on another page.
func TestUnlinked_SingleWordTitle(t *testing.T) {
	dm := newTestDB(t)
	idxU(t, dm, "vault", "NB", "Sec", "Onboarding", []parser.ParsedBlock{
		noteBlock(uuidU, "the onboarding guide"),
	})
	idxU(t, dm, "vault", "NB", "Sec", "Notes", []parser.ParsedBlock{
		noteBlock(uuidV, "review the Onboarding steps before launch"),
	})

	res, err := dm.GetUnlinkedMentionsPaged("vault", "NB", "Sec", "Onboarding", "", "", 50)
	if err != nil {
		t.Fatalf("GetUnlinkedMentionsPaged: %v", err)
	}
	if len(res.Results) != 1 {
		t.Fatalf("expected 1 unlinked mention, got %d: %+v", len(res.Results), res.Results)
	}
	m := res.Results[0]
	if m.SourcePage != "Notes" || m.MatchCount != 1 || len(m.SourceBlockIDs) != 1 || m.SourceBlockIDs[0] != uuidV {
		t.Errorf("unexpected mention: %+v", m)
	}
	if m.Title != "Onboarding" {
		t.Errorf("title: got %q want Onboarding", m.Title)
	}
	if m.Ambiguous {
		t.Errorf("unique title should not be ambiguous")
	}
	if res.Truncated {
		t.Error("single match must not report Truncated")
	}
	if len(m.SourceSnippets) != 1 {
		t.Fatalf("expected 1 snippet parallel to block ids, got %d", len(m.SourceSnippets))
	}
	if !strings.Contains(strings.ToLower(m.SourceSnippets[0]), "onboarding") {
		t.Errorf("snippet should contain title, got %q", m.SourceSnippets[0])
	}
}

// TestUnlinked_MultiWordTitle verifies phrase matching for multi-word titles.
func TestUnlinked_MultiWordTitle(t *testing.T) {
	dm := newTestDB(t)
	idxU(t, dm, "vault", "NB", "Sec", "Onboarding Friction", []parser.ParsedBlock{
		noteBlock(uuidU, "define onboarding friction"),
	})
	idxU(t, dm, "vault", "NB", "Sec", "Notes", []parser.ParsedBlock{
		noteBlock(uuidV, "we measured onboarding friction last quarter"),
		noteBlock(uuidW, "onboarding alone is not the same"),        // not the phrase
		noteBlock(uuidX, "friction during onboarding is different"), // reversed order
	})

	res, err := dm.GetUnlinkedMentionsPaged("vault", "NB", "Sec", "Onboarding Friction", "", "", 50)
	if err != nil {
		t.Fatalf("GetUnlinkedMentionsPaged: %v", err)
	}
	if len(res.Results) != 1 {
		t.Fatalf("expected 1 source page, got %d: %+v", len(res.Results), res.Results)
	}
	if res.Results[0].MatchCount != 1 {
		t.Errorf("expected exactly the phrase match (1), got %d", res.Results[0].MatchCount)
	}
}

// TestUnlinked_CaseInsensitive verifies matching is case-insensitive.
func TestUnlinked_CaseInsensitive(t *testing.T) {
	dm := newTestDB(t)
	idxU(t, dm, "vault", "NB", "Sec", "Onboarding", []parser.ParsedBlock{
		noteBlock(uuidU, "Onboarding title"),
	})
	idxU(t, dm, "vault", "NB", "Sec", "Lower", []parser.ParsedBlock{
		noteBlock(uuidV, "see onboarding here"),
	})
	idxU(t, dm, "vault", "NB", "Sec", "Upper", []parser.ParsedBlock{
		noteBlock(uuidW, "see ONBOARDING here"),
	})

	res, err := dm.GetUnlinkedMentionsPaged("vault", "NB", "Sec", "Onboarding", "", "", 50)
	if err != nil {
		t.Fatalf("GetUnlinkedMentionsPaged: %v", err)
	}
	if len(res.Results) != 2 {
		t.Fatalf("expected 2 source pages (lower + upper case mentions), got %d: %+v", len(res.Results), res.Results)
	}
}

// TestUnlinked_WordBoundary verifies that substring matches do not count:
// "Onboarding" must not match "Onboardings" or "pre-onboarding"-style tokens.
func TestUnlinked_WordBoundary(t *testing.T) {
	dm := newTestDB(t)
	idxU(t, dm, "vault", "NB", "Sec", "Target", []parser.ParsedBlock{
		noteBlock(uuidU, "Target page"),
	})
	idxU(t, dm, "vault", "NB", "Sec", "Notes", []parser.ParsedBlock{
		noteBlock(uuidV, "we targeted the audience"),    // "targeted" — not whole word
		noteBlock(uuidW, "multiple targets were found"), // "targets" — not whole word
	})

	res, err := dm.GetUnlinkedMentionsPaged("vault", "NB", "Sec", "Target", "", "", 50)
	if err != nil {
		t.Fatalf("GetUnlinkedMentionsPaged: %v", err)
	}
	if len(res.Results) != 0 {
		t.Fatalf("word-boundary: expected 0 mentions, got %d: %+v", len(res.Results), res.Results)
	}
}

// TestUnlinked_NonAsciiTitle verifies accented titles surface mentions: RE2's
// \b is ASCII-only, so the Unicode-aware boundaries in WordBoundaryTitleRE are
// required for a title like "Café" to match whole-word prose and be promotable.
func TestUnlinked_NonAsciiTitle(t *testing.T) {
	dm := newTestDB(t)
	idxU(t, dm, "vault", "NB", "Sec", "Café", []parser.ParsedBlock{
		noteBlock(uuidU, "Café project"),
	})
	idxU(t, dm, "vault", "NB", "Sec", "Notes", []parser.ParsedBlock{
		noteBlock(uuidV, "le café ouvre bientôt"), // accented whole-word mention — must match
		noteBlock(uuidW, "les cafés sont bons"),   // "cafés" — substring, not whole word
	})

	res, err := dm.GetUnlinkedMentionsPaged("vault", "NB", "Sec", "Café", "", "", 50)
	if err != nil {
		t.Fatalf("GetUnlinkedMentionsPaged: %v", err)
	}
	if len(res.Results) != 1 || res.Results[0].MatchCount != 1 {
		t.Fatalf("non-ascii: expected 1 page / 1 match, got %+v", res.Results)
	}
}

// TestUnlinked_SelfPageExcluded verifies the active page's own blocks are never
// returned as unlinked mentions of themselves.
func TestUnlinked_SelfPageExcluded(t *testing.T) {
	dm := newTestDB(t)
	idxU(t, dm, "vault", "NB", "Sec", "Onboarding", []parser.ParsedBlock{
		noteBlock(uuidU, "this Onboarding page mentions Onboarding"),
	})
	idxU(t, dm, "vault", "NB", "Sec", "Notes", []parser.ParsedBlock{
		noteBlock(uuidV, "see Onboarding"),
	})

	res, err := dm.GetUnlinkedMentionsPaged("vault", "NB", "Sec", "Onboarding", "", "", 50)
	if err != nil {
		t.Fatalf("GetUnlinkedMentionsPaged: %v", err)
	}
	if len(res.Results) != 1 {
		t.Fatalf("expected only the Notes page (self excluded), got %d: %+v", len(res.Results), res.Results)
	}
	if res.Results[0].SourcePage != "Notes" {
		t.Errorf("expected Notes, got %q", res.Results[0].SourcePage)
	}
}

// TestUnlinked_CodeBlocksExcluded verifies CODE blocks are not surfaced.
func TestUnlinked_CodeBlocksExcluded(t *testing.T) {
	dm := newTestDB(t)
	idxU(t, dm, "vault", "NB", "Sec", "Onboarding", []parser.ParsedBlock{
		noteBlock(uuidU, "Onboarding"),
	})
	idxU(t, dm, "vault", "NB", "Sec", "Notes", []parser.ParsedBlock{
		{ID: uuidV, Type: parser.BlockCode, RawText: "const Onboarding = 1", CleanText: "const Onboarding = 1", LineNumber: 1},
	})

	res, err := dm.GetUnlinkedMentionsPaged("vault", "NB", "Sec", "Onboarding", "", "", 50)
	if err != nil {
		t.Fatalf("GetUnlinkedMentionsPaged: %v", err)
	}
	if len(res.Results) != 0 {
		t.Fatalf("CODE-block mention should be excluded, got %d: %+v", len(res.Results), res.Results)
	}
}

// TestUnlinked_ResidualPlainInMixedBlock verifies a block that already has a
// [[…]] to the target still surfaces when residual plain title text remains
// (list/promote parity). Fully-linked-only blocks stay excluded.
func TestUnlinked_ResidualPlainInMixedBlock(t *testing.T) {
	dm := newTestDB(t)
	idxU(t, dm, "vault", "NB", "Sec", "Onboarding", []parser.ParsedBlock{
		noteBlock(uuidU, "Onboarding"),
	})
	// Mixed: linked + residual plain — must appear.
	idxU(t, dm, "vault", "NB", "Sec", "Mixed", []parser.ParsedBlock{
		noteBlock(uuidV, "see [[Onboarding]] for the Onboarding details"),
	})
	// Linked only — must not appear.
	idxU(t, dm, "vault", "NB", "Sec", "LinkedOnly", []parser.ParsedBlock{
		noteBlock(uuidW, "see [[Onboarding]] only"),
	})
	// Plain only — still appears.
	idxU(t, dm, "vault", "NB", "Sec", "Plain", []parser.ParsedBlock{
		noteBlock(uuidX, "see Onboarding details"),
	})

	res, err := dm.GetUnlinkedMentionsPaged("vault", "NB", "Sec", "Onboarding", "", "", 50)
	if err != nil {
		t.Fatalf("GetUnlinkedMentionsPaged: %v", err)
	}
	got := map[string]UnlinkedMention{}
	for _, m := range res.Results {
		got[m.SourcePage] = m
	}
	if _, ok := got["LinkedOnly"]; ok {
		t.Errorf("fully-linked-only page must be excluded, got %+v", got["LinkedOnly"])
	}
	if _, ok := got["Plain"]; !ok {
		t.Errorf("plain-only page missing: %+v", res.Results)
	}
	mixed, ok := got["Mixed"]
	if !ok {
		t.Fatalf("mixed linked+plain page missing: %+v", res.Results)
	}
	if mixed.MatchCount != 1 || len(mixed.SourceBlockIDs) != 1 || mixed.SourceBlockIDs[0] != uuidV {
		t.Errorf("mixed mention unexpected: %+v", mixed)
	}
	if len(mixed.SourceSnippets) != 1 {
		t.Fatalf("expected 1 residual snippet, got %d", len(mixed.SourceSnippets))
	}
	// Snippet must center on residual plain "Onboarding", not only the [[…]] hit.
	// The plain occurrence is preceded by "the ", so that context should appear.
	snip := mixed.SourceSnippets[0]
	if !strings.Contains(strings.ToLower(snip), "onboarding") {
		t.Errorf("snippet missing title: %q", snip)
	}
	if !strings.Contains(snip, "details") && !strings.Contains(strings.ToLower(snip), "the onboarding") {
		t.Errorf("snippet should reflect residual plain context, got %q", snip)
	}
}

// TestUnlinked_LinkedOnlyBlockExcluded verifies a block whose only title hit is
// inside [[…]] is not surfaced (no residual plain).
func TestUnlinked_LinkedOnlyBlockExcluded(t *testing.T) {
	dm := newTestDB(t)
	idxU(t, dm, "vault", "NB", "Sec", "Onboarding", []parser.ParsedBlock{
		noteBlock(uuidU, "Onboarding"),
	})
	idxU(t, dm, "vault", "NB", "Sec", "Notes", []parser.ParsedBlock{
		noteBlock(uuidV, "see [[Onboarding]] and nothing else"),
	})

	res, err := dm.GetUnlinkedMentionsPaged("vault", "NB", "Sec", "Onboarding", "", "", 50)
	if err != nil {
		t.Fatalf("GetUnlinkedMentionsPaged: %v", err)
	}
	if len(res.Results) != 0 {
		t.Fatalf("linked-only: expected 0 mentions, got %d: %+v", len(res.Results), res.Results)
	}
}

// TestFirstPlainTitleOccurrence verifies residual detection skips [[…]] spans.
func TestFirstPlainTitleOccurrence(t *testing.T) {
	re := WordBoundaryTitleRE("Onboarding")
	_, _, ok := FirstPlainTitleOccurrence("see [[Onboarding]] only", re)
	if ok {
		t.Error("linked-only should have no residual plain")
	}
	start, end, ok := FirstPlainTitleOccurrence("see [[Onboarding]] and Onboarding too", re)
	if !ok {
		t.Fatal("expected residual plain")
	}
	got := "see [[Onboarding]] and Onboarding too"[start:end]
	if !strings.EqualFold(got, "Onboarding") {
		t.Errorf("residual span %q", got)
	}
	// Prefer the plain hit after the link, not the title inside [[…]].
	if start < strings.Index("see [[Onboarding]] and Onboarding too", "]]") {
		t.Errorf("residual start %d should be after the wiki link", start)
	}
}

// TestUnlinked_ResidualSnippetCentersOnPlain verifies long mixed blocks produce
// a snippet window anchored on the residual plain title, not the earlier [[…]].
func TestUnlinked_ResidualSnippetCentersOnPlain(t *testing.T) {
	dm := newTestDB(t)
	idxU(t, dm, "vault", "NB", "Sec", "Onboarding", []parser.ParsedBlock{
		noteBlock(uuidU, "Onboarding"),
	})
	// >120 runes: early wiki link, then a distant residual plain hit.
	prefix := strings.Repeat("x", 80)
	mid := strings.Repeat("y", 80)
	suffix := strings.Repeat("z", 80)
	clean := prefix + "[[Onboarding]]" + mid + " Onboarding " + suffix
	idxU(t, dm, "vault", "NB", "Sec", "Long", []parser.ParsedBlock{
		noteBlock(uuidV, clean),
	})

	res, err := dm.GetUnlinkedMentionsPaged("vault", "NB", "Sec", "Onboarding", "", "", 50)
	if err != nil {
		t.Fatalf("GetUnlinkedMentionsPaged: %v", err)
	}
	if len(res.Results) != 1 || len(res.Results[0].SourceSnippets) != 1 {
		t.Fatalf("expected 1 mention with snippet, got %+v", res.Results)
	}
	snip := res.Results[0].SourceSnippets[0]
	if strings.Contains(snip, "[[Onboarding]]") {
		t.Errorf("snippet still anchored on early wiki link: %q", snip)
	}
	if !strings.Contains(snip, "Onboarding") {
		t.Errorf("snippet missing residual title: %q", snip)
	}
	// Residual-centered window should include mid/suffix context, not the x-prefix.
	if strings.Contains(snip, strings.Repeat("x", 20)) {
		t.Errorf("snippet dominated by early prefix (wrong center): %q", snip)
	}
	if !strings.Contains(snip, "y") && !strings.Contains(snip, "z") {
		t.Errorf("snippet missing residual neighborhood: %q", snip)
	}
}

// TestSnippetAround_CentersOnByteSpan unit-tests position-aware excerpting.
func TestSnippetAround_CentersOnByteSpan(t *testing.T) {
	prefix := strings.Repeat("a", 80)
	mid := " TARGET "
	suffix := strings.Repeat("b", 80)
	text := prefix + mid + suffix
	start := len(prefix) + 1 // 'T' of TARGET
	end := start + len("TARGET")
	got := snippetAround(text, start, end)
	if strings.Contains(got, strings.Repeat("a", 20)) && !strings.Contains(got, "TARGET") {
		t.Fatalf("unexpected: %q", got)
	}
	if !strings.Contains(got, "TARGET") {
		t.Errorf("expected TARGET in %q", got)
	}
	// Window should pull from both sides of the known span.
	if !strings.Contains(got, "a") || !strings.Contains(got, "b") {
		t.Errorf("expected padding from both sides: %q", got)
	}
}

// TestUnlinked_DedupeBySourcePage verifies multiple matching blocks on the same
// source page collapse into one mention with MatchCount + SourceBlockIDs.
func TestUnlinked_DedupeBySourcePage(t *testing.T) {
	dm := newTestDB(t)
	idxU(t, dm, "vault", "NB", "Sec", "Onboarding", []parser.ParsedBlock{
		noteBlock(uuidU, "Onboarding"),
	})
	idxU(t, dm, "vault", "NB", "Sec", "Notes", []parser.ParsedBlock{
		noteBlock(uuidV, "first Onboarding note"),
		noteBlock(uuidW, "second Onboarding note"),
		noteBlock(uuidX, "third Onboarding note"),
	})

	res, err := dm.GetUnlinkedMentionsPaged("vault", "NB", "Sec", "Onboarding", "", "", 50)
	if err != nil {
		t.Fatalf("GetUnlinkedMentionsPaged: %v", err)
	}
	if len(res.Results) != 1 {
		t.Fatalf("expected 1 deduped mention, got %d", len(res.Results))
	}
	m := res.Results[0]
	if m.MatchCount != 3 {
		t.Errorf("MatchCount: got %d want 3", m.MatchCount)
	}
	if len(m.SourceBlockIDs) != 3 {
		t.Errorf("SourceBlockIDs: got %d want 3", len(m.SourceBlockIDs))
	}
	if len(m.SourceSnippets) != len(m.SourceBlockIDs) {
		t.Errorf("SourceSnippets length %d != SourceBlockIDs %d", len(m.SourceSnippets), len(m.SourceBlockIDs))
	}
	for i, snip := range m.SourceSnippets {
		if !strings.Contains(strings.ToLower(snip), "onboarding") {
			t.Errorf("snippet[%d] missing title: %q", i, snip)
		}
	}
}

// TestUnlinked_AmbiguousBasename verifies an ambiguous leaf title is surfaced
// with Ambiguous=true and Candidates populated (promote is rejected upstream).
func TestUnlinked_AmbiguousBasename(t *testing.T) {
	dm := newTestDB(t)
	idxU(t, dm, "vault", "NB", "Journal", "Standup", []parser.ParsedBlock{
		noteBlock(uuidU, "journal entry"),
	})
	idxU(t, dm, "vault", "NB", "Log", "Standup", []parser.ParsedBlock{
		noteBlock(uuidV, "log entry"),
	})
	idxU(t, dm, "vault", "NB", "Sec", "Notes", []parser.ParsedBlock{
		noteBlock(uuidW, "today's Standup went well"),
	})

	res, err := dm.GetUnlinkedMentionsPaged("vault", "NB", "Journal", "Standup", "", "", 50)
	if err != nil {
		t.Fatalf("GetUnlinkedMentionsPaged: %v", err)
	}
	if len(res.Results) != 1 {
		t.Fatalf("expected 1 mention even when ambiguous, got %d", len(res.Results))
	}
	if !res.Results[0].Ambiguous {
		t.Errorf("expected Ambiguous=true for shared basename Standup")
	}
	if len(res.Results[0].Candidates) < 2 {
		t.Errorf("expected >=2 candidates, got %d", len(res.Results[0].Candidates))
	}
}

// TestUnlinked_SourceAware verifies linked-notebook mentions are surfaced with
// the correct source (linked:...).
func TestUnlinked_SourceAware(t *testing.T) {
	dm := newTestDB(t)
	idxU(t, dm, "vault", "NB", "Sec", "Onboarding", []parser.ParsedBlock{
		noteBlock(uuidU, "Onboarding"),
	})
	idxU(t, dm, "linked:ext", "Ext", "Sec", "Notes", []parser.ParsedBlock{
		noteBlock(uuidV, "linked note about Onboarding"),
	})

	res, err := dm.GetUnlinkedMentionsPaged("vault", "NB", "Sec", "Onboarding", "", "", 50)
	if err != nil {
		t.Fatalf("GetUnlinkedMentionsPaged: %v", err)
	}
	if len(res.Results) != 1 {
		t.Fatalf("expected 1 linked-source mention, got %d: %+v", len(res.Results), res.Results)
	}
	if res.Results[0].Source != "linked:ext" {
		t.Errorf("expected source linked:ext, got %q", res.Results[0].Source)
	}
}

// TestUnlinked_Pagination verifies cursor pagination through a multi-source
// result set yields the full set with no duplicates.
func TestUnlinked_Pagination(t *testing.T) {
	dm := newTestDB(t)
	idxU(t, dm, "vault", "NB", "Sec", "Onboarding", []parser.ParsedBlock{
		noteBlock(uuidU, "Onboarding"),
	})
	for i := 0; i < 12; i++ {
		bid := fmt.Sprintf("uuuu%03d-uuuu-4uuu-8uuu-uuuuuuuuuuuu", i)
		idxU(t, dm, "vault", "NB", "Sec", fmt.Sprintf("Page%02d", i), []parser.ParsedBlock{
			noteBlock(bid, "mentions Onboarding"),
		})
	}

	seen := make(map[string]bool)
	cursor := ""
	pages := 0
	for {
		res, err := dm.GetUnlinkedMentionsPaged("vault", "NB", "Sec", "Onboarding", cursor, "", 5)
		if err != nil {
			t.Fatalf("GetUnlinkedMentionsPaged cursor=%q: %v", cursor, err)
		}
		pages++
		for _, m := range res.Results {
			key := m.Source + "\x00" + m.SourceNotebook + "\x00" + m.SourceSection + "\x00" + m.SourcePage
			if seen[key] {
				t.Errorf("duplicate across pages: %+v", m)
			}
			seen[key] = true
		}
		if !res.HasMore {
			if res.Cursor != "" {
				t.Error("cursor should be empty when HasMore=false")
			}
			break
		}
		cursor = res.Cursor
	}
	if len(seen) != 12 {
		t.Errorf("expected 12 unique source pages across pages, got %d", len(seen))
	}
	if pages < 3 {
		t.Errorf("expected >=3 pages, got %d", pages)
	}
}

// TestUnlinked_LimitCapping verifies limit clamping (0→default, >max→max).
func TestUnlinked_LimitCapping(t *testing.T) {
	dm := newTestDB(t)
	idxU(t, dm, "vault", "NB", "Sec", "Onboarding", []parser.ParsedBlock{
		noteBlock(uuidU, "Onboarding"),
	})
	for i := 0; i < 3; i++ {
		bid := fmt.Sprintf("vvvv%03d-vvvv-4vvv-8vvv-vvvvvvvvvvvv", i)
		idxU(t, dm, "vault", "NB", "Sec", fmt.Sprintf("P%d", i), []parser.ParsedBlock{
			noteBlock(bid, "Onboarding"),
		})
	}
	r0, _ := dm.GetUnlinkedMentionsPaged("vault", "NB", "Sec", "Onboarding", "", "", 0)
	if len(r0.Results) != 3 {
		t.Errorf("limit 0 (default 50): expected all 3, got %d", len(r0.Results))
	}
	rBig, _ := dm.GetUnlinkedMentionsPaged("vault", "NB", "Sec", "Onboarding", "", "", 99999)
	if len(rBig.Results) != 3 {
		t.Errorf("limit 99999 (clamped): expected all 3, got %d", len(rBig.Results))
	}
}

// TestUnlinked_ShortTitleSkipped verifies titles shorter than 2 runes return
// empty (too many false positives to be actionable).
func TestUnlinked_ShortTitleSkipped(t *testing.T) {
	dm := newTestDB(t)
	idxU(t, dm, "vault", "NB", "Sec", "A", []parser.ParsedBlock{
		noteBlock(uuidU, "A"),
	})
	idxU(t, dm, "vault", "NB", "Sec", "Notes", []parser.ParsedBlock{
		noteBlock(uuidV, "a single A letter"),
	})
	res, err := dm.GetUnlinkedMentionsPaged("vault", "NB", "Sec", "A", "", "", 50)
	if err != nil {
		t.Fatalf("GetUnlinkedMentionsPaged: %v", err)
	}
	if len(res.Results) != 0 {
		t.Errorf("1-rune title should be skipped, got %d", len(res.Results))
	}
	if res.Truncated {
		t.Error("short-title early exit must not report Truncated")
	}
}

// idxUMany indexes n source pages each with one plain mention of title.
// Src%04d names are stable labels for set-membership assertions across batches.
// The title page body deliberately omits the title token so it does not consume
// an FTS batch slot (self-page is filtered in Go after the FTS LIMIT).
func idxUMany(t *testing.T, dm *DatabaseManager, title string, n int) {
	t.Helper()
	idxU(t, dm, "vault", "NB", "Sec", title, []parser.ParsedBlock{
		noteBlock(uuidU, "home page"),
	})
	for i := 0; i < n; i++ {
		pg := fmt.Sprintf("Src%04d", i)
		bid := fmt.Sprintf("%08x-aaaa-4aaa-8aaa-aaaaaaaaaaaa", i)
		idxU(t, dm, "vault", "NB", "Sec", pg, []parser.ParsedBlock{
			noteBlock(bid, "mentions "+title+" here"),
		})
	}
}

// collectUnlinkedPages walks residual cursor pages for one FTS batch (scanCursor).
func collectUnlinkedPages(t *testing.T, dm *DatabaseManager, scanCursor string) (pages []string, truncated bool, nextScan string) {
	t.Helper()
	cursor := ""
	for {
		page, err := dm.GetUnlinkedMentionsPaged("vault", "NB", "Sec", "Topic", cursor, scanCursor, 100)
		if err != nil {
			t.Fatalf("collect scan=%q cursor=%q: %v", scanCursor, cursor, err)
		}
		truncated = page.Truncated
		nextScan = page.ScanCursor
		for _, m := range page.Results {
			pages = append(pages, m.SourcePage)
		}
		if !page.HasMore {
			break
		}
		cursor = page.Cursor
	}
	return pages, truncated, nextScan
}

// TestUnlinked_ScanCapUnderExactOver verifies Truncated is false at and below
// unlinkedScanCap and true only when FTS candidates exceed the cap (limit+1 probe).
func TestUnlinked_ScanCapUnderExactOver(t *testing.T) {
	// Under cap.
	dmUnder := newTestDB(t)
	idxUMany(t, dmUnder, "Topic", 3)
	under, err := dmUnder.GetUnlinkedMentionsPaged("vault", "NB", "Sec", "Topic", "", "", 50)
	if err != nil {
		t.Fatalf("under: %v", err)
	}
	if under.Truncated {
		t.Error("under cap: Truncated should be false")
	}
	if under.ScanCursor != "" {
		t.Error("under cap: ScanCursor should be empty")
	}
	if len(under.Results) != 3 {
		t.Fatalf("under cap: expected 3 residual pages, got %d", len(under.Results))
	}

	// Exact cap: unlinkedScanCap FTS candidates → not truncated.
	dmExact := newTestDB(t)
	idxUMany(t, dmExact, "Topic", unlinkedScanCap)
	exactPages, exactTrunc, exactScan := collectUnlinkedPages(t, dmExact, "")
	if exactTrunc {
		t.Error("exact cap: Truncated should be false")
	}
	if exactScan != "" {
		t.Error("exact cap: ScanCursor should be empty")
	}
	if len(exactPages) != unlinkedScanCap {
		t.Fatalf("exact cap: expected %d residual pages, got %d", unlinkedScanCap, len(exactPages))
	}

	// Over cap: one extra FTS candidate → Truncated + ScanCursor; first batch has cap residuals.
	dmOver := newTestDB(t)
	idxUMany(t, dmOver, "Topic", unlinkedScanCap+1)
	over, err := dmOver.GetUnlinkedMentionsPaged("vault", "NB", "Sec", "Topic", "", "", 50)
	if err != nil {
		t.Fatalf("over: %v", err)
	}
	if !over.Truncated {
		t.Error("over cap: Truncated should be true")
	}
	if over.ScanCursor == "" {
		t.Error("over cap: ScanCursor required when Truncated")
	}
	// Page limit 50: first page has 50 residual pages; pool still truncated.
	if len(over.Results) != 50 {
		t.Fatalf("over cap page: expected 50 results, got %d", len(over.Results))
	}
	if !over.HasMore {
		t.Error("over cap: HasMore should be true (residual pages > limit)")
	}
	// Walk all residual pages from the first batch — exactly cap pages, path-sorted.
	pages, trunc, scanOut := collectUnlinkedPages(t, dmOver, "")
	if !trunc {
		t.Error("over walk: Truncated must stay true on every page")
	}
	if scanOut == "" {
		t.Error("over walk: ScanCursor must stay set while truncated")
	}
	if len(pages) != unlinkedScanCap {
		t.Fatalf("over walk: expected %d residual pages from capped batch, got %d", unlinkedScanCap, len(pages))
	}
	// Residual presentation is path-sorted within the batch.
	for i := 1; i < len(pages); i++ {
		if pages[i] < pages[i-1] {
			t.Fatalf("over walk: residual pages not path-sorted at %d: %q < %q", i, pages[i], pages[i-1])
		}
	}
	// First batch is a proper subset of all Src pages; the missing one is reachable via scan continuation.
	allWant := map[string]bool{}
	for i := 0; i < unlinkedScanCap+1; i++ {
		allWant[fmt.Sprintf("Src%04d", i)] = true
	}
	batchSet := map[string]bool{}
	for _, p := range pages {
		if !allWant[p] {
			t.Fatalf("over walk: unexpected page %q", p)
		}
		batchSet[p] = true
	}
	if len(batchSet) != unlinkedScanCap {
		t.Fatalf("over walk: expected %d unique pages, got %d", unlinkedScanCap, len(batchSet))
	}
	var missing []string
	for p := range allWant {
		if !batchSet[p] {
			missing = append(missing, p)
		}
	}
	if len(missing) != 1 {
		t.Fatalf("over walk: expected exactly 1 page beyond first batch, got %v", missing)
	}
}

// TestUnlinked_ScanCapTruncatedZeroResidual: FTS pool hits the cap but every
// candidate is fully linked (no residual plain) — Truncated stays true with an
// empty residual page list so the UI can still warn.
func TestUnlinked_ScanCapTruncatedZeroResidual(t *testing.T) {
	dm := newTestDB(t)
	idxU(t, dm, "vault", "NB", "Sec", "Topic", []parser.ParsedBlock{
		noteBlock(uuidU, "Topic home"),
	})
	for i := 0; i < unlinkedScanCap+1; i++ {
		pg := fmt.Sprintf("Link%04d", i)
		bid := fmt.Sprintf("%08x-bbbb-4bbb-8bbb-bbbbbbbbbbbb", i)
		idxU(t, dm, "vault", "NB", "Sec", pg, []parser.ParsedBlock{
			noteBlock(bid, "see [[Topic]] only"),
		})
	}
	res, err := dm.GetUnlinkedMentionsPaged("vault", "NB", "Sec", "Topic", "", "", 50)
	if err != nil {
		t.Fatalf("GetUnlinkedMentionsPaged: %v", err)
	}
	if !res.Truncated {
		t.Error("expected Truncated=true when FTS pool exceeds cap")
	}
	if res.ScanCursor == "" {
		t.Error("expected ScanCursor when Truncated")
	}
	if len(res.Results) != 0 {
		t.Fatalf("expected 0 residual pages (fully linked only), got %d", len(res.Results))
	}
	if res.HasMore {
		t.Error("empty residual set must not report HasMore")
	}
}

// TestUnlinked_DbClosed returns ErrDBClosed.
func TestUnlinked_DbClosed(t *testing.T) {
	dm := newTestDB(t)
	_ = dm.Close()
	_, err := dm.GetUnlinkedMentionsPaged("vault", "NB", "Sec", "Onboarding", "", "", 50)
	if err != ErrDBClosed {
		t.Errorf("expected ErrDBClosed, got %v", err)
	}
}

// TestUnlinked_EncodeDecodeCursor verifies the cursor round-trips.
func TestUnlinked_EncodeDecodeCursor(t *testing.T) {
	m := UnlinkedMention{Source: "vault", SourceNotebook: "NB", SourceSection: "Sec", SourcePage: "P"}
	enc := encodeUnlinkedCursor(m)
	if enc == "" {
		t.Fatal("empty cursor")
	}
	dec, ok := decodeUnlinkedCursor(enc)
	if !ok {
		t.Fatal("decode failed")
	}
	if dec != unlinkedCursorKey(m) {
		t.Errorf("round-trip mismatch: got %q want %q", dec, unlinkedCursorKey(m))
	}
	if _, ok := decodeUnlinkedCursor("!!!not-base64!!!"); ok {
		t.Error("garbage cursor should decode as false")
	}
	if _, ok := decodeUnlinkedCursor(""); ok {
		t.Error("empty cursor should decode as false")
	}
}

// --- Promote rewrite tests ---

// TestPromote_RewriteMigratesToBacklinks verifies the full promote flow at the
// DB layer is observable: after promotion (simulated by re-indexing a wrapped
// block), the mention migrates from unlinked into backlinks.
func TestPromote_RewriteMigratesToBacklinks(t *testing.T) {
	dm := newTestDB(t)
	idxU(t, dm, "vault", "NB", "Sec", "Onboarding", []parser.ParsedBlock{
		noteBlock(uuidU, "Onboarding"),
	})
	idxU(t, dm, "vault", "NB", "Sec", "Notes", []parser.ParsedBlock{
		noteBlock(uuidV, "review Onboarding before launch"),
	})

	// Before: 1 unlinked mention, 0 backlinks.
	res, _ := dm.GetUnlinkedMentionsPaged("vault", "NB", "Sec", "Onboarding", "", "", 50)
	if len(res.Results) != 1 {
		t.Fatalf("baseline unlinked: expected 1, got %d", len(res.Results))
	}
	bl, _ := dm.GetBacklinks("vault", "NB", "Sec", "Onboarding")
	if len(bl) != 0 {
		t.Fatalf("baseline backlinks: expected 0, got %d", len(bl))
	}

	// Simulate promote: re-index the source block with the wrapped text.
	idxU(t, dm, "vault", "NB", "Sec", "Notes", []parser.ParsedBlock{
		noteBlock(uuidV, "review [[Onboarding]] before launch"),
	})

	// After: unlinked drops to 0, backlinks gains the page-link.
	res, _ = dm.GetUnlinkedMentionsPaged("vault", "NB", "Sec", "Onboarding", "", "", 50)
	if len(res.Results) != 0 {
		t.Fatalf("post-promote unlinked: expected 0, got %d: %+v", len(res.Results), res.Results)
	}
	bl, _ = dm.GetBacklinks("vault", "NB", "Sec", "Onboarding")
	if len(bl) != 1 || bl[0].Kind != BacklinkPageLink {
		t.Fatalf("post-promote backlinks: expected 1 page-link, got %+v", bl)
	}
}

// TestPromote_AmbiguousRejected verifies the DB-layer ambiguity flag is set so
// the IPC layer (tested in app_unlinked_test.go) rejects promotion.
func TestPromote_AmbiguousRejected(t *testing.T) {
	dm := newTestDB(t)
	idxU(t, dm, "vault", "NB", "Journal", "Standup", []parser.ParsedBlock{
		noteBlock(uuidU, "journal entry"),
	})
	idxU(t, dm, "vault", "NB", "Log", "Standup", []parser.ParsedBlock{
		noteBlock(uuidV, "log entry"),
	})
	idxU(t, dm, "vault", "NB", "Sec", "Notes", []parser.ParsedBlock{
		noteBlock(uuidW, "Standup mention"),
	})

	res, _ := dm.GetUnlinkedMentionsPaged("vault", "NB", "Journal", "Standup", "", "", 50)
	if len(res.Results) != 1 || !res.Results[0].Ambiguous {
		t.Fatalf("expected 1 ambiguous mention, got %+v", res.Results)
	}
}

// TestUnlinked_ScanFillSkipsCodeToReachPlain: CODE hits under-fill a single FTS
// probe; loop-fill continues within unlinkedScanFillRounds so plain residuals
// appear on the first open without requiring Scan more.
func TestUnlinked_ScanFillSkipsCodeToReachPlain(t *testing.T) {
	dm := newTestDB(t)
	idxU(t, dm, "vault", "NB", "Sec", "Topic", []parser.ParsedBlock{
		noteBlock(uuidU, "home page"),
	})
	// A full FTS probe of CODE-only hits would yield zero keepers without fill.
	for i := 0; i < unlinkedScanCap; i++ {
		pg := fmt.Sprintf("Code%04d", i)
		bid := fmt.Sprintf("%08x-cccc-4ccc-8ccc-cccccccccccc", i)
		idxU(t, dm, "vault", "NB", "Sec", pg, []parser.ParsedBlock{
			{ID: bid, Type: parser.BlockCode, RawText: "const Topic = 1", CleanText: "const Topic = 1", LineNumber: 1},
		})
	}
	const plainN = 3
	for i := 0; i < plainN; i++ {
		pg := fmt.Sprintf("Plain%04d", i)
		bid := fmt.Sprintf("%08x-dddd-4ddd-8ddd-dddddddddddd", i)
		idxU(t, dm, "vault", "NB", "Sec", pg, []parser.ParsedBlock{
			noteBlock(bid, "mentions Topic here"),
		})
	}

	res, err := dm.GetUnlinkedMentionsPaged("vault", "NB", "Sec", "Topic", "", "", 50)
	if err != nil {
		t.Fatalf("GetUnlinkedMentionsPaged: %v", err)
	}
	if res.Truncated {
		t.Error("FTS exhausted after fill — Truncated should be false")
	}
	if res.ScanCursor != "" {
		t.Errorf("ScanCursor should be empty when not truncated, got %q", res.ScanCursor)
	}
	if len(res.Results) != plainN {
		t.Fatalf("loop-fill should surface %d plain residuals on first open, got %d: %+v", plainN, len(res.Results), res.Results)
	}
	for i, m := range res.Results {
		want := fmt.Sprintf("Plain%04d", i)
		if m.SourcePage != want {
			t.Errorf("result[%d]: got %q want %q", i, m.SourcePage, want)
		}
	}
}

// TestUnlinked_ScanCursorMultiBatch verifies capped FTS continuation surfaces
// residuals beyond the first unlinkedScanCap window without unbounded default scans.
func TestUnlinked_ScanCursorMultiBatch(t *testing.T) {
	dm := newTestDB(t)
	const extra = 3
	idxUMany(t, dm, "Topic", unlinkedScanCap+extra)

	batch1, trunc1, scan1 := collectUnlinkedPages(t, dm, "")
	if !trunc1 || scan1 == "" {
		t.Fatalf("batch1: want truncated with scan_cursor, got trunc=%v scan=%q", trunc1, scan1)
	}
	if len(batch1) != unlinkedScanCap {
		t.Fatalf("batch1: expected %d residuals, got %d", unlinkedScanCap, len(batch1))
	}

	batch2, trunc2, scan2 := collectUnlinkedPages(t, dm, scan1)
	if trunc2 {
		t.Error("batch2: should exhaust remaining candidates (not truncated)")
	}
	if scan2 != "" {
		t.Errorf("batch2: ScanCursor should be empty when not truncated, got %q", scan2)
	}
	if len(batch2) != extra {
		t.Fatalf("batch2: expected %d residuals, got %d", extra, len(batch2))
	}

	// Batches are disjoint and cover the full residual set.
	seen := map[string]bool{}
	for _, p := range batch1 {
		if seen[p] {
			t.Fatalf("duplicate in batch1: %s", p)
		}
		seen[p] = true
	}
	for _, p := range batch2 {
		if seen[p] {
			t.Fatalf("batch2 overlaps batch1: %s", p)
		}
		seen[p] = true
	}
	if len(seen) != unlinkedScanCap+extra {
		t.Fatalf("union size: got %d want %d", len(seen), unlinkedScanCap+extra)
	}
	for i := 0; i < unlinkedScanCap+extra; i++ {
		want := fmt.Sprintf("Src%04d", i)
		if !seen[want] {
			t.Errorf("missing residual %s across batches", want)
		}
	}
}

// TestUnlinked_ScanCursorInvalidSoftResets treats garbage scan_cursor as first batch.
func TestUnlinked_ScanCursorInvalidSoftResets(t *testing.T) {
	dm := newTestDB(t)
	idxUMany(t, dm, "Topic", 3)
	good, err := dm.GetUnlinkedMentionsPaged("vault", "NB", "Sec", "Topic", "", "", 50)
	if err != nil {
		t.Fatalf("good: %v", err)
	}
	bad, err := dm.GetUnlinkedMentionsPaged("vault", "NB", "Sec", "Topic", "", "!!!not-a-cursor!!!", 50)
	if err != nil {
		t.Fatalf("bad scan cursor: %v", err)
	}
	if len(bad.Results) != len(good.Results) {
		t.Fatalf("invalid scan_cursor should soft-reset to first batch: got %d want %d", len(bad.Results), len(good.Results))
	}
}

// TestUnlinked_ScanCursorResidualFilterAcrossBatches: first batch is fully linked
// (truncated, zero residual); second batch has plain residuals.
func TestUnlinked_ScanCursorResidualFilterAcrossBatches(t *testing.T) {
	dm := newTestDB(t)
	idxU(t, dm, "vault", "NB", "Sec", "Topic", []parser.ParsedBlock{
		noteBlock(uuidU, "Topic home"),
	})
	// First unlinkedScanCap FTS hits: linked only.
	for i := 0; i < unlinkedScanCap; i++ {
		pg := fmt.Sprintf("Link%04d", i)
		bid := fmt.Sprintf("%08x-bbbb-4bbb-8bbb-bbbbbbbbbbbb", i)
		idxU(t, dm, "vault", "NB", "Sec", pg, []parser.ParsedBlock{
			noteBlock(bid, "see [[Topic]] only"),
		})
	}
	// One more plain residual beyond the first batch.
	idxU(t, dm, "vault", "NB", "Sec", "PlainBeyond", []parser.ParsedBlock{
		noteBlock(uuidV, "plain Topic mention"),
	})

	first, err := dm.GetUnlinkedMentionsPaged("vault", "NB", "Sec", "Topic", "", "", 50)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	if !first.Truncated || first.ScanCursor == "" {
		t.Fatalf("first: want truncated+scan_cursor, got %+v", first)
	}
	if len(first.Results) != 0 {
		t.Fatalf("first: expected 0 residual (linked-only batch), got %d", len(first.Results))
	}

	second, err := dm.GetUnlinkedMentionsPaged("vault", "NB", "Sec", "Topic", "", first.ScanCursor, 50)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if second.Truncated {
		t.Error("second: should not be truncated")
	}
	if len(second.Results) != 1 || second.Results[0].SourcePage != "PlainBeyond" {
		t.Fatalf("second: expected PlainBeyond residual, got %+v", second.Results)
	}
}

// TestUnlinked_HasMoreOrthogonalToTruncated: residual paging and FTS truncation
// are independent flags.
func TestUnlinked_HasMoreOrthogonalToTruncated(t *testing.T) {
	dm := newTestDB(t)
	idxUMany(t, dm, "Topic", unlinkedScanCap+1)

	page, err := dm.GetUnlinkedMentionsPaged("vault", "NB", "Sec", "Topic", "", "", 10)
	if err != nil {
		t.Fatalf("page: %v", err)
	}
	if !page.Truncated || !page.HasMore {
		t.Fatalf("want truncated && has_more on first small page, got trunc=%v more=%v", page.Truncated, page.HasMore)
	}
	if page.ScanCursor == "" || page.Cursor == "" {
		t.Fatal("want both residual cursor and scan_cursor")
	}

	// Exhaust residual pages within the first batch; truncated stays true.
	cursor := page.Cursor
	for {
		next, err := dm.GetUnlinkedMentionsPaged("vault", "NB", "Sec", "Topic", cursor, "", 100)
		if err != nil {
			t.Fatalf("more: %v", err)
		}
		if !next.Truncated {
			t.Error("truncated must remain true while FTS window is capped")
		}
		if next.ScanCursor == "" {
			t.Error("scan_cursor must remain set while truncated")
		}
		if !next.HasMore {
			break
		}
		cursor = next.Cursor
	}
}

// TestUnlinked_EncodeDecodeScanCursor verifies u3 rowid keyset round-trip,
// legacy u2 soft-reset, and legacy u1 acceptance.
func TestUnlinked_EncodeDecodeScanCursor(t *testing.T) {
	dm := newTestDB(t)
	idxU(t, dm, "vault", "NB", "Sec", "Topic", []parser.ParsedBlock{
		noteBlock(uuidU, "home"),
	})
	idxU(t, dm, "vault", "NB", "Sec", "Src", []parser.ParsedBlock{
		noteBlock(uuidV, "mentions Topic here"),
	})
	db := dm.SQLDB()
	var wantRowid int64
	if err := db.QueryRow(`SELECT rowid FROM blocks WHERE id = ?`, uuidV).Scan(&wantRowid); err != nil {
		t.Fatalf("lookup: %v", err)
	}

	enc := encodeUnlinkedScanCursor(wantRowid, uuidV)
	if enc == "" {
		t.Fatal("empty scan cursor for positive rowid")
	}

	got := resolveUnlinkedScanCursor(enc)
	if got != wantRowid {
		t.Errorf("resolve u3: got rowid %d want %d", got, wantRowid)
	}

	// u3 without id still works.
	encRowOnly := encodeUnlinkedScanCursor(wantRowid, "")
	if got := resolveUnlinkedScanCursor(encRowOnly); got != wantRowid {
		t.Errorf("u3 row-only: got %d want %d", got, wantRowid)
	}

	// Legacy u2:uuid soft-resets (live UUID→rowid was a skip hazard).
	legacyU2 := base64.RawURLEncoding.EncodeToString([]byte(scanCursorPrefixV2 + uuidV))
	if got := resolveUnlinkedScanCursor(legacyU2); got != 0 {
		t.Errorf("legacy u2 should soft-reset to 0, got %d", got)
	}

	// Legacy u1:rowid still accepted.
	legacy := base64.RawURLEncoding.EncodeToString([]byte(scanCursorPrefixV1 + "42"))
	if got := resolveUnlinkedScanCursor(legacy); got != 42 {
		t.Errorf("legacy u1: got %d want 42", got)
	}

	if got := resolveUnlinkedScanCursor(""); got != 0 {
		t.Errorf("empty → 0, got %d", got)
	}
	if got := resolveUnlinkedScanCursor("!!!"); got != 0 {
		t.Errorf("garbage → 0, got %d", got)
	}
	if encodeUnlinkedScanCursor(0, uuidV) != "" {
		t.Error("non-positive rowid should not encode")
	}
}

// TestUnlinked_ScanFillRoundsExhaustion: CODE-only hits fill every probe for
// unlinkedScanFillRounds without reaching unlinkedScanCap keepers; the round
// budget binds with Truncated+ScanCursor so Scan more can reach later plains.
func TestUnlinked_ScanFillRoundsExhaustion(t *testing.T) {
	dm := newTestDB(t)
	idxU(t, dm, "vault", "NB", "Sec", "Topic", []parser.ParsedBlock{
		noteBlock(uuidU, "home page"),
	})
	// Exactly rounds×cap CODE hits: each fill round consumes one full probe of
	// CODE-only keepers-filtered rows and still sees probeMore until the last
	// probe of the budget... Actually we need probeMore true when rounds end:
	// rounds*cap CODE + 1 more CODE (or plain after) so the final round still
	// has more FTS beyond the window.
	codeN := unlinkedScanFillRounds * unlinkedScanCap
	for i := 0; i < codeN; i++ {
		pg := fmt.Sprintf("Code%05d", i)
		bid := fmt.Sprintf("%08x-eeee-4eee-8eee-eeeeeeeeeeee", i)
		idxU(t, dm, "vault", "NB", "Sec", pg, []parser.ParsedBlock{
			{ID: bid, Type: parser.BlockCode, RawText: "const Topic = 1", CleanText: "const Topic = 1", LineNumber: 1},
		})
	}
	// One extra CODE so the last probe of the budget still has moreFTS, then plains.
	idxU(t, dm, "vault", "NB", "Sec", "CodeExtra", []parser.ParsedBlock{
		{ID: "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee", Type: parser.BlockCode, RawText: "var Topic", CleanText: "var Topic", LineNumber: 1},
	})
	const plainN = 2
	for i := 0; i < plainN; i++ {
		pg := fmt.Sprintf("Plain%04d", i)
		bid := fmt.Sprintf("%08x-ffff-4fff-8fff-ffffffffffff", i)
		idxU(t, dm, "vault", "NB", "Sec", pg, []parser.ParsedBlock{
			noteBlock(bid, "mentions Topic here"),
		})
	}

	first, err := dm.GetUnlinkedMentionsPaged("vault", "NB", "Sec", "Topic", "", "", 50)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	if !first.Truncated {
		t.Fatal("round budget should bind with Truncated=true")
	}
	if first.ScanCursor == "" {
		t.Fatal("round budget bind must yield ScanCursor for continuation")
	}
	if len(first.Results) != 0 {
		t.Fatalf("CODE-only fill window should yield 0 residuals, got %d", len(first.Results))
	}

	// Consume ScanCursor until plains appear (may take multiple Scan more rounds
	// if remaining CODE still under-fills, but must terminate with plains).
	scan := first.ScanCursor
	var plains []string
	for step := 0; step < 8; step++ {
		page, err := dm.GetUnlinkedMentionsPaged("vault", "NB", "Sec", "Topic", "", scan, 50)
		if err != nil {
			t.Fatalf("step %d: %v", step, err)
		}
		for _, m := range page.Results {
			plains = append(plains, m.SourcePage)
		}
		if !page.Truncated {
			break
		}
		if page.ScanCursor == "" {
			t.Fatalf("step %d: truncated without scan_cursor", step)
		}
		if page.ScanCursor == scan {
			t.Fatalf("step %d: scan_cursor did not advance", step)
		}
		scan = page.ScanCursor
	}
	if len(plains) != plainN {
		t.Fatalf("after round-exhaustion continuation: want %d plains, got %v", plainN, plains)
	}
	for i, got := range plains {
		want := fmt.Sprintf("Plain%04d", i)
		if got != want {
			t.Errorf("plain[%d]: got %q want %q", i, got, want)
		}
	}
}

// TestUnlinked_ScanCursorSurvivesReindex: immutable rowid cursor still continues
// after an early non-anchor page is re-indexed.
func TestUnlinked_ScanCursorSurvivesReindex(t *testing.T) {
	dm := newTestDB(t)
	idxUMany(t, dm, "Topic", unlinkedScanCap+3)

	first, err := dm.GetUnlinkedMentionsPaged("vault", "NB", "Sec", "Topic", "", "", 50)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	if !first.Truncated || first.ScanCursor == "" {
		t.Fatalf("want truncated batch, got trunc=%v scan=%q", first.Truncated, first.ScanCursor)
	}

	// Re-index one early source page (new content, same path) so SQLite may
	// assign a new rowid while other pages stay put. Cursor stores the scan-time
	// exclusive rowid bound — must still surface remaining residuals.
	idxU(t, dm, "vault", "NB", "Sec", "Src0000", []parser.ParsedBlock{
		noteBlock("00000000-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "mentions Topic again"),
	})

	second, err := dm.GetUnlinkedMentionsPaged("vault", "NB", "Sec", "Topic", "", first.ScanCursor, 100)
	if err != nil {
		t.Fatalf("second after reindex: %v", err)
	}
	if len(second.Results) == 0 {
		t.Fatal("continuation after reindex should still surface remaining residuals")
	}
	seen := map[string]bool{}
	for _, m := range first.Results {
		seen[m.SourcePage] = true
	}
	newCount := 0
	for _, m := range second.Results {
		if !seen[m.SourcePage] {
			newCount++
		}
	}
	if newCount == 0 {
		t.Error("second batch after reindex should include pages not in first residual page")
	}
}

// TestUnlinked_ScanCursorAnchorReindexDoesNotSkip: re-indexing the last-examined
// anchor block to a higher rowid must not skip unread matches that still sit
// between the old bound and the anchor's new rowid (u2 live-UUID hazard).
func TestUnlinked_ScanCursorAnchorReindexDoesNotSkip(t *testing.T) {
	dm := newTestDB(t)
	// Cap+3 plain sources so batch 1 truncates with a known tail.
	idxUMany(t, dm, "Topic", unlinkedScanCap+3)

	first, err := dm.GetUnlinkedMentionsPaged("vault", "NB", "Sec", "Topic", "", "", unlinkedScanCap)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	if !first.Truncated || first.ScanCursor == "" {
		t.Fatalf("want truncated, got trunc=%v scan=%q", first.Truncated, first.ScanCursor)
	}

	// Decode the immutable bound, then re-index the anchor block id (if present
	// in the cursor) so it would move to a higher rowid under the old u2 scheme.
	raw, err := base64.RawURLEncoding.DecodeString(first.ScanCursor)
	if err != nil {
		t.Fatalf("decode cursor: %v", err)
	}
	s := string(raw)
	if !strings.HasPrefix(s, scanCursorPrefixV3) {
		t.Fatalf("want u3 cursor, got %q", s)
	}
	rest := s[len(scanCursorPrefixV3):]
	boundStr, anchorID, _ := strings.Cut(rest, ":")
	bound, err := strconv.ParseInt(boundStr, 10, 64)
	if err != nil || bound <= 0 {
		t.Fatalf("bad bound in cursor %q", s)
	}

	// Collect pages that should appear after the bound (second batch before reindex).
	before, err := dm.GetUnlinkedMentionsPaged("vault", "NB", "Sec", "Topic", "", first.ScanCursor, 100)
	if err != nil {
		t.Fatalf("before reindex continue: %v", err)
	}
	wantPages := map[string]bool{}
	for _, m := range before.Results {
		wantPages[m.SourcePage] = true
	}
	if len(wantPages) == 0 {
		t.Fatal("expected residual pages beyond first batch")
	}

	// Re-index the anchor block (or any block at the bound rowid) so its live
	// rowid moves past unread matches — u3 must still use the stored bound.
	if anchorID != "" {
		var pg string
		db := dm.SQLDB()
		_ = db.QueryRow(`SELECT page FROM blocks WHERE id = ?`, anchorID).Scan(&pg)
		if pg != "" && pg != "Topic" {
			idxU(t, dm, "vault", "NB", "Sec", pg, []parser.ParsedBlock{
				noteBlock(anchorID, "mentions Topic after reindex"),
			})
		}
	} else {
		// Fallback: re-index last residual page from batch 1.
		last := first.Results[len(first.Results)-1]
		idxU(t, dm, "vault", last.SourceNotebook, last.SourceSection, last.SourcePage, []parser.ParsedBlock{
			noteBlock("00000000-bbbb-4bbb-8bbb-bbbbbbbbbbbb", "mentions Topic after reindex"),
		})
	}

	after, err := dm.GetUnlinkedMentionsPaged("vault", "NB", "Sec", "Topic", "", first.ScanCursor, 100)
	if err != nil {
		t.Fatalf("after anchor reindex: %v", err)
	}
	gotPages := map[string]bool{}
	for _, m := range after.Results {
		gotPages[m.SourcePage] = true
	}
	for pg := range wantPages {
		if !gotPages[pg] {
			t.Errorf("after anchor reindex, missing residual page %q (would skip under live UUID cursor)", pg)
		}
	}
}

// TestUnlinked_PaddedPageSelfFilter: trimmed title and self-exclusion use the
// same normalized page so padded IPC values do not list the active page.
func TestUnlinked_PaddedPageSelfFilter(t *testing.T) {
	dm := newTestDB(t)
	idxU(t, dm, "vault", "NB", "Sec", "Topic", []parser.ParsedBlock{
		noteBlock(uuidU, "self mentions Topic on own page"),
	})
	idxU(t, dm, "vault", "NB", "Sec", "Other", []parser.ParsedBlock{
		noteBlock(uuidV, "other mentions Topic"),
	})

	res, err := dm.GetUnlinkedMentionsPaged("vault", "NB", "Sec", "  Topic  ", "", "", 50)
	if err != nil {
		t.Fatalf("padded page: %v", err)
	}
	for _, m := range res.Results {
		if m.SourcePage == "Topic" {
			t.Fatalf("active page must not appear as unlinked mention, got %+v", res.Results)
		}
	}
	if len(res.Results) != 1 || res.Results[0].SourcePage != "Other" {
		t.Fatalf("want only Other, got %+v", res.Results)
	}
}

// unlinkedScanSQL is the production candidate batch shape (kept in sync with
// scanUnlinkedCandidateBlocks) for EXPLAIN regression.
const unlinkedScanSQL = `SELECT b.rowid, b.id, b.source, b.notebook, b.section, b.page, b.type, COALESCE(b.clean_content,'')
		FROM (
			SELECT rowid AS rid FROM blocks_fts
			WHERE blocks_fts MATCH ?
			  AND rowid > ?
			ORDER BY rowid
			LIMIT ?
		) AS f
		JOIN blocks b ON b.rowid = f.rid
		ORDER BY f.rid`

const unlinkedPathOrderSQL = `SELECT b.rowid, b.id, b.source, b.notebook, b.section, b.page, COALESCE(b.clean_content,'')
		FROM blocks_fts
		JOIN blocks b ON b.rowid = blocks_fts.rowid
		WHERE blocks_fts MATCH ?
		  AND b.type <> 'CODE'
		  AND NOT (b.source = ? AND b.notebook = ? AND b.section = ? AND b.page = ?)
		ORDER BY b.source, b.notebook, b.section, b.page
		LIMIT ?`

func explainDetails(t *testing.T, dm *DatabaseManager, sql string, args ...any) []string {
	t.Helper()
	rows, err := dm.SQLDB().Query("EXPLAIN QUERY PLAN "+sql, args...)
	if err != nil {
		t.Fatalf("EXPLAIN: %v", err)
	}
	defer rows.Close()
	var plans []string
	for rows.Next() {
		var selectid, order, from, detail string
		if err := rows.Scan(&selectid, &order, &from, &detail); err != nil {
			t.Fatalf("scan EXPLAIN: %v", err)
		}
		plans = append(plans, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("EXPLAIN rows: %v", err)
	}
	return plans
}

// TestUnlinked_ResidualLoadMoreReusesScanCache verifies residual paging with the
// same scanCursor does not re-run FTS loop-fill (#838).
func TestUnlinked_ResidualLoadMoreReusesScanCache(t *testing.T) {
	dm := newTestDB(t)
	// Active page + enough residual source pages to force residual has_more.
	idxU(t, dm, "vault", "NB", "Sec", "Topic", []parser.ParsedBlock{
		noteBlock(uuidU, "Topic home"),
	})
	const n = 12
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("cccccccc-cccc-4ccc-8ccc-%012d", i)
		idxU(t, dm, "vault", "NB", "Sec", fmt.Sprintf("Src%02d", i), []parser.ParsedBlock{
			noteBlock(id, "mentions Topic here"),
		})
	}

	unlinkedScanCalls.Store(0)
	page1, err := dm.GetUnlinkedMentionsPaged("vault", "NB", "Sec", "Topic", "", "", 5)
	if err != nil {
		t.Fatalf("page1: %v", err)
	}
	if !page1.HasMore || page1.Cursor == "" {
		t.Fatalf("expected residual has_more, got %+v", page1)
	}
	if unlinkedScanCalls.Load() != 1 {
		t.Fatalf("page1 scans: got %d want 1", unlinkedScanCalls.Load())
	}

	page2, err := dm.GetUnlinkedMentionsPaged("vault", "NB", "Sec", "Topic", page1.Cursor, "", 5)
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	if unlinkedScanCalls.Load() != 1 {
		t.Fatalf("page2 must reuse cache: scans=%d want 1", unlinkedScanCalls.Load())
	}

	// Full residual list in one shot must equal page1+page2 continuation.
	all, err := dm.GetUnlinkedMentionsPaged("vault", "NB", "Sec", "Topic", "", "", 50)
	if err != nil {
		t.Fatalf("all: %v", err)
	}
	// all may hit cache too (same key) — still one scan total from cold start above.
	if unlinkedScanCalls.Load() != 1 {
		t.Fatalf("all residual from cache: scans=%d want 1", unlinkedScanCalls.Load())
	}
	combined := append(append([]UnlinkedMention{}, page1.Results...), page2.Results...)
	if len(combined) > len(all.Results) {
		t.Fatalf("combined %d > all %d", len(combined), len(all.Results))
	}
	for i := range combined {
		if combined[i].SourcePage != all.Results[i].SourcePage {
			t.Errorf("residual[%d]: got %q want %q", i, combined[i].SourcePage, all.Results[i].SourcePage)
		}
	}
}

// TestUnlinked_ScanMoreMissesCache verifies a new scan_cursor runs a fresh FTS scan.
func TestUnlinked_ScanMoreMissesCache(t *testing.T) {
	dm := newTestDB(t)
	idxUMany(t, dm, "Topic", 30)
	idxU(t, dm, "vault", "NB", "Sec", "Topic", []parser.ParsedBlock{
		noteBlock(uuidU, "Topic"),
	})

	unlinkedScanCalls.Store(0)
	if _, err := dm.GetUnlinkedMentionsPaged("vault", "NB", "Sec", "Topic", "", "", 5); err != nil {
		t.Fatalf("r1: %v", err)
	}
	if unlinkedScanCalls.Load() != 1 {
		t.Fatalf("r1 scans=%d", unlinkedScanCalls.Load())
	}
	// Different scanCursor token is a different cache key.
	fakeScan := encodeUnlinkedScanCursor(1, uuidU)
	if _, err := dm.GetUnlinkedMentionsPaged("vault", "NB", "Sec", "Topic", "", fakeScan, 5); err != nil {
		t.Fatalf("r2: %v", err)
	}
	if unlinkedScanCalls.Load() != 2 {
		t.Fatalf("new scanCursor must miss cache: scans=%d want 2", unlinkedScanCalls.Load())
	}
}

// TestUnlinked_CacheInvalidatesOnClearFileBlocks ensures watcher-style
// ClearFileBlocks(nil, ...) busts the residual FTS window cache.
func TestUnlinked_CacheInvalidatesOnClearFileBlocks(t *testing.T) {
	dm := newTestDB(t)
	idxU(t, dm, "vault", "NB", "Sec", "Topic", []parser.ParsedBlock{
		noteBlock(uuidU, "Topic"),
	})
	idxU(t, dm, "vault", "NB", "Sec", "SrcA", []parser.ParsedBlock{
		noteBlock(uuidV, "about Topic"),
	})
	idxU(t, dm, "vault", "NB", "Sec", "SrcB", []parser.ParsedBlock{
		noteBlock(uuidW, "about Topic"),
	})

	unlinkedScanCalls.Store(0)
	p1, err := dm.GetUnlinkedMentionsPaged("vault", "NB", "Sec", "Topic", "", "", 50)
	if err != nil {
		t.Fatalf("p1: %v", err)
	}
	if len(p1.Results) != 2 {
		t.Fatalf("want 2 mentions, got %d", len(p1.Results))
	}
	if unlinkedScanCalls.Load() != 1 {
		t.Fatalf("p1 scans=%d", unlinkedScanCalls.Load())
	}

	if err := dm.ClearFileBlocks(nil, "vault", "NB", "Sec", "SrcB"); err != nil {
		t.Fatalf("ClearFileBlocks: %v", err)
	}

	all, err := dm.GetUnlinkedMentionsPaged("vault", "NB", "Sec", "Topic", "", "", 50)
	if err != nil {
		t.Fatalf("all: %v", err)
	}
	if unlinkedScanCalls.Load() != 2 {
		t.Fatalf("after ClearFileBlocks scans=%d want 2", unlinkedScanCalls.Load())
	}
	for _, m := range all.Results {
		if m.SourcePage == "SrcB" {
			t.Fatalf("stale cache served deleted SrcB: %+v", all.Results)
		}
	}
	if len(all.Results) != 1 || all.Results[0].SourcePage != "SrcA" {
		t.Fatalf("want only SrcA, got %+v", all.Results)
	}
}

// TestUnlinked_LeafLookupCaseInsensitive matches PageMatchesTarget EqualFold.
func TestUnlinked_LeafLookupCaseInsensitive(t *testing.T) {
	dm := newTestDB(t)
	idxU(t, dm, "vault", "NB", "Sec", "Onboarding", []parser.ParsedBlock{
		noteBlock(uuidU, "home"),
	})
	// Second leaf differs only by case — both must surface as ambiguous for "onboarding".
	idxU(t, dm, "vault", "NB", "Other", "onboarding", []parser.ParsedBlock{
		noteBlock(uuidV, "other home"),
	})
	idxU(t, dm, "vault", "NB", "Sec", "Notes", []parser.ParsedBlock{
		noteBlock(uuidW, "see Onboarding please"),
	})

	res, err := dm.GetUnlinkedMentionsPaged("vault", "NB", "Sec", "Onboarding", "", "", 50)
	if err != nil {
		t.Fatalf("GetUnlinkedMentionsPaged: %v", err)
	}
	if len(res.Results) != 1 {
		t.Fatalf("expected 1 mention, got %d", len(res.Results))
	}
	if !res.Results[0].Ambiguous {
		t.Fatal("case-only leaf collision must be Ambiguous")
	}
	if len(res.Results[0].Candidates) < 2 {
		t.Fatalf("expected both case variants in candidates, got %+v", res.Results[0].Candidates)
	}

	// Leaf API with differently-cased query still finds stored pages.
	pages, err := dm.ListPagesByLeaf("ONBOARDING")
	if err != nil {
		t.Fatalf("ListPagesByLeaf: %v", err)
	}
	if len(pages) < 2 {
		t.Fatalf("ASCII leaf lookup: got %d want >=2: %+v", len(pages), pages)
	}
}

// TestUnlinked_LeafLookupUnicodeCaseFold covers non-ASCII case pairs that
// SQLite lower() cannot fold (Café vs CAFÉ) — must match EqualFold.
func TestUnlinked_LeafLookupUnicodeCaseFold(t *testing.T) {
	dm := newTestDB(t)
	idxU(t, dm, "vault", "NB", "Sec", "Café", []parser.ParsedBlock{
		noteBlock(uuidU, "home"),
	})
	idxU(t, dm, "vault", "NB", "Other", "CAFÉ", []parser.ParsedBlock{
		noteBlock(uuidV, "other"),
	})
	idxU(t, dm, "vault", "NB", "Sec", "Notes", []parser.ParsedBlock{
		// FTS/plain residual uses the active title string as stored on the open page.
		noteBlock(uuidW, "visit Café soon"),
	})

	pages, err := dm.ListPagesByLeaf("café")
	if err != nil {
		t.Fatalf("ListPagesByLeaf: %v", err)
	}
	if len(pages) != 2 {
		t.Fatalf("Unicode EqualFold leaf: got %d want 2: %+v", len(pages), pages)
	}

	res, err := dm.GetUnlinkedMentionsPaged("vault", "NB", "Sec", "Café", "", "", 50)
	if err != nil {
		t.Fatalf("GetUnlinkedMentionsPaged: %v", err)
	}
	if len(res.Results) != 1 {
		t.Fatalf("expected 1 mention, got %d: %+v", len(res.Results), res.Results)
	}
	if !res.Results[0].Ambiguous {
		t.Fatal("Café/CAFÉ must be Ambiguous under EqualFold")
	}
	if got := len(res.Results[0].Candidates); got < 2 {
		t.Fatalf("candidates: got %d want >=2: %+v", got, res.Results[0].Candidates)
	}
}

// TestUnlinked_CacheInvalidatesOnReindex ensures residual pages do not serve a
// stale FTS window after IndexFileBlocks (#838).
func TestUnlinked_CacheInvalidatesOnReindex(t *testing.T) {
	dm := newTestDB(t)
	idxU(t, dm, "vault", "NB", "Sec", "Topic", []parser.ParsedBlock{
		noteBlock(uuidU, "Topic"),
	})
	idxU(t, dm, "vault", "NB", "Sec", "SrcA", []parser.ParsedBlock{
		noteBlock(uuidV, "about Topic"),
	})
	idxU(t, dm, "vault", "NB", "Sec", "SrcB", []parser.ParsedBlock{
		noteBlock(uuidW, "about Topic"),
	})
	idxU(t, dm, "vault", "NB", "Sec", "SrcC", []parser.ParsedBlock{
		noteBlock(uuidX, "about Topic"),
	})

	unlinkedScanCalls.Store(0)
	p1, err := dm.GetUnlinkedMentionsPaged("vault", "NB", "Sec", "Topic", "", "", 2)
	if err != nil {
		t.Fatalf("p1: %v", err)
	}
	if unlinkedScanCalls.Load() != 1 {
		t.Fatalf("p1 scans=%d", unlinkedScanCalls.Load())
	}

	// Remove Topic mention from SrcC via reindex — batch membership changes.
	idxU(t, dm, "vault", "NB", "Sec", "SrcC", []parser.ParsedBlock{
		noteBlock(uuidX, "no mention anymore"),
	})

	// After invalidation, next call must rescan.
	all, err := dm.GetUnlinkedMentionsPaged("vault", "NB", "Sec", "Topic", "", "", 50)
	if err != nil {
		t.Fatalf("all: %v", err)
	}
	if unlinkedScanCalls.Load() != 2 {
		t.Fatalf("after reindex scans=%d want 2", unlinkedScanCalls.Load())
	}
	for _, m := range all.Results {
		if m.SourcePage == "SrcC" {
			t.Fatalf("stale cache served SrcC: %+v", all.Results)
		}
	}
	if len(all.Results) != 2 {
		t.Fatalf("want SrcA+SrcB only, got %d: %+v", len(all.Results), all.Results)
	}
	_ = p1
}

// TestUnlinked_NoFullInventoryOnPagedPath ensures GetUnlinkedMentionsPaged does
// not call listDistinctPages (#839).
func TestUnlinked_NoFullInventoryOnPagedPath(t *testing.T) {
	dm := newTestDB(t)
	// Many decoy pages with different leaves.
	for i := 0; i < 40; i++ {
		id := fmt.Sprintf("dddddddd-dddd-4ddd-8ddd-%012d", i)
		idxU(t, dm, "vault", "NB", "Sec", fmt.Sprintf("Decoy%02d", i), []parser.ParsedBlock{
			noteBlock(id, "decoy body"),
		})
	}
	idxU(t, dm, "vault", "NB", "Sec", "Topic", []parser.ParsedBlock{
		noteBlock(uuidU, "Topic"),
	})
	idxU(t, dm, "vault", "NB", "Sec", "Notes", []parser.ParsedBlock{
		noteBlock(uuidV, "see Topic please"),
	})

	before := listDistinctPagesCalls.Load()
	res, err := dm.GetUnlinkedMentionsPaged("vault", "NB", "Sec", "Topic", "", "", 50)
	if err != nil {
		t.Fatalf("GetUnlinkedMentionsPaged: %v", err)
	}
	if listDistinctPagesCalls.Load() != before {
		t.Fatalf("unlinked paged must not listDistinctPages: before=%d after=%d", before, listDistinctPagesCalls.Load())
	}
	if len(res.Results) != 1 || res.Results[0].Ambiguous {
		t.Fatalf("unique title: %+v", res.Results)
	}
}

// TestUnlinked_AmbiguousCandidateCap bounds Candidates on the wire (#839).
func TestUnlinked_AmbiguousCandidateCap(t *testing.T) {
	dm := newTestDB(t)
	const collisions = unlinkedAmbiguousCandidateCap + 5
	for i := 0; i < collisions; i++ {
		id := fmt.Sprintf("eeeeeeee-eeee-4eee-8eee-%012d", i)
		// Bodies must not contain the leaf title or they become residual hits.
		idxU(t, dm, "vault", "NB", fmt.Sprintf("S%02d", i), "Standup", []parser.ParsedBlock{
			noteBlock(id, "daily log entry"),
		})
	}
	idxU(t, dm, "vault", "NB", "Sec", "Notes", []parser.ParsedBlock{
		noteBlock(uuidU, "today Standup notes"),
	})

	res, err := dm.GetUnlinkedMentionsPaged("vault", "NB", "S00", "Standup", "", "", 50)
	if err != nil {
		t.Fatalf("GetUnlinkedMentionsPaged: %v", err)
	}
	if len(res.Results) != 1 {
		t.Fatalf("expected 1 mention, got %d", len(res.Results))
	}
	if !res.Results[0].Ambiguous {
		t.Fatal("expected Ambiguous")
	}
	if got := len(res.Results[0].Candidates); got != unlinkedAmbiguousCandidateCap {
		t.Fatalf("candidates cap: got %d want %d", got, unlinkedAmbiguousCandidateCap)
	}
	if !res.Results[0].CandidatesTruncated {
		t.Error("expected CandidatesTruncated when collisions exceed cap")
	}
	if res.Results[0].CandidatesTotal != collisions {
		t.Errorf("CandidatesTotal: got %d want %d", res.Results[0].CandidatesTotal, collisions)
	}
	// Active section affinity: S00/Standup should sort first among candidates.
	if res.Results[0].Candidates[0].Section != "S00" {
		t.Errorf("expected active section first, got %+v", res.Results[0].Candidates[0])
	}
}

// TestUnlinked_LeafLookupPlanUsesPageIndex prefers idx_blocks_page_lower.
func TestUnlinked_LeafLookupPlanUsesPageIndex(t *testing.T) {
	dm := newTestDB(t)
	idxU(t, dm, "vault", "NB", "Sec", "Topic", []parser.ParsedBlock{
		noteBlock(uuidU, "x"),
	})
	const q = `SELECT DISTINCT COALESCE(source, 'vault'), notebook, section, page
		FROM blocks WHERE lower(page) = lower(?) ORDER BY notebook, section, page`
	plans := explainDetails(t, dm, q, "Topic")
	joined := strings.ToUpper(strings.Join(plans, " | "))
	if !strings.Contains(joined, "IDX_BLOCKS_PAGE_LOWER") && !strings.Contains(joined, "PAGE") {
		t.Logf("leaf plan (index name may vary): %v", plans)
	}
	if strings.Contains(joined, "SCAN BLOCKS") && !strings.Contains(joined, "PAGE") && !strings.Contains(joined, "USING") {
		t.Errorf("unexpected full scan without page: %v", plans)
	}
}

// TestUnlinked_ScanPlanRowidKeyset documents why we use an FTS rowid subquery:
// path-ordered join plans TEMP-sort the full match set; the production shape
// limits inside blocks_fts before joining blocks.
//
// EXPLAIN QUERY PLAN wording varies by SQLite version. Hard-fail only when a
// TEMP sort is tied to the FTS match set (the regression we care about) — not
// an incidental outer ORDER BY f.rid temp that some planners may emit.
func TestUnlinked_ScanPlanRowidKeyset(t *testing.T) {
	dm := newTestDB(t)
	// Enough matches that a bad plan would sort a large set (still cheap in CI).
	idxUMany(t, dm, "Topic", 80)
	phrase := buildUnlinkedFTSPhrase("Topic")

	pathPlans := explainDetails(t, dm, unlinkedPathOrderSQL, phrase, "vault", "NB", "Sec", "Topic", unlinkedScanCap+1)
	pathJoined := strings.ToUpper(strings.Join(pathPlans, " | "))
	if !strings.Contains(pathJoined, "TEMP") {
		t.Logf("path-order plan (often TEMP B-TREE): %v", pathPlans)
	}

	rowidPlans := explainDetails(t, dm, unlinkedScanSQL, phrase, int64(0), unlinkedScanCap+1)
	rowidJoined := strings.Join(rowidPlans, " | ")
	if !strings.Contains(rowidJoined, "blocks_fts") {
		t.Errorf("expected blocks_fts in plan, got %v", rowidPlans)
	}
	// Fail only if TEMP appears on a plan line that also mentions the FTS
	// match set — that is the full-match-set sort we must not reintroduce.
	// A TEMP solely for outer ORDER BY f.rid (no FTS on that line) is ignored.
	for _, detail := range rowidPlans {
		u := strings.ToUpper(detail)
		if !strings.Contains(u, "TEMP") {
			continue
		}
		if strings.Contains(u, "BLOCKS_FTS") || strings.Contains(u, "MATCH") {
			t.Errorf("FTS match set must not TEMP-sort; plan line=%q full=%v", detail, rowidPlans)
		}
	}
	// Prefer the rowid-ordered FTS index (SQLite reports INDEX 64:…> for ORDER BY rowid).
	upper := strings.ToUpper(rowidJoined)
	if !strings.Contains(rowidJoined, "64:") && !strings.Contains(upper, "CO-ROUTINE") {
		t.Logf("plan missing INDEX 64 marker (SQLite version variance OK if no FTS TEMP): %v", rowidPlans)
	}
}
