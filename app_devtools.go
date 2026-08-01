package main

import (
	"fmt"
	"os"
	"strings"

	"silt/backend/config"
)

// DevTools / editor UI-preference bindings. The atomic flag setters here
// (format toolbar, focus mode, open-DevTools-on-startup) avoid the full
// saveConfig path so they don't clobber an unsaved settings draft. The runtime
// Dev Mode gate (devToolsRuntimeEnabled) mirrors main.go's launch-time
// shouldOpenDevtools: SILT_DEBUG=1 or the vault Dev Mode flag.

// SetShowFormatToolbar atomically writes the format-toolbar visibility to
// config.yaml. It exists so the global format-toolbar toggle (hotkey / floating
// button) does NOT route through the frontend's saveConfig — that path clears
// the settings dirty flag and would silently clobber a user's unsaved EditorTab
// draft. Mirrors AppendDismissedTip: lock, mutate the one field, self-write
// suppress, save. The frontend mirrors the field into its config snapshot
// without touching dirty.
func (a *App) SetShowFormatToolbar(value bool) error {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.vaultPath == "" {
		return fmt.Errorf("vault not loaded")
	}
	a.configMu.Lock()
	defer a.configMu.Unlock()
	a.cfg.UI.ShowFormatToolbar = &value
	return a.saveConfigTracked(a.cfg)
}

// SetNoteZoom atomically writes the per-vault note content zoom factor (#849).
// Same rationale as SetShowFormatToolbar — wheel/GUI must not clobber an
// unsaved settings draft. Value is clamped to [0.7, 2.0] on 0.1 steps.
func (a *App) SetNoteZoom(value float64) error {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.vaultPath == "" {
		return fmt.Errorf("vault not loaded")
	}
	a.configMu.Lock()
	defer a.configMu.Unlock()
	// Clamp via the same normalize helper used on load.
	a.cfg.UI.NoteZoom = config.NormalizeNoteZoomForSet(value)
	return a.saveConfigTracked(a.cfg)
}

// SetFocusMode atomically writes the editor focus-mode flag. Same rationale as
// SetShowFormatToolbar — avoids clobbering an unsaved settings draft.
func (a *App) SetFocusMode(value bool) error {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.vaultPath == "" {
		return fmt.Errorf("vault not loaded")
	}
	a.configMu.Lock()
	defer a.configMu.Unlock()
	a.cfg.Editor.FocusMode = &value
	return a.saveConfigTracked(a.cfg)
}

// SetOpenDevtoolsOnStartup atomically writes the Dev Mode (open DevTools on
// startup) flag. The About → Developer toggle routes through here instead of
// the full-config saveConfig path because that path clones a Svelte 5 $state
// proxy via structuredClone (throws DataCloneError in the webview, silently
// swallowed) and would clobber an unsaved EditorTab draft. Mirrors
// SetShowFormatToolbar.
func (a *App) SetOpenDevtoolsOnStartup(value bool) error {
	a.vaultMu.RLock()
	if a.vaultPath == "" {
		a.vaultMu.RUnlock()
		return fmt.Errorf("vault not loaded")
	}
	a.configMu.Lock()
	a.cfg.UI.OpenDevtoolsOnStartup = &value
	err := a.saveConfigTracked(a.cfg)
	a.configMu.Unlock()
	a.vaultMu.RUnlock()
	if err != nil {
		return err
	}
	// After locks released — sync takes vaultMu/configMu via devToolsRuntimeEnabled.
	a.syncOpenDevToolsMenuItem()
	return nil
}

// syncOpenDevToolsMenuItem enables View → Open Developer Tools when runtime
// DevTools are allowed (vault Dev Mode or SILT_DEBUG=1). Prefer disabled over
// hidden so the item stays visible in the menu structure (#684).
// Must not be called while holding vaultMu exclusively (RLock would deadlock).
func (a *App) syncOpenDevToolsMenuItem() {
	a.syncOpenDevToolsMenuItemEnabled(a.devToolsRuntimeEnabled())
}

// syncOpenDevToolsMenuItemEnabled applies a precomputed enable flag — use when
// the caller already holds vaultMu (e.g. initializeVaultServices).
func (a *App) syncOpenDevToolsMenuItemEnabled(enabled bool) {
	if a.openDevToolsMenuItem == nil {
		return
	}
	a.openDevToolsMenuItem.SetEnabled(enabled)
}

// OpenDevTools opens the webview developer tools when Dev Mode is enabled
// (#679). No-op (returns nil) when the flag is off, SILT_DEBUG is unset, the
// main window is unavailable, or no vault is loaded — production builds
// without the Wails devtools tag may also no-op at the platform layer.
//
// Runtime gate matches launch-time shouldOpenDevtools: vault config flag OR
// SILT_DEBUG=1. Vault is not required when SILT_DEBUG is set (process-global).
func (a *App) OpenDevTools() error {
	if a.mainWindow == nil {
		return nil
	}
	if !a.devToolsRuntimeEnabled() {
		return nil
	}
	a.mainWindow.OpenDevTools()
	return nil
}

// devToolsRuntimeEnabled reports whether runtime OpenDevTools should proceed.
// Mirrors shouldOpenDevtools (main.go): SILT_DEBUG=1 or vault Dev Mode flag.
func (a *App) devToolsRuntimeEnabled() bool {
	if strings.EqualFold(os.Getenv("SILT_DEBUG"), "1") {
		return true
	}
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.vaultPath == "" {
		return false
	}
	a.configMu.RLock()
	defer a.configMu.RUnlock()
	return a.cfg.UI.OpenDevtoolsOnStartup != nil && *a.cfg.UI.OpenDevtoolsOnStartup
}
