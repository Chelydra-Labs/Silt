import type { Editor } from '@tiptap/core'

/** Maps an editor document position to the popup's viewport anchor. */
export function popupCoordsAt(
  editor: Pick<Editor, 'view'>,
  from: number
): { left: number; top: number } {
  const coords = editor.view.coordsAtPos(from)
  return { left: coords.left, top: coords.bottom }
}
