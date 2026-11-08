// Hub-scoped undo state for the Tasks plugin (#1011).
//
// The Go journal is the source of truth; this module keeps a read-only mirror
// so every Task surface can render one consistent affordance. It is a module
// singleton like `state.svelte.ts`: the Tasks hub is a single mount at a time,
// and the drawer/board/list all need to see the same "what would undo do"
// answer (undo is global LIFO across surfaces, so a per-view store would lie).
//
// Refresh triggers, in order of freshness:
//  - after a successful tracked write (the journal captures with the write),
//  - on `block:changed` (debounced), which also covers writes made from other
//    Silt surfaces while the hub is open,
//  - after an undo itself, to learn the new journal head.
import type { PluginContext, TaskUndoResult, TaskUndoState } from '../../sdk'
import { friendlyCaughtError } from './errors'
import { undoPageLabel, undoResultAnnouncement } from './undoCopy'

// Debounce window for block:changed-driven peeks: a bulk paste emits one event
// per block and each peek is an IPC round-trip.
const REFRESH_DEBOUNCE_MS = 80

const EMPTY_STATE: TaskUndoState = {
  canUndo: false,
  remaining: 0,
  changedFields: [],
  blockIds: [],
  notebook: '',
  section: '',
  page: '',
  source: ''
}

let ctxRef: PluginContext | null = null
let unsubscribe: (() => void) | null = null
let refreshTimer: ReturnType<typeof setTimeout> | null = null
// Monotonic token: a stale peek must not overwrite a newer one.
let refreshSeq = 0

let journal = $state<TaskUndoState>({ ...EMPTY_STATE })
let undoPending = $state(false)
// Counter, not a boolean: a drawer field and a board drop can overlap, and the
// last release must be the one that re-enables undo.
let activeWrites = $state(0)
let announcement = $state('')
let error = $state('')

/** Bind the store to a plugin context and start watching the journal. */
export function initTasksUndo(ctx: PluginContext): void {
  resetTasksUndo()
  ctxRef = ctx
  unsubscribe = ctx.on('block:changed', () => {
    if (refreshTimer) clearTimeout(refreshTimer)
    refreshTimer = setTimeout(() => {
      refreshTimer = null
      void refreshTasksUndo()
    }, REFRESH_DEBOUNCE_MS)
  })
  void refreshTasksUndo()
}

/**
 * Drop the session mirror. Called on vault close/switch (the backend journal
 * is cleared there too) and by the test reset path.
 */
export function resetTasksUndo(): void {
  unsubscribe?.()
  unsubscribe = null
  if (refreshTimer) clearTimeout(refreshTimer)
  refreshTimer = null
  ctxRef = null
  refreshSeq++
  journal = { ...EMPTY_STATE }
  undoPending = false
  activeWrites = 0
  announcement = ''
  error = ''
}

/** Read-only peek at the journal head. Best-effort: a transient IPC failure
 * keeps the last known state instead of flapping the affordance. */
export async function refreshTasksUndo(): Promise<void> {
  const ctx = ctxRef
  if (!ctx) return
  const seq = ++refreshSeq
  try {
    const next = await ctx.getTaskUndoState()
    if (seq !== refreshSeq) return
    journal = next
  } catch {
    // See the header comment: the affordance degrades to its previous state.
  }
}

export function tasksUndoAvailable(): boolean {
  return (
    ctxRef !== null && journal.canUndo && !undoPending && activeWrites === 0
  )
}

/** Track a mutation write so undo pauses while it is in flight. */
export async function trackTaskUndoWrite<T>(write: Promise<T>): Promise<T> {
  activeWrites++
  try {
    return await write
  } finally {
    activeWrites--
    // The journal captures alongside the successful write; peeking here makes
    // undo available without waiting for the block:changed round-trip.
    void refreshTasksUndo()
  }
}

/**
 * Begin an in-flight write that is not expressed as a promise at the call
 * site (the drawer's optimistic fields). Returns a release function; calling
 * it more than once is safe.
 */
export function beginTaskUndoWrite(): () => void {
  activeWrites++
  let released = false
  return () => {
    if (released) return
    released = true
    activeWrites = Math.max(0, activeWrites - 1)
    void refreshTasksUndo()
  }
}

/**
 * Undo the newest journaled mutation. No-ops (returning null) while nothing is
 * undoable or another write/undo is in flight — surfaces disable the button in
 * that state, but the keyboard path relies on this guard too.
 */
export async function runTaskUndo(): Promise<TaskUndoResult | null> {
  const ctx = ctxRef
  if (!ctx || !tasksUndoAvailable()) return null
  undoPending = true
  error = ''
  try {
    const result = await ctx.undoLastTaskMutation()
    // `remaining` is authoritative for the depth after the call; the follow-up
    // peek only fills in the next entry's disclosure.
    journal = {
      ...journal,
      canUndo: result.remaining > 0,
      remaining: result.remaining
    }
    announcement = undoResultAnnouncement(result)
    await refreshTasksUndo()
    return result
  } catch (e) {
    error = `Couldn't undo: ${friendlyCaughtError(e)}`
    // The error banner is an assertive alert; clear the polite announcement so
    // a stale success message doesn't sit next to the failure.
    announcement = ''
    return null
  } finally {
    undoPending = false
  }
}

export function clearTasksUndoError(): void {
  error = ''
}

/** Reactive snapshot for the affordance components. */
export function getTasksUndoView() {
  return {
    canUndo: journal.canUndo,
    available: tasksUndoAvailable(),
    pending: undoPending,
    writePending: activeWrites > 0,
    remaining: journal.remaining,
    changedFields: journal.changedFields,
    pageLabel: undoPageLabel(journal.notebook, journal.section, journal.page),
    announcement,
    error
  }
}
