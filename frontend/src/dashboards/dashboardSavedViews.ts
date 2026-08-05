// Pure saved-view helpers for the typed-notes dashboard (#863). The dashboard
// has no code-defined system views today (only user-defined ones), but the
// shape mirrors silt-tasks/settings.ts so a future system-view set drops in:
// coerce → load (merge + dedup) → persist (strip system) against the
// `ui.dashboards.typed_notes.saved_views[]` namespace.
//
// Pure: no Svelte, no bindings, no settings store. The component wires the
// read (settings.config) and write (SaveSystemConfig); these functions take
// the raw slice as an argument so they're unit-testable without IPC mocks.

import {
  mergeViewById,
  stripSystemFlag,
  type SavedViewBase
} from '../lib/viewEngine/viewState'
import { viewMatchesState } from '../lib/viewEngine/savedViews'
import type { FilterState, SortState } from './dashboards'

/**
 * A user-defined dashboard view. Snapshots the dimensions a user can tweak on
 * the dashboard chrome (filter / sort / group-by / view-mode) plus the type
 * the view belongs to — a view only applies to its own type, so the saved-
 * views list filters by `typeId` for the active type.
 */
export interface DashboardSavedView extends SavedViewBase {
  typeId: string
  filter?: FilterState
  sort?: SortState
  /** '' means "no grouping" (a real snapshot value, distinct from omitted). */
  groupBy?: string
  viewMode?: 'list' | 'board'
}

/** The live dashboard state a view is compared against for active-highlight. */
export interface DashboardViewState {
  typeId: string
  filter: FilterState
  sort: SortState
  groupBy: string
  viewMode: 'list' | 'board'
}

/** Dims the matcher compares — `undefined`-by-view dims are skipped. */
const DASHBOARD_MATCH_DIMS = [
  'typeId',
  'filter',
  'sort',
  'groupBy',
  'viewMode'
] as const

/**
 * Validate + coerce a single persisted entry. Unknown / malformed values are
 * dropped rather than throwing — the user may have hand-edited the YAML, and
 * we'd rather land somewhere sane than lose the whole list over one bad row.
 * Missing required fields (id / name / typeId) DO drop the entry: a view
 * with no type can't be applied, and one with no id/name can't be shown.
 *
 * The `sys-` id prefix is reserved for future code-defined system views; a
 * user entry carrying one is rejected so it can't shadow a built-in.
 */
export function coerceDashboardSavedView(
  raw: unknown
): DashboardSavedView | null {
  if (!raw || typeof raw !== 'object') return null
  const r = raw as Record<string, unknown>
  if (typeof r.id !== 'string' || r.id.length === 0) return null
  if (typeof r.name !== 'string' || r.name.length === 0) return null
  if (r.id.startsWith('sys-')) return null
  if (typeof r.typeId !== 'string' || r.typeId.length === 0) return null

  const v: DashboardSavedView = { id: r.id, name: r.name, typeId: r.typeId }

  if (r.filter && typeof r.filter === 'object') {
    const f: Record<string, string> = {}
    for (const [k, val] of Object.entries(
      r.filter as Record<string, unknown>
    )) {
      // Only string values survive — the IPC's filter bag is Record<string,string>.
      if (typeof val === 'string') f[k] = val
    }
    v.filter = f
  }

  if (r.sort && typeof r.sort === 'object') {
    const s = r.sort as Record<string, unknown>
    v.sort = {
      property: typeof s.property === 'string' ? s.property : '',
      desc: s.desc === true
    }
  }

  if (typeof r.groupBy === 'string') v.groupBy = r.groupBy
  if (r.viewMode === 'list' || r.viewMode === 'board') v.viewMode = r.viewMode
  return v
}

/**
 * Load the user saved-views list from a raw config slice. Coerces each entry,
 * drops invalid ones, and dedupes by id (first wins — colliding ids in
 * hand-edited YAML shouldn't surface duplicates). System views aren't merged
 * in here because the dashboard defines none today; the component composes
 * them via mergeViewById if/when system views are added.
 *
 * Dropped entries (malformed / duplicate id) emit a single console.warn so a
 * user who hand-edited the YAML and broke a view gets a signal that it was
 * discarded — fail-loud rather than a silent loss.
 */
export function loadDashboardSavedViews(
  rawUser: unknown
): DashboardSavedView[] {
  if (!Array.isArray(rawUser)) return []
  const seen = new Set<string>()
  const out: DashboardSavedView[] = []
  let dropped = 0
  for (const entry of rawUser) {
    const v = coerceDashboardSavedView(entry)
    if (!v || seen.has(v.id)) {
      dropped++
      continue
    }
    seen.add(v.id)
    out.push(v)
  }
  if (dropped > 0) {
    console.warn(
      `dashboards: dropped ${dropped} invalid/duplicate saved-view entr${dropped === 1 ? 'y' : 'ies'} from config (hand-edited YAML?)`
    )
  }
  return out
}

/**
 * Strip the system marker before persisting. Mirrors silt-tasks'
 * persistSavedViews: system views are re-derived from code on every load
 * (they never consume YAML budget), and a stale `system: true` in YAML would
 * lock a user view out of deletion next load. The dashboard has no system
 * views today, but the strip stays defensive.
 */
export function persistableDashboardSavedViews(
  views: DashboardSavedView[]
): Omit<DashboardSavedView, 'system'>[] {
  return stripSystemFlag(views)
}

/** Merge two saved-view lists by id (later wins). */
export function mergeDashboardSavedViews(
  ...lists: DashboardSavedView[][]
): DashboardSavedView[] {
  return mergeViewById(...lists)
}

/**
 * "Does this view describe the live dashboard state?" — the active-highlight
 * check. Delegates to the generic partial-snapshot matcher with dashboard-
 * specific equality for the two object dims (filter is a string bag, sort is
 * a {property,desc} struct); the scalars (typeId / groupBy / viewMode) use
 * strict equality. A view that omits a dim matches any value for it.
 */
export function viewMatchesDashboardState(
  view: DashboardSavedView,
  state: DashboardViewState
): boolean {
  return viewMatchesState(
    view as unknown as Record<string, unknown>,
    state as unknown as Record<string, unknown>,
    DASHBOARD_MATCH_DIMS,
    (dim) => {
      if (dim === 'filter') return filterStateEqual
      if (dim === 'sort') return sortStateEqual
      return undefined
    }
  )
}

/** Structural equality for the FilterState bag (same keys + values). */
function filterStateEqual(a: unknown, b: unknown): boolean {
  if (!a || typeof a !== 'object') return a === b
  if (!b || typeof b !== 'object') return false
  const aa = a as Record<string, unknown>
  const bb = b as Record<string, unknown>
  const ka = Object.keys(aa)
  const kb = Object.keys(bb)
  if (ka.length !== kb.length) return false
  for (const k of ka) if (aa[k] !== bb[k]) return false
  return true
}

/** Structural equality for the SortState struct. */
function sortStateEqual(a: unknown, b: unknown): boolean {
  if (!a || typeof a !== 'object') return a === b
  if (!b || typeof b !== 'object') return false
  const aa = a as { property?: unknown; desc?: unknown }
  const bb = b as { property?: unknown; desc?: unknown }
  return aa.property === bb.property && !!aa.desc === !!bb.desc
}
