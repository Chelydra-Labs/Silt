import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest'
import { tick } from 'svelte'
import { render, screen, cleanup, fireEvent } from '@testing-library/svelte'

// jsdom polyfills: Svelte 5 transitions call element.animate(); the glassy
// modal uses transition:fly. ProseMirror's scrollToSelection / coordsAtPos
// (fired by setContent + focus) resolve caret rects through
// Range.getClientRects, which jsdom omits — stub it the same way the main
// editor's context-menu test does. elementFromPoint is touched by the
// Placeholder viewport tracker during editor construction. matchMedia is used
// by the responsive sidebar collapse (#780) and is absent from jsdom.
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
if (!Element.prototype.animate) {
  Element.prototype.animate = function () {
    return {
      cancel() {},
      finish() {},
      play() {},
      pause() {},
      reverse() {},
      commitStyles() {},
      addEventListener() {},
      removeEventListener() {},
      dispatchEvent() {
        return true
      },
      onfinish: null,
      oncancel: null,
      onremove: null,
      currentTime: 0,
      startTime: null,
      playbackRate: 1,
      playState: 'finished',
      replaceState: 'active',
      pending: false,
      id: '',
      effect: null,
      timeline: null,
      get finished() {
        return Promise.resolve()
      },
      get ready() {
        return Promise.resolve()
      }
    }
  } as unknown as Element['animate']
}

// Override the global matchMedia polyfill (vitest.setup.ts) with a mutable
// mock so the responsive-collapse test can flip `matches` per test and fire
// change listeners (resize must not clobber sidebarOpen — #826).
const mqlListeners: Array<() => void> = []
const mql = {
  matches: false,
  media: '',
  onchange: null,
  addEventListener: vi.fn((event: string, fn: () => void) => {
    if (event === 'change') mqlListeners.push(fn)
  }),
  removeEventListener: vi.fn((event: string, fn: () => void) => {
    if (event === 'change') {
      const i = mqlListeners.indexOf(fn)
      if (i >= 0) mqlListeners.splice(i, 1)
    }
  }),
  addListener: vi.fn(),
  removeListener: vi.fn(),
  dispatchEvent: vi.fn()
}
window.matchMedia = vi.fn(() => mql)

function fireMqlChange(matches: boolean) {
  mql.matches = matches
  for (const fn of [...mqlListeners]) fn()
}

vi.mock('@wailsio/runtime', () => ({
  Events: {
    On: vi.fn(() => () => {})
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
    Nullable: (fn: unknown) => fn,
    Array: () => [],
    Map: () => ({}),
    Any: {}
  }
}))

import TaskSubEditorModal from './TaskSubEditorModal.svelte'
import type { PluginContext } from '../../../sdk'
import { v2CtxStubs } from '../../../test-helpers'
import { STANDALONE_TASKS_NOTEBOOK } from '../../../../lib/standaloneTasksNav'

const mocks = vi.hoisted(() => ({
  fetchSubtree: vi.fn(),
  saveSubtreeBlocks: vi.fn(),
  sqliteQuery: vi.fn()
}))

function makeTaskRow(
  overrides: Record<string, unknown> = {}
): Record<string, unknown> {
  return {
    id: 'task-1',
    source: 'vault',
    notebook: 'Work',
    section: 'Journal',
    page: 'Daily',
    file_date: '2026-07-01',
    line_number: 1,
    clean_content: 'Ship the feature',
    status: 'TODO',
    owner: '',
    start_date: '',
    due_date: '2026-07-15',
    priority: 2,
    pinned: 0,
    progress: 0,
    recurrence: '',
    comments_count: 0,
    links_count: 0,
    created_at: '',
    completed_at: '',
    manual_order: 0,
    modified_at: '',
    estimate_minutes: null,
    subtask_total: 0,
    subtask_done: 0,
    tags: null,
    blocked_by: null,
    is_blocked: 0,
    ...overrides
  }
}

function makeCtx(overrides: Partial<PluginContext> = {}): PluginContext {
  return {
    activeNotebook: 'Work',
    activeSection: 'Journal',
    activePage: 'Daily',
    today: '2026-07-01',
    ...v2CtxStubs,
    fetchSubtree: mocks.fetchSubtree,
    saveSubtreeBlocks: mocks.saveSubtreeBlocks,
    sqliteQuery: mocks.sqliteQuery,
    // CommentThread (mounted via TaskMetadataSidebar) subscribes to block:changed.
    on: () => () => {},
    ...overrides
  } as PluginContext
}

async function flush() {
  await tick()
  await new Promise((r) => setTimeout(r, 0))
}

const BASE_PROPS = {
  blockId: 'task-1',
  notebook: 'Work',
  section: 'Journal',
  page: 'Daily',
  parentTaskText: 'Ship the feature'
}

describe('TaskSubEditorModal (#304)', () => {
  beforeEach(() => {
    mocks.fetchSubtree.mockReset().mockResolvedValue([])
    mocks.saveSubtreeBlocks.mockReset().mockResolvedValue(true)
    mocks.sqliteQuery
      .mockReset()
      .mockResolvedValue({ rows: [], truncated: false })
    mql.matches = false
  })

  afterEach(() => cleanup())

  it('renders a glassy dialog with the parent task title + breadcrumb', async () => {
    render(TaskSubEditorModal, {
      ...BASE_PROPS,
      ctx: makeCtx(),

      onClose: () => {}
    })
    await flush()

    const dialog = screen.getByRole('dialog')
    expect(dialog).toHaveAttribute('aria-modal', 'true')
    // The title is the parent task text.
    expect(dialog).toHaveTextContent('Ship the feature')
    // The breadcrumb carries notebook › section › page.
    expect(dialog).toHaveTextContent('Work')
    expect(dialog).toHaveTextContent('Journal')
    expect(dialog).toHaveTextContent('Daily')
  })

  it('omits breadcrumb and does not leak the synthetic .silt path for standalone tasks', async () => {
    render(TaskSubEditorModal, {
      ...BASE_PROPS,
      notebook: STANDALONE_TASKS_NOTEBOOK,
      section: '',
      page: 'tasks.md',
      ctx: makeCtx(),

      onClose: () => {}
    })
    await flush()

    const dialog = screen.getByRole('dialog')
    // No redundant label; synthetic notebook name + page file must not leak.
    expect(dialog).not.toHaveTextContent('Standalone task')
    expect(dialog).not.toHaveTextContent('.silt')
    expect(dialog).not.toHaveTextContent('tasks.md')
    // Header still shows the parent task title.
    expect(dialog).toHaveTextContent('Ship the feature')
  })

  it('mounts the TipTap editor once the subtree loads (loading state clears)', async () => {
    mocks.fetchSubtree.mockResolvedValue([
      {
        id: 'child-1',
        parent_id: 'task-1',
        type: 'NOTE',
        depth: 1,
        raw_text: '- a sub-note',
        clean_text: 'a sub-note',
        line_number: 2,
        file_date: '2026-07-01'
      }
    ])
    render(TaskSubEditorModal, {
      ...BASE_PROPS,
      ctx: makeCtx(),

      onClose: () => {}
    })
    // The mocked fetch resolves immediately, so the loading state is
    // transient; assert it clears and the editor (ProseMirror content) takes
    // its place. waitFor tolerates the brief loading window.
    await vi.waitFor(() =>
      expect(screen.queryByText('Loading sub-notes…')).toBeNull()
    )
    // The TipTap editor renders a .ProseMirror element once mounted.
    await vi.waitFor(() => {
      expect(document.querySelector('.ProseMirror')).not.toBeNull()
    })
  })

  it('normalizes subtree block depths relative to sub-editor root (starts at depth 0)', async () => {
    mocks.fetchSubtree.mockResolvedValue([
      {
        id: 'child-1',
        parent_id: 'task-1',
        type: 'NOTE',
        depth: 3,
        raw_text: '- indented sub-note',
        clean_text: 'indented sub-note',
        line_number: 5,
        file_date: '2026-07-01'
      }
    ])
    render(TaskSubEditorModal, {
      ...BASE_PROPS,
      ctx: makeCtx(),
      onClose: () => {}
    })
    await vi.waitFor(() => {
      const pm = document.querySelector('.ProseMirror')
      expect(pm).not.toBeNull()
      const note = pm?.querySelector('[data-type="note"]')
      expect(note).not.toBeNull()
      expect(note?.getAttribute('data-depth')).toBe('0')
    })
  })

  it('normalizes sibling depths even when one block has undefined depth', async () => {
    // A single malformed block with depth:undefined must not force minDepth to
    // 0 and suppress normalization for its siblings. minDepth is computed over
    // defined depths only, so the depth:3 sibling still rebases to 0.
    mocks.fetchSubtree.mockResolvedValue([
      {
        id: 'child-1',
        parent_id: 'task-1',
        type: 'NOTE',
        depth: 3,
        raw_text: '- indented sub-note',
        clean_text: 'indented sub-note',
        line_number: 5,
        file_date: '2026-07-01'
      },
      {
        id: 'child-2',
        parent_id: 'task-1',
        type: 'NOTE',
        // depth intentionally omitted (malformed) — must not suppress normalization.
        raw_text: '- malformed sub-note',
        clean_text: 'malformed sub-note',
        line_number: 6,
        file_date: '2026-07-01'
      }
    ])
    render(TaskSubEditorModal, {
      ...BASE_PROPS,
      ctx: makeCtx(),
      onClose: () => {}
    })
    await vi.waitFor(() => {
      const pm = document.querySelector('.ProseMirror')
      expect(pm).not.toBeNull()
      const notes = pm?.querySelectorAll('[data-type="note"]')
      expect(notes).not.toBeNull()
      expect(notes!.length).toBeGreaterThanOrEqual(1)
      // The depth:3 sibling normalizes to 0 despite the undefined-depth sibling.
      expect(notes![0]?.getAttribute('data-depth')).toBe('0')
    })
  })

  it('calls fetchSubtree with the block id on mount', async () => {
    render(TaskSubEditorModal, {
      ...BASE_PROPS,
      ctx: makeCtx(),

      onClose: () => {}
    })
    await flush()

    expect(mocks.fetchSubtree).toHaveBeenCalledWith('task-1')
  })

  it('backdrop click closes the modal (calls onClose)', async () => {
    const onClose = vi.fn()
    render(TaskSubEditorModal, {
      ...BASE_PROPS,
      ctx: makeCtx(),

      onClose
    })
    await flush()

    // Both the backdrop and the header X carry the same aria-label; the
    // backdrop is the first in DOM order and covers the full overlay.
    const closeBtns = screen.getAllByLabelText('Close sub-editor')
    await fireEvent.click(closeBtns[0])
    await flush()

    expect(onClose).toHaveBeenCalled()
  })

  it('Esc closes the modal', async () => {
    const onClose = vi.fn()
    render(TaskSubEditorModal, {
      ...BASE_PROPS,
      ctx: makeCtx(),

      onClose
    })
    await flush()

    await fireEvent.keyDown(window, { key: 'Escape' })
    await flush()

    expect(onClose).toHaveBeenCalled()
  })

  it('the close (X) button calls onClose', async () => {
    const onClose = vi.fn()
    render(TaskSubEditorModal, {
      ...BASE_PROPS,
      ctx: makeCtx(),

      onClose
    })
    await flush()

    // The header X button is the last Close-affordance in DOM order (the
    // backdrop button is first).
    const closeBtns = screen.getAllByLabelText('Close sub-editor')
    await fireEvent.click(closeBtns[closeBtns.length - 1])
    await flush()

    expect(onClose).toHaveBeenCalled()
  })

  it('flushes a pending snapshot when unmounted during an in-flight save (no data loss)', async () => {
    // Regression: unmount-without-close used to drop edits made during an
    // in-flight save because the retry was gated on a live editor, which
    // onDestroy tears down before the IPC resolves. The snapshot fix flushes
    // the captured doc directly. We assert a second save call lands.
    let resolveFirst!: () => void
    const firstSave = new Promise<void>((r) => (resolveFirst = r))
    const calls: unknown[][] = []
    mocks.saveSubtreeBlocks.mockImplementation((_id, children) => {
      calls.push(children)
      // First call hangs until the test resolves it; subsequent calls return.
      if (calls.length === 1) return firstSave.then(() => undefined)
      return Promise.resolve()
    })
    mocks.fetchSubtree.mockResolvedValue([])

    const { unmount } = render(TaskSubEditorModal, {
      ...BASE_PROPS,
      ctx: makeCtx(),
      onClose: () => {}
    })
    // Wait for the editor to mount.
    await vi.waitFor(() =>
      expect(document.querySelector('.ProseMirror')).not.toBeNull()
    )

    // Simulate a user edit by injecting text into the ProseMirror node and
    // dispatching a transaction so onUpdate fires + schedules a save.
    const pm = document.querySelector('.ProseMirror') as HTMLElement
    pm.textContent = 'edited content'
    // Dispatch an input event so TipTap's view picks up the DOM change and
    // fires onUpdate (which sets unsavedChanges + scheduleSave).
    pm.dispatchEvent(new InputEvent('input', { bubbles: true }))

    // Advance past the 600ms debounce so persist() runs (the first save).
    await vi.waitFor(() => expect(calls.length).toBeGreaterThanOrEqual(1))

    // Now unmount WITHOUT going through attemptClose (route-change path).
    // onDestroy runs synchronously, destroying the editor.
    unmount()

    // Resolve the first in-flight save. The snapshot flush must fire a
    // second save call even though the editor is gone.
    resolveFirst()
    await flush()
    // Allow the microtask queue to settle the finally + flush.
    await new Promise((r) => setTimeout(r, 10))

    expect(calls.length).toBeGreaterThanOrEqual(2)
  })
})

describe('TaskSubEditorModal — metadata sidebar (#780 / #826)', () => {
  beforeEach(() => {
    mocks.fetchSubtree.mockReset().mockResolvedValue([])
    mocks.saveSubtreeBlocks.mockReset().mockResolvedValue(true)
    mocks.sqliteQuery
      .mockReset()
      .mockResolvedValue({ rows: [makeTaskRow()], truncated: false })
    mql.matches = false
    mqlListeners.length = 0
  })

  afterEach(() => cleanup())

  it('renders the editor and the metadata sidebar side-by-side on wide viewports', async () => {
    render(TaskSubEditorModal, {
      ...BASE_PROPS,
      ctx: makeCtx(),
      onClose: () => {}
    })
    // Wait for the editor to mount.
    await vi.waitFor(() =>
      expect(document.querySelector('.ProseMirror')).not.toBeNull()
    )
    // Wait for the task detail to hydrate so the sidebar renders.
    await vi.waitFor(() => expect(mocks.sqliteQuery).toHaveBeenCalled())
    await flush()

    // The sidebar's Status heading is present alongside the editor.
    expect(screen.getByText('Status')).toBeTruthy()
    expect(screen.getByText('Due date')).toBeTruthy()
    // Wide layout exposes a Details toggle in the header (#826).
    expect(
      screen.getByRole('button', { name: 'Hide details' })
    ).toBeInTheDocument()
    expect(screen.getByText('Essentials')).toBeInTheDocument()
    expect(screen.getByLabelText('Owner')).toBeInTheDocument()
    expect(screen.getByTestId('task-planning-disclosure')).not.toHaveAttribute(
      'open'
    )
  })

  it('keeps the modal open when Escape dismisses a nested metadata popover', async () => {
    const onClose = vi.fn()
    render(TaskSubEditorModal, {
      ...BASE_PROPS,
      ctx: makeCtx(),
      onClose
    })
    await vi.waitFor(() => expect(mocks.sqliteQuery).toHaveBeenCalled())
    await flush()

    const dueTrigger = screen.getByRole('button', { name: /2026-07-15/ })
    await fireEvent.click(dueTrigger)
    await flush()
    expect(
      screen.getByRole('dialog', { name: 'Due date options' })
    ).toBeInTheDocument()

    dueTrigger.focus()
    await fireEvent.keyDown(dueTrigger, { key: 'Escape' })
    await flush()
    expect(dueTrigger).toHaveAttribute('aria-expanded', 'false')
    expect(onClose).not.toHaveBeenCalled()
    expect(document.activeElement).toBe(dueTrigger)

    await fireEvent.keyDown(window, { key: 'Escape' })
    await flush()
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('a metadata change persists via ctx and fires onMetaChanged', async () => {
    const onMetaChanged = vi.fn()
    const updateBlockState = vi
      .fn()
      .mockResolvedValue({ ok: true, spawnedId: '' })
    render(TaskSubEditorModal, {
      ...BASE_PROPS,
      ctx: makeCtx({ updateBlockState }),
      onMetaChanged,
      onClose: () => {}
    })
    await vi.waitFor(() =>
      expect(document.querySelector('.ProseMirror')).not.toBeNull()
    )
    await vi.waitFor(() => expect(mocks.sqliteQuery).toHaveBeenCalled())
    await flush()

    // Click DONE in the sidebar's status radiogroup.
    const done = screen.getByRole('radio', { name: 'Done' })
    await fireEvent.click(done)
    await flush()

    expect(updateBlockState).toHaveBeenCalledWith('task-1', 'DONE')
    expect(onMetaChanged).toHaveBeenCalled()
  })

  it('writes Start day changes through the shared sidebar context', async () => {
    const setTaskStartDate = vi.fn().mockResolvedValue(true)
    render(TaskSubEditorModal, {
      ...BASE_PROPS,
      ctx: makeCtx({ setTaskStartDate }),
      onClose: () => {}
    })
    await vi.waitFor(() =>
      expect(document.querySelector('.ProseMirror')).not.toBeNull()
    )
    await vi.waitFor(() => expect(mocks.sqliteQuery).toHaveBeenCalled())
    await flush()

    await fireEvent.change(screen.getByLabelText('Start day'), {
      target: { value: '2026-07-09' }
    })
    await flush()

    expect(setTaskStartDate).toHaveBeenCalledWith('task-1', '2026-07-09')
  })

  it('collapses the sidebar into a disclosure on narrow viewports', async () => {
    mql.matches = true
    render(TaskSubEditorModal, {
      ...BASE_PROPS,
      ctx: makeCtx(),
      onClose: () => {}
    })
    await vi.waitFor(() =>
      expect(document.querySelector('.ProseMirror')).not.toBeNull()
    )
    await vi.waitFor(() => expect(mocks.sqliteQuery).toHaveBeenCalled())
    await flush()

    // The disclosure toggle is present and collapsed by default on narrow.
    const toggle = screen.getByRole('button', { name: 'Show details' })
    expect(toggle).toHaveAttribute('aria-expanded', 'false')
    expect(toggle).not.toHaveAttribute('aria-controls')

    // Expanding reveals the sidebar.
    await fireEvent.click(toggle)
    await tick()
    expect(toggle).toHaveAttribute('aria-expanded', 'true')
    expect(toggle).toHaveAttribute('aria-controls', 'sub-editor-sidebar')
    expect(screen.getByText('Status')).toBeTruthy()
  })

  it('collapses the wide sidebar via the header Details toggle (#826)', async () => {
    render(TaskSubEditorModal, {
      ...BASE_PROPS,
      ctx: makeCtx(),
      onClose: () => {}
    })
    await vi.waitFor(() =>
      expect(document.querySelector('.ProseMirror')).not.toBeNull()
    )
    await vi.waitFor(() => expect(mocks.sqliteQuery).toHaveBeenCalled())
    await flush()

    expect(screen.getByText('Status')).toBeTruthy()
    const hide = screen.getByRole('button', { name: 'Hide details' })
    await fireEvent.click(hide)
    await tick()

    expect(screen.queryByText('Status')).toBeNull()
    expect(
      screen.getByRole('button', { name: 'Show details' })
    ).toBeInTheDocument()

    await fireEvent.click(screen.getByRole('button', { name: 'Show details' }))
    await tick()
    expect(screen.getByText('Status')).toBeTruthy()
  })

  it('does not override sidebarOpen when the viewport crosses the breakpoint (#826)', async () => {
    render(TaskSubEditorModal, {
      ...BASE_PROPS,
      ctx: makeCtx(),
      onClose: () => {}
    })
    await vi.waitFor(() =>
      expect(document.querySelector('.ProseMirror')).not.toBeNull()
    )
    await vi.waitFor(() => expect(mocks.sqliteQuery).toHaveBeenCalled())
    await flush()

    // User collapses on wide.
    await fireEvent.click(screen.getByRole('button', { name: 'Hide details' }))
    await tick()
    expect(screen.queryByText('Status')).toBeNull()

    // Resize to narrow — preference stays closed (no auto-expand).
    fireMqlChange(true)
    await tick()
    const narrowToggle = screen.getByRole('button', { name: 'Show details' })
    expect(narrowToggle).toHaveAttribute('aria-expanded', 'false')
    expect(screen.queryByText('Status')).toBeNull()

    // Expand on narrow, then resize back to wide — stays open.
    await fireEvent.click(narrowToggle)
    await tick()
    expect(screen.getByText('Status')).toBeTruthy()

    fireMqlChange(false)
    await tick()
    expect(screen.getByText('Status')).toBeTruthy()
    expect(
      screen.getByRole('button', { name: 'Hide details' })
    ).toHaveAttribute('aria-expanded', 'true')
  })
})
