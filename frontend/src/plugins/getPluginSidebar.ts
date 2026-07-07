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
 * sidebar slot when that view is active. After Phase 10 (#429) only
 * `tasks` is a live activity-bar entry; Tags and Notes return null.
 *
 * One-release alias window: `calendar` and `kanban` no longer have their
 * own plugins, but saved nav state (or an external `switch-view` event)
 * may still carry those ids. For one release we route both to the
 * silt-tasks sidebar so a stale id renders the unified sidebar instead of
 * the page-tree fallback. An `$effect` in App.svelte also redirects the
 * activity bar to `'tasks'` and hints the matching display mode. Drop
 * these two branches in N+1 once the old view-ids are fully removed.
 */
export function pluginIdForView(activeView: string): string | null {
  if (activeView === 'calendar') return 'silt-tasks' // alias (one release)
  if (activeView === 'kanban') return 'silt-tasks' // alias (one release)
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
