// Unit coverage for the pure global-hotkey resolver. The two load-bearing
// behaviors — the editor-focus guard and first-match-wins mutual exclusivity —
// were previously buried inline in App.svelte's handleGlobalKeyDown and had no
// direct test (App.svelte's component test only covers menu:save). These cases
// pin the contract the shell switch-dispatches on.
import { describe, expect, it } from 'vitest'
import { resolveGlobalHotkey, shouldApplyFormatBold } from './globalHotkeys'

// Build a keydown with modifier flags. key is the logical glyph (matchHotkey
// lowercases it, so case is irrelevant).
function key(key: string, mods: KeyboardEventInit = {}): KeyboardEvent {
  return new KeyboardEvent('keydown', { key, bubbles: true, ...mods })
}

// A representative subset of the config defaults (see backend/config/defaults.go).
const defaults: Record<string, string> = {
  open_search: 'Ctrl+Shift+F',
  find_in_page: 'Ctrl+F',
  replace: 'Ctrl+H',
  toggle_sidebar: 'Ctrl+\\',
  focus_sidebar: 'Ctrl+Shift+B',
  cycle_view_layout: 'Ctrl+Alt+V',
  new_task: 'Ctrl+Shift+N',
  toggle_view_mode: 'Ctrl+Alt+R',
  toggle_format_toolbar: 'Ctrl+F1',
  toggle_focus_mode: 'Ctrl+Shift+D',
  toggle_typewriter_mode: 'Ctrl+Shift+Y',
  open_settings: 'Ctrl+,',
  next_tab: 'Ctrl+Alt+Right',
  prev_tab: 'Ctrl+Alt+Left',
  close_tab: 'Ctrl+Shift+W',
  new_page: 'Ctrl+N',
  new_section: 'Ctrl+Alt+N',
  new_notebook: 'Ctrl+Alt+Shift+N',
  open_quick_switcher: 'Ctrl+P',
  open_shortcuts_help: 'Shift+?',
  // editor-owned (only matter for the focus guard):
  format_bold: 'Ctrl+B',
  set_h1: 'Ctrl+Alt+1',
  align_left: 'Ctrl+Shift+L'
}

describe('resolveGlobalHotkey', () => {
  it('resolves a global action when the editor is not focused', () => {
    expect(
      resolveGlobalHotkey(
        key('F', { ctrlKey: true, shiftKey: true }),
        defaults,
        false,
        false
      )
    ).toBe('open_search')
  })

  it('resolves format_bold on Ctrl+B when not in the editor', () => {
    // Ctrl+B is bold everywhere now that toggle_sidebar moved to Ctrl+\. When
    // the editor is not focused, the resolver returns format_bold so the
    // dispatch can focus the active editor and apply bold.
    expect(
      resolveGlobalHotkey(key('b', { ctrlKey: true }), defaults, false, false)
    ).toBe('format_bold')
  })

  it('suppresses Ctrl+B inside the editor (ProseMirror owns it)', () => {
    // When the editor is focused, the editor-owned suppression yields null so
    // ProseMirror applies bold natively (no double-fire).
    expect(
      resolveGlobalHotkey(key('b', { ctrlKey: true }), defaults, true, false)
    ).toBe(null)
  })

  it('suppresses set_/align_ chords inside the editor too', () => {
    expect(
      resolveGlobalHotkey(
        key('1', { ctrlKey: true, altKey: true }),
        defaults,
        true,
        false
      )
    ).toBe(null) // set_h1
    expect(
      resolveGlobalHotkey(
        key('L', { ctrlKey: true, shiftKey: true }),
        defaults,
        true,
        false
      )
    ).toBe(null) // align_left
  })

  it('suppresses remapped indent_block inside the editor', () => {
    // If indent is remapped onto a chord that is also a global action, the
    // editor-owned prefix must win while ProseMirror is focused.
    const hotkeys = {
      ...defaults,
      indent_block: 'Ctrl+B',
      format_bold: 'Ctrl+Shift+B'
    }
    expect(
      resolveGlobalHotkey(key('b', { ctrlKey: true }), hotkeys, true, false)
    ).toBe(null)
  })

  describe('#898 — scope-aware suppression matches shortcutScope', () => {
    // Regression: the conflict grid (shortcutScope) classifies table_* and the
    // exact-name toggle_* actions as editor-scoped, so it permits them to share
    // a chord with a global action (resolved by focus, not flagged as a
    // conflict). For that to be sound, the runtime resolver MUST suppress the
    // global handler for the same action set when the editor is focused —
    // otherwise both the editor keymap and the global handler fire (double-
    // fire). These cases pin the agreement: every action shortcutScope treats
    // as editor-scoped is one isEditorOwned suppresses, and vice-versa.
    //
    // Each case remaps an editor-scoped action onto open_search's chord
    // (Ctrl+Shift+F) and asserts: focused → null (editor wins, no double-fire),
    // unfocused → open_search (global still resolves).

    const cases: Array<[string, KeyboardEvent]> = [
      // Prefix-based editor ownership (table_ was the headline missing entry).
      ['table_insert_row_above', key('F', { ctrlKey: true, shiftKey: true })],
      ['table_insert_col_right', key('F', { ctrlKey: true, shiftKey: true })],
      // Exact-name editor ownership (no consistent prefix).
      ['toggle_quote', key('F', { ctrlKey: true, shiftKey: true })],
      ['toggle_bullet_list', key('F', { ctrlKey: true, shiftKey: true })],
      ['toggle_ordered_list', key('F', { ctrlKey: true, shiftKey: true })],
      ['toggle_details', key('F', { ctrlKey: true, shiftKey: true })],
      // format_bold stays editor-owned while focused even though shortcutScope
      // returns 'global' for it (it is also global-resolvable when unfocused).
      ['format_bold', key('F', { ctrlKey: true, shiftKey: true })]
    ]

    it.each(cases)(
      'suppresses the shared chord for editor-scoped %s when focused (no double-fire)',
      (action, event) => {
        const hotkeys = {
          ...defaults,
          [action]: 'Ctrl+Shift+F' // collide with open_search
        }
        expect(resolveGlobalHotkey(event, hotkeys, true, false)).toBe(null)
      }
    )

    it.each(cases)(
      'still fires the global action for %s collision when editor NOT focused',
      (action, event) => {
        const hotkeys = {
          ...defaults,
          [action]: 'Ctrl+Shift+F' // collide with open_search
        }
        // Editor not focused → the editor-scoped action is irrelevant and the
        // global action resolves normally.
        expect(resolveGlobalHotkey(event, hotkeys, false, false)).toBe(
          'open_search'
        )
      }
    )

    it('the editor-scoped action itself never resolves globally (it is not a GlobalHotkeyAction)', () => {
      // Belt-and-suspenders: an editor-scoped action sharing a chord with a
      // global one can't itself be returned by resolveGlobalHotkey because it
      // isn't a member of GlobalHotkeyAction, so even if suppression failed it
      // would fall through to open_search — never double-fire as the editor
      // action. This pins the type-level guarantee the no-double-fire property
      // rests on.
      const hotkeys = {
        ...defaults,
        table_insert_row_above: 'Ctrl+Shift+F'
      }
      // Unfocused, with no colliding global action on this chord: the editor
      // action is unknown to the resolver, so nothing fires.
      const noCollision = { ...hotkeys, open_search: 'Ctrl+Shift+G' }
      expect(
        resolveGlobalHotkey(
          key('F', { ctrlKey: true, shiftKey: true }),
          noCollision,
          false,
          false
        )
      ).toBe(null)
    })
  })

  it('still fires global actions while the editor is focused', () => {
    // toggle_view_mode is intentionally NOT editor-owned; it now lives on
    // Ctrl+Alt+R (relocated off Ctrl+Shift+V to avoid the OS paste-plain /
    // TasksHub display-cycle triple-fire).
    expect(
      resolveGlobalHotkey(
        key('r', { ctrlKey: true, altKey: true }),
        defaults,
        true,
        false
      )
    ).toBe('toggle_view_mode')
    // And search still works while typing.
    expect(
      resolveGlobalHotkey(
        key('F', { ctrlKey: true, shiftKey: true }),
        defaults,
        true,
        false
      )
    ).toBe('open_search')
  })

  it('first matching action wins (mutual exclusivity)', () => {
    // Remap two actions to the same chord; the earlier one in the ordered
    // chain must win so they can't double-fire.
    const remapped = {
      ...defaults,
      open_search: 'Ctrl+Shift+F',
      find_in_page: 'Ctrl+Shift+F'
    }
    expect(
      resolveGlobalHotkey(
        key('F', { ctrlKey: true, shiftKey: true }),
        remapped,
        false,
        false
      )
    ).toBe('open_search')
  })

  it('treats an empty (disabled) open_settings binding as never firing', () => {
    const disabled = { ...defaults, open_settings: '' }
    expect(
      resolveGlobalHotkey(key(',', { ctrlKey: true }), disabled, false, false)
    ).toBe(null)
  })

  it('returns null for an unmapped chord', () => {
    expect(
      resolveGlobalHotkey(key('z', { ctrlKey: true }), defaults, false, false)
    ).toBe(null)
  })

  it('gates the tab-strip fallback on hasDisplayedTabs', () => {
    const close = key('w', { ctrlKey: true, shiftKey: true })
    expect(resolveGlobalHotkey(close, defaults, false, true)).toBe('close_tab')
    expect(resolveGlobalHotkey(close, defaults, false, false)).toBe(null)
  })

  it('resolves next_tab / prev_tab only when tabs are displayed', () => {
    expect(
      resolveGlobalHotkey(
        key('ArrowRight', { ctrlKey: true, altKey: true }),
        defaults,
        false,
        true
      )
    ).toBe('next_tab')
    expect(
      resolveGlobalHotkey(
        key('ArrowLeft', { ctrlKey: true, altKey: true }),
        defaults,
        false,
        true
      )
    ).toBe('prev_tab')
  })

  it('resolves creation, switcher, and help actions', () => {
    expect(
      resolveGlobalHotkey(key('n', { ctrlKey: true }), defaults, false, false)
    ).toBe('new_page')
    expect(
      resolveGlobalHotkey(
        key('n', { ctrlKey: true, altKey: true }),
        defaults,
        false,
        false
      )
    ).toBe('new_section')
    expect(
      resolveGlobalHotkey(key('p', { ctrlKey: true }), defaults, false, false)
    ).toBe('open_quick_switcher')
    expect(
      resolveGlobalHotkey(key('?', { shiftKey: true }), defaults, false, false)
    ).toBe('open_shortcuts_help')
  })

  it('ignores composition and navigation actions in editable controls', () => {
    const composing = key('n', { ctrlKey: true, isComposing: true })
    expect(resolveGlobalHotkey(composing, defaults, false, false)).toBeNull()

    const input = document.createElement('input')
    let result: ReturnType<typeof resolveGlobalHotkey> = 'new_page'
    input.addEventListener('keydown', (event) => {
      result = resolveGlobalHotkey(event, defaults, false, false)
    })
    input.dispatchEvent(key('n', { ctrlKey: true }))
    expect(result).toBeNull()
    input.dispatchEvent(key('?', { shiftKey: true }))
    expect(result).toBeNull()
  })

  it.each(['input', 'textarea', 'select'] as const)(
    'suppresses creation and switcher shortcuts in %s controls',
    (tag) => {
      const control = document.createElement(tag)
      const resolve = (event: KeyboardEvent) =>
        resolveGlobalHotkey(event, defaults, false, false)

      for (const event of [
        key('n', { ctrlKey: true }),
        key('n', { ctrlKey: true, altKey: true }),
        key('n', { ctrlKey: true, altKey: true, shiftKey: true }),
        key('p', { ctrlKey: true })
      ]) {
        Object.defineProperty(event, 'target', { value: control })
        expect(resolve(event)).toBeNull()
      }
    }
  )

  it('suppresses navigation shortcuts in a non-editor contenteditable', () => {
    const control = document.createElement('div')
    control.setAttribute('contenteditable', 'true')
    const event = key('p', { ctrlKey: true })
    Object.defineProperty(event, 'target', { value: control })

    expect(resolveGlobalHotkey(event, defaults, false, false)).toBeNull()
  })

  it('allows modifier creation and switcher shortcuts in ProseMirror but suppresses typing', () => {
    const editor = document.createElement('div')
    editor.className = 'ProseMirror'
    editor.contentEditable = 'true'
    const paragraph = document.createElement('p')
    editor.append(paragraph)

    const resolveFromEditor = (event: KeyboardEvent) => {
      Object.defineProperty(event, 'target', { value: paragraph })
      return resolveGlobalHotkey(event, defaults, true, false)
    }

    expect(resolveFromEditor(key('n', { ctrlKey: true }))).toBe('new_page')
    expect(resolveFromEditor(key('n', { ctrlKey: true, altKey: true }))).toBe(
      'new_section'
    )
    expect(
      resolveFromEditor(
        key('n', { ctrlKey: true, altKey: true, shiftKey: true })
      )
    ).toBe('new_notebook')
    expect(resolveFromEditor(key('p', { ctrlKey: true }))).toBe(
      'open_quick_switcher'
    )
    expect(resolveFromEditor(key('?', { shiftKey: true }))).toBeNull()
  })

  it('honors explicit disabling and remapping for new actions', () => {
    const configured = {
      ...defaults,
      new_page: '',
      open_quick_switcher: 'Alt+O'
    }
    expect(
      resolveGlobalHotkey(key('n', { ctrlKey: true }), configured, false, false)
    ).toBeNull()
    expect(
      resolveGlobalHotkey(key('o', { altKey: true }), configured, false, false)
    ).toBe('open_quick_switcher')
  })
})

describe('shouldApplyFormatBold', () => {
  // Pins the four gating dimensions the dispatch-layer closure relied on:
  // view, dialog presence, editor existence, and editor visibility. The
  // predicate itself is pure — the caller computes isAnyDialogOpen and
  // editorVisible from DOM/state — so these cases don't touch the DOM.

  it('returns false for a non-editor view (dashboard)', () => {
    expect(
      shouldApplyFormatBold({
        activeView: 'dashboard',
        isAnyDialogOpen: false,
        editorVisible: true
      })
    ).toBe(false)
  })

  it('returns true on notes view with no dialog and a visible editor', () => {
    expect(
      shouldApplyFormatBold({
        activeView: 'notes',
        isAnyDialogOpen: false,
        editorVisible: true
      })
    ).toBe(true)
  })

  it('returns true on backlinks view with no dialog and a visible editor', () => {
    expect(
      shouldApplyFormatBold({
        activeView: 'backlinks',
        isAnyDialogOpen: false,
        editorVisible: true
      })
    ).toBe(true)
  })

  it('returns false when a dialog is open', () => {
    expect(
      shouldApplyFormatBold({
        activeView: 'notes',
        isAnyDialogOpen: true,
        editorVisible: true
      })
    ).toBe(false)
  })

  it('returns false when the editor is in a hidden background tab', () => {
    // offsetParent === null on the recovered editor — the user isn't looking
    // at the page getLastActiveEditor() would mutate.
    expect(
      shouldApplyFormatBold({
        activeView: 'notes',
        isAnyDialogOpen: false,
        editorVisible: false
      })
    ).toBe(false)
  })

  it('returns false when no editor could be recovered', () => {
    // The caller maps an absent editor to editorVisible: false; combined with
    // the predicate this keeps the absent-editor case a clean no-op.
    expect(
      shouldApplyFormatBold({
        activeView: 'notes',
        isAnyDialogOpen: false,
        editorVisible: false
      })
    ).toBe(false)
  })

  it('returns false for a hidden editor even on backlinks', () => {
    expect(
      shouldApplyFormatBold({
        activeView: 'backlinks',
        isAnyDialogOpen: false,
        editorVisible: false
      })
    ).toBe(false)
  })

  it('returns false when both a dialog is open and the editor is hidden', () => {
    expect(
      shouldApplyFormatBold({
        activeView: 'notes',
        isAnyDialogOpen: true,
        editorVisible: false
      })
    ).toBe(false)
  })
})
