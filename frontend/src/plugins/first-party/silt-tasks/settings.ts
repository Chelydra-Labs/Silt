// Per-vault preference I/O for the unified Tasks hub (#424).
//
// Every hub preference lives under `plugins.plugin_settings.silt-tasks` in the
// vault config.yaml (ARCHITECTURE §0 rule 2: per-vault plugin prefs → YAML).
// Reads come from the SDK's `ctx.getPluginSettings()` (async, per-active-
// notebook override-aware #133); writes go through `ctx.updatePluginSetting`
// (the atomic Go-side UpdatePluginSetting binding, configMu-guarded #120).
//
// Routing through the PluginContext SDK (not the app-level settings store)
// ensures linked-notebook co-located overrides are honored and the module is
// compatible with the planned per-plugin webview migration (#151/#152).
//
// Each build phase that persists a new key adds a load*/persist* pair here so
// there is one source of truth for the silt-tasks config shape. The final
// schema (incl. saved_views/columns/filters/default_scope/calendar_sub_mode)
// is owned by the migration issue (#431); this module is the read/write surface.

import type { PluginContext } from '../../sdk'
import { normalizeColumns, type BoardColumn } from './columns'
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

// Module-scoped config slice + persist function, set by initTasksSettings.
// Reads default to {} (all load* functions return their documented defaults
// for an empty slice) until the hub's onMount populates them async via
// ctx.getPluginSettings(). This honors the per-active-notebook override
// layer (#133) without a synchronous app-store import.
let configSlice: Record<string, unknown> = {}
let saveFn: ((key: string, value: unknown) => Promise<boolean>) | null = null

/**
 * Wire the SDK into the module: capture the persist function and load the
 * initial settings slice. Called once from TasksHub's onMount (before any
 * load* call). The returned slice is also available synchronously via
 * tasksSettings() for the hydration block that follows.
 */
export async function initTasksSettings(ctx: PluginContext): Promise<void> {
  saveFn = (key, value) =>
    ctx.updatePluginSetting(key, value).catch(() => false)
  configSlice = (await ctx.getPluginSettings()) ?? {}
}

/**
 * Re-read the settings slice from the SDK (e.g. after a config:changed event
 * or active-notebook:changed signals a stale cache). Also refreshes saveFn
 * so a post-vault-switch remount captures the new ctx.
 */
export async function reloadTasksSettings(ctx: PluginContext): Promise<void> {
  saveFn = (key, value) =>
    ctx.updatePluginSetting(key, value).catch(() => false)
  configSlice = (await ctx.getPluginSettings()) ?? {}
}

/**
 * Clear the module-scoped state on vault close/switch so a stale configSlice
 * or an invalid session-bound saveFn from the previous vault cannot leak into
 * the next. Called from the loader's vault:closing handler alongside
 * resetTaskHubState().
 */
export function resetTasksSettings(): void {
  configSlice = {}
  saveFn = null
}

/** The on-disk slice `plugins.plugin_settings['silt-tasks']`, or {}. */
export function tasksSettings(): Record<string, unknown> {
  return configSlice
}

export function isDisplayMode(v: unknown): v is DisplayMode {
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
  'owner',
  'modified',
  'estimate'
]

export function isGroupBy(v: unknown): v is GroupBy {
  return typeof v === 'string' && (GROUP_BY_VALUES as string[]).includes(v)
}

export function isSortMode(v: unknown): v is SortMode {
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
 * Persisted local author for the comment composer (#430). Distinguishes
 * "never set" (undefined) from "explicitly cleared" (''): a cleared pref
 * must be respected, not re-seeded from the OS username on every mount.
 * Returns `undefined` when the key is absent, `''` when the user cleared
 * the input, or the saved name otherwise.
 */
export function loadLocalAuthor(): string | undefined {
  const slice = tasksSettings()
  if (!('local_author' in slice)) return undefined
  const v = slice['local_author']
  return typeof v === 'string' ? v : ''
}

/** Atomically write the local author pref to the vault config. */
export function persistLocalAuthor(author: string): Promise<boolean> {
  return saveFn ? saveFn('local_author', author) : Promise.resolve(false)
}

/**
 * Persisted Board-mode status columns (#421/#437). The Board is the only
 * surface with user-managed columns (every other grouping dimension is
 * data-driven). Accepts legacy string[] and structured {name, wipLimit?}[]
 * via normalizeColumns. Defaults to the canonical TODO/DOING/DONE so the
 * status Board matches the retired silt-kanban experience on first paint.
 */
export function loadColumns(): BoardColumn[] {
  return normalizeColumns(tasksSettings()['columns'])
}

/**
 * Atomically write the Board-mode status columns (incl. soft WIP limits)
 * to the vault config. Persists structured objects so wipLimit survives;
 * unlimited columns omit the field for cleaner YAML.
 */
export function persistColumns(columns: BoardColumn[]): Promise<boolean> {
  const payload = columns.map((c) => {
    if (c.wipLimit != null && c.wipLimit >= 1) {
      return { name: c.name, wipLimit: c.wipLimit }
    }
    return { name: c.name }
  })
  return saveFn ? saveFn('columns', payload) : Promise.resolve(false)
}

/** Atomically write the default display mode to the vault config. */
export function persistDefaultDisplayMode(mode: DisplayMode): Promise<boolean> {
  return saveFn ? saveFn('default_display_mode', mode) : Promise.resolve(false)
}

/** Atomically write the Calendar sub-layout to the vault config. */
export function persistCalendarSubMode(
  mode: CalendarSubMode
): Promise<boolean> {
  return saveFn ? saveFn('calendar_sub_mode', mode) : Promise.resolve(false)
}

/** Atomically write the default group-by to the vault config. */
export function persistDefaultGroupBy(g: GroupBy): Promise<boolean> {
  return saveFn ? saveFn('default_group_by', g) : Promise.resolve(false)
}

/** Atomically write the default sort to the vault config. */
export function persistDefaultSort(s: SortMode): Promise<boolean> {
  return saveFn ? saveFn('default_sort', s) : Promise.resolve(false)
}

// ─── Saved views (#427) ────────────────────────────────────────────────────
//
// Three layers compose on hydrate:
//
//   1. SYSTEM_VIEWS — code-defined (savedViews.ts). Read-only, not persisted.
//   2. User views — `plugins.plugin_settings['silt-tasks'].saved_views[]`.
//
// Dedup is by id; SYSTEM_VIEWS ids are reserved (`sys-` prefix), so a user
// view can never collide.

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
  if (Array.isArray(r.columns) && r.columns.length > 0) {
    // Accept legacy string[] and structured BoardColumn[] (#437).
    v.columns = normalizeColumns(r.columns)
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
        : [],
      ...(typeof fr.stale === 'boolean' ? { stale: fr.stale } : {})
    }
  }
  return v
}

/**
 * Load the merged saved-views list: SYSTEM_VIEWS (code-defined, never
 * persisted) + user views from `saved_views[]`.
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
  return [...merged.values()]
}

/**
 * Persist the user-defined saved views to `saved_views[]`. Strips
 * system views first (they're re-derived from SYSTEM_VIEWS on every
 * load and never consume YAML budget). Returns the atomic-write
 * promise from ctx.updatePluginSetting (#120 clobber-safe).
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
  return saveFn ? saveFn('saved_views', userViews) : Promise.resolve(false)
}
