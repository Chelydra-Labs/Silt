<script lang="ts">
  import ModalShell from './ModalShell.svelte'

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

<ModalShell
  {open}
  titleId="quarantine-title"
  descId="quarantine-desc"
  title="Linked notebook moved or tampered"
  onEscape={onClose}
>
  {#snippet description()}
    {#each quarantinedLinks as q (q.id)}
      <strong>{q.display_name}</strong> has moved or been tampered with. Re-link it
      or unlink it.
    {/each}
  {/snippet}
  {#snippet actions()}
    {#each quarantinedLinks as q (q.id)}
      <button class="secondary" onclick={() => onUnlink(q.id)}
        >Unlink {q.display_name}</button
      >
      <button class="primary" onclick={() => onRelink(q.id)}
        >Re-link {q.display_name}</button
      >
    {/each}
  {/snippet}
</ModalShell>
