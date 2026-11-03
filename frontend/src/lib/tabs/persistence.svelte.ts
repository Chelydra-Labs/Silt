// Tab persistence / hydration for the multi-page editor surface (#142).
//
// Owns the debounced write of the pinned-tab set to config.yaml
// (SetOpenTabs) and the hydration of openTabs from config.yaml on vault
// open / reopen (GetOpenTabs). The reactive $state runes (openTabs,
// activeTabId) live in App.svelte; this factory reads/writes them through
// the deps interface so the module stays Svelte-agnostic and unit-testable.
//
// Two invariants live here and must survive any refactor:
//  1. The loadTabsSeq guard — only the most-recent loadPersistedTabs call's
//     result is applied, so overlapping calls (onMount + handleSelectFolder
//     firing in quick succession) can't race.
//  2. The locator-only tabSetKey — a view-mode change must NOT trigger a
//     full re-hydrate (that would rebuild tabs and remount editors whenever
//     the same set comes back — e.g. another Silt window's persist echo).
//     This window's own writes never fire config:changed: the backend's
//     config watcher suppresses self-writes, and the equal-set comparisons
//     below are defense-in-depth for suppression misses.

import { GetOpenTabs, SetOpenTabs } from '../../../bindings/silt/app.js'
import { generateTabId, type TabEntry, type ViewMode } from '../tabs'

/** A persisted open-tab entry as it appears in config.yaml's ui.open_tabs. */
export interface PersistedTabRef {
  notebook: string
  section: string
  page: string
  /** '' or absent means the Edit default; 'source' opts into Source view (#195). */
  view_mode?: string
}

/** GetOpenTabs() payload shape. */
export interface PersistedTabsPayload {
  open_tabs?: PersistedTabRef[] | null
  active_tab?: PersistedTabRef | null
}

/** The subset of SystemConfig the tab-persistence module reads. */
export interface TabRehydrateConfig {
  ui?: {
    open_tabs?: PersistedTabRef[] | null
  }
}

/**
 * The bridge between this Svelte-agnostic module and App.svelte's $state.
 * Each member is a live read/write into the reactive tab state.
 */
export interface TabPersistenceDeps {
  getTabs: () => TabEntry[]
  getActiveId: () => string
  setTabs: (tabs: TabEntry[]) => void
  setActiveId: (id: string) => void
  syncActiveFromTab: () => void
  setTabViewMode: (id: string, mode: ViewMode) => void
  /** Tab ids with a conflict resolution IPC in flight (#1005). Optional so
   *  bare test rigs need not stub it; absence means "nothing busy". */
  getConflictBusyTabIds?: () => ReadonlySet<string>
}

/**
 * Stable serialization of the persisted open_tabs list for change detection.
 * Locator-only by design: the config:changed handler compares previous and
 * next keys to decide whether to re-hydrate the tab strip on an external
 * config.yaml edit. A view-mode flip must not change the key (see module
 * docstring invariant 2).
 */
function tabSetKey(
  tabs:
    { notebook?: string; section?: string; page?: string }[] | null | undefined
): string {
  if (!tabs || tabs.length === 0) return ''
  return tabs
    .map((t) => `${t.notebook ?? ''}\x00${t.section ?? ''}\x00${t.page ?? ''}`)
    .sort()
    .join('|')
}

/** Locator identity of a tab — the key the deferred view-mode baseline and
 * the config reconcile both match tabs by (tab ids churn on rebuild). */
function tabLocatorKey(t: {
  notebook: string
  section: string
  page: string
}): string {
  return `${t.notebook}\x00${t.section}\x00${t.page}`
}

/**
 * Build the persistence/hydration helpers bound to App's reactive tab state.
 * Returns the public surface App consumes; the timers, sequence guard, and
 * hot-reload baseline stay module-private to this closure.
 */
export function createTabPersistence(deps: TabPersistenceDeps) {
  let persistTabsTimer: ReturnType<typeof setTimeout> | null = null
  // Snapshot of the persisted open_tabs list for config:changed change
  // detection. Kept here so loadPersistedTabs can update it alongside the
  // in-memory hydration (prevents a re-hydrate cycle), and re-pinned by
  // persistTabs after every successful SetOpenTabs so it tracks what is
  // actually on disk — a stale baseline misreads an equal-set config echo
  // as a locator diff and rebuilds the tab strip for nothing.
  let prevOpenTabsKey = ''

  // Monotonic request sequence for loadPersistedTabs. Only the most-recent
  // call's result is applied, so overlapping calls can't race — the later
  // call wins (see module docstring invariant 1).
  let loadTabsSeq = 0

  // Latest external ui.open_tabs config deferred while a tab is
  // conflict-busy (see handleConfigChangedTabRehydrate). Null = nothing
  // pending; a newer config:changed overwrites an older deferred one.
  let deferredRehydrateCfg: TabRehydrateConfig | undefined | null = null

  // In-memory state snapshotted when the current busy window's FIRST
  // deferral landed (#1005). At drain, anything that has moved off these
  // baselines is newer user action made mid-window (a view-mode toggle that
  // already persisted; an opened/closed/promoted tab) — replaying the stale
  // deferred snapshot over it would silently revert the user and, for view
  // modes, leave memory and disk diverged through the non-persisting
  // reconcile dep. Re-snapshotting on every overwrite would re-adopt
  // mid-window user actions as "baseline", so the snapshot is captured only
  // when there was nothing pending (a re-defer after an aborted drain apply
  // is a NEW window over fresh state and snapshots fresh). Plain records,
  // not SvelteMaps: never read in a reactive context, just compared at drain.
  let deferredBaselines: {
    viewModes: Record<string, ViewMode>
    locatorKey: string
  } | null = null

  // #1005 conflict-busy probes. Optional dep: absence (bare test rigs)
  // means "nothing busy".
  function busyTabCount(): number {
    return deps.getConflictBusyTabIds?.().size ?? 0
  }

  function tabIsBusy(tabId: string): boolean {
    return deps.getConflictBusyTabIds?.().has(tabId) ?? false
  }

  function schedulePersistTabs(): void {
    if (persistTabsTimer) clearTimeout(persistTabsTimer)
    persistTabsTimer = setTimeout(() => {
      persistTabsTimer = null
      void persistTabs()
    }, 250)
  }

  async function persistTabs(): Promise<void> {
    // Only persist PINNED page tabs + active (preview tabs are ephemeral).
    // Settings is a view, not a tab, so it's never in openTabs.
    const tabs = deps.getTabs()
    const activeId = deps.getActiveId()
    const pinned = tabs.filter((t) => !t.preview)
    const activeTab = tabs.find((t) => t.id === activeId)
    const activePersist = activeTab && !activeTab.preview ? activeTab : null
    try {
      await SetOpenTabs(
        pinned.map((t) => ({
          notebook: t.notebook,
          section: t.section,
          page: t.page,
          // Persist the per-tab view mode only when it's Source (#195);
          // absence on disk means the Edit default, keeping config.yaml lean.
          view_mode: t.viewMode === 'source' ? 'source' : ''
        })),
        activePersist
          ? {
              notebook: activePersist.notebook,
              section: activePersist.section,
              page: activePersist.page,
              view_mode: activePersist.viewMode === 'source' ? 'source' : ''
            }
          : null
      )
      // Disk now holds exactly `pinned` — pin the locator baseline to it so
      // the app's own write echo classifies as the no-op it is (see
      // prevOpenTabsKey).
      prevOpenTabsKey = tabSetKey(pinned)
    } catch (e) {
      console.error('SetOpenTabs failed:', e)
    }
  }

  // Load persisted tabs on vault open / reopen. Hydrates openTabs from the
  // pinned set + active stored in config.yaml.
  async function loadPersistedTabs(): Promise<void> {
    const seq = ++loadTabsSeq
    try {
      const result: PersistedTabsPayload = await GetOpenTabs()
      // Stale guard: a newer loadPersistedTabs call superseded this one.
      if (seq !== loadTabsSeq) return
      if (result?.open_tabs && result.open_tabs.length > 0) {
        // Re-gate after the async round trip (#1005): a conflict resolution
        // that started while the fetch was in flight must not have its
        // surface unmounted by this rebuild — the exact hazard the deferral
        // exists to prevent. Re-defer the freshly-read state (a NEW window:
        // fresh baselines over current config) and let the next drain
        // re-run the apply.
        if (busyTabCount() > 0) {
          deferredRehydrateCfg = { ui: { open_tabs: result.open_tabs } }
          deferredBaselines = snapshotDeferredBaselines()
          return
        }
        const now = Date.now()
        const tabs: TabEntry[] = result.open_tabs.map((t, i) => ({
          id: generateTabId(),
          notebook: t.notebook,
          section: t.section,
          page: t.page,
          preview: false, // persisted tabs are always pinned
          lastActivatedAt: now - i, // stable ordering for MRU
          // Restore the per-tab view mode (#195). Only "source" is persisted;
          // absence / any other value means the Edit default.
          viewMode: t.view_mode === 'source' ? 'source' : 'edit'
        }))
        deps.setTabs(tabs)
        // Restore active tab if it's in the set.
        if (result.active_tab) {
          const active = tabs.find(
            (t) =>
              t.notebook === result.active_tab!.notebook &&
              t.section === result.active_tab!.section &&
              t.page === result.active_tab!.page
          )
          if (active) {
            deps.setActiveId(active.id)
          }
        }
        // Fallback: if no active tab was persisted (or the persisted active
        // was pruned by the Go-side stale-tab check), activate the first
        // restored tab so the user sees a tab on launch instead of a blank
        // state.
        if (!deps.getActiveId() && tabs.length > 0) {
          deps.setActiveId(tabs[0].id)
        }
        deps.syncActiveFromTab()
        // Update the hot-reload baseline so this load doesn't immediately
        // trigger a re-hydrate cycle.
        prevOpenTabsKey = tabSetKey(
          result.open_tabs.map((t) => ({
            notebook: t.notebook,
            section: t.section,
            page: t.page
          }))
        )
      }
    } catch (e) {
      console.error('GetOpenTabs failed:', e)
    }
  }

  /**
   * Seed the hot-reload baseline from the settings store at boot, before the
   * first config:changed event arrives.
   */
  function initBaseline(
    tabs:
      | { notebook?: string; section?: string; page?: string }[]
      | null
      | undefined
  ): void {
    prevOpenTabsKey = tabSetKey(tabs)
  }

  /**
   * The tab-rehydration half of App's config:changed handler. Re-hydrates the
   * tab strip when an external ui.open_tabs edit changes the locator set, and
   * reconciles per-tab view_mode in place (no re-hydrate, no editor remount)
   * for external view-mode-only edits. This window's own persistTabs writes
   * never reach here as events — the backend's config watcher suppresses
   * self-writes — so the equal-set checks below are defense-in-depth against
   * suppression misses (a second Silt window, a slow watcher) and against
   * other clients' echoes.
   *
   * While any tab is conflict-busy (#1005) nothing applies: loadPersistedTabs
   * rebuilds openTabs with fresh ids (every surface unmounts, including one
   * mid-resolution) and a view_mode flip unmounts the same surface. The
   * latest config is deferred and applied by applyDeferredTabRehydrate when
   * the busy set drains.
   */
  function handleConfigChangedTabRehydrate(
    cfg: TabRehydrateConfig | undefined
  ): void {
    if (busyTabCount() > 0) {
      // Defer only configs that would actually change something: an own-echo
      // that slipped past the backend suppression would classify as a no-op
      // against the last-persisted baseline (persistTabs re-pins it) and
      // must not overwrite a deferred external diff.
      if (configWouldApplyChanges(cfg)) {
        if (deferredRehydrateCfg === null) {
          deferredBaselines = snapshotDeferredBaselines()
        }
        deferredRehydrateCfg = cfg
      }
      return
    }
    applyTabRehydrate(cfg)
  }

  // Snapshot the in-memory tab state a deferral is replayed against (see
  // deferredBaselines).
  function snapshotDeferredBaselines(): {
    viewModes: Record<string, ViewMode>
    locatorKey: string
  } {
    const tabs = deps.getTabs()
    return {
      viewModes: Object.fromEntries(
        tabs.map((t) => [tabLocatorKey(t), t.viewMode])
      ),
      locatorKey: tabSetKey(tabs)
    }
  }

  // Would this config change anything relative to the last-applied state?
  // Locator-set diff, or an in-place view_mode reconcile.
  function configWouldApplyChanges(
    cfg: TabRehydrateConfig | undefined
  ): boolean {
    if (tabSetKey(cfg?.ui?.open_tabs) !== prevOpenTabsKey) return true
    const tabs = deps.getTabs()
    for (const ref of cfg?.ui?.open_tabs ?? []) {
      const tab = tabs.find(
        (t) =>
          t.notebook === ref.notebook &&
          t.section === (ref.section ?? '') &&
          t.page === ref.page
      )
      if (!tab) continue
      if (tab.viewMode !== (ref.view_mode === 'source' ? 'source' : 'edit')) {
        return true
      }
    }
    return false
  }

  /**
   * Apply a rehydrate deferred by handleConfigChangedTabRehydrate, if any,
   * once no tab is conflict-busy anymore. Called from App's effect on the
   * busy set draining.
   */
  function applyDeferredTabRehydrate(): void {
    if (deferredRehydrateCfg === null) return
    if (busyTabCount() > 0) return
    const cfg = deferredRehydrateCfg
    const baselines = deferredBaselines
    deferredRehydrateCfg = null
    deferredBaselines = null
    if (applyTabRehydrate(cfg, baselines ?? undefined)) {
      // The snapshot predates the busy window: the user's own mid-window
      // persist rewrites the whole tab list and may have clobbered the
      // external view_mode on disk. Re-persist so the applied state lands.
      // (A rebuild leg that later aborts on a fresh busy set re-defers and
      // re-runs; its freshly-read state makes this write redundant at
      // worst, never divergent.)
      schedulePersistTabs()
    }
  }

  function applyTabRehydrate(
    cfg: TabRehydrateConfig | undefined,
    deferred?: { viewModes: Record<string, ViewMode>; locatorKey: string }
  ): boolean {
    let viewChanged = false
    if (deferred && tabSetKey(deps.getTabs()) !== deferred.locatorKey) {
      // Deferred replay only (#1005): the user opened/closed/promoted a tab
      // inside the busy window — memory is newer than the snapshot, and a
      // rebuild from the deferred decision would revert it. The user's own
      // persist wins the disk last-writer-wins; the next external edit
      // lands through a fresh config:changed.
    } else if (tabSetKey(cfg?.ui?.open_tabs) !== prevOpenTabsKey) {
      // loadPersistedTabs re-reads CURRENT config and owns the
      // prevOpenTabsKey update on completion — the deferred cfg only
      // decides WHETHER a rebuild is wanted, never what lands. Writing the
      // key eagerly from a stale cfg would swallow the next legitimate
      // same-key external edit as a no-op.
      void loadPersistedTabs()
    }
    const externalTabs = cfg?.ui?.open_tabs ?? []
    if (externalTabs.length > 0) {
      const tabs = deps.getTabs()
      for (const ref of externalTabs) {
        const tab = tabs.find(
          (t) =>
            t.notebook === ref.notebook &&
            t.section === (ref.section ?? '') &&
            t.page === ref.page
        )
        if (!tab) continue
        if (tabIsBusy(tab.id)) {
          // The surface-unmounting toggle waits for the resolution to settle;
          // the deferred re-run (or the next config:changed) reconciles it.
          continue
        }
        if (deferred) {
          // Deferred replay only (#1005): a mode that moved since the
          // snapshot was taken — or a tab that did not exist at snapshot
          // time — is user-owned mid-window state; the snapshot's value
          // for it is stale and must not replay.
          const atDefer = deferred.viewModes[tabLocatorKey(tab)]
          if (atDefer === undefined || atDefer !== tab.viewMode) continue
        }
        const mode: ViewMode = ref.view_mode === 'source' ? 'source' : 'edit'
        if (tab.viewMode !== mode) {
          deps.setTabViewMode(tab.id, mode)
          viewChanged = true
          // No schedulePersistTabs here — the immediate path's cfg is
          // already on disk, and the deferred path persists at the drain.
        }
      }
    }
    return viewChanged
  }

  /**
   * Drop any deferred rehydrate (#1005): its config belongs to the vault
   * that just closed — replaying it against the new vault's tabs would leak
   * view modes to same-named pages. Called from the vault-switch handler.
   */
  function resetDeferredRehydrate(): void {
    deferredRehydrateCfg = null
    deferredBaselines = null
  }

  /**
   * Flush any pending tab-state persistence so the user's last tab change
   * survives a component unmount / app close. Called from App's onMount
   * cleanup.
   */
  function flushPendingPersist(): void {
    if (persistTabsTimer) {
      clearTimeout(persistTabsTimer)
      persistTabsTimer = null
      void persistTabs()
    }
  }

  return {
    schedulePersistTabs,
    persistTabs,
    loadPersistedTabs,
    initBaseline,
    handleConfigChangedTabRehydrate,
    applyDeferredTabRehydrate,
    resetDeferredRehydrate,
    flushPendingPersist
  }
}
