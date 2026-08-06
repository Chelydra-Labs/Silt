package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"silt/backend/parser"
	"silt/backend/types"
)

// PageTypeInfo describes a page's resolved note type for the type manager UI.
// IsSet is false both for an untyped page and for a page whose type ref does
// not resolve to a known schema; RawType is non-empty in the latter case so the
// UI can render a raw chip without crashing on a hand-typed value.
type PageTypeInfo struct {
	TypeID  string        `json:"typeId"`
	Type    types.TypeDef `json:"type"`
	IsSet   bool          `json:"isSet"`
	RawType string        `json:"rawType"`
}

// PagePropertyValue is one property's schema plus its current value on a typed
// page, in schema declaration order. Unset properties appear with IsSet=false
// so the UI can render the full form. Options is only populated for select/
// multiselect properties.
type PagePropertyValue struct {
	Name     string   `json:"name"`
	Label    string   `json:"label"`
	Type     string   `json:"type"`
	Value    any      `json:"value"`
	IsSet    bool     `json:"isSet"`
	Required bool     `json:"required"`
	Options  []string `json:"options,omitempty"`
}

// PageCoreMetadata is the type-independent core-fields payload every page
// exposes in the PropertiesPanel (#867), regardless of whether it has a type.
// Composed at read time from several sources:
//   - Type/Date/Aliases/Created — parsed from the page frontmatter (the
//     source of truth, consistent with GetPageType/GetPageProperties).
//   - Tags — the frontmatter `tags:` array (editable via the same path).
//   - Modified — READ-ONLY, read from the files-table mtime cache (the
//     freshest value without re-stating the file).
//
// TagsAreReadOnly is false today (frontmatter-tags is the editable path); it
// is exposed so the panel can render body-only hashtag notes when a future
// toggle changes the policy. Aliases/Created are non-empty only when present
// in frontmatter; the panel treats empty as "unset" and writes the field on
// first edit.
type PageCoreMetadata struct {
	Notebook string   `json:"notebook"`
	Section  string   `json:"section"`
	Page     string   `json:"page"`
	Type     string   `json:"type"`
	Date     string   `json:"date"`
	Tags     []string `json:"tags"`
	Aliases  []string `json:"aliases"`
	Created  string   `json:"created"`
	// Modified is an ISO 8601 (RFC3339) timestamp derived from the file's
	// last-known mtime. Empty when the file has no files-table row yet.
	Modified string `json:"modified"`
	// TagsAreReadOnly is reserved for the frontmatter-vs-body hashtag policy
	// toggle (today frontmatter tags ARE editable so this is false). The
	// panel reads it to decide whether the tag control is disabled.
	TagsAreReadOnly bool `json:"tagsAreReadOnly"`
}

// CoreFieldUpdate is the field-granular write payload for SetPageCoreMetadata.
// Each pointer is optional; a nil field means "leave unchanged". This avoids a
// wholesale overwrite (which would require the caller to read-modify-write
// the whole struct and race a concurrent sibling edit). Mirrors the surgical
// single-field edit model of SetPageProperty.
type CoreFieldUpdate struct {
	// Date, when non-nil, sets the frontmatter `date:` field (YYYY-MM-DD or
	// a normalizeable M/D/YY form). An empty string clears it.
	Date *string `json:"date,omitempty"`
	// Aliases, when non-nil, replaces the frontmatter `aliases:` array. An
	// empty/nil slice clears it.
	Aliases *[]string `json:"aliases,omitempty"`
	// Created, when non-nil, sets the frontmatter `created:` field. An
	// empty string clears it.
	Created *string `json:"created,omitempty"`
	// Tags, when non-nil, replaces the frontmatter `tags:` array. An empty/
	// nil slice clears it.
	Tags *[]string `json:"tags,omitempty"`
}

// readPageFileForTypes reads and parses a page file, returning the raw content,
// parsed metadata (with Type + Frontmatter populated), the notebook's index
// source, and the absolute file path. Mirrors FetchPageMarkdown's path
// resolution — no duplicated path logic. The caller MUST already hold a.vaultMu
// (at least RLock); this helper never acquires it, matching writePageFileLocked's
// contract, so the write-path methods can call it under their own RLock without
// a re-entrant RLock (which would deadlock against a waiting writer).
func (a *App) readPageFileForTypes(notebook, section, page string) (string, parser.FileMetadata, string, string, error) {
	safeNotebook := sanitizePathSegment(notebook)
	// validateSectionPath (not sanitizePathSegment) so a multi-segment section
	// like "Projects/Active" survives — sanitizePathSegment strips the "/",
	// flattening it to "ProjectsActive" and ENOENT'ing nested-section pages.
	safeSection, err := validateSectionPath(section, true)
	if err != nil {
		return "", parser.FileMetadata{}, "", "", err
	}
	safePage := sanitizePathSegment(page)
	if safeNotebook == "" || safePage == "" {
		return "", parser.FileMetadata{}, "", "", fmt.Errorf("invalid path metadata")
	}
	source := a.resolveSourceByName(safeNotebook)
	notebookDir, err := a.resolveNotebookDir(safeNotebook, source)
	if err != nil {
		return "", parser.FileMetadata{}, "", "", fmt.Errorf("resolve notebook dir: %w", err)
	}
	filePath := filepath.Join(notebookDir, safeSection, safePage+".md")
	if !isPathWithinRoot(filePath, notebookDir) {
		return "", parser.FileMetadata{}, "", "", fmt.Errorf("path escapes notebook root")
	}
	contentBytes, err := os.ReadFile(filePath)
	if err != nil {
		// Notebook/section/page only — never the absolute vault path. MCP
		// surfaces this string to the client; the caller already supplied
		// the logical coordinates and does not need filesystem layout.
		if os.IsNotExist(err) {
			return "", parser.FileMetadata{}, "", "", fmt.Errorf(
				"page file not found: %s/%s/%s", safeNotebook, safeSection, safePage,
			)
		}
		return "", parser.FileMetadata{}, "", "", fmt.Errorf(
			"read page file failed: %s/%s/%s", safeNotebook, safeSection, safePage,
		)
	}
	// Parse so meta.Type + meta.Frontmatter (the all-keys map) are populated;
	// these are read-only parse artifacts used for type/property resolution.
	_, meta, _, _, perr := parser.ParseFileContent(string(contentBytes), safeNotebook, safeSection, safePage, fileOrDefaultDate(filePath), a.spacesPerTab)
	if perr != nil {
		return "", parser.FileMetadata{}, "", "", fmt.Errorf(
			"parse page file failed: %s/%s/%s", safeNotebook, safeSection, safePage,
		)
	}
	return string(contentBytes), meta, source, filePath, nil
}

// resolvePageTypeSchema resolves a page's note type from its parsed metadata.
// Returns the canonical id, the schema pointer, whether the type is set AND its
// schema loaded, and the raw frontmatter value (non-empty even when the id is
// unknown, so callers can surface a raw chip).
func resolvePageTypeSchema(meta parser.FileMetadata, typesDir string) (typeID string, td *types.TypeDef, isSet bool, rawType string) {
	rawType = meta.Type
	if rawType == "" {
		return "", nil, false, ""
	}
	id, err := types.ResolveTypeID(typesDir, rawType)
	if err != nil {
		return "", nil, false, rawType
	}
	def, err := types.GetType(typesDir, id)
	if err != nil {
		return id, nil, false, rawType
	}
	return id, def, true, rawType
}

// writePageFrontmatterEdit runs the canonical lock → read → edit → atomic-write
// → re-parse → re-index → project chain for a single frontmatter field edit,
// mirroring CreatePageFromTemplate's write path. edit receives the file content
// read INSIDE the write lock (so it cannot race a concurrent writer) and
// returns the new content. The caller MUST hold a.vaultMu (at least RLock) and
// have incremented a.wg. projectPageType runs AFTER the WithDBWrite closure
// because the DB methods open their own handle and must not re-enter the write
// lock.
//
// revalidate (optional, may be nil) runs INSIDE the file lock right after the
// read, closing the schema-validation/write race: a value validated against
// schema version N under vaultMu.RLock could otherwise be written under a
// schema version N+1 hot-reloaded in between. It receives the freshly-read
// on-disk content; a non-nil error aborts the write before disk is touched.
func (a *App) writePageFrontmatterEdit(filePath, source, notebook, section, page string, revalidate func(currentContent string) error, edit func(currentContent string) (string, error)) error {
	var writeErr error
	a.coordinator.LockFileWrite(filePath, func() {
		contentBytes, err := os.ReadFile(filePath)
		if err != nil {
			if os.IsNotExist(err) {
				// Logical coords only — absolute paths must not reach MCP clients.
				writeErr = fmt.Errorf("%w: page no longer exists (moved or deleted)", ErrPageMovedOrDeleted)
				return
			}
			writeErr = fmt.Errorf("read page file failed: %s/%s/%s", notebook, section, page)
			return
		}
		if revalidate != nil {
			if verr := revalidate(string(contentBytes)); verr != nil {
				writeErr = verr
				return
			}
		}
		newContent, err := edit(string(contentBytes))
		if err != nil {
			writeErr = err
			return
		}
		a.tracker.RegisterWrite(filePath)
		writeFn := parser.WriteFileAtomic
		if a.frontmatterWriteAtomic != nil {
			writeFn = a.frontmatterWriteAtomic
		}
		if err := writeFn(filePath, []byte(newContent)); err != nil {
			writeErr = err
			return
		}
		// Past this point the on-disk write has committed. The remaining steps
		// (re-parse, atomic block+projection publish) refresh in-memory state
		// only — a failure there means the dashboard is stale until the next
		// scan, not that the file is wrong. Return success either way: the
		// staleness belongs on the types:projection-error event + log, not the
		// error return (which the IPC/MCP/frontend uniformly treat as a write
		// rejection). No rollback — the file is the source of truth.
		blocks, meta, _, _, perr := parser.ParseFileContent(newContent, notebook, section, page, fileOrDefaultDate(filePath), a.spacesPerTab)
		if perr != nil {
			a.emit(EventTypesProjectionError, map[string]string{"source": source, "page": page})
			log.Printf("types: post-write ParseFileContent failed for %s/%s/%s (file saved; index will refresh on next scan): %v", notebook, section, page, perr)
			return
		}
		// Atomic block+projection publish: a SetPageProperty edit rewrites
		// frontmatter, so the projection must move in the same tx as the
		// blocks. indexFile emits types:projection-error on failure.
		if err := a.indexFile(source, meta.Notebook, meta.Section, meta.Page, blocks, meta, meta.Warnings...); err != nil {
			log.Printf("types: indexFile failed for %s/%s/%s (file saved; index will refresh on next scan): %v", meta.Notebook, meta.Section, meta.Page, err)
			return
		}
	})
	return writeErr
}

// pageRawFrontmatter returns the page's raw parsed YAML frontmatter (all keys,
// not just schema-declared ones). It is a read-only artifact of
// parser.ParseFileContent, exposed so the MCP host can return the unfiltered
// frontmatter alongside the schema-merged property view. Unexported: not an
// IPC-bound App method, just a helper for the in-package mcpBridge.
func (a *App) pageRawFrontmatter(notebook, section, page string) (map[string]any, error) {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	a.wg.Add(1)
	defer a.wg.Done()
	return a.pageRawFrontmatterLocked(notebook, section, page)
}

// pageRawFrontmatterLocked is the lock-held core of pageRawFrontmatter. The
// caller MUST hold a.vaultMu (at least RLock) and have incremented a.wg. Used
// by mcpBridge.GetPageMetadata to read all three views under a single RLock.
func (a *App) pageRawFrontmatterLocked(notebook, section, page string) (map[string]any, error) {
	if a.vaultPath == "" || a.db == nil {
		return nil, fmt.Errorf("vault not loaded")
	}
	_, meta, _, _, err := a.readPageFileForTypes(notebook, section, page)
	if err != nil {
		return nil, err
	}
	return meta.Frontmatter, nil
}

// GetPageType returns a page's resolved note type and schema. An untyped page
// returns IsSet=false. A page whose type ref does not resolve to a known schema
// returns IsSet=false with RawType set, so the UI can render a raw chip without
// crashing on a hand-typed value.
func (a *App) GetPageType(notebook, section, page string) (PageTypeInfo, error) {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	a.wg.Add(1)
	defer a.wg.Done()
	return a.getPageTypeLocked(notebook, section, page)
}

// getPageTypeLocked is the lock-held core of GetPageType. The caller MUST hold
// a.vaultMu (at least RLock) and have incremented a.wg. Used by
// mcpBridge.GetPageMetadata to read all three views under a single RLock.
func (a *App) getPageTypeLocked(notebook, section, page string) (PageTypeInfo, error) {
	if a.vaultPath == "" || a.db == nil {
		return PageTypeInfo{}, fmt.Errorf("vault not loaded")
	}
	_, meta, _, _, err := a.readPageFileForTypes(notebook, section, page)
	if err != nil {
		return PageTypeInfo{}, err
	}
	typeID, td, isSet, rawType := resolvePageTypeSchema(meta, a.typesDir())
	if !isSet {
		return PageTypeInfo{IsSet: false, RawType: rawType}, nil
	}
	return PageTypeInfo{TypeID: typeID, Type: *td, IsSet: true, RawType: rawType}, nil
}

// GetPageProperties returns every property of a page's type with its current
// value from the frontmatter, in schema declaration order. An untyped page or
// one whose type is unknown returns an empty slice. Unset properties appear
// with IsSet=false so the UI can render the full schema form.
func (a *App) GetPageProperties(notebook, section, page string) ([]PagePropertyValue, error) {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	a.wg.Add(1)
	defer a.wg.Done()
	return a.getPagePropertiesLocked(notebook, section, page)
}

// getPagePropertiesLocked is the lock-held core of GetPageProperties. The
// caller MUST hold a.vaultMu (at least RLock) and have incremented a.wg. Used
// by mcpBridge.GetPageMetadata to read all three views under a single RLock.
func (a *App) getPagePropertiesLocked(notebook, section, page string) ([]PagePropertyValue, error) {
	if a.vaultPath == "" || a.db == nil {
		return nil, fmt.Errorf("vault not loaded")
	}
	_, meta, _, _, err := a.readPageFileForTypes(notebook, section, page)
	if err != nil {
		return nil, err
	}
	_, td, isSet, _ := resolvePageTypeSchema(meta, a.typesDir())
	if !isSet || td == nil {
		return []PagePropertyValue{}, nil
	}
	out := make([]PagePropertyValue, 0, len(td.Properties))
	for _, pdef := range td.Properties {
		raw, present := lookupFrontmatter(meta.Frontmatter, pdef.Name)
		pv := PagePropertyValue{
			Name:     pdef.Name,
			Label:    pdef.DisplayLabel(),
			Type:     string(pdef.Type),
			Required: pdef.Required,
			Options:  pdef.Options,
		}
		if present && raw != nil {
			pv.Value = raw
			pv.IsSet = true
		}
		out = append(out, pv)
	}
	return out, nil
}

// SetPageProperty writes a single typed property value into the page's
// frontmatter. The value is validated BEFORE the file is touched, so an
// invalid value never persists. Validation has two phases:
//   - structural (types.ValidateValue): the value is the right Go shape for
//     the property type (a string for text/page, a list for pages, etc).
//   - relation-target (page/pages only): each referenced page exists in the
//     index and, when the property declares a Target type, is of that type.
//
// The edit is a surgical single-line replacement (parser.SetFrontmatterField),
// so every other frontmatter line — comments, unrelated keys, exact quoting —
// survives byte-for-byte. Relation validation mirrors block_references'
// source-only-FK design: it runs only at WRITE time; a target deleted later
// stays silently inert (no back-write, no two-way linking).
func (a *App) SetPageProperty(notebook, section, page, property string, value any) error {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	a.wg.Add(1)
	defer a.wg.Done()
	if a.vaultPath == "" || a.db == nil {
		return fmt.Errorf("vault not loaded")
	}
	_, meta, source, filePath, err := a.readPageFileForTypes(notebook, section, page)
	if err != nil {
		return err
	}
	_, td, isSet, _ := resolvePageTypeSchema(meta, a.typesDir())
	if !isSet || td == nil {
		return types.ValidationError{Field: property, Message: "page has no type; assign a type before setting properties"}
	}
	// Case-insensitive schema lookup resolves a differently-cased frontmatter
	// key to the canonical property name, which is what gets written back.
	pdef, ok := td.Property(property)
	if !ok {
		return types.ValidationError{
			Field:   property,
			Message: fmt.Sprintf("unknown property %q for type %q", property, td.ID),
		}
	}
	// Structural validation BEFORE the write so an invalid value never lands on
	// disk; the atomic write alone only guarantees durability, not correctness.
	if err := types.ValidateValue(td, pdef.Name, value); err != nil {
		return err
	}
	// Relation-target validation (page/pages): confirm each referenced page
	// exists in the index and — when the property declares a Target type — is
	// of that type. Lives in the app layer (not types.ValidateValue) because it
	// needs the live SQLite index, which the index-free types package cannot
	// reach. A nil value (clearing) skips this; an empty pages list has
	// nothing to validate.
	//
	// TOCTOU: the check runs once at entry, not under the file write lock — a
	// concurrent target delete between validation and write yields a dangling
	// relation, by-design inert at query time (mirrors block_references'
	// source-only FK, ARCHITECTURE §3); target delete never writes back to
	// the source page.
	if value != nil && (pdef.Type == types.PropPage || pdef.Type == types.PropPages) {
		if err := a.validateRelationTargets(source, pdef, value); err != nil {
			return err
		}
	}
	return a.writePageFrontmatterEdit(filePath, source, meta.Notebook, meta.Section, meta.Page,
		// Re-validate against the live schema INSIDE the file lock. The earlier
		// types.ValidateValue ran under vaultMu.RLock against schema version N;
		// a hot-reload between that check and the file lock could otherwise see
		// schema N+1 (e.g. a tighter max or a removed option). Re-resolving the
		// type and re-running ValidateValue here closes the window. The value
		// itself is the SDK/binding argument and does not need re-reading.
		func(currentContent string) error {
			_, cur, _, _, perr := parser.ParseFileContent(currentContent, meta.Notebook, meta.Section, meta.Page, fileOrDefaultDate(filePath), a.spacesPerTab)
			if perr != nil {
				return fmt.Errorf("re-validate: parse current file: %w", perr)
			}
			_, curTD, curSet, _ := resolvePageTypeSchema(cur, a.typesDir())
			if !curSet || curTD == nil {
				// Typed ValidationError so MCP classifies as structured rejection.
				return types.ValidationError{Field: pdef.Name, Message: "page no longer has a resolvable type"}
			}
			curPdef, ok := curTD.Property(pdef.Name)
			if !ok {
				return types.ValidationError{
					Field:   pdef.Name,
					Message: fmt.Sprintf("property %q is no longer declared by type %q", pdef.Name, curTD.ID),
				}
			}
			if err := types.ValidateValue(curTD, curPdef.Name, value); err != nil {
				return fmt.Errorf("value no longer validates against the current schema: %w", err)
			}
			return nil
		},
		func(currentContent string) (string, error) {
			return parser.SetFrontmatterField(currentContent, pdef.Name, value)
		})
}

// validateRelationTargets checks that every page-relation target in value
// exists in the index and (when the property declares a Target type) is of
// that type. It runs after types.ValidateValue (which checks the value's
// shape) and before the frontmatter write, so a dangling or wrong-type
// relation never lands on disk.
//
// Relation targets are page references stored as path/name strings. A
// path-style ref ("Work/People/Alice") is parsed as notebook=Work,
// section=People, page=Alice; a bare name ("Alice") is resolved to the first
// indexed page with that leaf. Existence is scoped to the source of the page
// being edited. See validateOneRelationTarget for the per-target checks.
func (a *App) validateRelationTargets(source string, pdef types.PropertyDef, value any) error {
	var refs []string
	switch pdef.Type {
	case types.PropPage:
		s, _ := value.(string)
		refs = []string{s}
	case types.PropPages:
		refs, _ = toStringSlice(value)
	default:
		return nil
	}
	// Normalize the declared target type to its canonical id so a Target given
	// as a display name ("Person") matches a projection stored as the id
	// ("person"). Falls back to the raw value when resolution fails.
	wantType := pdef.Target
	if wantType != "" {
		if id, err := types.ResolveTypeID(a.typesDir(), wantType); err == nil {
			wantType = id
		}
	}
	for _, ref := range refs {
		if err := a.validateOneRelationTarget(source, pdef.Name, ref, wantType); err != nil {
			return err
		}
	}
	return nil
}

// validateOneRelationTarget checks a single relation reference against the
// index: existence first, then (when wantType is non-empty) target-type.
// wantType is the canonical id of the property's declared Target (empty = any
// page is accepted). A reference with a "/" is checked at its parsed
// (notebook, section, page); a bare name is resolved via FindPageByLeaf.
//
// Semantic rejections (target missing, wrong type) return a ValidationError
// keyed at property so the MCP tool layer classifies them as validation — the
// value was rejected before any file I/O. DB/transient errors stay plain
// wrapped errors so they classify as IO failures, not validation.
func (a *App) validateOneRelationTarget(source, property, ref, wantType string) error {
	nb, sec, page, exact := parseRelationRef(ref)
	if exact {
		ok, err := a.db.PageExists(source, nb, sec, page)
		if err != nil {
			return fmt.Errorf("validate relation target %q: %w", ref, err)
		}
		if !ok {
			return types.ValidationError{Field: property, Message: fmt.Sprintf("relation target %q does not exist", ref)}
		}
	} else {
		// Bare page name: resolve via page_fold (case-insensitive leaf match,
		// same model as wiki-link / unlinked lookup). Use the canonical page
		// spelling from the index for the subsequent projection lookup.
		rnb, rsec, rpage, ok, err := a.db.FindPageByLeaf(source, page)
		if err != nil {
			return fmt.Errorf("validate relation target %q: %w", ref, err)
		}
		if !ok {
			return types.ValidationError{Field: property, Message: fmt.Sprintf("relation target %q does not exist", ref)}
		}
		nb, sec, page = rnb, rsec, rpage
	}
	if wantType != "" {
		proj, err := a.db.GetPageProjection(source, nb, sec, page)
		if err != nil {
			return fmt.Errorf("validate relation target %q: %w", ref, err)
		}
		// proj == nil: the target page is untyped/unindexed-for-projection,
		// which fails a required-target-type check.
		if proj == nil || proj.TypeName != wantType {
			return types.ValidationError{Field: property, Message: fmt.Sprintf("relation target %q is not of type %q", ref, wantType)}
		}
	}
	return nil
}

// parseRelationRef splits a relation reference into (notebook, section, page).
// A path-style ref (contains "/") follows the scanner's path model: the first
// segment is the notebook, the last is the page, and the middle joined by "/"
// is the section. A bare name (no "/") returns exact=false so the caller
// resolves it by leaf match anywhere in the index.
func parseRelationRef(ref string) (notebook, section, page string, exact bool) {
	ref = strings.TrimSpace(ref)
	if !strings.Contains(ref, "/") {
		return "", "", ref, false
	}
	parts := strings.Split(ref, "/")
	page = parts[len(parts)-1]
	notebook = parts[0]
	if len(parts) > 2 {
		section = strings.Join(parts[1:len(parts)-1], "/")
	}
	return notebook, section, page, true
}

// SetPageType assigns (or clears) a page's note type. When assigning, every
// property in the NEW schema that is already present in the page's frontmatter
// is validated; the names that fail are returned so the UI can warn. The
// values are kept as-is — not dropped or coerced (keep-and-flag) — so no user
// data is lost across a type switch. An empty typeName clears the type and
// returns a nil mismatched list.
func (a *App) SetPageType(notebook, section, page, typeName string) ([]string, error) {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	a.wg.Add(1)
	defer a.wg.Done()
	if a.vaultPath == "" || a.db == nil {
		return nil, fmt.Errorf("vault not loaded")
	}

	// Empty/whitespace ref clears the type. No schema to consult, so there are
	// no mismatches to flag.
	if strings.TrimSpace(typeName) == "" {
		_, meta, source, filePath, err := a.readPageFileForTypes(notebook, section, page)
		if err != nil {
			return nil, err
		}
		return nil, a.writePageFrontmatterEdit(filePath, source, meta.Notebook, meta.Section, meta.Page, nil, func(currentContent string) (string, error) {
			return parser.ClearFrontmatterField(currentContent, "type")
		})
	}

	// Resolve the requested type to its canonical id + schema BEFORE reading the
	// page, so an unknown type name errors out before any file work.
	typeID, err := types.ResolveTypeID(a.typesDir(), typeName)
	if err != nil {
		return nil, types.ValidationError{
			Field:   "type",
			Message: fmt.Sprintf("unknown type %q", typeName),
		}
	}
	newTD, err := types.GetType(a.typesDir(), typeID)
	if err != nil {
		return nil, fmt.Errorf("load type %q: %w", typeID, err)
	}

	_, meta, source, filePath, err := a.readPageFileForTypes(notebook, section, page)
	if err != nil {
		return nil, err
	}

	// Keep-and-flag mismatches are computed inside the file lock against
	// freshly-read bytes (mirrors TurnIntoPage / SetPageProperty revalidate)
	// so a concurrent external edit cannot stale the advisory flagged list.
	var mismatched []string
	revalidate := func(currentContent string) error {
		mismatched = nil
		_, freshMeta, _, _, perr := parser.ParseFileContent(
			currentContent, meta.Notebook, meta.Section, meta.Page,
			fileOrDefaultDate(filePath), a.spacesPerTab,
		)
		if perr != nil {
			return nil
		}
		for _, pdef := range newTD.Properties {
			raw, present := lookupFrontmatter(freshMeta.Frontmatter, pdef.Name)
			if !present || raw == nil {
				continue
			}
			if verr := types.ValidateValue(newTD, pdef.Name, raw); verr != nil {
				mismatched = append(mismatched, pdef.Name)
			}
		}
		return nil
	}

	writeErr := a.writePageFrontmatterEdit(filePath, source, meta.Notebook, meta.Section, meta.Page, revalidate, func(currentContent string) (string, error) {
		return parser.SetFrontmatterField(currentContent, "type", typeID)
	})
	if writeErr != nil {
		return nil, writeErr
	}
	return mismatched, nil
}

// TurnIntoPage assigns (or clears) a page's note type AND deletes orphan
// frontmatter keys in a SINGLE atomic write. The multi-step UI path (clear each
// orphan via ClearPageProperty, then SetPageType) could leave values deleted
// under the old type when the final type write failed — silent data loss under
// a sync-client lock / disk-full / AV. This method is the turn-into path's
// atomic replacement: either type+orphans all land, or the file is untouched.
// keep-and-flag mismatches match SetPageType. orphanProps may be empty (plain
// switch with no clears). An empty typeName clears the type.
func (a *App) TurnIntoPage(ctx context.Context, notebook, section, page, typeName string, orphanProps []string) ([]string, error) {
	_ = ctx
	// Mirror SetPageType's RLock posture (vaultMu guards App fields; the file
	// write is serialized by LockFileWrite inside writePageFrontmatterEdit).
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	a.wg.Add(1)
	defer a.wg.Done()
	if a.vaultPath == "" || a.db == nil {
		return nil, fmt.Errorf("vault not loaded")
	}

	_, meta, source, filePath, err := a.readPageFileForTypes(notebook, section, page)
	if err != nil {
		return nil, err
	}

	// Resolve the target type (or clear). Unknown type errors before any write.
	var typeID string
	var newTD *types.TypeDef
	clearType := strings.TrimSpace(typeName) == ""
	if !clearType {
		var rerr error
		typeID, rerr = types.ResolveTypeID(a.typesDir(), typeName)
		if rerr != nil {
			return nil, fmt.Errorf("unknown type %q: %w", typeName, rerr)
		}
		var gerr error
		newTD, gerr = types.GetType(a.typesDir(), typeID)
		if gerr != nil {
			return nil, fmt.Errorf("load type %q: %w", typeID, gerr)
		}
	}

	// Keep-and-flag mismatches are computed inside the file lock against
	// freshly-read bytes (same window SetPageProperty closes via revalidate)
	// so an external edit between the pre-lock read and the write cannot
	// stale the advisory list.
	var mismatched []string
	revalidate := func(currentContent string) error {
		mismatched = nil
		if clearType || newTD == nil {
			return nil
		}
		_, freshMeta, _, _, perr := parser.ParseFileContent(
			currentContent, meta.Notebook, meta.Section, meta.Page,
			fileOrDefaultDate(filePath), a.spacesPerTab,
		)
		if perr != nil {
			// Parse failure is not a validation rejection of the type switch;
			// let the edit path surface it if needed.
			return nil
		}
		for _, pdef := range newTD.Properties {
			raw, present := lookupFrontmatter(freshMeta.Frontmatter, pdef.Name)
			if !present || raw == nil {
				continue
			}
			if verr := types.ValidateValue(newTD, pdef.Name, raw); verr != nil {
				mismatched = append(mismatched, pdef.Name)
			}
		}
		return nil
	}

	// One edit pass: type rewrite + every orphan clear. writePageFrontmatterEdit
	// only touches disk after edit returns, so a failure leaves the file byte-
	// identical (type NOT switched, orphans NOT cleared).
	writeErr := a.writePageFrontmatterEdit(filePath, source, meta.Notebook, meta.Section, meta.Page, revalidate, func(currentContent string) (string, error) {
		content := currentContent
		var e error
		if clearType {
			content, e = parser.ClearFrontmatterField(content, "type")
		} else {
			content, e = parser.SetFrontmatterField(content, "type", typeID)
		}
		if e != nil {
			return "", e
		}
		for _, prop := range orphanProps {
			prop = strings.TrimSpace(prop)
			// Never clear the type key via the orphan list — type is handled
			// above. Empty names are no-ops.
			if prop == "" || strings.EqualFold(prop, "type") {
				continue
			}
			content, e = parser.ClearFrontmatterField(content, prop)
			if e != nil {
				return "", e
			}
		}
		return content, nil
	})
	if writeErr != nil {
		return nil, writeErr
	}
	return mismatched, nil
}

// ClearPageProperty removes a single typed property value from the page's
// frontmatter. Clearing a property on an untyped page is a no-op success
// (nothing to clear). The property name must be a known field of the page's
// type; an unknown name is an error so a typo does not silently succeed.
func (a *App) ClearPageProperty(notebook, section, page, property string) error {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	a.wg.Add(1)
	defer a.wg.Done()
	if a.vaultPath == "" || a.db == nil {
		return fmt.Errorf("vault not loaded")
	}
	_, meta, source, filePath, err := a.readPageFileForTypes(notebook, section, page)
	if err != nil {
		return err
	}
	_, td, isSet, _ := resolvePageTypeSchema(meta, a.typesDir())
	if !isSet || td == nil {
		// Untyped page: clearing a property is a no-op.
		return nil
	}
	pdef, ok := td.Property(property)
	if !ok {
		return fmt.Errorf("unknown property %q for type %q", property, td.ID)
	}
	return a.writePageFrontmatterEdit(filePath, source, meta.Notebook, meta.Section, meta.Page, nil, func(currentContent string) (string, error) {
		return parser.ClearFrontmatterField(currentContent, pdef.Name)
	})
}

// GetPageCoreMetadata returns the type-independent core metadata the
// PropertiesPanel renders above the type-defined section (#867). Every page
// exposes these fields whether or not it has a type. Composed at read time
// from frontmatter (type/date/tags/aliases/created) + the files-table mtime
// cache (modified). Tags are read from the frontmatter `tags:` array (the
// editable source of truth), NOT from the block-scoped tags index — the
// frontmatter is consistent with what SetPageCoreMetadata just wrote.
//
// Lock posture mirrors GetPageType/GetPageProperties (vaultMu.RLock). The
// files-table mtime lookup is a separate DB read under the same RLock so the
// snapshot is coherent (a concurrent writer cannot interleave).
func (a *App) GetPageCoreMetadata(notebook, section, page string) (PageCoreMetadata, error) {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	a.wg.Add(1)
	defer a.wg.Done()
	return a.getPageCoreMetadataLocked(notebook, section, page)
}

// getPageCoreMetadataLocked is the lock-held core of GetPageCoreMetadata. The
// caller MUST hold a.vaultMu (at least RLock) and have incremented a.wg.
func (a *App) getPageCoreMetadataLocked(notebook, section, page string) (PageCoreMetadata, error) {
	if a.vaultPath == "" || a.db == nil {
		return PageCoreMetadata{}, fmt.Errorf("vault not loaded")
	}
	_, meta, source, filePath, err := a.readPageFileForTypes(notebook, section, page)
	if err != nil {
		return PageCoreMetadata{}, err
	}
	out := PageCoreMetadata{
		Notebook: meta.Notebook,
		Section:  meta.Section,
		Page:     meta.Page,
		Type:     meta.Type,
		Date:     meta.Date,
		Tags:     meta.Tags,
		Aliases:  meta.Aliases,
		Created:  meta.Created,
	}
	// `date`: the parser pre-fills meta.Date with the file mtime (defaultDate)
	// so the file index always has a date. The panel reads this value directly:
	// if `date:` is absent from frontmatter the user cleared it, so surfacing
	// meta.Date would re-show a synthetic date the user just removed. Mirrors
	// the same guard in computePageCoreFromMeta (the projection write path).
	if v, ok := lookupFrontmatter(meta.Frontmatter, "date"); !ok || v == nil {
		out.Date = ""
	}
	if out.Tags == nil {
		out.Tags = []string{}
	}
	if out.Aliases == nil {
		out.Aliases = []string{}
	}
	// Modified: the freshest mtime the index has seen for the file. A
	// block-only write (task status / deps) bumps file mtime without
	// touching frontmatter; the files-table row is updated by
	// MarkFileIndexed on every successful write, so this stays current
	// without re-parsing. Empty when the file has no row yet (never
	// indexed, or a fresh vault that has not finished its scan).
	if mt, merr := a.db.FileMtime(filePath); merr == nil && mt > 0 {
		out.Modified = time.Unix(0, mt).UTC().Format(time.RFC3339)
	}
	_ = source // source reserved for future scope-filter; filePath is path-keyed
	return out, nil
}

// SetPageCoreMetadata applies a field-granular update to the editable core
// fields (date / aliases / created / tags). Each non-nil field in update is
// written; nil fields are left untouched. The write follows the established
// read→validate→atomic-frontmatter-write→reparse→reindex chain
// (writePageFrontmatterEdit), so a multi-field edit lands in ONE atomic file
// write and the page_core projection is re-derived in the same transaction.
//
// `type` is owned by SetPageType (it has a keep-and-flag advisory contract the
// panel depends on); this setter intentionally does not touch it. An empty
// update (every field nil) is a no-op success.
func (a *App) SetPageCoreMetadata(notebook, section, page string, update CoreFieldUpdate) error {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	a.wg.Add(1)
	defer a.wg.Done()
	if a.vaultPath == "" || a.db == nil {
		return fmt.Errorf("vault not loaded")
	}
	if update.Date == nil && update.Aliases == nil && update.Created == nil && update.Tags == nil {
		return nil // no-op
	}
	_, meta, source, filePath, err := a.readPageFileForTypes(notebook, section, page)
	if err != nil {
		return err
	}
	return a.writePageFrontmatterEdit(filePath, source, meta.Notebook, meta.Section, meta.Page, nil, func(currentContent string) (string, error) {
		content := currentContent
		var e error
		if update.Date != nil {
			// SetFrontmatterField renders the value via yamlInline; pass the
			// normalized date. An empty string clears via ClearFrontmatterField
			// so the field round-trips to "absent" rather than `date: ""`.
			d := strings.TrimSpace(*update.Date)
			if d == "" {
				content, e = parser.ClearFrontmatterField(content, "date")
			} else {
				// Normalize M/D/YY → YYYY-MM-DD (matching the parser's read-side
				// normalization) and reject anything that isn't a real calendar
				// date, so a garbage value can't land in blocks.file_date /
				// page_core.date and corrupt date-scoped queries.
				d = parser.NormalizeDate(d)
				if _, perr := time.Parse("2006-01-02", d); perr != nil {
					return "", fmt.Errorf("invalid date %q: use YYYY-MM-DD or M/D/YYYY", *update.Date)
				}
				content, e = parser.SetFrontmatterField(content, "date", d)
			}
			if e != nil {
				return "", e
			}
		}
		if update.Aliases != nil {
			if len(*update.Aliases) == 0 {
				content, e = parser.ClearFrontmatterField(content, "aliases")
			} else {
				content, e = parser.SetFrontmatterField(content, "aliases", *update.Aliases)
			}
			if e != nil {
				return "", e
			}
		}
		if update.Created != nil {
			c := strings.TrimSpace(*update.Created)
			if c == "" {
				content, e = parser.ClearFrontmatterField(content, "created")
			} else {
				// Reject anything that isn't one of the formats the codebase
				// itself produces for created: a bare calendar date, the
				// offset-less local-timestamp form the block renderer writes
				// (parser's 2006-01-02T15:04:05), minute-precision form the
				// datetime-local input emits (2006-01-02T15:04), or full
				// RFC3339 (with Z/offset, what yaml.v3 time.Time scalars format
				// to). Mirrors the date guard above; a garbage MCP/plugin value
				// must not land in page_core.created.
				createdOK := false
				for _, lay := range []string{"2006-01-02", "2006-01-02T15:04", "2006-01-02T15:04:05", time.RFC3339} {
					if _, perr := time.Parse(lay, c); perr == nil {
						createdOK = true
						break
					}
				}
				if !createdOK {
					return "", fmt.Errorf("invalid created %q: use YYYY-MM-DD or an ISO 8601 timestamp (e.g. 2026-08-05, 2026-08-05T14:30, or 2026-08-05T14:30:00Z)", *update.Created)
				}
				content, e = parser.SetFrontmatterField(content, "created", c)
			}
			if e != nil {
				return "", e
			}
		}
		if update.Tags != nil {
			if len(*update.Tags) == 0 {
				content, e = parser.ClearFrontmatterField(content, "tags")
			} else {
				content, e = parser.SetFrontmatterField(content, "tags", *update.Tags)
			}
			if e != nil {
				return "", e
			}
		}
		return content, nil
	})
}
