import { describe, expect, it } from 'vitest'
import { effectiveHotkeys, shortcutBinding } from './shortcutActions'

describe('shortcut action metadata', () => {
  it('supplies frontend defaults for new actions missing from older configs', () => {
    expect(effectiveHotkeys({}).new_page).toBe('Ctrl+N')
    expect(effectiveHotkeys({}).open_shortcuts_help).toBe('Shift+?')
    expect(effectiveHotkeys({}).open_date_glance).toBe('Ctrl+Alt+D')
  })

  it('preserves current remapped and explicitly disabled values', () => {
    const configured = { new_page: 'Alt+N', open_shortcuts_help: '' }
    expect(shortcutBinding('new_page', configured)).toBe('Alt+N')
    expect(shortcutBinding('open_shortcuts_help', configured)).toBe('')
    expect(effectiveHotkeys(configured).open_shortcuts_help).toBe('')
  })
})
