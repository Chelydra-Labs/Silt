<script lang="ts">
  import type {
    NavNotebook,
    NavigationPageRef,
    RecentPageRef
  } from '../lib/sidebar/types'
  import {
    locatorKey,
    pagePathLabel,
    RECENT_COLLAPSED_LIMIT
  } from '../lib/sidebar/navigationPreferences'

  interface Props {
    favorites: NavigationPageRef[]
    recents: RecentPageRef[]
    staleKeys: Set<string>
    notebooks: NavNotebook[]
    loading: boolean
    error: string
    collapsed: boolean
    onOpen: (ref: NavigationPageRef) => void
    onToggleFavorite: (ref: NavigationPageRef) => void
    onCollapsedChange: (collapsed: boolean) => void
    onRetry: () => void
  }

  let {
    favorites,
    recents,
    staleKeys,
    notebooks,
    loading,
    error,
    collapsed,
    onOpen,
    onToggleFavorite,
    onCollapsedChange,
    onRetry
  }: Props = $props()
  let emptyExpanded = $state(false)
  /** Session-only; not persisted to vault prefs. */
  let recentExpanded = $state(false)
  let hasPages = $derived(favorites.length > 0 || recents.length > 0)
  let effectiveCollapsed = $derived(
    emptyExpanded || error ? false : collapsed || !hasPages
  )
  let pinnedKeys = $derived(new Set(favorites.map((ref) => locatorKey(ref))))
  let recentHasMore = $derived(recents.length > RECENT_COLLAPSED_LIMIT)
  let visibleRecents = $derived(
    recentExpanded || !recentHasMore
      ? recents
      : recents.slice(0, RECENT_COLLAPSED_LIMIT)
  )

  function toggleCollapsed() {
    if (effectiveCollapsed && !hasPages && !error) {
      emptyExpanded = true
      onCollapsedChange(false)
      return
    }
    emptyExpanded = false
    onCollapsedChange(!effectiveCollapsed)
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
</script>

<section class="mx-1 mb-1" aria-labelledby="quick-access-title">
  <button
    type="button"
    class="w-full border-none bg-transparent px-2 py-1.5 flex items-center gap-1 cursor-pointer rounded text-left text-surface-sidebar-text-muted hover:text-surface-sidebar-text hover:bg-hover focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-accent-primary-start"
    aria-expanded={!effectiveCollapsed}
    aria-controls="quick-access-content"
    onclick={toggleCollapsed}
  >
    <span
      class="material-symbols-outlined text-icon-md transition-transform motion-reduce:transition-none"
      class:rotate-90={!effectiveCollapsed}
      aria-hidden="true">chevron_right</span
    >
    <span
      id="quick-access-title"
      class="flex-1 text-type-2xs font-label-sm-bold"
    >
      Quick access
    </span>
    {#if hasPages}<span
        class="text-type-3xs text-surface-sidebar-text-muted"
        aria-hidden="true">{favorites.length + recents.length}</span
      >{/if}
  </button>

  {#if !effectiveCollapsed}
    <div id="quick-access-content">
      {#if loading}
        <p
          class="px-2.5 pb-2 m-0 text-type-2xs text-surface-sidebar-text-muted"
        >
          Loading saved pages…
        </p>
      {:else if error}
        <div class="px-2.5 pb-2" role="status">
          <p class="m-0 text-type-2xs text-status-warn">{error}</p>
          <button class="retry" type="button" onclick={onRetry}
            >Try again</button
          >
        </div>
      {:else}
        <div class="px-1.5 pb-1.5 grid gap-1">
          {#if !hasPages}
            <p class="empty">
              No pinned or recent pages yet. Open a page or pin one to Quick
              Access.
            </p>
          {/if}
          {#if favorites.length > 0}<div>
              <h3 class="group-title">Pinned</h3>
              {#each favorites as ref (locatorKey(ref))}
                {@const itemState = pageState(ref)}
                <div class="quick-row">
                  <button
                    type="button"
                    class="page-link"
                    disabled={itemState.unavailable}
                    title={pagePathLabel(ref)}
                    aria-label={`${pagePathLabel(ref)}${itemState.label ? ` — ${itemState.label}` : ''}`}
                    onclick={() => onOpen(ref)}
                  >
                    <span class="truncate">{ref.page}</span>
                    <span class="path truncate"
                      >{ref.notebook}{ref.section
                        ? ` / ${ref.section}`
                        : ''}</span
                    >
                    {#if itemState.label}<span class="status"
                        >{itemState.label}</span
                      >{/if}
                  </button>
                  <button
                    type="button"
                    class="pin-toggle pinned"
                    aria-label={`Unpin ${ref.page} from Quick Access`}
                    title="Unpin"
                    onclick={() => onToggleFavorite(ref)}
                  >
                    <span
                      class="material-symbols-outlined text-icon-sm"
                      aria-hidden="true">push_pin</span
                    >
                  </button>
                </div>
              {/each}
            </div>{/if}

          {#if recents.length > 0}<div
              class:border-t={favorites.length > 0}
              class:pt-1={favorites.length > 0}
              class="border-surface-sidebar-border/70"
            >
              <h3 class="group-title">Recent</h3>
              {#each visibleRecents as ref (locatorKey(ref))}
                {@const itemState = pageState(ref)}
                {@const pinned = isPinned(ref)}
                <div class="quick-row">
                  <button
                    type="button"
                    class="page-link recent"
                    disabled={itemState.unavailable}
                    title={pagePathLabel(ref)}
                    aria-label={`${pagePathLabel(ref)}${itemState.label ? ` — ${itemState.label}` : ''}`}
                    onclick={() => onOpen(ref)}
                  >
                    <span class="truncate">{ref.page}</span>
                    <span class="path truncate"
                      >{ref.notebook}{ref.section
                        ? ` / ${ref.section}`
                        : ''}</span
                    >
                    {#if itemState.label}<span class="status"
                        >{itemState.label}</span
                      >{/if}
                  </button>
                  <button
                    type="button"
                    class="pin-toggle"
                    class:pinned
                    aria-label={pinned
                      ? `Unpin ${ref.page} from Quick Access`
                      : `Pin ${ref.page} to Quick Access`}
                    title={pinned ? 'Unpin' : 'Pin to Quick Access'}
                    onclick={() => onToggleFavorite(ref)}
                  >
                    <span
                      class="material-symbols-outlined text-icon-sm"
                      class:pin-outline={!pinned}
                      aria-hidden="true">push_pin</span
                    >
                  </button>
                </div>
              {/each}
              {#if recentHasMore}
                <button
                  type="button"
                  class="show-more"
                  aria-expanded={recentExpanded}
                  onclick={() => {
                    recentExpanded = !recentExpanded
                  }}
                >
                  {recentExpanded ? 'Show less' : 'Show more'}
                </button>
              {/if}
            </div>{/if}
        </div>
      {/if}
    </div>
  {/if}
</section>

<style>
  .group-title {
    margin: 0;
    padding: 0.25rem 0.35rem;
    display: flex;
    gap: 0.35rem;
    align-items: center;
    color: var(--color-surface-sidebar-text-muted);
    font-size: var(--text-type-3xs);
    font-weight: 650;
  }
  .empty {
    margin: 0;
    padding: 0.2rem 0.35rem 0.45rem;
    color: var(--color-surface-sidebar-text-muted);
    font-size: var(--text-type-3xs);
  }
  .quick-row {
    display: flex;
    align-items: center;
    gap: 0.15rem;
  }
  .page-link {
    min-width: 0;
    width: 100%;
    padding: 0.35rem;
    border: 0;
    border-radius: 0.4rem;
    background: transparent;
    color: var(--color-surface-sidebar-text);
    text-align: left;
    cursor: pointer;
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto;
    gap: 0 0.35rem;
    font: inherit;
    font-size: var(--text-type-2xs);
  }
  .page-link:hover:not(:disabled),
  .page-link:focus-visible {
    background: var(--color-hover);
    outline: none;
  }
  .page-link:disabled {
    cursor: default;
    opacity: 0.62;
  }
  .path {
    grid-column: 1 / -1;
    color: var(--color-surface-sidebar-text-muted);
    font-size: var(--text-type-3xs);
  }
  .status {
    grid-column: 1 / -1;
    color: var(--color-status-warn);
    font-size: var(--text-type-3xs);
  }
  .pin-toggle {
    flex: 0 0 auto;
    border: 0;
    background: transparent;
    color: var(--color-surface-sidebar-text-muted);
    border-radius: 0.35rem;
    padding: 0.25rem;
    cursor: pointer;
  }
  .pin-toggle.pinned {
    color: var(--color-accent-primary-start);
  }
  .pin-toggle:hover,
  .pin-toggle:focus-visible {
    background: var(--color-hover);
    outline: 1px solid var(--color-accent-primary-start);
    color: var(--color-accent-primary-start);
  }
  /* Outline weight for unpinned; filled when pinned (Material Symbols default). */
  .pin-outline {
    font-variation-settings:
      'FILL' 0,
      'wght' 300,
      'GRAD' 0,
      'opsz' 20;
  }
  .pin-toggle.pinned .material-symbols-outlined {
    font-variation-settings:
      'FILL' 1,
      'wght' 400,
      'GRAD' 0,
      'opsz' 20;
  }
  .show-more {
    margin: 0.1rem 0 0;
    padding: 0.3rem 0.35rem;
    border: 0;
    border-radius: 0.35rem;
    background: transparent;
    color: var(--color-surface-sidebar-text-muted);
    font: inherit;
    font-size: var(--text-type-3xs);
    font-weight: 600;
    text-align: left;
    cursor: pointer;
    width: 100%;
  }
  .show-more:hover,
  .show-more:focus-visible {
    color: var(--color-surface-sidebar-text);
    background: var(--color-hover);
    outline: none;
  }
  .show-more:focus-visible {
    outline: 1px solid var(--color-accent-primary-start);
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
