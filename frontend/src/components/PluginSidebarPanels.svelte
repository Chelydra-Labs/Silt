<script lang="ts">
  // Host for plugin-rendered sidebar panels (#117). Subscribes to the surface
  // manager and renders each registered sidebar-panel surface via a sandboxed
  // PluginSurfaceFrame. The frames run in isolated iframes (srcdoc +
  // allow-scripts, no allow-same-origin); a postMessage bridge proxies the
  // plugin's PluginContext.
  import { onMount, onDestroy } from 'svelte'
  import {
    getSurfaces,
    onSurfacesChanged,
    type PluginSurface
  } from '../plugins/surfaces'
  import PluginSurfaceFrame from './PluginSurfaceFrame.svelte'
  import { makePluginContext } from '../plugins/context'

  let surfaces = $state<PluginSurface[]>(getSurfaces('sidebar-panel'))

  const off = onSurfacesChanged((all) => {
    surfaces = all.filter((s) => s.kind === 'sidebar-panel')
  })

  onDestroy(() => off())
</script>

{#if surfaces.length > 0}
  <div class="px-1 pt-2 border-t border-border-muted mt-auto space-y-1">
    {#each surfaces as surface (surface.id)}
      <details class="group">
        <summary
          class="flex items-center gap-2 px-2 py-1.5 cursor-pointer hover:bg-hover rounded transition-colors select-none list-none"
        >
          <span
            class="material-symbols-outlined text-accent-primary-start/70 text-[16px]"
          >
            {surface.icon || 'extension'}
          </span>
          <span
            class="text-text-primary text-[11px] font-label-sm-bold flex-1 truncate"
          >
            {surface.label}
          </span>
          <span
            class="material-symbols-outlined text-text-muted text-[14px] group-open:rotate-180 transition-transform"
          >
            expand_more
          </span>
        </summary>
        <div
          class="h-48 mt-1 rounded-lg overflow-hidden border border-border-muted"
        >
          <PluginSurfaceFrame
            {surface}
            ctxProxy={makePluginContext(surface.pluginID) as any}
          />
        </div>
      </details>
    {/each}
  </div>
{/if}
