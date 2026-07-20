package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"silt/backend/config"
	"silt/backend/db"
	"silt/backend/parser"
	"sort"
	"strings"
)

// resolveNotebookDir returns the content directory for a notebook under the
// given source (#100): the folder whose direct children are the notebook's
// sections and section-less pages. For an in-vault notebook ('vault') that is
// <vaultPath>/<notebookName>; for a linked notebook ('linked:<id>') it is the
// linked root itself (sections/pages live directly under the external root).
// The caller MUST still guard any path built from this dir with
// isPathWithinRoot. Returns an error if the vault is not loaded or a linked
// source references an unregistered id.
func (a *App) resolveNotebookDir(notebookName, source string) (string, error) {
	if a.vaultPath == "" {
		return "", fmt.Errorf("vault not loaded")
	}
	if source == "" || source == config.LinkedNotebooksVaultSource {
		return filepath.Join(a.vaultPath, notebookName), nil
	}
	if strings.HasPrefix(source, "linked:") {
		id := strings.TrimPrefix(source, "linked:")
		// F3: check quarantine + fingerprint under RLock; the fingerprint
		// comparison is fast (a stat + struct comparison). A mismatch
		// quarantines the link so every downstream op fails closed until the
		// user re-links.
		a.configMu.RLock()
		if a.quarantinedLinks != nil {
			if _, q := a.quarantinedLinks[id]; q {
				a.configMu.RUnlock()
				return "", fmt.Errorf("linked notebook %q is quarantined (root moved or tampered); re-link it via Settings → Linked notebooks", id)
			}
		}
		var ln config.LinkedNotebook
		found := false
		for _, entry := range a.cfg.LinkedNotebooks {
			if entry.ID == id {
				ln = entry
				found = true
				break
			}
		}
		a.configMu.RUnlock()
		if !found {
			return "", fmt.Errorf("linked notebook %q is not registered", id)
		}
		// F3: re-verify the root fingerprint. A synced edit to config.yaml's
		// root_path redirects the link to an attacker-chosen folder; the
		// fingerprint comparison catches this on the next access and
		// quarantines the link.
		if ln.RootFingerprint != "" {
			currentFP, fpErr := config.ComputeRootFingerprint(ln.RootPath)
			if fpErr != nil {
				return "", fmt.Errorf("linked notebook %q root is inaccessible: %w", id, fpErr)
			}
			if currentFP != ln.RootFingerprint {
				a.quarantineLink(id, "fingerprint_mismatch")
				return "", fmt.Errorf("linked notebook %q root fingerprint mismatch (root moved or tampered); re-link it via Settings → Linked notebooks", id)
			}
		}
		return ln.RootPath, nil
	}
	return "", fmt.Errorf("unknown notebook source %q", source)
}

// nspKey is the source-aware (source, notebook, section, page) lookup key for
// the per-page block count map used by ListNavigation. Source leads so a
// linked notebook sharing a display name with a vault notebook gets its own
// counts (#100).
type nspKey struct{ src, n, s, p string }

// resolveSourceByName maps a notebook display name to its index source
// ("vault" or "linked:<id>"). It acquires configMu in read mode for the
// standalone callers below. Notebook display names are globally unique
// (LinkNotebook rejects collisions), so the name unambiguously resolves the
// source (#100).
func (a *App) resolveSourceByName(notebookName string) string {
	a.configMu.RLock()
	defer a.configMu.RUnlock()
	return a.resolveSourceByNameLocked(notebookName)
}

// resolveSourceByNameLocked is the lock-free inner form. The caller MUST hold
// configMu (read or write). Needed so GetPluginSettingsForNotebook — which
// holds configMu in WRITE mode (linkedConfigLocked mutates the cache map) —
// can resolve the source without self-deadlocking on a re-entrant RLock
// (sync.RWMutex blocks RLock while a writer holds the lock).
func (a *App) resolveSourceByNameLocked(notebookName string) string {
	for _, ln := range a.cfg.LinkedNotebooks {
		if ln.DisplayName == notebookName {
			return ln.Source()
		}
	}
	return config.LinkedNotebooksVaultSource
}

// ListNavigation returns the Notebook > Section > Page tree for the sidebar.
//
// The directory structure on disk is the single source of truth. Each
// directory is classified by what it DIRECTLY contains:
//   - A `.md` file directly under a folder is a PAGE belonging to that folder's
//     section (a page belongs to the folder it's in; the folder's own path
//     is the section path, multi-segment joined with `/`).
//   - A sub-directory of a folder is a nested SECTION. We recurse into it to
//     collect its own pages + its own nested sections. Empty sections are
//     preserved so a freshly-created section appears in the sidebar (#88).
//   - A `.md` file directly under a Notebook's root belongs to the section-less
//     group (Name = "").
//
// Block counts are merged from the index for per-page badges. The returned
// tree is a true tree: each section may carry `Children []NavigationSection`
// for arbitrarily-deep nesting.
func (a *App) ListNavigation() (parser.NavigationTree, error) {
	tree, err := func() (parser.NavigationTree, error) {
		a.vaultMu.RLock()
		defer a.vaultMu.RUnlock()
		if a.vaultPath == "" {
			return parser.NavigationTree{}, fmt.Errorf("vault not loaded")
		}

		a.wg.Add(1)
		defer a.wg.Done()

		// 1. Block counts per (source, notebook, section, page) from the index.
		// Source is part of the key so a linked notebook sharing a display name
		// with a vault notebook gets its own counts (#100). Lease-aware package
		// API — no raw SQLDB() across vault teardown.
		counts := map[nspKey]int{}
		if a.db != nil {
			a.coordinator.WithDBRead(func() {
				rows, err := a.db.CountBlocksGroupedByPage()
				if err != nil {
					// Match prior soft behavior: empty counts on query failure
					// (including ErrDBClosed during vault switch).
					return
				}
				for _, row := range rows {
					counts[nspKey{row.Source, row.Notebook, row.Section, row.Page}] = row.Count
				}
			})
		}

		tree := parser.NavigationTree{Notebooks: []parser.NavigationNotebook{}}
		nbEntries, err := os.ReadDir(a.vaultPath)
		if err != nil {
			return tree, fmt.Errorf("failed to read vault: %w", err)
		}

		for _, nbE := range nbEntries {
			nbName := nbE.Name()
			if !nbE.IsDir() || strings.HasPrefix(nbName, ".") {
				continue
			}
			nbPath := filepath.Join(a.vaultPath, nbName)
			rootPages, childSections := a.walkSections(nbPath, nbName, "", config.LinkedNotebooksVaultSource, counts)
			var sections []parser.NavigationSection
			// Direct .md files at the notebook root form the section-less
			// group (Name = ""), surfaced first in the sidebar.
			if len(rootPages) > 0 {
				sections = append(sections, parser.NavigationSection{
					Name:  "",
					Pages: rootPages,
				})
			}
			sections = append(sections, childSections...)
			tree.Notebooks = append(tree.Notebooks, parser.NavigationNotebook{
				Name:     nbName,
				Sections: sections,
				Source:   "vault",
			})
		}

		// 2. Linked notebooks prefer their live filesystem tree. The index is only
		// used while a trusted root is unavailable, preserving the last known tree
		// during offline sync outages.
		a.configMu.RLock()
		links := append([]config.LinkedNotebook(nil), a.cfg.LinkedNotebooks...)
		a.configMu.RUnlock()
		for _, ln := range links {
			src := ln.Source()
			var sections []parser.NavigationSection
			disconnected := false
			if linkedRoot, resolveErr := a.resolveNotebookDir(ln.DisplayName, src); resolveErr == nil {
				if info, statErr := os.Stat(linkedRoot); statErr == nil && info.IsDir() {
					rootPages, childSections := a.walkSections(linkedRoot, ln.DisplayName, "", src, counts)
					if len(rootPages) > 0 {
						sections = append(sections, parser.NavigationSection{Name: "", Pages: rootPages})
					}
					sections = append(sections, childSections...)
				} else {
					disconnected = true
				}
			} else {
				disconnected = true
			}
			if disconnected {
				sections = reconstructIndexedSections(counts, src, ln.DisplayName)
			}

			tree.Notebooks = append(tree.Notebooks, parser.NavigationNotebook{
				Name:         ln.DisplayName,
				Source:       src,
				RootPath:     ln.RootPath,
				Disconnected: disconnected,
				Sections:     sections,
			})
		}

		// Mix vault + linked notebooks alphabetically by name for a unified tree.
		sort.Slice(tree.Notebooks, func(i, j int) bool {
			return tree.Notebooks[i].Name < tree.Notebooks[j].Name
		})
		tree = normalizeNavTree(tree)
		return tree, nil
	}()
	if err != nil {
		return tree, err
	}
	if err := a.reconcileNavigationAgainstTree(tree); err != nil {
		log.Printf("ListNavigation: preference reconciliation failed: %v", err)
	}
	return tree, nil
}

// normalizeNavTree guarantees no nil slices cross the Wails IPC boundary. A Go
// nil slice serializes to JSON `null`, but the generated TS constructor passes
// `null` through unchanged — the frontend's `.length` reads then crash with
// "Cannot read properties of null", which tears down the reactive update and
// leaves the sidebar blank even though the data is correct (#140). Every
// Sections / Pages / Children slice is normalized to a non-nil empty array.
func normalizeNavTree(tree parser.NavigationTree) parser.NavigationTree {
	if tree.Notebooks == nil {
		tree.Notebooks = []parser.NavigationNotebook{}
	}
	for i := range tree.Notebooks {
		if tree.Notebooks[i].Sections == nil {
			tree.Notebooks[i].Sections = []parser.NavigationSection{}
		}
		for j := range tree.Notebooks[i].Sections {
			tree.Notebooks[i].Sections[j] = normalizeNavSection(tree.Notebooks[i].Sections[j])
		}
	}
	return tree
}

func normalizeNavSection(s parser.NavigationSection) parser.NavigationSection {
	if s.Pages == nil {
		s.Pages = []parser.NavigationPage{}
	}
	if s.Children == nil {
		s.Children = []parser.NavigationSection{}
	}
	for i := range s.Children {
		s.Children[i] = normalizeNavSection(s.Children[i])
	}
	return s
}

// walkSections reads `dirPath` once and returns:
//   - `pages`: the direct .md files in this directory (the "own pages").
//   - `sections`: one NavigationSection per sub-directory, each carrying its
//     own pages and recursively-built children.
//
// `parentSectionID` is the multi-segment section id of `dirPath` itself
// (empty at the notebook root). The caller (ListNavigation) is responsible
// for turning the notebook-root `pages` into the section-less group.
// Sections with no pages and no children are still emitted so freshly-
// created sections appear in the sidebar immediately.
func (a *App) walkSections(
	dirPath, nbName, parentSectionID, source string,
	counts map[nspKey]int,
) ([]parser.NavigationPage, []parser.NavigationSection) {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return nil, nil
	}

	var pages []parser.NavigationPage
	var subDirs []string

	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		// Skip the attachments/ directory in the sidebar navigator (#101) —
		// it holds binary assets, not pages/sections.
		if e.IsDir() && strings.EqualFold(name, "attachments") {
			continue
		}
		if e.IsDir() {
			subDirs = append(subDirs, name)
			continue
		}
		if !strings.EqualFold(filepath.Ext(name), ".md") {
			continue
		}
		pageName := strings.TrimSuffix(name, filepath.Ext(name))
		pages = append(pages, parser.NavigationPage{
			Name:  pageName,
			Count: counts[nspKey{source, nbName, parentSectionID, pageName}],
		})
	}
	sortNavPages(pages)
	sortStrings(subDirs)

	sections := []parser.NavigationSection{}

	for _, sd := range subDirs {
		var childID string
		if parentSectionID == "" {
			childID = sd
		} else {
			childID = parentSectionID + "/" + sd
		}
		childPath := filepath.Join(dirPath, sd)
		// Single read: the recursive call returns both the child's own
		// pages and its nested sections, so we never re-read childPath.
		childPages, childSections := a.walkSections(childPath, nbName, childID, source, counts)
		// Preserve the child even when empty so a freshly-created
		// section shows up in the sidebar.
		sections = append(sections, parser.NavigationSection{
			Name:     sd,
			Path:     childID,
			Pages:    childPages,
			Children: childSections,
		})
	}

	return pages, sections
}

func reconstructIndexedSections(counts map[nspKey]int, source, notebook string) []parser.NavigationSection {
	type node struct {
		section  parser.NavigationSection
		children map[string]*node
	}
	root := &node{children: map[string]*node{}}
	var rootPages []parser.NavigationPage
	for key, count := range counts {
		if key.src != source || key.n != notebook {
			continue
		}
		if key.s == "" {
			rootPages = append(rootPages, parser.NavigationPage{Name: key.p, Count: count})
			continue
		}
		current := root
		path := ""
		for _, part := range strings.Split(key.s, "/") {
			if path == "" {
				path = part
			} else {
				path += "/" + part
			}
			child := current.children[part]
			if child == nil {
				child = &node{section: parser.NavigationSection{Name: part, Path: path, Pages: []parser.NavigationPage{}, Children: []parser.NavigationSection{}}, children: map[string]*node{}}
				current.children[part] = child
			}
			current = child
		}
		current.section.Pages = append(current.section.Pages, parser.NavigationPage{Name: key.p, Count: count})
	}
	var build func(*node) []parser.NavigationSection
	build = func(parent *node) []parser.NavigationSection {
		keys := make([]string, 0, len(parent.children))
		for key := range parent.children {
			keys = append(keys, key)
		}
		sortStrings(keys)
		out := make([]parser.NavigationSection, 0, len(keys))
		for _, key := range keys {
			child := parent.children[key]
			sortNavPages(child.section.Pages)
			child.section.Children = build(child)
			out = append(out, child.section)
		}
		return out
	}
	sections := make([]parser.NavigationSection, 0)
	if len(rootPages) > 0 {
		sortNavPages(rootPages)
		sections = append(sections, parser.NavigationSection{Name: "", Pages: rootPages})
	}
	return append(sections, build(root)...)
}

func sortStrings(s []string) {
	sort.Strings(s)
}

func sortNavPages(p []parser.NavigationPage) {
	sort.Slice(p, func(i, j int) bool {
		return p[i].Name < p[j].Name
	})
}

// QueryTagHierarchy returns the hierarchical tag tree for the Tags Explorer.
func (a *App) QueryTagHierarchy() ([]parser.TagNode, error) {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.db == nil {
		return nil, fmt.Errorf("vault database not loaded")
	}
	a.wg.Add(1)
	defer a.wg.Done()
	var res []parser.TagNode
	var err error
	a.coordinator.WithDBRead(func() { res, err = a.db.QueryTagHierarchy() })
	return res, err
}

// QueryBlocksByTag returns blocks tagged at or beneath tagPath (prefix match).
func (a *App) QueryBlocksByTag(tagPath string) ([]parser.TaskResult, error) {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.db == nil {
		return nil, fmt.Errorf("vault database not loaded")
	}
	tagPath = strings.TrimSpace(strings.TrimPrefix(tagPath, "#"))
	if tagPath == "" || !config.IsValidTagPath(tagPath) {
		return []parser.TaskResult{}, nil
	}
	a.wg.Add(1)
	defer a.wg.Done()
	var res []parser.TaskResult
	var err error
	a.coordinator.WithDBRead(func() { res, err = a.db.QueryBlocksByTag(tagPath) })
	return res, err
}

// SearchBlocks fuzzy searches blocks and headings matching the query. Returns
// the first page (offset 0, limit 50) of FTS5-ranked results for backwards
// compatibility with the original binding; the Svelte search modal that needs
// pagination/snippets calls SearchBlocksPaged instead.
func (a *App) SearchBlocks(query string) ([]parser.TaskResult, error) {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.db == nil {
		return nil, fmt.Errorf("vault database not loaded")
	}

	a.wg.Add(1)
	defer a.wg.Done()

	var res []parser.TaskResult
	var err error
	a.coordinator.WithDBRead(func() {
		res, err = a.db.SearchBlocks(query)
	})

	return res, err
}

// SearchBlocksPaged runs the FTS5 search and returns a ranked, paginated
// envelope with highlighted snippets, the total match count, and a HasMore
// flag. offset/limit control the page (defaults applied by the caller).
// SearchBlocksPaged runs the FTS5 global search with optional filters (#186
// global enhancements: notebook/section/tag/type/sort/scope). The frontend
// SearchModal drives this; an empty SearchFilters reproduces the original
// unfiltered behavior (whole vault + linked notebooks, bm25 relevance).
func (a *App) SearchBlocksPaged(query string, offset, limit int, filters db.SearchFilters) (parser.SearchResult, error) {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.db == nil {
		return parser.SearchResult{}, fmt.Errorf("vault database not loaded")
	}

	a.wg.Add(1)
	defer a.wg.Done()

	var res parser.SearchResult
	var err error
	a.coordinator.WithDBRead(func() {
		res, err = a.db.SearchBlocksPaged(query, offset, limit, filters)
	})

	return res, err
}

// searchPagesMinLen is the minimum number of non-space characters a query
// must contain before SearchPages will enumerate the page catalog. Shorter
// queries (including empty) return immediately with no results so that
// rapid typeahead keystrokes don't thrash the DB.
const searchPagesMinLen = 2

// searchPagesMax is the hard cap on results SearchPages returns. The caller
// may request fewer but never more; the server always ranks and truncates
// at this bound so the typeahead picker stays responsive.
const searchPagesMax = 50

// SearchPages returns pages whose notebook/section/page path matches the
// query using a ranked contract:
//
//	Rank 0 (best):   exact page-name match (case-insensitive)
//	Rank 1:          page-name prefix match
//	Rank 2:          full display-path prefix match
//	Rank 3:          substring match anywhere in the display path
//
// Within the same rank tier results are ordered alphabetically by
// (notebook, section, page) for deterministic output. The returned slice is
// bounded to min(limit, searchPagesMax). Queries with fewer than
// searchPagesMinLen non-space characters return no results immediately
// (no DB round-trip).
func (a *App) SearchPages(query string, limit int) ([]parser.PageSummary, error) {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.db == nil {
		return nil, fmt.Errorf("vault database not loaded")
	}

	a.wg.Add(1)
	defer a.wg.Done()

	if limit <= 0 || limit > searchPagesMax {
		limit = searchPagesMax
	}

	// Reject queries that are too short to be meaningful.
	if nonSpaceLen(query) < searchPagesMinLen {
		return []parser.PageSummary{}, nil
	}

	var pages []db.PageLoc
	var err error
	a.coordinator.WithDBRead(func() { pages, err = a.db.ListDistinctPages() })
	if err != nil {
		return nil, err
	}

	q := strings.ToLower(query)

	// Score and collect matching pages.
	type scored struct {
		loc  db.PageLoc
		rank int
		path string // lowercase display path for matching
	}
	var hits []scored
	for _, loc := range pages {
		path := displayPath(loc)
		lp := strings.ToLower(path)
		page := strings.ToLower(loc.Page)

		var rank int = -1
		switch {
		case page == q:
			rank = 0 // exact page-name match
		case strings.HasPrefix(page, q):
			rank = 1 // page-name prefix
		case strings.HasPrefix(lp, q):
			rank = 2 // full display-path prefix
		case strings.Contains(lp, q):
			rank = 3 // substring anywhere in path
		}
		if rank < 0 {
			continue
		}
		hits = append(hits, scored{loc: loc, rank: rank, path: path})
	}

	// Sort by rank tier ascending, then alphabetical (notebook, section, page).
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].rank != hits[j].rank {
			return hits[i].rank < hits[j].rank
		}
		ai, aj := hits[i].loc, hits[j].loc
		if ai.Notebook != aj.Notebook {
			return ai.Notebook < aj.Notebook
		}
		if ai.Section != aj.Section {
			return ai.Section < aj.Section
		}
		return ai.Page < aj.Page
	})

	// Truncate to limit.
	if len(hits) > limit {
		hits = hits[:limit]
	}
	out := make([]parser.PageSummary, len(hits))
	for i, h := range hits {
		out[i] = parser.PageSummary{
			Source:   h.loc.Source,
			Notebook: h.loc.Notebook,
			Section:  h.loc.Section,
			Page:     h.loc.Page,
		}
	}
	return out, nil
}

// displayPath builds the ShortestUniquePath convention string:
// "nb/sec/page" or "nb/page" when section is empty.
func displayPath(loc db.PageLoc) string {
	if loc.Section != "" {
		return loc.Notebook + "/" + loc.Section + "/" + loc.Page
	}
	return loc.Notebook + "/" + loc.Page
}

// nonSpaceLen returns the number of non-space Unicode characters in s.
func nonSpaceLen(s string) int {
	n := 0
	for _, r := range s {
		if r != ' ' && r != '\t' && r != '\n' && r != '\r' {
			n++
		}
	}
	return n
}

// focusFilePath resolves the on-disk page file for a focus-lease operation,
// routing to the correct root via the notebook's source (#100). Shared by
// Acquire/Release/RefreshFocusLock so the lease key always matches the file
// the watcher sees — including linked notebooks.
func (a *App) focusFilePath(notebook, section, page string) (string, error) {
	safeNotebook := sanitizePathSegment(notebook)
	safeSection := sanitizePathSegment(section)
	safePage := sanitizePathSegment(page)
	if safeNotebook == "" || safePage == "" {
		return "", fmt.Errorf("invalid path metadata")
	}
	notebookDir, err := a.resolveNotebookDir(safeNotebook, a.resolveSourceByName(safeNotebook))
	if err != nil {
		return "", err
	}
	fp := filepath.Join(notebookDir, safeSection, safePage+".md")
	if !isPathWithinRoot(fp, notebookDir) {
		return "", fmt.Errorf("path escapes notebook root")
	}
	return fp, nil
}

// AcquireFocusLock registers a focus lock on a page file to ignore fsnotify updates.
func (a *App) AcquireFocusLock(notebook, section, page string) error {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.watcher == nil {
		return fmt.Errorf("watcher not running")
	}
	a.wg.Add(1)
	defer a.wg.Done()
	fp, err := a.focusFilePath(notebook, section, page)
	if err != nil {
		return err
	}
	a.watcher.LockFocus(fp)
	return nil
}

// ReleaseFocusLock removes a focus lock from a page file.
func (a *App) ReleaseFocusLock(notebook, section, page string) error {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.watcher == nil {
		return fmt.Errorf("watcher not running")
	}
	a.wg.Add(1)
	defer a.wg.Done()
	fp, err := a.focusFilePath(notebook, section, page)
	if err != nil {
		return err
	}
	a.watcher.UnlockFocus(fp)
	return nil
}

// RefreshFocusLock extends an existing focus lease for a page file. Called by the
// Svelte editor's heartbeat while it stays focused (#38); a no-op if the
// lease already expired (the editor must re-acquire).
func (a *App) RefreshFocusLock(notebook, section, page string) error {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.watcher == nil {
		return fmt.Errorf("watcher not running")
	}
	a.wg.Add(1)
	defer a.wg.Done()
	fp, err := a.focusFilePath(notebook, section, page)
	if err != nil {
		return err
	}
	a.watcher.RefreshFocus(fp)
	return nil
}
