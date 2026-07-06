package parser

import (
	"strings"
	"testing"
)

// TestParseLine_NoteAttributionTokens covers the NOTE-block comment-
// attribution tokens `[author::]` / `[ts::]` (#418): they parse into the
// dedicated ParsedBlock fields (NOT ExtraTokens), CleanText strips them, and
// a NOTE without the tokens leaves the fields empty. These model comment
// attribution — child NOTE blocks under a TASK are "comments."
//
// NOTE-only by construction: scanTaskTokens (TASK blocks) has no author/ts
// cases, so the task and NOTE token spaces are disjoint (see
// TestNoteAttribution_Isolation_TaskAuthorGoesToExtraTokens).
func TestParseLine_NoteAttributionTokens(t *testing.T) {
	t.Run("author/ts parsed into fields, CleanText stripped", func(t *testing.T) {
		line := "- comment text [author:: Alice] [ts:: 2026-07-06T15:30:00] <!-- id: 11111111-1111-1111-1111-111111111111 -->"
		block, _, _ := ParseLine(line, 1, 4)
		if block.Type != BlockNote {
			t.Fatalf("expected BlockNote, got %v", block.Type)
		}
		if block.Author != "Alice" {
			t.Errorf("expected Author='Alice', got %q", block.Author)
		}
		if block.Timestamp != "2026-07-06T15:30:00" {
			t.Errorf("expected Timestamp='2026-07-06T15:30:00', got %q", block.Timestamp)
		}
		if block.CleanText != "comment text" {
			t.Errorf("expected CleanText='comment text', got %q", block.CleanText)
		}
	})

	t.Run("blockquote prefix keeps attribution", func(t *testing.T) {
		// A NOTE block with a quote prefix (not a callout opener) still
		// carries the tokens — the comment UX supports `>`-quoted replies.
		line := "> quoted reply [author:: Bob] [ts:: 2026-07-06T16:00:00] <!-- id: 22222222-2222-2222-2222-222222222222 -->"
		block, _, _ := ParseLine(line, 1, 4)
		if block.Type != BlockNote {
			t.Fatalf("expected BlockNote for `>` prefix (no [!variant]), got %v", block.Type)
		}
		if block.Author != "Bob" {
			t.Errorf("expected Author='Bob', got %q", block.Author)
		}
		if block.Timestamp != "2026-07-06T16:00:00" {
			t.Errorf("expected Timestamp='2026-07-06T16:00:00', got %q", block.Timestamp)
		}
	})

	t.Run("tokens not duplicated into ExtraTokens", func(t *testing.T) {
		line := "- reply [author:: Carol] [ts:: 2026-07-06T09:00:00] <!-- id: 33333333-3333-3333-3333-333333333333 -->"
		block, _, _ := ParseLine(line, 1, 4)
		if block.Author == "" || block.Timestamp == "" {
			t.Fatalf("expected fields populated, got Author=%q Timestamp=%q", block.Author, block.Timestamp)
		}
		for _, tok := range block.ExtraTokens {
			lower := strings.ToLower(tok)
			if strings.Contains(lower, "author") || strings.Contains(lower, "ts") {
				t.Errorf("attribution token must not appear in ExtraTokens, found %q", tok)
			}
		}
	})

	t.Run("NOTE without the tokens keeps empty fields", func(t *testing.T) {
		line := "- a plain note <!-- id: 44444444-4444-4444-4444-444444444444 -->"
		block, _, _ := ParseLine(line, 1, 4)
		if block.Author != "" {
			t.Errorf("expected empty Author, got %q", block.Author)
		}
		if block.Timestamp != "" {
			t.Errorf("expected empty Timestamp, got %q", block.Timestamp)
		}
		if len(block.ExtraTokens) != 0 {
			t.Errorf("expected no ExtraTokens, got %v", block.ExtraTokens)
		}
		if block.CleanText != "a plain note" {
			t.Errorf("expected CleanText='a plain note', got %q", block.CleanText)
		}
	})

	t.Run("only author present (one of two tokens)", func(t *testing.T) {
		line := "- partial [author:: Dave] <!-- id: 55555555-5555-5555-5555-555555555555 -->"
		block, _, _ := ParseLine(line, 1, 4)
		if block.Author != "Dave" {
			t.Errorf("expected Author='Dave', got %q", block.Author)
		}
		if block.Timestamp != "" {
			t.Errorf("expected empty Timestamp, got %q", block.Timestamp)
		}
	})
}

// TestNoteAttribution_RoundTripByteForByte is the critical rule-#1 invariant
// for NOTE blocks: parse a NOTE with both tokens → render → the rendered line
// contains the tokens verbatim, and parse → render → parse → render is
// byte-stable. A drift here means the markdown source of truth is being
// silently mutated.
func TestNoteAttribution_RoundTripByteForByte(t *testing.T) {
	line := "- comment text [author:: Alice] [ts:: 2026-07-06T15:30:00] <!-- id: 66666666-6666-6666-6666-666666666666 -->"

	parsed, _, _ := ParseLine(line, 1, 4)
	if parsed.Author != "Alice" || parsed.Timestamp != "2026-07-06T15:30:00" {
		t.Fatalf("initial parse wrong: author=%q ts=%q", parsed.Author, parsed.Timestamp)
	}

	rendered := renderBlock(parsed, 4)
	for _, want := range []string{
		"[author:: Alice]",
		"[ts:: 2026-07-06T15:30:00]",
	} {
		if !strings.Contains(rendered, want) {
			t.Errorf("rendered line missing %q:\n%s", want, rendered)
		}
	}

	// Re-parse and assert field-level identity.
	reparsed, _, _ := ParseLine(rendered, 1, 4)
	if reparsed.Author != parsed.Author {
		t.Errorf("Author drifted: %q → %q", parsed.Author, reparsed.Author)
	}
	if reparsed.Timestamp != parsed.Timestamp {
		t.Errorf("Timestamp drifted: %q → %q", parsed.Timestamp, reparsed.Timestamp)
	}

	// Second render must be byte-identical to the first (canonical form).
	rendered2 := renderBlock(reparsed, 4)
	if rendered != rendered2 {
		t.Errorf("render is not byte-stable across two passes\n--- pass1 ---\n%s\n--- pass2 ---\n%s", rendered, rendered2)
	}
}

// TestRenderBlock_NoteAttributionOmitWhenEmpty verifies the renderer omits
// both tokens when their fields are empty — a NOTE without the tokens must
// render identically to pre-#418 output (no backfill on render).
func TestRenderBlock_NoteAttributionOmitWhenEmpty(t *testing.T) {
	block := ParsedBlock{
		ID:        "77777777-7777-7777-7777-777777777777",
		Type:      BlockNote,
		CleanText: "legacy note",
		RawText:   "- legacy note",
		FileDate:  "2026-07-06",
	}
	out := renderBlock(block, 4)
	if strings.Contains(out, "[author::") {
		t.Errorf("expected no [author::] token, got: %s", out)
	}
	if strings.Contains(out, "[ts::") {
		t.Errorf("expected no [ts::] token, got: %s", out)
	}
}

// TestNoteAttribution_Isolation_TaskAuthorGoesToExtraTokens is the critical
// isolation invariant (#418): a TASK line containing `[author:: X]` must NOT
// populate the task's Author field — scanTaskTokens has no `author` case, so
// the token falls through to ExtraTokens. This guarantees task queries (the
// `tasks` table) never pick up comment attribution. The token spaces are
// disjoint by design.
func TestNoteAttribution_Isolation_TaskAuthorGoesToExtraTokens(t *testing.T) {
	line := "- [ ] task that mentions an author [author:: Mallory] [ts:: 2026-07-06T00:00:00] <!-- id: 88888888-8888-8888-8888-888888888888 -->"
	block, _, _ := ParseLine(line, 1, 4)
	if block.Type != BlockTask {
		t.Fatalf("expected BlockTask, got %v", block.Type)
	}
	if block.Author != "" {
		t.Errorf("isolation violation: TASK Author field populated with %q — task queries must not see comment attribution", block.Author)
	}
	if block.Timestamp != "" {
		t.Errorf("isolation violation: TASK Timestamp field populated with %q", block.Timestamp)
	}
	// The unrecognized tokens land in ExtraTokens so the file still
	// round-trips (Dataview-compatible interop).
	joined := strings.Join(block.ExtraTokens, " ")
	if !strings.Contains(joined, "[author:: Mallory]") {
		t.Errorf("expected [author:: Mallory] in ExtraTokens (task token space), got %v", block.ExtraTokens)
	}
	if !strings.Contains(joined, "[ts:: 2026-07-06T00:00:00]") {
		t.Errorf("expected [ts:: 2026-07-06T00:00:00] in ExtraTokens, got %v", block.ExtraTokens)
	}
}

// TestNoteAttribution_Isolation_NoteTaskKeysIgnored verifies the inverse: a
// NOTE line containing a task-only token (`[owner::]`, `[due::]`) does NOT
// populate task-style fields on the NOTE — scanNoteTokens recognizes only
// author/ts. The unrecognized task tokens stay in CleanText verbatim (NOTE
// blocks have no ExtraTokens path — pre-#418 they kept all `[key:: value]`
// text in the rendered output).
func TestNoteAttribution_Isolation_NoteTaskKeysIgnored(t *testing.T) {
	line := "- note mentioning task syntax [owner:: Eve] [due:: 2026-08-01] [author:: Frank] <!-- id: 99999999-9999-9999-9999-999999999999 -->"
	block, _, _ := ParseLine(line, 1, 4)
	if block.Type != BlockNote {
		t.Fatalf("expected BlockNote, got %v", block.Type)
	}
	if block.Author != "Frank" {
		t.Errorf("expected Author='Frank' (recognized NOTE token), got %q", block.Author)
	}
	// Owner is a TASK field — must NOT be populated on a NOTE (there is no
	// Owner field on NOTE at all, but the point is the parser doesn't try
	// to map it). The [owner::] / [due::] tokens stay in CleanText verbatim
	// (the pre-#418 behavior for arbitrary Dataview tokens on notes).
	if !strings.Contains(block.CleanText, "[owner:: Eve]") {
		t.Errorf("expected [owner:: Eve] preserved in CleanText, got %q", block.CleanText)
	}
	if !strings.Contains(block.CleanText, "[due:: 2026-08-01]") {
		t.Errorf("expected [due:: 2026-08-01] preserved in CleanText, got %q", block.CleanText)
	}
	// The recognized [author:: Frank] token was stripped from CleanText
	// (mapped to the Author field instead).
	if strings.Contains(block.CleanText, "[author::") {
		t.Errorf("expected [author::] stripped from CleanText, got %q", block.CleanText)
	}
}
