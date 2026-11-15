package parser

import (
	"strings"
	"testing"
)

// TestIsCommentBlock pins the shared comment predicate: a comment is a NOTE
// carrying the explicit [comment::] marker or [author::]/[ts::] attribution;
// a NOTE with neither is prose, and the marker never classifies non-NOTEs.
func TestIsCommentBlock(t *testing.T) {
	tests := []struct {
		name  string
		block ParsedBlock
		want  bool
	}{
		{
			name:  "author only",
			block: ParsedBlock{Type: BlockNote, Author: "Dana"},
			want:  true,
		},
		{
			name:  "timestamp only",
			block: ParsedBlock{Type: BlockNote, Timestamp: "2026-07-07T14:22:00"},
			want:  true,
		},
		{
			name:  "author and timestamp",
			block: ParsedBlock{Type: BlockNote, Author: "Dana", Timestamp: "2026-07-07T14:22:00"},
			want:  true,
		},
		{
			name:  "explicit marker only",
			block: ParsedBlock{Type: BlockNote, IsComment: true},
			want:  true,
		},
		{
			name:  "marker plus attribution",
			block: ParsedBlock{Type: BlockNote, IsComment: true, Author: "Dana", Timestamp: "2026-07-07T14:22:00"},
			want:  true,
		},
		{
			name:  "neither token (prose note)",
			block: ParsedBlock{Type: BlockNote, CleanText: "just prose"},
			want:  false,
		},
		{
			name:  "whitespace-only attribution is not a token",
			block: ParsedBlock{Type: BlockNote, Author: "  ", Timestamp: "\t"},
			want:  false,
		},
		{
			name:  "task with attribution-shaped extras",
			block: ParsedBlock{Type: BlockTask, Author: "Dana", Timestamp: "2026-07-07T14:22:00"},
			want:  false,
		},
		{
			name:  "task with the marker is still not a comment",
			block: ParsedBlock{Type: BlockTask, IsComment: true},
			want:  false,
		},
		{
			name:  "non-NOTE with an author",
			block: ParsedBlock{Type: BlockHeader, Author: "Dana"},
			want:  false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsCommentBlock(tc.block); got != tc.want {
				t.Errorf("IsCommentBlock(%+v) = %v, want %v", tc.block, got, tc.want)
			}
		})
	}
}

// TestParseNoteCommentMarker covers the [comment::] token's grammar (#1159):
// the key is case-insensitive and any value — empty or not, surrounded by
// whitespace or not — marks the NOTE as a comment. The token is stripped from
// the displayed text in every form; an unhandled marker would render as body
// prose in both editors.
func TestParseNoteCommentMarker(t *testing.T) {
	tests := []struct {
		name string
		line string
	}{
		{
			name: "empty value",
			line: "- body [comment::] <!-- id: 1111aaaa-0000-0000-0000-000000000001 -->",
		},
		{
			name: "explicit true value",
			line: "- body [comment:: true] <!-- id: 1111aaaa-0000-0000-0000-000000000002 -->",
		},
		{
			name: "case-insensitive key",
			line: "- body [COMMENT::] <!-- id: 1111aaaa-0000-0000-0000-000000000003 -->",
		},
		{
			name: "surrounding whitespace",
			line: "- body   [comment:: ]   <!-- id: 1111aaaa-0000-0000-0000-000000000004 -->",
		},
		{
			name: "leading position",
			line: "- [comment::] body <!-- id: 1111aaaa-0000-0000-0000-000000000005 -->",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			block, _, _ := ParseLine(tc.line, 1, 4)
			if block.Type != BlockNote {
				t.Fatalf("expected BlockNote, got %v", block.Type)
			}
			if !block.IsComment {
				t.Errorf("expected IsComment=true for %q", tc.line)
			}
			if strings.Contains(strings.ToLower(block.CleanText), "comment::") {
				t.Errorf("marker must be stripped from CleanText, got %q", block.CleanText)
			}
			if block.CleanText != "body" {
				t.Errorf("expected CleanText='body', got %q", block.CleanText)
			}
		})
	}
}

// TestParseTaskCommentMarkerStaysExtraToken guards the disjoint token spaces:
// [comment::] on a TASK line is not comment metadata (scanTaskTokens has no
// `comment` case), so it falls through to ExtraTokens and the task query never
// sees it.
func TestParseTaskCommentMarkerStaysExtraToken(t *testing.T) {
	line := "- [ ] a task [comment::] <!-- id: 1111aaaa-0000-0000-0000-000000000006 -->"
	block, _, _ := ParseLine(line, 1, 4)
	if block.Type != BlockTask {
		t.Fatalf("expected BlockTask, got %v", block.Type)
	}
	if block.IsComment {
		t.Error("a TASK line must never parse IsComment=true")
	}
	joined := strings.Join(block.ExtraTokens, " ")
	if !strings.Contains(joined, "[comment::]") {
		t.Errorf("expected [comment::] preserved in ExtraTokens, got %v", block.ExtraTokens)
	}
}
