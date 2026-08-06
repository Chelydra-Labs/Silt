// Persistence-wiring coverage for the typed-notes dashboard saved-view IO
// layer (#863). The pure coerce/strip helpers are covered in
// dashboardSavedViews tests; this file pins the thin read/write surface that
// bridges the live `settings.config` snapshot with the
// `SetTypedNotesSavedViews` TOCTOU-hardened Go setter — including the
// newest-256 cap, optimistic mirror, and fail-loud error string contract.
//
// Mock pattern per AGENTS.md: vi.hoisted + createAppIpcMocks over the
// `$silt-app` alias (registered on globalThis by vitest.setup.ts so it is
// safe inside the hoisted factory). The alias resolves to the same physical
// file the production module imports via the relative bindings path, so
// mocking `$silt-app` intercepts `SetTypedNotesSavedViews`.

import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest'

const appMocks = vi.hoisted(() =>
  createAppIpcMocks({
    SetTypedNotesSavedViews: vi.fn().mockResolvedValue(undefined)
  })
)
vi.mock('$silt-app', () => appMocks)
// store.svelte.ts imports Events at module load — stub the wails runtime so
// importing the real store doesn't touch IPC.
vi.mock('@wailsio/runtime', () => ({
  Events: {
    On: vi.fn(() => () => {})
  },
  Call: { ByID: vi.fn(), ByName: vi.fn() },
  CancellablePromise: class {
    then() {
      return this
    }
    catch() {
      return this
    }
    finally() {
      return this
    }
  },
  Create: {
    Nullable: <T>(fn: T) => fn,
    Array: () => [],
    Map: () => ({}),
    Any: {}
  }
}))

// Imports AFTER mocks.
import { settings } from '../settings/store.svelte'
import {
  loadTypedNotesSavedViews,
  persistTypedNotesSavedViews
} from './dashboardSettings'
import type { DashboardSavedView } from './dashboardSavedViews'

// The config path the module reads: ui.dashboards.typed_notes.saved_views[].
// The seed is intentionally partial (only the nested path the module walks),
// so cast through unknown to satisfy the full SystemConfig shape.
function seedConfig(savedViews: unknown[]): void {
  settings.config = {
    ui: { dashboards: { typed_notes: { saved_views: savedViews } } },
    hotkeys: {}
  } as unknown as typeof settings.config
}

function mirroredSavedViews(): unknown[] {
  const ui = settings.config?.ui as
    { dashboards?: { typed_notes?: { saved_views?: unknown[] } } } | undefined
  return ui?.dashboards?.typed_notes?.saved_views ?? []
}

describe('dashboardSettings — typed-notes saved-view IO (#863)', () => {
  let warnSpy: ReturnType<typeof vi.spyOn>

  beforeEach(() => {
    appMocks.SetTypedNotesSavedViews.mockReset()
    appMocks.SetTypedNotesSavedViews.mockResolvedValue(undefined)
    // loadTypedNotesSavedViews surfaces dropped entries via console.warn —
    // silence it so the test output isn't polluted by the malformed-seed case.
    warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {})
  })

  afterEach(() => {
    // Avoid cross-test leakage through the singleton settings store.
    settings.config = null
    warnSpy.mockRestore()
  })

  it('loadTypedNotesSavedViews coerces the raw slice and drops malformed entries', () => {
    seedConfig([
      { id: 'a', name: 'Alpha', typeId: 't1' },
      {
        id: 'b',
        name: 'Beta',
        typeId: 't1',
        viewMode: 'list',
        groupBy: '',
        sort: { property: 'title', desc: true }
      },
      { id: '', name: 'no-id', typeId: 't1' }, // missing id → dropped
      { junk: true } // missing required fields → dropped
    ])

    const views = loadTypedNotesSavedViews()

    expect(views.map((v) => v.id)).toEqual(['a', 'b'])
    // Coerced optional dims survive the round-trip.
    expect(views[1]).toMatchObject({
      id: 'b',
      name: 'Beta',
      typeId: 't1',
      viewMode: 'list',
      groupBy: '',
      sort: { property: 'title', desc: true }
    })
  })

  it('persistTypedNotesSavedViews strips system views, calls SetTypedNotesSavedViews, returns null, and mirrors the slice', async () => {
    seedConfig([])
    const userView: DashboardSavedView = {
      id: 'x',
      name: 'X',
      typeId: 't1'
    }
    const systemFlagged: DashboardSavedView = {
      id: 'y',
      name: 'Y',
      typeId: 't1',
      system: true
    }

    const result = await persistTypedNotesSavedViews([userView, systemFlagged])

    expect(result).toBeNull()
    // The system-flagged view is stripped from the persistable list.
    expect(appMocks.SetTypedNotesSavedViews).toHaveBeenCalledTimes(1)
    expect(appMocks.SetTypedNotesSavedViews).toHaveBeenCalledWith([
      { id: 'x', name: 'X', typeId: 't1' }
    ])
    // Optimistic mirror into the live snapshot.
    expect(mirroredSavedViews()).toEqual([{ id: 'x', name: 'X', typeId: 't1' }])
  })

  it('returns the error message and does NOT mirror when SetTypedNotesSavedViews rejects', async () => {
    // Pre-existing data that must survive a failed persist (the local snapshot
    // is the source of truth until the Go write succeeds).
    seedConfig([{ id: 'prior', name: 'Prior', typeId: 't1' }])
    appMocks.SetTypedNotesSavedViews.mockRejectedValue(
      new Error('vault not loaded')
    )

    const result = await persistTypedNotesSavedViews([
      { id: 'new', name: 'New', typeId: 't1' }
    ])

    expect(result).toBe('vault not loaded')
    // The prior snapshot is intact — the failed write did not mirror.
    expect(mirroredSavedViews()).toEqual([
      { id: 'prior', name: 'Prior', typeId: 't1' }
    ])
  })

  it('caps the persisted list to the NEWEST 256 entries (slice(-256))', async () => {
    seedConfig([])
    // 260 user views: index 0 is the oldest head, index 259 is the newest tail.
    const many: DashboardSavedView[] = Array.from({ length: 260 }, (_, i) => ({
      id: `v${i}`,
      name: `View ${i}`,
      typeId: 't1'
    }))

    await persistTypedNotesSavedViews(many)

    expect(appMocks.SetTypedNotesSavedViews).toHaveBeenCalledTimes(1)
    const calledWith = appMocks.SetTypedNotesSavedViews.mock
      .calls[0][0] as DashboardSavedView[]
    expect(calledWith).toHaveLength(256)
    // Newest (last in source) survives; oldest head (v0) is dropped.
    expect(calledWith[0]).toMatchObject({ id: 'v4' })
    expect(calledWith[calledWith.length - 1]).toMatchObject({ id: 'v259' })
    expect(calledWith.find((v) => v.id === 'v0')).toBeUndefined()
    // The mirror reflects the same capped slice.
    expect(mirroredSavedViews()).toHaveLength(256)
  })

  it('persists an empty list by calling SetTypedNotesSavedViews with []', async () => {
    seedConfig([{ id: 'leftover', name: 'Leftover', typeId: 't1' }])

    const result = await persistTypedNotesSavedViews([])

    expect(result).toBeNull()
    expect(appMocks.SetTypedNotesSavedViews).toHaveBeenCalledWith([])
    // Empty mirror replaces the prior snapshot.
    expect(mirroredSavedViews()).toEqual([])
  })

  it('returns "Settings not loaded" when settings.config is null', async () => {
    settings.config = null
    const result = await persistTypedNotesSavedViews([
      { id: 'x', name: 'X', typeId: 't1' }
    ])
    expect(result).toBe('Settings not loaded')
    // The setter must not have been reached.
    expect(appMocks.SetTypedNotesSavedViews).not.toHaveBeenCalled()
  })
})
