package main

import "testing"

// TestMenuEventNames: emit must be callable with each menu event name without
// panicking. emit no-ops when wailsApp is nil, so this is a compile-time +
// runtime smoke test that the event names in menus.go are valid string events
// the App.emit contract accepts (and that emit is safe to call pre-init).
// The names here must stay in sync with the siltApp.emit calls in menus.go.
func TestMenuEventNames(t *testing.T) {
	app := &App{}
	for _, name := range []string{
		"menu:new-page", "menu:open-vault", "menu:save",
		"menu:toggle-sidebar", "menu:toggle-format-toolbar",
		"menu:find", "menu:focus-mode", "menu:settings", "menu:about",
	} {
		app.emit(name) // no-op when wailsApp is nil; verifies no panic
	}
}
