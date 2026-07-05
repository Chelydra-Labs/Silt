package themes

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"silt/backend/parser"
	"silt/backend/safeio"
)

// Background asset pipeline (#391): the unified per-zone background system
// stores user-supplied images either inline (small files → base64 data URI) or
// in a per-theme assets directory (larger files → relative reference). This
// file owns the storage decision, the on-disk theme mutation that writes the
// chosen reference into surfaces.<zone>.background, and the export-time copy
// of the assets directory so a theme round-trips through export/import without
// broken references (RFC §3).

const (
	// maxBackgroundAssetBytes bounds a single background image's size before it
	// is read into memory. The RFC §3 proposed cap is ~4 MB / 8 MP; rejecting
	// larger files outright keeps a hostile or accidentally-huge synced file
	// from driving unbounded allocation (#391 size gate).
	maxBackgroundAssetBytes int64 = 4 << 20 // 4 MB

	// base64InlineThreshold is the size below which an image is inlined as a
	// base64 data URI (D10): single-file themes stay portable and the picker's
	// common case — small textures, grain patterns, SVGs — never touches the
	// assets directory. Above this, the file is copied to <id>.assets/.
	base64InlineThreshold int64 = 50 * 1024 // 50 KB
)

// allowedBackgroundExts maps a lowercased extension to the MIME subtype used in
// the data URI. Anything outside this set is rejected at pick time so a user
// cannot point the background pipeline at an arbitrary (potentially huge or
// non-image) file.
var allowedBackgroundExts = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".webp": "image/webp",
	".gif":  "image/gif",
	".svg":  "image/svg+xml",
}

// bgFilenameSanitizer collapses any non-[a-z0-9-] run in a stored asset
// filename to a single '-', mirroring sanitizeThemeID so on-disk asset names
// are predictable and safe on every platform we ship.
var bgFilenameSanitizer = regexp.MustCompile(`[^a-z0-9-]+`)

// StoreBackgroundAsset reads the image at srcPath and returns the value to
// write into Background.Image. Small files (≤ base64InlineThreshold) are
// inlined as a base64 data URI; larger files are copied into the theme's
// assets directory at <themesDir>/<themeID>.assets/<sanitized-name>.<ext>.
//
// The returned reference is a complete, render-ready CSS background-image
// value (consistent with the v2 contract a theme's background.image already
// follows — e.g. Linen ships a url("data:…") value): a small file becomes
// url("data:<mime>;base64,…") and a large one becomes url("<id>.assets/<file>").
// Wrapping in url() keeps the value a single CSS token, so the ';' inside a
// base64 data URI cannot terminate the surrounding :root{--name:value;}
// declaration (the validator's isSafeBackgroundImage lets ';' through only
// inside url()/strings).
//
// isBase64 reports which branch was taken so the caller (and the IPC result)
// can tell the user whether the theme is now self-contained.
func StoreBackgroundAsset(themesDir, themeID, srcPath string) (ref string, isBase64 bool, err error) {
	if themesDir == "" {
		return "", false, errors.New("themes directory is empty (vault not loaded)")
	}
	if themeID == "" {
		return "", false, errors.New("theme id is empty")
	}
	if srcPath == "" {
		return "", false, errors.New("source path is empty")
	}
	ext := strings.ToLower(filepath.Ext(srcPath))
	mime, ok := allowedBackgroundExts[ext]
	if !ok {
		return "", false, fmt.Errorf("unsupported image extension %q (allowed: .png .jpg .jpeg .webp .gif .svg)", ext)
	}

	// Size-gate before reading: a clear message up front beats safeio's generic
	// "exceeds the cap" parse-time wording. safeio.ReadFileMax is still the
	// read bound (defense-in-depth against a file that grows between stat and
	// read).
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

	// Large file: copy into the per-theme assets directory. The stored
	// reference is the self-describing relative path <themeID>.assets/<file>
	// (wrapped in url() so it is a valid background-image value); the future
	// reference resolver (RFC §3) resolves it against the themes directory.
	// A short content hash is appended to the sanitized basename so two
	// zones that pick same-named images do not clobber each other on disk.
	baseName := strings.ToLower(filepath.Base(srcPath))
	base := sanitizeBgFilename(strings.TrimSuffix(baseName, ext))
	sum := sha256.Sum256(raw)
	filename := fmt.Sprintf("%s-%x%s", base, sum[:4], ext)
	assetsDir := filepath.Join(themesDir, themeID+".assets")
	if err := os.MkdirAll(assetsDir, 0o755); err != nil {
		return "", false, fmt.Errorf("failed to create assets directory %s: %w", assetsDir, err)
	}
	dst := filepath.Join(assetsDir, filename)
	if err := os.WriteFile(dst, raw, 0o600); err != nil {
		return "", false, fmt.Errorf("failed to write asset %s: %w", dst, err)
	}
	return fmt.Sprintf("url(\"%s.assets/%s\")", themeID, filename), false, nil
}

// sanitizeBgFilename lowercases the name and collapses non-[a-z0-9-] runs to a
// single '-', trimming and collapsing hyphens. An all-invalid name falls back
// to "background" so the asset always lands at a non-empty, stable filename.
func sanitizeBgFilename(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	name = bgFilenameSanitizer.ReplaceAllString(name, "-")
	name = strings.Trim(name, "-")
	for strings.Contains(name, "--") {
		name = strings.ReplaceAll(name, "--", "-")
	}
	if name == "" {
		return "background"
	}
	return name
}

// IsValidSurfaceZone reports whether zone is one of the 7 named surface zones
// a background can be attached to (app/sidebar/editor/panel/card/modal/popover,
// RFC §5). Exported so the Wails binding validates the zone before opening the
// native picker.
func IsValidSurfaceZone(zone string) bool {
	_, ok := zoneByName(zone)
	return ok
}

// SetThemeBackgroundImage writes bg into the given zone's background block on
// BOTH modes of the on-disk theme identified by themeID, then re-marshals the
// theme to canonical JSON and writes it atomically. A background image is
// typically mode-agnostic (the same photo behind dark and light text); a caller
// that wants a per-mode background edits the JSON directly.
//
// Only on-disk (custom) themes are mutable: an embedded first-class id is
// read-only (baked into the binary), so the caller must fork it first —
// PickBackgroundImage does this automatically. The bg block is validated
// (image safety, size enum, opacity range, blend keyword, scrim color) before
// any write, and a zone that was previously un-authored is seeded from its
// parent's resolved bg/border/text so the surface stays valid (those fields
// become required once the surface exists) and renders identically to its
// inherited state save for the new background.
func SetThemeBackgroundImage(themesDir, themeID, zone string, bg Background) error {
	if themesDir == "" {
		return errors.New("themes directory is empty (vault not loaded)")
	}
	if _, ok := zoneByName(zone); !ok {
		return fmt.Errorf("invalid surface zone %q (valid: app, sidebar, editor, panel, card, modal, popover)", zone)
	}
	if verrs := validateBackground("background", &bg); len(verrs) > 0 {
		return verrs
	}
	t, found, err := LoadByID(themesDir, themeID)
	if err != nil {
		return fmt.Errorf("failed to look up theme %q: %w", themeID, err)
	}
	if !found {
		return fmt.Errorf("theme %q is not an on-disk (custom) theme; export/fork it first", themeID)
	}

	// Two independent copies so the dark/light surfaces don't alias one pointer.
	darkBG := bg
	lightBG := bg
	setZoneBackground(&t.Modes.Dark.Surfaces, zone, &darkBG)
	setZoneBackground(&t.Modes.Light.Surfaces, zone, &lightBG)

	canon, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to re-serialize theme: %w", err)
	}
	dst := filepath.Join(themesDir, themeID+".json")
	if err := parser.WriteFileAtomic(dst, canon); err != nil {
		return fmt.Errorf("failed to write theme file: %w", err)
	}
	return nil
}

// setZoneBackground sets the background pointer on the named zone's Surface. A
// non-app zone that was not authored is seeded from its parent's resolved
// concrete Surface so the zone remains valid (bg/border/text are required once
// the surface pointer is non-nil) and looks unchanged apart from the background.
func setZoneBackground(s *Surfaces, zone string, bg *Background) {
	if zone == "app" {
		s.App.Background = bg
		return
	}
	z, ok := zoneByName(zone)
	if !ok {
		return
	}
	cur := z.get(*s)
	if cur == nil {
		parent := effectiveSurface(*s, z.parent)
		seeded := Surface{BG: parent.BG, Border: parent.Border, Text: parent.Text, Background: bg}
		assignSurface(s, zone, &seeded)
		return
	}
	cur.Background = bg
}

// assignSurface stores a Surface pointer under the named zone's field. app is
// a value field (handled by callers via s.App directly); the other eight zones
// are pointers (see surfaceZones in theme.go for the canonical list).
func assignSurface(s *Surfaces, zone string, surface *Surface) {
	switch zone {
	case "sidebar":
		s.Sidebar = surface
	case "editor":
		s.Editor = surface
	case "panel":
		s.Panel = surface
	case "card":
		s.Card = surface
	case "modal":
		s.Modal = surface
	case "popover":
		s.Popover = surface
	case "titlebar":
		s.Titlebar = surface
	case "activitybar":
		s.Activitybar = surface
	}
}

// effectiveSurface returns the concrete Surface a zone renders as, walking the
// inheritance tree (RFC §5) until an authored surface is found. app is always
// authored, so the walk always terminates — used to seed a previously-unset
// zone with its inherited values when attaching a background.
func effectiveSurface(s Surfaces, zone string) Surface {
	z, ok := zoneByName(zone)
	if !ok || z.parent == "" {
		return s.App
	}
	if cur := z.get(s); cur != nil {
		return *cur
	}
	return effectiveSurface(s, z.parent)
}

// ForkEmbeddedTheme writes a writable copy of an embedded first-class theme
// under the "user-" namespace (mirroring the importer's built-in collision
// prefix) and returns the forked id. Used by PickBackgroundImage when the
// active theme is embedded-only (not on disk): an embedded theme cannot be
// mutated in place, so editing its background first forks it onto disk. An
// existing fork is reused (not overwritten) so a user's prior edits survive a
// second pick.
func ForkEmbeddedTheme(themesDir, id string) (string, error) {
	if themesDir == "" {
		return "", errors.New("themes directory is empty (vault not loaded)")
	}
	if id == "" {
		return "", errors.New("theme id is empty")
	}
	t, ok := ParseEmbeddedByID(id)
	if !ok {
		return "", fmt.Errorf("theme %q is not an embedded first-class theme; nothing to fork", id)
	}
	forkedID := userPrefix + id
	dst := filepath.Join(themesDir, forkedID+".json")
	if _, err := os.Stat(dst); err == nil {
		// A fork already exists: preserve the user's prior edits.
		return forkedID, nil
	}
	t.ID = forkedID
	if err := os.MkdirAll(themesDir, 0o700); err != nil {
		return "", fmt.Errorf("failed to ensure themes dir: %w", err)
	}
	canon, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to serialize forked theme: %w", err)
	}
	if err := parser.WriteFileAtomic(dst, canon); err != nil {
		return "", fmt.Errorf("failed to write forked theme: %w", err)
	}
	return forkedID, nil
}
