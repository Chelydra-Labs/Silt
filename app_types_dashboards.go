package main

import (
	"fmt"
	"sort"
	"strings"

	"silt/backend/db"
)

// TypeDashboardProp is one property value of a dashboard row. The internal sort
// key (ValueSort) is intentionally dropped here — it is a coercion detail owned
// by the projection layer, and the frontend only renders the human value + type.
type TypeDashboardProp struct {
	Name      string `json:"name"`
	ValueText string `json:"valueText"`
	ValueType string `json:"valueType"`
}

// TypeDashboardRow is one page of the queried type with the property values the
// dashboard renders. Grouping is the frontend's job; this is the flat
// filtered+sorted list the IPC returns.
type TypeDashboardRow struct {
	Source     string              `json:"source"`
	Notebook   string              `json:"notebook"`
	Section    string              `json:"section"`
	Page       string              `json:"page"`
	Properties []TypeDashboardProp `json:"properties"`
}

// QueryPagesByType returns the pages of the given type, filtered and sorted for
// the per-type dashboard. filter maps a property name to a required value (a row
// is kept when: for scalar properties the ValueText equals the filter value; for
// multiselect/pages the ValueText contains the value as one of its comma-joined
// entries). sortProperty orders by the property's ValueSort (numeric/date/text
// coercion already computed at projection time); empty sortProperty sorts by page
// path (notebook, section, page). sortDesc reverses. Grouping is done in the
// frontend; this method returns a flat filtered+sorted list.
func (a *App) QueryPagesByType(typeName string, filter map[string]string, sortProperty string, sortDesc bool) ([]TypeDashboardRow, error) {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	a.wg.Add(1)
	defer a.wg.Done()
	if a.vaultPath == "" || a.db == nil {
		return nil, fmt.Errorf("vault not loaded")
	}

	rows, err := a.db.QueryPagesByType(typeName)
	if err != nil {
		return nil, err
	}

	filtered := make([]db.PageProjectionRow, 0, len(rows))
	for _, row := range rows {
		if rowMatchesDashboardFilter(row, filter) {
			filtered = append(filtered, row)
		}
	}

	// SliceStable so rows that compare equal on the sort key (including the
	// unset-as-empty case) keep a deterministic relative order; the explicit
	// page-path tiebreak makes the order independent of the DB's row ordering.
	sort.SliceStable(filtered, func(i, j int) bool {
		if sortDesc {
			return dashboardLess(filtered[j], filtered[i], sortProperty)
		}
		return dashboardLess(filtered[i], filtered[j], sortProperty)
	})

	out := make([]TypeDashboardRow, 0, len(filtered))
	for _, row := range filtered {
		props := make([]TypeDashboardProp, 0, len(row.Properties))
		for _, p := range row.Properties {
			props = append(props, TypeDashboardProp{
				Name:      p.Property,
				ValueText: p.ValueText,
				ValueType: p.ValueType,
			})
		}
		out = append(out, TypeDashboardRow{
			Source:     row.Source,
			Notebook:   row.Notebook,
			Section:    row.Section,
			Page:       row.Page,
			Properties: props,
		})
	}
	return out, nil
}

// rowMatchesDashboardFilter reports whether a projection row satisfies every
// filter entry. A property absent from the row (or present with an empty value)
// is treated as unset: it matches an empty-string filter value, and never
// matches a non-empty one. Multiselect/pages values are tokenized on the ", "
// join the projection layer writes, so a filter selects any row carrying the
// value as one of its entries.
func rowMatchesDashboardFilter(row db.PageProjectionRow, filter map[string]string) bool {
	for name, want := range filter {
		valueText, valueType := "", ""
		for _, p := range row.Properties {
			if p.Property == name {
				valueText = p.ValueText
				valueType = p.ValueType
				break
			}
		}
		if want == "" {
			// Empty filter selects unset/empty values only.
			if valueText != "" {
				return false
			}
			continue
		}
		// Non-empty filter never matches an unset property.
		if valueText == "" {
			return false
		}
		if isMultiValueType(valueType) {
			if !dashboardSliceContains(splitDashboardMultiValues(valueText), want) {
				return false
			}
		} else if valueText != want {
			return false
		}
	}
	return true
}

// dashboardLess is the row comparator: primary key is the sort property's
// ValueSort (unset property → empty string, so unset sorts before set); equal
// keys fall back to page path for a stable total order.
func dashboardLess(a, b db.PageProjectionRow, sortProperty string) bool {
	ka := dashboardSortKey(a, sortProperty)
	kb := dashboardSortKey(b, sortProperty)
	if ka != kb {
		return ka < kb
	}
	return dashboardPathKey(a) < dashboardPathKey(b)
}

func dashboardSortKey(row db.PageProjectionRow, sortProperty string) string {
	if sortProperty == "" {
		return ""
	}
	for _, p := range row.Properties {
		if p.Property == sortProperty {
			return p.ValueSort
		}
	}
	return ""
}

func dashboardPathKey(row db.PageProjectionRow) string {
	return row.Notebook + "\x00" + row.Section + "\x00" + row.Page
}

// isMultiValueType reports whether a value type stores multiple entries in its
// ValueText (comma-joined), so the filter applies contains-semantics rather
// than equality.
func isMultiValueType(valueType string) bool {
	return valueType == "multiselect" || valueType == "pages"
}

// splitDashboardMultiValues tokenizes a comma-joined multi-value. The
// projection layer joins on ", "; trailing/empty tokens are skipped so a value
// of "a, b" yields ["a", "b"].
func splitDashboardMultiValues(valueText string) []string {
	parts := strings.Split(valueText, ", ")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func dashboardSliceContains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
