package main

import (
	"os"
	"path/filepath"
	"testing"

	"silt/backend/config"
	"silt/backend/spellcheck"
)

// TestCustomDictionary_RoundTrip covers the #196 custom-dictionary IPC: get is
// empty initially, add lowercases + persists + returns the list, duplicate adds
// are idempotent, remove works, and empty/whitespace input is rejected. Mirrors
// the atomic config-RMW path the production bindings use.
func TestCustomDictionary_RoundTrip(t *testing.T) {
	app := newTestApp(t)

	// Empty + non-nil to start.
	got, err := app.GetCustomDictionary()
	if err != nil {
		t.Fatalf("GetCustomDictionary: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty initial dictionary, got %v", got)
	}

	// Add lowercases the word.
	res, err := app.AddCustomDictionaryWord("TypeScript")
	if err != nil {
		t.Fatalf("Add TypeScript: %v", err)
	}
	if len(res) != 1 || res[0] != "typescript" {
		t.Errorf("add returned %v, want [typescript]", res)
	}

	// Get reflects the add.
	got, err = app.GetCustomDictionary()
	if err != nil {
		t.Fatalf("GetCustomDictionary: %v", err)
	}
	if len(got) != 1 || got[0] != "typescript" {
		t.Errorf("get after add: %v, want [typescript]", got)
	}

	// Duplicate add is idempotent.
	res, err = app.AddCustomDictionaryWord("typescript")
	if err != nil {
		t.Fatalf("duplicate add: %v", err)
	}
	if len(res) != 1 {
		t.Errorf("duplicate add should be idempotent, got %v", res)
	}

	// Add a second word.
	if _, err := app.AddCustomDictionaryWord("OAuth"); err != nil {
		t.Fatalf("Add OAuth: %v", err)
	}
	got, err = app.GetCustomDictionary()
	if err != nil {
		t.Fatalf("GetCustomDictionary: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 words after two adds, got %v", got)
	}

	// Remove (case-insensitive match — normalize lowercased both).
	res, err = app.RemoveCustomDictionaryWord("TYPESCRIPT")
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if len(res) != 1 || res[0] != "oauth" {
		t.Errorf("remove returned %v, want [oauth]", res)
	}

	// Removing a word not present is a no-op (idempotent).
	res, err = app.RemoveCustomDictionaryWord("nonexistent")
	if err != nil {
		t.Fatalf("Remove nonexistent: %v", err)
	}
	if len(res) != 1 {
		t.Errorf("remove of absent word should be idempotent, got %v", res)
	}
}

// TestCustomDictionary_Validation confirms empty/whitespace input is rejected
// rather than producing an empty-dictionary entry.
func TestCustomDictionary_Validation(t *testing.T) {
	app := newTestApp(t)

	for _, bad := range []string{"", "   ", "\t\n"} {
		if _, err := app.AddCustomDictionaryWord(bad); err == nil {
			t.Errorf("AddCustomDictionaryWord(%q) should error", bad)
		}
	}

	// Nothing was added despite the rejected calls.
	got, err := app.GetCustomDictionary()
	if err != nil {
		t.Fatalf("GetCustomDictionary: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("dictionary should still be empty after rejected adds, got %v", got)
	}
}

// TestCustomDictionary_Persists confirms the word survives a fresh config Load
// (the atomic config.Save wrote it to disk, not just the in-memory copy).
func TestCustomDictionary_Persists(t *testing.T) {
	app := newTestApp(t)
	if _, err := app.AddCustomDictionaryWord("docker"); err != nil {
		t.Fatalf("Add: %v", err)
	}

	loaded, err := config.Load(app.vaultPath)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if len(loaded.Editor.CustomDictionary) != 1 || loaded.Editor.CustomDictionary[0] != "docker" {
		t.Errorf("custom_dictionary did not persist: %v", loaded.Editor.CustomDictionary)
	}
}

// TestCustomDictionary_ImportExport covers #338: export writes one word per
// line; import merges with summary; comments and duplicates handled.
func TestCustomDictionary_ImportExport(t *testing.T) {
	app := newTestApp(t)
	if _, err := app.AddCustomDictionaryWord("alpha"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.AddCustomDictionaryWord("beta"); err != nil {
		t.Fatal(err)
	}

	exportPath := filepath.Join(t.TempDir(), "dictionary.txt")
	if err := app.ExportCustomDictionary(exportPath); err != nil {
		t.Fatalf("export: %v", err)
	}
	data, err := os.ReadFile(exportPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "alpha\nbeta\n" {
		t.Errorf("export content = %q", data)
	}

	importPath := filepath.Join(t.TempDir(), "import.txt")
	if err := os.WriteFile(importPath, []byte("# comment\nalpha\ngamma\nBETA\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sum, err := app.ImportCustomDictionary(importPath)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if sum.Added != 1 || sum.Skipped != 2 || sum.TotalRead != 3 {
		t.Errorf("summary = %+v, want added=1 skipped=2 total=3", sum)
	}
	got, err := app.GetCustomDictionary()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Errorf("after import: %v", got)
	}
}

func TestListLanguagePacks_IPC(t *testing.T) {
	app := newTestApp(t)
	list, err := app.ListLanguagePacks()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) < 2 {
		t.Fatalf("expected catalog, got %d", len(list))
	}
	found := false
	for _, p := range list {
		if p.ID == "en-US" && p.Bundled && p.Installed {
			found = true
		}
	}
	if !found {
		t.Error("en-US bundled row missing")
	}
}

func TestGetDomainPackWords_SoftwareTerms(t *testing.T) {
	app := newTestApp(t)
	if err := app.EnsureDomainPack("software-terms"); err != nil {
		t.Fatal(err)
	}
	words, err := app.GetDomainPackWords("software-terms")
	if err != nil {
		t.Fatal(err)
	}
	if len(words) < 50 {
		t.Errorf("expected curated list, got %d", len(words))
	}
	// Ensure unknown fails loudly.
	if _, err := app.GetDomainPackWords("nope"); err == nil {
		t.Error("expected error for unknown domain")
	}
	_ = spellcheck.DefaultDomainIDs()
}
