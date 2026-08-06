// Regression guard for issue #868: no two GLOBAL-scope hotkey action in the
// resolved default set may share a chord, otherwise resolveGlobalHotkey's
// first-match-wins ordering becomes ambiguous and one action silently shadows
// another.
//
// The checked set is derived from `effectiveHotkeys({})` (the real resolver's
// defaults map: SHORTCUT_ACTIONS plus any backend-provided overrides), filtered
// to global-resolvable actions via `shortcutScope` — the single frontend source
// of truth for the scope split (shared with the HotkeysTab conflict grid).
// There is NO hand-maintained mirror: any action added to SHORTCUT_ACTIONS is
// automatically covered, and `toggle_properties_panel` (Ctrl+;, genuinely
// global-resolvable) is included by construction.
//
// Editor-scoped actions (format_, set_, align_, indent_, unindent_,
// table_, toggle_quote, toggle_details, toggle_bullet_list,
// toggle_ordered_list) are EXCLUDED by shortcutScope: they are consumed by
// the editor's ProseMirror keymap and never reach the global resolver while the
// editor is focused, so sharing a chord with a global action is resolved by
// focus — not a real conflict. The one exception is format_bold, which is ALSO
// global-resolvable when the editor is NOT focused (Ctrl+B is bold
// everywhere), so shortcutScope returns 'global' for it.
//
// tasks_command_palette is hub-scoped (handled by TasksHub's own keydown
// listener, not the global resolver) and is excluded; it shares Ctrl+K with
// the editor-scoped format_link, disambiguated by the hub's editable-target
// guard.
import { describe, expect, it } from 'vitest'
import { effectiveHotkeys, shortcutScope } from '../settings/shortcutActions'

describe('default hotkey map has no global-scope chord conflicts (#868)', () => {
  it('no two global-resolvable actions share a chord', () => {
    // Derive the checked set from the REAL resolver defaults map instead of a
    // hand-maintained mirror. Any action added to SHORTCUT_ACTIONS is
    // automatically covered.
    const resolved = effectiveHotkeys({})
    const seen = new Map<string, string>() // normalized chord → first action
    for (const [action, chord] of Object.entries(resolved)) {
      if (shortcutScope(action) !== 'global') continue
      if (!chord) continue // explicitly disabled — never conflicts
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
    // map or re-classifying every action as editor-scoped, either of which
    // would make the loop trivially pass).
    expect(seen.size).toBeGreaterThan(10)
  })

  it('toggle_properties_panel is covered (was missing from the old mirror)', () => {
    // Pin the specific finding-#6 regression: toggle_properties_panel
    // (Ctrl+;) is genuinely global-resolvable, so the conflict check must
    // include it. The chord is in `resolved` and shortcutScope returns
    // 'global' for it, so a duplicate of Ctrl+; anywhere else in the global
    // set would now fail the test above.
    const resolved = effectiveHotkeys({})
    expect(resolved.toggle_properties_panel).toBe('Ctrl+;')
    expect(shortcutScope('toggle_properties_panel')).toBe('global')
  })
})
