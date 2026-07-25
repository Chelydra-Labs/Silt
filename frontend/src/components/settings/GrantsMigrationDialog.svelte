<script lang="ts">
  import ModalShell from './ModalShell.svelte'

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

<ModalShell
  {open}
  titleId="grants-migration-title"
  descId="grants-migration-desc"
  title="Move plugin permissions"
  onEscape={onDecline}
>
  {#snippet description()}
    Silt is moving plugin permissions to per-host storage so they no longer
    travel with synced vaults.
    {Object.keys(pendingLegacyGrants).length}
    plugin(s) have existing permissions in this vault. Confirm to move them, or dismiss
    to re-grant each plugin on first use.
  {/snippet}
  {#snippet actions()}
    <button class="secondary" onclick={onDecline}>Dismiss</button>
    <button class="primary" onclick={onConfirm}>Move permissions</button>
  {/snippet}
</ModalShell>
