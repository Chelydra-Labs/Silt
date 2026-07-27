<script lang="ts">
  // Saved Views section of the Tasks sidebar (#432, #763). Lifted verbatim
  // from the god-file Sidebar.svelte: system + user views with fingerprint
  // highlight, inline rename, manage menu (⋯ + right-click), delete-confirm,
  // and drag/keyboard reorder.
  //
  // Reads the singleton hub state directly (state.svelte.ts) — no prop
  // drilling, no ctx (persistence flows through the settings module).
  import { tick } from 'svelte'
  import {
    getTaskHubState,
    applySavedView,
    deleteSavedView,
    saveView,
    reorderSavedViews,
    type SavedView
  } from '../state.svelte'
  import { viewMatchesState } from '../savedViews'
  import { persistSavedViews } from '../settings'
  import ContextMenu from '../../../../components/ContextMenu.svelte'
  import ConfirmModal from '../components/ConfirmModal.svelte'
  import ErrorBanner from '../components/ErrorBanner.svelte'
  import { isDevMode, openInspect } from '../../../../lib/devModeInspect'

  let hubState = $derived(getTaskHubState())
  let errorMsg = $state('')

  function activateView(view: SavedView) {
    applySavedView(view)
  }

  // Inline rename state (#470).
  let renamingId = $state<string | null>(null)
  let renameValue = $state('')
  let renameError = $state('')
  let renameInputEl = $state<HTMLInputElement | null>(null)

  // Manage-menu (⋯ button + right-click share the same menu).
  let manageMenu = $state<{
    viewId: string
    x: number
    y: number
    anchorEl: HTMLElement | null
  } | null>(null)

  // Delete-confirmation modal target.
  let deleteTarget = $state<SavedView | null>(null)

  // Drag-and-drop reorder state.
  let dragId = $state<string | null>(null)
  let dropTarget = $state<{ id: string; before: boolean } | null>(null)

  let userViews = $derived(hubState.savedViews.filter((v) => !v.system))

  function openManageMenu(
    view: SavedView,
    x: number,
    y: number,
    anchorEl: HTMLElement | null
  ) {
    if (view.system) return
    // Store the raw anchor; the $effect below clamps it to the viewport using
    // the menu's real rendered dimensions (accurate, vs. the former hardcoded
    // 180×220 estimate that could mis-clamp).
    manageMenu = { viewId: view.id, x, y, anchorEl }
  }

  function openManageMenuFromButton(e: MouseEvent, view: SavedView) {
    if (view.system) return
    e.stopPropagation()
    const rect = (e.currentTarget as HTMLElement).getBoundingClientRect()
    // If the same menu is already open, toggle it closed (button-as-toggle).
    if (manageMenu?.viewId === view.id) {
      manageMenu = null
      return
    }
    openManageMenu(
      view,
      rect.left,
      rect.bottom + 2,
      e.currentTarget as HTMLElement
    )
  }

  function onRowContextMenu(e: MouseEvent, view: SavedView) {
    if (view.system) return // system views: let the browser menu show
    e.preventDefault()
    openManageMenu(view, e.clientX, e.clientY, e.currentTarget as HTMLElement)
  }

  function closeManageMenu() {
    manageMenu = null
  }

  function startRename(view: SavedView) {
    if (view.system) return
    closeManageMenu()
    renamingId = view.id
    renameValue = view.name
    renameError = ''
    void tick().then(() => {
      renameInputEl?.focus()
      renameInputEl?.select()
    })
  }

  function cancelRename() {
    renamingId = null
    renameValue = ''
    renameError = ''
  }

  async function commitRename() {
    const id = renamingId
    if (!id) return
    const name = renameValue.trim()
    if (!name) {
      renameError = 'Enter a view name'
      return
    }
    // Exit rename mode synchronously so the blur handler can't double-fire.
    renamingId = null
    const view = getTaskHubState().savedViews.find((v) => v.id === id)
    if (!view) return
    saveView({ ...view, name })
    renameError = ''
    errorMsg = ''
    const ok = await persistSavedViews(getTaskHubState().savedViews)
    if (!ok) errorMsg = 'Saved Views: Failed to save view'
  }

  // Overwrite the view's stored dimensions with the current hub state
  // (mirrors TasksHub.commitUpdateActive). Only meaningful for the active
  // dirty view — the menu only offers it in that state.
  async function overwriteView(view: SavedView) {
    if (view.system) return
    closeManageMenu()
    const s = getTaskHubState()
    const updated: SavedView = {
      id: view.id,
      name: view.name,
      displayMode: s.displayMode,
      groupBy: s.groupBy,
      sort: s.sort,
      scope: s.scope,
      filters: {
        owners: [...s.filters.owners],
        priorities: [...s.filters.priorities],
        dueDate: s.filters.dueDate,
        tags: [...s.filters.tags],
        stale: s.filters.stale
      },
      calendarSubMode: s.calendarSubMode,
      columns: s.columns.map((c) => ({ ...c })),
      system: false
    }
    saveView(updated)
    applySavedView(updated) // clears the dirty flag
    errorMsg = ''
    const ok = await persistSavedViews(getTaskHubState().savedViews)
    if (!ok) errorMsg = 'Saved Views: Failed to save view'
  }

  function requestDelete(view: SavedView) {
    if (view.system) return
    closeManageMenu()
    deleteTarget = view
  }

  function cancelDelete() {
    deleteTarget = null
  }

  async function confirmDelete() {
    const view = deleteTarget
    if (!view) return
    deleteTarget = null
    if (view.system) return
    errorMsg = ''
    // Capture before the in-memory delete so a persist failure can restore it
    // — without this, the view vanishes from the UI but survives on disk.
    const viewToRemove = getTaskHubState().savedViews.find(
      (v) => v.id === view.id
    )
    deleteSavedView(view.id)
    const ok = await persistSavedViews(getTaskHubState().savedViews)
    if (!ok && viewToRemove) {
      saveView(viewToRemove)
      errorMsg =
        'Saved Views: Delete failed — the view will reappear on next launch.'
    } else if (!ok) {
      errorMsg =
        'Saved Views: Failed to delete view — will retry on next launch'
    }
  }

  async function persistViewList() {
    errorMsg = ''
    const ok = await persistSavedViews(getTaskHubState().savedViews)
    if (!ok) errorMsg = 'Saved Views: Failed to save view order'
  }

  // --- Reorder (drag + keyboard move) ------------------------------------

  function onViewDragStart(e: DragEvent, view: SavedView) {
    if (view.system) {
      e.preventDefault()
      return
    }
    dragId = view.id
    if (e.dataTransfer) {
      e.dataTransfer.effectAllowed = 'move'
      e.dataTransfer.setData('text/plain', view.id)
    }
  }

  function onViewDragOver(e: DragEvent, view: SavedView) {
    if (!dragId || view.system || view.id === dragId) return
    e.preventDefault()
    if (e.dataTransfer) e.dataTransfer.dropEffect = 'move'
    const rect = (e.currentTarget as HTMLElement).getBoundingClientRect()
    dropTarget = {
      id: view.id,
      before: e.clientY < rect.top + rect.height / 2
    }
  }

  async function onViewDrop(e: DragEvent, view: SavedView) {
    e.preventDefault()
    const fromId = dragId
    const before = dropTarget?.before ?? false
    dragId = null
    dropTarget = null
    if (!fromId || view.system || fromId === view.id) return
    reorderSavedViews(fromId, view.id, before)
    await persistViewList()
  }

  function onViewDragEnd() {
    dragId = null
    dropTarget = null
  }

  async function moveView(view: SavedView, direction: -1 | 1) {
    const list = getTaskHubState().savedViews.filter((v) => !v.system)
    const idx = list.findIndex((v) => v.id === view.id)
    if (idx < 0) return
    const swapIdx = idx + direction
    if (swapIdx < 0 || swapIdx >= list.length) return
    reorderSavedViews(
      view.id,
      list[swapIdx].id,
      direction === -1 // up → land before the predecessor
    )
    closeManageMenu()
    await persistViewList()
  }

  function canMoveUp(view: SavedView): boolean {
    const idx = userViews.findIndex((v) => v.id === view.id)
    return idx > 0
  }
  function canMoveDown(view: SavedView): boolean {
    const idx = userViews.findIndex((v) => v.id === view.id)
    return idx >= 0 && idx < userViews.length - 1
  }

  // Keep the menu's manageMenu referencing a live view — if the list changes
  // underneath (e.g. the view was deleted from elsewhere), close the menu.
  let manageMenuView = $derived.by(() => {
    const m = manageMenu
    if (!m) return undefined
    return hubState.savedViews.find((v) => v.id === m.viewId)
  })
</script>

<!-- Saved Views (lifted from KanbanSidebar; management UX #470) -->
<section aria-labelledby="tasks-saved-views-heading">
  <h3
    id="tasks-saved-views-heading"
    class="px-2 font-label-sm-bold uppercase tracking-widest text-type-2xs text-text-muted"
  >
    Saved Views
  </h3>
  <ul role="list" class="mt-1 space-y-0.5">
    {#each hubState.savedViews as view (view.id)}
      {@const isActive = viewMatchesState(view, hubState)}
      {@const isRenaming = renamingId === view.id}
      {@const isUser = view.system !== true}
      {@const isDragging = dragId === view.id}
      {@const isDropBefore = dropTarget?.id === view.id && dropTarget.before}
      {@const isDropAfter = dropTarget?.id === view.id && !dropTarget.before}
      <li
        class="group relative"
        data-testid={`view-row-${view.id}`}
        oncontextmenu={(e) => onRowContextMenu(e, view)}
        ondragover={(e) => onViewDragOver(e, view)}
        ondrop={(e) => onViewDrop(e, view)}
      >
        <div
          class="flex items-center gap-0.5 px-1 py-0.5 rounded text-type-sm font-body-md border transition-colors
            {isActive
            ? 'bg-accent-primary-glow border-accent-primary-start/30 text-accent-primary-start'
            : 'text-text-primary hover:bg-hover border-transparent'}
            {isDragging ? 'opacity-40' : ''}
            {isDropBefore
            ? 'border-t-2 border-t-accent-primary-start border-b-transparent'
            : ''}
            {isDropAfter
            ? 'border-b-2 border-b-accent-primary-start border-t-transparent'
            : ''}"
          data-testid={`view-${view.id}`}
        >
          {#if isUser}
            <span
              draggable="true"
              ondragstart={(e) => onViewDragStart(e, view)}
              ondragend={onViewDragEnd}
              class="flex items-center text-text-muted/30 group-hover:text-text-muted cursor-grab active:cursor-grabbing touch-none"
              title="Drag to reorder"
              aria-hidden="true"
              data-testid={`grip-${view.id}`}
            >
              <span class="material-symbols-outlined text-icon-sm"
                >drag_indicator</span
              >
            </span>
          {/if}

          {#if isRenaming}
            <label class="flex-1 sr-only" for={`rename-${view.id}`}>
              Rename {view.name}
            </label>
            <input
              id={`rename-${view.id}`}
              bind:this={renameInputEl}
              bind:value={renameValue}
              data-testid={`rename-input-${view.id}`}
              onkeydown={(e) => {
                if (e.key === 'Enter') {
                  e.preventDefault()
                  void commitRename()
                } else if (e.key === 'Escape') {
                  e.preventDefault()
                  cancelRename()
                }
              }}
              onblur={() => {
                // Commit on blur if a non-empty name was entered; cancel
                // otherwise. commitRename exits rename mode synchronously,
                // so a re-entrant blur is a no-op.
                if (renamingId) {
                  if (renameValue.trim()) void commitRename()
                  else cancelRename()
                }
              }}
              class="flex-1 min-w-0 px-1.5 py-1 rounded bg-surface-panel border border-accent-primary-start text-text-primary text-type-sm outline-none"
            />
            {#if renameError}
              <span class="sr-only" role="alert">{renameError}</span>
            {/if}
          {:else}
            <button
              type="button"
              onclick={() => activateView(view)}
              aria-pressed={isActive}
              class="flex-1 min-w-0 text-left px-1.5 py-1 rounded cursor-pointer border-none bg-transparent"
            >
              <span class="truncate inline-block max-w-full align-middle"
                >{view.name}</span
              >
              {#if view.id === hubState.activeSavedViewId && hubState.savedViewsDirty}
                <span
                  class="inline-block w-1.5 h-1.5 rounded-full bg-accent-secondary-start ml-1"
                  title="This view has unsaved changes"
                  aria-label="modified"
                ></span>
              {/if}
            </button>
          {/if}

          {#if view.system}
            <span
              class="material-symbols-outlined text-type-xs text-text-muted/50"
              aria-label="Built-in view"
              title="Built-in view — can't be modified">lock</span
            >
          {/if}

          {#if isUser}
            <button
              type="button"
              onclick={(e) => openManageMenuFromButton(e, view)}
              aria-haspopup="menu"
              aria-expanded={manageMenu?.viewId === view.id}
              aria-label="Manage view {view.name}"
              data-testid={`manage-view-${view.id}`}
              class="p-1 rounded text-text-muted hover:text-text-primary hover:bg-hover border-none bg-transparent cursor-pointer transition-opacity
                {manageMenu?.viewId === view.id
                ? 'opacity-100'
                : 'opacity-0 group-hover:opacity-100 focus-visible:opacity-100'}"
            >
              <span class="material-symbols-outlined text-icon-sm"
                >more_horiz</span
              >
            </button>
          {/if}
        </div>
      </li>
    {/each}
  </ul>
</section>

{#if errorMsg}
  <ErrorBanner message={errorMsg} compact />
{/if}

<!-- Saved-view manage menu (#470). The ⋯ button and right-click both feed
     into the same `manageMenu` state; delegates to the shared ContextMenu
     component (#491) for positioning, dismissal, keyboard nav, and chrome. -->
{#if manageMenu && manageMenuView}
  {@const v = manageMenuView}
  {@const canUpdate =
    hubState.activeSavedViewId === v.id && hubState.savedViewsDirty}
  <ContextMenu
    open={manageMenu !== null && manageMenuView !== undefined}
    anchor={{ x: manageMenu.x, y: manageMenu.y }}
    anchorEl={manageMenu?.anchorEl ?? null}
    onClose={closeManageMenu}
    ariaLabel={`Actions for ${v.name}`}
    backdropTestId="manage-view-backdrop"
    menuTestId="manage-view-menu"
  >
    {#if canUpdate}
      <button
        type="button"
        role="menuitem"
        onclick={() => void overwriteView(v)}
        data-testid="manage-update-view"
      >
        <span class="material-symbols-outlined text-icon-md" aria-hidden="true"
          >save</span
        >
        <span>Update "{v.name}"</span>
      </button>
    {/if}
    <button
      type="button"
      role="menuitem"
      onclick={() => startRename(v)}
      data-testid="manage-rename-view"
    >
      <span class="material-symbols-outlined text-icon-md" aria-hidden="true"
        >edit</span
      >
      <span>Rename…</span>
    </button>
    <button
      type="button"
      role="menuitem"
      disabled={!canMoveUp(v)}
      aria-disabled={!canMoveUp(v)}
      onclick={() => void moveView(v, -1)}
      data-testid="manage-move-up"
      class="disabled:opacity-40 disabled:cursor-not-allowed"
    >
      <span class="material-symbols-outlined text-icon-md" aria-hidden="true"
        >arrow_upward</span
      >
      <span>Move up</span>
    </button>
    <button
      type="button"
      role="menuitem"
      disabled={!canMoveDown(v)}
      aria-disabled={!canMoveDown(v)}
      onclick={() => void moveView(v, 1)}
      data-testid="manage-move-down"
      class="disabled:opacity-40 disabled:cursor-not-allowed"
    >
      <span class="material-symbols-outlined text-icon-md" aria-hidden="true"
        >arrow_downward</span
      >
      <span>Move down</span>
    </button>
    <div class="context-menu-separator" aria-hidden="true"></div>
    <button
      type="button"
      role="menuitem"
      onclick={() => requestDelete(v)}
      data-testid="manage-delete-view"
      class="text-status-danger"
    >
      <span class="material-symbols-outlined text-icon-md" aria-hidden="true"
        >delete</span
      >
      <span>Delete…</span>
    </button>
    {#if isDevMode()}
      <div class="context-menu-separator" aria-hidden="true"></div>
      <button
        type="button"
        role="menuitem"
        data-testid="manage-inspect"
        onclick={() => {
          closeManageMenu()
          void openInspect()
        }}
      >
        <span class="material-symbols-outlined text-icon-md" aria-hidden="true"
          >bug_report</span
        >
        <span>Inspect</span>
      </button>
    {/if}
  </ContextMenu>
{/if}

<!-- Delete confirmation (#470). In-app modal replaces window.confirm(). -->
{#if deleteTarget}
  <ConfirmModal
    title="Delete saved view?"
    message={`Delete “${deleteTarget.name}”? This view will be removed from your saved views.`}
    confirmLabel="Delete"
    cancelLabel="Cancel"
    destructive={true}
    dataTestId="delete-view-confirm"
    onConfirm={() => void confirmDelete()}
    onCancel={cancelDelete}
  />
{/if}
