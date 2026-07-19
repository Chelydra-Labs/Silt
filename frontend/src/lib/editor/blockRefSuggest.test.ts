import { describe, expect, it, vi } from 'vitest'
import { Editor } from '@tiptap/core'
import StarterKit from '@tiptap/starter-kit'
import {
  BlockReferenceNode,
  CodeBlock,
  SiltBlockExtensions,
  SiltInlineMarkExtensions
} from './schema'
import {
  BlockRefSuggest,
  applyBlockRefSuggestion,
  getBlkRefContext
} from './blockRefSuggest'
import type { DocJSON } from './types'

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
      ...SiltInlineMarkExtensions,
      CodeBlock,
      BlockReferenceNode,
      BlockRefSuggest
    ]
  })
}

function noteDoc(text: string): DocJSON {
  return {
    type: 'doc',
    content: [
      {
        type: 'noteBlock',
        attrs: { id: 'b1', depth: 0, bullet: '- ' },
        content: text ? [{ type: 'text', text }] : undefined
      }
    ]
  }
}

function contextFor(text: string) {
  const editor = makeEditor()
  editor.commands.setContent(noteDoc(text))
  editor.commands.setTextSelection(1 + text.length)
  const context = getBlkRefContext(editor.state)
  editor.destroy()
  return context
}

describe('BlockRefSuggest context detection', () => {
  it('detects line-start and whitespace-delimited triggers', () => {
    expect(contextFor('((')?.query).toBe('')
    expect(contextFor('See ((design notes')?.query).toBe('design notes')
  })

  it('accepts arbitrary search text except closing parentheses and newlines', () => {
    expect(contextFor('((API: v2 / auth!')?.query).toBe('API: v2 / auth!')
    expect(contextFor('((API)')).toBeNull()
  })

  it('rejects a trigger in the middle of a token or after punctuation', () => {
    expect(contextFor('word((query')).toBeNull()
    expect(contextFor('word.((query')).toBeNull()
  })

  it('rejects an expanded selection', () => {
    const editor = makeEditor()
    editor.commands.setContent(noteDoc('((query'))
    editor.commands.setTextSelection({ from: 1, to: 3 })
    expect(getBlkRefContext(editor.state)).toBeNull()
    editor.destroy()
  })

  it('rejects fenced and inline code', () => {
    const editor = makeEditor()
    editor.commands.setContent({
      type: 'doc',
      content: [
        {
          type: 'codeBlock',
          attrs: { id: 'c1', depth: 0, language: '' },
          content: [{ type: 'text', text: '((query' }]
        }
      ]
    })
    editor.commands.setTextSelection(8)
    expect(getBlkRefContext(editor.state)).toBeNull()

    editor.commands.setContent({
      type: 'doc',
      content: [
        {
          type: 'noteBlock',
          attrs: { id: 'b1', depth: 0, bullet: '- ' },
          content: [
            {
              type: 'text',
              text: '((query',
              marks: [{ type: 'code' }]
            }
          ]
        }
      ]
    })
    editor.commands.setTextSelection(8)
    expect(getBlkRefContext(editor.state)).toBeNull()
    editor.destroy()
  })
})

describe('applyBlockRefSuggestion', () => {
  it('replaces the trigger range with one atomic node in one dispatch', () => {
    const editor = makeEditor()
    editor.commands.setContent(noteDoc('See ((design notes'))
    editor.commands.setTextSelection(19)
    const originalDispatch = editor.view.dispatch.bind(editor.view)
    const dispatch = vi
      .spyOn(editor.view, 'dispatch')
      .mockImplementation(originalDispatch)

    expect(applyBlockRefSuggestion(editor, 'block-123')).toBe(true)
    expect(dispatch).toHaveBeenCalledTimes(1)
    const block = editor.getJSON().content![0] as {
      content?: Array<{ type: string; attrs?: { uuid?: string } }>
    }
    const ref = block.content?.find(
      (node) => node.type === 'blockReferenceNode'
    )
    expect(ref?.attrs?.uuid).toBe('block-123')
    expect(editor.state.doc.textContent).toBe('See ')
    expect(editor.state.selection.empty).toBe(true)
    editor.destroy()
  })

  it('does nothing without an active context', () => {
    const editor = makeEditor()
    editor.commands.setContent(noteDoc('plain text'))
    editor.commands.setTextSelection(5)
    expect(applyBlockRefSuggestion(editor, 'block-123')).toBe(false)
    expect(editor.state.doc.textContent).toBe('plain text')
    editor.destroy()
  })
})
