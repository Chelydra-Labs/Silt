<script lang="ts">
  import { SvelteSet } from 'svelte/reactivity'
  // List display mode of the Tasks hub (#424/#526). Time-horizon grouping
  // (Overdue/Today/Upcoming/Later/No Date/Completed) is the legacy Tasks
  // behavior; the grouping engine (#423) generalizes sections into arbitrary
  // dimensions. Filtering is server-side via buildQuery so hub FilterBar +
  // scope + smart-list constraints apply before the LIMIT 500 cap.
  //
  // The header (title + count) lives in TasksHub.svelte; this component
  // reports its open/done counts upward via onCountChange.
  import { onMount, onDestroy, tick, untrack } from 'svelte'
  import { flip } from 'svelte/animate'
  import { cubicOut } from 'svelte/easing'
  import type { PluginManifest } from '../../../sdk'
  import { endOfWeekISO } from '../../../../lib/dateGrid'
  import { buildFtsMatch } from '../../../../lib/ftsQuery'
  import { getTaskWeekStart } from '../../../../lib/taskWeekStart.svelte'
  import { STANDALONE_TASKS_NOTEBOOK } from '../../../../lib/standaloneTasksNav'
  import QuickAddTask from '../components/QuickAddTask.svelte'
  import PinToggle from '../components/PinToggle.svelte'
  import { friendlyCaughtError } from '../errors'
  import TaskEditDrawer from '../components/TaskEditDrawer.svelte'
  import TaskSubEditorModal from '../components/TaskSubEditorModal.svelte'
  import BlockedDoneGuard from '../components/BlockedDoneGuard.svelte'
  import SidebarResizeHandle from '../../../../components/SidebarResizeHandle.svelte'
  import {
    canFitInspectorSplit,
    clampInspectorPaneWidth,
    DEFAULT_INSPECTOR_SPLIT_RATIO,
    inspectorSplitRatioFromWidth,
    loadInspectorPaneWidth,
    maxInspectorPaneWidthForHost,
    MIN_INSPECTOR_PANE_WIDTH,
    MIN_LIST_MASTER_WIDTH,
    onTasksSettingsChanged,
    persistInspectorPaneWidth,
    referenceInspectorPaneWidthFromRatio,
    resolveInspectorPaneWidthForHost
  } from '../settings'
  import {
    coerceTaskRow,
    formatEstimateSum,
    PRIORITY_LABELS,
    priorityClass,
    type TaskDetail,
    type TaskViewProps
  } from '../types'
  import { dueDateClass, dueDateTextClass } from '../dueDate'
  import ErrorBanner from '../components/ErrorBanner.svelte'
  import { getTaskHubQueryContext, getTaskHubViewState } from '../state.svelte'
  import { binByDimension, type GroupSection } from '../grouping'
  import { buildQuery } from '../query'
  import { useBlockChangedReload, useBlockedDoneGuard } from '../shared.svelte'
  import { compareTasks, sortTasks } from '../taskOrder'
  import { trackTaskUndoWrite } from '../undo.svelte'

  interface Props extends TaskViewProps {
    /** Unused now (title lives in the hub); kept for direct-render compat. */
    manifest?: PluginManifest
    focusBlockId?: string
    focusKey?: string
  }

  let {
    ctx,
    manifest: _manifest,
    focusBlockId = '',
    focusKey = '',
    onCountChange
  }: Props = $props()

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
  let pinError = $state('')
  let pinErrorTimer: ReturnType<typeof setTimeout> | null = null
  // Pin writes re-bin the row into a different {#each} block, which destroys
  // the focused button; this holds the id to chase after the reload.
  let pendingPinFocus = $state<{
    id: string
    /** Target section; null means the write failed (focus once, no chase). */
    pinned: boolean | null
  } | null>(null)
  let pinFocusTimer: ReturnType<typeof setTimeout> | null = null
  let openTruncated = $state(false)
  let doneTruncated = $state(false)
  let showCompleted = $state(false)

  let selectedTask = $state<TaskDetail | null>(null)
  let selectedFilteredOut = $state(false)
  let subEditorTask = $state<TaskDetail | null>(null)
  let hostEl = $state<HTMLDivElement | null>(null)
  let hostWidth = $state(0)
  /** False until the host has a measured width — avoids overlay→pane flash. */
  let hostMeasured = $state(false)
  let paneRatio = $state(inspectorSplitRatioFromWidth(loadInspectorPaneWidth()))
  /** Inspector sidebar popover/dialog busy — blocks J/K while open. */
  let inspectorBusy = $state(false)
  // DONE-on-blocked guard (#302): shared hook owns pending state + blocker
  // fetch. ListView attaches the item so confirmBlockedDone can hand it to
  // commitMarkDown. On a fetch failure ListView surfaces markDownError + an
  // 8s auto-dismiss timer (matches the inline behavior pre-extraction).
  // ctx is a stable plugin-context singleton — see BoardView for rationale.
  // svelte-ignore state_referenced_locally
  const blockedGuard = useBlockedDoneGuard<TaskDetail>(ctx, (e) => {
    markDownError = friendlyCaughtError(e)
    markDownTimer = setTimeout(() => {
      markDownError = ''
      markDownTimer = null
    }, 8_000)
  })

  // Monotonic token so concurrent reload() calls can identify their own
  // response vs a later one (rapid scope/filter switches).
  let loadSeq = 0
  async function reload() {
    const my = ++loadSeq
    // Keep existing rows visible during refresh; only skeleton on first load.
    // untrack: reading open/done here must not re-subscribe the $effect that
    // calls reload (would loop on every openItems assignment).
    const hadRows = untrack(() => openItems.length > 0 || doneItems.length > 0)
    if (!hadRows) loading = true
    errorMsg = ''
    try {
      const hub = getTaskHubViewState()
      const ctxLike = getTaskHubQueryContext({
        activeNotebook: ctx.activeNotebook,
        activeSection: ctx.activeSection,
        activePage: ctx.activePage,
        today: ctx.today
      })

      // Open path: buildQuery with status:'open' + LIMIT 500. Skip entirely
      // when the smart-list is Completed (open set is empty by definition).
      const openPromise =
        hub.activeFilter === 'completed'
          ? Promise.resolve({ rows: [] as unknown[], truncated: false })
          : (() => {
              // status:'open' forces open-only even when activeFilter is 'all'
              // (date smart-lists / stale already add the same clause — fine).
              // Filter-then-limit: under sort:manual, groups past the 500th
              // matching row never load, so within-group DnD can look incomplete.
              const { sql, params } = buildQuery(
                hub.scope,
                hub.filters,
                ctxLike,
                {
                  groupBy: hub.groupBy,
                  sort: hub.sort,
                  activeFilter: hub.activeFilter,
                  status: 'open',
                  limit: 500
                }
              )
              return ctx.sqliteQuery(sql, params)
            })()

      // Done path: completed rows honoring every hub filter except dueDate,
      // stale, and shortlist (all three describe the current open-work focus,
      // so they would empty or reorder the Completed history section). Text
      // must ride along — otherwise "Find task…" visibly narrows open rows
      // while completed rows ignore it. Skip for date smart-lists (Completed
      // section is hidden there).
      const donePromise =
        hub.activeFilter === 'all' || hub.activeFilter === 'completed'
          ? (() => {
              const { sql, params } = buildQuery(
                hub.scope,
                {
                  ...hub.filters,
                  dueDate: '',
                  stale: false,
                  shortlist: false
                },
                ctxLike,
                {
                  activeFilter: 'completed',
                  status: 'done',
                  orderBy: 'b.file_date DESC',
                  limit: 200
                }
              )
              return ctx.sqliteQuery(sql, params)
            })()
          : Promise.resolve({ rows: [] as unknown[], truncated: false })

      const [openRes, doneRes] = await Promise.all([openPromise, donePromise])
      if (my !== loadSeq) return
      openItems = ((openRes.rows as unknown[]) ?? []).map((r) =>
        coerceTaskRow(r)
      )
      doneItems = (doneRes.rows as unknown as CompletedTaskItem[]) ?? []
      if (selectedTask) {
        const fresh = openItems.find((i) => i.id === selectedTask!.id)
        if (fresh) {
          selectedTask = fresh
          selectedFilteredOut = false
        } else {
          selectedFilteredOut = true
        }
      }
      // Backend truncated only fires at maxPluginQueryRows (5000), not SQL LIMIT.
      // Signal intentional design caps so the footer is useful.
      openTruncated = openRes.truncated || openItems.length >= 500
      doneTruncated = doneRes.truncated || doneItems.length >= 200
    } catch (e) {
      if (my !== loadSeq) return
      errorMsg = e instanceof Error ? e.message : String(e)
    } finally {
      if (my === loadSeq) loading = false
    }
  }

  let nowTick = $state(0)
  let nowInterval: ReturnType<typeof setInterval> | undefined

  onMount(() => {
    nowInterval = setInterval(() => {
      nowTick++
    }, 60_000)
  })

  // Measure the list host (not window) so narrow hub panes fall back to overlay.
  $effect(() => {
    const el = hostEl
    if (!el) return
    const applyWidth = (w: number) => {
      hostWidth = w
      hostMeasured = true
    }
    if (typeof ResizeObserver === 'undefined') {
      applyWidth(el.clientWidth)
      return
    }
    const ro = new ResizeObserver((entries) => {
      const w = entries[0]?.contentRect?.width ?? el.clientWidth
      applyWidth(w)
    })
    ro.observe(el)
    applyWidth(el.clientWidth)
    return () => ro.disconnect()
  })

  // Re-read persisted pane width after async settings hydrate / config reload.
  $effect(() => {
    return onTasksSettingsChanged(() => {
      paneRatio = inspectorSplitRatioFromWidth(loadInspectorPaneWidth())
    })
  })

  let usePane = $derived(
    !!selectedTask && hostMeasured && canFitInspectorSplit(hostWidth)
  )
  /** Defer mounting either inspector shell until host width is known. */
  let inspectorReady = $derived(!!selectedTask && hostMeasured)
  let paneMaxForHost = $derived(maxInspectorPaneWidthForHost(hostWidth))
  let defaultPaneWidthForHost = $derived(
    resolveInspectorPaneWidthForHost(hostWidth, DEFAULT_INSPECTOR_SPLIT_RATIO)
  )
  let effectivePaneWidth = $derived(
    resolveInspectorPaneWidthForHost(hostWidth, paneRatio)
  )

  function setPaneWidth(w: number) {
    const clamped = Math.min(clampInspectorPaneWidth(w), paneMaxForHost)
    paneRatio = inspectorSplitRatioFromWidth(clamped, hostWidth)
  }

  function commitPaneWidth(w: number) {
    const clamped = Math.min(clampInspectorPaneWidth(w), paneMaxForHost)
    const nextRatio = inspectorSplitRatioFromWidth(clamped, hostWidth)
    paneRatio = nextRatio
    void persistInspectorPaneWidth(
      referenceInspectorPaneWidthFromRatio(nextRatio)
    )
  }

  // Must be declared before the reload $effect so day-boundary ticks
  // re-subscribe and re-query date filters (overdue/today/upcoming).
  let today = $derived.by(() => {
    void nowTick
    return ctx.today
  })
  let weekStart = $derived(getTaskWeekStart())
  let weekEnd = $derived(endOfWeekISO(today, weekStart))

  // Reload whenever any reactive input the query depends on changes.
  $effect(() => {
    const hub = getTaskHubViewState()
    void hub.scope
    void hub.groupBy
    void hub.sort
    void hub.activeFilter
    void weekStart
    void today
    void ctx.activeNotebook
    void ctx.activeSection
    void ctx.activePage
    void hub.filters.owners
    void hub.filters.priorities
    void hub.filters.dueDate
    void hub.filters.tags
    void hub.filters.stale
    void hub.filters.shortlist
    void hub.filters.text
    void reload()
  })

  // Server-side filtering (#526): openItems already match hub scope/filters.
  let filteredOpen = $derived(openItems)

  // Smart-list filter for the Completed section (#432): 'completed' shows
  // only done rows; the other smart-list values empty the Completed section
  // (date-based smart lists are open-task scopes by definition).
  let activeFilter = $derived(getTaskHubViewState().activeFilter)
  let filteredDone = $derived(
    activeFilter === 'all' || activeFilter === 'completed' ? doneItems : []
  )

  // True when the list is narrowed by scope, smart-list, or FilterBar facets.
  // Used so a zero-result filtered query does not claim "All caught up" /
  // "No tasks yet" when tasks may still exist outside the current filters.
  let hasActiveListFilters = $derived.by(() => {
    const hub = getTaskHubViewState()
    if (hub.scope !== 'vault') return true
    if (hub.activeFilter !== 'all') return true
    if (hub.filters.owners.length > 0) return true
    if (hub.filters.priorities.length > 0) return true
    if (hub.filters.dueDate) return true
    if (hub.filters.tags.length > 0) return true
    if (hub.filters.stale) return true
    if (hub.filters.shortlist) return true
    // Text with no surviving FTS term changes no SQL, so it is not a filter.
    if (buildFtsMatch(hub.filters.text ?? '')) return true
    return false
  })

  // Report counts upward so the hub header stays in sync. Runs after every
  // reload / filter / nav change. Both counts reflect what's displayed:
  // open uses filteredOpen (filtered), done uses filteredDone (filtered to
  // the active smart-list). Using unfiltered doneItems here would inflate
  // the header count whenever a smart-list scope hid the completed rows.
  $effect(() => {
    const open = filteredOpen.length
    const done = filteredDone.length
    onCountChange?.(open, done)
  })

  // The hub's current grouping + sort dimensions, read reactively so a
  // selector change re-bins the rows without a re-query.
  let hubGroupBy = $derived(getTaskHubViewState().groupBy)
  let hubSort = $derived(getTaskHubViewState().sort)
  // Rank leads every client-side re-sort while the shortlist filter is the
  // active lens (#1092), mirroring query.ts's rankLead composition — the
  // SQL already orders rows rank-first, but buckets/sections re-sort below.
  let shortlistFirst = $derived(
    getTaskHubViewState().filters.shortlist ?? false
  )

  // Pinned open tasks render in their own block above every grouping branch,
  // so the group derivations below read `unpinnedOpen` and never duplicate a
  // pinned row (#1091).
  let pinnedOpen = $derived(
    sortTasks(
      hubSort,
      filteredOpen.filter((i) => i.pinned),
      { shortlistFirst }
    )
  )
  let unpinnedOpen = $derived(filteredOpen.filter((i) => !i.pinned))

  let overdue = $derived(
    unpinnedOpen.filter((i) => !!i.due_date && i.due_date < today)
  )
  let todayItems = $derived(
    unpinnedOpen.filter((i) => !!i.due_date && i.due_date === today)
  )
  let upcoming = $derived(
    unpinnedOpen
      .filter(
        (i) => !!i.due_date && i.due_date > today && i.due_date <= weekEnd
      )
      .sort((a, b) => compareTasks('dueDate', a, b, { shortlistFirst }))
  )
  let later = $derived(
    unpinnedOpen
      .filter((i) => !!i.due_date && i.due_date > weekEnd)
      .sort((a, b) => compareTasks('dueDate', a, b, { shortlistFirst }))
  )
  let undated = $derived(unpinnedOpen.filter((i) => !i.due_date))

  function sectionEstimateLabel(items: TaskDetail[]): string {
    const sum = items.reduce((acc, t) => acc + (t.estimate_minutes ?? 0), 0)
    if (sum <= 0) return ''
    return `${formatEstimateSum(sum)} estimated`
  }

  function taskTags(item: TaskDetail): string[] {
    return (item.tags ?? '')
      .split('|')
      .map((tag) => tag.trim())
      .filter(Boolean)
  }

  // Generalized grouping for status/owner/priority/tag/notebook/section/page.
  // 'none' returns a single flat section; 'dueDate' is handled by the legacy
  // $derived blocks above so the existing data-group keys + tone classes stay
  // byte-exact (preserves every ListView test).
  let groupedSections = $derived.by<GroupSection[]>(() => {
    if (hubGroupBy === 'dueDate' || hubGroupBy === 'none') return []
    const sections = binByDimension(unpinnedOpen, hubGroupBy, {
      today,
      weekStart
    })
    // Apply the within-section sort to each non-empty bucket; drop empty
    // buckets so the list stays compact when the data is sparse.
    return sections
      .filter((s) => s.items.length > 0)
      .map((s) => ({
        ...s,
        items: sortTasks(hubSort, s.items, { shortlistFirst })
      }))
  })

  // Collapsed state for the generalized sections. Tail-collapse heuristic:
  // when there are more than 10 sections, the ones after the 10th start
  // collapsed so the user isn't buried under a wall of headers. Stored as
  // a Set of keys so the user expanding one survives a re-bin.
  let collapsedSections = new SvelteSet<string>()
  $effect(() => {
    // Re-run when the groupBy dimension changes (the section keys change
    // shape with it) and seed the tail-collapse set.
    void hubGroupBy
    collapsedSections.clear()
    if (groupedSections.length > 10) {
      for (let i = 10; i < groupedSections.length; i++) {
        collapsedSections.add(groupedSections[i].key)
      }
    }
  })
  function toggleSection(key: string) {
    if (collapsedSections.has(key)) collapsedSections.delete(key)
    else collapsedSections.add(key)
  }

  // Removing the focused row takes the hub's Ctrl+Z scope with it (focus
  // falls to a detached node or <body>), so land focus on the row that slides
  // into its place — or the heading when the list empties.
  function rowIdAfterRemoval(removedId: string): string {
    const rowIds = Array.from(
      document.querySelectorAll<HTMLElement>(
        '[data-group]:not([data-group="completed"]) [data-block-id]'
      )
    ).map((row) => row.dataset.blockId ?? '')
    const removedIdx = rowIds.indexOf(removedId)
    if (removedIdx < 0) return ''
    return rowIds[removedIdx + 1] ?? rowIds[removedIdx - 1] ?? ''
  }

  async function focusAfterRowRemoval(nextId: string): Promise<void> {
    await tick()
    const row = nextId
      ? Array.from(
          document.querySelectorAll<HTMLElement>('[data-block-id]')
        ).find((el) => el.dataset.blockId === nextId)
      : null
    const target =
      row?.querySelector<HTMLElement>(
        'button[aria-label^="Edit metadata for"]'
      ) ?? document.querySelector<HTMLElement>('[data-tasks-hub] h1')
    target?.focus()
  }

  // Pin failures auto-dismiss like markDownError; a new commit clears the
  // previous message (optimisticField calls onError('') up front).
  function setPinError(message: string) {
    pinError = message
    if (pinErrorTimer) clearTimeout(pinErrorTimer)
    pinErrorTimer = null
    if (!message) return
    pinErrorTimer = setTimeout(() => {
      pinError = ''
      pinErrorTimer = null
    }, 8_000)
  }

  // Track the pin write so the hub's Undo button stays disabled while it
  // lands (same contract as done/reorder); focus follows the re-binned row.
  function schedulePinFocus(id: string, pinned: boolean | null) {
    pendingPinFocus = { id, pinned }
    if (pinFocusTimer) clearTimeout(pinFocusTimer)
    pinFocusTimer = setTimeout(() => {
      pinFocusTimer = null
      pendingPinFocus = null
    }, 1_500)
  }

  async function togglePin(item: TaskDetail, next: boolean) {
    let ok = false
    try {
      await trackTaskUndoWrite(ctx.updateTaskMeta(item.id, { pinned: next }))
      ok = true
    } finally {
      // The pending-disable blurs the button to <body>; restore focus even
      // on failure so the hub's Ctrl+Z scope survives.
      schedulePinFocus(item.id, ok ? next : null)
    }
    liveMessage = next ? 'Task pinned' : 'Task unpinned'
  }

  // Focus the old node once the button re-enables, then re-focus the re-binned
  // one after the debounced reload swapped {#each} blocks. Gives up when the
  // element reaches its target section (or the expiry timer fires).
  $effect(() => {
    void openItems
    const pending = pendingPinFocus
    if (!pending) return
    void tick().then(() => {
      if (pendingPinFocus !== pending) return
      const el = document.querySelector<HTMLButtonElement>(
        `[data-testid="tasks-pin-${CSS.escape(pending.id)}"]`
      )
      if (!el) return
      el.focus()
      if (pending.pinned === null) {
        // Failed write: the row never re-bins, so one refocus is enough once
        // the button re-enables.
        if (el.disabled) return
      } else {
        const group = el.closest('[data-group]')?.getAttribute('data-group')
        if (pending.pinned ? group !== 'pinned' : group === 'pinned') return
      }
      if (pinFocusTimer) clearTimeout(pinFocusTimer)
      pinFocusTimer = null
      pendingPinFocus = null
    })
  })

  async function commitMarkDown(item: TaskDetail) {
    markDownError = ''
    if (markDownTimer) clearTimeout(markDownTimer)
    try {
      await trackTaskUndoWrite(ctx.updateBlockState(item.id, 'DONE'))
      const nextRowId = rowIdAfterRemoval(item.id)
      openItems = openItems.filter((i) => i.id !== item.id)
      await focusAfterRowRemoval(nextRowId)
    } catch (e) {
      markDownError = friendlyCaughtError(e)
      markDownTimer = setTimeout(() => {
        markDownError = ''
        markDownTimer = null
      }, 8_000)
    }
  }

  async function markDone(item: TaskDetail) {
    if (blockedGuard.pending) return
    if (item.is_blocked) {
      // 'dialog' → BlockedDoneDialog opened (bail); 'error' → onError already
      // surfaced markDownError (bail); 'clear' → proceed to commit.
      const result = await blockedGuard.check(item.id, item.is_blocked, item)
      if (result !== 'clear') return
    }
    await commitMarkDown(item)
  }

  function confirmBlockedDone() {
    const pending = blockedGuard.pending
    blockedGuard.dismiss()
    if (pending) void commitMarkDown(pending.context)
  }

  function cancelBlockedDone() {
    blockedGuard.dismiss()
  }

  function selectTask(item: TaskDetail) {
    if (selectedTask?.id === item.id) {
      selectedTask = null
      selectedFilteredOut = false
      return
    }
    selectedTask = item
    selectedFilteredOut = false
  }

  function openSubEditor(item: TaskDetail) {
    subEditorTask = item
  }

  // Rendered open-task order for J/K and prev/next (matches section layout).
  let flatOpenOrder = $derived.by((): TaskDetail[] => {
    if (hubGroupBy === 'dueDate') {
      return [
        ...pinnedOpen,
        ...overdue,
        ...todayItems,
        ...upcoming,
        ...later,
        ...undated
      ]
    }
    if (hubGroupBy === 'none') {
      return [
        ...pinnedOpen,
        ...sortTasks(hubSort, unpinnedOpen, { shortlistFirst })
      ]
    }
    const out: TaskDetail[] = []
    for (const g of groupedSections) {
      if (collapsedSections.has(g.key)) continue
      out.push(...g.items)
    }
    return [...pinnedOpen, ...out]
  })

  let selectedIndex = $derived(
    selectedTask
      ? flatOpenOrder.findIndex((t) => t.id === selectedTask!.id)
      : -1
  )
  // Off-order selection (filtered out OR collapsed section) still allows
  // jumping into the visible order so buttons and J/K stay aligned.
  let hasPrevTask = $derived(
    flatOpenOrder.length > 0 && (selectedIndex > 0 || selectedIndex < 0)
  )
  let hasNextTask = $derived(
    flatOpenOrder.length > 0 &&
      (selectedIndex < 0 || selectedIndex < flatOpenOrder.length - 1)
  )

  function goPrevTask() {
    if (flatOpenOrder.length === 0) return
    if (selectedIndex < 0) {
      // Not in rendered order: K jumps to the last visible task.
      selectedTask = flatOpenOrder[flatOpenOrder.length - 1]
      selectedFilteredOut = false
      return
    }
    if (selectedIndex <= 0) return
    selectedTask = flatOpenOrder[selectedIndex - 1]
    selectedFilteredOut = false
  }

  function goNextTask() {
    if (flatOpenOrder.length === 0) return
    if (selectedIndex < 0) {
      // Not in rendered order: J jumps to the first visible task.
      selectedTask = flatOpenOrder[0]
      selectedFilteredOut = false
      return
    }
    if (selectedIndex >= flatOpenOrder.length - 1) return
    selectedTask = flatOpenOrder[selectedIndex + 1]
    selectedFilteredOut = false
  }

  function onListTraversalKeydown(e: KeyboardEvent) {
    if (!selectedTask || inspectorBusy) return
    const key = e.key
    if (key !== 'j' && key !== 'J' && key !== 'k' && key !== 'K') return
    const t = e.target
    if (t instanceof HTMLElement) {
      const tag = t.tagName
      if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') return
      if (t.isContentEditable) return
      if (t.closest('[role="dialog"], [role="listbox"], [role="menu"]')) return
    }
    e.preventDefault()
    if (key === 'j' || key === 'J') goNextTask()
    else goPrevTask()
  }

  $effect(() => {
    if (!selectedTask) return
    window.addEventListener('keydown', onListTraversalKeydown)
    return () => window.removeEventListener('keydown', onListTraversalKeydown)
  })

  // Repaint on any block mutation (created/mutated/rescheduled from any
  // surface). Debounced so a burst of block:changed events triggers one reload.
  // svelte-ignore state_referenced_locally
  useBlockChangedReload(ctx, reload)

  let highlightTimer: ReturnType<typeof setTimeout> | null = null
  let focusedRowId = $state('')

  // --- Manual-order DnD (#426) ------------------------------------------
  // Sort:'manual' turns each row into a drag source with a drag_indicator
  // handle. The handle is the only draggable element so the row body still
  // single-clicks into the edit drawer. Within a section/group, dropping a
  // row splices it into the target slot and renumbers the 1-based positions
  // for every shifted task; cross-group drops are a no-op here (BoardView
  // owns the dimension-reassign path; the List is for within-group order).
  let dragItem = $state<TaskDetail | null>(null)
  let dragGroupKey = ''
  let draggingId = $state<string | null>(null)
  let dragOverId = $state<string | null>(null)
  let orderError = $state('')
  let orderErrorTimer: ReturnType<typeof setTimeout> | null = null
  let liveMessage = $state('')

  function flashOrderError(e: unknown) {
    // Friendly coercion: typed failures (e.g. verified_write_failed naming
    // retained copies) ride the v3 cause envelope.
    orderError = friendlyCaughtError(e)
    if (orderErrorTimer) clearTimeout(orderErrorTimer)
    orderErrorTimer = setTimeout(() => {
      orderError = ''
      orderErrorTimer = null
    }, 8_000)
  }

  // Read the live ordered id sequence for a group from the DOM. The rows
  // are already rendered in their sorted order (sortTasks / binByDimension),
  // so the DOM is the source of truth for "what the user sees right now" —
  // including any optimistic in-place reorder.
  function groupItemIds(groupKey: string): string[] {
    const section = document.querySelector(
      `[data-group="${CSS.escape(groupKey)}"]`
    )
    if (!section) return []
    return Array.from(section.querySelectorAll('[data-block-id]'))
      .map((el) => el.getAttribute('data-block-id') ?? '')
      .filter(Boolean)
  }

  function onRowDragStart(e: DragEvent, item: TaskDetail, groupKey: string) {
    if (hubSort !== 'manual') {
      e.preventDefault()
      return
    }
    dragItem = item
    dragGroupKey = groupKey
    draggingId = item.id
    if (e.dataTransfer) {
      e.dataTransfer.effectAllowed = 'move'
      e.dataTransfer.setData('text/plain', item.id)
    }
  }

  function onRowDragEnd() {
    dragItem = null
    dragGroupKey = ''
    draggingId = null
    dragOverId = null
  }

  // Same lens gate as Board's card-level handlers (1d830527): while the
  // shortlist lens leads, rank dominates manual_order, so a drop would
  // renumber the section from the rank-sorted DOM — the drop is a no-op.
  function onRowDragOver(e: DragEvent, item: TaskDetail, groupKey: string) {
    if (hubSort !== 'manual' || shortlistFirst || !dragItem) return
    // Only within-group reorder; cross-group is a no-op (see Block above).
    if (groupKey !== dragGroupKey) return
    e.preventDefault()
    if (e.dataTransfer) e.dataTransfer.dropEffect = 'move'
    dragOverId = item.id
  }

  function onRowDrop(e: DragEvent, target: TaskDetail, groupKey: string) {
    if (hubSort !== 'manual' || shortlistFirst || !dragItem) return
    e.preventDefault()
    e.stopPropagation()
    const src = dragItem
    const srcId = src.id
    const targetId = target.id
    const wasOverId = dragOverId
    dragItem = null
    draggingId = null
    dragOverId = null
    if (groupKey !== dragGroupKey) return
    if (srcId === targetId) return

    const ids = groupItemIds(groupKey)
    const fromIdx = ids.indexOf(srcId)
    const toIdx = ids.indexOf(targetId)
    if (fromIdx === -1 || toIdx === -1) return
    const reordered = [...ids]
    reordered.splice(fromIdx, 1)
    // Splice-dance for "land BEFORE the target": removing src shifts the
    // target's index down by one when src was above it, so recompute the
    // insertion point against the post-removal array.
    const insertAt = fromIdx < toIdx ? toIdx - 1 : toIdx
    reordered.splice(insertAt, 0, srcId)

    // Renumber 1..N; collect the diffs so we only persist the tasks whose
    // 1-based position actually changed (typically the moved row + the
    // shifted rows between old and new positions).
    const changes: { id: string; order: number }[] = []
    reordered.forEach((id, i) => {
      const newOrder = i + 1
      const row = openItems.find((r) => r.id === id)
      if (row && (row.manual_order ?? 0) !== newOrder) {
        changes.push({ id, order: newOrder })
      }
    })
    if (changes.length === 0) return

    // Optimistic: patch local state so the row snaps to its new slot before
    // the IPC round-trip. Revert on error so a failed persist can't strand
    // the row at a slot the file doesn't back.
    const prevOrders = new Map(
      openItems.map((r) => [r.id, r.manual_order ?? 0])
    )
    openItems = openItems.map((r) => {
      const c = changes.find((x) => x.id === r.id)
      return c ? { ...r, manual_order: c.order } : r
    })
    liveMessage = `Task moved to position ${
      changes.find((c) => c.id === srcId)?.order ?? ''
    }.`

    void trackTaskUndoWrite(
      ctx.setTaskOrders(changes.map((c) => ({ id: c.id, order: c.order })))
    ).catch((err) => {
      // Revert every optimistic update; the next reload will surface the
      // file's actual state. One batch write = one revert on failure.
      openItems = openItems.map((r) => {
        if (!changes.some((x) => x.id === r.id)) return r
        return {
          ...r,
          manual_order: prevOrders.get(r.id) ?? r.manual_order
        }
      })
      flashOrderError(err)
      liveMessage = 'Reorder failed — reverted.'
    })
    // Prevent the wasOverId leak when the drop fires before dragleave.
    void wasOverId
  }

  $effect(() => {
    void focusKey
    const target = focusBlockId
    if (!target) return
    if (loading) return
    queueMicrotask(() => {
      const el = document.querySelector(
        `[data-group]:not([data-group="completed"]) [data-block-id="${CSS.escape(target)}"]`
      )
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
    if (pinErrorTimer) clearTimeout(pinErrorTimer)
    if (pinFocusTimer) clearTimeout(pinFocusTimer)
    if (highlightTimer) clearTimeout(highlightTimer)
    if (orderErrorTimer) clearTimeout(orderErrorTimer)
  })
</script>

{#snippet taskRow(item: TaskDetail, groupKey: string)}
  <div
    class="tasks-list-row group relative flex min-h-12 items-center gap-3 overflow-hidden rounded-lg border border-transparent px-3 py-2 pl-3.5 transition-all hover:border-surface-card-border hover:bg-hover hover:shadow-sm focus-within:border-border-focus focus-within:bg-hover"
    class:tasks-selected={selectedTask?.id === item.id}
    class:tasks-focused={focusedRowId === item.id}
    class:tasks-drag-over={dragOverId === item.id}
    class:tasks-dragging={draggingId === item.id}
    data-block-id={item.id}
    data-group-key={groupKey}
    data-status={item.status}
    data-task-hit
    aria-current={selectedTask?.id === item.id ? 'true' : undefined}
    role="listitem"
    ondragover={(e) => onRowDragOver(e, item, groupKey)}
    ondrop={(e) => onRowDrop(e, item, groupKey)}
    ondragleave={() => {
      if (dragOverId === item.id) dragOverId = null
    }}
  >
    {#if hubSort === 'manual'}
      <span
        class="material-symbols-outlined text-icon-sm text-text-muted/60 hover:text-text-muted cursor-grab active:cursor-grabbing flex-shrink-0 select-none"
        draggable="true"
        aria-hidden="true"
        title="Drag to reorder"
        data-testid={`tasks-row-drag-handle-${item.id}`}
        ondragstart={(e) => onRowDragStart(e, item, groupKey)}
        ondragend={onRowDragEnd}>drag_indicator</span
      >
    {/if}
    <button
      onclick={(e) => {
        e.stopPropagation()
        void markDone(item)
      }}
      title="Mark done"
      class="w-5 h-5 rounded todo-check flex-shrink-0 cursor-pointer border-surface-card-border bg-surface-card hover:border-accent-primary-start hover:bg-accent-primary-glow transition-colors"
      role="checkbox"
      aria-checked="false"
      aria-label="Mark done"
    ></button>
    <button
      onclick={() => selectTask(item)}
      onkeydown={(e) => {
        if (e.key === 'Enter' && e.shiftKey) {
          e.preventDefault()
          e.stopPropagation()
          openSubEditor(item)
        }
      }}
      class="flex-1 min-w-0 rounded text-left bg-transparent border-none p-0 cursor-pointer focus-visible:outline-none"
      aria-label={`Edit metadata for ${item.clean_content}${item.notebook === STANDALONE_TASKS_NOTEBOOK ? ', standalone task' : ''}${item.due_date ? `, due ${item.due_date}` : ', no due date'}${item.subtask_total > 0 ? `, ${item.subtask_done} of ${item.subtask_total} subtasks done` : ''}${item.shortlist_rank != null ? `, shortlist rank ${item.shortlist_rank}` : ''}`}
    >
      <div
        class="truncate text-type-md font-body-md font-medium text-text-primary transition-colors group-hover:text-accent-primary-start"
        data-testid="tasks-row-content"
      >
        {item.clean_content}
      </div>
      {#if item.notebook !== STANDALONE_TASKS_NOTEBOOK}
        <div class="truncate text-type-2xs text-text-muted font-label-sm">
          {item.notebook} › {item.section} › {item.page}
        </div>
      {/if}
    </button>
    <PinToggle
      pinned={item.pinned}
      onToggle={(next) => togglePin(item, next)}
      onError={setPinError}
      testId={`tasks-pin-${item.id}`}
    />
    <button
      type="button"
      title="Open sub-editor (Shift+Enter)"
      aria-label={`Edit notes for ${item.clean_content}`}
      onclick={(e) => {
        e.stopPropagation()
        openSubEditor(item)
      }}
      class="hidden opacity-40 hover:opacity-100 focus-visible:opacity-100 text-text-muted hover:text-accent-primary-start transition-all p-1 rounded border-none bg-transparent cursor-pointer flex-shrink-0 sm:block focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-border-focus"
    >
      <span class="material-symbols-outlined text-icon-md">edit_note</span>
    </button>
    {#if item.subtask_total > 0}
      <span
        class="text-type-2xs text-text-muted font-label-sm flex-shrink-0"
        data-testid={`tasks-subtask-badge-${item.id}`}
        title={`${item.subtask_done} of ${item.subtask_total} subtasks done`}
        aria-label={`${item.subtask_done} of ${item.subtask_total} subtasks done`}
        >[{item.subtask_done}/{item.subtask_total}]</span
      >
    {/if}
    {#if item.shortlist_rank != null}
      <span
        class="hidden rounded-full border border-surface-panel-border bg-surface-panel px-1.5 py-0.5 font-mono text-type-3xs text-text-primary md:inline-flex"
        data-testid={`tasks-row-shortlist-${item.id}`}
        title="Shortlist rank {item.shortlist_rank} (0 is the top rank)"
      >
        Shortlist {item.shortlist_rank}
      </span>
    {/if}
    {#if item.priority && item.priority <= 3}
      <span
        class="hidden rounded-full border px-1.5 py-0.5 text-type-3xs font-label-sm uppercase tracking-wide md:inline-flex {priorityClass(
          item.priority
        )}"
      >
        {PRIORITY_LABELS[item.priority] ?? 'Normal'}
      </span>
    {/if}
    {#if taskTags(item).length > 0}
      <span
        class="hidden max-w-24 truncate rounded-full border border-surface-card-border bg-surface-panel px-1.5 py-0.5 text-type-3xs font-label-sm text-text-muted lg:inline-flex"
        title={taskTags(item)[0]}
      >
        #{taskTags(item)[0]}
      </span>
    {/if}
    {#if item.owner}
      <span
        class="hidden max-w-24 truncate text-type-2xs text-accent-secondary-start bg-accent-secondary-glow border border-accent-secondary-start/30 rounded-full px-1.5 py-0.5 sm:inline-flex"
        title={`Owner: ${item.owner}`}>{item.owner}</span
      >
    {/if}
    {#if item.due_date}
      <span
        class="inline-flex items-center gap-1 text-type-2xs {item.status ===
        'DONE'
          ? 'text-text-muted'
          : dueDateTextClass(
              dueDateClass(item.due_date, today)
            )} font-label-sm flex-shrink-0"
        data-testid={`tasks-row-due-${item.id}`}
      >
        <span class="material-symbols-outlined text-icon-xs" aria-hidden="true"
          >schedule</span
        >
        {item.due_date}
      </span>
    {/if}
  </div>
{/snippet}

<div
  bind:this={hostEl}
  class="flex-1 flex flex-col min-h-0 overflow-hidden"
  data-tasks-view
>
  {#if markDownError}
    <ErrorBanner
      message={`Couldn't mark task done: ${markDownError}`}
      dataTestId="tasks-mark-done-error"
      onDismiss={() => {
        markDownError = ''
        if (markDownTimer) {
          clearTimeout(markDownTimer)
          markDownTimer = null
        }
      }}
    />
  {/if}

  {#if orderError}
    <ErrorBanner
      message={`Couldn't reorder task: ${orderError}`}
      dataTestId="tasks-order-error"
      onDismiss={() => {
        orderError = ''
        if (orderErrorTimer) {
          clearTimeout(orderErrorTimer)
          orderErrorTimer = null
        }
      }}
    />
  {/if}

  {#if pinError}
    <ErrorBanner
      message={`Couldn't update pin: ${pinError}`}
      dataTestId="tasks-pin-error"
      onDismiss={() => setPinError('')}
    />
  {/if}

  <!-- aria-live region for manual-order reorder + pin announcements -->
  <div class="sr-only" aria-live="polite" data-testid="tasks-live-message">
    {liveMessage}
  </div>

  <div class="flex flex-1 min-h-0 overflow-hidden">
    <div
      class="flex flex-col flex-1 min-w-0 min-h-0 overflow-hidden"
      style={usePane ? `min-width: ${MIN_LIST_MASTER_WIDTH}px` : undefined}
      data-testid="tasks-list-master"
    >
      <div
        class="flex-1 w-full overflow-y-auto px-3 py-4 space-y-5 custom-scrollbar sm:px-5 lg:px-6"
        data-testid="tasks-list-scroll"
      >
        {#if loading}
          <div
            class="skeleton-container"
            data-testid="tasks-loading"
            aria-busy="true"
            aria-label="Loading tasks"
          >
            {#each Array(4) as _, skelIdx (skelIdx)}
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
          {#if hasActiveListFilters}
            <div
              class="text-text-muted py-10 text-center font-body-md"
              data-testid="tasks-empty-filtered"
            >
              No tasks match the current filters or scope. Clear filters or
              widen the scope to see more.
            </div>
          {:else}
            <div
              class="text-text-muted py-10 text-center font-body-md"
              data-testid="tasks-empty"
            >
              No tasks yet. Type below or use
              <kbd>Ctrl+Shift+N</kbd> to quickly capture one.
            </div>
          {/if}
        {:else}
          {#if filteredOpen.length === 0}
            {#if hasActiveListFilters}
              <div
                class="text-center py-12 px-4 rounded-xl border border-dashed border-surface-panel-border bg-surface-panel/10 max-w-md mx-auto my-8 select-none"
                data-testid="tasks-open-empty-filtered"
              >
                <span
                  class="material-symbols-outlined text-text-muted text-5xl mb-2"
                  aria-hidden="true">filter_list_off</span
                >
                <h3 class="font-headline-md text-text-primary mb-1">
                  No open tasks match
                </h3>
                <p class="text-text-muted text-type-md font-body-md">
                  Nothing open matches the current filters or scope. Clear
                  filters or widen the scope — completed tasks below may still
                  be relevant.
                </p>
              </div>
            {:else}
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
                <p class="text-text-muted text-type-md font-body-md">
                  You have no active tasks. Add one in the box below or use
                  <kbd
                    class="px-1.5 py-0.5 rounded bg-hover text-text-primary border border-surface-panel-border font-mono text-type-xs"
                    >Ctrl+Shift+N</kbd
                  > to capture a new task.
                </p>
              </div>
            {/if}
          {/if}

          {#if pinnedOpen.length > 0}
            <section aria-label="Pinned" data-group="pinned">
              <h2
                class="font-label-sm-bold uppercase tracking-widest text-type-xs mb-2 flex items-center gap-2 text-text-muted"
              >
                Pinned
                <span
                  class="text-text-muted/60"
                  aria-live="polite"
                  data-testid="tasks-group-count"
                >
                  {pinnedOpen.length}
                </span>
                {#if sectionEstimateLabel(pinnedOpen)}
                  <span
                    class="text-text-muted/60 normal-case tracking-normal font-label-sm"
                    data-testid="tasks-group-estimate"
                  >
                    · {sectionEstimateLabel(pinnedOpen)}
                  </span>
                {/if}
              </h2>
              <div
                class="space-y-1 rounded-xl border border-surface-panel-border/60 bg-surface-panel/20 p-1"
              >
                {#each pinnedOpen as item (item.id)}
                  <div animate:flip={{ duration: 200, easing: cubicOut }}>
                    {@render taskRow(item, 'pinned')}
                  </div>
                {/each}
              </div>
            </section>
            {#if unpinnedOpen.length > 0}
              <hr
                data-testid="tasks-pinned-divider"
                class="border-surface-panel-border"
              />
            {/if}
          {/if}

          {#if hubGroupBy === 'dueDate'}
            {#each [{ key: 'overdue', label: 'Overdue', list: overdue, tone: 'error' }, { key: 'today', label: 'Today', list: todayItems, tone: 'primary' }, { key: 'upcoming', label: 'Upcoming', list: upcoming, tone: 'muted' }, { key: 'later', label: 'Later', list: later, tone: 'muted' }, { key: 'undated', label: 'No Date', list: undated, tone: 'muted' }] as group (group.key)}
              {#if group.list.length > 0}
                <section aria-label={group.label} data-group={group.key}>
                  <h2
                    class="font-label-sm-bold uppercase tracking-widest text-type-xs mb-2 flex items-center gap-2"
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
                    {#if sectionEstimateLabel(group.list)}
                      <span
                        class="text-text-muted/60 normal-case tracking-normal font-label-sm"
                        data-testid="tasks-group-estimate"
                      >
                        · {sectionEstimateLabel(group.list)}
                      </span>
                    {/if}
                  </h2>
                  <div
                    class="space-y-1 rounded-xl border border-surface-panel-border/60 bg-surface-panel/20 p-1"
                  >
                    {#each group.list as item (item.id)}
                      <div animate:flip={{ duration: 200, easing: cubicOut }}>
                        {@render taskRow(item, group.key)}
                      </div>
                    {/each}
                  </div>
                </section>
              {/if}
            {/each}
          {:else if hubGroupBy === 'none'}
            {#if unpinnedOpen.length > 0}
              <div
                class="space-y-1 rounded-xl border border-surface-panel-border/60 bg-surface-panel/20 p-1"
                data-group="all"
                aria-label="All Tasks"
              >
                {#each sortTasks( hubSort, unpinnedOpen, { shortlistFirst } ) as item (item.id)}
                  <div animate:flip={{ duration: 200, easing: cubicOut }}>
                    {@render taskRow(item, 'all')}
                  </div>
                {/each}
              </div>
            {/if}
          {:else}
            {#each groupedSections as group (group.key)}
              <section aria-label={group.label} data-group={group.key}>
                <h2
                  class="font-label-sm-bold uppercase tracking-widest text-type-xs mb-2 flex items-center gap-2 text-text-muted"
                >
                  <button
                    type="button"
                    onclick={() => toggleSection(group.key)}
                    aria-expanded={!collapsedSections.has(group.key)}
                    aria-controls={`tasks-group-${group.key}`}
                    class="flex items-center gap-2 bg-transparent border-none p-0 cursor-pointer uppercase tracking-widest text-type-xs font-label-sm-bold text-text-muted hover:text-text-primary"
                    data-testid={`tasks-group-toggle-${group.key}`}
                  >
                    {#if collapsedSections.has(group.key)}
                      <span class="material-symbols-outlined text-icon-sm"
                        >chevron_right</span
                      >
                    {:else}
                      <span class="material-symbols-outlined text-icon-sm"
                        >expand_more</span
                      >
                    {/if}
                    {group.label}
                    <span class="text-text-muted/60" aria-hidden="true"
                      >{group.items.length}</span
                    >
                    {#if sectionEstimateLabel(group.items)}
                      <span
                        class="text-text-muted/60 normal-case tracking-normal font-label-sm"
                        data-testid="tasks-group-estimate"
                        aria-hidden="true"
                      >
                        · {sectionEstimateLabel(group.items)}
                      </span>
                    {/if}
                  </button>
                </h2>
                {#if !collapsedSections.has(group.key)}
                  <div
                    id={`tasks-group-${group.key}`}
                    class="space-y-1 rounded-xl border border-surface-panel-border/60 bg-surface-panel/20 p-1"
                  >
                    {#each group.items as item (item.id)}
                      <div animate:flip={{ duration: 200, easing: cubicOut }}>
                        {@render taskRow(item, group.key)}
                      </div>
                    {/each}
                  </div>
                {/if}
              </section>
            {/each}
          {/if}

          {#if filteredDone.length > 0}
            <section aria-label="Completed" data-group="completed">
              <h2
                class="font-label-sm-bold uppercase tracking-widest text-type-xs mb-2 flex items-center gap-2 text-text-muted"
              >
                <button
                  type="button"
                  onclick={() => (showCompleted = !showCompleted)}
                  aria-expanded={showCompleted}
                  aria-controls="tasks-completed-list"
                  class="flex items-center gap-2 bg-transparent border-none p-0 cursor-pointer uppercase tracking-widest text-type-xs font-label-sm-bold text-text-muted hover:text-text-primary"
                  data-testid="tasks-completed-toggle"
                >
                  {#if showCompleted}
                    <span class="material-symbols-outlined text-icon-sm"
                      >expand_more</span
                    >
                  {:else}
                    <span class="material-symbols-outlined text-icon-sm"
                      >chevron_right</span
                    >
                  {/if}
                  Completed
                  <span class="text-text-muted/60" aria-hidden="true"
                    >{filteredDone.length}</span
                  >
                </button>
              </h2>
              {#if showCompleted}
                <div
                  id="tasks-completed-list"
                  class="space-y-1 opacity-60"
                  data-testid="tasks-completed-list"
                >
                  {#each filteredDone as item (item.id)}
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
                        {#if item.notebook !== STANDALONE_TASKS_NOTEBOOK}
                          <div
                            class="text-type-2xs text-text-muted uppercase tracking-widest font-label-sm"
                          >
                            {item.notebook} › {item.section} › {item.page}
                          </div>
                        {/if}
                      </div>
                      <span
                        class="text-type-2xs text-text-muted font-label-sm flex-shrink-0"
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
              class="text-text-muted text-type-sm font-body-md border-t border-surface-panel-border pt-3 mt-6"
              role="status"
              aria-live="polite"
              data-testid="tasks-truncated-notice"
            >
              Showing the first
              {filteredOpen.length + filteredDone.length}
              tasks — there are more below the display limit. Complete or reschedule
              some to surface them.
            </p>
          {/if}
        {/if}
      </div>
    </div>

    {#if inspectorReady && usePane}
      <SidebarResizeHandle
        edge="end"
        width={effectivePaneWidth}
        min={MIN_INSPECTOR_PANE_WIDTH}
        max={paneMaxForHost}
        defaultWidth={defaultPaneWidthForHost}
        onWidthChange={setPaneWidth}
        onWidthCommit={commitPaneWidth}
        ariaLabel="Resize task inspector"
      />
    {/if}
    <!-- Single TaskEditDrawer instance: pane is in-flow flex child; overlay
         uses position:fixed so it leaves the flex flow (#919). -->
    {#if inspectorReady}
      <div
        class={usePane ? 'flex-shrink-0 h-full overflow-hidden' : 'contents'}
        style={usePane ? `width: ${effectivePaneWidth}px` : undefined}
        data-testid={usePane ? 'tasks-inspector-pane' : undefined}
      >
        <TaskEditDrawer
          variant={usePane ? 'pane' : 'overlay'}
          task={selectedTask}
          filteredOut={selectedFilteredOut}
          {ctx}
          onMetaChanged={reload}
          onOpenSubEditor={() => selectedTask && (subEditorTask = selectedTask)}
          onClose={() => {
            selectedTask = null
            selectedFilteredOut = false
          }}
          onPrevTask={goPrevTask}
          onNextTask={goNextTask}
          {hasPrevTask}
          {hasNextTask}
          bind:busy={inspectorBusy}
        />
      </div>
    {/if}
  </div>

  <div
    class="tasks-quick-add-bar flex-shrink-0 border-t border-surface-panel-border px-3 py-2.5 sm:px-5 lg:px-6"
    data-testid="tasks-inline-quickadd"
  >
    <div class="w-full" data-testid="tasks-inline-quickadd-inner">
      <QuickAddTask
        {ctx}
        placeholder="Add a task — Enter to add"
        keepOpenAfterCreate={true}
        autofocus={false}
        clearOnEscape={true}
      />
    </div>
  </div>
</div>
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
<BlockedDoneGuard
  pending={blockedGuard.pending}
  cardText={blockedGuard.pending?.context.clean_content}
  onConfirm={confirmBlockedDone}
  onCancel={cancelBlockedDone}
/>

<style>
  /* Persistent inspector selection (distinct from deep-link flash). */
  .tasks-selected {
    background: color-mix(
      in srgb,
      var(--color-accent-primary-glow) 100%,
      transparent
    );
    box-shadow: 0 0 0 1px var(--color-accent-primary-start) inset;
  }
  /* Temporary deep-link / focus-jump flash. */
  .tasks-focused {
    background: color-mix(
      in srgb,
      var(--color-accent-primary-glow) 70%,
      transparent
    );
    box-shadow: 0 0 0 1px var(--color-accent-secondary-start) inset;
    transition:
      background 600ms ease-out,
      box-shadow 600ms ease-out;
  }
  .tasks-selected.tasks-focused {
    background: color-mix(
      in srgb,
      var(--color-accent-primary-glow) 100%,
      transparent
    );
    box-shadow: 0 0 0 1px var(--color-accent-primary-start) inset;
  }
  .tasks-list-row::before {
    content: '';
    position: absolute;
    inset: 0 auto 0 0;
    width: 2px;
    background: var(--color-text-muted);
    opacity: 0.25;
  }
  .tasks-list-row[data-status='DOING']::before {
    background: var(--color-accent-secondary-start);
    opacity: 1;
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
  .tasks-drag-over {
    border-color: var(--color-accent-primary-start);
    box-shadow: inset 0 2px 0 var(--color-accent-primary-start);
    background: var(--color-accent-primary-glow);
    transform: translateY(1px);
  }
  .tasks-dragging {
    opacity: 0.4;
  }
  .tasks-quick-add-bar {
    background: color-mix(in srgb, var(--color-surface-panel) 90%, transparent);
    backdrop-filter: blur(12px);
  }
  @media (prefers-reduced-transparency: reduce) {
    .tasks-quick-add-bar {
      background: var(--color-surface-panel);
      backdrop-filter: none;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .tasks-list-row {
      transition: none;
    }
  }
</style>
