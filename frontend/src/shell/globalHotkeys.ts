// Pure resolution of a global keydown → the single global action it should
// fire (or null). Extracted from App.svelte's handleGlobalKeyDown so the two
// load-bearing behaviors — the editor-focus guard and the first-match-wins
// mutual-exclusivity ordering — are unit-testable independent of the shell's
// reactive state and side effects.
//
// Side effects (toggling overlays, cycling tabs, etc.) stay in App.svelte,
// which calls resolveGlobalHotkey and switch-dispatches on the result. This
// mirrors the existing pure matchHotkey layer in settings/hotkeys.ts.
import { matchHotkey } from '../settings/hotkeys'

// A global action resolvable from a keyboard event. The order in resolveGlobalHotkey
// is load-bearing (first chord match wins).
export type GlobalHotkeyAction =
  | 'open_search'
  | 'find_in_page'
  | 'replace'
  | 'global_replace'
  | 'toggle_sidebar'
  | 'focus_sidebar'
  | 'cycle_view_layout'
  | 'open_template_picker'
  | 'new_task'
  | 'toggle_view_mode'
  | 'toggle_properties_panel'
  | 'toggle_format_toolbar'
  | 'toggle_focus_mode'
  | 'toggle_typewriter_mode'
  | 'open_settings'
  | 'next_tab'
  | 'prev_tab'
  | 'close_tab'
  | 'new_page'
  | 'new_section'
  | 'new_notebook'
  | 'open_quick_switcher'
  | 'open_shortcuts_help'
  | 'open_date_glance'
  | 'format_bold'

// Actions consumed by the editor's ProseMirror keymap when the contenteditable
// is focused. When the editor is focused and one of these chords is pressed,
// the global handler suppresses (returns null) so the editor can handle it and
// the two layers don't double-fire. toggle_view_mode is intentionally NOT
// here: no editor keymap handles it, so it stays global even while typing.
// indent_/unindent_ are editor keymap chords (defaults Tab / Shift-Tab);
// when remapped they must still suppress the global handler while focused.
//
// format_bold is editor-owned when the editor IS focused (ProseMirror applies
// bold natively), but when the editor is NOT focused it resolves globally so
// the dispatch layer can focus the active editor and apply bold — Ctrl+B is
// bold everywhere now that toggle_sidebar moved off it.
const EDITOR_OWNED_PREFIXES = [
  'format_',
  'set_',
  'align_',
  'indent_',
  'unindent_'
]

function isEditorOwned(action: string): boolean {
  return EDITOR_OWNED_PREFIXES.some((p) => action.startsWith(p))
}

// resolveGlobalHotkey returns the single global action a keydown should fire,
// or null when nothing applies. Ordering is load-bearing: actions are mutually
// exclusive and the first chord that matches wins, so two actions a user
// remapped to the same chord can't double-fire.
//
// editorFocused: whether the event target is inside a .ProseMirror surface.
// When true, an editor-owned chord (format/set/align) suppresses the global
// handler entirely (returns null) rather than falling through to the tab-strip
// fallback. The exception is format_bold, which the dispatch layer owns the
// "focus editor then apply" path for when the editor is not focused.
//
// hasDisplayedTabs gates the tab-strip fallback (next_tab/prev_tab/close_tab):
// those are no-ops when the active notebook has no open tabs (#142).
export function resolveGlobalHotkey(
  e: KeyboardEvent,
  hotkeys: Record<string, string | undefined>,
  editorFocused: boolean,
  hasDisplayedTabs: boolean
): GlobalHotkeyAction | null {
  if (e.isComposing || e.key === 'Process') return null
  if (editorFocused) {
    for (const [action, binding] of Object.entries(hotkeys)) {
      if (isEditorOwned(action) && matchHotkey(e, binding)) {
        // The editor will handle it; the global handler does nothing.
        return null
      }
    }
  }

  const ordered: GlobalHotkeyAction[] = [
    'open_search',
    'new_page',
    'new_section',
    'new_notebook',
    'open_quick_switcher',
    'open_shortcuts_help',
    'open_date_glance',
    'find_in_page',
    'replace',
    'global_replace',
    'toggle_sidebar',
    'focus_sidebar',
    'cycle_view_layout',
    'open_template_picker',
    'new_task',
    'toggle_view_mode',
    'toggle_properties_panel',
    'toggle_format_toolbar',
    'toggle_focus_mode',
    'toggle_typewriter_mode',
    'format_bold'
  ]
  for (const action of ordered) {
    if (matchHotkey(e, hotkeys[action])) {
      const target = e.target
      const proseMirror =
        target instanceof Element && !!target.closest('.ProseMirror')
      const editableControl =
        target instanceof Element &&
        !!target.closest('input, textarea, select, [contenteditable="true"]')
      const navigationAction = [
        'new_page',
        'new_section',
        'new_notebook',
        'open_quick_switcher',
        'open_shortcuts_help'
      ].includes(action)
      const plainTyping = !e.ctrlKey && !e.metaKey && !e.altKey
      if (proseMirror) {
        const editorGlobalAction = [
          'new_page',
          'new_section',
          'new_notebook',
          'open_quick_switcher'
        ].includes(action)
        if (plainTyping || (navigationAction && !editorGlobalAction))
          return null
      } else if (editableControl && (plainTyping || navigationAction)) {
        return null
      }
      return action
    }
  }
  // open_settings carries an explicit disabled-guard (an empty/undefined
  // binding must not fire) for fidelity with the original inline check;
  // matchHotkey(undefined/'' ) already returns false, but the guard documents
  // intent and is harmless.
  if (hotkeys.open_settings && matchHotkey(e, hotkeys.open_settings)) {
    return 'open_settings'
  }

  if (hasDisplayedTabs) {
    if (matchHotkey(e, hotkeys.next_tab)) return 'next_tab'
    if (matchHotkey(e, hotkeys.prev_tab)) return 'prev_tab'
    if (matchHotkey(e, hotkeys.close_tab)) return 'close_tab'
  }
  return null
}

// Pure decision for whether the dispatch layer should actually apply
// format_bold once resolveGlobalHotkey has handed it off. Extracted from
// App.svelte's applyFormatBold closure so the two safety properties — no-op
// while a modal dialog is open, and no-op when the recovered editor lives in
// a hidden (display:none) background tab — are unit-testable independent of
// the DOM. The caller computes isAnyDialogOpen (focus-inside-dialog check)
// and editorVisible (offsetParent !== null) from DOM/state and passes them
// in; this function stays free of side effects and DOM reads.
//
// activeView: only notes/backlinks mount an editor surface the dispatch can
//   recover, mirroring isPropertiesPanelAvailable. Dashboard/settings/etc. are
//   clean no-ops.
// isAnyDialogOpen: true when focus is inside a <dialog> or [role="dialog"].
//   Covers every modal (search, quick switcher, replace, quick add, template
//   picker, type editor, shortcut help, plus NamePrompt/Confirm/Choice/
//   SettingsMismatch dialogs) without enumerating each flag.
// editorVisible: false when the recovered editor's view.dom has offsetParent
//   === null (a hidden tab panel). getLastActiveEditor() survives blur, so
//   after a tab switch it can still point at the page the user left — guard
//   against mutating a page the user isn't looking at.
export function shouldApplyFormatBold(opts: {
  activeView: string
  isAnyDialogOpen: boolean
  editorVisible: boolean
}): boolean {
  if (opts.activeView !== 'notes' && opts.activeView !== 'backlinks')
    return false
  if (opts.isAnyDialogOpen) return false
  return opts.editorVisible
}
