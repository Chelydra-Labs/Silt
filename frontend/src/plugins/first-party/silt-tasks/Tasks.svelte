<script lang="ts">
  // Tasks view (#370) — a vault-scoped view of every active task (dated
  // and undated) grouped by time horizon. Companion surface to Calendar's
  // date-scoped agenda; surfaces undated tasks that the agenda's SQL
  // filter would otherwise drop.
  //
  // Grouping (issue AC set, plus the "No Date" group research from
  // Things 3 Anytime / Todoist no-date / Obsidian Tasks `no due date`):
  //   Overdue     — due_date < today             (error tone)
  //   Today       — due_date == today            (primary tone)
  //   Upcoming    — tomorrow <= due_date <= today+7  (muted tone)
  //   No Date     — due_date is null/empty       (muted tone, expanded by
  //                                                 default — the whole
  //                                                 point of this view is
  //                                                 to surface these)
  //   Completed   — status = DONE                (collapsed by default;
  //                                                 expanded on click;
  //                                                 ordered by file_date
  //                                                 DESC as the best-
  //                                                 available completion-
  //                                                 recency proxy until
  //                                                 a dedicated completed_at
  //                                                 column is added).
  //
  // Mark-done via ctx.updateBlockState; the resulting `block:changed`
  // event triggers a reload so the row leaves the active group on the
  // next tick (same pattern AgendaList.svelte uses).
  import { onMount, onDestroy } from 'svelte'
  import type { PluginContext, PluginManifest } from '../../sdk'
  import { plusDaysISO } from '../../sdk'

  interface Props {
    ctx: PluginContext
    manifest?: PluginManifest
    /**
     * Optional block id to scroll-into-view + transient-highlight when the
     * view mounts. Used by the standalone-task navigation router (#374)
     * so a search/tag/backlink jump that resolves to a `.silt` task
     * lands focused on the exact row rather than at the top of the list.
     */
    focusBlockId?: string
    /**
     * Monotonic counter (typically a timestamp string). Bumping this value
     * re-fires the focus $effect even when `focusBlockId` is unchanged —
     * so a second jump to the same task reflows the transient highlight.
     * Mirrors `searchTargetKey` for normal page jumps (App.svelte).
     */
    focusKey?: string
  }

  let { ctx, manifest, focusBlockId = '', focusKey = '' }: Props = $props()

  interface TaskItem {
    id: string
    notebook: string
    section: string
    page: string
    file_date: string
    line_number?: number
    clean_content: string
    status: string
    owner: string
    start_date: string
    due_date: string
    priority: number
    pinned?: boolean
  }

  let openItems = $state<TaskItem[]>([])
  let doneItems = $state<TaskItem[]>([])
  let loading = $state(true)
  let errorMsg = $state('')
  let markDoneError = $state('')
  let markDoneTimer: ReturnType<typeof setTimeout> | null = null

  // The completed group is collapsed by default (AC4). Toggled via the
  // header button; state is runtime-only (not persisted — v1; a future
  // per-plugin setting could remember the user's pref).
  let showCompleted = $state(false)

  async function reload() {
    loading = true
    errorMsg = ''
    try {
      const [openRes, doneRes] = await Promise.all([
        ctx.sqliteQuery(
          `SELECT b.id, b.notebook, b.section, b.page, b.file_date,
                  b.line_number, b.clean_content,
                  t.status, t.owner, t.start_date, t.due_date,
                  t.priority, t.pinned
           FROM blocks b JOIN tasks t ON b.id = t.block_id
           WHERE t.status != 'DONE'
           ORDER BY t.due_date IS NULL, t.due_date ASC, t.priority ASC
           LIMIT 500`
        ),
        ctx.sqliteQuery(
          `SELECT b.id, b.notebook, b.section, b.page, b.file_date,
                  b.clean_content, t.status
           FROM blocks b JOIN tasks t ON b.id = t.block_id
           WHERE t.status = 'DONE'
           ORDER BY b.file_date DESC
           LIMIT 200`
        )
      ])
      openItems = (openRes.rows as unknown as TaskItem[]) ?? []
      doneItems = (doneRes.rows as unknown as TaskItem[]) ?? []
    } catch (e) {
      errorMsg = e instanceof Error ? e.message : String(e)
    } finally {
      loading = false
    }
  }

  // Midnight rollover: re-bucket every 60s so a view left open past
  // local midnight does not sit on yesterday's date. Mirrors
  // AgendaList.svelte:71-92's nowTick pattern.
  let nowTick = $state(0)
  let nowInterval: ReturnType<typeof setInterval> | undefined

  onMount(() => {
    nowInterval = setInterval(() => {
      nowTick++
    }, 60_000)
    void reload()
  })

  let today = $derived.by(() => {
    void nowTick
    return ctx.today
  })
  let tomorrow = $derived(plusDaysISO(today, 1))
  let weekAhead = $derived(plusDaysISO(today, 7))

  // Disjoint buckets: each task lands in at most one group.
  let overdue = $derived(
    openItems.filter((i) => !!i.due_date && i.due_date < today)
  )
  let todayItems = $derived(
    openItems.filter((i) => !!i.due_date && i.due_date === today)
  )
  let upcoming = $derived(
    openItems
      .filter(
        (i) => !!i.due_date && i.due_date > tomorrow && i.due_date <= weekAhead
      )
      .sort((a, b) => a.due_date.localeCompare(b.due_date))
  )
  let undated = $derived(openItems.filter((i) => !i.due_date))

  async function markDone(item: TaskItem) {
    markDoneError = ''
    if (markDoneTimer) clearTimeout(markDoneTimer)
    try {
      await ctx.updateBlockState(item.id, 'DONE')
      openItems = openItems.filter((i) => i.id !== item.id)
    } catch (e) {
      markDoneError = e instanceof Error ? e.message : String(e)
      markDoneTimer = setTimeout(() => {
        markDoneError = ''
        markDoneTimer = null
      }, 8_000)
    }
  }

  function openItem(item: TaskItem) {
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

  // Subscribe to `block:changed` so a task marked done (or a task whose
  // due date was just set) reflows into the right group without a manual
  // reload — mirrors AgendaList.svelte:154-164.
  let offBlockChanged: (() => void) | undefined
  $effect(() => {
    offBlockChanged = ctx.on('block:changed', () => {
      void reload()
    })
  })

  // Focus the targeted row once the list is rendered (#374 AC4). The
  // scroll is a no-op if focusBlockId doesn't match any row (e.g. the
  // task was just marked done before the user could focus it). The
  // highlight auto-clears after 3s — transient, matching the spec's
  // assumption #2 in the open questions.
  let highlightTimer: ReturnType<typeof setTimeout> | null = null
  let focusedRowId = $state('')

  $effect(() => {
    // Track focusKey so a re-fire triggers the effect even when
    // focusBlockId is unchanged.
    void focusKey
    const target = focusBlockId
    if (!target) return
    if (loading) return
    // Allow the DOM to settle before scrolling.
    queueMicrotask(() => {
      const el = document.querySelector(
        `[data-block-id="${CSS.escape(target)}"]`
      ) as HTMLElement | null
      el?.scrollIntoView({ block: 'center', behavior: 'smooth' })
      focusedRowId = target
      if (highlightTimer) clearTimeout(highlightTimer)
      highlightTimer = setTimeout(() => {
        focusedRowId = ''
        highlightTimer = null
      }, 3_000)
    })
  })

  // Cleanup the highlight + interval + subscription timers on unmount.
  onDestroy(() => {
    if (nowInterval) clearInterval(nowInterval)
    if (markDoneTimer) clearTimeout(markDoneTimer)
    if (highlightTimer) clearTimeout(highlightTimer)
    offBlockChanged?.()
  })
</script>

<div class="flex-1 flex flex-col min-h-0 overflow-hidden" data-tasks-view>
  <header
    class="px-6 py-4 border-b border-border-muted flex items-center gap-3"
  >
    <span class="material-symbols-outlined text-accent-primary-start"
      >checklist</span
    >
    <h1 class="font-headline-lg text-headline-lg text-text-primary">
      {manifest?.name ?? 'Tasks'}
    </h1>
    <span
      class="text-text-muted text-[12px] font-body-md ml-auto"
      aria-live="polite"
      data-testid="tasks-open-count"
    >
      {openItems.length} active task{openItems.length === 1 ? '' : 's'}
    </span>
  </header>

  {#if markDoneError}
    <div
      class="px-6 py-2 bg-error-bg border-b border-error-border text-error text-[12px] font-body-md flex items-center gap-2"
      role="alert"
      data-testid="tasks-mark-done-error"
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
        data-testid="tasks-mark-done-error-dismiss"
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
      <div class="text-text-muted animate-pulse" data-testid="tasks-loading">
        Loading tasks…
      </div>
    {:else if errorMsg}
      <div class="text-error" data-testid="tasks-error">
        Failed to load: {errorMsg}
      </div>
    {:else if openItems.length === 0 && doneItems.length === 0}
      <div
        class="text-text-muted py-10 text-center font-body-md"
        data-testid="tasks-empty"
      >
        No tasks yet. Use <kbd>Ctrl+Shift+N</kbd> to quickly capture one.
      </div>
    {:else}
      {#each [{ key: 'overdue', label: 'Overdue', list: overdue, tone: 'error' }, { key: 'today', label: 'Today', list: todayItems, tone: 'primary' }, { key: 'upcoming', label: 'Upcoming', list: upcoming, tone: 'muted' }, { key: 'undated', label: 'No Date', list: undated, tone: 'muted' }] as group (group.key)}
        {#if group.list.length > 0}
          <section aria-label={group.label} data-group={group.key}>
            <h2
              class="font-label-sm-bold uppercase tracking-widest text-[11px] mb-2 flex items-center gap-2"
              class:text-error={group.tone === 'error'}
              class:text-accent-primary-start={group.tone === 'primary'}
              class:text-text-muted={group.tone === 'muted'}
            >
              {group.label}
              <span
                class="text-text-muted/60"
                aria-live="polite"
                data-testid="tasks-group-count"
                data-group-key={group.key}
              >
                {group.list.length}
              </span>
            </h2>
            <div class="space-y-1">
              {#each group.list as item (item.id)}
                <div
                  class="group flex items-center gap-3 px-3 py-2 rounded-lg hover:bg-hover transition-colors cursor-pointer ring-0"
                  class:tasks-focused={focusedRowId === item.id}
                  data-block-id={item.id}
                  onclick={() => openItem(item)}
                  onkeydown={(e) => {
                    if (e.key === 'Enter' || e.key === ' ') {
                      e.preventDefault()
                      openItem(item)
                    }
                  }}
                  role="button"
                  tabindex="0"
                  aria-label={`${item.clean_content}${item.due_date ? `, due ${item.due_date}` : ', no due date'}`}
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
                      data-testid="tasks-row-content"
                    >
                      {item.clean_content}
                    </div>
                    <div
                      class="text-[10px] text-text-muted uppercase tracking-widest font-label-sm"
                    >
                      {#if item.notebook === '.silt'}
                        Standalone task
                      {:else}
                        {item.notebook} › {item.section} › {item.page}
                      {/if}
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
                </div>
              {/each}
            </div>
          </section>
        {/if}
      {/each}

      {#if doneItems.length > 0}
        <section aria-label="Completed" data-group="completed">
          <h2
            class="font-label-sm-bold uppercase tracking-widest text-[11px] mb-2 flex items-center gap-2 text-text-muted"
          >
            <button
              type="button"
              onclick={() => (showCompleted = !showCompleted)}
              aria-expanded={showCompleted}
              aria-controls="tasks-completed-list"
              class="flex items-center gap-2 bg-transparent border-none p-0 cursor-pointer uppercase tracking-widest text-[11px] font-label-sm-bold text-text-muted hover:text-text-primary"
              data-testid="tasks-completed-toggle"
            >
              {#if showCompleted}
                <span class="material-symbols-outlined text-[14px]"
                  >expand_more</span
                >
              {:else}
                <span class="material-symbols-outlined text-[14px]"
                  >chevron_right</span
                >
              {/if}
              Completed
              <span class="text-text-muted/60" aria-live="polite"
                >{doneItems.length}</span
              >
            </button>
          </h2>
          {#if showCompleted}
            <div
              id="tasks-completed-list"
              class="space-y-1 opacity-60"
              data-testid="tasks-completed-list"
            >
              {#each doneItems as item (item.id)}
                <div
                  class="flex items-center gap-3 px-3 py-2 rounded-lg"
                  data-block-id={item.id}
                  role="button"
                  tabindex="0"
                  aria-label={`${item.clean_content}, completed`}
                >
                  <span
                    class="w-5 h-5 rounded todo-check-done flex-shrink-0"
                    aria-hidden="true"
                  ></span>
                  <div class="flex-1 min-w-0">
                    <div
                      class="text-text-muted text-sm font-body-md truncate line-through"
                    >
                      {item.clean_content}
                    </div>
                    <div
                      class="text-[10px] text-text-muted uppercase tracking-widest font-label-sm"
                    >
                      {#if item.notebook === '.silt'}
                        Standalone task
                      {:else}
                        {item.notebook} › {item.section} › {item.page}
                      {/if}
                    </div>
                  </div>
                  <span
                    class="text-[10px] text-text-muted font-label-sm flex-shrink-0"
                    >{item.file_date}</span
                  >
                </div>
              {/each}
            </div>
          {/if}
        </section>
      {/if}
    {/if}
  </div>
</div>

<style>
  /* Transient focus ring driven by data-focused id match — mirrors the
     "search-jump on a normal page" transient-highlight behavior the
     VirtualScrollContainer already does for non-.silt nav. */
  .tasks-focused {
    background: color-mix(
      in srgb,
      var(--color-accent-primary-glow) 100%,
      transparent
    );
    box-shadow: 0 0 0 1px var(--color-accent-primary-start) inset;
    transition:
      background 600ms ease-out,
      box-shadow 600ms ease-out;
  }
  /* The visual "done" mark — small filled square with a checkmark. */
  .todo-check-done {
    background: var(--color-accent-primary-start);
    position: relative;
  }
  .todo-check-done::after {
    content: '';
    position: absolute;
    left: 5px;
    top: 1px;
    width: 5px;
    height: 10px;
    border: solid var(--color-text-primary);
    border-width: 0 2px 2px 0;
    transform: rotate(45deg);
  }
</style>
