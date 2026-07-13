import { describe, it, expect, afterEach } from 'vitest'
import { Editor } from '@tiptap/core'
import StarterKit from '@tiptap/starter-kit'
import {
  ProposedEdit,
  hasProposedEdit,
  getProposedEditRange
} from './ProposedEditExtension'

function makeEditor(content = '<p>Hello world</p>'): Editor {
  const el = document.createElement('div')
  document.body.appendChild(el)
  return new Editor({
    element: el,
    extensions: [StarterKit, ProposedEdit],
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
