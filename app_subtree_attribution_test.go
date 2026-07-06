package main

import (
	"testing"
)

// TestFetchSubtree_ChildNoteAttribution verifies the FetchSubtree read path
// hydrates NOTE-block comment attribution (#418): a child NOTE carrying
// [author::]/[ts::] comes back with Author/Timestamp populated, and a child
// NOTE without them comes back empty. FetchSubtree parses the markdown file
// directly and returns ParsedBlock — the Author/Timestamp fields flow through
// automatically from the markdown source of truth (no DB JOIN needed; the
// block_meta table serves SQL-based queries like plugin sqliteQuery).
func TestFetchSubtree_ChildNoteAttribution(t *testing.T) {
	app := newTestApp(t)
	const (
		parent     = "d1e2f3a4-0000-0000-0000-000000000001"
		withAttrib = "d1e2f3a4-0000-0000-0000-000000000002"
		without    = "d1e2f3a4-0000-0000-0000-000000000003"
	)
	content := "- [ ] parent task <!-- id: " + parent + " -->\n" +
		"\t- reply [author:: Alice] [ts:: 2026-07-06T15:30:00] <!-- id: " + withAttrib + " -->\n" +
		"\t- plain reply <!-- id: " + without + " -->\n"
	indexTestFile(t, app, "W", "S", "FetchAttrib", "2026-07-06", content)

	got, err := app.FetchSubtree(parent)
	if err != nil {
		t.Fatalf("FetchSubtree: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 children, got %d: %+v", len(got), got)
	}

	// First child carries attribution tokens.
	if got[0].ID != withAttrib {
		t.Fatalf("child 0 = %s, want %s", got[0].ID, withAttrib)
	}
	if got[0].Author != "Alice" {
		t.Errorf("child 0 Author = %q, want 'Alice'", got[0].Author)
	}
	if got[0].Timestamp != "2026-07-06T15:30:00" {
		t.Errorf("child 0 Timestamp = %q, want '2026-07-06T15:30:00'", got[0].Timestamp)
	}
	if got[0].CleanText != "reply" {
		t.Errorf("child 0 CleanText = %q, want 'reply' (tokens stripped)", got[0].CleanText)
	}

	// Second child has no tokens — fields stay empty.
	if got[1].ID != without {
		t.Fatalf("child 1 = %s, want %s", got[1].ID, without)
	}
	if got[1].Author != "" {
		t.Errorf("child 1 Author = %q, want empty", got[1].Author)
	}
	if got[1].Timestamp != "" {
		t.Errorf("child 1 Timestamp = %q, want empty", got[1].Timestamp)
	}
}
