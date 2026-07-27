/**
 * Canonical Wails event names, generated from the Go EventName enum (events.go)
 * via cmd/genenums. Frontend listeners import from here instead of re-typing
 * bare string literals, so the FE event-name set cannot drift from the backend.
 *
 * Note: a few event names are frontend-INTERNAL only (active-notebook:changed,
 * selection:changed, editor:save) — those are dispatched in-process via the
 * plugin bus, never cross the IPC boundary, and are NOT in the generated
 * EventName set. They stay in the PluginEventName union (plugins/sdk.ts).
 */
export { EventName } from '../generated/enums'
import type { EventName as EventNameT } from '../generated/enums'
export type EventNameType = EventNameT
