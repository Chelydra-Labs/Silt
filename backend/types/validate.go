package types

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// maxPropertyValueRunes bounds text/page values and the combined runes of a
// multi-value property. Mirrors the MCP tool's MaxBlockTextRunes ceiling so a
// plugin-driven runaway value cannot bloat the SQLite index beyond what a
// single tool call could already write.
const maxPropertyValueRunes = 32000

var (
	// typeIDRe bounds a type id to filename-safe [a-z0-9_-]+. The id flows
	// into filepath.Join(typesDir, id+".yaml") on the write path, so it must
	// reject path separators, parent dirs, and NUL — CWE-22-safe on every
	// platform we ship. Mirrors templates.idRe.
	typeIDRe = regexp.MustCompile(`^[a-z0-9_-]+$`)
	// propNameRe bounds a property name to ^[a-z][a-z0-9_]*$ so it is a safe
	// frontmatter key, a safe SQL identifier, and free of smart-graph syntax
	// (colons, capitals, parentheses). Mirrors the template placeholder
	// grammar. The reserved-key check (notebook/section/page/date/tags/type)
	// is enforced in ValidateTypeDef, not the regex.
	propNameRe = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

	// reservedPropertyNames are frontmatter keys Silt owns (page identity, tag
	// extraction, locator fields). A property sharing one would be
	// indistinguishable from the system value at read time, so ValidateTypeDef
	// rejects it. Lowercase since propNameRe already forces lowercase.
	reservedPropertyNames = map[string]bool{
		"notebook": true,
		"section":  true,
		"page":     true,
		"date":     true,
		"tags":     true,
		"type":     true,
	}
)

// IsValidTypeID reports whether id is a safe type id (non-empty, [a-z0-9_-]+).
// Used by the write path to reject path-escape attempts before the id reaches
// filepath.Join.
func IsValidTypeID(id string) bool {
	return typeIDRe.MatchString(id)
}

// IsValidPropertyName reports whether name is a valid property name
// (^[a-z][a-z0-9_]*$), i.e. a safe frontmatter key.
func IsValidPropertyName(name string) bool {
	return propNameRe.MatchString(name)
}

// ValidationError describes a single type-schema problem in machine-readable
// form so the UI can surface "type X: property Y is invalid" without crashing
// on a bad file. Mirrors templates.ValidationError.
type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func (v ValidationError) Error() string {
	return fmt.Sprintf("type validation error at %s: %s", v.Field, v.Message)
}

// ValidationErrors aggregates per-field issues so a caller gets every problem in
// one pass instead of fixing them one at a time. The loader wraps these into a
// single error; SaveType propagates them over IPC so the type editor can name
// the offending field. Mirrors templates.ValidationErrors.
type ValidationErrors []ValidationError

func (ve ValidationErrors) Error() string {
	if len(ve) == 0 {
		return ""
	}
	msgs := make([]string, 0, len(ve))
	for _, e := range ve {
		msgs = append(msgs, e.Error())
	}
	return strings.Join(msgs, "; ")
}

// ValidateTypeDef checks a parsed type against the canonical schema, returning
// nil if well-formed or a ValidationErrors slice listing every structural
// problem. Collects all issues rather than failing fast so the editor can show
// the complete set at once.
func ValidateTypeDef(td *TypeDef) error {
	var errs ValidationErrors

	if td == nil {
		return ValidationErrors{{Field: "$", Message: "type is nil"}}
	}

	if strings.TrimSpace(td.Name) == "" {
		errs = append(errs, ValidationError{Field: "name", Message: "name is required"})
	}

	seen := map[string]bool{}
	for i, p := range td.Properties {
		prefix := fmt.Sprintf("properties[%d]", i)
		switch {
		case strings.TrimSpace(p.Name) == "":
			errs = append(errs, ValidationError{Field: prefix + ".name", Message: "property name is required"})
		case !propNameRe.MatchString(p.Name):
			errs = append(errs, ValidationError{
				Field:   prefix + ".name",
				Message: fmt.Sprintf("property name %q must be lowercase (^[a-z][a-z0-9_]*$) so it is a safe frontmatter key", p.Name),
			})
		case reservedPropertyNames[strings.ToLower(p.Name)]:
			errs = append(errs, ValidationError{
				Field:   prefix + ".name",
				Message: fmt.Sprintf("property name %q is reserved (collides with a system-managed frontmatter key)", p.Name),
			})
		case seen[strings.ToLower(p.Name)]:
			errs = append(errs, ValidationError{
				Field:   prefix + ".name",
				Message: fmt.Sprintf("duplicate property name %q", p.Name),
			})
		default:
			seen[strings.ToLower(p.Name)] = true
		}

		if !p.Type.Valid() {
			errs = append(errs, ValidationError{
				Field:   prefix + ".type",
				Message: fmt.Sprintf("property %q type %q must be one of %v", p.Name, p.Type, knownTypeList()),
			})
		}

		switch p.Type {
		case PropSelect:
			if len(p.Options) == 0 {
				errs = append(errs, ValidationError{
					Field:   prefix + ".options",
					Message: fmt.Sprintf("select property %q requires at least one option", p.Name),
				})
			}
		case PropNumber:
			if p.Min != nil && p.Max != nil && *p.Min > *p.Max {
				errs = append(errs, ValidationError{
					Field:   prefix + ".min",
					Message: fmt.Sprintf("number property %q has min (%v) greater than max (%v)", p.Name, *p.Min, *p.Max),
				})
			}
		}

		// Reuse ValidateValue on a synthetic single-property TypeDef so the
		// default's structural checks (type, options, min/max) live in one
		// place. A non-nil default never trips the Required-missing branch.
		if p.Default != nil {
			if err := ValidateValue(&TypeDef{Properties: []PropertyDef{p}}, p.Name, p.Default); err != nil {
				errs = append(errs, ValidationError{Field: prefix + ".default", Message: err.Error()})
			}
		}

		// Target shape check only; target TYPE existence needs the live index,
		// which this package does not touch. Catches typos and malformed ids.
		if (p.Type == PropPage || p.Type == PropPages) && p.Target != "" {
			if !IsValidTypeID(p.Target) {
				errs = append(errs, ValidationError{
					Field:   prefix + ".target",
					Message: fmt.Sprintf("relation target %q must be a valid type id ([a-z0-9_-]+)", p.Target),
				})
			}
		}
	}

	if td.HeroField != "" {
		if _, ok := td.Property(td.HeroField); !ok {
			errs = append(errs, ValidationError{
				Field:   "heroField",
				Message: fmt.Sprintf("heroField %q references an unknown property", td.HeroField),
			})
		}
	}

	if len(errs) == 0 {
		return nil
	}
	return errs
}

func knownTypeList() []string {
	out := make([]string, 0, 9)
	for _, t := range KnownPropertyTypes() {
		out = append(out, string(t))
	}
	return out
}

// ValidateValue structurally validates a single property value against its def
// in the given type. It returns nil for a valid value. A nil value is valid
// unless the property is Required (nil means "unset").
//
// A non-nil but empty value (e.g. "" for text, [] for multiselect) is treated
// as SET, not unset, so Required only rejects a truly missing (nil) value.
//
// Relation target EXISTENCE and target-TYPE checks are intentionally NOT done
// here: they need the live SQLite index (does the target page exist, and is it
// of def.Target?). The app layer performs that check after this structural
// validation passes, so this package stays index-free.
func ValidateValue(td *TypeDef, propName string, value any) error {
	def, ok := td.Property(propName)
	if !ok {
		return ValidationError{Field: propName, Message: "unknown property"}
	}

	// nil means "unset": valid unless the property is Required.
	if value == nil {
		if def.Required {
			return ValidationError{Field: propName, Message: "required property is missing"}
		}
		return nil
	}

	switch def.Type {
	case PropText, PropPage:
		s, ok := value.(string)
		if !ok {
			return ValidationError{Field: propName, Message: fmt.Sprintf("expected text, got %T", value)}
		}
		if def.Type == PropPage && strings.TrimSpace(s) == "" {
			return ValidationError{Field: propName, Message: "page relation must not be empty"}
		}
		if rs := len([]rune(s)); rs > maxPropertyValueRunes {
			return ValidationError{Field: propName, Message: fmt.Sprintf("value length %d exceeds the %d-rune limit", rs, maxPropertyValueRunes)}
		}
		return nil

	case PropDate:
		s, ok := value.(string)
		if !ok {
			return ValidationError{Field: propName, Message: fmt.Sprintf("expected a date string (YYYY-MM-DD), got %T", value)}
		}
		if _, err := time.Parse("2006-01-02", s); err != nil {
			return ValidationError{Field: propName, Message: fmt.Sprintf("%q is not a valid date (YYYY-MM-DD)", s)}
		}
		return nil

	case PropDateTime:
		s, ok := value.(string)
		if !ok {
			return ValidationError{Field: propName, Message: fmt.Sprintf("expected a datetime string, got %T", value)}
		}
		// A bare date is an acceptable datetime; otherwise require RFC3339.
		if _, err := time.Parse("2006-01-02", s); err == nil {
			return nil
		}
		if _, err := time.Parse(time.RFC3339, s); err != nil {
			return ValidationError{Field: propName, Message: fmt.Sprintf("%q is not a valid datetime (YYYY-MM-DD or RFC3339)", s)}
		}
		return nil

	case PropSelect:
		s, ok := value.(string)
		if !ok {
			return ValidationError{Field: propName, Message: fmt.Sprintf("expected a select value (string), got %T", value)}
		}
		// ValidateTypeDef enforces non-empty options; this guards PropertyDefs
		// built without going through it (tests, SDK, plugins) so a select
		// with no options cannot silently match any value.
		if len(def.Options) == 0 {
			return ValidationError{Field: propName, Message: "select property has no allowed options"}
		}
		if !containsString(def.Options, s) {
			return ValidationError{Field: propName, Message: fmt.Sprintf("%q is not one of the allowed options %v", s, def.Options)}
		}
		return nil

	case PropCheckbox:
		if _, ok := value.(bool); !ok {
			return ValidationError{Field: propName, Message: fmt.Sprintf("expected a boolean, got %T", value)}
		}
		return nil

	case PropNumber:
		f, ok := asFloat(value)
		if !ok {
			return ValidationError{Field: propName, Message: fmt.Sprintf("expected a number, got %T", value)}
		}
		if def.Min != nil && f < *def.Min {
			return ValidationError{Field: propName, Message: fmt.Sprintf("%v is below the minimum %v", f, *def.Min)}
		}
		if def.Max != nil && f > *def.Max {
			return ValidationError{Field: propName, Message: fmt.Sprintf("%v is above the maximum %v", f, *def.Max)}
		}
		return nil

	case PropMultiSelect, PropPages:
		strs, ok := asStringSlice(value)
		if !ok {
			return ValidationError{Field: propName, Message: fmt.Sprintf("expected a list, got %T", value)}
		}
		if def.Type == PropMultiSelect && len(def.Options) > 0 {
			for _, s := range strs {
				if !containsString(def.Options, s) {
					return ValidationError{Field: propName, Message: fmt.Sprintf("%q is not one of the allowed options %v", s, def.Options)}
				}
			}
		}
		// Cap the combined runes across elements to bound the stored value.
		total := 0
		for _, s := range strs {
			total += len([]rune(s))
		}
		if total > maxPropertyValueRunes {
			return ValidationError{Field: propName, Message: fmt.Sprintf("combined value length %d exceeds the %d-rune limit", total, maxPropertyValueRunes)}
		}
		return nil
	}

	// Unreachable: def.Type.Valid() is implied by td.Property having matched a
	// real property, but guard defensively against a future addition that
	// forgets a case here.
	return ValidationError{Field: propName, Message: fmt.Sprintf("unsupported property type %q", def.Type)}
}

// CoerceValue best-effort coerces value into the shape required by def, for the
// "Turn into" type-conversion flow where an existing value must be re-fit to a
// new property type. It returns the coerced value, or the original when it is
// already the right Go type, or an error when the value is fundamentally
// incompatible. Callers should keep the original on error (Capacities-style
// "keep and flag") rather than dropping user data.
func CoerceValue(def PropertyDef, value any) (any, error) {
	if value == nil {
		return nil, nil
	}
	switch def.Type {
	case PropNumber:
		if f, ok := asFloat(value); ok {
			return f, nil
		}
		if s, ok := value.(string); ok {
			if f, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil {
				return f, nil
			}
		}
		// ValidationError (not a plain error) so the MCP layer classifies a
		// bogus value as a structured rejection rather than a transient IO error.
		return nil, ValidationError{Field: def.Name, Message: fmt.Sprintf("cannot coerce %v (%T) to a number", value, value)}
	case PropCheckbox:
		if b, ok := value.(bool); ok {
			return b, nil
		}
		if s, ok := value.(string); ok {
			switch strings.ToLower(strings.TrimSpace(s)) {
			case "true", "yes", "1":
				return true, nil
			case "false", "no", "0":
				return false, nil
			}
		}
		return nil, ValidationError{Field: def.Name, Message: fmt.Sprintf("cannot coerce %v (%T) to a boolean", value, value)}
	case PropText, PropDate, PropDateTime, PropSelect, PropPage:
		if s, ok := value.(string); ok {
			return s, nil
		}
		if f, ok := asFloat(value); ok {
			// A number moving to a text-like field renders without trailing zeros.
			return strconv.FormatFloat(f, 'f', -1, 64), nil
		}
		return nil, ValidationError{Field: def.Name, Message: fmt.Sprintf("cannot coerce %v (%T) to text", value, value)}
	case PropMultiSelect, PropPages:
		if strs, ok := asStringSlice(value); ok {
			return strs, nil
		}
		if s, ok := value.(string); ok {
			// Comma-split so a single MCP tool call can write a multi-value
			// property ("Alice, Bob" → ["Alice","Bob"]). Empty/whitespace
			// segments are dropped; an all-empty input is rejected so callers
			// get a clear error rather than persisting an empty list.
			result := make([]string, 0, 1)
			for p := range strings.SplitSeq(s, ",") {
				if t := strings.TrimSpace(p); t != "" {
					result = append(result, t)
				}
			}
			if len(result) == 0 {
				return nil, ValidationError{Field: def.Name, Message: fmt.Sprintf("cannot coerce %q to a non-empty list", s)}
			}
			return result, nil
		}
		return nil, ValidationError{Field: def.Name, Message: fmt.Sprintf("cannot coerce %v (%T) to a list", value, value)}
	}
	return value, nil
}

// asFloat reports whether v is any Go numeric kind yaml.v3 / encoding/json can
// decode a number to, and returns it as float64. yaml.v3 decodes integers as
// `int` and floats as `float64`; JSON decodes all numbers as `float64`.
func asFloat(v any) (float64, bool) {
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

// asStringSlice normalizes a YAML/JSON-decoded list into []string. yaml.v3 and
// JSON both decode arrays into []any; the SDK and hand-built callers may pass
// []string directly. Every element must itself be a string.
func asStringSlice(v any) ([]string, bool) {
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

func containsString(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}
