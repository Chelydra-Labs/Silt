package main

import (
	"os"
	"path/filepath"
	"reflect"
	"sync"
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

// TestCustomDictionary_AddPreservesConcurrentExternalEdit verifies the add path
// merges a stale in-memory dictionary instead of writing it back over an
// external config.yaml edit.
func TestCustomDictionary_AddPreservesConcurrentExternalEdit(t *testing.T) {
	app := newTestApp(t)
	seedStaleConfigWithExternalFont(t, app, "StaleFont", "ExternalEditFont")

	res, err := app.AddCustomDictionaryWord("TypeScript")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if !reflect.DeepEqual(res, []string{"typescript"}) {
		t.Errorf("add returned %v, want [typescript]", res)
	}
	assertExternalFontSurvived(t, app, "ExternalEditFont")

	loaded, err := config.Load(app.vaultPath)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !reflect.DeepEqual(loaded.Editor.CustomDictionary, []string{"typescript"}) {
		t.Errorf("on-disk dictionary = %v, want [typescript]", loaded.Editor.CustomDictionary)
	}
	got, err := app.GetCustomDictionary()
	if err != nil {
		t.Fatalf("GetCustomDictionary: %v", err)
	}
	if !reflect.DeepEqual(got, []string{"typescript"}) {
		t.Errorf("in-memory dictionary = %v, want [typescript]", got)
	}
}

// TestCustomDictionary_RemovePreservesConcurrentExternalEdit verifies the
// removal path merges a stale in-memory dictionary instead of resurrecting the
// removed word or dropping an external edit.
func TestCustomDictionary_RemovePreservesConcurrentExternalEdit(t *testing.T) {
	app := newTestApp(t)
	if _, err := app.AddCustomDictionaryWord("alpha"); err != nil {
		t.Fatalf("seed alpha: %v", err)
	}
	if _, err := app.AddCustomDictionaryWord("beta"); err != nil {
		t.Fatalf("seed beta: %v", err)
	}
	seedStaleConfigWithExternalFont(t, app, "StaleFont", "ExternalEditFont")

	res, err := app.RemoveCustomDictionaryWord("ALPHA")
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if !reflect.DeepEqual(res, []string{"beta"}) {
		t.Errorf("remove returned %v, want [beta]", res)
	}
	assertExternalFontSurvived(t, app, "ExternalEditFont")

	loaded, err := config.Load(app.vaultPath)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !reflect.DeepEqual(loaded.Editor.CustomDictionary, []string{"beta"}) {
		t.Errorf("on-disk dictionary = %v, want [beta]", loaded.Editor.CustomDictionary)
	}
	got, err := app.GetCustomDictionary()
	if err != nil {
		t.Fatalf("GetCustomDictionary: %v", err)
	}
	if !reflect.DeepEqual(got, []string{"beta"}) {
		t.Errorf("in-memory dictionary = %v, want [beta]", got)
	}
}

// TestCustomDictionary_ImportPreservesConcurrentExternalEdit verifies the import
// path computes its added/skipped summary against the fresh disk state and
// preserves an unrelated external edit.
func TestCustomDictionary_ImportPreservesConcurrentExternalEdit(t *testing.T) {
	app := newTestApp(t)
	if _, err := app.AddCustomDictionaryWord("alpha"); err != nil {
		t.Fatalf("seed alpha: %v", err)
	}
	seedStaleConfigWithExternalFont(t, app, "StaleFont", "ExternalEditFont")

	importPath := filepath.Join(t.TempDir(), "import.txt")
	if err := os.WriteFile(importPath, []byte("alpha\ngamma\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sum, err := app.ImportCustomDictionary(importPath)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if sum.Added != 1 || sum.Skipped != 1 || sum.TotalRead != 2 {
		t.Errorf("summary = %+v, want added=1 skipped=1 total=2", sum)
	}
	assertExternalFontSurvived(t, app, "ExternalEditFont")

	loaded, err := config.Load(app.vaultPath)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !reflect.DeepEqual(loaded.Editor.CustomDictionary, []string{"alpha", "gamma"}) {
		t.Errorf("on-disk dictionary = %v, want [alpha gamma]", loaded.Editor.CustomDictionary)
	}
	got, err := app.GetCustomDictionary()
	if err != nil {
		t.Fatalf("GetCustomDictionary: %v", err)
	}
	if !reflect.DeepEqual(got, []string{"alpha", "gamma"}) {
		t.Errorf("in-memory dictionary = %v, want [alpha gamma]", got)
	}
}

// TestCustomDictionary_ConcurrentWithExternalReload runs dictionary adds
// concurrently with a watcher-style wholesale config replacement (applyConfig),
// confirming the re-read-under-lock path keeps every word and never corrupts
// the on-disk config (#987, AC #4). Run under -race.
func TestCustomDictionary_ConcurrentWithExternalReload(t *testing.T) {
	app := newTestApp(t)

	snapshot := func() config.SystemConfig {
		app.configMu.RLock()
		defer app.configMu.RUnlock()
		return app.cfg
	}

	stop := make(chan struct{})
	var extWg sync.WaitGroup
	extWg.Add(1)
	go func() {
		defer extWg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				app.applyConfig(snapshot())
			}
		}
	}()

	const n = 60
	unique := []string{"alpha", "beta", "gamma"}
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, err := app.AddCustomDictionaryWord(unique[i%len(unique)]); err != nil {
				t.Errorf("AddCustomDictionaryWord: %v", err)
			}
		}(i)
	}
	wg.Wait()
	close(stop)
	extWg.Wait()

	loaded, err := config.Load(app.vaultPath)
	if err != nil {
		t.Fatalf("final config.Load: %v", err)
	}
	got := map[string]int{}
	for _, w := range loaded.Editor.CustomDictionary {
		got[w]++
	}
	want := map[string]int{"alpha": 1, "beta": 1, "gamma": 1}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("dictionary cardinality after storm = %v, want %v", got, want)
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
