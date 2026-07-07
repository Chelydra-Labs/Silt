package vault

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"gopkg.in/yaml.v3"

	"silt/backend/config"
)

// writeYAML writes a config.yaml at <vault>/.system/config.yaml so the
// migrator's LoadLegacyTaskPluginSettings has something to read.
func writeYAML(t *testing.T, vaultPath, content string) {
	t.Helper()
	dir := filepath.Join(vaultPath, ".system")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// tasksEntry fails the test loudly if the migrator returned nil; otherwise
// it returns the migrated map as-is. The migrator's return value IS the
// silt-tasks entry (the caller assigns it to PluginSettings["silt-tasks"]).
func tasksEntry(t *testing.T, migrated map[string]any) map[string]any {
	t.Helper()
	if migrated == nil {
		t.Fatalf("expected non-nil migrated map")
	}
	return migrated
}

func TestMigrate_NilWhenSiltTasksAlreadyPresent(t *testing.T) {
	// Idempotency gate: once silt-tasks is in YAML, the migrator is a no-op
	// (returns nil) even if legacy keys are still present.
	raw := map[string]any{
		"silt-tasks":  map[string]any{"default_display_mode": "list"},
		"silt-kanban": map[string]any{"columns": []any{"A", "B"}},
	}
	if got := MigrateLegacyTaskPluginSettings(config.Defaults(), raw); got != nil {
		t.Errorf("expected nil when silt-tasks already present, got %v", got)
	}
}

func TestMigrate_NilWhenNoLegacyKeys(t *testing.T) {
	// Nothing to migrate: no legacy keys, no silt-tasks.
	raw := map[string]any{
		"unrelated-plugin": map[string]any{"x": 1},
	}
	if got := MigrateLegacyTaskPluginSettings(config.Defaults(), raw); got != nil {
		t.Errorf("expected nil when no legacy keys present, got %v", got)
	}
}

func TestMigrate_KanbanBoardsBecomeSavedViews(t *testing.T) {
	// boards[] migrate to saved_views[] with the hardcoded hub shape:
	// displayMode=board, groupBy=status, sort=manual. Original board UUIDs
	// must be preserved verbatim (coerceSavedView rejects `sys-` ids).
	raw := map[string]any{
		"silt-kanban": map[string]any{
			"columns": []any{"Backlog", "Active", "Done"},
			"boards": []any{
				map[string]any{
					"id":    "11111111-1111-1111-1111-111111111111",
					"name":  "Project A",
					"scope": "notebook",
				},
				map[string]any{
					"id":   "22222222-2222-2222-2222-222222222222",
					"name": "Project B",
				},
			},
		},
	}
	migrated := MigrateLegacyTaskPluginSettings(config.Defaults(), raw)
	tasks := tasksEntry(t, migrated)

	views, ok := tasks["saved_views"].([]any)
	if !ok {
		t.Fatalf("expected saved_views []any, got %T", tasks["saved_views"])
	}
	if len(views) != 2 {
		t.Fatalf("expected 2 saved views, got %d", len(views))
	}
	first := views[0].(map[string]any)
	if first["id"] != "11111111-1111-1111-1111-111111111111" {
		t.Errorf("board UUID not preserved: got %v", first["id"])
	}
	if first["displayMode"] != "board" {
		t.Errorf("expected displayMode=board, got %v", first["displayMode"])
	}
	if first["groupBy"] != "status" {
		t.Errorf("expected groupBy=status, got %v", first["groupBy"])
	}
	if first["sort"] != "manual" {
		t.Errorf("expected sort=manual, got %v", first["sort"])
	}
	if first["name"] != "Project A" {
		t.Errorf("expected name=Project A, got %v", first["name"])
	}
	if first["scope"] != "notebook" {
		t.Errorf("expected scope=notebook preserved, got %v", first["scope"])
	}
	// Each board inherits the migrated top-level columns.
	cols, ok := first["columns"].([]any)
	if !ok || len(cols) != 3 || cols[0] != "Backlog" {
		t.Errorf("expected columns inherited from kanban top-level, got %v", first["columns"])
	}
}

func TestMigrate_KanbanTopLevelFieldsCarried(t *testing.T) {
	// columns + filters + scope at the kanban top-level migrate to the
	// matching silt-tasks keys.
	raw := map[string]any{
		"silt-kanban": map[string]any{
			"columns": []any{"TODO", "DOING"},
			"filters": map[string]any{
				"owners":     []any{"alice"},
				"priorities": []any{1, 2},
				"dueDate":    "today",
				"tags":       []any{"urgent"},
			},
			"scope": "section",
		},
	}
	tasks := tasksEntry(t, MigrateLegacyTaskPluginSettings(config.Defaults(), raw))

	cols, ok := tasks["columns"].([]any)
	if !ok || len(cols) != 2 || cols[0] != "TODO" {
		t.Errorf("columns not migrated: got %v", tasks["columns"])
	}
	if tasks["default_scope"] != "section" {
		t.Errorf("default_scope not migrated: got %v", tasks["default_scope"])
	}
	filters, ok := tasks["filters"].(map[string]any)
	if !ok {
		t.Fatalf("filters not migrated: got %T", tasks["filters"])
	}
	if owners, _ := filters["owners"].([]any); len(owners) != 1 || owners[0] != "alice" {
		t.Errorf("filters.owners mismatch: got %v", filters["owners"])
	}
	if pri, _ := filters["priorities"].([]any); len(pri) != 2 {
		t.Errorf("filters.priorities mismatch: got %v", filters["priorities"])
	}
	if filters["dueDate"] != "today" {
		t.Errorf("filters.dueDate mismatch: got %v", filters["dueDate"])
	}
}

func TestMigrate_CalendarMonthView(t *testing.T) {
	raw := map[string]any{
		"silt-calendar": map[string]any{"view_mode": "month"},
	}
	tasks := tasksEntry(t, MigrateLegacyTaskPluginSettings(config.Defaults(), raw))
	if tasks["default_display_mode"] != "calendar" {
		t.Errorf("month → expected default_display_mode=calendar, got %v", tasks["default_display_mode"])
	}
	if tasks["calendar_sub_mode"] != "month" {
		t.Errorf("month → expected calendar_sub_mode=month, got %v", tasks["calendar_sub_mode"])
	}
}

func TestMigrate_CalendarWeekView(t *testing.T) {
	raw := map[string]any{
		"silt-calendar": map[string]any{"view_mode": "week"},
	}
	tasks := tasksEntry(t, MigrateLegacyTaskPluginSettings(config.Defaults(), raw))
	if tasks["default_display_mode"] != "calendar" {
		t.Errorf("week → expected default_display_mode=calendar, got %v", tasks["default_display_mode"])
	}
	if tasks["calendar_sub_mode"] != "week" {
		t.Errorf("week → expected calendar_sub_mode=week, got %v", tasks["calendar_sub_mode"])
	}
}

func TestMigrate_CalendarAgendaBecomesList(t *testing.T) {
	raw := map[string]any{
		"silt-calendar": map[string]any{"view_mode": "agenda"},
	}
	tasks := tasksEntry(t, MigrateLegacyTaskPluginSettings(config.Defaults(), raw))
	if tasks["default_display_mode"] != "list" {
		t.Errorf("agenda → expected default_display_mode=list, got %v", tasks["default_display_mode"])
	}
	// Agenda maps to list mode — the Defaults calendar_sub_mode (month) is
	// preserved because list mode has no calendar granularity to override.
	if tasks["calendar_sub_mode"] != "month" {
		t.Errorf("agenda → expected calendar_sub_mode to retain default month, got %v", tasks["calendar_sub_mode"])
	}
}

func TestMigrate_BothPluginsTogether(t *testing.T) {
	// When both legacy plugins are present, both contribute to the unified
	// map. Kanban supplies columns/saved_views; calendar supplies the
	// display-mode override.
	raw := map[string]any{
		"silt-kanban": map[string]any{
			"columns": []any{"A", "B"},
			"boards": []any{
				map[string]any{"id": "abc", "name": "Board 1"},
			},
		},
		"silt-calendar": map[string]any{"view_mode": "week"},
	}
	tasks := tasksEntry(t, MigrateLegacyTaskPluginSettings(config.Defaults(), raw))

	if tasks["default_display_mode"] != "calendar" {
		t.Errorf("calendar override lost: got %v", tasks["default_display_mode"])
	}
	if tasks["calendar_sub_mode"] != "week" {
		t.Errorf("calendar sub_mode lost: got %v", tasks["calendar_sub_mode"])
	}
	cols, _ := tasks["columns"].([]any)
	if len(cols) != 2 || cols[0] != "A" {
		t.Errorf("kanban columns lost: got %v", tasks["columns"])
	}
	views, _ := tasks["saved_views"].([]any)
	if len(views) != 1 {
		t.Errorf("kanban boards not migrated: got %d views", len(views))
	}
}

func TestMigrate_PreservesDefaultsNotSuppliedByLegacy(t *testing.T) {
	// A kanban-only migration must still surface every key the frontend
	// loaders read (e.g. default_sort survives because Defaults supplied it
	// and kanban has no analogue). This is why the migrator seeds from
	// Defaults rather than starting from an empty map.
	raw := map[string]any{
		"silt-kanban": map[string]any{"columns": []any{"A"}},
	}
	tasks := tasksEntry(t, MigrateLegacyTaskPluginSettings(config.Defaults(), raw))

	for _, key := range []string{
		"default_display_mode", "default_group_by", "default_sort",
		"default_scope", "calendar_sub_mode", "columns", "filters",
		"saved_views", "local_author",
	} {
		if _, has := tasks[key]; !has {
			t.Errorf("kanban-only migration lost Defaults key %q", key)
		}
	}
	if tasks["default_sort"] != "dueDate" {
		t.Errorf("default_sort default not preserved: got %v", tasks["default_sort"])
	}
}

func TestMigrate_BoardUUIDsNeverUseSysPrefix(t *testing.T) {
	// coerceSavedView (frontend) rejects ids starting with "sys-". The
	// migrator must preserve the user's board UUIDs verbatim — never
	// synthesize sys- ids.
	raw := map[string]any{
		"silt-kanban": map[string]any{
			"boards": []any{
				map[string]any{"id": "user-board-1", "name": "One"},
				map[string]any{"id": "user-board-2", "name": "Two"},
			},
		},
	}
	tasks := tasksEntry(t, MigrateLegacyTaskPluginSettings(config.Defaults(), raw))
	views, _ := tasks["saved_views"].([]any)
	for i, v := range views {
		id, _ := v.(map[string]any)["id"].(string)
		if len(id) >= 4 && id[:4] == "sys-" {
			t.Errorf("view %d has forbidden sys- prefix: %q", i, id)
		}
	}
}

func TestLoadLegacyTaskPluginSettings_MissingFile(t *testing.T) {
	// A vault with no config.yaml (e.g. brand-new, pre-ScaffoldVault) yields
	// an empty map — the migrator's gate then evaluates to "nothing to
	// migrate" and the migration is a clean no-op.
	tmp := t.TempDir()
	got := LoadLegacyTaskPluginSettings(tmp)
	if len(got) != 0 {
		t.Errorf("expected empty map for missing file, got %v", got)
	}
}

func TestLoadLegacyTaskPluginSettings_MalformedYAML(t *testing.T) {
	// A corrupt config.yaml must not break the migration path — the loader
	// returns an empty map and the regular Load path surfaces the parse
	// error separately. Mirror of LoadLegacyVaultGrants tolerance.
	tmp := t.TempDir()
	writeYAML(t, tmp, "this: is: not: valid: yaml: [")
	got := LoadLegacyTaskPluginSettings(tmp)
	if len(got) != 0 {
		t.Errorf("expected empty map for malformed YAML, got %v", got)
	}
}

func TestLoadLegacyTaskPluginSettings_ParsesValidYAML(t *testing.T) {
	// A well-formed config.yaml surfaces the plugins.plugin_settings
	// subtree as a raw map (the migrator's gate then keys off
	// "silt-kanban"/"silt-tasks"/etc. presence).
	tmp := t.TempDir()
	writeYAML(t, tmp, ""+
		"editor:\n"+
		"  font_family: Test\n"+
		"plugins:\n"+
		"  active: [silt-kanban]\n"+
		"  plugin_settings:\n"+
		"    silt-kanban:\n"+
		"      columns: [A, B]\n"+
		"      boards:\n"+
		"        - id: u-1\n"+
		"          name: Mine\n")
	got := LoadLegacyTaskPluginSettings(tmp)
	if _, has := got["silt-kanban"]; !has {
		t.Errorf("expected silt-kanban entry in raw map, got %v", got)
	}
	// Also confirm the partial decode rejects nothing else (editor.font_family
	// is in the same file but outside the subtree — it shouldn't appear).
	if _, leak := got["font_family"]; leak {
		t.Errorf("partial decode leaked non-plugin key: %v", got)
	}
}

// TestLoadLegacyTaskPluginSettings_RoundTripsThroughYAML adds a belt-and-
// braces check that the partial-decode struct shape matches what yaml.v3
// emits when ScaffoldVault writes a fresh config — guards against a future
// field-tag typo breaking the gate silently.
func TestLoadLegacyTaskPluginSettings_RoundTripsThroughYAML(t *testing.T) {
	in := map[string]any{
		"plugins": map[string]any{
			"plugin_settings": map[string]any{
				"silt-tasks":  map[string]any{"default_scope": "vault"},
				"silt-kanban": map[string]any{"default_col": "TODO"},
			},
		},
	}
	bytes, err := yaml.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	tmp := t.TempDir()
	writeYAML(t, tmp, string(bytes))

	got := LoadLegacyTaskPluginSettings(tmp)
	want := in["plugins"].(map[string]any)["plugin_settings"].(map[string]any)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("partial decode mismatch:\n got  %#v\n want %#v", got, want)
	}
}

// TestMigrate_BoardWithOwnFiltersOverridesTopLevel verifies a board
// carrying its own filters/scope produces a saved view that uses those
// per-board values, not the kanban top-level filters/default_scope.
func TestMigrate_BoardWithOwnFiltersOverridesTopLevel(t *testing.T) {
	raw := map[string]any{
		"silt-kanban": map[string]any{
			"columns": []any{"TODO", "DOING", "DONE"},
			"filters": map[string]any{
				"owners":     []any{"top-owner"},
				"priorities": []any{3},
				"dueDate":    "week",
				"tags":       []any{"top-tag"},
			},
			"scope": "vault",
			"boards": []any{
				map[string]any{
					"id":    "b-with-filters",
					"name":  "Filtered Board",
					"scope": "page",
					"filters": map[string]any{
						"owners":     []any{"board-owner"},
						"priorities": []any{1},
						"dueDate":    "today",
						"tags":       []any{"board-tag"},
					},
				},
			},
		},
	}
	migrated := MigrateLegacyTaskPluginSettings(config.Defaults(), raw)
	tasks := tasksEntry(t, migrated)

	views, ok := tasks["saved_views"].([]any)
	if !ok || len(views) != 1 {
		t.Fatalf("expected 1 saved view, got %v", tasks["saved_views"])
	}
	view := views[0].(map[string]any)

	// Per-board scope wins over top-level.
	if view["scope"] != "page" {
		t.Errorf("expected scope=page (per-board), got %v", view["scope"])
	}

	// Per-board filters win over top-level.
	filters, ok := view["filters"].(map[string]any)
	if !ok {
		t.Fatalf("expected filters map on view, got %T", view["filters"])
	}
	owners, _ := filters["owners"].([]any)
	if len(owners) != 1 || owners[0] != "board-owner" {
		t.Errorf("expected board-owner, got %v", owners)
	}
	if filters["dueDate"] != "today" {
		t.Errorf("expected dueDate=today (per-board), got %v", filters["dueDate"])
	}

	// Top-level filters do NOT pollute the view's filters.
	topOwners, _ := filters["owners"].([]any)
	for _, o := range topOwners {
		if o == "top-owner" {
			t.Error("top-level owner leaked into per-board view")
		}
	}

	// Top-level filters land on the tasks map's default filters (not the view).
	topFilters, ok := tasks["filters"].(map[string]any)
	if !ok {
		t.Fatalf("expected top-level filters on tasks map, got %T", tasks["filters"])
	}
	topFilterOwners, _ := topFilters["owners"].([]any)
	if len(topFilterOwners) != 1 || topFilterOwners[0] != "top-owner" {
		t.Errorf("expected top-owner in tasks filters, got %v", topFilterOwners)
	}

	// Top-level scope lands on tasks["default_scope"] (not the view).
	if tasks["default_scope"] != "vault" {
		t.Errorf("expected default_scope=vault (top-level), got %v", tasks["default_scope"])
	}
}
