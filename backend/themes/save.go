package themes

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"silt/backend/parser"
	"silt/backend/safeio"
)

// EditorStagingThemeID is a reserved theme id used only for custom-theme
// editor preview staging. Large images are written to
// themesDir/_editor.assets/<hash>.ext so themeAssetHandler can serve them
// (it only accepts <id>.assets/ paths with a valid IsValidThemeID).
// Never allocate, overwrite, rename, or delete this id as a user theme —
// that would clobber the shared staging assets directory.
const EditorStagingThemeID = "_editor"

// editorStagingThemeID is the unexported alias used throughout this package.
const editorStagingThemeID = EditorStagingThemeID

// isReservedThemeID reports whether id is reserved for internal use and must
// never be allocated or mutated as a user-facing custom theme.
func isReservedThemeID(id string) bool {
	return id == editorStagingThemeID
}

// editorStagingDir is the legacy relative directory under themesDir where the
// custom theme editor staged large background images. Kept so
// materializeStagingImageRef can still resolve in-flight refs from older
// editor sessions that used url(".editor-staging/...").
const editorStagingDir = ".editor-staging"

// GetThemeJSON returns the canonical JSON for the theme with the given id.
// On-disk custom themes win when present; otherwise the embedded first-class
// copy is returned. An unknown id returns an error.
func GetThemeJSON(themesDir, id string) ([]byte, error) {
	if !IsValidThemeID(id) {
		return nil, fmt.Errorf("invalid theme id %q", id)
	}

	// Disk first: LoadByID scans by the theme's id field (filename may differ).
	if themesDir != "" {
		t, found, err := LoadByID(themesDir, id)
		if err != nil {
			return nil, fmt.Errorf("failed to look up theme %q: %w", id, err)
		}
		if found {
			// First-class ids are embed-authoritative for listing; an orphan
			// same-id vault file must not seed the editor over the packaged
			// theme. Prefer the embed when the id is first-class.
			if _, ok := ParseEmbeddedByID(id); ok {
				// fall through to embed below
			} else {
				return marshalThemeJSON(t)
			}
		}
	}

	if t, ok := ParseEmbeddedByID(id); ok {
		return marshalThemeJSON(t)
	}
	return nil, fmt.Errorf("theme %q not found", id)
}

func marshalThemeJSON(t *Theme) ([]byte, error) {
	canon, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to serialize theme: %w", err)
	}
	return canon, nil
}

// SaveCustomTheme writes a validated theme to themesDir.
//
// overwrite=false always allocates a new id from the theme name (slug +
// user- prefix for embed collisions + -2/-3 suffixes for on-disk collisions).
// overwrite=true replaces an existing on-disk custom theme at t.ID; first-class
// embedded ids are refused even if a same-id orphan file exists on disk.
//
// The caller must have already validated t (ParseAndValidate). The returned
// ThemeInfo describes the saved on-disk theme (source "disk").
func SaveCustomTheme(themesDir string, t *Theme, overwrite bool) (*ThemeInfo, error) {
	if themesDir == "" {
		return nil, errors.New("themes directory is empty (vault not loaded)")
	}
	if t == nil {
		return nil, errors.New("theme is nil")
	}
	if strings.TrimSpace(t.Name) == "" {
		return nil, errors.New("theme name is required")
	}

	if overwrite {
		if err := assertOverwritableCustomID(themesDir, t.ID); err != nil {
			return nil, err
		}
	} else {
		newID, err := allocateNewThemeID(themesDir, t.Name)
		if err != nil {
			return nil, err
		}
		t.ID = newID
	}

	if err := os.MkdirAll(themesDir, 0o700); err != nil {
		return nil, fmt.Errorf("failed to ensure themes dir %s: %w", themesDir, err)
	}

	// Staging refs from PrepareBackgroundAsset must land in <id>.assets/ before
	// the theme JSON is written, or the saved theme would point at a transient path.
	if err := materializeStagingBackgrounds(themesDir, t); err != nil {
		return nil, err
	}

	canon, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to re-serialize theme: %w", err)
	}
	dst := filepath.Join(themesDir, t.ID+".json")
	if err := parser.WriteFileAtomic(dst, canon); err != nil {
		return nil, fmt.Errorf("failed to write theme file: %w", err)
	}

	InvalidateThemeCache(t.ID)
	info := t.AsInfo("disk")
	return &info, nil
}

// materializeStagingBackgrounds walks both modes' surfaces; for each
// background.image that references editor staging (url("_editor.assets/foo.png")
// or legacy url(".editor-staging/foo.png")), copy the staged file into
// themesDir/<themeID>.assets/<filename> and rewrite the image ref to
// url("<themeID>.assets/<filename>").
// Data-URI refs and already-materialized <id>.assets/ refs are left alone.
// Missing staging file → return error (fail loud).
func materializeStagingBackgrounds(themesDir string, t *Theme) error {
	if t == nil {
		return nil
	}
	if err := materializeSurfacesStaging(themesDir, t.ID, &t.Modes.Dark.Surfaces); err != nil {
		return err
	}
	return materializeSurfacesStaging(themesDir, t.ID, &t.Modes.Light.Surfaces)
}

func materializeSurfacesStaging(themesDir, themeID string, surfaces *Surfaces) error {
	if err := materializeSurfaceStaging(themesDir, themeID, &surfaces.App); err != nil {
		return err
	}
	// Optional zones are pointers; app is a value field (handled above).
	for _, s := range []*Surface{
		surfaces.Sidebar, surfaces.Editor, surfaces.Panel, surfaces.Modal,
		surfaces.Popover, surfaces.Card, surfaces.Titlebar, surfaces.Activitybar,
	} {
		if s == nil {
			continue
		}
		if err := materializeSurfaceStaging(themesDir, themeID, s); err != nil {
			return err
		}
	}
	return nil
}

func materializeSurfaceStaging(themesDir, themeID string, s *Surface) error {
	if s == nil || s.Background == nil || s.Background.Image == "" {
		return nil
	}
	newRef, err := materializeStagingImageRef(themesDir, themeID, s.Background.Image)
	if err != nil {
		return err
	}
	if newRef != "" {
		s.Background.Image = newRef
	}
	return nil
}

// materializeStagingImageRef rewrites a staging url(...) ref into a permanent
// <themeID>.assets/ ref after copying the file. Accepts both the current
// _editor.assets/ prefix and the legacy .editor-staging/ prefix. Returns ""
// when the ref is not a staging path (data URI, already-materialized assets,
// bare colors, etc.).
func materializeStagingImageRef(themesDir, themeID, image string) (string, error) {
	inner, ok := cssURLInner(image)
	if !ok {
		return "", nil
	}
	if strings.HasPrefix(inner, "data:") {
		return "", nil
	}
	rel := filepath.ToSlash(inner)

	var stagingRoot, filename string
	switch {
	case strings.HasPrefix(rel, editorStagingThemeID+".assets/"):
		filename = rel[len(editorStagingThemeID+".assets/"):]
		stagingRoot = filepath.Join(themesDir, editorStagingThemeID+".assets")
	case strings.HasPrefix(rel, editorStagingDir+"/"):
		filename = rel[len(editorStagingDir+"/"):]
		stagingRoot = filepath.Join(themesDir, editorStagingDir)
	default:
		return "", nil
	}

	// Content-addressed staging names are single path segments; reject traversal.
	if filename == "" || strings.Contains(filename, "..") || strings.ContainsAny(filename, `/\`) {
		return "", fmt.Errorf("invalid staging background ref %q", image)
	}

	src := filepath.Join(stagingRoot, filepath.Clean(filename))
	// Defense in depth: cleaned path must stay within the staging directory.
	if !isPathWithinDir(src, stagingRoot) {
		return "", fmt.Errorf("invalid staging background ref %q", image)
	}
	raw, err := os.ReadFile(src)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("staged background asset missing: %s", filename)
		}
		return "", fmt.Errorf("failed to read staged background %s: %w", filename, err)
	}

	assetsDir := filepath.Join(themesDir, themeID+".assets")
	if err := os.MkdirAll(assetsDir, 0o755); err != nil {
		return "", fmt.Errorf("failed to create theme assets dir: %w", err)
	}
	dst := filepath.Join(assetsDir, filename)
	if err := os.WriteFile(dst, raw, 0o600); err != nil {
		return "", fmt.Errorf("failed to materialize background asset: %w", err)
	}
	return fmt.Sprintf("url(\"%s.assets/%s\")", themeID, filename), nil
}

// isPathWithinDir reports whether path is dir itself or lives under it after
// filepath.Clean (CWE-22 backstop for staging materialization).
func isPathWithinDir(path, dir string) bool {
	cleanPath := filepath.Clean(path)
	cleanDir := filepath.Clean(dir)
	if cleanPath == cleanDir {
		return true
	}
	return strings.HasPrefix(cleanPath, cleanDir+string(filepath.Separator))
}

// cssURLInner extracts the path/data from a CSS url("...") / url('...') / url(...)
// value. Returns ok=false when the string is not a url() wrapper.
func cssURLInner(ref string) (string, bool) {
	ref = strings.TrimSpace(ref)
	if !strings.HasPrefix(ref, "url(") || !strings.HasSuffix(ref, ")") {
		return "", false
	}
	inner := strings.TrimSpace(ref[len("url(") : len(ref)-1])
	if len(inner) >= 2 {
		if (inner[0] == '"' && inner[len(inner)-1] == '"') ||
			(inner[0] == '\'' && inner[len(inner)-1] == '\'') {
			inner = inner[1 : len(inner)-1]
		}
	}
	return inner, true
}

// assertOverwritableCustomID refuses first-class embed ids, the reserved
// editor staging id, and missing files. Custom saves never clobber a
// built-in id as if it were the packaged theme.
func assertOverwritableCustomID(themesDir, id string) error {
	if !IsValidThemeID(id) {
		return fmt.Errorf("invalid theme id %q", id)
	}
	if isReservedThemeID(id) {
		return fmt.Errorf("cannot overwrite reserved theme id %q", id)
	}
	if _, ok := ParseEmbeddedByID(id); ok {
		return fmt.Errorf("cannot overwrite built-in theme %q; save as a new custom theme instead", id)
	}
	path := filepath.Join(themesDir, id+".json")
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			// Filename may differ from id — confirm via LoadByID.
			if _, found, lerr := LoadByID(themesDir, id); lerr != nil {
				return fmt.Errorf("failed to look up theme %q: %w", id, lerr)
			} else if !found {
				return fmt.Errorf("theme %q is not an on-disk custom theme", id)
			}
			// Found under a non-canonical filename: still refuse overwrite via
			// id.json path ambiguity — require the canonical <id>.json file so
			// Save always writes a stable path.
			return fmt.Errorf("theme %q is not stored as %s.json; cannot overwrite", id, id)
		}
		return fmt.Errorf("failed to stat theme %q: %w", id, err)
	}
	return nil
}

// allocateNewThemeID builds a free custom id from a display name: sanitize to
// [a-z0-9_-], namespace away from first-class embeds and the reserved editor
// staging id with the user- prefix, and append -2/-3… on on-disk collisions
// (mirrors importer namespacing, but always finds a free slot rather than
// refusing duplicates).
func allocateNewThemeID(themesDir, name string) (string, error) {
	base := sanitizeThemeID(name)
	if base == "" {
		return "", fmt.Errorf("theme name %q produces an empty id after sanitization", name)
	}
	id := base
	if _, ok := ParseEmbeddedByID(id); ok {
		id = userPrefix + id
	}
	// Reserved staging id (_editor) must never become a real theme file —
	// that would share a path with _editor.assets/ used for preview staging.
	if isReservedThemeID(id) {
		id = userPrefix + id
	}
	// Also refuse to land on a first-class/reserved id after prefixing
	// (defensive — user-<embed> is never itself first-class today).
	if _, err := os.Stat(filepath.Join(themesDir, id+".json")); err == nil {
		// collision
	} else if os.IsNotExist(err) {
		if _, ok := ParseEmbeddedByID(id); !ok && !isReservedThemeID(id) {
			return id, nil
		}
	} else if err != nil {
		return "", fmt.Errorf("failed to check theme id %q: %w", id, err)
	}

	for i := 2; i <= 99; i++ {
		proposed := fmt.Sprintf("%s-%d", id, i)
		if _, ok := ParseEmbeddedByID(proposed); ok {
			continue
		}
		if isReservedThemeID(proposed) {
			continue
		}
		if _, err := os.Stat(filepath.Join(themesDir, proposed+".json")); os.IsNotExist(err) {
			return proposed, nil
		} else if err != nil {
			return "", fmt.Errorf("failed to check theme id %q: %w", proposed, err)
		}
	}
	return "", fmt.Errorf("could not allocate a unique theme id for %q", name)
}

// RenameCustomTheme updates only the name field of an on-disk custom theme.
// First-class embedded ids, the reserved editor staging id, and missing
// files are refused.
func RenameCustomTheme(themesDir, id, name string) error {
	if themesDir == "" {
		return errors.New("themes directory is empty (vault not loaded)")
	}
	if !IsValidThemeID(id) {
		return fmt.Errorf("invalid theme id %q", id)
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("theme name is required")
	}
	if isReservedThemeID(id) {
		return fmt.Errorf("cannot rename reserved theme id %q", id)
	}
	if _, ok := ParseEmbeddedByID(id); ok {
		return fmt.Errorf("cannot rename built-in theme %q", id)
	}

	path := filepath.Join(themesDir, id+".json")
	t, err := LoadTheme(path)
	if err != nil {
		if os.IsNotExist(err) || errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("theme %q is not an on-disk custom theme", id)
		}
		// LoadTheme wraps read errors; surface a clearer missing-file message
		// when the path simply does not exist.
		if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
			return fmt.Errorf("theme %q is not an on-disk custom theme", id)
		}
		return err
	}
	if t.ID != id {
		// Defensive: file content id must match the path id we are renaming.
		return fmt.Errorf("theme file id %q does not match path id %q", t.ID, id)
	}

	t.Name = name
	// Re-validate so a rename cannot leave an otherwise-invalid theme on disk
	// if future name rules tighten; currently name is free-form non-empty.
	raw, err := json.Marshal(t)
	if err != nil {
		return fmt.Errorf("failed to serialize theme: %w", err)
	}
	if _, err := ParseAndValidate(raw); err != nil {
		return err
	}

	canon, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to re-serialize theme: %w", err)
	}
	if err := parser.WriteFileAtomic(path, canon); err != nil {
		return fmt.Errorf("failed to write theme file: %w", err)
	}
	InvalidateThemeCache(id)
	return nil
}

// DeleteCustomTheme removes <id>.json and <id>.assets/ for an on-disk custom
// theme. First-class embedded ids, the reserved editor staging id, and
// missing files are refused. The caller (App.DeleteCustomTheme) must refuse
// deleting the active theme.
func DeleteCustomTheme(themesDir, id string) error {
	if themesDir == "" {
		return errors.New("themes directory is empty (vault not loaded)")
	}
	if !IsValidThemeID(id) {
		return fmt.Errorf("invalid theme id %q", id)
	}
	if isReservedThemeID(id) {
		// Deleting _editor would RemoveAll(_editor.assets) and wipe live
		// editor preview staging for every open custom-theme editor.
		return fmt.Errorf("cannot delete reserved theme id %q", id)
	}
	if _, ok := ParseEmbeddedByID(id); ok {
		return fmt.Errorf("cannot delete built-in theme %q", id)
	}

	path := filepath.Join(themesDir, id+".json")
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("theme %q is not an on-disk custom theme", id)
		}
		return fmt.Errorf("failed to stat theme %q: %w", id, err)
	}

	if err := os.Remove(path); err != nil {
		return fmt.Errorf("failed to delete theme file: %w", err)
	}
	assetsDir := filepath.Join(themesDir, id+".assets")
	if err := os.RemoveAll(assetsDir); err != nil {
		return fmt.Errorf("failed to delete theme assets: %w", err)
	}
	InvalidateThemeCache(id)
	return nil
}

// PrepareBackgroundAsset reads an image like StoreBackgroundAsset but does not
// write into a theme. Small files (≤ base64InlineThreshold) become a data-URI
// reference; larger files are copied to themesDir/_editor.assets/<hash>.<ext>
// and returned as url("_editor.assets/..."). The reserved theme id lets
// themeAssetHandler serve the preview (it only accepts <id>.assets/ paths).
// Used by the custom theme editor for live preview before Save.
func PrepareBackgroundAsset(themesDir, srcPath string) (ref string, isBase64 bool, err error) {
	if themesDir == "" {
		return "", false, errors.New("themes directory is empty (vault not loaded)")
	}
	if srcPath == "" {
		return "", false, errors.New("source path is empty")
	}
	ext := strings.ToLower(filepath.Ext(srcPath))
	mime, ok := AllowedBackgroundExts[ext]
	if !ok {
		return "", false, fmt.Errorf("unsupported image extension %q (allowed: .png .jpg .jpeg .webp .gif .svg)", ext)
	}

	info, err := os.Stat(srcPath)
	if err != nil {
		return "", false, fmt.Errorf("failed to read source %s: %w", filepath.Base(srcPath), err)
	}
	if info.Size() > maxBackgroundAssetBytes {
		return "", false, fmt.Errorf("background image %s is %d bytes, which exceeds the %d-byte (4 MB) cap", filepath.Base(srcPath), info.Size(), maxBackgroundAssetBytes)
	}
	raw, err := safeio.ReadFileMax(srcPath, maxBackgroundAssetBytes)
	if err != nil {
		return "", false, fmt.Errorf("failed to read %s: %w", filepath.Base(srcPath), err)
	}

	if int64(len(raw)) <= base64InlineThreshold {
		encoded := base64.StdEncoding.EncodeToString(raw)
		return fmt.Sprintf("url(\"data:%s;base64,%s\")", mime, encoded), true, nil
	}

	// Large file: stage under _editor.assets/ with a content-addressed name so
	// re-picking the same bytes is idempotent, concurrent picks never clobber
	// each other, and themeAssetHandler can serve the preview URL.
	sum := sha256.Sum256(raw)
	filename := fmt.Sprintf("%x%s", sum[:8], ext)
	stagingDir := filepath.Join(themesDir, editorStagingThemeID+".assets")
	if err := os.MkdirAll(stagingDir, 0o755); err != nil {
		return "", false, fmt.Errorf("failed to create editor staging directory: %w", err)
	}
	dst := filepath.Join(stagingDir, filename)
	if err := os.WriteFile(dst, raw, 0o600); err != nil {
		return "", false, fmt.Errorf("failed to write staged asset: %w", err)
	}
	return fmt.Sprintf("url(\"%s.assets/%s\")", editorStagingThemeID, filename), false, nil
}
