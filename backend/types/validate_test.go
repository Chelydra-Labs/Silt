package types

import (
	"errors"
	"math"
	"strings"
	"testing"
)

func TestPropertyTypeValid(t *testing.T) {
	for _, pt := range KnownPropertyTypes() {
		if !pt.Valid() {
			t.Errorf("KnownPropertyTypes entry %q failed Valid()", pt)
		}
	}
	if (PropertyType("bogus")).Valid() {
		t.Error("bogus property type should be invalid")
	}
	if len(KnownPropertyTypes()) != 9 {
		t.Errorf("expected 9 property types, got %d", len(KnownPropertyTypes()))
	}
}

func TestPropertyLookup(t *testing.T) {
	td := &TypeDef{Properties: []PropertyDef{{Name: "Author"}, {Name: "ISBN"}}}
	if _, ok := td.Property("author"); !ok {
		t.Error("case-insensitive lookup failed for author")
	}
	if _, ok := td.Property("missing"); ok {
		t.Error("missing property should not be found")
	}
	if _, ok := td.Property(""); ok {
		t.Error("empty property name should not be found")
	}
}

func TestDisplayLabel(t *testing.T) {
	cases := []struct {
		def  PropertyDef
		want string
	}{
		{PropertyDef{Name: "author"}, "author"},
		{PropertyDef{Name: "author", Label: "Author Name"}, "Author Name"},
	}
	for _, c := range cases {
		if got := c.def.DisplayLabel(); got != c.want {
			t.Errorf("DisplayLabel() = %q, want %q", got, c.want)
		}
	}
}

func TestValidateTypeDef(t *testing.T) {
	min, max := 0.0, 5.0
	good := &TypeDef{
		Name:      "Book",
		HeroField: "title",
		Properties: []PropertyDef{
			{Name: "title", Type: PropText, Required: true},
			{Name: "rating", Type: PropNumber, Min: &min, Max: &max},
			{Name: "status", Type: PropSelect, Options: []string{"todo", "done"}},
			{Name: "keywords", Type: PropMultiSelect},
			{Name: "author", Type: PropPage, Target: "person"},
		},
	}
	if err := ValidateTypeDef(good); err != nil {
		t.Fatalf("good type should validate, got %v", err)
	}

	cases := []struct {
		name   string
		td     *TypeDef
		wantIn string // substring expected in the error message
	}{
		{"nil", nil, "type is nil"},
		{"missing name", &TypeDef{Name: "  "}, "name is required"},
		{"bad prop name", &TypeDef{Name: "X", Properties: []PropertyDef{{Name: "Bad Name", Type: PropText}}}, "must be lowercase"},
		{"unknown type", &TypeDef{Name: "X", Properties: []PropertyDef{{Name: "ok", Type: "bogus"}}}, "must be one of"},
		{"duplicate", &TypeDef{Name: "X", Properties: []PropertyDef{{Name: "a", Type: PropText}, {Name: "a", Type: PropText}}}, "duplicate property name"},
		{"select no options", &TypeDef{Name: "X", Properties: []PropertyDef{{Name: "s", Type: PropSelect}}}, "requires at least one option"},
		{"number min>max", &TypeDef{Name: "X", Properties: []PropertyDef{{Name: "n", Type: PropNumber, Min: &max, Max: &min}}}, "greater than max"},
		{"hero unknown", &TypeDef{Name: "X", HeroField: "nope", Properties: []PropertyDef{{Name: "a", Type: PropText}}}, "references an unknown property"},
		{"reserved name", &TypeDef{Name: "X", Properties: []PropertyDef{{Name: "type", Type: PropText}}}, "reserved core metadata field"},
		{"bad default", &TypeDef{Name: "X", Properties: []PropertyDef{{Name: "n", Type: PropNumber, Default: "not a number"}}}, "expected a number"},
		{"bad target", &TypeDef{Name: "X", Properties: []PropertyDef{{Name: "r", Type: PropPage, Target: "Bad Target!"}}}, "must be a valid type id"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateTypeDef(c.td)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", c.wantIn)
			}
			if !strings.Contains(err.Error(), c.wantIn) {
				t.Errorf("expected error containing %q, got %q", c.wantIn, err.Error())
			}
		})
	}
}

func TestValidateValue(t *testing.T) {
	td := &TypeDef{
		Name: "X",
		Properties: []PropertyDef{
			{Name: "txt", Type: PropText},
			{Name: "req", Type: PropText, Required: true},
			{Name: "num", Type: PropNumber, Min: ptrFloat(0), Max: ptrFloat(10)},
			{Name: "date", Type: PropDate},
			{Name: "dt", Type: PropDateTime},
			{Name: "chk", Type: PropCheckbox},
			{Name: "sel", Type: PropSelect, Options: []string{"a", "b"}},
			{Name: "selNoOpt", Type: PropSelect}, // no options — defense-in-depth path
			{Name: "multi", Type: PropMultiSelect, Options: []string{"x", "y"}},
			{Name: "multiFree", Type: PropMultiSelect},
			{Name: "pg", Type: PropPage},
			{Name: "pgs", Type: PropPages},
		},
	}

	cases := []struct {
		prop  string
		val   any
		valid bool
	}{
		// text
		{"txt", "hello", true},
		{"txt", 42, false},
		{"txt", nil, true}, // unset, not required
		{"txt", strings.Repeat("a", maxPropertyValueRunes), true},    // at the cap
		{"txt", strings.Repeat("a", maxPropertyValueRunes+1), false}, // over the cap
		// required
		{"req", nil, false},
		{"req", "ok", true},
		// number (yaml int + json float64)
		{"num", 5, true},
		{"num", float64(7.5), true},
		{"num", "5", false},
		{"num", -1, false}, // below min 0
		{"num", 99, false}, // above max 10
		// date
		{"date", "2026-08-01", true},
		{"date", "08/01/2026", false},
		{"date", "2024-02-30", false}, // Parse normalizes; round-trip rejects
		{"date", "2024-04-31", false},
		// datetime (bare date or RFC3339)
		{"dt", "2026-08-01", true},
		{"dt", "2026-08-01T10:00:00Z", true},
		{"dt", "2024-02-30", false},
		{"dt", "garbage", false},
		// checkbox
		{"chk", true, true},
		{"chk", "true", false},
		// select
		{"sel", "a", true},
		{"sel", "z", false},
		{"selNoOpt", "x", false}, // select with no options — constructed directly
		// multiselect
		{"multi", []any{"x", "y"}, true},
		{"multi", []any{"x", "z"}, false},
		{"multi", []any{1}, false},             // non-string element
		{"multiFree", []any{"anything"}, true}, // free-form ok
		{"multi", "x", false},                  // scalar not a list
		// page
		{"pg", "Work/Notes/Page", true},
		{"pg", "", false},
		// pages
		{"pgs", []any{"a", "b"}, true},
		// unknown property
		{"nope", "x", false},
	}
	for _, c := range cases {
		t.Run(c.prop, func(t *testing.T) {
			err := ValidateValue(td, c.prop, c.val)
			if c.valid && err != nil {
				t.Errorf("ValidateValue(%q, %v) = %v; want nil", c.prop, c.val, err)
			}
			if !c.valid && err == nil {
				t.Errorf("ValidateValue(%q, %v) = nil; want an error", c.prop, c.val)
			}
		})
	}
}

func TestValidateTypeDef_ReservedPropertyNames(t *testing.T) {
	// Full reserved set including core-metadata keys from #867/#898.
	for _, name := range []string{"notebook", "section", "page", "date", "tags", "type", "aliases", "created"} {
		t.Run(name, func(t *testing.T) {
			td := &TypeDef{Name: "X", Properties: []PropertyDef{{Name: name, Type: PropText}}}
			err := ValidateTypeDef(td)
			if err == nil {
				t.Fatalf("reserved name %q should be rejected", name)
			}
			// The message must be actionable (tell the user to rename) and name
			// the reserved field — a cryptic "is reserved" leaves an upgrading
			// vault with no fix path (PR #898 review finding #7).
			if !strings.Contains(err.Error(), "rename the property") {
				t.Errorf("reserved name %q: error %q should tell the user to rename the property", name, err.Error())
			}
		})
	}
	// A non-reserved valid name passes.
	td := &TypeDef{Name: "X", Properties: []PropertyDef{{Name: "author", Type: PropText}}}
	if err := ValidateTypeDef(td); err != nil {
		t.Errorf("non-reserved name 'author' should pass, got %v", err)
	}
}

func TestValidateValue_NumberRejectsNonFinite(t *testing.T) {
	td := &TypeDef{
		Name: "X",
		Properties: []PropertyDef{
			{Name: "num", Type: PropNumber, Min: ptrFloat(0), Max: ptrFloat(10)},
		},
	}
	for _, v := range []any{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if err := ValidateValue(td, "num", v); err == nil {
			t.Errorf("ValidateValue(num, %v) = nil; want non-finite rejection", v)
		}
	}
}

func TestValidateValue_MultiSelectRuneCap(t *testing.T) {
	// Combined runes across elements are capped, not just per-element.
	td := &TypeDef{Name: "X", Properties: []PropertyDef{{Name: "pgs", Type: PropPages}}}
	ok := []any{strings.Repeat("a", 100), strings.Repeat("b", 100)}
	if err := ValidateValue(td, "pgs", ok); err != nil {
		t.Errorf("combined length under cap should pass, got %v", err)
	}
	over := []any{strings.Repeat("a", maxPropertyValueRunes/2+1), strings.Repeat("b", maxPropertyValueRunes/2+1)}
	if err := ValidateValue(td, "pgs", over); err == nil {
		t.Error("combined length over cap should fail")
	}
}

func TestCoerceValue(t *testing.T) {
	min := 0.0
	cases := []struct {
		name string
		def  PropertyDef
		in   any
		want any
		ok   bool
	}{
		{"string->number", PropertyDef{Type: PropNumber}, "5", float64(5), true},
		{"number stays", PropertyDef{Type: PropNumber}, float64(5), float64(5), true},
		{"bad number", PropertyDef{Type: PropNumber}, "abc", nil, false},
		{"NaN string", PropertyDef{Type: PropNumber}, "NaN", nil, false},
		{"Inf string", PropertyDef{Type: PropNumber}, "Inf", nil, false},
		{"string->bool", PropertyDef{Type: PropCheckbox}, "true", true, true},
		{"bool stays", PropertyDef{Type: PropCheckbox}, false, false, true},
		{"number->text", PropertyDef{Type: PropText, Min: &min}, float64(42), "42", true},
		{"text stays", PropertyDef{Type: PropText}, "hi", "hi", true},
		{"select scalar->multi", PropertyDef{Type: PropMultiSelect}, "x", []string{"x"}, true},
		{"multi comma-split", PropertyDef{Type: PropPages}, "Alice, Bob , ,Carol", []string{"Alice", "Bob", "Carol"}, true},
		{"multi empty rejected", PropertyDef{Type: PropMultiSelect}, "  , ", nil, false},
		{"multi stays", PropertyDef{Type: PropPages}, []any{"a"}, []string{"a"}, true},
		{"nil passthrough", PropertyDef{Type: PropText}, nil, nil, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := CoerceValue(c.def, c.in)
			if c.ok && err != nil {
				t.Fatalf("CoerceValue err = %v; want nil", err)
			}
			if !c.ok && err == nil {
				t.Fatalf("CoerceValue err = nil; want an error")
			}
			if !c.ok {
				return
			}
			if !equalish(got, c.want) {
				t.Errorf("CoerceValue = %v (%T); want %v (%T)", got, got, c.want, c.want)
			}
		})
	}
}

func ptrFloat(v float64) *float64 { return &v }

// TestCoerceValue_IncompatibleReturnsValidationError pins the NB-5 contract:
// an incompatible value surfaces as a structured ValidationError (carrying the
// def name) so the MCP layer classifies it as a value rejection, not a
// transient IO error. The caller (mcpBridge.SetPageProperty) returns the error
// as-is without type-switching, so this is safe.
func TestCoerceValue_IncompatibleReturnsValidationError(t *testing.T) {
	cases := []struct {
		name string
		def  PropertyDef
		in   any
	}{
		{"string->number bogus", PropertyDef{Name: "rating", Type: PropNumber}, "abc"},
		{"string->bool maybe", PropertyDef{Name: "done", Type: PropCheckbox}, "maybe"},
		{"number->list", PropertyDef{Name: "tags", Type: PropMultiSelect}, float64(5)},
		{"slice->text", PropertyDef{Name: "title", Type: PropText}, []string{"a"}},
		{"empty->list", PropertyDef{Name: "tags", Type: PropPages}, "  , "},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := CoerceValue(c.def, c.in)
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			var vErr ValidationError
			if !errors.As(err, &vErr) {
				t.Fatalf("expected ValidationError, got %T: %v", err, err)
			}
			if vErr.Field != c.def.Name {
				t.Errorf("Field = %q, want %q", vErr.Field, c.def.Name)
			}
		})
	}
}

// equalish compares values tolerantly for the coerce tests (float vs number,
// []string vs []any).
func equalish(a, b any) bool {
	if af, ok := a.(float64); ok {
		switch bf := b.(type) {
		case float64:
			return af == bf
		case int:
			return af == float64(bf)
		}
	}
	if aslice, ok := a.([]string); ok {
		if bslice, ok := b.([]string); ok {
			if len(aslice) != len(bslice) {
				return false
			}
			for i := range aslice {
				if aslice[i] != bslice[i] {
					return false
				}
			}
			return true
		}
	}
	return a == b
}
