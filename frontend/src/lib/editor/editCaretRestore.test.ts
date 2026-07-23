import { describe, it, expect, vi } from 'vitest'
import {
  snapshotEditCaret,
  resolveCaretInDoc,
  applyEditCaret
} from './editCaretRestore'
import type { Node as ProseMirrorNode } from '@tiptap/pm/model'

/** Minimal Editor stub for snapshotEditCaret (stable-id walk + $from.start). */
function makeEditor(opts: {
  blockType?: string
  blockId?: string | null
  depth?: number
  pos?: number
  contentStart?: number
  /** Per-depth content starts when the matched id is not at leaf depth. */
  contentStarts?: Record<number, number>
  /** Extra ancestors from outer→inner (excluding the leaf block at depth). */
  ancestors?: { type: string; id?: string | null }[]
}): Parameters<typeof snapshotEditCaret>[0] {
  const depth = opts.depth ?? 1
  const blockType = opts.blockType ?? 'noteBlock'
  const blockId = opts.blockId === undefined ? 'block-a' : opts.blockId
  const pos = opts.pos ?? 5
  const contentStart = opts.contentStart ?? 1
  const contentStarts = opts.contentStarts ?? {}
  const ancestors = opts.ancestors ?? []
  return {
    state: {
      selection: {
        from: pos,
        $from: {
          depth,
          pos,
          start: (d: number) => {
            if (contentStarts[d] !== undefined) return contentStarts[d]
            return d === depth ? contentStart : 0
          },
          node: (d: number) => {
            if (d === depth) {
              return {
                type: { name: blockType },
                attrs: { id: blockId }
              }
            }
            // ancestors[0] is depth 1, … ancestors[depth-2] is depth-1
            const anc = ancestors[d - 1]
            if (anc) {
              return {
                type: { name: anc.type },
                attrs: { id: anc.id ?? null }
              }
            }
            return { type: { name: 'doc' }, attrs: {} }
          }
        }
      }
    }
  } as Parameters<typeof snapshotEditCaret>[0]
}

/** Minimal doc stub for resolveCaretInDoc. */
function makeDoc(
  blocks: { id: string; pos: number; contentSize: number }[]
): ProseMirrorNode {
  return {
    descendants(f: (node: ProseMirrorNode, pos: number) => boolean | void) {
      for (const b of blocks) {
        const cont = f(
          {
            attrs: { id: b.id },
            content: { size: b.contentSize }
          } as never,
          b.pos
        )
        if (cont === false) break
      }
    }
  } as unknown as ProseMirrorNode
}

describe('snapshotEditCaret', () => {
  it('returns null without an editor', () => {
    expect(snapshotEditCaret(null)).toBeNull()
    expect(snapshotEditCaret(undefined)).toBeNull()
  })

  it('returns null when no ancestor has a stable id', () => {
    const editor = makeEditor({ blockType: 'paragraph', blockId: null })
    expect(snapshotEditCaret(editor)).toBeNull()
  })

  it('returns null when the block has no id', () => {
    expect(snapshotEditCaret(makeEditor({ blockId: null }))).toBeNull()
    expect(snapshotEditCaret(makeEditor({ blockId: '' }))).toBeNull()
  })

  it('snapshots blockId and relative offset within the block', () => {
    // pos 8, content starts at 2 → offset 6
    const snap = snapshotEditCaret(
      makeEditor({ blockId: 'abc', pos: 8, contentStart: 2 })
    )
    expect(snap).toEqual({ blockId: 'abc', offsetInBlock: 6 })
  })

  it('clamps a negative relative offset to 0', () => {
    const snap = snapshotEditCaret(makeEditor({ pos: 0, contentStart: 5 }))
    expect(snap?.offsetInBlock).toBe(0)
  })

  it('captures caret inside a codeBlock (not in keymap BLOCK_TYPES)', () => {
    const snap = snapshotEditCaret(
      makeEditor({
        blockType: 'codeBlock',
        blockId: 'code-1',
        pos: 12,
        contentStart: 4
      })
    )
    expect(snap).toEqual({ blockId: 'code-1', offsetInBlock: 8 })
  })

  it('captures caret inside a table via the table node id', () => {
    // Selection in a cell: paragraph (no id) → tableCell → tableRow → table(id)
    // Matched id is table at depth 1; offset = pos - start(1) = 20 - 5 = 15.
    const snap = snapshotEditCaret(
      makeEditor({
        blockType: 'paragraph',
        blockId: null,
        depth: 4,
        pos: 20,
        contentStarts: { 1: 5 },
        ancestors: [
          { type: 'table', id: 'table-1' },
          { type: 'tableRow', id: null },
          { type: 'tableCell', id: null }
        ]
      })
    )
    expect(snap).toEqual({ blockId: 'table-1', offsetInBlock: 15 })
  })

  it('prefers the innermost stable-id ancestor', () => {
    // noteBlock inside callout: both have ids → noteBlock wins
    const snap = snapshotEditCaret(
      makeEditor({
        blockType: 'noteBlock',
        blockId: 'note-inner',
        depth: 2,
        pos: 9,
        contentStart: 5,
        ancestors: [{ type: 'calloutBlock', id: 'callout-outer' }]
      })
    )
    expect(snap).toEqual({ blockId: 'note-inner', offsetInBlock: 4 })
  })
})

describe('resolveCaretInDoc', () => {
  it('returns null when the block id is missing', () => {
    const doc = makeDoc([{ id: 'other', pos: 0, contentSize: 10 }])
    expect(
      resolveCaretInDoc(doc, { blockId: 'gone', offsetInBlock: 3 })
    ).toBeNull()
  })

  it('resolves pos + 1 + offset for a matching block', () => {
    const doc = makeDoc([
      { id: 'a', pos: 0, contentSize: 20 },
      { id: 'b', pos: 22, contentSize: 8 }
    ])
    // pos 22 + 1 + 3 = 26
    expect(resolveCaretInDoc(doc, { blockId: 'b', offsetInBlock: 3 })).toBe(26)
  })

  it('clamps offset when the block content shrank', () => {
    const doc = makeDoc([{ id: 'a', pos: 0, contentSize: 4 }])
    // offset 99 → clamp to 4 → 0 + 1 + 4 = 5
    expect(resolveCaretInDoc(doc, { blockId: 'a', offsetInBlock: 99 })).toBe(5)
  })

  it('clamps a negative offset to 0', () => {
    const doc = makeDoc([{ id: 'a', pos: 10, contentSize: 5 }])
    expect(resolveCaretInDoc(doc, { blockId: 'a', offsetInBlock: -3 })).toBe(11)
  })
})

describe('applyEditCaret', () => {
  it('returns false without editor or snapshot', () => {
    expect(applyEditCaret(null, { blockId: 'a', offsetInBlock: 0 })).toBe(false)
    expect(applyEditCaret({} as never, null)).toBe(false)
  })

  it('sets selection when resolve succeeds', () => {
    const setTextSelection = vi.fn()
    const focus = vi.fn()
    const editor = {
      isDestroyed: false,
      state: {
        doc: makeDoc([{ id: 'a', pos: 0, contentSize: 10 }])
      },
      commands: { setTextSelection, focus }
    } as never
    expect(applyEditCaret(editor, { blockId: 'a', offsetInBlock: 3 })).toBe(
      true
    )
    expect(setTextSelection).toHaveBeenCalledWith(4) // 0+1+3
    expect(focus).toHaveBeenCalled()
  })

  it('returns false when the block id is missing', () => {
    const editor = {
      isDestroyed: false,
      state: { doc: makeDoc([{ id: 'other', pos: 0, contentSize: 5 }]) },
      commands: { setTextSelection: vi.fn(), focus: vi.fn() }
    } as never
    expect(applyEditCaret(editor, { blockId: 'gone', offsetInBlock: 0 })).toBe(
      false
    )
  })
})
