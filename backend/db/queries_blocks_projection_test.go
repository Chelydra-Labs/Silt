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
