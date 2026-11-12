import {
  addDaysISO,
  addMonths,
  dateFromYmd,
  startOfWeekISO,
  startOfMonth,
  type WeekStart,
  ymd
} from '../../../../lib/dateGrid'

export interface StartDatePreset {
  label: string
  value: string
}

/** Resolve start-day shortcuts from the same local calendar boundaries as views. */
export function buildStartDatePresets(
  today: string,
  weekStart: WeekStart
): StartDatePreset[] {
  const tomorrow = addDaysISO(today, 1)
  const thisWeekStart = startOfWeekISO(today, weekStart)
  const startOfNextWeek = addDaysISO(thisWeekStart, 7)
  const nextMonthStart = ymd(startOfMonth(addMonths(dateFromYmd(today), 1)))
  const presets = [
    { label: 'Today', value: today },
    { label: 'Tomorrow', value: tomorrow },
    { label: 'Start of next week', value: startOfNextWeek },
    { label: 'Start of next month', value: nextMonthStart }
  ]
  // Shortcut dates can collide ("Tomorrow" == "Start of next month" on the
  // second-to-last day of a month). Keep the first — more specific — shortcut
  // so every preset offers a distinct date, matching the due-date presets.
  return presets.filter(
    (preset, i) => presets.findIndex((p) => p.value === preset.value) === i
  )
}
