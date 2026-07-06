import type { TaskStatus } from '../../sdk'

/**
 * The cross-surface task contract. Every task-edit surface (Tasks list,
 * Kanban board, future Calendar/Agenda) consumes this single shape so a task
 * edited from one surface behaves identically to one edited from another.
 *
 * Generalized from the former silt-kanban `KanbanCard`; the SQL projections in
 * `silt-kanban/query.ts` and `silt-tasks/Tasks.svelte` produce this shape.
 */
export interface TaskDetail {
  id: string
  notebook: string
  section: string
  page: string
  file_date: string
  clean_content: string
  status: TaskStatus
  owner: string
  start_date: string
  due_date: string
  priority: number
  pinned: boolean
  progress: number
  // Natural-language recurrence rule from [recur:: RULE] (#296). Empty/null
  // for one-off tasks; e.g. 'every week'.
  recurrence: string
  // Pipe-delimited dependent-task UUIDs from the task_dependencies join
  // table (#301). Absent/empty when the task has no prerequisites. The
  // is_blocked derived flag below drives the lock badge + DONE guard.
  blocked_by?: string
  // Derived: 1 when the task has at least one non-DONE prerequisite, else 0.
  // Computed by a correlated subquery in the SQL so the badge and the
  // DONE-confirm guard read it without a second round-trip.
  is_blocked?: number
  comments_count: number
  links_count: number
  // Pipe-delimited raw tag paths from a GROUP_CONCAT subquery; absent
  // when the task has no tags.
  tags?: string
  // Timestamps + manual order from the [created::]/[completed::]/[order::]
  // task tokens (#417). Stored as ISO-ish TEXT / INTEGER on the tasks table;
  // the SQL row mappers coerce SQL NULL → '' / 0 so these non-optional types
  // hold. Empty/0 means "unknown" — the common case for pre-existing tasks
  // that predate the columns.
  created_at: string
  completed_at: string
  manual_order: number
}

export const PRIORITY_LABELS: Record<number, string> = {
  1: 'Critical',
  2: 'Normal',
  3: 'Low'
}

// Standard statuses get friendly labels; custom lanes show their raw name.
export function laneLabel(s: string): string {
  if (s === 'TODO') return 'To Do'
  if (s === 'DOING') return 'In Progress'
  if (s === 'DONE') return 'Done'
  return s
}

export function priorityClass(p: number): string {
  if (p <= 1) return 'text-error border-error/20 bg-error/10'
  if (p === 2)
    return 'text-accent-primary-start border-accent-primary-start/20 bg-accent-primary-glow'
  return 'text-text-muted border-surface-card-border bg-surface-card'
}
