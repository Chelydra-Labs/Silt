package db

import (
	"strings"

	"silt/backend/parser"
)

// PageLoc is a distinct page location in the block index.
type PageLoc struct {
	Source   string
	Notebook string
	Section  string
	Page     string
}

// PageLinkRow is one row of the page_links reverse index.
type PageLinkRow struct {
	SourceNotebook string
	SourceSection  string
	SourcePage     string
	SourceBlockID  string
	TargetRaw      string
	Heading        string
	Alias          string
}

// ListDistinctPages returns every distinct (source, notebook, section, page)
// triple present in the blocks index. Used by wiki-link resolution (#545).
func (dm *DatabaseManager) ListDistinctPages() ([]PageLoc, error) {
	db, release, err := dm.handle()
	if err != nil {
		return nil, ErrDBClosed
	}
	defer release()
	rows, err := db.Query(`
		SELECT DISTINCT COALESCE(source, 'vault'), notebook, section, page
		FROM blocks
		ORDER BY notebook, section, page`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PageLoc
	for rows.Next() {
		var p PageLoc
		if err := rows.Scan(&p.Source, &p.Notebook, &p.Section, &p.Page); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ListPageLinksByTargetRaws returns reverse-index rows whose target_raw is in
// the given candidate list (used by rename/move rewrite).
func (dm *DatabaseManager) ListPageLinksByTargetRaws(targets []string) ([]PageLinkRow, error) {
	if len(targets) == 0 {
		return nil, nil
	}
	db, release, err := dm.handle()
	if err != nil {
		return nil, ErrDBClosed
	}
	defer release()
	placeholders := make([]string, len(targets))
	args := make([]any, len(targets))
	for i, t := range targets {
		placeholders[i] = "?"
		args[i] = t
	}
	q := `SELECT source_notebook, source_section, source_page, source_block_id,
	             target_raw, COALESCE(heading, ''), COALESCE(alias, '')
	      FROM page_links
	      WHERE target_raw IN (` + strings.Join(placeholders, ",") + `)`
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PageLinkRow
	for rows.Next() {
		var r PageLinkRow
		if err := rows.Scan(
			&r.SourceNotebook, &r.SourceSection, &r.SourcePage, &r.SourceBlockID,
			&r.TargetRaw, &r.Heading, &r.Alias,
		); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ResolvePageLink resolves a wiki-link target via shortest-unique-path rules
// (basename → section/page → notebook/section/page). Missing → Exists=false;
// multiple matches → Ambiguous=true with Candidates. No error for soft fails.
func (dm *DatabaseManager) ResolvePageLink(target string) (parser.PageReference, error) {
	ref := parser.PageReference{Target: target}
	pages, err := dm.ListDistinctPages()
	if err != nil {
		return ref, err
	}
	return ResolvePageLinkAgainst(target, pages), nil
}

// ResolvePageLinkAgainst is the pure resolution core (unit-testable without DB).
func ResolvePageLinkAgainst(target string, pages []PageLoc) parser.PageReference {
	ref := parser.PageReference{Target: target}
	norm := NormalizePageLinkTarget(target)
	if norm == "" {
		return ref
	}
	var matches []PageLoc
	for _, p := range pages {
		if PageMatchesTarget(p.Notebook, p.Section, p.Page, norm) {
			matches = append(matches, p)
		}
	}
	if len(matches) == 0 {
		return ref
	}
	if len(matches) > 1 {
		ref.Ambiguous = true
		for _, m := range matches {
			ref.Candidates = append(ref.Candidates, parser.PagePath{
				Source: m.Source, Notebook: m.Notebook, Section: m.Section, Page: m.Page,
			})
		}
		return ref
	}
	m := matches[0]
	ref.Exists = true
	ref.Source = m.Source
	ref.Notebook = m.Notebook
	ref.Section = m.Section
	ref.Page = m.Page
	ref.Shortest = ShortestUniquePath(m, pages)
	return ref
}

// NormalizePageLinkTarget trims and normalizes separators (Obsidian-compatible).
func NormalizePageLinkTarget(target string) string {
	t := strings.TrimSpace(target)
	t = strings.ReplaceAll(t, "\\", "/")
	// Collapse duplicate slashes; strip leading/trailing slashes.
	for strings.Contains(t, "//") {
		t = strings.ReplaceAll(t, "//", "/")
	}
	return strings.Trim(t, "/")
}

// PathVariants returns the path forms a page may be linked by, shortest first.
// Empty section → Notebook/Page (never Notebook//Page).
func PathVariants(notebook, section, page string) []string {
	if page == "" {
		return nil
	}
	out := []string{page}
	if section != "" {
		out = append(out, section+"/"+page)
		if notebook != "" {
			out = append(out, notebook+"/"+section+"/"+page)
		}
	} else if notebook != "" {
		out = append(out, notebook+"/"+page)
	}
	return out
}

// PageMatchesTarget reports whether a page is a valid resolution of target.
func PageMatchesTarget(notebook, section, page, target string) bool {
	norm := NormalizePageLinkTarget(target)
	if norm == "" || page == "" {
		return false
	}
	for _, v := range PathVariants(notebook, section, page) {
		if strings.EqualFold(v, norm) {
			return true
		}
	}
	// Multi-segment suffix: target "Active/Site" matches "Work/Projects/Active/Site"
	// when section is nested "Projects/Active".
	if section != "" && notebook != "" {
		full := notebook + "/" + section + "/" + page
		if strings.HasSuffix(strings.ToLower(full), "/"+strings.ToLower(norm)) {
			// Require segment boundary (HasSuffix already uses /+norm when norm has no leading /).
			return true
		}
	}
	return false
}

// ShortestUniquePath returns the shortest path form that uniquely identifies
// loc among pages. Falls back to the full path.
func ShortestUniquePath(loc PageLoc, pages []PageLoc) string {
	for _, v := range PathVariants(loc.Notebook, loc.Section, loc.Page) {
		n := 0
		for _, p := range pages {
			if PageMatchesTarget(p.Notebook, p.Section, p.Page, v) {
				n++
				if n > 1 {
					break
				}
			}
		}
		if n == 1 {
			return v
		}
	}
	if loc.Section != "" {
		return loc.Notebook + "/" + loc.Section + "/" + loc.Page
	}
	if loc.Notebook != "" {
		return loc.Notebook + "/" + loc.Page
	}
	return loc.Page
}

// MapTargetRaw rewrites a stored target_raw from an old page location to a new
// one, preserving the path "depth" the author used (basename vs section/page
// vs full path).
func MapTargetRaw(oldRaw, oldNB, oldSec, oldPage, newNB, newSec, newPage string) string {
	oldRaw = NormalizePageLinkTarget(oldRaw)
	oldVars := PathVariants(oldNB, oldSec, oldPage)
	newVars := PathVariants(newNB, newSec, newPage)
	for i, v := range oldVars {
		if v == oldRaw {
			if i < len(newVars) {
				return newVars[i]
			}
			// Depth mismatch (e.g. section cleared): use longest available.
			return newVars[len(newVars)-1]
		}
	}
	// Suffix form not in the fixed variant list (nested partial): swap page leaf.
	if strings.HasSuffix(oldRaw, "/"+oldPage) {
		prefix := strings.TrimSuffix(oldRaw, oldPage)
		// If the prefix embedded the old section path, rebuild from new location.
		if oldSec != "" && (oldRaw == oldSec+"/"+oldPage || strings.HasSuffix(oldRaw, "/"+oldSec+"/"+oldPage) || oldRaw == oldNB+"/"+oldSec+"/"+oldPage) {
			return MapTargetRaw(oldSec+"/"+oldPage, oldNB, oldSec, oldPage, newNB, newSec, newPage)
		}
		return prefix + newPage
	}
	if oldRaw == oldPage {
		return newPage
	}
	// Last resort: shortest new form.
	if len(newVars) > 0 {
		return newVars[0]
	}
	return newPage
}

// RewritePageLinksInContent replaces [[oldTarget…]] occurrences with
// [[newTarget…]], preserving #heading and |alias. Returns the new content and
// the number of replacements.
func RewritePageLinksInContent(content, oldTarget, newTarget string) (string, int) {
	if oldTarget == "" || newTarget == "" || oldTarget == newTarget {
		return content, 0
	}
	n := 0
	out := parser.PageLinkRegex.ReplaceAllStringFunc(content, func(match string) string {
		m := parser.PageLinkRegex.FindStringSubmatch(match)
		if m == nil || m[1] != oldTarget {
			return match
		}
		n++
		built := "[[" + newTarget
		if m[2] != "" {
			built += "#" + m[2]
		}
		if m[3] != "" {
			built += "|" + m[3]
		}
		built += "]]"
		return built
	})
	return out, n
}
