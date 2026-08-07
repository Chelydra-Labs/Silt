import { DEFAULT_WEEK_START, isWeekStart, type WeekStart } from './dateGrid'

/**
 * Reactive active-vault projection of the Tasks week-start preference.
 * Persistence remains owned by silt-tasks/settings; this bridge lets global
 * surfaces consume the active notebook override without reading config.yaml.
 */
export const taskWeekStart = $state({ value: DEFAULT_WEEK_START })

export function getTaskWeekStart(): WeekStart {
  return taskWeekStart.value
}

export function setTaskWeekStart(value: WeekStart): void {
  if (isWeekStart(value)) taskWeekStart.value = value
}

export function resetTaskWeekStart(): void {
  taskWeekStart.value = DEFAULT_WEEK_START
}
