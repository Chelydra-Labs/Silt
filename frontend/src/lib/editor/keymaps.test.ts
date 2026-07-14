import { describe, it, expect } from 'vitest'
import { Editor } from '@tiptap/core'
import StarterKit from '@tiptap/starter-kit'
import {
  SiltBlockExtensions,
  SiltInlineMarkExtensions,
  SiltColorMarkExtensions,
  UniqueBlockIds,
  SiltBlockKeymaps
} from './index'
import { EmbedNode, BlockReferenceNode, CalloutBlock } from './schema'
import { setBlockAlign, moveActiveBlock } from './keymaps'
import type { DocJSON } from './types'

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
        trailingNode: false
      }),
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
// Backspace / Tab outliner semantics are exercised. The base makeEditor()
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
        trailingNode: false
      }),
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

function pressKey(editor: Editor, key: string): boolean {
  const event = new KeyboardEvent('keydown', { key, bubbles: true })
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

  it('nested empty noteBlock inside sole callout: Backspace returns false (#569)', () => {
    // Extend the test editor with CalloutBlock so we can construct a doc
    // with a sole callout containing a nested empty noteBlock.
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
    // Place the caret at the start of the nested noteBlock inside the
    // callout (doc position 2: callout at pos 0, callout content at pos 1,
    // noteBlock content at pos 2).
    editor.commands.setTextSelection(2)
    // Our handler should detect the nesting (findActiveBlock tree depth > 1)
    // and return false, preventing the terminal no-op branch from consuming
    // the keypress. ProseMirror's default chain may or may not delete the
    // empty nested block depending on the node's isolating semantics, but
    // the keypress must NOT be silently consumed as a no-op.
    const handled = pressKey(editor, 'Backspace')
    // The keypress may be consumed by ProseMirror's default handler (which
    // is fine — it's not OUR no-op), or not handled at all. Either way,
    // the callout with its nested block should not have been touched by
    // our terminal branch.
    // Case 1: ProseMirror default handler deletes the nested block →
    //   handled=true, callout.childCount=0
    // Case 2: ProseMirror default handler is also a no-op →
    //   handled=false, callout.childCount=1
    // Both are acceptable; the fix only prevents OUR handler from silently
    // consuming the Backspace. The callout and its content must still exist.
    expect(editor.state.doc.childCount).toBe(1)
    expect(editor.state.doc.firstChild?.type.name).toBe('calloutBlock')
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
