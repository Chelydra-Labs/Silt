// Reactive snapshot of the type-schema reprojection worker's progress,
// surfaced on the type dashboard as a non-blocking progress region.
//
// Two sources feed the same {active, processed, total} shape:
//   - Cold seed: GetTypesReprojectionStatus() is queried once on attach() so a
//     freshly mounted dashboard renders an already-running pass (the live
//     event only fires for batches that begin AFTER the listener attaches).
//   - Live updates: the `types:reprojection:progress` event carries
//     TypesReprojectionProgressEvent {state, processed, total} as the batch
//     advances. The store sets active=false on "done" and hides itself.
//
// Payload field names match the Go structs in app_types.go (SoT). The event
// bus still delivers `any` at the boundary, so we normalize once here.
//
// Mirrors pageTypeState.svelte.ts's attach()/disposer pattern: attach() opens
// the event subscription and returns a cleanup closure the host wires into
// onMount's teardown.
import { Events } from '@wailsio/runtime'
import { EventName } from '../generated/enums'
import { GetTypesReprojectionStatus } from '../../bindings/silt/app.js'

/** Mirrors Go TypesReprojectionProgressEvent (app_types.go). */
export interface TypesReprojectionProgressEvent {
  state: string // "running" | "done" at runtime
  processed: number
  total: number
}

/** Mirrors Go TypesReprojectionStatus (app_types.go). */
export interface TypesReprojectionStatus {
  active: boolean
  processed: number
  total: number
}

export interface ReprojectionStatus {
  readonly active: boolean
  readonly processed: number
  readonly total: number
  /** Subscribe to `types:reprojection:progress` and seed from the cold-state
   *  IPC binding; returns a disposer for onMount cleanup. */
  attach: () => () => void
}

function asNumber(v: unknown): number {
  return typeof v === 'number' && Number.isFinite(v) ? v : 0
}

function normalizeProgress(data: unknown): TypesReprojectionProgressEvent {
  const d = (data ?? {}) as Partial<TypesReprojectionProgressEvent>
  return {
    state: typeof d.state === 'string' ? d.state : '',
    processed: asNumber(d.processed),
    total: asNumber(d.total)
  }
}

function normalizeStatus(data: unknown): TypesReprojectionStatus {
  const d = (data ?? {}) as Partial<TypesReprojectionStatus>
  return {
    active: d.active === true,
    processed: asNumber(d.processed),
    total: asNumber(d.total)
  }
}

export function createReprojectionStatus(): ReprojectionStatus {
  let active = $state(false)
  let processed = $state(0)
  let total = $state(0)
  // Set true by the first live progress event; lets seed() avoid clobbering a
  // fresher event with the stale snapshot captured during its IPC round-trip.
  let eventReceived = false

  function apply(ev: TypesReprojectionProgressEvent): void {
    total = ev.total
    processed = ev.processed
    active = ev.state !== 'done' && ev.total > 0
  }

  async function seed(): Promise<void> {
    try {
      const res = await GetTypesReprojectionStatus()
      if (res == null) return
      // A live event that arrived while this IPC was in flight is strictly
      // fresher than the snapshot captured at call time — don't let the stale
      // seed clobber it (the race window is small but real for fast batches).
      if (eventReceived) return
      const status = normalizeStatus(res)
      total = status.total
      processed = status.processed
      active = status.active
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
      apply(normalizeProgress(event?.data))
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
