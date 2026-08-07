// Reactive snapshot of the type-schema reprojection worker's progress,
// surfaced on the type dashboard as a non-blocking progress region.
//
// Two sources feed the same {active, processed, total} shape:
//   - Cold seed: GetTypesReprojectionStatus() is queried once on attach() so a
//     freshly mounted dashboard renders an already-running pass (the live
//     event only fires for batches that begin AFTER the listener attaches).
//   - Live updates: the `types:reprojection:progress` event carries
//     {state: "running"|"done", processed, total} as the batch advances. The
//     store sets active=false on "done" and hides itself.
//
// Mirrors pageTypeState.svelte.ts's attach()/disposer pattern: attach() opens
// the event subscription and returns a cleanup closure the host wires into
// onMount's teardown.
import { Events } from '@wailsio/runtime'
import { EventName } from '../generated/enums'
import { GetTypesReprojectionStatus } from '../../bindings/silt/app.js'

export interface ReprojectionStatus {
  readonly active: boolean
  readonly processed: number
  readonly total: number
  /** Subscribe to `types:reprojection:progress` and seed from the cold-state
   *  IPC binding; returns a disposer for onMount cleanup. */
  attach: () => () => void
}

export function createReprojectionStatus(): ReprojectionStatus {
  let active = $state(false)
  let processed = $state(0)
  let total = $state(0)
  // Set true by the first live progress event; lets seed() avoid clobbering a
  // fresher event with the stale snapshot captured during its IPC round-trip.
  let eventReceived = false

  function apply(
    state: string | undefined,
    nextProcessed: number,
    nextTotal: number
  ): void {
    total = nextTotal
    processed = nextProcessed
    active = state !== 'done' && nextTotal > 0
  }

  async function seed(): Promise<void> {
    try {
      const res = (await GetTypesReprojectionStatus()) as {
        active?: boolean
        processed?: number
        total?: number
      } | null
      if (!res) return
      // A live event that arrived while this IPC was in flight is strictly
      // fresher than the snapshot captured at call time — don't let the stale
      // seed clobber it (the race window is small but real for fast batches).
      if (eventReceived) return
      const t = typeof res.total === 'number' ? res.total : 0
      total = t
      processed = typeof res.processed === 'number' ? res.processed : 0
      active = res.active === true
    } catch {
      // Cold-state read is best-effort — a transient IPC failure leaves the
      // store idle; the live event will still catch any in-flight batch.
    }
  }

  function attach(): () => void {
    eventReceived = false
    void seed()
    const off = Events.On(EventName.EventTypesReprojectionProgress, (event) => {
      eventReceived = true
      const data = (event?.data ?? {}) as {
        state?: string
        processed?: number
        total?: number
      }
      apply(
        data.state,
        typeof data.processed === 'number' ? data.processed : 0,
        typeof data.total === 'number' ? data.total : 0
      )
    })
    return () => off()
  }

  return {
    get active() {
      return active
    },
    get processed() {
      return processed
    },
    get total() {
      return total
    },
    attach
  }
}
