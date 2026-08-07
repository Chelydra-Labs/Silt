import { addDays, dateFromYmd, ymd } from '../../../../lib/dateGrid'

/** Compute the next recurrence using local calendar dates, never UTC math. */
export function nextRecurrenceDate(
  recurrence: string,
  dueDate: string,
  now = new Date()
): string {
  if (!recurrence || !dueDate) return ''
  const due = dateFromYmd(dueDate)
  if (isNaN(due.getTime())) return ''
  const today = new Date(now)
  today.setHours(0, 0, 0, 0)
  if (due <= today) return ''

  const rule = recurrence.toLowerCase()
  let step: Date
  if (rule.includes('day') && !rule.includes('weekday')) {
    const n = parseInt(rule.match(/(\d+)\s*day/)?.[1] ?? '1')
    step = addDays(due, n)
  } else if (rule.includes('weekday')) {
    step = addDays(due, 1)
    while (step.getDay() === 0 || step.getDay() === 6) {
      step = addDays(step, 1)
    }
  } else if (rule.includes('week')) {
    const n = parseInt(rule.match(/(\d+)\s*week/)?.[1] ?? '1')
    step = addDays(due, n * 7)
  } else if (rule.includes('month')) {
    const n = parseInt(rule.match(/(\d+)\s*month/)?.[1] ?? '1')
    step = new Date(due.getFullYear(), due.getMonth() + n, due.getDate())
  } else if (rule.includes('year')) {
    const n = parseInt(rule.match(/(\d+)\s*year/)?.[1] ?? '1')
    step = new Date(due.getFullYear() + n, due.getMonth(), due.getDate())
  } else {
    return ''
  }
  return ymd(step)
}
