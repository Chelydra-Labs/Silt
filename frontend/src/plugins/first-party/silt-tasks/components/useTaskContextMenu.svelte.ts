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
 * Task surfaces in display order, deduped to one entry per task. Board
 * cards emit a nested marker pair (TaskCard wrapper + card), and
 * Calendar overflow controls ("+N more", `aria-haspopup`) are not tasks;
 * indexing raw markers made a removed card's own wrapper — or an
 * overflow button that vanishes when the count drops — the "neighbor".
 */
function taskSurfaces(): HTMLElement[] {
  return Array.from(
    document.querySelectorAll<HTMLElement>(
      '[data-task-hit]:not([aria-haspopup])'
    )
  ).filter((el) => !el.parentElement?.closest('[data-task-hit]'))
}

/**
 * Positional handle on the neighbor to rescue focus onto after a
 * removal-triggering write. The neighbor's DOM node is not stable across
 * the debounced reload — Board cards and Calendar chips remount when the
 * data re-bins — so the rescue re-resolves by position against the live
 * surface list rather than holding a node that may be detached by then.
 */
export interface NeighborTaskHit {
  /** Index of the anchored surface in the hit list at capture time. */
  pos: number
  /** Which side the neighbor was on: +1 next, -1 previous. */
  dir: 1 | -1
}

/**
 * Locate the neighboring task surface (row / card / chip) after `anchor`
 * in display order, falling back to the one before it. Captured while
 * the anchor still exists so a removal-triggering write knows where
 * keyboard focus should land.
 */
export function neighborTaskHit(anchor: HTMLElement): NeighborTaskHit | null {
  // The anchor is a focusable control (focusableTaskControl) for the
  // primitive's close-time restore — on the List that is a button inside
  // the row, not the hit element itself — so climb back to the task
  // surface before indexing neighbors. Climb to the OUTERMOST marker:
  // TaskCard marks both its wrapper and the card itself.
  let hit = anchor.closest<HTMLElement>('[data-task-hit]') ?? anchor
  for (
    let outer = hit.parentElement?.closest<HTMLElement>('[data-task-hit]');
    outer;
    outer = hit.parentElement?.closest<HTMLElement>('[data-task-hit]')
  ) {
    hit = outer
  }
  const hits = taskSurfaces()
  const idx = hits.indexOf(hit)
  if (idx < 0) return null
  if (idx + 1 < hits.length) return { pos: idx, dir: 1 }
  if (idx > 0) return { pos: idx, dir: -1 }
  return null
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
  neighbor: NeighborTaskHit | null
): void {
  const check = (attempt: number) => {
    if (!anchor.isConnected) {
      if (document.activeElement === document.body) {
        // Re-resolve against the live surface list: the removal freed the
        // anchored slot, so what was "next" now sits at the captured pos
        // and "previous" one slot earlier — a re-binned remount that
        // swapped every node under us still yields a connected landing.
        const live = taskSurfaces()
        const neighborEl = neighbor
          ? ((neighbor.dir === 1
              ? live[neighbor.pos]
              : live[neighbor.pos - 1]) ?? null)
          : null
        // Container surfaces (List rows) resolve to their first focusable
        // control — the mark-done checkbox; `data-task-primary` marks the
        // labeled content control, the same landing the row checkbox's own
        // removal path uses (focusAfterRowRemoval). Self-focusable surfaces
        // (Board card, Calendar chip) have no marker and focus as
        // themselves. An emptied surface falls through to the hub heading,
        // keeping the Ctrl+Z scope alive instead of stranding <body>.
        const target =
          neighborEl?.querySelector<HTMLElement>('[data-task-primary]') ??
          focusableTaskControl(neighborEl) ??
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
