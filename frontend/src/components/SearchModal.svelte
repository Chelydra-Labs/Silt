<script module lang="ts">
  export interface SearchModalResult {
    id: string
    source: string
    notebook: string
    section: string
    page: string
    file_date: string
    clean_content: string
    status?: string
    owner?: string
    due_date?: string
    priority?: number
    snippet?: string
  }
</script>

<script lang="ts">
  import { onMount } from 'svelte'
  import {
    SearchBlocksPaged,
    ListNavigation,
    QueryTagHierarchy,
    DistinctOwners
  } from '../../bindings/silt/app.js'
  import { STANDALONE_TASKS_NOTEBOOK } from '../lib/standaloneTasksNav'

  interface SearchResult {
    results: SearchModalResult[]
    total: number
    offset: number
    limit: number
    has_more: boolean
  }

  interface Props {
    onClose: () => void
    onJump: (res: SearchModalResult) => void
    onReplaceInVault?: (query: string) => void
  }

  let { onClose, onJump, onReplaceInVault }: Props = $props()

  let query = $state('')
  let results = $state<SearchModalResult[]>([])
  let selectedIdx = $state(0)
  let inputEl = $state<HTMLInputElement | null>(null)
  let listEl = $state<HTMLDivElement | null>(null)
  let dialogEl = $state<HTMLDivElement | null>(null)
  let previouslyFocused: HTMLElement | null = null
  let loading = $state(false)
  let total = $state(0)
  let hasMore = $state(false)
  let offset = $state(0)
  const pageSize = 20

  // Filters: scope (vault vs incl-linked), category (block type), sort,
  // notebook, and tag (#186 / #655). Empty notebook/tag = no filter.
  let scopeVaultOnly = $state(false)
  let typeFilter = $state('')
  let sortMode = $state<'relevance' | 'recency'>('relevance')
  let notebookFilter = $state('')
  let tagFilter = $state('')
  let notebookOptions = $state<string[]>([])
  let tagOptions = $state<string[]>([])
  let ownerOptions = $state<string[]>([])

  // Task-attribute filters (#1010). These ride SearchFilters' structured
  // predicates; setting any of them restricts results to matching TASK blocks
  // and suppresses page-title hits (a page row carries no task attributes).
  let ownerFilter = $state('')
  let statusFilter = $state('')
  let dueAfterFilter = $state('')
  let dueBeforeFilter = $state('')
  let priorityFloor = $state(0)
  let overdueOnly = $state(false)
  // Density: the seven attribute controls stay behind a disclosure so the
  // toolbar keeps single-row height at the dialog's 31.25rem cap.
  let taskFiltersOpen = $state(false)

  function taskFilterCount(): number {
    let n = 0
    if (statusFilter) n++
    if (ownerFilter.trim()) n++
    if (priorityFloor > 0) n++
    if (overdueOnly) n++
    if (dueAfterFilter || dueBeforeFilter) n++
    return n
  }

  const TYPE_CHIPS: { id: string; label: string }[] = [
    { id: '', label: 'All' },
    { id: 'TASK', label: 'Tasks' },
    { id: 'NOTE', label: 'Notes' },
    { id: 'HEADER', label: 'Headings' }
  ]

  // priorityMin is an urgency floor: N selects priority N and everything MORE
  // urgent (lower number). 0 = no floor.
  const PRIORITY_FLOORS: { value: number; label: string; title: string }[] = [
    { value: 0, label: 'Any', title: 'Any priority' },
    { value: 1, label: 'P1+', title: 'Critical only (priority 1)' },
    { value: 2, label: 'P2+', title: 'Critical or normal (priority 1–2)' },
    { value: 3, label: 'P3+', title: 'Any recorded priority (1–3)' }
  ]

  const FOCUSABLE =
    'a[href], button:not([disabled]):not([tabindex="-1"]), input:not([disabled]):not([tabindex="-1"]), select:not([disabled]):not([tabindex="-1"]), textarea:not([disabled]):not([tabindex="-1"]), [tabindex]:not([tabindex="-1"])'

  function focusableEls(): HTMLElement[] {
    if (!dialogEl) return []
    return Array.from(dialogEl.querySelectorAll<HTMLElement>(FOCUSABLE))
  }

  function hasTaskAttributeFilters(): boolean {
    return (
      ownerFilter.trim() !== '' ||
      statusFilter !== '' ||
      dueAfterFilter !== '' ||
      dueBeforeFilter !== '' ||
      priorityFloor > 0 ||
      overdueOnly
    )
  }

  /** True when a page row could never satisfy the active filters. */
  function hidesPageTitles(): boolean {
    return typeFilter !== '' || tagFilter !== '' || hasTaskAttributeFilters()
  }

  function hasActiveFilters(): boolean {
    return (
      scopeVaultOnly ||
      typeFilter !== '' ||
      sortMode !== 'relevance' ||
      notebookFilter !== '' ||
      tagFilter !== '' ||
      hasTaskAttributeFilters()
    )
  }

  function clearActiveFilters(): void {
    scopeVaultOnly = false
    typeFilter = ''
    sortMode = 'relevance'
    notebookFilter = ''
    tagFilter = ''
    ownerFilter = ''
    statusFilter = ''
    dueAfterFilter = ''
    dueBeforeFilter = ''
    priorityFloor = 0
    overdueOnly = false
  }

  function priorityFloorLabel(): string {
    return (
      PRIORITY_FLOORS.find((p) => p.value === priorityFloor)?.label ?? 'Any'
    )
  }

  // One signature over every input the request carries: a response whose
  // signature no longer matches current state is stale and must be dropped.
  function filterSig(q: string): string {
    return [
      q,
      typeFilter,
      sortMode,
      scopeVaultOnly,
      notebookFilter,
      tagFilter,
      ownerFilter,
      statusFilter,
      dueAfterFilter,
      dueBeforeFilter,
      priorityFloor,
      overdueOnly
    ].join('|')
  }

  // Re-run search whenever the query OR a filter changes (debounced). Resets to
  // the first page so the modal always shows the top-ranked matches for the
  // current query + filters.
  $effect(() => {
    const trimmed = query.trim()
    // Track the reactive filters so the effect re-runs on each change.
    void typeFilter
    void sortMode
    void scopeVaultOnly
    void notebookFilter
    void tagFilter
    void ownerFilter
    void statusFilter
    void dueAfterFilter
    void dueBeforeFilter
    void priorityFloor
    void overdueOnly
    if (!trimmed) {
      results = []
      selectedIdx = 0
      total = 0
      hasMore = false
      offset = 0
      loading = false
      return
    }

    const timeout = window.setTimeout(() => {
      offset = 0
      void performSearch(trimmed, 0, /*replace=*/ true)
    }, 175)

    return () => window.clearTimeout(timeout)
  })

  async function performSearch(q: string, off: number, replace: boolean) {
    // Capture the full filter state at call time so a stale response (from a
    // different query OR a toggled chip/filter) is detected on resolution.
    const sig = filterSig(q)
    loading = true
    try {
      const res: SearchResult = await SearchBlocksPaged(q, off, pageSize, {
        notebook: notebookFilter,
        section: '',
        tag: tagFilter,
        type: typeFilter,
        sort: sortMode,
        vaultOnly: scopeVaultOnly,
        owner: ownerFilter.trim(),
        status: statusFilter,
        dueBefore: dueBeforeFilter,
        dueAfter: dueAfterFilter,
        priorityMin: priorityFloor,
        overdue: overdueOnly,
        excludePageTitles: false
      })
      // Guard against a stale response landing after a newer query/filter.
      if (sig !== filterSig(query.trim())) return
      if (replace) {
        results = res.results || []
        selectedIdx = 0
      } else {
        results = [...results, ...(res.results || [])]
      }
      total = res.total
      hasMore = res.has_more
      offset = off
    } catch (e) {
      console.error('Search query failed:', e)
    } finally {
      if (query.trim() === q) loading = false
    }
  }

  function flattenTags(
    nodes: { path?: string; children?: unknown[] }[],
    out: string[] = []
  ): string[] {
    for (const n of nodes) {
      if (n.path) out.push(n.path)
      if (Array.isArray(n.children) && n.children.length > 0) {
        flattenTags(
          n.children as { path?: string; children?: unknown[] }[],
          out
        )
      }
    }
    return out
  }

  async function loadFilterOptions(): Promise<void> {
    try {
      const tree = await ListNavigation()
      notebookOptions = (tree?.notebooks ?? [])
        .map((n: { name: string }) => n.name)
        .filter((n: string) => n && n !== STANDALONE_TASKS_NOTEBOOK)
        .sort((a: string, b: string) => a.localeCompare(b))
    } catch (e) {
      console.error('SearchModal: ListNavigation failed:', e)
      notebookOptions = []
    }
    try {
      const tags = (await QueryTagHierarchy()) || []
      tagOptions = flattenTags(tags).sort((a, b) => a.localeCompare(b))
    } catch (e) {
      console.error('SearchModal: QueryTagHierarchy failed:', e)
      tagOptions = []
    }
    try {
      const owners = (await DistinctOwners('')) || []
      ownerOptions = owners
        .filter((o: string) => o.trim() !== '')
        .sort((a: string, b: string) => a.localeCompare(b))
    } catch (e) {
      console.error('SearchModal: DistinctOwners failed:', e)
      ownerOptions = []
    }
  }

  function loadMore() {
    if (loading || !hasMore) return
    void performSearch(query.trim(), offset + pageSize, false)
  }

  function handleListScroll() {
    if (!listEl || loading || !hasMore) return
    const { scrollTop, scrollHeight, clientHeight } = listEl
    // Trigger the next page when the user scrolls within ~120px of the bottom.
    if (scrollHeight - scrollTop - clientHeight < 120) {
      loadMore()
    }
  }

  function isTypeChipFocused(): boolean {
    const active = document.activeElement as HTMLElement | null
    return !!active?.closest('[data-type-chips]')
  }

  function cycleTypeChip(delta: number): void {
    const currentIdx = TYPE_CHIPS.findIndex((c) => c.id === typeFilter)
    const nextIdx = (currentIdx + delta + TYPE_CHIPS.length) % TYPE_CHIPS.length
    typeFilter = TYPE_CHIPS[nextIdx].id
    // Keep focus on the newly selected chip so arrow keys continue to work.
    queueMicrotask(() => {
      const chip = document.querySelector<HTMLElement>(
        `[data-type-chips] [data-type-chip="${TYPE_CHIPS[nextIdx].id}"]`
      )
      chip?.focus()
    })
  }

  /** True when focus is on notebook/tag (or other non-query) form controls so
   *  capture-phase result navigation does not steal select/input keys. */
  function isSecondaryFilterFocused(): boolean {
    const active = document.activeElement as HTMLElement | null
    if (!active || active === inputEl) return false
    const tag = active.tagName
    return tag === 'SELECT' || tag === 'INPUT' || tag === 'TEXTAREA'
  }

  function handleKeyDown(e: KeyboardEvent) {
    if (e.key === 'Tab' && dialogEl) {
      const els = focusableEls()
      if (els.length === 0) return
      const first = els[0]
      const last = els[els.length - 1]
      const active = document.activeElement as HTMLElement | null
      if (e.shiftKey) {
        if (active === first || !dialogEl.contains(active)) {
          e.preventDefault()
          last.focus()
        }
      } else if (active === last || !dialogEl.contains(active)) {
        e.preventDefault()
        first.focus()
      }
      return
    }
    // Cycle type chips with ArrowLeft/Right only when focus is already on the
    // chip group.
    if (
      (e.key === 'ArrowLeft' || e.key === 'ArrowRight') &&
      isTypeChipFocused()
    ) {
      e.preventDefault()
      cycleTypeChip(e.key === 'ArrowRight' ? 1 : -1)
      return
    }
    // Escape always closes; other nav keys leave form controls alone.
    if (e.key === 'Escape') {
      e.preventDefault()
      onClose()
      return
    }
    if (isSecondaryFilterFocused()) return
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      if (results.length > 0) {
        selectedIdx = (selectedIdx + 1) % results.length
        scrollSelectedIntoView()
      }
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      if (results.length > 0) {
        selectedIdx = (selectedIdx - 1 + results.length) % results.length
        scrollSelectedIntoView()
      }
    } else if (e.key === 'Enter') {
      e.preventDefault()
      if (results[selectedIdx]) {
        selectResult(results[selectedIdx])
      }
    }
  }

  function scrollSelectedIntoView() {
    // Defer until after the selectedIdx class flips the DOM.
    queueMicrotask(() => {
      if (!listEl) return
      const el = listEl.querySelector(`[data-idx="${selectedIdx}"]`)
      el?.scrollIntoView({ block: 'nearest' })
    })
  }

  function selectResult(res: SearchModalResult) {
    onJump(res)
    onClose()
  }

  function sourceLabel(source: string): 'Linked' | 'Vault' {
    return source.startsWith('linked:') ? 'Linked' : 'Vault'
  }

  /** Page-level results use the empty block id discriminator (see ARCHITECTURE). */
  function isPageResult(res: SearchModalResult): boolean {
    return res.id === ''
  }

  // sanitizeSnippet HTML-escapes the FTS5 snippet, then restores ONLY the
  // <mark>/</mark> highlight tags the snippet() function emits. This keeps
  // user-authored note text from injecting arbitrary HTML into the modal
  // while still rendering the relevance highlight.
  //
  // CSP context (#237, F2): the host-webview CSP does NOT enable
  // `require-trusted-types-for 'script'`, so this @html sink works without
  // a Trusted Types policy. If a future tightening enables Trusted Types,
  // wrap the returned string in a `policy.createHTML(...)` call (the
  // Svelte 5 compiler translates @html to element.innerHTML).
  function sanitizeSnippet(snip: string): string {
    if (!snip) return ''
    const esc = snip
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
    return esc.replace(/&lt;\/?mark&gt;/g, (m) =>
      m.includes('/') ? '</mark>' : '<mark>'
    )
  }

  onMount(() => {
    previouslyFocused = document.activeElement as HTMLElement | null
    if (inputEl) {
      inputEl.focus()
    }
    void loadFilterOptions()

    window.addEventListener('keydown', handleKeyDown, true)
    return () => {
      window.removeEventListener('keydown', handleKeyDown, true)
      if (previouslyFocused?.isConnected) {
        previouslyFocused.focus()
      }
    }
  })
</script>

<!-- Positioning wrapper: scrim + dialog as siblings (standard modal layout) -->
<div
  class="fixed inset-0 bg-black/45 backdrop-blur-[3px] z-[150] flex items-start justify-center pt-28"
>
  <button
    type="button"
    tabindex="-1"
    aria-label="Close search"
    onclick={onClose}
    class="absolute inset-0 cursor-default border-none p-0 bg-transparent"
  ></button>
  <!-- Modal Frame (Frosted Glass Panel) -->
  <div
    bind:this={dialogEl}
    role="dialog"
    aria-modal="true"
    aria-label="Search blocks"
    tabindex="-1"
    class="relative w-full max-w-2xl glass-palette glass-palette-strong border border-surface-modal-border rounded-xl shadow-2xl overflow-hidden flex flex-col max-h-[31.25rem]"
  >
    <!-- Search Input Area -->
    <div
      class="flex items-center gap-3 px-4 py-4 border-b border-surface-modal-border bg-surface-modal/30"
    >
      <span
        class="material-symbols-outlined text-text-muted text-icon-xl select-none"
        >search</span
      >
      <input
        bind:this={inputEl}
        bind:value={query}
        type="text"
        placeholder="Search notebooks, sections, or task content..."
        class="bg-transparent border-none outline-none text-text-primary text-type-lg font-body-md w-full focus:ring-0 placeholder:text-text-muted"
      />
      {#if query}
        <button
          type="button"
          aria-label="Clear search"
          onclick={() => {
            query = ''
            inputEl?.focus()
          }}
          class="p-1 rounded hover:bg-hover text-text-muted hover:text-text-primary border-none bg-transparent cursor-pointer flex items-center justify-center focus:outline-none flex-shrink-0"
        >
          <span class="material-symbols-outlined text-icon-lg">close</span>
        </button>
      {/if}
      {#if loading}
        <span
          class="material-symbols-outlined text-accent-primary-start animate-spin text-type-2xl select-none flex-shrink-0"
        >
          sync
        </span>
      {/if}
    </div>

    <!-- Filter controls (#186): scope (vault vs incl-linked) + category chips +
         sort. Keyboard-reachable via Tab; type chips also cycle with ←/→ when
         focused. Chips drive the `type` filter, scope drives VaultOnly, sort
         drives the SQL ORDER BY. -->
    <div
      class="flex items-center gap-2 px-4 py-2 border-b border-surface-modal-border bg-surface-modal/20 flex-wrap"
      role="toolbar"
      aria-label="Search filters"
    >
      <!-- Scope: Vault vs Vault+Linked (segmented). -->
      <div
        class="flex rounded-lg overflow-hidden border border-surface-modal-border"
      >
        <button
          type="button"
          class="px-2.5 py-1 text-type-xs font-label-sm-bold transition-colors border-none cursor-pointer"
          class:bg-accent-primary-start={scopeVaultOnly}
          class:text-text-on-accent={scopeVaultOnly}
          class:text-text-muted={!scopeVaultOnly}
          aria-pressed={scopeVaultOnly}
          onclick={() => (scopeVaultOnly = true)}
          title="Search in-vault blocks only">Vault</button
        >
        <button
          type="button"
          class="px-2.5 py-1 text-type-xs font-label-sm-bold transition-colors border-none cursor-pointer"
          class:bg-accent-primary-start={!scopeVaultOnly}
          class:text-text-on-accent={!scopeVaultOnly}
          class:text-text-muted={scopeVaultOnly}
          aria-pressed={!scopeVaultOnly}
          onclick={() => (scopeVaultOnly = false)}
          title="Include linked notebooks">+ Linked</button
        >
      </div>

      <!-- Category chips: filter by block type (single-select; "" = All). -->
      <div class="flex items-center gap-1 flex-wrap" data-type-chips>
        {#each TYPE_CHIPS as chip (chip.id)}
          <button
            type="button"
            data-type-chip={chip.id}
            class="px-2 py-1 rounded-md text-type-xs font-label-sm-bold transition-colors border cursor-pointer"
            class:bg-accent-primary-start={typeFilter === chip.id}
            class:text-text-on-accent={typeFilter === chip.id}
            class:bg-transparent={typeFilter !== chip.id}
            class:text-text-muted={typeFilter !== chip.id}
            class:border-surface-modal-border={typeFilter !== chip.id}
            aria-pressed={typeFilter === chip.id}
            onclick={() => (typeFilter = chip.id)}>{chip.label}</button
          >
        {/each}
      </div>

      <!-- Notebook filter (#655). -->
      <label class="flex items-center gap-1 text-type-xs text-text-muted">
        <span class="sr-only">Notebook</span>
        <select
          aria-label="Filter by notebook"
          bind:value={notebookFilter}
          class="max-w-[9rem] rounded-md border border-surface-modal-border bg-surface-modal px-1.5 py-1 text-type-xs text-text-primary cursor-pointer"
        >
          <option value="">All notebooks</option>
          {#each notebookOptions as nb (nb)}
            <option value={nb}>{nb}</option>
          {/each}
        </select>
      </label>

      <!-- Tag filter (#655): free text + datalist from indexed hierarchy. -->
      <label class="flex items-center gap-1 text-type-xs text-text-muted">
        <span class="sr-only">Tag</span>
        <input
          type="text"
          list="search-tag-options"
          aria-label="Filter by tag"
          placeholder="Tag…"
          bind:value={tagFilter}
          class="w-28 rounded-md border border-surface-modal-border bg-surface-modal px-1.5 py-1 text-type-xs text-text-primary placeholder:text-text-muted"
        />
        <datalist id="search-tag-options">
          {#each tagOptions as tag (tag)}
            <option value={tag}></option>
          {/each}
        </datalist>
      </label>

      <span
        class="hidden h-4 w-px bg-surface-modal-border sm:block"
        aria-hidden="true"
      ></span>

      <!-- Task-attribute filters (#1010): these map to SearchFilters'
           structured predicates. Any active here restricts results to task
           blocks and hides page-title hits (see the hint below). Grouped
           behind a disclosure so the toolbar keeps one row at the dialog's
           fixed height; query + type chips + sort stay visible. -->
      <button
        type="button"
        data-testid="search-task-filters-toggle"
        aria-expanded={taskFiltersOpen}
        aria-controls="search-task-filters"
        onclick={() => (taskFiltersOpen = !taskFiltersOpen)}
        class="flex items-center gap-1 rounded-md border px-2 py-1 text-type-xs font-label-sm-bold transition-colors cursor-pointer"
        class:border-accent-primary-start={taskFiltersOpen ||
          taskFilterCount() > 0}
        class:bg-accent-primary-start={taskFiltersOpen}
        class:text-text-on-accent={taskFiltersOpen}
        class:border-surface-modal-border={!taskFiltersOpen &&
          taskFilterCount() === 0}
        class:text-text-muted={!taskFiltersOpen}
      >
        <span class="material-symbols-outlined text-type-xs" aria-hidden="true"
          >filter_alt</span
        >
        <span>Task filters</span>
        {#if taskFilterCount() > 0}
          <span
            data-testid="search-task-filters-count"
            class="rounded-full px-1 text-type-3xs {taskFiltersOpen
              ? 'bg-text-on-accent/20'
              : 'bg-accent-primary-start/20'}">{taskFilterCount()}</span
          >
        {/if}
        <span class="material-symbols-outlined text-type-xs" aria-hidden="true"
          >{taskFiltersOpen ? 'expand_less' : 'expand_more'}</span
        >
      </button>

      <!-- Sort: Relevance vs Recent (segmented). -->
      <div
        class="ml-auto flex rounded-lg overflow-hidden border border-surface-modal-border"
      >
        <button
          type="button"
          class="px-2.5 py-1 text-type-xs font-label-sm-bold transition-colors border-none cursor-pointer"
          class:bg-accent-primary-start={sortMode === 'relevance'}
          class:text-text-on-accent={sortMode === 'relevance'}
          class:text-text-muted={sortMode !== 'relevance'}
          aria-pressed={sortMode === 'relevance'}
          onclick={() => (sortMode = 'relevance')}>Relevance</button
        >
        <button
          type="button"
          class="px-2.5 py-1 text-type-xs font-label-sm-bold transition-colors border-none cursor-pointer"
          class:bg-accent-primary-start={sortMode === 'recency'}
          class:text-text-on-accent={sortMode === 'recency'}
          class:text-text-muted={sortMode !== 'recency'}
          aria-pressed={sortMode === 'recency'}
          onclick={() => (sortMode = 'recency')}>Recent</button
        >
      </div>
    </div>

    {#if taskFiltersOpen}
      <div
        id="search-task-filters"
        class="flex items-center gap-2 px-4 py-2 border-b border-surface-modal-border bg-surface-modal/10 flex-wrap"
        role="group"
        aria-label="Task filters"
      >
        <label class="flex items-center gap-1 text-type-xs text-text-muted">
          <span class="sr-only">Task status</span>
          <select
            aria-label="Filter by task status"
            bind:value={statusFilter}
            class="max-w-[7.5rem] rounded-md border border-surface-modal-border bg-surface-modal px-1.5 py-1 text-type-xs text-text-primary cursor-pointer"
          >
            <option value="">Any status</option>
            <option value="TODO">To do</option>
            <option value="DOING">Doing</option>
            <option value="DONE">Done</option>
          </select>
        </label>

        <label class="flex items-center gap-1 text-type-xs text-text-muted">
          <span class="sr-only">Owner</span>
          <input
            type="text"
            list="search-owner-options"
            aria-label="Filter by owner"
            placeholder="Owner…"
            bind:value={ownerFilter}
            class="w-24 rounded-md border border-surface-modal-border bg-surface-modal px-1.5 py-1 text-type-xs text-text-primary placeholder:text-text-muted"
          />
          <datalist id="search-owner-options">
            {#each ownerOptions as owner (owner)}
              <option value={owner}></option>
            {/each}
          </datalist>
        </label>

        <div
          class="flex items-center gap-1"
          role="group"
          aria-label="Filter by priority urgency"
        >
          {#each PRIORITY_FLOORS as p (p.value)}
            <button
              type="button"
              title={p.title}
              aria-pressed={priorityFloor === p.value}
              onclick={() => (priorityFloor = p.value)}
              class="px-2 py-1 rounded-md text-type-xs font-label-sm-bold transition-colors border cursor-pointer"
              class:bg-accent-primary-start={priorityFloor === p.value}
              class:text-text-on-accent={priorityFloor === p.value}
              class:bg-transparent={priorityFloor !== p.value}
              class:text-text-muted={priorityFloor !== p.value}
              class:border-surface-modal-border={priorityFloor !== p.value}
            >
              {p.label}
            </button>
          {/each}
        </div>

        <button
          type="button"
          data-testid="search-overdue-toggle"
          aria-pressed={overdueOnly}
          title="Tasks whose due date is before today and are not done"
          onclick={() => (overdueOnly = !overdueOnly)}
          class="px-2 py-1 rounded-md text-type-xs font-label-sm-bold transition-colors border cursor-pointer"
          class:bg-accent-primary-start={overdueOnly}
          class:text-text-on-accent={overdueOnly}
          class:bg-transparent={!overdueOnly}
          class:text-text-muted={!overdueOnly}
          class:border-surface-modal-border={!overdueOnly}
        >
          Overdue
        </button>

        <label class="flex items-center gap-1 text-type-xs text-text-muted">
          <span class="sr-only">Due on or after</span>
          <input
            type="date"
            aria-label="Due on or after"
            bind:value={dueAfterFilter}
            class="w-[8.5rem] rounded-md border border-surface-modal-border bg-surface-modal px-1.5 py-1 text-type-xs text-text-primary"
          />
        </label>
        <label class="flex items-center gap-1 text-type-xs text-text-muted">
          <span class="sr-only">Due on or before</span>
          <input
            type="date"
            aria-label="Due on or before"
            bind:value={dueBeforeFilter}
            class="w-[8.5rem] rounded-md border border-surface-modal-border bg-surface-modal px-1.5 py-1 text-type-xs text-text-primary"
          />
        </label>
      </div>
    {/if}

    {#if hidesPageTitles()}
      <!-- Honest surfacing of the backend contract: the page-title leg is only
           merged when no type/tag/task-attribute filter is active, and task
           filters additionally join the task projection (dropping notes and
           headings). -->
      <p
        role="status"
        data-testid="search-title-leg-hint"
        class="px-4 py-1 text-type-2xs text-text-muted border-b border-surface-modal-border bg-surface-modal/10"
      >
        Page-title matches are hidden while a type, tag, or task filter is
        active. Task filters also restrict results to matching task rows — notes
        and headings are hidden.
      </p>
    {/if}

    <!-- Active notebook/tag/task filter chips (individually clearable). -->
    {#if notebookFilter || tagFilter || hasTaskAttributeFilters()}
      <div
        class="flex items-center gap-2 px-4 py-1.5 border-b border-surface-modal-border flex-wrap"
        role="group"
        aria-label="Active filters"
      >
        {#if notebookFilter}
          <button
            type="button"
            class="inline-flex items-center gap-1 rounded-full border border-accent-primary-start/40 bg-accent-primary-start/10 px-2 py-0.5 text-type-xs text-accent-primary-start cursor-pointer"
            aria-label="Clear notebook filter"
            onclick={() => (notebookFilter = '')}
          >
            <span
              class="material-symbols-outlined text-icon-xs"
              aria-hidden="true">menu_book</span
            >
            {notebookFilter}
            <span
              class="material-symbols-outlined text-icon-xs"
              aria-hidden="true">close</span
            >
          </button>
        {/if}
        {#if tagFilter}
          <button
            type="button"
            class="inline-flex items-center gap-1 rounded-full border border-accent-primary-start/40 bg-accent-primary-start/10 px-2 py-0.5 text-type-xs text-accent-primary-start cursor-pointer"
            aria-label="Clear tag filter"
            onclick={() => (tagFilter = '')}
          >
            <span
              class="material-symbols-outlined text-icon-xs"
              aria-hidden="true">sell</span
            >
            {tagFilter}
            <span
              class="material-symbols-outlined text-icon-xs"
              aria-hidden="true">close</span
            >
          </button>
        {/if}
        {#if ownerFilter.trim()}
          <button
            type="button"
            class="inline-flex items-center gap-1 rounded-full border border-accent-primary-start/40 bg-accent-primary-start/10 px-2 py-0.5 text-type-xs text-accent-primary-start cursor-pointer"
            aria-label="Clear owner filter"
            onclick={() => (ownerFilter = '')}
          >
            <span
              class="material-symbols-outlined text-icon-xs"
              aria-hidden="true">person</span
            >
            Owner: {ownerFilter.trim()}
            <span
              class="material-symbols-outlined text-icon-xs"
              aria-hidden="true">close</span
            >
          </button>
        {/if}
        {#if statusFilter}
          <button
            type="button"
            class="inline-flex items-center gap-1 rounded-full border border-accent-primary-start/40 bg-accent-primary-start/10 px-2 py-0.5 text-type-xs text-accent-primary-start cursor-pointer"
            aria-label="Clear status filter"
            onclick={() => (statusFilter = '')}
          >
            <span
              class="material-symbols-outlined text-icon-xs"
              aria-hidden="true">check_circle</span
            >
            Status: {statusFilter}
            <span
              class="material-symbols-outlined text-icon-xs"
              aria-hidden="true">close</span
            >
          </button>
        {/if}
        {#if priorityFloor > 0}
          <button
            type="button"
            class="inline-flex items-center gap-1 rounded-full border border-accent-primary-start/40 bg-accent-primary-start/10 px-2 py-0.5 text-type-xs text-accent-primary-start cursor-pointer"
            aria-label="Clear priority filter"
            onclick={() => (priorityFloor = 0)}
          >
            <span
              class="material-symbols-outlined text-icon-xs"
              aria-hidden="true">flag</span
            >
            Priority: {priorityFloorLabel()}
            <span
              class="material-symbols-outlined text-icon-xs"
              aria-hidden="true">close</span
            >
          </button>
        {/if}
        {#if overdueOnly}
          <button
            type="button"
            class="inline-flex items-center gap-1 rounded-full border border-accent-primary-start/40 bg-accent-primary-start/10 px-2 py-0.5 text-type-xs text-accent-primary-start cursor-pointer"
            aria-label="Clear overdue filter"
            onclick={() => (overdueOnly = false)}
          >
            <span
              class="material-symbols-outlined text-icon-xs"
              aria-hidden="true">schedule</span
            >
            Overdue
            <span
              class="material-symbols-outlined text-icon-xs"
              aria-hidden="true">close</span
            >
          </button>
        {/if}
        {#if dueAfterFilter || dueBeforeFilter}
          <button
            type="button"
            class="inline-flex items-center gap-1 rounded-full border border-accent-primary-start/40 bg-accent-primary-start/10 px-2 py-0.5 text-type-xs text-accent-primary-start cursor-pointer"
            aria-label="Clear due date range filter"
            onclick={() => {
              dueAfterFilter = ''
              dueBeforeFilter = ''
            }}
          >
            <span
              class="material-symbols-outlined text-icon-xs"
              aria-hidden="true">event</span
            >
            Due: {dueAfterFilter || '…'} → {dueBeforeFilter || '…'}
            <span
              class="material-symbols-outlined text-icon-xs"
              aria-hidden="true">close</span
            >
          </button>
        {/if}
      </div>
    {/if}

    {#if query.trim() && total > 0}
      <div
        class="px-5 py-1 text-type-xs text-text-muted font-body-md flex items-center justify-between"
      >
        <span>{total} {total === 1 ? 'result' : 'results'}</span>
        {#if onReplaceInVault}
          <button
            type="button"
            class="text-accent-primary-start hover:brightness-110 cursor-pointer text-type-xs font-label-sm-bold border-none bg-transparent"
            onclick={() => onReplaceInVault(query.trim())}
            >Replace in vault…</button
          >
        {/if}
      </div>
    {/if}

    <!-- Search Results List -->
    <div
      bind:this={listEl}
      onscroll={handleListScroll}
      class="flex-1 overflow-y-auto custom-scrollbar py-2"
    >
      {#if query.trim() === ''}
        <div class="text-text-muted text-center py-10 font-body-md select-none">
          Type queries to find headers, notes, or checklist items...
        </div>
      {:else if results.length === 0 && !loading}
        <div
          class="text-text-muted text-center py-10 font-body-md select-none flex flex-col items-center gap-2"
        >
          <span>No matches found for "{query}"</span>
          {#if hasActiveFilters()}
            <button
              type="button"
              class="text-type-xs text-accent-primary-start hover:underline border-none bg-transparent cursor-pointer"
              onclick={clearActiveFilters}
            >
              Clear active filters
            </button>
          {/if}
        </div>
      {:else}
        {#each results as res, idx (res.id + idx)}
          <button
            data-idx={idx}
            onclick={() => selectResult(res)}
            class="w-full px-5 py-3 border-none flex flex-col gap-1 text-left cursor-pointer transition-colors focus:outline-none hover:bg-hover/50"
            class:bg-accent-primary-glow={idx === selectedIdx}
            class:border-l-2={idx === selectedIdx}
            class:border-accent-primary-start={idx === selectedIdx}
          >
            <!-- Breadcrumb metadata. Standalone (`.silt`) results omit the
                 path entirely — no synthetic notebook leak and no redundant
                 "Standalone task" label; the click still routes to Tasks. -->
            <div
              class="flex items-center gap-1.5 text-type-2xs text-text-muted uppercase tracking-widest font-label-sm-bold"
            >
              <span
                aria-label={`${sourceLabel(res.source)} source`}
                class="rounded-full border border-surface-modal-border bg-surface-modal px-1.5 py-0.5 text-type-3xs tracking-wider text-text-muted"
                >{sourceLabel(res.source)}</span
              >
              {#if res.notebook !== STANDALONE_TASKS_NOTEBOOK}
                <span>{res.notebook}</span>
                <span class="material-symbols-outlined text-type-2xs"
                  >chevron_right</span
                >
                <span>{res.section}</span>
                {#if !isPageResult(res)}
                  <span class="material-symbols-outlined text-type-2xs"
                    >chevron_right</span
                  >
                  <span>{res.page}</span>
                  <span class="material-symbols-outlined text-type-2xs"
                    >chevron_right</span
                  >
                {/if}
              {/if}
              <span class="text-accent-primary-start">{res.file_date}</span>
            </div>

            <!-- Content preview with FTS5 highlight snippet. Page rows carry the
                 page name in clean_content and a <mark> title in snippet; the
                 sanitizer allows only the highlight tags through. -->
            <div
              class="font-body-md text-sm text-text-primary flex items-center gap-2"
            >
              {#if isPageResult(res)}
                <span
                  class="material-symbols-outlined text-icon-md text-accent-primary-start select-none"
                  aria-hidden="true">description</span
                >
                <span
                  class="text-type-2xs uppercase tracking-widest text-text-muted font-label-sm-bold"
                  >Page</span
                >
              {/if}
              {#if res.snippet}
                <!-- Sanitized in-script: only <mark> tags from FTS5 survive. -->
                <!-- eslint-disable-next-line svelte/no-at-html-tags -- sanitizeSnippet/DOMPurify -->
                <span>{@html sanitizeSnippet(res.snippet)}</span>
              {:else}
                <span>{res.clean_content}</span>
              {/if}
            </div>

            {#if !isPageResult(res) && (res.status || res.owner || res.due_date || res.priority)}
              <!-- Task attributes projected by the backend for TASK rows. -->
              <div
                class="flex flex-wrap items-center gap-x-2 gap-y-1 text-type-2xs text-text-muted"
                data-testid="search-result-task-meta"
              >
                {#if res.status}
                  <span
                    class="inline-flex items-center gap-1 rounded border border-surface-modal-border bg-surface-modal px-1 py-0.5"
                  >
                    <span
                      class="material-symbols-outlined text-icon-xs select-none"
                      aria-hidden="true"
                    >
                      {res.status === 'DONE'
                        ? 'check_circle'
                        : 'radio_button_unchecked'}
                    </span>
                    {res.status}
                  </span>
                {/if}
                {#if res.owner}
                  <span class="inline-flex items-center gap-1">
                    <span
                      class="material-symbols-outlined text-icon-xs select-none"
                      aria-hidden="true">person</span
                    >
                    {res.owner}
                  </span>
                {/if}
                {#if res.due_date}
                  <span class="inline-flex items-center gap-1">
                    <span
                      class="material-symbols-outlined text-icon-xs select-none"
                      aria-hidden="true">schedule</span
                    >
                    {res.due_date}
                  </span>
                {/if}
                {#if res.priority}
                  <span
                    class="inline-flex items-center gap-1"
                    title={`Priority ${res.priority}`}
                  >
                    <span
                      class="material-symbols-outlined text-icon-xs select-none"
                      aria-hidden="true">flag</span
                    >
                    P{res.priority}
                  </span>
                {/if}
              </div>
            {/if}
          </button>
        {/each}

        {#if hasMore}
          <div
            class="text-text-muted text-center py-3 text-type-xs font-body-md select-none"
          >
            {loading ? 'Loading more…' : 'Scroll for more results'}
          </div>
        {/if}
      {/if}
    </div>

    <!-- Result count footer -->
    {#if query.trim() !== '' && total > 0}
      <div
        class="px-4 py-2 border-t border-surface-modal-border text-type-2xs text-text-muted font-label-sm flex items-center justify-between bg-surface-modal/30"
      >
        <span>{total} match{total === 1 ? '' : 'es'}</span>
        <span class="opacity-60">↑↓ navigate · ⏎ open · esc close</span>
      </div>
    {/if}
  </div>
</div>

<style>
  :global(mark) {
    background: color-mix(
      in srgb,
      var(--color-accent-primary-start) 30%,
      transparent
    );
    color: var(--color-text-primary);
    border-radius: 3px;
    padding: 0 2px;
  }
</style>
