<script lang="ts">
  // Jump-to-Date mini-cal section of the Tasks sidebar (#432, #763). Lifted
  // verbatim from the god-file Sidebar.svelte: per-day task dots, click-to-
  // focus, prev/next/today nav, and the aria-current/aria-selected grid.
  //
  // Reads the singleton hub state directly (state.svelte.ts) — no prop
  // drilling. reloadSignal is owned by the container; this component's
  // $effect re-runs reloadDayDots() on every signal tick AND on month
  // change (miniCursor), replacing both the external reload and the old
  // lastSeenMiniKey gating.
  import type { PluginContext } from '../../../sdk'
  import { localToday } from '../../../sdk'
  import ErrorBanner from '../components/ErrorBanner.svelte'
  import {
    getTaskHubState,
    setFocusDate,
    clearFocusDate
  } from '../state.svelte'
  import {
    ymd,
    startOfMonth,
    endOfMonth,
    addMonths,
    monthWeeks as computeMonthWeeks
  } from '../../../../lib/dateGrid'

  interface Props {
    ctx: PluginContext
    reloadSignal: number
  }

  let { ctx, reloadSignal }: Props = $props()

  let hubState = $derived(getTaskHubState())
  let activeFocusDate = $derived(hubState.focusDate)

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

  // Roving tabindex for the mini-cal-day keyboard nav.
  let miniFocusIdx = $state(0)

  // Monotonic token: the day-rollover gate can fire on the same tick as a
  // debounced block:changed flush, racing two reloads (last-resolve-wins →
  // day-dot blink). Mirrors CalendarView/ListView/BoardView.
  let loadSeq = 0

  async function reloadDayDots(): Promise<void> {
    const my = ++loadSeq
    errorMsg = ''
    try {
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
      // A newer reload superseded this one; drop the stale bucket.
      if (my !== loadSeq) return
      byDate = bucket
    } catch (e) {
      // Don't let a stale failure clobber a newer successful reload.
      if (my !== loadSeq) return
      errorMsg =
        'Mini calendar: ' + (e instanceof Error ? e.message : String(e))
    }
  }

  // Initial load (reloadSignal starts at 0) + reload on container signal
  // AND on month change. pickDay doesn't touch miniCursor so same-month
  // picks don't re-query; prevMonth/nextMonth/focus-date-effect do → reload.
  $effect(() => {
    void reloadSignal
    void miniCursor
    void reloadDayDots()
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

  // Today's ISO key for the in-month today marker. Mirrors
  // miniCursorFromToday's injected-today semantics (ctx.today || localToday(),
  // not raw new Date() — ctx.today is what the smart-list counts track, so the
  // marker must agree with them near a month boundary). Re-evaluates on
  // reloadSignal (container's day-change signal) so the marker rolls over at
  // local midnight with the counts.
  //
  // aria reconciliation: today's cell uses aria-current="date" (aligns with the
  // Date Glance popover, and aria-current means "current date"). The picked/
  // filter-focus day (activeFocusDate) previously reused aria-current="date",
  // which misuses it — picked is now exposed via aria-selected instead.
  let todayKey = $derived.by(() => {
    void reloadSignal
    return ctx.today || localToday()
  })

  // Mini-cal month grid uses the pure dateGrid helpers shared with the Tasks
  // calendar and the Date Glance popover.
  let miniWeeks = $derived(computeMonthWeeks(miniCursor))

  // Clamp the roving-tabindex cursor when the visible month shrinks (e.g.
  // paging from a 6-week to a 5-week month). Without this, miniFocusIdx can
  // exceed the grid size and no cell carries tabindex=0, locking keyboard
  // users out of the grid until they click.
  $effect(() => {
    const total = miniWeeks.flat().length
    if (total > 0 && miniFocusIdx > total - 1) miniFocusIdx = total - 1
  })

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
</script>

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
          <span
            class="material-symbols-outlined text-icon-lg"
            aria-hidden="true">chevron_left</span
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
          <span
            class="material-symbols-outlined text-icon-lg"
            aria-hidden="true">chevron_right</span
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
              {@const isToday = key === todayKey}
              {@const isPicked = key === activeFocusDate}
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
                aria-current={isToday ? 'date' : undefined}
                aria-selected={isPicked ? 'true' : undefined}
                data-testid={`mini-day-${key}`}
                class="aspect-square flex flex-col items-center justify-center gap-0.5 rounded-md text-type-sm font-label-sm cursor-pointer border-none bg-transparent focus-visible:ring-2 focus-visible:ring-accent-primary-start focus-visible:outline-none
                  {isToday
                  ? 'bg-accent-primary-glow text-accent-primary-start font-label-sm-bold'
                  : inMonth
                    ? 'text-text-primary hover:bg-hover'
                    : 'text-text-muted/50 hover:bg-hover'}
                  {isPicked ? 'ring-1 ring-accent-primary-start' : ''}"
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

{#if errorMsg}
  <ErrorBanner message={errorMsg} compact />
{/if}
