<script lang="ts">
  // Filter bar for the per-type dashboard. A type picker + group-by picker +
  // one control per schema property. Active filters render as removable chips.
  //
  // All single-value pickers are native <select>s (mirrors the checkbox filter
  // path and PropertiesPanel's type select). The OS/browser renders the open
  // list outside the page overflow tree, so nothing here can be clipped by
  // `.dashboard{overflow:hidden}` or the tabpanel flex chain — the bug class
  // that custom absolute menus hit in the real webview.
  import type { DashboardColumn, FilterState } from './dashboards'
  import type { TypeDef } from '../properties/types'

  interface Props {
    types: TypeDef[]
    selectedType: string
    columns: DashboardColumn[]
    groupBy: string
    filter: FilterState
    totalCount: number
    onSelectType: (id: string) => void
    onGroupByChange: (name: string) => void
    onFilterChange: (filter: FilterState) => void
  }

  let {
    types,
    selectedType,
    columns,
    groupBy,
    filter,
    totalCount,
    onSelectType,
    onGroupByChange,
    onFilterChange
  }: Props = $props()

  // Filterable columns exclude the page-name pseudo-column.
  let filterColumns = $derived(columns.filter((c) => c.kind !== 'page-name'))
  let groupColumns = $derived(columns.filter((c) => c.kind !== 'page-name'))

  // Sentinel for the "Unset" option on select/multiselect filters. Empty string
  // already means "Any" (key missing → clearFilter); Unset must write
  // filter[name] = '' which is a distinct IPC state. Option values that
  // collide with this sentinel are vanishingly unlikely on a schema enum.
  const UNSET_SENTINEL = '__silt_unset__'

  // --- Per-property filter mutators ----------------------------------------
  // IPC semantics: filter[name] = '' selects rows where the property is UNSET;
  // a missing key means "no filter" (Any). So setFilter keeps the empty string,
  // and clearFilter is the explicit "Any" path. Input controls (text/date/
  // checkbox-select) treat an empty value as "Any" → clear, since they have no
  // separate Unset affordance.
  function setFilter(name: string, value: string): void {
    onFilterChange({ ...filter, [name]: value })
  }

  function clearFilter(name: string): void {
    const next = { ...filter }
    delete next[name]
    onFilterChange(next)
  }

  function inputFilter(name: string, value: string): void {
    if (value === '') clearFilter(name)
    else setFilter(name, value)
  }

  // Select/multiselect filter: map the three UI states onto the filter bag.
  function selectFilter(name: string, value: string): void {
    if (value === '') clearFilter(name)
    else if (value === UNSET_SENTINEL) setFilter(name, '')
    else setFilter(name, value)
  }

  function selectFilterValue(name: string): string {
    if (!(name in filter)) return ''
    if (filter[name] === '') return UNSET_SENTINEL
    return filter[name]
  }

  function clearAll(): void {
    onFilterChange({})
  }

  let activeEntries = $derived(
    Object.entries(filter).map(([name, value]) => ({
      name,
      value,
      label: columns.find((c) => c.key === name)?.label ?? name
    }))
  )

  // aria-live count of active filters.
  let liveMessage = $state('')
  let lastCount = -1
  $effect(() => {
    const n = activeEntries.length
    if (n === lastCount) return
    lastCount = n
    liveMessage =
      n === 0 ? 'No active filters' : `${n} active filter${n === 1 ? '' : 's'}`
  })
</script>

<div class="filter-bar">
  <!-- Type picker — native <select> with a decorative icon (closed state).
       The open list is text-only (UA-rendered); that's the robustness tradeoff. -->
  <div class="type-select-wrap">
    <span class="material-symbols-outlined type-icon" aria-hidden="true"
      >category</span
    >
    <select
      class="type-select"
      aria-label="Select a type"
      value={selectedType}
      onchange={(e) => onSelectType(e.currentTarget.value)}
    >
      {#if !selectedType}
        <option value="">Select a type</option>
      {/if}
      {#each types as t (t.id)}
        <option value={t.id}>{t.name || t.id}</option>
      {/each}
    </select>
    <span class="material-symbols-outlined type-caret" aria-hidden="true"
      >expand_more</span
    >
  </div>

  <!-- Group-by picker -->
  <div class="group-select-wrap">
    <label class="group-label" for="dash-group-by">
      <span class="material-symbols-outlined text-icon-sm" aria-hidden="true"
        >view_module</span
      >
      <span>Group</span>
    </label>
    <select
      id="dash-group-by"
      class="select-input"
      aria-label="Group by"
      value={groupBy}
      onchange={(e) => onGroupByChange(e.currentTarget.value)}
    >
      <option value="">None</option>
      {#each groupColumns as col (col.key)}
        <option value={col.key}>{col.label}</option>
      {/each}
    </select>
  </div>

  <div class="divider" aria-hidden="true"></div>

  <!-- Per-property filter controls -->
  {#each filterColumns as col (col.key)}
    <div class="filter-control">
      <label for={`flt-${col.key}`} class="filter-label">{col.label}</label>
      {#if col.kind === 'select' || col.kind === 'multiselect'}
        <select
          id={`flt-${col.key}`}
          class="select-input"
          aria-label="Filter {col.label}"
          value={selectFilterValue(col.key)}
          onchange={(e) => selectFilter(col.key, e.currentTarget.value)}
        >
          <option value="">Any</option>
          <option value={UNSET_SENTINEL}>Unset</option>
          {#each col.options ?? [] as opt (opt)}
            <option value={opt}>{opt}</option>
          {/each}
        </select>
      {:else if col.kind === 'checkbox'}
        <select
          id={`flt-${col.key}`}
          class="select-input"
          aria-label="Filter {col.label}"
          value={selectFilterValue(col.key)}
          onchange={(e) => selectFilter(col.key, e.currentTarget.value)}
        >
          <option value="">Any</option>
          <option value={UNSET_SENTINEL}>Unset</option>
          <option value="true">Yes</option>
          <option value="false">No</option>
        </select>
      {:else if col.kind === 'date' || col.kind === 'datetime'}
        <input
          id={`flt-${col.key}`}
          type={col.kind === 'datetime' ? 'datetime-local' : 'date'}
          class="text-input"
          aria-label="Filter {col.label}"
          value={filter[col.key] ?? ''}
          oninput={(e) => inputFilter(col.key, e.currentTarget.value)}
        />
      {:else}
        <input
          id={`flt-${col.key}`}
          type="text"
          class="text-input"
          aria-label="Filter {col.label}"
          placeholder=" "
          value={filter[col.key] ?? ''}
          oninput={(e) => inputFilter(col.key, e.currentTarget.value)}
        />
      {/if}
    </div>
  {/each}

  {#if activeEntries.length > 0}
    <button type="button" class="clear-all" onclick={clearAll}>
      <span class="material-symbols-outlined text-icon-sm" aria-hidden="true"
        >close</span
      >
      <span>Clear all</span>
    </button>
  {/if}

  <div class="ml-auto count" aria-live="polite">
    {totalCount}
    {totalCount === 1 ? 'page' : 'pages'}
  </div>

  <div class="sr-only" aria-live="polite">{liveMessage}</div>
</div>

{#if activeEntries.length > 0}
  <div class="chips-row">
    {#each activeEntries as entry (entry.name)}
      <button
        type="button"
        class="active-chip"
        onclick={() => clearFilter(entry.name)}
        aria-label="Remove filter {entry.label}: {entry.value}"
      >
        <span class="ac-label">{entry.label}</span>
        <span class="ac-value"
          >{entry.value === '' ? 'unset' : entry.value}</span
        >
        <span class="material-symbols-outlined text-icon-xs" aria-hidden="true"
          >close</span
        >
      </button>
    {/each}
  </div>
{/if}

<style>
  .filter-bar {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    padding: 0.5rem 1rem;
    border-bottom: 1px solid var(--color-surface-panel-border);
    flex-wrap: wrap;
    position: relative;
    background: var(--color-surface-panel);
  }
  .type-select-wrap {
    position: relative;
    display: inline-flex;
    align-items: center;
  }
  .type-icon {
    position: absolute;
    left: 0.4rem;
    font-size: var(--text-icon-sm);
    color: var(--color-text-muted);
    pointer-events: none;
  }
  .type-select {
    appearance: none;
    -webkit-appearance: none;
    -moz-appearance: none;
    padding: 0.25rem 1.5rem 0.25rem 1.6rem;
    border-radius: 0.375rem;
    border: 1px solid var(--color-surface-panel-border);
    background: var(--color-surface-app);
    color: var(--color-text-primary);
    font-size: var(--text-type-sm);
    font-family: var(--font-mono, monospace);
    cursor: pointer;
    max-width: 14rem;
  }
  .type-select:hover {
    background: var(--color-hover);
  }
  .type-select:focus-visible {
    outline: 2px solid var(--color-border-focus);
    outline-offset: 1px;
  }
  .type-caret {
    position: absolute;
    right: 0.3rem;
    font-size: var(--text-type-sm);
    color: var(--color-text-muted);
    pointer-events: none;
  }
  .group-select-wrap {
    display: inline-flex;
    align-items: center;
    gap: 0.3rem;
  }
  .group-label {
    display: inline-flex;
    align-items: center;
    gap: 0.2rem;
    color: var(--color-text-muted);
    font-size: var(--text-type-sm);
    font-family: var(--font-mono, monospace);
  }
  .filter-control {
    display: flex;
    flex-direction: column;
    gap: 0.15rem;
  }
  .filter-label {
    font-size: var(--text-type-2xs);
    color: var(--color-text-muted);
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }
  .text-input,
  .select-input {
    background: var(--color-surface-app);
    border: 1px solid var(--color-surface-panel-border);
    color: var(--color-text-primary);
    border-radius: 0.375rem;
    padding: 0.25rem 0.4rem;
    font-size: var(--text-type-sm);
    min-width: 6rem;
  }
  .text-input:focus-visible,
  .select-input:focus-visible {
    outline: 2px solid var(--color-border-focus);
    outline-offset: 1px;
  }
  .ml-auto {
    margin-left: auto;
  }
  .count {
    color: var(--color-text-muted);
    font-size: var(--text-type-sm);
  }
  .divider {
    width: 1px;
    align-self: stretch;
    background: var(--color-surface-panel-border);
    margin: 0 0.25rem;
  }
  .clear-all {
    display: inline-flex;
    align-items: center;
    gap: 0.25rem;
    border: 0;
    background: transparent;
    color: var(--color-text-muted);
    font-size: var(--text-type-sm);
    cursor: pointer;
    padding: 0.25rem;
  }
  .clear-all:hover {
    color: var(--color-status-danger);
  }
  .chips-row {
    display: flex;
    flex-wrap: wrap;
    gap: 0.3rem;
    padding: 0.4rem 1rem;
    border-bottom: 1px solid var(--color-surface-panel-border);
    background: var(--color-surface-panel);
  }
  .active-chip {
    display: inline-flex;
    align-items: center;
    gap: 0.3rem;
    padding: 0.15rem 0.45rem;
    border-radius: 9999px;
    border: 1px solid var(--color-accent-primary-start);
    background: var(--color-accent-primary-glow);
    color: var(--color-accent-primary-start);
    font-size: var(--text-type-2xs);
    cursor: pointer;
    font-family: var(--font-mono, monospace);
  }
  .active-chip:hover {
    background: var(--color-hover);
  }
  .active-chip:focus-visible {
    outline: 2px solid var(--color-border-focus);
    outline-offset: 1px;
  }
  .ac-label {
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }
  .ac-value {
    opacity: 0.85;
  }
</style>
