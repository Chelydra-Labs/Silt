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
 * Build a stable fingerprint of a SavedView's dimension values. Two views
 * that snapshot the same dimensions produce the same string, so the header
 * bookmark / sidebar list can detect "this view is active" without deep
 * equality. Uses `\u0000` (NUL) as the field separator — it can't appear
 * in any of the dimension values (UUIDs, enum names, YAML strings) so the
 * join is unambiguous.
 *
 * Generalized from KanbanSidebar.svelte:89-101 which only fingerprinted
 * scope+filters; the unified hub dimensions (displayMode/groupBy/sort/
 * calendarSubMode/columns) all participate now.
 */
export function fingerprintOf(view: SavedView): string {
  const f = view.filters ?? {
    owners: [],
    priorities: [],
    dueDate: '',
    tags: []
  }
  return [
    view.displayMode ?? '',
    view.groupBy ?? '',
    view.sort ?? '',
    view.scope ?? '',
    f.owners.join('|'),
    f.priorities.join('|'),
    f.dueDate,
    f.tags.join('|'),
    view.calendarSubMode ?? '',
    (view.columns ?? []).join('|')
  ].join('\u0000')
}

/**
 * Fingerprint of the CURRENT hub state — i.e. the view the user would
 * save if they clicked "Save current view" right now. Comparing this
 * against `fingerprintOf(activeSavedView)` tells the bookmark whether
 * to show "saved" (match) or "modified" (mismatch).
 */
export function fingerprintOfState(s: TaskHubState): string {
  const f = s.filters
  return [
    s.displayMode,
    s.groupBy,
    s.sort,
    s.scope,
    f.owners.join('|'),
    f.priorities.join('|'),
    f.dueDate,
    f.tags.join('|'),
    s.calendarSubMode,
    s.columns.join('|')
  ].join('\u0000')
}
