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

  // Honor prefers-reduced-motion: a 0-duration fly is a no-op visually but the
  // panel still mounts/unmounts normally, so there is no separate code path.
  const reduceMotion =
    typeof window !== 'undefined' &&
    window.matchMedia('(prefers-reduced-motion: reduce)').matches

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
    /** Open the blocking properties edit modal (#873). Optional so the panel
     *  stays mountable in isolation (tests); when provided, an "expand to
     *  dialog" affordance renders beside the close button. */
    onOpenModal?: () => void
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
    onOpenModal,
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

  // Esc listener bound only while the panel is open. Intentionally BUBBLE
  // phase (no capture flag): the blocking PropertiesEditModal registers its
  // own Esc handler in CAPTURE phase and calls stopPropagation, so when both
  // are mounted (modal open over the peek) the modal's handler wins and the
  // peek's does not also fire. Do not "modernize" this to capture without
  // coordinating with the modal — it would break the single-close contract.
  $effect(() => {
    if (!open) return
    window.addEventListener('keydown', onWindowKeydown)
    return () => window.removeEventListener('keydown', onWindowKeydown)
  })
</script>

{#if open}
  <div
    bind:this={panelRef}
    transition:fly={{ y: 40, duration: reduceMotion ? 0 : 180 }}
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
      actionsLayout="menu"
    >
      {#snippet trailing()}
        <div class="chrome-group">
          {#if onOpenModal}
            <button
              type="button"
              class="chrome expand"
              onclick={onOpenModal}
              aria-label="Edit all properties in dialog"
              title="Edit all properties in dialog"
            >
              <span
                class="material-symbols-outlined text-icon-sm"
                aria-hidden="true">open_in_full</span
              ><span class="chrome-label">Edit all</span>
            </button>
          {/if}
          <button
            type="button"
            class="chrome close"
            onclick={onClose}
            aria-label="Close properties"
            title="Close"
          >
            <span
              class="material-symbols-outlined text-icon-sm"
              aria-hidden="true">close</span
            >
          </button>
        </div>
      {/snippet}
    </PropertiesBody>
  </div>
{/if}

<style>
  .props-panel {
    flex: 0 0 auto;
    display: flex;
    flex-direction: column;
    /* Slimmer than the old 40vh ceiling: the peek's job narrowed to
       glanceable reference + light edits once the blocking edit modal
       (#873) took over full-field editing, so it no longer needs to reserve
       40% of the viewport. 32vh fits the type header + Core section + ~5–6
       fields without internal scroll on a typical laptop, and returns the
       difference to the editor (the surface the user is actually writing
       on). Many-field cases scroll here or promote to the modal via the
       "Edit all" affordance. */
    max-height: 32vh;
    min-height: 0;
    background: var(--color-surface-panel);
    border-top: 1px solid var(--color-surface-panel-border);
    color: var(--color-text-primary);
    outline: none;
  }
  /* The expand-to-dialog + close controls are authored in this shell (via the
     trailing snippet) so their styles live here, not in PropertiesBody. They
     read as one cohesive window-chrome unit — the accent-promoted "Edit all"
     is the bridge into the blocking modal (#873), close dismisses the peek. */
  .chrome-group {
    display: inline-flex;
    align-items: stretch;
    flex: 0 0 auto;
  }
  .chrome {
    display: inline-flex;
    align-items: center;
    gap: 0.25rem;
    border: 0;
    background: transparent;
    color: var(--color-text-muted);
    cursor: pointer;
    padding: 0.25rem 0.4rem;
    border-radius: 0.3rem;
    font-size: var(--text-type-2xs);
    font-weight: 600;
    letter-spacing: 0.02em;
    transition:
      background-color 120ms var(--transition-standard),
      color 120ms var(--transition-standard);
  }
  /* Hairline divider between the two chrome controls — present only when both
     are rendered (adjacent-sibling selector: matches .close only when it
     directly follows .expand, i.e. onOpenModal is wired). Avoids a severed
     edge when the peek renders close-only (tests / isolated mounts). */
  .expand + .close::before {
    content: '';
    width: 1px;
    height: 1rem;
    align-self: center;
    margin-right: 0.3rem;
    background: var(--color-surface-panel-border);
  }
  /* "Edit all" is the peek's primary affordance (it promotes into the focused
     editor), so it gets the accent vocabulary the modal shares — the two
     surfaces read as one system rather than a dialog that appears from
     nowhere. */
  .expand:hover,
  .expand:focus-visible {
    color: var(--color-accent-primary-start);
    background: var(--color-accent-primary-glow);
  }
  .close:hover {
    color: var(--color-text-primary);
    background: var(--color-hover);
  }
  .chrome:focus-visible {
    outline: 2px solid var(--color-border-focus);
    outline-offset: 1px;
  }
  @media (prefers-reduced-motion: reduce) {
    .chrome {
      transition: none;
    }
  }
</style>
