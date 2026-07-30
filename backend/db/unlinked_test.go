package db

import (
	"fmt"
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

	res, err := dm.GetUnlinkedMentionsPaged("vault", "NB", "Sec", "Onboarding", "", 50)
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

	res, err := dm.GetUnlinkedMentionsPaged("vault", "NB", "Sec", "Onboarding Friction", "", 50)
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

	res, err := dm.GetUnlinkedMentionsPaged("vault", "NB", "Sec", "Onboarding", "", 50)
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

	res, err := dm.GetUnlinkedMentionsPaged("vault", "NB", "Sec", "Target", "", 50)
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

	res, err := dm.GetUnlinkedMentionsPaged("vault", "NB", "Sec", "Café", "", 50)
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

	res, err := dm.GetUnlinkedMentionsPaged("vault", "NB", "Sec", "Onboarding", "", 50)
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

	res, err := dm.GetUnlinkedMentionsPaged("vault", "NB", "Sec", "Onboarding", "", 50)
	if err != nil {
		t.Fatalf("GetUnlinkedMentionsPaged: %v", err)
	}
	if len(res.Results) != 0 {
		t.Fatalf("CODE-block mention should be excluded, got %d: %+v", len(res.Results), res.Results)
	}
}

// TestUnlinked_AlreadyLinkedExcluded verifies a block that already contains a
// [[…]] resolving to the target page is NOT surfaced as unlinked.
func TestUnlinked_AlreadyLinkedExcluded(t *testing.T) {
	dm := newTestDB(t)
	idxU(t, dm, "vault", "NB", "Sec", "Onboarding", []parser.ParsedBlock{
		noteBlock(uuidU, "Onboarding"),
	})
	idxU(t, dm, "vault", "NB", "Sec", "Linked", []parser.ParsedBlock{
		noteBlock(uuidV, "see [[Onboarding]] for the Onboarding details"),
	})
	idxU(t, dm, "vault", "NB", "Sec", "Plain", []parser.ParsedBlock{
		noteBlock(uuidW, "see Onboarding details"),
	})

	res, err := dm.GetUnlinkedMentionsPaged("vault", "NB", "Sec", "Onboarding", "", 50)
	if err != nil {
		t.Fatalf("GetUnlinkedMentionsPaged: %v", err)
	}
	if len(res.Results) != 1 {
		t.Fatalf("expected only the Plain page (Linked excluded), got %d: %+v", len(res.Results), res.Results)
	}
	if res.Results[0].SourcePage != "Plain" {
		t.Errorf("expected Plain, got %q", res.Results[0].SourcePage)
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

	res, err := dm.GetUnlinkedMentionsPaged("vault", "NB", "Sec", "Onboarding", "", 50)
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

	res, err := dm.GetUnlinkedMentionsPaged("vault", "NB", "Journal", "Standup", "", 50)
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

	res, err := dm.GetUnlinkedMentionsPaged("vault", "NB", "Sec", "Onboarding", "", 50)
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
		res, err := dm.GetUnlinkedMentionsPaged("vault", "NB", "Sec", "Onboarding", cursor, 5)
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
	r0, _ := dm.GetUnlinkedMentionsPaged("vault", "NB", "Sec", "Onboarding", "", 0)
	if len(r0.Results) != 3 {
		t.Errorf("limit 0 (default 50): expected all 3, got %d", len(r0.Results))
	}
	rBig, _ := dm.GetUnlinkedMentionsPaged("vault", "NB", "Sec", "Onboarding", "", 99999)
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
	res, err := dm.GetUnlinkedMentionsPaged("vault", "NB", "Sec", "A", "", 50)
	if err != nil {
		t.Fatalf("GetUnlinkedMentionsPaged: %v", err)
	}
	if len(res.Results) != 0 {
		t.Errorf("1-rune title should be skipped, got %d", len(res.Results))
	}
}

// TestUnlinked_DbClosed returns ErrDBClosed.
func TestUnlinked_DbClosed(t *testing.T) {
	dm := newTestDB(t)
	_ = dm.Close()
	_, err := dm.GetUnlinkedMentionsPaged("vault", "NB", "Sec", "Onboarding", "", 50)
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
	res, _ := dm.GetUnlinkedMentionsPaged("vault", "NB", "Sec", "Onboarding", "", 50)
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
	res, _ = dm.GetUnlinkedMentionsPaged("vault", "NB", "Sec", "Onboarding", "", 50)
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

	res, _ := dm.GetUnlinkedMentionsPaged("vault", "NB", "Journal", "Standup", "", 50)
	if len(res.Results) != 1 || !res.Results[0].Ambiguous {
		t.Fatalf("expected 1 ambiguous mention, got %+v", res.Results)
	}
}
