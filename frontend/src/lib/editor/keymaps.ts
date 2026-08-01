import { Extension } from '@tiptap/core'
import type { Editor } from '@tiptap/core'
import type { Node as ProseMirrorNode } from '@tiptap/pm/model'
import { Plugin, PluginKey, TextSelection } from '@tiptap/pm/state'
import { keydownHandler } from '@tiptap/pm/keymap'
import { freshId } from './uniqueIdPlugin'
import { resolveShortcut } from '../../settings/hotkeys'
import { settings } from '../../settings/store.svelte'
import {
  parseOrderedBullet,
  renumberFollowingOrdered,
  renumberOrderedRunContaining,
  renumberVacatedOrderedRun,
  renumberAfterOrderedBlockMove,
  applyDepthChangeOnTransaction,
  formatOrderedBullet
} from './orderedList'

const UNORDERED_BULLETS = new Set(['- ', '* ', '+ '])

const siltConfigKeymapsKey = new PluginKey('siltConfigDrivenKeymaps')

// SiltBlockKeymaps — outliner keyboard semantics for the TipTap editor.
//
// Ports the keydown logic from the legacy BlockRenderer.svelte (lines 280-398)
// to TipTap keyboard shortcuts / ProseMirror keymap bindings:
//   - Enter: split at the caret into a new NoteBlock at the same depth
//     (text after the caret moves to the new block; empty when at end).
//   - Backspace at start: merge into a same-type sibling above; else clear
//     bullet, unindent, then delete+focus-prev.
//   - Delete at end: merge a same-type sibling below into this block (or drop
//     this block if it's empty and a same-type sibling follows).
//   - indent_block / unindent_block (config-driven, defaults Tab / Shift-Tab):
//     indent / unindent (bounded by previous sibling's depth + 1).
//   - ArrowUp / ArrowDown at block boundary: move focus to the previous/next block.
//
// Visual indent requires data-depth on the outer NodeView root (see
// outerNodeViewAttrs in nodeViews.ts); CSS targets `.ProseMirror > div[data-depth]`.

/**
 * The Silt block node types the keymaps operate on, in canonical order.
 * NoteBlock is first (default). `codeBlock` is intentionally absent: its
 * `text*` content model differs from the prose blocks' `inline*`, so it is
 * excluded from merging (and from `findActiveBlock` entirely). The
 * `node.type.spec.code` guard inside `mergeSiblingBlock` is defense-in-depth
 * in case a future change admits codeBlock here.
 */
export const BLOCK_TYPES = [
  'noteBlock',
  'taskBlock',
  'headerBlock',
  'calloutBlock'
] as const

/**
 * Block node types that carry a `depth` attr and participate in the outliner's
 * indent/outdent model. Callout blocks (and other new container/atomic types)
 * are intentionally excluded: they have no depth attr, so indenting them would
 * be silently dropped on save. Tab/Shift+Tab fall through for those so TipTap's
 * default (e.g. table cell navigation) applies.
 */
const DEPTH_BLOCK_TYPES = new Set(['noteBlock', 'taskBlock', 'headerBlock'])

/**
 * Walk up from the editor's current selection to the nearest enclosing block
 * node (noteBlock / taskBlock / headerBlock). Returns the block node and its
 * depth, or `null` if the selection is not inside a block.
 *
 * Shared by every consumer that needs "the block I'm currently in" —
 * previously inlined 4× in TipTapEditor and 2× in HeadingLevelMenu with
 * subtly different return shapes.
 */
export function findActiveBlock(
  editor: Editor
): { node: ProseMirrorNode; depth: number } | null {
  const pos = editor.state.selection.$from
  for (let d = pos.depth; d >= 1; d--) {
    const node = pos.node(d)
    if (BLOCK_TYPES.includes(node.type.name as (typeof BLOCK_TYPES)[number])) {
      return { node, depth: d }
    }
  }
  return null
}

function getNextBullet(currentBullet: string): string {
  if (!currentBullet) return ''
  if (['- ', '* ', '+ '].includes(currentBullet)) {
    return currentBullet
  }
  const parsed = parseOrderedBullet(currentBullet)
  if (parsed) {
    return `${parsed.n + 1}${parsed.punc}`
  }
  return currentBullet
}

// Convert the current block to a new type (#169). Provides the correct attrs
// for each type (discarding type-specific attrs that don't apply). Shared by
// the keymap shortcuts and TipTapEditor's slash command handler.
export function convertToBlock(
  editor: Editor,
  type: 'headerBlock' | 'noteBlock' | 'taskBlock',
  headerDepth?: number
): boolean {
  const active = findActiveBlock(editor)
  if (!active) return false
  const node = active.node
  // Guard: converting a calloutBlock to a prose type would silently destroy
  // its content (TipTap's setNode falls through to clearNodes when block+
  // children don't fit inline* content). The user must exit the callout
  // first (Down arrow) then convert the sibling noteBlock below.
  if (node.type.name === 'calloutBlock') return false
  const baseAttrs = {
    id: node.attrs.id,
    depth:
      type === 'headerBlock' ? (headerDepth ?? 1) : (node.attrs.depth ?? 0),
    file_date: node.attrs.file_date || ''
  }
  if (type === 'noteBlock') {
    editor.commands.setNode(type, { ...baseAttrs, bullet: '- ' })
  } else if (type === 'taskBlock') {
    editor.commands.setNode(type, {
      ...baseAttrs,
      status: 'TODO',
      owner: '',
      start_date: '',
      due_date: '',
      priority: 3
    })
  } else {
    editor.commands.setNode(type, baseAttrs)
  }
  return true
}

function currentBlockInfo(editor: Editor) {
  const active = findActiveBlock(editor)
  if (!active) return null
  const { node, depth } = active
  const pos = editor.state.selection.$from
  return {
    node,
    pos: pos.before(depth),
    depth: node.attrs.depth || 0,
    // index is the child index within the block's PARENT at its tree depth —
    // NOT necessarily the top-level doc index. For a block nested inside a
    // callout (tree depth 2), this is the index within the callout. Callers
    // that need the top-level child index (moveActiveBlock / ArrowUp /
    // ArrowDown) re-derive it from `info.pos` against `doc` children.
    // Indent uses same-parent nodeBefore instead (works nested + top-level).
    index: pos.index(depth)
  }
}

/**
 * A block is "empty" when it carries no visible content — no inline children,
 * or only whitespace text with no atomic inlines. Block refs / embeds are
 * atoms with empty textContent but are still content (Enter must not treat
 * `- ((uuid))` as an empty list item and clear the bullet).
 * Whitespace-only blocks behave as empty for BOTH the Backspace
 * unindent/delete path and the Delete drop-empty path so the two boundary
 * keys stay symmetric.
 */
function isBlockEmpty(node: ProseMirrorNode): boolean {
  if (node.content.size === 0) return true
  if (node.textContent.trim() !== '') return false
  let hasAtom = false
  node.forEach((child) => {
    if (child.isAtom) hasAtom = true
  })
  return !hasAtom
}

/**
 * Set block depth and, for ordered noteBlocks, renumber vacated/destination
 * same-depth runs so indent/unindent keep sequential markers (#837).
 */
function setBlockDepth(
  editor: Editor,
  nodePos: number,
  newDepth: number
): void {
  const tr = applyDepthChangeOnTransaction(editor.state.tr, nodePos, newDepth)
  if (tr.docChanged) editor.view.dispatch(tr)
}

/**
 * Indent the active depth-bearing block by one level, capped at previous
 * same-parent sibling depth + 1. Uses nodeBefore (not a top-level doc scan) so
 * blocks nested in callouts/details indent relative to their container siblings.
 * Returns true when the chord was consumed (including no-op at max depth /
 * first child) so Tab does not move browser focus. Returns false outside depth
 * blocks so table cell nav etc. can run.
 *
 * Ordered noteBlocks restart at `1` under the parent and renumber peers (#837).
 */
export function indentActiveBlock(editor: Editor): boolean {
  const info = currentBlockInfo(editor)
  if (!info) return false
  // Only the depth-bearing prose blocks support indent. Letting the chord fall
  // through for callout/code/table/details keeps TipTap's default (table cell
  // nav, etc.) instead of silently no-op'ing.
  if (!DEPTH_BLOCK_TYPES.has(info.node.type.name)) return false

  // Same-parent previous sibling — works at doc root and inside callout/details.
  const prev = editor.state.doc.resolve(info.pos).nodeBefore
  const maxDepth = prev ? (prev.attrs.depth || 0) + 1 : 0
  if (info.depth < maxDepth) {
    setBlockDepth(editor, info.pos, info.depth + 1)
  }
  return true
}

/**
 * Unindent the active depth-bearing block by one level (floor 0).
 * Ordered noteBlocks rejoin the parent-level sequence without gaps (#837).
 */
export function unindentActiveBlock(editor: Editor): boolean {
  const info = currentBlockInfo(editor)
  if (!info) return false
  if (!DEPTH_BLOCK_TYPES.has(info.node.type.name)) return false
  if (info.depth > 0) {
    setBlockDepth(editor, info.pos, info.depth - 1)
  }
  return true
}

function focusBlockAt(editor: Editor, blockIndex: number): void {
  const { doc } = editor.state
  if (blockIndex < 0 || blockIndex >= doc.childCount) return
  let pos = 0
  for (let i = 0; i < blockIndex; i++) {
    pos += doc.child(i).nodeSize
  }
  const child = doc.child(blockIndex)
  const endPos = pos + child.nodeSize - 1
  editor.commands.focus()
  const tr = editor.state.tr.setSelection(
    TextSelection.create(editor.state.doc, endPos, endPos)
  )
  editor.view.dispatch(tr)
}

/**
 * Resolve the same-parent sibling of the active block in the given direction.
 * Uses node-boundary positions rather than `ResolvedPos.index()`, which is
 * off-by-one when the caret sits at the very end of a block's content (it
 * reports the index of the FOLLOWING sibling). Returns the sibling node or
 * `null` when there is none (first/last child, or the position falls outside
 * the current parent — which is what enforces the AC's depth-boundary rule:
 * `nodeBefore`/`nodeAfter` at a node boundary never cross into a different
 * parent).
 */
function getSibling(
  editor: Editor,
  info: { node: ProseMirrorNode; pos: number },
  direction: 'forward' | 'backward'
): ProseMirrorNode | null {
  const { doc } = editor.state
  if (direction === 'forward') {
    // Position immediately after the current block = the next sibling's start
    // (or the parent's close position if this is the last child).
    return doc.resolve(info.pos + info.node.nodeSize).nodeAfter
  }
  return doc.resolve(info.pos).nodeBefore
}

/**
 * Merge the active block with a same-type same-parent sibling, in a single
 * ProseMirror transaction (#364). The survivor keeps its `id` (so the
 * uniqueIdPlugin does NOT remint), the other block's inline content is appended
 * onto the survivor (marks ride along on the text nodes), the emptied block is
 * removed, and the caret lands at the join boundary. `codeBlock` is excluded
 * (its `text*` content model differs); cross-type and cross-parent cases return
 * false so the caller can fall through to the per-type default. Built at the PM
 * level — no docToBlocks round-trip — so a single autosave fires.
 *
 * `direction` is 'forward' for Delete-at-end (merge the next sibling into the
 * current block) and 'backward' for Backspace-at-start (merge the current block
 * into the previous sibling).
 */
function mergeSiblingBlock(
  editor: Editor,
  direction: 'forward' | 'backward'
): boolean {
  const info = currentBlockInfo(editor)
  if (!info) return false
  const { node, pos } = info
  // codeBlock has a different content model (text* vs inline*); leave it alone.
  if (node.type.spec.code) return false

  const sibling = getSibling(editor, { node, pos }, direction)
  if (!sibling) return false
  if (sibling.type.name !== node.type.name) return false

  // create + replaceWith can throw if a future schema change makes the merged
  // content invalid for the block's content model; a keypress must never
  // propagate an uncaught throw out of the keymap, so the whole build+dispatch
  // is guarded — fail closed (no merge) and surface the cause for diagnostics.
  try {
    let merged: ProseMirrorNode
    let from: number
    let to: number
    let caretPos: number
    if (direction === 'forward') {
      // Delete at end of current: append sibling's content into the current
      // block; the current block survives with its own id.
      const mergedContent = node.content.append(sibling.content)
      merged = node.type.create(node.attrs, mergedContent)
      from = pos
      to = pos + node.nodeSize + sibling.nodeSize
      // Caret at the boundary between the two original contents.
      caretPos = pos + 1 + node.content.size
    } else {
      // Backspace at start of current: append the current block's content
      // into the previous sibling; the previous block survives with its id.
      const prevPos = pos - sibling.nodeSize
      const mergedContent = sibling.content.append(node.content)
      merged = sibling.type.create(sibling.attrs, mergedContent)
      from = prevPos
      to = pos + node.nodeSize
      // Caret at the end of the previous block's original content (the join
      // boundary), which is the standard text-editor join position.
      caretPos = prevPos + 1 + sibling.content.size
    }

    const tr = editor.state.tr.replaceWith(from, to, merged)
    tr.setSelection(TextSelection.create(tr.doc, caretPos))
    // No editor.commands.focus() here, deliberately: the editor already holds
    // focus during typing, and an extra focus() dispatch would split the merge
    // into two transactions, breaking atomic undo and the single-autosave
    // contract. Enter/focusBlockAt call focus() because they move focus into a
    // different block; a same-block merge does not.
    editor.view.dispatch(tr)
    return true
  } catch (e) {
    console.error('mergeSiblingBlock: merge dispatch failed', e)
    return false
  }
}

// Move the active top-level block up (-1) or down (+1), swapping it with its
// neighbor (#181 — keyboard complement to the drag handle). No-ops at the
// document edges or when the active block is not top-level (nested blocks are
// not reorderable this way; Tab/Shift-Tab still indent them).
export function moveActiveBlock(editor: Editor, direction: 1 | -1): boolean {
  if (!editor || editor.isDestroyed) return false
  const active = findActiveBlock(editor)
  if (!active) return false
  // Explicit top-level guard: only ProseMirror tree-depth-1 blocks are
  // reorderable (active.depth is the TREE depth from findActiveBlock — NOT
  // node.attrs.depth, which is the indent level, which would wrongly reject
  // legitimately-indented top-level blocks). Reordering a block nested inside a
  // callout/details would corrupt the doc structure.
  if (active.depth !== 1) return false
  const info = currentBlockInfo(editor)
  if (!info) return false
  const { doc, tr } = editor.state
  let idx = -1
  let posIdx = 0
  let acc = 0
  for (let i = 0; i < doc.childCount; i++) {
    if (acc === info.pos) {
      idx = i
      posIdx = acc
      break
    }
    acc += doc.child(i).nodeSize
  }
  if (idx < 0) return false
  const swap = direction === -1 ? idx - 1 : idx + 1
  if (swap < 0 || swap >= doc.childCount) return false
  const node = doc.child(idx)
  const size = node.nodeSize
  const ordered =
    node.type.name === 'noteBlock'
      ? parseOrderedBullet(String(node.attrs.bullet || ''))
      : null
  const nodeDepth = (node.attrs.depth as number) || 0
  let newTr = tr.delete(posIdx, posIdx + size)
  let insertPos: number
  if (direction === -1) {
    // Up: the previous block's start is unaffected by deleting the block after it.
    let posPrev = 0
    for (let i = 0; i < swap; i++) posPrev += doc.child(i).nodeSize
    insertPos = posPrev
    newTr = newTr.insert(posPrev, node)
  } else {
    // Down: after the deletion the next block sits at posIdx; insert after it.
    const nextSize = doc.child(swap).nodeSize
    insertPos = posIdx + nextSize
    newTr = newTr.insert(posIdx + nextSize, node)
  }
  if (ordered) {
    // Same-depth reorder: fix destination sequence + vacated source hole.
    const vacatedNear = newTr.mapping.map(posIdx)
    newTr = renumberAfterOrderedBlockMove(
      newTr,
      insertPos,
      vacatedNear,
      nodeDepth,
      ordered.punc
    )
  }
  editor.view.dispatch(newTr)
  focusBlockAt(editor, swap)
  return true
}

// Set the alignment attr on the current block (#173). No-op for TASK blocks
// (alignment is not supported on tasks — the taskBlock schema has no align attr).
// Shared by the keymap shortcuts and TipTapEditor's slash command handler.
export function setBlockAlign(editor: Editor, align: string): boolean {
  if (!editor || editor.isDestroyed) return false
  const active = findActiveBlock(editor)
  if (!active) return false
  if (active.node.type.name === 'taskBlock') return true // silently skip
  const nodePos = editor.state.selection.$from.before(active.depth)
  const tr = editor.state.tr.setNodeAttribute(nodePos, 'align', align)
  editor.view.dispatch(tr)
  return true
}

// Toggle the blockquote marker on the current noteBlock (#188). Quote and
// bullet are mutually exclusive — the on-disk serializer (docToBlocks) discards
// `bullet` while `quote` is set, so turning quote ON clears the bullet here to
// keep the in-editor state consistent with the save→reload cycle. Toggling
// quote OFF yields a plain note (the bullet was already '' from the quote
// state). No-op on TASK/HEADER blocks (quote is a NOTE marker).
export function toggleBlockQuote(editor: Editor): boolean {
  if (!editor || editor.isDestroyed) return false
  const active = findActiveBlock(editor)
  if (!active) return false
  if (active.node.type.name !== 'noteBlock') return true // silently skip
  const nodePos = editor.state.selection.$from.before(active.depth)
  const isQuote = !!active.node.attrs.quote
  // Quote and bullet are mutually exclusive on disk (docToBlocks discards
  // bullet when quote is set). Clearing bullet on toggle-ON keeps the
  // in-editor state consistent with the save→reload cycle.
  const tr = editor.state.tr.setNodeMarkup(nodePos, undefined, {
    ...active.node.attrs,
    quote: isQuote ? '' : '> ',
    bullet: isQuote ? (active.node.attrs.bullet ?? '') : ''
  })
  editor.view.dispatch(tr)
  return true
}

function isUnorderedBullet(bullet: string): boolean {
  return UNORDERED_BULLETS.has(bullet || '')
}

function isOrderedBullet(bullet: string): boolean {
  return parseOrderedBullet(bullet || '') != null
}

/**
 * Collect top-level positions of every noteBlock intersecting the selection
 * (or the active noteBlock when the selection is empty/caret).
 */
function selectedNoteBlockPositions(editor: Editor): number[] {
  const { from, to, empty } = editor.state.selection
  const positions: number[] = []
  if (empty) {
    const active = findActiveBlock(editor)
    if (!active || active.node.type.name !== 'noteBlock') return []
    positions.push(editor.state.selection.$from.before(active.depth))
    return positions
  }
  editor.state.doc.nodesBetween(from, to, (node, pos) => {
    if (node.type.name === 'noteBlock') {
      positions.push(pos)
      return false // do not descend into noteBlock inline content
    }
    return true
  })
  return positions
}

export type ListKind = 'unordered' | 'ordered'

/** True when every selected noteBlock is already the given list kind. */
export function selectionIsListKind(editor: Editor, kind: ListKind): boolean {
  if (!editor || editor.isDestroyed) return false
  const positions = selectedNoteBlockPositions(editor)
  if (positions.length === 0) return false
  return positions.every((pos) => {
    const node = editor.state.doc.nodeAt(pos)
    if (!node) return false
    const bullet = String(node.attrs.bullet || '')
    return kind === 'unordered'
      ? isUnorderedBullet(bullet)
      : isOrderedBullet(bullet)
  })
}

/**
 * Toggle unordered (`- `) or ordered (`1. `) list markers on selected
 * noteBlocks (#840). Multi-block selections convert each note line.
 *
 * Semantics:
 * - All selected notes already that kind → clear bullets (toggle off).
 * - Otherwise → set target kind (ordered numbered sequentially per depth run).
 * - Quote is cleared when applying a list (mutual exclusion with bullet).
 */
export function toggleList(editor: Editor, kind: ListKind): boolean {
  if (!editor || editor.isDestroyed) return false
  const positions = selectedNoteBlockPositions(editor)
  if (positions.length === 0) return false

  const turnOff = positions.every((pos) => {
    const node = editor.state.doc.nodeAt(pos)
    if (!node) return false
    const bullet = String(node.attrs.bullet || '')
    return kind === 'unordered'
      ? isUnorderedBullet(bullet)
      : isOrderedBullet(bullet)
  })

  let tr = editor.state.tr
  // Apply in reverse document order so positions stay valid.
  const orderedPos = [...positions].sort((a, b) => b - a)
  // Ordered toggle-off: capture vacated (depth, punc) before clearing so
  // remaining peers can renumber (e.g. 1. 2. 3. → off mid → 1. · 1.).
  const vacatedOrdered: { pos: number; depth: number; punc: string }[] = []
  if (kind === 'ordered' && turnOff) {
    for (const pos of orderedPos) {
      const node = tr.doc.nodeAt(pos)
      if (!node || node.type.name !== 'noteBlock') continue
      const parsed = parseOrderedBullet(String(node.attrs.bullet || ''))
      if (!parsed) continue
      vacatedOrdered.push({
        pos,
        depth: (node.attrs.depth as number) || 0,
        punc: parsed.punc
      })
    }
  }
  for (const pos of orderedPos) {
    const node = tr.doc.nodeAt(pos)
    if (!node || node.type.name !== 'noteBlock') continue
    if (turnOff) {
      tr = tr.setNodeMarkup(pos, undefined, {
        ...node.attrs,
        bullet: '',
        quote: node.attrs.quote || ''
      })
    } else if (kind === 'unordered') {
      tr = tr.setNodeMarkup(pos, undefined, {
        ...node.attrs,
        bullet: '- ',
        quote: ''
      })
    } else {
      // Ordered: seed 1. then renumber runs after all seeds applied.
      tr = tr.setNodeMarkup(pos, undefined, {
        ...node.attrs,
        bullet: formatOrderedBullet(1, '. '),
        quote: ''
      })
    }
  }

  if (kind === 'ordered' && turnOff) {
    // Attr-only clears keep positions stable; renumber is idempotent across
    // multi-block offs that touch the same run.
    for (const { pos, depth, punc } of vacatedOrdered) {
      tr = renumberVacatedOrderedRun(tr, pos, depth, punc)
    }
  } else if (kind === 'ordered' && !turnOff) {
    // Renumber each affected depth run. Calling containing-renumber per
    // selected pos is cheap and correct across disconnected runs.
    const forward = [...positions].sort((a, b) => a - b)
    for (const pos of forward) {
      const node = tr.doc.nodeAt(pos)
      if (!node) continue
      const depth = (node.attrs.depth as number) || 0
      const parsed = parseOrderedBullet(String(node.attrs.bullet || ''))
      if (!parsed) continue
      tr = renumberOrderedRunContaining(tr, pos, depth, parsed.punc)
    }
  }

  if (tr.docChanged) {
    editor.view.dispatch(tr)
  }
  return true
}

export function toggleUnorderedList(editor: Editor): boolean {
  return toggleList(editor, 'unordered')
}

export function toggleOrderedList(editor: Editor): boolean {
  return toggleList(editor, 'ordered')
}

// Insert a callout block at the current selection (#180/#308). The callout
// replaces the current block when it is an empty note, otherwise inserts a new
// callout below. The variant drives the icon + accent (CALLOUT_VARIANTS in
// schema.ts). Under `content: 'block+'` the callout MUST seed a placeholder
// paragraph (block+ requires ≥1 child).
export function insertCallout(editor: Editor, variant: string): boolean {
  if (!editor || editor.isDestroyed) return false
  const today = new Date().toISOString().slice(0, 10)
  const paragraph = editor.state.schema.nodes.paragraph
  const calloutNode = editor.state.schema.nodes.calloutBlock?.create(
    { id: null, variant, file_date: today },
    paragraph ? [paragraph.create()] : []
  )
  if (!calloutNode) return false
  // If the current block is an empty note/header, replace it in place.
  const active = findActiveBlock(editor)
  const isEmptyNote =
    active &&
    (active.node.type.name === 'noteBlock' ||
      active.node.type.name === 'headerBlock') &&
    (active.node.content.size === 0 || active.node.textContent.trim() === '')
  if (active && isEmptyNote) {
    const pos = editor.state.selection.$from.before(active.depth)
    editor.view.dispatch(
      editor.state.tr.replaceWith(pos, pos + active.node.nodeSize, calloutNode)
    )
    editor.commands.focus()
    return true
  }
  editor.commands.insertContent(calloutNode)
  editor.commands.focus()
  return true
}

// Insert a fenced code block at the current selection (#189). Replaces the
// current block when it is an empty note/header, otherwise inserts below.
export function insertCodeBlock(editor: Editor, language = ''): boolean {
  if (!editor || editor.isDestroyed) return false
  const today = new Date().toISOString().slice(0, 10)
  // An empty code block has NO text children — codeBlock's content is 'text*'
  // (zero or more), which a content-less create satisfies. ProseMirror rejects
  // empty *text nodes* (schema.text('') throws), so we must not synthesize one;
  // the user's typing adds real text nodes as they go.
  const codeNode = editor.state.schema.nodes.codeBlock?.create({
    id: null,
    language,
    file_date: today
  })
  if (!codeNode) return false
  const active = findActiveBlock(editor)
  const isEmptyNote =
    active &&
    (active.node.type.name === 'noteBlock' ||
      active.node.type.name === 'headerBlock') &&
    (active.node.content.size === 0 || active.node.textContent.trim() === '')
  if (active && isEmptyNote) {
    const pos = editor.state.selection.$from.before(active.depth)
    editor.view.dispatch(
      editor.state.tr.replaceWith(pos, pos + active.node.nodeSize, codeNode)
    )
    editor.commands.focus()
    return true
  }
  editor.commands.insertContent(codeNode)
  editor.commands.focus()
  return true
}

// Insert a centered block equation ($$...$$) at the current selection (#191).
// Replaces the current block when it is an empty note/header, otherwise inserts
// below. The latex is set on insert; the NodeView offers click-to-edit.
export function insertBlockMath(editor: Editor, latex = ''): boolean {
  if (!editor || editor.isDestroyed) return false
  const mathNode = editor.state.schema.nodes.blockMathNode?.create({
    id: freshId(),
    latex
  })
  if (!mathNode) return false
  const active = findActiveBlock(editor)
  const isEmptyNote =
    active &&
    (active.node.type.name === 'noteBlock' ||
      active.node.type.name === 'headerBlock') &&
    (active.node.content.size === 0 || active.node.textContent.trim() === '')
  if (active && isEmptyNote) {
    const pos = editor.state.selection.$from.before(active.depth)
    editor.view.dispatch(
      editor.state.tr.replaceWith(pos, pos + active.node.nodeSize, mathNode)
    )
    editor.commands.focus()
    return true
  }
  editor.commands.insertContent(mathNode)
  editor.commands.focus()
  return true
}

// Insert a foldable `<details>` section (#183). Builds the Details >
// DetailsSummary + DetailsContent(placeholder note) tree the TipTap extension
// expects. Replaces an empty note/header in place, otherwise inserts below.
export function insertDetails(editor: Editor): boolean {
  if (!editor || editor.isDestroyed) return false
  const schema = editor.state.schema
  if (!schema.nodes.details) return false
  const today = new Date().toISOString().slice(0, 10)
  // Mint an id for the placeholder up front: it is nested inside
  // detailsContent, so the UniqueBlockIds appendTransaction (which walks only
  // top-level blocks) never reaches it. Without a stable id the inner note
  // would bypass the outliner's identity-keyed ops until the next save.
  const placeholder = schema.nodes.noteBlock?.create(
    { id: freshId(), depth: 0, bullet: '', file_date: today },
    []
  )
  const detailsNode = schema.nodes.details.create(
    { id: null, open: true, file_date: today },
    [
      schema.nodes.detailsSummary.create(
        { id: null },
        schema.text('Section title')
      ),
      schema.nodes.detailsContent.create(
        { id: null },
        placeholder ? [placeholder] : []
      )
    ]
  )
  const active = findActiveBlock(editor)
  const isEmptyNote =
    active &&
    (active.node.type.name === 'noteBlock' ||
      active.node.type.name === 'headerBlock') &&
    (active.node.content.size === 0 || active.node.textContent.trim() === '')
  if (active && isEmptyNote) {
    const pos = editor.state.selection.$from.before(active.depth)
    editor.view.dispatch(
      editor.state.tr.replaceWith(pos, pos + active.node.nodeSize, detailsNode)
    )
    editor.commands.focus()
    return true
  }
  editor.commands.insertContent(detailsNode)
  editor.commands.focus()
  return true
}

// Toggle the `open` attr on the `<details>` enclosing the cursor (#183).
// Walks up from the selection to the nearest details node and flips open.
export function toggleDetails(editor: Editor): boolean {
  if (!editor || editor.isDestroyed) return false
  const $pos = editor.state.selection.$from
  for (let d = $pos.depth; d >= 1; d--) {
    const node = $pos.node(d)
    if (node.type.name === 'details') {
      const pos = $pos.before(d)
      editor.view.dispatch(
        editor.state.tr.setNodeAttribute(pos, 'open', !node.attrs.open)
      )
      return true
    }
  }
  return false
}

// Insert a GFM table (#172). rows/cols include the header row. Builds the
// table > tableRow(tableHeader×cols) + (rows-1)×tableRow(tableCell×cols)
// tree the TipTap Table extension expects, each cell seeded with an empty
// paragraph. Replaces an empty note/header in place, else inserts below.
export function insertTable(editor: Editor, rows = 3, cols = 3): boolean {
  if (!editor || editor.isDestroyed) return false
  const schema = editor.state.schema
  if (!schema.nodes.table) return false
  // TipTap's tableCell has content 'block+' and its row/column commands fill
  // new cells with paragraph nodes. Without a paragraph node in the schema
  // the cells would be empty/invalid and the table would silently fail to
  // insert — fail loudly instead of producing a broken table.
  const paragraph = schema.nodes.paragraph
  if (!paragraph) return false
  const today = new Date().toISOString().slice(0, 10)
  const emptyCell = (type: 'tableHeader' | 'tableCell') =>
    schema.nodes[type].create({}, paragraph.create())
  const headerRow = schema.nodes.tableRow.create(
    {},
    Array.from({ length: cols }, () => emptyCell('tableHeader'))
  )
  const dataRows = Array.from({ length: Math.max(rows - 1, 0) }, () =>
    schema.nodes.tableRow.create(
      {},
      Array.from({ length: cols }, () => emptyCell('tableCell'))
    )
  )
  const table = schema.nodes.table.create({ id: null, file_date: today }, [
    headerRow,
    ...dataRows
  ])
  const active = findActiveBlock(editor)
  const isEmptyNote =
    active &&
    (active.node.type.name === 'noteBlock' ||
      active.node.type.name === 'headerBlock') &&
    (active.node.content.size === 0 || active.node.textContent.trim() === '')
  if (active && isEmptyNote) {
    const pos = editor.state.selection.$from.before(active.depth)
    editor.view.dispatch(
      editor.state.tr.replaceWith(pos, pos + active.node.nodeSize, table)
    )
    editor.commands.focus()
    return true
  }
  editor.commands.insertContent(table)
  editor.commands.focus()
  return true
}

// Drop keymap entries whose key is '' — resolveShortcut returns '' when the
// user explicitly cleared a binding ("Leave empty to disable" in HotkeysTab),
// so the entry must be omitted rather than left at (or restored to) its
// default. An empty string is never a valid ProseMirror key, so removing it is
// safe. (Format marks coexist with TipTap StarterKit defaults; clearing such a
// binding here removes Silt's override but not the StarterKit's own default —
// a deeper change reserved for a follow-up.)
function omitDisabled(
  map: Record<string, () => boolean>
): Record<string, () => boolean> {
  delete map['']
  return map
}

// Build the config-driven editor shortcut map (#311). Reads config.hotkeys
// live from the settings store (called on every keydown via the config keymap
// plugin) so Settings remaps apply without remounting the editor. Absent
// entries fall back to hardcoded defaults; an explicitly empty entry disables
// the shortcut. Covers indent/unindent, heading levels, alignment,
// quote/details toggles, table row/col inserts, and inline format marks.
function buildConfigDrivenShortcuts(
  editor: Editor
): Record<string, () => boolean> {
  const hk = settings.config?.hotkeys ?? {}
  const pm = (configKey: string, def: string) =>
    resolveShortcut(configKey, def, hk)
  const map: Record<string, () => boolean> = {}

  // Outliner indent / unindent (defaults Tab / Shift-Tab).
  map[pm('indent_block', 'Tab')] = () => indentActiveBlock(editor)
  map[pm('unindent_block', 'Shift-Tab')] = () => unindentActiveBlock(editor)

  // Strikethrough — config-driven via format_strike (#311). TipTap's Strike
  // extension registers its own Mod-Shift-s default; this binding overrides
  // it with the user's config choice (or the standard Mod-Shift-x fallback).
  map[pm('format_strike', 'Mod-Shift-x')] = () => {
    editor.chain().focus().toggleStrike().run()
    return true
  }

  // Link — dispatches a custom event so TipTapEditor can show its inline
  // URL input (#168). If already linked, removes.
  map[pm('format_link', 'Mod-k')] = () => {
    const { selection } = editor.state
    if (selection.empty) return false
    if (editor.isActive('link')) {
      editor.chain().focus().unsetLink().run()
    } else {
      window.dispatchEvent(new CustomEvent('silt:open-link-input'))
    }
    return true
  }

  // Heading level shortcuts (#169 / #645).
  map[pm('set_h1', 'Mod-Alt-1')] = () =>
    convertToBlock(editor, 'headerBlock', 1)
  map[pm('set_h2', 'Mod-Alt-2')] = () =>
    convertToBlock(editor, 'headerBlock', 2)
  map[pm('set_h3', 'Mod-Alt-3')] = () =>
    convertToBlock(editor, 'headerBlock', 3)
  map[pm('set_h4', 'Mod-Alt-5')] = () =>
    convertToBlock(editor, 'headerBlock', 4)
  map[pm('set_h5', 'Mod-Alt-6')] = () =>
    convertToBlock(editor, 'headerBlock', 5)
  map[pm('set_h6', 'Mod-Alt-7')] = () =>
    convertToBlock(editor, 'headerBlock', 6)
  map[pm('set_note', 'Mod-Alt-0')] = () => convertToBlock(editor, 'noteBlock')
  map[pm('set_task', 'Mod-Alt-4')] = () => convertToBlock(editor, 'taskBlock')

  // Text alignment shortcuts (#173).
  map[pm('align_left', 'Mod-Shift-l')] = () => setBlockAlign(editor, 'left')
  map[pm('align_center', 'Mod-Shift-e')] = () => setBlockAlign(editor, 'center')
  map[pm('align_right', 'Mod-Shift-r')] = () => setBlockAlign(editor, 'right')
  map[pm('align_justify', 'Mod-Shift-j')] = () =>
    setBlockAlign(editor, 'justify')

  // Blockquote toggle (#188).
  map[pm('toggle_quote', 'Mod-Shift-9')] = () => toggleBlockQuote(editor)

  // List toggles (#840).
  map[pm('toggle_bullet_list', 'Mod-Shift-8')] = () =>
    toggleUnorderedList(editor)
  map[pm('toggle_ordered_list', 'Mod-Shift-7')] = () =>
    toggleOrderedList(editor)

  // Foldable details toggle (#183).
  map[pm('toggle_details', 'Mod-Shift-.')] = () => toggleDetails(editor)

  // Table row/column insert shortcuts (#172).
  map[pm('table_insert_row_above', 'Mod-Shift-ArrowUp')] = () =>
    editor.can().addRowBefore?.()
      ? (editor.chain().focus().addRowBefore().run(), true)
      : false
  map[pm('table_insert_row_below', 'Mod-Shift-ArrowDown')] = () =>
    editor.can().addRowAfter?.()
      ? (editor.chain().focus().addRowAfter().run(), true)
      : false
  map[pm('table_insert_col_left', 'Mod-Shift-ArrowLeft')] = () =>
    editor.can().addColumnBefore?.()
      ? (editor.chain().focus().addColumnBefore().run(), true)
      : false
  map[pm('table_insert_col_right', 'Mod-Shift-ArrowRight')] = () =>
    editor.can().addColumnAfter?.()
      ? (editor.chain().focus().addColumnAfter().run(), true)
      : false

  return omitDisabled(map)
}

// Register config-driven bindings for inline format marks (bold, italic, etc.)
// that are also handled by TipTap StarterKit extensions. Reads config live
// (same plugin path as buildConfigDrivenShortcuts) and coexists with
// StarterKit's hardcoded defaults when Silt's binding is disabled/absent.
function buildFormatMarkShortcuts(
  editor: Editor
): Record<string, () => boolean> {
  const hk = settings.config?.hotkeys ?? {}
  const pm = (configKey: string, def: string) =>
    resolveShortcut(configKey, def, hk)
  const map: Record<string, () => boolean> = {}

  map[pm('format_bold', 'Mod-b')] = () => {
    editor.chain().focus().toggleBold().run()
    return true
  }
  map[pm('format_italic', 'Mod-i')] = () => {
    editor.chain().focus().toggleItalic().run()
    return true
  }
  map[pm('format_underline', 'Mod-u')] = () => {
    editor.chain().focus().toggleUnderline().run()
    return true
  }
  map[pm('format_code', 'Mod-e')] = () => {
    editor.chain().focus().toggleCode().run()
    return true
  }
  map[pm('format_highlight', 'Mod-Shift-h')] = () => {
    editor.chain().focus().toggleHighlight().run()
    return true
  }
  map[pm('format_subscript', 'Mod-Shift-,')] = () => {
    editor.chain().focus().toggleSubscript().run()
    return true
  }
  map[pm('format_superscript', 'Mod-.')] = () => {
    editor.chain().focus().toggleSuperscript().run()
    return true
  }

  return omitDisabled(map)
}

/** All config-driven editor chords, rebuilt from the live settings store. */
function buildLiveConfigBindings(
  editor: Editor
): Record<string, () => boolean> {
  return {
    ...buildConfigDrivenShortcuts(editor),
    ...buildFormatMarkShortcuts(editor)
  }
}

export const SiltBlockKeymaps = Extension.create({
  name: 'siltBlockKeymaps',

  // Config-driven chords (indent, format marks, headings, …) live in a
  // dedicated plugin that rebuilds the binding map on every keydown from
  // settings.config.hotkeys. That way HotkeysTab saves apply immediately
  // without destroying/recreating the editor (addKeyboardShortcuts is only
  // evaluated once at extension init).
  addProseMirrorPlugins() {
    const editor = this.editor
    return [
      new Plugin({
        key: siltConfigKeymapsKey,
        props: {
          handleKeyDown(view, event) {
            const bindings = buildLiveConfigBindings(editor)
            return keydownHandler(bindings)(view, event)
          }
        }
      })
    ]
  },

  addKeyboardShortcuts() {
    return {
      Enter: () => {
        const info = currentBlockInfo(this.editor)
        if (!info) return false
        // codeBlock content model is text*; leave default handling alone.
        if (info.node.type.spec.code) return false

        // Default to a plain (no-bullet) line. Only noteBlocks inherit /
        // resequence the bullet marker — pressing Enter after a task or
        // header should start a fresh plain line, not a bulleted one (#258).
        // Mid-text split (move trailing content onto the new block) is also
        // noteBlock-only: task/header/callout keep their full body and only
        // append an empty note below (avoids demoting trailing task text).
        const isNote = info.node.type.name === 'noteBlock'

        // Empty list/quote item + Enter exits the container (the common list-exit
        // convention; mirrors Backspace bullet/quote clear). Nested depth is
        // left alone — unindent is Tab/Backspace, not Enter.
        // Only when the selection is collapsed: a cross-block selection must
        // delete the range first (standard Enter-with-selection), not clear
        // the marker while leaving selected content intact.
        if (
          isNote &&
          this.editor.state.selection.empty &&
          isBlockEmpty(info.node)
        ) {
          const bullet = (info.node.attrs.bullet as string) || ''
          const quote = (info.node.attrs.quote as string) || ''
          if (bullet) {
            const parsed = parseOrderedBullet(bullet)
            let tr = this.editor.state.tr.setNodeAttribute(
              info.pos,
              'bullet',
              ''
            )
            if (parsed) {
              tr = renumberVacatedOrderedRun(
                tr,
                info.pos,
                info.depth,
                parsed.punc
              )
            }
            this.editor.view.dispatch(tr)
            return true
          }
          if (quote) {
            this.editor.view.dispatch(
              this.editor.state.tr.setNodeAttribute(info.pos, 'quote', '')
            )
            return true
          }
        }

        let nextBullet = ''
        let nextQuote = ''
        if (isNote) {
          nextBullet = getNextBullet(info.node.attrs.bullet || '')
          // Quote continues across mid-text / end-of-line Enter (markdown `>`
          // lines); empty quote already exited above.
          nextQuote = (info.node.attrs.quote as string) || ''
          // Quote and bullet are mutually exclusive on noteBlock.
          if (nextQuote) nextBullet = ''
        }

        const { state } = this.editor
        const { selection, schema } = state
        const noteType = schema.nodes.noteBlock
        if (!noteType) return false

        const blockPos = info.pos
        // Content lives inside the block node (between open/close tokens).
        const contentEnd = blockPos + info.node.nodeSize - 1

        try {
          let tr = state.tr
          // Non-empty selection: drop it first so split is at the caret
          // (standard editor Enter-with-selection semantics).
          if (!selection.empty) {
            tr = tr.delete(selection.from, selection.to)
          }
          const cutFrom = tr.selection.from
          const mappedContentEnd = tr.mapping.map(contentEnd)

          // noteBlock only: text after the caret moves to the new block.
          // Other block types leave the source intact and insert empty below.
          const afterContent =
            isNote && cutFrom < mappedContentEnd
              ? tr.doc.slice(cutFrom, mappedContentEnd).content
              : null
          if (isNote && cutFrom < mappedContentEnd) {
            tr = tr.delete(cutFrom, mappedContentEnd)
          }

          const attrs = {
            id: null as string | null,
            depth: info.depth,
            bullet: nextBullet,
            quote: nextQuote,
            file_date: new Date().toISOString().slice(0, 10)
          }
          const newNode =
            afterContent && afterContent.size > 0
              ? noteType.create(attrs, afterContent)
              : noteType.create(attrs)

          // After in-block delete, block still starts at blockPos.
          const blockNode = tr.doc.nodeAt(blockPos)
          if (!blockNode) return false
          const insertPos = blockPos + blockNode.nodeSize
          tr = tr.insert(insertPos, newNode)
          // Ordered lists: bump every following same-depth ordered marker so
          // mid-list Enter does not leave duplicate numbers.
          const ordered = parseOrderedBullet(nextBullet)
          if (ordered) {
            tr = renumberFollowingOrdered(
              tr,
              insertPos,
              ordered.n,
              ordered.punc,
              info.depth
            )
          }
          // Caret at start of the new block (same-tx; no focus() split —
          // see mergeSiblingBlock).
          tr = tr.setSelection(TextSelection.create(tr.doc, insertPos + 1))
          this.editor.view.dispatch(tr)
          return true
        } catch (e) {
          console.error('Enter split: dispatch failed', e)
          return false
        }
      },

      Backspace: () => {
        const info = currentBlockInfo(this.editor)
        if (!info) return false

        const { selection } = this.editor.state
        const isAtStart =
          selection.from === selection.to && selection.$from.parentOffset === 0
        if (!isAtStart) return false

        // If the block is a note block and has a bullet, clear the bullet first.
        if (
          info.node.type.name === 'noteBlock' &&
          info.node.attrs.bullet &&
          info.node.attrs.bullet !== ''
        ) {
          const bullet = String(info.node.attrs.bullet || '')
          const parsed = parseOrderedBullet(bullet)
          let tr = this.editor.state.tr.setNodeAttribute(info.pos, 'bullet', '')
          if (parsed) {
            tr = renumberVacatedOrderedRun(
              tr,
              info.pos,
              info.depth,
              parsed.punc
            )
          }
          this.editor.view.dispatch(tr)
          return true
        }

        // Clear the quote marker next (parallel to bullet clearing — the two
        // are mutually exclusive on noteBlock, but quote needs its own step
        // or Backspace would consume the keypress on a sole empty quoted
        // block without removing the "> " marker).
        if (
          info.node.type.name === 'noteBlock' &&
          info.node.attrs.quote &&
          info.node.attrs.quote !== ''
        ) {
          const tr = this.editor.state.tr.setNodeAttribute(
            info.pos,
            'quote',
            ''
          )
          this.editor.view.dispatch(tr)
          return true
        }

        // Non-empty block at start: try to merge its content into the same-type
        // sibling above. Same-type-sibling merge takes precedence over the
        // empty-block unindent/delete path below (#364). If no same-type sibling
        // exists (cross-type, cross-parent, or first child), fall through.
        if (!isBlockEmpty(info.node)) {
          return mergeSiblingBlock(this.editor, 'backward')
        }

        // Sole empty taskBlock or headerBlock: convert to noteBlock so the
        // user has a one-press escape from an unwanted block type (#568).
        // This must run BEFORE the unindent check because headerBlock's
        // `depth` attr is the header level (1-6), not indentation — a sole
        // empty headerBlock would be caught by info.depth > 0 and unindented
        // instead of converted. Only converts when the block is the sole
        // top-level child (tree depth 1); a block nested inside a container
        // like a callout (tree depth > 1) falls through to the #569 guard
        // which returns false for nested blocks.
        const { doc } = this.editor.state
        const blockType = info.node.type.name
        if (
          doc.childCount <= 1 &&
          (blockType === 'taskBlock' || blockType === 'headerBlock')
        ) {
          const active = findActiveBlock(this.editor)
          if (active && active.depth === 1) {
            const baseAttrs = {
              id: info.node.attrs.id,
              depth: 0,
              file_date: info.node.attrs.file_date || '',
              bullet: ''
            }
            return this.editor.commands.setNode('noteBlock', baseAttrs)
          }
        }

        if (info.depth > 0) {
          // Unindent first.
          setBlockDepth(this.editor, info.pos, info.depth - 1)
          return true
        }

        // Consume the keypress as a no-op: this is the sole, empty block at
        // depth 0 — nothing to delete, merge, or unindent. Returning false
        // would fall through to StarterKit/ProseMirror's default chain, which
        // synthesizes a new node on an empty sole block (#552).
        if (doc.childCount <= 1) {
          // Guard: only consume as no-op if the selection is in a top-level
          // child. A block nested inside a sole callout (tree depth 2, attr
          // depth 0) has findActiveBlock returning at a depth > 1, meaning
          // the caret is inside a container, not directly in a doc child (#569).
          const active = findActiveBlock(this.editor)
          if (active && active.depth > 1) return false
          return true
        }

        // Find the current block's top-level index.
        let blockIndex = -1
        let acc = 0
        for (let i = 0; i < doc.childCount; i++) {
          if (acc === info.pos) {
            blockIndex = i
            break
          }
          acc += doc.child(i).nodeSize
        }
        if (blockIndex <= 0) return false

        // Delete and focus previous.
        const from = info.pos
        const to = info.pos + info.node.nodeSize
        this.editor.view.dispatch(this.editor.state.tr.delete(from, to))
        focusBlockAt(this.editor, blockIndex - 1)
        return true
      },

      Delete: () => {
        const info = currentBlockInfo(this.editor)
        if (!info) return false

        const { selection } = this.editor.state
        // Only act when the caret is collapsed at the end of the block's
        // inline content. ProseMirror's default Delete is a no-op at the end of
        // an isolating block, so this is the only path that joins the block
        // below (#364).
        const isAtEnd =
          selection.from === selection.to &&
          selection.$from.parentOffset === info.node.content.size
        if (!isAtEnd) return false

        // Empty current block + same-type sibling below: drop the empty block
        // so the sibling takes its place. The sibling keeps its own id (no
        // merge — the empty block contributes no content). Caret lands at the
        // start of the promoted sibling's content. Whitespace-only counts as
        // empty here (isBlockEmpty) so Delete and Backspace treat blank lines
        // symmetrically.
        if (isBlockEmpty(info.node)) {
          const sibling = getSibling(
            this.editor,
            { node: info.node, pos: info.pos },
            'forward'
          )
          if (!sibling || sibling.type.name !== info.node.type.name) {
            return false
          }
          const from = info.pos
          const to = info.pos + info.node.nodeSize
          const caret = info.pos + 1
          this.editor.commands.focus()
          const tr = this.editor.state.tr.delete(from, to)
          tr.setSelection(TextSelection.create(tr.doc, caret, caret))
          this.editor.view.dispatch(tr)
          return true
        }

        return mergeSiblingBlock(this.editor, 'forward')
      },

      // indent_block / unindent_block are registered via
      // buildConfigDrivenShortcuts (defaults Tab / Shift-Tab).

      ArrowUp: () => {
        const info = currentBlockInfo(this.editor)
        if (!info) return false
        const { selection } = this.editor.state
        // Only navigate when at the start of a block.
        if (selection.$from.parentOffset > 0) return false

        const { doc } = this.editor.state
        let blockIndex = -1
        let acc = 0
        for (let i = 0; i < doc.childCount; i++) {
          if (acc === info.pos) {
            blockIndex = i
            break
          }
          acc += doc.child(i).nodeSize
        }
        if (blockIndex > 0) {
          focusBlockAt(this.editor, blockIndex - 1)
          return true
        }
        return false
      },

      ArrowDown: () => {
        const info = currentBlockInfo(this.editor)
        if (!info) return false
        const { selection } = this.editor.state
        // Only navigate when at the end of a block.
        if (selection.$from.parentOffset < info.node.content.size) return false

        const { doc } = this.editor.state
        let blockIndex = -1
        let acc = 0
        for (let i = 0; i < doc.childCount; i++) {
          if (acc === info.pos) {
            blockIndex = i
            break
          }
          acc += doc.child(i).nodeSize
        }
        if (blockIndex >= 0 && blockIndex < doc.childCount - 1) {
          focusBlockAt(this.editor, blockIndex + 1)
          return true
        }
        return false
      },

      // Alt+ArrowUp/Down reorders the active block (#181) — the keyboard
      // complement to the drag handle. No Mod prefix, to avoid colliding with
      // the Mod-Shift-Arrow table row/column bindings.
      // Config-driven shortcuts (indent, format marks, headings, alignment,
      // tables, …) are handled by addProseMirrorPlugins so they read
      // settings.config.hotkeys on every keydown.
      'Alt-ArrowUp': () => moveActiveBlock(this.editor, -1),
      'Alt-ArrowDown': () => moveActiveBlock(this.editor, 1),

      // Shift-Enter: soft line break in the current prose block (#828), except
      // on a main-outline taskBlock where it opens TaskSubEditorModal (#781).
      // Inside the sub-editor (siltSubEditorHost.active) always soft-break so
      // nested task rows don't tear the modal down mid-autosave.
      'Shift-Enter': () => {
        const info = currentBlockInfo(this.editor)
        if (info?.node.type.name === 'taskBlock') {
          const subEditorStorage = this.editor.storage as unknown as Record<
            string,
            { active?: boolean }
          >
          if (!subEditorStorage.siltSubEditorHost?.active) {
            const blockId = info.node.attrs.id
            if (blockId) {
              window.dispatchEvent(
                new CustomEvent('silt:open-task-editor', {
                  detail: { blockId }
                })
              )
              return true
            }
          }
        }
        // SiltHardBreak.setHardBreak works inside isolating note/task/header.
        return this.editor.commands.setHardBreak()
      }
    }
  }
})
