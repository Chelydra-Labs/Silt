package parser

import "strings"

// IsCommentBlock reports whether b is a comment block: a NOTE carrying the
// explicit [comment::] marker or comment attribution ([author::] and/or
// [ts::]). Attribution stays a permanent fallback so every comment written
// before the marker existed remains classified. The NOTE type alone is not
// the test — prose in the sub-notes editor is also an unattributed NOTE.
// Mirrors commentBlocks.ts's isCommentBlock, which the editor uses to keep
// comments out of the TipTap document.
func IsCommentBlock(b ParsedBlock) bool {
	if b.Type != BlockNote {
		return false
	}
	return b.IsComment ||
		strings.TrimSpace(b.Author) != "" || strings.TrimSpace(b.Timestamp) != ""
}
