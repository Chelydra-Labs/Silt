// Regression coverage for the PluginContext pin/progress sentinel translation
// (#123) and the per-active-notebook settings resolver (#133). The Go
// bindings PluginUpdateTaskMeta + GetPluginSettingsForNotebook take raw
// args; the SDK wrapper in context.ts must translate the ergonomic API onto
// them exactly. Never hit real IPC — mock the Wails bindings (AGENTS.md
// canonical pattern).

import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest'

const mocks = vi.hoisted(() => ({
  pluginUpdateTaskMeta: vi.fn(() => Promise.resolve(true)),
  pluginRawQuery: vi.fn(() => Promise.resolve({ rows: [], truncated: false })),
  pluginMutateBlock: vi.fn(() => Promise.resolve(true)),
  pluginUpdateBlockState: vi.fn(() => Promise.resolve(true)),
  pluginSetTaskOrder: vi.fn(() => Promise.resolve(true)),
  pluginSetTaskStartDate: vi.fn(() => Promise.resolve(true)),
  getPluginSettingsForNotebook: vi.fn(() => Promise.resolve({})),
  pluginListPageVersions: vi.fn(
    (): Promise<
      Array<{ id: string; timestamp: string; source: string; bytes: number }>
    > => Promise.resolve([])
  ),
  pluginGetPageVersion: vi.fn(() => Promise.resolve('')),
  pluginRestorePageVersion: vi.fn(() => Promise.resolve()),
  getActiveLocation: vi.fn(() => ({
    notebook: 'Work',
    section: 'Journal',
    page: 'Daily'
  }))
}))

vi.mock('$silt-app', () =>
  createAppIpcMocks({
    PluginRawQuery: mocks.pluginRawQuery,
    PluginMutateBlock: mocks.pluginMutateBlock,
    PluginUpdateBlockState: mocks.pluginUpdateBlockState,
    PluginUpdateTaskMeta: mocks.pluginUpdateTaskMeta,
    PluginSetTaskOrder: mocks.pluginSetTaskOrder,
    PluginSetTaskStartDate: mocks.pluginSetTaskStartDate,
    GetPluginSettingsForNotebook: mocks.getPluginSettingsForNotebook,
    PluginListPageVersions: mocks.pluginListPageVersions,
    PluginGetPageVersion: mocks.pluginGetPageVersion,
    PluginRestorePageVersion: mocks.pluginRestorePageVersion
  })
)

vi.mock('./location.svelte', () => ({
  getActiveLocation: mocks.getActiveLocation
}))

import { makePluginContext } from './context'
import {
  editorKey,
  registerEditor,
  _resetEditorRegistryForTests
} from '../lib/editor/editorRegistry.svelte'

describe('makePluginContext — updateTaskMeta sentinel translation', () => {
  // F1 (#236): the SDK now threads pluginID + sessionToken through every
  // privileged binding. makePluginContext(pluginID, sessionToken?) captures
  // them; the assertions verify the full call signature.
  beforeEach(() => {
    mocks.pluginUpdateTaskMeta.mockClear()
  })

  it('maps pin true → 1', async () => {
    const ctx = makePluginContext('test-plugin')
    await ctx.updateTaskMeta('b1', { pinned: true })
    expect(mocks.pluginUpdateTaskMeta).toHaveBeenCalledWith(
      'test-plugin',
      '',
      'b1',
      1,
      -1
    )
  })

  it('maps pin false → 0 (explicit [pin:: false], #123)', async () => {
    const ctx = makePluginContext('test-plugin')
    await ctx.updateTaskMeta('b1', { pinned: false })
    expect(mocks.pluginUpdateTaskMeta).toHaveBeenCalledWith(
      'test-plugin',
      '',
      'b1',
      0,
      -1
    )
  })

  it('maps pin null → -2 (clear the token, #123)', async () => {
    const ctx = makePluginContext('test-plugin')
    await ctx.updateTaskMeta('b1', { pinned: null })
    expect(mocks.pluginUpdateTaskMeta).toHaveBeenCalledWith(
      'test-plugin',
      '',
      'b1',
      -2,
      -1
    )
  })

  it('maps omitted pin → -1 (no change)', async () => {
    const ctx = makePluginContext('test-plugin')
    await ctx.updateTaskMeta('b1', { progress: 50 })
    expect(mocks.pluginUpdateTaskMeta).toHaveBeenCalledWith(
      'test-plugin',
      '',
      'b1',
      -1,
      50
    )
  })

  it('maps progress undefined → -1, number → itself', async () => {
    const ctx = makePluginContext('test-plugin')
    await ctx.updateTaskMeta('b1', { progress: 75 })
    expect(mocks.pluginUpdateTaskMeta).toHaveBeenCalledWith(
      'test-plugin',
      '',
      'b1',
      -1,
      75
    )
  })

  it('maps both fields together', async () => {
    const ctx = makePluginContext('test-plugin')
    await ctx.updateTaskMeta('b1', { pinned: true, progress: 100 })
    expect(mocks.pluginUpdateTaskMeta).toHaveBeenCalledWith(
      'test-plugin',
      '',
      'b1',
      1,
      100
    )
  })

  it('threads a captured session token through (#236)', async () => {
    const ctx = makePluginContext('test-plugin', 'tok-abc')
    await ctx.updateTaskMeta('b1', { pinned: true })
    expect(mocks.pluginUpdateTaskMeta).toHaveBeenCalledWith(
      'test-plugin',
      'tok-abc',
      'b1',
      1,
      -1
    )
  })
})

// setTaskOrder wiring (#426): the SDK closure must thread pluginID +
// sessionToken through PluginSetTaskOrder so the Go side can verify the
// caller's identity (the same F1 contract as updateTaskMeta).
describe('makePluginContext — setTaskOrder wiring (#426)', () => {
  beforeEach(() => {
    mocks.pluginSetTaskOrder.mockClear()
  })

  it('calls PluginSetTaskOrder with the captured pluginID + token', async () => {
    const ctx = makePluginContext('silt-tasks', 'tok-xyz')
    await ctx.setTaskOrder('block-1', 7)
    expect(mocks.pluginSetTaskOrder).toHaveBeenCalledWith(
      'silt-tasks',
      'tok-xyz',
      'block-1',
      7
    )
  })

  it('passes 0 through verbatim (clears the [order::] token)', async () => {
    const ctx = makePluginContext('silt-tasks')
    await ctx.setTaskOrder('block-1', 0)
    expect(mocks.pluginSetTaskOrder).toHaveBeenCalledWith(
      'silt-tasks',
      '',
      'block-1',
      0
    )
  })
})

describe('makePluginContext — setTaskStartDate wiring', () => {
  beforeEach(() => {
    mocks.pluginSetTaskStartDate.mockClear()
  })

  it('threads identity and preserves set/clear values verbatim', async () => {
    const ctx = makePluginContext('silt-tasks', 'tok-start')

    await ctx.setTaskStartDate('block-1', '2026-08-03')
    await ctx.setTaskStartDate('block-1', '')

    expect(mocks.pluginSetTaskStartDate).toHaveBeenNthCalledWith(
      1,
      'silt-tasks',
      'tok-start',
      'block-1',
      '2026-08-03'
    )
    expect(mocks.pluginSetTaskStartDate).toHaveBeenNthCalledWith(
      2,
      'silt-tasks',
      'tok-start',
      'block-1',
      ''
    )
  })
})

describe('makePluginContext — getPluginSettings (#133)', () => {
  beforeEach(() => {
    mocks.getPluginSettingsForNotebook.mockClear()
    mocks.getActiveLocation.mockReset()
  })

  it('calls GetPluginSettingsForNotebook with the captured pluginID + live notebook', async () => {
    // The real getActiveLocation returns the SAME $state-backed object on
    // every call; its properties mutate in place (#69). Mirror that here so
    // the context's captured `loc` reference sees live navigation changes.
    const loc = { notebook: 'Work', section: 'Journal', page: 'Daily' }
    mocks.getActiveLocation.mockReturnValue(loc)
    mocks.getPluginSettingsForNotebook.mockResolvedValue({ columns: ['TODO'] })
    const ctx = makePluginContext('silt-tasks')
    const got = await ctx.getPluginSettings()
    expect(mocks.getPluginSettingsForNotebook).toHaveBeenCalledWith(
      'silt-tasks',
      'Work'
    )
    expect(got).toEqual({ columns: ['TODO'] })
  })

  it('normalizes a null/undefined response to an empty object', async () => {
    mocks.getActiveLocation.mockReturnValue({
      notebook: 'Work',
      section: '',
      page: ''
    })
    mocks.getPluginSettingsForNotebook.mockResolvedValue(
      null as unknown as Record<string, unknown>
    )
    const ctx = makePluginContext('p')
    const got = await ctx.getPluginSettings()
    expect(got).toEqual({})
  })

  it('reads the live active notebook at call time (not capture time)', async () => {
    // Simulate in-app navigation by mutating the SAME $state-backed object
    // the context captured (mirrors how location.svelte.ts works: the
    // object is stable, its properties change).
    const loc = { notebook: 'Work', section: 'Journal', page: 'Daily' }
    mocks.getActiveLocation.mockReturnValue(loc)
    mocks.getPluginSettingsForNotebook.mockResolvedValue({})
    const ctx = makePluginContext('silt-tasks')

    // Navigate to a linked notebook AFTER context construction. The reactive
    // getter must reflect the new value at the next getPluginSettings call.
    loc.notebook = 'Linked'
    await ctx.getPluginSettings()
    expect(mocks.getPluginSettingsForNotebook).toHaveBeenCalledWith(
      'silt-tasks',
      'Linked'
    )
  })
})

// openSettings (#472): dispatches the existing 'open-settings' DOM CustomEvent
// that App.svelte listens for, so plugins can deep-link into Settings without
// a new Go binding. Verified at the SDK boundary — no IPC involved.
describe('makePluginContext — openSettings dispatches open-settings event', () => {
  it('dispatches with the given tab detail', () => {
    const ctx = makePluginContext('test-plugin')
    const events: CustomEvent[] = []
    const handler = (e: Event) => events.push(e as CustomEvent)
    window.addEventListener('open-settings', handler)
    ctx.openSettings('ai')
    ctx.openSettings()
    window.removeEventListener('open-settings', handler)
    expect(events).toHaveLength(2)
    expect(events[0].detail).toBe('ai')
    expect(events[1].detail).toBe('')
  })
})

describe('makePluginContext — page history wrappers', () => {
  afterEach(() => {
    _resetEditorRegistryForTests()
    mocks.pluginRestorePageVersion.mockReset().mockResolvedValue(undefined)
  })

  function registerDirtyEditor(opts?: { flushOk?: boolean }): {
    flush: ReturnType<typeof vi.fn>
    forceExternalReload: ReturnType<typeof vi.fn>
    clearExternalReload: ReturnType<typeof vi.fn>
  } {
    const flush = vi.fn(async () => opts?.flushOk !== false)
    const forceExternalReload = vi.fn()
    const clearExternalReload = vi.fn()
    registerEditor({
      key: editorKey('Work', 'Journal', 'Daily'),
      isDirty: () => true,
      flush,
      forceExternalReload,
      clearExternalReload,
      setProposedEdit: () => false,
      clearProposedEdit: () => {},
      hasProposal: () => false,
      acceptProposedEdit: () => false,
      verifySelectionText: () => true
    })
    return { flush, forceExternalReload, clearExternalReload }
  }

  it('threads session token through list/get/restore', async () => {
    mocks.pluginListPageVersions.mockResolvedValueOnce([
      {
        id: 'v1',
        timestamp: '2026-08-16T18:00:00Z',
        source: 'editor',
        bytes: 12
      }
    ])
    mocks.pluginGetPageVersion.mockResolvedValueOnce('# body')
    const ctx = makePluginContext('hist-plugin', 'tok-hist')
    await expect(
      ctx.listPageVersions('Work', 'Journal', 'Daily')
    ).resolves.toEqual([
      {
        id: 'v1',
        timestamp: '2026-08-16T18:00:00Z',
        source: 'editor',
        bytes: 12
      }
    ])
    expect(mocks.pluginListPageVersions).toHaveBeenCalledWith(
      'hist-plugin',
      'tok-hist',
      'Work',
      'Journal',
      'Daily'
    )
    await expect(
      ctx.getPageVersion('Work', 'Journal', 'Daily', 'v1')
    ).resolves.toBe('# body')
    expect(mocks.pluginGetPageVersion).toHaveBeenCalledWith(
      'hist-plugin',
      'tok-hist',
      'Work',
      'Journal',
      'Daily',
      'v1'
    )
    await expect(
      ctx.restorePageVersion('Work', 'Journal', 'Daily', 'v1')
    ).resolves.toBe(true)
    expect(mocks.pluginRestorePageVersion).toHaveBeenCalledWith(
      'hist-plugin',
      'tok-hist',
      'Work',
      'Journal',
      'Daily',
      'v1'
    )
  })

  it('flushes a dirty editor registered under a case-variant locator', async () => {
    const { flush, forceExternalReload } = registerDirtyEditor()
    const ctx = makePluginContext('hist-plugin', 'tok-hist')
    await expect(
      ctx.restorePageVersion('work', 'journal', 'daily', 'v1')
    ).resolves.toBe(true)
    expect(flush).toHaveBeenCalled()
    expect(forceExternalReload).toHaveBeenCalled()
  })

  it('flushes a dirty editor and arms external reload before restore', async () => {
    const { flush, forceExternalReload, clearExternalReload } =
      registerDirtyEditor()
    const ctx = makePluginContext('hist-plugin', 'tok-hist')
    await expect(
      ctx.restorePageVersion('Work', 'Journal', 'Daily', 'v1')
    ).resolves.toBe(true)
    expect(flush).toHaveBeenCalled()
    expect(forceExternalReload).toHaveBeenCalled()
    expect(mocks.pluginRestorePageVersion).toHaveBeenCalled()
    expect(clearExternalReload).not.toHaveBeenCalled()
  })

  it('fails restore when the dirty editor cannot flush', async () => {
    const { flush, forceExternalReload } = registerDirtyEditor({
      flushOk: false
    })
    const ctx = makePluginContext('hist-plugin', 'tok-hist')
    await expect(
      ctx.restorePageVersion('Work', 'Journal', 'Daily', 'v1')
    ).rejects.toThrow(/save the current page/)
    expect(flush).toHaveBeenCalled()
    expect(forceExternalReload).not.toHaveBeenCalled()
    expect(mocks.pluginRestorePageVersion).not.toHaveBeenCalled()
  })

  it('clears external reload when restore IPC fails', async () => {
    const { forceExternalReload, clearExternalReload } = registerDirtyEditor()
    mocks.pluginRestorePageVersion.mockRejectedValueOnce(new Error('nope'))
    const ctx = makePluginContext('hist-plugin', 'tok-hist')
    await expect(
      ctx.restorePageVersion('Work', 'Journal', 'Daily', 'v1')
    ).rejects.toThrow(/nope/)
    expect(forceExternalReload).toHaveBeenCalled()
    expect(clearExternalReload).toHaveBeenCalled()
  })
})
