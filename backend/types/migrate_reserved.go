package types

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"silt/backend/parser"
	"silt/backend/safeio"
)

// Newly reserved core-metadata property names introduced with #867/#898.
// Older reserved keys (notebook/section/page/date/tags/type) were already
// enforced before that upgrade cliff and are not migrated here.
var migratableReservedPropNames = map[string]string{
	"created": "created_value",
	"aliases": "aliases_list",
}

// ReservedPropRename records one property rename applied during the #900
// load-time compatibility migration.
type ReservedPropRename struct {
	TypeID   string `json:"type_id"`
	TypeName string `json:"type_name"`
	File     string `json:"file"`
	From     string `json:"from"`
	To       string `json:"to"`
}

// SelfWriteArmer is called before each atomic type-file write so the App can
// suppress the type watcher feedback loop. path is the destination file;
// content is the bytes about to be written (may be nil on delete-style arms).
type SelfWriteArmer func(path string, content []byte)

// pickFreePropName returns preferred when free under taken (case-insensitive),
// otherwise preferred_2, preferred_3, … until free. Never returns a name that
// is still in reservedPropertyNames.
func pickFreePropName(preferred string, taken map[string]bool) string {
	candidate := preferred
	n := 1
	for {
		lower := strings.ToLower(candidate)
		if !taken[lower] && !reservedPropertyNames[lower] && IsValidPropertyName(candidate) {
			return candidate
		}
		n++
		candidate = fmt.Sprintf("%s_%d", preferred, n)
	}
}

// planReservedRenames computes from→to renames for properties whose names are
// in migratableReservedPropNames. Mutates nothing. Returns nil when no
// migration is needed.
func planReservedRenames(td *TypeDef) []ReservedPropRename {
	if td == nil {
		return nil
	}
	taken := map[string]bool{}
	for _, p := range td.Properties {
		if name := strings.ToLower(strings.TrimSpace(p.Name)); name != "" {
			taken[name] = true
		}
	}
	var out []ReservedPropRename
	// Deterministic order: walk properties in schema order.
	for _, p := range td.Properties {
		lower := strings.ToLower(strings.TrimSpace(p.Name))
		preferred, ok := migratableReservedPropNames[lower]
		if !ok {
			continue
		}
		// Free the old name so the preferred target can reuse it only if it
		// differs (it always does for our preferred map).
		delete(taken, lower)
		to := pickFreePropName(preferred, taken)
		taken[strings.ToLower(to)] = true
		out = append(out, ReservedPropRename{
			TypeID:   td.ID,
			TypeName: td.Name,
			From:     p.Name,
			To:       to,
		})
	}
	return out
}

// applyReservedRenames renames properties on td in place according to renames
// (matched case-insensitively on From). Updates heroField when it referenced
// an old name. Returns false when renames is empty.
func applyReservedRenames(td *TypeDef, renames []ReservedPropRename) bool {
	if td == nil || len(renames) == 0 {
		return false
	}
	byFrom := map[string]string{}
	for _, r := range renames {
		byFrom[strings.ToLower(r.From)] = r.To
	}
	for i := range td.Properties {
		lower := strings.ToLower(td.Properties[i].Name)
		if to, ok := byFrom[lower]; ok {
			td.Properties[i].Name = to
		}
	}
	if td.HeroField != "" {
		if to, ok := byFrom[strings.ToLower(td.HeroField)]; ok {
			td.HeroField = to
		}
	}
	return true
}

// parseTypeBytesUnvalidated unmarshals a type file the same way ParseTypeBytes
// does (id from filename, default Name) but skips ValidateTypeDef so a legacy
// reserved-name collision can be inspected and repaired.
func parseTypeBytesUnvalidated(raw []byte, filename string) (*TypeDef, error) {
	var td TypeDef
	if err := yaml.Unmarshal(raw, &td); err != nil {
		return nil, fmt.Errorf("invalid type yaml in %s: %w", filename, err)
	}
	td.ID = strings.ToLower(strings.TrimSuffix(filename, filepath.Ext(filename)))
	if td.Name == "" {
		td.Name = titleFromID(td.ID)
	}
	return &td, nil
}

// MigrateReservedPropertyNames scans typesDir for type schemas that declare
// `created` or `aliases` as property names (the #898 reservation cliff),
// renames those properties to free non-reserved names, and atomic-writes the
// repaired YAML. armSelfWrite is optional (nil-safe) and is invoked before
// each write.
//
// Only the two newly reserved names are migrated. Types with other validation
// errors are left untouched so they continue to surface as load errors.
// Idempotent by content: a clean types dir yields an empty result.
func MigrateReservedPropertyNames(typesDir string, armSelfWrite SelfWriteArmer) ([]ReservedPropRename, error) {
	if typesDir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(typesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read types directory %s: %w", typesDir, err)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	var all []ReservedPropRename
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if ext != ".yaml" && ext != ".yml" {
			continue
		}
		full := filepath.Join(typesDir, e.Name())
		raw, rerr := safeio.ReadFileMax(full, maxTypeFileBytes)
		if rerr != nil {
			// Missing mid-scan is fine; real I/O errors skip this file (will
			// still surface on ListTypes).
			log.Printf("types: reserved-prop migrate: skip read %s: %v", e.Name(), rerr)
			continue
		}
		td, perr := parseTypeBytesUnvalidated(raw, e.Name())
		if perr != nil {
			log.Printf("types: reserved-prop migrate: skip parse %s: %v", e.Name(), perr)
			continue
		}
		planned := planReservedRenames(td)
		if len(planned) == 0 {
			continue
		}
		for i := range planned {
			planned[i].File = e.Name()
			planned[i].TypeID = td.ID
			planned[i].TypeName = td.Name
		}
		applyReservedRenames(td, planned)
		// Full validation after rename — refuse to write a still-invalid schema
		// (other structural errors must remain visible as load failures).
		if verr := ValidateTypeDef(td); verr != nil {
			log.Printf("types: reserved-prop migrate: %s still invalid after rename: %v", e.Name(), verr)
			continue
		}
		out := SerializeType(td)
		if armSelfWrite != nil {
			armSelfWrite(full, out)
		}
		if werr := parser.WriteFileAtomic(full, out); werr != nil {
			log.Printf("types: reserved-prop migrate: write %s failed: %v", e.Name(), werr)
			continue
		}
		log.Printf("types: reserved-prop migrate: repaired %s (%d rename(s))", e.Name(), len(planned))
		all = append(all, planned...)
	}
	return all, nil
}

// LookupFrontmatterValue returns the frontmatter value for key (case-insensitive
// top-level) when present. Used by the App page-rewrite pass.
func LookupFrontmatterValue(fm map[string]any, key string) (any, bool) {
	if fm == nil {
		return nil, false
	}
	if v, ok := fm[key]; ok {
		return v, true
	}
	lower := strings.ToLower(key)
	for k, v := range fm {
		if strings.ToLower(k) == lower {
			return v, true
		}
	}
	return nil, false
}
