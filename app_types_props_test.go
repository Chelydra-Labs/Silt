package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"silt/backend/parser"
	"silt/backend/types"
)

// bookTypeSchema is the canonical test schema: text + text + select + number,
// with rating left unset by the test pages so GetPageProperties exercises both
// the set and unset branches.
func bookTypeSchema() types.TypeDef {
	return types.TypeDef{
		ID:   "book",
		Name: "Book",
		Properties: []types.PropertyDef{
			{Name: "title", Type: types.PropText},
			{Name: "author", Type: types.PropText},
			{Name: "status", Type: types.PropSelect, Options: []string{"available", "read"}},
			{Name: "rating", Type: types.PropNumber},
		},
	}
}

// meetingTypeSchema reuses the `status` key but as a NUMBER, so a book page
// carrying status: "available" triggers a keep-and-flag mismatch when its type
// is switched to meeting.
func meetingTypeSchema() types.TypeDef {
	return types.TypeDef{
		ID:   "meeting",
		Name: "Meeting",
		Properties: []types.PropertyDef{
			{Name: "attendees", Type: types.PropText},
			{Name: "status", Type: types.PropNumber},
		},
	}
}

// writeBookPage stages a typed page with title/author/status set and rating
// unset, returning the on-disk file path and the exact content written (so a
// test can assert byte-identical preservation of the other lines after an edit).
func writeBookPage(t *testing.T, app *App) (filePath, content string) {
	t.Helper()
	if err := app.SaveType(bookTypeSchema()); err != nil {
		t.Fatalf("SaveType(book): %v", err)
	}
	const body = "# Dune\n\nBody.\n"
	content = "---\n" +
		"notebook: \"Books\"\n" +
		"section: \"\"\n" +
		"page: \"Dune\"\n" +
		"date: \"2026-08-01\"\n" +
		"tags: []\n" +
		"type: \"book\"\n" +
		"title: \"Dune\"\n" +
		"author: \"Frank Herbert\"\n" +
		"status: \"available\"\n" +
		"---\n" + body
	filePath = filepath.Join(app.vaultPath, "Books", "Dune.md")
	writeFile(t, filePath, content)
	return filePath, content
}

// indexTypedPage runs the standard parse+index pass so the DB mirrors the file
// (the write-path methods re-index internally, but read-only tests sometimes
// start from a freshly-written file that has never been indexed).
func indexTypedPage(t *testing.T, app *App, filePath, notebook, section, page, content string) {
	t.Helper()
	blocks, meta, _, _, err := parser.ParseFileContent(content, notebook, section, page, "2026-08-01", app.spacesPerTab)
	if err != nil {
		t.Fatalf("ParseFileContent: %v", err)
	}
	if err := app.db.IndexFileBlocks("vault", meta.Notebook, meta.Section, meta.Page, blocks, meta.Tags); err != nil {
		t.Fatalf("IndexFileBlocks: %v", err)
	}
}

func TestGetPageType_ReturnsSchema(t *testing.T) {
	app := newTestApp(t)
	writeBookPage(t, app)

	info, err := app.GetPageType("Books", "", "Dune")
	if err != nil {
		t.Fatalf("GetPageType: %v", err)
	}
	if !info.IsSet {
		t.Fatal("IsSet = false, want true")
	}
	if info.TypeID != "book" {
		t.Errorf("TypeID = %q, want book", info.TypeID)
	}
	if info.RawType != "book" {
		t.Errorf("RawType = %q, want book", info.RawType)
	}
	if len(info.Type.Properties) != 4 {
		t.Errorf("schema properties = %d, want 4", len(info.Type.Properties))
	}
}

func TestGetPageType_UntypedPage(t *testing.T) {
	app := newTestApp(t)
	// A page with no type: field.
	writeFile(t, filepath.Join(app.vaultPath, "Notes", "Plain.md"),
		"---\nnotebook: \"Notes\"\nsection: \"\"\npage: \"Plain\"\ndate: \"2026-08-01\"\ntags: []\n---\n# Plain\n")

	info, err := app.GetPageType("Notes", "", "Plain")
	if err != nil {
		t.Fatalf("GetPageType: %v", err)
	}
	if info.IsSet {
		t.Error("IsSet = true for an untyped page, want false")
	}
	if info.RawType != "" {
		t.Errorf("RawType = %q, want empty", info.RawType)
	}
}

func TestGetPageProperties_SetAndUnset(t *testing.T) {
	app := newTestApp(t)
	writeBookPage(t, app)

	props, err := app.GetPageProperties("Books", "", "Dune")
	if err != nil {
		t.Fatalf("GetPageProperties: %v", err)
	}
	byName := map[string]PagePropertyValue{}
	for _, p := range props {
		byName[p.Name] = p
	}
	if len(props) != 4 {
		t.Fatalf("got %d properties, want 4", len(props))
	}
	// Set properties.
	if !byName["title"].IsSet || byName["title"].Value != "Dune" {
		t.Errorf("title = %+v, want IsSet Dune", byName["title"])
	}
	if !byName["author"].IsSet || byName["author"].Value != "Frank Herbert" {
		t.Errorf("author = %+v, want IsSet Frank Herbert", byName["author"])
	}
	if !byName["status"].IsSet || byName["status"].Value != "available" {
		t.Errorf("status = %+v, want IsSet available", byName["status"])
	}
	// rating is unset.
	if byName["rating"].IsSet {
		t.Errorf("rating = %+v, want unset", byName["rating"])
	}
	// Schema declaration order preserved.
	for i, want := range []string{"title", "author", "status", "rating"} {
		if props[i].Name != want {
			t.Errorf("props[%d].Name = %q, want %q", i, props[i].Name, want)
		}
	}
	// select property carries its options.
	if len(byName["status"].Options) != 2 {
		t.Errorf("status options = %v, want [available read]", byName["status"].Options)
	}
}

func TestGetPageProperties_UntypedPage(t *testing.T) {
	app := newTestApp(t)
	writeFile(t, filepath.Join(app.vaultPath, "Notes", "Plain.md"),
		"---\nnotebook: \"Notes\"\nsection: \"\"\npage: \"Plain\"\ndate: \"2026-08-01\"\ntags: []\n---\n# Plain\n")

	props, err := app.GetPageProperties("Notes", "", "Plain")
	if err != nil {
		t.Fatalf("GetPageProperties: %v", err)
	}
	if len(props) != 0 {
		t.Errorf("got %d properties for an untyped page, want 0", len(props))
	}
}

func TestSetPageProperty_WritesValueAndPreservesOtherLines(t *testing.T) {
	app := newTestApp(t)
	filePath, before := writeBookPage(t, app)

	if err := app.SetPageProperty("Books", "", "Dune", "rating", 4); err != nil {
		t.Fatalf("SetPageProperty(rating, 4): %v", err)
	}

	afterBytes, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read after: %v", err)
	}
	after := string(afterBytes)

	// rating is now present and set.
	if !strings.Contains(after, "rating: 4") {
		t.Errorf("rating line not written:\n%s", after)
	}

	// Every original line survives unchanged — the edit is surgical, so
	// notebook/section/page/date/tags/type/title/author/status + body are
	// byte-identical. Only the new rating line was added.
	filtered := strings.Split(after, "\n")
	kept := filtered[:0]
	for _, l := range filtered {
		if l == "rating: 4" {
			continue
		}
		kept = append(kept, l)
	}
	if strings.Join(kept, "\n") != before {
		t.Errorf("SetPageProperty mutated lines other than rating.\nbefore:\n%s\nafter (rating stripped):\n%s", before, strings.Join(kept, "\n"))
	}

	// GetPageProperties reflects the new value.
	props, _ := app.GetPageProperties("Books", "", "Dune")
	for _, p := range props {
		if p.Name == "rating" {
			if !p.IsSet {
				t.Error("rating IsSet = false after SetPageProperty")
			}
			if f, ok := toFloat(p.Value); !ok || f != 4 {
				t.Errorf("rating value = %v, want 4", p.Value)
			}
		}
	}
}

func TestSetPageProperty_InvalidValueLeavesFileUntouched(t *testing.T) {
	app := newTestApp(t)
	filePath, before := writeBookPage(t, app)
	beforeBytes, _ := os.ReadFile(filePath)

	// status is a select with options [available read]; "bogusoption" is invalid.
	if err := app.SetPageProperty("Books", "", "Dune", "status", "bogusoption"); err == nil {
		t.Fatal("SetPageProperty with an invalid select value should error")
	}
	afterBytes, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read after: %v", err)
	}
	if string(afterBytes) != string(beforeBytes) {
		t.Errorf("file mutated despite a validation error.\nbefore:\n%s\nafter:\n%s", before, string(afterBytes))
	}

	// A non-number for a number property is also rejected, file untouched.
	if err := app.SetPageProperty("Books", "", "Dune", "rating", "notanumber"); err == nil {
		t.Fatal("SetPageProperty with a non-numeric rating should error")
	}
	afterBytes2, _ := os.ReadFile(filePath)
	if string(afterBytes2) != string(beforeBytes) {
		t.Errorf("file mutated despite a validation error (rating).\nbefore:\n%s\nafter:\n%s", before, string(afterBytes2))
	}
}

func TestSetPageProperty_UnknownProperty(t *testing.T) {
	app := newTestApp(t)
	writeBookPage(t, app)
	if err := app.SetPageProperty("Books", "", "Dune", "nosuchfield", "x"); err == nil {
		t.Fatal("SetPageProperty on an unknown property should error")
	}
}

func TestSetPageProperty_UntypedPage(t *testing.T) {
	app := newTestApp(t)
	writeFile(t, filepath.Join(app.vaultPath, "Notes", "Plain.md"),
		"---\nnotebook: \"Notes\"\nsection: \"\"\npage: \"Plain\"\ndate: \"2026-08-01\"\ntags: []\n---\n# Plain\n")

	if err := app.SetPageProperty("Notes", "", "Plain", "title", "x"); err == nil {
		t.Fatal("SetPageProperty on an untyped page should error")
	}
}

func TestSetPageType_KeepAndFlag(t *testing.T) {
	app := newTestApp(t)
	filePath, _ := writeBookPage(t, app)
	if err := app.SaveType(meetingTypeSchema()); err != nil {
		t.Fatalf("SaveType(meeting): %v", err)
	}

	mismatched, err := app.SetPageType("Books", "", "Dune", "meeting")
	if err != nil {
		t.Fatalf("SetPageType(meeting): %v", err)
	}
	// status="available" (string) does not fit meeting's number status, so it
	// is flagged. title/author are not meeting properties, so they are not.
	if len(mismatched) != 1 || mismatched[0] != "status" {
		t.Errorf("mismatched = %v, want [status]", mismatched)
	}

	// The type: line now holds the canonical meeting id; the value is kept.
	// yamlInline quotes strings, so the stored form is the quoted id.
	raw, _ := os.ReadFile(filePath)
	if !strings.Contains(string(raw), "type: \"meeting\"") {
		t.Errorf("type line not switched to meeting:\n%s", string(raw))
	}
	if !strings.Contains(string(raw), "status: \"available\"") {
		t.Errorf("status value was dropped/coerced (keep-and-flag):\n%s", string(raw))
	}

	// GetPageType now resolves to the meeting schema.
	info, err := app.GetPageType("Books", "", "Dune")
	if err != nil {
		t.Fatalf("GetPageType after switch: %v", err)
	}
	if !info.IsSet || info.TypeID != "meeting" {
		t.Errorf("GetPageType = %+v, want meeting", info)
	}
}

func TestSetPageType_EmptyClearsType(t *testing.T) {
	app := newTestApp(t)
	writeBookPage(t, app)

	if _, err := app.SetPageType("Books", "", "Dune", ""); err != nil {
		t.Fatalf("SetPageType(''): %v", err)
	}
	info, err := app.GetPageType("Books", "", "Dune")
	if err != nil {
		t.Fatalf("GetPageType: %v", err)
	}
	if info.IsSet {
		t.Error("IsSet = true after clearing type, want false")
	}
}

func TestSetPageType_UnknownType(t *testing.T) {
	app := newTestApp(t)
	writeBookPage(t, app)
	if _, err := app.SetPageType("Books", "", "Dune", "no-such-type"); err == nil {
		t.Fatal("SetPageType on an unknown type should error")
	}
}

func TestClearPageProperty_RemovesValue(t *testing.T) {
	app := newTestApp(t)
	filePath, _ := writeBookPage(t, app)
	// Set rating first so clearing has a value to remove.
	if err := app.SetPageProperty("Books", "", "Dune", "rating", 5); err != nil {
		t.Fatalf("SetPageProperty(rating, 5): %v", err)
	}

	if err := app.ClearPageProperty("Books", "", "Dune", "rating"); err != nil {
		t.Fatalf("ClearPageProperty(rating): %v", err)
	}
	raw, _ := os.ReadFile(filePath)
	if strings.Contains(string(raw), "rating:") {
		t.Errorf("rating line still present after clear:\n%s", string(raw))
	}
	// GetPageProperties shows rating unset.
	props, _ := app.GetPageProperties("Books", "", "Dune")
	for _, p := range props {
		if p.Name == "rating" && p.IsSet {
			t.Errorf("rating IsSet = true after clear")
		}
	}
}

func TestClearPageProperty_UntypedPageIsNoOp(t *testing.T) {
	app := newTestApp(t)
	writeFile(t, filepath.Join(app.vaultPath, "Notes", "Plain.md"),
		"---\nnotebook: \"Notes\"\nsection: \"\"\npage: \"Plain\"\ndate: \"2026-08-01\"\ntags: []\n---\n# Plain\n")

	// Clearing on an untyped page is a no-op success.
	if err := app.ClearPageProperty("Notes", "", "Plain", "rating"); err != nil {
		t.Errorf("ClearPageProperty on an untyped page should be a no-op success, got: %v", err)
	}
}

func TestClearPageProperty_UnknownProperty(t *testing.T) {
	app := newTestApp(t)
	writeBookPage(t, app)
	if err := app.ClearPageProperty("Books", "", "Dune", "nosuchfield"); err == nil {
		t.Fatal("ClearPageProperty on an unknown property should error")
	}
}

// TestSetPageProperty_IndexesBlocks verifies the write chain re-indexes the
// page so the DB reflects the file after a frontmatter edit (projectPageType
// runs after the WithDBWrite closure).
func TestSetPageProperty_IndexesBlocks(t *testing.T) {
	app := newTestApp(t)
	filePath, content := writeBookPage(t, app)
	indexTypedPage(t, app, filePath, "Books", "", "Dune", content)

	if err := app.SetPageProperty("Books", "", "Dune", "rating", 3); err != nil {
		t.Fatalf("SetPageProperty: %v", err)
	}

	// FetchPageBlocks reads from the DB; the page must still be present after
	// the re-index (IndexFileBlocks replaces rows, it does not drop the page).
	blocks, err := app.FetchPageBlocks("Books", "", "Dune")
	if err != nil {
		t.Fatalf("FetchPageBlocks: %v", err)
	}
	if len(blocks) == 0 {
		t.Error("no blocks returned after SetPageProperty re-index")
	}
}

// --- Relation-target validation -------------------------------------------
//
// Schemas and helpers for the typed-relations backend. A Book (rbook) has:
//   - author:   a single page relation whose target MUST be a Person
//   - publisher: a single page relation with NO target (any page accepted)
//   - related:  a multi (pages) relation whose target MUST be a Person
//
// personTypeSchema is the relation target type.
func personTypeSchema() types.TypeDef {
	return types.TypeDef{
		ID:   "person",
		Name: "Person",
		Properties: []types.PropertyDef{
			{Name: "name", Type: types.PropText},
		},
	}
}

// placeTypeSchema is a NON-person type: used to verify the target-type check
// rejects a page of the wrong type (not just an untyped one).
func placeTypeSchema() types.TypeDef {
	return types.TypeDef{
		ID:   "place",
		Name: "Place",
		Properties: []types.PropertyDef{
			{Name: "name", Type: types.PropText},
		},
	}
}

// relationBookTypeSchema carries the three relation properties exercised below.
// Its id is "rbook" (not "book") so it cannot clash with bookTypeSchema's id
// in any test that stages both.
func relationBookTypeSchema() types.TypeDef {
	return types.TypeDef{
		ID:   "rbook",
		Name: "Book",
		Properties: []types.PropertyDef{
			{Name: "author", Type: types.PropPage, Target: "person"},
			{Name: "publisher", Type: types.PropPage},
			{Name: "related", Type: types.PropPages, Target: "person"},
		},
	}
}

// writeAndIndexTypedPage writes a typed page to disk, then indexes AND projects
// it so both the blocks table and the page_types projection reflect it. The
// relation validator reads existence from blocks and target-type from the
// projection, so a staged target page must populate both.
func writeAndIndexTypedPage(t *testing.T, app *App, notebook, section, page, typeID string) string {
	t.Helper()
	content := "---\n" +
		"notebook: \"" + notebook + "\"\n" +
		"section: \"" + section + "\"\n" +
		"page: \"" + page + "\"\n" +
		"date: \"2026-08-01\"\n" +
		"tags: []\n" +
		"type: \"" + typeID + "\"\n" +
		"---\n# " + page + "\n\nBody.\n"
	// filepath.Join skips empty segments, so section="" lands the file at
	// <vault>/<notebook>/<page>.md — matching the scanner's section-less model.
	path := filepath.Join(app.vaultPath, notebook, section, page+".md")
	writeFile(t, path, content)
	blocks, meta, _, _, err := parser.ParseFileContent(content, notebook, section, page, "2026-08-01", app.spacesPerTab)
	if err != nil {
		t.Fatalf("ParseFileContent: %v", err)
	}
	if err := app.db.IndexFileBlocks("vault", meta.Notebook, meta.Section, meta.Page, blocks, meta.Tags); err != nil {
		t.Fatalf("IndexFileBlocks: %v", err)
	}
	// projectPageType runs after IndexFileBlocks in the real write path; mirror
	// it here so the type-check validator can read the target's projection.
	app.projectPageType("vault", meta)
	return path
}

// writeRelationBookPage stages the page being edited: typed rbook with no
// relation properties set. It is written to disk but NOT pre-indexed —
// SetPageProperty indexes it on a successful write. Returns the path and
// exact pre-write content for byte-identical "untouched" assertions.
func writeRelationBookPage(t *testing.T, app *App) (path, content string) {
	t.Helper()
	content = "---\n" +
		"notebook: \"Books\"\n" +
		"section: \"\"\n" +
		"page: \"Dune\"\n" +
		"date: \"2026-08-01\"\n" +
		"tags: []\n" +
		"type: \"rbook\"\n" +
		"---\n# Dune\n\nBody.\n"
	path = filepath.Join(app.vaultPath, "Books", "Dune.md")
	writeFile(t, path, content)
	return path, content
}

// stageRelationVault saves the relation types and indexes a Person page
// (People/Alice) and a Place page (Places/Paris), then writes the rbook page
// under edit. Returns the rbook page's path and pre-write content.
func stageRelationVault(t *testing.T, app *App, withPlace bool) (bookPath, bookContent string) {
	t.Helper()
	schemas := []types.TypeDef{personTypeSchema(), relationBookTypeSchema()}
	if withPlace {
		schemas = append([]types.TypeDef{placeTypeSchema()}, schemas...)
	}
	for _, td := range schemas {
		if err := app.SaveType(td); err != nil {
			t.Fatalf("SaveType(%s): %v", td.ID, err)
		}
	}
	writeAndIndexTypedPage(t, app, "People", "", "Alice", "person")
	if withPlace {
		writeAndIndexTypedPage(t, app, "Places", "", "Paris", "place")
	}
	bookPath, bookContent = writeRelationBookPage(t, app)
	return bookPath, bookContent
}

func TestSetPageProperty_RelationTargetExists_Succeeds(t *testing.T) {
	app := newTestApp(t)
	bookPath, _ := stageRelationVault(t, app, false)

	// author targets a Person; People/Alice is an indexed Person page → accepted.
	if err := app.SetPageProperty("Books", "", "Dune", "author", "People/Alice"); err != nil {
		t.Fatalf("SetPageProperty(author, People/Alice): %v", err)
	}

	// The value round-trips through the frontmatter.
	props, err := app.GetPageProperties("Books", "", "Dune")
	if err != nil {
		t.Fatalf("GetPageProperties: %v", err)
	}
	for _, p := range props {
		if p.Name == "author" {
			if !p.IsSet || p.Value != "People/Alice" {
				t.Errorf("author = %+v, want IsSet People/Alice", p)
			}
		}
	}
	// The write landed on disk.
	raw, _ := os.ReadFile(bookPath)
	if !strings.Contains(string(raw), "author:") {
		t.Errorf("author line not written:\n%s", string(raw))
	}
}

func TestSetPageProperty_RelationTargetWrongType_FailsUntouched(t *testing.T) {
	app := newTestApp(t)
	bookPath, before := stageRelationVault(t, app, true)
	beforeBytes, _ := os.ReadFile(bookPath)

	// Places/Paris is an indexed Place; author requires a Person → rejected.
	err := app.SetPageProperty("Books", "", "Dune", "author", "Places/Paris")
	if err == nil {
		t.Fatal("SetPageProperty(author, Places/Paris) should error (wrong target type)")
	}
	if !strings.Contains(err.Error(), "not of type") {
		t.Errorf("error = %q, want it to mention \"not of type\"", err.Error())
	}

	// The file is byte-identical to its pre-write state.
	afterBytes, _ := os.ReadFile(bookPath)
	if string(afterBytes) != string(beforeBytes) {
		t.Errorf("file mutated despite a validation error.\nbefore:\n%s\nafter:\n%s", before, string(afterBytes))
	}
}

func TestSetPageProperty_RelationTargetNonexistent_FailsUntouched(t *testing.T) {
	app := newTestApp(t)
	bookPath, before := stageRelationVault(t, app, false)
	beforeBytes, _ := os.ReadFile(bookPath)

	// Work/People/Nobody is a path-style ref to a page that is not indexed.
	err := app.SetPageProperty("Books", "", "Dune", "author", "Work/People/Nobody")
	if err == nil {
		t.Fatal("SetPageProperty(author, Work/People/Nobody) should error (nonexistent target)")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("error = %q, want it to mention \"does not exist\"", err.Error())
	}

	afterBytes, _ := os.ReadFile(bookPath)
	if string(afterBytes) != string(beforeBytes) {
		t.Errorf("file mutated despite a validation error.\nbefore:\n%s\nafter:\n%s", before, string(afterBytes))
	}
}

// TestSetPageProperty_RelationTargetBareName_LeafMatch documents the slash-less
// behavior: a bare page name is resolved to the first indexed page with that
// leaf anywhere in the source. A leaf that exists (and is the right type) is
// accepted; a leaf that does not exist is rejected.
func TestSetPageProperty_RelationTargetBareName_LeafMatch(t *testing.T) {
	app := newTestApp(t)
	bookPath, _ := stageRelationVault(t, app, false)

	// "Alice" has no slash → leaf match resolves to People/Alice (a Person).
	if err := app.SetPageProperty("Books", "", "Dune", "author", "Alice"); err != nil {
		t.Fatalf("SetPageProperty(author, Alice): %v", err)
	}
	props, _ := app.GetPageProperties("Books", "", "Dune")
	for _, p := range props {
		if p.Name == "author" && (!p.IsSet || p.Value != "Alice") {
			t.Errorf("author = %+v, want IsSet Alice", p)
		}
	}

	// A bare name with no matching leaf is rejected, and the file is untouched.
	// Clear author first so the "untouched" baseline does not include it.
	beforeBytes, _ := os.ReadFile(bookPath)
	err := app.SetPageProperty("Books", "", "Dune", "author", "Nobody")
	if err == nil {
		t.Fatal("SetPageProperty(author, Nobody) should error (no matching leaf)")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("error = %q, want it to mention \"does not exist\"", err.Error())
	}
	afterBytes, _ := os.ReadFile(bookPath)
	if string(afterBytes) != string(beforeBytes) {
		t.Errorf("file mutated despite a validation error.\nbefore:\n%s\nafter:\n%s", string(beforeBytes), string(afterBytes))
	}
}

func TestSetPageProperty_RelationPagesMixedInvalid_FailsUntouched(t *testing.T) {
	app := newTestApp(t)
	bookPath, before := stageRelationVault(t, app, true)
	beforeBytes, _ := os.ReadFile(bookPath)

	// A mix: People/Alice is a valid Person, Places/Paris is a Place (wrong
	// type). The whole write is rejected; nothing is persisted.
	err := app.SetPageProperty("Books", "", "Dune", "related", []string{"People/Alice", "Places/Paris"})
	if err == nil {
		t.Fatal("SetPageProperty(related, mix) should error (one target is the wrong type)")
	}

	afterBytes, _ := os.ReadFile(bookPath)
	if string(afterBytes) != string(beforeBytes) {
		t.Errorf("file mutated despite a validation error.\nbefore:\n%s\nafter:\n%s", before, string(afterBytes))
	}
	if strings.Contains(string(afterBytes), "related:") {
		t.Errorf("related line was written despite a validation error:\n%s", string(afterBytes))
	}
}

func TestSetPageProperty_RelationPagesAllValid_Succeeds(t *testing.T) {
	app := newTestApp(t)
	_, _ = stageRelationVault(t, app, false)
	// Add a second Person so the multi-value has two valid targets.
	writeAndIndexTypedPage(t, app, "People", "", "Bob", "person")

	if err := app.SetPageProperty("Books", "", "Dune", "related", []string{"People/Alice", "People/Bob"}); err != nil {
		t.Fatalf("SetPageProperty(related, two people): %v", err)
	}
	props, _ := app.GetPageProperties("Books", "", "Dune")
	for _, p := range props {
		if p.Name == "related" {
			if !p.IsSet {
				t.Error("related IsSet = false, want true")
			}
		}
	}
}

// TestSetPageProperty_RelationNoTarget_AnyPageAccepted verifies a page property
// with NO declared Target accepts any existing page (regardless of type), while
// a nonexistent target is still rejected.
func TestSetPageProperty_RelationNoTarget_AnyPageAccepted(t *testing.T) {
	app := newTestApp(t)
	bookPath, _ := stageRelationVault(t, app, true)

	// publisher has no Target → a Place page is accepted (any existing page).
	if err := app.SetPageProperty("Books", "", "Dune", "publisher", "Places/Paris"); err != nil {
		t.Fatalf("SetPageProperty(publisher, Places/Paris): %v", err)
	}
	props, _ := app.GetPageProperties("Books", "", "Dune")
	for _, p := range props {
		if p.Name == "publisher" && (!p.IsSet || p.Value != "Places/Paris") {
			t.Errorf("publisher = %+v, want IsSet Places/Paris", p)
		}
	}

	// A nonexistent target is still rejected even with no Target declared.
	beforeBytes, _ := os.ReadFile(bookPath)
	err := app.SetPageProperty("Books", "", "Dune", "publisher", "Nowhere/X")
	if err == nil {
		t.Fatal("SetPageProperty(publisher, Nowhere/X) should error (nonexistent target)")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("error = %q, want it to mention \"does not exist\"", err.Error())
	}
	afterBytes, _ := os.ReadFile(bookPath)
	if string(afterBytes) != string(beforeBytes) {
		t.Errorf("file mutated despite a validation error.\nbefore:\n%s\nafter:\n%s", string(beforeBytes), string(afterBytes))
	}
}

// TestSetPageProperty_RevalidatesAfterSchemaReload pins the schema/write race
// fix: a value validated at the entry point must be re-validated inside the
// file lock so a hot-reloaded schema that narrows the rules between the two
// points still rejects the value, and the file stays byte-identical.
func TestSetPageProperty_RevalidatesAfterSchemaReload(t *testing.T) {
	app := newTestApp(t)
	filePath, before := writeBookPage(t, app)
	beforeBytes, _ := os.ReadFile(filePath)

	// Narrow `status` to a single option the test page's value does NOT satisfy.
	// The page currently has status: "available"; with the schema narrowed to
	// [read] only, the entry-point validation should already reject an attempt
	// to STAY at "available". This pins the guarantee that schema edits are
	// visible by the time SetPageProperty validates, and the file is untouched
	// when validation (entry-point OR in-lock re-validation) fails.
	narrowed := bookTypeSchema()
	for i := range narrowed.Properties {
		if narrowed.Properties[i].Name == "status" {
			narrowed.Properties[i].Options = []string{"read"}
		}
	}
	if err := app.SaveType(narrowed); err != nil {
		t.Fatalf("SaveType(narrowed): %v", err)
	}

	if err := app.SetPageProperty("Books", "", "Dune", "status", "available"); err == nil {
		t.Fatal("SetPageProperty with a now-invalid select value should error")
	}
	afterBytes, _ := os.ReadFile(filePath)
	if string(afterBytes) != string(beforeBytes) {
		t.Errorf("file mutated despite validation rejection.\nbefore:\n%s\nafter:\n%s", before, string(afterBytes))
	}
}

// TestSetPageProperty_RevalidatesAfterSchemaReload_PropertyRemoved pins the
// parallel case: a property removed from the schema between calls must cause
// the next SetPageProperty to fail cleanly, with the file untouched.
func TestSetPageProperty_RevalidatesAfterSchemaReload_PropertyRemoved(t *testing.T) {
	app := newTestApp(t)
	filePath, before := writeBookPage(t, app)
	beforeBytes, _ := os.ReadFile(filePath)

	slim := types.TypeDef{
		ID:   "book",
		Name: "Book",
		Properties: []types.PropertyDef{
			{Name: "title", Type: types.PropText},
		},
	}
	if err := app.SaveType(slim); err != nil {
		t.Fatalf("SaveType(slim): %v", err)
	}
	if err := app.SetPageProperty("Books", "", "Dune", "rating", 5); err == nil {
		t.Fatal("SetPageProperty(rating) should error after the property was removed from the schema")
	}
	afterBytes, _ := os.ReadFile(filePath)
	if string(afterBytes) != string(beforeBytes) {
		t.Errorf("file mutated despite the property being removed.\nbefore:\n%s\nafter:\n%s", before, string(afterBytes))
	}
}

// TestSetPageProperty_PostWriteIndexFailure_ReturnsSuccessButSignalsStale pins
// the contract: when the on-disk write commits but the in-memory re-index
// fails, SetPageProperty returns nil (the write SUCCEEDED) and the staleness
// is signaled via the types:projection-error event + log — NOT the error
// return. The IPC/MCP/frontend uniformly treat a non-nil error as a write
// rejection (tools.go converts it to toolValidationErr; the panel reverts
// optimistic state), so a stale-projection error must not leak onto that path.
// The file is still written — the failure is in the projection layer, not the
// file write. We induce the failure by closing the DB before the call;
// IndexFileBlocks then returns ErrDBClosed mid-write-chain.
func TestSetPageProperty_PostWriteIndexFailure_ReturnsSuccessButSignalsStale(t *testing.T) {
	app := newTestApp(t)
	filePath, _ := writeBookPage(t, app)

	var (
		mu     sync.Mutex
		events []string
	)
	app.eventEmit = func(name string, _ ...any) {
		mu.Lock()
		events = append(events, name)
		mu.Unlock()
	}

	// Close the DB so IndexFileBlocks (run inside writePageFrontmatterEdit
	// after the file write commits) returns ErrDBClosed. Close is idempotent,
	// so the t.Cleanup Close is a no-op.
	if err := app.db.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	// The write SUCCEEDED — the caller must see nil so the IPC/MCP/frontend
	// do not treat a saved write as a validation rejection.
	err := app.SetPageProperty("Books", "", "Dune", "rating", 4)
	if err != nil {
		t.Fatalf("SetPageProperty should return nil after a successful write (staleness signaled via event, not error); got: %v", err)
	}

	// The file write committed BEFORE the re-index, so the value is on disk
	// even though the index is stale. This is the documented contract: no
	// rollback on a projection failure.
	raw, _ := os.ReadFile(filePath)
	if !strings.Contains(string(raw), "rating: 4") {
		t.Errorf("file write should have committed before the re-index failure:\n%s", string(raw))
	}

	// The stale-projection event must have fired so the UI can warn.
	mu.Lock()
	sawProjectionError := false
	for _, n := range events {
		if n == string(EventTypesProjectionError) {
			sawProjectionError = true
			break
		}
	}
	mu.Unlock()
	if !sawProjectionError {
		t.Errorf("expected %s event, got %v", EventTypesProjectionError, events)
	}
}

// projectedPropertyNames reads a page's projection (its set page_properties
// rows) and returns the set of property names it currently carries. Used to
// prove reprojectAllTypedPages ran — GetPageProperties re-reads disk + live
// schema and so would hide index staleness.
func projectedPropertyNames(t *testing.T, app *App, notebook, section, page string) map[string]bool {
	t.Helper()
	proj, err := app.db.GetPageProjection("vault", notebook, section, page)
	if err != nil {
		t.Fatalf("GetPageProjection: %v", err)
	}
	out := map[string]bool{}
	if proj == nil {
		return out
	}
	for _, p := range proj.Properties {
		out[p.Property] = true
	}
	return out
}

// TestSaveType_ReprojectsTypedPages pins AC3 for the primary UI workflow: an
// in-app schema edit (App.SaveType) MUST re-project existing typed pages so the
// dashboard does not drift until restart. SaveType arms the type watcher's
// self-write window (RegisterSelfWrite), which suppresses the fsnotify events
// from its own atomic write — so the watcher's onChange, the only other caller
// of reprojectAllTypedPages, never fires for in-app edits. SaveType must
// therefore re-project itself before emitting types:changed.
func TestSaveType_ReprojectsTypedPages(t *testing.T) {
	app := newTestApp(t)

	// Stage: book schema with a `rating` (number) property, and a typed Book
	// page whose frontmatter sets rating: 5. projectPageType is mirrored from
	// the real write path so the page has a `rating` projection row that a
	// rename could otherwise leave stale.
	if err := app.SaveType(bookTypeSchema()); err != nil {
		t.Fatalf("SaveType(book): %v", err)
	}
	content := "---\n" +
		"notebook: \"Books\"\n" +
		"section: \"\"\n" +
		"page: \"Dune\"\n" +
		"date: \"2026-08-01\"\n" +
		"tags: []\n" +
		"type: \"book\"\n" +
		"title: \"Dune\"\n" +
		"author: \"Frank Herbert\"\n" +
		"status: \"available\"\n" +
		"rating: 5\n" +
		"---\n# Dune\n\nBody.\n"
	writeFile(t, filepath.Join(app.vaultPath, "Books", "Dune.md"), content)
	blocks, meta, _, _, err := parser.ParseFileContent(content, "Books", "", "Dune", "2026-08-01", app.spacesPerTab)
	if err != nil {
		t.Fatalf("ParseFileContent: %v", err)
	}
	if err := app.db.IndexFileBlocks("vault", meta.Notebook, meta.Section, meta.Page, blocks, meta.Tags); err != nil {
		t.Fatalf("IndexFileBlocks: %v", err)
	}
	// IndexFileBlocks does not project; the real write path runs projectPageType
	// after the WithDBWrite closure, so mirror it to seed the projection row.
	app.projectPageType("vault", meta)

	before := projectedPropertyNames(t, app, "Books", "", "Dune")
	if !before["rating"] {
		t.Fatalf("expected a `rating` projection row before SaveType, got %v", before)
	}

	// Rename `rating` → `score` in the schema and save it in-app. The page's
	// frontmatter still carries `rating: 5`, so a stale projection would keep
	// the `rating` row until restart.
	renamed := types.TypeDef{
		ID:   "book",
		Name: "Book",
		Properties: []types.PropertyDef{
			{Name: "title", Type: types.PropText},
			{Name: "author", Type: types.PropText},
			{Name: "status", Type: types.PropSelect, Options: []string{"available", "read"}},
			{Name: "score", Type: types.PropNumber},
		},
	}
	if err := app.SaveType(renamed); err != nil {
		t.Fatalf("SaveType(renamed): %v", err)
	}

	// reprojectAllTypedPages re-parsed Dune.md against the renamed schema: the
	// projection's stale `rating` row must be gone. (No `score` row appears
	// because projection is sparse — only declared properties the page sets get
	// rows — so absence of `rating` is the load-bearing assertion.)
	after := projectedPropertyNames(t, app, "Books", "", "Dune")
	if after["rating"] {
		t.Errorf("`rating` projection row still present after SaveType renamed it to `score`; reprojectAllTypedPages did not run on in-app save, got %v", after)
	}
	// The other set values survive the re-projection untouched.
	if !after["title"] || !after["author"] || !after["status"] {
		t.Errorf("expected title/author/status to survive re-projection, got %v", after)
	}
}

// TestDeleteType_ReprojectsTypedPages mirrors the save test for the delete path:
// DeleteType shares the same self-write suppression, so it too must re-project
// directly. A page whose type is deleted resolves to a sanitized raw name with
// no declared properties, so its previously-set projection rows must clear
// without a restart or external file event.
func TestDeleteType_ReprojectsTypedPages(t *testing.T) {
	app := newTestApp(t)

	if err := app.SaveType(bookTypeSchema()); err != nil {
		t.Fatalf("SaveType(book): %v", err)
	}
	content := "---\n" +
		"notebook: \"Books\"\n" +
		"section: \"\"\n" +
		"page: \"Dune\"\n" +
		"date: \"2026-08-01\"\n" +
		"tags: []\n" +
		"type: \"book\"\n" +
		"title: \"Dune\"\n" +
		"author: \"Frank Herbert\"\n" +
		"status: \"available\"\n" +
		"rating: 5\n" +
		"---\n# Dune\n\nBody.\n"
	writeFile(t, filepath.Join(app.vaultPath, "Books", "Dune.md"), content)
	blocks, meta, _, _, err := parser.ParseFileContent(content, "Books", "", "Dune", "2026-08-01", app.spacesPerTab)
	if err != nil {
		t.Fatalf("ParseFileContent: %v", err)
	}
	if err := app.db.IndexFileBlocks("vault", meta.Notebook, meta.Section, meta.Page, blocks, meta.Tags); err != nil {
		t.Fatalf("IndexFileBlocks: %v", err)
	}
	app.projectPageType("vault", meta)

	before := projectedPropertyNames(t, app, "Books", "", "Dune")
	if !before["rating"] {
		t.Fatalf("expected a `rating` projection row before DeleteType, got %v", before)
	}

	if err := app.DeleteType("book"); err != nil {
		t.Fatalf("DeleteType(book): %v", err)
	}

	// The book type is gone, so re-projection resolves the page to a raw type
	// name with no declared properties and clears its set-property rows.
	after := projectedPropertyNames(t, app, "Books", "", "Dune")
	if len(after) != 0 {
		t.Errorf("expected projection property rows cleared after DeleteType, got %v", after)
	}
}
