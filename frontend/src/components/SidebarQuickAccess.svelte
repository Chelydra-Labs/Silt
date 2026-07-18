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

  interface Props {
    favorites: NavigationPageRef[]
    recents: RecentPageRef[]
    staleKeys: Set<string>
    notebooks: NavNotebook[]
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
    loading,
    error,
    onOpen,
    onToggleFavorite,
    onRetry
  }: Props = $props()

  function state(ref: NavigationPageRef) {
    const notebook = notebooks.find((item) => item.name === ref.notebook)
    if (staleKeys.has(locatorKey(ref)))
      return { unavailable: true, label: 'Unavailable' }
    if (notebook?.disconnected)
      return { unavailable: true, label: 'Linked notebook offline' }
    return { unavailable: false, label: '' }
  }
</script>

<section
  class="mx-1 mb-2 rounded-xl border border-accent-primary-start/20 overflow-hidden quick-access"
  aria-labelledby="quick-access-title"
>
  <div class="px-2.5 pt-2 pb-1 flex items-center justify-between">
    <h2
      id="quick-access-title"
      class="m-0 text-type-3xs uppercase tracking-[0.16em] font-label-sm-bold text-accent-primary-start"
    >
      Quick access
    </h2>
    <span
      class="material-symbols-outlined text-icon-sm text-accent-primary-start/70"
      aria-hidden="true">bolt</span
    >
  </div>

  {#if loading}
    <p class="px-2.5 pb-2 m-0 text-type-2xs text-surface-sidebar-text-muted">
      Loading saved pages…
    </p>
  {:else if error}
    <div class="px-2.5 pb-2" role="status">
      <p class="m-0 text-type-2xs text-status-warn">{error}</p>
      <button class="retry" type="button" onclick={onRetry}>Try again</button>
    </div>
  {:else}
    <div class="px-1.5 pb-1.5 grid gap-1">
      <div>
        <h3 class="group-title">
          <span
            class="material-symbols-outlined text-icon-sm"
            aria-hidden="true">star</span
          >
          Favorites
        </h3>
        {#if favorites.length === 0}
          <p class="empty">Favorite a page from its menu.</p>
        {:else}
          {#each favorites as ref (locatorKey(ref))}
            {@const itemState = state(ref)}
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
                  >{ref.notebook}{ref.section ? ` / ${ref.section}` : ''}</span
                >
                {#if itemState.label}<span class="status"
                    >{itemState.label}</span
                  >{/if}
              </button>
              <button
                type="button"
                class="favorite-toggle"
                aria-label={`Remove ${ref.page} from favorites`}
                title="Remove from favorites"
                onclick={() => onToggleFavorite(ref)}
              >
                <span
                  class="material-symbols-outlined text-icon-sm"
                  aria-hidden="true">star</span
                >
              </button>
            </div>
          {/each}
        {/if}
      </div>

      <div class="border-t border-surface-sidebar-border/70 pt-1">
        <h3 class="group-title">
          <span
            class="material-symbols-outlined text-icon-sm"
            aria-hidden="true">history</span
          >
          Recent
        </h3>
        {#if recents.length === 0}
          <p class="empty">Pages you open will appear here.</p>
        {:else}
          {#each recents.slice(0, 6) as ref (locatorKey(ref))}
            {@const itemState = state(ref)}
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
                >{ref.notebook}{ref.section ? ` / ${ref.section}` : ''}</span
              >
              {#if itemState.label}<span class="status">{itemState.label}</span
                >{/if}
            </button>
          {/each}
        {/if}
      </div>
    </div>
  {/if}
</section>

<style>
  .quick-access {
    background:
      radial-gradient(
        circle at 100% 0%,
        color-mix(in srgb, var(--color-accent-primary-start) 15%, transparent),
        transparent 52%
      ),
      color-mix(in srgb, var(--color-surface-card) 78%, transparent);
    box-shadow: inset 0 1px 0
      color-mix(in srgb, var(--color-text-primary) 7%, transparent);
  }
  .group-title {
    margin: 0;
    padding: 0.25rem 0.35rem;
    display: flex;
    gap: 0.35rem;
    align-items: center;
    color: var(--color-surface-sidebar-text);
    font-size: var(--text-type-2xs);
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
  .favorite-toggle {
    flex: 0 0 auto;
    border: 0;
    background: transparent;
    color: var(--color-accent-primary-start);
    border-radius: 0.35rem;
    padding: 0.25rem;
    cursor: pointer;
  }
  .favorite-toggle:hover,
  .favorite-toggle:focus-visible {
    background: var(--color-hover);
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
