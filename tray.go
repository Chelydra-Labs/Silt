package main

import (
	_ "embed"
	"log"

	"silt/backend/vault"

	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed build/appicon.png
var trayIconBytes []byte

// closeToTrayEnabled reads the user-global setting. Default-off: an absent
// field (nil pointer) means the window closes normally. The setting is
// user-global because the window exists before any vault is open.
func closeToTrayEnabled() bool {
	settings, err := vault.LoadSettings()
	if err != nil || settings.CloseToTray == nil {
		return false
	}
	return *settings.CloseToTray
}

// SetCloseToTray persists the close-to-tray preference to settings.json.
func (a *App) SetCloseToTray(enabled bool) error {
	settings, err := vault.LoadSettings()
	if err != nil {
		return err
	}
	settings.CloseToTray = &enabled
	return vault.SaveSettings(settings)
}

// RequestClose is the canonical window-close handler. When close-to-tray is
// enabled, the window hides to the tray instead of quitting (#501). The tray
// Quit menu item and the "quit" hotkey call Quit() directly to bypass this.
func (a *App) RequestClose() {
	if a.wailsApp == nil {
		return
	}
	if closeToTrayEnabled() {
		// Hide to tray — the process stays alive. The user restores via
		// the tray icon click or the Show menu item.
		if a.mainWindow != nil {
			a.mainWindow.Hide()
		}
		return
	}
	a.Quit()
}

// Quit triggers the application quit path. ServiceShutdown (the v3 lifecycle
// hook) drains in-flight calls, flushes WAL, stops watchers, and runs plugin
// onVaultClose hooks before the process exits. Called from the tray Quit
// menu item, the TitleBar close button (when close-to-tray is off), and the
// Application.Quit frontend binding.
func (a *App) Quit() {
	if a.wailsApp == nil {
		return
	}
	a.wailsApp.Quit()
}

// setupTray creates the system tray with a context menu (Show/Hide, Quit)
// and attaches the main window so a single click toggles visibility (#501).
func setupTray(app *application.App, siltApp *App, window application.Window) *application.SystemTray {
	menu := app.NewMenu()
	menu.Add("Show Silt").OnClick(safeMenuCallback("tray-show", func(ctx *application.Context) {
		window.Show().Focus()
	}))
	menu.Add("Hide Silt").OnClick(safeMenuCallback("tray-hide", func(ctx *application.Context) {
		window.Hide()
	}))
	menu.AddSeparator()
	menu.Add("Quit Silt").OnClick(safeMenuCallback("tray-quit", func(ctx *application.Context) {
		siltApp.Quit()
	}))

	tray := app.SystemTray.New()
	if err := tray.SetIcon(trayIconBytes); err != nil {
		log.Printf("tray: SetIcon failed: %v", err)
	}
	tray.SetTooltip("Silt")
	tray.SetMenu(menu)
	tray.AttachWindow(window)
	tray.Run()

	return tray
}
