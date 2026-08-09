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

	var matches []headingMatch
	for _, h := range listHeadingPaths(blocks) {
		ok := false
		if fullPath {
			ok = strings.ToLower(h.Path) == wantLower
		} else {
			// Leaf is the last :: segment.
			leaf := h.Path
			if i := strings.LastIndex(leaf, "::"); i >= 0 {
				leaf = leaf[i+2:]
			}
			ok = strings.ToLower(leaf) == wantLower
		}
		if ok {
			matches = append(matches, h)
		}
	}
	return matches
}

// listHeadingPaths returns every HEADER on the page with its hierarchical path.
// Used for match + not-found candidate lists (teach-the-model errors).
func listHeadingPaths(blocks []parser.ParsedBlock) []headingMatch {
	// stack[i] is the CleanText of the open header at depth i+1 (markdown # = 1).
	var stack []string
	var out []headingMatch
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
		out = append(out, headingMatch{ID: b.ID, Path: strings.Join(stack, "::")})
	}
	return out
}

// headingCandidatePaths returns up to max unique HEADER paths for error payloads.
func headingCandidatePaths(blocks []parser.ParsedBlock, max int) []string {
	if max <= 0 {
		max = 20
	}
	seen := map[string]bool{}
	var paths []string
	for _, h := range listHeadingPaths(blocks) {
		if h.Path == "" || seen[h.Path] {
			continue
		}
		seen[h.Path] = true
		paths = append(paths, h.Path)
		if len(paths) >= max {
			break
		}
	}
	return paths
}
