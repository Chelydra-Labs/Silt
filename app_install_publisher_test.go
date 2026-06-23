package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"silt/backend/parser"
	"silt/backend/vault"
)

// writeSiltPluginArchive builds a .silt-plugin archive at dest whose
// plugin.json carries the given author. The plugin is a no-op (empty
// index.js) since the publisher-trust test never runs the plugin code.
func writeSiltPluginArchive(t *testing.T, dest, id, name, version, author string) {
	t.Helper()
	buf := new(bytes.Buffer)
	w := zip.NewWriter(buf)
	manifest := map[string]string{
		"id":      id,
		"name":    name,
		"version": version,
		"main":    "index.js",
	}
	if author != "" {
		manifest["author"] = author
	}
	mb, _ := json.Marshal(manifest)
	mf, err := w.Create("plugin.json")
	if err != nil {
		t.Fatalf("create plugin.json: %v", err)
	}
	if _, err := mf.Write(mb); err != nil {
		t.Fatalf("write plugin.json: %v", err)
	}
	idx, err := w.Create("index.js")
	if err != nil {
		t.Fatalf("create index.js: %v", err)
	}
	if _, err := idx.Write([]byte("export default { manifest: { id: '" + id + "' } };\n")); err != nil {
		t.Fatalf("write index.js: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	if err := os.WriteFile(dest, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write archive: %v", err)
	}
}

// saveTrustedPublishers is a tiny helper that writes a settings.json
// containing only TrustedPublishers (and the dark-default theme), overriding
// the process-wide UserConfigDir via APPDATA / XDG_CONFIG_HOME so tests do
// not leak state across runs.
func saveTrustedPublishers(t *testing.T, settingsDir string, list []string) {
	t.Helper()
	t.Setenv("APPDATA", settingsDir)
	t.Setenv("XDG_CONFIG_HOME", settingsDir)
	// Marker so newTestApp respects our APPDATA override instead of replacing
	// it with its own temp dir (F4 isolation).
	t.Setenv("SILT_TEST_HOST_CONFIG", settingsDir)
	settings := &vault.AppSettings{
		ActiveTheme:       "cyber_forest",
		ThemeMode:         "dark",
		TrustedPublishers: list,
	}
	if err := vault.SaveSettings(settings); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
}

// InstallPlugin honors TrustedPublishers when the list is non-empty (#111
// distribution v2, #150 follow-up). A plugin whose Author is on the list
// installs; a plugin whose Author is not on the list is rejected and the
// partial install is rolled back. An empty Author cannot match a non-empty
// list — anonymous plugins require an explicit trust decision.
func TestInstallPlugin_RejectsUntrustedAuthor(t *testing.T) {
	settingsDir := t.TempDir()
	saveTrustedPublishers(t, settingsDir, []string{"trusted-publisher"})

	app := newTestApp(t)
	archive := filepath.Join(t.TempDir(), "untrusted.silt-plugin")
	writeSiltPluginArchive(t, archive, "untrusted", "Untrusted", "1.0.0", "random-dev")

	_, err := app.InstallPlugin(archive)
	if err == nil {
		t.Fatal("expected rejection for untrusted author")
	}
	// The rollback must have removed the extracted plugin directory so a
	// failed install does not leave dead state behind.
	extracted := filepath.Join(app.vaultPath, ".system", "plugins", "untrusted")
	if _, statErr := os.Stat(extracted); statErr == nil {
		t.Errorf("expected the failed install to be rolled back, but %s exists", extracted)
	}
}

// InstallPlugin allows a plugin whose Author is in the trusted list.
func TestInstallPlugin_AllowsTrustedAuthor(t *testing.T) {
	settingsDir := t.TempDir()
	saveTrustedPublishers(t, settingsDir, []string{"trusted-publisher", "another-publisher"})

	app := newTestApp(t)
	archive := filepath.Join(t.TempDir(), "trusted.silt-plugin")
	writeSiltPluginArchive(t, archive, "trusted", "Trusted", "1.0.0", "trusted-publisher")

	manifest, err := app.InstallPlugin(archive)
	if err != nil {
		t.Fatalf("InstallPlugin: %v", err)
	}
	if manifest.ID != "trusted" {
		t.Errorf("manifest.ID = %q, want %q", manifest.ID, "trusted")
	}
	// Files extracted.
	extracted := filepath.Join(app.vaultPath, ".system", "plugins", "trusted")
	if _, err := os.Stat(filepath.Join(extracted, "index.js")); err != nil {
		t.Errorf("index.js not installed: %v", err)
	}
}

// InstallPlugin rejects a plugin with an empty Author when TrustedPublishers
// is non-empty. Anonymous plugins cannot match a trust list, which is the
// correct defense-in-depth default (#150 follow-up).
func TestInstallPlugin_RejectsAnonymousAuthorWhenTrustListIsSet(t *testing.T) {
	settingsDir := t.TempDir()
	saveTrustedPublishers(t, settingsDir, []string{"trusted-publisher"})

	app := newTestApp(t)
	archive := filepath.Join(t.TempDir(), "anon.silt-plugin")
	writeSiltPluginArchive(t, archive, "anon", "Anonymous", "1.0.0", "")

	_, err := app.InstallPlugin(archive)
	if err == nil {
		t.Fatal("expected rejection for anonymous plugin when TrustedPublishers is non-empty")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("author is empty")) {
		t.Errorf("error = %v, want to mention 'author is empty'", err)
	}
}

// InstallPlugin preserves the current "everyone is welcome" posture when
// TrustedPublishers is empty/nil. Populating the list is an explicit
// opt-in to a stricter stance; the unpopulated state must not break
// existing installs (#150 follow-up backward-compat guarantee).
func TestInstallPlugin_AllowsAllWhenTrustListEmpty(t *testing.T) {
	settingsDir := t.TempDir()
	saveTrustedPublishers(t, settingsDir, nil)

	app := newTestApp(t)
	archive := filepath.Join(t.TempDir(), "anyone.silt-plugin")
	writeSiltPluginArchive(t, archive, "anyone", "Anyone", "1.0.0", "any-author")

	manifest, err := app.InstallPlugin(archive)
	if err != nil {
		t.Fatalf("InstallPlugin with empty trust list should allow any author: %v", err)
	}
	if manifest.ID != "anyone" {
		t.Errorf("manifest.ID = %q, want %q", manifest.ID, "anyone")
	}
}

// Author matching is case-insensitive and trims whitespace so a user's
// TrustedPublishers entry like "  Trusted-Publisher  " matches a manifest
// that declares "TRUSTED-PUBLISHER" via TrimSpace on the manifest side
// (a JSON string is already trimmed, but the user list may not be).
func TestInstallPlugin_AuthorMatchIsCaseInsensitiveTrimmed(t *testing.T) {
	settingsDir := t.TempDir()
	saveTrustedPublishers(t, settingsDir, []string{"  Trusted-Publisher  "})

	app := newTestApp(t)
	archive := filepath.Join(t.TempDir(), "case.silt-plugin")
	writeSiltPluginArchive(t, archive, "case", "Case", "1.0.0", "TRUSTED-PUBLISHER")

	_, err := app.InstallPlugin(archive)
	if err != nil {
		t.Fatalf("expected case-insensitive trimmed match: %v", err)
	}
}

// enforcePublisherTrust unit-tests the gate function directly so the
// policy is pinned without the InstallPlugin setup overhead. The
// LoadSettings error path is a fail-open by design (transient settings
// I/O must not brick plugin installs).
func TestEnforcePublisherTrust_Policy(t *testing.T) {
	settingsDir := t.TempDir()
	saveTrustedPublishers(t, settingsDir, []string{"acme"})

	t.Run("empty list allows any author", func(t *testing.T) {
		emptyDir := t.TempDir()
		saveTrustedPublishers(t, emptyDir, nil)
		if err := enforcePublisherTrust("anything"); err != nil {
			t.Errorf("empty list should allow, got %v", err)
		}
	})
	t.Run("listed author is allowed", func(t *testing.T) {
		if err := enforcePublisherTrust("acme"); err != nil {
			t.Errorf("listed author should be allowed, got %v", err)
		}
	})
	t.Run("unlisted author is rejected", func(t *testing.T) {
		err := enforcePublisherTrust("random")
		if err == nil {
			t.Fatal("unlisted author should be rejected")
		}
	})
	t.Run("empty author is rejected when list is set", func(t *testing.T) {
		err := enforcePublisherTrust("")
		if err == nil {
			t.Fatal("empty author should be rejected when list is set")
		}
	})
}

// silence the unused-import linter when this file is the only consumer
// of these packages in its package; the compiler would otherwise fail
// on a fresh tree that hasn't pulled in parser yet.
var _ = parser.PluginManifest{}
