package mcp

import (
	"strings"

	"silt/backend/parser"
)

// headingMatch is one HEADER on a page with its hierarchical path
// (ancestor CleanText joined by "::", leaf last).
type headingMatch struct {
	ID   string
	Path string // e.g. "Meeting::Notes"
}

// matchHeadings finds HEADER blocks whose path matches heading.
// Bare names match the leaf case-insensitively; paths containing "::" match
// the full ancestor path case-insensitively. Depth stack rebuilds hierarchy
// from sequential HEADER Depth fields (same model as outline nesting).
func matchHeadings(blocks []parser.ParsedBlock, heading string) []headingMatch {
	want := strings.TrimSpace(heading)
	if want == "" {
		return nil
	}
	fullPath := strings.Contains(want, "::")
	wantLower := strings.ToLower(want)

	// stack[i] is the CleanText of the open header at depth i+1 (markdown # = 1).
	var stack []string
	var matches []headingMatch

	for _, b := range blocks {
		if b.Type != parser.BlockHeader {
			continue
		}
		depth := b.Depth
		if depth < 1 {
			depth = 1
		}
		// Pop deeper-or-equal levels so this header becomes the leaf at depth.
		if depth <= len(stack) {
			stack = stack[:depth-1]
		}
		// Pad gaps if a file jumps levels (rare); empty segments keep path stable.
		for len(stack) < depth-1 {
			stack = append(stack, "")
		}
		leaf := strings.TrimSpace(b.CleanText)
		stack = append(stack, leaf)
		path := strings.Join(stack, "::")
		pathLower := strings.ToLower(path)
		leafLower := strings.ToLower(leaf)
		ok := false
		if fullPath {
			ok = pathLower == wantLower
		} else {
			ok = leafLower == wantLower
		}
		if ok {
			matches = append(matches, headingMatch{ID: b.ID, Path: path})
		}
	}
	return matches
}
