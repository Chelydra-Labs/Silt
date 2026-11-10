<script lang="ts">
  import type {
    NavNotebook,
    NavigationPageRef,
    RecentPageRef
  } from '../lib/sidebar/types'
  import {
    locatorKey,
    pagePathLabel
  } from '../lib/sidebar/navigationPreferences'
  import ContextMenu from './ContextMenu.svelte'
  import {
    isContextMenuKey,
    openContextMenuFromKeyboard
  } from '../lib/contextMenuKey'
  import { usePageLocatorMenu } from '../lib/pageLocatorMenu.svelte'

  interface Props {
    favorites: NavigationPageRef[]
    recents: RecentPageRef[]
    staleKeys: Set<string>
    notebooks: NavNotebook[]
    activeNotebook?: string
    activeSection?: string
    activePage?: string
    loading: boolean
    error: string
    onOpen: (ref: NavigationPageRef) => void
    onToggleFavorite: (ref: NavigationPageRef) => void
    /** Open in a pinned tab; omitted hosts hide the "Open in new tab" item. */
    onOpenInNewTab?: (ref: NavigationPageRef) => boolean
    onRetry: () => void
  }

  let {
    favorites,
    recents,
    staleKeys,
    notebooks,
    activeNotebook,
    activeSection,
    activePage,
    loading,
    error,
    onOpen,
    onToggleFavorite,
    onOpenInNewTab,
    onRetry
  }: Props = $props()

  let pinnedKeys = $derived(new Set(favorites.map((ref) => locatorKey(ref))))

  // Shared page-locator menu (#1120): one instance for both row kinds. The
  // host passes only the actions that apply here — toggle favorite (existing
  // prop), open in new tab (host), and copy reference (owned by the helper).
  // The pin handler is injected only when the host provides it so the item
  // subset reflects the prop, not an always-present wrapper.
  // svelte-ignore state_referenced_locally
  // The host (SidebarNavTree) passes onOpenInNewTab statically at mount; the
  // one-time capture is the intent, same as TaskContextMenu's stable ctx.
  const locatorMenu = usePageLocatorMenu<NavigationPageRef>({
    onOpenInNewTab: onOpenInNewTab
      ? (ref) => void onOpenInNewTab(ref)
      : undefined,
    onToggleFavorite: (ref) => onToggleFavorite(ref),
    isFavorite: (ref) => pinnedKeys.has(locatorKey(ref))
  })

  // Rows are native buttons (keyboard-focusable), so the context-menu key
  // dispatches a synthetic contextmenu at the focused button — the exact same
  // per-row handler runs for keyboard and mouse.
  function onRowKeydown(e: KeyboardEvent): void {
    if (!isContextMenuKey(e)) return
    e.preventDefault()
    openContextMenuFromKeyboard(e.currentTarget as HTMLElement)
  }

  function pageState(ref: NavigationPageRef) {
    const notebook = notebooks.find((item) => item.name === ref.notebook)
    if (staleKeys.has(locatorKey(ref)))
      return { unavailable: true, label: 'Unavailable' }
    if (notebook?.disconnected)
      return { unavailable: true, label: 'Linked notebook offline' }
    return { unavailable: false, label: '' }
  }

  function isPinned(ref: NavigationPageRef): boolean {
    return pinnedKeys.has(locatorKey(ref))
  }

  function isActive(ref: NavigationPageRef): boolean {
    return (
      ref.notebook === (activeNotebook ?? '') &&
      (ref.section ?? '') === (activeSection ?? '') &&
      ref.page === (activePage ?? '')
    )
  }

  function sectionBadge(ref: NavigationPageRef): string {
    const currentNb = activeNotebook ?? ''
    if (ref.notebook === currentNb) {
      return ref.section || ''
    }
    return ref.section ? ref.notebook + ' / ' + ref.section : ref.notebook
  }
</script>

<div
  id="sidebar-quick-panel"
  role="tabpanel"
  aria-label="Quick Access"
  aria-labelledby="sidebar-tab-quick"
  aria-busy={loading}
  class="flex-1 overflow-y-auto custom-scrollbar px-2 py-1 flex flex-col gap-3"
>
  <div class="sr-only" aria-live="polite">
    {loading
      ? 'Loading saved pages.'
      : error
        ? 'Saved pages could not be refreshed. Showing the last list.'
        : ''}
  </div>
  {#if loading}
    <p class="px-2 py-1 m-0 text-type-2xs text-surface-sidebar-text-muted">
      Loading saved pages…
    </p>
  {:else}
    {#if error}
      <div class="px-2 py-1" role="status">
        <p class="m-0 text-type-2xs text-status-warn">{error}</p>
        <button class="retry" type="button" onclick={onRetry}>Try again</button>
      </div>
    {/if}
    <!-- Pinned Section -->
    <div class="flex flex-col gap-1">
      <div
        class="px-1 text-surface-sidebar-text-muted text-type-3xs uppercase tracking-widest font-label-sm-bold flex items-center gap-1 select-none"
      >
        <span
          class="material-symbols-outlined text-icon-xs text-accent-primary-start"
          aria-hidden="true">push_pin</span
        >
        <span>Pinned ({favorites.length})</span>
      </div>
      {#if favorites.length === 0}
        <p class="empty">
          No pinned pages yet. Pin pages to access them quickly.
        </p>
      {:else}
        <div class="flex flex-col gap-0.5">
          {#each favorites as ref (locatorKey(ref))}
            {@const itemState = pageState(ref)}
            {@const active = isActive(ref)}
            {@const badge = sectionBadge(ref)}
            <div class="quick-row group">
              <button
                type="button"
                class="page-link"
                class:active
                disabled={itemState.unavailable}
                title={pagePathLabel(ref)}
                aria-label={itemState.label
                  ? pagePathLabel(ref) + ' — ' + itemState.label
                  : pagePathLabel(ref)}
                aria-keyshortcuts="Shift+F10"
                onclick={() => onOpen(ref)}
                onkeydown={onRowKeydown}
                oncontextmenu={(e) => {
                  // Unavailable rows keep the native menu — the locator
                  // actions target a page that can't be opened.
                  if (!itemState.unavailable) locatorMenu.openFromEvent(e, ref)
                }}
              >
                <span
                  class="material-symbols-outlined text-icon-xs text-surface-sidebar-text-muted flex-shrink-0"
                  aria-hidden="true">description</span
                >
                <span class="truncate flex-1">{ref.page}</span>
                {#if badge}
                  <span class="path-badge truncate">{badge}</span>
                {/if}
                {#if itemState.label}
                  <span class="status">{itemState.label}</span>
                {/if}
              </button>
              <button
                type="button"
                class="pin-toggle hover-only pinned"
                aria-label={'Unpin ' + ref.page + ' from Quick Access'}
                title="Unpin"
                onclick={() => onToggleFavorite(ref)}
              >
                <span
                  class="material-symbols-outlined pin-icon"
                  aria-hidden="true">push_pin</span
                >
              </button>
            </div>
          {/each}
        </div>
      {/if}
    </div>

    <!-- Recent Section -->
    <div
      class="flex flex-col gap-1 border-t border-surface-sidebar-border/40 pt-2"
    >
      <div
        class="px-1 text-surface-sidebar-text-muted text-type-3xs uppercase tracking-widest font-label-sm-bold flex items-center gap-1 select-none"
      >
        <span
          class="material-symbols-outlined text-icon-xs text-surface-sidebar-text-muted"
          aria-hidden="true">schedule</span
        >
        <span>Recent ({recents.length})</span>
      </div>
      {#if recents.length === 0}
        <p class="empty">No recent pages yet.</p>
      {:else}
        <div class="flex flex-col gap-0.5">
          {#each recents as ref (locatorKey(ref))}
            {@const itemState = pageState(ref)}
            {@const pinned = isPinned(ref)}
            {@const active = isActive(ref)}
            {@const badge = sectionBadge(ref)}
            <div class="quick-row group">
              <button
                type="button"
                class="page-link recent"
                class:active
                disabled={itemState.unavailable}
                title={pagePathLabel(ref)}
                aria-label={itemState.label
                  ? pagePathLabel(ref) + ' — ' + itemState.label
                  : pagePathLabel(ref)}
                aria-keyshortcuts="Shift+F10"
                onclick={() => onOpen(ref)}
                onkeydown={onRowKeydown}
                oncontextmenu={(e) => {
                  if (!itemState.unavailable) locatorMenu.openFromEvent(e, ref)
                }}
              >
                <span
                  class="material-symbols-outlined text-icon-xs text-surface-sidebar-text-muted flex-shrink-0"
                  aria-hidden="true">description</span
                >
                <span class="truncate flex-1">{ref.page}</span>
                {#if badge}
                  <span class="path-badge truncate">{badge}</span>
                {/if}
                {#if itemState.label}
                  <span class="status">{itemState.label}</span>
                {/if}
              </button>
              <button
                type="button"
                class="pin-toggle hover-only"
                class:pinned
                aria-label={pinned
                  ? 'Unpin ' + ref.page + ' from Quick Access'
                  : 'Pin ' + ref.page + ' to Quick Access'}
                title={pinned ? 'Unpin' : 'Pin to Quick Access'}
                onclick={() => onToggleFavorite(ref)}
              >
                <span
                  class="material-symbols-outlined pin-icon"
                  class:pin-outline={!pinned}
                  aria-hidden="true">push_pin</span
                >
              </button>
            </div>
          {/each}
        </div>
      {/if}
    </div>
  {/if}

  {#if locatorMenu.menu}
    <ContextMenu
      open={true}
      anchor={{ x: locatorMenu.menu.x, y: locatorMenu.menu.y }}
      anchorEl={locatorMenu.menu.anchorEl}
      onClose={locatorMenu.close}
      ariaLabel={`Actions for ${locatorMenu.menu.locator.page}`}
      menuTestId="quick-access-locator-menu"
    >
      {#each locatorMenu.items as item (item.id)}
        <button
          type="button"
          role="menuitem"
          data-testid={`quick-access-menu-${item.id}`}
          onclick={item.run}
        >
          <span
            class="material-symbols-outlined text-icon-md"
            aria-hidden="true">{item.icon}</span
          >
          <span>{item.label}</span>
        </button>
      {/each}
    </ContextMenu>
  {/if}
</div>

<style>
  .empty {
    margin: 0;
    padding: 0.25rem 0.35rem;
    color: var(--color-surface-sidebar-text-muted);
    font-size: var(--text-type-3xs);
  }
  .quick-row {
    display: flex;
    align-items: center;
    gap: 0.15rem;
    position: relative;
    border-radius: 0.35rem;
  }
  .page-link {
    min-width: 0;
    width: 100%;
    padding: 0.25rem 0.35rem;
    border: 0;
    border-radius: 0.35rem;
    background: transparent;
    color: var(--color-surface-sidebar-text);
    text-align: left;
    cursor: pointer;
    display: flex;
    align-items: center;
    gap: 0.35rem;
    font: inherit;
    font-size: var(--text-type-2xs);
    line-height: 1.2;
  }
  .page-link:hover:not(:disabled),
  .page-link:focus-visible {
    background: var(--color-hover);
    outline: none;
  }
  .page-link.active {
    background: var(--color-hover);
    color: var(--color-accent-primary-start);
    font-weight: 600;
  }
  .page-link:disabled {
    cursor: default;
    opacity: 0.62;
  }
  .path-badge {
    max-width: 110px;
    color: var(--color-surface-sidebar-text-muted);
    font-size: var(--text-type-3xs);
    opacity: 0.75;
    flex-shrink: 1;
  }
  .status {
    color: var(--color-status-warn);
    font-size: var(--text-type-3xs);
    flex-shrink: 0;
  }
  .pin-toggle {
    flex: 0 0 auto;
    border: 0;
    background: transparent;
    color: var(--color-surface-sidebar-text-muted);
    border-radius: 0.25rem;
    padding: 0.1rem 0.2rem;
    cursor: pointer;
    transition:
      opacity 120ms ease,
      color 120ms ease,
      background 120ms ease;
    display: inline-flex;
    align-items: center;
    justify-content: center;
  }
  .pin-toggle.hover-only {
    opacity: 0;
  }
  .quick-row:hover .pin-toggle.hover-only,
  .pin-toggle:hover,
  .pin-toggle:focus-visible {
    opacity: 0.75;
  }
  .pin-toggle:hover,
  .pin-toggle:focus-visible {
    opacity: 1;
    background: var(--color-hover);
    color: var(--color-accent-primary-start);
  }
  .pin-icon {
    font-size: 13px !important;
    width: 13px;
    height: 13px;
    line-height: 13px;
  }
  /* Outline weight for unpinned; filled when pinned (Material Symbols default). */
  .pin-outline {
    font-variation-settings:
      'FILL' 0,
      'wght' 250,
      'GRAD' 0,
      'opsz' 20;
  }
  .pin-toggle.pinned .pin-icon {
    font-variation-settings:
      'FILL' 1,
      'wght' 350,
      'GRAD' 0,
      'opsz' 20;
  }
  .retry {
    margin-top: 0.3rem;
    border: 0;
    padding: 0;
    background: transparent;
    color: var(--color-accent-primary-start);
    text-decoration: underline;
    cursor: pointer;
    font-size: var(--text-type-2xs);
  }
</style>
