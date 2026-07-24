// Regression: outer NodeView roots must carry schema HTML attrs (data-depth,
// data-type, …) so `.ProseMirror > div[data-depth='N']` indent CSS and related
// direct-child chrome selectors match. #339 removed the Svelte $effect that
// lifted data-depth onto the PM root; without SvelteNodeViewRenderer `attrs`,
// depth lives only on the inner [data-node-view-wrapper] and Tab indent is
// invisible even though node.attrs.depth updates correctly.

import { describe, it, expect, vi } from 'vitest'

const mocks = vi.hoisted(() => ({
  resolveBlockReference: vi.fn(),
  resolvePageLink: vi.fn(),
  pluginMutateBlock: vi.fn(),
  fetchPageBlocks: vi.fn(),
  saveFileBlocks: vi.fn(),
  acquireFocusLock: vi.fn(),
  refreshFocusLock: vi.fn(),
  releaseFocusLock: vi.fn()
}))

vi.mock('../../../bindings/silt/app.js', () => ({
  ResolveBlockReference: mocks.resolveBlockReference,
  ResolvePageLink: mocks.resolvePageLink,
  PluginMutateBlock: mocks.pluginMutateBlock,
  FetchPageBlocks: mocks.fetchPageBlocks,
  SaveFileBlocks: mocks.saveFileBlocks,
  AcquireFocusLock: mocks.acquireFocusLock,
  RefreshFocusLock: mocks.refreshFocusLock,
  ReleaseFocusLock: mocks.releaseFocusLock
}))

vi.mock('@wailsio/runtime', () => ({
  Events: { On: vi.fn(() => () => {}) },
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
    Nullable: (fn: unknown) => fn,
    Array: () => [],
    Map: () => ({}),
    Any: {}
  }
}))

import { Editor } from '@tiptap/core'
import StarterKit from '@tiptap/starter-kit'
import {
  SiltBlockExtensionsWithNodeViews,
  UniqueBlockIds,
  SiltBlockKeymaps
} from './index'
import {
  mountNodeViewEditor,
  mkBlock,
  createContainer
} from './nodeview-test-harness'

function pressTab(editor: Editor, shift = false): boolean {
  const event = new KeyboardEvent('keydown', {
    key: 'Tab',
    bubbles: true,
    shiftKey: shift,
    cancelable: true
  })
  let handled = false
  editor.view.someProp('handleKeyDown', (handler) => {
    if (handled) return true
    const ret = handler(editor.view, event)
    if (ret) handled = true
    return ret
  })
  return handled
}

function topLevelBlockRoots(pm: Element): HTMLElement[] {
  return Array.from(pm.children).filter(
    (el): el is HTMLElement =>
      el instanceof HTMLElement &&
      (el.classList.contains('node-noteBlock') ||
        el.classList.contains('node-taskBlock') ||
        el.classList.contains('node-headerBlock'))
  )
}

describe('block depth DOM contract (NodeView outer attrs)', () => {
  it('puts data-depth and data-type on ProseMirror direct children', async () => {
    const idA = 'aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee'
    const idB = '11111111-2222-4333-8444-555555555555'
    const blocks = [
      mkBlock('NOTE', {
        id: idA,
        clean_text: 'parent',
        depth: 0,
        raw_text: '- parent'
      }),
      mkBlock('NOTE', {
        id: idB,
        clean_text: 'child',
        depth: 2,
        raw_text: '- child'
      })
    ]
    const { container, cleanup } = await mountNodeViewEditor(blocks)
    const pm = container.querySelector('.ProseMirror')
    expect(pm).toBeTruthy()

    const roots = topLevelBlockRoots(pm!)
    expect(roots).toHaveLength(2)
    expect(roots[0].getAttribute('data-depth')).toBe('0')
    expect(roots[0].getAttribute('data-type')).toBe('note')
    expect(roots[0].getAttribute('data-id')).toBe(idA)
    expect(roots[1].getAttribute('data-depth')).toBe('2')
    expect(roots[1].getAttribute('data-type')).toBe('note')
    expect(roots[1].getAttribute('data-id')).toBe(idB)

    // CSS contract: indent rules use `.ProseMirror > div[data-depth='N']`.
    expect(pm!.querySelectorAll(':scope > [data-depth]').length).toBe(2)
    expect(pm!.querySelectorAll(':scope > [data-type="note"]').length).toBe(2)

    cleanup()
  })

  it('puts data-type/data-depth on task and header outer roots', async () => {
    const blocks = [
      mkBlock('HEADER', { clean_text: 'Title', depth: 2 }),
      mkBlock('TASK', {
        clean_text: 'Do it',
        depth: 1,
        status: 'DONE'
      })
    ]
    const { container, cleanup } = await mountNodeViewEditor(blocks)
    const pm = container.querySelector('.ProseMirror')!
    const header = pm.querySelector(':scope > .node-headerBlock')
    const task = pm.querySelector(':scope > .node-taskBlock')

    expect(header?.getAttribute('data-type')).toBe('header')
    expect(header?.getAttribute('data-depth')).toBe('2')
    expect(task?.getAttribute('data-type')).toBe('task')
    expect(task?.getAttribute('data-depth')).toBe('1')
    expect(task?.getAttribute('data-status')).toBe('DONE')

    cleanup()
  })

  it('updates outer data-depth when Tab indents a block', async () => {
    // Full stack: NodeViews + keymaps so Tab mutates depth and the outer
    // root attrs refresh on the same transaction path production uses.
    const { container, cleanup: removeContainer } = createContainer()
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
          trailingNode: false
        }),
        ...SiltBlockExtensionsWithNodeViews,
        UniqueBlockIds,
        SiltBlockKeymaps
      ],
      content: {
        type: 'doc',
        content: [
          {
            type: 'noteBlock',
            attrs: { id: 'a', depth: 0, bullet: '- ' },
            content: [{ type: 'text', text: 'parent' }]
          },
          {
            type: 'noteBlock',
            attrs: { id: 'b', depth: 0, bullet: '- ' },
            content: [{ type: 'text', text: 'child' }]
          }
        ]
      }
    })
    await new Promise((r) => setTimeout(r, 0))

    const secondPos = editor.state.doc.child(0).nodeSize
    editor.commands.setTextSelection(secondPos + 1)
    expect(pressTab(editor)).toBe(true)
    expect(editor.state.doc.child(1).attrs.depth).toBe(1)

    // Drain microtasks so NodeView update + updateElementAttributes settle.
    await new Promise((r) => setTimeout(r, 0))

    const pm = container.querySelector('.ProseMirror')!
    const roots = topLevelBlockRoots(pm)
    expect(roots).toHaveLength(2)
    expect(roots[1].getAttribute('data-depth')).toBe('1')

    // Shift-Tab must refresh the outer root too (same updateElementAttributes path).
    expect(pressTab(editor, true)).toBe(true)
    expect(editor.state.doc.child(1).attrs.depth).toBe(0)
    await new Promise((r) => setTimeout(r, 0))
    expect(topLevelBlockRoots(pm)[1].getAttribute('data-depth')).toBe('0')

    editor.destroy()
    removeContainer()
  })
})
