package types

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPickFreePropName(t *testing.T) {
	taken := map[string]bool{"title": true}
	if got := pickFreePropName("created_value", taken); got != "created_value" {
		t.Errorf("free preferred: got %q", got)
	}
	taken["created_value"] = true
	if got := pickFreePropName("created_value", taken); got != "created_value_2" {
		t.Errorf("collision: got %q, want created_value_2", got)
	}
	taken["created_value_2"] = true
	if got := pickFreePropName("created_value", taken); got != "created_value_3" {
		t.Errorf("second collision: got %q, want created_value_3", got)
	}
}

func TestPlanAndApplyReservedRenames(t *testing.T) {
	td := &TypeDef{
		ID:        "book",
		Name:      "Book",
		HeroField: "created",
		Properties: []PropertyDef{
			{Name: "title", Type: PropText},
			{Name: "created", Type: PropDate},
			{Name: "aliases", Type: PropText},
			{Name: "created_value", Type: PropText}, // forces suffix
		},
	}
	planned := planReservedRenames(td)
	if len(planned) != 2 {
		t.Fatalf("planned = %d, want 2: %+v", len(planned), planned)
	}
	byFrom := map[string]string{}
	for _, r := range planned {
		byFrom[r.From] = r.To
	}
	if byFrom["created"] != "created_value_2" {
		t.Errorf("created → %q, want created_value_2", byFrom["created"])
	}
	if byFrom["aliases"] != "aliases_list" {
		t.Errorf("aliases → %q, want aliases_list", byFrom["aliases"])
	}
	applyReservedRenames(td, planned)
	if td.HeroField != "created_value_2" {
		t.Errorf("heroField = %q, want created_value_2", td.HeroField)
	}
	if err := ValidateTypeDef(td); err != nil {
		t.Fatalf("after rename ValidateTypeDef: %v", err)
	}
	names := map[string]bool{}
	for _, p := range td.Properties {
		names[p.Name] = true
	}
	if names["created"] || names["aliases"] {
		t.Errorf("reserved names still present: %v", names)
	}
}

func TestPlanReservedRenames_Noop(t *testing.T) {
	td := &TypeDef{
		Name:       "Book",
		Properties: []PropertyDef{{Name: "title", Type: PropText}},
	}
	if got := planReservedRenames(td); len(got) != 0 {
		t.Errorf("expected no renames, got %+v", got)
	}
}

func TestMigrateReservedPropertyNames_RewritesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "book.yaml")
	src := `# Silt note type
name: Book
heroField: created
properties:
  - name: title
    type: text
  - name: created
    type: date
  - name: aliases
    type: text
`
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	var armed []string
	renames, err := MigrateReservedPropertyNames(dir, func(p string, _ []byte) {
		armed = append(armed, p)
	})
	if err != nil {
		t.Fatalf("MigrateReservedPropertyNames: %v", err)
	}
	if len(renames) != 2 {
		t.Fatalf("renames = %d, want 2: %+v", len(renames), renames)
	}
	if len(armed) != 1 || armed[0] != path {
		t.Errorf("self-write arm = %v, want [%s]", armed, path)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	if strings.Contains(body, "name: created\n") || strings.Contains(body, "name: aliases\n") {
		t.Errorf("reserved names still in file:\n%s", body)
	}
	if !strings.Contains(body, "created_value") || !strings.Contains(body, "aliases_list") {
		t.Errorf("expected renamed properties in file:\n%s", body)
	}
	if !strings.Contains(body, "heroField: created_value") {
		t.Errorf("heroField not updated:\n%s", body)
	}
	// Loads cleanly under full validation.
	if _, err := ParseTypeBytes(raw, "book.yaml"); err != nil {
		t.Fatalf("ParseTypeBytes after migrate: %v", err)
	}
	// Second pass is a no-op.
	again, err := MigrateReservedPropertyNames(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 0 {
		t.Errorf("second pass renames = %+v, want empty", again)
	}
}

func TestMigrateReservedPropertyNames_LeavesOtherInvalidUntouched(t *testing.T) {
	dir := t.TempDir()
	// Invalid type: unknown property type AND reserved prop. After rename the
	// unknown type still fails ValidateTypeDef → file must not be rewritten.
	path := filepath.Join(dir, "broken.yaml")
	src := `name: Broken
properties:
  - name: created
    type: not_a_real_type
`
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	renames, err := MigrateReservedPropertyNames(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(renames) != 0 {
		t.Errorf("expected no successful renames, got %+v", renames)
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(before) {
		t.Errorf("invalid file was rewritten:\nbefore=%q\nafter=%q", before, after)
	}
}

func TestMigrateReservedPropertyNames_MissingDir(t *testing.T) {
	renames, err := MigrateReservedPropertyNames(filepath.Join(t.TempDir(), "nope"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(renames) != 0 {
		t.Errorf("got %+v", renames)
	}
}
