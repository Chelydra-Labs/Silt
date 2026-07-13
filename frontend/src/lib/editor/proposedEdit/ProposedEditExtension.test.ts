import { describe, it, expect, afterEach } from 'vitest'
import { Editor } from '@tiptap/core'
import StarterKit from '@tiptap/starter-kit'
import {
  ProposedEdit,
  hasProposedEdit,
  getProposedEditRange
} from './ProposedEditExtension'
import { SiltBlockExtensions } from '../index'

function makeEditor(content = '<p>Hello world</p>'): Editor {
  const el = document.createElement('div')
  document.body.appendChild(el)
  return new Editor({
    element: el,
    extensions: [StarterKit, ProposedEdit],
    content
  })
}

/** Editor with Silt noteBlock schema (needed for multi-block accept tests). */
function makeSiltEditor(content: string): Editor {
  const el = document.createElement('div')
  document.body.appendChild(el)
  return new Editor({
    element: el,
    extensions: [
      StarterKit.configure({
        heading: false,
        bulletList: false,
        orderedList: false,
        listItem: false,
        blockquote: false,
        codeBlock: false,
        horizontalRule: false
      }),
      ...SiltBlockExtensions,
      ProposedEdit
    ],
    content
  })
}

let editors: Editor[] = []
function track(ed: Editor): Editor {
  editors.push(ed)
  return ed
}
afterEach(() => {
  for (const e of editors) e.destroy()
  editors = []
})

/** Extract noteBlock nodes from editor JSON, ignoring trailing paragraphs
 *  that StarterKit's paragraph type may add during normalization. */
function noteBlocks(ed: Editor): any[] {
  return (ed.getJSON().content as any[]).filter((b) => b.type === 'noteBlock')
}

describe('ProposedEdit extension (#543)', () => {
  it('shows a preview without mutating the document', () => {
    const ed = track(makeEditor())
    const before = ed.getJSON()
    ed.commands.setProposedEdit({ from: 1, to: 6, markdown: 'Hi' })
    expect(hasProposedEdit(ed)).toBe(true)
    expect(getProposedEditRange(ed)).toEqual({ from: 1, to: 6 })
    // Document is unchanged (preview is view-only).
    expect(ed.getJSON()).toEqual(before)
    // The strike decoration renders in the DOM.
    expect(ed.view.dom.querySelector('.silt-proposed-delete')).toBeTruthy()
    expect(ed.view.dom.querySelector('.silt-proposed-edit')).toBeTruthy()
  })

  it('reject clears the preview and leaves the document untouched', () => {
    const ed = track(makeEditor())
    const before = ed.getJSON()
    ed.commands.setProposedEdit({ from: 1, to: 6, markdown: 'Hi' })
    expect(hasProposedEdit(ed)).toBe(true)
    ed.commands.rejectProposedEdit()
    expect(hasProposedEdit(ed)).toBe(false)
    expect(getProposedEditRange(ed)).toBeNull()
    expect(ed.getJSON()).toEqual(before)
    expect(ed.view.dom.querySelector('.silt-proposed-edit')).toBeNull()
  })

  it('accept replaces the inline range with the proposed markdown', () => {
    const ed = track(makeEditor())
    ed.commands.setProposedEdit({ from: 1, to: 6, markdown: 'Hi' })
    ed.commands.acceptProposedEdit()
    expect(hasProposedEdit(ed)).toBe(false)
    expect(ed.getText()).toBe('Hi world')
  })

  it('accept preserves inline markdown marks (bold)', () => {
    const ed = track(makeEditor('<p>plain text here</p>'))
    // Replace "plain" (pos 1-6) with bold "**bold**".
    ed.commands.setProposedEdit({ from: 1, to: 6, markdown: '**bold**' })
    ed.commands.acceptProposedEdit()
    const json = ed.getJSON()
    // The first paragraph's first child is a bold-marked text node.
    const para = (json.content as any[])[0]
    const firstChild = para.content[0]
    expect(firstChild.type).toBe('text')
    expect(firstChild.text).toBe('bold')
    expect(firstChild.marks?.[0].type).toBe('bold')
  })

  it('accept replaces a multi-block selection in one transaction', () => {
    const ed = track(makeEditor('<p>First</p><p>Second</p>'))
    // Selection spans both paragraphs (pos 1 .. end of second paragraph).
    ed.commands.setProposedEdit({ from: 1, to: 14, markdown: 'Replaced' })
    ed.commands.acceptProposedEdit()
    expect(hasProposedEdit(ed)).toBe(false)
    expect(ed.getText()).toBe('Replaced')
  })

  it('maps the preview range through an edit before the range', () => {
    const ed = track(makeEditor('<p>Hello world</p>'))
    ed.commands.setProposedEdit({ from: 7, to: 12, markdown: 'there' })
    // Insert text at the start, shifting the preview range right.
    ed.chain().focus().insertContentAt(1, 'XX ').run()
    const range = getProposedEditRange(ed)
    expect(range).toBeTruthy()
    // Range shifted by the 3 inserted chars; preview still tracks "world".
    expect(ed.view.dom.querySelector('.silt-proposed-delete')).toBeTruthy()
    if (range) expect(range.to).toBeGreaterThan(range.from)
  })

  it('refuses a collapsed range and returns false', () => {
    const ed = track(makeEditor())
    const ok = ed.commands.setProposedEdit({ from: 5, to: 5, markdown: 'x' })
    expect(ok).toBe(false)
    expect(hasProposedEdit(ed)).toBe(false)
  })

  it('refuses empty markdown on set and accept (no partial wipe)', () => {
    const ed = track(makeEditor())
    const original = ed.getText()
    expect(
      ed.commands.setProposedEdit({ from: 1, to: 6, markdown: '   ' })
    ).toBe(false)
    expect(hasProposedEdit(ed)).toBe(false)
    // Force a non-empty preview then clear markdown via re-set is blocked;
    // accept with empty would no-op if state were corrupted.
    ed.commands.setProposedEdit({ from: 1, to: 6, markdown: 'Hi' })
    expect(hasProposedEdit(ed)).toBe(true)
    // Reject path still works; empty set never lands.
    ed.commands.rejectProposedEdit()
    expect(ed.getText()).toBe(original)
  })

  it('auto-dismisses when the underlying range is deleted', () => {
    const ed = track(makeEditor('<p>Hello world</p>'))
    ed.commands.setProposedEdit({ from: 7, to: 12, markdown: 'there' })
    expect(hasProposedEdit(ed)).toBe(true)
    // Delete the "world" range the proposal covers.
    ed.chain().focus().deleteRange({ from: 7, to: 12 }).run()
    expect(hasProposedEdit(ed)).toBe(false)
    expect(getProposedEditRange(ed)).toBeNull()
  })

  it('accept is a single undo step; reject adds no history', () => {
    const ed = track(makeEditor())
    const original = ed.getJSON()
    ed.commands.setProposedEdit({ from: 1, to: 6, markdown: 'Hi' })
    ed.commands.acceptProposedEdit()
    expect(ed.getText()).toBe('Hi world')
    // One undo reverts the accept.
    ed.commands.undo()
    expect(ed.getJSON()).toEqual(original)

    // Reject path: setting + rejecting adds no doc history.
    ed.commands.setProposedEdit({ from: 1, to: 6, markdown: 'Yo' })
    ed.commands.rejectProposedEdit()
    // Undo is still the accept (reject added no history entry).
    ed.commands.undo()
    expect(ed.getJSON()).toEqual(original)
  })

  it('fires onAccept after a successful accept', () => {
    const ed = track(makeEditor())
    let called = false
    ed.commands.setProposedEdit({
      from: 1,
      to: 6,
      markdown: 'Hi',
      onAccept: () => {
        called = true
      }
    })
    ed.commands.acceptProposedEdit()
    expect(called).toBe(true)
  })
})

describe('ProposedEdit multi-block replace (#548)', () => {
  // noteBlock HTML: <div data-type="note">text</div>
  // Two noteBlocks "First" (5 chars) + "Second" (6 chars):
  //   pos 0: before block 1
  //   pos 1: start of block 1 content
  //   pos 6: end of block 1 content ("First")
  //   pos 7: between blocks
  //   pos 8: start of block 2 content
  //   pos 14: end of block 2 content ("Second")
  //   pos 15: end of doc

  it('multi-paragraph accept on block-spanning selection creates separate noteBlocks', () => {
    const ed = track(
      makeSiltEditor(
        '<div data-type="note">First</div><div data-type="note">Second</div>'
      )
    )
    ed.commands.setProposedEdit({
      from: 1,
      to: 14,
      markdown: 'Para one\n\nPara two'
    })
    ed.commands.acceptProposedEdit()
    expect(hasProposedEdit(ed)).toBe(false)
    const blocks = noteBlocks(ed)
    // Two noteBlocks replace the original two — multi-paragraph content
    // preserves structure instead of flattening to one line.
    expect(blocks.length).toBe(2)
    expect(blocks[0].content[0].text).toBe('Para one')
    expect(blocks[1].content[0].text).toBe('Para two')
  })

  it('multi-paragraph accept preserves inline marks across paragraphs', () => {
    const ed = track(
      makeSiltEditor(
        '<div data-type="note">First</div><div data-type="note">Second</div>'
      )
    )
    ed.commands.setProposedEdit({
      from: 1,
      to: 14,
      markdown: '**bold**\n\n*italic*'
    })
    ed.commands.acceptProposedEdit()
    const blocks = noteBlocks(ed)
    expect(blocks[0].content[0].text).toBe('bold')
    expect(blocks[0].content[0].marks?.[0].type).toBe('bold')
    expect(blocks[1].content[0].text).toBe('italic')
    expect(blocks[1].content[0].marks?.[0].type).toBe('italic')
  })

  it('single-paragraph on block-spanning selection uses inline path', () => {
    const ed = track(
      makeSiltEditor(
        '<div data-type="note">First</div><div data-type="note">Second</div>'
      )
    )
    ed.commands.setProposedEdit({ from: 1, to: 14, markdown: 'Replaced' })
    ed.commands.acceptProposedEdit()
    expect(hasProposedEdit(ed)).toBe(false)
    // Single-paragraph markdown on a block-spanning selection uses the inline
    // path (flattened), producing one block with the text.
    const text = ed.getText().replace(/\n/g, '')
    expect(text).toBe('Replaced')
  })

  it('multi-paragraph on inline selection uses inline path (flatten)', () => {
    const ed = track(makeSiltEditor('<div data-type="note">Hello world</div>'))
    // from=1, to=6 is within the first (and only) noteBlock — inline selection.
    ed.commands.setProposedEdit({
      from: 1,
      to: 6,
      markdown: 'Line one\n\nLine two'
    })
    ed.commands.acceptProposedEdit()
    const blocks = noteBlocks(ed)
    // Still one noteBlock — multi-paragraph on inline selection is flattened.
    expect(blocks.length).toBe(1)
    // The double-newline was collapsed by the inline path (single \n→space).
    expect(blocks[0].content[0].text).toContain('Line one')
    expect(blocks[0].content[0].text).toContain('Line two')
  })

  it('schema-incompatible fallback: setProposedEdit returns false without noteBlock', () => {
    // StarterKit-only editor has no noteBlock node type. A multi-paragraph
    // proposal on a block-spanning selection can't use the multi-block path,
    // so setProposedEdit returns false (panel-only fallback).
    const ed = track(makeEditor('<p>First</p><p>Second</p>'))
    const ok = ed.commands.setProposedEdit({
      from: 1,
      to: 14,
      markdown: 'Para one\n\nPara two'
    })
    expect(ok).toBe(false)
    expect(hasProposedEdit(ed)).toBe(false)
  })

  it('multi-block accept is a single undo step', () => {
    const ed = track(
      makeSiltEditor(
        '<div data-type="note">First</div><div data-type="note">Second</div>'
      )
    )
    const originalBlocks = noteBlocks(ed)
    ed.commands.setProposedEdit({
      from: 1,
      to: 14,
      markdown: 'Para one\n\nPara two'
    })
    ed.commands.acceptProposedEdit()
    expect(noteBlocks(ed).length).toBe(2)
    // One undo reverts the multi-block accept (restores original noteBlocks).
    ed.commands.undo()
    const undoneBlocks = noteBlocks(ed)
    expect(undoneBlocks.length).toBe(originalBlocks.length)
    expect(undoneBlocks[0].content[0].text).toBe(
      originalBlocks[0].content[0].text
    )
    expect(undoneBlocks[1].content[0].text).toBe(
      originalBlocks[1].content[0].text
    )
  })

  it('reject adds no history entry (multi-block proposal)', () => {
    const ed = track(
      makeSiltEditor(
        '<div data-type="note">First</div><div data-type="note">Second</div>'
      )
    )
    const originalTexts = noteBlocks(ed).map((b) => b.content?.[0]?.text ?? '')
    ed.commands.setProposedEdit({
      from: 1,
      to: 14,
      markdown: 'Para one\n\nPara two'
    })
    ed.commands.rejectProposedEdit()
    expect(hasProposedEdit(ed)).toBe(false)
    // Reject is meta-only — no doc history entry. Undo is a no-op (nothing
    // to undo), and the noteBlock content is unchanged.
    ed.commands.undo()
    const undoneTexts = noteBlocks(ed).map((b) => b.content?.[0]?.text ?? '')
    expect(undoneTexts).toEqual(originalTexts)
  })

  it('partial-block selection replaces whole blocks (isolating behavior)', () => {
    // Select from inside block 1 (after "Fi", pos 3) to inside block 2
    // (before "ond" of "Second", pos 12). With noteBlock's isolating:true,
    // the multi-block path expands to whole-block boundaries — the entire
    // "First" and "Second" blocks are replaced.
    const ed = track(
      makeSiltEditor(
        '<div data-type="note">First</div><div data-type="note">Second</div>'
      )
    )
    ed.commands.setProposedEdit({
      from: 3,
      to: 12,
      markdown: 'XX\n\nYY'
    })
    ed.commands.acceptProposedEdit()
    const blocks = noteBlocks(ed)
    expect(blocks.length).toBe(2)
    expect(blocks[0].content[0].text).toBe('XX')
    expect(blocks[1].content[0].text).toBe('YY')
  })

  it('multi-paragraph with 3 paragraphs creates 3 noteBlocks', () => {
    const ed = track(
      makeSiltEditor(
        '<div data-type="note">First</div><div data-type="note">Second</div>'
      )
    )
    ed.commands.setProposedEdit({
      from: 1,
      to: 14,
      markdown: 'One\n\nTwo\n\nThree'
    })
    ed.commands.acceptProposedEdit()
    const blocks = noteBlocks(ed)
    expect(blocks.length).toBe(3)
    expect(blocks[0].content[0].text).toBe('One')
    expect(blocks[1].content[0].text).toBe('Two')
    expect(blocks[2].content[0].text).toBe('Three')
  })
})
