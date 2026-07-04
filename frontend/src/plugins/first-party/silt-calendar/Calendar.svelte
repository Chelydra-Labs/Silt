<script lang="ts">
  import { onMount, onDestroy } from 'svelte'
  import type { PluginContext, PluginManifest } from '../../sdk'
  import { plusDaysISO } from '../../sdk'
  import { settings, updatePluginSetting } from '../../../settings/store.svelte'
  import { getFocusState } from './focusState.svelte'
  import AgendaList from './AgendaList.svelte'
  import QuickAddTask from '../shared/QuickAddTask.svelte'

  interface Props {
    ctx: PluginContext
    manifest: PluginManifest
  }

  let { ctx, manifest }: Props = $props()

  interface CalItem {
    id: string
    notebook: string
    section: string
    page: string
    file_date: string
    clean_content: string
    status: string
    due_date: string
  }

  // Calendar/Agenda unified view (#322). The mode is persisted to the
  // plugin's settings so a user who prefers the agenda list keeps it
  // across reloads; the default is 'month' for parity with the previous
  // standalone Calendar.
  type ViewMode = 'month' | 'week' | 'agenda'
  function initialMode(): ViewMode {
    const cfgMode = settings.config?.plugins?.plugin_settings?.['silt-calendar']
      ?.view_mode as ViewMode | undefined
    return cfgMode === 'week' || cfgMode === 'agenda' ? cfgMode : 'month'
  }
  let mode = $state<ViewMode>(initialMode())
  let modeLoaded = $state(false)
  // Anchor date for the visible window.
  let cursor = $state(new Date())
  let byDate = $state<Record<string, CalItem[]>>({})
  let loading = $state(true)
  let errorMsg = $state('')
  let agendaTaskCount = $state(0)

  // Reactive "now" so the today-highlight updates if the calendar stays
  // mounted past midnight (ticks every 60s; only re-evaluates isToday).
  let nowTick = $state(0)
  let nowInterval: ReturnType<typeof setInterval> | undefined
  // Repaint the grid when any block changes (task created/mutated/rescheduled
  // from any surface). The calendar's sidebar + agenda sub-views already
  // subscribe; the main grid now does too so quick-add and drag-and-drop
  // reschedule land immediately. Debounced so a burst of block:changed events
  // (e.g. a bulk op) triggers one reload.
  let blockChangedTimer: ReturnType<typeof setTimeout> | null = null
  let unsubBlockChanged: (() => void) | null = null

  onMount(() => {
    reload()
    nowInterval = setInterval(() => {
      nowTick++
    }, 60_000)
    unsubBlockChanged = ctx.on('block:changed', () => {
      if (mode === 'agenda') return // AgendaList handles its own refresh
      if (blockChangedTimer) clearTimeout(blockChangedTimer)
      blockChangedTimer = setTimeout(() => void reload(), 80)
    })
  })
  onDestroy(() => {
    if (nowInterval) clearInterval(nowInterval)
    if (blockChangedTimer) clearTimeout(blockChangedTimer)
    unsubBlockChanged?.()
  })

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

  function ymd(d: Date) {
    return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(
      d.getDate()
    ).padStart(2, '0')}`
  }
  function startOfWeek(d: Date) {
    const x = new Date(d)
    x.setDate(x.getDate() - x.getDay())
    x.setHours(0, 0, 0, 0)
    return x
  }
  function startOfMonth(d: Date) {
    return new Date(d.getFullYear(), d.getMonth(), 1)
  }
  function endOfMonth(d: Date) {
    return new Date(d.getFullYear(), d.getMonth() + 1, 0)
  }
  function addMonths(d: Date, n: number) {
    return new Date(d.getFullYear(), d.getMonth() + n, 1)
  }
  function addDays(d: Date, n: number) {
    const x = new Date(d)
    x.setDate(x.getDate() + n)
    return x
  }
  function isSameDay(a: Date, b: Date) {
    return ymd(a) === ymd(b)
  }

  // Month grid: weeks of Sunday..Saturday covering the month.
  let monthWeeks = $derived.by(() => {
    if (mode !== 'month') return []
    const first = startOfWeek(startOfMonth(cursor))
    const weeks: Date[][] = []
    let cur = first
    const monthEnd = endOfMonth(cursor)
    // 6 rows covers any month.
    for (let w = 0; w < 6; w++) {
      const row: Date[] = []
      for (let i = 0; i < 7; i++) {
        row.push(cur)
        cur = addDays(cur, 1)
      }
      weeks.push(row)
      if (cur > monthEnd && w >= 3) break
    }
    return weeks
  })

  let weekDays = $derived.by(() => {
    if (mode !== 'week') return []
    const first = startOfWeek(cursor)
    return Array.from({ length: 7 }, (_, i) => addDays(first, i))
  })

  let windowStart = $derived(
    mode === 'month' ? startOfWeek(startOfMonth(cursor)) : startOfWeek(cursor)
  )
  let windowEnd = $derived(
    mode === 'month'
      ? addDays(startOfWeek(endOfMonth(cursor)), 6)
      : addDays(startOfWeek(cursor), 6)
  )

  let heading = $derived(
    mode === 'month'
      ? `${MONTHS[cursor.getMonth()]} ${cursor.getFullYear()}`
      : `${MONTHS[cursor.getMonth()]} ${startOfWeek(cursor).getDate()}–${addDays(startOfWeek(cursor), 6).getDate()}, ${cursor.getFullYear()}`
  )

  async function reload() {
    loading = true
    errorMsg = ''
    try {
      const s = ymd(windowStart)
      const e = ymd(windowEnd)
      const { rows } = await ctx.sqliteQuery(
        `SELECT b.id, b.notebook, b.section, b.page, b.file_date,
                b.clean_content, t.status, t.due_date
         FROM blocks b JOIN tasks t ON b.id = t.block_id
         WHERE t.due_date IS NOT NULL AND t.due_date != ''
           AND t.due_date >= ? AND t.due_date <= ?
         ORDER BY t.due_date ASC, t.priority ASC`,
        [s, e]
      )
      const bucket: Record<string, CalItem[]> = {}
      for (const r of rows as unknown as CalItem[]) {
        if (!r.due_date) continue
        ;(bucket[r.due_date] ||= []).push(r)
      }
      byDate = bucket
    } catch (e) {
      errorMsg = e instanceof Error ? e.message : String(e)
    } finally {
      loading = false
    }
  }

  // Reload whenever the visible window shifts. Skipped in agenda mode
  // — the AgendaList subcomponent handles its own query/refresh
  // (#322). Without this guard, a focusDate change in agenda mode
  // would mutate cursor (which we skip above) and the reload would
  // not fire anyway. Belt-and-suspenders.
  $effect(() => {
    if (mode === 'agenda') return
    void windowStart
    void windowEnd
    reload()
  })

  function prev() {
    cursor = mode === 'month' ? addMonths(cursor, -1) : addDays(cursor, -7)
  }
  function next() {
    cursor = mode === 'month' ? addMonths(cursor, 1) : addDays(cursor, 7)
  }
  function goToday() {
    cursor = new Date()
  }

  function openItem(item: CalItem) {
    window.dispatchEvent(
      new CustomEvent('navigate-to-block', {
        detail: {
          notebook: item.notebook,
          section: item.section,
          page: item.page,
          date: item.file_date,
          blockId: item.id
        }
      })
    )
  }

  // --- Drag-and-drop task rescheduling (#292/#293/#294) --------------------
  // A task card is draggable onto any day cell (month or week). On drop the
  // [due:: YYYY-MM-DD] token is rewritten on disk via ctx.setTaskDueDate
  // (#293), the block:changed listener (subscribed in onMount) repaints the
  // grid. HTML5 DnD has no keyboard semantics, so a focused card also
  // responds to Alt+Arrows (±1 day / ±7 days) through the SAME reschedule
  // path — the mouse and keyboard flows call one function (#294).

  let dragTaskId = $state<string | null>(null)
  // The dragged card's full item is captured so (a) the mouse-drop path can
  // skip a no-op drop on the task's own current cell (avoids a wasted atomic
  // write + re-index + repaint for zero semantic effect), and (b) the
  // aria-live announcement can include the title for parity with the keyboard
  // path (#292 AC).
  let dragTaskItem = $state<CalItem | null>(null)
  let overCellDate = $state<string | null>(null)
  // aria-live announcement of the last reschedule (mouse or keyboard) so
  // screen-reader users hear the result (#292 AC).
  let rescheduleAnnouncement = $state('')

  function onCardDragStart(e: DragEvent, item: CalItem) {
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
  }

  function onCellDragOver(e: DragEvent, day: Date) {
    // preventDefault is required for a cell to accept a drop.
    e.preventDefault()
    if (e.dataTransfer) e.dataTransfer.dropEffect = 'move'
    const key = ymd(day)
    if (overCellDate !== key) overCellDate = key
  }
  function onCellDragLeave(e: DragEvent, day: Date) {
    // Only clear when truly leaving this cell (not entering a child element).
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
    // No-op guard: dropping a card back on its own current due-date cell would
    // otherwise round-trip through an atomic write + full re-parse + re-index +
    // repaint for zero semantic effect.
    if (item && item.due_date === target) {
      rescheduleAnnouncement = `Already scheduled for ${target}`
      return
    }
    await reschedule(id, target, item?.clean_content)
  }

  // reschedule rewrites the due date and announces the result. Shared by the
  // mouse drop path and the keyboard Alt+Arrow path so there is one source
  // of truth for the mutation + announcement. title is optional — the
  // keyboard path has the card's item; the mouse-drop path knows only the id,
  // so the announcement degrades gracefully to a date-only message.
  async function reschedule(blockId: string, newDate: string, title?: string) {
    try {
      await ctx.setTaskDueDate(blockId, newDate)
      // The grid repaints via the block:changed listener; the announcement
      // gives immediate non-visual feedback.
      rescheduleAnnouncement = title
        ? `Rescheduled "${title}" to ${newDate}`
        : `Rescheduled to ${newDate}`
    } catch (e) {
      rescheduleAnnouncement =
        e instanceof Error ? e.message : 'Reschedule failed'
    }
  }

  // Keyboard reschedule on a focused task card (#294): Alt+ArrowLeft/Right
  // shifts ±1 day, Alt+ArrowUp/Down shifts ±7 days. The cell's own arrow
  // handler ignores Alt-modified keys so the two don't collide.
  function onCardKeydown(e: KeyboardEvent, item: CalItem) {
    if (!e.altKey) return
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
    const base = item.due_date || ymd(new Date())
    void reschedule(item.id, plusDaysISO(base, delta), item.clean_content)
  }

  // --- Quick-add standalone tasks (#368) ------------------------------------
  // Two surfaces: (1) clicking the empty area of a day cell opens an inline
  // quick-add prefilled with that day's date; (2) the "New task" toolbar
  // button opens one with no due date (the task lands in Agenda / un-dated).
  // Both route through ctx.createTask; the block:changed listener repaints.
  let quickAddDate = $state<string | null>(null) // null = closed; a date = open

  function openQuickAddForDay(day: Date) {
    quickAddDate = ymd(day)
  }
  function openQuickAddUndated() {
    quickAddDate = '' // empty string = open, undated
  }
  function closeQuickAdd() {
    quickAddDate = null
  }

  // Keyboard navigation across month cells: arrows move focus by day (clamping
  // to the grid), Enter opens the focused day's first task.
  function onCellKeydown(e: KeyboardEvent, day: Date) {
    // Alt-modified arrows are handled by the focused task card's reschedule
    // handler (#294); let them through.
    if (e.altKey) return
    const map: Record<string, number> = {
      ArrowRight: 1,
      ArrowLeft: -1,
      ArrowDown: 7,
      ArrowUp: -7
    }
    const delta = map[e.key]
    if (delta === undefined) return
    e.preventDefault()
    const grid = monthWeeks.flat()
    const idx = grid.findIndex((d) => ymd(d) === ymd(day))
    if (idx < 0) return
    const next = Math.min(Math.max(idx + delta, 0), grid.length - 1)
    const targetDt = grid[next]
    if (!targetDt) return
    const el = document.querySelector<HTMLElement>(
      `[data-celldate="${ymd(targetDt)}"]`
    )
    el?.focus()
  }

  // Reactive today string — re-evaluates when the 60s tick fires so the
  // today-highlight updates if the calendar stays mounted past midnight.
  let todayKey = $derived.by(() => {
    void nowTick
    return ymd(new Date())
  })

  // Persist view_mode to plugin settings (debounced via the same atomic
  // UpdatePluginSetting path Kanban uses for columns/filters, #120).
  // Surface a save failure as `modeError` so the user knows their mode
  // pick won't survive a reload (matches the `configError` pattern in
  // Kanban.svelte). Without this, a silent saveConfig rejection leaves
  // the user wondering why their mode reverts on restart.
  let modeSaveTimer: ReturnType<typeof setTimeout> | null = null
  let modeError = $state('')
  onDestroy(() => {
    if (modeSaveTimer) clearTimeout(modeSaveTimer)
  })
  $effect(() => {
    const m = mode
    // Skip the very first run that re-reads the persisted value back —
    // that would be a no-op write of the value we just loaded.
    if (!modeLoaded) {
      modeLoaded = true
      return
    }
    if (modeSaveTimer) clearTimeout(modeSaveTimer)
    modeSaveTimer = setTimeout(() => {
      void persistMode(m)
    }, 400)
  })
  async function persistMode(m: ViewMode) {
    if (!settings.config) return
    modeError = ''
    const ok = await updatePluginSetting('silt-calendar', 'view_mode', m)
    if (!ok) modeError = settings.error || "Couldn't save view mode"
  }

  // React to the sidebar's focusDate: jump the main view's cursor to the
  // matching month/week so the user sees the day they clicked. For
  // agenda mode the AgendaList subcomponent scrolls the matching group
  // itself, so we only need the cursor jump for month/week. Skip the
  // cursor write in agenda mode so the reload $effect at line ~174
  // doesn't fire on focusDate changes — agenda doesn't read cursor,
  // and re-running reload() in agenda mode means a wasted IPC against
  // the SQLite index that AgendaList then ignores.
  $effect(() => {
    if (mode === 'agenda') return
    const focus = getFocusState().focusDate
    if (!focus) {
      // No focus date — reset cursor to today so the user lands back on
      // the current month after clearing a jump (#323 hardening). This
      // also covers the "user dismissed the jump" path from the
      // mini-calendar's "Clear jump date" affordance.
      cursor = new Date()
      return
    }
    const [y, m, d] = focus.split('-').map(Number)
    if (!y || !m || !d) return
    cursor = new Date(y, m - 1, d)
  })

  // Dim class helper: a month/week cell with a due-date task that does
  // NOT match the active smart-list filter gets an opacity-30 ring so
  // the user can focus on the matching slice without hiding the others
  // entirely (industry-standard parity — Things 3 / MS To Do dim rather
  // than hide).
  function itemMatchesFilter(item: CalItem): boolean {
    const f = getFocusState().activeFilter
    if (f === 'all') return true
    const t = todayKey
    if (f === 'overdue') return item.due_date < t
    // "Today" smart list = exactly due today. Overdue tasks are NOT
    // dimmed by the Today filter — they live in the separate Overdue
    // smart list. Matches the SQL bucket in CalendarSidebar.
    if (f === 'today') return item.due_date === t
    // "Upcoming" = strictly future (today is its own smart list).
    // Matches the SQL bucket in CalendarSidebar which also excludes
    // today from the Upcoming count. Clicking the Upcoming badge and
    // the filter result must agree.
    if (f === 'upcoming')
      return item.due_date > t && item.due_date <= plusDaysISO(t, 7)
    // The month/week grid query loads tasks without a status filter,
    // so DONE tasks are present here and brighten under "Completed";
    // non-DONE tasks dim out.
    if (f === 'completed') return item.status === 'DONE'
    return true
  }
</script>

<div class="flex-1 flex flex-col min-h-0 overflow-hidden">
  <header class="px-6 py-4 border-b border-border-muted flex flex-col gap-3">
    <!-- Row 1: Icon, Title, Switcher, New Task -->
    <div class="flex items-center gap-3 w-full flex-wrap">
      <span class="material-symbols-outlined text-accent-primary-start">
        {mode === 'agenda' ? 'event_repeat' : 'calendar_month'}
      </span>
      <h1
        class="font-headline-lg text-headline-lg text-text-primary flex items-baseline gap-2"
      >
        {mode === 'agenda' ? 'Agenda' : (manifest?.name ?? 'Calendar')}
        {#if mode === 'agenda'}
          <span
            class="text-text-muted text-[12px] font-body-md normal-case font-normal ml-2"
            data-testid="agenda-active-count"
          >
            {agendaTaskCount} active task{agendaTaskCount === 1 ? '' : 's'}
          </span>
        {/if}
      </h1>

      <div class="ml-auto flex items-center gap-2">
        <div
          class="flex items-center gap-0.5 bg-surface border border-border-muted rounded-lg p-0.5"
        >
          <button
            onclick={() => (mode = 'month')}
            class="px-2.5 py-1 rounded font-label-sm border-none cursor-pointer transition-colors"
            class:bg-hover={mode === 'month'}
            class:text-accent-primary-start={mode === 'month'}
            class:text-text-muted={mode !== 'month'}>Month</button
          >
          <button
            onclick={() => (mode = 'week')}
            class="px-2.5 py-1 rounded font-label-sm border-none cursor-pointer transition-colors"
            class:bg-hover={mode === 'week'}
            class:text-accent-primary-start={mode === 'week'}
            class:text-text-muted={mode !== 'week'}>Week</button
          >
          <button
            onclick={() => (mode = 'agenda')}
            class="px-2.5 py-1 rounded font-label-sm border-none cursor-pointer transition-colors"
            class:bg-hover={mode === 'agenda'}
            class:text-accent-primary-start={mode === 'agenda'}
            class:text-text-muted={mode !== 'agenda'}>Agenda</button
          >
        </div>
        <button
          type="button"
          onclick={openQuickAddUndated}
          data-testid="calendar-new-task-btn"
          class="flex items-center gap-1 px-2.5 py-1 rounded border border-accent-primary-start/40 text-accent-primary-start hover:bg-accent-primary-glow font-label-sm bg-transparent cursor-pointer transition-colors"
        >
          <span class="material-symbols-outlined text-[16px]">add</span>New task
        </button>
      </div>
    </div>

    <!-- Row 2: Date Range & Navigation (only for Month/Week views) -->
    {#if mode !== 'agenda'}
      <div class="flex items-center gap-2 flex-wrap">
        <h2 class="font-headline-md text-headline-md text-text-primary">
          {heading}
        </h2>
        <div class="flex items-center gap-1 ml-2">
          <button
            onclick={prev}
            class="p-1.5 rounded hover:bg-hover text-text-muted hover:text-accent-primary-start border-none bg-transparent cursor-pointer"
            aria-label="Previous"
          >
            <span class="material-symbols-outlined text-[18px]"
              >chevron_left</span
            >
          </button>
          <button
            onclick={goToday}
            class="px-2.5 py-1 rounded border border-border-muted text-text-muted hover:text-accent-primary-start hover:border-accent-primary-start/40 font-label-sm border bg-transparent cursor-pointer transition-colors"
            >Today</button
          >
          <button
            onclick={next}
            class="p-1.5 rounded hover:bg-hover text-text-muted hover:text-accent-primary-start border-none bg-transparent cursor-pointer"
            aria-label="Next"
          >
            <span class="material-symbols-outlined text-[18px]"
              >chevron_right</span
            >
          </button>
        </div>
      </div>
    {/if}
  </header>

  {#if getFocusState().activeFilter !== 'all' && mode !== 'agenda'}
    <div
      class="px-6 py-1.5 border-b border-border-muted bg-accent-primary-glow flex items-center gap-2 text-[12px] font-body-md"
      role="status"
      aria-live="polite"
    >
      <span
        class="material-symbols-outlined text-[14px] text-accent-primary-start"
        >filter_alt</span
      >
      <span class="text-text-primary"
        >Filtered by: <strong>{getFocusState().activeFilter}</strong></span
      >
      <button
        type="button"
        onclick={() => {
          // Mirror the sidebar's X affordance: clear filter.
          const ev = new CustomEvent('calendar:clear-filter')
          window.dispatchEvent(ev)
        }}
        aria-label="Clear filter"
        class="ml-auto p-1 rounded hover:bg-hover text-text-muted hover:text-error border-none bg-transparent cursor-pointer"
      >
        <span class="material-symbols-outlined text-[14px]">close</span>
      </button>
    </div>
  {/if}

  {#if modeError}
    <div
      class="px-6 py-1.5 border-b border-yellow-500/30 bg-yellow-500/10 flex items-center gap-2 text-[12px] font-body-md"
      role="alert"
      data-testid="mode-error"
    >
      <span class="material-symbols-outlined text-[14px] text-yellow-300"
        >save</span
      >
      <span class="text-text-primary">{modeError}</span>
      <button
        type="button"
        onclick={() => (modeError = '')}
        aria-label="Dismiss error"
        class="ml-auto p-1 rounded hover:bg-hover text-text-muted border-none bg-transparent cursor-pointer"
      >
        <span class="material-symbols-outlined text-[14px]">close</span>
      </button>
    </div>
  {/if}

  {#if mode !== 'agenda'}
    <!-- Screen-reader announcement of drag/keyboard reschedules (#292 AC). -->
    <div class="sr-only" role="status" aria-live="polite">
      {rescheduleAnnouncement}
    </div>
  {/if}

  {#if quickAddDate === ''}
    <!-- Toolbar "New task" quick-add: undated. The created task lands in the
         Agenda / un-dated list (#368). -->
    <div class="px-6 py-2 border-b border-border-muted bg-panel">
      <div class="max-w-md">
        <QuickAddTask
          {ctx}
          placeholder="New task (no due date) — Enter to add, Esc to close"
          keepOpenAfterCreate={false}
          onCreated={closeQuickAdd}
          onCancel={closeQuickAdd}
        />
      </div>
    </div>
  {/if}

  {#if mode === 'agenda'}
    <!-- Agenda mode renders the extracted grouped-list component. The
         shared focusState drives its scroll-to-group and dim behaviour. -->
    <AgendaList {ctx} {manifest} bind:taskCount={agendaTaskCount} />
  {:else}
    <div class="flex-1 overflow-auto custom-scrollbar p-4">
      {#if loading}
        <div class="text-text-muted animate-pulse p-6">Loading…</div>
      {:else if errorMsg}
        <div class="text-error p-6">{errorMsg}</div>
      {:else if mode === 'month'}
        <!-- Month grid -->
        <div class="grid grid-cols-7 gap-1 min-w-[700px]">
          {#each DOW as d}
            <div
              class="text-center text-[10px] uppercase tracking-widest font-label-sm-bold text-text-muted py-1"
            >
              {d}
            </div>
          {/each}
          {#each monthWeeks as week}
            {#each week as day}
              {@const inMonth = day.getMonth() === cursor.getMonth()}
              {@const isToday = ymd(day) === todayKey}
              {@const items = byDate[ymd(day)] ?? []}
              <div
                role="gridcell"
                tabindex="0"
                data-celldate={ymd(day)}
                aria-label={`${day.toDateString()}${items.length ? ', ' + items.length + ' task' + (items.length === 1 ? '' : 's') : ''}`}
                aria-dropeffect="move"
                onkeydown={(e) => {
                  if (e.key === 'Enter') {
                    e.preventDefault()
                    if (items[0]) openItem(items[0])
                    else openQuickAddForDay(day)
                  } else {
                    onCellKeydown(e, day)
                  }
                }}
                ondragover={(e) => onCellDragOver(e, day)}
                ondragleave={(e) => onCellDragLeave(e, day)}
                ondrop={(e) => onCellDrop(e, day)}
                onclick={(e) => {
                  // Open quick-add when clicking the cell OR a non-interactive
                  // child (the date number / header label), but not when the
                  // click lands on a task card button or the quick-add input.
                  const t = e.target as HTMLElement
                  if (t.closest('button,input')) return
                  openQuickAddForDay(day)
                }}
                class="min-h-[88px] rounded-lg border p-1.5 flex flex-col gap-0.5 transition-all focus:outline-none focus:border-accent-primary-start focus:ring-1 focus:ring-accent-primary-start/40 {overCellDate ===
                ymd(day)
                  ? 'border-accent-primary-glow ring-2 ring-accent-primary-glow/40'
                  : inMonth
                    ? 'border-border-muted bg-panel'
                    : 'border-border-muted/30 bg-transparent'}"
              >
                <span
                  class="text-[11px] font-label-sm-bold w-5 h-5 flex items-center justify-center rounded-full"
                  class:bg-accent-primary-start={isToday}
                  class:text-void={isToday}
                  class:text-text-muted={!isToday && !inMonth}
                  class:text-text-primary={!isToday && inMonth}
                  >{day.getDate()}</span
                >
                {#each items.slice(0, 3) as item (item.id)}
                  <button
                    draggable="true"
                    aria-keyshortcuts="Alt+ArrowLeft Alt+ArrowRight Alt+ArrowUp Alt+ArrowDown"
                    ondragstart={(e) => onCardDragStart(e, item)}
                    ondragend={onCardDragEnd}
                    onkeydown={(e) => onCardKeydown(e, item)}
                    onclick={() => openItem(item)}
                    class="text-left text-[10px] truncate px-1 py-0.5 rounded bg-accent-primary-glow border border-accent-primary-start/20 text-accent-primary-start hover:brightness-110 transition-all cursor-pointer {dragTaskId ===
                    item.id
                      ? 'opacity-40'
                      : ''}"
                    class:opacity-30={!itemMatchesFilter(item)}
                    title={item.clean_content}>{item.clean_content}</button
                  >
                {/each}
                {#if items.length > 3}
                  <span class="text-[9px] text-text-muted px-1"
                    >+{items.length - 3} more</span
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
          {/each}
        </div>
      {:else}
        <!-- Week view: day columns -->
        <div class="grid grid-cols-7 gap-2 min-w-[700px]">
          {#each weekDays as day}
            {@const isToday = ymd(day) === todayKey}
            {@const items = byDate[ymd(day)] ?? []}
            <div
              class="flex flex-col gap-1.5 min-h-[120px]"
              role="gridcell"
              tabindex="0"
              data-celldate={ymd(day)}
              aria-label={`${day.toDateString()}${items.length ? ', ' + items.length + ' task' + (items.length === 1 ? '' : 's') : ''}`}
              aria-dropeffect="move"
              ondragover={(e) => onCellDragOver(e, day)}
              ondragleave={(e) => onCellDragLeave(e, day)}
              ondrop={(e) => onCellDrop(e, day)}
              onclick={(e) => {
                const t = e.target as HTMLElement
                if (t.closest('button,input')) return
                openQuickAddForDay(day)
              }}
              onkeydown={(e) => {
                // Enter on an empty focused cell opens quick-add (keyboard
                // parity with the click affordance); arrow keys move focus.
                if (e.key === 'Enter' && e.target === e.currentTarget) {
                  e.preventDefault()
                  openQuickAddForDay(day)
                }
              }}
              class:ring-2={overCellDate === ymd(day)}
              class:ring-accent-primary-glow={overCellDate === ymd(day)}
              class:rounded-lg={overCellDate === ymd(day)}
            >
              <div class="text-center pb-2 border-b border-border-muted">
                <div
                  class="text-[10px] uppercase tracking-widest font-label-sm-bold text-text-muted"
                >
                  {DOW[day.getDay()]}
                </div>
                <span
                  class="inline-flex items-center justify-center w-7 h-7 rounded-full text-[13px] font-label-sm-bold mt-1"
                  class:bg-accent-primary-start={isToday}
                  class:text-void={isToday}
                  class:text-text-primary={!isToday}>{day.getDate()}</span
                >
              </div>
              {#each items as item (item.id)}
                <button
                  draggable="true"
                  aria-keyshortcuts="Alt+ArrowLeft Alt+ArrowRight Alt+ArrowUp Alt+ArrowDown"
                  ondragstart={(e) => onCardDragStart(e, item)}
                  ondragend={onCardDragEnd}
                  onkeydown={(e) => onCardKeydown(e, item)}
                  onclick={() => openItem(item)}
                  class="text-left text-[12px] px-2 py-1.5 rounded bg-panel border border-border-muted hover:border-accent-primary-start/40 text-text-primary transition-all cursor-pointer {dragTaskId ===
                  item.id
                    ? 'opacity-40'
                    : ''}"
                  class:opacity-30={!itemMatchesFilter(item)}
                  title={item.clean_content}>{item.clean_content}</button
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
      {/if}
    </div>
  {/if}
</div>

<style>
  .sr-only {
    position: absolute;
    width: 1px;
    height: 1px;
    padding: 0;
    margin: -1px;
    overflow: hidden;
    clip: rect(0, 0, 0, 0);
    white-space: nowrap;
    border: 0;
  }
</style>
