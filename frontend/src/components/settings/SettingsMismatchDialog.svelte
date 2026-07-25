<script lang="ts">
  import { fade } from 'svelte/transition'

  // F20: trust-anchor fingerprint mismatch modal. The backend detected that
  // vault_path or trusted_publishers changed since the last launch (possible
  // tampering, or a legit external edit). Presentational only — the open
  // flag, the ConfirmSettingsChange IPC, and the error-notification path are
  // owned by App and passed in as props.
  interface Props {
    open: boolean
    onClose: () => void
    onConfirm: () => void
  }
  let { open, onClose, onConfirm }: Props = $props()
</script>

{#if open}
  <div
    class="settings-mismatch-overlay"
    role="alertdialog"
    aria-modal="true"
    aria-labelledby="settings-mismatch-title"
    aria-describedby="settings-mismatch-desc"
    tabindex="-1"
    onkeydown={(e) => {
      if (e.key === 'Escape') onClose()
    }}
    transition:fade={{ duration: 150 }}
  >
    <div class="settings-mismatch-modal glass-palette-strong">
      <h2 id="settings-mismatch-title">Settings changed</h2>
      <p id="settings-mismatch-desc">
        Silt's vault path or trusted-publishers list has changed since the last
        launch. Confirm this change is intentional. If you did not make this
        change, dismiss and verify your <code>settings.json</code>.
      </p>
      <div class="settings-mismatch-actions">
        <button class="secondary" onclick={onClose}>Dismiss</button>
        <button class="primary" onclick={onConfirm}>Confirm change</button>
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

  .settings-mismatch-modal code {
    padding: 1px 4px;
    border-radius: 3px;
    background: rgba(255, 255, 255, 0.08);
    font-family: var(--font-mono, monospace);
    font-size: 0.85em;
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
