<script lang="ts" module>
  let nextId = 0

  // Chip box shared with TaskCard's layout slot: the interactive pill is a
  // sibling of the card root (nested-interactive rule), so the card reserves
  // the pill's width with an invisible chip mirrored from these classes.
  export const PRIORITY_CHIP_CLASS =
    'px-1.5 py-0.5 border rounded-full font-label-sm text-type-3xs uppercase tracking-wide'
</script>

<script lang="ts">
  import { untrack } from 'svelte'
  import { nextPriority } from '../../../../lib/taskPriority'
  import { optimisticField } from '../optimisticField.svelte'
  import { priorityClass, priorityLabel } from '../types'

  interface Props {
    priority: number
    onCycle: (next: number) => Promise<unknown> | void
    testId?: string
    /** Host layout classes (e.g. overlay placement in a card header). */
    class?: string
  }

  let { priority, onCycle, testId, class: className = '' }: Props = $props()

  nextId += 1
  const tipId = `priority-pill-tip-${nextId}`

  const priorityField = optimisticField<number>({
    initial: untrack(() => priority),
    // Adopt a returned promise so a failed host write still reverts; a
    // synchronous host callback resolves immediately.
    write: (next) => Promise.resolve(onCycle(next))
  })

  // While a write is pending, the optimistic value wins: a reload landing
  // mid-write must not flip the label back to the pre-click prop. Only an
  // idle prop change re-syncs (same contract as PinToggle).
  $effect(() => {
    const next = priority
    if (untrack(() => !priorityField.pending)) priorityField.reset(next)
  })

  let label = $derived(priorityLabel(priorityField.value))
  let next = $derived(nextPriority(priorityField.value))
  // Unset (or legacy out-of-range) renders as a ghost pill revealed on hover.
  let ghost = $derived(priorityField.value < 1 || priorityField.value > 3)
  let announce = $derived(
    `Priority: ${label} — click to set ${priorityLabel(next)}`
  )
  let tipOpen = $state(false)

  function cycle(e: MouseEvent) {
    e.stopPropagation()
    void priorityField.commit(nextPriority(priorityField.value))
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter' || e.key === ' ') {
      // Host rows/cards treat Enter/Space as selection. Stop that handler while
      // leaving the button's native click activation intact.
      e.stopPropagation()
    } else if (e.key === 'Escape' && tipOpen) {
      e.stopPropagation()
      tipOpen = false
    }
  }
</script>

<span class={className}>
  <span class="relative inline-flex">
    <button
      type="button"
      data-testid={testId}
      aria-label={announce}
      aria-describedby={tipOpen ? tipId : undefined}
      onclick={cycle}
      onkeydown={onKeydown}
      onmouseenter={() => (tipOpen = true)}
      onmouseleave={() => (tipOpen = false)}
      onfocus={() => (tipOpen = true)}
      onblur={() => (tipOpen = false)}
      class="rounded-full p-1 transition-opacity hover:bg-hover focus:outline-none focus-visible:ring-1 focus-visible:ring-accent-primary-start {ghost
        ? 'opacity-0 group-hover:opacity-100 group-focus-within:opacity-100 focus-visible:opacity-100'
        : ''}"
    >
      <span
        class="{PRIORITY_CHIP_CLASS} {ghost
          ? 'border-dashed border-surface-card-border text-text-muted'
          : priorityClass(priorityField.value)}"
      >
        {label}
      </span>
    </button>
    {#if tipOpen}
      <span
        id={tipId}
        role="tooltip"
        class="pointer-events-none absolute right-0 top-full z-50 mt-1 w-max max-w-52 rounded-lg border border-surface-panel-border bg-surface-panel px-2.5 py-1.5 text-type-xs font-label-sm text-text-primary shadow-lg"
      >
        {announce}
      </span>
    {/if}
  </span>
</span>
