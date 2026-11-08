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
    it('returns populated fixtures across all domains in default state', async () => {
      const q = '?harness=1&view=notes&state=default'
      expect(await invokeHarnessBinding('IsVaultInitialized', [], q)).toBe(true)

      const cfg = (await invokeHarnessBinding('GetSystemConfig', [], q)) as {
        vault_id: string
      }
      expect(cfg.vault_id).toBe('vault-audit-001')

      const nav = (await invokeHarnessBinding('ListNavigation', [], q)) as {
        notebooks: { name: string }[]
      }
      expect(nav.notebooks.length).toBeGreaterThan(0)

      const blocks = (await invokeHarnessBinding(
        'FetchPageBlocks',
        ['Product', 'Architecture', 'Design System Roadmap'],
        q
      )) as { type: string }[]
      expect(blocks.map((b) => b.type)).toEqual(
        expect.arrayContaining([
          'heading',
          'note',
          'callout',
          'task',
          'code',
          'table'
        ])
      )

      const tags = (await invokeHarnessBinding('GetTags', [], q)) as unknown[]
      expect(tags.length).toBeGreaterThan(0)

      const tagBlocks = (await invokeHarnessBinding(
        'GetBlocksByTag',
        ['ui/tokens'],
        q
      )) as unknown[]
      expect(tagBlocks.length).toBeGreaterThan(0)

      const types = (await invokeHarnessBinding(
        'ListTypes',
        [],
        q
      )) as unknown[]
      expect(types.length).toBeGreaterThan(0)

      const tasks = (await invokeHarnessBinding(
        'PluginQueryTasks',
        [],
        q
      )) as unknown[]
      expect(tasks.length).toBeGreaterThan(0)

      const themes = (await invokeHarnessBinding(
        'ListThemes',
        [],
        q
      )) as unknown[]
      expect(themes.length).toBeGreaterThan(0)

      const templates = (await invokeHarnessBinding(
        'ListTemplates',
        [],
        q
      )) as unknown[]
      expect(templates.length).toBeGreaterThan(0)

      const aiCfg = (await invokeHarnessBinding(
        'GetAIProviderConfig',
        [],
        q
      )) as { model: string }
      expect(aiCfg.model).toBe('gpt-4o-mini')

      const mcpStatus = (await invokeHarnessBinding('GetMCPStatus', [], q)) as {
        state: string
      }
      expect(mcpStatus.state).toBe('running')

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
      expect(await invokeHarnessBinding('GetActiveTheme', [], q)).toEqual({
        id: 'cyber-ink',
        mode: 'dark'
      })

      expect(await invokeHarnessBinding('ListTypes', [], q)).toEqual([])
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
