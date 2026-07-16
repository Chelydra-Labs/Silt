package config

import (
	"fmt"
	"sort"
	"strings"

	"silt/backend/spellcheck"
)

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
	// Off-by-default AI plugins must stay disabled on upgraded vaults.
	// yaml.Unmarshal replaces the Defaults() Disabled slice entirely when the
	// file has any `plugins.disabled:` entry (e.g. only silt-ai-summary), so a
	// pre-feature config would otherwise load new AI plugins as enabled and
	// start embedding note content without opt-in (PR #540 review).
	cfg = seedOptInDisabledPlugins(cfg)
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
		cfg.Editor.SpellcheckDomains = spellcheck.DefaultDomainIDs()
	} else {
		seen := make(map[string]bool, len(cfg.Editor.SpellcheckDomains))
		out := make([]string, 0, len(cfg.Editor.SpellcheckDomains))
		for _, id := range cfg.Editor.SpellcheckDomains {
			id = strings.TrimSpace(id)
			if id == "" || !spellcheck.IsKnownDomainID(id) || seen[id] {
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

// optInDisabledPluginIDs are first-party plugins introduced as OFF-by-default
// that must be re-seeded into plugins.disabled for upgraded vaults. Only list
// plugins that are NEW in a release — never re-seed long-shipped ids (e.g.
// silt-ai-summary) or users who already enabled them would be re-disabled.
// YAML decode replaces the Defaults() Disabled slice when the file has any
// plugins.disabled entry, so pre-feature configs omit new ids entirely.
var optInDisabledPluginIDs = []string{
	"silt-ai-qa",        // Sprint 22 / #224 — PR #540 review
	"silt-ai-assistant", // Sprint 23 / #230 — Writing Assistant
	"silt-ai-agent",     // AI Agent — opt-in by default
}

// seededOptInDisabledKey is a one-shot marker list under plugin_settings so we
// never re-disable a plugin the user explicitly enabled after the seed ran.
const seededOptInDisabledKey = "_seeded_opt_in_disabled"

// seedOptInDisabledPlugins appends missing off-by-default AI plugin ids to
// plugins.disabled exactly once per id. Marker lives in plugin_settings so a
// later user enable (remove from disabled) is not undone on the next Load.
func seedOptInDisabledPlugins(cfg SystemConfig) SystemConfig {
	if cfg.Plugins.PluginSettings == nil {
		cfg.Plugins.PluginSettings = map[string]any{}
	}
	seeded := stringSliceFromAny(cfg.Plugins.PluginSettings[seededOptInDisabledKey])
	seededSet := make(map[string]bool, len(seeded))
	for _, id := range seeded {
		seededSet[id] = true
	}
	disabledSet := make(map[string]bool, len(cfg.Plugins.Disabled))
	for _, id := range cfg.Plugins.Disabled {
		disabledSet[id] = true
	}
	changed := false
	for _, id := range optInDisabledPluginIDs {
		if seededSet[id] {
			continue
		}
		// First encounter of this opt-in plugin id: force disabled for
		// upgraded vaults that never listed it. Fresh Defaults already include
		// it; append is a no-op-ish (duplicate avoided via disabledSet).
		if !disabledSet[id] {
			cfg.Plugins.Disabled = append(cfg.Plugins.Disabled, id)
			disabledSet[id] = true
		}
		seeded = append(seeded, id)
		seededSet[id] = true
		changed = true
	}
	if changed || len(seeded) > 0 {
		// Always store as []string so YAML round-trip + DeepEqual stay stable
		// (yaml.v3 reloads sequences as []any).
		cfg.Plugins.PluginSettings[seededOptInDisabledKey] = seeded
	}
	return cfg
}

func stringSliceFromAny(v any) []string {
	switch t := v.(type) {
	case []string:
		return append([]string(nil), t...)
	case []any:
		out := make([]string, 0, len(t))
		for _, e := range t {
			if s, ok := e.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}
