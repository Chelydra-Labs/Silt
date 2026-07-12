<script lang="ts">
  import { fly } from 'svelte/transition'
  import { makePluginContext } from '../../context'
  import { getSessionToken } from '../../loader'
  import {
    writingAssistantDrawer,
    closeWritingAssistantDrawer
  } from './drawer.svelte'
  import { getAssistantController } from './state.svelte'
  import AssistantPanel from './AssistantPanel.svelte'

  const PLUGIN_ID = 'silt-ai-assistant'

  let open = $derived(writingAssistantDrawer.open)
  let hasController = $derived(!!getAssistantController())
  let ctx = $derived.by(() => {
    if (!open || !hasController) return null
    return makePluginContext(PLUGIN_ID, getSessionToken(PLUGIN_ID) ?? undefined)
  })

  function onKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape' && writingAssistantDrawer.open) {
      e.preventDefault()
      closeWritingAssistantDrawer()
    }
  }

  $effect(() => {
    if (!open) return
    window.addEventListener('keydown', onKeydown)
    return () => window.removeEventListener('keydown', onKeydown)
  })
</script>

{#if open && ctx && hasController}
  <aside
    transition:fly={{ x: 28, duration: 180 }}
    class="wa-drawer"
    aria-label="Writing Assistant"
  >
    <AssistantPanel {ctx} onClose={closeWritingAssistantDrawer} />
  </aside>
{/if}

<style>
  .wa-drawer {
    flex: 0 0 380px;
    width: 380px;
    max-width: min(380px, 42vw);
    height: 100%;
    min-height: 0;
    border-left: 1px solid var(--color-surface-panel-border, #2a2a30);
    background: var(--color-surface-card, #1a1a1e);
    display: flex;
    flex-direction: column;
    overflow: hidden;
    z-index: 20;
  }

  @media (max-width: 900px) {
    .wa-drawer {
      position: fixed;
      right: 0;
      top: 0;
      bottom: 0;
      height: 100%;
      max-width: min(380px, 92vw);
      box-shadow: -8px 0 32px rgba(0, 0, 0, 0.35);
      z-index: 50;
    }
  }
</style>
