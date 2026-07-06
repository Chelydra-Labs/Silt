// Unified shared state for the future Tasks hub (#419, milestone #37
// phase 4). This module is the forward-looking single source of truth
// that supersedes two parallel patterns invented independently:
//
//   - silt-calendar/focusState.svelte.ts — { focusDate, activeFilter }
//     shared between Calendar.svelte + CalendarSidebar.svelte (#322).
//   - silt-kanban/kanbanSharedState.svelte.ts — { scope, filters,
//     scopeUserOverride, boardOwners, boardTags } shared between
//     Kanban.svelte + KanbanSidebar.svelte (#323).
//
// The two never unified because they shipped against separate surfaces.
// The Tasks hub (milestone #37) needs BOTH shapes at once: a future
// hub renders either a list (Calendar/Agenda lineage) or a board
// (Kanban lineage), so its state has to carry scope+filters AND
// focusDate+activeFilter together. This module is that carrier.
//
// PURELY ADDITIVE this phase: no existing consumer imports this yet.
// The two old modules stay live until milestone #38 migrates their
// consumers, then they are deleted. The scopeUserOverride invariant
// (#124) is ported verbatim — see setScope / narrowScopeTo /
// clearScopeOverride.

// Scope + filters mirror the Kanban types (../silt-kanban/types.ts) but
// are re-declared here under forward-looking names instead of imported.
// Importing from silt-kanban would create a backward dependency from
// the future-facing module onto the module it replaces; when #38
// deletes silt-kanban these types survive in place. The shapes are
// kept identical so #38 can swap imports one-for-one.

/** Navigation scope the board/list is narrowed to. */
export type Scope = 'vault' | 'notebook' | 'section' | 'page'

/** The due-date quick-pick from the filter bar. `''` = no filter. */
export type DueDateFilter = '' | 'overdue' | 'today' | 'week' | 'none'

/**
 * Task-level filters applied on top of the scope. Shape mirrors
 * KanbanFilters so a migrated Kanban caller can write the same literal.
 */
export interface TaskFilters {
  owners: string[]
  priorities: number[]
  dueDate: DueDateFilter
  tags: string[]
}

/** Smart-list filter the user picked from a sidebar (Calendar lineage). */
export type CalendarFilter =
  'all' | 'today' | 'upcoming' | 'overdue' | 'completed'

/**
 * The hub's three rendering modes (#424). `list` is the
 * Calendar/Agenda/Tasks lineage (flat rows grouped by time horizon);
 * `board` is the Kanban lineage (columns by status/priority);
 * `calendar` is the month/week grid (#425). Milestone #37 carried
 * list+board; #38 adds calendar so the segmented switcher can offer
 * all three even while the Board/Calendar renderers are stubs.
 */
export type DisplayMode = 'list' | 'board' | 'calendar'

/**
 * The Calendar display mode's sub-layout (#425). `month` (default) is
 * the full month grid; `week` is the 7-day strip. Ignored by list/board.
 */
export type CalendarSubMode = 'month' | 'week'

/**
 * The dimension to group rows/columns by. `none` keeps the query's
 * default priority-then-due-date order; the others re-order via the
 * unified query builder (./query.ts) so each group lands contiguous.
 *
 * `dueDate` is the canonical time-horizon bucketing (Overdue/Today/
 * Upcoming/Later/No Date) the legacy Tasks list has always rendered —
 * it's the default so the unmodified list experience survives the
 * grouping engine landing. The high-cardinality dimensions (tag/
 * notebook/section/page) are binned client-side by grouping.ts; the
 * query builder does not try to ORDER BY them server-side.
 */
export type GroupBy =
  | 'none'
  | 'status'
  | 'priority'
  | 'owner'
  | 'dueDate'
  | 'tag'
  | 'notebook'
  | 'section'
  | 'page'

/**
 * The within-group row ordering, picked from the Sort selector (#423).
 * `manual` honours `[order::]`; the others re-ORDER BY the query (or
 * re-sort client-side when a re-query isn't triggered). `dueDate` is the
 * legacy default so the unmodified list experience survives.
 */
export type SortMode =
  'manual' | 'dueDate' | 'priority' | 'title' | 'created' | 'owner'

/**
 * A named saved view for the hub (#323 saved-board lineage, generalized
 * to carry the groupBy dimension the unified hub adds). Persisted by
 * the future hub the way Kanban boards persist today; this module only
 * holds the live list.
 */
export interface SavedView {
  /** UUID generated client-side via crypto.randomUUID(). */
  id: string
  /** User-given view name; shown in the sidebar list. */
  name: string
  scope?: Scope
  filters?: TaskFilters
  groupBy?: GroupBy
}

/**
 * The unified hub state. Combines Kanban's scope+filters+override with
 * Calendar's focusDate+activeFilter, and adds the two new dimensions
 * (displayMode, groupBy) plus a saved-views list.
 */
export interface TaskHubState {
  displayMode: DisplayMode
  groupBy: GroupBy
  sort: SortMode
  scope: Scope
  /** True once the user manually picks a scope (#124). */
  scopeUserOverride: boolean
  filters: TaskFilters
  /** Picked from a mini calendar; YYYY-MM-DD. */
  focusDate: string
  /** Active smart-list filter; 'all' means no filter. */
  activeFilter: CalendarFilter
  /** Calendar display mode's sub-layout (#425); ignored by list/board. */
  calendarSubMode: CalendarSubMode
  savedViews: SavedView[]
}

const DEFAULT_FILTERS: TaskFilters = {
  owners: [],
  priorities: [],
  dueDate: '',
  tags: []
}

function freshDefaults(): TaskHubState {
  return {
    displayMode: 'list',
    groupBy: 'dueDate',
    sort: 'dueDate',
    scope: 'vault',
    scopeUserOverride: false,
    filters: { ...DEFAULT_FILTERS },
    focusDate: '',
    activeFilter: 'all',
    calendarSubMode: 'month',
    savedViews: []
  }
}

const _state: TaskHubState = $state(freshDefaults())

/** Read the current hub state (used by the hub component + sidebar). */
export function getTaskHubState(): TaskHubState {
  return _state
}

/** Switch the hub between list and board rendering. */
export function setDisplayMode(mode: DisplayMode): void {
  _state.displayMode = mode
}

/** Set the Calendar display mode's sub-layout (month/week). */
export function setCalendarSubMode(mode: CalendarSubMode): void {
  _state.calendarSubMode = mode
}

/** Change the grouping dimension. The query builder reads this on re-query. */
export function setGroupBy(g: GroupBy): void {
  _state.groupBy = g
}

/** Change the within-group sort. The query builder / ListView reads this. */
export function setSort(mode: SortMode): void {
  _state.sort = mode
}

/**
 * User-initiated scope change (scope button or sidebar radio). Flips
 * scopeUserOverride so subsequent navigation stops auto-narrowing
 * (#124 invariant, ported verbatim from kanbanSharedState).
 */
export function setScope(s: Scope): void {
  _state.scope = s
  _state.scopeUserOverride = true
}

/**
 * Navigation-driven scope auto-narrow (#124). Mutates scope WITHOUT
 * flipping scopeUserOverride, and is a NO-OP once the user has manually
 * picked a scope. Ported verbatim: the early return is the load-bearing
 * line — without it, nav would clobber the user's explicit pick.
 */
export function narrowScopeTo(s: Scope): void {
  if (_state.scopeUserOverride) return
  _state.scope = s
}

/** User reset (clicked "Follow" / reset). Re-enables navigation auto-narrow. */
export function clearScopeOverride(): void {
  _state.scopeUserOverride = false
}

/** Full filters replacement (the FilterBar / sidebar quick-toggle writes). */
export function setFilters(f: TaskFilters): void {
  _state.filters = f
}

/** Clear all active filters. */
export function clearFilters(): void {
  _state.filters = { ...DEFAULT_FILTERS }
}

/**
 * Set the focus date (from a mini-calendar click). Dispatches
 * `calendar:focus-date` on window so non-Svelte consumers (and the
 * future hub's main view) can listen — ported from focusState so the
 * event contract carries forward unchanged.
 */
export function setFocusDate(iso: string): void {
  _state.focusDate = iso
  window.dispatchEvent(
    new CustomEvent('calendar:focus-date', { detail: { date: iso } })
  )
}

/** Clear the focus date (called when the user dismisses a pick). */
export function clearFocusDate(): void {
  _state.focusDate = ''
  window.dispatchEvent(
    new CustomEvent('calendar:focus-date', { detail: { date: '' } })
  )
}

/** Set the active smart-list filter. */
export function setActiveFilter(f: CalendarFilter): void {
  _state.activeFilter = f
}

/** Reset the filter to 'all' (the X / "All Tasks" affordance). */
export function clearActiveFilter(): void {
  _state.activeFilter = 'all'
}

/** Save a named view to the sidebar list. */
export function saveView(view: SavedView): void {
  const existing = _state.savedViews.findIndex((v) => v.id === view.id)
  if (existing >= 0) {
    _state.savedViews[existing] = view
  } else {
    _state.savedViews.push(view)
  }
}

/**
 * Apply a saved view. Restores whichever of scope / filters / groupBy
 * the view carries, and flips the override flag (clicking a saved view
 * is a user intent — same contract as kanbanSharedState.applySavedBoard).
 */
export function applySavedView(view: SavedView): void {
  if (view.scope !== undefined) {
    _state.scope = view.scope
    _state.scopeUserOverride = true
  }
  if (view.filters !== undefined) {
    _state.filters = { ...view.filters }
  }
  if (view.groupBy !== undefined) {
    _state.groupBy = view.groupBy
  }
}

/**
 * Reset the unified hub state to defaults. Called by the loader's
 * vault:closing handler so scope/filters/override/focus from the
 * previous vault don't linger into the next — same contract as the
 * two old reset functions, extended to cover the new fields.
 */
export function resetTaskHubState(): void {
  const defaults = freshDefaults()
  _state.displayMode = defaults.displayMode
  _state.groupBy = defaults.groupBy
  _state.sort = defaults.sort
  _state.scope = defaults.scope
  _state.scopeUserOverride = defaults.scopeUserOverride
  _state.filters = defaults.filters
  _state.focusDate = defaults.focusDate
  _state.activeFilter = defaults.activeFilter
  _state.calendarSubMode = defaults.calendarSubMode
  _state.savedViews = []
}
