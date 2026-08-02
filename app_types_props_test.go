package main

import (
	"os"
	"path/filepath"
	"strings"
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
