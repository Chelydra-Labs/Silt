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
  // Last-known-persisted value — the revert target if a write fails. Advanced
  // on every successful commit and on external reset.
  let prev: T = opts.initial
  // Latest edit held while a write is in flight. `hasQueued` is the presence
  // flag because T may legitimately be any value (incl. undefined/0/'').
  let queued: T | undefined
  let hasQueued = false

  async function commit(next: T): Promise<boolean> {
    if (pending) {
      // Don't silently drop a fast second edit during a slow write — queue
      // the latest intent so the most-recent keystroke wins once the write
      // settles. Optimistic UI still reflects `next` immediately.
      queued = next
      hasQueued = true
      value = next
      return false
    }
    value = next
    pending = true
    opts.onError?.('')
    try {
      await opts.write(next)
      prev = next
      opts.onChanged?.()
      return true
    } catch (e) {
      value = prev
      opts.onError?.(coerceIPCError(e).message)
      return false
    } finally {
      pending = false
      if (hasQueued) {
        const replay = queued as T
        hasQueued = false
        queued = undefined
        // Fire-and-forget: pending is clear, so this runs immediately (or
        // re-queues if another edit raced in). After a failed write, `prev`
        // already holds the persisted value, so the replay commits against
        // the reverted state — the user's latest intent still wins.
        void commit(replay)
      }
    }
  }

  // External reset (page change / value refresh) without invoking write. A
  // reset fired while a write is pending (a types:changed / projection-error
  // refresh racing the in-flight write) is skipped so the user's queued edit
  // is not silently dropped; the next reset, once pending clears, re-seeds.
  function reset(next: T): void {
    if (pending) return
    hasQueued = false
    queued = undefined
    prev = next
    value = next
  }

  // Advance the persisted snapshot without invoking write. Used by the
  // clear-field path (ClearPageProperty) so a later failed commit reverts to
  // the cleared state, not the pre-clear value.
  function markPersisted(next: T): void {
    prev = next
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
    reset,
    markPersisted
  }
}
