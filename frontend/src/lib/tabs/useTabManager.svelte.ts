// Tab manager controller (#768, #142).
//
// Owns the multi-page editor's tab state machine wiring: the openTabs /
// activeTabId runes (single source of truth), the per-notebook displayedTabs
// derivation, the `.silt-stray` fail-loud guard, the open-tabs provider for
// agent UI location, and the open/select/close/promote/reorder/cycle/
// view-mode handlers. Wraps the pure state machine in lib/tabs.ts and
// composes createTabPersistence so the whole tab lifecycle is one surface.
//
// Extracted via the proven createX(deps) factory idiom: the $state runes move
// IN here and are exposed as getters (returning bare $state from a plain
// object would snapshot the initial value). App supplies the navigation
// triple getters/setters + the settings/config getter through the deps
// interface; everything else (persistence, recents, the stray guard) is
// owned here.
import { RecordRecentPage } from '../../../bindings/silt/app.js'
import {
  openPage as openPageState,
  closeTab as closeTabState,
  promotePreview as promotePreviewState,
  cycleTab as cycleTabState,
  reorderTab as reorderTabState,
  setTabViewMode as setTabViewModeState,
  mergeReorderedTabs,
  type TabEntry,
  type PageRef,
  type OpenPageMode,
  type ViewMode
} from '../tabs'
import {
  createTabPersistence,
  type TabRehydrateConfig
} from './persistence.svelte'
import { createRecentPageRecorder } from '../navigationTargets'
import {
  createRecentSaveTracker,
  type EditorSaveState
} from '../editor/recentSaveTracker'
import { isStandaloneTaskRef, routeJumpTarget } from '../standaloneTasksNav'
import {
  clearSelectionFocusIfPage,
  setOpenTabsProvider
} from '../../plugins/ui-location'

/** The settings subset the tab manager reads live. */
export interface TabManagerSettings {
  ui?: {
    enable_preview_tabs?: boolean
    max_open_tabs?: number
  }
  editor?: {
    default_view_mode?: string
  }
}

export interface TabManagerDeps {
  /** Live read of the active notebook (displayedTabs filters by it). */
  getActiveNotebook: () => string
  setActiveNotebook: (notebook: string) => void
  setActiveSection: (section: string) => void
  setActivePage: (page: string) => void
  /** Live read of the per-vault config (preview/pin + default view mode). */
  getSettings: () => TabManagerSettings
  /** Guard invoked before a context switch away from an unsaved templates
   *  draft. Returns true when the switch may proceed. */
  confirmTemplateTransition: () => boolean
  /** Route a `.silt` standalone-task locator to the Tasks view. */
  openTasksView: (blockId: string | undefined) => void
}

export interface PageRenamedDetail {
  notebook: string
  section: string
  oldName: string
  newName: string
}

export interface TabManagerController {
  // State (read by App markup + other controllers via getters).
  get openTabs(): TabEntry[]
  get activeTabId(): string
  get displayedTabs(): TabEntry[]
  // Handlers (bound to TabStrip props + navigation funnels).
  openPage: (
    ref: PageRef,
    mode: OpenPageMode,
    blockTarget?: { fileDate?: string; blockId?: string }
  ) => void
  handleSelectTab: (id: string) => void
  handleCloseTab: (id: string) => void
  handlePromoteTab: (id: string) => void
  handleReorderTab: (fromId: string, toId: string, before: boolean) => void
  handleCycleTab: (dir: 1 | -1) => void
  handleToggleViewMode: (tabId: string) => void
  selectNotebookContext: (notebook: string) => void
  syncActiveFromTab: () => void
  pageRenamed: (detail: PageRenamedDetail) => void
  /** Repoint a page's section across the open tabs (sidebar drag, #177). */
  pageMoved: (
    notebook: string,
    fromSection: string,
    toSection: string,
    page: string
  ) => void
  /** Rename a tab's page by id (editor-driven rename). */
  renameTab: (tabId: string, newName: string) => void
  /** Mirror an editor's save state onto its tab header (dirty/error/phase)
   *  and feed the recents MRU tracker. */
  setTabSaveState: (
    tabId: string,
    state: {
      phase: EditorSaveState['phase']
      dirty: boolean
      error: string | null
    }
  ) => void
  /** Clear all tabs + the active id (vault move/switch reset). */
  resetTabs: () => void
  /** Tracks a recent-save for the recents MRU; called by editor save state. */
  trackRecentSave: (tabId: string, state: EditorSaveState) => void
  /** Drop the recents recorder cache (vault close / switch). */
  invalidateRecentPages: () => void
  // Persistence surface (called from App onMount + config:changed handler).
  loadPersistedTabs: () => Promise<void>
  initBaseline: (
    tabs:
      | { notebook?: string; section?: string; page?: string }[]
      | null
      | undefined
  ) => void
  handleConfigChangedTabRehydrate: (cfg: TabRehydrateConfig | undefined) => void
  flushPendingPersist: () => void
}

/**
 * Build the tab manager. Called once at component init; the $state + $derived
 * + $effect live for the component's lifetime (no $effect.root needed).
 */
export function createTabManager(deps: TabManagerDeps): TabManagerController {
  let openTabs = $state<TabEntry[]>([])
  let activeTabId = $state<string>('')

  // Fail-loud guard (#374 hardening): the `.silt` synthetic notebook is hidden
  // by design and must never materialize as a tab. The routing guard in
  // openPage covers every funnel, but future call sites could forget. This
  // effect runs on every openTabs mutation; if a `.silt` entry appears it logs
  // a loud warning and drops it.
  $effect(() => {
    const stray = openTabs.find((t) => isStandaloneTaskRef(t.notebook))
    if (stray) {
      console.warn(
        '[silt] routing invariant violated: a .silt tab was added to openTabs. ',
        'Removing it. This should be impossible — please file a bug with the stack trace.',
        stray
      )
      openTabs = openTabs.filter((t) => t.id !== stray.id)
      if (activeTabId === stray.id) activeTabId = ''
      schedulePersistTabs()
    }
  })

  // Per-notebook tab scoping: the tab strip + editor surface show only tabs
  // for the active notebook. The full openTabs array (all notebooks) persists
  // to config.yaml so switching notebooks preserves each notebook's set.
  const displayedTabs = $derived(
    openTabs.filter((t) => t.notebook === deps.getActiveNotebook())
  )

  // Register the open-tabs provider for agent UI location (#680). The effect
  // re-runs on every openTabs/activeTabId mutation; cleared on unmount.
  $effect(() => {
    const tabs = openTabs
    const activeId = activeTabId
    setOpenTabsProvider(() =>
      tabs.map((t) => ({
        notebook: t.notebook,
        section: t.section,
        page: t.page,
        preview: t.preview,
        active: t.id === activeId
      }))
    )
    return () => setOpenTabsProvider(null)
  })

  const recentPageRecorder = createRecentPageRecorder(
    (ref) => RecordRecentPage(ref.notebook, ref.section, ref.page),
    () =>
      window.dispatchEvent(new CustomEvent('navigation-preferences-changed')),
    (error) => console.error('RecordRecentPage failed:', error)
  )

  function recordRecentActivation(ref: PageRef): void {
    recentPageRecorder.record(ref)
  }

  const trackRecentSave = createRecentSaveTracker((tabId) => {
    const tab = openTabs.find((candidate) => candidate.id === tabId)
    if (tab) recordRecentActivation(tab)
  })

  function syncActiveFromTab(): void {
    const tab = openTabs.find((t) => t.id === activeTabId)
    if (tab) {
      deps.setActiveNotebook(tab.notebook)
      deps.setActiveSection(tab.section)
      deps.setActivePage(tab.page)
    }
  }

  const tabPersistence = createTabPersistence({
    getTabs: () => openTabs,
    getActiveId: () => activeTabId,
    setTabs: (t) => (openTabs = t),
    setActiveId: (id) => (activeTabId = id),
    syncActiveFromTab,
    setTabViewMode: (id, mode) => {
      openTabs = setTabViewModeState(
        { tabs: openTabs, activeId: activeTabId },
        id,
        mode
      ).tabs
    }
  })
  const { schedulePersistTabs, loadPersistedTabs } = tabPersistence

  function openPage(
    ref: PageRef,
    mode: OpenPageMode,
    blockTarget: { fileDate?: string; blockId?: string } | undefined = undefined
  ): void {
    // Standalone-task routing (#374): a `.silt` locator delegates to the Tasks
    // view instead of opening a raw `.silt` tab.
    const target = routeJumpTarget({ ...ref, blockTarget })
    if (target.kind === 'tasks-view') {
      deps.openTasksView(target.blockTarget?.blockId)
      return
    }
    const cfg = deps.getSettings()
    const enablePreviewTabs = cfg.ui?.enable_preview_tabs !== false
    const maxOpenTabs = cfg.ui?.max_open_tabs ?? 8
    const defaultViewMode: ViewMode =
      cfg.editor?.default_view_mode === 'source' ? 'source' : 'edit'
    const result = openPageState(
      { tabs: openTabs, activeId: activeTabId },
      ref,
      mode,
      { enablePreviewTabs, maxOpenTabs, blockTarget, defaultViewMode }
    )
    openTabs = result.tabs
    activeTabId = result.activeId
    syncActiveFromTab()
    schedulePersistTabs()
    recordRecentActivation(ref)
  }

  function handleToggleViewMode(tabId: string): void {
    const tab = openTabs.find((t) => t.id === tabId)
    if (!tab) return
    const next: ViewMode = tab.viewMode === 'edit' ? 'source' : 'edit'
    openTabs = setTabViewModeState(
      { tabs: openTabs, activeId: activeTabId },
      tabId,
      next
    ).tabs
    schedulePersistTabs()
  }

  function selectNotebookContext(notebook: string): void {
    if (!deps.confirmTemplateTransition()) return
    deps.setActiveNotebook(notebook)
    const notebookTabs = openTabs
      .filter((tab) => tab.notebook === notebook)
      .sort((a, b) => b.lastActivatedAt - a.lastActivatedAt)
    activeTabId = notebookTabs[0]?.id ?? ''
    if (activeTabId) syncActiveFromTab()
    else {
      deps.setActiveSection('')
      deps.setActivePage('')
    }
  }

  function handleSelectTab(id: string): void {
    activeTabId = id
    const now = Date.now()
    openTabs = openTabs.map((t) =>
      t.id === id ? { ...t, lastActivatedAt: now } : t
    )
    syncActiveFromTab()
    schedulePersistTabs()
    const activated = openTabs.find((tab) => tab.id === id)
    if (activated) recordRecentActivation(activated)
  }

  function handleCloseTab(id: string): void {
    const closing = openTabs.find((t) => t.id === id)
    if (closing) {
      clearSelectionFocusIfPage(closing.notebook, closing.section, closing.page)
    }
    const result = closeTabState({ tabs: openTabs, activeId: activeTabId }, id)
    openTabs = result.tabs
    activeTabId = result.activeId
    syncActiveFromTab()
    schedulePersistTabs()
  }

  function handlePromoteTab(id: string): void {
    openTabs = promotePreviewState(
      { tabs: openTabs, activeId: activeTabId },
      id
    ).tabs
    schedulePersistTabs()
  }

  function handleReorderTab(
    fromId: string,
    toId: string,
    before: boolean
  ): void {
    // Dragging a preview tab pins it on drop. The promotion happens before the
    // reorder so the pinned tab is the one spliced into the new position.
    const draggedTab = openTabs.find((t) => t.id === fromId)
    if (draggedTab?.preview) {
      openTabs = promotePreviewState(
        { tabs: openTabs, activeId: activeTabId },
        fromId
      ).tabs
    }
    // Reorder within the displayed (per-notebook) tabs, then splice the
    // reordered subset back into the full openTabs array.
    const result = reorderTabState(
      { tabs: displayedTabs, activeId: activeTabId },
      fromId,
      toId,
      before
    )
    openTabs = mergeReorderedTabs(
      openTabs,
      result.tabs,
      deps.getActiveNotebook()
    )
    schedulePersistTabs()
  }

  function handleCycleTab(dir: 1 | -1): void {
    // Cycle within the displayed (per-notebook) tabs only — Ctrl+Tab must not
    // jump to a hidden tab in another notebook.
    const result = cycleTabState(
      { tabs: displayedTabs, activeId: activeTabId },
      dir
    )
    openTabs = openTabs.map((t) => {
      const updated = result.tabs.find((x) => x.id === t.id)
      return updated ?? t
    })
    activeTabId = result.activeId
    syncActiveFromTab()
    schedulePersistTabs()
  }

  function pageRenamed(detail: PageRenamedDetail): void {
    openTabs = openTabs.map((t) =>
      t.notebook === detail.notebook &&
      t.section === detail.section &&
      t.page === detail.oldName
        ? { ...t, page: detail.newName }
        : t
    )
  }

  function pageMoved(
    notebook: string,
    fromSection: string,
    toSection: string,
    page: string
  ): void {
    openTabs = openTabs.map((t) =>
      t.notebook === notebook && t.section === fromSection && t.page === page
        ? { ...t, section: toSection }
        : t
    )
    schedulePersistTabs()
  }

  function renameTab(tabId: string, newName: string): void {
    openTabs = openTabs.map((t) =>
      t.id === tabId ? { ...t, page: newName } : t
    )
  }

  function setTabSaveState(
    tabId: string,
    state: {
      phase: EditorSaveState['phase']
      dirty: boolean
      error: string | null
    }
  ): void {
    openTabs = openTabs.map((t) =>
      t.id === tabId
        ? {
            ...t,
            dirty: state.dirty,
            saveError: state.error,
            savePhase: state.phase
          }
        : t
    )
    trackRecentSave(tabId, state)
  }

  /** Clear all tabs + the active id (vault move/switch reset). */
  function resetTabs(): void {
    openTabs = []
    activeTabId = ''
  }

  return {
    get openTabs() {
      return openTabs
    },
    get activeTabId() {
      return activeTabId
    },
    get displayedTabs() {
      return displayedTabs
    },
    openPage,
    handleSelectTab,
    handleCloseTab,
    handlePromoteTab,
    handleReorderTab,
    handleCycleTab,
    handleToggleViewMode,
    selectNotebookContext,
    syncActiveFromTab,
    pageRenamed,
    pageMoved,
    renameTab,
    setTabSaveState,
    resetTabs,
    trackRecentSave,
    invalidateRecentPages: () => recentPageRecorder.invalidate(),
    loadPersistedTabs,
    initBaseline: tabPersistence.initBaseline,
    handleConfigChangedTabRehydrate:
      tabPersistence.handleConfigChangedTabRehydrate,
    flushPendingPersist: tabPersistence.flushPendingPersist
  }
}
