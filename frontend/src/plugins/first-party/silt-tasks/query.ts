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
import type {
  CalendarFilter,
  DueDateFilter,
  GroupBy,
  Scope,
  SortMode,
  TaskFilters
} from './state.svelte'

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
   * Within-group row ordering (#423 Sort selector). When absent, the
   * groupBy-driven ORDER BY is used unchanged (backward compatible).
   * When present, it overrides the within-group tiebreaker — the
   * grouping column (status/owner) still leads so groups stay contiguous,
   * but rows inside a group respect the chosen sort.
   */
  sort?: SortMode
  /**
   * Bounds the due date to a [start, end] inclusive window. Used by
   * Calendar-style queries (e.g. "this month's grid"). Both bounds
   * are `?`-bound; `null`/empty due dates fall outside any window.
   */
  window?: { start: string; end: string }
  /**
   * Sidebar smart-list filter (#432). When set and != 'all', adds a WHERE
   * constraint that narrows the result set to the smart list's scope.
   * Date-based smart lists (today/overdue/upcoming) take PRECEDENCE over
   * filters.dueDate — the smart list wins so clicking "Today" after
   * selecting the "Overdue" quick-pick doesn't produce an empty
   * intersection. 'completed' adds a status='DONE' clause alongside the
   * existing filters; 'all' is a no-op.
   */
  activeFilter?: CalendarFilter
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
 *
 * The high-cardinality dimensions ('tag'/'notebook'/'section'/'page')
 * also share the 'none' clause: their groups are derived client-side in
 * grouping.ts (tag requires a multi-membership split across pipe-delimited
 * values; notebook/section/page are dimensions of the row's location, not
 * columns the query needs to surface as a sort key). The row set is the
 * same either way; only the binning differs.
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
    case 'tag':
    case 'notebook':
    case 'section':
    case 'page':
    default:
      // Legacy Kanban order: priority first, due date as tiebreaker.
      return ` ORDER BY t.priority ASC, COALESCE(t.due_date, '9999-12-31') ASC`
  }
}

/**
 * The within-group ORDER BY clause for a given SortMode. Always ends with
 * the canonical tiebreaker (due date then priority) so equal-primary-key
 * rows stay in a deterministic order.
 *
 * SQLite has no NULLS LAST syntax; the CASE WHEN ... THEN 1 ELSE 0 trick
 * emulates it for manual_order, and COALESCE/NULLIF push empty-string
 * sentinels ('', the SQL row mapper's NULL coercion) to the bottom for
 * owner / created_at.
 */
function sortClauseFor(sort: SortMode): string {
  const tiebreaker = "COALESCE(t.due_date, '9999-12-31') ASC, t.priority ASC"
  switch (sort) {
    case 'manual':
      return ` ORDER BY CASE WHEN t.manual_order IS NULL THEN 1 ELSE 0 END, t.manual_order ASC, ${tiebreaker}`
    case 'priority':
      return ` ORDER BY t.priority ASC, ${tiebreaker}`
    case 'title':
      return ` ORDER BY b.clean_content ASC, ${tiebreaker}`
    case 'created':
      return ` ORDER BY CASE WHEN t.created_at IS NULL OR t.created_at = '' THEN '9999' ELSE t.created_at END ASC, ${tiebreaker}`
    case 'owner':
      return ` ORDER BY COALESCE(NULLIF(t.owner, ''), '~') ASC, ${tiebreaker}`
    case 'modified':
      // Recently modified first; null/empty modified_at sorts as oldest.
      return ` ORDER BY CASE WHEN t.modified_at IS NULL OR t.modified_at = '' THEN '0000' ELSE t.modified_at END DESC, ${tiebreaker}`
    case 'estimate':
      // Null estimates last; then ascending minutes.
      return ` ORDER BY CASE WHEN t.estimate_minutes IS NULL THEN 1 ELSE 0 END, t.estimate_minutes ASC, ${tiebreaker}`
    case 'dueDate':
    default:
      return ` ORDER BY ${tiebreaker}`
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
           t.modified_at, t.estimate_minutes, t.subtask_total, t.subtask_done,
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
  // Smart-list precedence (#432): date-based active filters (today/overdue/
  // upcoming) override filters.dueDate so clicking a smart list after picking
  // a due-date quick-pick doesn't produce an empty intersection.
  const af = options?.activeFilter
  const smartListIsDateBased =
    af === 'today' || af === 'overdue' || af === 'upcoming'
  if (f.dueDate && !smartListIsDateBased) {
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
  // Stale filter (#440): open tasks with no modified_at or last touch older
  // than 30 local days. Compare date-only prefix so ISO timestamps work.
  if (f.stale) {
    const cutoff = plusDaysISO(ctx.today, -30)
    where.push(
      "t.status != 'DONE' AND (t.modified_at IS NULL OR t.modified_at = '' OR substr(t.modified_at, 1, 10) < ?)"
    )
    params.push(cutoff)
  }
  if (options?.window) {
    where.push('t.due_date >= ?', 't.due_date <= ?')
    params.push(options.window.start, options.window.end)
  }
  // Smart-list filter (#432). Maps the sidebar's semantic filter to WHERE
  // clauses matching the count-query semantics in Sidebar.svelte.
  if (af && af !== 'all') {
    const today = ctx.today
    if (af === 'today') {
      where.push("t.status != 'DONE'", 't.due_date = ?')
      params.push(today)
    } else if (af === 'overdue') {
      where.push("t.status != 'DONE'", 't.due_date < ?')
      params.push(today)
    } else if (af === 'upcoming') {
      where.push("t.status != 'DONE'", 't.due_date > ?', 't.due_date <= ?')
      params.push(today, plusDaysISO(today, 7))
    } else if (af === 'completed') {
      where.push("t.status = 'DONE'")
    }
  }
  const orderBy = composeOrderBy(options?.groupBy, options?.sort)
  const whereClause = where.length
    ? ' WHERE ' + where.join(' AND ')
    : ' WHERE 1=1'
  return { sql: baseSelect + whereClause + orderBy, params }
}

/**
 * Compose the final ORDER BY. When `sort` is present it always wins for
 * within-group order; if `groupBy` is also present and is one of the
 * server-sortable dimensions (status/owner), the grouping column stays
 * leading so groups remain contiguous, and the sort clause provides the
 * within-group tiebreaker. If only `groupBy` is present, the legacy
 * orderByFor clause is used unchanged. If neither is present, the legacy
 * 'none' priority-first order falls out of orderByFor.
 */
function composeOrderBy(
  groupBy: GroupBy | undefined,
  sort: SortMode | undefined
): string {
  if (!sort) return orderByFor(groupBy)
  const within = sortClauseFor(sort).replace('ORDER BY ', '')
  if (groupBy === 'status') {
    return ` ORDER BY t.status ASC, ${within}`
  }
  if (groupBy === 'owner') {
    // Group by owner: leading key is owner (NULLIF so empty sorts into
    // the trailing "Unassigned" bucket client-side); within-group uses
    // the sort clause, but a sort=owner would duplicate the column —
    // harmless and keeps the code path uniform.
    return ` ORDER BY COALESCE(NULLIF(t.owner, ''), '~') ASC, ${within}`
  }
  // 'none', 'priority', 'dueDate', 'tag', 'notebook', 'section', 'page',
  // and undefined: the sort clause is the whole ORDER BY.
  return sortClauseFor(sort)
}
