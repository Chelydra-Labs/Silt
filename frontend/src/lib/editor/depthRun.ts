import type { Node as ProseMirrorNode } from '@tiptap/pm/model'
import { Fragment } from '@tiptap/pm/model'
import { MAX_DEPTH } from './constants'

// Subtree-aware block relocation, shared by the Alt+Arrow mover
// (keymaps.ts) and the drag indent-on-drop handler (dragIndentDrop.ts).
// Both consumers relocate a block together with its contiguous depth run —
// the same run the Tab/Shift-Tab indent family shifts — so no child is ever
// stranded before a block it no longer belongs under. Everything here is
// pure over the doc so the relocation math is unit-testable in jsdom (the
// HTML5 drag pipeline itself is not drivable there).

/**
 * Block node types that carry an outliner `depth` attr and participate in
 * the indent/outdent model. Two exclusions, both deliberate:
 * - `headerBlock`: its `depth` attr is the heading LEVEL (1-6), not an
 *   indent — it never joins a run as a MEMBER (a header terminates every
 *   scan), though it can HEAD a run: its body notes at level+1 follow it
 *   through moves, exactly like the Backspace unindent treats it.
 * - regions (callout/code/table/details and other depth-carrying atomic
 *   blocks): they join runs as MEMBERS when deeper than the head (#1061)
 *   but never INITIATE one — the set guards initiation (Tab on a region
 *   falls through to TipTap defaults), so regions stay out of it.
 */
export const DEPTH_BLOCK_TYPES = new Set(['noteBlock', 'taskBlock'])

/** Can a block of `nodeType` start a depth run that relocation sweeps up? */
export function initiatesDepthRun(nodeType: string): boolean {
  return DEPTH_BLOCK_TYPES.has(nodeType) || nodeType === 'headerBlock'
}

/** A contiguous depth run: geometry plus membership. */
export interface DepthRunSpan {
  /** Member start positions in document order (head first). */
  positions: number[]
  /** Member nodes in document order (head first). */
  nodes: ProseMirrorNode[]
  /** Contiguous span start — the head member's position. */
  from: number
  /** Contiguous span end (exclusive) — the last member's end. */
  to: number
}

/**
 * Positions of the block at `headPos` plus every following same-parent
 * sibling in its subtree: the contiguous run of blocks whose depth attr is
 * greater than the head's. The run ends at a header (its depth attr is a
 * heading level, not an indent) or at the first sibling at or shallower
 * than the head's depth.
 *
 * Region blocks (code/table/callout/details and other depth-carrying
 * atomic blocks) are MEMBERS under the same rule as prose (#1061): a
 * region deeper than the head shifts with the run — an indented fence
 * under a task moves with the task's subtree, so its depth band survives
 * indent, moves, and drags intact. A region at or above the head's depth
 * is a boundary exactly like a prose sibling there, and regions never
 * head a run (see `depthRunSpanForNode`).
 *
 * Sibling boundaries come from resolved `nodeAfter` at node-boundary
 * positions, which never crosses into a different parent — so a block
 * nested inside a callout/details collects its run relative to container
 * siblings, exactly like `getSibling`/`indentActiveBlock` do.
 */
export function depthRunSpan(
  doc: ProseMirrorNode,
  headPos: number,
  headDepth: number
): DepthRunSpan {
  const head = doc.nodeAt(headPos)
  const positions = [headPos]
  const nodes = head ? [head] : []
  let to = headPos + (head?.nodeSize ?? 0)
  let pos = headPos
  let node = head
  while (node && pos + node.nodeSize < doc.content.size) {
    const next = doc.resolve(pos + node.nodeSize).nodeAfter
    if (!next) break // parent's last child — never cross the container
    if (next.type.name === 'headerBlock') break
    // Membership is purely depth-based: prose and region siblings alike
    // join when deeper than the head, and either kind ends the scan at or
    // below the head's depth (the header check above keeps heading levels
    // out of the comparison).
    if (((next.attrs.depth as number) || 0) <= headDepth) break
    positions.push(pos + node.nodeSize)
    nodes.push(next)
    to = pos + node.nodeSize + next.nodeSize
    pos += node.nodeSize
    node = next
  }
  return { positions, nodes, from: headPos, to }
}

/**
 * The run a specific node heads when relocated. Region blocks never
 * initiate a run (their depth attr positions them inside a parent's run,
 * but nothing nests under a region as a run head), so a dragged/moved
 * region relocates alone — regions are members, never initiators.
 */
export function depthRunSpanForNode(
  doc: ProseMirrorNode,
  headPos: number,
  headNode: ProseMirrorNode
): DepthRunSpan {
  if (!initiatesDepthRun(headNode.type.name)) {
    return {
      positions: [headPos],
      nodes: [headNode],
      from: headPos,
      to: headPos + headNode.nodeSize
    }
  }
  return depthRunSpan(doc, headPos, (headNode.attrs.depth as number) || 0)
}

/** Relocation plan: swap the run with its adjacent non-member sibling. */
export interface AdjacentRunSwapPlan {
  /** Contiguous span to relocate (original-doc coords). */
  from: number
  to: number
  /**
   * Insert position in ORIGINAL-doc coords — callers map it through their
   * own delete (the mover deletes before inserting, like the drag path).
   */
  insertAt: number
  /** Doc-child index the run head lands at after the swap (focus target). */
  focusIndex: number
  /** Run member start positions (original-doc coords, head first). */
  positions: number[]
}

/**
 * The head of the run that ends just before `pos` — the swap partner for
 * an upward move. Walks backward from the adjacent block, extending while
 * the current head nests under its predecessor (depth(head) > depth(before),
 * the mirror of the forward scan's membership rule) and skipping region
 * members interleaved mid-run. A header never extends under an earlier
 * block (it terminates every forward scan, so it is always its own run's
 * head). A region ADJACENT to `pos` extends the same way: a deeper region
 * belongs to the partner head's run (#1061), and inserting before the
 * region itself would strand it re-parented under the moved run. When the
 * adjacent region does not nest under the preceding block (at or above its
 * depth), it is a boundary and stays its own swap slot.
 */
function precedingRunHead(
  doc: ProseMirrorNode,
  pos: number
): { pos: number; node: ProseMirrorNode } | null {
  const adjacent = doc.resolve(pos).nodeBefore
  if (!adjacent) return null
  const adjacentPos = pos - adjacent.nodeSize
  const isOutlineBlock = (n: ProseMirrorNode) =>
    DEPTH_BLOCK_TYPES.has(n.type.name) || n.type.name === 'headerBlock'
  const depthOf = (n: ProseMirrorNode) => (n.attrs.depth as number) || 0
  // A region may ride along as the walk's starting candidate (member in
  // tow); if the walk never reaches a depth-bearing head, the adjacent
  // region is returned as its own swap slot below.
  let headPos = adjacentPos
  let head = adjacent
  let cursor = adjacentPos
  while (cursor > 0 && head.type.name !== 'headerBlock') {
    const before = doc.resolve(cursor).nodeBefore
    if (!before) break
    const beforePos = cursor - before.nodeSize
    if (isOutlineBlock(before)) {
      if (depthOf(head) > depthOf(before)) {
        head = before
        headPos = beforePos
        cursor = beforePos
        continue
      }
      break
    }
    // Region interleaved mid-run: whether it is a member of the run's
    // head or a boundary, inserting at a head found further back keeps
    // the whole slice intact — keep walking without extending.
    cursor = beforePos
  }
  return { pos: headPos, node: head }
}

/**
 * Plan the Alt+Arrow move: relocate the depth run headed at `headPos` one
 * slot up/down by swapping it with the adjacent sibling block's own whole
 * run — swapping with the neighbor block alone would strand the loser's
 * children under the winner. Returns null at the document edges (nothing
 * to swap with). Top-level only — mirrors the mover's tree-depth guard;
 * nested blocks are not reorderable.
 */
export function planAdjacentRunSwap(
  doc: ProseMirrorNode,
  headPos: number,
  headNode: ProseMirrorNode,
  direction: 1 | -1
): AdjacentRunSwapPlan | null {
  const span = depthRunSpanForNode(doc, headPos, headNode)
  // Walk doc children to map span boundaries to child indices. The span
  // may contain region nodes interleaved between members, so the index
  // math must walk real children rather than divide by member count.
  const startIndexOf: number[] = [] // child start pos → child index
  const endIndexOf: number[] = [] // child end pos → child index
  let pos = 0
  for (let i = 0; i < doc.childCount; i++) {
    const child = doc.child(i)
    startIndexOf[pos] = i
    endIndexOf[pos + child.nodeSize] = i
    pos += child.nodeSize
  }
  const headIndex = startIndexOf[headPos]
  const lastIndex = endIndexOf[span.to]
  if (headIndex == null || lastIndex == null) return null
  if (direction === -1) {
    const target = precedingRunHead(doc, span.from)
    if (!target) return null
    // Inserting before the previous run's head automatically carries that
    // run's members along behind it.
    const focusIndex = startIndexOf[target.pos]
    if (focusIndex == null) return null
    return {
      from: span.from,
      to: span.to,
      insertAt: target.pos,
      focusIndex,
      positions: span.positions
    }
  }
  const target = doc.resolve(span.to).nodeAfter
  if (!target) return null
  // The neighbor's own run may span several children; the head lands right
  // after all of them.
  const targetSpan = depthRunSpanForNode(doc, span.to, target)
  const insertAt = targetSpan.to
  const targetEnd = endIndexOf[insertAt]
  if (targetEnd == null || lastIndex >= doc.childCount - 1) return null
  // Head lands after the whole target run: its slot shifts by the run's
  // child count past the head's old neighbor slot.
  const focusIndex = headIndex + targetEnd - lastIndex
  return {
    from: span.from,
    to: span.to,
    insertAt,
    focusIndex,
    positions: span.positions
  }
}

/**
 * The depth delta a drop applies to a whole run: the resolved column
 * targets the HEAD, and every member shifts by the same delta so relative
 * depths (the subtree shape) survive. The delta is capped so the DEEPEST
 * member never exceeds `maxDepth` — clamping per-member instead would
 * flatten the subtree at the cap, the exact corruption subtree-awareness
 * exists to prevent — and floored so the head never surfaces above 0.
 */
export function clampRunDepthDelta(
  resolvedDepth: number,
  headDepth: number,
  deepestMemberDepth: number,
  maxDepth: number = MAX_DEPTH
): number {
  return Math.max(
    -headDepth,
    Math.min(resolvedDepth - headDepth, maxDepth - deepestMemberDepth)
  )
}

/**
 * Build the replacement content for a relocated span: every block at a
 * run-member position (prose and region members alike, #1061) shifts its
 * depth attr by `delta` (clamped to [0, maxDepth] as defense against
 * already-over-cap legacy docs), so the whole depth band moves together.
 * Callers delete `span.from..span.to` and insert the returned fragment at
 * the mapped position.
 */
export function shiftRunDepths(
  doc: ProseMirrorNode,
  span: DepthRunSpan,
  delta: number,
  maxDepth: number = MAX_DEPTH
): Fragment {
  const memberOffsets = new Set(span.positions.map((p) => p - span.from))
  const children: ProseMirrorNode[] = []
  let pos = span.from
  while (pos < span.to) {
    const node = doc.nodeAt(pos)
    if (!node) break
    if (delta !== 0 && memberOffsets.has(pos - span.from)) {
      const shifted = Math.max(
        0,
        Math.min(((node.attrs.depth as number) || 0) + delta, maxDepth)
      )
      children.push(
        node.type.create(
          { ...node.attrs, depth: shifted },
          node.content,
          node.marks
        )
      )
    } else {
      children.push(node)
    }
    pos += node.nodeSize
  }
  return Fragment.fromArray(children)
}
