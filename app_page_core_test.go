package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"silt/backend/db"
	"silt/backend/parser"
	"silt/backend/types"
	"silt/backend/vault"
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

// TestGetPageCoreMetadata_ModifiedRefreshedAfterWrite verifies the read-only
// `modified` reflects an in-app write, not just the startup-scan mtime. Every
// write path funnels through indexFile, which now refreshes the files-table
// row; GetPageCoreMetadata reads that mtime. Without the indexFile
// MarkFileIndexed call, modified would stay empty (no files row) or hold the
// pre-write mtime all session because the watcher ignores self-writes.
func TestGetPageCoreMetadata_ModifiedRefreshedAfterWrite(t *testing.T) {
	app := newTestApp(t)
	writeCorePage(t, app, "Notes", "", "Plan",
		"notebook: \"Notes\"\npage: \"Plan\"\ndate: \"2026-08-05\"\n")

	newDate := "2026-09-09"
	if err := app.SetPageCoreMetadata("Notes", "", "Plan", CoreFieldUpdate{Date: &newDate}); err != nil {
		t.Fatalf("SetPageCoreMetadata: %v", err)
	}
	got, err := app.GetPageCoreMetadata("Notes", "", "Plan")
	if err != nil {
		t.Fatalf("GetPageCoreMetadata: %v", err)
	}
	if got.Modified == "" {
		t.Fatal("Modified empty after in-app core write; indexFile should refresh the files-table mtime so the Core panel stays current without an external-triggered scan")
	}
	// Sanity: it's the RFC3339 timestamp GetPageCoreMetadata emits (not garbage).
	if _, perr := time.Parse(time.RFC3339, got.Modified); perr != nil {
		t.Errorf("Modified not a parseable RFC3339 timestamp: got %q (%v)", got.Modified, perr)
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

// TestComputeBatchProjections_LinkedSourceDateRecoveredFromFrontmatter pins
// the linked-notebook batch ingest path: LinkNotebook builds ScanResults from
// parser.FileMetadata, and page_core.date must populate even when the
// ScanResult.Date is empty. The frontmatter fallback in
// computePageCoreFromMeta recovers the date from raw frontmatter, and a
// hand-built ScanResult with NO Date set proves that path works for linked
// notebooks (the bug shape: the linked ingest previously omitted Date, so
// every linked page projected an empty core.date).
func TestComputeBatchProjections_LinkedSourceDateRecoveredFromFrontmatter(t *testing.T) {
	app := newTestApp(t)
	// Date is intentionally NOT set on the ScanResult — the frontmatter
	// fallback must recover it (this is the linked-notebook bug shape).
	results := []parser.ScanResult{{
		Notebook: "External",
		Section:  "Notes",
		Page:     "Imported",
		Source:   "linked:test",
		Frontmatter: map[string]any{
			"date":    "2026-03-12",
			"created": "2026-03-12T09:00:00",
		},
	}}
	out := computeBatchProjections(app, results)
	if len(out) != 1 {
		t.Fatalf("expected 1 projection, got %d", len(out))
	}
	core := out[0].Core
	if core.Date != "2026-03-12" {
		t.Errorf("linked-source core.Date not recovered from frontmatter: got %q, want 2026-03-12", core.Date)
	}
	if core.Created != "2026-03-12T09:00:00" {
		t.Errorf("linked-source core.Created not recovered from frontmatter: got %q", core.Created)
	}
}

// TestComputePageCoreFromMeta_DateFallback pins the date fallback in
// computePageCoreFromMeta directly: a FileMetadata with no Date but a
// frontmatter date (in either string or time.Time shape) must recover it.
// The unquoted-time.Time shape is what yaml.v3 produces for `date: 2026-08-05`.
func TestComputePageCoreFromMeta_DateFallback(t *testing.T) {
	// String date (quoted frontmatter form)
	core := computePageCoreFromMeta(parser.FileMetadata{
		Frontmatter: map[string]any{"date": "2026-07-15"},
	})
	if core.Date != "2026-07-15" {
		t.Errorf("string date not recovered: got %q", core.Date)
	}
	// time.Time date (unquoted frontmatter form — what yaml.v3 resolves to)
	core = computePageCoreFromMeta(parser.FileMetadata{
		Frontmatter: map[string]any{"date": time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC)},
	})
	if core.Date != "2026-08-05" {
		t.Errorf("time.Time date not recovered: got %q", core.Date)
	}
	// Pre-set Date takes precedence over frontmatter (no overwrite).
	core = computePageCoreFromMeta(parser.FileMetadata{
		Date:        "2026-01-01",
		Frontmatter: map[string]any{"date": "2026-07-15"},
	})
	if core.Date != "2026-01-01" {
		t.Errorf("pre-set Date should take precedence: got %q", core.Date)
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

// TestBlockOnlyWrite_RefreshesCoreModified pins the block-only write mtime
// refresh added in commit 61fa4861: a task-status edit routes through
// IndexFileBlocks (NOT indexFile) so the unified atomic path's MarkFileIndexed
// never runs. Without markFileIndexedBestEffort at the end of the block-only
// write chain, the files-table mtime cache stays at the pre-write value all
// session (the fsnotify watcher ignores self-writes) and GetPageCoreMetadata
// surfaces a stale/empty `modified`.
//
// This test seeds a typed page with a task, captures the seed files-table
// mtime, runs SetTaskOwner (a canonical block-only write), and asserts the
// cache was refreshed: db.FileMtime returns a strictly-greater value AND
// GetPageCoreMetadata.Modified is non-empty + RFC3339-shaped.
func TestBlockOnlyWrite_RefreshesCoreModified(t *testing.T) {
	app := newTestApp(t)
	const taskID = "aaaaaaaa-1111-1111-1111-111111111111"

	// Seed a typed page with one task through the unified atomic path so the
	// files-table mtime row is populated. The body carries one task block so a
	// SetTaskOwner edit has a target.
	content := "---\n" +
		"notebook: \"Notes\"\n" +
		"section: \"\"\n" +
		"page: \"Chores\"\n" +
		"date: \"2026-08-05\"\n" +
		"tags: []\n" +
		"---\n# Chores\n\n" +
		"- [ ] walk the dog <!-- id: " + taskID + " -->\n"
	filePath := filepath.Join(app.vaultPath, "Notes", "Chores.md")
	writeFile(t, filePath, content)
	blocks, meta, _, _, err := parser.ParseFileContent(content, "Notes", "", "Chores", "2026-08-05", app.spacesPerTab)
	if err != nil {
		t.Fatalf("ParseFileContent: %v", err)
	}
	if err := app.indexFile("vault", "Notes", "", "Chores", blocks, meta, meta.Warnings...); err != nil {
		t.Fatalf("indexFile: %v", err)
	}
	stat, statErr := os.Stat(filePath)
	if statErr != nil {
		t.Fatalf("stat seed: %v", statErr)
	}
	if err := app.db.MarkFileIndexed(nil, filePath, stat.ModTime().UnixNano(), stat.Size()); err != nil {
		t.Fatalf("seed MarkFileIndexed: %v", err)
	}

	seedMtime, err := app.db.FileMtime(filePath)
	if err != nil {
		t.Fatalf("seed FileMtime: %v", err)
	}
	if seedMtime <= 0 {
		t.Fatalf("seed FileMtime = %d, want positive (indexFile + MarkFileIndexed should populate it)", seedMtime)
	}

	// Some filesystems have coarse mtime resolution; bump the clock so the
	// block-only write produces a strictly-greater mtime than the seed.
	ensureMtimeBumps(t)

	// Block-only write: SetTaskOwner routes through mutateTaskBlock →
	// IndexFileBlocks + markFileIndexedBestEffort. IndexFileBlocks does NOT
	// touch the files-table; only the best-effort marker call does.
	if err := app.SetTaskOwner(taskID, "Alice"); err != nil {
		t.Fatalf("SetTaskOwner: %v", err)
	}

	postMtime, err := app.db.FileMtime(filePath)
	if err != nil {
		t.Fatalf("post FileMtime: %v", err)
	}
	if postMtime <= seedMtime {
		t.Errorf("block-only write did not refresh files-table mtime: seed=%d post=%d (markFileIndexedBestEffort regression)", seedMtime, postMtime)
	}

	got, err := app.GetPageCoreMetadata("Notes", "", "Chores")
	if err != nil {
		t.Fatalf("GetPageCoreMetadata after block-only write: %v", err)
	}
	if got.Modified == "" {
		t.Fatal("Modified empty after block-only write; markFileIndexedBestEffort should refresh the cache so the Core panel reads the new mtime")
	}
	if _, perr := time.Parse(time.RFC3339, got.Modified); perr != nil {
		t.Errorf("Modified not RFC3339 after block-only write: got %q (%v)", got.Modified, perr)
	}
}

// TestMarkFileIndexedBestEffort_UsesPreCommitStatNotPostCommitStat pins the
// fix for PR #898 review finding #2: markFileIndexedBestEffort must record the
// mtime/size snapshot its caller captured BEFORE IndexFileBlocks commits, NOT
// re-stat the file after the index commit. A post-commit re-stat would
// reintroduce the [index-commit, stat] window dcd2a6cd closed for indexFile:
// an external edit (Obsidian/Dropbox/second Silt window) landing between the
// app's write and that late stat would get its mtime recorded against the
// pre-edit indexed content, and a warm restart's IsFileUnchanged would match
// and silently persist the stale content.
//
// This simulates the window directly: capture the pre-commit FileInfo, then
// bump the on-disk mtime to a strictly-later value (the "external edit"), then
// hand the marker the pre-commit snapshot. A correct marker records the
// pre-commit mtime; a marker that re-stats post-commit would record the bumped
// (external) mtime and fail.
func TestMarkFileIndexedBestEffort_UsesPreCommitStatNotPostCommitStat(t *testing.T) {
	app := newTestApp(t)

	// Stage a file + seed its files-table row so the marker has a path to update
	// (MarkFileIndexed is an upsert, but seeding keeps the test honest about the
	// "refresh an existing row" production path).
	filePath := filepath.Join(app.vaultPath, "Notes", "Window.md")
	writeFile(t, filePath, "---\nnotebook: \"Notes\"\npage: \"Window\"\ntags: []\n---\n# Window\n")
	if err := app.db.MarkFileIndexed(nil, filePath, 1, 1); err != nil {
		t.Fatalf("seed MarkFileIndexed: %v", err)
	}

	// Pre-commit snapshot — exactly what the call sites hand the marker (taken
	// right after WriteFileAtomic, before IndexFileBlocks commits).
	preStat, err := os.Stat(filePath)
	if err != nil {
		t.Fatalf("pre-commit stat: %v", err)
	}
	preMtime := preStat.ModTime().UnixNano()

	// Simulate a concurrent external edit landing in the [index-commit, stat]
	// window by moving the on-disk mtime strictly later than the snapshot.
	future := preStat.ModTime().Add(2 * time.Hour)
	if err := os.Chtimes(filePath, future, future); err != nil {
		t.Fatalf("os.Chtimes (simulate external edit): %v", err)
	}
	diskMtimeAfterEdit, err := os.Stat(filePath)
	if err != nil {
		t.Fatalf("post-edit stat: %v", err)
	}
	if diskMtimeAfterEdit.ModTime().UnixNano() == preMtime {
		t.Fatalf("test pre-condition failed: os.Chtimes did not advance the on-disk mtime; cannot discriminate pre- vs post-commit stat")
	}

	// The marker must use the caller's snapshot — the on-disk mtime is now
	// `future`, but a correct marker records preMtime because it never re-stats.
	app.markFileIndexedBestEffort(filePath, preStat)

	got, err := app.db.FileMtime(filePath)
	if err != nil {
		t.Fatalf("FileMtime after mark: %v", err)
	}
	if got != preMtime {
		t.Errorf("files-row mtime = %d, want pre-commit snapshot %d (marker must use the caller's stat, not re-stat post-commit — a late stat would record the external edit's %d against pre-edit indexed content)",
			got, preMtime, diskMtimeAfterEdit.ModTime().UnixNano())
	}
}

// TestMarkFileIndexedBestEffort_NilStatIsNoOp pins the best-effort contract:
// when the caller could not stat the file (WriteFileAtomic's stat failed), the
// marker must not touch the files row rather than panic on a nil FileInfo.
func TestMarkFileIndexedBestEffort_NilStatIsNoOp(t *testing.T) {
	app := newTestApp(t)
	filePath := filepath.Join(app.vaultPath, "Notes", "Nil.md")
	writeFile(t, filePath, "---\nnotebook: \"Notes\"\npage: \"Nil\"\ntags: []\n---\n# Nil\n")
	if err := app.db.MarkFileIndexed(nil, filePath, 123456, 7); err != nil {
		t.Fatalf("seed MarkFileIndexed: %v", err)
	}

	app.markFileIndexedBestEffort(filePath, nil) // must not panic, must not clobber

	got, err := app.db.FileMtime(filePath)
	if err != nil {
		t.Fatalf("FileMtime: %v", err)
	}
	if got != 123456 {
		t.Errorf("nil stat clobbered the files row: mtime = %d, want 123456 (nil stat must be a no-op)", got)
	}
}

// TestWarmUpgrade_PageCoreBackfillRunsEvenWhenProjectionMarkerSet pins the
// dual-marker contract (#867): a vault that already shipped typed notes
// (PageProjectionBackfillMarker recorded by a prior version) but has NOT yet
// run the page_core backfill (PageCoreBackfillMarker absent) must STILL project
// page_core rows for warm-skipped pages on the next open. Gating core backfill
// on the projection marker would skip it for exactly these vaults — every
// 0.4.x typed-notes vault upgrading to the page_core version.
//
// This test drives the full initializeVaultServices path: seed a typed page,
// confirm both markers land on first open, then simulate the warm-upgrade
// state (files-table intact + projection marker set + core marker wiped +
// page_core table wiped) and reopen. The page_core row must come back AND the
// core backfill marker must be recorded on success.
func TestWarmUpgrade_PageCoreBackfillRunsEvenWhenProjectionMarkerSet(t *testing.T) {
	t.Setenv("SILT_DATA_DIR", t.TempDir())
	hostConfigDir := t.TempDir()
	t.Setenv("APPDATA", hostConfigDir)
	t.Setenv("XDG_CONFIG_HOME", hostConfigDir)

	vaultPath := t.TempDir()
	if err := vault.ScaffoldVault(vaultPath); err != nil {
		t.Fatalf("ScaffoldVault: %v", err)
	}
	bookPath := filepath.Join(vaultPath, "Books", "Dune.md")
	writeFile(t, bookPath, "---\n"+
		"notebook: \"Books\"\n"+
		"section: \"\"\n"+
		"page: \"Dune\"\n"+
		"date: \"2026-08-01\"\n"+
		"tags: []\n"+
		"type: \"book\"\n"+
		"title: \"Dune\"\n"+
		"---\n# Dune\n\nBody.\n")

	// First open: indexes the page and records BOTH backfill markers.
	app := &App{spacesPerTab: 4}
	if err := app.initializeVaultServices(vaultPath); err != nil {
		t.Fatalf("first initializeVaultServices: %v", err)
	}
	if err := app.SaveType(bookTypeSchema()); err != nil {
		t.Fatalf("SaveType: %v", err)
	}
	// Force a re-projection so the book page is typed-projected + page_core
	// is populated under the running version. The exact trigger isn't
	// load-bearing here — only that the row exists after first open.
	if row, _ := app.db.GetPageCoreProjection("vault", "Books", "", "Dune"); row == nil {
		t.Fatalf("first open: page_core row missing for Dune — every indexed page must get one")
	}
	projDone, _ := app.db.SchemaMigrationApplied(db.PageProjectionBackfillMarker)
	coreDone, _ := app.db.SchemaMigrationApplied(db.PageCoreBackfillMarker)
	if err := app.CloseVault(); err != nil {
		t.Fatalf("CloseVault: %v", err)
	}

	// Simulate the warm-upgrade window: the projection marker (set by the
	// prior typed-notes version) is intact, but the page_core marker + rows
	// are absent (the upgraded version has not yet backfilled). The files
	// table stays intact so the next open warm-skips Dune.md.
	app2 := &App{spacesPerTab: 4}
	if err := app2.initializeVaultServices(vaultPath); err != nil {
		t.Fatalf("reopen to stage warm-upgrade state: %v", err)
	}
	// Stage: projection marker preserved (mimics prior version's ledger),
	// core marker + page_core rows wiped (mimics pre-page_core upgrade).
	if !projDone {
		// First open should have set the projection marker. If something
		// upstream changed, the rest of the test cannot stage the dual-marker
		// state it is meant to assert.
		if err := app2.db.RecordSchemaMigration(db.PageProjectionBackfillMarker); err != nil {
			t.Fatalf("stage RecordSchemaMigration: %v", err)
		}
	}
	if coreDone {
		if _, err := app2.db.SQLDB().Exec(
			"DELETE FROM schema_migrations WHERE name = ?", db.PageCoreBackfillMarker,
		); err != nil {
			t.Fatalf("wipe core marker: %v", err)
		}
	}
	if _, err := app2.db.SQLDB().Exec("DELETE FROM page_core"); err != nil {
		t.Fatalf("wipe page_core rows: %v", err)
	}
	// Sanity: the dual-marker state we wanted is now in place.
	projSet, _ := app2.db.SchemaMigrationApplied(db.PageProjectionBackfillMarker)
	coreSet, _ := app2.db.SchemaMigrationApplied(db.PageCoreBackfillMarker)
	if !projSet || coreSet {
		t.Fatalf("stage mismatch: projection=%v core=%v, want projection=true core=false", projSet, coreSet)
	}
	if err := app2.CloseVault(); err != nil {
		t.Fatalf("CloseVault after staging: %v", err)
	}

	// Warm upgrade reopen: files table unchanged → Dune.md warm-skipped. With
	// the projection marker set, the projection backfill loop is a no-op for
	// Dune. The page_core backfill MUST still run (its own marker is missing)
	// and record its marker on success — the dual-marker invariant.
	app3 := &App{spacesPerTab: 4}
	if err := app3.initializeVaultServices(vaultPath); err != nil {
		t.Fatalf("warm-upgrade initializeVaultServices: %v", err)
	}
	defer func() { _ = app3.CloseVault() }()

	// (a) page_core rows ARE produced for the warm-skipped page.
	row, err := app3.db.GetPageCoreProjection("vault", "Books", "", "Dune")
	if err != nil {
		t.Fatalf("warm-upgrade GetPageCoreProjection: %v", err)
	}
	if row == nil {
		t.Fatal("warm-upgrade: page_core row missing for Dune — the dual-marker gate skipped the core backfill even though PageCoreBackfillMarker was unset")
	}
	if row.Type != "book" {
		t.Errorf("warm-upgrade page_core Type = %q, want book", row.Type)
	}
	if row.Date != "2026-08-01" {
		t.Errorf("warm-upgrade page_core Date = %q, want 2026-08-01", row.Date)
	}

	// (b) PageCoreBackfillMarker gets recorded on success — proves the
	// dual-marker gate did not piggy-back on the projection marker.
	coreRecorded, err := app3.db.SchemaMigrationApplied(db.PageCoreBackfillMarker)
	if err != nil {
		t.Fatalf("post-upgrade SchemaMigrationApplied(core): %v", err)
	}
	if !coreRecorded {
		t.Fatal("PageCoreBackfillMarker not recorded after warm-upgrade backfill — it would re-run on every open until recorded, defeating the one-shot contract")
	}
}
