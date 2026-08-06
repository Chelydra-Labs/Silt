// System-default saved views + fingerprint helpers (#427).
//
// The three SYSTEM_VIEWS are code-defined: they ship with every install,
// are read-only (hideable but not deletable), and never persist to
// config.yaml — they're re-derived from this module on every load. They
// give the user a non-empty "Saved Views" list on first paint (a brand-new
// vault has no user views yet) and demonstrate each display mode.
//
// The "is the live state the same as this saved view?" check is the generic
// partial-snapshot matcher from lib/viewEngine/savedViews.ts; this module
// supplies the task-specific dim set + the two dims that need structural
// comparison (`columns` via columnsEqual incl. wipLimit; `filters` via the
// nested partial bag check). SYSTEM_VIEWS itself stays task-specific.

import { columnsEqual } from './columns'
import type { BoardColumn } from './columns'
import type { SavedView, TaskFilters, TaskHubState } from './state.svelte'
import {
  viewMatchesState as veViewMatchesState,
  arrayEqual
} from '../../../lib/viewEngine/savedViews'

/**
 * The three built-in saved views. Ids are stable strings (not UUIDs) so
 * persistSavedViews can recognize + strip them on write, and so a user
 * view can never collide (settings.ts forbids the `sys-` prefix when
 * loading user views — the prefix is the system marker).
 */
export const SYSTEM_VIEWS: SavedView[] = [
  {
    id: 'sys-today-board',
    name: "Today's Board",
    displayMode: 'board',
    groupBy: 'status',
    sort: 'dueDate',
    scope: 'vault',
    filters: { owners: [], priorities: [], dueDate: 'today', tags: [] },
    system: true
  },
  {
    id: 'sys-by-owner',
    name: 'By Owner',
    displayMode: 'list',
    groupBy: 'owner',
    sort: 'priority',
    scope: 'vault',
    filters: { owners: [], priorities: [], dueDate: '', tags: [] },
    system: true
  },
  {
    id: 'sys-week-calendar',
    name: "This Week's Calendar",
    displayMode: 'calendar',
    groupBy: 'dueDate',
    sort: 'dueDate',
    scope: 'vault',
    filters: { owners: [], priorities: [], dueDate: 'week', tags: [] },
    calendarSubMode: 'week',
    system: true
  }
]

/** The dims a SavedView can snapshot — the matcher compares only these. */
const MATCH_DIMS = [
  'displayMode',
  'groupBy',
  'sort',
  'scope',
  'calendarSubMode',
  'columns',
  'filters'
] as const

/**
 * Partial comparison of the `filters` bag: each sub-field the VIEW defines
 * is compared; undefined-by-view sub-fields are skipped. `stale` is the
 * exception — it's compared unconditionally (mirrors the original contract:
 * a view with no stale opinion still shouldn't match a state surfacing
 * stale-only tasks).
 */
function filtersEqual(viewFilters: unknown, stateFilters: unknown): boolean {
  if (!viewFilters || typeof viewFilters !== 'object') return false
  if (!stateFilters || typeof stateFilters !== 'object') return false
  const v = viewFilters as TaskFilters
  const s = stateFilters as TaskFilters
  if (v.owners !== undefined && !arrayEqual(v.owners, s.owners)) return false
  if (v.priorities !== undefined && !arrayEqual(v.priorities, s.priorities))
    return false
  if (v.dueDate !== undefined && v.dueDate !== s.dueDate) return false
  if (v.tags !== undefined && !arrayEqual(v.tags, s.tags)) return false
  if (!!v.stale !== !!s.stale) return false
  return true
}

/**
 * Lenient "does this view match the current state?" check (#427, #432).
 *
 * System views are partial templates — they define only the dims they care
 * about (e.g. "Today's Board" specifies board/status/today-filters but says
 * nothing about calendarSubMode or columns). The strict fingerprint
 * comparison treats undefined view dims as empty strings, so it never
 * matches a state that has those dims populated — system views never
 * highlighted as active.
 *
 * Delegates to the generic partial-snapshot matcher with task-specific
 * equality for `columns` (structural, wipLimit-aware) and `filters` (nested
 * partial bag). A user view that snapshots every dim reduces to the strict
 * check. Used by the Sidebar active-highlight and the TasksHub bookmark icon.
 */
export function viewMatchesState(view: SavedView, s: TaskHubState): boolean {
  return veViewMatchesState(
    view as unknown as Record<string, unknown>,
    s as unknown as Record<string, unknown>,
    MATCH_DIMS,
    (dim) => {
      if (dim === 'columns') {
        return (a, b) => columnsEqual(a as BoardColumn[], b as BoardColumn[])
      }
      if (dim === 'filters') return filtersEqual
      return undefined
    }
  )
}
