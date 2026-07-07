package vault

import (
	"path/filepath"

	"gopkg.in/yaml.v3"

	"silt/backend/config"
	"silt/backend/safeio"
)

// task_plugin_migrate.go implements the Phase 9 (#431) one-time migration of
// the legacy silt-calendar + silt-kanban plugin settings into the unified
// silt-tasks hub. The frontend's per-plugin id collapse (Phase 10) needs
// every vault's task config to live under a single key, but users on the
// pre-unification release wrote silt-kanban (boards/columns/filters/scope)
// and silt-calendar (view_mode) — those YAML keys must be carried forward.
//
// The migration is GATED + IDEMPOTENT: it runs only when the on-disk YAML
// lacks a silt-tasks entry (the gate) and writes that entry on first run, so
// every subsequent launch is a no-op. Old keys are NOT removed here — they
// remain in config.yaml for one release so a downgrade/revert still sees the
// user's prior config. The next release (N+1) drops the old keys entirely.
//
// The on-disk YAML is read via a throwaway partial decode (not config.Load,
// which merges Defaults and would always populate silt-tasks — defeating the
// gate). This mirrors LoadLegacyVaultGrants.

// LegacyTaskPluginSettings is a partial decode of config.yaml's
// plugins.plugin_settings subtree, scoped to ONLY what the migrator needs to
// see: the raw user-authored plugin entries. Used to gate the migration on
// what's actually in YAML, not what Defaults() would inject.
type LegacyTaskPluginSettings struct {
	Plugins struct {
		PluginSettings map[string]any `yaml:"plugin_settings,omitempty"`
	} `yaml:"plugins"`
}

// LoadLegacyTaskPluginSettings reads <vault>/.system/config.yaml and decodes
// ONLY the plugins.plugin_settings subtree. Returns the raw map (which may
// contain silt-calendar, silt-kanban, silt-tasks, and any other plugin's
// settings), or an empty map when the file is missing or unparseable. A
// parse error anywhere outside the subtree is ignored — the migration is
// best-effort, and a broken config.yaml surfaces through the regular Load
// path with a real error.
func LoadLegacyTaskPluginSettings(vaultPath string) map[string]any {
	configPath := filepath.Join(vaultPath, ".system", "config.yaml")
	// Bound the read at 256 KB (mirrors config.Load) so a runaway or hostile
	// config.yaml can't OOM the partial-decode path.
	data, err := safeio.ReadFileMax(configPath, 256*1024)
	if err != nil {
		return map[string]any{}
	}
	var partial LegacyTaskPluginSettings
	if err := yaml.Unmarshal(data, &partial); err != nil {
		return map[string]any{}
	}
	if partial.Plugins.PluginSettings == nil {
		return map[string]any{}
	}
	return partial.Plugins.PluginSettings
}

// MigrateLegacyTaskPluginSettings maps silt-calendar + silt-kanban settings
// into the unified silt-tasks schema. Returns nil in two cases:
//   - silt-tasks already present in rawYAMLSettings (idempotent — the gate
//     check fires on the raw on-disk YAML, not the in-memory cfg, which is
//     always populated by Defaults);
//   - neither silt-calendar nor silt-kanban present (nothing to migrate).
//
// defaultsCfg supplies the seed (Defaults().Plugins.PluginSettings["silt-tasks"])
// so the returned map carries every key the frontend loaders read, even when
// a legacy plugin didn't supply it. It is taken by value because App.cfg is
// itself stored by value (a snapshot is appropriate — Defaults is a pure
// function of the running binary, not of the live cfg).
//
// Otherwise it returns the new silt-tasks entry (a complete map — every key
// the frontend reads is present, seeded from Defaults so missing legacy
// fields don't poke holes in the schema). Old keys are NOT removed; the
// caller writes only the silt-tasks entry and leaves the legacy stanzas in
// place for one release.
//
// Field mapping (issue #431):
//
//	silt-kanban.boards[]    → silt-tasks.saved_views[]   (displayMode=board,
//	                          groupBy=status, sort=manual; per-board scope
//	                          + filters carried over; columns inherited
//	                          from the migrated silt-tasks.columns)
//	silt-kanban.columns     → silt-tasks.columns
//	silt-kanban.filters     → silt-tasks.filters
//	silt-kanban.scope       → silt-tasks.default_scope
//	silt-calendar.view_mode → silt-tasks.default_display_mode +
//	                          silt-tasks.calendar_sub_mode
//	                          ({month,week} → calendar + same sub_mode;
//	                           agenda → list, no sub_mode override)
func MigrateLegacyTaskPluginSettings(
	defaultsCfg config.SystemConfig,
	rawYAMLSettings map[string]any,
) map[string]any {
	if _, hasTasks := rawYAMLSettings["silt-tasks"]; hasTasks {
		return nil
	}
	_, hasKanban := rawYAMLSettings["silt-kanban"]
	_, hasCalendar := rawYAMLSettings["silt-calendar"]
	if !hasKanban && !hasCalendar {
		return nil
	}

	// Seed from Defaults so every key the frontend loaders read is present,
	// even when a legacy plugin didn't supply it.
	tasksMap := map[string]any{}
	if defaultsSeed, ok := defaultsCfg.Plugins.PluginSettings["silt-tasks"].(map[string]any); ok {
		for k, v := range defaultsSeed {
			tasksMap[k] = v
		}
	}

	if kanban, ok := rawYAMLSettings["silt-kanban"].(map[string]any); ok {
		migrateKanbanIntoTasks(kanban, tasksMap)
	}
	if calendar, ok := rawYAMLSettings["silt-calendar"].(map[string]any); ok {
		migrateCalendarIntoTasks(calendar, tasksMap)
	}
	return tasksMap
}

// migrateKanbanIntoTasks maps the four kanban top-level fields + the boards[]
// collection into silt-tasks in place. boards[] become user saved_views[]
// (NOT system views — the frontend's coerceSavedView rejects `sys-` ids, so
// the original board UUIDs are preserved verbatim).
func migrateKanbanIntoTasks(kanban, tasks map[string]any) {
	if cols, ok := kanban["columns"].([]any); ok && len(cols) > 0 {
		colsCopy := make([]any, len(cols))
		copy(colsCopy, cols)
		tasks["columns"] = colsCopy
	}
	if filters, ok := kanban["filters"].(map[string]any); ok {
		tasks["filters"] = normalizeFilters(filters)
	}
	if scope, ok := kanban["scope"].(string); ok && scope != "" {
		tasks["default_scope"] = scope
	}
	if boards, ok := kanban["boards"].([]any); ok && len(boards) > 0 {
		columns, _ := tasks["columns"].([]any)
		views := make([]any, 0, len(boards))
		for _, b := range boards {
			board, ok := b.(map[string]any)
			if !ok {
				continue
			}
			view := map[string]any{
				"id":          board["id"],
				"name":        board["name"],
				"displayMode": "board",
				"groupBy":     "status",
				"sort":        "manual",
			}
			if scope, ok := board["scope"].(string); ok && scope != "" {
				view["scope"] = scope
			}
			if filters, ok := board["filters"].(map[string]any); ok {
				view["filters"] = normalizeFilters(filters)
			}
			if len(columns) > 0 {
				colsCopy := make([]any, len(columns))
				copy(colsCopy, columns)
				view["columns"] = colsCopy
			}
			views = append(views, view)
		}
		tasks["saved_views"] = views
	}
}

// migrateCalendarIntoTasks maps the calendar view_mode into the unified
// default_display_mode + calendar_sub_mode. {month, week} → calendar mode
// (sub_mode preserves the user's choice); agenda → list mode (no sub_mode
// override — list mode has no calendar granularity).
func migrateCalendarIntoTasks(calendar, tasks map[string]any) {
	viewMode, ok := calendar["view_mode"].(string)
	if !ok || viewMode == "" {
		return
	}
	switch viewMode {
	case "month":
		tasks["default_display_mode"] = "calendar"
		tasks["calendar_sub_mode"] = "month"
	case "week":
		tasks["default_display_mode"] = "calendar"
		tasks["calendar_sub_mode"] = "week"
	case "agenda":
		tasks["default_display_mode"] = "list"
	}
}

// normalizeFilters coerces a raw filters map into the canonical silt-tasks
// TaskFilters shape {owners, priorities, dueDate, tags}. Missing fields
// default to empty collections; type-mismatched fields are dropped
// (best-effort — the frontend validator also coerces, but a clean shape here
// avoids a needless round-trip).
func normalizeFilters(raw map[string]any) map[string]any {
	out := map[string]any{
		"owners":     []any{},
		"priorities": []any{},
		"dueDate":    "",
		"tags":       []any{},
	}
	if v, ok := raw["owners"].([]any); ok {
		out["owners"] = v
	}
	if v, ok := raw["priorities"].([]any); ok {
		out["priorities"] = v
	}
	if v, ok := raw["dueDate"].(string); ok {
		out["dueDate"] = v
	}
	if v, ok := raw["tags"].([]any); ok {
		out["tags"] = v
	}
	return out
}
