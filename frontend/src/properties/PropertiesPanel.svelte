<script lang="ts">
  // Bottom-docked properties panel — the editing surface for a typed page.
  // NON-BLOCKING inspector (mirrors TaskEditDrawer's contract): no scrim,
  // `aria-modal="false"`, focus moves to the panel on open + restores to the
  // trigger on close, Esc closes but defers to an open type menu first. Docks
  // to the bottom of the editor column (a flex-col sibling of the editor); the
  // editor is flex-1 so it shrinks and the reading context is preserved.
  import { tick } from 'svelte'
  import { fly } from 'svelte/transition'
  import { ClearPageProperty, SetPageType } from '../../bindings/silt/app.js'
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
  let previouslyFocused: HTMLElement | null = null
  let wasOpen = false

  // Local mirror of the most recent field/type-switch failure so the aria-live
  // banner appears immediately on rejection in an isolated render. The external
  // `error` prop carries controller-level failures (refresh); the banner shows
  // whichever is set.
  let liveError = $state('')

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
    }
  }

  function handleChooseType(name: string): void {
    menuOpen = false
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
      // Clear orphans BEFORE the switch: ClearPageProperty resolves the CURRENT
      // (old) schema, so it would reject an orphaned name once the new type
      // is in place. Clearing first removes the keys while they're still known.
      // Sequential on purpose: each call read-modify-writes the SAME markdown
      // file, so concurrent clears would lose updates. Attempt ALL of them and
      // collect failures — aborting the switch on the first error would leave
      // the page half-cleaned with no indication of which clears landed.
      if (clearOrphaned && orphanNames.length > 0) {
        const failed: string[] = []
        for (const p of orphanNames) {
          try {
            await ClearPageProperty(
              locator.notebook,
              locator.section,
              locator.page,
              p
            )
          } catch {
            failed.push(p)
          }
        }
        if (failed.length > 0) {
          const noun = failed.length === 1 ? 'property' : 'properties'
          throw new Error(
            `Failed to clear ${failed.length} ${noun}: ${failed.join(', ')}`
          )
        }
      }
      await commitType(newId)
    } catch (e) {
      const message = coerceIPCError(e).message
      liveError = message
      onError(message)
      onChanged()
    }
  }

  function cancelTurnInto(): void {
    turnInto = null
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

  // Type-picker menu state. `menuOpen` doubles as the panel-Esc deferral flag
  // (an open popover consumes Escape first).
  let menuOpen = $state(false)
  let menuButtonRef = $state<HTMLButtonElement | null>(null)

  // Viewport-aware placement. This panel docks to the bottom of the window, so
  // a menu opening downward off the anchor would clip past the viewport edge.
  // Measuring both sides lets the menu flip upward when there's more room
  // above, and the dynamic max-height clamps it to whichever side wins so it
  // never overflows regardless of viewport height or number of entries.
  let menuPlacement = $state<'bottom' | 'top'>('bottom')
  let menuMaxHeightPx = $state<number | null>(null)

  // Anchor→menu gap (matches the CSS `top: calc(100% + 0.2rem)` — a hair
  // under 4px) + a small viewport-edge pad so the menu never kisses the
  // window border.
  const MENU_GAP_PX = 4
  const MENU_VIEWPORT_PAD_PX = 8
  // Soft cap so the menu doesn't dominate tall viewports; the chosen side's
  // available space wins when it's smaller than this.
  const MENU_SOFT_CAP_PX = 18 * 16

  function measureMenu(): void {
    if (!menuButtonRef) {
      menuPlacement = 'bottom'
      menuMaxHeightPx = null
      return
    }
    const rect = menuButtonRef.getBoundingClientRect()
    const spaceBelow =
      window.innerHeight - rect.bottom - MENU_GAP_PX - MENU_VIEWPORT_PAD_PX
    const spaceAbove = rect.top - MENU_GAP_PX - MENU_VIEWPORT_PAD_PX
    if (spaceBelow >= spaceAbove) {
      menuPlacement = 'bottom'
      menuMaxHeightPx = Math.min(MENU_SOFT_CAP_PX, Math.max(0, spaceBelow))
    } else {
      menuPlacement = 'top'
      menuMaxHeightPx = Math.min(MENU_SOFT_CAP_PX, Math.max(0, spaceAbove))
    }
  }

  function openMenu(): void {
    menuOpen = true
    void tick().then(() => {
      measureMenu()
      const first = panelRef?.querySelector<HTMLElement>('[role="menuitem"]')
      first?.focus()
    })
  }

  function toggleMenu(): void {
    menuOpen = !menuOpen
    if (menuOpen) {
      void tick().then(() => {
        measureMenu()
        const first = panelRef?.querySelector<HTMLElement>('[role="menuitem"]')
        first?.focus()
      })
    }
  }

  // Re-measure on viewport resize while the menu is open (window shrunk,
  // editor column resized, etc.). Also pings once on the next frame in case
  // the panel layout shifted between open and paint.
  $effect(() => {
    if (!menuOpen) return
    const onResize = (): void => measureMenu()
    window.addEventListener('resize', onResize)
    const raf = requestAnimationFrame(onResize)
    return () => {
      window.removeEventListener('resize', onResize)
      cancelAnimationFrame(raf)
    }
  })

  function closeMenu(): void {
    menuOpen = false
    menuButtonRef?.focus()
  }

  // Empty-state escape hatches: open the in-app type editor or restore the
  // shipped examples. Both close the menu first so the modal/IPC owns focus
  // and the menu doesn't reopen on the next render.
  function handleCreateType(): void {
    menuOpen = false
    onCreateType?.()
  }

  function handleRestoreExamples(): void {
    menuOpen = false
    onRestoreExamples?.()
  }

  // Slash-command bridge: when the host bumps typeMenuRequest (and the panel is
  // open), open the type menu. The initial 0 value is treated as "no request"
  // so this doesn't fire on first mount.
  $effect(() => {
    void typeMenuRequest
    if (!open || typeMenuRequest === 0) return
    openMenu()
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
    if (e.key === 'Escape') {
      if (menuOpen) {
        e.preventDefault()
        closeMenu()
        return
      }
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

  // Click-outside closes the type menu (not the whole panel).
  function onPanelPointerDown(e: PointerEvent): void {
    if (!menuOpen) return
    const target = e.target as HTMLElement | null
    if (target && !target.closest('[data-type-menu]')) closeMenu()
  }

  let typeName = $derived(info.isSet ? info.type.name || info.type.id : '')
  let hasType = $derived(info.isSet)
  // A `type:` ref that didn't resolve to a known type def. The strip renders a
  // subdued raw chip for this; the panel matches with a distinct message + a
  // remove affordance to clear the bogus ref.
  let isUnknownType = $derived(!info.isSet && info.rawType.length > 0)
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
    onpointerdown={onPanelPointerDown}
  >
    <header class="header">
      <div class="title-group" data-type-menu>
        <button
          type="button"
          bind:this={menuButtonRef}
          class="type-button"
          onclick={toggleMenu}
          aria-haspopup="menu"
          aria-expanded={menuOpen}
        >
          {#if hasType && info.type.icon}
            <span
              class="material-symbols-outlined text-icon-sm"
              aria-hidden="true">{info.type.icon}</span
            >
          {:else}
            <span
              class="material-symbols-outlined text-icon-sm"
              aria-hidden="true">label</span
            >
          {/if}
          <span>{hasType ? typeName : 'Assign a type'}</span>
          <span
            class="material-symbols-outlined text-icon-sm"
            aria-hidden="true">{menuOpen ? 'expand_less' : 'expand_more'}</span
          >
        </button>

        {#if menuOpen}
          <div
            class="menu"
            class:menu-top={menuPlacement === 'top'}
            style={menuMaxHeightPx != null
              ? `max-height: ${menuMaxHeightPx}px`
              : ''}
            role="menu"
            aria-label="Page type"
          >
            {#if typesLoading && types.length === 0}
              <div class="menu-hint" role="status">Loading…</div>
            {:else if types.length === 0}
              <!--
                The dead-end empty state: instead of just announcing "No types
                defined." and leaving the user stuck, offer the two in-app
                escapes (the editor and the example restore). Status role stays
                so screen readers announce the situation; the menu items are
                the actions.
              -->
              <div class="menu-hint" role="status">No types defined.</div>
              <button
                type="button"
                role="menuitem"
                class="menu-item"
                onclick={handleCreateType}
              >
                <span
                  class="material-symbols-outlined text-icon-sm"
                  aria-hidden="true">add_circle</span
                >
                Create type…
              </button>
              <button
                type="button"
                role="menuitem"
                class="menu-item"
                onclick={handleRestoreExamples}
              >
                <span
                  class="material-symbols-outlined text-icon-sm"
                  aria-hidden="true">restart_alt</span
                >
                Restore examples
              </button>
            {:else}
              {#each types as t (t.id)}
                <button
                  type="button"
                  role="menuitem"
                  class="menu-item"
                  class:active={hasType && info.type.id === t.id}
                  onclick={() => handleChooseType(t.id)}
                >
                  {#if t.icon}
                    <span
                      class="material-symbols-outlined text-icon-sm"
                      aria-hidden="true">{t.icon}</span
                    >
                  {/if}
                  <span>{t.name || t.id}</span>
                </button>
              {/each}
            {/if}
            {#if hasType || isUnknownType}
              <div class="menu-sep" role="separator" aria-hidden="true"></div>
              <button
                type="button"
                role="menuitem"
                class="menu-item danger"
                onclick={() => handleChooseType('')}
              >
                <span
                  class="material-symbols-outlined text-icon-sm"
                  aria-hidden="true">remove_circle_outline</span
                >
                Remove type
              </button>
            {/if}
          </div>
        {/if}
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
    position: relative;
    min-width: 0;
  }
  .type-button {
    display: inline-flex;
    align-items: center;
    gap: 0.3rem;
    padding: 0.25rem 0.5rem;
    border-radius: 0.375rem;
    border: 1px solid var(--color-surface-panel-border);
    background: var(--color-surface-app);
    color: var(--color-text-primary);
    font-size: var(--text-type-sm);
    cursor: pointer;
    max-width: 20rem;
  }
  .type-button:hover {
    background: var(--color-hover);
  }
  .type-button:focus-visible {
    outline: 2px solid var(--color-border-focus);
    outline-offset: 1px;
  }
  .type-button > span:nth-child(2) {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .menu {
    position: absolute;
    /* Default opens below the trigger. `measureMenu()` flips this when there's
       more room above (bottom-docked panel case). */
    top: calc(100% + 0.2rem);
    left: 0;
    z-index: 50;
    min-width: 14rem;
    /* Soft cap; the inline `max-height` set by measureMenu() clamps further to
       the available viewport space. Kept here so a non-JS / pre-measure frame
       still has a reasonable bound. */
    max-height: 18rem;
    overflow-y: auto;
    background: var(--color-surface-popover);
    border: 1px solid var(--color-surface-popover-border);
    border-radius: 0.5rem;
    box-shadow: var(--shadow-lg);
    padding: 0.25rem;
  }
  .menu-top {
    /* Anchor the menu above the trigger (measured side with more room). */
    top: auto;
    bottom: calc(100% + 0.2rem);
  }
  .menu-item {
    display: flex;
    align-items: center;
    gap: 0.4rem;
    width: 100%;
    text-align: left;
    padding: 0.35rem 0.5rem;
    border: 0;
    background: transparent;
    color: var(--color-surface-popover-text);
    border-radius: 0.3rem;
    font-size: var(--text-type-sm);
    cursor: pointer;
  }
  .menu-item:hover,
  .menu-item:focus-visible {
    background: var(--color-hover);
    outline: none;
  }
  .menu-item:focus-visible {
    outline: 2px solid var(--color-border-focus);
    outline-offset: 1px;
  }
  .menu-item.active {
    color: var(--color-accent-primary-start);
  }
  .menu-item.danger {
    color: var(--color-status-danger);
  }
  .menu-hint {
    padding: 0.4rem 0.5rem;
    color: var(--color-text-muted);
    font-size: var(--text-type-xs);
  }
  .menu-sep {
    height: 1px;
    background: var(--color-surface-popover-border);
    margin: 0.2rem 0;
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
