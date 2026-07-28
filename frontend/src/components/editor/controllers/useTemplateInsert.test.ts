// Unit tests for the template-insert controller (#769). Covers the
// empty/non-empty cursor-vs-append branching, the pending-dialog lifecycle,
// the editor-destroyed guard, and the showTemplatePicker getter/setter. Uses
// the $effect.root harness because the factory owns $state cells.
import { describe, it, expect, beforeEach, vi } from 'vitest'
import type { Editor } from 'svelte-tiptap'
import { createTemplateInsertHarness } from './useTemplateInsertHarness.svelte'

// blocksToDoc is the only runtime import from lib/editor; stub it so the test
// never touches the real converter (the mock editor captures what is inserted).
vi.mock('../../../lib/editor', () => ({
  blocksToDoc: vi.fn(() => ({ content: 'MOCK_DOC_CONTENT' }))
}))

interface MockEditor {
  isDestroyed: boolean
  state: {
    doc: {
      textContent: string
      content: { size: number }
      forEach: (fn: () => void) => void
    }
    selection: { $from: { pos: number }; head: number }
  }
  commands: {
    insertContent: ReturnType<typeof vi.fn>
    insertContentAt: ReturnType<typeof vi.fn>
    focus: ReturnType<typeof vi.fn>
  }
}

function makeEditor(
  opts: {
    text?: string
    blockCount?: number
    fromPos?: number
    docSize?: number
    destroyed?: boolean
  } = {}
): MockEditor {
  const blockCount = opts.blockCount ?? 1
  return {
    isDestroyed: opts.destroyed ?? false,
    state: {
      doc: {
        textContent: opts.text ?? '',
        content: { size: opts.docSize ?? 100 },
        forEach: (fn: () => void) => {
          for (let i = 0; i < blockCount; i++) fn()
        }
      },
      selection: {
        $from: { pos: opts.fromPos ?? 1 },
        head: opts.fromPos ?? 1
      }
    },
    commands: {
      insertContent: vi.fn(),
      insertContentAt: vi.fn(),
      focus: vi.fn()
    }
  }
}

describe('createTemplateInsert', () => {
  let harness: ReturnType<typeof createTemplateInsertHarness> | null = null

  beforeEach(() => {
    harness?.destroy()
  })

  function harnessWith(editor: MockEditor | null) {
    harness = createTemplateInsertHarness(() => editor as unknown as Editor)
    return harness.controller
  }

  it('inserts at cursor immediately when the editor is effectively empty', () => {
    const editor = makeEditor({ text: '', blockCount: 1 })
    const ctrl = harnessWith(editor)

    ctrl.handleTemplateInsert([])

    expect(editor.commands.insertContent).toHaveBeenCalledWith(
      'MOCK_DOC_CONTENT'
    )
    expect(editor.commands.insertContentAt).not.toHaveBeenCalled()
    // No pending dialog on the fast path.
    expect(ctrl.pendingTemplateBlocks).toBeNull()
  })

  it('inserts at cursor when the caret is at doc end even if non-empty', () => {
    const editor = makeEditor({ text: 'content', fromPos: 99, docSize: 100 })
    const ctrl = harnessWith(editor)

    ctrl.handleTemplateInsert([])

    expect(editor.commands.insertContent).toHaveBeenCalled()
    expect(ctrl.pendingTemplateBlocks).toBeNull()
  })

  it('opens the append-vs-cursor dialog when non-empty and the caret is mid-doc', () => {
    const editor = makeEditor({ text: 'content', fromPos: 5, docSize: 100 })
    const ctrl = harnessWith(editor)

    const blocks = [{ type: 'NOTE', clean_text: 'x' }] as never
    ctrl.handleTemplateInsert(blocks)

    expect(editor.commands.insertContent).not.toHaveBeenCalled()
    // $state deeply proxies the array, so compare content not identity.
    expect(ctrl.pendingTemplateBlocks).toStrictEqual(blocks)
    // Focus is captured from document.activeElement at dialog-open time.
    expect(ctrl.templateInsertReturnFocus).toBe(
      document.activeElement as HTMLElement
    )
  })

  it('confirmTemplateAtCursor inserts at cursor and clears the dialog', () => {
    const editor = makeEditor({ text: 'content', fromPos: 5, docSize: 100 })
    const ctrl = harnessWith(editor)
    const blocks = [{ type: 'NOTE', clean_text: 'x' }] as never
    ctrl.handleTemplateInsert(blocks)
    expect(ctrl.pendingTemplateBlocks).toBeTruthy()

    ctrl.confirmTemplateAtCursor()

    expect(editor.commands.insertContent).toHaveBeenCalledWith(
      'MOCK_DOC_CONTENT'
    )
    expect(ctrl.pendingTemplateBlocks).toBeNull()
  })

  it('confirmTemplateAppend inserts at doc end and clears the dialog', () => {
    const editor = makeEditor({ text: 'content', fromPos: 5, docSize: 100 })
    const ctrl = harnessWith(editor)
    ctrl.handleTemplateInsert([{ type: 'NOTE' }] as never)

    editor.commands.insertContentAt.mockClear()
    ctrl.confirmTemplateAppend()

    expect(editor.commands.insertContentAt).toHaveBeenCalledWith(
      100,
      'MOCK_DOC_CONTENT'
    )
    expect(ctrl.pendingTemplateBlocks).toBeNull()
  })

  it('cancelTemplateInsert clears the pending dialog without inserting', () => {
    const editor = makeEditor({ text: 'content', fromPos: 5, docSize: 100 })
    const ctrl = harnessWith(editor)
    ctrl.handleTemplateInsert([{ type: 'NOTE' }] as never)

    ctrl.cancelTemplateInsert()

    expect(editor.commands.insertContent).not.toHaveBeenCalled()
    expect(ctrl.pendingTemplateBlocks).toBeNull()
  })

  it('is a no-op when the editor is destroyed', () => {
    const editor = makeEditor({ text: '', destroyed: true })
    const ctrl = harnessWith(editor)

    ctrl.handleTemplateInsert([])

    expect(editor.commands.insertContent).not.toHaveBeenCalled()
    expect(ctrl.pendingTemplateBlocks).toBeNull()
  })

  it('openTemplatePicker sets showTemplatePicker and the setter writes it back', () => {
    const ctrl = harnessWith(makeEditor())

    expect(ctrl.showTemplatePicker).toBe(false)
    ctrl.openTemplatePicker()
    expect(ctrl.showTemplatePicker).toBe(true)
    ctrl.showTemplatePicker = false
    expect(ctrl.showTemplatePicker).toBe(false)
  })

  it('confirm/cancel are no-ops when nothing is pending', () => {
    const editor = makeEditor()
    const ctrl = harnessWith(editor)

    ctrl.confirmTemplateAtCursor()
    ctrl.confirmTemplateAppend()
    ctrl.cancelTemplateInsert()

    expect(editor.commands.insertContent).not.toHaveBeenCalled()
    expect(editor.commands.insertContentAt).not.toHaveBeenCalled()
  })
})
