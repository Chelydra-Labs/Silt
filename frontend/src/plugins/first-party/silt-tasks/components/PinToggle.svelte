<script lang="ts">
  import { untrack } from 'svelte'
  import { optimisticField } from '../optimisticField.svelte'

  interface Props {
    pinned: boolean
    onToggle: (next: boolean) => Promise<unknown> | void
    /** Receives the friendly failure message; '' clears it at commit start. */
    onError?: (message: string) => void
    disabled?: boolean
    testId?: string
    /** Host layout classes (e.g. positioning in a card header). */
    class?: string
  }

  let {
    pinned,
    onToggle,
    onError,
    disabled = false,
    testId,
    class: className = ''
  }: Props = $props()

  const pinField = optimisticField<boolean>({
    initial: untrack(() => pinned),
    // Adopt a returned promise so a failed host write still reverts; a
    // synchronous host callback resolves immediately.
    write: (next) => Promise.resolve(onToggle(next)),
    onError: (message) => onError?.(message)
  })

  // While a write is pending, the optimistic value wins: a reload landing
  // mid-write must not flip the icon back to the pre-click prop. Only an
  // idle prop change re-syncs (no settle-time pass — see pending untrack).
  $effect(() => {
    const next = pinned
    if (untrack(() => !pinField.pending)) pinField.reset(next)
  })

  function toggle(e: MouseEvent) {
    e.stopPropagation()
    void pinField.commit(!pinField.value)
  }

  function onKeydown(e: KeyboardEvent) {
    // Host rows/cards treat Enter/Space as selection. Stop that handler while
    // leaving the button's native click activation intact.
    if (e.key === 'Enter' || e.key === ' ') e.stopPropagation()
  }
</script>

<button
  type="button"
  onclick={toggle}
  onkeydown={onKeydown}
  disabled={disabled || pinField.pending}
  aria-pressed={pinField.value}
  aria-label={pinField.value ? 'Unpin task' : 'Pin task'}
  title={pinField.value ? 'Unpin task' : 'Pin task'}
  data-testid={testId}
  data-pinned={pinField.value}
  class="pin-button shrink-0 rounded p-1 transition-colors hover:bg-hover focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-accent-primary-start disabled:cursor-not-allowed disabled:opacity-50 {pinField.value
    ? 'text-accent-primary-start'
    : 'text-text-muted'} {className}"
>
  <!-- Static icon font (no FILL axis): the pinned cue is a base bar the eye
       can read without hue. -->
  <span
    class="pin-icon material-symbols-outlined relative text-icon-sm"
    aria-hidden="true"
    >push_pin{#if pinField.value}<span
        class="pin-state-mark absolute inset-x-1 bottom-0 h-0.5 rounded-full bg-current"
        data-testid="pin-state-mark"
      ></span>{/if}</span
  >
</button>
