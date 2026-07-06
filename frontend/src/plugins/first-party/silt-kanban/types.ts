/**
 * Kanban-scoped types — the board filter / saved-board config that has no
 * meaning outside Kanban. The cross-surface task shape (`TaskDetail`) and
 * the label helpers (`PRIORITY_LABELS` / `laneLabel` / `priorityClass`) live
 * in `../shared/types`; import them directly from there.
 */

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
