/**
 * Pure helpers for selection-bubble show timing (pointer-up gate).
 * Kept free of DOM so Vitest can pin the hold/publish contract without
 * mounting TipTapEditor.
 */

export type BubbleCoords = {
  left: number
  top: number
  bottom: number
}

/**
 * While the pointer is down (drag-select), withhold published coords and
 * stash the latest pending range. On release, prefer a fresh read.
 */
export function gateBubbleCoords(
  pointerDown: boolean,
  pending: BubbleCoords | null,
  next: BubbleCoords | null
): { published: BubbleCoords | null; pending: BubbleCoords | null } {
  if (pointerDown) {
    return { published: null, pending: next }
  }
  return { published: next ?? pending, pending: null }
}
