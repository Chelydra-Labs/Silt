import { describe, expect, it } from 'vitest'
import {
  ymd,
  startOfWeek,
  startOfMonth,
  endOfMonth,
  addMonths,
  addDays,
  monthWeeks,
  endOfWeek,
  startOfWeekISO,
  endOfWeekISO,
  addDaysISO
} from './dateGrid'

describe('dateGrid', () => {
  describe('ymd', () => {
    it('formats a local Date as zero-padded YYYY-MM-DD', () => {
      expect(ymd(new Date(2026, 0, 5))).toBe('2026-01-05')
      expect(ymd(new Date(2026, 10, 25))).toBe('2026-11-25')
      expect(ymd(new Date(2026, 11, 31))).toBe('2026-12-31')
    })

    it('does not drift via UTC (constructs from local components)', () => {
      // A date at local midnight must format to its own day, not a UTC-shifted
      // one. toISOString().slice(0,10) would shift backward in western tzs.
      const d = new Date(2026, 2, 15, 0, 0, 0)
      expect(ymd(d)).toBe('2026-03-15')
    })
  })

  describe('startOfWeek', () => {
    it('returns the Sunday of the containing week at 00:00:00', () => {
      // Wednesday Jan 7 2026 → Sunday Jan 4 2026
      const d = startOfWeek(new Date(2026, 0, 7))
      expect(d.getDay()).toBe(0)
      expect(ymd(d)).toBe('2026-01-04')
      expect(d.getHours()).toBe(0)
    })

    it('is idempotent on a Sunday', () => {
      const sun = new Date(2026, 2, 1) // Mar 1 2026 is a Sunday
      expect(ymd(startOfWeek(sun))).toBe('2026-03-01')
    })

    it('supports Monday-start weeks across a Sunday boundary', () => {
      const d = new Date(2026, 2, 1) // Sunday
      expect(ymd(startOfWeek(d, 'monday'))).toBe('2026-02-23')
      expect(ymd(endOfWeek(d, 'monday'))).toBe('2026-03-01')
    })

    it('keeps ISO boundary helpers in local calendar time', () => {
      expect(startOfWeekISO('2026-01-01', 'monday')).toBe('2025-12-29')
      expect(endOfWeekISO('2026-01-01', 'monday')).toBe('2026-01-04')
      expect(addDaysISO('2025-12-31', 1)).toBe('2026-01-01')
    })
  })

  describe('startOfMonth / endOfMonth', () => {
    it('startOfMonth is day 1 at midnight', () => {
      expect(ymd(startOfMonth(new Date(2026, 1, 15)))).toBe('2026-02-01')
    })

    it('endOfMonth is the last calendar day (day 0 of next month)', () => {
      expect(ymd(endOfMonth(new Date(2026, 0, 10)))).toBe('2026-01-31')
      expect(ymd(endOfMonth(new Date(2026, 1, 10)))).toBe('2026-02-28') // non-leap
      expect(ymd(endOfMonth(new Date(2024, 1, 10)))).toBe('2024-02-29') // leap
      expect(ymd(endOfMonth(new Date(2026, 3, 10)))).toBe('2026-04-30')
    })
  })

  describe('addMonths / addDays', () => {
    it('addMonths moves the month and resets to day 1', () => {
      expect(ymd(addMonths(new Date(2026, 0, 15), 1))).toBe('2026-02-01')
      expect(ymd(addMonths(new Date(2026, 0, 15), -1))).toBe('2025-12-01')
      expect(ymd(addMonths(new Date(2026, 11, 15), 1))).toBe('2027-01-01')
    })

    it('addDays shifts days and preserves time', () => {
      const d = addDays(new Date(2026, 2, 30, 9, 30), 5)
      expect(ymd(d)).toBe('2026-04-04')
      expect(d.getHours()).toBe(9)
    })
  })

  describe('monthWeeks', () => {
    it('emits 4–6 rows of exactly 7 consecutive Sunday-start days', () => {
      const weeks = monthWeeks(new Date(2026, 5, 15)) // Jun 2026
      expect(weeks.length).toBeGreaterThanOrEqual(4)
      expect(weeks.length).toBeLessThanOrEqual(6)
      for (const row of weeks) expect(row).toHaveLength(7)
      // First cell is a Sunday.
      expect(weeks[0][0].getDay()).toBe(0)
      // Days are consecutive (+1 each cell across rows).
      const flat = weeks.flat()
      for (let i = 1; i < flat.length; i++) {
        const diff = Math.round(
          (flat[i].getTime() - flat[i - 1].getTime()) / 86_400_000
        )
        expect(diff).toBe(1)
      }
    })

    it('returns 4 rows for a 28-day February starting on Sunday', () => {
      // Feb 2026: 28 days, Feb 1 is a Sunday → exactly 4 rows.
      expect(monthWeeks(new Date(2026, 1, 15))).toHaveLength(4)
    })

    it('returns 5 rows for a 31-day month starting on Sunday', () => {
      // Mar 2026: 31 days, Mar 1 is a Sunday → 5 rows.
      expect(monthWeeks(new Date(2026, 2, 15))).toHaveLength(5)
    })

    it('returns 6 rows for a 31-day month starting on Saturday', () => {
      // Jan 2022: 31 days, Jan 1 is a Saturday (6 leading days) → 6 rows.
      expect(monthWeeks(new Date(2022, 0, 15))).toHaveLength(6)
    })

    it('includes every calendar day of the month in the grid', () => {
      const weeks = monthWeeks(new Date(2026, 2, 15)) // Mar 2026 (31 days)
      const inMonthDays = weeks
        .flat()
        .filter((d) => d.getMonth() === 2)
        .map((d) => d.getDate())
      expect(inMonthDays).toEqual(Array.from({ length: 31 }, (_, i) => i + 1))
    })

    it('places Monday first in a Monday-start month grid', () => {
      const first = monthWeeks(new Date(2026, 2, 15), 'monday')[0][0]
      expect(first.getDay()).toBe(1)
      expect(ymd(first)).toBe('2026-02-23')
    })
  })
})
