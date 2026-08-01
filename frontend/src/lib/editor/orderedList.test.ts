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
  applyDepthChangeOnTransaction
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
