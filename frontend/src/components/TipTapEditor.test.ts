// Component-level test for TipTapEditor smart-graph content (#127). Renders
// a live TipTap editor with SiltBlockExtensionsWithNodeViews and verifies
// that {{embed:uuid}} and ((uuid)) content renders via the NodeView pipeline
// (SvelteNodeViewRenderer → EmbedNodeView/BlockReferenceNodeView →
// EmbedPortal/BlockReferenceChip).
//
// This is the test #127 explicitly asked for: a component-level NodeView
// integration test that exercises the full Svelte rendering path inside a
// live TipTap editor instance.

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { waitFor } from '@testing-library/svelte'
import {
  mountNodeViewEditor,
  mkBlock,
  FIXTURE_UUID_A,
  FIXTURE_UUID_B
} from '../lib/editor/nodeview-test-harness'
import { Editor } from '@tiptap/core'
import StarterKit from '@tiptap/starter-kit'
import {
  SiltBlockExtensionsWithNodeViews,
  SiltHardBreak,
  SiltBlockKeymaps,
  UniqueBlockIds,
  blocksToDoc,
  docToBlocks
} from '../lib/editor'

const mocks = vi.hoisted(() => ({
  resolveBlockReference: vi.fn(),
  resolvePageLink: vi.fn(),
  pluginMutateBlock: vi.fn(),
  fetchPageBlocks: vi.fn(),
  saveFileBlocks: vi.fn(),
  acquireFocusLock: vi.fn(),
  refreshFocusLock: vi.fn(),
  releaseFocusLock: vi.fn(),
  queryTagHierarchy: vi.fn().mockResolvedValue([]),
  recordTagUsage: vi.fn().mockResolvedValue(undefined),
  eventsOn: vi.fn(() => () => {})
}))

vi.mock('$silt-app', () =>
  createAppIpcMocks({
    ResolveBlockReference: mocks.resolveBlockReference,
    ResolvePageLink: mocks.resolvePageLink,
    PluginMutateBlock: mocks.pluginMutateBlock,
    FetchPageBlocks: mocks.fetchPageBlocks,
    SaveFileBlocks: mocks.saveFileBlocks,
    AcquireFocusLock: mocks.acquireFocusLock,
    RefreshFocusLock: mocks.refreshFocusLock,
    ReleaseFocusLock: mocks.releaseFocusLock,
    QueryTagHierarchy: mocks.queryTagHierarchy,
    RecordTagUsage: mocks.recordTagUsage
  })
)

vi.mock('@wailsio/runtime', () => ({
  Events: {
    On: mocks.eventsOn
  },
  Call: { ByID: vi.fn(), ByName: vi.fn() },
  CancellablePromise: class {
    then() {
      return this
    }
    catch() {
      return this
    }
    finally() {
      return this
    }
  },
  Create: {
    Nullable: <T>(fn: T) => fn,
    Array: () => [],
    Map: () => ({}),
    Any: {}
  }
}))

describe('TipTapEditor smart-graph content (#127)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mocks.resolveBlockReference.mockResolvedValue({
      exists: true,
      id: FIXTURE_UUID_A,
      notebook: 'Work',
      section: 'Projects',
      page: 'Site',
      file_date: '2026-06-15',
      clean_text: 'embedded content'
    })
  })
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('renders an embedNode NodeView for a sole {{embed:uuid}} block', async () => {
    const blocks = [
      mkBlock('NOTE', { clean_text: `{{embed:${FIXTURE_UUID_A}}}` })
    ]
    const { container, cleanup } = await mountNodeViewEditor(blocks)

    // A sole-embed NOTE becomes a top-level embedNode. The NodeView mounts
    // an EmbedPortal which calls ResolveBlockReference.
    await waitFor(() => {
      expect(mocks.resolveBlockReference).toHaveBeenCalledWith(FIXTURE_UUID_A)
    })

    // The NodeView target gets the class node-embedNode.
    expect(container.querySelector('.node-embedNode')).toBeTruthy()

    cleanup()
  })

  it('renders a blockReferenceNode NodeView for inline ((uuid))', async () => {
    const blocks = [
      mkBlock('NOTE', {
        clean_text: `See ((${FIXTURE_UUID_B})) for details.`
      })
    ]
    const { container, cleanup } = await mountNodeViewEditor(blocks)

    // The inline block reference renders as a NodeView with class
    // node-blockReferenceNode. Its component (BlockReferenceChip) calls
    // ResolveBlockReference.
    await waitFor(() => {
      expect(mocks.resolveBlockReference).toHaveBeenCalledWith(FIXTURE_UUID_B)
    })
    expect(container.querySelector('.node-blockReferenceNode')).toBeTruthy()

    cleanup()
  })

  it('renders both embeds and references in the same block', async () => {
    const blocks = [
      mkBlock('NOTE', {
        clean_text: `Pre {{embed:${FIXTURE_UUID_A}}} mid ((${FIXTURE_UUID_B})) post`
      })
    ]
    const { container, cleanup } = await mountNodeViewEditor(blocks)

    await waitFor(() => {
      expect(mocks.resolveBlockReference).toHaveBeenCalledWith(FIXTURE_UUID_A)
      expect(mocks.resolveBlockReference).toHaveBeenCalledWith(FIXTURE_UUID_B)
    })

    // Both NodeView types should be rendered.
    expect(container.querySelector('.node-embedNode')).toBeTruthy()
    expect(container.querySelector('.node-blockReferenceNode')).toBeTruthy()

    cleanup()
  })

  it('renders a NodeViewWrapper element for each smart-graph node', async () => {
    const blocks = [
      mkBlock('NOTE', {
        clean_text: `{{embed:${FIXTURE_UUID_A}}} and ((${FIXTURE_UUID_B}))`
      })
    ]
    const { container, cleanup } = await mountNodeViewEditor(blocks)

    // Every Svelte NodeView renders inside a [data-node-view-wrapper] element
    // (the NodeViewWrapper component from svelte-tiptap).
    await waitFor(() => {
      const wrappers = container.querySelectorAll('[data-node-view-wrapper]')
      expect(wrappers.length).toBeGreaterThanOrEqual(2)
    })

    cleanup()
  })

  it('renders multiple NoteBlock NodeViews for multiple blocks', async () => {
    const blocks = [
      mkBlock('NOTE', { clean_text: 'first block' }),
      mkBlock('NOTE', { clean_text: 'second block' })
    ]
    const { container, cleanup } = await mountNodeViewEditor(blocks)

    // Each NOTE block gets its own NodeView.
    const noteNodes = container.querySelectorAll('.node-noteBlock')
    expect(noteNodes.length).toBeGreaterThanOrEqual(2)

    cleanup()
  })

  // #593: picking a block from the /embed picker inserts a complete embed
  // portal. The insertion is a pre-built embedNode (the editor has no live
  // input rule that converts raw `{{embed:uuid}}` text), so it must render a
  // live EmbedPortal immediately — not wait for a save+reload. The picker
  // workflow itself (BlockPickerModal → onPick) is covered by that modal's own
  // suite; this asserts the insertion contract handleEmbedPick must satisfy:
  // the PICKED block id lands in the `uuid` attr and round-trips back to
  // `{{embed:<picked-id>}}`, never the node's instance `id` (an id/uuid swap
  // would silently embed the wrong block).
  it('renders a live embedNode when an embedNode is inserted via commands (#593)', async () => {
    // Distinct instance id vs. picked uuid so the round-trip catches a swap.
    const instanceId = crypto.randomUUID()
    const { editor, container, cleanup } = await mountNodeViewEditor([
      mkBlock('NOTE', { clean_text: 'host line' })
    ])

    // Mirror handleEmbedPick: insert an embedNode pointing at FIXTURE_UUID_A.
    editor.commands.insertContent({
      type: 'embedNode',
      attrs: { id: instanceId, uuid: FIXTURE_UUID_A, bullet: '' }
    })

    await waitFor(() => {
      expect(container.querySelector('.node-embedNode')).toBeTruthy()
      expect(mocks.resolveBlockReference).toHaveBeenCalledWith(FIXTURE_UUID_A)
    })

    // The picked uuid — not the node instance id — must serialize into the
    // on-disk token, so the embed targets the block the user chose.
    const serialized = docToBlocks(editor.getJSON())
    const embedBlock = serialized.find((b) => b.clean_text.includes('{{embed:'))
    expect(embedBlock).toBeTruthy()
    expect(embedBlock!.clean_text).toBe(`{{embed:${FIXTURE_UUID_A}}}`)
    expect(embedBlock!.clean_text).not.toContain(instanceId)

    cleanup()
  })
})

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

function makeShiftEnterEditor(content: ReturnType<typeof blocksToDoc>): Editor {
  const container = document.createElement('div')
  document.body.appendChild(container)
  const editor = new Editor({
    element: container,
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
        hardBreak: false
      }),
      SiltHardBreak,
      ...SiltBlockExtensionsWithNodeViews,
      SiltBlockKeymaps,
      UniqueBlockIds
    ],
    content
  })
  // Attach container for cleanup via editor.view.dom parent.
  ;(editor as Editor & { __testContainer?: HTMLElement }).__testContainer =
    container
  return editor
}

function destroyShiftEnterEditor(editor: Editor): void {
  const container = (editor as Editor & { __testContainer?: HTMLElement })
    .__testContainer
  editor.destroy()
  container?.remove()
}

describe('Shift-Enter in taskBlock opens the modal (#781)', () => {
  it('dispatches silt:open-task-editor with the block id', async () => {
    const TASK_ID = 'task-shift-enter-001'
    const editor = makeShiftEnterEditor(
      blocksToDoc([
        mkBlock('TASK', { id: TASK_ID, clean_text: 'a task', status: 'TODO' })
      ])
    )
    await new Promise((r) => setTimeout(r, 0))

    const handler = vi.fn()
    window.addEventListener('silt:open-task-editor', handler)

    editor.commands.focus()
    pressShiftEnter(editor)

    expect(handler).toHaveBeenCalledTimes(1)
    const detail = (handler.mock.calls[0][0] as CustomEvent).detail
    expect(detail).toEqual({ blockId: TASK_ID })

    window.removeEventListener('silt:open-task-editor', handler)
    destroyShiftEnterEditor(editor)
  })

  it('does NOT dispatch silt:open-task-editor inside a noteBlock', async () => {
    const editor = makeShiftEnterEditor(
      blocksToDoc([mkBlock('NOTE', { clean_text: 'a note' })])
    )
    await new Promise((r) => setTimeout(r, 0))

    const handler = vi.fn()
    window.addEventListener('silt:open-task-editor', handler)

    editor.commands.focus()
    pressShiftEnter(editor)

    expect(handler).not.toHaveBeenCalled()

    window.removeEventListener('silt:open-task-editor', handler)
    destroyShiftEnterEditor(editor)
  })

  it('does NOT dispatch silt:open-task-editor inside a TaskSubEditorModal', async () => {
    const TASK_ID = 'task-shift-enter-sub-001'
    const editor = makeShiftEnterEditor(
      blocksToDoc([
        mkBlock('TASK', { id: TASK_ID, clean_text: 'a task', status: 'TODO' })
      ])
    )
    await new Promise((r) => setTimeout(r, 0))

    // Mark this editor as the one rendered inside TaskSubEditorModal — the
    // flag TaskSubEditorModal registers on its host editor.
    ;(editor.storage as unknown as Record<string, unknown>).siltSubEditorHost =
      {
        active: true
      }

    const handler = vi.fn()
    window.addEventListener('silt:open-task-editor', handler)

    editor.commands.focus()
    pressShiftEnter(editor)

    expect(handler).not.toHaveBeenCalled()
    // Soft break instead (#828).
    expect(editor.state.doc.childCount).toBe(1)
    const json = editor.state.doc.child(0).toJSON() as {
      content?: Array<{ type: string }>
    }
    expect((json.content || []).some((c) => c.type === 'hardBreak')).toBe(true)

    window.removeEventListener('silt:open-task-editor', handler)
    destroyShiftEnterEditor(editor)
  })
})

describe('Shift-Enter soft break in noteBlock (#828)', () => {
  it('inserts hardBreak and keeps a single block', async () => {
    const editor = makeShiftEnterEditor(
      blocksToDoc([
        mkBlock('NOTE', { id: 'note-1', clean_text: 'hello world' })
      ])
    )
    await new Promise((r) => setTimeout(r, 0))

    editor.commands.focus('end')
    pressShiftEnter(editor)

    expect(editor.state.doc.childCount).toBe(1)
    const saved = docToBlocks(editor.getJSON())
    expect(saved).toHaveLength(1)
    expect(saved[0].clean_text).toContain('<br>')

    destroyShiftEnterEditor(editor)
  })
})
