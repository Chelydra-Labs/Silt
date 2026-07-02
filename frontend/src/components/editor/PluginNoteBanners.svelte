<script lang="ts">
  // PluginNoteBanners — host for the 'note-banner' surface kind (#215).
  // Renders registered banners at the top of the note view, above the TipTap
  // editor content, in registration order. Mirrors FormattingFirstRunTip's
  // theming + dismissal UX (role="status", aria-live, accessible close).
  // Third-party banners render via PluginSurfaceFrame (sandboxed iframe).
  import { onDestroy } from 'svelte'
  import {
    getSurfaces,
    onSurfacesChanged,
    unregisterSurface,
    type PluginSurface
  } from '../../plugins/surfaces'
  import PluginSurfaceFrame from '../PluginSurfaceFrame.svelte'
  import { makePluginContext } from '../../plugins/context'

  let surfaces = $state<PluginSurface[]>(getSurfaces('note-banner'))

  const off = onSurfacesChanged((all) => {
    surfaces = all.filter((s) => s.kind === 'note-banner')
  })

  onDestroy(() => off())

  // Dismiss a banner. The surface is removed from the registry immediately so
  // the banner disappears; PERSISTENT dismissal state is the plugin's
  // responsibility (recommended: updatePluginSetting('<id>', 'dismissed_notes',
  // [...])). The close button's accessible name is derived from the banner
  // label so a screen reader announces "Dismiss Summary" etc.
  function dismiss(surface: PluginSurface) {
    unregisterSurface(surface.id)
  }
</script>

{#if surfaces.length > 0}
  <!-- Stacking: predictable order (registration order), max-height + overflow
       so several banners coexist without pushing the editor out of view. -->
  <div
    class="plugin-note-banners"
    role="region"
    aria-label="Plugin banners"
    style="max-height: 30vh; overflow-y: auto;"
  >
    {#each surfaces as surface (surface.id)}
      <div
        class="note-banner"
        role="status"
        aria-live="polite"
        aria-label={surface.label}
      >
        <span class="material-symbols-outlined banner-icon" aria-hidden="true"
          >{surface.icon || 'campaign'}</span
        >
        <div class="banner-frame-wrapper">
          <PluginSurfaceFrame
            {surface}
            ctxProxy={makePluginContext(surface.pluginID) as any}
          />
        </div>
        <button
          type="button"
          class="banner-dismiss"
          onclick={() => dismiss(surface)}
          aria-label="Dismiss {surface.label}"
          title="Dismiss"
        >
          <span class="material-symbols-outlined" aria-hidden="true">close</span
          >
        </button>
      </div>
    {/each}
  </div>
{/if}

<style>
  .plugin-note-banners {
    display: flex;
    flex-direction: column;
    gap: 4px;
    margin-bottom: 4px;
  }

  .note-banner {
    display: flex;
    align-items: stretch;
    gap: 6px;
    padding: 6px 10px;
    border-radius: 8px;
    background: color-mix(
      in srgb,
      var(--color-accent-primary-glow, #6fa3ff) 10%,
      var(--color-surface, #1a1d24)
    );
    border: 1px solid
      color-mix(
        in srgb,
        var(--color-accent-primary-glow, #6fa3ff) 25%,
        transparent
      );
  }

  .banner-icon {
    font-size: 18px;
    color: var(--color-accent-primary-glow, #6fa3ff);
    flex-shrink: 0;
    align-self: flex-start;
    margin-top: 2px;
  }

  .banner-frame-wrapper {
    flex: 1;
    min-width: 0;
    /* The iframe content is sandboxed; constrain its height so it doesn't
       blow out the banner's compact layout. */
    max-height: 120px;
    overflow: hidden;
    border-radius: 4px;
  }

  .banner-dismiss {
    flex-shrink: 0;
    align-self: flex-start;
    margin-top: 2px;
    padding: 2px;
    border: none;
    border-radius: 6px;
    background: transparent;
    color: var(--color-text-muted, #8b95a3);
    cursor: pointer;
    transition:
      background 0.1s,
      color 0.1s;
    line-height: 0;
  }

  .banner-dismiss:hover {
    background: color-mix(
      in srgb,
      var(--color-accent-primary-start, #4f7cff) 15%,
      transparent
    );
    color: var(--color-text-primary, #e6e6e6);
  }

  .banner-dismiss .material-symbols-outlined {
    font-size: 18px;
  }
</style>
