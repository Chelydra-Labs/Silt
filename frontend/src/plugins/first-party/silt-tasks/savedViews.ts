// System-default saved views + fingerprint helpers (#427).
//
// The three SYSTEM_VIEWS are code-defined: they ship with every install,
// are read-only (hideable but not deletable), and never persist to
// config.yaml — they're re-derived from this module on every load. They
// give the user a non-empty "Saved Views" list on first paint (a brand-new
// vault has no user views yet) and demonstrate each display mode.
//
// The two fingerprint helpers power the "is the live state the same as
// this saved view?" check used by the header bookmark and (Phase 7) the
// sidebar list highlight. Two views with identical dimensions share a
// fingerprint, so the comparison reduces to a string ===.

import { columnsEqual } from './columns'
import type { SavedView, TaskHubState } from './state.svelte'

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

/**
 * Lenient "does this view match the current state?" check (#427, #432).
 *
 * System views are partial templates — they define only the dims they care
 * about (e.g. "Today's Board" specifies board/status/today-filters but says
 * nothing about calendarSubMode or columns). The strict fingerprint
 * comparison (fingerprintOf === fingerprintOfState) treats undefined view
 * dims as empty strings, so it never matches a state that has those dims
 * populated — system views never highlighted as active.
 *
 * This function compares ONLY the dims the view defines. A user view that
 * snapshots every dim reduces to the strict check (all 10 defined → all 10
 * compared). A system view that omits calendarSubMode/columns matches any
 * state where its defined dims equal the state's, regardless of what the
 * state has for the omitted dims. This matches user mental model: a system
 * view is a starting template, not a complete snapshot.
 *
 * Used by the Sidebar active-highlight and the TasksHub bookmark icon. The
 * `savedViewsDirty` flag (set by every state setter when a view is active)
 * remains the source of truth for "user has diverged from the saved
 * snapshot" — it triggers the "Update / Save as new" menu prompt even for
 * system views when the user changes an omitted dim, which is the right
 * behavior (the user has personalized the template and may want to save
 * their version).
 */
export function viewMatchesState(view: SavedView, s: TaskHubState): boolean {
  if (view.displayMode !== undefined && view.displayMode !== s.displayMode)
    return false
  if (view.groupBy !== undefined && view.groupBy !== s.groupBy) return false
  if (view.sort !== undefined && view.sort !== s.sort) return false
  if (view.scope !== undefined && view.scope !== s.scope) return false
  if (
    view.calendarSubMode !== undefined &&
    view.calendarSubMode !== s.calendarSubMode
  )
    return false
  if (view.columns !== undefined && !columnsEqual(view.columns, s.columns))
    return false
  if (view.filters !== undefined) {
    if (
      view.filters.owners !== undefined &&
      !arrayEqual(view.filters.owners, s.filters.owners)
    )
      return false
    if (
      view.filters.priorities !== undefined &&
      !arrayEqual(view.filters.priorities, s.filters.priorities)
    )
      return false
    if (
      view.filters.dueDate !== undefined &&
      view.filters.dueDate !== s.filters.dueDate
    )
      return false
    if (
      view.filters.tags !== undefined &&
      !arrayEqual(view.filters.tags, s.filters.tags)
    )
      return false
    if (!!view.filters.stale !== !!s.filters.stale) return false
  }
  return true
}

function arrayEqual<T>(a: T[], b: T[]): boolean {
  if (a.length !== b.length) return false
  for (let i = 0; i < a.length; i++) {
    if (a[i] !== b[i]) return false
  }
  return true
}
