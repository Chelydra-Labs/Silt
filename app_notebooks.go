package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"silt/backend/monitor"
	"silt/backend/parser"
)

// CreateNotebook creates a top-level notebook folder under the vault root.
func (a *App) CreateNotebook(name string) error {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	safeName := sanitizePathSegment(name)
	if safeName == "" {
		return fmt.Errorf("notebook name is required")
	}
	nbPath := filepath.Join(a.vaultPath, safeName)
	if !isPathWithinRoot(nbPath, a.vaultPath) {
		return fmt.Errorf("path escapes vault")
	}
	if _, err := os.Stat(nbPath); err == nil {
		return fmt.Errorf("notebook %q already exists", safeName)
	}
	if a.nameCollidesWithLink(safeName, "") {
		return fmt.Errorf("a linked notebook named %q already exists; unlink or rename it first", safeName)
	}
	if err := os.MkdirAll(nbPath, 0755); err != nil {
		return fmt.Errorf("failed to create notebook: %w", err)
	}
	return nil
}

// OpenNotebook registers an existing notebook folder. The folder must live
// inside the vault root (the index is rebuilt from a single watched root);
// external notebooks are rejected explicitly rather than silently linked.
// Returns the notebook name (the folder's base name).
func (a *App) OpenNotebook(folderPath string) (string, error) {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	absPath, err := filepath.Abs(folderPath)
	if err != nil {
		return "", fmt.Errorf("invalid folder path: %w", err)
	}
	if !isPathWithinRoot(absPath, a.vaultPath) {
		return "", fmt.Errorf("notebooks must live inside the Silt vault")
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return "", fmt.Errorf("folder not found: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("selected path is not a folder")
	}
	// The notebook is a top-level child of the vault root.
	rel, err := filepath.Rel(a.vaultPath, absPath)
	if err != nil {
		return "", err
	}
	relClean := filepath.ToSlash(rel)
	parts := strings.Split(relClean, "/")
	if len(parts) != 1 {
		return "", fmt.Errorf("a notebook must be a top-level folder in the vault (got %q)", relClean)
	}
	name := parts[0]
	if a.nameCollidesWithLink(name, "") {
		return "", fmt.Errorf("a linked notebook named %q already exists; unlink or rename it first", name)
	}
	return name, nil
}

// PickNotebookFolder opens the native folder picker and registers the chosen
// folder as a notebook. Returns the notebook name, or empty string if the user
// cancelled. Keeping the dialog on the Go side matches InitializeVault and
// avoids depending on frontend runtime dialog bindings.
func (a *App) PickNotebookFolder() (string, error) {
	if a.wailsApp == nil {
		return "", fmt.Errorf("application context not ready")
	}
	selectedPath, err := a.openDirectoryDialog("Open Notebook Folder")
	if err != nil {
		return "", fmt.Errorf("failed to open folder picker: %w", err)
	}
	if selectedPath == "" {
		return "", nil // user cancelled
	}
	return a.OpenNotebook(selectedPath)
}

// RevealNotebookInOS opens the notebook's on-disk folder in the OS file
// manager (vault notebook under the vault root, or a linked notebook's
// external root). Used by the sidebar context menu (#653).
func (a *App) RevealNotebookInOS(notebook string) error {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.vaultPath == "" {
		return fmt.Errorf("vault not loaded")
	}
	name := strings.TrimSpace(notebook)
	if name == "" {
		return fmt.Errorf("notebook name is required")
	}
	source := a.resolveSourceByName(name)
	dir, err := a.resolveNotebookDir(name, source)
	if err != nil {
		return err
	}
	// Vault notebooks must stay under the vault root (resolveNotebookDir
	// contract). Linked roots are external by design and fingerprint-bound.
	if !strings.HasPrefix(source, "linked:") && !isPathWithinRoot(dir, a.vaultPath) {
		return fmt.Errorf("notebook path escapes vault root")
	}
	if _, err := os.Stat(dir); err != nil {
		return fmt.Errorf("notebook folder not found: %w", err)
	}
	return openNative(dir)
}

// nameCollidesWithLink reports whether a display name is taken by a registered
// linked notebook other than excludeID (used when renaming a link in place).
// This enforces the GLOBAL name-uniqueness invariant from the VAULT side
// (CreateNotebook / OpenNotebook / RenameNotebook) that resolveSourceByName
// depends on: names must be unique across vault + linked so the name alone maps
// to one source. Without it, a vault notebook sharing a linked name makes
// resolveSourceByName route every notebook-scoped op (incl. DeletePage →
// os.Remove in place) to the external root — silent misrouting + data loss.
func (a *App) nameCollidesWithLink(name, excludeID string) bool {
	a.configMu.RLock()
	defer a.configMu.RUnlock()
	for _, ln := range a.cfg.LinkedNotebooks {
		if ln.ID != excludeID && ln.DisplayName == name {
			return true
		}
	}
	return false
}

// onReMintWarning is the watcher hook for the mass-re-mint heuristic (#443).
// When a re-parse of a previously-indexed file mints far more block ids than
// expected (signaling an external tool/sync stripped the `<!-- id: ... -->`
// comments, which silently breaks every ((uuid)) reference to those blocks),
// the watcher calls this with the details. It emits an index:re-mint-warning
// Wails event so the frontend can surface a non-blocking warning with the
// recovery path. Called from the watcher goroutine; only touches the event
// emitter (no locks).
func (a *App) onReMintWarning(w monitor.ReMintWarning) {
	a.emit(EventIndexReMintWarning, w)
}

// CreateSection creates a section folder under an explicit parent section.
// parentPath is empty for a notebook-root child; name is one new segment.
func (a *App) CreateSection(notebook, parentPath, name string) error {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	safeNotebook := sanitizePathSegment(notebook)
	safeParent, err := validateSectionPath(parentPath, true)
	if err != nil {
		return invalidNavigationPath(err)
	}
	if safeNotebook == "" {
		return fmt.Errorf("invalid notebook or parent section path")
	}
	safeName := sanitizePathSegment(name)
	if safeName == "" || strings.ContainsAny(name, "/\\") {
		return fmt.Errorf("section name must be one path segment")
	}
	notebookDir, err := a.resolveNotebookDir(safeNotebook, a.resolveSourceByName(safeNotebook))
	if err != nil {
		return err
	}
	secPath := filepath.Join(notebookDir, safeParent, safeName)
	if !isCreationPathWithinRoot(secPath, notebookDir) {
		return fmt.Errorf("path escapes notebook root")
	}
	if err := os.MkdirAll(secPath, 0755); err != nil {
		return fmt.Errorf("failed to create section: %w", err)
	}
	return nil
}

// CreatePage scaffolds the first daily note inside
// <vault>/<notebook>/[<section>/]<page>/ and indexes it, returning the date
// used. Section may be empty, in which case the page lives directly under the
// notebook. This is the streaming unit shown in the timeline editor.
func (a *App) CreatePage(notebook, section, page, dateStr string) (string, error) {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	safeNotebook := sanitizePathSegment(notebook)
	safeSection, sectionErr := validateSectionPath(section, true)
	safePage := sanitizePathSegment(page)
	if sectionErr != nil {
		return "", invalidNavigationPath(sectionErr)
	}
	if safeNotebook == "" || safePage == "" {
		return "", fmt.Errorf("notebook and page names are required (section is optional)")
	}
	safeDate := sanitizePathSegment(dateStr)
	if safeDate == "" {
		safeDate = time.Now().Format("2006-01-02")
	}

	// Resolve the notebook's root from its source (#100): vault →
	// <vault>/<notebook>, linked → the linked root. Page IS a file at
	// <root>/[<section>/]<page>.md.
	source := a.resolveSourceByName(safeNotebook)
	notebookDir, err := a.resolveNotebookDir(safeNotebook, source)
	if err != nil {
		return "", err
	}
	filePath := filepath.Join(notebookDir, safeSection, safePage+".md")
	if !isCreationPathWithinRoot(filePath, notebookDir) {
		return "", fmt.Errorf("path escapes notebook root")
	}

	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return "", fmt.Errorf("failed to create parent directory: %w", err)
	}

	if _, err := os.Stat(filePath); err == nil {
		return safeDate, nil // already exists
	}

	// Create an empty page — just frontmatter, no scaffold blocks. The user
	// starts with a blank editor; the page's date lives in the frontmatter
	// metadata, not as a visible content block.
	scaffoldFrontmatter := fmt.Sprintf("---\nnotebook: %s\nsection: %s\npage: %s\ndate: %s\ntags: []\n---\n",
		strconv.Quote(safeNotebook), strconv.Quote(safeSection), strconv.Quote(safePage), strconv.Quote(safeDate))

	a.wg.Add(1)
	defer a.wg.Done()

	var writeErr error
	a.coordinator.LockFileWrite(filePath, func() {
		if _, statErr := os.Stat(filePath); statErr == nil {
			return
		} else if !os.IsNotExist(statErr) {
			writeErr = fmt.Errorf("failed to check page target: %w", statErr)
			return
		}
		a.tracker.RegisterWrite(filePath)
		if err := parser.WriteFileAtomic(filePath, []byte(scaffoldFrontmatter)); err != nil {
			writeErr = err
			return
		}

		blocks, meta, _, _, err := parser.ParseFileContent(scaffoldFrontmatter, safeNotebook, safeSection, safePage, safeDate, a.spacesPerTab)
		if err == nil {
			var idxErr error
			a.coordinator.WithDBWrite(func() {
				idxErr = a.db.IndexFileBlocks(source, meta.Notebook, meta.Section, meta.Page, blocks, meta.Tags, meta.Warnings...)
			})
			if idxErr != nil {
				log.Printf("CreatePage: IndexFileBlocks failed for %s/%s/%s: %v", meta.Notebook, meta.Section, meta.Page, idxErr)
			}
		}
	})

	if writeErr != nil {
		return "", fmt.Errorf("failed to write scaffolded page note: %w", writeErr)
	}

	return safeDate, nil
}
