package parser

import "fmt"

// HierarchyViolation describes one block whose on-disk indentation could not
// resolve to the parent its depth implies. Produced only by DeriveHierarchy.
type HierarchyViolation struct {
	BlockID    string
	Type       BlockType
	LineNumber int
	Depth      int
	// ParentID / ParentDepth describe the repair: the nearest resolvable
	// ancestor the block was attached to. ParentID is empty (ParentDepth -1)
	// when no ancestor existed at all — the file opens indented — in which
	// case the block stays parentless at its own depth.
	ParentID    string
	ParentDepth int
}

// String renders the violation for the per-file warnings channel, in the same
// spirit as the frontmatter warnings: a one-line diagnostic a maintainer can
// grep and a user can act on (fix the indentation in the source note).
func (v HierarchyViolation) String() string {
	if v.ParentID == "" {
		return fmt.Sprintf("hierarchy violation: %s block %s (line %d) at depth %d has no block above it to parent to; left parentless",
			v.Type, v.BlockID, v.LineNumber, v.Depth)
	}
	return fmt.Sprintf("hierarchy violation (repaired): %s block %s (line %d) at depth %d has no block at depth %d; attached to nearest resolvable parent %s at depth %d",
		v.Type, v.BlockID, v.LineNumber, v.Depth, v.Depth-1, v.ParentID, v.ParentDepth)
}

// DeriveHierarchy is the ONLY place block parentage is derived. It walks the
// slice once in file order and assigns each block's ParentID in place from its
// Depth:
//
//   - a depth 0 block is a root (ParentID "").
//   - a depth > 0 block's parent is the most recent block at depth-1.
//   - when no block sits at depth-1 (an indentation jump — e.g. an external
//     edit landing at depth 4 straight after a depth-0 block), the block is
//     repaired: attached to the nearest resolvable ancestor, the deepest
//     preceding block at a shallower depth. Depth itself is never rewritten,
//     so the repair touches only the in-memory model and a repaired file
//     round-trips byte-stably.
//   - when no ancestor exists at all (the file opens indented), the block
//     stays parentless.
//
// HEADER blocks are exempt from violation reporting: their Depth is the
// heading level (1–6), not indentation, so a parentless top heading or a
// level jump (`#` straight to `###`) is idiomatic markdown rather than
// structural corruption. They still receive ParentID from the same walk so
// every reader sees one hierarchy.
//
// Every repair and every unresolvable non-HEADER block is returned as a
// HierarchyViolation so callers report the malformed structure instead of
// silently indexing it. ParseFileContent runs this pass on every parse and
// forwards the violations to the per-file warnings channel (repair on load,
// report on save). All readers of parentage — derived task counts, subtree
// extraction, the frontend — consume the ParentID this function produced;
// nothing recomputes the hierarchy.
func DeriveHierarchy(blocks []ParsedBlock) []HierarchyViolation {
	var violations []HierarchyViolation
	// stack[d] is the id of the most recent block at depth d, or "" for a
	// depth no block has occupied since the last shallower block.
	var stack []string
	for i := range blocks {
		b := &blocks[i]
		if b.ID == "" {
			continue
		}
		if b.Depth < 0 {
			// Defensive: parseLeadingIndent never yields negatives, but a
			// caller-constructed slice (SDK sub-editor splice) could; a
			// negative depth would index the stack out of range below.
			b.Depth = 0
		}
		b.ParentID = ""
		if b.Depth > 0 {
			parentID := ""
			parentDepth := -1
			for p := b.Depth - 1; p >= 0; p-- {
				if p < len(stack) && stack[p] != "" {
					parentID = stack[p]
					parentDepth = p
					break
				}
			}
			if parentID != "" {
				b.ParentID = parentID
				if parentDepth != b.Depth-1 && b.Type != BlockHeader {
					violations = append(violations, HierarchyViolation{
						BlockID: b.ID, Type: b.Type, LineNumber: b.LineNumber,
						Depth: b.Depth, ParentID: parentID, ParentDepth: parentDepth,
					})
				}
			} else if b.Type != BlockHeader {
				violations = append(violations, HierarchyViolation{
					BlockID: b.ID, Type: b.Type, LineNumber: b.LineNumber,
					Depth: b.Depth, ParentID: "", ParentDepth: -1,
				})
			}
		}
		for len(stack) <= b.Depth {
			stack = append(stack, "")
		}
		stack[b.Depth] = b.ID
		for d := b.Depth + 1; d < len(stack); d++ {
			stack[d] = ""
		}
	}
	return violations
}
