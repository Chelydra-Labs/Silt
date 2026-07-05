import type { TaskStatus } from '../../sdk'

/**
 * Shared types and label helpers for the silt-kanban plugin.
 *
 * Single source of truth — previously KanbanCard, KanbanFilters,
 * PRIORITY_LABELS, and the status-label helpers were re-declared in
 * Kanban.svelte, CardDetailPanel.svelte, and FilterBar.svelte.
 */

export interface KanbanCard {
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
  // for one-off tasks; e.g. 'every week'. The Kanban SQL aliases t.recur to
  // this field (query.ts).
  recurrence: string
  // Pipe-delimited dependent-task UUIDs from the task_dependencies join
  // table (#301). Absent/empty when the task has no prerequisites. The
  // is_blocked derived flag below drives the lock badge + DONE guard.
  blocked_by?: string
  // Derived: 1 when the task has at least one non-DONE prerequisite, else 0.
  // Computed by a correlated subquery in the Kanban SQL (query.ts) so the
  // badge and the DONE-confirm guard read it without a second round-trip.
  is_blocked?: number
  comments_count: number
  links_count: number
  // Pipe-delimited raw tag paths from a GROUP_CONCAT subquery; absent
  // when the card has no tags.
  tags?: string
}

// Persisted in config.yaml under plugins.plugin_settings.silt-kanban.filters.
export interface KanbanFilters {
  owners: string[]
  priorities: number[]
  dueDate: '' | 'overdue' | 'today' | 'week' | 'none'
  tags: string[]
}

export type Scope = 'vault' | 'notebook' | 'section' | 'page'

/**
 * A named Kanban configuration (#323). Persisted to
 * `plugins.plugin_settings.silt-kanban.boards[]` in config.yaml. Clicking
 * a saved board applies its `scope` + `filters` to the live board; the
 * underlying settings (KanbanFilters, Scope) are the existing types so
 * a saved board never goes out of sync with the live board schema.
 */
export interface SavedBoard {
  /** UUID generated client-side via crypto.randomUUID(). */
  id: string
  /** User-given board name; shown in the sidebar list. */
  name: string
  scope: Scope
  filters: KanbanFilters
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
