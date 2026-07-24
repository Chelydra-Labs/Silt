<script lang="ts">
  // Date Glance popover (#730). A compact, single-purpose month grid for
  // referencing dates while writing — not a second calendar (no task dots,
  // event sync, or format picker). One shared instance driven by
  // dateGlanceState; three openers (status-bar chip, global hotkey,
  // /calendar slash command) all funnel through it.
  //
  // Picking a day inserts YYYY-MM-DD at the cursor when an editor target was
  // captured at open time, otherwise copies it to the clipboard with a toast.
  import { tick } from 'svelte'
  import Popover from './Popover.svelte'
  import { dateGlance, closeDateGlance } from '../lib/dateGlanceState.svelte'
  import { monthWeeks, ymd, addMonths } from '../lib/dateGrid'
  import { copyText } from '../lib/pageActions'
  import { pushNotification } from '../notifications/store.svelte'

  const DOW = ['S', 'M', 'T', 'W', 'T', 'F', 'S']

  let cursor = $state(new Date())
  let todayKey = $state(ymd(new Date()))
  // ISO date of the grid cell that holds the roving tabindex (0); every other
  // cell is -1 so Tab/Shift-Tab exits the grid while arrows move within it.
  let focusISO = $state('')

  let weeks = $derived(monthWeeks(cursor))
  let flat = $derived(weeks.flat())
  let monthLabel = $derived(
    cursor.toLocaleDateString(undefined, { month: 'long', year: 'numeric' })
  )

  // Reset to today each time the popover opens, and focus today's cell.
  let wasOpen = false
  $effect(() => {
    const isOpen = dateGlance.open
    if (isOpen && !wasOpen) {
      const now = new Date()
      cursor = now
      todayKey = ymd(now)
      focusISO = todayKey
      void tick().then(() => {
        document
          .querySelector<HTMLElement>(`[data-glance-date="${todayKey}"]`)
          ?.focus()
      })
    }
    wasOpen = isOpen
  })

  function prevMonth(): void {
    cursor = addMonths(cursor, -1)
  }
  function nextMonth(): void {
    cursor = addMonths(cursor, 1)
  }
  function goToday(): void {
    const now = new Date()
    cursor = now
    focusISO = todayKey
  }

  async function pickDay(day: Date): Promise<void> {
    const iso = ymd(day)
    const editor = dateGlance.insertEditor
    if (editor && !editor.isDestroyed) {
      // Re-focus restores the cursor to its last position (ProseMirror keeps
      // the selection across blur), so the date lands where the user was
      // typing even though focus moved into the grid.
      editor.chain().focus().insertContent(iso).run()
    } else {
      await copyText(iso)
      pushNotification({
        kind: 'success',
        message: `Copied ${iso}`,
        autoDismissMs: 2500
      })
    }
    closeDateGlance()
  }

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
        moveFocus(0)
        break
      case 'End':
        e.preventDefault()
        moveFocus(len - 1)
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
  anchor={dateGlance.anchor}
  class="w-[268px] rounded-xl border border-surface-popover-border bg-surface-popover shadow-2xl"
>
  {#snippet content()}
    <div
      role="dialog"
      aria-modal="false"
      aria-label="Date glance — pick a date to insert or copy"
      class="p-3"
    >
      <div class="mb-2 flex items-center justify-between gap-1">
        <button
          type="button"
          aria-label="Previous month"
          class="flex h-7 w-7 items-center justify-center rounded-md border border-transparent text-text-muted hover:bg-hover hover:text-text-primary focus-visible:ring-2 focus-visible:ring-accent-primary-start cursor-pointer"
          onclick={prevMonth}
        >
          <span class="material-symbols-outlined text-[18px]" aria-hidden="true"
            >chevron_left</span
          >
        </button>
        <span
          class="min-w-0 flex-1 text-center text-type-sm font-label-sm-bold text-text-primary truncate"
          >{monthLabel}</span
        >
        <button
          type="button"
          aria-label="Next month"
          class="flex h-7 w-7 items-center justify-center rounded-md border border-transparent text-text-muted hover:bg-hover hover:text-text-primary focus-visible:ring-2 focus-visible:ring-accent-primary-start cursor-pointer"
          onclick={nextMonth}
        >
          <span class="material-symbols-outlined text-[18px]" aria-hidden="true"
            >chevron_right</span
          >
        </button>
      </div>

      <div
        class="mb-1 grid grid-cols-7 gap-0.5 text-center text-type-3xs text-text-muted"
        aria-hidden="true"
      >
        {#each DOW as d (d)}
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
                  ? 'bg-accent-primary-start/15 text-accent-primary-start font-label-sm-bold ring-1 ring-accent-primary-start/40'
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

      <div
        class="mt-2 flex justify-center border-t border-surface-popover-border pt-2"
      >
        <button
          type="button"
          class="rounded-md px-3 py-1 text-type-xs text-text-muted hover:bg-hover hover:text-text-primary focus-visible:ring-2 focus-visible:ring-accent-primary-start cursor-pointer"
          onclick={goToday}
        >
          Today
        </button>
      </div>
    </div>
  {/snippet}
</Popover>
