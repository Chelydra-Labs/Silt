<script lang="ts">
  import PageHistoryModal from './PageHistoryModal.svelte'
  import {
    OPEN_PAGE_HISTORY_EVENT,
    type OpenPageHistoryDetail
  } from './openPageHistory'

  let target = $state<OpenPageHistoryDetail | null>(null)

  function onOpen(event: Event): void {
    const detail = (event as CustomEvent<OpenPageHistoryDetail>).detail
    if (!detail?.notebook || !detail.page || !detail.nonce) return
    target = { ...detail, section: detail.section ?? '' }
  }

  $effect(() => {
    window.addEventListener(OPEN_PAGE_HISTORY_EVENT, onOpen)
    return () => window.removeEventListener(OPEN_PAGE_HISTORY_EVENT, onOpen)
  })
</script>

{#if target}
  {#key target.nonce}
    <PageHistoryModal
      notebook={target.notebook}
      section={target.section}
      page={target.page}
      onClose={() => (target = null)}
    />
  {/key}
{/if}
