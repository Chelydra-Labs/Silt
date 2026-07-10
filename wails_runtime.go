package main

import (
	"fmt"
)

// FileFilter mirrors the v2 runtime.FileFilter for dialog filter specs.
// Kept as a plain struct so dialog wrappers don't leak Wails types into
// the call sites.
type FileFilter struct {
	DisplayName string
	Pattern     string
}

// emit sends a Wails event to the frontend. No-ops when wailsApp is nil
// (tests have no Wails lifecycle, so event emission is silently skipped
// to preserve the pre-migration test behavior).
func (a *App) emit(name string, data ...any) {
	if a.wailsApp == nil {
		return
	}
	a.wailsApp.Event.Emit(name, data...)
}

// openDirectoryDialog opens a native folder picker. Returns "" on cancel.
func (a *App) openDirectoryDialog(title string) (string, error) {
	if a.wailsApp == nil {
		return "", fmt.Errorf("application not ready")
	}
	return a.wailsApp.Dialog.OpenFile().
		SetTitle(title).
		CanChooseDirectories(true).
		CanChooseFiles(false).
		PromptForSingleSelection()
}

// openFileDialog opens a native file picker with optional filters.
// Returns "" on cancel.
func (a *App) openFileDialog(title string, filters []FileFilter) (string, error) {
	if a.wailsApp == nil {
		return "", fmt.Errorf("application not ready")
	}
	d := a.wailsApp.Dialog.OpenFile().SetTitle(title)
	for _, f := range filters {
		d.AddFilter(f.DisplayName, f.Pattern)
	}
	return d.PromptForSingleSelection()
}

// saveFileDialog opens a native save-file picker. Returns "" on cancel.
func (a *App) saveFileDialog(title, defaultFilename string, filters []FileFilter) (string, error) {
	if a.wailsApp == nil {
		return "", fmt.Errorf("application not ready")
	}
	d := a.wailsApp.Dialog.SaveFile().SetMessage(title).SetFilename(defaultFilename)
	for _, f := range filters {
		d.AddFilter(f.DisplayName, f.Pattern)
	}
	return d.PromptForSingleSelection()
}

// clipboardGetText reads the system clipboard. Returns "" when the
// clipboard is empty or holds non-text content (matching v2 behavior).
func (a *App) clipboardGetText() (string, error) {
	if a.wailsApp == nil {
		return "", fmt.Errorf("application not ready")
	}
	text, ok := a.wailsApp.Clipboard.Text()
	if !ok {
		return "", nil
	}
	return text, nil
}

// clipboardSetText writes text to the system clipboard.
func (a *App) clipboardSetText(text string) {
	if a.wailsApp == nil {
		return
	}
	a.wailsApp.Clipboard.SetText(text)
}

// browserOpenURL opens a URL in the system default browser.
func (a *App) browserOpenURL(url string) {
	if a.wailsApp == nil {
		return
	}
	a.wailsApp.Browser.OpenURL(url)
}
