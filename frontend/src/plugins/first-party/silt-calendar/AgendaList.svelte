<script lang="ts">
  // AgendaList — the agenda-style grouped task view extracted from the
  // legacy Agenda.svelte plugin (#322). Renders inside Calendar.svelte
  // when the unified view mode is 'agenda'. Owns the same Overdue / Today /
  // Tomorrow / Upcoming grouping, the same markDone + openItem behavior,
  // and the same refresh-on-block:changed reactivity.
  //
  // The grouping compares against `today` (the SDK's local-day anchor,
  // #118) so the buckets match the user's local midnight, not UTC.
  import { onMount, onDestroy, tick } from 'svelte'
  import type { PluginContext, PluginManifest } from '../../sdk'
  import { plusDaysISO } from '../../sdk'
  // BlockedDoneDialog is the shared DONE-on-blocked guard surface (#302) —
  // the same glassy, focus-trapped dialog Kanban, Agenda, and the task
  // drawer all use.
  import BlockedDoneDialog from '../shared/BlockedDoneDialog.svelte'

  interface Props {
    ctx: PluginContext
    manifest?: PluginManifest
    taskCount?: number
  }

  let { ctx, manifest, taskCount = $bindable(0) }: Props = $props()

  interface AgendaItem {
    id: string
    notebook: string
    section: string
    page: string
    file_date: string
    clean_content: string
    status: string
    owner: string
    start_date: string
    due_date: string
    priority: number
    recurrence?: string
    /** 1 when the task has an open prerequisite (#301/#302). */
    is_blocked?: number
  }

  let items = $state<AgendaItem[]>([])
  let loading = $state(true)
  let errorMsg = $state('')
  let markDoneError = $state('')
  let markDoneTimer: ReturnType<typeof setTimeout> | null = null

  // DONE-on-blocked confirm (#302): when a blocked task is marked done, pause
  // and open the shared BlockedDoneDialog listing the open prerequisites.
  // Null = no prompt open.
  let pendingBlockedDone = $state<{
    item: AgendaItem
    blockers: { id: string; clean_content?: string }[]
  } | null>(null)

  async function reload() {
    loading = true
    errorMsg = ''
    try {
      const { rows } = await ctx.sqliteQuery(
        `SELECT b.id, b.notebook, b.section, b.page, b.file_date, b.line_number,
                b.clean_content, t.status, t.owner, t.start_date, t.due_date, t.priority,
                t.recur AS recurrence,
                EXISTS (
                  SELECT 1 FROM task_dependencies d
                  JOIN tasks bt ON bt.block_id = d.blocked_by_id
                  WHERE d.block_id = b.id AND bt.status != 'DONE'
                ) AS is_blocked
         FROM blocks b JOIN tasks t ON b.id = t.block_id
         WHERE t.status != 'DONE'
         ORDER BY (t.due_date IS NULL OR t.due_date = '') ASC,
                  t.due_date ASC, t.priority ASC
         LIMIT 1000`
      )
      items = (rows as unknown as AgendaItem[]) ?? []
    } catch (e) {
      errorMsg = e instanceof Error ? e.message : String(e)
    } finally {
      loading = false
    }
  }

  // The local-day anchors (`today` / `tomorrow` / `weekAhead`) drive
  // the bucket grouping below. They depend on `ctx.today` (an SDK
  // getter — see sdk.ts:82), but ctx.today is a plain getter that
  // re-evaluates only when its reactive deps change. To re-bucket
  // across midnight when the agenda view stays mounted, we maintain
  // a 60s `nowTick` and depend on it (mirrors Calendar.svelte:50-53's
  // pattern). Without this, a user with the agenda open at 23:59
  // would see today's bucket sit on yesterday's date until they
  // remounted.
  let nowTick = $state(0)
  let nowInterval: ReturnType<typeof setInterval> | undefined

  onMount(() => {
    nowInterval = setInterval(() => {
      nowTick++
    }, 60_000)
  })
  onDestroy(() => {
    if (nowInterval) clearInterval(nowInterval)
  })

  let today = $derived.by(() => {
    void nowTick
    return ctx.today
  })
  // tomorrow / weekAhead derive from the local `today` (which is
  // nowTick-aware), not from ctx.today directly — ctx.today is a plain
  // getter and a $derived that only reads it has no reactive dep, so it
  // would freeze at mount-time instead of re-bucketing after midnight.
  let tomorrow = $derived(plusDaysISO(today, 1))
  let weekAhead = $derived(plusDaysISO(today, 7))

  // Undated tasks (NULL/'' due_date) surface in their own bucket so a
  // quick-added standalone task with no due date is visible here — #368's
  // "the created task appears in the Agenda / un-dated list" AC. The date
  // comparisons below guard on truthiness so a null/empty due_date never
  // falls into Overdue (null < today would otherwise be false, but '' sorts
  // before every dated string and would mis-bucket).
  let undated = $derived(items.filter((i) => !i.due_date))
  let overdue = $derived(
    items.filter((i) => !!i.due_date && i.due_date < today)
  )
  let todayItems = $derived(
    items.filter((i) => !!i.due_date && i.due_date === today)
  )
  let tomorrowItems = $derived(
    items.filter((i) => !!i.due_date && i.due_date === tomorrow)
  )
  let upcoming = $derived(
    items
      .filter((i) => !!i.due_date && i.due_date > tomorrow)
      .sort((a, b) => a.due_date.localeCompare(b.due_date))
  )

  async function markDone(item: AgendaItem) {
    markDoneError = ''
    if (markDoneTimer) clearTimeout(markDoneTimer)
    // DONE-on-blocked guard (#302): if the task carries open prerequisites,
    // pause and open the BlockedDoneDialog (the same glassy, focus-trapped
    // surface the Kanban uses) so the DONE guard is consistent across views.
    if (item.is_blocked && !pendingBlockedDone) {
      const blockers = await ctx.getTaskBlockers(item.id)
      if (blockers.length > 0) {
        pendingBlockedDone = {
          item,
          blockers: blockers.map((b) => ({
            id: b.id,
            clean_content: b.clean_content
          }))
        }
        return
      }
    }
    await persistDone(item)
  }

  // Persist the DONE transition for a task and drop it from the list on
  // success. Shared by the unblocked path (markDone) and the confirm path
  // (confirmBlockedDone).
  async function persistDone(item: AgendaItem) {
    try {
      await ctx.updateBlockState(item.id, 'DONE')
      // Only remove from the list once the backend confirmed the change;
      // otherwise the UI would drift from the index on failure.
      items = items.filter((i) => i.id !== item.id)
    } catch (e) {
      markDoneError = e instanceof Error ? e.message : String(e)
      // Auto-clear after 8s so the banner doesn't sit forever on a list
      // that's actually working (the user dismissed it without further
      // action). The user can also dismiss manually via the banner's
      // close button.
      markDoneTimer = setTimeout(() => {
        markDoneError = ''
        markDoneTimer = null
      }, 8_000)
    }
  }

  // Confirm/cancel the DONE-on-blocked prompt (#302). The item is held in
  // pendingBlockedDone; confirm persists it, cancel just closes the dialog.
  async function confirmBlockedDone() {
    const pending = pendingBlockedDone
    if (!pending) return
    pendingBlockedDone = null
    await persistDone(pending.item)
  }

  function cancelBlockedDone() {
    pendingBlockedDone = null
  }

  function openItem(item: AgendaItem) {
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

  // Reload when the plugin's typed event bus reports a block change so a
  // task that just got marked done (or that just got a new due date)
  // shows up / disappears without a manual refresh.
  let offBlockChanged: (() => void) | undefined
  onMount(() => {
    reload()
    offBlockChanged = ctx.on('block:changed', () => {
      reload()
    })
  })
  onDestroy(() => {
    offBlockChanged?.()
    if (markDoneTimer) clearTimeout(markDoneTimer)
  })

  // Reactive scroll: when the sidebar's focusDate or activeFilter changes
  // and the agenda groups are rendered, scroll the relevant group into
  // view. Uses tick() to ensure the DOM has settled before scrolling.
  import { getFocusState, clearActiveFilter } from './focusState.svelte'

  // Expose the active filter on the script body so the in-view banner
  // (rendered below the header) can read it without a redundant
  // getFocusState() call. The value is still reactive because it reads
  // a $state field under the hood.
  let activeFilter = $derived(getFocusState().activeFilter)

  $effect(() => {
    taskCount = items.length
  })

  $effect(() => {
    const { focusDate, activeFilter: filter } = getFocusState()
    if (!focusDate && filter === 'all') return
    void items
    void tick().then(() => {
      const target = focusDate || (filter === 'today' ? today : '')
      const sel = target
        ? `[data-group-date="${target}"]`
        : `[data-group="today"]`
      const el = document.querySelector(sel) as HTMLElement | null
      el?.scrollIntoView({ block: 'start', behavior: 'smooth' })
    })
  })

  // Dim tasks that don't match the active smart-list filter.
  function matchesFilter(item: AgendaItem): boolean {
    const { activeFilter } = getFocusState()
    if (activeFilter === 'all') return true
    if (activeFilter === 'overdue') return item.due_date < today
    // "Today" smart list = exactly due today. Overdue tasks are NOT
    // matched by the Today filter — they live in the separate Overdue
    // smart list. Matches the SQL bucket in CalendarSidebar.
    if (activeFilter === 'today') return item.due_date === today
    // "Upcoming" = strictly future (today is its own smart list).
    // Matches the SQL bucket in CalendarSidebar which also excludes
    // today from the Upcoming count.
    if (activeFilter === 'upcoming')
      return item.due_date > today && item.due_date <= weekAhead
    return true
  }
</script>

<div class="flex-1 flex flex-col min-h-0 overflow-hidden" data-agenda-list>
  {#if activeFilter !== 'all'}
    <div
      class="px-6 py-1.5 border-b border-surface-panel-border bg-accent-primary-glow flex items-center gap-2 text-[12px] font-body-md"
      role="status"
      aria-live="polite"
      data-testid="agenda-filter-banner"
    >
      <span
        class="material-symbols-outlined text-[14px] text-accent-primary-start"
        >filter_alt</span
      >
      <span class="text-text-primary"
        >Focused on: <strong>{activeFilter}</strong></span
      >
      <button
        type="button"
        onclick={clearActiveFilter}
        aria-label="Clear filter"
        data-testid="agenda-clear-filter"
        class="ml-auto p-1 rounded hover:bg-hover text-text-muted hover:text-error border-none bg-transparent cursor-pointer"
      >
        <span class="material-symbols-outlined text-[14px]">close</span>
      </button>
    </div>
  {/if}

  {#if markDoneError}
    <div
      class="px-6 py-2 bg-error-bg border-b border-error-border text-error text-[12px] font-body-md flex items-center gap-2"
      role="alert"
      data-testid="mark-done-error"
    >
      <span class="flex-1">Couldn't mark task done: {markDoneError}</span>
      <button
        type="button"
        aria-label="Dismiss error"
        onclick={() => {
          markDoneError = ''
          if (markDoneTimer) {
            clearTimeout(markDoneTimer)
            markDoneTimer = null
          }
        }}
        data-testid="mark-done-error-dismiss"
        class="p-1 rounded hover:bg-hover text-text-muted hover:text-error border-none bg-transparent cursor-pointer"
      >
        <span class="material-symbols-outlined text-[14px]">close</span>
      </button>
    </div>
  {/if}

  <div
    class="flex-1 overflow-y-auto custom-scrollbar px-6 py-4 space-y-6 max-w-4xl w-full"
  >
    {#if loading}
      <div class="skeleton-container">
        {#each Array(3) as _}
          <div class="skeleton-row">
            <div class="skeleton-circle"></div>
            <div class="skeleton-text title"></div>
            <div class="skeleton-badge"></div>
          </div>
        {/each}
      </div>
    {:else if errorMsg}
      <div class="text-error">Failed to load: {errorMsg}</div>
    {:else if activeFilter === 'completed'}
      <div
        class="text-text-muted py-10 text-center font-body-md max-w-md mx-auto"
      >
        Agenda shows active tasks only. Switch to Month or Week to review
        completed tasks.
      </div>
    {:else if items.length === 0}
      <div class="text-center py-16 px-4 max-w-sm mx-auto select-none">
        <span
          class="material-symbols-outlined text-text-muted/40 text-[48px] mb-3"
          aria-hidden="true">calendar_today</span
        >
        <h3 class="font-headline-md text-text-primary mb-1">Clear agenda</h3>
        <p class="text-text-muted text-[13px] font-body-md">
          Nothing scheduled. Add a due date to any task in your notes to see it
          here, or use the Calendar to schedule items.
        </p>
      </div>
    {:else}
      {#each [{ key: 'overdue', label: 'Overdue', list: overdue, tone: 'error', date: '' }, { key: 'today', label: 'Today', list: todayItems, tone: 'primary', date: today }, { key: 'tomorrow', label: 'Tomorrow', list: tomorrowItems, tone: 'secondary', date: tomorrow }, { key: 'upcoming', label: 'Upcoming', list: upcoming, tone: 'muted', date: '' }, { key: 'undated', label: 'Undated', list: undated, tone: 'muted', date: '' }] as group (group.key)}
        {#if group.list.length > 0}
          <section
            data-group={group.key}
            data-group-date={group.date}
            aria-label={group.label}
          >
            <h2
              class="font-label-sm-bold uppercase tracking-widest text-[11px] mb-2 flex items-center gap-2"
              class:text-error={group.tone === 'error'}
              class:text-accent-primary-start={group.tone === 'primary'}
              class:text-accent-secondary-start={group.tone === 'secondary'}
              class:text-text-muted={group.tone === 'muted'}
            >
              {group.label}
              <span class="text-text-muted/60">{group.list.length}</span>
            </h2>
            <div class="space-y-1">
              {#each group.list as item (item.id)}
                <div
                  class="group flex items-center gap-3 px-3 py-2 rounded-lg hover:bg-hover transition-colors cursor-pointer"
                  class:opacity-30={!matchesFilter(item)}
                  onclick={() => openItem(item)}
                  onkeydown={(e) => {
                    if (e.key === 'Enter' || e.key === ' ') {
                      e.preventDefault()
                      openItem(item)
                    }
                  }}
                  role="button"
                  tabindex="0"
                >
                  <button
                    onclick={(e) => {
                      e.stopPropagation()
                      markDone(item)
                    }}
                    title="Mark done"
                    class="w-5 h-5 rounded todo-check flex-shrink-0 cursor-pointer hover:border-accent-primary-start"
                    aria-label="Mark done"
                  ></button>
                  <div class="flex-1 min-w-0">
                    <div
                      class="text-text-primary text-sm font-body-md truncate"
                    >
                      {item.clean_content}
                    </div>
                    <div
                      class="text-[10px] text-text-muted uppercase tracking-widest font-label-sm"
                    >
                      {item.notebook} › {item.section} › {item.page}
                    </div>
                  </div>
                  {#if item.owner}
                    <span
                      class="text-[10px] text-accent-secondary-start bg-accent-secondary-glow border border-accent-secondary-start/30 rounded px-1.5 py-0.5"
                      >[{item.owner}]</span
                    >
                  {/if}
                  {#if item.due_date}
                    <span
                      class="text-[10px] text-text-muted font-label-sm flex-shrink-0"
                      >{item.due_date}</span
                    >
                  {/if}
                  {#if item.recurrence}
                    <span
                      class="text-accent-secondary-start flex-shrink-0"
                      title="Recurring: {item.recurrence}"
                    >
                      <span
                        class="material-symbols-outlined text-[12px]"
                        aria-hidden="true">event_repeat</span
                      >
                    </span>
                  {/if}
                  {#if item.is_blocked}
                    <span
                      class="text-status-warn flex-shrink-0"
                      role="img"
                      title="Blocked by unfinished prerequisite task(s)"
                      aria-label="Blocked by unfinished prerequisite task(s)"
                    >
                      <span
                        class="material-symbols-outlined text-[12px]"
                        aria-hidden="true">lock</span
                      >
                    </span>
                  {/if}
                </div>
              {/each}
            </div>
          </section>
        {/if}
      {/each}
    {/if}
  </div>
</div>

{#if pendingBlockedDone}
  <BlockedDoneDialog
    cardText={pendingBlockedDone.item.clean_content}
    blockers={pendingBlockedDone.blockers}
    onConfirm={confirmBlockedDone}
    onCancel={cancelBlockedDone}
  />
{/if}
