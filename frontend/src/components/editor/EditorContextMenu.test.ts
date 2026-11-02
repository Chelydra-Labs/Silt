import { describe, expect, it, vi } from 'vitest'
import { render, fireEvent, screen } from '@testing-library/svelte'
import EditorContextMenu from './EditorContextMenu.svelte'

// Minimal editor stub covering the mutating handlers this menu owns; the
// clipboard helpers are spied at the module boundary.
const clipboardMocks = vi.hoisted(() => ({
  cutSelection: vi.fn(),
  pasteFromClipboard: vi.fn(),
  duplicateBlock: vi.fn(),
  deleteBlock: vi.fn()
}))

vi.mock('../../lib/editor/clipboard', () => ({
  cutSelection: clipboardMocks.cutSelection,
  pasteFromClipboard: clipboardMocks.pasteFromClipboard,
  duplicateBlock: clipboardMocks.duplicateBlock,
  deleteBlock: clipboardMocks.deleteBlock
}))

function makeEditor(opts: { editable?: boolean } = {}) {
  return {
    isDestroyed: false,
    isEditable: opts.editable !== false,
    storage: {},
    commands: { focus: vi.fn() },
    chain: () => ({
      focus: () => ({ unsetAllMarks: () => ({ run: vi.fn() }) })
    })
  }
}

const baseProps = {
  menu: { x: 10, y: 10, activeBlockId: 'blk-1' },
  selectionEmpty: false,
  isLastBlock: false,
  onClose: vi.fn()
}

// #1005: a conflict-held editor is setEditable(false) — Cut/Paste/Duplicate/
// Delete/Clear dispatch commands that isEditable never gates; the visible
// edit would render and the armed reload would destroy it.
describe('EditorContextMenu conflict hold (#1005)', () => {
  it('disables and refuses every mutating action on a non-editable editor', async () => {
    const editor = makeEditor({ editable: false })
    render(EditorContextMenu, {
      props: { ...baseProps, editor: editor as never }
    })
    for (const name of [
      /Cut/,
      /Paste/,
      /Duplicate Block/,
      /Delete Block/,
      /Clear Formatting/
    ]) {
      const item = screen.getByRole('menuitem', {
        name: name as RegExp
      }) as HTMLButtonElement
      expect(item.disabled, String(name)).toBe(true)
    }
    // Defense-in-depth: forced clicks cannot reach the mutation helpers.
    await fireEvent.click(
      screen.getByRole('menuitem', { name: /Duplicate Block/ })
    )
    await fireEvent.click(
      screen.getByRole('menuitem', { name: /Delete Block/ })
    )
    await fireEvent.click(screen.getByRole('menuitem', { name: /Cut/ }))
    await fireEvent.click(screen.getByRole('menuitem', { name: /Paste/ }))
    expect(clipboardMocks.duplicateBlock).not.toHaveBeenCalled()
    expect(clipboardMocks.deleteBlock).not.toHaveBeenCalled()
    expect(clipboardMocks.cutSelection).not.toHaveBeenCalled()
    expect(clipboardMocks.pasteFromClipboard).not.toHaveBeenCalled()
  })

  it('keeps read-only clipboard actions available', () => {
    const editor = makeEditor({ editable: false })
    render(EditorContextMenu, {
      props: { ...baseProps, editor: editor as never }
    })
    for (const name of [
      /Copy Ctrl\+C$/,
      /Copy as Markdown$/,
      /Copy Block Reference$/
    ]) {
      expect(
        (screen.getByRole('menuitem', { name }) as HTMLButtonElement).disabled
      ).toBe(false)
    }
  })
})
