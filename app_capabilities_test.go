package main

import (
	"strings"
	"sync"
	"testing"

	"silt/backend/config"
	"silt/backend/plugins"
)

// requireGrant denies an ungranted third-party plugin and returns a structured
// CapabilityDeniedError (never a generic error or a panic).
func TestRequireGrant_DeniesUngrantedThirdParty(t *testing.T) {
	app := newTestApp(t)
	err := app.requireGrant("some-third-party", plugins.CapWriteFiles)
	derr, ok := err.(*plugins.CapabilityDeniedError)
	if !ok {
		t.Fatalf("want *CapabilityDeniedError, got %T (%v)", err, err)
	}
	if derr.Plugin != "some-third-party" {
		t.Errorf("denied error plugin = %q, want %q", derr.Plugin, "some-third-party")
	}
	if derr.Capability != string(plugins.CapWriteFiles) {
		t.Errorf("denied error capability = %q", derr.Capability)
	}
}

// requireGrant implicitly grants a first-party (bundled) plugin every
// capability — they are trusted by definition (#113).
func TestRequireGrant_FirstPartyAlwaysGranted(t *testing.T) {
	app := newTestApp(t)
	for _, cap := range []plugins.Capability{
		plugins.CapWriteFiles,
		plugins.CapNetwork,
		plugins.CapOSOpen,
		plugins.CapEditorSchema,
	} {
		if err := app.requireGrant("silt-kanban", cap); err != nil {
			t.Errorf("first-party silt-kanban %q: want nil, got %v", cap, err)
		}
	}
}

// requireGrant rejects path-traversal pluginIDs before they reach filepath.Join
// in scratch-dir / audit-log paths (#113 security hardening).
func TestRequireGrant_RejectsPathTraversalPluginID(t *testing.T) {
	app := newTestApp(t)
	for _, pid := range []string{
		"../../etc/passwd",
		"..\\..\\Windows\\System32",
		"...",
		"plugin/id",
		"plugin\x00null",
		"",
	} {
		err := app.requireGrant(pid, plugins.CapNetwork)
		if err == nil {
			t.Errorf("requireGrant(%q) should be rejected as invalid pluginID", pid)
		}
	}
}

// A first-party ID works because seedFirstPartyGrants populated the grants
// table at config-load time — NOT because of a special-case bypass. A
// non-first-party ID with no grant entry is denied.
func TestPluginFetch_FirstPartyIDDeniedWithoutSeededGrant(t *testing.T) {
	app := newTestApp(t)
	// "silt-agenda" works because seedFirstPartyGrants ran in newTestApp.
	_ = app.RequestCapability("third-party", string(plugins.CapNetwork), "")

	// A random non-first-party, non-granted ID is denied.
	if err := app.requireGrant("not-a-real-plugin", plugins.CapNetwork); err == nil {
		t.Fatal("ungranted third-party should be denied")
	}
}

// RequestCapability rejects path-traversal pluginIDs (#113 security).
func TestRequestCapability_RejectsInvalidPluginID(t *testing.T) {
	app := newTestApp(t)
	for _, pid := range []string{"../../x", "..\\..\\y", "", "a/b", "a b"} {
		err := app.RequestCapability(pid, string(plugins.CapNetwork), "")
		if err == nil {
			t.Errorf("RequestCapability(%q) should be rejected", pid)
		}
	}
}

// RequestCapability + requireGrant round-trips and persists to config.yaml so
// the grant survives a reload (re-Load).
func TestRequestCapability_RoundTripsAndPersists(t *testing.T) {
	app := newTestApp(t)
	pid := "net-plugin"

	if err := app.RequestCapability(pid, string(plugins.CapNetwork), ""); err != nil {
		t.Fatalf("RequestCapability: %v", err)
	}
	if err := app.requireGrant(pid, plugins.CapNetwork); err != nil {
		t.Fatalf("requireGrant after grant: %v", err)
	}

	// A different capability is still denied.
	if err := app.requireGrant(pid, plugins.CapWriteFiles); err == nil {
		t.Fatal("write-files should still be denied")
	}

	// Qualifier is recorded.
	qual, ok := app.grantedQualifier(pid, plugins.CapNetwork)
	if !ok || qual != plugins.QualGranted {
		t.Errorf("qualifier = %q ok=%v, want granted true", qual, ok)
	}

	// Persisted: reload config from disk and the grant survives.
	cfg, err := reloadConfig(app)
	if err != nil {
		t.Fatalf("reloadConfig: %v", err)
	}
	if got := cfg.Plugins.Grants[pid][string(plugins.CapNetwork)]; got != plugins.QualGranted {
		t.Errorf("persisted grant = %q, want %q", got, plugins.QualGranted)
	}
}

// RequestCapability accepts a qualifier and persists it.
func TestRequestCapability_WithQualifier(t *testing.T) {
	app := newTestApp(t)
	if err := app.RequestCapability("p", string(plugins.CapWriteFiles), "notebook"); err != nil {
		t.Fatalf("RequestCapability: %v", err)
	}
	qual, ok := app.grantedQualifier("p", plugins.CapWriteFiles)
	if !ok || qual != plugins.QualNotebook {
		t.Errorf("qualifier = %q ok=%v, want notebook", qual, ok)
	}
}

// RequestCapability rejects unknown capabilities (so a plugin can't enlarge
// its rights via typos or future names).
func TestRequestCapability_RejectsUnknownCapability(t *testing.T) {
	app := newTestApp(t)
	err := app.RequestCapability("p", "exec", "")
	if err == nil {
		t.Fatal("exec should be rejected (deferred capability)")
	}
	if !strings.Contains(err.Error(), "exec") {
		t.Errorf("error should name exec: %v", err)
	}
	if err := app.RequestCapability("p", "bogus-cap", ""); err == nil {
		t.Fatal("bogus capability should be rejected")
	}
}

// RevokeCapability removes the grant; capability=="" revokes all grants.
func TestRevokeCapability(t *testing.T) {
	app := newTestApp(t)
	pid := "p"
	_ = app.RequestCapability(pid, string(plugins.CapNetwork), "")
	_ = app.RequestCapability(pid, string(plugins.CapOSOpen), "")

	if err := app.RevokeCapability(pid, string(plugins.CapNetwork)); err != nil {
		t.Fatalf("RevokeCapability: %v", err)
	}
	if err := app.requireGrant(pid, plugins.CapNetwork); err == nil {
		t.Fatal("network should be denied after revoke")
	}
	if err := app.requireGrant(pid, plugins.CapOSOpen); err != nil {
		t.Fatalf("os-open should still be granted: %v", err)
	}

	// capability=="" revokes everything.
	if err := app.RevokeCapability(pid, ""); err != nil {
		t.Fatalf("RevokeCapability all: %v", err)
	}
	if err := app.requireGrant(pid, plugins.CapOSOpen); err == nil {
		t.Fatal("os-open should be denied after revoke-all")
	}
}

// GetGrantedCapabilities excludes first-party plugins (they are implicit).
func TestGetGrantedCapabilities_ExcludesFirstParty(t *testing.T) {
	app := newTestApp(t)
	_ = app.RequestCapability("silt-kanban", string(plugins.CapNetwork), "")
	_ = app.RequestCapability("third-party", string(plugins.CapNetwork), "")
	grants, err := app.GetGrantedCapabilities()
	if err != nil {
		t.Fatalf("GetGrantedCapabilities: %v", err)
	}
	if _, ok := grants["silt-kanban"]; ok {
		t.Error("first-party grants should not be surfaced")
	}
	if _, ok := grants["third-party"]; !ok {
		t.Error("third-party grant should be surfaced")
	}
}

// Concurrent RequestCapability callers from many goroutines must not panic
// (configMu serializes the read-modify-write).
func TestRequestCapability_ConcurrentNoPanic(t *testing.T) {
	app := newTestApp(t)
	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			_ = app.RequestCapability("p", string(plugins.CapNetwork), "")
		}()
	}
	wg.Wait()
	if err := app.requireGrant("p", plugins.CapNetwork); err != nil {
		t.Fatalf("grant lost under concurrency: %v", err)
	}
}

// grantedQualifier reports false for an ungranted capability.
func TestGrantedQualifier_Ungranted(t *testing.T) {
	app := newTestApp(t)
	if _, ok := app.grantedQualifier("p", plugins.CapNetwork); ok {
		t.Fatal("ungranted capability should report false")
	}
}

// reloadConfig re-reads the on-disk config.yaml for a test App (without
// re-running applyConfig), used to assert grants persisted.
func reloadConfig(app *App) (config.SystemConfig, error) {
	return config.Load(app.vaultPath)
}
