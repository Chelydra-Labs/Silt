package themes

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGetThemeJSON_Embedded(t *testing.T) {
	raw, err := GetThemeJSON("", DefaultThemeID)
	if err != nil {
		t.Fatalf("GetThemeJSON(embedded): %v", err)
	}
	var tParsed Theme
	if err := json.Unmarshal(raw, &tParsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if tParsed.ID != DefaultThemeID {
		t.Errorf("id = %q, want %q", tParsed.ID, DefaultThemeID)
	}
	if tParsed.Name == "" {
		t.Error("expected non-empty name")
	}
}

func TestGetThemeJSON_DiskCustom(t *testing.T) {
	themesDir := t.TempDir()
	custom := validV2Theme()
	custom.ID = "terra-test"
	custom.Name = "Terra Test"
	canon, err := json.MarshalIndent(custom, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(themesDir, "terra-test.json"), canon, 0o644); err != nil {
		t.Fatal(err)
	}

	raw, err := GetThemeJSON(themesDir, "terra-test")
	if err != nil {
		t.Fatalf("GetThemeJSON(disk): %v", err)
	}
	var got Theme
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.ID != "terra-test" || got.Name != "Terra Test" {
		t.Errorf("got id=%q name=%q", got.ID, got.Name)
	}
}

func TestGetThemeJSON_FirstClassPrefersEmbedOverOrphanDisk(t *testing.T) {
	themesDir := t.TempDir()
	// Orphan same-id file with a different name — must not shadow the embed.
	orphan := validV2Theme()
	orphan.ID = DefaultThemeID
	orphan.Name = "ORPHAN"
	canon, _ := json.MarshalIndent(orphan, "", "  ")
	if err := os.WriteFile(filepath.Join(themesDir, DefaultThemeID+".json"), canon, 0o644); err != nil {
		t.Fatal(err)
	}

	raw, err := GetThemeJSON(themesDir, DefaultThemeID)
	if err != nil {
		t.Fatalf("GetThemeJSON: %v", err)
	}
	var got Theme
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Name == "ORPHAN" {
		t.Error("first-class GetThemeJSON must prefer embed over orphan disk file")
	}
}

func TestGetThemeJSON_Unknown(t *testing.T) {
	if _, err := GetThemeJSON(t.TempDir(), "no-such-theme"); err == nil {
		t.Fatal("expected error for unknown id")
	}
}

func TestSaveCustomTheme_NewFromName(t *testing.T) {
	themesDir := t.TempDir()
	th := validV2Theme()
	th.ID = "ignored-id"
	th.Name = "My Custom Theme"

	info, err := SaveCustomTheme(themesDir, th, false)
	if err != nil {
		t.Fatalf("SaveCustomTheme: %v", err)
	}
	if info.ID != "my-custom-theme" {
		t.Errorf("id = %q, want my-custom-theme", info.ID)
	}
	if info.Source != "disk" {
		t.Errorf("source = %q, want disk", info.Source)
	}
	if _, err := os.Stat(filepath.Join(themesDir, info.ID+".json")); err != nil {
		t.Errorf("expected file on disk: %v", err)
	}
	// ID on the theme object was rewritten.
	if th.ID != info.ID {
		t.Errorf("theme.ID = %q, want %q", th.ID, info.ID)
	}
}

func TestSaveCustomTheme_NewNamespacesEmbedName(t *testing.T) {
	themesDir := t.TempDir()
	// Name that slugs to a first-class id must get the user- prefix.
	th := validV2Theme()
	th.Name = "Cyber Forest" // → cyber-forest, not cyber_forest — use exact embed id slug

	// Force the slug to hit the default id by setting name that sanitizes to it.
	// sanitizeThemeID("cyber_forest") keeps underscores.
	th.Name = "cyber_forest"
	info, err := SaveCustomTheme(themesDir, th, false)
	if err != nil {
		t.Fatalf("SaveCustomTheme: %v", err)
	}
	if !strings.HasPrefix(info.ID, userPrefix) {
		t.Errorf("expected user- prefix for embed-colliding name, got %q", info.ID)
	}
	if _, ok := ParseEmbeddedByID(info.ID); ok {
		t.Errorf("saved id %q must not be first-class", info.ID)
	}
}

func TestSaveCustomTheme_NewCollisionSuffix(t *testing.T) {
	themesDir := t.TempDir()
	th1 := validV2Theme()
	th1.Name = "Dup Theme"
	info1, err := SaveCustomTheme(themesDir, th1, false)
	if err != nil {
		t.Fatalf("first save: %v", err)
	}
	th2 := validV2Theme()
	th2.Name = "Dup Theme"
	info2, err := SaveCustomTheme(themesDir, th2, false)
	if err != nil {
		t.Fatalf("second save: %v", err)
	}
	if info1.ID == info2.ID {
		t.Errorf("expected distinct ids, both %q", info1.ID)
	}
	if !strings.HasSuffix(info2.ID, "-2") {
		t.Errorf("expected -2 suffix on collision, got %q", info2.ID)
	}
}

func TestSaveCustomTheme_OverwriteDiskCustom(t *testing.T) {
	themesDir := t.TempDir()
	th := validV2Theme()
	th.Name = "Editable"
	info, err := SaveCustomTheme(themesDir, th, false)
	if err != nil {
		t.Fatalf("initial save: %v", err)
	}

	// Reload, mutate description, overwrite.
	raw, err := os.ReadFile(filepath.Join(themesDir, info.ID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseAndValidate(raw)
	if err != nil {
		t.Fatal(err)
	}
	parsed.Description = "updated description"
	parsed.Name = "Editable Renamed"

	info2, err := SaveCustomTheme(themesDir, parsed, true)
	if err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	if info2.ID != info.ID {
		t.Errorf("overwrite changed id %q → %q", info.ID, info2.ID)
	}
	if info2.Name != "Editable Renamed" {
		t.Errorf("name = %q", info2.Name)
	}

	raw2, err := os.ReadFile(filepath.Join(themesDir, info.ID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw2), "updated description") {
		t.Error("overwrite did not persist description")
	}
}

func TestSaveCustomTheme_RefuseOverwriteEmbed(t *testing.T) {
	themesDir := t.TempDir()
	th, ok := ParseEmbeddedByID(DefaultThemeID)
	if !ok {
		t.Fatal("default embed missing")
	}
	if _, err := SaveCustomTheme(themesDir, th, true); err == nil {
		t.Fatal("expected refuse overwrite of pure embed")
	}
}

func TestSaveCustomTheme_RefuseOverwriteMissing(t *testing.T) {
	themesDir := t.TempDir()
	th := validV2Theme()
	th.ID = "not-on-disk"
	if _, err := SaveCustomTheme(themesDir, th, true); err == nil {
		t.Fatal("expected refuse overwrite of missing custom")
	}
}

func TestRenameCustomTheme(t *testing.T) {
	themesDir := t.TempDir()
	th := validV2Theme()
	th.Name = "Before"
	info, err := SaveCustomTheme(themesDir, th, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := RenameCustomTheme(themesDir, info.ID, "After"); err != nil {
		t.Fatalf("RenameCustomTheme: %v", err)
	}
	raw, err := GetThemeJSON(themesDir, info.ID)
	if err != nil {
		t.Fatal(err)
	}
	var got Theme
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Name != "After" {
		t.Errorf("name = %q, want After", got.Name)
	}
	if got.ID != info.ID {
		t.Errorf("id changed on rename: %q → %q", info.ID, got.ID)
	}
}

func TestRenameCustomTheme_RefuseEmbed(t *testing.T) {
	if err := RenameCustomTheme(t.TempDir(), DefaultThemeID, "Nope"); err == nil {
		t.Fatal("expected refuse rename of embed")
	}
}

func TestDeleteCustomTheme(t *testing.T) {
	themesDir := t.TempDir()
	th := validV2Theme()
	th.Name = "Doomed"
	info, err := SaveCustomTheme(themesDir, th, false)
	if err != nil {
		t.Fatal(err)
	}
	// Create a fake assets dir to confirm it is removed.
	assets := filepath.Join(themesDir, info.ID+".assets")
	if err := os.MkdirAll(assets, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assets, "x.png"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := DeleteCustomTheme(themesDir, info.ID); err != nil {
		t.Fatalf("DeleteCustomTheme: %v", err)
	}
	if _, err := os.Stat(filepath.Join(themesDir, info.ID+".json")); !os.IsNotExist(err) {
		t.Errorf("json still present: %v", err)
	}
	if _, err := os.Stat(assets); !os.IsNotExist(err) {
		t.Errorf("assets still present: %v", err)
	}
}

func TestDeleteCustomTheme_RefuseEmbedAndMissing(t *testing.T) {
	themesDir := t.TempDir()
	if err := DeleteCustomTheme(themesDir, DefaultThemeID); err == nil {
		t.Fatal("expected refuse delete of embed")
	}
	if err := DeleteCustomTheme(themesDir, "nope"); err == nil {
		t.Fatal("expected refuse delete of missing")
	}
}

func TestPrepareBackgroundAsset_SmallBase64(t *testing.T) {
	themesDir := t.TempDir()
	src := writeAsset(t, "tiny.png", 1024)
	ref, isBase64, err := PrepareBackgroundAsset(themesDir, src)
	if err != nil {
		t.Fatalf("PrepareBackgroundAsset: %v", err)
	}
	if !isBase64 {
		t.Errorf("expected base64, got ref=%q", ref)
	}
	if !strings.HasPrefix(ref, "url(\"data:image/png;base64,") {
		t.Errorf("unexpected ref: %q", ref)
	}
	// No staging dir for small files.
	if _, err := os.Stat(filepath.Join(themesDir, editorStagingDir)); !os.IsNotExist(err) {
		t.Errorf("expected no staging dir for small file, err=%v", err)
	}
}

func TestPrepareBackgroundAsset_LargeStaging(t *testing.T) {
	themesDir := t.TempDir()
	src := writeAsset(t, "big.jpg", 60*1024)
	ref, isBase64, err := PrepareBackgroundAsset(themesDir, src)
	if err != nil {
		t.Fatalf("PrepareBackgroundAsset: %v", err)
	}
	if isBase64 {
		t.Errorf("expected staged file, got base64 ref=%q", ref)
	}
	if !strings.HasPrefix(ref, "url(\".editor-staging/") || !strings.HasSuffix(ref, ".jpg\")") {
		t.Errorf("unexpected staging ref: %q", ref)
	}
	// Extract filename and confirm it exists.
	inner := strings.TrimPrefix(ref, "url(\"")
	inner = strings.TrimSuffix(inner, "\")")
	full := filepath.Join(themesDir, filepath.FromSlash(inner))
	if _, err := os.Stat(full); err != nil {
		t.Errorf("staged file missing at %s: %v", full, err)
	}
}

func TestSaveCustomTheme_MaterializesStagingBackground(t *testing.T) {
	themesDir := t.TempDir()
	src := writeAsset(t, "big.png", 60*1024)
	ref, isBase64, err := PrepareBackgroundAsset(themesDir, src)
	if err != nil {
		t.Fatalf("PrepareBackgroundAsset: %v", err)
	}
	if isBase64 {
		t.Fatalf("expected staging ref for large image, got base64")
	}

	th := validV2Theme()
	th.Name = "Staged BG Theme"
	th.Modes.Dark.Surfaces.App.Background = &Background{Image: ref, Size: "cover"}

	info, err := SaveCustomTheme(themesDir, th, false)
	if err != nil {
		t.Fatalf("SaveCustomTheme: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(themesDir, info.ID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	if strings.Contains(body, editorStagingDir) {
		t.Errorf("theme JSON still references %s", editorStagingDir)
	}
	// json.Marshal escapes " as \u0022 inside strings by default.
	wantAssetPath := info.ID + `.assets/`
	if !strings.Contains(body, wantAssetPath) {
		t.Errorf("expected assets path %q in theme JSON; body snippet:\n%s", wantAssetPath, body)
	}

	inner, ok := cssURLInner(ref)
	if !ok {
		t.Fatalf("could not parse staging ref %q", ref)
	}
	filename := strings.TrimPrefix(filepath.ToSlash(inner), editorStagingDir+"/")
	assetPath := filepath.Join(themesDir, info.ID+".assets", filename)
	if _, err := os.Stat(assetPath); err != nil {
		t.Errorf("materialized asset missing at %s: %v", assetPath, err)
	}
	// In-memory theme should also be rewritten for the caller.
	wantRefPrefix := `url("` + info.ID + `.assets/`
	if th.Modes.Dark.Surfaces.App.Background == nil ||
		!strings.HasPrefix(th.Modes.Dark.Surfaces.App.Background.Image, wantRefPrefix) {
		t.Errorf("in-memory theme image not rewritten: %+v", th.Modes.Dark.Surfaces.App.Background)
	}
}

func TestSaveCustomTheme_DataURIBackgroundUnchanged(t *testing.T) {
	themesDir := t.TempDir()
	src := writeAsset(t, "tiny.png", 1024)
	ref, isBase64, err := PrepareBackgroundAsset(themesDir, src)
	if err != nil {
		t.Fatalf("PrepareBackgroundAsset: %v", err)
	}
	if !isBase64 {
		t.Fatalf("expected data-URI ref for small image, got %q", ref)
	}

	th := validV2Theme()
	th.Name = "Inline BG Theme"
	th.Modes.Dark.Surfaces.App.Background = &Background{Image: ref}

	info, err := SaveCustomTheme(themesDir, th, false)
	if err != nil {
		t.Fatalf("SaveCustomTheme: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(themesDir, info.ID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	// json.Marshal escapes " as \u0022; match the data URI payload itself.
	if !strings.Contains(body, "data:image/png;base64,") {
		t.Errorf("data-URI background not preserved in theme JSON; body snippet:\n%s", body)
	}
	if strings.Contains(body, editorStagingDir) {
		t.Error("unexpected staging ref for data-URI background")
	}
	if _, err := os.Stat(filepath.Join(themesDir, info.ID+".assets")); !os.IsNotExist(err) {
		t.Errorf("expected no assets dir for data-URI-only theme, err=%v", err)
	}
}

func TestSaveCustomTheme_MissingStagingBackgroundErrors(t *testing.T) {
	themesDir := t.TempDir()
	th := validV2Theme()
	th.Name = "Missing Staging"
	th.Modes.Dark.Surfaces.App.Background = &Background{
		Image: `url(".editor-staging/deadbeef.png")`,
	}
	if _, err := SaveCustomTheme(themesDir, th, false); err == nil {
		t.Fatal("expected error for missing staged asset")
	}
}
