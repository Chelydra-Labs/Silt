package themes

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestMain provides test isolation for the process-local cache: clear
// it before and after the suite so pointer-identity assertions and
// entry-count checks are deterministic regardless of execution order.
func TestMain(m *testing.M) {
	ResetCacheForTests()
	code := m.Run()
	ResetCacheForTests()
	os.Exit(code)
}

func TestCachedThemeByID_EmptyIDFallsBackToDefault(t *testing.T) {
	ResetCacheForTests()
	th, err := CachedThemeByID(t.TempDir(), "")
	if err != nil {
		t.Fatalf("CachedThemeByID: %v", err)
	}
	if th == nil || th.ID != DefaultThemeID {
		t.Errorf("expected embedded default, got %+v", th)
	}
}

func TestCachedThemeByID_BuiltInIDFallsBackToDefault(t *testing.T) {
	th, err := CachedThemeByID(t.TempDir(), DefaultThemeID)
	if err != nil {
		t.Fatalf("CachedThemeByID: %v", err)
	}
	if th == nil || th.ID != DefaultThemeID {
		t.Errorf("expected embedded default, got %+v", th)
	}
}

func TestCachedThemeByID_UnknownIDFallsBackToDefault(t *testing.T) {
	// No vault loaded, no file on disk — fall back to the embedded
	// default rather than failing the launch. The active id might be
	// stale (file deleted); the first-paint color is then the shipped
	// default, which is always safe.
	th, err := CachedThemeByID(t.TempDir(), "no-such-theme")
	if err != nil {
		t.Fatalf("CachedThemeByID: %v", err)
	}
	if th == nil || th.ID != DefaultThemeID {
		t.Errorf("expected embedded default, got %+v", th)
	}
}

func TestCachedThemeByID_EmptyThemesDirFallsBackToDefault(t *testing.T) {
	th, err := CachedThemeByID("", "anything")
	if err != nil {
		t.Fatalf("CachedThemeByID: %v", err)
	}
	if th == nil || th.ID != DefaultThemeID {
		t.Errorf("expected embedded default, got %+v", th)
	}
}

// TestCachedThemeByID_RejectsPathTraversalIDs: ids containing path
// separators, parent-dir references, or NUL must fall back to the
// embedded default rather than constructing a path outside themesDir
// (CWE-22). Each case is verified by placing a canary file at the
// would-be traversal target; if the path-traversal was honoured the
// canary would be loaded and the test would see its id.
func TestCachedThemeByID_RejectsPathTraversalIDs(t *testing.T) {
	themesDir := t.TempDir()
	canaryDir := t.TempDir()
	canaryID := "canary"
	canaryPath := filepath.Join(canaryDir, canaryID+".json")
	mustWriteTheme(t, filepath.Dir(canaryPath), filepath.Base(canaryPath), validCustomThemeJSON)

	cases := []string{
		"../" + filepath.Base(canaryDir) + "/" + canaryID, // escapes themesDir upward
		canaryID + "/../" + canaryID,                      // uses separator + parent ref
		"foo\\bar",                                        // backslash separator
		canaryID + "\x00suffix",                           // NUL truncation attempt
		"./" + canaryID,                                   // leading "./" — file is at id, not ./id
	}
	for _, id := range cases {
		t.Run(id, func(t *testing.T) {
			th, err := CachedThemeByID(themesDir, id)
			if err != nil {
				t.Fatalf("CachedThemeByID(%q): %v", id, err)
			}
			if th == nil || th.ID == canaryID {
				t.Errorf("CachedThemeByID(%q) loaded the canary; path traversal succeeded (theme=%+v)", id, th)
			}
			if th.ID != DefaultThemeID {
				t.Errorf("CachedThemeByID(%q) = %q, want default", id, th.ID)
			}
		})
	}
}

func TestCachedThemeByID_LoadsFromDisk(t *testing.T) {
	themesDir := t.TempDir()
	src := filepath.Join(t.TempDir(), "src.json")
	mustWriteTheme(t, filepath.Dir(src), filepath.Base(src), validCustomThemeJSON)
	if _, err := ImportThemeFromPath(themesDir, src); err != nil {
		t.Fatalf("import: %v", err)
	}
	th, err := CachedThemeByID(themesDir, "terra-test")
	if err != nil {
		t.Fatalf("CachedThemeByID: %v", err)
	}
	if th == nil || th.ID != "terra-test" {
		t.Errorf("expected terra-test, got %+v", th)
	}
}

func TestCachedThemeByID_CachesParsedTheme(t *testing.T) {
	themesDir := t.TempDir()
	src := filepath.Join(t.TempDir(), "src.json")
	mustWriteTheme(t, filepath.Dir(src), filepath.Base(src), validCustomThemeJSON)
	if _, err := ImportThemeFromPath(themesDir, src); err != nil {
		t.Fatalf("import: %v", err)
	}
	th1, err := CachedThemeByID(themesDir, "terra-test")
	if err != nil {
		t.Fatalf("CachedThemeByID: %v", err)
	}
	th2, err := CachedThemeByID(themesDir, "terra-test")
	if err != nil {
		t.Fatalf("CachedThemeByID: %v", err)
	}
	// Same pointer on cache hit; LoadTheme would re-parse.
	if th1 != th2 {
		t.Errorf("expected cache hit (same pointer), got different objects")
	}
}

func TestCachedThemeByID_InvalidFileFallsBackToDefault(t *testing.T) {
	themesDir := t.TempDir()
	// Write a broken theme file directly (bypass the importer so we can
	// simulate a hand-edited broken file).
	if err := writeBytes(t, filepath.Join(themesDir, "broken.json"), []byte(`{not valid`)); err != nil {
		t.Fatalf("write: %v", err)
	}
	th, err := CachedThemeByID(themesDir, "broken")
	if err != nil {
		t.Fatalf("CachedThemeByID: %v", err)
	}
	if th == nil || th.ID != DefaultThemeID {
		t.Errorf("expected embedded default fallback, got %+v", th)
	}
}

func TestInvalidateThemeCache_DropsOne(t *testing.T) {
	themesDir := t.TempDir()
	src := filepath.Join(t.TempDir(), "src.json")
	mustWriteTheme(t, filepath.Dir(src), filepath.Base(src), validCustomThemeJSON)
	if _, err := ImportThemeFromPath(themesDir, src); err != nil {
		t.Fatalf("import: %v", err)
	}
	th1, err := CachedThemeByID(themesDir, "terra-test")
	if err != nil {
		t.Fatalf("CachedThemeByID: %v", err)
	}
	InvalidateThemeCache("terra-test")
	th2, err := CachedThemeByID(themesDir, "terra-test")
	if err != nil {
		t.Fatalf("CachedThemeByID: %v", err)
	}
	// After invalidation the second call must produce a fresh *Theme
	// pointer (LoadTheme returns a new struct each time).
	if th1 == th2 {
		t.Errorf("expected different pointers after invalidation, got same")
	}
}

func TestInvalidateThemeCache_DropsAll(t *testing.T) {
	themesDir := t.TempDir()
	src := filepath.Join(t.TempDir(), "src.json")
	mustWriteTheme(t, filepath.Dir(src), filepath.Base(src), validCustomThemeJSON)
	if _, err := ImportThemeFromPath(themesDir, src); err != nil {
		t.Fatalf("import: %v", err)
	}
	th1, _ := CachedThemeByID(themesDir, "terra-test")
	InvalidateThemeCache()
	th2, _ := CachedThemeByID(themesDir, "terra-test")
	if th1 == th2 {
		t.Errorf("expected different pointers after invalidate-all, got same")
	}
}

func TestInvalidateThemeCache_UnknownIDIsNoOp(t *testing.T) {
	// Just shouldn't panic; cache remains in a usable state.
	InvalidateThemeCache("never-cached", "")
	themesDir := t.TempDir()
	if _, err := CachedThemeByID(themesDir, DefaultThemeID); err != nil {
		t.Errorf("cache still functional: %v", err)
	}
}

func TestCachedThemeByID_PicksUpModTime(t *testing.T) {
	// Mutate the on-disk file between cache calls and assert the
	// mtime check reloads. We bump the mtime explicitly because file
	// systems vary in resolution; the cache key is the mtime itself.
	themesDir := t.TempDir()
	src := filepath.Join(t.TempDir(), "src.json")
	mustWriteTheme(t, filepath.Dir(src), filepath.Base(src), validCustomThemeJSON)
	if _, err := ImportThemeFromPath(themesDir, src); err != nil {
		t.Fatalf("import: %v", err)
	}
	th1, err := CachedThemeByID(themesDir, "terra-test")
	if err != nil {
		t.Fatalf("CachedThemeByID: %v", err)
	}
	// Bump the mtime by 2s to be safe across FS resolution.
	path := filepath.Join(themesDir, "terra-test.json")
	newTime := time.Now().Add(2 * time.Second)
	if err := touchFile(path, newTime); err != nil {
		t.Fatalf("touch: %v", err)
	}
	th2, err := CachedThemeByID(themesDir, "terra-test")
	if err != nil {
		t.Fatalf("CachedThemeByID after mtime bump: %v", err)
	}
	if th1 == th2 {
		t.Errorf("expected reload after mtime bump, got cached pointer")
	}
}

// TestCachedThemeByID_FirstClassIgnoresVaultShadow pins the embed-authoritative
// contract for the launch-path cache: a same-id file in the vault (a legacy
// ScaffoldVault seed or a manual copy) must NEVER shadow the packaged embedded
// version of a first-class theme. We write a STALE cyber_forest.json with a
// clearly different surfaces.app.bg and assert CachedThemeByID returns the
// embedded copy (embedded bg, NOT the stale on-disk sentinel). Regression
// guard for the bug where a stale seeded file silently overrode the evolved
// embedded version on launch.
func TestCachedThemeByID_FirstClassIgnoresVaultShadow(t *testing.T) {
	ResetCacheForTests()
	dir := t.TempDir()
	// A cyber_forest.json whose dark surfaces.app.bg is a sentinel (#abcdef)
	// clearly different from the embedded default's bg, so a stale-vault
	// leak is unambiguous.
	stale := strings.Replace(minimalValidJSON, `"id": "test-theme"`, `"id": "cyber_forest"`, 1)
	stale = strings.Replace(stale, `"#0c0c0e"`, `"#abcdef"`, 1) // dark surfaces.app.bg → sentinel
	mustWriteTheme(t, dir, "cyber_forest.json", stale)

	th, err := CachedThemeByID(dir, "cyber_forest")
	if err != nil {
		t.Fatalf("CachedThemeByID(cyber_forest) with vault shadow: %v", err)
	}
	if th.ID != "cyber_forest" {
		t.Fatalf("resolved id = %q, want cyber_forest", th.ID)
	}
	// Embedded copy is authoritative: dark surfaces.app.bg must be the
	// embedded default's value, NOT the stale on-disk sentinel #abcdef.
	// The expected value is derived from the packaged embedded default so
	// this assertion survives any future default-theme edit.
	def, err := ParseDefault()
	if err != nil {
		t.Fatalf("ParseDefault: %v", err)
	}
	wantBG := def.Modes.Dark.Surfaces.App.BG
	if got := th.Modes.Dark.Surfaces.App.BG; got != wantBG {
		t.Errorf("cyber_forest dark surfaces.app.bg = %q, want embedded default %q (vault shadow %q must be ignored)",
			got, wantBG, "#abcdef")
	}
}
