// Cross-module contract for the "open page history" utility-bar / sidebar action.
//
// Lives in a plain .ts module (not a .svelte <script module>) so the const +
// interface are unambiguously importable as named members — same reason
// openTasksForPage.ts is a standalone module.

export const OPEN_PAGE_HISTORY_EVENT = 'silt:open-page-history' as const

export interface OpenPageHistoryDetail {
  notebook: string
  section: string
  page: string
  nonce: string
}

declare global {
  interface WindowEventMap {
    'silt:open-page-history': CustomEvent<OpenPageHistoryDetail>
  }
}
