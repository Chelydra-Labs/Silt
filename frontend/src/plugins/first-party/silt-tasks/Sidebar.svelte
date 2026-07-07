<script lang="ts">
  // Unified Tasks sidebar (#432). Fuses the four task-navigation surfaces
  // that previously lived in two separate plugins into a single sidebar
  // for the silt-tasks hub:
  //
  //   1. Smart Lists — Today/Upcoming/Overdue/Completed/All with live
  //      counts (lifted from CalendarSidebar, #322).
  //   2. Saved Views — system + user views with fingerprint highlight
  //      (lifted from KanbanSidebar, #323).
  //   3. Jump-to-Date mini-cal — per-day dots + click-to-focus (lifted
  //      from CalendarSidebar).
  //   4. Active Filters — owner/priority/due/tag checkboxes (lifted from
  //      KanbanSidebar).
  //
  // All four sections read+write the unified hub state (state.svelte.ts)
  // so a tweak here is instantly reflected in the header FilterBar and
  // every display-mode renderer.
  import { onMount } from 'svelte'
  import type { PluginContext, PluginManifest } from '../../sdk'
  import { plusDaysISO, localToday } from '../../sdk'
  import { PRIORITY_LABELS } from './types'
  import {
    getTaskHubState,
    setActiveFilter,
    clearActiveFilter,
    setFocusDate,
    clearFocusDate,
    setFilters,
    clearFilters,
    applySavedView,
    deleteSavedView,
    type SavedView,
    type TaskFilters,
    type DueDateFilter,
    type CalendarFilter
  } from './state.svelte'
  import { viewMatchesState } from './savedViews'
  import { persistSavedViews } from './settings'

  interface Props {
    ctx: PluginContext
    manifest?: PluginManifest
  }

  let { ctx, manifest }: Props = $props()

  let hubState = $derived(getTaskHubState())
  let liveFilters = $derived(hubState.filters)
  let activeFilter = $derived(hubState.activeFilter)
  let activeFocusDate = $derived(hubState.focusDate)

  interface Counts {
    today: number
    upcoming: number
    overdue: number
    completed: number
    all: number
  }

  let counts = $state<Counts>({
    today: 0,
    upcoming: 0,
    overdue: 0,
    completed: 0,
    all: 0
  })
  let byDate = $state<Record<string, number>>({})
  // Owner/tag facet universes — queried directly by this sidebar (mirrors
  // TasksHub's reloadFacets) so the filter checkboxes have an option list
  // without depending on the hub to bridge them across.
  let owners = $state<string[]>([])
  let tags = $state<string[]>([])
  let loading = $state(true)
  let errorMsg = $state('')

  // Mini-calendar cursor (independent of the main view's cursor). Anchored
  // to ctx.today so the visible month tracks the same "today" the smart-
  // list counts do — not wall-clock new Date() which can disagree near a
  // month boundary when ctx.today is injected.
  function miniCursorFromToday(): Date {
    const iso = ctx.today || localToday()
    const [y, m, d] = iso.split('-').map(Number)
    return new Date(y ?? 1970, (m ?? 1) - 1, d ?? 1)
  }
  let miniCursor = $state(miniCursorFromToday())

  // Roving tabindex for the smart-list and mini-cal-day keyboard nav.
  let listFocusIdx = $state(0)
  let miniFocusIdx = $state(0)

  async function reloadCounts(): Promise<void> {
    const today = ctx.today || localToday()
    const tomorrow = plusDaysISO(today, 1)
    const weekAhead = plusDaysISO(today, 7)
    // `AS "all"` is double-quoted because ALL is a SQLite keyword
    // (UNION ALL / SELECT ALL); a bare `AS all` is a syntax error.
    const res = await ctx.sqliteQuery(
      `SELECT
          SUM(CASE WHEN t.status != 'DONE' AND t.due_date = ? THEN 1 ELSE 0 END) AS today,
          SUM(CASE WHEN t.status != 'DONE' AND t.due_date >= ? AND t.due_date <= ? THEN 1 ELSE 0 END) AS upcoming,
          SUM(CASE WHEN t.status != 'DONE' AND t.due_date < ? THEN 1 ELSE 0 END) AS overdue,
          SUM(CASE WHEN t.status = 'DONE' AND t.due_date = ? THEN 1 ELSE 0 END) AS completed,
          SUM(CASE WHEN t.status != 'DONE' THEN 1 ELSE 0 END) AS "all"
       FROM blocks b JOIN tasks t ON b.id = t.block_id`,
      [today, tomorrow, weekAhead, today, today]
    )
    const row = (res.rows?.[0] ?? {}) as Record<string, unknown>
    counts = {
      today: Number(row.today ?? 0),
      upcoming: Number(row.upcoming ?? 0),
      overdue: Number(row.overdue ?? 0),
      completed: Number(row.completed ?? 0),
      all: Number(row.all ?? 0)
    }
  }

  async function reloadDayDots(): Promise<void> {
    const first = ymd(firstOfMonth(miniCursor))
    const last = ymd(lastOfMonth(miniCursor))
    const dayRes = await ctx.sqliteQuery(
      `SELECT t.due_date AS d, COUNT(*) AS c
       FROM blocks b JOIN tasks t ON b.id = t.block_id
       WHERE t.status != 'DONE'
         AND t.due_date IS NOT NULL AND t.due_date != ''
         AND t.due_date >= ? AND t.due_date <= ?
       GROUP BY t.due_date`,
      [first, last]
    )
    const bucket: Record<string, number> = {}
    for (const r of dayRes.rows as unknown as Array<{
      d: string
      c: number
    }>) {
      if (r.d) bucket[r.d] = r.c
    }
    byDate = bucket
  }

  async function reloadFacets(): Promise<void> {
    const [ownerRes, tagRes] = await Promise.all([
      ctx.sqliteQuery(
        `SELECT DISTINCT owner FROM tasks WHERE owner IS NOT NULL AND owner != '' ORDER BY owner ASC LIMIT 200`
      ),
      ctx.sqliteQuery(
        `SELECT DISTINCT level_0 FROM tags ORDER BY level_0 ASC LIMIT 200`
      )
    ])
    owners = ((ownerRes.rows as unknown as Array<{ owner: string }>) ?? []).map(
      (r) => r.owner
    )
    tags = ((tagRes.rows as unknown as Array<{ level_0: string }>) ?? []).map(
      (r) => r.level_0
    )
  }

  async function reload(): Promise<void> {
    loading = true
    errorMsg = ''
    // Counts + day-dots fail together (sidebar is unuseable without them);
    // facets are best-effort so a tags-table hiccup doesn't break the
    // smart-list counts.
    try {
      await Promise.all([reloadCounts(), reloadDayDots()])
    } catch (e) {
      errorMsg = e instanceof Error ? e.message : String(e)
    } finally {
      loading = false
    }
    try {
      await reloadFacets()
    } catch {
      // Facets are best-effort; leave the universes at their last value.
    }
  }

  // Refresh strategy mirrors CalendarSidebar: block:changed (debounced
  // like TasksHub's reloadFacets), refresh-navigation, and a 60s nowTick
  // so a long-mounted sidebar re-buckets counts at midnight.
  let blockTimer: ReturnType<typeof setTimeout> | null = null
  let offBlock: (() => void) | undefined
  let nowTick = $state(0)
  let nowInterval: ReturnType<typeof setInterval> | undefined
  onMount(() => {
    offBlock = ctx.on('block:changed', () => {
      if (blockTimer) clearTimeout(blockTimer)
      blockTimer = setTimeout(() => {
        void reload()
      }, 200)
    })
    const onRefresh = () => {
      void reload()
    }
    window.addEventListener('refresh-navigation', onRefresh)
    nowInterval = setInterval(() => {
      nowTick++
    }, 60_000)
    return () => {
      window.removeEventListener('refresh-navigation', onRefresh)
      if (blockTimer) clearTimeout(blockTimer)
      offBlock?.()
      if (nowInterval) clearInterval(nowInterval)
    }
  })

  // Re-run reload() only when the local-day or mini-cal month actually
  // changes — a bare nowTick with no day change is a no-op (mirrors
  // CalendarSidebar's gating effect).
  let lastSeenToday = ''
  let lastSeenMiniKey = ''
  $effect(() => {
    void nowTick
    const t = ctx.today
    const miniKey = `${miniCursor.getFullYear()}-${miniCursor.getMonth()}`
    if (t === lastSeenToday && miniKey === lastSeenMiniKey) return
    lastSeenToday = t
    lastSeenMiniKey = miniKey
    void reload()
  })

  // When the user picks a day outside the visible month (via the main
  // view dispatching calendar:focus-date), keep the picked cell visible.
  $effect(() => {
    const fd = activeFocusDate
    if (!fd) return
    const [y, m] = fd.split('-').map(Number)
    if (!y || !m) return
    if (miniCursor.getFullYear() === y && miniCursor.getMonth() === m - 1)
      return
    miniCursor = new Date(y, m - 1, 1)
  })

  // --- Date helpers (mirror Calendar.svelte's local helpers) --------------

  const MONTHS = [
    'January',
    'February',
    'March',
    'April',
    'May',
    'June',
    'July',
    'August',
    'September',
    'October',
    'November',
    'December'
  ]
  const DOW = ['S', 'M', 'T', 'W', 'T', 'F', 'S']

  function ymd(d: Date): string {
    return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(
      d.getDate()
    ).padStart(2, '0')}`
  }
  function firstOfMonth(d: Date): Date {
    return new Date(d.getFullYear(), d.getMonth(), 1)
  }
  function lastOfMonth(d: Date): Date {
    return new Date(d.getFullYear(), d.getMonth() + 1, 0)
  }
  function startOfWeek(d: Date): Date {
    const x = new Date(d)
    x.setDate(x.getDate() - x.getDay())
    x.setHours(0, 0, 0, 0)
    return x
  }
  function addDays(d: Date, n: number): Date {
    const x = new Date(d)
    x.setDate(x.getDate() + n)
    return x
  }

  let miniWeeks = $derived.by(() => {
    const first = startOfWeek(firstOfMonth(miniCursor))
    const last = lastOfMonth(miniCursor)
    const weeks: Date[][] = []
    let cur = first
    for (let w = 0; w < 6; w++) {
      const row: Date[] = []
      for (let i = 0; i < 7; i++) {
        row.push(cur)
        cur = addDays(cur, 1)
      }
      weeks.push(row)
      if (cur > last && w >= 3) break
    }
    return weeks
  })

  function prevMonth() {
    miniCursor = new Date(
      miniCursor.getFullYear(),
      miniCursor.getMonth() - 1,
      1
    )
  }
  function nextMonth() {
    miniCursor = new Date(
      miniCursor.getFullYear(),
      miniCursor.getMonth() + 1,
      1
    )
  }
  function pickDay(d: Date) {
    setFocusDate(ymd(d))
  }
  function goMiniToday() {
    miniCursor = miniCursorFromToday()
    clearFocusDate()
  }

  // --- Smart-list keyboard nav -------------------------------------------

  const smartLists = [
    { id: 'today', label: 'Today' },
    { id: 'upcoming', label: 'Upcoming' },
    { id: 'overdue', label: 'Overdue' },
    { id: 'completed', label: 'Completed' },
    { id: 'all', label: 'All Tasks' }
  ] as const

  function activateList(id: CalendarFilter) {
    if (id === 'all') {
      clearActiveFilter()
    } else {
      setActiveFilter(id)
    }
  }

  function onListKeydown(e: KeyboardEvent) {
    const max = smartLists.length - 1
    let nextIdx = listFocusIdx
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      nextIdx = Math.min(max, listFocusIdx + 1)
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      nextIdx = Math.max(0, listFocusIdx - 1)
    } else if (e.key === 'Home') {
      e.preventDefault()
      nextIdx = 0
    } else if (e.key === 'End') {
      e.preventDefault()
      nextIdx = max
    } else if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault()
      const item = smartLists[listFocusIdx]
      if (item) activateList(item.id)
      return
    } else {
      return
    }
    listFocusIdx = nextIdx
    const next = smartLists[nextIdx]
    if (next) {
      document.querySelector<HTMLElement>(`[data-testid="${next.id}"]`)?.focus()
    }
  }

  function onDayKeydown(e: KeyboardEvent, flatIdx: number) {
    const total = miniWeeks.flat().length
    let next = flatIdx
    let handled = true
    if (e.key === 'ArrowRight') next = flatIdx + 1
    else if (e.key === 'ArrowLeft') next = flatIdx - 1
    else if (e.key === 'ArrowDown') next = flatIdx + 7
    else if (e.key === 'ArrowUp') next = flatIdx - 7
    else if (e.key === 'Home') next = flatIdx - (flatIdx % 7)
    else if (e.key === 'End') next = flatIdx + (6 - (flatIdx % 7))
    else if (e.key === 'Enter' || e.key === ' ') {
      const day = miniWeeks.flat()[flatIdx]
      if (day) pickDay(day)
      handled = true
    } else handled = false
    if (handled) {
      e.preventDefault()
      miniFocusIdx = Math.max(0, Math.min(total - 1, next))
      const el = document.querySelector<HTMLElement>(
        `[data-mini-day="${miniFocusIdx}"]`
      )
      el?.focus()
    }
  }

  // --- Saved-view actions ------------------------------------------------

  function activateView(view: SavedView) {
    applySavedView(view)
  }

  async function deleteView(view: SavedView) {
    if (view.system) return
    if (!window.confirm(`Delete saved view "${view.name}"?`)) return
    errorMsg = ''
    deleteSavedView(view.id)
    try {
      await persistSavedViews(getTaskHubState().savedViews)
    } catch (e) {
      errorMsg = e instanceof Error ? e.message : String(e)
    }
  }

  // --- Filter quick-toggles (mirror KanbanSidebar bidirectional sync) ----

  function toggleOwner(o: string) {
    const has = liveFilters.owners.includes(o)
    setFilters({
      ...liveFilters,
      owners: has
        ? liveFilters.owners.filter((x) => x !== o)
        : [...liveFilters.owners, o]
    })
  }

  function togglePriority(p: number) {
    const has = liveFilters.priorities.includes(p)
    setFilters({
      ...liveFilters,
      priorities: has
        ? liveFilters.priorities.filter((x) => x !== p)
        : [...liveFilters.priorities, p]
    })
  }

  function setDueDateChip(d: DueDateFilter) {
    setFilters({ ...liveFilters, dueDate: d })
  }

  function toggleTag(t: string) {
    const has = liveFilters.tags.includes(t)
    setFilters({
      ...liveFilters,
      tags: has
        ? liveFilters.tags.filter((x) => x !== t)
        : [...liveFilters.tags, t]
    })
  }

  // aria-live region announces count + filter changes.
  let liveMessage = $state('')
  let lastMsgJson = ''
  $effect(() => {
    const j = JSON.stringify({
      c: counts,
      f: liveFilters,
      v: hubState.savedViews.length
    })
    if (j !== lastMsgJson) {
      lastMsgJson = j
      const f = liveFilters
      liveMessage = `Counts: ${counts.today} today, ${counts.upcoming} upcoming, ${counts.overdue} overdue, ${counts.completed} completed, ${counts.all} total. Active filters: ${
        f.owners.length +
        f.priorities.length +
        (f.dueDate ? 1 : 0) +
        f.tags.length
      }. ${hubState.savedViews.length} saved views.`
    }
  })

  const DUE_OPTIONS: { v: DueDateFilter; l: string; testid: string }[] = [
    { v: '', l: 'All', testid: 'due-all' },
    { v: 'overdue', l: 'Overdue', testid: 'due-overdue' },
    { v: 'today', l: 'Today', testid: 'due-today' },
    { v: 'week', l: 'This Week', testid: 'due-week' },
    { v: 'none', l: 'No Date', testid: 'due-none' }
  ]
</script>

<aside
  class="flex flex-col gap-4 px-3 py-3"
  aria-label="Tasks sidebar"
  data-test-tasks-sidebar
>
  <!-- Smart Lists (lifted from CalendarSidebar) -->
  <section aria-labelledby="tasks-smart-lists-heading">
    <h3
      id="tasks-smart-lists-heading"
      class="px-2 font-label-sm-bold uppercase tracking-widest text-[10px] text-text-muted"
    >
      Smart Lists
    </h3>
    {#if counts.all > 0 || counts.completed > 0}
      <ul role="listbox" aria-label="Smart lists" class="mt-1 space-y-0.5">
        {#each smartLists as item, i (item.id)}
          {@const selected = activeFilter === item.id}
          <li>
            <button
              type="button"
              role="option"
              aria-selected={selected}
              tabindex={i === listFocusIdx ? 0 : -1}
              data-testid={item.id}
              onclick={() => {
                listFocusIdx = i
                activateList(item.id)
              }}
              onkeydown={onListKeydown}
              onfocus={() => (listFocusIdx = i)}
              class="w-full flex items-center gap-2 px-2 py-1.5 rounded text-left text-[12px] font-body-md cursor-pointer border-none bg-transparent transition-colors
              {selected
                ? 'bg-accent-primary-glow text-accent-primary-start'
                : 'text-text-primary hover:bg-hover'}"
            >
              <span
                class="material-symbols-outlined text-[14px]"
                class:text-error={item.id === 'overdue'}
                class:text-accent-primary-start={item.id !== 'overdue'}
              >
                {item.id === 'today'
                  ? 'today'
                  : item.id === 'upcoming'
                    ? 'event_upcoming'
                    : item.id === 'overdue'
                      ? 'error'
                      : item.id === 'completed'
                        ? 'check_circle'
                        : 'list_alt'}
              </span>
              <span class="flex-1 truncate">{item.label}</span>
              <span
                class="text-[10px] text-text-muted bg-surface-popover px-1.5 py-0.5 rounded-sm font-label-sm"
                aria-label="{counts[item.id as keyof Counts]} tasks"
                data-testid={`count-${item.id}`}
              >
                {counts[item.id as keyof Counts]}
              </span>
            </button>
          </li>
        {/each}
      </ul>
    {:else}
      <p
        class="mt-1 px-2 py-2 text-[11px] font-body-md text-text-muted"
        data-testid="calendar-empty-state"
      >
        No active tasks — set a due date on a task to populate this view.
      </p>
    {/if}
    {#if activeFilter !== 'all'}
      <button
        type="button"
        onclick={() => clearActiveFilter()}
        data-testid="clear-filter"
        class="mt-1 w-full flex items-center justify-center gap-1 px-2 py-1 rounded text-[11px] font-label-sm text-text-muted hover:text-error cursor-pointer border border-dashed border-surface-popover-border bg-transparent transition-colors"
      >
        <span class="material-symbols-outlined text-[12px]">close</span>
        Clear filter
      </button>
    {/if}
  </section>

  <!-- Saved Views (lifted from KanbanSidebar) -->
  <section aria-labelledby="tasks-saved-views-heading">
    <h3
      id="tasks-saved-views-heading"
      class="px-2 font-label-sm-bold uppercase tracking-widest text-[10px] text-text-muted"
    >
      Saved Views
    </h3>
    <ul role="list" class="mt-1 space-y-0.5">
      {#each hubState.savedViews as view (view.id)}
        {@const isActive = viewMatchesState(view, hubState)}
        <li>
          <div
            class="flex items-center gap-1 px-1 py-0.5 rounded text-[12px] font-body-md border border-transparent
              {isActive
              ? 'bg-accent-primary-glow border-accent-primary-start/30 text-accent-primary-start'
              : 'text-text-primary hover:bg-hover border-transparent'}"
            data-testid={`view-${view.id}`}
          >
            <button
              type="button"
              onclick={() => activateView(view)}
              aria-pressed={isActive}
              class="flex-1 text-left px-1.5 py-1 rounded cursor-pointer border-none bg-transparent"
            >
              {view.name}
              {#if hubState.activeSavedViewId === view.id && hubState.savedViewsDirty}
                <span class="text-text-muted text-[10px]"> (modified)</span>
              {/if}
            </button>
            <button
              type="button"
              onclick={() => void deleteView(view)}
              disabled={view.system === true}
              aria-disabled={view.system === true}
              aria-hidden={view.system === true}
              aria-label="Delete view {view.name}"
              data-testid={`delete-view-${view.id}`}
              class="p-1 rounded text-text-muted hover:text-error border-none bg-transparent cursor-pointer
                {view.system === true ? 'hidden' : ''}"
            >
              <span class="material-symbols-outlined text-[12px]">delete</span>
            </button>
          </div>
        </li>
      {/each}
    </ul>
  </section>

  <!-- Jump-to-Date mini-cal (lifted from CalendarSidebar) -->
  <section aria-labelledby="tasks-mini-heading">
    <h3
      id="tasks-mini-heading"
      class="px-2 font-label-sm-bold uppercase tracking-widest text-[10px] text-text-muted"
    >
      Jump to Date
    </h3>
    <div class="mt-1 px-2">
      <div class="flex items-center justify-between mb-1">
        <button
          type="button"
          onclick={prevMonth}
          aria-label="Previous month"
          class="p-1 rounded hover:bg-hover text-text-muted hover:text-accent-primary-start border-none bg-transparent cursor-pointer"
        >
          <span class="material-symbols-outlined text-[14px]">chevron_left</span
          >
        </button>
        <span class="text-text-primary text-[11px] font-label-sm-bold">
          {MONTHS[miniCursor.getMonth()]}
          {miniCursor.getFullYear()}
        </span>
        <button
          type="button"
          onclick={nextMonth}
          aria-label="Next month"
          class="p-1 rounded hover:bg-hover text-text-muted hover:text-accent-primary-start border-none bg-transparent cursor-pointer"
        >
          <span class="material-symbols-outlined text-[14px]"
            >chevron_right</span
          >
        </button>
      </div>
      <div class="flex justify-end mb-1">
        <button
          type="button"
          onclick={goMiniToday}
          aria-label="Jump mini-calendar to today"
          data-testid="mini-today"
          class="px-1.5 py-0.5 rounded border border-surface-popover-border text-text-muted hover:text-accent-primary-start hover:border-accent-primary-start/40 font-label-sm border bg-transparent cursor-pointer transition-colors"
        >
          Today
        </button>
      </div>
      <div class="grid grid-cols-7 gap-0.5" role="grid">
        {#each DOW as d}
          <div
            class="text-center text-[9px] uppercase tracking-widest font-label-sm-bold text-text-muted py-0.5"
          >
            {d}
          </div>
        {/each}
        {#each miniWeeks as week, wi (wi)}
          {#each week as day, di (di)}
            {@const inMonth = day.getMonth() === miniCursor.getMonth()}
            {@const key = ymd(day)}
            {@const count = byDate[key] ?? 0}
            {@const flatIdx = wi * 7 + di}
            <button
              type="button"
              role="gridcell"
              tabindex={flatIdx === miniFocusIdx ? 0 : -1}
              data-mini-day={flatIdx}
              data-mini-date={key}
              data-test-mini-day={key}
              onclick={() => pickDay(day)}
              onkeydown={(e) => onDayKeydown(e, flatIdx)}
              aria-label={`${key}${count ? ', ' + count + ' task' + (count === 1 ? '' : 's') : ''}`}
              aria-current={key === activeFocusDate ? 'date' : undefined}
              data-testid={`mini-day-${key}`}
              class="aspect-square flex flex-col items-center justify-center rounded text-[10px] font-label-sm cursor-pointer border-none bg-transparent
                {inMonth
                ? 'text-text-primary hover:bg-hover'
                : 'text-text-muted/50'}
                {key === activeFocusDate
                ? 'ring-1 ring-accent-primary-start bg-accent-primary-glow'
                : ''}"
            >
              <span>{day.getDate()}</span>
              {#if count > 0}
                <span
                  class="w-1 h-1 rounded-full bg-accent-primary-start"
                  aria-hidden="true"
                ></span>
              {/if}
            </button>
          {/each}
        {/each}
      </div>
      {#if activeFocusDate}
        <button
          type="button"
          onclick={() => clearFocusDate()}
          data-testid="clear-focus"
          class="mt-1 w-full flex items-center justify-center gap-1 px-2 py-1 rounded text-[11px] font-label-sm text-text-muted hover:text-error cursor-pointer border border-dashed border-surface-popover-border bg-transparent transition-colors"
        >
          <span class="material-symbols-outlined text-[12px]">close</span>
          Clear jump date
        </button>
      {/if}
    </div>
  </section>

  <!-- Active Filters (lifted from KanbanSidebar) -->
  <section aria-labelledby="tasks-filters-heading">
    <h3
      id="tasks-filters-heading"
      class="px-2 font-label-sm-bold uppercase tracking-widest text-[10px] text-text-muted"
    >
      Active Filters
    </h3>
    <div class="mt-1 space-y-2">
      <!-- Owners -->
      {#if owners.length > 0}
        <div>
          <p
            class="px-2 text-[10px] font-label-sm-bold text-text-muted uppercase tracking-widest mb-1"
          >
            Owners
          </p>
          <ul class="space-y-0.5">
            {#each owners as o (o)}
              {@const checked = liveFilters.owners.includes(o)}
              <li>
                <label
                  class="flex items-center gap-2 px-2 py-1 rounded text-[12px] font-body-md cursor-pointer hover:bg-hover"
                >
                  <input
                    type="checkbox"
                    {checked}
                    onchange={() => toggleOwner(o)}
                    data-testid={`owner-${o}`}
                    class="rounded border-surface-sidebar-border bg-surface-sidebar"
                  />
                  <span class="text-text-primary">{o}</span>
                </label>
              </li>
            {/each}
          </ul>
        </div>
      {/if}

      <!-- Priorities -->
      <div>
        <p
          class="px-2 text-[10px] font-label-sm-bold text-text-muted uppercase tracking-widest mb-1"
        >
          Priority
        </p>
        <ul class="space-y-0.5">
          {#each [1, 2, 3] as p (p)}
            {@const checked = liveFilters.priorities.includes(p)}
            <li>
              <label
                class="flex items-center gap-2 px-2 py-1 rounded text-[12px] font-body-md cursor-pointer hover:bg-hover"
              >
                <input
                  type="checkbox"
                  {checked}
                  onchange={() => togglePriority(p)}
                  data-testid={`priority-${p}`}
                  class="rounded border-surface-sidebar-border bg-surface-sidebar"
                />
                <span class="text-text-primary"
                  >P{p} · {PRIORITY_LABELS[p] ?? 'Normal'}</span
                >
              </label>
            </li>
          {/each}
        </ul>
      </div>

      <!-- Due-date quick-pick -->
      <div>
        <p
          class="px-2 text-[10px] font-label-sm-bold text-text-muted uppercase tracking-widest mb-1"
        >
          Due
        </p>
        <ul
          role="radiogroup"
          aria-label="Due-date quick-pick"
          class="space-y-0.5"
        >
          {#each DUE_OPTIONS as opt (opt.v)}
            <li>
              <button
                type="button"
                role="radio"
                aria-checked={liveFilters.dueDate === opt.v}
                data-testid={opt.testid}
                onclick={() => setDueDateChip(opt.v)}
                class="w-full flex items-center gap-2 px-2 py-1 rounded text-[12px] font-body-md cursor-pointer border-none bg-transparent text-left
                  {liveFilters.dueDate === opt.v
                  ? 'bg-accent-primary-glow text-accent-primary-start'
                  : 'text-text-primary hover:bg-hover'}"
              >
                <span
                  class="w-2 h-2 rounded-full"
                  class:bg-accent-primary-start={liveFilters.dueDate === opt.v}
                  class:bg-surface-sidebar-border={liveFilters.dueDate !==
                    opt.v}
                ></span>
                <span class="flex-1">{opt.l}</span>
              </button>
            </li>
          {/each}
        </ul>
      </div>

      <!-- Tags -->
      {#if tags.length > 0}
        <div>
          <p
            class="px-2 text-[10px] font-label-sm-bold text-text-muted uppercase tracking-widest mb-1"
          >
            Tags
          </p>
          <ul class="space-y-0.5">
            {#each tags as t (t)}
              {@const checked = liveFilters.tags.includes(t)}
              <li>
                <label
                  class="flex items-center gap-2 px-2 py-1 rounded text-[12px] font-body-md cursor-pointer hover:bg-hover"
                >
                  <input
                    type="checkbox"
                    {checked}
                    onchange={() => toggleTag(t)}
                    data-testid={`tag-${t}`}
                    class="rounded border-surface-sidebar-border bg-surface-sidebar"
                  />
                  <span class="text-text-primary">{t}</span>
                </label>
              </li>
            {/each}
          </ul>
        </div>
      {/if}

      <!-- Clear all -->
      {#if liveFilters.owners.length || liveFilters.priorities.length || liveFilters.dueDate || liveFilters.tags.length}
        <button
          type="button"
          onclick={() => clearFilters()}
          data-testid="clear-filters"
          class="w-full flex items-center justify-center gap-1 px-2 py-1 rounded text-[11px] font-label-sm text-text-muted hover:text-error cursor-pointer border border-dashed border-surface-sidebar-border bg-transparent transition-colors"
        >
          <span class="material-symbols-outlined text-[12px]">close</span>
          Clear all filters
        </button>
      {/if}
    </div>
  </section>

  <!-- aria-live region announces count + filter changes -->
  <div class="sr-only" aria-live="polite">{liveMessage}</div>

  {#if errorMsg}
    <p class="px-2 text-error text-[11px] font-body-md" role="alert">
      {errorMsg}
    </p>
  {/if}
</aside>
