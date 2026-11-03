import { beforeEach, describe, expect, it, vi, type Mock } from 'vitest'
import {
  createTabPersistence,
  type PersistedTabsPayload,
  type TabPersistenceDeps
} from './persistence.svelte'
import type { TabEntry, ViewMode } from '../tabs'

// Canonical IPC-boundary mock (AGENTS.md): the Wails binding is stubbed so the
// sequence guard and the locator-key logic are exercised without real IPC.
const mocks = vi.hoisted(() => ({
  GetOpenTabs: vi.fn(),
  SetOpenTabs: vi.fn()
}))
vi.mock('$silt-app', () =>
  createAppIpcMocks({
    GetOpenTabs: mocks.GetOpenTabs,
    SetOpenTabs: mocks.SetOpenTabs
  })
)

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

  // #1005: an external ui.open_tabs edit must not rebuild the tab strip (or
  // flip a surface-unmounting view mode) while a conflict resolution is in
  // flight — the deferred diff applies when the busy set drains.
  describe('conflict-busy gating (#1005)', () => {
    it('defers the locator-set re-hydrate while a tab is conflict-busy, then applies it on drain', async () => {
      const busy = new Set<string>(['tab1'])
      const persistence = createTabPersistence({
        ...deps,
        getConflictBusyTabIds: () => busy
      })
      persistence.initBaseline([{ notebook: 'nb', section: '', page: 'page1' }])
      mocks.GetOpenTabs.mockResolvedValue({
        open_tabs: [{ notebook: 'nb', section: '', page: 'page2' }]
      })
      mocks.GetOpenTabs.mockClear()

      // External config edit lands mid-resolution: nothing applies.
      persistence.handleConfigChangedTabRehydrate({
        ui: { open_tabs: [{ notebook: 'nb', section: '', page: 'page2' }] }
      })
      expect(mocks.GetOpenTabs).not.toHaveBeenCalled()
      expect(deps.setTabs).not.toHaveBeenCalled()
      expect(deps.setTabViewMode).not.toHaveBeenCalled()

      // A self-write echo while still busy must NOT clobber the deferred
      // external diff (it is a no-op against the last-applied baseline).
      persistence.handleConfigChangedTabRehydrate({
        ui: { open_tabs: [{ notebook: 'nb', section: '', page: 'page1' }] }
      })
      expect(mocks.GetOpenTabs).not.toHaveBeenCalled()

      // The busy set drains → the deferred external diff applies.
      busy.clear()
      persistence.applyDeferredTabRehydrate()
      await new Promise((resolve) => setTimeout(resolve, 50))
      expect(deps.setTabs).toHaveBeenCalledTimes(1)
      const applied = deps.setTabs.mock.calls[0][0] as TabEntry[]
      expect(applied).toHaveLength(1)
      expect(applied[0].page).toBe('page2')
    })

    it('never flips the view mode of a conflict-busy tab, even after the drain re-run', () => {
      const busy = new Set<string>(['tab1'])
      const persistence = createTabPersistence({
        ...deps,
        getConflictBusyTabIds: () => busy
      })
      persistence.initBaseline([{ notebook: 'nb', section: '', page: 'page1' }])
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

      // View-mode-only external edit while the tab resolves: deferred whole.
      persistence.handleConfigChangedTabRehydrate({
        ui: {
          open_tabs: [
            { notebook: 'nb', section: '', page: 'page1', view_mode: 'source' }
          ]
        }
      })
      expect(deps.setTabViewMode).not.toHaveBeenCalled()

      // Drain while the tab is STILL busy: nothing applies.
      persistence.applyDeferredTabRehydrate()
      expect(deps.setTabViewMode).not.toHaveBeenCalled()

      // Fully drained: the deferred reconcile now flips the (settled) tab.
      busy.clear()
      persistence.applyDeferredTabRehydrate()
      expect(deps.setTabViewMode).toHaveBeenCalledWith('tab1', 'source')
    })

    // The deferred snapshot is stale by definition: anything the user did
    // inside the busy window is newer. A view-mode toggle is allowed on any
    // non-busy tab and persists immediately, so replaying the snapshot over
    // it would revert the toggle through the non-persisting reconcile dep
    // and leave memory and disk diverged (#1005).
    it('does not replay the deferred snapshot over a user view-mode toggle made inside the window', async () => {
      const busy = new Set<string>(['tabA'])
      const tabs: TabEntry[] = [
        {
          id: 'tabA',
          notebook: 'nb',
          section: '',
          page: 'p1',
          preview: false,
          lastActivatedAt: 1,
          viewMode: 'edit'
        },
        {
          id: 'tabB',
          notebook: 'nb',
          section: '',
          page: 'p2',
          preview: false,
          lastActivatedAt: 0,
          viewMode: 'edit'
        }
      ]
      const setTabViewMode = vi.fn((id: string, mode: ViewMode) => {
        const tab = tabs.find((t) => t.id === id)
        if (tab) tab.viewMode = mode
      })
      const persistence = createTabPersistence({
        ...deps,
        getTabs: vi.fn(() => tabs),
        setTabViewMode,
        getConflictBusyTabIds: () => busy
      })
      persistence.initBaseline([
        { notebook: 'nb', section: '', page: 'p1' },
        { notebook: 'nb', section: '', page: 'p2' }
      ])

      // External edit flips p1 to 'source' while tabA resolves → deferred.
      persistence.handleConfigChangedTabRehydrate({
        ui: {
          open_tabs: [
            { notebook: 'nb', section: '', page: 'p1', view_mode: 'source' },
            { notebook: 'nb', section: '', page: 'p2', view_mode: 'edit' }
          ]
        }
      })
      expect(setTabViewMode).not.toHaveBeenCalled()

      // The user toggles non-busy tabB to 'source' mid-window; the app
      // persists that choice via its own schedulePersistTabs.
      tabs[1].viewMode = 'source'

      // Drain: p1's external flip applies, tabB's in-window toggle does
      // not revert to the snapshot's stale 'edit'.
      busy.clear()
      persistence.applyDeferredTabRehydrate()
      expect(setTabViewMode).toHaveBeenCalledTimes(1)
      expect(setTabViewMode).toHaveBeenCalledWith('tabA', 'source')
      expect(tabs[1].viewMode).toBe('source')

      // The applied state persists: the user's mid-window persist rewrote
      // the whole tab list and could have clobbered the external
      // view_mode on disk, so the drain re-persists.
      mocks.SetOpenTabs.mockClear()
      persistence.flushPendingPersist()
      await new Promise((resolve) => setTimeout(resolve, 0))
      expect(mocks.SetOpenTabs).toHaveBeenCalledTimes(1)
      expect(mocks.SetOpenTabs.mock.calls[0][0]).toEqual([
        expect.objectContaining({ page: 'p1', view_mode: 'source' }),
        expect.objectContaining({ page: 'p2', view_mode: 'source' })
      ])
    })

    // prevOpenTabsKey must track the app's own persistTabs writes: without
    // the re-pin, the baseline goes stale after any in-app pinned-tab
    // change and the next equal-set config echo is misread as a locator
    // diff — deferred during busy and rebuilt at drain for nothing (fresh
    // tab ids, every editor surface remounts).
    it('an app-persisted tab set keeps the locator baseline fresh: an equal-set config echo neither defers nor rebuilds', async () => {
      const busy = new Set<string>(['tabA'])
      const tabs: TabEntry[] = [
        {
          id: 'tabA',
          notebook: 'nb',
          section: '',
          page: 'p1',
          preview: false,
          lastActivatedAt: 0,
          viewMode: 'edit'
        }
      ]
      const persistence = createTabPersistence({
        ...deps,
        getTabs: vi.fn(() => tabs),
        getConflictBusyTabIds: () => busy
      })
      // Boot baseline predates the app's own pinned-tab write.
      persistence.initBaseline([])

      // The app persists its current pinned set.
      await persistence.persistTabs()

      // An external config event whose locator set equals the now-persisted
      // in-memory tabs must classify as a no-op — nothing defers, so the
      // drain rebuilds nothing (GetOpenTabs is the rebuild fetch).
      persistence.handleConfigChangedTabRehydrate({
        ui: { open_tabs: [{ notebook: 'nb', section: '', page: 'p1' }] }
      })
      busy.clear()
      persistence.applyDeferredTabRehydrate()
      await new Promise((resolve) => setTimeout(resolve, 50))
      expect(mocks.GetOpenTabs).not.toHaveBeenCalled()
      expect(deps.setTabs).not.toHaveBeenCalled()
    })
  })
})
