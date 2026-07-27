package main

import (
	"fmt"
	"log"
	"path/filepath"

	"silt/backend/themes"
	"silt/backend/vault"
)

// --- Theme background-asset staging IPC (#391, #401) -----------------------
//
// The asset-staging cluster is a distinct sub-concern from theme-metadata
// CRUD (GetThemeJSON / SaveCustomTheme / RenameCustomTheme / DeleteCustomTheme
// / import-export, which live in app_themes.go). It covers the per-zone
// background-image picker, the editor's preview-only staging, and staging
// cleanup.

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
	if a.wailsApp == nil {
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
	selected, err := a.openFileDialog("Select a background image", []FileFilter{
		{DisplayName: "Images (*.png;*.jpg;*.jpeg;*.webp;*.gif;*.svg)", Pattern: "*.png;*.jpg;*.jpeg;*.webp;*.gif;*.svg"},
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
	// Emit theme:changed (singular) so the active theme's tokens —
	// including the freshly-written --silt-bg-<zone>-image — re-inject
	// immediately. Mirrors ApplyTheme's emission; without it the user
	// would have to switch theme or mode to see the new background.
	// targetID is the on-disk theme the asset was written to (a fork
	// counts as the new active theme — its selection was persisted
	// above), and settings.ThemeMode is the unchanged current mode.
	a.emit(EventThemeChanged, map[string]string{
		"id": targetID, "mode": settings.ThemeMode,
	})
	// themes:changed (plural) refreshes the picker listing so the
	// forked theme appears / the cached entry is dropped.
	a.emit(EventThemesChanged, struct{}{})
	log.Printf("themes: PickBackgroundImage(zone=%q) → theme %q forked=%v base64=%v", zone, targetID, forked, isBase64)
	return &BackgroundImageResult{
		ThemeID:   targetID,
		Forked:    forked,
		Zone:      zone,
		Reference: ref,
		Base64:    isBase64,
	}, nil
}

// PrepareBackgroundAssetResult is the IPC payload for PrepareBackgroundAsset:
// a CSS background-image reference for the editor working copy, plus whether
// the asset was inlined as a data URI.
type PrepareBackgroundAssetResult struct {
	Reference string `json:"reference"`
	Base64    bool   `json:"base64"`
}

// PickImageFile opens the native image picker and returns the chosen path
// without writing anything. Empty string means the user cancelled. The
// frontend feeds the path to PrepareBackgroundAsset for editor preview.
func (a *App) PickImageFile() (string, error) {
	if a.wailsApp == nil {
		return "", fmt.Errorf("application context not ready")
	}
	selected, err := a.openFileDialog("Select an image", []FileFilter{
		{DisplayName: "Images (*.png;*.jpg;*.jpeg;*.webp;*.gif;*.svg)", Pattern: "*.png;*.jpg;*.jpeg;*.webp;*.gif;*.svg"},
	})
	if err != nil {
		return "", fmt.Errorf("failed to open file picker: %w", err)
	}
	return selected, nil
}

// PrepareBackgroundAsset stages an image for the custom theme editor without
// mutating any theme file. Small images become data URIs; large ones land in
// <themesDir>/_editor.assets/ and return a relative url() reference that
// themeAssetHandler can serve for live preview.
func (a *App) PrepareBackgroundAsset(srcPath string) (*PrepareBackgroundAssetResult, error) {
	a.vaultMu.RLock()
	vaultPath := a.vaultPath
	a.vaultMu.RUnlock()
	if vaultPath == "" {
		return nil, fmt.Errorf("vault not loaded")
	}
	themesDir := filepath.Join(vaultPath, ".system", "themes")
	// Staging writes are serialized with other theme-dir mutations so a
	// concurrent delete/import cannot race MkdirAll/WriteFile on the same tree.
	a.themeWriteMu.Lock()
	defer a.themeWriteMu.Unlock()

	// Re-check vault still open at the snapshotted path before writing
	// into themesDir (mirrors DeleteCustomTheme / SaveCustomTheme).
	a.vaultMu.RLock()
	closed := a.vaultPath != vaultPath
	a.vaultMu.RUnlock()
	if closed {
		return nil, fmt.Errorf("vault closed during background asset prepare")
	}

	ref, isBase64, err := themes.PrepareBackgroundAsset(themesDir, srcPath)
	if err != nil {
		return nil, err
	}
	return &PrepareBackgroundAssetResult{Reference: ref, Base64: isBase64}, nil
}

// ClearEditorStaging removes themesDir/_editor.assets/ so discarded image
// picks do not accumulate between editor sessions.
func (a *App) ClearEditorStaging() error {
	a.vaultMu.RLock()
	vaultPath := a.vaultPath
	a.vaultMu.RUnlock()
	if vaultPath == "" {
		return fmt.Errorf("vault not loaded")
	}
	themesDir := filepath.Join(vaultPath, ".system", "themes")
	a.themeWriteMu.Lock()
	defer a.themeWriteMu.Unlock()

	a.vaultMu.RLock()
	closed := a.vaultPath != vaultPath
	a.vaultMu.RUnlock()
	if closed {
		return fmt.Errorf("vault closed during editor staging clear")
	}

	return themes.ClearEditorStaging(themesDir)
}
