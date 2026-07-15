<script lang="ts">
  // Phase 5 staging confirmation banner (#605). Renders when the agent loop
  // pauses on a staged destructive op. Confirm commits the op; Reject marks
  // the token consumed without executing. The dialog is keyboard-operable:
  // Confirm is auto-focused on mount, Escape rejects, and the live region
  // announces the operation summary for screen readers.
  import type { StagingEvent } from './agent-loop'

  interface Props {
    event: StagingEvent
    onConfirm: (token: string) => void
    onReject: (token: string) => void
  }

  let { event, onConfirm, onReject }: Props = $props()

  let confirmBtn = $state<HTMLButtonElement | null>(null)

  // Autofocus the Confirm button so a press of Enter confirms (the primary
  // path) without forcing the user to tab. $effect runs after mount.
  $effect(() => {
    confirmBtn?.focus()
  })

  function onKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      e.preventDefault()
      onReject(event.token)
    } else if (
      e.key === 'Enter' &&
      (e.target as HTMLElement)?.tagName !== 'BUTTON'
    ) {
      // Enter on the dialog body confirms; on a button it activates that button.
      e.preventDefault()
      onConfirm(event.token)
    }
  }

  function confirmClick() {
    onConfirm(event.token)
  }
  function rejectClick() {
    onReject(event.token)
  }

  // Friendly display strings per operation kind. Falls back to the raw kind
  // (Title-Cased) for ops not listed here — Phase 6 tools register their own
  // kinds, so a default keeps the dialog readable while new tools land.
  const KIND_LABELS: Record<string, string> = {
    delete_blocks: 'Delete blocks',
    merge_pages: 'Merge pages',
    rename_tag: 'Rename tag',
    bulk_update: 'Bulk update'
  }
  function kindLabel(kind: string): string {
    return (
      KIND_LABELS[kind] ??
      kind.replace(/_/g, ' ').replace(/\b\w/g, (c) => c.toUpperCase())
    )
  }

  const title = $derived(kindLabel(event.preview.kind))
  // The summary is the headline (e.g. "Delete 3 blocks in Work/Notes/Decisions");
  // details are the optional longer breakdown. Both go in the live region.
  const summary = $derived(
    event.preview.affectedCount != null
      ? `${title}: ${event.preview.summary} (${event.preview.affectedCount} item(s) affected)`
      : `${title}: ${event.preview.summary}`
  )
</script>

<!-- role="dialog" + aria-modal makes screen readers treat this as a blocking
     prompt; aria-labelledby points at the headline, aria-describedby at the
     detail body. tabindex=-1 lets the dialog receive focus for key handling. -->
<div
  class="staging"
  role="dialog"
  aria-modal="true"
  aria-labelledby="staging-title"
  aria-describedby="staging-summary"
  tabindex="-1"
  onkeydown={onKeydown}
>
  <div class="staging-icon-row">
    <span class="material-symbols-outlined staging-icon" aria-hidden="true"
      >warning</span
    >
    <h3 id="staging-title" class="staging-title">{title}</h3>
  </div>
  <!-- aria-live=assertive so a screen reader announces the staged op the
       moment it appears. -->
  <p id="staging-summary" class="staging-summary" aria-live="assertive">
    {summary}
  </p>
  {#if event.preview.details}
    <pre class="staging-details" aria-live="polite">{event.preview
        .details}</pre>
  {/if}
  <div class="staging-actions">
    <button
      type="button"
      class="staging-btn reject"
      aria-label="Reject operation"
      onclick={rejectClick}
    >
      Reject
    </button>
    <button
      type="button"
      class="staging-btn confirm"
      aria-label="Confirm operation"
      bind:this={confirmBtn}
      onclick={confirmClick}
    >
      Confirm
    </button>
  </div>
</div>

<style>
  .staging {
    margin: 0.5rem 0.75rem;
    padding: 0.6rem 0.7rem;
    border: 1px solid var(--color-status-warn, #fbbf24);
    border-left-width: 3px;
    border-radius: 0.45rem;
    background: color-mix(
      in srgb,
      var(--color-status-warn, #fbbf24) 8%,
      var(--color-surface-panel, transparent)
    );
    display: flex;
    flex-direction: column;
    gap: 0.35rem;
  }
  .staging-icon-row {
    display: flex;
    align-items: center;
    gap: 0.4rem;
  }
  .staging-icon {
    color: var(--color-status-warn, #fbbf24);
    font-size: 1.1rem;
  }
  .staging-title {
    margin: 0;
    font-size: 0.875rem;
    font-weight: 600;
    color: var(--color-text-primary, currentColor);
  }
  .staging-summary {
    margin: 0;
    line-height: 1.45;
    color: var(--color-text-primary, currentColor);
  }
  .staging-details {
    margin: 0;
    padding: 0.4rem 0.5rem;
    background: var(--color-surface-input, #121216);
    border-radius: 0.35rem;
    font-family: ui-monospace, monospace;
    font-size: 0.72rem;
    white-space: pre-wrap;
    word-break: break-word;
    max-height: 8rem;
    overflow-y: auto;
    color: var(--color-text-primary, currentColor);
  }
  .staging-actions {
    display: flex;
    gap: 0.4rem;
    justify-content: flex-end;
    margin-top: 0.15rem;
  }
  .staging-btn {
    border: none;
    border-radius: 0.45rem;
    padding: 0.35rem 0.85rem;
    font: inherit;
    font-weight: 600;
    cursor: pointer;
  }
  .staging-btn:focus-visible {
    outline: 2px solid var(--color-accent-primary-start, #2dd4bf);
    outline-offset: 1px;
  }
  .staging-btn.confirm {
    background: var(--color-accent-primary-start, #2dd4bf);
    color: #04201d;
  }
  .staging-btn.reject {
    background: transparent;
    color: var(--color-text-primary, currentColor);
    border: 1px solid var(--color-surface-panel-border, #2a2a30);
  }
  .staging-btn.reject:hover,
  .staging-btn.reject:focus-visible {
    border-color: var(--color-status-danger, #f43f5e);
    color: var(--color-status-danger, #f43f5e);
  }
</style>
