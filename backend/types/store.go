package types

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"silt/backend/parser"
)

// idSanitizeRe matches any character that is NOT allowed in a type id. Used by
// TypeIDFromName to derive a filename-safe id from a human display name.
var idSanitizeRe = regexp.MustCompile(`[^a-z0-9_-]+`)

// TypeIDFromName derives a valid type id from a human display name: lowercase,
// replace every disallowed run with a single "-", trim/collapse hyphens. An
// all-symbol name collapses to the fallback "type" so SaveType never produces an
// empty or invalid id.
func TypeIDFromName(name string) string {
	id := idSanitizeRe.ReplaceAllString(strings.ToLower(strings.TrimSpace(name)), "-")
	id = strings.Trim(id, "-")
	// Collapse runs of hyphens left by adjacent disallowed chars.
	id = collapseHyphens(id)
	if id == "" || !IsValidTypeID(id) {
		return "type"
	}
	return id
}

func collapseHyphens(s string) string {
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	return s
}

// SerializeType re-emits a type in its canonical on-disk form: a single YAML
// document (the schema fields; ID omitted via yaml:"-"). This is what SaveType
// writes atomically, so a re-saved type round-trips through ParseTypeBytes.
func SerializeType(td *TypeDef) []byte {
	if td == nil {
		return []byte("name: \"\"\n")
	}
	out, err := yaml.Marshal(td)
	if err != nil {
		// A marshal failure here can only mean the struct is unrepresentable,
		// which is a programmer error. Fall back rather than crash.
		return []byte(fmt.Sprintf("name: %q\n", td.Name))
	}
	// A leading comment documents the file's role for users who open it raw;
	// it is intentionally the only comment SerializeType emits.
	return []byte("# Silt note type — see docs. Edit the fields below.\n" + string(out))
}

// SaveType validates td, derives an id when one is not set, and writes the
// canonical form atomically to <typesDir>/<id>.yaml. The atomic write (temp +
// rename) guarantees the file is either the previous version or the new one in
// full, never a half-written file — the same durability guarantee as every
// other writer in Silt.
//
// Self-write suppression (so the file watcher does not reload on Silt's own
// save) is the caller's responsibility: the App calls RegisterSelfWrite before
// this function, mirroring how SaveTemplate interacts with the template
// watcher. The package stays decoupled from the watcher to avoid an import
// cycle. Mirrors templates.SaveTemplate.
func SaveType(typesDir string, td *TypeDef) error {
	if typesDir == "" {
		return fmt.Errorf("types directory is empty (vault not loaded)")
	}
	if td == nil {
		return fmt.Errorf("type is nil")
	}
	explicitID := strings.TrimSpace(td.ID) != ""
	if !explicitID {
		td.ID = TypeIDFromName(td.Name)
	}
	if !IsValidTypeID(td.ID) {
		return fmt.Errorf("invalid type id %q (must be lowercase [a-z0-9_-])", td.ID)
	}
	if err := ValidateTypeDef(td); err != nil {
		return err
	}
	if err := os.MkdirAll(typesDir, 0o700); err != nil {
		return fmt.Errorf("failed to ensure types dir %s: %w", typesDir, err)
	}
	dst := filepath.Join(typesDir, td.ID+".yaml")
	// Two display names can sanitize to the same id ("My Book!" / "My--Book"
	// → "my-book"). Reject a derived id that would silently clobber an
	// unrelated type; an explicit td.ID is an intentional update of that file.
	if !explicitID {
		if existing, _, perr := loadOne(dst); perr == nil && existing.Name != td.Name {
			return fmt.Errorf("type id %q collides with existing type %q; choose a different name", td.ID, existing.Name)
		}
	}
	if err := parser.WriteFileAtomic(dst, SerializeType(td)); err != nil {
		return fmt.Errorf("failed to write type file: %w", err)
	}
	return nil
}

// DeleteType removes the on-disk type with the given id. A missing file is not
// an error — deleting an already-deleted type is a no-op success (idempotent).
// Only the canonical .yaml variant is removed; a stray .yml is left to the user
// to reconcile. Mirrors templates.DeleteTemplate.
func DeleteType(typesDir, id string) error {
	if typesDir == "" {
		return fmt.Errorf("types directory is empty (vault not loaded)")
	}
	if id == "" {
		return fmt.Errorf("type id is required")
	}
	if !IsValidTypeID(id) {
		return fmt.Errorf("invalid type id %q", id)
	}
	dst := filepath.Join(typesDir, id+".yaml")
	if err := os.Remove(dst); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete type %q: %w", id, err)
	}
	return nil
}
