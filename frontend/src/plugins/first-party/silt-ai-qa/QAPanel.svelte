<script lang="ts">
  // Q&A sidebar panel (#227) — first-party sidebar-panel surface component.
  import type { PluginContext } from '../../sdk'
  import { getQAController } from './state.svelte'

  interface Props {
    ctx: PluginContext
  }
  let { ctx }: Props = $props()

  const ctl = $derived(getQAController())
  let question = $state('')
  let inputEl: HTMLTextAreaElement | null = $state(null)

  async function onAsk() {
    if (!ctl || !question.trim()) return
    const q = question
    question = ''
    await ctl.ask(ctx, q)
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      void onAsk()
    }
  }

  function navigateTo(blockId: string) {
    window.dispatchEvent(
      new CustomEvent('navigate-to-block', {
        detail: { blockId }
      })
    )
  }

  function statusLabel(): string {
    if (!ctl) return ''
    switch (ctl.panelStatus) {
      case 'asking':
        return 'Retrieving…'
      case 'streaming':
        return 'Answering…'
      case 'no-results':
        return 'No relevant notes'
      case 'no-chat-provider':
        return 'Chat model not configured'
      case 'no-embedding-provider':
        return 'Embedding model not configured'
      case 'error':
        return ctl.errorMessage || 'Error'
      case 'indexing':
        return 'Index building…'
      default:
        return ''
    }
  }

  const busy = $derived(
    ctl?.panelStatus === 'asking' || ctl?.panelStatus === 'streaming'
  )
</script>

<div class="qa-panel" role="region" aria-label="AI Q&A">
  <header class="qa-header">
    <span class="material-symbols-outlined" aria-hidden="true">forum</span>
    <h2>Ask your notes</h2>
    <button
      type="button"
      class="qa-clear"
      onclick={() => ctl?.clear()}
      disabled={!ctl || ctl.messages.length === 0}
      title="Clear conversation"
    >
      Clear
    </button>
  </header>

  {#if ctl?.progress?.status === 'indexing'}
    <div class="qa-banner info" role="status">
      Indexing… {ctl.progress.done}/{ctl.progress.total}
      {ctl.progress.message ? ` — ${ctl.progress.message}` : ''}
    </div>
  {:else if ctl?.progress?.status === 'unconfigured' || (ctl && !ctl.embedReady())}
    <div class="qa-banner warn" role="status">
      Configure an embedding model in Settings → AI Provider, then rebuild the
      index.
    </div>
  {:else if ctl && !ctl.chatReady()}
    <div class="qa-banner warn" role="status">
      Configure a chat model in Settings → AI Provider to ask questions.
    </div>
  {/if}

  <div class="qa-messages" aria-live="polite" aria-relevant="additions text">
    {#if ctl}
      {#each ctl.messages as msg, i (i)}
        <div class="qa-msg" data-role={msg.role}>
          <div class="qa-msg-role">
            {msg.role === 'user' ? 'You' : 'Assistant'}
          </div>
          <div class="qa-msg-body">{msg.content}</div>
          {#if msg.citations && msg.citations.length > 0}
            <ul class="qa-cites">
              {#each msg.citations as c (c.blockId + c.index)}
                <li>
                  <button
                    type="button"
                    class="qa-cite"
                    title={c.snippet}
                    onclick={() => navigateTo(c.blockId)}
                    onkeydown={(e) => {
                      if (e.key === 'Enter' || e.key === ' ') {
                        e.preventDefault()
                        navigateTo(c.blockId)
                      }
                    }}
                  >
                    [{c.index}] {c.notebook}/{c.page}
                  </button>
                </li>
              {/each}
            </ul>
          {/if}
        </div>
      {/each}
    {/if}
    {#if statusLabel()}
      <div class="qa-status" role="status">{statusLabel()}</div>
    {/if}
  </div>

  {#if ctl && ctl.citations.length > 0 && ctl.panelStatus === 'idle'}
    <div class="qa-sources">
      <div class="qa-sources-label">Sources</div>
      <ul>
        {#each ctl.citations as c (c.blockId + '-src')}
          <li>
            <button
              type="button"
              class="qa-cite"
              title={c.snippet}
              onclick={() => navigateTo(c.blockId)}
            >
              [{c.index}] {c.notebook}/{c.section}/{c.page}
            </button>
          </li>
        {/each}
      </ul>
    </div>
  {/if}

  <div class="qa-input-row">
    <label class="sr-only" for="qa-input">Question</label>
    <textarea
      id="qa-input"
      bind:this={inputEl}
      bind:value={question}
      rows="2"
      placeholder="What did I decide about…?"
      onkeydown={onKeydown}
      disabled={busy}></textarea>
    {#if busy}
      <button type="button" class="qa-btn stop" onclick={() => ctl?.stop()}>
        Stop
      </button>
    {:else}
      <button
        type="button"
        class="qa-btn ask"
        onclick={() => void onAsk()}
        disabled={!question.trim()}
      >
        Ask
      </button>
    {/if}
  </div>
</div>

<style>
  .qa-panel {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 12rem;
    background: var(--surface-panel, #1a1a1e);
    color: var(--surface-sidebar-text, #e8e8ec);
    font-size: 0.8125rem;
  }
  .qa-header {
    display: flex;
    align-items: center;
    gap: 0.4rem;
    padding: 0.5rem 0.6rem;
    border-bottom: 1px solid var(--surface-panel-border, #2a2a30);
  }
  .qa-header h2 {
    margin: 0;
    font-size: 0.8125rem;
    font-weight: 600;
    flex: 1;
  }
  .qa-clear {
    background: transparent;
    border: none;
    color: var(--surface-sidebar-text-muted, #9a9aa3);
    cursor: pointer;
    font-size: 0.75rem;
  }
  .qa-clear:disabled {
    opacity: 0.4;
    cursor: default;
  }
  .qa-banner {
    padding: 0.4rem 0.6rem;
    font-size: 0.75rem;
  }
  .qa-banner.warn {
    background: color-mix(in srgb, #f59e0b 15%, transparent);
  }
  .qa-banner.info {
    background: color-mix(in srgb, #3b82f6 15%, transparent);
  }
  .qa-messages {
    flex: 1;
    overflow-y: auto;
    padding: 0.5rem 0.6rem;
    display: flex;
    flex-direction: column;
    gap: 0.6rem;
  }
  .qa-msg[data-role='user'] .qa-msg-body {
    opacity: 0.9;
  }
  .qa-msg-role {
    font-size: 0.65rem;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    color: var(--surface-sidebar-text-muted, #9a9aa3);
    margin-bottom: 0.15rem;
  }
  .qa-msg-body {
    white-space: pre-wrap;
    line-height: 1.4;
  }
  .qa-cites,
  .qa-sources ul {
    list-style: none;
    margin: 0.35rem 0 0;
    padding: 0;
    display: flex;
    flex-wrap: wrap;
    gap: 0.25rem;
  }
  .qa-cite {
    border: 1px solid var(--surface-panel-border, #2a2a30);
    background: color-mix(
      in srgb,
      var(--accent-primary-start, #6366f1) 12%,
      transparent
    );
    color: inherit;
    border-radius: 999px;
    padding: 0.1rem 0.45rem;
    font-size: 0.7rem;
    cursor: pointer;
  }
  .qa-cite:hover,
  .qa-cite:focus-visible {
    outline: 2px solid var(--accent-primary-start, #6366f1);
    outline-offset: 1px;
  }
  .qa-sources {
    padding: 0.35rem 0.6rem;
    border-top: 1px solid var(--surface-panel-border, #2a2a30);
  }
  .qa-sources-label {
    font-size: 0.65rem;
    text-transform: uppercase;
    color: var(--surface-sidebar-text-muted, #9a9aa3);
    margin-bottom: 0.25rem;
  }
  .qa-status {
    font-size: 0.75rem;
    color: var(--surface-sidebar-text-muted, #9a9aa3);
    font-style: italic;
  }
  .qa-input-row {
    display: flex;
    gap: 0.35rem;
    padding: 0.5rem 0.6rem;
    border-top: 1px solid var(--surface-panel-border, #2a2a30);
  }
  .qa-input-row textarea {
    flex: 1;
    resize: none;
    border-radius: 0.4rem;
    border: 1px solid var(--surface-panel-border, #2a2a30);
    background: var(--surface-input, #121216);
    color: inherit;
    padding: 0.35rem 0.5rem;
    font: inherit;
  }
  .qa-btn {
    border: none;
    border-radius: 0.4rem;
    padding: 0 0.75rem;
    font: inherit;
    font-weight: 600;
    cursor: pointer;
  }
  .qa-btn.ask {
    background: var(--accent-primary-start, #6366f1);
    color: #fff;
  }
  .qa-btn.ask:disabled {
    opacity: 0.4;
    cursor: default;
  }
  .qa-btn.stop {
    background: #7f1d1d;
    color: #fff;
  }
  .sr-only {
    position: absolute;
    width: 1px;
    height: 1px;
    padding: 0;
    margin: -1px;
    overflow: hidden;
    clip: rect(0, 0, 0, 0);
    border: 0;
  }
</style>
