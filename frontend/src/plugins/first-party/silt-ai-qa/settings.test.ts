import { describe, expect, it } from 'vitest'
import { DEFAULT_SETTINGS, resolveSettings } from './settings'

describe('resolveSettings', () => {
  it('returns defaults for empty input', () => {
    expect(resolveSettings(null)).toEqual(DEFAULT_SETTINGS)
    expect(DEFAULT_SETTINGS.top_k).toBe(10)
    expect(DEFAULT_SETTINGS.max_context_chars).toBe(24000)
    expect(DEFAULT_SETTINGS.stale_reason).toBeNull()
    expect(DEFAULT_SETTINGS.rerank_enabled).toBe(false)
  })

  it('merges known keys and clamps hybrid weight', () => {
    const s = resolveSettings({
      hybrid_weight: 0.8,
      top_k: 12,
      notebook_scope: ['Work', ''],
      auto_reembed: false // legacy key ignored (#850)
    })
    expect(s.hybrid_weight).toBe(0.8)
    expect(s.top_k).toBe(12)
    expect(s.notebook_scope).toEqual(['Work'])
    expect(s).not.toHaveProperty('auto_reembed')
  })

  it('ignores out-of-range hybrid weight', () => {
    expect(resolveSettings({ hybrid_weight: 2 }).hybrid_weight).toBe(
      DEFAULT_SETTINGS.hybrid_weight
    )
  })

  it('accepts top_k up to 100 and max_context_chars from 1000', () => {
    expect(resolveSettings({ top_k: 100 }).top_k).toBe(100)
    expect(resolveSettings({ top_k: 101 }).top_k).toBe(DEFAULT_SETTINGS.top_k)
    expect(resolveSettings({ max_context_chars: 1000 }).max_context_chars).toBe(
      1000
    )
    expect(resolveSettings({ max_context_chars: 500 }).max_context_chars).toBe(
      DEFAULT_SETTINGS.max_context_chars
    )
  })

  it('resolves stale_reason and rerank_enabled', () => {
    expect(
      resolveSettings({ stale_reason: 'Model changed' }).stale_reason
    ).toBe('Model changed')
    expect(resolveSettings({ stale_reason: null }).stale_reason).toBeNull()
    expect(resolveSettings({ rerank_enabled: true }).rerank_enabled).toBe(true)
  })
})
