package config

import (
	"silt/backend/ai"
	"silt/backend/spellcheck"
)

// Defaults returns a fully-populated SystemConfig matching the config.yaml
// scaffolded by vault.ScaffoldVault, so a missing/empty field is never a
// nil-deref and "first run" behaves like a fresh scaffold.
func Defaults() SystemConfig {
	return SystemConfig{
		Notebooks: NotebooksConfig{
			DefaultActive: "Work",
		},
		Editor: EditorConfig{
			FontFamily:              "Plus Jakarta Sans",
			MonoFontFamily:          "JetBrains Mono",
			FontSizePx:              14,
			LineHeight:              1.6,
			TabIndentSpaces:         4,
			AutoSaveDelayMs:         500,
			FocusHighlightAncestors: true,
			ShowWordCount:           boolPtr(false),
			FocusMode:               boolPtr(false),
			DefaultViewMode:         stringPtr("edit"),
			SpellcheckEnabled:       boolPtr(true),
			SpellcheckLanguage:      stringPtr("en-US"),
			DateFormat:              stringPtr("YYYY-MM-DD"),
			TypewriterMode:          boolPtr(false),
			TypewriterModeRatio:     float64Ptr(0.5),
			CustomDictionary:        []string{},
			SpellcheckDomains:       spellcheck.DefaultDomainIDs(),
		},
		Parsing: ParsingConfig{
			AutoInjectUUID:      true,
			DefaultTaskPriority: 3,
		},
		Hotkeys: map[string]string{
			// Default chords are anchored to widely-established editor/OS
			// conventions so muscle memory transfers and the native menu and
			// config-driven webview layers don't both fire on the same key.
			// Tab-chord actions (close/next/prev tab) avoid Ctrl+W and
			// Ctrl+Tab because WebView2 unreliably relays those to the
			// webview; sidebar toggle moves to Ctrl+\ so Ctrl+B is bold
			// everywhere. Windows/Linux only.
			// open_search: cross-vault global search → Ctrl+Shift+F (the
			// cross-file "find in files" convention; single-document editors
			// have no cross-file search). Frees Ctrl+P for future Print.
			"open_search": "Ctrl+Shift+F",
			// toggle_sidebar → Ctrl+\ so Ctrl+B is unambiguously bold. A
			// common outliner convention for panel toggle.
			"toggle_sidebar": "Ctrl+\\",
			"focus_sidebar":  "Ctrl+Shift+B",
			// cycle_view_layout → Ctrl+Alt+V. Alt+Tab is the OS window-switcher
			// on Windows/Linux (captured before the app sees it) and never fired.
			"cycle_view_layout": "Ctrl+Alt+V",
			// open_settings → Ctrl+, (the universal settings convention.
			// Opens settings as a workspace tab. This freed Ctrl+, from
			// format_subscript, which moved to Ctrl+Shift+, below.
			"open_settings":        "Ctrl+,",
			"new_page":             "Ctrl+N",
			"new_section":          "Ctrl+Alt+N",
			"new_notebook":         "Ctrl+Alt+Shift+N",
			"open_quick_switcher":  "Ctrl+P",
			"open_shortcuts_help":  "Shift+?",
			"indent_block":         "Tab",
			"unindent_block":       "Shift+Tab",
			"open_template_picker": "Ctrl+Shift+T",
			// Global standalone-task quick-add. Opens an app-level overlay
			// (not a plugin action) that creates a task in
			// <vault>/.silt/tasks.md. "N" for New; Shift+T was taken by the
			// template picker.
			"new_task": "Ctrl+Shift+N",
			// Hub-scoped Tasks command palette. Same chord as format_link
			// (Ctrl+K); conflict is resolved by focus scope — the hub
			// handler only fires when focus is not an input / textarea /
			// contenteditable / ProseMirror, so the editor keeps format_link
			// while typing.
			"tasks_command_palette": "Ctrl+K",
			// Tab strip hotkeys. Remapped off Ctrl+W / Ctrl+Tab because
			// WebView2 unreliably relays those chords to the webview; the
			// Ctrl+Alt+Arrow forms are stable. Each may be remapped or
			// disabled (set to "") from Settings → General.
			"next_tab":  "Ctrl+Alt+Right",
			"prev_tab":  "Ctrl+Alt+Left",
			"close_tab": "Ctrl+Shift+W",
			// Inline formatting hotkeys. Standard editor bindings so muscle
			// memory transfers. Each is overridable per-vault via the
			// deep-merge. The editor's ProseMirror keymaps consume these
			// inside the contenteditable; the global handler skips them when
			// the editor is focused. Ctrl+B is bold unconditionally now that
			// toggle_sidebar moved off it.
			"format_bold":        "Ctrl+B",
			"format_italic":      "Ctrl+I",
			"format_underline":   "Ctrl+U",
			"format_strike":      "Alt+Shift+5",
			"format_code":        "Ctrl+E",
			"format_link":        "Ctrl+K",
			"format_highlight":   "Ctrl+Shift+H",
			"format_subscript":   "Ctrl+Shift,",
			"format_superscript": "Ctrl+.",
			// Heading level hotkeys. H4–H6 use Alt+5..7 because Ctrl+Alt+4
			// is reserved for set_task.
			"set_h1":   "Ctrl+Alt+1",
			"set_h2":   "Ctrl+Alt+2",
			"set_h3":   "Ctrl+Alt+3",
			"set_h4":   "Ctrl+Alt+5",
			"set_h5":   "Ctrl+Alt+6",
			"set_h6":   "Ctrl+Alt+7",
			"set_note": "Ctrl+Alt+0",
			"set_task": "Ctrl+Alt+4",
			// Text alignment hotkeys. Standard alignment bindings.
			"align_left":    "Ctrl+Shift+L",
			"align_center":  "Ctrl+Shift+E",
			"align_right":   "Ctrl+Shift+R",
			"align_justify": "Ctrl+Shift+J",
			// Blockquote toggle. Standard blockquote binding.
			"toggle_quote": "Ctrl+Shift+9",
			// Foldable details toggle. Ctrl+Shift+. (Ctrl+. is taken by the
			// Superscript mark).
			"toggle_details": "Ctrl+Shift+.",
			// Table row/column insert hotkeys. Standard row/column-insert
			// bindings; deletion + merge are toolbar-only in v1.
			"table_insert_row_above": "Ctrl+Shift+Up",
			"table_insert_row_below": "Ctrl+Shift+Down",
			"table_insert_col_left":  "Ctrl+Shift+Left",
			"table_insert_col_right": "Ctrl+Shift+Right",
			// View mode toggle. Moved off Ctrl+Shift+V (the OS
			// paste-without-formatting convention, and the TasksHub
			// display-mode cycle) to Ctrl+Alt+R so the three no longer
			// collide.
			"toggle_view_mode": "Ctrl+Alt+R",
			// Formatting toolbar toggle and focus mode toggle.
			// toggle_format_toolbar → Ctrl+F1 (the toggle-ribbon convention);
			// frees Ctrl+Shift+F for global search (open_search above).
			"toggle_format_toolbar": "Ctrl+F1",
			"toggle_focus_mode":     "Ctrl+Shift+D",
			// Sprint 17 — Search, Find/Replace & Writing Aids.
			// find_in_page (Ctrl+F) and replace (Ctrl+H) are the universal
			// in-editor find/replace bindings. global_replace
			// (Ctrl+Shift+G) escalates replace to a cross-vault "replace in
			// files"). toggle_typewriter_mode (Ctrl+Shift+Y) pairs with
			// toggle_focus_mode (Ctrl+Shift+D) as the writing-mode toggles.
			// Spellcheck deliberately has NO hotkey — it's wavy-underline +
			// right-click + a FormatToolbar button (see SPECS.md).
			"find_in_page":           "Ctrl+F",
			"replace":                "Ctrl+H",
			"global_replace":         "Ctrl+Shift+G",
			"toggle_typewriter_mode": "Ctrl+Shift+Y",
			// Date Glance popover (#730). Opens from any view; Ctrl+Alt+D
			// (D for Date) avoids collisions — Ctrl+Shift+D is focus mode,
			// and no editor-owned set_/format_ chord uses this combo.
			"open_date_glance": "Ctrl+Alt+D",
		},
		Plugins: PluginsConfig{
			// silt-tasks is the unified task surface (Phase 9 / #431),
			// succeeding the retired standalone silt-calendar / silt-kanban.
			Active: []string{"silt-tasks"},
			// First-party AI modules are gated by ai.features (#632), not
			// plugins.disabled. Disabled stays empty for fresh vaults.
			Disabled: []string{},
			PluginSettings: map[string]any{
				// silt-tasks is the unified hub (Phase 9 / #431). Every key
				// the frontend loaders read (settings.ts) is seeded so a
				// fresh vault — or a migrated one — never nil-derefs.
				// saved_views starts empty: SYSTEM_VIEWS are code-derived
				// on every load (savedViews.ts invariant), and persisted
				// user views come only from explicit save action. Slice
				// values use []any (not []string) so they survive a YAML
				// round-trip — yaml.v3 loads sequences as []any, so a
				// []string seed would mismatch on Load.
				"silt-tasks": map[string]any{
					"default_display_mode": "list",
					"default_group_by":     "dueDate",
					"default_sort":         "dueDate",
					"default_scope":        "vault",
					"calendar_sub_mode":    "month",
					"columns":              []any{"TODO", "DOING", "DONE"},
					"filters": map[string]any{
						"owners":     []any{},
						"priorities": []any{},
						"dueDate":    "",
						"tags":       []any{},
					},
					"saved_views":  []any{},
					"local_author": "",
				},
			},
		},
		UI: UIConfig{
			SidebarWidth: 256,
			NavOrder: NavOrder{
				Sections: map[string][]string{},
				Pages:    map[string][]string{},
			},
			OpenTabs:         []TabRef{},
			ExpandedSections: []NavigationSectionRef{},
			RecentPages:      []RecentPage{},
			Favorites:        []NavigationPageRef{},
			// EnablePreviewTabs defaults to true (industry-standard parity). Stored as
			// a *bool so "unset" is distinguishable from "explicitly false";
			// the frontend treats nil as true.
			EnablePreviewTabs: boolPtr(true),
			// MaxOpenTabs caps the simultaneously-mounted editor count
			// (#142 §3). 8 is the documented default; on overflow the
			// frontend LRU-evicts least-recently-active preview tabs first,
			// then oldest pinned. 0 (legacy config without the key) is
			// normalized to 8 in normalize().
			MaxOpenTabs: 8,
			// ShowFormatToolbar defaults to true (#168). Stored as *bool so
			// "unset" is distinguishable from "explicitly false"; the frontend
			// treats nil as true.
			ShowFormatToolbar: boolPtr(true),
			// ShowTabDirtyIndicators defaults to true (#167). Same *bool
			// semantics as EnablePreviewTabs: "unset" stays distinguishable
			// from "explicitly false" through the Load → normalize path.
			ShowTabDirtyIndicators: boolPtr(true),
			DismissedTips:          []string{},
			// SidebarView is intentionally NOT seeded here: normalize owns its
			// default ("tree") and the one-shot migration from the legacy
			// quick_access_collapsed key (see normalize.go + Load). Seeding it
			// would mask the legacy-key presence detection in Load.
			OpenDevtoolsOnStartup: boolPtr(false),
			Formatting: FormattingConfig{
				TypographyEnabled: boolPtr(true),
				ColorEnabled:      boolPtr(true),
				MathEnabled:       boolPtr(true),
			},
			RecentTags: []string{},
		},
		// AI providers ship unconfigured (Sprint 20): no chat model, no
		// embedding model, no endpoint. Features ship OFF (#632) so a fresh
		// vault never phones a model until the user enables AI. UseKeyring
		// defaults true so the first key lands in the OS keyring (#218).
		AI: AIConfig{
			Chat:       AIProviderConfig{ProviderType: ai.ProviderLocal, BaseURL: DefaultAIBaseURL},
			Embedding:  AIProviderConfig{ProviderType: ai.ProviderLocal, BaseURL: DefaultAIBaseURL},
			Features:   AIFeaturesConfig{}, // all false
			UseKeyring: boolPtr(true),
		},
	}
}

// boolPtr is a small helper for the Defaults() block so *bool fields can be
// initialized inline without a temporary variable.
func boolPtr(b bool) *bool { return &b }

// stringPtr is a small helper for the Defaults() block so *string fields can be
// initialized inline without a temporary variable.
func stringPtr(s string) *string { return &s }

// float64Ptr is a small helper for the Defaults() block so *float64 fields can
// be initialized inline without a temporary variable.
func float64Ptr(f float64) *float64 { return &f }

// intPtr is a small helper so *int fields (AI provider advanced knobs) can be
// initialized inline without a temporary variable.
func intPtr(i int) *int { return &i }
