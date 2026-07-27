package main

import "testing"

// TestMenuEventNames: emit must be callable with each menu EventName const
// without panicking. emit no-ops when wailsApp is nil, so this is a compile-time
// + runtime smoke test that the menu event consts are valid EventName values the
// App.emit contract accepts (and that emit is safe to call pre-init). The consts
// here must stay in sync with the siltApp.emit calls in menus.go.
func TestMenuEventNames(t *testing.T) {
	app := &App{}
	for _, name := range []EventName{
		EventMenuNewPage, EventMenuOpenVault, EventMenuSave,
		EventMenuToggleSidebar, EventMenuToggleFormatToolbar,
		EventMenuFind, EventMenuFocusMode, EventMenuSettings, EventMenuAbout,
	} {
		app.emit(name) // no-op when wailsApp is nil; verifies no panic
	}
}
