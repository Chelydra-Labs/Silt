package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"silt/backend/db"
	"silt/backend/parser"
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

// rewriteInboundPageLinks rewrites [[…]] links that point at oldNB/oldSec/oldPage
// so they point at newNB/newSec/newPage, preserving |alias and #heading. Runs
// inside the caller's LockFileWrite; reindexes each rewritten source file.
// Block UUIDs are never touched — only the link text changes (#545).
// Emits page-links:rewritten when any file was rewritten or failed so the UI
// can surface partial failure (silent log-only was a hardening gap).
func (a *App) rewriteInboundPageLinks(oldNB, oldSec, oldPage, newNB, newSec, newPage string) {
	if a.db == nil || oldPage == "" || newPage == "" {
		return
	}
	if oldNB == newNB && oldSec == newSec && oldPage == newPage {
		return
	}
	candidates := db.PathVariants(oldNB, oldSec, oldPage)
	var rows []db.PageLinkRow
	err := a.coordinator.WithDBReadResult(func() error {
		got, err := a.db.ListPageLinksByTargetRaws(candidates)
		if err != nil {
			return err
		}
		rows = got
		return nil
	})
	if err != nil {
		log.Printf("rewriteInboundPageLinks: list: %v", err)
		a.emit("page-links:rewritten", pageLinksRewriteResult{Failed: 1})
		return
	}
	if len(rows) == 0 {
		return
	}

	// Group by source page (one file rewrite per page).
	type srcKey struct{ nb, sec, page string }
	type rewrite struct {
		oldRaw string
		newRaw string
	}
	byFile := map[srcKey][]rewrite{}
	for _, r := range rows {
		newRaw := db.MapTargetRaw(r.TargetRaw, oldNB, oldSec, oldPage, newNB, newSec, newPage)
		if newRaw == r.TargetRaw {
			continue
		}
		k := srcKey{r.SourceNotebook, r.SourceSection, r.SourcePage}
		byFile[k] = append(byFile[k], rewrite{oldRaw: r.TargetRaw, newRaw: newRaw})
	}

	rewritten := 0
	failed := 0
	for k, rewrites := range byFile {
		// Deduplicate rewrite pairs (same oldRaw may appear on many blocks).
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
		contentBytes, err := os.ReadFile(filePath)
		if err != nil {
			log.Printf("rewriteInboundPageLinks: read %s: %v", filePath, err)
			failed++
			continue
		}
		content := string(contentBytes)
		total := 0
		for oldRaw, newRaw := range seen {
			var n int
			content, n = db.RewritePageLinksInContent(content, oldRaw, newRaw)
			total += n
		}
		if total == 0 {
			continue
		}
		a.tracker.RegisterWrite(filePath)
		if err := parser.WriteFileAtomic(filePath, []byte(content)); err != nil {
			log.Printf("rewriteInboundPageLinks: write %s: %v", filePath, err)
			failed++
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
