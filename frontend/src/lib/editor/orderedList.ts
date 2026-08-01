import type { Node as ProseMirrorNode } from '@tiptap/pm/model'
import type { Transaction } from '@tiptap/pm/state'

/**
 * Ordered-list helpers for flat noteBlock lists (#836 Enter renumber, #837
 * indent/unindent). Markers stay GFM-simple (`N.` / `N)`); hierarchical
 * outline labels (1.1) are display-only.
 */

/** Parse `1. ` / `2) ` ordered markers; null for unordered/plain. */
export function parseOrderedBullet(
  bullet: string
): { n: number; punc: string } | null {
  const match = (bullet || '').match(/^(\d+)([.)]\s)$/)
  if (!match) return null
  return { n: parseInt(match[1], 10), punc: match[2] }
}

export function formatOrderedBullet(n: number, punc: string): string {
  return `${n}${punc}`
}

/**
 * After inserting an ordered noteBlock, renumber every following contiguous
 * same-depth ordered note with the same punctuation so mid-list Enter does
 * not leave duplicate numbers (2 → new 3, old 3 stays 3).
 *
 * Walks document order after `fromPos`. Deeper nested notes (attrs.depth >
 * depth) are skipped so a child under item 2 does not stop renumber of later
 * same-depth parents. Stops at a shallower block, non-note, or bullet-style break.
 */
export function renumberFollowingOrdered(
  tr: Transaction,
  fromPos: number,
  startNum: number,
  punc: string,
  depth: number
): Transaction {
  const startNode = tr.doc.nodeAt(fromPos)
  if (!startNode) return tr
  let pos = fromPos + startNode.nodeSize
  let expected = startNum + 1
  while (pos < tr.doc.content.size) {
    const node = tr.doc.nodeAt(pos)
    if (!node || node.type.name !== 'noteBlock') break
    const nodeDepth = (node.attrs.depth as number) || 0
    if (nodeDepth > depth) {
      pos += node.nodeSize
      continue
    }
    if (nodeDepth < depth) break
    const parsed = parseOrderedBullet(String(node.attrs.bullet || ''))
    if (!parsed || parsed.punc !== punc) break
    const want = formatOrderedBullet(expected, punc)
    if (String(node.attrs.bullet || '') !== want) {
      tr = tr.setNodeMarkup(pos, undefined, { ...node.attrs, bullet: want })
    }
    expected++
    const after = tr.doc.nodeAt(pos)
    if (!after) break
    pos += after.nodeSize
  }
  return tr
}

/**
 * Walk backward from `fromPos` to the first noteBlock of a contiguous
 * same-depth ordered run (same punctuation). Skips deeper nested notes.
 */
export function findOrderedRunStart(
  doc: ProseMirrorNode,
  fromPos: number,
  depth: number,
  punc: string
): number | null {
  const startNode = doc.nodeAt(fromPos)
  if (!startNode || startNode.type.name !== 'noteBlock') return null
  const startParsed = parseOrderedBullet(String(startNode.attrs.bullet || ''))
  if (!startParsed || startParsed.punc !== punc) return null
  if (((startNode.attrs.depth as number) || 0) !== depth) return null

  let runStart = fromPos
  let pos = fromPos
  while (pos > 0) {
    let before: ProseMirrorNode
    let beforePos: number
    try {
      const $pos = doc.resolve(pos)
      const prev = $pos.nodeBefore
      if (!prev) break
      before = prev
      beforePos = pos - prev.nodeSize
    } catch {
      break
    }
    if (before.type.name !== 'noteBlock') break
    const bd = (before.attrs.depth as number) || 0
    if (bd > depth) {
      pos = beforePos
      continue
    }
    if (bd < depth) break
    const parsed = parseOrderedBullet(String(before.attrs.bullet || ''))
    if (!parsed || parsed.punc !== punc) break
    runStart = beforePos
    pos = beforePos
  }
  return runStart
}

/**
 * Renumber a contiguous same-depth ordered run starting at `startPos`
 * (1, 2, 3, …) with fixed punctuation. Skips deeper nested notes.
 */
export function renumberOrderedRunFromStart(
  tr: Transaction,
  startPos: number,
  depth: number,
  punc: string
): Transaction {
  let expected = 1
  let pos = startPos
  while (pos < tr.doc.content.size) {
    const node = tr.doc.nodeAt(pos)
    if (!node || node.type.name !== 'noteBlock') break
    const nodeDepth = (node.attrs.depth as number) || 0
    if (nodeDepth > depth) {
      pos += node.nodeSize
      continue
    }
    if (nodeDepth < depth) break
    const parsed = parseOrderedBullet(String(node.attrs.bullet || ''))
    if (!parsed || parsed.punc !== punc) break
    const want = formatOrderedBullet(expected, punc)
    if (String(node.attrs.bullet || '') !== want) {
      tr = tr.setNodeMarkup(pos, undefined, { ...node.attrs, bullet: want })
    }
    expected++
    const after = tr.doc.nodeAt(pos)
    if (!after) break
    pos += after.nodeSize
  }
  return tr
}

/** Renumber the ordered run that contains `pos` at the given depth/punc. */
export function renumberOrderedRunContaining(
  tr: Transaction,
  pos: number,
  depth: number,
  punc: string
): Transaction {
  const start = findOrderedRunStart(tr.doc, pos, depth, punc)
  if (start == null) return tr
  return renumberOrderedRunFromStart(tr, start, depth, punc)
}

/**
 * After a depth change on an ordered noteBlock at `pos` (already applied on
 * `tr`), renumber the vacated old-depth run and the destination new-depth run.
 */
export function fixupOrderedAfterDepthChange(
  tr: Transaction,
  pos: number,
  oldDepth: number,
  newDepth: number,
  punc: string
): Transaction {
  if (oldDepth === newDepth) return tr

  // Destination run (includes the moved node).
  tr = renumberOrderedRunContaining(tr, pos, newDepth, punc)

  // Vacated run at old depth: find a same-depth ordered peer near `pos`.
  const peerPos = findNearbyOrderedPeer(tr.doc, pos, oldDepth, punc)
  if (peerPos != null) {
    tr = renumberOrderedRunContaining(tr, peerPos, oldDepth, punc)
  }
  return tr
}

/**
 * Find an ordered noteBlock at `depth` with `punc` near `fromPos` (previous
 * then next), skipping deeper nested notes. Used to locate the vacated run
 * after a node left that depth.
 */
function findNearbyOrderedPeer(
  doc: ProseMirrorNode,
  fromPos: number,
  depth: number,
  punc: string
): number | null {
  const node = doc.nodeAt(fromPos)
  if (!node) return null

  // Walk backward
  let pos = fromPos
  while (pos > 0) {
    let before: ProseMirrorNode
    let beforePos: number
    try {
      const $pos = doc.resolve(pos)
      const prev = $pos.nodeBefore
      if (!prev) break
      before = prev
      beforePos = pos - prev.nodeSize
    } catch {
      break
    }
    if (before.type.name !== 'noteBlock') break
    const bd = (before.attrs.depth as number) || 0
    if (bd > depth) {
      pos = beforePos
      continue
    }
    if (bd < depth) break
    const parsed = parseOrderedBullet(String(before.attrs.bullet || ''))
    if (parsed && parsed.punc === punc) return beforePos
    break
  }

  // Walk forward from after the node at fromPos
  pos = fromPos + node.nodeSize
  while (pos < doc.content.size) {
    const next = doc.nodeAt(pos)
    if (!next || next.type.name !== 'noteBlock') break
    const nd = (next.attrs.depth as number) || 0
    if (nd > depth) {
      pos += next.nodeSize
      continue
    }
    if (nd < depth) break
    const parsed = parseOrderedBullet(String(next.attrs.bullet || ''))
    if (parsed && parsed.punc === punc) return pos
    break
  }
  return null
}

/**
 * Apply depth change for a block on `tr`. For ordered noteBlocks, restarts
 * nested markers at 1 and renumbers affected same-depth runs (#837).
 */
export function applyDepthChangeOnTransaction(
  tr: Transaction,
  nodePos: number,
  newDepth: number
): Transaction {
  const node = tr.doc.nodeAt(nodePos)
  if (!node) return tr
  const oldDepth = (node.attrs.depth as number) || 0
  if (oldDepth === newDepth) return tr

  const parsed =
    node.type.name === 'noteBlock'
      ? parseOrderedBullet(String(node.attrs.bullet || ''))
      : null

  if (!parsed) {
    return tr.setNodeAttribute(nodePos, 'depth', newDepth)
  }

  // Temporary bullet; full run renumber assigns the real sequence.
  const tempBullet = formatOrderedBullet(1, parsed.punc)
  tr = tr.setNodeMarkup(nodePos, undefined, {
    ...node.attrs,
    depth: newDepth,
    bullet: tempBullet
  })
  return fixupOrderedAfterDepthChange(
    tr,
    nodePos,
    oldDepth,
    newDepth,
    parsed.punc
  )
}

/**
 * Hierarchical outline label for display (e.g. `1.1)` / `1.2.`). Uses
 * sequential position within each depth's ordered run — not multi-level
 * on-disk markers. Returns null when the node is not an ordered noteBlock.
 */
export function formatOrderedOutlineLabel(
  doc: ProseMirrorNode,
  nodePos: number
): string | null {
  const target = doc.nodeAt(nodePos)
  if (!target || target.type.name !== 'noteBlock') return null
  const targetParsed = parseOrderedBullet(String(target.attrs.bullet || ''))
  if (!targetParsed) return null

  const targetDepth = (target.attrs.depth as number) || 0
  // counters[d] = sequential index within the current ordered run at depth d
  const counters: number[] = []
  // Scan the same parent container as nodePos (siblings only).
  let parent: ProseMirrorNode = doc
  let scanFrom = 0
  try {
    const $pos = doc.resolve(nodePos)
    if ($pos.parent) {
      parent = $pos.parent
      scanFrom = $pos.start($pos.depth)
      // When parent is doc, start() is 0; children are direct.
      // nodePos is absolute; iterate parent.forEach with absolute positions.
    }
  } catch {
    return targetParsed.n + targetParsed.punc.trimEnd()
  }

  let foundPath: number[] = []
  let sawTarget = false
  parent.forEach((child, offset) => {
    if (sawTarget) return
    const absPos = scanFrom + offset
    if (child.type.name !== 'noteBlock') {
      // Non-note breaks all ordered runs in this container.
      counters.length = 0
      return
    }
    const d = (child.attrs.depth as number) || 0
    // Truncate deeper levels when moving up the outline.
    if (counters.length > d + 1) counters.length = d + 1

    const parsed = parseOrderedBullet(String(child.attrs.bullet || ''))
    if (parsed) {
      // Ensure parent levels exist (valid indent tree should already).
      while (counters.length < d) counters.push(1)
      if (counters.length === d) {
        counters.push(1)
      } else {
        // Same depth: next sibling in the ordered run.
        counters[d] = (counters[d] || 0) + 1
        counters.length = d + 1
      }
    } else if (counters.length > d) {
      // Non-ordered at depth d ends the run at d and deeper.
      counters.length = d
    }

    if (absPos === nodePos) {
      foundPath = counters.slice(0, targetDepth + 1)
      sawTarget = true
    }
  })

  if (!sawTarget || foundPath.length === 0) {
    return `${targetParsed.n}${targetParsed.punc}`.trimEnd()
  }
  const puncChar = targetParsed.punc.trim() // '.' or ')'
  return `${foundPath.join('.')}${puncChar}`
}
