<script lang="ts">
  import { SvelteSet } from 'svelte/reactivity'
  import { onMount, tick } from 'svelte'
  import { Events } from '@wailsio/runtime'
  import SidebarSection from './SidebarSection.svelte'
  import SidebarQuickAccess from './SidebarQuickAccess.svelte'
  import PluginSidebarPanels from './PluginSidebarPanels.svelte'
  import TagSidebarPanel from './TagSidebarPanel.svelte'
  import BacklinksSidebarPanel from './BacklinksSidebarPanel.svelte'
  import { sortByName } from '../lib/sidebar/navOrder'
  import { DragDropManager } from '../lib/sidebar/useDragDrop'
  import { useNavLoader } from '../lib/sidebar/useNavLoader.svelte'
  import { useNavCrud } from '../lib/sidebar/useNavCrud.svelte'
  import { useSidebarContextMenu } from '../lib/sidebar/useSidebarContextMenu.svelte'
  import type {
    NavigationPageRef,
    NavigationPreferences,
    NavigationTree
  } from '../lib/sidebar/types'
  import {
    pageNodeId,
    sectionNodeId,
    visibleTreeNodes
  } from '../lib/sidebar/navTree'
  import {
    expandActiveAncestors,
    expandedPathsForNotebook,
    locatorKey
  } from '../lib/sidebar/navigationPreferences'
  import ContextMenu from './ContextMenu.svelte'
  import NamePromptDialog from './NamePromptDialog.svelte'
  import { isDevMode, openInspect } from '../lib/devModeInspect'
  import SettingsNav from './settings/SettingsNav.svelte'
  import { settings } from '../settings/store.svelte'
  import { shortcutBinding } from '../settings/shortcutActions'

  import type { PluginContext, PluginManifest } from '../plugins/sdk'
  import {
    getPluginSidebar,
    pluginIdForView
  } from '../plugins/getPluginSidebar'
  import { getSessionToken } from '../plugins/loader'
  import { makePluginContext } from '../plugins/context'
  import { loadedPlugins } from '../plugins/store.svelte'

  interface Props {
    activeNotebook: string
    activeSection: string
    activePage: string
    activeView: string
    selectedTag?: string
    /** Active settings section id (general/editor/…). Owned by App; the
     *  settings sidebar nav binds it. */
    settingsSection?: string
    collapsed: boolean
    sidebarWidth?: number
    sidebarDragging?: boolean
    onSelectNotebook: (notebook: string) => void
    onSelectSection: (section: string) => void
    onSelectPage: (notebook: string, section: string, page: string) => void
    onPinPage: (notebook: string, section: string, page: string) => void
    onSelectView: (view: string) => void
    onNavigationLoaded?: (tree: NavigationTree) => void
    onNavigationPreferencesLoaded?: (preferences: NavigationPreferences) => void
    onNavigationStatus?: (loading: boolean, error: string) => void
    onPageMoved?: (
      notebook: string,
      fromSection: string,
      toSection: string,
      page: string
    ) => void
  }

  let {
    activeNotebook = $bindable(),
    activeSection = $bindable(),
    activePage = $bindable(),
    activeView = $bindable(),
    selectedTag = $bindable(''),
    settingsSection = $bindable('general'),
    collapsed = $bindable(),
    sidebarWidth = 256,
    sidebarDragging = false,
    onSelectNotebook,
    onSelectSection,
    onSelectPage,
    onPinPage,
    onSelectView,
    onNavigationLoaded,
    onNavigationPreferencesLoaded,
    onNavigationStatus,
    onPageMoved
  }: Props = $props()

  // Active-triple read/write handed to the composables. The bindable props
  // stay here (the template renders against them), but loading/reconciliation
  // mutates them through this single channel.
  const getActive = () => ({
    notebook: activeNotebook,
    section: activeSection,
    page: activePage
  })
  const setActive = (patch: {
    notebook?: string
    section?: string
    page?: string
  }) => {
    if (patch.notebook !== undefined) activeNotebook = patch.notebook
    if (patch.section !== undefined) activeSection = patch.section
    if (patch.page !== undefined) activePage = patch.page
  }

  // Resolve the active view's plugin sidebar (#321). Mirrors the lookup
  // PluginView.svelte does for the main view: read the live plugin entry
  // from the reactive store, build the context with the session token the
  // loader registered. The render branch uses the resolved `SidebarCmp`
  // and passes it `{ ctx, manifest }` — the same shape PluginView passes.
  //
  // Gating on `loadedPlugins.loadersReady` re-runs this derived when the
  // flag flips back to true AFTER vault:closing's clear→re-register cycle,
  // so getSessionToken(id) captures the FRESH token instead of an empty
  // one captured mid-teardown (#326 item 5).
  let pluginSidebarEntry = $derived(getPluginSidebar(activeView))
  let SidebarCmp = $derived(pluginSidebarEntry?.sidebarComponent)
  let pluginSidebarCtx: PluginContext | null = $derived.by(() => {
    if (!loadedPlugins.loadersReady) return null // suspend during vault switch
    const id = pluginIdForView(activeView)
    if (!id) return null
    return makePluginContext(id, getSessionToken(id))
  })
  let pluginSidebarManifest: PluginManifest | null = $derived(
    pluginSidebarEntry?.manifest ?? null
  )

  let showNotebookDropdown = $state(false)

  // --- composables -------------------------------------------------------
  // Loader owns tree/preferences/expansion/tab; CRUD owns create/rename/delete
  // + action-prompt state; context-menu owns the right-click cluster. Each
  // is consumed below via reactive aliases so the template is unchanged.
  const loader = useNavLoader({
    getActive,
    setActive,
    // Wrap callback props so each call reads the live prop value (props are
    // reactive; referencing them bare in this object literal would snapshot
    // the initial value — the `state_referenced_locally` trap).
    onSelectNotebook: (nb: string) => onSelectNotebook(nb),
    onNavigationLoaded: (t: NavigationTree) => onNavigationLoaded?.(t),
    onNavigationPreferencesLoaded: (p: NavigationPreferences) =>
      onNavigationPreferencesLoaded?.(p),
    onNavigationStatus: (loading: boolean, error: string) =>
      onNavigationStatus?.(loading, error)
  })

  const crud = useNavCrud({
    getActive,
    setActive,
    getTree: () => loader.tree,
    getActiveView: () => activeView,
    setActiveView: (view: string) => {
      activeView = view
    },
    onSelectView: (view: string) => onSelectView(view),
    onSelectNotebook: (nb: string) => onSelectNotebook(nb),
    onSelectSection: (section: string) => onSelectSection(section),
    onSelectPage: (nb: string, section: string, page: string) =>
      onSelectPage(nb, section, page),
    getExpandedSections: () => loader.expandedSections,
    setExpandedSections: (next: SvelteSet<string>) =>
      loader.setExpandedSections(next),
    toggleSection: (path: string) => loader.toggleSection(path),
    setLocalExpansion: (notebook: string, path: string, expanded: boolean) =>
      loader.setLocalExpansion(notebook, path, expanded),
    loadNavigation: () => loader.loadNavigation(),
    loadNavigationPreferences: () => loader.loadNavigationPreferences(),
    handleSelectNotebook,
    setShowNotebookDropdown: (visible: boolean) => {
      showNotebookDropdown = visible
    }
  })

  const menu = useSidebarContextMenu({
    getTree: () => loader.tree,
    setActive: (patch: { section?: string }) => {
      if (patch.section !== undefined) activeSection = patch.section
    },
    onSelectSection: (section: string) => onSelectSection(section),
    openRename: crud.openRename,
    requestDelete: crud.requestDelete,
    requestActionPrompt: crud.requestActionPrompt,
    handleCreatePageInline: crud.handleCreatePageInline,
    toggleFavorite: loader.toggleFavorite,
    setActionError: crud.setActionError
  })

  // Drag-and-drop state stays on the shell (it owns the rendered drop
  // indicators); the manager delegates persistence to the loader's
  // nav-order manager and reloads via the loader after a move.
  let dragItem = $state<{
    level: string
    name: string
    section: string
  } | null>(null)
  let dropTarget = $state<{
    level: string
    name: string
    before: boolean
  } | null>(null)
  let dndError = $state('')
  let dndErrorTimer: ReturnType<typeof setTimeout> | null = null

  function showDndError(msg: string) {
    dndError = msg
    if (dndErrorTimer) clearTimeout(dndErrorTimer)
    dndErrorTimer = setTimeout(() => {
      dndError = ''
      dndErrorTimer = null
    }, 4000)
  }

  const dnd = new DragDropManager({
    getActiveNotebook: () => activeNotebook,
    getActiveNotebookSections: () => activeNotebookObj?.sections ?? [],
    navOrder: loader.navOrderManager,
    onDragItemChange: (item) => {
      dragItem = item
    },
    onDropTargetChange: (target) => {
      dropTarget = target
    },
    onError: showDndError,
    onMoved: async () => {
      await loader.loadNavigation()
      await loader.navOrderManager.load()
    },
    onPageMoved: (nb, from, to, page) => onPageMoved?.(nb, from, to, page)
  })

  function handleDragStart(
    e: DragEvent,
    level: string,
    name: string,
    section: string = ''
  ) {
    dnd.handleDragStart(e, level, name, section)
  }
  function handleDragOver(e: DragEvent, level: string, name: string) {
    dnd.handleDragOver(e, level, name)
  }
  function handleDragLeave() {
    dnd.handleDragLeave()
  }
  async function handleDrop(
    e: DragEvent,
    level: string,
    targetName: string,
    notebook: string = '',
    section: string = ''
  ) {
    await dnd.handleDrop(e, level, targetName, notebook, section)
  }
  function handleDragEnd() {
    dnd.handleDragEnd()
  }

  // Expanded section names (within the active notebook). The active section is
  // always expanded so the active path stays visible (spatial memory).
  let focusedTreeItemId = $state('')
  let typeahead = ''
  let typeaheadTimer: ReturnType<typeof setTimeout> | null = null

  let activeNotebookObj = $derived(
    loader.tree.notebooks.find((nb) => nb.name === activeNotebook)
  )

  // Sections sorted by nav_order (falling back to alphabetical) for #68.
  let sortedSections = $derived.by(() => {
    if (!activeNotebookObj) return []
    return sortByName(
      activeNotebookObj.sections,
      loader.navOrder.sections[activeNotebook] ?? [],
      (section) => section.path
    )
  })

  let visibleNodes = $derived(
    visibleTreeNodes(
      activeNotebookObj
        ? { ...activeNotebookObj, sections: sortedSections }
        : undefined,
      loader.expandedSections
    )
  )

  // --- reactive aliases onto composable state ---------------------------
  // The template (and the tree keyboard-nav below) read these by their bare
  // names; each aliases a composable getter so Svelte 5 reactivity is
  // preserved across the module boundary (destructuring would snapshot).
  let tree = $derived(loader.tree)
  let navigationLoading = $derived(loader.navigationLoading)
  let navigationError = $derived(loader.navigationError)
  let preferences = $derived(loader.preferences)
  let preferencesLoading = $derived(loader.preferencesLoading)
  let preferencesError = $derived(loader.preferencesError)
  let sidebarTab = $derived(loader.sidebarTab)
  let expandedSections = $derived(loader.expandedSections)
  let navOrder = $derived(loader.navOrder)
  let favoriteKeys = $derived(loader.favoriteKeys)
  let favoriteState = $derived(loader.favoriteState)
  let recentState = $derived(loader.recentState)

  let createMode = $derived(crud.createMode)
  let editingMode = $derived(crud.editingMode)
  let renameCtx = $derived(crud.renameCtx)
  let newName = $derived(crud.newName)
  let createError = $derived(crud.createError)
  let creating = $derived(crud.creating)
  let actionPrompt = $derived(crud.actionPrompt)
  let actionPromptError = $derived(crud.actionPromptError)
  let actionBusy = $derived(crud.actionBusy)
  let actionError = $derived(crud.actionError)
  let deleteTarget = $derived(crud.deleteTarget)
  let deleteTargetDisposition = $derived(crud.deleteTargetDisposition)

  let contextMenu = $derived(menu.contextMenu)
  let contextMenuUnlink = $derived(menu.contextMenuUnlink)
  let contextMenuPageRef = $derived(menu.contextMenuPageRef)
  let contextNotebook = $derived(menu.contextNotebook)
  let contextUnavailable = $derived(menu.contextUnavailable)
  let contextMenuTargetId = $derived(menu.contextMenuTargetId)

  // Handler aliases (stable closures — safe to alias by reference).
  const loadNavigation = loader.loadNavigation
  const loadNavigationPreferences = loader.loadNavigationPreferences
  const toggleSection = loader.toggleSection
  const setSidebarTab = loader.setSidebarTab
  const toggleFavorite = loader.toggleFavorite
  const openCreate = crud.openCreate
  const openRename = crud.openRename
  const closeNamePrompt = crud.closeNamePrompt
  const handleCreate = crud.handleCreate
  const handleOpenNotebookFolder = crud.handleOpenNotebookFolder
  const handleLinkExternalNotebook = crud.handleLinkExternalNotebook
  const handleCreatePageInline = crud.handleCreatePageInline
  const confirmActionPrompt = crud.confirmActionPrompt
  const closeActionPrompt = crud.closeActionPrompt
  const confirmDelete = crud.confirmDelete
  const cancelDelete = crud.cancelDelete
  const namePromptTitle = crud.namePromptTitle
  const namePromptLabel = crud.namePromptLabel
  const namePromptPlaceholder = crud.namePromptPlaceholder
  const namePromptConfirmLabel = crud.namePromptConfirmLabel
  const openContextMenu = menu.openContextMenu
  const closeContextMenu = menu.closeContextMenu
  const handleContextRename = menu.handleContextRename
  const handleContextDelete = menu.handleContextDelete
  const handleContextReveal = menu.handleContextReveal
  const handleContextFavorite = menu.handleContextFavorite
  const handleContextNewPage = menu.handleContextNewPage
  const handleContextCopyPage = menu.handleContextCopyPage
  const handleContextCopyNotebook = menu.handleContextCopyNotebook
  const openDuplicatePrompt = menu.openDuplicatePrompt
  const openChildSectionPrompt = menu.openChildSectionPrompt

  $effect(() => {
    const nodes = visibleNodes
    const current = focusedTreeItemId
    if (nodes.some((node) => node.id === current)) return
    const activeId = activePage
      ? pageNodeId({
          notebook: activeNotebook,
          section: activeSection,
          page: activePage
        })
      : activeSection
        ? sectionNodeId(activeNotebook, activeSection)
        : ''
    focusedTreeItemId =
      nodes.find((node) => node.id === activeId)?.id ?? nodes[0]?.id ?? ''
  })

  // Sections are optional — a page can live directly under a notebook — so the
  // only persistent hint is "create/open a notebook"; section guidance is
  // hover-only on the buttons. A native title on a disabled button never shows,
  // so the wrapper span carries it.
  function shortcutSuffix(
    action: Parameters<typeof shortcutBinding>[0]
  ): string {
    const binding = shortcutBinding(action, settings.config?.hotkeys ?? {})
    return binding ? ` (${binding})` : ''
  }
  let sectionHint = $derived(
    activeNotebook
      ? `New Section${shortcutSuffix('new_section')}`
      : 'Create or open a Notebook first'
  )
  let pageHint = $derived(
    activeNotebook
      ? activeSection
        ? `New Page in ${activeSection}${shortcutSuffix('new_page')}`
        : `New Page (no section)${shortcutSuffix('new_page')}`
      : 'Create or open a Notebook first'
  )
  let templateHint = $derived(
    `New page from template${shortcutSuffix('open_template_picker')}`
  )
  let hideSidebarHint = $derived(
    `Hide sidebar${shortcutSuffix('toggle_sidebar')}`
  )
  let nextStep = $derived(
    !activeNotebook ? 'Create or open a Notebook to get started.' : ''
  )
  let hasNoContent = $derived(
    activeNotebookObj &&
      activeNotebookObj.sections.filter((s) => s.name !== '').length === 0 &&
      (activeNotebookObj.sections.find((s) => s.name === '')?.pages.length ??
        0) === 0
  )

  function onSidebarTabKeydown(e: KeyboardEvent) {
    if (e.key !== 'ArrowLeft' && e.key !== 'ArrowRight') return
    e.preventDefault()
    const next = sidebarTab === 'tree' ? 'quick' : 'tree'
    void setSidebarTab(next)
    // Roving tabindex: move focus with selection (WAI-ARIA Tabs).
    void tick().then(() => {
      document
        .getElementById(
          next === 'tree' ? 'sidebar-tab-tree' : 'sidebar-tab-quick'
        )
        ?.focus()
    })
  }

  function setFocusedTreeItem(id: string) {
    focusedTreeItemId = id
  }

  async function focusTreeItem(id: string) {
    focusedTreeItemId = id
    await tick()
    const item = Array.from(
      document.querySelectorAll<HTMLElement>(
        '[data-sidebar-tree] [data-tree-id]'
      )
    ).find((candidate) => candidate.dataset.treeId === id)
    item?.focus()
  }

  function activateTreeNode(id: string) {
    const node = visibleNodes.find((candidate) => candidate.id === id)
    if (!node) return
    if (node.kind === 'section') {
      onSelectSection(node.section)
      toggleSection(node.section)
    } else {
      handleSelectPage(node.section, node.page)
    }
  }

  function handleTreeKeydown(event: KeyboardEvent) {
    const target = (event.target as HTMLElement).closest<HTMLElement>(
      '[data-tree-id]'
    )
    const id = target?.dataset.treeId
    if (!id) return
    const index = visibleNodes.findIndex((node) => node.id === id)
    if (index < 0) return
    const node = visibleNodes[index]
    let destination = ''
    if (event.key === 'ArrowDown')
      destination = visibleNodes[index + 1]?.id ?? id
    else if (event.key === 'ArrowUp')
      destination = visibleNodes[index - 1]?.id ?? id
    else if (event.key === 'Home') destination = visibleNodes[0]?.id ?? id
    else if (event.key === 'End') destination = visibleNodes.at(-1)?.id ?? id
    else if (event.key === 'ArrowRight' && node.kind === 'section') {
      if (!expandedSections.has(node.section)) toggleSection(node.section)
      else destination = visibleNodes[index + 1]?.id ?? id
    } else if (event.key === 'ArrowLeft') {
      if (node.kind === 'section' && expandedSections.has(node.section)) {
        toggleSection(node.section)
      } else if (node.parentId) destination = node.parentId
    } else if (event.key === 'Enter' || event.key === ' ') {
      activateTreeNode(id)
    } else if (
      event.key === 'ContextMenu' ||
      (event.shiftKey && event.key === 'F10')
    ) {
      const bounds = target?.getBoundingClientRect()
      target?.dispatchEvent(
        new MouseEvent('contextmenu', {
          bubbles: true,
          clientX: bounds?.left ?? 0,
          clientY: bounds?.bottom ?? 0
        })
      )
    } else if (
      event.key.length === 1 &&
      !event.shiftKey &&
      !event.ctrlKey &&
      !event.metaKey &&
      !event.altKey &&
      /^[\p{L}\p{N}]$/u.test(event.key)
    ) {
      typeahead += event.key.toLocaleLowerCase()
      if (typeaheadTimer) clearTimeout(typeaheadTimer)
      typeaheadTimer = setTimeout(() => (typeahead = ''), 650)
      const ordered = [
        ...visibleNodes.slice(index + 1),
        ...visibleNodes.slice(0, index + 1)
      ]
      destination =
        ordered.find((candidate) =>
          candidate.label.toLocaleLowerCase().startsWith(typeahead)
        )?.id ?? ''
    } else return
    event.preventDefault()
    event.stopPropagation()
    if (destination) void focusTreeItem(destination)
  }

  function handleSelectNotebook(nb: string) {
    activeNotebook = nb
    activeSection = ''
    activePage = ''
    showNotebookDropdown = false
    onSelectNotebook(nb)
    // Expand the first section if present, for orientation.
    const nbObj = loader.tree.notebooks.find((n) => n.name === nb)
    loader.setExpandedSections(
      new SvelteSet(expandedPathsForNotebook(loader.preferences, nb))
    )
    focusedTreeItemId = nbObj?.sections.find((section) => section.path)?.path
      ? sectionNodeId(nb, nbObj.sections.find((section) => section.path)!.path)
      : ''
  }

  function handleSelectPage(section: string, page: string) {
    activeSection = section
    activePage = page
    onSelectPage(activeNotebook, section, page)
  }

  function handleQuickPage(ref: NavigationPageRef) {
    activeNotebook = ref.notebook
    loader.setExpandedSections(
      new SvelteSet(
        expandActiveAncestors(
          expandedPathsForNotebook(loader.preferences, ref.notebook),
          ref.section
        )
      )
    )
    handleSelectPage(ref.section, ref.page)
  }

  function handlePinPage(section: string, page: string) {
    activeSection = section
    activePage = page
    onPinPage(activeNotebook, section, page)
  }

  onMount(() => {
    void loadNavigation()
    void loadNavigationPreferences()
    void loader.loadNavOrder()
    const handleRefresh = () => {
      void loadNavigation()
      void loadNavigationPreferences()
      void loader.loadNavOrder()
    }
    const handleCreatePageInlineEvent = (e: Event) => {
      const sectionName =
        (e as CustomEvent).detail?.sectionName ?? activeSection ?? ''
      void handleCreatePageInline(sectionName)
    }
    const handlePreferenceRefresh = () => void loadNavigationPreferences()
    const handleNavigationCreate = (event: Event) => {
      const kind = (event as CustomEvent).detail?.kind
      if (kind === 'notebook') openCreate('notebook')
      else if (kind === 'section' && activeNotebook) openCreate('section')
    }
    window.addEventListener('refresh-navigation', handleRefresh)
    window.addEventListener(
      'navigation-preferences-changed',
      handlePreferenceRefresh
    )
    window.addEventListener('create-page-inline', handleCreatePageInlineEvent)
    window.addEventListener('open-navigation-create', handleNavigationCreate)
    const offConfigChanged = Events.On(
      'config:changed',
      () => void loadNavigationPreferences()
    )
    return () => {
      offConfigChanged()
      window.removeEventListener('refresh-navigation', handleRefresh)
      window.removeEventListener(
        'navigation-preferences-changed',
        handlePreferenceRefresh
      )
      window.removeEventListener(
        'create-page-inline',
        handleCreatePageInlineEvent
      )
      window.removeEventListener(
        'open-navigation-create',
        handleNavigationCreate
      )
      if (dndErrorTimer) clearTimeout(dndErrorTimer)
      if (typeaheadTimer) clearTimeout(typeaheadTimer)
    }
  })
</script>

<aside
  data-sidebar
  class="bg-surface-sidebar border-r border-surface-sidebar-border flex flex-col py-1 h-full flex-shrink-0 select-none z-40"
  style:width={collapsed ? '0px' : sidebarWidth + 'px'}
  style:transition={sidebarDragging ? 'none' : 'all 200ms ease-out'}
  style:overflow={collapsed ? 'hidden' : 'visible'}
  style:border-right={collapsed
    ? '0'
    : '1px solid var(--color-surface-sidebar-border)'}
>
  <div
    class="px-3 py-3 flex flex-col gap-1 relative flex-1 overflow-hidden flex"
  >
    {#if activeView === 'tags'}
      <TagSidebarPanel bind:selectedTag />
    {:else if activeView === 'backlinks'}
      <BacklinksSidebarPanel
        notebook={activeNotebook}
        section={activeSection}
        page={activePage}
      />
    {:else if activeView === 'settings'}
      <!-- Settings view: the sidebar IS the section nav (#511 rework). The
           matching panel lives in the content area (SettingsPanel). -->
      <SettingsNav bind:section={settingsSection} />
    {:else if SidebarCmp && pluginSidebarCtx}
      <!-- Plugin-provided primary sidebar (#321). The active view's plugin
           owns the entire sidebar slot when it registers a sidebarComponent;
           the notebook selector + page tree are skipped because the plugin
           is responsible for any navigation affordance it wants to expose. -->
      <SidebarCmp ctx={pluginSidebarCtx} manifest={pluginSidebarManifest} />
    {:else}
      <!-- Notebook selector -->
      <div class="px-1 mb-3 relative">
        <button
          type="button"
          aria-label={activeNotebook
            ? `Active notebook: ${activeNotebook}`
            : 'Choose a notebook'}
          onclick={() => (showNotebookDropdown = !showNotebookDropdown)}
          class="w-full border-none bg-transparent text-left flex items-center gap-2 cursor-pointer group px-2 py-1.5 rounded hover:bg-hover transition-all duration-150 focus-visible:ring-2 focus-visible:ring-accent-primary-start"
          class:bg-hover={showNotebookDropdown}
          aria-haspopup="menu"
          aria-expanded={showNotebookDropdown}
          aria-controls="notebook-switcher-menu"
        >
          <div class="flex flex-col min-w-0 flex-1">
            <span
              class="text-surface-sidebar-text font-headline-md text-headline-md truncate"
              >{activeNotebook || 'No Notebook'}</span
            >
            <span
              class="text-surface-sidebar-text-muted text-type-3xs uppercase tracking-widest font-label-sm-bold"
              >Active Notebook</span
            >
          </div>
          <span
            class="material-symbols-outlined text-surface-sidebar-text-muted text-icon-lg group-hover:text-accent-primary-start transition-colors"
          >
            {showNotebookDropdown ? 'expand_less' : 'expand_more'}
          </span>
        </button>

        {#if showNotebookDropdown}
          <button
            tabindex="-1"
            aria-label="Close notebook menu"
            onclick={() => (showNotebookDropdown = false)}
            class="fixed inset-0 z-[60] cursor-default border-none bg-transparent p-0"
          ></button>
          <div
            id="notebook-switcher-menu"
            role="menu"
            aria-label="Notebooks"
            class="absolute left-1 right-1 top-14 glass-palette border border-accent-primary-start/20 rounded-lg shadow-2xl z-[70] py-2 max-h-[60vh] overflow-y-auto custom-scrollbar"
            style="backdrop-filter: blur(16px); background: color-mix(in srgb, var(--color-surface-sidebar) 92%, transparent);"
          >
            {#if tree.notebooks.length === 0}
              <div
                class="px-4 py-3 text-surface-sidebar-text-muted text-type-sm font-body-md"
              >
                No notebooks yet.
              </div>
            {:else}
              {#each tree.notebooks as nb (nb.name)}
                <button
                  id={`notebook-menu-${encodeURIComponent(nb.name)}`}
                  onclick={() => handleSelectNotebook(nb.name)}
                  oncontextmenu={(e) => {
                    openContextMenu(e, 'notebook', nb.name)
                  }}
                  class="flex items-center gap-3 px-4 py-2 w-full text-left cursor-pointer hover:bg-hover transition-colors font-body-md border-none bg-transparent"
                  aria-haspopup="menu"
                  role="menuitem"
                  aria-current={nb.name === activeNotebook ? 'true' : undefined}
                  aria-expanded={contextMenuTargetId ===
                    `notebook:${encodeURIComponent(nb.name)}`}
                  aria-controls={contextMenuTargetId ===
                  `notebook:${encodeURIComponent(nb.name)}`
                    ? 'sidebar-context-menu'
                    : undefined}
                >
                  <span
                    class="material-symbols-outlined text-accent-primary-start text-icon-lg"
                    >folder_special</span
                  >
                  <span
                    class="font-label-sm text-label-sm text-surface-sidebar-text truncate flex-1"
                    >{nb.name}</span
                  >
                  {#if nb.source && nb.source !== 'vault'}
                    <span
                      class="material-symbols-outlined text-icon-sm {nb.disconnected
                        ? 'text-status-warn'
                        : 'text-surface-sidebar-text-muted'}"
                      title={nb.disconnected
                        ? `Linked (offline): ${nb.root_path}`
                        : `Linked: ${nb.root_path}`}
                      aria-label={nb.disconnected
                        ? 'Linked notebook offline'
                        : 'Linked notebook'}
                      >{nb.disconnected ? 'cloud_off' : 'link'}</span
                    >
                  {/if}
                  {#if nb.name === activeNotebook}
                    <span
                      class="material-symbols-outlined text-accent-primary-start text-icon-md"
                      >check</span
                    >
                  {/if}
                </button>
              {/each}
            {/if}

            <div class="border-t border-surface-sidebar-border mt-1 pt-1">
              <button
                onclick={() => {
                  showNotebookDropdown = false
                  openCreate('notebook')
                }}
                title={`New Notebook${shortcutSuffix('new_notebook')}`}
                class="flex items-center gap-3 px-4 py-2 w-full text-left cursor-pointer hover:bg-hover transition-colors font-body-md border-none bg-transparent text-accent-primary-start"
              >
                <span class="material-symbols-outlined text-icon-lg"
                  >create_new_folder</span
                >
                <span class="font-label-sm text-label-sm">New Notebook</span>
              </button>
              <button
                onclick={handleOpenNotebookFolder}
                disabled={creating}
                class="flex items-center gap-3 px-4 py-2 w-full text-left cursor-pointer hover:bg-hover transition-colors font-body-md border-none bg-transparent text-surface-sidebar-text-muted disabled:opacity-50"
              >
                <span class="material-symbols-outlined text-icon-lg"
                  >folder_open</span
                >
                <span class="font-label-sm text-label-sm">Open Notebook…</span>
              </button>
              <button
                onclick={handleLinkExternalNotebook}
                disabled={creating}
                title="Link a folder that lives outside the vault (e.g. a synced SharePoint mount); it is edited in place, never copied in."
                class="flex items-center gap-3 px-4 py-2 w-full text-left cursor-pointer hover:bg-hover transition-colors font-body-md border-none bg-transparent text-surface-sidebar-text-muted disabled:opacity-50"
              >
                <span class="material-symbols-outlined text-icon-lg"
                  >add_link</span
                >
                <span class="font-label-sm text-label-sm"
                  >Link External Folder…</span
                >
              </button>
            </div>
          </div>
        {/if}
      </div>

      <!-- Primary actions (icon-only, consistent style). Each button is wrapped
         in a span whose title gives the prerequisite reason — a native title
         on a disabled button doesn't show, but on the wrapper it does. -->
      <div
        class="px-1 flex items-stretch gap-0.5 mb-1 p-0.5 bg-surface-sidebar border border-surface-sidebar-border rounded-lg"
      >
        <span title={sectionHint} class="flex-1 flex">
          <button
            onclick={() => openCreate('section')}
            disabled={!activeNotebook}
            title={sectionHint}
            aria-label="New Section"
            class="w-full bg-transparent border-none text-surface-sidebar-text-muted hover:text-accent-primary-start hover:bg-hover disabled:opacity-40 disabled:hover:bg-transparent disabled:cursor-not-allowed py-1.5 rounded flex items-center justify-center transition-all cursor-pointer focus:outline-none"
          >
            <span class="material-symbols-outlined text-type-2xl"
              >create_new_folder</span
            >
          </button>
        </span>
        <div class="w-px bg-surface-sidebar-border my-1.5 flex-shrink-0"></div>
        <span title={pageHint} class="flex-1 flex">
          <button
            onclick={() => handleCreatePageInline(activeSection || '')}
            disabled={!activeNotebook}
            title={pageHint}
            aria-label="New Page"
            class="w-full bg-transparent border-none text-surface-sidebar-text-muted hover:text-accent-primary-start hover:bg-hover disabled:opacity-40 disabled:hover:bg-transparent disabled:cursor-not-allowed py-1.5 rounded flex items-center justify-center transition-all cursor-pointer focus:outline-none"
          >
            <span class="material-symbols-outlined text-type-2xl">note_add</span
            >
          </button>
        </span>
        <div class="w-px bg-surface-sidebar-border my-1.5 flex-shrink-0"></div>
        <span title={templateHint} class="flex-1 flex">
          <button
            onclick={() =>
              window.dispatchEvent(new CustomEvent('open-template-picker'))}
            disabled={!activeNotebook}
            title={templateHint}
            aria-label="New Page from Template"
            class="w-full bg-transparent border-none text-surface-sidebar-text-muted hover:text-accent-primary-start hover:bg-hover disabled:opacity-40 disabled:hover:bg-transparent disabled:cursor-not-allowed py-1.5 rounded flex items-center justify-center transition-all cursor-pointer focus:outline-none"
          >
            <span class="material-symbols-outlined text-type-2xl"
              >content_copy</span
            >
          </button>
        </span>
      </div>
      {#if nextStep}
        <div
          class="px-2 pb-2 text-type-2xs text-surface-sidebar-text-muted font-label-sm flex items-center gap-1"
        >
          <span
            class="material-symbols-outlined text-icon-xs text-accent-primary-start/70"
            >info</span
          >
          {nextStep}
        </div>
      {/if}

      <!-- Sidebar View Mode Switcher -->
      <div
        class="mx-1 mb-2 flex items-center gap-0.5 bg-surface-sidebar border border-surface-sidebar-border p-0.5 rounded-md select-none"
        role="tablist"
        aria-label="Sidebar navigation views"
      >
        <button
          type="button"
          role="tab"
          id="sidebar-tab-tree"
          aria-selected={sidebarTab === 'tree'}
          aria-controls="sidebar-tree-panel"
          tabindex={sidebarTab === 'tree' ? 0 : -1}
          title="Notebook tree view"
          aria-label="Notebook tree view"
          class="flex-1 py-1 px-2 border-none rounded cursor-pointer transition-all flex items-center justify-center gap-1"
          class:bg-hover={sidebarTab === 'tree'}
          class:text-surface-sidebar-text={sidebarTab === 'tree'}
          class:text-surface-sidebar-text-muted={sidebarTab !== 'tree'}
          onkeydown={onSidebarTabKeydown}
          onclick={() => void setSidebarTab('tree')}
        >
          <span class="material-symbols-outlined tab-icon" aria-hidden="true"
            >account_tree</span
          >
        </button>
        <button
          type="button"
          role="tab"
          id="sidebar-tab-quick"
          aria-selected={sidebarTab === 'quick'}
          aria-controls="sidebar-quick-panel"
          tabindex={sidebarTab === 'quick' ? 0 : -1}
          title="Quick access bookmarks and recents"
          aria-label="Quick access bookmarks and recents"
          class="flex-1 py-1 px-2 border-none rounded cursor-pointer transition-all flex items-center justify-center gap-1"
          class:bg-hover={sidebarTab === 'quick'}
          class:text-surface-sidebar-text={sidebarTab === 'quick'}
          class:text-surface-sidebar-text-muted={sidebarTab !== 'quick'}
          onkeydown={onSidebarTabKeydown}
          onclick={() => void setSidebarTab('quick')}
        >
          <span class="material-symbols-outlined tab-icon" aria-hidden="true"
            >push_pin</span
          >
          {#if preferences.favorites.length + preferences.recent_pages.length > 0}
            <span class="text-type-3xs opacity-75 font-label-sm-bold"
              >({preferences.favorites.length +
                preferences.recent_pages.length})</span
            >
          {/if}
        </button>
      </div>

      {#if sidebarTab === 'quick'}
        <SidebarQuickAccess
          favorites={preferences.favorites}
          recents={preferences.recent_pages}
          staleKeys={new Set([
            ...favoriteState.stale.map((ref) => locatorKey(ref)),
            ...recentState.stale.map((ref) => locatorKey(ref))
          ])}
          notebooks={tree.notebooks}
          {activeNotebook}
          {activeSection}
          {activePage}
          loading={preferencesLoading}
          error={preferencesError}
          onOpen={handleQuickPage}
          onToggleFavorite={(ref) => void toggleFavorite(ref)}
          onRetry={() => void loadNavigationPreferences()}
        />
      {:else}
        <!-- Navigation tree -->
        <div
          id="sidebar-tree-panel"
          role="tabpanel"
          aria-label="Notebook tree"
          aria-labelledby="sidebar-tab-tree"
          class="flex-1 overflow-y-auto custom-scrollbar px-1"
          data-sidebar-scroll
        >
          <div class="sr-only" aria-live="polite">
            {navigationError
              ? `Navigation could not be refreshed. ${navigationError}`
              : navigationLoading
                ? 'Loading navigation.'
                : 'Navigation loaded.'}
          </div>
          {#if navigationError}
            <div
              class="mb-2 mx-1 p-2 rounded-lg border border-status-warn/35 bg-status-warn/10 text-type-2xs text-surface-sidebar-text"
              role="status"
            >
              <p class="m-0">
                The page list could not be refreshed. Your previous list is
                still available.
              </p>
              <button
                type="button"
                class="mt-1 border-none bg-transparent p-0 text-accent-primary-start underline cursor-pointer"
                onclick={() => void loadNavigation()}>Try again</button
              >
            </div>
          {/if}
          {#if !activeNotebookObj}
            <div
              class="text-surface-sidebar-text-muted py-10 text-center font-body-md text-type-md border border-dashed border-surface-sidebar-border rounded-lg mx-1"
            >
              {#if tree.notebooks.length === 0}
                No notebooks yet.<br />Create or open one to begin.
              {:else}
                Select a notebook.
              {/if}
            </div>
          {:else}
            <div
              role="tree"
              tabindex="-1"
              aria-label={`${activeNotebook} pages`}
              data-sidebar-tree
              onkeydown={handleTreeKeydown}
            >
              {#if hasNoContent}
                <div
                  class="text-surface-sidebar-text-muted py-6 text-center font-body-md text-type-md border border-dashed border-surface-sidebar-border rounded-lg mx-1"
                >
                  No sections or pages yet.<br />Create one to get started.
                </div>
              {/if}
              {#each sortedSections.filter((s) => s.name !== '') as sec (sec.name)}
                <SidebarSection
                  section={sec}
                  depth={0}
                  {activeNotebook}
                  {activeSection}
                  {activePage}
                  {expandedSections}
                  {navOrder}
                  {dropTarget}
                  {dragItem}
                  {focusedTreeItemId}
                  {contextMenuTargetId}
                  onTreeItemFocus={setFocusedTreeItem}
                  onToggleSection={toggleSection}
                  onSelectPage={handleSelectPage}
                  onPinPage={handlePinPage}
                  {onSelectSection}
                  onCreatePageInline={handleCreatePageInline}
                  onDragStart={handleDragStart}
                  onDragOver={handleDragOver}
                  onDragLeave={handleDragLeave}
                  onDrop={handleDrop}
                  onDragEnd={handleDragEnd}
                  onContextMenu={openContextMenu}
                />
              {/each}

              <!-- Section-less root pages -->
              {#each sortedSections.filter((s) => s.name === '') as rootSec (rootSec.path || 'root')}
                {#if rootSec.pages.length > 0}
                  <div
                    class="h-px bg-surface-sidebar-border my-3 mx-1.5 opacity-50"
                  ></div>
                  {#each sortByName(rootSec.pages, navOrder.pages[`${activeNotebook}/`] ?? []) as pg (pg.name)}
                    {@const isActive =
                      activeSection === '' && activePage === pg.name}
                    <button
                      onclick={() => handleSelectPage('', pg.name)}
                      onfocus={() =>
                        setFocusedTreeItem(
                          pageNodeId({
                            notebook: activeNotebook,
                            section: '',
                            page: pg.name
                          })
                        )}
                      ondblclick={() => handlePinPage('', pg.name)}
                      onauxclick={(e) => {
                        if (e.button === 1) {
                          e.preventDefault()
                          handlePinPage('', pg.name)
                        }
                      }}
                      oncontextmenu={(e) =>
                        openContextMenu(e, 'page', activeNotebook, '', pg.name)}
                      draggable="true"
                      ondragstart={(e) =>
                        handleDragStart(e, 'page', pg.name, '')}
                      ondragover={(e) =>
                        dnd.handleDragOver(
                          e,
                          'page',
                          pg.name,
                          `\u0000${pg.name}`
                        )}
                      ondragleave={handleDragLeave}
                      ondrop={(e) =>
                        handleDrop(e, 'page', pg.name, activeNotebook, '')}
                      ondragend={handleDragEnd}
                      class="relative w-full text-left pl-7 pr-2 py-1.5 rounded text-type-md font-body-md transition-colors border-none bg-transparent cursor-pointer flex items-center gap-2"
                      class:bg-hover={isActive}
                      class:text-accent-primary-start={isActive}
                      class:text-surface-sidebar-text-muted={!isActive}
                      class:hover:text-surface-sidebar-text={!isActive}
                      class:drag-over-top={dropTarget?.level === 'page' &&
                        dropTarget.name === `\u0000${pg.name}` &&
                        dropTarget.before}
                      class:drag-over-bottom={dropTarget?.level === 'page' &&
                        dropTarget.name === `\u0000${pg.name}` &&
                        !dropTarget.before}
                      role="treeitem"
                      data-tree-id={pageNodeId({
                        notebook: activeNotebook,
                        section: '',
                        page: pg.name
                      })}
                      tabindex={focusedTreeItemId ===
                      pageNodeId({
                        notebook: activeNotebook,
                        section: '',
                        page: pg.name
                      })
                        ? 0
                        : -1}
                      aria-level="1"
                      aria-selected={isActive}
                      aria-haspopup="menu"
                      aria-controls={contextMenuTargetId ===
                      pageNodeId({
                        notebook: activeNotebook,
                        section: '',
                        page: pg.name
                      })
                        ? 'sidebar-context-menu'
                        : undefined}
                    >
                      {#if isActive}
                        <span
                          class="absolute left-1 top-1 bottom-1 w-0.5 bg-accent-primary-start rounded-full"
                        ></span>
                      {/if}
                      <span class="truncate flex-1" title={pg.name}
                        >{pg.name}</span
                      >
                    </button>
                  {/each}
                {/if}
              {/each}
              <!-- Notebook-root drop zone (#177): drag a page here to move it
             out of any section (section-less / root). Invisible until a
             page is actively dragged over it. -->
              <div
                class="mx-1 mt-1 rounded transition-colors min-h-6"
                class:drag-over-into={dropTarget?.level === 'section' &&
                  dropTarget.name === '__root__'}
                ondragover={(e) => {
                  if (!dragItem || dragItem.level !== 'page') return
                  e.preventDefault()
                  if (e.dataTransfer) e.dataTransfer.dropEffect = 'move'
                  dropTarget = {
                    level: 'section',
                    name: '__root__',
                    before: false
                  }
                }}
                ondragleave={handleDragLeave}
                ondrop={(e) =>
                  handleDrop(e, 'section', '__root__', activeNotebook, '')}
                role="none"
              >
                {#if dragItem?.level === 'page'}
                  <div
                    class="text-surface-sidebar-text-muted text-type-xs font-body-md py-1.5 px-2 text-center border border-dashed border-surface-sidebar-border rounded"
                  >
                    Drop to move to notebook root
                  </div>
                {/if}
              </div>
            </div>
          {/if}
        </div>
      {/if}
    {/if}
  </div>

  <!-- DnD error toast (#177 collision / FS error). Perceivable without
       color via icon + text; aria-live so AT users hear the error. -->
  {#if dndError}
    <div
      class="fixed bottom-4 left-1/2 -translate-x-1/2 z-[200] flex items-center gap-2 px-4 py-2.5 rounded-lg shadow-2xl border border-status-danger/40 bg-surface-sidebar"
      role="alert"
      aria-live="assertive"
    >
      <span
        class="material-symbols-outlined text-status-danger text-icon-lg"
        aria-hidden="true">error</span
      >
      <span class="text-surface-sidebar-text text-type-md font-body-md"
        >{dndError}</span
      >
    </div>
  {/if}
  {#if actionError}
    <div
      class="fixed bottom-4 left-1/2 -translate-x-1/2 z-[200] flex items-center gap-2 px-4 py-2.5 rounded-lg shadow-2xl border border-status-danger/40 bg-surface-sidebar"
      role="alert"
      aria-live="assertive"
    >
      <span
        class="material-symbols-outlined text-status-danger text-icon-lg"
        aria-hidden="true">error</span
      >
      <span class="text-surface-sidebar-text text-type-md font-body-md"
        >{actionError}</span
      >
      <button
        type="button"
        class="ml-2 border-none bg-transparent text-surface-sidebar-text-muted cursor-pointer"
        aria-label="Dismiss error"
        onclick={() => crud.setActionError('')}>Dismiss</button
      >
    </div>
  {/if}

  <!-- Shared create/rename dialog (#662) — NamePromptDialog for all nav CRUD. -->
  {#if createMode}
    <NamePromptDialog
      title={namePromptTitle()}
      label={namePromptLabel()}
      initialValue={newName}
      placeholder={namePromptPlaceholder()}
      confirmLabel={namePromptConfirmLabel()}
      errorMessage={createError}
      busy={creating}
      dataTestId="sidebar-name-prompt"
      onConfirm={(value) => void handleCreate(value)}
      onCancel={closeNamePrompt}
    />
  {/if}
  {#if actionPrompt}
    <NamePromptDialog
      title={actionPrompt.kind === 'duplicate'
        ? 'Duplicate Page'
        : 'New Child Section'}
      label={actionPrompt.kind === 'duplicate' ? 'Page name' : 'Section name'}
      initialValue={actionPrompt.initialValue}
      placeholder={actionPrompt.kind === 'duplicate'
        ? 'Duplicate page name…'
        : 'Child section name…'}
      confirmLabel={actionBusy
        ? actionPrompt.kind === 'duplicate'
          ? 'Duplicating…'
          : 'Creating…'
        : actionPrompt.kind === 'duplicate'
          ? 'Duplicate'
          : 'Create'}
      errorMessage={actionPromptError}
      busy={actionBusy}
      dataTestId="sidebar-action-prompt"
      onConfirm={(value) => void confirmActionPrompt(value)}
      onCancel={closeActionPrompt}
    />
  {/if}

  <!-- Sidebar Footer -->
  <div
    class="px-3 py-2 border-t border-surface-sidebar-border flex items-center justify-between bg-surface-sidebar flex-shrink-0"
  >
    <button
      onclick={() => (collapsed = true)}
      aria-label="Hide sidebar"
      title={hideSidebarHint}
      class="p-1.5 rounded hover:bg-hover text-surface-sidebar-text-muted hover:text-accent-primary-start transition-all duration-150 border-none bg-transparent cursor-pointer focus:outline-none flex items-center justify-center hover:scale-105 active:scale-95"
    >
      <span class="material-symbols-outlined text-icon-lg"
        >left_panel_close</span
      >
    </button>

    <!-- Plugin sidebar panels (#117) -->
    <PluginSidebarPanels />
  </div>
</aside>

<!-- Context menu (#62). Delegates to the shared ContextMenu component (#491)
     which handles positioning, dismissal, keyboard nav, and scroll-scope (#492). -->
<ContextMenu
  open={contextMenu !== null}
  anchor={contextMenu ? { x: contextMenu.x, y: contextMenu.y } : null}
  anchorEl={contextMenu?.anchorEl ?? null}
  onClose={closeContextMenu}
  ariaLabel="Actions"
  menuId="sidebar-context-menu"
>
  {#if !contextMenuUnlink}
    <!-- Linked notebooks cannot be renamed in place (ARCHITECTURE §3.1);
         RenameNotebook refuses them — omit the dead-end menu item. -->
    <button
      type="button"
      onclick={handleContextRename}
      role="menuitem"
      disabled={contextUnavailable}
      aria-disabled={contextUnavailable}
    >
      <span class="material-symbols-outlined text-icon-md">edit</span>
      Rename
    </button>
  {/if}
  {#if contextMenuPageRef}
    <button
      type="button"
      onclick={openDuplicatePrompt}
      role="menuitem"
      disabled={contextUnavailable}
      aria-disabled={contextUnavailable}
    >
      <span class="material-symbols-outlined text-icon-md" aria-hidden="true"
        >file_copy</span
      >
      Duplicate…
    </button>
    <button
      type="button"
      onclick={() => handleContextCopyPage('path')}
      role="menuitem"
    >
      <span class="material-symbols-outlined text-icon-md" aria-hidden="true"
        >content_copy</span
      >
      Copy Page Path
    </button>
    <button
      type="button"
      onclick={() => handleContextCopyPage('reference')}
      role="menuitem"
    >
      <span class="material-symbols-outlined text-icon-md" aria-hidden="true"
        >link</span
      >
      Copy Page Reference
    </button>
    <button
      type="button"
      onclick={handleContextReveal}
      role="menuitem"
      disabled={contextUnavailable}
      aria-disabled={contextUnavailable}
    >
      <span class="material-symbols-outlined text-icon-md" aria-hidden="true"
        >folder_open</span
      >
      Reveal in file manager
    </button>
    <button type="button" onclick={handleContextFavorite} role="menuitem">
      <span
        class="material-symbols-outlined text-icon-md"
        class:pin-menu-filled={favoriteKeys.has(locatorKey(contextMenuPageRef))}
        aria-hidden="true"
      >
        push_pin
      </span>
      {favoriteKeys.has(locatorKey(contextMenuPageRef))
        ? 'Unpin'
        : 'Pin to Quick Access'}
    </button>
  {/if}
  {#if contextMenu?.level === 'section' || contextMenu?.level === 'page'}
    <button
      type="button"
      onclick={handleContextNewPage}
      role="menuitem"
      disabled={contextUnavailable}
      aria-disabled={contextUnavailable}
    >
      <span class="material-symbols-outlined text-icon-md" aria-hidden="true"
        >note_add</span
      >
      New Page Here
    </button>
  {/if}
  {#if contextMenu?.level === 'section'}
    <button
      type="button"
      onclick={openChildSectionPrompt}
      role="menuitem"
      disabled={contextUnavailable}
      aria-disabled={contextUnavailable}
    >
      <span class="material-symbols-outlined text-icon-md" aria-hidden="true"
        >create_new_folder</span
      >
      New child section…
    </button>
  {/if}
  {#if contextMenu?.level === 'notebook'}
    <button
      type="button"
      onclick={handleContextNewPage}
      role="menuitem"
      disabled={contextUnavailable}
      aria-disabled={contextUnavailable}
    >
      <span class="material-symbols-outlined text-icon-md" aria-hidden="true"
        >note_add</span
      >
      New Page Here
    </button>
    <button type="button" onclick={handleContextCopyNotebook} role="menuitem">
      <span class="material-symbols-outlined text-icon-md" aria-hidden="true"
        >content_copy</span
      >
      Copy Notebook Path
    </button>
    <button
      type="button"
      onclick={handleContextReveal}
      role="menuitem"
      disabled={contextUnavailable}
      aria-disabled={contextUnavailable}
    >
      <span class="material-symbols-outlined text-icon-md">folder_open</span>
      Reveal in file manager
    </button>
  {/if}
  <button
    type="button"
    onclick={handleContextDelete}
    role="menuitem"
    disabled={contextUnavailable && !contextMenuUnlink}
    aria-disabled={contextUnavailable && !contextMenuUnlink}
    class="text-status-danger"
  >
    <span class="material-symbols-outlined text-icon-md"
      >{contextMenuUnlink ? 'link_off' : 'delete'}</span
    >
    {contextMenuUnlink ? 'Unlink' : 'Delete'}
  </button>
  {#if isDevMode()}
    <div class="context-menu-separator" aria-hidden="true"></div>
    <button
      type="button"
      role="menuitem"
      onclick={() => {
        closeContextMenu()
        void openInspect()
      }}
    >
      <span class="material-symbols-outlined text-icon-md" aria-hidden="true"
        >bug_report</span
      >
      Inspect
    </button>
  {/if}
</ContextMenu>

<!-- Delete confirmation dialog (#62) -->
{#if deleteTarget}
  <div
    class="fixed inset-0 bg-black/40 backdrop-blur-[2px] z-[190] flex items-center justify-center"
  >
    <button
      tabindex="-1"
      aria-label="Cancel delete"
      onclick={cancelDelete}
      class="absolute inset-0 cursor-default border-none bg-transparent p-0"
    ></button>
    <div
      role="dialog"
      aria-modal="true"
      aria-label="Confirm delete"
      tabindex="-1"
      class="relative w-full max-w-sm glass-palette glass-palette-strong border border-surface-modal-border rounded-xl shadow-2xl overflow-hidden"
    >
      <div class="px-5 py-4 border-b border-surface-modal-border">
        <h2
          class="font-headline-md text-headline-md text-text-primary flex items-center gap-2"
        >
          <span
            class="material-symbols-outlined text-icon-lg {deleteTargetDisposition ===
            'trash'
              ? 'text-status-danger'
              : deleteTargetDisposition === 'permanent'
                ? 'text-status-warn'
                : 'text-status-danger'}"
            aria-hidden="true"
            >{deleteTargetDisposition === 'unlink'
              ? 'link_off'
              : deleteTargetDisposition === 'permanent'
                ? 'warning'
                : 'delete'}</span
          >
          {#if deleteTargetDisposition === 'unlink'}
            Unlink Notebook?
          {:else if deleteTargetDisposition === 'permanent'}
            Permanently delete {deleteTarget.level}?
          {:else}
            Delete {deleteTarget.level}?
          {/if}
        </h2>
        <p class="text-text-muted text-type-sm font-body-md mt-1">
          {#if deleteTargetDisposition === 'unlink'}
            Unlinking <strong>{deleteTarget.label}</strong> stops indexing it.
            Its files are left <strong>completely untouched</strong> — re-link the
            folder later to index it again.
          {:else if deleteTargetDisposition === 'permanent'}
            This will <strong>permanently delete</strong>
            {deleteTarget.label} from the external linked folder. It is
            <strong>not</strong> moved to vault
            <code>.system/trash/</code> and cannot be recovered from Silt.
          {:else}
            This will move the {deleteTarget.label} to
            <code>.system/trash/</code>. You can recover it from there manually.
          {/if}
        </p>
      </div>
      <div class="flex items-center justify-end gap-2 px-5 py-3">
        <button
          onclick={cancelDelete}
          class="px-4 py-2 rounded-lg text-text-muted hover:text-text-primary font-label-sm-bold transition-colors border-none bg-transparent cursor-pointer"
        >
          Cancel
        </button>
        <button
          onclick={confirmDelete}
          class="px-4 py-2 rounded-lg bg-status-danger/20 border border-status-danger/40 text-status-danger font-label-sm-bold hover:brightness-110 transition-all cursor-pointer"
        >
          {deleteTargetDisposition === 'unlink'
            ? 'Unlink'
            : deleteTargetDisposition === 'permanent'
              ? 'Delete permanently'
              : 'Delete'}
        </button>
      </div>
    </div>
  </div>
{/if}

<style>
  :global(.drag-over-top) {
    box-shadow: inset 0 2px 0 var(--color-accent-primary-start);
  }
  :global(.drag-over-bottom) {
    box-shadow: inset 0 -2px 0 var(--color-accent-primary-start);
  }
  :global(.drag-over-into) {
    background: color-mix(
      in srgb,
      var(--color-accent-primary-start) 18%,
      transparent
    );
    box-shadow: inset 0 0 0 1px var(--color-accent-primary-start);
    border-radius: 6px;
  }
  .pin-menu-filled {
    font-variation-settings:
      'FILL' 1,
      'wght' 400,
      'GRAD' 0,
      'opsz' 20;
  }
  .tab-icon {
    font-size: 14px;
    font-variation-settings:
      'FILL' 0,
      'wght' 300,
      'GRAD' 0,
      'opsz' 20;
  }
</style>
