import { beforeEach, describe, expect, it, vi, type Mock } from 'vitest'
import {
  createTabPersistence,
  type PersistedTabsPayload,
  type TabPersistenceDeps
} from './persistence.svelte'
import type { TabEntry } from '../tabs'

// Canonical IPC-boundary mock (AGENTS.md): the Wails binding is stubbed so the
// sequence guard and the locator-key logic are exercised without real IPC.
const mocks = vi.hoisted(() => ({
  GetOpenTabs: vi.fn(),
  SetOpenTabs: vi.fn()
}))
vi.mock('../../../bindings/silt/app.js', () => ({
  GetOpenTabs: mocks.GetOpenTabs,
  SetOpenTabs: mocks.SetOpenTabs
}))

function makeDeps(): TabPersistenceDeps & {
  setTabs: Mock
  getTabs: Mock
  setTabViewMode: Mock
} {
  const tabs: TabEntry[] = []
  return {
    getTabs: vi.fn(() => tabs),
    getActiveId: vi.fn(() => tabs[0]?.id ?? ''),
    setTabs: vi.fn((next: TabEntry[]) => {
      tabs.splice(0, tabs.length, ...next)
    }),
    setActiveId: vi.fn(),
    syncActiveFromTab: vi.fn(),
    setTabViewMode: vi.fn()
  }
}

// A manually-resolved promise so a test can order overlapping GetOpenTabs
// resolutions and prove the sequence guard discards the stale one.
function deferred<T>(): { promise: Promise<T>; resolve: (v: T) => void } {
  let resolve!: (v: T) => void
  const promise = new Promise<T>((r) => {
    resolve = r
  })
  return { promise, resolve }
}

// Locks the two load-bearing invariants of the tab-persistence extraction
// (see module docstring). These are the subtle ones that transitive App-level
// coverage can't pin precisely: a stale async load and a view-mode-only flip.
describe('tab persistence invariants', () => {
  let deps: ReturnType<typeof makeDeps>
  beforeEach(() => {
    vi.clearAllMocks()
    deps = makeDeps()
  })

  it('discards a superseded loadPersistedTabs result (loadTabsSeq guard)', async () => {
    const persistence = createTabPersistence(deps)
    const payloadA: PersistedTabsPayload = {
      open_tabs: [{ notebook: 'nb', section: '', page: 'A' }]
    }
    const payloadB: PersistedTabsPayload = {
      open_tabs: [{ notebook: 'nb', section: '', page: 'B' }]
    }
    const a = deferred<PersistedTabsPayload>()
    const b = deferred<PersistedTabsPayload>()
    let n = 0
    // First load() resolves to A (late); the second load() resolves to B.
    mocks.GetOpenTabs.mockImplementation(() =>
      n++ === 0 ? a.promise : b.promise
    )

    const pA = persistence.loadPersistedTabs() // seq 1
    const pB = persistence.loadPersistedTabs() // seq 2 — supersedes A
    a.resolve(payloadA) // resolves late: seq 1 !== 2 → must be discarded
    b.resolve(payloadB) // seq 2 === 2 → applied
    await Promise.all([pA, pB])

    // Only B's result reached setTabs; A's stale payload never overwrote it.
    expect(deps.setTabs).toHaveBeenCalledTimes(1)
    const applied = deps.setTabs.mock.calls[0][0] as TabEntry[]
    expect(applied).toHaveLength(1)
    expect(applied[0].page).toBe('B')
  })

  it('a view-mode flip does not re-hydrate (locator-only tabSetKey) but reconciles the mode', () => {
    const persistence = createTabPersistence(deps)
    // Baseline locator set: one tab at nb//page1.
    persistence.initBaseline([{ notebook: 'nb', section: '', page: 'page1' }])
    // In-memory tab: same locator, currently in 'edit' view.
    deps.getTabs.mockReturnValue([
      {
        id: 'tab1',
        notebook: 'nb',
        section: '',
        page: 'page1',
        preview: false,
        lastActivatedAt: 0,
        viewMode: 'edit'
      } as TabEntry
    ])
    mocks.GetOpenTabs.mockClear()

    // External config edit: SAME locator, view_mode flipped to 'source'.
    persistence.handleConfigChangedTabRehydrate({
      ui: {
        open_tabs: [
          { notebook: 'nb', section: '', page: 'page1', view_mode: 'source' }
        ]
      }
    })

    // Locator set unchanged → no re-hydrate (GetOpenTabs is the re-hydrate fetch).
    expect(mocks.GetOpenTabs).not.toHaveBeenCalled()
    // The view-mode flip is reconciled in place — no tab rebuild / editor remount.
    expect(deps.setTabViewMode).toHaveBeenCalledWith('tab1', 'source')
  })
})
