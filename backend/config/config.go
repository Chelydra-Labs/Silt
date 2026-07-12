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
	"sort"
	"strings"

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

// AIConfig holds the shared chat-LLM and embedding-model provider configuration
// (Sprint 20). The two are INDEPENDENT: a user may run a local Ollama chat model
// and a cloud embedding endpoint, or any other combination. Both chat and
// embedding call OpenAI-compatible endpoints; providerType only nudges the
// default base URL + whether a key is expected.
type AIConfig struct {
	Chat      AIProviderConfig `yaml:"chat" json:"chat"`
	Embedding AIProviderConfig `yaml:"embedding" json:"embedding"`
	// UseKeyring, when true (default), stores provider API keys in the OS
	// credential store instead of plaintext config.yaml (#218). Tri-state so
	// "unset" stays distinguishable from "explicitly false" through the Load →
	// normalize path. nil reads as true downstream.
	UseKeyring *bool `yaml:"use_keyring,omitempty" json:"use_keyring,omitempty"`
}

// AIProviderConfig is one provider endpoint (chat OR embedding). It is the unit
// the AI Provider settings page edits. APIKey carries the yaml tag so the value
// persists to config.yaml (the migration/fallback slot when the OS keyring is
// unavailable), but the json tag is "-" so a GetSystemConfig / GetAIProviderConfig
// round-trip can NEVER leak the secret to plugin JS or the frontend. The
// dedicated SetAIAPIKey binding is the only write path; a full SaveSystemConfig
// round-trip preserves the existing key server-side (see SaveSystemConfig).
type AIProviderConfig struct {
	ProviderType    string   `yaml:"provider_type,omitempty" json:"provider_type"`                 // "local" | "openai-compatible"
	BaseURL         string   `yaml:"base_url,omitempty" json:"base_url"`                           // e.g. http://localhost:11434 (Ollama) or https://openrouter.ai/api/v1
	APIKey          string   `yaml:"api_key,omitempty" json:"-"`                                   // NEVER serialized to JS
	Model           string   `yaml:"model,omitempty" json:"model"`                                 // e.g. qwen3:30b-a3b, nomic-embed-text
	Temperature     *float64 `yaml:"temperature,omitempty" json:"temperature,omitempty"`           // chat only
	MaxTokens       *int     `yaml:"max_tokens,omitempty" json:"max_tokens,omitempty"`             // chat only
	ReasoningEffort *string  `yaml:"reasoning_effort,omitempty" json:"reasoning_effort,omitempty"` // chat only: "none"|"minimal"|"low"|"medium"|"high"|"xhigh"|"max"
	TimeoutMs       *int     `yaml:"timeout_ms,omitempty" json:"timeout_ms,omitempty"`             // per-call; default 60000
	Dimensions      *int     `yaml:"dimensions,omitempty" json:"dimensions,omitempty"`             // embeddings only (truncation)
}

// AI provider type discriminators. "local" targets an Ollama/llama.cpp instance
// on the same machine (no key expected by default); "openai-compatible" targets
// a cloud/local OpenAI-compatible endpoint (OpenRouter, LM Studio, OpenAI,
// llama-server) where a Bearer token is expected. "google" and "anthropic"
// target the providers' native first-party APIs (#479), bypassing the OpenAI
// compat shape for better stability + structured-output support. The ai package
// dispatches on these values; the string literals MUST match ai.Provider*.
const (
	AIProviderLocal            = "local"
	AIProviderOpenAICompatible = "openai-compatible"
	AIProviderGoogle           = "google"
	AIProviderAnthropic        = "anthropic"
)

// DefaultAIBaseURL is the conventional local endpoint (Ollama's default port).
// Used as the default base URL when providerType is "local" and none is set.
const DefaultAIBaseURL = "http://localhost:11434"

// DefaultGoogleBaseURL is the Google AI Studio (generativelanguage) endpoint.
// The native generateContent / batchEmbedContents / listModels paths are rooted
// under /v1beta/.
const DefaultGoogleBaseURL = "https://generativelanguage.googleapis.com"

// DefaultAnthropicBaseURL is the Anthropic Messages API endpoint.
const DefaultAnthropicBaseURL = "https://api.anthropic.com"

// DefaultAITimeoutMs is the per-call timeout when AIProviderConfig.TimeoutMs is
// unset. Generous (LLM completions are slow) but bounded so a dead endpoint
// cannot hang a plugin call forever.
const DefaultAITimeoutMs = 60000

// validAIReasoningEfforts is the set of reasoning_effort values accepted across
// OpenAI-compatible providers (OpenAI, Ollama, vLLM, OpenRouter, …). "none"
// means "do not send the parameter"; the others ramp the reasoning budget. Kept
// as the single source of truth so the binding layer can reject a typo at the
// gate (instead of forwarding it to a provider for a 400) and NormalizeAIConfig
// can drop a stale/unknown value from a hand-edited config.yaml.
var validAIReasoningEfforts = map[string]bool{
	"none": true, "minimal": true, "low": true,
	"medium": true, "high": true, "xhigh": true, "max": true,
}

// IsValidAIReasoningEffort reports whether s is a recognized reasoning_effort
// value. Callers pass a already-trimmed value. Used by the AI binding layer
// (UpdateAIProviderConfig / PluginAIComplete) to reject unknown values early.
func IsValidAIReasoningEffort(s string) bool {
	return validAIReasoningEfforts[s]
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
	SidebarWidth      int      `yaml:"sidebar_width" json:"sidebar_width"`
	NavOrder          NavOrder `yaml:"nav_order,omitempty" json:"nav_order,omitempty"`
	OpenTabs          []TabRef `yaml:"open_tabs,omitempty" json:"open_tabs,omitempty"`
	ActiveTab         *TabRef  `yaml:"active_tab,omitempty" json:"active_tab,omitempty"`
	EnablePreviewTabs *bool    `yaml:"enable_preview_tabs,omitempty" json:"enable_preview_tabs,omitempty"`
	MaxOpenTabs       int      `yaml:"max_open_tabs,omitempty" json:"max_open_tabs,omitempty"`
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

// hotkeyModifiers are the modifier tokens allowed in a hotkey binding
// (case-insensitive). Everything else in a binding is treated as the key.
var hotkeyModifiers = map[string]bool{
	"ctrl": true, "control": true, "shift": true,
	"alt": true, "option": true, "meta": true,
	"cmd": true, "command": true, "win": true,
}

// ValidateHotkeys rejects bindings that would parse to a null hotkey and
// silently disable the action. An empty binding is allowed (it means
// "intentionally disabled" — matchHotkey never fires — which is also the only
// way to disable a hotkey, since deleting the key would restore the default
// via the YAML merge). A non-empty binding must contain at least one
// non-modifier token, mirroring the frontend parseHotkey's null outcome so the
// two layers agree on what is valid.
func ValidateHotkeys(hotkeys map[string]string) error {
	for action, binding := range hotkeys {
		binding = strings.TrimSpace(binding)
		if binding == "" {
			continue // explicitly disabled
		}
		hasKey := false
		for _, p := range strings.Split(strings.ToLower(binding), "+") {
			t := strings.TrimSpace(p)
			if t == "" {
				continue // tolerate stray empty segments (e.g. "Ctrl++P")
			}
			if !hotkeyModifiers[t] {
				hasKey = true
			}
		}
		if !hasKey {
			return fmt.Errorf("invalid hotkey for %q: %q has no key (only modifiers)", action, binding)
		}
	}
	// NOTE: no cross-action duplicate-chord check. Several defaults share a
	// chord by design and are disambiguated by focus context — e.g. Ctrl+B is
	// toggle_sidebar (global, fires when the editor is not focused) AND
	// format_bold (consumed by the editor's ProseMirror keymap when focused).
	// A blanket duplicate detector would reject every config built from these
	// defaults. The one known *unintended* collision — format_subscript ↔
	// open_settings on Ctrl+, after the #511 move — is resolved deterministically
	// in normalize() instead, since the YAML merge can't tell a persisted old
	// default from an explicit choice.
	return nil
}

// ConfigPath returns the absolute path to a vault's config.yaml.
func ConfigPath(vaultPath string) string {
	return filepath.Join(vaultPath, ".system", "config.yaml")
}

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
			TypewriterMode:          boolPtr(false),
			TypewriterModeRatio:     float64Ptr(0.5),
			CustomDictionary:        []string{},
			SpellcheckDomains:       []string{"software-terms"},
		},
		Parsing: ParsingConfig{
			AutoInjectUUID:      true,
			DefaultTaskPriority: 3,
		},
		Hotkeys: map[string]string{
			// Sprint 17 hotkey realignment (convention-anchored; see SPECS.md
			// "Keyboard Shortcuts"). Windows/Linux only.
			// open_search: cross-vault global search → Ctrl+Shift+F (the VS
			// Code / Sublime / Notepad++ "find in files" convention; Office/Docs
			// have no cross-file search). Frees Ctrl+P for future Print.
			"open_search": "Ctrl+Shift+F",
			// open_command_palette → Alt+Q (Office "Tell Me" / search-the-app
			// convention). Frees Ctrl+/ for other use.
			"open_command_palette": "Alt+Q",
			"toggle_sidebar":       "Ctrl+B",
			"focus_sidebar":        "Ctrl+Shift+B",
			// cycle_view_layout → Ctrl+Alt+V. Alt+Tab is the OS window-switcher
			// on Windows/Linux (captured before the app sees it) and never fired.
			"cycle_view_layout": "Ctrl+Alt+V",
			// open_settings → Ctrl+, (the universal settings convention; VS Code
			// and most editors). #511 opens settings as a workspace tab. Note this
			// freed Ctrl+, from format_subscript, which moved to Ctrl+Shift+, below.
			"open_settings":        "Ctrl+,",
			"indent_block":         "Tab",
			"unindent_block":       "Shift+Tab",
			"open_template_picker": "Ctrl+Shift+T",
			// Global standalone-task quick-add (#368). Opens an app-level
			// overlay (not a plugin action) that creates a task in
			// <vault>/.silt/tasks.md. "N" for New; Shift+T was taken by the
			// template picker.
			"new_task": "Ctrl+Shift+N",
			// Hub-scoped Tasks command palette (#436). Same chord as
			// format_link (Ctrl+K); conflict is resolved by focus scope —
			// the hub handler only fires when focus is not an input /
			// textarea / contenteditable / ProseMirror, so the editor keeps
			// format_link while typing.
			"tasks_command_palette": "Ctrl+K",
			// Tab strip hotkeys (#142). `tab` and `w` already parse cleanly
			// via the frontend parseHotkey layer (KEY_ALIASES in
			// frontend/src/settings/hotkeys.ts). Each may be remapped or
			// disabled (set to "") from Settings → General.
			"next_tab":  "Ctrl+Tab",
			"prev_tab":  "Ctrl+Shift+Tab",
			"close_tab": "Ctrl+W",
			// Inline formatting hotkeys (#168). Standard editor bindings
			// so muscle memory transfers. Each is overridable per-vault via
			// the deep-merge. The editor's ProseMirror keymaps consume these
			// inside the contenteditable; the global handler skips them when
			// the editor is focused (Ctrl+B resolution).
			"format_bold":        "Ctrl+B",
			"format_italic":      "Ctrl+I",
			"format_underline":   "Ctrl+U",
			"format_strike":      "Alt+Shift+5",
			"format_code":        "Ctrl+E",
			"format_link":        "Ctrl+K",
			"format_highlight":   "Ctrl+Shift+H",
			"format_subscript":   "Ctrl+Shift,",
			"format_superscript": "Ctrl+.",
			// Heading level hotkeys (#169). Standard heading-level bindings.
			"set_h1":   "Ctrl+Alt+1",
			"set_h2":   "Ctrl+Alt+2",
			"set_h3":   "Ctrl+Alt+3",
			"set_note": "Ctrl+Alt+0",
			"set_task": "Ctrl+Alt+4",
			// Text alignment hotkeys (#173). Standard alignment bindings.
			"align_left":    "Ctrl+Shift+L",
			"align_center":  "Ctrl+Shift+E",
			"align_right":   "Ctrl+Shift+R",
			"align_justify": "Ctrl+Shift+J",
			// Blockquote toggle (#188). Standard blockquote binding.
			"toggle_quote": "Ctrl+Shift+9",
			// Foldable details toggle (#183). Ctrl+Shift+. (Ctrl+. is taken by
			// the Superscript mark).
			"toggle_details": "Ctrl+Shift+.",
			// Table row/column insert hotkeys (#172). Standard row/column-insert
			// bindings; deletion + merge are toolbar-only in v1.
			"table_insert_row_above": "Ctrl+Shift+Up",
			"table_insert_row_below": "Ctrl+Shift+Down",
			"table_insert_col_left":  "Ctrl+Shift+Left",
			"table_insert_col_right": "Ctrl+Shift+Right",
			// View mode toggle (#171). Standard source/view toggle binding.
			"toggle_view_mode": "Ctrl+Shift+V",
			// Formatting toolbar toggle and focus mode toggle (#168 Phase 3).
			// toggle_format_toolbar → Ctrl+F1 (Office "toggle ribbon" convention);
			// frees Ctrl+Shift+F for global search (open_search above).
			"toggle_format_toolbar": "Ctrl+F1",
			"toggle_focus_mode":     "Ctrl+Shift+D",
			// Sprint 17 — Search, Find/Replace & Writing Aids.
			// find_in_page (Ctrl+F) and replace (Ctrl+H) are the universal
			// in-editor find/replace bindings (VS Code/Docs/Office). global_replace
			// (Ctrl+Shift+G) escalates replace to cross-vault (VS Code "replace in
			// files"). toggle_typewriter_mode (Ctrl+Shift+Y) pairs with
			// toggle_focus_mode (Ctrl+Shift+D) as the writing-mode toggles.
			// Spellcheck deliberately has NO hotkey — it's wavy-underline +
			// right-click + a FormatToolbar button (see SPECS.md).
			"find_in_page":           "Ctrl+F",
			"replace":                "Ctrl+H",
			"global_replace":         "Ctrl+Shift+G",
			"toggle_typewriter_mode": "Ctrl+Shift+Y",
		},
		Plugins: PluginsConfig{
			// silt-tasks is the unified task surface (Phase 9 / #431),
			// succeeding the retired standalone silt-calendar / silt-kanban.
			Active: []string{"silt-tasks"},
			// silt-ai-summary (#220) ships OFF by default: it is the first plugin
			// that sends note content to an external LLM endpoint, so the user
			// opts in explicitly (Plugins tab) after configuring a provider.
			Disabled: []string{"silt-ai-summary"},
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
			OpenTabs: []TabRef{},
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
			OpenDevtoolsOnStartup:  boolPtr(false),
			Formatting: FormattingConfig{
				TypographyEnabled: boolPtr(true),
				ColorEnabled:      boolPtr(true),
				MathEnabled:       boolPtr(true),
			},
		},
		// AI providers ship unconfigured (Sprint 20): no chat model, no
		// embedding model, no endpoint. The AI Provider page's empty-state
		// nudge fires until the user configures one. UseKeyring defaults
		// true so the first key a user enters lands in the OS keyring, not
		// plaintext config.yaml (#218).
		AI: AIConfig{
			Chat:       AIProviderConfig{ProviderType: AIProviderLocal, BaseURL: DefaultAIBaseURL},
			Embedding:  AIProviderConfig{ProviderType: AIProviderLocal, BaseURL: DefaultAIBaseURL},
			UseKeyring: boolPtr(true),
		},
	}
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

// LinkedConfigPath returns the absolute path to a linked notebook's
// co-located config.yaml. Per the storage-of-truth model (#133), data
// attached to a notebook travels with the notebook: for a linked (external)
// notebook, per-notebook plugin overrides live at
// `<linkedRoot>/.system/config.yaml`, so an external notebook on SharePoint
// carries its own config with it — not in the vault. Silt treats this file
// as READ-ONLY / user-authored; plugin settings continue to persist to the
// vault-scoped config.yaml via the atomic UpdatePluginSetting path. The
// co-located file is purely an override layer the user authors on the
// external mount.
func LinkedConfigPath(linkedRoot string) string {
	return filepath.Join(linkedRoot, ".system", "config.yaml")
}

// LoadLinked reads a linked notebook's co-located `<linkedRoot>/.system/
// config.yaml` (#133). A missing file is NOT an error: it returns Defaults()
// with a nil error, because a linked notebook without a co-located config is
// the normal case (the vault-scoped config.yaml still provides the baseline).
// A file that exists but fails to parse returns Defaults() with a wrapped
// error — the caller MUST surface this so the user can fix the source rather
// than silently inheriting defaults. Mirrors Load's decode-over-Defaults
// semantics so omitted sections keep their default values.
func LoadLinked(linkedRoot string) (SystemConfig, error) {
	path := LinkedConfigPath(linkedRoot)
	data, err := safeio.ReadFileMax(path, maxConfigYAMLBytes)
	if err != nil {
		if os.IsNotExist(err) {
			return Defaults(), nil
		}
		return Defaults(), fmt.Errorf("failed to read linked config.yaml: %w", err)
	}
	cfg := Defaults()
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Defaults(), fmt.Errorf("failed to parse linked config.yaml: %w", err)
	}
	return normalize(cfg), nil
}

// MergePluginSettings deep-merges two per-plugin settings maps for the
// co-located config override layer (#133). `vault` is the plugin's entry in
// the vault-scoped config.yaml; `linked` is the plugin's entry in the linked
// notebook's co-located config.yaml. The result is a NEW map (vault is not
// mutated) where:
//   - keys present ONLY in vault are preserved;
//   - keys present ONLY in linked are added;
//   - keys present in BOTH are merged: nested `map[string]any` values merge
//     recursively (linked's sub-keys override vault's per-key); scalars and
//     arrays from linked REPLACE vault's.
//
// This mirrors the user expectation that "the notebook's value wins" without
// losing vault defaults the notebook did not override. Both inputs may be nil
// (treated as empty); the result is always non-nil.
func MergePluginSettings(vault, linked map[string]any) map[string]any {
	out := make(map[string]any, len(vault)+len(linked))
	for k, v := range vault {
		out[k] = cloneValue(v)
	}
	for k, lv := range linked {
		if rv, ok := out[k]; ok {
			if rmap, rOK := rv.(map[string]any); rOK {
				if lmap, lOK := lv.(map[string]any); lOK {
					out[k] = mergeMaps(rmap, lmap)
					continue
				}
			}
		}
		out[k] = cloneValue(lv)
	}
	return out
}

// mergeMaps returns a new map that is `a` deep-merged with `b` (b wins per
// key, nested maps recurse). Neither input is mutated.
func mergeMaps(a, b map[string]any) map[string]any {
	out := make(map[string]any, len(a)+len(b))
	for k, v := range a {
		out[k] = cloneValue(v)
	}
	for k, bv := range b {
		if av, ok := out[k]; ok {
			if amap, aOK := av.(map[string]any); aOK {
				if bmap, bOK := bv.(map[string]any); bOK {
					out[k] = mergeMaps(amap, bmap)
					continue
				}
			}
		}
		out[k] = cloneValue(bv)
	}
	return out
}

// cloneValue returns a deep copy of a YAML-derived value. Only the types
// yaml.v3 can produce are handled: map[string]any, []any, string, bool, int,
// int64, float64, and nil. Maps and slices are deep-copied so the merge
// never aliases the caller's input; scalars are returned as-is (immutable).
func cloneValue(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, vv := range x {
			out[k] = cloneValue(vv)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, vv := range x {
			out[i] = cloneValue(vv)
		}
		return out
	default:
		return v
	}
}

// normalize guarantees non-nil slices/maps and a populated hotkeys table so
// downstream consumers (and JSON serialization over the IPC boundary) never
// see null where an empty collection is meant.
func normalize(cfg SystemConfig) SystemConfig {
	if cfg.Plugins.Active == nil {
		cfg.Plugins.Active = []string{}
	}
	if cfg.Plugins.Disabled == nil {
		cfg.Plugins.Disabled = []string{}
	}
	if cfg.Plugins.PluginSettings == nil {
		cfg.Plugins.PluginSettings = map[string]any{}
	}
	// NOTE: grants normalization removed — grants now live in per-host storage
	// (backend/vault/grants.go, F4). The field is gone from PluginsConfig so a
	// synced vault's legacy `grants:` block is silently ignored by yaml.v3.
	if cfg.Hotkeys == nil {
		cfg.Hotkeys = map[string]string{}
	}
	// format_subscript ↔ open_settings upgrade migration (#511). The Ctrl+,
	// chord moved from format_subscript to the new open_settings default, but
	// the YAML merge decodes-over-Defaults and can't tell a persisted old
	// default from an explicit choice — so a config saved before this change
	// ends up with format_subscript AND open_settings both at "Ctrl+,". Move
	// subscript to its new home (Ctrl+Shift,) only when the exact collision is
	// present, so a user who customized either binding is never touched.
	if cfg.Hotkeys["format_subscript"] == "Ctrl+," && cfg.Hotkeys["open_settings"] == "Ctrl+," {
		cfg.Hotkeys["format_subscript"] = "Ctrl+Shift,"
	}
	if cfg.UI.NavOrder.Sections == nil {
		cfg.UI.NavOrder.Sections = map[string][]string{}
	}
	if cfg.UI.NavOrder.Pages == nil {
		cfg.UI.NavOrder.Pages = map[string][]string{}
	}
	if cfg.UI.SidebarWidth < 200 {
		cfg.UI.SidebarWidth = 256
	}
	if cfg.UI.OpenTabs == nil {
		cfg.UI.OpenTabs = []TabRef{}
	}
	// Per-tab ViewMode (#195): only "source" is a meaningful override; every
	// other value (including a hand-edited garbage string) collapses to "" so
	// the frontend reads the Edit default. Applied to both OpenTabs and the
	// ActiveTab pointer — both persist view_mode, so a corrupted value on
	// either must not survive normalize. Kept defensive rather than strict —
	// TabRef entries are pruned against ListNavigation upstream, so an unknown
	// value must never abort the whole config load.
	for i := range cfg.UI.OpenTabs {
		if cfg.UI.OpenTabs[i].ViewMode != "source" {
			cfg.UI.OpenTabs[i].ViewMode = ""
		}
	}
	if cfg.UI.ActiveTab != nil && cfg.UI.ActiveTab.ViewMode != "source" {
		cfg.UI.ActiveTab.ViewMode = ""
	}
	// MaxOpenTabs: 0 (legacy config without the key) → 8 (the default).
	// Negative or absurdly-small values also fall back. An upper bound of
	// 32 prevents a user from mounting hundreds of TipTap editors
	// simultaneously and exhausting memory (#142 hardening).
	if cfg.UI.MaxOpenTabs < 1 {
		cfg.UI.MaxOpenTabs = 8
	}
	if cfg.UI.MaxOpenTabs > 32 {
		cfg.UI.MaxOpenTabs = 32
	}
	// EnablePreviewTabs: nil → true (industry-standard parity). The field is a *bool
	// so "unset" stays distinguishable from "explicitly false" through the
	// Load → normalize path; once normalized, the frontend reads nil as
	// true.
	if cfg.UI.EnablePreviewTabs == nil {
		cfg.UI.EnablePreviewTabs = boolPtr(true)
	}
	// ShowFormatToolbar: nil → true (#168). Same *bool semantics as
	// EnablePreviewTabs.
	if cfg.UI.ShowFormatToolbar == nil {
		cfg.UI.ShowFormatToolbar = boolPtr(true)
	}
	// ShowTabDirtyIndicators: nil → true (#167). Same *bool semantics.
	if cfg.UI.ShowTabDirtyIndicators == nil {
		cfg.UI.ShowTabDirtyIndicators = boolPtr(true)
	}
	if cfg.UI.DismissedTips == nil {
		cfg.UI.DismissedTips = []string{}
	}
	// TypographyEnabled: nil → true (#168 Phase 3).
	if cfg.UI.Formatting.TypographyEnabled == nil {
		cfg.UI.Formatting.TypographyEnabled = boolPtr(true)
	}
	if cfg.UI.Formatting.ColorEnabled == nil {
		cfg.UI.Formatting.ColorEnabled = boolPtr(true)
	}
	// MathEnabled: nil → true (#191).
	if cfg.UI.Formatting.MathEnabled == nil {
		cfg.UI.Formatting.MathEnabled = boolPtr(true)
	}
	// ShowWordCount: nil → false (#168 Phase 3). Opt-in.
	if cfg.Editor.ShowWordCount == nil {
		cfg.Editor.ShowWordCount = boolPtr(false)
	}
	// FocusMode: nil → false (#168 Phase 3).
	if cfg.Editor.FocusMode == nil {
		cfg.Editor.FocusMode = boolPtr(false)
	}
	// DefaultViewMode: nil → "edit" (#171). Validate to edit/source.
	if cfg.Editor.DefaultViewMode == nil {
		cfg.Editor.DefaultViewMode = stringPtr("edit")
	} else {
		v := strings.TrimSpace(*cfg.Editor.DefaultViewMode)
		if v != "edit" && v != "source" {
			cfg.Editor.DefaultViewMode = stringPtr("edit")
		} else {
			cfg.Editor.DefaultViewMode = stringPtr(v)
		}
	}
	// SpellcheckEnabled: nil → true (#196). Matches every competing note app;
	// markdown purists disable from Settings → Editor.
	if cfg.Editor.SpellcheckEnabled == nil {
		cfg.Editor.SpellcheckEnabled = boolPtr(true)
	}
	// SpellcheckLanguage: nil → "en-US" (#196). A non-empty value must name a
	// dictionary shipped under frontend/public/dictionaries/<lang>/; an empty
	// or whitespace-only value collapses to the default rather than failing
	// the whole config load (defensive — a hand-edited blank shouldn't abort).
	if cfg.Editor.SpellcheckLanguage == nil {
		cfg.Editor.SpellcheckLanguage = stringPtr("en-US")
	} else {
		v := strings.TrimSpace(*cfg.Editor.SpellcheckLanguage)
		if v == "" {
			v = "en-US"
		}
		cfg.Editor.SpellcheckLanguage = stringPtr(v)
	}
	// TypewriterMode: nil → false (#187). Opt-in distraction-free scroll.
	if cfg.Editor.TypewriterMode == nil {
		cfg.Editor.TypewriterMode = boolPtr(false)
	}
	// TypewriterModeRatio: nil → 0.5 (iA Writer default; #187). Clamp to
	// [0.1, 0.9] so the active line stays meaningfully on-screen — 0.0 would
	// pin it to the very top edge, 1.0 to the very bottom.
	if cfg.Editor.TypewriterModeRatio == nil {
		cfg.Editor.TypewriterModeRatio = float64Ptr(0.5)
	} else {
		r := *cfg.Editor.TypewriterModeRatio
		if r < 0.1 {
			r = 0.1
		}
		if r > 0.9 {
			r = 0.9
		}
		cfg.Editor.TypewriterModeRatio = float64Ptr(r)
	}
	// CustomDictionary: the per-vault spellcheck word list (#196). Normalize
	// to a non-nil, de-duplicated, trimmed, lowercased, sorted slice so the
	// IPC layer never serializes null and lookups are deterministic. Case is
	// flattened because Hunspell lookups are case-insensitive for en-US and
	// the list is a set, not an order-preserving collection.
	if cfg.Editor.CustomDictionary == nil {
		cfg.Editor.CustomDictionary = []string{}
	} else {
		seen := make(map[string]bool, len(cfg.Editor.CustomDictionary))
		out := make([]string, 0, len(cfg.Editor.CustomDictionary))
		for _, w := range cfg.Editor.CustomDictionary {
			w = strings.ToLower(strings.TrimSpace(w))
			if w == "" || seen[w] {
				continue
			}
			seen[w] = true
			out = append(out, w)
		}
		sort.Strings(out)
		cfg.Editor.CustomDictionary = out
	}
	// SpellcheckDomains: enabled technical word-list packs (#337). nil →
	// default ["software-terms"] so fresh installs get common false-positive
	// coverage without a download. Empty explicit slice is preserved (user
	// turned everything off). Unknown IDs are dropped; known IDs are
	// de-duplicated and sorted.
	if cfg.Editor.SpellcheckDomains == nil {
		cfg.Editor.SpellcheckDomains = []string{"software-terms"}
	} else {
		known := map[string]bool{
			"software-terms": true,
			"typescript":     true,
			"python":         true,
			"data-science":   true,
		}
		seen := make(map[string]bool, len(cfg.Editor.SpellcheckDomains))
		out := make([]string, 0, len(cfg.Editor.SpellcheckDomains))
		for _, id := range cfg.Editor.SpellcheckDomains {
			id = strings.TrimSpace(id)
			if id == "" || !known[id] || seen[id] {
				continue
			}
			seen[id] = true
			out = append(out, id)
		}
		sort.Strings(out)
		cfg.Editor.SpellcheckDomains = out
	}
	// OpenDevtoolsOnStartup: nil → false. Dev Mode is opt-in from About.
	if cfg.UI.OpenDevtoolsOnStartup == nil {
		cfg.UI.OpenDevtoolsOnStartup = boolPtr(false)
	}
	// AI provider config (Sprint 20). UseKeyring defaults true (#218): keys
	// belong in the OS credential store, not plaintext config.yaml. Each
	// provider's type collapses to a known discriminator; an empty base URL
	// for a local provider falls back to the Ollama default. Keys are never
	// validated here (they may legitimately be empty for a local endpoint).
	cfg.AI = NormalizeAIConfig(cfg.AI)
	return cfg
}

// normalizeAIConfig applies the AI provider normalization rules. Exported so
// the dedicated UpdateAIProviderConfig binding can normalize a single patch the
// same way the full-config normalize path does.
func NormalizeAIConfig(ai AIConfig) AIConfig {
	if ai.UseKeyring == nil {
		ai.UseKeyring = boolPtr(true)
	}
	ai.Chat = normalizeAIProvider(ai.Chat, true)
	ai.Embedding = normalizeAIProvider(ai.Embedding, false)
	return ai
}

// normalizeAIProvider coerces one provider block into a canonical form. isChat
// distinguishes the chat block (Temperature/MaxTokens apply) from the embedding
// block (Dimensions applies); the unused advanced knob on the wrong block is
// dropped so a stale value cannot drift in config.yaml.
func normalizeAIProvider(p AIProviderConfig, isChat bool) AIProviderConfig {
	p.ProviderType = strings.TrimSpace(p.ProviderType)
	switch p.ProviderType {
	case AIProviderLocal, AIProviderOpenAICompatible, AIProviderGoogle, AIProviderAnthropic:
		// known type — keep as-is
	default:
		// Unknown/empty → local (the safest default — nothing leaves the
		// machine, no key expected).
		p.ProviderType = AIProviderLocal
	}
	p.BaseURL = strings.TrimSpace(p.BaseURL)
	if p.BaseURL == "" {
		switch p.ProviderType {
		case AIProviderLocal:
			p.BaseURL = DefaultAIBaseURL
		case AIProviderGoogle:
			p.BaseURL = DefaultGoogleBaseURL
		case AIProviderAnthropic:
			p.BaseURL = DefaultAnthropicBaseURL
		}
	}
	p.Model = strings.TrimSpace(p.Model)
	p.APIKey = strings.TrimSpace(p.APIKey)
	// Validate reasoning_effort against the documented enum so a stale or
	// hand-typed unknown value is dropped rather than forwarded to a provider
	// for a 400. Applies to chat only; normalize drops it for embeddings below.
	if p.ReasoningEffort != nil {
		re := strings.TrimSpace(*p.ReasoningEffort)
		if IsValidAIReasoningEffort(re) {
			p.ReasoningEffort = &re
		} else {
			p.ReasoningEffort = nil
		}
	}
	// Drop advanced knobs that don't apply to this block so a user who flips
	// chat↔embedding in the UI doesn't leave a stale value behind.
	if isChat {
		p.Dimensions = nil
	} else {
		p.Temperature = nil
		p.MaxTokens = nil
		p.ReasoningEffort = nil
	}
	// Bound the per-call timeout. A negative value is nonsensical; an
	// absurdly large value would let a dead endpoint hang a plugin call. nil
	// is left to the service to default at call time.
	if p.TimeoutMs != nil {
		t := *p.TimeoutMs
		if t < 0 {
			t = 0
		}
		if t > 300000 { // 5 min hard cap
			t = 300000
		}
		p.TimeoutMs = intPtr(t)
	}
	// Dimensions must be positive when set (a 0/negative would truncate to
	// nothing). Left as a pointer so "unset" stays distinct from a deliberate
	// model-native value.
	if p.Dimensions != nil && *p.Dimensions <= 0 {
		p.Dimensions = nil
	}
	return p
}

// boolPtr is a small helper for the Defaults() block so *bool fields can be
// initialized inline without a temporary variable.
func boolPtr(b bool) *bool { return &b }

// stringPtr is a small helper for the Defaults() block so *string fields can
// be initialized inline without a temporary variable.
func stringPtr(s string) *string { return &s }

// float64Ptr is a small helper for the Defaults() block so *float64 fields can
// be initialized inline without a temporary variable.
func float64Ptr(f float64) *float64 { return &f }

// intPtr is a small helper so *int fields (AI provider advanced knobs) can be
// initialized inline without a temporary variable.
func intPtr(i int) *int { return &i }

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
