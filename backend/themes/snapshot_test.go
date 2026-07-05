package themes

import (
	"slices"
	"sort"
	"strings"
	"testing"
)

// expectedV2ColorKeys is the always-emitted v2 color-token set: the 7 surface
// zones × 3 (bg/border/text), the 4 zone-agnostic interaction tokens, the 3
// text-emphasis levels, 6 accent colors, 3 status colors, 3 error colors, and
// the 2 Material-3 aliases that map onto the app zone.
var expectedV2ColorKeys = []string{
	// 7 surface zones × 5 (bg/border/text/text-muted/text-disabled).
	"--color-surface-app", "--color-surface-app-border", "--color-surface-app-text", "--color-surface-app-text-muted", "--color-surface-app-text-disabled",
	"--color-surface-sidebar", "--color-surface-sidebar-border", "--color-surface-sidebar-text", "--color-surface-sidebar-text-muted", "--color-surface-sidebar-text-disabled",
	"--color-surface-editor", "--color-surface-editor-border", "--color-surface-editor-text", "--color-surface-editor-text-muted", "--color-surface-editor-text-disabled",
	"--color-surface-panel", "--color-surface-panel-border", "--color-surface-panel-text", "--color-surface-panel-text-muted", "--color-surface-panel-text-disabled",
	"--color-surface-card", "--color-surface-card-border", "--color-surface-card-text", "--color-surface-card-text-muted", "--color-surface-card-text-disabled",
	"--color-surface-modal", "--color-surface-modal-border", "--color-surface-modal-text", "--color-surface-modal-text-muted", "--color-surface-modal-text-disabled",
	"--color-surface-popover", "--color-surface-popover-border", "--color-surface-popover-text", "--color-surface-popover-text-muted", "--color-surface-popover-text-disabled",
	// Zone-agnostic interaction tokens.
	"--color-hover", "--color-active", "--color-border-active", "--color-border-focus",
	// Text-emphasis levels.
	"--color-text-primary", "--color-text-muted", "--color-text-disabled",
	// Accents.
	"--color-accent-primary-start", "--color-accent-primary-end", "--color-accent-primary-glow",
	"--color-accent-secondary-start", "--color-accent-secondary-end", "--color-accent-secondary-glow",
	// Status.
	"--color-status-warn", "--color-status-danger", "--color-status-success",
	// Themeable error family.
	"--color-error", "--color-error-bg", "--color-error-border",
}

// expectedV2GeometryKeys is the always-emitted geometry ramp (Flatten emits
// sensible defaults when a theme omits radius/spacing/shadow).
var expectedV2GeometryKeys = []string{
	"--radius-sm", "--radius-md", "--radius-lg", "--radius-xl", "--radius-full",
	"--spacing-sm", "--spacing-md", "--spacing-lg", "--spacing-xl",
	"--shadow-sm", "--shadow-md", "--shadow-lg",
}

// expectedV2EditorKeys is the always-emitted editor-canvas interaction block
// (Flatten derives defaults from the active palette when a theme omits it).
var expectedV2EditorKeys = []string{
	"--color-editor-caret", "--color-editor-selection", "--color-editor-selection-text",
	"--color-editor-link", "--color-editor-link-hover", "--color-editor-highlight",
}

// expectedV2TypographyKeys are the theme-level font keys Flatten emits when a
// theme carries a typography block (the default does).
var expectedV2TypographyKeys = []string{
	"--font-body", "--font-mono", "--font-headline",
}

// isBackgroundOverlayKey reports whether a token is one of the opt-in
// --silt-bg-<zone>-* overlay keys Flatten emits only for surfaces that
// declare a background block.
func isBackgroundOverlayKey(k string) bool {
	return strings.HasPrefix(k, "--silt-bg-")
}

// isTypeScaleKey reports whether a token is one of the optional type-scale
// keys (--font-size-*/--line-height-*/--font-weight-*) a typography.scale
// block may emit.
func isTypeScaleKey(k string) bool {
	return strings.HasPrefix(k, "--font-size-") ||
		strings.HasPrefix(k, "--line-height-") ||
		strings.HasPrefix(k, "--font-weight-")
}

// TestDefaultTheme_FlattensV2TokenSet asserts the embedded default flattens
// to exactly the v2 canonical token set: every always-emitted color, geometry,
// and editor key is present, plus the default's typography fonts; the only
// allowed extras are the optional type-scale and background-overlay keys; and
// NO legacy v1 token (--color-void/surface/panel/chrome-*) survives.
func TestDefaultTheme_FlattensV2TokenSet(t *testing.T) {
	th, err := ParseDefault()
	if err != nil {
		t.Fatalf("embedded default is invalid: %v", err)
	}
	if th.SchemaVersion != SupportedSchemaVersion {
		t.Errorf("default schema_version = %q, want %q", th.SchemaVersion, SupportedSchemaVersion)
	}
	for _, c := range []string{"dark", "light"} {
		got := th.Flatten(c)
		// Every always-emitted key must be present.
		for _, k := range expectedV2ColorKeys {
			if _, ok := got[k]; !ok {
				t.Errorf("%s mode: missing canonical v2 token %s", c, k)
			}
		}
		for _, k := range expectedV2GeometryKeys {
			if _, ok := got[k]; !ok {
				t.Errorf("%s mode: missing geometry token %s", c, k)
			}
		}
		for _, k := range expectedV2EditorKeys {
			if _, ok := got[k]; !ok {
				t.Errorf("%s mode: missing editor token %s", c, k)
			}
		}
		// The default ships a typography block, so its fonts are present.
		for _, k := range expectedV2TypographyKeys {
			if _, ok := got[k]; !ok {
				t.Errorf("%s mode: missing typography token %s", c, k)
			}
		}
		// Any extra key must be an allowed optional (background overlay or
		// type-scale), never a stray legacy token.
		for k := range got {
			if slices.Contains(expectedV2ColorKeys, k) ||
				slices.Contains(expectedV2GeometryKeys, k) ||
				slices.Contains(expectedV2EditorKeys, k) ||
				slices.Contains(expectedV2TypographyKeys, k) ||
				isBackgroundOverlayKey(k) || isTypeScaleKey(k) {
				continue
			}
			t.Errorf("%s mode: unexpected token %s (v2 emits only the canonical set + optional background/type-scale)", c, k)
		}
		// Legacy v1 tokens must NOT survive the v2 Flatten.
		for _, legacy := range []string{
			"--color-void", "--color-surface", "--color-panel",
			"--color-border-muted", "--color-border-zinc", "--color-chrome-void",
			"--silt-texture-image", "--silt-texture-display",
		} {
			if _, ok := got[legacy]; ok {
				t.Errorf("%s mode: legacy v1 token %s must not be emitted", c, legacy)
			}
		}
	}
}

// TestDefaultTheme_GoldenSnapshot value-level pin. The v2 default
// (cyber_forest.json) is being re-authored natively in parallel; once the
// v2 values are finalized, pin them here exactly so any future drift fails
// with a precise diff. For now the token-set guard above covers structure.

// TestFirstClassThemes_FlattenShape pins the structural contract for every
// non-default first-class theme: both modes flatten to the canonical v2
// color/geometry/editor token set, the typography block is present, and any
// extra key is the opt-in background overlay (--silt-bg-*) or the type-scale.
func TestFirstClassThemes_FlattenShape(t *testing.T) {
	all, err := EmbeddedThemes()
	if err != nil {
		t.Fatalf("EmbeddedThemes: %v", err)
	}
	for _, th := range all {
		if th.ID == DefaultThemeID {
			continue
		}
		if th.Typography == nil {
			t.Errorf("%s: expected a typography block", th.ID)
		}
		for _, mode := range []string{"dark", "light"} {
			flat := th.Flatten(mode)
			// Every canonical v2 color token must be present.
			for _, k := range expectedV2ColorKeys {
				if _, ok := flat[k]; !ok {
					t.Errorf("%s [%s]: missing token %s", th.ID, mode, k)
				}
			}
			for _, k := range expectedV2GeometryKeys {
				if _, ok := flat[k]; !ok {
					t.Errorf("%s [%s]: missing geometry token %s", th.ID, mode, k)
				}
			}
			for _, k := range expectedV2EditorKeys {
				if _, ok := flat[k]; !ok {
					t.Errorf("%s [%s]: missing editor token %s", th.ID, mode, k)
				}
			}
			// Typography family tokens are emitted when authored; all
			// first-class themes author them, so absence is a Flatten regression.
			for _, k := range expectedV2TypographyKeys {
				if _, ok := flat[k]; !ok {
					t.Errorf("%s [%s]: missing typography token %s", th.ID, mode, k)
				}
			}
			// Any extra key must be the opt-in background overlay or
			// type-scale, not a stray or legacy token.
			for k := range flat {
				switch {
				case slices.Contains(expectedV2ColorKeys, k),
					slices.Contains(expectedV2GeometryKeys, k),
					slices.Contains(expectedV2EditorKeys, k),
					slices.Contains(expectedV2TypographyKeys, k),
					isBackgroundOverlayKey(k),
					isTypeScaleKey(k):
					// allowed canonical or optional key
				default:
					t.Errorf("%s [%s]: unexpected token %s (only --silt-bg-* and type-scale are allowed extras)",
						th.ID, mode, k)
				}
			}
		}
	}
}

// TestBackgroundDisplay_GatedByBackgroundBlock pins the --silt-bg-<zone>-display
// contract: a surface that declares a background block flips its overlay on
// (display:block), and a surface without one emits nothing. This is the exact
// behavior the overlay's display:var(--silt-bg-<zone>-display,none) relies on;
// a regression here would either hide a theme's texture/photo or force a
// composited layer onto every non-background surface.
func TestBackgroundDisplay_GatedByBackgroundBlock(t *testing.T) {
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
			for _, z := range surfaceZones {
				s := z.get(m.Surfaces)
				key := "--silt-bg-" + z.name + "-display"
				_, hasDisplay := flat[key]
				if s != nil && s.Background != nil {
					if !hasDisplay {
						t.Errorf("%s [%s] %s: surface with a background block must emit %s", th.ID, mode, z.name, key)
					} else if flat[key] != "block" {
						t.Errorf("%s [%s] %s: %s = %q, want \"block\"", th.ID, mode, z.name, key, flat[key])
					}
				} else {
					if hasDisplay {
						t.Errorf("%s [%s] %s: surface without a background block must NOT emit %s (got %q)",
							th.ID, mode, z.name, key, flat[key])
					}
				}
			}
		}
	}
}

// --- retained helpers (kept for the value-level golden snapshot to come) -----

func assertTokenMap(t *testing.T, mode string, want, got map[string]string) {
	t.Helper()
	keys := make(map[string]struct{}, len(want))
	for k := range want {
		keys[k] = struct{}{}
	}
	for k := range got {
		keys[k] = struct{}{}
	}
	sorted := make([]string, 0, len(keys))
	for k := range keys {
		sorted = append(sorted, k)
	}
	sort.Strings(sorted)
	var b strings.Builder
	mismatch := 0
	for _, k := range sorted {
		w, wantOK := want[k]
		g, gotOK := got[k]
		if !wantOK {
			mismatch++
			b.WriteString("\n  + " + k + " = " + g + " (unexpected token in theme)")
			continue
		}
		if !gotOK {
			mismatch++
			b.WriteString("\n  - " + k + " (missing from theme)")
			continue
		}
		if w != g {
			mismatch++
			b.WriteString("\n  ~ " + k + ": want " + w + ", got " + g)
		}
	}
	if mismatch > 0 {
		t.Errorf("%s mode: theme drifted from the golden snapshot (%d token(s) changed):%s\n"+
			"If the change is intentional, update the golden map in snapshot_test.go.",
			mode, mismatch, b.String())
	}
}

func titleCase(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
