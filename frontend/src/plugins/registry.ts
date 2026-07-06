import type { RegisteredPlugin } from './sdk'
import Agenda from './first-party/silt-agenda/Agenda.svelte'
import Calendar from './first-party/silt-calendar/Calendar.svelte'
import CalendarSidebar from './first-party/silt-calendar/CalendarSidebar.svelte'
import Kanban from './first-party/silt-kanban/Kanban.svelte'
import KanbanSidebar from './first-party/silt-kanban/KanbanSidebar.svelte'
import Tasks from './first-party/silt-tasks/Tasks.svelte'
import AttachmentsPlugin from './first-party/silt-attachments'

// First-party plugin registry: bundled Svelte components that ship with the
// app. Third-party plugins live in .system/plugins/ and are loaded by the
// loader; both go through the identical PluginContext SDK.
const registry = new Map<string, RegisteredPlugin>()

// Register built-in plugins. Agenda (#17) and Calendar (#18) are built
// exclusively on the PluginContext SDK, exactly as a third-party plugin would.
registerPlugin({
  manifest: {
    id: 'silt-agenda',
    name: 'Agenda',
    version: '1.0.0',
    author: 'Silt',
    description: 'Rolling agenda of overdue, today, and upcoming tasks.',
    icon: 'event_repeat'
  },
  component: Agenda,
  source: 'first-party'
})
registerPlugin({
  manifest: {
    id: 'silt-calendar',
    name: 'Calendar',
    version: '1.0.0',
    author: 'Silt',
    description:
      'Month, week, and agenda layouts of tasks by due date. Smart-list sidebar (#322).',
    icon: 'calendar_month',
    capabilities: { 'content-mutate': true }
  },
  component: Calendar,
  sidebarComponent: CalendarSidebar,
  source: 'first-party'
})
registerPlugin({
  manifest: {
    id: 'silt-kanban',
    name: 'Kanban',
    version: '1.0.0',
    author: 'Silt',
    description: 'Drag-and-drop task board (TODO / DOING / DONE).',
    icon: 'view_kanban',
    capabilities: { 'content-mutate': true },
    settings: [
      {
        key: 'default_col',
        label: 'Default Column',
        type: 'select',
        options: ['TODO', 'DOING', 'DONE'],
        default: 'TODO'
      }
    ]
  },
  component: Kanban,
  sidebarComponent: KanbanSidebar,
  source: 'first-party'
})
// silt-attachments (#101): attaches files to notes via /attach. The plugin
// module exports its component + onVaultOpen hook (which registers the slash
// command). Unlike Agenda/Calendar/Kanban, this plugin uses the v2 SDK
// lifecycle hooks + slash-command registry.
registerPlugin({
  manifest: AttachmentsPlugin.manifest,
  component: AttachmentsPlugin.component,
  onVaultOpen: AttachmentsPlugin.onVaultOpen,
  source: 'first-party'
})
// silt-tasks (#370): vault-scoped Tasks view — every active task (dated
// and undated) grouped by Overdue / Today / Upcoming / No Date / Completed.
// Sibling surface to the Calendar's date-scoped agenda; exists so undated
// tasks (the natural output of the global quick-add) are visible. Built on
// the same PluginContext SDK surface as AgendaList. Task creation flows
// through PluginCreateTask, which is gated by content-mutate — so the
// capability is declared here and seeded by FirstPartyPluginIDs on the Go
// side (#407).
registerPlugin({
  manifest: {
    id: 'silt-tasks',
    name: 'Tasks',
    version: '1.0.0',
    author: 'Silt',
    description:
      'Vault-scoped view of every active task grouped by Overdue, Today, Upcoming, No Date, and Completed.',
    icon: 'checklist',
    capabilities: { 'content-mutate': true }
  },
  component: Tasks,
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
