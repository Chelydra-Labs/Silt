import { describe, expect, it } from 'vitest'
import { buildDueDatePresets } from './dueDatePresets'

describe('buildDueDatePresets', () => {
  it('resolves all presets against a Sunday-start week and month rollover', () => {
    // 2026-01-31 is both the end of the week and the end of the month, so
    // those shortcuts collide with "Today" and are dropped (first wins).
    expect(buildDueDatePresets('2026-01-31', 'sunday')).toEqual([
      { label: 'Today', value: '2026-01-31' },
      { label: 'Tomorrow', value: '2026-02-01' },
      { label: 'End of next week', value: '2026-02-07' },
      { label: 'End of next month', value: '2026-02-28' }
    ])
  })

  it('drops "End of week" when it collides with "Tomorrow" (weekly case)', () => {
    // 2026-11-13 is the week's second-to-last day: "Tomorrow" and "End of
    // week" both resolve to 2026-11-14. Dates must stay unique — the context
    // menu keys rows and testids by value (regression: ListView context menu
    // each_key_duplicate on 2026-11-12).
    const presets = buildDueDatePresets('2026-11-13', 'sunday')
    expect(presets).toEqual([
      { label: 'Today', value: '2026-11-13' },
      { label: 'Tomorrow', value: '2026-11-14' },
      { label: 'End of next week', value: '2026-11-21' },
      { label: 'End of month', value: '2026-11-30' },
      { label: 'End of next month', value: '2026-12-31' }
    ])
    expect(new Set(presets.map((p) => p.value)).size).toBe(presets.length)
  })

  it('uses Sunday as the end of a Monday-start week', () => {
    const presets = buildDueDatePresets('2026-07-06', 'monday')
    expect(presets[2]).toEqual({ label: 'End of week', value: '2026-07-12' })
    expect(presets[3]).toEqual({
      label: 'End of next week',
      value: '2026-07-19'
    })
  })
})
