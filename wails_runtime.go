package main

import (
	"fmt"
	"log"
	"strings"
)

// maxStartupEvents bounds the pre-mount event queue so a frontend that never
// calls MarkFrontendReady (broken build, JS error) can't grow it unbounded.
// 200 is generous — real startup emits ~10 distinct event types.
const maxStartupEvents = 200

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

// emitOrQueue emits a Wails event OR, until MarkFrontendReady signals the
// frontend has mounted its listeners, appends a copy to startupEvents so
// GetStartupEvents can replay it. In Wails v3, ServiceStartup runs before the
// webview exists, so a plain emit there is silently dropped — the queue is the
// retrieval path. After MarkFrontendReady, this collapses to a plain emit (no
// copy, no lock on the hot path). The stored payload is data[0] (or nil when
// data is empty), matching how Wails delivers a single-arg event as ev.data on
// the JS side. Startup emits use exactly one arg, so this is exact.
//
// The queue and live emit are mutually exclusive (OR, not AND): a pre-ready
// event is queued ONLY (replayed once by GetStartupEvents); a post-ready event
// is emitted ONLY. Emitting AND queueing during the IPC round-trip gap between
// listener registration and MarkFrontendReady would double-deliver (live +
// replay), surfacing two modals/toasts for a single event.
func (a *App) emitOrQueue(name string, data ...any) {
	var payload any
	if len(data) > 0 {
		payload = data[0]
	}
	a.startupEventsMu.Lock()
	if !a.frontendReady {
		if len(a.startupEvents) < maxStartupEvents {
			a.startupEvents = append(a.startupEvents, startupEvent{Name: name, Payload: payload})
		} else if !a.startupDropLogged {
			// Frontend never marked ready (broken build / JS error) and the
			// queue is full — log once so the otherwise-silent loss is
			// diagnosable instead of vanishing without a trace.
			a.startupDropLogged = true
			log.Printf("silt: startup event queue capped at %d; dropping further events (first dropped %q) — frontend never called MarkFrontendReady", maxStartupEvents, name)
		}
		a.startupEventsMu.Unlock()
		return // queued — GetStartupEvents replays once the frontend mounts
	}
	a.startupEventsMu.Unlock()
	a.emit(name, data...)
}

// normalizeCancel converts a v3 dialog cancel error into the v2 contract
// ("", nil). v3's cfd library returns ("", ErrorCancelled) on user cancel;
// v2 returned ("", nil). The cancel sentinel lives in a Wails internal
// package, so we match on the error string. This restores the documented
// "Returns \"\" on cancel" contract that every picker callsite relies on.
func normalizeCancel(path string, err error) (string, error) {
	// v3 returns ("", ErrorCancelled) on user cancel, so a genuine cancel
	// always carries an empty path. Require path == "" in addition to the
	// substring match so a real error whose message merely contains "cancel"
	// (e.g. an I/O failure on a path like .../cancelled-meeting.md) is not
	// swallowed as a successful empty cancel.
	if err != nil && path == "" && strings.Contains(strings.ToLower(err.Error()), "cancel") {
		return "", nil
	}
	return path, err
}

// openDirectoryDialog opens a native folder picker. Returns "" on cancel.
func (a *App) openDirectoryDialog(title string) (string, error) {
	if a.wailsApp == nil {
		return "", fmt.Errorf("application not ready")
	}
	path, err := a.wailsApp.Dialog.OpenFile().
		SetTitle(title).
		CanChooseDirectories(true).
		CanChooseFiles(false).
		PromptForSingleSelection()
	return normalizeCancel(path, err)
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
	path, err := d.PromptForSingleSelection()
	return normalizeCancel(path, err)
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
	path, err := d.PromptForSingleSelection()
	return normalizeCancel(path, err)
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
