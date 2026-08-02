package types

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"silt/backend/safeio"
)

// maxTypeFileBytes bounds an on-disk type file before it is parsed. Caps the
// whole-file read so yaml.Unmarshal cannot be fed unbounded bytes by a hostile
// synced type file. Mirrors templates.maxTemplateFileBytes.
const maxTypeFileBytes int64 = 64 << 10 // 64 KB

// ErrTypeNotFound is returned (wrapped) by GetType/ResolveTypeID when no type
// matches. Callers use errors.Is to distinguish "not found" from I/O errors.
var ErrTypeNotFound = errors.New("type not found")

// TypeLoadError records a single type file that could not be loaded, so
// ListTypes can surface broken files to the UI without dropping them silently or
// aborting the enumeration. Mirrors templates.TemplateLoadError.
type TypeLoadError struct {
	File    string `json:"file"`
	Message string `json:"message"`
}

// ListTypesResult is returned by ListTypes: every valid type plus any per-file
// load errors (so the type manager can name the broken file) and forward-compat
// warnings.
type ListTypesResult struct {
	Types    []TypeDef       `json:"types"`
	Errors   []TypeLoadError `json:"errors"`
	Warnings []TypeLoadError `json:"warnings"`
}

// ParseTypeBytes parses raw type YAML (filename is used to derive the id and
// for error messages). The id is the filename stem — never taken from the YAML
// body — so a file rename is the canonical rename and the on-disk body stays
// purely declarative. A missing Name defaults to a title-cased id so a minimal
// one-line file still loads. Returns a wrapped ValidationErrors when the parsed
// type is structurally invalid.
func ParseTypeBytes(raw []byte, filename string) (*TypeDef, error) {
	var td TypeDef
	if err := yaml.Unmarshal(raw, &td); err != nil {
		return nil, fmt.Errorf("invalid type yaml in %s: %w", filename, err)
	}
	td.ID = strings.TrimSuffix(filename, filepath.Ext(filename))
	if td.Name == "" {
		td.Name = titleFromID(td.ID)
	}
	if err := ValidateTypeDef(&td); err != nil {
		return nil, fmt.Errorf("type %s is invalid: %w", filename, err)
	}
	return &td, nil
}

// titleFromID turns a kebab-case id ("book-notes") into a display name
// ("Book notes") for the Name-default fallback only. Authored names always win.
func titleFromID(id string) string {
	if id == "" {
		return ""
	}
	words := strings.Split(id, "-")
	for i, w := range words {
		if w == "" {
			continue
		}
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}

// loadOne reads and parses a single on-disk type file, returning the raw bytes
// alongside the parsed type so callers (ListTypes) can run diagnostic passes
// without re-reading the file. Mirrors templates.loadOne.
func loadOne(path string) (*TypeDef, []byte, error) {
	raw, err := safeio.ReadFileMax(path, maxTypeFileBytes)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read type %s: %w", filepath.Base(path), err)
	}
	td, err := ParseTypeBytes(raw, filepath.Base(path))
	if err != nil {
		return nil, nil, err
	}
	return td, raw, nil
}

// detectUnknownTypeKeys warns about top-level keys (and property-map keys)
// that yaml.Unmarshal silently ignores. A typo like `propertis:` would
// otherwise yield an empty-schema type with no diagnostic. Best-effort: a
// map parse failure here is silent because ParseTypeBytes already accepted
// the same bytes. KnownFields(true) is deliberately avoided so forward-compat
// with newer Silt type files (extra keys) does not turn into hard errors.
func detectUnknownTypeKeys(raw []byte, filename string) []TypeLoadError {
	var root map[string]any
	if err := yaml.Unmarshal(raw, &root); err != nil {
		return nil
	}
	knownTypeDef := map[string]bool{
		"name": true, "description": true, "icon": true,
		"heroField": true, "properties": true,
	}
	knownProp := map[string]bool{
		"name": true, "label": true, "type": true, "required": true,
		"options": true, "default": true, "min": true, "max": true,
		"target": true, "cardinality": true, "description": true,
	}
	var msgs []string
	for k := range root {
		if !knownTypeDef[k] {
			msgs = append(msgs, fmt.Sprintf("%s: unknown key %q ignored", filename, k))
		}
	}
	if props, ok := root["properties"].([]any); ok {
		for i, p := range props {
			pm, ok := p.(map[string]any)
			if !ok {
				continue
			}
			for k := range pm {
				if !knownProp[k] {
					msgs = append(msgs, fmt.Sprintf("%s: unknown key %q in properties[%d] ignored", filename, k, i))
				}
			}
		}
	}
	sort.Strings(msgs)
	warns := make([]TypeLoadError, len(msgs))
	for i, m := range msgs {
		warns[i] = TypeLoadError{File: filename, Message: m}
	}
	return warns
}

// ListTypes enumerates <typesDir>/*.yaml + *.yml, returning every valid type.
// Invalid files are collected into Errors (never panic); a missing typesDir is
// not an error — it yields an empty result (a fresh vault has no types yet).
// Types are deduped by ID (first valid definition wins) and sorted by (Name, ID)
// for stable UI ordering. Mirrors templates.ListTemplates.
func ListTypes(typesDir string) (*ListTypesResult, error) {
	res := &ListTypesResult{
		Types:    []TypeDef{},
		Errors:   []TypeLoadError{},
		Warnings: []TypeLoadError{},
	}
	if typesDir == "" {
		return res, nil
	}
	seen := map[string]bool{}

	entries, err := os.ReadDir(typesDir)
	if err != nil {
		// A missing dir is expected (fresh/empty vault) and is not an error;
		// a real I/O error (permissions) is surfaced.
		if os.IsNotExist(err) {
			return res, nil
		}
		return nil, fmt.Errorf("failed to read types directory %s: %w", typesDir, err)
	}

	// Sort entries by name so "first valid definition wins" is deterministic
	// across platforms with different readdir orders.
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if ext != ".yaml" && ext != ".yml" {
			continue
		}
		full := filepath.Join(typesDir, e.Name())
		td, raw, loadErr := loadOne(full)
		if loadErr != nil {
			res.Errors = append(res.Errors, TypeLoadError{
				File:    e.Name(),
				Message: loadErr.Error(),
			})
			continue
		}
		res.Warnings = append(res.Warnings, detectUnknownTypeKeys(raw, e.Name())...)
		id := strings.ToLower(td.ID)
		if seen[id] {
			continue // first valid definition of an id wins
		}
		seen[id] = true
		res.Types = append(res.Types, *td)
	}

	sort.Slice(res.Types, func(i, j int) bool {
		if res.Types[i].Name != res.Types[j].Name {
			return res.Types[i].Name < res.Types[j].Name
		}
		return res.Types[i].ID < res.Types[j].ID
	})
	return res, nil
}

// GetType resolves a single type by id via an O(1) direct file lookup
// (<id>.yaml then <id>.yml), mirroring templates.GetTemplate's direct-lookup
// contract. Returns ErrTypeNotFound (wrapped) when the id is absent; genuine
// I/O errors propagate.
func GetType(typesDir, id string) (*TypeDef, error) {
	if id == "" {
		return nil, fmt.Errorf("%w: %q", ErrTypeNotFound, id)
	}
	if typesDir != "" {
		for _, ext := range []string{".yaml", ".yml"} {
			path := filepath.Join(typesDir, id+ext)
			td, _, err := loadOne(path)
			if err == nil {
				return td, nil
			}
			if !os.IsNotExist(extractNotExist(err)) {
				return nil, err
			}
		}
	}
	return nil, fmt.Errorf("%w: %q", ErrTypeNotFound, id)
}

// extractNotExist unwraps a fmt.Errorf-wrapped error to find an underlying
// os.PathError/NotExist so GetType can distinguish "file absent" (try next ext
// / not found) from a real I/O failure.
func extractNotExist(err error) error {
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		return pathErr
	}
	// safeio.ReadFileMax wraps with fmt.Errorf; the underlying os error is in
	// the chain, so errors.Is against os.ErrNotExist also works.
	if errors.Is(err, os.ErrNotExist) {
		return os.ErrNotExist
	}
	return err
}

// ResolveTypeID resolves a frontmatter `type:` reference to its canonical id.
// It accepts the id directly (exact, then case-insensitive) OR the type's
// display Name (case-insensitive), so a user may write `type: Book` by name
// while the canonical stored form remains the lowercased id. Returns the
// canonical id, or ErrTypeNotFound (wrapped) when nothing matches.
func ResolveTypeID(typesDir, ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", fmt.Errorf("%w: empty type reference", ErrTypeNotFound)
	}
	res, err := ListTypes(typesDir)
	if err != nil {
		return "", err
	}
	lowerRef := strings.ToLower(ref)
	// Exact id, then case-insensitive id, then case-insensitive name.
	for _, t := range res.Types {
		if t.ID == ref {
			return t.ID, nil
		}
	}
	for _, t := range res.Types {
		if strings.ToLower(t.ID) == lowerRef {
			return t.ID, nil
		}
	}
	for _, t := range res.Types {
		if strings.ToLower(t.Name) == lowerRef {
			return t.ID, nil
		}
	}
	return "", fmt.Errorf("%w: %q", ErrTypeNotFound, ref)
}
