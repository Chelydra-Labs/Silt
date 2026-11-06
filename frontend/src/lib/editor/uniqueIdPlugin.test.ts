import { describe, it, expect } from 'vitest'
import { Editor } from '@tiptap/core'
import StarterKit from '@tiptap/starter-kit'
import {
  SiltBlockExtensions,
  SiltInlineMarkExtensions,
  SiltColorMarkExtensions,
  UniqueBlockIds,
  SiltHardBreak
} from './index'
import { docToBlocks } from './converters'
import { EmbedNode, BlockReferenceNode } from './schema'
import type { DocJSON } from './types'

// Real TipTap editor wired with the Silt schema — same makeEditor pattern as
// keymaps.test.ts (no Placeholder, avoiding the jsdom elementFromPoint gap).
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

/** Two blocks sharing one id — the shape a hand-edited file produces on load. */
function duplicateIdDoc(id: string): DocJSON {
  return {
    type: 'doc',
    content: [
      {
        type: 'taskBlock',
        attrs: { id, depth: 0, status: 'TODO' },
        content: [{ type: 'text', text: 'original' }]
      },
      {
        type: 'taskBlock',
        attrs: { id, depth: 0, status: 'TODO' },
        content: [{ type: 'text', text: 'the duplicate copy' }]
      }
    ]
  }
}

function topLevelIds(editor: Editor): (string | null)[] {
  const ids: (string | null)[] = []
  editor.state.doc.forEach((child) => {
    ids.push((child.attrs as Record<string, unknown>).id as string | null)
  })
  return ids
}

describe('uniqueIdPlugin — within-document duplicate repair', () => {
  it('a duplicate id from a hand-edited file is repaired on the next edit: the second block receives a fresh id, the first keeps it', () => {
    const editor = makeEditor()
    const shared = '11111111-1111-1111-1111-111111111111'
    editor.commands.setContent(duplicateIdDoc(shared))

    const ids = topLevelIds(editor)
    expect(ids[0]).toBe(shared)
    expect(ids[1]).toBeTruthy()
    expect(ids[1]).not.toBe(shared)
    editor.destroy()
  })

  it('a block with a null id (freshly typed) gets one on the next transaction', () => {
    const editor = makeEditor()
    const doc: DocJSON = {
      type: 'doc',
      content: [
        {
          type: 'taskBlock',
          attrs: { id: null, depth: 0, status: 'TODO' },
          content: [{ type: 'text', text: 'typed before the plugin ran' }]
        }
      ]
    }
    editor.commands.setContent(doc)
    expect(topLevelIds(editor)[0]).toBeTruthy()
    editor.destroy()
  })

  it('the repaired document saves without duplicate ids — docToBlocks emits a PK-safe block list', () => {
    const editor = makeEditor()
    const shared = '22222222-2222-2222-2222-222222222222'
    editor.commands.setContent(duplicateIdDoc(shared))

    const blocks = docToBlocks(editor.getJSON())
    const ids = blocks.map((b) => b.id)
    expect(ids).toHaveLength(2)
    expect(new Set(ids).size).toBe(2)
    // The block that keeps the original id stays addressable by existing
    // ((uuid)) references; only the copy is re-identified.
    expect(ids[0]).toBe(shared)
    editor.destroy()
  })

  it('an already-clean document is untouched (no repair transaction)', () => {
    const editor = makeEditor()
    const doc: DocJSON = {
      type: 'doc',
      content: [
        {
          type: 'taskBlock',
          attrs: {
            id: 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa',
            depth: 0,
            status: 'TODO'
          },
          content: [{ type: 'text', text: 'fine' }]
        },
        {
          type: 'noteBlock',
          attrs: {
            id: 'bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb',
            depth: 0,
            bullet: '- '
          },
          content: [{ type: 'text', text: 'also fine' }]
        }
      ]
    }
    editor.commands.setContent(doc)
    const ids = topLevelIds(editor)
    expect(ids).toEqual([
      'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa',
      'bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb'
    ])
    editor.destroy()
  })
})
