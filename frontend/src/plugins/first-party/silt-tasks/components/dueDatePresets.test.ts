import { describe, expect, it } from 'vitest'
import { buildDueDatePresets } from './dueDatePresets'

describe('buildDueDatePresets', () => {
  it('resolves all presets against a Sunday-start week and month rollover', () => {
    expect(buildDueDatePresets('2026-01-31', 'sunday')).toEqual([
      { label: 'Today', value: '2026-01-31' },
      { label: 'Tomorrow', value: '2026-02-01' },
      { label: 'End of week', value: '2026-01-31' },
      { label: 'End of next week', value: '2026-02-07' },
      { label: 'End of month', value: '2026-01-31' },
      { label: 'End of next month', value: '2026-02-28' }
    ])
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
