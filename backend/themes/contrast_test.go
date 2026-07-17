package themes

import (
	"math"
	"strings"
	"testing"
)

// approxRatio computes the WCAG contrast ratio between two color strings
// using the same math as the production harness, rounded to 2 dp for legible
// failure messages.
func approxRatio(t *testing.T, a, b string) float64 {
	t.Helper()
	r, ok := ContrastRatio(a, b)
	if !ok {
		t.Fatalf("ContrastRatio(%q,%q) not parseable", a, b)
	}
	return math.Round(r*100) / 100
}

// resolveCSSVar resolves a var(--name) reference (and chains, e.g.
// popover→modal→panel→app) against the flattened token map to the concrete
// color it will resolve to at runtime. The v2 surface model emits var()
// inheritance for omitted zones; the contrast gate must measure the RESOLVED
// color, not the raw var() string.
func resolveCSSVar(flat map[string]string, v string) string {
	v = strings.TrimSpace(v)
	for strings.HasPrefix(v, "var(") && strings.HasSuffix(v, ")") {
		name := strings.TrimSpace(v[4 : len(v)-1])
		next, ok := flat[name]
		if !ok {
			return v // unresolvable; let ContrastRatio surface the parse failure
		}
		v = strings.TrimSpace(next)
	}
	return v
}

// TestContrastRatio_ReferencePairs pins the WCAG formula against known
// reference values so a future refactor of the math is caught.
func TestContrastRatio_ReferencePairs(t *testing.T) {
	cases := []struct {
		name string
		a, b string
		want float64 // exact WCAG ratio
		tol  float64
	}{
		{"black on white", "#ffffff", "#000000", 21.0, 0.05},
		{"white on black", "#000000", "#ffffff", 21.0, 0.05},
		{"black on black", "#000000", "#000000", 1.0, 0.001},
		{"#777 on #fff (WCAG sample)", "#777777", "#ffffff", 4.48, 0.05},
	}
	for _, c := range cases {
		got, ok := ContrastRatio(c.a, c.b)
		if !ok {
			t.Fatalf("%s: not parseable", c.name)
		}
		if math.Abs(got-c.want) > c.tol {
			t.Errorf("%s: ContrastRatio = %.3f, want %.2f (±%.2f)", c.name, got, c.want, c.tol)
		}
	}
}

// TestContrastRatio_AcceptedColorForms ensures the harness handles every
// color grammar the validator permits (#hex variants, rgb()/rgba(), and the
// v2-first-class oklch() form).
func TestContrastRatio_AcceptedColorForms(t *testing.T) {
	cases := []struct {
		name string
		a, b string
	}{
		{"#rrggbb", "#0c0c0e", "#ffffff"},
		{"#rgb", "#fff", "#000"},
		{"#rrggbbaa (alpha dropped)", "#0c0c0eff", "#ffffffff"},
		{"rgb()", "rgb(12,12,14)", "rgb(255,255,255)"},
		{"rgba()", "rgba(12,12,14,1)", "rgba(255,255,255,1)"},
		{"rgb() percent", "rgb(5%,5%,5%)", "rgb(100%,100%,100%)"},
		{"oklch()", "oklch(0.3 0.02 250)", "oklch(0.95 0.02 250)"},
		{"oklch() with alpha", "oklch(0.3 0.02 250 / 0.5)", "#ffffff"},
	}
	for _, c := range cases {
		if _, ok := ContrastRatio(c.a, c.b); !ok {
			t.Errorf("%s: expected ok, got false (%q vs %q)", c.name, c.a, c.b)
		}
	}
	// Unparseable forms are rejected.
	for _, bad := range []string{"red", "hsl(0,0%,0%)", "url(x)", "", "not-a-color"} {
		if _, ok := ContrastRatio(bad, "#fff"); ok {
			t.Errorf("expected %q to be rejected, got ok", bad)
		}
	}
	// Malformed alpha values are rejected.
	for _, bad := range []string{"rgba(12,12,14,bad)", "rgba(12,12,14,2)", "rgba(12,12,14,-1)"} {
		if _, ok := ContrastRatio(bad, "#fff"); ok {
			t.Errorf("expected %q to be rejected (bad alpha), got ok", bad)
		}
	}
	// Malformed oklch is rejected.
	for _, bad := range []string{"oklch(0.5)", "oklch(a b c)", "oklch(0.5 0.1 250 4)"} {
		if _, ok := ContrastRatio(bad, "#fff"); ok {
			t.Errorf("expected %q to be rejected (malformed oklch), got ok", bad)
		}
	}
	// NaN/Inf in an RGB component are rejected (strconv.ParseFloat
	// accepts them with nil error; without the non-finite guard the
	// harness would coerce NaN->0 and return a bogus ratio). Alpha-NaN
	// is also rejected (parseColorRGBA validates alpha for compositing).
	for _, bad := range []string{
		"rgba(NaN,0,0,0.5)", "rgb(Inf,0,0)", "rgb(-Inf,0,0)", "rgba(12,12,14,NaN)",
	} {
		if _, ok := ContrastRatio(bad, "#fff"); ok {
			t.Errorf("expected %q to be rejected (non-finite component), got ok", bad)
		}
	}
}

// TestResolveAccentOn_TranslucentStart composites translucent accent.start
// against the app surface before picking on-ink (review: uncomposited RGB
// treats rgba(255,255,255,.1) as white → black ink on a nearly-black fill).
func TestResolveAccentOn_TranslucentStart(t *testing.T) {
	darkSurface := "#0c0c0e"
	// Translucent white on dark app → painted fill is near-black → white ink.
	got := resolveAccentOn(AccentTriple{Start: "rgba(255,255,255,0.1)"}, darkSurface)
	if got != "#ffffff" {
		t.Errorf("translucent white on dark surface: on-ink = %q, want #ffffff", got)
	}
	// Same via 8-digit hex (~0.1 alpha).
	got = resolveAccentOn(AccentTriple{Start: "#ffffff1a"}, darkSurface)
	if got != "#ffffff" {
		t.Errorf("translucent #ffffff1a on dark surface: on-ink = %q, want #ffffff", got)
	}
	// Opaque teal still prefers near-black (unchanged solid path).
	got = resolveAccentOn(AccentTriple{Start: "#0d9488"}, darkSurface)
	if got != "#0a0a0a" && got != "#000000" {
		t.Errorf("opaque teal: on-ink = %q, want dark ink", got)
	}
	// Authored On wins even for translucent start.
	got = resolveAccentOn(AccentTriple{Start: "rgba(255,255,255,0.1)", On: "#ff00ff"}, darkSurface)
	if got != "#ff00ff" {
		t.Errorf("authored On: got %q, want #ff00ff", got)
	}
	// No surface + translucent → safe dark fill → white ink.
	got = resolveAccentOn(AccentTriple{Start: "rgba(255,255,255,0.1)"}, "")
	if got != "#ffffff" {
		t.Errorf("translucent without surface: on-ink = %q, want #ffffff", got)
	}
}

// themeZonePairs enumerates, for one theme, the resolved (text, bg) pairs
// the WCAG gate measures: each of the 7 surface zones' text on its own bg
// (inherited zones resolve through Flatten before measuring), plus the
// zone-agnostic text-muted/disabled on surface-app, the accent starts on
// surface-app, and border-focus on surface-app. Used by the audit-log test
// so a future palette regression is obvious from the log.
type contrastPair struct {
	label  string
	fg, bg string
}

func themeZonePairs(t *testing.T, th *Theme) map[string][]contrastPair {
	t.Helper()
	pairs := map[string][]contrastPair{}
	for _, mode := range []string{"dark", "light"} {
		flat := th.Flatten(mode)
		appBG := flat["--color-surface-app"]
		var ps []contrastPair
		for _, z := range surfaceZones {
			bg := resolveCSSVar(flat, flat[z.cssBg])
			text := resolveCSSVar(flat, flat[z.cssText])
			ps = append(ps, contrastPair{z.name + " text on bg", text, bg})
		}
		for _, fg := range []string{flat["--color-text-muted"], flat["--color-text-disabled"]} {
			ps = append(ps, contrastPair{"text-emphasis on surface-app", fg, appBG})
		}
		for _, fg := range []string{flat["--color-accent-primary-start"], flat["--color-accent-secondary-start"]} {
			ps = append(ps, contrastPair{"accent start on surface-app", fg, appBG})
		}
		ps = append(ps, contrastPair{"border-focus on surface-app", flat["--color-border-focus"], appBG})
		pairs[mode] = ps
	}
	return pairs
}

// TestWCAG_DefaultTheme_ReportsAllRatios logs every measured ratio for the
// embedded default so the assertion thresholds below are auditable and a
// future palette regression is obvious from the log.
func TestWCAG_DefaultTheme_ReportsAllRatios(t *testing.T) {
	th, err := ParseDefault()
	if err != nil {
		t.Fatalf("ParseDefault: %v", err)
	}
	for mode, ps := range themeZonePairs(t, th) {
		for _, p := range ps {
			t.Logf("[%-5s] %-32s %s / %s = %.2f:1", mode, p.label, p.fg, p.bg, approxRatio(t, p.fg, p.bg))
		}
	}
}

// assertZoneContrast runs the v2 contrast gate for one theme across both
// modes: every zone's resolved text on its resolved bg ≥ minPrimary (4.5 AA,
// 7.0 AAA for Stark), and text-muted on surface-app ≥ 4.5 (the WCAG AA target
// for body/metadata text). Accent starts on surface-app ≥ 3.0 (AA non-text)
// are checked too. Inherited zones resolve through Flatten before measuring.
//
// Two categories are deliberately NOT hard-asserted, matching the original
// harness intent and WCAG scope:
//   - text-disabled is WCAG 1.4.3-exempt ("text that is part of an inactive
//     user interface component"). Every shipped theme intentionally renders
//     disabled text at ~2-3:1 so it READS as disabled; asserting 4.5 would
//     test against a non-applicable standard. It is logged for audit only.
//   - border-focus ≥ 3.0 (WCAG 1.4.11/2.4.11) is a hard invariant ONLY for
//     Stark, whose border-led AAA design depends on unmistakable focus rings.
//     The other themes render focus as a subtle hairline (~2.5-3:1) and are
//     not designed to that bar; they are logged for audit only.
func assertZoneContrast(t *testing.T, th *Theme) {
	t.Helper()
	minPrimary := 4.5 // WCAG AA standard
	isStark := th.ID == "silt-stark"
	if isStark {
		minPrimary = 7.0 // Stark is designed for AAA (see DESIGN.md)
	}
	for _, mode := range []string{"dark", "light"} {
		flat := th.Flatten(mode)
		for _, z := range surfaceZones {
			bg := resolveCSSVar(flat, flat[z.cssBg])
			text := resolveCSSVar(flat, flat[z.cssText])
			if r := approxRatio(t, text, bg); r < minPrimary {
				t.Errorf("%s [%s]: %s text %s on bg %s = %.2f:1, want >= %.1f",
					th.ID, mode, z.name, text, bg, r, minPrimary)
			}
			// text-muted is a GLOBAL emphasis token rendered on every surface
			// (metadata, captions, secondary text on cards/panels/modals), not
			// just the app canvas. Each zone carries (or inherits) its own
			// text-muted so a zone whose luminance differs from the app (e.g. a
			// dark sidebar in a light theme) can override it. Measure the
			// zone-resolved value against that zone's bg — otherwise muted text
			// ships unreadable on raised/tinted/dark-chrome surfaces.
			zoneMuted := resolveCSSVar(flat, flat[z.cssTextMuted])
			if r := approxRatio(t, zoneMuted, bg); r < 4.5 {
				t.Errorf("%s [%s]: text-muted %s on %s bg %s = %.2f:1, want >= 4.5 (AA). "+
					"Adjust modes.%s.text_muted (or surfaces.%s.text_muted for a dark-chrome zone).",
					th.ID, mode, zoneMuted, z.name, bg, r, mode, z.name)
			}
		}
		// text-muted on surface-app is now covered by the per-zone loop above;
		// keep an explicit app message for the actionable hint.
		appBG := resolveCSSVar(flat, flat["--color-surface-app"])
		if r := approxRatio(t, flat["--color-text-muted"], appBG); r < 4.5 {
			t.Errorf("%s [%s]: text-muted %s on surface-app = %.2f:1, want >= 4.5 (AA). "+
				"Bump modes.%s.text_muted lighter (dark) / darker (light).",
				th.ID, mode, flat["--color-text-muted"], r, mode)
		}
		// text-disabled is WCAG-exempt (inactive UI); audit-log only.
		// (see the method doc for why this is not a hard gate.)
		// Accent starts are non-text UI (focus rings, swatches, icons): ≥ 3:1.
		for _, fg := range []string{flat["--color-accent-primary-start"], flat["--color-accent-secondary-start"]} {
			if r := approxRatio(t, fg, appBG); r < 3.0 {
				t.Errorf("%s [%s]: accent %s on surface-app = %.2f:1, want >= 3.0 (AA non-text)",
					th.ID, mode, fg, r)
			}
		}
		// On-accent label ink vs accent start: solid CTA text must meet AA 4.5:1.
		for _, pair := range []struct{ on, start, label string }{
			{flat["--color-accent-primary-on"], flat["--color-accent-primary-start"], "primary"},
			{flat["--color-accent-secondary-on"], flat["--color-accent-secondary-start"], "secondary"},
			{flat["--color-text-on-accent"], flat["--color-accent-primary-start"], "text-on-accent"},
		} {
			if pair.on == "" || pair.start == "" {
				t.Errorf("%s [%s]: missing on-accent tokens for %s", th.ID, mode, pair.label)
				continue
			}
			if r := approxRatio(t, pair.on, pair.start); r < 4.5 {
				t.Errorf("%s [%s]: %s on %s start = %.2f:1, want >= 4.5 (AA text on accent)",
					th.ID, mode, pair.label, pair.start, r)
			}
		}
		// border-focus ≥ 3.0 is a hard invariant only for Stark; others are audit-only.
		focusR := approxRatio(t, flat["--color-border-focus"], appBG)
		if isStark && focusR < 3.0 {
			t.Errorf("%s [%s]: border-focus on surface-app = %.2f:1, want >= 3.0 (WCAG 2.4.11/1.4.11)",
				th.ID, mode, focusR)
		}
	}
}

// TestWCAG_FirstClassThemes_AllMeetsTargets asserts every embedded first-class
// theme meets the v2 zone-contrast matrix, and logs every measured ratio so a
// future palette regression is obvious from the test output.
func TestWCAG_FirstClassThemes_AllMeetsTargets(t *testing.T) {
	all, err := EmbeddedThemes()
	if err != nil {
		t.Fatalf("EmbeddedThemes: %v", err)
	}
	for _, th := range all {
		for mode, ps := range themeZonePairs(t, th) {
			for _, p := range ps {
				t.Logf("[%-14s %-5s] %-32s = %.2f:1", th.ID, mode, p.label, approxRatio(t, p.fg, p.bg))
			}
		}
		assertZoneContrast(t, th)
	}
}

// TestWCAG_Stark_AAA_PrimaryText keeps Stark's designated AAA invariant:
// its app-zone primary text clears 7:1 in both modes (a theme-specific
// design goal above the AA legal floor).
func TestWCAG_Stark_AAA_PrimaryText(t *testing.T) {
	th, ok := ParseEmbeddedByID("silt-stark")
	if !ok {
		t.Fatal("silt-stark not embedded")
	}
	const min = 7.0
	for _, mode := range []string{"dark", "light"} {
		flat := th.Flatten(mode)
		text := resolveCSSVar(flat, flat["--color-surface-app-text"])
		bg := flat["--color-surface-app"]
		if r := approxRatio(t, text, bg); r < min {
			t.Errorf("stark [%s]: app text %s on %s = %.2f:1, want >= %.1f (AAA)", mode, text, bg, r, min)
		}
	}
}

// TestAccentDistinctness_AllFirstClassThemes guards the docs/THEMING.md §4 rule
// that primary and secondary must be visually distinct so the "go/done" and
// "in-progress" states never blur together. We assert a minimum sRGB Euclidean
// distance between accent.primary.start and accent.secondary.start for every
// first-class theme in both modes. Reading from the v2 structs
// (m.Accent.Primary.Start) keeps the guard authoritative even if Flatten's
// token names change.
func TestAccentDistinctness_AllFirstClassThemes(t *testing.T) {
	const minDist = 30.0
	all, err := EmbeddedThemes()
	if err != nil {
		t.Fatalf("EmbeddedThemes: %v", err)
	}
	for _, th := range all {
		for _, m := range []Mode{th.Modes.Dark, th.Modes.Light} {
			d := rgbDistance(t, m.Accent.Primary.Start, m.Accent.Secondary.Start)
			if d < minDist {
				t.Errorf("%s: primary/secondary accent distance = %.1f, want >= %.1f (accents must stay distinct)",
					th.ID, d, minDist)
			}
		}
	}
}

// TestStatusErrorDistinct_AllFirstClassThemes guards the documented contract
// (theme.go Error docstring): status.danger (destructive actions) and error.fg
// (validation/invalid input) are deliberately distinct, so a destructive button
// never reads identically to an inline validation error. We assert a minimum
// sRGB Euclidean distance between the two for every first-class theme in both
// modes. Reading from the v2 structs keeps the guard authoritative even if
// Flatten's token names change.
func TestStatusErrorDistinct_AllFirstClassThemes(t *testing.T) {
	const minDist = 15.0
	all, err := EmbeddedThemes()
	if err != nil {
		t.Fatalf("EmbeddedThemes: %v", err)
	}
	for _, th := range all {
		for _, m := range []Mode{th.Modes.Dark, th.Modes.Light} {
			d := rgbDistance(t, m.Status.Danger, m.Error.FG)
			if d < minDist {
				t.Errorf("%s: status.danger %s vs error.fg %s distance = %.1f, want >= %.1f (danger and error must stay distinct)",
					th.ID, m.Status.Danger, m.Error.FG, d, minDist)
			}
		}
	}
}

// TestErrorForegroundReadable_AllFirstClassThemes guards the readability of
// error.fg, which renders as inline validation/error TEXT (role="alert"
// messages, form errors, failure banners) on the app, panel, and modal
// surfaces. The per-zone text gate does not cover error.fg (it is not a
// surface text token), so this assertion closes that gap: error.fg must clear
// AA 4.5:1 against each surface it renders on, in both modes. A theme whose
// error.fg is tuned only for distinctness from danger (e.g. a too-light tint)
// will fail here and must be darkened/shifted until it reads as text.
func TestErrorForegroundReadable_AllFirstClassThemes(t *testing.T) {
	const min = 4.5
	surfaces := []string{"app", "panel", "modal"}
	all, err := EmbeddedThemes()
	if err != nil {
		t.Fatalf("EmbeddedThemes: %v", err)
	}
	for _, th := range all {
		for _, mode := range []string{"dark", "light"} {
			flat := th.Flatten(mode)
			fg := resolveCSSVar(flat, flat["--color-error"])
			for _, z := range surfaces {
				bg := resolveCSSVar(flat, flat["--color-surface-"+z])
				if r := approxRatio(t, fg, bg); r < min {
					t.Errorf("%s [%s]: error.fg %s on %s bg %s = %.2f:1, want >= %.1f (AA text). "+
						"Darken/shift modes.%s.error.fg until it reads as text on the %s surface.",
						th.ID, mode, fg, z, bg, r, min, mode, z)
				}
			}
		}
	}
}

// TestTextPrimaryDistinctFromDefault_AllFirstClassThemes guards the #138
// regression: every first-class theme's body-text color must be perceptibly
// distinct from the default's, so switching themes produces a visibly
// different result. Reading from the v2 structs (m.Surfaces.App.Text) keeps
// the guard authoritative. See contrast_test.go history for the anchoring
// rationale (the default is the anchor because two themes are intentionally
// close by design).
func TestTextPrimaryDistinctFromDefault_AllFirstClassThemes(t *testing.T) {
	const minDist = 13.0
	all, err := EmbeddedThemes()
	if err != nil {
		t.Fatalf("EmbeddedThemes: %v", err)
	}
	defaults, ok := findByID(all, DefaultThemeID)
	if !ok {
		t.Fatalf("default theme %q not in EmbeddedThemes", DefaultThemeID)
	}
	for _, mode := range []string{"dark", "light"} {
		var anchor string
		if mode == "dark" {
			anchor = defaults.Modes.Dark.Surfaces.App.Text
		} else {
			anchor = defaults.Modes.Light.Surfaces.App.Text
		}
		for _, th := range all {
			if th.ID == DefaultThemeID {
				continue
			}
			var got string
			if mode == "dark" {
				got = th.Modes.Dark.Surfaces.App.Text
			} else {
				got = th.Modes.Light.Surfaces.App.Text
			}
			d := rgbDistance(t, anchor, got)
			if d < minDist {
				t.Errorf("%s [%s]: app text %s is only %.1f sRGB units from default %s, want >= %.1f (themes must read visibly distinct per #138)",
					th.ID, mode, got, d, anchor, minDist)
			}
		}
	}
}

// TestStatusColorsNonText_AllFirstClassThemes closes the gap that the per-zone
// text gate covers only TEXT tokens: status.warn/danger/success render chiefly
// as button fills, badges, and icons (non-text UI), so they must clear the WCAG
// 1.4.11 non-text 3:1 floor against the app surface they sit on. Reading from
// the v2 structs (m.Status.*) keeps the guard authoritative even if Flatten's
// token names change — same anchoring rationale as TestAccentDistinctness.
func TestStatusColorsNonText_AllFirstClassThemes(t *testing.T) {
	const min = 3.0
	all, err := EmbeddedThemes()
	if err != nil {
		t.Fatalf("EmbeddedThemes: %v", err)
	}
	for _, th := range all {
		for _, mode := range []string{"dark", "light"} {
			var m Mode
			if mode == "dark" {
				m = th.Modes.Dark
			} else {
				m = th.Modes.Light
			}
			flat := th.Flatten(mode)
			appBG := resolveCSSVar(flat, flat["--color-surface-app"])
			for _, c := range []struct{ leaf, color string }{
				{"warn", m.Status.Warn},
				{"danger", m.Status.Danger},
				{"success", m.Status.Success},
			} {
				if r := approxRatio(t, c.color, appBG); r < min {
					t.Errorf("%s [%s]: status.%s %s on surface-app %s = %.2f:1, want >= %.1f (AA non-text UI). "+
						"Bump modes.%s.status.%s lighter (dark) / darker (light) until badges/icons read against the app canvas.",
						th.ID, mode, c.leaf, c.color, appBG, r, min, mode, c.leaf)
				}
			}
		}
	}
}

// TestTextMutedOnInteractionStates_AllFirstClassThemes closes the gap that the
// static per-zone text gate measures text-muted only against each zone's resting
// bg: list rows and menu items repaint that bg with the hover/active overlay on
// interaction, and text-muted (secondary row text, shortcuts) must stay AA-4.5
// readable on those repainted surfaces. The interaction tokens are opaque
// zone-agnostic tints, so the text-vs-overlay ratio is measured directly against
// the resolved hover/active colors (logged with the app+panel surfaces they
// render on, since the underlying surface does not change the ratio).
func TestTextMutedOnInteractionStates_AllFirstClassThemes(t *testing.T) {
	const min = 4.5
	all, err := EmbeddedThemes()
	if err != nil {
		t.Fatalf("EmbeddedThemes: %v", err)
	}
	for _, th := range all {
		for _, mode := range []string{"dark", "light"} {
			flat := th.Flatten(mode)
			textMuted := resolveCSSVar(flat, flat["--color-text-muted"])
			appBG := resolveCSSVar(flat, flat["--color-surface-app"])
			panelBG := resolveCSSVar(flat, flat["--color-surface-panel"])
			for _, ix := range []struct{ label, token string }{
				{"hover", flat["--color-hover"]},
				{"active", flat["--color-active"]},
			} {
				overlay := resolveCSSVar(flat, ix.token)
				r := approxRatio(t, textMuted, overlay)
				t.Logf("[%-14s %-5s] text-muted %s on %s overlay %s = %.2f:1 (app %s / panel %s)",
					th.ID, mode, textMuted, ix.label, overlay, r, appBG, panelBG)
				if r < min {
					t.Errorf("%s [%s]: text-muted %s on %s overlay %s = %.2f:1, want >= %.1f (AA text). "+
						"Adjust modes.%s.text_muted or the %s tint so secondary row text stays readable on interacted rows (app + panel).",
						th.ID, mode, textMuted, ix.label, overlay, r, min, mode, ix.label)
				}
			}
		}
	}
}

// findByID returns the theme with the given id from the slice, or ok=false.
func findByID(all []*Theme, id string) (*Theme, bool) {
	for _, th := range all {
		if th.ID == id {
			return th, true
		}
	}
	return nil, false
}

// rgbDistance is the sRGB Euclidean distance between two colors. parseColorAny
// resolves any accepted v2 form (hex, rgb()/rgba(), oklch()) to sRGB first.
func rgbDistance(t *testing.T, a, b string) float64 {
	t.Helper()
	ar, ag, ab, ok := parseColorAny(a)
	if !ok {
		t.Fatalf("parseColorAny(%q) failed", a)
	}
	br, bg, bb, ok := parseColorAny(b)
	if !ok {
		t.Fatalf("parseColorAny(%q) failed", b)
	}
	dr := float64(ar) - float64(br)
	dg := float64(ag) - float64(bg)
	db := float64(ab) - float64(bb)
	return math.Sqrt(dr*dr + dg*dg + db*db)
}

// TestDeriveInkOnAccent pins omit-path ink picks for common accent fills (#624).
func TestDeriveInkOnAccent(t *testing.T) {
	// cyber_forest light primary teal — white fails AA; dark ink required.
	teal := DeriveInkOnAccent("#0d9488")
	if teal != "#0a0a0a" && teal != "#000000" {
		t.Errorf("teal ink = %q, want near/pure black", teal)
	}
	if r, ok := ContrastRatio(teal, "#0d9488"); !ok || r < 4.5 {
		t.Errorf("teal ink contrast = %.2f, want >= 4.5", r)
	}

	// Near-black fill → white label.
	if got := DeriveInkOnAccent("#0a0a0a"); got != "#ffffff" {
		t.Errorf("near-black fill ink = %q, want #ffffff", got)
	}

	// Medium indigo often needs pure black when near-black is short of 4.5.
	indigo := DeriveInkOnAccent("#6366f1")
	if indigo != "#0a0a0a" && indigo != "#000000" {
		t.Errorf("indigo ink = %q, want near/pure black", indigo)
	}
	if r, ok := ContrastRatio(indigo, "#6366f1"); !ok || r < 4.5 {
		t.Errorf("indigo ink contrast = %.2f, want >= 4.5", r)
	}
}
