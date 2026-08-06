import {
  addDaysISO,
  addMonths,
  dateFromYmd,
  endOfMonth,
  endOfWeekISO,
  type WeekStart,
  ymd
} from '../../../../lib/dateGrid'

export interface DueDatePreset {
  label: string
  value: string
}

/** Resolve due-date shortcuts from the same local calendar boundaries as views. */
export function buildDueDatePresets(
  today: string,
  weekStart: WeekStart
): DueDatePreset[] {
  const endOfWeek = endOfWeekISO(today, weekStart)
  const endOfNextWeek = addDaysISO(endOfWeek, 7)
  const todayDate = dateFromYmd(today)
  const endOfCurrentMonth = ymd(endOfMonth(todayDate))
  const endOfNextMonth = ymd(endOfMonth(addMonths(todayDate, 1)))

  return [
    { label: 'Today', value: today },
    { label: 'Tomorrow', value: addDaysISO(today, 1) },
    { label: 'End of week', value: endOfWeek },
    { label: 'End of next week', value: endOfNextWeek },
    { label: 'End of month', value: endOfCurrentMonth },
    { label: 'End of next month', value: endOfNextMonth }
  ]
}
