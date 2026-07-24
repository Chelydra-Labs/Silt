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
  import { onMount, tick } from 'svelte'
  import type { PluginContext, PluginManifest } from '../../sdk'
  import { plusDaysISO, localToday } from '../../sdk'
  import ErrorBanner from './components/ErrorBanner.svelte'
  import ConfirmModal from './components/ConfirmModal.svelte'
  import {
    getTaskHubState,
    setActiveFilter,
    clearActiveFilter,
    setFocusDate,
    clearFocusDate,
    applySavedView,
    deleteSavedView,
    saveView,
    reorderSavedViews,
    type SavedView,
    type CalendarFilter
  } from './state.svelte'
  import { viewMatchesState } from './savedViews'
  import { persistSavedViews } from './settings'
  import ContextMenu from '../../../components/ContextMenu.svelte'
  import { isDevMode, openInspect } from '../../../lib/devModeInspect'
  import {
    ymd,
    startOfMonth,
    endOfMonth,
    addMonths,
    monthWeeks as computeMonthWeeks
  } from '../../../lib/dateGrid'

  interface Props {
    ctx: PluginContext
    manifest?: PluginManifest
  }

  let { ctx, manifest: _manifest }: Props = $props()

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
  let errorMsg = $state('')
  let calendarExpanded = $state(
    typeof localStorage !== 'undefined'
      ? localStorage.getItem('silt-tasks-mini-cal-expanded') !== 'false'
      : true
  )

  function toggleCalendar() {
    calendarExpanded = !calendarExpanded
    if (typeof localStorage !== 'undefined') {
      localStorage.setItem(
        'silt-tasks-mini-cal-expanded',
        String(calendarExpanded)
      )
    }
  }

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
          SUM(CASE WHEN t.status = 'DONE' THEN 1 ELSE 0 END) AS completed,
          SUM(CASE WHEN t.status != 'DONE' THEN 1 ELSE 0 END) AS "all"
       FROM blocks b JOIN tasks t ON b.id = t.block_id`,
      [today, tomorrow, weekAhead, today]
    )
    const row = res.rows?.[0] ?? {}
    counts = {
      today: Number(row.today ?? 0),
      upcoming: Number(row.upcoming ?? 0),
      overdue: Number(row.overdue ?? 0),
      completed: Number(row.completed ?? 0),
      all: Number(row.all ?? 0)
    }
  }

  async function reloadDayDots(): Promise<void> {
    const first = ymd(startOfMonth(miniCursor))
    const last = ymd(endOfMonth(miniCursor))
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

  async function reload(): Promise<void> {
    errorMsg = ''
    // Counts + day-dots fail together (sidebar is unuseable without them)
    try {
      await Promise.all([reloadCounts(), reloadDayDots()])
    } catch (e) {
      errorMsg = e instanceof Error ? e.message : String(e)
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

  // Mini-cal month grid uses the pure dateGrid helpers shared with the Tasks
  // calendar and the Date Glance popover.
  let miniWeeks = $derived(computeMonthWeeks(miniCursor))

  function prevMonth() {
    miniCursor = addMonths(miniCursor, -1)
  }
  function nextMonth() {
    miniCursor = addMonths(miniCursor, 1)
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
    let nextIdx: number
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

  // Inline rename state (#470).
  let renamingId = $state<string | null>(null)
  let renameValue = $state('')
  let renameError = $state('')
  let renameInputEl = $state<HTMLInputElement | null>(null)

  // Manage-menu (⋯ button + right-click share the same menu).
  let manageMenu = $state<{
    viewId: string
    x: number
    y: number
    anchorEl: HTMLElement | null
  } | null>(null)

  // Delete-confirmation modal target.
  let deleteTarget = $state<SavedView | null>(null)

  // Drag-and-drop reorder state.
  let dragId = $state<string | null>(null)
  let dropTarget = $state<{ id: string; before: boolean } | null>(null)

  let userViews = $derived(hubState.savedViews.filter((v) => !v.system))

  function openManageMenu(
    view: SavedView,
    x: number,
    y: number,
    anchorEl: HTMLElement | null
  ) {
    if (view.system) return
    // Store the raw anchor; the $effect below clamps it to the viewport using
    // the menu's real rendered dimensions (accurate, vs. the former hardcoded
    // 180×220 estimate that could mis-clamp).
    manageMenu = { viewId: view.id, x, y, anchorEl }
  }

  function openManageMenuFromButton(e: MouseEvent, view: SavedView) {
    if (view.system) return
    e.stopPropagation()
    const rect = (e.currentTarget as HTMLElement).getBoundingClientRect()
    // If the same menu is already open, toggle it closed (button-as-toggle).
    if (manageMenu?.viewId === view.id) {
      manageMenu = null
      return
    }
    openManageMenu(
      view,
      rect.left,
      rect.bottom + 2,
      e.currentTarget as HTMLElement
    )
  }

  function onRowContextMenu(e: MouseEvent, view: SavedView) {
    if (view.system) return // system views: let the browser menu show
    e.preventDefault()
    openManageMenu(view, e.clientX, e.clientY, e.currentTarget as HTMLElement)
  }

  function closeManageMenu() {
    manageMenu = null
  }

  function startRename(view: SavedView) {
    if (view.system) return
    closeManageMenu()
    renamingId = view.id
    renameValue = view.name
    renameError = ''
    void tick().then(() => {
      renameInputEl?.focus()
      renameInputEl?.select()
    })
  }

  function cancelRename() {
    renamingId = null
    renameValue = ''
    renameError = ''
  }

  async function commitRename() {
    const id = renamingId
    if (!id) return
    const name = renameValue.trim()
    if (!name) {
      renameError = 'Enter a view name'
      return
    }
    // Exit rename mode synchronously so the blur handler can't double-fire.
    renamingId = null
    const view = getTaskHubState().savedViews.find((v) => v.id === id)
    if (!view) return
    saveView({ ...view, name })
    renameError = ''
    errorMsg = ''
    const ok = await persistSavedViews(getTaskHubState().savedViews)
    if (!ok) errorMsg = 'Failed to save view'
  }

  // Overwrite the view's stored dimensions with the current hub state
  // (mirrors TasksHub.commitUpdateActive). Only meaningful for the active
  // dirty view — the menu only offers it in that state.
  async function overwriteView(view: SavedView) {
    if (view.system) return
    closeManageMenu()
    const s = getTaskHubState()
    const updated: SavedView = {
      id: view.id,
      name: view.name,
      displayMode: s.displayMode,
      groupBy: s.groupBy,
      sort: s.sort,
      scope: s.scope,
      filters: {
        owners: [...s.filters.owners],
        priorities: [...s.filters.priorities],
        dueDate: s.filters.dueDate,
        tags: [...s.filters.tags],
        stale: s.filters.stale
      },
      calendarSubMode: s.calendarSubMode,
      columns: s.columns.map((c) => ({ ...c })),
      system: false
    }
    saveView(updated)
    applySavedView(updated) // clears the dirty flag
    errorMsg = ''
    const ok = await persistSavedViews(getTaskHubState().savedViews)
    if (!ok) errorMsg = 'Failed to save view'
  }

  function requestDelete(view: SavedView) {
    if (view.system) return
    closeManageMenu()
    deleteTarget = view
  }

  function cancelDelete() {
    deleteTarget = null
  }

  async function confirmDelete() {
    const view = deleteTarget
    if (!view) return
    deleteTarget = null
    if (view.system) return
    errorMsg = ''
    // Capture before the in-memory delete so a persist failure can restore it
    // — without this, the view vanishes from the UI but survives on disk.
    const viewToRemove = getTaskHubState().savedViews.find(
      (v) => v.id === view.id
    )
    deleteSavedView(view.id)
    const ok = await persistSavedViews(getTaskHubState().savedViews)
    if (!ok && viewToRemove) {
      saveView(viewToRemove)
      errorMsg = 'Delete failed — the view will reappear on next launch.'
    } else if (!ok) {
      errorMsg = 'Failed to delete view — will retry on next launch'
    }
  }

  async function persistViewList() {
    errorMsg = ''
    const ok = await persistSavedViews(getTaskHubState().savedViews)
    if (!ok) errorMsg = 'Failed to save view order'
  }

  // --- Reorder (drag + keyboard move) ------------------------------------

  function onViewDragStart(e: DragEvent, view: SavedView) {
    if (view.system) {
      e.preventDefault()
      return
    }
    dragId = view.id
    if (e.dataTransfer) {
      e.dataTransfer.effectAllowed = 'move'
      e.dataTransfer.setData('text/plain', view.id)
    }
  }

  function onViewDragOver(e: DragEvent, view: SavedView) {
    if (!dragId || view.system || view.id === dragId) return
    e.preventDefault()
    if (e.dataTransfer) e.dataTransfer.dropEffect = 'move'
    const rect = (e.currentTarget as HTMLElement).getBoundingClientRect()
    dropTarget = {
      id: view.id,
      before: e.clientY < rect.top + rect.height / 2
    }
  }

  async function onViewDrop(e: DragEvent, view: SavedView) {
    e.preventDefault()
    const fromId = dragId
    const before = dropTarget?.before ?? false
    dragId = null
    dropTarget = null
    if (!fromId || view.system || fromId === view.id) return
    reorderSavedViews(fromId, view.id, before)
    await persistViewList()
  }

  function onViewDragEnd() {
    dragId = null
    dropTarget = null
  }

  async function moveView(view: SavedView, direction: -1 | 1) {
    const list = getTaskHubState().savedViews.filter((v) => !v.system)
    const idx = list.findIndex((v) => v.id === view.id)
    if (idx < 0) return
    const swapIdx = idx + direction
    if (swapIdx < 0 || swapIdx >= list.length) return
    reorderSavedViews(
      view.id,
      list[swapIdx].id,
      direction === -1 // up → land before the predecessor
    )
    closeManageMenu()
    await persistViewList()
  }

  function canMoveUp(view: SavedView): boolean {
    const idx = userViews.findIndex((v) => v.id === view.id)
    return idx > 0
  }
  function canMoveDown(view: SavedView): boolean {
    const idx = userViews.findIndex((v) => v.id === view.id)
    return idx >= 0 && idx < userViews.length - 1
  }

  // Keep the menu's manageMenu referencing a live view — if the list changes
  // underneath (e.g. the view was deleted from elsewhere), close the menu.
  let manageMenuView = $derived.by(() => {
    const m = manageMenu
    if (!m) return undefined
    return hubState.savedViews.find((v) => v.id === m.viewId)
  })

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
</script>

<aside
  class="flex flex-col gap-4 px-3 py-3"
  aria-label="Tasks sidebar"
  data-test-tasks-sidebar
>
  <!-- Smart Lists (lifted from CalendarSidebar). Counts are vault-wide: the
       smart-list query carries no scope/filter predicates, so a count can
       exceed what the (scope/filter-aware) hub shows. -->
  <section aria-labelledby="tasks-smart-lists-heading">
    <h3
      id="tasks-smart-lists-heading"
      class="px-2 font-label-sm-bold uppercase tracking-widest text-type-2xs text-text-muted"
    >
      Smart Lists
      <span
        class="font-label-sm normal-case tracking-normal text-text-muted/60"
      >
        (vault-wide)
      </span>
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
              class="w-full flex items-center gap-2 px-2 py-1.5 rounded text-left text-type-sm font-body-md cursor-pointer border-none bg-transparent transition-colors
              {selected
                ? 'bg-accent-primary-glow text-accent-primary-start'
                : 'text-text-primary hover:bg-hover'}"
            >
              <span
                class="material-symbols-outlined text-icon-sm"
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
                class="text-type-2xs text-text-muted bg-surface-popover px-1.5 py-0.5 rounded-sm font-label-sm"
                aria-label="{counts[item.id]} tasks"
                data-testid={`count-${item.id}`}
              >
                {counts[item.id]}
              </span>
            </button>
          </li>
        {/each}
      </ul>
    {:else}
      <p
        class="mt-1 px-2 py-2 text-type-xs font-body-md text-text-muted"
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
        class="mt-1 w-full flex items-center justify-center gap-1 px-2 py-1 rounded text-type-xs font-label-sm text-text-muted hover:text-error cursor-pointer border border-dashed border-surface-popover-border bg-transparent transition-colors"
      >
        <span class="material-symbols-outlined text-icon-xs">close</span>
        Clear filter
      </button>
    {/if}
  </section>

  <!-- Saved Views (lifted from KanbanSidebar; management UX #470) -->
  <section aria-labelledby="tasks-saved-views-heading">
    <h3
      id="tasks-saved-views-heading"
      class="px-2 font-label-sm-bold uppercase tracking-widest text-type-2xs text-text-muted"
    >
      Saved Views
    </h3>
    <ul role="list" class="mt-1 space-y-0.5">
      {#each hubState.savedViews as view (view.id)}
        {@const isActive = viewMatchesState(view, hubState)}
        {@const isRenaming = renamingId === view.id}
        {@const isUser = view.system !== true}
        {@const isDragging = dragId === view.id}
        {@const isDropBefore = dropTarget?.id === view.id && dropTarget.before}
        {@const isDropAfter = dropTarget?.id === view.id && !dropTarget.before}
        <li
          class="group relative"
          data-testid={`view-row-${view.id}`}
          oncontextmenu={(e) => onRowContextMenu(e, view)}
          ondragover={(e) => onViewDragOver(e, view)}
          ondrop={(e) => onViewDrop(e, view)}
        >
          <div
            class="flex items-center gap-0.5 px-1 py-0.5 rounded text-type-sm font-body-md border transition-colors
              {isActive
              ? 'bg-accent-primary-glow border-accent-primary-start/30 text-accent-primary-start'
              : 'text-text-primary hover:bg-hover border-transparent'}
              {isDragging ? 'opacity-40' : ''}
              {isDropBefore
              ? 'border-t-2 border-t-accent-primary-start border-b-transparent'
              : ''}
              {isDropAfter
              ? 'border-b-2 border-b-accent-primary-start border-t-transparent'
              : ''}"
            data-testid={`view-${view.id}`}
          >
            {#if isUser}
              <span
                draggable="true"
                ondragstart={(e) => onViewDragStart(e, view)}
                ondragend={onViewDragEnd}
                class="flex items-center text-text-muted/30 group-hover:text-text-muted cursor-grab active:cursor-grabbing touch-none"
                title="Drag to reorder"
                aria-hidden="true"
                data-testid={`grip-${view.id}`}
              >
                <span class="material-symbols-outlined text-icon-sm"
                  >drag_indicator</span
                >
              </span>
            {/if}

            {#if isRenaming}
              <label class="flex-1 sr-only" for={`rename-${view.id}`}>
                Rename {view.name}
              </label>
              <input
                id={`rename-${view.id}`}
                bind:this={renameInputEl}
                bind:value={renameValue}
                data-testid={`rename-input-${view.id}`}
                onkeydown={(e) => {
                  if (e.key === 'Enter') {
                    e.preventDefault()
                    void commitRename()
                  } else if (e.key === 'Escape') {
                    e.preventDefault()
                    cancelRename()
                  }
                }}
                onblur={() => {
                  // Commit on blur if a non-empty name was entered; cancel
                  // otherwise. commitRename exits rename mode synchronously,
                  // so a re-entrant blur is a no-op.
                  if (renamingId) {
                    if (renameValue.trim()) void commitRename()
                    else cancelRename()
                  }
                }}
                class="flex-1 min-w-0 px-1.5 py-1 rounded bg-surface-panel border border-accent-primary-start text-text-primary text-type-sm outline-none"
              />
              {#if renameError}
                <span class="sr-only" role="alert">{renameError}</span>
              {/if}
            {:else}
              <button
                type="button"
                onclick={() => activateView(view)}
                aria-pressed={isActive}
                class="flex-1 min-w-0 text-left px-1.5 py-1 rounded cursor-pointer border-none bg-transparent"
              >
                <span class="truncate inline-block max-w-full align-middle"
                  >{view.name}</span
                >
                {#if view.id === hubState.activeSavedViewId && hubState.savedViewsDirty}
                  <span
                    class="inline-block w-1.5 h-1.5 rounded-full bg-accent-secondary-start ml-1"
                    title="This view has unsaved changes"
                    aria-label="modified"
                  ></span>
                {/if}
              </button>
            {/if}

            {#if view.system}
              <span
                class="material-symbols-outlined text-type-xs text-text-muted/50"
                aria-label="Built-in view"
                title="Built-in view — can't be modified">lock</span
              >
            {/if}

            {#if isUser}
              <button
                type="button"
                onclick={(e) => openManageMenuFromButton(e, view)}
                aria-haspopup="menu"
                aria-expanded={manageMenu?.viewId === view.id}
                aria-label="Manage view {view.name}"
                data-testid={`manage-view-${view.id}`}
                class="p-1 rounded text-text-muted hover:text-text-primary hover:bg-hover border-none bg-transparent cursor-pointer transition-opacity
                  {manageMenu?.viewId === view.id
                  ? 'opacity-100'
                  : 'opacity-0 group-hover:opacity-100 focus-visible:opacity-100'}"
              >
                <span class="material-symbols-outlined text-icon-sm"
                  >more_horiz</span
                >
              </button>
            {/if}
          </div>
        </li>
      {/each}
    </ul>
  </section>

  <!-- Jump-to-Date mini-cal (lifted from CalendarSidebar) -->
  <section aria-labelledby="tasks-mini-heading" class="flex flex-col">
    <div class="flex items-center justify-between px-2">
      <h3
        id="tasks-mini-heading"
        class="font-label-sm-bold uppercase tracking-widest text-type-2xs text-text-muted"
      >
        Jump to Date
      </h3>
      <button
        type="button"
        data-testid="toggle-mini-calendar"
        onclick={toggleCalendar}
        aria-expanded={calendarExpanded}
        aria-controls="tasks-mini-calendar-content"
        class="p-0.5 rounded text-text-muted hover:text-text-primary hover:bg-hover border-none bg-transparent cursor-pointer flex items-center transition-colors"
      >
        <span class="material-symbols-outlined text-icon-sm">
          {calendarExpanded ? 'expand_less' : 'expand_more'}
        </span>
      </button>
    </div>
    {#if calendarExpanded}
      <div id="tasks-mini-calendar-content" class="mt-1 px-2">
        <div class="flex items-center justify-between mb-1">
          <button
            type="button"
            onclick={prevMonth}
            aria-label="Previous month"
            class="flex h-7 w-7 items-center justify-center rounded-md border border-transparent text-text-muted hover:bg-hover hover:text-text-primary focus-visible:ring-2 focus-visible:ring-accent-primary-start cursor-pointer"
          >
            <span class="material-symbols-outlined text-icon-lg"
              >chevron_left</span
            >
          </button>
          <span class="text-text-primary text-type-xs font-label-sm-bold">
            {MONTHS[miniCursor.getMonth()]}
            {miniCursor.getFullYear()}
          </span>
          <button
            type="button"
            onclick={nextMonth}
            aria-label="Next month"
            class="flex h-7 w-7 items-center justify-center rounded-md border border-transparent text-text-muted hover:bg-hover hover:text-text-primary focus-visible:ring-2 focus-visible:ring-accent-primary-start cursor-pointer"
          >
            <span class="material-symbols-outlined text-icon-lg"
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
            class="rounded-md px-3 py-1 text-type-xs text-text-muted hover:bg-hover hover:text-text-primary focus-visible:ring-2 focus-visible:ring-accent-primary-start cursor-pointer"
          >
            Today
          </button>
        </div>
        <div class="grid grid-cols-7 gap-0.5" role="grid">
          <div role="row" class="contents">
            {#each DOW as d, dowI (dowI)}
              <div
                role="columnheader"
                class="text-center text-type-3xs uppercase tracking-widest font-label-sm-bold text-text-muted py-0.5"
              >
                {d}
              </div>
            {/each}
          </div>
          {#each miniWeeks as week, wi (wi)}
            <div role="row" class="contents">
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
                  onclick={() => {
                    miniFocusIdx = flatIdx
                    pickDay(day)
                  }}
                  onkeydown={(e) => onDayKeydown(e, flatIdx)}
                  aria-label={`${key}${count ? ', ' + count + ' task' + (count === 1 ? '' : 's') : ''}`}
                  aria-current={key === activeFocusDate ? 'date' : undefined}
                  data-testid={`mini-day-${key}`}
                  class="aspect-square flex flex-col items-center justify-center gap-0.5 rounded text-type-sm font-label-sm cursor-pointer border-none bg-transparent focus-visible:ring-2 focus-visible:ring-accent-primary-start focus-visible:outline-none
                    {inMonth
                    ? 'text-text-primary hover:bg-hover'
                    : 'text-text-muted/50 hover:bg-hover'}
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
            </div>
          {/each}
        </div>
        {#if activeFocusDate}
          <button
            type="button"
            onclick={() => clearFocusDate()}
            data-testid="clear-focus"
            class="mt-1 w-full flex items-center justify-center gap-1 px-2 py-1 rounded text-type-xs font-label-sm text-text-muted hover:text-error cursor-pointer border border-dashed border-surface-popover-border bg-transparent transition-colors"
          >
            <span class="material-symbols-outlined text-icon-xs">close</span>
            Clear jump date
          </button>
        {/if}
      </div>
    {/if}
  </section>

  <!-- aria-live region announces count + filter changes -->
  <div class="sr-only" aria-live="polite">{liveMessage}</div>

  {#if errorMsg}
    <ErrorBanner message={errorMsg} compact />
  {/if}
</aside>

<!-- Saved-view manage menu (#470). The ⋯ button and right-click both feed
     into the same `manageMenu` state; delegates to the shared ContextMenu
     component (#491) for positioning, dismissal, keyboard nav, and chrome. -->
{#if manageMenu && manageMenuView}
  {@const v = manageMenuView}
  {@const canUpdate =
    hubState.activeSavedViewId === v.id && hubState.savedViewsDirty}
  <ContextMenu
    open={manageMenu !== null && manageMenuView !== undefined}
    anchor={{ x: manageMenu.x, y: manageMenu.y }}
    anchorEl={manageMenu?.anchorEl ?? null}
    onClose={closeManageMenu}
    ariaLabel={`Actions for ${v.name}`}
    backdropTestId="manage-view-backdrop"
    menuTestId="manage-view-menu"
  >
    {#if canUpdate}
      <button
        type="button"
        role="menuitem"
        onclick={() => void overwriteView(v)}
        data-testid="manage-update-view"
      >
        <span class="material-symbols-outlined text-icon-md" aria-hidden="true"
          >save</span
        >
        <span>Update "{v.name}"</span>
      </button>
    {/if}
    <button
      type="button"
      role="menuitem"
      onclick={() => startRename(v)}
      data-testid="manage-rename-view"
    >
      <span class="material-symbols-outlined text-icon-md" aria-hidden="true"
        >edit</span
      >
      <span>Rename…</span>
    </button>
    <button
      type="button"
      role="menuitem"
      disabled={!canMoveUp(v)}
      aria-disabled={!canMoveUp(v)}
      onclick={() => void moveView(v, -1)}
      data-testid="manage-move-up"
      class="disabled:opacity-40 disabled:cursor-not-allowed"
    >
      <span class="material-symbols-outlined text-icon-md" aria-hidden="true"
        >arrow_upward</span
      >
      <span>Move up</span>
    </button>
    <button
      type="button"
      role="menuitem"
      disabled={!canMoveDown(v)}
      aria-disabled={!canMoveDown(v)}
      onclick={() => void moveView(v, 1)}
      data-testid="manage-move-down"
      class="disabled:opacity-40 disabled:cursor-not-allowed"
    >
      <span class="material-symbols-outlined text-icon-md" aria-hidden="true"
        >arrow_downward</span
      >
      <span>Move down</span>
    </button>
    <div class="context-menu-separator" aria-hidden="true"></div>
    <button
      type="button"
      role="menuitem"
      onclick={() => requestDelete(v)}
      data-testid="manage-delete-view"
      class="text-status-danger"
    >
      <span class="material-symbols-outlined text-icon-md" aria-hidden="true"
        >delete</span
      >
      <span>Delete…</span>
    </button>
    {#if isDevMode()}
      <div class="context-menu-separator" aria-hidden="true"></div>
      <button
        type="button"
        role="menuitem"
        data-testid="manage-inspect"
        onclick={() => {
          closeManageMenu()
          void openInspect()
        }}
      >
        <span class="material-symbols-outlined text-icon-md" aria-hidden="true"
          >bug_report</span
        >
        <span>Inspect</span>
      </button>
    {/if}
  </ContextMenu>
{/if}

<!-- Delete confirmation (#470). In-app modal replaces window.confirm(). -->
{#if deleteTarget}
  <ConfirmModal
    title="Delete saved view?"
    message={`Delete “${deleteTarget.name}”? This view will be removed from your saved views.`}
    confirmLabel="Delete"
    cancelLabel="Cancel"
    destructive={true}
    dataTestId="delete-view-confirm"
    onConfirm={() => void confirmDelete()}
    onCancel={cancelDelete}
  />
{/if}
