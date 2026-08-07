<script lang="ts">
  import { onMount, tick } from 'svelte'
  import type { SearchModalResult } from './components/SearchModal.svelte'
  import {
    resolveBreadcrumbSectionSelection,
    adaptSearchNavigation,
    hasPageLocator,
    resolveSourceNavigationTarget,
    resolveDashboardOpenTarget
  } from './lib/navigationTargets'
  import type { SourceNavigationRef } from './lib/navigationTargets'
  import { coerceIPCError } from './lib/ipcError'
  import {
    IsVaultInitialized,
    InitializeVault,
    CloseVault,
    GetSidebarWidth,
    SetSidebarWidth,
    CreateStandaloneTask,
    RestoreExampleTypes
  } from '../bindings/silt/app.js'
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
  import SettingsMismatchDialog from './components/settings/SettingsMismatchDialog.svelte'
  import GrantsMigrationDialog from './components/settings/GrantsMigrationDialog.svelte'
  import QuarantinedLinksDialog from './components/settings/QuarantinedLinksDialog.svelte'
  import { hasUnsavedTemplateDraft } from './components/settings/templateDraftSession'
  import { shortcutBinding } from './settings/shortcutActions'
  import {
    getSettingsSections,
    resolveSettingsSectionId
  } from './components/settings/settingsSections.svelte'
  import QuickAddTask from './plugins/first-party/silt-tasks/components/QuickAddTask.svelte'
  import { loadPlugins } from './plugins/loader'
  import {
    initConfigHotReload,
    loadConfig,
    settings
  } from './settings/store.svelte'
  import { isDevMode, openInspect } from './lib/devModeInspect'
  import ContextMenu from './components/ContextMenu.svelte'
  import { initEditorTokens } from './settings/editor-tokens.svelte'
  import { initThemes } from './theme/store.svelte'
  import { initTemplates } from './templates/store.svelte'
  import TemplatePicker from './templates/TemplatePicker.svelte'
  import { createSettingsDialogs } from './shell/useSettingsDialogs.svelte'
  import { createGlobalHotkeyDispatch } from './shell/useGlobalHotkeyDispatch.svelte'
  import { shouldApplyFormatBold } from './shell/globalHotkeys'
  import { createStartupEvents } from './shell/useStartupEvents.svelte'
  import { ASSIGN_PAGE_TYPE_EVENT } from './shell/pageTypeEvents'
  import { effectiveHotkeys } from './settings/shortcutActions'
  import { findBarState } from './lib/editor/search/findBarState.svelte'
  import { editorKey, getEditor } from './lib/editor/editorRegistry.svelte'
  import SidebarResizeHandle from './components/SidebarResizeHandle.svelte'
  import PluginModalHost from './components/PluginModalHost.svelte'
  import { getAIAvailability } from './plugins/shared/ai-chat/availability'
  import AIChatDrawer from './plugins/shared/ai-chat/AIChatDrawer.svelte'
  import TaskEditorModalHost from './components/editor/TaskEditorModalHost.svelte'
  import {
    aiChatDrawer,
    toggleAIChatDrawer
  } from './plugins/shared/ai-chat/drawer.svelte'
  import PluginStatusBar from './components/PluginStatusBar.svelte'
  import DateGlance from './components/DateGlance.svelte'
  import PageTypePill from './properties/PageTypePill.svelte'
  import PropertiesPanel from './properties/PropertiesPanel.svelte'
  import PropertiesEditModal from './properties/PropertiesEditModal.svelte'
  import TypeEditorDialog from './properties/TypeEditorDialog.svelte'
  import { createPageTypeController } from './properties/pageTypeState.svelte'
  import TypeDashboard from './dashboards/TypeDashboard.svelte'
  import { toggleDateGlance } from './lib/dateGlanceState.svelte'
  import {
    getActiveEditor,
    getLastActiveEditor
  } from './lib/editor/activeEditor.svelte'
  import {
    shortcutHelp,
    toggleShortcutHelp,
    closeShortcutHelp
  } from './lib/shortcutHelpState.svelte'
  import { setActiveLocation } from './plugins/location.svelte'
  import { clearSelectionFocus } from './plugins/ui-location'
  import ToastContainer from './components/ToastContainer.svelte'
  import Onboarding from './components/Onboarding.svelte'
  import { pushNotification } from './notifications/store.svelte'
  import {
    initStartupUpdateCheck,
    disposeUpdateStore
  } from './updates/store.svelte'
  import { type OpenPageMode } from './lib/tabs'
  import { createTabManager } from './lib/tabs/useTabManager.svelte'
  import { nextView } from './lib/viewCycle'
  import {
    flattenNavigation,
    notebookNavigationMetadata,
    type NotebookNavigationMetadata,
    type NavigationCatalogItem
  } from './lib/navigationCatalog'
  import type {
    NavigationPageRef,
    NavigationPreferences
  } from './lib/sidebar/types'
  import { routeJumpTarget } from './lib/standaloneTasksNav'

  let isInitialized = $state(false)
  let loading = $state(true)

  // Tab manager (#142, #768). Owns openTabs/activeTabId, the per-notebook
  // displayedTabs derivation, the `.silt-stray` guard, the open-tabs provider,
  // and the open/select/close/promote/reorder/cycle/view-mode handlers. Deps
  // getters read the navigation triple + settings lazily (the controller's
  // $effect/$derived evaluate after init, by which point the triple exists).
  const tabManager = createTabManager({
    getActiveNotebook: () => activeNotebook,
    setActiveNotebook: (nb) => (activeNotebook = nb),
    setActiveSection: (sec) => (activeSection = sec),
    setActivePage: (pg) => (activePage = pg),
    getSettings: () => settings.config ?? {},
    confirmTemplateTransition,
    openTasksView
  })

  // Navigation state (3-level: notebook > section > page). Kept in sync with
  // the active tab; also set directly by onSelectNotebook/onSelectSection
  // (sidebar browsing context without opening a page).
  let activeNotebook = $state('')
  let activeSection = $state('')
  let activePage = $state('')
  let activeView = $state('notes')
  const views = [
    { id: 'notes', label: 'Notes', icon: 'description' },
    { id: 'backlinks', label: 'Backlinks', icon: 'hub' },
    { id: 'tags', label: 'Tags', icon: 'label' },
    { id: 'tasks', label: 'Tasks', icon: 'checklist' }
  ]
  let selectedTag = $state('')

  // Shell state
  let sidebarCollapsed = $state(false)
  let sidebarWidth = $state(256)
  // True while the resize handle is pointer-dragging so Sidebar can disable
  // its width transition (avoids laggy animated resize).
  let sidebarDragging = $state(false)
  let manuallyCollapsed = $state(false)

  // Dev Mode–only Inspect on empty content chrome (#683) — no other menu there.
  let emptyChromeMenu = $state<{
    open: boolean
    anchor: { x: number; y: number } | null
  }>({ open: false, anchor: null })

  function openEmptyChromeInspectMenu(e: MouseEvent): void {
    if (!isDevMode()) return
    e.preventDefault()
    emptyChromeMenu = { open: true, anchor: { x: e.clientX, y: e.clientY } }
  }

  function closeEmptyChromeMenu(): void {
    emptyChromeMenu = { open: false, anchor: null }
  }

  // Sync active navigation to the reactive plugin location (#69). Plugins
  // read ctx.activeNotebook/Section/Page via live getters backed by this state.
  $effect(() => {
    setActiveLocation(activeNotebook, activeSection, activePage)
  })

  // --- Tab management (#142) -----------------------------------------------
  // openPage / handlers / persistence / the open-tabs provider all live in the
  // tabManager controller above.

  function confirmTemplateTransition(): boolean {
    return (
      !(
        activeView === 'settings' &&
        settingsSection === 'templates' &&
        hasUnsavedTemplateDraft()
      ) || window.confirm('Leave Templates? Your draft will be kept for later.')
    )
  }

  function selectView(view: string): void {
    if (view !== activeView && !confirmTemplateTransition()) return
    activeView = view
  }

  function showBacklinks(): void {
    activeView = 'backlinks'
    sidebarCollapsed = false
    manuallyCollapsed = false
  }

  function openSectionContext(section: string): void {
    const selection = resolveBreadcrumbSectionSelection(
      activeSection,
      activePage,
      section
    )
    activeSection = selection.section
    activePage = selection.page
  }

  function openFromQuickSwitcher(
    item: NavigationCatalogItem,
    mode: OpenPageMode
  ): void {
    tabManager.openPage(item, mode)
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

  let showSearch = $state(false)
  let showQuickSwitcher = $state(false)
  let navigationCatalog = $state<NavigationCatalogItem[]>([])
  let navigationNotebookMetadata = $state<
    Record<string, NotebookNavigationMetadata>
  >({})
  let navigationPreferences = $state<NavigationPreferences>({
    expanded_sections: [],
    recent_pages: [],
    favorites: [],
    sidebar_view: 'tree'
  })
  let navigationCatalogLoading = $state(true)
  let navigationCatalogError = $state('')
  let activeNotebookMetadata = $derived(
    navigationNotebookMetadata[activeNotebook]
  )

  // Typed-notes controller. Reads the active locator via live closures so it
  // stays in sync with navigation; App drives re-fetching via the $effect on
  // the locator + view below. The controller owns the `types:changed`
  // subscription (attached in onMount) and the panel-open flag.
  const pageType = createPageTypeController({
    getLocator: () => ({
      notebook: activeNotebook,
      section: activeSection,
      page: activePage
    })
  })
  // Re-fetch the active page's type + properties whenever the user navigates
  // (the controller reads the locator via the closure above). Reading the
  // three locator vars + view inside the effect wires Svelte's dependency
  // tracking; the panel only applies to note pages.
  $effect(() => {
    if (activeView !== 'notes' && activeView !== 'backlinks') return
    // Touch all three so a section-only or page-only change still re-runs.
    void activeNotebook
    void activeSection
    void activePage
    void pageType.refresh()
  })

  // Per-type dashboard view. Reached from the type strip's "View all" action;
  // exits via its Back button or by opening a page (which returns to notes).
  let dashboardType = $state('')
  function openTypeDashboard(typeId: string): void {
    if (!typeId) return
    dashboardType = typeId
    activeView = 'dashboard'
  }

  // In-app type editor modal. Mounted at the shell level so the same dialog
  // serves the dashboard's "New type" header button, the dashboard's empty
  // state, and the PropertiesPanel type-menu dead-end — one surface for
  // "create a type from scratch". SaveType emits types:changed → the panel
  // and dashboard refresh themselves, so onClose just closes.
  let typeEditorOpen = $state(false)
  function openTypeEditor(): void {
    typeEditorOpen = true
  }

  // Restore the shipped example types pack. Idempotent server-side:
  // a type whose id exists is left untouched, so this is safe to repeat. The
  // returned ids distinguish "newly restored" from "already present" so the
  // toast reflects what actually happened.
  async function restoreExampleTypes(): Promise<void> {
    try {
      const ids = (await RestoreExampleTypes()) as string[] | null
      if (ids && ids.length > 0) {
        const names = ids
          .map((id) => id.charAt(0).toUpperCase() + id.slice(1))
          .join(', ')
        pushNotification({
          kind: 'success',
          message: `Restored ${names}.`
        })
      } else {
        pushNotification({
          kind: 'info',
          message: 'Example types are already present.'
        })
      }
    } catch (e) {
      pushNotification({
        kind: 'error',
        message: coerceIPCError(e).message
      })
    }
  }
  function openDashboardPage(locator: {
    source: string
    notebook: string
    section: string
    page: string
  }): void {
    // The tab system identifies pages by notebook/section/path only (no source
    // field), so a linked-notebook row colliding with a vault page would open
    // the wrong tab. Gate linked-source rows until tabs carry source.
    const target = resolveDashboardOpenTarget(locator)
    if (target.kind === 'blocked') {
      pushNotification({ kind: 'info', message: target.reason })
      return
    }
    tabManager.openPage(target.ref, 'preview')
    activeView = 'notes'
  }
  // Sidebar right-click "Page properties" → open the properties panel + arm
  // its type menu on the chosen page. Same linked-source gate as
  // openDashboardPage (the tab system has no source field). If the target is
  // already active, skip the tab churn and just open + arm. Mirrors the /type
  // slash command's compose pattern (App.svelte:onAssignType).
  async function handleTypePageTarget(ref: NavigationPageRef): Promise<void> {
    const source = navigationNotebookMetadata[ref.notebook]?.source ?? 'vault'
    const target = resolveDashboardOpenTarget({
      source,
      notebook: ref.notebook,
      section: ref.section,
      page: ref.page
    })
    if (target.kind === 'blocked') {
      pushNotification({ kind: 'info', message: target.reason })
      return
    }
    const isActive =
      activeNotebook === ref.notebook &&
      activeSection === ref.section &&
      activePage === ref.page
    if (!isActive) {
      tabManager.openPage(ref, 'pin')
      await tick()
    }
    pageType.open()
    pageType.requestTypeMenu()
  }
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
  // Settings / quarantine dialog controller (#768). Owns the three modal
  // overlays' open state + their IPC action handlers. Driven by the
  // startup-events handlers (open*) and the dialog props (close*/confirm*).
  const settingsDialogs = createSettingsDialogs()

  // Focused block ancestry path highlighting
  let activeFocusedBlockAncestors = $state<string[]>([])
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

  // Global-hotkey dispatch (#768). Owns the window 'keydown' listener that
  // resolves a config-driven chord and switch-dispatches the action to App's
  // shell state. Every side effect is a closure over App's $state / handlers;
  // attach()/detach() wire into onMount init + cleanup.
  const hotkeyDispatch = createGlobalHotkeyDispatch({
    getHotkeys: () => effectiveHotkeys(settings.config?.hotkeys ?? {}),
    getHasDisplayedTabs: () => tabManager.displayedTabs.length > 0,
    getActiveTabId: () => tabManager.activeTabId,
    isActiveTabDisplayed: () =>
      tabManager.displayedTabs.some((t) => t.id === tabManager.activeTabId),
    getSidebarCollapsed: () => sidebarCollapsed,
    toggleSearch: () => (showSearch = !showSearch),
    toggleQuickSwitcher: () => (showQuickSwitcher = !showQuickSwitcher),
    toggleGlobalReplace: () => (showGlobalReplace = !showGlobalReplace),
    toggleQuickAdd: () => (showQuickAdd = !showQuickAdd),
    setSidebarCollapsed: (collapsed) => {
      sidebarCollapsed = collapsed
      manuallyCollapsed = collapsed
    },
    toggleShortcutHelp: () => toggleShortcutHelp(),
    toggleDateGlance: () => toggleDateGlance(getActiveEditor()),
    openFind: () => findBarState.openFind(),
    openReplace: () => findBarState.openReplace(),
    cycleView: () => cycleView(),
    openTemplatePicker: () => {
      templatePickerMode = 'new-page'
      showTemplatePicker = !showTemplatePicker
    },
    requestNavigationCreation: (kind) => void requestNavigationCreation(kind),
    openSettings: () => openSettings(),
    toggleViewMode: (tabId) => tabManager.handleToggleViewMode(tabId),
    togglePropertiesPanel: () => pageType.toggle(),
    // Mirrors the {#if} that mounts <PropertiesPanel> (the editor-tab view);
    // see useGlobalHotkeyDispatch — toggling anywhere else is a silent no-op.
    isPropertiesPanelAvailable: () =>
      activeView === 'notes' || activeView === 'backlinks',
    closeTab: (tabId) => tabManager.handleCloseTab(tabId),
    cycleTab: (dir) => tabManager.handleCycleTab(dir),
    // Ctrl+B is bold everywhere an editor is the relevant surface. The resolver
    // suppressed this when the editor was focused (ProseMirror owns the in-editor
    // path); here the editor is not focused, so recover an editor and apply bold.
    // Gated to notes/backlinks (mirrors isPropertiesPanelAvailable above) and to
    // no-dialog-open so a Ctrl+B inside any modal is a clean no-op. The dialog
    // check is generic — focus lives inside the open dialog, so
    // document.activeElement.closest('dialog, [role="dialog"]') matches every
    // modal (search, quick switcher, replace, quick add, template picker, type
    // editor, shortcut help, plus NamePrompt/Confirm/Choice/SettingsMismatch)
    // without enumerating each overlay flag. The recovered editor is also
    // checked against hidden background tabs: getLastActiveEditor() is a global
    // that survives blur, so after a tab switch (or the Ctrl+Alt+→/← cycle
    // chords, which leave the new tab's editor unfocused) it can still point at
    // the page the user left. A display:none tab panel makes the editor's
    // view.dom non-visible (offsetParent === null) — guard against that so
    // Ctrl+B never mutates a page the user isn't looking at. The decision itself
    // is delegated to the pure shouldApplyFormatBold predicate (shell/
    // globalHotkeys.ts) so the gating contract is unit-tested.
    applyFormatBold: () => {
      const editor = getActiveEditor() ?? getLastActiveEditor()
      if (!editor || editor.isDestroyed) return
      const dom = editor.view?.dom as HTMLElement | null
      const isAnyDialogOpen = !!document.activeElement?.closest(
        'dialog, [role="dialog"]'
      )
      if (
        !shouldApplyFormatBold({
          activeView,
          isAnyDialogOpen,
          editorVisible: dom ? dom.offsetParent !== null : false
        })
      )
        return
      editor.chain().focus().toggleBold().run()
    }
  })

  // Startup-events controller (#768). Owns every onMount-registered listener
  // (Wails Events.On + startup-lifecycle window addEventListener), the
  // dispatchStartupEvent replay router, and the MarkFrontendReady backlog
  // drain. attach()/dispose() wire into onMount init + cleanup. Deps are lazy
  // closures over App's $state + App-defined handlers.
  const startup = createStartupEvents({
    getActiveNotebook: () => activeNotebook,
    getActiveSection: () => activeSection,
    getActivePage: () => activePage,
    setActiveNotebook: (nb) => (activeNotebook = nb),
    setActiveSection: (sec) => (activeSection = sec),
    setActivePage: (pg) => (activePage = pg),
    setActiveView: (view) => (activeView = view),
    getSettings: () => settings.config,
    setSettingsSection: (id) => (settingsSection = id),
    setShowSearch: (v) => (showSearch = v),
    setShowQuickAdd: (v) => (showQuickAdd = v),
    getShowTemplatePicker: () => showTemplatePicker,
    setShowTemplatePicker: (v) => (showTemplatePicker = v),
    setTemplatePickerMode: (m) => (templatePickerMode = m),
    setSelectedTag: (t) => (selectedTag = t),
    getSidebarCollapsed: () => sidebarCollapsed,
    setSidebarCollapsed: (collapsed) => {
      sidebarCollapsed = collapsed
      manuallyCollapsed = collapsed
    },
    setSearchTargetHeading: (h) => (searchTargetHeading = h),
    setSearchTargetKey: (k) => (searchTargetKey = k),
    getNavigationCatalog: () => navigationCatalog,
    settingsDialogs,
    tabManager,
    openSettings,
    openTasksView,
    handleSwitchVault,
    handleMenuSave,
    handleSearchJump
  })

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
    void checkInit()
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
    void tabManager.loadPersistedTabs()
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

    hotkeyDispatch.attach()
    startup.attach()
    const disposePageType = pageType.attach()

    // /type slash command → open the properties panel + its type menu. The
    // panel's existing type-menu logic does the untyped-vs-typed branching
    // (direct assign vs Turn-into dialog), so this just opens both surfaces.
    const onAssignType = (): void => {
      if (!activeNotebook || !activePage) return
      pageType.open()
      pageType.requestTypeMenu()
    }
    window.addEventListener(ASSIGN_PAGE_TYPE_EVENT, onAssignType)

    return () => {
      hotkeyDispatch.detach()
      startup.dispose()
      disposeEditorTokens()
      disposeThemes()
      disposeTemplates()
      disposeUpdateStore()
      disposePageType()
      window.removeEventListener(ASSIGN_PAGE_TYPE_EVENT, onAssignType)
      // Flush any pending tab-state persistence so the user's last
      // change survives a component unmount / app close (#142 hardening).
      tabManager.flushPendingPersist()
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
        void tabManager.loadPersistedTabs()
        window.dispatchEvent(new CustomEvent('refresh-navigation'))
      }
    } catch (e) {
      alert('Failed to initialize vault: ' + String(e))
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
      tabManager.resetTabs()
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
    locator: SourceNavigationRef,
    date: string,
    blockId: string
  ) {
    const { notebook, section, page } = locator
    // Defense in depth for incomplete locators (#877). Toast lives on the
    // navigate-to-block/page bus edge so we only warn here — no second toast.
    if (!hasPageLocator(locator)) {
      console.warn('handleSearchJump ignored: missing notebook/page', {
        notebook,
        section,
        page,
        blockId
      })
      return
    }
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
    const activeTab = tabManager.openTabs.find(
      (t) => t.id === tabManager.activeTabId
    )
    const isSamePage =
      activeTab &&
      activeTab.notebook === notebook &&
      activeTab.section === section &&
      activeTab.page === page
    tabManager.openPage(locator, isSamePage ? 'activate-only' : 'preview', {
      fileDate: date,
      blockId
    })
    activeView = 'notes'
    searchTargetBlockId = blockId
    searchTargetHeading = ''
    searchTargetKey = `${date}:${blockId}:${Date.now()}`
  }

  // Called by the TemplatePicker when a new page is created from a template.
  // Navigates to the freshly-created page (the reactive cascade loads it in
  // the editor) and refreshes the sidebar tree so the new page appears.
  function handleTemplatePageCreated(page: string): void {
    tabManager.openPage(
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

  function handleSearchResultJump(res: SearchModalResult): void {
    const jump = adaptSearchNavigation(res)
    const locator = resolveSourceNavigationTarget(
      navigationCatalog,
      jump.locator
    )
    handleSearchJump(locator, jump.date, jump.blockId)
  }

  // Whether an editor-bearing view has a complete page target. Backlinks owns
  // the sidebar only, so the active note remains mounted beside it.
  // With tabs (#142), also requires an active tab so closing the last tab
  // returns to the blank view. displayedTabs ensures per-notebook scoping.
  let notesReady = $derived(
    (activeView === 'notes' || activeView === 'backlinks') &&
      !!activeNotebook &&
      !!activePage &&
      !!tabManager.activeTabId &&
      tabManager.displayedTabs.length > 0
  )

  // Switch to the Settings view and select a section (general/editor/
  // appearance/…). Settings is a view, not a tab (#511 rework): no tab state is
  // touched, and the navigation triple is left intact so the user returns to
  // their page when they leave Settings. The section persists across opens so
  // re-entering Settings returns the user to the panel they last visited.
  function openSettings(section: string = 'general') {
    if (
      activeView === 'settings' &&
      settingsSection === 'templates' &&
      section !== 'templates' &&
      !confirmTemplateTransition()
    )
      return
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
      onAIClick={getAIAvailability().drawerAvailable
        ? () => toggleAIChatDrawer()
        : undefined}
      aiOpen={aiChatDrawer.open}
    >
      {#if activeView === 'notes' || activeView === 'backlinks'}
        <TabStrip
          tabs={tabManager.displayedTabs}
          activeTabId={tabManager.activeTabId}
          onSelectTab={tabManager.handleSelectTab}
          onCloseTab={tabManager.handleCloseTab}
          onPromoteTab={tabManager.handlePromoteTab}
          onReorderTab={tabManager.handleReorderTab}
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
                  selectView(v.id)
                  if (activeView !== v.id) return
                  sidebarCollapsed = false
                  manuallyCollapsed = false
                }
              }}
              class="relative w-9 h-9 rounded-lg flex items-center justify-center transition-all cursor-pointer border-none bg-transparent hover:bg-hover hover:scale-105 active:scale-95 group focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start motion-reduce:transform-none motion-reduce:transition-none"
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
          class="relative w-9 h-9 rounded-lg flex items-center justify-center transition-all cursor-pointer border-none bg-transparent hover:bg-hover hover:scale-105 active:scale-95 group focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start motion-reduce:transform-none motion-reduce:transition-none"
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
            style:color="var(--color-nav-icon-settings)">settings</span
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
            title={`Show sidebar${shortcutBinding('toggle_sidebar', settings.config?.hotkeys ?? {}) ? ` (${shortcutBinding('toggle_sidebar', settings.config?.hotkeys ?? {})})` : ''}`}
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
          onSelectNotebook={tabManager.selectNotebookContext}
          onSelectSection={(sec) => (activeSection = sec)}
          onSelectPage={(nb, sec, pg) => {
            // Single-click opens in preview mode (industry-standard parity, #142).
            tabManager.openPage(
              { notebook: nb, section: sec, page: pg },
              'preview'
            )
          }}
          onPinPage={(nb, sec, pg) => {
            // Double-click / middle-click opens a pinned tab (#142).
            tabManager.openPage({ notebook: nb, section: sec, page: pg }, 'pin')
          }}
          onTypePageTarget={handleTypePageTarget}
          onSelectView={selectView}
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
            tabManager.pageMoved(nb, fromSection, toSection, page)
            if (
              activeNotebook === nb &&
              activePage === page &&
              activeSection === fromSection
            ) {
              activeSection = toSection
            }
          }}
        />

        {#if !sidebarCollapsed}
          <SidebarResizeHandle
            width={sidebarWidth}
            onWidthChange={handleSidebarWidthChange}
            onWidthCommit={handleSidebarWidthCommit}
            onDragStart={handleSidebarDragStart}
            onDragEnd={handleSidebarDragEnd}
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
                '-'} tab={tabManager.activeTabId || '-'} dt={tabManager
                .displayedTabs.length} nr={notesReady}
            </div>
          {/if}
          {#if activeView === 'notes' || activeView === 'backlinks'}
            <PageBreadcrumb
              notebook={activeNotebook}
              section={activeSection}
              page={activePage}
              {activeView}
              linked={activeNotebookMetadata?.linked ?? false}
              disconnected={activeNotebookMetadata?.disconnected ?? false}
              onSelectNotebook={tabManager.selectNotebookContext}
              onSelectSection={openSectionContext}
              onOpenPage={() =>
                tabManager.openPage(
                  {
                    notebook: activeNotebook,
                    section: activeSection,
                    page: activePage
                  },
                  'activate-only'
                )}
              onOpenBacklinks={showBacklinks}
            >
              {#snippet meta()}
                <PageTypePill
                  info={pageType.info}
                  heroValue={pageType.heroValue}
                  onOpen={pageType.open}
                  onViewAll={() => openTypeDashboard(pageType.info.type.id)}
                  onOpenWithTypeMenu={pageType.requestTypeMenu}
                />
              {/snippet}
            </PageBreadcrumb>
            {#if notesReady}
              <div
                id="silt-tabpanel"
                role="tabpanel"
                aria-labelledby="silt-tab-{tabManager.activeTabId}"
                class="flex-1 min-h-0 flex flex-col overflow-hidden"
              >
                {#each tabManager.displayedTabs as tab (tab.id)}
                  <div
                    class="flex-1 min-h-0 flex flex-col overflow-hidden"
                    style:display={tab.id === tabManager.activeTabId
                      ? 'flex'
                      : 'none'}
                  >
                    <VirtualScrollContainer
                      source={navigationNotebookMetadata[tab.notebook]
                        ?.source ?? 'vault'}
                      notebook={tab.notebook}
                      section={tab.section}
                      page={tab.page}
                      viewMode={tab.viewMode}
                      onToggleViewMode={() =>
                        tabManager.handleToggleViewMode(tab.id)}
                      isActive={tab.id === tabManager.activeTabId}
                      targetBlockId={tab.id === tabManager.activeTabId
                        ? searchTargetBlockId
                        : ''}
                      targetHeading={tab.id === tabManager.activeTabId
                        ? searchTargetHeading
                        : ''}
                      targetKey={tab.id === tabManager.activeTabId
                        ? searchTargetKey
                        : ''}
                      activeFocusedBlockAncestors={tab.id ===
                      tabManager.activeTabId
                        ? activeFocusedBlockAncestors
                        : []}
                      onBlockFocus={tab.id === tabManager.activeTabId
                        ? handleBlockFocus
                        : undefined}
                      onBlockBlur={tab.id === tabManager.activeTabId
                        ? handleBlockBlur
                        : undefined}
                      onPageRenamed={(newName) => {
                        // Update the tab's page name AND the active triple.
                        tabManager.renameTab(tab.id, newName)
                        if (tab.id === tabManager.activeTabId)
                          activePage = newName
                      }}
                      onFirstEdit={tab.preview
                        ? () => tabManager.handlePromoteTab(tab.id)
                        : undefined}
                      onSaveStateChange={(s) => {
                        // Surface the editor's save state on the tab header
                        // so it's visible from any tab (#167, #546).
                        tabManager.setTabSaveState(tab.id, s)
                      }}
                    />
                  </div>
                {/each}
              </div>
              <PropertiesPanel
                open={pageType.panelOpen}
                info={pageType.info}
                values={pageType.values}
                mismatched={pageType.mismatched}
                error={pageType.error}
                loading={pageType.loading}
                types={pageType.types}
                typesLoading={pageType.typesLoading}
                typeMenuRequest={pageType.typeMenuRequest}
                core={pageType.core}
                onCommitCore={pageType.commitCore}
                locator={{
                  notebook: activeNotebook,
                  section: activeSection,
                  page: activePage
                }}
                onClose={pageType.close}
                onOpenModal={pageType.openModal}
                onChanged={pageType.refresh}
                onMismatched={pageType.setMismatched}
                onError={pageType.setError}
                onCreateType={openTypeEditor}
                onRestoreExamples={restoreExampleTypes}
              />
              <PropertiesEditModal
                open={pageType.modalOpen}
                info={pageType.info}
                values={pageType.values}
                mismatched={pageType.mismatched}
                error={pageType.error}
                loading={pageType.loading}
                types={pageType.types}
                typesLoading={pageType.typesLoading}
                typeMenuRequest={pageType.typeMenuRequest}
                core={pageType.core}
                onCommitCore={pageType.commitCore}
                locator={{
                  notebook: activeNotebook,
                  section: activeSection,
                  page: activePage
                }}
                onClose={pageType.closeModal}
                onChanged={pageType.refresh}
                onMismatched={pageType.setMismatched}
                onError={pageType.setError}
                onCreateType={openTypeEditor}
                onRestoreExamples={restoreExampleTypes}
              />
            {:else}
              <!-- svelte-ignore a11y_no_static_element_interactions -->
              <!-- Dev Mode Inspect only; no native menu on empty chrome (#683). -->
              <div
                class="flex-1 flex flex-col items-center justify-center text-center px-8 select-none"
                oncontextmenu={openEmptyChromeInspectMenu}
              >
                <span
                  class="material-symbols-outlined text-text-muted text-display-sm mb-4 opacity-40"
                  >edit_note</span
                >
                <h2
                  class="font-headline-md text-headline-md text-text-primary mb-2"
                >
                  {#if tabManager.openTabs.length > 0 && !tabManager.activeTabId}
                    No active tab — click a tab above to switch
                  {:else if !activeNotebook}
                    Create or open a notebook to begin
                  {:else if tabManager.openTabs.length === 0}
                    No pages open
                  {:else}
                    Select or create a page
                  {/if}
                </h2>
                <p class="text-text-muted font-body-md max-w-md mb-5">
                  {#if tabManager.openTabs.length === 0}
                    Click a page in the sidebar to open it in a tab.
                    Single-click opens a preview; double-click opens a pinned
                    tab.
                  {:else}
                    Silt organizes notes as Notebook › Section › Page. Use the
                    sidebar navigator to create your first notebook, then add a
                    section and a page to start writing.
                  {/if}
                </p>
                {#if activeNotebook && tabManager.openTabs.length === 0}
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
          {:else if activeView === 'dashboard'}
            <TypeDashboard
              typeName={dashboardType}
              onOpenPage={openDashboardPage}
              onBack={() => (activeView = 'notes')}
              onCreateType={openTypeEditor}
              onRestoreExamples={restoreExampleTypes}
            />
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
        <TaskEditorModalHost />
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

  {#if shortcutHelp.open}
    <ShortcutHelp onClose={closeShortcutHelp} />
  {/if}

  {#if emptyChromeMenu.open && isDevMode()}
    <ContextMenu
      open={emptyChromeMenu.open}
      anchor={emptyChromeMenu.anchor}
      onClose={closeEmptyChromeMenu}
      ariaLabel="Developer actions"
      menuId="empty-chrome-context-menu"
    >
      <button
        type="button"
        role="menuitem"
        onclick={() => {
          closeEmptyChromeMenu()
          void openInspect()
        }}
      >
        <span class="material-symbols-outlined text-icon-md" aria-hidden="true"
          >bug_report</span
        >
        Inspect
      </button>
    </ContextMenu>
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

  <SettingsMismatchDialog
    open={settingsDialogs.showSettingsMismatch}
    onClose={settingsDialogs.closeSettingsMismatch}
    onConfirm={settingsDialogs.confirmSettingsMismatch}
  />

  <TypeEditorDialog
    open={typeEditorOpen}
    onClose={() => (typeEditorOpen = false)}
  />

  <GrantsMigrationDialog
    open={settingsDialogs.showGrantsMigration}
    pendingLegacyGrants={settingsDialogs.pendingLegacyGrants}
    onDecline={settingsDialogs.declineGrantsMigration}
    onConfirm={settingsDialogs.confirmGrantsMigration}
  />

  <QuarantinedLinksDialog
    open={settingsDialogs.quarantinedLinks.length > 0}
    quarantinedLinks={settingsDialogs.quarantinedLinks}
    onClose={settingsDialogs.clearQuarantinedLinks}
    onUnlink={settingsDialogs.handleUnlinkNotebook}
    onRelink={settingsDialogs.handleRelinkNotebook}
  />

  <!-- Plugin rendered-UI surfaces (#117) -->
  <PluginModalHost />
  <PluginStatusBar />
  <DateGlance />
</main>

<ToastContainer />
