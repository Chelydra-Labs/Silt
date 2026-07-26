<script lang="ts">
  // Notebook navigation experience: selector dropdown, primary create actions,
  // the tree/quick-access tab switcher, and the notebook›section›page tree.
  // Extracted verbatim from Sidebar.svelte; all markup, styling, a11y, and
  // interaction behavior is preserved. State + handlers that serve only this
  // surface live here; shared controllers (loader/crud/menu/dnd) are threaded
  // from the host so a single source of truth is maintained.
  import { SvelteSet } from 'svelte/reactivity'
  import { onDestroy, tick, untrack } from 'svelte'
  import SidebarSection from '../SidebarSection.svelte'
  import SidebarQuickAccess from '../SidebarQuickAccess.svelte'
  import { sortByName } from '../../lib/sidebar/navOrder'
  import {
    pageNodeId,
    sectionNodeId,
    visibleTreeNodes
  } from '../../lib/sidebar/navTree'
  import {
    expandActiveAncestors,
    expandedPathsForNotebook,
    locatorKey
  } from '../../lib/sidebar/navigationPreferences'
  import type { NavigationPageRef } from '../../lib/sidebar/types'
  import type { NavLoader } from '../../lib/sidebar/useNavLoader.svelte'
  import type { NavCrud } from '../../lib/sidebar/useNavCrud.svelte'
  import type { SidebarContextMenu } from '../../lib/sidebar/useSidebarContextMenu.svelte'
  import {
    DragDropManager,
    type DragItem,
    type DropTarget
  } from '../../lib/sidebar/useDragDrop'
  import { shortcutBinding } from '../../settings/shortcutActions'
  import { settings } from '../../settings/store.svelte'

  interface Props {
    activeNotebook: string
    activeSection: string
    activePage: string
    showNotebookDropdown: boolean
    dropTarget: DropTarget | null
    dragItem: DragItem | null
    // Bound up to the host: handleSelectNotebook (a CRUD dep, so it lives in
    // the host) writes focus on notebook switch, while this component owns
    // keyboard-driven focus + the active-node tracking effect.
    focusedTreeItemId: string
    loader: NavLoader
    crud: NavCrud
    menu: SidebarContextMenu
    dnd: DragDropManager
    onSelectNotebook: (notebook: string) => void
    onSelectSection: (section: string) => void
    onSelectPage: (notebook: string, section: string, page: string) => void
    onPinPage: (notebook: string, section: string, page: string) => void
    /** Host-owned hybrid handler (writes active props + expansion); also a
     *  CRUD dep, so it stays in the host and is threaded back in. */
    handleSelectNotebook: (notebook: string) => void
  }

  let {
    activeNotebook = $bindable(),
    activeSection = $bindable(),
    activePage = $bindable(),
    showNotebookDropdown = $bindable(),
    dropTarget = $bindable(),
    dragItem,
    focusedTreeItemId = $bindable(''),
    loader: loaderProp,
    crud: crudProp,
    menu: menuProp,
    dnd: dndProp,
    onSelectNotebook,
    onSelectSection,
    onSelectPage,
    onPinPage,
    handleSelectNotebook
  }: Props = $props()

  // The host constructs these controller instances once and never swaps them,
  // so read them untracked into stable locals — method/handler aliases below
  // would otherwise trip state_referenced_locally on the reactive prop refs.
  const loader = untrack(() => loaderProp)
  const crud = untrack(() => crudProp)
  const menu = untrack(() => menuProp)
  const dnd = untrack(() => dndProp)

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
  // The template reads these by bare name; each aliases a composable getter
  // so Svelte 5 reactivity is preserved (destructuring would snapshot).
  let tree = $derived(loader.tree)
  let navigationLoading = $derived(loader.navigationLoading)
  let navigationError = $derived(loader.navigationError)
  let preferences = $derived(loader.preferences)
  let preferencesLoading = $derived(loader.preferencesLoading)
  let preferencesError = $derived(loader.preferencesError)
  let sidebarTab = $derived(loader.sidebarTab)
  let expandedSections = $derived(loader.expandedSections)
  let navOrder = $derived(loader.navOrder)
  let favoriteState = $derived(loader.favoriteState)
  let recentState = $derived(loader.recentState)

  let creating = $derived(crud.creating)

  const loadNavigation = loader.loadNavigation
  const loadNavigationPreferences = loader.loadNavigationPreferences
  const toggleSection = loader.toggleSection
  const setSidebarTab = loader.setSidebarTab
  const toggleFavorite = loader.toggleFavorite
  const openCreate = crud.openCreate
  const handleCreatePageInline = crud.handleCreatePageInline
  const handleOpenNotebookFolder = crud.handleOpenNotebookFolder
  const handleLinkExternalNotebook = crud.handleLinkExternalNotebook
  let contextMenuTargetId = $derived(menu.contextMenuTargetId)
  const openContextMenu = menu.openContextMenu

  // Expanded section names (within the active notebook). The active section is
  // always expanded so the active path stays visible (spatial memory).
  let typeahead = ''
  let typeaheadTimer: ReturnType<typeof setTimeout> | null = null

  // Clear the typeahead timer if the nav tree unmounts mid-keystroke (e.g. on
  // view switch); the host's onMount used to own this cleanup.
  onDestroy(() => {
    if (typeaheadTimer) clearTimeout(typeaheadTimer)
  })

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

  // DnD handlers delegate to the manager. Start/Leave/Drop/DragEnd are bound
  // manager methods (signatures match SidebarSection's prop types exactly).
  // handleDragOver stays a 3-arg adapter: SidebarSection passes a 4th
  // `identity` arg for pages, but historic behavior routes section pages
  // through the manager's default targetIdentity (= name); passing identity
  // through would change the drop-target key and the drag-indicator matching.
  const handleDragStart = dnd.handleDragStart.bind(dnd)
  function handleDragOver(e: DragEvent, level: string, name: string) {
    dnd.handleDragOver(e, level, name)
  }
  const handleDragLeave = dnd.handleDragLeave.bind(dnd)
  const handleDrop = dnd.handleDrop.bind(dnd)
  const handleDragEnd = dnd.handleDragEnd.bind(dnd)
</script>

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
          <span class="material-symbols-outlined text-icon-lg">folder_open</span
          >
          <span class="font-label-sm text-label-sm">Open Notebook…</span>
        </button>
        <button
          onclick={handleLinkExternalNotebook}
          disabled={creating}
          title="Link a folder that lives outside the vault (e.g. a synced SharePoint mount); it is edited in place, never copied in."
          class="flex items-center gap-3 px-4 py-2 w-full text-left cursor-pointer hover:bg-hover transition-colors font-body-md border-none bg-transparent text-surface-sidebar-text-muted disabled:opacity-50"
        >
          <span class="material-symbols-outlined text-icon-lg">add_link</span>
          <span class="font-label-sm text-label-sm">Link External Folder…</span>
        </button>
      </div>
    </div>
  {/if}
</div>

<!-- Primary actions (icon-only, consistent style). Each button is wrapped
   in a span whose title gives the prerequisite reason — a native title on
   a disabled button doesn't show, but on the wrapper it does. -->
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
      <span class="material-symbols-outlined text-type-2xl">note_add</span>
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
      <span class="material-symbols-outlined text-type-2xl">content_copy</span>
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
          The page list could not be refreshed. Your previous list is still
          available.
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
              {@const isActive = activeSection === '' && activePage === pg.name}
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
                ondragstart={(e) => handleDragStart(e, 'page', pg.name, '')}
                ondragover={(e) =>
                  dnd.handleDragOver(e, 'page', pg.name, `\u0000${pg.name}`)}
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
                <span class="truncate flex-1" title={pg.name}>{pg.name}</span>
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

<style>
  .tab-icon {
    font-size: 14px;
    font-variation-settings:
      'FILL' 0,
      'wght' 300,
      'GRAD' 0,
      'opsz' 20;
  }
</style>
