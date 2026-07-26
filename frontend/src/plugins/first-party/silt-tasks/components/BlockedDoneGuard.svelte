<script lang="ts">
  import BlockedDoneDialog from './BlockedDoneDialog.svelte'
  import type { BlockerRef } from '../shared.svelte'

  /**
   * Renders {@link BlockedDoneDialog} when `useBlockedDoneGuard` has a pending
   * blocked-DONE confirmation. The guard instance and confirm/cancel handlers
   * stay at each call site (Board reverts an optimistic move, List re-commits,
   * Drawer reverts the status radio — too divergent to fold in); this only
   * de-duplicates the `{#if pending}<BlockedDoneDialog/>{/if}` markup.
   *
   * `cardText` is a per-site prop because its source differs (pending.context.*
   * for Board/List, `task.clean_content` for the Drawer). Svelte 5 passes props
   * as lazy getters, so a call-site expression like
   * `blockedGuard.pending?.context.card.clean_content` is only evaluated once
   * `pending` is truthy — the undefined-when-null case never reaches the Dialog.
   */
  let {
    pending,
    cardText,
    onConfirm,
    onCancel
  }: {
    pending: { blockers: BlockerRef[] } | null
    cardText: string | undefined
    onConfirm: () => void
    onCancel: () => void
  } = $props()
</script>

{#if pending}
  <BlockedDoneDialog
    cardText={cardText ?? ''}
    blockers={pending.blockers}
    {onConfirm}
    {onCancel}
  />
{/if}
