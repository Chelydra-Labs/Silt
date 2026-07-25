// One-field optimistic-commit primitive shared by TaskEditDrawer's metadata
// editors. Owns the field's reactive { value, pending } plus the
// snapshot → optimistic set → write → revert-on-error skeleton that the pin,
// recurrence, due-date, status, estimate, owner, priority, tags, and title
// editors each hand-rolled before this extraction.
//
// Per-field concerns that the factory deliberately does NOT know about (the
// task guard, popover close calls, anchor-based revert for status/priority,
// tag announcements, estimate validation) stay in the component's thin
// wrappers — only the duplicated write skeleton lives here.
import { friendlyCaughtError } from './errors'

export interface OptimisticFieldOptions<T> {
  initial: T
  // The awaitable write; the resolved value is ignored (Wails setters resolve
  // boolean). Typed as Promise<unknown> so callers needn't discard the return.
  write: (value: T) => Promise<unknown>
  onChanged?: () => void
  // Invoked with '' at the start of every commit (clearing any prior banner,
  // matching the component's `metaError = ''` cleared-state convention) and
  // with the friendly message when the write fails.
  onError?: (message: string) => void
}

export function optimisticField<T>(opts: OptimisticFieldOptions<T>) {
  let value = $state(opts.initial)
  let pending = $state(false)
  // Snapshot of the pre-commit value; the revert target. Initialized to
  // initial only to satisfy the type — it is reassigned before every read.
  let prev: T = opts.initial

  async function commit(next: T): Promise<boolean> {
    if (pending) return false
    prev = value
    value = next
    pending = true
    opts.onError?.('')
    try {
      await opts.write(next)
      opts.onChanged?.()
      return true
    } catch (e) {
      value = prev
      opts.onError?.(friendlyCaughtError(e))
      return false
    } finally {
      pending = false
    }
  }

  // External reset (e.g. when the task prop changes) without invoking write.
  function reset(next: T) {
    value = next
  }

  return {
    get value() {
      return value
    },
    set value(v: T) {
      value = v
    },
    get pending() {
      return pending
    },
    commit,
    reset
  }
}
