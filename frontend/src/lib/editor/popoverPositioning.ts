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

/**
 * Position a popover relative to an anchor, flipping above the anchor when it
 * would otherwise overflow the bottom of the viewport. Vertical flip is the
 * common overflow case (a palette opening at the last visible line); the
 * horizontal axis only clamps. Used by the slash palette and the table picker
 * so they share one tested decision instead of two inline copies.
 *
 * Pure: reads viewport + anchor + popover size from the args (jsdom-testable).
 */
export function flipOrClamp(
  anchor: AnchorRect,
  popover: { width: number; height: number },
  viewport: Viewport
): { left: number; top: number } {
  const opensAbove = anchor.top - popover.height - POPOVER_MARGIN
  // Flip up when there is no room below but there IS room above; otherwise
  // fall through to a plain clamp so the popover never overlaps the anchor.
  const preferAbove =
    anchor.bottom + popover.height > viewport.height - POPOVER_MARGIN &&
    opensAbove >= POPOVER_MARGIN
  const top = preferAbove ? opensAbove : anchor.bottom
  return clampToViewport(
    { x: anchor.left, y: top, width: popover.width, height: popover.height },
    viewport
  )
}
