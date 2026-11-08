import { afterEach, describe, expect, it } from 'vitest'
import { System, Window } from '@wailsio/runtime'
import {
  installAuditHarnessTransport,
  invokeHarnessBinding,
  isAuditHarnessActive,
  isAuditHarnessTransportInstalled,
  parseAuditHarnessParams,
  setAuditHarnessTestOverrides,
  transformAppBindingsForHarness
} from './auditHarness'

describe('auditHarness', () => {
  afterEach(() => {
    setAuditHarnessTestOverrides(null, null)
  })

  describe('parseAuditHarnessParams & isAuditHarnessActive', () => {
    it('returns disabled defaults when harness param and env flag are absent', () => {
      const parsed = parseAuditHarnessParams('')
      expect(parsed).toEqual({
        enabled: false,
        view: null,
        state: 'default',
        tab: null,
        dialog: null,
        tag: null
      })
      expect(isAuditHarnessActive('')).toBe(false)
    })

    it('parses valid view, state, tab, dialog, and tag query parameters', () => {
      const parsed = parseAuditHarnessParams(
        '?harness=1&view=settings&state=error&tab=appearance&dialog=confirm&tag=ui/tokens'
      )
      expect(parsed).toEqual({
        enabled: true,
        view: 'settings',
        state: 'error',
        tab: 'appearance',
        dialog: 'confirm',
        tag: 'ui/tokens'
      })
      expect(
        isAuditHarnessActive(
          '?harness=1&view=settings&state=error&tab=appearance'
        )
      ).toBe(true)
    })

    it('ignores unknown view, state, tab, and dialog values safely', () => {
      const parsed = parseAuditHarnessParams(
        '?harness=1&view=unknown&state=bogus&tab=nope&dialog=invalid'
      )
      expect(parsed).toEqual({
        enabled: true,
        view: null,
        state: 'default',
        tab: null,
        dialog: null,
        tag: null
      })
    })

    it('activates when VITE_AUDIT_HARNESS env override is enabled', () => {
      setAuditHarnessTestOverrides('?view=dashboard&state=empty', true)
      expect(isAuditHarnessActive()).toBe(true)
      expect(parseAuditHarnessParams()).toEqual({
        enabled: true,
        view: 'dashboard',
        state: 'empty',
        tab: null,
        dialog: null,
        tag: null
      })
    })
  })

  describe('invokeHarnessBinding — default & onboarding states', () => {
    it('returns populated Wails envelope fixtures across all domains in default state', async () => {
      const q = '?harness=1&view=notes&state=default'
      expect(await invokeHarnessBinding('IsVaultInitialized', [], q)).toBe(true)

      const cfg = (await invokeHarnessBinding('GetSystemConfig', [], q)) as {
        notebooks: { path: string; default_active: string }
        linked_notebooks: { id: string; root_path: string }[]
        editor: { spellcheck_enabled: boolean; font_size_px: number }
        parsing: { auto_inject_uuid: boolean }
        plugins: { active: string[] }
        ui: { sidebar_width: number }
        ai: {
          chat: { model: string }
          local_mcp: { http_enabled: boolean; http_port: number }
        }
      }
      expect(cfg.notebooks.default_active).toBe('Product')
      expect(cfg.linked_notebooks[0].id).toBe('linked-eng')
      expect(cfg.editor.spellcheck_enabled).toBe(true)
      expect(cfg.parsing.auto_inject_uuid).toBe(true)
      expect(cfg.plugins.active).toContain('silt-tasks')
      expect(cfg.ui.sidebar_width).toBe(264)
      expect(cfg.ai.chat.model).toBe('gpt-4o-mini')
      expect(cfg.ai.local_mcp.http_enabled).toBe(true)

      const nav = (await invokeHarnessBinding('ListNavigation', [], q)) as {
        notebooks: { name: string }[]
      }
      expect(nav.notebooks.length).toBeGreaterThan(0)

      const blocks = (await invokeHarnessBinding(
        'FetchPageBlocks',
        ['Product', 'Architecture', 'Design System Roadmap'],
        q
      )) as { type: string; raw_text: string; clean_text: string }[]
      expect(blocks.map((b) => b.type)).toEqual(
        expect.arrayContaining([
          'HEADER',
          'NOTE',
          'CALLOUT',
          'TASK',
          'CODE',
          'TABLE'
        ])
      )
      expect(blocks[0].raw_text).toContain('# Design System Roadmap')

      const tags = (await invokeHarnessBinding('GetTags', [], q)) as unknown[]
      expect(tags.length).toBeGreaterThan(0)

      const tagBlocks = (await invokeHarnessBinding(
        'GetBlocksByTag',
        ['ui/tokens'],
        q
      )) as unknown[]
      expect(tagBlocks.length).toBeGreaterThan(0)

      const typesRes = (await invokeHarnessBinding('ListTypes', [], q)) as {
        types: { id: string; name: string; heroField: string }[]
        errors: unknown[]
        warnings: unknown[]
      }
      expect(typesRes.types[0]).toMatchObject({
        id: 'project',
        name: 'Project',
        heroField: 'status'
      })
      expect(typesRes.errors).toEqual([])

      const pageTypeInfo = (await invokeHarnessBinding(
        'GetPageType',
        ['Product', 'Architecture', 'Design System Roadmap'],
        q
      )) as { typeId: string; isSet: boolean; rawType: string }
      expect(pageTypeInfo).toMatchObject({
        typeId: 'project',
        isSet: true,
        rawType: 'project'
      })

      const pageProps = (await invokeHarnessBinding(
        'GetPageProperties',
        ['Product', 'Architecture', 'Design System Roadmap'],
        q
      )) as { name: string; value: unknown; isSet: boolean }[]
      expect(pageProps[0]).toMatchObject({
        name: 'status',
        value: 'In Progress',
        isSet: true
      })

      const coreMeta = (await invokeHarnessBinding(
        'GetPageCoreMetadata',
        ['Product', 'Architecture', 'Design System Roadmap'],
        q
      )) as { type: string; tagsAreReadOnly: boolean }
      expect(coreMeta.type).toBe('project')
      expect(coreMeta.tagsAreReadOnly).toBe(false)

      const typeRows = (await invokeHarnessBinding(
        'QueryPagesByType',
        ['project'],
        q
      )) as { properties: { name: string; valueText: string }[] }[]
      expect(Array.isArray(typeRows[0].properties)).toBe(true)
      expect(typeRows[0].properties[0]).toMatchObject({
        name: 'status',
        valueText: 'In Progress'
      })

      const reproj = (await invokeHarnessBinding(
        'GetTypesReprojectionStatus',
        [],
        q
      )) as { active: boolean; processed: number; total: number }
      expect(reproj).toEqual({ active: false, processed: 4, total: 4 })

      const rawTasks = (await invokeHarnessBinding(
        'PluginRawQuery',
        [
          'silt-tasks',
          'token',
          "SELECT * FROM blocks b JOIN tasks t ON b.id = t.block_id WHERE t.status != 'DONE'",
          []
        ],
        q
      )) as { rows: { status: string; tags: string }[]; truncated: boolean }
      expect(rawTasks.truncated).toBe(false)
      expect(rawTasks.rows.length).toBe(2)
      expect(rawTasks.rows.every((r) => r.status !== 'DONE')).toBe(true)
      expect(rawTasks.rows[0].tags).toBe('ui/tokens|design')

      const doneTasks = (await invokeHarnessBinding(
        'PluginRawQuery',
        [
          'silt-tasks',
          'token',
          "SELECT * FROM blocks b JOIN tasks t ON b.id = t.block_id WHERE t.status = 'DONE'",
          []
        ],
        q
      )) as { rows: { status: string }[]; truncated: boolean }
      expect(doneTasks.rows).toHaveLength(1)
      expect(doneTasks.rows[0].status).toBe('DONE')

      const undoState = (await invokeHarnessBinding(
        'PluginGetTaskUndoState',
        ['silt-tasks', 'token'],
        q
      )) as { canUndo: boolean; remaining: number }
      expect(undoState.canUndo).toBe(true)
      expect(undoState.remaining).toBe(1)

      const undoRes = (await invokeHarnessBinding(
        'PluginUndoLastTaskMutation',
        ['silt-tasks', 'token'],
        q
      )) as { undone: boolean; remaining: number }
      expect(undoRes.undone).toBe(true)
      expect(undoRes.remaining).toBe(0)

      const activeTheme = (await invokeHarnessBinding(
        'GetActiveTheme',
        [],
        q
      )) as {
        id: string
        mode: string
        tokens: Record<string, string>
        dark_tokens: Record<string, string>
        light_tokens: Record<string, string>
        bg_void: string
      }
      expect(activeTheme.id).toBe('cyber_forest')
      expect(activeTheme.mode).toBe('dark')
      expect(activeTheme.dark_tokens['--color-surface-app']).toBe('#0d1117')
      expect(activeTheme.light_tokens['--color-surface-app']).toBe('#f8fafc')

      const themesRes = (await invokeHarnessBinding('ListThemes', [], q)) as {
        themes: { id: string; swatches: string[]; source: string }[]
        errors: unknown[]
        flat_tokens: Record<string, { dark: Record<string, string> }>
      }
      expect(themesRes.themes.length).toBeGreaterThan(0)
      expect(themesRes.themes[0].swatches.length).toBeGreaterThan(0)
      expect(themesRes.flat_tokens.cyber_forest.dark).toBeDefined()

      const themeDoc = JSON.parse(
        (await invokeHarnessBinding(
          'GetThemeJSON',
          ['cyber_forest'],
          q
        )) as string
      ) as { schema_version: string; modes: { dark: { surfaces: unknown } } }
      expect(themeDoc.schema_version).toBe('2.0.0')
      expect(themeDoc.modes.dark.surfaces).toBeDefined()

      const templatesRes = (await invokeHarnessBinding(
        'ListTemplates',
        [],
        q
      )) as {
        templates: { placeholders: { name: string; description: string }[] }[]
        errors: unknown[]
        warnings: unknown[]
      }
      expect(templatesRes.templates.length).toBeGreaterThan(0)
      expect(templatesRes.templates[0].placeholders[0].description).toBe(
        'Feature Name'
      )

      const aiCfg = (await invokeHarnessBinding(
        'GetAIProviderConfig',
        [],
        q
      )) as { chat: { model: string; has_key: boolean } }
      expect(aiCfg.chat.model).toBe('gpt-4o-mini')
      expect(aiCfg.chat.has_key).toBe(true)

      const aiProbe = (await invokeHarnessBinding(
        'TestAIConnection',
        [],
        q
      )) as { ok: boolean; message: string }
      expect(aiProbe.ok).toBe(true)

      const aiAudit = (await invokeHarnessBinding('GetAIAudit', [], q)) as {
        plugin: string
        kind: string
        at: string
      }[]
      expect(aiAudit[0]).toMatchObject({
        plugin: 'silt-ai-qa',
        kind: 'chat'
      })

      const mcpStatus = (await invokeHarnessBinding('GetMCPStatus', [], q)) as {
        state: string
      }
      expect(mcpStatus.state).toBe('running')

      const updateSettings = (await invokeHarnessBinding(
        'GetUpdateSettings',
        [],
        q
      )) as { autoCheck: boolean; shouldAutoCheck: boolean }
      expect(updateSettings.autoCheck).toBe(true)

      const secStats = (await invokeHarnessBinding(
        'GetPluginSecurityStats',
        [],
        q
      )) as { pluginId: string; denials: number; rateLimited: number }[]
      expect(secStats[0]).toEqual({
        pluginId: 'silt-tasks',
        denials: 0,
        rateLimited: 0
      })

      const dicts = (await invokeHarnessBinding(
        'ListSpellcheckDictionaries',
        [],
        q
      )) as unknown[]
      expect(dicts.length).toBeGreaterThan(0)
    })

    it('returns IsVaultInitialized() === false when view=onboarding', async () => {
      const q = '?harness=1&view=onboarding'
      expect(await invokeHarnessBinding('IsVaultInitialized', [], q)).toBe(
        false
      )
    })

    it('maps GetPluginSettingsForNotebook display mode to view=tasks|kanban|calendar', async () => {
      const listSettings = (await invokeHarnessBinding(
        'GetPluginSettingsForNotebook',
        ['silt-tasks', 'Product'],
        '?harness=1&view=tasks'
      )) as { default_display_mode: string }
      expect(listSettings.default_display_mode).toBe('list')

      const boardSettings = (await invokeHarnessBinding(
        'GetPluginSettingsForNotebook',
        ['silt-tasks', 'Product'],
        '?harness=1&view=kanban'
      )) as { default_display_mode: string }
      expect(boardSettings.default_display_mode).toBe('board')

      const calendarSettings = (await invokeHarnessBinding(
        'GetPluginSettingsForNotebook',
        ['silt-tasks', 'Product'],
        '?harness=1&view=calendar'
      )) as { default_display_mode: string }
      expect(calendarSettings.default_display_mode).toBe('calendar')
    })
  })

  describe('invokeHarnessBinding — empty, error, and loading states', () => {
    it('returns empty collections for content queries when state=empty while keeping bootstrap working', async () => {
      const q = '?harness=1&view=dashboard&state=empty'
      expect(await invokeHarnessBinding('IsVaultInitialized', [], q)).toBe(true)
      const activeTheme = (await invokeHarnessBinding(
        'GetActiveTheme',
        [],
        q
      )) as { id: string; mode: string }
      expect(activeTheme.id).toBe('cyber_forest')
      expect(activeTheme.mode).toBe('dark')

      expect(await invokeHarnessBinding('ListTypes', [], q)).toEqual({
        types: [],
        errors: [],
        warnings: []
      })
      expect(await invokeHarnessBinding('ListTemplates', [], q)).toEqual({
        templates: [],
        errors: [],
        warnings: []
      })
      expect(await invokeHarnessBinding('PluginRawQuery', [], q)).toEqual({
        rows: [],
        truncated: false
      })
      expect(
        await invokeHarnessBinding('QueryPagesByType', ['project'], q)
      ).toEqual([])
      expect(await invokeHarnessBinding('GetTags', [], q)).toEqual([])
      expect(
        await invokeHarnessBinding('GetBlocksByTag', ['ui/tokens'], q)
      ).toEqual([])
      expect(await invokeHarnessBinding('PluginQueryTasks', [], q)).toEqual([])
      expect(await invokeHarnessBinding('GetBacklinksPaged', [], q)).toEqual({
        results: [],
        cursor: '',
        hasMore: false
      })
      expect(
        await invokeHarnessBinding('GetUnlinkedMentionsPaged', [], q)
      ).toEqual({
        results: [],
        cursor: '',
        hasMore: false
      })
      expect(await invokeHarnessBinding('SearchBlocksPaged', [], q)).toEqual({
        results: [],
        total: 0,
        offset: 0,
        limit: 20,
        has_more: false
      })
      expect(await invokeHarnessBinding('SearchPages', [], q)).toEqual([])

      const notesEmpty = '?harness=1&view=notes&state=empty'
      expect(
        await invokeHarnessBinding('ListNavigation', [], notesEmpty)
      ).toEqual({
        notebooks: []
      })
      expect(await invokeHarnessBinding('GetOpenTabs', [], notesEmpty)).toEqual(
        {
          open_tabs: [],
          active_tab: null
        }
      )
    })

    it('rejects content queries and onboarding folder pick with HARNESS_ERROR when state=error', async () => {
      const q = '?harness=1&view=tags&state=error'
      // Shell bootstrap still succeeds so the target view mounts
      expect(await invokeHarnessBinding('IsVaultInitialized', [], q)).toBe(true)
      expect(await invokeHarnessBinding('GetSystemConfig', [], q)).toBeTruthy()

      await expect(invokeHarnessBinding('GetTags', [], q)).rejects.toThrow(
        /HARNESS_ERROR/
      )
      await expect(
        invokeHarnessBinding('QueryBlocksByTag', ['ui/tokens'], q)
      ).rejects.toThrow(/HARNESS_ERROR/)

      await expect(
        invokeHarnessBinding(
          'PickNotebookFolder',
          [],
          '?harness=1&view=onboarding&state=error'
        )
      ).rejects.toThrow(/HARNESS_ERROR/)
    })

    it('returns a pending non-resolving Promise for content queries when state=loading', async () => {
      const q = '?harness=1&view=tasks&state=loading'
      expect(await invokeHarnessBinding('IsVaultInitialized', [], q)).toBe(true)

      const settled = await Promise.race([
        invokeHarnessBinding('PluginQueryTasks', [], q).then(() => 'resolved'),
        new Promise<string>((resolve) =>
          setTimeout(() => resolve('pending'), 25)
        )
      ])
      expect(settled).toBe('pending')
    })
  })

  describe('@wailsio/runtime transport & app.js transform', () => {
    it('installs the Wails runtime transport stub when harness mode is active', async () => {
      setAuditHarnessTestOverrides('?harness=1', false)
      expect(installAuditHarnessTransport()).toBe(true)
      expect(isAuditHarnessTransportInstalled()).toBe(true)

      await expect(Window.IsMaximised()).resolves.toBe(false)
      await expect(Window.Size()).resolves.toEqual({
        width: 1440,
        height: 900
      })
      await expect(System.IsDarkMode()).resolves.toBe(true)
    })

    it('transforms bindings/silt/app.js exports to check isAuditHarnessActive()', () => {
      const sample = [
        'export function IsVaultInitialized() {',
        '    return $Call.ByID(123);',
        '}',
        'export function FetchPageBlocks(notebook, section, page) {',
        '    return $Call.ByID(456, notebook, section, page);',
        '}'
      ].join('\n')

      const output = transformAppBindingsForHarness(sample, './auditHarness.ts')
      expect(output).toContain(
        'import { isAuditHarnessActive, invokeHarnessBinding } from "./auditHarness.ts";'
      )
      expect(output).toContain(
        'if (isAuditHarnessActive()) return invokeHarnessBinding("IsVaultInitialized", []);'
      )
      expect(output).toContain(
        'if (isAuditHarnessActive()) return invokeHarnessBinding("FetchPageBlocks", [notebook, section, page]);'
      )
    })
  })
})
