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
//     full re-hydrate (that would rebuild tabs and remount editors on every
//     in-app toggle, since the frontend's own persistTabs write also fires
//     config:changed).

import { GetOpenTabs, SetOpenTabs } from '../../../bindings/silt/app.js'
import {
  generateTabId,
  setTabViewMode as setTabViewModeState,
  type TabEntry,
  type ViewMode
} from '../tabs'

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

/**
 * Build the persistence/hydration helpers bound to App's reactive tab state.
 * Returns the public surface App consumes; the timers, sequence guard, and
 * hot-reload baseline stay module-private to this closure.
 */
export function createTabPersistence(deps: TabPersistenceDeps) {
  let persistTabsTimer: ReturnType<typeof setTimeout> | null = null
  // Snapshot of the persisted open_tabs list for config:changed change
  // detection. Kept here so loadPersistedTabs can update it alongside the
  // in-memory hydration (prevents a re-hydrate cycle).
  let prevOpenTabsKey = ''

  // Monotonic request sequence for loadPersistedTabs. Only the most-recent
  // call's result is applied, so overlapping calls can't race — the later
  // call wins (see module docstring invariant 1).
  let loadTabsSeq = 0

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
   * for external view-mode-only edits. The frontend's own persistTabs writes
   * match the in-memory state, so they produce no diff here.
   */
  function handleConfigChangedTabRehydrate(
    cfg: TabRehydrateConfig | undefined
  ): void {
    const nextTabsKey = tabSetKey(cfg?.ui?.open_tabs)
    if (nextTabsKey !== prevOpenTabsKey) {
      prevOpenTabsKey = nextTabsKey
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
        const mode: ViewMode = ref.view_mode === 'source' ? 'source' : 'edit'
        if (tab.viewMode !== mode) {
          deps.setTabViewMode(tab.id, mode)
          // Do NOT schedulePersistTabs — this change is already on disk.
        }
      }
    }
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
    flushPendingPersist
  }
}
