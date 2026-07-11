package db

import (
	"database/sql"
	"testing"

	"silt/backend/parser"
)

// sampleNoteBlockWithAttribution builds a NOTE block carrying [author::] /
// [ts::] for the block_meta projection tests (#418).
func sampleNoteBlockWithAttribution(id string, line int, author, ts string) parser.ParsedBlock {
	b := sampleNoteBlock(id, line)
	b.Author = author
	b.Timestamp = ts
	return b
}

// TestIndexFileBlocks_BlockMetaProjection verifies the NOTE-block comment-
// attribution tokens (`[author::]` / `[ts::]`, #418) project into the sparse
// block_meta table: NOTE blocks with the tokens get a row, NOTE blocks
// without them get NO row, and TASK blocks never get a row (disjoint token
// spaces — scanTaskTokens has no author/ts cases).
func TestIndexFileBlocks_BlockMetaProjection(t *testing.T) {
	dm := newTestDB(t)

	const (
		noteBoth   = "aaaaaaaa-1111-1111-1111-111111111111"
		noteAuthor = "aaaaaaaa-2222-2222-2222-222222222222"
		noneNote   = "aaaaaaaa-3333-3333-3333-333333333333"
		taskID     = "aaaaaaaa-4444-4444-4444-444444444444"
	)
	blocks := []parser.ParsedBlock{
		sampleNoteBlockWithAttribution(noteBoth, 1, "Alice", "2026-07-06T15:30:00"),
		sampleNoteBlockWithAttribution(noteAuthor, 2, "Bob", ""),
		sampleNoteBlock(noneNote, 3),
		func() parser.ParsedBlock {
			// A TASK block that (hypothetically) had Author/Timestamp set
			// on the struct — the indexer must NOT project it into
			// block_meta (NOTE-only projection).
			b := sampleTaskBlock(taskID, 4)
			b.Author = "Mallory"
			b.Timestamp = "2026-07-06T00:00:00"
			return b
		}(),
	}
	if err := dm.IndexFileBlocks("vault", "Work", "Journal", "Daily", blocks, nil); err != nil {
		t.Fatalf("IndexFileBlocks: %v", err)
	}

	cases := []struct {
		id             string
		wantRow        bool
		authorValid    bool
		authorVal      string
		timestampValid bool
		timestampVal   string
	}{
		{noteBoth, true, true, "Alice", true, "2026-07-06T15:30:00"},
		{noteAuthor, true, true, "Bob", false, ""}, // empty ts → NULL
		{noneNote, false, false, "", false, ""},    // no tokens → no row
		{taskID, false, false, "", false, ""},      // NOTE-only — task never projected
	}
	for _, c := range cases {
		var author, timestamp sql.NullString
		err := dm.SQLDB().QueryRow(
			"SELECT author, timestamp FROM block_meta WHERE block_id = ?", c.id,
		).Scan(&author, &timestamp)
		if c.wantRow {
			if err != nil {
				t.Errorf("%s: expected block_meta row, got scan error: %v", c.id, err)
				continue
			}
			if author.Valid != c.authorValid {
				t.Errorf("%s: author valid=%v want %v", c.id, author.Valid, c.authorValid)
			} else if author.Valid && author.String != c.authorVal {
				t.Errorf("%s: author=%q want %q", c.id, author.String, c.authorVal)
			}
			if timestamp.Valid != c.timestampValid {
				t.Errorf("%s: timestamp valid=%v want %v", c.id, timestamp.Valid, c.timestampValid)
			} else if timestamp.Valid && timestamp.String != c.timestampVal {
				t.Errorf("%s: timestamp=%q want %q", c.id, timestamp.String, c.timestampVal)
			}
		} else {
			if err == nil {
				t.Errorf("%s: expected NO block_meta row, got one (author=%v ts=%v)", c.id, author, timestamp)
			}
		}
	}

	// Total row count: only noteBoth + noteAuthor (noneNote has no row,
	// taskID is NOTE-only-projection so never gets one).
	var rowCount int
	if err := dm.SQLDB().QueryRow("SELECT COUNT(*) FROM block_meta").Scan(&rowCount); err != nil {
		t.Fatalf("count block_meta: %v", err)
	}
	if rowCount != 2 {
		t.Errorf("expected 2 block_meta rows, got %d", rowCount)
	}
}

// TestIndexFileBlocks_BlockMetaReindexNoStaleRows is the re-derivable cache
// invariant (rule #4): re-indexing the same blocks must not accumulate stale
// rows, and re-indexing with tokens REMOVED must delete the now-stale row.
// The table is sparse and must always match the current markdown state.
func TestIndexFileBlocks_BlockMetaReindexNoStaleRows(t *testing.T) {
	dm := newTestDB(t)

	const (
		keep   = "bbbbbbbb-1111-1111-1111-111111111111"
		clear  = "bbbbbbbb-2222-2222-2222-222222222222"
		addNew = "bbbbbbbb-3333-3333-3333-333333333333"
	)
	// First index: keep + clear both carry attribution.
	first := []parser.ParsedBlock{
		sampleNoteBlockWithAttribution(keep, 1, "Alice", "2026-07-06T10:00:00"),
		sampleNoteBlockWithAttribution(clear, 2, "Bob", "2026-07-06T11:00:00"),
	}
	if err := dm.IndexFileBlocks("vault", "Work", "Journal", "Daily", first, nil); err != nil {
		t.Fatalf("first index: %v", err)
	}
	var n int
	if err := dm.SQLDB().QueryRow("SELECT COUNT(*) FROM block_meta").Scan(&n); err != nil {
		t.Fatalf("count after first: %v", err)
	}
	if n != 2 {
		t.Fatalf("expected 2 rows after first index, got %d", n)
	}

	// Second index: keep unchanged, clear has tokens REMOVED, addNew is new.
	second := []parser.ParsedBlock{
		sampleNoteBlockWithAttribution(keep, 1, "Alice", "2026-07-06T10:00:00"), // unchanged
		sampleNoteBlock(clear, 2), // tokens cleared → row must delete
		sampleNoteBlockWithAttribution(addNew, 3, "Carol", "2026-07-06T12:00:00"), // new
	}
	if err := dm.IndexFileBlocks("vault", "Work", "Journal", "Daily", second, nil); err != nil {
		t.Fatalf("second index: %v", err)
	}

	// Expect exactly 2 rows: keep + addNew. `clear` must be gone (no
	// accumulation of stale rows; the cleared token deletes the row).
	if err := dm.SQLDB().QueryRow("SELECT COUNT(*) FROM block_meta").Scan(&n); err != nil {
		t.Fatalf("count after reindex: %v", err)
	}
	if n != 2 {
		t.Errorf("expected 2 rows after reindex (no stale accumulation), got %d", n)
	}

	// `clear` should have no row now (tokens were removed).
	var dummy string
	err := dm.SQLDB().QueryRow("SELECT author FROM block_meta WHERE block_id = ?", clear).Scan(&dummy)
	if err == nil {
		t.Errorf("expected `clear` block_meta row to be deleted after tokens removed, but found author=%q", dummy)
	}

	// `keep` should still carry its values (upsert overwrote, not duplicated).
	var author, timestamp string
	if err := dm.SQLDB().QueryRow("SELECT author, timestamp FROM block_meta WHERE block_id = ?", keep).Scan(&author, &timestamp); err != nil {
		t.Errorf("keep row lost after reindex: %v", err)
	}
	if author != "Alice" || timestamp != "2026-07-06T10:00:00" {
		t.Errorf("keep row drift: author=%q ts=%q", author, timestamp)
	}
}

// TestIndexScanResults_BlockMetaProjection mirrors the IndexFileBlocks
// projection test for the batched scan-results path. Both indexers must
// agree: NOTE blocks with [author::]/[ts::] get a sparse block_meta row.
func TestIndexScanResults_BlockMetaProjection(t *testing.T) {
	dm := newTestDB(t)

	const (
		noteWithBoth = "cccccccc-1111-1111-1111-111111111111"
		noteWithout  = "cccccccc-2222-2222-2222-222222222222"
	)
	results := []parser.ScanResult{{
		Notebook: "Work",
		Section:  "Journal",
		Page:     "Daily",
		Source:   "vault",
		Blocks: []parser.ParsedBlock{
			sampleNoteBlockWithAttribution(noteWithBoth, 1, "Dave", "2026-07-06T14:00:00"),
			sampleNoteBlock(noteWithout, 2),
		},
	}}
	if _, _, err := dm.IndexScanResults(results); err != nil {
		t.Fatalf("IndexScanResults: %v", err)
	}

	var author, timestamp string
	if err := dm.SQLDB().QueryRow(
		"SELECT author, timestamp FROM block_meta WHERE block_id = ?", noteWithBoth,
	).Scan(&author, &timestamp); err != nil {
		t.Fatalf("expected block_meta row for %s: %v", noteWithBoth, err)
	}
	if author != "Dave" || timestamp != "2026-07-06T14:00:00" {
		t.Errorf("projection drift: author=%q ts=%q", author, timestamp)
	}

	// NOTE without tokens → no row.
	var dummy string
	err := dm.SQLDB().QueryRow("SELECT author FROM block_meta WHERE block_id = ?", noteWithout).Scan(&dummy)
	if err == nil {
		t.Errorf("expected NO block_meta row for token-less NOTE, got author=%q", dummy)
	}
}
