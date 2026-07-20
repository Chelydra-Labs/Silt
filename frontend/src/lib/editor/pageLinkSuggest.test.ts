import { describe, expect, it, vi } from 'vitest'
import { Editor } from '@tiptap/core'
import StarterKit from '@tiptap/starter-kit'
import {
  CodeBlock,
  PageLinkNode,
  SiltBlockExtensions,
  SiltInlineMarkExtensions
} from './schema'
import {
  PageLinkSuggest,
  applyPageLinkSuggestion,
  getPageLinkContext,
  normalizePageLinkAlias,
  pageLinkPath,
  rankPageLinks
} from './pageLinkSuggest'
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
      PageLinkNode,
      PageLinkSuggest
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
  const context = getPageLinkContext(editor.state)
  editor.destroy()
  return context
}

describe('PageLinkSuggest context detection', () => {
  it('detects line-start and whitespace-delimited triggers', () => {
    expect(contextFor('[[')?.query).toBe('')
    expect(contextFor('See [[launch plan')?.query).toBe('launch plan')
  })

  it('rejects mid-token, alias, heading, and completed-link modes', () => {
    expect(contextFor('word[[page')).toBeNull()
    expect(contextFor('[[page|alias')).toBeNull()
    expect(contextFor('[[page#heading')).toBeNull()
    expect(contextFor('[[page]]')).toBeNull()
  })

  it('rejects expanded selections and code', () => {
    const editor = makeEditor()
    editor.commands.setContent(noteDoc('[['))
    editor.commands.setTextSelection({ from: 1, to: 2 })
    expect(getPageLinkContext(editor.state)).toBeNull()

    editor.commands.setContent({
      type: 'doc',
      content: [
        {
          type: 'codeBlock',
          attrs: { id: 'c1', depth: 0, language: '' },
          content: [{ type: 'text', text: '[[page' }]
        }
      ]
    })
    editor.commands.setTextSelection(7)
    expect(getPageLinkContext(editor.state)).toBeNull()

    editor.commands.setContent({
      type: 'doc',
      content: [
        {
          type: 'noteBlock',
          attrs: { id: 'b1', depth: 0, bullet: '- ' },
          content: [{ type: 'text', text: '[[page', marks: [{ type: 'code' }] }]
        }
      ]
    })
    editor.commands.setTextSelection(7)
    expect(getPageLinkContext(editor.state)).toBeNull()
    editor.destroy()
  })
})

describe('page-link ranking and insertion', () => {
  it('reranks server results by fuzzy page-name score', () => {
    const items = [
      { notebook: 'NB', section: 'S', page: 'Airplane Notes' },
      { notebook: 'NB', section: 'S', page: 'Planning' },
      { notebook: 'NB', section: 'S', page: 'Plan' }
    ]
    expect(rankPageLinks(items, 'plan').map((item) => item.page)).toEqual([
      'Plan',
      'Planning',
      'Airplane Notes'
    ])
  })

  it('resolves the full path and inserts one shortest-target atomic node', async () => {
    const editor = makeEditor()
    editor.commands.setContent(noteDoc('See [[plan'))
    editor.commands.setTextSelection(11)
    const resolve = vi.fn().mockResolvedValue({
      exists: true,
      shortest: 'Roadmap'
    })
    const originalDispatch = editor.view.dispatch.bind(editor.view)
    const dispatch = vi
      .spyOn(editor.view, 'dispatch')
      .mockImplementation(originalDispatch)

    await expect(
      applyPageLinkSuggestion(
        editor,
        { notebook: 'Work', section: 'Plans', page: 'Roadmap' },
        resolve,
        'Launch]| plan\nv2'
      )
    ).resolves.toBe(true)
    expect(resolve).toHaveBeenCalledWith('Work/Plans/Roadmap')
    expect(dispatch).toHaveBeenCalledTimes(1)
    const link = (
      editor.getJSON().content![0] as {
        content?: Array<{
          type: string
          attrs?: Record<string, string | null>
        }>
      }
    ).content?.find((node) => node.type === 'pageLinkNode')
    expect(link?.attrs).toMatchObject({
      target: 'Roadmap',
      heading: null,
      alias: 'Launch plan v2'
    })
    expect(editor.state.selection.empty).toBe(true)
    editor.destroy()
  })

  it('qualifies linked-root resolution and preserves that target in one atomic node', async () => {
    const editor = makeEditor()
    editor.commands.setContent(noteDoc('See [[road'))
    editor.commands.setTextSelection(11)
    const resolve = vi.fn().mockResolvedValue({
      exists: true,
      shortest: 'linked:team-drive/Work/Plans/Roadmap'
    })

    await expect(
      applyPageLinkSuggestion(
        editor,
        {
          source: 'linked:team-drive',
          notebook: 'Work',
          section: 'Plans',
          page: 'Roadmap'
        },
        resolve
      )
    ).resolves.toBe(true)

    expect(resolve).toHaveBeenCalledWith('linked:team-drive/Work/Plans/Roadmap')
    const content = (editor.getJSON().content?.[0].content ?? []) as Array<{
      type?: string
      attrs?: Record<string, unknown>
    }>
    expect(content.filter((node) => node.type === 'pageLinkNode')).toHaveLength(
      1
    )
    expect(
      content.find((node) => node.type === 'pageLinkNode')?.attrs?.target
    ).toBe('linked:team-drive/Work/Plans/Roadmap')
    editor.destroy()
  })

  it('keeps vault lookup paths unqualified and strips wiki-link delimiters from aliases', () => {
    expect(
      pageLinkPath({
        source: 'vault',
        notebook: 'Work',
        section: 'Plans',
        page: 'Roadmap'
      })
    ).toBe('Work/Plans/Roadmap')
    expect(normalizePageLinkAlias('Launch]|plan\nnext')).toBe('Launchplan next')
  })

  it('does not mutate the document when resolution fails', async () => {
    const editor = makeEditor()
    editor.commands.setContent(noteDoc('[['))
    editor.commands.setTextSelection(3)
    await expect(
      applyPageLinkSuggestion(
        editor,
        { notebook: 'NB', section: 'S', page: 'Missing' },
        vi.fn().mockResolvedValue({ exists: false, shortest: '' })
      )
    ).rejects.toThrow('could not be resolved')
    expect(editor.state.doc.textContent).toBe('[[')
    editor.destroy()
  })
})
