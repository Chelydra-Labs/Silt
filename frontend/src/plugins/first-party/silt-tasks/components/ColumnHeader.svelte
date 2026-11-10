<script lang="ts">
  // Presentational board column header. Lifted out of BoardView to slim the
  // parent; the board still owns all menu/rename/WIP-edit state and passes
  // pre-bound callbacks + bindable inputs (rename value, WIP draft). The
  // action menu renders through the shared ContextMenu primitive (#1121):
  // the ⋯ button, right-click, and the context-menu key all open the same
  // items; the primitive owns positioning, dismissal, roving focus, and
  // focus return to the ⋯ button.
  import { tick } from 'svelte'
  import ContextMenu from '../../../../components/ContextMenu.svelte'
  import {
    isContextMenuKey,
    openContextMenuFromKeyboard
  } from '../../../../lib/contextMenuKey'
  import { formatEstimateSum } from '../types'

  interface Props {
    colKey: string
    colLabel: string
    colValue: string
    cardCount: number
    estimateSum: number
    canManage: boolean
    wipLimit: number | null
    overWip: boolean
    dndEnabled: boolean
    renaming: boolean
    /** Open-menu anchor (pointer/button coords); null = closed. */
    menu: { x: number; y: number } | null
    wipEditing: boolean
    wipEditError: string
    renameValue: string
    wipDraft: string
    onOpenMenu: (x: number, y: number) => void
    onCloseMenu: () => void
    onStartRename: () => void
    onCommitRename: () => void
    onCancelRename: () => void
    onRemoveColumn: () => void
    onStartWipEdit: () => void
    onApplyWipLimit: () => void
    onClearWipLimit: () => void
    onWipEscape: () => void
    onColDragStart: (e: DragEvent) => void
    onColDragOver: (e: DragEvent) => void
    onColDrop: (e: DragEvent) => void
    onColDragEnd: () => void
  }

  let {
    colKey,
    colLabel,
    colValue,
    cardCount,
    estimateSum,
    canManage,
    wipLimit,
    overWip,
    dndEnabled,
    renaming,
    menu,
    wipEditing,
    wipEditError,
    renameValue = $bindable(),
    wipDraft = $bindable(),
    onOpenMenu,
    onCloseMenu,
    onStartRename,
    onCommitRename,
    onCancelRename,
    onRemoveColumn,
    onStartWipEdit,
    onApplyWipLimit,
    onClearWipLimit,
    onWipEscape,
    onColDragStart,
    onColDragOver,
    onColDrop,
    onColDragEnd
  }: Props = $props()

  // The ⋯ button doubles as the menu's anchorEl: focus returns to it on
  // dismiss and scroll-dismissal scopes to its scrollable ancestor.
  let menuBtnEl = $state<HTMLButtonElement | null>(null)
  let renameInputEl = $state<HTMLInputElement | null>(null)
  let wipInputEl = $state<HTMLInputElement | null>(null)
  let wipItemEl = $state<HTMLButtonElement | null>(null)

  function onMenuButtonClick(e: MouseEvent) {
    // Button-as-toggle (SavedViews shape): clicking while this column's menu
    // is open closes it instead of re-anchoring.
    if (menu) {
      onCloseMenu()
      return
    }
    const rect = (e.currentTarget as HTMLElement).getBoundingClientRect()
    onOpenMenu(rect.left, rect.bottom + 2)
  }

  function onHeaderContextMenu(e: MouseEvent) {
    e.preventDefault()
    onOpenMenu(e.clientX, e.clientY)
  }

  function onMenuButtonKeydown(e: KeyboardEvent) {
    if (!isContextMenuKey(e)) return
    // Synthesized contextmenu on the ⋯ button bubbles to the header's own
    // handler, so mouse and keyboard share one open path.
    e.preventDefault()
    openContextMenuFromKeyboard(e.currentTarget as HTMLElement)
  }

  // Both inline editors replace an element that held focus (the menu items
  // or the label), so focus is steered explicitly; without this it falls to
  // <body> and keyboard users lose the editor.
  $effect(() => {
    if (renaming) {
      void tick().then(() => {
        renameInputEl?.focus()
        renameInputEl?.select()
      })
    }
  })
  $effect(() => {
    if (wipEditing && menu) {
      void tick().then(() => wipInputEl?.focus())
    }
  })
</script>

<div
  class="flex min-h-11 items-center justify-between border-b border-surface-panel-border bg-surface-panel px-3 py-2.5"
  role="group"
  aria-label={`${colLabel} column header`}
  draggable={canManage ? 'true' : undefined}
  ondragstart={canManage ? (e) => onColDragStart(e) : undefined}
  ondragover={canManage ? (e) => onColDragOver(e) : undefined}
  ondrop={canManage ? (e) => onColDrop(e) : undefined}
  ondragend={canManage ? onColDragEnd : undefined}
  oncontextmenu={canManage ? onHeaderContextMenu : undefined}
  title={!dndEnabled ? `Task location can't be changed by dragging` : undefined}
>
  <div class="flex items-center gap-2 min-w-0">
    {#if canManage}
      <span
        class="material-symbols-outlined text-icon-sm text-text-muted/50 hover:text-text-muted cursor-grab active:cursor-grabbing shrink-0 transition-colors"
        title="Drag to reorder"
        spellcheck="false">drag_indicator</span
      >
    {/if}
    <span
      class="w-2 h-2 rounded-full shrink-0 shadow-sm"
      class:bg-text-muted={colValue !== 'DOING' && colValue !== 'DONE'}
      class:bg-accent-secondary-start={colValue === 'DOING'}
      class:bg-accent-primary-start={colValue === 'DONE'}
    ></span>
    {#if renaming}
      <input
        type="text"
        bind:this={renameInputEl}
        bind:value={renameValue}
        onkeydown={(e) => {
          if (e.key === 'Enter') onCommitRename()
          else if (e.key === 'Escape') onCancelRename()
        }}
        onblur={() => onCommitRename()}
        class="bg-surface-panel border border-accent-primary-start/40 rounded px-1.5 py-0.5 text-type-xs font-label-sm-bold uppercase tracking-widest text-text-primary outline-none w-28"
        aria-label="Rename column"
      />
    {:else}
      <h2
        class="truncate text-type-xs font-label-sm-bold uppercase tracking-widest text-text-primary"
        title={canManage ? 'Double-click to rename' : colLabel}
        ondblclick={canManage ? onStartRename : undefined}
      >
        {colLabel}
      </h2>
    {/if}
    {#if wipLimit != null}
      <span
        class="border border-surface-panel-border bg-surface-card text-type-2xs px-1.5 py-0.5 rounded-full font-label-sm {overWip
          ? 'text-status-warn'
          : 'text-text-muted'}"
        data-testid={`board-wip-badge-${colKey}`}
        title={overWip
          ? `Over WIP limit (${cardCount} / ${wipLimit})`
          : `WIP limit ${cardCount} / ${wipLimit}`}
        aria-label={`${cardCount} of ${wipLimit} WIP${overWip ? ', over limit' : ''}`}
      >
        {cardCount} / {wipLimit}
      </span>
    {:else}
      <span
        class="border border-surface-panel-border bg-surface-card text-text-muted text-type-2xs px-1.5 py-0.5 rounded-full font-label-sm"
        >{cardCount}</span
      >
    {/if}
    {#if estimateSum > 0}
      <span
        class="text-text-muted/60 text-type-2xs font-label-sm truncate"
        data-testid={`board-col-estimate-${colKey}`}
        title={`${formatEstimateSum(estimateSum)} estimated`}
      >
        {formatEstimateSum(estimateSum)} estimated
      </span>
    {/if}
  </div>
  <div class="shrink-0 flex items-center">
    {#if canManage}
      <button
        type="button"
        bind:this={menuBtnEl}
        onclick={onMenuButtonClick}
        onkeydown={onMenuButtonKeydown}
        aria-label="Column actions"
        aria-expanded={menu !== null}
        aria-haspopup="menu"
        aria-keyshortcuts="Shift+F10"
        class="flex h-7 w-7 items-center justify-center rounded-md border border-transparent text-text-muted hover:border-surface-panel-border hover:bg-hover hover:text-text-primary transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-border-focus"
      >
        <span class="material-symbols-outlined text-icon-md">more_horiz</span>
      </button>
      <ContextMenu
        open={menu !== null}
        anchor={menu}
        anchorEl={menuBtnEl}
        onClose={onCloseMenu}
        ariaLabel={`Actions for ${colLabel} column`}
        backdropTestId={`board-col-menu-backdrop-${colKey}`}
        menuTestId={`board-col-menu-${colKey}`}
      >
        <button
          type="button"
          role="menuitem"
          onclick={onStartRename}
          data-testid={`board-rename-menu-${colKey}`}
        >
          <span
            class="material-symbols-outlined text-icon-md"
            aria-hidden="true">edit</span
          >
          <span>Rename</span>
        </button>
        {#if wipEditing}
          <div
            class="px-2.5 py-2 space-y-1.5"
            data-testid={`board-wip-edit-${colKey}`}
          >
            <label
              class="block text-type-2xs font-label-sm text-text-muted"
              for={`wip-input-${colKey}`}
            >
              WIP limit (empty = unlimited)
            </label>
            <input
              id={`wip-input-${colKey}`}
              bind:this={wipInputEl}
              type="number"
              min="1"
              step="1"
              class="w-full rounded border border-surface-card-border bg-surface-card px-2 py-1 text-type-sm text-text-primary"
              bind:value={wipDraft}
              aria-invalid={wipEditError ? 'true' : undefined}
              data-testid={`board-wip-input-${colKey}`}
              onkeydown={(e) => {
                // The input owns its keys: stopPropagation keeps the
                // primitive's menu roving (arrows/Home/End) from stealing
                // focus and Escape from closing the whole menu.
                if (e.key === 'Enter') {
                  e.preventDefault()
                  e.stopPropagation()
                  onApplyWipLimit()
                } else if (e.key === 'Escape') {
                  e.preventDefault()
                  e.stopPropagation()
                  onWipEscape()
                  void tick().then(() => wipItemEl?.focus())
                } else if (
                  e.key.startsWith('Arrow') ||
                  e.key === 'Home' ||
                  e.key === 'End'
                ) {
                  e.stopPropagation()
                }
              }}
            />
            {#if wipEditError}
              <p class="text-type-2xs text-error m-0" role="alert">
                {wipEditError}
              </p>
            {/if}
            <div class="flex gap-1 pt-0.5">
              <button
                type="button"
                class="flex-1 px-2 py-1 rounded bg-accent-primary-start/15 text-accent-primary-start text-type-2xs font-label-sm"
                data-testid={`board-wip-apply-${colKey}`}
                onclick={onApplyWipLimit}
              >
                Apply
              </button>
              <button
                type="button"
                class="flex-1 px-2 py-1 rounded hover:bg-hover text-type-2xs font-label-sm text-text-muted"
                data-testid={`board-wip-clear-${colKey}`}
                onclick={onClearWipLimit}
              >
                Clear
              </button>
            </div>
          </div>
        {:else}
          <button
            type="button"
            role="menuitem"
            bind:this={wipItemEl}
            onclick={onStartWipEdit}
            data-testid={`board-wip-menu-${colKey}`}
          >
            <span
              class="material-symbols-outlined text-icon-md"
              aria-hidden="true">speed</span
            >
            <span>WIP limit…</span>
          </button>
        {/if}
        <button
          type="button"
          role="menuitem"
          class="text-status-danger"
          onclick={onRemoveColumn}
          data-testid={`board-remove-menu-${colKey}`}
        >
          <span
            class="material-symbols-outlined text-icon-md"
            aria-hidden="true">delete</span
          >
          <span>Remove</span>
        </button>
      </ContextMenu>
    {/if}
  </div>
</div>
