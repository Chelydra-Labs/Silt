package main

import (
	"fmt"
	"path/filepath"
	"testing"

	"silt/backend/parser"
	"silt/backend/types"
)

// dashboardBookSchema is the per-type-dashboard test schema: title/author text,
// status select (todo/done), and a numeric rating that some pages leave unset so
// the filter and sort paths exercise both the set and unset branches. Distinct
// from bookTypeSchema so the two test files do not collide on the shared helper.
func dashboardBookSchema() types.TypeDef {
	return types.TypeDef{
		ID:   "book",
		Name: "Book",
		Properties: []types.PropertyDef{
			{Name: "title", Type: types.PropText},
			{Name: "author", Type: types.PropText},
			{Name: "status", Type: types.PropSelect, Options: []string{"todo", "done"}},
			{Name: "rating", Type: types.PropNumber},
		},
	}
}

// writeDashboardBook stages a typed book page and runs the full parse → index
// blocks → project-type chain, mirroring the production write path so the
// projection rows (with their numeric ValueSort coercion) mirror what a real
// save produces. rating == nil leaves the rating unset.
func writeDashboardBook(t *testing.T, app *App, page, author, status string, rating any) {
	t.Helper()
	content := "---\n" +
		"notebook: \"Books\"\n" +
		"section: \"\"\n" +
		"page: \"" + page + "\"\n" +
		"date: \"2026-08-01\"\n" +
		"tags: []\n" +
		"type: \"book\"\n" +
		"title: \"" + page + "\"\n" +
		"author: \"" + author + "\"\n" +
		"status: \"" + status + "\"\n"
	if rating != nil {
		content += fmt.Sprintf("rating: %v\n", rating)
	}
	content += "---\n# " + page + "\n"
	filePath := filepath.Join(app.vaultPath, "Books", page+".md")
	writeFile(t, filePath, content)
	indexDashboardBook(t, app, content, page)
}

// indexDashboardBook parses a written book page and feeds it through block
// indexing + projectPageType, which is the same chain the write-path methods use
// after a frontmatter edit.
func indexDashboardBook(t *testing.T, app *App, content, page string) {
	t.Helper()
	blocks, meta, _, _, err := parser.ParseFileContent(content, "Books", "", page, "2026-08-01", app.spacesPerTab)
	if err != nil {
		t.Fatalf("ParseFileContent(%s): %v", page, err)
	}
	if err := app.db.IndexFileBlocks("vault", meta.Notebook, meta.Section, meta.Page, blocks, meta.Tags); err != nil {
		t.Fatalf("IndexFileBlocks(%s): %v", page, err)
	}
	app.projectPageType("vault", meta)
}

// seedDashboardBooks populates a fresh vault with four book pages whose values
// exercise the dashboard's filter and sort paths:
//   - Alpha: done, rating 5
//   - Beta:  todo, rating 3
//   - Gamma: done, rating 10 (lexicographically "10" < "3" < "5", so an
//     accidental ValueText sort would mis-order; the numeric ValueSort keeps
//     3 < 5 < 10)
//   - Delta: todo, rating unset
func seedDashboardBooks(t *testing.T, app *App) {
	t.Helper()
	if err := app.SaveType(dashboardBookSchema()); err != nil {
		t.Fatalf("SaveType(book): %v", err)
	}
	writeDashboardBook(t, app, "Alpha", "Author A", "done", 5)
	writeDashboardBook(t, app, "Beta", "Author B", "todo", 3)
	writeDashboardBook(t, app, "Gamma", "Author C", "done", 10)
	writeDashboardBook(t, app, "Delta", "Author D", "todo", nil)
}

func pageNames(rows []TypeDashboardRow) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Page)
	}
	return out
}

func ratingText(rows []TypeDashboardRow, page string) (string, bool) {
	for _, r := range rows {
		if r.Page != page {
			continue
		}
		for _, p := range r.Properties {
			if p.Name == "rating" {
				return p.ValueText, true
			}
		}
		return "", true // page present, rating unset → empty
	}
	return "", false
}

func TestQueryPagesByType_AllSortedByPath(t *testing.T) {
	app := newTestApp(t)
	seedDashboardBooks(t, app)

	rows, err := app.QueryPagesByType("book", nil, "", false)
	if err != nil {
		t.Fatalf("QueryPagesByType: %v", err)
	}
	if got, want := pageNames(rows), []string{"Alpha", "Beta", "Delta", "Gamma"}; !equalStringSlices(got, want) {
		t.Errorf("path order = %v, want %v", got, want)
	}
	for _, r := range rows {
		// ValueSort is dropped from the IPC result; the frontend sees only the
		// human value + type.
		for _, p := range r.Properties {
			if p.Name == "rating" && p.ValueType != "number" {
				t.Errorf("rating valueType = %q, want number", p.ValueType)
			}
		}
	}
}

func TestQueryPagesByType_FilterByStatus(t *testing.T) {
	app := newTestApp(t)
	seedDashboardBooks(t, app)

	done, err := app.QueryPagesByType("book", map[string]string{"status": "done"}, "", false)
	if err != nil {
		t.Fatalf("QueryPagesByType(done): %v", err)
	}
	if got, want := pageNames(done), []string{"Alpha", "Gamma"}; !equalStringSlices(got, want) {
		t.Errorf("status=done rows = %v, want %v", got, want)
	}

	todo, err := app.QueryPagesByType("book", map[string]string{"status": "todo"}, "", false)
	if err != nil {
		t.Fatalf("QueryPagesByType(todo): %v", err)
	}
	if got, want := pageNames(todo), []string{"Beta", "Delta"}; !equalStringSlices(got, want) {
		t.Errorf("status=todo rows = %v, want %v", got, want)
	}
}

func TestQueryPagesByType_SortByRatingNegative(t *testing.T) {
	app := newTestApp(t)
	// Unique type id so shared in-memory DB pollution from other book tests
	// cannot leak into this ordering assertion.
	schema := types.TypeDef{
		ID:   "negsort",
		Name: "NegSort",
		Properties: []types.PropertyDef{
			{Name: "rating", Type: types.PropNumber},
		},
	}
	if err := app.SaveType(schema); err != nil {
		t.Fatalf("SaveType: %v", err)
	}
	writeTypedDashboardPage := func(page string, rating float64) {
		t.Helper()
		content := "---\n" +
			"notebook: \"Books\"\nsection: \"\"\npage: \"" + page + "\"\n" +
			"date: \"2026-08-01\"\ntags: []\ntype: \"negsort\"\n" +
			fmt.Sprintf("rating: %v\n", rating) +
			"---\n# " + page + "\n"
		filePath := filepath.Join(app.vaultPath, "Books", page+".md")
		writeFile(t, filePath, content)
		blocks, meta, _, _, err := parser.ParseFileContent(content, "Books", "", page, "2026-08-01", app.spacesPerTab)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if err := app.db.IndexFileBlocks("vault", meta.Notebook, meta.Section, meta.Page, blocks, meta.Tags); err != nil {
			t.Fatalf("index: %v", err)
		}
		app.projectPageType("vault", meta)
	}
	// Numeric ascending must be -1.5 < -1.2 < 0.5 (plain %020.6f reverses negatives).
	writeTypedDashboardPage("NegA", -1.2)
	writeTypedDashboardPage("NegB", -1.5)
	writeTypedDashboardPage("Pos", 0.5)

	rows, err := app.QueryPagesByType("negsort", nil, "rating", false)
	if err != nil {
		t.Fatalf("QueryPagesByType: %v", err)
	}
	got := pageNames(rows)
	want := []string{"NegB", "NegA", "Pos"}
	if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("sort by negative rating = %v, want %v", got, want)
	}
}

func TestQueryPagesByType_ResolvesDisplayName(t *testing.T) {
	app := newTestApp(t)
	// Unique type so shared-DB book rows from other tests do not inflate the count.
	schema := types.TypeDef{
		ID:   "dispname",
		Name: "Display Name Type",
		Properties: []types.PropertyDef{
			{Name: "title", Type: types.PropText},
		},
	}
	if err := app.SaveType(schema); err != nil {
		t.Fatalf("SaveType: %v", err)
	}
	content := "---\nnotebook: \"Books\"\nsection: \"\"\npage: \"Only\"\n" +
		"date: \"2026-08-01\"\ntags: []\ntype: \"dispname\"\ntitle: \"Only\"\n---\n# Only\n"
	writeFile(t, filepath.Join(app.vaultPath, "Books", "Only.md"), content)
	blocks, meta, _, _, err := parser.ParseFileContent(content, "Books", "", "Only", "2026-08-01", app.spacesPerTab)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := app.db.IndexFileBlocks("vault", meta.Notebook, meta.Section, meta.Page, blocks, meta.Tags); err != nil {
		t.Fatalf("index: %v", err)
	}
	app.projectPageType("vault", meta)

	// Frontend uses canonical ids; IPC/MCP callers may pass the display name.
	rows, err := app.QueryPagesByType("Display Name Type", nil, "", false)
	if err != nil {
		t.Fatalf("QueryPagesByType(display name): %v", err)
	}
	if len(rows) != 1 || rows[0].Page != "Only" {
		t.Fatalf("QueryPagesByType(display name) = %+v, want 1 row Only", rows)
	}
}

func TestQueryPagesByType_SortByRatingNumeric(t *testing.T) {
	app := newTestApp(t)
	seedDashboardBooks(t, app)

	asc, err := app.QueryPagesByType("book", nil, "rating", false)
	if err != nil {
		t.Fatalf("QueryPagesByType(asc): %v", err)
	}
	// Unset rating (Delta) sorts as empty → first; then numeric order 3, 5, 10.
	// A ValueText sort would have mis-ordered "10" before "3" before "5".
	if got, want := pageNames(asc), []string{"Delta", "Beta", "Alpha", "Gamma"}; !equalStringSlices(got, want) {
		t.Errorf("rating asc = %v, want %v (numeric ValueSort, not lexicographic ValueText)", got, want)
	}
	// Sanity: the ratings on the sorted rows really are 3 < 5 < 10.
	if r, ok := ratingText(asc, "Beta"); !ok || r != "3" {
		t.Errorf("Beta rating = %q, want 3", r)
	}
	if r, ok := ratingText(asc, "Gamma"); !ok || r != "10" {
		t.Errorf("Gamma rating = %q, want 10", r)
	}

	desc, err := app.QueryPagesByType("book", nil, "rating", true)
	if err != nil {
		t.Fatalf("QueryPagesByType(desc): %v", err)
	}
	// sortDesc reverses: Gamma(10), Alpha(5), Beta(3), Delta(unset).
	if got, want := pageNames(desc), []string{"Gamma", "Alpha", "Beta", "Delta"}; !equalStringSlices(got, want) {
		t.Errorf("rating desc = %v, want %v", got, want)
	}
}

func TestQueryPagesByType_SortUnsetPropertyConsistent(t *testing.T) {
	app := newTestApp(t)
	seedDashboardBooks(t, app)

	// Sorting by rating puts the unset page (Delta) at a deterministic
	// position: empty ValueSort sorts first ascending and last descending.
	asc, err := app.QueryPagesByType("book", nil, "rating", false)
	if err != nil {
		t.Fatalf("QueryPagesByType(asc): %v", err)
	}
	if len(asc) == 0 || asc[0].Page != "Delta" {
		t.Errorf("unset rating should sort first ascending; got %v", pageNames(asc))
	}

	desc, err := app.QueryPagesByType("book", nil, "rating", true)
	if err != nil {
		t.Fatalf("QueryPagesByType(desc): %v", err)
	}
	if len(desc) == 0 || desc[len(desc)-1].Page != "Delta" {
		t.Errorf("unset rating should sort last descending; got %v", pageNames(desc))
	}
}

func TestQueryPagesByType_FilterEmptyMatchesUnset(t *testing.T) {
	app := newTestApp(t)
	seedDashboardBooks(t, app)

	// An empty-string filter value matches pages where the property is unset.
	// Only Delta has no rating.
	unset, err := app.QueryPagesByType("book", map[string]string{"rating": ""}, "", false)
	if err != nil {
		t.Fatalf("QueryPagesByType(rating=\"\"): %v", err)
	}
	if got, want := pageNames(unset), []string{"Delta"}; !equalStringSlices(got, want) {
		t.Errorf("rating=\"\" rows = %v, want %v", got, want)
	}
}

func TestQueryPagesByType_NonexistentTypeIsEmpty(t *testing.T) {
	app := newTestApp(t)
	seedDashboardBooks(t, app)

	rows, err := app.QueryPagesByType("nonexistent", nil, "", false)
	if err != nil {
		t.Fatalf("QueryPagesByType(nonexistent): %v", err)
	}
	if rows == nil {
		t.Error("expected non-nil empty slice for an unknown type, got nil")
	}
	if len(rows) != 0 {
		t.Errorf("expected 0 rows for an unknown type, got %d (%v)", len(rows), pageNames(rows))
	}
}

// equalStringSlices is a small order-sensitive helper so the dashboard tests do
// not pull in reflect.DeepEqual for the common []string comparison.
func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
