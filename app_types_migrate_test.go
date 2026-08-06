package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"silt/backend/config"
	"silt/backend/db"
	"silt/backend/types"
)

// TestMigrateReservedTypeProperties_EndToEnd stages a legacy type schema with
// created/aliases properties + a typed page carrying those FM keys, runs the
// vault-open migration, and asserts schema + page + notice + loadability.
func TestMigrateReservedTypeProperties_EndToEnd(t *testing.T) {
	app := newTestApp(t)

	// Legacy type that would fail ValidateTypeDef after #898.
	typesDir := app.typesDir()
	if err := os.MkdirAll(typesDir, 0o700); err != nil {
		t.Fatal(err)
	}
	legacy := `name: Book
heroField: created
properties:
  - name: title
    type: text
  - name: created
    type: date
  - name: aliases
    type: text
`
	if err := os.WriteFile(filepath.Join(typesDir, "book.yaml"), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}

	// Typed page with property values under the reserved keys + an untyped
	// page that owns core created/aliases (must not be rewritten).
	writeCorePage(t, app, "Notes", "", "Dune",
		"notebook: \"Notes\"\nsection: \"\"\npage: \"Dune\"\ntype: book\n"+
			"created: \"2020-01-15\"\naliases: \"Dune Chronicles\"\ntitle: \"Dune\"\n")
	writeCorePage(t, app, "Notes", "", "Journal",
		"notebook: \"Notes\"\nsection: \"\"\npage: \"Journal\"\n"+
			"created: \"2026-08-01T09:00:00\"\naliases: [\"Daily\"]\n")

	// Before migration the type must fail to load.
	if _, err := types.GetType(typesDir, "book"); err == nil {
		t.Fatal("legacy type should fail GetType before migration")
	}

	app.migrateReservedTypeProperties()
	flushReprojection(t, app)

	// Type loads and has renamed properties.
	td, err := types.GetType(typesDir, "book")
	if err != nil {
		t.Fatalf("GetType after migrate: %v", err)
	}
	if td.HeroField != "created_value" {
		t.Errorf("heroField = %q, want created_value", td.HeroField)
	}
	names := map[string]bool{}
	for _, p := range td.Properties {
		names[p.Name] = true
	}
	if names["created"] || names["aliases"] {
		t.Errorf("reserved names still on schema: %v", names)
	}
	if !names["created_value"] || !names["aliases_list"] {
		t.Errorf("renamed props missing: %v", names)
	}

	// Typed page FM keys rewritten; values preserved.
	dunePath := filepath.Join(app.vaultPath, "Notes", "Dune.md")
	duneRaw, err := os.ReadFile(dunePath)
	if err != nil {
		t.Fatal(err)
	}
	dune := string(duneRaw)
	if strings.Contains(dune, "created: \"2020-01-15\"") || strings.Contains(dune, "created: 2020-01-15") {
		// Could still match if format differs — check key presence more carefully.
	}
	if !strings.Contains(dune, "created_value:") {
		t.Errorf("Dune missing created_value:\n%s", dune)
	}
	if !strings.Contains(dune, "aliases_list:") {
		t.Errorf("Dune missing aliases_list:\n%s", dune)
	}
	// Old reserved keys should be gone from the typed page (property values
	// moved). A bare `created:` / `aliases:` line must not remain.
	for _, line := range strings.Split(dune, "\n") {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "created:") && !strings.HasPrefix(trim, "created_value:") {
			t.Errorf("Dune still has reserved created key: %q", trim)
		}
		if strings.HasPrefix(trim, "aliases:") && !strings.HasPrefix(trim, "aliases_list:") {
			t.Errorf("Dune still has reserved aliases key: %q", trim)
		}
	}
	if !strings.Contains(dune, "2020-01-15") {
		t.Errorf("created value not preserved:\n%s", dune)
	}
	if !strings.Contains(dune, "Dune Chronicles") {
		t.Errorf("aliases value not preserved:\n%s", dune)
	}

	// Untyped page core metadata untouched.
	journalPath := filepath.Join(app.vaultPath, "Notes", "Journal.md")
	journalRaw, _ := os.ReadFile(journalPath)
	journal := string(journalRaw)
	if !strings.Contains(journal, "created: \"2026-08-01T09:00:00\"") && !strings.Contains(journal, "created: 2026-08-01T09:00:00") {
		t.Errorf("untyped page created core field disturbed:\n%s", journal)
	}
	if strings.Contains(journal, "created_value:") {
		t.Errorf("untyped page should not gain created_value:\n%s", journal)
	}

	// Notice stamped.
	cfg, err := config.Load(app.vaultPath)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	foundNotice := false
	for _, tip := range cfg.UI.DismissedTips {
		if tip == reservedPropRenameNotice {
			foundNotice = true
		}
		if tip == reservedPropRenameAck {
			t.Error("ack should not be stamped on migrate")
		}
	}
	if !foundNotice {
		t.Errorf("dismissed_tips missing notice stamp: %v", cfg.UI.DismissedTips)
	}
	rawList, ok := cfg.Plugins.PluginSettings[reservedPropRenamesSettingsKey]
	if !ok {
		t.Fatal("plugin_settings missing rename list")
	}
	list, ok := rawList.([]any)
	if !ok || len(list) < 2 {
		t.Errorf("rename list = %#v, want ≥2 entries", rawList)
	}

	// ListTypes succeeds with the book type present.
	res, err := app.ListTypes()
	if err != nil {
		t.Fatalf("ListTypes: %v", err)
	}
	found := false
	for _, tdef := range res.Types {
		if tdef.ID == "book" {
			found = true
		}
	}
	if !found {
		t.Errorf("book type missing from ListTypes: %+v", res.Types)
	}

	// Second pass is a no-op (no extra renames; notice may re-stamp — that's OK).
	app.migrateReservedTypeProperties()
	td2, err := types.GetType(typesDir, "book")
	if err != nil {
		t.Fatal(err)
	}
	if td2.HeroField != "created_value" {
		t.Errorf("second pass disturbed heroField: %q", td2.HeroField)
	}
}

// TestMigrateReservedTypeProperties_PageCoreOnlyPath exercises the cold-index
// branch: page present in page_core but absent from page_types (warm-skip /
// projection lag). Migration must still rewrite FM via ListPageCoreTypeMatches.
func TestMigrateReservedTypeProperties_PageCoreOnlyPath(t *testing.T) {
	app := newTestApp(t)

	typesDir := app.typesDir()
	if err := os.MkdirAll(typesDir, 0o700); err != nil {
		t.Fatal(err)
	}
	legacy := `name: Book
properties:
  - name: title
    type: text
  - name: created
    type: date
`
	if err := os.WriteFile(filepath.Join(typesDir, "book.yaml"), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}

	// File on disk only — do not indexFile/writeCorePage (those project page_types).
	fmBody := "notebook: \"Notes\"\nsection: \"\"\npage: \"Dune\"\ntype: book\n" +
		"created: \"2020-01-15\"\ntitle: \"Dune\"\n"
	content := "---\n" + fmBody + "---\n# Dune\n\nBody.\n"
	dunePath := filepath.Join(app.vaultPath, "Notes", "Dune.md")
	writeFile(t, dunePath, content)

	if err := app.db.IndexPageCore("vault", "Notes", "", "Dune", db.PageCoreFields{
		Type:    "book",
		Created: "2020-01-15",
	}); err != nil {
		t.Fatalf("IndexPageCore: %v", err)
	}
	// Cold path = page_core only. Clear any page_types row so migrate cannot
	// satisfy the locator via GetTypedPageLocatorsByIDs (mirrors warm-skip).
	if err := app.db.ClearPageProjection("vault", "Notes", "", "Dune"); err != nil {
		t.Fatalf("ClearPageProjection: %v", err)
	}
	core, err := app.db.GetPageCoreProjection("vault", "Notes", "", "Dune")
	if err != nil || core == nil || core.Type != "book" {
		t.Fatalf("precondition page_core: row=%+v err=%v", core, err)
	}

	typedLocs, err := app.db.GetTypedPageLocatorsByIDs([]string{"book"})
	if err != nil {
		t.Fatalf("GetTypedPageLocatorsByIDs: %v", err)
	}
	if len(typedLocs) != 0 {
		t.Fatalf("precondition: page_types has %d book locators, want 0 (cold path)", len(typedLocs))
	}

	app.migrateReservedTypeProperties()

	duneRaw, err := os.ReadFile(dunePath)
	if err != nil {
		t.Fatal(err)
	}
	dune := string(duneRaw)
	if !strings.Contains(dune, "created_value:") {
		t.Errorf("Dune missing created_value after page_core-only migrate:\n%s", dune)
	}
	for _, line := range strings.Split(dune, "\n") {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "created:") && !strings.HasPrefix(trim, "created_value:") {
			t.Errorf("Dune still has reserved created key: %q", trim)
		}
	}
	if !strings.Contains(dune, "2020-01-15") {
		t.Errorf("created value not preserved:\n%s", dune)
	}

	td, err := types.GetType(typesDir, "book")
	if err != nil {
		t.Fatalf("GetType after migrate: %v", err)
	}
	names := map[string]bool{}
	for _, p := range td.Properties {
		names[p.Name] = true
	}
	if names["created"] || !names["created_value"] {
		t.Errorf("schema props = %v, want created_value and no created", names)
	}
}

func TestMigrateReservedTypeProperties_NoopCleanVault(t *testing.T) {
	app := newTestApp(t)
	// Seed types (book/meeting) have no reserved props.
	app.migrateReservedTypeProperties()
	cfg, err := config.Load(app.vaultPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, tip := range cfg.UI.DismissedTips {
		if tip == reservedPropRenameNotice {
			t.Error("clean vault must not stamp migration notice")
		}
	}
	if _, ok := cfg.Plugins.PluginSettings[reservedPropRenamesSettingsKey]; ok {
		t.Error("clean vault must not write rename list")
	}
}

func TestMigrateReservedTypeProperties_SaveStillRejects(t *testing.T) {
	app := newTestApp(t)
	err := types.SaveType(app.typesDir(), &types.TypeDef{
		Name:       "Bad",
		Properties: []types.PropertyDef{{Name: "created", Type: types.PropText}},
	})
	if err == nil {
		t.Fatal("SaveType must still reject reserved created")
	}
	if !strings.Contains(err.Error(), "rename the property") {
		t.Errorf("error = %v", err)
	}
}
