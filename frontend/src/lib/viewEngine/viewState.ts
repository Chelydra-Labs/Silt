// Serializable saved-view identity contracts + persist helpers for surfaces
// that store user-defined views. Extracted from the strip loop the typed-notes
// dashboard hand-rolled; silt-tasks keeps its own standalone SavedView and
// persistence loop for now (its system-view merge is more involved), so this
// module is consumed by the dashboard today — adopt it in silt-tasks later if
// the shapes converge.
//
// Pure: no Svelte, no bindings. The dashboard's DashboardSavedView extends
// {@link SavedViewBase} and adds its own dimension fields; the strip helper is
// domain-agnostic so a persistence layer doesn't duplicate the bookkeeping.

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
