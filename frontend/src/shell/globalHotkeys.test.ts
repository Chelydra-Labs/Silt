// Unit coverage for the pure global-hotkey resolver. The two load-bearing
// behaviors — the editor-focus guard and first-match-wins mutual exclusivity —
// were previously buried inline in App.svelte's handleGlobalKeyDown and had no
// direct test (App.svelte's component test only covers menu:save). These cases
// pin the contract the shell switch-dispatches on.
import { describe, expect, it } from 'vitest'
import { resolveGlobalHotkey } from './globalHotkeys'

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
  toggle_sidebar: 'Ctrl+B',
  focus_sidebar: 'Ctrl+Shift+B',
  cycle_view_layout: 'Ctrl+Alt+V',
  new_task: 'Ctrl+Shift+N',
  toggle_view_mode: 'Ctrl+Shift+V',
  toggle_format_toolbar: 'Ctrl+F1',
  toggle_focus_mode: 'Ctrl+Shift+D',
  toggle_typewriter_mode: 'Ctrl+Shift+Y',
  open_settings: 'Ctrl+,',
  next_tab: 'Ctrl+Tab',
  prev_tab: 'Ctrl+Shift+Tab',
  close_tab: 'Ctrl+W',
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

  it('resolves toggle_sidebar on Ctrl+B when not in the editor', () => {
    expect(
      resolveGlobalHotkey(key('b', { ctrlKey: true }), defaults, false, false)
    ).toBe('toggle_sidebar')
  })

  it('suppresses Ctrl+B inside the editor (format_bold owns it)', () => {
    // The famous collision: Ctrl+B is toggle_sidebar globally AND format_bold in
    // the editor. Editor-focused must yield null so the editor handles it.
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

  it('still fires global actions while the editor is focused', () => {
    // toggle_view_mode is intentionally NOT editor-owned (#171/#195).
    expect(
      resolveGlobalHotkey(
        key('V', { ctrlKey: true, shiftKey: true }),
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
    const close = key('w', { ctrlKey: true })
    expect(resolveGlobalHotkey(close, defaults, false, true)).toBe('close_tab')
    expect(resolveGlobalHotkey(close, defaults, false, false)).toBe(null)
  })

  it('resolves next_tab / prev_tab only when tabs are displayed', () => {
    expect(
      resolveGlobalHotkey(key('Tab', { ctrlKey: true }), defaults, false, true)
    ).toBe('next_tab')
    expect(
      resolveGlobalHotkey(
        key('Tab', { ctrlKey: true, shiftKey: true }),
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
