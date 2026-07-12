import { describe, expect, it } from 'vitest'
import { DEFAULT_SETTINGS, resolveSettings } from './settings'

describe('resolveSettings', () => {
  it('returns defaults for empty input', () => {
    expect(resolveSettings(null)).toEqual(DEFAULT_SETTINGS)
  })

  it('merges known keys and clamps hybrid weight', () => {
    const s = resolveSettings({
      hybrid_weight: 0.8,
      top_k: 12,
      notebook_scope: ['Work', ''],
      auto_reembed: false
    })
    expect(s.hybrid_weight).toBe(0.8)
    expect(s.top_k).toBe(12)
    expect(s.notebook_scope).toEqual(['Work'])
    expect(s.auto_reembed).toBe(false)
  })

  it('ignores out-of-range hybrid weight', () => {
    expect(resolveSettings({ hybrid_weight: 2 }).hybrid_weight).toBe(
      DEFAULT_SETTINGS.hybrid_weight
    )
  })
})
