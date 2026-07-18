import { describe, it, expect, afterEach } from 'vitest'
import { Editor } from '@tiptap/core'
import StarterKit from '@tiptap/starter-kit'
import { SiltBlockExtensions } from './index'
import { extractHeadings, extractHeadingsFromEditor } from './outline'

const editors: Editor[] = []
function track(e: Editor): Editor {
  editors.push(e)
  return e
}
afterEach(() => {
  while (editors.length) {
    const e = editors.pop()
    if (e && !e.isDestroyed) e.destroy()
  }
})

function makeEditor(content: object): Editor {
  return track(
    new Editor({
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
        ...SiltBlockExtensions
      ],
      content
    })
  )
}

describe('outline extractHeadings (#659)', () => {
  it('lists H1–H6 in document order with depth and text', () => {
    const editor = makeEditor({
      type: 'doc',
      content: [
        {
          type: 'headerBlock',
          attrs: { id: 'h1', depth: 1 },
          content: [{ type: 'text', text: 'Title' }]
        },
        {
          type: 'noteBlock',
          attrs: { id: 'n1', depth: 0, bullet: '' },
          content: [{ type: 'text', text: 'body' }]
        },
        {
          type: 'headerBlock',
          attrs: { id: 'h2', depth: 2 },
          content: [{ type: 'text', text: 'Section' }]
        },
        {
          type: 'headerBlock',
          attrs: { id: 'h3', depth: 3 },
          content: [{ type: 'text', text: 'Sub' }]
        }
      ]
    })
    const items = extractHeadingsFromEditor(editor)
    expect(items).toHaveLength(3)
    expect(items.map((i) => i.depth)).toEqual([1, 2, 3])
    expect(items.map((i) => i.text)).toEqual(['Title', 'Section', 'Sub'])
    expect(items.map((i) => i.id)).toEqual(['h1', 'h2', 'h3'])
  })

  it('returns empty for docs without headers', () => {
    const editor = makeEditor({
      type: 'doc',
      content: [
        {
          type: 'noteBlock',
          attrs: { id: 'n1', depth: 0, bullet: '' },
          content: [{ type: 'text', text: 'only note' }]
        }
      ]
    })
    expect(extractHeadings(editor.state.doc)).toEqual([])
  })
})
