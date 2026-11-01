package main

// ResolveExternalConflict settles a whole-page save the external-edit gate
// refused (#1005). Both versions always survive on disk: the loser is written
// to a sibling conflict copy named `<page>.conflict-<YYYYMMDD-HHMMSS>.md` (the
// Syncthing/Dropbox convention) that is registered and explicitly indexed so
// it surfaces as a real page, not a watcher ghost.
//
//	resolution      original path              conflict sibling
//	keep_mine       my stashed editor save     theirs (from disk)
//	keep_both       my stashed editor save     theirs (from disk)
//	keep_theirs     theirs (already on disk)   my stashed editor save
//
// keep_mine and keep_both have identical disk outcomes; both enum values
// exist because they are different user endorsements of the same fact.
//
// The refused save's payload was stashed at refusal time (see
// refusedPageWrite) — the editor never has to resend it. Without a stash
// (e.g. the app restarted between refusal and click) the call fails loudly
// instead of guessing.

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"silt/backend/core"
	"silt/backend/parser"
)

// External-edit conflict resolutions, mirrored on the frontend.
const (
	conflictResolutionKeepMine   = "keep_mine"
	conflictResolutionKeepTheirs = "keep_theirs"
	conflictResolutionKeepBoth   = "keep_both"
)

// pageFrontmatterLine matches a `page:` frontmatter key (YAML allows a space
// before the colon; keys are case-sensitive by spec but tolerate the common
// capitalization drift seen in hand-edited vaults).
var pageFrontmatterLine = regexp.MustCompile(`^[Pp]age[ \t]*:`)

// ResolveExternalConflict is the IPC entry point (notebook, section, page,
// resolution). All writes happen under LockFileWrite for the original path.
// It returns the page name of the conflict copy it created (for the success
// announcement), or an empty string when nothing was written.
func (a *App) ResolveExternalConflict(notebook, section, page, resolution string) (string, error) {
	switch resolution {
	case conflictResolutionKeepMine, conflictResolutionKeepTheirs, conflictResolutionKeepBoth:
	default:
		return "", fmt.Errorf("invalid resolution %q (valid: keep_mine, keep_theirs, keep_both)", resolution)
	}

	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.db == nil {
		return "", fmt.Errorf("vault database not loaded")
	}

	safeNotebook := sanitizePathSegment(notebook)
	safeSection, secErr := historySection(section)
	if secErr != nil {
		return "", invalidNavigationPath(secErr)
	}
	safePage := sanitizePathSegment(page)
	if safeNotebook == "" || safePage == "" {
		return "", fmt.Errorf("invalid path metadata")
	}

	source := a.resolveSourceByName(safeNotebook)
	notebookDir, err := a.resolveNotebookDir(safeNotebook, source)
	if err != nil {
		return "", fmt.Errorf("resolve notebook dir: %w", err)
	}
	filePath := filepath.Join(notebookDir, safeSection, safePage+".md")
	if !isPathWithinRoot(filePath, notebookDir) {
		return "", fmt.Errorf("path escapes notebook root")
	}

	a.wg.Add(1)
	defer a.wg.Done()

	// The stash is popped INSIDE LockFileWrite: a racing refused save that
	// wins the lock first re-stashes the newest buffer, and the in-lock pop
	// then replays that newest version. Popping outside the lock could
	// replay an older buffer while the newer stash lands and gets orphaned.
	key := core.NormalizeFileLockPath(filePath)
	var conflictPage string
	var resolveErr error
	noStash := false
	a.coordinator.LockFileWrite(filePath, func() {
		rw, ok := a.popRefusedWrite(key)
		if !ok {
			noStash = true
			return
		}
		conflictPage, resolveErr = a.resolveExternalConflictLocked(rw, filePath, resolution)
		if resolveErr != nil {
			// Restore the stash so the user can retry the resolution; the
			// disk state is unchanged (every step below fails before writing
			// when it fails at all). Only-if-absent: a newer refusal must
			// never be clobbered by this older buffer.
			a.restoreRefusedWriteIfAbsent(key, rw)
		}
	})
	if noStash {
		return "", wrapSentinelAsIPCError(CodeExternalEditConflict,
			"no pending external-edit conflict for this page — retry the save",
			errExternalEditConflict)
	}
	if resolveErr != nil {
		return "", resolveErr
	}

	// Emit after the lock releases so subscribers can re-fetch without
	// re-entering the file write lock (mirrors the save funnels). Both the
	// original and the new copy changed; conflictPage names the copy actually
	// written (suffix-aware), not a recomputed guess.
	a.emitBlockChanged("", safeNotebook, safeSection, safePage, "")
	a.emitBlockChanged("", safeNotebook, safeSection, conflictPage, "")
	return conflictPage, nil
}

// conflictPageName is the conflict copy's page/file stem. Second resolution
// within the same wall-clock second gets a numeric suffix so the two files
// cannot collide.
func conflictPageName(page string, now time.Time) string {
	return page + ".conflict-" + now.Format("20060102-150405")
}

// uniqueConflictSibling returns the sibling path for a conflict copy, adding a
// numeric suffix when a previous copy already occupies the timestamped name.
func uniqueConflictSibling(dir, page string, now time.Time) string {
	name := conflictPageName(page, now)
	sibling := filepath.Join(dir, name+".md")
	for i := 2; ; i++ {
		if _, err := os.Stat(sibling); os.IsNotExist(err) {
			return sibling
		}
		sibling = filepath.Join(dir, fmt.Sprintf("%s-%d.md", name, i))
	}
}

// resolveExternalConflictLocked performs one resolution and returns the page
// name of the conflict copy it created. The caller holds LockFileWrite for
// filePath. Fails before any write when preconditions fail, so an error here
// leaves disk (and the stash) untouched.
func (a *App) resolveExternalConflictLocked(rw refusedPageWrite, filePath, resolution string) (string, error) {
	theirs, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("read current page version: %w", err)
	}
	dir := filepath.Dir(filePath)
	now := time.Now()
	sibling := uniqueConflictSibling(dir, rw.page, now)
	conflictPage := strings.TrimSuffix(filepath.Base(sibling), ".md")

	if resolution == conflictResolutionKeepTheirs {
		// My (stashed) version becomes the conflict copy; theirs stays on the
		// original path. The copy is written through the same funnels so it is
		// registered, indexed, and fingerprint-refreshed like any Silt page.
		if rw.isMarkdown {
			_, werr := a.writePageMarkdownLocked(sibling, rw.source, rw.notebook, rw.section, conflictPage,
				rw.notebook, rw.section, conflictPage, nil, rw.markdown, rw.reason, false)
			if werr != nil {
				return "", werr
			}
		} else {
			if werr := a.writePageFileLocked(sibling, rw.source, rw.notebook, rw.section, conflictPage, rw.blocks, rw.reason); werr != nil {
				return "", werr
			}
		}
		// The external edit was never indexed (that is what made it
		// "external"); index theirs now so app views match disk, then
		// re-baseline the fingerprint to theirs.
		blocks, meta, _, _, perr := parser.ParseFileContent(string(theirs),
			rw.notebook, rw.section, rw.page, fileOrDefaultDate(filePath), a.spacesPerTab)
		if perr != nil {
			log.Printf("resolve conflict: parse theirs for %s/%s/%s failed (file kept; next scan indexes it): %v",
				rw.notebook, rw.section, rw.page, perr)
		} else if idxErr := a.indexFile(rw.source, meta.Notebook, meta.Section, meta.Page, blocks, meta, meta.Warnings...); idxErr != nil {
			log.Printf("resolve conflict: index theirs for %s/%s/%s failed (file kept; next scan indexes it): %v",
				rw.notebook, rw.section, rw.page, idxErr)
		}
		a.refreshFingerprintAfterWrite(filePath, theirs)
		return conflictPage, nil
	}

	// keep_mine / keep_both: archive theirs to the conflict copy, then replay
	// my stashed save onto the original path.
	archived := []byte(conflictCopyContent(theirs, conflictPage, rw.notebook, rw.section))
	if err := a.writeConflictCopyLocked(sibling, rw.source, rw.notebook, rw.section, archived); err != nil {
		return "", err
	}
	if rw.isMarkdown {
		_, werr := a.writePageMarkdownLocked(filePath, rw.source, rw.notebook, rw.section, rw.page,
			rw.notebook, rw.section, rw.page, theirs, rw.markdown, rw.reason, false)
		if werr != nil {
			return "", werr
		}
	} else {
		if werr := a.writePageFileLocked(filePath, rw.source, rw.notebook, rw.section, rw.page, rw.blocks, rw.reason); werr != nil {
			return "", werr
		}
	}
	return conflictPage, nil
}

// writeConflictCopyLocked writes + indexes one conflict copy. The content is
// user content (not Silt state), so it is registered with the tracker and
// explicitly indexed — it must surface as a page immediately, not wait for
// the watcher. notebook/section are the ORIGINAL page's safe locator: they
// are the ParseFileContent defaults (and the minted frontmatter fallback),
// so a copy of a frontmatter-less external version indexes under the same
// notebook/section as its sibling instead of a junk `notebook=”` row.
func (a *App) writeConflictCopyLocked(siblingPath, source, notebook, section string, content []byte) error {
	a.tracker.RegisterWrite(siblingPath)
	if err := atomicPageWrite(siblingPath, content); err != nil {
		return fmt.Errorf("write conflict copy: %w", err)
	}
	conflictPage := strings.TrimSuffix(filepath.Base(siblingPath), ".md")
	blocks, meta, _, _, perr := parser.ParseFileContent(string(content),
		notebook, section, conflictPage, fileOrDefaultDate(siblingPath), a.spacesPerTab)
	if perr != nil {
		// The copy is safely on disk; a parse failure only delays indexing
		// until the next scan. Never fail the resolution after the write.
		log.Printf("resolve conflict: parse copy %s failed: %v", siblingPath, perr)
		return nil
	}
	if idxErr := a.indexFile(source, meta.Notebook, meta.Section, meta.Page, blocks, meta, meta.Warnings...); idxErr != nil {
		log.Printf("resolve conflict: index copy %s failed: %v", siblingPath, idxErr)
	}
	return nil
}

// conflictCopyContent returns theirs with the frontmatter `page:` rewritten to
// the conflict name, so the copy indexes as its own page instead of colliding
// with the original's (source, notebook, section, page) locator. The body is
// byte-identical to theirs. notebook/section (the original page's locator)
// are minted only when theirs has no frontmatter to carry them — otherwise
// the parse defaults handle missing keys.
func conflictCopyContent(theirs []byte, conflictPage, notebook, section string) string {
	frontmatter, body := parser.SplitFrontmatter(string(theirs))
	if frontmatter == "" {
		// No frontmatter to rewrite — mint a minimal block carrying the
		// conflict identity and its home locator (so a rescan keeps the copy
		// in the same notebook/section as its sibling); the body stays
		// verbatim.
		var b strings.Builder
		b.WriteString("---\n")
		if notebook != "" {
			b.WriteString("notebook: " + strconv.Quote(notebook) + "\n")
		}
		if section != "" {
			b.WriteString("section: " + strconv.Quote(section) + "\n")
		}
		b.WriteString("page: " + strconv.Quote(conflictPage) + "\n---\n")
		b.WriteString(body)
		return b.String()
	}
	lines := strings.Split(strings.TrimSuffix(frontmatter, "\n"), "\n")
	out := make([]string, 0, len(lines)+1)
	out = append(out, lines[0]) // opening ---
	replaced := false
	for _, ln := range lines[1:] {
		if !replaced && pageFrontmatterLine.MatchString(ln) {
			out = append(out, "page: "+strconv.Quote(conflictPage))
			replaced = true
			continue
		}
		out = append(out, ln)
	}
	if !replaced {
		out = append(out, "page: "+strconv.Quote(conflictPage))
	}
	return strings.Join(out, "\n") + "\n" + body
}
