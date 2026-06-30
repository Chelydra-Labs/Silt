import { describe, it, expect } from 'vitest'
import {
  configKeyToProseMirrorKey,
  resolveShortcut,
  resolveHotkeyDisplay
} from './hotkeys'

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

  it('returns the default when config entry is empty string', () => {
    const hotkeys = { toggle_quote: '' }
    expect(resolveShortcut('toggle_quote', 'Mod-Shift-9', hotkeys)).toBe(
      'Mod-Shift-9'
    )
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
