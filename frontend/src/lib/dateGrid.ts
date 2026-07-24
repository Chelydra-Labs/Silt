// Pure local-time month-grid arithmetic shared by the Tasks calendar, the
// sidebar mini-cal, and the global Date Glance popover. Extracted here so the
// three surfaces share one source of truth instead of three inline copies.
//
// All functions operate on LOCAL time. Never use Date.toISOString().slice(0,10)
// here or in callers — it shifts a day backward near midnight in western
// timezones. ymd() formats the local components directly.

/** Zero-pad a number to two digits. */
const PAD2 = (n: number): string => String(n).padStart(2, '0')

/** Format a Date as local YYYY-MM-DD (no UTC drift). */
export function ymd(d: Date): string {
  return `${d.getFullYear()}-${PAD2(d.getMonth() + 1)}-${PAD2(d.getDate())}`
}

/** Sunday-start week containing d, at 00:00:00 local. */
export function startOfWeek(d: Date): Date {
  const x = new Date(d)
  x.setDate(x.getDate() - x.getDay())
  x.setHours(0, 0, 0, 0)
  return x
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
 * The weeks of the month containing `cursor`, as a Sunday-start Date[][] grid.
 * Emits up to 6 rows of 7 days; stops after the month ends once at least 4 rows
 * exist so February and 4-row-spanning months don't carry a trailing empty row.
 */
export function monthWeeks(cursor: Date): Date[][] {
  const first = startOfWeek(startOfMonth(cursor))
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
