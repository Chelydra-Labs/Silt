package types

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTypeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

const bookYAML = `name: Book
description: A reading note
heroField: title
properties:
  - name: title
    type: text
    required: true
  - name: author
    type: page
    target: person
  - name: status
    type: select
    options: [todo, reading, done]
  - name: rating
    type: number
    min: 0
    max: 5
`

const personYAML = `name: Person
properties:
  - name: email
    type: text
`

func TestParseTypeBytes_Good(t *testing.T) {
	td, err := ParseTypeBytes([]byte(bookYAML), "book.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if td.ID != "book" {
		t.Errorf("ID = %q, want book", td.ID)
	}
	if td.Name != "Book" {
		t.Errorf("Name = %q, want Book", td.Name)
	}
	if len(td.Properties) != 4 {
		t.Fatalf("got %d properties, want 4", len(td.Properties))
	}
	if td.Properties[1].Target != "person" {
		t.Errorf("author target = %q, want person", td.Properties[1].Target)
	}
}

func TestParseTypeBytes_BadYAML(t *testing.T) {
	if _, err := ParseTypeBytes([]byte("name: [unclosed\n"), "bad.yaml"); err == nil {
		t.Fatal("expected error for malformed yaml")
	}
}

func TestParseTypeBytes_NameDefault(t *testing.T) {
	td, err := ParseTypeBytes([]byte("properties:\n  - name: x\n    type: text\n"), "meeting-notes.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if td.Name != "Meeting Notes" {
		t.Errorf("default Name = %q, want 'Meeting Notes'", td.Name)
	}
}

func TestListTypes_Multiple(t *testing.T) {
	dir := t.TempDir()
	writeTypeFile(t, dir, "book.yaml", bookYAML)
	writeTypeFile(t, dir, "person.yaml", personYAML)

	res, err := ListTypes(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Types) != 2 {
		t.Fatalf("got %d types, want 2", len(res.Types))
	}
	// Sorted by Name: Book, Person.
	if res.Types[0].Name != "Book" || res.Types[1].Name != "Person" {
		t.Errorf("order = %s, %s; want Book, Person", res.Types[0].Name, res.Types[1].Name)
	}
	if len(res.Errors) != 0 {
		t.Errorf("unexpected errors: %v", res.Errors)
	}
}

func TestListTypes_MalformedSoftFail(t *testing.T) {
	dir := t.TempDir()
	writeTypeFile(t, dir, "book.yaml", bookYAML)
	writeTypeFile(t, dir, "broken.yaml", "name: [unclosed\n")

	res, err := ListTypes(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Types) != 1 {
		t.Fatalf("good type should still load; got %d types", len(res.Types))
	}
	if len(res.Errors) != 1 || res.Errors[0].File != "broken.yaml" {
		t.Fatalf("expected one error for broken.yaml, got %v", res.Errors)
	}
}

func TestListTypes_UnknownKeyWarning(t *testing.T) {
	// A typo like `propertis:` is silently ignored by yaml.Unmarshal; the
	// loader should still load the type (with empty properties) AND emit a
	// warning so the typo is visible.
	dir := t.TempDir()
	writeTypeFile(t, dir, "typo.yaml", "name: Typo\npropertis:\n  - name: x\n    type: text\n")

	res, err := ListTypes(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Types) != 1 {
		t.Fatalf("type should still load with empty properties; got %d types and errs=%v", len(res.Types), res.Errors)
	}
	if len(res.Warnings) == 0 {
		t.Fatalf("expected a warning for unknown key 'propertis', got none")
	}
	found := false
	for _, w := range res.Warnings {
		if strings.Contains(w.Message, "propertis") {
			found = true
		}
	}
	if !found {
		t.Errorf("warnings did not mention 'propertis': %v", res.Warnings)
	}
}

func TestListTypes_MissingDir(t *testing.T) {
	res, err := ListTypes(filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil {
		t.Fatalf("missing dir should not error, got %v", err)
	}
	if len(res.Types) != 0 {
		t.Errorf("expected empty type list, got %d", len(res.Types))
	}
}

func TestListTypes_EmptyDir(t *testing.T) {
	res, err := ListTypes("")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Types) != 0 {
		t.Errorf("expected empty, got %d", len(res.Types))
	}
}

// TestParseTypeBytes_LowercasesFilenameStem pins load-path id canonicalization:
// a hand-created Meeting.yaml must load as id "meeting" so SaveType/DeleteType
// (which use lowercased ids) hit the same file.
func TestParseTypeBytes_LowercasesFilenameStem(t *testing.T) {
	td, err := ParseTypeBytes([]byte(bookYAML), "Meeting.yaml")
	if err != nil {
		t.Fatalf("ParseTypeBytes: %v", err)
	}
	if td.ID != "meeting" {
		t.Errorf("ID = %q, want meeting", td.ID)
	}
}

func TestGetType(t *testing.T) {
	dir := t.TempDir()
	writeTypeFile(t, dir, "book.yaml", bookYAML)

	td, err := GetType(dir, "book")
	if err != nil {
		t.Fatalf("GetType book: %v", err)
	}
	if td.ID != "book" {
		t.Errorf("ID = %q", td.ID)
	}

	if _, err := GetType(dir, "missing"); !errors.Is(err, ErrTypeNotFound) {
		t.Errorf("missing type err = %v; want ErrTypeNotFound", err)
	}
	if _, err := GetType(dir, ""); !errors.Is(err, ErrTypeNotFound) {
		t.Errorf("empty id err = %v; want ErrTypeNotFound", err)
	}
}

// TestGetType_BrokenYAMLFallsThroughToYML pins the extension fallback: a
// broken canonical .yaml must not mask a valid sibling .yml for the same id.
func TestGetType_BrokenYAMLFallsThroughToYML(t *testing.T) {
	dir := t.TempDir()
	writeTypeFile(t, dir, "book.yaml", "name: [unclosed\n")
	writeTypeFile(t, dir, "book.yml", bookYAML)

	td, err := GetType(dir, "book")
	if err != nil {
		t.Fatalf("GetType should load .yml after broken .yaml: %v", err)
	}
	if td.ID != "book" || td.Name != "Book" {
		t.Errorf("got id=%q name=%q, want book/Book", td.ID, td.Name)
	}
}

func TestGetType_TraversalRejected(t *testing.T) {
	// typesDir lives under a parent; a canary type file sits OUTSIDE typesDir
	// where an unsanitized filepath.Join("../parent/canary") would reach it.
	// IsValidTypeID must reject the traversal id before any filesystem access,
	// so the canary is never read and the attempt looks like any other miss.
	root := t.TempDir()
	typesDir := filepath.Join(root, "types")
	writeTypeFile(t, typesDir, "book.yaml", bookYAML)
	writeTypeFile(t, root, "canary.yaml", personYAML) // reachable via "../canary"

	for _, bad := range []string{
		"../canary",        // parent escape, would hit the canary
		"../../etc/passwd", // classic absolute escape
		"..",
		"bad/id",
	} {
		t.Run(bad, func(t *testing.T) {
			td, err := GetType(typesDir, bad)
			if !errors.Is(err, ErrTypeNotFound) {
				t.Errorf("GetType(%q) err = %v; want ErrTypeNotFound", bad, err)
			}
			if td != nil {
				t.Errorf("GetType(%q) returned a type from outside typesDir: %+v", bad, td)
			}
		})
	}
}

func TestResolveTypeID(t *testing.T) {
	ResetCacheForTests()
	dir := t.TempDir()
	writeTypeFile(t, dir, "book.yaml", bookYAML)
	writeTypeFile(t, dir, "person.yaml", personYAML)

	cases := []struct {
		ref  string
		want string
		ok   bool
	}{
		{"book", "book", true},     // exact id
		{"BOOK", "book", true},     // case-insensitive id
		{"Book", "book", true},     // display name
		{"PERSON", "person", true}, // case-insensitive name
		{"nope", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		t.Run(c.ref, func(t *testing.T) {
			got, err := ResolveTypeID(dir, c.ref)
			if c.ok {
				if err != nil {
					t.Fatalf("ResolveTypeID(%q) err = %v", c.ref, err)
				}
				if got != c.want {
					t.Errorf("ResolveTypeID(%q) = %q, want %q", c.ref, got, c.want)
				}
			} else if !errors.Is(err, ErrTypeNotFound) {
				t.Errorf("ResolveTypeID(%q) err = %v; want ErrTypeNotFound", c.ref, err)
			}
		})
	}

	// ResolveTypeID routes through CachedListTypes so reproject loops do not
	// re-read every type file per page.
	globalTypeCache.mu.RLock()
	_, ok := globalTypeCache.entries[dir]
	globalTypeCache.mu.RUnlock()
	if !ok {
		t.Fatal("expected ResolveTypeID to populate the types cache")
	}
}
