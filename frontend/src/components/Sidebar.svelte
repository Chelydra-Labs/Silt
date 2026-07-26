<script lang="ts">
  import { SvelteSet } from 'svelte/reactivity'
  import { onMount } from 'svelte'
  import { Events } from '@wailsio/runtime'
  import SidebarPluginPanels from './sidebar/SidebarPluginPanels.svelte'
  import SidebarNavTree from './sidebar/SidebarNavTree.svelte'
  import PluginSidebarPanels from './PluginSidebarPanels.svelte'
  import { DragDropManager } from '../lib/sidebar/useDragDrop'
  import { useNavLoader } from '../lib/sidebar/useNavLoader.svelte'
  import { useNavCrud } from '../lib/sidebar/useNavCrud.svelte'
  import { useSidebarContextMenu } from '../lib/sidebar/useSidebarContextMenu.svelte'
  import type {
    NavigationPreferences,
    NavigationTree
  } from '../lib/sidebar/types'
  import { sectionNodeId } from '../lib/sidebar/navTree'
  import {
    expandedPathsForNotebook,
    locatorKey
  } from '../lib/sidebar/navigationPreferences'
  import ContextMenu from './ContextMenu.svelte'
  import NamePromptDialog from './NamePromptDialog.svelte'
  import { isDevMode, openInspect } from '../lib/devModeInspect'
  import { settings } from '../settings/store.svelte'
  import { shortcutBinding } from '../settings/shortcutActions'

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

  let showNotebookDropdown = $state(false)
  // Bound down to SidebarNavTree: handleSelectNotebook (below) writes focus on
  // notebook switch, while the tree owns keyboard-driven focus + the
  // active-node tracking effect.
  let focusedTreeItemId = $state('')

  // --- composables -------------------------------------------------------
  // Loader owns tree/preferences/expansion/tab; CRUD owns create/rename/delete
  // + action-prompt state; context-menu owns the right-click cluster. Each
  // is consumed by the host (dialogs/toasts/context-menu) and threaded into
  // SidebarNavTree for the tree surface.
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
  // indicators + the error toast); the manager delegates persistence to the
  // loader's nav-order manager and reloads via the loader after a move.
  // dragItem/dropTarget are bound down to SidebarNavTree, which renders the
  // draggable rows + the notebook-root drop zone.
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
    getActiveNotebookSections: () =>
      loader.tree.notebooks.find((nb) => nb.name === activeNotebook)
        ?.sections ?? [],
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

  // --- reactive aliases onto composable state ---------------------------
  // Read as properties to preserve Svelte 5 reactivity across the module
  // boundary (destructuring would snapshot). Only the aliases the host's own
  // template (toasts / dialogs / context menu) needs live here; the tree
  // aliases moved with SidebarNavTree.
  let favoriteKeys = $derived(loader.favoriteKeys)

  let createMode = $derived(crud.createMode)
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
  let contextUnavailable = $derived(menu.contextUnavailable)

  // Handler aliases (stable closures — safe to alias by reference).
  const closeNamePrompt = crud.closeNamePrompt
  const handleCreate = crud.handleCreate
  const namePromptTitle = crud.namePromptTitle
  const namePromptLabel = crud.namePromptLabel
  const namePromptPlaceholder = crud.namePromptPlaceholder
  const namePromptConfirmLabel = crud.namePromptConfirmLabel
  const confirmActionPrompt = crud.confirmActionPrompt
  const closeActionPrompt = crud.closeActionPrompt
  const confirmDelete = crud.confirmDelete
  const cancelDelete = crud.cancelDelete
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

  function shortcutSuffix(
    action: Parameters<typeof shortcutBinding>[0]
  ): string {
    const binding = shortcutBinding(action, settings.config?.hotkeys ?? {})
    return binding ? ` (${binding})` : ''
  }
  let hideSidebarHint = $derived(
    `Hide sidebar${shortcutSuffix('toggle_sidebar')}`
  )

  onMount(() => {
    void loader.loadNavigation()
    void loader.loadNavigationPreferences()
    void loader.loadNavOrder()
    const handleRefresh = () => {
      void loader.loadNavigation()
      void loader.loadNavigationPreferences()
      void loader.loadNavOrder()
    }
    const handleCreatePageInlineEvent = (e: Event) => {
      const sectionName =
        (e as CustomEvent).detail?.sectionName ?? activeSection ?? ''
      void crud.handleCreatePageInline(sectionName)
    }
    const handlePreferenceRefresh = () =>
      void loader.loadNavigationPreferences()
    const handleNavigationCreate = (event: Event) => {
      const kind = (event as CustomEvent).detail?.kind
      if (kind === 'notebook') crud.openCreate('notebook')
      else if (kind === 'section' && activeNotebook) crud.openCreate('section')
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
      () => void loader.loadNavigationPreferences()
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
    <SidebarPluginPanels
      {activeView}
      {activeNotebook}
      {activeSection}
      {activePage}
      bind:selectedTag
      bind:settingsSection
    >
      <SidebarNavTree
        bind:activeNotebook
        bind:activeSection
        bind:activePage
        bind:showNotebookDropdown
        bind:dropTarget
        bind:focusedTreeItemId
        {dragItem}
        {loader}
        {crud}
        {menu}
        {dnd}
        {onSelectSection}
        {onSelectPage}
        {onPinPage}
        {handleSelectNotebook}
      />
    </SidebarPluginPanels>
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
</style>
