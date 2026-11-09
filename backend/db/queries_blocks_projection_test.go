package db

// FetchPageBlocks is the block editor's read model: its rows re-render
// through RenderFileContent on every save, so every file-resident token the
// parser round-trips must survive the DB projection — a field the SELECT
// drops is stripped from the file on the next save, and a lossy
// reconstruction (estimate minutes) false-drifts the rows-anchored load
// baseline into a permanent refusal loop (#1005).

import (
	"testing"

	"silt/backend/parser"
)

// TestFetchPageBlocks_HydratesFileResidentTokens pins the projection for
// [recur::], [progress::] and the raw [estimate::] token: the rows must
// carry exactly what the parser extracts from the same raw line, not a
// display-formatted stand-in.
func TestFetchPageBlocks_HydratesFileResidentTokens(t *testing.T) {
	dm := newTestDB(t)
	id := "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
	raw := "- [ ] ship it [recur:: every week] [progress:: 50] [estimate:: 90m] <!-- id: " + id + " -->"
	blocks, _, _, _, err := parser.ParseFileContent(
		"---\nnotebook: \"Work\"\nsection: \"Journal\"\npage: \"Daily\"\ndate: \"2026-01-01\"\ntags: []\n---\n"+raw+"\n",
		"Work", "Journal", "Daily", "2026-01-01", 4)
	if err != nil {
		t.Fatalf("ParseFileContent: %v", err)
	}
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0].Recurrence != "every week" || blocks[0].Progress != 50 || blocks[0].Estimate != "90m" {
		t.Fatalf("seed parse lost tokens: %+v", blocks[0])
	}
	if err := dm.IndexFileBlocks("vault", "Work", "Journal", "Daily", blocks, nil); err != nil {
		t.Fatalf("IndexFileBlocks: %v", err)
	}

	got, err := dm.FetchPageBlocks("vault", "Work", "Journal", "Daily")
	if err != nil {
		t.Fatalf("FetchPageBlocks: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 row, got %d", len(got))
	}
	if got[0].Recurrence != "every week" {
		t.Errorf("Recurrence not hydrated: got %q, want %q", got[0].Recurrence, "every week")
	}
	if got[0].Progress != 50 {
		t.Errorf("Progress not hydrated: got %d, want 50", got[0].Progress)
	}
	// The raw token form, not FormatEstimateMinutes(90) == "1.5h".
	if got[0].Estimate != "90m" {
		t.Errorf("Estimate must be the raw token form: got %q, want %q", got[0].Estimate, "90m")
	}
}

// TestFetchPageBlocks_HydratesPinBlockedByAndUnknownTokens pins the projection
// for the remaining render-emitted task tokens: [pin::] (tri-state),
// [blocked_by:: ((uuid))] and unknown [key:: value] tokens. Like
// recur/progress/estimate, their only storage is the raw line — a SELECT that
// drops them strips the tokens from the file on every rows-echo save (global
// replace, MCP update_blocks) and blinds the rows-anchored load-baseline
// comparison (#1005).
func TestFetchPageBlocks_HydratesPinBlockedByAndUnknownTokens(t *testing.T) {
	dm := newTestDB(t)
	id := "ddddddaa-dddd-4ddd-8ddd-ddddddddddda"
	depID := "ddddddbb-dddd-4ddd-8ddd-dddddddddddb"
	raw := "- [ ] pinned task [pin:: true] [blocked_by:: ((" + depID + "))] [mood:: focused] <!-- id: " + id + " -->"
	blocks, _, _, _, err := parser.ParseFileContent(
		"---\nnotebook: \"Work\"\nsection: \"Journal\"\npage: \"Pins\"\ndate: \"2026-01-02\"\ntags: []\n---\n"+raw+"\n",
		"Work", "Journal", "Pins", "2026-01-02", 4)
	if err != nil {
		t.Fatalf("ParseFileContent: %v", err)
	}
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	seed := blocks[0]
	if seed.Pinned == nil || !*seed.Pinned {
		t.Fatalf("seed parse lost pin: %+v", seed.Pinned)
	}
	if len(seed.BlockedBy) != 1 || seed.BlockedBy[0] != depID {
		t.Fatalf("seed parse lost blocked_by: %+v", seed.BlockedBy)
	}
	if len(seed.ExtraTokens) != 1 || seed.ExtraTokens[0] != "[mood:: focused]" {
		t.Fatalf("seed parse lost unknown token: %+v", seed.ExtraTokens)
	}
	if err := dm.IndexFileBlocks("vault", "Work", "Journal", "Pins", blocks, nil); err != nil {
		t.Fatalf("IndexFileBlocks: %v", err)
	}

	got, err := dm.FetchPageBlocks("vault", "Work", "Journal", "Pins")
	if err != nil {
		t.Fatalf("FetchPageBlocks: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 row, got %d", len(got))
	}
	// Tri-state pin: re-derived from the raw line, so nil/false/true all
	// survive like the queries_tasks.go column path.
	if got[0].Pinned == nil || !*got[0].Pinned {
		t.Errorf("Pinned not hydrated: got %v, want &true", got[0].Pinned)
	}
	if len(got[0].BlockedBy) != 1 || got[0].BlockedBy[0] != depID {
		t.Errorf("BlockedBy not hydrated: got %v, want [%s]", got[0].BlockedBy, depID)
	}
	if len(got[0].ExtraTokens) != 1 || got[0].ExtraTokens[0] != "[mood:: focused]" {
		t.Errorf("ExtraTokens not hydrated: got %v, want [mood:: focused] (verbatim token)", got[0].ExtraTokens)
	}
}

// TestFetchPageBlocks_HydratesShortlistRank pins the [shortlist::] projection
// (#1092): like pin/blocked_by/unknown tokens, the rank's only storage is the
// raw line, so a hydration gap would strip [shortlist:: 0] from the file on
// every rows-echo save. Rank 0 must hydrate to &0 (a real top rank), and no
// token must hydrate to nil.
func TestFetchPageBlocks_HydratesShortlistRank(t *testing.T) {
	dm := newTestDB(t)
	rankedID := "ddddddaa-dddd-4ddd-8ddd-dddddddddddc"
	unrankedID := "ddddddaa-dddd-4ddd-8ddd-dddddddddddd"
	rankedRaw := "- [ ] top focus [shortlist:: 0] <!-- id: " + rankedID + " -->"
	unrankedRaw := "- [ ] plain task <!-- id: " + unrankedID + " -->"
	fm := "---\nnotebook: \"Work\"\nsection: \"Journal\"\npage: \"Ranks\"\ndate: \"2026-01-03\"\ntags: []\n---\n" + rankedRaw + "\n" + unrankedRaw + "\n"
	blocks, _, _, _, err := parser.ParseFileContent(fm, "Work", "Journal", "Ranks", "2026-01-03", 4)
	if err != nil {
		t.Fatalf("ParseFileContent: %v", err)
	}
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(blocks))
	}
	if blocks[0].ShortlistRank == nil || *blocks[0].ShortlistRank != 0 {
		t.Fatalf("seed parse lost rank 0: %+v", blocks[0].ShortlistRank)
	}
	if blocks[1].ShortlistRank != nil {
		t.Fatalf("expected nil seed rank, got %+v", blocks[1].ShortlistRank)
	}
	if err := dm.IndexFileBlocks("vault", "Work", "Journal", "Ranks", blocks, nil); err != nil {
		t.Fatalf("IndexFileBlocks: %v", err)
	}

	got, err := dm.FetchPageBlocks("vault", "Work", "Journal", "Ranks")
	if err != nil {
		t.Fatalf("FetchPageBlocks: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(got))
	}
	if got[0].ShortlistRank == nil || *got[0].ShortlistRank != 0 {
		t.Errorf("rank 0 not hydrated: got %v, want &0 (top rank, not unranked)", got[0].ShortlistRank)
	}
	if got[1].ShortlistRank != nil {
		t.Errorf("unranked task hydrated a rank: got %v, want nil", got[1].ShortlistRank)
	}
}
