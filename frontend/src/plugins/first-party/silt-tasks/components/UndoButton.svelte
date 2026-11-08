<script lang="ts">
  // One undo affordance for every hub surface (#1011). Undo is global LIFO
  // across Silt surfaces, so the button always names the journal head rather
  // than the view it happens to sit in. `toolbar` is the hub-header chip;
  // `block` is the drawer's full-width footer action.
  import { getTasksUndoView, runTaskUndo } from '../undo.svelte'
  import { undoButtonLabel } from '../undoCopy'

  interface Props {
    variant?: 'toolbar' | 'block'
  }

  let { variant = 'toolbar' }: Props = $props()

  let view = $derived(getTasksUndoView())
  let label = $derived(undoButtonLabel(view))
</script>

<button
  type="button"
  onclick={() => void runTaskUndo()}
  disabled={!view.available}
  aria-keyshortcuts="Control+Z"
  aria-label={label}
  title={label}
  data-testid={variant === 'block' ? 'tasks-undo-drawer' : 'tasks-undo'}
  data-undo-pending={view.pending ? 'true' : undefined}
  class={variant === 'block'
    ? 'flex w-full items-center justify-center gap-2 rounded border border-surface-card-border bg-surface-card px-3 py-2 font-label-sm-bold text-text-primary transition-all hover:bg-hover focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start disabled:cursor-not-allowed disabled:opacity-40 disabled:hover:bg-surface-card'
    : 'flex h-8 shrink-0 cursor-pointer items-center gap-1.5 rounded-lg border border-surface-panel-border bg-surface-panel px-2 font-label-sm text-type-xs text-text-muted shadow-sm transition-all hover:border-border-active hover:bg-hover hover:text-text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start disabled:cursor-not-allowed disabled:opacity-40 disabled:hover:border-surface-panel-border disabled:hover:bg-surface-panel disabled:hover:text-text-muted'}
>
  <span class="material-symbols-outlined text-icon-md" aria-hidden="true"
    >undo</span
  >
  <span>Undo</span>
  {#if view.canUndo && view.remaining > 1}
    <!-- Honest depth hint: a cross-file reorder journals one entry per file,
       so one undo can leave more work even though the drag felt like one
       gesture. The full "N changes left" phrasing is in aria-label. -->
    <span
      class="rounded-full bg-hover px-1.5 py-0.5 text-type-3xs font-label-sm tabular-nums text-text-muted"
      aria-hidden="true">{view.remaining}</span
    >
  {/if}
</button>
