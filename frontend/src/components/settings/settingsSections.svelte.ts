// Shared derivation of the settings section list, consumed by both
// SettingsNav (the sidebar tablist) and SettingsPanel (the content tabpanel).
// Splitting SettingsShell into nav + panel (#511 rework) means both render
// sites need the same ordered list (General/Editor/…/plugin tabs/Dev/About),
// so this is the single source of truth for it.

import { settings } from '../../settings/store.svelte'
import { loadedPlugins } from '../../plugins/store.svelte'
import { getSurfaces, onSurfacesChanged } from '../../plugins/surfaces'
import type { RegisteredPlugin } from '../../plugins/sdk'
import type { PluginSurface } from '../../plugins/surfaces'

export interface SettingsSection {
  id: string
  label: string
  icon: string
  /** Present only on `plugin:*` sections — the bespoke-settings plugin. */
  plugin?: RegisteredPlugin
}

// Reactive trigger for surface-list changes. `onSurfacesChanged` bumps this
// so a `$derived` reading getSettingsSections() re-runs when a plugin
// registers/unregisters its settings surface. The hook itself runs once and
// never mutates $state synchronously, so calling it from inside the derived
// is safe (the bump happens later, when surfaces actually change).
let surfacesTick = $state(0)
let surfacesHooked = false

function ensureSurfacesHooked(): void {
  if (surfacesHooked) return
  surfacesHooked = true
  onSurfacesChanged(() => {
    surfacesTick++
  })
}

/**
 * The ordered settings section list, derived from the settings store
 * (dev-mode gate), the loaded-plugin registry (first-party bespoke pages),
 * and the surfaces registry (third-party settings-panel surfaces). Reactive:
 * reading it inside a `$derived` or component render establishes
 * dependencies on all three sources.
 */
export function getSettingsSections(): SettingsSection[] {
  ensureSurfacesHooked()
  void surfacesTick // establish reactive dependency on surface changes
  const devMode = settings.config?.ui?.open_devtools_on_startup === true

  const settingsSurfaces: PluginSurface[] = getSurfaces('settings-panel')

  const pluginSections: SettingsSection[] = []
  // First-party: compiled Svelte settings page.
  for (const plugin of loadedPlugins.plugins.values()) {
    if (plugin.settingsPageComponent) {
      pluginSections.push({
        id: `plugin:${plugin.manifest.id}`,
        label: plugin.manifest.name,
        icon: plugin.manifest.icon ?? 'tune',
        plugin
      })
    }
  }
  // Third-party: 'settings-panel' iframe surface (one per plugin).
  const seen = new Set(pluginSections.map((s) => s.id))
  for (const surface of settingsSurfaces) {
    const id = `plugin:${surface.pluginID}`
    if (seen.has(id)) continue
    const plugin = loadedPlugins.plugins.get(surface.pluginID)
    if (plugin) {
      pluginSections.push({
        id,
        label: plugin.manifest.name,
        icon: plugin.manifest.icon ?? 'tune',
        plugin
      })
      seen.add(id)
    }
  }

  return [
    { id: 'general', label: 'General', icon: 'settings' },
    { id: 'editor', label: 'Editor', icon: 'edit_note' },
    { id: 'appearance', label: 'Appearance', icon: 'palette' },
    { id: 'ai', label: 'AI Provider', icon: 'smart_toy' },
    { id: 'hotkeys', label: 'Hotkeys', icon: 'keyboard' },
    { id: 'plugins', label: 'Plugins', icon: 'extension' },
    ...pluginSections,
    ...(devMode ? [{ id: 'dev', label: 'Dev', icon: 'code' }] : []),
    { id: 'about', label: 'About', icon: 'info' }
  ]
}
