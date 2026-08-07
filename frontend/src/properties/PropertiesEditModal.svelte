<script lang="ts">
  // Blocking properties edit modal (#873). A focused, full-field editing session
  // that complements the non-blocking peek (PropertiesPanel). Renders the SAME
  // shared PropertiesBody so field UI has one source of truth; the surface owns
  // the overlay chrome, focus trap, Esc / backdrop / close, and focus restore.
  // Edits are write-through-per-field (the existing model) so close is always
  // clean — no dirty/draft state.
  import { tick } from 'svelte'
  import { fade, fly } from 'svelte/transition'
  import { trapFocus } from '../lib/focusTrap'
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
    typeMenuRequest?: number
    core?: PageCoreMetadata
    onCommitCore?: (update: CoreFieldUpdate) => Promise<void>
    onClose: () => void
    onChanged: () => void
    onMismatched: (names: string[]) => void
    onError: (message: string) => void
    onCreateType?: () => void
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

  let surfaceRef = $state<HTMLDivElement | null>(null)
  let previouslyFocused: HTMLElement | null = null
  let wasOpen = false

  // Focus management: capture the opener on open, move focus into the modal,
  // and restore on close (guarded by isConnected so an unmounted opener — e.g.
  // the peek's expand button after a navigation — falls back to the editor).
  $effect(() => {
    if (open && !wasOpen) {
      wasOpen = true
      previouslyFocused = document.activeElement as HTMLElement | null
      void focusFirst()
    } else if (!open && wasOpen) {
      wasOpen = false
      if (previouslyFocused?.isConnected) previouslyFocused.focus?.()
      previouslyFocused = null
    }
  })

  async function focusFirst(): Promise<void> {
    await tick()
    const surface = surfaceRef
    if (!surface) return
    // Land on the type <select> — the first editable control and a sensible
    // starting point for both typed (Tab forward to fields) and untyped (pick a
    // type) pages. Skip it when the roster is empty (select is disabled and
    // unfocusable) and focus the surface so focus is at least inside the modal;
    // Tab then reaches the Create/Restore recovery buttons. Fall back to the
    // surface if the select is absent entirely.
    const typeSelect = surface.querySelector<HTMLElement>(
      '[aria-label="Page type"]'
    )
    if (typeSelect && !typeSelect.hasAttribute('disabled')) {
      typeSelect.focus()
      return
    }
    surface.focus()
  }

  function handleKeydown(e: KeyboardEvent): void {
    if (e.key === 'Escape') {
      // stopPropagation so the peek's own window-Esc handler (the peek stays
      // mounted underneath) does not also fire and close it.
      e.preventDefault()
      e.stopPropagation()
      onClose()
    }
  }

  // Trap + Esc listener bound only while open. trapFocus handles Tab/Shift+Tab;
  // the keydown listener handles Esc (surface-specific side effect).
  $effect(() => {
    if (!open) return
    const disposeTrap = surfaceRef ? trapFocus(surfaceRef) : () => {}
    window.addEventListener('keydown', handleKeydown, true)
    return () => {
      disposeTrap()
      window.removeEventListener('keydown', handleKeydown, true)
    }
  })
</script>

{#if open}
  <div
    class="modal-overlay"
    transition:fade={{ duration: 120 }}
    data-focus-trap
  >
    <!-- Full-size click-to-close sentinel (tabindex="-1" so it stays out of the
         Tab cycle, handled by the trap util). -->
    <button
      type="button"
      tabindex="-1"
      aria-label="Close edit properties"
      data-testid="modal-backdrop"
      class="backdrop-click"
      onclick={onClose}
    ></button>
    <div
      bind:this={surfaceRef}
      transition:fly={{ y: 16, duration: 160 }}
      role="dialog"
      aria-modal="true"
      aria-label="Edit page properties"
      tabindex="-1"
      class="modal-surface"
    >
      <header class="modal-header">
        <div class="modal-title-group">
          <span class="material-symbols-outlined" aria-hidden="true">tune</span>
          <h2 class="modal-title">Edit properties</h2>
        </div>
        <button
          type="button"
          class="modal-close"
          onclick={onClose}
          aria-label="Close edit properties"
        >
          <span
            class="material-symbols-outlined text-icon-md"
            aria-hidden="true">close</span
          >
        </button>
      </header>

      <div class="modal-body custom-scrollbar">
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
        />
      </div>
    </div>
  </div>
{/if}

<style>
  .modal-overlay {
    position: fixed;
    inset: 0;
    z-index: 200;
    display: flex;
    align-items: center;
    justify-content: center;
    /* Translucent dim (~50%) so the user retains some document context per
       #873's open question, while still obscuring the editor underneath. */
    background: rgba(0, 0, 0, 0.5);
    backdrop-filter: blur(2px);
  }
  .backdrop-click {
    position: absolute;
    inset: 0;
    border: 0;
    background: transparent;
    cursor: default;
    padding: 0;
  }
  .modal-surface {
    position: relative;
    width: 40rem;
    max-width: calc(100vw - 2rem);
    /* Cap height so the body scrolls independently while the header stays put;
       ≤12 fields fit a typical viewport without internal scroll. */
    max-height: calc(100vh - 4rem);
    display: flex;
    flex-direction: column;
    background: var(--color-surface-modal);
    border: 1px solid var(--color-surface-modal-border);
    border-radius: 0.75rem;
    box-shadow: var(--shadow-lg);
    color: var(--color-surface-modal-text);
    overflow: hidden;
    outline: none;
  }
  .modal-surface:focus-visible {
    outline: 2px solid var(--color-border-focus);
    outline-offset: 2px;
  }
  .modal-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.5rem;
    padding: 0.6rem 0.9rem;
    border-bottom: 1px solid var(--color-surface-modal-border);
    flex: 0 0 auto;
  }
  .modal-title-group {
    display: inline-flex;
    align-items: center;
    gap: 0.4rem;
    min-width: 0;
  }
  .modal-title-group .material-symbols-outlined {
    font-size: var(--text-type-md);
    color: var(--color-text-muted);
  }
  .modal-title {
    margin: 0;
    font-family: var(--font-headline, sans-serif);
    font-size: var(--text-type-lg);
    font-weight: 600;
    color: var(--color-text-primary);
  }
  .modal-close {
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
  .modal-close:hover {
    color: var(--color-text-primary);
    background: var(--color-hover);
  }
  .modal-close:focus-visible {
    outline: 2px solid var(--color-border-focus);
    outline-offset: 1px;
  }
  .modal-body {
    flex: 1 1 auto;
    min-height: 0;
    overflow-y: auto;
    /* PropertiesBody's own grid lays out the fields; this region just scrolls. */
  }
</style>
