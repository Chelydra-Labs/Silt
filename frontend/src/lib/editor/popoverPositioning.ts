/**
 * Clamp a popover's intended top-left position so the popover stays inside the
 * viewport with an 8px margin. Used by every selection-anchored popover
 * (slash menu, context menu, link input, color picker, meta-suggest) — the
 * previous inline copies had subtly hard-coded widths/heights that drifted
 * from the actual popover size.
 *
 * Callers anchored to a pane can pass `horizontal` to pin the popover inside
 * that column in viewport coordinates; the viewport stays the outer bound so a
 * pane wider than the window still cannot push the popover off-screen.
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

/** Viewport-space horizontal band a popover should stay inside. */
export interface PopoverHorizontalBounds {
  left: number
  right: number
}

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
 *
 * When `horizontal` is given the left edge is additionally held inside that
 * band. A band narrower than the popover pins the left edge to the band's
 * start: overflowing the far side is the only alternative, and doing so would
 * put the popover back over whatever the band exists to avoid — unless the
 * band's start itself lies past the window's right edge, where the viewport
 * (the outermost bound) takes precedence.
 */
export function clampToViewport(
  rect: BoundedRect,
  viewport: Viewport,
  horizontal?: PopoverHorizontalBounds
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
  if (horizontal) {
    // The viewport margin floor wins over a band that starts off-screen.
    const minLeft = Math.max(POPOVER_MARGIN, horizontal.left + POPOVER_MARGIN)
    const maxLeft = horizontal.right - width - POPOVER_MARGIN
    const bandLeft =
      maxLeft < minLeft ? minLeft : Math.min(maxLeft, Math.max(minLeft, left))
    // The viewport is the outermost bound: a band that starts past the window's
    // right edge must not carry the popover off-screen with it.
    const viewportMax = viewport.width - width - POPOVER_MARGIN
    left = Math.max(POPOVER_MARGIN, Math.min(viewportMax, bandLeft))
  }
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
  opts?: { placement?: FlipPlacement; horizontal?: PopoverHorizontalBounds }
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
    viewport,
    opts?.horizontal
  )
}
