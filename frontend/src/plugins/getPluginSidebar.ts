// Sidebar resolution helper (#321). `Sidebar.svelte` uses this to look up
// the active view's plugin and render its `sidebarComponent` in place of
// the page tree. Mirrors the resolution `PluginView.svelte` does for the
// main view's `component`.
//
// Only first-party plugins ship compiled Svelte components, so this only
// resolves views whose plugin id has a `silt-` prefix and a non-null
// sidebarComponent on the registered entry. Everything else (tags, notes,
// or a plugin view without a registered sidebar) returns null, and the
// caller falls back to the page tree.

import type { RegisteredPlugin } from './sdk'
import { loadedPlugins } from './store.svelte'

/**
 * Map a top-level view id to the plugin id whose sidebar should own the
 * sidebar slot when that view is active. Three views ship compiled
 * sidebar components today: `calendar`, `kanban`, and `tasks`. Tags and
 * Notes return null. The calendar/kanban→silt-tasks alias window lands
 * in Phase 10 (when the standalone plugins are deleted); until then
 * each id maps to its own plugin.
 */
export function pluginIdForView(activeView: string): string | null {
  if (activeView === 'calendar') return 'silt-calendar'
  if (activeView === 'kanban') return 'silt-kanban'
  if (activeView === 'tasks') return 'silt-tasks'
  return null
}

/**
 * Resolve the active view's plugin entry. Returns null when:
 *   - the view is not a plugin view (notes / tags), or
 *   - the plugin is not loaded (disabled or not registered), or
 *   - the plugin did not register a `sidebarComponent`.
 *
 * The resolved object is the same `RegisteredPlugin` the main view sees,
 * so `manifest` is available for the sidebar component's `manifest` prop.
 */
export function getPluginSidebar(activeView: string): RegisteredPlugin | null {
  const id = pluginIdForView(activeView)
  if (!id) return null
  const reg = loadedPlugins.plugins.get(id)
  if (!reg || !reg.sidebarComponent) return null
  return reg
}
