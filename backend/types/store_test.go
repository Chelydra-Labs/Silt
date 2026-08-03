package types

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTypeIDFromName(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"Book", "book"},
		{"My Book", "my-book"},
		{"My  Fancy -- Type!", "my-fancy-type"},
		{"ALLCAPS", "allcaps"},
		{"   ", "type"},
		{"!!!", "type"},
		{"already_kebab", "already_kebab"}, // underscore is a valid id char, preserved
	}
	for _, c := range cases {
		got := TypeIDFromName(c.name)
		if got != c.want {
			t.Errorf("TypeIDFromName(%q) = %q, want %q", c.name, got, c.want)
		}
		if !IsValidTypeID(got) {
			t.Errorf("TypeIDFromName(%q) produced invalid id %q", c.name, got)
		}
	}
}

func TestSaveType_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	td := &TypeDef{
		ID:        "book",
		Name:      "Book",
		HeroField: "title",
		Properties: []PropertyDef{
			{Name: "title", Type: PropText, Required: true},
			{Name: "rating", Type: PropNumber},
		},
	}
	if err := SaveType(dir, td); err != nil {
		t.Fatalf("SaveType: %v", err)
	}

	// The canonical file is <id>.yaml.
	if _, err := os.Stat(filepath.Join(dir, "book.yaml")); err != nil {
		t.Errorf("expected book.yaml on disk: %v", err)
	}

	res, err := ListTypes(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Types) != 1 {
		t.Fatalf("expected 1 type after save, got %d", len(res.Types))
	}
	got := res.Types[0]
	if got.ID != "book" || got.Name != "Book" {
		t.Errorf("round-trip = {ID:%s Name:%s}, want {book Book}", got.ID, got.Name)
	}
	if len(got.Properties) != 2 || got.Properties[0].Name != "title" {
		t.Errorf("round-trip properties mismatch: %+v", got.Properties)
	}
}

func TestSaveType_DerivesIDFromName(t *testing.T) {
	dir := t.TempDir()
	td := &TypeDef{Name: "Meeting Notes", Properties: []PropertyDef{{Name: "x", Type: PropText}}}
	if err := SaveType(dir, td); err != nil {
		t.Fatalf("SaveType: %v", err)
	}
	if td.ID != "meeting-notes" {
		t.Errorf("derived ID = %q, want meeting-notes", td.ID)
	}
	if _, err := os.Stat(filepath.Join(dir, "meeting-notes.yaml")); err != nil {
		t.Errorf("expected meeting-notes.yaml: %v", err)
	}
}

func TestSaveType_RejectsInvalid(t *testing.T) {
	dir := t.TempDir()
	// Missing name → ValidateTypeDef fails.
	if err := SaveType(dir, &TypeDef{ID: "bad", Properties: []PropertyDef{{Name: "Bad Name", Type: PropText}}}); err == nil {
		t.Error("expected error for invalid type")
	}
	// Empty dir.
	if err := SaveType("", &TypeDef{Name: "X"}); err == nil {
		t.Error("expected error for empty types dir")
	}
	// Nil.
	if err := SaveType(dir, nil); err == nil {
		t.Error("expected error for nil type")
	}
}

func TestSerializeType_HasCommentAndName(t *testing.T) {
	out := string(SerializeType(&TypeDef{Name: "Book", Properties: []PropertyDef{{Name: "x", Type: PropText}}}))
	if !strings.HasPrefix(out, "# Silt note type") {
		t.Errorf("expected leading comment, got: %s", out)
	}
	if !strings.Contains(out, "name: Book") {
		t.Errorf("expected 'name: Book' in output, got: %s", out)
	}
}

func TestDeleteType_Idempotent(t *testing.T) {
	dir := t.TempDir()
	td := &TypeDef{ID: "book", Name: "Book", Properties: []PropertyDef{{Name: "x", Type: PropText}}}
	if err := SaveType(dir, td); err != nil {
		t.Fatal(err)
	}
	if err := DeleteType(dir, "book"); err != nil {
		t.Errorf("delete existing: %v", err)
	}
	// Deleting again (already gone) is a no-op success.
	if err := DeleteType(dir, "book"); err != nil {
		t.Errorf("delete missing should be idempotent, got %v", err)
	}
}

func TestDeleteType_RejectsBadID(t *testing.T) {
	dir := t.TempDir()
	if err := DeleteType(dir, "../escape"); err == nil {
		t.Error("expected error for path-escape id")
	}
	if err := DeleteType(dir, ""); err == nil {
		t.Error("expected error for empty id")
	}
}

// TestSaveType_DerivedIDCollisionRejected pins the NB-8 contract: two display
// names that sanitize to the same id ("My Book!" / "My--Book" → "my-book") must
// not silently clobber each other when the id is derived. The first file is
// left untouched, and re-saving the same type (same name) still succeeds.
func TestSaveType_DerivedIDCollisionRejected(t *testing.T) {
	dir := t.TempDir()
	first := &TypeDef{Name: "My Book!", Properties: []PropertyDef{{Name: "x", Type: PropText}}}
	if err := SaveType(dir, first); err != nil {
		t.Fatalf("save first: %v", err)
	}
	if first.ID != "my-book" {
		t.Fatalf("first ID = %q, want my-book", first.ID)
	}

	// Different display name, same derived id → collision.
	second := &TypeDef{Name: "My--Book", Properties: []PropertyDef{{Name: "y", Type: PropText}}}
	err := SaveType(dir, second)
	if err == nil {
		t.Fatal("expected collision error for second type, got nil")
	}
	if !strings.Contains(err.Error(), "collides") {
		t.Errorf("unexpected error message: %v", err)
	}
	if second.ID != "my-book" {
		t.Errorf("second ID = %q, want my-book", second.ID)
	}

	// The first type file must be untouched.
	res, err := ListTypes(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Types) != 1 {
		t.Fatalf("expected 1 type after collision, got %d", len(res.Types))
	}
	if res.Types[0].Name != "My Book!" {
		t.Errorf("existing type Name = %q, want %q (clobbered)", res.Types[0].Name, "My Book!")
	}

	// Re-saving the same type (same name, same derived id) still succeeds —
	// legitimate idempotent update, not a collision.
	if err := SaveType(dir, &TypeDef{Name: "My Book!", Properties: []PropertyDef{{Name: "x", Type: PropText}}}); err != nil {
		t.Errorf("re-saving same name should succeed, got: %v", err)
	}
}

// TestSaveType_ExplicitIDOverwritesDifferentName confirms an explicit td.ID is
// treated as an intentional update of that file: renaming via the same id must
// not trip the collision guard.
func TestSaveType_ExplicitIDOverwritesDifferentName(t *testing.T) {
	dir := t.TempDir()
	if err := SaveType(dir, &TypeDef{ID: "book", Name: "Book", Properties: []PropertyDef{{Name: "x", Type: PropText}}}); err != nil {
		t.Fatalf("save: %v", err)
	}
	// Explicit same id, different Name → intentional update, must succeed.
	if err := SaveType(dir, &TypeDef{ID: "book", Name: "Novel", Properties: []PropertyDef{{Name: "x", Type: PropText}}}); err != nil {
		t.Errorf("explicit-id rename should succeed, got: %v", err)
	}
	res, err := ListTypes(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Types) != 1 || res.Types[0].Name != "Novel" {
		t.Errorf("expected single type renamed to Novel, got %+v", res.Types)
	}
}
