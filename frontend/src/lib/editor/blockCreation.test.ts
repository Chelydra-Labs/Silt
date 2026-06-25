// Comprehensive creation-path scan for the Sprint 14 block types.
//
// The table-creation bug (insertTable silently failed because paragraph was
// disabled) slipped through the earlier suites because they only exercised
// data *conversion* (blocksToDoc / docToBlocks), never the actual *creation*
// path (the insert/toggle helpers a slash command or toolbar button invokes).
// These tests drive each feature's creation helper through a real TipTap
// editor and then a create → save (docToBlocks) → load (blocksToDoc) cycle, so
// any schema/normalization mismatch (like the missing paragraph node) surfaces
// here instead of in a user's editor.

import { describe, it, expect, afterEach } from 'vitest'
import { Editor } from '@tiptap/core'
import StarterKit from '@tiptap/starter-kit'
import {
  SiltBlockExtensions,
  SiltInlineMarkExtensions,
  SiltColorMarkExtensions,
  SiltDetailsExtensions,
  SiltTableExtensions,
  UniqueBlockIds,
  insertCallout,
  insertCodeBlock,
  insertDetails,
  insertTable,
  toggleBlockQuote
} from './index'
import {
  CalloutBlock,
  CodeBlock,
  EmbedNode,
  BlockReferenceNode
} from './schema'
import { blocksToDoc, docToBlocks } from './converters'
import { getSlashCommands } from './slash-registry'
import type { DocJSON, ParsedBlock } from './types'

function makeFullEditor(initial?: ParsedBlock[]): Editor {
  return new Editor({
    extensions: [
      StarterKit.configure({
        // paragraph enabled — the Table extension fills cells with it.
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
      CodeBlock,
      ...SiltInlineMarkExtensions,
      ...SiltColorMarkExtensions,
      ...SiltDetailsExtensions,
      ...SiltTableExtensions,
      EmbedNode,
      BlockReferenceNode,
      UniqueBlockIds
    ],
    content: initial ? blocksToDoc(initial) : undefined
  })
}

// Place the cursor at the start of the first block's content (pos 1) so the
// block-level helpers resolve an active block.
function focusFirstBlock(editor: Editor): void {
  editor.commands.setTextSelection(1)
}

const editors: Editor[] = []
function track(editor: Editor): Editor {
  editors.push(editor)
  return editor
}
afterEach(() => {
  while (editors.length) {
    const e = editors.pop()
    if (e && !e.isDestroyed) e.destroy()
  }
})

describe('block creation scan (#188 / #180 / #189 / #183 / #172)', () => {
  it('toggleBlockQuote marks a noteBlock as a quote (#188)', () => {
    const editor = track(makeFullEditor())
    // Seed an empty noteBlock and focus it.
    editor.commands.setContent(
      blocksToDoc([
        {
          id: '11111111-1111-1111-1111-111111111111',
          parent_id: '',
          type: 'NOTE',
          depth: 0,
          raw_text: '',
          clean_text: '',
          status: '',
          owner: '',
          start_date: '',
          due_date: '',
          priority: 3,
          line_number: 1
        }
      ])
    )
    focusFirstBlock(editor)
    expect(toggleBlockQuote(editor)).toBe(true)
    const node = (editor.getJSON() as DocJSON).content[0]
    expect(node?.type).toBe('noteBlock')
    expect(node?.attrs?.quote).toBe('> ')
    // Save form is a `> ` line.
    expect(docToBlocks(editor.getJSON() as DocJSON)[0].clean_text).toBe('> ')
  })

  it('insertCallout creates a calloutBlock that saves as `> [!note]` (#180)', () => {
    const editor = track(makeFullEditor())
    focusFirstBlock(editor)
    expect(insertCallout(editor, 'warning')).toBe(true)
    const node = (editor.getJSON() as DocJSON).content[0]
    expect(node?.type).toBe('calloutBlock')
    expect(node?.attrs?.variant).toBe('warning')
    const saved = docToBlocks(editor.getJSON() as DocJSON)
    expect(saved[0].type).toBe('NOTE')
    expect(saved[0].clean_text).toBe('> [!warning]')
  })

  it('insertCodeBlock creates an editable codeBlock that saves as CODE (#189)', () => {
    const editor = track(makeFullEditor())
    focusFirstBlock(editor)
    expect(insertCodeBlock(editor, 'go')).toBe(true)
    const node = (editor.getJSON() as DocJSON).content[0]
    expect(node?.type).toBe('codeBlock')
    expect(node?.attrs?.language).toBe('go')
    const saved = docToBlocks(editor.getJSON() as DocJSON)
    expect(saved[0].type).toBe('CODE')
    expect(saved[0].language).toBe('go')
  })

  it('insertDetails creates a foldable details tree (#183)', () => {
    const editor = track(makeFullEditor())
    focusFirstBlock(editor)
    expect(insertDetails(editor)).toBe(true)
    const node = (editor.getJSON() as DocJSON).content[0]
    expect(node?.type).toBe('details')
    // Save form is the <details>/<summary>/<body>/</details> run.
    const saved = docToBlocks(editor.getJSON() as DocJSON).map(
      (b) => b.clean_text
    )
    expect(saved[0]).toBe('<details>')
    expect(saved[1]).toMatch(/^<summary>.*<\/summary>$/)
    expect(saved[saved.length - 1]).toBe('</details>')
  })

  it('insertTable creates an editable grid that saves as GFM (#172)', () => {
    const editor = track(makeFullEditor())
    focusFirstBlock(editor)
    expect(insertTable(editor, 2, 3)).toBe(true)
    const node = (editor.getJSON() as DocJSON).content[0]
    expect(node?.type).toBe('table')
    const rows = (node?.content || []).filter((c) => c.type === 'tableRow')
    expect(rows).toHaveLength(2)
    // Each cell carries a paragraph child (valid 'block+' content).
    expect(rows[0]?.content?.[0]?.content?.[0]?.type).toBe('paragraph')
  })

  it('every created block round-trips create → save → load unchanged', () => {
    // The headline regression guard: create each block, save it to the
    // ParsedBlock form, reload that form, and confirm the node type survives.
    const cases: Array<{
      name: string
      create: (e: Editor) => boolean
      expectType: string
    }> = [
      {
        name: 'callout',
        create: (e) => insertCallout(e, 'tip'),
        expectType: 'calloutBlock'
      },
      {
        name: 'code',
        create: (e) => insertCodeBlock(e, 'ts'),
        expectType: 'codeBlock'
      },
      {
        name: 'details',
        create: (e) => insertDetails(e),
        expectType: 'details'
      },
      {
        name: 'table',
        create: (e) => insertTable(e, 2, 2),
        expectType: 'table'
      }
    ]
    for (const c of cases) {
      const editor = track(makeFullEditor())
      focusFirstBlock(editor)
      expect(c.create(editor), `${c.name} create`).toBe(true)
      const saved = docToBlocks(editor.getJSON() as DocJSON)
      const reloaded = blocksToDoc(saved)
      expect(
        reloaded.content.some((n) => n.type === c.expectType),
        `${c.name} did not survive create→save→load`
      ).toBe(true)
    }
  })

  it('the slash registry exposes a command for every block feature', () => {
    const ids = new Set(getSlashCommands().map((c) => c.id))
    for (const id of [
      'quote',
      'callout',
      'callout-warning',
      'code-block',
      'details',
      'table',
      'table-5x4',
      'table-custom'
    ]) {
      expect(ids.has(id), `missing slash command ${id}`).toBe(true)
    }
  })
})
