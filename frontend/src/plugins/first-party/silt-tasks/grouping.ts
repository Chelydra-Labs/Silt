// Pure grouping helpers for the Tasks hub (#423). Given a flat list of
// TaskDetail rows + a GroupBy dimension, produce ordered GroupSections
// the renderer can map over. Sorting within a section is the caller's
// concern (the query's ORDER BY, or ListView's client-side re-sort);
// grouping.ts only assigns rows to buckets.
//
// The 'dueDate' bins produce the same data-group keys the legacy Tasks
// ListView has always rendered (overdue/today/upcoming/later/undated),
// so the existing tests stay byte-exact when ListView delegates to
// binByDimension instead of its own $derived filters.

import { plusDaysISO } from '../../sdk'
import type { TaskDetail } from './types'
import { laneLabel } from './types'
import type { GroupBy } from './state.svelte'

export interface GroupSection {
  /** Stable data-group key (used as the DOM attribute + test selector). */
  key: string
  /** Human-readable heading (used as aria-label). */
  label: string
  items: TaskDetail[]
}

const STATUS_ORDER: readonly string[] = ['TODO', 'DOING', 'DONE']

function dueDateBucket(
  iso: string | undefined | null,
  today: string
): 'overdue' | 'today' | 'upcoming' | 'later' | 'undated' {
  if (!iso) return 'undated'
  if (iso < today) return 'overdue'
  if (iso === today) return 'today'
  // Tomorrow (today+1) through today+7 share the "Upcoming" bucket so the
  // 5-bucket shape matches the legacy ListView's data-group keys exactly.
  const weekAhead = plusDaysISO(today, 7)
  if (iso >= plusDaysISO(today, 1) && iso <= weekAhead) return 'upcoming'
  return 'later'
}

function binByDueDate(rows: TaskDetail[], today: string): GroupSection[] {
  const buckets: Record<
    'overdue' | 'today' | 'upcoming' | 'later' | 'undated',
    TaskDetail[]
  > = {
    overdue: [],
    today: [],
    upcoming: [],
    later: [],
    undated: []
  }
  for (const r of rows) {
    buckets[dueDateBucket(r.due_date, today)].push(r)
  }
  return [
    { key: 'overdue', label: 'Overdue', items: buckets.overdue },
    { key: 'today', label: 'Today', items: buckets.today },
    { key: 'upcoming', label: 'Upcoming', items: buckets.upcoming },
    { key: 'later', label: 'Later', items: buckets.later },
    { key: 'undated', label: 'No Date', items: buckets.undated }
  ]
}

function binByStatus(rows: TaskDetail[]): GroupSection[] {
  const byStatus = new Map<string, TaskDetail[]>()
  const customOrder: string[] = []
  for (const r of rows) {
    const s = r.status || 'TODO'
    if (!byStatus.has(s)) {
      byStatus.set(s, [])
      if (!STATUS_ORDER.includes(s)) customOrder.push(s)
    }
    byStatus.get(s)!.push(r)
  }
  const ordered = [
    ...STATUS_ORDER.filter((s) => byStatus.has(s)),
    ...customOrder.sort((a, b) => a.localeCompare(b))
  ]
  return ordered.map((s) => ({
    key: s,
    label: laneLabel(s),
    items: byStatus.get(s) ?? []
  }))
}

/**
 * The trailing bucket label for items with an empty/null value, per
 * dimension. Empty string means "no trailing bucket" (the dimension
 * never produces an empty value, e.g. status defaults to TODO).
 */
function unassignedLabel(dim: GroupBy): string {
  switch (dim) {
    case 'owner':
      return 'Unassigned'
    case 'priority':
      return 'No Priority'
    case 'tag':
      return 'No Tag'
    case 'notebook':
      return 'No Notebook'
    case 'section':
      return 'No Section'
    case 'page':
      return 'No Page'
    default:
      return 'Unassigned'
  }
}

/**
 * Pull the dimension's value out of a row, normalized to a non-empty
 * string or '' (which routes to the trailing Unassigned bucket).
 */
function rowValue(row: TaskDetail, dim: GroupBy): string {
  switch (dim) {
    case 'owner':
      return (row.owner ?? '').trim()
    case 'priority':
      // Priority 0 is the legacy "unset" sentinel — treat as empty so
      // unprioritized tasks land in the "No Priority" bucket.
      return row.priority && row.priority > 0 ? String(row.priority) : ''
    case 'notebook':
      return (row.notebook ?? '').trim()
    case 'section':
      return (row.section ?? '').trim()
    case 'page':
      return (row.page ?? '').trim()
    default:
      return ''
  }
}

function valueLabel(value: string, dim: GroupBy): string {
  if (dim === 'priority') {
    // PRIORITY_LABELS maps 1→Critical, 2→Normal, 3→Low; fall back to the
    // raw numeric for any custom value.
    const labels: Record<string, string> = {
      '1': 'Critical',
      '2': 'Normal',
      '3': 'Low'
    }
    return labels[value] ?? `Priority ${value}`
  }
  return value
}

/**
 * Bin rows by a single-value dimension (owner/priority/notebook/section/
 * page). Non-empty values are sorted alphabetically (numeric-asc for
 * priority); the empty/zero rows form a trailing "Unassigned"/"No X"
 * section so they're always discoverable at the bottom.
 */
function binByScalar(
  rows: TaskDetail[],
  dim: Exclude<GroupBy, 'none' | 'dueDate' | 'status' | 'tag'>
): GroupSection[] {
  const groups = new Map<string, TaskDetail[]>()
  const unassigned: TaskDetail[] = []
  for (const r of rows) {
    const v = rowValue(r, dim)
    if (!v) {
      unassigned.push(r)
      continue
    }
    if (!groups.has(v)) groups.set(v, [])
    groups.get(v)!.push(r)
  }
  const keys = [...groups.keys()].sort((a, b) => {
    if (dim === 'priority') return Number(a) - Number(b)
    return a.localeCompare(b)
  })
  const sections = keys.map((k) => ({
    key: `${dim}-${k}`,
    label: valueLabel(k, dim),
    items: groups.get(k) ?? []
  }))
  if (unassigned.length > 0) {
    sections.push({
      key: `${dim}-__unassigned__`,
      label: unassignedLabel(dim),
      items: unassigned
    })
  }
  return sections
}

/**
 * Bin rows by tag. A row with multiple tags appears once per tag
 * (multi-membership); rows with no tags fall into the trailing
 * "No Tag" bucket. Tag order is alphabetical.
 */
function binByTag(rows: TaskDetail[]): GroupSection[] {
  const groups = new Map<string, TaskDetail[]>()
  const unassigned: TaskDetail[] = []
  for (const r of rows) {
    const tags = (r.tags ?? '')
      .split('|')
      .map((t) => t.trim())
      .filter(Boolean)
    if (tags.length === 0) {
      unassigned.push(r)
      continue
    }
    for (const t of tags) {
      if (!groups.has(t)) groups.set(t, [])
      groups.get(t)!.push(r)
    }
  }
  const keys = [...groups.keys()].sort((a, b) => a.localeCompare(b))
  const sections = keys.map((k) => ({
    key: `tag-${k}`,
    label: k,
    items: groups.get(k) ?? []
  }))
  if (unassigned.length > 0) {
    sections.push({
      key: 'tag-__unassigned__',
      label: unassignedLabel('tag'),
      items: unassigned
    })
  }
  return sections
}

/**
 * Bin rows into GroupSections for the given dimension. Always returns
 * the canonical bucket set for that dimension in the canonical order —
 * empty buckets are kept (length 0) so the caller can decide whether
 * to render them.
 */
export function binByDimension(
  rows: TaskDetail[],
  groupBy: GroupBy,
  ctx: { today: string }
): GroupSection[] {
  switch (groupBy) {
    case 'none':
      return [{ key: 'all', label: 'All Tasks', items: [...rows] }]
    case 'dueDate':
      return binByDueDate(rows, ctx.today)
    case 'status':
      return binByStatus(rows)
    case 'tag':
      return binByTag(rows)
    case 'owner':
    case 'priority':
    case 'notebook':
    case 'section':
    case 'page':
      return binByScalar(rows, groupBy)
    default:
      return [{ key: 'all', label: 'All Tasks', items: [...rows] }]
  }
}
