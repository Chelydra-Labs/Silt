/**
 * Kanban-scoped types. The task shape + label helpers now live in
 * `../shared/types` (`TaskDetail`) so every task surface consumes one
 * contract; this file keeps the board-scoped filter/board-config types that
 * have no meaning outside Kanban.
 */

// Re-export the shared contract + helpers so existing silt-kanban imports
// (`import { PRIORITY_LABELS, laneLabel } from './types'`) keep resolving
// during the staged extraction. Phase 5 migrates call sites to import
// directly from `../shared/types`. `TaskDetail` is a type; the helpers are
// runtime values, so the two re-exports use separate clauses.
export type { TaskDetail } from '../shared/types'
export { PRIORITY_LABELS, laneLabel, priorityClass } from '../shared/types'

import type { TaskDetail } from '../shared/types'

/**
 * Backwards-compat alias: silt-kanban internals historically reference
 * `KanbanCard`. `TaskDetail` IS the type; this alias keeps Phase 1 a small,
 * compile-clean step. Full call-site migration to `TaskDetail` (and removal
 * of this alias) completes in Phase 5.
 */
export type KanbanCard = TaskDetail

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
