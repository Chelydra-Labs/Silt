package vault

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSettings_CorruptJSON(t *testing.T) {
	// settings.json exists but contains unparseable content. LoadSettings
	// must return an error, not silently return AppSettings{}.

	// Override UserConfigDir by setting APPDATA (Windows) or
	// XDG_CONFIG_HOME (Linux/macOS).
	dir := t.TempDir()
	t.Setenv("APPDATA", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)

	path, err := GetSettingsPath()
	if err != nil {
		t.Skipf("cannot determine config path on this platform: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("{not valid json!!!!}"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, err = LoadSettings()
	if err == nil {
		t.Fatalf("expected error for corrupt settings.json, got nil")
	}
}

func TestSaveSettings_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("APPDATA", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)

	original := &AppSettings{
		VaultPath: filepath.Join(dir, "my-vault"),
	}
	if err := SaveSettings(original); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}

	loaded, err := LoadSettings()
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if loaded.VaultPath != original.VaultPath {
		t.Errorf("round-trip mismatch: got %q, want %q", loaded.VaultPath, original.VaultPath)
	}
}
