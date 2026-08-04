// Explicit, bounded local undo/redo history for the Source buffer.
//
// The browser's native textarea undo is unobservable: its stack cannot be
// inspected, its boundaries are not synchronised with seed/reload/external
// replacement, and direct `.value` assignment (which Svelte performs on
// rerender) can clear it without warning. Phase 1 of #861 verified the
// Wails WebView is no exception, so Source mode owns a deliberate, bounded
// history here and never relies on execCommand or native keyboard undo.

export interface SourceHistorySelection {
  start: number
  end: number
  direction: 'forward' | 'backward' | 'none'
}

export interface SourceHistoryEntry {
  value: string
  selection: SourceHistorySelection
}

export interface SourceHistoryPushOptions {
  /**
   * When true, the history may merge this entry into the previous one if it
   * is contiguous in time (within `coalesceMs`) and the pointer is at the
   * top of the stack. Callers decide caret contiguity; the history only
   * guards the time window and redo-tail invariants.
   */
  coalesce?: boolean
}

export interface SourceHistoryOptions {
  /** Maximum entries retained. Older entries drop off the front. */
  max?: number
  /** Time window in ms during which a coalesceable push merges with the top. */
  coalesceMs?: number
  /** Injectable clock for deterministic tests. */
  now?: () => number
}

export interface SourceHistory {
  /** Record an edit. Clears any redo tail unless it coalesces with the top. */
  push(entry: SourceHistoryEntry, opts?: SourceHistoryPushOptions): void
  /** The currently-active entry (top of the applied branch). */
  current(): SourceHistoryEntry | null
  canUndo(): boolean
  canRedo(): boolean
  /** Move the pointer back; returns the now-active entry, or null if none. */
  undo(): SourceHistoryEntry | null
  /** Move the pointer forward; returns the now-active entry, or null if none. */
  redo(): SourceHistoryEntry | null
  /** Boundary: discard every entry and seed with the given state. */
  reset(entry: SourceHistoryEntry): void
  /** Total entries stored (both branches). */
  size(): number
}

export const SOURCE_HISTORY_MAX_DEFAULT = 100
export const SOURCE_HISTORY_COALESCE_MS_DEFAULT = 400

export function createSourceHistory(
  opts: SourceHistoryOptions = {}
): SourceHistory {
  const max = opts.max ?? SOURCE_HISTORY_MAX_DEFAULT
  const coalesceMs = opts.coalesceMs ?? SOURCE_HISTORY_COALESCE_MS_DEFAULT
  const now = opts.now ?? (() => Date.now())

  const stack: SourceHistoryEntry[] = []
  // -1 = empty; otherwise index of the currently-applied entry. The redo
  // branch is everything in `stack` above the pointer.
  let pointer = -1
  let lastPushAt = -Infinity

  function push(
    entry: SourceHistoryEntry,
    pushOpts?: SourceHistoryPushOptions
  ): void {
    const at = now()
    const atTop = pointer >= 0 && pointer === stack.length - 1
    const withinWindow = at - lastPushAt <= coalesceMs
    const coalescable = pushOpts?.coalesce === true && atTop && withinWindow

    if (coalescable) {
      stack[pointer] = entry
    } else {
      // Drop any redo tail before pushing a new branch — typing after an
      // undo must invalidate the redo path.
      stack.length = pointer + 1
      stack.push(entry)
      if (stack.length > max) {
        // Slide the window forward by one; pointer lands on the new top.
        stack.shift()
        pointer = stack.length - 1
      } else {
        pointer++
      }
    }
    lastPushAt = at
  }

  function current(): SourceHistoryEntry | null {
    return pointer >= 0 ? (stack[pointer] ?? null) : null
  }

  function canUndo(): boolean {
    return pointer > 0
  }

  function canRedo(): boolean {
    return pointer >= 0 && pointer < stack.length - 1
  }

  function undo(): SourceHistoryEntry | null {
    if (!canUndo()) return null
    pointer--
    return stack[pointer] ?? null
  }

  function redo(): SourceHistoryEntry | null {
    if (!canRedo()) return null
    pointer++
    return stack[pointer] ?? null
  }

  function reset(entry: SourceHistoryEntry): void {
    stack.length = 0
    stack.push(entry)
    pointer = 0
    // Next push must NOT coalesce with the seed/reload/external state.
    lastPushAt = -Infinity
  }

  function size(): number {
    return stack.length
  }

  return { push, current, canUndo, canRedo, undo, redo, reset, size }
}
