import { beforeEach, describe, expect, it, vi } from 'vitest'

// Drive the plugin entry's side-effectful surface swap (mountForPage) through
// its event handlers. The pure decision (decideMountKind) is covered in
// index.test.ts; this pins the registerSurface/unregisterSurface call sequence
// for the banner↔chip transition + the idempotency guard.

const { mockRegister, mockUnregister, mockController, mockSettings } = vi.hoisted(() => ({
  mockRegister: vi.fn(),
  mockUnregister: vi.fn(),
  mockController: {
    state: new Map<string, unknown>(),
    getSettings: vi.fn(),
    generateFor: vi.fn(async () => ({ ok: true, result: {} })),
    scheduleGenerate: vi.fn(),
    cancelPending: vi.fn(),
    clear: vi.fn(),
    dispose: vi.fn()
  },
  mockSettings: {
    config: { ai: { chat: { model: 'qwen3:30b', provider_type: 'local' } } }
  }
}))

vi.mock('../../surfaces', () => ({
  registerSurface: mockRegister,
  unregisterSurface: mockUnregister
}))
vi.mock('./state.svelte', () => ({
  createSummaryController: () => mockController,
  readProviderInfo: () => ({ isConfigured: true, configuredModel: 'qwen3:30b' })
}))
vi.mock('./cache', () => ({ resetCacheState: vi.fn() }))
// Stub the Svelte components so the test doesn't pull in their transitive
// import chains — only the reference is passed to registerSurface.
vi.mock('./SummaryBanner.svelte', () => ({ default: vi.fn() }))
vi.mock('./AISummaryPanel.svelte', () => ({ default: vi.fn() }))
vi.mock('./AISummarySettings.svelte', () => ({ default: vi.fn() }))
vi.mock('../../../settings/store.svelte', () => ({
  settings: mockSettings,
  loadConfig: vi.fn(),
  saveConfig: vi.fn(),
  updatePluginSetting: vi.fn()
}))

import plugin from './index'
import type { PluginContext } from '../../sdk'

const BASE_SETTINGS = {
  auto_on_open: true,
  on_demand_only: false,
  summary_length: 'medium' as const,
  facets: { tasks: true, risks: true, decisions: true },
  regenerate_debounce_ms: 3000,
  max_note_chars: 12000,
  dismissed_notes: [] as string[]
}

function makeCtx() {
  const handlers: Record<string, (evt: unknown) => void> = {}
  const ctx = {
    activeNotebook: 'NB',
    activeSection: 'S',
    activePage: 'P',
    on: vi.fn((evt: string, handler: (evt: unknown) => void) => {
      handlers[evt] = handler
      return () => {}
    }),
    updatePluginSetting: vi.fn(async () => true)
  } as unknown as PluginContext
  return { ctx, handlers }
}

function noteEvt() {
  return { notebook: 'NB', section: 'S', page: 'P' }
}

describe('mountForPage surface swap', () => {
  let ctx: ReturnType<typeof makeCtx>['ctx']
  let handlers: ReturnType<typeof makeCtx>['handlers']

  beforeEach(() => {
    // Reset module-level state (controller, mountedKind, lastPageId) so each
    // test starts clean. onVaultClose nulls everything + unregisters.
    plugin.onVaultClose!()
    mockRegister.mockClear()
    mockUnregister.mockClear()
    mockController.generateFor.mockClear()
    mockController.getSettings.mockReset()
    mockController.getSettings.mockReturnValue({ ...BASE_SETTINGS, facets: { ...BASE_SETTINGS.facets } })
    const made = makeCtx()
    ctx = made.ctx
    handlers = made.handlers
    plugin.onVaultOpen(ctx)
  })

  it('registers the banner surface on note open (not dismissed, not on-demand)', () => {
    handlers['active-notebook:changed'](noteEvt())
    expect(mockRegister).toHaveBeenCalledTimes(1)
    expect(mockRegister.mock.calls[0][0]).toMatchObject({
      id: 'silt-ai-summary:banner',
      kind: 'note-banner'
    })
  })

  it('swaps banner → re-open chip when the note is already dismissed', () => {
    // First open mounts the banner.
    handlers['active-notebook:changed'](noteEvt())
    // Re-open with the note now dismissed.
    mockController.getSettings.mockReturnValue({
      ...BASE_SETTINGS,
      facets: { ...BASE_SETTINGS.facets },
      dismissed_notes: ['NB/S/P']
    })
    mockRegister.mockClear()
    mockUnregister.mockClear()
    handlers['active-notebook:changed'](noteEvt())
    expect(mockUnregister).toHaveBeenCalledWith('silt-ai-summary:banner')
    expect(mockRegister).toHaveBeenCalledTimes(1)
    expect(mockRegister.mock.calls[0][0]).toMatchObject({
      id: 'silt-ai-summary:reopen',
      kind: 'status-bar-item'
    })
  })

  it('is a no-op when the target kind is already mounted (idempotency guard)', () => {
    handlers['active-notebook:changed'](noteEvt())
    const callsAfterFirst = mockRegister.mock.calls.length
    // Re-fire the same event → same decision → mountForPage returns early.
    handlers['active-notebook:changed'](noteEvt())
    expect(mockRegister.mock.calls.length).toBe(callsAfterFirst)
  })

  it('transitions chip → banner on the re-open click (async persist handoff)', async () => {
    // Mount the chip (dismissed state).
    mockController.getSettings.mockReturnValue({
      ...BASE_SETTINGS,
      facets: { ...BASE_SETTINGS.facets },
      dismissed_notes: ['NB/S/P']
    })
    handlers['active-notebook:changed'](noteEvt())
    // Grab the chip's onClick from the registerSurface call.
    const chipCall = mockRegister.mock.calls.find(
      (c) => c[0].id === 'silt-ai-summary:reopen'
    )
    expect(chipCall).toBeTruthy()
    const onClick = chipCall![0].onClick as () => void
    mockRegister.mockClear()
    mockUnregister.mockClear()
    // Click the chip → clears dismissal (persist) → mounts the banner.
    onClick()
    // The .then() runs after the persist promise resolves (microtask queue).
    await new Promise((r) => setTimeout(r, 0))
    expect(mockUnregister).toHaveBeenCalledWith('silt-ai-summary:reopen')
    expect(mockRegister).toHaveBeenCalledTimes(1)
    expect(mockRegister.mock.calls[0][0]).toMatchObject({
      id: 'silt-ai-summary:banner',
      kind: 'note-banner'
    })
  })
})
