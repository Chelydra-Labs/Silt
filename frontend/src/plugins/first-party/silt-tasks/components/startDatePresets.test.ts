import { describe, expect, it } from 'vitest'
import { buildStartDatePresets } from './startDatePresets'

describe('buildStartDatePresets', () => {
  it('resolves all presets against a Sunday-start week and month rollover', () => {
    expect(buildStartDatePresets('2026-01-31', 'sunday')).toEqual([
      { label: 'Today', value: '2026-01-31' },
      { label: 'Tomorrow', value: '2026-02-01' },
      { label: 'Start of next week', value: '2026-02-01' },
      { label: 'Start of next month', value: '2026-02-01' }
    ])
  })

  it('uses Monday as the start of a Monday-start week', () => {
    // 2026-07-06 is a Monday; next week starts 2026-07-13.
    const presets = buildStartDatePresets('2026-07-06', 'monday')
    expect(presets[2]).toEqual({
      label: 'Start of next week',
      value: '2026-07-13'
    })
  })

  it('uses Sunday as the start of a Sunday-start week', () => {
    // 2026-07-08 is a Wednesday; Sunday-start next week is 2026-07-12.
    const presets = buildStartDatePresets('2026-07-08', 'sunday')
    expect(presets[2]).toEqual({
      label: 'Start of next week',
      value: '2026-07-12'
    })
  })
})
