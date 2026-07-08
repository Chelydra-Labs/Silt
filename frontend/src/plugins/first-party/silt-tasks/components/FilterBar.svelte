<script lang="ts">
  // Shared filter chip row for the Tasks hub (#424). Lifted from the legacy
  // silt-kanban FilterBar (identical shape — TaskFilters and KanbanFilters
  // are the same {owners, priorities, dueDate, tags}); the type now comes
  // from the unified state module so this component survives the kanban
  // retirement (#429). The sidebar (#432) mirrors these toggles bidirectionally.
  import { fly } from 'svelte/transition'
  import type {
    TaskFilters,
    DueDateFilter,
    GroupBy,
    SortMode,
    Scope
  } from '../state.svelte'

  interface Props {
    filters: TaskFilters
    owners: string[]
    tags: string[]
    onFiltersChange: (f: TaskFilters) => void
    groupBy: GroupBy
    onGroupByChange: (g: GroupBy) => void
    sort: SortMode
    onSortChange: (s: SortMode) => void
    scope: Scope
    onScopeChange: (s: Scope) => void
    isScopeDisabled: (s: string) => boolean
    scopeCrumb: string
    scopeUserOverride: boolean
    onResetScope: () => void
    totalCount: number
  }

  let {
    filters,
    owners,
    tags,
    onFiltersChange,
    groupBy,
    onGroupByChange,
    sort,
    onSortChange,
    scope,
    onScopeChange,
    isScopeDisabled,
    scopeCrumb,
    scopeUserOverride,
    onResetScope,
    totalCount
  }: Props = $props()

  type ChipKey =
    'scope' | 'group' | 'sort' | 'owner' | 'priority' | 'dueDate' | 'tags'
  let openChip = $state<ChipKey | null>(null)

  // #462: facet-list search. The DISTINCT facet queries (TasksHub.reloadFacets)
  // cap at 200 rows; with that many owners or tags, finding one is a scroll
  // hunt. A search field appears at the top of the owner/tag popovers only
  // when the list exceeds ~10 items (small lists are scannable without it).
  // Filtering is client-side over the already-fetched array — no debounce
  // needed at <500 items ($derived recomputes synchronously).
  const FACET_SEARCH_THRESHOLD = 10
  let ownerQuery = $state('')
  let tagQuery = $state('')
  let filteredOwners = $derived(
    ownerQuery.trim()
      ? owners.filter((o) =>
          o.toLowerCase().includes(ownerQuery.trim().toLowerCase())
        )
      : owners
  )
  let filteredTags = $derived(
    tagQuery.trim()
      ? tags.filter((t) =>
          t.toLowerCase().includes(tagQuery.trim().toLowerCase())
        )
      : tags
  )
  // Reset the search drafts when a popover closes so reopening starts fresh.
  $effect(() => {
    if (openChip !== 'owner') ownerQuery = ''
    if (openChip !== 'tags') tagQuery = ''
  })

  function toggleChip(k: ChipKey) {
    openChip = openChip === k ? null : k
  }
  function close() {
    openChip = null
  }

  const PRIORITIES = [
    { value: 1, label: 'Critical' },
    { value: 2, label: 'Normal' },
    { value: 3, label: 'Low' }
  ]
  const DUE_OPTIONS: { value: DueDateFilter; label: string }[] = [
    { value: '', label: 'All' },
    { value: 'overdue', label: 'Overdue' },
    { value: 'today', label: 'Today' },
    { value: 'week', label: 'This Week' },
    { value: 'none', label: 'No Date' }
  ]
  const SCOPES: { value: Scope; label: string; icon: string }[] = [
    { value: 'vault', label: 'Vault', icon: 'database' },
    { value: 'notebook', label: 'Notebook', icon: 'book' },
    { value: 'section', label: 'Section', icon: 'tag' },
    { value: 'page', label: 'Page', icon: 'description' }
  ]
  const GROUP_OPTIONS: { value: GroupBy; label: string; icon: string }[] = [
    { value: 'none', label: 'None', icon: 'grid_off' },
    { value: 'status', label: 'Status', icon: 'check_circle' },
    { value: 'owner', label: 'Owner', icon: 'person' },
    { value: 'priority', label: 'Priority', icon: 'flag' },
    { value: 'dueDate', label: 'Due date', icon: 'schedule' },
    { value: 'tag', label: 'Tag', icon: 'label' },
    { value: 'notebook', label: 'Notebook', icon: 'book' },
    { value: 'section', label: 'Section', icon: 'tag' },
    { value: 'page', label: 'Page', icon: 'description' }
  ]
  const SORT_OPTIONS: { value: SortMode; label: string; icon: string }[] = [
    { value: 'manual', label: 'Manual', icon: 'drag_indicator' },
    { value: 'dueDate', label: 'Due date', icon: 'schedule' },
    { value: 'priority', label: 'Priority', icon: 'flag' },
    { value: 'title', label: 'Title', icon: 'title' },
    { value: 'created', label: 'Created', icon: 'calendar_today' },
    { value: 'owner', label: 'Owner', icon: 'person' }
  ]

  function toggleOwner(o: string) {
    const has = filters.owners.includes(o)
    onFiltersChange({
      ...filters,
      owners: has
        ? filters.owners.filter((x) => x !== o)
        : [...filters.owners, o]
    })
  }
  function togglePriority(p: number) {
    const has = filters.priorities.includes(p)
    onFiltersChange({
      ...filters,
      priorities: has
        ? filters.priorities.filter((x) => x !== p)
        : [...filters.priorities, p]
    })
  }
  function setDueDate(d: DueDateFilter) {
    onFiltersChange({ ...filters, dueDate: d })
  }
  function toggleTag(t: string) {
    const has = filters.tags.includes(t)
    onFiltersChange({
      ...filters,
      tags: has ? filters.tags.filter((x) => x !== t) : [...filters.tags, t]
    })
  }
  function clearAll() {
    onFiltersChange({ owners: [], priorities: [], dueDate: '', tags: [] })
  }

  function handlePopoverKeydown(e: KeyboardEvent) {
    if (!['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(e.key)) return
    e.preventDefault()
    const container = e.currentTarget as HTMLElement
    const items = Array.from(
      container.querySelectorAll<HTMLElement>(
        'button, input[type="checkbox"], label'
      )
    )
    if (items.length === 0) return
    const activeIdx = items.indexOf(document.activeElement as HTMLElement)
    let nextIdx = activeIdx

    if (e.key === 'ArrowDown') {
      nextIdx = (activeIdx + 1) % items.length
    } else if (e.key === 'ArrowUp') {
      nextIdx = (activeIdx - 1 + items.length) % items.length
    } else if (e.key === 'Home') {
      nextIdx = 0
    } else if (e.key === 'End') {
      nextIdx = items.length - 1
    }

    items[nextIdx]?.focus()
  }

  // #462 follow-up: when the facet search field is focused, ArrowDown should
  // move focus into the first visible option (the listbox's own keydown handler
  // only fires for keys on listbox descendants, so the search input — a sibling
  // of the listbox — needs its own bridge). ArrowLeft/Right/Home/End are left
  // alone so the user can edit the query with the caret normally.
  function onFacetSearchKeydown(e: KeyboardEvent) {
    if (e.key !== 'ArrowDown') return
    const popover = (e.currentTarget as HTMLElement).closest('[role="group"]')
    // Focus the first checkbox directly (not the wrapping <label>, which isn't
    // focusable and would no-op the focus() call).
    const first = popover?.querySelector<HTMLInputElement>(
      'input[type="checkbox"]'
    )
    if (!first) return
    e.preventDefault()
    first.focus()
  }

  // A chip counts as "active" if it has at least one selection; the Clear-all
  // affordance appears once any chip is active.
  let activeCount = $derived(
    (filters.owners.length ? 1 : 0) +
      (filters.priorities.length ? 1 : 0) +
      (filters.dueDate ? 1 : 0) +
      (filters.tags.length ? 1 : 0)
  )

  function dueLabel(): string {
    return DUE_OPTIONS.find((o) => o.value === filters.dueDate)?.label ?? 'All'
  }

  // Escape closes the open chip popover. Bound to window while a chip is
  // open so it works regardless of where focus lives inside the popover.
  $effect(() => {
    if (!openChip) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') close()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  })
</script>

<div
  class="flex items-center gap-2 px-6 py-2 border-b border-surface-panel-border flex-wrap relative"
>
  <!-- Scope Chip -->
  <div class="relative">
    <button
      type="button"
      data-testid="tasks-hub-scope-toggle"
      onclick={() => toggleChip('scope')}
      aria-expanded={openChip === 'scope'}
      aria-haspopup="listbox"
      class="flex items-center gap-1.5 px-2.5 py-1 rounded border border-surface-panel-border bg-surface-panel text-[12px] font-label-sm text-text-muted hover:bg-hover hover:text-text-primary transition-colors {openChip ===
      'scope'
        ? 'border-accent-primary-start/40 text-text-primary'
        : ''}"
    >
      <span class="material-symbols-outlined text-[14px]">
        {scope === 'vault'
          ? 'database'
          : scope === 'notebook'
            ? 'book'
            : scope === 'section'
              ? 'tag'
              : 'description'}
      </span>
      <span
        >Scope: {scope === 'vault'
          ? 'Vault'
          : scope[0].toUpperCase() + scope.slice(1)}</span
      >
      <span class="material-symbols-outlined text-[12px]">expand_more</span>
    </button>
    {#if openChip === 'scope'}
      <div
        transition:fly={{ y: -4, duration: 100 }}
        onkeydown={handlePopoverKeydown}
        class="absolute z-50 mt-1 min-w-[160px] bg-surface-popover border border-surface-popover-border rounded-lg shadow-xl py-1"
        role="listbox"
        tabindex="-1"
        aria-label="Filter by scope"
      >
        {#each SCOPES as opt (opt.value)}
          {@const disabled = isScopeDisabled(opt.value)}
          <button
            type="button"
            data-testid={`scope-option-${opt.value}`}
            {disabled}
            onclick={() => {
              onScopeChange(opt.value)
              close()
            }}
            title={disabled ? `Select a ${opt.value} first` : undefined}
            class="w-full text-left flex items-center gap-2 px-3 py-1.5 hover:bg-hover text-[12px] font-label-sm transition-colors disabled:opacity-40 disabled:cursor-not-allowed disabled:hover:bg-transparent {scope ===
            opt.value
              ? 'text-accent-primary-start'
              : 'text-text-primary'}"
          >
            <span class="material-symbols-outlined text-[14px]">{opt.icon}</span
            >
            <span>{opt.label}</span>
            {#if scope === opt.value}
              <span class="material-symbols-outlined text-[14px] ml-auto"
                >check</span
              >
            {/if}
          </button>
        {/each}
      </div>
    {/if}
  </div>

  <div class="h-4 w-[1px] bg-surface-panel-border mx-2"></div>

  <!-- Group by Chip -->
  <div class="relative">
    <button
      type="button"
      data-testid="tasks-hub-group-by-toggle"
      onclick={() => toggleChip('group')}
      aria-expanded={openChip === 'group'}
      aria-haspopup="listbox"
      class="flex items-center gap-1.5 px-2.5 py-1 rounded border border-surface-panel-border bg-surface-panel text-[12px] font-label-sm text-text-muted hover:bg-hover hover:text-text-primary transition-colors {openChip ===
        'group' || groupBy !== 'none'
        ? 'border-accent-primary-start/40 text-text-primary'
        : ''}"
    >
      <span class="material-symbols-outlined text-[14px]">view_module</span>
      <span
        >Group: {GROUP_OPTIONS.find((o) => o.value === groupBy)?.label ??
          'None'}</span
      >
      <span class="material-symbols-outlined text-[12px]">expand_more</span>
    </button>
    {#if openChip === 'group'}
      <div
        transition:fly={{ y: -4, duration: 100 }}
        onkeydown={handlePopoverKeydown}
        class="absolute z-50 mt-1 min-w-[160px] bg-surface-popover border border-surface-popover-border rounded-lg shadow-xl py-1 max-h-64 overflow-y-auto custom-scrollbar"
        role="listbox"
        tabindex="-1"
        aria-label="Group tasks by"
      >
        {#each GROUP_OPTIONS as opt (opt.value)}
          <button
            type="button"
            data-testid={`group-option-${opt.value}`}
            onclick={() => {
              onGroupByChange(opt.value)
              close()
            }}
            class="w-full text-left flex items-center gap-2 px-3 py-1.5 hover:bg-hover text-[12px] font-label-sm transition-colors {groupBy ===
            opt.value
              ? 'text-accent-primary-start'
              : 'text-text-primary'}"
          >
            <span class="material-symbols-outlined text-[14px]">{opt.icon}</span
            >
            <span>{opt.label}</span>
            {#if groupBy === opt.value}
              <span class="material-symbols-outlined text-[14px] ml-auto"
                >check</span
              >
            {/if}
          </button>
        {/each}
      </div>
    {/if}
  </div>

  <!-- Sort Chip -->
  <div class="relative">
    <button
      type="button"
      data-testid="tasks-hub-sort-toggle"
      onclick={() => toggleChip('sort')}
      aria-expanded={openChip === 'sort'}
      aria-haspopup="listbox"
      class="flex items-center gap-1.5 px-2.5 py-1 rounded border border-surface-panel-border bg-surface-panel text-[12px] font-label-sm text-text-muted hover:bg-hover hover:text-text-primary transition-colors {openChip ===
        'sort' || sort !== 'manual'
        ? 'border-accent-primary-start/40 text-text-primary'
        : ''}"
    >
      <span class="material-symbols-outlined text-[14px]">sort</span>
      <span
        >Sort: {SORT_OPTIONS.find((o) => o.value === sort)?.label ??
          'Manual'}</span
      >
      <span class="material-symbols-outlined text-[12px]">expand_more</span>
    </button>
    {#if openChip === 'sort'}
      <div
        transition:fly={{ y: -4, duration: 100 }}
        onkeydown={handlePopoverKeydown}
        class="absolute z-50 mt-1 min-w-[160px] bg-surface-popover border border-surface-popover-border rounded-lg shadow-xl py-1"
        role="listbox"
        tabindex="-1"
        aria-label="Sort tasks by"
      >
        {#each SORT_OPTIONS as opt (opt.value)}
          <button
            type="button"
            data-testid={`sort-option-${opt.value}`}
            onclick={() => {
              onSortChange(opt.value)
              close()
            }}
            class="w-full text-left flex items-center gap-2 px-3 py-1.5 hover:bg-hover text-[12px] font-label-sm transition-colors {sort ===
            opt.value
              ? 'text-accent-primary-start'
              : 'text-text-primary'}"
          >
            <span class="material-symbols-outlined text-[14px]">{opt.icon}</span
            >
            <span>{opt.label}</span>
            {#if sort === opt.value}
              <span class="material-symbols-outlined text-[14px] ml-auto"
                >check</span
              >
            {/if}
          </button>
        {/each}
      </div>
    {/if}
  </div>

  <div class="h-4 w-[1px] bg-surface-panel-border mx-2"></div>
  <!-- Owner chip -->
  <div class="relative">
    <button
      type="button"
      onclick={() => toggleChip('owner')}
      aria-expanded={openChip === 'owner'}
      aria-haspopup="true"
      class="flex items-center gap-1.5 px-2.5 py-1 rounded border border-surface-panel-border bg-surface-panel text-[12px] font-label-sm text-text-muted hover:bg-hover hover:text-text-primary transition-colors {openChip ===
        'owner' || filters.owners.length
        ? 'border-accent-primary-start/40 text-text-primary'
        : ''}"
    >
      <span class="material-symbols-outlined text-[14px]">person</span>
      <span
        >Owner{filters.owners.length ? ` (${filters.owners.length})` : ''}</span
      >
      <span class="material-symbols-outlined text-[12px]">expand_more</span>
    </button>
    {#if openChip === 'owner'}
      <div
        transition:fly={{ y: -4, duration: 100 }}
        class="absolute z-50 mt-1 min-w-[180px] bg-surface-popover border border-surface-popover-border rounded-lg shadow-xl py-1 max-h-64 overflow-y-auto custom-scrollbar"
        role="group"
        tabindex="-1"
        aria-label="Filter by owner"
      >
        {#if owners.length > FACET_SEARCH_THRESHOLD}
          <div
            class="px-2 py-1 sticky top-[-4px] bg-surface-popover border-b border-surface-popover-border mb-1"
          >
            <input
              type="text"
              value={ownerQuery}
              oninput={(e) => (ownerQuery = e.currentTarget.value)}
              onkeydown={onFacetSearchKeydown}
              placeholder="Filter…"
              aria-label="Filter owners by name"
              data-testid="owner-facet-search"
              class="w-full px-2 py-1 rounded border border-surface-popover-border bg-surface-panel text-text-primary placeholder:text-text-muted text-[11px] focus:outline-none focus:ring-1 focus:ring-accent-primary-start/40"
            />
          </div>
        {/if}
        <div
          role="listbox"
          aria-label="Owners"
          tabindex="-1"
          onkeydown={handlePopoverKeydown}
        >
          {#if owners.length === 0}
            <div class="px-3 py-2 text-[11px] text-text-muted font-label-sm">
              No owners
            </div>
          {:else if filteredOwners.length === 0}
            <div class="px-3 py-2 text-[11px] text-text-muted font-label-sm">
              No matches
            </div>
          {:else}
            {#each filteredOwners as o (o)}
              <label
                class="flex items-center gap-2 px-3 py-1.5 hover:bg-hover cursor-pointer text-[12px] font-label-sm text-text-primary"
              >
                <input
                  type="checkbox"
                  checked={filters.owners.includes(o)}
                  onchange={() => toggleOwner(o)}
                  class="accent-accent-primary-start"
                />
                <span class="truncate">{o}</span>
              </label>
            {/each}
          {/if}
        </div>
      </div>
    {/if}
  </div>

  <!-- Priority chip -->
  <div class="relative">
    <button
      type="button"
      onclick={() => toggleChip('priority')}
      aria-expanded={openChip === 'priority'}
      aria-haspopup="true"
      class="flex items-center gap-1.5 px-2.5 py-1 rounded border border-surface-panel-border bg-surface-panel text-[12px] font-label-sm text-text-muted hover:bg-hover hover:text-text-primary transition-colors {openChip ===
        'priority' || filters.priorities.length
        ? 'border-accent-primary-start/40 text-text-primary'
        : ''}"
    >
      <span class="material-symbols-outlined text-[14px]">flag</span>
      <span
        >Priority{filters.priorities.length
          ? ` (${filters.priorities.length})`
          : ''}</span
      >
      <span class="material-symbols-outlined text-[12px]">expand_more</span>
    </button>
    {#if openChip === 'priority'}
      <div
        transition:fly={{ y: -4, duration: 100 }}
        onkeydown={handlePopoverKeydown}
        class="absolute z-50 mt-1 min-w-[160px] bg-surface-popover border border-surface-popover-border rounded-lg shadow-xl py-1"
        role="listbox"
        tabindex="-1"
        aria-label="Filter by priority"
      >
        {#each PRIORITIES as p (p.value)}
          <label
            class="flex items-center gap-2 px-3 py-1.5 hover:bg-hover cursor-pointer text-[12px] font-label-sm text-text-primary"
          >
            <input
              type="checkbox"
              checked={filters.priorities.includes(p.value)}
              onchange={() => togglePriority(p.value)}
              class="accent-accent-primary-start"
            />
            <span>{p.label}</span>
          </label>
        {/each}
      </div>
    {/if}
  </div>

  <!-- Due date chip -->
  <div class="relative">
    <button
      type="button"
      onclick={() => toggleChip('dueDate')}
      aria-expanded={openChip === 'dueDate'}
      aria-haspopup="listbox"
      class="flex items-center gap-1.5 px-2.5 py-1 rounded border border-surface-panel-border bg-surface-panel text-[12px] font-label-sm text-text-muted hover:bg-hover hover:text-text-primary transition-colors {openChip ===
        'dueDate' || filters.dueDate
        ? 'border-accent-primary-start/40 text-text-primary'
        : ''}"
    >
      <span class="material-symbols-outlined text-[14px]">schedule</span>
      <span>{filters.dueDate ? dueLabel() : 'Due date'}</span>
      <span class="material-symbols-outlined text-[12px]">expand_more</span>
    </button>
    {#if openChip === 'dueDate'}
      <div
        transition:fly={{ y: -4, duration: 100 }}
        onkeydown={handlePopoverKeydown}
        class="absolute z-50 mt-1 min-w-[160px] bg-surface-popover border border-surface-popover-border rounded-lg shadow-xl py-1"
        role="listbox"
        tabindex="-1"
        aria-label="Filter by due date"
      >
        {#each DUE_OPTIONS as opt (opt.value)}
          <button
            type="button"
            onclick={() => setDueDate(opt.value)}
            class="w-full text-left flex items-center gap-2 px-3 py-1.5 hover:bg-hover text-[12px] font-label-sm {filters.dueDate ===
            opt.value
              ? 'text-accent-primary-start'
              : 'text-text-primary'}"
          >
            <span>{opt.label}</span>
            {#if filters.dueDate === opt.value}
              <span class="material-symbols-outlined text-[14px] ml-auto"
                >check</span
              >
            {/if}
          </button>
        {/each}
      </div>
    {/if}
  </div>

  <!-- Tags chip -->
  <div class="relative">
    <button
      type="button"
      onclick={() => toggleChip('tags')}
      aria-expanded={openChip === 'tags'}
      aria-haspopup="true"
      class="flex items-center gap-1.5 px-2.5 py-1 rounded border border-surface-panel-border bg-surface-panel text-[12px] font-label-sm text-text-muted hover:bg-hover hover:text-text-primary transition-colors {openChip ===
        'tags' || filters.tags.length
        ? 'border-accent-primary-start/40 text-text-primary'
        : ''}"
    >
      <span class="material-symbols-outlined text-[14px]">label</span>
      <span>Tags{filters.tags.length ? ` (${filters.tags.length})` : ''}</span>
      <span class="material-symbols-outlined text-[12px]">expand_more</span>
    </button>
    {#if openChip === 'tags'}
      <div
        transition:fly={{ y: -4, duration: 100 }}
        class="absolute z-50 mt-1 min-w-[200px] bg-surface-popover border border-surface-popover-border rounded-lg shadow-xl py-1 max-h-64 overflow-y-auto custom-scrollbar"
        role="group"
        tabindex="-1"
        aria-label="Filter by tag"
      >
        {#if tags.length > FACET_SEARCH_THRESHOLD}
          <div
            class="px-2 py-1 sticky top-[-4px] bg-surface-popover border-b border-surface-popover-border mb-1"
          >
            <input
              type="text"
              value={tagQuery}
              oninput={(e) => (tagQuery = e.currentTarget.value)}
              onkeydown={onFacetSearchKeydown}
              placeholder="Filter…"
              aria-label="Filter tags by name"
              data-testid="tag-facet-search"
              class="w-full px-2 py-1 rounded border border-surface-popover-border bg-surface-panel text-text-primary placeholder:text-text-muted text-[11px] focus:outline-none focus:ring-1 focus:ring-accent-primary-start/40"
            />
          </div>
        {/if}
        <div
          role="listbox"
          aria-label="Tags"
          tabindex="-1"
          onkeydown={handlePopoverKeydown}
        >
          {#if tags.length === 0}
            <div class="px-3 py-2 text-[11px] text-text-muted font-label-sm">
              No tags
            </div>
          {:else if filteredTags.length === 0}
            <div class="px-3 py-2 text-[11px] text-text-muted font-label-sm">
              No matches
            </div>
          {:else}
            {#each filteredTags as t (t)}
              <label
                class="flex items-center gap-2 px-3 py-1.5 hover:bg-hover cursor-pointer text-[12px] font-label-sm text-text-primary"
              >
                <input
                  type="checkbox"
                  checked={filters.tags.includes(t)}
                  onchange={() => toggleTag(t)}
                  class="accent-accent-primary-start"
                />
                <span class="truncate">{t}</span>
              </label>
            {/each}
          {/if}
        </div>
      </div>
    {/if}
  </div>

  {#if activeCount > 0}
    <button
      type="button"
      onclick={clearAll}
      class="flex items-center gap-1 px-2 py-1 text-[12px] font-label-sm text-text-muted hover:text-error transition-colors"
    >
      <span class="material-symbols-outlined text-[14px]">close</span>
      <span>Clear all</span>
    </button>
  {/if}

  {#if activeCount > 0}
    <span class="text-[11px] text-text-muted font-label-sm">
      {activeCount} active filter{activeCount === 1 ? '' : 's'}
    </span>
  {/if}

  <div class="ml-auto flex items-center gap-2">
    <span class="text-text-muted text-[12px] font-body-md">
      {scopeCrumb} · {totalCount} task{totalCount === 1 ? '' : 's'}
    </span>
    {#if scopeUserOverride}
      <button
        type="button"
        onclick={onResetScope}
        aria-label="Reset Tasks scope to follow navigation"
        title="Follow navigation"
        class="flex items-center gap-1 px-1.5 py-0.5 rounded border border-surface-panel-border text-text-muted hover:text-accent-primary-start hover:border-accent-primary-start/40 transition-colors"
      >
        <span class="material-symbols-outlined text-[14px]">my_location</span>
        <span class="font-label-sm">Follow</span>
      </button>
    {/if}
  </div>

  <!-- Click-away backdrop: closes whichever chip popover is open. -->
  {#if openChip}
    <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
    <div
      class="fixed inset-0 z-40"
      role="presentation"
      onclick={close}
      tabindex="-1"
      aria-hidden="true"
    ></div>
  {/if}
</div>
