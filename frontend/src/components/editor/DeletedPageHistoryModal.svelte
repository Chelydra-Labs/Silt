<script lang="ts">
  import { onMount, tick } from 'svelte'
  import { ListDeletedPageHistory } from '../../../bindings/silt/app.js'
  import { trapFocus } from '../../lib/focusTrap'
  import { asString } from '../../lib/asString'
  import PageHistoryModal from './PageHistoryModal.svelte'

  interface DeletedPageRow {
    notebook: string
    section: string
    page: string
    source: string
    versionCount: number
    latestTimestamp: string
    latestBytes: number
  }

  interface Props {
    onClose: () => void
  }

  let { onClose }: Props = $props()

  const dtf = new Intl.DateTimeFormat(undefined, {
    year: 'numeric',
    month: 'short',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit'
  })

  let dialogRef = $state<HTMLDivElement | null>(null)
  let listRef = $state<HTMLDivElement | null>(null)
  let searchRef = $state<HTMLInputElement | null>(null)
  let previouslyFocused: HTMLElement | null = null
  let rows = $state<DeletedPageRow[]>([])
  let query = $state('')
  let selectedKey = $state<string | null>(null)
  let listLoading = $state(true)
  let listError = $state('')
  let opened = $state<DeletedPageRow | null>(null)

  let filtered = $derived(filterRows(rows, query))
  let selected = $derived(
    filtered.find((r) => rowKey(r) === selectedKey) ?? null
  )
  let countLabel = $derived(
    listLoading
      ? 'Loading deleted pages…'
      : formatCount(rows.length, filtered.length, query)
  )

  function rowKey(row: DeletedPageRow): string {
    return `${row.notebook}::${row.section}::${row.page}::${row.source}`
  }

  function locatorPath(row: DeletedPageRow): string {
    return row.section
      ? `${row.notebook} / ${row.section} / ${row.page}`
      : `${row.notebook} / ${row.page}`
  }

  function sourceLabel(source: string): string {
    if (source === 'linked') return 'Linked'
    if (source === 'vault') return 'Vault'
    return source
  }

  function formatTimestamp(iso: string): string {
    if (!iso) return 'Unknown time'
    const d = new Date(iso)
    return Number.isNaN(d.getTime()) ? iso : dtf.format(d)
  }

  function formatVersionCount(n: number): string {
    return `${n} ${n === 1 ? 'version' : 'versions'}`
  }

  function filterRows(list: DeletedPageRow[], raw: string): DeletedPageRow[] {
    const q = raw.trim().toLowerCase()
    if (!q) return list
    return list.filter((row) => {
      const path = locatorPath(row).toLowerCase()
      return (
        row.notebook.toLowerCase().includes(q) ||
        row.section.toLowerCase().includes(q) ||
        row.page.toLowerCase().includes(q) ||
        path.includes(q)
      )
    })
  }

  function formatCount(total: number, visible: number, raw: string): string {
    if (total === 0) return 'No deleted pages with history.'
    if (visible === 0) return 'No matching deleted pages.'
    const noun = visible === 1 ? 'page' : 'pages'
    return raw.trim()
      ? `${visible} matching deleted ${noun}`
      : `${visible} deleted ${noun}`
  }

  function asRow(raw: Record<string, unknown>): DeletedPageRow {
    return {
      notebook: asString(raw.notebook),
      section: asString(raw.section),
      page: asString(raw.page),
      source: asString(raw.source),
      versionCount: Number(raw.versionCount ?? 0),
      latestTimestamp: asString(raw.latestTimestamp),
      latestBytes: Number(raw.latestBytes ?? 0)
    }
  }

  async function loadRows(): Promise<void> {
    listLoading = true
    listError = ''
    try {
      const result = ((await ListDeletedPageHistory()) ??
        []) as DeletedPageRow[]
      rows = result.map((r) => asRow(r as unknown as Record<string, unknown>))
      selectedKey =
        (selectedKey && rows.some((r) => rowKey(r) === selectedKey)
          ? selectedKey
          : null) ?? (rows[0] ? rowKey(rows[0]) : null)
      listLoading = false
      await tick()
      searchRef?.focus()
    } catch (e) {
      listError = e instanceof Error ? e.message : String(e)
      rows = []
      selectedKey = null
    } finally {
      listLoading = false
    }
  }

  function selectRow(key: string): void {
    selectedKey = key
    scrollSelectedIntoView()
  }

  function openSelected(): void {
    if (!selected) return
    opened = selected
  }

  function moveSelection(delta: number): void {
    if (filtered.length === 0) return
    const idx = filtered.findIndex((r) => rowKey(r) === selectedKey)
    const from = idx < 0 ? 0 : idx
    const next = Math.max(0, Math.min(filtered.length - 1, from + delta))
    selectRow(rowKey(filtered[next]))
  }

  function scrollSelectedIntoView(): void {
    queueMicrotask(() => {
      if (!listRef || !selectedKey) return
      const el = listRef.querySelector(
        `[data-deleted-key="${CSS.escape(selectedKey)}"]`
      )
      el?.scrollIntoView?.({ block: 'nearest' })
    })
  }

  function handleKeydown(e: KeyboardEvent): void {
    if (opened) return
    if (e.key === 'Escape') {
      e.preventDefault()
      e.stopPropagation()
      onClose()
      return
    }
    const inList = !!(e.target as HTMLElement | null)?.closest(
      '[data-testid="deleted-page-history-list"]'
    )
    if (!inList) return
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      moveSelection(1)
      return
    }
    if (e.key === 'ArrowUp') {
      e.preventDefault()
      moveSelection(-1)
      return
    }
    if (e.key === 'Home') {
      e.preventDefault()
      if (filtered[0]) selectRow(rowKey(filtered[0]))
      return
    }
    if (e.key === 'End') {
      e.preventDefault()
      const last = filtered[filtered.length - 1]
      if (last) selectRow(rowKey(last))
      return
    }
    if (e.key === 'Enter') {
      if (!selected) return
      e.preventDefault()
      openSelected()
    }
  }

  $effect(() => {
    const visible = filtered
    if (visible.length === 0) {
      selectedKey = null
      return
    }
    if (!visible.some((r) => rowKey(r) === selectedKey)) {
      selectedKey = rowKey(visible[0])
    }
  })

  onMount(() => {
    previouslyFocused = document.activeElement as HTMLElement | null
    window.addEventListener('keydown', handleKeydown, true)
    void tick().then(() => dialogRef?.focus())
    void loadRows()
    return () => {
      window.removeEventListener('keydown', handleKeydown, true)
      if (previouslyFocused?.isConnected) previouslyFocused.focus?.()
    }
  })

  $effect(() => {
    if (opened || !dialogRef) return
    const dispose = trapFocus(dialogRef)
    return () => dispose()
  })
</script>

{#if opened}
  <PageHistoryModal
    notebook={opened.notebook}
    section={opened.section}
    page={opened.page}
    deleted
    onBack={() => (opened = null)}
    {onClose}
  />
{:else}
  <div
    class="fixed inset-0 z-[180] flex items-center justify-center bg-black/45 backdrop-blur-[3px] px-4"
    data-focus-trap
  >
    <button
      type="button"
      tabindex="-1"
      aria-label="Close deleted page history"
      class="absolute inset-0 cursor-default border-none bg-transparent p-0"
      onclick={onClose}
    ></button>

    <div
      bind:this={dialogRef}
      role="dialog"
      aria-modal="true"
      aria-labelledby="deleted-page-history-title"
      aria-describedby="deleted-page-history-honesty"
      tabindex="-1"
      data-testid="deleted-page-history-modal"
      class="dialog-surface relative flex h-[min(32rem,calc(100vh-4rem))] w-full max-w-2xl flex-col overflow-hidden rounded-xl border border-surface-modal-border glass-palette glass-palette-strong shadow-2xl"
    >
      <header
        class="flex items-center gap-3 border-b border-surface-modal-border px-5 py-3.5"
      >
        <span
          class="material-symbols-outlined text-icon-xl text-accent-primary-start"
          aria-hidden="true">history</span
        >
        <div class="min-w-0 flex-1">
          <h2
            id="deleted-page-history-title"
            class="font-headline-md text-headline-md text-text-primary"
          >
            Deleted pages
          </h2>
          <p
            id="deleted-page-history-honesty"
            class="mt-1 text-type-2xs font-body-md text-text-muted"
          >
            Snapshots stay in this vault under .system/history. They sync with
            the vault, are not encrypted, and are not a backup.
          </p>
        </div>
        <button
          type="button"
          aria-label="Close deleted page history"
          onclick={onClose}
          class="flex h-8 w-8 items-center justify-center rounded-lg border-none bg-transparent text-text-muted transition-colors hover:bg-hover hover:text-text-primary"
        >
          <span
            class="material-symbols-outlined text-icon-lg"
            aria-hidden="true">close</span
          >
        </button>
      </header>

      <div class="search-bar">
        <span
          class="material-symbols-outlined text-icon-lg text-text-muted"
          aria-hidden="true">search</span
        >
        <input
          bind:this={searchRef}
          bind:value={query}
          type="search"
          aria-label="Search deleted pages"
          aria-controls="deleted-page-history-list"
          placeholder="Search notebook, section, or page"
          data-testid="deleted-page-history-search"
          class="search-input"
        />
      </div>

      <p
        class="count-line"
        role="status"
        aria-live="polite"
        data-testid="deleted-page-history-count"
      >
        {countLabel}
      </p>

      {#if listError}
        <div
          class="mx-5 mb-3 flex items-start gap-2 rounded-lg border border-error-border bg-error-bg px-3 py-2 text-type-sm font-body-md text-error"
          role="alert"
        >
          <span
            class="material-symbols-outlined text-icon-lg"
            aria-hidden="true">error</span
          >
          <span class="flex-1">{listError}</span>
          <button
            type="button"
            class="rounded-md border border-error-border bg-transparent px-2 py-1 text-type-xs font-label-sm-bold text-error hover:brightness-110"
            onclick={() => void loadRows()}
          >
            Retry
          </button>
        </div>
      {/if}

      <div
        bind:this={listRef}
        id="deleted-page-history-list"
        class="deleted-list custom-scrollbar"
        role="listbox"
        aria-label="Deleted pages"
        aria-busy={listLoading || undefined}
        tabindex="0"
        aria-activedescendant={filtered.length > 0 && selectedKey
          ? `deleted-page-${selectedKey}`
          : undefined}
        data-testid="deleted-page-history-list"
      >
        {#if listLoading}
          <div
            class="flex items-center gap-2 px-4 py-6 text-type-sm font-body-md text-text-muted"
            role="status"
          >
            <span
              class="material-symbols-outlined animate-spin text-icon-lg text-accent-primary-start"
              aria-hidden="true">sync</span
            >
            Loading deleted pages…
          </div>
        {:else if filtered.length === 0}
          <div
            class="flex flex-col items-start gap-2 px-4 py-6"
            data-testid="deleted-page-history-empty"
          >
            <p class="text-type-md font-body-md text-text-primary">
              {rows.length === 0
                ? 'No deleted pages with history.'
                : 'No matching deleted pages.'}
            </p>
            <p class="text-type-sm font-body-md text-text-muted">
              {rows.length === 0
                ? 'Pages with leftover snapshots will appear here after they are deleted.'
                : 'Try a different notebook, section, or page name.'}
            </p>
          </div>
        {:else}
          {#each filtered as row (rowKey(row))}
            <button
              type="button"
              role="option"
              id={`deleted-page-${rowKey(row)}`}
              data-deleted-key={rowKey(row)}
              aria-selected={selectedKey === rowKey(row)}
              tabindex="-1"
              class="deleted-row"
              class:selected={selectedKey === rowKey(row)}
              onclick={() => {
                selectRow(rowKey(row))
                openSelected()
              }}
            >
              <span class="deleted-path">{locatorPath(row)}</span>
              <span class="deleted-meta">
                <span class="source-badge source-{row.source}">
                  {sourceLabel(row.source)}
                </span>
                <span>{formatTimestamp(row.latestTimestamp)}</span>
                <span>{formatVersionCount(row.versionCount)}</span>
              </span>
            </button>
          {/each}
        {/if}
      </div>
    </div>
  </div>
{/if}

<style>
  .dialog-surface:focus-visible {
    outline: 2px solid var(--color-border-focus);
    outline-offset: 2px;
  }

  .search-bar {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 12px 20px 8px;
  }

  .search-input {
    width: 100%;
    border: none;
    background: transparent;
    color: var(--color-text-primary);
    font-size: var(--text-type-lg);
    outline: none;
  }

  .search-input::placeholder {
    color: var(--color-text-muted);
  }

  .count-line {
    margin: 0 20px 8px;
    color: var(--color-text-muted);
    font-size: var(--text-type-xs);
  }

  .deleted-list {
    min-height: 0;
    flex: 1;
    overflow-y: auto;
    border-top: 1px solid var(--color-surface-modal-border);
    background: color-mix(in srgb, var(--color-surface-panel) 35%, transparent);
  }

  .deleted-row {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 4px;
    width: 100%;
    padding: 10px 16px;
    border: none;
    border-left: 2px solid transparent;
    background: transparent;
    color: inherit;
    text-align: left;
    cursor: pointer;
    transition:
      background 120ms ease,
      border-color 120ms ease;
  }

  .deleted-row:hover {
    background: var(--color-hover);
  }

  .deleted-row.selected {
    background: color-mix(
      in srgb,
      var(--color-accent-primary-start) 12%,
      transparent
    );
    border-left-color: var(--color-accent-primary-start);
  }

  .deleted-row:focus-visible {
    outline: 2px solid var(--color-accent-primary-start);
    outline-offset: -2px;
  }

  .deleted-path {
    font-size: var(--text-type-sm);
    font-weight: 600;
    color: var(--color-text-primary);
  }

  .deleted-meta {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 8px;
    color: var(--color-text-muted);
    font-size: var(--text-type-2xs);
  }

  .source-badge {
    display: inline-flex;
    align-items: center;
    padding: 1px 6px;
    border-radius: 999px;
    border: 1px solid var(--color-surface-panel-border);
    background: color-mix(in srgb, var(--color-surface-card) 70%, transparent);
    color: var(--color-text-muted);
    font-size: var(--text-type-2xs);
    font-weight: 700;
    letter-spacing: 0.04em;
    text-transform: uppercase;
  }

  .source-badge.source-linked {
    border-color: color-mix(
      in srgb,
      var(--color-accent-secondary-start) 45%,
      var(--color-surface-panel-border)
    );
    color: var(--color-accent-secondary-start);
  }
</style>
