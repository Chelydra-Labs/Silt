// Package config parses and persists Silt's system configuration
// (<vault>/.system/config.yaml). It is the single source of truth for all
// non-vault-path application settings: editor defaults, parsing rules,
// hotkeys, and the plugin registry. The vault path itself still lives in the
// OS-config settings.json (it must be known before any vault can be opened).
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
	"silt/backend/safeio"
)

// maxConfigYAMLBytes bounds a vault/linked config.yaml before it is parsed.
// A hostile synced config cannot drive unbounded allocation ahead of
// yaml.Unmarshal (audit F12).
const maxConfigYAMLBytes int64 = 256 << 10 // 256 KB

// SystemConfig is the parsed contents of <vault>/.system/config.yaml. It
// mirrors the schema documented in SPECS.md §9.1.
type SystemConfig struct {
	Notebooks NotebooksConfig   `yaml:"notebooks" json:"notebooks"`
	Editor    EditorConfig      `yaml:"editor" json:"editor"`
	Parsing   ParsingConfig     `yaml:"parsing" json:"parsing"`
	Hotkeys   map[string]string `yaml:"hotkeys" json:"hotkeys"`
	Plugins   PluginsConfig     `yaml:"plugins" json:"plugins"`
	UI        UIConfig          `yaml:"ui" json:"ui"`
	// AI is the shared provider config consumed by every AI plugin via the
	// ctx.ai SDK (Sprint 20). Cross-cutting (not plugin-scoped), so it lives
	// at the top level alongside UI. API keys are never serialized to JS —
	// see AIProviderConfig.APIKey's `json:"-"` tag.
	AI              AIConfig         `yaml:"ai,omitempty" json:"ai"`
	LinkedNotebooks []LinkedNotebook `yaml:"linked_notebooks,omitempty" json:"linked_notebooks,omitempty"`
}

// NotebooksConfig holds spatial-mapping defaults.
type NotebooksConfig struct {
	Path          string `yaml:"path" json:"path"`
	DefaultActive string `yaml:"default_active" json:"default_active"`
}

// EditorConfig holds editor rendering and behaviour defaults.
type EditorConfig struct {
	FontFamily              string  `yaml:"font_family" json:"font_family"`
	MonoFontFamily          string  `yaml:"mono_font_family" json:"mono_font_family"`
	FontSizePx              int     `yaml:"font_size_px" json:"font_size_px"`
	LineHeight              float64 `yaml:"line_height" json:"line_height"`
	TabIndentSpaces         int     `yaml:"tab_indent_spaces" json:"tab_indent_spaces"`
	AutoSaveDelayMs         int     `yaml:"auto_save_delay_ms" json:"auto_save_delay_ms"`
	FocusHighlightAncestors bool    `yaml:"focus_highlight_ancestors" json:"focus_highlight_ancestors"`
	// ShowWordCount controls the subtle word/char count in the editor status
	// area (#168 Phase 3). Default false — opt-in so we add no chrome by default.
	ShowWordCount *bool `yaml:"show_word_count,omitempty" json:"show_word_count,omitempty"`
	// FocusMode dims all paragraphs except the active one for distraction-free
	// writing (#168 Phase 3). Default false.
	FocusMode *bool `yaml:"focus_mode,omitempty" json:"focus_mode,omitempty"`
	// DefaultViewMode controls whether pages open in "edit" (TipTap WYSIWYG)
	// or "source" (raw markdown) mode (#171). Default "edit".
	DefaultViewMode *string `yaml:"default_view_mode,omitempty" json:"default_view_mode,omitempty"`
	// SpellcheckEnabled gates the inline typo-js spellcheck layer (#196).
	// Default true — matches every competing note app; markdown purists can
	// disable from Settings → Editor. Stored as *bool so "unset" stays
	// distinguishable from "explicitly false" through the Load → normalize
	// path. Spellcheck is a pure view-layer decoration; the on-disk file is
	// never modified by it.
	SpellcheckEnabled *bool `yaml:"spellcheck_enabled,omitempty" json:"spellcheck_enabled,omitempty"`
	// SpellcheckLanguage selects the Hunspell dictionary (#196). v1 ships
	// en-US bundled; the value must name a dictionary present under
	// frontend/public/dictionaries/<lang>/. Default "en-US".
	SpellcheckLanguage *string `yaml:"spellcheck_language,omitempty" json:"spellcheck_language,omitempty"`
	// TypewriterMode keeps the active line at a fixed vertical ratio of the
	// editor viewport (#187). Default false (opt-in) — it pairs naturally
	// with FocusMode but is independently togglable. Pure scroll-presentation;
	// zero content/schema/on-disk impact.
	TypewriterMode *bool `yaml:"typewriter_mode,omitempty" json:"typewriter_mode,omitempty"`
	// TypewriterModeRatio is the viewport fraction (0–1) at which the active
	// line is held when TypewriterMode is on (#187). Default 0.5 (center,
	// matching iA Writer). normalize() clamps to [0.1, 0.9].
	TypewriterModeRatio *float64 `yaml:"typewriter_mode_ratio,omitempty" json:"typewriter_mode_ratio,omitempty"`
	// CustomDictionary is the per-vault list of user-added spellcheck words
	// (#196). Lives in the YAML tier (ARCHITECTURE §0 rule 2 — per-vault UI
	// prefs), NOT a separate file and NOT SQLite (it is user intent).
	// normalize() guarantees non-nil, de-duplicated, trimmed, lowercased,
	// sorted. A linked notebook may carry its own co-located override
	// (arrays replace, §3.1) so an external notebook travels with its words.
	CustomDictionary []string `yaml:"custom_dictionary,omitempty" json:"custom_dictionary,omitempty"`
	// SpellcheckDomains is the set of enabled technical word-list pack IDs
	// (#337). Each ID names a catalog entry (e.g. "software-terms",
	// "typescript"). Packs merge into the spellcheck Set layer on top of
	// Hunspell. Default ["software-terms"]. normalize() guarantees non-nil,
	// de-duplicated, sorted, unknown IDs dropped.
	SpellcheckDomains []string `yaml:"spellcheck_domains,omitempty" json:"spellcheck_domains,omitempty"`
}

// ParsingConfig holds the task-parse rules. The task regexes themselves
// (TaskCheckboxRegex / TaskTokenRegex) are fixed package-level constants in
// the parser and are intentionally NOT user-editable: a user-supplied regex
// on a synced vault is a catastrophic-backtracking DoS vector against the
// indexer (audit F11). Only non-regex parse knobs live here.
type ParsingConfig struct {
	AutoInjectUUID      bool `yaml:"auto_inject_uuid" json:"auto_inject_uuid"`
	DefaultTaskPriority int  `yaml:"default_task_priority" json:"default_task_priority"`
}

// PluginsConfig mirrors the `plugins:` block of config.yaml. PluginSettings is
// an opaque per-plugin map (the plugin manager surfaces it read-only).
//
// NOTE: capability Grants lived here pre-F4 but have moved to per-host storage
// (see backend/vault/grants.go). A legacy config.yaml may still carry a
// `grants:` block under `plugins:` — it is silently ignored on load (yaml.v3
// drops unknown fields) and migrated to the host store on first launch
// (initializeVaultServices → ConfirmGrantsMigration). The field is gone from
// the struct so a synced vault can never re-introduce grants via config.yaml.
type PluginsConfig struct {
	Active         []string       `yaml:"active" json:"active"`
	Disabled       []string       `yaml:"disabled" json:"disabled"`
	PluginSettings map[string]any `yaml:"plugin_settings" json:"plugin_settings"`
}

// LinkedNotebook is an external notebook root registered into the vault but
// living outside it (e.g. a synced SharePoint/OneDrive folder). It is edited
// IN PLACE — never copied into the vault — so its existing source of truth and
// sync/conflict semantics are preserved (#100). The link registry
// (config.yaml `linked_notebooks:`) is vault-scoped state alongside the active
// plugin list; the markdown content (and any co-located <root>/.system/) stays
// with the notebook root and is the product.
type LinkedNotebook struct {
	ID              string `yaml:"id" json:"id"`                                                 // stable id, e.g. "linked-<short>"; source column = "linked:"+ID
	RootPath        string `yaml:"root_path" json:"root_path"`                                   // absolute path to the external notebook root
	DisplayName     string `yaml:"display_name" json:"display_name"`                             // sidebar label (the notebook "name")
	RootFingerprint string `yaml:"root_fingerprint,omitempty" json:"root_fingerprint,omitempty"` // F3: host-verified trust anchor; see fingerprint.go
}

// Source returns the `blocks.source` discriminator value for this linked
// notebook ('linked:<id>'), matching what the indexer writes.
func (l LinkedNotebook) Source() string { return "linked:" + l.ID }

// LinkedNotebooksSource is the `blocks.source` value for in-vault notebooks.
const LinkedNotebooksVaultSource = "vault"

// UIConfig holds per-vault UI preferences (sidebar width, custom navigation
// ordering, the open-tab set). Stored in the YAML tier (per-vault) per
// ARCHITECTURE §0 rule #2.
type UIConfig struct {
	SidebarWidth      int                    `yaml:"sidebar_width" json:"sidebar_width"`
	NavOrder          NavOrder               `yaml:"nav_order,omitempty" json:"nav_order,omitempty"`
	OpenTabs          []TabRef               `yaml:"open_tabs,omitempty" json:"open_tabs,omitempty"`
	ActiveTab         *TabRef                `yaml:"active_tab,omitempty" json:"active_tab,omitempty"`
	ExpandedSections  []NavigationSectionRef `yaml:"expanded_sections,omitempty" json:"expanded_sections,omitempty"`
	RecentPages       []RecentPage           `yaml:"recent_pages,omitempty" json:"recent_pages,omitempty"`
	Favorites         []NavigationPageRef    `yaml:"favorites,omitempty" json:"favorites,omitempty"`
	EnablePreviewTabs *bool                  `yaml:"enable_preview_tabs,omitempty" json:"enable_preview_tabs,omitempty"`
	MaxOpenTabs       int                    `yaml:"max_open_tabs,omitempty" json:"max_open_tabs,omitempty"`
	// ShowFormatToolbar controls the persistent format toolbar visibility
	// (#168). Default true; users who want outliner-minimal density can hide
	// it from Settings. The bubble, slash commands, hotkeys, and hover menu
	// remain functional when hidden.
	ShowFormatToolbar *bool `yaml:"show_format_toolbar,omitempty" json:"show_format_toolbar,omitempty"`
	// ShowTabDirtyIndicators controls the per-tab dirty/save-failed glyph on
	// the tab header (#167). Default true; users who find the visual churn
	// noisy (Silt auto-saves on a 500ms debounce, so most dirty state is
	// sub-second) can hide the tab glyph. The in-editor save-state indicator
	// is unaffected — it remains the authoritative surface.
	ShowTabDirtyIndicators *bool `yaml:"show_tab_dirty_indicators,omitempty" json:"show_tab_dirty_indicators,omitempty"`
	// DismissedTips tracks one-time UI tips the user has dismissed (per-vault).
	// Used by the formatting first-run tip (#168). Same persistence tier as
	// sidebar_width.
	DismissedTips []string `yaml:"dismissed_tips,omitempty" json:"dismissed_tips,omitempty"`
	// OpenDevtoolsOnStartup opens the Chromium DevTools inspector on app launch.
	// Default false. Intended for diagnostics on non-developer machines.
	OpenDevtoolsOnStartup *bool `yaml:"open_devtools_on_startup,omitempty" json:"open_devtools_on_startup,omitempty"`
	// Formatting holds inline-formatting-related UI toggles (#168 Phase 3, #170).
	Formatting FormattingConfig `yaml:"formatting,omitempty" json:"formatting,omitempty"`
}

// NavigationSectionRef is the canonical identity of a section in a vault.
// Path is relative to Notebook and uses forward slashes at the IPC boundary.
type NavigationSectionRef struct {
	Notebook string `yaml:"notebook" json:"notebook"`
	Path     string `yaml:"path" json:"path"`
}

// NavigationPageRef is the canonical identity of a page. Section is empty for
// a page directly under the notebook root.
type NavigationPageRef struct {
	Notebook string `yaml:"notebook" json:"notebook"`
	Section  string `yaml:"section" json:"section"`
	Page     string `yaml:"page" json:"page"`
}

// RecentPage records a successfully opened or saved page. Unix seconds keep
// the YAML compact and make the value stable across platforms.
type RecentPage struct {
	NavigationPageRef `yaml:"inline" json:",inline"`
	OpenedAt          int64 `yaml:"opened_at" json:"opened_at"`
}

// FormattingConfig holds per-vault toggles for inline formatting features.
type FormattingConfig struct {
	// TypographyEnabled controls smart input replacements (-- → —, (c) → ©,
	// straight → curly quotes). Default true; markdown purists can disable (#168).
	TypographyEnabled *bool `yaml:"typography_enabled,omitempty" json:"typography_enabled,omitempty"`
	// ColorEnabled controls the text/background color pickers (#170). Default
	// true; markdown purists can disable to keep files 100% portable. The marks
	// still parse from incoming files when disabled; only the editor's setColor
	// calls become no-ops.
	ColorEnabled *bool `yaml:"color_enabled,omitempty" json:"color_enabled,omitempty"`
	// MathEnabled controls the LaTeX math features (#191): the /math slash
	// command and KaTeX rendering of $…$ / $$…$$. Default true. Existing math
	// in files still round-trips when disabled; the toggle removes the in-editor
	// insertion affordance.
	MathEnabled *bool `yaml:"math_enabled,omitempty" json:"math_enabled,omitempty"`
}

// TabRef is a persisted reference to an open tab's page (#142). It is the
// YAML-serializable form of a frontend TabEntry. The locator triple is always
// persisted; ViewMode records a tab stuck in Source view (#195 — absence means
// the default, Edit). Preview flag, scroll/cursor state, and the like are
// ephemeral (industry-standard parity: preview tabs are not restored across
// restarts). The frontend filters to pinned tabs before calling SetOpenTabs.
type TabRef struct {
	Notebook string `yaml:"notebook" json:"notebook"`
	Section  string `yaml:"section" json:"section"`
	Page     string `yaml:"page" json:"page"`
	// ViewMode is the per-tab Edit/Source override (#195). Only "source" is
	// meaningfully persisted — "" / "edit" both mean the Edit default, so the
	// frontend writes the field only when the tab is in Source view (keeping
	// config.yaml lean). normalize() sanitizes any other value to "".
	ViewMode string `yaml:"view_mode,omitempty" json:"view_mode,omitempty"`
}

// NavOrder stores explicit ordering for the sidebar navigator tree. Folders on
// disk have no inherent custom order; this map overrides the default
// alphabetical sort. Keys not present in the map fall back to alphabetical.
type NavOrder struct {
	Notebooks []string            `yaml:"notebooks,omitempty" json:"notebooks,omitempty"`
	Sections  map[string][]string `yaml:"sections,omitempty" json:"sections,omitempty"`
	Pages     map[string][]string `yaml:"pages,omitempty" json:"pages,omitempty"`
}

// ConfigPath returns the absolute path to a vault's config.yaml.
func ConfigPath(vaultPath string) string {
	return filepath.Join(vaultPath, ".system", "config.yaml")
}

// Load reads <vault>/.system/config.yaml. A missing file is not an error: it
// returns Defaults() so a fresh vault works without an explicit config. A file
// that exists but fails to parse returns an error (do not silently fall
// through to defaults — the user has a config, it is just broken). Fields
// absent from the file keep their default values.
func Load(vaultPath string) (SystemConfig, error) {
	data, err := safeio.ReadFileMax(ConfigPath(vaultPath), maxConfigYAMLBytes)
	if err != nil {
		if os.IsNotExist(err) {
			return Defaults(), nil
		}
		return Defaults(), fmt.Errorf("failed to read config.yaml: %w", err)
	}

	// Decode over the defaults so omitted sections keep their default values
	// rather than being zero-valued.
	//
	// Merge semantics worth knowing: yaml.v3 decodes into the pre-populated
	// struct, so SCALAR fields absent from the file keep their default, while
	// MAP fields (hotkeys, plugin_settings) are MERGED — keys present in the
	// file override defaults, but keys ABSENT from the file are NOT removed.
	// Practically: deleting a default hotkey/plugin-setting entry from
	// config.yaml will silently restore it on the next load. To "remove" a
	// hotkey, set it to an empty string ("") rather than deleting the line.
	// (A zero-value-first unmarshal + custom presence-aware merge would change
	// this, but it is a deliberate behavior change left out of scope here.)
	cfg := Defaults()
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Defaults(), fmt.Errorf("failed to parse config.yaml: %w", err)
	}
	cfg = normalize(cfg)
	return cfg, nil
}

// Save atomically writes cfg to <vault>/.system/config.yaml. Atomicity
// (temp file + fsync + rename) guarantees the on-disk file is either the
// previous version or the new one in full, never a half-written file.
func Save(vaultPath string, cfg SystemConfig) error {
	cfg = normalize(cfg)
	out, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to marshal config.yaml: %w", err)
	}
	return writeFileAtomic(ConfigPath(vaultPath), out, 0o600)
}

// writeFileAtomic writes data to a sibling temp file, fsyncs it, then renames
// it over path. Kept local (rather than reusing parser.WriteFileAtomic) so the
// config package stays decoupled from the markdown parser.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".config-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // best-effort cleanup on any failure path

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename temp file: %w", err)
	}
	return nil
}
