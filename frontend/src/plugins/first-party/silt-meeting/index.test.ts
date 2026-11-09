// silt-meeting plugin test (#1105). Pins the /meeting command contract:
// registration metadata, doc-start insertion target (the trigger caret pos
// is ignored), the built node shape, the empty-doc replace path, and
// repeated invocation. The fake editor mirrors the small ProseMirror
// surface insertMeetingSection touches; real-editor serialization and paste
// behavior is covered in converters.test.ts and clipboardPaste.test.ts.
import { describe, expect, it, beforeEach, vi } from 'vitest'

import plugin, {
  buildMeetingSection,
  insertMeetingSection,
  MEETING_FIELDS
} from './index'
import type { PluginContext } from '../../sdk'

// A minimal ProseMirror-shaped node. nodeSize mirrors the real math
// (container: 1 + Σ child sizes + 1; text: its length) so the caret
// position the plugin computes can be asserted exactly, and descendants
// walks with positions relative to the outer node like ProseMirror's.
interface FakeNode {
  type: { name: string }
  attrs: Record<string, unknown>
  content: FakeNode[]
  text?: string
  nodeSize: number
  descendants: (cb: (node: FakeNode, pos: number) => boolean | void) => void
}

// The transaction subset the plugin touches (chained for the replace path).
interface FakeTr {
  replaceWith: (from: number, to: number, node: unknown) => FakeTr
}

function textNode(text: string): FakeNode {
  return {
    type: { name: 'text' },
    attrs: {},
    content: [],
    text,
    nodeSize: text.length,
    descendants: () => {}
  }
}

function fakeSchema() {
  const create =
    (name: string) =>
    (attrs: Record<string, unknown>, content?: FakeNode[]): FakeNode => {
      // NodeType.create accepts a bare node or an array; normalize to array.
      const kids =
        content == null ? [] : Array.isArray(content) ? content : [content]
      const node: FakeNode = {
        type: { name },
        attrs,
        content: kids,
        nodeSize: 0,
        descendants: () => {}
      }
      node.descendants = (cb) => {
        // Mirrors ProseMirror: positions are relative to the node's CONTENT
        // start, so the first child sits at offset 0 and a child's subtree
        // starts one past the child's own position.
        const walk = (n: FakeNode, offset: number): void => {
          let pos = offset
          for (const child of n.content) {
            if (cb(child, pos) !== false) walk(child, pos + 1)
            pos += child.nodeSize
          }
        }
        walk(node, 0)
      }
      node.nodeSize = 1 + node.content.reduce((s, c) => s + c.nodeSize, 0) + 1
      return node
    }
  const nodeType = (name: string) => ({ create: create(name) })
  return {
    nodes: {
      details: nodeType('details'),
      detailsSummary: nodeType('detailsSummary'),
      detailsContent: nodeType('detailsContent'),
      noteBlock: nodeType('noteBlock')
    },
    text: textNode
  }
}

// A top-level doc child: the emptiness check reads content.size and
// textContent, the replace path reads nodeSize.
function docBlock(text: string, typeName = 'noteBlock') {
  return {
    type: { name: typeName },
    attrs: { id: 'seed' },
    content: { size: text.length },
    textContent: text,
    nodeSize: text.length + 2
  }
}

function makeFakeEditor(children: ReturnType<typeof docBlock>[]) {
  const current: unknown[] = [...children]
  const calls = {
    insertContentAt: vi.fn((pos: number, value: unknown) => {
      current.unshift(value)
      return true
    }),
    setTextSelection: vi.fn(),
    focus: vi.fn(),
    replaceWith: vi.fn(),
    dispatch: vi.fn()
  }
  const tr: FakeTr = {
    replaceWith: (from, to, node) => {
      // The only replace path is the empty-doc trigger block at doc start.
      calls.replaceWith(from, to, node)
      expect(from).toBe(0)
      expect(to).toBe(2)
      current.splice(0, 1, node)
      return tr
    }
  }
  const editor = {
    isDestroyed: false,
    state: {
      schema: fakeSchema(),
      doc: {
        get childCount() {
          return current.length
        },
        child: (i: number) => current[i]
      },
      tr
    },
    view: { dispatch: calls.dispatch },
    commands: {
      insertContentAt: calls.insertContentAt,
      setTextSelection: calls.setTextSelection,
      focus: calls.focus
    }
  }
  return { editor, calls, current }
}

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/

function assertMeetingSection(section: FakeNode | null) {
  expect(section).not.toBeNull()
  expect(section!.type.name).toBe('details')
  expect(section!.attrs.open).toBe(true)
  // Top-level id stays null: UniqueBlockIds assigns it after insertion.
  expect(section!.attrs.id).toBeNull()
  const [summary, content] = section!.content
  expect(summary.type.name).toBe('detailsSummary')
  expect(summary.content[0]?.text).toBe('Meeting information')
  expect(content.type.name).toBe('detailsContent')
  expect(content.content.map((f) => f.content[0]?.text)).toEqual([
    ...MEETING_FIELDS
  ])
  for (const field of content.content) {
    expect(field.type.name).toBe('noteBlock')
    expect(field.attrs.depth).toBe(0)
    expect(field.attrs.bullet).toBe('')
    // Nested inside detailsContent, these mint ids up front — the
    // UniqueBlockIds walk only covers top-level blocks.
    expect(String(field.attrs.id)).toMatch(UUID)
  }
}

describe('silt-meeting plugin (#1105)', () => {
  const registerSlashCommand = vi.fn(() => () => {})

  function makeCtx(): PluginContext {
    return { registerSlashCommand } as unknown as PluginContext
  }

  beforeEach(() => {
    registerSlashCommand.mockClear()
  })

  it('exports a valid manifest', () => {
    expect(plugin.manifest.id).toBe('silt-meeting')
    expect(plugin.manifest.name).toBe('Meeting Information')
    expect(plugin.manifest.version).toBe('1.0.0')
    expect(plugin.manifest.capabilities?.['editor-schema']).toBe(true)
  })

  it('registers the /meeting slash command on vault open', () => {
    plugin.onVaultOpen?.(makeCtx())
    expect(registerSlashCommand).toHaveBeenCalledTimes(1)
    const cmd = (registerSlashCommand.mock.calls as unknown[][])[0]?.[0] as {
      id?: string
      label?: string
      description?: string
      onSelect?: (editor: unknown, pos: number) => void
    }
    expect(cmd.id).toBe('meeting')
    expect(cmd.label).toBe('Meeting information')
    expect(cmd.description).toBeTruthy()
    expect(typeof cmd.onSelect).toBe('function')
  })

  it('onSelect prepends at doc start, ignoring the trigger caret pos', () => {
    const { editor, calls } = makeFakeEditor([docBlock('existing body')])
    plugin.onVaultOpen?.(makeCtx())
    const cmd = (registerSlashCommand.mock.calls as unknown[][])[0]?.[0] as {
      onSelect: (editor: unknown, pos: number) => void
    }
    // 42 is the post-trigger-deletion caret — the command targets doc start.
    cmd.onSelect(editor, 42)
    expect(calls.insertContentAt).toHaveBeenCalledTimes(1)
    const [pos, section] = calls.insertContentAt.mock.calls[0] as [
      number,
      FakeNode
    ]
    expect(pos).toBe(0)
    assertMeetingSection(section)
    // Caret parks at the start of the first field line (inside the
    // section): the first field block sits at content-relative 22 (summary
    // spans 0..21), +2 crosses the details' and the block's open tokens.
    expect(calls.setTextSelection).toHaveBeenCalledWith(24)
    expect(calls.focus).toHaveBeenCalled()
  })

  it('replaces the emptied trigger block in a brand-new note', () => {
    const { editor, calls, current } = makeFakeEditor([docBlock('')])
    expect(
      insertMeetingSection(
        editor as unknown as Parameters<typeof insertMeetingSection>[0]
      )
    ).toBe(true)
    expect(calls.replaceWith).toHaveBeenCalledTimes(1)
    const [from, to, section] = calls.replaceWith.mock.calls[0] as [
      number,
      number,
      FakeNode
    ]
    expect(from).toBe(0)
    expect(to).toBe(2) // the empty trigger noteBlock
    assertMeetingSection(section)
    expect(calls.insertContentAt).not.toHaveBeenCalled()
    expect(current).toHaveLength(1)
  })

  it('replaces an emptied header trigger block too (insertDetails parity)', () => {
    // A /h1-style command can leave an empty header as the sole block —
    // keymaps' insertDetails admits noteBlock || headerBlock, and so does
    // the emptiness check here: the section must become the only block,
    // not leave a stray empty heading below itself.
    const { editor, calls, current } = makeFakeEditor([
      docBlock('', 'headerBlock')
    ])
    expect(
      insertMeetingSection(
        editor as unknown as Parameters<typeof insertMeetingSection>[0]
      )
    ).toBe(true)
    expect(calls.replaceWith).toHaveBeenCalledTimes(1)
    assertMeetingSection(calls.replaceWith.mock.calls[0]?.[2] as FakeNode)
    expect(calls.insertContentAt).not.toHaveBeenCalled()
    expect(current).toHaveLength(1)
  })

  it('a second invocation inserts a second section (no dedupe)', () => {
    const { editor, calls, current } = makeFakeEditor([docBlock('body')])
    expect(
      insertMeetingSection(
        editor as unknown as Parameters<typeof insertMeetingSection>[0]
      )
    ).toBe(true)
    expect(
      insertMeetingSection(
        editor as unknown as Parameters<typeof insertMeetingSection>[0]
      )
    ).toBe(true)
    expect(calls.insertContentAt).toHaveBeenCalledTimes(2)
    expect(current).toHaveLength(3)
    expect((current[0] as FakeNode).type.name).toBe('details')
    expect((current[1] as FakeNode).type.name).toBe('details')
    expect((current[2] as ReturnType<typeof docBlock>).textContent).toBe('body')
  })

  it('degrades gracefully when the schema lacks the details extension', () => {
    const { editor, calls } = makeFakeEditor([docBlock('body')])
    delete (editor.state.schema.nodes as { details?: unknown }).details
    expect(
      insertMeetingSection(
        editor as unknown as Parameters<typeof insertMeetingSection>[0]
      )
    ).toBe(false)
    expect(calls.insertContentAt).not.toHaveBeenCalled()
    expect(calls.replaceWith).not.toHaveBeenCalled()
  })

  it('buildMeetingSection stamps today on the section and its fields', () => {
    const section = buildMeetingSection(
      fakeSchema() as never,
      '2026-11-09'
    ) as unknown as FakeNode | null
    expect(section!.attrs.file_date).toBe('2026-11-09')
    for (const field of section!.content[1].content) {
      expect(field.attrs.file_date).toBe('2026-11-09')
    }
  })
})
