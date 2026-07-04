// Package dependencies handles the task-dependency graph carried by the
// [blocked_by:: ((uuid))] Dataview token on task lines (#301).
//
// A task may declare zero or more prerequisites — other task blocks it is
// "blocked by" — using the standard block-reference syntax inside an inline
// metadata token:
//
//   - [ ] Ship feature [blocked_by:: ((a)) ((b))]
//
// The token's value is one or more space-separated ((uuid)) references. This
// package owns three concerns:
//
//   - ExtractRefs / FormatRefs — the lossless string ↔ []string mapping for
//     the token value, reusing parser.BlockRefRegex so the UUID grammar stays
//     in one place.
//   - DetectsCycle / WouldCreateCycle — a directed-graph cycle check so the
//     setter can refuse to add an edge that would close a loop (A→B→A). The
//     graph is the existing dependency edges plus the proposed addition.
//
// The package is pure (no I/O, no app state) so it is trivially unit-testable
// and can be called from the IPC setter without introducing a new dependency
// surface — the same rationale as backend/recurrence (#296).
package dependencies

import (
	"fmt"
	"regexp"
	"strings"
)

// uuidRefRegex matches a ((uuid)) block reference, mirroring
// parser.BlockRefRegex. The dependency package is intentionally free of a
// parser import (the parser imports this package for ExtractRefs/
// FormatRefs/WouldCreateCycle, so a reverse import would be a cycle). The
// UUID grammar is the stable RFC 4122 v4 form and is unlikely to drift; if
// it ever does, parser.BlockRefRegex is the canonical source.
var uuidRefRegex = regexp.MustCompile(`\(\(([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})\)\)`)

// ExtractRefs pulls every ((uuid)) block reference out of a [blocked_by::]
// token value, returning the bare UUIDs in the order they appear. Non-ref
// text (stray prose, malformed UUIDs) is silently ignored — the token is
// permissive on read so a hand-edited file doesn't fail to index. Duplicate
// UUIDs are de-duplicated, preserving first-seen order.
func ExtractRefs(value string) []string {
	matches := uuidRefRegex.FindAllStringSubmatch(value, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(matches))
	refs := make([]string, 0, len(matches))
	for _, m := range matches {
		id := m[1]
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		refs = append(refs, id)
	}
	if len(refs) == 0 {
		return nil
	}
	return refs
}

// FormatRefs is the inverse of ExtractRefs: it renders a UUID list as the
// canonical token value, one ((uuid)) per ref separated by single spaces.
// The empty list renders as the empty string so an unset dependency clears
// the token entirely on render.
func FormatRefs(refs []string) string {
	if len(refs) == 0 {
		return ""
	}
	parts := make([]string, 0, len(refs))
	for _, r := range refs {
		parts = append(parts, fmt.Sprintf("((%s))", r))
	}
	return strings.Join(parts, " ")
}

// WouldCreateCycle reports whether adding the edge from→to to the given edge
// set would introduce a cycle. Edges map a task UUID to the UUIDs it is
// blocked by (i.e. from→to means "from is blocked by to"). Adding from→to
// closes a loop exactly when `to` can already reach `from` along the existing
// edges — so this is a plain reachability query from `to` for `from`.
//
// Self-edges (from == to) are cycles by definition and are rejected without
// consulting the edge set. Runs in O(V+E) over the reachable subgraph. The
// setter keeps the persisted graph acyclic, so a pre-existing cycle (only
// possible via a hand-edited file) doesn't reach this check in production;
// warnOnDependencyCycle surfaces those separately at index time.
func WouldCreateCycle(edges map[string][]string, from, to string) bool {
	if from == to {
		return true
	}
	// Adding from→to creates a cycle iff `to` can already reach `from`.
	// A pre-existing cycle elsewhere in the reachable subgraph is irrelevant
	// to whether THIS edge closes a loop, so this is reachability — not
	// general cycle detection.
	visited := make(map[string]bool)
	var dfs func(node string) bool
	dfs = func(node string) bool {
		if node == from {
			return true
		}
		if visited[node] {
			return false
		}
		visited[node] = true
		for _, next := range edges[node] {
			if dfs(next) {
				return true
			}
		}
		return false
	}
	return dfs(to)
}

// DetectsCycle reports whether the given edge set contains any cycle. It is
// the whole-graph analogue of WouldCreateCycle, used to assert invariants on
// a freshly indexed graph (defensive — the setter prevents cycles, but a
// hand-edited or externally synced file could still introduce one).
func DetectsCycle(edges map[string][]string) bool {
	const (
		white = 0 // unvisited
		gray  = 1 // on the current DFS path
		black = 2 // fully explored
	)
	color := make(map[string]int)
	var visit func(node string) bool
	visit = func(node string) bool {
		color[node] = gray
		for _, next := range edges[node] {
			switch color[next] {
			case white:
				if visit(next) {
					return true
				}
			case gray:
				return true // back edge → cycle
			}
		}
		color[node] = black
		return false
	}
	for node := range edges {
		if color[node] == white {
			if visit(node) {
				return true
			}
		}
	}
	return false
}
