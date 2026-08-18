<script lang="ts">
  import PageHistoryModal from './PageHistoryModal.svelte'
  import DeletedPageHistoryModal from './DeletedPageHistoryModal.svelte'
  import {
    OPEN_PAGE_HISTORY_EVENT,
    type OpenPageHistoryDetail
  } from './openPageHistory'
  import {
    OPEN_DELETED_PAGE_HISTORY_EVENT,
    type OpenDeletedPageHistoryDetail
  } from './openDeletedPageHistory'

  let target = $state<OpenPageHistoryDetail | null>(null)
  let deletedNonce = $state<string | null>(null)

  function onOpen(event: Event): void {
    const detail = (event as CustomEvent<OpenPageHistoryDetail>).detail
    if (!detail?.notebook || !detail.page || !detail.nonce) return
    deletedNonce = null
    target = { ...detail, section: detail.section ?? '' }
  }

  function onOpenDeleted(event: Event): void {
    const detail = (event as CustomEvent<OpenDeletedPageHistoryDetail>).detail
    if (!detail?.nonce) return
    target = null
    deletedNonce = detail.nonce
  }

  $effect(() => {
    window.addEventListener(OPEN_PAGE_HISTORY_EVENT, onOpen)
    window.addEventListener(OPEN_DELETED_PAGE_HISTORY_EVENT, onOpenDeleted)
    return () => {
      window.removeEventListener(OPEN_PAGE_HISTORY_EVENT, onOpen)
      window.removeEventListener(OPEN_DELETED_PAGE_HISTORY_EVENT, onOpenDeleted)
    }
  })
</script>

{#if deletedNonce}
  {#key deletedNonce}
    <DeletedPageHistoryModal onClose={() => (deletedNonce = null)} />
  {/key}
{:else if target}
  {#key target.nonce}
    <PageHistoryModal
      notebook={target.notebook}
      section={target.section}
      page={target.page}
      onClose={() => (target = null)}
    />
  {/key}
{/if}
