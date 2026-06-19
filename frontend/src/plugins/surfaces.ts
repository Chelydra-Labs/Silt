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

export type SurfaceKind = 'sidebar-panel' | 'modal' | 'status-bar-item'

export interface PluginSurface {
  /** Unique surface id (<pluginID>:<surfaceId>). */
  id: string
  pluginID: string
  kind: SurfaceKind
  label: string
  icon?: string
  /** The HTML document rendered inside the sandboxed iframe (srcdoc). */
  html: string
}

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
 * Returns an unregister function.
 */
export function registerSurface(surface: PluginSurface): () => void {
  if (!surface.id || !surface.pluginID || !surface.html) {
    throw new Error('Surface requires id, pluginID, and html')
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
