package themes

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mustWriteTheme writes a JSON theme file into dir.
func mustWriteTheme(t *testing.T, dir, name, json string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(json), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// TestParseAndValidate_RejectsOversize pins the F12 defense-in-depth cap: a
// theme JSON larger than the budget is rejected before Unmarshal allocates.
func TestParseAndValidate_RejectsOversize(t *testing.T) {
	_, err := ParseAndValidate(make([]byte, maxThemeJSONBytes+1))
	if err == nil {
		t.Fatal("expected oversize theme JSON to be rejected")
	}
	if !strings.Contains(err.Error(), "exceeds the") {
		t.Errorf("error %q must mention the byte cap", err.Error())
	}
}

// TestLoadTheme_RejectsOversize pins the F12 read-side cap: a hostile theme
// file on disk is rejected without unbounded allocation.
func TestLoadTheme_RejectsOversize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "huge.json")
	if err := os.WriteFile(path, make([]byte, maxThemeJSONBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadTheme(path)
	if err == nil {
		t.Fatal("expected oversize theme file to be rejected")
	}
	if !strings.Contains(err.Error(), "exceeds the") {
		t.Errorf("error %q must mention the byte cap", err.Error())
	}
}

// minimalValidJSON is a structurally-valid canonical v2 theme (both modes,
// app surface only, required status.success + error family) used as the base
// for mutation in tests. v2 is hard-enforced: schema_version must be "2.0.0".
const minimalValidJSON = `{
  "schema_version": "2.0.0",
  "id": "test-theme",
  "name": "Test Theme",
  "author": "Tester",
  "description": "test",
  "modes": {
    "dark": {
      "surfaces": {"app": {"bg": "#0c0c0e", "border": "#1e1e23", "text": "#dee3e6"}},
      "hover": "#1c1c21",
      "active": "#222226",
      "border_active": "#3f3f46",
      "border_focus": "#52525b",
      "text_muted": "#8b8b94",
      "text_disabled": "#4b5563",
      "accent": {
        "primary": {"start": "#2dd4bf", "end": "#0d9488", "glow": "rgba(20, 184, 166, 0.15)"},
        "secondary": {"start": "#6366f1", "end": "#a855f7", "glow": "rgba(168, 85, 247, 0.12)"}
      },
      "status": {"warn": "#fbbf24", "danger": "#f43f5e", "success": "#22c55e"},
      "error": {"fg": "#ffb4ab", "bg": "#93000a", "border": "#7d2a2a"}
    },
    "light": {
      "surfaces": {"app": {"bg": "#f8fafc", "border": "#e2e8f0", "text": "#0f172a"}},
      "hover": "#e2e8f0",
      "active": "#cbd5e1",
      "border_active": "#94a3b8",
      "border_focus": "#64748b",
      "text_muted": "#4d5667",
      "text_disabled": "#94a3b8",
      "accent": {
        "primary": {"start": "#0d9488", "end": "#115e59", "glow": "rgba(13, 148, 136, 0.10)"},
        "secondary": {"start": "#4f46e5", "end": "#7c3aed", "glow": "rgba(79, 70, 229, 0.08)"}
      },
      "status": {"warn": "#d97706", "danger": "#e11d48", "success": "#16a34a"},
      "error": {"fg": "#ba1a1a", "bg": "#ffdad6", "border": "#93000a"}
    }
  }
}`

func TestValidate_ValidTheme(t *testing.T) {
	th, err := ParseAndValidate([]byte(minimalValidJSON))
	if err != nil {
		t.Fatalf("expected valid, got %v", err)
	}
	if th.ID != "test-theme" {
		t.Errorf("id mismatch: %q", th.ID)
	}
	if th.SchemaVersion != SupportedSchemaVersion {
		t.Errorf("schema_version = %q, want %q", th.SchemaVersion, SupportedSchemaVersion)
	}
}

func TestValidate_MissingToken(t *testing.T) {
	bad := strings.Replace(minimalValidJSON, `"#2dd4bf"`, `""`, 1)
	_, err := ParseAndValidate([]byte(bad))
	if err == nil {
		t.Fatalf("expected validation error for missing token, got nil")
	}
	verrs, ok := err.(ValidationErrors)
	if !ok {
		t.Fatalf("expected ValidationErrors, got %T", err)
	}
	found := false
	for _, e := range verrs {
		if strings.Contains(e.Field, "accent.primary.start") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected an error on accent.primary.start, got %v", verrs)
	}
}

// TestValidate_MissingRequiredTokens pins each of the v2-required slots:
// omitting any one is rejected with the matching field path. v2 made
// status.success and the entire error family required (the v1 optional
// paths are retired), so each gets a dedicated case.
func TestValidate_MissingRequiredTokens(t *testing.T) {
	cases := []struct {
		name  string
		field string // substring expected in the reported Field
		from  string // exact source token to blank
	}{
		{"surfaces.app.bg", "surfaces.app.bg", `"#0c0c0e"`},
		{"hover", "hover", `"#1c1c21"`},
		{"border_focus", "border_focus", `"#52525b"`},
		{"status.success", "status.success", `"#22c55e"`},
		{"error.fg", "error.fg", `"#ffb4ab"`},
		{"error.bg", "error.bg", `"#93000a"`},
		{"error.border", "error.border", `"#7d2a2a"`},
		{"text_muted", "text_muted", `"#8b8b94"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			bad := strings.Replace(minimalValidJSON, c.from, `""`, 1)
			_, err := ParseAndValidate([]byte(bad))
			if err == nil {
				t.Fatalf("expected validation error for missing %s", c.field)
			}
			verrs, ok := err.(ValidationErrors)
			if !ok {
				t.Fatalf("expected ValidationErrors, got %T", err)
			}
			found := false
			for _, e := range verrs {
				if strings.HasSuffix(e.Field, "."+c.field) || strings.HasSuffix(e.Field, c.field) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("expected an error on %s, got %v", c.field, verrs)
			}
		})
	}
}

func TestValidate_BadColor(t *testing.T) {
	bad := strings.Replace(minimalValidJSON, `"#2dd4bf"`, `"not-a-color"`, 1)
	_, err := ParseAndValidate([]byte(bad))
	if err == nil {
		t.Fatalf("expected validation error for bad color, got nil")
	}
}

// TestValidate_BadColors covers the malformed-color paths across every
// accepted grammar: bad hex, malformed oklch, oklch with too few components,
// and rgb out of range. oklch is now a first-class color form in v2.
func TestValidate_BadColors(t *testing.T) {
	cases := map[string]string{
		"bad hex":           `"#gggggg"`,
		"short hex":         `"#ff"`,
		"oklch too few":     `"oklch(0.5)"`,
		"oklch non-numeric": `"oklch(a b c)"`,
		"oklch too many":    `"oklch(0.5 0.1 250 4)"`,
		"named color":       `"red"`,
		"hsl (unsupported)": `"hsl(0,0%,0%)"`,
		"rgb out of range":  `"rgb(300,0,0)"`,
		"rgba alpha > 1":    `"rgba(0,0,0,2)"`,
		"url() injection":   `"url(http://evil.example/x)"`,
	}
	for name, val := range cases {
		t.Run(name, func(t *testing.T) {
			bad := strings.Replace(minimalValidJSON, `"#2dd4bf"`, val, 1)
			_, err := ParseAndValidate([]byte(bad))
			if err == nil {
				t.Errorf("expected validation error for %s (%s)", name, val)
			}
		})
	}
}

func TestValidate_MissingIdentity(t *testing.T) {
	bad := strings.Replace(minimalValidJSON, `"id": "test-theme"`, `"id": ""`, 1)
	_, err := ParseAndValidate([]byte(bad))
	if err == nil {
		t.Fatalf("expected validation error for missing id, got nil")
	}
	if !strings.Contains(err.Error(), "id is required") {
		t.Fatalf("expected id-required error, got %v", err)
	}
}

func TestValidate_UnparseableJSON(t *testing.T) {
	_, err := ParseAndValidate([]byte("{not json"))
	if err == nil {
		t.Fatalf("expected parse error, got nil")
	}
}

// TestValidate_RejectsWrongSchemaVersion pins the v2 hard-enforcement:
// schema_version is NOT informational anymore. A theme carrying any other
// version (including the legacy v1 "1.0.0" and a hypothetical future "9.9.9")
// is rejected outright so a versioned theme never loads as a silently-wrong v2.
func TestValidate_RejectsWrongSchemaVersion(t *testing.T) {
	for _, v := range []string{`"1.0.0"`, `"9.9.9"`, `"2.0.1"`, `"1.5.0"`} {
		t.Run(v, func(t *testing.T) {
			bad := strings.Replace(minimalValidJSON, `"schema_version": "2.0.0"`, `"schema_version": `+v, 1)
			_, err := ParseAndValidate([]byte(bad))
			if err == nil {
				t.Fatalf("expected schema_version %s to be rejected", v)
			}
			if !strings.Contains(err.Error(), "schema_version") {
				t.Errorf("expected schema_version in error, got: %v", err)
			}
		})
	}
}

// TestValidate_MissingSchemaVersionRejected: a missing schema_version is an
// empty string, which is not SupportedSchemaVersion, so it is rejected
// (rather than silently treated as a default).
func TestValidate_MissingSchemaVersionRejected(t *testing.T) {
	bad := strings.Replace(minimalValidJSON, `"schema_version": "2.0.0",`, ``, 1)
	_, err := ParseAndValidate([]byte(bad))
	if err == nil {
		t.Fatal("expected validation error for missing schema_version")
	}
	if !strings.Contains(err.Error(), "schema_version") {
		t.Fatalf("expected schema_version in error, got: %v", err)
	}
}

// TestValidate_RejectsUnknownJSONFields pins the DisallowUnknownFields gate:
// a typo like "borde" or a leftover v1 key (bg/border/text/texture) is now a
// hard parse error, not a silently-dropped token. This is the v2 contract
// that makes "my theme is missing a token" debuggable.
func TestValidate_RejectsUnknownJSONFields(t *testing.T) {
	cases := map[string]string{
		"typo":           strings.Replace(minimalValidJSON, `"border_active": "#3f3f46"`, `"borde_active": "#3f3f46"`, 1),
		"v1 bg block":    strings.Replace(minimalValidJSON, `"surfaces":`, `"bg": {"void": "#000000"}, "surfaces":`, 1),
		"v1 texture":     strings.Replace(minimalValidJSON, `"status":`, `"texture": {"image": "url(x)"}, "status":`, 1),
		"top-level typo": strings.Replace(minimalValidJSON, `"author": "Tester"`, `"authr": "Tester"`, 1),
	}
	for name, bad := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ParseAndValidate([]byte(bad))
			if err == nil {
				t.Fatalf("expected unknown-field rejection for %s", name)
			}
			if !strings.Contains(err.Error(), "not parseable") && !strings.Contains(err.Error(), "unknown") {
				t.Errorf("expected a parse/unknown-field error, got: %v", err)
			}
		})
	}
}

// darkOnlyJSON is a structurally-valid v2 dark theme with NO light mode
// object. The validator must report every required token under modes.light
// as missing (a zero-valued Mode struct has empty token fields, each of
// which fails the required-token check).
const darkOnlyJSON = `{
  "schema_version": "2.0.0",
  "id": "test-theme",
  "name": "Test Theme",
  "modes": {
    "dark": {
      "surfaces": {"app": {"bg": "#0c0c0e", "border": "#1e1e23", "text": "#dee3e6"}},
      "hover": "#1c1c21",
      "active": "#222226",
      "border_active": "#3f3f46",
      "border_focus": "#52525b",
      "text_muted": "#8b8b94",
      "text_disabled": "#4b5563",
      "accent": {
        "primary": {"start": "#2dd4bf", "end": "#0d9488", "glow": "rgba(20, 184, 166, 0.15)"},
        "secondary": {"start": "#6366f1", "end": "#a855f7", "glow": "rgba(168, 85, 247, 0.12)"}
      },
      "status": {"warn": "#fbbf24", "danger": "#f43f5e", "success": "#22c55e"},
      "error": {"fg": "#ffb4ab", "bg": "#93000a", "border": "#7d2a2a"}
    }
  }
}`

// TestValidate_MissingLightMode: a theme that defines only modes.dark must
// be rejected with every required modes.light token reported as missing.
// The set of flagged tokens must exactly equal the v2 required set
// (requiredModeTokens) — no more, no less.
func TestValidate_MissingLightMode(t *testing.T) {
	_, err := ParseAndValidate([]byte(darkOnlyJSON))
	if err == nil {
		t.Fatal("expected validation error for a theme missing modes.light")
	}
	verrs, ok := err.(ValidationErrors)
	if !ok {
		t.Fatalf("expected ValidationErrors, got %T: %v", err, err)
	}
	got := map[string]bool{}
	for _, e := range verrs {
		if !strings.HasPrefix(e.Field, "modes.light.") {
			t.Errorf("unexpected non-light error: %+v", e)
			continue
		}
		got[strings.TrimPrefix(e.Field, "modes.light.")] = true
	}
	for _, tok := range requiredModeTokens {
		if !got[tok] {
			t.Errorf("expected required light token %q to be flagged, got %v", tok, got)
		}
	}
	if len(got) != len(requiredModeTokens) {
		t.Errorf("expected exactly %d distinct required light tokens flagged, got %d (%v)",
			len(requiredModeTokens), len(got), got)
	}
}

func TestIsValidColor(t *testing.T) {
	good := []string{
		"#fff", "#ffffff", "#ffffffff",
		"rgba(0,0,0,0.5)", "rgba(0, 0, 0, 0)", "rgba(255,255,255,1)",
		"rgb(1,2,3)", "rgb(100%, 0%, 0%)",
		// v2 first-class oklch grammar.
		"oklch(0.5 0.1 250)",
		"oklch(0.5 0.1 250 / 0.5)",
		"oklch(50% 0.1 250)",
		"oklch(0.8 0 0)",
	}
	for _, c := range good {
		if !isValidColor(c) {
			t.Errorf("isValidColor(%q) = false, want true", c)
		}
	}
	bad := []string{
		"", "white", "#ff", "#gggggg", "hsl(0,0%,0%)",
		"rgba(0,0,0)",       // missing alpha
		"rgba(999,0,0,0.5)", // rgb component out of range
		"rgba(0,0,0,2)",     // alpha > 1
		"rgba(0,0,0,-1)",    // alpha < 0
		"rgb(1,2,3,4)",      // too many components
		"rgb(300,0,0)",      // out of range
		"rgba(a,b,c,d)",     // non-numeric
		// Malformed oklch.
		"oklch(0.5)",           // too few components
		"oklch(0.5 0.1)",       // too few components
		"oklch(a b c)",         // non-numeric
		"oklch(0.5 0.1 250 4)", // too many components
		// NaN/Inf: strconv.ParseFloat accepts them with a nil error, and
		// NaN range comparisons (v < 0 || v > 255) are both false, so
		// without an explicit non-finite guard these slip through the
		// schema sandbox (#48).
		"rgba(NaN,0,0,0.5)",  // NaN rgb component
		"rgba(12,12,14,NaN)", // NaN alpha channel
		"rgb(Inf,0,0)",       // +Inf component
		"rgb(-Inf,0,0)",      // -Inf component
	}
	for _, c := range bad {
		if isValidColor(c) {
			t.Errorf("isValidColor(%q) = true, want false", c)
		}
	}
}

func TestParseDefault_IsValid(t *testing.T) {
	th, err := ParseDefault()
	if err != nil {
		t.Fatalf("embedded default is invalid: %v", err)
	}
	if th.ID != DefaultThemeID {
		t.Errorf("default id = %q, want %q", th.ID, DefaultThemeID)
	}
	if th.SchemaVersion != SupportedSchemaVersion {
		t.Errorf("default schema_version = %q, want %q", th.SchemaVersion, SupportedSchemaVersion)
	}
	// Flatten must produce the v2 app surface tokens.
	tokens := th.Flatten("dark")
	for _, k := range []string{
		"--color-surface-app", "--color-surface-app-border", "--color-surface-app-text",
		"--color-hover", "--color-active", "--color-border-active", "--color-border-focus",
		"--color-text-primary", "--color-text-muted", "--color-text-disabled",
		"--color-error", "--color-error-bg", "--color-error-border",
	} {
		if _, ok := tokens[k]; !ok {
			t.Errorf("Flatten missing %s", k)
		}
	}
	// v1 tokens must NOT survive.
	for _, k := range []string{"--color-void", "--color-surface", "--color-chrome-void"} {
		if _, ok := tokens[k]; ok {
			t.Errorf("Flatten must not emit legacy v1 token %s", k)
		}
	}
}

func TestFlatten_DarkLightDiffer(t *testing.T) {
	th := validV2Theme()
	dark := th.Flatten("dark")
	light := th.Flatten("light")
	if dark["--color-surface-app"] == light["--color-surface-app"] {
		t.Errorf("dark/light app bg should differ (dark=%s light=%s)",
			dark["--color-surface-app"], light["--color-surface-app"])
	}
}

func TestBGVoid(t *testing.T) {
	th := validV2Theme()
	if got, want := th.BGVoid("dark"), th.Modes.Dark.Surfaces.App.BG; got != want {
		t.Errorf("BGVoid dark = %s, want %s", got, want)
	}
	if got, want := th.BGVoid("light"), th.Modes.Light.Surfaces.App.BG; got != want {
		t.Errorf("BGVoid light = %s, want %s", got, want)
	}
	if got, want := th.BGVoid("system"), th.Modes.Dark.Surfaces.App.BG; got != want {
		t.Errorf("BGVoid system should resolve to dark app bg: %s, want %s", got, want)
	}
}

// firstClassIDs is the curated roster of embedded first-class theme ids.
// A test pins this exactly so an accidental addition/removal of a shipped
// theme is caught (the picker's first-party set is an intentional product
// decision, not a side effect of the embed).
var firstClassIDs = map[string]bool{
	DefaultThemeID:    true,
	"silt-terra-noir": true,
	"silt-linen":      true,
	"silt-stark":      true,
	"silt-graphite":   true,
	"silt-bubblegum":  true,
	"silt-frost":      true,
	"silt-synthwave":  true,
	"silt-daybreak":   true,
	"silt-aggie":      true,
	"silt-altgeld":    true,
}

// assertEmbeddedSet asserts that res contains exactly the embedded
// first-class roster, with the primary default labeled source="default"
// and every other first-class theme source="bundled", and that
// FlatTokens carries a dark+light map per id.
func assertEmbeddedSet(t *testing.T, res *ListThemesResult) {
	t.Helper()
	if got, want := len(res.Themes), len(firstClassIDs); got != want {
		t.Fatalf("expected %d embedded first-class themes, got %d: %+v", want, got, res.Themes)
	}
	for _, ti := range res.Themes {
		if !firstClassIDs[ti.ID] {
			t.Errorf("unexpected theme id %q in embedded-only listing", ti.ID)
		}
		wantSrc := "bundled"
		if ti.ID == DefaultThemeID {
			wantSrc = "default"
		}
		if ti.Source != wantSrc {
			t.Errorf("theme %q source = %q, want %q", ti.ID, ti.Source, wantSrc)
		}
		ft, ok := res.FlatTokens[ti.ID]
		if !ok {
			t.Errorf("theme %q missing FlatTokens", ti.ID)
			continue
		}
		if len(ft.Dark) == 0 || len(ft.Light) == 0 {
			t.Errorf("theme %q has empty FlatTokens (dark=%d light=%d)", ti.ID, len(ft.Dark), len(ft.Light))
		}
	}
}

func TestListThemes_EmptyDir(t *testing.T) {
	dir := t.TempDir() // exists but empty
	res, err := ListThemes(dir)
	if err != nil {
		t.Fatalf("ListThemes empty dir: %v", err)
	}
	// Empty dir → the full embedded first-class roster.
	assertEmbeddedSet(t, res)
}

func TestListThemes_MissingDir(t *testing.T) {
	// A nonexistent themes dir (fresh vault before scaffold) is not an
	// error and yields the embedded first-class roster.
	res, err := ListThemes(filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil {
		t.Fatalf("ListThemes missing dir: %v", err)
	}
	assertEmbeddedSet(t, res)
}

func TestListThemes_EmptyPath(t *testing.T) {
	// An empty themesDir (no vault open yet) must not call os.ReadDir("") and
	// must still yield the embedded first-class roster rather than erroring.
	res, err := ListThemes("")
	if err != nil {
		t.Fatalf("ListThemes empty path: %v", err)
	}
	assertEmbeddedSet(t, res)
}

func TestListThemes_OnDiskPlusMalformed(t *testing.T) {
	dir := t.TempDir()
	mustWriteTheme(t, dir, "custom.json", minimalValidJSON)
	mustWriteTheme(t, dir, "broken.json", "{not json")

	res, err := ListThemes(dir)
	if err != nil {
		t.Fatalf("ListThemes: %v", err)
	}
	ids := map[string]bool{}
	for _, ti := range res.Themes {
		ids[ti.ID] = true
	}
	if !ids["test-theme"] {
		t.Fatalf("expected on-disk test-theme, got %v", ids)
	}
	for id := range firstClassIDs {
		if !ids[id] {
			t.Errorf("expected embedded first-class theme %q, got %v", id, ids)
		}
	}
	if len(res.Themes) != 1+len(firstClassIDs) {
		t.Errorf("expected %d themes (1 on-disk + %d embedded), got %d", 1+len(firstClassIDs), len(firstClassIDs), len(res.Themes))
	}
	if len(res.Errors) != 1 || !strings.Contains(res.Errors[0].File, "broken.json") {
		t.Fatalf("expected 1 load error for broken.json, got %+v", res.Errors)
	}
}

func TestResolveActive_KnownID(t *testing.T) {
	dir := t.TempDir()
	mustWriteTheme(t, dir, "custom.json", minimalValidJSON)
	t1, err := ResolveActive(dir, "test-theme", "dark")
	if err != nil {
		t.Fatalf("ResolveActive known id: %v", err)
	}
	if t1.ID != "test-theme" {
		t.Errorf("resolved id = %q, want test-theme", t1.ID)
	}
}

func TestResolveActive_UnknownID_FallsBackToDefault(t *testing.T) {
	dir := t.TempDir()
	t1, err := ResolveActive(dir, "no-such-id", "dark")
	if err != nil {
		t.Fatalf("ResolveActive unknown id: %v", err)
	}
	if t1.ID != DefaultThemeID {
		t.Errorf("expected fallback to default, got %q", t1.ID)
	}
}

func TestResolveActive_EmptyID_FallsBackToDefault(t *testing.T) {
	dir := t.TempDir()
	t1, err := ResolveActive(dir, "", "dark")
	if err != nil {
		t.Fatalf("ResolveActive empty id: %v", err)
	}
	if t1.ID != DefaultThemeID {
		t.Errorf("expected default, got %q", t1.ID)
	}
}

func TestHexToRGB(t *testing.T) {
	cases := []struct {
		in      string
		r, g, b uint8
		ok      bool
	}{
		{"#0c0c0e", 12, 12, 14, true},
		{"#ffffff", 255, 255, 255, true},
		{"#ffffffff", 255, 255, 255, true}, // 8-digit (alpha ignored)
		{"#0c0c0eff", 12, 12, 14, true},    // 8-digit w/ alpha → matches #0c0c0e
		{"#fff", 255, 255, 255, true},
		{"#000", 0, 0, 0, true},
		{" #0c0c0e ", 12, 12, 14, true},
		{"nope", 0, 0, 0, false},
		{"#ff", 0, 0, 0, false},
		{"#gggggg", 0, 0, 0, false},
	}
	for _, c := range cases {
		r, g, b, ok := HexToRGB(c.in)
		if ok != c.ok || r != c.r || g != c.g || b != c.b {
			t.Errorf("HexToRGB(%q) = (%d,%d,%d,%v), want (%d,%d,%d,%v)",
				c.in, r, g, b, ok, c.r, c.g, c.b, c.ok)
		}
	}
}

// --- Typography tests (v2 keeps typography as a theme-level optional block) --

func TestValidate_TypographyOptional(t *testing.T) {
	// A theme without a typography section must still validate (backward compat).
	th, err := ParseAndValidate([]byte(minimalValidJSON))
	if err != nil {
		t.Fatalf("theme without typography should validate: %v", err)
	}
	if th.Typography != nil {
		t.Errorf("expected nil Typography, got %+v", th.Typography)
	}
}

func TestValidate_TypographyValid(t *testing.T) {
	withTypo := strings.Replace(
		minimalValidJSON,
		`"modes": {`,
		`"typography": {
      "font_family": "'Inter', sans-serif",
      "mono_font_family": "'JetBrains Mono', monospace",
      "headline_font": "'Hanken Grotesk', sans-serif"
    },
    "modes": {`,
		1,
	)
	th, err := ParseAndValidate([]byte(withTypo))
	if err != nil {
		t.Fatalf("valid typography should pass: %v", err)
	}
	if th.Typography == nil {
		t.Fatal("expected non-nil Typography")
	}
	if th.Typography.FontFamily != "'Inter', sans-serif" {
		t.Errorf("FontFamily = %q", th.Typography.FontFamily)
	}
}

func TestValidate_TypographyRejectsCSSInjection(t *testing.T) {
	bad := []string{
		"'Inter'; body { background: red",
		"'Inter'} body{",
		"'Inter'<script>alert(1)</script>",
		"'Inter'>bad",
	}
	for _, v := range bad {
		withBad := strings.Replace(
			minimalValidJSON,
			`"modes": {`,
			`"typography": { "font_family": "`+v+`" },
    "modes": {`,
			1,
		)
		_, err := ParseAndValidate([]byte(withBad))
		if err == nil {
			t.Errorf("expected validation error for font_family %q", v)
		}
	}
}

// TestValidate_BackgroundRejectsCSSInjection covers the validateBackground
// security barrier — the background.image value flows verbatim into a CSS
// background-image inside the :root{--name:value;} injection context, so it
// must reject declaration-breaking characters (;, {, }, raw <, >, and a
// backslash CSS-escape), out-of-range opacity, unrecognized blend modes, a
// bad scrim color, and an unrecognized size enum. Mirrors the sibling
// typography barrier. Calls validateBackground directly for precise unit
// coverage of every rejection path, then proves the full ParseAndValidate
// pipeline rejects a crafted theme file end-to-end.
func TestValidate_BackgroundRejectsCSSInjection(t *testing.T) {
	// A valid background block must pass (cover photo with a scrim).
	valid := &Background{
		Image: "url(data:image/svg+xml,%3Csvg%3E%3C/svg%3E)",
		Size:  "cover", Opacity: 0.5, Blend: "overlay", Scrim: "#000000",
	}
	if err := validateBackground("modes.dark.surfaces.app.background", valid); err != nil {
		t.Fatalf("valid background should pass, got %v", err)
	}
	// Empty image is allowed: a scrim-only or blend-only overlay is meaningful.
	if err := validateBackground("modes.dark.surfaces.app.background", &Background{Opacity: 0.5, Blend: "overlay", Scrim: "#111111"}); err != nil {
		t.Errorf("background with no image but a scrim should pass, got %v", err)
	}

	// image: reject every CSS-injection character.
	badImages := []string{
		"url(data:); body{background:red}",    // ; { }
		"url(data:)}body{",                    // } {
		"url(data:)<script>alert(1)</script>", // < >
		"url(data:)>bad",                      // >
		`url(data:)\escape`,                   // backslash (CSS escape sequence)
	}
	for _, img := range badImages {
		b := &Background{Image: img, Opacity: 0.5, Blend: "overlay"}
		if err := validateBackground("modes.dark.surfaces.app.background", b); err == nil {
			t.Errorf("expected validation error for background.image %q", img)
		}
	}

	// size: must be a recognized mode (tile/cover/contain).
	if err := validateBackground("modes.dark.surfaces.app.background", &Background{Image: "url(data:x)", Size: "stretch"}); err == nil {
		t.Errorf("expected validation error for background.size %q", "stretch")
	}
	for _, sz := range []string{"tile", "cover", "contain"} {
		b := &Background{Image: "url(data:x)", Size: sz, Opacity: 0.5, Blend: "overlay"}
		if err := validateBackground("modes.dark.surfaces.app.background", b); err != nil {
			t.Errorf("size %q should pass, got %v", sz, err)
		}
	}

	// opacity: must be a number in [0,1].
	badOpacities := []float64{1.5, -0.1}
	for _, op := range badOpacities {
		b := &Background{Image: "url(data:x)", Opacity: op, Blend: "overlay"}
		if err := validateBackground("modes.dark.surfaces.app.background", b); err == nil {
			t.Errorf("expected validation error for background.opacity %v", op)
		}
	}
	for _, op := range []float64{0, 0.06, 1} {
		b := &Background{Image: "url(data:x)", Opacity: op, Blend: "overlay"}
		if err := validateBackground("modes.dark.surfaces.app.background", b); err != nil {
			t.Errorf("opacity %v should pass, got %v", op, err)
		}
	}

	// blend: must be a recognized mix-blend-mode keyword.
	if err := validateBackground("modes.dark.surfaces.app.background", &Background{Image: "url(data:x)", Opacity: 0.5, Blend: "bogus-blend"}); err == nil {
		t.Errorf("expected validation error for background.blend %q", "bogus-blend")
	}
	for _, bl := range []string{"overlay", "multiply", "normal", "soft-light"} {
		b := &Background{Image: "url(data:x)", Opacity: 0.5, Blend: bl}
		if err := validateBackground("modes.dark.surfaces.app.background", b); err != nil {
			t.Errorf("blend %q should pass, got %v", bl, err)
		}
	}

	// scrim: must be a valid color.
	if err := validateBackground("modes.dark.surfaces.app.background", &Background{Image: "url(data:x)", Opacity: 0.5, Scrim: "not-a-color"}); err == nil {
		t.Errorf("expected validation error for background.scrim %q", "not-a-color")
	}

	// End-to-end: a crafted theme JSON with an injection image on the app
	// surface is rejected by the full ParseAndValidate pipeline.
	crafted := strings.Replace(
		minimalValidJSON,
		`{"bg": "#0c0c0e", "border": "#1e1e23", "text": "#dee3e6"}`,
		`{"bg": "#0c0c0e", "border": "#1e1e23", "text": "#dee3e6", "background": {"image": "url(x); body{background:red}", "size": "cover", "opacity": 0.5, "blend": "overlay"}}`,
		1,
	)
	if _, err := ParseAndValidate([]byte(crafted)); err == nil {
		t.Errorf("expected ParseAndValidate to reject a crafted theme with an injection background.image")
	}
}

func TestValidate_TypographyPartial(t *testing.T) {
	// Only headline_font defined — other fields are optional.
	partial := strings.Replace(
		minimalValidJSON,
		`"modes": {`,
		`"typography": { "headline_font": "'Playfair Display', serif" },
    "modes": {`,
		1,
	)
	th, err := ParseAndValidate([]byte(partial))
	if err != nil {
		t.Fatalf("partial typography should pass: %v", err)
	}
	if th.Typography.HeadlineFont != "'Playfair Display', serif" {
		t.Errorf("HeadlineFont = %q", th.Typography.HeadlineFont)
	}
	if th.Typography.FontFamily != "" {
		t.Errorf("FontFamily should be empty, got %q", th.Typography.FontFamily)
	}
}

func TestFlatten_TypographyEmittedWhenPresent(t *testing.T) {
	withTypo := strings.Replace(
		minimalValidJSON,
		`"modes": {`,
		`"typography": {
      "font_family": "'Inter', sans-serif",
      "headline_font": "'Hanken Grotesk', sans-serif"
    },
    "modes": {`,
		1,
	)
	th, err := ParseAndValidate([]byte(withTypo))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	dark := th.Flatten("dark")
	if dark["--font-body"] != "'Inter', sans-serif" {
		t.Errorf("--font-body = %q", dark["--font-body"])
	}
	if dark["--font-headline"] != "'Hanken Grotesk', sans-serif" {
		t.Errorf("--font-headline = %q", dark["--font-headline"])
	}
	if _, ok := dark["--font-mono"]; ok {
		t.Errorf("--font-mono should be absent (mono_font_family not set)")
	}
}

func TestFlatten_TypographyAbsentWhenNoSection(t *testing.T) {
	th, err := ParseAndValidate([]byte(minimalValidJSON))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	dark := th.Flatten("dark")
	for _, key := range []string{"--font-body", "--font-mono", "--font-headline"} {
		if _, ok := dark[key]; ok {
			t.Errorf("%s should be absent when theme has no typography section", key)
		}
	}
}

func TestIsValidFontFamily(t *testing.T) {
	good := []string{
		"'Inter', sans-serif",
		"'JetBrains Mono', monospace",
		"serif",
		"Georgia, 'Times New Roman', serif",
		"system-ui",
	}
	bad := []string{
		"'Inter'; body{",
		"'Inter'} div{",
		"'><script>",
		// CSS escape-sequence bypass: \3B resolves to ; at CSS-parse time.
		"'Inter'\\3B background:red;/*",
		"'Inter'\\7D body{",
	}
	for _, v := range good {
		if !isValidFontFamily(v) {
			t.Errorf("expected %q to be valid", v)
		}
	}
	for _, v := range bad {
		if isValidFontFamily(v) {
			t.Errorf("expected %q to be rejected", v)
		}
	}
}

// --- Derivation tests (v2 OKLCH-aware hover/active/disabled) ----------------

// TestDeriveHover_PreservesFormatAndLightens asserts DeriveHover returns the
// seed's authored format (hex→hex, oklch→oklch) and is perceptibly lighter
// (delta-L > 0) in the Oklab lightness axis.
func TestDeriveHover_PreservesFormatAndLightens(t *testing.T) {
	hexSeed := "#0c0c0e"
	hexHover := DeriveHover(hexSeed)
	if !strings.HasPrefix(hexHover, "#") {
		t.Errorf("DeriveHover(hex) = %q, want hex output", hexHover)
	}
	seedL, _ := oklabLightness(hexSeed)
	hoverL, _ := oklabLightness(hexHover)
	if hoverL <= seedL {
		t.Errorf("DeriveHover should lighten: seed L=%.4f, hover L=%.4f (delta %.4f)",
			seedL, hoverL, hoverL-seedL)
	}

	oklchSeed := "oklch(0.3 0.05 250)"
	oklchHover := DeriveHover(oklchSeed)
	if !strings.HasPrefix(oklchHover, "oklch(") {
		t.Errorf("DeriveHover(oklch) = %q, want oklch output", oklchHover)
	}
	seedL2, _ := oklabLightness(oklchSeed)
	hoverL2, _ := oklabLightness(oklchHover)
	if hoverL2 <= seedL2 {
		t.Errorf("DeriveHover(oklch) should lighten: seed L=%.4f, hover L=%.4f", seedL2, hoverL2)
	}
}

// TestDeriveActive_PreservesFormatAndDarkens asserts DeriveActive returns the
// seed's format and is perceptibly deeper (delta-L < 0).
func TestDeriveActive_PreservesFormatAndDarkens(t *testing.T) {
	seed := "#8b8b94"
	active := DeriveActive(seed)
	if !strings.HasPrefix(active, "#") {
		t.Errorf("DeriveActive(hex) = %q, want hex output", active)
	}
	seedL, _ := oklabLightness(seed)
	activeL, _ := oklabLightness(active)
	if activeL >= seedL {
		t.Errorf("DeriveActive should darken: seed L=%.4f, active L=%.4f", seedL, activeL)
	}

	oklchSeed := "oklch(0.6 0.05 250)"
	oklchActive := DeriveActive(oklchSeed)
	if !strings.HasPrefix(oklchActive, "oklch(") {
		t.Errorf("DeriveActive(oklch) = %q, want oklch output", oklchActive)
	}
	seedL2, _ := oklabLightness(oklchSeed)
	activeL2, _ := oklabLightness(oklchActive)
	if activeL2 >= seedL2 {
		t.Errorf("DeriveActive(oklch) should darken: seed L=%.4f, active L=%.4f", seedL2, activeL2)
	}
}

// TestDeriveDisabled_PreservesFormatAndDesaturates asserts DeriveDisabled
// keeps the format and lowers chroma (the a/b opponents pulled toward grey).
func TestDeriveDisabled_PreservesFormatAndDesaturates(t *testing.T) {
	seed := "#2dd4bf" // a chromatic teal
	dis := DeriveDisabled(seed)
	if !strings.HasPrefix(dis, "#") {
		t.Errorf("DeriveDisabled(hex) = %q, want hex output", dis)
	}
	seedC, _ := oklabChroma(seed)
	disC, _ := oklabChroma(dis)
	if disC >= seedC {
		t.Errorf("DeriveDisabled should desaturate: seed C=%.4f, disabled C=%.4f", seedC, disC)
	}

	oklchSeed := "oklch(0.7 0.15 180)"
	oklchDis := DeriveDisabled(oklchSeed)
	if !strings.HasPrefix(oklchDis, "oklch(") {
		t.Errorf("DeriveDisabled(oklch) = %q, want oklch output", oklchDis)
	}
	seedC2, _ := oklabChroma(oklchSeed)
	disC2, _ := oklabChroma(oklchDis)
	if disC2 >= seedC2 {
		t.Errorf("DeriveDisabled(oklch) should desaturate: seed C=%.4f, disabled C=%.4f", seedC2, disC2)
	}
}

// --- Flatten structure tests (v2 surface zones + format preservation) --------

// TestFlatten_EmitsSurfaceAppAndAliases pins the v2 canonical surface tokens
// and the Material-3 aliases that map onto the app zone.
func TestFlatten_EmitsSurfaceAppAndAliases(t *testing.T) {
	th := validV2Theme()
	flat := th.Flatten("dark")
	if flat["--color-surface-app"] != "#0c0c0e" {
		t.Errorf("--color-surface-app = %q, want #0c0c0e", flat["--color-surface-app"])
	}
	if flat["--color-surface-app-border"] != "#1e1e23" {
		t.Errorf("--color-surface-app-border = %q", flat["--color-surface-app-border"])
	}
	if flat["--color-surface-app-text"] != "#dee3e6" {
		t.Errorf("--color-surface-app-text = %q", flat["--color-surface-app-text"])
	}
	// text-primary is a first-class semantic emphasis token: primary body
	// text, by definition the app zone's foreground (parallel to text-muted /
	// text-disabled, which are zone-agnostic emphasis levels).
	if flat["--color-text-primary"] != "var(--color-surface-app-text)" {
		t.Errorf("--color-text-primary = %q, want var(--color-surface-app-text)", flat["--color-text-primary"])
	}
}

// TestFlatten_OmittedZoneInheritsParent pins the strict-tree inheritance
// graph emitted by Flatten for zones the author did not set. validV2Theme
// authors only app, so every other zone falls back to its parent via var().
func TestFlatten_OmittedZoneInheritsParent(t *testing.T) {
	th := validV2Theme()
	flat := th.Flatten("dark")
	// Direct app children.
	for _, z := range []string{"sidebar", "editor", "panel"} {
		if got, want := flat["--color-surface-"+z], "var(--color-surface-app)"; got != want {
			t.Errorf("--color-surface-%s = %q, want %q", z, got, want)
		}
	}
	// panel's children.
	if got, want := flat["--color-surface-card"], "var(--color-surface-panel)"; got != want {
		t.Errorf("--color-surface-card = %q, want %q", got, want)
	}
	if got, want := flat["--color-surface-modal"], "var(--color-surface-panel)"; got != want {
		t.Errorf("--color-surface-modal = %q, want %q", got, want)
	}
	// popover inherits modal.
	if got, want := flat["--color-surface-popover"], "var(--color-surface-modal)"; got != want {
		t.Errorf("--color-surface-popover = %q, want %q", got, want)
	}
}

// TestFlatten_AuthoredZoneEmitsConcreteValue confirms a zone the author DID
// set emits concrete values (not var()) and the background overlay keys flip.
func TestFlatten_AuthoredZoneEmitsConcreteValue(t *testing.T) {
	th := validV2Theme()
	th.Modes.Dark.Surfaces.Panel = &Surface{
		BG: "#161619", Border: "#27272a", Text: "#dee3e6",
		Background: &Background{Image: "url(x)", Size: "cover", Opacity: 0.4, Blend: "overlay"},
	}
	flat := th.Flatten("dark")
	if flat["--color-surface-panel"] != "#161619" {
		t.Errorf("--color-surface-panel = %q, want #161619", flat["--color-surface-panel"])
	}
	if flat["--silt-bg-panel-display"] != "block" {
		t.Errorf("--silt-bg-panel-display = %q, want block", flat["--silt-bg-panel-display"])
	}
	if flat["--silt-bg-panel-image"] != "url(x)" {
		t.Errorf("--silt-bg-panel-image = %q", flat["--silt-bg-panel-image"])
	}
	// card still inherits panel (we only authored panel, not card).
	if flat["--color-surface-card"] != "var(--color-surface-panel)" {
		t.Errorf("--color-surface-card = %q, want var(--color-surface-panel)", flat["--color-surface-card"])
	}
}

// TestFlatten_OKLCHVerbatimAndHexVerbatim pins the format-preservation rule:
// OKLCH-authored colors flatten verbatim (the author wrote oklch, Flatten
// emits oklch), and hex flattens verbatim. Derivation is eager but Flatten
// itself is a pass-through — no format coercion.
func TestFlatten_OKLCHVerbatimAndHexVerbatim(t *testing.T) {
	th := validV2Theme()
	th.Modes.Dark.Surfaces.App.BG = "oklch(0.3 0.05 250)"
	th.Modes.Dark.Surfaces.App.Text = "#ffffff"
	th.Modes.Dark.Accent.Primary.Start = "oklch(0.7 0.15 180)"
	flat := th.Flatten("dark")
	if flat["--color-surface-app"] != "oklch(0.3 0.05 250)" {
		t.Errorf("oklch app bg should flatten verbatim, got %q", flat["--color-surface-app"])
	}
	if flat["--color-surface-app-text"] != "#ffffff" {
		t.Errorf("hex app text should flatten verbatim, got %q", flat["--color-surface-app-text"])
	}
	if flat["--color-accent-primary-start"] != "oklch(0.7 0.15 180)" {
		t.Errorf("oklch accent should flatten verbatim, got %q", flat["--color-accent-primary-start"])
	}
}
