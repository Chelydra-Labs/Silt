<script lang="ts">
  // Board display mode of the Tasks hub (#421). Lifts the proven
  // silt-kanban Board (DnD, keyboard parity, DONE guard, per-column
  // quick-add, column management, card richness) and generalizes it to any
  // grouping dimension via the shared grouping engine (#423).
  //
  // The hub owns the chrome (title/count header, mode switch, FilterBar,
  // scope breadcrumb, group-by/sort selectors); BoardView consumes
  // scope/filters/groupBy/sort from getTaskHubState() and renders only the
  // columns strip + its own error/notice banners + modals/drawer.
  //
  // Dimension-aware DnD: each active groupBy routes a drop to the SDK setter
  // that owns the binned dimension. ctx.updateTaskMeta only covers
  // {pinned, progress}, so a drop dispatches to updateBlockState /
  // setTaskOwner / setTaskPriority / setTaskDueDate / setTaskTags depending
  // on groupBy. Notebook/section/page are file-location dimensions, not
  // mutable metadata — DnD is disabled for them (cards render read-only).
  import { flip } from 'svelte/animate'
  import { cubicOut } from 'svelte/easing'
  import { onMount, onDestroy } from 'svelte'
  import type { PluginContext, TaskStatus } from '../../../sdk'
  import { plusDaysISO } from '../../../sdk'
  import TaskEditDrawer from '../components/TaskEditDrawer.svelte'
  import TaskSubEditorModal from '../components/TaskSubEditorModal.svelte'
  import BlockedDoneDialog from '../components/BlockedDoneDialog.svelte'
  import ConfirmModal from '../components/ConfirmModal.svelte'
  import QuickAddTask from '../components/QuickAddTask.svelte'
  import {
    formatEstimateSum,
    PRIORITY_LABELS,
    laneLabel,
    priorityClass,
    type TaskDetail
  } from '../types'
  import { dueDateClass, dueDateTextClass } from '../dueDate'
  import ErrorBanner from '../components/ErrorBanner.svelte'
  import {
    cloneColumns,
    columnNames,
    type BoardColumn as StatusColumn
  } from '../columns'
  import {
    getTaskHubQueryContext,
    getTaskHubViewState,
    setColumns,
    setDisplayMode,
    type GroupBy
  } from '../state.svelte'
  import { binByDimension } from '../grouping'
  import { buildQuery } from '../query'
  import { loadColumns, persistColumns } from '../settings'

  interface Props {
    ctx: PluginContext
    /** Hub subscribes to keep its header count in sync. */
    onCountChange?: (open: number, done: number) => void
  }

  let { ctx, onCountChange }: Props = $props()

  const ALL_STATUSES: TaskStatus[] = ['TODO', 'DOING', 'DONE']
  // Dimensions whose value is mutable metadata a drop can rewrite. Location
  // dimensions (notebook/section/page) and 'none' are excluded — a task's
  // file location is not metadata and can't be changed by dragging.
  const DND_DIMENSIONS: ReadonlySet<GroupBy> = new Set([
    'status',
    'owner',
    'priority',
    'dueDate',
    'tag'
  ])

  // A rendered Board lane. `value` is the dimension value a drop/quick-add
  // dispatches with — '' routes to the dimension's "clear" path (Unassigned
  // owner, No Date due date, No Tag no-op). Named Lane (not BoardColumn) so
  // it doesn't collide with the persisted StatusColumn model in columns.ts.
  interface Lane {
    key: string
    label: string
    value: string
    items: TaskDetail[]
  }

  // Status columns are the ONLY user-managed columns (configured + persisted,
  // including soft WIP limits #437). Every other dimension derives its
  // columns from the loaded data.
  let statusColumns = $state<StatusColumn[]>(loadColumns())
  let rows = $state<TaskDetail[]>([])
  let columns = $state<Lane[]>([])
  let loading = $state(true)
  let errorMsg = $state('')
  let moveError = $state('')
  let configError = $state('')

  let selectedCard = $state<TaskDetail | null>(null)
  let subEditorCard = $state<TaskDetail | null>(null)
  let pendingBlockedDone = $state<{
    card: TaskDetail
    fromColKey: string
    toCol: Lane
    blockers: { id: string; clean_content?: string }[]
  } | null>(null)
  // Soft WIP-limit confirm (#437): shown when a drop/keyboard move would
  // push a status column over its configured cap. Cancel snaps back;
  // Confirm proceeds with the status change.
  let pendingWipConfirm = $state<{
    card: TaskDetail
    fromColKey: string
    toCol: Lane
  } | null>(null)
  // Soft WIP confirm for per-column quick-add (create path, not drag).
  let pendingQuickAddWip = $state<{
    resolve: (ok: boolean) => void
  } | null>(null)

  // --- Hub state (reactive reads) -----------------------------------------
  let groupBy = $derived(getTaskHubViewState().groupBy)
  let sort = $derived(getTaskHubViewState().sort)
  let scope = $derived(getTaskHubViewState().scope)
  let filters = $derived(getTaskHubViewState().filters)
  let today = $derived(ctx.today)
  let dndEnabled = $derived(DND_DIMENSIONS.has(groupBy))

  // --- DnD state -----------------------------------------------------------
  let dragCard: TaskDetail | null = null
  let dragFromColKey = ''
  let draggingId = $state<string | null>(null)
  // Within-column manual reorder (#426): the card the pointer is hovering
  // on; null when not over a valid same-column drop target.
  let dragOverCardId = $state<string | null>(null)
  let liveMessage = $state('')

  // --- Column-management UI state (status dimension only) -----------------
  let menuCol = $state<string | null>(null)
  let renamingColKey = $state<string | null>(null)
  let renameValue = $state('')
  let quickAddCol = $state<string | null>(null)
  let colDragIndex = $state<number | null>(null)

  // --- High-cardinality guard ---------------------------------------------
  // >10 columns → non-blocking hint; >20 → refuse to render + fall back to
  // List mode so a 200-notebook vault doesn't freeze the board layout.
  let tooManyColumns = $state(false)

  function dueDateAnchor(bucketKey: string, iso: string): string {
    // Deterministic anchor per bucket so a drop produces a stable due date
    // the next query will re-bin into the same column. Overdue → today
    // nudges an already-late task to the local day; Later → today+30 picks
    // a far-but-finite horizon rather than leaving the task undated.
    switch (bucketKey) {
      case 'overdue':
      case 'today':
        return iso
      case 'upcoming':
        return plusDaysISO(iso, 3)
      case 'later':
        return plusDaysISO(iso, 30)
      case 'undated':
      default:
        return ''
    }
  }

  function unionTags(card: TaskDetail, tag: string): string[] {
    const existing = (card.tags ?? '')
      .split('|')
      .map((t) => t.trim())
      .filter(Boolean)
    // Multi-membership: a task can appear in several tag columns. Dropping
    // into a tag column ADDS that tag without removing the others.
    return existing.includes(tag) ? existing : [...existing, tag]
  }

  // The dimension-aware drop dispatcher. Each groupBy routes to the SDK
  // setter that actually owns the binned field on disk.
  function dispatchDrop(card: TaskDetail, toCol: Lane): Promise<unknown> {
    switch (groupBy) {
      case 'status':
        return ctx.updateBlockState(card.id, toCol.value as TaskStatus)
      case 'owner':
        // '' clears the owner (the Unassigned column).
        return ctx.setTaskOwner(card.id, toCol.value)
      case 'priority':
        return ctx.setTaskPriority(card.id, Number(toCol.value))
      case 'dueDate':
        return ctx.setTaskDueDate(card.id, dueDateAnchor(toCol.value, today))
      case 'tag':
        // No Tag column (value='') is a no-op — we don't strip tags on drop.
        if (!toCol.value) return Promise.resolve(true)
        return ctx.setTaskTags(card.id, unionTags(card, toCol.value))
      default:
        return Promise.resolve(true)
    }
  }

  function announceMove(toCol: Lane): string {
    switch (groupBy) {
      case 'status':
        return `Task moved to ${toCol.label}`
      case 'owner':
        return `Task reassigned to ${toCol.label}`
      case 'priority':
        return `Task priority set to ${toCol.label}`
      case 'dueDate':
        return `Task due date set to ${toCol.label}`
      case 'tag':
        return toCol.value ? `Tag ${toCol.label} added` : 'No change'
      default:
        return 'Task moved'
    }
  }

  function snapshotColumns(): Lane[] {
    return columns.map((c) => ({ ...c, items: [...c.items] }))
  }

  // Apply the optimistic dimension move to the rendered columns. Tag is
  // multi-membership (card joins the target without leaving the source); the
  // single-membership dimensions remove the card from the source column.
  function applyOptimistic(card: TaskDetail, fromColKey: string, toCol: Lane) {
    const multiMembership = groupBy === 'tag'
    // For tag drops, the card's tags field must be updated optimistically
    // so a rapid successive drop reads the fresh tag set (not the stale one
    // from before the first drop's block:changed reload). unionTags is the
    // same helper dispatchDrop feeds to setTaskTags, so the optimistic
    // value matches what the persist will land.
    const tagUpdate =
      multiMembership && toCol.value
        ? { tags: unionTags(card, toCol.value).join('|') }
        : {}
    columns = columns.map((c) => {
      if (c.key === toCol.key) {
        if (c.items.some((i) => i.id === card.id)) return c
        return { ...c, items: [...c.items, { ...card, ...tagUpdate }] }
      }
      if (!multiMembership && c.key === fromColKey) {
        return { ...c, items: c.items.filter((i) => i.id !== card.id) }
      }
      // For tag DnD, the card stays in its other columns too — refresh its
      // tags there as well so a later read sees the union.
      if (multiMembership && c.items.some((i) => i.id === card.id)) {
        // A tagless card that just gained a tag must leave the No Tag bucket
        // optimistically, or it appears in two columns until the reload.
        if (!c.value && tagUpdate.tags) {
          return { ...c, items: c.items.filter((i) => i.id !== card.id) }
        }
        return {
          ...c,
          items: c.items.map((i) =>
            i.id === card.id ? { ...i, ...tagUpdate } : i
          )
        }
      }
      return c
    })
  }

  function revertTo(prev: Lane[]) {
    columns = prev
  }

  // Restore the card to its source column (used by the DONE-guard cancel
  // path and the confirm-failure path).
  function revertOptimistic(card: TaskDetail, fromColKey: string, toCol: Lane) {
    columns = columns.map((c) => {
      if (c.key === toCol.key) {
        return { ...c, items: c.items.filter((i) => i.id !== card.id) }
      }
      if (c.key === fromColKey) {
        if (c.items.some((i) => i.id === card.id)) return c
        return { ...c, items: [...c.items, { ...card }] }
      }
      return c
    })
  }

  // Monotonic token so a failed earlier move can't revert over a later
  // optimistic move (mirrors Kanban's moveSeq).
  let moveSeq = 0
  // In-flight setTaskOrders IPC; a newer within-column reorder awaits it so
  // the newer write lands after the older one's on disk. IPC arrival order
  // across near-simultaneous calls isn't FIFO, and the Go side is last-
  // writer-wins — without serialization a stale older orderByID resolving
  // later would clobber the user's newest intent. See commitManualReorder.
  let reorderInFlight: Promise<void> | null = null
  /** Soft WIP limit for a status lane, or null when unlimited. */
  function wipLimitFor(statusName: string): number | null {
    const col = statusColumns.find((c) => c.name === statusName)
    const lim = col?.wipLimit
    return lim != null && lim >= 1 ? lim : null
  }

  /**
   * True when moving `card` into `toCol` would push that status column over
   * its soft WIP limit (#437). Same-column / non-status moves never trip it.
   */
  function wouldExceedWip(
    card: TaskDetail,
    fromColKey: string,
    toCol: Lane
  ): boolean {
    if (groupBy !== 'status') return false
    if (toCol.key === fromColKey) return false
    const limit = wipLimitFor(toCol.value)
    if (limit == null) return false
    // Already counted in the target (shouldn't happen for status) → no bump.
    if (toCol.items.some((i) => i.id === card.id)) return false
    return toCol.items.length + 1 > limit
  }

  async function commitDrop(card: TaskDetail, fromColKey: string, toCol: Lane) {
    if (!dndEnabled) return
    if (groupBy !== 'tag' && toCol.key === fromColKey) return
    if (groupBy === 'tag' && !toCol.value) {
      liveMessage =
        "No Tag column doesn't remove tags — drop on a tag column to add."
      return
    }

    const my = ++moveSeq
    moveError = ''
    const prev = snapshotColumns()

    // Soft WIP-limit guard (#437, status dimension only): pause before the
    // persist so the user can confirm or cancel. Optimistic placement runs
    // first so Cancel can snap the card back (mirrors the DONE-blocked flow).
    // Skip WIP when blocked-DONE will also fire — one modal is enough; the
    // blocked-DONE dialog already implies the move proceeds if confirmed.
    const skipWipForBlockedDone =
      groupBy === 'status' && toCol.value === 'DONE' && !!card.is_blocked
    if (
      wouldExceedWip(card, fromColKey, toCol) &&
      !pendingWipConfirm &&
      !pendingBlockedDone &&
      !skipWipForBlockedDone
    ) {
      applyOptimistic(card, fromColKey, toCol)
      pendingWipConfirm = { card, fromColKey, toCol }
      return
    }

    // DONE-on-blocked guard (#302, status dimension only): pause before the
    // persist so the user can confirm or cancel. applyOptimistic runs AFTER
    // the getTaskBlockers await + moveSeq check so a second drop that bumps
    // moveSeq during the await leaves nothing stranded in DONE (was: card
    // stuck in DONE with no revert, no persist, no dialog).
    if (
      groupBy === 'status' &&
      toCol.value === 'DONE' &&
      card.is_blocked &&
      !pendingBlockedDone
    ) {
      try {
        const blockers = await ctx.getTaskBlockers(card.id)
        // A second drop during the await bumped moveSeq; abandon this
        // commit so a stale confirm dialog can't land over a newer move.
        // Nothing is stranded — applyOptimistic hasn't run yet.
        if (my !== moveSeq) return
        if (blockers.length > 0) {
          applyOptimistic(card, fromColKey, toCol)
          pendingBlockedDone = {
            card,
            fromColKey,
            toCol,
            blockers: blockers.map((b) => ({
              id: b.id,
              clean_content: b.clean_content
            }))
          }
          return
        }
      } catch {
        // Blocker lookup failed — proceed with the persist below rather than
        // stranding the card in an un-committed optimistic state.
      }
    }
    applyOptimistic(card, fromColKey, toCol)

    liveMessage = announceMove(toCol)
    try {
      await dispatchDrop(card, toCol)
      // Cross-column manual sort (#426): the moved card joins after the
      // destination's highest existing order. Source columns aren't
      // renumbered on removal (gap-tolerant), so destination orders can be
      // non-contiguous (e.g. [1, 5]) — count-based destLen+1 would land the
      // card mid-column rather than at the tail. Within-column reorder is
      // owned by commitManualReorder via the card-level drop handler.
      if (sort === 'manual') {
        const destItems = prev.find((c) => c.key === toCol.key)?.items ?? []
        const maxOrder =
          destItems.length > 0
            ? Math.max(...destItems.map((c) => c.manual_order ?? 0))
            : 0
        try {
          await ctx.setTaskOrder(card.id, maxOrder + 1)
        } catch (e) {
          if (my !== moveSeq) return
          moveError = e instanceof Error ? e.message : String(e)
          // The dimension change in dispatchDrop already persisted; reverting
          // the optimistic column placement would desync from disk. Reload to
          // pick up the on-disk state (new column, stale order value).
          await reload()
          liveMessage = 'Move partially failed — reloaded.'
        }
      }
    } catch (e) {
      if (my !== moveSeq) return
      moveError = e instanceof Error ? e.message : String(e)
      revertTo(prev)
      liveMessage = 'Move failed — reverted.'
    }
  }

  async function confirmBlockedDone() {
    const pending = pendingBlockedDone
    if (!pending) return
    pendingBlockedDone = null
    try {
      await ctx.updateBlockState(pending.card.id, 'DONE')
      // Cross-column manual sort (#426): mirror commitDrop. The DONE jump is
      // a cross-column move under manual sort, so the card needs an order
      // token beyond the destination's tail. prev (snapshot at drag-start) is
      // not in scope here; read the current destination column instead.
      if (sort === 'manual') {
        const destItems =
          columns.find((c) => c.key === pending.toCol.key)?.items ?? []
        const maxOrder =
          destItems.length > 0
            ? Math.max(...destItems.map((c) => c.manual_order ?? 0))
            : 0
        try {
          await ctx.setTaskOrder(pending.card.id, maxOrder + 1)
        } catch (e) {
          moveError = e instanceof Error ? e.message : String(e)
          // The status change already persisted; reload picks up on-disk state
          // (new column, stale order value) rather than desyncing.
          await reload()
          liveMessage = 'Move partially failed — reloaded.'
          return
        }
      }
      liveMessage = 'Task completed despite open prerequisites.'
    } catch (e) {
      moveError = e instanceof Error ? e.message : String(e)
      revertOptimistic(pending.card, pending.fromColKey, pending.toCol)
      liveMessage = 'Move failed — reverted.'
    }
  }

  function cancelBlockedDone() {
    const pending = pendingBlockedDone
    if (!pending) return
    pendingBlockedDone = null
    revertOptimistic(pending.card, pending.fromColKey, pending.toCol)
    liveMessage = 'Move cancelled.'
  }

  function focusCard(cardId: string) {
    document.querySelector<HTMLElement>(`[data-card="${cardId}"]`)?.focus?.()
  }

  async function confirmWipOverLimit() {
    const pending = pendingWipConfirm
    if (!pending) return
    pendingWipConfirm = null
    const { card, fromColKey, toCol } = pending
    // Card is already optimistically in the target. Hand off to the
    // blocked-DONE guard when applicable; otherwise persist the drop.
    if (groupBy === 'status' && toCol.value === 'DONE' && card.is_blocked) {
      try {
        const blockers = await ctx.getTaskBlockers(card.id)
        if (blockers.length > 0) {
          pendingBlockedDone = {
            card,
            fromColKey,
            toCol,
            blockers: blockers.map((b) => ({
              id: b.id,
              clean_content: b.clean_content
            }))
          }
          focusCard(card.id)
          return
        }
      } catch {
        // Proceed with persist below.
      }
    }
    liveMessage = announceMove(toCol)
    try {
      await dispatchDrop(card, toCol)
      if (sort === 'manual') {
        // Destination already includes the optimistic card; exclude it when
        // computing the prior max order so we don't count the card itself.
        const destItems =
          columns
            .find((c) => c.key === toCol.key)
            ?.items.filter((i) => i.id !== card.id) ?? []
        const maxOrder =
          destItems.length > 0
            ? Math.max(...destItems.map((c) => c.manual_order ?? 0))
            : 0
        try {
          await ctx.setTaskOrder(card.id, maxOrder + 1)
        } catch (e) {
          moveError = e instanceof Error ? e.message : String(e)
          await reload()
          liveMessage = 'Move partially failed — reloaded.'
          focusCard(card.id)
          return
        }
      }
    } catch (e) {
      moveError = e instanceof Error ? e.message : String(e)
      revertOptimistic(card, fromColKey, toCol)
      liveMessage = 'Move failed — reverted.'
    }
    focusCard(card.id)
  }

  function cancelWipOverLimit() {
    const pending = pendingWipConfirm
    if (!pending) return
    pendingWipConfirm = null
    revertOptimistic(pending.card, pending.fromColKey, pending.toCol)
    liveMessage = 'Move cancelled — column is over its WIP limit.'
    focusCard(pending.card.id)
  }

  // --- Column derivation --------------------------------------------------
  // Status: configured columns (user-managed). Priority: P1/P2/P3 only
  // (legacy priority-0 rows join P3 but keep their value). Every other
  // dimension: data-driven via the shared grouping engine. Owner reorders
  // so the Unassigned bucket sits far-left per the issue spec.
  function deriveColumns(
    loaded: TaskDetail[],
    g: GroupBy,
    configuredStatuses: string[],
    iso: string
  ): Lane[] {
    if (g === 'status') {
      const byStatus = new Map<string, TaskDetail[]>()
      for (const r of loaded) {
        const s = r.status || 'TODO'
        if (!byStatus.has(s)) byStatus.set(s, [])
        byStatus.get(s)!.push(r)
      }
      return configuredStatuses.map((s) => ({
        key: `status-${s}`,
        label: laneLabel(s),
        value: s,
        items: byStatus.get(s) ?? []
      }))
    }
    if (g === 'priority') {
      const buckets: Record<'1' | '2' | '3', TaskDetail[]> = {
        '1': [],
        '2': [],
        '3': []
      }
      for (const r of loaded) {
        const p = r.priority && r.priority > 0 ? r.priority : 3
        const key = p <= 1 ? '1' : p === 2 ? '2' : '3'
        buckets[key].push(r)
      }
      return (['1', '2', '3'] as const).map((p) => ({
        key: `priority-${p}`,
        label: PRIORITY_LABELS[Number(p)] ?? `Priority ${p}`,
        value: p,
        items: buckets[p]
      }))
    }
    const sections = binByDimension(loaded, g, { today: iso })
    if (g === 'dueDate') {
      // value carries the bucket KEY (e.g. 'today') so dueDateAnchor can
      // switch on it — the label ('Today') isn't a stable dispatch handle.
      return sections.map((s) => ({
        key: s.key,
        label: s.label,
        value: s.key,
        items: s.items
      }))
    }
    if (g === 'owner') {
      // Unassigned bucket moves to the far left (matches the issue spec).
      const unassigned = sections.find((s) => s.key.endsWith('__unassigned__'))
      const rest = sections.filter((s) => s !== unassigned)
      const ordered = unassigned ? [unassigned, ...rest] : rest
      return ordered.map((s) => ({
        key: s.key,
        label: s.label,
        value: s.key.endsWith('__unassigned__') ? '' : s.label,
        items: s.items
      }))
    }
    return sections.map((s) => ({
      key: s.key,
      label: s.label,
      value: s.key.endsWith('__unassigned__') ? '' : s.label,
      items: s.items
    }))
  }

  function columnEstimateSum(items: TaskDetail[]): number {
    return items.reduce((acc, c) => acc + (c.estimate_minutes ?? 0), 0)
  }

  function rebin() {
    columns = deriveColumns(rows, groupBy, columnNames(statusColumns), today)
  }

  /** Persist status columns + mirror into hub state (saved-view dirty flag). */
  function saveStatusColumns(next: StatusColumn[], prev: StatusColumn[]) {
    statusColumns = next
    setColumns(next)
    void persistColumns(next).then((ok) => {
      if (!ok) {
        configError = 'Failed to save columns'
        statusColumns = prev
        setColumns(prev)
        rebin()
      }
    })
    rebin()
  }

  // Monotonic token so concurrent reload() calls can identify their own
  // response vs a later one (rapid scope/groupBy switches).
  let loadSeq = 0
  async function reload() {
    const my = ++loadSeq
    loading = true
    errorMsg = ''
    try {
      const { sql, params } = buildQuery(
        scope,
        filters,
        getTaskHubQueryContext({
          activeNotebook: ctx.activeNotebook,
          activeSection: ctx.activeSection,
          activePage: ctx.activePage,
          today
        }),
        { groupBy, sort, activeFilter: getTaskHubViewState().activeFilter }
      )
      const { rows: raw } = await ctx.sqliteQuery(sql, params)
      if (my !== loadSeq) return
      rows = (raw as unknown as TaskDetail[]).map((r) => ({
        ...r,
        pinned: !!r.pinned,
        created_at: r.created_at ?? '',
        completed_at: r.completed_at ?? '',
        manual_order: r.manual_order ?? 0,
        modified_at: r.modified_at ?? '',
        estimate_minutes:
          r.estimate_minutes == null ? null : Number(r.estimate_minutes),
        subtask_total: r.subtask_total ?? 0,
        subtask_done: r.subtask_done ?? 0
      }))
      rebin()
      // Keep the open drawer in sync with fresh data; if the task left the
      // result set, keep the last-known snapshot rather than snapping closed.
      if (selectedCard) {
        const fresh = rows.find((r) => r.id === selectedCard!.id)
        if (fresh) selectedCard = fresh
      }
    } catch (e) {
      if (my !== loadSeq) return
      errorMsg = e instanceof Error ? e.message : String(e)
    } finally {
      if (my === loadSeq) loading = false
    }
  }

  onMount(() => {
    void reload()
  })

  // Reload whenever any reactive input the query depends on changes.
  $effect(() => {
    void scope
    void groupBy
    void sort
    void getTaskHubViewState().activeFilter
    void today
    void ctx.activeNotebook
    void ctx.activeSection
    void ctx.activePage
    void filters.owners
    void filters.priorities
    void filters.dueDate
    void filters.tags
    void filters.stale
    void reload()
  })

  // When the groupBy dimension changes, re-derive columns even before the
  // re-query lands so the header strip flips dimension immediately.
  $effect(() => {
    void groupBy
    void statusColumns
    rebin()
  })

  // Repaint on any block mutation (created/mutated/rescheduled from any
  // surface). Debounced so a burst of block:changed events triggers one
  // reload.
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

  // High-cardinality guard: >20 columns refuses to render and falls back to
  // List mode (auto-switches the hub). Fires once per overload.
  $effect(() => {
    const count = columns.length
    if (count > 20 && !tooManyColumns) {
      tooManyColumns = true
      setDisplayMode('list')
      ctx
        .notify({
          title: 'Tasks',
          body: `Too many ${groupBy} values for Board view — switched to List.`
        })
        .catch(() => {})
    }
    if (count <= 20 && tooManyColumns) {
      tooManyColumns = false
    }
  })

  // Report counts upward so the hub header stays in sync.
  $effect(() => {
    const open = rows.filter((r) => r.status !== 'DONE').length
    const done = rows.filter((r) => r.status === 'DONE').length
    onCountChange?.(open, done)
  })

  // --- Column management (status dimension only) --------------------------
  function toggleColMenu(colKey: string) {
    const next = menuCol === colKey ? null : colKey
    menuCol = next
    if (!next || next !== colKey) {
      wipEditCol = null
      wipEditError = ''
    }
  }
  function startRename(statusName: string, colKey: string) {
    renamingColKey = colKey
    renameValue = statusName
    menuCol = null
  }
  function commitRename(oldStatus: string) {
    const v = renameValue.trim()
    renamingColKey = null
    const names = columnNames(statusColumns)
    if (!v || v === oldStatus || names.includes(v)) return
    const prev = cloneColumns(statusColumns)
    const next = statusColumns.map((c) =>
      c.name === oldStatus ? { ...c, name: v } : c
    )
    configError = ''
    saveStatusColumns(next, prev)
  }
  function cancelRename() {
    renamingColKey = null
  }
  async function addColumn() {
    const name = window.prompt('New column name')?.trim()
    if (!name || columnNames(statusColumns).includes(name)) return
    const prev = cloneColumns(statusColumns)
    configError = ''
    saveStatusColumns([...statusColumns, { name }], prev)
  }
  async function removeColumn(statusName: string) {
    menuCol = null
    if (
      !window.confirm(
        `Remove column "${laneLabel(statusName)}"? Cards keep their status.`
      )
    )
      return
    const prev = cloneColumns(statusColumns)
    configError = ''
    saveStatusColumns(
      statusColumns.filter((c) => c.name !== statusName),
      prev
    )
  }
  // Inline WIP editor in the column menu (replaces window.prompt for a11y).
  let wipEditCol = $state<string | null>(null)
  let wipDraft = $state('')
  let wipEditError = $state('')

  function startWipEdit(statusName: string) {
    const current = statusColumns.find((c) => c.name === statusName)
    wipEditCol = statusName
    wipEditError = ''
    wipDraft =
      current?.wipLimit != null && current.wipLimit >= 1
        ? String(current.wipLimit)
        : ''
  }

  function applyWipLimit(statusName: string) {
    const trimmed = wipDraft.trim()
    let wipLimit: number | null
    if (trimmed === '') {
      wipLimit = null
    } else {
      const n = Number(trimmed)
      if (!Number.isFinite(n) || n < 1) {
        wipEditError = 'Enter a whole number ≥ 1, or leave empty for unlimited'
        return
      }
      wipLimit = Math.floor(n)
    }
    const prev = cloneColumns(statusColumns)
    const next = statusColumns.map((c) => {
      if (c.name !== statusName) return c
      if (wipLimit == null) {
        const { wipLimit: _drop, ...rest } = c
        void _drop
        return rest
      }
      return { ...c, wipLimit }
    })
    configError = ''
    wipEditCol = null
    wipEditError = ''
    menuCol = null
    saveStatusColumns(next, prev)
  }

  function clearWipLimit(statusName: string) {
    const prev = cloneColumns(statusColumns)
    const next = statusColumns.map((c) => {
      if (c.name !== statusName) return c
      const { wipLimit: _drop, ...rest } = c
      void _drop
      return rest
    })
    configError = ''
    wipEditCol = null
    wipEditError = ''
    menuCol = null
    saveStatusColumns(next, prev)
  }
  function onColDragStart(e: DragEvent, i: number) {
    colDragIndex = i
    if (e.dataTransfer) {
      e.dataTransfer.effectAllowed = 'move'
      e.dataTransfer.setData('text/plain', `col:${i}`)
    }
  }
  function onColDragOver(e: DragEvent, i: number) {
    if (colDragIndex === null) return
    e.preventDefault()
    if (e.dataTransfer) e.dataTransfer.dropEffect = 'move'
  }
  function onColDrop(e: DragEvent, i: number) {
    if (colDragIndex === null) return
    e.preventDefault()
    const from = colDragIndex
    colDragIndex = null
    if (from === i) return
    const prev = cloneColumns(statusColumns)
    const next = [...statusColumns]
    const [moved] = next.splice(from, 1)
    next.splice(i, 0, moved)
    configError = ''
    saveStatusColumns(next, prev)
  }

  // --- Card DnD -----------------------------------------------------------
  function onDragStart(e: DragEvent, card: TaskDetail, colKey: string) {
    if (!dndEnabled) return
    dragCard = card
    dragFromColKey = colKey
    draggingId = card.id
    colDragIndex = null
    if (e.dataTransfer) {
      e.dataTransfer.effectAllowed = 'move'
      e.dataTransfer.setData('text/plain', card.id)
    }
  }
  function onLaneDragOver(e: DragEvent, col: Lane) {
    if (!dndEnabled || !dragCard) return
    e.preventDefault()
    if (e.dataTransfer) e.dataTransfer.dropEffect = 'move'
  }
  function onLaneDrop(e: DragEvent, col: Lane) {
    if (!dndEnabled || !dragCard) return
    e.preventDefault()
    const card = dragCard
    const fromKey = dragFromColKey
    cleanupDrag()
    void commitDrop(card, fromKey, col)
  }

  // Within-column manual reorder (#426): when sort='manual', each card is
  // also a drop target so the user can insert the dragged card BEFORE a
  // specific sibling. The card-level handler runs first (event bubbles
  // upward); for same-column manual drops it owns the persist + optimistic
  // update and stops propagation so the lane handler doesn't double-fire.
  function onCardDragOver(e: DragEvent, card: TaskDetail, col: Lane) {
    if (sort !== 'manual' || !dndEnabled || !dragCard) return
    if (col.key !== dragFromColKey) return
    e.preventDefault()
    if (e.dataTransfer) e.dataTransfer.dropEffect = 'move'
    dragOverCardId = card.id
  }
  function onCardDrop(e: DragEvent, target: TaskDetail, col: Lane) {
    if (sort !== 'manual' || !dndEnabled || !dragCard) return
    if (col.key !== dragFromColKey) return
    e.preventDefault()
    e.stopPropagation()
    const src = dragCard
    const fromKey = dragFromColKey
    dragOverCardId = null
    cleanupDrag()
    if (src.id === target.id) return
    void commitManualReorder(src, target, col, fromKey)
  }

  // Renumber the cards in `col` after `src` is moved to the slot just before
  // `target`. Persists the diffs via a single batch setTaskOrders call (one
  // atomic write per file) with optimistic update + revert. Source group/
  // column is not renumbered on cross-column moves (gap-tolerant) — only the
  // destination column's 1-based sequence changes.
  async function commitManualReorder(
    src: TaskDetail,
    target: TaskDetail,
    col: Lane,
    fromKey: string
  ) {
    moveError = ''
    const my = ++moveSeq
    const prev = snapshotColumns()
    const cards = col.items
    const fromIdx = cards.findIndex((c) => c.id === src.id)
    const toIdx = cards.findIndex((c) => c.id === target.id)
    if (fromIdx === -1 || toIdx === -1) return
    const reordered = [...cards]
    reordered.splice(fromIdx, 1)
    // Splice-dance for "land BEFORE the target": removing src shifts the
    // target's index down by one when src was above it, so recompute the
    // insertion point against the post-removal array.
    const insertAt = fromIdx < toIdx ? toIdx - 1 : toIdx
    reordered.splice(insertAt, 0, src)

    const changes: { id: string; order: number }[] = []
    reordered.forEach((c, i) => {
      const newOrder = i + 1
      if ((c.manual_order ?? 0) !== newOrder) {
        changes.push({ id: c.id, order: newOrder })
      }
    })
    if (changes.length === 0) return

    // Optimistic: patch the column's items so the card snaps to its slot
    // before the IPC round-trip; revert on failure (mirror commitDrop). The
    // reordered cards MUST carry their fresh manual_order values — a second
    // reorder before this IPC completes would otherwise read stale values,
    // skip a card whose stale value happens to equal its new position, and
    // write a colliding [order::] token to disk.
    columns = columns.map((c) =>
      c.key === col.key
        ? {
            ...c,
            items: reordered.map((item, i) => ({
              ...item,
              manual_order: i + 1
            }))
          }
        : c
    )
    liveMessage = `Task moved to position ${
      changes.find((c) => c.id === src.id)?.order ?? ''
    } in ${col.label}.`
    // Serialize reorder IPCs so a newer reorder's write always lands after
    // an older one's on disk (the Go file-lock serializes, but IPC arrival
    // order across near-simultaneous calls is not guaranteed FIFO). The
    // optimistic UI already reflects the user's latest intent (manual_order
    // patched above); this ensures disk agrees.
    if (reorderInFlight) await reorderInFlight.catch(() => {})
    try {
      const p = ctx.setTaskOrders(
        changes.map((c) => ({ id: c.id, order: c.order }))
      )
      reorderInFlight = p.then(
        () => {},
        () => {}
      )
      await p
      if (my !== moveSeq) {
        // A newer reorder happened during the IPC. Reload to reconcile —
        // the newer reorder's IPC may have already written or may write
        // next; either way, disk is the source of truth now.
        void reload()
        return
      }
    } catch (e) {
      if (my !== moveSeq) return
      moveError = e instanceof Error ? e.message : String(e)
      revertTo(prev)
      liveMessage = 'Reorder failed — reverted.'
    }
  }

  function cleanupDrag() {
    dragCard = null
    dragFromColKey = ''
    draggingId = null
    dragOverCardId = null
  }

  // --- Keyboard parity (a11y) --------------------------------------------
  // ArrowLeft/Right move the focused card between adjacent columns for DnD-
  // enabled dimensions; Enter/Space open the inspector drawer; Shift+Enter
  // opens the sub-editor. Location dimensions (DnD disabled) get no arrow
  // move — the card stays put.
  function onCardKeydown(e: KeyboardEvent, card: TaskDetail, col: Lane) {
    if (dndEnabled && (e.key === 'ArrowRight' || e.key === 'ArrowLeft')) {
      e.preventDefault()
      const idx = columns.findIndex((c) => c.key === col.key)
      if (idx === -1) return
      const nextIdx =
        e.key === 'ArrowRight'
          ? Math.min(idx + 1, columns.length - 1)
          : Math.max(idx - 1, 0)
      if (nextIdx === idx) return
      void commitDrop(card, col.key, columns[nextIdx])
      return
    }
    if (e.key === 'Enter' && e.shiftKey) {
      e.preventDefault()
      subEditorCard = card
    } else if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault()
      selectedCard = card
    }
  }

  // --- Per-column quick-add ----------------------------------------------
  // Returns the createTask prefill for a column, or null when quick-add
  // isn't supported (custom status columns; location dimensions).
  function quickAddFor(col: Lane): {
    status?: TaskStatus
    dueDate?: string
    onCreated?: (id: string) => void
    beforeCreate?: () => boolean | Promise<boolean>
  } | null {
    if (groupBy === 'status') {
      if (!ALL_STATUSES.includes(col.value as TaskStatus)) return null
      return {
        status: col.value as TaskStatus,
        // Soft WIP confirm when the next create would exceed the column cap
        // (drag/keyboard already go through commitDrop → wouldExceedWip).
        beforeCreate: () => {
          const limit = wipLimitFor(col.value)
          if (limit == null) return true
          const live = columns.find((c) => c.key === col.key)
          const count = live?.items.length ?? 0
          if (count + 1 <= limit) return true
          return new Promise<boolean>((resolve) => {
            pendingQuickAddWip = { resolve }
          })
        }
      }
    }
    if (groupBy === 'owner') {
      // Unassigned (value='') → new task already has no owner; nothing to set.
      return col.value
        ? { onCreated: (id) => void ctx.setTaskOwner(id, col.value) }
        : {}
    }
    if (groupBy === 'priority') {
      return {
        onCreated: (id) => void ctx.setTaskPriority(id, Number(col.value))
      }
    }
    if (groupBy === 'dueDate') {
      const anchor = dueDateAnchor(col.value, today)
      return anchor ? { dueDate: anchor } : {}
    }
    if (groupBy === 'tag') {
      // No Tag column → new task already has no tags.
      return col.value
        ? { onCreated: (id) => void ctx.setTaskTags(id, [col.value]) }
        : {}
    }
    if (groupBy === 'none') return {}
    return null
  }

  onDestroy(() => {
    // No persistent timers to clear here; block:changed cleanup runs in the
    // effect's teardown. Hook kept for symmetry with Kanban/ListView.
  })

  let totalCards = $derived(columns.reduce((sum, c) => sum + c.items.length, 0))

  /** True when any status column is over its soft WIP limit (#437). */
  let anyOverWip = $derived(
    groupBy === 'status' &&
      columns.some((col) => {
        const limit = wipLimitFor(col.value)
        return limit != null && col.items.length > limit
      })
  )
</script>

<div
  class="flex-1 flex flex-col min-h-0 overflow-hidden"
  data-testid="tasks-board"
>
  {#if moveError}
    <ErrorBanner message={`Couldn't move task: ${moveError}`} />
  {/if}

  {#if configError}
    <ErrorBanner
      kind="warning"
      message={`Couldn't save board layout: ${configError}`}
    />
  {/if}

  {#if anyOverWip}
    <div
      class="px-6 py-2 bg-status-warn/10 border-b border-status-warn/30 text-status-warn text-type-sm font-body-md flex items-center gap-2"
      role="status"
      data-testid="board-wip-over-limit"
    >
      <span class="material-symbols-outlined text-icon-md" aria-hidden="true"
        >warning</span
      >
      <span>WIP over limit</span>
    </div>
  {/if}

  {#if columns.length > 10 && columns.length <= 20}
    <div
      class="px-6 py-2 bg-status-warn/10 border-b border-status-warn/30 text-status-warn text-type-sm font-body-md flex items-center gap-2"
      role="status"
    >
      <span class="material-symbols-outlined text-icon-md">info</span>
      <span>
        {columns.length}
        {groupBy} values — switch to List mode or filter to fewer to keep the board
        manageable.
      </span>
    </div>
  {/if}

  <!-- aria-live region for drag/keyboard move announcements -->
  <div class="sr-only" aria-live="polite">{liveMessage}</div>

  <div class="flex-1 overflow-hidden">
    {#if loading}
      <!-- Skeleton: 5 dashed column shells with shimmering card placeholders,
           mirroring the rendered flex-gapped column layout closely enough to
           feel like a preview. Reuses the global .skeleton-text shimmer so the
           pulse matches ListView's loading rows. -->
      <div
        class="h-full flex gap-4 p-4 overflow-x-auto custom-scrollbar"
        data-testid="tasks-board-loading"
        aria-busy="true"
        aria-label="Loading board"
      >
        {#each Array(5) as _, i (i)}
          <div class="flex-1 min-w-55 space-y-2">
            <div class="skeleton-text" style="width: 40%; height: 14px"></div>
            {#each Array(3) as _, j (j)}
              <div
                class="rounded-lg border border-dashed border-surface-panel-border p-3 space-y-1.5"
              >
                <div class="skeleton-text" style="width: 75%"></div>
                <div class="skeleton-text subtitle"></div>
              </div>
            {/each}
          </div>
        {/each}
      </div>
    {:else if errorMsg}
      <ErrorBanner message={errorMsg} />
    {:else}
      <div
        class="h-full flex gap-4 p-4 overflow-x-auto custom-scrollbar"
        role="list"
        aria-label="Board columns"
      >
        {#each columns as col, colIdx (col.key)}
          {@const cards = col.items}
          {@const canManage = groupBy === 'status'}
          {@const isStatusCol = ALL_STATUSES.includes(col.value as TaskStatus)}
          {@const statusName = canManage
            ? col.value.replace(/^status-/, '')
            : ''}
          {@const wipLimit = canManage ? wipLimitFor(col.value) : null}
          {@const overWip = wipLimit != null && cards.length > wipLimit}
          {@const qa = quickAddFor(col)}
          <section
            class="flex flex-col min-w-70 flex-1 max-w-100 rounded-lg border border-surface-panel-border bg-surface-panel/50 {colDragIndex ===
            colIdx
              ? 'opacity-50'
              : ''} {overWip ? 'ring-1 ring-status-warn/40' : ''}"
            role="group"
            aria-label={col.label}
            data-wip-over={overWip ? 'true' : undefined}
            ondragover={(e) => onLaneDragOver(e, col)}
            ondrop={(e) => onLaneDrop(e, col)}
          >
            <!-- svelte-ignore a11y_no_static_element_interactions
                 Column drag-reorder (status dimension) is a pointer-only
                 affordance; Rename/Remove are exposed via the header menu
                 button for keyboard users. -->
            <div
              class="flex items-center justify-between px-3 py-2.5 border-b border-surface-panel-border"
              draggable={canManage ? 'true' : undefined}
              ondragstart={canManage
                ? (e) => onColDragStart(e, colIdx)
                : undefined}
              ondragover={canManage
                ? (e) => onColDragOver(e, colIdx)
                : undefined}
              ondrop={canManage ? (e) => onColDrop(e, colIdx) : undefined}
              ondragend={canManage ? () => (colDragIndex = null) : undefined}
              title={!dndEnabled
                ? `Task location can't be changed by dragging`
                : undefined}
            >
              <div class="flex items-center gap-2 min-w-0">
                {#if canManage}
                  <span
                    class="material-symbols-outlined text-icon-sm text-text-muted cursor-grab active:cursor-grabbing shrink-0"
                    title="Drag to reorder"
                    spellcheck="false">drag_indicator</span
                  >
                {/if}
                <span
                  class="w-2 h-2 rounded-full shrink-0"
                  class:bg-text-muted={col.value !== 'DOING' &&
                    col.value !== 'DONE'}
                  class:bg-accent-secondary-start={col.value === 'DOING'}
                  class:bg-accent-primary-start={col.value === 'DONE'}
                ></span>
                {#if renamingColKey === col.key}
                  <input
                    type="text"
                    bind:value={renameValue}
                    onkeydown={(e) => {
                      if (e.key === 'Enter') commitRename(statusName)
                      else if (e.key === 'Escape') cancelRename()
                    }}
                    onblur={() => commitRename(statusName)}
                    class="bg-surface-panel border border-accent-primary-start/40 rounded px-1.5 py-0.5 text-type-xs font-label-sm-bold uppercase tracking-widest text-text-primary outline-none w-28"
                    aria-label="Rename column"
                  />
                {:else}
                  <h2
                    class="font-label-sm-bold uppercase tracking-widest text-type-xs text-text-muted truncate"
                    title={canManage ? 'Double-click to rename' : col.label}
                    ondblclick={canManage
                      ? () => startRename(statusName, col.key)
                      : undefined}
                  >
                    {col.label}
                  </h2>
                {/if}
                {#if wipLimit != null}
                  <span
                    class="bg-hover text-type-2xs px-1.5 py-0.5 rounded-sm font-label-sm {overWip
                      ? 'text-status-warn'
                      : 'text-text-muted'}"
                    data-testid={`board-wip-badge-${col.key}`}
                    title={overWip
                      ? `Over WIP limit (${cards.length} / ${wipLimit})`
                      : `WIP limit ${cards.length} / ${wipLimit}`}
                    aria-label={`${cards.length} of ${wipLimit} WIP${overWip ? ', over limit' : ''}`}
                  >
                    {cards.length} / {wipLimit}
                  </span>
                {:else}
                  <span
                    class="bg-hover text-text-muted text-type-2xs px-1.5 py-0.5 rounded-sm font-label-sm"
                    >{cards.length}</span
                  >
                {/if}
                {#if columnEstimateSum(cards) > 0}
                  <span
                    class="text-text-muted/60 text-type-2xs font-label-sm truncate"
                    data-testid={`board-col-estimate-${col.key}`}
                    title={`${formatEstimateSum(columnEstimateSum(cards))} estimated`}
                  >
                    {formatEstimateSum(columnEstimateSum(cards))} estimated
                  </span>
                {/if}
              </div>
              <div class="relative shrink-0 flex items-center">
                {#if canManage}
                  <button
                    type="button"
                    onclick={() => toggleColMenu(col.key)}
                    aria-label="Column actions"
                    aria-expanded={menuCol === col.key}
                    aria-haspopup="true"
                    class="text-text-muted hover:text-text-primary transition-colors p-0.5"
                  >
                    <span class="material-symbols-outlined text-icon-md"
                      >more_horiz</span
                    >
                  </button>
                  {#if menuCol === col.key}
                    <div
                      class="absolute right-0 top-full mt-1 z-50 min-w-35 bg-surface-popover border border-surface-popover-border rounded-lg shadow-xl py-1"
                      role="menu"
                      tabindex="-1"
                      onkeydown={(e) => {
                        if (e.key === 'Escape') menuCol = null
                      }}
                    >
                      <button
                        type="button"
                        onclick={() => startRename(statusName, col.key)}
                        class="w-full text-left flex items-center gap-2 px-3 py-1.5 hover:bg-hover text-type-sm font-label-sm text-text-primary"
                        role="menuitem"
                      >
                        <span class="material-symbols-outlined text-icon-sm"
                          >edit</span
                        >
                        Rename
                      </button>
                      {#if wipEditCol === statusName}
                        <div
                          class="px-3 py-2 space-y-1.5 border-t border-b border-surface-popover-border"
                          data-testid={`board-wip-edit-${col.key}`}
                        >
                          <label
                            class="block text-type-2xs font-label-sm text-text-muted"
                            for={`wip-input-${col.key}`}
                          >
                            WIP limit (empty = unlimited)
                          </label>
                          <input
                            id={`wip-input-${col.key}`}
                            type="number"
                            min="1"
                            step="1"
                            class="w-full rounded border border-surface-card-border bg-surface-card px-2 py-1 text-type-sm text-text-primary"
                            bind:value={wipDraft}
                            aria-invalid={wipEditError ? 'true' : undefined}
                            data-testid={`board-wip-input-${col.key}`}
                            onkeydown={(e) => {
                              if (e.key === 'Enter') {
                                e.preventDefault()
                                applyWipLimit(statusName)
                              } else if (e.key === 'Escape') {
                                e.preventDefault()
                                e.stopPropagation()
                                wipEditCol = null
                                wipEditError = ''
                              }
                            }}
                          />
                          {#if wipEditError}
                            <p class="text-type-2xs text-error" role="alert">
                              {wipEditError}
                            </p>
                          {/if}
                          <div class="flex gap-1 pt-0.5">
                            <button
                              type="button"
                              class="flex-1 px-2 py-1 rounded bg-accent-primary-start/15 text-accent-primary-start text-type-2xs font-label-sm"
                              data-testid={`board-wip-apply-${col.key}`}
                              onclick={() => applyWipLimit(statusName)}
                            >
                              Apply
                            </button>
                            <button
                              type="button"
                              class="flex-1 px-2 py-1 rounded hover:bg-hover text-type-2xs font-label-sm text-text-muted"
                              data-testid={`board-wip-clear-${col.key}`}
                              onclick={() => clearWipLimit(statusName)}
                            >
                              Clear
                            </button>
                          </div>
                        </div>
                      {:else}
                        <button
                          type="button"
                          onclick={() => startWipEdit(statusName)}
                          class="w-full text-left flex items-center gap-2 px-3 py-1.5 hover:bg-hover text-type-sm font-label-sm text-text-primary"
                          role="menuitem"
                          data-testid={`board-wip-menu-${col.key}`}
                        >
                          <span class="material-symbols-outlined text-icon-sm"
                            >speed</span
                          >
                          WIP limit…
                        </button>
                      {/if}
                      <button
                        type="button"
                        onclick={() => removeColumn(statusName)}
                        class="w-full text-left flex items-center gap-2 px-3 py-1.5 hover:bg-hover text-type-sm font-label-sm text-error"
                        role="menuitem"
                      >
                        <span class="material-symbols-outlined text-icon-sm"
                          >delete</span
                        >
                        Remove
                      </button>
                    </div>
                  {/if}
                {/if}
              </div>
            </div>
            <div
              class="flex-1 overflow-y-auto custom-scrollbar p-2 space-y-2 min-h-25"
            >
              {#each cards as card, i (card.id)}
                <div
                  data-card={card.id}
                  data-index={i}
                  role="button"
                  tabindex="0"
                  aria-grabbed={draggingId === card.id ? 'true' : 'false'}
                  aria-label={`${card.clean_content}, ${col.label}${card.owner ? `, owner ${card.owner}` : ''}${card.due_date ? `, due ${card.due_date}` : ''}${card.pinned ? ', pinned' : ''}${card.recurrence ? `, recurring ${card.recurrence}` : ''}${card.is_blocked ? ', blocked by unfinished prerequisite' : ''}${card.subtask_total > 0 ? `, ${card.subtask_done} of ${card.subtask_total} subtasks done` : ''}.${dndEnabled ? ' Arrow keys change ' + groupBy + '.' : ''}`}
                  draggable={dndEnabled ? 'true' : 'false'}
                  animate:flip={{ duration: 200, easing: cubicOut }}
                  class="group relative bg-surface-card border border-surface-card-border rounded-lg p-3 transition-all duration-200 hover:bg-hover hover:-translate-y-px hover:shadow-lg focus:outline-none focus:ring-2 focus:ring-accent-primary-start/40 {card.status ===
                  'DOING'
                    ? 'border-l-2 border-l-accent-secondary-start'
                    : ''} {dndEnabled ? 'cursor-grab' : ''} {draggingId ===
                  card.id
                    ? 'opacity-40 rotate-2'
                    : ''} {dragOverCardId === card.id
                    ? 'ring-2 ring-accent-primary-start/60'
                    : ''}"
                  ondragstart={(e) => onDragStart(e, card, col.key)}
                  ondragend={cleanupDrag}
                  ondragover={(e) => onCardDragOver(e, card, col)}
                  ondragleave={() => {
                    if (dragOverCardId === card.id) dragOverCardId = null
                  }}
                  ondrop={(e) => onCardDrop(e, card, col)}
                  onkeydown={(e) => onCardKeydown(e, card, col)}
                  onclick={() => (selectedCard = card)}
                >
                  {#if card.pinned}
                    <span
                      class="material-symbols-outlined absolute top-2 right-2 text-icon-sm text-accent-primary-start"
                      aria-label="pinned">push_pin</span
                    >
                  {/if}
                  <div class="flex justify-between items-start mb-2 gap-2">
                    {#if card.priority && card.priority <= 3}
                      <span
                        class="px-1.5 py-0.5 border rounded-sm font-label-sm text-type-3xs uppercase tracking-wide {priorityClass(
                          card.priority
                        )}"
                      >
                        {PRIORITY_LABELS[card.priority] ?? 'Normal'}
                      </span>
                    {/if}
                    {#if card.status === 'DONE'}
                      <span
                        class="material-symbols-outlined text-accent-primary-start text-icon-md {card.pinned
                          ? ''
                          : 'ml-auto'}">check_circle</span
                      >
                    {/if}
                  </div>
                  <p
                    class="text-type-md font-body-md text-text-primary mb-2 {card.status ===
                    'DONE'
                      ? 'line-through opacity-60'
                      : ''}"
                  >
                    {card.clean_content}
                  </p>
                  {#if card.progress > 0}
                    <div
                      class="h-0.5 bg-surface-panel rounded overflow-hidden mb-2"
                    >
                      <div
                        class="h-full bg-accent-secondary-start transition-all"
                        style="width: {card.progress}%"
                      ></div>
                    </div>
                  {/if}
                  <div class="flex justify-between items-center gap-2">
                    <div class="flex items-center gap-1.5">
                      {#if card.subtask_total > 0}
                        <span
                          class="text-type-3xs text-text-muted font-label-sm"
                          data-testid={`board-subtask-badge-${card.id}`}
                          title={`${card.subtask_done} of ${card.subtask_total} subtasks done`}
                          aria-label={`${card.subtask_done} of ${card.subtask_total} subtasks done`}
                        >
                          [{card.subtask_done}/{card.subtask_total}]
                        </span>
                      {/if}
                      {#if card.owner}
                        <span
                          class="text-type-3xs text-accent-secondary-start bg-accent-secondary-glow border border-accent-secondary-start/30 rounded-sm px-1.5 py-0.5 font-label-sm"
                        >
                          [{card.owner}]
                        </span>
                      {/if}
                    </div>
                    <div class="flex items-center gap-1.5">
                      {#if card.comments_count > 0}
                        <span
                          class="text-type-3xs text-text-muted font-label-sm flex items-center gap-0.5"
                          title="{card.comments_count} comments"
                        >
                          <span class="material-symbols-outlined text-icon-xs"
                            >chat_bubble</span
                          >
                          {card.comments_count}
                        </span>
                      {/if}
                      {#if card.links_count > 0}
                        <span
                          class="text-type-3xs text-text-muted font-label-sm flex items-center gap-0.5"
                          title="{card.links_count} links"
                        >
                          <span class="material-symbols-outlined text-icon-xs"
                            >link</span
                          >
                          {card.links_count}
                        </span>
                      {/if}
                      {#if card.due_date}
                        <span
                          class="text-type-3xs {card.status === 'DONE'
                            ? 'text-text-muted'
                            : dueDateTextClass(
                                dueDateClass(card.due_date, today)
                              )} font-label-sm flex items-center gap-0.5"
                        >
                          <span class="material-symbols-outlined text-icon-xs"
                            >schedule</span
                          >
                          {card.due_date}
                        </span>
                      {/if}
                      {#if card.recurrence}
                        <span
                          class="text-accent-secondary-start flex items-center"
                          title="Recurring: {card.recurrence}"
                        >
                          <span
                            class="material-symbols-outlined text-icon-xs"
                            aria-hidden="true">event_repeat</span
                          >
                        </span>
                      {/if}
                      {#if card.is_blocked}
                        <span
                          class="text-status-warn flex items-center"
                          role="img"
                          title="Blocked by unfinished prerequisite task(s)"
                          aria-label="Blocked by unfinished prerequisite task(s)"
                        >
                          <span
                            class="material-symbols-outlined text-icon-xs"
                            aria-hidden="true">lock</span
                          >
                        </span>
                      {/if}
                    </div>
                  </div>
                </div>
              {/each}
              {#if cards.length === 0}
                <div
                  class="text-center text-text-muted text-type-xs font-body-md py-6 border border-dashed border-surface-panel-border rounded-lg"
                >
                  No {col.label.toLowerCase()} tasks
                </div>
              {/if}
            </div>
            {#if qa !== null}
              <div
                class="shrink-0 px-2 py-1.5 border-t border-surface-panel-border"
              >
                {#if quickAddCol === col.key}
                  <QuickAddTask
                    {ctx}
                    status={qa.status}
                    dueDate={qa.dueDate}
                    onCreated={qa.onCreated}
                    beforeCreate={qa.beforeCreate}
                    placeholder={`Add to ${col.label} — Enter to add`}
                    keepOpenAfterCreate={true}
                    onCancel={() => {
                      if (quickAddCol === col.key) quickAddCol = null
                    }}
                  />
                {:else}
                  <button
                    type="button"
                    onclick={() => (quickAddCol = col.key)}
                    aria-label={`Add task to ${col.label}`}
                    data-testid={`board-add-${col.key}`}
                    class="w-full flex items-center justify-end gap-1 py-1 text-type-xs font-label-sm text-text-muted hover:text-accent-primary-start transition-colors border-none bg-transparent cursor-pointer"
                  >
                    <span class="material-symbols-outlined text-icon-sm"
                      >add</span
                    >
                    Add
                  </button>
                {/if}
              </div>
            {/if}
          </section>
        {/each}

        <!-- Add-column affordance: enabled only for status (user-managed
             columns). Other dimensions are data-driven. -->
        <div class="flex flex-col justify-center min-w-20">
          {#if groupBy === 'status'}
            <button
              type="button"
              onclick={addColumn}
              class="flex items-center gap-1 px-2.5 py-1 rounded border border-surface-panel-border bg-surface-panel text-type-sm font-label-sm text-text-muted hover:text-accent-primary-start hover:border-accent-primary-start/40 transition-colors self-start"
              aria-label="Add column"
            >
              <span class="material-symbols-outlined text-icon-md">add</span>
              <span>Column</span>
            </button>
          {:else}
            <button
              type="button"
              disabled
              title="Columns are data-driven when grouping by {groupBy}"
              aria-label="Add column (disabled)"
              class="flex items-center gap-1 px-2.5 py-1 rounded border border-surface-panel-border bg-surface-panel text-type-sm font-label-sm text-text-muted/40 cursor-not-allowed self-start"
            >
              <span class="material-symbols-outlined text-icon-md">add</span>
              <span>Column</span>
            </button>
          {/if}
        </div>
      </div>
    {/if}
  </div>
</div>

<TaskEditDrawer
  task={selectedCard}
  {ctx}
  onClose={() => (selectedCard = null)}
  onMetaChanged={reload}
  onOpenSubEditor={() => selectedCard && (subEditorCard = selectedCard)}
/>

{#if menuCol}
  <!-- Click-away for the column action menu. -->
  <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
  <div
    class="fixed inset-0 z-40"
    aria-hidden="true"
    onclick={() => (menuCol = null)}
  ></div>
{/if}

{#if pendingBlockedDone}
  <BlockedDoneDialog
    cardText={pendingBlockedDone.card.clean_content}
    blockers={pendingBlockedDone.blockers}
    onConfirm={confirmBlockedDone}
    onCancel={cancelBlockedDone}
  />
{/if}

{#if pendingWipConfirm}
  <ConfirmModal
    title="WIP limit"
    message="This column is over its WIP limit. Add anyway?"
    confirmLabel="Add anyway"
    cancelLabel="Cancel"
    dataTestId="board-wip-confirm"
    onConfirm={confirmWipOverLimit}
    onCancel={cancelWipOverLimit}
  />
{/if}

{#if pendingQuickAddWip}
  <ConfirmModal
    title="WIP limit"
    message="This column is over its WIP limit. Add anyway?"
    confirmLabel="Add anyway"
    cancelLabel="Cancel"
    dataTestId="board-wip-quickadd-confirm"
    onConfirm={() => {
      const p = pendingQuickAddWip
      pendingQuickAddWip = null
      p?.resolve(true)
    }}
    onCancel={() => {
      const p = pendingQuickAddWip
      pendingQuickAddWip = null
      p?.resolve(false)
    }}
  />
{/if}

{#if subEditorCard}
  <TaskSubEditorModal
    blockId={subEditorCard.id}
    notebook={subEditorCard.notebook}
    section={subEditorCard.section}
    page={subEditorCard.page}
    parentTaskText={subEditorCard.clean_content}
    {ctx}
    onClose={() => {
      void reload()
      subEditorCard = null
    }}
  />
{/if}

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
