<script lang="ts">
  // List display mode of the Tasks hub (#424). The time-horizon grouping
  // (Overdue/Today/Upcoming/Later/No Date/Completed) is the legacy Tasks
  // behavior; the grouping-engine issue (#423) generalizes the section
  // pattern into arbitrary dimensions. This phase keeps the proven queries
  // + binning and adds a client-side filter bridge against the unified hub
  // state so the hub's shared FilterBar + scope breadcrumb affect the list
  // immediately. Phase 2 moves filtering server-side via the unified
  // buildQuery and adds the group-by selector.
  //
  // The header (title + count) lives in TasksHub.svelte; this component
  // reports its open/done counts upward via onCountChange.
  import { onMount, onDestroy } from 'svelte'
  import type { PluginContext, PluginManifest } from '../../../sdk'
  import { plusDaysISO } from '../../../sdk'
  import { STANDALONE_TASKS_NOTEBOOK } from '../../../../lib/standaloneTasksNav'
  import QuickAddTask from '../../shared/QuickAddTask.svelte'
  import TaskEditDrawer from '../../shared/TaskEditDrawer.svelte'
  import TaskSubEditorModal from '../../shared/TaskSubEditorModal.svelte'
  import BlockedDoneDialog from '../../shared/BlockedDoneDialog.svelte'
  import type { TaskDetail } from '../../shared/types'
  import { getTaskHubState } from '../state.svelte'

  interface Props {
    ctx: PluginContext
    /** Unused now (title lives in the hub); kept for direct-render compat. */
    manifest?: PluginManifest
    focusBlockId?: string
    focusKey?: string
    /** Hub subscribes to keep its header count in sync. */
    onCountChange?: (open: number, done: number) => void
  }

  let { ctx, focusBlockId = '', focusKey = '', onCountChange }: Props = $props()

  // Completed rows are display-only; a narrower shape than the open-task
  // TaskDetail (which the edit drawer requires).
  interface CompletedTaskItem {
    id: string
    notebook: string
    section: string
    page: string
    file_date: string
    clean_content: string
    status: string
  }

  let openItems = $state<TaskDetail[]>([])
  let doneItems = $state<CompletedTaskItem[]>([])
  let loading = $state(true)
  let errorMsg = $state('')
  let markDownError = $state('')
  let markDownTimer: ReturnType<typeof setTimeout> | null = null
  let openTruncated = $state(false)
  let doneTruncated = $state(false)
  let showCompleted = $state(false)

  let selectedTask = $state<TaskDetail | null>(null)
  let subEditorTask = $state<TaskDetail | null>(null)
  let pendingBlockedDone = $state<{
    item: TaskDetail
    blockers: { id: string; clean_content?: string }[]
  } | null>(null)

  async function reload() {
    loading = true
    errorMsg = ''
    try {
      const [openRes, doneRes] = await Promise.all([
        ctx.sqliteQuery(
          `SELECT b.id, b.notebook, b.section, b.page, b.file_date,
                  b.clean_content,
                  t.status, t.owner, t.start_date, t.due_date,
                  t.priority, t.pinned, t.progress,
                  t.recur AS recurrence, t.comments_count, t.links_count,
                  t.created_at, t.completed_at, t.manual_order,
                  (SELECT GROUP_CONCAT(raw_path, '|') FROM tags WHERE block_id = b.id) AS tags,
                  (SELECT GROUP_CONCAT(blocked_by_id, '|') FROM task_dependencies WHERE block_id = b.id) AS blocked_by,
                  EXISTS (
                    SELECT 1 FROM task_dependencies d
                    JOIN tasks bt ON bt.block_id = d.blocked_by_id
                    WHERE d.block_id = b.id AND bt.status != 'DONE'
                  ) AS is_blocked
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
      openItems = ((openRes.rows as unknown as TaskDetail[]) ?? []).map(
        (r) => ({
          ...r,
          pinned: !!r.pinned,
          created_at: r.created_at ?? '',
          completed_at: r.completed_at ?? '',
          manual_order: r.manual_order ?? 0
        })
      )
      doneItems = (doneRes.rows as unknown as CompletedTaskItem[]) ?? []
      if (selectedTask) {
        const fresh = openItems.find((i) => i.id === selectedTask!.id)
        if (fresh) selectedTask = fresh
      }
      openTruncated = openRes.truncated
      doneTruncated = doneRes.truncated
    } catch (e) {
      errorMsg = e instanceof Error ? e.message : String(e)
    } finally {
      loading = false
    }
  }

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

  // Client-side filter bridge (#424): apply the hub's scope + filter
  // selection to the fetched open set. At default state (scope=vault,
  // empty filters) this is a no-op so legacy behavior is unchanged. The
  // grouping-engine issue (#423) moves this server-side via buildQuery.
  function passesHubFilters(item: TaskDetail): boolean {
    const s = getTaskHubState()
    if (s.scope === 'notebook' && item.notebook !== ctx.activeNotebook)
      return false
    if (
      s.scope === 'section' &&
      (item.notebook !== ctx.activeNotebook ||
        item.section !== ctx.activeSection)
    )
      return false
    if (
      s.scope === 'page' &&
      (item.notebook !== ctx.activeNotebook ||
        item.section !== ctx.activeSection ||
        item.page !== ctx.activePage)
    )
      return false
    if (s.filters.owners.length && !s.filters.owners.includes(item.owner))
      return false
    if (
      s.filters.priorities.length &&
      !s.filters.priorities.includes(item.priority)
    )
      return false
    if (s.filters.dueDate) {
      const d = s.filters.dueDate
      if (d === 'none') {
        if (item.due_date) return false
      } else if (d === 'overdue') {
        if (!item.due_date || item.due_date >= today) return false
      } else if (d === 'today') {
        if (item.due_date !== today) return false
      } else if (d === 'week') {
        if (
          !item.due_date ||
          item.due_date < today ||
          item.due_date > weekAhead
        )
          return false
      }
    }
    if (s.filters.tags.length) {
      const itemTags = (item.tags ?? '').split('|').filter(Boolean)
      if (!s.filters.tags.some((t) => itemTags.includes(t))) return false
    }
    return true
  }

  let filteredOpen = $derived(openItems.filter(passesHubFilters))

  // Report counts upward so the hub header stays in sync. Runs after every
  // reload / filter / nav change.
  $effect(() => {
    const open = filteredOpen.length
    const done = doneItems.length
    onCountChange?.(open, done)
  })

  let overdue = $derived(
    filteredOpen.filter((i) => !!i.due_date && i.due_date < today)
  )
  let todayItems = $derived(
    filteredOpen.filter((i) => !!i.due_date && i.due_date === today)
  )
  let upcoming = $derived(
    filteredOpen
      .filter(
        (i) => !!i.due_date && i.due_date >= tomorrow && i.due_date <= weekAhead
      )
      .sort((a, b) => a.due_date.localeCompare(b.due_date))
  )
  let later = $derived(
    filteredOpen
      .filter((i) => !!i.due_date && i.due_date > weekAhead)
      .sort((a, b) => a.due_date.localeCompare(b.due_date))
  )
  let undated = $derived(filteredOpen.filter((i) => !i.due_date))

  async function commitMarkDown(item: TaskDetail) {
    markDownError = ''
    if (markDownTimer) clearTimeout(markDownTimer)
    try {
      await ctx.updateBlockState(item.id, 'DONE')
      openItems = openItems.filter((i) => i.id !== item.id)
    } catch (e) {
      markDownError = e instanceof Error ? e.message : String(e)
      markDownTimer = setTimeout(() => {
        markDownError = ''
        markDownTimer = null
      }, 8_000)
    }
  }

  async function markDone(item: TaskDetail) {
    if (pendingBlockedDone) return
    if (item.is_blocked && !pendingBlockedDone) {
      try {
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
      } catch (e) {
        markDownError = e instanceof Error ? e.message : String(e)
        markDownTimer = setTimeout(() => {
          markDownError = ''
          markDownTimer = null
        }, 8_000)
        return
      }
    }
    await commitMarkDown(item)
  }

  function confirmBlockedDone() {
    const pending = pendingBlockedDone
    pendingBlockedDone = null
    if (pending) void commitMarkDown(pending.item)
  }

  function cancelBlockedDone() {
    pendingBlockedDone = null
  }

  function openDrawer(item: TaskDetail) {
    selectedTask = item
  }

  function openSubEditor(item: TaskDetail) {
    subEditorTask = item
  }

  let blockChangedTimer: ReturnType<typeof setTimeout> | null = null
  $effect(() => {
    const off = ctx.on('block:changed', () => {
      if (blockChangedTimer) clearTimeout(blockChangedTimer)
      blockChangedTimer = setTimeout(() => {
        void reload()
      }, 80)
    })
    return () => {
      if (blockChangedTimer) clearTimeout(blockChangedTimer)
      off()
    }
  })

  let highlightTimer: ReturnType<typeof setTimeout> | null = null
  let focusedRowId = $state('')

  $effect(() => {
    void focusKey
    const target = focusBlockId
    if (!target) return
    if (loading) return
    queueMicrotask(() => {
      const el = document.querySelector(
        `[data-group]:not([data-group="completed"]) [data-block-id="${CSS.escape(target)}"]`
      ) as HTMLElement | null
      if (!el) return
      el.scrollIntoView({ block: 'center', behavior: 'smooth' })
      focusedRowId = target
      if (highlightTimer) clearTimeout(highlightTimer)
      highlightTimer = setTimeout(() => {
        focusedRowId = ''
        highlightTimer = null
      }, 3_000)
    })
  })

  onDestroy(() => {
    if (nowInterval) clearInterval(nowInterval)
    if (markDownTimer) clearTimeout(markDownTimer)
    if (highlightTimer) clearTimeout(highlightTimer)
  })
</script>

<div class="flex-1 flex flex-col min-h-0 overflow-hidden" data-tasks-view>
  {#if markDownError}
    <div
      class="px-6 py-2 bg-error-bg border-b border-error-border text-error text-[12px] font-body-md flex items-center gap-2"
      role="alert"
      data-testid="tasks-mark-done-error"
    >
      <span class="flex-1">Couldn't mark task done: {markDownError}</span>
      <button
        type="button"
        aria-label="Dismiss error"
        onclick={() => {
          markDownError = ''
          if (markDownTimer) {
            clearTimeout(markDownTimer)
            markDownTimer = null
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
      <div class="skeleton-container" data-testid="tasks-loading">
        {#each Array(4) as _}
          <div class="skeleton-row">
            <div class="skeleton-circle"></div>
            <div class="skeleton-text title"></div>
            <div class="skeleton-badge"></div>
          </div>
        {/each}
      </div>
    {:else if errorMsg}
      <div class="text-error" data-testid="tasks-error">
        Failed to load: {errorMsg}
      </div>
    {:else if filteredOpen.length === 0 && doneItems.length === 0}
      <div
        class="text-text-muted py-10 text-center font-body-md"
        data-testid="tasks-empty"
      >
        No tasks yet. Type below or use
        <kbd>Ctrl+Shift+N</kbd> to quickly capture one.
      </div>
    {:else}
      {#if filteredOpen.length === 0}
        <div
          class="text-center py-12 px-4 rounded-xl border border-dashed border-surface-panel-border bg-surface-panel/10 max-w-md mx-auto my-8 select-none"
        >
          <span
            class="material-symbols-outlined text-accent-primary-start text-5xl mb-2"
            aria-hidden="true">celebrate</span
          >
          <h3 class="font-headline-md text-text-primary mb-1">
            All caught up!
          </h3>
          <p class="text-text-muted text-[13px] font-body-md">
            You have no active tasks. Restore a completed task below to the
            active list, type in the box below, or use
            <kbd
              class="px-1.5 py-0.5 rounded bg-hover text-text-primary border border-surface-panel-border font-mono text-[11px]"
              >Ctrl+Shift+N</kbd
            > to capture a new task.
          </p>
        </div>
      {/if}

      {#each [{ key: 'overdue', label: 'Overdue', list: overdue, tone: 'error' }, { key: 'today', label: 'Today', list: todayItems, tone: 'primary' }, { key: 'upcoming', label: 'Upcoming', list: upcoming, tone: 'muted' }, { key: 'later', label: 'Later', list: later, tone: 'muted' }, { key: 'undated', label: 'No Date', list: undated, tone: 'muted' }] as group (group.key)}
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
              >
                {group.list.length}
              </span>
            </h2>
            <div class="space-y-1">
              {#each group.list as item (item.id)}
                <div
                  class="group flex items-center gap-3 px-3 py-2 rounded-lg hover:bg-hover transition-colors"
                  class:tasks-focused={focusedRowId === item.id}
                  data-block-id={item.id}
                >
                  <button
                    onclick={(e) => {
                      e.stopPropagation()
                      markDone(item)
                    }}
                    title="Mark done"
                    class="w-5 h-5 rounded todo-check flex-shrink-0 cursor-pointer hover:border-accent-primary-start"
                    role="checkbox"
                    aria-checked="false"
                    aria-label="Mark done"
                  ></button>
                  <button
                    onclick={() => openDrawer(item)}
                    onkeydown={(e) => {
                      if (e.key === 'Enter' && e.shiftKey) {
                        e.preventDefault()
                        e.stopPropagation()
                        openSubEditor(item)
                      }
                    }}
                    class="flex-1 min-w-0 text-left bg-transparent border-none p-0 cursor-pointer"
                    aria-label={`Edit metadata for ${item.clean_content}${item.due_date ? `, due ${item.due_date}` : ', no due date'}`}
                  >
                    <div
                      class="text-text-primary text-sm font-body-md truncate"
                      data-testid="tasks-row-content"
                    >
                      {item.clean_content}
                    </div>
                    <div
                      class="text-[10px] text-text-muted uppercase tracking-widest font-label-sm"
                    >
                      {#if item.notebook === STANDALONE_TASKS_NOTEBOOK}
                        Standalone task
                      {:else}
                        {item.notebook} › {item.section} › {item.page}
                      {/if}
                    </div>
                  </button>
                  <button
                    type="button"
                    title="Open sub-editor (Shift+Enter)"
                    aria-label={`Edit notes for ${item.clean_content}`}
                    onclick={(e) => {
                      e.stopPropagation()
                      openSubEditor(item)
                    }}
                    class="opacity-40 hover:opacity-100 focus-visible:opacity-100 text-text-muted hover:text-accent-primary-start transition-opacity p-1 rounded border-none bg-transparent cursor-pointer flex-shrink-0"
                  >
                    <span class="material-symbols-outlined text-[16px]"
                      >edit_note</span
                    >
                  </button>
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
              <span class="text-text-muted/60" aria-hidden="true"
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
                      {#if item.notebook === STANDALONE_TASKS_NOTEBOOK}
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
      {#if openTruncated || doneTruncated}
        <p
          class="text-text-muted text-[12px] font-body-md border-t border-surface-panel-border pt-3 mt-6"
          role="status"
          aria-live="polite"
          data-testid="tasks-truncated-notice"
        >
          Showing the first
          {filteredOpen.length + doneItems.length}
          tasks — there are more below the display limit. Complete or reschedule some
          to surface them.
        </p>
      {/if}
    {/if}
  </div>

  <div
    class="px-6 py-3 border-t border-surface-panel-border bg-surface-panel flex-shrink-0"
    data-testid="tasks-inline-quickadd"
  >
    <QuickAddTask
      {ctx}
      placeholder="Add a task — Enter to add"
      keepOpenAfterCreate={true}
      autofocus={false}
      clearOnEscape={true}
    />
  </div>
</div>

<TaskEditDrawer
  task={selectedTask}
  {ctx}
  onMetaChanged={reload}
  onOpenSubEditor={() => selectedTask && (subEditorTask = selectedTask)}
  onClose={() => (selectedTask = null)}
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
      reload()
      subEditorTask = null
    }}
  />
{/if}
{#if pendingBlockedDone}
  <BlockedDoneDialog
    cardText={pendingBlockedDone.item.clean_content}
    blockers={pendingBlockedDone.blockers}
    onConfirm={confirmBlockedDone}
    onCancel={cancelBlockedDone}
  />
{/if}

<style>
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
  .todo-check:focus-visible {
    outline: 2px solid var(--color-accent-primary-start);
    outline-offset: 2px;
  }
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
