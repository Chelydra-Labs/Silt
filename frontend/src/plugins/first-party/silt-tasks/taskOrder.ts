// Client-side task ordering shared by the List and Board views (#1091).
// Mirrors the SQL ORDER BY in query.ts for the active SortMode so a sort or
// grouping change without a re-query produces the same order the next query
// would. The pin primary key partitions pinned rows ahead of unpinned ones;
// each subset keeps the active SortMode.
import type { SortMode } from './state.svelte'
import type { TaskDetail } from './types'

export function compareTasks(
  sort: SortMode,
  a: TaskDetail,
  b: TaskDetail
): number {
  // Callers pass open rows only, so a pinned DONE task never reaches here.
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

export function sortTasks(sort: SortMode, rows: TaskDetail[]): TaskDetail[] {
  return [...rows].sort((a, b) => compareTasks(sort, a, b))
}
