import { describe, it, expect } from 'vitest'
import { Editor } from '@tiptap/core'
import StarterKit from '@tiptap/starter-kit'
import {
  SiltBlockExtensions,
  SiltTableExtensions,
  UniqueBlockIds,
  insertTable
} from './index'

// Cell-navigation tests for GFM tables (#172/#287). TipTap's Table extension
// binds Tab/Shift-Tab to goToNextCell/goToPreviousCell (and Tab at the last
// cell appends a new row). These tests prove that navigation is functional and
// wired under Silt's schema — the keyboard binding is stock TipTap, dispatched
// here via ProseMirror's handleKeyDown prop (the keymap plugin's entry point),
// the same pattern used by keymaps.test.ts's Enter handler.

function makeTableEditor(): Editor {
  return new Editor({
    extensions: [
      StarterKit.configure({
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
      ...SiltTableExtensions,
      UniqueBlockIds
    ]
  })
}

// Dispatch a Tab/Shift-Tab keydown through ProseMirror's keymap (the Table
// extension's addKeyboardShortcuts registers under handleKeyDown).
function pressTab(editor: Editor, shift = false): void {
  const event = new KeyboardEvent('keydown', {
    key: 'Tab',
    bubbles: true,
    shiftKey: shift
  })
  editor.view.someProp('handleKeyDown', (handler) =>
    handler(editor.view, event)
  )
}

// Positions of every cell in document order.
function cellPositions(editor: Editor): number[] {
  const positions: number[] = []
  editor.state.doc.descendants((node, pos) => {
    if (node.type.name === 'tableHeader' || node.type.name === 'tableCell') {
      positions.push(pos)
      return false
    }
    return true
  })
  return positions
}

// Index of the cell currently containing the selection (or -1).
function currentCellIndex(editor: Editor, positions: number[]): number {
  const sel = editor.state.selection.from
  for (let i = 0; i < positions.length; i++) {
    const node = editor.state.doc.nodeAt(positions[i])
    if (!node) continue
    if (sel >= positions[i] && sel <= positions[i] + node.nodeSize) return i
  }
  return -1
}

function rowCount(editor: Editor): number {
  let count = 0
  editor.state.doc.descendants((node) => {
    if (node.type.name === 'tableRow') {
      count++
      return false
    }
    return true
  })
  return count
}

describe('GFM table cell navigation (#172/#287)', () => {
  it('Tab moves the cursor to the next cell', () => {
    const editor = makeTableEditor()
    expect(insertTable(editor, 2, 2)).toBe(true)
    const positions = cellPositions(editor)
    expect(positions.length).toBe(4)
    // Place the caret inside the first cell's paragraph.
    editor
      .chain()
      .setTextSelection(positions[0] + 2)
      .run()
    expect(currentCellIndex(editor, positions)).toBe(0)

    pressTab(editor)
    expect(currentCellIndex(editor, positions)).toBe(1)
    editor.destroy()
  })

  it('Shift-Tab moves the cursor to the previous cell', () => {
    const editor = makeTableEditor()
    insertTable(editor, 2, 2)
    const positions = cellPositions(editor)
    editor
      .chain()
      .setTextSelection(positions[1] + 2)
      .run()
    expect(currentCellIndex(editor, positions)).toBe(1)

    pressTab(editor, true)
    expect(currentCellIndex(editor, positions)).toBe(0)
    editor.destroy()
  })

  it('Tab at the last cell appends a new row', () => {
    const editor = makeTableEditor()
    insertTable(editor, 2, 2)
    const positions = cellPositions(editor)
    // Caret in the last cell.
    editor
      .chain()
      .setTextSelection(positions[positions.length - 1] + 2)
      .run()
    expect(rowCount(editor)).toBe(2)

    pressTab(editor)
    // TipTap's Tab handler adds a row below when there is no next cell.
    expect(rowCount(editor)).toBe(3)
    editor.destroy()
  })

  it('goToNextCell / goToPreviousCell commands move across cells', () => {
    // The command-level navigation (what Tab/Shift-Tab bind to) works regardless
    // of keymap dispatch, guarding against jsdom event-routing regressions.
    const editor = makeTableEditor()
    insertTable(editor, 2, 3)
    const positions = cellPositions(editor)
    editor
      .chain()
      .setTextSelection(positions[0] + 2)
      .run()
    expect(currentCellIndex(editor, positions)).toBe(0)

    expect(editor.commands.goToNextCell()).toBe(true)
    expect(currentCellIndex(editor, positions)).toBe(1)
    expect(editor.commands.goToPreviousCell()).toBe(true)
    expect(currentCellIndex(editor, positions)).toBe(0)
    editor.destroy()
  })
})
