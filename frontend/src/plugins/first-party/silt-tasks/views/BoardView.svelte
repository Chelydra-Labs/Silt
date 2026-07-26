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
  // Reactive logic is split into two `.svelte.ts` controllers instantiated
  // below (mirrors the aiProviderController / localMcpController pattern):
  //  - useBoardDnd:        card/lane DnD, drop dispatcher, manual-reorder
  //                        serialization, DONE-guard + WIP-confirm commit
  //                        continuations.
  //  - useStatusColumns:   status-column config (rename/add/remove/WIP),
  //                        drag-reorder, persistence, optimistic snapshot.
  // This file is the render surface + the WIP-limit gating + per-column
  // quick-add shell. Dimension-aware DnD: each active groupBy routes a drop
  // to the SDK setter that owns the binned dimension. ctx.updateTaskMeta
  // only covers {pinned, progress}, so a drop dispatches to
  // updateBlockState / setTaskOwner / setTaskPriority / setTaskDueDate /
  // setTaskTags depending on groupBy. Notebook/section/page are file-
  // location dimensions, not mutable metadata — DnD is disabled for them.
  import { flip } from 'svelte/animate'
  import { cubicOut } from 'svelte/easing'
  import { onMount, onDestroy } from 'svelte'
  import type { TaskStatus } from '../../../sdk'
  import TaskEditDrawer from '../components/TaskEditDrawer.svelte'
  import TaskSubEditorModal from '../components/TaskSubEditorModal.svelte'
  import BlockedDoneDialog from '../components/BlockedDoneDialog.svelte'
  import ConfirmModal from '../components/ConfirmModal.svelte'
  import QuickAddTask from '../components/QuickAddTask.svelte'
  import ColumnHeader from '../components/ColumnHeader.svelte'
  import TaskCard from '../components/TaskCard.svelte'
  import {
    coerceTaskRow,
    PRIORITY_LABELS,
    laneLabel,
    type TaskDetail,
    type TaskViewProps
  } from '../types'
  import ErrorBanner from '../components/ErrorBanner.svelte'
  import { columnNames } from '../columns'
  import {
    getTaskHubQueryContext,
    getTaskHubViewState,
    setDisplayMode,
    type GroupBy
  } from '../state.svelte'
  import { binByDimension } from '../grouping'
  import { buildQuery } from '../query'
  import { useBlockChangedReload, useBlockedDoneGuard } from '../shared.svelte'
  import {
    createBoardDndController,
    dueDateAnchor,
    errMsg,
    type Lane
  } from './controllers/useBoardDnd.svelte'
  import { createStatusColumnsController } from './controllers/useStatusColumns.svelte'

  type Props = TaskViewProps

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

  // Core render model. BoardView owns the rendered columns + rows + load
  // lifecycle; both controllers access `columns` through the accessors below
  // so optimistic patches land on the same $state cell the template reads.
  let rows = $state<TaskDetail[]>([])
  let columns = $state<Lane[]>([])
  let loading = $state(true)
  let errorMsg = $state('')

  let selectedCard = $state<TaskDetail | null>(null)
  let subEditorCard = $state<TaskDetail | null>(null)
  // DONE-on-blocked guard (#302): the shared hook owns the pending state +
  // blocker fetch; BoardView attaches the optimistic-move context (card +
  // source/dest columns) the confirm/cancel handlers need to revert or
  // persist. BoardView swallows blocker-fetch errors and proceeds (avoids
  // stranding an optimistic card), so onError is a no-op.
  // ctx is a stable plugin-context singleton (created once by the hub; identity
  // never changes for this component's lifetime) — capturing it once matches the
  // prior inline $effect subscribes-once semantics.
  // svelte-ignore state_referenced_locally
  const blockedGuard = useBlockedDoneGuard<{
    card: TaskDetail
    fromColKey: string
    toCol: Lane
  }>(ctx)
  // Soft WIP-limit confirm (#437): shown when a drop/keyboard move would
  // push a status column over its configured cap. Cancel snaps back;
  // Confirm proceeds with the status change. The state stays in BoardView
  // (the ConfirmModal flow); the commit continuation lives in the DnD
  // controller so the whole drop lifecycle is in one place.
  let pendingWipConfirm = $state<{
    card: TaskDetail
    fromColKey: string
    toCol: Lane
  } | null>(null)
  // Soft WIP confirm for per-column quick-add (create path, not drag).
  let pendingQuickAddWip = $state<{
    resolve: (ok: boolean) => void
  } | null>(null)
  // Per-column quick-add shell: which column's inline add row is open.
  let quickAddCol = $state<string | null>(null)

  // --- Hub state (reactive reads) -----------------------------------------
  let groupBy = $derived(getTaskHubViewState().groupBy)
  let sort = $derived(getTaskHubViewState().sort)
  let scope = $derived(getTaskHubViewState().scope)
  let filters = $derived(getTaskHubViewState().filters)
  let today = $derived(ctx.today)
  let dndEnabled = $derived(DND_DIMENSIONS.has(groupBy))

  // --- High-cardinality guard ---------------------------------------------
  // >10 columns → non-blocking hint; >20 → refuse to render + fall back to
  // List mode so a 200-notebook vault doesn't freeze the board layout.
  let tooManyColumns = $state(false)

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
      // eslint-disable-next-line svelte/prefer-svelte-reactivity -- non-reactive local/helper
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
    columns = deriveColumns(
      rows,
      groupBy,
      columnNames(cols.statusColumns),
      today
    )
  }

  // Shared accessors over the rendered columns — both controllers read/write
  // through these so reactive updates land on this $state cell.
  const getColumns = () => columns
  const setColumnsState = (v: Lane[]) => {
    columns = v
  }

  // --- Controllers --------------------------------------------------------
  // Instantiated once per BoardView mount (mirrors AIProviderTab's
  // createAIProviderController / createLocalMcpController usage). cols must
  // precede dnd (dnd's snapshot/revert/clearColDrag delegates to it).
  const cols = createStatusColumnsController({
    getColumns,
    setColumns: setColumnsState,
    rebin
  })

  // ctx is a stable plugin-context singleton (see blockedGuard above);
  // capturing it once matches the prior inline closures.
  // svelte-ignore state_referenced_locally
  const dnd = createBoardDndController({
    ctx,
    getColumns,
    setColumns: setColumnsState,
    getGroupBy: () => groupBy,
    getSort: () => sort,
    getToday: () => today,
    isDndEnabled: () => dndEnabled,
    snapshotColumns: () => cols.snapshotColumns(),
    revertTo: (p) => cols.revertTo(p),
    reload,
    wouldExceedWip,
    blockedGuard,
    getPendingWipConfirm: () => pendingWipConfirm,
    setPendingWipConfirm: (v) => {
      pendingWipConfirm = v
    },
    clearColDragIndex: () => cols.clearColDragIndex()
  })

  /** Soft WIP limit for a status lane, or null when unlimited. */
  function wipLimitFor(statusName: string): number | null {
    const col = cols.statusColumns.find((c) => c.name === statusName)
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
      rows = (raw as unknown[]).map((r) => coerceTaskRow(r))
      rebin()
      // Keep the open drawer in sync with fresh data; if the task left the
      // result set, keep the last-known snapshot rather than snapping closed.
      if (selectedCard) {
        const fresh = rows.find((r) => r.id === selectedCard!.id)
        if (fresh) selectedCard = fresh
      }
    } catch (e) {
      if (my !== loadSeq) return
      errorMsg = errMsg(e)
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
    void cols.statusColumns
    rebin()
  })

  // Repaint on any block mutation (created/mutated/rescheduled from any
  // surface). Debounced so a burst of block:changed events triggers one
  // reload.
  // svelte-ignore state_referenced_locally
  useBlockChangedReload(ctx, reload)

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

  // --- Keyboard parity (a11y) --------------------------------------------
  // ArrowLeft/Right move the focused card between adjacent columns for DnD-
  // enabled dimensions (routes through the DnD controller's commitDrop);
  // Enter/Space open the inspector drawer; Shift+Enter opens the sub-editor.
  // Location dimensions (DnD disabled) get no arrow move — the card stays
  // put. Kept in BoardView because it straddles DnD-move + drawer-open.
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
      void dnd.commitDrop(card, col.key, columns[nextIdx])
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
  {#if dnd.moveError}
    <ErrorBanner message={`Couldn't move task: ${dnd.moveError}`} />
  {/if}

  {#if cols.configError}
    <ErrorBanner
      kind="warning"
      message={`Couldn't save board layout: ${cols.configError}`}
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
  <div class="sr-only" aria-live="polite">{dnd.liveMessage}</div>

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
          {@const statusName = canManage
            ? col.value.replace(/^status-/, '')
            : ''}
          {@const wipLimit = canManage ? wipLimitFor(col.value) : null}
          {@const overWip = wipLimit != null && cards.length > wipLimit}
          {@const qa = quickAddFor(col)}
          <section
            class="flex flex-col min-w-70 flex-1 max-w-100 rounded-lg border border-surface-panel-border bg-surface-panel/50 {cols.colDragIndex ===
            colIdx
              ? 'opacity-50'
              : ''} {overWip ? 'ring-1 ring-status-warn/40' : ''}"
            role="group"
            aria-label={col.label}
            data-wip-over={overWip ? 'true' : undefined}
            ondragover={(e) => dnd.onLaneDragOver(e, col)}
            ondrop={(e) => dnd.onLaneDrop(e, col)}
          >
            <!-- Column drag-reorder (status dimension) is a pointer-only
                 affordance; Rename/Remove are exposed via the header menu
                 button for keyboard users. -->
            <ColumnHeader
              colKey={col.key}
              colLabel={col.label}
              colValue={col.value}
              cardCount={cards.length}
              estimateSum={columnEstimateSum(cards)}
              {canManage}
              {wipLimit}
              {overWip}
              {dndEnabled}
              renaming={cols.renamingColKey === col.key}
              menuOpen={cols.menuCol === col.key}
              wipEditing={cols.wipEditCol === statusName}
              wipEditError={cols.wipEditError}
              bind:renameValue={cols.renameValue}
              bind:wipDraft={cols.wipDraft}
              onToggleMenu={() => cols.toggleColMenu(col.key)}
              onStartRename={() => cols.startRename(statusName, col.key)}
              onCommitRename={() => cols.commitRename(statusName)}
              onCancelRename={cols.cancelRename}
              onRemoveColumn={() => cols.removeColumn(statusName)}
              onStartWipEdit={() => cols.startWipEdit(statusName)}
              onApplyWipLimit={() => cols.applyWipLimit(statusName)}
              onClearWipLimit={() => cols.clearWipLimit(statusName)}
              onMenuEscape={() => (cols.menuCol = null)}
              onWipEscape={() => cols.escapeWipEdit()}
              onColDragStart={(e) => cols.onColDragStart(e, colIdx)}
              onColDragOver={(e) => cols.onColDragOver(e, colIdx)}
              onColDrop={(e) => cols.onColDrop(e, colIdx)}
              onColDragEnd={() => cols.clearColDragIndex()}
            />
            <div
              class="flex-1 overflow-y-auto custom-scrollbar p-2 space-y-2 min-h-25"
            >
              {#each cards as card, i (card.id)}
                <div animate:flip={{ duration: 200, easing: cubicOut }}>
                  <TaskCard
                    {card}
                    index={i}
                    colLabel={col.label}
                    {dndEnabled}
                    {groupBy}
                    {today}
                    dragging={dnd.draggingId === card.id}
                    dragOver={dnd.dragOverCardId === card.id}
                    onDragStart={(e) => dnd.onDragStart(e, card, col.key)}
                    onDragEnd={dnd.cleanupDrag}
                    onCardDragOver={(e) => dnd.onCardDragOver(e, card, col)}
                    onCardDragLeave={() => dnd.clearDragOverIf(card.id)}
                    onCardDrop={(e) => dnd.onCardDrop(e, card, col)}
                    onKeydown={(e) => onCardKeydown(e, card, col)}
                    onSelect={() => (selectedCard = card)}
                  />
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
              onclick={cols.addColumn}
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

{#if cols.menuCol}
  <!-- Click-away for the column action menu. -->
  <div
    class="fixed inset-0 z-40"
    aria-hidden="true"
    onclick={() => (cols.menuCol = null)}
  ></div>
{/if}

{#if blockedGuard.pending}
  <BlockedDoneDialog
    cardText={blockedGuard.pending.context.card.clean_content}
    blockers={blockedGuard.pending.blockers}
    onConfirm={dnd.confirmBlockedDone}
    onCancel={dnd.cancelBlockedDone}
  />
{/if}

{#if pendingWipConfirm}
  <ConfirmModal
    title="WIP limit"
    message="This column is over its WIP limit. Add anyway?"
    confirmLabel="Add anyway"
    cancelLabel="Cancel"
    dataTestId="board-wip-confirm"
    onConfirm={dnd.confirmWipOverLimit}
    onCancel={dnd.cancelWipOverLimit}
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
