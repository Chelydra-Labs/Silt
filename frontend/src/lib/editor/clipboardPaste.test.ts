// Context-menu Paste parity: the menu has no ClipboardEvent to read, so it
// feeds `pasteFromClipboard` through a clipboard payload reader and then into
// `view.pasteHTML`/`view.pasteText`. These tests use a real TipTap editor with
// the production-shaped Silt schema (plus the normalizer and PlainPaste, as on
// the real surface) so the assertion is "menu paste lands the same blocks and
// marks as Ctrl+V", not "the right function was called".
//
// The reader itself is unit-tested in clipboardHtml.test.ts; here it is mocked
// so the pipeline — not platform clipboard permissions — is under test.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { Editor } from '@tiptap/core'
import StarterKit from '@tiptap/starter-kit'
import type { JSONContent } from '@tiptap/core'

const readClipboardPayload = vi.hoisted(() => vi.fn())

vi.mock('./clipboardHtml', () => ({ readClipboardPayload }))

import {
  ExternalPasteNormalizer,
  SiltBlockExtensions,
  SiltColorMarkExtensions,
  SiltDetailsExtensions,
  SiltHardBreak,
  SiltInlineMarkExtensions,
  SiltTableExtensions,
  UniqueBlockIds
} from './index'
import { CodeBlock, siltPasteProps } from './schema'
import { PlainPaste, _setShiftHeldForTests } from './plainPaste'
import { PASTE_HTML_SIZE_CAP } from './paste/normalizePastedHTML'
import { OUTLOOK_BODY } from './paste/fixtures'
import { pasteFromClipboard, type ClipboardDeps } from './clipboard'

const editors: Editor[] = []
const notify = vi.fn()

function makeEditor(): Editor {
  const editor = new Editor({
    extensions: [
      StarterKit.configure({
        heading: false,
        bulletList: false,
        orderedList: false,
        listItem: false,
        blockquote: false,
        codeBlock: false,
        horizontalRule: false,
        trailingNode: false,
        hardBreak: false,
        link: { openOnClick: false, autolink: true }
      }),
      SiltHardBreak,
      ...SiltBlockExtensions,
      CodeBlock,
      ...SiltInlineMarkExtensions,
      ...SiltColorMarkExtensions,
      ...SiltDetailsExtensions,
      ...SiltTableExtensions,
      ExternalPasteNormalizer,
      PlainPaste,
      UniqueBlockIds
    ],
    editorProps: siltPasteProps,
    content: {
      type: 'doc',
      content: [
        {
          type: 'noteBlock',
          attrs: { id: 'seed', depth: 0, bullet: '', quote: '' },
          content: [{ type: 'text', text: 'seed' }]
        }
      ]
    }
  })
  editors.push(editor)
  return editor
}

function deps(editor: Editor): ClipboardDeps {
  return {
    // svelte-tiptap's Editor type adds a contentElement ref the headless
    // @tiptap/core Editor lacks; the clipboard handlers only touch view/state.
    editor: editor as unknown as ClipboardDeps['editor'],
    notify,
    menu: () => null
  }
}

function blocks(editor: Editor): JSONContent[] {
  return editor.getJSON().content ?? []
}

function textOf(node: JSONContent): string {
  if (node.text) return node.text
  return (node.content ?? []).map(textOf).join('')
}

function stripIds(node: JSONContent): JSONContent {
  const copy: JSONContent = { ...node }
  if (copy.attrs) {
    copy.attrs = { ...copy.attrs }
    delete copy.attrs.id
  }
  if (copy.content) copy.content = copy.content.map(stripIds)
  return copy
}

beforeEach(() => {
  readClipboardPayload.mockReset()
  readClipboardPayload.mockResolvedValue({ html: '', text: '' })
  notify.mockClear()
})

afterEach(() => {
  while (editors.length) {
    const editor = editors.pop()
    if (editor && !editor.isDestroyed) editor.destroy()
  }
  _setShiftHeldForTests(false)
})

describe('context-menu paste', () => {
  it('produces the same document as a native pasteHTML of the same fixture', async () => {
    const native = makeEditor()
    native.commands.selectAll()
    native.view.pasteHTML(OUTLOOK_BODY)

    const menu = makeEditor()
    menu.commands.selectAll()
    readClipboardPayload.mockResolvedValueOnce({
      html: OUTLOOK_BODY,
      text: 'Q3 Planning\nBold launch and italic body text\nShip the beta'
    })
    await pasteFromClipboard(deps(menu))

    // Sanity-check the fixture actually exercised the rich path, then assert
    // full parity (ids differ by design — UniqueBlockIds mints fresh ones).
    const pasted = blocks(menu)
    expect(pasted[0].type).toBe('headerBlock')
    expect(pasted[0].attrs?.depth).toBe(2)
    expect(
      pasted.filter((node) => node.attrs?.bullet === '- ').map(textOf)
    ).toEqual(['Ship the beta', 'Write the docs'])
    expect(stripIds(menu.getJSON())).toEqual(stripIds(native.getJSON()))
  })

  it('inserts one block per line for a text-only clipboard', async () => {
    const editor = makeEditor()
    editor.commands.selectAll()
    readClipboardPayload.mockResolvedValueOnce({
      html: '',
      text: 'first line\nsecond line\nthird line'
    })
    await pasteFromClipboard(deps(editor))

    const pasted = blocks(editor)
    expect(pasted.map((node) => node.type)).toEqual([
      'noteBlock',
      'noteBlock',
      'noteBlock'
    ])
    expect(pasted.map(textOf)).toEqual([
      'first line',
      'second line',
      'third line'
    ])
  })

  it('falls back to the text payload when the HTML is oversized', async () => {
    const editor = makeEditor()
    editor.commands.selectAll()
    readClipboardPayload.mockResolvedValueOnce({
      html: `<h1>Ignored</h1>${'x'.repeat(PASTE_HTML_SIZE_CAP + 1)}`,
      text: 'fallback text'
    })
    await pasteFromClipboard(deps(editor))

    const pasted = blocks(editor)
    expect(pasted.some((node) => node.type === 'headerBlock')).toBe(false)
    expect(pasted.some((node) => node.type === 'table')).toBe(false)
    expect(pasted.map(textOf)).toEqual(['fallback text'])
  })

  it('routes internal (data-pm-slice) HTML through pasteHTML unchanged', async () => {
    // A real internal copy: serializeForClipboard wraps the slice in the
    // data-pm-slice marker the normalizer passes through untouched.
    const source = makeEditor()
    source.commands.setContent({
      type: 'doc',
      content: [
        {
          type: 'noteBlock',
          attrs: { id: 'copied', depth: 0, bullet: '- ', quote: '' },
          content: [{ type: 'text', text: 'copied block' }]
        }
      ]
    })
    source.commands.selectAll()
    const { dom, text } = source.view.serializeForClipboard(
      source.state.selection.content()
    )

    const editor = makeEditor()
    editor.commands.selectAll()
    readClipboardPayload.mockResolvedValueOnce({ html: dom.innerHTML, text })
    await pasteFromClipboard(deps(editor))

    expect(dom.innerHTML).toContain('data-pm-slice')
    expect(editor.getJSON()).toMatchObject({
      content: [
        {
          type: 'noteBlock',
          attrs: { bullet: '- ' },
          content: [{ type: 'text', text: 'copied block' }]
        }
      ]
    })
  })

  it('takes the rich path even when a stale shift flag is set (Mod-Shift-v regression)', async () => {
    // A menu paste has no keydown, so the shift flag can only be stale here.
    // PlainPaste must decline (its handlePaste needs clipboardData) and the
    // HTML path must win, matching Ctrl+V.
    _setShiftHeldForTests(true)
    const editor = makeEditor()
    editor.commands.selectAll()
    readClipboardPayload.mockResolvedValueOnce({
      html: OUTLOOK_BODY,
      text: 'Q3 Planning'
    })
    await pasteFromClipboard(deps(editor))

    const pasted = blocks(editor)
    expect(pasted[0].type).toBe('headerBlock')
    expect(pasted.some((node) => node.attrs?.bullet === '- ')).toBe(true)
  })

  it('does nothing and does not toast for an empty clipboard', async () => {
    const editor = makeEditor()
    const before = editor.getJSON()
    readClipboardPayload.mockResolvedValueOnce({ html: '', text: '' })
    await pasteFromClipboard(deps(editor))

    expect(editor.getJSON()).toEqual(before)
    expect(notify).not.toHaveBeenCalled()
  })

  it('toasts when the paste pipeline fails outside the reader', async () => {
    const editor = makeEditor()
    vi.spyOn(editor.view, 'pasteText').mockImplementation(() => {
      throw new Error('pipeline boom')
    })
    readClipboardPayload.mockResolvedValueOnce({ html: '', text: 'text' })
    await pasteFromClipboard(deps(editor))

    expect(notify).toHaveBeenCalledWith(
      expect.objectContaining({ kind: 'error' })
    )
  })
})
