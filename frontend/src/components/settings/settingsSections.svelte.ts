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
 * The unified content width each section's body uses is applied directly by
 * each tab's root container (form-style tabs center at `max-w-4xl`; grid/list
 * tabs like themes/plugins/AI use `max-w-6xl`). There is no `width` field on
 * SettingsSection — the per-tab class is the single source of truth.
 */
export interface SettingsSection {
  id: string
  label: string
  icon: string
  /** One-line description shown in the shared panel header. */
  description: string
  /** Visual cluster in the sidebar nav. */
  group: SettingsGroup
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
        plugin
      })
      seen.add(id)
    }
  }

  // Built-ins first (stable order), then plugin tabs. AI plugin settings
  // share the intelligence group with AI Provider — if they were appended
  // after Customize plugins, SettingsNav would emit a second "Intelligence"
  // divider and Svelte's keyed each would throw each_key_duplicate.
  const core: SettingsSection[] = [
    {
      id: 'general',
      label: 'General',
      icon: 'settings',
      description: 'Workspace, window, dictionary, and update preferences.',
      group: 'workspace'
    },
    {
      id: 'editor',
      label: 'Editor',
      icon: 'edit_note',
      description: 'Typography, writing aids, and editor behaviour.',
      group: 'look-feel'
    },
    {
      id: 'appearance',
      label: 'Appearance',
      icon: 'palette',
      description: 'Themes, color mode, and custom theme import.',
      group: 'look-feel'
    },
    {
      id: 'ai',
      label: 'AI Provider',
      icon: 'smart_toy',
      description: 'Chat and embedding models, keys, and connection tests.',
      group: 'intelligence'
    },
    {
      id: 'hotkeys',
      label: 'Hotkeys',
      icon: 'keyboard',
      description: 'Keyboard shortcuts for commands and navigation.',
      group: 'customize'
    },
    {
      id: 'plugins',
      label: 'Plugins',
      icon: 'extension',
      description: 'Install, enable, and manage capabilities.',
      group: 'customize'
    },
    ...pluginSections,
    ...(devMode
      ? [
          {
            id: 'dev',
            label: 'Dev',
            icon: 'code',
            description: 'Diagnostic tools for development.',
            group: 'about' as const
          } satisfies SettingsSection
        ]
      : []),
    {
      id: 'about',
      label: 'About',
      icon: 'info',
      description: 'Version, updates, and developer mode.',
      group: 'about'
    }
  ]

  // Stable group order so each divider appears once; preserve relative order
  // within a group (core tabs before plugin tabs that share the group).
  const groupRank = new Map(SETTINGS_GROUP_ORDER.map((g, i) => [g, i] as const))
  return core
    .map((s, i) => ({ s, i }))
    .sort((a, b) => {
      const ga = groupRank.get(a.s.group) ?? 99
      const gb = groupRank.get(b.s.group) ?? 99
      if (ga !== gb) return ga - gb
      return a.i - b.i
    })
    .map(({ s }) => s)
}

/** Section shown when an unknown section id is requested — a typo'd
 * `ctx.openSettings('foo')` from a plugin, or a stale persisted id. 'general'
 * is the always-present landing tab. */
export const FALLBACK_SETTINGS_SECTION = 'general'

/**
 * Resolve a requested settings-section id to a known one, falling back to the
 * default when it is absent or unknown. Pure (takes the known-id list) so it
 * can be unit-tested without mounting the reactive registry. Centralizes the
 * validation that both the open-settings path (PluginContext.openSettings →
 * App.openSettings) and the in-view section jump share, so a bad id never
 * renders a blank panel or a dangling `aria-labelledby`. */
export function resolveSettingsSectionId(
  requested: string | undefined | null,
  knownIds: string[]
): string {
  return requested && knownIds.includes(requested)
    ? requested
    : FALLBACK_SETTINGS_SECTION
}
