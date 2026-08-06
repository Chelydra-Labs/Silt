<script lang="ts">
  // Smart Lists section of the Tasks sidebar (#432, #763). Lifted verbatim
  // from the god-file Sidebar.svelte: Today/Upcom­/Overdue/Completed/All
  // rows with live vault-wide counts, plus the aria-live count announcer.
  //
  // Reads the singleton hub state directly (state.svelte.ts) — no prop
  // drilling. reloadSignal is owned by the container; this component's
  // $effect re-runs reloadCounts() on every tick (initial mount = 0).
  import type { PluginContext } from '../../../sdk'
  import { localToday } from '../../../sdk'
  import { endOfWeekISO } from '../../../../lib/dateGrid'
  import { getTaskWeekStart } from '../../../../lib/taskWeekStart.svelte'
  import ErrorBanner from '../components/ErrorBanner.svelte'
  import {
    getTaskHubState,
    setActiveFilter,
    clearActiveFilter,
    type CalendarFilter
  } from '../state.svelte'

  interface Props {
    ctx: PluginContext
    reloadSignal: number
  }

  let { ctx, reloadSignal }: Props = $props()

  let hubState = $derived(getTaskHubState())
  let activeFilter = $derived(hubState.activeFilter)

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

  let errorMsg = $state('')
  // Suppresses the empty-state hint during the initial IPC gap — counts
  // init to all-zeros, so without this the "set a due date" hint flashes
  // even in a vault with hundreds of tasks. Flipped false on first success.
  let loading = $state(true)

  // Roving tabindex for the smart-list keyboard nav.
  let listFocusIdx = $state(0)

  // Monotonic token: a refresh-navigation during a block:changed debounce
  // window can race two reloads (last-resolve-wins → count blink). The
  // guard drops stale results. Mirrors CalendarView/ListView/BoardView.
  let loadSeq = 0

  async function reloadCounts(): Promise<void> {
    const my = ++loadSeq
    errorMsg = ''
    try {
      const today = ctx.today || localToday()
      const weekEnd = endOfWeekISO(today, getTaskWeekStart())
      // `AS "all"` is double-quoted because ALL is a SQLite keyword
      // (UNION ALL / SELECT ALL); a bare `AS all` is a syntax error.
      const res = await ctx.sqliteQuery(
        `SELECT
           SUM(CASE WHEN t.status != 'DONE' AND t.due_date = ? THEN 1 ELSE 0 END) AS today,
           SUM(CASE WHEN t.status != 'DONE' AND t.due_date > ? AND t.due_date <= ? THEN 1 ELSE 0 END) AS upcoming,
           SUM(CASE WHEN t.status != 'DONE' AND t.due_date < ? THEN 1 ELSE 0 END) AS overdue,
           SUM(CASE WHEN t.status = 'DONE' THEN 1 ELSE 0 END) AS completed,
           SUM(CASE WHEN t.status != 'DONE' THEN 1 ELSE 0 END) AS "all"
        FROM blocks b JOIN tasks t ON b.id = t.block_id`,
        [today, today, weekEnd, today]
      )
      const row = res.rows?.[0] ?? {}
      // A newer reload superseded this one; drop the stale result.
      if (my !== loadSeq) return
      counts = {
        today: Number(row.today ?? 0),
        upcoming: Number(row.upcoming ?? 0),
        overdue: Number(row.overdue ?? 0),
        completed: Number(row.completed ?? 0),
        all: Number(row.all ?? 0)
      }
      loading = false
    } catch (e) {
      // Don't let a stale failure clobber a newer successful reload.
      if (my !== loadSeq) return
      errorMsg = 'Smart Lists: ' + (e instanceof Error ? e.message : String(e))
    }
  }

  // Initial load (reloadSignal starts at 0) + reload on every container
  // signal. Reading reloadSignal inside the effect tracks it.
  $effect(() => {
    void reloadSignal
    void reloadCounts()
  })

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

  // aria-live region announces count changes only. Filter state belongs to
  // the header FilterBar and saved-views count to a sibling section, so
  // announcing them here would be misleading context now that this region
  // lives in SmartLists.
  let liveMessage = $state('')
  let lastMsgJson = ''
  $effect(() => {
    const j = JSON.stringify({ c: counts })
    if (j !== lastMsgJson) {
      lastMsgJson = j
      liveMessage = `Counts: ${counts.today} today, ${counts.upcoming} upcoming, ${counts.overdue} overdue, ${counts.completed} completed, ${counts.all} total.`
    }
  })
</script>

<section
  aria-labelledby="tasks-smart-lists-heading"
  class="rounded-xl border border-surface-sidebar-border bg-surface-panel/35 p-2 shadow-sm"
>
  <h3
    id="tasks-smart-lists-heading"
    class="px-1.5 pt-0.5 font-label-sm-bold uppercase tracking-widest text-type-2xs text-text-muted"
  >
    Smart Lists
    <span class="font-label-sm normal-case tracking-normal text-text-muted/60">
      (vault-wide)
    </span>
  </h3>
  {#if counts.all > 0 || counts.completed > 0}
    <ul role="listbox" aria-label="Smart lists" class="mt-1.5 space-y-0.5">
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
            class="group relative w-full flex min-h-9 items-center gap-2 overflow-hidden px-2.5 py-1.5 rounded-lg text-left text-type-sm font-body-md cursor-pointer border transition-all focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-border-focus
            {selected
              ? 'border-accent-primary-start/25 bg-accent-primary-glow text-accent-primary-start shadow-sm'
              : 'border-transparent bg-transparent text-text-primary hover:border-surface-sidebar-border hover:bg-hover'}"
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
              class="min-w-6 text-center text-type-2xs text-text-muted bg-surface-popover border border-surface-popover-border px-1.5 py-0.5 rounded-full font-label-sm"
              aria-label="{counts[item.id]} tasks"
              data-testid={`count-${item.id}`}
            >
              {counts[item.id]}
            </span>
          </button>
        </li>
      {/each}
    </ul>
  {:else if !loading}
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
      class="mt-1.5 w-full flex items-center justify-center gap-1 px-2 py-1 rounded-lg text-type-xs font-label-sm text-text-muted hover:bg-error/10 hover:text-error cursor-pointer border border-dashed border-surface-popover-border bg-transparent transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-border-focus"
    >
      <span class="material-symbols-outlined text-icon-xs">close</span>
      Clear filter
    </button>
  {/if}
</section>

<!-- aria-live region announces count changes (filter changes are announced by the header FilterBar) -->
<div class="sr-only" aria-live="polite">{liveMessage}</div>

{#if errorMsg}
  <ErrorBanner message={errorMsg} compact />
{/if}
