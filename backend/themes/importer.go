package themes

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"silt/backend/parser"
	"silt/backend/safeio"
)

// importMu serializes concurrent ImportThemeFromPath calls so the
// namespace-then-write sequence is atomic with respect to other
// imports. Without this, a rapid multi-file drag-drop could race:
// import A checks os.Stat (id not on disk), import B checks os.Stat
// (same id, also not on disk), both proceed to WriteFileAtomic, and
// one clobbers the other. The mutex makes the check-then-write
// critical section indivisible within the process.
var importMu sync.Mutex

// ImportResult is the IPC payload returned by App.ImportTheme: the metadata
// of the imported theme, whether its id was renamed (and what it was renamed
// from), and any non-fatal warnings (currently empty, reserved for future
// "schema is forward-versioned" notices). Renamed is true when the source
// file's id was reserved (e.g. cyber_forest) or otherwise had to be adjusted
// to avoid clobbering an existing on-disk theme; the UI can show a "imported
// as <id>" toast so the user understands why a different id appears.
type ImportResult struct {
	Info             ThemeInfo         `json:"info"`
	RenamedFromID    string            `json:"renamed_from_id,omitempty"`
	Renamed          bool              `json:"renamed"`
	Warnings         []string          `json:"warnings,omitempty"`
	ValidationErrors []ValidationError `json:"validation_errors,omitempty"`
}

// idSanitizer matches the plugin manifest id pattern (^[a-z0-9_-]+$). The
// importer uses it to normalize theme ids so they are safe to use as file
// names and as keys in the listing. Mirrors backend/plugins/installer.go
// but additionally keeps underscores, which the existing built-in id
// (DefaultThemeID = "cyber_forest") relies on — stricter sanitization
// would rename the built-in on import and the namespacing step below
// would then prepend "user-" to a name that is already canonical. The
// allowed set ([a-z0-9_-]) is filename-safe on every platform we ship
// (Windows, macOS, Linux).
var idSanitizer = regexp.MustCompile(`[^a-z0-9_-]+`)

const userPrefix = "user-"

// ErrImportDuplicate is returned by ImportThemeFromPath when the resolved id
// already exists on disk. The frontend can detect it via errors.As and show
// "theme id X already exists" with a hint to rename in the source JSON.
var ErrImportDuplicate = errors.New("theme id already exists")

// ImportThemeFromPath validates a theme JSON file at srcPath, namespaces its
// id to avoid collisions with built-in / already-imported themes, and writes
// it atomically to themesDir. The shared validator (ParseAndValidate) is the
// single source of truth for "is this a valid theme" — the loader uses the
// same call, so any theme that survives an import is exactly the same kind
// of object ListThemes enumerates.
//
// Sandbox by validator, not by string sanitization: every token slot is
// checked by isSafeBackgroundImage / isValidFontFamily / isSafeCSSValue, which
// reject <, >, \, and top-level declaration-breakers — so embedded <script>
// or markup cannot reach the on-disk file even though url() (background
// images), font-family strings, and color-mix()/var() shadows are accepted.
// Unknown fields are rejected too (DisallowUnknownFields → structured
// ValidationErrors), so a JSON with extra keys fails rather than silently
// becoming a structurally-different theme.
func ImportThemeFromPath(themesDir, srcPath string) (*ImportResult, error) {
	if themesDir == "" {
		return nil, fmt.Errorf("themes directory is empty (vault not loaded)")
	}
	if srcPath == "" {
		return nil, fmt.Errorf("source path is empty")
	}
	if _, err := os.Stat(srcPath); err != nil {
		return nil, fmt.Errorf("failed to read source: %w", err)
	}
	raw, err := safeio.ReadFileMax(srcPath, maxThemeJSONBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to read source %s: %w", filepath.Base(srcPath), err)
	}
	t, err := ParseAndValidate(raw)
	if err != nil {
		var verrs ValidationErrors
		if errors.As(err, &verrs) {
			return &ImportResult{ValidationErrors: verrs}, nil
		}
		return nil, err
	}

	originalID := t.ID
	t.ID = sanitizeThemeID(t.ID)
	if t.ID == "" {
		return nil, fmt.Errorf("theme ID %q is invalid after sanitization", originalID)
	}

	// The namespace-check-then-write sequence must be atomic with
	// respect to other concurrent imports (rapid multi-file drag-drop).
	// Without the lock, two imports of the same id could both pass the
	// os.Stat existence check and then race to WriteFileAtomic.
	importMu.Lock()
	defer importMu.Unlock()

	// Reject any built-in id outright? The issue says "Sanitize/namespace
	// imported theme ids to avoid collisions with built-in themes" — a
	// built-in (e.g. cyber_forest) is the canonical default shipped from
	// embed.FS, so an import that lands on the same id would silently
	// replace the on-disk copy of the default. Namespace it under the
	// "user-" prefix instead. Collisions with on-disk themes are a hard
	// error (the user must rename the source id) so we never silently
	// overwrite a different theme.
	t.ID, err = namespaceThemeID(themesDir, t.ID, originalID)
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(themesDir, 0o700); err != nil {
		return nil, fmt.Errorf("failed to ensure themes dir %s: %w", themesDir, err)
	}

	// Re-serialize the (possibly id-rewritten) theme and write atomically.
	// The atomic write guarantees the new file is either the previous
	// version or the new one in full, never a half-written file truncated
	// by power loss. Re-marshalling from the parsed struct also normalises
	// the on-disk JSON to the canonical form, so the imported file matches
	// what subsequent ListThemes enumerations will surface.
	canon, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to re-serialize theme: %w", err)
	}
	dst := filepath.Join(themesDir, t.ID+".json")
	if err := parser.WriteFileAtomic(dst, canon); err != nil {
		return nil, fmt.Errorf("failed to write theme file: %w", err)
	}

	res := &ImportResult{
		Info:          t.AsInfo("disk"),
		RenamedFromID: originalID,
		Renamed:       originalID != t.ID,
		Warnings:      nil,
	}
	return res, nil
}

// ExportThemeToPath resolves the theme with the given id from themesDir (or
// the embedded default when id matches DefaultThemeID or is empty) and
// writes it verbatim to dstPath as JSON. Used by the picker to let the
// user download the active theme for round-trip editing. Atomic write
// follows the same temp+rename protocol as every other writer.
//
// When the active id is a custom theme that is NOT on disk (file deleted,
// corrupted, stale id), the function errors rather than silently writing
// the embedded default — the user expects to export their actual theme,
// not a surprise fallback.
func ExportThemeToPath(themesDir, id, dstPath string) error {
	if dstPath == "" {
		return fmt.Errorf("destination path is empty")
	}
	if themesDir == "" {
		return fmt.Errorf("themes directory is empty (vault not loaded)")
	}
	// Defense-in-depth: id flows into filepath.Join(themesDir, id+".assets")
	// via copyAssetsDirIfExists. An empty id resolves to the embedded default
	// further down; any non-empty id must pass the safe-id check.
	if id != "" && !IsValidThemeID(id) {
		return fmt.Errorf("invalid theme id %q", id)
	}
	// Guard against silent default-fallback: if the user's active id is a
	// custom theme but the file is missing/corrupted, ResolveActive would
	// silently return the embedded default. Error instead so the user knows
	// their theme file is gone.
	if id != "" && id != DefaultThemeID {
		if _, found, err := LoadByID(themesDir, id); err != nil {
			return fmt.Errorf("failed to look up theme %q for export: %w", id, err)
		} else if !found {
			return fmt.Errorf("active theme %q is not on disk; cannot export (the file may have been moved or deleted)", id)
		}
	}
	t, err := ResolveActive(themesDir, id, "dark")
	if err != nil {
		return err
	}
	canon, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize theme: %w", err)
	}
	if err := parser.WriteFileAtomic(dstPath, canon); err != nil {
		return fmt.Errorf("failed to write export: %w", err)
	}
	// Round-trip the assets directory too: a theme that references
	// <id>.assets/<file> would point at a missing file after export/import if
	// only the JSON were copied (RFC §3). No assets dir → nothing to copy.
	if err := copyAssetsDirIfExists(themesDir, id, dstPath); err != nil {
		return fmt.Errorf("failed to copy theme assets for export: %w", err)
	}
	return nil
}

// copyAssetsDirIfExists copies <themesDir>/<id>.assets/ to <dstDir>/<id>.assets/
// (next to the exported JSON) when it exists, so an assets-dir theme round-trips
// through export/import without broken references. A missing dir is the common
// case (base64 / embedded-name themes have no assets dir) and is a no-op.
func copyAssetsDirIfExists(themesDir, id, dstPath string) error {
	// Defense-in-depth: id flows into filepath.Join(themesDir, id+".assets").
	// All other id→path sites are guarded upstream; this is the only one that
	// took an unvalidated id (a caller could pass a path-traversal string).
	if !IsValidThemeID(id) {
		return fmt.Errorf("invalid theme id %q", id)
	}
	srcAssets := filepath.Join(themesDir, id+".assets")
	info, err := os.Stat(srcAssets)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if !info.IsDir() {
		return nil
	}
	dstAssets := filepath.Join(filepath.Dir(dstPath), id+".assets")
	return copyDir(srcAssets, dstAssets, 0)
}

// maxAssetsCopyDepth bounds how deep copyDir will recurse through the
// per-theme assets directory. The tree is intentionally shallow (one level
// of <id>.assets/<file>); a deeper tree is either a hostile zip-bomb
// attempt or an accidentally-synced nested folder, and either way the
// export should fail loudly instead of walking an unbounded chain of
// directories. Depth is counted from the entry call (depth 0 = the assets
// dir itself).
const maxAssetsCopyDepth = 3

// copyDir recursively copies src to dst. Used only by the export path for the
// per-theme assets directory; the tree is shallow in practice but the walk
// handles nested directories defensively. Files are copied read-only (0o644)
// since the destination is an export target, not the live store. Each file is
// size-bounded by maxBackgroundAssetBytes (matching the StoreBackgroundAsset
// ingest cap) so a hostile or accidentally-huge synced asset cannot drive
// unbounded allocation on export.
//
// Symlinks are NEVER followed: an attacker who can drop a symlink into the
// assets dir could point it anywhere on disk, and a naive copy would faithfully
// reproduce the link target's contents into the export (or loop on a cyclic
// link). Each entry is Lstat'd so symlinks are detected and skipped. The depth
// bound (maxAssetsCopyDepth) is the cycle / bomb backstop in case a future
// change reintroduces a follow path.
func copyDir(src, dst string, depth int) error {
	if depth > maxAssetsCopyDepth {
		return fmt.Errorf("assets directory nested too deep (>%d levels): %s", maxAssetsCopyDepth, src)
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		srcPath := filepath.Join(src, e.Name())
		dstPath := filepath.Join(dst, e.Name())
		// Lstat (not Stat) so symlinks are visible as symlinks rather
		// than being transparently resolved to their target. e.IsDir()
		// calls Stat under the hood and would follow the link, defeating
		// the guard.
		info, err := os.Lstat(srcPath)
		if err != nil {
			return err
		}
		// Skip symlinks rather than following them — see the doc comment.
		if info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		if info.IsDir() {
			if err := copyDir(srcPath, dstPath, depth+1); err != nil {
				return err
			}
			continue
		}
		data, err := safeio.ReadFileMax(srcPath, maxBackgroundAssetBytes)
		if err != nil {
			return fmt.Errorf("asset %s exceeds the %d-byte cap: %w", srcPath, maxBackgroundAssetBytes, err)
		}
		if err := os.WriteFile(dstPath, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// sanitizeThemeID lowercases the id and replaces any non-[a-z0-9-] char with
// a single '-'. Collapses runs and trims leading/trailing '-'. Returns "" for
// an input that contains no valid chars (caller should treat as a validation
// error — id is required by Validate, but we defend here too).
func sanitizeThemeID(id string) string {
	id = strings.ToLower(strings.TrimSpace(id))
	id = idSanitizer.ReplaceAllString(id, "-")
	id = strings.Trim(id, "-")
	for strings.Contains(id, "--") {
		id = strings.ReplaceAll(id, "--", "-")
	}
	return id
}

// namespaceThemeID ensures the chosen id does not collide with an embedded
// first-class theme or with any on-disk theme already present in themesDir.
// First-class ids get a "user-" prefix; a second collision appends -2, -3, …
// Collisions with on-disk themes (where the original id is already in the
// user namespace) are a hard error so the user can rename the source JSON
// rather than silently overwrite a different theme.
func namespaceThemeID(themesDir, id, originalID string) (string, error) {
	// 1. If the id matches ANY embedded first-class theme, namespace it.
	//    The embed-authoritative loader skips on-disk themes whose id
	//    matches a first-class id and serves the embedded copy instead,
	//    so an import that keeps the id would silently never appear in
	//    the picker (the documented export→edit→re-import loop breaks
	//    for 10/11 themes). ParseEmbeddedByID is the single roster check
	//    across the full shipped set.
	if _, ok := ParseEmbeddedByID(id); ok {
		id = userPrefix + id
	}

	// 2. On-disk collision: refuse to overwrite. The user can rename the
	//    source JSON's id and try again. A "renamed" id that collides with
	//    an existing on-disk theme (rare) gets a counter suffix.
	if _, err := os.Stat(filepath.Join(themesDir, id+".json")); err == nil {
		// The original id (pre-sanitize) colliding with an existing file
		// is a duplicate import — refuse it. Otherwise (we already renamed
		// it because it was a built-in) try a counter.
		if id == originalID || originalID == "" {
			return "", fmt.Errorf("%w: %s", ErrImportDuplicate, id)
		}
		proposed := id
		for i := 2; ; i++ {
			proposed = fmt.Sprintf("%s-%d", id, i)
			if _, err := os.Stat(filepath.Join(themesDir, proposed+".json")); os.IsNotExist(err) {
				id = proposed
				break
			}
			if i > 99 {
				return "", fmt.Errorf("%w: %s", ErrImportDuplicate, originalID)
			}
		}
	}

	return id, nil
}

// ExistingOnDiskIDs returns the sorted set of theme ids already present on
// disk in themesDir. Used by namespaceThemeID and exposed for tests; not
// part of the public picker surface (the picker uses ListThemes, which
// dedupes on-disk + embedded and surfaces load errors).
func ExistingOnDiskIDs(themesDir string) ([]string, error) {
	if themesDir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(themesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	ids := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".json") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, nil
}
