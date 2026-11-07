import { describe, it, expect } from 'vitest'
import { Editor } from '@tiptap/core'
import StarterKit from '@tiptap/starter-kit'
import { SiltBlockExtensions } from './index'
import { CodeBlock } from './schema'
import {
  DEPTH_BLOCK_TYPES,
  depthRunSpan,
  depthRunSpanForNode,
  planAdjacentRunSwap,
  clampRunDepthDelta,
  shiftRunDepths
} from './depthRun'
import { MAX_DEPTH } from './constants'

// The HTML5 drag/drop pipeline cannot be driven from jsdom (per AGENTS.md),
// so the subtree-aware relocation is extracted as pure helpers over the doc
// and tested here: both consumers (Alt+Arrow move in keymaps.ts, indent-on-
// drop in dragIndentDrop.ts) build their transactions from these plans.

const note = (id: string, text: string, depth: number) => ({
  type: 'noteBlock' as const,
  attrs: { id, depth, bullet: '- ' },
  content: [{ type: 'text' as const, text }]
})

const code = (id: string, text: string, depth: number) => ({
  type: 'codeBlock' as const,
  attrs: { id, depth, language: 'ts' },
  content: [{ type: 'text' as const, text }]
})

function makeDoc(content: unknown[]): Editor {
  const editor = new Editor({
    extensions: [
      StarterKit.configure({
        paragraph: false,
        heading: false,
        bulletList: false,
        orderedList: false,
        listItem: false,
        blockquote: false,
        codeBlock: false,
        horizontalRule: false,
        trailingNode: false
      }),
      ...SiltBlockExtensions,
      CodeBlock
    ],
    content: { type: 'doc', content: content as never }
  })
  return editor
}

function startOf(editor: Editor, id: string): number {
  let pos = 0
  for (let i = 0; i < editor.state.doc.childCount; i++) {
    const child = editor.state.doc.child(i)
    if (child.attrs.id === id) return pos
    pos += child.nodeSize
  }
  throw new Error(`no block ${id}`)
}

describe('depthRunSpan — contiguous run collection', () => {
  it('collects the head plus every deeper following sibling, stops at a shallower one', () => {
    const editor = makeDoc([
      note('a', 'l0', 0),
      note('p', 'parent', 0),
      note('c1', 'child', 1),
      note('g1', 'grandchild', 2),
      note('tail', 'l0', 0)
    ])
    const span = depthRunSpan(editor.state.doc, startOf(editor, 'p'), 0)
    expect(span.positions).toHaveLength(3)
    expect(span.nodes.map((n) => n.attrs.id)).toEqual(['p', 'c1', 'g1'])
    // Span math: from the head's start to the last member's END.
    expect(span.from).toBe(startOf(editor, 'p'))
    const g1 = editor.state.doc.nodeAt(startOf(editor, 'g1'))
    expect(span.to).toBe(startOf(editor, 'g1') + (g1?.nodeSize ?? 0))
    editor.destroy()
  })

  it('collects deeper region siblings as members, keeping the span contiguous', () => {
    const editor = makeDoc([
      note('p', 'parent', 0),
      code('r', 'const x = 1', 1),
      note('c', 'child', 2),
      note('tail', 'l0', 0)
    ])
    const span = depthRunSpan(editor.state.doc, startOf(editor, 'p'), 0)
    // A deeper region is a run MEMBER (#1061): it shifts with the subtree
    // like prose, and the deeper note after it still belongs too.
    expect(span.nodes.map((n) => n.attrs.id)).toEqual(['p', 'r', 'c'])
    expect(span.from).toBe(0)
    const c = editor.state.doc.nodeAt(startOf(editor, 'c'))
    expect(span.to).toBe(startOf(editor, 'c') + (c?.nodeSize ?? 0))
    editor.destroy()
  })

  it('terminates at a region at or above the head depth — boundary, not member', () => {
    const editor = makeDoc([
      note('p', 'parent', 0),
      code('r', 'const x = 1', 0),
      note('c', 'child', 1),
      note('tail', 'l0', 0)
    ])
    const span = depthRunSpan(editor.state.doc, startOf(editor, 'p'), 0)
    // Same rule as prose at ≤ depth: a region at the head's own depth is a
    // sibling, not a child — the run (and any indent behind it) ends there.
    expect(span.nodes.map((n) => n.attrs.id)).toEqual(['p'])
    const p = editor.state.doc.nodeAt(startOf(editor, 'p'))
    expect(span.to).toBe(startOf(editor, 'p') + (p?.nodeSize ?? 0))
    editor.destroy()
  })

  it('terminates at a header — its level attr is never a member', () => {
    const editor = makeDoc([
      note('p', 'parent', 0),
      note('c', 'child', 1),
      {
        type: 'headerBlock',
        attrs: { id: 'h', depth: 1 },
        content: [{ type: 'text', text: 'Section' }]
      },
      note('t', 'l0', 0)
    ])
    const span = depthRunSpan(editor.state.doc, startOf(editor, 'p'), 0)
    expect(span.nodes.map((n) => n.attrs.id)).toEqual(['p', 'c'])
    expect(span.to).toBe(startOf(editor, 'h'))
    editor.destroy()
  })
})

describe('depthRunSpanForNode — run initiation', () => {
  it('exposes the depth-bearing block types (header excluded as member, admitted as head)', () => {
    expect(DEPTH_BLOCK_TYPES.has('noteBlock')).toBe(true)
    expect(DEPTH_BLOCK_TYPES.has('taskBlock')).toBe(true)
    expect(DEPTH_BLOCK_TYPES.has('headerBlock')).toBe(false)
  })

  it('a region head never initiates a run — regions are members only', () => {
    const editor = makeDoc([code('r', 'const x = 1', 1), note('c', 'child', 2)])
    const node = editor.state.doc.nodeAt(0)
    const span = depthRunSpanForNode(editor.state.doc, 0, node!)
    // Dragging/moving a region relocates the region alone; a following
    // deeper note is NOT swept along (regions never head a run — only
    // noteBlock/taskBlock/headerBlock can initiate one).
    expect(span.positions).toEqual([0])
    expect(span.nodes.map((n) => n.attrs.id)).toEqual(['r'])
    expect(span.to).toBe(node!.nodeSize)
    editor.destroy()
  })

  it('a header head sweeps its body notes (level N ≡ flat depth N)', () => {
    const editor = makeDoc([
      {
        type: 'headerBlock',
        attrs: { id: 'h', depth: 2 },
        content: [{ type: 'text', text: 'Section' }]
      },
      note('b', 'body', 3)
    ])
    const node = editor.state.doc.nodeAt(0)
    const span = depthRunSpanForNode(editor.state.doc, 0, node!)
    expect(span.nodes.map((n) => n.attrs.id)).toEqual(['h', 'b'])
    editor.destroy()
  })
})

describe('planAdjacentRunSwap — Alt+Arrow relocation plan', () => {
  it('plans a down-swap: insert after the adjacent sibling — including its children', () => {
    const editor = makeDoc([
      note('x', 'prev', 0),
      note('a', 'parent', 0),
      note('b', 'child', 1),
      note('c', 'next parent', 0),
      note('cc', 'next child', 1)
    ])
    const doc = editor.state.doc
    const headPos = startOf(editor, 'a')
    const head = doc.nodeAt(headPos)!
    const plan = planAdjacentRunSwap(doc, headPos, head, 1)!
    expect(plan).not.toBeNull()
    const spanTo =
      startOf(editor, 'b') + doc.nodeAt(startOf(editor, 'b'))!.nodeSize
    expect(plan.from).toBe(headPos)
    expect(plan.to).toBe(spanTo)
    // Insert after the neighbor's whole run — landing between the neighbor
    // and its children would re-parent them.
    const cc = doc.nodeAt(startOf(editor, 'cc'))!
    expect(plan.insertAt).toBe(startOf(editor, 'cc') + cc.nodeSize)
    // Post-swap the head lands right after the neighbor's whole run.
    expect(plan.focusIndex).toBe(3)
    editor.destroy()
  })

  it('plans an up-swap: insert before the adjacent non-member sibling', () => {
    const editor = makeDoc([
      note('x', 'prev', 0),
      note('a', 'parent', 0),
      note('b', 'child', 1)
    ])
    const doc = editor.state.doc
    const headPos = startOf(editor, 'a')
    const head = doc.nodeAt(headPos)!
    const plan = planAdjacentRunSwap(doc, headPos, head, -1)!
    expect(plan).not.toBeNull()
    expect(plan.insertAt).toBe(startOf(editor, 'x'))
    expect(plan.focusIndex).toBe(0)
    editor.destroy()
  })

  it('plans an up-swap past a subtree ending in a deeper region member', () => {
    // #1061: the fence (depth 1) is x's run member, so the swap must land
    // before X. Inserting before the region itself (the old "region is its
    // own swap slot" rule) would strand it re-parented under the moved run.
    const editor = makeDoc([
      note('x', 'prev head', 0),
      code('r', 'const x = 1', 1),
      note('a', 'parent', 0),
      note('b', 'child', 1)
    ])
    const doc = editor.state.doc
    const headPos = startOf(editor, 'a')
    const head = doc.nodeAt(headPos)!
    const plan = planAdjacentRunSwap(doc, headPos, head, -1)!
    expect(plan).not.toBeNull()
    expect(plan.insertAt).toBe(startOf(editor, 'x'))
    expect(plan.focusIndex).toBe(0)
    editor.destroy()
  })

  it('returns null at both document edges', () => {
    // Up edge: the run starts at the first child.
    const first = makeDoc([
      note('a', 'parent', 0),
      note('b', 'child', 1),
      note('c', 'tail', 0)
    ])
    expect(
      planAdjacentRunSwap(first.state.doc, 0, first.state.doc.child(0), -1)
    ).toBeNull()
    first.destroy()
    // Down edge: the run's last member is the doc's last child.
    const last = makeDoc([
      note('c', 'head', 0),
      note('a', 'parent', 0),
      note('b', 'child', 1)
    ])
    const aPos = startOf(last, 'a')
    expect(
      planAdjacentRunSwap(last.state.doc, aPos, last.state.doc.nodeAt(aPos)!, 1)
    ).toBeNull()
    last.destroy()
  })
})

describe('clampRunDepthDelta — whole-run depth delta for drops', () => {
  it('applies the resolved delta when the whole run fits under the cap', () => {
    // Head 0, deepest member 2, dropped at column 2 → everyone shifts +2.
    expect(clampRunDepthDelta(2, 0, 2, MAX_DEPTH)).toBe(2)
  })

  it('clamps at MAX_DEPTH on the deepest member, not per-member', () => {
    // Head 5 with a depth-6 child dropped at column 6: shifting +1 would
    // write depth 7, so the whole run holds at delta 0 (relative depths
    // preserved — a per-member clamp would flatten the child onto 6).
    expect(clampRunDepthDelta(MAX_DEPTH, 5, 6, MAX_DEPTH)).toBe(0)
    // Head 4 with a depth-5 child dropped at column 6: delta caps at +1.
    expect(clampRunDepthDelta(MAX_DEPTH, 4, 5, MAX_DEPTH)).toBe(1)
  })

  it('never surfaces the head above depth 0', () => {
    expect(clampRunDepthDelta(0, 3, 4, MAX_DEPTH)).toBe(-3)
  })
})

describe('shiftRunDepths — relocation steps for a drag mid-subtree', () => {
  it('applies the drop delta to every run member, preserving relative depths', () => {
    const editor = makeDoc([
      note('x', 'l0', 0),
      note('p', 'parent', 0),
      note('c', 'child', 1),
      note('g', 'grandchild', 2),
      note('tail', 'l0', 0)
    ])
    const doc = editor.state.doc
    const headPos = startOf(editor, 'p')
    const span = depthRunSpanForNode(doc, headPos, doc.nodeAt(headPos)!)
    const resolved = 2
    const delta = clampRunDepthDelta(resolved, 0, 2, MAX_DEPTH)
    expect(delta).toBe(2)
    // Mirror the drag transaction: delete the span, insert the shifted
    // fragment at the mapped drop position (here: the doc top).
    let tr = editor.state.tr.delete(span.from, span.to)
    const insertAt = tr.mapping.map(0)
    tr = tr.insert(insertAt, shiftRunDepths(doc, span, delta))
    editor.view.dispatch(tr)
    const ids = Array.from(
      { length: editor.state.doc.childCount },
      (_, i) => editor.state.doc.child(i).attrs.id
    )
    const depths = Array.from({ length: editor.state.doc.childCount }, (_, i) =>
      Number(editor.state.doc.child(i).attrs.depth ?? 0)
    )
    // Whole run relocated, relative depths [0,1,2] → [2,3,4].
    expect(ids).toEqual(['p', 'c', 'g', 'x', 'tail'])
    expect(depths).toEqual([2, 3, 4, 0, 0])
    editor.destroy()
  })

  it('shifts interleaved region members with the run delta', () => {
    const editor = makeDoc([
      note('p', 'parent', 0),
      code('r', 'const x = 1', 1),
      note('c', 'child', 2),
      note('tail', 'l0', 0)
    ])
    const doc = editor.state.doc
    const headPos = startOf(editor, 'p')
    const span = depthRunSpanForNode(doc, headPos, doc.nodeAt(headPos)!)
    const delta = clampRunDepthDelta(1, 0, 2, MAX_DEPTH)
    expect(delta).toBe(1)
    let tr = editor.state.tr.delete(span.from, span.to)
    tr = tr.insert(0, shiftRunDepths(doc, span, delta))
    editor.view.dispatch(tr)
    // The region rides along AND shifts +1 — a member exactly like prose
    // (#1061), so the serializer keeps the whole depth band together.
    const ids = Array.from(
      { length: editor.state.doc.childCount },
      (_, i) => editor.state.doc.child(i).attrs.id
    )
    const depths = Array.from({ length: editor.state.doc.childCount }, (_, i) =>
      Number(editor.state.doc.child(i).attrs.depth ?? 0)
    )
    expect(ids).toEqual(['p', 'r', 'c', 'tail'])
    expect(depths).toEqual([1, 2, 3, 0])
    editor.destroy()
  })

  it('mirrors the drag step-7: a dragged parent sweeps its deeper region sibling', () => {
    // jsdom cannot drive handleDrop (per AGENTS.md), so the drag path's
    // run construction is pinned here at the helper level: the dragged
    // parent's span must include the deeper fence, and the whole-run
    // delta/clamp must see the region's depth.
    const editor = makeDoc([
      note('x', 'l0', 0),
      note('p', 'parent', 0),
      code('r', 'const x = 1', 1),
      note('c', 'child', 1),
      note('tail', 'l0', 0)
    ])
    const doc = editor.state.doc
    const headPos = startOf(editor, 'p')
    const run = depthRunSpanForNode(doc, headPos, doc.nodeAt(headPos)!)
    expect(run.nodes.map((n) => n.attrs.id)).toEqual(['p', 'r', 'c'])
    const deepest = run.nodes.reduce(
      (max, n) => Math.max(max, (n.attrs.depth as number) || 0),
      0
    )
    const delta = clampRunDepthDelta(1, 0, deepest, MAX_DEPTH)
    expect(delta).toBe(1)
    let tr = editor.state.tr.delete(run.from, run.to)
    tr = tr.insert(0, shiftRunDepths(doc, run, delta))
    editor.view.dispatch(tr)
    const ids = Array.from(
      { length: editor.state.doc.childCount },
      (_, i) => editor.state.doc.child(i).attrs.id
    )
    const depths = Array.from({ length: editor.state.doc.childCount }, (_, i) =>
      Number(editor.state.doc.child(i).attrs.depth ?? 0)
    )
    expect(ids).toEqual(['p', 'r', 'c', 'x', 'tail'])
    expect(depths).toEqual([1, 2, 2, 0, 0])
    editor.destroy()
  })

  it('the whole-run clamp sees region member depths (region at MAX_DEPTH)', () => {
    const editor = makeDoc([
      note('p', 'parent', 0),
      code('r', 'const x = 1', 6),
      note('x', 'l0', 0)
    ])
    const doc = editor.state.doc
    const headPos = startOf(editor, 'p')
    const run = depthRunSpanForNode(doc, headPos, doc.nodeAt(headPos)!)
    expect(run.nodes.map((n) => n.attrs.id)).toEqual(['p', 'r'])
    const deepest = run.nodes.reduce(
      (max, n) => Math.max(max, (n.attrs.depth as number) || 0),
      0
    )
    // The region member sits at MAX_DEPTH, so a +1 indent is capped to a
    // no-op — indenting the parent alone would strand the fence's band.
    expect(clampRunDepthDelta(1, 0, deepest, MAX_DEPTH)).toBe(0)
    editor.destroy()
  })

  it('holds the run in place when the deepest member sits at MAX_DEPTH', () => {
    const editor = makeDoc([
      note('p', 'parent', 5),
      note('c', 'child', 6),
      note('x', 'l0', 0)
    ])
    const doc = editor.state.doc
    const headPos = startOf(editor, 'p')
    const span = depthRunSpanForNode(doc, headPos, doc.nodeAt(headPos)!)
    const delta = clampRunDepthDelta(MAX_DEPTH, 5, 6, MAX_DEPTH)
    expect(delta).toBe(0)
    const frag = shiftRunDepths(doc, span, delta)
    expect(frag.child(0).attrs.depth).toBe(5)
    expect(frag.child(1).attrs.depth).toBe(6)
    editor.destroy()
  })
})
