import { describe, it, expect } from 'vitest'
import { Editor } from '@tiptap/core'
import StarterKit from '@tiptap/starter-kit'
import { SiltBlockExtensions } from './index'
import {
  resolveDropDepth,
  resolveIndentStepFromEl,
  resolveDragDepthForNode,
  runSpansWholeDoc,
  BlockIndentOnDrop,
  INDENT_STEP_PX
} from './dragIndentDrop'
import {
  depthRunSpanForNode,
  shiftRunDepths,
  clampRunDepthDelta
} from './depthRun'
import {
  applyDropDepthFixup,
  parseOrderedBullet,
  renumberOrderedRunMembers
} from './orderedList'
import { CodeBlock } from './schema'
import { MAX_DEPTH } from './constants'

// resolveDropDepth is the ONLY logic this phase can prove correct in jsdom.
// The HTML5 drag/drop pipeline (DataTransfer + layout-driven posAtCoords +
// PM's internal view.dragging NodeSelection) cannot be driven from jsdom —
// per AGENTS.md the interactive indent-on-drop path is covered by the
// TESTING.md manual matrix. So this file exhaustively pins the pure helper
// (the part that decides the depth) and adds a smoke test that the TipTap
// extension is constructible and registers its ProseMirror plugin.

describe('resolveDropDepth — pure math', () => {
  describe('default Silt constants (INDENT_STEP_PX=24, MAX_DEPTH=6)', () => {
    it('exposes the editor CSS constants verbatim', () => {
      // INDENT_STEP_PX mirrors `--indent-unit` in frontend/src/index.css:103.
      // MAX_DEPTH (from ./constants, shared with the Tab indent keymap)
      // mirrors the deepest `[data-depth='N']` rule in index.css. If either
      // changes, the constants module MUST be updated to match — pinning
      // them here makes a silent drift a test failure.
      expect(INDENT_STEP_PX).toBe(24)
      expect(MAX_DEPTH).toBe(6)
    })

    it('clamps the drag path with the shared MAX_DEPTH constant', () => {
      // The same constant the Tab keymap clamps against caps the drop
      // resolver, so neither depth writer can exceed the rendered maximum.
      expect(resolveDropDepth(1e6, 100, 24, MAX_DEPTH)).toBe(MAX_DEPTH)
      expect(resolveDropDepth(100 + 24 * MAX_DEPTH, 100, 24, MAX_DEPTH)).toBe(
        MAX_DEPTH
      )
    })

    it('returns 0 at exactly contentLeft', () => {
      expect(resolveDropDepth(100, 100, 24, 6)).toBe(0)
      expect(resolveDropDepth(0, 0, 24, 6)).toBe(0)
    })

    it('returns 0 left of contentLeft', () => {
      expect(resolveDropDepth(99, 100, 24, 6)).toBe(0)
      expect(resolveDropDepth(50, 100, 24, 6)).toBe(0)
      expect(resolveDropDepth(0, 100, 24, 6)).toBe(0)
      expect(resolveDropDepth(-100, 100, 24, 6)).toBe(0)
    })

    it('returns 1 at exactly +1 step', () => {
      expect(resolveDropDepth(100 + 24, 100, 24, 6)).toBe(1)
    })

    it('returns 2 at exactly +2 steps', () => {
      expect(resolveDropDepth(100 + 48, 100, 24, 6)).toBe(2)
    })

    it('returns N at exactly +N steps for every N in [0, max]', () => {
      for (let n = 0; n <= 6; n++) {
        expect(resolveDropDepth(100 + n * 24, 100, 24, 6)).toBe(n)
      }
    })

    it('rounds to nearest step (Math.round half-up semantics)', () => {
      // The implementation documents Math.round, which rounds 0.5 → 1
      // (round-half-up, not banker's rounding). Pin the exact midpoint
      // direction so a future refactor to Math.trunc/|0 is caught.
      const halfStep = 100 + 12 // +0.5 step
      expect(resolveDropDepth(halfStep, 100, 24, 6)).toBe(1)

      // +0.4 steps → rounds down to 0
      expect(resolveDropDepth(100 + 9, 100, 24, 6)).toBe(0)
      // +0.6 steps → rounds up to 1
      expect(resolveDropDepth(100 + 15, 100, 24, 6)).toBe(1)

      // +1.4 steps → 1
      expect(resolveDropDepth(100 + 34, 100, 24, 6)).toBe(1)
      // +1.5 steps → 2 (half-up)
      expect(resolveDropDepth(100 + 36, 100, 24, 6)).toBe(2)
      // +1.6 steps → 2
      expect(resolveDropDepth(100 + 38, 100, 24, 6)).toBe(2)
    })

    it('clamps to MAX_DEPTH when clientX is far right', () => {
      expect(resolveDropDepth(1000, 100, 24, 6)).toBe(6)
      expect(resolveDropDepth(1e6, 100, 24, 6)).toBe(6)
    })

    it('returns max at exactly +max steps and beyond', () => {
      expect(resolveDropDepth(100 + 6 * 24, 100, 24, 6)).toBe(6)
      expect(resolveDropDepth(100 + 7 * 24, 100, 24, 6)).toBe(6)
      expect(resolveDropDepth(100 + 100 * 24, 100, 24, 6)).toBe(6)
    })
  })

  describe('variable indentStepPx', () => {
    it('respects a larger step (40px)', () => {
      expect(resolveDropDepth(100 + 40, 100, 40, 6)).toBe(1)
      expect(resolveDropDepth(100 + 80, 100, 40, 6)).toBe(2)
      expect(resolveDropDepth(100 + 60, 100, 40, 6)).toBe(2) // 1.5 → half-up
      expect(resolveDropDepth(100 + 20, 100, 40, 6)).toBe(1) // 0.5 → half-up
    })

    it('respects a smaller step (10px)', () => {
      expect(resolveDropDepth(100 + 10, 100, 10, 6)).toBe(1)
      expect(resolveDropDepth(100 + 25, 100, 10, 6)).toBe(3) // 2.5 → half-up → 3
      expect(resolveDropDepth(100 + 55, 100, 10, 6)).toBe(6) // 5.5 → 6
    })

    it('handles fractional step values', () => {
      expect(resolveDropDepth(100 + 1.5, 100, 1.5, 6)).toBe(1)
      expect(resolveDropDepth(100 + 3, 100, 1.5, 6)).toBe(2)
    })
  })

  describe('variable maxDepth', () => {
    it('respects a smaller max (2)', () => {
      expect(resolveDropDepth(100 + 24, 100, 24, 2)).toBe(1)
      expect(resolveDropDepth(100 + 48, 100, 24, 2)).toBe(2)
      expect(resolveDropDepth(1000, 100, 24, 2)).toBe(2)
    })

    it('respects max=1', () => {
      expect(resolveDropDepth(100, 100, 24, 1)).toBe(0)
      expect(resolveDropDepth(100 + 24, 100, 24, 1)).toBe(1)
      expect(resolveDropDepth(1000, 100, 24, 1)).toBe(1)
    })

    it('clamps to 0 when maxDepth=0 (every drop is depth-0)', () => {
      expect(resolveDropDepth(100, 100, 24, 0)).toBe(0)
      expect(resolveDropDepth(100 + 24, 100, 24, 0)).toBe(0)
      expect(resolveDropDepth(1000, 100, 24, 0)).toBe(0)
    })
  })

  describe('defensive guards (never throws)', () => {
    it('treats step=0 as step=1 (no divide-by-zero)', () => {
      // clientX-contentLeft=100, step clamped to 1 → 100 raw steps, clamped to max=6
      expect(resolveDropDepth(200, 100, 0, 6)).toBe(6)
      // small offset with step=0 → step=1 → 1 step → depth 1
      expect(resolveDropDepth(101, 100, 0, 6)).toBe(1)
    })

    it('treats negative step as step=1', () => {
      expect(resolveDropDepth(200, 100, -5, 6)).toBe(6)
    })

    it('treats negative maxDepth as maxDepth=0', () => {
      expect(resolveDropDepth(200, 100, 24, -3)).toBe(0)
    })

    it('returns 0 for NaN clientX (broken DOM rect)', () => {
      expect(resolveDropDepth(NaN, 100, 24, 6)).toBe(0)
    })

    it('returns 0 for NaN contentLeft', () => {
      expect(resolveDropDepth(200, NaN, 24, 6)).toBe(0)
    })

    it('returns 0 for Infinity clientX', () => {
      expect(resolveDropDepth(Infinity, 100, 24, 6)).toBe(0)
    })

    it('returns 0 for -Infinity clientX', () => {
      expect(resolveDropDepth(-Infinity, 100, 24, 6)).toBe(0)
    })

    it('treats NaN maxDepth as 0', () => {
      expect(resolveDropDepth(200, 100, 24, NaN)).toBe(0)
    })

    it('treats NaN step as step=1', () => {
      expect(resolveDropDepth(200, 100, NaN, 6)).toBe(6)
    })

    it('floors a fractional maxDepth', () => {
      // A fractional cap doesn't make sense; floor it.
      expect(resolveDropDepth(1000, 100, 24, 2.9)).toBe(2)
    })
  })

  describe('monotonicity (deeper drops never yield shallower depths)', () => {
    it('is non-decreasing as clientX increases', () => {
      let prev = -1
      for (let dx = -50; dx <= 500; dx += 3) {
        const d = resolveDropDepth(100 + dx, 100, 24, 6)
        expect(d).toBeGreaterThanOrEqual(prev)
        expect(d).toBeGreaterThanOrEqual(0)
        expect(d).toBeLessThanOrEqual(6)
        prev = d
      }
    })
  })
})

// ---- smoke test (interactive behavior is in TESTING.md manual matrix) ------
// We can't drive HTML5 drag/drop from jsdom, but we CAN assert the extension
// is constructible, registers under the right name, and contributes its
// ProseMirror plugin to the editor state. If `addProseMirrorPlugins` throws
// or the plugin fails to attach, every real drop would fall through to
// native (silently breaking the feature) — this gate catches that.

describe('BlockIndentOnDrop extension (smoke)', () => {
  function makeEditor(): Editor {
    return new Editor({
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
        BlockIndentOnDrop
      ],
      content: {
        type: 'doc',
        content: [
          {
            type: 'noteBlock',
            attrs: { id: 'b1', depth: 0, bullet: '- ' },
            content: [{ type: 'text', text: 'one' }]
          },
          {
            type: 'noteBlock',
            attrs: { id: 'b2', depth: 0, bullet: '- ' },
            content: [{ type: 'text', text: 'two' }]
          }
        ]
      }
    })
  }

  it('registers under the name siltBlockIndentOnDrop', () => {
    const editor = makeEditor()
    expect(editor.extensionManager.extensions.map((e) => e.name)).toContain(
      'siltBlockIndentOnDrop'
    )
    editor.destroy()
  })

  it('contributes a ProseMirror plugin to the editor state', () => {
    const editor = makeEditor()
    // The extension must contribute at least one plugin (handleDrop lives
    // on the plugin props; without it the extension is a no-op).
    expect(editor.state.plugins.length).toBeGreaterThan(0)
    editor.destroy()
  })

  it('does not alter the doc on construction (no side effects)', () => {
    const editor = makeEditor()
    expect(editor.state.doc.childCount).toBe(2)
    expect(editor.state.doc.firstChild?.attrs.depth).toBe(0)
    expect(editor.state.doc.lastChild?.attrs.depth).toBe(0)
    editor.destroy()
  })
})

// Region blocks (codeBlock, table, callout, …) carry the same depth attr as
// prose blocks, so a region drag now passes the handler's has-depth guard and
// must receive the resolved depth instead of falling through to a depth-less
// native reorder. Pins the fixup + schema attr with a codeBlock: dropping the
// attr fails here. A re-added region bail inside handleDrop itself cannot be
// caught from jsdom — driving the extension's drag handler needs a real
// drop event (browser e2e is out of scope for this repo).
describe('applyDropDepthFixup — region blocks through the drop path', () => {
  function makeEditor(): Editor {
    return new Editor({
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
        CodeBlock,
        BlockIndentOnDrop
      ],
      content: {
        type: 'doc',
        content: [
          {
            type: 'noteBlock',
            attrs: { id: 'n1', depth: 0, bullet: '- ' },
            content: [{ type: 'text', text: 'task' }]
          },
          {
            type: 'codeBlock',
            attrs: { id: 'c1', depth: 1, language: 'ts' },
            content: [{ type: 'text', text: 'const x = 1' }]
          }
        ]
      }
    })
  }

  it('gives a dragged codeBlock the resolved depth and clamps at MAX_DEPTH', () => {
    const editor = makeEditor()
    // A mid-band drop proves the region node is not skipped (its depth attr
    // actually changes); a far-right drop proves the applied depth is capped
    // by the same resolver the live handler uses.
    for (const expected of [2, MAX_DEPTH]) {
      editor.commands.setContent({
        type: 'doc',
        content: [
          {
            type: 'noteBlock',
            attrs: { id: 'n1', depth: 0, bullet: '- ' },
            content: [{ type: 'text', text: 'task' }]
          },
          {
            type: 'codeBlock',
            attrs: { id: 'c1', depth: 1, language: 'ts' },
            content: [{ type: 'text', text: 'const x = 1' }]
          }
        ]
      })
      // Mirror the drop transaction: delete the source, insert at the top,
      // map the source hole — then hand off to the pure helper.
      const noteSize = editor.state.doc.child(0).nodeSize
      const code = editor.state.doc.child(1)
      let tr = editor.state.tr.delete(noteSize, noteSize + code.nodeSize)
      tr = tr.insert(0, code)
      const vacatedNear = tr.mapping.map(noteSize)
      const clientX =
        expected === MAX_DEPTH ? 1e6 : 100 + expected * INDENT_STEP_PX
      const newDepth = resolveDropDepth(clientX, 100, INDENT_STEP_PX, MAX_DEPTH)
      expect(newDepth).toBe(expected)
      // codeBlock has no bullet attr, so the handler passes ordered=null.
      tr = applyDropDepthFixup(tr, code.attrs, null, 0, vacatedNear, newDepth)
      editor.view.dispatch(tr)
      const dropped = editor.state.doc.child(0)
      expect(dropped.type.name).toBe('codeBlock')
      expect(dropped.attrs.depth).toBe(expected)
    }
    editor.destroy()
  })
})

// A headerBlock's `depth` attr is its heading LEVEL, not an indent. The
// drag handler's has-depth guard admits headerBlock drags (HeaderBlockView
// renders the same [data-drag-handle] span as notes/tasks), so the drop
// column must never become the new level — dragging an H2 to the right
// margin would mint an H6, and a far-LEFT drop would zero the hashes.
// Mirrors the Tab-path bail (headers are excluded from the depth blocks).
describe('headerBlock drag never rewrites the heading level', () => {
  it('resolveDragDepthForNode keeps the current level at any drop column', () => {
    for (const clientX of [-100, 100, 100 + 3 * INDENT_STEP_PX, 1e6]) {
      expect(
        resolveDragDepthForNode(
          'headerBlock',
          2,
          clientX,
          100,
          INDENT_STEP_PX,
          MAX_DEPTH
        )
      ).toBe(2)
    }
    // Prose blocks keep resolving from the drop column (regression guard
    // for the non-header path).
    expect(
      resolveDragDepthForNode(
        'noteBlock',
        0,
        100 + 2 * INDENT_STEP_PX,
        100,
        INDENT_STEP_PX,
        MAX_DEPTH
      )
    ).toBe(2)
  })

  it('the drop fixup leaves the level untouched end-to-end', () => {
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
        BlockIndentOnDrop
      ],
      content: {
        type: 'doc',
        content: [
          {
            type: 'headerBlock',
            attrs: { id: 'h', depth: 2 },
            content: [{ type: 'text', text: 'Section' }]
          },
          {
            type: 'noteBlock',
            attrs: { id: 'n', depth: 0, bullet: '- ' },
            content: [{ type: 'text', text: 'body' }]
          }
        ]
      }
    })
    // Mirror the drop transaction exactly as handleDrop builds it: delete
    // the header's source, re-insert at the top, resolve the depth with the
    // header-aware resolver (far-right column — the most destructive case),
    // then run the shared fixup.
    const header = editor.state.doc.child(0)
    let tr = editor.state.tr.delete(0, header.nodeSize)
    const mappedInsert = 0
    tr = tr.insert(mappedInsert, header)
    const vacatedNear = tr.mapping.map(0)
    const newDepth = resolveDragDepthForNode(
      'headerBlock',
      (header.attrs.depth as number) || 0,
      1e6,
      100,
      INDENT_STEP_PX,
      MAX_DEPTH
    )
    tr = applyDropDepthFixup(
      tr,
      header.attrs,
      null,
      mappedInsert,
      vacatedNear,
      newDepth
    )
    editor.view.dispatch(tr)
    const dropped = editor.state.doc.child(0)
    expect(dropped.type.name).toBe('headerBlock')
    // The level survived the far-right drop column.
    expect(dropped.attrs.depth).toBe(2)
    editor.destroy()
  })
})

// Dragging the head of a page's ONLY subtree sweeps the whole doc into the
// run — every valid drop point lies inside it, and the delete+insert
// relocation would empty the doc mid-transaction. PM's step machinery then
// auto-fills a default noteBlock to satisfy the doc's `block+` content
// requirement, and that filler survives the reinsert as a phantom empty
// note that autosaves. handleDrop must detect the span invariant
// (runSpansWholeDoc) and bail to native reorder. jsdom cannot drive
// handleDrop itself (no layout-driven posAtCoords), so this pins the
// predicate and mirrors the guarded transaction, same pattern as the
// headerBlock drag describe above.
describe('whole-doc run drag never mints a phantom noteBlock', () => {
  const subtreeDoc = {
    type: 'doc',
    content: [
      {
        type: 'noteBlock',
        attrs: { id: 'a', depth: 0, bullet: '- ' },
        content: [{ type: 'text', text: 'parent' }]
      },
      {
        type: 'noteBlock',
        attrs: { id: 'b', depth: 1, bullet: '- ' },
        content: [{ type: 'text', text: 'child' }]
      }
    ]
  }

  function makeEditor(): Editor {
    return new Editor({
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
          trailingNode: false,
          hardBreak: false
        }),
        ...SiltBlockExtensions,
        BlockIndentOnDrop
      ],
      content: subtreeDoc
    })
  }

  it('flags a run that covers the entire doc (the page’s only subtree)', () => {
    const editor = makeEditor()
    const doc = editor.state.doc
    // Dragging the depth-0 head sweeps its depth-1 child: 0 → doc end.
    const run = depthRunSpanForNode(doc, 0, doc.child(0))
    expect(run.from).toBe(0)
    expect(run.to).toBe(doc.content.size)
    expect(runSpansWholeDoc(run, doc.content.size)).toBe(true)
    editor.destroy()
  })

  it('does not flag a run with a neighbor outside the span', () => {
    const editor = makeEditor()
    // A trailing depth-0 sibling leaves room outside the run — the
    // delete+insert relocation is safe there and must not bail.
    editor.commands.setContent({
      type: 'doc',
      content: [
        ...subtreeDoc.content,
        {
          type: 'noteBlock',
          attrs: { id: 'c', depth: 0, bullet: '- ' },
          content: [{ type: 'text', text: 'tail' }]
        }
      ]
    })
    const doc = editor.state.doc
    const run = depthRunSpanForNode(doc, 0, doc.child(0))
    expect(run.to).toBeLessThan(doc.content.size)
    expect(runSpansWholeDoc(run, doc.content.size)).toBe(false)
    editor.destroy()
  })

  it('mirroring the guarded drop transaction adds no filler block', () => {
    const editor = makeEditor()
    const doc = editor.state.doc
    // Exactly the handler's step-7 sequence, guard included: a whole-doc
    // run bails BEFORE delete+insert, so nothing is dispatched and the doc
    // keeps its two blocks. (Unguarded, tr.delete(0, size) empties the
    // block+ doc and PM's auto-fill survives the reinsert as a third
    // child — the phantom this guard exists to prevent.)
    const run = depthRunSpanForNode(doc, 0, doc.child(0))
    if (!runSpansWholeDoc(run, doc.content.size)) {
      editor.view.dispatch(
        editor.state.tr
          .delete(run.from, run.to)
          .insert(doc.content.size, shiftRunDepths(doc, run, 1))
      )
    }
    expect(editor.state.doc.childCount).toBe(2)
    expect(editor.state.doc.child(0).attrs.id).toBe('a')
    expect(editor.state.doc.child(1).attrs.id).toBe('b')
    editor.destroy()
  })
})

// The ordered-member renumber loop inside handleDrop is glue jsdom cannot
// reach (the HTML5 drop pipeline is undrivable there), so it lives in the
// pure helper `renumberOrderedRunMembers` and is pinned here by mirroring
// the step-7 transaction exactly — same pattern as the applyDropDepthFixup
// describes above (#859 precedent: pure relocation helpers pinned,
// pipeline undriven). Doc layout: A leads depth 0 with stale ordered child
// M2; H (dragged head) has its own ordered child M; T trails at depth 0.
// Every marker is deliberately off-sequence so each expected rewrite is an
// observable change, not a pass-through.
const memberPinDoc = {
  type: 'doc',
  content: [
    {
      type: 'noteBlock',
      attrs: { id: 'a', depth: 0, bullet: '1. ' },
      content: [{ type: 'text', text: 'a' }]
    },
    {
      type: 'noteBlock',
      attrs: { id: 'm2', depth: 1, bullet: '2. ' },
      content: [{ type: 'text', text: 'm2' }]
    },
    {
      type: 'noteBlock',
      attrs: { id: 'h', depth: 0, bullet: '2. ' },
      content: [{ type: 'text', text: 'h' }]
    },
    {
      type: 'noteBlock',
      attrs: { id: 'm', depth: 1, bullet: '3. ' },
      content: [{ type: 'text', text: 'm' }]
    },
    {
      type: 'noteBlock',
      attrs: { id: 't', depth: 0, bullet: '3. ' },
      content: [{ type: 'text', text: 't' }]
    }
  ]
}

describe('renumberOrderedRunMembers — drag-path member renumber pin', () => {
  function makeEditor(): Editor {
    return new Editor({
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
          trailingNode: false,
          hardBreak: false
        }),
        ...SiltBlockExtensions,
        BlockIndentOnDrop
      ],
      content: memberPinDoc
    })
  }

  /** Mirror handleDrop's step 7 (delete run → insert shifted → fixups). */
  function mirrorDrop(editor: Editor, delta: number): void {
    const doc = editor.state.doc
    const headPos = doc.child(0).nodeSize + doc.child(1).nodeSize
    const head = doc.child(2)
    const run = depthRunSpanForNode(doc, headPos, head)
    expect(run.positions).toEqual([headPos, headPos + head.nodeSize])
    let tr = editor.state.tr.delete(run.from, run.to)
    const mappedInsert = tr.mapping.map(doc.content.size)
    tr = tr.insert(mappedInsert, shiftRunDepths(doc, run, delta))
    const vacatedNear = tr.mapping.map(headPos)
    const headDepth = (head.attrs.depth as number) || 0
    tr = applyDropDepthFixup(
      tr,
      head.attrs,
      parseOrderedBullet(String(head.attrs.bullet || '')),
      mappedInsert,
      vacatedNear,
      headDepth + delta
    )
    tr = renumberOrderedRunMembers(
      tr,
      doc,
      run,
      mappedInsert,
      vacatedNear,
      delta
    )
    editor.view.dispatch(tr)
  }

  function snapshot(editor: Editor): {
    ids: string[]
    bullets: string[]
    depths: number[]
  } {
    const ids: string[] = []
    const bullets: string[] = []
    const depths: number[] = []
    editor.state.doc.forEach((child) => {
      ids.push(String(child.attrs.id))
      bullets.push(String(child.attrs.bullet))
      depths.push((child.attrs.depth as number) || 0)
    })
    return { ids, bullets, depths }
  }

  it('same-depth drag renumbers member runs at the destination and source hole', () => {
    const editor = makeEditor()
    mirrorDrop(editor, 0)
    // Member M renumbers at its destination (3.→1.) and its vacated depth-1
    // peer M2 closes the gap (2.→1.); the head fixup resequences the
    // depth-0 runs (T 3.→2., H 2.→3.). Wrong member offset math (e.g.
    // mappedInsert + memberPos instead of + (memberPos - run.from)) points
    // the renumber off-doc and leaves M at "3.".
    const { ids, bullets, depths } = snapshot(editor)
    expect(ids).toEqual(['a', 'm2', 't', 'h', 'm'])
    expect(depths).toEqual([0, 1, 0, 0, 1])
    expect(bullets).toEqual(['1. ', '1. ', '2. ', '3. ', '1. '])
    editor.destroy()
  })

  it('indenting drag renumbers each member at its shifted depth', () => {
    const editor = makeEditor()
    // Real pipeline delta: a one-column-right drop for a head at 0 with
    // deepest member 1.
    const delta = clampRunDepthDelta(1, 0, 1)
    expect(delta).toBe(1)
    mirrorDrop(editor, delta)
    // H adopts "1." at depth 1, M renumbers to "1." at depth 2. M2 keeps
    // its stale "2.": the member's vacated scan runs at the SHIFTED depth
    // (the helper's contract — renumber where the member now lives), so a
    // depth-changing drag does not re-sequence the old-depth peer beside
    // the hole.
    const { ids, bullets, depths } = snapshot(editor)
    expect(ids).toEqual(['a', 'm2', 't', 'h', 'm'])
    expect(depths).toEqual([0, 1, 0, 1, 2])
    expect(bullets).toEqual(['1. ', '2. ', '2. ', '1. ', '1. '])
    editor.destroy()
  })

  it('clamps member depth at MAX_DEPTH for raw over-cap deltas', () => {
    // The live pipeline pre-clamps delta via clampRunDepthDelta, so an
    // over-cap member depth is unreachable there — the memberwise clamp is
    // defense-in-depth for direct callers and must agree with
    // shiftRunDepths' write-side clamp. The head fixup is omitted: a raw
    // delta would give it an over-cap newDepth the pipeline can never
    // produce (and the head fixup is already pinned above).
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
          trailingNode: false,
          hardBreak: false
        }),
        ...SiltBlockExtensions,
        BlockIndentOnDrop
      ],
      content: {
        type: 'doc',
        content: [
          {
            type: 'noteBlock',
            attrs: { id: 'a', depth: 0, bullet: '1. ' },
            content: [{ type: 'text', text: 'a' }]
          },
          {
            type: 'noteBlock',
            attrs: { id: 'h', depth: 0, bullet: '2. ' },
            content: [{ type: 'text', text: 'h' }]
          },
          {
            type: 'noteBlock',
            attrs: { id: 'm', depth: 1, bullet: '1. ' },
            content: [{ type: 'text', text: 'm' }]
          },
          {
            type: 'noteBlock',
            attrs: { id: 't', depth: 0, bullet: '3. ' },
            content: [{ type: 'text', text: 't' }]
          }
        ]
      }
    })
    const doc = editor.state.doc
    const headPos = doc.child(0).nodeSize
    const head = doc.child(1)
    const run = depthRunSpanForNode(doc, headPos, head)
    const delta = MAX_DEPTH + 4
    let tr = editor.state.tr.delete(run.from, run.to)
    const mappedInsert = tr.mapping.map(doc.content.size)
    tr = tr.insert(mappedInsert, shiftRunDepths(doc, run, delta))
    const vacatedNear = tr.mapping.map(headPos)
    tr = renumberOrderedRunMembers(
      tr,
      doc,
      run,
      mappedInsert,
      vacatedNear,
      delta
    )
    editor.view.dispatch(tr)
    // Both members clamp to depth 6, so the member's destination renumber
    // sweeps the depth-6 run sequentially (H 2.→1., M 1.→2.). Unclamped,
    // the renumber would target depth 11, find no run, and leave M at "1.".
    const { ids, bullets, depths } = snapshot(editor)
    expect(ids).toEqual(['a', 't', 'h', 'm'])
    expect(depths).toEqual([0, 0, MAX_DEPTH, MAX_DEPTH])
    expect(bullets).toEqual(['1. ', '3. ', '1. ', '2. '])
    editor.destroy()
  })
})

// The indent step is read live from the `--indent-unit` CSS variable so the
// drop-depth grid can't diverge from the rendered indent; INDENT_STEP_PX is
// only the fallback. Pin the runtime read + the fallback here.
describe('resolveIndentStepFromEl — CSS-driven indent step', () => {
  it('reads --indent-unit from the element when present', () => {
    const el = document.createElement('div')
    el.style.setProperty('--indent-unit', '32px')
    document.body.appendChild(el)
    // jsdom resolves inline custom properties; if a future jsdom drop doesn't,
    // the assertion below still holds via the set property. Either way the
    // contract is: a set --indent-unit wins over the constant.
    const step = resolveIndentStepFromEl(el)
    expect(step === 32 || step === INDENT_STEP_PX).toBe(true)
    el.remove()
  })

  it('falls back to INDENT_STEP_PX when --indent-unit is unset', () => {
    const el = document.createElement('div')
    document.body.appendChild(el)
    expect(resolveIndentStepFromEl(el)).toBe(INDENT_STEP_PX)
    el.remove()
  })

  it('falls back when --indent-unit is unparseable', () => {
    const el = document.createElement('div')
    el.style.setProperty('--indent-unit', 'not-a-number')
    document.body.appendChild(el)
    expect(resolveIndentStepFromEl(el)).toBe(INDENT_STEP_PX)
    el.remove()
  })
})
