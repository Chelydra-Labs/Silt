<script lang="ts">
  import { fade } from 'svelte/transition'
  import type { Snippet } from 'svelte'

  // Shared chrome for the small alert-style modals (settings mismatch,
  // grants migration, quarantined links). Owns the overlay, the glass
  // surface, the title/description typography, and the action-button
  // styling so each dialog carries only its own copy + actions. The open
  // flag, the IPC handlers, and the per-dialog Escape routing stay with the
  // caller via onEscape — GrantsMigration intentionally wires Escape to
  // onDecline (a side effect), so Escape is not hard-coded here.
  interface Props {
    open: boolean
    titleId: string
    descId: string
    title: string
    onEscape: () => void
    description: Snippet
    actions: Snippet
  }
  let { open, titleId, descId, title, onEscape, description, actions }: Props =
    $props()
</script>

{#if open}
  <div
    class="overlay"
    role="alertdialog"
    aria-modal="true"
    aria-labelledby={titleId}
    aria-describedby={descId}
    tabindex="-1"
    onkeydown={(e) => {
      if (e.key === 'Escape') onEscape()
    }}
    transition:fade={{ duration: 150 }}
  >
    <div class="surface glass-palette-strong">
      <h2 id={titleId}>{title}</h2>
      <p id={descId}>{@render description()}</p>
      <div class="actions">
        {@render actions()}
      </div>
    </div>
  </div>
{/if}

<style>
  .overlay {
    position: fixed;
    inset: 0;
    z-index: 10000;
    display: flex;
    align-items: center;
    justify-content: center;
    background: rgba(0, 0, 0, 0.4);
    backdrop-filter: blur(2px);
  }

  .surface {
    max-width: 460px;
    padding: 28px 32px;
    border-radius: 12px;
    border: 1px solid var(--color-surface-modal-border);
    box-shadow: 0 16px 48px rgba(0, 0, 0, 0.4);
  }

  .surface h2 {
    margin: 0 0 12px;
    font-size: 1.15rem;
    color: var(--color-text-primary);
  }

  .surface p {
    margin: 0 0 20px;
    font-size: 0.9rem;
    line-height: 1.5;
    color: var(--color-text-muted);
  }

  /* <code> appears in the settings-mismatch description snippet. */
  .surface :global(code) {
    padding: 1px 4px;
    border-radius: 3px;
    background: rgba(255, 255, 255, 0.08);
    font-family: var(--font-mono, monospace);
    font-size: 0.85em;
  }

  .actions {
    display: flex;
    justify-content: flex-end;
    gap: 10px;
  }

  /* Action buttons are rendered via the caller's snippet, so their rules
     use :global to reach the slotted buttons while staying scoped to this
     .actions container. */
  .actions :global(button) {
    padding: 8px 18px;
    border: none;
    border-radius: 8px;
    font-size: 0.875rem;
    cursor: pointer;
    transition: all 150ms var(--transition-standard);
  }

  .actions :global(button:focus-visible) {
    outline: 2px solid var(--color-accent-primary-start);
    outline-offset: 2px;
  }

  .actions :global(.secondary) {
    background: transparent;
    color: var(--color-text-muted);
    border: 1px solid var(--color-surface-modal-border);
  }

  .actions :global(.secondary:hover) {
    background: var(--color-hover);
    color: var(--color-text-primary);
    border-color: var(--color-border-active);
  }

  .actions :global(.primary) {
    background: var(--color-accent-primary-start);
    color: var(--color-surface-app);
    font-weight: 600;
  }

  .actions :global(.primary:hover) {
    filter: brightness(1.1);
    box-shadow: 0 0 12px var(--color-accent-primary-glow);
  }
</style>
