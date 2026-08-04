package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"silt/backend/db"
	"silt/backend/parser"
	"silt/backend/types"
)

// computePageProjection resolves a page's note-type id and computes its set
// of projected property rows from the live type schema. Pure: no DB access.
// Returns ("", nil) for an untyped page (caller clears the projection). A
// known schema miss falls back to a sanitized raw type id with no property
// rows, so a hand-typed `type:` value still groups the page gracefully
// (mirrors the prior projectPageType fallback).
//
// Split out of projectPageType so atomic block+projection write paths can
// pre-compute the payload before opening the DB write, then pass it into
// IndexFileWithProjection / IndexScanResultsWithProjection so blocks and
// projection share one transaction.
func (a *App) computePageProjection(meta parser.FileMetadata) (string, []db.ProjectedProperty) {
	if meta.Type == "" {
		return "", nil
	}
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
	return typeID, props
}

// computeBatchProjections returns the per-result projection payload paired
// with a scanner batch. projections[i] corresponds to results[i]; a result
// that will be skipped (res.Notebook == "" or res.Err != nil) gets a zero
// ScanProjection that IndexScanResultsWithProjection ignores. Used by the
// cold-start and linked-tree scans so the batched atomic publish covers
// blocks AND projection in one transaction.
func computeBatchProjections(a *App, results []parser.ScanResult) []db.ScanProjection {
	out := make([]db.ScanProjection, len(results))
	for i, res := range results {
		if res.Notebook == "" || res.Err != nil {
			continue
		}
		typeID, props := a.computePageProjection(parser.FileMetadata{
			Notebook:    res.Notebook,
			Section:     res.Section,
			Page:        res.Page,
			Type:        res.Type,
			Frontmatter: res.Frontmatter,
		})
		out[i] = db.ScanProjection{TypeID: typeID, Props: props}
	}
	return out
}

// projectPageType projects a page's note type and its set property values into
// the working-memory index (typed-notes feature). It is called AFTER a
// frontmatter-affecting (re)index — never inside the coordinator's WithDBWrite
// closure, because the DB methods acquire their own handle and must not re-enter
// the write lock.
//
// Projection-only path: this stays for callers that re-project a page WITHOUT
// re-indexing its blocks (external-edit re-projection via onExternalPageChanged
// and schema-triggered re-projection via reprojectAllTypedPages). Every
// frontmatter-affecting block write now routes through IndexFileWithProjection
// so the projection publish shares the block transaction.
//
// Resolution + value extraction use the live type schema (mtime-cached); the DB
// stores the result. A page whose type is empty is un-projected (cleared) so a
// page that loses its type does not linger in the dashboards.
// projectPageType returns a non-nil error only for DB failures (clear/index);
// callers that need restart-safe backfill (vault_init) must not record success
// markers when any page fails. Soft failures (unknown type → raw id) still
// return nil.
func (a *App) projectPageType(source string, meta parser.FileMetadata) error {
	if a.db == nil {
		return nil
	}
	if source == "" {
		source = "vault"
	}
	notebook, section, page := meta.Notebook, meta.Section, meta.Page
	if notebook == "" && section == "" && page == "" {
		return nil
	}

	typeID, props := a.computePageProjection(meta)
	if typeID == "" {
		// Untyped page: clear any stale projection so the dashboards drop it.
		if err := a.db.ClearPageProjection(source, notebook, section, page); err != nil {
			log.Printf("types: ClearPageProjection(%s/%s/%s/%s) failed: %v", source, notebook, section, page, err)
			a.emit(EventTypesProjectionError, map[string]string{"source": source, "page": page})
			return err
		}
		return nil
	}
	if err := a.db.IndexPageProjection(source, notebook, section, page, typeID, props); err != nil {
		log.Printf("types: IndexPageProjection(%s/%s/%s/%s) failed: %v", source, notebook, section, page, err)
		a.emit(EventTypesProjectionError, map[string]string{"source": source, "page": page})
		return err
	}
	return nil
}

// projectProperty builds one projection row from a raw frontmatter value and its
// schema definition. ValueText is the wire form for dashboard cells/filters
// (multi-values are a JSON string array so entries may contain commas);
// ValueSort is a type-correct coercion so the dashboard sorts/groups uniformly
// (numbers sign-aware padded, dates ISO, checkboxes 0/1, multi-values sorted
// JSON).
func projectProperty(pdef types.PropertyDef, raw any) db.ProjectedProperty {
	return db.ProjectedProperty{
		Property:  pdef.Name,
		ValueType: string(pdef.Type),
		ValueText: formatPropertyValue(raw),
		ValueSort: propertySortKey(pdef.Type, raw),
	}
}

// formatMultiValues encodes multiselect/pages entries as a JSON string array.
// Comma-joined text cannot round-trip an option like "a, b"; JSON can. Empty
// input yields "[]". Marshal failure falls back to a quoted single-element
// array of the joined form so the row is never silently dropped.
func formatMultiValues(parts []string) string {
	if parts == nil {
		parts = []string{}
	}
	b, err := json.Marshal(parts)
	if err != nil {
		fallback, _ := json.Marshal([]string{strings.Join(parts, ", ")})
		return string(fallback)
	}
	return string(b)
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
	case time.Time:
		// yaml.v3 decodes bare date/datetime scalars as time.Time; normalize
		// to ISO so quoted and unquoted frontmatter project identically.
		if v.Hour() == 0 && v.Minute() == 0 && v.Second() == 0 && v.Nanosecond() == 0 {
			return v.Format("2006-01-02")
		}
		return v.UTC().Format(time.RFC3339)
	case []any:
		parts := make([]string, 0, len(v))
		for _, el := range v {
			parts = append(parts, formatScalar(el))
		}
		return formatMultiValues(parts)
	case []string:
		return formatMultiValues(v)
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

// numberSortKey encodes f so byte-wise ascending order matches numeric order
// across the full float64 range (including values beyond ±1e14). Uses the
// IEEE-754 bit pattern with sign-bit transform: negatives sort before
// positives, and magnitude order is preserved within each half. Prefix "0"
// keeps all ordered values before NaN ("1").
func numberSortKey(f float64) string {
	if math.IsNaN(f) {
		return "1"
	}
	bits := math.Float64bits(f)
	if bits&(1<<63) != 0 {
		// Negative: invert all bits so more-negative sorts first.
		bits = ^bits
	} else {
		// Non-negative: flip sign bit so they sort after all negatives.
		bits |= 1 << 63
	}
	return fmt.Sprintf("0%016x", bits)
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
			// Same JSON encoding as ValueText so sort keys stay unambiguous
			// when an entry contains a comma.
			return formatMultiValues(sorted)
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
			// Only drop the projection when the file is confirmed gone. A
			// transient lock/IO error during sync must not erase the locator
			// (worklist is derived from page_types) or the page becomes
			// invisible to future schema revalidation.
			if os.IsNotExist(err) {
				if cerr := a.db.ClearPageProjection(loc.Source, loc.Notebook, loc.Section, loc.Page); cerr != nil {
					log.Printf("types: ClearPageProjection(%s/%s/%s/%s) after missing file during re-projection: %v", loc.Source, loc.Notebook, loc.Section, loc.Page, cerr)
				}
			} else {
				log.Printf("types: read %s during re-projection failed (projection kept): %v", filePath, err)
			}
			a.emit(EventTypesProjectionError, map[string]string{"source": loc.Source, "page": loc.Page})
			continue
		}
		// Re-parse so meta.Type + meta.Frontmatter reflect the current disk
		// state; the schema cache has already been invalidated upstream.
		_, meta, _, _, perr := parser.ParseFileContent(string(contentBytes), safeNotebook, safeSection, safePage, fileOrDefaultDate(filePath), a.spacesPerTab)
		if perr != nil {
			// Keep the prior projection on parse failure — the file still
			// exists and may become readable again; clearing would drop the
			// locator from the next revalidation worklist.
			log.Printf("types: parse %s during re-projection failed (projection kept): %v", filePath, perr)
			a.emit(EventTypesProjectionError, map[string]string{"source": loc.Source, "page": loc.Page})
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
		_ = a.projectPageType(loc.Source, meta)
	}
}

// onExternalPageChanged re-projects a page from its on-disk frontmatter, or
// clears the projection when the file is gone / lost its type line.
// Projection-only helper: the watcher's external-edit reindex path now
// publishes blocks AND projection atomically via the AtomicReindexHandler
// installed in initializeVaultServices, so this function is no longer in
// the watcher's hot path. Retained as a primitive for direct callers (tests
// and any future projection-only reconciliation).
// Takes vaultMu.RLock; safe because stopWatchersOutsideLock closes the monitor
// watcher OUTSIDE the teardown Lock, so this handler can drain mid-close
// instead of deadlocking.
func (a *App) onExternalPageChanged(notebook, section, page string) {
	// No a.wg.Add here: this runs on the monitor-watcher dispatch goroutine,
	// which is not itself tracked by a.wg. stopWatchersOutsideLock drains the
	// watcher before ServiceShutdown's Wait; an Add from an untracked goroutine
	// can race Wait and panic. Type-watcher onChange follows the same rule.
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
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
