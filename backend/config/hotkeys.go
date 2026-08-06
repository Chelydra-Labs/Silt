package config

import (
	"strings"
)

// isEditorOrHubScopedAction reports whether a hotkey action is resolved by the
// editor's ProseMirror keymap (editor-scoped) or the TasksHub keydown listener
// (hub-scoped) rather than the global resolver. Those actions may share a chord
// with a global action — focus/scope disambiguates them — so they are excluded
// from the defaults-uniqueness invariant check
// (TestDefaults_NoGlobalHotkeyChordConflict), which mirrors the frontend's
// defaults.noconflict.test.ts over the canonical Defaults() map.
//
// Mirrors the EDITOR_OWNED_PREFIXES classification in the frontend resolver
// (frontend/src/shell/globalHotkeys.ts) and the shortcutScope split in
// frontend/src/settings/shortcutActions.ts. The layers must agree so a chord
// the backend treats as scoped is one the frontend resolver would actually
// dispatch in a non-global context, and vice-versa.
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
