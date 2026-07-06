<script lang="ts">
  // Tasks hub shell (#424). One "Tasks" destination hosting a List / Board /
  // Calendar mode switcher over the unified hub state. The shell owns the
  // shared chrome — title + count header, mode segmented control, FilterBar,
  // scope breadcrumb — and routes to the active display-mode renderer in the
  // content area. Each renderer owns its own QuickAdd placement.
  //
  // Phase 1: List is the live renderer (views/ListView.svelte); Board and
  // Calendar render placeholder stubs until their issues land. Group-by /
  // sort selectors arrive with the grouping-engine issue (#423).
  import { onMount, untrack } from 'svelte'
  import type { PluginContext, PluginManifest } from '../../sdk'
  import FilterBar from './components/FilterBar.svelte'
  import ListView from './views/ListView.svelte'
  import BoardView from './views/BoardView.svelte'
  import CalendarView from './views/CalendarView.svelte'
  import {
    getTaskHubState,
    setDisplayMode,
    setFilters,
    setScope,
    setGroupBy,
    setSort,
    clearScopeOverride,
    narrowScopeTo,
    type DisplayMode,
    type GroupBy,
    type Scope,
    type SortMode,
    type TaskFilters
  } from './state.svelte'
  import {
    loadDefaultDisplayMode,
    loadDefaultGroupBy,
    loadDefaultSort,
    persistDefaultDisplayMode,
    persistDefaultGroupBy,
    persistDefaultSort
  } from './settings'

  interface Props {
    ctx: PluginContext
    manifest?: PluginManifest
    focusBlockId?: string
    focusKey?: string
  }

  let { ctx, manifest, focusBlockId = '', focusKey = '' }: Props = $props()

  const MODES: { value: DisplayMode; label: string; icon: string }[] = [
    { value: 'list', label: 'List', icon: 'checklist' },
    { value: 'board', label: 'Board', icon: 'view_kanban' },
    { value: 'calendar', label: 'Calendar', icon: 'calendar_month' }
  ]
  const SCOPES: Scope[] = ['vault', 'notebook', 'section', 'page']
  const GROUP_OPTIONS: { value: GroupBy; label: string }[] = [
    { value: 'none', label: 'None' },
    { value: 'status', label: 'Status' },
    { value: 'owner', label: 'Owner' },
    { value: 'priority', label: 'Priority' },
    { value: 'dueDate', label: 'Due date' },
    { value: 'tag', label: 'Tag' },
    { value: 'notebook', label: 'Notebook' },
    { value: 'section', label: 'Section' },
    { value: 'page', label: 'Page' }
  ]
  const SORT_OPTIONS: { value: SortMode; label: string }[] = [
    { value: 'manual', label: 'Manual' },
    { value: 'dueDate', label: 'Due date' },
    { value: 'priority', label: 'Priority' },
    { value: 'title', label: 'Title' },
    { value: 'created', label: 'Created' },
    { value: 'owner', label: 'Owner' }
  ]

  let hubState = $derived(getTaskHubState())

  // Counts reported upward by the active renderer (List today).
  let openCount = $state(0)
  let doneCount = $state(0)
  function handleCountChange(open: number, done: number) {
    openCount = open
    doneCount = done
  }

  // --- Display mode ------------------------------------------------------
  // Hydrate from the persisted default once on mount; afterwards every user
  // switch is persisted. untrack so the initial set doesn't loop through the
  // $derived that reads hubState.displayMode.
  onMount(() => {
    untrack(() => {
      const persisted = loadDefaultDisplayMode()
      if (persisted !== getTaskHubState().displayMode) {
        setDisplayMode(persisted)
      }
      // Group-by + sort hydrate the same way (#423). Independent of display
      // mode so the user's preferred grouping survives a List → Board hop.
      const persistedGroup = loadDefaultGroupBy()
      if (persistedGroup !== getTaskHubState().groupBy) {
        setGroupBy(persistedGroup)
      }
      const persistedSort = loadDefaultSort()
      if (persistedSort !== getTaskHubState().sort) {
        setSort(persistedSort)
      }
    })
    void reloadFacets()
  })

  function chooseMode(mode: DisplayMode) {
    setDisplayMode(mode)
    void persistDefaultDisplayMode(mode)
  }

  function chooseGroupBy(g: GroupBy) {
    setGroupBy(g)
    void persistDefaultGroupBy(g)
  }

  function chooseSort(s: SortMode) {
    setSort(s)
    void persistDefaultSort(s)
  }

  // Ctrl+Shift+V cycles List → Board → Calendar → List.
  function onGlobalKeydown(e: KeyboardEvent) {
    if (!(e.ctrlKey && e.shiftKey) || e.key !== 'V') return
    e.preventDefault()
    const order: DisplayMode[] = ['list', 'board', 'calendar']
    const idx = order.indexOf(getTaskHubState().displayMode)
    chooseMode(order[(idx + 1) % order.length])
  }
  $effect(() => {
    window.addEventListener('keydown', onGlobalKeydown)
    return () => window.removeEventListener('keydown', onGlobalKeydown)
  })

  // WAI-ARIA radiogroup keyboard pattern for the mode switcher: ArrowLeft/
  // Right move between options (wrapping), Home/End jump to the boundaries.
  // Roving tabindex — the checked option is tabbable (0), others are not (-1).
  function onModeKeydown(e: KeyboardEvent) {
    if (!['ArrowRight', 'ArrowLeft', 'Home', 'End'].includes(e.key)) return
    e.preventDefault()
    const dir = e.key === 'ArrowLeft' || e.key === 'End' ? -1 : 1
    let start: number
    if (e.key === 'Home') start = 0
    else if (e.key === 'End') start = MODES.length - 1
    else
      start =
        (MODES.findIndex((m) => m.value === getTaskHubState().displayMode) +
          dir +
          MODES.length) %
        MODES.length
    for (let i = 0; i < MODES.length; i++) {
      const next =
        (((start + i * dir) % MODES.length) + MODES.length) % MODES.length
      if (next !== start || i === 0) {
        chooseMode(MODES[next].value)
        ;(e.currentTarget as HTMLElement)
          .querySelector<HTMLElement>(`[data-mode="${MODES[next].value}"]`)
          ?.focus()
        return
      }
    }
  }

  // --- Scope breadcrumb --------------------------------------------------
  function defaultScope(): Scope {
    if (ctx.activePage) return 'page'
    if (ctx.activeSection) return 'section'
    if (ctx.activeNotebook) return 'notebook'
    return 'vault'
  }
  function isScopeDisabled(s: string): boolean {
    if (s === 'notebook') return !ctx.activeNotebook
    if (s === 'section') return !ctx.activeSection
    if (s === 'page') return !ctx.activePage
    return false
  }
  function onScopeKeydown(e: KeyboardEvent) {
    if (!['ArrowRight', 'ArrowLeft', 'Home', 'End'].includes(e.key)) return
    e.preventDefault()
    const dir = e.key === 'ArrowLeft' || e.key === 'End' ? -1 : 1
    let start: number
    if (e.key === 'Home') start = 0
    else if (e.key === 'End') start = SCOPES.length - 1
    else
      start =
        (SCOPES.indexOf(getTaskHubState().scope) + dir + SCOPES.length) %
        SCOPES.length
    for (let i = 0; i < SCOPES.length; i++) {
      const next =
        (((start + i * dir) % SCOPES.length) + SCOPES.length) % SCOPES.length
      if (!isScopeDisabled(SCOPES[next])) {
        setScope(SCOPES[next])
        ;(e.currentTarget as HTMLElement)
          .querySelector<HTMLElement>(`[data-scope="${SCOPES[next]}"]`)
          ?.focus()
        return
      }
    }
  }
  function resetScopeToContext() {
    clearScopeOverride()
    untrack(() => setScope(defaultScope()))
  }
  // Navigation auto-narrow (#124) — a no-op once the user picks a scope.
  $effect(() => {
    // Track the active nav triple so the effect re-runs on navigation.
    void ctx.activeNotebook
    void ctx.activeSection
    void ctx.activePage
    narrowScopeTo(defaultScope())
  })
  let scopeCrumb = $derived.by(() => {
    const s = getTaskHubState().scope
    if (s === 'vault') return 'Vault'
    if (s === 'notebook')
      return ctx.activeNotebook
        ? `Notebook · ${ctx.activeNotebook}`
        : 'Notebook'
    if (s === 'section')
      return ctx.activeSection ? `Section · ${ctx.activeSection}` : 'Section'
    return ctx.activePage ? `Page · ${ctx.activePage}` : 'Page'
  })

  // --- Filter facets (owners + tags universe for the shared FilterBar) ---
  // Lightweight DISTINCT queries; refreshed on block:changed. Kept in the
  // shell so every display mode sees the same filter universe.
  let allOwners = $state<string[]>([])
  let allTags = $state<string[]>([])
  let facetTimer: ReturnType<typeof setTimeout> | null = null
  async function reloadFacets() {
    try {
      const [ownerRes, tagRes] = await Promise.all([
        ctx.sqliteQuery(
          `SELECT DISTINCT owner FROM tasks WHERE owner IS NOT NULL AND owner != '' ORDER BY owner ASC LIMIT 200`
        ),
        ctx.sqliteQuery(
          `SELECT DISTINCT level_0 FROM tags ORDER BY level_0 ASC LIMIT 200`
        )
      ])
      allOwners = (
        (ownerRes.rows as unknown as Array<{ owner: string }>) ?? []
      ).map((r) => r.owner)
      allTags = (
        (tagRes.rows as unknown as Array<{ level_0: string }>) ?? []
      ).map((r) => r.level_0)
    } catch {
      // Facets are best-effort; the FilterBar simply shows an empty universe
      // if the query fails (e.g. before a vault is fully indexed).
    }
  }
  $effect(() => {
    const off = ctx.on('block:changed', () => {
      if (facetTimer) clearTimeout(facetTimer)
      facetTimer = setTimeout(() => {
        void reloadFacets()
      }, 200)
    })
    return () => {
      if (facetTimer) clearTimeout(facetTimer)
      off()
    }
  })

  function handleFiltersChange(f: TaskFilters) {
    setFilters(f)
  }

  let totalCount = $derived(openCount + doneCount)
</script>

<div class="flex-1 flex flex-col min-h-0 overflow-hidden" data-tasks-hub>
  <header
    class="px-6 py-3 border-b border-surface-panel-border flex items-center gap-3 flex-wrap"
  >
    <span class="material-symbols-outlined text-accent-primary-start"
      >checklist</span
    >
    <h1
      class="font-headline-lg text-headline-lg text-text-primary flex items-baseline gap-2"
    >
      {manifest?.name ?? 'Tasks'}
      <span
        class="text-text-muted text-[12px] font-body-md normal-case font-normal ml-1"
        aria-live="polite"
        data-testid="tasks-hub-count"
      >
        {openCount} active{openCount === 1 ? '' : 's'}{doneCount > 0
          ? ` · ${doneCount} done`
          : ''}
      </span>
    </h1>

    <!-- View-mode segmented control (WAI-ARIA radiogroup, roving tabindex). -->
    <div
      class="ml-auto flex items-center gap-1 p-1 rounded-lg border border-surface-panel-border bg-surface-panel"
      role="radiogroup"
      aria-label="Tasks display mode"
      tabindex="-1"
      data-testid="tasks-hub-mode-switcher"
      onkeydown={onModeKeydown}
    >
      {#each MODES as m (m.value)}
        <button
          type="button"
          role="radio"
          aria-checked={hubState.displayMode === m.value}
          aria-label={`${m.label} mode`}
          tabindex={hubState.displayMode === m.value ? 0 : -1}
          data-mode={m.value}
          data-testid={`tasks-hub-mode-${m.value}`}
          onclick={() => chooseMode(m.value)}
          title={`${m.label} mode (Ctrl+Shift+V)`}
          class="flex items-center gap-1.5 px-2.5 py-1 rounded text-[12px] font-label-sm transition-colors border-none cursor-pointer {hubState.displayMode ===
          m.value
            ? 'bg-accent-primary-start text-text-primary'
            : 'bg-transparent text-text-muted hover:bg-hover hover:text-text-primary'}"
        >
          <span class="material-symbols-outlined text-[14px]" aria-hidden="true"
            >{m.icon}</span
          >
          <span aria-hidden="true">{m.label}</span>
        </button>
      {/each}
    </div>
  </header>

  <!-- Group-by + Sort selectors (#423). Native selects for accessibility
       (keyboard-operable, option list exposed to AT). Both persist on change. -->
  <div
    class="px-6 py-1.5 border-b border-surface-panel-border flex items-center gap-3 flex-wrap"
  >
    <label
      class="flex items-center gap-1.5 text-[12px] font-label-sm text-text-muted"
    >
      <span class="material-symbols-outlined text-[14px]" aria-hidden="true"
        >view_module</span
      >
      <span>Group by</span>
      <select
        data-testid="tasks-hub-group-by"
        aria-label="Group tasks by"
        value={hubState.groupBy}
        onchange={(e) => chooseGroupBy(e.currentTarget.value as GroupBy)}
        class="bg-surface-panel border border-surface-panel-border rounded px-1.5 py-0.5 text-[12px] font-label-sm text-text-primary cursor-pointer focus-visible:outline focus-visible:outline-2 focus-visible:outline-accent-primary-start"
      >
        {#each GROUP_OPTIONS as opt (opt.value)}
          <option value={opt.value} selected={hubState.groupBy === opt.value}
            >{opt.label}</option
          >
        {/each}
      </select>
    </label>
    <label
      class="flex items-center gap-1.5 text-[12px] font-label-sm text-text-muted"
    >
      <span class="material-symbols-outlined text-[14px]" aria-hidden="true"
        >sort</span
      >
      <span>Sort</span>
      <select
        data-testid="tasks-hub-sort"
        aria-label="Sort tasks by"
        value={hubState.sort}
        onchange={(e) => chooseSort(e.currentTarget.value as SortMode)}
        class="bg-surface-panel border border-surface-panel-border rounded px-1.5 py-0.5 text-[12px] font-label-sm text-text-primary cursor-pointer focus-visible:outline focus-visible:outline-2 focus-visible:outline-accent-primary-start"
      >
        {#each SORT_OPTIONS as opt (opt.value)}
          <option value={opt.value} selected={hubState.sort === opt.value}
            >{opt.label}</option
          >
        {/each}
      </select>
    </label>
  </div>

  <!-- Scope breadcrumb + follow toggle (shared across modes). -->
  <div
    class="px-6 py-1.5 border-b border-surface-panel-border flex items-center gap-2 flex-wrap"
    role="radiogroup"
    aria-label="Tasks scope"
    tabindex="-1"
    onkeydown={onScopeKeydown}
  >
    {#each SCOPES as s (s)}
      <button
        type="button"
        role="radio"
        aria-checked={hubState.scope === s}
        tabindex={hubState.scope === s ? 0 : -1}
        data-scope={s}
        disabled={isScopeDisabled(s)}
        title={isScopeDisabled(s) ? `Select a ${s} first` : undefined}
        onclick={() => setScope(s)}
        class="px-2.5 py-0.5 rounded font-label-sm text-[12px] border-none cursor-pointer transition-colors disabled:opacity-40 disabled:cursor-not-allowed"
        class:bg-hover={hubState.scope === s}
        class:text-accent-primary-start={hubState.scope === s}
        class:text-text-muted={hubState.scope !== s}
      >
        {s === 'vault' ? 'Vault' : s[0].toUpperCase() + s.slice(1)}
      </button>
    {/each}
    <span
      class="text-text-muted text-[12px] font-body-md ml-auto flex items-center gap-2"
    >
      <span>{scopeCrumb} · {totalCount} task{totalCount === 1 ? '' : 's'}</span>
      {#if hubState.scopeUserOverride}
        <button
          type="button"
          onclick={resetScopeToContext}
          aria-label="Reset Tasks scope to follow navigation"
          title="Follow navigation"
          class="flex items-center gap-1 px-1.5 py-0.5 rounded border border-surface-panel-border text-text-muted hover:text-accent-primary-start hover:border-accent-primary-start/40 transition-colors"
        >
          <span class="material-symbols-outlined text-[14px]">my_location</span>
          <span class="font-label-sm">Follow</span>
        </button>
      {/if}
    </span>
  </div>

  <FilterBar
    filters={hubState.filters}
    owners={allOwners}
    tags={allTags}
    onFiltersChange={handleFiltersChange}
  />

  <div class="flex-1 flex flex-col min-h-0 overflow-hidden">
    {#if hubState.displayMode === 'list'}
      <ListView
        {ctx}
        {focusBlockId}
        {focusKey}
        onCountChange={handleCountChange}
      />
    {:else if hubState.displayMode === 'board'}
      <BoardView {ctx} onCountChange={handleCountChange} />
    {:else}
      <CalendarView />
    {/if}
  </div>
</div>
