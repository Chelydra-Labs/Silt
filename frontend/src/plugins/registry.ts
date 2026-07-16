import type { RegisteredPlugin } from './sdk'
import TasksHub from './first-party/silt-tasks/TasksHub.svelte'
import TasksSidebar from './first-party/silt-tasks/Sidebar.svelte'
import AttachmentsPlugin from './first-party/silt-attachments'
import AISummaryPlugin from './first-party/silt-ai-summary'
import AIQAPlugin from './first-party/silt-ai-qa'
import AIAssistantPlugin from './first-party/silt-ai-assistant'
import AIAgentPlugin from './first-party/silt-ai-agent'

// First-party plugin registry: bundled Svelte components that ship with the
// app. Third-party plugins live in .system/plugins/ and are loaded by the
// loader; both go through the identical PluginContext SDK.
const registry = new Map<string, RegisteredPlugin>()

// silt-attachments (#101): attaches files to notes via /attach. The plugin
// module exports its component + onVaultOpen hook (which registers the slash
// command). Uses the v2 SDK lifecycle hooks + slash-command registry.
registerPlugin({
  manifest: AttachmentsPlugin.manifest,
  component: AttachmentsPlugin.component,
  onVaultOpen: AttachmentsPlugin.onVaultOpen,
  source: 'first-party'
})
// silt-tasks (#370 → #424 unification, #429 retirement): the single Tasks
// surface. Milestone #38 collapsed the four overlapping task plugins
// (Agenda/Calendar/Tasks/Kanban) into one hub with List / Board / Calendar
// display modes over a grouping-first engine. Built on the PluginContext SDK;
// task creation flows through PluginCreateTask, gated by content-mutate and
// seeded by FirstPartyPluginIDs on the Go side (#407).
registerPlugin({
  manifest: {
    id: 'silt-tasks',
    name: 'Tasks',
    version: '1.0.0',
    author: 'Silt',
    description:
      'Unified Tasks hub — List, Board, and Calendar views of every task grouped by any dimension.',
    icon: 'checklist',
    capabilities: { 'content-mutate': true }
  },
  component: TasksHub,
  sidebarComponent: TasksSidebar,
  source: 'first-party'
})
// silt-ai-summary (#220–#223): dismissible per-note summary banner. The first
// AI-capable first-party plugin — gated by `ai` + `plugin-db`, off by default
// (added to plugins.disabled in config.Defaults). The plugin module exports
// its component (an informational panel) + onVaultOpen/onVaultClose hooks that
// wire the controller; the live per-note surface is the SummaryBanner
// component rendered via the note-banner host (Phase 3).
registerPlugin({
  manifest: AISummaryPlugin.manifest,
  component: AISummaryPlugin.component,
  settingsPageComponent: AISummaryPlugin.settingsPageComponent,
  onVaultOpen: AISummaryPlugin.onVaultOpen,
  onVaultClose: AISummaryPlugin.onVaultClose,
  onShutdown: AISummaryPlugin.onShutdown,
  source: 'first-party'
})
// silt-ai-qa (#224–#228): semantic search + RAG Q&A. Off by default; needs
// ai + plugin-db. Headless lifecycle provider for the unified AI drawer.
registerPlugin({
  manifest: AIQAPlugin.manifest,
  settingsPageComponent: AIQAPlugin.settingsPageComponent,
  onVaultOpen: AIQAPlugin.onVaultOpen,
  onVaultClose: AIQAPlugin.onVaultClose,
  onShutdown: AIQAPlugin.onShutdown,
  source: 'first-party'
})
// silt-ai-assistant (#229–#233): Writing Assistant — curated writing actions
// with accept/reject. Off by default; ai + content-mutate. Headless lifecycle
// provider for slash commands and the unified AI drawer.
registerPlugin({
  manifest: AIAssistantPlugin.manifest,
  settingsPageComponent: AIAssistantPlugin.settingsPageComponent,
  onVaultOpen: AIAssistantPlugin.onVaultOpen,
  onVaultClose: AIAssistantPlugin.onVaultClose,
  onShutdown: AIAssistantPlugin.onShutdown,
  source: 'first-party'
})
// silt-ai-agent (#596): AI agent that uses tools to search, read, create, and
// organize notes. Drives ctx.ai.complete with a tool catalog; off by default;
// ai + content-mutate + plugin-db + read-files. Headless lifecycle provider for
// the unified AI drawer.
registerPlugin({
  manifest: AIAgentPlugin.manifest,
  onVaultOpen: AIAgentPlugin.onVaultOpen,
  onVaultClose: AIAgentPlugin.onVaultClose,
  onShutdown: AIAgentPlugin.onShutdown,
  source: 'first-party'
})

export function registerPlugin(plugin: RegisteredPlugin): void {
  // #214: a plugin declares EITHER a bespoke settings page
  // (settingsPageComponent) OR the generic settings schema (manifest.settings),
  // NOT both. Registering both is a configuration error — fail loudly rather
  // than silently preferring one.
  if (plugin.settingsPageComponent && plugin.manifest.settings?.length) {
    throw new Error(
      `plugin ${plugin.manifest.id}: cannot declare both settingsPageComponent and manifest.settings (choose one settings UI)`
    )
  }
  registry.set(plugin.manifest.id, plugin)
}

export function getFirstParty(id: string): RegisteredPlugin | undefined {
  return registry.get(id)
}

export function firstPartyPlugins(): RegisteredPlugin[] {
  return [...registry.values()]
}
