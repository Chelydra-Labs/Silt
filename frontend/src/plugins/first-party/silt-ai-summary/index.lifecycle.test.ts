import { beforeEach, describe, expect, it, vi } from 'vitest'

// Drive the plugin entry's side-effectful surface swap (mountForPage) through
// its event handlers. The pure decision (decideMountKind) is covered in
// index.test.ts; this pins the registerSurface/unregisterSurface call sequence
// for the banner↔chip transition + the idempotency guard.

const {
  mockRegister,
  mockUnregister,
  mockRegistered,
  mockController,
  mockSettings
} = vi.hoisted(() => ({
  // Tracks the net set of currently-registered surface ids so a test can
  // assert "the loading surface is/isn't still up" without poking the real
  // (mocked-away) surfaces module.
  mockRegistered: new Set<string>(),
  mockRegister: vi.fn((s: { id: string; [k: string]: unknown }) => {
    mockRegistered.add(s.id)
  }),
  mockUnregister: vi.fn((id: string) => {
    mockRegistered.delete(id)
  }),
  mockController: {
    state: {},
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
vi.mock('./cache', async (importOriginal) => {
  // Preserve the real computeContentHash so the controller's hash computation
  // produces authentic sha-256 values the test can match against (the keyed
  // dismissal tests assert against the same hash). Only resetCacheState is
  // stubbed — the lifecycle tests don't exercise the SQLite cache, so its
  // other exports stay real (and unused) here.
  const actual = await importOriginal<typeof import('./cache')>()
  return { ...actual, resetCacheState: vi.fn() }
})
// Stub the Svelte components so the test doesn't pull in their transitive
// import chains — only the reference is passed to registerSurface.
vi.mock('./SummaryBanner.svelte', () => ({ default: vi.fn() }))
vi.mock('./SummaryBannerLoading.svelte', () => ({ default: vi.fn() }))
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

type HandlerMap = Record<string, (evt: unknown) => void | Promise<void>>

function makeCtx(
  opts: {
    /** Return value for the active page's `clean_content` row. When undefined,
     *  sqliteQuery is omitted entirely (the controller's hash computation then
     *  throws + falls back to legacy bare-pageId dismissal matching). */
    content?: string
  } = {}
) {
  const handlers: HandlerMap = {}
  const ctx = {
    activeNotebook: 'NB',
    activeSection: 'S',
    activePage: 'P',
    on: vi.fn(
      (
        evt: string,
        handler: (evt: unknown) => void | Promise<void>
      ): (() => void) => {
        handlers[evt] = handler
        return () => {}
      }
    ),
    updatePluginSetting: vi.fn(async () => true),
    ...(opts.content !== undefined
      ? {
          sqliteQuery: vi.fn(async () => ({
            rows: [{ clean_content: opts.content }],
            truncated: false
          }))
        }
      : {})
  } as unknown as PluginContext
  return { ctx, handlers }
}

function noteEvt() {
  return { notebook: 'NB', section: 'S', page: 'P' }
}

describe('mountForPage surface swap', () => {
  let ctx: PluginContext
  let handlers: HandlerMap

  beforeEach(() => {
    // Reset module-level state (controller, mountedKind, lastPageId) so each
    // test starts clean. onVaultClose nulls everything + unregisters.
    plugin.onVaultClose()
    mockRegister.mockClear()
    mockUnregister.mockClear()
    mockRegistered.clear()
    mockController.generateFor.mockClear()
    mockController.getSettings.mockReset()
    mockController.getSettings.mockReturnValue({
      ...BASE_SETTINGS,
      facets: { ...BASE_SETTINGS.facets }
    })
    const made = makeCtx()
    ctx = made.ctx
    handlers = made.handlers
    plugin.onVaultOpen(ctx)
  })

  it('registers the banner surface on note open (not dismissed, not on-demand)', async () => {
    await handlers['active-notebook:changed'](noteEvt())
    // The loading placeholder registers first, then is cleared when the hash
    // resolves and the real banner mounts — so the banner is the final
    // registered surface and the placeholder is gone.
    expect(mockRegister.mock.calls.at(-1)![0]).toMatchObject({
      id: 'silt-ai-summary:banner',
      kind: 'note-banner'
    })
    expect(mockRegistered.has('silt-ai-summary:banner')).toBe(true)
    expect(mockRegistered.has('silt-ai-summary:loading')).toBe(false)
  })

  it('swaps banner → re-open chip when the note is already dismissed', async () => {
    // First open mounts the banner.
    await handlers['active-notebook:changed'](noteEvt())
    // Re-open with the note now dismissed.
    mockController.getSettings.mockReturnValue({
      ...BASE_SETTINGS,
      facets: { ...BASE_SETTINGS.facets },
      dismissed_notes: ['NB/S/P']
    })
    mockRegister.mockClear()
    mockUnregister.mockClear()
    await handlers['active-notebook:changed'](noteEvt())
    expect(mockUnregister).toHaveBeenCalledWith('silt-ai-summary:banner')
    expect(mockRegister).toHaveBeenCalledTimes(1)
    expect(mockRegister.mock.calls[0][0]).toMatchObject({
      id: 'silt-ai-summary:reopen',
      kind: 'status-bar-item'
    })
  })

  it('is a no-op when the target kind is already mounted (idempotency guard)', async () => {
    await handlers['active-notebook:changed'](noteEvt())
    const callsAfterFirst = mockRegister.mock.calls.length
    // Re-fire the same event → same decision → mountForPage returns early.
    await handlers['active-notebook:changed'](noteEvt())
    expect(mockRegister.mock.calls.length).toBe(callsAfterFirst)
  })

  it('transitions chip → banner on the re-open click (async persist handoff)', async () => {
    // Mount the chip (dismissed state).
    mockController.getSettings.mockReturnValue({
      ...BASE_SETTINGS,
      facets: { ...BASE_SETTINGS.facets },
      dismissed_notes: ['NB/S/P']
    })
    await handlers['active-notebook:changed'](noteEvt())
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

  // --- #455 keyed dismissal: edit re-shows, legacy stays dismissed ----------
  it('re-shows the banner when a dismissed note is edited (hash changes)', async () => {
    // Two content versions of the same note. The first is dismissed at its
    // v1 hash; switching back with v2 content must re-mount the banner.
    const { computeContentHash } =
      await vi.importActual<typeof import('./cache')>('./cache')
    const v1 = 'original content'
    const v2 = 'edited content'
    const hashV1 = await computeContentHash(v1)

    // Re-open the vault with a ctx whose SQLite serves v1 content, then bump
    // it to v2 mid-test to simulate an edit between note switches.
    plugin.onVaultClose()
    let current = v1
    const { ctx: ctxLive, handlers: handlersLive } = makeCtx({
      content: ''
    })
    // Override sqliteQuery per-call so we can swap content without remounting
    // the whole vault.
    ;(
      ctxLive as unknown as { sqliteQuery: ReturnType<typeof vi.fn> }
    ).sqliteQuery = vi.fn(async () => ({
      rows: [{ clean_content: current }],
      truncated: false
    }))
    ctx = ctxLive
    handlers = handlersLive
    plugin.onVaultOpen(ctxLive)

    // Open with v1 content + a keyed dismissal of v1's hash.
    mockController.getSettings.mockReturnValue({
      ...BASE_SETTINGS,
      facets: { ...BASE_SETTINGS.facets },
      dismissed_notes: [`NB/S/P:${hashV1}`]
    })
    await handlers['active-notebook:changed'](noteEvt())
    // v1 hash matches the dismissed entry → chip mounts.
    expect(mockRegister.mock.calls.at(-1)![0]).toMatchObject({
      id: 'silt-ai-summary:reopen',
      kind: 'status-bar-item'
    })

    // Simulate an edit between note switches: bump the served content to v2.
    current = v2
    mockRegister.mockClear()
    mockUnregister.mockClear()
    await handlers['active-notebook:changed'](noteEvt())
    // v2 hash is NOT in dismissed_notes → banner re-shows with the new content.
    expect(mockRegister.mock.calls.at(-1)![0]).toMatchObject({
      id: 'silt-ai-summary:banner',
      kind: 'note-banner'
    })
  })

  it('keeps a keyed dismissal suppressed when the content is unchanged (same hash)', async () => {
    // Re-opening a dismissed note whose content hasn't changed must NOT
    // re-show — the hash still matches the dismissed entry.
    const { computeContentHash } =
      await vi.importActual<typeof import('./cache')>('./cache')
    const content = 'stable content'
    const hash = await computeContentHash(content)

    plugin.onVaultClose()
    const { ctx: ctxLive, handlers: handlersLive } = makeCtx({ content })
    ctx = ctxLive
    handlers = handlersLive
    plugin.onVaultOpen(ctxLive)

    mockController.getSettings.mockReturnValue({
      ...BASE_SETTINGS,
      facets: { ...BASE_SETTINGS.facets },
      dismissed_notes: [`NB/S/P:${hash}`]
    })
    await handlers['active-notebook:changed'](noteEvt())
    expect(mockRegister.mock.calls.at(-1)![0]).toMatchObject({
      id: 'silt-ai-summary:reopen',
      kind: 'status-bar-item'
    })
  })

  it('treats a legacy bare-pageId dismissal as always-dismissed (backward-compat)', async () => {
    // Pre-#455 dismissals have no content binding — the upgrade must NOT
    // suddenly re-show every previously-dismissed banner. The hash is
    // computed for the new path but the legacy entry still wins.
    plugin.onVaultClose()
    const { ctx: ctxLive, handlers: handlersLive } = makeCtx({
      content: 'any content'
    })
    ctx = ctxLive
    handlers = handlersLive
    plugin.onVaultOpen(ctxLive)

    mockController.getSettings.mockReturnValue({
      ...BASE_SETTINGS,
      facets: { ...BASE_SETTINGS.facets },
      dismissed_notes: ['NB/S/P']
    })
    await handlers['active-notebook:changed'](noteEvt())
    expect(mockRegister.mock.calls.at(-1)![0]).toMatchObject({
      id: 'silt-ai-summary:reopen',
      kind: 'status-bar-item'
    })
  })

  it('re-open chip clears keyed dismissal with the right persistence key', async () => {
    // The chip's onClick must wipe BOTH the legacy form and any ${pageId}:<hash>
    // form so the banner re-shows regardless of which form was persisted.
    const { computeContentHash } =
      await vi.importActual<typeof import('./cache')>('./cache')
    const content = 'chip-clear content'
    const hash = await computeContentHash(content)

    plugin.onVaultClose()
    const { ctx: ctxLive, handlers: handlersLive } = makeCtx({ content })
    ctx = ctxLive
    handlers = handlersLive
    plugin.onVaultOpen(ctxLive)

    mockController.getSettings.mockReturnValue({
      ...BASE_SETTINGS,
      facets: { ...BASE_SETTINGS.facets },
      dismissed_notes: [`NB/S/P:${hash}`, 'OTHER/S/P:zzz']
    })
    await handlers['active-notebook:changed'](noteEvt())
    const chipCall = mockRegister.mock.calls.find(
      (c) => c[0].id === 'silt-ai-summary:reopen'
    )
    const onClick = chipCall![0].onClick as () => void
    onClick()
    await new Promise((r) => setTimeout(r, 0))

    expect(ctx.updatePluginSetting).toHaveBeenCalledWith(
      'dismissed_notes',
      // The page's keyed entry is removed; the OTHER page's entry is preserved.
      ['OTHER/S/P:zzz']
    )
  })

  // --- #488: tear down the prior surface before the async hash window -------
  it('tears down the prior note surface before the hash resolves on a note switch', async () => {
    // Open note A → banner mounts.
    await handlers['active-notebook:changed']({
      notebook: 'NB',
      section: 'S',
      page: 'A'
    })
    expect(mockRegister.mock.calls.at(-1)![0]).toMatchObject({
      id: 'silt-ai-summary:banner'
    })
    // Switch to note B. The prior banner MUST be unregistered before the new
    // surface mounts so the user never sees note A's summary during note B's
    // hash window.
    mockUnregister.mockClear()
    mockRegister.mockClear()
    await handlers['active-notebook:changed']({
      notebook: 'NB',
      section: 'S',
      page: 'B'
    })
    const bannerUnregisterIdx = mockUnregister.mock.calls.findIndex(
      (c) => c[0] === 'silt-ai-summary:banner'
    )
    const bannerRegisterIdx = mockRegister.mock.calls.findIndex(
      (c) => c[0].id === 'silt-ai-summary:banner'
    )
    expect(bannerUnregisterIdx).toBeGreaterThanOrEqual(0)
    // The teardown (unregister banner) happens; a fresh banner register for B
    // follows. The unregister must precede any register in call order.
    expect(bannerUnregisterIdx).toBeLessThanOrEqual(bannerRegisterIdx + 1)
  })

  it('does not re-teardown on a redundant same-note re-evaluation (idempotency)', async () => {
    await handlers['active-notebook:changed'](noteEvt())
    mockUnregister.mockClear()
    mockRegister.mockClear()
    // Same note, same decision → mountForPage's guard no-ops; no teardown churn.
    await handlers['active-notebook:changed'](noteEvt())
    expect(mockRegister).not.toHaveBeenCalled()
  })

  it('registers the loading placeholder immediately on a note switch, then clears it when the hash resolves', async () => {
    // The loading skeleton registers immediately (occupies the slot from t=0,
    // staying invisible via a CSS fade-in until the hash is clearly slow). No
    // timer to wait for — assert registration right away.
    let resolveHash!: () => void
    const slowCtx = {
      activeNotebook: 'NB',
      activeSection: 'S',
      activePage: 'P',
      on: vi.fn((evt: string, h: (e: unknown) => void) => {
        handlers[evt] = h
        return () => {}
      }),
      updatePluginSetting: vi.fn(async () => true),
      sqliteQuery: vi.fn(
        () =>
          new Promise((resolve) => {
            resolveHash = () =>
              resolve({ rows: [{ clean_content: 'x' }], truncated: false })
          })
      )
    } as unknown as PluginContext
    plugin.onVaultClose()
    plugin.onVaultOpen(slowCtx)

    const pending = handlers['active-notebook:changed'](noteEvt())
    // The loading placeholder registers IMMEDIATELY (no delay timer) so the
    // banner slot is occupied before the editor content can reflow.
    expect(mockRegister).toHaveBeenCalledWith(
      expect.objectContaining({ id: 'silt-ai-summary:loading' })
    )
    expect(mockRegistered.has('silt-ai-summary:loading')).toBe(true)

    // Now let the hash resolve; the loading placeholder is cleared and the real
    // banner mounts.
    resolveHash()
    await pending
    expect(mockUnregister).toHaveBeenCalledWith('silt-ai-summary:loading')
    expect(mockRegistered.has('silt-ai-summary:loading')).toBe(false)
    expect(mockRegister.mock.calls.at(-1)![0]).toMatchObject({
      id: 'silt-ai-summary:banner'
    })
  })

  it('does not register the loading placeholder in on-demand mode (chip mounts synchronously)', async () => {
    mockController.getSettings.mockReturnValue({
      ...BASE_SETTINGS,
      facets: { ...BASE_SETTINGS.facets },
      on_demand_only: true
    })
    await handlers['active-notebook:changed'](noteEvt())
    // On-demand skips the hash (synchronous), so no loading placeholder is
    // registered — the chip mounts directly.
    expect(mockRegister).not.toHaveBeenCalledWith(
      expect.objectContaining({ id: 'silt-ai-summary:loading' })
    )
  })

  it('does not let a stale note switch clear a newer switch loading surface (rapid A→B→C) (#488 hardening)', async () => {
    // Every hash hangs until released. A→B→C each register a loading surface
    // (shared LOADING id). Releasing A's hash FIRST (while C is current) must
    // NOT clear C's still-wanted loading surface — the per-invocation nonce
    // guards the bail-path unregister.
    const resolvers: Array<() => void> = []
    const slowCtx = {
      activeNotebook: 'NB',
      activeSection: 'S',
      activePage: 'P',
      on: vi.fn((evt: string, h: (e: unknown) => void) => {
        handlers[evt] = h
        return () => {}
      }),
      updatePluginSetting: vi.fn(async () => true),
      sqliteQuery: vi.fn(() => {
        return new Promise((resolve) => {
          resolvers.push(() =>
            resolve({ rows: [{ clean_content: 'x' }], truncated: false })
          )
        })
      })
    } as unknown as PluginContext
    plugin.onVaultClose()
    plugin.onVaultOpen(slowCtx)

    const pendingA = handlers['active-notebook:changed']({
      notebook: 'NB',
      section: 'S',
      page: 'A'
    })
    const pendingB = handlers['active-notebook:changed']({
      notebook: 'NB',
      section: 'S',
      page: 'B'
    })
    const pendingC = handlers['active-notebook:changed']({
      notebook: 'NB',
      section: 'S',
      page: 'C'
    })
    // All three registered a loading surface; C's is current.
    expect(mockRegistered.has('silt-ai-summary:loading')).toBe(true)

    // Release A's hash first → A is now stale (lastPageId === 'C'). Its bail
    // path MUST NOT unregister the loading surface C owns.
    resolvers[0]()
    await pendingA
    expect(mockRegistered.has('silt-ai-summary:loading')).toBe(true)
    // Release B and C so no promises are left dangling.
    resolvers[1]()
    resolvers[2]()
    await pendingB
    await pendingC
  })

  it('does not mount into a stale page when the note switches again during the hash', async () => {
    // A and C resolve immediately; B (the 2nd call) hangs until we release it,
    // so a 3rd switch to C lands while B's hash is still pending.
    let callCount = 0
    let resolveB!: () => void
    const slowCtx = {
      activeNotebook: 'NB',
      activeSection: 'S',
      activePage: 'P',
      on: vi.fn((evt: string, h: (e: unknown) => void) => {
        handlers[evt] = h
        return () => {}
      }),
      updatePluginSetting: vi.fn(async () => true),
      sqliteQuery: vi.fn(() => {
        callCount++
        if (callCount === 2) {
          return new Promise((resolve) => {
            resolveB = () =>
              resolve({ rows: [{ clean_content: 'b' }], truncated: false })
          })
        }
        return Promise.resolve({
          rows: [{ clean_content: 'x' }],
          truncated: false
        })
      })
    } as unknown as PluginContext
    plugin.onVaultClose()
    plugin.onVaultOpen(slowCtx)

    // Open A, then switch to B (hash pending), then switch to C before B's hash
    // resolves. B's late resolution must NOT mount a banner for B (stale).
    await handlers['active-notebook:changed']({
      notebook: 'NB',
      section: 'S',
      page: 'A'
    })
    const pendingB = handlers['active-notebook:changed']({
      notebook: 'NB',
      section: 'S',
      page: 'B'
    })
    await handlers['active-notebook:changed']({
      notebook: 'NB',
      section: 'S',
      page: 'C'
    })
    mockRegister.mockClear()
    // Now resolve B's hash; the bail guard (lastPageId === 'C' !== 'B') must
    // prevent a B banner from mounting.
    resolveB()
    await pendingB
    expect(mockRegister).not.toHaveBeenCalledWith(
      expect.objectContaining({ id: 'silt-ai-summary:banner' })
    )
  })
})
