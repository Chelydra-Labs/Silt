<script lang="ts">
  // Writing Assistant panel — drawer or hub (#230).
  import type { PluginContext } from '../../sdk'
  import { enabledActions, actionById } from './catalog'
  import { getAssistantController } from './state.svelte'
  import type { ActionId } from './types'

  interface Props {
    ctx: PluginContext
    onClose?: () => void
  }
  let { ctx, onClose }: Props = $props()

  const ctl = $derived(getAssistantController())
  const actions = $derived(ctl ? enabledActions(ctl.settings) : [])
  const busy = $derived(
    ctl?.panelStatus === 'running' || ctl?.panelStatus === 'streaming'
  )
  const proposal = $derived(ctl?.proposal)
  const streamText = $derived(ctl?.streamText ?? '')

  function statusLabel(): string {
    if (!ctl) return ''
    switch (ctl.panelStatus) {
      case 'running':
        return 'Working…'
      case 'streaming':
        return 'Generating…'
      case 'ready':
        return 'Proposal ready — review before accepting'
      case 'no-chat-provider':
        return 'Chat model not configured'
      case 'no-embedding-provider':
        return 'Embedding model not configured'
      case 'no-input':
        return ctl.errorMessage || 'Need input'
      case 'error':
        return ctl.errorMessage || 'Error'
      default:
        return ''
    }
  }

  async function onRun() {
    if (!ctl || busy) return
    await ctl.run(ctx, ctl.selectedAction, {
      instruction: ctl.instruction
    })
  }

  async function onAccept() {
    if (!ctl) return
    await ctl.accept(ctx)
  }

  function onDiscard() {
    ctl?.discard()
  }

  function pickAction(id: ActionId) {
    if (!ctl || busy) return
    ctl.selectedAction = id
  }

  function openAISettings() {
    ctx.openSettings('ai')
  }
</script>

<div class="wa-panel" role="region" aria-label="Writing Assistant">
  <header class="wa-header">
    <span class="material-symbols-outlined" aria-hidden="true">ink_pen</span>
    <h2>Writing Assistant</h2>
    {#if onClose}
      <button
        type="button"
        class="wa-icon-btn"
        onclick={onClose}
        aria-label="Close Writing Assistant"
        title="Close (Esc)"
      >
        <span class="material-symbols-outlined" aria-hidden="true">close</span>
      </button>
    {/if}
  </header>

  {#if ctl && !ctl.chatReady()}
    <div class="wa-banner" role="status">
      <p>Configure a chat model to use writing actions.</p>
      <button type="button" class="wa-link" onclick={openAISettings}
        >Open AI Provider</button
      >
    </div>
  {/if}

  <div class="wa-actions" role="listbox" aria-label="Actions">
    {#each actions as a (a.id)}
      <button
        type="button"
        role="option"
        class="wa-action"
        class:active={ctl?.selectedAction === a.id}
        aria-selected={ctl?.selectedAction === a.id}
        disabled={busy}
        onclick={() => pickAction(a.id)}
      >
        <span class="material-symbols-outlined" aria-hidden="true"
          >{a.icon}</span
        >
        <span class="wa-action-label">{a.label}</span>
      </button>
    {/each}
  </div>

  {#if ctl && actionById(ctl.selectedAction)?.acceptsInstruction}
    <label class="wa-field">
      <span class="wa-field-label">Description</span>
      <textarea
        class="wa-textarea"
        rows="3"
        placeholder="What should the draft cover?"
        bind:value={ctl.instruction}
        disabled={busy}></textarea>
    </label>
  {/if}

  <div class="wa-run-row">
    <button
      type="button"
      class="wa-primary"
      disabled={busy || !ctl}
      onclick={() => void onRun()}
    >
      {busy ? 'Running…' : 'Run'}
    </button>
  </div>

  <div class="wa-status" role="status" aria-live="polite" aria-atomic="false">
    {statusLabel()}
  </div>

  <div class="wa-output" aria-live="polite">
    {#if proposal?.warning}
      <p class="wa-warn">{proposal.warning}</p>
    {/if}

    {#if proposal?.tags?.length}
      <ul class="wa-checklist">
        {#each proposal.tags as t (t.tag)}
          <li>
            <label>
              <input
                type="checkbox"
                checked={proposal.selectedTags?.includes(t.tag)}
                onchange={() => ctl?.toggleTag(t.tag)}
              />
              <span>#{t.tag}</span>
              {#if t.existing}
                <span class="wa-badge">existing</span>
              {/if}
            </label>
          </li>
        {/each}
      </ul>
    {:else if proposal?.related?.length}
      <ul class="wa-checklist">
        {#each proposal.related as r (r.blockId)}
          <li>
            <label>
              <input
                type="checkbox"
                checked={proposal.selectedRelatedIds?.includes(r.blockId)}
                onchange={() => ctl?.toggleRelated(r.blockId)}
              />
              <span class="wa-snippet" title={r.snippet}>{r.snippet}</span>
              <span class="wa-badge">{r.score.toFixed(2)}</span>
            </label>
          </li>
        {/each}
      </ul>
    {:else if streamText || proposal?.proposedMarkdown}
      <pre class="wa-preview">{streamText || proposal?.proposedMarkdown}</pre>
    {:else if !busy}
      <p class="wa-empty">
        Run an action to preview a proposal. Nothing is written until you
        accept.
      </p>
    {/if}
  </div>

  {#if proposal?.status === 'ready'}
    <div class="wa-footer">
      <button type="button" class="wa-primary" onclick={() => void onAccept()}>
        Accept
      </button>
      <button type="button" class="wa-secondary" onclick={onDiscard}>
        Discard
      </button>
    </div>
  {/if}
</div>

<style>
  .wa-panel {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
    background: var(--color-surface-card);
    color: var(--color-text-primary);
  }
  .wa-header {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    padding: 0.75rem 1rem;
    border-bottom: 1px solid var(--color-surface-panel-border);
  }
  .wa-header h2 {
    flex: 1;
    margin: 0;
    font-size: var(--type-lg, 1rem);
    font-weight: 600;
  }
  .wa-icon-btn {
    background: transparent;
    border: none;
    color: var(--color-text-muted);
    cursor: pointer;
    padding: 0.25rem;
    border-radius: 0.375rem;
  }
  .wa-icon-btn:hover {
    color: var(--color-text-primary);
    background: var(--color-surface-panel);
  }
  .wa-banner {
    margin: 0.75rem 1rem;
    padding: 0.75rem;
    border-radius: 0.75rem;
    border: 1px solid var(--color-accent-primary-start, #888);
    background: color-mix(
      in oklab,
      var(--color-accent-primary-start, #888) 12%,
      transparent
    );
  }
  .wa-banner p {
    margin: 0 0 0.5rem;
    font-size: 0.875rem;
  }
  .wa-link {
    background: none;
    border: none;
    color: var(--color-accent-primary-start);
    cursor: pointer;
    text-decoration: underline;
    padding: 0;
    font-size: 0.875rem;
  }
  .wa-actions {
    display: flex;
    flex-wrap: wrap;
    gap: 0.375rem;
    padding: 0.75rem 1rem;
  }
  .wa-action {
    display: inline-flex;
    align-items: center;
    gap: 0.25rem;
    padding: 0.35rem 0.55rem;
    border-radius: 999px;
    border: 1px solid var(--color-surface-panel-border);
    background: var(--color-surface-panel);
    color: var(--color-text-muted);
    font-size: 0.75rem;
    cursor: pointer;
  }
  .wa-action.active {
    color: var(--color-text-primary);
    border-color: var(--color-accent-primary-start);
    background: color-mix(
      in oklab,
      var(--color-accent-primary-start) 15%,
      transparent
    );
  }
  .wa-action .material-symbols-outlined {
    font-size: 1rem;
  }
  .wa-field {
    display: flex;
    flex-direction: column;
    gap: 0.35rem;
    padding: 0 1rem 0.5rem;
  }
  .wa-field-label {
    font-size: 0.75rem;
    color: var(--color-text-muted);
  }
  .wa-textarea {
    width: 100%;
    resize: vertical;
    border-radius: 0.5rem;
    border: 1px solid var(--color-surface-panel-border);
    background: var(--color-surface-app);
    color: var(--color-text-primary);
    padding: 0.5rem 0.65rem;
    font: inherit;
  }
  .wa-run-row {
    padding: 0 1rem 0.5rem;
  }
  .wa-primary {
    background: var(--color-accent-primary-start);
    color: var(--color-surface-app);
    border: none;
    border-radius: 0.5rem;
    padding: 0.45rem 0.9rem;
    font-weight: 600;
    cursor: pointer;
  }
  .wa-primary:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
  .wa-secondary {
    background: transparent;
    border: 1px solid var(--color-surface-panel-border);
    color: var(--color-text-primary);
    border-radius: 0.5rem;
    padding: 0.45rem 0.9rem;
    cursor: pointer;
  }
  .wa-status {
    min-height: 1.25rem;
    padding: 0 1rem;
    font-size: 0.8rem;
    color: var(--color-text-muted);
  }
  .wa-output {
    flex: 1;
    min-height: 0;
    overflow: auto;
    padding: 0.5rem 1rem 1rem;
  }
  .wa-preview {
    margin: 0;
    white-space: pre-wrap;
    word-break: break-word;
    font-family: var(--font-mono, ui-monospace, monospace);
    font-size: 0.8rem;
    line-height: 1.45;
    padding: 0.75rem;
    border-radius: 0.5rem;
    background: var(--color-surface-panel);
    border: 1px solid var(--color-surface-panel-border);
  }
  .wa-empty {
    color: var(--color-text-muted);
    font-size: 0.875rem;
  }
  .wa-warn {
    color: var(--color-text-muted);
    font-size: 0.8rem;
    margin: 0 0 0.5rem;
  }
  .wa-checklist {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 0.4rem;
  }
  .wa-checklist label {
    display: flex;
    align-items: flex-start;
    gap: 0.4rem;
    font-size: 0.85rem;
  }
  .wa-snippet {
    flex: 1;
    min-width: 0;
  }
  .wa-badge {
    font-size: 0.7rem;
    color: var(--color-text-muted);
    border: 1px solid var(--color-surface-panel-border);
    border-radius: 999px;
    padding: 0 0.35rem;
  }
  .wa-footer {
    display: flex;
    gap: 0.5rem;
    padding: 0.75rem 1rem;
    border-top: 1px solid var(--color-surface-panel-border);
  }
</style>
