<script lang="ts">
  // Presentational board column header. Lifted out of BoardView to slim the
  // parent; the board still owns all menu/rename/WIP-edit state and passes
  // pre-bound callbacks + bindable inputs (rename value, WIP draft).
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
    menuOpen: boolean
    wipEditing: boolean
    wipEditError: string
    renameValue: string
    wipDraft: string
    onToggleMenu: () => void
    onStartRename: () => void
    onCommitRename: () => void
    onCancelRename: () => void
    onRemoveColumn: () => void
    onStartWipEdit: () => void
    onApplyWipLimit: () => void
    onClearWipLimit: () => void
    onMenuEscape: () => void
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
    menuOpen,
    wipEditing,
    wipEditError,
    renameValue = $bindable(),
    wipDraft = $bindable(),
    onToggleMenu,
    onStartRename,
    onCommitRename,
    onCancelRename,
    onRemoveColumn,
    onStartWipEdit,
    onApplyWipLimit,
    onClearWipLimit,
    onMenuEscape,
    onWipEscape,
    onColDragStart,
    onColDragOver,
    onColDrop,
    onColDragEnd
  }: Props = $props()
</script>

<div
  class="flex items-center justify-between px-3 py-2.5 border-b border-surface-panel-border"
  role="group"
  aria-label={`${colLabel} column header`}
  draggable={canManage ? 'true' : undefined}
  ondragstart={canManage ? (e) => onColDragStart(e) : undefined}
  ondragover={canManage ? (e) => onColDragOver(e) : undefined}
  ondrop={canManage ? (e) => onColDrop(e) : undefined}
  ondragend={canManage ? onColDragEnd : undefined}
  title={!dndEnabled ? `Task location can't be changed by dragging` : undefined}
>
  <div class="flex items-center gap-2 min-w-0">
    {#if canManage}
      <span
        class="material-symbols-outlined text-icon-sm text-text-muted cursor-grab active:cursor-grabbing shrink-0"
        title="Drag to reorder"
        spellcheck="false">drag_indicator</span
      >
    {/if}
    <span
      class="w-2 h-2 rounded-full shrink-0"
      class:bg-text-muted={colValue !== 'DOING' && colValue !== 'DONE'}
      class:bg-accent-secondary-start={colValue === 'DOING'}
      class:bg-accent-primary-start={colValue === 'DONE'}
    ></span>
    {#if renaming}
      <input
        type="text"
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
        class="font-label-sm-bold uppercase tracking-widest text-type-xs text-text-muted truncate"
        title={canManage ? 'Double-click to rename' : colLabel}
        ondblclick={canManage ? onStartRename : undefined}
      >
        {colLabel}
      </h2>
    {/if}
    {#if wipLimit != null}
      <span
        class="bg-hover text-type-2xs px-1.5 py-0.5 rounded-sm font-label-sm {overWip
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
        class="bg-hover text-text-muted text-type-2xs px-1.5 py-0.5 rounded-sm font-label-sm"
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
  <div class="relative shrink-0 flex items-center">
    {#if canManage}
      <button
        type="button"
        onclick={onToggleMenu}
        aria-label="Column actions"
        aria-expanded={menuOpen}
        aria-haspopup="true"
        class="text-text-muted hover:text-text-primary transition-colors p-0.5"
      >
        <span class="material-symbols-outlined text-icon-md">more_horiz</span>
      </button>
      {#if menuOpen}
        <div
          class="absolute right-0 top-full mt-1 z-50 min-w-35 bg-surface-popover border border-surface-popover-border rounded-lg shadow-xl py-1"
          role="menu"
          tabindex="-1"
          onkeydown={(e) => {
            if (e.key === 'Escape') onMenuEscape()
          }}
        >
          <button
            type="button"
            onclick={onStartRename}
            class="w-full text-left flex items-center gap-2 px-3 py-1.5 hover:bg-hover text-type-sm font-label-sm text-text-primary"
            role="menuitem"
          >
            <span class="material-symbols-outlined text-icon-sm">edit</span>
            Rename
          </button>
          {#if wipEditing}
            <div
              class="px-3 py-2 space-y-1.5 border-t border-b border-surface-popover-border"
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
                type="number"
                min="1"
                step="1"
                class="w-full rounded border border-surface-card-border bg-surface-card px-2 py-1 text-type-sm text-text-primary"
                bind:value={wipDraft}
                aria-invalid={wipEditError ? 'true' : undefined}
                data-testid={`board-wip-input-${colKey}`}
                onkeydown={(e) => {
                  if (e.key === 'Enter') {
                    e.preventDefault()
                    onApplyWipLimit()
                  } else if (e.key === 'Escape') {
                    e.preventDefault()
                    e.stopPropagation()
                    onWipEscape()
                  }
                }}
              />
              {#if wipEditError}
                <p class="text-type-2xs text-error" role="alert">
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
              onclick={onStartWipEdit}
              class="w-full text-left flex items-center gap-2 px-3 py-1.5 hover:bg-hover text-type-sm font-label-sm text-text-primary"
              role="menuitem"
              data-testid={`board-wip-menu-${colKey}`}
            >
              <span class="material-symbols-outlined text-icon-sm">speed</span>
              WIP limit…
            </button>
          {/if}
          <button
            type="button"
            onclick={onRemoveColumn}
            class="w-full text-left flex items-center gap-2 px-3 py-1.5 hover:bg-hover text-type-sm font-label-sm text-error"
            role="menuitem"
          >
            <span class="material-symbols-outlined text-icon-sm">delete</span>
            Remove
          </button>
        </div>
      {/if}
    {/if}
  </div>
</div>
