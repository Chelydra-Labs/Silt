<script lang="ts">
  import { onDestroy } from 'svelte'
  import { fly } from 'svelte/transition'
  import { makePluginContext } from '../../context'
  import { getSessionToken } from '../../loader'
  import ChatShell from './ChatShell.svelte'
  import { createAIChatController } from './ai-chat-controller.svelte'
  import {
    aiChatDrawer,
    closeAIChatDrawer,
    openAIChatDrawer
  } from './drawer.svelte'
  import { AI_CHAT_COMMAND_EVENT, type AIChatCommandDetail } from './commands'
  import type { EvidenceTarget } from './types'

  const PLUGIN_ID = 'silt-ai-agent'
  const chat = createAIChatController()

  let open = $derived(aiChatDrawer.open)
  let queuedCommand = $state<AIChatCommandDetail | null>(null)
  let ctx = $derived.by(() => {
    if (!open) return null
    return makePluginContext(PLUGIN_ID, getSessionToken(PLUGIN_ID) ?? undefined)
  })

  $effect(() => {
    if (ctx) chat.attach(ctx)
  })

  function onAIChatCommand(event: Event) {
    const detail = (event as CustomEvent<AIChatCommandDetail>).detail
    if (!detail?.text) return
    queuedCommand = detail
    openAIChatDrawer()
  }

  $effect(() => {
    window.addEventListener(AI_CHAT_COMMAND_EVENT, onAIChatCommand)
    return () =>
      window.removeEventListener(AI_CHAT_COMMAND_EVENT, onAIChatCommand)
  })

  $effect(() => {
    if (!ctx || !queuedCommand) return
    const command = queuedCommand
    queuedCommand = null
    void chat.send(command.text, command.request)
  })

  function onWindowKeydown(event: KeyboardEvent) {
    if (event.key !== 'Escape' || !aiChatDrawer.open) return
    event.preventDefault()
    closeAIChatDrawer()
  }

  $effect(() => {
    if (!open) return
    window.addEventListener('keydown', onWindowKeydown)
    return () => window.removeEventListener('keydown', onWindowKeydown)
  })

  onDestroy(() => chat.dispose())

  function navigateEvidence(target: EvidenceTarget) {
    window.dispatchEvent(
      new CustomEvent('navigate-to-block', {
        detail: { blockId: target.blockId }
      })
    )
  }
</script>

{#if open && ctx}
  <aside
    transition:fly={{ x: 28, duration: 180 }}
    class="ai-chat-drawer"
    aria-label="Silt AI"
  >
    {#snippet drawerActions()}
      <button
        type="button"
        class="close-button"
        aria-label="Close Silt AI"
        onclick={closeAIChatDrawer}
      >
        <span class="material-symbols-outlined" aria-hidden="true">close</span>
      </button>
    {/snippet}

    <ChatShell
      title="Silt AI"
      transcript={chat.transcript}
      busy={chat.busy}
      providerReady={chat.providerReady}
      actions={drawerActions}
      onSend={(text) => chat.send(text)}
      onStop={chat.stop}
      onAcceptProposal={(id) => void chat.acceptProposal(id)}
      onDiscardProposal={(id) => void chat.discardProposal(id)}
      onConfirmStaging={(token) => chat.resolveStaging(token, true)}
      onRejectStaging={(token) => chat.resolveStaging(token, false)}
      onOpenSettings={() => ctx.openSettings('ai')}
      onNavigateEvidence={navigateEvidence}
      onClear={chat.clear}
    />
  </aside>
{/if}

<style>
  .ai-chat-drawer {
    flex: 0 0 380px;
    width: 380px;
    max-width: min(380px, 42vw);
    height: 100%;
    min-height: 0;
    border-left: 1px solid var(--color-surface-panel-border);
    background: var(--color-surface-panel);
    display: flex;
    flex-direction: column;
    overflow: hidden;
    z-index: 20;
  }

  .close-button {
    display: grid;
    place-items: center;
    padding: 0.25rem;
    border: 0;
    color: var(--color-text-muted);
    background: transparent;
    cursor: pointer;
  }

  .close-button:hover {
    color: var(--color-text-primary);
  }

  .close-button:focus-visible {
    outline: 2px solid var(--color-border-focus);
    outline-offset: 1px;
  }

  @media (max-width: 900px) {
    .ai-chat-drawer {
      position: fixed;
      inset: 0 0 0 auto;
      max-width: min(380px, 92vw);
      box-shadow: -8px 0 32px var(--color-surface-app);
      z-index: 50;
    }
  }
</style>
