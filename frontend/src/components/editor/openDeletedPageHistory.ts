export const OPEN_DELETED_PAGE_HISTORY_EVENT =
  'silt:open-deleted-page-history' as const

export interface OpenDeletedPageHistoryDetail {
  nonce: string
}

export function openDeletedPageHistory(): void {
  window.dispatchEvent(
    new CustomEvent(OPEN_DELETED_PAGE_HISTORY_EVENT, {
      detail: { nonce: `${Date.now()}-${Math.random()}` }
    })
  )
}

declare global {
  interface WindowEventMap {
    'silt:open-deleted-page-history': CustomEvent<OpenDeletedPageHistoryDetail>
  }
}
