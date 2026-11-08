// Dev-only browser visual-audit harness transport and query parameter router.
// Activated via `?harness=1` or `VITE_AUDIT_HARNESS=1` during `vite` dev serve
// so all views, states (`default`, `loading`, `empty`, `error`), settings tabs,
// and global dialogs can be rendered and audited in a standard browser without
// the Go backend.

import { setTransport } from '@wailsio/runtime'
import {
  AUDIT_ACTIVE_THEME_RESULT,
  AUDIT_AI_AUDIT_ENTRIES,
  AUDIT_AI_MODELS,
  AUDIT_AI_PROVIDER_CONFIG,
  AUDIT_APP_VERSION,
  AUDIT_BACKLINKS_PAGE,
  AUDIT_CUSTOM_DICTIONARY,
  AUDIT_LOCAL_AUTHOR,
  AUDIT_MCP_AUDIT_ENTRIES,
  AUDIT_MCP_CONFIG,
  AUDIT_MCP_INSTALL_HINT,
  AUDIT_MCP_STATUS,
  AUDIT_NAVIGATION,
  AUDIT_NAV_ORDER,
  AUDIT_OPEN_TABS,
  AUDIT_PAGE_BLOCKS,
  AUDIT_PAGE_CORE_METADATA,
  AUDIT_PAGE_MARKDOWN,
  AUDIT_PAGE_PROPERTIES,
  AUDIT_PAGE_TYPE_INFO,
  AUDIT_PLUGIN_SECURITY_STATS,
  AUDIT_SEARCH_RESULTS,
  AUDIT_SIDEBAR_WIDTH,
  AUDIT_SPELLCHECK_DOMAIN_PACKS,
  AUDIT_SPELLCHECK_LANGUAGE_PACKS,
  AUDIT_SYSTEM_CONFIG,
  AUDIT_TAGGED_BLOCKS,
  AUDIT_TAG_HIERARCHY,
  AUDIT_TASKS_ROWS,
  AUDIT_TASK_UNDO_RESULT,
  AUDIT_TASK_UNDO_STATE,
  AUDIT_TEMPLATES_LIST,
  AUDIT_THEMES_RESULT,
  AUDIT_THEME_JSON,
  AUDIT_TYPES,
  AUDIT_TYPES_REPROJECTION_STATUS,
  AUDIT_TYPE_PAGES,
  AUDIT_UNLINKED_MENTIONS_PAGE,
  AUDIT_UPDATE_SETTINGS
} from './auditFixtures'

export type AuditHarnessView =
  | 'onboarding'
  | 'notes'
  | 'backlinks'
  | 'tags'
  | 'dashboard'
  | 'tasks'
  | 'calendar'
  | 'kanban'
  | 'settings'

export type AuditHarnessState = 'default' | 'loading' | 'empty' | 'error'

export type AuditHarnessTab =
  | 'general'
  | 'editor'
  | 'appearance'
  | 'ai'
  | 'hotkeys'
  | 'templates'
  | 'plugins'
  | 'dev'
  | 'about'

export type AuditHarnessDialog =
  | 'search'
  | 'quick-switcher'
  | 'shortcuts'
  | 'template-picker'
  | 'global-replace'
  | 'confirm'
  | 'choice'
  | 'name-prompt'

export interface AuditHarnessParams {
  enabled: boolean
  view: AuditHarnessView | null
  state: AuditHarnessState
  tab: AuditHarnessTab | null
  dialog: AuditHarnessDialog | null
  tag: string | null
}

const VALID_VIEWS = new Set<AuditHarnessView>([
  'onboarding',
  'notes',
  'backlinks',
  'tags',
  'dashboard',
  'tasks',
  'calendar',
  'kanban',
  'settings'
])

const VALID_STATES = new Set<AuditHarnessState>([
  'default',
  'loading',
  'empty',
  'error'
])

const VALID_TABS = new Set<AuditHarnessTab>([
  'general',
  'editor',
  'appearance',
  'ai',
  'hotkeys',
  'templates',
  'plugins',
  'dev',
  'about'
])

const VALID_DIALOGS = new Set<AuditHarnessDialog>([
  'search',
  'quick-switcher',
  'shortcuts',
  'template-picker',
  'global-replace',
  'confirm',
  'choice',
  'name-prompt'
])

// Content-bearing IPC methods that respond to `state=empty|loading|error` while
// shell/theme/config bootstrap bindings remain resolved so the target view can mount.
const CONTENT_QUERY_BINDINGS = new Set<string>([
  'ListTypes',
  'QueryPagesByType',
  'GetTags',
  'QueryTagHierarchy',
  'GetBlocksByTag',
  'QueryBlocksByTag',
  'PluginQueryTasks',
  'PluginRawQuery',
  'QueryTasks',
  'GetBacklinksPaged',
  'GetUnlinkedMentionsPaged',
  'SearchBlocksPaged',
  'SearchPages'
])

let testSearchOverride: string | null = null
let testEnvHarnessOverride: boolean | null = null
let transportInstalled = false

/** Override query string / env flag in unit tests without mutating window.location. */
export function setAuditHarnessTestOverrides(
  search: string | null,
  envEnabled: boolean | null = null
): void {
  testSearchOverride = search
  testEnvHarnessOverride = envEnabled
}

function currentSearch(explicitSearch?: string): string {
  if (explicitSearch !== undefined) return explicitSearch
  if (testSearchOverride !== null) return testSearchOverride
  if (typeof window !== 'undefined' && window.location) {
    return window.location.search
  }
  return ''
}

function isEnvHarnessFlagSet(): boolean {
  if (testEnvHarnessOverride !== null) return testEnvHarnessOverride
  try {
    return import.meta.env?.VITE_AUDIT_HARNESS === '1'
  } catch {
    return false
  }
}

export function parseAuditHarnessParams(
  explicitSearch?: string
): AuditHarnessParams {
  const raw = currentSearch(explicitSearch)
  const sp = new URLSearchParams(raw.startsWith('?') ? raw.slice(1) : raw)
  const harnessParam = sp.get('harness')
  const enabled =
    harnessParam === '1' || harnessParam === 'true' || isEnvHarnessFlagSet()

  const rawView = sp.get('view') as AuditHarnessView | null
  const view = rawView && VALID_VIEWS.has(rawView) ? rawView : null

  const rawState = sp.get('state') as AuditHarnessState | null
  const state: AuditHarnessState =
    rawState && VALID_STATES.has(rawState) ? rawState : 'default'

  const rawTab = sp.get('tab') as AuditHarnessTab | null
  const tab = rawTab && VALID_TABS.has(rawTab) ? rawTab : null

  const rawDialog = sp.get('dialog') as AuditHarnessDialog | null
  const dialog = rawDialog && VALID_DIALOGS.has(rawDialog) ? rawDialog : null

  const tag = sp.get('tag')?.trim() || null

  return { enabled, view, state, tab, dialog, tag }
}

export function isAuditHarnessActive(explicitSearch?: string): boolean {
  return parseAuditHarnessParams(explicitSearch).enabled
}

/**
 * Installs a no-op `@wailsio/runtime` transport so direct runtime calls
 * (`Window.IsMaximised`, `Window.Minimise`, `Events.Emit`, `System.IsMac`, etc.)
 * resolve cleanly in a standalone browser without hitting `/wails/runtime`.
 */
export function installAuditHarnessTransport(): boolean {
  if (!isAuditHarnessActive()) return false
  setTransport({
    call: (objectID: number, method: number): Promise<unknown> => {
      // Namespaces in @wailsio/runtime:
      // 0 = Call, 6 = Window, 8 = System
      if (objectID === 6) {
        // Window.IsFocused (12), Window.Resizable (22) -> true
        if (method === 12 || method === 22) return Promise.resolve(true)
        // Window.IsFullscreen (13), Window.IsMaximised (14), Window.IsMinimised (15) -> false
        if (method === 13 || method === 14 || method === 15) {
          return Promise.resolve(false)
        }
        // Window.Size (37)
        if (method === 37) return Promise.resolve({ width: 1440, height: 900 })
        // Window.Width (44) / Window.Height (10)
        if (method === 44) return Promise.resolve(1440)
        if (method === 10) return Promise.resolve(900)
        return Promise.resolve(null)
      }
      if (objectID === 8) {
        // System.IsDarkMode (0) -> true, IsLinux/IsMac/IsWindows -> false/true
        if (method === 0) return Promise.resolve(true)
        return Promise.resolve(false)
      }
      return Promise.resolve(null)
    }
  })
  transportInstalled = true
  return true
}

export function isAuditHarnessTransportInstalled(): boolean {
  return transportInstalled
}

/**
 * Dev-only source transform for `bindings/silt/app.js`. Wraps every exported
 * Wails binding function so browser audit sessions (`?harness=1`) resolve from
 * deterministic fixtures while normal `wails3 dev` calls fall through to `$Call.ByID`.
 */
export function transformAppBindingsForHarness(
  code: string,
  harnessImportPath = '../../src/lib/testing/auditHarness.ts'
): string {
  if (code.includes('invokeHarnessBinding')) return code
  const header = `import { isAuditHarnessActive, invokeHarnessBinding } from ${JSON.stringify(harnessImportPath)};\n`
  const transformed = code.replace(
    /export function (\w+)\s*\(([^)]*)\)\s*\{/g,
    (_match, fnName: string, params: string) => {
      const argList = params.trim() ? `[${params}]` : '[]'
      return `export function ${fnName}(${params}) {\n    if (isAuditHarnessActive()) return invokeHarnessBinding(${JSON.stringify(fnName)}, ${argList});`
    }
  )
  return header + transformed
}

function createHarnessError(): Error {
  return new Error(
    JSON.stringify({
      code: 'HARNESS_ERROR',
      message: 'Simulated IPC failure in audit harness'
    })
  )
}

function neverResolvingPromise<T = unknown>(): Promise<T> {
  return new Promise<T>(() => {})
}

function cloneFixture<T>(value: T): T {
  if (value === undefined || value === null || typeof value !== 'object') {
    return value
  }
  return JSON.parse(JSON.stringify(value)) as T
}

function shouldTargetContentQuery(
  name: string,
  view: AuditHarnessView | null,
  tab: AuditHarnessTab | null,
  dialog: AuditHarnessDialog | null
): boolean {
  if (CONTENT_QUERY_BINDINGS.has(name)) {
    // In `PluginRawQuery`, facet queries (`SELECT DISTINCT ...`) can resolve
    // empty during loading/error while the main task query (`FROM blocks b JOIN tasks t`)
    // reflects the requested state.
    return true
  }
  if (
    name === 'ListTemplates' &&
    (dialog === 'template-picker' || tab === 'templates')
  ) {
    return true
  }
  if (
    (name === 'FetchPageBlocks' ||
      name === 'FetchPageBlocksProgrammatic' ||
      name === 'FetchPageMarkdown' ||
      name === 'FetchPageMarkdownProgrammatic') &&
    view === 'notes'
  ) {
    return true
  }
  return false
}

function resolveTaskPluginRawQuery(
  args: unknown[],
  state: AuditHarnessState
): { rows: unknown[]; truncated: boolean } {
  const sql =
    typeof args[2] === 'string'
      ? args[2]
      : typeof args[0] === 'string'
        ? args[0]
        : ''
  if (state === 'empty') return { rows: [], truncated: false }
  if (sql.includes('SELECT DISTINCT owner')) {
    return { rows: [{ owner: 'alex' }, { owner: 'sam' }], truncated: false }
  }
  if (sql.includes('SELECT DISTINCT level_0')) {
    return {
      rows: [
        { tag: 'ui' },
        { tag: 'design' },
        { tag: 'a11y' },
        { tag: 'release' }
      ],
      truncated: false
    }
  }
  const allRows = cloneFixture(AUDIT_TASKS_ROWS)
  if (sql.includes("t.status = 'DONE'")) {
    return {
      rows: allRows.filter((row) => row.status === 'DONE'),
      truncated: false
    }
  }
  if (sql.includes("t.status != 'DONE'")) {
    return {
      rows: allRows.filter((row) => row.status !== 'DONE'),
      truncated: false
    }
  }
  return { rows: allRows, truncated: false }
}

export function invokeHarnessBinding(
  name: string,
  args: unknown[] = [],
  explicitSearch?: string
): Promise<unknown> {
  const params = parseAuditHarnessParams(explicitSearch)
  const { view, state, tab, dialog } = params

  // Onboarding state handling
  if (name === 'IsVaultInitialized') {
    if (view === 'onboarding') {
      if (state === 'loading') return neverResolvingPromise()
      return Promise.resolve(false)
    }
    return Promise.resolve(true)
  }

  if (
    (name === 'PickNotebookFolder' || name === 'InitializeVault') &&
    (view === 'onboarding' || state === 'error')
  ) {
    if (state === 'error') {
      return Promise.reject(createHarnessError())
    }
    if (state === 'loading') {
      return neverResolvingPromise()
    }
    return Promise.resolve(
      name === 'PickNotebookFolder' ? '/Users/alex/Documents/SiltVault' : true
    )
  }

  // Simulated loading or error state for content queries
  if (shouldTargetContentQuery(name, view, tab, dialog)) {
    if (state === 'loading') {
      return neverResolvingPromise()
    }
    if (state === 'error') {
      return Promise.reject(createHarnessError())
    }
  }

  // Simulated empty state for content queries
  if (state === 'empty') {
    switch (name) {
      case 'ListTypes':
        return Promise.resolve({ types: [], errors: [], warnings: [] })
      case 'ListTemplates':
        return Promise.resolve({ templates: [], errors: [], warnings: [] })
      case 'PluginRawQuery':
        return Promise.resolve({ rows: [], truncated: false })
      case 'QueryPagesByType':
      case 'GetTags':
      case 'QueryTagHierarchy':
      case 'GetBlocksByTag':
      case 'QueryBlocksByTag':
      case 'PluginQueryTasks':
      case 'QueryTasks':
      case 'SearchPages':
        return Promise.resolve([])
      case 'GetBacklinksPaged':
      case 'GetUnlinkedMentionsPaged':
        return Promise.resolve({ results: [], cursor: '', hasMore: false })
      case 'SearchBlocksPaged':
        return Promise.resolve({
          results: [],
          total: 0,
          offset: 0,
          limit: 20,
          has_more: false
        })
      case 'ListNavigation':
        if (view === 'notes') return Promise.resolve({ notebooks: [] })
        break
      case 'GetOpenTabs':
        if (view === 'notes') {
          return Promise.resolve({ open_tabs: [], active_tab: null })
        }
        break
      case 'FetchPageBlocks':
      case 'FetchPageBlocksProgrammatic':
        if (view === 'notes') return Promise.resolve([])
        break
    }
  }

  // Default / bootstrap bindings
  switch (name) {
    case 'GetSystemConfig':
      return Promise.resolve(cloneFixture(AUDIT_SYSTEM_CONFIG))
    case 'GetSidebarWidth':
      return Promise.resolve(AUDIT_SIDEBAR_WIDTH)
    case 'ListNavigation':
      return Promise.resolve(cloneFixture(AUDIT_NAVIGATION))
    case 'GetNavigationPreferences':
      return Promise.resolve({
        sidebar_view: 'tree',
        expanded_sections: [
          { notebook: 'Product', path: 'Architecture' },
          { notebook: 'Product', path: 'Research' },
          { notebook: 'Operations', path: 'Sprints' }
        ],
        favorites: [
          {
            notebook: 'Product',
            section: 'Architecture',
            page: 'Design System Roadmap'
          }
        ],
        recent_pages: [
          {
            notebook: 'Product',
            section: 'Architecture',
            page: 'Design System Roadmap',
            opened_at: 1776520000
          },
          {
            notebook: 'Product',
            section: 'Research',
            page: 'User Interview Synthesis',
            opened_at: 1776510000
          }
        ]
      })
    case 'GetOpenTabs':
      return Promise.resolve(cloneFixture(AUDIT_OPEN_TABS))
    case 'GetNavOrder':
      return Promise.resolve(cloneFixture(AUDIT_NAV_ORDER))
    case 'FetchPageBlocks':
    case 'FetchPageBlocksProgrammatic':
      return Promise.resolve(cloneFixture(AUDIT_PAGE_BLOCKS))
    case 'FetchPageMarkdown':
    case 'FetchPageMarkdownProgrammatic':
      return Promise.resolve(AUDIT_PAGE_MARKDOWN)
    case 'GetPageType':
      return Promise.resolve(cloneFixture(AUDIT_PAGE_TYPE_INFO))
    case 'GetPageProperties':
      return Promise.resolve(cloneFixture(AUDIT_PAGE_PROPERTIES))
    case 'GetPageCoreMetadata':
      return Promise.resolve(cloneFixture(AUDIT_PAGE_CORE_METADATA))
    case 'ListTypes':
      return Promise.resolve({
        types: cloneFixture(AUDIT_TYPES),
        errors: [],
        warnings: []
      })
    case 'GetType': {
      const requested = typeof args[0] === 'string' ? args[0] : 'project'
      const found =
        AUDIT_TYPES.find((t) => t.id === requested || t.name === requested) ??
        AUDIT_TYPES[0]
      return Promise.resolve(cloneFixture(found))
    }
    case 'QueryPagesByType':
      return Promise.resolve(cloneFixture(AUDIT_TYPE_PAGES))
    case 'GetTypesReprojectionStatus':
      return Promise.resolve(cloneFixture(AUDIT_TYPES_REPROJECTION_STATUS))
    case 'GetTags':
    case 'QueryTagHierarchy':
      return Promise.resolve(cloneFixture(AUDIT_TAG_HIERARCHY))
    case 'GetBlocksByTag':
    case 'QueryBlocksByTag': {
      const requestedTag = typeof args[0] === 'string' ? args[0] : ''
      if (!requestedTag) {
        return Promise.resolve(cloneFixture(AUDIT_TAGGED_BLOCKS))
      }
      const filtered = AUDIT_TAGGED_BLOCKS.filter(
        (b) =>
          b.full_tag === requestedTag ||
          b.full_tag.startsWith(`${requestedTag}/`)
      )
      return Promise.resolve(
        cloneFixture(filtered.length > 0 ? filtered : AUDIT_TAGGED_BLOCKS)
      )
    }
    case 'GetBacklinksPaged':
      return Promise.resolve(cloneFixture(AUDIT_BACKLINKS_PAGE))
    case 'GetUnlinkedMentionsPaged':
      return Promise.resolve(cloneFixture(AUDIT_UNLINKED_MENTIONS_PAGE))
    case 'SearchBlocksPaged':
      return Promise.resolve(cloneFixture(AUDIT_SEARCH_RESULTS))
    case 'SearchPages':
      return Promise.resolve(
        cloneFixture(
          AUDIT_SEARCH_RESULTS.results.map((r) => ({
            source: r.source,
            notebook: r.notebook,
            section: r.section,
            page: r.page,
            title: r.page,
            snippet: r.snippet ?? r.clean_content
          }))
        )
      )
    case 'DistinctOwners':
      return Promise.resolve(['alex', 'sam', 'morgan'])
    case 'PluginQueryTasks':
    case 'QueryTasks':
      return Promise.resolve(cloneFixture(AUDIT_TASKS_ROWS))
    case 'PluginRawQuery':
      return Promise.resolve(resolveTaskPluginRawQuery(args, state))
    case 'PluginGetTaskUndoState':
      return Promise.resolve(cloneFixture(AUDIT_TASK_UNDO_STATE))
    case 'PluginUndoLastTaskMutation':
      return Promise.resolve(cloneFixture(AUDIT_TASK_UNDO_RESULT))
    case 'GetPluginSettingsForNotebook': {
      const mode =
        view === 'kanban' ? 'board' : view === 'calendar' ? 'calendar' : 'list'
      return Promise.resolve({
        default_display_mode: mode,
        week_start: 'monday',
        show_completed: true
      })
    }
    case 'GetActiveTheme':
    case 'ApplyTheme':
    case 'SetThemeMode':
      return Promise.resolve(cloneFixture(AUDIT_ACTIVE_THEME_RESULT))
    case 'ListThemes':
      return Promise.resolve(cloneFixture(AUDIT_THEMES_RESULT))
    case 'GetThemeJSON':
      return Promise.resolve(AUDIT_THEME_JSON)
    case 'ListTemplates':
      return Promise.resolve({
        templates: cloneFixture(AUDIT_TEMPLATES_LIST),
        errors: [],
        warnings: []
      })
    case 'GetTemplate': {
      const id = typeof args[0] === 'string' ? args[0] : 'architecture-rfc'
      const found =
        AUDIT_TEMPLATES_LIST.find((t) => t.id === id) ?? AUDIT_TEMPLATES_LIST[0]
      return Promise.resolve(cloneFixture(found))
    }
    case 'RenderTemplate':
      return Promise.resolve(AUDIT_TEMPLATES_LIST[0].body)
    case 'RenderTemplateBlocks':
      return Promise.resolve(cloneFixture(AUDIT_PAGE_BLOCKS))
    case 'GetAIProviderConfig':
      return Promise.resolve(cloneFixture(AUDIT_AI_PROVIDER_CONFIG))
    case 'GetAIAudit':
      return Promise.resolve(cloneFixture(AUDIT_AI_AUDIT_ENTRIES))
    case 'ListModels':
      return Promise.resolve(cloneFixture(AUDIT_AI_MODELS))
    case 'TestAIConnection':
      return Promise.resolve({
        ok: true,
        message: 'Connected to gpt-4o-mini (142ms)'
      })
    case 'GetMCPStatus':
    case 'GetLocalMCPStatus':
      return Promise.resolve(cloneFixture(AUDIT_MCP_STATUS))
    case 'GetLocalMCPConfig':
      return Promise.resolve(cloneFixture(AUDIT_MCP_CONFIG))
    case 'GetLocalMCPInstallHint':
      return Promise.resolve(AUDIT_MCP_INSTALL_HINT)
    case 'GetLocalMCPToken':
      return Promise.resolve('silt-mcp-audit-token-0123456789abcdef')
    case 'GetMCPAudit':
      return Promise.resolve(cloneFixture(AUDIT_MCP_AUDIT_ENTRIES))
    case 'GetUpdateSettings':
      return Promise.resolve(cloneFixture(AUDIT_UPDATE_SETTINGS))
    case 'GetAppVersion':
      return Promise.resolve(AUDIT_APP_VERSION)
    case 'GetLocalAuthor':
      return Promise.resolve(AUDIT_LOCAL_AUTHOR)
    case 'GetCloseToTray':
      return Promise.resolve(false)
    case 'ListSpellcheckDictionaries':
    case 'ListLanguagePacks':
      return Promise.resolve(cloneFixture(AUDIT_SPELLCHECK_LANGUAGE_PACKS))
    case 'ListDomainPacks':
      return Promise.resolve(cloneFixture(AUDIT_SPELLCHECK_DOMAIN_PACKS))
    case 'GetCustomDictionary':
      return Promise.resolve(cloneFixture(AUDIT_CUSTOM_DICTIONARY))
    case 'GetLanguagePackContent':
      return Promise.resolve({ aff: 'SET UTF-8\n', dic: '2\nsilt\nwails\n' })
    case 'GetDomainPackWords':
      return Promise.resolve(['TipTap', 'Svelte', 'SQLite'])
    case 'ListPlugins':
      return Promise.resolve([])
    case 'GetGrantedCapabilities':
      return Promise.resolve({})
    case 'RegisterPluginSession':
      return Promise.resolve('harness-plugin-session-token')
    case 'GetPluginSecurityStats':
      return Promise.resolve(cloneFixture(AUDIT_PLUGIN_SECURITY_STATS))
    case 'MarkFrontendReady':
    case 'ListQuarantinedLinks':
    case 'GetPendingGrantsMigration':
    case 'GetNetworkAuditLog':
    case 'ListPageVersions':
    case 'ListDeletedPages':
      return Promise.resolve([])
    default:
      return Promise.resolve(null)
  }
}
