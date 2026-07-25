<script lang="ts">
  import { fade } from 'svelte/transition'

  // F3: linked-notebook quarantine modal. The root of one or more linked
  // notebooks was moved or tampered with; each row offers re-link
  // (PickLinkedNotebook after UnlinkNotebook) or unlink (UnlinkNotebook).
  // Presentational only — the IPC and error-notification paths are owned by
  // App and passed in as props. The open flag is `quarantinedLinks.length > 0`.
  export interface QuarantinedLink {
    id: string
    display_name: string
    root_path: string
  }

  interface Props {
    open: boolean
    quarantinedLinks: QuarantinedLink[]
    onClose: () => void
    onUnlink: (id: string) => void
    onRelink: (id: string) => void
  }
  let { open, quarantinedLinks, onClose, onUnlink, onRelink }: Props = $props()
</script>

{#if open}
  <div
    class="settings-mismatch-overlay"
    role="alertdialog"
    aria-modal="true"
    aria-labelledby="quarantine-title"
    aria-describedby="quarantine-desc"
    tabindex="-1"
    onkeydown={(e) => {
      if (e.key === 'Escape') onClose()
    }}
    transition:fade={{ duration: 150 }}
  >
    <div class="settings-mismatch-modal glass-palette-strong">
      <h2 id="quarantine-title">Linked notebook moved or tampered</h2>
      <p id="quarantine-desc">
        {#each quarantinedLinks as q (q.id)}
          <strong>{q.display_name}</strong> has moved or been tampered with. Re-link
          it or unlink it.
        {/each}
      </p>
      <div class="settings-mismatch-actions">
        {#each quarantinedLinks as q (q.id)}
          <button class="secondary" onclick={() => onUnlink(q.id)}
            >Unlink {q.display_name}</button
          >
          <button class="primary" onclick={() => onRelink(q.id)}
            >Re-link {q.display_name}</button
          >
        {/each}
      </div>
    </div>
  </div>
{/if}

<style>
  .settings-mismatch-overlay {
    position: fixed;
    inset: 0;
    z-index: 10000;
    display: flex;
    align-items: center;
    justify-content: center;
    background: rgba(0, 0, 0, 0.4);
    backdrop-filter: blur(2px);
  }

  .settings-mismatch-modal {
    max-width: 460px;
    padding: 28px 32px;
    border-radius: 12px;
    border: 1px solid var(--color-surface-modal-border);
    box-shadow: 0 16px 48px rgba(0, 0, 0, 0.4);
  }

  .settings-mismatch-modal h2 {
    margin: 0 0 12px;
    font-size: 1.15rem;
    color: var(--color-text-primary);
  }

  .settings-mismatch-modal p {
    margin: 0 0 20px;
    font-size: 0.9rem;
    line-height: 1.5;
    color: var(--color-text-muted);
  }

  .settings-mismatch-actions {
    display: flex;
    justify-content: flex-end;
    gap: 10px;
  }

  .settings-mismatch-actions button {
    padding: 8px 18px;
    border: none;
    border-radius: 8px;
    font-size: 0.875rem;
    cursor: pointer;
    transition: all 150ms var(--transition-standard);
  }

  .settings-mismatch-actions button:focus-visible {
    outline: 2px solid var(--color-accent-primary-start);
    outline-offset: 2px;
  }

  .settings-mismatch-actions .secondary {
    background: transparent;
    color: var(--color-text-muted);
    border: 1px solid var(--color-surface-modal-border);
  }

  .settings-mismatch-actions .secondary:hover {
    background: var(--color-hover);
    color: var(--color-text-primary);
    border-color: var(--color-border-active);
  }

  .settings-mismatch-actions .primary {
    background: var(--color-accent-primary-start);
    color: var(--color-surface-app);
    font-weight: 600;
  }

  .settings-mismatch-actions .primary:hover {
    filter: brightness(1.1);
    box-shadow: 0 0 12px var(--color-accent-primary-glow);
  }
</style>
