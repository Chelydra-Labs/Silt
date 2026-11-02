import { describe, it, expect, vi } from 'vitest'
import { tick } from 'svelte'
import type { Editor } from 'svelte-tiptap'
import {
  createPopoversController,
  type PopoversController
} from './usePopovers.svelte'

// #1005: a conflict-held editor is setEditable(false) — the popover cluster's
// apply paths dispatch chain()/commands directly (which isEditable never
// gates), so each must refuse instead of rendering an edit the armed reload
// destroys. Popovers can outlive the hold arming (opened before the banner),
// so the refusal is checked at dispatch time, not open time.

function makeEditor(opts: { editable?: boolean } = {}) {
  const run = vi.fn()
  const focusChain = {
    setLink: vi.fn(() => ({ run })),
    setMark: vi.fn(() => ({ run })),
    unsetMark: vi.fn(() => ({ run })),
    run
  }
  const editor = {
    isDestroyed: false,
    isEditable: opts.editable !== false,
    state: {
      selection: {
        empty: false,
        from: 2,
        to: 5,
        $from: { parentOffset: 0, start: () => 1 }
      }
    },
    view: {
      dom: document.createElement('div'),
      coordsAtPos: () => ({ left: 10, top: 10, bottom: 20 })
    },
    isActive: vi.fn(() => false),
    getAttributes: vi.fn(() => ({})),
    chain: vi.fn(() => ({ focus: () => focusChain })),
    commands: {
      focus: vi.fn(),
      insertContent: vi.fn()
    }
  }
  return { editor, run, focusChain }
}

function harness(editor: Editor | null): PopoversController {
  let controller!: PopoversController
  const cleanup = $effect.root(() => {
    controller = createPopoversController({ getEditor: () => editor })
  })
  void cleanup
  return controller
}

describe('createPopovers conflict hold (#1005)', () => {
  it('applyLinkInput refuses setLink on a non-editable editor', () => {
    const { editor, run } = makeEditor({ editable: false })
    const ctrl = harness(editor as never)
    ctrl.linkInputValue = 'https://example.com'
    ctrl.applyLinkInput()
    expect(run).not.toHaveBeenCalled()
    expect(ctrl.showLinkInput).toBe(false)
  })

  it('confirmTableSize refuses insertTable on a non-editable editor', () => {
    const { editor, run } = makeEditor({ editable: false })
    const ctrl = harness(editor as never)
    ctrl.confirmTableSize(3, 3)
    expect(run).not.toHaveBeenCalled()
  })

  it('applyColorFromPopover refuses setMark/unsetMark on a non-editable editor', () => {
    const { editor, run } = makeEditor({ editable: false })
    const ctrl = harness(editor as never)
    ctrl.applyColorFromPopover('#ff0000')
    ctrl.applyColorFromPopover(null)
    expect(run).not.toHaveBeenCalled()
  })

  it('handleEmbedPick refuses insertContent on a non-editable editor', () => {
    const { editor } = makeEditor({ editable: false })
    const ctrl = harness(editor as never)
    ctrl.handleEmbedPick('block-uuid')
    expect(editor.commands.insertContent).not.toHaveBeenCalled()
  })

  it('still applies on an editable editor (control)', async () => {
    const { editor, run } = makeEditor()
    const ctrl = harness(editor as never)
    ctrl.linkInputValue = 'https://example.com'
    ctrl.applyLinkInput()
    await tick()
    expect(run).toHaveBeenCalled()
  })
})
