package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"silt/backend/parser"
)

var pageBlockReferencePattern = regexp.MustCompile(`\(\(([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})\)\)`)
var pageEmbedReferencePattern = regexp.MustCompile(`\{\{embed:([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})\}\}`)

// These seams let backend tests exercise post-write failures without
// weakening the production write path. They are intentionally package-local;
// callers cannot bypass the rollback protocol.
var duplicatePagePostWriteParse = parser.ParseFileContent
var duplicatePageBeforeWrite = func() {}
var duplicatePageIndex = func(a *App, source, notebook, section, page string, blocks []parser.ParsedBlock, tags []string, warnings ...string) error {
	var err error
	a.coordinator.WithDBWrite(func() {
		err = a.db.IndexFileBlocks(source, notebook, section, page, blocks, tags, warnings...)
	})
	return err
}

func validatePageActionSegment(value, label string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", NewIPCError(CodeInvalidNavigationPath, label+" is required")
	}
	if strings.ContainsAny(value, "/\\") {
		return "", NewIPCError(CodeInvalidNavigationPath, label+" must be one path segment")
	}
	if _, err := validateSectionPath(value, false); err != nil {
		return "", invalidNavigationPath(fmt.Errorf("invalid %s: %w", label, err))
	}
	return value, nil
}

func pageActionLocation(notebook, section, page string) (string, string, string, error) {
	safeNotebook, err := validatePageActionSegment(notebook, "notebook name")
	if err != nil {
		return "", "", "", err
	}
	safeSection, sectionErr := validateSectionPath(section, true)
	if sectionErr != nil {
		return "", "", "", invalidNavigationPath(sectionErr)
	}
	safePage, pageErr := validatePageActionSegment(page, "page name")
	if pageErr != nil {
		return "", "", "", pageErr
	}
	return safeNotebook, safeSection, safePage, nil
}

func pageActionError(code IPCErrorCode, message string, err error) error {
	if err != nil {
		message = fmt.Sprintf("%s: %v", message, err)
	}
	return NewIPCError(code, message)
}

// DuplicatePage copies a page within its resolved notebook root. The target
// remains in the same section and linked content never crosses into the vault.
func (a *App) DuplicatePage(notebook, section, page, targetName string) error {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.vaultPath == "" || a.db == nil {
		return fmt.Errorf("vault not loaded")
	}

	safeNotebook, safeSection, safePage, err := pageActionLocation(notebook, section, page)
	if err != nil {
		return err
	}
	safeTarget, err := validatePageActionSegment(targetName, "target page name")
	if err != nil {
		return err
	}
	if safeTarget == safePage {
		return NewIPCError(CodeNavigationConflict, fmt.Sprintf("a page named %q already exists", safeTarget))
	}

	source := a.resolveSourceByName(safeNotebook)
	notebookDir, err := a.resolveNotebookDir(safeNotebook, source)
	if err != nil {
		return pageActionError(CodeNavigationUnavailable, "notebook root is unavailable", err)
	}
	if info, statErr := os.Stat(notebookDir); statErr != nil || !info.IsDir() {
		if strings.HasPrefix(source, "linked:") {
			return pageActionError(CodeNavigationUnavailable, "linked notebook root is unavailable", statErr)
		}
		return pageActionError(CodeNavigationNotFound, "notebook root not found", statErr)
	}
	sourcePath := filepath.Join(notebookDir, filepath.FromSlash(safeSection), safePage+".md")
	targetPath := filepath.Join(notebookDir, filepath.FromSlash(safeSection), safeTarget+".md")
	if !isPathWithinRoot(sourcePath, notebookDir) || !isCreationPathWithinRoot(targetPath, notebookDir) {
		return NewIPCError(CodeInvalidNavigationPath, "page path escapes notebook root")
	}

	a.wg.Add(1)
	defer a.wg.Done()
	var runErr error
	a.coordinator.LockFileWrite(targetPath, func() {
		if _, statErr := os.Stat(sourcePath); statErr != nil {
			if os.IsNotExist(statErr) {
				runErr = pageActionError(CodeNavigationNotFound, "source page not found", statErr)
			} else {
				runErr = pageActionError(CodeNavigationUnavailable, "source page cannot be read", statErr)
			}
			return
		}
		if _, statErr := os.Stat(targetPath); statErr == nil {
			runErr = NewIPCError(CodeNavigationConflict, fmt.Sprintf("a page named %q already exists", safeTarget))
			return
		} else if !os.IsNotExist(statErr) {
			runErr = pageActionError(CodeNavigationDuplicate, "target page cannot be checked", statErr)
			return
		}

		content, readErr := os.ReadFile(sourcePath)
		if readErr != nil {
			runErr = pageActionError(CodeNavigationNotFound, "source page cannot be read", readErr)
			return
		}
		blocks, _, canonical, _, parseErr := parser.ParseFileContent(
			string(content), safeNotebook, safeSection, safePage,
			fileOrDefaultDate(sourcePath), a.spacesPerTab,
		)
		if parseErr != nil {
			runErr = pageActionError(CodeNavigationDuplicate, "source page cannot be parsed", parseErr)
			return
		}

		idMap := make(map[string]string, len(blocks))
		for i := range blocks {
			if blocks[i].ID == "" {
				continue
			}
			idMap[blocks[i].ID] = uuid.NewString()
		}
		for i := range blocks {
			oldID := blocks[i].ID
			if newID, ok := idMap[oldID]; ok {
				blocks[i].ID = newID
			}
			if newParent, ok := idMap[blocks[i].ParentID]; ok {
				blocks[i].ParentID = newParent
			}
			for j, blockedBy := range blocks[i].BlockedBy {
				if newID, ok := idMap[blockedBy]; ok {
					blocks[i].BlockedBy[j] = newID
				}
			}
			blocks[i].CleanText = remapPageReferences(blocks[i].CleanText, idMap)
		}

		frontmatter, body := parser.SplitFrontmatter(canonical)
		if frontmatter == "" {
			frontmatter = fmt.Sprintf("---\nnotebook: %q\nsection: %q\npage: %q\n---\n", safeNotebook, safeSection, safeTarget)
		} else {
			frontmatter = updateFrontmatterField(frontmatter, "notebook", safeNotebook)
			frontmatter = updateFrontmatterField(frontmatter, "section", safeSection)
			frontmatter = updateFrontmatterField(frontmatter, "page", safeTarget)
		}
		body = remapPageReferences(body, idMap)
		duplicate := parser.RenderFileContent(blocks, body, frontmatter, a.spacesPerTab)

		duplicatePageBeforeWrite()
		a.tracker.RegisterWrite(targetPath)
		if writeErr := parser.WriteFileAtomic(targetPath, []byte(duplicate)); writeErr != nil {
			runErr = pageActionError(CodeNavigationDuplicate, "duplicate page cannot be written", writeErr)
			return
		}
		parsedBlocks, meta, _, _, reparseErr := duplicatePagePostWriteParse(
			duplicate, safeNotebook, safeSection, safeTarget,
			fileOrDefaultDate(targetPath), a.spacesPerTab,
		)
		if reparseErr != nil {
			runErr = rollbackDuplicateTarget(a, targetPath, source, safeNotebook, safeSection, safeTarget,
				pageActionError(CodeNavigationDuplicate, "duplicate page could not be parsed; target rolled back", reparseErr))
			return
		}
		indexErr := duplicatePageIndex(a, source, meta.Notebook, meta.Section, meta.Page, parsedBlocks, meta.Tags, meta.Warnings...)
		if indexErr != nil {
			runErr = rollbackDuplicateTarget(a, targetPath, source, safeNotebook, safeSection, safeTarget,
				pageActionError(CodeNavigationDuplicate, "duplicate page could not be indexed; target rolled back", indexErr))
		}
	})
	return runErr
}

func rollbackDuplicateTarget(a *App, targetPath, source, notebook, section, page string, cause error) error {
	a.tracker.RegisterWrite(targetPath)
	removeErr := os.Remove(targetPath)
	var clearErr error
	a.coordinator.WithDBWrite(func() {
		if err := a.db.ClearFileBlocks(nil, source, notebook, section, page); err != nil {
			clearErr = err
		}
		if err := a.db.ForgetFile(targetPath); err != nil && clearErr == nil {
			clearErr = err
		}
	})
	if removeErr != nil && !os.IsNotExist(removeErr) {
		return pageActionError(CodeNavigationDuplicate, "duplicate failed and rollback could not remove the target", fmt.Errorf("%v; original failure: %w", removeErr, cause))
	}
	if clearErr != nil {
		return pageActionError(CodeNavigationDuplicate, "duplicate failed and rollback could not clean the index", fmt.Errorf("%v; original failure: %w", clearErr, cause))
	}
	return cause
}

func remapPageReferences(content string, ids map[string]string) string {
	if len(ids) == 0 {
		return content
	}
	content = pageBlockReferencePattern.ReplaceAllStringFunc(content, func(match string) string {
		id := match[2 : len(match)-2]
		if replacement, ok := ids[id]; ok {
			return "((" + replacement + "))"
		}
		return match
	})
	return pageEmbedReferencePattern.ReplaceAllStringFunc(content, func(match string) string {
		id := match[len("{{embed:") : len(match)-len("}}")]
		if replacement, ok := ids[id]; ok {
			return "{{embed:" + replacement + "}}"
		}
		return match
	})
}

// RevealPageInOS resolves and opens the page file through the same native
// opener used by notebook reveal. The resolved path is never accepted from
// the caller, which keeps vault and linked-root guards server-side.
func (a *App) RevealPageInOS(notebook, section, page string) error {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.vaultPath == "" {
		return fmt.Errorf("vault not loaded")
	}
	safeNotebook, safeSection, safePage, err := pageActionLocation(notebook, section, page)
	if err != nil {
		return err
	}
	source := a.resolveSourceByName(safeNotebook)
	notebookDir, err := a.resolveNotebookDir(safeNotebook, source)
	if err != nil {
		return pageActionError(CodeNavigationUnavailable, "notebook root is unavailable", err)
	}
	if info, statErr := os.Stat(notebookDir); statErr != nil || !info.IsDir() {
		if strings.HasPrefix(source, "linked:") {
			return pageActionError(CodeNavigationUnavailable, "linked notebook root is unavailable", statErr)
		}
		return pageActionError(CodeNavigationNotFound, "notebook root not found", statErr)
	}
	filePath := filepath.Join(notebookDir, filepath.FromSlash(safeSection), safePage+".md")
	if !isPathWithinRoot(filePath, notebookDir) || (!strings.HasPrefix(source, "linked:") && !isPathWithinRoot(filePath, a.vaultPath)) {
		return NewIPCError(CodeInvalidNavigationPath, "page path escapes notebook root")
	}
	info, statErr := os.Stat(filePath)
	if statErr != nil {
		if os.IsNotExist(statErr) {
			return pageActionError(CodeNavigationNotFound, "page file not found", statErr)
		}
		return pageActionError(CodeNavigationUnavailable, "page file is unavailable", statErr)
	}
	if info.IsDir() {
		return NewIPCError(CodeNavigationReveal, "page path is a directory")
	}
	if err := openNative(filePath); err != nil {
		return pageActionError(CodeNavigationReveal, "failed to reveal page", err)
	}
	return nil
}
