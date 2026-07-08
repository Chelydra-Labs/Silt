/**
 * Trailing-edge debounce for the Tasks hub.
 *
 * #442: the TaskEditDrawer radiogroups commit on every ArrowLeft/Right/Home/End
 * keypress, and the next handler swallows any arrow pressed during the in-flight
 * write (`if (priorityPending) return`). A user tapping Right three times
 * quickly sees only the first commit land. The fix separates instant local
 * selection from a debounced (~200ms trailing) commit — this util implements
 * the trailing debounce.
 *
 * `trigger()` reschedules the timer on every call (cancel-and-reschedule), so
 * rapid calls collapse to a single invocation of `fn` after `ms` of quiet.
 * `cancel()` clears the pending timer — callers MUST call it from a Svelte 5
 * `$effect` teardown so an unmounting component never fires a stale commit.
 *
 * Synchronous `fn` only: the radiogroup commit is fire-and-forget with its own
 * revert-on-error path, so this util intentionally does not await or rethrow.
 */
export interface TrailingDebounce {
  /** Reschedule the trailing invocation. Safe to call repeatedly. */
  trigger: () => void
  /** Cancel any pending invocation. Idempotent. */
  cancel: () => void
  /** True when a trailing invocation is currently scheduled. */
  pending: () => boolean
}

export function trailingDebounce(fn: () => void, ms: number): TrailingDebounce {
  let timer: ReturnType<typeof setTimeout> | undefined

  return {
    trigger() {
      if (timer !== undefined) clearTimeout(timer)
      timer = setTimeout(() => {
        timer = undefined
        fn()
      }, ms)
    },
    cancel() {
      if (timer !== undefined) {
        clearTimeout(timer)
        timer = undefined
      }
    },
    pending() {
      return timer !== undefined
    }
  }
}
