<script lang="ts">
  import { onDestroy, tick } from 'svelte'
  import { fly, fade } from 'svelte/transition'
  import { makePluginContext } from '../../context'
  import { getSessionToken } from '../../loader'
  import ChatShell from './ChatShell.svelte'
  import { createAIChatController } from './ai-chat-controller.svelte'
  import {
    aiChatDrawer,
    closeAIChatDrawer,
    openAIChatDrawer,
    registerAIChatController
  } from './drawer.svelte'
  import { AI_CHAT_COMMAND_EVENT, type AIChatCommandDetail } from './commands'
  import type { EvidenceTarget } from './types'
  import { getAIAvailability } from './availability'

  const PLUGIN_ID = 'silt-ai-agent'
  const chat = createAIChatController()
  // Register so drawer close / vault teardown can stop in-flight runs.
  registerAIChatController(chat)

  let open = $derived(aiChatDrawer.open)
  let aiOn = $derived(getAIAvailability().drawerAvailable)
  let queuedCommand = $state<AIChatCommandDetail | null>(null)
  let drawerEl = $state<HTMLElement | null>(null)
  let isMobile = $state(false)
  // Cache the plugin context per session token so closing and reopening the
  // drawer does NOT create a new context object. attach() treats a new object
  // as a vault switch and clears the transcript; caching keeps the same object
  // across open/close cycles (only a real token change — new vault session —
  // produces a new context and triggers the vault-switch clear).
  let cachedToken: string | undefined
  let cachedCtx: ReturnType<typeof makePluginContext> | null = null
  let sessionReady = $derived(!!getSessionToken(PLUGIN_ID))
  let ctx = $derived.by(() => {
    if (!open || !aiOn) return null
    const token = getSessionToken(PLUGIN_ID) ?? undefined
    // Master AI on but agent session not registered yet (reload mid-flight):
    // still open the shell so the empty-state / setup banner can show.
    if (!token && !sessionReady) {
      return null
    }
    if (cachedCtx && token === cachedToken) return cachedCtx
    cachedCtx = makePluginContext(PLUGIN_ID, token)
    cachedToken = token
    return cachedCtx
  })

  $effect(() => {
    if (ctx) chat.attach(ctx)
  })

  function onAIChatCommand(event: Event) {
    // Gate on master AI enablement + session (session may lag a feature flip
    // until the next vault reload).
    if (!getAIAvailability().drawerAvailable) return
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
    // Hold the queue until send can accept it. Consuming before send would
    // drop slash commands dispatched mid-run (send no-ops when busy).
    if (chat.busy || chat.pendingConfirmation || !chat.providerReady) return
    const command = queuedCommand
    queuedCommand = null
    void chat.send(command.text, command.request)
  })

  // Focus lifecycle: on open, remember the element that had focus (the
  // titlebar toggle) and move focus into the drawer; on close, restore it so
  // keyboard users are not stranded on a removed node.
  $effect(() => {
    if (!open) return
    const lastFocused =
      document.activeElement instanceof HTMLElement
        ? document.activeElement
        : null
    void tick().then(() => {
      const composer = document.getElementById(
        'ai-chat-composer'
      ) as HTMLTextAreaElement | null
      if (composer && !composer.disabled) {
        composer.focus()
      } else {
        drawerEl?.focus()
      }
    })
    return () => {
      if (lastFocused?.isConnected) lastFocused.focus()
    }
  })

  // The drawer behaves as a modal only on small viewports, where it covers the
  // application. Track the breakpoint so aria-modal + the backdrop apply only
  // then (desktop keeps it a non-modal side panel).
  $effect(() => {
    const mq = window.matchMedia('(max-width: 900px)')
    const sync = () => {
      isMobile = mq.matches
    }
    sync()
    mq.addEventListener('change', sync)
    return () => mq.removeEventListener('change', sync)
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

  onDestroy(() => {
    registerAIChatController(null)
    chat.dispose()
  })

  function navigateEvidence(target: EvidenceTarget) {
    window.dispatchEvent(
      new CustomEvent('navigate-to-block', {
        detail: { blockId: target.blockId }
      })
    )
  }

  // Mobile modal focus trap: keep Tab cycling inside the dialog so it cannot
  // escape into the app behind the scrim. (Desktop is a non-modal panel, so
  // the trap is gated on isMobile.) The confirmation card in ChatShell owns
  // its own trap and stops propagation, so the two compose.
  function onDrawerKeydown(event: KeyboardEvent) {
    if (!isMobile || event.key !== 'Tab' || !drawerEl) return
    const focusable = Array.from(
      drawerEl.querySelectorAll<HTMLElement>(
        'a[href], button:not([disabled]), textarea:not([disabled]), input:not([disabled]), select:not([disabled]), [tabindex]:not([tabindex="-1"])'
      )
    )
    if (focusable.length === 0) return
    const first = focusable[0]
    const last = focusable[focusable.length - 1]
    const active = document.activeElement
    if (event.shiftKey && (active === first || !drawerEl.contains(active))) {
      event.preventDefault()
      last.focus()
    } else if (
      !event.shiftKey &&
      (active === last || !drawerEl.contains(active))
    ) {
      event.preventDefault()
      first.focus()
    }
  }
</script>

{#if open && aiOn}
  {#if isMobile}
    <button
      type="button"
      class="ai-chat-scrim"
      tabindex="-1"
      aria-hidden="true"
      transition:fade={{ duration: 150 }}
      onclick={closeAIChatDrawer}
    ></button>
  {/if}
  <div
    id="silt-ai-drawer"
    transition:fly={{ x: 28, duration: 180 }}
    class="ai-chat-drawer"
    class:mobile={isMobile}
    role="dialog"
    aria-modal={isMobile || undefined}
    aria-label="Silt AI"
    tabindex="-1"
    bind:this={drawerEl}
    onkeydown={onDrawerKeydown}
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

    {#if ctx}
      <ChatShell
        title="Silt AI"
        transcript={chat.transcript}
        busy={chat.busy}
        lastOutcome={chat.lastOutcome}
        providerReady={chat.providerReady}
        actions={drawerActions}
        onSend={(text) => {
          void chat.send(text)
        }}
        onStop={chat.stop}
        onAcceptProposal={(id) => void chat.acceptProposal(id)}
        onDiscardProposal={(id) => void chat.discardProposal(id)}
        onConfirmStaging={(token) => chat.resolveStaging(token, true)}
        onRejectStaging={(token) => chat.resolveStaging(token, false)}
        onOpenSettings={() => {
          ctx?.openSettings('ai')
          closeAIChatDrawer()
        }}
        onNavigateEvidence={navigateEvidence}
        onClear={chat.clear}
      />
    {:else}
      <div class="session-missing" role="status">
        <p>
          AI is enabled, but the agent session is not ready yet. Reload the
          vault or open Settings → AI to finish setup.
        </p>
        <button
          type="button"
          class="close-button"
          aria-label="Close Silt AI"
          onclick={closeAIChatDrawer}
        >
          <span class="material-symbols-outlined" aria-hidden="true">close</span
          >
        </button>
      </div>
    {/if}
  </div>
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

  .ai-chat-scrim {
    position: fixed;
    inset: 0;
    z-index: 60;
    border: 0;
    padding: 0;
    background: color-mix(in srgb, var(--color-surface-app) 55%, transparent);
    cursor: default;
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

  .mobile {
    position: fixed;
    inset: 0 0 0 auto;
    max-width: min(380px, 92vw);
    box-shadow: -8px 0 32px var(--color-surface-app);
    z-index: 61;
  }

  .session-missing {
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
    padding: 1rem;
    color: var(--color-text-muted);
    font-size: 0.875rem;
  }
</style>
