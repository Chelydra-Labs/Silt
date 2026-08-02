package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

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

// readPageFileForTypes reads and parses a page file, returning the raw content,
// parsed metadata (with Type + Frontmatter populated), the notebook's index
// source, and the absolute file path. Mirrors FetchPageMarkdown's path
// resolution — no duplicated path logic. The caller MUST already hold a.vaultMu
// (at least RLock); this helper never acquires it, matching writePageFileLocked's
// contract, so the write-path methods can call it under their own RLock without
// a re-entrant RLock (which would deadlock against a waiting writer).
func (a *App) readPageFileForTypes(notebook, section, page string) (string, parser.FileMetadata, string, string, error) {
	safeNotebook := sanitizePathSegment(notebook)
	safeSection := sanitizePathSegment(section)
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
		if os.IsNotExist(err) {
			return "", parser.FileMetadata{}, "", "", fmt.Errorf("page file not found: %s", filePath)
		}
		return "", parser.FileMetadata{}, "", "", fmt.Errorf("read page file: %w", err)
	}
	// Parse so meta.Type + meta.Frontmatter (the all-keys map) are populated;
	// these are read-only parse artifacts used for type/property resolution.
	_, meta, _, _, perr := parser.ParseFileContent(string(contentBytes), safeNotebook, safeSection, safePage, fileOrDefaultDate(filePath), a.spacesPerTab)
	if perr != nil {
		return "", parser.FileMetadata{}, "", "", fmt.Errorf("parse page file: %w", perr)
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
func (a *App) writePageFrontmatterEdit(filePath, source, notebook, section, page string, edit func(currentContent string) (string, error)) error {
	var writeErr error
	a.coordinator.LockFileWrite(filePath, func() {
		contentBytes, err := os.ReadFile(filePath)
		if err != nil {
			if os.IsNotExist(err) {
				writeErr = errPageMovedOrDeleted(filePath)
				return
			}
			writeErr = err
			return
		}
		newContent, err := edit(string(contentBytes))
		if err != nil {
			writeErr = err
			return
		}
		a.tracker.RegisterWrite(filePath)
		if err := parser.WriteFileAtomic(filePath, []byte(newContent)); err != nil {
			writeErr = err
			return
		}
		blocks, meta, _, _, perr := parser.ParseFileContent(newContent, notebook, section, page, fileOrDefaultDate(filePath), a.spacesPerTab)
		if perr != nil {
			return
		}
		var idxErr error
		a.coordinator.WithDBWrite(func() {
			idxErr = a.db.IndexFileBlocks(source, meta.Notebook, meta.Section, meta.Page, blocks, meta.Tags, meta.Warnings...)
		})
		if idxErr != nil {
			log.Printf("types: IndexFileBlocks failed for %s/%s/%s: %v", meta.Notebook, meta.Section, meta.Page, idxErr)
		}
		a.projectPageType(source, meta)
	})
	return writeErr
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
		return fmt.Errorf("page has no type; assign a type before setting properties")
	}
	// Case-insensitive schema lookup resolves a differently-cased frontmatter
	// key to the canonical property name, which is what gets written back.
	pdef, ok := td.Property(property)
	if !ok {
		return fmt.Errorf("unknown property %q for type %q", property, td.ID)
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
	if value != nil && (pdef.Type == types.PropPage || pdef.Type == types.PropPages) {
		if err := a.validateRelationTargets(source, pdef, value); err != nil {
			return err
		}
	}
	return a.writePageFrontmatterEdit(filePath, source, meta.Notebook, meta.Section, meta.Page, func(currentContent string) (string, error) {
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
		if err := a.validateOneRelationTarget(source, ref, wantType); err != nil {
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
func (a *App) validateOneRelationTarget(source, ref, wantType string) error {
	nb, sec, page, exact := parseRelationRef(ref)
	if exact {
		ok, err := a.db.PageExists(source, nb, sec, page)
		if err != nil {
			return fmt.Errorf("validate relation target %q: %w", ref, err)
		}
		if !ok {
			return fmt.Errorf("relation target %q does not exist", ref)
		}
	} else {
		// Bare page name: resolve to the first indexed page with that leaf.
		rnb, rsec, ok, err := a.db.FindPageByLeaf(source, page)
		if err != nil {
			return fmt.Errorf("validate relation target %q: %w", ref, err)
		}
		if !ok {
			return fmt.Errorf("relation target %q does not exist", ref)
		}
		nb, sec = rnb, rsec
	}
	if wantType != "" {
		proj, err := a.db.GetPageProjection(source, nb, sec, page)
		if err != nil {
			return fmt.Errorf("validate relation target %q: %w", ref, err)
		}
		// proj == nil: the target page is untyped/unindexed-for-projection,
		// which fails a required-target-type check.
		if proj == nil || proj.TypeName != wantType {
			return fmt.Errorf("relation target %q is not of type %q", ref, wantType)
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
		return nil, a.writePageFrontmatterEdit(filePath, source, meta.Notebook, meta.Section, meta.Page, func(currentContent string) (string, error) {
			return parser.ClearFrontmatterField(currentContent, "type")
		})
	}

	// Resolve the requested type to its canonical id + schema BEFORE reading the
	// page, so an unknown type name errors out before any file work.
	typeID, err := types.ResolveTypeID(a.typesDir(), typeName)
	if err != nil {
		return nil, fmt.Errorf("unknown type %q: %w", typeName, err)
	}
	newTD, err := types.GetType(a.typesDir(), typeID)
	if err != nil {
		return nil, fmt.Errorf("load type %q: %w", typeID, err)
	}

	_, meta, source, filePath, err := a.readPageFileForTypes(notebook, section, page)
	if err != nil {
		return nil, err
	}

	// Keep-and-flag: validate every currently-set property that the new schema
	// also declares. Failing names are returned for the UI to warn; the values
	// stay in frontmatter unchanged (no drop, no coerce).
	var mismatched []string
	for _, pdef := range newTD.Properties {
		raw, present := lookupFrontmatter(meta.Frontmatter, pdef.Name)
		if !present || raw == nil {
			continue
		}
		if verr := types.ValidateValue(newTD, pdef.Name, raw); verr != nil {
			mismatched = append(mismatched, pdef.Name)
		}
	}

	writeErr := a.writePageFrontmatterEdit(filePath, source, meta.Notebook, meta.Section, meta.Page, func(currentContent string) (string, error) {
		return parser.SetFrontmatterField(currentContent, "type", typeID)
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
	return a.writePageFrontmatterEdit(filePath, source, meta.Notebook, meta.Section, meta.Page, func(currentContent string) (string, error) {
		return parser.ClearFrontmatterField(currentContent, pdef.Name)
	})
}
