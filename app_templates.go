package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"silt/backend/parser"
	"silt/backend/templates"
	"strconv"
	"strings"
	"time"
)

// templatesDir returns the on-disk user-template directory, mirroring themesDir.
// Returns "" when no vault is open (the embedded set is still served).
func (a *App) templatesDir() string {
	if a.vaultPath == "" {
		return ""
	}
	return filepath.Join(a.vaultPath, ".system", "templates")
}

// ListTemplates enumerates available templates (on-disk user templates + the
// embedded first-class set, deduped with on-disk winning) and any per-file
// load errors. Works before a vault is open (returns just the embedded set,
// mirroring ListThemes).
func (a *App) ListTemplates() (*templates.ListTemplatesResult, error) {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	a.wg.Add(1)
	defer a.wg.Done()
	return templates.ListTemplates(a.templatesDir())
}

// GetTemplate resolves a single template by id (on-disk then embedded) and
// returns the full Template including Body. Used by the picker to render a
// live preview + drive the placeholder form. Returns a user-facing error when
// the id is on neither tier.
func (a *App) GetTemplate(id string) (templates.Template, error) {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	a.wg.Add(1)
	defer a.wg.Done()
	if id == "" {
		return templates.Template{}, fmt.Errorf("template id is required")
	}
	t, err := templates.CachedGetTemplate(a.templatesDir(), id)
	if err != nil {
		return templates.Template{}, err
	}
	return *t, nil
}

// RenderTemplate renders the template with the given id, substituting the four
// default placeholders (date/time/iso_date/weekday from the current local time)
// plus any caller-supplied vars. Smart-graph syntax ({{embed:uuid}}, ((uuid)))
// passes through untouched. Non-fatal warnings (unknown placeholders) are
// logged, not returned — Wails exposes only the first non-error return value,
// and the picker preview intentionally ignores forward-compat warnings.
func (a *App) RenderTemplate(id string, vars map[string]string) (string, error) {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	a.wg.Add(1)
	defer a.wg.Done()
	if id == "" {
		return "", fmt.Errorf("template id is required")
	}
	t, err := templates.CachedGetTemplate(a.templatesDir(), id)
	if err != nil {
		return "", err
	}
	rendered, warnings := templates.Render(t, vars, templates.RenderOptions{})
	for _, w := range warnings {
		log.Printf("templates: RenderTemplate(%q) warning: %s", id, w)
	}
	return rendered, nil
}

// SaveUserTemplate validates t, rejects any builtin:// id (read-only), and
// writes the canonical form atomically to <vault>/.system/templates/<id>.md.
// The template watcher's self-write window is armed before the write (and
// cleared on failure) so the resulting fsnotify events do not trigger a
// redundant reload. Emits templates:changed so the picker re-lists immediately.
// Mirrors App.ImportTheme.
func (a *App) SaveUserTemplate(t templates.Template) error {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	a.wg.Add(1)
	defer a.wg.Done()
	if a.vaultPath == "" {
		return fmt.Errorf("vault not loaded")
	}
	if a.templateWatcher != nil {
		a.templateWatcher.RegisterSelfWrite()
	}
	if a.tracker != nil {
		a.tracker.RegisterWrite(filepath.Join(a.templatesDir(), t.ID+".md"))
	}
	if err := templates.SaveTemplate(a.templatesDir(), &t); err != nil {
		if a.templateWatcher != nil {
			a.templateWatcher.UnregisterSelfWrite()
		}
		log.Printf("templates: SaveUserTemplate(%q) failed: %v", t.ID, err)
		return err
	}
	templates.InvalidateTemplateCache(t.ID)
	a.emit(EventTemplatesChanged, struct{}{})
	log.Printf("templates: SaveUserTemplate → saved %q", t.ID)
	return nil
}

// DeleteUserTemplate removes the on-disk user template with the given id.
// Builtin ids are rejected (read-only). Emits templates:changed. Idempotent
// (deleting an already-deleted template is a no-op success).
func (a *App) DeleteUserTemplate(id string) error {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	a.wg.Add(1)
	defer a.wg.Done()
	if a.vaultPath == "" {
		return fmt.Errorf("vault not loaded")
	}
	if a.templateWatcher != nil {
		a.templateWatcher.RegisterSelfWrite()
	}
	if a.tracker != nil {
		a.tracker.RegisterWrite(filepath.Join(a.templatesDir(), id+".md"))
	}
	if err := templates.DeleteTemplate(a.templatesDir(), id); err != nil {
		if a.templateWatcher != nil {
			a.templateWatcher.UnregisterSelfWrite()
		}
		log.Printf("templates: DeleteUserTemplate(%q) failed: %v", id, err)
		return err
	}
	templates.InvalidateTemplateCache(id)
	a.emit(EventTemplatesChanged, struct{}{})
	log.Printf("templates: DeleteUserTemplate → removed %q", id)
	return nil
}

// RegisterPluginTemplates adds a plugin's templates to the runtime registry
// (#96). Each template MUST have Source = "plugin" and PluginID = pluginID
// (the registry rejects mismatches). Emits templates:changed so the picker's
// listing refreshes immediately. The plugin tier is in-memory only — no
// disk write, no LockFileWrite, no atomic-rename.
func (a *App) RegisterPluginTemplates(pluginID string, tpls []*templates.Template) error {
	a.wg.Add(1)
	defer a.wg.Done()
	// Set Source and PluginID uniformly on each template (defensive — the
	// registry also validates). Nil elements are filtered out so the
	// registry never receives them (it rejects nil entries).
	var valid []*templates.Template
	for _, t := range tpls {
		if t == nil {
			continue
		}
		t.Source = templates.SourcePlugin
		t.PluginID = pluginID
		valid = append(valid, t)
	}
	if err := templates.RegisterPluginTemplates(pluginID, valid); err != nil {
		log.Printf("templates: RegisterPluginTemplates(%q) failed: %v", pluginID, err)
		return err
	}
	a.emit(EventTemplatesChanged, struct{}{})
	log.Printf("templates: RegisterPluginTemplates → %d templates for %q", len(valid), pluginID)
	return nil
}

// UnregisterPluginTemplates removes a plugin's templates from the runtime
// registry. Idempotent. Emits templates:changed.
func (a *App) UnregisterPluginTemplates(pluginID string) {
	a.wg.Add(1)
	defer a.wg.Done()
	templates.UnregisterPluginTemplates(pluginID)
	a.emit(EventTemplatesChanged, struct{}{})
	log.Printf("templates: UnregisterPluginTemplates → %q", pluginID)
}

// ReloadTemplates forces a re-scan of the templates directory + cache flush.
// Used by the template watcher's onChange callback (external edit detected) and
// available as a manual refresh affordance. Emits templates:changed.
func (a *App) ReloadTemplates() error {
	a.wg.Add(1)
	defer a.wg.Done()
	templates.InvalidateTemplateCache()
	a.emit(EventTemplatesChanged, struct{}{})
	return nil
}

// templateTypeBinding maps a built-in template id to the example type id and
// optional placeholder→property seeds applied only on CreatePageFromTemplate
// (new page create). Insert-at-cursor (RenderTemplateBlocks) never forces a
// type. notes / daily-note / weekly-review intentionally have no entry.
type templateTypeBinding struct {
	typeID string
	// placeholder → property name; only non-empty vars are written.
	props map[string]string
}

// templateTypeBindings is the closed map of template id → type + hero seeds.
// Keep in sync with docs/TEMPLATES.md pairing note and issue #914 ACs.
var templateTypeBindings = map[string]templateTypeBinding{
	"meeting-notes": {typeID: "meeting", props: map[string]string{"meeting_title": "topic"}},
	"reading-notes": {typeID: "book", props: map[string]string{"title": "title"}},
	"project-brief": {typeID: "project", props: map[string]string{"project_name": "title"}},
	"decision-log":  {typeID: "decision", props: map[string]string{"title": "title"}},
	"one-on-one":    {typeID: "one_on_one", props: map[string]string{"with": "with"}},
	"standup-notes": {typeID: "standup", props: map[string]string{"project_name": "project"}},
	"retrospective": {typeID: "retrospective", props: map[string]string{"title": "title"}},
}

// scaffoldTemplateFrontmatter builds the standard page frontmatter plus any
// template→type binding (type: + seeded property values from placeholders).
func scaffoldTemplateFrontmatter(notebook, section, page, date, created string, templateID string, vars map[string]string) string {
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "notebook: %s\n", strconv.Quote(notebook))
	fmt.Fprintf(&b, "section: %s\n", strconv.Quote(section))
	fmt.Fprintf(&b, "page: %s\n", strconv.Quote(page))
	fmt.Fprintf(&b, "date: %s\n", strconv.Quote(date))
	fmt.Fprintf(&b, "created: %s\n", strconv.Quote(created))
	b.WriteString("tags: []\n")
	if bind, ok := templateTypeBindings[templateID]; ok {
		fmt.Fprintf(&b, "type: %s\n", strconv.Quote(bind.typeID))
		for placeholder, prop := range bind.props {
			val := ""
			if vars != nil {
				val = strings.TrimSpace(vars[placeholder])
			}
			if val == "" {
				continue
			}
			fmt.Fprintf(&b, "%s: %s\n", prop, strconv.Quote(val))
		}
	}
	b.WriteString("---\n")
	return b.String()
}

// CreatePageFromTemplate creates a new page pre-filled with a rendered
// template's body. It composes with the existing CreatePage write path: render
// the template, prepend the standard frontmatter (SPECS §3.3), write atomically
// (temp + rename, SPECS §7.1) under the file-write lock + self-write tracker,
// and index the resulting blocks via ParseFileContent so task/embed/tag
// pipelines pick them up immediately. When the template id is in
// templateTypeBindings, the scaffold also sets type: and obvious placeholder→
// property defaults (#914). Returns the resolved date string.
func (a *App) CreatePageFromTemplate(notebook, section, page, dateStr, templateID string, vars map[string]string) (string, error) {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.vaultPath == "" || a.db == nil {
		return "", fmt.Errorf("vault not loaded")
	}
	if templateID == "" {
		return "", fmt.Errorf("template id is required")
	}
	t, err := templates.CachedGetTemplate(a.templatesDir(), templateID)
	if err != nil {
		return "", err
	}
	rendered, warnings := templates.Render(t, vars, templates.RenderOptions{})
	for _, w := range warnings {
		log.Printf("templates: CreatePageFromTemplate(%q) warning: %s", templateID, w)
	}

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

	// Resolve the notebook root from its source (#100).
	tplSource := a.resolveSourceByName(safeNotebook)
	notebookDir, err := a.resolveNotebookDir(safeNotebook, tplSource)
	if err != nil {
		return "", err
	}
	var filePath string
	if safeSection == "" {
		filePath = filepath.Join(notebookDir, safePage+".md")
	} else {
		filePath = filepath.Join(notebookDir, safeSection, safePage+".md")
	}
	if !isCreationPathWithinRoot(filePath, notebookDir) {
		return "", fmt.Errorf("path escapes notebook root")
	}
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return "", fmt.Errorf("failed to create parent directory: %w", err)
	}
	// Fast-path existence check (no lock). Re-checked under LockFileWrite so a
	// concurrent create cannot clobber between Stat and write (#652 race).
	if _, err := os.Stat(filePath); err == nil {
		return "", NewIPCError(
			CodePageExists,
			fmt.Sprintf("a page named %q already exists", safePage),
		)
	}

	// `created` stamps the page's birth time once (#867); subsequent edits
	// preserve it verbatim, matching how date/tags/type round-trip. Mirrors
	// CreatePage's scaffold so template-based pages don't carry an empty
	// created until manually edited. Mapped templates also seed type: + hero
	// properties from placeholders (#914).
	createdStr := time.Now().Format("2006-01-02T15:04:05")
	scaffoldFrontmatter := scaffoldTemplateFrontmatter(
		safeNotebook, safeSection, safePage, safeDate, createdStr, templateID, vars,
	)
	content := scaffoldFrontmatter + rendered

	a.wg.Add(1)
	defer a.wg.Done()

	var writeErr error
	a.coordinator.LockFileWrite(filePath, func() {
		if _, err := os.Stat(filePath); err == nil {
			writeErr = NewIPCError(
				CodePageExists,
				fmt.Sprintf("a page named %q already exists", safePage),
			)
			return
		}
		a.tracker.RegisterWrite(filePath)
		if err := parser.WriteFileAtomic(filePath, []byte(content)); err != nil {
			writeErr = err
			return
		}
		blocks, meta, _, _, perr := parser.ParseFileContent(content, safeNotebook, safeSection, safePage, safeDate, a.spacesPerTab)
		if perr == nil {
			// Atomic block+projection publish: a template may carry a `type:`
			// line, so the projection must land in the same tx as the blocks.
			if idxErr := a.indexFile(tplSource, meta.Notebook, meta.Section, meta.Page, blocks, meta, meta.Warnings...); idxErr != nil {
				log.Printf("CreatePageFromTemplate: indexFile failed for %s/%s/%s: %v", meta.Notebook, meta.Section, meta.Page, idxErr)
			}
		}
	})
	if writeErr != nil {
		// Preserve typed page_exists; wrap other write failures.
		if _, ok := writeErr.(*IPCError); ok {
			return "", writeErr
		}
		return "", fmt.Errorf("failed to write templated page: %w", writeErr)
	}
	return safeDate, nil
}

// RenderTemplateBlocks renders the template with the given id and parses the
// rendered Markdown into ParsedBlocks for the "insert at cursor" flow. Each
// call produces blocks with fresh UUIDs (the rendered body has no <!-- id: -->
// comments, so EnsureBlockID mints new ones), so inserting the same template
// twice never collides in the blocks-table PK. The frontend converts the
// returned blocks via blocksToDoc() → editor.commands.insertContent; the
// UniqueBlockIds extension also guards against any residual collision.
func (a *App) RenderTemplateBlocks(id string, vars map[string]string) ([]parser.ParsedBlock, error) {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	a.wg.Add(1)
	defer a.wg.Done()
	if id == "" {
		return nil, fmt.Errorf("template id is required")
	}
	t, err := templates.CachedGetTemplate(a.templatesDir(), id)
	if err != nil {
		return nil, err
	}
	rendered, warnings := templates.Render(t, vars, templates.RenderOptions{})
	for _, w := range warnings {
		log.Printf("templates: RenderTemplateBlocks(%q) warning: %s", id, w)
	}
	spaces := a.spacesPerTab
	if spaces <= 0 {
		spaces = 4
	}
	blocks, _, _, _, perr := parser.ParseFileContent(rendered, "", "", "", "", spaces)
	if perr != nil {
		return nil, fmt.Errorf("failed to parse rendered template %q: %w", id, perr)
	}
	return blocks, nil
}
