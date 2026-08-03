package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"silt/backend/parser"
	"silt/backend/types"
)

// mcpMetaBridge wraps mcpBridge to expose the typed-property surface for
// integration tests. mcpBridge already implements mcp.Bridge; the wrapper just
// narrows the type so tests call the three Phase-7 methods without a type
// assertion on every line.
type mcpMetaBridge struct{ mcpBridge }

func newMetaBridge(app *App) mcpMetaBridge {
	return mcpMetaBridge{mcpBridge{app: app, vaultPath: app.vaultPath}}
}

// writeBookPageForMCP is the local equivalent of writeBookPage in
// app_types_props_test.go, sized for the bridge tests. Defined here so the file
// is self-contained; the helper in app_types_props_test.go is package-scoped
// but a fresh local keeps the on-disk contract obvious next to its assertions.
func writeBookPageForMCP(t *testing.T, app *App) (filePath, content string) {
	t.Helper()
	if err := app.SaveType(types.TypeDef{
		ID:   "book",
		Name: "Book",
		Properties: []types.PropertyDef{
			{Name: "title", Type: types.PropText},
			{Name: "status", Type: types.PropSelect, Options: []string{"available", "read"}},
			{Name: "rating", Type: types.PropNumber},
			{Name: "author", Type: types.PropPage, Target: "person"},
		},
	}); err != nil {
		t.Fatalf("SaveType(book): %v", err)
	}
	if err := app.SaveType(types.TypeDef{
		ID:   "person",
		Name: "Person",
		Properties: []types.PropertyDef{
			{Name: "name", Type: types.PropText},
		},
	}); err != nil {
		t.Fatalf("SaveType(person): %v", err)
	}
	content = "---\n" +
		"notebook: \"Books\"\n" +
		"section: \"\"\n" +
		"page: \"Dune\"\n" +
		"date: \"2026-08-01\"\n" +
		"tags: []\n" +
		"type: \"book\"\n" +
		"title: \"Dune\"\n" +
		"status: \"available\"\n" +
		"---\n# Dune\n\nBody.\n"
	filePath = filepath.Join(app.vaultPath, "Books", "Dune.md")
	writeFile(t, filePath, content)
	return filePath, content
}

// indexPageForMCP parses + indexes a page so the DB mirrors the file. The
// relation validator reads target existence from the index, so a staged Person
// target page must be indexed before set_page_property can accept it.
func indexPageForMCP(t *testing.T, app *App, content, notebook, section, page string) {
	t.Helper()
	blocks, meta, _, _, err := parser.ParseFileContent(content, notebook, section, page, "2026-08-01", app.spacesPerTab)
	if err != nil {
		t.Fatalf("ParseFileContent: %v", err)
	}
	if err := app.db.IndexFileBlocks("vault", meta.Notebook, meta.Section, meta.Page, blocks, meta.Tags); err != nil {
		t.Fatalf("IndexFileBlocks: %v", err)
	}
	app.projectPageType("vault", meta)
}

// TestMCPBridge_GetPageMetadata_TypedAndUntyped verifies the bridge combines
// the three App reads into one snapshot: schema-merged properties + type id +
// raw frontmatter for a typed page, and an empty type with raw frontmatter for
// an untyped page.
func TestMCPBridge_GetPageMetadata_TypedAndUntyped(t *testing.T) {
	app := newTestApp(t)
	bridge := newMetaBridge(app)
	filePath, content := writeBookPageForMCP(t, app)
	indexPageForMCP(t, app, content, "Books", "", "Dune")

	res, err := bridge.GetPageMetadata(context.Background(), "Books", "", "Dune")
	if err != nil {
		t.Fatalf("GetPageMetadata: %v", err)
	}
	if res.Type != "book" {
		t.Errorf("Type = %q want book", res.Type)
	}
	if len(res.Properties) != 4 {
		t.Fatalf("Properties len = %d want 4: %+v", len(res.Properties), res.Properties)
	}
	// status is set in the frontmatter; rating is not.
	var foundStatusSet, foundRatingUnset bool
	for _, p := range res.Properties {
		if p.Name == "status" {
			if p.IsSet {
				foundStatusSet = true
			}
		}
		if p.Name == "rating" && !p.IsSet {
			foundRatingUnset = true
		}
	}
	if !foundStatusSet {
		t.Error("status IsSet = false, want true")
	}
	if !foundRatingUnset {
		t.Error("rating IsSet = true, want false")
	}
	// Raw frontmatter is the all-keys parsed map, not just schema-declared keys.
	if res.Frontmatter == nil {
		t.Fatal("Frontmatter = nil")
	}
	if res.Frontmatter["title"] != "Dune" {
		t.Errorf("Frontmatter title = %v want Dune", res.Frontmatter["title"])
	}
	if res.Frontmatter["notebook"] != "Books" {
		t.Errorf("Frontmatter notebook = %v want Books", res.Frontmatter["notebook"])
	}

	// Untyped page: Type empty, Properties empty, Frontmatter still populated.
	writeFile(t, filepath.Join(app.vaultPath, "Notes", "Plain.md"),
		"---\nnotebook: \"Notes\"\nsection: \"\"\npage: \"Plain\"\ndate: \"2026-08-01\"\ntags: []\n---\n# Plain\n")
	res2, err := bridge.GetPageMetadata(context.Background(), "Notes", "", "Plain")
	if err != nil {
		t.Fatalf("GetPageMetadata (untyped): %v", err)
	}
	if res2.Type != "" {
		t.Errorf("untyped Type = %q want empty", res2.Type)
	}
	if len(res2.Properties) != 0 {
		t.Errorf("untyped Properties = %+v want empty", res2.Properties)
	}
	if res2.Frontmatter == nil {
		t.Error("untyped Frontmatter = nil, want raw map")
	}

	// Missing page surfaces the read error verbatim — no opaque wrapping.
	if _, err := bridge.GetPageMetadata(context.Background(), "Books", "", "Missing"); err == nil {
		t.Fatal("expected error for missing page")
	}

	// book.md file should still exist (smoke-check that the read path did not
	// accidentally create or mutate anything).
	if _, err := os.Stat(filePath); err != nil {
		t.Fatalf("typed page file vanished: %v", err)
	}
}

// TestMCPBridge_GetPageMetadata_RawTypeForUnknownSchema verifies that a page
// whose type ref does not resolve to a known schema surfaces the raw ref in
// Type (not empty), so clients can render a raw chip instead of seeing a
// silent untyped page.
func TestMCPBridge_GetPageMetadata_RawTypeForUnknownSchema(t *testing.T) {
	app := newTestApp(t)
	bridge := newMetaBridge(app)
	writeFile(t, filepath.Join(app.vaultPath, "Notes", "Weird.md"),
		"---\nnotebook: \"Notes\"\nsection: \"\"\npage: \"Weird\"\ndate: \"2026-08-01\"\ntags: []\ntype: \"no-such-schema\"\n---\n# Weird\n")
	res, err := bridge.GetPageMetadata(context.Background(), "Notes", "", "Weird")
	if err != nil {
		t.Fatalf("GetPageMetadata: %v", err)
	}
	if res.Type != "no-such-schema" {
		t.Errorf("Type = %q want the raw ref no-such-schema", res.Type)
	}
	if len(res.Properties) != 0 {
		t.Errorf("Properties = %+v, want empty for unknown schema", res.Properties)
	}
}

// TestMCPBridge_GetPageMetadata_CoherentAfterSetProperty pins the single-lock
// snapshot contract on GetPageMetadata: after a write, the three views (type,
// schema-merged properties, raw frontmatter) must agree — every property the
// schema merges as IsSet also appears in the raw frontmatter with the same
// value, and the type id matches. GetPageMetadata reads all three under one
// vaultMu.RLock via the *Locked helpers, so a concurrent writer cannot yield a
// mixed N / N+1 snapshot. A full concurrent-race test is not deterministic, so
// the coherence invariant (the property the load-bearing fix protects) is the
// assertion.
func TestMCPBridge_GetPageMetadata_CoherentAfterSetProperty(t *testing.T) {
	app := newTestApp(t)
	bridge := newMetaBridge(app)
	filePath, content := writeBookPageForMCP(t, app)
	indexPageForMCP(t, app, content, "Books", "", "Dune")

	if err := bridge.SetPageProperty(context.Background(), "Books", "", "Dune", "rating", "4"); err != nil {
		t.Fatalf("SetPageProperty(rating, 4): %v", err)
	}

	res, err := bridge.GetPageMetadata(context.Background(), "Books", "", "Dune")
	if err != nil {
		t.Fatalf("GetPageMetadata: %v", err)
	}

	if res.Type != "book" {
		t.Fatalf("Type = %q want book", res.Type)
	}
	// The type id in the result must match the raw frontmatter `type` key — a
	// mixed snapshot would let them drift.
	if got, _ := res.Frontmatter["type"].(string); got != "book" {
		t.Errorf("frontmatter type = %v want book (result/frontmatter disagree)", res.Frontmatter["type"])
	}

	// Every schema-merged property marked IsSet must also be present in the raw
	// frontmatter with the same value. This is the coherence invariant: if
	// properties came from write N+1 and frontmatter from N (the bug the single
	// lock closes), rating could read IsSet in properties but absent in the raw
	// frontmatter (or vice versa).
	var ratingSeen bool
	for _, p := range res.Properties {
		if !p.IsSet {
			continue
		}
		raw, present := res.Frontmatter[p.Name]
		if !present {
			t.Errorf("property %q IsSet in schema-merged view but absent from raw frontmatter (mixed snapshot)", p.Name)
			continue
		}
		if p.Name == "rating" {
			ratingSeen = true
			pf, _ := toFloat(p.Value)
			rf, _ := toFloat(raw)
			if pf != 4 || rf != 4 {
				t.Errorf("rating: property %v / frontmatter %v both want 4", p.Value, raw)
			}
			continue
		}
		// text/select properties: both sides are strings.
		sv, _ := p.Value.(string)
		rv, _ := raw.(string)
		if sv != rv {
			t.Errorf("property %q: schema-merged %q disagrees with raw frontmatter %q", p.Name, sv, rv)
		}
	}

	if !ratingSeen {
		t.Fatalf("rating not IsSet after SetPageProperty: %+v", res.Properties)
	}

	// The disk file reflects the same state (sanity: the read path did not
	// mutate anything and the write landed).
	rawFile, _ := os.ReadFile(filePath)
	if !strings.Contains(string(rawFile), "rating:") {
		t.Errorf("rating line missing from disk:\n%s", string(rawFile))
	}
}

// TestMCPBridge_SetPageProperty_InvalidValueLeavesFileByteIdentical is the
// bridge-level safety contract: an invalid value must reach the bridge, fail
// validation INSIDE App.SetPageProperty (which runs the validator before any
// I/O), and leave the on-disk file byte-identical. Drives the same path the
// set_page_property MCP tool takes, end-to-end through the bridge.
func TestMCPBridge_SetPageProperty_InvalidValueLeavesFileByteIdentical(t *testing.T) {
	app := newTestApp(t)
	bridge := newMetaBridge(app)
	filePath, content := writeBookPageForMCP(t, app)
	before, _ := os.ReadFile(filePath)

	cases := []struct {
		name     string
		property string
		value    string
		errSub   string
	}{
		// A non-numeric value coerces/validates against a number property; the
		// bridge coerces "4" to 4.0 (accepted) but "not-a-number" cannot coerce
		// → CoerceValue returns an error before App.SetPageProperty runs.
		{"non-coercible number", "rating", "not-a-number", "cannot coerce"},
		// "bogus" passes structural coercion (select expects a string), but
		// App.SetPageProperty's structural validator rejects it as not one of
		// [available read] before the write.
		{"invalid select option", "status", "bogus", "not one of the allowed options"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := bridge.SetPageProperty(context.Background(), "Books", "", "Dune", tc.property, tc.value)
			if err == nil {
				t.Fatalf("expected error for %s=%q", tc.property, tc.value)
			}
			if !strings.Contains(err.Error(), tc.errSub) {
				t.Errorf("err = %q, want it to contain %q", err.Error(), tc.errSub)
			}
			after, _ := os.ReadFile(filePath)
			if string(after) != string(before) {
				t.Errorf("file mutated despite %s validation failure.\nbefore:\n%s\nafter:\n%s",
					tc.property, content, string(after))
			}
		})
	}
}

// TestMCPBridge_SetPageProperty_RelationTargetsRejected exercises the
// relation-target check at the bridge level: a wrong-type or nonexistent
// target is rejected and the file is untouched. Mirrors the app-level tests
// but goes through the mcpBridge string-coercion path.
func TestMCPBridge_SetPageProperty_RelationTargetsRejected(t *testing.T) {
	app := newTestApp(t)
	bridge := newMetaBridge(app)
	bookPath, bookContent := writeBookPageForMCP(t, app)

	// Stage a Person target (valid) and a non-Person typed page (wrong type)
	// so the relation validator can distinguish them.
	personPath := filepath.Join(app.vaultPath, "People", "Alice.md")
	personContent := "---\n" +
		"notebook: \"People\"\nsection: \"\"\npage: \"Alice\"\n" +
		"date: \"2026-08-01\"\ntags: []\ntype: \"person\"\n" +
		"name: \"Alice\"\n---\n# Alice\n"
	writeFile(t, personPath, personContent)
	indexPageForMCP(t, app, personContent, "People", "", "Alice")

	placePath := filepath.Join(app.vaultPath, "Places", "Paris.md")
	// Paris uses a distinct `place` type so it is a known-type page that is
	// NOT a person (the relation validator rejects it for an author:Person
	// property). Using `place` instead of `book` keeps this test from
	// polluting the dashboard tests' QueryPagesByType("book") — those tests
	// share the in-memory SQLite DB via cache=shared.
	if err := app.SaveType(types.TypeDef{
		ID:   "place",
		Name: "Place",
		Properties: []types.PropertyDef{
			{Name: "name", Type: types.PropText},
		},
	}); err != nil {
		t.Fatalf("SaveType(place): %v", err)
	}
	placeContent := "---\n" +
		"notebook: \"Places\"\nsection: \"\"\npage: \"Paris\"\n" +
		"date: \"2026-08-01\"\ntags: []\ntype: \"place\"\n" +
		"name: \"Paris\"\n---\n# Paris\n"
	writeFile(t, placePath, placeContent)
	indexPageForMCP(t, app, placeContent, "Places", "", "Paris")

	before, _ := os.ReadFile(bookPath)

	// Wrong type: Places/Paris is type=book, but author requires a person.
	err := bridge.SetPageProperty(context.Background(), "Books", "", "Dune", "author", "Places/Paris")
	if err == nil {
		t.Fatal("expected error for wrong-type relation target")
	}
	if !strings.Contains(err.Error(), "not of type") {
		t.Errorf("err = %q, want 'not of type'", err.Error())
	}
	after, _ := os.ReadFile(bookPath)
	if string(after) != string(before) {
		t.Errorf("file mutated despite wrong-type relation.\nbefore:\n%s\nafter:\n%s",
			bookContent, string(after))
	}

	// Nonexistent path ref: validator rejects, file untouched.
	err = bridge.SetPageProperty(context.Background(), "Books", "", "Dune", "author", "Work/People/Nobody")
	if err == nil {
		t.Fatal("expected error for nonexistent relation target")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("err = %q, want 'does not exist'", err.Error())
	}
	after2, _ := os.ReadFile(bookPath)
	if string(after2) != string(before) {
		t.Errorf("file mutated despite nonexistent relation.\nbefore:\n%s\nafter:\n%s",
			bookContent, string(after2))
	}

	// Valid target: People/Alice is a person, so the write succeeds and the
	// file gains an author line.
	err = bridge.SetPageProperty(context.Background(), "Books", "", "Dune", "author", "People/Alice")
	if err != nil {
		t.Fatalf("expected success for valid relation, got: %v", err)
	}
	after3, _ := os.ReadFile(bookPath)
	if !strings.Contains(string(after3), "author:") {
		t.Errorf("expected author line written; got:\n%s", string(after3))
	}
}

// TestMCPBridge_SetPageProperty_CoercesNumberValue confirms the bridge coerces
// a string value to the property's Go type before delegating, so a numeric
// property accepts "4" (the form an MCP client must send given the tool's
// string field).
func TestMCPBridge_SetPageProperty_CoercesNumberValue(t *testing.T) {
	app := newTestApp(t)
	bridge := newMetaBridge(app)
	filePath, _ := writeBookPageForMCP(t, app)

	if err := bridge.SetPageProperty(context.Background(), "Books", "", "Dune", "rating", "4"); err != nil {
		t.Fatalf("expected coercion to accept \"4\", got: %v", err)
	}
	raw, _ := os.ReadFile(filePath)
	if !strings.Contains(string(raw), "rating:") {
		t.Errorf("expected rating line written; got:\n%s", string(raw))
	}
	// The metadata reflects the coerced numeric value (a number, not "4").
	res, err := bridge.GetPageMetadata(context.Background(), "Books", "", "Dune")
	if err != nil {
		t.Fatalf("GetPageMetadata: %v", err)
	}
	for _, p := range res.Properties {
		if p.Name == "rating" {
			if !p.IsSet {
				t.Error("rating IsSet = false, want true")
			}
			if f, ok := toFloat(p.Value); !ok || f != 4 {
				t.Errorf("rating Value = %v (%T), want 4", p.Value, p.Value)
			}
		}
	}
}

// TestMCPBridge_SetPageProperty_MultiValueCommaSplit verifies the bridge
// accepts a comma-separated string for a multiselect property and persists
// every element. Without the comma-split in CoerceValue, a single tool call
// could only write one value.
func TestMCPBridge_SetPageProperty_MultiValueCommaSplit(t *testing.T) {
	app := newTestApp(t)
	bridge := newMetaBridge(app)

	// Self-contained schema with a multiselect; not reusing writeBookPageForMCP
	// so the shared helper's property count assertion in another test is
	// unaffected.
	if err := app.SaveType(types.TypeDef{
		ID:   "movie",
		Name: "Movie",
		Properties: []types.PropertyDef{
			{Name: "title", Type: types.PropText},
			{Name: "genres", Type: types.PropMultiSelect, Options: []string{"sci-fi", "fantasy", "drama"}},
		},
	}); err != nil {
		t.Fatalf("SaveType(movie): %v", err)
	}
	content := "---\n" +
		"notebook: \"Films\"\nsection: \"\"\npage: \"Dune2021\"\n" +
		"date: \"2026-08-01\"\ntags: []\ntype: \"movie\"\n" +
		"title: \"Dune\"\n---\n# Dune\n"
	writeFile(t, filepath.Join(app.vaultPath, "Films", "Dune2021.md"), content)

	// Whitespace around commas is trimmed; empty segments are dropped.
	if err := bridge.SetPageProperty(context.Background(), "Films", "", "Dune2021", "genres", "sci-fi,  fantasy ,"); err != nil {
		t.Fatalf("expected comma-split write to succeed, got: %v", err)
	}

	res, err := bridge.GetPageMetadata(context.Background(), "Films", "", "Dune2021")
	if err != nil {
		t.Fatalf("GetPageMetadata: %v", err)
	}
	for _, p := range res.Properties {
		if p.Name != "genres" {
			continue
		}
		if !p.IsSet {
			t.Fatal("genres IsSet = false, want true")
		}
		got, ok := p.Value.([]any)
		if !ok {
			t.Fatalf("genres Value = %v (%T), want []any", p.Value, p.Value)
		}
		if len(got) != 2 || got[0] != "sci-fi" || got[1] != "fantasy" {
			t.Errorf("genres Value = %v, want [sci-fi fantasy]", got)
		}
	}

	// An all-empty input is rejected before any I/O.
	before, _ := os.ReadFile(filepath.Join(app.vaultPath, "Films", "Dune2021.md"))
	if err := bridge.SetPageProperty(context.Background(), "Films", "", "Dune2021", "genres", "  , "); err == nil {
		t.Fatal("expected error for all-empty multi-value input")
	}
	after, _ := os.ReadFile(filepath.Join(app.vaultPath, "Films", "Dune2021.md"))
	if string(after) != string(before) {
		t.Errorf("file mutated despite empty multi-value rejection.\nbefore:\n%s\nafter:\n%s",
			string(before), string(after))
	}
}

// TestMCPBridge_SetPageType_AssignsAndClears exercises the type-assign + clear
// path through the bridge, including the validation-before-write contract for
// the assign path (existing values that fail the new schema are flagged but
// kept on disk — the file is still written with the new type id).
func TestMCPBridge_SetPageType_AssignsAndClears(t *testing.T) {
	app := newTestApp(t)
	bridge := newMetaBridge(app)
	filePath, _ := writeBookPageForMCP(t, app)

	// Assigning the same type succeeds and the file is untouched except for
	// the type line (which already matches). No values are flagged.
	flagged, err := bridge.SetPageType(context.Background(), "Books", "", "Dune", "book")
	if err != nil {
		t.Fatalf("SetPageType(book): %v", err)
	}
	if len(flagged) != 0 {
		t.Errorf("flagged = %v, want empty for same-type assign", flagged)
	}
	res, _ := bridge.GetPageMetadata(context.Background(), "Books", "", "Dune")
	if res.Type != "book" {
		t.Errorf("Type = %q want book", res.Type)
	}

	// Clearing: empty type id removes the type line; file still readable.
	if _, err := bridge.SetPageType(context.Background(), "Books", "", "Dune", ""); err != nil {
		t.Fatalf("SetPageType(''): %v", err)
	}
	res2, _ := bridge.GetPageMetadata(context.Background(), "Books", "", "Dune")
	if res2.Type != "" {
		t.Errorf("Type = %q want empty after clear", res2.Type)
	}
	if len(res2.Properties) != 0 {
		t.Errorf("Properties = %+v, want empty after clear", res2.Properties)
	}
	// Other frontmatter keys survive the clear.
	if res2.Frontmatter["title"] != "Dune" {
		t.Errorf("title lost after clear: %v", res2.Frontmatter["title"])
	}

	// Unknown type → rejected, file untouched.
	before, _ := os.ReadFile(filePath)
	if _, err := bridge.SetPageType(context.Background(), "Books", "", "Dune", "no-such-type"); err == nil {
		t.Fatal("expected error for unknown type")
	}
	after, _ := os.ReadFile(filePath)
	if string(after) != string(before) {
		t.Errorf("file mutated despite unknown type rejection.\nbefore:\n%s\nafter:\n%s",
			string(before), string(after))
	}
}

// TestMCPBridge_SetPageType_KeepAndFlag verifies the bridge surfaces the
// keep-and-flag list: switching to a new schema whose declared properties
// clash with the page's existing values returns those names (the values stay
// on disk unchanged). Drives the path the set_page_type tool surfaces as
// `flagged` in its success response.
func TestMCPBridge_SetPageType_KeepAndFlag(t *testing.T) {
	app := newTestApp(t)
	bridge := newMetaBridge(app)
	_, _ = writeBookPageForMCP(t, app)

	// meeting declares `status` as a number; the book page has status
	// "available" (a string), so status gets flagged. The other book
	// properties (title) are not meeting properties, so they are not checked.
	if err := app.SaveType(types.TypeDef{
		ID:   "meeting",
		Name: "Meeting",
		Properties: []types.PropertyDef{
			{Name: "attendees", Type: types.PropText},
			{Name: "status", Type: types.PropNumber},
		},
	}); err != nil {
		t.Fatalf("SaveType(meeting): %v", err)
	}

	flagged, err := bridge.SetPageType(context.Background(), "Books", "", "Dune", "meeting")
	if err != nil {
		t.Fatalf("SetPageType(meeting): %v", err)
	}
	if len(flagged) != 1 || flagged[0] != "status" {
		t.Errorf("flagged = %v, want [status]", flagged)
	}

	// The type id was still written; only the value-shape mismatch was flagged.
	res, _ := bridge.GetPageMetadata(context.Background(), "Books", "", "Dune")
	if res.Type != "meeting" {
		t.Errorf("Type = %q want meeting", res.Type)
	}
}
