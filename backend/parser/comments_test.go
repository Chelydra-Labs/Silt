package parser

import "testing"

// TestIsCommentBlock pins the shared attribution predicate: a comment is a
// NOTE with [author::] and/or [ts::]; a NOTE without either is prose, not a
// provably-attributed comment.
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
