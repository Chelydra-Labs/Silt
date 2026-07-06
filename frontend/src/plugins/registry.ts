import type { RegisteredPlugin } from './sdk'
import Calendar from './first-party/silt-calendar/Calendar.svelte'
import CalendarSidebar from './first-party/silt-calendar/CalendarSidebar.svelte'
import Kanban from './first-party/silt-kanban/Kanban.svelte'
import KanbanSidebar from './first-party/silt-kanban/KanbanSidebar.svelte'
import TasksHub from './first-party/silt-tasks/TasksHub.svelte'
import TasksSidebar from './first-party/silt-tasks/Sidebar.svelte'
import AttachmentsPlugin from './first-party/silt-attachments'

// First-party plugin registry: bundled Svelte components that ship with the
// app. Third-party plugins live in .system/plugins/ and are loaded by the
// loader; both go through the identical PluginContext SDK.
const registry = new Map<string, RegisteredPlugin>()

// Register built-in plugins. Calendar (#18) is built exclusively on the
// PluginContext SDK, exactly as a third-party plugin would.
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
// silt-tasks (#370 → #424 unification): the single Tasks surface. Milestone
// #38 collapses the four overlapping task plugins (Agenda/Calendar/Tasks/
// Kanban) into one hub with List / Board / Calendar display modes over a
// grouping-first engine. During the transition the standalone silt-calendar
// and silt-kanban registrations stay live; the retire issue (#429) removes
// them once user settings are migrated (#431). Built on the PluginContext SDK;
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
