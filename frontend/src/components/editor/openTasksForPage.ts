// Cross-module contract for the "open tasks for page" utility-bar action.
//
// Lives in a plain .ts module (not the EditorUtilityBar.svelte <script module>)
// so the const + interface are unambiguously importable as named members —
// the ambient *.svelte TypeScript shim does not reliably resolve named
// exports from a .svelte file across the toolchain.

export const OPEN_TASKS_FOR_PAGE_EVENT = 'silt:open-tasks-for-page' as const

export interface OpenTasksForPageDetail {
  source: string
  notebook: string
  section: string
  page: string
  nonce: string
}

declare global {
  interface WindowEventMap {
    'silt:open-tasks-for-page': CustomEvent<OpenTasksForPageDetail>
  }
}
