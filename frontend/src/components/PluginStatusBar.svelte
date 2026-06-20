<script lang="ts">
  // Host for plugin-rendered status-bar items (#117). Subscribes to the
  // surface manager and renders each registered status-bar-item surface as
  // a compact icon+label chip in the status bar area.
  import { onDestroy } from 'svelte'
  import {
    getSurfaces,
    onSurfacesChanged,
    type PluginSurface
  } from '../plugins/surfaces'

  let surfaces = $state<PluginSurface[]>(getSurfaces('status-bar-item'))
  const off = onSurfacesChanged((all) => {
    surfaces = all.filter((s) => s.kind === 'status-bar-item')
  })
  onDestroy(() => off())
</script>

{#if surfaces.length > 0}
  <div class="flex items-center gap-1 px-1">
    {#each surfaces as surface (surface.id)}
      <button
        onclick={() => {
          // Status-bar items are informational; clicking could open a surface-
          // specific action. For now, just visual.
        }}
        class="flex items-center gap-1 px-1.5 py-0.5 rounded text-text-muted hover:text-accent-primary-start hover:bg-bg-hover transition-colors text-[10px] font-label-sm border border-transparent hover:border-border-muted cursor-default"
        title={surface.label}
        aria-label={surface.label}
      >
        {#if surface.icon}
          <span class="material-symbols-outlined text-[14px]"
            >{surface.icon}</span
          >
        {/if}
        <span class="truncate max-w-[80px]">{surface.label}</span>
      </button>
    {/each}
  </div>
{/if}
