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
import {
  GENUINE_TABLE,
  NESTED_ORDERED_LIST,
  OUTLOOK_BODY
} from './paste/fixtures'
import {
  copySelection,
  pasteFromClipboard,
  type ClipboardDeps
} from './clipboard'
import { insertMeetingSection } from '../../plugins/first-party/silt-meeting'

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

/** Seed a note block followed by a code block holding `code`. */
function makeCodeEditor(code = 'abc'): Editor {
  const editor = makeEditor()
  editor.commands.setContent({
    type: 'doc',
    content: [
      {
        type: 'noteBlock',
        attrs: { id: 'seed', depth: 0, bullet: '', quote: '' },
        content: [{ type: 'text', text: 'seed' }]
      },
      {
        type: 'codeBlock',
        attrs: { language: '' },
        content: [{ type: 'text', text: code }]
      }
    ]
  })
  return editor
}

/** Caret `offset` characters into the first code block. */
function caretInFirstCodeBlock(editor: Editor, offset = 1): number {
  let pos = 0
  editor.state.doc.descendants((node, nodePos) => {
    if (node.type.name === 'codeBlock') {
      pos = nodePos + 1 + offset
      return false
    }
    return true
  })
  return pos
}

beforeEach(() => {
  readClipboardPayload.mockReset()
  readClipboardPayload.mockResolvedValue({
    html: '',
    text: '',
    unavailable: false
  })
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

  it('inserts the text payload at the caret inside a code block (HTML clipboard)', async () => {
    // A copied code snippet arrives as `<pre>`/`<p>` HTML plus mirrored text.
    for (const html of ['<pre>pasted</pre>', '<p>pasted</p>']) {
      const native = makeCodeEditor()
      native.commands.setTextSelection(caretInFirstCodeBlock(native))
      native.view.pasteText('pasted')

      const menu = makeCodeEditor()
      menu.commands.setTextSelection(caretInFirstCodeBlock(menu))
      readClipboardPayload.mockResolvedValueOnce({
        html,
        text: 'pasted',
        unavailable: false
      })
      await pasteFromClipboard(deps(menu))

      // Not split into [code, note, code] — the code block stays one block and
      // the insertion matches the native pasteText baseline exactly.
      expect(blocks(menu)).toHaveLength(2)
      expect(blocks(menu)[1].type).toBe('codeBlock')
      expect(textOf(blocks(menu)[1])).toBe('apastedbc')
      expect(stripIds(menu.getJSON())).toEqual(stripIds(native.getJSON()))
    }
  })

  it('matches the native text paste inside a code block (text-only clipboard)', async () => {
    const native = makeCodeEditor()
    native.commands.setTextSelection(caretInFirstCodeBlock(native))
    native.view.pasteText('pasted')

    const menu = makeCodeEditor()
    menu.commands.setTextSelection(caretInFirstCodeBlock(menu))
    readClipboardPayload.mockResolvedValueOnce({
      html: '',
      text: 'pasted',
      unavailable: false
    })
    await pasteFromClipboard(deps(menu))

    expect(blocks(menu)).toHaveLength(2)
    expect(stripIds(menu.getJSON())).toEqual(stripIds(native.getJSON()))
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

  it('round-trips an editor selection through serialized internal HTML like native copy/paste', async () => {
    const source = makeEditor()
    source.commands.selectAll()
    source.view.pasteHTML(OUTLOOK_BODY)
    source.view.pasteHTML(GENUINE_TABLE)
    source.commands.selectAll()
    const { dom: nativeDom, text: nativeText } =
      source.view.serializeForClipboard(source.state.selection.content())

    const native = makeEditor()
    native.commands.selectAll()
    native.view.pasteHTML(nativeDom.innerHTML)

    let writtenItem: { data: Record<string, Blob> } | undefined
    class TestClipboardItem {
      static supports = () => true
      constructor(data: Record<string, Blob>) {
        this.data = data
      }
      data: Record<string, Blob>
    }
    const originalItem = globalThis.ClipboardItem
    const originalClipboard = navigator.clipboard
    Object.defineProperty(globalThis, 'ClipboardItem', {
      value: TestClipboardItem,
      configurable: true
    })
    Object.defineProperty(navigator, 'clipboard', {
      value: {
        write: vi.fn(async (items: { data: Record<string, Blob> }[]) => {
          writtenItem = items[0]
        })
      },
      configurable: true
    })
    copySelection(deps(source))
    await vi.waitFor(() => expect(writtenItem).toBeDefined())
    const copiedHtml = await writtenItem!.data['text/html'].text()
    const copiedText = await writtenItem!.data['text/plain'].text()

    const menu = makeEditor()
    menu.commands.selectAll()
    readClipboardPayload.mockResolvedValueOnce({
      html: copiedHtml,
      text: copiedText
    })
    await pasteFromClipboard(deps(menu))

    expect(copiedHtml).toContain('data-pm-slice')
    expect(copiedText).toBe(nativeText)
    expect(stripIds(menu.getJSON())).toEqual(stripIds(native.getJSON()))
    expect(menu.getJSON()).toMatchObject({
      content: expect.arrayContaining([
        expect.objectContaining({ type: 'headerBlock' }),
        expect.objectContaining({
          attrs: expect.objectContaining({ bullet: '- ' })
        })
      ])
    })
    expect(JSON.stringify(menu.getJSON())).toContain('"type":"table"')
    expect(JSON.stringify(menu.getJSON())).toContain('"type":"bold"')
    Object.defineProperty(globalThis, 'ClipboardItem', {
      value: originalItem,
      configurable: true
    })
    Object.defineProperty(navigator, 'clipboard', {
      value: originalClipboard,
      configurable: true
    })
  })

  it('preserves nested list depth through menu copy and paste', async () => {
    const source = makeEditor()
    source.commands.selectAll()
    source.view.pasteHTML(NESTED_ORDERED_LIST)
    source.commands.selectAll()
    const nativeSource = source.getJSON()

    let writtenItem: { data: Record<string, Blob> } | undefined
    class TestClipboardItem {
      static supports = () => true
      constructor(data: Record<string, Blob>) {
        this.data = data
      }
      data: Record<string, Blob>
    }
    const originalItem = globalThis.ClipboardItem
    const originalClipboard = navigator.clipboard
    Object.defineProperty(globalThis, 'ClipboardItem', {
      value: TestClipboardItem,
      configurable: true
    })
    Object.defineProperty(navigator, 'clipboard', {
      value: {
        write: vi.fn(async (items: { data: Record<string, Blob> }[]) => {
          writtenItem = items[0]
        })
      },
      configurable: true
    })
    copySelection(deps(source))
    await vi.waitFor(() => expect(writtenItem).toBeDefined())

    const menu = makeEditor()
    menu.commands.selectAll()
    readClipboardPayload.mockResolvedValueOnce({
      html: await writtenItem!.data['text/html'].text(),
      text: await writtenItem!.data['text/plain'].text()
    })
    await pasteFromClipboard(deps(menu))

    expect(stripIds(menu.getJSON())).toEqual(stripIds(nativeSource))
    const depths: number[] = []
    const visit = (node: JSONContent) => {
      if (typeof node.attrs?.depth === 'number') depths.push(node.attrs.depth)
      node.content?.forEach(visit)
    }
    visit(menu.getJSON())
    expect(depths.some((depth) => depth > 0)).toBe(true)
    Object.defineProperty(globalThis, 'ClipboardItem', {
      value: originalItem,
      configurable: true
    })
    Object.defineProperty(navigator, 'clipboard', {
      value: originalClipboard,
      configurable: true
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

  it('does nothing and does not toast for a readable-but-empty clipboard', async () => {
    const editor = makeEditor()
    const before = editor.getJSON()
    readClipboardPayload.mockResolvedValueOnce({
      html: '',
      text: '',
      unavailable: false
    })
    await pasteFromClipboard(deps(editor))

    expect(editor.getJSON()).toEqual(before)
    expect(notify).not.toHaveBeenCalled()
  })

  it('toasts when every clipboard source failed', async () => {
    const editor = makeEditor()
    const before = editor.getJSON()
    readClipboardPayload.mockResolvedValueOnce({
      html: '',
      text: '',
      unavailable: true
    })
    await pasteFromClipboard(deps(editor))

    expect(editor.getJSON()).toEqual(before)
    expect(notify).toHaveBeenCalledWith(
      expect.objectContaining({
        kind: 'error',
        message: 'Paste failed: the clipboard is unavailable.'
      })
    )
  })

  it('toasts when the paste pipeline fails outside the reader', async () => {
    const editor = makeEditor()
    vi.spyOn(editor.view, 'pasteText').mockImplementation(() => {
      throw new Error('pipeline boom')
    })
    readClipboardPayload.mockResolvedValueOnce({
      html: '',
      text: 'text',
      unavailable: false
    })
    await pasteFromClipboard(deps(editor))

    expect(notify).toHaveBeenCalledWith(
      expect.objectContaining({
        kind: 'error',
        message: 'Paste failed: the clipboard contents could not be inserted.'
      })
    )
  })
})

describe('paste inside a /meeting section (#1105)', () => {
  it('an Outlook paste at the section caret lands inside detailsContent', () => {
    const editor = makeEditor()
    // The seeded 'seed' noteBlock is non-empty, so the section prepends via
    // the insert path and the caret parks on the first field line.
    expect(insertMeetingSection(editor)).toBe(true)

    // Sanity: the caret really is inside the section before the paste.
    const $from = editor.state.selection.$from
    const chain: string[] = []
    for (let d = 1; d <= $from.depth; d++) chain.push($from.node(d).type.name)
    expect(chain).toEqual(['details', 'detailsContent', 'noteBlock'])

    // Real interop target: a captured Outlook/Word clipboard body.
    editor.view.pasteHTML(OUTLOOK_BODY)

    const top = blocks(editor)
    // Nothing was appended at doc end: the section is still first and the
    // seed block is still last, unchanged.
    expect(top.map((node) => node.type)).toEqual(['details', 'noteBlock'])
    expect(textOf(top[1])).toBe('seed')
    // The Outlook content normalized inside the section, alongside the
    // seeded field labels.
    const detailsText = textOf(top[0])
    expect(detailsText).toContain('Meeting information')
    expect(detailsText).toContain('Subject:')
    expect(detailsText).toContain('Q3 Planning')
    // 'Bold launch' and 'and italic body text' are separated by the
    // fixture's mso-spacerun NBSP, so assert them separately.
    expect(detailsText).toContain('Bold launch')
    expect(detailsText).toContain('and italic body text')
    expect(detailsText).toContain('Ship the beta')
    expect(detailsText).toContain('Write the docs')
  })
})
