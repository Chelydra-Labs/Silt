package main

import (
	"fmt"
	"log"
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

func propertySortKey(pt types.PropertyType, raw any) string {
	switch pt {
	case types.PropNumber:
		if f, ok := toFloat(raw); ok {
			// Fixed-width zero-padding (sign-aware) so lexicographic order
			// matches numeric order across typical dashboard ranges.
			return fmt.Sprintf("%020.6f", f)
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
