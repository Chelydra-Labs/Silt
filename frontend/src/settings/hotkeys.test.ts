import { describe, it, expect } from 'vitest'
import {
  configKeyToProseMirrorKey,
  resolveShortcut,
  resolveHotkeyDisplay,
  matchHotkey,
  formatHotkey,
  parseHotkey,
  hotkeyFromKeyboardEvent
} from './hotkeys'

describe('formatHotkey + parseHotkey round-trip (#519)', () => {
  it.each([
    'Ctrl+Shift+9',
    'Ctrl+Alt+2',
    'Meta+K',
    'Ctrl+/',
    'Ctrl+Shift+Escape',
    // Capture-widget long forms (KeyboardEvent.key) must round-trip (#521 review).
    'Ctrl+Shift+ArrowUp',
    'Ctrl+Shift+ArrowDown',
    'Ctrl+Space'
  ])('round-trips %s', (binding) => {
    const parsed = parseHotkey(binding)
    expect(parsed).not.toBeNull()
    const formatted = formatHotkey(parsed!)
    expect(parseHotkey(formatted)).toEqual(parsed)
  })

  it('formats with stable modifier order Ctrl+Alt+Shift+Meta', () => {
    expect(
      formatHotkey({
        ctrl: true,
        alt: true,
        shift: true,
        meta: true,
        key: 'p'
      })
    ).toBe('Ctrl+Alt+Shift+Meta+P')
  })

  it('formats Space as a named token (not a trailing space)', () => {
    expect(
      formatHotkey({
        ctrl: true,
        shift: false,
        alt: false,
        meta: false,
        key: ' '
      })
    ).toBe('Ctrl+Space')
    expect(parseHotkey('Ctrl+Space')).toEqual({
      ctrl: true,
      shift: false,
      alt: false,
      meta: false,
      key: ' '
    })
  })

  it('formats arrow keys as ArrowUp (not Arrowup)', () => {
    expect(
      formatHotkey({
        ctrl: true,
        shift: true,
        alt: false,
        meta: false,
        key: 'arrowup'
      })
    ).toBe('Ctrl+Shift+ArrowUp')
  })
})

describe('hotkeyFromKeyboardEvent (#519)', () => {
  function keyEvent(partial: Partial<KeyboardEvent>): KeyboardEvent {
    return {
      key: 'a',
      ctrlKey: false,
      shiftKey: false,
      altKey: false,
      metaKey: false,
      ...partial
    } as KeyboardEvent
  }

  it('returns null for pure modifier keys', () => {
    expect(hotkeyFromKeyboardEvent(keyEvent({ key: 'Control' }))).toBeNull()
    expect(hotkeyFromKeyboardEvent(keyEvent({ key: 'Shift' }))).toBeNull()
  })

  it('returns null for Dead and Unidentified (IME / international layouts)', () => {
    expect(hotkeyFromKeyboardEvent(keyEvent({ key: 'Dead' }))).toBeNull()
    expect(
      hotkeyFromKeyboardEvent(keyEvent({ key: 'Unidentified' }))
    ).toBeNull()
  })

  it('captures Ctrl+Shift+9', () => {
    const h = hotkeyFromKeyboardEvent(
      keyEvent({ key: '9', ctrlKey: true, shiftKey: true })
    )
    expect(h).toEqual({
      ctrl: true,
      shift: true,
      alt: false,
      meta: false,
      key: '9'
    })
    expect(formatHotkey(h!)).toBe('Ctrl+Shift+9')
  })

  it('captures ArrowUp and formats for PM conversion', () => {
    const h = hotkeyFromKeyboardEvent(
      keyEvent({ key: 'ArrowUp', ctrlKey: true, shiftKey: true })
    )
    expect(h?.key).toBe('arrowup')
    expect(formatHotkey(h!)).toBe('Ctrl+Shift+ArrowUp')
    expect(configKeyToProseMirrorKey(formatHotkey(h!))).toBe(
      'Mod-Shift-ArrowUp'
    )
  })

  it('captures Space as a named Space token', () => {
    const h = hotkeyFromKeyboardEvent(keyEvent({ key: ' ', ctrlKey: true }))
    expect(h?.key).toBe(' ')
    expect(formatHotkey(h!)).toBe('Ctrl+Space')
    expect(parseHotkey(formatHotkey(h!))).not.toBeNull()
  })
})

describe('configKeyToProseMirrorKey', () => {
  it('converts Ctrl+Shift+9 → Mod-Shift-9', () => {
    expect(configKeyToProseMirrorKey('Ctrl+Shift+9')).toBe('Mod-Shift-9')
  })

  it('converts Ctrl+Alt+1 → Mod-Alt-1', () => {
    expect(configKeyToProseMirrorKey('Ctrl+Alt+1')).toBe('Mod-Alt-1')
  })

  it('converts Ctrl+Shift+Up → Mod-Shift-ArrowUp', () => {
    expect(configKeyToProseMirrorKey('Ctrl+Shift+Up')).toBe('Mod-Shift-ArrowUp')
  })

  it('converts capture-widget long form Ctrl+Shift+ArrowUp → Mod-Shift-ArrowUp', () => {
    // formatHotkey emits ArrowUp; parseHotkey lowercases to arrowup; PM needs ArrowUp.
    expect(configKeyToProseMirrorKey('Ctrl+Shift+ArrowUp')).toBe(
      'Mod-Shift-ArrowUp'
    )
    expect(configKeyToProseMirrorKey('Ctrl+Shift+ArrowDown')).toBe(
      'Mod-Shift-ArrowDown'
    )
  })

  it('converts Ctrl+Space → Mod- (space key)', () => {
    expect(configKeyToProseMirrorKey('Ctrl+Space')).toBe('Mod- ')
  })

  it('converts Ctrl+Shift+Down → Mod-Shift-ArrowDown', () => {
    expect(configKeyToProseMirrorKey('Ctrl+Shift+Down')).toBe(
      'Mod-Shift-ArrowDown'
    )
  })

  it('converts Ctrl+Shift+Left → Mod-Shift-ArrowLeft', () => {
    expect(configKeyToProseMirrorKey('Ctrl+Shift+Left')).toBe(
      'Mod-Shift-ArrowLeft'
    )
  })

  it('converts Ctrl+Shift+Right → Mod-Shift-ArrowRight', () => {
    expect(configKeyToProseMirrorKey('Ctrl+Shift+Right')).toBe(
      'Mod-Shift-ArrowRight'
    )
  })

  it('converts Ctrl+Shift+. → Mod-Shift-.', () => {
    expect(configKeyToProseMirrorKey('Ctrl+Shift+.')).toBe('Mod-Shift-.')
  })

  it('converts Ctrl+B → Mod-b', () => {
    expect(configKeyToProseMirrorKey('Ctrl+B')).toBe('Mod-b')
  })

  it('converts Cmd+Shift+9 → Mod-Shift-9 (Mac notation)', () => {
    expect(configKeyToProseMirrorKey('Cmd+Shift+9')).toBe('Mod-Shift-9')
  })

  it('converts Ctrl+K → Mod-k', () => {
    expect(configKeyToProseMirrorKey('Ctrl+K')).toBe('Mod-k')
  })

  it('converts Ctrl+/ → Mod-/', () => {
    expect(configKeyToProseMirrorKey('Ctrl+/')).toBe('Mod-/')
  })

  it('returns empty string for empty input', () => {
    expect(configKeyToProseMirrorKey('')).toBe('')
  })

  it('returns empty string for null/undefined', () => {
    expect(configKeyToProseMirrorKey(null)).toBe('')
    expect(configKeyToProseMirrorKey(undefined)).toBe('')
  })

  it('returns empty string for input with no key segment', () => {
    expect(configKeyToProseMirrorKey('Ctrl+')).toBe('')
  })
})

describe('resolveShortcut', () => {
  it('returns the converted key when config entry is present', () => {
    const hotkeys = { toggle_quote: 'Ctrl+Q' }
    expect(resolveShortcut('toggle_quote', 'Mod-Shift-9', hotkeys)).toBe(
      'Mod-q'
    )
  })

  it('returns the default when config entry is absent', () => {
    const hotkeys = {}
    expect(resolveShortcut('toggle_quote', 'Mod-Shift-9', hotkeys)).toBe(
      'Mod-Shift-9'
    )
  })

  it('returns "" (disabled) when config entry is an empty string', () => {
    // "Leave empty to disable" (HotkeysTab): an explicitly cleared binding must
    // NOT restore the default. Callers drop the '' entry from the keymap.
    const hotkeys = { toggle_quote: '' }
    expect(resolveShortcut('toggle_quote', 'Mod-Shift-9', hotkeys)).toBe('')
  })

  it('returns the default when config entry is invalid', () => {
    const hotkeys = { toggle_quote: 'Ctrl+' }
    expect(resolveShortcut('toggle_quote', 'Mod-Shift-9', hotkeys)).toBe(
      'Mod-Shift-9'
    )
  })
})

describe('resolveHotkeyDisplay', () => {
  it('returns the configured binding in display form (no ProseMirror conversion)', () => {
    const hotkeys = { format_bold: 'Ctrl+B', toggle_quote: 'Ctrl+Shift+9' }
    expect(resolveHotkeyDisplay('format_bold', hotkeys)).toBe('Ctrl+B')
    expect(resolveHotkeyDisplay('toggle_quote', hotkeys)).toBe('Ctrl+Shift+9')
  })

  it('returns the binding verbatim for remapped actions', () => {
    // User remapped bold to Cmd+B — the display must reflect that, not the
    // shipped default. (This is the drift bug the refactor fixes.)
    const hotkeys = { format_bold: 'Cmd+B' }
    expect(resolveHotkeyDisplay('format_bold', hotkeys)).toBe('Cmd+B')
  })

  it('returns "" when the action is absent from the map', () => {
    expect(resolveHotkeyDisplay('format_bold', {})).toBe('')
    expect(
      resolveHotkeyDisplay('format_bold', { format_italic: 'Ctrl+I' })
    ).toBe('')
  })

  it('returns "" when the action is explicitly disabled (set to "")', () => {
    const hotkeys = { format_bold: '' }
    expect(resolveHotkeyDisplay('format_bold', hotkeys)).toBe('')
  })

  it('handles nullish map defensively', () => {
    expect(
      resolveHotkeyDisplay('format_bold', {} as Record<string, string>)
    ).toBe('')
  })
})

// The global quick-add overlay (#368) is dispatched by the App.svelte keydown
// handler via matchHotkey(e, hotkeys.new_task). The default binding is
// Ctrl+Shift+N — chosen because Ctrl+Shift+T is already open_template_picker.
// These tests pin the dispatch logic so a regression (e.g. reverting to the
// colliding Ctrl+Shift+T) is caught at the config layer.
describe('new_task hotkey dispatch (#368)', () => {
  const newTask = 'Ctrl+Shift+N'
  const templatePicker = 'Ctrl+Shift+T'

  function keyEvent(key: string, opts: KeyboardEventInit): KeyboardEvent {
    return new KeyboardEvent('keydown', { key, ...opts })
  }

  it('matches the default Ctrl+Shift+N binding', () => {
    expect(
      matchHotkey(keyEvent('n', { ctrlKey: true, shiftKey: true }), newTask)
    ).toBe(true)
  })

  it('does NOT match Ctrl+Shift+T (the template-picker binding)', () => {
    expect(
      matchHotkey(keyEvent('t', { ctrlKey: true, shiftKey: true }), newTask)
    ).toBe(false)
  })

  it('the template-picker binding still matches Ctrl+Shift+T (no collision)', () => {
    expect(
      matchHotkey(
        keyEvent('t', { ctrlKey: true, shiftKey: true }),
        templatePicker
      )
    ).toBe(true)
  })

  it('rejects when the action is disabled (empty binding)', () => {
    expect(
      matchHotkey(keyEvent('n', { ctrlKey: true, shiftKey: true }), '')
    ).toBe(false)
  })
})
