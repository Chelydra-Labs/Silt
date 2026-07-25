<script lang="ts">
  import { fade } from 'svelte/transition'

  // F4: grants migration modal. The vault's legacy config.yaml carries a
  // grants block this host has never seen; the user can move them to
  // per-host storage (onConfirm) or dismiss to re-grant each plugin on first
  // use (onDecline — same path as Escape). Presentational only — the IPC
  // (ConfirmGrantsMigration / DeclineGrantsMigration) and error/notification
  // paths are owned by App and passed in as props.
  interface Props {
    open: boolean
    pendingLegacyGrants: Record<string, Record<string, string>>
    onDecline: () => void
    onConfirm: () => void
  }
  let { open, pendingLegacyGrants, onDecline, onConfirm }: Props = $props()
</script>

{#if open}
  <div
    class="settings-mismatch-overlay"
    role="alertdialog"
    aria-modal="true"
    aria-labelledby="grants-migration-title"
    aria-describedby="grants-migration-desc"
    tabindex="-1"
    onkeydown={(e) => {
      if (e.key === 'Escape') onDecline()
    }}
    transition:fade={{ duration: 150 }}
  >
    <div class="settings-mismatch-modal glass-palette-strong">
      <h2 id="grants-migration-title">Move plugin permissions</h2>
      <p id="grants-migration-desc">
        Silt is moving plugin permissions to per-host storage so they no longer
        travel with synced vaults.
        {Object.keys(pendingLegacyGrants).length}
        plugin(s) have existing permissions in this vault. Confirm to move them, or
        dismiss to re-grant each plugin on first use.
      </p>
      <div class="settings-mismatch-actions">
        <button class="secondary" onclick={onDecline}>Dismiss</button>
        <button class="primary" onclick={onConfirm}>Move permissions</button>
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
