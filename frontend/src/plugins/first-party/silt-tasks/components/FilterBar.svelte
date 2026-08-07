<script lang="ts">
  // Shared filter chip row for the Tasks hub (#424). Lifted from the legacy
  // silt-kanban FilterBar (identical shape — TaskFilters and KanbanFilters
  // are the same {owners, priorities, dueDate, tags}); the type now comes
  // from the unified state module so this component survives the kanban
  // retirement (#429). The sidebar (#432) mirrors these toggles bidirectionally.
  import { tick } from 'svelte'
  import { fly } from 'svelte/transition'
  import type {
    TaskFilters,
    DueDateFilter,
    GroupBy,
    SortMode,
    Scope
  } from '../state.svelte'
  import { motionDuration } from '../motion'

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
  let activeTrigger = $state<HTMLButtonElement | null>(null)

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

  function toggleChip(k: ChipKey, trigger: HTMLButtonElement) {
    if (openChip === k) {
      close()
      return
    }
    activeTrigger = trigger
    openChip = k
    void tick().then(() => {
      document
        .querySelector<HTMLElement>(
          `[data-filter-popover="${k}"] input:not([type="checkbox"]), [data-filter-popover="${k}"] button:not(:disabled), [data-filter-popover="${k}"] input[type="checkbox"]`
        )
        ?.focus()
    })
  }
  function close(returnFocus = false) {
    openChip = null
    if (returnFocus) void tick().then(() => activeTrigger?.focus())
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
    { value: 'owner', label: 'Owner', icon: 'person' },
    { value: 'modified', label: 'Recently Modified', icon: 'update' },
    { value: 'estimate', label: 'Estimate', icon: 'timer' }
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
  function toggleStale() {
    onFiltersChange({ ...filters, stale: !filters.stale })
  }
  function clearAll() {
    onFiltersChange({
      owners: [],
      priorities: [],
      dueDate: '',
      tags: [],
      stale: false
    })
  }

  function handlePopoverKeydown(e: KeyboardEvent) {
    if (!['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(e.key)) return
    e.preventDefault()
    const container = e.currentTarget as HTMLElement
    const items = Array.from(
      container.querySelectorAll<HTMLElement>(
        'button:not(:disabled), input[type="checkbox"]:not(:disabled)'
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
    if (e.key === 'Home' || e.key === 'End') {
      // The surrounding popover uses Home/End for option navigation. Keep
      // those native caret keys inside the search field.
      e.stopPropagation()
      return
    }
    if (e.key !== 'ArrowDown') return
    const popover = (e.currentTarget as HTMLElement).closest(
      '[data-filter-popover]'
    )
    // Focus the first checkbox directly (not the wrapping <label>, which isn't
    // focusable and would no-op the focus() call).
    const first = popover?.querySelector<HTMLInputElement>(
      'input[type="checkbox"]'
    )
    if (!first) return
    e.preventDefault()
    e.stopPropagation()
    first.focus()
  }

  // A chip counts as "active" if it has at least one selection; the Clear-all
  // affordance appears once any chip is active.
  let activeCount = $derived(
    (filters.owners.length ? 1 : 0) +
      (filters.priorities.length ? 1 : 0) +
      (filters.dueDate ? 1 : 0) +
      (filters.tags.length ? 1 : 0) +
      (filters.stale ? 1 : 0)
  )

  // aria-live announcer for active-filter changes. The unified sidebar used to
  // carry this in its combined counts+filters+views region; the decomposition
  // moved counts to SmartLists and trimmed that region, so filter state lives
  // here — this is its announcement home (AGENTS.md: aria-live for dynamic
  // updates). Gates on lastAnnouncedFilters to avoid re-announcing unchanged.
  let filterLiveMessage = $state('')
  let lastAnnouncedFilters = -1
  $effect(() => {
    const n = activeCount
    if (n === lastAnnouncedFilters) return
    lastAnnouncedFilters = n
    filterLiveMessage =
      n === 0 ? 'No active filters' : `${n} active filter${n === 1 ? '' : 's'}`
  })

  function dueLabel(): string {
    return DUE_OPTIONS.find((o) => o.value === filters.dueDate)?.label ?? 'All'
  }

  // Escape closes the open chip popover. Bound to window while a chip is
  // open so it works regardless of where focus lives inside the popover.
  $effect(() => {
    if (!openChip) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') close(true)
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  })
</script>

<div
  class="tasks-filter-toolbar relative z-20 flex flex-wrap items-center gap-1.5 border-b border-surface-panel-border px-3 py-2 sm:px-5 lg:px-6"
  aria-label="Task view controls"
>
  <!-- Scope Chip -->
  <div class="relative">
    <button
      type="button"
      data-testid="tasks-hub-scope-toggle"
      onclick={(e) => toggleChip('scope', e.currentTarget)}
      aria-expanded={openChip === 'scope'}
      aria-haspopup="listbox"
      aria-controls="tasks-filter-scope"
      data-active={openChip === 'scope' ? 'true' : undefined}
      class="task-filter-chip"
    >
      <span class="material-symbols-outlined text-icon-sm">
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
      <span class="material-symbols-outlined text-icon-xs">expand_more</span>
    </button>
    {#if openChip === 'scope'}
      <div
        id="tasks-filter-scope"
        data-filter-popover="scope"
        transition:fly={{ y: -4, duration: motionDuration(100) }}
        onkeydown={handlePopoverKeydown}
        class="task-filter-menu absolute left-0 z-50 mt-1 min-w-40 py-1"
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
              close(true)
            }}
            title={disabled ? `Select a ${opt.value} first` : undefined}
            role="option"
            aria-selected={scope === opt.value}
            class="task-filter-option disabled:opacity-40 disabled:cursor-not-allowed disabled:hover:bg-transparent {scope ===
            opt.value
              ? 'text-accent-primary-start'
              : 'text-text-primary'}"
          >
            <span class="material-symbols-outlined text-icon-sm"
              >{opt.icon}</span
            >
            <span>{opt.label}</span>
            {#if scope === opt.value}
              <span class="material-symbols-outlined text-icon-sm ml-auto"
                >check</span
              >
            {/if}
          </button>
        {/each}
      </div>
    {/if}
  </div>

  <div class="mx-1 hidden h-5 w-px bg-surface-panel-border sm:block"></div>

  <!-- Group by Chip -->
  <div class="relative">
    <button
      type="button"
      data-testid="tasks-hub-group-by-toggle"
      onclick={(e) => toggleChip('group', e.currentTarget)}
      aria-expanded={openChip === 'group'}
      aria-haspopup="listbox"
      aria-controls="tasks-filter-group"
      data-active={openChip === 'group' || groupBy !== 'none'
        ? 'true'
        : undefined}
      class="task-filter-chip"
    >
      <span class="material-symbols-outlined text-icon-sm">view_module</span>
      <span
        >Group: {GROUP_OPTIONS.find((o) => o.value === groupBy)?.label ??
          'None'}</span
      >
      <span class="material-symbols-outlined text-icon-xs">expand_more</span>
    </button>
    {#if openChip === 'group'}
      <div
        id="tasks-filter-group"
        data-filter-popover="group"
        transition:fly={{ y: -4, duration: motionDuration(100) }}
        onkeydown={handlePopoverKeydown}
        class="task-filter-menu absolute left-0 z-50 mt-1 max-h-64 min-w-40 overflow-y-auto py-1 custom-scrollbar"
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
              close(true)
            }}
            role="option"
            aria-selected={groupBy === opt.value}
            class="task-filter-option {groupBy === opt.value
              ? 'text-accent-primary-start'
              : 'text-text-primary'}"
          >
            <span class="material-symbols-outlined text-icon-sm"
              >{opt.icon}</span
            >
            <span>{opt.label}</span>
            {#if groupBy === opt.value}
              <span class="material-symbols-outlined text-icon-sm ml-auto"
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
      onclick={(e) => toggleChip('sort', e.currentTarget)}
      aria-expanded={openChip === 'sort'}
      aria-haspopup="listbox"
      aria-controls="tasks-filter-sort"
      data-active={openChip === 'sort' || sort !== 'manual'
        ? 'true'
        : undefined}
      class="task-filter-chip"
    >
      <span class="material-symbols-outlined text-icon-sm">sort</span>
      <span
        >Sort: {SORT_OPTIONS.find((o) => o.value === sort)?.label ??
          'Manual'}</span
      >
      <span class="material-symbols-outlined text-icon-xs">expand_more</span>
    </button>
    {#if openChip === 'sort'}
      <div
        id="tasks-filter-sort"
        data-filter-popover="sort"
        transition:fly={{ y: -4, duration: motionDuration(100) }}
        onkeydown={handlePopoverKeydown}
        class="task-filter-menu absolute left-0 z-50 mt-1 min-w-45 py-1"
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
              close(true)
            }}
            role="option"
            aria-selected={sort === opt.value}
            class="task-filter-option {sort === opt.value
              ? 'text-accent-primary-start'
              : 'text-text-primary'}"
          >
            <span class="material-symbols-outlined text-icon-sm"
              >{opt.icon}</span
            >
            <span>{opt.label}</span>
            {#if sort === opt.value}
              <span class="material-symbols-outlined text-icon-sm ml-auto"
                >check</span
              >
            {/if}
          </button>
        {/each}
      </div>
    {/if}
  </div>

  <div class="mx-1 hidden h-5 w-px bg-surface-panel-border sm:block"></div>
  <!-- Owner chip -->
  <div class="relative">
    <button
      type="button"
      onclick={(e) => toggleChip('owner', e.currentTarget)}
      aria-expanded={openChip === 'owner'}
      aria-haspopup="dialog"
      aria-controls="tasks-filter-owner"
      data-active={openChip === 'owner' || filters.owners.length > 0
        ? 'true'
        : undefined}
      class="task-filter-chip"
    >
      <span class="material-symbols-outlined text-icon-sm">person</span>
      <span
        >Owner{filters.owners.length ? ` (${filters.owners.length})` : ''}</span
      >
      <span class="material-symbols-outlined text-icon-xs">expand_more</span>
    </button>
    {#if openChip === 'owner'}
      <div
        id="tasks-filter-owner"
        data-filter-popover="owner"
        transition:fly={{ y: -4, duration: motionDuration(100) }}
        class="task-filter-menu absolute left-0 z-50 mt-1 max-h-64 min-w-45 overflow-y-auto py-1 custom-scrollbar"
        role="dialog"
        tabindex="-1"
        aria-label="Filter by owner"
        onkeydown={handlePopoverKeydown}
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
              class="w-full px-2 py-1 rounded border border-surface-popover-border bg-surface-panel text-text-primary placeholder:text-text-muted text-type-xs focus:outline-none focus:ring-1 focus:ring-accent-primary-start/40"
            />
          </div>
        {/if}
        <div role="group" aria-label="Owners" tabindex="-1">
          {#if owners.length === 0}
            <div class="px-3 py-2 text-type-xs text-text-muted font-label-sm">
              No owners
            </div>
          {:else if filteredOwners.length === 0}
            <div class="px-3 py-2 text-type-xs text-text-muted font-label-sm">
              No matches
            </div>
          {:else}
            {#each filteredOwners as o (o)}
              <label
                class="flex items-center gap-2 px-3 py-1.5 hover:bg-hover cursor-pointer text-type-sm font-label-sm text-text-primary"
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
      onclick={(e) => toggleChip('priority', e.currentTarget)}
      aria-expanded={openChip === 'priority'}
      aria-haspopup="dialog"
      aria-controls="tasks-filter-priority"
      data-active={openChip === 'priority' || filters.priorities.length > 0
        ? 'true'
        : undefined}
      class="task-filter-chip"
    >
      <span class="material-symbols-outlined text-icon-sm">flag</span>
      <span
        >Priority{filters.priorities.length
          ? ` (${filters.priorities.length})`
          : ''}</span
      >
      <span class="material-symbols-outlined text-icon-xs">expand_more</span>
    </button>
    {#if openChip === 'priority'}
      <div
        id="tasks-filter-priority"
        data-filter-popover="priority"
        transition:fly={{ y: -4, duration: motionDuration(100) }}
        onkeydown={handlePopoverKeydown}
        class="task-filter-menu absolute left-0 z-50 mt-1 min-w-40 py-1"
        role="dialog"
        tabindex="-1"
        aria-label="Filter by priority"
      >
        {#each PRIORITIES as p (p.value)}
          <label
            class="flex items-center gap-2 px-3 py-1.5 hover:bg-hover cursor-pointer text-type-sm font-label-sm text-text-primary"
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
      onclick={(e) => toggleChip('dueDate', e.currentTarget)}
      aria-expanded={openChip === 'dueDate'}
      aria-haspopup="listbox"
      aria-controls="tasks-filter-due-date"
      data-active={openChip === 'dueDate' || !!filters.dueDate
        ? 'true'
        : undefined}
      class="task-filter-chip"
    >
      <span class="material-symbols-outlined text-icon-sm">schedule</span>
      <span>{filters.dueDate ? dueLabel() : 'Due date'}</span>
      <span class="material-symbols-outlined text-icon-xs">expand_more</span>
    </button>
    {#if openChip === 'dueDate'}
      <div
        id="tasks-filter-due-date"
        data-filter-popover="dueDate"
        transition:fly={{ y: -4, duration: motionDuration(100) }}
        onkeydown={handlePopoverKeydown}
        class="task-filter-menu absolute left-0 z-50 mt-1 min-w-40 py-1"
        role="listbox"
        tabindex="-1"
        aria-label="Filter by due date"
      >
        {#each DUE_OPTIONS as opt (opt.value)}
          <button
            type="button"
            onclick={() => {
              setDueDate(opt.value)
              close(true)
            }}
            role="option"
            aria-selected={filters.dueDate === opt.value}
            class="task-filter-option {filters.dueDate === opt.value
              ? 'text-accent-primary-start'
              : 'text-text-primary'}"
          >
            <span>{opt.label}</span>
            {#if filters.dueDate === opt.value}
              <span class="material-symbols-outlined text-icon-sm ml-auto"
                >check</span
              >
            {/if}
          </button>
        {/each}
      </div>
    {/if}
  </div>

  <!-- Stale chip (#440): open tasks untouched for 30+ days -->
  <button
    type="button"
    data-testid="filter-stale-toggle"
    onclick={toggleStale}
    aria-pressed={!!filters.stale}
    title="Open tasks with no last-modified time, or last modified more than 30 days ago."
    data-active={filters.stale ? 'true' : undefined}
    class="task-filter-chip"
  >
    <span class="material-symbols-outlined text-icon-sm">history</span>
    <span>Stale (30d)</span>
  </button>

  <!-- Tags chip -->
  <div class="relative">
    <button
      type="button"
      onclick={(e) => toggleChip('tags', e.currentTarget)}
      aria-expanded={openChip === 'tags'}
      aria-haspopup="dialog"
      aria-controls="tasks-filter-tags"
      data-active={openChip === 'tags' || filters.tags.length > 0
        ? 'true'
        : undefined}
      class="task-filter-chip"
    >
      <span class="material-symbols-outlined text-icon-sm">label</span>
      <span>Tags{filters.tags.length ? ` (${filters.tags.length})` : ''}</span>
      <span class="material-symbols-outlined text-icon-xs">expand_more</span>
    </button>
    {#if openChip === 'tags'}
      <div
        id="tasks-filter-tags"
        data-filter-popover="tags"
        transition:fly={{ y: -4, duration: motionDuration(100) }}
        class="task-filter-menu absolute right-0 z-50 mt-1 max-h-64 min-w-50 overflow-y-auto py-1 custom-scrollbar sm:left-0 sm:right-auto"
        role="dialog"
        tabindex="-1"
        aria-label="Filter by tag"
        onkeydown={handlePopoverKeydown}
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
              class="w-full px-2 py-1 rounded border border-surface-popover-border bg-surface-panel text-text-primary placeholder:text-text-muted text-type-xs focus:outline-none focus:ring-1 focus:ring-accent-primary-start/40"
            />
          </div>
        {/if}
        <div role="group" aria-label="Tags" tabindex="-1">
          {#if tags.length === 0}
            <div class="px-3 py-2 text-type-xs text-text-muted font-label-sm">
              No tags
            </div>
          {:else if filteredTags.length === 0}
            <div class="px-3 py-2 text-type-xs text-text-muted font-label-sm">
              No matches
            </div>
          {:else}
            {#each filteredTags as t (t)}
              <label
                class="flex items-center gap-2 px-3 py-1.5 hover:bg-hover cursor-pointer text-type-sm font-label-sm text-text-primary"
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
      class="flex min-h-8 items-center gap-1 rounded-md border border-transparent px-2 text-type-sm font-label-sm text-text-muted transition-colors hover:border-error/20 hover:bg-error/10 hover:text-error focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-border-focus"
    >
      <span class="material-symbols-outlined text-icon-sm">close</span>
      <span>Clear all</span>
    </button>
  {/if}

  {#if activeCount > 0}
    <span class="hidden text-type-xs text-text-muted font-label-sm lg:inline">
      {activeCount} active filter{activeCount === 1 ? '' : 's'}
    </span>
  {/if}

  <div class="ml-auto flex min-h-8 items-center gap-2 pl-1">
    <span class="whitespace-nowrap text-text-muted text-type-xs font-label-sm">
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
        <span class="material-symbols-outlined text-icon-sm">my_location</span>
        <span class="font-label-sm">Follow</span>
      </button>
    {/if}
  </div>

  <!-- Click-away backdrop: closes whichever chip popover is open. -->
  {#if openChip}
    <div
      class="fixed inset-0 z-40 cursor-default border-none bg-transparent"
      role="presentation"
      aria-hidden="true"
      tabindex="-1"
      onclick={() => close(true)}
      data-testid="tasks-filter-backdrop"
    ></div>
  {/if}

  <!-- aria-live: announces active-filter count changes (AGENTS.md a11y). -->
  <div class="sr-only" aria-live="polite">{filterLiveMessage}</div>
</div>

<style>
  .tasks-filter-toolbar {
    background: color-mix(in srgb, var(--color-surface-app) 88%, transparent);
    backdrop-filter: blur(14px);
  }

  :global(.task-filter-chip) {
    display: flex;
    min-height: 2rem;
    align-items: center;
    gap: 0.375rem;
    border: 1px solid var(--color-surface-panel-border);
    border-radius: var(--radius-md);
    background: var(--color-surface-panel);
    padding: 0 0.625rem;
    color: var(--color-text-muted);
    font-family: var(--font-mono, var(--editor-mono-font-family));
    font-size: var(--text-type-sm);
    cursor: pointer;
    transition:
      border-color 150ms var(--transition-standard),
      background 150ms var(--transition-standard),
      color 150ms var(--transition-standard),
      transform 150ms var(--transition-standard);
  }

  :global(.task-filter-chip:hover) {
    border-color: var(--color-border-active);
    background: var(--color-hover);
    color: var(--color-text-primary);
  }

  :global(.task-filter-chip:active) {
    transform: translateY(1px);
  }

  :global(.task-filter-chip[data-active='true']) {
    border-color: color-mix(
      in srgb,
      var(--color-accent-primary-start) 45%,
      var(--color-surface-panel-border)
    );
    background: var(--color-accent-primary-glow);
    color: var(--color-accent-primary-start);
  }

  :global(.task-filter-chip:focus-visible) {
    outline: 2px solid var(--color-border-focus);
    outline-offset: 2px;
  }

  :global(.task-filter-menu) {
    border: 1px solid var(--color-surface-popover-border);
    border-radius: var(--radius-lg);
    background: var(--color-surface-popover);
    box-shadow: var(--shadow-lg);
  }

  :global(.task-filter-option) {
    display: flex;
    width: 100%;
    align-items: center;
    gap: 0.5rem;
    border: 0;
    background: transparent;
    padding: 0.5rem 0.75rem;
    text-align: left;
    font-family: var(--font-mono, var(--editor-mono-font-family));
    font-size: var(--text-type-sm);
    cursor: pointer;
    transition: background 120ms var(--transition-standard);
  }

  :global(.task-filter-option:hover),
  :global(.task-filter-option:focus-visible) {
    background: var(--color-hover);
    outline: none;
  }

  @media (prefers-reduced-transparency: reduce) {
    .tasks-filter-toolbar {
      background: var(--color-surface-app);
      backdrop-filter: none;
    }
  }

  @media (prefers-reduced-motion: reduce) {
    :global(.task-filter-chip) {
      transition: none;
    }
  }
</style>
