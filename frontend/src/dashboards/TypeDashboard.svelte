<script lang="ts">
  // Per-type dashboard container. Owns: the type list (ListTypes), the selected
  // type, the query (QueryPagesByType) with a trailing-debounce on filter
  // typing, client-side group-by binning, and loading/error/empty states.
  // Read-only — no optimistic writes. Mirrors the silt-tasks hub's reload +
  // debounce + grouping shape, minus the plugin context.
  import { SvelteSet } from 'svelte/reactivity'
  import { onMount, onDestroy, untrack } from 'svelte'
  import { Events } from '@wailsio/runtime'
  import { ListTypes, QueryPagesByType } from '../../bindings/silt/app.js'
  import { coerceIPCError } from '../lib/ipcError'
  import { EventName } from '../generated/enums'
  import { trailingDebounce } from '../plugins/first-party/silt-tasks/debounce'
  import type { TypeDef } from '../properties/types'
  import TypeDashboardFilters from './TypeDashboardFilters.svelte'
  import TypeDashboardTable from './TypeDashboardTable.svelte'
  import TypeDashboardBoard from './TypeDashboardBoard.svelte'
  import {
    PAGE_COLUMN_KEY,
    binByProperty,
    buildColumns,
    type DashboardColumn,
    type FilterState,
    type GroupSection,
    type SortState,
    type TypeDashboardRow
  } from './dashboards'

  interface Props {
    /** Initial type id to display (may be '' to land on the picker). */
    typeName: string
    /** Open a page in the editor (wired by App to tab_manager.openPage). */
    onOpenPage: (locator: {
      source: string
      notebook: string
      section: string
      page: string
    }) => void
    /** Leave the dashboard (return to the editor). */
    onBack: () => void
    /** Open the in-app type editor (header "New type" + empty-state action).
     *  Optional so the dashboard stays mountable in isolation (tests). */
    onCreateType?: () => void
    /** Restore the shipped example types (Book, Meeting). Optional — the
     *  dashboard is the type-management surface, so it owns both empty-state
     *  escapes. */
    onRestoreExamples?: () => void
  }

  let { typeName, onOpenPage, onBack, onCreateType, onRestoreExamples }: Props =
    $props()

  let types = $state<TypeDef[]>([])
  let typesLoading = $state(true)
  let typesError = $state('')

  // Seed from the incoming prop once (untrack: the dashboard's type picker
  // then owns the value; a parent prop change does not silently switch types).
  let selectedType = $state(untrack(() => typeName))
  let rows = $state<TypeDashboardRow[]>([])
  let loading = $state(false)
  let error = $state('')

  let filter = $state<FilterState>({})
  let sort = $state<SortState>({ property: PAGE_COLUMN_KEY, desc: false })
  let groupBy = $state('')
  // View mode is local-only (not persisted) — the dashboard's primary mode
  // is the dense table; board is a glance surface for grouped browsing.
  let viewMode = $state<'list' | 'board'>('list')

  // Collapsed group keys survive a re-bin (SvelteSet so toggles are reactive).
  let collapsed = new SvelteSet<string>()

  // --- Type list -----------------------------------------------------------
  async function loadTypes(): Promise<void> {
    typesLoading = true
    typesError = ''
    try {
      const res = (await ListTypes()) as { types?: TypeDef[] } | null
      types = res?.types ?? []
      // If the incoming typeName isn't in the list (or none was passed), land
      // on the first available type so the dashboard shows something.
      if (types.length > 0 && !types.some((t) => t.id === selectedType)) {
        selectedType = types[0].id
      }
    } catch (e) {
      typesError = coerceIPCError(e).message
    } finally {
      typesLoading = false
    }
  }

  onMount(() => {
    void loadTypes()
    // The type set can change while the dashboard is mounted (a type file is
    // edited/added/deleted externally). Re-fetch the list + refresh the query
    // so the picker, columns, and rows don't go stale. Mirrors pageTypeState's
    // types:changed subscription + disposer pattern.
    const offTypesChanged = Events.On(EventName.EventTypesChanged, () => {
      void loadTypes()
      void reload()
    })
    return () => {
      offTypesChanged()
    }
  })

  // --- Query (debounced on filter) -----------------------------------------
  let loadSeq = 0
  async function reload(): Promise<void> {
    if (!selectedType) {
      rows = []
      return
    }
    const my = ++loadSeq
    // Keep existing rows during a refresh; only skeleton on the first load.
    if (rows.length === 0) loading = true
    error = ''
    try {
      const result = (await QueryPagesByType(
        selectedType,
        filter,
        sort.property,
        sort.desc
      )) as TypeDashboardRow[] | null
      if (my !== loadSeq) return
      rows = result ?? []
    } catch (e) {
      if (my !== loadSeq) return
      error = coerceIPCError(e).message
    } finally {
      if (my === loadSeq) loading = false
    }
  }

  // Trailing debounce: a burst of filter keystrokes (or rapid sort/type
  // switches) collapses to one query. One effect watching every input keeps a
  // single reload path so there's no mount double-fire.
  const debouncedReload = trailingDebounce(() => void reload(), 180)
  onDestroy(() => debouncedReload.cancel())

  $effect(() => {
    void selectedType
    void sort.property
    void sort.desc
    // Touch every filter value so any single one re-triggers the debounce.
    for (const k of Object.keys(filter)) void filter[k]
    void filter
    debouncedReload.trigger()
  })

  // --- Derived view state --------------------------------------------------
  let currentType = $derived(types.find((t) => t.id === selectedType) ?? null)
  let columns = $derived<DashboardColumn[]>(buildColumns(currentType))

  let sections = $derived.by<GroupSection[]>(() => {
    if (groupBy === '' || !currentType) {
      return [{ key: '__all__', label: '', rows }]
    }
    const prop = (currentType.properties ?? []).find((p) => p.name === groupBy)
    return binByProperty(rows, prop, groupBy)
  })

  function onSort(property: string): void {
    if (sort.property === property) {
      sort = { property, desc: !sort.desc }
    } else {
      sort = { property, desc: false }
    }
  }

  function toggleGroup(key: string): void {
    if (collapsed.has(key)) collapsed.delete(key)
    else collapsed.add(key)
  }

  // Reset collapse state when the group-by dimension changes (keys reshape).
  $effect(() => {
    void groupBy
    collapsed.clear()
  })

  let resultCount = $derived(rows.length)

  // Distinguishes "no pages of this type exist" from "filters excluded all
  // pages" so the empty state can offer a Clear-filters affordance in the
  // latter case. A present key is itself the filter — the IPC treats
  // filter[name]='' as "select rows where the property is unset", so the
  // empty string is a real filter value, not the absence of one.
  let hasFilters = $derived(Object.keys(filter).length > 0)
  let activeFilterCount = $derived(Object.keys(filter).length)

  function clearFilters(): void {
    filter = {}
  }

  // Switching types invalidates the prior type's filter/sort/group-by: the new
  // type's schema may not have the chosen property, which would otherwise blank
  // the dashboard with a stale, non-matching filter. Also drop rows immediately
  // so the previous type's pages never render under the new schema's columns
  // during the debounce/IPC window (reload only skeletons when rows is empty).
  function selectType(id: string): void {
    filter = {}
    sort = { property: PAGE_COLUMN_KEY, desc: false }
    groupBy = ''
    rows = []
    // Cover the trailing-debounce window so we never flash the empty state
    // ("No pages of this type") before reload starts.
    loading = id !== ''
    selectedType = id
  }

  // View-mode radiogroup: ArrowLeft/Right move between options (wrapping),
  // Home/End jump to boundaries. Roving tabindex — the active option is the
  // tab stop, others are removed from the tab order.
  const VIEW_MODES = [
    { value: 'list', label: 'List', icon: 'table_rows' },
    { value: 'board', label: 'Board', icon: 'view_kanban' }
  ] as const

  function chooseMode(mode: 'list' | 'board'): void {
    viewMode = mode
  }

  function onModeKeydown(e: KeyboardEvent): void {
    if (!['ArrowRight', 'ArrowLeft', 'Home', 'End'].includes(e.key)) return
    e.preventDefault()
    // currentTarget resets to null once the handler returns, so capture the
    // radiogroup element synchronously before scheduling the focus move.
    const group = e.currentTarget as HTMLElement
    const order = VIEW_MODES.map((m) => m.value)
    let next: 'list' | 'board'
    if (e.key === 'Home') next = order[0]
    else if (e.key === 'End') next = order[order.length - 1]
    else {
      const dir = e.key === 'ArrowLeft' ? -1 : 1
      const idx = order.indexOf(viewMode)
      next = order[(idx + dir + order.length) % order.length]
    }
    chooseMode(next)
    queueMicrotask(() => {
      group.querySelector<HTMLElement>(`[data-mode="${next}"]`)?.focus()
    })
  }
</script>

<section class="dashboard" aria-labelledby="dashboard-title">
  <header class="header">
    <div class="title-row">
      <button
        type="button"
        class="back"
        onclick={onBack}
        aria-label="Back to editor"
      >
        <span class="material-symbols-outlined text-icon-md" aria-hidden="true"
          >arrow_back</span
        >
      </button>
      <h1 id="dashboard-title" class="title">
        {#if currentType?.icon}
          <span
            class="material-symbols-outlined text-icon-lg"
            aria-hidden="true">{currentType.icon}</span
          >
        {/if}
        {currentType?.name ?? 'Type dashboard'}
      </h1>
      <div class="title-actions">
        <button
          type="button"
          class="new-type-btn"
          onclick={() => onCreateType?.()}
        >
          <span
            class="material-symbols-outlined text-icon-sm"
            aria-hidden="true">add</span
          >
          New type
        </button>
      </div>
    </div>
    {#if currentType?.description}
      <p class="desc">{currentType.description}</p>
    {/if}
  </header>

  {#if typesLoading}
    <div class="state" role="status" aria-live="polite">Loading types…</div>
  {:else if typesError}
    <div class="state error" role="alert">{typesError}</div>
  {:else if types.length === 0}
    <div class="state empty">
      <span class="material-symbols-outlined text-icon-2xl" aria-hidden="true"
        >category</span
      >
      <h2 class="state-title">No types defined yet</h2>
      <p class="state-body">
        Create a type to see its pages here, or restore the shipped examples.
      </p>
      <div class="state-actions">
        <button
          type="button"
          class="action-btn primary"
          onclick={() => onCreateType?.()}
        >
          <span
            class="material-symbols-outlined text-icon-sm"
            aria-hidden="true">add_circle</span
          >
          Create type
        </button>
        <button
          type="button"
          class="action-btn"
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
  {:else}
    <TypeDashboardFilters
      {types}
      {selectedType}
      {columns}
      {groupBy}
      {filter}
      totalCount={resultCount}
      onSelectType={selectType}
      onGroupByChange={(name) => (groupBy = name)}
      onFilterChange={(f) => (filter = f)}
    />

    {#if error}
      <div class="state error" role="alert">{error}</div>
    {:else if loading}
      <div class="state" role="status" aria-live="polite">Loading…</div>
    {:else if rows.length === 0}
      {#if hasFilters}
        <div class="state empty">
          <span
            class="material-symbols-outlined text-icon-2xl"
            aria-hidden="true">filter_alt_off</span
          >
          <h2 class="state-title">No matches</h2>
          <p class="state-body">
            No pages match your
            {activeFilterCount === 1 ? 'filter' : 'filters'}. Try adjusting or
            clearing them.
          </p>
          <button type="button" class="clear-btn" onclick={clearFilters}>
            <span
              class="material-symbols-outlined text-icon-sm"
              aria-hidden="true">close</span
            >
            Clear {activeFilterCount === 1 ? 'filter' : 'filters'}
          </button>
        </div>
      {:else}
        <div class="state empty">
          <span
            class="material-symbols-outlined text-icon-2xl"
            aria-hidden="true">inbox</span
          >
          <h2 class="state-title">No pages of this type</h2>
          <p class="state-body">Assign this type to a page to see it here.</p>
        </div>
      {/if}
    {:else}
      <div class="view-shell">
        <div
          class="view-toggle"
          role="radiogroup"
          aria-label="Dashboard view mode"
          tabindex="-1"
          onkeydown={onModeKeydown}
        >
          {#each VIEW_MODES as m (m.value)}
            <button
              type="button"
              role="radio"
              aria-checked={viewMode === m.value}
              aria-label="{m.label} view"
              tabindex={viewMode === m.value ? 0 : -1}
              data-mode={m.value}
              class="view-toggle-btn"
              class:active={viewMode === m.value}
              onclick={() => chooseMode(m.value)}
            >
              <span
                class="material-symbols-outlined text-icon-sm"
                aria-hidden="true">{m.icon}</span
              >
              <span aria-hidden="true">{m.label}</span>
            </button>
          {/each}
        </div>
      </div>
      {#if viewMode === 'board'}
        <TypeDashboardBoard
          {columns}
          {sections}
          grouped={groupBy !== ''}
          heroField={currentType?.heroField ?? ''}
          {onOpenPage}
        />
      {:else}
        <TypeDashboardTable
          {columns}
          {sections}
          grouped={groupBy !== ''}
          {sort}
          heroField={currentType?.heroField ?? ''}
          {collapsed}
          {onSort}
          onToggleGroup={toggleGroup}
          {onOpenPage}
        />
      {/if}
    {/if}
  {/if}
</section>

<style>
  .dashboard {
    display: flex;
    flex-direction: column;
    flex: 1 1 auto;
    min-height: 0;
    overflow: hidden;
  }
  .header {
    padding: 0.75rem 1rem;
    border-bottom: 1px solid var(--color-surface-panel-border);
    flex: 0 0 auto;
  }
  .title-row {
    display: flex;
    align-items: center;
    gap: 0.5rem;
  }
  .title-actions {
    margin-left: auto;
  }
  .new-type-btn {
    display: inline-flex;
    align-items: center;
    gap: 0.3rem;
    padding: 0.35rem 0.75rem;
    border: 1px solid var(--color-accent-primary-start);
    background: var(--color-accent-primary-glow);
    color: var(--color-accent-primary-start);
    border-radius: 0.375rem;
    font-family: var(--font-body, sans-serif);
    font-size: var(--text-type-sm);
    font-weight: 600;
    cursor: pointer;
    transition: background 120ms var(--transition-standard);
  }
  .new-type-btn:hover {
    background: var(--color-hover);
  }
  .new-type-btn:focus-visible {
    outline: 2px solid var(--color-border-focus);
    outline-offset: 1px;
  }
  .back {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    border: 1px solid var(--color-surface-panel-border);
    background: var(--color-surface-app);
    color: var(--color-text-muted);
    border-radius: 0.375rem;
    padding: 0.25rem;
    cursor: pointer;
  }
  .back:hover {
    background: var(--color-hover);
    color: var(--color-text-primary);
  }
  .back:focus-visible {
    outline: 2px solid var(--color-border-focus);
    outline-offset: 1px;
  }
  .title {
    display: inline-flex;
    align-items: center;
    gap: 0.4rem;
    font-family: var(--font-headline, sans-serif);
    font-size: var(--text-type-xl);
    font-weight: 600;
    color: var(--color-text-primary);
    margin: 0;
  }
  .desc {
    color: var(--color-text-muted);
    font-size: var(--text-type-sm);
    margin: 0.35rem 0 0;
  }
  .state {
    padding: 2rem;
    color: var(--color-text-muted);
    font-size: var(--text-type-md);
    text-align: center;
  }
  .state.error {
    color: var(--color-error-fg);
  }
  .state.empty {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 0.4rem;
    padding: 3rem 1rem;
  }
  .state.empty .material-symbols-outlined {
    opacity: 0.4;
  }
  .state-title {
    font-family: var(--font-headline, sans-serif);
    font-size: var(--text-type-lg);
    font-weight: 600;
    color: var(--color-text-primary);
    margin: 0;
  }
  .state-body {
    color: var(--color-text-muted);
    font-size: var(--text-type-sm);
    margin: 0;
    max-width: 28rem;
  }
  .state-actions {
    display: flex;
    flex-wrap: wrap;
    justify-content: center;
    gap: 0.5rem;
    margin-top: 0.8rem;
  }
  .action-btn {
    display: inline-flex;
    align-items: center;
    gap: 0.3rem;
    padding: 0.35rem 0.75rem;
    border: 1px solid var(--color-surface-panel-border);
    background: var(--color-surface-app);
    color: var(--color-text-primary);
    border-radius: 0.375rem;
    font-family: var(--font-body, sans-serif);
    font-size: var(--text-type-sm);
    font-weight: 600;
    cursor: pointer;
    transition:
      background 120ms var(--transition-standard),
      color 120ms var(--transition-standard);
  }
  .action-btn:hover {
    background: var(--color-hover);
  }
  .action-btn:focus-visible {
    outline: 2px solid var(--color-border-focus);
    outline-offset: 1px;
  }
  .action-btn.primary {
    border-color: var(--color-accent-primary-start);
    background: var(--color-accent-primary-glow);
    color: var(--color-accent-primary-start);
  }
  .action-btn.primary:hover {
    background: var(--color-hover);
  }
  .clear-btn {
    display: inline-flex;
    align-items: center;
    gap: 0.3rem;
    margin-top: 0.6rem;
    padding: 0.3rem 0.65rem;
    border: 1px solid var(--color-accent-primary-start);
    background: var(--color-accent-primary-glow);
    color: var(--color-accent-primary-start);
    border-radius: 0.375rem;
    font-size: var(--text-type-sm);
    font-family: var(--font-body, sans-serif);
    cursor: pointer;
  }
  .clear-btn:hover {
    background: var(--color-hover);
  }
  .clear-btn:focus-visible {
    outline: 2px solid var(--color-border-focus);
    outline-offset: 1px;
  }
  .view-shell {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    padding: 0.4rem 1rem;
    border-bottom: 1px solid var(--color-surface-panel-border);
  }
  .view-toggle {
    display: inline-flex;
    align-items: center;
    gap: 0.25rem;
    padding: 0.2rem;
    background: var(--color-surface-panel);
    border: 1px solid var(--color-surface-panel-border);
    border-radius: 0.5rem;
  }
  .view-toggle:focus-within {
    outline: 2px solid var(--color-border-focus);
    outline-offset: 1px;
  }
  .view-toggle-btn {
    display: inline-flex;
    align-items: center;
    gap: 0.3rem;
    border: 0;
    background: transparent;
    color: var(--color-text-muted);
    font-family: var(--font-body, sans-serif);
    font-size: var(--text-type-sm);
    padding: 0.2rem 0.55rem;
    border-radius: 0.375rem;
    cursor: pointer;
    transition:
      background 120ms var(--transition-standard),
      color 120ms var(--transition-standard);
  }
  .view-toggle-btn:hover {
    background: var(--color-hover);
    color: var(--color-text-primary);
  }
  .view-toggle-btn.active {
    background: var(--color-accent-primary-start);
    color: var(--color-text-on-accent);
  }
  .view-toggle-btn:focus-visible {
    outline: 2px solid var(--color-border-focus);
    outline-offset: 1px;
  }
</style>
