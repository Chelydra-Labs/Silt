<script lang="ts">
  // Host for plugin-rendered modal surfaces (#117). Subscribes to the surface
  // manager; when a modal surface is registered it renders in a focus-trapped
  // overlay. Closing the modal is via Esc / backdrop click which unregisters it.
  import { onDestroy, onMount } from 'svelte'
  import {
    getSurfaces,
    onSurfacesChanged,
    unregisterSurface,
    type PluginSurface
  } from '../plugins/surfaces'
  import PluginSurfaceFrame from './PluginSurfaceFrame.svelte'
  import { makePluginContext } from '../plugins/context'

  let surfaces = $state<PluginSurface[]>(getSurfaces('modal'))
  const off = onSurfacesChanged((all) => {
    surfaces = all.filter((s) => s.kind === 'modal')
  })
  onDestroy(() => off())

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape' && surfaces.length > 0) {
      unregisterSurface(surfaces[0].id)
    }
  }

  onMount(() => {
    window.addEventListener('keydown', handleKeydown)
    return () => window.removeEventListener('keydown', handleKeydown)
  })
</script>

{#if surfaces.length > 0}
  {#each surfaces as surface (surface.id)}
    <div
      class="fixed inset-0 bg-black/40 backdrop-blur-[2px] z-[200] flex items-center justify-center"
    >
      <button
        tabindex="-1"
        aria-label="Close"
        onclick={() => unregisterSurface(surface.id)}
        class="absolute inset-0 cursor-default border-none bg-transparent p-0"
      ></button>
      <div
        role="dialog"
        aria-modal="true"
        aria-label={surface.label}
        class="relative bg-panel border border-border-active rounded-xl shadow-2xl max-w-lg w-full mx-4 overflow-hidden"
        style="min-height: 200px; max-height: 80vh;"
      >
        <div
          class="flex items-center justify-between px-4 py-2 border-b border-border-muted"
        >
          <span class="text-text-primary text-[13px] font-label-sm-bold"
            >{surface.label}</span
          >
          <button
            onclick={() => unregisterSurface(surface.id)}
            aria-label="Close modal"
            class="text-text-muted hover:text-text-primary border-none bg-transparent cursor-pointer p-1 rounded"
          >
            <span class="material-symbols-outlined text-[18px]">close</span>
          </button>
        </div>
        <div class="h-[60vh]">
          <PluginSurfaceFrame
            {surface}
            ctxProxy={makePluginContext(surface.pluginID) as any}
          />
        </div>
      </div>
    </div>
  {/each}
{/if}
