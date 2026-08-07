<script lang="ts">
  // Bottom-docked properties peek — the NON-BLOCKING inspector shell. Mirrors
  // TaskEditDrawer's contract: no scrim, `aria-modal="false"`, focus moves to
  // the panel on open + restores to the trigger on close, Esc closes. All
  // content + content behavior (type select, turn-into, banners, fields) lives
  // in the shared PropertiesBody so the blocking edit modal reuses it verbatim.
  // The close control is supplied via a `trailing` snippet so it sits in the
  // body's header row; docks to the bottom of the editor column (a flex-col
  // sibling of the editor); the editor is flex-1 so it shrinks and the reading
  // context is preserved.
  import { tick } from 'svelte'
  import { fly } from 'svelte/transition'
  import PropertiesBody from './PropertiesBody.svelte'
  import type {
    CoreFieldUpdate,
    PageCoreMetadata,
    PageLocator,
    PagePropertyValue,
    PageTypeInfo,
    TypeDef
  } from './types'

  interface Props {
    open: boolean
    info: PageTypeInfo
    values: PagePropertyValue[]
    mismatched: string[]
    error: string
    loading: boolean
    types: TypeDef[]
    typesLoading: boolean
    locator: PageLocator
    /** Monotonic slash-command signal — forwarded to PropertiesBody so it
     *  focuses the type <select>. */
    typeMenuRequest?: number
    /** Type-independent core metadata (#867). Optional — forwarded to the body. */
    core?: PageCoreMetadata
    onCommitCore?: (update: CoreFieldUpdate) => Promise<void>
    onClose: () => void
    /** After a successful type switch / property commit (re-fetches values). */
    onChanged: () => void
    /** Keep-and-flag names from a type switch (renders inline field warnings). */
    onMismatched: (names: string[]) => void
    /** Field-level save failures (aria-live banner). */
    onError: (message: string) => void
    /** Open the in-app type editor (the empty-state escape hatch). Optional —
     *  defaults to no-op so the panel stays mountable in isolation (tests). */
    onCreateType?: () => void
    /** Restore the shipped example types (Book, Meeting). */
    onRestoreExamples?: () => void
  }

  let {
    open,
    info,
    values,
    mismatched,
    error,
    loading,
    types,
    typesLoading,
    locator,
    typeMenuRequest = 0,
    core = undefined,
    onCommitCore = undefined,
    onClose,
    onChanged,
    onMismatched,
    onError,
    onCreateType,
    onRestoreExamples
  }: Props = $props()

  let panelRef = $state<HTMLDivElement | null>(null)
  let previouslyFocused: HTMLElement | null = null
  let wasOpen = false

  // Focus management (non-blocking): move focus into the panel on open, restore
  // to the trigger on close. NOT trapped — the editor stays interactive.
  $effect(() => {
    if (open && !wasOpen) {
      wasOpen = true
      previouslyFocused = document.activeElement as HTMLElement | null
      void tick().then(() => panelRef?.focus())
    } else if (!open && wasOpen) {
      wasOpen = false
      if (previouslyFocused?.isConnected) previouslyFocused.focus?.()
      previouslyFocused = null
    }
  })

  function onWindowKeydown(e: KeyboardEvent): void {
    if (!open) return
    // A native <select> captures Escape at the browser level to close its
    // dropdown before it can reach the window listener, so there's no
    // popover-deferral step here — Esc goes straight to closing the panel.
    if (e.key === 'Escape') {
      e.preventDefault()
      onClose()
    }
  }

  // Esc listener bound only while the panel is open.
  $effect(() => {
    if (!open) return
    window.addEventListener('keydown', onWindowKeydown)
    return () => window.removeEventListener('keydown', onWindowKeydown)
  })
</script>

{#if open}
  <div
    bind:this={panelRef}
    transition:fly={{ y: 40, duration: 180 }}
    class="props-panel"
    role="dialog"
    aria-modal="false"
    aria-label="Page properties"
    tabindex="-1"
  >
    <PropertiesBody
      {info}
      {values}
      {mismatched}
      {error}
      {loading}
      {types}
      {typesLoading}
      {locator}
      {typeMenuRequest}
      {core}
      {onCommitCore}
      {onChanged}
      {onMismatched}
      {onError}
      {onCreateType}
      {onRestoreExamples}
    >
      {#snippet trailing()}
        <button
          type="button"
          class="close"
          onclick={onClose}
          aria-label="Close properties"
        >
          <span
            class="material-symbols-outlined text-icon-md"
            aria-hidden="true">close</span
          >
        </button>
      {/snippet}
    </PropertiesBody>
  </div>
{/if}

<style>
  .props-panel {
    flex: 0 0 auto;
    display: flex;
    flex-direction: column;
    max-height: 40vh;
    min-height: 0;
    background: var(--color-surface-panel);
    border-top: 1px solid var(--color-surface-panel-border);
    color: var(--color-text-primary);
    outline: none;
  }
  /* The close control is authored in this shell (via the trailing snippet) so
     its style lives here, not in PropertiesBody. */
  .close {
    display: inline-flex;
    align-items: center;
    border: 0;
    background: transparent;
    color: var(--color-text-muted);
    cursor: pointer;
    border-radius: 0.3rem;
    padding: 0.2rem;
    flex: 0 0 auto;
  }
  .close:hover {
    color: var(--color-text-primary);
    background: var(--color-hover);
  }
  .close:focus-visible {
    outline: 2px solid var(--color-border-focus);
    outline-offset: 1px;
  }
</style>
