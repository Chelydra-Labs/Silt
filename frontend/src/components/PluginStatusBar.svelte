<script lang="ts">
  // Host for plugin-rendered status-bar items (#117). Subscribes to the
  // surface manager and renders each registered status-bar-item surface as
  // a compact icon+label chip in the status bar area. An optional `trailing`
  // snippet renders at the right end so first-party controls (the Date Glance
  // chip) share the same bar instead of stacking a second strip.
  import { onDestroy } from 'svelte'
  import type { Snippet } from 'svelte'
  import {
    getSurfaces,
    onSurfacesChanged,
    type PluginSurface
  } from '../plugins/surfaces'

  interface Props {
    trailing?: Snippet
  }
  let { trailing }: Props = $props()

  let surfaces = $state<PluginSurface[]>(getSurfaces('status-bar-item'))
  const off = onSurfacesChanged((all) => {
    surfaces = all.filter((s) => s.kind === 'status-bar-item')
  })
  onDestroy(() => off())
</script>

{#if surfaces.length > 0 || trailing}
  <div
    class="flex items-center justify-between gap-1 px-3 py-1 bg-surface-panel border-t border-surface-panel-border h-6 select-none w-full flex-shrink-0 z-50"
  >
    <div class="flex items-center gap-1 min-w-0">
      {#each surfaces as surface (surface.id)}
        <button
          onclick={() => surface.onClick?.()}
          class="flex items-center gap-1 px-1.5 py-0.5 rounded text-text-muted hover:text-accent-primary-start hover:bg-hover transition-colors text-type-2xs font-label-sm border border-transparent hover:border-surface-panel-border cursor-default"
          title={surface.label}
          aria-label={surface.label}
        >
          {#if surface.icon}
            <span class="material-symbols-outlined text-icon-sm"
              >{surface.icon}</span
            >
          {/if}
          <span class="truncate max-w-20">{surface.label}</span>
        </button>
      {/each}
    </div>
    {@render trailing?.()}
  </div>
{/if}
