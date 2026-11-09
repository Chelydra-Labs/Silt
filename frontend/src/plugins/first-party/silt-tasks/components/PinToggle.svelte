<script lang="ts">
  import { untrack } from 'svelte'
  import { optimisticField } from '../optimisticField.svelte'

  interface Props {
    pinned: boolean
    onToggle: (next: boolean) => Promise<unknown>
    /** Receives the friendly failure message; '' clears it at commit start. */
    onError?: (message: string) => void
    disabled?: boolean
    testId?: string
  }

  let { pinned, onToggle, onError, disabled = false, testId }: Props = $props()

  const pinField = optimisticField<boolean>({
    initial: untrack(() => pinned),
    write: (next) => onToggle(next),
    onError: (message) => onError?.(message)
  })

  // A reload may replace the row's data mid-write; only accept the fresh
  // prop once the optimistic commit settles so a late response can't flip
  // the icon back to the pre-click value.
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
  class="pin-button shrink-0 rounded p-1 transition-colors hover:bg-hover focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-accent-primary-start disabled:cursor-not-allowed disabled:opacity-50 {pinField.value
    ? 'text-accent-primary-start'
    : 'text-text-muted'}"
>
  <span class="material-symbols-outlined text-icon-sm" aria-hidden="true"
    >push_pin</span
  >
</button>
