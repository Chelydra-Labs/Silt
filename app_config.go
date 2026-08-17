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
// read-mutate-saveConfig dance which could race an external config.yaml edit
// (e.g. an external editor) landing between the read and the Go-side atomic write (#120).
// Only plugins.plugin_settings[pluginID][key] is touched; every other config
// field is preserved verbatim, so a concurrent external edit to an unrelated
// section is not clobbered.
//
// TOCTOU hardening (#475): config.yaml is RE-READ from disk inside configMu.Lock
// immediately before the mutation, so an external edit that landed after the
// last config.Load (vault init / watcher applyConfig) is merged into the save
// rather than overwritten. The mutation is applied to the fresh read, a.cfg is
// refreshed, then config.Save serializes the result. The external-edit loss
// window shrinks to the lock-hold duration (no cross-IPC gap). If the re-read
// fails (corrupt file), the call refuses loudly with an error — safer than
// silently overwriting the corrupt file with the stale in-memory cfg, which
// would destroy the user's only signal that their config is broken.
//
// Atomicity: configMu is held across the re-read, in-memory mutation, AND the
// disk save, so concurrent internal callers (and the watcher's applyConfig)
// cannot interleave a snapshot-and-save and lose an update. The external-edit
// race is handled by RegisterSelfWrite (suppresses the watcher's reaction to
// our own write) + this lock. Like SaveSystemConfig it does NOT emit
// config:changed (the frontend store updates optimistically; external edits
// still flow through the watcher -> applyConfig with emit).
func (a *App) UpdatePluginSetting(pluginID string, key string, value any) error {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.vaultPath == "" {
		return fmt.Errorf("vault not loaded")
	}
	if pluginID == "" || key == "" {
		return fmt.Errorf("pluginID and key are required")
	}
	a.configMu.Lock()
	defer a.configMu.Unlock()
	// Re-read config.yaml under the lock so an external edit to an unrelated
	// key is preserved rather than overwritten by the stale in-memory cfg.
	// A re-read failure (corrupt/unreadable file) is returned as an error —
	// refusing loudly is safer than silently overwriting the corrupt file
	// with the stale in-memory cfg, which would destroy the user's only
	// signal that their config is broken.
	freshCfg, loadErr := config.Load(a.vaultPath)
	if loadErr != nil {
		return fmt.Errorf("cannot read config.yaml before update: %w", loadErr)
	}
	if freshCfg.Plugins.PluginSettings == nil {
		freshCfg.Plugins.PluginSettings = map[string]any{}
	}
	entry, _ := freshCfg.Plugins.PluginSettings[pluginID].(map[string]any)
	if entry == nil {
		entry = map[string]any{}
	}
	entry[key] = value
	freshCfg.Plugins.PluginSettings[pluginID] = entry
	a.cfg = freshCfg
	return a.saveConfigTracked(a.cfg)
}

// AppendDismissedTip records a one-time UI tip ID as dismissed (#197). Mirrors
// the atomic pattern of UpdatePluginSetting: vaultMu.RLock + configMu.Lock held
// across the in-memory mutation and config.Save, with RegisterSelfWrite
// suppressing the watcher's reaction to our own write. Idempotent — calling
// twice with the same tipID produces a single-entry slice. Like the other
// internal atomic setters, it does NOT emit config:changed; the frontend
// settings store mirrors the change optimistically and external edits still
// flow through watcher → applyConfig.
func (a *App) AppendDismissedTip(tipID string) error {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.vaultPath == "" {
		return fmt.Errorf("vault not loaded")
	}
	if tipID == "" {
		return fmt.Errorf("tipID is required")
	}
	a.configMu.Lock()
	defer a.configMu.Unlock()
	if a.cfg.UI.DismissedTips == nil {
		a.cfg.UI.DismissedTips = []string{}
	}
	for _, existing := range a.cfg.UI.DismissedTips {
		if existing == tipID {
			return nil
		}
	}
	a.cfg.UI.DismissedTips = append(a.cfg.UI.DismissedTips, tipID)
	return a.saveConfigTracked(a.cfg)
}

// SetTypedNotesSavedViews atomically replaces the typed-notes dashboard's
// saved-view list at ui.dashboards.typed_notes.saved_views. Mirrors the
// TOCTOU-hardened contract of UpdatePluginSetting (#120/#475): config.yaml is
// re-read under configMu.Lock immediately before the mutation, so a concurrent
// external edit (Obsidian/Dropbox/second Silt window/hand-edit) that landed
// after the last config.Load is merged into the save rather than overwritten.
// Only the nested saved_views slice is touched; every other config field is
// preserved verbatim. The dashboard owns the nested shape, so Go carries the
// list as an opaque any slice (the frontend coerces on load via the
// Dashboards map[string]any blob). Like the other internal atomic setters it
// does NOT emit config:changed — the frontend settings store mirrors the
// change optimistically and external edits still flow through watcher →
// applyConfig.
func (a *App) SetTypedNotesSavedViews(views []any) error {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.vaultPath == "" {
		return fmt.Errorf("vault not loaded")
	}
	a.configMu.Lock()
	defer a.configMu.Unlock()
	// Re-read config.yaml under the lock so an external edit to an unrelated
	// section is preserved rather than overwritten by the stale in-memory cfg
	// (same rationale as UpdatePluginSetting). A re-read failure refuses
	// loudly rather than destroying the user's signal that config is broken.
	freshCfg, loadErr := config.Load(a.vaultPath)
	if loadErr != nil {
		return fmt.Errorf("cannot read config.yaml before update: %w", loadErr)
	}
	if freshCfg.UI.Dashboards == nil {
		freshCfg.UI.Dashboards = map[string]any{}
	}
	typedNotes, _ := freshCfg.UI.Dashboards["typed_notes"].(map[string]any)
	if typedNotes == nil {
		typedNotes = map[string]any{}
	}
	typedNotes["saved_views"] = views
	freshCfg.UI.Dashboards["typed_notes"] = typedNotes
	a.cfg = freshCfg
	return a.saveConfigTracked(a.cfg)
}
