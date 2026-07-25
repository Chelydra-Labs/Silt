<script lang="ts">
  import ModalShell from './ModalShell.svelte'

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

<ModalShell
  {open}
  titleId="settings-mismatch-title"
  descId="settings-mismatch-desc"
  title="Settings changed"
  onEscape={onClose}
>
  {#snippet description()}
    Silt's vault path or trusted-publishers list has changed since the last
    launch. Confirm this change is intentional. If you did not make this change,
    dismiss and verify your <code>settings.json</code>.
  {/snippet}
  {#snippet actions()}
    <button class="secondary" onclick={onClose}>Dismiss</button>
    <button class="primary" onclick={onConfirm}>Confirm change</button>
  {/snippet}
</ModalShell>
