import { describe, expect, it } from 'vitest'
import { DEFAULT_SETTINGS, resolveSettings } from './settings'

describe('resolveSettings', () => {
  it('returns defaults for null', () => {
    const s = resolveSettings(null)
    expect(s.max_input_chars).toBe(DEFAULT_SETTINGS.max_input_chars)
    expect(s.actions_enabled['draft-expand']).toBe(true)
    expect(s.existing_vocab_only).toBe(true)
  })

  it('merges action toggles and overrides', () => {
    const s = resolveSettings({
      actions_enabled: { 'draft-expand': false },
      max_tag_suggestions: 3,
      prompt_overrides: { 'improve-clarity': 'Be terse.' }
    })
    expect(s.actions_enabled['draft-expand']).toBe(false)
    expect(s.actions_enabled['rewrite-succinct']).toBe(true)
    expect(s.max_tag_suggestions).toBe(3)
    expect(s.prompt_overrides['improve-clarity']).toBe('Be terse.')
  })

  it('ignores invalid numbers', () => {
    const s = resolveSettings({ max_input_chars: -1, max_tag_suggestions: 0 })
    expect(s.max_input_chars).toBe(DEFAULT_SETTINGS.max_input_chars)
    expect(s.max_tag_suggestions).toBe(DEFAULT_SETTINGS.max_tag_suggestions)
  })
})
