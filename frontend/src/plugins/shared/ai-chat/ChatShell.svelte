<script lang="ts">
  import { tick, type Snippet } from 'svelte'
  import { Browser } from '@wailsio/runtime'
  import type {
    AIChatEntry,
    ConfirmationEntry,
    EvidenceTarget,
    ToolCallEntry,
    ToolResultEntry
  } from './types'
  import { renderChatMarkdown } from './renderChatMarkdown'
  import { isSafeLinkHref } from '../../../lib/editor/converters/validate'
  import {
    evidenceGroupSummaryLabel,
    filterTranscriptForBusyDisplay,
    groupTranscript,
    toolActivitySummaryLabel
  } from './groupTranscript'

  interface Props {
    title?: string
    transcript: AIChatEntry[]
    busy: boolean
    lastOutcome: 'complete' | 'stopped' | 'error' | null
    providerReady: boolean
    actions?: Snippet
    onSend: (text: string) => void | Promise<void>
    onStop: () => void
    onAcceptProposal: (id: string) => void
    onDiscardProposal: (id: string) => void
    onConfirmStaging: (token: string) => void
    onRejectStaging: (token: string) => void
    onOpenSettings: () => void
    onNavigateEvidence: (target: EvidenceTarget) => void
    onClear: () => void
  }

  let {
    title = 'Silt AI',
    transcript,
    busy,
    lastOutcome,
    providerReady,
    actions,
    onSend,
    onStop,
    onAcceptProposal,
    onDiscardProposal,
    onConfirmStaging,
    onRejectStaging,
    onOpenSettings,
    onNavigateEvidence,
    onClear
  }: Props = $props()

  let draft = $state('')
  let transcriptEl = $state<HTMLElement | null>(null)
  let confirmationEl = $state<HTMLElement | null>(null)
  let safeActionEl = $state<HTMLButtonElement | null>(null)
  let stickToBottom = true
  let expanded = $state<Record<string, boolean>>({})
  let completionAnnouncement = $state('')
  let wasBusy = false

  const pendingConfirmation = $derived(
    transcript.find(
      (entry): entry is ConfirmationEntry =>
        entry.kind === 'confirmation' &&
        (entry.state ?? 'pending') === 'pending'
    )
  )
  const pendingConfirmationId = $derived(pendingConfirmation?.id ?? null)
  const composerDisabled = $derived(!providerReady || !!pendingConfirmation)
  // While a run is in progress, hide only the current turn's tool cards so
  // prior multi-turn activity disclosures stay visible (#845).
  const displayTranscript = $derived(
    filterTranscriptForBusyDisplay(transcript, busy)
  )
  const displaySegments = $derived(groupTranscript(displayTranscript))

  function entryPreview(value: unknown, limit = 96): string {
    const text = typeof value === 'string' ? value : safeJson(value)
    return text.length > limit ? `${text.slice(0, limit)}…` : text
  }

  function safeJson(value: unknown): string {
    try {
      return JSON.stringify(value, null, 2)
    } catch {
      return '(unavailable)'
    }
  }

  function toggleExpanded(id: string) {
    expanded = { ...expanded, [id]: !expanded[id] }
  }

  async function send() {
    const text = draft.trim()
    if (!text || busy || composerDisabled) return
    draft = ''
    await onSend(text)
  }

  function onComposerKeydown(event: KeyboardEvent) {
    if (event.key === 'Escape' && busy && !pendingConfirmation) {
      event.preventDefault()
      event.stopPropagation()
      onStop()
      return
    }
    if (event.key === 'Enter' && !event.shiftKey) {
      event.preventDefault()
      void send()
    }
  }

  function onTranscriptScroll() {
    if (!transcriptEl) return
    const distance =
      transcriptEl.scrollHeight -
      transcriptEl.scrollTop -
      transcriptEl.clientHeight
    stickToBottom = distance < 32
  }

  // Markdown {@html} emits bare <a href>. Route safe absolute links through
  // the OS browser so the Wails webview is not navigated away (AboutTab
  // convention). Always preventDefault on transcript anchors so unsafe hrefs
  // cannot navigate the webview either. Attached via $effect for a11y.
  function isOpenableExternalHref(href: string): boolean {
    if (!href || !isSafeLinkHref(href)) return false
    // Protocol-relative and path-relative are not OS-browser targets.
    if (
      href.startsWith('//') ||
      href.startsWith('#') ||
      href.startsWith('/') ||
      href.startsWith('./') ||
      href.startsWith('../')
    ) {
      return false
    }
    try {
      const u = new URL(href)
      return (
        u.protocol === 'http:' ||
        u.protocol === 'https:' ||
        u.protocol === 'mailto:'
      )
    } catch {
      return false
    }
  }

  function onTranscriptClick(e: MouseEvent): void {
    const a = (e.target as HTMLElement | null)?.closest?.('a')
    if (!a || !transcriptEl?.contains(a)) return
    const href = a.getAttribute('href')
    if (!href) return
    e.preventDefault()
    if (!isOpenableExternalHref(href)) return
    void Browser.OpenURL(href)
  }

  $effect(() => {
    const el = transcriptEl
    if (!el) return
    el.addEventListener('click', onTranscriptClick)
    return () => el.removeEventListener('click', onTranscriptClick)
  })

  function focusableWithin(element: HTMLElement): HTMLElement[] {
    return Array.from(
      element.querySelectorAll<HTMLElement>(
        'button:not([disabled]), [href], input:not([disabled]), textarea:not([disabled]), select:not([disabled]), [tabindex]:not([tabindex="-1"])'
      )
    )
  }

  function onConfirmationKeydown(
    event: KeyboardEvent,
    confirmation: ConfirmationEntry
  ) {
    event.stopPropagation()
    if (event.key === 'Escape') {
      event.preventDefault()
      onRejectStaging(confirmation.token)
      return
    }
    if (event.key !== 'Tab' || !confirmationEl) return

    const focusable = focusableWithin(confirmationEl)
    const first = focusable[0]
    const last = focusable[focusable.length - 1]
    const active = document.activeElement
    if (!first || !last) {
      event.preventDefault()
      confirmationEl.focus()
    } else if (
      event.shiftKey &&
      (active === first || !confirmationEl.contains(active))
    ) {
      event.preventDefault()
      last.focus()
    } else if (
      !event.shiftKey &&
      (active === last || !confirmationEl.contains(active))
    ) {
      event.preventDefault()
      first.focus()
    }
  }

  $effect(() => {
    transcript.map((entry) =>
      entry.kind === 'text' ? `${entry.id}:${entry.content}` : entry.id
    )
    void tick().then(() => {
      if (stickToBottom && transcriptEl) {
        transcriptEl.scrollTop = transcriptEl.scrollHeight
      }
    })
  })

  $effect(() => {
    if (busy) {
      // Announce work started without narrating streamed tokens (which would
      // flood assistive tech). The completion line below closes the loop.
      completionAnnouncement = 'Silt is responding.'
    } else if (wasBusy && lastOutcome !== null) {
      // Per-run terminal outcome from the controller. Guard on lastOutcome so
      // clear()/vault-switch (busy→false with lastOutcome null) does not fire
      // a spurious "complete/stopped" announcement on an empty transcript.
      completionAnnouncement =
        lastOutcome === 'error'
          ? 'AI response ended with an error.'
          : lastOutcome === 'stopped'
            ? 'AI response stopped.'
            : 'AI response complete.'
    }
    wasBusy = busy
  })

  // Drop expand-state keys when the transcript is wiped (New chat / vault
  // switch). ChatShell is not destroyed, so without this the map grows for
  // the life of the drawer.
  $effect(() => {
    if (transcript.length === 0 && Object.keys(expanded).length > 0) {
      expanded = {}
    }
  })

  $effect(() => {
    if (!pendingConfirmationId) return
    const previouslyFocused =
      document.activeElement instanceof HTMLElement
        ? document.activeElement
        : null
    void tick().then(() => safeActionEl?.focus())
    return () => {
      if (previouslyFocused?.isConnected) previouslyFocused.focus()
    }
  })
</script>

<section class="chat-shell" aria-label={title}>
  <header class="chat-header">
    <div class="chat-mark" aria-hidden="true">
      <span class="material-symbols-outlined">auto_awesome</span>
    </div>
    <div class="chat-heading">
      <span class="eyebrow">Workspace intelligence</span>
      <h2>{title}</h2>
    </div>
    <div class="header-actions">
      {@render actions?.()}
      <button
        type="button"
        class="quiet-button"
        disabled={transcript.length === 0}
        onclick={onClear}>New chat</button
      >
    </div>
  </header>

  {#if !providerReady}
    <div class="provider-banner" role="status">
      <span class="material-symbols-outlined" aria-hidden="true">tune</span>
      <div>
        <strong>Connect a chat model</strong>
        <p>Choose an AI provider before starting a conversation.</p>
      </div>
      <button type="button" class="text-button" onclick={onOpenSettings}
        >Open AI settings</button
      >
    </div>
  {/if}

  <div
    class="transcript"
    bind:this={transcriptEl}
    role="log"
    aria-label={`${title} conversation`}
    aria-live="polite"
    aria-relevant="additions"
    aria-atomic="false"
    aria-busy={busy}
    onscroll={onTranscriptScroll}
  >
    {#if transcript.length === 0}
      <div class="empty-state">
        <span class="empty-orbit" aria-hidden="true">
          <span class="material-symbols-outlined">north_east</span>
        </span>
        <h3>Start with the outcome.</h3>
        <p>
          Ask Silt to explore your notes, explain its work, or prepare a change
          for your review.
        </p>
      </div>
    {/if}

    {#each displaySegments as segment (segment.kind === 'entry' ? segment.entry.id : segment.id)}
      {#if segment.kind === 'tool-activity'}
        {@const group = segment}
        <article class="utility-card tool-activity-group">
          <button
            type="button"
            class="utility-summary"
            aria-expanded={expanded[group.id] ?? false}
            aria-controls={`${group.id}-details`}
            onclick={() => toggleExpanded(group.id)}
          >
            <span
              class="tool-glyph material-symbols-outlined"
              aria-hidden="true">construction</span
            >
            <span class="utility-copy">
              <strong
                >{toolActivitySummaryLabel(
                  group.callCount,
                  group.resultCount
                )}</strong
              >
            </span>
            <span class="material-symbols-outlined" aria-hidden="true"
              >{expanded[group.id] ? 'expand_less' : 'expand_more'}</span
            >
          </button>
          {#if expanded[group.id]}
            <div id={`${group.id}-details`} class="tool-activity-body">
              {#each group.items as item (item.id)}
                {#if item.kind === 'tool-call'}
                  {@const call = item as ToolCallEntry}
                  <div class="tool-activity-item">
                    <button
                      type="button"
                      class="utility-summary nested"
                      aria-expanded={expanded[call.id] ?? false}
                      aria-controls={`${call.id}-details`}
                      onclick={() => toggleExpanded(call.id)}
                    >
                      <span
                        class="tool-glyph material-symbols-outlined"
                        aria-hidden="true">terminal</span
                      >
                      <span class="utility-copy">
                        <span class="entry-label">Tool call</span>
                        <strong>{call.toolName}</strong>
                        <span>{entryPreview(call.args)}</span>
                      </span>
                      <span class="material-symbols-outlined" aria-hidden="true"
                        >{expanded[call.id]
                          ? 'expand_less'
                          : 'expand_more'}</span
                      >
                    </button>
                    {#if expanded[call.id]}
                      <pre id={`${call.id}-details`}>{safeJson(call.args)}</pre>
                    {/if}
                  </div>
                {:else}
                  {@const result = item as ToolResultEntry}
                  <div
                    class="tool-activity-item"
                    class:error-card={!!result.error}
                  >
                    <button
                      type="button"
                      class="utility-summary nested"
                      aria-expanded={expanded[result.id] ?? false}
                      aria-controls={`${result.id}-details`}
                      onclick={() => toggleExpanded(result.id)}
                    >
                      <span
                        class="tool-glyph material-symbols-outlined"
                        aria-hidden="true"
                        >{result.error ? 'error' : 'check_circle'}</span
                      >
                      <span class="utility-copy">
                        <span class="entry-label">Tool result</span>
                        <strong>{result.toolName}</strong>
                        <span
                          >{entryPreview(result.error ?? result.output)}</span
                        >
                      </span>
                      {#if result.truncated}<span class="truncated"
                          >Truncated</span
                        >{/if}
                      <span class="material-symbols-outlined" aria-hidden="true"
                        >{expanded[result.id]
                          ? 'expand_less'
                          : 'expand_more'}</span
                      >
                    </button>
                    {#if expanded[result.id]}
                      <pre id={`${result.id}-details`}>{result.error ??
                          result.output}</pre>
                    {/if}
                  </div>
                {/if}
              {/each}
            </div>
          {/if}
        </article>
      {:else if segment.kind === 'evidence-group'}
        {@const group = segment}
        <article class="utility-card evidence-group">
          <button
            type="button"
            class="utility-summary"
            aria-expanded={expanded[group.id] ?? false}
            aria-controls={`${group.id}-details`}
            onclick={() => toggleExpanded(group.id)}
          >
            <span
              class="tool-glyph material-symbols-outlined"
              aria-hidden="true">menu_book</span
            >
            <span class="utility-copy">
              <strong>{evidenceGroupSummaryLabel(group.items.length)}</strong>
            </span>
            <span class="material-symbols-outlined" aria-hidden="true"
              >{expanded[group.id] ? 'expand_less' : 'expand_more'}</span
            >
          </button>
          {#if expanded[group.id]}
            <div id={`${group.id}-details`} class="evidence-group-body">
              {#each group.items as entry (entry.id)}
                <article class="evidence-card nested">
                  <span class="citation-index">{entry.citationIndex}</span>
                  <div class="evidence-copy">
                    <span class="entry-label">Source</span>
                    <strong>{entry.title}</strong>
                    {#if entry.excerpt}<p>{entry.excerpt}</p>{/if}
                  </div>
                  <button
                    type="button"
                    class="icon-button"
                    aria-label={`Open source ${entry.citationIndex}: ${entry.title}`}
                    onclick={() => onNavigateEvidence(entry.target)}
                  >
                    <span class="material-symbols-outlined" aria-hidden="true"
                      >arrow_outward</span
                    >
                  </button>
                </article>
              {/each}
            </div>
          {/if}
        </article>
      {:else if segment.entry.kind === 'text'}
        {@const entry = segment.entry}
        <article
          class="message"
          class:user-message={entry.role === 'user'}
          class:streaming={entry.streaming}
          aria-live="off"
        >
          <span class="entry-label"
            >{entry.role === 'user' ? 'You' : 'Silt'}</span
          >
          {#if entry.role === 'assistant'}
            <!-- Assistant replies are markdown; sanitize before {@html}. -->
            <div class="message-copy message-md">
              <!-- eslint-disable-next-line svelte/no-at-html-tags -- renderChatMarkdown sanitizes HTML -->
              {@html renderChatMarkdown(entry.content)}
            </div>
          {:else}
            <div class="message-copy">{entry.content}</div>
          {/if}
          {#if entry.streaming}
            <span class="stream-caret" aria-hidden="true"></span>
          {/if}
        </article>
      {:else if segment.entry.kind === 'evidence'}
        {@const entry = segment.entry}
        <article class="evidence-card">
          <span class="citation-index">{entry.citationIndex}</span>
          <div class="evidence-copy">
            <span class="entry-label">Source</span>
            <strong>{entry.title}</strong>
            {#if entry.excerpt}<p>{entry.excerpt}</p>{/if}
          </div>
          <button
            type="button"
            class="icon-button"
            aria-label={`Open source ${entry.citationIndex}: ${entry.title}`}
            onclick={() => onNavigateEvidence(entry.target)}
          >
            <span class="material-symbols-outlined" aria-hidden="true"
              >arrow_outward</span
            >
          </button>
        </article>
      {:else if segment.entry.kind === 'proposal'}
        {@const entry = segment.entry}
        <article class="proposal-card">
          <div class="proposal-heading">
            <span class="material-symbols-outlined" aria-hidden="true"
              >edit_note</span
            >
            <div>
              <span class="entry-label">Proposed change</span>
              <h3>{entry.title}</h3>
            </div>
          </div>
          {#if entry.description}<p>{entry.description}</p>{/if}
          <pre>{entry.content}</pre>
          {#if (entry.state ?? 'pending') === 'pending'}
            <div class="card-actions">
              <button
                type="button"
                class="primary-button"
                onclick={() => onAcceptProposal(entry.id)}>Accept</button
              >
              <button
                type="button"
                class="secondary-button"
                onclick={() => onDiscardProposal(entry.id)}>Discard</button
              >
            </div>
          {:else}
            <span class="resolved-label">{entry.state}</span>
          {/if}
        </article>
      {:else if segment.entry.kind === 'confirmation'}
        {@const entry = segment.entry}
        {@const isDanger = (entry.severity ?? 'danger') === 'danger'}
        {#if (entry.state ?? 'pending') === 'pending'}
          <div
            class="confirmation-card"
            class:confirmation-danger={isDanger}
            class:confirmation-normal={!isDanger}
            bind:this={confirmationEl}
            role="alertdialog"
            aria-labelledby={`confirmation-title-${entry.id}`}
            aria-describedby={`confirmation-summary-${entry.id}`}
            tabindex="-1"
            onkeydown={(event) => onConfirmationKeydown(event, entry)}
          >
            <div class="proposal-heading">
              <span
                class="material-symbols-outlined"
                class:danger-glyph={isDanger}
                aria-hidden="true">{isDanger ? 'warning' : 'edit_note'}</span
              >
              <div>
                <span class="entry-label">Your approval is required</span>
                <h3 id={`confirmation-title-${entry.id}`}>
                  {isDanger ? 'Review bulk change' : 'Approve vault change'}
                </h3>
              </div>
            </div>
            <p id={`confirmation-summary-${entry.id}`}>{entry.summary}</p>
            {#if entry.affectedCount != null}
              <p class="muted-copy">{entry.affectedCount} items affected</p>
            {/if}
            {#if entry.details}<pre>{entry.details}</pre>{/if}
            <p class="muted-copy confirmation-hint">
              Reject or Stop to cancel. Escape rejects while this card is open.
            </p>
            <div class="card-actions">
              <button
                type="button"
                class="secondary-button"
                bind:this={safeActionEl}
                onclick={() => onRejectStaging(entry.token)}>Reject</button
              >
              <button
                type="button"
                class={isDanger ? 'danger-button' : 'primary-button'}
                onclick={() => onConfirmStaging(entry.token)}
                >{isDanger ? `Confirm ${entry.operation}` : 'Approve'}</button
              >
            </div>
          </div>
        {:else}
          <div class="resolved-line">
            <span class="material-symbols-outlined" aria-hidden="true"
              >{entry.state === 'confirmed' ? 'check' : 'close'}</span
            >
            {entry.summary} — {entry.state}
          </div>
        {/if}
      {:else if segment.entry.kind === 'status'}
        {@const entry = segment.entry}
        <div
          role={entry.status === 'error' ? 'alert' : 'status'}
          class:error-status={entry.status === 'error'}
          class:done-status={entry.status === 'done'}
          class:stopped-status={entry.status === 'stopped'}
          class="status-line"
        >
          <span class="status-pulse" aria-hidden="true"></span>
          <span>{entry.message}</span>
        </div>
      {/if}
    {/each}
  </div>

  <div class="sr-only" role="status" aria-live="polite" aria-atomic="true">
    {completionAnnouncement}
  </div>

  <div class="composer-wrap">
    <label for="ai-chat-composer">Message Silt AI</label>
    <div class="composer">
      <textarea
        id="ai-chat-composer"
        bind:value={draft}
        rows="2"
        placeholder={busy
          ? 'Silt is working…'
          : 'Ask, explore, or propose a change…'}
        disabled={composerDisabled}
        onkeydown={onComposerKeydown}></textarea>
      {#if busy}
        <button
          type="button"
          class="stop-button"
          aria-label="Stop AI response"
          onclick={onStop}
        >
          <span class="material-symbols-outlined" aria-hidden="true">stop</span>
          Stop
        </button>
      {:else}
        <button
          type="button"
          class="send-button"
          aria-label="Send message"
          disabled={composerDisabled || !draft.trim()}
          onclick={() => void send()}
        >
          <span class="material-symbols-outlined" aria-hidden="true"
            >arrow_upward</span
          >
        </button>
      {/if}
    </div>
    <span class="composer-hint">Enter to send · Shift+Enter for a new line</span
    >
  </div>
</section>

<style>
  .chat-shell {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
    overflow: hidden;
    color: var(--color-text-primary);
    background:
      radial-gradient(
        circle at 100% 0%,
        var(--color-accent-secondary-glow),
        transparent 34%
      ),
      var(--color-surface-panel);
    font-family: var(--font-body);
  }
  .chat-header {
    display: flex;
    align-items: center;
    gap: 0.75rem;
    padding: 0.75rem 1rem;
    border-bottom: 1px solid var(--color-surface-panel-border);
    background: color-mix(in srgb, var(--color-surface-card) 82%, transparent);
  }
  .chat-mark {
    display: grid;
    place-items: center;
    width: 2rem;
    height: 2rem;
    border: 1px solid var(--color-surface-card-border);
    border-radius: 0.75rem 0.25rem 0.75rem 0.25rem;
    color: var(--color-accent-primary-start);
    background: var(--color-accent-primary-glow);
    transform: rotate(-3deg);
  }
  .chat-mark span {
    font-size: 1.1rem;
  }
  .chat-heading {
    flex: 1;
    min-width: 0;
  }
  .chat-heading h2 {
    margin: 0;
    font-family: var(--font-headline);
    font-size: 1rem;
    line-height: 1.2;
  }
  .eyebrow,
  .entry-label {
    display: block;
    color: var(--color-text-muted);
    font-size: 0.625rem;
    font-weight: 700;
    letter-spacing: 0.08em;
    text-transform: uppercase;
  }
  .header-actions {
    display: flex;
    align-items: center;
    gap: 0.5rem;
  }
  button {
    font: inherit;
  }
  button:focus-visible,
  textarea:focus-visible {
    outline: 2px solid var(--color-border-focus);
    outline-offset: 1px;
  }
  .quiet-button,
  .text-button {
    border: 0;
    color: var(--color-text-muted);
    background: transparent;
    cursor: pointer;
  }
  .quiet-button {
    font-size: 0.8125rem;
  }
  .quiet-button:hover,
  .text-button:hover {
    color: var(--color-text-primary);
  }
  button:disabled {
    opacity: 0.45;
    cursor: default;
  }
  .provider-banner {
    display: grid;
    grid-template-columns: auto 1fr auto;
    align-items: center;
    gap: 0.75rem;
    margin: 0.75rem 1rem 0;
    padding: 0.75rem;
    border: 1px solid var(--color-border-active);
    border-radius: 0.75rem;
    background: var(--color-surface-card);
  }
  .provider-banner > span {
    color: var(--color-accent-secondary-start);
  }
  .provider-banner p {
    margin: 0.15rem 0 0;
    color: var(--color-text-muted);
  }
  .provider-banner .text-button {
    color: var(--color-accent-primary-start);
    font-weight: 700;
  }
  .transcript {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
    padding: 1rem;
    scroll-behavior: smooth;
  }
  /* Hold each message at its natural height so the transcript scrolls
     instead of compressing items to fit the viewport. */
  .transcript > * {
    flex-shrink: 0;
  }
  .empty-state {
    width: min(24rem, 90%);
    margin: auto;
    padding: 2rem 1rem;
    text-align: center;
  }
  .empty-orbit {
    display: grid;
    place-items: center;
    width: 3.5rem;
    height: 3.5rem;
    margin: 0 auto 1rem;
    border: 1px solid var(--color-surface-panel-border);
    border-radius: 50%;
    color: var(--color-accent-primary-start);
    box-shadow: 0 0 0 0.5rem var(--color-accent-primary-glow);
  }
  .empty-state h3 {
    margin: 0;
    font-family: var(--font-headline);
    font-size: 1.25rem;
  }
  .empty-state p {
    color: var(--color-text-muted);
    line-height: 1.6;
  }
  .message {
    max-width: 92%;
    animation: arrive 160ms ease-out;
  }
  .message-copy {
    margin-top: 0.2rem;
    white-space: pre-wrap;
    line-height: 1.55;
  }
  /* Rendered assistant markdown: block elements own their spacing. */
  .message-md {
    white-space: normal;
  }
  .message-md :global(p) {
    margin: 0 0 0.55em;
  }
  .message-md :global(p:last-child) {
    margin-bottom: 0;
  }
  .message-md :global(h1),
  .message-md :global(h2),
  .message-md :global(h3),
  .message-md :global(h4),
  .message-md :global(h5),
  .message-md :global(h6) {
    margin: 0.75em 0 0.35em;
    font-family: var(--font-headline);
    line-height: 1.25;
  }
  .message-md :global(h1) {
    font-size: 1.25rem;
  }
  .message-md :global(h2) {
    font-size: 1.1rem;
  }
  .message-md :global(h3),
  .message-md :global(h4),
  .message-md :global(h5),
  .message-md :global(h6) {
    font-size: 1rem;
  }
  .message-md :global(ul),
  .message-md :global(ol) {
    margin: 0.35em 0 0.55em;
    padding-left: 1.35em;
  }
  .message-md :global(li) {
    margin: 0.15em 0;
  }
  .message-md :global(pre) {
    margin: 0.45em 0;
    padding: 0.55em 0.7em;
    overflow-x: auto;
    border-radius: 0.45rem;
    background: color-mix(in srgb, var(--color-surface-card) 80%, transparent);
    border: 1px solid var(--color-surface-card-border);
    font-size: 0.85em;
  }
  .message-md :global(code) {
    font-family: var(--font-mono, ui-monospace, monospace);
    font-size: 0.9em;
  }
  .message-md :global(:not(pre) > code) {
    padding: 0.1em 0.3em;
    border-radius: 0.25rem;
    background: color-mix(in srgb, var(--color-surface-card) 70%, transparent);
  }
  .message-md :global(blockquote) {
    margin: 0.45em 0;
    padding-left: 0.75em;
    border-left: 3px solid var(--color-accent-primary-start);
    color: var(--color-text-muted);
  }
  .message-md :global(a) {
    color: var(--color-accent-primary-start);
  }
  .user-message {
    align-self: flex-end;
    padding: 0.65rem 0.75rem;
    border: 1px solid var(--color-surface-card-border);
    border-radius: 0.8rem 0.8rem 0.2rem 0.8rem;
    background: var(--color-surface-card);
  }
  .stream-caret {
    display: inline-block;
    width: 0.4rem;
    height: 0.9rem;
    margin-left: 0.2rem;
    vertical-align: text-bottom;
    background: var(--color-accent-secondary-start);
    animation: blink 900ms steps(1) infinite;
  }
  .evidence-card,
  .proposal-card,
  .confirmation-card {
    border: 1px solid var(--color-surface-card-border);
    border-radius: 0.75rem;
    background: var(--color-surface-card);
    animation: arrive 160ms ease-out;
  }
  .evidence-card {
    display: grid;
    grid-template-columns: auto 1fr auto;
    align-items: start;
    gap: 0.75rem;
    padding: 0.75rem;
    border-left: 3px solid var(--color-accent-secondary-start);
  }
  .evidence-card.nested {
    border-radius: 0.5rem;
    animation: none;
  }
  .evidence-group-body {
    display: flex;
    flex-direction: column;
    gap: 0.4rem;
    padding: 0.45rem 0.5rem 0.55rem;
    border-top: 1px solid var(--color-surface-panel-border);
  }
  .citation-index {
    display: grid;
    place-items: center;
    min-width: 1.75rem;
    height: 1.75rem;
    border-radius: 50%;
    color: var(--color-accent-secondary-start);
    background: var(--color-accent-secondary-glow);
    font-family: var(--font-mono);
    font-size: 0.8125rem;
  }
  .evidence-copy p {
    margin: 0.35rem 0 0;
    color: var(--color-text-muted);
    line-height: 1.45;
  }
  .icon-button {
    display: grid;
    place-items: center;
    border: 0;
    color: var(--color-accent-primary-start);
    background: transparent;
    cursor: pointer;
  }
  .utility-card {
    overflow: hidden;
    border: 1px solid var(--color-surface-panel-border);
    border-radius: 0.5rem;
    background: color-mix(in srgb, var(--color-surface-card) 72%, transparent);
  }
  .tool-activity-body {
    display: flex;
    flex-direction: column;
    gap: 0.25rem;
    padding: 0 0.35rem 0.45rem;
    border-top: 1px solid var(--color-surface-panel-border);
  }
  .tool-activity-item {
    border-radius: 0.35rem;
    background: color-mix(in srgb, var(--color-surface-card) 55%, transparent);
  }
  .utility-summary {
    display: flex;
    align-items: center;
    gap: 0.65rem;
    width: 100%;
    padding: 0.55rem 0.65rem;
    border: 0;
    color: var(--color-text-primary);
    text-align: left;
    background: transparent;
    cursor: pointer;
  }
  .utility-summary.nested {
    padding: 0.4rem 0.5rem;
  }
  .utility-summary:hover {
    background: var(--color-hover);
  }
  .tool-glyph {
    color: var(--color-accent-secondary-start);
  }
  .utility-copy {
    flex: 1;
    min-width: 0;
    display: grid;
    grid-template-columns: auto 1fr;
    gap: 0.1rem 0.5rem;
  }
  .utility-copy .entry-label {
    grid-column: 1 / -1;
  }
  .utility-copy > span:last-child {
    overflow: hidden;
    color: var(--color-text-muted);
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  pre {
    margin: 0;
    padding: 0.75rem;
    overflow: auto;
    color: var(--color-text-primary);
    background: var(--color-surface-app);
    font-family: var(--font-mono);
    font-size: 0.8125rem;
    line-height: 1.5;
    white-space: pre-wrap;
    word-break: break-word;
  }
  .truncated,
  .resolved-label {
    color: var(--color-status-warn);
    font-size: 0.75rem;
    text-transform: capitalize;
  }
  .error-card {
    border-color: var(--color-error-border);
  }
  .error-card .tool-glyph {
    color: var(--color-error);
  }
  .proposal-card,
  .confirmation-card {
    padding: 0.85rem;
  }
  .proposal-card {
    border-top: 2px solid var(--color-accent-primary-start);
  }
  .proposal-heading {
    display: flex;
    align-items: center;
    gap: 0.65rem;
  }
  .proposal-heading > span {
    color: var(--color-accent-primary-start);
  }
  .proposal-heading h3 {
    margin: 0.1rem 0 0;
    font-family: var(--font-headline);
    font-size: 0.875rem;
  }
  .proposal-card > p,
  .confirmation-card > p {
    margin: 0.65rem 0;
    line-height: 1.5;
  }
  .proposal-card pre,
  .confirmation-card pre {
    margin-top: 0.65rem;
    border-radius: 0.5rem;
  }
  .card-actions {
    display: flex;
    justify-content: flex-end;
    gap: 0.5rem;
    margin-top: 0.75rem;
  }
  .primary-button,
  .secondary-button,
  .danger-button,
  .stop-button,
  .send-button {
    border-radius: 0.5rem;
    padding: 0.45rem 0.8rem;
    font-weight: 700;
    cursor: pointer;
  }
  .primary-button,
  .send-button {
    border: 0;
    color: var(--color-surface-app);
    background: var(--color-accent-primary-start);
  }
  .secondary-button {
    border: 1px solid var(--color-surface-panel-border);
    color: var(--color-text-primary);
    background: transparent;
  }
  .confirmation-card.confirmation-danger {
    border-color: var(--color-status-danger);
    background: color-mix(
      in srgb,
      var(--color-status-danger) 7%,
      var(--color-surface-card)
    );
  }
  .confirmation-card.confirmation-normal {
    border-top: 2px solid var(--color-accent-primary-start);
  }
  .confirmation-hint {
    font-size: 0.75rem;
  }
  .proposal-heading > .danger-glyph {
    color: var(--color-status-danger);
  }
  .proposal-heading > span:not(.danger-glyph) {
    color: var(--color-accent-primary-start);
  }
  .danger-button,
  .stop-button {
    border: 1px solid var(--color-status-danger);
    color: var(--color-status-danger);
    background: color-mix(in srgb, var(--color-status-danger) 12%, transparent);
  }
  .muted-copy {
    color: var(--color-text-muted);
  }
  .resolved-line,
  .status-line {
    display: flex;
    align-items: center;
    gap: 0.4rem;
    color: var(--color-text-muted);
    font-size: 0.8125rem;
  }
  .resolved-line > span {
    font-size: 1rem;
  }
  .status-pulse {
    width: 0.4rem;
    height: 0.4rem;
    border-radius: 50%;
    background: var(--color-accent-secondary-start);
    box-shadow: 0 0 0 0.25rem var(--color-accent-secondary-glow);
  }
  .error-status {
    color: var(--color-error);
  }
  .error-status .status-pulse {
    background: var(--color-error);
    box-shadow: none;
  }
  .done-status {
    color: var(--color-status-success, var(--color-accent-primary-start));
  }
  .done-status .status-pulse {
    background: var(--color-status-success, var(--color-accent-primary-start));
    box-shadow: none;
  }
  .stopped-status {
    color: var(--color-text-muted);
  }
  .stopped-status .status-pulse {
    background: var(--color-text-muted);
    box-shadow: none;
  }
  .composer-wrap {
    padding: 0.65rem 0.75rem 0.75rem;
    border-top: 1px solid var(--color-surface-panel-border);
    background: var(--color-surface-card);
  }
  .composer-wrap > label {
    display: block;
    margin: 0 0 0.35rem 0.2rem;
    color: var(--color-text-muted);
    font-size: 0.75rem;
  }
  .composer {
    display: flex;
    align-items: flex-end;
    gap: 0.5rem;
    padding: 0.4rem;
    border: 1px solid var(--color-surface-card-border);
    border-radius: 0.75rem;
    background: var(--color-surface-app);
  }
  .composer:focus-within {
    border-color: var(--color-border-focus);
  }
  .composer textarea {
    flex: 1;
    min-width: 0;
    resize: none;
    border: 0;
    outline: 0;
    color: var(--color-text-primary);
    background: transparent;
    font: inherit;
    line-height: 1.45;
  }
  .composer textarea:disabled {
    color: var(--color-text-disabled);
  }
  .send-button {
    display: grid;
    place-items: center;
    width: 2.25rem;
    height: 2.25rem;
    padding: 0;
  }
  .send-button span {
    font-size: 1rem;
  }
  .stop-button {
    display: flex;
    align-items: center;
    gap: 0.25rem;
  }
  .stop-button span {
    font-size: 1rem;
  }
  .composer-hint {
    display: block;
    margin: 0.3rem 0.2rem 0;
    color: var(--color-text-disabled);
    font-size: 0.625rem;
  }
  @keyframes arrive {
    from {
      opacity: 0;
      transform: translateY(0.25rem);
    }
    to {
      opacity: 1;
      transform: translateY(0);
    }
  }
  @keyframes blink {
    50% {
      opacity: 0;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .message,
    .evidence-card,
    .proposal-card,
    .confirmation-card {
      animation: none;
    }
    .transcript {
      scroll-behavior: auto;
    }
  }
</style>
