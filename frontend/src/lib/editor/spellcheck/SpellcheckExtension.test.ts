import { describe, it, expect, beforeEach, vi } from 'vitest'

// Mock the dictionary so checkWord flags exactly one word ("mispelled"). This
// isolates the extension's tokenization + scope-filter logic from Hunspell.
// vi.mock is hoisted above the static import below by vitest's transformer.
const mocks = vi.hoisted(() => ({
  checkWord: vi.fn((w: string) => w.toLowerCase() !== 'mispelled')
}))
vi.mock('./dictionary', () => ({
  checkWord: mocks.checkWord,
  loadDictionary: vi.fn().mockResolvedValue({ loaded: true }),
  setCustomWords: vi.fn(),
  suggest: vi.fn().mockReturnValue([])
}))

import { Editor } from '@tiptap/core'
import StarterKit from '@tiptap/starter-kit'
import Link from '@tiptap/extension-link'
import { Spellcheck } from './SpellcheckExtension'

function mount(content: string) {
  const container = document.createElement('div')
  document.body.appendChild(container)
  const editor = new Editor({
    element: container,
    extensions: [StarterKit, Link, Spellcheck],
    content
  })
  return {
    editor,
    cleanup: () => {
      editor.destroy()
      container.remove()
    }
  }
}

/** Count rendered .silt-spell-error spans in the editor DOM. */
function errorCount(editor: Editor): number {
  return editor.view.dom.querySelectorAll('.silt-spell-error').length
}

describe('Spellcheck extension (#196)', () => {
  beforeEach(() => {
    mocks.checkWord.mockClear()
  })

  it('underlines the misspelled word', () => {
    const { editor, cleanup } = mount('<p>this is mispelled text</p>')
    try {
      expect(errorCount(editor)).toBe(1)
    } finally {
      cleanup()
    }
  })

  it('flags every occurrence of the misspelled word', () => {
    const { editor, cleanup } = mount('<p>mispelled and mispelled again</p>')
    try {
      expect(errorCount(editor)).toBe(2)
    } finally {
      cleanup()
    }
  })

  it('does NOT flag inside a fenced code block', () => {
    const { editor, cleanup } = mount(
      '<pre><code>mispelled code here</code></pre>'
    )
    try {
      expect(errorCount(editor)).toBe(0)
    } finally {
      cleanup()
    }
  })

  it('does NOT flag inside inline code', () => {
    const { editor, cleanup } = mount(
      '<p>this is <code>mispelled</code> ok</p>'
    )
    try {
      expect(errorCount(editor)).toBe(0)
    } finally {
      cleanup()
    }
  })

  it('does NOT flag inside a link', () => {
    const { editor, cleanup } = mount(
      '<p><a href="http://x">mispelled</a> mispelled</p>'
    )
    try {
      // Only the standalone occurrence; the link text is skipped.
      expect(errorCount(editor)).toBe(1)
    } finally {
      cleanup()
    }
  })

  it('skips ALLCAPS acronyms (JSON, API)', () => {
    const { editor, cleanup } = mount('<p>MISPELLED mispelled</p>')
    try {
      // Only the mixed-case occurrence; the ALLCAPS one is skipped.
      expect(errorCount(editor)).toBe(1)
    } finally {
      cleanup()
    }
  })

  it('skips camelCase identifiers', () => {
    const { editor, cleanup } = mount('<p>mispelled misPelled</p>')
    try {
      // camelCase "misPelled" is skipped; only plain "mispelled" flags.
      expect(errorCount(editor)).toBe(1)
    } finally {
      cleanup()
    }
  })

  it('skips Dataview [key:: value] tokens', () => {
    const { editor, cleanup } = mount('<p>[owner:: mispelled] mispelled</p>')
    try {
      // The token inside [owner:: ...] is skipped; the standalone one flags.
      expect(errorCount(editor)).toBe(1)
    } finally {
      cleanup()
    }
  })

  it('rechecks after a debounced doc edit (catches newly-typed misspellings)', async () => {
    const { editor, cleanup } = mount('<p>good text</p>')
    try {
      expect(errorCount(editor)).toBe(0)
      // Type a misspelled word as a standalone token (trailing space keeps it
      // from merging with the preceding text into one word).
      editor.commands.insertContent({ type: 'text', text: 'mispelled ' })
      // Before the debounce fires, no new underline.
      expect(errorCount(editor)).toBe(0)
      // After the debounce (300 ms), the new word is flagged.
      await new Promise((r) => setTimeout(r, 360))
      expect(errorCount(editor)).toBe(1)
    } finally {
      cleanup()
    }
  })
})
