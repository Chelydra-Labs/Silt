// Per-vault preference I/O for the unified Tasks hub (#424).
//
// Every hub preference lives under `plugins.plugin_settings.silt-tasks` in the
// vault config.yaml (ARCHITECTURE §0 rule 2: per-vault plugin prefs → YAML).
// Reads come from the synchronous `settings` snapshot (fast first paint); the
// atomic Go-side `updatePluginSetting` (configMu-guarded, #120) is the single
// write path so a concurrent external config edit can't be silently clobbered.
//
// Each build phase that persists a new key adds a load*/persist* pair here so
// there is one source of truth for the silt-tasks config shape. The final
// schema (incl. saved_views/columns/filters/default_scope/calendar_sub_mode)
// is owned by the migration issue (#431); this module is the read/write surface.

import { settings, updatePluginSetting } from '../../../settings/store.svelte'
import type {
  CalendarSubMode,
  DisplayMode,
  GroupBy,
  SortMode
} from './state.svelte'

export const TASKS_PLUGIN_ID = 'silt-tasks'

/** The on-disk slice `plugins.plugin_settings['silt-tasks']`, or {}. */
export function tasksSettings(): Record<string, unknown> {
  const ps = settings.config?.plugins?.plugin_settings as
    Record<string, Record<string, unknown>> | undefined
  const slice = ps?.[TASKS_PLUGIN_ID]
  return slice && typeof slice === 'object' ? slice : {}
}

function isDisplayMode(v: unknown): v is DisplayMode {
  return v === 'list' || v === 'board' || v === 'calendar'
}

function isCalendarSubMode(v: unknown): v is CalendarSubMode {
  return v === 'month' || v === 'week'
}

const GROUP_BY_VALUES: readonly GroupBy[] = [
  'none',
  'status',
  'priority',
  'owner',
  'dueDate',
  'tag',
  'notebook',
  'section',
  'page'
]

const SORT_MODE_VALUES: readonly SortMode[] = [
  'manual',
  'dueDate',
  'priority',
  'title',
  'created',
  'owner'
]

function isGroupBy(v: unknown): v is GroupBy {
  return typeof v === 'string' && (GROUP_BY_VALUES as string[]).includes(v)
}

function isSortMode(v: unknown): v is SortMode {
  return typeof v === 'string' && (SORT_MODE_VALUES as string[]).includes(v)
}

/** Persisted default display mode; 'list' when unset/invalid. */
export function loadDefaultDisplayMode(): DisplayMode {
  const v = tasksSettings()['default_display_mode']
  return isDisplayMode(v) ? v : 'list'
}

/** Persisted Calendar sub-layout (month/week); 'month' when unset/invalid. */
export function loadCalendarSubMode(): CalendarSubMode {
  const v = tasksSettings()['calendar_sub_mode']
  return isCalendarSubMode(v) ? v : 'month'
}

/** Persisted default group-by; 'dueDate' when unset/invalid (#423). */
export function loadDefaultGroupBy(): GroupBy {
  const v = tasksSettings()['default_group_by']
  return isGroupBy(v) ? v : 'dueDate'
}

/** Persisted default sort; 'dueDate' when unset/invalid (#423). */
export function loadDefaultSort(): SortMode {
  const v = tasksSettings()['default_sort']
  return isSortMode(v) ? v : 'dueDate'
}

/** Atomically write the default display mode to the vault config. */
export function persistDefaultDisplayMode(mode: DisplayMode): Promise<boolean> {
  return updatePluginSetting(TASKS_PLUGIN_ID, 'default_display_mode', mode)
}

/** Atomically write the Calendar sub-layout to the vault config. */
export function persistCalendarSubMode(
  mode: CalendarSubMode
): Promise<boolean> {
  return updatePluginSetting(TASKS_PLUGIN_ID, 'calendar_sub_mode', mode)
}

/** Atomically write the default group-by to the vault config. */
export function persistDefaultGroupBy(g: GroupBy): Promise<boolean> {
  return updatePluginSetting(TASKS_PLUGIN_ID, 'default_group_by', g)
}

/** Atomically write the default sort to the vault config. */
export function persistDefaultSort(s: SortMode): Promise<boolean> {
  return updatePluginSetting(TASKS_PLUGIN_ID, 'default_sort', s)
}
