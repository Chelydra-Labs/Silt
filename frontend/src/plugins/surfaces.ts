// Plugin rendered-UI surface manager (#117). Third-party plugins cannot
// compile Svelte at runtime, so they render through a sandboxed <iframe>
// (srcdoc) with a postMessage bridge that proxies PluginContext calls back to
// the host. Strong isolation: no arbitrary third-party script in the main
// webview.
//
// A plugin requests surfaces through ctx.registerSurface(...) (capability-
// gated by ui-surface). The host mounts each surface into the appropriate slot
// (sidebar panel, modal, status-bar item). This module tracks active surfaces
// and their cleanup.
//
// Capability gate (#158): registerSurface checks isGranted(pluginID,
// 'ui-surface') from the trusted Go grant cache BEFORE adding to the
// registry. This is the client-side mirror of the Go-side
// PluginRegisterSurface gate (#154) — defense in depth.

import type { Component } from 'svelte'
import { isGranted } from './grants.svelte'

export type SurfaceKind =
  | 'sidebar-panel'
  | 'modal'
  | 'status-bar-item'
  | 'command-palette-entry'
  | 'settings-panel'
  | 'note-banner'

export interface PluginSurface {
  /** Unique surface id (<pluginID>:<surfaceId>). */
  id: string
  pluginID: string
  kind: SurfaceKind
  label: string
  icon?: string
  /** The HTML document rendered inside the sandboxed iframe (srcdoc). Required
   *  for third-party plugins (which cannot compile Svelte at runtime). */
  html?: string
  /** First-party-only: a compiled Svelte component rendered directly in the
   *  host webview (no iframe isolation). When present, the host ignores
   *  `html` and mounts this component instead — first-party plugins already
   *  run in the main webview, so the sandbox bridge is unnecessary overhead
   *  for rich, interactive surfaces (e.g. the AI Summary banner). Third-party
   *  plugins have no compiled component to pass; this field is conventionally
   *  first-party. Either `component` or `html` MUST be supplied for content-
   *  rendering kinds (sidebar-panel/modal/note-banner/settings-panel). */
  // Props vary per surface; bare Component defaults to {} and rejects real UIs.
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  component?: Component<any>
  /** Extra props forwarded to a first-party `component` (merged with the
   *  host-supplied ctx + onDismiss). Ignored for the iframe path. */
  props?: Record<string, unknown>
  /** Click handler for chrome-rendered kinds (status-bar-item) where the host
   *  draws the affordance from label/icon. First-party callbacks only — a
   *  third-party plugin cannot inject executable code this way (its surface
   *  runs in the iframe). */
  onClick?: () => void
}

/** Surface kinds whose content the host renders from the surface's own
 *  component/html. Chrome-rendered kinds (status-bar-item,
 *  command-palette-entry) draw their affordance from label/icon and do not
 *  require content. */
const CONTENT_KINDS: ReadonlySet<SurfaceKind> = new Set([
  'sidebar-panel',
  'modal',
  'settings-panel',
  'note-banner'
])

type SurfaceListener = (surfaces: PluginSurface[]) => void

const surfaces = new Map<string, PluginSurface>()
const listeners = new Set<SurfaceListener>()

function notify() {
  const list = [...surfaces.values()]
  for (const fn of listeners) fn(list)
}

/**
 * Register a plugin surface. The plugin's HTML runs in a sandboxed iframe; the
 * bridge SDK (injected by SurfaceFrame) proxies PluginContext over postMessage.
 *
 * Capability gate (#158): checks isGranted(pluginID, 'ui-surface') from the
 * trusted Go grant cache. An ungranted plugin's surface is silently dropped
 * (warn). This is defense-in-depth alongside the Go-side
 * PluginRegisterSurface gate (#154).
 *
 * Returns an unregister function.
 */
export function registerSurface(surface: PluginSurface): () => void {
  if (!surface.id || !surface.pluginID) {
    throw new Error('Surface requires id and pluginID')
  }
  // A content-rendering surface must carry content one way or the other: an
  // HTML document for the sandboxed iframe path, or a compiled Svelte component
  // for the first-party direct-render path (#221). Chrome-rendered kinds
  // (status-bar-item, command-palette-entry) draw from label/icon + onClick.
  if (CONTENT_KINDS.has(surface.kind) && !surface.html && !surface.component) {
    throw new Error(
      `Surface ${surface.id} (${surface.kind}) requires html or component`
    )
  }
  if (!isGranted(surface.pluginID, 'ui-surface')) {
    console.warn(
      `[silt] plugin ${surface.pluginID} cannot register surfaces without the ui-surface capability`
    )
    return () => {}
  }
  surfaces.set(surface.id, surface)
  notify()
  return () => {
    surfaces.delete(surface.id)
    notify()
  }
}

/** Unregister a single surface by id. */
export function unregisterSurface(id: string): void {
  if (surfaces.delete(id)) notify()
}

/** Unregister every surface for a plugin (disable / uninstall / vault-close). */
export function unregisterPluginSurfaces(pluginID: string): void {
  let changed = false
  for (const id of [...surfaces.keys()]) {
    if (surfaces.get(id)?.pluginID === pluginID) {
      surfaces.delete(id)
      changed = true
    }
  }
  if (changed) notify()
}

/** Get surfaces of a specific kind (e.g. all sidebar panels). */
export function getSurfaces(kind?: SurfaceKind): PluginSurface[] {
  const list = [...surfaces.values()]
  return kind ? list.filter((s) => s.kind === kind) : list
}

/** Subscribe to surface-list changes. Returns an unsubscribe. */
export function onSurfacesChanged(fn: SurfaceListener): () => void {
  listeners.add(fn)
  return () => listeners.delete(fn)
}

/** Test-only: clear all surfaces. */
export function resetSurfacesForTests(): void {
  surfaces.clear()
  listeners.clear()
}
