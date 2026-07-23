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
    activeNotebook?: string
    activeSection?: string
    activePage?: string
    loading: boolean
    error: string
    onOpen: (ref: NavigationPageRef) => void
    onToggleFavorite: (ref: NavigationPageRef) => void
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
    onRetry
  }: Props = $props()

  let explicitTab: string | null = $state(null)
  let activeTab = $derived.by(() => {
    if (explicitTab !== null) return explicitTab
    return favorites.length > 0 ? 'pinned' : 'recent'
  })

  /** Session-only; not persisted to vault prefs. */
  let recentExpanded = $state(false)

  let hasPages = $derived(favorites.length > 0 || recents.length > 0)
  let pinnedKeys = $derived(new Set(favorites.map((ref) => locatorKey(ref))))
  let recentHasMore = $derived(recents.length > RECENT_COLLAPSED_LIMIT)
  let visibleRecents = $derived(
    recentExpanded || !recentHasMore
      ? recents
      : recents.slice(0, RECENT_COLLAPSED_LIMIT)
  )

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

{#if hasPages || loading || error}
  <section
    class="mx-1 mb-2.5 p-1 bg-surface-sidebar border border-surface-sidebar-border/70 rounded-lg shadow-sm"
    aria-label="Quick access"
  >
    {#if loading}
      <p class="px-2 py-1 m-0 text-type-2xs text-surface-sidebar-text-muted">
        Loading saved pages…
      </p>
    {:else if error}
      <div class="px-2 py-1" role="status">
        <p class="m-0 text-type-2xs text-status-warn">{error}</p>
        <button class="retry" type="button" onclick={onRetry}>Try again</button>
      </div>
    {:else}
      <!-- Segmented View Switcher -->
      <div
        class="flex items-center gap-0.5 bg-hover/40 p-0.5 rounded-md mb-1 border border-surface-sidebar-border/40"
        role="tablist"
        aria-label="Quick Access Categories"
      >
        <button
          type="button"
          role="tab"
          id="quick-access-tab-pinned"
          aria-selected={activeTab === 'pinned'}
          aria-controls="quick-access-panel"
          class="flex-1 py-1 px-1.5 border-none rounded text-type-3xs font-label-sm-bold cursor-pointer transition-all flex items-center justify-center gap-1"
          class:bg-surface-sidebar={activeTab === 'pinned'}
          class:shadow-sm={activeTab === 'pinned'}
          class:text-surface-sidebar-text={activeTab === 'pinned'}
          class:text-surface-sidebar-text-muted={activeTab !== 'pinned'}
          onclick={() => (explicitTab = 'pinned')}
        >
          <span>Pinned</span>
          {#if favorites.length > 0}
            <span class="opacity-75">({favorites.length})</span>
          {/if}
        </button>
        <button
          type="button"
          role="tab"
          id="quick-access-tab-recent"
          aria-selected={activeTab === 'recent'}
          aria-controls="quick-access-panel"
          class="flex-1 py-1 px-1.5 border-none rounded text-type-3xs font-label-sm-bold cursor-pointer transition-all flex items-center justify-center gap-1"
          class:bg-surface-sidebar={activeTab === 'recent'}
          class:shadow-sm={activeTab === 'recent'}
          class:text-surface-sidebar-text={activeTab === 'recent'}
          class:text-surface-sidebar-text-muted={activeTab !== 'recent'}
          onclick={() => (explicitTab = 'recent')}
        >
          <span>Recent</span>
          {#if recents.length > 0}
            <span class="opacity-75">({recents.length})</span>
          {/if}
        </button>
      </div>

      <!-- Tab Panel Contents -->
      <div id="quick-access-panel" role="tabpanel">
        {#if activeTab === 'pinned'}
          {#if favorites.length === 0}
            <p class="empty">No pinned pages yet.</p>
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
                    onclick={() => onOpen(ref)}
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
        {:else if activeTab === 'recent'}
          {#if recents.length === 0}
            <p class="empty">No recent pages yet.</p>
          {:else}
            <div class="flex flex-col gap-0.5">
              {#each visibleRecents as ref (locatorKey(ref))}
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
                    onclick={() => onOpen(ref)}
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
            </div>
          {/if}
        {/if}
      </div>
    {/if}
  </section>
{/if}

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
  .show-more {
    margin: 0.1rem 0 0;
    padding: 0.25rem 0.35rem;
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
