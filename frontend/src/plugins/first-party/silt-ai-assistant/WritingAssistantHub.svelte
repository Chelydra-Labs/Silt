<script lang="ts">
  // Main plugin view when opened from Plugins / activity (informational hub).
  import type { PluginContext, PluginManifest } from '../../sdk'
  import { openWritingAssistantDrawer } from './drawer.svelte'
  import { enabledActions } from './catalog'
  import { getAssistantController } from './state.svelte'

  interface Props {
    ctx: PluginContext
    manifest?: PluginManifest
  }
  let { ctx, manifest }: Props = $props()
  const ctl = $derived(getAssistantController())
  const actions = $derived(ctl ? enabledActions(ctl.settings) : [])
</script>

<div class="wa-hub p-8 max-w-2xl">
  <h1 class="text-text-primary text-type-2xl font-bold mb-2">
    {manifest?.name ?? 'Writing Assistant'}
  </h1>
  <p class="text-text-muted mb-6 leading-relaxed">
    {manifest?.description ??
      'Curated AI writing actions with accept/reject — never unsolicited writes.'}
  </p>

  <button
    type="button"
    class="mb-6 px-4 py-2 rounded-lg bg-accent-primary-start text-surface-app font-semibold"
    onclick={() => openWritingAssistantDrawer()}
  >
    Open assistant panel
  </button>

  <h2 class="text-text-primary font-semibold mb-2">Enabled actions</h2>
  <ul class="space-y-2 text-text-muted text-type-md">
    {#each actions as a (a.id)}
      <li>
        <strong class="text-text-primary">{a.label}</strong> — {a.description}
        <span class="text-text-disabled">(/ {a.slashLabel})</span>
      </li>
    {/each}
  </ul>

  <p class="mt-6 text-text-muted text-type-sm">
    Configure models under
    <button
      type="button"
      class="underline text-accent-primary-start"
      onclick={() => ctx.openSettings('ai')}>AI Provider</button
    >
    and toggles under
    <button
      type="button"
      class="underline text-accent-primary-start"
      onclick={() => ctx.openSettings('plugin:silt-ai-assistant')}
      >Writing Assistant settings</button
    >.
  </p>
</div>
