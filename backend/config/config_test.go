package config

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// writeFile is a tiny helper for tests.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestDefaults_Populated(t *testing.T) {
	d := Defaults()
	// Every section must have sensible non-zero values so a fresh vault never
	// nil-derefs.
	if d.Editor.FontFamily == "" || d.Editor.MonoFontFamily == "" {
		t.Errorf("defaults editor fonts must be set: %+v", d.Editor)
	}
	if d.Editor.FontSizePx <= 0 || d.Editor.TabIndentSpaces <= 0 {
		t.Errorf("defaults editor sizes must be positive: %+v", d.Editor)
	}
	if d.Editor.LineHeight <= 0 || d.Editor.AutoSaveDelayMs <= 0 {
		t.Errorf("defaults editor numeric fields must be positive: %+v", d.Editor)
	}
	if !d.Editor.FocusHighlightAncestors {
		t.Errorf("defaults focus_highlight_ancestors should be true")
	}
	if !d.Parsing.AutoInjectUUID {
		t.Errorf("defaults auto_inject_uuid should be true")
	}
	if d.Parsing.DefaultTaskPriority <= 0 {
		t.Errorf("defaults default_task_priority must be positive")
	}
	if len(d.Hotkeys) == 0 {
		t.Errorf("defaults hotkeys must be populated")
	}
	if _, ok := d.Hotkeys["open_search"]; !ok {
		t.Errorf("defaults hotkeys missing open_search")
	}
	if _, ok := d.Hotkeys["focus_sidebar"]; !ok {
		t.Errorf("defaults hotkeys missing focus_sidebar (#326 item 8)")
	}
	if len(d.Plugins.Active) == 0 {
		t.Errorf("defaults plugins.active must be populated")
	}
}

func TestDefaults_Phase5NavigationHotkeys(t *testing.T) {
	want := map[string]string{
		"new_page":            "Ctrl+N",
		"new_section":         "Ctrl+Alt+N",
		"new_notebook":        "Ctrl+Alt+Shift+N",
		"open_quick_switcher": "Ctrl+P",
		"open_shortcuts_help": "Shift+?",
	}
	defaults := Defaults()
	for key, binding := range want {
		if got := defaults.Hotkeys[key]; got != binding {
			t.Errorf("default hotkey %q = %q, want %q", key, got, binding)
		}
	}
}

func TestLoad_Phase5HotkeyOverridesNormalizeWithoutReset(t *testing.T) {
	vault := t.TempDir()
	writeFile(t, ConfigPath(vault), "hotkeys:\n  new_page: Alt+N\n  open_quick_switcher: ''\n")
	cfg, err := Load(vault)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Hotkeys["new_page"] != "Alt+N" {
		t.Errorf("new_page override = %q, want Alt+N", cfg.Hotkeys["new_page"])
	}
	if cfg.Hotkeys["open_quick_switcher"] != "" {
		t.Errorf("explicitly disabled quick switcher = %q, want empty", cfg.Hotkeys["open_quick_switcher"])
	}
	if cfg.Hotkeys["new_section"] != "Ctrl+Alt+N" || cfg.Hotkeys["new_notebook"] != "Ctrl+Alt+Shift+N" || cfg.Hotkeys["open_shortcuts_help"] != "Shift+?" {
		t.Errorf("absent Phase 5 defaults were not retained: %+v", cfg.Hotkeys)
	}
	normalized := Normalize(cfg)
	if normalized.Hotkeys["new_page"] != "Alt+N" || normalized.Hotkeys["open_quick_switcher"] != "" {
		t.Errorf("Normalize changed Phase 5 overrides: %+v", normalized.Hotkeys)
	}
}

// TestSave_RestrictiveFilePermissions pins the F7 hardening: config.yaml is
// written 0o600 and its .system/ parent 0o700 so a co-tenant on a multi-user
// host cannot read the plugin grant table / linked-notebook paths / settings.
func TestSave_RestrictiveFilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits are not enforced on Windows")
	}
	vault := t.TempDir()
	if err := Save(vault, Defaults()); err != nil {
		t.Fatalf("Save: %v", err)
	}
	cfgInfo, err := os.Stat(ConfigPath(vault))
	if err != nil {
		t.Fatalf("stat config.yaml: %v", err)
	}
	if got := cfgInfo.Mode().Perm(); got != 0o600 {
		t.Errorf("config.yaml perm = %o, want 0o600", got)
	}
	sysInfo, err := os.Stat(filepath.Join(vault, ".system"))
	if err != nil {
		t.Fatalf("stat .system: %v", err)
	}
	if got := sysInfo.Mode().Perm(); got != 0o700 {
		t.Errorf(".system perm = %o, want 0o700", got)
	}
}

// TestLoad_RejectsOversizeConfig pins F12: an oversized config.yaml is rejected
// at read time without unbounded allocation ahead of yaml.Unmarshal.
func TestLoad_RejectsOversizeConfig(t *testing.T) {
	vault := t.TempDir()
	if err := os.MkdirAll(filepath.Join(vault, ".system"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ConfigPath(vault), make([]byte, maxConfigYAMLBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(vault)
	if err == nil {
		t.Fatal("expected oversize config.yaml to be rejected")
	}
	if !strings.Contains(err.Error(), "exceeds the") {
		t.Errorf("error %q must mention the byte cap", err.Error())
	}
}

// TestLoadLinked_RejectsOversizeConfig pins F12 for the co-located linked
// notebook config.yaml override layer.
func TestLoadLinked_RejectsOversizeConfig(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".system"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(LinkedConfigPath(root), make([]byte, maxConfigYAMLBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadLinked(root)
	if err == nil {
		t.Fatal("expected oversize linked config.yaml to be rejected")
	}
	if !strings.Contains(err.Error(), "exceeds the") {
		t.Errorf("error %q must mention the byte cap", err.Error())
	}
}

// TestLoad_LegacyShorthandRegexIgnored pins F11: the user-editable
// shorthand_regex is removed (it was dead config — the parser uses fixed
// package-level regexes, never this field). A synced vault carrying a
// catastrophic-backtracking regex such as ^(a+)+$ must load cleanly with the
// value silently dropped (yaml.v3 ignores unknown keys), so it can never
// reach the indexer.
func TestLoad_LegacyShorthandRegexIgnored(t *testing.T) {
	vault := t.TempDir()
	hostile := "parsing:\n  auto_inject_uuid: true\n  default_task_priority: 3\n  shorthand_regex: \"^(a+)+$\"\n"
	writeFile(t, ConfigPath(vault), hostile)
	cfg, err := Load(vault)
	if err != nil {
		t.Fatalf("config with legacy shorthand_regex should load: %v", err)
	}
	if want := Defaults().Parsing; cfg.Parsing != want {
		t.Errorf("legacy shorthand_regex should be dropped; Parsing = %+v, want %+v", cfg.Parsing, want)
	}
}

func TestLoad_MissingFile_ReturnsDefaults(t *testing.T) {
	tmp := t.TempDir() // no config.yaml present
	cfg, err := Load(tmp)
	if err != nil {
		t.Fatalf("missing config should not error, got %v", err)
	}
	d := Defaults()
	if cfg.Editor.FontSizePx != d.Editor.FontSizePx {
		t.Errorf("missing file should yield default font size, got %d", cfg.Editor.FontSizePx)
	}
	if cfg.Editor.FontFamily != d.Editor.FontFamily {
		t.Errorf("missing file should yield default font family, got %q", cfg.Editor.FontFamily)
	}
}

func TestLoad_HappyPath_OverridesDefaults(t *testing.T) {
	tmp := t.TempDir()
	writeFile(t, ConfigPath(tmp), strings.Join([]string{
		"editor:",
		"  font_family: Inter",
		"  tab_indent_spaces: 2",
		"  auto_save_delay_ms: 750",
		"hotkeys:",
		"  open_search: Ctrl+K",
		"plugins:",
		"  active:",
		"    - my-plugin",
		"  disabled: []",
	}, "\n"))
	cfg, err := Load(tmp)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Editor.FontFamily != "Inter" {
		t.Errorf("font override: want Inter, got %q", cfg.Editor.FontFamily)
	}
	if cfg.Editor.TabIndentSpaces != 2 {
		t.Errorf("tab override: want 2, got %d", cfg.Editor.TabIndentSpaces)
	}
	if cfg.Editor.AutoSaveDelayMs != 750 {
		t.Errorf("autosave override: want 750, got %d", cfg.Editor.AutoSaveDelayMs)
	}
	// Fields NOT in the file must keep their defaults.
	d := Defaults()
	if cfg.Editor.FontSizePx != d.Editor.FontSizePx {
		t.Errorf("absent font_size_px should keep default, got %d", cfg.Editor.FontSizePx)
	}
	if cfg.Parsing.AutoInjectUUID != d.Parsing.AutoInjectUUID {
		t.Errorf("absent parsing.auto_inject_uuid should keep default")
	}
	// Present hotkey overridden, absent ones keep defaults.
	if cfg.Hotkeys["open_search"] != "Ctrl+K" {
		t.Errorf("hotkey override: want Ctrl+K, got %q", cfg.Hotkeys["open_search"])
	}
	if cfg.Hotkeys["indent_block"] != d.Hotkeys["indent_block"] {
		t.Errorf("absent hotkey should keep default")
	}
	if len(cfg.Plugins.Active) != 1 || cfg.Plugins.Active[0] != "my-plugin" {
		t.Errorf("plugins.active override: %v", cfg.Plugins.Active)
	}
}

func TestLoad_MalformedYAML_ReturnsError(t *testing.T) {
	tmp := t.TempDir()
	writeFile(t, ConfigPath(tmp), "editor:\n  font_family: [unterminated\n  : : :")
	_, err := Load(tmp)
	if err == nil {
		t.Fatalf("malformed YAML must return an error, not silently fall through")
	}
	if !strings.Contains(err.Error(), "parse config.yaml") {
		t.Errorf("error should mention parse, got %v", err)
	}
}

func TestSave_RoundTrip(t *testing.T) {
	tmp := t.TempDir()
	original := Defaults()
	original.Editor.FontFamily = "Custom Font"
	original.Editor.TabIndentSpaces = 8
	original.Hotkeys["custom_action"] = "Ctrl+Shift+X"
	original.Plugins.PluginSettings["my-plugin"] = map[string]any{"key": "value"}
	// Save/Load both run normalize (opt-in disabled seed, nil maps, …). Compare
	// against the normalized form so seed markers are not a false mismatch.
	original = normalize(original)

	if err := Save(tmp, original); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := Load(tmp)
	if err != nil {
		t.Fatalf("Load after Save: %v", err)
	}
	if !reflect.DeepEqual(loaded, original) {
		t.Errorf("round-trip mismatch:\n got  %+v\n want %+v", loaded, original)
	}
}

func TestSave_Atomic_NoPartialWrite(t *testing.T) {
	// Save must leave exactly one config.yaml and no leftover temp files.
	tmp := t.TempDir()
	if err := Save(tmp, Defaults()); err != nil {
		t.Fatalf("Save: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(tmp, ".system"))
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	if len(entries) != 1 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("expected exactly 1 file under .system, got %d: %v", len(entries), names)
	}
}

func TestNormalize_NeverNil(t *testing.T) {
	cfg := normalize(SystemConfig{})
	if cfg.Plugins.Active == nil || cfg.Plugins.Disabled == nil {
		t.Errorf("normalize must produce non-nil plugin slices")
	}
	if cfg.Plugins.PluginSettings == nil {
		t.Errorf("normalize must produce non-nil plugin_settings")
	}
	if cfg.Hotkeys == nil {
		t.Errorf("normalize must produce non-nil hotkeys")
	}
}

// TestDefaults_ContainsSiltTasks pins the Phase 9 (#431) seed: Defaults
// writes a silt-tasks entry covering every key the frontend loaders
// (settings.ts) read, and Active contains silt-tasks (Phase 10 / #429
// retired the standalone silt-calendar and silt-kanban ids).
// TestDefaults_AIFeaturesOffByDefault pins #632: AI product features ship off
// and first-party AI plugins are not listed in plugins.disabled.
func TestDefaults_AIFeaturesOffByDefault(t *testing.T) {
	d := Defaults()
	if d.AI.Features.Enabled || d.AI.Features.RAGEnabled || d.AI.Features.SummariesEnabled {
		t.Fatalf("Defaults AI features must be off; got %+v", d.AI.Features)
	}
	for _, id := range d.Plugins.Disabled {
		if IsFirstPartyAIPlugin(id) {
			t.Errorf("Defaults().Plugins.Disabled must not list first-party AI %q", id)
		}
	}
}

// TestNormalize_MigratesAIFeaturesFromDisabled: legacy per-plugin enables map
// into ai.features once, and AI ids are stripped from plugins.disabled.
// Derivation requires the legacy opt-in seed marker so partial disabled lists
// cannot silently enable AI.
func TestNormalize_MigratesAIFeaturesFromDisabled(t *testing.T) {
	seeded := map[string]any{seededOptInDisabledKey: []string{AIPluginQA, AIPluginAssistant}}

	// All four disabled → features stay off (seeded or not).
	cfg := normalize(SystemConfig{
		Plugins: PluginsConfig{
			Disabled: []string{
				AIPluginSummary, AIPluginQA, AIPluginAssistant, AIPluginAgent,
			},
			PluginSettings: map[string]any{},
		},
	})
	if cfg.AI.Features.Enabled || cfg.AI.Features.RAGEnabled || cfg.AI.Features.SummariesEnabled {
		t.Fatalf("all-disabled vault must keep features off; got %+v", cfg.AI.Features)
	}
	for _, id := range cfg.Plugins.Disabled {
		if IsFirstPartyAIPlugin(id) {
			t.Errorf("AI id %q must be stripped from disabled after migration", id)
		}
	}
	if cfg.Plugins.PluginSettings[aiFeaturesMigratedKey] != true {
		t.Fatal("migration marker must be set")
	}

	// Agent enabled (not in disabled) + seed marker → master on.
	cfg2 := normalize(SystemConfig{
		Plugins: PluginsConfig{
			Disabled:       []string{AIPluginSummary, AIPluginQA, AIPluginAssistant},
			PluginSettings: cloneSettings(seeded),
		},
	})
	if !cfg2.AI.Features.Enabled {
		t.Fatal("agent not disabled must set Features.Enabled")
	}
	if cfg2.AI.Features.RAGEnabled || cfg2.AI.Features.SummariesEnabled {
		t.Fatalf("only agent on should not enable RAG/summaries; got %+v", cfg2.AI.Features)
	}

	// QA enabled → RAG + master.
	cfg3 := normalize(SystemConfig{
		Plugins: PluginsConfig{
			Disabled:       []string{AIPluginSummary, AIPluginAssistant, AIPluginAgent},
			PluginSettings: cloneSettings(seeded),
		},
	})
	if !cfg3.AI.Features.Enabled || !cfg3.AI.Features.RAGEnabled {
		t.Fatalf("qa enabled must set Enabled+RAG; got %+v", cfg3.AI.Features)
	}

	// Summary enabled → summaries + master.
	cfg4 := normalize(SystemConfig{
		Plugins: PluginsConfig{
			Disabled:       []string{AIPluginQA, AIPluginAssistant, AIPluginAgent},
			PluginSettings: cloneSettings(seeded),
		},
	})
	if !cfg4.AI.Features.Enabled || !cfg4.AI.Features.SummariesEnabled {
		t.Fatalf("summary enabled must set Enabled+Summaries; got %+v", cfg4.AI.Features)
	}

	// All four enabled (none in disabled) + seed marker ⇒ every feature on.
	// A power user who enabled all four must not see AI silently turn off on
	// upgrade just because the legacy disabled list is empty (#632).
	cfg5 := normalize(SystemConfig{
		Plugins: PluginsConfig{
			Disabled:       nil,
			PluginSettings: cloneSettings(seeded),
		},
	})
	if !cfg5.AI.Features.Enabled || !cfg5.AI.Features.RAGEnabled || !cfg5.AI.Features.SummariesEnabled {
		t.Fatalf("all-four-enabled seeded vault must turn all features on; got %+v", cfg5.AI.Features)
	}
	for _, id := range cfg5.Plugins.Disabled {
		if IsFirstPartyAIPlugin(id) {
			t.Errorf("AI id %q must be stripped from disabled after migration", id)
		}
	}

	// Second normalize must not re-derive after user turns features off.
	cfg4.AI.Features = AIFeaturesConfig{}
	cfg4 = normalize(cfg4)
	if cfg4.AI.Features.Enabled {
		t.Fatal("post-migration normalize must not re-enable features from empty disabled")
	}

	// Partial unseeded listing must not enable AI (legacy hand-edit / never-normalized).
	partial := normalize(SystemConfig{
		Plugins: PluginsConfig{
			Disabled:       []string{AIPluginSummary},
			PluginSettings: map[string]any{},
		},
	})
	if partial.AI.Features.Enabled || partial.AI.Features.RAGEnabled || partial.AI.Features.SummariesEnabled {
		t.Fatalf("partial unseeded disabled must keep features off; got %+v", partial.AI.Features)
	}
	if partial.Plugins.PluginSettings[aiFeaturesMigratedKey] != true {
		t.Fatal("partial vault must still be marked migrated")
	}
	for _, id := range partial.Plugins.Disabled {
		if IsFirstPartyAIPlugin(id) {
			t.Errorf("AI id %q must be stripped even when features stay off", id)
		}
	}
}

func cloneSettings(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// TestNormalize_ClampsAIFeatureDependents: RAG/summaries cannot stay on when
// master is off.
func TestNormalize_ClampsAIFeatureDependents(t *testing.T) {
	cfg := normalize(SystemConfig{
		AI: AIConfig{
			Features: AIFeaturesConfig{
				Enabled:          false,
				RAGEnabled:       true,
				SummariesEnabled: true,
			},
		},
		Plugins: PluginsConfig{PluginSettings: map[string]any{aiFeaturesMigratedKey: true}},
	})
	if cfg.AI.Features.RAGEnabled || cfg.AI.Features.SummariesEnabled {
		t.Fatalf("dependents must clamp when master off; got %+v", cfg.AI.Features)
	}
}

func TestAIPluginLoadEnabled(t *testing.T) {
	off := AIFeaturesConfig{}
	on := AIFeaturesConfig{Enabled: true}
	rag := AIFeaturesConfig{Enabled: true, RAGEnabled: true}
	sum := AIFeaturesConfig{Enabled: true, SummariesEnabled: true}

	if AIPluginLoadEnabled(off, AIPluginAgent) {
		t.Error("agent off when master off")
	}
	if !AIPluginLoadEnabled(on, AIPluginAgent) || !AIPluginLoadEnabled(on, AIPluginAssistant) {
		t.Error("agent+assistant on when master on")
	}
	if AIPluginLoadEnabled(on, AIPluginQA) {
		t.Error("qa requires RAG")
	}
	if !AIPluginLoadEnabled(rag, AIPluginQA) {
		t.Error("qa on when RAG on")
	}
	if AIPluginLoadEnabled(on, AIPluginSummary) {
		t.Error("summary requires flag")
	}
	if !AIPluginLoadEnabled(sum, AIPluginSummary) {
		t.Error("summary on when flag on")
	}
}

func TestDefaults_ContainsSiltTasks(t *testing.T) {
	d := Defaults()
	ps, ok := d.Plugins.PluginSettings["silt-tasks"].(map[string]any)
	if !ok {
		t.Fatalf("Defaults() missing silt-tasks plugin settings")
	}
	for _, key := range []string{
		"default_display_mode", "default_group_by", "default_sort",
		"default_scope", "calendar_sub_mode", "columns", "filters",
		"saved_views", "local_author",
	} {
		if _, has := ps[key]; !has {
			t.Errorf("silt-tasks defaults missing key %q", key)
		}
	}
	activeIncludes := func(id string) bool {
		for _, a := range d.Plugins.Active {
			if a == id {
				return true
			}
		}
		return false
	}
	if !activeIncludes("silt-tasks") {
		t.Errorf("Defaults().Plugins.Active missing %q", "silt-tasks")
	}
}

func TestValidateHotkeys(t *testing.T) {
	cases := []struct {
		name    string
		hotkeys map[string]string
		wantErr bool
	}{
		{"valid single", map[string]string{"open_search": "Ctrl+Shift+F"}, false},
		{"valid multi-modifier + named", map[string]string{"x": "Ctrl+Shift+Slash"}, false},
		{"empty allowed (disabled)", map[string]string{"open_search": ""}, false},
		{"stray empty segment tolerated", map[string]string{"open_search": "Ctrl++P"}, false},
		{"modifier-only rejected", map[string]string{"open_search": "Ctrl+Shift"}, true},
		{"single modifier rejected", map[string]string{"open_search": "Ctrl"}, true},
		{"whitespace-only rejected", map[string]string{"open_search": "   "}, false}, // trims to empty = disabled
		{"nil map ok", nil, false},
		{"distinct chords ok", map[string]string{"a": "Ctrl+P", "b": "Ctrl+Shift+P"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateHotkeys(c.hotkeys)
			if c.wantErr && err == nil {
				t.Errorf("expected error, got nil")
			}
			if !c.wantErr && err != nil {
				t.Errorf("expected no error, got %v", err)
			}
		})
	}
}

// TestNormalizeHotkeyCollisionMigration confirms the #511 upgrade path: a
// config persisted before the Ctrl+, move carries format_subscript: "Ctrl+,",
// which collides with the new open_settings default after the YAML merge.
// normalize() moves subscript to its new home and leaves customized bindings.
func TestNormalizeHotkeyCollisionMigration(t *testing.T) {
	t.Run("colliding legacy values are migrated", func(t *testing.T) {
		cfg := Defaults()
		cfg.Hotkeys["format_subscript"] = "Ctrl+," // legacy persisted default
		cfg.Hotkeys["open_settings"] = "Ctrl+,"    // new default (collision)
		out := normalize(cfg)
		if out.Hotkeys["format_subscript"] != "Ctrl+Shift," {
			t.Errorf("format_subscript should migrate to Ctrl+Shift,, got %q", out.Hotkeys["format_subscript"])
		}
		if out.Hotkeys["open_settings"] != "Ctrl+," {
			t.Errorf("open_settings should stay Ctrl+,, got %q", out.Hotkeys["open_settings"])
		}
	})
	t.Run("customized subscript is untouched", func(t *testing.T) {
		cfg := Defaults()
		cfg.Hotkeys["format_subscript"] = "Ctrl+Alt+S" // user customized — no collision
		cfg.Hotkeys["open_settings"] = "Ctrl+,"
		out := normalize(cfg)
		if out.Hotkeys["format_subscript"] != "Ctrl+Alt+S" {
			t.Errorf("customized subscript must not be touched, got %q", out.Hotkeys["format_subscript"])
		}
	})
	t.Run("clean defaults are untouched", func(t *testing.T) {
		out := normalize(Defaults())
		if out.Hotkeys["format_subscript"] != "Ctrl+Shift," {
			t.Errorf("clean default subscript changed: %q", out.Hotkeys["format_subscript"])
		}
	})
}

// TestNormalizeHotkeyDefaultsV1Migration confirms the v1 realignment: each
// renamed chord migrates from its exact legacy default to the new default, a
// customized binding is preserved untouched, and a clean current default is
// left alone. Mirrors the format_subscript migration precedent above.
//
// next_tab/prev_tab are Windows-only migrations: on Linux (WebKitGTK) Ctrl+Tab
// works and Ctrl+Alt+←/→ are WM-captured, so the legacy default is preserved
// rather than rewritten. Those two cases run only when runtime.GOOS == "windows"
// (see the platform gate in normalize.go).
func TestNormalizeHotkeyDefaultsV1Migration(t *testing.T) {
	type c struct {
		action string
		legacy string
		want   string
	}
	cases := []c{
		{"toggle_sidebar", "Ctrl+B", "Ctrl+\\"},
		{"toggle_view_mode", "Ctrl+Shift+V", "Ctrl+Alt+R"},
		{"close_tab", "Ctrl+W", "Ctrl+Shift+W"},
		{"next_tab", "Ctrl+Tab", "Ctrl+Alt+Right"},
		{"prev_tab", "Ctrl+Shift+Tab", "Ctrl+Alt+Left"},
	}
	for _, tc := range cases {
		// next_tab/prev_tab migration is Windows-only — the legacy Ctrl+Tab
		// is preserved verbatim on Linux/macOS where Ctrl+Alt+Arrow is captured
		// by the window manager. Skip those two on non-Windows so the test
		// suite stays green across platforms.
		if (tc.action == "next_tab" || tc.action == "prev_tab") && runtime.GOOS != "windows" {
			continue
		}
		t.Run(tc.action+" legacy value migrates", func(t *testing.T) {
			cfg := Defaults()
			cfg.Hotkeys[tc.action] = tc.legacy
			out := normalize(cfg)
			if out.Hotkeys[tc.action] != tc.want {
				t.Errorf("%s should migrate %q → %q, got %q", tc.action, tc.legacy, tc.want, out.Hotkeys[tc.action])
			}
		})
		t.Run(tc.action+" customized value is preserved", func(t *testing.T) {
			cfg := Defaults()
			cfg.Hotkeys[tc.action] = "Ctrl+F24" // a value no default uses
			out := normalize(cfg)
			if out.Hotkeys[tc.action] != "Ctrl+F24" {
				t.Errorf("%s customization must not be touched, got %q", tc.action, out.Hotkeys[tc.action])
			}
		})
	}
	t.Run("clean current defaults are untouched", func(t *testing.T) {
		out := normalize(Defaults())
		for _, tc := range cases {
			if tc.action == "next_tab" || tc.action == "prev_tab" {
				continue // platform-conditional default; checked in TestDefaults_NextPrevTabPlatformConditional
			}
			if out.Hotkeys[tc.action] != tc.want {
				t.Errorf("clean default %s changed: got %q", tc.action, out.Hotkeys[tc.action])
			}
		}
	})
	t.Run("legacy values stamp the one-time notice", func(t *testing.T) {
		cfg := Defaults()
		cfg.Hotkeys["close_tab"] = "Ctrl+W" // legacy
		out := normalize(cfg)
		if !containsString(out.UI.DismissedTips, "hotkeys_defaults_v1_notice") {
			t.Errorf("expected hotkeys_defaults_v1_notice stamp after migration")
		}
	})
	t.Run("clean defaults do not stamp the notice", func(t *testing.T) {
		out := normalize(Defaults())
		if containsString(out.UI.DismissedTips, "hotkeys_defaults_v1_notice") {
			t.Errorf("clean defaults should not stamp the notice")
		}
	})
}

// TestNormalize_NextPrevTabMigration_PlatformGate documents and pins the
// platform gate on the v1 next_tab/prev_tab migration. The migration from
// Ctrl+Tab → Ctrl+Alt+Right (and the prev_tab sibling) only fires on Windows,
// where WebView2 drops Ctrl+Tab; on Linux/macOS the working Ctrl+Tab default
// must survive normalize() unchanged because Ctrl+Alt+←/→ are WM-captured.
//
// We cannot flip runtime.GOOS from a test, so this case asserts the contract
// for the platform the test binary is running on. On Windows the legacy
// Ctrl+Tab IS migrated (the WebView2 case the remap was added for); on
// non-Windows the legacy default is preserved verbatim — pinning the
// platform-branch invariant.
func TestNormalize_NextPrevTabMigration_PlatformGate(t *testing.T) {
	cfg := Defaults()
	cfg.Hotkeys["next_tab"] = "Ctrl+Tab"
	cfg.Hotkeys["prev_tab"] = "Ctrl+Shift+Tab"
	out := normalize(cfg)
	if runtime.GOOS == "windows" {
		if out.Hotkeys["next_tab"] != "Ctrl+Alt+Right" {
			t.Errorf("windows: next_tab should migrate to Ctrl+Alt+Right, got %q", out.Hotkeys["next_tab"])
		}
		if out.Hotkeys["prev_tab"] != "Ctrl+Alt+Left" {
			t.Errorf("windows: prev_tab should migrate to Ctrl+Alt+Left, got %q", out.Hotkeys["prev_tab"])
		}
	} else {
		if out.Hotkeys["next_tab"] != "Ctrl+Tab" {
			t.Errorf("non-windows: next_tab Ctrl+Tab must be preserved, got %q", out.Hotkeys["next_tab"])
		}
		if out.Hotkeys["prev_tab"] != "Ctrl+Shift+Tab" {
			t.Errorf("non-windows: prev_tab Ctrl+Shift+Tab must be preserved, got %q", out.Hotkeys["prev_tab"])
		}
	}
}

// TestDefaults_NextPrevTabPlatformConditional pins the defaults.go
// platform-conditional for next_tab/prev_tab (#863): on Windows the default is
// Ctrl+Alt+Right/Left (WebView2 drops Ctrl+Tab); on Linux/macOS the native
// Ctrl+Tab / Ctrl+Shift+Tab is kept because Ctrl+Alt+←/→ are WM-captured.
// close_tab (Ctrl+Shift+W) is platform-independent.
func TestDefaults_NextPrevTabPlatformConditional(t *testing.T) {
	d := Defaults()
	if runtime.GOOS == "windows" {
		if d.Hotkeys["next_tab"] != "Ctrl+Alt+Right" {
			t.Errorf("windows default next_tab: want Ctrl+Alt+Right, got %q", d.Hotkeys["next_tab"])
		}
		if d.Hotkeys["prev_tab"] != "Ctrl+Alt+Left" {
			t.Errorf("windows default prev_tab: want Ctrl+Alt+Left, got %q", d.Hotkeys["prev_tab"])
		}
	} else {
		if d.Hotkeys["next_tab"] != "Ctrl+Tab" {
			t.Errorf("non-windows default next_tab: want Ctrl+Tab, got %q", d.Hotkeys["next_tab"])
		}
		if d.Hotkeys["prev_tab"] != "Ctrl+Shift+Tab" {
			t.Errorf("non-windows default prev_tab: want Ctrl+Shift+Tab, got %q", d.Hotkeys["prev_tab"])
		}
	}
	// close_tab is reliable on every platform; never conditional.
	if d.Hotkeys["close_tab"] != "Ctrl+Shift+W" {
		t.Errorf("close_tab default: want Ctrl+Shift+W (all platforms), got %q", d.Hotkeys["close_tab"])
	}
}

// TestDefaults_NoGlobalHotkeyChordConflict pins the global-scope uniqueness
// invariant over the REAL Defaults() map — binding directly to the source of
// truth so a future default change that introduces a real conflict fails this
// test. Mirrors frontend/src/shell/defaults.noconflict.test.ts but reads the
// canonical map (no hand-maintained mirror).
//
// Editor-scoped actions (consumed by the editor's ProseMirror keymap while the
// editor is focused, never reaching the global resolver) are EXCLUDED:
//   - prefixes: format_, set_, align_, indent_, unindent_
//   - exact names: toggle_quote, toggle_details, table_insert_*
//
// The one exception is format_bold: Ctrl+B is bold EVERYWHERE (the editor
// unfocused case is global-resolvable too), so it stays in the checked set.
//
// tasks_command_palette is hub-scoped (TasksHub keydown listener, not the
// global resolver) and is excluded; it shares Ctrl+K with editor-scoped
// format_link, disambiguated by the hub's editable-target guard.
func TestDefaults_NoGlobalHotkeyChordConflict(t *testing.T) {
	d := Defaults()
	seen := make(map[string]string) // normalized chord → first action
	count := 0
	for action, chord := range d.Hotkeys {
		if isEditorOrHubScopedAction(action) {
			continue
		}
		count++
		normalized := strings.ToLower(chord)
		if prior, exists := seen[normalized]; exists {
			t.Errorf("global chord conflict: %q and %q both default to %q",
				prior, action, chord)
			continue
		}
		seen[normalized] = action
	}
	// Sanity: the checked set is non-empty — guards against a future edit that
	// accidentally re-classifies every action as editor-scoped, which would
	// make the loop trivially pass.
	if count < 10 {
		t.Errorf("expected at least 10 global-resolvable actions, got %d", count)
	}
}

// isEditorOrHubScopedAction now lives in hotkeys.go so the post-migration
// duplicate-chord scan in normalize() can share the same scope classification
// with this test (single source of truth — keeps the defaults-uniqueness
// invariant here aligned with the runtime conflict scan).

// TestNormalizeHotkeyDefaultsV1_OneShotNoRefire verifies the one-shot gate:
// after the first normalize migrates legacy chords + stamps the marker, a user
// who deliberately re-binds a chord to its legacy value must NOT have it
// re-migrated on a subsequent normalize (which runs on both Load and Save).
// Without the gate, the exact-legacy-match migration would silently clobber
// the explicit post-upgrade remap — violating the "user remaps survive"
// contract (#868 review finding).
func TestNormalizeHotkeyDefaultsV1_OneShotNoRefire(t *testing.T) {
	cfg := Defaults()
	cfg.Hotkeys["toggle_sidebar"] = "Ctrl+B" // legacy default
	out := normalize(cfg)
	if out.Hotkeys["toggle_sidebar"] != "Ctrl+\\" {
		t.Fatalf("first normalize: want Ctrl+\\, got %q", out.Hotkeys["toggle_sidebar"])
	}
	if done, _ := out.Plugins.PluginSettings[hotkeysDefaultsV1MigratedKey].(bool); !done {
		t.Fatal("one-shot migration marker not set after first normalize")
	}
	// Simulate the user re-binding toggle_sidebar back to the legacy Ctrl+B
	// after the upgrade. The next normalize (e.g. on Settings save) must NOT
	// re-migrate it — the marker gates the whole block.
	out.Hotkeys["toggle_sidebar"] = "Ctrl+B"
	out = normalize(out)
	if out.Hotkeys["toggle_sidebar"] != "Ctrl+B" {
		t.Fatalf("second normalize re-migrated an explicit remap: want Ctrl+B preserved, got %q", out.Hotkeys["toggle_sidebar"])
	}
	// The gate must hold across further normalizes.
	out = normalize(out)
	if out.Hotkeys["toggle_sidebar"] != "Ctrl+B" {
		t.Fatalf("third normalize re-migrated: want Ctrl+B preserved, got %q", out.Hotkeys["toggle_sidebar"])
	}
}

// TestNormalizeHotkeys_GlobalConflictScan verifies the post-migration
// duplicate-chord scan: when a vault's deliberate remap puts two
// global-resolvable actions on the same chord, normalize stamps
// hotkeys_global_conflict_notice so the frontend can surface a dismissible
// pointer (first-match-wins would otherwise silently shadow one of them).
// Covers the canonical #868 case: focus_sidebar=Ctrl+B (deliberate remap)
// colliding with format_bold=Ctrl+B (the v1 realignment default).
//
// Observability only — neither binding is rewritten, and the explicit-remap
// contract from TestNormalizeHotkeyDefaultsV1_OneShotNoRefire is preserved.
func TestNormalizeHotkeys_GlobalConflictScan(t *testing.T) {
	t.Run("stamps notice when two global actions share a chord", func(t *testing.T) {
		cfg := Defaults()
		// Deliberate remap of focus_sidebar onto Ctrl+B (the format_bold
		// default). After normalize, both global actions live on Ctrl+B.
		cfg.Hotkeys["focus_sidebar"] = "Ctrl+B"
		out := normalize(cfg)
		if !containsString(out.UI.DismissedTips, hotkeysGlobalConflictNotice) {
			t.Errorf("expected %q stamp for focus_sidebar=Ctrl+B vs format_bold=Ctrl+B",
				hotkeysGlobalConflictNotice)
		}
		// The remap is preserved (observability, not resolution).
		if out.Hotkeys["focus_sidebar"] != "Ctrl+B" {
			t.Errorf("focus_sidebar remap clobbered: got %q", out.Hotkeys["focus_sidebar"])
		}
		if out.Hotkeys["format_bold"] != "Ctrl+B" {
			t.Errorf("format_bold default changed: got %q", out.Hotkeys["format_bold"])
		}
	})

	t.Run("no stamp for clean defaults", func(t *testing.T) {
		out := normalize(Defaults())
		if containsString(out.UI.DismissedTips, hotkeysGlobalConflictNotice) {
			t.Errorf("clean defaults should not stamp %q", hotkeysGlobalConflictNotice)
		}
	})

	t.Run("no stamp for editor-scoped overlap (focus disambiguates)", func(t *testing.T) {
		// format_italic is editor-scoped: it may share a chord with a global
		// action because the editor's ProseMirror keymap owns it while focused.
		cfg := Defaults()
		cfg.Hotkeys["format_italic"] = "Ctrl+N" // same as global new_page
		out := normalize(cfg)
		if containsString(out.UI.DismissedTips, hotkeysGlobalConflictNotice) {
			t.Errorf("editor-scoped overlap should not stamp %q", hotkeysGlobalConflictNotice)
		}
	})

	t.Run("stamp is idempotent across normalizes", func(t *testing.T) {
		cfg := Defaults()
		cfg.Hotkeys["focus_sidebar"] = "Ctrl+B"
		out := normalize(cfg)
		if n := strings.Count(strings.Join(out.UI.DismissedTips, ","), hotkeysGlobalConflictNotice); n != 1 {
			t.Fatalf("first normalize: want 1 stamp, got %d", n)
		}
		// A second normalize (e.g. Settings save) must not duplicate the stamp.
		out = normalize(out)
		if n := strings.Count(strings.Join(out.UI.DismissedTips, ","), hotkeysGlobalConflictNotice); n != 1 {
			t.Errorf("second normalize: want 1 stamp, got %d", n)
		}
	})

	t.Run("resolved conflict does not re-stamp", func(t *testing.T) {
		// When the user fixes the remap after acknowledging, the scan should
		// not re-add the stamp — so the dismissible tip stays dismissed.
		cfg := Defaults()
		cfg.Hotkeys["focus_sidebar"] = "Ctrl+B"
		out := normalize(cfg)
		// User fixes the remap and the dismissed tip is dropped (acked).
		out.Hotkeys["focus_sidebar"] = "Ctrl+Shift+B"
		tips := out.UI.DismissedTips[:0]
		for _, tip := range out.UI.DismissedTips {
			if tip != hotkeysGlobalConflictNotice {
				tips = append(tips, tip)
			}
		}
		out.UI.DismissedTips = tips
		out = normalize(out)
		if containsString(out.UI.DismissedTips, hotkeysGlobalConflictNotice) {
			t.Errorf("resolved conflict should not re-stamp %q", hotkeysGlobalConflictNotice)
		}
	})
}

// TestFindGlobalHotkeyConflicts pins the conflict-finder directly. The
// normalize-driven test above only checks the stamp side-effect; this covers
// the multiple-conflicts and determinism cases.
func TestFindGlobalHotkeyConflicts(t *testing.T) {
	t.Run("empty map on conflict-free config", func(t *testing.T) {
		if got := FindGlobalHotkeyConflicts(Defaults().Hotkeys); len(got) != 0 {
			t.Errorf("expected no conflicts on Defaults, got %v", got)
		}
	})

	t.Run("surfaces two global actions on the same chord", func(t *testing.T) {
		hotkeys := map[string]string{
			"format_bold":   "Ctrl+B",
			"focus_sidebar": "Ctrl+B", // conflict
			"open_settings": "Ctrl+,",
		}
		got := FindGlobalHotkeyConflicts(hotkeys)
		if len(got) != 1 {
			t.Fatalf("expected 1 conflict, got %v", got)
		}
		owners := got["ctrl+b"]
		if len(owners) != 2 {
			t.Fatalf("expected 2 owners on ctrl+b, got %v", owners)
		}
		// Owners are sorted for determinism.
		if owners[0] != "focus_sidebar" || owners[1] != "format_bold" {
			t.Errorf("unexpected owner order: %v", owners)
		}
	})

	t.Run("editor-scoped overlap is not a conflict", func(t *testing.T) {
		hotkeys := map[string]string{
			"format_italic": "Ctrl+K", // editor-scoped
			"new_page":      "Ctrl+K", // global
		}
		if got := FindGlobalHotkeyConflicts(hotkeys); len(got) != 0 {
			t.Errorf("editor/global overlap should not be flagged: %v", got)
		}
	})

	t.Run("disabled binding never conflicts", func(t *testing.T) {
		hotkeys := map[string]string{
			"format_bold":   "",
			"focus_sidebar": "",
		}
		if got := FindGlobalHotkeyConflicts(hotkeys); len(got) != 0 {
			t.Errorf("empty bindings should not conflict: %v", got)
		}
	})
}

// --- #133: co-located per-notebook config ---

// TestLinkedConfigPath confirms the co-located path lives at
// <linkedRoot>/.system/config.yaml, mirroring the vault config layout so the
// same per-notebook-attached-state contract holds on both roots.
func TestLinkedConfigPath(t *testing.T) {
	got := LinkedConfigPath("/mnt/share/Ext")
	want := filepath.ToSlash(filepath.Join("/mnt/share/Ext", ".system", "config.yaml"))
	if filepath.ToSlash(got) != want {
		t.Errorf("LinkedConfigPath = %q, want %q", got, want)
	}
}

// TestLoadLinked_MissingFileReturnsDefaults verifies the normal case: a
// linked notebook WITHOUT a co-located config.yaml is not an error — the
// vault-scoped config.yaml provides the baseline, and Defaults fills any gap.
func TestLoadLinked_MissingFileReturnsDefaults(t *testing.T) {
	tmp := t.TempDir() // no .system/config.yaml
	cfg, err := LoadLinked(tmp)
	if err != nil {
		t.Fatalf("missing co-located file should not error, got %v", err)
	}
	// Defaults() shape — the default active notebook proves we got the
	// canonical defaults rather than a zero struct.
	if cfg.Notebooks.DefaultActive != Defaults().Notebooks.DefaultActive {
		t.Errorf("expected Defaults(), got DefaultActive=%q", cfg.Notebooks.DefaultActive)
	}
	if cfg.Plugins.PluginSettings == nil {
		t.Error("expected non-nil PluginSettings from Defaults()")
	}
}

// TestLoadLinked_ParsesAndOverrides confirms a present co-located config.yaml
// is parsed and its plugin_settings surface (the merge-with-vault happens at
// the App layer, not here — LoadLinked returns the parsed linked config
// verbatim).
func TestLoadLinked_ParsesAndOverrides(t *testing.T) {
	tmp := t.TempDir()
	writeFile(t, LinkedConfigPath(tmp), ""+
		"plugins:\n"+
		"  plugin_settings:\n"+
		"    my-plugin:\n"+
		"      columns: [Backlog, In Progress, Done]\n"+
		"      theme: dark\n")
	cfg, err := LoadLinked(tmp)
	if err != nil {
		t.Fatalf("LoadLinked: %v", err)
	}
	plug, ok := cfg.Plugins.PluginSettings["my-plugin"].(map[string]any)
	if !ok {
		t.Fatalf("expected my-plugin settings map, got %T", cfg.Plugins.PluginSettings["my-plugin"])
	}
	if plug["theme"] != "dark" {
		t.Errorf("theme override: got %v, want dark", plug["theme"])
	}
	cols, ok := plug["columns"].([]any)
	if !ok || len(cols) != 3 {
		t.Errorf("columns override: got %v", plug["columns"])
	}
}

// TestLoadLinked_UnparseableReturnsError locks the fail-loud contract: a
// present-but-broken co-located config must NOT silently fall through to
// Defaults (that would hide a user's broken file from them).
func TestLoadLinked_UnparseableReturnsError(t *testing.T) {
	tmp := t.TempDir()
	writeFile(t, LinkedConfigPath(tmp), "plugins:\n  plugin_settings: [unterminated\n  : : :")
	_, err := LoadLinked(tmp)
	if err == nil {
		t.Fatalf("unparseable co-located config must return an error")
	}
	if !strings.Contains(err.Error(), "parse linked config.yaml") {
		t.Errorf("error should mention parse, got %v", err)
	}
}

// TestMergePluginSettings_LinkedOverridesVaultPerKey covers the merge contract:
// linked keys win per-key; nested maps merge recursively; scalars and arrays
// from linked REPLACE vault's; vault-only keys survive; neither input is
// mutated.
func TestMergePluginSettings_LinkedOverridesVaultPerKey(t *testing.T) {
	vault := map[string]any{
		"columns": []any{"TODO", "DOING", "DONE"},
		"filters": map[string]any{
			"owners":     []any{"Alice"},
			"priorities": []any{1, 2},
		},
		"vault_only": "keep",
	}
	linked := map[string]any{
		"columns": []any{"Backlog", "Done"},
		"filters": map[string]any{
			"priorities": []any{3},
			"tags":       []any{"work"},
		},
		"linked_only": "add",
	}

	// Snapshot inputs to prove MergePluginSettings does not mutate them.
	vaultBefore := deepCopy(vault)
	linkedBefore := deepCopy(linked)

	got := MergePluginSettings(vault, linked)

	// Scalar/array: linked replaces vault.
	if cols, ok := got["columns"].([]any); !ok || len(cols) != 2 || cols[0] != "Backlog" {
		t.Errorf("columns: expected linked to replace vault, got %v", got["columns"])
	}
	// Nested map: recursive per-key merge.
	filters, ok := got["filters"].(map[string]any)
	if !ok {
		t.Fatalf("filters missing or wrong type: %T", got["filters"])
	}
	// vault-only sub-key preserved.
	if owners, ok := filters["owners"].([]any); !ok || len(owners) != 1 || owners[0] != "Alice" {
		t.Errorf("filters.owners: expected vault preserved, got %v", filters["owners"])
	}
	// linked sub-key replaces vault's (array replacement, same as top-level).
	// reflect.DeepEqual verifies both value AND type (yaml.v3 decodes
	// integer literals as `int`, not int64 — but the type must match exactly).
	if !reflect.DeepEqual(filters["priorities"], []any{3}) {
		t.Errorf("filters.priorities: expected linked to replace vault with [3], got %v", filters["priorities"])
	}
	// linked-only sub-key added.
	if tags, ok := filters["tags"].([]any); !ok || len(tags) != 1 || tags[0] != "work" {
		t.Errorf("filters.tags: expected linked-only addition, got %v", filters["tags"])
	}
	// vault-only top-level key preserved.
	if got["vault_only"] != "keep" {
		t.Errorf("vault_only: expected preserved, got %v", got["vault_only"])
	}
	// linked-only top-level key added.
	if got["linked_only"] != "add" {
		t.Errorf("linked_only: expected added, got %v", got["linked_only"])
	}

	// Inputs not mutated.
	if !reflect.DeepEqual(vault, vaultBefore) {
		t.Errorf("MergePluginSettings mutated vault input:\n before=%v\n after =%v", vaultBefore, vault)
	}
	if !reflect.DeepEqual(linked, linkedBefore) {
		t.Errorf("MergePluginSettings mutated linked input:\n before=%v\n after =%v", linkedBefore, linked)
	}
}

// TestMergePluginSettings_NilInputsAreEmpty confirms both nil inputs are
// tolerated and the result is always a non-nil map.
func TestMergePluginSettings_NilInputsAreEmpty(t *testing.T) {
	got := MergePluginSettings(nil, nil)
	if got == nil {
		t.Fatal("expected non-nil result for nil inputs")
	}
	if len(got) != 0 {
		t.Errorf("expected empty merge of two nils, got %v", got)
	}

	got = MergePluginSettings(map[string]any{"a": 1}, nil)
	if got["a"] != 1 {
		t.Errorf("vault-only merge lost key, got %v", got)
	}
	got = MergePluginSettings(nil, map[string]any{"b": 2})
	if got["b"] != 2 {
		t.Errorf("linked-only merge lost key, got %v", got)
	}
}

// deepCopy is a test-only helper that clones a map[string]any snapshot for
// mutation-comparison. It does not need to handle every YAML type — only the
// types used in the merge tests above.
func deepCopy(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		switch x := v.(type) {
		case map[string]any:
			out[k] = deepCopy(x)
		case []any:
			cp := make([]any, len(x))
			copy(cp, x)
			out[k] = cp
		default:
			out[k] = v
		}
	}
	return out
}

// --- #142: open-tab persistence config ---

// TestDefaults_TabsConfig verifies the tab-strip defaults ship in Defaults():
// enable_preview_tabs=true, max_open_tabs=8, next_tab/prev_tab/close_tab
// hotkeys present, and OpenTabs is a non-nil empty slice (not nil) so JSON
// serialization over IPC never yields null.
func TestDefaults_TabsConfig(t *testing.T) {
	d := Defaults()
	if d.UI.EnablePreviewTabs == nil || *d.UI.EnablePreviewTabs != true {
		t.Errorf("defaults enable_preview_tabs should be *true, got %v", d.UI.EnablePreviewTabs)
	}
	if d.UI.MaxOpenTabs != 8 {
		t.Errorf("defaults max_open_tabs should be 8, got %d", d.UI.MaxOpenTabs)
	}
	if d.UI.OpenTabs == nil {
		t.Errorf("defaults open_tabs should be non-nil empty slice, got nil")
	}
	if len(d.UI.OpenTabs) != 0 {
		t.Errorf("defaults open_tabs should be empty, got %v", d.UI.OpenTabs)
	}
	for _, key := range []string{"next_tab", "prev_tab", "close_tab"} {
		if _, ok := d.Hotkeys[key]; !ok {
			t.Errorf("defaults hotkeys missing %q", key)
		}
	}
	// Tab-strip chords were remapped off Ctrl+W / Ctrl+Tab because WebView2
	// unreliably relays those to the webview. See defaults.go for the rationale.
	if d.Hotkeys["next_tab"] != "Ctrl+Alt+Right" {
		t.Errorf("next_tab default: got %q", d.Hotkeys["next_tab"])
	}
	if d.Hotkeys["prev_tab"] != "Ctrl+Alt+Left" {
		t.Errorf("prev_tab default: got %q", d.Hotkeys["prev_tab"])
	}
	if d.Hotkeys["close_tab"] != "Ctrl+Shift+W" {
		t.Errorf("close_tab default: got %q", d.Hotkeys["close_tab"])
	}
}

func TestNavigationPreferences_NormalizeAndRoundTrip(t *testing.T) {
	cfg := Defaults()
	cfg.UI.ExpandedSections = []NavigationSectionRef{
		{Notebook: "Work", Path: "Projects/Active"},
		{Notebook: "Work", Path: "Projects/Active"},
		{Notebook: "Work", Path: "../escape"},
	}
	cfg.UI.Favorites = []NavigationPageRef{
		{Notebook: "Work", Section: "Projects/Active", Page: "Site"},
		{Notebook: "Work", Section: "Projects/Active", Page: "Site"},
		{Notebook: "", Page: "broken"},
	}
	cfg.UI.RecentPages = []RecentPage{
		{NavigationPageRef: NavigationPageRef{Notebook: "Work", Section: "Projects/Active", Page: "Site"}, OpenedAt: 10},
		{NavigationPageRef: NavigationPageRef{Notebook: "Work", Section: "Projects/Active", Page: "Site"}, OpenedAt: 20},
		{NavigationPageRef: NavigationPageRef{Notebook: "Work", Section: "../escape", Page: "Bad"}, OpenedAt: 30},
	}
	normalized := Normalize(cfg)
	if len(normalized.UI.ExpandedSections) != 1 || normalized.UI.ExpandedSections[0].Path != "Projects/Active" {
		t.Fatalf("expanded sections were not normalized: %+v", normalized.UI.ExpandedSections)
	}
	if len(normalized.UI.Favorites) != 1 || len(normalized.UI.RecentPages) != 1 || normalized.UI.RecentPages[0].OpenedAt != 20 {
		t.Fatalf("page preferences were not normalized: favorites=%+v recent=%+v", normalized.UI.Favorites, normalized.UI.RecentPages)
	}
	tmp := t.TempDir()
	if err := Save(tmp, normalized); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := Load(tmp)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(loaded.UI.ExpandedSections, normalized.UI.ExpandedSections) || !reflect.DeepEqual(loaded.UI.Favorites, normalized.UI.Favorites) || !reflect.DeepEqual(loaded.UI.RecentPages, normalized.UI.RecentPages) {
		t.Fatalf("navigation preferences did not round-trip: loaded=%+v", loaded.UI)
	}
}

func TestSidebarView_DefaultNormalizeMigrationAndRoundTrip(t *testing.T) {
	// Defaults() intentionally does NOT seed SidebarView (normalize owns it),
	// so a raw Defaults() value has nil here. Normalize finalizes it to "tree".
	defaults := Defaults()
	if defaults.UI.SidebarView != nil {
		t.Fatalf("Defaults() must not seed SidebarView (normalize owns it), got %v", defaults.UI.SidebarView)
	}
	normalized := Normalize(defaults)
	if normalized.UI.SidebarView == nil || *normalized.UI.SidebarView != "tree" {
		t.Fatalf("nil SidebarView should normalize to \"tree\", got %v", normalized.UI.SidebarView)
	}

	// Invalid value collapses to the "tree" default.
	bogus := "bogus"
	legacy := Defaults()
	legacy.UI.SidebarView = &bogus
	normalized = Normalize(legacy)
	if normalized.UI.SidebarView == nil || *normalized.UI.SidebarView != "tree" {
		t.Fatalf("invalid SidebarView should normalize to \"tree\", got %v", normalized.UI.SidebarView)
	}

	// Explicit "quick" survives normalization.
	quick := "quick"
	legacy = Defaults()
	legacy.UI.SidebarView = &quick
	normalized = Normalize(legacy)
	if normalized.UI.SidebarView == nil || *normalized.UI.SidebarView != "quick" {
		t.Fatalf("explicit \"quick\" should survive normalization, got %v", normalized.UI.SidebarView)
	}

	// Legacy quick_access_collapsed: false migrates to sidebar_view: "quick".
	vaultPath := t.TempDir()
	writeFile(t, ConfigPath(vaultPath), "ui:\n  sidebar_width: 256\n  quick_access_collapsed: false\n")
	loaded, err := Load(vaultPath)
	if err != nil {
		t.Fatalf("Load legacy config with collapsed=false: %v", err)
	}
	if loaded.UI.SidebarView == nil || *loaded.UI.SidebarView != "quick" {
		t.Fatalf("legacy quick_access_collapsed: false should migrate to \"quick\", got %v", loaded.UI.SidebarView)
	}

	// Legacy quick_access_collapsed: true migrates to sidebar_view: "tree".
	writeFile(t, ConfigPath(vaultPath), "ui:\n  sidebar_width: 256\n  quick_access_collapsed: true\n")
	loaded, err = Load(vaultPath)
	if err != nil {
		t.Fatalf("Load legacy config with collapsed=true: %v", err)
	}
	if loaded.UI.SidebarView == nil || *loaded.UI.SidebarView != "tree" {
		t.Fatalf("legacy quick_access_collapsed: true should migrate to \"tree\", got %v", loaded.UI.SidebarView)
	}

	// Legacy config without either key loads as the "tree" default.
	writeFile(t, ConfigPath(vaultPath), "ui:\n  sidebar_width: 256\n")
	loaded, err = Load(vaultPath)
	if err != nil {
		t.Fatalf("Load legacy config without sidebar key: %v", err)
	}
	if loaded.UI.SidebarView == nil || *loaded.UI.SidebarView != "tree" {
		t.Fatalf("absent sidebar key should default to \"tree\", got %v", loaded.UI.SidebarView)
	}

	// When both keys are present, the new sidebar_view wins over the legacy bool.
	writeFile(t, ConfigPath(vaultPath), "ui:\n  sidebar_width: 256\n  quick_access_collapsed: false\n  sidebar_view: tree\n")
	loaded, err = Load(vaultPath)
	if err != nil {
		t.Fatalf("Load config with both keys: %v", err)
	}
	if loaded.UI.SidebarView == nil || *loaded.UI.SidebarView != "tree" {
		t.Fatalf("new sidebar_view should win over legacy quick_access_collapsed, got %v", loaded.UI.SidebarView)
	}

	// Explicit "quick" round-trips through Save/Load and the legacy key is NOT
	// re-emitted (the field is gone from SystemConfig, so Save cannot write it).
	roundTripPath := t.TempDir()
	if err := Save(roundTripPath, normalized); err != nil {
		t.Fatalf("Save sidebar_view preference: %v", err)
	}
	roundTripped, err := Load(roundTripPath)
	if err != nil {
		t.Fatalf("Load round-tripped config: %v", err)
	}
	if roundTripped.UI.SidebarView == nil || *roundTripped.UI.SidebarView != "quick" {
		t.Fatalf("explicit \"quick\" did not round-trip, got %v", roundTripped.UI.SidebarView)
	}
	savedBytes, err := os.ReadFile(ConfigPath(roundTripPath))
	if err != nil {
		t.Fatalf("read saved config: %v", err)
	}
	if strings.Contains(string(savedBytes), "quick_access_collapsed") {
		t.Fatalf("Save must not emit the legacy quick_access_collapsed key, got:\n%s", string(savedBytes))
	}
}

// TestOpenTabs_RoundTrip confirms OpenTabs + ActiveTab survive Save → Load
// with byte-for-byte fidelity, including the section-less case (Section == "").
func TestOpenTabs_RoundTrip(t *testing.T) {
	tmp := t.TempDir()
	original := Defaults()
	previewOff := false
	original.UI.EnablePreviewTabs = &previewOff
	original.UI.MaxOpenTabs = 12
	original.UI.OpenTabs = []TabRef{
		{Notebook: "Work", Section: "Projects", Page: "Site"},
		{Notebook: "Work", Section: "", Page: "Top"},
		{Notebook: "Personal", Section: "Journal", Page: "Daily"},
	}
	original.UI.ActiveTab = &TabRef{Notebook: "Work", Section: "Projects", Page: "Site"}

	if err := Save(tmp, original); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := Load(tmp)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(loaded.UI.OpenTabs, original.UI.OpenTabs) {
		t.Errorf("open_tabs round-trip:\n got  %+v\n want %+v", loaded.UI.OpenTabs, original.UI.OpenTabs)
	}
	if loaded.UI.ActiveTab == nil || !reflect.DeepEqual(*loaded.UI.ActiveTab, *original.UI.ActiveTab) {
		t.Errorf("active_tab round-trip:\n got  %+v\n want %+v", loaded.UI.ActiveTab, original.UI.ActiveTab)
	}
	if loaded.UI.EnablePreviewTabs == nil || *loaded.UI.EnablePreviewTabs != false {
		t.Errorf("enable_preview_tabs=false round-trip: got %v", loaded.UI.EnablePreviewTabs)
	}
	if loaded.UI.MaxOpenTabs != 12 {
		t.Errorf("max_open_tabs round-trip: got %d, want 12", loaded.UI.MaxOpenTabs)
	}
}

// TestLoad_LegacyConfigMissingTabFields verifies a config.yaml authored
// before #142 (no ui.open_tabs / enable_preview_tabs / max_open_tabs keys)
// loads cleanly with the new fields filled from Defaults — backward compat.
func TestLoad_LegacyConfigMissingTabFields(t *testing.T) {
	tmp := t.TempDir()
	writeFile(t, ConfigPath(tmp), strings.Join([]string{
		"editor:",
		"  font_family: Inter",
		"ui:",
		"  sidebar_width: 280",
	}, "\n"))
	cfg, err := Load(tmp)
	if err != nil {
		t.Fatalf("legacy config Load: %v", err)
	}
	// The pre-existing ui.sidebar_width override is honored.
	if cfg.UI.SidebarWidth != 280 {
		t.Errorf("sidebar_width override lost: got %d", cfg.UI.SidebarWidth)
	}
	// The new fields default-in cleanly (not zero-value).
	if cfg.UI.OpenTabs == nil || len(cfg.UI.OpenTabs) != 0 {
		t.Errorf("legacy open_tabs should default to empty non-nil slice, got %v", cfg.UI.OpenTabs)
	}
	if cfg.UI.EnablePreviewTabs == nil || *cfg.UI.EnablePreviewTabs != true {
		t.Errorf("legacy enable_preview_tabs should default to *true, got %v", cfg.UI.EnablePreviewTabs)
	}
	if cfg.UI.MaxOpenTabs != 8 {
		t.Errorf("legacy max_open_tabs should default to 8, got %d", cfg.UI.MaxOpenTabs)
	}
	if cfg.UI.ActiveTab != nil {
		t.Errorf("legacy active_tab should default to nil, got %+v", *cfg.UI.ActiveTab)
	}
}

// TestLoad_MalformedOpenTabsEntryNotFatal confirms a malformed open_tabs
// entry does NOT abort the entire config load — yaml.v3 decodes a
// missing-field entry as an empty TabRef, which the App-layer GetOpenTabs
// prunes against ListNavigation. A parse-level error is still raised for
// genuinely broken YAML (covered by TestLoad_MalformedYAML_ReturnsError).
func TestLoad_MalformedOpenTabsEntryNotFatal(t *testing.T) {
	tmp := t.TempDir()
	writeFile(t, ConfigPath(tmp), strings.Join([]string{
		"ui:",
		"  open_tabs:",
		"    - notebook: Work",
		"      section: Projects",
		"      page: Site",
		"    - notebook: Personal",
		"      # page missing — decodes as empty string, pruned later",
	}, "\n"))
	cfg, err := Load(tmp)
	if err != nil {
		t.Fatalf("malformed open_tabs entry should not be fatal, got %v", err)
	}
	if len(cfg.UI.OpenTabs) != 2 {
		t.Fatalf("expected 2 open_tabs entries (1 valid, 1 partial), got %d", len(cfg.UI.OpenTabs))
	}
	// The partial entry decodes with an empty Page; the App-layer
	// GetOpenTabs prunes it against ListNavigation.
	if cfg.UI.OpenTabs[1].Page != "" {
		t.Errorf("partial entry page should be empty string, got %q", cfg.UI.OpenTabs[1].Page)
	}
}

// TestNormalize_MaxOpenTabsClamp confirms MaxOpenTabs of 0 or negative
// (legacy/invalid) is normalized to the default 8, while positive values
// pass through untouched (including 1 and very large values).
func TestNormalize_MaxOpenTabsClamp(t *testing.T) {
	cases := []struct {
		in, want int
	}{
		{0, 8},     // legacy missing key → default
		{-1, 8},    // invalid negative → default
		{1, 1},     // minimum valid
		{8, 8},     // the default itself
		{20, 20},   // user-configured large value honored
		{32, 32},   // upper bound
		{33, 32},   // clamped to upper bound
		{1000, 32}, // absurdly large → clamped (#142 hardening)
	}
	for _, c := range cases {
		cfg := normalize(SystemConfig{UI: UIConfig{MaxOpenTabs: c.in}})
		if cfg.UI.MaxOpenTabs != c.want {
			t.Errorf("normalize MaxOpenTabs %d: got %d, want %d", c.in, cfg.UI.MaxOpenTabs, c.want)
		}
	}
}

// TestNormalize_EnablePreviewTabsNilBecomesTrue confirms the *bool field is
// normalized to *true when nil (so the frontend reads a stable default),
// while an explicit false survives the normalize pass unchanged.
func TestNormalize_EnablePreviewTabsNilBecomesTrue(t *testing.T) {
	// nil → *true
	cfg := normalize(SystemConfig{})
	if cfg.UI.EnablePreviewTabs == nil || *cfg.UI.EnablePreviewTabs != true {
		t.Errorf("normalize nil → *true, got %v", cfg.UI.EnablePreviewTabs)
	}
	// explicit false survives
	f := false
	cfg = normalize(SystemConfig{UI: UIConfig{EnablePreviewTabs: &f}})
	if cfg.UI.EnablePreviewTabs == nil || *cfg.UI.EnablePreviewTabs != false {
		t.Errorf("normalize should preserve explicit false, got %v", cfg.UI.EnablePreviewTabs)
	}
}

// TestDefaults_FormattingConfig confirms the #168 formatting config fields and
// hotkeys have correct defaults.
func TestDefaults_FormattingConfig(t *testing.T) {
	d := Defaults()
	if d.UI.ShowFormatToolbar == nil || *d.UI.ShowFormatToolbar != true {
		t.Errorf("defaults show_format_toolbar should be *true, got %v", d.UI.ShowFormatToolbar)
	}
	if d.UI.DismissedTips == nil {
		t.Errorf("defaults dismissed_tips should be non-nil empty slice")
	}
	if len(d.UI.DismissedTips) != 0 {
		t.Errorf("defaults dismissed_tips should be empty, got %v", d.UI.DismissedTips)
	}
	for _, key := range []string{
		"format_bold", "format_italic", "format_underline", "format_strike",
		"format_code", "format_link", "format_highlight",
		"format_subscript", "format_superscript",
	} {
		if _, ok := d.Hotkeys[key]; !ok {
			t.Errorf("defaults hotkeys missing %q", key)
		}
	}
	// Heading level hotkeys (#169 / #645).
	for _, key := range []string{
		"set_h1", "set_h2", "set_h3", "set_h4", "set_h5", "set_h6",
		"set_note", "set_task",
	} {
		if _, ok := d.Hotkeys[key]; !ok {
			t.Errorf("defaults hotkeys missing %q", key)
		}
	}
	if d.Hotkeys["format_bold"] != "Ctrl+B" {
		t.Errorf("format_bold default: got %q", d.Hotkeys["format_bold"])
	}
	if d.Hotkeys["format_italic"] != "Ctrl+I" {
		t.Errorf("format_italic default: got %q", d.Hotkeys["format_italic"])
	}
	if d.Hotkeys["format_subscript"] != "Ctrl+Shift," {
		t.Errorf("format_subscript default: got %q", d.Hotkeys["format_subscript"])
	}
	// open_settings (#511): the universal Ctrl+, settings convention.
	if d.Hotkeys["open_settings"] != "Ctrl+," {
		t.Errorf("open_settings default: got %q", d.Hotkeys["open_settings"])
	}
	// tasks_command_palette (#436): hub-scoped Ctrl+K (format_link keeps the
	// same default; focus scope resolves the conflict).
	if d.Hotkeys["tasks_command_palette"] != "Ctrl+K" {
		t.Errorf("tasks_command_palette default: got %q", d.Hotkeys["tasks_command_palette"])
	}
	// Alignment (#173) + blockquote (#188) hotkeys.
	for _, key := range []string{
		"align_left", "align_center", "align_right", "align_justify", "toggle_quote",
	} {
		if _, ok := d.Hotkeys[key]; !ok {
			t.Errorf("defaults hotkeys missing %q", key)
		}
	}
	if d.Hotkeys["toggle_quote"] != "Ctrl+Shift+9" {
		t.Errorf("toggle_quote default: got %q", d.Hotkeys["toggle_quote"])
	}
	if d.Hotkeys["toggle_details"] != "Ctrl+Shift+." {
		t.Errorf("toggle_details default: got %q", d.Hotkeys["toggle_details"])
	}
	// Table row/column insert hotkeys (#172).
	for _, key := range []string{
		"table_insert_row_above", "table_insert_row_below",
		"table_insert_col_left", "table_insert_col_right",
	} {
		if _, ok := d.Hotkeys[key]; !ok {
			t.Errorf("defaults hotkeys missing %q", key)
		}
	}
}

// TestFormattingConfig_RoundTrip confirms ShowFormatToolbar + DismissedTips
// survive Save → Load with byte-for-byte fidelity.
func TestFormattingConfig_RoundTrip(t *testing.T) {
	tmp := t.TempDir()
	original := Defaults()
	toolbarOff := false
	original.UI.ShowFormatToolbar = &toolbarOff
	original.UI.DismissedTips = []string{"formatting_tip_v1", "other_tip"}

	if err := Save(tmp, original); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := Load(tmp)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.UI.ShowFormatToolbar == nil || *loaded.UI.ShowFormatToolbar != false {
		t.Errorf("show_format_toolbar=false round-trip: got %v", loaded.UI.ShowFormatToolbar)
	}
	if !reflect.DeepEqual(loaded.UI.DismissedTips, original.UI.DismissedTips) {
		t.Errorf("dismissed_tips round-trip:\n got  %+v\n want %+v", loaded.UI.DismissedTips, original.UI.DismissedTips)
	}
}

// TestNormalize_ShowFormatToolbarNilBecomesTrue confirms the *bool field is
// normalized to *true when nil (so the frontend reads a stable default).
func TestNormalize_ShowFormatToolbarNilBecomesTrue(t *testing.T) {
	cfg := normalize(SystemConfig{})
	if cfg.UI.ShowFormatToolbar == nil || *cfg.UI.ShowFormatToolbar != true {
		t.Errorf("normalize nil → *true, got %v", cfg.UI.ShowFormatToolbar)
	}
	f := false
	cfg = normalize(SystemConfig{UI: UIConfig{ShowFormatToolbar: &f}})
	if cfg.UI.ShowFormatToolbar == nil || *cfg.UI.ShowFormatToolbar != false {
		t.Errorf("normalize should preserve explicit false, got %v", cfg.UI.ShowFormatToolbar)
	}
	if cfg.UI.DismissedTips == nil {
		t.Errorf("normalize should ensure non-nil dismissed_tips")
	}
}

// TestDefaults_EditorEnhancements confirms the Phase 3 editor enhancement
// config fields have correct defaults.
func TestDefaults_EditorEnhancements(t *testing.T) {
	d := Defaults()
	if d.UI.Formatting.TypographyEnabled == nil || *d.UI.Formatting.TypographyEnabled != true {
		t.Errorf("defaults typography_enabled should be *true, got %v", d.UI.Formatting.TypographyEnabled)
	}
	if d.Editor.ShowWordCount == nil || *d.Editor.ShowWordCount != false {
		t.Errorf("defaults show_word_count should be *false, got %v", d.Editor.ShowWordCount)
	}
	if d.Editor.FocusMode == nil || *d.Editor.FocusMode != false {
		t.Errorf("defaults focus_mode should be *false, got %v", d.Editor.FocusMode)
	}
}

// TestNormalize_EditorEnhancements confirms *bool normalization for the
// Phase 3 fields.
func TestNormalize_EditorEnhancements(t *testing.T) {
	// nil → defaults
	cfg := normalize(SystemConfig{})
	if cfg.UI.Formatting.TypographyEnabled == nil || *cfg.UI.Formatting.TypographyEnabled != true {
		t.Errorf("normalize typography nil → *true, got %v", cfg.UI.Formatting.TypographyEnabled)
	}
	if cfg.Editor.ShowWordCount == nil || *cfg.Editor.ShowWordCount != false {
		t.Errorf("normalize show_word_count nil → *false, got %v", cfg.Editor.ShowWordCount)
	}
	if cfg.Editor.FocusMode == nil || *cfg.Editor.FocusMode != false {
		t.Errorf("normalize focus_mode nil → *false, got %v", cfg.Editor.FocusMode)
	}
	// Explicit values survive
	tv := true
	cfg = normalize(SystemConfig{UI: UIConfig{Formatting: FormattingConfig{TypographyEnabled: &tv}}})
	if cfg.UI.Formatting.TypographyEnabled == nil || *cfg.UI.Formatting.TypographyEnabled != true {
		t.Errorf("normalize should preserve explicit typography true, got %v", cfg.UI.Formatting.TypographyEnabled)
	}
	fv := false
	cfg = normalize(SystemConfig{UI: UIConfig{Formatting: FormattingConfig{TypographyEnabled: &fv}}})
	if cfg.UI.Formatting.TypographyEnabled == nil || *cfg.UI.Formatting.TypographyEnabled != false {
		t.Errorf("normalize should preserve explicit typography false, got %v", cfg.UI.Formatting.TypographyEnabled)
	}
}

// TestDefaults_ShowTabDirtyIndicators confirms the #167 tab dirty indicator
// toggle defaults to *true.
func TestDefaults_ShowTabDirtyIndicators(t *testing.T) {
	d := Defaults()
	if d.UI.ShowTabDirtyIndicators == nil || *d.UI.ShowTabDirtyIndicators != true {
		t.Errorf("defaults show_tab_dirty_indicators should be *true, got %v", d.UI.ShowTabDirtyIndicators)
	}
}

// TestNormalize_ShowTabDirtyIndicatorsNilBecomesTrue confirms nil normalizes
// to *true (legacy config without the key) while explicit false is preserved.
func TestNormalize_ShowTabDirtyIndicatorsNilBecomesTrue(t *testing.T) {
	// nil → *true
	cfg := normalize(SystemConfig{})
	if cfg.UI.ShowTabDirtyIndicators == nil || *cfg.UI.ShowTabDirtyIndicators != true {
		t.Errorf("normalize nil → *true, got %v", cfg.UI.ShowTabDirtyIndicators)
	}
	// explicit false preserved
	f := false
	cfg = normalize(SystemConfig{UI: UIConfig{ShowTabDirtyIndicators: &f}})
	if cfg.UI.ShowTabDirtyIndicators == nil || *cfg.UI.ShowTabDirtyIndicators != false {
		t.Errorf("normalize should preserve explicit false, got %v", cfg.UI.ShowTabDirtyIndicators)
	}
	// explicit true preserved
	tv := true
	cfg = normalize(SystemConfig{UI: UIConfig{ShowTabDirtyIndicators: &tv}})
	if cfg.UI.ShowTabDirtyIndicators == nil || *cfg.UI.ShowTabDirtyIndicators != true {
		t.Errorf("normalize should preserve explicit true, got %v", cfg.UI.ShowTabDirtyIndicators)
	}
}

// TestShowTabDirtyIndicators_RoundTrip confirms YAML round-trip for the
// #167 toggle (true, false, and legacy-missing-key paths).
func TestShowTabDirtyIndicators_RoundTrip(t *testing.T) {
	tmp := t.TempDir()
	original := Defaults()
	off := false
	original.UI.ShowTabDirtyIndicators = &off

	if err := Save(tmp, original); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := Load(tmp)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.UI.ShowTabDirtyIndicators == nil || *loaded.UI.ShowTabDirtyIndicators != false {
		t.Errorf("show_tab_dirty_indicators=false round-trip: got %v", loaded.UI.ShowTabDirtyIndicators)
	}
}

// TestLoad_LegacyConfigMissingShowTabDirtyIndicators verifies a config.yaml
// authored before #167 (no ui.show_tab_dirty_indicators key) loads cleanly
// with the field defaulted to *true — backward compat.
func TestLoad_LegacyConfigMissingShowTabDirtyIndicators(t *testing.T) {
	tmp := t.TempDir()
	writeFile(t, ConfigPath(tmp), strings.Join([]string{
		"editor:",
		"  font_family: Inter",
		"ui:",
		"  sidebar_width: 280",
	}, "\n"))
	cfg, err := Load(tmp)
	if err != nil {
		t.Fatalf("legacy config Load: %v", err)
	}
	if cfg.UI.ShowTabDirtyIndicators == nil || *cfg.UI.ShowTabDirtyIndicators != true {
		t.Errorf("legacy show_tab_dirty_indicators should default to *true, got %v", cfg.UI.ShowTabDirtyIndicators)
	}
}

// --- #195: per-tab view mode persistence (TabRef.ViewMode) ---

// TestTabRef_ViewMode_RoundTrip confirms a Source-mode tab persists
// view_mode across Save → Load, while an Edit-mode tab stays the zero value
// (the frontend writes the field only when Source, keeping config.yaml lean).
func TestTabRef_ViewMode_RoundTrip(t *testing.T) {
	tmp := t.TempDir()
	original := Defaults()
	original.UI.OpenTabs = []TabRef{
		{Notebook: "Work", Section: "Projects", Page: "Site", ViewMode: "source"},
		{Notebook: "Work", Section: "", Page: "Top"}, // Edit (default) → omitted on disk
	}
	original.UI.ActiveTab = &TabRef{Notebook: "Work", Section: "Projects", Page: "Site", ViewMode: "source"}

	if err := Save(tmp, original); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := Load(tmp)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(loaded.UI.OpenTabs, original.UI.OpenTabs) {
		t.Errorf("open_tabs view_mode round-trip:\n got  %+v\n want %+v", loaded.UI.OpenTabs, original.UI.OpenTabs)
	}
	if loaded.UI.ActiveTab == nil || loaded.UI.ActiveTab.ViewMode != "source" {
		t.Errorf("active_tab view_mode round-trip: got %+v", loaded.UI.ActiveTab)
	}
}

// TestNormalize_TabRefViewModeSanitize confirms normalize collapses any
// non-"source" value (including hand-edited garbage) to "" (the Edit default),
// while "source" survives. This is the storage-side defense: the frontend
// also reads non-"source" as Edit, but normalize keeps config.yaml clean so a
// corrupted entry can't persist a bogus string.
func TestNormalize_TabRefViewModeSanitize(t *testing.T) {
	active := TabRef{Notebook: "A", Page: "p", ViewMode: "garbage"}
	cfg := normalize(SystemConfig{UI: UIConfig{
		OpenTabs: []TabRef{
			{Notebook: "A", Page: "p", ViewMode: "source"},
			{Notebook: "B", Page: "p", ViewMode: ""},
			{Notebook: "C", Page: "p", ViewMode: "edit"},
			{Notebook: "D", Page: "p", ViewMode: "garbage"},
		},
		ActiveTab: &active,
	}})
	got := cfg.UI.OpenTabs
	if got[0].ViewMode != "source" {
		t.Errorf("source should survive, got %q", got[0].ViewMode)
	}
	if got[1].ViewMode != "" {
		t.Errorf("empty should stay empty (Edit default), got %q", got[1].ViewMode)
	}
	if got[2].ViewMode != "" {
		t.Errorf("edit should collapse to empty, got %q", got[2].ViewMode)
	}
	if got[3].ViewMode != "" {
		t.Errorf("garbage should collapse to empty, got %q", got[3].ViewMode)
	}
	// The ActiveTab pointer is sanitized too — it persists view_mode, so a
	// hand-edited garbage value must not survive normalize.
	if cfg.UI.ActiveTab == nil || cfg.UI.ActiveTab.ViewMode != "" {
		t.Errorf("ActiveTab garbage should collapse to empty, got %+v", cfg.UI.ActiveTab)
	}

	// A source ActiveTab survives; a nil ActiveTab is left alone.
	src := TabRef{Notebook: "A", Page: "p", ViewMode: "source"}
	if got := normalize(SystemConfig{UI: UIConfig{ActiveTab: &src}}).UI.ActiveTab; got == nil || got.ViewMode != "source" {
		t.Errorf("ActiveTab source should survive, got %+v", got)
	}
	if got := normalize(SystemConfig{}).UI.ActiveTab; got != nil {
		t.Errorf("nil ActiveTab should stay nil, got %+v", got)
	}
}

// TestLoad_LegacyOpenTabsMissingViewMode verifies a config.yaml authored
// before #195 (open_tabs entries without view_mode) loads cleanly with each
// entry's ViewMode as the zero value — backward compat.
func TestLoad_LegacyOpenTabsMissingViewMode(t *testing.T) {
	tmp := t.TempDir()
	writeFile(t, ConfigPath(tmp), strings.Join([]string{
		"ui:",
		"  open_tabs:",
		"    - notebook: Work",
		"      section: Projects",
		"      page: Site",
	}, "\n"))
	cfg, err := Load(tmp)
	if err != nil {
		t.Fatalf("legacy config Load: %v", err)
	}
	if len(cfg.UI.OpenTabs) != 1 {
		t.Fatalf("expected 1 open_tabs entry, got %d", len(cfg.UI.OpenTabs))
	}
	if cfg.UI.OpenTabs[0].ViewMode != "" {
		t.Errorf("legacy open_tabs entry view_mode should default to empty (Edit), got %q", cfg.UI.OpenTabs[0].ViewMode)
	}
}

// --- Sprint 17: search / find-replace / writing aids config ---

// TestDefaults_SearchWritingAids confirms the Sprint 17 editor config fields
// and hotkeys ship with the documented defaults.
func TestDefaults_SearchWritingAids(t *testing.T) {
	d := Defaults()
	// Editor fields.
	if d.Editor.SpellcheckEnabled == nil || *d.Editor.SpellcheckEnabled != true {
		t.Errorf("defaults spellcheck_enabled should be *true, got %v", d.Editor.SpellcheckEnabled)
	}
	if d.Editor.SpellcheckLanguage == nil || *d.Editor.SpellcheckLanguage != "en-US" {
		t.Errorf("defaults spellcheck_language should be *\"en-US\", got %v", d.Editor.SpellcheckLanguage)
	}
	if d.Editor.TypewriterMode == nil || *d.Editor.TypewriterMode != false {
		t.Errorf("defaults typewriter_mode should be *false, got %v", d.Editor.TypewriterMode)
	}
	if d.Editor.TypewriterModeRatio == nil || *d.Editor.TypewriterModeRatio != 0.5 {
		t.Errorf("defaults typewriter_mode_ratio should be *0.5, got %v", d.Editor.TypewriterModeRatio)
	}
	if d.Editor.CustomDictionary == nil {
		t.Errorf("defaults custom_dictionary should be non-nil empty slice, got nil")
	}
	if len(d.Editor.CustomDictionary) != 0 {
		t.Errorf("defaults custom_dictionary should be empty, got %v", d.Editor.CustomDictionary)
	}
	if len(d.Editor.SpellcheckDomains) != 1 || d.Editor.SpellcheckDomains[0] != "software-terms" {
		t.Errorf("defaults spellcheck_domains should be [software-terms], got %v", d.Editor.SpellcheckDomains)
	}
	// Sprint 17 hotkeys.
	hkCases := map[string]string{
		"find_in_page":           "Ctrl+F",
		"replace":                "Ctrl+H",
		"global_replace":         "Ctrl+Shift+G",
		"toggle_typewriter_mode": "Ctrl+Shift+Y",
	}
	for key, want := range hkCases {
		if got, ok := d.Hotkeys[key]; !ok {
			t.Errorf("defaults hotkeys missing %q", key)
		} else if got != want {
			t.Errorf("defaults hotkey %q: got %q, want %q", key, got, want)
		}
	}
	// Spellcheck must NOT have a hotkey by design (wavy underline + right-click
	// + toolbar button). Pin this so a future change can't silently add one.
	if _, ok := d.Hotkeys["spellcheck_suggest"]; ok {
		t.Errorf("spellcheck must have no hotkey by design; found %q", d.Hotkeys["spellcheck_suggest"])
	}
}

// TestNormalize_SearchWritingAids confirms *bool/*string/*float64 normalization
// for the Sprint 17 fields: nil → defaults; explicit values survive.
func TestNormalize_SearchWritingAids(t *testing.T) {
	// nil → defaults.
	cfg := normalize(SystemConfig{})
	if cfg.Editor.SpellcheckEnabled == nil || *cfg.Editor.SpellcheckEnabled != true {
		t.Errorf("normalize spellcheck_enabled nil → *true, got %v", cfg.Editor.SpellcheckEnabled)
	}
	if cfg.Editor.SpellcheckLanguage == nil || *cfg.Editor.SpellcheckLanguage != "en-US" {
		t.Errorf("normalize spellcheck_language nil → *\"en-US\", got %v", cfg.Editor.SpellcheckLanguage)
	}
	if cfg.Editor.DateFormat == nil || *cfg.Editor.DateFormat != "YYYY-MM-DD" {
		t.Errorf("normalize date_format nil → *\"YYYY-MM-DD\", got %v", cfg.Editor.DateFormat)
	}
	if cfg.Editor.TypewriterMode == nil || *cfg.Editor.TypewriterMode != false {
		t.Errorf("normalize typewriter_mode nil → *false, got %v", cfg.Editor.TypewriterMode)
	}
	if cfg.Editor.TypewriterModeRatio == nil || *cfg.Editor.TypewriterModeRatio != 0.5 {
		t.Errorf("normalize typewriter_mode_ratio nil → *0.5, got %v", cfg.Editor.TypewriterModeRatio)
	}
	if cfg.Editor.CustomDictionary == nil || len(cfg.Editor.CustomDictionary) != 0 {
		t.Errorf("normalize custom_dictionary nil → empty non-nil slice, got %v", cfg.Editor.CustomDictionary)
	}
	if len(cfg.Editor.SpellcheckDomains) != 1 || cfg.Editor.SpellcheckDomains[0] != "software-terms" {
		t.Errorf("normalize spellcheck_domains nil → [software-terms], got %v", cfg.Editor.SpellcheckDomains)
	}

	// Explicit empty domains preserved (user turned all off).
	cfg = normalize(SystemConfig{Editor: EditorConfig{SpellcheckDomains: []string{}}})
	if cfg.Editor.SpellcheckDomains == nil || len(cfg.Editor.SpellcheckDomains) != 0 {
		t.Errorf("normalize empty spellcheck_domains should stay empty, got %v", cfg.Editor.SpellcheckDomains)
	}
	// Unknown domain IDs dropped; known kept sorted.
	cfg = normalize(SystemConfig{Editor: EditorConfig{SpellcheckDomains: []string{"python", "nope", "typescript", "python"}}})
	wantDomains := []string{"python", "typescript"}
	if !reflect.DeepEqual(cfg.Editor.SpellcheckDomains, wantDomains) {
		t.Errorf("spellcheck_domains normalize: got %v want %v", cfg.Editor.SpellcheckDomains, wantDomains)
	}

	// Explicit values survive.
	scOff := false
	cfg = normalize(SystemConfig{Editor: EditorConfig{SpellcheckEnabled: &scOff}})
	if cfg.Editor.SpellcheckEnabled == nil || *cfg.Editor.SpellcheckEnabled != false {
		t.Errorf("normalize should preserve explicit spellcheck false, got %v", cfg.Editor.SpellcheckEnabled)
	}
	twOn := true
	cfg = normalize(SystemConfig{Editor: EditorConfig{TypewriterMode: &twOn}})
	if cfg.Editor.TypewriterMode == nil || *cfg.Editor.TypewriterMode != true {
		t.Errorf("normalize should preserve explicit typewriter true, got %v", cfg.Editor.TypewriterMode)
	}
	lang := "en-GB"
	cfg = normalize(SystemConfig{Editor: EditorConfig{SpellcheckLanguage: &lang}})
	if cfg.Editor.SpellcheckLanguage == nil || *cfg.Editor.SpellcheckLanguage != "en-GB" {
		t.Errorf("normalize should preserve explicit language, got %v", cfg.Editor.SpellcheckLanguage)
	}

	// Empty/whitespace language collapses to default (defensive — a hand-edited
	// blank must not break spellcheck).
	empty := "   "
	cfg = normalize(SystemConfig{Editor: EditorConfig{SpellcheckLanguage: &empty}})
	if cfg.Editor.SpellcheckLanguage == nil || *cfg.Editor.SpellcheckLanguage != "en-US" {
		t.Errorf("normalize empty language → \"en-US\", got %v", cfg.Editor.SpellcheckLanguage)
	}
}

// TestNormalize_DateFormat confirms date_format normalizes to "YYYY-MM-DD"
// for nil/empty/unknown and preserves each of the 9 valid format IDs.
func TestNormalize_DateFormat(t *testing.T) {
	// Empty / whitespace → default.
	empty := "   "
	cfg := normalize(SystemConfig{Editor: EditorConfig{DateFormat: &empty}})
	if cfg.Editor.DateFormat == nil || *cfg.Editor.DateFormat != "YYYY-MM-DD" {
		t.Errorf("normalize empty date_format → \"YYYY-MM-DD\", got %v", cfg.Editor.DateFormat)
	}

	// Unknown value → default.
	garbage := "DD.MM.YYYY"
	cfg = normalize(SystemConfig{Editor: EditorConfig{DateFormat: &garbage}})
	if cfg.Editor.DateFormat == nil || *cfg.Editor.DateFormat != "YYYY-MM-DD" {
		t.Errorf("normalize unknown date_format → \"YYYY-MM-DD\", got %v", cfg.Editor.DateFormat)
	}

	// Each valid ID is preserved.
	for _, id := range []string{
		"YYYY-MM-DD", "DD-MMM-YY", "MM/DD/YYYY", "DD/MM/YYYY",
		"MMM D, YYYY", "long", "D MMM YYYY", "MM/DD/YY", "DD/MM/YY",
	} {
		v := id
		cfg = normalize(SystemConfig{Editor: EditorConfig{DateFormat: &v}})
		if cfg.Editor.DateFormat == nil || *cfg.Editor.DateFormat != id {
			t.Errorf("normalize should preserve date_format %q, got %v", id, cfg.Editor.DateFormat)
		}
	}
}

// TestNormalize_TypewriterRatioClamp confirms the ratio is clamped to [0.1, 0.9]
// so the active line stays meaningfully on-screen.
func TestNormalize_TypewriterRatioClamp(t *testing.T) {
	cases := []struct {
		in, want float64
	}{
		{0.0, 0.1},  // below floor → clamped up
		{0.05, 0.1}, // below floor
		{0.1, 0.1},  // floor boundary
		{0.3, 0.3},  // in-range passes through
		{0.5, 0.5},  // default
		{0.9, 0.9},  // ceiling boundary
		{0.95, 0.9}, // above ceiling → clamped down
		{1.0, 0.9},  // above ceiling
		{-0.5, 0.1}, // negative → floor
	}
	for _, c := range cases {
		r := c.in
		cfg := normalize(SystemConfig{Editor: EditorConfig{TypewriterModeRatio: &r}})
		got := *cfg.Editor.TypewriterModeRatio
		if got != c.want {
			t.Errorf("normalize ratio %v: got %v, want %v", c.in, got, c.want)
		}
	}
}

// TestNormalize_CustomDictionary confirms the custom word list is normalized to
// a non-nil, de-duplicated, trimmed, lowercased, sorted slice.
func TestNormalize_CustomDictionary(t *testing.T) {
	cfg := normalize(SystemConfig{Editor: EditorConfig{CustomDictionary: []string{
		"  TypeScript  ", // trimmed
		"typescript",     // dup (after trim+lowercase)
		"OAuth",          // lowercased
		"",               // empty dropped
		"   ",            // whitespace dropped
		"git",
		"API", // lowercased → "api"
	}}})
	want := []string{"api", "git", "oauth", "typescript"}
	if !reflect.DeepEqual(cfg.Editor.CustomDictionary, want) {
		t.Errorf("custom_dictionary normalize:\n got  %v\n want %v", cfg.Editor.CustomDictionary, want)
	}
}

// TestSearchWritingAids_RoundTrip confirms the Sprint 17 editor fields survive
// Save → Load with byte-for-byte fidelity.
func TestSearchWritingAids_RoundTrip(t *testing.T) {
	tmp := t.TempDir()
	original := Defaults()
	scOff := false
	twOn := true
	ratio := 0.35
	lang := "en-GB"
	original.Editor.SpellcheckEnabled = &scOff
	original.Editor.SpellcheckLanguage = &lang
	original.Editor.TypewriterMode = &twOn
	original.Editor.TypewriterModeRatio = &ratio
	original.Editor.CustomDictionary = []string{"typescript", "oauth", "git"}

	if err := Save(tmp, original); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := Load(tmp)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Editor.SpellcheckEnabled == nil || *loaded.Editor.SpellcheckEnabled != false {
		t.Errorf("spellcheck_enabled round-trip: got %v", loaded.Editor.SpellcheckEnabled)
	}
	if loaded.Editor.SpellcheckLanguage == nil || *loaded.Editor.SpellcheckLanguage != "en-GB" {
		t.Errorf("spellcheck_language round-trip: got %v", loaded.Editor.SpellcheckLanguage)
	}
	if loaded.Editor.TypewriterMode == nil || *loaded.Editor.TypewriterMode != true {
		t.Errorf("typewriter_mode round-trip: got %v", loaded.Editor.TypewriterMode)
	}
	if loaded.Editor.TypewriterModeRatio == nil || *loaded.Editor.TypewriterModeRatio != 0.35 {
		t.Errorf("typewriter_mode_ratio round-trip: got %v", loaded.Editor.TypewriterModeRatio)
	}
	want := []string{"git", "oauth", "typescript"} // sorted by normalize
	if !reflect.DeepEqual(loaded.Editor.CustomDictionary, want) {
		t.Errorf("custom_dictionary round-trip:\n got  %v\n want %v", loaded.Editor.CustomDictionary, want)
	}
}

// --- recent_tags config ---

// TestDefaults_RecentTags confirms RecentTags defaults to an empty non-nil slice.
func TestDefaults_RecentTags(t *testing.T) {
	d := Defaults()
	if d.UI.RecentTags == nil {
		t.Errorf("defaults recent_tags should be non-nil empty slice, got nil")
	}
	if len(d.UI.RecentTags) != 0 {
		t.Errorf("defaults recent_tags should be empty, got %v", d.UI.RecentTags)
	}
}

// TestNormalize_RecentTags confirms case-insensitive dedup, cap at
// MaxRecentTags, empty/whitespace dropping, and order preservation.
func TestNormalize_RecentTags(t *testing.T) {
	t.Run("nil becomes empty", func(t *testing.T) {
		cfg := normalize(SystemConfig{})
		if cfg.UI.RecentTags == nil || len(cfg.UI.RecentTags) != 0 {
			t.Errorf("nil → empty non-nil slice, got %v", cfg.UI.RecentTags)
		}
	})
	t.Run("case-insensitive dedup keeps first casing", func(t *testing.T) {
		cfg := normalize(SystemConfig{UI: UIConfig{RecentTags: []string{
			"Work/Project", "work/project", "work/PROJECT",
		}}})
		if len(cfg.UI.RecentTags) != 1 || cfg.UI.RecentTags[0] != "Work/Project" {
			t.Errorf("case-insensitive dedup: got %v, want [Work/Project]", cfg.UI.RecentTags)
		}
	})
	t.Run("empty and whitespace dropped", func(t *testing.T) {
		cfg := normalize(SystemConfig{UI: UIConfig{RecentTags: []string{
			"", "  ", "tag1", "  ", "tag2",
		}}})
		if !reflect.DeepEqual(cfg.UI.RecentTags, []string{"tag1", "tag2"}) {
			t.Errorf("empty/whitespace filter: got %v", cfg.UI.RecentTags)
		}
	})
	t.Run("capped at MaxRecentTags", func(t *testing.T) {
		tags := make([]string, MaxRecentTags+5)
		for i := range tags {
			tags[i] = fmt.Sprintf("tag-%02d", i)
		}
		cfg := normalize(SystemConfig{UI: UIConfig{RecentTags: tags}})
		if len(cfg.UI.RecentTags) != MaxRecentTags {
			t.Errorf("cap: got %d, want %d", len(cfg.UI.RecentTags), MaxRecentTags)
		}
		if cfg.UI.RecentTags[0] != "tag-00" {
			t.Errorf("first should be tag-00, got %q", cfg.UI.RecentTags[0])
		}
	})
	t.Run("order preserved", func(t *testing.T) {
		cfg := normalize(SystemConfig{UI: UIConfig{RecentTags: []string{
			"beta", "alpha", "gamma",
		}}})
		if !reflect.DeepEqual(cfg.UI.RecentTags, []string{"beta", "alpha", "gamma"}) {
			t.Errorf("order: got %v", cfg.UI.RecentTags)
		}
	})
}

// TestRecentTags_RoundTrip confirms recent_tags survive Save → Load.
func TestRecentTags_RoundTrip(t *testing.T) {
	tmp := t.TempDir()
	original := Defaults()
	original.UI.RecentTags = []string{"work/project", "personal/journal", "IDEAS"}

	if err := Save(tmp, original); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := Load(tmp)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(loaded.UI.RecentTags, original.UI.RecentTags) {
		t.Errorf("recent_tags round-trip:\n got  %v\n want %v", loaded.UI.RecentTags, original.UI.RecentTags)
	}
}

// TestLoad_LegacyConfigMissingRecentTags verifies backward compat: a config
// authored before recent_tags was added (no ui.recent_tags key) loads cleanly with the field
// defaulting to an empty slice.
func TestLoad_LegacyConfigMissingRecentTags(t *testing.T) {
	tmp := t.TempDir()
	writeFile(t, ConfigPath(tmp), "ui:\n  sidebar_width: 280\n")
	cfg, err := Load(tmp)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.UI.RecentTags == nil || len(cfg.UI.RecentTags) != 0 {
		t.Errorf("legacy recent_tags should default to empty non-nil slice, got %v", cfg.UI.RecentTags)
	}
}

func TestIsValidTagPath(t *testing.T) {
	valid := []string{
		"a",
		"A",
		"work/project",
		"work/project/milestone-one",
		"a_b_c",
		"X1",
		"work/project/milestone_one/v2-0",
		strings.Repeat("a", MaxTagPathBytes),
	}
	for _, tag := range valid {
		if !IsValidTagPath(tag) {
			t.Errorf("IsValidTagPath(%q): expected true", tag)
		}
	}

	invalid := []string{
		"",
		"123start",
		"/starts-slash",
		"-starts-hyphen",
		"_starts-under",
		"has space",
		"has\ttab",
		"has\nnewline",
		"has!bang",
		"has.dot",
		"has:colon",
		"a\x00null",
		"a\x1Besc",
		strings.Repeat("a", MaxTagPathBytes+1),
	}
	for _, tag := range invalid {
		if IsValidTagPath(tag) {
			t.Errorf("IsValidTagPath(%q): expected false", tag)
		}
	}
}

func TestNormalizeRecentTags_FiltersInvalidEntries(t *testing.T) {
	cfg := normalize(SystemConfig{UI: UIConfig{RecentTags: []string{
		"work/project",  // valid
		"has space",     // invalid: space
		"123bad",        // invalid: starts with digit
		"good/tag",      // valid
		"evil\nnewline", // invalid: newline
		"",              // empty
		"   ",           // whitespace
	}}})
	want := []string{"work/project", "good/tag"}
	if !reflect.DeepEqual(cfg.UI.RecentTags, want) {
		t.Errorf("filtered recent_tags:\n got  %v\n want %v", cfg.UI.RecentTags, want)
	}
}

func TestNormalize_NoteZoom(t *testing.T) {
	cfg := normalize(SystemConfig{})
	if cfg.UI.NoteZoom == nil || *cfg.UI.NoteZoom != 1.0 {
		t.Fatalf("nil note_zoom → 1.0, got %v", cfg.UI.NoteZoom)
	}
	hi := 9.0
	cfg = normalize(SystemConfig{UI: UIConfig{NoteZoom: &hi}})
	if cfg.UI.NoteZoom == nil || *cfg.UI.NoteZoom != 2.0 {
		t.Fatalf("clamp high → 2.0, got %v", cfg.UI.NoteZoom)
	}
	lo := 0.1
	cfg = normalize(SystemConfig{UI: UIConfig{NoteZoom: &lo}})
	if cfg.UI.NoteZoom == nil || *cfg.UI.NoteZoom != 0.7 {
		t.Fatalf("clamp low → 0.7, got %v", cfg.UI.NoteZoom)
	}
	mid := 1.24
	cfg = normalize(SystemConfig{UI: UIConfig{NoteZoom: &mid}})
	if cfg.UI.NoteZoom == nil || *cfg.UI.NoteZoom != 1.2 {
		t.Fatalf("snap 1.24 → 1.2, got %v", cfg.UI.NoteZoom)
	}
}

// TestDashboards_RoundTrip guards the frontend-owned dashboard config blob
// (e.g. the typed-notes dashboard's saved views at
// ui.dashboards.typed_notes.saved_views). Go carries it as an opaque
// map[string]any; if the UIConfig field were removed, the nested YAML keys
// would be silently dropped on round-trip and the dashboard's saved-view
// persistence would become a no-op.
func TestDashboards_RoundTrip(t *testing.T) {
	src := SystemConfig{UI: UIConfig{Dashboards: map[string]any{
		"typed_notes": map[string]any{
			"saved_views": []any{
				map[string]any{"id": "abc", "name": "Active Projects"},
			},
		},
	}}}
	out, err := yaml.Marshal(src)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back SystemConfig
	if err := yaml.Unmarshal(out, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	tn, _ := back.UI.Dashboards["typed_notes"].(map[string]any)
	if tn == nil {
		t.Fatalf("typed_notes blob dropped on round-trip; Dashboards=%v", back.UI.Dashboards)
	}
	views, _ := tn["saved_views"].([]any)
	if len(views) != 1 {
		t.Fatalf("saved_views did not round-trip; got %v", views)
	}
	first, _ := views[0].(map[string]any)
	if first["name"] != "Active Projects" {
		t.Fatalf("saved_view name lost; got %v", first)
	}
}
