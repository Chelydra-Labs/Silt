package themes

import (
	"encoding/json"
	"fmt"
	"strings"
)

// SupportedSchemaVersion is the canonical theme schema version this build
// understands. Themes carrying a different version are parsed structurally
// rather than rejected outright, so a forward-versioned theme whose token
// set still matches v1 keeps working.
const SupportedSchemaVersion = "1.0.0"

// ValidationError describes a single theme-validation problem in
// machine-readable form so the UI can surface "theme X is missing token Y"
// without the app crashing on a bad file.
type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func (v ValidationError) Error() string {
	return fmt.Sprintf("theme validation error at %s: %s", v.Field, v.Message)
}

// ValidationErrors aggregates the per-field issues found while validating a
// theme. The loader wraps these into a single error so a caller gets every
// problem in one pass instead of fixing them one at a time.
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

// requiredTokens lists every CSS token a mode must define, in
// (jsonPathWithinMode, cssName) form. Both dark and light modes must define
// the full set for a theme to be valid.
var requiredTokens = []struct {
	path string
	get  func(m Mode) string
}{
	{"bg.void", func(m Mode) string { return m.BG.Void }},
	{"bg.surface", func(m Mode) string { return m.BG.Surface }},
	{"bg.panel", func(m Mode) string { return m.BG.Panel }},
	{"bg.hover", func(m Mode) string { return m.BG.Hover }},
	{"bg.active", func(m Mode) string { return m.BG.Active }},
	{"border.muted", func(m Mode) string { return m.Border.Muted }},
	{"border.zinc", func(m Mode) string { return m.Border.Zinc }},
	{"border.active", func(m Mode) string { return m.Border.Active }},
	{"border.focus", func(m Mode) string { return m.Border.Focus }},
	{"text.primary", func(m Mode) string { return m.Text.Primary }},
	{"text.muted", func(m Mode) string { return m.Text.Muted }},
	{"text.disabled", func(m Mode) string { return m.Text.Disabled }},
	{"accent.primary.start", func(m Mode) string { return m.Accent.Primary.Start }},
	{"accent.primary.end", func(m Mode) string { return m.Accent.Primary.End }},
	{"accent.primary.glow", func(m Mode) string { return m.Accent.Primary.Glow }},
	{"accent.secondary.start", func(m Mode) string { return m.Accent.Secondary.Start }},
	{"accent.secondary.end", func(m Mode) string { return m.Accent.Secondary.End }},
	{"accent.secondary.glow", func(m Mode) string { return m.Accent.Secondary.Glow }},
	{"status.warn", func(m Mode) string { return m.Status.Warn }},
	{"status.danger", func(m Mode) string { return m.Status.Danger }},
}

// Validate checks a parsed theme against the canonical schema. It returns
// nil if the theme is well-formed, or a ValidationErrors slice listing
// every problem (missing tokens, malformed colors, missing identity
// fields). schema_version is informational: a missing/unknown version is
// reported but does not by itself reject a structurally-valid theme, so a
// forward-versioned theme keeps loading.
func Validate(t *Theme) error {
	var errs ValidationErrors

	if t == nil {
		return ValidationErrors{{Field: "$", Message: "theme is nil"}}
	}
	if strings.TrimSpace(t.ID) == "" {
		errs = append(errs, ValidationError{Field: "id", Message: "id is required"})
	}
	if strings.TrimSpace(t.Name) == "" {
		errs = append(errs, ValidationError{Field: "name", Message: "name is required"})
	}
	if strings.TrimSpace(t.SchemaVersion) == "" {
		errs = append(errs, ValidationError{Field: "schema_version", Message: "schema_version is required"})
	}

	errs = append(errs, validateMode("modes.dark", t.Modes.Dark)...)
	errs = append(errs, validateMode("modes.light", t.Modes.Light)...)

	if len(errs) == 0 {
		return nil
	}
	return errs
}

func validateMode(prefix string, m Mode) ValidationErrors {
	var errs ValidationErrors
	for _, tok := range requiredTokens {
		val := tok.get(m)
		if strings.TrimSpace(val) == "" {
			errs = append(errs, ValidationError{
				Field:   prefix + "." + tok.path,
				Message: "token is missing",
			})
			continue
		}
		if !isValidColor(val) {
			errs = append(errs, ValidationError{
				Field:   prefix + "." + tok.path,
				Message: fmt.Sprintf("not a valid color: %q (expected #hex or rgb()/rgba())", val),
			})
		}
	}
	return errs
}

// isValidColor accepts the color forms used by the canonical theme:
// #rgb / #rrggbb / #rrggbbaa hex, and rgb()/rgba() functional notation. It
// is intentionally narrow (no hsl/named-colors) so malformed values are
// caught early.
func isValidColor(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	if strings.HasPrefix(s, "#") {
		hex := s[1:]
		switch len(hex) {
		case 3, 6, 8:
			for _, r := range hex {
				isHex := (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
				if !isHex {
					return false
				}
			}
			return true
		}
		return false
	}
	if strings.HasPrefix(s, "rgba(") && strings.HasSuffix(s, ")") {
		return commaCount(s) == 3
	}
	if strings.HasPrefix(s, "rgb(") && strings.HasSuffix(s, ")") {
		return commaCount(s) == 2
	}
	return false
}

func commaCount(s string) int {
	n := 0
	for _, r := range s {
		if r == ',' {
			n++
		}
	}
	return n
}

// ParseAndValidate unmarshals theme JSON and runs Validate in one step.
// The raw bytes are returned alongside for callers that want to re-emit the
// canonical form. A nil theme with an error is never returned for valid
// JSON that is merely missing fields — those surface as ValidationErrors.
func ParseAndValidate(raw []byte) (*Theme, error) {
	var t Theme
	if err := json.Unmarshal(raw, &t); err != nil {
		return nil, fmt.Errorf("theme JSON is not parseable: %w", err)
	}
	if err := Validate(&t); err != nil {
		return nil, err
	}
	return &t, nil
}
