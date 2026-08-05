// Serializable saved-view identity contracts + load/persist helpers for
// surfaces that store user-defined views. Extracted from the merge/strip loops
// the typed-notes dashboard hand-rolled; silt-tasks keeps its own persistence
// loop for now (its system-view merge is more involved), so adopt this module
// there later rather than claiming full shared ownership today.
//
// Pure: no Svelte, no bindings. Concrete view interfaces (silt-tasks
// SavedView, dashboard DashboardSavedView) extend {@link SavedViewBase} and
// add their own dimension fields. The merge/strip helpers are domain-agnostic
// so a persistence layer doesn't duplicate the bookkeeping.

/** Common identity fields every saved view carries, regardless of surface. */
export interface SavedViewBase {
  /** Stable id (UUID for user views; `sys-`-prefixed for code-defined ones). */
  id: string
  /** User-given display name (sidebar list + header). */
  name: string
  /** True for code-defined defaults (read-only, not persisted to YAML). */
  system?: boolean
}

/**
 * Merge multiple saved-view lists by id (later lists win on collision). Used
 * by loaders that compose code-defined system views with user views read from
 * config — dedup is by id so a user view can never shadow a system one (the
 * `sys-` prefix is reserved at coerce time).
 */
export function mergeViewById<T extends { id: string }>(...lists: T[][]): T[] {
  const merged = new Map<string, T>()
  for (const list of lists) {
    for (const v of list) {
      merged.set(v.id, v)
    }
  }
  return [...merged.values()]
}

/**
 * Strip system views + the `system` marker before persisting. System views
 * are re-derived from code on every load (never consume YAML budget), and a
 * stale `system: true` in YAML would lock a user view out of deletion on the
 * next load. The `system` key is removed from every emitted record.
 */
export function stripSystemFlag<V extends SavedViewBase>(
  views: V[]
): Omit<V, 'system'>[] {
  return views
    .filter((v) => !v.system)
    .map((v) => {
      const { system: _system, ...rest } = v
      void _system
      return rest
    })
}
