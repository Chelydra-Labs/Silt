package db

import (
	"database/sql"
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
	Source         string // 'vault' | 'linked:<id>' — the root the linking page belongs to
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
	return listDistinctPages(db)
}

// ListPagesByLeaf returns distinct locations whose leaf page name equals leaf.
func (dm *DatabaseManager) ListPagesByLeaf(leaf string) ([]PageLoc, error) {
	db, release, err := dm.handle()
	if err != nil {
		return nil, ErrDBClosed
	}
	defer release()
	return listPagesByLeaf(db, leaf)
}

// PageExistsExact reports whether the concrete path exists in the blocks index.
func (dm *DatabaseManager) PageExistsExact(source, notebook, section, page string) (bool, error) {
	db, release, err := dm.handle()
	if err != nil {
		return false, ErrDBClosed
	}
	defer release()
	return pageExistsExact(db, source, notebook, section, page)
}

// listDistinctPages is the no-handle-reentry internal helper. Callers that
// already hold a handle lease MUST call this instead of ListDistinctPages.
func listDistinctPages(db *sql.DB) ([]PageLoc, error) {
	listDistinctPagesCalls++
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

// listDistinctPagesCalls counts full-inventory scans (tests for #839).
var listDistinctPagesCalls int

// listPagesByLeaf returns distinct page locations whose leaf page name equals
// leaf (exact match, same case rules as stored paths / ResolvePageLinkAgainst).
// Used by unlinked ambiguity so large vaults do not load the full inventory.
func listPagesByLeaf(db *sql.DB, leaf string) ([]PageLoc, error) {
	leaf = strings.TrimSpace(leaf)
	if leaf == "" {
		return nil, nil
	}
	rows, err := db.Query(`
		SELECT DISTINCT COALESCE(source, 'vault'), notebook, section, page
		FROM blocks
		WHERE page = ?
		ORDER BY notebook, section, page`, leaf)
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

// pageExistsExact reports whether a concrete path is present in the blocks index.
func pageExistsExact(db *sql.DB, source, notebook, section, page string) (bool, error) {
	if source == "" {
		source = "vault"
	}
	var n int
	err := db.QueryRow(`
		SELECT 1 FROM blocks
		WHERE COALESCE(source, 'vault') = ? AND notebook = ? AND section = ? AND page = ?
		LIMIT 1`, source, notebook, section, page).Scan(&n)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// ListAllPageLinks returns every row in the page_links reverse index. Used by
// the rename rewrite pass to resolve-gate each target (ambiguous links are
// left untouched) rather than blindly matching path variants.
func (dm *DatabaseManager) ListAllPageLinks() ([]PageLinkRow, error) {
	db, release, err := dm.handle()
	if err != nil {
		return nil, ErrDBClosed
	}
	defer release()
	rows, err := db.Query(`SELECT COALESCE(source,'vault'), source_notebook, source_section, source_page, source_block_id,
	             target_raw, COALESCE(heading, ''), COALESCE(alias, '')
	      FROM page_links`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PageLinkRow
	for rows.Next() {
		var r PageLinkRow
		if err := rows.Scan(
			&r.Source, &r.SourceNotebook, &r.SourceSection, &r.SourcePage, &r.SourceBlockID,
			&r.TargetRaw, &r.Heading, &r.Alias,
		); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// pageLinkTargetINBatchSize is the max number of lower(target_raw) bind args
// per SELECT. SQLite default max variable number is 999; stay under with
// headroom. ListPageLinksByTargetRaws chunks larger candidate sets.
const pageLinkTargetINBatchSize = 900

// ListPageLinksByTargetRaws returns reverse-index rows whose target_raw matches
// any candidate (case-insensitive). Used by rename/move inbound collect and
// rewrite so large vaults avoid loading the full page_links table. Matching is
// on lower(target_raw) so [[MyPage]] and [[mypage]] both hit; resolution still
// gates ambiguous basenames in Go. Prefer LinkTargetRawCandidates for the
// candidate list (path variants + segment suffixes).
//
// Large candidate lists are queried in batches of pageLinkTargetINBatchSize so
// notebook/section renames with hundreds of pages never drop targets.
//
// Resolved target_* columns are intentionally unused: indexing stores them
// NULL; authority is target_raw + page inventory resolution.
func (dm *DatabaseManager) ListPageLinksByTargetRaws(targets []string) ([]PageLinkRow, error) {
	if len(targets) == 0 {
		return nil, nil
	}
	db, release, err := dm.handle()
	if err != nil {
		return nil, ErrDBClosed
	}
	defer release()
	return listPageLinksByTargetRaws(db, targets)
}

// listPageLinksByTargetRaws is the no-handle-reentry internal helper. Callers
// that already hold a handle lease MUST call this instead of
// ListPageLinksByTargetRaws.
func listPageLinksByTargetRaws(db *sql.DB, targets []string) ([]PageLinkRow, error) {
	// Dedupe lowercased candidates for the IN list.
	seen := make(map[string]bool, len(targets))
	args := make([]any, 0, len(targets))
	for _, t := range targets {
		n := strings.ToLower(strings.TrimSpace(t))
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		args = append(args, n)
	}
	if len(args) == 0 {
		return nil, nil
	}

	var out []PageLinkRow
	// Dedupe rows across batches (same link can match multiple candidates).
	rowSeen := make(map[string]bool)
	for start := 0; start < len(args); start += pageLinkTargetINBatchSize {
		end := start + pageLinkTargetINBatchSize
		if end > len(args) {
			end = len(args)
		}
		batch := args[start:end]
		placeholders := make([]string, len(batch))
		for i := range batch {
			placeholders[i] = "?"
		}
		// lower(target_raw) uses idx_page_links_raw_lower when present.
		q := `SELECT COALESCE(source,'vault'), source_notebook, source_section, source_page, source_block_id,
		             target_raw, COALESCE(heading, ''), COALESCE(alias, '')
		      FROM page_links
		      WHERE lower(target_raw) IN (` + strings.Join(placeholders, ",") + `)`
		rows, err := db.Query(q, batch...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var r PageLinkRow
			if err := rows.Scan(
				&r.Source, &r.SourceNotebook, &r.SourceSection, &r.SourcePage, &r.SourceBlockID,
				&r.TargetRaw, &r.Heading, &r.Alias,
			); err != nil {
				rows.Close()
				return nil, err
			}
			key := r.Source + "\x00" + r.SourceNotebook + "\x00" + r.SourceSection + "\x00" + r.SourcePage + "\x00" +
				r.SourceBlockID + "\x00" + r.TargetRaw
			if rowSeen[key] {
				continue
			}
			rowSeen[key] = true
			out = append(out, r)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// LinkTargetSpec describes a page location for candidate generation, including
// the source root so source-qualified target forms are emitted for linked
// notebooks.
type LinkTargetSpec struct {
	Source   string // 'vault' | 'linked:<id>'
	Notebook string
	Section  string
	Page     string
}

// LinkTargetRawCandidates returns every target_raw form that may uniquely
// resolve to the given page locations: PathVariants plus intermediate
// path-segment suffixes not already covered by PathVariants (so [[Active/Site]]
// still matches Work/Projects/Active/Site). For non-vault sources, also emits
// the source-qualified form (e.g. "linked:abc/Work/Site") since ShortestUniquePath
// emits that form for cross-source basename collisions. Candidates are deduped
// globally across all targets. Callers pass the full list to
// ListPageLinksByTargetRaws, which batches the SQL IN clause — do not truncate here.
func LinkTargetRawCandidates(targets []LinkTargetSpec) []string {
	seen := make(map[string]bool)
	var primary, suffixes []string
	add := func(dst *[]string, s string) {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			return
		}
		seen[s] = true
		*dst = append(*dst, s)
	}
	for _, t := range targets {
		if t.Page == "" {
			continue
		}
		variants := PathVariants(t.Notebook, t.Section, t.Page)
		variantSet := make(map[string]bool, len(variants))
		for _, v := range variants {
			variantSet[strings.ToLower(v)] = true
			add(&primary, v)
		}
		// Source-qualified candidates for non-vault sources (linked notebooks).
		// ShortestUniquePath emits "linked:<id>/Notebook/Section/Page" and its
		// shorter variants when basename collides across sources.
		if t.Source != "" && t.Source != "vault" {
			// Emit all qualified suffix forms: source/Page, source/Section/Page,
			// source/Notebook/Section/Page.
			qParts := []string{t.Source}
			if t.Notebook != "" {
				qParts = append(qParts, t.Notebook)
			}
			if t.Section != "" {
				qParts = append(qParts, strings.Split(t.Section, "/")...)
			}
			qParts = append(qParts, t.Page)
			for i := 1; i < len(qParts); i++ {
				qSuf := strings.Join(qParts[i:], "/")
				qFull := t.Source + "/" + qSuf
				add(&primary, qFull)
			}
		}
		// Intermediate suffixes only (skip forms already in PathVariants).
		var parts []string
		if t.Notebook != "" {
			parts = append(parts, t.Notebook)
		}
		if t.Section != "" {
			parts = append(parts, strings.Split(t.Section, "/")...)
		}
		parts = append(parts, t.Page)
		// i=0 is the full path (already a PathVariant when notebook set);
		// i=len-1 is the bare page (already a PathVariant). Middle values
		// cover PageMatchesTarget's HasSuffix rule.
		for i := 1; i < len(parts)-1; i++ {
			suf := strings.Join(parts[i:], "/")
			if variantSet[strings.ToLower(suf)] {
				continue
			}
			add(&suffixes, suf)
		}
	}
	return append(primary, suffixes...)
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

// ParseQualifiedTarget separates a source-qualified target like
// "linked:abc/Notebook/Section/Page" into (source, path) and a boolean
// indicating whether the target is source-qualified. Only "linked:" is a
// source qualifier; "vault/..." is a valid legacy notebook path prefix and
// is never treated as source-qualified (under the globally unique
// notebook-name invariant). Unqualified targets return ("", target, false).
func ParseQualifiedTarget(target string) (source, path string, qualified bool) {
	norm := NormalizePageLinkTarget(target)
	if norm == "" {
		return "", "", false
	}
	if idx := strings.Index(norm, "/"); idx > 0 {
		prefix := norm[:idx]
		if strings.HasPrefix(prefix, "linked:") {
			return prefix, norm[idx+1:], true
		}
	}
	return "", norm, false
}

// QualifiedPath returns a source-qualified path for a linked page location:
// "linked:<id>/Notebook/Section/Page". Vault pages return the unqualified
// path (no "vault/" prefix) since vault notebook names are globally unique
// and never need source disambiguation.
func QualifiedPath(loc PageLoc) string {
	if strings.HasPrefix(loc.Source, "linked:") {
		if loc.Section != "" {
			return loc.Source + "/" + loc.Notebook + "/" + loc.Section + "/" + loc.Page
		}
		if loc.Notebook != "" {
			return loc.Source + "/" + loc.Notebook + "/" + loc.Page
		}
		return loc.Source + "/" + loc.Page
	}
	if loc.Section != "" {
		return loc.Notebook + "/" + loc.Section + "/" + loc.Page
	}
	if loc.Notebook != "" {
		return loc.Notebook + "/" + loc.Page
	}
	return loc.Page
}

// ResolvePageLinkAgainst is the pure resolution core (unit-testable without DB).
// Source-qualified targets (e.g. "linked:abc/Work/Site") resolve only against
// pages in that source. Unqualified targets resolve across all sources.
func ResolvePageLinkAgainst(target string, pages []PageLoc) parser.PageReference {
	ref := parser.PageReference{Target: target}

	source, path, qualified := ParseQualifiedTarget(target)
	if path == "" {
		return ref
	}
	if qualified {
		// Source-qualified: match only pages in the given source.
		return resolveScoped(source, path, pages)
	}
	return resolveScoped("", path, pages)
}

// resolveScoped resolves a (possibly empty) source-scoped path against pages.
// Empty source means match across all sources; non-empty means match only
// pages in that source.
func resolveScoped(source, path string, pages []PageLoc) parser.PageReference {
	ref := parser.PageReference{Target: path}
	norm := NormalizePageLinkTarget(path)
	if norm == "" {
		return ref
	}

	var matches []PageLoc
	for _, p := range pages {
		if source != "" && p.Source != source {
			continue
		}
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
// loc among pages. Falls back to the full path. When a linked page's basename
// collides across sources and all unqualified forms are ambiguous, the
// source-qualified form "linked:<id>/Notebook/Section/Page" is emitted.
// Vault pages never receive a "vault/" prefix (under the globally unique
// notebook-name invariant, vault paths are always unambiguous).
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
	// All unqualified forms are ambiguous. Linked pages use their source
	// prefix for disambiguation; vault pages fall back to the full
	// unqualified path (which is unique under the invariant).
	return sourceFullPath(loc)
}

// sourceFullPath returns "source/Notebook/Section/Page" for linked sources,
// or the unqualified "Notebook/Section/Page" for vault sources.
func sourceFullPath(loc PageLoc) string {
	if strings.HasPrefix(loc.Source, "linked:") {
		if loc.Section != "" {
			return loc.Source + "/" + loc.Notebook + "/" + loc.Section + "/" + loc.Page
		}
		if loc.Notebook != "" {
			return loc.Source + "/" + loc.Notebook + "/" + loc.Page
		}
		return loc.Source + "/" + loc.Page
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
// vs full path). Case-insensitive to match resolution semantics. Source-qualified
// targets (e.g. "linked:abc/Work/Site") have their qualifier stripped before
// matching, then the qualifier is re-applied to the new form.
func MapTargetRaw(oldRaw, oldNB, oldSec, oldPage, newNB, newSec, newPage string) string {
	oldRawNorm := NormalizePageLinkTarget(oldRaw)

	// Handle source-qualified forms: strip qualifier, map the path portion,
	// re-apply the qualifier. The qualifier (source prefix) is preserved across
	// renames since it identifies the source root, not the page location.
	srcPrefix := ""
	stripped := oldRawNorm
	if _, path, q := ParseQualifiedTarget(oldRaw); q {
		srcPrefix = oldRawNorm[:len(oldRawNorm)-len(path)]
		stripped = path
	}
	if srcPrefix != "" {
		// Map the unqualified path portion, then re-qualify.
		newUnqual := MapTargetRaw(stripped, oldNB, oldSec, oldPage, newNB, newSec, newPage)
		return srcPrefix + newUnqual
	}

	oldVars := PathVariants(oldNB, oldSec, oldPage)
	newVars := PathVariants(newNB, newSec, newPage)
	for i, v := range oldVars {
		if strings.EqualFold(v, oldRawNorm) {
			if i < len(newVars) {
				return newVars[i]
			}
			return newVars[len(newVars)-1]
		}
	}
	// Suffix form not in the fixed variant list (nested partial): swap page
	// leaf, preserving the prefix path the author typed.
	if strings.HasSuffix(strings.ToLower(oldRawNorm), strings.ToLower("/"+oldPage)) {
		prefix := oldRawNorm[:len(oldRawNorm)-len(oldPage)]
		if oldSec != "" && (strings.EqualFold(oldRawNorm, oldSec+"/"+oldPage) ||
			strings.HasSuffix(strings.ToLower(oldRawNorm), strings.ToLower("/"+oldSec+"/"+oldPage)) ||
			strings.EqualFold(oldRawNorm, oldNB+"/"+oldSec+"/"+oldPage)) {
			return MapTargetRaw(oldSec+"/"+oldPage, oldNB, oldSec, oldPage, newNB, newSec, newPage)
		}
		return prefix + newPage
	}
	if strings.EqualFold(oldRawNorm, oldPage) {
		return newPage
	}
	if len(newVars) > 0 {
		return newVars[0]
	}
	return newPage
}

// RewritePageLinksInContent replaces [[oldTarget…]] occurrences with
// [[newTarget…]], preserving #heading and |alias. Case-insensitive target
// match (consistent with resolution). Fenced code regions (``` … ```) are
// skipped so literal [[…]] in code samples is never corrupted. Returns the
// new content and the number of replacements.
func RewritePageLinksInContent(content, oldTarget, newTarget string) (string, int) {
	if oldTarget == "" || newTarget == "" || strings.EqualFold(oldTarget, newTarget) {
		return content, 0
	}
	targetLower := strings.ToLower(oldTarget)
	n := 0
	lines := strings.Split(content, "\n")
	inFence := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		lines[i] = parser.PageLinkRegex.ReplaceAllStringFunc(line, func(match string) string {
			m := parser.PageLinkRegex.FindStringSubmatch(match)
			if m == nil || strings.ToLower(m[1]) != targetLower {
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
	}
	if n == 0 {
		return content, 0
	}
	return strings.Join(lines, "\n"), n
}
