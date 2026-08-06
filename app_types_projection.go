package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
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
		meta := parser.FileMetadata{
			Notebook:    res.Notebook,
			Section:     res.Section,
			Page:        res.Page,
			Type:        res.Type,
			Date:        res.Date,
			Frontmatter: res.Frontmatter,
		}
		typeID, props := a.computePageProjection(meta)
		out[i] = db.ScanProjection{
			TypeID: typeID,
			Props:  props,
			Core:   computePageCoreFromMeta(meta),
		}
	}
	return out
}

// computePageCore derives a page's type-independent core-metadata projection
// payload (#867) from parsed frontmatter. Pure: no DB access. The App layer
// is the source of truth for these values; the indexer inserts the result
// atomically with blocks + page_types/page_properties. type/date/aliases/
// created come from the parser-populated FileMetadata fields (round-tripped
// verbatim with the rest of the frontmatter).
func (a *App) computePageCore(meta parser.FileMetadata) db.PageCoreFields {
	return computePageCoreFromMeta(meta)
}

// computePageCoreFromMeta is the pure core of computePageCore, shared with
// computeBatchProjections (which synthesizes a FileMetadata from a ScanResult
// and has no App receiver). type/date round-trip through the parser; aliases
// is read from the raw frontmatter map when the typed field is empty (handles
// a typed-decode failure that skips Aliases but leaves it in Frontmatter).
func computePageCoreFromMeta(meta parser.FileMetadata) db.PageCoreFields {
	core := db.PageCoreFields{
		// Type deliberately holds the RAW frontmatter ref (e.g. "meeting"), not
		// the canonicalized page_types.type_name id. No consumer joins the two
		// today; the canonicalization decision (raw ref vs id) is deferred until
		// the first join-consumer lands, at which point both sides must agree.
		Type:    meta.Type,
		Date:    meta.Date,
		Aliases: meta.Aliases,
		Created: meta.Created,
	}
	if core.Aliases == nil && meta.Frontmatter != nil {
		if v, ok := lookupFrontmatter(meta.Frontmatter, "aliases"); ok && v != nil {
			if s, ok := toStringSlice(v); ok {
				core.Aliases = s
			} else if str, ok := v.(string); ok && str != "" {
				// Tolerate a hand-authored scalar `aliases: foo` as a one-element
				// list. The typed decode into []string fails on a scalar, so
				// without this the value would be silently dropped from the
				// projection (and then from the panel) even though frontmatter
				// holds it — and a panel save would clear it. Interop with
				// Obsidian / hand-edited YAML.
				core.Aliases = []string{str}
			}
		}
	}
	// `created` is recovered from the raw frontmatter when the typed field is
	// empty — the batch ingest path synthesizes a FileMetadata from a ScanResult
	// that carries frontmatter but may not have populated Created, and a
	// hand-authored bare created: survives in Frontmatter.
	if core.Created == "" && meta.Frontmatter != nil {
		if v, ok := lookupFrontmatter(meta.Frontmatter, "created"); ok {
			if str, ok := v.(string); ok && str != "" {
				core.Created = str
			}
		}
	}
	// `date` is recovered from the raw frontmatter when the typed field is
	// empty — mirrors the created/aliases fallbacks. The vault and linked
	// scanners set meta.Date from the parser, but a hand-built ScanResult
	// (or a future caller that skips the parser) would otherwise project an
	// empty core.date even when frontmatter carries one. An unquoted
	// `date: 2026-08-05` survives in Frontmatter as a time.Time, so handle
	// both shapes (NormalizeDate takes the string form).
	if core.Date == "" && meta.Frontmatter != nil {
		if v, ok := lookupFrontmatter(meta.Frontmatter, "date"); ok {
			switch d := v.(type) {
			case string:
				if d != "" {
					core.Date = parser.NormalizeDate(d)
				}
			case time.Time:
				core.Date = d.Format("2006-01-02")
			}
		}
	}
	return core
}

// projectPageType projects a page's note type and its set property values into
// the working-memory index (typed-notes feature). It is called AFTER a
// frontmatter-affecting (re)index — never inside the coordinator's WithDBWrite
// closure, because the DB methods acquire their own handle and must not re-enter
// the write lock.
//
// Projection-only path: this stays for callers that re-project a page WITHOUT
// re-indexing its blocks (the projectionReprojectWorker's per-locator step).
// Every frontmatter-affecting block write now routes through IndexFileWithProjection
// so the projection publish shares the block transaction.
//
// Note: this path touches ONLY page_types / page_properties (schema-derived).
// It deliberately does NOT update page_core: page_core's fields (type, date,
// aliases, created) are frontmatter-derived, not schema-derived, so a type-
// definition rename / hot-reload that triggers this re-projection cannot change
// page_core. page_core is republished only via the frontmatter-affecting block
// index path (IndexFileWithProjection / IndexScanResultsWithProjection).
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

// projectPageCore writes a page's type-independent core-metadata row to
// page_core. Used by the warm-upgrade backfill (initializeVaultServices) for
// pages whose blocks were warm-skipped on restart and thus never entered the
// unified IndexFileWithProjection path that would otherwise publish page_core.
// Unlike projectPageType, this writes a row for EVERY page (typed OR untyped) —
// page_core's whole point is the untyped case. Reproducible from frontmatter
// (cardinal rule 4). No event emission on failure; the caller aggregates
// backfillFailed so a partial backfill retries next open.
func (a *App) projectPageCore(source string, meta parser.FileMetadata) error {
	if a.db == nil {
		return nil
	}
	if source == "" {
		source = "vault"
	}
	if meta.Notebook == "" && meta.Section == "" && meta.Page == "" {
		return nil
	}
	core := computePageCoreFromMeta(meta)
	return a.db.IndexPageCore(source, meta.Notebook, meta.Section, meta.Page, core)
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

// reprojectAllTypedPages was the synchronous full-vault reprojection that
// every schema-edit caller (SaveType / DeleteType / ReloadTypes / the type
// watcher / RestoreExampleTypes) used to invoke under vaultMu. Phase 5 / #866
// replaced those callers with enqueueReprojection, which routes through the
// scoped coalescing worker in app_types_worker.go — disk reads + DB writes
// happen outside the lifecycle lock, the worker re-fetches schema per
// iteration, and the scoped GetTypedPageLocatorsByIDs query keeps the work
// proportional to the affected pages. The body lives on as
// projectionReprojectWorker.reprojectOneLocator (single-locator step).
