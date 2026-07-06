package main

import (
	"fmt"
	"log"
	"path/filepath"
	"silt/backend/themes"
	"silt/backend/vault"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// --- Theme engine IPC (#45) -----------------------------------------------

// ActiveThemeResult is the IPC payload returned by GetActiveTheme /
// ApplyTheme. It carries the active theme id/name, the STORED mode
// (dark|light|system), the effective token map for the first paint, both
// dark/light maps so the frontend can resolve "system" locally without a
// second round-trip, and the resolved bg.void for the native webview
// background.
type ActiveThemeResult struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Mode        string            `json:"mode"`         // stored: dark|light|system
	Tokens      map[string]string `json:"tokens"`       // effective (first-paint) map
	DarkTokens  map[string]string `json:"dark_tokens"`  // always present
	LightTokens map[string]string `json:"light_tokens"` // always present
	BGVoid      string            `json:"bg_void"`      // effective bg.void for webview
}

// effectiveMode resolves a stored ThemeMode to a concrete dark/light for the
// first paint. "system" is resolved to "dark" here as the shipped default;
// the frontend re-resolves "system" via prefers-color-scheme using both
// token maps, so the backend never needs to query the OS.
func effectiveMode(mode string) string {
	if mode == "light" {
		return "light"
	}
	return "dark" // dark + system + unknown → dark first paint
}

// buildThemeResult assembles the IPC payload from a parsed theme + stored mode.
func buildThemeResult(t *themes.Theme, mode string) ActiveThemeResult {
	em := effectiveMode(mode)
	return ActiveThemeResult{
		ID:          t.ID,
		Name:        t.Name,
		Mode:        mode,
		Tokens:      t.Flatten(em),
		DarkTokens:  t.Flatten("dark"),
		LightTokens: t.Flatten("light"),
		BGVoid:      t.BGVoid(em),
	}
}

// themesDir returns <vault>/.system/themes, or "" before a vault is open.
func (a *App) themesDir() string {
	if a.vaultPath == "" {
		return ""
	}
	return filepath.Join(a.vaultPath, ".system", "themes")
}

// ListThemes enumerates available themes (on-disk + the embedded default)
// and any per-file load errors. Works before a vault is open (returns just
// the embedded default).
func (a *App) ListThemes() (*themes.ListThemesResult, error) {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	return themes.ListThemes(a.themesDir())
}

// GetActiveTheme reads AppSettings, resolves the active theme (falling back
// to the embedded default when the id is missing/invalid), and returns the
// token maps for injection. Always succeeds with the default theme on a
// fresh/empty vault so the app can render on first paint.
func (a *App) GetActiveTheme() (ActiveThemeResult, error) {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	settings, err := vault.LoadSettings()
	if err != nil {
		// Settings exist but are unreadable — surface it rather than
		// masking with the default (matches the startup() policy).
		return ActiveThemeResult{}, fmt.Errorf("failed to load settings: %w", err)
	}
	t, err := themes.ResolveActive(a.themesDir(), settings.ActiveTheme, settings.ThemeMode)
	if err != nil {
		return ActiveThemeResult{}, err
	}
	return buildThemeResult(t, settings.ThemeMode), nil
}

// ApplyTheme selects a theme and mode, persists it to settings, and returns
// the new token maps. Both id and mode are validated: an unknown id or an
// invalid mode returns a structured error and is NOT persisted.
//
// The on-disk theme scan happens exactly once (per #76): themes.LoadByID
// reads the themesDir and returns the parsed theme in a single pass. The
// previous implementation called ListThemes (reads + parses every file)
// followed by ResolveActive (reads the directory a second time to find the
// same theme), so every switch did two directory scans + 2N parses.
func (a *App) ApplyTheme(id, mode string) (ActiveThemeResult, error) {
	// vaultMu.RLock guards the lifecycle read of themesDir/vaultPath so the
	// path stays valid for the call's duration. It does NOT guard the
	// settings.json write — UpdateSettings serializes that via
	// settingsWriteMu (#404). No theme FILE is written here, so
	// themeWriteMu is not needed. Two concurrent ApplyTheme calls both
	// pass the RLock, but settingsWriteMu makes each settings write atomic.
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if !vault.ValidThemeMode(mode) {
		return ActiveThemeResult{}, fmt.Errorf("invalid mode %q (valid: dark, light, system)", mode)
	}
	// Resolve the requested theme in one pass. The embedded default is
	// always available; any other id must live on disk. A typo or stale id
	// errors here rather than silently snapping to the default.
	var (
		t   *themes.Theme
		err error
	)
	if id == themes.DefaultThemeID {
		t, err = themes.ParseDefault()
		if err != nil {
			return ActiveThemeResult{}, err
		}
	} else {
		var found bool
		t, found, err = themes.LoadByID(a.themesDir(), id)
		if err != nil {
			return ActiveThemeResult{}, fmt.Errorf("failed to look up theme %q: %w", id, err)
		}
		if !found {
			// Not on disk: a first-class id may still be available from the
			// embedded roster (a wiped or pre-Sprint-8 themes dir shouldn't
			// prevent switching to a shipped theme). ResolveActive does the
			// same fallback for the startup path; mirror it here so the
			// picker's "apply" and the launch-time resolve can't disagree
			// on whether a theme is selectable. A genuinely unknown id
			// (e.g. typo) still falls through to the error below.
			if et, ok := themes.ParseEmbeddedByID(id); ok {
				t = et
			} else {
				return ActiveThemeResult{}, fmt.Errorf("theme %q is not available", id)
			}
		}
	}

	// Persist the selection atomically. Use the actually-resolved theme id
	// (t.ID) rather than the requested id: if the caller requested the
	// embedded default and the file vanished mid-request, settings stays
	// consistent with what is rendered.
	if _, err := vault.UpdateSettings(func(s *vault.AppSettings) {
		s.ActiveTheme = t.ID
		s.ThemeMode = mode
	}); err != nil {
		return ActiveThemeResult{}, fmt.Errorf("failed to persist theme selection: %w", err)
	}

	res := buildThemeResult(t, mode)
	log.Printf("themes: ApplyTheme(id=%q mode=%q) → resolved %q", id, mode, t.ID)
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "theme:changed", map[string]string{
			"id": t.ID, "mode": mode,
		})
	}
	return res, nil
}

// PickThemeFile opens the native file picker (filtered to *.json) and
// returns the chosen path. The empty string means the user cancelled. The
// frontend feeds the returned path to ImportTheme — the backend does all
// validation and writing, so the frontend never touches the filesystem
// directly.
func (a *App) PickThemeFile() (string, error) {
	if a.ctx == nil {
		return "", fmt.Errorf("application context not ready")
	}
	selected, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Select a theme JSON",
		Filters: []runtime.FileFilter{
			{DisplayName: "Silt Theme (*.json)", Pattern: "*.json"},
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to open file picker: %w", err)
	}
	return selected, nil
}

// ImportTheme validates a theme JSON at srcPath, namespaces its id to
// avoid collisions with built-ins / already-imported themes, and writes
// it atomically to <vault>/.system/themes/. The shared validator
// (themes.ParseAndValidate) is the same call the loader uses, so a
// successfully imported theme is the exact same object ListThemes will
// enumerate on the next picker refresh.
//
// On success the Wails-bound event "themes:changed" is emitted so any
// subscribed frontend (the picker, future command palette, etc.)
// re-fetches the listing immediately. The active theme is NOT changed:
// a fresh import is unselected until the user picks it.
//
// The in-process theme cache (#73) is invalidated so a launch-time
// background-color resolution that runs after the import will pick up
// the new file instead of a stale parse.
func (a *App) ImportTheme(srcPath string) (*themes.ImportResult, error) {
	a.vaultMu.RLock()
	vaultPath := a.vaultPath
	a.vaultMu.RUnlock()
	if vaultPath == "" {
		return nil, fmt.Errorf("vault not loaded")
	}
	// Compute themesDir from the snapshotted vaultPath so the write always
	// targets the correct absolute directory even if the vault closes between
	// the RUnlock and the themeWriteMu acquisition (#404 hardening). Calling
	// a.themesDir() here would re-read a.vaultPath without a lock and could
	// return a relative ".system/themes" path if CloseVault nil'd the field.
	themesDir := filepath.Join(vaultPath, ".system", "themes")
	// Serialize the theme-file write so two concurrent imports can't race
	// on the importer's collision-check-then-write (#404). The validation
	// step is pure CPU (no disk write) and could run outside the lock, but
	// keeping the whole import under themeWriteMu ensures the
	// check-then-write is atomic w.r.t. other theme writers.
	a.themeWriteMu.Lock()
	defer a.themeWriteMu.Unlock()
	res, err := themes.ImportThemeFromPath(themesDir, srcPath)
	if err != nil {
		log.Printf("themes: ImportTheme(%q) failed: %v", filepath.Base(srcPath), err)
		return nil, err
	}
	if len(res.ValidationErrors) > 0 {
		log.Printf("themes: ImportTheme(%q) rejected: %d validation error(s)", filepath.Base(srcPath), len(res.ValidationErrors))
		return res, nil
	}
	log.Printf("themes: ImportTheme(%q) → imported as %q (renamed=%v)", filepath.Base(srcPath), res.Info.ID, res.Renamed)
	themes.InvalidateThemeCache(res.Info.ID)
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "themes:changed", struct{}{})
	}
	return res, nil
}

// BackgroundImageResult is the IPC payload returned by PickBackgroundImage.
// ThemeID is the on-disk theme the background was written to (which may be a
// freshly-created fork of an embedded theme — see Forked); Reference is the
// CSS background-image value now stored at surfaces.<zone>.background.image;
// Base64 reports whether the asset was inlined as a data URI (true) or copied
// to the theme's <id>.assets/ directory (false).
type BackgroundImageResult struct {
	ThemeID   string `json:"theme_id"`
	Forked    bool   `json:"forked"`
	Zone      string `json:"zone"`
	Reference string `json:"reference"`
	Base64    bool   `json:"base64"`
}

// PickBackgroundImage opens a native image picker, stores the chosen file via
// the asset pipeline, and writes it into surfaces.<zone>.background of the
// active theme. The active theme is the one in AppSettings; if it is an
// embedded first-class theme (not on disk), it is auto-forked under the
// "user-" namespace first so the edit lands on a writable copy and the
// fork becomes the active theme. Cancelling the picker returns (nil, nil).
//
// On success the cache is invalidated and "themes:changed" is emitted so the
// picker (and any future live preview) re-fetches. This is the engine half of
// #391; the per-zone picker UI is Phase 2 (#401).
func (a *App) PickBackgroundImage(zone string) (*BackgroundImageResult, error) {
	// Snapshot the vault path under the lifecycle read lock, then release it
	// BEFORE opening the native dialog (#404). The dialog blocks indefinitely
	// (user picks a file, goes for coffee), and holding vaultMu.RLock across
	// it would block any vaultMu.Lock writer (vault open/close, settings
	// migration) for the dialog's entire duration. themeWriteMu (acquired
	// below, after the dialog) serializes the actual file mutations.
	a.vaultMu.RLock()
	vaultPath := a.vaultPath
	a.vaultMu.RUnlock()
	if vaultPath == "" {
		return nil, fmt.Errorf("vault not loaded")
	}
	if a.ctx == nil {
		return nil, fmt.Errorf("application context not ready")
	}
	if !themes.IsValidSurfaceZone(zone) {
		return nil, fmt.Errorf("invalid surface zone %q (valid: %s)", zone, themes.ValidSurfaceZoneNames())
	}

	settings, err := vault.LoadSettings()
	if err != nil {
		return nil, fmt.Errorf("failed to load settings: %w", err)
	}

	// Open the picker BEFORE acquiring any write lock so a cancel is a pure
	// no-op (no fork, no settings change) and the blocking dialog never
	// holds a lock. An empty selection means the user cancelled.
	selected, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Select a background image",
		Filters: []runtime.FileFilter{
			{DisplayName: "Images (*.png;*.jpg;*.jpeg;*.webp;*.gif;*.svg)", Pattern: "*.png;*.jpg;*.jpeg;*.webp;*.gif;*.svg"},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to open file picker: %w", err)
	}
	if selected == "" {
		return nil, nil
	}

	// Serialize the theme-file mutations so the fork's stat-then-write and
	// the background re-marshal can't race a concurrent ImportTheme or a
	// second PickBackgroundImage (#404). The settings write inside this
	// section goes through UpdateSettings (settingsWriteMu), so the lock
	// ordering is themeWriteMu → settingsWriteMu — never reversed.
	//
	// Compute themesDir from the snapshotted vaultPath (not a.themesDir(),
	// which re-reads a.vaultPath without a lock) so the write targets the
	// correct absolute directory even if the vault closed during the dialog
	// above (#404 hardening).
	a.themeWriteMu.Lock()
	defer a.themeWriteMu.Unlock()
	// Re-check the vault hasn't closed during the (indefinite) native dialog
	// above. If CloseVault ran while the user was picking a file, vaultPath is
	// now stale — writing to it + persisting the fork as ActiveTheme would
	// snap the next launch's theme resolution to a file that no longer exists
	// on a different vault. The re-check is a brief vaultMu.RLock (not nested
	// with themeWriteMu — vaultMu is acquired-and-released inside themeWriteMu),
	// matching the lock layering proven safe above (#404 TOCTOU hardening).
	a.vaultMu.RLock()
	closed := a.vaultPath != vaultPath
	a.vaultMu.RUnlock()
	if closed {
		return nil, fmt.Errorf("vault closed during background pick")
	}
	themesDir := filepath.Join(vaultPath, ".system", "themes")

	// If the active theme is embedded-only (no on-disk file), fork it so the
	// background edit targets a writable copy. Mirrors the importer's "user-"
	// namespace for built-in id collisions; a pre-existing fork is reused.
	targetID := settings.ActiveTheme
	forked := false
	if _, found, err := themes.LoadByID(themesDir, settings.ActiveTheme); err != nil {
		return nil, fmt.Errorf("failed to look up theme %q: %w", settings.ActiveTheme, err)
	} else if !found {
		forkedID, err := themes.ForkEmbeddedTheme(themesDir, settings.ActiveTheme)
		if err != nil {
			return nil, err
		}
		targetID = forkedID
		forked = true
		if _, err := vault.UpdateSettings(func(s *vault.AppSettings) {
			s.ActiveTheme = targetID
		}); err != nil {
			return nil, fmt.Errorf("failed to persist forked theme selection: %w", err)
		}
	}

	ref, isBase64, err := themes.StoreBackgroundAsset(themesDir, targetID, selected)
	if err != nil {
		return nil, err
	}
	// A picked photo is the common case: cover the surface at full opacity.
	// Without these defaults Opacity is the zero value (0), which emitBackground
	// writes verbatim and the overlay CSS applies as fully transparent — a
	// "successful" pick that renders nothing.
	bg := themes.Background{Image: ref, Size: "cover", Opacity: 1.0}
	if err := themes.SetThemeBackgroundImage(themesDir, targetID, zone, bg); err != nil {
		return nil, err
	}
	themes.InvalidateThemeCache(targetID)
	if a.ctx != nil {
		// Emit theme:changed (singular) so the active theme's tokens —
		// including the freshly-written --silt-bg-<zone>-image — re-inject
		// immediately. Mirrors ApplyTheme's emission; without it the user
		// would have to switch theme or mode to see the new background.
		// targetID is the on-disk theme the asset was written to (a fork
		// counts as the new active theme — its selection was persisted
		// above), and settings.ThemeMode is the unchanged current mode.
		runtime.EventsEmit(a.ctx, "theme:changed", map[string]string{
			"id": targetID, "mode": settings.ThemeMode,
		})
		// themes:changed (plural) refreshes the picker listing so the
		// forked theme appears / the cached entry is dropped.
		runtime.EventsEmit(a.ctx, "themes:changed", struct{}{})
	}
	log.Printf("themes: PickBackgroundImage(zone=%q) → theme %q forked=%v base64=%v", zone, targetID, forked, isBase64)
	return &BackgroundImageResult{
		ThemeID:   targetID,
		Forked:    forked,
		Zone:      zone,
		Reference: ref,
		Base64:    isBase64,
	}, nil
}

// PickExportPath opens the native save-file dialog (filtered to *.json)
// and returns the chosen path. The empty string means the user
// cancelled. The frontend feeds the returned path to ExportActiveTheme.
// defaultFilename is offered as the initial file name (e.g.
// "<theme-id>.json"); pass "" to let the OS pick a default.
func (a *App) PickExportPath(defaultFilename string) (string, error) {
	if a.ctx == nil {
		return "", fmt.Errorf("application context not ready")
	}
	return runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "Export active theme",
		DefaultFilename: defaultFilename,
		Filters: []runtime.FileFilter{
			{DisplayName: "Silt Theme (*.json)", Pattern: "*.json"},
		},
	})
}

// ExportActiveTheme writes the currently active theme verbatim to
// dstPath as JSON, so the user can round-trip edit it (and re-import).
// The active id is read from AppSettings; the embedded default ships
// even when the on-disk copy is missing.
func (a *App) ExportActiveTheme(dstPath string) error {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.vaultPath == "" {
		return fmt.Errorf("vault not loaded")
	}
	settings, err := vault.LoadSettings()
	if err != nil {
		return fmt.Errorf("failed to load settings: %w", err)
	}
	return themes.ExportThemeToPath(a.themesDir(), settings.ActiveTheme, dstPath)
}
