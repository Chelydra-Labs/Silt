// Reactive controller for the Tasks Board drag-and-drop engine (#421/#426).
// Owns card/lane drag handlers, the dimension-aware drop dispatcher, the
// optimistic column updates + revert, the DONE-on-blocked + soft-WIP confirm
// commit continuations, and the within-column manual-reorder serialization.
//
// Lifted from BoardView.svelte following the repo's `.svelte.ts` rune-class
// pattern (aiProviderController / localMcpController): the controller owns its
// reactive state and exposes getters + methods; BoardView is a thin view over
// this surface. The shared render model (`columns`) + hub-derived values
// (`groupBy`/`sort`/`today`/`dndEnabled`) and the WIP-gating decision stay in
// BoardView and are injected as accessors so reactive reads/writes land on the
// same $state cell.
//
// CRITICAL: commitManualReorder's moveSeq + reorderInFlight serialization is
// load-bearing concurrency — it was copied verbatim (only the single `columns`
// read/write was rewired to the shared accessor) so the optimistic manual-
// reorder mutation stays safe under rapid reorders. manualOrder.test.ts is the
// golden master.
import type { PluginContext, TaskStatus } from '../../../../sdk'
import { plusDaysISO } from '../../../../sdk'
import type { GroupBy, SortMode } from '../../state.svelte'
import type { TaskDetail } from '../../types'
import type { useBlockedDoneGuard } from '../../shared.svelte'
import { nextManualOrder } from '../manualOrder'

// A rendered Board lane. `value` is the dimension value a drop/quick-add
// dispatches with — '' routes to the dimension's "clear" path (Unassigned
// owner, No Date due date, No Tag no-op). Named Lane (not BoardColumn) so it
// doesn't collide with the persisted StatusColumn model in columns.ts.
export interface Lane {
  key: string
  label: string
  value: string
  items: TaskDetail[]
}

// Context the DONE-on-blocked guard carries through to the confirm/cancel
// handlers (the optimistic card + its source/destination columns).
export interface BoardDndContext {
  card: TaskDetail
  fromColKey: string
  toCol: Lane
}

// Coerce a caught value to its message string. Raw extraction (no friendly
// mapping) so the rendered error text matches the pre-refactor behavior.
export function errMsg(e: unknown): string {
  return e instanceof Error ? e.message : String(e)
}

// Deterministic anchor per bucket so a drop produces a stable due date the
// next query will re-bin into the same column. Overdue → today nudges an
// already-late task to the local day; Later → today+30 picks a far-but-finite
// horizon rather than leaving the task undated. Shared with BoardView's
// per-column quick-add prefill.
export function dueDateAnchor(bucketKey: string, iso: string): string {
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

// Multi-membership: a task can appear in several tag columns. Dropping into a
// tag column ADDS that tag without removing the others.
function unionTags(card: TaskDetail, tag: string): string[] {
  const existing = (card.tags ?? '')
    .split('|')
    .map((t) => t.trim())
    .filter(Boolean)
  return existing.includes(tag) ? existing : [...existing, tag]
}

export interface BoardDndDeps {
  ctx: PluginContext
  // Shared render model (BoardView's $state `columns`). Reads/writes land on
  // the same reactive cell so optimistic patches re-render immediately.
  getColumns: () => Lane[]
  setColumns: (next: Lane[]) => void
  // Hub-derived dimension/sort/today/DnD-enabled reads.
  getGroupBy: () => GroupBy
  getSort: () => SortMode
  getToday: () => string
  isDndEnabled: () => boolean
  // Column-config helpers owned by useStatusColumns.
  snapshotColumns: () => Lane[]
  revertTo: (prev: Lane[]) => void
  // BoardView-owned collaborators.
  reload: () => Promise<void>
  wouldExceedWip: (card: TaskDetail, fromColKey: string, toCol: Lane) => boolean
  blockedGuard: ReturnType<typeof useBlockedDoneGuard<BoardDndContext>>
  // Soft WIP-limit confirm state stays in BoardView (the ConfirmModal flow);
  // the commit continuation lives here.
  getPendingWipConfirm: () => BoardDndContext | null
  setPendingWipConfirm: (v: BoardDndContext | null) => void
  // Clear the column-drag index when a card drag starts (column controller
  // owns it; a card drag must not show the column-drag ghost).
  clearColDragIndex: () => void
}

export function createBoardDndController({
  ctx,
  getColumns,
  setColumns,
  getGroupBy,
  getSort,
  getToday,
  isDndEnabled,
  snapshotColumns,
  revertTo,
  reload,
  wouldExceedWip,
  blockedGuard,
  getPendingWipConfirm,
  setPendingWipConfirm,
  clearColDragIndex
}: BoardDndDeps) {
  // --- DnD state -----------------------------------------------------------
  let dragCard: TaskDetail | null = null
  let dragFromColKey = ''
  let draggingId = $state<string | null>(null)
  // Within-column manual reorder (#426): the card the pointer is hovering
  // on; null when not over a valid same-column drop target.
  let dragOverCardId = $state<string | null>(null)
  let liveMessage = $state('')
  let moveError = $state('')

  // Monotonic token so a failed earlier move can't revert over a later
  // optimistic move (mirrors Kanban's moveSeq).
  let moveSeq = 0
  // In-flight setTaskOrders IPC; a newer within-column reorder awaits it so
  // the newer write lands after the older one's on disk. IPC arrival order
  // across near-simultaneous calls isn't FIFO, and the Go side is last-
  // writer-wins — without serialization a stale older orderByID resolving
  // later would clobber the user's newest intent. See commitManualReorder.
  let reorderInFlight: Promise<void> | null = null

  // The dimension-aware drop dispatcher. Each groupBy routes to the SDK
  // setter that actually owns the binned field on disk.
  function dispatchDrop(card: TaskDetail, toCol: Lane): Promise<unknown> {
    const groupBy = getGroupBy()
    const today = getToday()
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
    const groupBy = getGroupBy()
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

  // Apply the optimistic dimension move to the rendered columns. Tag is
  // multi-membership (card joins the target without leaving the source); the
  // single-membership dimensions remove the card from the source column.
  function applyOptimistic(card: TaskDetail, fromColKey: string, toCol: Lane) {
    const groupBy = getGroupBy()
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
    setColumns(
      getColumns().map((c) => {
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
    )
  }

  // Restore the card to its source column (used by the DONE-guard cancel
  // path and the confirm-failure path).
  function revertOptimistic(card: TaskDetail, fromColKey: string, toCol: Lane) {
    setColumns(
      getColumns().map((c) => {
        if (c.key === toCol.key) {
          return { ...c, items: c.items.filter((i) => i.id !== card.id) }
        }
        if (c.key === fromColKey) {
          if (c.items.some((i) => i.id === card.id)) return c
          return { ...c, items: [...c.items, { ...card }] }
        }
        return c
      })
    )
  }

  async function commitDrop(card: TaskDetail, fromColKey: string, toCol: Lane) {
    const groupBy = getGroupBy()
    const sort = getSort()
    const dndEnabled = isDndEnabled()
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
      !getPendingWipConfirm() &&
      !blockedGuard.pending &&
      !skipWipForBlockedDone
    ) {
      applyOptimistic(card, fromColKey, toCol)
      setPendingWipConfirm({ card, fromColKey, toCol })
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
      !blockedGuard.pending
    ) {
      // resolveBlockers (not check) so BoardView's moveSeq concurrency guard
      // can run between the await and the dialog open — a stale confirm
      // dialog must not land over a newer move.
      const result = await blockedGuard.resolveBlockers(card.id)
      if (result.ok) {
        // A second drop during the await bumped moveSeq; abandon this
        // commit so a stale confirm dialog can't land over a newer move.
        // Nothing is stranded — applyOptimistic hasn't run yet.
        if (my !== moveSeq) return
        if (result.blockers.length > 0) {
          applyOptimistic(card, fromColKey, toCol)
          blockedGuard.open(result.blockers, { card, fromColKey, toCol })
          return
        }
      }
      // !result.ok: blocker lookup failed — proceed with the persist below
      // rather than stranding the card in an un-committed optimistic state
      // (no moveSeq check on this path — matches the prior empty catch).
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
        try {
          await ctx.setTaskOrder(card.id, nextManualOrder(destItems))
        } catch (e) {
          if (my !== moveSeq) return
          moveError = errMsg(e)
          // The dimension change in dispatchDrop already persisted; reverting
          // the optimistic column placement would desync from disk. Reload to
          // pick up the on-disk state (new column, stale order value).
          await reload()
          liveMessage = 'Move partially failed — reloaded.'
        }
      }
    } catch (e) {
      if (my !== moveSeq) return
      moveError = errMsg(e)
      revertTo(prev)
      liveMessage = 'Move failed — reverted.'
    }
  }

  async function confirmBlockedDone() {
    const pending = blockedGuard.pending
    if (!pending) return
    blockedGuard.dismiss()
    const { card, fromColKey, toCol } = pending.context
    const sort = getSort()
    try {
      await ctx.updateBlockState(card.id, 'DONE')
      // Cross-column manual sort (#426): mirror commitDrop. The DONE jump is
      // a cross-column move under manual sort, so the card needs an order
      // token beyond the destination's tail. prev (snapshot at drag-start) is
      // not in scope here; read the current destination column instead.
      if (sort === 'manual') {
        const destItems =
          getColumns().find((c) => c.key === toCol.key)?.items ?? []
        try {
          await ctx.setTaskOrder(card.id, nextManualOrder(destItems))
        } catch (e) {
          moveError = errMsg(e)
          // The status change already persisted; reload picks up on-disk state
          // (new column, stale order value) rather than desyncing.
          await reload()
          liveMessage = 'Move partially failed — reloaded.'
          return
        }
      }
      liveMessage = 'Task completed despite open prerequisites.'
    } catch (e) {
      moveError = errMsg(e)
      revertOptimistic(card, fromColKey, toCol)
      liveMessage = 'Move failed — reverted.'
    }
  }

  function cancelBlockedDone() {
    const pending = blockedGuard.pending
    if (!pending) return
    blockedGuard.dismiss()
    const { card, fromColKey, toCol } = pending.context
    revertOptimistic(card, fromColKey, toCol)
    liveMessage = 'Move cancelled.'
  }

  function focusCard(cardId: string) {
    document.querySelector<HTMLElement>(`[data-card="${cardId}"]`)?.focus?.()
  }

  async function confirmWipOverLimit() {
    const pending = getPendingWipConfirm()
    if (!pending) return
    setPendingWipConfirm(null)
    const { card, fromColKey, toCol } = pending
    const groupBy = getGroupBy()
    const sort = getSort()
    // Card is already optimistically in the target. Hand off to the
    // blocked-DONE guard when applicable; otherwise persist the drop.
    if (groupBy === 'status' && toCol.value === 'DONE' && card.is_blocked) {
      // User already confirmed WIP; no concurrent drop concern here, but the
      // shared resolver still wires the fetch + shape map + error policy.
      const result = await blockedGuard.resolveBlockers(card.id)
      if (result.ok && result.blockers.length > 0) {
        blockedGuard.open(result.blockers, { card, fromColKey, toCol })
        focusCard(card.id)
        return
      }
      // !result.ok: blocker lookup failed — proceed with persist below.
    }
    liveMessage = announceMove(toCol)
    try {
      await dispatchDrop(card, toCol)
      if (sort === 'manual') {
        // Destination already includes the optimistic card; exclude it when
        // computing the prior max order so we don't count the card itself.
        const destItems =
          getColumns()
            .find((c) => c.key === toCol.key)
            ?.items.filter((i) => i.id !== card.id) ?? []
        try {
          await ctx.setTaskOrder(card.id, nextManualOrder(destItems))
        } catch (e) {
          moveError = errMsg(e)
          await reload()
          liveMessage = 'Move partially failed — reloaded.'
          focusCard(card.id)
          return
        }
      }
    } catch (e) {
      moveError = errMsg(e)
      revertOptimistic(card, fromColKey, toCol)
      liveMessage = 'Move failed — reverted.'
    }
    focusCard(card.id)
  }

  function cancelWipOverLimit() {
    const pending = getPendingWipConfirm()
    if (!pending) return
    setPendingWipConfirm(null)
    revertOptimistic(pending.card, pending.fromColKey, pending.toCol)
    liveMessage = 'Move cancelled — column is over its WIP limit.'
    focusCard(pending.card.id)
  }

  // --- Card DnD -----------------------------------------------------------
  function onDragStart(e: DragEvent, card: TaskDetail, colKey: string) {
    if (!isDndEnabled()) return
    dragCard = card
    dragFromColKey = colKey
    draggingId = card.id
    clearColDragIndex()
    if (e.dataTransfer) {
      e.dataTransfer.effectAllowed = 'move'
      e.dataTransfer.setData('text/plain', card.id)
    }
  }
  function onLaneDragOver(e: DragEvent, _col: Lane) {
    if (!isDndEnabled() || !dragCard) return
    e.preventDefault()
    if (e.dataTransfer) e.dataTransfer.dropEffect = 'move'
  }
  function onLaneDrop(e: DragEvent, col: Lane) {
    if (!isDndEnabled() || !dragCard) return
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
    if (getSort() !== 'manual' || !isDndEnabled() || !dragCard) return
    if (col.key !== dragFromColKey) return
    e.preventDefault()
    if (e.dataTransfer) e.dataTransfer.dropEffect = 'move'
    dragOverCardId = card.id
  }
  function onCardDrop(e: DragEvent, target: TaskDetail, col: Lane) {
    if (getSort() !== 'manual' || !isDndEnabled() || !dragCard) return
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
  //
  // VERBATIM concurrency logic (moveSeq + reorderInFlight serialization):
  // only the single `columns` read/write was rewired to the shared accessor.
  async function commitManualReorder(
    src: TaskDetail,
    target: TaskDetail,
    col: Lane,
    _fromKey: string
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
    setColumns(
      getColumns().map((c) =>
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
      moveError = errMsg(e)
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

  // Card drag-leave clears the hover marker when the pointer leaves the card
  // it was over (the template's onCardDragLeave callback).
  function clearDragOverIf(cardId: string) {
    if (dragOverCardId === cardId) dragOverCardId = null
  }

  return {
    // reactive state (read via getters so the template tracks $state)
    get draggingId() {
      return draggingId
    },
    get dragOverCardId() {
      return dragOverCardId
    },
    get liveMessage() {
      return liveMessage
    },
    get moveError() {
      return moveError
    },
    // actions
    onDragStart,
    onLaneDragOver,
    onLaneDrop,
    onCardDragOver,
    onCardDrop,
    clearDragOverIf,
    cleanupDrag,
    // Exposed so BoardView's keyboard handler can route arrow-key moves
    // through the same commit path as a drop.
    commitDrop,
    confirmBlockedDone,
    cancelBlockedDone,
    confirmWipOverLimit,
    cancelWipOverLimit
  }
}

export type BoardDndController = ReturnType<typeof createBoardDndController>
