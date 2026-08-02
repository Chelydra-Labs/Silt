<script lang="ts">
  // Per-type dashboard container. Owns: the type list (ListTypes), the selected
  // type, the query (QueryPagesByType) with a trailing-debounce on filter
  // typing, client-side group-by binning, and loading/error/empty states.
  // Read-only — no optimistic writes. Mirrors the silt-tasks hub's reload +
  // debounce + grouping shape, minus the plugin context.
  import { SvelteSet } from 'svelte/reactivity'
  import { onMount, onDestroy, untrack } from 'svelte'
  import { ListTypes, QueryPagesByType } from '../../bindings/silt/app.js'
  import { coerceIPCError } from '../lib/ipcError'
  import { trailingDebounce } from '../plugins/first-party/silt-tasks/debounce'
  import type { TypeDef } from '../properties/types'
  import TypeDashboardFilters from './TypeDashboardFilters.svelte'
  import TypeDashboardTable from './TypeDashboardTable.svelte'
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
  }

  let { typeName, onOpenPage, onBack }: Props = $props()

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
        Create a type definition in <code>.system/types/</code> to see its pages here.
      </p>
    </div>
  {:else}
    <TypeDashboardFilters
      {types}
      {selectedType}
      {columns}
      {groupBy}
      {filter}
      totalCount={resultCount}
      onSelectType={(id) => (selectedType = id)}
      onGroupByChange={(name) => (groupBy = name)}
      onFilterChange={(f) => (filter = f)}
    />

    {#if error}
      <div class="state error" role="alert">{error}</div>
    {:else if loading}
      <div class="state" role="status" aria-live="polite">Loading…</div>
    {:else if rows.length === 0}
      <div class="state empty">
        <span class="material-symbols-outlined text-icon-2xl" aria-hidden="true"
          >inbox</span
        >
        <h2 class="state-title">No pages of this type</h2>
        <p class="state-body">Assign this type to a page to see it here.</p>
      </div>
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
  code {
    font-family: var(--font-mono, monospace);
    font-size: var(--text-type-sm);
    background: var(--color-surface-panel);
    padding: 0.05rem 0.3rem;
    border-radius: 0.25rem;
  }
</style>
