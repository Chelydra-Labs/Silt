package themes

import (
	"strconv"
	"strings"
)

// Theme is the parsed canonical v2 theme. It mirrors the JSON schema in
// docs/theme-system-v2-rfc.md §2 and DESIGN.md §7 / SPECS.md §6.4: a
// modes-based object with hue-agnostic semantic accents, 9 named surface
// zones, OKLCH-aware colors, and optional geometry/typography/editor/
// background sub-trees. See themes/cyber_forest.json for the canonical
// example.
//
// v2 is the only supported schema (SupportedSchemaVersion). v1 themes are
// rejected; first-party themes are re-authored natively (no migration — see
// ADR docs/decisions/0002-theme-schema-v2-no-migration.md).
type Theme struct {
	SchemaVersion string      `json:"schema_version"`
	ID            string      `json:"id"`
	Name          string      `json:"name"`
	Author        string      `json:"author"`
	Description   string      `json:"description"`
	Typography    *Typography `json:"typography,omitempty"`
	Modes         Modes       `json:"modes"`
}

// Typography holds the optional font-family choices (theme-level, not
// per-mode — families rarely change between dark/light) plus an optional
// type scale (sizes/line-heights/weights). When present, families are
// injected as --font-headline/-body/-mono and the scale as --font-size-* /
// --line-height-* / --font-weight-*; when absent, the config's editor.*
// values remain in effect (backward compatible).
type Typography struct {
	FontFamily     string     `json:"font_family,omitempty"`
	MonoFontFamily string     `json:"mono_font_family,omitempty"`
	HeadlineFont   string     `json:"headline_font,omitempty"`
	Scale          *TypeScale `json:"scale,omitempty"`
}

// TypeScale is the optional type ramp. Each value is a CSS dimension
// (sizes) or unitless number (line-heights, weights).
type TypeScale struct {
	Size       map[string]string `json:"size,omitempty"`        // xs/sm/base/lg/xl/2xl
	LineHeight map[string]string `json:"line_height,omitempty"` // tight/normal/relaxed
	Weight     map[string]string `json:"weight,omitempty"`      // normal/medium/semibold
}

// Modes holds the per-appearance token sets. Both dark and light are
// required for a theme to be valid.
type Modes struct {
	Dark  Mode `json:"dark"`
	Light Mode `json:"light"`
}

// Mode is one appearance (dark or light) of a theme.
//
// Surface model: 9 named zones (app/sidebar/editor/panel/modal/popover/card/
// titlebar/activitybar), each {bg, border, text}. Only `app` is required; the
// others inherit from their parent zone (RFC §5) via var() fallback chains
// emitted by Flatten.
// The flat v1 bg model and the binary chrome block are removed.
//
// Interaction tokens (hover/active/border_active/border_focus) are
// zone-agnostic — the same gesture reads the same way on every surface.
//
// radius/spacing/shadow/editor are optional; Flatten emits sensible defaults
// so a theme that omits them renders with v1-equivalent geometry/type.
type Mode struct {
	Surfaces     Surfaces          `json:"surfaces"`
	Hover        string            `json:"hover"`
	Active       string            `json:"active"`
	BorderActive string            `json:"border_active"`
	BorderFocus  string            `json:"border_focus"`
	TextMuted    string            `json:"text_muted"`
	TextDisabled string            `json:"text_disabled"`
	Accent       Accent            `json:"accent"`
	Status       Status            `json:"status"`
	Error        Error             `json:"error"`
	Radius       *Radius           `json:"radius,omitempty"`
	Spacing      *Spacing          `json:"spacing,omitempty"`
	Shadow       *Shadow           `json:"shadow,omitempty"`
	Editor       *Editor           `json:"editor,omitempty"`
	FocusGlow    string            `json:"focus_glow,omitempty"`
	BorderGlow   string            `json:"border_glow,omitempty"`
	NavIcons     map[string]string `json:"nav_icons,omitempty"`
}

// Surfaces holds the 9 named surface zones. App is the always-authored root;
// the rest are optional and inherit from their parent when absent.
type Surfaces struct {
	App         Surface  `json:"app"`
	Sidebar     *Surface `json:"sidebar,omitempty"`
	Editor      *Surface `json:"editor,omitempty"`
	Panel       *Surface `json:"panel,omitempty"`
	Modal       *Surface `json:"modal,omitempty"`
	Popover     *Surface `json:"popover,omitempty"`
	Card        *Surface `json:"card,omitempty"`
	Titlebar    *Surface `json:"titlebar,omitempty"`
	Activitybar *Surface `json:"activitybar,omitempty"`
}

// Surface is one UI region's canvas: a background, a hairline border, a
// foreground text color, optional per-zone text-emphasis overrides, and an
// optional decorative/photo background block.
//
// Per-zone text emphasis (TextMuted/TextDisabled) is required ONLY when a
// zone's bg luminance differs enough from the app zone that the global
// text-muted/text-disabled would be unreadable on it — the canonical case is
// a dark sidebar zone in an otherwise light theme (Daybreak, Bubblegum). When
// omitted, Flatten falls back to the global emphasis tokens so a normal zone
// pays no cost and reads identically to the app canvas.
type Surface struct {
	BG           string      `json:"bg"`
	Border       string      `json:"border"`
	Text         string      `json:"text"`
	TextMuted    string      `json:"text_muted,omitempty"`
	TextDisabled string      `json:"text_disabled,omitempty"`
	Background   *Background `json:"background,omitempty"`
}

// Background is the unified per-zone surface overlay that subsumes the v1
// texture block. A "texture" is a background with size: tile + low opacity +
// an embedded asset reference; a "background photo" is size: cover with a
// scrim and a user-supplied file. Same primitive, different parameters.
//
// The image reference is sandboxed by validation (no CSS declaration-breaking
// characters) so it cannot escape the :root{--name:value;} injection context.
type Background struct {
	Image    string  `json:"image,omitempty"`    // embedded-name | relative-path | data URI
	Size     string  `json:"size,omitempty"`     // tile | cover | contain
	Opacity  float64 `json:"opacity,omitempty"`  // 0–1 overlay strength
	Blend    string  `json:"blend,omitempty"`    // mix-blend-mode keyword
	Position string  `json:"position,omitempty"` // CSS background-position
	Scrim    string  `json:"scrim,omitempty"`    // tint color layered over the image
}

// Accent holds the two semantic accents (primary = "go/done", secondary =
// "in progress"). Components reference only the semantic names; each theme
// maps its concrete hues onto them.
type Accent struct {
	Primary   AccentTriple `json:"primary"`
	Secondary AccentTriple `json:"secondary"`
}

// AccentTriple is a start/end/glow gradient triple plus on-accent label ink.
// On is the preferred text/icon color for solid fills using Start (WCAG AA
// 4.5:1). When omitted on user themes, Flatten derives black/white from Start
// luminance; first-party themes always author an explicit value.
type AccentTriple struct {
	Start string `json:"start"`
	End   string `json:"end"`
	Glow  string `json:"glow"`
	On    string `json:"on,omitempty"`
}

// Status holds warn/danger/success semantic colors. Success is required in
// v2 (the v1 #165 optional path is retired).
type Status struct {
	Warn    string `json:"warn"`
	Danger  string `json:"danger"`
	Success string `json:"success"`
}

// Error is the themeable error family: foreground (inline/validation errors),
// background, and border. This replaces the static Material-3 --color-error
// (#ffb4ab) that won regardless of active theme — the single biggest "my
// theme doesn't fully apply" gap (#386). status.danger (destructive actions)
// and error.fg (validation/invalid input) are deliberately distinct.
type Error struct {
	FG     string `json:"fg"`
	BG     string `json:"bg"`
	Border string `json:"border"`
}

// Radius is the optional corner-radius ramp.
type Radius struct {
	SM   string `json:"sm"`
	MD   string `json:"md"`
	LG   string `json:"lg"`
	XL   string `json:"xl"`
	Full string `json:"full"`
}

// Spacing is the optional spatial-rhythm ramp.
type Spacing struct {
	SM string `json:"sm"`
	MD string `json:"md"`
	LG string `json:"lg"`
	XL string `json:"xl"`
}

// Shadow is the optional elevation ramp. Values are full CSS box-shadows that
// typically reference theme colors via color-mix(in oklch, ...) so shadows
// respect the palette in both modes.
type Shadow struct {
	SM string `json:"sm"`
	MD string `json:"md"`
	LG string `json:"lg"`
}

// Editor holds the optional editor-canvas interaction colors: caret, text
// selection, link, and highlight marker. These are interaction elements, not
// surfaces, so the block is top-level on Mode (not nested under the editor
// surface zone) per RFC §2.6.
type Editor struct {
	Caret         string `json:"caret"`
	Selection     string `json:"selection"`
	SelectionText string `json:"selection_text"`
	Link          string `json:"link"`
	LinkHover     string `json:"link_hover"`
	Highlight     string `json:"highlight"`
}

// surfaceZone describes one named surface zone for Flatten inheritance and
// the contrast gate. Parent == "" means the zone is a root (always concrete).
// The bg/border/text/text-muted/text-disabled quintet maps a zone to its
// emitted CSS custom properties.
type surfaceZone struct {
	name            string
	parent          string
	cssBg           string
	cssBorder       string
	cssText         string
	cssTextMuted    string
	cssTextDisabled string
	get             func(s Surfaces) *Surface
}

// surfaceZones is the canonical, ordered zone list with the strict-tree
// inheritance graph (RFC §5 / decision D6):
//
//	app ─┬─ sidebar, editor, panel, titlebar, activitybar
//	     └─ panel ─┬─ card
//	               └─ modal ── popover
var surfaceZones = []surfaceZone{
	{name: "app", parent: "", cssBg: "--color-surface-app", cssBorder: "--color-surface-app-border", cssText: "--color-surface-app-text", cssTextMuted: "--color-surface-app-text-muted", cssTextDisabled: "--color-surface-app-text-disabled",
		get: func(s Surfaces) *Surface { return &s.App }},
	{name: "sidebar", parent: "app", cssBg: "--color-surface-sidebar", cssBorder: "--color-surface-sidebar-border", cssText: "--color-surface-sidebar-text", cssTextMuted: "--color-surface-sidebar-text-muted", cssTextDisabled: "--color-surface-sidebar-text-disabled",
		get: func(s Surfaces) *Surface { return s.Sidebar }},
	{name: "editor", parent: "app", cssBg: "--color-surface-editor", cssBorder: "--color-surface-editor-border", cssText: "--color-surface-editor-text", cssTextMuted: "--color-surface-editor-text-muted", cssTextDisabled: "--color-surface-editor-text-disabled",
		get: func(s Surfaces) *Surface { return s.Editor }},
	{name: "panel", parent: "app", cssBg: "--color-surface-panel", cssBorder: "--color-surface-panel-border", cssText: "--color-surface-panel-text", cssTextMuted: "--color-surface-panel-text-muted", cssTextDisabled: "--color-surface-panel-text-disabled",
		get: func(s Surfaces) *Surface { return s.Panel }},
	{name: "card", parent: "panel", cssBg: "--color-surface-card", cssBorder: "--color-surface-card-border", cssText: "--color-surface-card-text", cssTextMuted: "--color-surface-card-text-muted", cssTextDisabled: "--color-surface-card-text-disabled",
		get: func(s Surfaces) *Surface { return s.Card }},
	{name: "modal", parent: "panel", cssBg: "--color-surface-modal", cssBorder: "--color-surface-modal-border", cssText: "--color-surface-modal-text", cssTextMuted: "--color-surface-modal-text-muted", cssTextDisabled: "--color-surface-modal-text-disabled",
		get: func(s Surfaces) *Surface { return s.Modal }},
	{name: "popover", parent: "modal", cssBg: "--color-surface-popover", cssBorder: "--color-surface-popover-border", cssText: "--color-surface-popover-text", cssTextMuted: "--color-surface-popover-text-muted", cssTextDisabled: "--color-surface-popover-text-disabled",
		get: func(s Surfaces) *Surface { return s.Popover }},
	{name: "titlebar", parent: "app", cssBg: "--color-surface-titlebar", cssBorder: "--color-surface-titlebar-border", cssText: "--color-surface-titlebar-text", cssTextMuted: "--color-surface-titlebar-text-muted", cssTextDisabled: "--color-surface-titlebar-text-disabled",
		get: func(s Surfaces) *Surface { return s.Titlebar }},
	{name: "activitybar", parent: "app", cssBg: "--color-surface-activitybar", cssBorder: "--color-surface-activitybar-border", cssText: "--color-surface-activitybar-text", cssTextMuted: "--color-surface-activitybar-text-muted", cssTextDisabled: "--color-surface-activitybar-text-disabled",
		get: func(s Surfaces) *Surface { return s.Activitybar }},
}

// zoneByName resolves a zone name to its surfaceZone descriptor.
func zoneByName(name string) (surfaceZone, bool) {
	for _, z := range surfaceZones {
		if z.name == name {
			return z, true
		}
	}
	return surfaceZone{}, false
}

// emitSurface writes one zone's bg/border/text CSS custom properties. When
// the author set the zone, concrete values are written. When absent (nil),
// each property falls back to its parent zone's token via var() so a theme
// switch repaints both surfaces in one cycle and the property always resolves.
// Per-zone text emphasis (text-muted/disabled) emits concrete when authored,
// otherwise falls back to the GLOBAL emphasis tokens — so only a zone whose
// luminance differs from the app (e.g. a dark sidebar) needs to override them.
func emitSurface(out map[string]string, z surfaceZone, s *Surface) {
	if s != nil {
		out[z.cssBg] = strings.TrimSpace(s.BG)
		out[z.cssBorder] = strings.TrimSpace(s.Border)
		out[z.cssText] = strings.TrimSpace(s.Text)
		if v := strings.TrimSpace(s.TextMuted); v != "" {
			out[z.cssTextMuted] = v
		} else {
			out[z.cssTextMuted] = "var(--color-text-muted)"
		}
		if v := strings.TrimSpace(s.TextDisabled); v != "" {
			out[z.cssTextDisabled] = v
		} else {
			out[z.cssTextDisabled] = "var(--color-text-disabled)"
		}
		emitBackground(out, z, s.Background)
		return
	}
	p, _ := zoneByName(z.parent) // app is the only root; parents always resolve
	out[z.cssBg] = "var(" + p.cssBg + ")"
	out[z.cssBorder] = "var(" + p.cssBorder + ")"
	out[z.cssText] = "var(" + p.cssText + ")"
	out[z.cssTextMuted] = "var(" + p.cssTextMuted + ")"
	out[z.cssTextDisabled] = "var(" + p.cssTextDisabled + ")"
}

// emitBackground writes a zone's optional decorative/photo background. Emitted
// only when the surface declares a background block; absent surfaces pay no
// compositing cost. --silt-bg-<zone>-display gates the overlay's existence
// (the global body::before / per-zone overlay defaults to none; only a
// surface that declares a background flips it).
func emitBackground(out map[string]string, z surfaceZone, b *Background) {
	if b == nil {
		return
	}
	out["--silt-bg-"+z.name+"-display"] = "block"
	if v := strings.TrimSpace(b.Image); v != "" {
		out["--silt-bg-"+z.name+"-image"] = v
	}
	if v := strings.TrimSpace(b.Size); v != "" {
		out["--silt-bg-"+z.name+"-size"] = v
	}
	out["--silt-bg-"+z.name+"-opacity"] = trimFloat(b.Opacity)
	if v := strings.TrimSpace(b.Blend); v != "" {
		out["--silt-bg-"+z.name+"-blend"] = v
	}
	if v := strings.TrimSpace(b.Position); v != "" {
		out["--silt-bg-"+z.name+"-position"] = v
	}
	if v := strings.TrimSpace(b.Scrim); v != "" {
		out["--silt-bg-"+z.name+"-scrim"] = v
	}
}

// trimFloat formats a float without trailing zeros (0.40 not 0.400000).
func trimFloat(f float64) string {
	s := strconv.FormatFloat(f, 'f', -1, 64)
	return s
}

// Flatten produces the flat map of CSS custom-property names → values for
// the given mode ("dark" or "light"). The keys are the --color-* / --radius-*
// / --spacing-* / --shadow-* / --font-* names that Tailwind v4's @theme block
// declares (and generates utilities from), so the runtime injector overrides
// the SAME custom properties the utility classes read — one namespace, no
// bridge/alias layer. An unknown mode falls back to "dark".
func (t *Theme) Flatten(mode string) map[string]string {
	m := t.Modes.Dark
	if mode == "light" {
		m = t.Modes.Light
	}
	out := map[string]string{}

	// Surface zones (concrete when authored, var() inheritance when omitted).
	for _, z := range surfaceZones {
		emitSurface(out, z, z.get(m.Surfaces))
	}

	// Zone-agnostic interaction tokens.
	out["--color-hover"] = strings.TrimSpace(m.Hover)
	out["--color-active"] = strings.TrimSpace(m.Active)
	out["--color-border-active"] = strings.TrimSpace(m.BorderActive)
	out["--color-border-focus"] = strings.TrimSpace(m.BorderFocus)

	// Zone-agnostic text-emphasis levels. Primary text is the app zone's text
	// (RFC §2.2 maps text.primary → surfaces.app.text); muted/disabled are
	// emphasis variants that apply on every surface, parallel to hover/active.
	out["--color-text-primary"] = "var(--color-surface-app-text)"
	out["--color-text-muted"] = strings.TrimSpace(m.TextMuted)
	out["--color-text-disabled"] = strings.TrimSpace(m.TextDisabled)

	// Accents (on = label ink for solid fills using start; derived when omitted).
	// App surface is the CTA backdrop for compositing translucent starts.
	appBG := strings.TrimSpace(m.Surfaces.App.BG)
	out["--color-accent-primary-start"] = m.Accent.Primary.Start
	out["--color-accent-primary-end"] = m.Accent.Primary.End
	out["--color-accent-primary-glow"] = m.Accent.Primary.Glow
	out["--color-accent-primary-on"] = resolveAccentOn(m.Accent.Primary, appBG)
	out["--color-accent-secondary-start"] = m.Accent.Secondary.Start
	out["--color-accent-secondary-end"] = m.Accent.Secondary.End
	out["--color-accent-secondary-glow"] = m.Accent.Secondary.Glow
	out["--color-accent-secondary-on"] = resolveAccentOn(m.Accent.Secondary, appBG)
	// Semantic alias for solid primary CTAs (Tailwind text-text-on-accent).
	out["--color-text-on-accent"] = out["--color-accent-primary-on"]

	// Status.
	out["--color-status-warn"] = m.Status.Warn
	out["--color-status-danger"] = m.Status.Danger
	out["--color-status-success"] = m.Status.Success

	// Themeable error family (replaces the static Material-3 --color-error).
	out["--color-error"] = m.Error.FG
	out["--color-error-bg"] = m.Error.BG
	out["--color-error-border"] = m.Error.Border

	// Geometry (optional; defaults keep v1-equivalent geometry).
	flattenGeometry(out, m)

	// Editor interaction tokens (optional; defaults keep browser equivalents).
	flattenEditor(out, m)

	// Effects (optional; "none" when omitted so existing themes are unaffected).
	flattenEffects(out, m)

	// Nav icons (optional; defaults to activitybar text-muted).
	flattenNavIcons(out, m)

	// Typography (theme-level, not per-mode).
	flattenTypography(out, t.Typography)

	return out
}

// flattenGeometry emits radius/spacing/shadow with sensible defaults so a
// theme that omits them renders identically to v1. Shadows reference the
// active palette via color-mix so they read correctly in both modes.
func flattenGeometry(out map[string]string, m Mode) {
	if r := m.Radius; r != nil {
		out["--radius-sm"] = strings.TrimSpace(r.SM)
		out["--radius-md"] = strings.TrimSpace(r.MD)
		out["--radius-lg"] = strings.TrimSpace(r.LG)
		out["--radius-xl"] = strings.TrimSpace(r.XL)
		out["--radius-full"] = strings.TrimSpace(r.Full)
	} else {
		out["--radius-sm"] = "4px"
		out["--radius-md"] = "8px"
		out["--radius-lg"] = "12px"
		out["--radius-xl"] = "16px"
		out["--radius-full"] = "9999px"
	}
	if sp := m.Spacing; sp != nil {
		out["--spacing-sm"] = strings.TrimSpace(sp.SM)
		out["--spacing-md"] = strings.TrimSpace(sp.MD)
		out["--spacing-lg"] = strings.TrimSpace(sp.LG)
		out["--spacing-xl"] = strings.TrimSpace(sp.XL)
	} else {
		out["--spacing-sm"] = "4px"
		out["--spacing-md"] = "8px"
		out["--spacing-lg"] = "16px"
		out["--spacing-xl"] = "24px"
	}
	if sh := m.Shadow; sh != nil {
		out["--shadow-sm"] = strings.TrimSpace(sh.SM)
		out["--shadow-md"] = strings.TrimSpace(sh.MD)
		out["--shadow-lg"] = strings.TrimSpace(sh.LG)
	} else {
		out["--shadow-sm"] = "0 1px 2px color-mix(in oklch, var(--color-surface-app) 40%, transparent)"
		out["--shadow-md"] = "0 4px 12px color-mix(in oklch, var(--color-surface-app) 35%, transparent)"
		out["--shadow-lg"] = "0 12px 32px color-mix(in oklch, var(--color-surface-app) 30%, transparent)"
	}
}

// flattenEditor emits the editor-canvas interaction tokens with defaults that
// keep the editor readable when a theme omits the block. Defaults are derived
// from the active palette so an un-themed editor still matches its theme.
func flattenEditor(out map[string]string, m Mode) {
	if e := m.Editor; e != nil {
		out["--color-editor-caret"] = strings.TrimSpace(e.Caret)
		out["--color-editor-selection"] = strings.TrimSpace(e.Selection)
		out["--color-editor-selection-text"] = strings.TrimSpace(e.SelectionText)
		out["--color-editor-link"] = strings.TrimSpace(e.Link)
		out["--color-editor-link-hover"] = strings.TrimSpace(e.LinkHover)
		out["--color-editor-highlight"] = strings.TrimSpace(e.Highlight)
		return
	}
	// Sensible defaults: caret/selection derive from the primary accent so an
	// un-themed editor still feels coherent with its theme.
	out["--color-editor-caret"] = "var(--color-accent-primary-start)"
	out["--color-editor-selection"] = "color-mix(in oklch, var(--color-accent-primary-start) 30%, transparent)"
	out["--color-editor-selection-text"] = "var(--color-surface-editor-text)"
	out["--color-editor-link"] = "var(--color-accent-secondary-start)"
	out["--color-editor-link-hover"] = "var(--color-accent-secondary-end)"
	out["--color-editor-highlight"] = "color-mix(in oklch, var(--color-status-warn) 40%, transparent)"
}

// flattenTypography emits the optional font-family choices and type scale.
// Families fall through to config-driven --editor-* values via CSS fallbacks
// when absent; the scale falls through to hardcoded CSS defaults.
func flattenTypography(out map[string]string, ty *Typography) {
	if ty == nil {
		return
	}
	if v := strings.TrimSpace(ty.HeadlineFont); v != "" {
		out["--font-headline"] = v
	}
	if v := strings.TrimSpace(ty.FontFamily); v != "" {
		out["--font-body"] = v
	}
	if v := strings.TrimSpace(ty.MonoFontFamily); v != "" {
		out["--font-mono"] = v
	}
	if sc := ty.Scale; sc != nil {
		for k, v := range sc.Size {
			if vv := strings.TrimSpace(v); vv != "" {
				out["--font-size-"+k] = vv
			}
		}
		for k, v := range sc.LineHeight {
			if vv := strings.TrimSpace(v); vv != "" {
				out["--line-height-"+k] = vv
			}
		}
		for k, v := range sc.Weight {
			if vv := strings.TrimSpace(v); vv != "" {
				out["--font-weight-"+k] = vv
			}
		}
	}
}

// flattenEffects emits the optional glow effect tokens. "none" when omitted so
// existing themes are unaffected — CSS box-shadow: none is a no-op.
func flattenEffects(out map[string]string, m Mode) {
	if v := strings.TrimSpace(m.FocusGlow); v != "" {
		out["--focus-glow"] = v
	} else {
		out["--focus-glow"] = "none"
	}
	if v := strings.TrimSpace(m.BorderGlow); v != "" {
		out["--border-glow"] = v
	} else {
		out["--border-glow"] = "none"
	}
}

// flattenNavIcons emits the optional navigation icon colors. Defaults to
// var(--color-surface-activitybar-text-muted) when omitted.
func flattenNavIcons(out map[string]string, m Mode) {
	// Pre-populate the canonical views so they are always present.
	canonical := []string{"notes", "tags", "calendar", "tasks", "kanban", "settings"}
	for _, id := range canonical {
		out["--color-nav-icon-"+id] = "var(--color-surface-activitybar-text-muted)"
	}

	// Layer on any custom overrides from the theme map.
	for k, v := range m.NavIcons {
		if val := strings.TrimSpace(v); val != "" {
			out["--color-nav-icon-"+k] = val
		}
	}
}

// BGVoid returns the resolved app-zone background for the given mode, used to
// set the native webview BackgroundColour without a full flatten round-trip.
func (t *Theme) BGVoid(mode string) string {
	if mode == "light" {
		return t.Modes.Light.Surfaces.App.BG
	}
	return t.Modes.Dark.Surfaces.App.BG
}

// HexToRGB parses a #rgb / #rrggbb / #rrggbbaa hex color into its 8-bit
// components. The 8-digit form (with alpha) is accepted but the alpha channel
// is intentionally dropped: this seeds the native webview BackgroundColour,
// which is an opaque window background where alpha has no meaning. Non-hex
// inputs (including oklch()) return ok=false so the caller keeps a safe
// default; oklch backgrounds fall back through the cache to the embedded
// default on the launch path.
func HexToRGB(s string) (r, g, b uint8, ok bool) {
	s = strings.TrimSpace(s)
	if len(s) == 0 || s[0] != '#' {
		return 0, 0, 0, false
	}
	hex := s[1:]
	var full string
	switch len(hex) {
	case 3:
		full = string([]byte{hex[0], hex[0], hex[1], hex[1], hex[2], hex[2]})
	case 6:
		full = hex
	case 8:
		full = hex[0:6]
	default:
		return 0, 0, 0, false
	}
	ri, ok1 := parseHexByte(full[0:2])
	gi, ok2 := parseHexByte(full[2:4])
	bi, ok3 := parseHexByte(full[4:6])
	if !ok1 || !ok2 || !ok3 {
		return 0, 0, 0, false
	}
	return ri, gi, bi, true
}

func parseHexByte(s string) (uint8, bool) {
	hi, ok1 := hexDigit(s[0])
	lo, ok2 := hexDigit(s[1])
	if !ok1 || !ok2 {
		return 0, false
	}
	return hi*16 + lo, true
}

func hexDigit(c byte) (uint8, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}

// ThemeInfo is the lightweight metadata returned by ListThemes for the picker
// UI and the active-theme summary.
type ThemeInfo struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Author      string   `json:"author"`
	Description string   `json:"description"`
	Swatches    []string `json:"swatches"` // preview color hexes (primary/secondary start)
	Source      string   `json:"source"`   // "disk" | "default" | "bundled"
}

// AsInfo converts a parsed Theme into the lightweight ThemeInfo, deriving
// preview swatches from the dark-mode accent starts.
func (t *Theme) AsInfo(source string) ThemeInfo {
	return ThemeInfo{
		ID:          t.ID,
		Name:        t.Name,
		Author:      t.Author,
		Description: t.Description,
		Swatches:    []string{t.Modes.Dark.Accent.Primary.Start, t.Modes.Dark.Accent.Secondary.Start},
		Source:      source,
	}
}
