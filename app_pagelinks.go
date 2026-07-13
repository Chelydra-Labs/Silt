package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
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
// Runs inside the caller's notebook-root LockFileWrite; acquires a per-source-
// file LockFileWrite for each rewrite so it cannot race autosave.
func (a *App) rewriteInboundPageLinks(oldNB, oldSec, oldPage, newNB, newSec, newPage string) {
	if a.db == nil || oldPage == "" || newPage == "" {
		return
	}
	if oldNB == newNB && oldSec == newSec && oldPage == newPage {
		return
	}

	// Fetch all page_links rows + the full page inventory for resolution.
	var rows []db.PageLinkRow
	var pages []db.PageLoc
	err := a.coordinator.WithDBReadResult(func() error {
		got, err := a.db.ListAllPageLinks()
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
		// concurrent SaveFileBlocks (autosave) on the same source file.
		writeOK := false
		a.coordinator.LockFileWrite(filePath, func() {
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
			if err := parser.WriteFileAtomic(filePath, []byte(content)); err != nil {
				log.Printf("rewriteInboundPageLinks: write %s: %v", filePath, err)
				return
			}
			writeOK = true
		})
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
