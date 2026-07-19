// Hand-curated search index for the settings page.
//
// SettingsSearch.svelte matches the user's query against this index and jumps
// to the matching section (plus an optional anchor control). Every `sectionId`
// must exist in getSettingsSections() — the test suite asserts that so the
// index can't drift from the real section list. Plugin bespoke-settings tabs
// are added dynamically at query time (one coarse entry per plugin) so the
// index doesn't need to know plugin ids ahead of time.

import { loadedPlugins } from '../../plugins/store.svelte'
import { getSurfaces } from '../../plugins/surfaces'

export interface SettingsIndexEntry {
  /** Visible result label. */
  label: string
  /** Extra terms matched alongside the label (synonyms, jargon). */
  keywords: string[]
  /** Must match a section id from getSettingsSections(). */
  sectionId: string
  /** Optional element id to scroll to + ring inside the section. */
  anchorId?: string
}

/**
 * The static (core) index. Grouped by section for readability; the matcher
 * flattens it. Keep entries short and user-facing — this is what people type.
 */
const CORE_INDEX: SettingsIndexEntry[] = [
  {
    label: 'Page templates',
    keywords: ['template', 'markdown', 'duplicate', 'new page', 'manage'],
    sectionId: 'templates'
  },
  // Workspace / General
  {
    label: 'Vault path',
    keywords: ['workspace', 'folder', 'notebook', 'location', 'move', 'copy'],
    sectionId: 'general'
  },
  {
    label: 'Move or copy vault',
    keywords: ['relocate', 'export', 'import', 'backup', 'archive'],
    sectionId: 'general'
  },
  {
    label: 'Close to tray',
    keywords: ['minimize', 'tray', 'background', 'quit', 'window'],
    sectionId: 'general'
  },
  {
    label: 'Switch vault',
    keywords: ['change', 'workspace', 'open', 'folder'],
    sectionId: 'general'
  },
  {
    label: 'Custom dictionary',
    keywords: [
      'spellcheck',
      'words',
      'add word',
      'spelling',
      'import',
      'export'
    ],
    sectionId: 'general',
    anchorId: 'general-dictionary'
  },

  // Editor
  {
    label: 'Font family',
    keywords: ['typography', 'typeface', 'body font', 'text'],
    sectionId: 'editor',
    anchorId: 'editor-typography'
  },
  {
    label: 'Monospace font',
    keywords: ['code', 'mono', 'typography'],
    sectionId: 'editor',
    anchorId: 'editor-typography'
  },
  {
    label: 'Font size',
    keywords: ['text size', 'zoom', 'typography'],
    sectionId: 'editor',
    anchorId: 'editor-typography'
  },
  {
    label: 'Line height',
    keywords: ['spacing', 'leading', 'typography'],
    sectionId: 'editor',
    anchorId: 'editor-typography'
  },
  {
    label: 'Tab width',
    keywords: ['indent', 'spaces', 'tab'],
    sectionId: 'editor',
    anchorId: 'editor-typography'
  },
  {
    label: 'Auto-save delay',
    keywords: ['autosave', 'save', 'writing', 'preferences'],
    sectionId: 'editor',
    anchorId: 'editor-preferences'
  },
  {
    label: 'Smart typography',
    keywords: ['em-dash', 'smart quotes', 'dashes', 'formatting'],
    sectionId: 'editor',
    anchorId: 'editor-preferences'
  },
  {
    label: 'Spellcheck',
    keywords: [
      'spelling',
      'underline',
      'misspelled',
      'language',
      'dictionary',
      'enable'
    ],
    sectionId: 'editor',
    anchorId: 'editor-spellcheck-packs'
  },
  {
    label: 'Spellcheck language',
    keywords: [
      'language pack',
      'en-GB',
      'german',
      'french',
      'spanish',
      'dictionary download'
    ],
    sectionId: 'editor',
    anchorId: 'editor-spellcheck-packs'
  },
  {
    label: 'Domain word lists',
    keywords: [
      'software terms',
      'typescript',
      'python',
      'technical',
      'false positive'
    ],
    sectionId: 'editor',
    anchorId: 'editor-spellcheck-packs'
  },
  {
    label: 'Typewriter mode',
    keywords: ['centered', 'focus', 'writing'],
    sectionId: 'editor',
    anchorId: 'editor-preferences'
  },
  {
    label: 'Focus mode',
    keywords: ['dim', 'inactive', 'paragraph', 'concentrate'],
    sectionId: 'editor',
    anchorId: 'editor-preferences'
  },
  {
    label: 'Format toolbar',
    keywords: ['toolbar', 'formatting', 'bold', 'italic'],
    sectionId: 'editor',
    anchorId: 'editor-preferences'
  },
  {
    label: 'Word count',
    keywords: ['count', 'words', 'statistics'],
    sectionId: 'editor',
    anchorId: 'editor-preferences'
  },

  // Appearance
  {
    label: 'Theme',
    keywords: ['appearance', 'color', 'palette', 'skin', 'dark', 'light'],
    sectionId: 'appearance'
  },
  {
    label: 'Color mode',
    keywords: ['dark', 'light', 'system', 'appearance'],
    sectionId: 'appearance'
  },
  {
    label: 'Import theme',
    keywords: ['custom', 'json', 'appearance', 'install theme'],
    sectionId: 'appearance'
  },
  {
    label: 'Export theme',
    keywords: ['custom', 'json', 'appearance', 'save theme'],
    sectionId: 'appearance'
  },

  // AI Provider
  {
    label: 'Chat model',
    keywords: [
      'ai',
      'llm',
      'provider',
      'openai',
      'ollama',
      'google',
      'anthropic',
      'gemini',
      'claude'
    ],
    sectionId: 'ai'
  },
  {
    label: 'Embedding model',
    keywords: ['ai', 'vector', 'embedding', 'semantic', 'search'],
    sectionId: 'ai'
  },
  {
    label: 'API key',
    keywords: ['secret', 'credential', 'ai', 'password', 'keyring'],
    sectionId: 'ai'
  },
  {
    label: 'Base URL',
    keywords: ['endpoint', 'server', 'ai', 'provider', 'ollama', 'localhost'],
    sectionId: 'ai'
  },
  {
    label: 'Test connection',
    keywords: ['ai', 'probe', 'ping', 'check', 'connect'],
    sectionId: 'ai'
  },
  {
    label: 'Key storage',
    keywords: ['keyring', 'keychain', 'security', 'secret', 'config.yaml'],
    sectionId: 'ai'
  },
  {
    label: 'Local MCP',
    keywords: [
      'mcp',
      'agent',
      'claude',
      'opencode',
      'codex',
      'loopback',
      'stdio',
      'local ai integration'
    ],
    sectionId: 'ai',
    anchorId: 'ai-local-mcp'
  },
  {
    label: 'Answer Style',
    keywords: [
      'ai',
      'tuning',
      'temperature',
      'creativity',
      'advanced',
      'precise',
      'creative'
    ],
    sectionId: 'ai'
  },
  {
    label: 'Thinking Depth',
    keywords: ['ai', 'tuning', 'reasoning', 'effort', 'advanced'],
    sectionId: 'ai'
  },
  {
    label: 'Index Density',
    keywords: [
      'ai',
      'embedding',
      'dimensions',
      'truncation',
      'matryoshka',
      'advanced'
    ],
    sectionId: 'ai'
  },
  {
    label: 'AI activity log',
    keywords: ['audit', 'calls', 'history', 'tokens', 'ai'],
    sectionId: 'ai'
  },
  {
    label: 'Enable AI features',
    keywords: [
      'master',
      'toggle',
      'semantic search',
      'rag',
      'summaries',
      'writing'
    ],
    sectionId: 'ai',
    anchorId: 'ai-setup'
  },
  {
    label: 'Writing Assistant actions',
    keywords: ['draft', 'rewrite', 'clarity', 'tasks', 'tags', 'proposals'],
    sectionId: 'ai',
    anchorId: 'ai-writing-tuning'
  },
  {
    label: 'Semantic search index',
    keywords: [
      'hybrid',
      'balance',
      'context breadth',
      'rebuild',
      'embedding',
      'qa'
    ],
    sectionId: 'ai',
    anchorId: 'ai-search-tuning'
  },
  {
    label: 'Note summaries',
    keywords: ['banner', 'auto_on_open', 'facets', 'summary length'],
    sectionId: 'ai',
    anchorId: 'ai-summary-tuning'
  },

  // Hotkeys
  {
    label: 'Keyboard shortcuts',
    keywords: ['hotkeys', 'keybindings', 'shortcuts', 'accelerator'],
    sectionId: 'hotkeys',
    anchorId: 'hotkeys-shortcuts'
  },

  // Plugins
  {
    label: 'Install plugin',
    keywords: ['extensions', 'add', 'silt-plugin', 'download'],
    sectionId: 'plugins'
  },
  {
    label: 'Enable plugin',
    keywords: ['disable', 'toggle', 'extensions'],
    sectionId: 'plugins'
  },
  {
    label: 'Plugin capabilities',
    keywords: ['permissions', 'network', 'read files', 'ai', 'extensions'],
    sectionId: 'plugins'
  },
  {
    label: 'Check plugin updates',
    keywords: ['update', 'extensions', 'version'],
    sectionId: 'plugins'
  },

  // About
  {
    label: 'App version',
    keywords: ['about', 'version', 'build'],
    sectionId: 'about'
  },
  {
    label: 'Check for updates',
    keywords: ['about', 'release', 'download', 'version'],
    sectionId: 'about'
  },
  {
    label: 'Dev mode',
    keywords: ['developer', 'devtools', 'diagnostics', 'about'],
    sectionId: 'about'
  },
  {
    label: 'Source & feedback links',
    keywords: ['github', 'issues', 'about', 'links'],
    sectionId: 'about'
  }
]

/**
 * Coarse entries for plugin bespoke-settings tabs (one per plugin that
 * registers a settings surface). Built at query time so the index tracks
 * installs/uninstalls without a manual edit.
 */
function pluginEntries(): SettingsIndexEntry[] {
  const out: SettingsIndexEntry[] = []
  const seen = new Set<string>()
  for (const plugin of loadedPlugins.plugins.values()) {
    if (!plugin.settingsPageComponent) continue
    // First-party AI fine-tuning is indexed under sectionId 'ai' above.
    if (plugin.manifest.id.startsWith('silt-ai-')) continue
    const id = `plugin:${plugin.manifest.id}`
    if (seen.has(id)) continue
    seen.add(id)
    out.push({
      label: `${plugin.manifest.name} settings`,
      keywords: ['plugin', 'extension', plugin.manifest.id],
      sectionId: id
    })
  }
  for (const surface of getSurfaces('settings-panel')) {
    const id = `plugin:${surface.pluginID}`
    if (seen.has(id)) continue
    if (surface.pluginID.startsWith('silt-ai-')) continue
    const plugin = loadedPlugins.plugins.get(surface.pluginID)
    if (!plugin) continue
    seen.add(id)
    out.push({
      label: `${plugin.manifest.name} settings`,
      keywords: ['plugin', 'extension', plugin.manifest.id],
      sectionId: id
    })
  }
  return out
}

/** The full index (core + dynamic plugin entries). */
export function getSettingsIndex(): SettingsIndexEntry[] {
  return [...CORE_INDEX, ...pluginEntries()]
}

/**
 * Match a query against the index. Case-insensitive substring on label or any
 * keyword. Returns at most `limit` entries, label-first. An empty query
 * returns [] (the popover is closed for empty input).
 */
export function searchSettings(
  query: string,
  limit = 12
): SettingsIndexEntry[] {
  const q = query.trim().toLowerCase()
  if (!q) return []
  const matches = getSettingsIndex().filter((e) => {
    if (e.label.toLowerCase().includes(q)) return true
    return e.keywords.some((k) => k.toLowerCase().includes(q))
  })
  return matches.slice(0, limit)
}
