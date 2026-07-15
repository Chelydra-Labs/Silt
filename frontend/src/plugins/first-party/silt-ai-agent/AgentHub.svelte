<script lang="ts">
  // AI Agent chat surface (#596). Renders the message list (user prompts,
  // streamed assistant text, and transparent tool-call cards), an input box
  // with send/stop buttons, and keyboard handling (Enter sends, Escape
  // stops). Designed to mirror silt-ai-qa's QAPanel styling conventions.
  //
  // Phase 5 staging (#605): when a destructive op is awaiting confirmation,
  // StagingConfirm renders below the chat and the input is disabled until
  // the user resolves it.
  import type { PluginContext } from '../../sdk'
  import { getAgentController, type AgentMessage } from './state.svelte'
  import StagingConfirm from './StagingConfirm.svelte'

  interface Props {
    ctx?: PluginContext
  }
  let { ctx }: Props = $props()

  const ctl = $derived(getAgentController())
  let input = $state('')

  const RUNNING_PLACEHOLDER = 'Agent is working…'
  const IDLE_PLACEHOLDER = 'Ask the agent to search, read, or create notes…'

  async function onSend() {
    if (!ctl || !ctx || !input.trim() || ctl.running) return
    const text = input
    input = ''
    await ctl.send(ctx, text)
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      void onSend()
    } else if (e.key === 'Escape' && ctl?.running) {
      e.preventDefault()
      ctl.cancel()
    }
  }

  function onConfirmStaging(token: string) {
    ctl?.resolveStaging(token, true)
  }
  function onRejectStaging(token: string) {
    ctl?.resolveStaging(token, false)
  }

  function truncatedPreview(content: string): string {
    if (content.length <= 80) return content
    return content.slice(0, 80) + '…'
  }

  function argsPreview(args: Record<string, unknown>): string {
    try {
      const s = JSON.stringify(args)
      return truncatedPreview(s)
    } catch {
      return '(arguments)'
    }
  }

  // Collapsible tool-card state keyed by message id.
  let expanded = $state<Record<string, boolean>>({})
  function toggle(id: string) {
    expanded = { ...expanded, [id]: !expanded[id] }
  }

  function onToolCardKeydown(e: KeyboardEvent, id: string) {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault()
      toggle(id)
    }
  }
</script>

<div class="agent-panel" role="region" aria-label="AI Agent">
  <header class="agent-header">
    <span class="material-symbols-outlined" aria-hidden="true">smart_toy</span>
    <h2>AI Agent</h2>
  </header>

  <div
    class="agent-messages"
    role="log"
    aria-live="polite"
    aria-relevant="additions text"
  >
    {#if ctl && ctl.messages.length === 0}
      <div class="agent-empty">
        <p>
          Ask the agent to search, read, create, and organize notes in your
          vault. It shows each tool call as it works.
        </p>
      </div>
    {/if}
    {#if ctl}
      {#each ctl.messages as msg (msg.id)}
        {#if msg.role === 'user'}
          <div class="agent-msg user">
            <div class="agent-msg-role">You</div>
            <div class="agent-msg-body">{msg.content}</div>
          </div>
        {:else if msg.role === 'assistant'}
          <div class="agent-msg assistant">
            <div class="agent-msg-role">Agent</div>
            <div class="agent-msg-body">{msg.content}</div>
          </div>
        {:else if msg.toolCall}
          <div class="agent-tool-card">
            <button
              type="button"
              class="tool-summary"
              aria-expanded={expanded[msg.id] ?? false}
              aria-label={`Tool call: ${msg.toolCall.name}. ${truncatedPreview('')}`.trim()}
              onclick={() => toggle(msg.id)}
              onkeydown={(e) => onToolCardKeydown(e, msg.id)}
            >
              <span
                class="material-symbols-outlined tool-icon"
                aria-hidden="true">build</span
              >
              <span class="tool-name">{msg.toolCall.name}</span>
              <span class="tool-args">{argsPreview(msg.toolCall.args)}</span>
              <span
                class="material-symbols-outlined chevron"
                aria-hidden="true"
              >
                {expanded[msg.id] ? 'expand_less' : 'expand_more'}
              </span>
            </button>
            {#if expanded[msg.id]}
              <pre class="tool-detail">{JSON.stringify(
                  msg.toolCall.args,
                  null,
                  2
                )}</pre>
            {/if}
          </div>
        {:else if msg.toolResult}
          <div class="agent-tool-card result">
            <button
              type="button"
              class="tool-summary"
              aria-expanded={expanded[msg.id] ?? false}
              aria-label={`Tool result: ${msg.toolResult.name}`}
              onclick={() => toggle(msg.id)}
              onkeydown={(e) => onToolCardKeydown(e, msg.id)}
            >
              <span
                class="material-symbols-outlined tool-icon"
                aria-hidden="true"
              >
                {msg.toolResult.error ? 'error' : 'check_circle'}
              </span>
              <span class="tool-name">{msg.toolResult.name}</span>
              <span class="tool-args">
                {msg.toolResult.error
                  ? msg.toolResult.error
                  : truncatedPreview(msg.toolResult.content)}
              </span>
              {#if msg.toolResult.truncated}
                <span
                  class="tool-trunc"
                  title="Result exceeds 10KB and was truncated"
                >
                  [… truncated]
                </span>
              {/if}
              <span
                class="material-symbols-outlined chevron"
                aria-hidden="true"
              >
                {expanded[msg.id] ? 'expand_less' : 'expand_more'}
              </span>
            </button>
            {#if expanded[msg.id]}
              <pre
                class="tool-detail"
                title={msg.toolResult.truncated
                  ? 'Result exceeds 10KB and was truncated for the model'
                  : undefined}>{msg.toolResult.error ??
                  msg.toolResult.content}</pre>
            {/if}
          </div>
        {/if}
      {/each}
    {/if}
  </div>

  {#if ctl?.pendingStaging}
    <StagingConfirm
      event={ctl.pendingStaging}
      onConfirm={onConfirmStaging}
      onReject={onRejectStaging}
    />
  {/if}

  <div class="agent-input-row">
    <label class="sr-only" for="agent-input">Message for AI agent</label>
    <textarea
      id="agent-input"
      bind:value={input}
      rows="2"
      placeholder={ctl?.running ? RUNNING_PLACEHOLDER : IDLE_PLACEHOLDER}
      aria-label="Message for AI agent"
      onkeydown={onKeydown}
      disabled={ctl?.running || !!ctl?.pendingStaging}></textarea>
    {#if ctl?.running}
      <button
        type="button"
        class="agent-btn stop"
        aria-label="Stop agent"
        onclick={() => ctl?.cancel()}
      >
        Stop
      </button>
    {:else}
      <button
        type="button"
        class="agent-btn send"
        aria-label="Send message"
        onclick={() => void onSend()}
        disabled={!input.trim() || !!ctl?.pendingStaging}
      >
        Send
      </button>
    {/if}
  </div>
</div>

<style>
  .agent-panel {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
    color: var(--color-text-primary, var(--surface-sidebar-text, #e8e8ec));
    font-size: 0.8125rem;
  }
  .agent-header {
    display: flex;
    align-items: center;
    gap: 0.4rem;
    padding: 0.65rem 0.75rem;
    border-bottom: 1px solid var(--color-surface-panel-border, #2a2a30);
    flex-shrink: 0;
  }
  .agent-header h2 {
    margin: 0;
    font-size: 0.875rem;
    font-weight: 600;
  }
  .agent-messages {
    flex: 1;
    overflow-y: auto;
    padding: 0.75rem;
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
    min-height: 0;
  }
  .agent-empty {
    opacity: 0.9;
    line-height: 1.45;
  }
  .agent-empty p {
    margin: 0;
  }
  .agent-msg-role {
    font-size: 0.65rem;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    color: var(--color-text-muted, #9a9aa3);
    margin-bottom: 0.15rem;
  }
  .agent-msg-body {
    white-space: pre-wrap;
    line-height: 1.45;
  }
  .agent-msg.user .agent-msg-body {
    background: color-mix(
      in srgb,
      var(--color-accent-primary-start, #6366f1) 12%,
      transparent
    );
    border-radius: 0.45rem;
    padding: 0.4rem 0.55rem;
  }
  .tool-summary {
    display: flex;
    align-items: center;
    gap: 0.35rem;
    width: 100%;
    text-align: left;
    border: 1px solid var(--color-surface-panel-border, #2a2a30);
    border-radius: 0.45rem;
    padding: 0.35rem 0.5rem;
    background: transparent;
    color: inherit;
    font: inherit;
    cursor: pointer;
  }
  .tool-summary:hover,
  .tool-summary:focus-visible {
    border-color: var(--color-accent-primary-start, #6366f1);
    outline: none;
  }
  .tool-summary:focus-visible {
    outline: 2px solid var(--color-accent-primary-start, #6366f1);
    outline-offset: 1px;
  }
  .tool-icon,
  .chevron {
    font-size: 1rem;
    color: var(--color-text-muted, #9a9aa3);
  }
  .tool-name {
    font-weight: 600;
    flex-shrink: 0;
  }
  .tool-args {
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    opacity: 0.85;
  }
  .tool-trunc {
    font-size: 0.7rem;
    color: var(--color-status-warn, #fbbf24);
    flex-shrink: 0;
  }
  .tool-detail {
    margin: 0.35rem 0 0;
    padding: 0.4rem 0.5rem;
    background: var(--color-surface-input, #121216);
    border-radius: 0.35rem;
    font-family: ui-monospace, monospace;
    font-size: 0.72rem;
    white-space: pre-wrap;
    word-break: break-word;
    max-height: 12rem;
    overflow-y: auto;
  }
  .agent-input-row {
    display: flex;
    gap: 0.4rem;
    padding: 0.65rem 0.75rem;
    border-top: 1px solid var(--color-surface-panel-border, #2a2a30);
    flex-shrink: 0;
  }
  .agent-input-row textarea {
    flex: 1;
    resize: none;
    border-radius: 0.45rem;
    border: 1px solid var(--color-surface-panel-border, #2a2a30);
    background: var(--color-surface-input, #121216);
    color: inherit;
    padding: 0.45rem 0.55rem;
    font: inherit;
  }
  .agent-input-row textarea:disabled {
    opacity: 0.6;
  }
  .agent-btn {
    border: none;
    border-radius: 0.45rem;
    padding: 0 0.85rem;
    font: inherit;
    font-weight: 600;
    cursor: pointer;
  }
  .agent-btn.send {
    background: var(--color-accent-primary-start, #6366f1);
    color: #fff;
  }
  .agent-btn.send:disabled {
    opacity: 0.4;
    cursor: default;
  }
  .agent-btn.stop {
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
