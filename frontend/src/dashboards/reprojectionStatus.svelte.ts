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
      const t = typeof res.total === 'number' ? res.total : 0
      total = t
      processed = typeof res.processed === 'number' ? res.processed : 0
      // Trust the backend's active flag (total>0 at the source) over a
      // derived check so a race where the event lands between seed and the
      // first event doesn't resurrect a stale active state.
      active = res.active === true
    } catch {
      // Cold-state read is best-effort — a transient IPC failure leaves the
      // store idle; the live event will still catch any in-flight batch.
    }
  }

  function attach(): () => void {
    void seed()
    const off = Events.On(EventName.EventTypesReprojectionProgress, (event) => {
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
