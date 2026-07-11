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

/**
 * Labeled clusters the sections are grouped into in the sidebar. The order
 * here is the render order. Group dividers are purely visual — the WAI-ARIA
 * tablist stays one flat list (a divider is never a tab), so roving
 * tabindex/Arrow/Home/End keep working across group boundaries.
 */
export type SettingsGroup =
  'workspace' | 'look-feel' | 'intelligence' | 'customize' | 'about'

export const SETTINGS_GROUP_ORDER: SettingsGroup[] = [
  'workspace',
  'look-feel',
  'intelligence',
  'customize',
  'about'
]

export const SETTINGS_GROUP_LABELS: Record<SettingsGroup, string> = {
  workspace: 'Workspace',
  'look-feel': 'Look & feel',
  intelligence: 'Intelligence',
  customize: 'Customize',
  about: 'About'
}

/**
 * The unified content width each section's body uses, driven from the panel
 * header so every tab shares one designed surface. Form-style tabs center at
 * `max-w-4xl`; grid/list tabs (themes, plugins, AI) use the full panel width.
 */
export type SettingsWidth = 'form' | 'wide'

export interface SettingsSection {
  id: string
  label: string
  icon: string
  /** One-line description shown in the shared panel header. */
  description: string
  /** Visual cluster in the sidebar nav. */
  group: SettingsGroup
  /** Content measure applied by SettingsPanel's scroll container. */
  width: SettingsWidth
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
  // First-party: compiled Svelte settings page. Visually indented under the
  // Plugins group divider to signal parent/child, but still a direct nav
  // target (a real tab) — see SettingsNav.
  for (const plugin of loadedPlugins.plugins.values()) {
    if (plugin.settingsPageComponent) {
      pluginSections.push({
        id: `plugin:${plugin.manifest.id}`,
        label: plugin.manifest.name,
        icon: plugin.manifest.icon ?? 'tune',
        description: `${plugin.manifest.name} settings`,
        // AI plugins belong under Intelligence (alongside AI Provider), not
        // Customize with the rest of the plugin bespoke-settings tabs.
        group: plugin.manifest.id.startsWith('silt-ai-')
          ? 'intelligence'
          : 'customize',
        width: 'wide',
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
        description: `${plugin.manifest.name} settings`,
        group: plugin.manifest.id.startsWith('silt-ai-')
          ? 'intelligence'
          : 'customize',
        width: 'wide',
        plugin
      })
      seen.add(id)
    }
  }

  return [
    {
      id: 'general',
      label: 'General',
      icon: 'settings',
      description: 'Workspace, window, and update preferences.',
      group: 'workspace',
      width: 'form'
    },
    {
      id: 'editor',
      label: 'Editor',
      icon: 'edit_note',
      description: 'Typography, writing aids, and editor behaviour.',
      group: 'look-feel',
      width: 'form'
    },
    {
      id: 'appearance',
      label: 'Appearance',
      icon: 'palette',
      description: 'Themes, color mode, and custom theme import.',
      group: 'look-feel',
      width: 'wide'
    },
    {
      id: 'ai',
      label: 'AI Provider',
      icon: 'smart_toy',
      description: 'Chat and embedding models, keys, and connection tests.',
      group: 'intelligence',
      width: 'wide'
    },
    {
      id: 'hotkeys',
      label: 'Hotkeys',
      icon: 'keyboard',
      description: 'Keyboard shortcuts for commands and navigation.',
      group: 'customize',
      width: 'form'
    },
    {
      id: 'plugins',
      label: 'Plugins',
      icon: 'extension',
      description: 'Install, enable, and manage capabilities.',
      group: 'customize',
      width: 'wide'
    },
    ...pluginSections,
    ...(devMode
      ? [
          {
            id: 'dev',
            label: 'Dev',
            icon: 'code',
            description: 'Diagnostic tools for development.',
            group: 'about' as const,
            width: 'form' as const
          } satisfies SettingsSection
        ]
      : []),
    {
      id: 'about',
      label: 'About',
      icon: 'info',
      description: 'Version, updates, and developer mode.',
      group: 'about',
      width: 'form'
    }
  ]
}
