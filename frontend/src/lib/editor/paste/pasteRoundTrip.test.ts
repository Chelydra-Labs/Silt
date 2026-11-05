// PLAN §6 round-trip: a pasted fixture must survive the save/reload path.
// `view.pasteHTML` exercises the normalizer + schema; `docToBlocks` is what
// SaveFileBlocks writes and `blocksToDoc` is what the next load parses. If a
// marker the normalizer emits cannot be represented in the block model (e.g.
// a lettered list marker), the second conversion silently drops it — this loop
// is the guard for that class of bug.
//
// Pattern mirrors converters.test.ts: compare the semantic fields of the two
// block lists. `raw_text` is included here (unlike converters.test.ts) because
// for pasted content it is what carries the bullet/quote marker on disk — a
// marker the model cannot re-detect disappears from raw_text on reload.
// `parent_id`/`line_number` are excluded as they are derived from depth/order.

import { afterEach, describe, expect, it } from 'vitest'
import { Editor } from '@tiptap/core'
import StarterKit from '@tiptap/starter-kit'
import type { JSONContent } from '@tiptap/core'
import {
  ExternalPasteNormalizer,
  SiltBlockExtensions,
  SiltColorMarkExtensions,
  SiltDetailsExtensions,
  SiltHardBreak,
  SiltInlineMarkExtensions,
  SiltTableExtensions,
  UniqueBlockIds,
  blocksToDoc,
  docToBlocks
} from '../index'
import { CodeBlock, siltPasteProps } from '../schema'
import type { DocJSON, ParsedBlock } from '../types'
import {
  BLOCKQUOTE_BODY,
  CODE_BLOCK,
  CSS_ONLY_EMPHASIS,
  GENUINE_TABLE,
  GOOGLE_DOCS_BODY,
  LAYOUT_TABLE_BODY,
  LETTERED_MSO_LIST,
  MSO_PSEUDO_LIST,
  NESTED_ORDERED_LIST,
  OUTLOOK_BODY,
  PRESENTATION_TABLE_2CELL,
  SHADED_CONTAINER,
  SILT_INTERNAL_CLIPBOARD
} from './fixtures'

const FIXTURES: [string, string][] = [
  ['Outlook body', OUTLOOK_BODY],
  ['nested ordered list', NESTED_ORDERED_LIST],
  ['layout table', LAYOUT_TABLE_BODY],
  ['mso pseudo list', MSO_PSEUDO_LIST],
  ['lettered mso list', LETTERED_MSO_LIST],
  ['CSS-only emphasis', CSS_ONLY_EMPHASIS],
  ['blockquote', BLOCKQUOTE_BODY],
  ['code block', CODE_BLOCK],
  ['shaded container', SHADED_CONTAINER],
  ['genuine table', GENUINE_TABLE],
  ['Google Docs body', GOOGLE_DOCS_BODY],
  ['two-cell presentation table', PRESENTATION_TABLE_2CELL],
  ['internal clipboard', SILT_INTERNAL_CLIPBOARD]
]

const SEMANTIC_FIELDS = [
  'id',
  'type',
  'depth',
  'status',
  'owner',
  'start_date',
  'due_date',
  'priority',
  'raw_text',
  'clean_text',
  'file_date'
] as const

function semantic(block: ParsedBlock): Record<string, unknown> {
  return Object.fromEntries(
    SEMANTIC_FIELDS.map((field) => [field, block[field]])
  )
}

const editors: Editor[] = []

function makeEditor(): Editor {
  const editor = new Editor({
    extensions: [
      StarterKit.configure({
        heading: false,
        bulletList: false,
        orderedList: false,
        listItem: false,
        blockquote: false,
        codeBlock: false,
        horizontalRule: false,
        trailingNode: false,
        hardBreak: false,
        link: { openOnClick: false, autolink: true }
      }),
      SiltHardBreak,
      ...SiltBlockExtensions,
      CodeBlock,
      ...SiltInlineMarkExtensions,
      ...SiltColorMarkExtensions,
      ...SiltDetailsExtensions,
      ...SiltTableExtensions,
      ExternalPasteNormalizer,
      UniqueBlockIds
    ],
    editorProps: siltPasteProps
  })
  editors.push(editor)
  return editor
}

afterEach(() => {
  while (editors.length) {
    const editor = editors.pop()
    if (editor && !editor.isDestroyed) editor.destroy()
  }
})

function paste(editor: Editor, html: string): JSONContent[] {
  editor.commands.selectAll()
  editor.view.pasteHTML(html)
  return editor.getJSON().content ?? []
}

function pastedBlocks(editor: Editor, html: string): ParsedBlock[] {
  return docToBlocks({ type: 'doc', content: paste(editor, html) } as DocJSON)
}

describe('external paste markdown round-trip', () => {
  it.each(FIXTURES)(
    '%s survives paste → docToBlocks → blocksToDoc',
    (_name, fixture) => {
      const pasted = pastedBlocks(makeEditor(), fixture)
      const back = docToBlocks(blocksToDoc(pasted))
      expect(back.map(semantic)).toEqual(pasted.map(semantic))
    }
  )

  it('keeps bullets, quote prefixes and nesting depths after reload', () => {
    const pasted = pastedBlocks(makeEditor(), NESTED_ORDERED_LIST)
    const back = docToBlocks(blocksToDoc(pasted))
    expect(back.map((b) => [b.depth, b.clean_text.split('\n')[0]])).toEqual(
      pasted.map((b) => [b.depth, b.clean_text.split('\n')[0]])
    )
    // The ordered/nested markers are only visible via raw_text's bullet.
    expect(back.map((b) => b.raw_text)).toEqual(pasted.map((b) => b.raw_text))

    const quoted = pastedBlocks(makeEditor(), BLOCKQUOTE_BODY)
    const quotedBack = docToBlocks(blocksToDoc(quoted))
    expect(quotedBack.map((b) => b.clean_text)).toEqual(
      quoted.map((b) => b.clean_text)
    )
    expect(quotedBack.map((b) => b.clean_text)).toEqual([
      '> Quoted first',
      '> Quoted second **with bold**',
      '>> Nested quote'
    ])
  })

  it('keeps inline marks through the markdown round-trip', () => {
    const pasted = pastedBlocks(makeEditor(), SHADED_CONTAINER)
    const back = docToBlocks(blocksToDoc(pasted))
    const find = (blocks: ParsedBlock[], needle: string) =>
      blocks.find((b) => b.clean_text.includes(needle))?.clean_text
    expect(find(back, 'Highlighted run')).toBe(find(pasted, 'Highlighted run'))
    expect(find(back, 'red run')).toBe(find(pasted, 'red run'))
  })

  it('keeps table rows and columns through the round-trip', () => {
    const pasted = pastedBlocks(makeEditor(), GENUINE_TABLE)
    const back = docToBlocks(blocksToDoc(pasted))
    expect(back).toHaveLength(1)
    expect(back[0].clean_text).toBe(pasted[0].clean_text)
    const rows = back[0].clean_text.split('\n')
    expect(rows).toHaveLength(4)
    expect(rows[3].split('|').map((cell) => cell.trim())).toEqual([
      '',
      '',
      '3',
      '4',
      ''
    ])
  })

  it('keeps lettered markers as a persistable unordered bullet', () => {
    const pasted = pastedBlocks(makeEditor(), LETTERED_MSO_LIST)
    expect(pasted.map((b) => b.raw_text)).toEqual(['- Apple', '- Banana'])
    const back = docToBlocks(blocksToDoc(pasted))
    expect(back.map((b) => b.raw_text)).toEqual(['- Apple', '- Banana'])
  })
})
