package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"silt/backend/core"
	"silt/backend/db"
	"silt/backend/parser"
	"sort"
	"strings"
)

// ResolvePageLink resolves a [[target]] wiki-link via shortest-unique-path
// rules (basename → section/page → notebook/section/page). Missing targets
// return Exists=false (no error); ambiguous targets return Ambiguous=true
// with Candidates. Linked notebooks are disambiguated by blocks.source (#545).
func (a *App) ResolvePageLink(target string) (parser.PageReference, error) {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	ref := parser.PageReference{Target: target}
	if a.db == nil {
		return ref, fmt.Errorf("vault database not loaded")
	}
	a.wg.Add(1)
	defer a.wg.Done()

	var out parser.PageReference
	err := a.coordinator.WithDBReadResult(func() error {
		got, err := a.db.ResolvePageLink(target)
		if err != nil {
			return err
		}
		out = got
		return nil
	})
	if err != nil {
		return ref, err
	}
	return out, nil
}

// pageLinksRewriteResult is the payload of the "page-links:rewritten" event
// emitted after a rename/move rewrites inbound wiki-links (#545 harden).
type pageLinksRewriteResult struct {
	Rewritten int `json:"rewritten"`
	Failed    int `json:"failed"`
}

// rewriteInboundPageLinks rewrites [[…]] links that uniquely resolve to the
// old page location so they track the rename/move. Ambiguous links (same
// basename in two pages) are left untouched — rewriting them would corrupt
// the other page's inbound links (#545 review fix).
//
// Acquires a per-source-file LockFileWrite for each rewrite so it cannot race
// autosave. Structural callers pass heldPaths (normalized lock set) so sources
// already locked skip re-entry; any source not in the set still takes
// LockFileWrite (#691).
func (a *App) rewriteInboundPageLinks(oldNB, oldSec, oldPage, newNB, newSec, newPage string) {
	a.rewriteInboundPageLinksWithJournal(oldNB, oldSec, oldPage, newNB, newSec, newPage, nil, nil)
}

// rewriteStaleInboundAfterRename rewrites residual [[old]] links after a
// structural rename unlocks. The under-lock pass may miss a source that saved
// a brand-new wiki-link after missingInboundPaths returned empty; by then the
// old page is gone from the index so resolve-gating would skip those rows.
// This sweep matches target_raw against the old location directly and only
// rewrites when the raw is currently unresolved (broken) and not ambiguous.
func (a *App) rewriteStaleInboundAfterRename(oldNB, oldSec, oldPage, newNB, newSec, newPage string) {
	if a.db == nil || oldPage == "" || newPage == "" {
		return
	}
	if oldNB == newNB && oldSec == newSec && oldPage == newPage {
		return
	}
	candidates := db.LinkTargetRawCandidates([]struct{ Notebook, Section, Page string }{
		{oldNB, oldSec, oldPage},
	})
	var rows []db.PageLinkRow
	var pages []db.PageLoc
	err := a.coordinator.WithDBReadResult(func() error {
		got, err := a.db.ListPageLinksByTargetRaws(candidates)
		if err != nil {
			return err
		}
		rows = got
		pages, err = a.db.ListDistinctPages()
		return err
	})
	if err != nil || len(rows) == 0 {
		return
	}

	type srcKey struct{ nb, sec, page string }
	type rewrite struct{ oldRaw, newRaw string }
	byFile := map[srcKey][]rewrite{}
	for _, r := range rows {
		if !db.PageMatchesTarget(oldNB, oldSec, oldPage, r.TargetRaw) {
			continue
		}
		ref := db.ResolvePageLinkAgainst(r.TargetRaw, pages)
		if ref.Ambiguous {
			continue
		}
		if ref.Exists {
			// Already points at a live page (new location or unrelated) — leave.
			continue
		}
		newRaw := db.MapTargetRaw(r.TargetRaw, oldNB, oldSec, oldPage, newNB, newSec, newPage)
		if newRaw == r.TargetRaw {
			continue
		}
		k := srcKey{r.SourceNotebook, r.SourceSection, r.SourcePage}
		byFile[k] = append(byFile[k], rewrite{oldRaw: r.TargetRaw, newRaw: newRaw})
	}
	if len(byFile) == 0 {
		return
	}

	srcKeys := make([]srcKey, 0, len(byFile))
	for k := range byFile {
		srcKeys = append(srcKeys, k)
	}
	sort.Slice(srcKeys, func(i, j int) bool {
		a, b := srcKeys[i], srcKeys[j]
		return a.nb+"\x00"+a.sec+"\x00"+a.page < b.nb+"\x00"+b.sec+"\x00"+b.page
	})

	rewritten := 0
	for _, k := range srcKeys {
		seen := map[string]string{}
		for _, rw := range byFile[k] {
			seen[rw.oldRaw] = rw.newRaw
		}
		source := a.resolveSourceByName(k.nb)
		notebookDir, err := a.resolveNotebookDir(k.nb, source)
		if err != nil {
			continue
		}
		filePath := filepath.Join(notebookDir, k.sec, k.page+".md")
		if !isPathWithinRoot(filePath, notebookDir) {
			continue
		}
		writeOK := false
		a.coordinator.LockFileWrite(filePath, func() {
			contentBytes, err := os.ReadFile(filePath)
			if err != nil {
				return
			}
			content := string(contentBytes)
			total := 0
			for oldRaw, newRaw := range seen {
				var n int
				content, n = db.RewritePageLinksInContent(content, oldRaw, newRaw)
				total += n
			}
			if total == 0 {
				return
			}
			a.tracker.RegisterWrite(filePath)
			if err := parser.WriteFileAtomic(filePath, []byte(content)); err != nil {
				return
			}
			writeOK = true
		})
		if writeOK {
			a.reindexFile(filePath, k.nb, k.sec, k.page)
			rewritten++
		}
	}
	if rewritten > 0 {
		log.Printf("rewriteStaleInboundAfterRename: rewritten=%d (%s/%s/%s → %s/%s/%s)",
			rewritten, oldNB, oldSec, oldPage, newNB, newSec, newPage)
		a.emit("page-links:rewritten", pageLinksRewriteResult{Rewritten: rewritten})
	}
}

// rewriteInboundPageLinksWithJournal rewrites inbound wiki-links. heldPaths is
// the normalized set of file locks the structural caller already holds; when
// non-nil, sources in that set skip LockFileWrite (non-reentrant). Sources not
// in the set still take LockFileWrite so a late-arriving inbound file cannot
// race autosave (#691).
func (a *App) rewriteInboundPageLinksWithJournal(oldNB, oldSec, oldPage, newNB, newSec, newPage string, journal map[string]renameLinkJournalEntry, heldPaths map[string]bool) {
	if a.db == nil || oldPage == "" || newPage == "" {
		return
	}
	if oldNB == newNB && oldSec == newSec && oldPage == newPage {
		return
	}

	// Filtered page_links rows (by target_raw candidates) + full page inventory
	// for resolve-gating. Avoids full-table scans on large vaults.
	var rows []db.PageLinkRow
	var pages []db.PageLoc
	candidates := db.LinkTargetRawCandidates([]struct{ Notebook, Section, Page string }{
		{oldNB, oldSec, oldPage},
	})
	err := a.coordinator.WithDBReadResult(func() error {
		got, err := a.db.ListPageLinksByTargetRaws(candidates)
		if err != nil {
			return err
		}
		rows = got
		pages, err = a.db.ListDistinctPages()
		return err
	})
	if err != nil {
		log.Printf("rewriteInboundPageLinks: list: %v", err)
		a.emit("page-links:rewritten", pageLinksRewriteResult{Failed: 1})
		return
	}
	if len(rows) == 0 {
		return
	}

	// For each row, resolve target_raw against the page inventory. Only
	// rewrite links that UNAMBIGUOUSLY resolve to the old page location.
	// This handles basename ambiguity, case-insensitive spelling, and
	// path-suffix forms in one pass.
	type srcKey struct{ nb, sec, page string }
	type rewrite struct {
		oldRaw string
		newRaw string
	}
	byFile := map[srcKey][]rewrite{}
	for _, r := range rows {
		ref := db.ResolvePageLinkAgainst(r.TargetRaw, pages)
		if !ref.Exists {
			continue
		}
		// Only rewrite links that point at the OLD page being renamed.
		if ref.Notebook != oldNB || ref.Section != oldSec || ref.Page != oldPage {
			continue
		}
		newRaw := db.MapTargetRaw(r.TargetRaw, oldNB, oldSec, oldPage, newNB, newSec, newPage)
		if newRaw == r.TargetRaw {
			continue
		}
		k := srcKey{r.SourceNotebook, r.SourceSection, r.SourcePage}
		byFile[k] = append(byFile[k], rewrite{oldRaw: r.TargetRaw, newRaw: newRaw})
	}
	if len(byFile) == 0 {
		return
	}

	// Sort source files for deterministic lock acquisition order.
	srcKeys := make([]srcKey, 0, len(byFile))
	for k := range byFile {
		srcKeys = append(srcKeys, k)
	}
	sort.Slice(srcKeys, func(i, j int) bool {
		a, b := srcKeys[i], srcKeys[j]
		return a.nb+"\x00"+a.sec+"\x00"+a.page < b.nb+"\x00"+b.sec+"\x00"+b.page
	})

	rewritten := 0
	failed := 0
	for _, k := range srcKeys {
		rewrites := byFile[k]
		seen := map[string]string{}
		for _, rw := range rewrites {
			seen[rw.oldRaw] = rw.newRaw
		}
		source := a.resolveSourceByName(k.nb)
		notebookDir, err := a.resolveNotebookDir(k.nb, source)
		if err != nil {
			log.Printf("rewriteInboundPageLinks: resolve %s: %v", k.nb, err)
			failed++
			continue
		}
		filePath := filepath.Join(notebookDir, k.sec, k.page+".md")
		if !isPathWithinRoot(filePath, notebookDir) {
			log.Printf("rewriteInboundPageLinks: path escapes root: %s", filePath)
			failed++
			continue
		}

		// Acquire the per-file write lock so the rewrite cannot race a
		// concurrent SaveFileBlocks (autosave) on the same source file —
		// unless the structural caller already holds this exact path (#691).
		writeOK := false
		doRewrite := func() {
			contentBytes, err := os.ReadFile(filePath)
			if err != nil {
				log.Printf("rewriteInboundPageLinks: read %s: %v", filePath, err)
				return
			}
			content := string(contentBytes)
			total := 0
			for oldRaw, newRaw := range seen {
				var n int
				content, n = db.RewritePageLinksInContent(content, oldRaw, newRaw)
				total += n
			}
			if total == 0 {
				return
			}
			a.tracker.RegisterWrite(filePath)
			if journal != nil {
				if _, exists := journal[filePath]; !exists {
					journal[filePath] = renameLinkJournalEntry{
						content: append([]byte(nil), contentBytes...), source: source,
						notebook: k.nb, section: k.sec, page: k.page,
					}
				}
			}
			if err := parser.WriteFileAtomic(filePath, []byte(content)); err != nil {
				log.Printf("rewriteInboundPageLinks: write %s: %v", filePath, err)
				return
			}
			writeOK = true
		}
		held := heldPaths != nil && heldPaths[core.NormalizeFileLockPath(filePath)]
		if held {
			doRewrite()
		} else {
			a.coordinator.LockFileWrite(filePath, doRewrite)
		}
		if !writeOK {
			if len(seen) > 0 {
				failed++
			}
			continue
		}
		a.reindexFile(filePath, k.nb, k.sec, k.page)
		rewritten++
	}
	if rewritten > 0 || failed > 0 {
		log.Printf("rewriteInboundPageLinks: rewritten=%d failed=%d (%s/%s/%s → %s/%s/%s)",
			rewritten, failed, oldNB, oldSec, oldPage, newNB, newSec, newPage)
		a.emit("page-links:rewritten", pageLinksRewriteResult{
			Rewritten: rewritten,
			Failed:    failed,
		})
	}
}

// rewriteInboundPageLinksForSection rewrites links to every page that lived
// under oldSec after a section rename (oldSec → newSec) within notebook.
func (a *App) rewriteInboundPageLinksForSection(notebook, oldSec, newSec string, pageNames []string) {
	for _, page := range pageNames {
		page = strings.TrimSuffix(page, ".md")
		if page == "" {
			continue
		}
		a.rewriteInboundPageLinks(notebook, oldSec, page, notebook, newSec, page)
	}
}

// renameTarget is a (notebook, section, page) location used by structural
// rename inbound collect.
type renameTarget struct{ nb, sec, page string }

// collectInboundSourcePaths returns on-disk paths of pages that have wiki-links
// uniquely resolving to any of the given targets. Structural rename locks these
// together with descendant paths so inbound rewrites do not nest LockFileWrite
// under LockPathsWrite. Returns an error on DB failure so callers fail closed
// rather than renaming without inbound locks.
func (a *App) collectInboundSourcePaths(targets []renameTarget) ([]string, error) {
	if a.db == nil || len(targets) == 0 {
		return nil, nil
	}
	want := make(map[string]bool, len(targets))
	candIn := make([]struct{ Notebook, Section, Page string }, 0, len(targets))
	for _, t := range targets {
		if t.page == "" {
			continue
		}
		want[t.nb+"\x00"+t.sec+"\x00"+t.page] = true
		candIn = append(candIn, struct{ Notebook, Section, Page string }{t.nb, t.sec, t.page})
	}
	if len(want) == 0 {
		return nil, nil
	}

	candidates := db.LinkTargetRawCandidates(candIn)
	var rows []db.PageLinkRow
	var pages []db.PageLoc
	err := a.coordinator.WithDBReadResult(func() error {
		got, err := a.db.ListPageLinksByTargetRaws(candidates)
		if err != nil {
			return err
		}
		rows = got
		pages, err = a.db.ListDistinctPages()
		return err
	})
	if err != nil {
		return nil, err
	}

	seenPath := map[string]bool{}
	var out []string
	for _, r := range rows {
		ref := db.ResolvePageLinkAgainst(r.TargetRaw, pages)
		if !ref.Exists {
			continue
		}
		if !want[ref.Notebook+"\x00"+ref.Section+"\x00"+ref.Page] {
			continue
		}
		source := a.resolveSourceByName(r.SourceNotebook)
		notebookDir, err := a.resolveNotebookDir(r.SourceNotebook, source)
		if err != nil {
			continue
		}
		filePath := filepath.Join(notebookDir, r.SourceSection, r.SourcePage+".md")
		if !isPathWithinRoot(filePath, notebookDir) || seenPath[filePath] {
			continue
		}
		seenPath[filePath] = true
		out = append(out, filePath)
	}
	return out, nil
}

// missingInboundPaths returns inbound source paths for targets that are not
// already in locked (normalized). Used inside LockPathsWrite to detect
// collect-then-lock TOCTOU and retry with an extended lock set.
func (a *App) missingInboundPaths(targets []renameTarget, locked map[string]bool) ([]string, error) {
	fresh, err := a.collectInboundSourcePaths(targets)
	if err != nil {
		return nil, err
	}
	var missing []string
	for _, p := range fresh {
		n := core.NormalizeFileLockPath(p)
		if n != "" && !locked[n] {
			missing = append(missing, p)
		}
	}
	return missing, nil
}

// unionLockPaths appends extra paths not already present (by normalized key).
func unionLockPaths(base []string, extra []string) []string {
	seen := lockPathSet(base)
	out := append([]string{}, base...)
	for _, p := range extra {
		n := core.NormalizeFileLockPath(p)
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, p)
	}
	return out
}
