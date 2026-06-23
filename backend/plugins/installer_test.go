package plugins

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// writeZip builds a zip archive at dest from a map of path→content.
func writeZip(t *testing.T, dest string, files map[string]string) {
	t.Helper()
	buf := new(bytes.Buffer)
	w := zip.NewWriter(buf)
	for name, content := range files {
		f, err := w.Create(name)
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		if _, err := f.Write([]byte(content)); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	if err := os.WriteFile(dest, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write archive: %v", err)
	}
}

func manifestJSON(id, name, version string) string {
	b, _ := json.Marshal(map[string]string{
		"id":      id,
		"name":    name,
		"version": version,
		"main":    "index.js",
	})
	return string(b)
}

func TestValidateAndInstall_HappyPath(t *testing.T) {
	vault := t.TempDir()
	_ = os.MkdirAll(filepath.Join(vault, ".system", "plugins"), 0o755)
	archive := filepath.Join(t.TempDir(), "good.silt-plugin")
	writeZip(t, archive, map[string]string{
		"plugin.json": manifestJSON("my-plugin", "My Plugin", "1.2.0"),
		"index.js":    "export default { manifest: { id: 'my-plugin' } };\n",
	})

	m, warns, err := Validate(archive)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if m.ID != "my-plugin" || m.Name != "My Plugin" || m.Version != "1.2.0" {
		t.Errorf("unexpected manifest: %+v", m)
	}
	if len(warns) != 0 {
		t.Errorf("expected no warnings, got %v", warns)
	}

	installed, err := Install(vault, archive)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if installed.ID != "my-plugin" {
		t.Errorf("expected installed id my-plugin, got %s", installed.ID)
	}
	// Files extracted.
	if _, err := os.Stat(filepath.Join(vault, ".system", "plugins", "my-plugin", "index.js")); err != nil {
		t.Errorf("index.js not installed: %v", err)
	}
}

// TestInstall_RestrictiveFilePermissions pins F19: the extracted plugin files
// (plugin.json + index.js) are 0o600 and the plugin directory tree 0o700 so a
// co-tenant cannot read a plugin's cached data.
func TestInstall_RestrictiveFilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits are not enforced on Windows")
	}
	vault := t.TempDir()
	_ = os.MkdirAll(filepath.Join(vault, ".system", "plugins"), 0o700)
	archive := filepath.Join(t.TempDir(), "perm.silt-plugin")
	writeZip(t, archive, map[string]string{
		"plugin.json": manifestJSON("perm-plugin", "Perm", "1.0.0"),
		"index.js":    "export default {};\n",
	})
	if _, err := Install(vault, archive); err != nil {
		t.Fatalf("Install: %v", err)
	}
	base := filepath.Join(vault, ".system", "plugins", "perm-plugin")
	for _, rel := range []string{"plugin.json", "index.js"} {
		info, err := os.Stat(filepath.Join(base, rel))
		if err != nil {
			t.Fatalf("stat %s: %v", rel, err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Errorf("%s perm = %o, want 0o600", rel, got)
		}
	}
	dirInfo, err := os.Stat(base)
	if err != nil {
		t.Fatalf("stat plugin dir: %v", err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o700 {
		t.Errorf("plugin dir perm = %o, want 0o700", got)
	}
}

func TestValidate_RejectsBadArchives(t *testing.T) {
	vault := t.TempDir()
	_ = os.MkdirAll(filepath.Join(vault, ".system", "plugins"), 0o755)

	tests := []struct {
		name  string
		files map[string]string
	}{
		{"missing manifest", map[string]string{"index.js": "x"}},
		{"bad id uppercase", map[string]string{"plugin.json": manifestJSON("MyPlugin", "x", "1"), "index.js": "x"}},
		{"missing main", map[string]string{"plugin.json": manifestJSON("ok", "x", "1")}},
		{"zip-slip", map[string]string{
			"plugin.json": manifestJSON("slip", "x", "1"),
			"index.js":    "x",
			"../evil.txt": "pwned",
		}},
		{"absolute path", map[string]string{
			"plugin.json":  manifestJSON("abs", "x", "1"),
			"index.js":     "x",
			"/etc/evil":    "pwned",
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			archive := filepath.Join(t.TempDir(), "bad.silt-plugin")
			writeZip(t, archive, tc.files)
			if _, _, err := Validate(archive); err == nil {
				t.Errorf("expected Validate to reject %s", tc.name)
			}
			if _, err := Install(vault, archive); err == nil {
				t.Errorf("expected Install to reject %s", tc.name)
			}
		})
	}
}

func TestInstall_RefusesDuplicate(t *testing.T) {
	vault := t.TempDir()
	_ = os.MkdirAll(filepath.Join(vault, ".system", "plugins"), 0o755)
	archive := filepath.Join(t.TempDir(), "dup.silt-plugin")
	writeZip(t, archive, map[string]string{
		"plugin.json": manifestJSON("dup", "Dup", "1.0.0"),
		"index.js":    "x",
	})

	if _, err := Install(vault, archive); err != nil {
		t.Fatalf("first install: %v", err)
	}
	if _, err := Install(vault, archive); err == nil {
		t.Errorf("expected duplicate install to be refused")
	}
}

func TestUninstall_RemovesAndRejectsTraversal(t *testing.T) {
	vault := t.TempDir()
	_ = os.MkdirAll(filepath.Join(vault, ".system", "plugins"), 0o755)
	archive := filepath.Join(t.TempDir(), "u.silt-plugin")
	writeZip(t, archive, map[string]string{
		"plugin.json": manifestJSON("removable", "Removable", "1.0.0"),
		"index.js":    "x",
	})
	if _, err := Install(vault, archive); err != nil {
		t.Fatalf("install: %v", err)
	}

	if err := Uninstall(vault, "removable"); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if _, err := os.Stat(filepath.Join(vault, ".system", "plugins", "removable")); !os.IsNotExist(err) {
		t.Errorf("expected plugin dir removed")
	}

	if err := Uninstall(vault, "../../escape"); err == nil {
		t.Errorf("expected traversal id rejected")
	}
}

func TestEnableDisable_SentinelToggle(t *testing.T) {
	vault := t.TempDir()
	_ = os.MkdirAll(filepath.Join(vault, ".system", "plugins", "toggleable"), 0o755)

	if IsDisabled(filepath.Join(vault, ".system", "plugins", "toggleable")) {
		t.Errorf("expected not disabled initially")
	}
	if err := SetDisabled(vault, "toggleable", true); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if !IsDisabled(filepath.Join(vault, ".system", "plugins", "toggleable")) {
		t.Errorf("expected disabled after SetDisabled(true)")
	}
	if err := SetDisabled(vault, "toggleable", false); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if IsDisabled(filepath.Join(vault, ".system", "plugins", "toggleable")) {
		t.Errorf("expected enabled after SetDisabled(false)")
	}
}

func TestUninstall_RejectsDotSegmentAndTraversal(t *testing.T) {
	vault := t.TempDir()
	_ = os.MkdirAll(filepath.Join(vault, ".system", "plugins"), 0o755)

	// "..." must NOT resolve to "." (which would wipe the entire plugins dir).
	for _, evil := range []string{"...", ".", "", "..", "../escape", "/etc"} {
		if err := Uninstall(vault, evil); err == nil {
			t.Errorf("expected Uninstall(%q) to be rejected", evil)
		}
	}
}

func TestValidate_RejectsCustomMain(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "custom.silt-plugin")
	// manifest with a non-index.js main
	customMain, _ := json.Marshal(map[string]string{
		"id":   "custom",
		"name": "Custom",
		"main": "foo.js",
	})
	writeZip(t, archive, map[string]string{
		"plugin.json": string(customMain),
		"foo.js":      "x",
	})
	if _, _, err := Validate(archive); err == nil {
		t.Errorf("expected Validate to reject a manifest.main other than index.js")
	}
}

func TestValidate_AcceptsEmptyMain(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "ok.silt-plugin")
	writeZip(t, archive, map[string]string{
		"plugin.json": manifestJSON("ok", "Ok", "1.0.0"),
		"index.js":    "x",
	})
	if _, _, err := Validate(archive); err != nil {
		t.Errorf("expected Validate to accept an empty main (defaults to index.js): %v", err)
	}
}

// manifestWithCaps builds a plugin.json with a capabilities declaration.
func manifestWithCaps(id string, caps map[string]any) string {
	b, _ := json.Marshal(map[string]any{
		"id":          id,
		"name":        id,
		"version":     "1.0.0",
		"main":        "index.js",
		"capabilities": caps,
	})
	return string(b)
}

func TestValidate_AcceptsKnownCapabilities(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "caps.silt-plugin")
	writeZip(t, archive, map[string]string{
		"plugin.json": manifestWithCaps("caps", map[string]any{
			"network":     true,
			"write-files": "notebook",
		}),
		"index.js": "x",
	})
	m, _, err := Validate(archive)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	got, ok := m.Capabilities["network"]
	if !ok || got != true {
		t.Errorf("network cap = %v ok=%v, want true", got, ok)
	}
	if q, ok := m.Capabilities["write-files"].(string); !ok || q != "notebook" {
		t.Errorf("write-files cap = %v, want notebook string", m.Capabilities["write-files"])
	}
}

func TestValidate_RejectsDeferredExecCapability(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "exec.silt-plugin")
	writeZip(t, archive, map[string]string{
		"plugin.json": manifestWithCaps("exec", map[string]any{"exec": true}),
		"index.js":    "x",
	})
	if _, _, err := Validate(archive); err == nil {
		t.Fatal("Validate must reject the deferred 'exec' capability")
	}
}

func TestValidate_RejectsUnknownCapability(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "bogus.silt-plugin")
	writeZip(t, archive, map[string]string{
		"plugin.json": manifestWithCaps("bogus", map[string]any{"totally-fake": true}),
		"index.js":    "x",
	})
	if _, _, err := Validate(archive); err == nil {
		t.Fatal("Validate must reject an unknown capability")
	}
}

func TestValidate_RejectsBadQualifier(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "badq.silt-plugin")
	writeZip(t, archive, map[string]string{
		"plugin.json": manifestWithCaps("badq", map[string]any{"network": "global"}),
		"index.js":    "x",
	})
	if _, _, err := Validate(archive); err == nil {
		t.Fatal("Validate must reject an invalid scope qualifier")
	}
}

func TestNormalizeCapabilities_EmptyAndTrue(t *testing.T) {
	out, err := NormalizeCapabilities(map[string]any{})
	if err != nil {
		t.Fatalf("empty: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("empty -> %v, want empty map", out)
	}
	out, err = NormalizeCapabilities(map[string]any{"network": true})
	if err != nil {
		t.Fatalf("true: %v", err)
	}
	if out["network"] != QualGranted {
		t.Errorf("true -> %q, want granted", out["network"])
	}
}

// An invalid qualifier reports the scopes meaningful for *that capability*
// (write-files honors granted|notebook|vault), not a misleading "granted" from
// treating the qualifier string itself as the capability.
func TestNormalizeCapabilities_InvalidQualifierMessage(t *testing.T) {
	_, err := NormalizeCapabilities(map[string]any{"write-files": "global"})
	if err == nil {
		t.Fatal("expected error for invalid qualifier")
	}
	msg := err.Error()
	if !strings.Contains(msg, "write-files") || !strings.Contains(msg, "granted|notebook|vault") {
		t.Errorf("error %q must name the capability and its valid scopes", msg)
	}
	if strings.Contains(msg, `(expected granted)`) {
		t.Errorf("error %q still uses the misleading default-scope message", msg)
	}
}

func TestValidateSettingsSchema(t *testing.T) {
	good := []map[string]any{
		{"key": "columns", "label": "Columns", "type": "list", "default": []any{"TODO", "DONE"}},
		{"key": "color", "label": "Accent", "type": "color"},
	}
	if err := validateSettingsSchema(good); err != nil {
		t.Fatalf("good schema rejected: %v", err)
	}

	badCases := []struct {
		name string
		s    []map[string]any
	}{
		{"missing key", []map[string]any{{"label": "X", "type": "string"}}},
		{"missing label", []map[string]any{{"key": "x", "type": "string"}}},
		{"bad type", []map[string]any{{"key": "x", "label": "X", "type": "bogus"}}},
		{"duplicate key", []map[string]any{
			{"key": "x", "label": "X", "type": "string"},
			{"key": "x", "label": "Y", "type": "string"},
		}},
		// #155: default vs declared type mismatches
		{"string default is number", []map[string]any{{"key": "x", "label": "X", "type": "string", "default": float64(42)}}},
		{"number default is string", []map[string]any{{"key": "x", "label": "X", "type": "number", "default": "not-a-num"}}},
		{"bool default is string", []map[string]any{{"key": "x", "label": "X", "type": "bool", "default": "true"}}},
		{"select default not in options", []map[string]any{{"key": "x", "label": "X", "type": "select", "options": []any{"A", "B"}, "default": "C"}}},
		{"select default without options", []map[string]any{{"key": "x", "label": "X", "type": "select", "default": "A"}}},
		{"color default is named color", []map[string]any{{"key": "x", "label": "X", "type": "color", "default": "red"}}},
		{"color default is hsl", []map[string]any{{"key": "x", "label": "X", "type": "color", "default": "hsl(0, 100%, 50%)"}}},
		{"keymap default is number", []map[string]any{{"key": "x", "label": "X", "type": "keymap", "default": float64(42)}}},
		{"list default is string", []map[string]any{{"key": "x", "label": "X", "type": "list", "default": "not-array"}}},
	}
	for _, bc := range badCases {
		if err := validateSettingsSchema(bc.s); err == nil {
			t.Errorf("%s: expected error, got nil", bc.name)
		}
	}

	// Good defaults that SHOULD pass.
	goodDefaults := []map[string]any{
		{"key": "name", "label": "Name", "type": "string", "default": "hello"},
		{"key": "count", "label": "Count", "type": "number", "default": float64(5)},
		{"key": "enabled", "label": "Enabled", "type": "bool", "default": true},
		{"key": "mode", "label": "Mode", "type": "select", "options": []any{"A", "B"}, "default": "A"},
		{"key": "accent", "label": "Accent", "type": "color", "default": "#ff0000"},
		{"key": "hotkey", "label": "Hotkey", "type": "keymap", "default": "Ctrl+K"},
		{"key": "tags", "label": "Tags", "type": "list", "default": []any{"a", "b"}},
	}
	if err := validateSettingsSchema(goodDefaults); err != nil {
		t.Fatalf("good defaults rejected: %v", err)
	}
}

// =========================================================================
// Runtime integrity verification (#161)
// =========================================================================

func TestInstall_ComputesContentSHA256(t *testing.T) {
	vault := t.TempDir()
	_ = os.MkdirAll(filepath.Join(vault, ".system", "plugins"), 0o755)
	archive := filepath.Join(t.TempDir(), "sha.silt-plugin")
	indexJS := `export default { manifest: { id: "sha", name: "SHA" } };`
	writeZip(t, archive, map[string]string{
		"plugin.json": manifestJSON("sha", "SHA", "1.0.0"),
		"index.js":    indexJS,
	})

	m, err := Install(vault, archive)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}

	// The manifest should carry a non-empty contentSha256.
	if m.ContentSHA256 == "" {
		t.Fatal("Install should compute and set ContentSHA256")
	}

	// Read the on-disk plugin.json and verify it contains contentSha256.
	onDiskManifest, err := os.ReadFile(filepath.Join(vault, ".system", "plugins", "sha", "plugin.json"))
	if err != nil {
		t.Fatalf("read on-disk manifest: %v", err)
	}
	var check struct {
		ContentSha256 string `json:"contentSha256"`
	}
	if err := json.Unmarshal(onDiskManifest, &check); err != nil {
		t.Fatalf("parse on-disk manifest: %v", err)
	}
	if check.ContentSha256 == "" {
		t.Fatal("on-disk plugin.json should carry contentSha256")
	}

	// The hash should match the actual index.js content.
	hash := sha256hex([]byte(indexJS))
	if check.ContentSha256 != hash {
		t.Errorf("contentSha256 = %q, want %q", check.ContentSha256, hash)
	}

	// Tampering with index.js changes the hash (the stored hash no longer matches).
	tamperedPath := filepath.Join(vault, ".system", "plugins", "sha", "index.js")
	if err := os.WriteFile(tamperedPath, []byte("TAMPERED"), 0o644); err != nil {
		t.Fatalf("tamper: %v", err)
	}
	tamperedHash := sha256hex([]byte("TAMPERED"))
	if tamperedHash == check.ContentSha256 {
		t.Error("tampered content should produce a different hash")
	}
}

func TestListPlugins_CarriesContentSHA256(t *testing.T) {
	vault := t.TempDir()
	_ = os.MkdirAll(filepath.Join(vault, ".system", "plugins"), 0o755)
	archive := filepath.Join(t.TempDir(), "list-sha.silt-plugin")
	writeZip(t, archive, map[string]string{
		"plugin.json": manifestJSON("list-sha", "List SHA", "1.0.0"),
		"index.js":    `export default {};`,
	})
	if _, err := Install(vault, archive); err != nil {
		t.Fatalf("Install: %v", err)
	}

	// Read the on-disk manifest and verify it has contentSha256.
	data, err := os.ReadFile(filepath.Join(vault, ".system", "plugins", "list-sha", "plugin.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	if m.ContentSHA256 == "" {
		t.Fatal("ListPlugins should surface contentSha256 from the on-disk manifest")
	}
}

// sha256hex computes the hex-encoded sha256 of data (test helper for #161).
func sha256hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// =========================================================================
// First-party id reservation (#240, audit F5)
// =========================================================================

// TestValidate_RejectsFirstPartyIDs verifies that every reserved first-party
// id is rejected at install time. Without this gate a third-party archive
// claiming "silt-kanban" would install cleanly into
// .system/plugins/silt-kanban/ and either confuse the user with a duplicate
// Settings entry (while the real Kanban runs from the bundle) or, should the
// bundle ever drop the id, inherit the seeded first-party grants.
func TestValidate_RejectsFirstPartyIDs(t *testing.T) {
	for id := range FirstPartyPluginIDs {
		t.Run(id, func(t *testing.T) {
			archive := filepath.Join(t.TempDir(), "impostor.silt-plugin")
			writeZip(t, archive, map[string]string{
				"plugin.json": manifestJSON(id, "Impostor", "1.0.0"),
				"index.js":    "x",
			})
			_, _, err := Validate(archive)
			if err == nil {
				t.Fatalf("Validate must reject reserved first-party id %q", id)
			}
			if !strings.Contains(err.Error(), "reserved for a bundled plugin") {
				t.Errorf("error %q must explain the id is reserved", err)
			}
		})
	}
}

// TestValidate_AcceptsNearCollisionIDs verifies the reservation gate is an
// EXACT match — ids that merely share a prefix or suffix with a first-party
// id must still be accepted so the gate does not over-broadly reject
// legitimate plugin ids.
func TestValidate_AcceptsNearCollisionIDs(t *testing.T) {
	nearMisses := []string{
		"silt-kanban2",     // suffix
		"silt-kanban-evil", // extra segment
		"silt-agenda2",     // suffix
		"silts-kanban",     // prefix typo
		"my-silt-kanban",   // prefix
	}
	for _, id := range nearMisses {
		t.Run(id, func(t *testing.T) {
			archive := filepath.Join(t.TempDir(), "near.silt-plugin")
			writeZip(t, archive, map[string]string{
				"plugin.json": manifestJSON(id, "Near Miss", "1.0.0"),
				"index.js":    "x",
			})
			if _, _, err := Validate(archive); err != nil {
				t.Errorf("Validate must accept near-collision id %q (exact-match gate): %v", id, err)
			}
		})
	}
}

// TestInstall_RejectsFirstPartyID verifies the defense-in-depth check inside
// Install: even if Validate somehow returned a manifest with a first-party id
// (it never should), Install must refuse to create the on-disk plugin
// directory. The reserved-id gate is too important to live in one place only.
func TestInstall_RejectsFirstPartyID(t *testing.T) {
	vault := t.TempDir()
	_ = os.MkdirAll(filepath.Join(vault, ".system", "plugins"), 0o755)
	archive := filepath.Join(t.TempDir(), "impostor.silt-plugin")
	writeZip(t, archive, map[string]string{
		"plugin.json": manifestJSON("silt-kanban", "Impostor", "1.0.0"),
		"index.js":    "x",
	})

	if _, err := Install(vault, archive); err == nil {
		t.Fatal("Install must reject a reserved first-party id")
	}
	// And critically, no directory was created on disk.
	if _, err := os.Stat(filepath.Join(vault, ".system", "plugins", "silt-kanban")); !os.IsNotExist(err) {
		t.Errorf("Install must not create a directory for a reserved id; stat err=%v", err)
	}
}

// TestInstall_PreservesUnknownManifestFields verifies the sha256 injection
// round-trips plugin.json through a generic map so custom/unknown fields the
// author included (repository, bugs, keywords, ...) survive on disk instead of
// being dropped by a struct marshal.
func TestInstall_PreservesUnknownManifestFields(t *testing.T) {
	vault := t.TempDir()
	_ = os.MkdirAll(filepath.Join(vault, ".system", "plugins"), 0o755)
	archive := filepath.Join(t.TempDir(), "custom.silt-plugin")
	// Manifest carries custom metadata the Manifest struct does not model.
	custom, _ := json.Marshal(map[string]any{
		"id":        "custom",
		"name":      "Custom",
		"version":   "1.0.0",
		"main":      "index.js",
		"repository": "https://example.com/repo",
		"keywords":  []string{"notes", "demo"},
		"bugs":      map[string]any{"url": "https://example.com/issues"},
	})
	writeZip(t, archive, map[string]string{
		"plugin.json": string(custom),
		"index.js":    "export default {};",
	})

	if _, err := Install(vault, archive); err != nil {
		t.Fatalf("Install: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(vault, ".system", "plugins", "custom", "plugin.json"))
	if err != nil {
		t.Fatalf("read on-disk manifest: %v", err)
	}
	var check map[string]any
	if err := json.Unmarshal(data, &check); err != nil {
		t.Fatalf("parse on-disk manifest: %v", err)
	}
	// The injected integrity hash must be present.
	if check["contentSha256"] == nil || check["contentSha256"] == "" {
		t.Error("on-disk plugin.json should carry contentSha256")
	}
	// Custom author fields must survive the round-trip.
	if check["repository"] != "https://example.com/repo" {
		t.Errorf("custom field repository lost: got %v", check["repository"])
	}
	if keywords, ok := check["keywords"].([]any); !ok || len(keywords) != 2 {
		t.Errorf("custom field keywords lost: got %T %v", check["keywords"], check["keywords"])
	}
	if bugs, ok := check["bugs"].(map[string]any); !ok || bugs["url"] != "https://example.com/issues" {
		t.Errorf("custom field bugs lost: got %v", check["bugs"])
	}
	// And the modeled id is still correct.
	if check["id"] != "custom" {
		t.Errorf("id field corrupted: got %v", check["id"])
	}
}
