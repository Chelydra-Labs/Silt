package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"silt/backend/config"
	"silt/backend/types"
)

// These are characterization tests (Feathers) for the linked-notebook subsystem
// living in app_notebooks.go. They pin the CURRENT behavior of the paths that
// were NOT already covered by app_api_test.go's linked-notebook suite, so a
// behavior-preserving file relocation (app_notebooks.go -> app_linked_notebooks.go
// + friends) has a safety net. Each test asserts what the code does today,
// quirks included — not what it "should" do.
//
// Reuses the canonical harness from app_api_test.go: newTestApp, writeFile, and
// direct config.Load / config.Save for registry assertions. app.watcher is left
// nil by newTestApp, exactly as the existing linked-notebook tests assume
// (LinkNotebook/UnlinkNotebook guard every watcher call behind `!= nil`).

// TestLinkNotebook_RejectsVaultAncestor pins the containment guard: linking a
// folder that CONTAINS the vault is rejected. Without this, the watcher would
// observe the vault itself as part of the linked root and double-index it. The
// only on-disk ancestor of the scaffolded vault is its parent dir, so that is
// what we link — LinkNotebook rejects before any watch/index side effect.
func TestLinkNotebook_RejectsVaultAncestor(t *testing.T) {
	app := newTestApp(t)

	ancestor := filepath.Dir(app.vaultPath)
	if _, err := os.Stat(ancestor); err != nil {
		t.Fatalf("vault ancestor dir must exist: %v", err)
	}

	_, err := app.LinkNotebook(ancestor)
	if err == nil {
		t.Fatal("expected error linking a folder that contains the vault, got nil")
	}
	if !strings.Contains(err.Error(), "contains the vault") {
		t.Fatalf("error = %v, want it to mention containing the vault", err)
	}

	// The rejected link must not have been registered.
	app.configMu.RLock()
	got := len(app.cfg.LinkedNotebooks)
	app.configMu.RUnlock()
	if got != 0 {
		t.Errorf("rejected ancestor link was registered: %d links present", got)
	}
}

// TestLinkNotebook_RejectsDuplicateLinks pins rejectLinkCollision's existing-
// link branch: a second link whose RootPath OR DisplayName matches an already-
// registered link is refused. The existing suite only covers a collision with a
// VAULT notebook; the link-vs-link duplicate paths are untested.
func TestLinkNotebook_RejectsDuplicateLinks(t *testing.T) {
	t.Run("same RootPath", func(t *testing.T) {
		app := newTestApp(t)
		parent := t.TempDir()
		ext := filepath.Join(parent, "Shared")
		if err := os.MkdirAll(ext, 0o755); err != nil {
			t.Fatal(err)
		}

		if _, err := app.LinkNotebook(ext); err != nil {
			t.Fatalf("first link: %v", err)
		}
		// Linking the SAME folder again must be rejected.
		if _, err := app.LinkNotebook(ext); err == nil {
			t.Fatal("expected error re-linking the same folder, got nil")
		}
		app.configMu.RLock()
		got := len(app.cfg.LinkedNotebooks)
		app.configMu.RUnlock()
		if got != 1 {
			t.Errorf("duplicate RootPath link should leave 1 link registered, got %d", got)
		}
	})

	t.Run("same DisplayName from distinct folder", func(t *testing.T) {
		app := newTestApp(t)
		folderA := filepath.Join(t.TempDir(), "Shared")
		folderB := filepath.Join(t.TempDir(), "Shared")
		for _, p := range []string{folderA, folderB} {
			if err := os.MkdirAll(p, 0o755); err != nil {
				t.Fatal(err)
			}
		}

		if _, err := app.LinkNotebook(folderA); err != nil {
			t.Fatalf("first link: %v", err)
		}
		// Different folder, same base name -> ambiguous in the sidebar; rejected.
		if _, err := app.LinkNotebook(folderB); err == nil {
			t.Fatal("expected error linking a second folder with the same display name, got nil")
		}
		app.configMu.RLock()
		got := len(app.cfg.LinkedNotebooks)
		app.configMu.RUnlock()
		if got != 1 {
			t.Errorf("duplicate DisplayName link should leave 1 link registered, got %d", got)
		}
	})
}

// TestLinkNotebook_ProjectsTypedPages pins the typed-notes contract for linked
// notebooks: indexLinkedTree must copy meta.Type + meta.Frontmatter onto the
// ScanResult so the post-index projectPageType loop populates page_types /
// page_properties. Without those fields, linked typed pages never appear on
// dashboards and a relink clears any prior projection.
func TestLinkNotebook_ProjectsTypedPages(t *testing.T) {
	app := newTestApp(t)
	// Unique type id so shared in-memory DB book rows from other tests cannot
	// leak into (or be polluted by) this assertion.
	if err := app.SaveType(types.TypeDef{
		ID:   "linkproj",
		Name: "LinkProj",
		Properties: []types.PropertyDef{
			{Name: "title", Type: types.PropText},
			{Name: "rating", Type: types.PropNumber},
		},
	}); err != nil {
		t.Fatalf("SaveType(linkproj): %v", err)
	}

	ext := filepath.Join(t.TempDir(), "LinkedBooks")
	if err := os.MkdirAll(ext, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(ext, "Dune.md"),
		"---\nnotebook: LinkedBooks\nsection: \"\"\npage: Dune\ndate: 2026-08-01\ntags: []\n"+
			"type: linkproj\ntitle: \"Dune\"\nrating: 5\n---\n# Dune\n")

	ln, err := app.LinkNotebook(ext)
	if err != nil {
		t.Fatalf("LinkNotebook: %v", err)
	}

	rows, err := app.QueryPagesByType("linkproj", nil, "", false)
	if err != nil {
		t.Fatalf("QueryPagesByType: %v", err)
	}
	if len(rows) != 1 || rows[0].Source != ln.Source() || rows[0].Page != "Dune" {
		t.Fatalf("linked typed page missing from dashboard; got %+v", rows)
	}
	propVals := map[string]string{}
	for _, p := range rows[0].Properties {
		propVals[p.Name] = p.ValueText
	}
	if propVals["title"] != "Dune" || propVals["rating"] != "5" {
		t.Errorf("projected props = %v, want title=Dune rating=5", propVals)
	}
}

// TestLinkNotebook_ForcesDisplayNameAsNotebookColumn pins indexLinkedTree's
// forced-notebook quirk: a linked file's frontmatter `notebook:` value is NOT
// what lands in the blocks table. indexLinkedTree sets ScanResult.Notebook to
// the link's DisplayName (the external root IS one notebook), so the row is
// queryable under notebook = DisplayName even when the file's own frontmatter
// disagrees. The existing batched-indexing tests only use files whose
// frontmatter already matches the DisplayName, so this override is unexercised.
func TestLinkNotebook_ForcesDisplayNameAsNotebookColumn(t *testing.T) {
	app := newTestApp(t)

	// A named external root so DisplayName is predictable ("Ext").
	ext := filepath.Join(t.TempDir(), "Ext")
	if err := os.MkdirAll(ext, 0o755); err != nil {
		t.Fatal(err)
	}
	// The file's frontmatter deliberately lies about the notebook name.
	writeFile(t, filepath.Join(ext, "Plan.md"),
		"---\nnotebook: DifferentName\nsection: \"\"\npage: Plan\ndate: 2026-06-16\ntags: []\n---\n"+
			"# Plan\n- [ ] do <!-- id: aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa -->\n")

	ln, err := app.LinkNotebook(ext)
	if err != nil {
		t.Fatalf("LinkNotebook: %v", err)
	}
	if ln.DisplayName != "Ext" {
		t.Fatalf("DisplayName = %q, want Ext", ln.DisplayName)
	}
	src := ln.Source()

	// The indexed row carries the FORCED DisplayName, not the frontmatter value.
	var indexedNotebook string
	var frontmatterLeak int
	app.coordinator.WithDBRead(func() {
		_ = app.db.SQLDB().QueryRow(
			"SELECT notebook FROM blocks WHERE source = ? LIMIT 1", src,
		).Scan(&indexedNotebook)
		_ = app.db.SQLDB().QueryRow(
			"SELECT COUNT(*) FROM blocks WHERE source = ? AND notebook = ?", src, "DifferentName",
		).Scan(&frontmatterLeak)
	})
	if indexedNotebook != "Ext" {
		t.Errorf("blocks.notebook = %q, want forced DisplayName %q", indexedNotebook, "Ext")
	}
	if frontmatterLeak != 0 {
		t.Errorf("frontmatter notebook %q leaked into %d index row(s) under source %s", "DifferentName", frontmatterLeak, src)
	}
}

// TestResolveQuarantinedLinks_SurfacesOnlyQuarantined pins the public IPC that
// the existing quarantine suite never calls directly: ResolveQuarantinedLinks
// returns exactly the quarantined linked notebooks with their full
// (ID, DisplayName, RootPath) shape, and a trusted sibling is excluded.
func TestResolveQuarantinedLinks_SurfacesOnlyQuarantined(t *testing.T) {
	app := newTestApp(t)

	ext1 := filepath.Join(t.TempDir(), "Quarantined")
	ext2 := filepath.Join(t.TempDir(), "Trusted")
	for _, p := range []string{ext1, ext2} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	ln1, err := app.LinkNotebook(ext1)
	if err != nil {
		t.Fatalf("link ext1: %v", err)
	}
	ln2, err := app.LinkNotebook(ext2)
	if err != nil {
		t.Fatalf("link ext2: %v", err)
	}

	// Corrupt ln1's stored fingerprint and run the vault-open verification,
	// which quarantines any link whose stored FP no longer matches the root.
	app.configMu.Lock()
	for i := range app.cfg.LinkedNotebooks {
		if app.cfg.LinkedNotebooks[i].ID == ln1.ID {
			app.cfg.LinkedNotebooks[i].RootFingerprint = "corrupted-fingerprint"
			break
		}
	}
	if err := config.Save(app.vaultPath, app.cfg); err != nil {
		t.Fatalf("config.Save: %v", err)
	}
	app.configMu.Unlock()

	app.quarantinedLinks = make(map[string]struct{})
	app.verifyLinkedNotebookFingerprints()

	q, err := app.ResolveQuarantinedLinks()
	if err != nil {
		t.Fatalf("ResolveQuarantinedLinks: %v", err)
	}
	if len(q) != 1 {
		t.Fatalf("expected exactly 1 quarantined link, got %d: %+v", len(q), q)
	}
	got := q[0]
	if got.ID != ln1.ID || got.DisplayName != ln1.DisplayName || got.RootPath != ln1.RootPath {
		t.Errorf("quarantined entry = %+v, want {ID:%s DisplayName:%s RootPath:%s}",
			got, ln1.ID, ln1.DisplayName, ln1.RootPath)
	}
	// The trusted sibling must NOT appear.
	for _, e := range q {
		if e.ID == ln2.ID {
			t.Errorf("trusted link %s was incorrectly surfaced as quarantined", ln2.ID)
		}
	}
}

// TestGetPluginSettingsForNotebook_LinkedRecursiveNestedMapMerge pins the part
// of MergePluginSettings the existing override test leaves unexercised: a
// NESTED map value merges RECURSIVELY (vault's sub-keys survive where linked
// didn't override, linked's sub-keys are added), while a scalar or array value
// from linked REPLACES vault's wholesale. The existing LinkedMergesOverVault
// test only covers top-level per-key merge + array replace at depth 1.
func TestGetPluginSettingsForNotebook_LinkedRecursiveNestedMapMerge(t *testing.T) {
	app := newTestApp(t)

	// Vault baseline: a nested object, an array, and a scalar.
	app.configMu.Lock()
	app.cfg.Plugins.PluginSettings["silt-kanban"] = map[string]any{
		"columns": []any{"TODO", "DOING", "DONE"}, // array -> linked REPLACES
		"ui": map[string]any{ // nested map -> RECURSIVE merge
			"theme":   "light",
			"density": "comfortable",
		},
		"scalar": "vault-value", // scalar -> linked REPLACES
	}
	app.configMu.Unlock()

	// Linked co-located override.
	ext := filepath.Join(t.TempDir(), "Ext")
	if err := os.MkdirAll(ext, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := config.LinkedConfigPath(ext)
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatal(err)
	}
	linkedYAML := "plugins:\n  plugin_settings:\n    silt-kanban:\n" +
		"      columns: [Backlog]\n" + // replaces whole array
		"      ui:\n        density: compact\n        lang: en\n" + // recursive merge
		"      scalar: linked-value\n" // replaces scalar
	if err := os.WriteFile(cfgPath, []byte(linkedYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	app.configMu.Lock()
	app.cfg.LinkedNotebooks = []config.LinkedNotebook{
		{ID: "ext", RootPath: ext, DisplayName: "Ext"},
	}
	app.configMu.Unlock()

	got, err := app.GetPluginSettingsForNotebook("silt-kanban", "Ext")
	if err != nil {
		t.Fatalf("GetPluginSettingsForNotebook: %v", err)
	}

	// Array replaced wholesale (not concatenated/merged).
	cols, ok := got["columns"].([]any)
	if !ok || len(cols) != 1 || cols[0] != "Backlog" {
		t.Errorf("nested array should be REPLACED, got %v", got["columns"])
	}

	// Scalar replaced.
	if got["scalar"] != "linked-value" {
		t.Errorf("scalar should be REPLACED, got %v", got["scalar"])
	}

	// Nested map merged RECURSIVELY: vault key survived, linked key overrode,
	// linked-only key added.
	ui, ok := got["ui"].(map[string]any)
	if !ok {
		t.Fatalf("nested 'ui' should be a map, got %T", got["ui"])
	}
	if ui["theme"] != "light" {
		t.Errorf("vault-only nested key 'ui.theme' should survive, got %v", ui["theme"])
	}
	if ui["density"] != "compact" {
		t.Errorf("linked nested key 'ui.density' should override, got %v", ui["density"])
	}
	if ui["lang"] != "en" {
		t.Errorf("linked-only nested key 'ui.lang' should be added, got %v", ui["lang"])
	}
}

// TestUnlinkNotebook_IdempotentOnUnknownID pins the documented idempotency
// contract: unlinking an id that was never registered (or already unlinked) is
// a no-op that returns nil, leaving the registry untouched. This is the
// behavior the frontend relies on when retrying an unlink after a reload.
func TestUnlinkNotebook_IdempotentOnUnknownID(t *testing.T) {
	app := newTestApp(t)

	ext := filepath.Join(t.TempDir(), "Ext")
	if err := os.MkdirAll(ext, 0o755); err != nil {
		t.Fatal(err)
	}
	ln, err := app.LinkNotebook(ext)
	if err != nil {
		t.Fatalf("LinkNotebook: %v", err)
	}

	// Unknown id -> no-op, no error.
	if err := app.UnlinkNotebook("linked-does-not-exist"); err != nil {
		t.Fatalf("UnlinkNotebook on unknown id should be a no-op, got %v", err)
	}

	// The real link is still registered.
	app.configMu.RLock()
	count := len(app.cfg.LinkedNotebooks)
	present := false
	for _, entry := range app.cfg.LinkedNotebooks {
		if entry.ID == ln.ID {
			present = true
		}
	}
	app.configMu.RUnlock()
	if count != 1 || !present {
		t.Errorf("unknown-id unlink disturbed the registry: count=%d present=%v", count, present)
	}
}
