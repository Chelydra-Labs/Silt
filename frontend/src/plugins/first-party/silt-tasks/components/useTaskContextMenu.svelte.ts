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
 * The closest focusable control at/inside `el` (the element itself when
 * focusable, else its first focusable descendant). Menu anchors that are not
 * focusable — the List row `div[role="listitem"]` — would make the
 * primitive's close-time focus restore a silent no-op and drop focus to
 * `<body>`, taking the hub's Ctrl+Z scope (TasksHub) with it; anchoring to a
 * real control inside the row keeps that scope alive.
 */
export function focusableTaskControl(
  el: HTMLElement | null
): HTMLElement | null {
  if (!el) return null
  if (el.tabIndex >= 0 && !el.matches(':disabled')) return el
  return el.querySelector<HTMLElement>(
    'button:not([disabled]), [href], input:not([disabled]), select, textarea, [tabindex]:not([tabindex="-1"])'
  )
}

/**
 * The neighboring task-surface element (row / card / chip — all carry
 * `data-task-hit`) after `anchor` in display order, falling back to the one
 * before it. Captured while the anchor still exists so a removal-triggering
 * write knows where keyboard focus should land.
 */
export function neighborTaskHit(anchor: HTMLElement): HTMLElement | null {
  const hits = Array.from(
    document.querySelectorAll<HTMLElement>('[data-task-hit]')
  )
  const idx = hits.indexOf(anchor)
  if (idx < 0) return null
  return hits[idx + 1] ?? hits[idx - 1] ?? null
}

// Removals land after the write resolves + block:changed's debounced reload,
// so the rescue polls past that window rather than checking once.
const REMOVAL_POLL_MS = 25
const REMOVAL_POLL_MAX = 40 // ~1s: write round-trip + 80ms reload debounce

/**
 * Keep keyboard focus inside the view after a menu write whose debounced
 * reload removes the anchored row/card/chip (Mark done on the List, delete
 * anywhere, pin/due re-bins). Until the anchor detaches there is nothing to
 * do; once it does — and only if focus actually fell to `<body>` rather than
 * the user having moved it — land on the captured neighbor (or the hub
 * heading when the surface emptied), preserving the hub's Ctrl+Z scope the
 * same way the row checkbox's removal path does.
 */
export function rescueFocusAfterWrite(
  anchor: HTMLElement,
  neighbor: HTMLElement | null
): void {
  const check = (attempt: number) => {
    if (!anchor.isConnected) {
      if (document.activeElement === document.body) {
        const target =
          focusableTaskControl(neighbor) ??
          document.querySelector<HTMLElement>('[data-tasks-hub] h1')
        target?.focus()
      }
      return
    }
    if (attempt >= REMOVAL_POLL_MAX) return
    setTimeout(() => check(attempt + 1), REMOVAL_POLL_MS)
  }
  check(0)
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
    const primary =
      target && target === document.activeElement
        ? target
        : e.currentTarget instanceof HTMLElement
          ? e.currentTarget
          : target
    menu = {
      task,
      x: e.clientX,
      y: e.clientY,
      anchorEl: focusableTaskControl(primary) ?? primary
    }
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
