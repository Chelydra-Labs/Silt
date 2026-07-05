package themes

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// IsValidThemeID reports whether id is a safe component for a theme
// filename. The id flows into filepath.Join(themesDir, id+".json") on the
// launch path, so it must not contain path-separator, parent-dir, or NUL
// characters that could let a user-controlled active_theme resolve a file
// outside the themes dir (CWE-22). The set of valid characters is
// intentionally narrow ([a-z0-9_-]).
func IsValidThemeID(id string) bool {
	if id == "" {
		return false
	}
	for _, c := range id {
		switch {
		case c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9':
		case c == '-' || c == '_':
		default:
			return false
		}
	}
	return true
}

// SupportedSchemaVersion is the only theme schema version this build accepts.
// v2 is a breaking change (surface zones, OKLCH, removed bg/chrome/texture);
// themes carrying any other version are rejected with a descriptive error.
// There is no v1→v2 migration (single-user project; first-party themes are
// re-authored natively) — see ADR docs/decisions/0002-theme-schema-v2-no-migration.md.
const SupportedSchemaVersion = "2.0.0"

// ValidationError describes a single theme-validation problem in
// machine-readable form so the UI can surface "theme X is missing token Y"
// without crashing on a bad file.
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

// Validate checks a parsed theme against the v2 schema. It returns nil if the
// theme is well-formed, or a ValidationErrors slice listing every problem.
// schema_version is hard-enforced: anything other than SupportedSchemaVersion
// is rejected outright so a forward/backward-versioned theme never loads as a
// silently-wrong v2.
func Validate(t *Theme) error {
	var errs ValidationErrors

	if t == nil {
		return ValidationErrors{{Field: "$", Message: "theme is nil"}}
	}
	if strings.TrimSpace(t.ID) == "" {
		errs = append(errs, ValidationError{Field: "id", Message: "id is required"})
	} else if !IsValidThemeID(t.ID) {
		errs = append(errs, ValidationError{Field: "id", Message: fmt.Sprintf("id %q must be lowercase [a-z0-9_-]", t.ID)})
	}
	if strings.TrimSpace(t.Name) == "" {
		errs = append(errs, ValidationError{Field: "name", Message: "name is required"})
	}
	if v := strings.TrimSpace(t.SchemaVersion); v != SupportedSchemaVersion {
		return ValidationErrors{{
			Field:   "schema_version",
			Message: fmt.Sprintf("unsupported schema_version %q: only %q is supported (v2 is a breaking change; re-author the theme in v2)", v, SupportedSchemaVersion),
		}}
	}

	errs = append(errs, validateMode("modes.dark", t.Modes.Dark)...)
	errs = append(errs, validateMode("modes.light", t.Modes.Light)...)

	// Typography is optional (theme-level). When present, font-family fields
	// and the optional scale are validated.
	if t.Typography != nil {
		errs = append(errs, validateFontField("typography.font_family", t.Typography.FontFamily)...)
		errs = append(errs, validateFontField("typography.mono_font_family", t.Typography.MonoFontFamily)...)
		errs = append(errs, validateFontField("typography.headline_font", t.Typography.HeadlineFont)...)
		if t.Typography.Scale != nil {
			errs = append(errs, validateScaleMap("typography.scale.size", t.Typography.Scale.Size)...)
			errs = append(errs, validateScaleMap("typography.scale.line_height", t.Typography.Scale.LineHeight)...)
			errs = append(errs, validateScaleMap("typography.scale.weight", t.Typography.Scale.Weight)...)
		}
	}

	if len(errs) == 0 {
		return nil
	}
	return errs
}

func validateMode(prefix string, m Mode) ValidationErrors {
	var errs ValidationErrors

	// Surface zones: app is required, the rest are optional (inherit when
	// absent). Every authored zone must carry a valid bg/border/text triple
	// and, optionally, a validated background block.
	errs = append(errs, validateSurface(prefix+".surfaces.app", &m.Surfaces.App, true)...)
	for _, z := range surfaceZones {
		if z.name == "app" {
			continue
		}
		if s := z.get(m.Surfaces); s != nil {
			errs = append(errs, validateSurface(prefix+".surfaces."+z.name, s, false)...)
		}
	}

	// Zone-agnostic interaction tokens.
	errs = append(errs, validateColorField(prefix+".hover", m.Hover)...)
	errs = append(errs, validateColorField(prefix+".active", m.Active)...)
	errs = append(errs, validateColorField(prefix+".border_active", m.BorderActive)...)
	errs = append(errs, validateColorField(prefix+".border_focus", m.BorderFocus)...)
	errs = append(errs, validateColorField(prefix+".text_muted", m.TextMuted)...)
	errs = append(errs, validateColorField(prefix+".text_disabled", m.TextDisabled)...)

	// Accents.
	errs = append(errs, validateTriple(prefix+".accent.primary", m.Accent.Primary)...)
	errs = append(errs, validateTriple(prefix+".accent.secondary", m.Accent.Secondary)...)

	// Status (success now required — the v1 #165 optional path is retired).
	errs = append(errs, validateColorField(prefix+".status.warn", m.Status.Warn)...)
	errs = append(errs, validateColorField(prefix+".status.danger", m.Status.Danger)...)
	errs = append(errs, validateColorField(prefix+".status.success", m.Status.Success)...)

	// Themeable error family (required — replaces static Material-3 --color-error).
	errs = append(errs, validateColorField(prefix+".error.fg", m.Error.FG)...)
	errs = append(errs, validateColorField(prefix+".error.bg", m.Error.BG)...)
	errs = append(errs, validateColorField(prefix+".error.border", m.Error.Border)...)

	// Optional geometry blocks: validate colors/dimensions only when present.
	if m.Radius != nil {
		errs = append(errs, validateDim(prefix+".radius.sm", m.Radius.SM)...)
		errs = append(errs, validateDim(prefix+".radius.md", m.Radius.MD)...)
		errs = append(errs, validateDim(prefix+".radius.lg", m.Radius.LG)...)
		errs = append(errs, validateDim(prefix+".radius.xl", m.Radius.XL)...)
		errs = append(errs, validateDim(prefix+".radius.full", m.Radius.Full)...)
	}
	if m.Spacing != nil {
		errs = append(errs, validateDim(prefix+".spacing.sm", m.Spacing.SM)...)
		errs = append(errs, validateDim(prefix+".spacing.md", m.Spacing.MD)...)
		errs = append(errs, validateDim(prefix+".spacing.lg", m.Spacing.LG)...)
		errs = append(errs, validateDim(prefix+".spacing.xl", m.Spacing.XL)...)
	}
	if m.Shadow != nil {
		errs = append(errs, validateShadowValue(prefix+".shadow.sm", m.Shadow.SM)...)
		errs = append(errs, validateShadowValue(prefix+".shadow.md", m.Shadow.MD)...)
		errs = append(errs, validateShadowValue(prefix+".shadow.lg", m.Shadow.LG)...)
	}
	if m.Editor != nil {
		e := m.Editor
		errs = append(errs, validateColorField(prefix+".editor.caret", e.Caret)...)
		errs = append(errs, validateColorField(prefix+".editor.selection", e.Selection)...)
		errs = append(errs, validateColorField(prefix+".editor.selection_text", e.SelectionText)...)
		errs = append(errs, validateColorField(prefix+".editor.link", e.Link)...)
		errs = append(errs, validateColorField(prefix+".editor.link_hover", e.LinkHover)...)
		errs = append(errs, validateColorField(prefix+".editor.highlight", e.Highlight)...)
	}
	// Optional glow effects: CSS box-shadow strings, validated like shadows.
	if v := strings.TrimSpace(m.FocusGlow); v != "" {
		errs = append(errs, validateShadowValue(prefix+".focus_glow", v)...)
	}
	if v := strings.TrimSpace(m.BorderGlow); v != "" {
		errs = append(errs, validateShadowValue(prefix+".border_glow", v)...)
	}

	// Optional nav icon color overrides. Keys flow verbatim into CSS custom-
	// property names (out["--color-nav-icon-"+k]), so they are gated by the
	// same safe-key whitelist as typography scale keys — a key like
	// "x;--color-error" would otherwise be CSS injection. Values are validated
	// like standard colors when present.
	for k, v := range m.NavIcons {
		if !scaleKeyPattern.MatchString(k) {
			errs = append(errs, ValidationError{Field: prefix + ".nav_icons." + k, Message: "nav_icons key must match ^[a-z0-9-]+$"})
			continue
		}
		if val := strings.TrimSpace(v); val != "" {
			errs = append(errs, validateColorField(prefix+".nav_icons."+k, val)...)
		}
	}

	return errs
}

// validateSurface checks one zone's bg/border/text colors. When required is
// false, an all-empty surface is treated as "not authored" (the caller has
// already decided it is present via the pointer check, so this only matters
// for app, which is always required).
func validateSurface(prefix string, s *Surface, required bool) ValidationErrors {
	if s == nil {
		if required {
			return ValidationErrors{{Field: prefix, Message: "surface is required"}}
		}
		return nil
	}
	return append(append(append(append(append(
		validateColorField(prefix+".bg", s.BG),
		validateColorField(prefix+".border", s.Border)...),
		validateColorField(prefix+".text", s.Text)...),
		validateOptionalColor(prefix+".text_muted", s.TextMuted)...),
		validateOptionalColor(prefix+".text_disabled", s.TextDisabled)...),
		validateBackground(prefix+".background", s.Background)...)
}

// validateOptionalColor accepts an empty value (the field is optional) but
// validates the format when present. Used for per-zone text-emphasis overrides.
func validateOptionalColor(field, value string) ValidationErrors {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if !isValidColor(value) {
		return ValidationErrors{{Field: field, Message: fmt.Sprintf("not a valid color: %q (expected #hex, rgb()/rgba(), or oklch())", value)}}
	}
	return nil
}

// validateTriple checks a start/end/glow accent triple. Glow is commonly a
// translucent rgba(); all three must be present and valid colors.
func validateTriple(prefix string, t AccentTriple) ValidationErrors {
	var errs ValidationErrors
	errs = append(errs, validateColorField(prefix+".start", t.Start)...)
	errs = append(errs, validateColorField(prefix+".end", t.End)...)
	errs = append(errs, validateColorField(prefix+".glow", t.Glow)...)
	return errs
}

// validateColorField requires a non-empty, format-valid color.
func validateColorField(field, value string) ValidationErrors {
	value = strings.TrimSpace(value)
	if value == "" {
		return ValidationErrors{{Field: field, Message: "token is missing"}}
	}
	if !isValidColor(value) {
		return ValidationErrors{{Field: field, Message: fmt.Sprintf("not a valid color: %q (expected #hex, rgb()/rgba(), or oklch())", value)}}
	}
	return nil
}

// validateDim requires a non-empty CSS dimension-ish value (length). It is
// permissive on units (px/rem/em/%/vh are all valid for radii/spacing) but
// rejects injection characters that could break the :root{--name:value;}
// context.
func validateDim(field, value string) ValidationErrors {
	value = strings.TrimSpace(value)
	if value == "" {
		return ValidationErrors{{Field: field, Message: "dimension is missing"}}
	}
	if !isSafeCSSValue(value) {
		return ValidationErrors{{Field: field, Message: fmt.Sprintf("not a safe dimension value: %q", value)}}
	}
	return nil
}

// validateShadowValue checks a box-shadow string. Shadows may reference theme
// tokens via color-mix()/var(), so the check is the structural-safety one
// (no declaration-breaking characters), not a color parse.
func validateShadowValue(field, value string) ValidationErrors {
	value = strings.TrimSpace(value)
	if value == "" {
		return ValidationErrors{{Field: field, Message: "shadow is missing"}}
	}
	if !isSafeCSSValue(value) {
		return ValidationErrors{{Field: field, Message: fmt.Sprintf("not a safe shadow value: %q", value)}}
	}
	return nil
}

// validateScaleMap checks the optional typography-scale maps. Empty maps are
// valid (the scale is optional); keys must match the safe-key whitelist
// (they flow verbatim into CSS custom-property names) and values must be
// safe CSS.
func validateScaleMap(prefix string, m map[string]string) ValidationErrors {
	var errs ValidationErrors
	for k, v := range m {
		if !scaleKeyPattern.MatchString(k) {
			errs = append(errs, ValidationError{Field: prefix + "." + k, Message: "scale key must match ^[a-z0-9-]+$"})
			continue
		}
		if !isSafeCSSValue(strings.TrimSpace(v)) {
			errs = append(errs, ValidationError{Field: prefix + "." + k, Message: fmt.Sprintf("not a safe value: %q", v)})
		}
	}
	return errs
}

// scaleKeyPattern whitelists typography-scale map keys. Keys are interpolated
// verbatim into CSS custom-property names (out["--font-size-"+k]), so a key
// like "sm;--color-surface-app" would be a CSS injection. The set matches the
// safe-identifier chars used elsewhere in the schema (lowercase alnum and '-').
var scaleKeyPattern = regexp.MustCompile(`^[a-z0-9-]+$`)

// validateBackground sandbox-checks an optional per-zone background block. The
// image value flows verbatim into a CSS background-image, so it must not
// contain declaration-breaking characters. Size must be a recognized mode;
// opacity in [0,1]; blend a recognized mix-blend-mode; scrim a valid color.
func validateBackground(prefix string, b *Background) ValidationErrors {
	if b == nil {
		return nil
	}
	var errs ValidationErrors
	if img := strings.TrimSpace(b.Image); img != "" {
		if !isSafeBackgroundImage(img) {
			errs = append(errs, ValidationError{Field: prefix + ".image", Message: fmt.Sprintf("not a safe background.image value: %q (must not contain ;, {, }, <, >, or \\)", img)})
		}
	}
	if sz := strings.TrimSpace(b.Size); sz != "" {
		switch sz {
		case "tile", "cover", "contain":
		default:
			errs = append(errs, ValidationError{Field: prefix + ".size", Message: fmt.Sprintf("background.size %q must be tile, cover, or contain", sz)})
		}
	}
	if b.Opacity < 0 || b.Opacity > 1 || math.IsNaN(b.Opacity) || math.IsInf(b.Opacity, 0) {
		errs = append(errs, ValidationError{Field: prefix + ".opacity", Message: fmt.Sprintf("background.opacity must be a number in [0,1], got %v", b.Opacity)})
	}
	if bl := strings.TrimSpace(b.Blend); bl != "" {
		if !validBlendModes[bl] {
			errs = append(errs, ValidationError{Field: prefix + ".blend", Message: fmt.Sprintf("background.blend %q is not a recognized mix-blend-mode", bl)})
		}
	}
	if sc := strings.TrimSpace(b.Scrim); sc != "" {
		if !isValidColor(sc) {
			errs = append(errs, ValidationError{Field: prefix + ".scrim", Message: fmt.Sprintf("background.scrim must be a color, got %q", sc)})
		}
	}
	if p := strings.TrimSpace(b.Position); p != "" {
		if !isSafeCSSValue(p) {
			errs = append(errs, ValidationError{Field: prefix + ".position", Message: fmt.Sprintf("not a safe background.position value: %q", p)})
		}
	}
	return errs
}

// validBlendModes is the set of CSS mix-blend-mode keywords a background blend
// may use. Restricting to this allowlist keeps the injected value predictable.
var validBlendModes = map[string]bool{
	"normal": true, "multiply": true, "screen": true, "overlay": true,
	"darken": true, "lighten": true, "color-dodge": true, "color-burn": true,
	"hard-light": true, "soft-light": true, "difference": true, "exclusion": true,
	"hue": true, "saturation": true, "color": true, "luminosity": true,
}

func validateFontField(field, value string) ValidationErrors {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if !isValidFontFamily(value) {
		return ValidationErrors{{Field: field, Message: fmt.Sprintf("not a valid font-family value: %q (must not contain ;, {, }, <, or >)", value)}}
	}
	return nil
}

// isValidFontFamily accepts any string that does not contain characters that
// could break out of a CSS property declaration context. Backslash is
// included because CSS escape sequences (\3B → ;) could bypass the literal
// character checks at CSS-parse time.
func isValidFontFamily(s string) bool {
	for _, c := range s {
		switch c {
		case ';', '{', '}', '<', '>', '\\':
			return false
		}
	}
	return true
}

// isSafeCSSValue accepts dimension/shadow/position strings that cannot escape
// the CSS custom-property declaration context. Same denylist as font families.
func isSafeCSSValue(s string) bool {
	return isValidFontFamily(s)
}

// isSafeBackgroundImage accepts background-image values that cannot escape the
// CSS custom-property declaration context (:root{--name:VALUE;}). The value
// flows verbatim into a CSS custom property consumed by background-image, so a
// top-level declaration-breaker (';', '{', '}') would terminate the property
// early and let attacker-controlled CSS leak. Breakers are still allowed
// INSIDE url()/strings: CSS parses url("…;…") and "a;b" as single tokens, so a
// ';' sealed inside them cannot terminate the surrounding declaration — which
// is what makes a base64 data URI (url("data:image/png;base64,…"), #391) safe.
//
// Raw <, >, \ are rejected unconditionally: an unencoded <svg> could inject
// markup and a CSS backslash-escape could disguise a breaker, so the few values
// that need them must percent-encode (data URIs) or avoid them entirely. The
// scan tracks paren depth and string state so a breaker at the top level
// (outside any url()/string) is the only thing that fails the check.
func isSafeBackgroundImage(s string) bool {
	depth := 0
	inString := byte(0)
	for i := 0; i < len(s); i++ {
		c := s[i]
		// <, >, \ are never needed in a background-image value and are
		// rejected outright (percent-encode or omit them).
		if c == '<' || c == '>' || c == '\\' {
			return false
		}
		if inString != 0 {
			if c == inString {
				inString = 0
			}
			continue
		}
		switch c {
		case '"', '\'':
			inString = c
		case '(':
			depth++
		case ')':
			if depth == 0 {
				return false // unbalanced ')' would close the declaration
			}
			depth--
		case ';', '{', '}':
			if depth == 0 {
				return false // top-level declaration-breaker
			}
		}
	}
	return inString == 0 && depth == 0
}

// isValidColor accepts the color forms used by the v2 theme: #hex (#rgb /
// #rrggbb / #rrggbbaa), rgb()/rgba(), and oklch(L C H) / oklch(L C H / A).
// Component ranges are validated so malformed values are caught at validation
// time. NaN/Inf are explicitly rejected (Go's ParseFloat accepts them).
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
	if strings.HasPrefix(s, "oklch(") && strings.HasSuffix(s, ")") {
		_, ok := parseOKLCH(s)
		return ok
	}
	inner, wantParts := "", 0
	switch {
	case strings.HasPrefix(s, "rgba(") && strings.HasSuffix(s, ")"):
		inner, wantParts = s[len("rgba("):len(s)-1], 4
	case strings.HasPrefix(s, "rgb(") && strings.HasSuffix(s, ")"):
		inner, wantParts = s[len("rgb("):len(s)-1], 3
	default:
		return false
	}
	parts := strings.Split(inner, ",")
	if len(parts) != wantParts {
		return false
	}
	for i, p := range parts {
		p = strings.TrimSpace(p)
		percent := strings.HasSuffix(p, "%")
		num := p
		if percent {
			num = p[:len(p)-1]
		}
		v, err := strconv.ParseFloat(num, 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
		if i == wantParts-1 && wantParts == 4 {
			// Alpha: numeric 0–1 or CSS Color 4 percent 0–100%.
			if percent {
				if v < 0 || v > 100 {
					return false
				}
			} else if v < 0 || v > 1 {
				return false
			}
		} else if percent {
			if v < 0 || v > 100 {
				return false
			}
		} else if v < 0 || v > 255 {
			return false
		}
	}
	return true
}

// maxThemeJSONBytes bounds a theme JSON before it is parsed. A hostile or
// corrupted theme file cannot drive unbounded allocation ahead of Validate.
const maxThemeJSONBytes int64 = 1 << 20 // 1 MB

// ParseAndValidate unmarshals theme JSON and runs Validate in one step. v2
// rejects unknown fields (a typo like "borde" no longer silently drops a
// token), so a json.Decoder with DisallowUnknownFields is used. The raw bytes
// are returned alongside for callers that want to re-emit the canonical form.
func ParseAndValidate(raw []byte) (*Theme, error) {
	if int64(len(raw)) > maxThemeJSONBytes {
		return nil, fmt.Errorf("theme JSON exceeds the %d-byte cap; refusing to parse", maxThemeJSONBytes)
	}
	var t Theme
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&t); err != nil {
		return nil, fmt.Errorf("theme JSON is not parseable: %w", err)
	}
	if err := Validate(&t); err != nil {
		return nil, err
	}
	return &t, nil
}
