import type { TaskDetail } from '../types'

/**
 * Open-task context-menu state (#1116): which task the menu targets, where it
 * anchors, and which element owns it (for the shared ContextMenu's scoped
 * scroll-dismissal + focus restore). One instance is hosted per view
 * (List / Board / Calendar), mirroring the SavedViews manage-menu shape.
 */
export interface TaskContextMenuState {
  task: TaskDetail
  x: number
  y: number
  anchorEl: HTMLElement | null
}

/**
 * Headless controller for the per-view TaskContextMenu host: `openFromEvent`
 * is the single entry point for both triggers — a real right-click and the
 * synthetic contextmenu that the shared context-menu-key helper dispatches at
 * the focused element (Shift+F10 / Menu key), so both run the exact same
 * per-node handler.
 */
export function useTaskContextMenu() {
  let menu = $state<TaskContextMenuState | null>(null)

  function openFromEvent(e: MouseEvent, task: TaskDetail): void {
    e.preventDefault()
    const target = e.target instanceof HTMLElement ? e.target : null
    // Keyboard invocation dispatches at the focused control (e.g. the List
    // row's content button — the row itself is not focusable), so anchoring
    // to the focused element keeps the primitive's close-time focus restore
    // on a focusable target. Mouse invocation anchors to the handler's
    // element (row / card / chip). Either way the anchor sits inside the
    // same scroll container, so scroll-dismiss scoping is identical.
    const anchorEl =
      target && target === document.activeElement
        ? target
        : e.currentTarget instanceof HTMLElement
          ? e.currentTarget
          : target
    menu = { task, x: e.clientX, y: e.clientY, anchorEl }
  }

  function close(): void {
    menu = null
  }

  return {
    get menu() {
      return menu
    },
    openFromEvent,
    close
  }
}

export type TaskContextMenuController = ReturnType<typeof useTaskContextMenu>
