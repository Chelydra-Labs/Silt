import { describe, expect, it } from 'vitest'

import { resolveSettings, DEFAULT_SETTINGS } from './settings'

describe('resolveSettings', () => {
  it('returns defaults for a null/undefined raw', () => {
    expect(resolveSettings(undefined)).toEqual(DEFAULT_SETTINGS)
    expect(resolveSettings(null)).toEqual(DEFAULT_SETTINGS)
  })

  it('merges stored booleans over defaults', () => {
    const s = resolveSettings({ auto_on_open: false, on_demand_only: true })
    expect(s.auto_on_open).toBe(false)
    expect(s.on_demand_only).toBe(true)
  })

  it('normalizes the incoherent !auto_on_open && !on_demand_only to on-demand', () => {
    // Reachable via hand-edited config or a partial write. Without this, the
    // banner mounts but never generates → a perpetual skeleton with a disabled
    // Regenerate. On-demand is the honest fallback (the re-open chip shows).
    const s = resolveSettings({ auto_on_open: false, on_demand_only: false })
    expect(s.auto_on_open).toBe(false)
    expect(s.on_demand_only).toBe(true)
  })

  it('leaves the coherent default (auto on, not on-demand) untouched', () => {
    const s = resolveSettings({})
    expect(s.auto_on_open).toBe(true)
    expect(s.on_demand_only).toBe(false)
  })

  it('leaves on-demand-only untouched (auto_on_open is irrelevant then)', () => {
    const s = resolveSettings({ auto_on_open: true, on_demand_only: true })
    expect(s.auto_on_open).toBe(true)
    expect(s.on_demand_only).toBe(true)
  })

  it('filters non-string entries from dismissed_notes', () => {
    const s = resolveSettings({ dismissed_notes: ['a', 7, null, 'b'] })
    expect(s.dismissed_notes).toEqual(['a', 'b'])
  })
})
