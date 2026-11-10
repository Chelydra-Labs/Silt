<script lang="ts" module>
  let nextId = 0

  // Chip box shared with TaskCard's layout slot: the interactive pill is a
  // sibling of the card root (nested-interactive rule), so the card reserves
  // the pill's width with an invisible chip mirrored from these classes.
  export const PRIORITY_CHIP_CLASS =
    'px-1.5 py-0.5 border rounded-full font-label-sm text-type-3xs uppercase tracking-wide'
</script>

<script lang="ts">
  import { tick, untrack } from 'svelte'
  import { portal } from '../../../../lib/portal'
  import { flipOrClamp } from '../../../../lib/editor/popoverPositioning'
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
  let buttonEl = $state<HTMLButtonElement | null>(null)
  let tipEl = $state<HTMLElement | null>(null)
  // Start off-screen so the first paint never flashes at the viewport origin;
  // placeTip() pins the layer to the button before the browser paints (same
  // trick as <Popover>).
  let tipLeft = $state(-9999)
  let tipTop = $state(-9999)

  function placeTip() {
    const btn = buttonEl
    const tip = tipEl
    if (!btn || !tip) return
    const r = btn.getBoundingClientRect()
    // Right-aligned to the button (the old right-0 look), flipped above when
    // the bottom would clip, then clamped to the viewport.
    const placed = flipOrClamp(
      { top: r.top, bottom: r.bottom, left: r.right - tip.offsetWidth },
      { width: tip.offsetWidth, height: tip.offsetHeight },
      { width: window.innerWidth, height: window.innerHeight }
    )
    tipLeft = placed.left
    tipTop = placed.top
  }

  // Reposition — never strand — across ancestor scroll and viewport resize:
  // keyboard focus scrolls the row into view, so a hide-on-scroll tooltip
  // would close on the very users it exists for.
  $effect(() => {
    if (!tipOpen) return
    placeTip()
    void tick().then(placeTip)
    const reposition = () => placeTip()
    document.addEventListener('scroll', reposition, {
      capture: true,
      passive: true
    })
    window.addEventListener('resize', reposition, { passive: true })
    return () => {
      document.removeEventListener('scroll', reposition, { capture: true })
      window.removeEventListener('resize', reposition)
    }
  })

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
      bind:this={buttonEl}
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
  </span>
</span>
{#if tipOpen}
  <!-- The List row clips with overflow-hidden (ListView .tasks-list-row) and
       cards clip their content too: an in-flow tooltip anchored to the pill is
       cut to a sliver. Portaled fixed layer measured against the button —
       escapes every ancestor overflow/transform (same convention as
       <Popover>). jsdom can't prove the escape; PriorityPill.test.ts pins the
       structural contract instead. -->
  <span
    use:portal
    bind:this={tipEl}
    id={tipId}
    role="tooltip"
    class="pointer-events-none fixed z-50 w-max max-w-52 rounded-lg border border-surface-panel-border bg-surface-panel px-2.5 py-1.5 text-type-xs font-label-sm text-text-primary shadow-lg"
    style:left="{tipLeft}px"
    style:top="{tipTop}px"
  >
    {announce}
  </span>
{/if}
