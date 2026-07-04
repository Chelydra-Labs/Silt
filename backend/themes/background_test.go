package themes

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- StoreBackgroundAsset --------------------------------------------------

// writeAsset writes n bytes of payload to a file with the given name and
// returns its path. The content is irrelevant to the pipeline (only the
// extension and size matter), so a deterministic filler keeps the test
// readable.
func writeAsset(t *testing.T, name string, n int) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	payload := make([]byte, n)
	for i := range payload {
		payload[i] = byte('a' + (i % 26))
	}
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

func TestStoreBackgroundAsset_SmallFileInlinedAsBase64(t *testing.T) {
	themesDir := t.TempDir()
	// 1 KB png → under the 50 KB threshold → data URI.
	src := writeAsset(t, "photo.png", 1024)

	ref, isBase64, err := StoreBackgroundAsset(themesDir, "terra-test", src)
	if err != nil {
		t.Fatalf("StoreBackgroundAsset: %v", err)
	}
	if !isBase64 {
		t.Errorf("expected isBase64=true for a small file, got ref=%q", ref)
	}
	if want := "url(\"data:image/png;base64,"; !strings.HasPrefix(ref, want) {
		t.Errorf("expected base64 data-URI reference starting with %q, got %q", want, ref)
	}
	if !strings.HasSuffix(ref, "\")") {
		t.Errorf("expected reference to close the url() wrapper, got %q", ref)
	}
	// No assets directory should have been created for an inlined asset.
	if _, err := os.Stat(filepath.Join(themesDir, "terra-test.assets")); !os.IsNotExist(err) {
		t.Errorf("expected no assets dir for an inlined asset, got err=%v", err)
	}
	// The reference must survive validation (the url()-wrapped ';' is safe).
	if err := validateBackground("background", &Background{Image: ref}); err != nil {
		t.Errorf("inlined reference failed validation: %v", err)
	}
}

func TestStoreBackgroundAsset_LargeFileCopiedToAssetsDir(t *testing.T) {
	themesDir := t.TempDir()
	// 60 KB jpg → over the 50 KB threshold → copied to assets dir.
	src := writeAsset(t, "Photo (1).JPG", 60*1024)

	ref, isBase64, err := StoreBackgroundAsset(themesDir, "terra-test", src)
	if err != nil {
		t.Fatalf("StoreBackgroundAsset: %v", err)
	}
	if isBase64 {
		t.Errorf("expected isBase64=false for a large file, got ref=%q", ref)
	}
	wantRef := "url(\"terra-test.assets/photo-1.jpg\")"
	if ref != wantRef {
		t.Errorf("reference = %q, want %q (sanitized filename + self-describing path)", ref, wantRef)
	}
	// The file was copied into the per-theme assets directory.
	copied := filepath.Join(themesDir, "terra-test.assets", "photo-1.jpg")
	if _, err := os.Stat(copied); err != nil {
		t.Errorf("expected asset copied to %s: %v", copied, err)
	}
	// The reference must survive validation.
	if err := validateBackground("background", &Background{Image: ref}); err != nil {
		t.Errorf("assets reference failed validation: %v", err)
	}
}

func TestStoreBackgroundAsset_OversizeRejected(t *testing.T) {
	themesDir := t.TempDir()
	src := writeAsset(t, "huge.png", int(maxBackgroundAssetBytes)+1)

	_, _, err := StoreBackgroundAsset(themesDir, "terra-test", src)
	if err == nil {
		t.Fatal("expected error for a file over the 4 MB cap")
	}
	if !strings.Contains(err.Error(), "exceeds the") {
		t.Errorf("expected the error to mention the cap, got: %v", err)
	}
}

func TestStoreBackgroundAsset_BadExtensionRejected(t *testing.T) {
	themesDir := t.TempDir()
	src := writeAsset(t, "notes.txt", 100)

	_, _, err := StoreBackgroundAsset(themesDir, "terra-test", src)
	if err == nil {
		t.Fatal("expected error for an unsupported extension")
	}
	if !strings.Contains(err.Error(), "unsupported image extension") {
		t.Errorf("expected 'unsupported image extension' in error, got: %v", err)
	}
}

func TestStoreBackgroundAsset_NoThemesDir(t *testing.T) {
	src := writeAsset(t, "photo.png", 100)
	if _, _, err := StoreBackgroundAsset("", "terra-test", src); err == nil {
		t.Fatal("expected error for empty themes dir")
	}
}

// --- SetThemeBackgroundImage -----------------------------------------------

// importCustomTheme imports validCustomThemeJSON into themesDir and returns
// the on-disk id ("terra-test"). It only authors the app surface, so the
// editor zone is un-authored and exercises the seeding path.
func importCustomTheme(t *testing.T, themesDir string) string {
	t.Helper()
	src := filepath.Join(t.TempDir(), "src.json")
	mustWriteTheme(t, filepath.Dir(src), filepath.Base(src), validCustomThemeJSON)
	res, err := ImportThemeFromPath(themesDir, src)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	return res.Info.ID
}

func TestSetThemeBackgroundImage_AppZoneRoundTrip(t *testing.T) {
	themesDir := t.TempDir()
	id := importCustomTheme(t, themesDir)
	bg := Background{Image: "url(\"data:image/png;base64,iVBORw0KGgo=\")", Size: "cover", Opacity: 0.3, Blend: "overlay"}

	if err := SetThemeBackgroundImage(themesDir, id, "app", bg); err != nil {
		t.Fatalf("SetThemeBackgroundImage: %v", err)
	}
	// Re-load and confirm both modes carry the background on the app surface.
	reloaded, found, err := LoadByID(themesDir, id)
	if err != nil || !found {
		t.Fatalf("LoadByID after set: err=%v found=%v", err, found)
	}
	for _, mode := range []string{"dark", "light"} {
		var m Mode
		if mode == "dark" {
			m = reloaded.Modes.Dark
		} else {
			m = reloaded.Modes.Light
		}
		if m.Surfaces.App.Background == nil {
			t.Fatalf("%s app background is nil after set", mode)
		}
		if got := m.Surfaces.App.Background.Image; got != bg.Image {
			t.Errorf("%s app background.image = %q, want %q", mode, got, bg.Image)
		}
	}
}

// TestSetThemeBackgroundImage_UnauthoredZoneSeedsFromParent: the editor zone
// is not authored in validCustomThemeJSON, so attaching a background must seed
// bg/border/text from app and the result must re-validate on reload.
func TestSetThemeBackgroundImage_UnauthoredZoneSeedsFromParent(t *testing.T) {
	themesDir := t.TempDir()
	id := importCustomTheme(t, themesDir)
	bg := Background{Image: "url(\"terra-test.assets/foo.png\")"}

	if err := SetThemeBackgroundImage(themesDir, id, "editor", bg); err != nil {
		t.Fatalf("SetThemeBackgroundImage editor: %v", err)
	}
	reloaded, found, err := LoadByID(themesDir, id)
	if err != nil || !found {
		t.Fatalf("LoadByID: err=%v found=%v", err, found)
	}
	// Editor surface now exists on both modes with inherited app colors + the bg.
	for _, mode := range []string{"dark", "light"} {
		var m Mode
		if mode == "dark" {
			m = reloaded.Modes.Dark
		} else {
			m = reloaded.Modes.Light
		}
		if m.Surfaces.Editor == nil {
			t.Fatalf("%s editor surface nil after set", mode)
		}
		if m.Surfaces.Editor.Background == nil || m.Surfaces.Editor.Background.Image != bg.Image {
			t.Errorf("%s editor background not set: %+v", mode, m.Surfaces.Editor.Background)
		}
		// Seeded from app so the surface is valid (bg/border/text present).
		if m.Surfaces.Editor.BG == "" || m.Surfaces.Editor.Border == "" || m.Surfaces.Editor.Text == "" {
			t.Errorf("%s editor surface not seeded from app: %+v", mode, m.Surfaces.Editor)
		}
	}
}

func TestSetThemeBackgroundImage_RejectsEmbeddedID(t *testing.T) {
	themesDir := t.TempDir()
	// DefaultThemeID is embedded-only (not on disk in this empty themesDir).
	bg := Background{Image: "url(\"data:image/png;base64,x\")"}
	err := SetThemeBackgroundImage(themesDir, DefaultThemeID, "app", bg)
	if err == nil {
		t.Fatal("expected error when writing to an embedded-only theme id")
	}
	if !strings.Contains(err.Error(), "not an on-disk") {
		t.Errorf("expected 'not an on-disk' hint, got: %v", err)
	}
}

func TestSetThemeBackgroundImage_RejectsInvalidZone(t *testing.T) {
	themesDir := t.TempDir()
	id := importCustomTheme(t, themesDir)
	err := SetThemeBackgroundImage(themesDir, id, "toolbar", Background{Image: "url(x)"})
	if err == nil {
		t.Fatal("expected error for an invalid zone")
	}
	if !strings.Contains(err.Error(), "invalid surface zone") {
		t.Errorf("expected 'invalid surface zone' in error, got: %v", err)
	}
}

func TestSetThemeBackgroundImage_RejectsBadSize(t *testing.T) {
	themesDir := t.TempDir()
	id := importCustomTheme(t, themesDir)
	err := SetThemeBackgroundImage(themesDir, id, "app", Background{Image: "url(x)", Size: "stretch"})
	if err == nil {
		t.Fatal("expected validation error for a bad size enum")
	}
}

func TestSetThemeBackgroundImage_NoThemesDir(t *testing.T) {
	err := SetThemeBackgroundImage("", "x", "app", Background{Image: "url(x)"})
	if err == nil {
		t.Fatal("expected error for empty themes dir")
	}
}

// --- ForkEmbeddedTheme -----------------------------------------------------

func TestForkEmbeddedTheme_CreatesUserPrefixedCopy(t *testing.T) {
	themesDir := t.TempDir()
	forkedID, err := ForkEmbeddedTheme(themesDir, "silt-linen")
	if err != nil {
		t.Fatalf("ForkEmbeddedTheme: %v", err)
	}
	if want := "user-silt-linen"; forkedID != want {
		t.Errorf("forked id = %q, want %q", forkedID, want)
	}
	// The forked file exists on disk and loads with the forked id.
	t2, found, err := LoadByID(themesDir, forkedID)
	if err != nil || !found {
		t.Fatalf("LoadByID(%q): err=%v found=%v", forkedID, err, found)
	}
	if t2.ID != forkedID {
		t.Errorf("forked theme id = %q, want %q", t2.ID, forkedID)
	}
}

func TestForkEmbeddedTheme_ReusesExistingFork(t *testing.T) {
	themesDir := t.TempDir()
	id, err := ForkEmbeddedTheme(themesDir, "silt-linen")
	if err != nil {
		t.Fatalf("first fork: %v", err)
	}
	// Corrupt the fork's author to prove a second fork does not overwrite it.
	path := filepath.Join(themesDir, id+".json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fork: %v", err)
	}
	patched := strings.Replace(string(raw), `"Silt"`, `"EDITED-USER-FORK"`, 1)
	if patched == string(raw) {
		t.Skip("could not patch author field; skipping overwrite assertion")
	}
	if err := os.WriteFile(path, []byte(patched), 0o600); err != nil {
		t.Fatalf("write patched fork: %v", err)
	}
	if _, err := ForkEmbeddedTheme(themesDir, "silt-linen"); err != nil {
		t.Fatalf("second fork: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("re-read fork: %v", err)
	}
	if string(got) != patched {
		t.Errorf("existing fork was overwritten by a second ForkEmbeddedTheme call")
	}
}

func TestForkEmbeddedTheme_RejectsNonEmbeddedID(t *testing.T) {
	if _, err := ForkEmbeddedTheme(t.TempDir(), "no-such-theme"); err == nil {
		t.Fatal("expected error for a non-embedded id")
	}
}

// --- IsValidSurfaceZone ----------------------------------------------------

func TestIsValidSurfaceZone(t *testing.T) {
	valid := []string{"app", "sidebar", "editor", "panel", "card", "modal", "popover"}
	for _, z := range valid {
		if !IsValidSurfaceZone(z) {
			t.Errorf("IsValidSurfaceZone(%q) = false, want true", z)
		}
	}
	for _, z := range []string{"", "toolbar", "APP", "toast"} {
		if IsValidSurfaceZone(z) {
			t.Errorf("IsValidSurfaceZone(%q) = true, want false", z)
		}
	}
}

// --- Export round-trip with assets dir -------------------------------------

func TestExportThemeToPath_CopiesAssetsDir(t *testing.T) {
	themesDir := t.TempDir()
	id := importCustomTheme(t, themesDir)

	// Attach an editor background that copies into <id>.assets/ (large file).
	src := writeAsset(t, "big.jpg", 60*1024)
	ref, isBase64, err := StoreBackgroundAsset(themesDir, id, src)
	if err != nil {
		t.Fatalf("StoreBackgroundAsset: %v", err)
	}
	if isBase64 {
		t.Fatal("expected the asset to be copied, not inlined")
	}
	if err := SetThemeBackgroundImage(themesDir, id, "editor", Background{Image: ref}); err != nil {
		t.Fatalf("SetThemeBackgroundImage: %v", err)
	}
	// Sanity: the assets dir exists in the source themes dir.
	srcAssets := filepath.Join(themesDir, id+".assets")
	if _, err := os.Stat(srcAssets); err != nil {
		t.Fatalf("source assets dir missing: %v", err)
	}

	// Export to an unrelated destination directory.
	dstDir := t.TempDir()
	dstPath := filepath.Join(dstDir, "exported.json")
	if err := ExportThemeToPath(themesDir, id, dstPath); err != nil {
		t.Fatalf("ExportThemeToPath: %v", err)
	}
	// The assets directory was copied alongside the exported JSON.
	dstAssets := filepath.Join(dstDir, id+".assets")
	entries, err := os.ReadDir(dstAssets)
	if err != nil {
		t.Fatalf("expected exported assets dir %s: %v", dstAssets, err)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 asset in exported dir, got %d", len(entries))
	}
	// The exported JSON re-validates and still references the assets dir.
	if _, err := ParseAndValidate(mustReadFile(t, dstPath)); err != nil {
		t.Errorf("exported theme failed validation: %v", err)
	}
}

func TestExportThemeToPath_NoAssetsDirIsNoop(t *testing.T) {
	// A theme with only an inlined (base64) background has no assets dir;
	// export must still succeed and write only the JSON.
	themesDir := t.TempDir()
	id := importCustomTheme(t, themesDir)
	src := writeAsset(t, "tiny.png", 100) // inlined
	ref, _, err := StoreBackgroundAsset(themesDir, id, src)
	if err != nil {
		t.Fatalf("StoreBackgroundAsset: %v", err)
	}
	if err := SetThemeBackgroundImage(themesDir, id, "app", Background{Image: ref}); err != nil {
		t.Fatalf("SetThemeBackgroundImage: %v", err)
	}
	dstDir := t.TempDir()
	dstPath := filepath.Join(dstDir, "exported.json")
	if err := ExportThemeToPath(themesDir, id, dstPath); err != nil {
		t.Fatalf("ExportThemeToPath: %v", err)
	}
	entries, _ := os.ReadDir(dstDir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".assets") {
			t.Errorf("did not expect an assets dir in export destination, found %s", e.Name())
		}
	}
}

// --- isSafeBackgroundImage regression guards -------------------------------

func TestIsSafeBackgroundImage_UrlWrappedBase64Accepted(t *testing.T) {
	good := []string{
		"url(x)",
		"url(\"data:image/svg+xml,%3Csvg%3E%3C/svg%3E\")",
		"url(\"data:image/png;base64,iVBORw0KGgo=\")",
		`repeating-linear-gradient(0deg, rgba(1,2,3,0.8) 0 1px, transparent 1px 5px), url("data:image/png;base64,YQ==")`,
		"",
	}
	for _, s := range good {
		if err := validateBackground("background", &Background{Image: s}); err != nil {
			t.Errorf("expected %q to be safe, got error: %v", s, err)
		}
	}
}

func TestIsSafeBackgroundImage_TopLevelBreakersRejected(t *testing.T) {
	bad := []string{
		"url(data:); body{background:red}", // ; { } outside url()
		"url(data:)}body{",                 // } { outside url()
		"url(data:)<script>alert(1)</script>",
		"data:image/png;base64,xxx", // bare data URI (no url() wrapper) — top-level ';'
		"url(data:)\\escape",
	}
	for _, s := range bad {
		if err := validateBackground("background", &Background{Image: s}); err == nil {
			t.Errorf("expected %q to be rejected, got nil", s)
		}
	}
}
