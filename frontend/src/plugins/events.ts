// Plugin event bus (#106). Fans out host events to per-plugin subscribers and
// guarantees cleanup on disable/uninstall/vault-close so a plugin can never
// leak listeners across reloads.
//
// Two event transports feed the same dispatcher:
//   1. Wails host events (block:changed, config:changed) arrive via
//      EventsOn from wailsjs/runtime/runtime.js — subscribed lazily on the
//      first subscriber.
//   2. Frontend-internal events (active-notebook:changed, selection:changed)
//      are dispatched in-process via `dispatch` (the navigator + the editor
//      emit these; there is no Go round-trip).
//
// The dispatcher is a single module-scoped singleton: every ctx.on() call
// registers into it, keyed by (pluginID, event). cleanupPlugin(pluginID)
// removes every subscription for a plugin — the loader calls it on disable /
// uninstall / vault-close.

import { EventsOn, EventsOff } from '../../wailsjs/runtime/runtime.js'
import type { PluginEventName, PluginEventPayload } from './sdk'
type AnyCb = (payload: any) => void

interface Subscription {
  cb: AnyCb
}

// subscribers[eventName] -> Map<pluginID, Set<Subscription>>
const subscribers = new Map<PluginEventName, Map<string, Set<Subscription>>>()

// Wails event names that need a single global EventsOn listener (the rest are
// dispatched in-process). The handler just forwards the payload into dispatch.
const wailsHostEvents: PluginEventName[] = ['block:changed', 'config:changed']

// Track which Wails listeners are active so we EventsOff on the last unsubscribe.
const activeWailsListeners = new Set<PluginEventName>()

/**
 * Subscribe a plugin's callback to a host event. Returns an unsubscribe that
 * removes ONLY this subscription. The host also auto-cleans every plugin's
 * subscriptions via cleanupPlugin on disable/uninstall/vault-close.
 */
export function subscribe<E extends PluginEventName>(
  pluginID: string,
  event: E,
  cb: (payload: PluginEventPayload<E>) => void
): () => void {
  if (!pluginID) {
    throw new Error('subscribe requires a pluginID')
  }
  let byPlugin = subscribers.get(event)
  if (!byPlugin) {
    byPlugin = new Map()
    subscribers.set(event, byPlugin)
  }
  let set = byPlugin.get(pluginID)
  if (!set) {
    set = new Set()
    byPlugin.set(pluginID, set)
  }
  const sub: Subscription = { cb: cb as AnyCb }
  set.add(sub)

  // Lazily attach a single Wails listener the first time any plugin subscribes
  // to a host event (block:changed / config:changed). The Wails payload is
  // untyped at the IPC boundary; the cast asserts it matches the Go struct
  // shape (single source of truth lives in sdk.ts payload types).
  if (wailsHostEvents.includes(event) && !activeWailsListeners.has(event)) {
    activeWailsListeners.add(event)
    EventsOn(event, (payload) =>
      dispatch(event, payload as PluginEventPayload<E>)
    )
  }

  return () => {
    const s = subscribers.get(event)?.get(pluginID)
    if (s) {
      s.delete(sub)
      if (s.size === 0) {
        subscribers.get(event)?.delete(pluginID)
      }
    }
    // Tear down the Wails listener when the last subscriber for a host event
    // goes away (keeps the runtime listener set minimal).
    if (wailsHostEvents.includes(event)) {
      const remaining = subscribers.get(event)
      const empty = !remaining || remaining.size === 0
      if (empty && activeWailsListeners.has(event)) {
        activeWailsListeners.delete(event)
        EventsOff(event)
      }
    }
  }
}

/**
 * Dispatch a frontend-internal event to every subscriber. Used by the
 * navigator (active-notebook:changed) and the editor (selection:changed).
 * Host events (block:changed / config:changed) arrive via the Wails listener
 * and are dispatched here too, so there is a single fan-out path.
 */
export function dispatch<E extends PluginEventName>(
  event: E,
  payload: PluginEventPayload<E>
): void {
  const byPlugin = subscribers.get(event)
  if (!byPlugin) return
  // Snapshot EVERY subscription across all plugins BEFORE dispatching, so a
  // callback that unsubscribes (itself or a sibling) during dispatch cannot
  // mutate the live map under iteration.
  const subs: Subscription[] = []
  for (const set of byPlugin.values()) {
    for (const sub of set) subs.push(sub)
  }
  for (const sub of subs) {
    try {
      sub.cb(payload)
    } catch (err) {
      // A plugin callback throwing must never break sibling plugins or the
      // host. Log and continue (fail-soft for the bus itself; the plugin's
      // own errors are the plugin's responsibility).
      // eslint-disable-next-line no-console
      console.error(`[silt plugin bus] ${event} handler threw:`, err)
    }
  }
}

/**
 * Remove every subscription for a plugin across all events. Called by the
 * loader on disable / uninstall / vault-close (#106).
 */
export function cleanupPlugin(pluginID: string): void {
  for (const [event, byPlugin] of subscribers) {
    if (byPlugin.delete(pluginID)) {
      // Tear down a Wails listener if that was the last subscriber.
      if (
        wailsHostEvents.includes(event) &&
        byPlugin.size === 0 &&
        activeWailsListeners.has(event)
      ) {
        activeWailsListeners.delete(event)
        EventsOff(event)
      }
    }
  }
}

/**
 * Remove every subscription for every plugin. Used in tests and on a full
 * vault teardown to guarantee a clean slate.
 */
export function clearAllSubscribers(): void {
  for (const event of activeWailsListeners) {
    EventsOff(event)
  }
  activeWailsListeners.clear()
  subscribers.clear()
}

/** Test-only: count live subscriptions for a plugin+event. */
export function subscriberCount(
  pluginID: string,
  event?: PluginEventName
): number {
  if (event) {
    return subscribers.get(event)?.get(pluginID)?.size ?? 0
  }
  let n = 0
  for (const byPlugin of subscribers.values()) {
    n += byPlugin.get(pluginID)?.size ?? 0
  }
  return n
}
