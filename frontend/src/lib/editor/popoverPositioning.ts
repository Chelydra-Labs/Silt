/**
 * Clamp a popover's intended top-left position so the popover stays inside the
 * viewport with an 8px margin. Used by every selection-anchored popover
 * (slash menu, context menu, link input, color picker, meta-suggest) — the
 * previous inline copies had subtly hard-coded widths/heights that drifted
 * from the actual popover size.
 *
 * Pure: reads viewport dimensions from the args so it is unit-testable.
 */
export interface Viewport {
  width: number
  height: number
}

export interface BoundedRect {
  /** Intended top-left X (e.g. the cursor's `clientX`). */
  x: number
  /** Intended top-left Y (e.g. the cursor's `clientY`). */
  y: number
  /** Width of the popover that will hang off (x, y). */
  width: number
  /** Height of the popover that will hang off (x, y). */
  height: number
}

export const POPOVER_MARGIN = 8

/**
 * Nearest scrollable ancestor of `el`, or `document` when none exists.
 * Used to scope popover scroll-dismiss so unrelated regions do not close menus.
 */
export function findScrollableAncestor(
  el: HTMLElement | null
): HTMLElement | Document {
  if (!el) return document
  let current: HTMLElement | null = el.parentElement
  while (current) {
    const style = window.getComputedStyle(current)
    if (
      style.overflowY === 'auto' ||
      style.overflowY === 'scroll' ||
      style.overflow === 'auto' ||
      style.overflow === 'scroll'
    ) {
      return current
    }
    current = current.parentElement
  }
  return document
}

/**
 * Returns the clamped `{ left, top }` so the popover is fully on-screen.
 * If the popover is larger than the viewport, both axes fall back to the
 * margin so it stays anchored at the top-left rather than going negative.
 */
export function clampToViewport(
  rect: BoundedRect,
  viewport: Viewport
): { left: number; top: number } {
  let { x: left, y: top } = rect
  const { width, height } = rect
  if (left + width > viewport.width) {
    left = viewport.width - width - POPOVER_MARGIN
  }
  if (top + height > viewport.height) {
    top = viewport.height - height - POPOVER_MARGIN
  }
  left = Math.max(POPOVER_MARGIN, left)
  top = Math.max(POPOVER_MARGIN, top)
  return { left, top }
}

export interface AnchorRect {
  /** Anchor top (e.g. cursor top, `coordsAtPos().top`). */
  top: number
  /** Anchor bottom (e.g. `coordsAtPos().bottom`). */
  bottom: number
  /** Anchor left (e.g. `coordsAtPos().left`). */
  left: number
}

export type FlipPlacement = 'below' | 'above'

/**
 * Position a popover relative to an anchor. Default placement is below the
 * anchor (slash palette / table picker). Pass `placement: 'above'` for
 * selection bubbles so the toolbar does not cover selected text — flips below
 * only when there is no room above.
 *
 * Pure: reads viewport + anchor + popover size from the args (jsdom-testable).
 */
export function flipOrClamp(
  anchor: AnchorRect,
  popover: { width: number; height: number },
  viewport: Viewport,
  opts?: { placement?: FlipPlacement }
): { left: number; top: number } {
  const placement = opts?.placement ?? 'below'
  const opensAbove = anchor.top - popover.height - POPOVER_MARGIN
  const opensBelow = anchor.bottom + POPOVER_MARGIN
  const roomBelow =
    anchor.bottom + popover.height <= viewport.height - POPOVER_MARGIN
  const roomAbove = opensAbove >= POPOVER_MARGIN

  let top: number
  if (placement === 'above') {
    // Prefer above; flip below only when above would clip.
    top = roomAbove ? opensAbove : opensBelow
  } else {
    // Prefer below; flip above when below would clip and above fits.
    top = !roomBelow && roomAbove ? opensAbove : opensBelow
  }
  return clampToViewport(
    { x: anchor.left, y: top, width: popover.width, height: popover.height },
    viewport
  )
}
