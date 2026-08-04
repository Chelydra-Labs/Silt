<script lang="ts">
  // Bottom-docked properties panel — the editing surface for a typed page.
  // NON-BLOCKING inspector (mirrors TaskEditDrawer's contract): no scrim,
  // `aria-modal="false"`, focus moves to the panel on open + restores to the
  // trigger on close, Esc closes. Type assignment uses a native <select> —
  // the browser-rendered dropdown is immune to the panel's overflow:hidden
  // (a custom popover clipped here in the real webview). Create type and
  // Restore examples live as always-visible buttons beside the select. Docks
  // to the bottom of the editor column (a flex-col sibling of the editor);
  // the editor is flex-1 so it shrinks and the reading context is preserved.
  import { tick } from 'svelte'
  import { fly } from 'svelte/transition'
  import { SetPageType, TurnIntoPage } from '../../bindings/silt/app.js'
  import { coerceIPCError } from '../lib/ipcError'
  import PropertyField from './PropertyField.svelte'
  import TurnIntoDialog from './TurnIntoDialog.svelte'
  import type {
    PageLocator,
    PagePropertyValue,
    PageTypeInfo,
    PropertyDef,
    TypeDef
  } from './types'

  interface Props {
    open: boolean
    info: PageTypeInfo
    values: PagePropertyValue[]
    mismatched: string[]
    error: string
    /** True while the controller's GetPageType/GetPageProperties fetch is in
     *  flight — used to show a loading state instead of the empty-type message. */
    loading: boolean
    types: TypeDef[]
    typesLoading: boolean
    locator: PageLocator
    /** Monotonic slash-command signal — when it bumps, open the type menu. */
    typeMenuRequest?: number
    onClose: () => void
    /** After a successful type switch / property commit (re-fetches values). */
    onChanged: () => void
    /** Keep-and-flag names from a type switch (renders inline field warnings). */
    onMismatched: (names: string[]) => void
    /** Field-level save failures (aria-live banner). */
    onError: (message: string) => void
    /** Open the in-app type editor (the empty-state escape hatch). Optional —
     *  defaults to no-op so the panel stays mountable in isolation (tests,
     *  older call sites). */
    onCreateType?: () => void
    /** Restore the shipped example types (Book, Meeting). Optional for the
     *  same reason as onCreateType. */
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
    onClose,
    onChanged,
    onMismatched,
    onError,
    onCreateType,
    onRestoreExamples
  }: Props = $props()

  let panelRef = $state<HTMLDivElement | null>(null)
  let typeSelectRef = $state<HTMLSelectElement | null>(null)
  let previouslyFocused: HTMLElement | null = null
  let wasOpen = false

  // Local mirror of the most recent field/type-switch failure so the aria-live
  // banner appears immediately on rejection in an isolated render. The external
  // `error` prop carries controller-level failures (refresh); the banner shows
  // whichever is set.
  let liveError = $state('')

  // Drop stale field/type errors when the user navigates to another page —
  // otherwise the previous page's rejection banner lingers on the new page.
  let lastLocatorKey = ''
  $effect(() => {
    const key = `${locator.notebook}/${locator.section}/${locator.page}`
    if (lastLocatorKey !== '' && lastLocatorKey !== key) {
      liveError = ''
    }
    lastLocatorKey = key
  })

  function handleFieldError(message: string): void {
    liveError = message
    onError(message)
  }

  function handleChanged(): void {
    // A successful commit clears the stale failure banner.
    liveError = ''
    onChanged()
  }

  // Turn-into orchestration. A TYPED page switching to a different type (or
  // being cleared) previews the conversion via TurnIntoDialog so the user can
  // see how existing values fare + opt in to clearing orphans. Untyped → typed
  // has nothing to map, so it assigns directly.
  let turnInto = $state<{ newId: string; newLabel: string } | null>(null)

  function resetTypeSelect(): void {
    // Svelte only re-applies the select's value attr when the expression deps
    // change; after a rejected write info is unchanged so the DOM would keep
    // showing the picked type. Mirror cancelTurnInto.
    if (typeSelectRef) {
      typeSelectRef.value = info.isSet ? info.type.id : ''
    }
  }

  async function commitType(name: string): Promise<void> {
    try {
      const result = (await SetPageType(
        locator.notebook,
        locator.section,
        locator.page,
        name
      )) as string[] | null
      onMismatched(result ?? [])
      liveError = ''
      onError('')
      onChanged()
    } catch (e) {
      const message = coerceIPCError(e).message
      liveError = message
      onError(message)
      resetTypeSelect()
    }
  }

  function handleChooseType(name: string): void {
    // Same-type re-pick is a no-op.
    if (info.isSet && name === info.type.id) return
    if (!info.isSet) {
      void commitType(name)
      return
    }
    const newType = types.find((t) => t.id === name)
    turnInto = {
      newId: name,
      newLabel: name === '' ? 'No type' : newType?.name || name
    }
  }

  async function confirmTurnInto(
    orphanNames: string[],
    clearOrphaned: boolean
  ): Promise<void> {
    const newId = turnInto?.newId ?? ''
    turnInto = null
    try {
      // Atomic turn-into: type rewrite + orphan clears land in ONE file write
      // (TurnIntoPage). The old clear-loop-then-SetPageType path could delete
      // orphan values under the OLD type when the final type write failed
      // (sync-client lock / disk full) — silent data loss. On failure here
      // the frontmatter is untouched.
      const orphans = clearOrphaned ? orphanNames : []
      const result = (await TurnIntoPage(
        locator.notebook,
        locator.section,
        locator.page,
        newId,
        orphans
      )) as string[] | null
      onMismatched(result ?? [])
      liveError = ''
      onError('')
      onChanged()
    } catch (e) {
      const message = coerceIPCError(e).message
      liveError = message
      onError(message)
      resetTypeSelect()
      onChanged()
    }
  }

  function cancelTurnInto(): void {
    turnInto = null
    // The user picked an option then cancelled the conversion preview, so the
    // <select>'s DOM value still shows the picked type while info is
    // unchanged. Reset imperatively to reflect info.
    resetTypeSelect()
  }

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

  // Slash-command bridge: when the host bumps typeMenuRequest (the untyped
  // pill's click or a `/type` slash), focus the native <select> so the user
  // can immediately pick a type. When the browser supports it, also call
  // showPicker() to auto-open the dropdown — a no-op-safe upgrade; focus
  // alone is the reliable baseline. The initial 0 value is "no request" so
  // this doesn't fire on first mount. Double-tick defers past the panel-open
  // focus effect (which focuses panelRef) so the select ends up focused.
  // Slash-command bridge: when the host bumps typeMenuRequest (the untyped
  // pill's click or a `/type` slash), focus the native <select> so the user
  // can immediately pick a type. When the browser supports it, also call
  // showPicker() to auto-open the dropdown — a no-op-safe upgrade; focus
  // alone is the reliable baseline. The initial 0 value is "no request" so
  // this doesn't fire on first mount. Double-tick defers past the panel-open
  // focus effect (which focuses panelRef) so the select ends up focused.
  // When the roster is empty the select is disabled and unskippably
  // unfocusable — bail so focus stays on the panel (the Create type /
  // Restore examples buttons are the recovery path there).
  $effect(() => {
    void typeMenuRequest
    if (!open || typeMenuRequest === 0) return
    void tick()
      .then(() => tick())
      .then(() => {
        if (!typeSelectRef || typeSelectRef.disabled) return
        typeSelectRef.focus()
        if (typeof HTMLSelectElement.prototype.showPicker === 'function') {
          try {
            typeSelectRef.showPicker()
          } catch {
            // showPicker throws if the user-activation window expired or the
            // element isn't ready; the focus above already covers the case.
          }
        }
      })
  })

  // Lookup table for min/max (the value envelope omits them). Keyed by property
  // name from the resolved type schema so the number input can enforce bounds.
  let schemaByName = $derived.by(() => {
    // Rebuilt fresh on each derived recompute; never mutated reactively, so a
    // plain Map is correct and SvelteMap would only add tracking overhead.
    // eslint-disable-next-line svelte/prefer-svelte-reactivity -- local derived lookup
    const map = new Map<string, PropertyDef>()
    for (const p of info.type?.properties ?? []) map.set(p.name, p)
    return map
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

  let hasType = $derived(info.isSet)
  // A `type:` ref that didn't resolve to a known type def. The pill renders a
  // subdued raw chip for this; the panel matches with a distinct message + a
  // Remove-type affordance to clear the bogus ref (the <select> can't reach
  // it: info.type.id is already '' so picking "No type" wouldn't fire a change).
  let isUnknownType = $derived(!info.isSet && info.rawType.length > 0)
  // The <select>'s first option is a sentinel whose label depends on state:
  // a placeholder cue for untyped, an explicit "No type" pick for typed/
  // unknown (the unassign path). Loading and empty-roster states get their
  // own disabled-placeholder labels.
  let typePlaceholder = $derived(
    typesLoading && types.length === 0
      ? 'Loading…'
      : types.length === 0
        ? 'No types defined'
        : hasType || isUnknownType
          ? 'No type'
          : 'Assign a type…'
  )
  // Disable when there's nothing to pick — the Create/Restore buttons stay
  // enabled so the user can recover from an empty roster.
  let selectDisabled = $derived(typesLoading || types.length === 0)
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
    <header class="header">
      <div class="title-group">
        <span class="material-symbols-outlined type-icon" aria-hidden="true">
          {hasType && info.type.icon ? info.type.icon : 'label'}
        </span>
        <!-- Native <select>: the dropdown is rendered by the OS/browser and
             therefore never clipped by this panel's overflow:hidden (a custom
             popover failed exactly there in the real webview). appearance:none
             lets us shape the closed state; the caret is a sibling overlay so
             the dropdown itself stays browser-native. -->
        <div class="type-select-wrap">
          <select
            bind:this={typeSelectRef}
            class="type-select"
            value={hasType ? info.type.id : ''}
            onchange={(e) => handleChooseType(e.currentTarget.value)}
            disabled={selectDisabled}
            aria-label="Page type"
          >
            <option value="">{typePlaceholder}</option>
            {#each types as t (t.id)}
              <option value={t.id}>{t.name || t.id}</option>
            {/each}
          </select>
          <span class="material-symbols-outlined type-caret" aria-hidden="true"
            >expand_more</span
          >
        </div>
        <div class="actions">
          {#if isUnknownType}
            <!--
              The <select> can't clear a bogus raw ref: info.type.id is already
              '' so picking "No type" wouldn't fire a change. Surface an
              explicit Remove-type action for the unknown case only — typed
              pages reach the unassign path via the select's "No type" option.
            -->
            <button
              type="button"
              class="action danger"
              onclick={() => handleChooseType('')}
            >
              <span
                class="material-symbols-outlined text-icon-sm"
                aria-hidden="true">remove_circle_outline</span
              >
              Remove type
            </button>
          {/if}
          <button type="button" class="action" onclick={() => onCreateType?.()}>
            <span
              class="material-symbols-outlined text-icon-sm"
              aria-hidden="true">add_circle</span
            >
            Create type…
          </button>
          <button
            type="button"
            class="action"
            onclick={() => onRestoreExamples?.()}
          >
            <span
              class="material-symbols-outlined text-icon-sm"
              aria-hidden="true">restart_alt</span
            >
            Restore examples
          </button>
        </div>
      </div>

      <button
        type="button"
        class="close"
        onclick={onClose}
        aria-label="Close properties"
      >
        <span class="material-symbols-outlined text-icon-md" aria-hidden="true"
          >close</span
        >
      </button>
    </header>

    <!-- aria-live: save failures (assertive) + keep-and-flag notices (polite). -->
    {#if error || liveError}
      <p class="banner error" role="alert">
        {error || liveError}
      </p>
    {/if}
    {#if mismatched.length > 0}
      <p class="banner warn" role="status" aria-live="polite">
        Some values don't fit the new type and were kept as-is.
      </p>
    {/if}

    <div class="fields custom-scrollbar">
      {#if loading && values.length === 0}
        <p class="empty" role="status" aria-live="polite">Loading…</p>
      {:else if isUnknownType}
        <p class="empty">Unrecognized type '{info.rawType}'.</p>
        <p class="empty">This type isn't defined in .system/types.</p>
      {:else if !hasType}
        <p class="empty">
          This page has no type. Assign one to add typed properties.
        </p>
      {:else if values.length === 0}
        <p class="empty">This type has no properties.</p>
      {:else}
        {#each values as v (v.name)}
          {@const schema = schemaByName.get(v.name)}
          <PropertyField
            value={v}
            {locator}
            min={schema?.min ?? null}
            max={schema?.max ?? null}
            target={schema?.target ?? ''}
            mismatched={mismatched.includes(v.name)}
            onError={handleFieldError}
            onChanged={handleChanged}
            onResync={onChanged}
          />
        {/each}
      {/if}
    </div>
  </div>
{/if}

{#if turnInto}
  <TurnIntoDialog
    open={true}
    {locator}
    oldTypeId={info.type.id}
    newTypeId={turnInto.newId}
    newTypeLabel={turnInto.newLabel}
    onConfirm={confirmTurnInto}
    onCancel={cancelTurnInto}
  />
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
  .header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.5rem;
    padding: 0.4rem 0.75rem;
    border-bottom: 1px solid var(--color-surface-panel-border);
    flex: 0 0 auto;
  }
  .title-group {
    display: inline-flex;
    align-items: center;
    gap: 0.4rem;
    min-width: 0;
    flex-wrap: wrap;
  }
  .type-icon {
    font-size: var(--text-type-md);
    color: var(--color-text-muted);
    flex: 0 0 auto;
  }
  /* appearance:none lets us shape the closed state; the dropdown itself stays
     browser-rendered (the robustness win — native dropdowns render outside the
     panel's overflow:hidden). The caret is a sibling overlay so we don't rely
     on the UA's built-in arrow. */
  .type-select-wrap {
    position: relative;
    display: inline-flex;
    align-items: center;
  }
  .type-select {
    appearance: none;
    -webkit-appearance: none;
    -moz-appearance: none;
    display: inline-flex;
    align-items: center;
    padding: 0.25rem 1.5rem 0.25rem 0.5rem;
    border-radius: 0.375rem;
    border: 1px solid var(--color-surface-panel-border);
    background: var(--color-surface-app);
    color: var(--color-text-primary);
    font-size: var(--text-type-sm);
    cursor: pointer;
    max-width: 20rem;
  }
  .type-select:hover:not(:disabled) {
    background: var(--color-hover);
  }
  .type-select:focus-visible {
    outline: 2px solid var(--color-border-focus);
    outline-offset: 1px;
  }
  .type-select:disabled {
    opacity: 0.6;
    cursor: not-allowed;
  }
  .type-caret {
    position: absolute;
    right: 0.3rem;
    font-size: var(--text-type-sm);
    color: var(--color-text-muted);
    pointer-events: none;
  }
  .actions {
    display: inline-flex;
    align-items: center;
    gap: 0.25rem;
    flex-wrap: wrap;
  }
  .action {
    display: inline-flex;
    align-items: center;
    gap: 0.25rem;
    padding: 0.2rem 0.4rem;
    border: 1px solid var(--color-surface-panel-border);
    border-radius: 0.3rem;
    background: transparent;
    color: var(--color-text-muted);
    font-size: var(--text-type-xs);
    cursor: pointer;
  }
  .action:hover {
    background: var(--color-hover);
    color: var(--color-text-primary);
  }
  .action:focus-visible {
    outline: 2px solid var(--color-border-focus);
    outline-offset: 1px;
  }
  .action.danger {
    color: var(--color-status-danger);
  }
  .action.danger:hover {
    color: var(--color-status-danger);
    background: var(--color-error-bg);
  }
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
  .banner {
    margin: 0;
    padding: 0.4rem 0.75rem;
    font-size: var(--text-type-xs);
  }
  .banner.error {
    background: var(--color-error-bg);
    color: var(--color-error-fg);
    border-bottom: 1px solid var(--color-error-border);
  }
  .banner.warn {
    color: var(--color-status-warn);
    border-bottom: 1px solid var(--color-surface-panel-border);
  }
  .fields {
    padding: 0.6rem 0.75rem;
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(15rem, 1fr));
    gap: 0.6rem 0.9rem;
    overflow-y: auto;
    min-height: 0;
  }
  .empty {
    grid-column: 1 / -1;
    color: var(--color-text-muted);
    font-size: var(--text-type-sm);
    padding: 0.5rem 0;
  }
</style>
