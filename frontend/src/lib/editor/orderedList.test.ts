import { describe, it, expect } from 'vitest'
import { Editor } from '@tiptap/core'
import StarterKit from '@tiptap/starter-kit'
import {
  SiltBlockExtensions,
  SiltInlineMarkExtensions,
  SiltColorMarkExtensions,
  UniqueBlockIds,
  SiltHardBreak
} from './index'
import { EmbedNode, BlockReferenceNode } from './schema'
import {
  parseOrderedBullet,
  formatOrderedOutlineLabel,
  applyDepthChangeOnTransaction,
  renumberAfterOrderedBlockMove
} from './orderedList'

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
      SiltHardBreak,
      ...SiltBlockExtensions,
      ...SiltInlineMarkExtensions,
      ...SiltColorMarkExtensions,
      EmbedNode,
      BlockReferenceNode,
      UniqueBlockIds
    ]
  })
}

describe('parseOrderedBullet', () => {
  it('parses dot and paren styles', () => {
    expect(parseOrderedBullet('1. ')).toEqual({ n: 1, punc: '. ' })
    expect(parseOrderedBullet('12) ')).toEqual({ n: 12, punc: ') ' })
  })

  it('rejects unordered and bare text', () => {
    expect(parseOrderedBullet('- ')).toBeNull()
    expect(parseOrderedBullet('')).toBeNull()
    expect(parseOrderedBullet('1.1) ')).toBeNull()
  })
})

describe('formatOrderedOutlineLabel', () => {
  it('builds hierarchical labels for nested ordered items', () => {
    const editor = makeEditor()
    editor.commands.setContent({
      type: 'doc',
      content: [
        {
          type: 'noteBlock',
          attrs: { id: 'n1', depth: 0, bullet: '1) ' },
          content: [{ type: 'text', text: 'one' }]
        },
        {
          type: 'noteBlock',
          attrs: { id: 'n2', depth: 1, bullet: '1) ' },
          content: [{ type: 'text', text: 'nested' }]
        },
        {
          type: 'noteBlock',
          attrs: { id: 'n3', depth: 1, bullet: '2) ' },
          content: [{ type: 'text', text: 'nested two' }]
        },
        {
          type: 'noteBlock',
          attrs: { id: 'n4', depth: 0, bullet: '2) ' },
          content: [{ type: 'text', text: 'two' }]
        }
      ]
    })
    const p0 = 0
    const p1 = editor.state.doc.child(0).nodeSize
    const p2 = p1 + editor.state.doc.child(1).nodeSize
    const p3 = p2 + editor.state.doc.child(2).nodeSize
    expect(formatOrderedOutlineLabel(editor.state.doc, p0)).toBe('1)')
    expect(formatOrderedOutlineLabel(editor.state.doc, p1)).toBe('1.1)')
    expect(formatOrderedOutlineLabel(editor.state.doc, p2)).toBe('1.2)')
    expect(formatOrderedOutlineLabel(editor.state.doc, p3)).toBe('2)')
    editor.destroy()
  })
})

describe('applyDepthChangeOnTransaction', () => {
  it('renumbers on indent within a transaction', () => {
    const editor = makeEditor()
    editor.commands.setContent({
      type: 'doc',
      content: [
        {
          type: 'noteBlock',
          attrs: { id: 'n1', depth: 0, bullet: '1. ' },
          content: [{ type: 'text', text: 'a' }]
        },
        {
          type: 'noteBlock',
          attrs: { id: 'n2', depth: 0, bullet: '2. ' },
          content: [{ type: 'text', text: 'b' }]
        },
        {
          type: 'noteBlock',
          attrs: { id: 'n3', depth: 0, bullet: '3. ' },
          content: [{ type: 'text', text: 'c' }]
        }
      ]
    })
    const mid = editor.state.doc.child(0).nodeSize
    const tr = applyDepthChangeOnTransaction(editor.state.tr, mid, 1)
    editor.view.dispatch(tr)
    expect(editor.state.doc.child(0).attrs.bullet).toBe('1. ')
    expect(editor.state.doc.child(1).attrs).toMatchObject({
      bullet: '1. ',
      depth: 1
    })
    expect(editor.state.doc.child(2).attrs.bullet).toBe('2. ')
    editor.destroy()
  })

  it('adopts destination-run punctuation when unindenting into a ) list', () => {
    const editor = makeEditor()
    editor.commands.setContent({
      type: 'doc',
      content: [
        {
          type: 'noteBlock',
          attrs: { id: 'n1', depth: 0, bullet: '1) ' },
          content: [{ type: 'text', text: 'a' }]
        },
        {
          type: 'noteBlock',
          attrs: { id: 'n2', depth: 1, bullet: '1. ' },
          content: [{ type: 'text', text: 'b' }]
        },
        {
          type: 'noteBlock',
          attrs: { id: 'n3', depth: 0, bullet: '2) ' },
          content: [{ type: 'text', text: 'c' }]
        }
      ]
    })
    const mid = editor.state.doc.child(0).nodeSize
    const tr = applyDepthChangeOnTransaction(editor.state.tr, mid, 0)
    editor.view.dispatch(tr)
    expect(
      [0, 1, 2].map((i) => editor.state.doc.child(i).attrs.bullet)
    ).toEqual(['1) ', '2) ', '3) '])
    editor.destroy()
  })
})

describe('renumberAfterOrderedBlockMove', () => {
  it('renumbers same-depth reorder 1. 2. 3. → move first after last', () => {
    const editor = makeEditor()
    editor.commands.setContent({
      type: 'doc',
      content: [
        {
          type: 'noteBlock',
          attrs: { id: 'n1', depth: 0, bullet: '1. ' },
          content: [{ type: 'text', text: 'a' }]
        },
        {
          type: 'noteBlock',
          attrs: { id: 'n2', depth: 0, bullet: '2. ' },
          content: [{ type: 'text', text: 'b' }]
        },
        {
          type: 'noteBlock',
          attrs: { id: 'n3', depth: 0, bullet: '3. ' },
          content: [{ type: 'text', text: 'c' }]
        }
      ]
    })
    const first = editor.state.doc.child(0)
    const firstSize = first.nodeSize
    let tr = editor.state.tr.delete(0, firstSize)
    const insertAt = tr.doc.content.size
    tr = tr.insert(insertAt, first)
    const vacatedNear = tr.mapping.map(0)
    tr = renumberAfterOrderedBlockMove(tr, insertAt, vacatedNear, 0, '. ')
    editor.view.dispatch(tr)
    expect(
      [0, 1, 2].map((i) => editor.state.doc.child(i).attrs.bullet)
    ).toEqual(['1. ', '2. ', '3. '])
    expect([0, 1, 2].map((i) => editor.state.doc.child(i).textContent)).toEqual(
      ['b', 'c', 'a']
    )
    editor.destroy()
  })

  it('renumbers vacated source when moving ordered across a plain gap', () => {
    const editor = makeEditor()
    editor.commands.setContent({
      type: 'doc',
      content: [
        {
          type: 'noteBlock',
          attrs: { id: 'n1', depth: 0, bullet: '1. ' },
          content: [{ type: 'text', text: 'a' }]
        },
        {
          type: 'noteBlock',
          attrs: { id: 'n2', depth: 0, bullet: '2. ' },
          content: [{ type: 'text', text: 'b' }]
        },
        {
          type: 'noteBlock',
          attrs: { id: 'plain', depth: 0, bullet: '' },
          content: [{ type: 'text', text: 'gap' }]
        },
        {
          type: 'noteBlock',
          attrs: { id: 'n3', depth: 0, bullet: '1. ' },
          content: [{ type: 'text', text: 'c' }]
        }
      ]
    })
    // Move n2 (2.) after plain into the second run.
    const n1Size = editor.state.doc.child(0).nodeSize
    const n2 = editor.state.doc.child(1)
    const n2Size = n2.nodeSize
    const n2Pos = n1Size
    let tr = editor.state.tr.delete(n2Pos, n2Pos + n2Size)
    // After delete: n1, plain, n3 — insert after plain (before n3).
    const plainPos = tr.doc.child(0).nodeSize
    const plainSize = tr.doc.child(1).nodeSize
    const insertAt = plainPos + plainSize
    tr = tr.insert(insertAt, n2)
    const vacatedNear = tr.mapping.map(n2Pos)
    tr = renumberAfterOrderedBlockMove(tr, insertAt, vacatedNear, 0, '. ')
    editor.view.dispatch(tr)
    // n1 alone → 1.; moved+n3 → 1. 2.
    expect(
      [0, 1, 2, 3].map((i) => ({
        t: editor.state.doc.child(i).textContent,
        b: editor.state.doc.child(i).attrs.bullet
      }))
    ).toEqual([
      { t: 'a', b: '1. ' },
      { t: 'gap', b: '' },
      { t: 'b', b: '1. ' },
      { t: 'c', b: '2. ' }
    ])
    editor.destroy()
  })
})
