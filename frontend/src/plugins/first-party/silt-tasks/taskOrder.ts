// Client-side task ordering shared by the List and Board views (#1091).
// Mirrors the SQL ORDER BY in query.ts for the active SortMode so a sort or
// grouping change without a re-query produces the same order the next query
// would. The pin primary key partitions pinned rows ahead of unpinned ones;
// each subset keeps the active SortMode.
import type { SortMode } from './state.svelte'
import type { TaskDetail } from './types'

export interface TaskOrderOptions {
  // While the shortlist filter is the active lens (#1092) the rank column
  // leads ahead of the pin-first key, mirroring query.ts's rankLead
  // composition. Unranked rows sort last (the SQL predicate guarantees
  // every row is ranked when the filter is on; nulls-last is the defensive
  // order for off-filter strays).
  shortlistFirst?: boolean
}

function compareRankFirst(a: TaskDetail, b: TaskDetail): number {
  const ar = a.shortlist_rank
  const br = b.shortlist_rank
  if ((ar == null) !== (br == null)) return ar == null ? 1 : -1
  if (ar !== br && ar != null && br != null) return ar - br
  return 0
}

export function compareTasks(
  sort: SortMode,
  a: TaskDetail,
  b: TaskDetail,
  options?: TaskOrderOptions
): number {
  // Rank leads the pin key while the shortlist filter is active — the
  // curated 0..3 order is the filter's whole point (#1092).
  if (options?.shortlistFirst) {
    const byRank = compareRankFirst(a, b)
    if (byRank !== 0) return byRank
  }
  // Pin leads unconditionally otherwise: Board lanes include completed rows
  // and their pin token still sorts first (they show no control/glyph);
  // List's completed path bypasses this comparator via file_date ordering.
  if (a.pinned !== b.pinned) return a.pinned ? -1 : 1
  switch (sort) {
    case 'manual': {
      const am = a.manual_order ?? 0
      const bm = b.manual_order ?? 0
      if ((am === 0) !== (bm === 0)) return am === 0 ? 1 : -1
      if (am !== bm) return am - bm
      break
    }
    case 'priority': {
      if (a.priority !== b.priority) return a.priority - b.priority
      break
    }
    case 'title': {
      const c = (a.clean_content ?? '').localeCompare(b.clean_content ?? '')
      if (c !== 0) return c
      break
    }
    case 'created': {
      const ac = a.created_at || '9999'
      const bc = b.created_at || '9999'
      if (ac !== bc) return ac.localeCompare(bc)
      break
    }
    case 'owner': {
      const ao = a.owner || '~'
      const bo = b.owner || '~'
      if (ao !== bo) return ao.localeCompare(bo)
      break
    }
    case 'modified': {
      // Recently modified first; empty/null as oldest.
      const am = a.modified_at || '0000'
      const bm = b.modified_at || '0000'
      if (am !== bm) return bm.localeCompare(am)
      break
    }
    case 'estimate': {
      // Null estimates last; then ascending minutes.
      const ae = a.estimate_minutes
      const be = b.estimate_minutes
      const aNull = ae == null
      const bNull = be == null
      if (aNull !== bNull) return aNull ? 1 : -1
      if (!aNull && !bNull && ae !== be) return ae - be
      break
    }
    case 'dueDate':
    default:
      // Fall through to the due-date tiebreaker below.
      break
  }
  return (a.due_date || '9999-12-31').localeCompare(b.due_date || '9999-12-31')
}

export function sortTasks(
  sort: SortMode,
  rows: TaskDetail[],
  options?: TaskOrderOptions
): TaskDetail[] {
  return [...rows].sort((a, b) => compareTasks(sort, a, b, options))
}
