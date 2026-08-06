<script lang="ts">
  import { SvelteDate } from 'svelte/reactivity'
  // Calendar display mode of the Tasks hub (#425). Lifts the proven
  // silt-calendar month/week grid + drag-reschedule + quick-add patterns,
  // and promotes the standalone Calendar's CalItem to the unified TaskDetail
  // so the grid reads through the same grouping/query engine as List + Board.
  //
  // The hub owns the chrome (title/count header, mode switch, FilterBar,
  // scope breadcrumb, group-by/sort selectors); CalendarView consumes
  // scope/filters/calendarSubMode/focusDate from getTaskHubState() and renders
  // only the grid + its own error/notice banners + drawer/sub-editor.
  //
  // Single-click chip → TaskEditDrawer (non-blocking); Shift+Enter →
  // TaskSubEditorModal — identical to List/Board. This closes #414 (the old
  // standalone Calendar dispatched navigate-to-block on chip click).
  //
  // Calendar ignores the hub's groupBy (the grid has no second axis); a
  // one-time notice informs the user the first time per session they enter
  // Calendar mode with a non-'none'/non-'dueDate' groupBy active.
  import { onMount, onDestroy } from 'svelte'
  import { plusDaysISO } from '../../../sdk'
  import TaskEditDrawer from '../components/TaskEditDrawer.svelte'
  import ErrorBanner from '../components/ErrorBanner.svelte'
  import TaskSubEditorModal from '../components/TaskSubEditorModal.svelte'
  import QuickAddTask from '../components/QuickAddTask.svelte'
  import { coerceTaskRow, type TaskDetail, type TaskViewProps } from '../types'
  import {
    getTaskHubQueryContext,
    getTaskHubViewState,
    setCalendarSubMode,
    type CalendarSubMode
  } from '../state.svelte'
  import { buildQuery } from '../query'
  import { loadCalendarSubMode, persistCalendarSubMode } from '../settings'
  import { useBlockChangedReload } from '../shared.svelte'
  import {
    ymd,
    startOfWeek,
    endOfWeek,
    startOfMonth,
    endOfMonth,
    addMonths,
    addDays,
    monthWeeks as computeMonthWeeks
  } from '../../../../lib/dateGrid'
  import { getTaskWeekStart } from '../../../../lib/taskWeekStart.svelte'

  type Props = TaskViewProps

  let { ctx, onCountChange }: Props = $props()

  // --- Hub state (reactive reads) ----------------------------------------
  let subMode = $derived(getTaskHubViewState().calendarSubMode)
  let scope = $derived(getTaskHubViewState().scope)
  let filters = $derived(getTaskHubViewState().filters)
  let groupBy = $derived(getTaskHubViewState().groupBy)
  let weekStart = $derived(getTaskWeekStart())
  let today = $derived(ctx.today)

  // --- Local state --------------------------------------------------------
  // Anchor date for the visible window; the focus-date listener pans this.
  // Initialized from ctx.today (not wall-clock new SvelteDate()) so the visible
  // month tracks the same "today" the Sidebar's mini-cal does — avoids drift
  // near a month boundary when ctx.today is injected (#118).
  function cursorFromToday(): Date {
    const iso = ctx.today
    const [y, m, d] = iso.split('-').map(Number)
    return new SvelteDate(y ?? 1970, (m ?? 1) - 1, d ?? 1)
  }
  let cursor = $state(cursorFromToday())
  let byDate = $state<Record<string, TaskDetail[]>>({})
  let undated = $state<TaskDetail[]>([])
  // All-overdue-open rows (no lower-bound window) so tasks from past months
  // still surface in today's cell. Filled by a third query in reload().
  let overdueAll = $state<TaskDetail[]>([])
  let loading = $state(true)
  let errorMsg = $state('')
  let selectedTask = $state<TaskDetail | null>(null)
  let subEditorTask = $state<TaskDetail | null>(null)
  // null = closed; '' = open undated; YYYY-MM-DD = open for that day.
  let quickAddDate = $state<string | null>(null)

  // --- Drag-and-drop reschedule (lifted from silt-calendar/Calendar.svelte)
  let dragTaskId = $state<string | null>(null)
  let dragTaskItem = $state<TaskDetail | null>(null)
  let overCellDate = $state<string | null>(null)
  let overNoDate = $state(false)
  let rescheduleAnnouncement = $state('')

  // Reactive "now" so the today-highlight repaints past midnight (ticks 60s).
  let nowTick = $state(0)
  let nowInterval: ReturnType<typeof setInterval> | undefined

  // Roving tabindex for the day-cell grid: only the focused cell joins the
  // tab order (tabindex=0); the rest are tabindex=-1. Arrows move focus
  // within the grid; Tab/Shift-Tab exits to the next/previous UI element.
  let cellFocusIdx = $state(0)

  // --- Calendar arithmetic (lifted verbatim from silt-calendar) ----------
  const DOW = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat']
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

  // --- Layout derivations ------------------------------------------------
  // monthWeeks (month view) and weekDays (week view) use the pure dateGrid
  // helpers shared with the sidebar mini-cal and the Date Glance popover.
  let monthWeeks = $derived(
    subMode === 'month' ? computeMonthWeeks(cursor, weekStart) : []
  )

  let weekDays = $derived.by(() => {
    if (subMode !== 'week') return []
    const first = startOfWeek(cursor, weekStart)
    return Array.from({ length: 7 }, (_, i) => addDays(first, i))
  })

  let windowStart = $derived(
    subMode === 'month'
      ? startOfWeek(startOfMonth(cursor), weekStart)
      : startOfWeek(cursor, weekStart)
  )
  let windowEnd = $derived(
    subMode === 'month'
      ? endOfWeek(endOfMonth(cursor), weekStart)
      : endOfWeek(cursor, weekStart)
  )

  let dayLabels = $derived(
    Array.from(
      { length: 7 },
      (_, i) => DOW[(i + (weekStart === 'monday' ? 1 : 0)) % 7]
    )
  )

  function formatWeekHeading(start: Date, end: Date): string {
    const startMonth = MONTHS[start.getMonth()]
    const endMonth = MONTHS[end.getMonth()]
    const startYear = start.getFullYear()
    const endYear = end.getFullYear()
    if (startYear !== endYear) {
      return `${startMonth} ${start.getDate()}, ${startYear}–${endMonth} ${end.getDate()}, ${endYear}`
    }
    if (start.getMonth() !== end.getMonth()) {
      return `${startMonth} ${start.getDate()}–${endMonth} ${end.getDate()}, ${endYear}`
    }
    return `${startMonth} ${start.getDate()}–${end.getDate()}, ${endYear}`
  }

  let heading = $derived(
    subMode === 'month'
      ? `${MONTHS[cursor.getMonth()]} ${cursor.getFullYear()}`
      : formatWeekHeading(
          startOfWeek(cursor, weekStart),
          endOfWeek(cursor, weekStart)
        )
  )

  let todayKey = $derived.by(() => {
    void nowTick
    // ctx.today is the SDK's local-day anchor (#118) — same invariant the
    // Sidebar uses. Avoids any drift between the cell-highlight and the
    // "today" filter facet.
    return today
  })

  // Overdue open tasks surface into today's cell so they aren't lost in past
  // days. Sourced from the all-overdue-open query (no lower bound) so tasks
  // from past months — which fall outside the visible window — surface too.
  // The surfaced chip carries an error-tone stripe so the user can tell why
  // it appears under today; the task also remains in its actual due-date
  // cell when that date is inside the visible window.
  let overdueSurfaced = $derived.by(() => {
    const out: TaskDetail[] = []
    // eslint-disable-next-line svelte/prefer-svelte-reactivity -- non-reactive local/helper
    const seen = new Set<string>()
    for (const r of overdueAll) {
      if (!seen.has(r.id)) {
        seen.add(r.id)
        out.push(r)
      }
    }
    return out
  })

  // --- Query/reload ------------------------------------------------------
  // Three queries: a windowed SELECT for the visible month/week, a separate
  // undated SELECT for the "No Date" strip, and an all-overdue-open SELECT
  // so tasks from past months surface in today's cell. The unified buildQuery
  // handles scope/owner/priority/tag filters; the window option adds the
  // due-date bounds, overriding filters.dueDate='none' surfaces null/empty
  // rows the window would otherwise exclude, and activeFilter='overdue'
  // produces an unbounded (status open + due < today) query.
  let loadSeq = 0
  async function reload() {
    const my = ++loadSeq
    loading = true
    errorMsg = ''
    try {
      const ctxLike = getTaskHubQueryContext({
        activeNotebook: ctx.activeNotebook,
        activeSection: ctx.activeSection,
        activePage: ctx.activePage,
        today
      })
      const winQ = buildQuery(scope, filters, ctxLike, {
        window: { start: ymd(windowStart), end: ymd(windowEnd) },
        activeFilter: getTaskHubViewState().activeFilter
      })
      const undatedQ = buildQuery(
        scope,
        { ...filters, dueDate: 'none' },
        ctxLike,
        { activeFilter: getTaskHubViewState().activeFilter }
      )
      // Unbounded overdue: tasks due before today, open, scoped/filtered by
      // the user's other filters. No `window` so past months aren't trimmed.
      // LIMIT 500 bounds memory on a vault with years of stale overdue rows.
      const overdueQ = buildQuery(scope, filters, ctxLike, {
        activeFilter: 'overdue'
      })
      const overdueSql = overdueQ.sql + ' LIMIT 500'
      const [winRes, undatedRes, overdueRes] = await Promise.all([
        ctx.sqliteQuery(winQ.sql, winQ.params),
        ctx.sqliteQuery(undatedQ.sql, undatedQ.params),
        ctx.sqliteQuery(overdueSql, overdueQ.params)
      ])
      if (my !== loadSeq) return
      const winRows = (winRes.rows as unknown[]).map((r) => coerceTaskRow(r))
      const bucket: Record<string, TaskDetail[]> = {}
      for (const r of winRows) {
        if (!r.due_date) continue
        ;(bucket[r.due_date] ||= []).push(r)
      }
      byDate = bucket
      undated = (undatedRes.rows as unknown[]).map((r) => coerceTaskRow(r))
      overdueAll = (overdueRes.rows as unknown[]).map((r) => coerceTaskRow(r))
      // Keep the open drawer in sync with fresh data.
      if (selectedTask) {
        const fresh =
          winRows.find((r) => r.id === selectedTask!.id) ??
          undated.find((r) => r.id === selectedTask!.id) ??
          overdueAll.find((r) => r.id === selectedTask!.id)
        if (fresh) selectedTask = fresh
      }
    } catch (e) {
      if (my !== loadSeq) return
      errorMsg = e instanceof Error ? e.message : String(e)
    } finally {
      if (my === loadSeq) loading = false
    }
  }

  onMount(() => {
    // Hydrate persisted sub-mode into hub state once on mount.
    const persisted = loadCalendarSubMode()
    if (persisted !== subMode) setCalendarSubMode(persisted)
    void reload()
    nowInterval = setInterval(() => {
      nowTick++
    }, 60_000)
  })

  onDestroy(() => {
    if (nowInterval) clearInterval(nowInterval)
    if (subModeSaveTimer) clearTimeout(subModeSaveTimer)
  })

  // Reload whenever any reactive input the query depends on changes.
  $effect(() => {
    void subMode
    void weekStart
    void scope
    void filters.owners
    void filters.priorities
    void filters.dueDate
    void filters.tags
    void filters.stale
    void ctx.activeNotebook
    void ctx.activeSection
    void ctx.activePage
    void reload()
  })

  // Repaint on any block mutation (created/mutated/rescheduled from any
  // surface). Debounced so a burst of block:changed events triggers one
  // reload.
  // svelte-ignore state_referenced_locally
  useBlockChangedReload(ctx, reload)

  // Sidebar mini-cal drives focus via setFocusDate (state.svelte), which
  // dispatches the calendar:focus-date window event. Pan the visible
  // month/week so the picked date lands in view. Empty date resets to today.
  $effect(() => {
    const handler = (e: Event) => {
      const ce = e as CustomEvent<{ date: string }>
      const iso = ce.detail?.date
      if (!iso) {
        cursor = new SvelteDate()
        return
      }
      const [y, m, d] = iso.split('-').map(Number)
      if (!y || !m || !d) return
      cursor = new SvelteDate(y, m - 1, d)
    }
    window.addEventListener('calendar:focus-date', handler)
    return () => window.removeEventListener('calendar:focus-date', handler)
  })

  // Report counts upward so the hub header stays in sync. Includes the
  // windowed tasks, the undated strip, AND overdue-surfaced tasks (deduped
  // against the windowed rows so a task due inside the visible window isn't
  // double-counted when it also surfaces in today's cell). Without the
  // overdue contribution the header under-reports vs Board.
  $effect(() => {
    const win = Object.values(byDate).flat()
    const winIds = new Set(win.map((r) => r.id))
    const allOpen = [
      ...win,
      ...undated,
      ...overdueAll.filter((o) => !winIds.has(o.id))
    ]
    const open = allOpen.filter((r) => r.status !== 'DONE').length
    const done = allOpen.filter((r) => r.status === 'DONE').length
    onCountChange?.(open, done)
  })

  // --- Sub-mode toggle (Month/Week) --------------------------------------
  let subModeSaveTimer: ReturnType<typeof setTimeout> | null = null
  let subModeError = $state('')
  const SUBMODES: CalendarSubMode[] = ['month', 'week']
  function chooseSubMode(m: CalendarSubMode) {
    setCalendarSubMode(m)
    // Reset roving tabindex: month grid has up to 42 cells, week has 7, so a
    // stale idx from the other layout would leave no cell with tabindex=0.
    cellFocusIdx = 0
    if (subModeSaveTimer) clearTimeout(subModeSaveTimer)
    subModeSaveTimer = setTimeout(() => {
      void persistCalendarSubMode(m).then((ok) => {
        if (!ok) subModeError = "Couldn't save calendar layout"
      })
    }, 200)
  }

  // WAI-ARIA radiogroup keyboard pattern for the Month/Week submode toggle:
  // ArrowLeft/Right move between options (wrapping), Home/End jump to the
  // boundaries. Mirrors the hub mode switcher (TasksHub.svelte).
  function onSubmodeKeydown(e: KeyboardEvent) {
    if (!['ArrowRight', 'ArrowLeft', 'Home', 'End'].includes(e.key)) return
    e.preventDefault()
    const dir = e.key === 'ArrowLeft' || e.key === 'End' ? -1 : 1
    let start: number
    if (e.key === 'Home') start = 0
    else if (e.key === 'End') start = SUBMODES.length - 1
    else
      start =
        (SUBMODES.indexOf(subMode) + dir + SUBMODES.length) % SUBMODES.length
    chooseSubMode(SUBMODES[start])
    ;(e.currentTarget as HTMLElement)
      .querySelector<HTMLElement>(
        `[data-testid="calendar-submode-${SUBMODES[start]}"]`
      )
      ?.focus()
  }

  // --- Group-by one-time notice -----------------------------------------
  // Calendar ignores groupBy (the grid has no second axis). The first time
  // per session the user enters Calendar mode with an incompatible groupBy,
  // surface a non-blocking notice so they aren't left wondering why their
  // owner/status grouping "disappeared". sessionStorage gates it to once
  // per session (not per render).
  const GROUPBY_NOTICE_KEY = 'silt-tasks:calendar-groupby-notice-shown'
  let groupByNotice = $state(false)
  $effect(() => {
    const g = groupBy
    if (g !== 'none' && g !== 'dueDate') {
      try {
        if (!sessionStorage.getItem(GROUPBY_NOTICE_KEY)) {
          groupByNotice = true
          sessionStorage.setItem(GROUPBY_NOTICE_KEY, '1')
        }
      } catch {
        // sessionStorage may be unavailable (private mode); show once per mount.
        groupByNotice = true
      }
    }
  })
  function dismissGroupByNotice() {
    groupByNotice = false
  }

  // --- Navigation -------------------------------------------------------
  function prev() {
    cursor = subMode === 'month' ? addMonths(cursor, -1) : addDays(cursor, -7)
  }
  function next() {
    cursor = subMode === 'month' ? addMonths(cursor, 1) : addDays(cursor, 7)
  }
  function goToday() {
    cursor = cursorFromToday()
  }

  // --- Quick-add --------------------------------------------------------
  function openQuickAddForDay(day: Date) {
    quickAddDate = ymd(day)
  }
  function openQuickAddUndated() {
    quickAddDate = ''
  }
  function closeQuickAdd() {
    quickAddDate = null
  }

  // --- Drag-and-drop reschedule (lifted from silt-calendar, #292/#293/#294)
  // A chip is draggable onto any day cell OR the "No Date" strip. Drop on a
  // day rewrites [due:: YYYY-MM-DD]; drop on the strip clears the due date.
  // HTML5 DnD has no keyboard semantics, so a focused chip also responds to
  // Alt+Arrows (±1 day / ±7 days) through the same reschedule path.
  function onCardDragStart(e: DragEvent, item: TaskDetail) {
    dragTaskId = item.id
    dragTaskItem = item
    if (e.dataTransfer) {
      e.dataTransfer.setData('text/plain', item.id)
      e.dataTransfer.effectAllowed = 'move'
    }
  }
  function onCardDragEnd() {
    dragTaskId = null
    dragTaskItem = null
    overCellDate = null
    overNoDate = false
  }
  function onCellDragOver(e: DragEvent, day: Date) {
    e.preventDefault()
    if (e.dataTransfer) e.dataTransfer.dropEffect = 'move'
    const key = ymd(day)
    if (overCellDate !== key) overCellDate = key
    overNoDate = false
  }
  function onCellDragLeave(e: DragEvent, day: Date) {
    const rt = e.relatedTarget as Node | null
    if (rt && (e.currentTarget as HTMLElement).contains(rt)) return
    if (overCellDate === ymd(day)) overCellDate = null
  }
  async function onCellDrop(e: DragEvent, day: Date) {
    e.preventDefault()
    const id = dragTaskId ?? e.dataTransfer?.getData('text/plain') ?? ''
    const item = dragTaskItem
    overCellDate = null
    dragTaskId = null
    dragTaskItem = null
    if (!id) return
    const target = ymd(day)
    // No-op guard: dropping a chip on its own current due-date cell would
    // round-trip through an atomic write for zero semantic effect.
    if (item && item.due_date === target) {
      rescheduleAnnouncement = `Already scheduled for ${target}`
      return
    }
    await reschedule(id, target, item?.clean_content)
  }
  // "No Date" strip drop → clear due date.
  function onNoDateDragOver(e: DragEvent) {
    if (!dragTaskId) return
    e.preventDefault()
    if (e.dataTransfer) e.dataTransfer.dropEffect = 'move'
    overNoDate = true
    overCellDate = null
  }
  function onNoDateDragLeave(e: DragEvent) {
    const rt = e.relatedTarget as Node | null
    if (rt && (e.currentTarget as HTMLElement).contains(rt)) return
    overNoDate = false
  }
  async function onNoDateDrop(e: DragEvent) {
    if (!dragTaskId) return
    e.preventDefault()
    const id = dragTaskId
    const item = dragTaskItem
    overNoDate = false
    dragTaskId = null
    dragTaskItem = null
    if (!id) return
    if (item && !item.due_date) {
      rescheduleAnnouncement = 'Already has no due date'
      return
    }
    await reschedule(id, '', item?.clean_content)
  }

  // reschedule rewrites the due date and announces the result. Shared by the
  // mouse-drop paths (day cell + No Date strip) and the keyboard Alt+Arrow
  // path so there is one source of truth for the mutation + announcement.
  async function reschedule(blockId: string, newDate: string, title?: string) {
    try {
      await ctx.setTaskDueDate(blockId, newDate)
      rescheduleAnnouncement = title
        ? newDate
          ? `Rescheduled "${title}" to ${newDate}`
          : `Cleared due date on "${title}"`
        : newDate
          ? `Rescheduled to ${newDate}`
          : 'Cleared due date'
    } catch (e) {
      rescheduleAnnouncement =
        e instanceof Error ? e.message : 'Reschedule failed'
    }
  }

  // Chip keyboard handling. Alt+Arrows reschedule (±1/±7 days); Shift+Enter
  // opens the sub-editor; plain Enter/Space fall through to the native button
  // click → drawer open (so the chip's onclick is the single source for that).
  function onCardKeydown(e: KeyboardEvent, item: TaskDetail) {
    if (e.altKey) {
      const map: Record<string, number> = {
        ArrowLeft: -1,
        ArrowRight: 1,
        ArrowUp: -7,
        ArrowDown: 7
      }
      const delta = map[e.key]
      if (delta === undefined) return
      e.preventDefault()
      e.stopPropagation()
      const base = item.due_date || todayKey
      void reschedule(item.id, plusDaysISO(base, delta), item.clean_content)
      return
    }
    if (e.key === 'Enter' && e.shiftKey) {
      e.preventDefault()
      e.stopPropagation()
      subEditorTask = item
    }
  }

  // Month/week-cell keyboard navigation: arrows move focus by day (clamped
  // to the visible grid); Enter opens quick-add for the focused day. Shared
  // between month + week views so week-view gets full arrow parity (was
  // Enter-only). Alt-modified arrows are handled by the focused chip's
  // reschedule handler — let them through. Grid source switches on subMode
  // (monthWeeks is [] in week view, weekDays is [] in month view).
  function onCellKeydown(e: KeyboardEvent, day: Date) {
    if (e.altKey) return
    if (e.key === 'Enter') {
      e.preventDefault()
      openQuickAddForDay(day)
      return
    }
    const map: Record<string, number> = {
      ArrowRight: 1,
      ArrowLeft: -1,
      ArrowDown: 7,
      ArrowUp: -7
    }
    const delta = map[e.key]
    if (delta === undefined) return
    e.preventDefault()
    const grid = subMode === 'month' ? monthWeeks.flat() : weekDays
    const idx = grid.findIndex((d) => ymd(d) === ymd(day))
    if (idx < 0) return
    const next = Math.min(Math.max(idx + delta, 0), grid.length - 1)
    const targetDt = grid[next]
    if (!targetDt) return
    cellFocusIdx = next
    document
      .querySelector<HTMLElement>(`[data-celldate="${ymd(targetDt)}"]`)
      ?.focus()
  }

  // Compose the chip list for a day cell: the tasks actually due that day,
  // capped at 3 with a "+N more" indicator.
  function cellItems(day: Date): TaskDetail[] {
    return byDate[ymd(day)] ?? []
  }
</script>

<div
  class="flex-1 flex flex-col min-h-0 overflow-hidden"
  data-testid="tasks-calendar"
>
  <header
    class="calendar-toolbar flex flex-col gap-2.5 border-b border-surface-panel-border px-3 py-3 sm:px-5 lg:px-6"
  >
    <!-- Row 1: Icon, sub-mode toggle, New task -->
    <div class="flex items-center gap-3 w-full flex-wrap">
      <span
        class="flex h-8 w-8 items-center justify-center rounded-lg bg-accent-primary-glow text-accent-primary-start"
        aria-hidden="true"
      >
        <span class="material-symbols-outlined text-icon-lg">
          {subMode === 'month' ? 'calendar_month' : 'calendar_view_week'}
        </span>
      </span>

      <!-- Month/Week sub-toggle ( BELOW the main hub mode switcher — rendered
           inside CalendarView's header area). Reads/writes calendarSubMode. -->
      <div
        class="flex items-center gap-0.5 bg-surface-panel border border-surface-panel-border rounded-lg p-1 shadow-sm"
        role="radiogroup"
        aria-label="Calendar layout"
        tabindex="-1"
        onkeydown={onSubmodeKeydown}
      >
        <button
          type="button"
          role="radio"
          aria-checked={subMode === 'month'}
          aria-label="Month grid"
          data-testid="calendar-submode-month"
          tabindex={subMode === 'month' ? 0 : -1}
          onclick={() => chooseSubMode('month')}
          class="min-h-7 px-2.5 py-1 rounded-md font-label-sm border cursor-pointer transition-all focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-border-focus {subMode ===
          'month'
            ? 'border-accent-primary-start/30 bg-accent-primary-glow text-accent-primary-start'
            : 'border-transparent bg-transparent text-text-muted hover:bg-hover'}"
          >Month</button
        >
        <button
          type="button"
          role="radio"
          aria-checked={subMode === 'week'}
          aria-label="Week strip"
          data-testid="calendar-submode-week"
          tabindex={subMode === 'week' ? 0 : -1}
          onclick={() => chooseSubMode('week')}
          class="min-h-7 px-2.5 py-1 rounded-md font-label-sm border cursor-pointer transition-all focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-border-focus {subMode ===
          'week'
            ? 'border-accent-primary-start/30 bg-accent-primary-glow text-accent-primary-start'
            : 'border-transparent bg-transparent text-text-muted hover:bg-hover'}"
          >Week</button
        >
      </div>

      <div class="ml-auto flex items-center gap-2">
        <button
          type="button"
          onclick={openQuickAddUndated}
          data-testid="calendar-new-task-btn"
          class="flex min-h-8 items-center gap-1 rounded-lg border border-accent-primary-start/30 bg-accent-primary-glow px-2.5 py-1 text-accent-primary-start shadow-sm hover:border-accent-primary-start hover:bg-accent-primary-start hover:text-text-on-accent font-label-sm cursor-pointer transition-all focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-border-focus"
        >
          <span class="material-symbols-outlined text-icon-md">add</span>New
          task
        </button>
      </div>
    </div>

    <!-- Row 2: Date range + navigation -->
    <div class="flex items-center gap-2 flex-wrap">
      <h2 class="font-headline-md text-headline-md min-w-0 text-text-primary">
        {heading}
      </h2>
      <div class="flex items-center gap-1 ml-2">
        <button
          type="button"
          onclick={prev}
          class="flex h-8 w-8 items-center justify-center rounded-lg hover:bg-hover text-text-muted hover:text-accent-primary-start border border-transparent bg-transparent cursor-pointer focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-border-focus"
          aria-label="Previous"
        >
          <span class="material-symbols-outlined text-icon-lg"
            >chevron_left</span
          >
        </button>
        <button
          type="button"
          onclick={goToday}
          class="min-h-8 px-2.5 py-1 rounded-lg border border-surface-panel-border text-text-muted hover:bg-hover hover:text-accent-primary-start hover:border-accent-primary-start/40 font-label-sm bg-surface-panel cursor-pointer transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-border-focus"
          >Today</button
        >
        <button
          type="button"
          onclick={next}
          class="flex h-8 w-8 items-center justify-center rounded-lg hover:bg-hover text-text-muted hover:text-accent-primary-start border border-transparent bg-transparent cursor-pointer focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-border-focus"
          aria-label="Next"
        >
          <span class="material-symbols-outlined text-icon-lg"
            >chevron_right</span
          >
        </button>
      </div>
    </div>
  </header>

  {#if groupByNotice}
    <div
      class="px-6 py-1.5 border-b border-status-warn/30 bg-status-warn/10 flex items-center gap-2 text-type-sm font-body-md"
      role="status"
      data-testid="calendar-groupby-notice"
    >
      <span class="material-symbols-outlined text-icon-sm text-status-warn"
        >info</span
      >
      <span class="text-text-primary"
        >Calendar mode shows tasks by due date — group-by is ignored.</span
      >
      <button
        type="button"
        onclick={dismissGroupByNotice}
        aria-label="Dismiss notice"
        class="ml-auto p-1 rounded hover:bg-hover text-text-muted hover:text-text-primary border-none bg-transparent cursor-pointer"
      >
        <span class="material-symbols-outlined text-icon-sm">close</span>
      </button>
    </div>
  {/if}

  {#if subModeError}
    <ErrorBanner
      kind="warning"
      message={subModeError}
      dataTestId="calendar-submode-error"
      onDismiss={() => (subModeError = '')}
    />
  {/if}

  <!-- Screen-reader announcement of drag/keyboard reschedules. -->
  <div class="sr-only" role="status" aria-live="polite">
    {rescheduleAnnouncement}
  </div>

  {#if quickAddDate === ''}
    <!-- Toolbar "New task" quick-add: undated. -->
    <div
      class="px-3 py-2.5 border-b border-surface-panel-border bg-surface-panel sm:px-5 lg:px-6"
    >
      <div class="max-w-md">
        <QuickAddTask
          {ctx}
          placeholder="New task (no due date) — Enter to add"
          keepOpenAfterCreate={false}
          onCreated={closeQuickAdd}
          onCancel={closeQuickAdd}
        />
      </div>
    </div>
  {/if}

  {#if errorMsg}
    <!-- Hoisted above the padded content container so it renders as a
         full-width strip, matching BoardView/ListView's error placement. -->
    <ErrorBanner message={errorMsg} />
  {/if}
  <div class="flex-1 overflow-auto custom-scrollbar p-3 sm:p-4 lg:p-5">
    {#if loading}
      <!-- Skeleton: 6×7 day-cell grid mirroring the month layout, so the
           switch from skeleton to real grid doesn't reflow. Reuses the global
           .skeleton-text shimmer (gated by prefers-reduced-motion in
           index.css). -->
      <div
        class="grid grid-cols-7 gap-1 min-w-[43.75rem]"
        data-testid="tasks-calendar-loading"
        aria-busy="true"
        aria-label="Loading calendar"
      >
        {#each Array(42) as _, i (i)}
          <div
            class="min-h-23 rounded border border-dashed border-surface-panel-border p-1.5 space-y-1"
          >
            <div class="skeleton-text" style="width: 30%"></div>
            <div class="skeleton-text subtitle"></div>
          </div>
        {/each}
      </div>
    {:else if errorMsg}
      <!-- Fatal query error: the grid is suppressed (the banner above shows
           the message; nothing to render in the padded content area). -->
    {:else if subMode === 'month'}
      <!-- Month grid (lifted from silt-calendar). 7-column CSS grid; each day
           cell carries role="gridcell" and a data-celldate for testing/DnD.
           ARIA grid structure: container role="grid", each week a role="row",
           DOW headers role="columnheader". The row wrappers use display:contents
           so the CSS grid layout is unaffected (children participate in the
           parent grid). -->
      <div class="grid grid-cols-7 gap-1.5 min-w-[43.75rem]" role="grid">
        <div role="row" class="contents">
          {#each dayLabels as d, dowI (dowI)}
            <div
              role="columnheader"
              class="text-center text-type-2xs uppercase tracking-widest font-label-sm-bold text-text-muted py-1"
            >
              {d}
            </div>
          {/each}
        </div>
        {#each monthWeeks as week, weekIdx (weekIdx)}
          <div role="row" class="contents">
            {#each week as day, dayIdx (dayIdx)}
              {@const flatIdx = weekIdx * 7 + dayIdx}
              {@const inMonth = day.getMonth() === cursor.getMonth()}
              {@const isToday = ymd(day) === todayKey}
              {@const items = cellItems(day)}
              {@const isTodayCell = isToday}
              {@const overdueHere = isTodayCell
                ? overdueSurfaced.filter(
                    (o) => !items.some((i) => i.id === o.id)
                  )
                : []}
              <div
                role="gridcell"
                tabindex={cellFocusIdx === flatIdx ? 0 : -1}
                data-celldate={ymd(day)}
                aria-label={`${day.toDateString()}${
                  items.length + overdueHere.length
                    ? ', ' +
                      (items.length + overdueHere.length) +
                      ' task' +
                      (items.length + overdueHere.length === 1 ? '' : 's')
                    : ''
                }`}
                onkeydown={(e) => onCellKeydown(e, day)}
                ondragover={(e) => onCellDragOver(e, day)}
                ondragleave={(e) => onCellDragLeave(e, day)}
                ondrop={(e) => onCellDrop(e, day)}
                onclick={(e) => {
                  // Open quick-add when clicking the cell OR a non-interactive
                  // child (the date number / header label), but not when the
                  // click lands on a task chip button, the + button, or the
                  // quick-add input.
                  const t = e.target as HTMLElement
                  if (t.closest('button,input')) return
                  openQuickAddForDay(day)
                }}
                class="calendar-day-cell group min-h-22 rounded-lg border p-1.5 flex flex-col gap-1 transition-all focus:outline-none focus:border-accent-primary-start focus:ring-2 focus:ring-border-focus {overCellDate ===
                ymd(day)
                  ? 'calendar-day-drop-target'
                  : inMonth
                    ? 'border-surface-panel-border bg-surface-panel/70 hover:border-border-active hover:bg-surface-panel'
                    : 'border-surface-panel-border/30 bg-surface-panel/20'} {isToday
                  ? 'border-accent-primary-start/40'
                  : ''}"
              >
                <div class="flex items-center justify-between">
                  <span
                    class="text-type-xs font-label-sm-bold w-5 h-5 flex items-center justify-center rounded-full"
                    class:bg-accent-primary-start={isToday}
                    class:text-text-on-accent={isToday}
                    class:text-text-muted={!isToday && !inMonth}
                    class:text-text-primary={!isToday && inMonth}
                    >{day.getDate()}</span
                  >
                  <button
                    type="button"
                    onclick={(e) => {
                      e.stopPropagation()
                      openQuickAddForDay(day)
                    }}
                    aria-label="Add task for {ymd(day)}"
                    data-testid="calendar-day-add"
                    class="flex h-6 w-6 items-center justify-center rounded-md text-text-muted opacity-50 hover:bg-hover hover:text-accent-primary-start group-hover:opacity-100 border-none bg-transparent cursor-pointer p-0 leading-none focus-visible:opacity-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-border-focus"
                  >
                    <span class="material-symbols-outlined text-icon-sm"
                      >add</span
                    >
                  </button>
                </div>
                {#each items.slice(0, 3) as item (item.id)}
                  <button
                    type="button"
                    draggable="true"
                    aria-keyshortcuts="Enter Shift+Enter Alt+ArrowLeft Alt+ArrowRight Alt+ArrowUp Alt+ArrowDown"
                    ondragstart={(e) => onCardDragStart(e, item)}
                    ondragend={onCardDragEnd}
                    onkeydown={(e) => onCardKeydown(e, item)}
                    onclick={() => (selectedTask = item)}
                    data-status={item.status}
                    class="calendar-task-chip text-left text-type-2xs truncate px-1.5 py-1 rounded-md bg-surface-card border border-surface-card-border text-text-primary hover:border-accent-primary-start/40 hover:bg-hover transition-all cursor-pointer focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-border-focus {dragTaskId ===
                    item.id
                      ? 'opacity-40'
                      : ''}"
                    title={item.clean_content}>{item.clean_content}</button
                  >
                {/each}
                {#if items.length > 3}
                  <span class="text-type-3xs text-text-muted px-1"
                    >+{items.length - 3} more</span
                  >
                {/if}
                <!-- Overdue open tasks surface in today's cell with an error-tone
                   stripe so they're not lost in past days. They ALSO remain in
                   their actual due-date cell above so the truth stays visible. -->
                {#each overdueHere.slice(0, 2) as item (item.id)}
                  <button
                    type="button"
                    draggable="true"
                    data-overdue-surfaced="true"
                    aria-keyshortcuts="Enter Shift+Enter Alt+ArrowLeft Alt+ArrowRight Alt+ArrowUp Alt+ArrowDown"
                    ondragstart={(e) => onCardDragStart(e, item)}
                    ondragend={onCardDragEnd}
                    onkeydown={(e) => onCardKeydown(e, item)}
                    onclick={() => (selectedTask = item)}
                    class="text-left text-type-2xs truncate px-1 py-0.5 rounded bg-error-bg border-l-2 border-l-error text-error hover:brightness-110 transition-all cursor-pointer {dragTaskId ===
                    item.id
                      ? 'opacity-40'
                      : ''}"
                    title="Overdue (was due {item.due_date}): {item.clean_content}"
                    aria-label="Overdue (was due {item.due_date}): {item.clean_content}"
                    >{item.clean_content}</button
                  >
                {/each}
                {#if overdueHere.length > 2}
                  <span class="text-type-3xs text-error px-1"
                    >+{overdueHere.length - 2} overdue</span
                  >
                {/if}
                {#if quickAddDate === ymd(day)}
                  <QuickAddTask
                    {ctx}
                    dueDate={ymd(day)}
                    keepOpenAfterCreate={false}
                    onCreated={closeQuickAdd}
                    onCancel={closeQuickAdd}
                  />
                {/if}
              </div>
            {/each}
          </div>
        {/each}
      </div>
    {:else}
      <!-- Week view: 7 day columns (lifted from silt-calendar). ARIA grid
           structure mirrors month view: role="grid" + role="row" wrapper. -->
      <div class="grid grid-cols-7 gap-2 min-w-[43.75rem]" role="grid">
        <div role="row" class="contents">
          {#each weekDays as day, i (i)}
            {@const isToday = ymd(day) === todayKey}
            {@const items = cellItems(day)}
            {@const overdueHere = isToday
              ? overdueSurfaced.filter((o) => !items.some((i) => i.id === o.id))
              : []}
            <div
              class="calendar-week-day flex min-h-40 flex-col gap-1.5 rounded-xl border border-surface-panel-border bg-surface-panel/50 p-2 transition-all hover:border-border-active {overCellDate ===
              ymd(day)
                ? 'calendar-day-drop-target'
                : ''}"
              role="gridcell"
              tabindex={cellFocusIdx === i ? 0 : -1}
              data-celldate={ymd(day)}
              aria-label={`${day.toDateString()}${
                items.length + overdueHere.length
                  ? ', ' +
                    (items.length + overdueHere.length) +
                    ' task' +
                    (items.length + overdueHere.length === 1 ? '' : 's')
                  : ''
              }`}
              ondragover={(e) => onCellDragOver(e, day)}
              ondragleave={(e) => onCellDragLeave(e, day)}
              ondrop={(e) => onCellDrop(e, day)}
              onclick={(e) => {
                const t = e.target as HTMLElement
                if (t.closest('button,input')) return
                openQuickAddForDay(day)
              }}
              onkeydown={(e) => onCellKeydown(e, day)}
            >
              <div
                class="flex items-center justify-between pb-2 border-b border-surface-panel-border"
              >
                <div>
                  <div
                    class="text-type-2xs uppercase tracking-widest font-label-sm-bold text-text-muted"
                  >
                    {DOW[day.getDay()]}
                  </div>
                  <span
                    class="inline-flex items-center justify-center w-7 h-7 rounded-full text-type-md font-label-sm-bold mt-1"
                    class:bg-accent-primary-start={isToday}
                    class:text-text-on-accent={isToday}
                    class:text-text-primary={!isToday}>{day.getDate()}</span
                  >
                </div>
                <button
                  type="button"
                  onclick={(e) => {
                    e.stopPropagation()
                    openQuickAddForDay(day)
                  }}
                  aria-label="Add task for {ymd(day)}"
                  data-testid="calendar-day-add"
                  class="text-text-muted hover:text-accent-primary-start border-none bg-transparent cursor-pointer p-0 leading-none"
                >
                  <span class="material-symbols-outlined text-icon-sm">add</span
                  >
                </button>
              </div>
              {#each items as item (item.id)}
                <button
                  type="button"
                  draggable="true"
                  aria-keyshortcuts="Enter Shift+Enter Alt+ArrowLeft Alt+ArrowRight Alt+ArrowUp Alt+ArrowDown"
                  ondragstart={(e) => onCardDragStart(e, item)}
                  ondragend={onCardDragEnd}
                  onkeydown={(e) => onCardKeydown(e, item)}
                  onclick={() => (selectedTask = item)}
                  data-status={item.status}
                  class="calendar-task-chip text-left text-type-sm px-2 py-1.5 rounded-md bg-surface-card border border-surface-card-border hover:border-accent-primary-start/40 hover:bg-hover text-text-primary shadow-sm transition-all cursor-pointer focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-border-focus {dragTaskId ===
                  item.id
                    ? 'opacity-40'
                    : ''}"
                  title={item.clean_content}>{item.clean_content}</button
                >
              {/each}
              {#each overdueHere as item (item.id)}
                <button
                  type="button"
                  draggable="true"
                  data-overdue-surfaced="true"
                  aria-keyshortcuts="Enter Shift+Enter Alt+ArrowLeft Alt+ArrowRight Alt+ArrowUp Alt+ArrowDown"
                  ondragstart={(e) => onCardDragStart(e, item)}
                  ondragend={onCardDragEnd}
                  onkeydown={(e) => onCardKeydown(e, item)}
                  onclick={() => (selectedTask = item)}
                  class="text-left text-type-sm px-2 py-1.5 rounded bg-error-bg border-l-2 border-l-error text-error transition-all cursor-pointer {dragTaskId ===
                  item.id
                    ? 'opacity-40'
                    : ''}"
                  title="Overdue (was due {item.due_date}): {item.clean_content}"
                  aria-label="Overdue (was due {item.due_date}): {item.clean_content}"
                  >{item.clean_content}</button
                >
              {/each}
              {#if quickAddDate === ymd(day)}
                <QuickAddTask
                  {ctx}
                  dueDate={ymd(day)}
                  keepOpenAfterCreate={false}
                  onCreated={closeQuickAdd}
                  onCancel={closeQuickAdd}
                />
              {/if}
            </div>
          {/each}
        </div>
      </div>
    {/if}

    <!-- "No Date" strip: undated tasks. Always visible when there are any.
         Drop a grid chip here → clears due date; drag a strip chip onto a day
         cell → sets due date (handled by the day cell's ondrop above). -->
    {#if undated.length > 0}
      <section
        class="mt-4 p-3 rounded-xl border border-surface-panel-border bg-surface-panel/50 transition-all {overNoDate
          ? 'border-accent-primary-start ring-2 ring-accent-primary-glow'
          : ''}"
        aria-label="No Date tasks"
        data-testid="calendar-no-date-strip"
        ondragover={onNoDateDragOver}
        ondragleave={onNoDateDragLeave}
        ondrop={onNoDateDrop}
      >
        <div class="flex items-center gap-2 mb-2">
          <span class="material-symbols-outlined text-icon-sm text-text-muted"
            >event_busy</span
          >
          <h3
            class="font-label-sm-bold uppercase tracking-widest text-type-2xs text-text-muted"
          >
            No Date
          </h3>
          <span
            class="bg-hover text-text-muted text-type-2xs px-1.5 py-0.5 rounded-sm font-label-sm"
            >{undated.length}</span
          >
        </div>
        <ul class="flex flex-wrap gap-1.5">
          {#each undated as item (item.id)}
            <li>
              <button
                type="button"
                draggable="true"
                aria-keyshortcuts="Enter Shift+Enter Alt+ArrowLeft Alt+ArrowRight Alt+ArrowUp Alt+ArrowDown"
                ondragstart={(e) => onCardDragStart(e, item)}
                ondragend={onCardDragEnd}
                onkeydown={(e) => onCardKeydown(e, item)}
                onclick={() => (selectedTask = item)}
                class="text-left text-type-xs truncate max-w-55 px-2 py-1 rounded bg-surface-card border border-surface-card-border text-text-primary hover:border-accent-primary-start/40 transition-all cursor-pointer {dragTaskId ===
                item.id
                  ? 'opacity-40'
                  : ''}"
                title={item.clean_content}>{item.clean_content}</button
              >
            </li>
          {/each}
        </ul>
      </section>
    {/if}
  </div>
</div>

<TaskEditDrawer
  task={selectedTask}
  {ctx}
  onClose={() => (selectedTask = null)}
  onMetaChanged={reload}
  onOpenSubEditor={() => selectedTask && (subEditorTask = selectedTask)}
/>

{#if subEditorTask}
  <TaskSubEditorModal
    blockId={subEditorTask.id}
    notebook={subEditorTask.notebook}
    section={subEditorTask.section}
    page={subEditorTask.page}
    parentTaskText={subEditorTask.clean_content}
    {ctx}
    onClose={() => {
      void reload()
      subEditorTask = null
    }}
  />
{/if}

<style>
  .calendar-toolbar {
    background: color-mix(
      in srgb,
      var(--color-surface-panel) 72%,
      var(--color-surface-app)
    );
  }

  .calendar-day-drop-target {
    border-color: var(--color-accent-primary-start);
    background: var(--color-accent-primary-glow);
    box-shadow: inset 0 0 0 1px var(--color-accent-primary-start);
    transform: translateY(-1px);
  }

  .calendar-task-chip {
    position: relative;
    padding-left: 0.625rem;
  }

  .calendar-task-chip::before {
    content: '';
    position: absolute;
    inset: 0.25rem auto 0.25rem 0.2rem;
    width: 2px;
    border-radius: var(--radius-full);
    background: var(--color-text-muted);
    opacity: 0.5;
  }

  .calendar-task-chip[data-status='DOING']::before {
    background: var(--color-accent-secondary-start);
    opacity: 1;
  }

  .calendar-task-chip[data-status='DONE']::before {
    background: var(--color-accent-primary-start);
    opacity: 1;
  }

  @media (prefers-reduced-motion: reduce) {
    .calendar-day-cell,
    .calendar-week-day,
    .calendar-task-chip {
      transition: none;
    }
  }
</style>
