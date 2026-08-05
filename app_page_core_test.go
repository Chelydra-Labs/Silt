package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"silt/backend/parser"
	"silt/backend/types"
)

// writeCorePage stages a page with the given frontmatter body (everything
// between the fences) and indexes it through the atomic path so its blocks +
// page_core + projection land in one tx. It also calls MarkFileIndexed so the
// files-table mtime cache is populated (mirrors the production write path
// reindexFileContent, which GetPageCoreMetadata reads for `modified`).
func writeCorePage(t *testing.T, app *App, notebook, section, page, fmBody string) (filePath, content string) {
	t.Helper()
	content = "---\n" + fmBody + "---\n# " + page + "\n\nBody.\n"
	filePath = filepath.Join(app.vaultPath, notebook, section, page+".md")
	writeFile(t, filePath, content)
	blocks, meta, _, _, err := parser.ParseFileContent(content, notebook, section, page, "2026-08-01", app.spacesPerTab)
	if err != nil {
		t.Fatalf("ParseFileContent: %v", err)
	}
	if err := app.indexFile("vault", meta.Notebook, meta.Section, meta.Page, blocks, meta, meta.Warnings...); err != nil {
		t.Fatalf("indexFile: %v", err)
	}
	// Mirror reindexFileContent's MarkFileIndexed step so FileMtime +
	// GetPageCoreMetadata.Modified compose non-zero in the read tests.
	stat, statErr := os.Stat(filePath)
	if statErr != nil {
		t.Fatalf("stat: %v", statErr)
	}
	if err := app.db.MarkFileIndexed(nil, filePath, stat.ModTime().UnixNano(), stat.Size()); err != nil {
		t.Fatalf("MarkFileIndexed: %v", err)
	}
	return filePath, content
}

// ensureMtimeBumps sleeps briefly so a re-write of the same file observes a
// strictly-greater mtime (some filesystems have coarse mtime resolution).
func ensureMtimeBumps(t *testing.T) {
	t.Helper()
	time.Sleep(20 * time.Millisecond)
}

// TestPageCore_RebuildsFromFrontmatter asserts the projection rebuilds from
// frontmatter + mtime on index (#867). The page carries every core field; the
// projected row must mirror them.
func TestPageCore_RebuildsFromFrontmatter(t *testing.T) {
	app := newTestApp(t)
	fmBody := "notebook: \"Notes\"\n" +
		"section: \"\"\n" +
		"page: \"Plan\"\n" +
		"date: \"2026-08-05\"\n" +
		"created: \"2026-08-05T09:30:00\"\n" +
		"tags: [work, planning]\n" +
		"aliases: [\"The Plan\", \"Master Plan\"]\n" +
		"type: \"\"\n"
	filePath, _ := writeCorePage(t, app, "Notes", "", "Plan", fmBody)

	row, err := app.db.GetPageCoreProjection("vault", "Notes", "", "Plan")
	if err != nil {
		t.Fatalf("GetPageCoreProjection: %v", err)
	}
	if row == nil {
		t.Fatal("page_core row missing — every indexed page must get one")
	}
	if row.Type != "" {
		t.Errorf("Type = %q, want empty (untyped)", row.Type)
	}
	if row.Date != "2026-08-05" {
		t.Errorf("Date = %q, want 2026-08-05", row.Date)
	}
	if row.Created != "2026-08-05T09:30:00" {
		t.Errorf("Created = %q, want 2026-08-05T09:30:00", row.Created)
	}
	wantAliases := []string{"The Plan", "Master Plan"}
	if len(row.Aliases) != len(wantAliases) {
		t.Errorf("Aliases = %v, want %v", row.Aliases, wantAliases)
	} else {
		for i := range wantAliases {
			if row.Aliases[i] != wantAliases[i] {
				t.Errorf("Aliases[%d] = %q, want %q", i, row.Aliases[i], wantAliases[i])
			}
		}
	}

	// mtime cache populated by the index path so Modified composes non-empty.
	mt, err := app.db.FileMtime(filePath)
	if err != nil {
		t.Fatalf("FileMtime: %v", err)
	}
	if mt <= 0 {
		t.Errorf("FileMtime = %d, want a positive Unix-nano mtime", mt)
	}
}

// TestPageCore_UntypedPageGetsRowWhileProjectionStaysEmpty pins the central
// #867 invariant: an untyped page gets a page_core row (so the panel can
// render core fields) while page_types/page_properties stay empty (the type
// dashboard does not list it).
func TestPageCore_UntypedPageGetsRowWhileProjectionStaysEmpty(t *testing.T) {
	app := newTestApp(t)
	fmBody := "notebook: \"Notes\"\n" +
		"section: \"\"\n" +
		"page: \"Plain\"\n" +
		"date: \"2026-08-05\"\n" +
		"tags: []\n"
	writeCorePage(t, app, "Notes", "", "Plain", fmBody)

	row, err := app.db.GetPageCoreProjection("vault", "Notes", "", "Plain")
	if err != nil {
		t.Fatalf("GetPageCoreProjection: %v", err)
	}
	if row == nil {
		t.Fatal("untyped page must still get a page_core row")
	}
	if row.Type != "" {
		t.Errorf("Type = %q, want empty for untyped page", row.Type)
	}

	// page_types/page_properties MUST stay empty — the whole point of #867.
	proj, err := app.db.GetPageProjection("vault", "Notes", "", "Plain")
	if err != nil {
		t.Fatalf("GetPageProjection: %v", err)
	}
	if proj != nil {
		t.Errorf("untyped page must not have a page_types row, got %+v", proj)
	}
}

// TestPageCore_TypedPageAlsoGetsRow verifies a typed page gets BOTH a
// page_core row AND its page_types projection — the two projections are
// complementary, not mutually exclusive.
func TestPageCore_TypedPageAlsoGetsRow(t *testing.T) {
	app := newTestApp(t)
	if err := app.SaveType(bookTypeSchema()); err != nil {
		t.Fatalf("SaveType: %v", err)
	}
	fmBody := "notebook: \"Books\"\n" +
		"section: \"\"\n" +
		"page: \"Dune\"\n" +
		"date: \"2026-08-01\"\n" +
		"tags: []\n" +
		"type: \"book\"\n" +
		"title: \"Dune\"\n"
	writeCorePage(t, app, "Books", "", "Dune", fmBody)

	row, err := app.db.GetPageCoreProjection("vault", "Books", "", "Dune")
	if err != nil {
		t.Fatalf("GetPageCoreProjection: %v", err)
	}
	if row == nil {
		t.Fatal("typed page must also get a page_core row")
	}
	if row.Type != "book" {
		t.Errorf("Type = %q, want book", row.Type)
	}
	proj, err := app.db.GetPageProjection("vault", "Books", "", "Dune")
	if err != nil {
		t.Fatalf("GetPageProjection: %v", err)
	}
	if proj == nil || proj.TypeName != "book" {
		t.Errorf("page_types projection = %+v, want type=book", proj)
	}
}

// TestGetPageCoreMetadata_ComposesAllFields asserts the IPC read path
// composes type/date/tags/aliases/created/modified into a single payload.
func TestGetPageCoreMetadata_ComposesAllFields(t *testing.T) {
	app := newTestApp(t)
	fmBody := "notebook: \"Notes\"\n" +
		"section: \"\"\n" +
		"page: \"Plan\"\n" +
		"date: \"2026-08-05\"\n" +
		"created: \"2026-08-05T09:30:00\"\n" +
		"tags: [work]\n" +
		"aliases: [\"The Plan\"]\n"
	writeCorePage(t, app, "Notes", "", "Plan", fmBody)

	meta, err := app.GetPageCoreMetadata("Notes", "", "Plan")
	if err != nil {
		t.Fatalf("GetPageCoreMetadata: %v", err)
	}
	if meta.Date != "2026-08-05" {
		t.Errorf("Date = %q", meta.Date)
	}
	if meta.Created != "2026-08-05T09:30:00" {
		t.Errorf("Created = %q", meta.Created)
	}
	if len(meta.Tags) != 1 || meta.Tags[0] != "work" {
		t.Errorf("Tags = %v, want [work]", meta.Tags)
	}
	if len(meta.Aliases) != 1 || meta.Aliases[0] != "The Plan" {
		t.Errorf("Aliases = %v, want [The Plan]", meta.Aliases)
	}
	if meta.Modified == "" {
		t.Error("Modified = empty, want an RFC3339 timestamp from the files-table mtime")
	}
	// Tags/Aliases MUST be non-nil empty slices (not null) so the frontend's
	// .length reads never NPE.
	meta2, _ := app.GetPageCoreMetadata("Notes", "", "Plan")
	_ = meta2.Tags[0]
	_ = meta2.Aliases[0]
}

// TestSetPageCoreMetadata_WritesRoundTrip verifies each editable field
// round-trips through frontmatter atomically AND triggers reindex (so the
// page_core projection reflects the new value).
func TestSetPageCoreMetadata_WritesRoundTrip(t *testing.T) {
	app := newTestApp(t)
	fmBody := "notebook: \"Notes\"\n" +
		"section: \"\"\n" +
		"page: \"Plan\"\n" +
		"date: \"2026-08-05\"\n" +
		"tags: []\n"
	writeCorePage(t, app, "Notes", "", "Plan", fmBody)

	// Date edit.
	newDate := "2026-09-01"
	if err := app.SetPageCoreMetadata("Notes", "", "Plan", CoreFieldUpdate{Date: &newDate}); err != nil {
		t.Fatalf("SetPageCoreMetadata(date): %v", err)
	}
	row, _ := app.db.GetPageCoreProjection("vault", "Notes", "", "Plan")
	if row.Date != "2026-09-01" {
		t.Errorf("after date edit: projection Date = %q, want 2026-09-01", row.Date)
	}

	// Aliases edit (string array).
	newAliases := []string{"Alias One", "Alias Two"}
	if err := app.SetPageCoreMetadata("Notes", "", "Plan", CoreFieldUpdate{Aliases: &newAliases}); err != nil {
		t.Fatalf("SetPageCoreMetadata(aliases): %v", err)
	}
	row, _ = app.db.GetPageCoreProjection("vault", "Notes", "", "Plan")
	if len(row.Aliases) != 2 || row.Aliases[0] != "Alias One" || row.Aliases[1] != "Alias Two" {
		t.Errorf("after aliases edit: projection Aliases = %v, want [Alias One, Alias Two]", row.Aliases)
	}

	// Created edit.
	newCreated := "2026-10-02T08:00:00"
	if err := app.SetPageCoreMetadata("Notes", "", "Plan", CoreFieldUpdate{Created: &newCreated}); err != nil {
		t.Fatalf("SetPageCoreMetadata(created): %v", err)
	}
	row, _ = app.db.GetPageCoreProjection("vault", "Notes", "", "Plan")
	if row.Created != "2026-10-02T08:00:00" {
		t.Errorf("after created edit: projection Created = %q, want 2026-10-02T08:00:00", row.Created)
	}

	// Tags edit.
	newTags := []string{"work", "priority"}
	if err := app.SetPageCoreMetadata("Notes", "", "Plan", CoreFieldUpdate{Tags: &newTags}); err != nil {
		t.Fatalf("SetPageCoreMetadata(tags): %v", err)
	}
	got, _ := app.GetPageCoreMetadata("Notes", "", "Plan")
	if len(got.Tags) != 2 || got.Tags[0] != "work" || got.Tags[1] != "priority" {
		t.Errorf("after tags edit: Tags = %v, want [work, priority]", got.Tags)
	}

	// On-disk frontmatter must carry the new keys (atomic write survived).
	content, _ := readFileForTest(t, app, "Notes", "", "Plan")
	checks := map[string]bool{
		"date: \"2026-09-01\"":             true,
		"aliases: [\"Alias One\"":          true,
		"created: \"2026-10-02T08:00:00\"": true,
		"tags: [\"work\", \"priority\"]":   true,
	}
	for want := range checks {
		if !strings.Contains(content, want) {
			t.Errorf("on-disk frontmatter missing %q. Content:\n%s", want, content)
		}
	}
}

// TestSetPageCoreMetadata_NoOpEmptyUpdate verifies an update with every field
// nil is a no-op (no reindex, no file touch).
func TestSetPageCoreMetadata_NoOpEmptyUpdate(t *testing.T) {
	app := newTestApp(t)
	fmBody := "notebook: \"Notes\"\n" +
		"section: \"\"\n" +
		"page: \"Plan\"\n" +
		"date: \"2026-08-05\"\n" +
		"tags: []\n"
	filePath, _ := writeCorePage(t, app, "Notes", "", "Plan", fmBody)

	before, _ := readFileForTest(t, app, "Notes", "", "Plan")
	if err := app.SetPageCoreMetadata("Notes", "", "Plan", CoreFieldUpdate{}); err != nil {
		t.Fatalf("SetPageCoreMetadata(empty): %v", err)
	}
	after, _ := readFileForTest(t, app, "Notes", "", "Plan")
	if before != after {
		t.Errorf("empty update touched the file at %s", filePath)
	}
}

// TestSetPageCoreMetadata_ClearsViaEmptyValues verifies an empty string / nil
// slice clears the corresponding frontmatter key.
func TestSetPageCoreMetadata_ClearsViaEmptyValues(t *testing.T) {
	app := newTestApp(t)
	fmBody := "notebook: \"Notes\"\n" +
		"section: \"\"\n" +
		"page: \"Plan\"\n" +
		"date: \"2026-08-05\"\n" +
		"created: \"2026-08-05T09:30:00\"\n" +
		"tags: [work]\n" +
		"aliases: [\"The Plan\"]\n"
	writeCorePage(t, app, "Notes", "", "Plan", fmBody)

	emptyStr := ""
	emptySlice := []string{}
	if err := app.SetPageCoreMetadata("Notes", "", "Plan", CoreFieldUpdate{
		Date:    &emptyStr,
		Created: &emptyStr,
		Tags:    &emptySlice,
		Aliases: &emptySlice,
	}); err != nil {
		t.Fatalf("SetPageCoreMetadata(clear): %v", err)
	}
	content, _ := readFileForTest(t, app, "Notes", "", "Plan")
	for _, key := range []string{"date:", "created:", "tags:", "aliases:"} {
		if strings.Contains(content, "\n"+key) {
			t.Errorf("clearing did not remove %q. Content:\n%s", key, content)
		}
	}
}

// TestReservedPropertyNames_IncludesCoreFields pins the reserved-key guard
// rejects aliases/created as type-property names so a schema cannot collide
// with #867's core fields.
func TestReservedPropertyNames_IncludesCoreFields(t *testing.T) {
	for _, name := range []string{"aliases", "created"} {
		td := bookTypeSchema()
		td.Properties = append(td.Properties, types.PropertyDef{Name: name, Type: types.PropText})
		if err := types.ValidateTypeDef(&td); err == nil {
			t.Errorf("reserved name %q should be rejected as a property name", name)
		}
	}
}

// TestComputeBatchProjections_DateAndCreatedPopulateCore covers the cold-start
// / linked-tree batch ingest path: computeBatchProjections synthesizes a
// FileMetadata per ScanResult, and the Core projection must carry the scanner-
// resolved date (res.Date) and recover `created` from the raw frontmatter.
// Previously the synthesized meta omitted Date and there was no created
// fallback, so every batch-indexed page got an empty-core row on the primary
// ingest path (review finding P1 #2).
func TestComputeBatchProjections_DateAndCreatedPopulateCore(t *testing.T) {
	app := newTestApp(t)
	results := []parser.ScanResult{{
		Notebook: "Work",
		Section:  "",
		Page:     "Plan",
		Date:     "2026-07-15",
		Frontmatter: map[string]any{
			"created": "2026-01-01T00:00:00",
			"aliases": []any{"foo", "bar"},
		},
	}}
	out := computeBatchProjections(app, results)
	if len(out) != 1 {
		t.Fatalf("expected 1 projection, got %d", len(out))
	}
	core := out[0].Core
	if core.Date != "2026-07-15" {
		t.Errorf("Core.Date not populated from res.Date: got %q", core.Date)
	}
	if core.Created != "2026-01-01T00:00:00" {
		t.Errorf("Core.Created not recovered from frontmatter: got %q", core.Created)
	}
	if len(core.Aliases) != 2 || core.Aliases[0] != "foo" {
		t.Errorf("Core.Aliases not populated: got %v", core.Aliases)
	}
}

// TestComputePageCoreFromMeta_ScalarAliases confirms a hand-authored scalar
// `aliases: foo` (not a list) is tolerated as a one-element list rather than
// silently dropped from the projection — interop with Obsidian / hand-edited
// frontmatter. Without the fallback, the typed []string decode fails, the
// projection loses the value, and a subsequent panel save would clear it.
func TestComputePageCoreFromMeta_ScalarAliases(t *testing.T) {
	meta := parser.FileMetadata{
		Notebook:    "Work",
		Section:     "",
		Page:        "Plan",
		Frontmatter: map[string]any{"aliases": "foo"},
	}
	core := computePageCoreFromMeta(meta)
	if len(core.Aliases) != 1 || core.Aliases[0] != "foo" {
		t.Errorf("scalar aliases not tolerated: got %v, want [foo]", core.Aliases)
	}

	// An empty scalar string is NOT promoted to a one-element list (it would
	// store [""] rather than clearing the field).
	core = computePageCoreFromMeta(parser.FileMetadata{
		Frontmatter: map[string]any{"aliases": ""},
	})
	if len(core.Aliases) != 0 {
		t.Errorf("empty scalar aliases should be dropped: got %v", core.Aliases)
	}
}

// readFileForTest reads a page file from the vault for assertion.
func readFileForTest(t *testing.T, app *App, notebook, section, page string) (string, error) {
	t.Helper()
	path := filepath.Join(app.vaultPath, notebook, section, page+".md")
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
