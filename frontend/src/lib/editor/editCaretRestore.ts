// Caret restore across Edit→Source→Edit (#331).
//
// TipTap is fully torn down in Source mode, so raw ProseMirror positions are
// useless after remount. We snapshot a stable block id + relative offset inside
// that block's content, then re-resolve against the rebuilt doc.

import type { Editor } from '@tiptap/core'
import type { Node as ProseMirrorNode } from '@tiptap/pm/model'
import { findActiveBlock } from './keymaps'

export type EditCaretSnapshot = {
  blockId: string
  /** Offset from the start of the block's content (not a raw doc position). */
  offsetInBlock: number
}

/**
 * Capture the caret relative to the enclosing Silt block. Returns null when
 * there is no active block or the block has no stable id (nothing to re-find).
 */
export function snapshotEditCaret(
  editor: Editor | null | undefined
): EditCaretSnapshot | null {
  if (!editor?.state?.selection?.$from) return null
  try {
    const active = findActiveBlock(editor)
    if (!active) return null
    const blockId = active.node.attrs?.id as string | null | undefined
    if (!blockId) return null
    const $from = editor.state.selection.$from
    // start(depth) is the first content position inside the block node.
    const offsetInBlock = Math.max(0, $from.pos - $from.start(active.depth))
    return { blockId, offsetInBlock }
  } catch {
    // Incomplete editor stubs / torn-down state — scroll-only fallback.
    return null
  }
}

/**
 * Resolve a caret snapshot to a document position in a (possibly rebuilt) doc.
 * Clamps the offset when the block shrank. Returns null if the block id is gone.
 */
export function resolveCaretInDoc(
  doc: ProseMirrorNode,
  snapshot: EditCaretSnapshot
): number | null {
  let matchPos = -1
  let matchNode: ProseMirrorNode | null = null
  doc.descendants((node, pos) => {
    if (matchNode) return false
    if (node.attrs?.id === snapshot.blockId) {
      matchPos = pos
      matchNode = node
      return false
    }
    return true
  })
  if (!matchNode || matchPos < 0) return null
  // Content lives between the open/close tokens; content.size is the max offset.
  const maxOffset = (matchNode as ProseMirrorNode).content.size
  const offset = Math.min(Math.max(0, snapshot.offsetInBlock), maxOffset)
  return matchPos + 1 + offset
}
