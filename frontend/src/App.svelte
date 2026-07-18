<script lang="ts">
  import { onMount, tick } from 'svelte'
  import {
    IsVaultInitialized,
    InitializeVault,
    CloseVault,
    GetSidebarWidth,
    SetSidebarWidth,
    GetOpenTabs,
    SetOpenTabs,
    ConfirmSettingsChange,
    ConfirmGrantsMigration,
    DeclineGrantsMigration,
    ResolveQuarantinedLinks,
    PickLinkedNotebook,
    UnlinkNotebook,
    CreateStandaloneTask,
    MarkFrontendReady,
    GetStartupEvents,
    RecordRecentPage
  } from '../bindings/silt/app.js'
  import { Events } from '@wailsio/runtime'
  import type * as config from '../bindings/silt/backend/config/models.js'
  import { fade } from 'svelte/transition'
  import TitleBar from './components/TitleBar.svelte'
  import Sidebar from './components/Sidebar.svelte'
  import { themeEditorSession } from './theme/editor/session.svelte'
  import VirtualScrollContainer from './components/VirtualScrollContainer.svelte'
  import TabStrip from './components/TabStrip.svelte'
  import SearchModal from './components/SearchModal.svelte'
  import QuickSwitcher from './components/QuickSwitcher.svelte'
  import PageBreadcrumb from './components/PageBreadcrumb.svelte'
  import ShortcutHelp from './components/ShortcutHelp.svelte'
  import GlobalReplaceModal from './components/editor/GlobalReplaceModal.svelte'
  import TagsExplorer from './components/TagsExplorer.svelte'
  import PluginView from './components/PluginView.svelte'
  import SettingsPanel from './components/settings/SettingsPanel.svelte'
  import {
    getSettingsSections,
    resolveSettingsSectionId
  } from './components/settings/settingsSections.svelte'
  import QuickAddTask from './plugins/first-party/silt-tasks/components/QuickAddTask.svelte'
  import { loadPlugins } from './plugins/loader'
  import { refreshGrants } from './plugins/grants.svelte'
  import { revokeRevokedContributions } from './plugins/reconcile'
  import {
    initConfigHotReload,
    loadConfig,
    settings,
    type SystemConfig,
    toggleFormatToolbar,
    toggleFocusMode,
    toggleTypewriterMode
  } from './settings/store.svelte'
  import { initEditorTokens } from './settings/editor-tokens.svelte'
  import { initThemes } from './theme/store.svelte'
  import { initTemplates } from './templates/store.svelte'
  import TemplatePicker from './templates/TemplatePicker.svelte'
  import { resolveGlobalHotkey } from './shell/globalHotkeys'
  import { effectiveHotkeys } from './settings/shortcutActions'
  import { findBarState } from './lib/editor/search/findBarState.svelte'
  import {
    clearAllEditors,
    editorKey,
    getEditor
  } from './lib/editor/editorRegistry.svelte'
  import SidebarResizeHandle from './components/SidebarResizeHandle.svelte'
  import PluginModalHost from './components/PluginModalHost.svelte'
  import { getAIAvailability } from './plugins/shared/ai-chat/availability'
  import AIChatDrawer from './plugins/shared/ai-chat/AIChatDrawer.svelte'
  import {
    aiChatDrawer,
    toggleAIChatDrawer
  } from './plugins/shared/ai-chat/drawer.svelte'
  import PluginStatusBar from './components/PluginStatusBar.svelte'
  import { setActiveLocation } from './plugins/location.svelte'
  import {
    clearSelectionFocus,
    clearSelectionFocusIfPage,
    setOpenTabsProvider
  } from './plugins/ui-location'
  import ToastContainer from './components/ToastContainer.svelte'
  import Onboarding from './components/Onboarding.svelte'
  import { pushNotification } from './notifications/store.svelte'
  import { reMintToast, type ReMintWarning } from './lib/reMintToast'
  import {
    initStartupUpdateCheck,
    disposeUpdateStore
  } from './updates/store.svelte'
  import {
    openPage as openPageState,
    closeTab as closeTabState,
    promotePreview as promotePreviewState,
    cycleTab as cycleTabState,
    reorderTab as reorderTabState,
    setTabViewMode as setTabViewModeState,
    mergeReorderedTabs,
    generateTabId,
    type TabEntry,
    type PageRef,
    type OpenPageMode,
    type ViewMode
  } from './lib/tabs'
  import { nextView } from './lib/viewCycle'
  import {
    flattenNavigation,
    notebookNavigationMetadata,
    type NotebookNavigationMetadata,
    type NavigationCatalogItem
  } from './lib/navigationCatalog'
  import type { NavigationPreferences } from './lib/sidebar/types'
  import {
    isStandaloneTaskRef,
    routeJumpTarget
  } from './lib/standaloneTasksNav'

  let isInitialized = $state(false)
  let loading = $state(true)

  // Tab state (#142). The tab list + active id are the source of truth for
  // the multi-page editor surface. The legacy active-notebook/section/page
  // triple (still read by Sidebar, plugins, breadcrumbs) is kept in sync
  // from the active tab by the tabSync effect below. The Sidebar's
  // onSelectPage callback funnels through openPage(); onSelectNotebook/
  // onSelectSection set the triple directly (sidebar context without a tab
  // change).
  let openTabs = $state<TabEntry[]>([])
  let activeTabId = $state<string>('')

  // Fail-loud guard (#374 hardening): the `.silt` synthetic notebook is
  // hidden by design — under no circumstance should it ever materialize
  // as a tab in `openTabs`. The routing guard in `openPage` /
  // `handleSearchJump` / `handleNavigateToBlock` covers every funnel in
  // this file today, but future call sites could forget. This effect
  // runs every time `openTabs` mutates; if any entry's notebook is
  // `.silt` we log a loud warning (so the regression is visible in the
  // console) and drop the entry from the in-memory state. We
  // intentionally do not auto-remove silently — the warning is the
  // point — but the removal prevents a leaked `.silt` tab from
  // surviving into the next renderer pass.
  $effect(() => {
    const stray = openTabs.find((t) => isStandaloneTaskRef(t.notebook))
    if (stray) {
      console.warn(
        '[silt] routing invariant violated: a .silt tab was added to openTabs. ',
        'Removing it. This should be impossible — please file a bug with the stack trace.',
        stray
      )
      openTabs = openTabs.filter((t) => t.id !== stray.id)
      // If that stray tab happened to be active, clear the active id
      // too — `syncActiveFromTab` will pick a sane replacement off the
      // MRU head.
      if (activeTabId === stray.id) activeTabId = ''
      // Persist the cleanup so config.yaml's open_tabs stops re-emitting
      // the stray entry on every launch. Without this, an upgrade-time
      // user would see a console.warn loop on every restart until they
      // manually edit their config.
      schedulePersistTabs()
    }
  })

  // Per-notebook tab scoping: the tab strip and editor surface only show
  // tabs for the active notebook. The full openTabs array (all notebooks)
  // persists to config.yaml so switching notebooks preserves each
  // notebook's tab set. (#142 — user request: tabs should not carry over
  // when switching notebooks.)
  let displayedTabs = $derived(
    openTabs.filter((t) => t.notebook === activeNotebook)
  )

  // Navigation state (3-level: notebook > section > page). Kept in sync with
  // the active tab; also set directly by onSelectNotebook/onSelectSection
  // (sidebar browsing context without opening a page).
  let activeNotebook = $state('')
  let activeSection = $state('')
  let activePage = $state('')
  let activeView = $state('notes')
  const views = [
    { id: 'notes', label: 'Notes', icon: 'description' },
    { id: 'tags', label: 'Tags', icon: 'label' },
    { id: 'tasks', label: 'Tasks', icon: 'checklist' }
  ]
  let selectedTag = $state('')

  // Shell state
  let sidebarCollapsed = $state(false)
  let sidebarWidth = $state(256)
  let manuallyCollapsed = $state(false)
  let sidebarDragging = $state(false)

  // Sync active navigation to the reactive plugin location (#69). Plugins
  // read ctx.activeNotebook/Section/Page via live getters backed by this state.
  $effect(() => {
    setActiveLocation(activeNotebook, activeSection, activePage)
  })

  // Register open-tabs provider for agent UI location (#680). Cleared on unmount.
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

  // --- Tab management (#142) -----------------------------------------------

  // The per-vault default view mode a freshly-opened tab starts in (#195).
  // Read live from the settings store so a config.yaml edit takes effect on
  // the next open without a rebind.
  let defaultViewMode = $derived(
    settings.config?.editor?.default_view_mode === 'source'
      ? ('source' as const)
      : ('edit' as const)
  )

  // The single entry point for opening a page. All "open a page" callers
  // (sidebar click, search jump, navigate-to-block, refs) funnel through
  // here so the preview/pin logic lives in one place. Wraps the pure state
  // machine from tabs.ts and applies the result to the $state runes.
  function openPage(
    ref: PageRef,
    mode: OpenPageMode,
    blockTarget: { fileDate?: string; blockId?: string } | undefined = undefined
  ): void {
    // Standalone-task navigation router (#374). A `.silt` page locator
    // is a synthetic notebook whose only purpose is the standalone-tasks
    // file (#368). Routing through the editor's open-page funnel would
    // open a raw `.silt / tasks` tab and leak the synthetic notebook name
    // into the tab header. Delegate to openTasksView instead.
    const target = routeJumpTarget({ ...ref, blockTarget })
    if (target.kind === 'tasks-view') {
      openTasksView(target.blockTarget?.blockId)
      return
    }
    const enablePreviewTabs = settings.config?.ui?.enable_preview_tabs !== false
    const maxOpenTabs = settings.config?.ui?.max_open_tabs ?? 8
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

  function recordRecentActivation(ref: PageRef): void {
    void RecordRecentPage(ref.notebook, ref.section, ref.page)
      .then(() =>
        window.dispatchEvent(new CustomEvent('navigation-preferences-changed'))
      )
      .catch((e) => console.error('RecordRecentPage failed:', e))
  }

  // Toggle a tab between Edit and Source view (#195). The mode lives on
  // TabEntry (single source of truth) and persists to config.yaml on the next
  // debounced flush. No window-event indirection — App owns the state.
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

  // Sync activeNotebook/Section/Page from the active tab so every downstream
  // consumer (Sidebar, plugins, breadcrumbs) keeps working unchanged.
  function syncActiveFromTab(): void {
    const tab = openTabs.find((t) => t.id === activeTabId)
    if (tab) {
      activeNotebook = tab.notebook
      activeSection = tab.section
      activePage = tab.page
    }
  }

  function selectNotebookContext(notebook: string): void {
    activeNotebook = notebook
    const notebookTabs = openTabs
      .filter((tab) => tab.notebook === notebook)
      .sort((a, b) => b.lastActivatedAt - a.lastActivatedAt)
    activeTabId = notebookTabs[0]?.id ?? ''
    if (activeTabId) syncActiveFromTab()
    else {
      activeSection = ''
      activePage = ''
    }
  }

  function openSectionContext(section: string): void {
    const destination = navigationCatalog.find(
      (item) =>
        item.notebook === activeNotebook &&
        !item.disconnected &&
        (item.section === section || item.section.startsWith(`${section}/`))
    )
    if (destination) openPage(destination, 'preview')
    else {
      activeSection = section
      activePage = ''
    }
  }

  function openFromQuickSwitcher(
    item: NavigationCatalogItem,
    mode: OpenPageMode
  ): void {
    openPage(item, mode)
    activeView = 'notes'
  }

  async function requestNavigationCreation(
    kind: 'page' | 'section' | 'notebook'
  ): Promise<void> {
    if (kind !== 'notebook' && !activeNotebook) {
      pushNotification({
        kind: 'error',
        message: `Open a notebook before creating a ${kind}.`
      })
      return
    }
    if (kind !== 'notebook' && activeNotebookMetadata?.disconnected) {
      pushNotification({
        kind: 'error',
        message:
          'This linked notebook is offline. Reconnect it before creating anything.'
      })
      return
    }
    activeView = 'notes'
    sidebarCollapsed = false
    manuallyCollapsed = false
    await tick()
    if (kind === 'page') {
      window.dispatchEvent(
        new CustomEvent('create-page-inline', {
          detail: { sectionName: activeSection ?? '' }
        })
      )
    } else {
      window.dispatchEvent(
        new CustomEvent('open-navigation-create', { detail: { kind } })
      )
    }
  }

  function handleSelectTab(id: string): void {
    activeTabId = id
    // Bump MRU ordering.
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
    // industry-standard parity (#175 AC4): dragging a preview tab pins it on drop.
    // The promotion happens before the reorder so the pinned tab is the
    // one that gets spliced into the new position.
    const draggedTab = openTabs.find((t) => t.id === fromId)
    if (draggedTab?.preview) {
      openTabs = promotePreviewState(
        { tabs: openTabs, activeId: activeTabId },
        fromId
      ).tabs
    }
    // Reorder within the displayed (per-notebook) tabs, then splice the
    // reordered subset back into the full openTabs array — non-displayed
    // (other-notebook) tabs keep their relative positions.
    const result = reorderTabState(
      { tabs: displayedTabs, activeId: activeTabId },
      fromId,
      toId,
      before
    )
    openTabs = mergeReorderedTabs(openTabs, result.tabs, activeNotebook)
    schedulePersistTabs()
  }

  function handleCycleTab(dir: 1 | -1): void {
    // Cycle within the displayed (per-notebook) tabs only — Ctrl+Tab must
    // not jump to a hidden tab in another notebook (#142 review: cycling
    // across openTabs violated per-notebook scoping).
    const result = cycleTabState(
      { tabs: displayedTabs, activeId: activeTabId },
      dir
    )
    // Merge the MRU-bumped tabs (from the cycled subset) back into the
    // full openTabs array. cycleTabState → activateTab bumps
    // lastActivatedAt on the newly-active tab; without this merge,
    // repeated Ctrl+Tab presses would use stale timestamps and the
    // cycling order would degrade (#142 review: discarded MRU bump).
    openTabs = openTabs.map((t) => {
      const updated = result.tabs.find((x) => x.id === t.id)
      return updated ?? t
    })
    activeTabId = result.activeId
    syncActiveFromTab()
    schedulePersistTabs()
  }

  // --- Tab persistence (debounced 250ms, pinned-only) ----------------------

  let persistTabsTimer: ReturnType<typeof setTimeout> | null = null
  // Snapshot of the persisted open_tabs list for config:changed change
  // detection. Declared at component scope so loadPersistedTabs can update
  // it alongside the in-memory hydration (prevents a re-hydrate cycle).
  let prevOpenTabsKey = ''

  function schedulePersistTabs(): void {
    if (persistTabsTimer) clearTimeout(persistTabsTimer)
    persistTabsTimer = setTimeout(() => {
      persistTabsTimer = null
      void persistTabs()
    }, 250)
  }

  async function persistTabs(): Promise<void> {
    // Only persist PINNED page tabs + active (preview tabs are ephemeral —
    // parity). Settings is a view, not a tab, so it's never in openTabs.
    const pinned = openTabs.filter((t) => !t.preview)
    const activeTab = openTabs.find((t) => t.id === activeTabId)
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
        (activePersist
          ? {
              notebook: activePersist.notebook,
              section: activePersist.section,
              page: activePersist.page,
              view_mode: activePersist.viewMode === 'source' ? 'source' : ''
            }
          : null) as unknown as config.TabRef
      )
    } catch (e) {
      console.error('SetOpenTabs failed:', e)
    }
  }

  // Monotonic request sequence for loadPersistedTabs. Only the most-recent
  // call's result is applied, so overlapping calls (onMount + handleSelectFolder
  // firing in quick succession) don't race — the later call wins (#142 hardening).
  let loadTabsSeq = 0

  // Load persisted tabs on vault open / reopen. Hydrates openTabs from the
  // pinned set + active stored in config.yaml.
  async function loadPersistedTabs(): Promise<void> {
    const seq = ++loadTabsSeq
    try {
      const result = await GetOpenTabs()
      // Stale guard: a newer loadPersistedTabs call superseded this one.
      if (seq !== loadTabsSeq) return
      if (result?.open_tabs && result.open_tabs.length > 0) {
        const now = Date.now()
        openTabs = result.open_tabs.map((t, i) => ({
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
        // Restore active tab if it's in the set.
        if (result.active_tab) {
          const active = openTabs.find(
            (t) =>
              t.notebook === result.active_tab!.notebook &&
              t.section === result.active_tab!.section &&
              t.page === result.active_tab!.page
          )
          if (active) {
            activeTabId = active.id
          }
        }
        // Fallback: if no active tab was persisted (or the persisted active
        // was pruned by the Go-side stale-tab check), activate the first
        // restored tab so the user sees a tab on launch instead of a blank
        // state. (#142 review: nil active_tab left displayedTabs empty.)
        if (!activeTabId && openTabs.length > 0) {
          activeTabId = openTabs[0].id
        }
        syncActiveFromTab()
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
  let showSearch = $state(false)
  let showQuickSwitcher = $state(false)
  let showShortcutHelp = $state(false)
  let navigationCatalog = $state<NavigationCatalogItem[]>([])
  let navigationNotebookMetadata = $state<
    Record<string, NotebookNavigationMetadata>
  >({})
  let navigationPreferences = $state<NavigationPreferences>({
    expanded_sections: [],
    recent_pages: [],
    favorites: []
  })
  let navigationCatalogLoading = $state(true)
  let navigationCatalogError = $state('')
  let activeNotebookMetadata = $derived(
    navigationNotebookMetadata[activeNotebook]
  )
  let showGlobalReplace = $state(false)
  // Global standalone-task quick-add overlay (#368). Opened by the new_task
  // hotkey (default Ctrl+Shift+N). Creates a task in <vault>/.silt/tasks.md
  // via the app-level CreateStandaloneTask binding (not plugin-gated).
  let showQuickAdd = $state(false)
  let globalReplaceQuery = $state('')
  // The active settings section (general/editor/appearance/…). Settings is a
  // view (#511 rework): this id selects which section the sidebar nav +
  // content panel show. Owned here as the single source of truth, bound down
  // to Sidebar→SettingsNav and passed to SettingsPanel.
  let settingsSection = $state('general')
  let showTemplatePicker = $state(false)
  let templatePickerMode = $state<'new-page' | 'insert'>('new-page')
  // F20: set when the backend emits settings:fingerprint-mismatch — the
  // trust-anchor fields (vault_path / trusted_publishers) changed since the
  // last launch. The modal asks the user to confirm or dismiss; confirm
  // clears the sentinel via ConfirmSettingsChange so the next launch is quiet.
  let showSettingsMismatch = $state(false)
  // F4: set when the backend detects a legacy grants: block in this vault's
  // config.yaml that the host has never seen. The modal asks the user to
  // confirm moving grants to per-host storage.
  let showGrantsMigration = $state(false)
  let pendingLegacyGrants = $state<Record<string, Record<string, string>>>({})
  // F3: quarantined linked notebooks (root_path moved or tampered). The modal
  // offers re-link (PickLinkedNotebook) or unlink (UnlinkNotebook).
  let quarantinedLinks = $state<
    { id: string; display_name: string; root_path: string }[]
  >([])

  // Focused block ancestry path highlighting
  let activeFocusedBlockAncestors = $state<string[]>([])
  let searchTargetDate = $state('')
  let searchTargetBlockId = $state('')
  let searchTargetHeading = $state('')
  let searchTargetKey = $state('')
  // Tasks view focus target (#374) — set by openTasksView when a
  // navigation resolves to a `.silt` block. PluginView passes these
  // straight through to the Tasks component as `focusBlockId` /
  // `focusKey`, mirroring the searchTarget* pair above.
  let tasksFocusBlockId = $state('')
  let tasksFocusKey = $state('')

  // Standalone-task navigation router (#374). Switches the active view
  // to Tasks and primes the focus target so the targeted row scrolls
  // into view and gets a transient highlight. No `.silt` page tab is
  // ever created — the Tasks view is a single-mount surface (the
  // activity-bar entry is the single source of view identity for it).
  function openTasksView(blockId: string | undefined): void {
    activeView = 'tasks'
    tasksFocusBlockId = blockId ?? ''
    // Monotonic key so the Tasks view's focus effect re-fires on each
    // navigation, matching the searchTargetKey pattern for normal jumps.
    tasksFocusKey = `${blockId ?? ''}:${Date.now()}`
    // Expand the sidebar on *initial* routing into Tasks so the
    // activity-bar entry is the source of the view identity and the
    // user gets the full canvas. Respect a user who has subsequently
    // collapsed the sidebar via the toggle or Ctrl+B — re-popping it
    // on every subsequent navigation is a UX wisp.
    if (!manuallyCollapsed) {
      sidebarCollapsed = false
    }
  }

  onMount(() => {
    async function checkInit() {
      try {
        isInitialized = await IsVaultInitialized()
      } catch (e) {
        console.error('Startup check failed:', e)
      } finally {
        loading = false
      }
    }
    checkInit()
    // Best-effort: load the config first so the initial loadPlugins call
    // observes plugins.disabled on a cold start (a config.yaml that ships
    // with a pre-disabled first-party plugin must NOT load it on the first
    // paint). loadConfig errors out before a vault is open; that's fine —
    // loadPlugins will then see an empty disabled set, matching the
    // pre-PR behavior.
    loadConfig()
      .catch((e) => console.error('Startup config load failed:', e))
      .finally(() => {
        loadPlugins('', '', '').catch((e) =>
          console.error('Plugin load failed:', e)
        )
      })

    // Load the persisted sidebar width from config.yaml (#63).
    GetSidebarWidth()
      .then((px) => {
        sidebarWidth = px
      })
      .catch(() => {})
    // Restore the persisted open-tab set from config.yaml (#142).
    void loadPersistedTabs()
    // Subscribe to config hot-reload (config:changed from Go) so the settings
    // store refreshes on external edits to .system/config.yaml.
    initConfigHotReload()
    // Inject editor typography CSS variables from config and re-inject on
    // hot-reload. Uses $effect.root to watch the reactive settings store.
    // The returned disposer is called on unmount to prevent duplicate root
    // effects during dev hot-reload.
    const disposeEditorTokens = initEditorTokens()
    // Populate the theme listing store (#47) and subscribe to the
    // backend's "themes:changed" event so an imported theme appears in
    // the picker immediately. Disposed on unmount alongside the other
    // store initializers.
    const disposeThemes = initThemes()
    const disposeTemplates = initTemplates()

    // Throttled startup update check (#312): one quiet GitHub Releases lookup
    // per 24h. Silent on failure (AC5); raises a toast only when an update is
    // available (AC2). Runs independently of any vault being open.
    void initStartupUpdateCheck()

    // Hot-reload the plugin registry when an external config.yaml edit
    // changes plugins.disabled (e.g. the user hand-edits the file as
    // documented in docs/PLUGIN_DEVELOPMENT.md). Diff against the last
    // seen value so unrelated config changes (theme, hotkeys, etc.) do
    // not pay the ESM-import + plugin init cost.
    let prevDisabled: string[] = settings.config?.plugins?.disabled ?? []
    // Initialize the tab hot-reload baseline from the settings store.
    prevOpenTabsKey = tabSetKey(settings.config?.ui?.open_tabs)
    const offConfigChangedReload = Events.On('config:changed', (ev: any) => {
      const cfg: SystemConfig = ev.data
      const next = (cfg?.plugins?.disabled ?? []) as string[]
      if (!arraysEqual(prevDisabled, next)) {
        prevDisabled = [...next]
        loadPlugins(activeNotebook, activeSection, activePage).catch((e) =>
          console.error('Plugin reload after config change failed:', e)
        )
      }
      // Re-hydrate tabs if the external ui.open_tabs block changed
      // (user hand-edited config.yaml or another process wrote it).
      // tabSetKey is intentionally locator-only: a view-mode change must
      // NOT trigger a full re-hydrate (that would rebuild tabs and remount
      // editors on every in-app toggle, since the frontend's own
      // persistTabs write also fires config:changed).
      const nextTabsKey = tabSetKey(cfg?.ui?.open_tabs)
      if (nextTabsKey !== prevOpenTabsKey) {
        prevOpenTabsKey = nextTabsKey
        void loadPersistedTabs()
      }
      // Reconcile per-tab view_mode from an external config.yaml edit
      // in place — no re-hydrate, no editor remount. The frontend's own
      // writes match the in-memory state, so they produce no diff here;
      // only an external hand-edit (or another process) flips a mode.
      const externalTabs = cfg?.ui?.open_tabs ?? []
      if (externalTabs.length > 0) {
        for (const ref of externalTabs) {
          const tab = openTabs.find(
            (t) =>
              t.notebook === ref.notebook &&
              t.section === (ref.section ?? '') &&
              t.page === ref.page
          )
          if (!tab) continue
          const mode = ref.view_mode === 'source' ? 'source' : 'edit'
          if (tab.viewMode !== mode) {
            openTabs = setTabViewModeState(
              { tabs: openTabs, activeId: activeTabId },
              tab.id,
              mode
            ).tabs
            // Do NOT schedulePersistTabs — this change is already on disk.
          }
        }
      }
    })

    function handleOpenSettings(e: Event) {
      const detail = (e as CustomEvent).detail
      // ctx.openSettings dispatches detail: tab ?? '' — an empty/missing
      // detail means "general".
      const section = typeof detail === 'string' && detail ? detail : 'general'
      openSettings(section)
    }

    // Tasks hub command palette (#436) delegates Find task / Add task to the
    // app-level SearchModal and QuickAdd overlays via these host events.
    function handleOpenSearch() {
      showSearch = true
    }
    function handleOpenQuickAdd() {
      showQuickAdd = true
    }

    // Summary-strip chips in GeneralTab dispatch this to jump between
    // settings sections while already in the settings view (no view change).
    function handleSettingsJump(e: Event) {
      const detail = (e as CustomEvent).detail
      if (!detail || typeof detail.section !== 'string') return
      // Validate against the live section registry via the shared resolver so
      // a typo'd id from a future dispatcher can't render an empty/broken
      // panel. Falls back to 'general' on a miss rather than navigating nowhere.
      settingsSection = resolveSettingsSectionId(
        detail.section,
        getSettingsSections().map((s) => s.id)
      )
    }
    // Move keyboard focus into the active sidebar (#326 item 8). Expands the
    // sidebar if collapsed, then focuses the first focusable element inside it
    // (a tree node, a smart-list radio, a scope radio, or a search input —
    // whichever the active sidebar surfaces first). Ctrl+Shift+B is not a
    // format shortcut, so it fires globally even while the editor is focused.
    async function focusSidebar() {
      if (sidebarCollapsed) {
        sidebarCollapsed = false
        manuallyCollapsed = false
      }
      await tick()
      // One rAF so the expand's width transition has started and the target
      // is laid out before we focus it.
      requestAnimationFrame(() => {
        const aside = document.querySelector<HTMLElement>('[data-sidebar]')
        if (!aside) return
        const focusable = aside.querySelector<HTMLElement>(
          'button:not([disabled]), input:not([disabled]), [href], [tabindex]:not([tabindex="-1"])'
        )
        focusable?.focus()
      })
    }

    function handleGlobalKeyDown(e: KeyboardEvent) {
      // Config-driven global shortcuts. Resolution (editor-focus guard +
      // first-match-wins ordering) lives in the pure resolveGlobalHotkey so
      // it is unit-tested; this handler only switch-dispatches the result.
      const hotkeys = effectiveHotkeys(settings.config?.hotkeys ?? {})
      const eventTarget = e.target
      const editorFocused =
        eventTarget instanceof Element && !!eventTarget.closest('.ProseMirror')
      const action = resolveGlobalHotkey(
        e,
        hotkeys,
        editorFocused,
        displayedTabs.length > 0
      )
      if (!action) return
      e.preventDefault()
      switch (action) {
        case 'open_search':
          showSearch = !showSearch
          break
        case 'new_page':
          void requestNavigationCreation('page')
          break
        case 'new_section':
          void requestNavigationCreation('section')
          break
        case 'new_notebook':
          void requestNavigationCreation('notebook')
          break
        case 'open_quick_switcher':
          showQuickSwitcher = !showQuickSwitcher
          break
        case 'open_shortcuts_help':
          showShortcutHelp = !showShortcutHelp
          break
        case 'find_in_page':
          findBarState.openFind()
          break
        case 'replace':
          findBarState.openReplace()
          break
        case 'global_replace':
          showGlobalReplace = !showGlobalReplace
          break
        case 'toggle_sidebar':
          sidebarCollapsed = !sidebarCollapsed
          manuallyCollapsed = sidebarCollapsed
          break
        case 'focus_sidebar':
          void focusSidebar()
          break
        case 'cycle_view_layout':
          cycleView()
          break
        case 'open_template_picker':
          templatePickerMode = 'new-page'
          showTemplatePicker = !showTemplatePicker
          break
        case 'new_task':
          showQuickAdd = !showQuickAdd
          break
        case 'toggle_view_mode':
          // Flip the active tab's view mode directly (#195) — no window-event
          // indirection, App owns the per-tab state.
          if (activeTabId) handleToggleViewMode(activeTabId)
          break
        case 'toggle_format_toolbar':
          void toggleFormatToolbar()
          break
        case 'toggle_focus_mode':
          void toggleFocusMode()
          break
        case 'toggle_typewriter_mode':
          void toggleTypewriterMode()
          break
        case 'open_settings':
          openSettings()
          break
        case 'next_tab':
          handleCycleTab(1)
          break
        case 'prev_tab':
          handleCycleTab(-1)
          break
        case 'close_tab':
          // Guard: only close if the active tab is visible in the current
          // notebook's displayed set (#142 review: closing a hidden tab
          // from another notebook would be surprising to the user).
          if (activeTabId && displayedTabs.some((t) => t.id === activeTabId)) {
            handleCloseTab(activeTabId)
          }
          break
      }
    }

    // Smart Graph navigation: refs/embeds/tag-pills dispatch these.
    function handleNavigateToBlock(e: Event) {
      const d = (e as CustomEvent).detail
      if (d) {
        // Standalone-task routing guard (#374). A `.silt` notebook ref
        // routes to the Tasks view instead of a raw page tab. The Tasks
        // view's `focusBlockId` prop handles scroll+highlight on mount.
        const target = routeJumpTarget({
          notebook: d.notebook,
          section: d.section,
          page: d.page,
          blockTarget: d.blockId ? { blockId: d.blockId } : undefined
        })
        if (target.kind === 'tasks-view') {
          openTasksView(target.blockTarget?.blockId)
          return
        }
        handleSearchJump(d.notebook, d.section, d.page, d.date, d.blockId)
      }
    }
    // Wiki-link navigation (#545). Opens the resolved page; optional heading
    // scrolls to the matching HEADER block after open.
    function handleNavigateToPage(e: Event) {
      const d = (e as CustomEvent).detail
      if (!d?.notebook || !d?.page) return
      handleSearchJump(
        d.notebook,
        d.section ?? '',
        d.page,
        d.date ?? '',
        d.blockId ?? ''
      )
      if (d.heading) {
        searchTargetHeading = d.heading
        searchTargetKey = `heading:${d.heading}:${Date.now()}`
      }
    }
    function handleNavigateToTag(e: Event) {
      const tagPath = (e as CustomEvent).detail
      if (tagPath) {
        selectedTag = tagPath
        activeView = 'tags'
      }
    }
    function handleSwitchView(e: Event) {
      // PluginsTab "Open view" + any other switch-view dispatcher.
      const detail = (e as CustomEvent).detail
      if (typeof detail === 'string' && detail) {
        activeView = detail
      }
    }
    function handleOpenPluginManager() {
      // The plugin manager is the "Plugins" section inside Settings.
      openSettings('plugins')
    }
    async function handlePluginsChanged() {
      // Refresh grants BEFORE re-running discovery so re-registration reads the
      // updated capabilities (fixes the race where loadPlugins read a stale
      // grant cache), then drop contributions from plugins that lost a
      // capability without a full reload (#582). grants.svelte no longer
      // subscribes to plugins:changed itself — this is the single orchestrator.
      await refreshGrants()
      revokeRevokedContributions()
      // Re-run discovery with the live location so newly installed/enabled
      // plugins appear and removed ones drop out.
      loadPlugins(activeNotebook, activeSection, activePage).catch((e) =>
        console.error('Plugin reload failed:', e)
      )
    }
    function handleOpenTemplatePicker() {
      templatePickerMode = 'new-page'
      showTemplatePicker = true
    }
    function handlePageRenamed(e: Event) {
      const { notebook, section, oldName, newName } = (e as CustomEvent)
        .detail as {
        notebook: string
        section: string
        oldName: string
        newName: string
      }
      openTabs = openTabs.map((t) =>
        t.notebook === notebook && t.section === section && t.page === oldName
          ? { ...t, page: newName }
          : t
      )
      if (
        activeNotebook === notebook &&
        activeSection === section &&
        activePage === oldName
      ) {
        activePage = newName
      }
    }

    window.addEventListener('keydown', handleGlobalKeyDown)
    window.addEventListener('navigate-to-block', handleNavigateToBlock)
    window.addEventListener('navigate-to-page', handleNavigateToPage)
    window.addEventListener('navigate-to-tag', handleNavigateToTag)
    window.addEventListener('switch-view', handleSwitchView)
    window.addEventListener('open-plugin-manager', handleOpenPluginManager)
    window.addEventListener('open-settings', handleOpenSettings)
    window.addEventListener('open-template-picker', handleOpenTemplatePicker)
    window.addEventListener('open-search', handleOpenSearch)
    window.addEventListener('open-quick-add', handleOpenQuickAdd)
    window.addEventListener('silt:change-vault', handleSwitchVault)
    window.addEventListener('silt:settings-jump', handleSettingsJump)
    window.addEventListener('page-renamed', handlePageRenamed)
    // `plugins:changed` is a Wails event (Go runtime.EventsEmit), so it must
    // be received via Events.On — a DOM addEventListener would never fire.
    const offPluginsChanged = Events.On(
      'plugins:changed',
      () => void handlePluginsChanged()
    )
    // `vault:moved` fires after a successful vault Move/Copy-Switch (#141).
    // The backend has already reinitialized services at the new path; reset
    // navigation, close settings, and reload the (vault-scoped) config store
    // so the UI reflects the new workspace. If the optional old-vault removal
    // didn't happen, payload.warning carries the reason → surface a non-
    // blocking toast (the move itself succeeded).
    const offVaultMoved = Events.On('vault:moved', (ev: any) => {
      const e: { from?: string; to?: string; warning?: string } = ev.data
      activeNotebook = ''
      activeSection = ''
      activePage = ''
      openTabs = []
      activeTabId = ''
      activeView = 'notes'
      clearSelectionFocus()
      // Drop any editor reconciliation handles tied to the old vault so a
      // teardown that bypassed Svelte $effect cleanup can't leave a stale
      // editor buffer flushing into the new vault (#345).
      clearAllEditors()
      loadConfig().catch((e) =>
        console.error('Post-move config reload failed:', e)
      )
      window.dispatchEvent(new CustomEvent('refresh-navigation'))
      if (e?.warning) {
        pushNotification({ kind: 'error', message: e.warning })
      }
    })
    // F20: trust-anchor fingerprint mismatch — the backend detected that
    // vault_path or trusted_publishers changed since last launch (possible
    // tampering, or a legit external edit). Show a confirmation modal; the
    // user can confirm (clears the sentinel) or dismiss (mismatch persists
    // on next launch). Extracted to a named handler so both the live
    // Events.On listener and the startup-event replay (dispatchStartupEvent)
    // run the exact same code.
    function handleSettingsMismatch() {
      showSettingsMismatch = true
    }
    const offSettingsMismatch = Events.On(
      'settings:fingerprint-mismatch',
      handleSettingsMismatch
    )
    // F4: grants migration — the vault's legacy config.yaml carries a grants
    // block this host has never seen. Show a one-time confirmation modal.
    function handleGrantsMigration(
      grants: Record<string, Record<string, string>>
    ) {
      pendingLegacyGrants = grants
      showGrantsMigration = true
    }
    const offGrantsMigration = Events.On(
      'grants:migration-required',
      (ev: any) => {
        handleGrantsMigration(ev.data)
      }
    )
    // F3: linked-notebook quarantined — the root was moved or tampered with.
    // Refresh the quarantine list so the modal shows the latest set.
    async function handleLinkedQuarantined() {
      try {
        quarantinedLinks = await ResolveQuarantinedLinks()
      } catch (e) {
        console.error('ResolveQuarantinedLinks failed:', e)
      }
    }
    const offLinkedQuarantined = Events.On(
      'linked-notebook:quarantined',
      handleLinkedQuarantined
    )
    // Vault init failed during startup (settings.json unreadable, DB open
    // failed, network-filesystem vault, watcher start failed, …). Without
    // this listener the backend error vanishes and every page renders blank
    // with no clue why — the user sees a dead frame. Surface it as a sticky
    // error toast so the cause is visible. (Wails delivers OnStartup after
    // the frontend mounts, so this listener is registered in time.)
    function handleVaultInitError(msg: string) {
      pushNotification({
        kind: 'error',
        message: `Vault failed to initialize: ${msg}`,
        autoDismissMs: 0
      })
    }
    const offVaultInitError = Events.On('vault:init-error', (ev: any) => {
      handleVaultInitError(ev.data)
    })
    // Non-fatal init warnings (symlink skips, permission errors during scan).
    // These don't block usage but explain missing/partial content.
    function handleVaultInitWarnings(warnings: string[]) {
      if (!warnings?.length) return
      pushNotification({
        kind: 'info',
        message: `Vault initialized with warnings: ${warnings.join('; ')}`,
        autoDismissMs: 0
      })
    }
    const offVaultInitWarnings = Events.On('vault:init-warnings', (ev: any) => {
      handleVaultInitWarnings(ev.data)
    })
    // fsnotify subscription failures (watch limit, permissions). File-change
    // watching is degraded for these paths — indexing and autosave
    // reconciliation may not track external edits to them.
    function handleVaultWatchCoverage(failedPaths: string[]) {
      if (!failedPaths?.length) return
      pushNotification({
        kind: 'info',
        message: `File watching unavailable for ${failedPaths.length} path(s). External edits to these folders won't auto-sync.`,
        autoDismissMs: 0
      })
    }
    const offVaultWatchCoverage = Events.On(
      'vault:watch-coverage',
      (ev: any) => {
        handleVaultWatchCoverage(ev.data)
      }
    )
    // Mass id re-mint detection (#443): an external tool/sync stripped the
    // block-identity comments from a previously-indexed file, so the parser
    // re-minted fresh UUIDs — which can break note-to-note links pointing at
    // those blocks. The toast (built by reMintToast) is sticky, leads with
    // the user-visible impact, and offers a "Show file" CTA. The builder is
    // extracted so its payload-shaping contract is unit-testable.
    const offReMintWarning = Events.On('index:re-mint-warning', (ev: any) => {
      const w: ReMintWarning = ev.data
      if (!w) return
      pushNotification(reMintToast(w, openPage))
    })
    // Wiki-link rename rewrite summary (#545 harden). Partial failures used
    // to be log-only; surface a toast so inbound [[…]] that failed to update
    // are not silent.
    const offPageLinksRewritten = Events.On(
      'page-links:rewritten',
      (ev: any) => {
        const d = ev?.data as
          { rewritten?: number; failed?: number } | undefined
        if (!d) return
        const rewritten = d.rewritten ?? 0
        const failed = d.failed ?? 0
        if (failed > 0) {
          pushNotification({
            kind: 'error',
            message:
              rewritten > 0
                ? `Updated ${rewritten} linked page(s); ${failed} could not be rewritten.`
                : `Could not rewrite wiki-links in ${failed} page(s). Check the log for details.`,
            autoDismissMs: 0
          })
        } else if (rewritten > 0) {
          pushNotification({
            kind: 'info',
            message: `Updated wiki-links in ${rewritten} page(s).`,
            autoDismissMs: 4000
          })
        }
      }
    )

    // Native menu events (#503) — the Go-side menu items emit these; wire
    // them to the same handlers the keyboard shortcuts use so menu and
    // hotkey actions are indistinguishable.
    const offMenuNewPage = Events.On('menu:new-page', () => {
      templatePickerMode = 'new-page'
      showTemplatePicker = !showTemplatePicker
    })
    const offMenuOpenVault = Events.On('menu:open-vault', () => {
      void handleSwitchVault()
    })
    const offMenuSave = Events.On('menu:save', () => void handleMenuSave())
    const offMenuToggleSidebar = Events.On('menu:toggle-sidebar', () => {
      sidebarCollapsed = !sidebarCollapsed
      manuallyCollapsed = sidebarCollapsed
    })
    const offMenuToggleFormatToolbar = Events.On(
      'menu:toggle-format-toolbar',
      () => void toggleFormatToolbar()
    )
    const offMenuFind = Events.On('menu:find', () => {
      findBarState.openFind()
    })
    const offMenuFocusMode = Events.On('menu:focus-mode', () => {
      void toggleFocusMode()
    })
    const offMenuSettings = Events.On('menu:settings', () => {
      openSettings('general')
    })
    const offMenuAbout = Events.On('menu:about', () => {
      openSettings('about')
    })

    // Wails v3 fires ServiceStartup before the webview exists, so every
    // startup-time emit (vault:init-error, settings:fingerprint-mismatch,
    // grants:migration-required, vault:init-warnings, vault:watch-coverage,
    // linked-notebook:quarantined) is lost — no JS listener was registered
    // yet. The backend stashes those via emitOrQueue; here we mark the
    // frontend ready (stop queueing), drain the queue, and replay each event
    // through the same named handler its live Events.On listener uses, so a
    // startup event is indistinguishable from a live one to the handler.
    function dispatchStartupEvent(name: string, data: any): void {
      switch (name) {
        case 'settings:fingerprint-mismatch':
          handleSettingsMismatch()
          break
        case 'grants:migration-required':
          handleGrantsMigration(data)
          break
        case 'linked-notebook:quarantined':
          void handleLinkedQuarantined()
          break
        case 'vault:init-error':
          handleVaultInitError(data)
          break
        case 'vault:init-warnings':
          handleVaultInitWarnings(data)
          break
        case 'vault:watch-coverage':
          handleVaultWatchCoverage(data)
          break
        default:
          break
      }
    }

    void (async () => {
      try {
        await MarkFrontendReady()
        const missed = await GetStartupEvents()
        for (const ev of missed ?? []) {
          dispatchStartupEvent(ev.Name, ev.Payload)
        }
      } catch (e) {
        console.error('Startup event replay failed:', e)
      }
    })()

    return () => {
      window.removeEventListener('keydown', handleGlobalKeyDown)
      window.removeEventListener('navigate-to-block', handleNavigateToBlock)
      window.removeEventListener('navigate-to-page', handleNavigateToPage)
      window.removeEventListener('navigate-to-tag', handleNavigateToTag)
      window.removeEventListener('switch-view', handleSwitchView)
      window.removeEventListener('open-plugin-manager', handleOpenPluginManager)
      window.removeEventListener('open-settings', handleOpenSettings)
      window.removeEventListener(
        'open-template-picker',
        handleOpenTemplatePicker
      )
      window.removeEventListener('open-search', handleOpenSearch)
      window.removeEventListener('open-quick-add', handleOpenQuickAdd)
      window.removeEventListener('silt:change-vault', handleSwitchVault)
      window.removeEventListener('silt:settings-jump', handleSettingsJump)
      window.removeEventListener('page-renamed', handlePageRenamed)
      offPluginsChanged()
      offVaultMoved()
      offConfigChangedReload()
      offSettingsMismatch()
      offGrantsMigration()
      offLinkedQuarantined()
      offVaultInitError()
      offVaultInitWarnings()
      offVaultWatchCoverage()
      offReMintWarning()
      offPageLinksRewritten()
      offMenuNewPage()
      offMenuOpenVault()
      offMenuSave()
      offMenuToggleSidebar()
      offMenuToggleFormatToolbar()
      offMenuFind()
      offMenuFocusMode()
      offMenuSettings()
      offMenuAbout()
      disposeEditorTokens()
      disposeThemes()
      disposeTemplates()
      disposeUpdateStore()
      // Flush any pending tab-state persistence so the user's last tab
      // change survives a component unmount / app close (#142 hardening).
      if (persistTabsTimer) {
        clearTimeout(persistTabsTimer)
        persistTabsTimer = null
        void persistTabs()
      }
    }
  })

  async function handleSelectFolder() {
    try {
      const success = await InitializeVault()
      if (success) {
        isInitialized = true
        // Populate the config store now that a vault exists so config-driven
        // global shortcuts work immediately after onboarding.
        loadConfig().catch((e) =>
          console.error('Post-init config load failed:', e)
        )
        // Restore the persisted tab set from config.yaml (#142).
        void loadPersistedTabs()
        window.dispatchEvent(new CustomEvent('refresh-navigation'))
      }
    } catch (e) {
      alert('Failed to initialize vault: ' + e)
    }
  }

  // Change Vault: tear down the active vault and re-show the onboarding
  // screen so the user can pick (or re-pick) a workspace folder (#33). The
  // backend CloseVault waits on any in-flight writes and checkpoints the WAL.
  async function handleChangeVault() {
    try {
      await CloseVault()
      // Re-query rather than assume — CloseVault is the source of truth.
      isInitialized = await IsVaultInitialized()
      clearSelectionFocus()
      activeNotebook = ''
      activeSection = ''
      activePage = ''
      activeView = 'notes'
      // Clear the tab strip (#142).
      openTabs = []
      activeTabId = ''
    } catch (e) {
      console.error('Failed to close vault:', e)
    }
  }

  // Settings → Workspace → "Switch vault…" entry. Runs the same tear-down flow
  // as the (removed) sidebar Change Vault button, returning the user to the
  // onboarding screen to pick a vault. The Settings tab (if open) is cleared
  // by handleChangeVault's openTabs = [] reset.
  async function handleSwitchVault() {
    await handleChangeVault()
  }

  function handleSearchJump(
    notebook: string,
    section: string,
    page: string,
    date: string,
    blockId: string
  ) {
    // Standalone-task routing guard (#374). A `.silt` notebook ref from
    // the search modal routes to the Tasks view; we deliberately do NOT
    // set `activeView = 'notes'` (which would jump the user out of the
    // Tasks view after the search dialog closes) and we do NOT add a
    // `.silt` page tab. The Tasks view's focusBlockId prop handles the
    // scroll+highlight on mount.
    const target = routeJumpTarget({
      notebook,
      section,
      page,
      blockTarget: blockId ? { blockId } : undefined
    })
    if (target.kind === 'tasks-view') {
      openTasksView(target.blockTarget?.blockId)
      return
    }
    // Route through openPage (preview-tab semantics, #142).
    // Use activate-only when the target IS the active page so block
    // navigation does not re-bump the MRU timestamp (the state machine's
    // activate-only path is a true no-op on tab state, just sets the
    // scroll-to-block target). Otherwise open in preview mode.
    const activeTab = openTabs.find((t) => t.id === activeTabId)
    const isSamePage =
      activeTab &&
      activeTab.notebook === notebook &&
      activeTab.section === section &&
      activeTab.page === page
    openPage(
      { notebook, section, page },
      isSamePage ? 'activate-only' : 'preview',
      { fileDate: date, blockId }
    )
    activeView = 'notes'
    searchTargetDate = date
    searchTargetBlockId = blockId
    searchTargetHeading = ''
    searchTargetKey = `${date}:${blockId}:${Date.now()}`
  }

  // Called by the TemplatePicker when a new page is created from a template.
  // Navigates to the freshly-created page (the reactive cascade loads it in
  // the editor) and refreshes the sidebar tree so the new page appears.
  function handleTemplatePageCreated(page: string): void {
    openPage(
      { notebook: activeNotebook, section: activeSection, page },
      'preview'
    )
    activeView = 'notes'
    window.dispatchEvent(new CustomEvent('refresh-navigation'))
  }

  function handleBlockFocus(blockId: string, ancestors: string[]) {
    activeFocusedBlockAncestors = ancestors
  }

  function handleBlockBlur() {
    activeFocusedBlockAncestors = []
  }

  // Sidebar resize handlers (#63).
  const MIN_MAIN_WIDTH = 480

  function handleSidebarWidthChange(px: number) {
    sidebarWidth = px
  }

  let setSidebarTimer: ReturnType<typeof setTimeout> | null = null
  function handleSidebarWidthCommit(px: number) {
    sidebarWidth = px
    if (setSidebarTimer) clearTimeout(setSidebarTimer)
    setSidebarTimer = setTimeout(() => {
      SetSidebarWidth(Math.round(px)).catch((e) =>
        console.error('SetSidebarWidth failed:', e)
      )
    }, 250)
  }

  function handleSidebarDragStart() {
    sidebarDragging = true
  }
  function handleSidebarDragEnd() {
    sidebarDragging = false
  }

  // SearchModal returns a flat result object; adapt it to the 5-arg jump.
  function handleSearchResultJump(res: any) {
    handleSearchJump(res.notebook, res.section, res.page, res.file_date, res.id)
  }

  // Whether the notes view has a complete (notebook/section/page) target.
  // With tabs (#142), also requires an active tab so closing the last tab
  // returns to the blank view. displayedTabs ensures per-notebook scoping.
  let notesReady = $derived(
    activeView === 'notes' &&
      !!activeNotebook &&
      !!activePage &&
      !!activeTabId &&
      displayedTabs.length > 0
  )

  // Switch to the Settings view and select a section (general/editor/
  // appearance/…). Settings is a view, not a tab (#511 rework): no tab state is
  // touched, and the navigation triple is left intact so the user returns to
  // their page when they leave Settings. The section persists across opens so
  // re-entering Settings returns the user to the panel they last visited.
  function openSettings(section: string = 'general') {
    // Validate against the live section registry so an unknown id (e.g. a
    // typo'd ctx.openSettings('foo') from a plugin) can't render a blank
    // panel or point aria-labelledby at a nonexistent tab. Falls back to
    // 'general'. Mirrors the validation in handleSettingsJump via the shared
    // resolver so both entry paths are consistent.
    settingsSection = resolveSettingsSectionId(
      section,
      getSettingsSections().map((s) => s.id)
    )
    activeView = 'settings'
  }

  // Native menu Save (#503): flush the active editor's pending autosave
  // directly. There is no global Ctrl+S keymap, so the previous handler —
  // which synthesized a keydown — was a silent no-op. The editor already
  // debounces autosave; this is the explicit "save now" path, and it targets
  // the editor mounted for the active page. With no page context (Tasks view,
  // sidebar browsing, onboarding) or no mounted editor, there is nothing to
  // flush — return quietly. A flush that reports failure or rejects surfaces
  // through the existing notification channel.
  async function handleMenuSave(): Promise<void> {
    if (!activePage) return
    const editor = getEditor(
      editorKey(activeNotebook, activeSection, activePage)
    )
    if (!editor) return
    try {
      const ok = await editor.flush()
      if (!ok) {
        pushNotification({
          kind: 'error',
          message:
            'Could not save the current page — unsaved edits are still pending.'
        })
      }
    } catch (e) {
      console.error('menu:save flush failed:', e)
      pushNotification({
        kind: 'error',
        message: 'Could not save the current page.'
      })
    }
  }

  // Ordered view cycle for the cycle_view_layout hotkey (default Ctrl+Alt+V).
  // If the current view is not in the list (e.g. a plugin view), jump to
  // 'notes' as the anchor.
  function cycleView() {
    activeView = nextView(activeView)
  }

  // Order-independent string-array equality (the disabled list is a set
  // semantically — config.yaml can re-order it without changing meaning).
  // Used by the config:changed handler to decide whether to re-run
  // loadPlugins on a hot-reload.
  function arraysEqual(a: readonly string[], b: readonly string[]): boolean {
    if (a.length !== b.length) return false
    const setA = new Set(a)
    return b.every((x) => setA.has(x))
  }

  // Stable serialization of the persisted open_tabs list for change detection.
  // The config:changed handler compares the previous and next keys to decide
  // whether to re-hydrate the tab strip on an external config.yaml edit.
  function tabSetKey(
    tabs: { notebook?: string; section?: string; page?: string }[] | undefined
  ): string {
    if (!tabs || tabs.length === 0) return ''
    return tabs
      .map(
        (t) => `${t.notebook ?? ''}\x00${t.section ?? ''}\x00${t.page ?? ''}`
      )
      .sort()
      .join('|')
  }
</script>

<main
  class="w-full h-full flex flex-col bg-surface-app text-text-primary overflow-hidden font-body-md"
>
  {#if loading || !isInitialized}
    <Onboarding
      {loading}
      initialized={isInitialized}
      onSelectFolder={handleSelectFolder}
    />
  {:else}
    <TitleBar
      bind:sidebarCollapsed
      {sidebarWidth}
      onSearchClick={() => (showSearch = true)}
      onSwitcherClick={() => (showQuickSwitcher = true)}
      onShortcutHelpClick={() => (showShortcutHelp = true)}
      onAIClick={getAIAvailability().drawerAvailable
        ? () => toggleAIChatDrawer()
        : undefined}
      aiOpen={aiChatDrawer.open}
    >
      {#if activeView === 'notes'}
        <TabStrip
          tabs={displayedTabs}
          {activeTabId}
          onSelectTab={handleSelectTab}
          onCloseTab={handleCloseTab}
          onPromoteTab={handlePromoteTab}
          onReorderTab={handleReorderTab}
          showDirtyIndicators={settings.config?.ui
            ?.show_tab_dirty_indicators !== false}
        />
      {:else}
        <div
          class="flex items-center px-4 py-1 text-surface-sidebar-text-muted text-type-xs uppercase tracking-widest font-label-sm-bold"
        >
          {activeView === 'settings'
            ? themeEditorSession.open
              ? 'Theme editor'
              : 'Settings'
            : (views.find((v) => v.id === activeView)?.label ?? activeView)}
        </div>
      {/if}
    </TitleBar>

    <div class="flex-1 flex mt-12 w-full relative min-h-0">
      <!-- Activity Bar -->
      <div
        class="w-12 bg-surface-activitybar border-r border-surface-activitybar-border flex flex-col items-center py-4 justify-between h-full select-none z-50 flex-shrink-0"
      >
        <div class="flex flex-col gap-4 items-center w-full">
          {#each views as v (v.id)}
            <button
              onclick={() => {
                if (activeView === v.id) {
                  sidebarCollapsed = !sidebarCollapsed
                  manuallyCollapsed = sidebarCollapsed
                } else {
                  activeView = v.id
                  sidebarCollapsed = false
                  manuallyCollapsed = false
                }
              }}
              class="relative w-9 h-9 rounded-lg flex items-center justify-center transition-all cursor-pointer border-none bg-transparent hover:bg-hover hover:scale-105 active:scale-95 group focus:outline-none"
              class:text-accent-primary-start={activeView === v.id}
              class:text-surface-activitybar-text-muted={activeView !== v.id}
              aria-label={v.label}
              aria-pressed={activeView === v.id}
              title={v.label}
            >
              {#if activeView === v.id}
                <div
                  class="absolute left-0 top-2 bottom-2 w-0.5 bg-accent-primary-start rounded-full shadow-accent-glow"
                  style:opacity={sidebarCollapsed ? '0.5' : '1'}
                ></div>
              {/if}
              <span
                class="material-symbols-outlined text-type-2xl"
                style:color={activeView === v.id
                  ? undefined
                  : `var(--color-nav-icon-${v.id})`}>{v.icon}</span
              >
            </button>
          {/each}
        </div>

        <button
          onclick={() => openSettings('general')}
          class="relative w-9 h-9 rounded-lg flex items-center justify-center transition-all cursor-pointer border-none bg-transparent hover:bg-hover hover:scale-105 active:scale-95 group focus:outline-none"
          class:text-accent-primary-start={activeView === 'settings'}
          class:text-surface-activitybar-text-muted={activeView !== 'settings'}
          aria-label="Settings"
          aria-pressed={activeView === 'settings'}
          title="Settings"
        >
          {#if activeView === 'settings'}
            <div
              class="absolute left-0 top-2 bottom-2 w-0.5 bg-accent-primary-start rounded-full shadow-accent-glow"
              style:opacity={sidebarCollapsed ? '0.5' : '1'}
            ></div>
          {/if}
          <span
            class="material-symbols-outlined text-type-2xl"
            style:color={`var(--color-nav-icon-settings)`}>settings</span
          >
        </button>
      </div>

      <!-- Immersive theme editor: hide Settings sidebar so we don't stack
           activity bar → settings sections → editor groups. -->
      {#if !(activeView === 'settings' && themeEditorSession.open)}
        {#if sidebarCollapsed}
          <button
            onclick={() => {
              sidebarCollapsed = false
              manuallyCollapsed = false
            }}
            transition:fade={{ duration: 150 }}
            aria-label="Show sidebar"
            title="Show sidebar (Ctrl+B)"
            class="absolute bottom-4 left-16 z-50 w-8 h-8 rounded-lg bg-surface-sidebar/80 backdrop-blur-md border border-surface-sidebar-border text-surface-sidebar-text-muted hover:text-accent-primary-start hover:border-accent-primary-start/40 flex items-center justify-center transition-all cursor-pointer shadow-lg hover:scale-105 active:scale-95"
          >
            <span class="material-symbols-outlined text-icon-lg"
              >left_panel_open</span
            >
          </button>
        {/if}

        <Sidebar
          bind:activeNotebook
          bind:activeSection
          bind:activePage
          bind:activeView
          bind:selectedTag
          bind:settingsSection
          bind:collapsed={sidebarCollapsed}
          {sidebarWidth}
          {sidebarDragging}
          onSelectNotebook={selectNotebookContext}
          onSelectSection={(sec) => (activeSection = sec)}
          onSelectPage={(nb, sec, pg) => {
            // Single-click opens in preview mode (industry-standard parity, #142).
            openPage({ notebook: nb, section: sec, page: pg }, 'preview')
          }}
          onPinPage={(nb, sec, pg) => {
            // Double-click / middle-click opens a pinned tab (#142).
            openPage({ notebook: nb, section: sec, page: pg }, 'pin')
          }}
          onSelectView={(v) => (activeView = v)}
          onNavigationLoaded={(tree) => {
            navigationCatalog = flattenNavigation(tree)
            navigationNotebookMetadata = notebookNavigationMetadata(tree)
          }}
          onNavigationPreferencesLoaded={(preferences) =>
            (navigationPreferences = preferences)}
          onNavigationStatus={(loading, error) => {
            navigationCatalogLoading = loading
            navigationCatalogError = error
          }}
          onPageMoved={(nb, fromSection, toSection, page) => {
            // A page was dragged across sections in the sidebar (#177). Update
            // the open tab for this specific page+section so its section field
            // points to the new location. Matching on fromSection is critical —
            // without it, a same-named sibling in another section would also be
            // repointed, causing its next save to write to the wrong path.
            openTabs = openTabs.map((t) =>
              t.notebook === nb && t.section === fromSection && t.page === page
                ? { ...t, section: toSection }
                : t
            )
            if (
              activeNotebook === nb &&
              activePage === page &&
              activeSection === fromSection
            ) {
              activeSection = toSection
            }
            schedulePersistTabs()
          }}
        />

        {#if !sidebarCollapsed}
          <SidebarResizeHandle
            width={sidebarWidth}
            onWidthChange={handleSidebarWidthChange}
            onWidthCommit={handleSidebarWidthCommit}
          />
        {/if}
      {/if}

      <!-- Content viewport + optional AI Search right drawer -->
      <div class="flex-1 h-full min-w-0 flex overflow-hidden bg-surface-app">
        <div class="flex-1 h-full min-w-0 flex flex-col overflow-hidden">
          {#if settings.config?.ui?.open_devtools_on_startup === true}
            <div
              class="absolute bottom-2 left-1/2 -translate-x-1/2 z-[999] bg-red-600 text-white text-type-2xs font-mono px-2 py-1 rounded opacity-80 pointer-events-none"
            >
              view={activeView} nb={activeNotebook || '-'} pg={activePage ||
                '-'} tab={activeTabId || '-'} dt={displayedTabs.length} nr={notesReady}
            </div>
          {/if}
          {#if activeView === 'notes'}
            <PageBreadcrumb
              notebook={activeNotebook}
              section={activeSection}
              page={activePage}
              {activeView}
              linked={activeNotebookMetadata?.linked ?? false}
              disconnected={activeNotebookMetadata?.disconnected ?? false}
              onSelectNotebook={selectNotebookContext}
              onSelectSection={openSectionContext}
              onOpenPage={() =>
                openPage(
                  {
                    notebook: activeNotebook,
                    section: activeSection,
                    page: activePage
                  },
                  'activate-only'
                )}
            />
            {#if notesReady}
              <div
                id="silt-tabpanel"
                role="tabpanel"
                aria-labelledby="silt-tab-{activeTabId}"
                class="flex-1 min-h-0 flex flex-col overflow-hidden"
              >
                {#each displayedTabs as tab (tab.id)}
                  <div
                    class="flex-1 min-h-0 flex flex-col overflow-hidden"
                    style:display={tab.id === activeTabId ? 'flex' : 'none'}
                  >
                    <VirtualScrollContainer
                      notebook={tab.notebook}
                      section={tab.section}
                      page={tab.page}
                      viewMode={tab.viewMode}
                      onToggleViewMode={() => handleToggleViewMode(tab.id)}
                      isActive={tab.id === activeTabId}
                      targetBlockId={tab.id === activeTabId
                        ? searchTargetBlockId
                        : ''}
                      targetHeading={tab.id === activeTabId
                        ? searchTargetHeading
                        : ''}
                      targetKey={tab.id === activeTabId ? searchTargetKey : ''}
                      activeFocusedBlockAncestors={tab.id === activeTabId
                        ? activeFocusedBlockAncestors
                        : []}
                      onBlockFocus={tab.id === activeTabId
                        ? handleBlockFocus
                        : undefined}
                      onBlockBlur={tab.id === activeTabId
                        ? handleBlockBlur
                        : undefined}
                      onPageRenamed={(newName) => {
                        // Update the tab's page name AND the active triple.
                        openTabs = openTabs.map((t) =>
                          t.id === tab.id ? { ...t, page: newName } : t
                        )
                        if (tab.id === activeTabId) activePage = newName
                      }}
                      onFirstEdit={tab.preview
                        ? () => handlePromoteTab(tab.id)
                        : undefined}
                      onSaveStateChange={(s) => {
                        // Surface the editor's save state on the tab header
                        // so it's visible from any tab (#167, #546).
                        openTabs = openTabs.map((t) =>
                          t.id === tab.id
                            ? {
                                ...t,
                                dirty: s.dirty,
                                saveError: s.error,
                                savePhase: s.phase
                              }
                            : t
                        )
                      }}
                    />
                  </div>
                {/each}
              </div>
            {:else}
              <div
                class="flex-1 flex flex-col items-center justify-center text-center px-8 select-none"
              >
                <span
                  class="material-symbols-outlined text-text-muted text-display-sm mb-4 opacity-40"
                  >edit_note</span
                >
                <h2
                  class="font-headline-md text-headline-md text-text-primary mb-2"
                >
                  {#if openTabs.length > 0 && !activeTabId}
                    No active tab — click a tab above to switch
                  {:else if !activeNotebook}
                    Create or open a notebook to begin
                  {:else if openTabs.length === 0}
                    No pages open
                  {:else}
                    Select or create a page
                  {/if}
                </h2>
                <p class="text-text-muted font-body-md max-w-md mb-5">
                  {#if openTabs.length === 0}
                    Click a page in the sidebar to open it in a tab.
                    Single-click opens a preview; double-click opens a pinned
                    tab.
                  {:else}
                    Silt organizes notes as Notebook › Section › Page. Use the
                    sidebar navigator to create your first notebook, then add a
                    section and a page to start writing.
                  {/if}
                </p>
                {#if activeNotebook && openTabs.length === 0}
                  <div class="flex items-center gap-3">
                    <button
                      onclick={() => {
                        window.dispatchEvent(
                          new CustomEvent('create-page-inline', {
                            detail: { sectionName: activeSection || '' }
                          })
                        )
                      }}
                      class="px-4 py-2 rounded-lg bg-accent-primary-start border border-accent-primary-start/40 text-text-on-accent font-label-sm-bold hover:brightness-110 transition-all cursor-pointer flex items-center gap-2"
                    >
                      <span
                        class="material-symbols-outlined text-icon-lg"
                        aria-hidden="true">note_add</span
                      >
                      Create Page
                    </button>
                    <button
                      onclick={() => {
                        templatePickerMode = 'new-page'
                        showTemplatePicker = true
                      }}
                      class="px-4 py-2 rounded-lg bg-transparent border border-surface-panel-border text-text-primary font-label-sm-bold hover:bg-hover transition-all cursor-pointer flex items-center gap-2"
                    >
                      <span
                        class="material-symbols-outlined text-icon-lg"
                        aria-hidden="true">article</span
                      >
                      New from Template
                    </button>
                  </div>
                {/if}
              </div>
            {/if}
          {:else if activeView === 'tags'}
            <TagsExplorer {selectedTag} />
          {:else if activeView === 'tasks' || activeView === 'calendar' || activeView === 'kanban'}
            <PluginView
              pluginId="silt-tasks"
              {activeNotebook}
              {activeSection}
              {activePage}
              focusBlockId={tasksFocusBlockId}
              focusKey={tasksFocusKey}
            />
          {:else if activeView === 'settings'}
            <SettingsPanel
              bind:section={settingsSection}
              {activeNotebook}
              {activeSection}
              {activePage}
            />
          {:else}
            <!-- Unknown view -->
            <div class="flex-1 p-8 flex flex-col select-none">
              <h1
                class="font-headline-lg text-headline-lg text-text-primary mb-2 capitalize"
              >
                {activeView}
              </h1>
            </div>
          {/if}
        </div>
        <AIChatDrawer />
      </div>
    </div>
  {/if}

  {#if showSearch}
    <SearchModal
      onClose={() => (showSearch = false)}
      onJump={handleSearchResultJump}
      onReplaceInVault={(q) => {
        globalReplaceQuery = q
        showSearch = false
        showGlobalReplace = true
      }}
    />
  {/if}

  {#if showQuickSwitcher}
    <QuickSwitcher
      catalog={navigationCatalog}
      recents={navigationPreferences.recent_pages}
      loading={navigationCatalogLoading}
      error={navigationCatalogError}
      onRetry={() =>
        window.dispatchEvent(new CustomEvent('refresh-navigation'))}
      onOpen={openFromQuickSwitcher}
      onClose={() => (showQuickSwitcher = false)}
    />
  {/if}

  {#if showShortcutHelp}
    <ShortcutHelp onClose={() => (showShortcutHelp = false)} />
  {/if}

  {#if showGlobalReplace}
    <GlobalReplaceModal
      initialQuery={globalReplaceQuery}
      onClose={() => (showGlobalReplace = false)}
    />
  {/if}

  {#if showQuickAdd}
    <!-- Global standalone-task quick-add overlay (#368). Reuses the shared
         QuickAddTask component (same Enter/Escape/busy/error behavior as the
         calendar + kanban surfaces) with an app-level createTask shim over
         CreateStandaloneTask — App.svelte has no plugin ctx. Default TODO,
         no due date. Click-outside / Escape dismisses. -->
    <div
      class="fixed inset-0 z-[200] flex items-start justify-center pt-[20vh] bg-black/40 backdrop-blur-[2px]"
      role="presentation"
      onclick={(e) => {
        // Click on the backdrop itself (not a descendant) dismisses.
        if (e.target === e.currentTarget) showQuickAdd = false
      }}
    >
      <div
        class="w-full max-w-md glass-palette glass-palette-strong border border-surface-modal-border rounded-xl shadow-2xl p-5"
        transition:fade={{ duration: 120 }}
        role="dialog"
        aria-modal="true"
        aria-label="New task"
        tabindex="-1"
      >
        <h2 class="font-headline-lg text-headline-lg text-text-primary mb-3">
          New task
        </h2>
        <QuickAddTask
          createTask={async (opts) => {
            const id = await CreateStandaloneTask(
              opts.title,
              opts.dueDate ?? '',
              opts.status ?? 'TODO'
            )
            // Hand the user off to the Tasks view + focus the new row,
            // so the success of Ctrl+Shift+N is visible without manual
            // navigation. The Tasks view's focusBlockId handles
            // scroll+highlight.
            showQuickAdd = false
            openTasksView(id)
            return id
          }}
          placeholder="Task title — Enter to add, Esc to close"
          keepOpenAfterCreate={false}
          onCreated={() => (showQuickAdd = false)}
          onCancel={() => (showQuickAdd = false)}
        />
      </div>
    </div>
  {/if}

  {#if showTemplatePicker}
    <TemplatePicker
      mode={templatePickerMode}
      notebook={activeNotebook}
      section={activeSection}
      onClose={() => (showTemplatePicker = false)}
      onCreatedPage={handleTemplatePageCreated}
    />
  {/if}

  {#if showSettingsMismatch}
    <div
      class="settings-mismatch-overlay"
      role="alertdialog"
      aria-modal="true"
      aria-labelledby="settings-mismatch-title"
      aria-describedby="settings-mismatch-desc"
      tabindex="-1"
      onkeydown={(e) => {
        if (e.key === 'Escape') showSettingsMismatch = false
      }}
      transition:fade={{ duration: 150 }}
    >
      <div class="settings-mismatch-modal glass-palette-strong">
        <h2 id="settings-mismatch-title">Settings changed</h2>
        <p id="settings-mismatch-desc">
          Silt's vault path or trusted-publishers list has changed since the
          last launch. Confirm this change is intentional. If you did not make
          this change, dismiss and verify your <code>settings.json</code>.
        </p>
        <div class="settings-mismatch-actions">
          <button
            class="secondary"
            onclick={() => (showSettingsMismatch = false)}>Dismiss</button
          >
          <button
            class="primary"
            onclick={async () => {
              try {
                await ConfirmSettingsChange()
                showSettingsMismatch = false
              } catch (e) {
                pushNotification({
                  kind: 'error',
                  message: `Failed to confirm settings change: ${e}`
                })
              }
            }}>Confirm change</button
          >
        </div>
      </div>
    </div>
  {/if}

  {#if showGrantsMigration}
    <div
      class="settings-mismatch-overlay"
      role="alertdialog"
      aria-modal="true"
      aria-labelledby="grants-migration-title"
      aria-describedby="grants-migration-desc"
      tabindex="-1"
      onkeydown={async (e) => {
        if (e.key === 'Escape') {
          try {
            await DeclineGrantsMigration()
          } catch (err) {
            console.error('DeclineGrantsMigration failed:', err)
          }
          showGrantsMigration = false
        }
      }}
      transition:fade={{ duration: 150 }}
    >
      <div class="settings-mismatch-modal glass-palette-strong">
        <h2 id="grants-migration-title">Move plugin permissions</h2>
        <p id="grants-migration-desc">
          Silt is moving plugin permissions to per-host storage so they no
          longer travel with synced vaults. {Object.keys(pendingLegacyGrants)
            .length}{' '}
          plugin(s) have existing permissions in this vault. Confirm to move them,
          or dismiss to re-grant each plugin on first use.
        </p>
        <div class="settings-mismatch-actions">
          <button
            class="secondary"
            onclick={async () => {
              try {
                await DeclineGrantsMigration()
              } catch (e) {
                console.error('DeclineGrantsMigration failed:', e)
              }
              showGrantsMigration = false
            }}>Dismiss</button
          >
          <button
            class="primary"
            onclick={async () => {
              try {
                await ConfirmGrantsMigration(pendingLegacyGrants)
                showGrantsMigration = false
              } catch (e) {
                pushNotification({
                  kind: 'error',
                  message: `Failed to move plugin permissions: ${e}`
                })
              }
            }}>Move permissions</button
          >
        </div>
      </div>
    </div>
  {/if}

  {#if quarantinedLinks.length > 0}
    <div
      class="settings-mismatch-overlay"
      role="alertdialog"
      aria-modal="true"
      aria-labelledby="quarantine-title"
      aria-describedby="quarantine-desc"
      tabindex="-1"
      onkeydown={(e) => {
        if (e.key === 'Escape') quarantinedLinks = []
      }}
      transition:fade={{ duration: 150 }}
    >
      <div class="settings-mismatch-modal glass-palette-strong">
        <h2 id="quarantine-title">Linked notebook moved or tampered</h2>
        <p id="quarantine-desc">
          {#each quarantinedLinks as q (q.id)}
            <strong>{q.display_name}</strong> has moved or been tampered with. Re-link
            it or unlink it.
          {/each}
        </p>
        <div class="settings-mismatch-actions">
          {#each quarantinedLinks as q (q.id)}
            <button
              class="secondary"
              onclick={async () => {
                try {
                  await UnlinkNotebook(q.id)
                  quarantinedLinks = quarantinedLinks.filter(
                    (l) => l.id !== q.id
                  )
                } catch (e) {
                  pushNotification({
                    kind: 'error',
                    message: `Failed to unlink ${q.display_name}: ${e}`
                  })
                }
              }}>Unlink {q.display_name}</button
            >
            <button
              class="primary"
              onclick={async () => {
                try {
                  await UnlinkNotebook(q.id)
                  await PickLinkedNotebook()
                  quarantinedLinks = quarantinedLinks.filter(
                    (l) => l.id !== q.id
                  )
                } catch (e) {
                  pushNotification({
                    kind: 'error',
                    message: `Failed to re-link ${q.display_name}: ${e}`
                  })
                }
              }}>Re-link {q.display_name}</button
            >
          {/each}
        </div>
      </div>
    </div>
  {/if}

  <!-- Plugin rendered-UI surfaces (#117) -->
  <PluginModalHost />
  <PluginStatusBar />
</main>

<ToastContainer />

<style>
  .settings-mismatch-overlay {
    position: fixed;
    inset: 0;
    z-index: 10000;
    display: flex;
    align-items: center;
    justify-content: center;
    background: rgba(0, 0, 0, 0.4);
    backdrop-filter: blur(2px);
  }

  .settings-mismatch-modal {
    max-width: 460px;
    padding: 28px 32px;
    border-radius: 12px;
    border: 1px solid var(--color-surface-modal-border);
    box-shadow: 0 16px 48px rgba(0, 0, 0, 0.4);
  }

  .settings-mismatch-modal h2 {
    margin: 0 0 12px;
    font-size: 1.15rem;
    color: var(--color-text-primary);
  }

  .settings-mismatch-modal p {
    margin: 0 0 20px;
    font-size: 0.9rem;
    line-height: 1.5;
    color: var(--color-text-muted);
  }

  .settings-mismatch-modal code {
    padding: 1px 4px;
    border-radius: 3px;
    background: rgba(255, 255, 255, 0.08);
    font-family: var(--font-mono, monospace);
    font-size: 0.85em;
  }

  .settings-mismatch-actions {
    display: flex;
    justify-content: flex-end;
    gap: 10px;
  }

  .settings-mismatch-actions button {
    padding: 8px 18px;
    border: none;
    border-radius: 8px;
    font-size: 0.875rem;
    cursor: pointer;
    transition: all 150ms var(--transition-standard);
  }

  .settings-mismatch-actions button:focus-visible {
    outline: 2px solid var(--color-accent-primary-start);
    outline-offset: 2px;
  }

  .settings-mismatch-actions .secondary {
    background: transparent;
    color: var(--color-text-muted);
    border: 1px solid var(--color-surface-modal-border);
  }

  .settings-mismatch-actions .secondary:hover {
    background: var(--color-hover);
    color: var(--color-text-primary);
    border-color: var(--color-border-active);
  }

  .settings-mismatch-actions .primary {
    background: var(--color-accent-primary-start);
    color: var(--color-surface-app);
    font-weight: 600;
  }

  .settings-mismatch-actions .primary:hover {
    filter: brightness(1.1);
    box-shadow: 0 0 12px var(--color-accent-primary-glow);
  }
</style>
