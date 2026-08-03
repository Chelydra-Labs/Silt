<script lang="ts">
  // Filter bar for the per-type dashboard. A type picker + group-by picker +
  // one control per schema property. Active filters render as removable chips.
  // The control set mirrors the silt-tasks FilterBar's single-open-popover
  // pattern: at most one dropdown is open at a time, Esc closes it, and a
  // full-screen click-away backdrop handles outside clicks.
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

  let openMenu = $state<string | null>(null)

  function close(): void {
    openMenu = null
  }

  // --- Listbox keyboard model (WAI-ARIA roving tabindex) ------------------
  // The three dropdowns are role="listbox"; Arrow/Home/End move the active
  // option, Enter/Space activate via the native button click. Only one menu
  // is open at a time, so a single activeIdx scoped to whichever menu is
  // open is sufficient. Mirrors the view-mode radiogroup in TypeDashboard.
  let activeIdx = $state(0)

  function menuOptionCount(menuId: string): number {
    if (menuId === 'type') return types.length
    if (menuId === 'group') return 1 + groupColumns.length
    const col = filterColumns.find((c) => c.key === menuId)
    return col ? 2 + (col.options?.length ?? 0) : 0
  }

  // Where focus should land when a menu opens: the currently-selected option
  // (the one with aria-selected="true"), or the first if none is selected.
  function menuInitialIdx(menuId: string): number {
    if (menuId === 'type') {
      const i = types.findIndex((t) => t.id === selectedType)
      return i < 0 ? 0 : i
    }
    if (menuId === 'group') {
      if (groupBy === '') return 0
      const i = groupColumns.findIndex((c) => c.key === groupBy)
      return i < 0 ? 0 : i + 1
    }
    const col = filterColumns.find((c) => c.key === menuId)
    if (!col) return 0
    if (!(menuId in filter)) return 0
    if (filter[menuId] === '') return 1
    const i = (col.options ?? []).findIndex((o) => o === filter[menuId])
    return i < 0 ? 0 : i + 2
  }

  function toggleMenu(id: string): void {
    if (openMenu === id) {
      close()
      return
    }
    openMenu = id
    activeIdx = menuInitialIdx(id)
    const targetIdx = activeIdx
    // The listbox DOM renders on the same flush; focus after it settles so
    // arrow keys work immediately without an intervening Tab.
    queueMicrotask(() => {
      document
        .querySelector<HTMLElement>(
          `[data-listbox="${id}"] [data-option-idx="${targetIdx}"]`
        )
        ?.focus()
    })
  }

  // Roving tabindex: the active option is the tab stop; siblings are removed
  // from the tab order. Closed menus aren't rendered, so this only matters
  // for the open one.
  function optTabindex(menuId: string, idx: number): 0 | -1 {
    return openMenu === menuId && activeIdx === idx ? 0 : -1
  }

  function onListboxKeydown(e: KeyboardEvent): void {
    if (!openMenu) return
    if (!['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(e.key)) return
    e.preventDefault()
    const count = menuOptionCount(openMenu)
    if (count === 0) return
    let next = activeIdx
    if (e.key === 'Home') next = 0
    else if (e.key === 'End') next = count - 1
    else if (e.key === 'ArrowDown') next = Math.min(activeIdx + 1, count - 1)
    else next = Math.max(activeIdx - 1, 0)
    if (next === activeIdx) return
    activeIdx = next
    const container = e.currentTarget as HTMLElement
    queueMicrotask(() => {
      container
        .querySelector<HTMLElement>(`[data-option-idx="${next}"]`)
        ?.focus()
    })
  }

  let selectedTypeLabel = $derived(
    types.find((t) => t.id === selectedType)?.name ?? 'Select a type'
  )
  let groupLabel = $derived(
    groupBy === ''
      ? 'None'
      : (columns.find((c) => c.key === groupBy)?.label ?? groupBy)
  )

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

  // Esc closes whichever popover is open.
  $effect(() => {
    if (!openMenu) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') close()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  })
</script>

<div class="filter-bar">
  <!-- Type picker -->
  <div class="relative">
    <button
      type="button"
      class="chip-button"
      class:active={openMenu === 'type'}
      onclick={() => toggleMenu('type')}
      aria-expanded={openMenu === 'type'}
      aria-haspopup="listbox"
    >
      <span class="material-symbols-outlined text-icon-sm" aria-hidden="true"
        >category</span
      >
      <span>{selectedTypeLabel}</span>
      <span class="material-symbols-outlined text-icon-xs" aria-hidden="true"
        >expand_more</span
      >
    </button>
    {#if openMenu === 'type'}
      <div
        class="menu"
        role="listbox"
        aria-label="Select a type"
        tabindex="-1"
        data-listbox="type"
        onkeydown={onListboxKeydown}
      >
        {#each types as t, i (t.id)}
          <button
            type="button"
            role="option"
            class="menu-item"
            class:selected={t.id === selectedType}
            aria-selected={t.id === selectedType}
            data-option-idx={i}
            tabindex={optTabindex('type', i)}
            onclick={() => {
              onSelectType(t.id)
              close()
            }}
          >
            {#if t.icon}
              <span
                class="material-symbols-outlined text-icon-sm"
                aria-hidden="true">{t.icon}</span
              >
            {/if}
            <span>{t.name || t.id}</span>
            {#if t.id === selectedType}
              <span
                class="material-symbols-outlined text-icon-sm ml-auto"
                aria-hidden="true">check</span
              >
            {/if}
          </button>
        {/each}
      </div>
    {/if}
  </div>

  <!-- Group-by picker -->
  <div class="relative">
    <button
      type="button"
      class="chip-button"
      class:active={openMenu === 'group' || groupBy !== ''}
      onclick={() => toggleMenu('group')}
      aria-expanded={openMenu === 'group'}
      aria-haspopup="listbox"
    >
      <span class="material-symbols-outlined text-icon-sm" aria-hidden="true"
        >view_module</span
      >
      <span>Group: {groupLabel}</span>
      <span class="material-symbols-outlined text-icon-xs" aria-hidden="true"
        >expand_more</span
      >
    </button>
    {#if openMenu === 'group'}
      <div
        class="menu"
        role="listbox"
        aria-label="Group by"
        tabindex="-1"
        data-listbox="group"
        onkeydown={onListboxKeydown}
      >
        <button
          type="button"
          role="option"
          class="menu-item"
          class:selected={groupBy === ''}
          aria-selected={groupBy === ''}
          data-option-idx="0"
          tabindex={optTabindex('group', 0)}
          onclick={() => {
            onGroupByChange('')
            close()
          }}
        >
          <span>None</span>
          {#if groupBy === ''}
            <span
              class="material-symbols-outlined text-icon-sm ml-auto"
              aria-hidden="true">check</span
            >
          {/if}
        </button>
        {#each groupColumns as col, i (col.key)}
          <button
            type="button"
            role="option"
            class="menu-item"
            class:selected={groupBy === col.key}
            aria-selected={groupBy === col.key}
            data-option-idx={i + 1}
            tabindex={optTabindex('group', i + 1)}
            onclick={() => {
              onGroupByChange(col.key)
              close()
            }}
          >
            <span>{col.label}</span>
            {#if groupBy === col.key}
              <span
                class="material-symbols-outlined text-icon-sm ml-auto"
                aria-hidden="true">check</span
              >
            {/if}
          </button>
        {/each}
      </div>
    {/if}
  </div>

  <div class="divider" aria-hidden="true"></div>

  <!-- Per-property filter controls -->
  {#each filterColumns as col (col.key)}
    <div class="filter-control">
      <label for={`flt-${col.key}`} class="filter-label">{col.label}</label>
      {#if col.kind === 'select' || col.kind === 'multiselect'}
        <div class="relative">
          <button
            type="button"
            class="chip-button"
            class:active={openMenu === col.key || filter[col.key] != null}
            id={`flt-${col.key}`}
            onclick={() => toggleMenu(col.key)}
            aria-expanded={openMenu === col.key}
            aria-haspopup="listbox"
            aria-label="Filter {col.label}"
          >
            <span>{filter[col.key] ?? 'Any'}</span>
            <span
              class="material-symbols-outlined text-icon-xs"
              aria-hidden="true">expand_more</span
            >
          </button>
          {#if openMenu === col.key}
            <div
              class="menu"
              role="listbox"
              aria-label="Filter {col.label}"
              tabindex="-1"
              data-listbox={col.key}
              onkeydown={onListboxKeydown}
            >
              <button
                type="button"
                role="option"
                class="menu-item"
                aria-selected={!(col.key in filter)}
                data-option-idx="0"
                tabindex={optTabindex(col.key, 0)}
                onclick={() => {
                  clearFilter(col.key)
                  close()
                }}
              >
                <span>Any</span>
              </button>
              <button
                type="button"
                role="option"
                class="menu-item"
                aria-selected={filter[col.key] === ''}
                data-option-idx="1"
                tabindex={optTabindex(col.key, 1)}
                onclick={() => {
                  setFilter(col.key, '')
                  close()
                }}
              >
                <span class="italic">Unset</span>
              </button>
              {#each col.options ?? [] as opt, i (opt)}
                <button
                  type="button"
                  role="option"
                  class="menu-item"
                  class:selected={filter[col.key] === opt}
                  aria-selected={filter[col.key] === opt}
                  data-option-idx={i + 2}
                  tabindex={optTabindex(col.key, i + 2)}
                  onclick={() => {
                    setFilter(col.key, opt)
                    close()
                  }}
                >
                  <span>{opt}</span>
                </button>
              {/each}
            </div>
          {/if}
        </div>
      {:else if col.kind === 'checkbox'}
        <select
          id={`flt-${col.key}`}
          class="select-input"
          aria-label="Filter {col.label}"
          value={filter[col.key] ?? ''}
          onchange={(e) => inputFilter(col.key, e.currentTarget.value)}
        >
          <option value="">Any</option>
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

  <!-- Active filter chips (quick-remove). Rendered below in the chips row. -->

  {#if openMenu}
    <button
      type="button"
      class="backdrop"
      aria-hidden="true"
      tabindex="-1"
      onclick={close}
    ></button>
  {/if}

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
  .relative {
    position: relative;
    display: inline-flex;
  }
  .chip-button {
    display: inline-flex;
    align-items: center;
    gap: 0.3rem;
    padding: 0.25rem 0.5rem;
    border-radius: 0.375rem;
    border: 1px solid var(--color-surface-panel-border);
    background: var(--color-surface-app);
    color: var(--color-text-muted);
    font-size: var(--text-type-sm);
    font-family: var(--font-mono, monospace);
    cursor: pointer;
    max-width: 14rem;
  }
  .chip-button > span:nth-child(2) {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .chip-button:hover {
    background: var(--color-hover);
    color: var(--color-text-primary);
  }
  .chip-button.active {
    border-color: var(--color-accent-primary-start);
    color: var(--color-text-primary);
  }
  .chip-button:focus-visible {
    outline: 2px solid var(--color-border-focus);
    outline-offset: 1px;
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
  .menu {
    position: absolute;
    top: calc(100% + 0.2rem);
    left: 0;
    z-index: 50;
    min-width: 12rem;
    max-height: 16rem;
    overflow-y: auto;
    background: var(--color-surface-popover);
    border: 1px solid var(--color-surface-popover-border);
    border-radius: 0.5rem;
    box-shadow: var(--shadow-lg);
    padding: 0.25rem;
  }
  .menu-item {
    display: flex;
    align-items: center;
    gap: 0.4rem;
    width: 100%;
    text-align: left;
    padding: 0.35rem 0.5rem;
    border: 0;
    background: transparent;
    color: var(--color-surface-popover-text);
    border-radius: 0.3rem;
    font-size: var(--text-type-sm);
    cursor: pointer;
  }
  .menu-item:hover {
    background: var(--color-hover);
  }
  .menu-item:focus-visible {
    outline: 2px solid var(--color-border-focus);
    outline-offset: 1px;
  }
  .menu-item.selected {
    color: var(--color-accent-primary-start);
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
  .backdrop {
    position: fixed;
    inset: 0;
    z-index: 40;
    background: transparent;
    border: 0;
    cursor: default;
    padding: 0;
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
  .italic {
    font-style: italic;
    opacity: 0.7;
  }
</style>
