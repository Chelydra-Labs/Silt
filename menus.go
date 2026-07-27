package main

import (
	"log"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// safeMenuCallback wraps a menu/tray OnClick closure so a panic (nil receiver,
// platform-specific window API quirk, future nil-deref) is recovered and logged
// instead of crashing the process. The callbacks run on a Wails event-dispatch
// goroutine with no recovery, so without this a panic is fatal —
// ServiceShutdown never runs, the WAL is not checkpointed, and in-flight plugin
// hooks are skipped. The cost is trivial; the resilience gain is significant.
func safeMenuCallback(name string, fn func(*application.Context)) func(*application.Context) {
	return func(ctx *application.Context) {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("menu/tray callback %q panic recovered: %v", name, r)
			}
		}()
		fn(ctx)
	}
}

// setupMenus creates the platform-aware application menu and wires menu items
// to frontend actions via Wails events (#503). Standard editing roles (Undo,
// Redo, Cut, Copy, Paste, Select All) use v3's built-in platform handling so
// keyboard shortcuts work natively without custom JS dispatch. Custom items
// (New Page, Open Vault, Save, Find, Toggle Sidebar, Settings) emit events
// that App.svelte listens for — the same handlers the hotkeys use.
//
// On Windows/Linux the menu is a classic menu bar. macOS is not shipped
// (removed per owner decision); the menu structure is still platform-correct
// if macOS is added later (standard roles + app menu).
func setupMenus(app *application.App, siltApp *App) {
	menu := app.NewMenu()

	// --- File ---
	fileMenu := menu.AddSubmenu("File")
	fileMenu.Add("New Page").SetAccelerator("Ctrl+N").OnClick(safeMenuCallback("new-page", func(ctx *application.Context) {
		siltApp.emit(EventMenuNewPage)
	}))
	fileMenu.Add("Open Vault...").SetAccelerator("Ctrl+O").OnClick(safeMenuCallback("open-vault", func(ctx *application.Context) {
		siltApp.emit(EventMenuOpenVault)
	}))
	fileMenu.Add("Save").SetAccelerator("Ctrl+S").OnClick(safeMenuCallback("save", func(ctx *application.Context) {
		siltApp.emit(EventMenuSave)
	}))
	fileMenu.AddSeparator()
	fileMenu.AddRole(application.Quit)

	// --- Edit (standard roles — platform handles shortcuts natively) ---
	editMenu := menu.AddSubmenu("Edit")
	editMenu.AddRole(application.Undo)
	editMenu.AddRole(application.Redo)
	editMenu.AddSeparator()
	editMenu.AddRole(application.Cut)
	editMenu.AddRole(application.Copy)
	editMenu.AddRole(application.Paste)
	editMenu.AddRole(application.SelectAll)

	// --- View ---
	viewMenu := menu.AddSubmenu("View")
	viewMenu.Add("Toggle Sidebar").SetAccelerator("Ctrl+B").OnClick(safeMenuCallback("toggle-sidebar", func(ctx *application.Context) {
		siltApp.emit(EventMenuToggleSidebar)
	}))
	viewMenu.Add("Toggle Format Toolbar").OnClick(safeMenuCallback("toggle-format-toolbar", func(ctx *application.Context) {
		siltApp.emit(EventMenuToggleFormatToolbar)
	}))
	viewMenu.Add("Find...").SetAccelerator("Ctrl+F").OnClick(safeMenuCallback("find", func(ctx *application.Context) {
		siltApp.emit(EventMenuFind)
	}))
	viewMenu.Add("Focus Mode").OnClick(safeMenuCallback("focus-mode", func(ctx *application.Context) {
		siltApp.emit(EventMenuFocusMode)
	}))
	viewMenu.AddSeparator()
	viewMenu.Add("Settings...").SetAccelerator("Ctrl+,").OnClick(safeMenuCallback("settings", func(ctx *application.Context) {
		siltApp.emit(EventMenuSettings)
	}))
	// Dev Mode Inspect (#679/#684). Disabled when Dev Mode and SILT_DEBUG are
	// both off so the item stays discoverable but inert. Label is "Open" not
	// "Toggle" — the API only opens the inspector.
	devToolsItem := viewMenu.Add("Open Developer Tools").
		SetAccelerator("Ctrl+Shift+F12").
		OnClick(safeMenuCallback("open-devtools", func(ctx *application.Context) {
			if err := siltApp.OpenDevTools(); err != nil {
				log.Printf("OpenDevTools: %v", err)
			}
		}))
	siltApp.openDevToolsMenuItem = devToolsItem
	siltApp.syncOpenDevToolsMenuItem()

	// --- Help ---
	helpMenu := menu.AddSubmenu("Help")
	helpMenu.Add("About Silt").OnClick(safeMenuCallback("about", func(ctx *application.Context) {
		siltApp.emit(EventMenuAbout)
	}))

	app.Menu.SetApplicationMenu(menu)
}
