import { describe, expect, it } from 'vitest'
import { nextRecurrenceDate } from './recurrencePreview'

describe('nextRecurrenceDate', () => {
  const beforeDstBoundary = new Date(2026, 2, 6, 12)

  it('adds days and weeks as local calendar dates across DST boundaries', () => {
    expect(
      nextRecurrenceDate('every day', '2026-03-07', beforeDstBoundary)
    ).toBe('2026-03-08')
    expect(
      nextRecurrenceDate('every week', '2026-03-07', beforeDstBoundary)
    ).toBe('2026-03-14')
  })

  it('skips weekends for weekday recurrence', () => {
    expect(
      nextRecurrenceDate('every weekday', '2026-07-03', new Date(2026, 6, 2))
    ).toBe('2026-07-06')
  })

  it('returns local month/year dates and omits unsupported or past previews', () => {
    const now = new Date(2026, 0, 1)
    expect(nextRecurrenceDate('every month', '2026-01-15', now)).toBe(
      '2026-02-15'
    )
    expect(nextRecurrenceDate('every year', '2026-01-15', now)).toBe(
      '2027-01-15'
    )
    expect(nextRecurrenceDate('custom rule', '2026-01-15', now)).toBe('')
    expect(nextRecurrenceDate('every day', '2025-12-31', now)).toBe('')
  })
})
