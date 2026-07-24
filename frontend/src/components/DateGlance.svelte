<script lang="ts">
  // Date Glance popover (#730). A compact, single-purpose month grid for
  // referencing dates while writing — not a second calendar (no task dots,
  // event sync, or format picker). Three openers (status-bar chip, global
  // hotkey, /calendar slash) funnel through dateGlanceState.
  //
  // The month/year label is clickable for a drill-down quick-jump selector
  // (days → months → years). Arrows remain for sequential navigation.
  import { tick } from 'svelte'
  import Popover from './Popover.svelte'
  import { dateGlance, closeDateGlance } from '../lib/dateGlanceState.svelte'
  import { monthWeeks, ymd, addMonths } from '../lib/dateGrid'
  import { formatDate, resolveDateFormat } from '../lib/dateFormat'
  import { settings } from '../settings/store.svelte'
  import { copyText } from '../lib/pageActions'
  import { pushNotification } from '../notifications/store.svelte'

  const DOW = ['S', 'M', 'T', 'W', 'T', 'F', 'S']
  const MONTHS_SHORT = [
    'Jan',
    'Feb',
    'Mar',
    'Apr',
    'May',
    'Jun',
    'Jul',
    'Aug',
    'Sep',
    'Oct',
    'Nov',
    'Dec'
  ]

  let cursor = $state(new Date())
  let todayKey = $state(ymd(new Date()))
  let focusISO = $state('')
  // Drill-down view: days (default) → months → years
  let calView = $state<'days' | 'months' | 'years'>('days')
  // First year in the 12-year picker grid
  let yearRangeStart = $state(0)

  let weeks = $derived(monthWeeks(cursor))
  let flat = $derived(weeks.flat())
  let monthLabel = $derived(
    cursor.toLocaleDateString(undefined, { month: 'long', year: 'numeric' })
  )
  let yearLabel = $derived(String(cursor.getFullYear()))
  let yearRangeLabel = $derived(`${yearRangeStart}–${yearRangeStart + 11}`)
  let yearList = $derived(
    Array.from({ length: 12 }, (_, i) => yearRangeStart + i)
  )
  let insertMode = $derived(
    !!(dateGlance.insertEditor && !dateGlance.insertEditor.isDestroyed)
  )
  let popoverAnchor = $derived(dateGlance.anchor ?? document.body)
  let configuredFormat = $derived(
    resolveDateFormat(settings.config?.editor?.date_format)
  )
  let prevAria = $derived(
    calView === 'days'
      ? 'Previous month'
      : calView === 'months'
        ? 'Previous year'
        : 'Previous year range'
  )
  let nextAria = $derived(
    calView === 'days'
      ? 'Next month'
      : calView === 'months'
        ? 'Next year'
        : 'Next year range'
  )

  // Open/close transition via openGen + focus save/restore.
  let prevOpen = false
  let prevGen = -1
  let previousFocus: HTMLElement | null = null

  $effect(() => {
    const isOpen = dateGlance.open
    const gen = dateGlance.openGen
    if (isOpen && (!prevOpen || gen !== prevGen)) {
      if (!prevOpen) {
        previousFocus = document.activeElement as HTMLElement | null
      }
      const now = new Date()
      cursor = now
      todayKey = ymd(now)
      focusISO = todayKey
      calView = 'days'
      void tick().then(() => {
        document
          .querySelector<HTMLElement>(`[data-glance-date="${todayKey}"]`)
          ?.focus()
      })
    } else if (!isOpen && prevOpen) {
      previousFocus?.focus()
      previousFocus = null
    }
    prevOpen = isOpen
    prevGen = gen
  })

  // --- Navigation (arrows adapt to the active calView) -------------------

  function prevNav(): void {
    if (calView === 'days') {
      cursor = addMonths(cursor, -1)
      clampFocusISO()
    } else if (calView === 'months') {
      cursor = new Date(cursor.getFullYear() - 1, cursor.getMonth(), 1)
    } else {
      yearRangeStart -= 12
    }
  }
  function nextNav(): void {
    if (calView === 'days') {
      cursor = addMonths(cursor, 1)
      clampFocusISO()
    } else if (calView === 'months') {
      cursor = new Date(cursor.getFullYear() + 1, cursor.getMonth(), 1)
    } else {
      yearRangeStart += 12
    }
  }

  function goToday(): void {
    const now = new Date()
    cursor = now
    focusISO = todayKey
    calView = 'days'
  }

  function clampFocusISO(): void {
    const cells = flat
    if (cells.some((d) => ymd(d) === focusISO)) return
    const inMonth = cells.find((d) => d.getMonth() === cursor.getMonth())
    focusISO = inMonth ? ymd(inMonth) : ymd(cells[0])
  }

  // --- Drill-down -------------------------------------------------------

  function enterMonths(): void {
    calView = 'months'
  }
  function enterYears(): void {
    yearRangeStart = cursor.getFullYear() - (cursor.getFullYear() % 12)
    calView = 'years'
  }
  function pickMonth(m: number): void {
    cursor = new Date(cursor.getFullYear(), m, 1)
    calView = 'days'
    clampFocusISO()
  }
  function pickYear(y: number): void {
    cursor = new Date(y, cursor.getMonth(), 1)
    calView = 'months'
  }

  // --- Day-pick (insert or copy) ----------------------------------------

  async function copyWithToast(dateStr: string): Promise<void> {
    const ok = await copyText(dateStr)
    if (ok) {
      pushNotification({
        kind: 'success',
        message: `Copied ${dateStr}`,
        autoDismissMs: 2500
      })
    }
  }

  async function pickDay(day: Date): Promise<void> {
    const dateStr = formatDate(day, configuredFormat)
    const editor = dateGlance.insertEditor
    if (editor && !editor.isDestroyed) {
      try {
        const ok = editor.chain().focus().insertContent(dateStr).run()
        if (ok) {
          previousFocus = null
          closeDateGlance()
          return
        }
      } catch (e) {
        console.error('[silt] date-glance insert failed:', e)
      }
    }
    await copyWithToast(dateStr)
    closeDateGlance()
  }

  // --- Keyboard nav (day grid only) -------------------------------------

  function moveFocus(nextIdx: number): void {
    focusISO = ymd(flat[nextIdx])
    document
      .querySelector<HTMLElement>(`[data-glance-date="${focusISO}"]`)
      ?.focus()
  }

  function onGridKeydown(e: KeyboardEvent): void {
    const cells = flat
    const len = cells.length
    if (!len) return
    let idx = cells.findIndex((d) => ymd(d) === focusISO)
    if (idx < 0) idx = 0
    switch (e.key) {
      case 'ArrowRight':
        e.preventDefault()
        moveFocus((idx + 1) % len)
        break
      case 'ArrowLeft':
        e.preventDefault()
        moveFocus((idx - 1 + len) % len)
        break
      case 'ArrowDown':
        e.preventDefault()
        moveFocus((idx + 7) % len)
        break
      case 'ArrowUp':
        e.preventDefault()
        moveFocus((idx - 7 + len) % len)
        break
      case 'Home':
        e.preventDefault()
        moveFocus(Math.floor(idx / 7) * 7)
        break
      case 'End':
        e.preventDefault()
        moveFocus(Math.floor(idx / 7) * 7 + 6)
        break
      case 'PageUp':
        e.preventDefault()
        prevNav()
        break
      case 'PageDown':
        e.preventDefault()
        nextNav()
        break
      case 'Enter':
      case ' ':
        e.preventDefault()
        void pickDay(cells[idx])
        break
    }
  }
</script>

<Popover
  open={dateGlance.open}
  onClose={closeDateGlance}
  anchor={popoverAnchor}
  class="w-72 rounded-xl border border-surface-popover-border bg-surface-popover shadow-2xl"
>
  {#snippet content()}
    <div
      role="dialog"
      aria-modal="false"
      aria-label="Pick a date to insert or copy"
      class="p-3"
    >
      <!-- Header: arrows + clickable label (drill-down entry) -->
      <div class="mb-2 flex items-center justify-between gap-1">
        <button
          type="button"
          aria-label={prevAria}
          class="flex h-7 w-7 items-center justify-center rounded-md border border-transparent text-text-muted hover:bg-hover hover:text-text-primary focus-visible:ring-2 focus-visible:ring-accent-primary-start cursor-pointer"
          onclick={prevNav}
        >
          <span
            class="material-symbols-outlined text-icon-lg"
            aria-hidden="true">chevron_left</span
          >
        </button>

        {#if calView === 'days'}
          <button
            type="button"
            class="min-w-0 flex-1 rounded-md text-center text-type-sm font-label-sm-bold text-text-primary hover:bg-hover py-1 truncate cursor-pointer focus-visible:ring-2 focus-visible:ring-accent-primary-start"
            title="Jump to month"
            onclick={enterMonths}
          >
            {monthLabel}
          </button>
        {:else if calView === 'months'}
          <button
            type="button"
            class="min-w-0 flex-1 rounded-md text-center text-type-sm font-label-sm-bold text-text-primary hover:bg-hover py-1 truncate cursor-pointer focus-visible:ring-2 focus-visible:ring-accent-primary-start"
            title="Jump to year"
            onclick={enterYears}
          >
            {yearLabel}
          </button>
        {:else}
          <span
            class="min-w-0 flex-1 text-center text-type-sm font-label-sm-bold text-text-primary truncate py-1"
          >
            {yearRangeLabel}
          </span>
        {/if}

        <button
          type="button"
          aria-label={nextAria}
          class="flex h-7 w-7 items-center justify-center rounded-md border border-transparent text-text-muted hover:bg-hover hover:text-text-primary focus-visible:ring-2 focus-visible:ring-accent-primary-start cursor-pointer"
          onclick={nextNav}
        >
          <span
            class="material-symbols-outlined text-icon-lg"
            aria-hidden="true">chevron_right</span
          >
        </button>
      </div>

      <!-- Day grid -->
      {#if calView === 'days'}
        <div
          class="mb-1 grid grid-cols-7 gap-0.5 text-center text-type-3xs uppercase tracking-[0.12em] text-text-muted"
          aria-hidden="true"
        >
          {#each DOW as d, i (i)}
            <span class="py-0.5">{d}</span>
          {/each}
        </div>

        <div
          role="grid"
          tabindex="-1"
          aria-label={monthLabel}
          class="grid grid-cols-7 gap-0.5"
          onkeydown={onGridKeydown}
        >
          {#each weeks as week, wi (wi)}
            <div role="row" class="contents">
              {#each week as day (ymd(day))}
                {@const iso = ymd(day)}
                {@const inMonth = day.getMonth() === cursor.getMonth()}
                {@const isToday = iso === todayKey}
                <button
                  type="button"
                  role="gridcell"
                  data-glance-date={iso}
                  tabindex={iso === focusISO ? 0 : -1}
                  aria-label={day.toLocaleDateString(undefined, {
                    weekday: 'long',
                    year: 'numeric',
                    month: 'long',
                    day: 'numeric'
                  })}
                  aria-current={isToday ? 'date' : undefined}
                  class="flex aspect-square items-center justify-center rounded-md text-type-sm transition-colors focus-visible:ring-2 focus-visible:ring-accent-primary-start focus-visible:outline-none cursor-pointer {isToday
                    ? 'bg-accent-primary-glow text-accent-primary-start font-label-sm-bold'
                    : inMonth
                      ? 'text-text-primary hover:bg-hover'
                      : 'text-text-disabled hover:bg-hover'}"
                  onclick={() => pickDay(day)}
                >
                  {day.getDate()}
                </button>
              {/each}
            </div>
          {/each}
        </div>

        <!-- Month picker -->
      {:else if calView === 'months'}
        <div class="grid grid-cols-3 gap-1">
          {#each MONTHS_SHORT as m, i (i)}
            <button
              type="button"
              class="flex items-center justify-center rounded-lg py-2.5 text-type-sm transition-colors hover:bg-hover cursor-pointer focus-visible:ring-2 focus-visible:ring-accent-primary-start focus-visible:outline-none {i ===
              cursor.getMonth()
                ? 'bg-accent-primary-glow text-accent-primary-start font-label-sm-bold'
                : 'text-text-primary'}"
              aria-current={i === cursor.getMonth() ? 'true' : undefined}
              onclick={() => pickMonth(i)}
            >
              {m}
            </button>
          {/each}
        </div>

        <!-- Year picker -->
      {:else}
        <div class="grid grid-cols-3 gap-1">
          {#each yearList as y (y)}
            <button
              type="button"
              class="flex items-center justify-center rounded-lg py-2.5 text-type-sm transition-colors hover:bg-hover cursor-pointer focus-visible:ring-2 focus-visible:ring-accent-primary-start focus-visible:outline-none {y ===
              cursor.getFullYear()
                ? 'bg-accent-primary-glow text-accent-primary-start font-label-sm-bold'
                : 'text-text-primary'}"
              aria-current={y === cursor.getFullYear() ? 'true' : undefined}
              onclick={() => pickYear(y)}
            >
              {y}
            </button>
          {/each}
        </div>
      {/if}

      <div class="mt-2 border-t border-surface-popover-border pt-2">
        <p
          class="mb-1.5 text-center text-type-3xs text-text-muted"
          aria-live="polite"
        >
          {insertMode ? 'Inserts at cursor' : 'Copies to clipboard'}
        </p>
        <div class="flex justify-center">
          <button
            type="button"
            class="rounded-md px-3 py-1 text-type-xs text-text-muted hover:bg-hover hover:text-text-primary focus-visible:ring-2 focus-visible:ring-accent-primary-start cursor-pointer"
            onclick={goToday}
          >
            Today
          </button>
        </div>
      </div>
    </div>
  {/snippet}
</Popover>
