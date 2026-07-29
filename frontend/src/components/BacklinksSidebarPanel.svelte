<script lang="ts">
  import { onMount } from 'svelte'
  import { Events } from '@wailsio/runtime'
  import { EventName } from '../generated/enums'
  import {
    GetBacklinksPaged,
    GetUnlinkedMentionsPaged,
    PromoteUnlinkedMention
  } from '../../bindings/silt/app.js'
  import { pushNotification } from '../notifications/store.svelte'

  export type Backlink = {
    linkKind: 'page' | 'block-ref' | 'embed'
    sourceNotebook: string
    sourceSection: string
    sourcePage: string
    sourceBlockId: string
    snippet: string
    source?: string
  }

  type BacklinksPage = {
    results: Backlink[]
    cursor: string
    hasMore: boolean
  }

  type PagePath = {
    source: string
    notebook: string
    section: string
    page: string
  }

  type UnlinkedMention = {
    source: string
    sourceNotebook: string
    sourceSection: string
    sourcePage: string
    sourceBlockIds: string[]
    matchCount: number
    title: string
    ambiguous: boolean
    candidates?: PagePath[]
  }

  type UnlinkedMentionsPage = {
    results: UnlinkedMention[]
    cursor: string
    hasMore: boolean
  }

  interface Props {
    notebook: string
    section: string
    page: string
  }

  let { notebook, section, page }: Props = $props()
  let backlinks = $state<Backlink[]>([])
  let initialLoading = $state(false)
  let refreshing = $state(false)
  let loadingMore = $state(false)
  let cursor = $state('')
  let hasMore = $state(false)
  let error = $state('')
  let errorAction = $state<'initial' | 'refresh' | 'more'>('initial')
  let request = 0
  let refreshTimer: ReturnType<typeof setTimeout> | null = null
  const pageSize = 50

  // Unlinked-mentions leg.
  let unlinked = $state<UnlinkedMention[]>([])
  let unlinkedLoading = $state(false)
  let unlinkedExpanded = $state(false)
  let unlinkedBusyBlockId = $state<string | null>(null)
  let unlinkedError = $state('')
  let unlinkedRequest = 0

  const groups = $derived.by(() => {
    // Ephemeral grouping inside $derived — plain Map is correct.
    // eslint-disable-next-line svelte/prefer-svelte-reactivity -- local derived helper
    const grouped = new Map<string, { key: string; links: Backlink[] }>()
    for (const link of backlinks) {
      const key = `${link.source ?? 'vault'}\u0000${link.sourceNotebook}\u0000${link.sourceSection}\u0000${link.sourcePage}`
      const current = grouped.get(key)
      if (current) current.links.push(link)
      else grouped.set(key, { key, links: [link] })
    }
    return [...grouped.values()]
  })

  const countLabel = $derived(
    groups.length === 1
      ? '1 page links here'
      : `${groups.length} pages link here`
  )

  async function getBacklinksPage(
    activeNotebook: string,
    activeSection: string,
    activePage: string,
    activeCursor: string
  ): Promise<BacklinksPage> {
    const result = (await GetBacklinksPaged(
      activeNotebook,
      activeSection,
      activePage,
      activeCursor,
      pageSize
    )) as unknown as BacklinksPage | null
    return result ?? { results: [], cursor: '', hasMore: false }
  }

  function backlinkKey(link: Backlink): string {
    return [
      link.source ?? 'vault',
      link.sourceNotebook,
      link.sourceSection,
      link.sourcePage,
      link.linkKind,
      link.sourceBlockId
    ].join('\u0000')
  }

  function uniqueLinks(current: Backlink[], incoming: Backlink[]): Backlink[] {
    // eslint-disable-next-line svelte/prefer-svelte-reactivity -- local helper set
    const seen = new Set(current.map(backlinkKey))
    return [
      ...current,
      ...incoming.filter((link) => {
        const key = backlinkKey(link)
        if (seen.has(key)) return false
        seen.add(key)
        return true
      })
    ]
  }

  async function loadFirstPage(
    activeNotebook: string,
    activeSection: string,
    activePage: string,
    retainProjection: boolean
  ): Promise<void> {
    const sequence = ++request
    cursor = ''
    hasMore = false
    loadingMore = false
    error = ''
    errorAction = retainProjection ? 'refresh' : 'initial'
    if (!activeNotebook || !activePage) {
      backlinks = []
      initialLoading = false
      refreshing = false
      return
    }
    if (!retainProjection) backlinks = []
    initialLoading = !retainProjection
    refreshing = retainProjection
    try {
      const result = await getBacklinksPage(
        activeNotebook,
        activeSection,
        activePage,
        ''
      )
      if (sequence !== request) return
      backlinks = uniqueLinks([], result.results ?? [])
      cursor = result.cursor ?? ''
      hasMore = Boolean(result.hasMore)
      error = ''
    } catch (cause) {
      if (sequence !== request) return
      error =
        cause instanceof Error
          ? cause.message
          : 'Backlinks could not be loaded.'
    } finally {
      if (sequence === request) {
        initialLoading = false
        refreshing = false
      }
    }
  }

  function refresh(): void {
    void loadFirstPage(notebook, section, page, backlinks.length > 0)
    void loadUnlinked(notebook, section, page)
  }

  async function loadMore(): Promise<void> {
    if (loadingMore || !hasMore || !cursor) return
    const sequence = request
    const nextCursor = cursor
    loadingMore = true
    error = ''
    errorAction = 'more'
    try {
      const result = await getBacklinksPage(notebook, section, page, nextCursor)
      if (sequence !== request) return
      backlinks = uniqueLinks(backlinks, result.results ?? [])
      cursor = result.cursor ?? ''
      hasMore = Boolean(result.hasMore)
    } catch (cause) {
      if (sequence !== request) return
      error =
        cause instanceof Error
          ? cause.message
          : 'More backlinks could not be loaded.'
    } finally {
      if (sequence === request) loadingMore = false
    }
  }

  async function getUnlinkedPage(
    activeNotebook: string,
    activeSection: string,
    activePage: string
  ): Promise<UnlinkedMention[]> {
    const result = (await GetUnlinkedMentionsPaged(
      activeNotebook,
      activeSection,
      activePage,
      '',
      pageSize
    )) as unknown as UnlinkedMentionsPage | null
    return result?.results ?? []
  }

  // loadUnlinked fetches the unlinked-mentions leg. Runs alongside the backlinks
  // refresh (same debounced block:changed trigger) but with its own request
  // sequence so a slow backlinks page never suppresses an unlinked refresh.
  async function loadUnlinked(
    activeNotebook: string,
    activeSection: string,
    activePage: string
  ): Promise<void> {
    const sequence = ++unlinkedRequest
    if (!activeNotebook || !activePage) {
      unlinked = []
      unlinkedError = ''
      return
    }
    unlinkedLoading = true
    unlinkedError = ''
    try {
      const results = await getUnlinkedPage(
        activeNotebook,
        activeSection,
        activePage
      )
      if (sequence !== unlinkedRequest) return
      unlinked = results
    } catch (cause) {
      if (sequence !== unlinkedRequest) return
      unlinkedError =
        cause instanceof Error
          ? cause.message
          : 'Unlinked mentions could not be loaded.'
    } finally {
      if (sequence === unlinkedRequest) unlinkedLoading = false
    }
  }

  // promoteMention wraps the first plain-text mention of the page title in the
  // chosen source block with a [[shortest]] link. On success the entry migrates
  // into the backlinks leg on the next refresh; the optimistic removal keeps the
  // row from flickering until the debounced refresh lands.
  async function promoteMention(mention: UnlinkedMention, blockId: string) {
    if (mention.ambiguous || unlinkedBusyBlockId) return
    unlinkedBusyBlockId = blockId
    try {
      await PromoteUnlinkedMention(blockId, notebook, section, page)
      pushNotification({
        kind: 'success',
        message: `Linked “${mention.title}” in ${mention.sourcePage}.`
      })
      // Optimistically drop the promoted block from the row; the debounced
      // block:changed refresh reconciles the rest (migrating it into backlinks).
      unlinked = unlinked
        .map((m) =>
          m === mention
            ? {
                ...m,
                sourceBlockIds: m.sourceBlockIds.filter((id) => id !== blockId),
                matchCount: Math.max(0, m.matchCount - 1)
              }
            : m
        )
        .filter((m) => m.sourceBlockIds.length > 0)
      refresh()
    } catch (cause) {
      const message =
        cause instanceof Error ? cause.message : 'Could not link the mention.'
      pushNotification({ kind: 'error', message })
    } finally {
      unlinkedBusyBlockId = null
    }
  }

  function openPage(link: Backlink): void {
    window.dispatchEvent(
      new CustomEvent('navigate-to-page', {
        detail: {
          notebook: link.sourceNotebook,
          section: link.sourceSection,
          page: link.sourcePage,
          source: link.source ?? 'vault'
        }
      })
    )
  }

  function openExactBlock(link: Backlink): void {
    window.dispatchEvent(
      new CustomEvent('navigate-to-block', {
        detail: {
          notebook: link.sourceNotebook,
          section: link.sourceSection,
          page: link.sourcePage,
          blockId: link.sourceBlockId,
          source: link.source ?? 'vault'
        }
      })
    )
  }

  function kindLabel(kind: Backlink['linkKind']): string {
    if (kind === 'block-ref') return 'Block reference'
    if (kind === 'embed') return 'Embed'
    return 'Page link'
  }

  function kindMark(kind: Backlink['linkKind']): string {
    if (kind === 'block-ref') return '(('
    if (kind === 'embed') return '{{'
    return '[['
  }

  function sourceLabel(source?: string): string {
    return source?.startsWith('linked:') ? 'Linked' : 'Vault'
  }

  onMount(() => {
    void loadUnlinked(notebook, section, page)
    const offBlock = Events.On(EventName.EventBlockChanged, () => {
      if (refreshTimer) clearTimeout(refreshTimer)
      refreshTimer = setTimeout(() => {
        refreshTimer = null
        refresh()
      }, 200)
    })
    return () => {
      request++
      unlinkedRequest++
      if (refreshTimer) clearTimeout(refreshTimer)
      offBlock()
    }
  })

  $effect(() => {
    const activeNotebook = notebook
    const activeSection = section
    const activePage = page
    if (refreshTimer) {
      clearTimeout(refreshTimer)
      refreshTimer = null
    }
    unlinkedExpanded = false
    void loadFirstPage(activeNotebook, activeSection, activePage, false)
    void loadUnlinked(activeNotebook, activeSection, activePage)
  })
</script>

<nav
  aria-label="Backlinks to current page"
  class="flex-grow flex flex-col min-h-0 bg-surface-sidebar"
>
  <header
    class="px-3 py-3 border-b border-surface-sidebar-border flex-shrink-0"
  >
    <div class="flex items-center gap-2">
      <span
        class="material-symbols-outlined text-accent-primary-start text-type-2xl"
        aria-hidden="true">hub</span
      >
      <div class="min-w-0">
        <h2 class="font-headline-md text-headline-md text-text-primary">
          Backlinks
        </h2>
        <p
          class="m-0 text-type-2xs text-surface-sidebar-text-muted font-label-sm"
          aria-live="polite"
          aria-atomic="true"
        >
          {initialLoading
            ? 'Loading backlinks…'
            : refreshing
              ? 'Refreshing backlinks…'
              : countLabel}
        </p>
      </div>
    </div>
  </header>

  <div class="flex-grow overflow-y-auto custom-scrollbar p-2">
    {#if !notebook || !page}
      <div class="text-center px-4 py-10 text-text-muted">
        <span
          class="material-symbols-outlined text-icon-2xl opacity-60"
          aria-hidden="true">draft</span
        >
        <p class="mt-2 text-text-primary font-label-sm-bold">
          Open a page first
        </p>
        <p class="mt-1 text-type-xs leading-relaxed">
          Backlinks follow the page you are viewing.
        </p>
      </div>
    {:else if initialLoading}
      <div
        class="flex flex-col items-center px-4 py-10 text-text-muted"
        role="status"
        aria-live="polite"
      >
        <span
          class="material-symbols-outlined text-icon-2xl text-accent-primary-start animate-pulse"
          aria-hidden="true">hub</span
        >
        <p class="mt-2 text-text-primary font-label-sm-bold">
          Loading backlinks…
        </p>
        <p class="mt-1 text-type-xs">Finding pages that point here.</p>
      </div>
    {:else}
      {#if error}
        <div
          class="m-1 mb-2 p-3 rounded-lg border border-status-warn/35 bg-status-warn/10"
          role="alert"
        >
          <p class="m-0 text-type-xs font-label-sm-bold text-text-primary">
            {errorAction === 'more'
              ? 'More backlinks could not be loaded.'
              : errorAction === 'refresh'
                ? 'Backlinks could not be refreshed.'
                : 'Backlinks could not be loaded.'}
          </p>
          <p class="mt-1 mb-0 text-type-2xs text-text-muted">{error}</p>
          <button
            type="button"
            class="mt-2 p-0 border-none bg-transparent text-accent-primary-start text-type-xs underline cursor-pointer focus-visible:ring-2 focus-visible:ring-accent-primary-start rounded"
            onclick={errorAction === 'more' ? loadMore : refresh}
            >Try again</button
          >
        </div>
      {/if}
      {#if groups.length === 0 && !error}
        <div class="text-center px-4 py-10 text-text-muted">
          <span
            class="material-symbols-outlined text-icon-2xl opacity-60"
            aria-hidden="true">link_off</span
          >
          <p class="mt-2 text-text-primary font-label-sm-bold">
            No pages link here yet
          </p>
          <p class="mt-1 text-type-xs leading-relaxed">
            Link this page with <code class="text-accent-secondary-start"
              >[[page]]</code
            >
            or reference one of its blocks with
            <code class="text-accent-secondary-start">((block))</code>.
          </p>
        </div>
      {:else}
        <div class="flex flex-col gap-2">
          {#each groups as group (group.key)}
            {@const source = group.links[0]}
            <section
              class="rounded-lg border border-surface-sidebar-border bg-surface-panel/35 overflow-hidden"
            >
              <h3 class="m-0">
                <button
                  type="button"
                  class="w-full px-2.5 py-2 border-none bg-transparent text-left flex items-center gap-2 text-surface-sidebar-text hover:bg-hover cursor-pointer transition-colors focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-accent-primary-start"
                  onclick={() => openPage(source)}
                  aria-label={`Open page ${source.sourcePage}`}
                >
                  <span
                    class="material-symbols-outlined text-icon-md text-accent-primary-start"
                    aria-hidden="true">description</span
                  >
                  <span class="min-w-0 flex-1">
                    <span class="flex items-center gap-1.5 min-w-0">
                      <span class="truncate font-label-sm-bold text-label-sm"
                        >{source.sourcePage}</span
                      >
                      <span
                        class="flex-shrink-0 rounded-full border border-surface-sidebar-border bg-surface-sidebar px-1.5 py-0.5 text-type-3xs uppercase tracking-wider text-surface-sidebar-text-muted font-label-sm-bold"
                        >{sourceLabel(source.source)}</span
                      >
                    </span>
                    <span
                      class="block truncate text-type-3xs text-surface-sidebar-text-muted"
                    >
                      {[source.sourceNotebook, source.sourceSection]
                        .filter(Boolean)
                        .join(' / ')}
                    </span>
                  </span>
                  <span
                    class="text-type-3xs text-surface-sidebar-text-muted"
                    aria-label={`${group.links.length} references`}
                    >{group.links.length}</span
                  >
                </button>
              </h3>
              <ul
                class="m-0 p-0 list-none border-t border-surface-sidebar-border/70"
              >
                {#each group.links as link, index (`${link.linkKind}:${link.sourceBlockId}:${index}`)}
                  <li
                    class="flex items-stretch hover:bg-hover transition-colors"
                  >
                    <button
                      type="button"
                      class="group min-w-0 flex-1 px-2.5 py-2 border-none bg-transparent text-left flex items-start gap-2 cursor-pointer focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-accent-primary-start"
                      onclick={() => openPage(link)}
                      aria-label={`Open ${kindLabel(link.linkKind).toLowerCase()} in page ${link.sourcePage}: ${link.snippet}`}
                    >
                      <span
                        class="mt-0.5 min-w-7 text-center rounded bg-accent-primary-start/10 text-accent-primary-start font-mono text-type-2xs font-bold"
                        title={kindLabel(link.linkKind)}
                        aria-hidden="true">{kindMark(link.linkKind)}</span
                      >
                      <span class="min-w-0 flex-1">
                        <span
                          class="block text-type-3xs uppercase tracking-wider text-surface-sidebar-text-muted font-label-sm-bold"
                        >
                          {kindLabel(link.linkKind)}
                        </span>
                        <span
                          class="block mt-0.5 text-type-xs text-surface-sidebar-text leading-snug line-clamp-3"
                        >
                          {link.snippet || 'Reference in this page'}
                        </span>
                      </span>
                      <span
                        class="material-symbols-outlined text-icon-sm text-surface-sidebar-text-muted group-hover:text-accent-primary-start"
                        aria-hidden="true"
                      >
                        arrow_forward
                      </span>
                    </button>
                    {#if link.sourceBlockId}
                      <button
                        type="button"
                        class="self-stretch w-9 flex-shrink-0 border-none border-l border-surface-sidebar-border/70 bg-transparent text-surface-sidebar-text-muted hover:bg-accent-primary-start/10 hover:text-accent-primary-start cursor-pointer transition-colors focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-accent-primary-start"
                        onclick={() => openExactBlock(link)}
                        aria-label={`Jump to exact block for ${kindLabel(link.linkKind).toLowerCase()} in ${link.sourcePage}: ${link.snippet}`}
                        title="Jump to exact block"
                      >
                        <span
                          class="material-symbols-outlined text-icon-sm"
                          aria-hidden="true">my_location</span
                        >
                      </button>
                    {/if}
                  </li>
                {/each}
              </ul>
            </section>
          {/each}
          {#if hasMore}
            <button
              type="button"
              class="w-full rounded-lg border border-surface-sidebar-border bg-surface-panel/35 px-3 py-2 text-label-sm font-label-sm-bold text-accent-primary-start hover:bg-hover cursor-pointer transition-colors disabled:cursor-wait disabled:opacity-65 focus-visible:ring-2 focus-visible:ring-accent-primary-start"
              disabled={loadingMore}
              aria-label={loadingMore
                ? 'Loading more backlinks'
                : 'Load more backlinks'}
              onclick={loadMore}
            >
              {loadingMore ? 'Loading more…' : 'Load more'}
            </button>
          {/if}
        </div>
      {/if}
      {#if unlinked.length > 0 || unlinkedLoading || unlinkedError}
        <section
          class="mt-2 rounded-lg border border-surface-sidebar-border bg-surface-panel/35 overflow-hidden"
          aria-labelledby="unlinked-mentions-title"
        >
          <h3 class="m-0">
            <button
              type="button"
              class="w-full px-2.5 py-2 border-none bg-transparent text-left flex items-center gap-2 text-surface-sidebar-text hover:bg-hover cursor-pointer transition-colors focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-accent-primary-start"
              aria-expanded={unlinkedExpanded}
              aria-controls="unlinked-mentions-body"
              onclick={() => (unlinkedExpanded = !unlinkedExpanded)}
            >
              <span
                class="material-symbols-outlined text-icon-md text-accent-primary-start transition-transform {unlinkedExpanded
                  ? 'rotate-90'
                  : ''}"
                aria-hidden="true">chevron_right</span
              >
              <span class="min-w-0 flex-1">
                <span class="block truncate font-label-sm-bold text-label-sm">
                  Unlinked mentions
                </span>
                <span
                  id="unlinked-mentions-title"
                  class="block truncate text-type-3xs text-surface-sidebar-text-muted"
                  aria-live="polite"
                  aria-atomic="true"
                >
                  {unlinkedLoading
                    ? 'Finding unlinked mentions…'
                    : unlinked.length === 1
                      ? '1 page mentions this title'
                      : `${unlinked.length} pages mention this title`}
                </span>
              </span>
            </button>
          </h3>
          {#if unlinkedExpanded}
            <div
              id="unlinked-mentions-body"
              class="border-t border-surface-sidebar-border/70"
            >
              {#if unlinkedError}
                <p
                  class="m-0 px-2.5 py-2 text-type-2xs text-status-warn"
                  role="alert"
                >
                  {unlinkedError}
                </p>
              {/if}
              <ul class="m-0 p-0 list-none">
                {#each unlinked as mention, index (`${mention.source}\u0000${mention.sourceNotebook}\u0000${mention.sourceSection}\u0000${mention.sourcePage}:${index}`)}
                  <li
                    class="border-b border-surface-sidebar-border/60 last:border-b-0"
                  >
                    <div
                      class="px-2.5 py-2 flex items-center gap-2 text-surface-sidebar-text"
                    >
                      <button
                        type="button"
                        class="min-w-0 flex-1 border-none bg-transparent text-left p-0 cursor-pointer hover:text-accent-primary-start focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-accent-primary-start rounded"
                        onclick={() =>
                          openPage({
                            sourceNotebook: mention.sourceNotebook,
                            sourceSection: mention.sourceSection,
                            sourcePage: mention.sourcePage,
                            source: mention.source ?? 'vault'
                          } as Backlink)}
                        aria-label={`Open page ${mention.sourcePage}`}
                      >
                        <span class="flex items-center gap-1.5 min-w-0">
                          <span
                            class="truncate font-label-sm-bold text-label-sm"
                            >{mention.sourcePage}</span
                          >
                          <span
                            class="flex-shrink-0 rounded-full border border-surface-sidebar-border bg-surface-sidebar px-1.5 py-0.5 text-type-3xs uppercase tracking-wider text-surface-sidebar-text-muted font-label-sm-bold"
                            aria-label={`${mention.matchCount} mention${mention.matchCount === 1 ? '' : 's'}`}
                            >{mention.matchCount}</span
                          >
                          {#if mention.ambiguous}
                            <span
                              class="flex-shrink-0 rounded-full border border-status-warn/40 bg-status-warn/10 px-1.5 py-0.5 text-type-3xs uppercase tracking-wider text-status-warn font-label-sm-bold"
                              title={mention.candidates
                                ?.map(
                                  (c) => `${c.notebook}/${c.section}/${c.page}`
                                )
                                .join(', ')}>Ambiguous</span
                            >
                          {/if}
                        </span>
                        <span
                          class="block truncate text-type-3xs text-surface-sidebar-text-muted"
                        >
                          {[mention.sourceNotebook, mention.sourceSection]
                            .filter(Boolean)
                            .join(' / ')}
                        </span>
                      </button>
                    </div>
                    {#if mention.ambiguous}
                      <p
                        class="m-0 px-2.5 pb-2 text-type-3xs text-surface-sidebar-text-muted"
                      >
                        Link disabled — title matches
                        {mention.candidates
                          ?.map((c) => `${c.notebook}/${c.section}/${c.page}`)
                          .join(', ')}.
                      </p>
                    {:else}
                      <ul class="m-0 p-0 pb-1 list-none">
                        {#each mention.sourceBlockIds as blockId, bIndex (`${blockId}:${bIndex}`)}
                          <li
                            class="px-2.5 flex items-center justify-between gap-2"
                          >
                            <span
                              class="truncate text-type-3xs text-surface-sidebar-text-muted font-mono"
                              >{blockId.slice(0, 8)}…</span
                            >
                            <button
                              type="button"
                              class="flex-shrink-0 rounded border border-surface-sidebar-border bg-transparent px-2 py-0.5 text-type-3xs text-accent-primary-start hover:bg-accent-primary-start/10 cursor-pointer transition-colors disabled:cursor-wait disabled:opacity-65 focus-visible:ring-2 focus-visible:ring-accent-primary-start"
                              disabled={unlinkedBusyBlockId === blockId}
                              aria-label={`Link mention of ${mention.title} in block ${blockId.slice(0, 8)} on page ${mention.sourcePage}`}
                              aria-busy={unlinkedBusyBlockId === blockId}
                              onclick={() => promoteMention(mention, blockId)}
                            >
                              {unlinkedBusyBlockId === blockId
                                ? 'Linking…'
                                : 'Link'}
                            </button>
                          </li>
                        {/each}
                      </ul>
                    {/if}
                  </li>
                {/each}
              </ul>
            </div>
          {/if}
        </section>
      {/if}
    {/if}
  </div>
</nav>
