/**
 * The view cycle driven by the `cycle_view_layout` hotkey (default Ctrl+Alt+V).
 *
 * Pure function + const — pulled out of App.svelte so the cycle order is
 * testable in isolation. If `current` is not in the cycle, `nextView`
 * jumps to `'notes'` as the anchor.
 *
 * Phase 10 (#429) collapsed the activity bar from five entries
 * (notes/tags/calendar/tasks/kanban) to three: Calendar and Kanban are now
 * display modes of the unified silt-tasks hub. Saved nav state that still
 * holds `'calendar'`/`'kanban'` is aliased to silt-tasks for one release via
 * getPluginSidebar + the App.svelte redirect effect; those view-ids are
 * intentionally NOT in the cycle.
 */
export const VIEW_CYCLE = ['notes', 'tags', 'tasks'] as const
export type CycleView = (typeof VIEW_CYCLE)[number]

/**
 * Return the next view in the cycle, or `'notes'` if `current` is not in the
 * cycle. Wraps modulo `VIEW_CYCLE.length`.
 */
export function nextView(current: string): CycleView {
  const idx = VIEW_CYCLE.indexOf(current as CycleView)
  if (idx === -1) return 'notes'
  return VIEW_CYCLE[(idx + 1) % VIEW_CYCLE.length]
}
