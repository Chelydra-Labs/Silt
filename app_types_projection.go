package main

import (
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"silt/backend/db"
	"silt/backend/parser"
	"silt/backend/types"
)

// projectPageType projects a page's note type and its set property values into
// the working-memory index (typed-notes feature). It is called AFTER a
// frontmatter-affecting (re)index — never inside the coordinator's WithDBWrite
// closure, because the DB methods acquire their own handle and must not re-enter
// the write lock.
//
// Resolution + value extraction use the live type schema (mtime-cached); the DB
// stores the result. A page whose type is empty is un-projected (cleared) so a
// page that loses its type does not linger in the dashboards. Block-only
// mutations (task status, recurrence, dependencies) do not call this: they do
// not touch frontmatter, so the projection is unchanged.
func (a *App) projectPageType(source string, meta parser.FileMetadata) {
	if a.db == nil {
		return
	}
	if source == "" {
		source = "vault"
	}
	notebook, section, page := meta.Notebook, meta.Section, meta.Page
	if notebook == "" && section == "" && page == "" {
		return
	}

	if meta.Type == "" {
		// Untyped page: clear any stale projection so the dashboards drop it.
		if err := a.db.ClearPageProjection(source, notebook, section, page); err != nil {
			log.Printf("types: ClearPageProjection(%s/%s/%s/%s) failed: %v", source, notebook, section, page, err)
			a.emit(EventTypesProjectionError, map[string]string{"source": source, "page": page})
		}
		return
	}

	// Resolve the frontmatter type ref (id or display name) to its canonical
	// id. An unknown type is tracked under a sanitized raw name with no
	// property rows, so a hand-typed `type:` value still groups the page
	// gracefully (the UI shows a raw type chip).
	typeID, err := types.ResolveTypeID(a.typesDir(), meta.Type)
	if err != nil {
		typeID = types.TypeIDFromName(meta.Type)
	}

	var props []db.ProjectedProperty
	if td, gerr := types.GetType(a.typesDir(), typeID); gerr == nil {
		for _, pdef := range td.Properties {
			raw, present := lookupFrontmatter(meta.Frontmatter, pdef.Name)
			if !present || raw == nil {
				continue // sparse: only set values get rows
			}
			props = append(props, projectProperty(pdef, raw))
		}
	}

	if err := a.db.IndexPageProjection(source, notebook, section, page, typeID, props); err != nil {
		log.Printf("types: IndexPageProjection(%s/%s/%s/%s) failed: %v", source, notebook, section, page, err)
		a.emit(EventTypesProjectionError, map[string]string{"source": source, "page": page})
	}
}

// projectProperty builds one projection row from a raw frontmatter value and its
// schema definition. ValueText is human-readable; ValueSort is a type-correct
// coercion so the dashboard sorts/groups uniformly across property kinds
// (numbers zero-padded to be lexicographically ordered, dates left as ISO which
// sort naturally, checkboxes as 0/1, multi-values as a sorted joined set).
func projectProperty(pdef types.PropertyDef, raw any) db.ProjectedProperty {
	return db.ProjectedProperty{
		Property:  pdef.Name,
		ValueType: string(pdef.Type),
		ValueText: formatPropertyValue(raw),
		ValueSort: propertySortKey(pdef.Type, raw),
	}
}

func formatPropertyValue(raw any) string {
	switch v := raw.(type) {
	case string:
		return v
	case bool:
		if v {
			return "true"
		}
		return "false"
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(v), 'f', -1, 64)
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case []any:
		parts := make([]string, 0, len(v))
		for _, el := range v {
			parts = append(parts, formatScalar(el))
		}
		return strings.Join(parts, ", ")
	case []string:
		return strings.Join(v, ", ")
	case nil:
		return ""
	}
	return fmt.Sprintf("%v", raw)
}

func formatScalar(el any) string {
	switch v := el.(type) {
	case string:
		return v
	case bool:
		if v {
			return "true"
		}
		return "false"
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case int:
		return strconv.Itoa(v)
	}
	return fmt.Sprintf("%v", el)
}

// numberSortBias keeps negative keys non-negative inside the fixed-width field
// so lexicographic order matches numeric order. Matches the ~10^14 magnitude
// the padding was designed for (see numberSortKey).
const numberSortBias = 1e14

// numberSortKey encodes f so byte-wise ascending order matches numeric order,
// including negatives. Plain %020.6f puts the minus inside the width field, so
// "-1.2" < "-1.5" lexicographically while -1.5 < -1.2 numerically.
// Layout: '0'+biased abs for negatives, '1'+padded for non-negatives — every
// negative sorts before every non-negative, and within each half order is correct.
func numberSortKey(f float64) string {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return fmt.Sprintf("%v", f)
	}
	if f < 0 {
		return fmt.Sprintf("0%020.6f", numberSortBias+f)
	}
	return fmt.Sprintf("1%020.6f", f)
}

func propertySortKey(pt types.PropertyType, raw any) string {
	switch pt {
	case types.PropNumber:
		if f, ok := toFloat(raw); ok {
			return numberSortKey(f)
		}
		return formatPropertyValue(raw)
	case types.PropCheckbox:
		if b, ok := raw.(bool); ok {
			if b {
				return "1"
			}
			return "0"
		}
		return formatPropertyValue(raw)
	case types.PropMultiSelect, types.PropPages:
		if strs, ok := toStringSlice(raw); ok {
			sorted := append([]string(nil), strs...)
			sort.Strings(sorted)
			return strings.Join(sorted, ",")
		}
		return formatPropertyValue(raw)
	default:
		// text/date/datetime/select/page: the string value sorts naturally;
		// dates are ISO so chronological order is preserved.
		return formatPropertyValue(raw)
	}
}

// lookupFrontmatter finds a property value by name, case-insensitively, in the
// raw frontmatter map. yaml.v3 keys are lowercase-normalized by the parser's
// default, but a hand-edited key may differ in case.
func lookupFrontmatter(fm map[string]any, name string) (any, bool) {
	if fm == nil {
		return nil, false
	}
	if v, ok := fm[name]; ok {
		return v, true
	}
	lower := strings.ToLower(name)
	for k, v := range fm {
		if strings.ToLower(k) == lower {
			return v, true
		}
	}
	return nil, false
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case int32:
		return float64(n), true
	}
	return 0, false
}

func toStringSlice(v any) ([]string, bool) {
	switch s := v.(type) {
	case []string:
		return s, true
	case []any:
		out := make([]string, 0, len(s))
		for _, el := range s {
			str, ok := el.(string)
			if !ok {
				return nil, false
			}
			out = append(out, str)
		}
		return out, true
	}
	return nil, false
}

// reprojectAllTypedPages re-derives every typed page's projection from its
// frontmatter against the freshly-loaded type schema. Called from the type
// watcher's onChange handler so a schema edit (e.g. adding a property to
// book.yaml) reaches pages that have already been indexed — without it the
// dashboard would drift until each page is independently re-touched.
//
// The caller MUST hold vaultMu (RLock suffices — the body only reads
// a.db / a.vaultPath; the watcher path at vault_init.go takes Lock as a
// lifecycle handoff), and projectPageType / ClearPageProjection /
// resolveNotebookDir touch fields (a.db, a.vaultPath) guarded by vaultMu.
func (a *App) reprojectAllTypedPages() {
	if a.db == nil {
		return
	}
	locators, err := a.db.GetAllTypedPageLocators()
	if err != nil {
		log.Printf("types: GetAllTypedPageLocators failed during re-projection: %v", err)
		return
	}
	for _, loc := range locators {
		// Resolve the page's on-disk file the same way readPageFileForTypes
		// does, then re-parse just the frontmatter. A missing/unreadable file
		// (page deleted, external edit mid-scan) is skipped: the regular
		// watcher path will reconcile it on the next file event.
		safeNotebook := sanitizePathSegment(loc.Notebook)
		// validateSectionPath (not sanitizePathSegment) so a multi-segment
		// section like "Projects/Active" survives — sanitizePathSegment strips
		// the "/", flattening it to "ProjectsActive" and ENOENT'ing the file.
		safeSection, sectionErr := validateSectionPath(loc.Section, true)
		safePage := sanitizePathSegment(loc.Page)
		if sectionErr != nil {
			continue
		}
		if safeNotebook == "" || safePage == "" {
			continue
		}
		notebookDir, err := a.resolveNotebookDir(safeNotebook, loc.Source)
		if err != nil {
			continue
		}
		filePath := filepath.Join(notebookDir, safeSection, safePage+".md")
		if !isPathWithinRoot(filePath, notebookDir) {
			continue
		}
		contentBytes, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}
		// Re-parse so meta.Type + meta.Frontmatter reflect the current disk
		// state; the schema cache has already been invalidated upstream.
		_, meta, _, _, perr := parser.ParseFileContent(string(contentBytes), safeNotebook, safeSection, safePage, fileOrDefaultDate(filePath), a.spacesPerTab)
		if perr != nil {
			continue
		}
		if meta.Type == "" {
			// Page lost its type externally; drop the stale projection row.
			if err := a.db.ClearPageProjection(loc.Source, loc.Notebook, loc.Section, loc.Page); err != nil {
				log.Printf("types: ClearPageProjection(%s/%s/%s/%s) during re-projection failed: %v", loc.Source, loc.Notebook, loc.Section, loc.Page, err)
				a.emit(EventTypesProjectionError, map[string]string{"source": loc.Source, "page": loc.Page})
			}
			continue
		}
		a.projectPageType(loc.Source, meta)
	}
}

// onExternalPageChanged re-projects a page after the monitor watcher reindexes
// (or clears) it from an external edit. The watcher's IndexFileBlocks drops the
// typed projection via ClearFileBlocks, but the watcher path — unlike the App's
// write paths — does not re-project, so without this an Obsidian/sync edit would
// drop the page from type dashboards until restart. Takes vaultMu.RLock; safe
// because stopWatchersOutsideLock closes the monitor watcher OUTSIDE the
// teardown Lock, so this handler can drain mid-close instead of deadlocking.
func (a *App) onExternalPageChanged(notebook, section, page string) {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	a.wg.Add(1)
	defer a.wg.Done()
	if a.vaultPath == "" || a.db == nil {
		return
	}
	safeNotebook := sanitizePathSegment(notebook)
	safeSection, sectionErr := validateSectionPath(section, true)
	safePage := sanitizePathSegment(page)
	if safeNotebook == "" || safePage == "" || sectionErr != nil {
		return
	}
	source := a.resolveSourceByName(safeNotebook)
	_, meta, _, _, err := a.readPageFileForTypes(notebook, section, page)
	if err != nil {
		// File gone/unreadable: drop any lingering projection row (idempotent
		// with the watcher's ClearFileBlocks, but covers a read failure path).
		if cerr := a.db.ClearPageProjection(source, safeNotebook, safeSection, safePage); cerr != nil {
			log.Printf("types: ClearPageProjection on external change (%s/%s/%s/%s) failed: %v", source, safeNotebook, safeSection, safePage, cerr)
		}
		return
	}
	// projectPageType re-derives type + set properties from the freshly-parsed
	// frontmatter, or clears the projection when the type was removed.
	a.projectPageType(source, meta)
}
