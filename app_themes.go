package main

import (
	"fmt"
	"log"
	"path/filepath"
	"strings"

	"silt/backend/themes"
	"silt/backend/vault"
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
//
// themeWriteMu serializes ActiveTheme writes with DeleteCustomTheme's
// active-theme guard (TOCTOU): Delete checks ActiveTheme under the same
// mutex before removing a theme file. Do NOT hold vaultMu across the
// themeWriteMu acquisition — lock order is snapshot vaultPath under
// vaultMu.RLock, release, then themeWriteMu (optionally re-check vault
// under a brief RLock while holding themeWriteMu).
func (a *App) ApplyTheme(id, mode string) (ActiveThemeResult, error) {
	if !vault.ValidThemeMode(mode) {
		return ActiveThemeResult{}, fmt.Errorf("invalid mode %q (valid: dark, light, system)", mode)
	}

	// Snapshot vault path / themesDir under vaultMu, then release before any
	// themeWriteMu work so we never hold vaultMu while waiting on themeWriteMu
	// (deadlock risk with handlers that take themeWriteMu then vaultMu.RLock).
	a.vaultMu.RLock()
	vaultPath := a.vaultPath
	themesDir := a.themesDir()
	a.vaultMu.RUnlock()
	if vaultPath == "" {
		return ActiveThemeResult{}, fmt.Errorf("vault not loaded")
	}

	// Resolve the requested theme in one pass (read-only; no themeWriteMu).
	// The embedded default is always available; any other id must live on
	// disk or in the first-class embed roster. A typo or stale id errors
	// here rather than silently snapping to the default.
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
		t, found, err = themes.LoadByID(themesDir, id)
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

	// Serialize ActiveTheme persistence with DeleteCustomTheme's active check.
	a.themeWriteMu.Lock()
	defer a.themeWriteMu.Unlock()

	// Re-check vault still open at the snapshotted path before writing
	// settings (mirrors DeleteCustomTheme / SaveCustomTheme #404 hardening).
	a.vaultMu.RLock()
	closed := a.vaultPath != vaultPath
	a.vaultMu.RUnlock()
	if closed {
		return ActiveThemeResult{}, fmt.Errorf("vault closed during theme apply")
	}

	// Persist the selection atomically. Use the actually-resolved theme id
	// (t.ID) rather than the requested id: if the caller requested the
	// embedded default and the file vanished mid-request, settings stays
	// consistent with what is rendered.
	// Lock order: themeWriteMu (held) → settingsWriteMu (inside UpdateSettings).
	if _, err := vault.UpdateSettings(func(s *vault.AppSettings) {
		s.ActiveTheme = t.ID
		s.ThemeMode = mode
	}); err != nil {
		return ActiveThemeResult{}, fmt.Errorf("failed to persist theme selection: %w", err)
	}

	res := buildThemeResult(t, mode)
	log.Printf("themes: ApplyTheme(id=%q mode=%q) → resolved %q", id, mode, t.ID)
	a.emit("theme:changed", map[string]string{
		"id": t.ID, "mode": mode,
	})
	return res, nil
}

// PickThemeFile opens the native file picker (filtered to *.json) and
// returns the chosen path. The empty string means the user cancelled. The
// frontend feeds the returned path to ImportTheme — the backend does all
// validation and writing, so the frontend never touches the filesystem
// directly.
func (a *App) PickThemeFile() (string, error) {
	if a.wailsApp == nil {
		return "", fmt.Errorf("application context not ready")
	}
	selected, err := a.openFileDialog("Select a theme JSON", []FileFilter{
		{DisplayName: "Silt Theme (*.json)", Pattern: "*.json"},
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
	a.emit("themes:changed", struct{}{})
	return res, nil
}

// PickExportPath opens the native save-file dialog (filtered to *.json)
// and returns the chosen path. The empty string means the user
// cancelled. The frontend feeds the returned path to ExportActiveTheme.
// defaultFilename is offered as the initial file name (e.g.
// "<theme-id>.json"); pass "" to let the OS pick a default.
func (a *App) PickExportPath(defaultFilename string) (string, error) {
	if a.wailsApp == nil {
		return "", fmt.Errorf("application context not ready")
	}
	return a.saveFileDialog("Export active theme", defaultFilename, []FileFilter{
		{DisplayName: "Silt Theme (*.json)", Pattern: "*.json"},
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

// --- Custom theme editor IPC (#393, #401) ----------------------------------

// SaveCustomThemeRequest is the IPC payload for SaveCustomTheme.
// JSON is the full v2 theme document; Overwrite replaces an existing on-disk
// custom theme (refused for built-ins); Apply selects the saved theme as
// active and emits theme:changed; Name optionally overrides the theme name
// before save (and drives the new-id slug when Overwrite is false).
type SaveCustomThemeRequest struct {
	JSON      string `json:"json"`
	Overwrite bool   `json:"overwrite"`
	Apply     bool   `json:"apply"`
	Name      string `json:"name,omitempty"`
}

// SaveCustomThemeResult is returned by SaveCustomTheme after a successful write.
type SaveCustomThemeResult struct {
	Info    themes.ThemeInfo `json:"info"`
	Applied bool             `json:"applied"`
}

// GetThemeJSON returns the canonical JSON string for the theme with the given
// id (on-disk custom first, else embedded first-class). Used by the custom
// theme editor to seed its working copy.
func (a *App) GetThemeJSON(id string) (string, error) {
	a.vaultMu.RLock()
	themesDir := a.themesDir()
	a.vaultMu.RUnlock()
	raw, err := themes.GetThemeJSON(themesDir, id)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// SaveCustomTheme validates and persists a custom theme. overwrite=false
// always allocates a new id from the name; overwrite=true replaces an
// existing on-disk custom theme only. When Apply is true the saved theme
// becomes active (themeWriteMu → UpdateSettings) and theme:changed is
// emitted. themes:changed is always emitted on success so the picker refreshes.
func (a *App) SaveCustomTheme(req SaveCustomThemeRequest) (*SaveCustomThemeResult, error) {
	a.vaultMu.RLock()
	vaultPath := a.vaultPath
	a.vaultMu.RUnlock()
	if vaultPath == "" {
		return nil, fmt.Errorf("vault not loaded")
	}
	if strings.TrimSpace(req.JSON) == "" {
		return nil, fmt.Errorf("theme JSON is empty")
	}

	t, err := themes.ParseAndValidate([]byte(req.JSON))
	if err != nil {
		return nil, err
	}
	if name := strings.TrimSpace(req.Name); name != "" {
		t.Name = name
	}

	themesDir := filepath.Join(vaultPath, ".system", "themes")
	a.themeWriteMu.Lock()
	defer a.themeWriteMu.Unlock()

	// Re-check vault still open at the snapshotted path before file IO
	// (mirrors PickBackgroundImage #404 hardening).
	a.vaultMu.RLock()
	closed := a.vaultPath != vaultPath
	a.vaultMu.RUnlock()
	if closed {
		return nil, fmt.Errorf("vault closed during theme save")
	}

	info, err := themes.SaveCustomTheme(themesDir, t, req.Overwrite)
	if err != nil {
		log.Printf("themes: SaveCustomTheme failed: %v", err)
		return nil, err
	}

	result := &SaveCustomThemeResult{Info: *info}
	if req.Apply {
		settings, err := vault.LoadSettings()
		if err != nil {
			return nil, fmt.Errorf("failed to load settings: %w", err)
		}
		// Lock order: themeWriteMu (held) → settingsWriteMu (inside UpdateSettings).
		if _, err := vault.UpdateSettings(func(s *vault.AppSettings) {
			s.ActiveTheme = info.ID
		}); err != nil {
			return nil, fmt.Errorf("failed to persist active theme: %w", err)
		}
		a.emit("theme:changed", map[string]string{
			"id": info.ID, "mode": settings.ThemeMode,
		})
		result.Applied = true
	}

	log.Printf("themes: SaveCustomTheme → id=%q overwrite=%v applied=%v", info.ID, req.Overwrite, result.Applied)
	a.emit("themes:changed", struct{}{})
	return result, nil
}

// RenameCustomTheme updates only the display name of an on-disk custom theme.
// Built-in (embedded) ids are refused.
func (a *App) RenameCustomTheme(id, name string) error {
	a.vaultMu.RLock()
	vaultPath := a.vaultPath
	a.vaultMu.RUnlock()
	if vaultPath == "" {
		return fmt.Errorf("vault not loaded")
	}
	themesDir := filepath.Join(vaultPath, ".system", "themes")
	a.themeWriteMu.Lock()
	defer a.themeWriteMu.Unlock()

	// Re-check vault still open at the snapshotted path before file IO
	// (mirrors PickBackgroundImage #404 hardening).
	a.vaultMu.RLock()
	closed := a.vaultPath != vaultPath
	a.vaultMu.RUnlock()
	if closed {
		return fmt.Errorf("vault closed during theme rename")
	}

	if err := themes.RenameCustomTheme(themesDir, id, name); err != nil {
		return err
	}
	// Listing always refreshes. When the renamed theme is active, also emit
	// theme:changed so listeners that only watch the active theme pick up
	// the new display name (#533).
	settings, err := vault.LoadSettings()
	if err == nil && settings.ActiveTheme == id {
		mode := settings.ThemeMode
		if mode == "" {
			mode = "dark"
		}
		a.emit("theme:changed", map[string]string{
			"id": id, "mode": mode, "name": name,
		})
	}
	a.emit("themes:changed", struct{}{})
	return nil
}

// DeleteCustomTheme removes an on-disk custom theme and its assets directory.
// Refuses the active theme, built-in ids, and missing files. Active-theme
// check runs under themeWriteMu so a concurrent ApplyTheme cannot race the
// delete past the guard (TOCTOU).
func (a *App) DeleteCustomTheme(id string) error {
	a.vaultMu.RLock()
	vaultPath := a.vaultPath
	a.vaultMu.RUnlock()
	if vaultPath == "" {
		return fmt.Errorf("vault not loaded")
	}

	themesDir := filepath.Join(vaultPath, ".system", "themes")
	// Acquire themeWriteMu BEFORE LoadSettings/active check so ApplyTheme
	// (which also holds themeWriteMu when writing ActiveTheme) cannot slip
	// the active id past this guard between check and delete.
	a.themeWriteMu.Lock()
	defer a.themeWriteMu.Unlock()

	// Re-check vault still open at the snapshotted path before any IO.
	a.vaultMu.RLock()
	closed := a.vaultPath != vaultPath
	a.vaultMu.RUnlock()
	if closed {
		return fmt.Errorf("vault closed during theme delete")
	}

	settings, err := vault.LoadSettings()
	if err != nil {
		return fmt.Errorf("failed to load settings: %w", err)
	}
	if settings.ActiveTheme == id {
		return fmt.Errorf("cannot delete the active theme %q; switch themes first", id)
	}

	if err := themes.DeleteCustomTheme(themesDir, id); err != nil {
		return err
	}
	log.Printf("themes: DeleteCustomTheme(%q)", id)
	a.emit("themes:changed", struct{}{})
	return nil
}
