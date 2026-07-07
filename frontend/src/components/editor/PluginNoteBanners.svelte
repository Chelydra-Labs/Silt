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

  // Cache contexts per pluginID so a surfaces-list change doesn't rebuild
  // the context for every banner on every render (avoids needless iframe
  // srcdoc rebuilds in PluginSurfaceFrame). Invalidated for pluginIDs that
  // leave the surfaces list (disable/enable issues a fresh session token).
  const ctxCache = new Map<string, any>()

  // host→iframe post closures per surface.id, handed back by each
  // PluginSurfaceFrame via onBridgeReady (#355). Used to notify a plugin its
  // banner was dismissed so it can persist dismissal state
  // (ctx.updatePluginSetting('dismissed_notes', [...])) BEFORE the surface is
  // torn down. Entries are dropped when a surface leaves the list.
  const postFns = new Map<string, (msg: any) => void>()

  const off = onSurfacesChanged((all) => {
    surfaces = all.filter((s) => s.kind === 'note-banner')
    // Evict cached contexts + post closures for surfaces no longer present —
    // their session tokens are revoked on teardown, so a stale ctx would fail
    // server-side, and a stale post closure would target a torn-down iframe.
    const activeIDs = new Set(surfaces.map((s) => s.id))
    for (const id of [...postFns.keys()]) {
      if (!activeIDs.has(id)) postFns.delete(id)
    }
    const activePluginIDs = new Set(surfaces.map((s) => s.pluginID))
    for (const id of ctxCache.keys()) {
      if (!activePluginIDs.has(id)) ctxCache.delete(id)
    }
  })

  onDestroy(() => off())

  // Pending dismiss grace-timer (window.setTimeout handle). Tracked so onDestroy
  // can clear it: if the note view unmounts within the grace window (switch
  // note, close app) the timeout must not fire doRemove against torn-down DOM.
  // Single-surface guard — only one dismiss can be in flight at a time (the
  // dismissedThisTick debounce) so one handle is sufficient.
  let dismissTimer: number | null = null

  function ctxFor(pluginID: string): any {
    let ctx = ctxCache.get(pluginID)
    if (!ctx) {
      ctx = makePluginContext(pluginID) as any
      ctxCache.set(pluginID, ctx)
    }
    return ctx
  }

  // Dismiss a banner. Before removing the surface we send a host→iframe
  // 'dismiss' event (#355) so the plugin can persist its dismissal state
  // (recommended: ctx.updatePluginSetting('dismissed_notes', [...])).
  // updatePluginSetting is now in the surface bridge's allowedMethods, so the
  // documented pattern is finally reachable. `persistent` is false for the
  // default close ("Dismiss for now"); a plugin may treat the event however it
  // likes (the protocol carries the flag for future "Don't show again" UI).
  //
  // A 400ms timeout fallback guarantees the surface is removed even if a
  // plugin's dismiss handler hangs — no banner can wedge the host.
  //
  // Focus management (#215 a11y): the close button lives inside the banner, so
  // removing the banner destroys the focused element. Before removal, move
  // focus to the next banner's close button (or, if none, to the container so
  // focus doesn't fall to <body>).
  const DISMISS_TIMEOUT_MS = 400
  let dismissedThisTick: string | null = null

  // onDismissFor(surface) returns the closure a first-party component receives
  // as its `onDismiss` prop. It converges on the same dismiss path the host
  // chrome close button uses, so a component that renders its own "Got it"
  // affordance and the host close button both produce the same teardown +
  // persistence. The iframe path does not use this (it gets a host→iframe
  // 'dismiss' postMessage instead).
  function onDismissFor(surface: PluginSurface): () => void {
    return () => dismiss(surface)
  }

  function dismiss(surface: PluginSurface) {
    if (dismissedThisTick === surface.id) return // idempotent on double-click
    dismissedThisTick = surface.id

    // First-party components dismiss synchronously (no iframe to signal); the
    // iframe path still gets the host→plugin 'dismiss' postMessage + grace
    // window so its updatePluginSetting call can land before teardown.
    if (!surface.component) {
      const post = postFns.get(surface.id)
      try {
        post?.({
          __siltSurface: 'event',
          type: 'dismiss',
          payload: { surfaceId: surface.id, persistent: false }
        })
      } catch {
        /* best-effort notify — teardown below is the guarantee */
      }
    }

    const doRemove = () => {
      dismissTimer = null
      // Reset the debounce guard so a plugin re-enabled and re-registered with
      // the same surface.id can be dismissed again (the guard is only meant to
      // debounce a single click during the grace window).
      if (dismissedThisTick === surface.id) dismissedThisTick = null
      const idx = surfaces.findIndex((s) => s.id === surface.id)
      const next = surfaces[idx + 1]
      unregisterSurface(surface.id)
      // Defer so the DOM updates before we focus.
      queueMicrotask(() => {
        if (next) {
          const nextBtn = document.querySelector<HTMLButtonElement>(
            `[data-banner-close="${next.id}"]`
          )
          nextBtn?.focus()
        } else {
          // No more banners — return focus to the container (Tab will move
          // into the editor on the next press).
          containerEl?.focus()
        }
      })
    }

    if (surface.component) {
      // First-party: no grace window needed (the component is in-process and
      // its persistence runs synchronously via onDismiss before this call).
      doRemove()
      return
    }
    // Third-party: give the plugin a chance to persist, but never hang the host.
    dismissTimer = window.setTimeout(doRemove, DISMISS_TIMEOUT_MS)
  }

  onDestroy(() => {
    off()
    if (dismissTimer) {
      window.clearTimeout(dismissTimer)
      dismissTimer = null
    }
  })

  let containerEl: HTMLDivElement | null = $state(null)

  // Collapse affordance (#358): when more than 2 banners stack, the default
  // collapses them into a single summary to avoid pushing the editor down.
  // The user expands to see all; dismissing one while expanded drops back
  // below the threshold automatically.
  const COLLAPSE_THRESHOLD = 2
  let collapsed = $state(true)
  let showCollapse = $derived(surfaces.length > COLLAPSE_THRESHOLD)
  // Visible banners: none while collapsed (the summary takes their place);
  // all of them when expanded or when under the threshold.
  let visibleSurfaces = $derived(showCollapse && collapsed ? [] : surfaces)
</script>

{#if surfaces.length > 0}
  <!-- Stacking: predictable order (registration order), max-height + overflow
       so several banners coexist without pushing the editor out of view. The
       custom-scrollbar class styles the overflow per the app convention. -->
  <div
    bind:this={containerEl}
    class="plugin-note-banners custom-scrollbar"
    role="region"
    aria-label="Plugin banners"
    tabindex="-1"
  >
    {#if showCollapse}
      <button
        type="button"
        class="banner-collapse-toggle"
        aria-expanded={!collapsed}
        aria-controls="banner-stack"
        onclick={() => (collapsed = !collapsed)}
      >
        <span class="material-symbols-outlined" aria-hidden="true"
          >{collapsed ? 'expand' : 'compress'}</span
        >
        {surfaces.length} plugin {surfaces.length === 1 ? 'banner' : 'banners'} —
        {collapsed ? 'show' : 'hide'}
      </button>
    {/if}

    {#if showCollapse && collapsed}
      <!-- Collapsed state removes the per-banner role=status live regions, so a
           screen reader would not learn when a new banner arrives (the toggle
           button text change is not itself announced). This visually-hidden
           polite region announces the count + the latest label so arrivals and
           departures are spoken without a visible affordance. -->
      <div class="sr-only" aria-live="polite" aria-atomic="true">
        {surfaces.length} plugin {surfaces.length === 1 ? 'banner' : 'banners'} active.
        Latest: {surfaces[surfaces.length - 1]?.label ?? 'unknown'}.
      </div>
    {/if}

    <div id="banner-stack" class="banner-stack">
      {#each visibleSurfaces as surface (surface.id)}
        {#if surface.component}
          {@const Banner = surface.component}
          {@const extra = surface.props ?? {}}
          <!-- First-party direct-render path (#221): a compiled Svelte component
               mounted in the host webview. The component owns its complete
               chrome — role=region, aria-live, icon, content, regenerate, AND a
               close button carrying data-banner-close (so the host's
               cross-banner focus management still works). The host supplies ctx
               + onDismiss; the component persists its own dismissal state before
               calling onDismiss (the iframe path can't do this in-process, which
               is why third-party banners still use the host chrome below). -->
          <Banner {...extra} ctx={ctxFor(surface.pluginID)} onDismiss={onDismissFor(surface)} />
        {:else}
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
                ctxProxy={ctxFor(surface.pluginID)}
                onBridgeReady={(post) => postFns.set(surface.id, post)}
              />
            </div>
            <button
              type="button"
              class="banner-dismiss"
              data-banner-close={surface.id}
              onclick={() => dismiss(surface)}
              aria-label="Dismiss {surface.label}"
              title="Dismiss {surface.label}"
            >
              <span class="material-symbols-outlined" aria-hidden="true"
                >close</span
              >
            </button>
          </div>
        {/if}
      {/each}
    </div>
  </div>
{/if}

<style>
  /* Visually hidden but available to assistive tech (the collapsed-stack live
     region). Standard visually-hidden pattern; not globalized because no other
     component needs it yet. */
  .sr-only {
    position: absolute;
    width: 1px;
    height: 1px;
    padding: 0;
    margin: -1px;
    overflow: hidden;
    clip: rect(0, 0, 0, 0);
    white-space: nowrap;
    border: 0;
  }

  .plugin-note-banners {
    display: flex;
    flex-direction: column;
    gap: 4px;
    margin-bottom: 4px;
    max-height: 30vh;
    overflow-y: auto;
  }

  .banner-stack {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  /* Banner chrome theming is aligned with FormattingFirstRunTip (12% / 30%
     accent-glow mixes) so dismissible highlight regions share a look. The
     ratios live here as the single source for the note-banner variant. */
  .note-banner {
    display: flex;
    align-items: stretch;
    gap: 6px;
    padding: 6px 10px;
    border-radius: 8px;
    background: color-mix(
      in srgb,
      var(--color-accent-primary-glow) 12%,
      var(--color-surface-card)
    );
    border: 1px solid
      color-mix(in srgb, var(--color-accent-primary-glow) 30%, transparent);
  }

  .banner-collapse-toggle {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 6px 10px;
    border-radius: 8px;
    border: 1px solid
      color-mix(in srgb, var(--color-accent-primary-glow) 30%, transparent);
    background: color-mix(
      in srgb,
      var(--color-accent-primary-glow) 12%,
      var(--color-surface-card)
    );
    color: var(--color-text-primary);
    font-size: 12px;
    cursor: pointer;
    transition:
      background 0.1s,
      color 0.1s;
  }

  .banner-collapse-toggle:hover {
    background: color-mix(
      in srgb,
      var(--color-accent-primary-glow) 18%,
      var(--color-surface-card)
    );
  }

  .banner-collapse-toggle .material-symbols-outlined {
    font-size: 16px;
  }

  .banner-icon {
    font-size: 18px;
    color: var(--color-accent-primary-glow);
    flex-shrink: 0;
    align-self: flex-start;
    margin-top: 2px;
  }

  .banner-frame-wrapper {
    flex: 1;
    min-width: 0;
    /* The iframe content is sandboxed; constrain its height so it doesn't
       blow out the banner's compact layout. Truncated banner text is
       scrollable (hidden auto) rather than silently clipped (#358). */
    max-height: 120px;
    overflow: hidden auto;
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
    color: var(--color-text-muted);
    cursor: pointer;
    transition:
      background 0.1s,
      color 0.1s;
    line-height: 0;
  }

  .banner-dismiss:hover {
    background: color-mix(
      in srgb,
      var(--color-accent-primary-start) 15%,
      transparent
    );
    color: var(--color-text-primary);
  }

  .banner-dismiss .material-symbols-outlined {
    font-size: 18px;
  }
</style>
