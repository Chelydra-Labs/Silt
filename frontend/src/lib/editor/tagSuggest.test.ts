import { describe, expect, it, vi } from 'vitest'
import { Editor } from '@tiptap/core'
import StarterKit from '@tiptap/starter-kit'
import {
  CodeBlock,
  SiltBlockExtensions,
  SiltInlineMarkExtensions
} from './schema'
import {
  TagSuggest,
  applyTagSuggestion,
  filterTags,
  flattenTagHierarchy,
  getTagContext,
  rankTags,
  type TagItem
} from './tagSuggest'
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
      TagSuggest
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
  const context = getTagContext(editor.state)
  editor.destroy()
  return context
}

describe('TagSuggest context detection', () => {
  it('detects line-start and whitespace-delimited tag paths', () => {
    expect(contextFor('#')?.query).toBe('')
    expect(contextFor('Use #Work_2/project-name')?.query).toBe(
      'Work_2/project-name'
    )
  })

  it('rejects mid-token triggers and characters outside the tag pattern', () => {
    expect(contextFor('word#tag')).toBeNull()
    expect(contextFor('word.#tag')).toBeNull()
    expect(contextFor('#tag name')).toBeNull()
    expect(contextFor('#tag!')).toBeNull()
    expect(contextFor('#123start')).toBeNull()
    expect(contextFor('#étiquette')).toBeNull()
  })

  it('accepts tag paths through the backend byte limit only', () => {
    expect(contextFor(`#${'a'.repeat(256)}`)?.query).toHaveLength(256)
    expect(contextFor(`#${'a'.repeat(257)}`)).toBeNull()
  })

  it('rejects expanded selections and code', () => {
    const editor = makeEditor()
    editor.commands.setContent(noteDoc('#tag'))
    editor.commands.setTextSelection({ from: 1, to: 3 })
    expect(getTagContext(editor.state)).toBeNull()

    editor.commands.setContent({
      type: 'doc',
      content: [
        {
          type: 'codeBlock',
          attrs: { id: 'c1', depth: 0, language: '' },
          content: [{ type: 'text', text: '#tag' }]
        }
      ]
    })
    editor.commands.setTextSelection(5)
    expect(getTagContext(editor.state)).toBeNull()

    editor.commands.setContent({
      type: 'doc',
      content: [
        {
          type: 'noteBlock',
          attrs: { id: 'b1', depth: 0, bullet: '- ' },
          content: [{ type: 'text', text: '#tag', marks: [{ type: 'code' }] }]
        }
      ]
    })
    editor.commands.setTextSelection(5)
    expect(getTagContext(editor.state)).toBeNull()
    editor.destroy()
  })
})

describe('tag hierarchy ranking', () => {
  const tags: TagItem[] = [
    { path: 'work/project', count: 3 },
    { path: 'alpha', count: 2 },
    { path: 'archive', count: 9 },
    { path: 'personal', count: 2 }
  ]

  it('flattens hierarchy nodes and de-duplicates paths', () => {
    expect(
      flattenTagHierarchy([
        {
          name: 'work',
          path: 'work',
          count: 4,
          children: [
            {
              name: 'project',
              path: 'work/project',
              count: 3,
              children: []
            }
          ]
        },
        { name: 'work', path: 'work', count: 2, children: [] }
      ])
    ).toEqual([
      { path: 'work', count: 4 },
      { path: 'work/project', count: 3 }
    ])
  })

  it('sorts recents first, then count descending and alphabetically', () => {
    expect(
      rankTags(tags, ['personal', 'WORK/PROJECT']).map((tag) => tag.path)
    ).toEqual(['personal', 'work/project', 'archive', 'alpha'])
  })

  it('uses prefix matches before fuzzy-score fallback', () => {
    expect(filterTags(tags, 'ar', []).map((tag) => tag.path)).toEqual([
      'archive'
    ])
    expect(filterTags(tags, 'wrkprj', []).map((tag) => tag.path)).toEqual([
      'work/project'
    ])
    expect(filterTags(tags, 'zzz', [])).toEqual([])
  })
})

describe('applyTagSuggestion', () => {
  it('atomically replaces the active query with literal tag text', () => {
    const editor = makeEditor()
    editor.commands.setContent(noteDoc('Use #wor'))
    editor.commands.setTextSelection(9)
    const originalDispatch = editor.view.dispatch.bind(editor.view)
    const dispatch = vi
      .spyOn(editor.view, 'dispatch')
      .mockImplementation(originalDispatch)

    expect(applyTagSuggestion(editor, 'work/project')).toBe(true)
    expect(dispatch).toHaveBeenCalledTimes(1)
    expect(editor.state.doc.textContent).toBe('Use #work/project')
    expect(editor.getJSON().content?.[0].content).toEqual([
      { type: 'text', text: 'Use #work/project' }
    ])
    editor.destroy()
  })

  it('does nothing without a valid active context or path', () => {
    const editor = makeEditor()
    editor.commands.setContent(noteDoc('plain'))
    editor.commands.setTextSelection(6)
    expect(applyTagSuggestion(editor, 'work')).toBe(false)
    expect(applyTagSuggestion(editor, 'bad path')).toBe(false)
    expect(applyTagSuggestion(editor, '123start')).toBe(false)
    expect(editor.state.doc.textContent).toBe('plain')
    editor.destroy()
  })
})
