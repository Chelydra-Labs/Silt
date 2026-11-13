package parser

import "strings"

// IsCommentBlock reports whether b is a comment block: a NOTE carrying
// comment attribution ([author::] and/or [ts::]). Attribution — not the NOTE
// type alone — is the test, because prose in the sub-notes editor is also an
// unattributed NOTE. Mirrors commentBlocks.ts's isCommentBlock, which the
// editor uses to keep comments out of the TipTap document.
func IsCommentBlock(b ParsedBlock) bool {
	if b.Type != BlockNote {
		return false
	}
	return strings.TrimSpace(b.Author) != "" || strings.TrimSpace(b.Timestamp) != ""
}
