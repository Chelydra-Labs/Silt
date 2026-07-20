package config

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"silt/backend/spellcheck"
)

const (
	MaxRecentPages      = 20
	MaxExpandedSections = 512
	MaxFavoritePages    = 512
	MaxRecentTags       = 12
	MaxTagPathBytes     = 256
)

// tagPathRegex enforces the canonical tag-path character contract shared by
// the indexer (backend/db.tagRegex) and the editor tag-suggest extension.
// A valid tag path starts with an ASCII letter and contains only ASCII
// letters, digits, underscores, hyphens, and slashes.
var tagPathRegex = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_/-]*$`)

// IsValidTagPath checks that a tag path matches the canonical character
// contract (ASCII letter start, [a-zA-Z0-9_/-] thereafter) and fits within
// the per-tag byte budget. Used by RecordTagUsage IPC validation and
// normalizeRecentTags defense-in-depth.
func IsValidTagPath(tag string) bool {
	return len(tag) > 0 && len(tag) <= MaxTagPathBytes && tagPathRegex.MatchString(tag)
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
	// #632: migrate legacy per-plugin AI opt-in (plugins.disabled) into
	// ai.features once, then strip first-party AI ids from plugins.disabled
	// so Plugins tab no longer owns AI enablement.
	cfg = migrateAIFeaturesFromPlugins(cfg)
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
	cfg.UI.ExpandedSections = normalizeExpandedSections(cfg.UI.ExpandedSections)
	cfg.UI.RecentPages = normalizeRecentPages(cfg.UI.RecentPages)
	cfg.UI.RecentTags = normalizeRecentTags(cfg.UI.RecentTags)
	cfg.UI.Favorites = normalizeFavoritePages(cfg.UI.Favorites)
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
	// QuickAccessCollapsed: nil → true so older config files retain the
	// existing collapsed disclosure default.
	if cfg.UI.QuickAccessCollapsed == nil {
		cfg.UI.QuickAccessCollapsed = boolPtr(true)
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

// Normalize applies the same safe defaults and input cleanup used by Load and
// Save. It is exported for the App's serialized narrow mutation path.
func Normalize(cfg SystemConfig) SystemConfig { return normalize(cfg) }

func validNotebookPart(s string) bool {
	s = strings.TrimSpace(s)
	return s != "" && s != "." && s != ".." && !strings.ContainsAny(s, `/\\`) &&
		!strings.ContainsRune(s, 0) && strings.IndexFunc(s, func(r rune) bool { return r < 32 }) < 0
}

func validRelativeSectionPath(s string, allowEmpty bool) bool {
	if s == "" {
		return allowEmpty
	}
	parts := strings.Split(s, "/")
	for _, part := range parts {
		if !validNotebookPart(part) {
			return false
		}
	}
	return true
}

func normalizePageRef(ref NavigationPageRef) (NavigationPageRef, bool) {
	ref.Notebook = strings.TrimSpace(ref.Notebook)
	ref.Section = strings.TrimSpace(strings.ReplaceAll(ref.Section, `\`, "/"))
	ref.Page = strings.TrimSpace(ref.Page)
	if !validNotebookPart(ref.Notebook) || !validNotebookPart(ref.Page) || !validRelativeSectionPath(ref.Section, true) {
		return NavigationPageRef{}, false
	}
	return ref, true
}

func normalizeExpandedSections(in []NavigationSectionRef) []NavigationSectionRef {
	out := make([]NavigationSectionRef, 0, minInt(len(in), MaxExpandedSections))
	seen := make(map[string]struct{}, len(in))
	for _, ref := range in {
		ref.Notebook = strings.TrimSpace(ref.Notebook)
		ref.Path = strings.TrimSpace(strings.ReplaceAll(ref.Path, `\`, "/"))
		if !validNotebookPart(ref.Notebook) || !validRelativeSectionPath(ref.Path, false) {
			continue
		}
		key := ref.Notebook + "\x00" + ref.Path
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, ref)
		if len(out) == MaxExpandedSections {
			break
		}
	}
	return out
}

func normalizeFavoritePages(in []NavigationPageRef) []NavigationPageRef {
	out := make([]NavigationPageRef, 0, minInt(len(in), MaxFavoritePages))
	seen := make(map[string]struct{}, len(in))
	for _, ref := range in {
		ref, ok := normalizePageRef(ref)
		if !ok {
			continue
		}
		key := pageRefKey(ref)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, ref)
		if len(out) == MaxFavoritePages {
			break
		}
	}
	return out
}

func normalizeRecentPages(in []RecentPage) []RecentPage {
	latest := make(map[string]RecentPage, len(in))
	for _, recent := range in {
		ref, ok := normalizePageRef(recent.NavigationPageRef)
		if !ok || recent.OpenedAt <= 0 {
			continue
		}
		recent.NavigationPageRef = ref
		key := pageRefKey(ref)
		if prior, exists := latest[key]; !exists || recent.OpenedAt > prior.OpenedAt {
			latest[key] = recent
		}
	}
	out := make([]RecentPage, 0, len(latest))
	for _, recent := range latest {
		out = append(out, recent)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].OpenedAt != out[j].OpenedAt {
			return out[i].OpenedAt > out[j].OpenedAt
		}
		return pageRefKey(out[i].NavigationPageRef) < pageRefKey(out[j].NavigationPageRef)
	})
	if len(out) > MaxRecentPages {
		out = out[:MaxRecentPages]
	}
	return out
}

// normalizeRecentTags deduplicates case-insensitively, preserves the first
// occurrence's capitalization, and caps at MaxRecentTags. Empty/whitespace-only
// entries and entries that don't match the canonical tag-path character contract
// are dropped (defense-in-depth against corrupted or hand-edited config).
func normalizeRecentTags(in []string) []string {
	out := make([]string, 0, minInt(len(in), MaxRecentTags))
	seen := make(map[string]struct{}, len(in))
	for _, tag := range in {
		tag = strings.TrimSpace(tag)
		if tag == "" || !IsValidTagPath(tag) {
			continue
		}
		key := strings.ToLower(tag)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, tag)
		if len(out) == MaxRecentTags {
			break
		}
	}
	return out
}

func pageRefKey(ref NavigationPageRef) string {
	return ref.Notebook + "\x00" + ref.Section + "\x00" + ref.Page
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// aiFeaturesMigratedKey is a one-shot marker under plugin_settings so the
// plugins.disabled → ai.features migration (#632) runs exactly once per vault.
const aiFeaturesMigratedKey = "_ai_features_migrated"

// seededOptInDisabledKey is the legacy marker written by seedOptInDisabledPlugins
// (removed in #632). Presence proves the vault once tracked the full opt-in AI
// set, so absence from plugins.disabled is a real user enable — not a partial
// YAML list that never listed the other AI ids (e.g. only silt-ai-summary).
const seededOptInDisabledKey = "_seeded_opt_in_disabled"

// migrateAIFeaturesFromPlugins derives ai.features from the legacy per-plugin
// disabled list on first load after #632, then removes first-party AI ids from
// plugins.disabled. Subsequent loads leave features alone (user edits win).
//
// Mapping (plugin NOT in disabled ⇒ feature on), only when wasSeeded:
//   - silt-ai-agent or silt-ai-assistant enabled → Features.Enabled
//   - silt-ai-qa enabled → Features.RAGEnabled (+ Enabled)
//   - silt-ai-summary enabled → Features.SummariesEnabled (+ Enabled)
//
// Unseeded / partial disabled lists fall through to the safe all-off default
// so a vault with disabled: [silt-ai-summary] alone cannot silently enable AI.
func migrateAIFeaturesFromPlugins(cfg SystemConfig) SystemConfig {
	if cfg.Plugins.PluginSettings == nil {
		cfg.Plugins.PluginSettings = map[string]any{}
	}
	if cfg.Plugins.PluginSettings[aiFeaturesMigratedKey] == true {
		// Still strip any AI ids that reappeared in disabled (hand-edit / old UI).
		cfg.Plugins.Disabled = stripFirstPartyAIFromDisabled(cfg.Plugins.Disabled)
		return cfg
	}

	disabled := make(map[string]bool, len(cfg.Plugins.Disabled))
	for _, id := range cfg.Plugins.Disabled {
		disabled[id] = true
	}

	agentOn := !disabled[AIPluginAgent]
	assistantOn := !disabled[AIPluginAssistant]
	qaOn := !disabled[AIPluginQA]
	summaryOn := !disabled[AIPluginSummary]

	// If none of the four AI ids appear in disabled at all, this may be a
	// pre-AI-plugin config OR a post-#632 fresh vault. Prefer safe default
	// (features stay zero) — UNLESS the vault was seeded, in which case an
	// empty disabled list means the user deliberately removed every AI id.
	anyAIListed := disabled[AIPluginAgent] || disabled[AIPluginAssistant] ||
		disabled[AIPluginQA] || disabled[AIPluginSummary]
	_, wasSeeded := cfg.Plugins.PluginSettings[seededOptInDisabledKey]
	// Absence from disabled means "enabled" only when the vault previously
	// tracked the full opt-in set (seed marker). Partial unseeded listings
	// must not flip Features on.
	switch {
	case !wasSeeded:
		// Unseeded / partial listings cannot disambiguate "enabled" from
		// "never listed" — leave features at the safe all-off default.
	case !anyAIListed:
		// Seeded vault with no AI ids disabled ⇒ the user removed all four
		// from the legacy opt-out list, i.e. explicitly enabled every AI
		// plugin. Upgrading such a vault must not silently turn AI off.
		cfg.AI.Features.Enabled = true
		cfg.AI.Features.RAGEnabled = true
		cfg.AI.Features.SummariesEnabled = true
	default:
		// Seeded vault with at least one AI id still disabled: derive per
		// plugin (absence from disabled ⇒ feature on).
		if agentOn || assistantOn || qaOn || summaryOn {
			cfg.AI.Features.Enabled = true
		}
		if qaOn {
			cfg.AI.Features.Enabled = true
			cfg.AI.Features.RAGEnabled = true
		}
		if summaryOn {
			cfg.AI.Features.Enabled = true
			cfg.AI.Features.SummariesEnabled = true
		}
	}

	cfg.Plugins.Disabled = stripFirstPartyAIFromDisabled(cfg.Plugins.Disabled)
	cfg.Plugins.PluginSettings[aiFeaturesMigratedKey] = true
	return cfg
}

func stripFirstPartyAIFromDisabled(disabled []string) []string {
	if len(disabled) == 0 {
		return disabled
	}
	out := make([]string, 0, len(disabled))
	for _, id := range disabled {
		if IsFirstPartyAIPlugin(id) {
			continue
		}
		out = append(out, id)
	}
	return out
}
