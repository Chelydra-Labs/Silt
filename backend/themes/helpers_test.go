package themes

import (
	"math"
	"os"
	"strings"
	"testing"
	"time"
)

// writeBytes is a tiny helper for writing raw bytes (used by cache tests
// that need to write broken JSON to test the invalid-file fallback).
func writeBytes(t *testing.T, path string, b []byte) error {
	t.Helper()
	if err := os.MkdirAll(dirOf(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func dirOf(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			return p[:i]
		}
	}
	return "."
}

// touchFile sets the mtime of path to t.
func touchFile(path string, t time.Time) error {
	return os.Chtimes(path, t, t)
}

// --- v2 shared fixtures -----------------------------------------------------
//
// The v2 schema is breaking: a Mode now carries a 7-zone Surfaces tree
// (only app required), zone-agnostic interaction tokens, a required
// status.success, and a required error family. These helpers return
// minimally-valid v2 objects so every test fixture stays DRY and a mutation
// test can clone + tweak one field without re-stating the whole schema.

// requiredModeTokens is the canonical per-mode token set Validate requires
// (every other field is optional). Used by the "all required tokens flagged"
// test to pin the v2 required set exactly: 3 app-surface colors + 6
// zone-agnostic tokens + 6 accent-triple colors + 3 status colors + 3 error
// colors = 21.
var requiredModeTokens = []string{
	"surfaces.app.bg", "surfaces.app.border", "surfaces.app.text",
	"hover", "active", "border_active", "border_focus", "text_muted", "text_disabled",
	"accent.primary.start", "accent.primary.end", "accent.primary.glow",
	"accent.secondary.start", "accent.secondary.end", "accent.secondary.glow",
	"status.warn", "status.danger", "status.success",
	"error.fg", "error.bg", "error.border",
}

// validV2Mode returns a minimally-valid dark Mode: only the app surface is
// authored (the other six zones inherit from app via Flatten), all required
// top-level tokens are set, and every color is valid hex. Clone and mutate
// one field to exercise a specific validation failure path.
func validV2Mode() Mode {
	return Mode{
		Surfaces: Surfaces{
			App: Surface{BG: "#0c0c0e", Border: "#1e1e23", Text: "#dee3e6"},
		},
		Hover:        "#1c1c21",
		Active:       "#222226",
		BorderActive: "#3f3f46",
		BorderFocus:  "#52525b",
		TextMuted:    "#8b8b94",
		TextDisabled: "#4b5563",
		Accent: Accent{
			Primary:   AccentTriple{Start: "#2dd4bf", End: "#0d9488", Glow: "rgba(20, 184, 166, 0.15)"},
			Secondary: AccentTriple{Start: "#6366f1", End: "#a855f7", Glow: "rgba(168, 85, 247, 0.12)"},
		},
		Status: Status{Warn: "#fbbf24", Danger: "#f43f5e", Success: "#22c55e"},
		Error:  Error{FG: "#ffb4ab", BG: "#93000a", Border: "#7d2a2a"},
	}
}

// validV2ModeLight returns a minimally-valid light Mode (light palette so
// dark/light flattens to visibly different tokens).
func validV2ModeLight() Mode {
	return Mode{
		Surfaces: Surfaces{
			App: Surface{BG: "#f8fafc", Border: "#e2e8f0", Text: "#0f172a"},
		},
		Hover:        "#e2e8f0",
		Active:       "#cbd5e1",
		BorderActive: "#94a3b8",
		BorderFocus:  "#64748b",
		TextMuted:    "#4d5667",
		TextDisabled: "#94a3b8",
		Accent: Accent{
			Primary:   AccentTriple{Start: "#0d9488", End: "#115e59", Glow: "rgba(13, 148, 136, 0.10)"},
			Secondary: AccentTriple{Start: "#4f46e5", End: "#7c3aed", Glow: "rgba(79, 70, 229, 0.08)"},
		},
		Status: Status{Warn: "#d97706", Danger: "#e11d48", Success: "#16a34a"},
		Error:  Error{FG: "#ba1a1a", BG: "#ffdad6", Border: "#93000a"},
	}
}

// validV2Theme returns a minimally-valid v2 Theme (id "test-theme", both
// modes populated) for tests that need a parsed theme without touching the
// embedded first-class set.
func validV2Theme() *Theme {
	return &Theme{
		SchemaVersion: SupportedSchemaVersion,
		ID:            "test-theme",
		Name:          "Test Theme",
		Author:        "Tester",
		Description:   "test",
		Modes:         Modes{Dark: validV2Mode(), Light: validV2ModeLight()},
	}
}

// --- color-math helpers (shared by derivation + contrast tests) -------------

// oklabLightness returns the Oklab L component (0–1) of any accepted theme
// color form. Used by the derivation tests to assert hover lightens and
// active darkens regardless of the seed's authored format.
func oklabLightness(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "oklch(") {
		lch, ok := parseOKLCH(s)
		if !ok {
			return 0, false
		}
		return lch.L, true
	}
	if lab, ok := hexToOklab(s); ok {
		return lab.L, true
	}
	if r, g, b, ok := ToSRGB(s); ok {
		return sRGBToOklab(r, g, b).L, true
	}
	return 0, false
}

// oklabChroma returns the Oklab chroma (sqrt(a^2+b^2)) of a color, used to
// assert DeriveDisabled desaturates the seed.
func oklabChroma(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "oklch(") {
		lch, ok := parseOKLCH(s)
		if !ok {
			return 0, false
		}
		return lch.C, true
	}
	if lab, ok := hexToOklab(s); ok {
		return math.Sqrt(lab.a*lab.a + lab.b*lab.b), true
	}
	if r, g, b, ok := ToSRGB(s); ok {
		lab := sRGBToOklab(r, g, b)
		return math.Sqrt(lab.a*lab.a + lab.b*lab.b), true
	}
	return 0, false
}
