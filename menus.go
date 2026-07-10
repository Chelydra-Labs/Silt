package main

import (
	"github.com/wailsapp/wails/v3/pkg/application"
)

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
	fileMenu.Add("New Page").SetAccelerator("Ctrl+N").OnClick(func(ctx *application.Context) {
		siltApp.emit("menu:new-page")
	})
	fileMenu.Add("Open Vault...").SetAccelerator("Ctrl+O").OnClick(func(ctx *application.Context) {
		siltApp.emit("menu:open-vault")
	})
	fileMenu.Add("Save").SetAccelerator("Ctrl+S").OnClick(func(ctx *application.Context) {
		siltApp.emit("menu:save")
	})
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
	viewMenu.Add("Toggle Sidebar").SetAccelerator("Ctrl+B").OnClick(func(ctx *application.Context) {
		siltApp.emit("menu:toggle-sidebar")
	})
	viewMenu.Add("Toggle Format Toolbar").OnClick(func(ctx *application.Context) {
		siltApp.emit("menu:toggle-format-toolbar")
	})
	viewMenu.Add("Find...").SetAccelerator("Ctrl+F").OnClick(func(ctx *application.Context) {
		siltApp.emit("menu:find")
	})
	viewMenu.Add("Focus Mode").OnClick(func(ctx *application.Context) {
		siltApp.emit("menu:focus-mode")
	})
	viewMenu.AddSeparator()
	viewMenu.Add("Settings...").SetAccelerator("Ctrl+,").OnClick(func(ctx *application.Context) {
		siltApp.emit("menu:settings")
	})

	// --- Help ---
	helpMenu := menu.AddSubmenu("Help")
	helpMenu.Add("About Silt").OnClick(func(ctx *application.Context) {
		siltApp.emit("menu:about")
	})

	app.Menu.SetApplicationMenu(menu)
}
