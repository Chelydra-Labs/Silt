// Pure local-time month-grid arithmetic shared by the Tasks calendar, the
// sidebar mini-cal, and the global Date Glance popover. Extracted here so the
// three surfaces share one source of truth instead of three inline copies.
//
// All functions operate on LOCAL time. Never use Date.toISOString().slice(0,10)
// here or in callers — it shifts a day backward near midnight in western
// timezones. ymd() formats the local components directly.

/** Zero-pad a number to two digits. */
const PAD2 = (n: number): string => String(n).padStart(2, '0')

/** The supported first day of a calendar week. */
export type WeekStart = 'sunday' | 'monday'

export const DEFAULT_WEEK_START: WeekStart = 'sunday'

/** Validate a persisted or externally supplied week-start value. */
export function isWeekStart(value: unknown): value is WeekStart {
  return value === 'sunday' || value === 'monday'
}

/** Resolve malformed persisted values to the product default. */
export function normalizeWeekStart(value: unknown): WeekStart {
  return isWeekStart(value) ? value : DEFAULT_WEEK_START
}

/** Format a Date as local YYYY-MM-DD (no UTC drift). */
export function ymd(d: Date): string {
  return `${d.getFullYear()}-${PAD2(d.getMonth() + 1)}-${PAD2(d.getDate())}`
}

/** Parse a YYYY-MM-DD value as a local midnight Date. */
export function dateFromYmd(iso: string): Date {
  const [year, month, day] = iso.split('-').map(Number)
  return new Date(year || 1970, (month || 1) - 1, day || 1)
}

/** Week containing d, at 00:00:00 local, using the configured first day. */
export function startOfWeek(
  d: Date,
  weekStart: WeekStart = DEFAULT_WEEK_START
): Date {
  const x = new Date(d)
  const dayIndex = weekStart === 'monday' ? (x.getDay() + 6) % 7 : x.getDay()
  x.setDate(x.getDate() - dayIndex)
  x.setHours(0, 0, 0, 0)
  return x
}

/** Last day of d's configured week at 00:00:00 local. */
export function endOfWeek(
  d: Date,
  weekStart: WeekStart = DEFAULT_WEEK_START
): Date {
  return addDays(startOfWeek(d, weekStart), 6)
}

/** Format the configured week boundaries for SQL/date-only comparisons. */
export function weekBounds(
  d: Date,
  weekStart: WeekStart = DEFAULT_WEEK_START
): { start: string; end: string } {
  return {
    start: ymd(startOfWeek(d, weekStart)),
    end: ymd(endOfWeek(d, weekStart))
  }
}

export function startOfWeekISO(
  iso: string,
  weekStart: WeekStart = DEFAULT_WEEK_START
): string {
  return ymd(startOfWeek(dateFromYmd(iso), weekStart))
}

export function endOfWeekISO(
  iso: string,
  weekStart: WeekStart = DEFAULT_WEEK_START
): string {
  return ymd(endOfWeek(dateFromYmd(iso), weekStart))
}

/** Add local calendar days to a YYYY-MM-DD value. */
export function addDaysISO(iso: string, n: number): string {
  return ymd(addDays(dateFromYmd(iso), n))
}

/** First day of d's month at 00:00:00 local. */
export function startOfMonth(d: Date): Date {
  return new Date(d.getFullYear(), d.getMonth(), 1)
}

/** Last day of d's month at 00:00:00 local (day 0 of the next month). */
export function endOfMonth(d: Date): Date {
  return new Date(d.getFullYear(), d.getMonth() + 1, 0)
}

/** A Date `n` months from d's month (day 1). Used for prev/next month nav. */
export function addMonths(d: Date, n: number): Date {
  return new Date(d.getFullYear(), d.getMonth() + n, 1)
}

/** A Date `n` days from d (preserves time-of-day). */
export function addDays(d: Date, n: number): Date {
  const x = new Date(d)
  x.setDate(x.getDate() + n)
  return x
}

/**
 * The weeks of the month containing `cursor`, as a configured Date[][] grid.
 * Emits up to 6 rows of 7 days; stops after the month ends once at least 4 rows
 * exist so February and 4-row-spanning months don't carry a trailing empty row.
 */
export function monthWeeks(
  cursor: Date,
  weekStart: WeekStart = DEFAULT_WEEK_START
): Date[][] {
  const first = startOfWeek(startOfMonth(cursor), weekStart)
  const monthEnd = endOfMonth(cursor)
  const weeks: Date[][] = []
  let cur = first
  for (let w = 0; w < 6; w++) {
    const row: Date[] = []
    for (let i = 0; i < 7; i++) {
      row.push(cur)
      cur = addDays(cur, 1)
    }
    weeks.push(row)
    if (cur > monthEnd && w >= 3) break
  }
  return weeks
}
