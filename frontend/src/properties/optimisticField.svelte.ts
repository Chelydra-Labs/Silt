// Optimistic-commit primitive for typed-property edits. Mirrors the proven
// skeleton from the silt-tasks plugin (snapshot → optimistic set → write →
// revert-on-error) without reaching across the plugin boundary: the plugin's
// version imports a plugin-local error formatter, so chrome code carries its own
// thin equivalent here. The friendly message comes from coerceIPCError, the
// shared backend error-code coercion.
import { coerceIPCError } from '../lib/ipcError'

export interface OptimisticFieldOptions<T> {
  initial: T
  // The awaitable write; resolved value is ignored (Wails setters resolve void).
  write: (value: T) => Promise<unknown>
  onChanged?: () => void
  // Invoked with '' at the start of every commit (clearing any prior banner)
  // and with the friendly message when the write fails.
  onError?: (message: string) => void
}

export function optimisticField<T>(opts: OptimisticFieldOptions<T>) {
  let value = $state(opts.initial)
  let pending = $state(false)
  // Snapshot of the pre-commit value; the revert target. Initialized to
  // initial only to satisfy the type — reassigned before every read.
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
      opts.onError?.(coerceIPCError(e).message)
      return false
    } finally {
      pending = false
    }
  }

  // External reset (when the page changes or the value refreshes) without
  // invoking write.
  function reset(next: T): void {
    value = next
  }

  return {
    get value(): T {
      return value
    },
    set value(v: T) {
      value = v
    },
    get pending(): boolean {
      return pending
    },
    commit,
    reset
  }
}
