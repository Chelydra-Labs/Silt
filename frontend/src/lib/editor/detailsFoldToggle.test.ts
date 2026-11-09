// Details fold-toggle regression (#1115 review). With `persist: true` the
// tiptap NodeView's toggle button dispatches a document transaction
// (setNodeMarkup flipping `open`). Silt intercepts the click in the capture
// phase (schema.ts detailsFoldToggleKey) and re-dispatches with
// `addToHistory: false`, preserving identity attrs. These tests pin the
// three consumer-visible guarantees: a fold toggle never enters undo
// history, never reaches onUpdate as a content edit (the autosave /
// sub-editor dirty guards skip tagged transactions), and never resets the
// details node's `id`/`file_date` (stock setNodeMarkup would).

import { describe, it, expect, afterEach } from 'vitest'
import { Editor } from '@tiptap/core'
import StarterKit from '@tiptap/starter-kit'
import type { Transaction } from '@tiptap/pm/state'
import {
  SiltBlockExtensions,
  SiltDetailsExtensions,
  isDetailsFoldToggle
} from './index'

const editors: Editor[] = []
function track(editor: Editor): Editor {
  editors.push(editor)
  return editor
}
afterEach(() => {
  while (editors.length) {
    const editor = editors.pop()
    if (editor && !editor.isDestroyed) editor.destroy()
  }
})

// One details section (open) holding a single noteBlock, shaped like the
// section /meeting inserts.
function detailsDoc() {
  return {
    type: 'doc',
    content: [
      {
        type: 'details',
        attrs: { id: 'det-1', open: true, depth: 0, file_date: '2026-11-09' },
        content: [
          {
            type: 'detailsSummary',
            content: [{ type: 'text', text: 'Meeting information' }]
          },
          {
            type: 'detailsContent',
            content: [
              {
                type: 'noteBlock',
                attrs: { id: 'field-1', depth: 0, bullet: '' },
                content: [{ type: 'text', text: 'Subject:' }]
              }
            ]
          }
        ]
      }
    ]
  }
}

function makeEditor(onUpdate: (tr: Transaction) => void): Editor {
  return track(
    new Editor({
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
        ...SiltDetailsExtensions
      ],
      content: detailsDoc(),
      onUpdate: ({ transaction }) => onUpdate(transaction)
    })
  )
}

// The details NodeView auto-expands via setTimeout; settle it before
// clicking so the class/aria assertions see the expanded state first.
function settleInitialExpansion(): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, 0))
}

function toggleButton(editor: Editor): {
  wrapper: HTMLElement
  toggle: HTMLButtonElement
} {
  const wrapper = editor.view.dom.querySelector('[data-type="details"]')
  expect(wrapper).toBeTruthy()
  const toggle = wrapper!.firstElementChild
  expect(toggle instanceof HTMLButtonElement).toBe(true)
  return {
    wrapper: wrapper as HTMLElement,
    toggle: toggle as HTMLButtonElement
  }
}

describe('details fold toggle interception (persist: true)', () => {
  it('a toggle click flips `open` without entering undo history', async () => {
    const seen: boolean[] = []
    const editor = makeEditor((tr) => seen.push(isDetailsFoldToggle(tr)))
    const { wrapper, toggle } = toggleButton(editor)
    await settleInitialExpansion()
    expect(toggle.getAttribute('aria-label')).toBe('Collapse details content')

    toggle.click()

    const details = editor.state.doc.child(0)
    expect(details.type.name).toBe('details')
    expect(details.attrs.open).toBe(false)
    // Nothing else was edited: a fold toggle must leave nothing to undo.
    expect(editor.can().undo()).toBe(false)
    // onUpdate saw exactly the tagged toggle transaction — the autosave /
    // sub-editor dirty guards skip it.
    expect(seen).toEqual([true])
    // The NodeView stayed in sync with the model.
    expect(toggle.getAttribute('aria-label')).toBe('Expand details content')
    expect(wrapper.classList.contains('is-open')).toBe(false)
  })

  it('toggling twice returns to expanded, still with no history entry', async () => {
    const editor = makeEditor(() => {})
    const { toggle } = toggleButton(editor)
    await settleInitialExpansion()

    toggle.click()
    toggle.click()

    expect(editor.state.doc.child(0).attrs.open).toBe(true)
    expect(editor.can().undo()).toBe(false)
  })

  it('a toggle preserves the details node identity attrs', async () => {
    const editor = makeEditor(() => {})
    const { toggle } = toggleButton(editor)
    await settleInitialExpansion()

    toggle.click()

    const attrs = editor.state.doc.child(0).attrs
    // Stock setNodeMarkup({open}) would reset these to their defaults —
    // re-minting the block's id (#310 identity instability).
    expect(attrs.id).toBe('det-1')
    expect(attrs.file_date).toBe('2026-11-09')
    expect(attrs.depth).toBe(0)
  })

  it('undo after a fold toggle reverts the text edit, not the fold', async () => {
    const seen: boolean[] = []
    const editor = makeEditor((tr) => seen.push(isDetailsFoldToggle(tr)))
    const { toggle } = toggleButton(editor)
    await settleInitialExpansion()

    editor.commands.insertContent('hello')
    expect(seen).toEqual([false])
    expect(editor.can().undo()).toBe(true)

    toggle.click()
    expect(seen).toEqual([false, true])
    expect(editor.state.doc.child(0).attrs.open).toBe(false)

    editor.commands.undo()
    // The user's last real edit is undone; the fold stays collapsed —
    // Ctrl+Z after a fold click must not "invisibly" re-expand.
    expect(editor.state.doc.textContent).not.toContain('hello')
    expect(editor.state.doc.child(0).type.name).toBe('details')
    expect(editor.state.doc.child(0).attrs.open).toBe(false)
    // Nothing left to undo: the toggle never entered history.
    expect(editor.can().undo()).toBe(false)
  })
})
