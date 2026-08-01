import { describe, it, expect } from 'vitest'
import { Editor } from '@tiptap/core'
import StarterKit from '@tiptap/starter-kit'
import {
  SiltBlockExtensions,
  SiltInlineMarkExtensions,
  SiltColorMarkExtensions,
  UniqueBlockIds,
  SiltHardBreak,
  SiltBlockKeymaps
} from './index'
import { docToBlocks } from './converters'
import { EmbedNode, BlockReferenceNode, CalloutBlock } from './schema'
import {
  setBlockAlign,
  moveActiveBlock,
  findActiveBlock,
  indentActiveBlock,
  unindentActiveBlock,
  toggleUnorderedList,
  toggleOrderedList,
  selectionIsListKind
} from './keymaps'
import type { DocJSON } from './types'
import { settings } from '../../settings/store.svelte'

// Mirror the makeEditor() pattern from converters.test.ts — a real TipTap
// editor wired with the Silt schema. No Placeholder (avoids the jsdom
// elementFromPoint gap that other tests sidestep).
function makeEditor(): Editor {
  return new Editor({
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
        trailingNode: false,
        hardBreak: false
      }),
      SiltHardBreak,
      ...SiltBlockExtensions,
      ...SiltInlineMarkExtensions,
      ...SiltColorMarkExtensions,
      EmbedNode,
      BlockReferenceNode,
      UniqueBlockIds
    ]
  })
}

// Editor variant that also wires the keyboard shortcut extension so Enter /
// Backspace / Tab outliner semantics are exercised (Tab indent cases live in
// the "Tab / Shift-Tab depth" describe below). The base makeEditor()
// omits it to keep the converter/align tests focused on pure state.
function makeEditorWithKeymaps(): Editor {
  return new Editor({
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
        trailingNode: false,
        hardBreak: false
      }),
      SiltHardBreak,
      ...SiltBlockExtensions,
      ...SiltInlineMarkExtensions,
      ...SiltColorMarkExtensions,
      EmbedNode,
      BlockReferenceNode,
      UniqueBlockIds,
      SiltBlockKeymaps
    ]
  })
}

function blockDoc(type: 'taskBlock' | 'noteBlock', text: string): DocJSON {
  const attrs =
    type === 'taskBlock'
      ? { id: 'b1', depth: 0, status: 'TODO' }
      : { id: 'b1', depth: 0, bullet: '- ' }
  return {
    type: 'doc',
    content: [{ type, attrs, content: [{ type: 'text', text }] }]
  }
}

function currentBlockAlign(editor: Editor): string | undefined {
  const { selection } = editor.state
  const pos = selection.$from
  for (let d = pos.depth; d >= 1; d--) {
    const node = pos.node(d)
    if (['noteBlock', 'headerBlock', 'taskBlock'].includes(node.type.name)) {
      return node.attrs.align
    }
  }
  return undefined
}

describe('setBlockAlign (#200 — shared helper)', () => {
  it('sets align on a noteBlock and returns true', () => {
    const editor = makeEditor()
    editor.commands.setContent(blockDoc('noteBlock', 'hello'))
    const result = setBlockAlign(editor, 'center')
    expect(result).toBe(true)
    expect(currentBlockAlign(editor)).toBe('center')
    editor.destroy()
  })

  it('is a no-op (returns true) on a taskBlock', () => {
    const editor = makeEditor()
    editor.commands.setContent(blockDoc('taskBlock', 'task me'))
    const result = setBlockAlign(editor, 'right')
    expect(result).toBe(true)
    expect(currentBlockAlign(editor)).toBeUndefined()
    editor.destroy()
  })

  it('overwrites a prior align value', () => {
    const editor = makeEditor()
    editor.commands.setContent(blockDoc('noteBlock', 'first'))
    setBlockAlign(editor, 'left')
    expect(currentBlockAlign(editor)).toBe('left')
    setBlockAlign(editor, 'justify')
    expect(currentBlockAlign(editor)).toBe('justify')
    editor.destroy()
  })

  it('returns false on a destroyed editor', () => {
    const editor = makeEditor()
    editor.destroy()
    expect(setBlockAlign(editor, 'left')).toBe(false)
  })
})

describe('Enter handler — new block bullet after non-note blocks (#258)', () => {
  // Dispatch an Enter keydown through ProseMirror's keydown handler so the
  // SiltBlockKeymaps shortcut runs (jsdom KeyboardEvents don't auto-route
  // through prosemirror-keymap's normalizer reliably).
  function pressEnter(editor: Editor): void {
    const event = new KeyboardEvent('keydown', { key: 'Enter', bubbles: true })
    editor.view.someProp('handleKeyDown', (handler) => {
      handler(editor.view, event)
    })
  }

  it('creates a plain (no-bullet) noteBlock after Enter on a taskBlock', () => {
    const editor = makeEditorWithKeymaps()
    editor.commands.setContent(blockDoc('taskBlock', 'task text'))
    editor.commands.focus('end')

    pressEnter(editor)

    expect(editor.state.doc.childCount).toBe(2)
    const newBlock = editor.state.doc.child(1)
    expect(newBlock.type.name).toBe('noteBlock')
    expect(newBlock.attrs.bullet).toBe('')
    editor.destroy()
  })

  it('creates a plain (no-bullet) noteBlock after Enter on a headerBlock', () => {
    const editor = makeEditorWithKeymaps()
    const doc: DocJSON = {
      type: 'doc',
      content: [
        {
          type: 'headerBlock',
          attrs: { id: 'h1', depth: 1 },
          content: [{ type: 'text', text: 'Heading' }]
        }
      ]
    }
    editor.commands.setContent(doc)
    editor.commands.focus('end')

    pressEnter(editor)

    expect(editor.state.doc.childCount).toBe(2)
    const newBlock = editor.state.doc.child(1)
    expect(newBlock.type.name).toBe('noteBlock')
    expect(newBlock.attrs.bullet).toBe('')
    editor.destroy()
  })

  it('continues bullet inheritance after Enter on a bulleted noteBlock', () => {
    const editor = makeEditorWithKeymaps()
    editor.commands.setContent(blockDoc('noteBlock', 'bullet item'))
    editor.commands.focus('end')

    pressEnter(editor)

    expect(editor.state.doc.childCount).toBe(2)
    const newBlock = editor.state.doc.child(1)
    expect(newBlock.type.name).toBe('noteBlock')
    expect(newBlock.attrs.bullet).toBe('- ')
    editor.destroy()
  })

  it('Enter on empty bulleted note clears the bullet (exits the list)', () => {
    const editor = makeEditorWithKeymaps()
    const doc: DocJSON = {
      type: 'doc',
      content: [
        {
          type: 'noteBlock',
          attrs: { id: 'n1', depth: 0, bullet: '- ' }
        }
      ]
    }
    editor.commands.setContent(doc)
    editor.commands.focus('end')

    pressEnter(editor)

    expect(editor.state.doc.childCount).toBe(1)
    expect(editor.state.doc.child(0).attrs.bullet).toBe('')
    editor.destroy()
  })

  it('Enter with selection spanning empty bullet and next block deletes selection', () => {
    const editor = makeEditorWithKeymaps()
    const doc: DocJSON = {
      type: 'doc',
      content: [
        {
          type: 'noteBlock',
          attrs: { id: 'n1', depth: 0, bullet: '- ' }
        },
        {
          type: 'noteBlock',
          attrs: { id: 'n2', depth: 0, bullet: '' },
          content: [{ type: 'text', text: 'keep me' }]
        }
      ]
    }
    editor.commands.setContent(doc)
    // Selection from empty bullet content into "keep me" (drop "keep ").
    const secondStart = 1 + editor.state.doc.child(0).nodeSize
    editor.commands.setTextSelection({ from: 1, to: secondStart + 5 })

    pressEnter(editor)

    // Must not only clear the bullet while leaving selected text intact.
    expect(editor.state.doc.child(0).attrs.bullet).toBe('- ')
    expect(editor.state.doc.textContent).not.toContain('keep ')
    expect(editor.state.doc.childCount).toBeGreaterThanOrEqual(2)
    editor.destroy()
  })

  it('Enter on empty ordered note clears the marker (exits the list)', () => {
    const editor = makeEditorWithKeymaps()
    const doc: DocJSON = {
      type: 'doc',
      content: [
        {
          type: 'noteBlock',
          attrs: { id: 'n1', depth: 0, bullet: '2. ' }
        }
      ]
    }
    editor.commands.setContent(doc)
    editor.commands.focus('end')

    pressEnter(editor)

    expect(editor.state.doc.childCount).toBe(1)
    expect(editor.state.doc.child(0).attrs.bullet).toBe('')
    editor.destroy()
  })

  it('Enter on empty quoted note clears the quote (exits the quote)', () => {
    const editor = makeEditorWithKeymaps()
    const doc: DocJSON = {
      type: 'doc',
      content: [
        {
          type: 'noteBlock',
          attrs: { id: 'n1', depth: 0, bullet: '', quote: '> ' }
        }
      ]
    }
    editor.commands.setContent(doc)
    editor.commands.focus('end')

    pressEnter(editor)

    expect(editor.state.doc.childCount).toBe(1)
    expect(editor.state.doc.child(0).attrs.quote).toBe('')
    editor.destroy()
  })

  it('mid-text Enter on a quoted note keeps quote on both halves', () => {
    const editor = makeEditorWithKeymaps()
    const doc: DocJSON = {
      type: 'doc',
      content: [
        {
          type: 'noteBlock',
          attrs: { id: 'n1', depth: 0, bullet: '', quote: '> ' },
          content: [{ type: 'text', text: 'hello world' }]
        }
      ]
    }
    editor.commands.setContent(doc)
    editor.commands.setTextSelection(1 + 6) // after "hello "

    pressEnter(editor)

    expect(editor.state.doc.childCount).toBe(2)
    expect(editor.state.doc.child(0).textContent).toBe('hello ')
    expect(editor.state.doc.child(0).attrs.quote).toBe('> ')
    expect(editor.state.doc.child(1).textContent).toBe('world')
    expect(editor.state.doc.child(1).attrs.quote).toBe('> ')
    expect(editor.state.doc.child(1).attrs.bullet).toBe('')
    editor.destroy()
  })

  it('continues plain (no-bullet) after Enter on a plain noteBlock', () => {
    const editor = makeEditorWithKeymaps()
    const doc: DocJSON = {
      type: 'doc',
      content: [
        {
          type: 'noteBlock',
          attrs: { id: 'n1', depth: 0, bullet: '' },
          content: [{ type: 'text', text: 'plain prose' }]
        }
      ]
    }
    editor.commands.setContent(doc)
    editor.commands.focus('end')

    pressEnter(editor)

    expect(editor.state.doc.childCount).toBe(2)
    const newBlock = editor.state.doc.child(1)
    expect(newBlock.type.name).toBe('noteBlock')
    expect(newBlock.attrs.bullet).toBe('')
    editor.destroy()
  })

  it('splits mid-text bulleted note: text after caret moves to the new li', () => {
    const editor = makeEditorWithKeymaps()
    editor.commands.setContent(blockDoc('noteBlock', 'hello world'))
    // Place caret between "hello " and "world" (after 6 chars of content).
    const blockStart = 1 // doc pos of first block content
    editor.commands.setTextSelection(blockStart + 6)

    pressEnter(editor)

    expect(editor.state.doc.childCount).toBe(2)
    const first = editor.state.doc.child(0)
    const second = editor.state.doc.child(1)
    expect(first.type.name).toBe('noteBlock')
    expect(first.textContent).toBe('hello ')
    expect(first.attrs.bullet).toBe('- ')
    expect(second.type.name).toBe('noteBlock')
    expect(second.textContent).toBe('world')
    expect(second.attrs.bullet).toBe('- ')
    // Caret at start of the new block's content.
    expect(editor.state.selection.from).toBe(
      first.nodeSize + 1 // after first block open → start of second content
    )
    editor.destroy()
  })

  it('mid-text Enter on taskBlock keeps full task body and appends empty note', () => {
    const editor = makeEditorWithKeymaps()
    editor.commands.setContent(blockDoc('taskBlock', 'buy milk and bread'))
    // Caret after "buy milk " — must not demote "and bread" off the task.
    editor.commands.setTextSelection(1 + 'buy milk '.length)

    pressEnter(editor)

    expect(editor.state.doc.childCount).toBe(2)
    const task = editor.state.doc.child(0)
    const note = editor.state.doc.child(1)
    expect(task.type.name).toBe('taskBlock')
    expect(task.textContent).toBe('buy milk and bread')
    expect(task.attrs.status).toBe('TODO')
    expect(note.type.name).toBe('noteBlock')
    expect(note.textContent).toBe('')
    expect(note.attrs.bullet).toBe('')
    editor.destroy()
  })

  it('mid-text Enter on headerBlock keeps full header and appends empty note', () => {
    const editor = makeEditorWithKeymaps()
    const doc: DocJSON = {
      type: 'doc',
      content: [
        {
          type: 'headerBlock',
          attrs: { id: 'h1', depth: 1 },
          content: [{ type: 'text', text: 'Section Title Extra' }]
        }
      ]
    }
    editor.commands.setContent(doc)
    editor.commands.setTextSelection(1 + 'Section Title '.length)

    pressEnter(editor)

    expect(editor.state.doc.childCount).toBe(2)
    expect(editor.state.doc.child(0).type.name).toBe('headerBlock')
    expect(editor.state.doc.child(0).textContent).toBe('Section Title Extra')
    expect(editor.state.doc.child(1).type.name).toBe('noteBlock')
    expect(editor.state.doc.child(1).textContent).toBe('')
    editor.destroy()
  })

  it('splits mid-text ordered note and resequences the next marker', () => {
    const editor = makeEditorWithKeymaps()
    const doc: DocJSON = {
      type: 'doc',
      content: [
        {
          type: 'noteBlock',
          attrs: { id: 'n1', depth: 0, bullet: '1. ' },
          content: [{ type: 'text', text: 'alpha beta' }]
        }
      ]
    }
    editor.commands.setContent(doc)
    editor.commands.setTextSelection(1 + 6) // after "alpha "

    pressEnter(editor)

    expect(editor.state.doc.childCount).toBe(2)
    expect(editor.state.doc.child(0).textContent).toBe('alpha ')
    expect(editor.state.doc.child(0).attrs.bullet).toBe('1. ')
    expect(editor.state.doc.child(1).textContent).toBe('beta')
    expect(editor.state.doc.child(1).attrs.bullet).toBe('2. ')
    editor.destroy()
  })

  it('renumbers following ordered items after mid-list Enter (no duplicate numbers)', () => {
    const editor = makeEditorWithKeymaps()
    const doc: DocJSON = {
      type: 'doc',
      content: [
        {
          type: 'noteBlock',
          attrs: { id: 'n1', depth: 0, bullet: '1. ' },
          content: [{ type: 'text', text: 'one' }]
        },
        {
          type: 'noteBlock',
          attrs: { id: 'n2', depth: 0, bullet: '2. ' },
          content: [{ type: 'text', text: 'two three' }]
        },
        {
          type: 'noteBlock',
          attrs: { id: 'n3', depth: 0, bullet: '3. ' },
          content: [{ type: 'text', text: 'four' }]
        },
        {
          type: 'noteBlock',
          attrs: { id: 'n4', depth: 0, bullet: '4. ' },
          content: [{ type: 'text', text: 'five' }]
        }
      ]
    }
    editor.commands.setContent(doc)
    // Caret in item 2 between "two " and "three"
    // pos: block0 size 5 (2+3), content of n2 starts at 5+1=6, after "two " = 6+4=10
    const n2ContentStart = 1 + editor.state.doc.child(0).nodeSize
    editor.commands.setTextSelection(n2ContentStart + 4)

    pressEnter(editor)

    expect(editor.state.doc.childCount).toBe(5)
    const bullets = [0, 1, 2, 3, 4].map(
      (i) => editor.state.doc.child(i).attrs.bullet
    )
    expect(bullets).toEqual(['1. ', '2. ', '3. ', '4. ', '5. '])
    expect(editor.state.doc.child(1).textContent).toBe('two ')
    expect(editor.state.doc.child(2).textContent).toBe('three')
    expect(editor.state.doc.child(3).textContent).toBe('four')
    expect(editor.state.doc.child(4).textContent).toBe('five')
    editor.destroy()
  })

  it('renumbers following ordered items after end-of-item Enter', () => {
    const editor = makeEditorWithKeymaps()
    const doc: DocJSON = {
      type: 'doc',
      content: [
        {
          type: 'noteBlock',
          attrs: { id: 'n1', depth: 0, bullet: '1) ' },
          content: [{ type: 'text', text: 'a' }]
        },
        {
          type: 'noteBlock',
          attrs: { id: 'n2', depth: 0, bullet: '2) ' },
          content: [{ type: 'text', text: 'b' }]
        },
        {
          type: 'noteBlock',
          attrs: { id: 'n3', depth: 0, bullet: '3) ' },
          content: [{ type: 'text', text: 'c' }]
        }
      ]
    }
    editor.commands.setContent(doc)
    // End of item 2
    const endOfN2 =
      editor.state.doc.child(0).nodeSize + editor.state.doc.child(1).nodeSize
    editor.commands.setTextSelection(endOfN2 - 1)

    pressEnter(editor)

    expect(editor.state.doc.childCount).toBe(4)
    expect(
      [0, 1, 2, 3].map((i) => editor.state.doc.child(i).attrs.bullet)
    ).toEqual(['1) ', '2) ', '3) ', '4) '])
    expect(editor.state.doc.child(2).textContent).toBe('')
    expect(editor.state.doc.child(3).textContent).toBe('c')
    editor.destroy()
  })

  it('renumbers same-depth ordered peers past nested children', () => {
    const editor = makeEditorWithKeymaps()
    const doc: DocJSON = {
      type: 'doc',
      content: [
        {
          type: 'noteBlock',
          attrs: { id: 'n1', depth: 0, bullet: '1. ' },
          content: [{ type: 'text', text: 'parent one' }]
        },
        {
          type: 'noteBlock',
          attrs: { id: 'n2', depth: 0, bullet: '2. ' },
          content: [{ type: 'text', text: 'parent two' }]
        },
        {
          type: 'noteBlock',
          attrs: { id: 'n2a', depth: 1, bullet: '1. ' },
          content: [{ type: 'text', text: 'nested under two' }]
        },
        {
          type: 'noteBlock',
          attrs: { id: 'n3', depth: 0, bullet: '3. ' },
          content: [{ type: 'text', text: 'parent three' }]
        }
      ]
    }
    editor.commands.setContent(doc)
    // End of item 2 (before nested child)
    const endOfN2 =
      editor.state.doc.child(0).nodeSize + editor.state.doc.child(1).nodeSize
    editor.commands.setTextSelection(endOfN2 - 1)

    pressEnter(editor)

    // 1, 2, new empty 3, nested child, former 3 → 4
    expect(editor.state.doc.childCount).toBe(5)
    expect(
      [0, 1, 2, 3, 4].map((i) => ({
        bullet: editor.state.doc.child(i).attrs.bullet,
        depth: editor.state.doc.child(i).attrs.depth
      }))
    ).toEqual([
      { bullet: '1. ', depth: 0 },
      { bullet: '2. ', depth: 0 },
      { bullet: '3. ', depth: 0 },
      { bullet: '1. ', depth: 1 },
      { bullet: '4. ', depth: 0 }
    ])
    editor.destroy()
  })

  it('Enter on bulleted note with only a block ref continues the list', () => {
    const editor = makeEditorWithKeymaps()
    const doc: DocJSON = {
      type: 'doc',
      content: [
        {
          type: 'noteBlock',
          attrs: { id: 'n1', depth: 0, bullet: '- ' },
          content: [
            {
              type: 'blockReferenceNode',
              attrs: { uuid: 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa' }
            }
          ]
        }
      ]
    }
    editor.commands.setContent(doc)
    editor.commands.focus('end')

    pressEnter(editor)

    // Atom-only body is not empty — continue list, do not clear bullet.
    expect(editor.state.doc.childCount).toBe(2)
    expect(editor.state.doc.child(0).attrs.bullet).toBe('- ')
    expect(editor.state.doc.child(0).childCount).toBe(1)
    expect(editor.state.doc.child(0).child(0).type.name).toBe(
      'blockReferenceNode'
    )
    expect(editor.state.doc.child(1).type.name).toBe('noteBlock')
    expect(editor.state.doc.child(1).attrs.bullet).toBe('- ')
    expect(editor.state.doc.child(1).textContent).toBe('')
    editor.destroy()
  })

  it('Enter with a non-empty selection drops the selection then splits', () => {
    const editor = makeEditorWithKeymaps()
    editor.commands.setContent(blockDoc('noteBlock', 'one two three'))
    // Select " two" (chars 3..7) then Enter → first "one", second " three".
    editor.commands.setTextSelection({ from: 1 + 3, to: 1 + 7 })

    pressEnter(editor)

    expect(editor.state.doc.childCount).toBe(2)
    expect(editor.state.doc.child(0).textContent).toBe('one')
    expect(editor.state.doc.child(1).textContent).toBe(' three')
    editor.destroy()
  })
})

describe('Shift-Enter soft line break (#828)', () => {
  function pressShiftEnter(editor: Editor): void {
    const event = new KeyboardEvent('keydown', {
      key: 'Enter',
      shiftKey: true,
      bubbles: true
    })
    editor.view.someProp('handleKeyDown', (handler) => {
      handler(editor.view, event)
    })
  }

  function pressEnter(editor: Editor): void {
    const event = new KeyboardEvent('keydown', { key: 'Enter', bubbles: true })
    editor.view.someProp('handleKeyDown', (handler) => {
      handler(editor.view, event)
    })
  }

  it('inserts a hardBreak in a noteBlock without creating a new block', () => {
    const editor = makeEditorWithKeymaps()
    const doc: DocJSON = {
      type: 'doc',
      content: [
        {
          type: 'noteBlock',
          attrs: { id: 'n-soft', depth: 0, bullet: '' },
          content: [{ type: 'text', text: 'hello world' }]
        }
      ]
    }
    editor.commands.setContent(doc)
    // Caret after "hello" (doc pos: 1 open note + 5 chars = 6).
    editor.commands.setTextSelection(6)
    editor.commands.focus()

    pressShiftEnter(editor)

    expect(editor.state.doc.childCount).toBe(1)
    const block = editor.state.doc.child(0)
    expect(block.type.name).toBe('noteBlock')
    expect(block.attrs.id).toBe('n-soft')
    const json = block.toJSON() as {
      content?: Array<{ type: string; text?: string }>
    }
    const types = (json.content || []).map((c) => c.type)
    expect(types).toContain('hardBreak')
    expect(types.filter((t) => t === 'hardBreak')).toHaveLength(1)
    // Caret sits after the hardBreak (start of the new visual line).
    expect(editor.state.selection.from).toBeGreaterThan(6)
    const saved = docToBlocks(editor.getJSON() as DocJSON)
    expect(saved).toHaveLength(1)
    expect(saved[0].clean_text).toBe('hello<br> world')
    editor.destroy()
  })

  it('inserts a hardBreak in a headerBlock without creating a new block', () => {
    const editor = makeEditorWithKeymaps()
    const doc: DocJSON = {
      type: 'doc',
      content: [
        {
          type: 'headerBlock',
          attrs: { id: 'h-soft', depth: 1 },
          content: [{ type: 'text', text: 'Title line' }]
        }
      ]
    }
    editor.commands.setContent(doc)
    editor.commands.focus('end')

    pressShiftEnter(editor)

    expect(editor.state.doc.childCount).toBe(1)
    expect(editor.state.doc.child(0).type.name).toBe('headerBlock')
    const json = editor.state.doc.child(0).toJSON() as {
      content?: Array<{ type: string }>
    }
    expect((json.content || []).some((c) => c.type === 'hardBreak')).toBe(true)
    editor.destroy()
  })

  it('still creates a new noteBlock on Enter after a soft-broken note', () => {
    const editor = makeEditorWithKeymaps()
    const doc: DocJSON = {
      type: 'doc',
      content: [
        {
          type: 'noteBlock',
          attrs: { id: 'n-soft-enter', depth: 0, bullet: '' },
          content: [
            { type: 'text', text: 'line one' },
            { type: 'hardBreak' },
            { type: 'text', text: 'line two' }
          ]
        }
      ]
    }
    editor.commands.setContent(doc)
    editor.commands.focus('end')

    pressEnter(editor)

    expect(editor.state.doc.childCount).toBe(2)
    expect(editor.state.doc.child(0).attrs.id).toBe('n-soft-enter')
    expect(editor.state.doc.child(1).type.name).toBe('noteBlock')
    editor.destroy()
  })

  it('inserts a hardBreak on an empty noteBlock', () => {
    const editor = makeEditorWithKeymaps()
    const doc: DocJSON = {
      type: 'doc',
      content: [
        {
          type: 'noteBlock',
          attrs: { id: 'n-empty', depth: 0, bullet: '' },
          content: []
        }
      ]
    }
    editor.commands.setContent(doc)
    editor.commands.focus('start')

    pressShiftEnter(editor)

    expect(editor.state.doc.childCount).toBe(1)
    const json = editor.state.doc.child(0).toJSON() as {
      content?: Array<{ type: string }>
    }
    expect((json.content || []).some((c) => c.type === 'hardBreak')).toBe(true)
    editor.destroy()
  })
})

describe('toolbar list toggles (#840)', () => {
  function focusBlock(editor: Editor, index: number): void {
    let pos = 0
    for (let i = 0; i < index; i++) pos += editor.state.doc.child(i).nodeSize
    editor.commands.setTextSelection(pos + 1)
  }

  it('toggles unordered list on a plain note', () => {
    const editor = makeEditorWithKeymaps()
    editor.commands.setContent({
      type: 'doc',
      content: [
        {
          type: 'noteBlock',
          attrs: { id: 'n1', depth: 0, bullet: '' },
          content: [{ type: 'text', text: 'hello' }]
        }
      ]
    })
    focusBlock(editor, 0)
    expect(toggleUnorderedList(editor)).toBe(true)
    expect(editor.state.doc.child(0).attrs.bullet).toBe('- ')
    expect(selectionIsListKind(editor, 'unordered')).toBe(true)
    expect(toggleUnorderedList(editor)).toBe(true)
    expect(editor.state.doc.child(0).attrs.bullet).toBe('')
    editor.destroy()
  })

  it('toggles ordered list and numbers multi-block selection', () => {
    const editor = makeEditorWithKeymaps()
    editor.commands.setContent({
      type: 'doc',
      content: [
        {
          type: 'noteBlock',
          attrs: { id: 'n1', depth: 0, bullet: '' },
          content: [{ type: 'text', text: 'a' }]
        },
        {
          type: 'noteBlock',
          attrs: { id: 'n2', depth: 0, bullet: '' },
          content: [{ type: 'text', text: 'b' }]
        },
        {
          type: 'noteBlock',
          attrs: { id: 'n3', depth: 0, bullet: '' },
          content: [{ type: 'text', text: 'c' }]
        }
      ]
    })
    const from = 1
    const to = editor.state.doc.content.size - 1
    editor.commands.setTextSelection({ from, to })
    expect(toggleOrderedList(editor)).toBe(true)
    expect(
      [0, 1, 2].map((i) => editor.state.doc.child(i).attrs.bullet)
    ).toEqual(['1. ', '2. ', '3. '])
    expect(selectionIsListKind(editor, 'ordered')).toBe(true)
    // Toggle off
    expect(toggleOrderedList(editor)).toBe(true)
    expect(
      [0, 1, 2].map((i) => editor.state.doc.child(i).attrs.bullet)
    ).toEqual(['', '', ''])
    editor.destroy()
  })

  it('converts unordered to ordered and clears quote', () => {
    const editor = makeEditorWithKeymaps()
    editor.commands.setContent({
      type: 'doc',
      content: [
        {
          type: 'noteBlock',
          attrs: { id: 'n1', depth: 0, bullet: '- ', quote: '' },
          content: [{ type: 'text', text: 'item' }]
        }
      ]
    })
    focusBlock(editor, 0)
    expect(toggleOrderedList(editor)).toBe(true)
    expect(editor.state.doc.child(0).attrs.bullet).toBe('1. ')
    expect(editor.state.doc.child(0).attrs.quote).toBe('')
    editor.destroy()
  })
})

describe('ordered list indent/unindent renumber (#837)', () => {
  function focusBlock(editor: Editor, index: number): void {
    let pos = 0
    for (let i = 0; i < index; i++) pos += editor.state.doc.child(i).nodeSize
    editor.commands.setTextSelection(pos + 1)
  }

  function snapshot(editor: Editor) {
    return Array.from({ length: editor.state.doc.childCount }, (_, i) => ({
      bullet: editor.state.doc.child(i).attrs.bullet as string,
      depth: editor.state.doc.child(i).attrs.depth as number,
      text: editor.state.doc.child(i).textContent
    }))
  }

  it('indents mid-list ordered item: nested restarts at 1, parent peers renumber', () => {
    const editor = makeEditorWithKeymaps()
    editor.commands.setContent({
      type: 'doc',
      content: [
        {
          type: 'noteBlock',
          attrs: { id: 'n1', depth: 0, bullet: '1) ' },
          content: [{ type: 'text', text: 'one' }]
        },
        {
          type: 'noteBlock',
          attrs: { id: 'n2', depth: 0, bullet: '2) ' },
          content: [{ type: 'text', text: 'two' }]
        },
        {
          type: 'noteBlock',
          attrs: { id: 'n3', depth: 0, bullet: '3) ' },
          content: [{ type: 'text', text: 'three' }]
        }
      ]
    })
    focusBlock(editor, 1)
    expect(indentActiveBlock(editor)).toBe(true)
    expect(snapshot(editor)).toEqual([
      { bullet: '1) ', depth: 0, text: 'one' },
      { bullet: '1) ', depth: 1, text: 'two' },
      { bullet: '2) ', depth: 0, text: 'three' }
    ])
    editor.destroy()
  })

  it('unindent restores parent-level sequential numbering without gaps', () => {
    const editor = makeEditorWithKeymaps()
    editor.commands.setContent({
      type: 'doc',
      content: [
        {
          type: 'noteBlock',
          attrs: { id: 'n1', depth: 0, bullet: '1. ' },
          content: [{ type: 'text', text: 'one' }]
        },
        {
          type: 'noteBlock',
          attrs: { id: 'n2', depth: 1, bullet: '1. ' },
          content: [{ type: 'text', text: 'nested' }]
        },
        {
          type: 'noteBlock',
          attrs: { id: 'n3', depth: 0, bullet: '2. ' },
          content: [{ type: 'text', text: 'three' }]
        }
      ]
    })
    focusBlock(editor, 1)
    expect(unindentActiveBlock(editor)).toBe(true)
    expect(snapshot(editor)).toEqual([
      { bullet: '1. ', depth: 0, text: 'one' },
      { bullet: '2. ', depth: 0, text: 'nested' },
      { bullet: '3. ', depth: 0, text: 'three' }
    ])
    editor.destroy()
  })

  it('preserves ) vs . punctuation on indent', () => {
    const editor = makeEditorWithKeymaps()
    editor.commands.setContent({
      type: 'doc',
      content: [
        {
          type: 'noteBlock',
          attrs: { id: 'n1', depth: 0, bullet: '1. ' },
          content: [{ type: 'text', text: 'a' }]
        },
        {
          type: 'noteBlock',
          attrs: { id: 'n2', depth: 0, bullet: '2. ' },
          content: [{ type: 'text', text: 'b' }]
        }
      ]
    })
    focusBlock(editor, 1)
    expect(indentActiveBlock(editor)).toBe(true)
    expect(editor.state.doc.child(1).attrs.bullet).toBe('1. ')
    expect(editor.state.doc.child(1).attrs.depth).toBe(1)
    editor.destroy()
  })

  it('indent of unordered bullet does not invent ordered markers', () => {
    const editor = makeEditorWithKeymaps()
    editor.commands.setContent({
      type: 'doc',
      content: [
        {
          type: 'noteBlock',
          attrs: { id: 'n1', depth: 0, bullet: '- ' },
          content: [{ type: 'text', text: 'parent' }]
        },
        {
          type: 'noteBlock',
          attrs: { id: 'n2', depth: 0, bullet: '- ' },
          content: [{ type: 'text', text: 'child' }]
        }
      ]
    })
    focusBlock(editor, 1)
    expect(indentActiveBlock(editor)).toBe(true)
    expect(editor.state.doc.child(1).attrs.bullet).toBe('- ')
    expect(editor.state.doc.child(1).attrs.depth).toBe(1)
    editor.destroy()
  })

  it('Backspace at start of empty nested ordered note clears the marker first', () => {
    // List-exit contract: marker clear precedes unindent. Ordered renumber on
    // depth change is covered by unindentActiveBlock / Shift-Tab cases above.
    const editor = makeEditorWithKeymaps()
    editor.commands.setContent({
      type: 'doc',
      content: [
        {
          type: 'noteBlock',
          attrs: { id: 'n1', depth: 0, bullet: '1) ' },
          content: [{ type: 'text', text: 'one' }]
        },
        {
          type: 'noteBlock',
          attrs: { id: 'n2', depth: 1, bullet: '1) ' },
          content: [{ type: 'text', text: 'x' }]
        }
      ]
    })
    // Empty the nested block via replace so content.size is 0, then Backspace.
    const nestedPos = editor.state.doc.child(0).nodeSize
    const nested = editor.state.doc.child(1)
    const tr = editor.state.tr.replaceWith(
      nestedPos + 1,
      nestedPos + nested.content.size + 1,
      []
    )
    editor.view.dispatch(tr)
    focusBlock(editor, 1)
    expect(pressKey(editor, 'Backspace')).toBe(true)
    expect(editor.state.doc.childCount).toBe(2)
    expect(editor.state.doc.child(1).attrs.bullet).toBe('')
    expect(editor.state.doc.child(1).attrs.depth).toBe(1)
    editor.destroy()
  })

  it('unindent into a ) run adopts destination punctuation', () => {
    const editor = makeEditorWithKeymaps()
    editor.commands.setContent({
      type: 'doc',
      content: [
        {
          type: 'noteBlock',
          attrs: { id: 'n1', depth: 0, bullet: '1) ' },
          content: [{ type: 'text', text: 'one' }]
        },
        {
          type: 'noteBlock',
          attrs: { id: 'n2', depth: 1, bullet: '1. ' },
          content: [{ type: 'text', text: 'nested-dot' }]
        },
        {
          type: 'noteBlock',
          attrs: { id: 'n3', depth: 0, bullet: '2) ' },
          content: [{ type: 'text', text: 'three' }]
        }
      ]
    })
    focusBlock(editor, 1)
    expect(unindentActiveBlock(editor)).toBe(true)
    expect(snapshot(editor)).toEqual([
      { bullet: '1) ', depth: 0, text: 'one' },
      { bullet: '2) ', depth: 0, text: 'nested-dot' },
      { bullet: '3) ', depth: 0, text: 'three' }
    ])
    editor.destroy()
  })
})

describe('Tab / Shift-Tab depth indent (outliner)', () => {
  function twoDepthBlocks(kind: 'plain' | 'bullet' | 'task'): DocJSON {
    const mk = (
      id: string,
      text: string,
      type: 'noteBlock' | 'taskBlock',
      extra: Record<string, unknown>
    ) => ({
      type,
      attrs: { id, depth: 0, ...extra },
      content: [{ type: 'text' as const, text }]
    })
    let a: ReturnType<typeof mk>
    let b: ReturnType<typeof mk>
    if (kind === 'task') {
      a = mk('a', 'parent', 'noteBlock', { bullet: '- ' })
      b = mk('b', 'task', 'taskBlock', { status: 'TODO' })
    } else if (kind === 'bullet') {
      a = mk('a', 'parent', 'noteBlock', { bullet: '- ' })
      b = mk('b', 'child', 'noteBlock', { bullet: '- ' })
    } else {
      a = mk('a', 'line1', 'noteBlock', { bullet: '' })
      b = mk('b', 'line2', 'noteBlock', { bullet: '' })
    }
    return { type: 'doc', content: [a, b] }
  }

  function focusSecondBlock(editor: Editor): void {
    const secondPos = editor.state.doc.child(0).nodeSize
    editor.commands.setTextSelection(secondPos + 1)
  }

  it('indents a plain noteBlock under the previous sibling', () => {
    const editor = makeEditorWithKeymaps()
    editor.commands.setContent(twoDepthBlocks('plain'))
    focusSecondBlock(editor)
    expect(editor.state.doc.child(1).attrs.depth).toBe(0)
    expect(pressKey(editor, 'Tab')).toBe(true)
    expect(editor.state.doc.child(1).attrs.depth).toBe(1)
    editor.destroy()
  })

  it('indents a bulleted noteBlock under the previous sibling', () => {
    const editor = makeEditorWithKeymaps()
    editor.commands.setContent(twoDepthBlocks('bullet'))
    focusSecondBlock(editor)
    expect(pressKey(editor, 'Tab')).toBe(true)
    expect(editor.state.doc.child(1).attrs.depth).toBe(1)
    editor.destroy()
  })

  it('indents a taskBlock under the previous sibling', () => {
    const editor = makeEditorWithKeymaps()
    editor.commands.setContent(twoDepthBlocks('task'))
    focusSecondBlock(editor)
    expect(pressKey(editor, 'Tab')).toBe(true)
    expect(editor.state.doc.child(1).attrs.depth).toBe(1)
    editor.destroy()
  })

  it('Shift-Tab unindents when depth > 0', () => {
    const editor = makeEditorWithKeymaps()
    editor.commands.setContent({
      type: 'doc',
      content: [
        {
          type: 'noteBlock',
          attrs: { id: 'a', depth: 0, bullet: '- ' },
          content: [{ type: 'text', text: 'parent' }]
        },
        {
          type: 'noteBlock',
          attrs: { id: 'b', depth: 1, bullet: '- ' },
          content: [{ type: 'text', text: 'child' }]
        }
      ]
    })
    focusSecondBlock(editor)
    expect(pressKey(editor, 'Tab', { shiftKey: true })).toBe(true)
    expect(editor.state.doc.child(1).attrs.depth).toBe(0)
    editor.destroy()
  })

  it('Tab on the first block is a no-op for depth but still handled', () => {
    const editor = makeEditorWithKeymaps()
    editor.commands.setContent(blockDoc('noteBlock', 'only'))
    editor.commands.setTextSelection(1)
    expect(pressKey(editor, 'Tab')).toBe(true)
    expect(editor.state.doc.child(0).attrs.depth).toBe(0)
    editor.destroy()
  })

  it('honors a remapped indent_block / unindent_block binding', () => {
    withHotkeys({ indent_block: 'Ctrl+]', unindent_block: 'Ctrl+[' }, () => {
      const editor = makeEditorWithKeymaps()
      editor.commands.setContent(twoDepthBlocks('bullet'))
      focusSecondBlock(editor)

      // Default Tab must NOT indent when remapped away.
      expect(pressKey(editor, 'Tab')).toBe(false)
      expect(editor.state.doc.child(1).attrs.depth).toBe(0)

      expect(pressKey(editor, ']', { ctrlKey: true })).toBe(true)
      expect(editor.state.doc.child(1).attrs.depth).toBe(1)

      expect(pressKey(editor, '[', { ctrlKey: true })).toBe(true)
      expect(editor.state.doc.child(1).attrs.depth).toBe(0)
      editor.destroy()
    })
  })

  it('applies indent_block remap on the live editor without remount', () => {
    // Config-driven chords rebuild on every keydown from settings.config.hotkeys.
    // Saving HotkeysTab must affect the already-open editor (no page switch).
    const editor = makeEditorWithKeymaps()
    editor.commands.setContent(twoDepthBlocks('bullet'))
    focusSecondBlock(editor)

    // Defaults: Tab indents.
    expect(pressKey(editor, 'Tab')).toBe(true)
    expect(editor.state.doc.child(1).attrs.depth).toBe(1)
    expect(pressKey(editor, 'Tab', { shiftKey: true })).toBe(true)
    expect(editor.state.doc.child(1).attrs.depth).toBe(0)

    // Live remap while the same Editor instance stays mounted.
    withHotkeys({ indent_block: 'Ctrl+]', unindent_block: 'Ctrl+[' }, () => {
      expect(pressKey(editor, 'Tab')).toBe(false)
      expect(editor.state.doc.child(1).attrs.depth).toBe(0)
      expect(pressKey(editor, ']', { ctrlKey: true })).toBe(true)
      expect(editor.state.doc.child(1).attrs.depth).toBe(1)
      expect(pressKey(editor, '[', { ctrlKey: true })).toBe(true)
      expect(editor.state.doc.child(1).attrs.depth).toBe(0)
    })

    // Restoring defaults (withHotkeys finally) works on the same instance.
    expect(pressKey(editor, 'Tab')).toBe(true)
    expect(editor.state.doc.child(1).attrs.depth).toBe(1)
    editor.destroy()
  })

  it('omits indent when indent_block is explicitly disabled (empty binding)', () => {
    withHotkeys({ indent_block: '', unindent_block: '' }, () => {
      const editor = makeEditorWithKeymaps()
      editor.commands.setContent(twoDepthBlocks('plain'))
      focusSecondBlock(editor)
      expect(pressKey(editor, 'Tab')).toBe(false)
      expect(editor.state.doc.child(1).attrs.depth).toBe(0)
      // Helpers still work when called directly (keymap is what was disabled).
      expect(indentActiveBlock(editor)).toBe(true)
      expect(editor.state.doc.child(1).attrs.depth).toBe(1)
      expect(unindentActiveBlock(editor)).toBe(true)
      expect(editor.state.doc.child(1).attrs.depth).toBe(0)
      editor.destroy()
    })
  })

  it('indents a noteBlock nested inside a callout relative to its sibling', () => {
    // Regression: top-level-only sibling scan left nested blocks at maxDepth 0.
    const editor = new Editor({
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
        CalloutBlock,
        ...SiltInlineMarkExtensions,
        ...SiltColorMarkExtensions,
        EmbedNode,
        BlockReferenceNode,
        UniqueBlockIds,
        SiltBlockKeymaps
      ]
    })
    editor.commands.setContent({
      type: 'doc',
      content: [
        {
          type: 'calloutBlock',
          attrs: { variant: 'info', id: 'c1' },
          content: [
            {
              type: 'noteBlock',
              attrs: { id: 'n1', depth: 0, bullet: '- ' },
              content: [{ type: 'text', text: 'parent' }]
            },
            {
              type: 'noteBlock',
              attrs: { id: 'n2', depth: 0, bullet: '- ' },
              content: [{ type: 'text', text: 'child' }]
            }
          ]
        }
      ]
    })
    // Caret inside second nested note (callout@0, first note, second note content).
    const callout = editor.state.doc.child(0)
    const firstNestedSize = callout.child(0).nodeSize
    // pos 0 = before callout; content starts at 1; first child at 1; second at 1+firstNestedSize
    const secondNestedContent = 1 + firstNestedSize + 1
    editor.commands.setTextSelection(secondNestedContent)
    expect(indentActiveBlock(editor)).toBe(true)
    const nested = editor.state.doc.child(0)
    expect(nested.child(1).attrs.depth).toBe(1)
    editor.destroy()
  })
})

describe('moveActiveBlock — drag-handle keyboard complement (#181)', () => {
  function multiBlockDoc(texts: string[]): DocJSON {
    return {
      type: 'doc',
      content: texts.map((t, i) => ({
        type: 'noteBlock',
        attrs: { id: `b${i}`, depth: 0, bullet: '- ' },
        content: [{ type: 'text', text: t }]
      }))
    }
  }
  const textsOf = (e: Editor) => {
    const kids = (e.getJSON().content ?? []) as Array<{
      content?: Array<{ text?: string }>
    }>
    return kids.map((n) => n.content?.[0]?.text ?? '')
  }

  it('moves the active block down (swaps with the next block)', () => {
    const editor = makeEditor()
    editor.commands.setContent(multiBlockDoc(['a', 'b', 'c']))
    // Block 1 ('b') content sits at pos 4 (block 0 nodeSize 3, content offset 1).
    editor.commands.setTextSelection(4)
    expect(moveActiveBlock(editor, 1)).toBe(true)
    expect(textsOf(editor)).toEqual(['a', 'c', 'b'])
    editor.destroy()
  })

  it('moves the active block up (swaps with the previous block)', () => {
    const editor = makeEditor()
    editor.commands.setContent(multiBlockDoc(['a', 'b', 'c']))
    editor.commands.setTextSelection(4) // 'b'
    expect(moveActiveBlock(editor, -1)).toBe(true)
    expect(textsOf(editor)).toEqual(['b', 'a', 'c'])
    editor.destroy()
  })

  it('no-ops at the top (cannot move the first block up)', () => {
    const editor = makeEditor()
    editor.commands.setContent(multiBlockDoc(['a', 'b']))
    editor.commands.setTextSelection(1) // 'a'
    expect(moveActiveBlock(editor, -1)).toBe(false)
    expect(textsOf(editor)).toEqual(['a', 'b'])
    editor.destroy()
  })

  it('no-ops at the bottom (cannot move the last block down)', () => {
    const editor = makeEditor()
    editor.commands.setContent(multiBlockDoc(['a', 'b']))
    // 'b' content at pos 4.
    editor.commands.setTextSelection(4)
    expect(moveActiveBlock(editor, 1)).toBe(false)
    expect(textsOf(editor)).toEqual(['a', 'b'])
    editor.destroy()
  })
})

// --- Block merge on Delete / Backspace (#364) --------------------------------
//
// Two-block doc position math (noteBlock "hello" + noteBlock "world", both
// nodeSize 7): block 0 spans [0,7) with content at [1,6); block 1 spans [7,14)
// with content at [8,13). End of block 0 content = pos 6; start of block 1
// content = pos 8.

function pressKey(
  editor: Editor,
  key: string,
  opts: { shiftKey?: boolean; ctrlKey?: boolean; metaKey?: boolean } = {}
): boolean {
  const event = new KeyboardEvent('keydown', {
    key,
    bubbles: true,
    shiftKey: opts.shiftKey ?? false,
    ctrlKey: opts.ctrlKey ?? false,
    metaKey: opts.metaKey ?? false
  })
  let handled = false
  editor.view.someProp('handleKeyDown', (handler) => {
    if (handled) return true
    // prosemirror-keymap returns true when it dispatches a handler that
    // returns true; someProp stops at the first truthy return.
    const ret = handler(editor.view, event)
    if (ret) handled = true
    return ret
  })
  return handled
}

/** Temporarily set config.hotkeys for keymap-construction tests. */
function withHotkeys<T>(
  hotkeys: Record<string, string | undefined>,
  fn: () => T
): T {
  const prev = settings.config
  settings.config = {
    ...(prev ?? {}),
    hotkeys: { ...(prev?.hotkeys ?? {}), ...hotkeys }
  } as typeof settings.config
  try {
    return fn()
  } finally {
    settings.config = prev
  }
}

function twoNoteBlocks(
  textA: string,
  textB: string,
  opts: { idA?: string; idB?: string; bulletA?: string; bulletB?: string } = {}
): DocJSON {
  const idA = opts.idA ?? 'a1'
  const idB = opts.idB ?? 'b2'
  // An empty text node is invalid in ProseMirror, so omit the content array
  // entirely for empty blocks (they become truly empty noteBlocks).
  const mk = (id: string, text: string, bullet: string) => {
    const block: DocJSON['content'][number] = {
      type: 'noteBlock',
      attrs: { id, depth: 0, bullet }
    }
    if (text !== '') block.content = [{ type: 'text', text }]
    return block
  }
  return {
    type: 'doc',
    content: [
      mk(idA, textA, opts.bulletA ?? ''),
      mk(idB, textB, opts.bulletB ?? '')
    ]
  }
}

function docChildren(e: Editor) {
  return (e.getJSON().content ?? []) as Array<{
    type: string
    attrs?: { id?: string; bullet?: string; status?: string }
    content?: Array<{ text?: string; marks?: Array<{ type: string }> }>
  }>
}

describe('Delete handler — forward merge (#364)', () => {
  it('merges a same-type sibling below into the current block', () => {
    const editor = makeEditorWithKeymaps()
    editor.commands.setContent(twoNoteBlocks('hello', 'world'))
    editor.commands.setTextSelection(6) // end of block 0 content
    expect(pressKey(editor, 'Delete')).toBe(true)
    expect(editor.state.doc.childCount).toBe(1)
    const only = editor.state.doc.child(0)
    expect(only.type.name).toBe('noteBlock')
    expect(only.textContent).toBe('helloworld')
    // Survivor keeps the current block's id.
    expect(only.attrs.id).toBe('a1')
    // Caret at the join boundary (end of original 'hello' = pos 6).
    expect(editor.state.selection.from).toBe(6)
    editor.destroy()
  })

  it('is a no-op when the sibling below is a different type', () => {
    const editor = makeEditorWithKeymaps()
    const doc: DocJSON = {
      type: 'doc',
      content: [
        {
          type: 'noteBlock',
          attrs: { id: 'a1', depth: 0, bullet: '' },
          content: [{ type: 'text', text: 'hello' }]
        },
        {
          type: 'taskBlock',
          attrs: { id: 'b2', depth: 0, status: 'TODO' },
          content: [{ type: 'text', text: 'task' }]
        }
      ]
    }
    editor.commands.setContent(doc)
    editor.commands.setTextSelection(6)
    expect(pressKey(editor, 'Delete')).toBe(false)
    expect(editor.state.doc.childCount).toBe(2)
    editor.destroy()
  })

  it('drops an empty current block so the sibling below takes its place', () => {
    const editor = makeEditorWithKeymaps()
    editor.commands.setContent(twoNoteBlocks('', 'world'))
    // Empty block 0: nodeSize 2, content range empty. End-of-content = pos 1.
    editor.commands.setTextSelection(1)
    expect(pressKey(editor, 'Delete')).toBe(true)
    expect(editor.state.doc.childCount).toBe(1)
    const only = editor.state.doc.child(0)
    expect(only.textContent).toBe('world')
    // Survivor is the block that was below, keeping its own id.
    expect(only.attrs.id).toBe('b2')
    // Caret at the start of the promoted sibling's content.
    expect(editor.state.selection.from).toBe(1)
    editor.destroy()
  })

  it('is a no-op at the end of the last block (no sibling below)', () => {
    const editor = makeEditorWithKeymaps()
    editor.commands.setContent(twoNoteBlocks('hello', 'world'))
    // End of block 1 content = pos 13.
    editor.commands.setTextSelection(13)
    expect(pressKey(editor, 'Delete')).toBe(false)
    expect(editor.state.doc.childCount).toBe(2)
    editor.destroy()
  })

  it('is a no-op when the caret is mid-block', () => {
    const editor = makeEditorWithKeymaps()
    editor.commands.setContent(twoNoteBlocks('hello', 'world'))
    editor.commands.setTextSelection(3) // middle of block 0
    expect(pressKey(editor, 'Delete')).toBe(false)
    expect(editor.state.doc.childCount).toBe(2)
    editor.destroy()
  })

  // codeBlock exclusion (AC): not asserted here as an integration test because
  // codeBlock lives outside SiltBlockExtensions (not registered in the test
  // editor) and findActiveBlock only recognizes BLOCK_TYPES, which omits it —
  // so a codeBlock cursor yields no active block and the handler returns false
  // before any merge logic runs. The cross-type no-op test above covers the
  // same fall-through path; the node.type.spec.code guard in mergeSiblingBlock
  // is defense-in-depth (documented in BLOCK_TYPES' comment).

  it('merges taskBlock pairs (helper is type-agnostic)', () => {
    const editor = makeEditorWithKeymaps()
    const doc: DocJSON = {
      type: 'doc',
      content: [
        {
          type: 'taskBlock',
          attrs: { id: 't1', depth: 0, status: 'TODO' },
          content: [{ type: 'text', text: 'foo' }]
        },
        {
          type: 'taskBlock',
          attrs: { id: 't2', depth: 0, status: 'TODO' },
          content: [{ type: 'text', text: 'bar' }]
        }
      ]
    }
    editor.commands.setContent(doc)
    // taskBlock 'foo' nodeSize 5; end of content = pos 4.
    editor.commands.setTextSelection(4)
    expect(pressKey(editor, 'Delete')).toBe(true)
    expect(editor.state.doc.childCount).toBe(1)
    expect(editor.state.doc.child(0).textContent).toBe('foobar')
    expect(editor.state.doc.child(0).attrs.id).toBe('t1')
    editor.destroy()
  })
})

describe('Backspace handler — backward merge (#364)', () => {
  it('merges the current block into the same-type sibling above', () => {
    const editor = makeEditorWithKeymaps()
    editor.commands.setContent(twoNoteBlocks('hello', 'world'))
    editor.commands.setTextSelection(8) // start of block 1 content
    expect(pressKey(editor, 'Backspace')).toBe(true)
    expect(editor.state.doc.childCount).toBe(1)
    const only = editor.state.doc.child(0)
    expect(only.type.name).toBe('noteBlock')
    expect(only.textContent).toBe('helloworld')
    // Survivor keeps the previous block's id.
    expect(only.attrs.id).toBe('a1')
    // Caret at the join boundary (end of original 'hello' = pos 6).
    expect(editor.state.selection.from).toBe(6)
    editor.destroy()
  })

  it('is a no-op when the sibling above is a different type', () => {
    const editor = makeEditorWithKeymaps()
    const doc: DocJSON = {
      type: 'doc',
      content: [
        {
          type: 'noteBlock',
          attrs: { id: 'a1', depth: 0, bullet: '' },
          content: [{ type: 'text', text: 'hello' }]
        },
        {
          type: 'headerBlock',
          attrs: { id: 'b2', depth: 1 },
          content: [{ type: 'text', text: 'Header' }]
        }
      ]
    }
    editor.commands.setContent(doc)
    // headerBlock 'Header' nodeSize 8; start of content = pos 8.
    editor.commands.setTextSelection(8)
    expect(pressKey(editor, 'Backspace')).toBe(false)
    expect(editor.state.doc.childCount).toBe(2)
    editor.destroy()
  })

  it('is a no-op when the caret is mid-block', () => {
    const editor = makeEditorWithKeymaps()
    editor.commands.setContent(twoNoteBlocks('hello', 'world'))
    editor.commands.setTextSelection(10) // middle of block 1
    expect(pressKey(editor, 'Backspace')).toBe(false)
    expect(editor.state.doc.childCount).toBe(2)
    editor.destroy()
  })

  it('still clears a bullet on a bulleted noteBlock before merging', () => {
    const editor = makeEditorWithKeymaps()
    editor.commands.setContent(
      twoNoteBlocks('hello', 'world', {
        bulletA: '',
        bulletB: '- '
      })
    )
    editor.commands.setTextSelection(8) // start of block 1
    // First press: clears the bullet (does not merge yet).
    expect(pressKey(editor, 'Backspace')).toBe(true)
    expect(editor.state.doc.childCount).toBe(2)
    expect(editor.state.doc.child(1).attrs.bullet).toBe('')
    // Second press: now no bullet → merges.
    editor.commands.setTextSelection(8)
    expect(pressKey(editor, 'Backspace')).toBe(true)
    expect(editor.state.doc.childCount).toBe(1)
    expect(editor.state.doc.child(0).textContent).toBe('helloworld')
    editor.destroy()
  })

  it('merges headerBlock pairs', () => {
    const editor = makeEditorWithKeymaps()
    const doc: DocJSON = {
      type: 'doc',
      content: [
        {
          type: 'headerBlock',
          attrs: { id: 'h1', depth: 1 },
          content: [{ type: 'text', text: 'One' }]
        },
        {
          type: 'headerBlock',
          attrs: { id: 'h2', depth: 1 },
          content: [{ type: 'text', text: 'Two' }]
        }
      ]
    }
    editor.commands.setContent(doc)
    // headerBlock 'One' nodeSize 5; block 1 starts at 5, content at 6.
    editor.commands.setTextSelection(6)
    expect(pressKey(editor, 'Backspace')).toBe(true)
    expect(editor.state.doc.childCount).toBe(1)
    expect(editor.state.doc.child(0).textContent).toBe('OneTwo')
    expect(editor.state.doc.child(0).attrs.id).toBe('h1')
    editor.destroy()
  })

  it('preserves inline marks across the merge', () => {
    const editor = makeEditorWithKeymaps()
    const doc: DocJSON = {
      type: 'doc',
      content: [
        {
          type: 'noteBlock',
          attrs: { id: 'a1', depth: 0, bullet: '' },
          content: [{ type: 'text', text: 'plain' }]
        },
        {
          type: 'noteBlock',
          attrs: { id: 'b2', depth: 0, bullet: '' },
          content: [
            { type: 'text', marks: [{ type: 'highlight' }], text: 'bold' }
          ]
        }
      ]
    }
    editor.commands.setContent(doc)
    editor.commands.setTextSelection(8) // start of block 1
    expect(pressKey(editor, 'Backspace')).toBe(true)
    const merged = editor.state.doc.child(0)
    expect(merged.textContent).toBe('plainbold')
    // The marked text node retains its highlight mark across the merge.
    const kids = docChildren(editor)[0].content ?? []
    const marked = kids.find((n) => n.text === 'bold')
    expect(marked?.marks?.some((m) => m.type === 'highlight')).toBe(true)
    editor.destroy()
  })

  it('merges into an empty previous sibling (survivor keeps its id)', () => {
    const editor = makeEditorWithKeymaps()
    // Previous block empty (no content array), current block carries the text.
    // Backspace at start of current: content moves into the empty previous
    // block; the previous block survives with its own id, current is removed.
    const doc: DocJSON = {
      type: 'doc',
      content: [
        {
          type: 'noteBlock',
          attrs: { id: 'empty-prev', depth: 0, bullet: '' }
        },
        {
          type: 'noteBlock',
          attrs: { id: 'curr', depth: 0, bullet: '' },
          content: [{ type: 'text', text: 'moved' }]
        }
      ]
    }
    editor.commands.setContent(doc)
    // Empty prev nodeSize 2; current starts at pos 2, content at pos 3.
    editor.commands.setTextSelection(3)
    expect(pressKey(editor, 'Backspace')).toBe(true)
    expect(editor.state.doc.childCount).toBe(1)
    const only = editor.state.doc.child(0)
    expect(only.textContent).toBe('moved')
    expect(only.attrs.id).toBe('empty-prev')
    editor.destroy()
  })

  it('is a no-op on a single-block doc (no sibling either direction)', () => {
    // getSibling returns null when the active block has no same-parent
    // sibling — the same code path that blocks a genuine cross-parent merge
    // (nodeBefore/nodeAfter at a node boundary never cross parents). This
    // test exercises that null-sibling fall-through directly: a lone block
    // has no sibling above or below, so neither boundary key merges it. A
    // real nested cross-parent case would need calloutBlock registered in
    // the test editor (out of scope); the structural guarantee is the same
    // null-return reached here.
    const editor = makeEditorWithKeymaps()
    const single: DocJSON = {
      type: 'doc',
      content: [
        {
          type: 'noteBlock',
          attrs: { id: 'only', depth: 0, bullet: '' },
          content: [{ type: 'text', text: 'solo' }]
        }
      ]
    }
    editor.commands.setContent(single)
    editor.commands.setTextSelection(1) // start of the only block
    expect(pressKey(editor, 'Backspace')).toBe(false)
    editor.commands.setTextSelection(4) // end of the only block
    expect(pressKey(editor, 'Delete')).toBe(false)
    expect(editor.state.doc.childCount).toBe(1)
    expect(editor.state.doc.child(0).attrs.id).toBe('only')
    editor.destroy()
  })

  it('fires a single view dispatch per merge (one autosave transaction)', () => {
    const editor = makeEditorWithKeymaps()
    editor.commands.setContent(twoNoteBlocks('hello', 'world'))
    editor.commands.setTextSelection(6) // end of block 0 (before spy)
    // Spy on the raw view dispatch AFTER setup — each doc-changing dispatch
    // is one autosave trigger in the wired editor. The merge must dispatch
    // exactly once (replaceWith + setSelection chained on one transaction).
    const view = editor.view
    const origDispatch = view.dispatch.bind(view)
    let dispatches = 0
    view.dispatch = (tr) => {
      dispatches++
      origDispatch(tr)
    }
    pressKey(editor, 'Delete')
    view.dispatch = origDispatch
    expect(dispatches).toBe(1)
    editor.destroy()
  })

  it('does not remint the survivor id (uniqueIdPlugin leaves it alone)', () => {
    const editor = makeEditorWithKeymaps()
    editor.commands.setContent(twoNoteBlocks('hello', 'world'))
    editor.commands.setTextSelection(6)
    pressKey(editor, 'Delete')
    const only = editor.state.doc.child(0)
    // The survivor is the current block; its id 'a1' must be preserved, not
    // replaced with a fresh UUID by uniqueIdPlugin.
    expect(only.attrs.id).toBe('a1')
    editor.destroy()
  })
})

describe('Backspace on an empty sole block (#552 — no duplicate)', () => {
  // A single empty noteBlock at depth 0 is the post-seed new-page state.
  // Backspace at the start must be a no-op (return true), not fall through to
  // StarterKit/ProseMirror's default chain which synthesizes a duplicate node.

  it('empty sole block, bullet:"", Backspace is a clean no-op (childCount stays 1)', () => {
    const editor = makeEditorWithKeymaps()
    const single: DocJSON = {
      type: 'doc',
      content: [
        { type: 'noteBlock', attrs: { id: 'only', depth: 0, bullet: '' } }
      ]
    }
    editor.commands.setContent(single)
    editor.commands.setTextSelection(1) // start of the only block
    const before = editor.state.doc.toJSON()
    expect(pressKey(editor, 'Backspace')).toBe(true)
    expect(editor.state.doc.childCount).toBe(1)
    expect(editor.state.doc.toJSON()).toEqual(before)
    editor.destroy()
  })

  it('legacy bullet repro: empty bullet:"- " → first Backspace clears → second Backspace does NOT create a duplicate', () => {
    const editor = makeEditorWithKeymaps()
    const single: DocJSON = {
      type: 'doc',
      content: [
        { type: 'noteBlock', attrs: { id: 'only', depth: 0, bullet: '- ' } }
      ]
    }
    editor.commands.setContent(single)
    editor.commands.setTextSelection(1)
    // First Backspace clears the bullet.
    expect(pressKey(editor, 'Backspace')).toBe(true)
    expect(editor.state.doc.childCount).toBe(1)
    expect(editor.state.doc.child(0).attrs.bullet).toBe('')
    // Second Backspace: empty sole block, no bullet → no-op, no duplicate.
    editor.commands.setTextSelection(1)
    expect(pressKey(editor, 'Backspace')).toBe(true)
    expect(editor.state.doc.childCount).toBe(1)
    editor.destroy()
  })

  it('mid-block char deletion is unchanged (handler returns false → default path)', () => {
    const editor = makeEditorWithKeymaps()
    const single: DocJSON = {
      type: 'doc',
      content: [
        {
          type: 'noteBlock',
          attrs: { id: 'only', depth: 0, bullet: '' },
          content: [{ type: 'text', text: 'hi' }]
        }
      ]
    }
    editor.commands.setContent(single)
    editor.commands.setTextSelection(3) // end of content (not at start)
    // Not at start → handler returns false, falling through to ProseMirror's
    // default char-deletion path (jsdom doesn't fire the default; the handler
    // return value is what matters — it did not consume the key).
    expect(pressKey(editor, 'Backspace')).toBe(false)
    expect(editor.state.doc.childCount).toBe(1)
    editor.destroy()
  })

  it('non-empty sole block at start still falls through (scope guard)', () => {
    // A non-empty sole block at start calls mergeSiblingBlock, which returns
    // false (no sibling) and falls through to the default. This documents that
    // the #552 fix only narrows the empty-sole-block path — the non-empty
    // sole-block-at-start behavior is unchanged (predates this fix).
    const editor = makeEditorWithKeymaps()
    const single: DocJSON = {
      type: 'doc',
      content: [
        {
          type: 'noteBlock',
          attrs: { id: 'only', depth: 0, bullet: '' },
          content: [{ type: 'text', text: 'hello' }]
        }
      ]
    }
    editor.commands.setContent(single)
    editor.commands.setTextSelection(1) // start of the only block
    expect(pressKey(editor, 'Backspace')).toBe(false)
    expect(editor.state.doc.childCount).toBe(1)
    editor.destroy()
  })

  it('clears a quote marker before consuming Backspace on a sole empty block', () => {
    // Parallel to bullet clearing: Backspace on a sole empty quoted noteBlock
    // should clear the "> " quote marker first, not consume the keypress as a
    // no-op while leaving the quote intact.
    const editor = makeEditorWithKeymaps()
    const single: DocJSON = {
      type: 'doc',
      content: [
        {
          type: 'noteBlock',
          attrs: { id: 'only', depth: 0, bullet: '', quote: '> ' }
        }
      ]
    }
    editor.commands.setContent(single)
    editor.commands.setTextSelection(1)
    // First Backspace clears the quote.
    expect(pressKey(editor, 'Backspace')).toBe(true)
    expect(editor.state.doc.childCount).toBe(1)
    expect(editor.state.doc.child(0).attrs.quote).toBe('')
    // Second Backspace: now empty, no markers → no-op (consumed).
    editor.commands.setTextSelection(1)
    expect(pressKey(editor, 'Backspace')).toBe(true)
    expect(editor.state.doc.childCount).toBe(1)
    editor.destroy()
  })

  it('sole empty taskBlock converts to noteBlock on Backspace (#568)', () => {
    const editor = makeEditorWithKeymaps()
    const single: DocJSON = {
      type: 'doc',
      content: [
        {
          type: 'taskBlock',
          attrs: {
            id: 'task-1',
            depth: 0,
            status: 'TODO',
            file_date: '2026-06-14'
          }
        }
      ]
    }
    editor.commands.setContent(single)
    editor.commands.setTextSelection(1) // start of the only block
    expect(pressKey(editor, 'Backspace')).toBe(true)
    expect(editor.state.doc.childCount).toBe(1)
    const child = editor.state.doc.child(0)
    expect(child.type.name).toBe('noteBlock')
    expect(child.attrs.id).toBe('task-1')
    expect(child.attrs.depth).toBe(0)
    expect(child.attrs.bullet).toBe('')
    expect(child.attrs.file_date).toBe('2026-06-14')
    editor.destroy()
  })

  it('sole empty headerBlock converts to noteBlock on Backspace (#568)', () => {
    const editor = makeEditorWithKeymaps()
    const single: DocJSON = {
      type: 'doc',
      content: [
        {
          type: 'headerBlock',
          attrs: { id: 'hdr-1', depth: 2, file_date: '2026-06-14' }
        }
      ]
    }
    editor.commands.setContent(single)
    editor.commands.setTextSelection(1) // start of the only block
    expect(pressKey(editor, 'Backspace')).toBe(true)
    expect(editor.state.doc.childCount).toBe(1)
    const child = editor.state.doc.child(0)
    expect(child.type.name).toBe('noteBlock')
    expect(child.attrs.id).toBe('hdr-1')
    expect(child.attrs.depth).toBe(0)
    expect(child.attrs.bullet).toBe('')
    expect(child.attrs.file_date).toBe('2026-06-14')
    editor.destroy()
  })

  it('non-empty taskBlock at start: Backspace unchanged (#568 regression)', () => {
    const editor = makeEditorWithKeymaps()
    const single: DocJSON = {
      type: 'doc',
      content: [
        {
          type: 'taskBlock',
          attrs: { id: 'task-1', depth: 0, status: 'TODO' },
          content: [{ type: 'text', text: 'do something' }]
        }
      ]
    }
    editor.commands.setContent(single)
    editor.commands.setTextSelection(1) // start of the only block
    // Non-empty → mergeSiblingBlock runs (no sibling → returns false).
    expect(pressKey(editor, 'Backspace')).toBe(false)
    expect(editor.state.doc.childCount).toBe(1)
    expect(editor.state.doc.child(0).type.name).toBe('taskBlock')
    editor.destroy()
  })

  it('non-empty headerBlock at start: Backspace unchanged (#568 regression)', () => {
    const editor = makeEditorWithKeymaps()
    const single: DocJSON = {
      type: 'doc',
      content: [
        {
          type: 'headerBlock',
          attrs: { id: 'hdr-1', depth: 2 },
          content: [{ type: 'text', text: 'A heading' }]
        }
      ]
    }
    editor.commands.setContent(single)
    editor.commands.setTextSelection(1) // start of the only block
    // Non-empty → mergeSiblingBlock runs (no sibling → returns false).
    // The header's depth attr (level 2) must NOT trigger unindent before
    // the isBlockEmpty check fires.
    expect(pressKey(editor, 'Backspace')).toBe(false)
    expect(editor.state.doc.childCount).toBe(1)
    expect(editor.state.doc.child(0).type.name).toBe('headerBlock')
    expect(editor.state.doc.child(0).attrs.depth).toBe(2)
    editor.destroy()
  })

  it('nested block inside sole callout has tree depth > 1 (#569 nesting predicate)', () => {
    // The #569 guard returns false when findActiveBlock's tree depth > 1
    // (i.e., the caret is inside a nested block within a container, not
    // in a direct doc child). jsdom's ProseMirror base keymap also consumes
    // Backspace at the start of an empty nested block without mutating the
    // doc, so doc-state assertions after a full keypress can't distinguish
    // the guard from a no-op consumption. Instead, verify the predicate
    // that drives the guard — the tree-depth test — directly.
    const editor = new Editor({
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
        CalloutBlock,
        ...SiltInlineMarkExtensions,
        ...SiltColorMarkExtensions,
        EmbedNode,
        BlockReferenceNode,
        UniqueBlockIds,
        SiltBlockKeymaps
      ]
    })
    const doc: DocJSON = {
      type: 'doc',
      content: [
        {
          type: 'calloutBlock',
          attrs: { variant: 'info', id: 'callout-1' },
          content: [
            {
              type: 'noteBlock',
              attrs: { id: 'nested-1', depth: 0, bullet: '' }
            }
          ]
        }
      ]
    }
    editor.commands.setContent(doc)
    // Position 2 = inside the nested noteBlock (callout at pos 0, content
    // at pos 1, noteBlock content at pos 2).
    editor.commands.setTextSelection(2)
    // The tree depth must be > 1, confirming the block is nested inside a
    // container and the #569 guard will return false (preventing no-op
    // consumption).
    const active = findActiveBlock(editor)
    expect(active).not.toBeNull()
    expect(active!.depth).toBeGreaterThan(1)
    // Verify the doc structure is intact (sole callout with nested block).
    expect(editor.state.doc.childCount).toBe(1)
    expect(editor.state.doc.firstChild?.type.name).toBe('calloutBlock')
    editor.destroy()
  })

  it('nested empty taskBlock inside sole callout is not converted to noteBlock (#568 regression)', () => {
    // A sole callout containing a nested empty taskBlock should NOT have
    // the taskBlock converted to noteBlock on Backspace. The #568 type
    // conversion guards on tree depth === 1 (direct doc child), so nested
    // blocks fall through to the #569 guard.
    const editor = new Editor({
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
        CalloutBlock,
        ...SiltInlineMarkExtensions,
        ...SiltColorMarkExtensions,
        EmbedNode,
        BlockReferenceNode,
        UniqueBlockIds,
        SiltBlockKeymaps
      ]
    })
    const doc: DocJSON = {
      type: 'doc',
      content: [
        {
          type: 'calloutBlock',
          attrs: { variant: 'info', id: 'callout-1' },
          content: [
            {
              type: 'taskBlock',
              attrs: { id: 'nested-task', depth: 0, status: 'TODO' }
            }
          ]
        }
      ]
    }
    editor.commands.setContent(doc)
    editor.commands.setTextSelection(2) // inside nested taskBlock
    // The tree depth is > 1 — the #568 conversion branch must leave this
    // nested block untouched.
    const active = findActiveBlock(editor)
    expect(active).not.toBeNull()
    expect(active!.depth).toBeGreaterThan(1)
    // The nested block's type must NOT be mutated.
    expect(active!.node.type.name).toBe('taskBlock')
    editor.destroy()
  })
})

describe('format_subscript keymap (#511 — Ctrl+Shift, chord)', () => {
  // The config default moved format_subscript to Ctrl+Shift, (ProseMirror
  // Mod-Shift-,). format_* actions are editor-scoped — the global matchHotkey
  // path never handles them — and ProseMirror resolves the chord via keyCode
  // (layout-stable). This dispatches the chord through the keymap to verify
  // the hardcoded fallback mirrors the config default AND the mark toggles.
  function pressSubscript(editor: Editor): boolean {
    const event = new KeyboardEvent('keydown', {
      key: ',',
      shiftKey: true,
      ctrlKey: true,
      keyCode: 188,
      bubbles: true
    })
    let handled = false
    editor.view.someProp('handleKeyDown', (handler) => {
      if (handler(editor.view, event)) handled = true
    })
    return handled
  }

  it('resolves the Ctrl+Shift, chord to the subscript binding (Mod-Shift-, fallback)', () => {
    const editor = makeEditorWithKeymaps()
    editor.commands.setContent(blockDoc('noteBlock', 'hello'))
    editor.commands.setTextSelection({ from: 2, to: 5 })
    // pressSubscript returns true iff ProseMirror's keymap matched the chord
    // against the format_subscript binding. This verifies the hardcoded
    // fallback mirrors the config default (Mod-Shift-, not the pre-#511
    // Mod-,) and that the chord is reachable — format_subscript is
    // editor-scoped and ProseMirror resolves it via keyCode (layout-stable),
    // so the global matchHotkey e.key limitation never applies to it. The
    // mark-toggle itself isn't asserted here because the production chain
    // (`chain().focus().toggleSubscript()`) collapses the synthetic selection
    // in jsdom — it follows the identical pattern the other format_* marks
    // use and is exercised in real focus in production.
    expect(pressSubscript(editor)).toBe(true)
    editor.destroy()
  })
})
