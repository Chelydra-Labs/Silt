// Regression guard for issue #868: no two GLOBAL-scope hotkey actions in the
// resolved default set may share a chord, otherwise resolveGlobalHotkey's
// first-match-wins ordering becomes ambiguous and one action silently shadows
// another. The canonical defaults live in backend/config/defaults.go; the
// frontend keeps this mirror in sync (there is no Go→TS codegen for the
// hotkey table today).
//
// Editor-scoped actions (format_, set_, align_, indent_, unindent_,
// toggle_quote, toggle_details, table_) are EXCLUDED from the conflict check:
// they are consumed by the editor's ProseMirror keymap and never reach the
// global resolver while the editor is focused, so sharing a chord with a
// global action is resolved by focus — not a real conflict. The one exception
// is format_bold, which is ALSO global-resolvable when the editor is NOT
// focused (Ctrl+B is bold everywhere), so it stays in the checked set.
//
// tasks_command_palette is hub-scoped (handled by TasksHub's own keydown
// listener, not the global resolver) and is excluded; it shares Ctrl+K with
// the editor-scoped format_link, disambiguated by the hub's editable-target
// guard.
import { describe, expect, it } from 'vitest'

// Canonical global defaults — mirror of backend/config/defaults.go. Only the
// global-resolvable actions are listed; editor/hub-scoped actions are omitted
// (see file header).
const GLOBAL_DEFAULTS: Record<string, string> = {
  open_search: 'Ctrl+Shift+F',
  toggle_sidebar: 'Ctrl+\\',
  focus_sidebar: 'Ctrl+Shift+B',
  cycle_view_layout: 'Ctrl+Alt+V',
  open_settings: 'Ctrl+,',
  new_page: 'Ctrl+N',
  new_section: 'Ctrl+Alt+N',
  new_notebook: 'Ctrl+Alt+Shift+N',
  open_quick_switcher: 'Ctrl+P',
  open_shortcuts_help: 'Shift+?',
  open_template_picker: 'Ctrl+Shift+T',
  new_task: 'Ctrl+Shift+N',
  next_tab: 'Ctrl+Alt+Right',
  prev_tab: 'Ctrl+Alt+Left',
  close_tab: 'Ctrl+Shift+W',
  // format_bold is global-resolvable when the editor is not focused (Ctrl+B is
  // bold everywhere now that toggle_sidebar moved to Ctrl+\).
  format_bold: 'Ctrl+B',
  toggle_view_mode: 'Ctrl+Alt+R',
  toggle_format_toolbar: 'Ctrl+F1',
  toggle_focus_mode: 'Ctrl+Shift+D',
  find_in_page: 'Ctrl+F',
  replace: 'Ctrl+H',
  global_replace: 'Ctrl+Shift+G',
  toggle_typewriter_mode: 'Ctrl+Shift+Y',
  open_date_glance: 'Ctrl+Alt+D'
}

describe('default hotkey map has no global-scope chord conflicts (#868)', () => {
  it('no two global-resolvable actions share a chord', () => {
    const seen = new Map<string, string>() // normalized chord → first action
    for (const [action, chord] of Object.entries(GLOBAL_DEFAULTS)) {
      const normalized = chord.toLowerCase()
      const prior = seen.get(normalized)
      if (prior !== undefined) {
        // Two global actions on the same chord would race in
        // resolveGlobalHotkey's first-match-wins loop.
        throw new Error(
          `Chord conflict: ${prior} and ${action} both default to "${chord}"`
        )
      }
      seen.set(normalized, action)
    }
    // Sanity: the set is non-empty (guards against a future edit wiping the
    // map and making the loop trivially pass).
    expect(seen.size).toBe(Object.keys(GLOBAL_DEFAULTS).length)
    expect(seen.size).toBeGreaterThan(10)
  })
})
