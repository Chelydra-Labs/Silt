/**
 * Document outline / TOC helpers (#659).
 * Walks headerBlock nodes in document order using stable block ids.
 */
import type { Editor } from '@tiptap/core'
import type { Node as PmNode } from '@tiptap/pm/model'

export interface OutlineHeading {
  id: string
  depth: number
  text: string
  pos: number
}

export function extractHeadings(doc: PmNode): OutlineHeading[] {
  const items: OutlineHeading[] = []
  doc.descendants((node, pos) => {
    if (node.type.name !== 'headerBlock') return
    const depth = Number(node.attrs.depth) || 1
    const id = (node.attrs.id as string) || `pos-${pos}`
    const text = node.textContent.trim() || 'Untitled heading'
    items.push({ id, depth, text, pos })
  })
  return items
}

export function extractHeadingsFromEditor(
  editor: Editor | null
): OutlineHeading[] {
  if (!editor || editor.isDestroyed) return []
  return extractHeadings(editor.state.doc)
}

/** Scroll the editor so the heading at `pos` is visible and place the caret. */
export function jumpToHeading(editor: Editor, pos: number): boolean {
  if (!editor || editor.isDestroyed) return false
  const docSize = editor.state.doc.content.size
  const safePos = Math.max(1, Math.min(pos + 1, docSize))
  return editor.chain().focus().setTextSelection(safePos).scrollIntoView().run()
}

/**
 * Pick the active heading for scroll-spy: last heading whose DOM top is at or
 * above the scroll parent's top + offset.
 */
export function activeHeadingId(
  headings: OutlineHeading[],
  scrollParent: HTMLElement | null,
  offset = 48
): string | null {
  if (!scrollParent || headings.length === 0) return null
  const parentTop = scrollParent.getBoundingClientRect().top + offset
  let active: string | null = headings[0]?.id ?? null
  for (const h of headings) {
    const el = scrollParent.querySelector(`[data-id="${CSS.escape(h.id)}"]`)
    if (!el) continue
    const top = el.getBoundingClientRect().top
    if (top <= parentTop) active = h.id
    else break
  }
  return active
}
