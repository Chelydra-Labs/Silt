// Unified SQL builder for the Tasks hub (#419, milestone #37 phase 4).
//
// Lifted from silt-kanban/query.ts (the most capable builder of the
// three task surfaces — Tasks/Calendar/Agenda inline simpler variants)
// and extended with two optional levers the unified hub needs:
//   - `groupBy`: re-orders the result so rows/columns of the same group
//     land contiguous (the hub's list mode groups by status / priority /
//     owner / dueDate; board mode groups by status or priority).
//   - `window`: adds a due-date WHERE window so Calendar-style queries
//     can ask for "just this month" without re-deriving the SQL inline.
//
// PURELY ADDITIVE this phase: no existing consumer imports this yet.
// silt-kanban/query.ts stays live until milestone #38 migrates the
// Kanban consumer, then it is deleted. The base SELECT, scope branches,
// filter branches, and the default ORDER BY are byte-for-byte the
// proven Kanban builder — only the two optional params are new.
//
// Pure function: no side effects, no $state, no IPC. All values flow
// through `?` placeholders; nothing is string-interpolated into the SQL.

import { plusDaysISO } from '../../sdk'
import type { DueDateFilter, GroupBy, Scope, TaskFilters } from './state.svelte'

/**
 * The shape `buildQuery` reads from the PluginContext. Pass an explicit
 * narrow object so the query builder is obviously pure and unit-testable
 * without instantiating a real PluginContext.
 */
export interface QueryCtxLike {
  activeNotebook: string
  activeSection: string
  activePage: string
  today: string
}

/**
 * Optional levers the unified hub adds on top of the Kanban builder.
 * Both default to absent so a plain `buildQuery(scope, filters, ctx)`
 * call produces the exact same SQL as the legacy builder.
 */
export interface BuildQueryOptions {
  /**
   * Re-orders rows so each group is contiguous. Does not add a WHERE
   * clause — grouping is a sort concern, not a filter concern.
   */
  groupBy?: GroupBy
  /**
   * Bounds the due date to a [start, end] inclusive window. Used by
   * Calendar-style queries (e.g. "this month's grid"). Both bounds
   * are `?`-bound; `null`/empty due dates fall outside any window.
   */
  window?: { start: string; end: string }
}

/**
 * Build a `b.id IN (?, ?, ...)` placeholder list for an arbitrary number of
 * values. Used so the owner and priority WHERE-fragments share one shape.
 */
function inClause(column: string, values: unknown[]): string {
  return `${column} IN (${values.map(() => '?').join(', ')})`
}

/**
 * The ORDER BY clause for a given groupBy. Each clause puts the grouping
 * column first (so equal values are contiguous) and falls back to the
 * canonical priority-then-due-date tiebreaker so within-group order is
 * stable and matches the legacy board when groupBy is 'none'/'priority'.
 *
 * 'priority' intentionally shares the 'none' clause — the legacy order
 * already sorts by priority first, so grouping by priority is a no-op
 * on the sort and the hub bins client-side off the leading column.
 */
function orderByFor(groupBy: GroupBy | undefined): string {
  const tiebreaker = "COALESCE(t.due_date, '9999-12-31') ASC, t.priority ASC"
  switch (groupBy) {
    case 'status':
      return ` ORDER BY t.status ASC, ${tiebreaker}`
    case 'owner':
      return ` ORDER BY t.owner ASC, ${tiebreaker}`
    case 'dueDate':
      return ` ORDER BY ${tiebreaker}`
    case 'priority':
    case 'none':
    default:
      // Legacy Kanban order: priority first, due date as tiebreaker.
      return ` ORDER BY t.priority ASC, COALESCE(t.due_date, '9999-12-31') ASC`
  }
}

/**
 * Build the parameterised SQL for the unified Tasks hub query.
 *
 * Two phases:
 *  1. Scope WHERE — narrows by vault / notebook / section / page using the
 *     active navigation triple.
 *  2. Filter WHERE — owners (IN), priorities (IN), due-date quick-pick
 *     (overdue / today / week / none), tags (subquery on the `tags`
 *     table), and the optional Calendar-style date window. All bound via
 *     `?` params — never string-interpolated.
 *
 * Due dates compare against the LOCAL day (ctxLike.today), not SQLite's
 * date('now') which is UTC and produced off-by-one results near local
 * midnight (#118). due_date is stored as YYYY-MM-DD text, so lexicographic
 * comparison against the param matches the old date('now') semantics exactly.
 *
 * `t.pinned` is a tri-state cache column (#135) — NULL/0/1 for
 * absent/false/true. The board treats only `1` as pinned.
 */
export function buildQuery(
  s: Scope,
  f: TaskFilters,
  ctx: QueryCtxLike,
  options?: BuildQueryOptions
): { sql: string; params: unknown[] } {
  const baseSelect = `SELECT b.id, b.notebook, b.section, b.page, b.file_date, b.line_number,
           b.clean_content, t.status, t.owner, t.start_date, t.due_date, t.priority,
           t.pinned, t.progress, t.recur AS recurrence, t.comments_count, t.links_count,
           t.created_at, t.completed_at, t.manual_order,
           (SELECT GROUP_CONCAT(raw_path, '|') FROM tags WHERE block_id = b.id) AS tags,
           (SELECT GROUP_CONCAT(blocked_by_id, '|') FROM task_dependencies WHERE block_id = b.id) AS blocked_by,
           EXISTS (
             SELECT 1 FROM task_dependencies d
             JOIN tasks bt ON bt.block_id = d.blocked_by_id
             WHERE d.block_id = b.id AND bt.status != 'DONE'
           ) AS is_blocked
    FROM blocks b JOIN tasks t ON b.id = t.block_id`
  const where: string[] = []
  const params: unknown[] = []
  switch (s) {
    case 'vault':
      break
    case 'notebook':
      where.push('b.notebook = ?')
      params.push(ctx.activeNotebook)
      break
    case 'section':
      where.push('b.notebook = ?', 'b.section = ?')
      params.push(ctx.activeNotebook, ctx.activeSection)
      break
    case 'page':
      where.push('b.notebook = ?', 'b.section = ?', 'b.page = ?')
      params.push(ctx.activeNotebook, ctx.activeSection, ctx.activePage)
      break
  }
  if (f.owners.length) {
    where.push(inClause('t.owner', f.owners))
    params.push(...f.owners)
  }
  if (f.priorities.length) {
    where.push(inClause('t.priority', f.priorities))
    params.push(...f.priorities)
  }
  if (f.dueDate) {
    const today = ctx.today
    const due = f.dueDate as DueDateFilter
    if (due === 'overdue') {
      where.push('t.due_date < ?')
      params.push(today)
    } else if (due === 'today') {
      where.push('t.due_date = ?')
      params.push(today)
    } else if (due === 'week') {
      where.push('t.due_date BETWEEN ? AND ?')
      params.push(today, plusDaysISO(today, 7))
    } else if (due === 'none') {
      where.push("(t.due_date IS NULL OR t.due_date = '')")
    }
  }
  if (f.tags.length) {
    where.push(
      `b.id IN (SELECT block_id FROM tags WHERE raw_path IN (${f.tags
        .map(() => '?')
        .join(', ')}))`
    )
    params.push(...f.tags)
  }
  if (options?.window) {
    where.push('t.due_date >= ?', 't.due_date <= ?')
    params.push(options.window.start, options.window.end)
  }
  const orderBy = orderByFor(options?.groupBy)
  const whereClause = where.length
    ? ' WHERE ' + where.join(' AND ')
    : ' WHERE 1=1'
  return { sql: baseSelect + whereClause + orderBy, params }
}
