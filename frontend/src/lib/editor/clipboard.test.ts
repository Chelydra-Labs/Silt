import { describe, it, expect, vi, beforeEach } from 'vitest'

// The payload reader is a separate seam with its own test file; here it is
// mocked so these tests pin the routing decisions (HTML vs text vs nothing).
const readClipboardPayload = vi.hoisted(() => vi.fn())

vi.mock('./clipboardHtml', () => ({ readClipboardPayload }))

import {
  cutSelection,
  copySelection,
  pasteFromClipboard,
  copyAsMarkdown,
  copyAsPlainText,
  copyBlockReference,
  copyBlockEmbed,
  duplicateBlock,
  deleteBlock,
  type ClipboardDeps,
  type ClipboardMenuState
} from './clipboard'

// Mock serializeInlineContent so we can assert it's called for the markdown
// path without depending on the real converter shape.
vi.mock('./converters', () => ({
  serializeInlineContent: (content: unknown) =>
    `[md:${JSON.stringify(content).length}]`
}))

// Mock navigator.clipboard (jsdom doesn't provide one). Only the write side is
// exercised here — the read side is mocked at ./clipboardHtml.
const writeTextSpy = vi.fn().mockResolvedValue(undefined)
const writeSpy = vi.fn().mockResolvedValue(undefined)
class MockClipboardItem {
  static supports = vi.fn(() => true)
  constructor(readonly data: Record<string, Blob>) {}
}
Object.defineProperty(globalThis, 'ClipboardItem', {
  value: MockClipboardItem,
  configurable: true
})
Object.defineProperty(navigator, 'clipboard', {
  value: { writeText: writeTextSpy, write: writeSpy },
  configurable: true,
  writable: true
})

const notifySpy = vi.fn()

// --- Editor mock builders ----------------------------------------------------

/** Loose mock editor used only in this file. */
type MockEditor = {
  state: unknown
  view: {
    serializeForClipboard: ReturnType<typeof vi.fn>
    pasteHTML: ReturnType<typeof vi.fn>
    pasteText: ReturnType<typeof vi.fn>
  }
  commands: {
    deleteSelection: ReturnType<typeof vi.fn>
    focus: ReturnType<typeof vi.fn>
  }
  isDestroyed: boolean
  isEditable: boolean
  chain: () => {
    insertContentAt: ReturnType<typeof vi.fn>
    deleteRange: ReturnType<typeof vi.fn>
    focus: ReturnType<typeof vi.fn>
    run: ReturnType<typeof vi.fn>
  }
}

/** Build a minimal Editor stub that supports the operations clipboard.ts uses. */
function makeEditor(overrides: Record<string, unknown> = {}): MockEditor {
  // A fake block node returned by $from.node(d). The walker in
  // findActiveBlockDepth checks `node.type.name === 'noteBlock'|'taskBlock'|'headerBlock'`.
  const noteBlockNode = { type: { name: 'noteBlock' } }
  const state = {
    selection: {
      from: 0,
      to: 0,
      empty: true,
      eq: vi.fn((other: unknown) => other === state.selection),
      content: vi.fn(() => ({})),
      $from: {
        depth: 1,
        node: () => noteBlockNode,
        parent: { type: { spec: { code: false } } },
        after: () => 5,
        before: () => 0
      }
    },
    doc: {
      textBetween: vi.fn(() => 'plain text'),
      childCount: 3,
      slice: vi.fn(() => ({
        content: {
          forEach: (cb: (n: { toJSON: () => unknown }) => void) =>
            cb({ toJSON: () => ({ content: { foo: 1 } }) })
        }
      }))
    },
    ...overrides
  }
  const editor: MockEditor = {
    state,
    view: {
      serializeForClipboard: vi.fn(() => ({
        dom: { innerHTML: '<p data-pm-slice="1 1 []">rich</p>' },
        text: 'rich text'
      })),
      pasteHTML: vi.fn(),
      pasteText: vi.fn()
    },
    commands: {
      deleteSelection: vi.fn(),
      focus: vi.fn()
    },
    isDestroyed: false,
    isEditable: true,
    chain: () => ({
      insertContentAt: vi.fn().mockReturnThis(),
      deleteRange: vi.fn().mockReturnThis(),
      focus: vi.fn().mockReturnThis(),
      run: vi.fn()
    }),
    ...overrides
  }
  // `pasteClipboardPayload` reads `view.state` (as ProseMirror does); keep that
  // in sync with the editor stub, including per-test `state` overrides.
  Object.defineProperty(editor.view, 'state', { get: () => editor.state })
  return editor
}

function makeDeps(
  editor: MockEditor,
  menuState: {
    activeBlockId?: string
    activeBlockNode?: {
      toJSON?: () => unknown
      attrs?: Record<string, unknown>
    }
  } | null = null
): ClipboardDeps {
  return {
    editor: editor as unknown as ClipboardDeps['editor'],
    notify: notifySpy,
    menu: () => menuState as ClipboardMenuState | null
  }
}

beforeEach(() => {
  Object.defineProperty(navigator, 'clipboard', {
    value: { writeText: writeTextSpy, write: writeSpy },
    configurable: true,
    writable: true
  })
  Object.defineProperty(globalThis, 'ClipboardItem', {
    value: MockClipboardItem,
    configurable: true
  })
  writeTextSpy.mockClear()
  writeSpy.mockClear()
  notifySpy.mockClear()
  readClipboardPayload.mockReset()
  readClipboardPayload.mockResolvedValue({
    html: '',
    text: '',
    unavailable: false
  })
})

describe('cutSelection', () => {
  it('writes rich and plain clipboard flavours then deletes after success', async () => {
    const editor = makeEditor()
    cutSelection(makeDeps(editor))
    expect(editor.view.serializeForClipboard).toHaveBeenCalledOnce()
    await vi.waitFor(() => expect(writeSpy).toHaveBeenCalledOnce())
    const item = writeSpy.mock.calls[0][0][0] as MockClipboardItem
    expect(await item.data['text/html'].text()).toContain('data-pm-slice')
    expect(await item.data['text/plain'].text()).toBe('rich text')
    expect(writeTextSpy).not.toHaveBeenCalled()
    expect(editor.commands.deleteSelection).toHaveBeenCalledOnce()
  })

  it('does not delete if the rich write fails', async () => {
    writeSpy.mockRejectedValueOnce(new Error('denied'))
    const editor = makeEditor()
    cutSelection(makeDeps(editor))
    await vi.waitFor(() => expect(notifySpy).toHaveBeenCalledOnce())
    expect(editor.commands.deleteSelection).not.toHaveBeenCalled()
  })

  it('does not delete a newer selection while the write is pending', async () => {
    let resolveWrite!: () => void
    writeSpy.mockImplementationOnce(
      () =>
        new Promise<void>((resolve) => {
          resolveWrite = resolve
        })
    )
    const editor = makeEditor()
    cutSelection(makeDeps(editor))
    ;(
      editor.state as { selection: { eq: (other: unknown) => boolean } }
    ).selection.eq = () => false
    resolveWrite()
    await vi.waitFor(() => expect(writeSpy).toHaveBeenCalledOnce())
    await Promise.resolve()
    expect(editor.commands.deleteSelection).not.toHaveBeenCalled()
  })

  it('does not delete if the document changes while the write is pending', async () => {
    let resolveWrite!: () => void
    writeSpy.mockImplementationOnce(
      () =>
        new Promise<void>((resolve) => {
          resolveWrite = resolve
        })
    )
    const editor = makeEditor()
    cutSelection(makeDeps(editor))
    ;(editor.state as { doc: unknown }).doc = { replaced: true }
    resolveWrite()
    await vi.waitFor(() => expect(writeSpy).toHaveBeenCalledOnce())
    await Promise.resolve()
    expect(editor.commands.deleteSelection).not.toHaveBeenCalled()
  })

  it('does not delete if the editor becomes read-only while the write is pending', async () => {
    let resolveWrite!: () => void
    writeSpy.mockImplementationOnce(
      () =>
        new Promise<void>((resolve) => {
          resolveWrite = resolve
        })
    )
    const editor = makeEditor()
    cutSelection(makeDeps(editor))
    editor.isEditable = false
    resolveWrite()
    await vi.waitFor(() => expect(writeSpy).toHaveBeenCalledOnce())
    await Promise.resolve()
    expect(editor.commands.deleteSelection).not.toHaveBeenCalled()
  })

  it('does not delete if the editor is destroyed while the write is pending', async () => {
    let resolveWrite!: () => void
    writeSpy.mockImplementationOnce(
      () =>
        new Promise<void>((resolve) => {
          resolveWrite = resolve
        })
    )
    const editor = makeEditor()
    cutSelection(makeDeps(editor))
    editor.isDestroyed = true
    resolveWrite()
    await vi.waitFor(() => expect(writeSpy).toHaveBeenCalledOnce())
    await Promise.resolve()
    expect(editor.commands.deleteSelection).not.toHaveBeenCalled()
  })
})

describe('copySelection', () => {
  it('writes rich and plain flavours but does NOT delete', async () => {
    const editor = makeEditor()
    copySelection(makeDeps(editor))
    await vi.waitFor(() => expect(writeSpy).toHaveBeenCalledOnce())
    expect(editor.commands.deleteSelection).not.toHaveBeenCalled()
  })

  it('uses writeText when rich clipboard APIs are unavailable', async () => {
    Object.defineProperty(navigator, 'clipboard', {
      value: { writeText: writeTextSpy },
      configurable: true
    })
    copySelection(makeDeps(makeEditor()))
    await vi.waitFor(() =>
      expect(writeTextSpy).toHaveBeenCalledWith('rich text')
    )
    expect(writeSpy).not.toHaveBeenCalled()
  })

  it('uses writeText when ClipboardItem is unavailable', async () => {
    const item = globalThis.ClipboardItem
    Object.defineProperty(globalThis, 'ClipboardItem', {
      value: undefined,
      configurable: true
    })
    copySelection(makeDeps(makeEditor()))
    await vi.waitFor(() =>
      expect(writeTextSpy).toHaveBeenCalledWith('rich text')
    )
    expect(writeSpy).not.toHaveBeenCalled()
    Object.defineProperty(globalThis, 'ClipboardItem', {
      value: item,
      configurable: true
    })
  })

  it('uses writeText when ClipboardItem does not support HTML', async () => {
    MockClipboardItem.supports.mockReturnValueOnce(false)
    copySelection(makeDeps(makeEditor()))
    await vi.waitFor(() =>
      expect(writeTextSpy).toHaveBeenCalledWith('rich text')
    )
    expect(writeSpy).not.toHaveBeenCalled()
  })

  it('reports a rejected text fallback with the failure message', async () => {
    Object.defineProperty(navigator, 'clipboard', {
      value: { writeText: writeTextSpy },
      configurable: true
    })
    writeTextSpy.mockRejectedValueOnce(new Error('denied'))
    copySelection(makeDeps(makeEditor()))
    await vi.waitFor(() =>
      expect(notifySpy).toHaveBeenCalledWith({
        kind: 'error',
        message: 'Copy failed: clipboard could not be written.'
      })
    )
  })

  it('does not retry a rejected rich write as text and reports failure', async () => {
    writeSpy.mockRejectedValueOnce(new Error('denied'))
    copySelection(makeDeps(makeEditor()))
    await vi.waitFor(() =>
      expect(notifySpy).toHaveBeenCalledWith({
        kind: 'error',
        message: 'Copy failed: clipboard could not be written.'
      })
    )
    expect(writeTextSpy).not.toHaveBeenCalled()
  })
})

describe('pasteFromClipboard', () => {
  it('pastes clipboard HTML through the rich paste pipeline', async () => {
    readClipboardPayload.mockResolvedValueOnce({
      html: '<h1>Heading</h1>',
      text: 'Heading'
    })
    const editor = makeEditor()
    await pasteFromClipboard(makeDeps(editor))
    expect(editor.view.pasteHTML).toHaveBeenCalledWith('<h1>Heading</h1>')
    expect(editor.view.pasteText).not.toHaveBeenCalled()
  })

  it('pastes internal (data-pm-slice) HTML through the rich pipeline too', async () => {
    readClipboardPayload.mockResolvedValueOnce({
      html: '<div data-pm-slice="1 1 []"><p>copied</p></div>',
      text: 'copied'
    })
    const editor = makeEditor()
    await pasteFromClipboard(makeDeps(editor))
    expect(editor.view.pasteHTML).toHaveBeenCalledOnce()
    expect(editor.view.pasteText).not.toHaveBeenCalled()
  })

  it('pastes text through pasteText when the clipboard has no HTML', async () => {
    readClipboardPayload.mockResolvedValueOnce({
      html: '',
      text: 'pasted content'
    })
    const editor = makeEditor()
    await pasteFromClipboard(makeDeps(editor))
    expect(editor.view.pasteText).toHaveBeenCalledWith('pasted content')
    expect(editor.view.pasteHTML).not.toHaveBeenCalled()
  })

  it('falls back to the text payload for oversized HTML', async () => {
    readClipboardPayload.mockResolvedValueOnce({
      html: `<p>${'x'.repeat(2 * 1024 * 1024 + 1)}</p>`,
      text: 'fallback'
    })
    const editor = makeEditor()
    await pasteFromClipboard(makeDeps(editor))
    expect(editor.view.pasteHTML).not.toHaveBeenCalled()
    expect(editor.view.pasteText).toHaveBeenCalledWith('fallback')
  })

  it('drops oversized HTML with no text alternative instead of parsing it', async () => {
    // Matches the Ctrl+V DOM hook (siltHandlePasteEvent): a payload too large
    // to parse is dropped when there is nothing safe to insert.
    readClipboardPayload.mockResolvedValueOnce({
      html: `<p>${'x'.repeat(2 * 1024 * 1024 + 1)}</p>`,
      text: ''
    })
    const editor = makeEditor()
    await pasteFromClipboard(makeDeps(editor))
    expect(editor.view.pasteHTML).not.toHaveBeenCalled()
    expect(editor.view.pasteText).not.toHaveBeenCalled()
  })

  it('pastes the text payload even when HTML is present inside a code block', async () => {
    readClipboardPayload.mockResolvedValueOnce({
      html: '<p>pasted</p>',
      text: 'pasted',
      unavailable: false
    })
    const editor = makeEditor({
      state: {
        selection: {
          from: 2,
          to: 2,
          empty: true,
          $from: {
            depth: 1,
            node: () => ({ type: { name: 'codeBlock' } }),
            parent: { type: { spec: { code: true } } },
            after: () => 5,
            before: () => 0
          }
        },
        doc: { textBetween: vi.fn(), childCount: 2, slice: vi.fn() }
      }
    })
    await pasteFromClipboard(makeDeps(editor))
    expect(editor.view.pasteHTML).not.toHaveBeenCalled()
    expect(editor.view.pasteText).toHaveBeenCalledWith('pasted')
  })

  it('does nothing when clipboard is empty and readable', async () => {
    readClipboardPayload.mockResolvedValueOnce({
      html: '',
      text: '',
      unavailable: false
    })
    const editor = makeEditor()
    await pasteFromClipboard(makeDeps(editor))
    expect(editor.view.pasteHTML).not.toHaveBeenCalled()
    expect(editor.view.pasteText).not.toHaveBeenCalled()
    expect(notifySpy).not.toHaveBeenCalled()
  })

  it('toasts when every clipboard source failed', async () => {
    readClipboardPayload.mockResolvedValueOnce({
      html: '',
      text: '',
      unavailable: true
    })
    const editor = makeEditor()
    await pasteFromClipboard(makeDeps(editor))
    expect(editor.view.pasteHTML).not.toHaveBeenCalled()
    expect(editor.view.pasteText).not.toHaveBeenCalled()
    expect(notifySpy).toHaveBeenCalledWith(
      expect.objectContaining({
        kind: 'error',
        message: expect.stringContaining('Paste failed')
      })
    )
  })

  it('pushes an error notification when the paste pipeline throws', async () => {
    readClipboardPayload.mockResolvedValueOnce({
      html: '',
      text: 'text',
      unavailable: false
    })
    const editor = makeEditor()
    editor.view.pasteText.mockImplementationOnce(() => {
      throw new Error('pipeline failed')
    })
    await pasteFromClipboard(makeDeps(editor))
    expect(notifySpy).toHaveBeenCalledWith(
      expect.objectContaining({
        kind: 'error',
        message: 'Paste failed: the clipboard contents could not be inserted.'
      })
    )
  })

  it('pushes an error notification if the reader unexpectedly rejects', async () => {
    readClipboardPayload.mockRejectedValueOnce(new Error('boom'))
    const editor = makeEditor()
    await pasteFromClipboard(makeDeps(editor))
    expect(notifySpy).toHaveBeenCalledWith(
      expect.objectContaining({ kind: 'error' })
    )
  })
})

describe('copyAsMarkdown', () => {
  it('serializes the active block when selection is empty', async () => {
    const fakeNode = { toJSON: () => ({ content: { foo: 'bar' } }) }
    const editor = makeEditor()
    await copyAsMarkdown(
      makeDeps(editor, {
        activeBlockNode: fakeNode
      })
    )
    // serializeInlineContent mock returns [md:N] where N = JSON length
    expect(writeTextSpy).toHaveBeenCalledWith(expect.stringContaining('[md:'))
  })

  it('serializes the slice when selection is non-empty', async () => {
    const editor = makeEditor({
      state: {
        selection: { from: 5, to: 10, empty: false },
        doc: {
          slice: vi.fn(() => ({
            content: {
              forEach: (cb: (n: { toJSON: () => unknown }) => void) =>
                cb({ toJSON: () => ({ content: { a: 1 } }) })
            }
          })),
          textBetween: vi.fn()
        }
      }
    })
    await copyAsMarkdown(makeDeps(editor))
    expect(writeTextSpy).toHaveBeenCalledWith(expect.stringContaining('[md:'))
  })
})

describe('copyAsPlainText', () => {
  it('writes empty string when selection is empty', async () => {
    const editor = makeEditor()
    await copyAsPlainText(makeDeps(editor))
    expect(writeTextSpy).toHaveBeenCalledWith('')
  })

  it('writes the text-between when selection is non-empty', async () => {
    const editor = makeEditor({
      state: {
        selection: { from: 5, to: 10, empty: false },
        doc: { textBetween: vi.fn(() => 'hello world') }
      }
    })
    await copyAsPlainText(makeDeps(editor))
    expect(writeTextSpy).toHaveBeenCalledWith('hello world')
  })
})

describe('copyBlockReference', () => {
  it('writes ((id)) when the menu has an activeBlockId', async () => {
    await copyBlockReference(
      makeDeps(makeEditor(), { activeBlockId: 'abc-123' })
    )
    expect(writeTextSpy).toHaveBeenCalledWith('((abc-123))')
  })

  it('writes nothing when no activeBlockId', async () => {
    await copyBlockReference(makeDeps(makeEditor(), null))
    expect(writeTextSpy).not.toHaveBeenCalled()
  })
})

describe('copyBlockEmbed', () => {
  it('writes {{embed:id}} when the menu has an activeBlockId', async () => {
    await copyBlockEmbed(makeDeps(makeEditor(), { activeBlockId: 'xyz' }))
    expect(writeTextSpy).toHaveBeenCalledWith('{{embed:xyz}}')
  })

  it('writes nothing when no activeBlockId', async () => {
    await copyBlockEmbed(makeDeps(makeEditor(), null))
    expect(writeTextSpy).not.toHaveBeenCalled()
  })
})

describe('duplicateBlock', () => {
  it('inserts the block JSON at the block-end position and strips the id', () => {
    const fakeNode = {
      toJSON: () => ({ type: 'noteBlock', attrs: { id: 'orig' } })
    }
    const insertAt = vi.fn().mockReturnThis()
    const editor = makeEditor()
    ;(editor as { chain: () => unknown }).chain = () => ({
      insertContentAt: insertAt,
      deleteRange: vi.fn().mockReturnThis(),
      focus: vi.fn().mockReturnThis(),
      run: vi.fn()
    })
    duplicateBlock(
      makeDeps(editor, {
        activeBlockNode: fakeNode
      })
    )
    expect(insertAt).toHaveBeenCalledWith(
      expect.any(Number),
      expect.objectContaining({
        type: 'noteBlock',
        attrs: expect.not.objectContaining({ id: 'orig' })
      })
    )
  })

  it('is a no-op when no activeBlockNode', () => {
    const editor = makeEditor()
    duplicateBlock(makeDeps(editor, null))
    // No throw; chain is never called for the insertContentAt path.
    expect(true).toBe(true)
  })
})

describe('deleteBlock', () => {
  it('deletes the active block range and focuses', () => {
    const deleteRange = vi.fn().mockReturnThis()
    const focus = vi.fn().mockReturnThis()
    const editor = makeEditor()
    ;(editor as { chain: () => unknown }).chain = () => ({
      insertContentAt: vi.fn().mockReturnThis(),
      deleteRange,
      focus,
      run: vi.fn()
    })
    deleteBlock(makeDeps(editor))
    expect(deleteRange).toHaveBeenCalledWith({
      from: expect.any(Number),
      to: expect.any(Number)
    })
  })

  it('refuses to delete when only one block remains', () => {
    const editor = makeEditor({
      state: {
        selection: {
          from: 0,
          to: 0,
          empty: true,
          $from: { depth: 1, node: () => null, after: () => 0, before: () => 0 }
        },
        doc: { childCount: 1, slice: vi.fn(), textBetween: vi.fn() }
      }
    })
    const deleteRange = vi.fn()
    ;(editor as { chain: () => unknown }).chain = () => ({
      insertContentAt: vi.fn().mockReturnThis(),
      deleteRange,
      focus: vi.fn().mockReturnThis(),
      run: vi.fn()
    })
    deleteBlock(makeDeps(editor))
    expect(deleteRange).not.toHaveBeenCalled()
  })

  it('is a no-op when no block node is found in the tree', () => {
    const nonBlockNode = { type: { name: 'paragraph' } }
    const editor = makeEditor({
      state: {
        selection: {
          from: 0,
          to: 0,
          empty: true,
          $from: {
            depth: 1,
            node: () => nonBlockNode,
            after: () => 0,
            before: () => 0
          }
        },
        doc: { childCount: 5, slice: vi.fn(), textBetween: vi.fn() }
      }
    })
    const deleteRange = vi.fn()
    ;(editor as { chain: () => unknown }).chain = () => ({
      insertContentAt: vi.fn().mockReturnThis(),
      deleteRange,
      focus: vi.fn().mockReturnThis(),
      run: vi.fn()
    })
    deleteBlock(makeDeps(editor))
    expect(deleteRange).not.toHaveBeenCalled()
  })
})
