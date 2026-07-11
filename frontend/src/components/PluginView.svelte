<script lang="ts">
  import { loadedPlugins } from '../plugins/store.svelte'
  import { makePluginContext } from '../plugins/context'
  import { getSessionToken } from '../plugins/loader'

  interface Props {
    pluginId: string
    activeNotebook: string
    activeSection: string
    activePage: string
    /**
     * Optional focus target forwarded to plugin components that recognize
     * it (#370 / #374 — silt-tasks uses this to scroll-and-highlight the
     * targeted block row when a search/tag/backlink jump lands on a
     * standalone task). Plugins that don't declare a `focusBlockId` prop
     * receive `undefined` and ignore it.
     */
    focusBlockId?: string
    /**
     * Monotonic counter (typically a timestamp) that bumps every time the
     * focus should be re-applied — even if `focusBlockId` is the same as
     * a previous focus. Mirrors the `searchTargetKey` pattern for normal
     * page jumps so the Tasks view's `$effect` re-fires on each
     * navigation. Empty string disables re-focus.
     */
    focusKey?: string
  }

  let {
    pluginId,
    activeNotebook,
    activeSection,
    activePage,
    focusBlockId,
    focusKey
  }: Props = $props()

  let plugin = $derived(loadedPlugins.plugins.get(pluginId))
  let loadError = $derived(loadedPlugins.errors.find((e) => e.id === pluginId))

  // The shared reactive context (#69). activeNotebook/Section/Page are live
  // getters backed by location.svelte.ts $state, so plugins that read them in
  // a reactive context re-render automatically on navigation. pluginId is
  // captured so getPluginSettings resolves this plugin's entry (#133).
  // svelte-ignore state_referenced_locally: pluginId is a stable prop
  // identifying which plugin this view renders; it does not change during
  // the component's lifetime, so capturing the initial value is correct.
  //
  // $derived (not module-const) so it re-runs when loadersReady flips back
  // to true after vault:closing's clear→re-register cycle. Without this the
  // const captured at mount could carry an empty session token and every
  // privileged SDK call would fail for the component's whole life (#326
  // item 5). The render branch gates on ctx being non-null so the plugin
  // suspends (renders nothing) during the vault-switch window.
  let ctx = $derived.by(() => {
    if (!loadedPlugins.loadersReady) return null // suspend during vault switch
    return makePluginContext(pluginId, getSessionToken(pluginId))
  })
</script>

{#if loadError}
  <div class="flex-1 p-8 flex flex-col select-none">
    <h1
      class="font-headline-lg text-headline-lg text-text-primary mb-2 capitalize"
    >
      {pluginId}
    </h1>
    <p class="text-error font-body-md">
      Plugin failed to load: {loadError.message}
    </p>
  </div>
{:else if !plugin}
  <div class="flex-1 p-8 flex flex-col select-none">
    <div class="flex items-center gap-3 mb-3">
      <span class="material-symbols-outlined text-text-muted text-icon-2xl"
        >extension_off</span
      >
      <div>
        <h1
          class="font-headline-lg text-headline-lg text-text-primary capitalize"
        >
          {pluginId}
        </h1>
        <p class="text-text-muted text-type-sm font-body-md">
          plugin not registered
        </p>
      </div>
    </div>
    <p class="text-text-muted font-body-md">
      This plugin slot is reserved for a future plugin. First-party plugins
      (Calendar, Kanban, Tasks) are bundled; install others via the plugin
      manager.
    </p>
  </div>
{:else if ctx}
  {@const Plugin = plugin.component}
  <Plugin {ctx} manifest={plugin.manifest} {focusBlockId} {focusKey} />
{/if}
