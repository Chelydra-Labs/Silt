// Component test for TaskBlockView (#781) — verifies the hover-revealed pencil
// button dispatches silt:open-task-editor with the task block's id.

import { describe, it, expect, beforeEach, vi } from 'vitest'
import {
  mountNodeViewEditor,
  mkBlock
} from '../../lib/editor/nodeview-test-harness'

if (typeof document !== 'undefined' && !document.elementFromPoint) {
  document.elementFromPoint = () => document.body
}
if (
  typeof window !== 'undefined' &&
  window.Range &&
  !Range.prototype.getClientRects
) {
  const zeroRect: DOMRect = {
    x: 0,
    y: 0,
    left: 0,
    top: 0,
    right: 0,
    bottom: 0,
    width: 0,
    height: 0,
    toJSON() {
      return this
    }
  }
  Range.prototype.getClientRects = (() => [
    zeroRect
  ]) as unknown as typeof Range.prototype.getClientRects
  Range.prototype.getBoundingClientRect = () => zeroRect
}

const mocks = vi.hoisted(() => ({
  eventsOn: vi.fn(() => () => {})
}))

vi.mock('$silt-app', () => createAppIpcMocks({}))

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

describe('TaskBlockView pencil button (#781)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('renders the pencil button with aria-label "Open task editor"', async () => {
    const TASK_ID = 'task-abc-123'
    const blocks = [
      mkBlock('TASK', {
        id: TASK_ID,
        clean_text: 'My task',
        status: 'TODO'
      })
    ]
    const { container, cleanup } = await mountNodeViewEditor(blocks)

    const btn = container.querySelector(
      'button[aria-label="Open task editor"]'
    ) as HTMLElement | null
    expect(btn).toBeTruthy()

    cleanup()
  })

  it('clicking the pencil dispatches silt:open-task-editor with the block id', async () => {
    const TASK_ID = 'task-xyz-456'
    const blocks = [
      mkBlock('TASK', {
        id: TASK_ID,
        clean_text: 'Another task',
        status: 'TODO'
      })
    ]
    const { container, cleanup } = await mountNodeViewEditor(blocks)

    const handler = vi.fn()
    window.addEventListener('silt:open-task-editor', handler)

    const btn = container.querySelector(
      'button[aria-label="Open task editor"]'
    ) as HTMLElement
    btn.click()

    expect(handler).toHaveBeenCalledTimes(1)
    const detail = (handler.mock.calls[0][0] as CustomEvent).detail
    expect(detail).toEqual({ blockId: TASK_ID })

    window.removeEventListener('silt:open-task-editor', handler)
    cleanup()
  })
})

// #1005: a conflict-held editor is setEditable(false) — updateAttributes
// dispatches regardless of isEditable, and the visible status flip would be
// destroyed by the armed reload. The checkbox must refuse.
describe('TaskBlockView conflict hold (#1005)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('refuses the status cycle on a non-editable editor', async () => {
    const blocks = [
      mkBlock('TASK', {
        id: 'task-held-1',
        clean_text: 'Held task',
        status: 'TODO'
      })
    ]
    const { editor, container, cleanup } = await mountNodeViewEditor(blocks)
    editor.setEditable(false)

    const btn = container.querySelector(
      'button[aria-label="Mark task as doing"]'
    ) as HTMLElement
    btn.click()
    await new Promise((r) => setTimeout(r, 0))
    expect(editor.getJSON()).toBeTruthy()
    // The attribute dispatch was refused: the doc still carries TODO.
    const json = JSON.stringify(editor.getJSON())
    expect(json).toContain('TODO')
    expect(json).not.toContain('DOING')

    cleanup()
  })
})
