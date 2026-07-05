package themes

import (
	"strings"
	"testing"
)

// TestEmbeddedThemes_RosterAndValid pins the curated first-class set: every
// embedded theme parses + validates, the roster is exactly the known ids, and
// each carries both modes + the default typography block (Sprint 8 themes ship
// with fonts rather than inheriting config defaults).
func TestEmbeddedThemes_RosterAndValid(t *testing.T) {
	all, err := EmbeddedThemes()
	if err != nil {
		t.Fatalf("EmbeddedThemes: %v", err)
	}
	if len(all) != len(firstClassIDs) {
		t.Fatalf("expected %d embedded themes, got %d", len(firstClassIDs), len(all))
	}
	for _, th := range all {
		if !firstClassIDs[th.ID] {
			t.Errorf("unexpected embedded theme id %q", th.ID)
		}
		if th.Typography == nil {
			t.Errorf("embedded theme %q missing typography block", th.ID)
			continue
		}
		if th.Typography.FontFamily == "" || th.Typography.MonoFontFamily == "" || th.Typography.HeadlineFont == "" {
			t.Errorf("embedded theme %q has an incomplete typography block: %+v", th.ID, th.Typography)
		}
		// Both modes must flatten to the full token set.
		if len(th.Flatten("dark")) == 0 || len(th.Flatten("light")) == 0 {
			t.Errorf("embedded theme %q has an empty mode flatten", th.ID)
		}
	}
}

// TestParseEmbeddedByID resolves each first-class id directly from embed and
// confirms an unknown id returns ok=false (rather than the default).
func TestParseEmbeddedByID(t *testing.T) {
	for id := range firstClassIDs {
		th, ok := ParseEmbeddedByID(id)
		if !ok {
			t.Errorf("ParseEmbeddedByID(%q) = false, want true", id)
			continue
		}
		if th.ID != id {
			t.Errorf("ParseEmbeddedByID(%q) returned id %q", id, th.ID)
		}
	}
	if _, ok := ParseEmbeddedByID("not-a-real-theme"); ok {
		t.Error("ParseEmbeddedByID(unknown) = true, want false")
	}
	if _, ok := ParseEmbeddedByID(""); ok {
		t.Error("ParseEmbeddedByID(\"\") = true, want false")
	}
}

// TestEmbeddedThemeFiles_UsedByScaffold confirms every embedded theme is keyed
// by a "<id>.json" filename so ScaffoldVault writes one file per first-class
// theme with no gaps or extras.
func TestEmbeddedThemeFiles_UsedByScaffold(t *testing.T) {
	files, err := EmbeddedThemeFiles()
	if err != nil {
		t.Fatalf("EmbeddedThemeFiles: %v", err)
	}
	if len(files) != len(firstClassIDs) {
		t.Fatalf("expected %d files, got %d", len(firstClassIDs), len(files))
	}
	for id := range firstClassIDs {
		fn := id + ".json"
		raw, ok := files[fn]
		if !ok {
			t.Errorf("missing embedded theme file %q", fn)
			continue
		}
		if len(raw) == 0 {
			t.Errorf("embedded theme file %q is empty", fn)
		}
	}
}

// TestListThemes_EmbedWinsOverVaultForFirstClass: when the vault holds a
// same-id cyber_forest.json (a legacy ScaffoldVault seed or a manual copy),
// the embedded first-class copy is authoritative — the on-disk file is
// ignored, the listing's cyber_forest is source="default", and FlatTokens
// reflect the embedded values, not the stale on-disk sentinel. Custom ids
// still resolve from disk as before.
func TestListThemes_EmbedWinsOverVaultForFirstClass(t *testing.T) {
	dir := t.TempDir()
	// A cyber_forest variant whose surfaces.app.bg differs from the embedded
	// default so we can prove the on-disk copy is NOT the one selected.
	edited := strings.Replace(minimalValidJSON, `"id": "test-theme"`, `"id": "cyber_forest"`, 1)
	edited = strings.Replace(edited, `"#0c0c0e"`, `"#abcdef"`, 1) // dark surfaces.app.bg → sentinel
	mustWriteTheme(t, dir, "cyber_forest.json", edited)
	// A genuinely custom id alongside it must still resolve from disk.
	custom := strings.Replace(minimalValidJSON, `"id": "test-theme"`, `"id": "terra-test"`, 1)
	mustWriteTheme(t, dir, "terra-test.json", custom)

	res, err := ListThemes(dir)
	if err != nil {
		t.Fatalf("ListThemes: %v", err)
	}
	var cf *ThemeInfo
	var customTI *ThemeInfo
	for i := range res.Themes {
		switch res.Themes[i].ID {
		case DefaultThemeID:
			cf = &res.Themes[i]
		case "terra-test":
			customTI = &res.Themes[i]
		}
	}
	if cf == nil {
		t.Fatalf("cyber_forest not in listing: %+v", res.Themes)
	}
	if cf.Source != "default" {
		t.Errorf("cyber_forest source = %q, want \"default\" (embed wins over vault shadow)", cf.Source)
	}
	// The embedded dark surfaces.app.bg (#0e0f12) must be surfaced, NOT the
	// on-disk sentinel #abcdef — proves the on-disk shadow was ignored.
	if got := res.FlatTokens[DefaultThemeID].Dark["--color-surface-app"]; got != "#0e0f12" {
		t.Errorf("cyber_forest dark surface-app = %q, want #0e0f12 (embedded, not vault shadow %q)", got, "#abcdef")
	}
	// Custom id still resolves from disk (source="disk").
	if customTI == nil {
		t.Fatalf("custom terra-test not in listing: %+v", res.Themes)
	}
	if customTI.Source != "disk" {
		t.Errorf("custom terra-test source = %q, want \"disk\"", customTI.Source)
	}
}

// TestResolveActive_FirstClassEmbeddedOffDisk: a non-default first-class active
// id resolves from embed when the themes dir is empty, so a wiped dir (or an
// existing vault scaffolded before the theme shipped) does not flash the
// default palette on launch.
func TestResolveActive_FirstClassEmbeddedOffDisk(t *testing.T) {
	dir := t.TempDir() // empty: nothing on disk
	for id := range firstClassIDs {
		if id == DefaultThemeID {
			continue
		}
		th, err := ResolveActive(dir, id, "dark")
		if err != nil {
			t.Errorf("ResolveActive(%q): %v", id, err)
			continue
		}
		if th.ID != id {
			t.Errorf("ResolveActive(%q) returned id %q", id, th.ID)
		}
	}
	// An unknown id still falls back to the default (never bricks the app).
	th, err := ResolveActive(dir, "no-such-id", "dark")
	if err != nil {
		t.Fatalf("ResolveActive(unknown): %v", err)
	}
	if th.ID != DefaultThemeID {
		t.Errorf("unknown id resolved to %q, want default %q", th.ID, DefaultThemeID)
	}
}

// TestCachedThemeByID_FirstClassEmbeddedOffDisk: the launch-path cache resolves
// a non-default first-class id from embed when it is not on disk, removing the
// "non-default active theme flashes the default" regression for first-class
// themes (the #73 analog). A totally unknown id still falls back to default.
func TestCachedThemeByID_FirstClassEmbeddedOffDisk(t *testing.T) {
	ResetCacheForTests()
	dir := t.TempDir() // empty
	th, err := CachedThemeByID(dir, "silt-graphite")
	if err != nil {
		t.Fatalf("CachedThemeByID(silt-graphite) off-disk: %v", err)
	}
	if th.ID != "silt-graphite" {
		t.Errorf("CachedThemeByID(silt-graphite) = %q, want silt-graphite", th.ID)
	}
	// Pre-vault (empty themesDir) also resolves first-class from embed.
	ResetCacheForTests()
	th, err = CachedThemeByID("", "silt-stark")
	if err != nil {
		t.Fatalf("CachedThemeByID(silt-stark) pre-vault: %v", err)
	}
	if th.ID != "silt-stark" {
		t.Errorf("CachedThemeByID(silt-stark) pre-vault = %q, want silt-stark", th.ID)
	}
	// Unknown id → default fallback.
	ResetCacheForTests()
	th, err = CachedThemeByID(dir, "totally-unknown")
	if err != nil {
		t.Fatalf("CachedThemeByID(unknown): %v", err)
	}
	if th.ID != DefaultThemeID {
		t.Errorf("CachedThemeByID(unknown) = %q, want default", th.ID)
	}
}

// TestEmbeddedThemes_DeterministicOrder guards the by-id sort so
// ListThemes/ScaffoldVault behave identically across runs.
func TestEmbeddedThemes_DeterministicOrder(t *testing.T) {
	a, _ := EmbeddedThemes()
	b, _ := EmbeddedThemes()
	if len(a) != len(b) {
		t.Fatalf("non-deterministic length: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i].ID != b[i].ID {
			t.Fatalf("non-deterministic order: pos %d %q vs %q", i, a[i].ID, b[i].ID)
		}
	}
}
