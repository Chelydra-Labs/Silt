// Package types owns Silt's user-defined NOTE TYPES and their typed-property
// schemas. A note type is a named collection of typed properties (author,
// status, rating, …) declared in a YAML file under <vault>/.system/types/.
// Pages opt in by setting `type: <id>` in their frontmatter and inherit the
// type's property schema.
//
// The package is deliberately self-contained: it knows nothing about the SQLite
// index, the App, or the frontend. It loads/validates/persists type schemas and
// validates property VALUES structurally. The one check it cannot make — that a
// `page`/`pages` relation target actually exists and is of the declared target
// type — needs the live index, so it stays in the app layer (see validate.go
// ValidateValue).
//
// Structure mirrors the proven backend/templates package (loader / store /
// cache / watcher) so the two stay conceptually parallel.
package types

import "strings"

// PropertyType is the closed taxonomy of property value types Silt supports.
// Adding a value here is the ONLY change needed to extend the type system; the
// validator, loader, and UI all dispatch on this set.
type PropertyType string

const (
	// PropText is free-form text (stored as a YAML/JSON string).
	PropText PropertyType = "text"
	// PropNumber is a numeric value (YAML/JSON number).
	PropNumber PropertyType = "number"
	// PropDate is a calendar date, YYYY-MM-DD.
	PropDate PropertyType = "date"
	// PropDateTime is a date with optional time; a bare YYYY-MM-DD is accepted.
	PropDateTime PropertyType = "datetime"
	// PropCheckbox is a boolean.
	PropCheckbox PropertyType = "checkbox"
	// PropSelect is a single value drawn from a fixed option set.
	PropSelect PropertyType = "select"
	// PropMultiSelect is zero or more values from an optional option set; an
	// empty option set means free-form (tags-style).
	PropMultiSelect PropertyType = "multiselect"
	// PropPage is a relation to a single page (the def.Target names the
	// allowed target type; empty Target = any page).
	PropPage PropertyType = "page"
	// PropPages is a relation to zero or more pages.
	PropPages PropertyType = "pages"
)

// Valid reports whether p is one of the known property types.
func (p PropertyType) Valid() bool {
	switch p {
	case PropText, PropNumber, PropDate, PropDateTime, PropCheckbox,
		PropSelect, PropMultiSelect, PropPage, PropPages:
		return true
	}
	return false
}

// KnownPropertyTypes returns the full taxonomy in declaration order. The UI
// type-picker iterates this so the list has one source of truth.
func KnownPropertyTypes() []PropertyType {
	return []PropertyType{
		PropText, PropNumber, PropDate, PropDateTime, PropCheckbox,
		PropSelect, PropMultiSelect, PropPage, PropPages,
	}
}

// Cardinality bounds a relation property: a single target or many.
type Cardinality string

const (
	// CardOne is a single-value relation (the default for PropPage).
	CardOne Cardinality = "one"
	// CardMany is a multi-value relation (the default for PropPages).
	CardMany Cardinality = "many"
)

// PropertyDef describes one property of a type. Name is BOTH the frontmatter
// key under which the value is stored AND the machine identity used in queries
// and dashboards; it is therefore constrained to ^[a-z][a-z0-9_]*$ (filename-
// safe, SQL-safe, and unable to collide with the system-managed frontmatter
// keys notebook/section/page/date/tags/type/aliases/created). Label is an
// optional human label; when unset it falls back to Name.
type PropertyDef struct {
	Name        string       `yaml:"name" json:"name"`
	Label       string       `yaml:"label,omitempty" json:"label,omitempty"`
	Type        PropertyType `yaml:"type" json:"type"`
	Required    bool         `yaml:"required,omitempty" json:"required,omitempty"`
	Options     []string     `yaml:"options,omitempty" json:"options,omitempty"`
	Default     any          `yaml:"default,omitempty" json:"default,omitempty"`
	Min         *float64     `yaml:"min,omitempty" json:"min,omitempty"`
	Max         *float64     `yaml:"max,omitempty" json:"max,omitempty"`
	Target      string       `yaml:"target,omitempty" json:"target,omitempty"`
	Cardinality Cardinality  `yaml:"cardinality,omitempty" json:"cardinality,omitempty"`
	Description string       `yaml:"description,omitempty" json:"description,omitempty"`
}

// DisplayLabel returns the human label for the property: Label if set, else Name.
func (p PropertyDef) DisplayLabel() string {
	if p.Label != "" {
		return p.Label
	}
	return p.Name
}

// TypeDef is a user-defined note type with its property schema. ID is the
// machine identity (the filename stem under .system/types/); it is derived by
// the loader and never serialized into the YAML body (yaml:"-"), so a file
// rename is the canonical way to rename a type and the on-disk format stays
// purely declarative.
type TypeDef struct {
	ID          string        `yaml:"-" json:"id"`
	Name        string        `yaml:"name" json:"name"`
	Description string        `yaml:"description,omitempty" json:"description,omitempty"`
	Icon        string        `yaml:"icon,omitempty" json:"icon,omitempty"`
	HeroField   string        `yaml:"heroField,omitempty" json:"heroField,omitempty"`
	Properties  []PropertyDef `yaml:"properties,omitempty" json:"properties,omitempty"`
}

// Property looks up a property by Name, case-insensitively. Returns the def and
// true when found. Case-insensitive matching tolerates a user typing the
// property name with different casing in frontmatter while the schema stays the
// authority on the canonical form.
func (t *TypeDef) Property(name string) (PropertyDef, bool) {
	if t == nil {
		return PropertyDef{}, false
	}
	want := strings.ToLower(strings.TrimSpace(name))
	for _, p := range t.Properties {
		if strings.ToLower(p.Name) == want {
			return p, true
		}
	}
	return PropertyDef{}, false
}
