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
  // Whether a day-pick will insert (editor captured) or copy (no editor).
  let insertMode = $derived(
    !!(dateGlance.insertEditor && !dateGlance.insertEditor.isDestroyed)
  )
  // H1: fall back to document.body so the popover still renders + closes even
  // if the chip isn't mounted (defensive — normally the chip is always present).
  let popoverAnchor = $derived(dateGlance.anchor ?? document.body)

  // Open/close transition detection via openGen (robust to re-opens while
  // already open) + focus save/restore for keyboard users.
  let prevOpen = false
  let prevGen = -1
  let previousFocus: HTMLElement | null = null

  $effect(() => {
    const isOpen = dateGlance.open
    const gen = dateGlance.openGen
    if (isOpen && (!prevOpen || gen !== prevGen)) {
      // Fresh open or re-open — reset view to today + focus today's cell.
      if (!prevOpen) {
        previousFocus = document.activeElement as HTMLElement | null
      }
      const now = new Date()
      cursor = now
      todayKey = ymd(now)
      focusISO = todayKey
      void tick().then(() => {
        document
          .querySelector<HTMLElement>(`[data-glance-date="${todayKey}"]`)
          ?.focus()
      })
    } else if (!isOpen && prevOpen) {
      // Just closed — restore focus to the opener element.
      previousFocus?.focus()
      previousFocus = null
    }
    prevOpen = isOpen
    prevGen = gen
  })

  function prevMonth(): void {
    cursor = addMonths(cursor, -1)
    clampFocusISO()
  }
  function nextMonth(): void {
    cursor = addMonths(cursor, 1)
    clampFocusISO()
  }
  function goToday(): void {
    const now = new Date()
    cursor = now
    focusISO = todayKey
  }

  // After cursor changes, ensure focusISO points at a cell in the current
  // view so the next arrow-key press starts from a sensible position.
  function clampFocusISO(): void {
    const cells = flat
    if (cells.some((d) => ymd(d) === focusISO)) return
    const inMonth = cells.find((d) => d.getMonth() === cursor.getMonth())
    focusISO = inMonth ? ymd(inMonth) : ymd(cells[0])
  }

  async function copyWithToast(iso: string): Promise<void> {
    const ok = await copyText(iso)
    if (ok) {
      pushNotification({
        kind: 'success',
        message: `Copied ${iso}`,
        autoDismissMs: 2500
      })
    }
    // copyText already emits its own error toast on failure — don't double up.
  }

  async function pickDay(day: Date): Promise<void> {
    const iso = ymd(day)
    const editor = dateGlance.insertEditor
    if (editor && !editor.isDestroyed) {
      try {
        const ok = editor.chain().focus().insertContent(iso).run()
        if (ok) {
          // Don't restore focus to the opener (chip) — the insert just moved
          // focus into the editor where the user should keep typing.
          previousFocus = null
          closeDateGlance()
          return
        }
        // Transaction rejected (schema/dispatcher) — fall through to clipboard.
      } catch (e) {
        console.error('[silt] date-glance insert failed:', e)
        // Fall through to clipboard.
      }
    }
    await copyWithToast(iso)
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
        moveFocus(Math.floor(idx / 7) * 7)
        break
      case 'End':
        e.preventDefault()
        moveFocus(Math.floor(idx / 7) * 7 + 6)
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
      <div class="mb-2 flex items-center justify-between gap-1">
        <button
          type="button"
          aria-label="Previous month"
          class="flex h-7 w-7 items-center justify-center rounded-md border border-transparent text-text-muted hover:bg-hover hover:text-text-primary focus-visible:ring-2 focus-visible:ring-accent-primary-start cursor-pointer"
          onclick={prevMonth}
        >
          <span
            class="material-symbols-outlined text-icon-lg"
            aria-hidden="true">chevron_left</span
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
          <span
            class="material-symbols-outlined text-icon-lg"
            aria-hidden="true">chevron_right</span
          >
        </button>
      </div>

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
