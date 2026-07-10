package main

import "testing"

// TestCloseToTrayEnabled_DefaultOff: with no settings.json the field is nil,
// so closeToTrayEnabled must report false — the documented default-off
// behaviour (an absent CloseToTray pointer means the window closes normally).
func TestCloseToTrayEnabled_DefaultOff(t *testing.T) {
	configDirOverride(t)
	if closeToTrayEnabled() {
		t.Fatalf("closeToTrayEnabled() = true; want false when CloseToTray is unset")
	}
}

// TestSetCloseToTray: SetCloseToTray must be callable on an *App whose
// wailsApp is nil (it never touches the Wails app — only settings on disk)
// and must round-trip through settings.json.
func TestSetCloseToTray(t *testing.T) {
	configDirOverride(t)
	app := &App{}
	if err := app.SetCloseToTray(true); err != nil {
		t.Fatalf("SetCloseToTray(true) failed: %v", err)
	}
	if !closeToTrayEnabled() {
		t.Fatalf("closeToTrayEnabled() = false after SetCloseToTray(true)")
	}
}

// TestGetCloseToTray: the getter is the frontend's source of truth for the
// toggle's initial state, so it must default off (no settings.json) and mirror
// every SetCloseToTray round-trip — otherwise the UI would show the wrong
// switch position on mount after a toggle+restart.
func TestGetCloseToTray(t *testing.T) {
	configDirOverride(t)
	app := &App{}
	if app.GetCloseToTray() {
		t.Fatalf("GetCloseToTray() = true; want false when CloseToTray is unset")
	}
	if err := app.SetCloseToTray(true); err != nil {
		t.Fatalf("SetCloseToTray(true) failed: %v", err)
	}
	if !app.GetCloseToTray() {
		t.Fatalf("GetCloseToTray() = false after SetCloseToTray(true)")
	}
	if err := app.SetCloseToTray(false); err != nil {
		t.Fatalf("SetCloseToTray(false) failed: %v", err)
	}
	if app.GetCloseToTray() {
		t.Fatalf("GetCloseToTray() = true after SetCloseToTray(false)")
	}
}

// TestRequestClose_NoWailsApp: RequestClose is a safe no-op when wailsApp is
// nil — the guard returns before touching the window or re-reading settings.
func TestRequestClose_NoWailsApp(t *testing.T) {
	app := &App{}
	app.RequestClose() // must not panic
}

// TestQuit_NoWailsApp: Quit is a safe no-op when wailsApp is nil.
func TestQuit_NoWailsApp(t *testing.T) {
	app := &App{}
	app.Quit() // must not panic
}
