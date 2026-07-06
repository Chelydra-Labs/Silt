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
import { SYSTEM_VIEWS } from './savedViews'
import type {
  CalendarSubMode,
  DisplayMode,
  GroupBy,
  SavedView,
  SortMode,
  TaskFilters
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

/**
 * Persisted Board-mode status columns (#421). The Board is the only surface
 * with user-managed columns (every other grouping dimension is data-driven).
 * Defaults to the canonical TODO/DOING/DONE so the status Board matches the
 * retired silt-kanban experience on first paint.
 */
export function loadColumns(): string[] {
  const v = tasksSettings()['columns']
  return Array.isArray(v) && v.every((x) => typeof x === 'string') && v.length
    ? [...v]
    : ['TODO', 'DOING', 'DONE']
}

/** Atomically write the Board-mode status columns to the vault config. */
export function persistColumns(columns: string[]): Promise<boolean> {
  return updatePluginSetting(TASKS_PLUGIN_ID, 'columns', [...columns])
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

// ─── Saved views (#427) ────────────────────────────────────────────────────
//
// Three layers compose on hydrate:
//
//   1. SYSTEM_VIEWS — code-defined (savedViews.ts). Read-only, not persisted.
//   2. User views — `plugins.plugin_settings['silt-tasks'].saved_views[]`.
//   3. Legacy Kanban boards — `plugins.plugin_settings['silt-kanban'].boards[]`
//      forward-mapped so a pre-migration vault still shows its boards.
//
// Dedup is by id; SYSTEM_VIEWS ids are reserved (`sys-` prefix), so a user
// view can never collide. The forward-read of legacy boards is the
// compat shim before the one-time migration (Phase 9 / #431) lands.

/**
 * Validate + coerce a single persisted view entry. Unknown enum values
 * fall back to defaults rather than dropping the whole view — the user
 * may have hand-edited the YAML and we'd rather land somewhere sane
 * than lose a saved configuration over a typo. Missing required fields
 * (id/name) DO drop the entry: a view with no id can't be activated,
 * and one with no name can't be displayed.
 */
function coerceSavedView(raw: unknown): SavedView | null {
  if (!raw || typeof raw !== 'object') return null
  const r = raw as Record<string, unknown>
  if (typeof r.id !== 'string' || r.id.length === 0) return null
  if (typeof r.name !== 'string' || r.name.length === 0) return null
  // Reserve the `sys-` prefix for code-defined system views; a user view
  // carrying one is treated as malformed so persistSavedViews can't
  // shadow a built-in by user input.
  if (r.id.startsWith('sys-')) return null

  const v: SavedView = { id: r.id, name: r.name }
  if (isDisplayMode(r.displayMode)) v.displayMode = r.displayMode
  if (isGroupBy(r.groupBy)) v.groupBy = r.groupBy
  if (isSortMode(r.sort)) v.sort = r.sort
  if (
    r.scope === 'vault' ||
    r.scope === 'notebook' ||
    r.scope === 'section' ||
    r.scope === 'page'
  ) {
    v.scope = r.scope
  }
  if (isCalendarSubMode(r.calendarSubMode)) {
    v.calendarSubMode = r.calendarSubMode
  }
  if (
    Array.isArray(r.columns) &&
    r.columns.every((x) => typeof x === 'string')
  ) {
    v.columns = [...(r.columns as string[])]
  }
  if (r.filters && typeof r.filters === 'object') {
    const fr = r.filters as Record<string, unknown>
    v.filters = {
      owners: Array.isArray(fr.owners)
        ? (fr.owners.filter((x) => typeof x === 'string') as string[])
        : [],
      priorities: Array.isArray(fr.priorities)
        ? (fr.priorities.filter((x) => typeof x === 'number') as number[])
        : [],
      dueDate:
        typeof fr.dueDate === 'string' &&
        ['', 'overdue', 'today', 'week', 'none'].includes(fr.dueDate)
          ? (fr.dueDate as TaskFilters['dueDate'])
          : '',
      tags: Array.isArray(fr.tags)
        ? (fr.tags.filter((x) => typeof x === 'string') as string[])
        : []
    }
  }
  return v
}

/**
 * Load the merged saved-views list: SYSTEM_VIEWS (code-defined, never
 * persisted) + user views from `saved_views[]` + legacy Kanban boards
 * forward-mapped to views. Dedup by id (system ids win; user views can't
 * carry `sys-` so there's no real collision).
 */
export function loadSavedViews(): SavedView[] {
  const merged = new Map<string, SavedView>()
  for (const sv of SYSTEM_VIEWS) merged.set(sv.id, { ...sv })

  const rawUser = tasksSettings()['saved_views']
  if (Array.isArray(rawUser)) {
    for (const entry of rawUser) {
      const v = coerceSavedView(entry)
      if (v) merged.set(v.id, v)
    }
  }
  for (const legacy of loadLegacyKanbanBoardsAsViews()) {
    if (!merged.has(legacy.id)) merged.set(legacy.id, legacy)
  }
  return [...merged.values()]
}

/**
 * Read the legacy `plugins.plugin_settings['silt-kanban'].boards[]` and
 * map each SavedBoard to a SavedView with displayMode='board' (a saved
 * board is just a saved view of the board). The one-time migration
 * (#431 / Phase 9) writes these into saved_views[]; until that lands,
 * this forward-read keeps a pre-migration vault's boards visible.
 */
export function loadLegacyKanbanBoardsAsViews(): SavedView[] {
  const ps = settings.config?.plugins?.plugin_settings as
    Record<string, Record<string, unknown>> | undefined
  const kanbanSlice = ps?.['silt-kanban']
  const raw = kanbanSlice?.['boards']
  if (!Array.isArray(raw)) return []
  const views: SavedView[] = []
  for (const entry of raw) {
    if (!entry || typeof entry !== 'object') continue
    const r = entry as Record<string, unknown>
    if (typeof r.id !== 'string' || typeof r.name !== 'string') continue
    if (
      r.scope !== 'vault' &&
      r.scope !== 'notebook' &&
      r.scope !== 'section' &&
      r.scope !== 'page'
    ) {
      continue
    }
    const fr = (r.filters ?? {}) as Record<string, unknown>
    views.push({
      id: r.id,
      name: r.name,
      displayMode: 'board',
      groupBy: 'status',
      sort: 'manual',
      columns: undefined,
      scope: r.scope as SavedView['scope'],
      filters: {
        owners: Array.isArray(fr.owners)
          ? (fr.owners.filter((x) => typeof x === 'string') as string[])
          : [],
        priorities: Array.isArray(fr.priorities)
          ? (fr.priorities.filter((x) => typeof x === 'number') as number[])
          : [],
        dueDate:
          typeof fr.dueDate === 'string'
            ? (fr.dueDate as TaskFilters['dueDate'])
            : '',
        tags: Array.isArray(fr.tags)
          ? (fr.tags.filter((x) => typeof x === 'string') as string[])
          : []
      }
    })
  }
  return views
}

/**
 * Persist the user-defined saved views to `saved_views[]`. Strips
 * system views first (they're re-derived from SYSTEM_VIEWS on every
 * load and never consume YAML budget). Returns the atomic-write
 * promise from updatePluginSetting (#120 clobber-safe).
 */
export function persistSavedViews(views: SavedView[]): Promise<boolean> {
  const userViews = views
    .filter((v) => !v.system)
    .map((v) => {
      // Strip the `system` flag on write — it's a code-side marker and
      // a stale `system: true` in YAML would lock a user view out of
      // deletion on next load.
      const { system: _system, ...rest } = v
      void _system
      return rest
    })
  return updatePluginSetting(TASKS_PLUGIN_ID, 'saved_views', userViews)
}
