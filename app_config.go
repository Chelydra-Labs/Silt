package main

import (
	"fmt"

	"silt/backend/config"
)

// System-config IPC surface: read/clone/validate/persist of config.yaml plus
// the atomic single-field setters (plugin settings, dismissed tips) that
// replace the frontend read-mutate-saveConfig race. All persistence routes
// through saveConfigTracked so the hot-reload watcher suppresses our own
// atomic write. Linked-notebook security reconciliation
// (reconcileLinkedNotebookSecurityLocked / seedFirstPartyGrants) lives in
// app_linked_notebooks.go alongside the rest of the linked-notebook subsystem.

// GetSystemConfig returns an independent snapshot of the parsed system config.
// IPC callers must not be able to mutate maps and slices in the live config
// after the read lock is released.
func (a *App) GetSystemConfig() (config.SystemConfig, error) {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.vaultPath == "" {
		return config.Defaults(), fmt.Errorf("vault not loaded")
	}
	a.configMu.RLock()
	defer a.configMu.RUnlock()
	return config.Clone(a.cfg), nil
}

// GetConfigLoadError returns the error from the initial config.yaml load (if
// any) and clears it. The startup load runs before the frontend subscribes to
// config:error, so that event can be missed; this binding lets the frontend
// retrieve the one-shot error on its first loadConfig() so a broken config is
// surfaced rather than silently masked by Defaults(). Returns "" when there
// was no error (or it was already retrieved).
func (a *App) GetConfigLoadError() string {
	a.configMu.Lock()
	defer a.configMu.Unlock()
	if a.configLoadErr == nil {
		return ""
	}
	msg := a.configLoadErr.Error()
	a.configLoadErr = nil
	return msg
}

// saveConfigTracked persists cfg atomically while managing the config watcher's
// self-write suppression window. The window is armed BEFORE the write so Silt's
// own atomic save (temp + rename → a burst of fsnotify events) is suppressed,
// then cleared via UnregisterSelfWrite if the write fails — otherwise a failed
// save would leave the 500ms window open and silently drop a legitimate
// external edit landing inside it. Safe to call while holding configMu:
// config.Save and the watcher's selfMu are independent of configMu. This is the
// single source of truth for the register/unregister discipline; every atomic
// config setter routes its persist through here.
func (a *App) saveConfigTracked(cfg config.SystemConfig) error {
	if a.configWatcher != nil {
		a.configWatcher.RegisterSelfWrite()
	}
	if err := config.Save(a.vaultPath, cfg); err != nil {
		if a.configWatcher != nil {
			a.configWatcher.UnregisterSelfWrite()
		}
		return err
	}
	return nil
}

// SaveSystemConfig validates, persists atomically, and applies the new config.
// The self-write is registered first so the hot-reload watcher ignores the
// fsnotify event from our own atomic write.
func (a *App) SaveSystemConfig(cfg config.SystemConfig) error {
	if cfg.Editor.TabIndentSpaces <= 0 {
		return fmt.Errorf("invalid config: editor.tab_indent_spaces must be positive")
	}
	if cfg.Editor.FontSizePx <= 0 {
		return fmt.Errorf("invalid config: editor.font_size_px must be positive")
	}
	if cfg.Editor.LineHeight <= 0 {
		return fmt.Errorf("invalid config: editor.line_height must be positive")
	}
	if cfg.Editor.AutoSaveDelayMs < 0 {
		return fmt.Errorf("invalid config: editor.auto_save_delay_ms must be non-negative")
	}
	if cfg.Editor.MaxVersionsPerPage < 1 || cfg.Editor.MaxVersionsPerPage > 500 {
		return fmt.Errorf("invalid config: editor.max_versions_per_page must be between 1 and 500")
	}
	if cfg.Editor.AutoVersioningMinIntervalSec < 0 || cfg.Editor.AutoVersioningMinIntervalSec > 3600 {
		return fmt.Errorf("invalid config: editor.auto_versioning_min_interval_sec must be between 0 and 3600")
	}
	if err := config.ValidateHotkeys(cfg.Hotkeys); err != nil {
		return err
	}
	// Clone the frontend snapshot before the serialized mutation. Besides
	// preventing stale navigation state from being adopted, this keeps mutable
	// maps and slices in the caller's value out of the live config on failure.
	incoming := config.Clone(cfg)
	var quarantined []map[string]string
	err := a.mutateConfig(func(current *config.SystemConfig) error {
		// SaveSystemConfig remains for unrelated settings, but navigation state
		// is backend-owned and must never be replaced by a stale whole snapshot.
		// The linked-notebook registry is likewise backend-owned: a snapshot
		// taken before a link was added must not remove that link.
		incoming.UI.NavOrder = config.CloneNavOrder(current.UI.NavOrder)
		incoming.UI.OpenTabs = append([]config.TabRef(nil), current.UI.OpenTabs...)
		if current.UI.ActiveTab == nil {
			incoming.UI.ActiveTab = nil
		} else {
			active := *current.UI.ActiveTab
			incoming.UI.ActiveTab = &active
		}
		incoming.UI.ExpandedSections = append([]config.NavigationSectionRef(nil), current.UI.ExpandedSections...)
		incoming.UI.RecentPages = append([]config.RecentPage(nil), current.UI.RecentPages...)
		incoming.UI.Favorites = append([]config.NavigationPageRef(nil), current.UI.Favorites...)
		incoming.LinkedNotebooks = append([]config.LinkedNotebook(nil), current.LinkedNotebooks...)
		incoming.AI.Chat.APIKey = current.AI.Chat.APIKey
		incoming.AI.Embedding.APIKey = current.AI.Embedding.APIKey
		// Treat this like a config reload for linked-notebook security: frontend
		// snapshots are not trusted sources for roots or their fingerprints.
		quarantined = a.reconcileLinkedNotebookSecurityLocked(&incoming)
		*current = incoming
		a.spacesPerTab = incoming.Editor.TabIndentSpaces
		return nil
	})
	if err != nil {
		return err
	}
	a.configMu.Lock()
	a.seedFirstPartyGrants()
	a.configMu.Unlock()
	for _, q := range quarantined {
		a.emit(EventLinkedNotebookQuarantined, q)
	}
	return nil
}

// applyConfig stores the parsed config under configMu, applies the live
// Go-side knobs (tab indent width), then emits config:changed so the frontend
// refreshes editor settings, hotkeys, and per-plugin settings.
func (a *App) applyConfig(cfg config.SystemConfig) {
	quarantined := a.applyConfigLocked(cfg)
	a.emit(EventConfigChanged, cfg)
	// F3: emit linked-notebook:quarantined for any links whose root_path
	// changed in the external edit (synced-vault attack vector).
	for _, q := range quarantined {
		a.emit(EventLinkedNotebookQuarantined, q)
	}
}

// applyConfigLocked updates a.cfg + live knobs under the write lock. Split out
// so initializeVaultServices can set the config (and spacesPerTab) before the
// first scan without emitting an event for a vault the frontend hasn't seen yet.
// Returns a slice of newly-quarantined linked-notebook event payloads (for
// applyConfig to emit after unlock — the lock is held here so quarantineLink
// cannot be called).
func (a *App) applyConfigLocked(cfg config.SystemConfig) []map[string]string {
	a.configMu.Lock()
	defer a.configMu.Unlock()
	quarantined := a.reconcileLinkedNotebookSecurityLocked(&cfg)
	a.cfg = cfg
	if cfg.Editor.TabIndentSpaces > 0 {
		a.spacesPerTab = cfg.Editor.TabIndentSpaces
	}
	a.seedFirstPartyGrants()
	return quarantined
}

// UpdatePluginSetting atomically updates a single per-plugin setting key and
// persists it — the targeted read-modify-write that replaces the frontend
// read-mutate-saveConfig dance, which could race an external config.yaml edit
// landing between the read and the Go-side write (#120). Only
// plugins.plugin_settings[pluginID][key] is touched, so an unrelated external
// edit is merged rather than clobbered; mutateConfigFresh documents the TOCTOU
// policy. Like SaveSystemConfig it does NOT emit config:changed — the frontend
// store updates optimistically and external edits flow through the watcher.
func (a *App) UpdatePluginSetting(pluginID string, key string, value any) error {
	if pluginID == "" || key == "" {
		return fmt.Errorf("pluginID and key are required")
	}
	return a.mutateConfigFresh(func(cfg *config.SystemConfig) (bool, error) {
		if cfg.Plugins.PluginSettings == nil {
			cfg.Plugins.PluginSettings = map[string]any{}
		}
		entry, _ := cfg.Plugins.PluginSettings[pluginID].(map[string]any)
		if entry == nil {
			entry = map[string]any{}
		}
		entry[key] = value
		cfg.Plugins.PluginSettings[pluginID] = entry
		return true, nil
	})
}

// AppendDismissedTip records a one-time UI tip ID as dismissed (#197). Routes
// through mutateConfigFresh so an external edit to an unrelated field survives.
// Idempotent — calling twice with the same tipID produces a single-entry slice
// and skips the write. Like the other internal atomic setters, it does NOT emit
// config:changed; the frontend settings store mirrors the change optimistically
// and external edits still flow through watcher → applyConfig.
func (a *App) AppendDismissedTip(tipID string) error {
	if tipID == "" {
		return fmt.Errorf("tipID is required")
	}
	return a.mutateConfigFresh(func(cfg *config.SystemConfig) (bool, error) {
		for _, existing := range cfg.UI.DismissedTips {
			if existing == tipID {
				return false, nil
			}
		}
		if cfg.UI.DismissedTips == nil {
			cfg.UI.DismissedTips = []string{}
		}
		cfg.UI.DismissedTips = append(cfg.UI.DismissedTips, tipID)
		return true, nil
	})
}

// SetTypedNotesSavedViews atomically replaces the typed-notes dashboard's
// saved-view list at ui.dashboards.typed_notes.saved_views. Routes through
// mutateConfigFresh so a concurrent external edit (Obsidian/Dropbox/second Silt
// window/hand-edit) landing after the last config.Load is merged rather than
// overwritten. Only the nested saved_views slice is touched. The dashboard owns
// the nested shape, so Go carries the list as an opaque any slice (the frontend
// coerces on load via the Dashboards map[string]any blob). Like the other
// internal atomic setters it does NOT emit config:changed.
func (a *App) SetTypedNotesSavedViews(views []any) error {
	return a.mutateConfigFresh(func(cfg *config.SystemConfig) (bool, error) {
		if cfg.UI.Dashboards == nil {
			cfg.UI.Dashboards = map[string]any{}
		}
		typedNotes, _ := cfg.UI.Dashboards["typed_notes"].(map[string]any)
		if typedNotes == nil {
			typedNotes = map[string]any{}
		}
		typedNotes["saved_views"] = views
		cfg.UI.Dashboards["typed_notes"] = typedNotes
		return true, nil
	})
}
