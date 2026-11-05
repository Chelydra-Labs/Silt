import { describe, it, expect } from 'vitest'
import { Editor } from '@tiptap/core'
import StarterKit from '@tiptap/starter-kit'
import {
  PlainPaste,
  _setShiftHeldForTests,
  _getShiftHeldForTests
} from './plainPaste'

// PlainPaste reads the clipboard's text/plain in handlePaste when shift was
// held (Ctrl+Shift+V) and inserts it as plain text, suppressing ProseMirror's
// default rich-HTML parse. The shift flag is set by the keydown handler and
// reset by handlePaste; jsdom's ClipboardEvent doesn't populate clipboardData
// reliably, so these tests invoke the merged handlePaste prop directly with a
// stub DataTransfer and set the shift precondition via the test accessor.

function mountEditor() {
  const container = document.createElement('div')
  document.body.appendChild(container)
  const editor = new Editor({
    element: container,
    extensions: [StarterKit, PlainPaste],
    content: '<p></p>'
  })
  return {
    editor,
    cleanup: () => {
      editor.destroy()
      container.remove()
    }
  }
}

/** A minimal ClipboardEvent stand-in with a DataTransfer.getData('text/plain'). */
function pasteEvent(plainText: string): ClipboardEvent {
  return {
    clipboardData: {
      getData: (type: string) => (type === 'text/plain' ? plainText : '')
    }
  } as unknown as ClipboardEvent
}

/** PlainPaste's runtime plugin spec props (the Plugin type hides `.key`). */
function plainPasteProps(editor: Editor): {
  handleDOMEvents?: {
    paste?: (view: Editor['view'], event: ClipboardEvent) => boolean | void
  }
  handlePaste?: (
    view: Editor['view'],
    event: ClipboardEvent,
    slice: ReturnType<Editor['state']['selection']['content']>
  ) => boolean | void
} {
  const plugin = (
    editor.view.state.plugins as unknown as Array<{
      key?: string
      spec?: {
        props?: {
          handleDOMEvents?: {
            paste?: (
              view: Editor['view'],
              event: ClipboardEvent
            ) => boolean | void
          }
          handlePaste?: (
            view: Editor['view'],
            event: ClipboardEvent,
            slice: ReturnType<Editor['state']['selection']['content']>
          ) => boolean | void
        }
      }
    }>
  ).find((p) => typeof p.key === 'string' && p.key.startsWith('plainPaste'))
  return plugin?.spec?.props ?? {}
}

/** Simulate the real paste order: the DOM `paste` prop runs first
 *  (`runCustomHandler`), then ProseMirror's `doPaste` walks the `handlePaste`
 *  chain — Link defers (returns false), then PlainPaste runs. Calling
 *  handlePaste alone would skip the flag snapshot the DOM hook performs. */
function simulatePaste(editor: Editor, event: ClipboardEvent): boolean {
  const props = plainPasteProps(editor)
  props.handleDOMEvents?.paste?.(editor.view, event)
  return (
    props.handlePaste?.(
      editor.view,
      event,
      editor.state.selection.content()
    ) === true
  )
}

describe('PlainPaste', () => {
  it('inserts text/plain and suppresses default when shift held', () => {
    const { editor, cleanup } = mountEditor()
    try {
      _setShiftHeldForTests(true)
      const handled = simulatePaste(editor, pasteEvent('hello plain'))
      expect(handled).toBe(true)
      expect(editor.getText()).toBe('hello plain')
    } finally {
      cleanup()
    }
  })

  it('falls through (returns false) when shift NOT held — rich paste proceeds', () => {
    const { editor, cleanup } = mountEditor()
    try {
      _setShiftHeldForTests(false)
      const handled = simulatePaste(editor, pasteEvent('ignored'))
      expect(handled).toBe(false)
      expect(editor.getText()).toBe('')
    } finally {
      cleanup()
    }
  })

  it('returns false for an empty text/plain payload (nothing to insert)', () => {
    const { editor, cleanup } = mountEditor()
    try {
      _setShiftHeldForTests(true)
      const handled = simulatePaste(editor, pasteEvent(''))
      expect(handled).toBe(false)
    } finally {
      cleanup()
    }
  })

  it('resets the shift flag after a handled paste so a following paste is not plain', () => {
    const { editor, cleanup } = mountEditor()
    try {
      _setShiftHeldForTests(true)
      expect(simulatePaste(editor, pasteEvent('first'))).toBe(true)
      // The DOM paste hook consumed the flag — a second paste with no
      // intervening keydown (e.g. a context-menu paste) must NOT be plain.
      expect(_getShiftHeldForTests()).toBe(false)
      expect(simulatePaste(editor, pasteEvent('second'))).toBe(false)
      expect(editor.getText()).toBe('first')
    } finally {
      cleanup()
    }
  })

  it('clears the flag even when another handlePaste handler claims the paste', () => {
    // A direct editorProp handlePaste runs before this plugin's handler and
    // wins by returning true (the shared block-paste path does exactly that for
    // multi-line plain text into an empty block). Only the DOM hook runs, yet
    // the flag must not survive into the next context-menu paste.
    const { editor, cleanup } = mountEditor()
    try {
      _setShiftHeldForTests(true)
      const props = plainPasteProps(editor)
      props.handleDOMEvents?.paste?.(editor.view, pasteEvent('claimed'))
      expect(_getShiftHeldForTests()).toBe(false)
      // Preempted means handlePaste never ran for that paste; the next paste
      // must still take the rich path rather than inserting raw plain text.
      expect(simulatePaste(editor, pasteEvent('rich'))).toBe(false)
      expect(editor.getText()).toBe('')
    } finally {
      cleanup()
    }
  })

  it('keydown handler sets the shift flag from the event modifier', () => {
    // Confirms the keydown→flag glue is wired (the diagnostic the earlier
    // dispatch-based tests couldn't isolate). Real user input flows through
    // this same handler; a context-menu paste has no preceding keydown.
    const { editor, cleanup } = mountEditor()
    try {
      _setShiftHeldForTests(false)
      editor.view.dom.dispatchEvent(
        new KeyboardEvent('keydown', { shiftKey: true, bubbles: true })
      )
      // Whether or not the DOM-event handler fires in jsdom (ProseMirror's
      // event pipeline is part-internal), the feature's correctness rests on
      // handlePaste honoring the flag — covered above. This assertion pins the
      // intended wiring; if jsdom doesn't fire handleDOMEvents, this is the
      // canary that surfaces it rather than silently passing.
      expect(_getShiftHeldForTests()).toBe(true)
    } finally {
      cleanup()
    }
  })
})
