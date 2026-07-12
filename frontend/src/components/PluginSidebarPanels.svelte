<script lang="ts">
  // Host for plugin-rendered sidebar panels (#117, #227). Subscribes to the
  // surface manager. First-party surfaces may supply a compiled Svelte
  // `component` (direct mount, same pattern as note-banner #221); third-party
  // surfaces use sandboxed PluginSurfaceFrame iframes.
  import { onDestroy } from 'svelte'
  import {
    getSurfaces,
    onSurfacesChanged,
    type PluginSurface
  } from '../plugins/surfaces'
  import PluginSurfaceFrame from './PluginSurfaceFrame.svelte'
  import { makePluginContext } from '../plugins/context'
  import { getSessionToken } from '../plugins/loader'

  let surfaces = $state<PluginSurface[]>(getSurfaces('sidebar-panel'))

  const ctxCache = new Map<string, any>()

  const off = onSurfacesChanged((all) => {
    surfaces = all.filter((s) => s.kind === 'sidebar-panel')
    const active = new Set(surfaces.map((s) => s.pluginID))
    for (const id of ctxCache.keys()) {
      if (!active.has(id)) ctxCache.delete(id)
    }
  })

  onDestroy(() => off())

  function ctxFor(pluginID: string): any {
    let ctx = ctxCache.get(pluginID)
    if (!ctx) {
      ctx = makePluginContext(pluginID, getSessionToken(pluginID)) as any
      ctxCache.set(pluginID, ctx)
    }
    return ctx
  }
</script>

{#if surfaces.length > 0}
  <div class="px-1 pt-2 border-t border-surface-panel-border mt-auto space-y-1">
    {#each surfaces as surface (surface.id)}
      <!-- Collapsed by default + compact height: shared chrome for all
           sidebar-panel surfaces (third-party iframes and any first-party
           panels). AI Assistant uses a dedicated right drawer, not this host. -->
      <details class="group">
        <summary
          class="flex items-center gap-2 px-2 py-1.5 cursor-pointer hover:bg-hover rounded transition-colors select-none list-none"
        >
          <span
            class="material-symbols-outlined text-accent-primary-start/70 text-icon-md"
          >
            {surface.icon || 'extension'}
          </span>
          <span
            class="text-surface-sidebar-text text-type-xs font-label-sm-bold flex-1 truncate"
          >
            {surface.label}
          </span>
          <span
            class="material-symbols-outlined text-surface-sidebar-text-muted text-icon-sm group-open:rotate-180 transition-transform"
          >
            expand_more
          </span>
        </summary>
        <div
          class="h-48 mt-1 rounded-lg overflow-hidden border border-surface-panel-border"
        >
          {#if surface.component}
            {@const Panel = surface.component}
            {@const extra = surface.props ?? {}}
            <Panel {...extra} ctx={ctxFor(surface.pluginID)} />
          {:else}
            <PluginSurfaceFrame {surface} ctxProxy={ctxFor(surface.pluginID)} />
          {/if}
        </div>
      </details>
    {/each}
  </div>
{/if}
