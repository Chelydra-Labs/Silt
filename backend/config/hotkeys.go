package config

import (
	"sort"
	"strings"
)

// isEditorOrHubScopedAction reports whether a hotkey action is resolved by the
// editor's ProseMirror keymap (editor-scoped) or the TasksHub keydown listener
// (hub-scoped) rather than the global resolver. Those actions may share a chord
// with a global action — focus/scope disambiguates them — so they are excluded
// from any global conflict check (defaults uniqueness invariant, post-migration
// duplicate scan, ValidateHotkeys cross-action check).
//
// Mirrors the EDITOR_OWNED_PREFIXES classification in the frontend resolver
// (frontend/src/shell/globalHotkeys.ts). The two layers must agree so a chord
// the backend flags as a conflict is one the frontend resolver would actually
// dispatch ambiguously (first-match-wins), and vice-versa.
//
// format_bold is the ONE format_ action that is also global-resolvable (Ctrl+B
// is bold everywhere, including when the editor is unfocused), so it is NOT
// editor-scoped for the purposes of this check.
func isEditorOrHubScopedAction(action string) bool {
	if action == "format_bold" {
		return false
	}
	switch {
	case strings.HasPrefix(action, "format_"),
		strings.HasPrefix(action, "set_"),
		strings.HasPrefix(action, "align_"),
		strings.HasPrefix(action, "indent_"),
		strings.HasPrefix(action, "unindent_"),
		strings.HasPrefix(action, "table_"):
		return true
	}
	switch action {
	case "toggle_quote",
		"toggle_details",
		"tasks_command_palette":
		return true
	}
	return false
}

// FindGlobalHotkeyConflicts returns the global-resolvable actions that share a
// normalized chord with at least one other global-resolvable action. Used by
// the post-migration duplicate scan in normalize() to surface vaults where a
// deliberate user remap (e.g. focus_sidebar=Ctrl+B) collides with a default
// that the v1 migration made global-resolvable (format_bold=Ctrl+B). Returns a
// map of normalized chord → the actions sharing it (sorted for determinism),
// limited to chords owned by 2+ global actions. Empty map when there are no
// global conflicts.
//
// Editor/hub-scoped actions are excluded (focus disambiguates them), and an
// explicitly-disabled binding ("") never conflicts. Normalization is
// case-insensitive only (no fuzzy matching) so the result agrees with the
// frontend's first-match-wins resolver, which compares matchHotkey on the raw
// binding string.
func FindGlobalHotkeyConflicts(hotkeys map[string]string) map[string][]string {
	actions := make([]string, 0, len(hotkeys))
	for action := range hotkeys {
		actions = append(actions, action)
	}
	sort.Strings(actions)
	byChord := make(map[string][]string)
	for _, action := range actions {
		if isEditorOrHubScopedAction(action) {
			continue
		}
		raw := strings.TrimSpace(hotkeys[action])
		if raw == "" {
			continue
		}
		normalized := strings.ToLower(raw)
		byChord[normalized] = append(byChord[normalized], action)
	}
	conflicts := make(map[string][]string)
	for chord, owners := range byChord {
		if len(owners) > 1 {
			conflicts[chord] = owners
		}
	}
	return conflicts
}
