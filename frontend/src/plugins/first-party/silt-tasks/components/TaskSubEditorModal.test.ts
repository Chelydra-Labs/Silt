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
import TaskEditDrawer from './TaskEditDrawer.svelte'
import type { PluginContext } from '../../../sdk'
import type { TaskDetail } from '../types'
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
    shortlist_rank: null,
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
    // The accessible name comes from the sr-only header heading (the visible
    // title lives in the sidebar's editable input).
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
    // Header still carries the parent task title in its sr-only heading.
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

  it('leaves underline enabled in the production sub-editor schema (#1030)', async () => {
    mocks.fetchSubtree.mockResolvedValue([])
    render(TaskSubEditorModal, {
      ...BASE_PROPS,
      ctx: makeCtx(),

      onClose: () => {}
    })
    await vi.waitFor(() => {
      expect(document.querySelector('.ProseMirror')).not.toBeNull()
    })

    // The component builds its own StarterKit.configure list; a silent
    // `underline: false` there would not be caught by the schema-level tests.
    const pm = document.querySelector('.ProseMirror') as HTMLElement & {
      editor: { schema: { marks: Record<string, unknown> } }
    }
    expect(pm.editor.schema.marks.underline).toBeDefined()
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

  it('keeps failed edits open and supports retrying the save', async () => {
    const onClose = vi.fn()
    mocks.saveSubtreeBlocks
      .mockRejectedValueOnce(new Error('disk locked'))
      .mockRejectedValueOnce(new Error('disk locked'))
      .mockResolvedValueOnce(true)
    render(TaskSubEditorModal, {
      ...BASE_PROPS,
      ctx: makeCtx(),
      onClose
    })
    await vi.waitFor(() =>
      expect(document.querySelector('.ProseMirror')).not.toBeNull()
    )

    const pm = document.querySelector('.ProseMirror') as HTMLElement
    pm.textContent = 'edited content'
    pm.dispatchEvent(new InputEvent('input', { bubbles: true }))
    await vi.waitFor(() =>
      expect(screen.getByText(/Save failed: disk locked/)).toBeInTheDocument()
    )

    expect(onClose).not.toHaveBeenCalled()
    await fireEvent.click(screen.getAllByLabelText('Close sub-editor').at(-1)!)
    await vi.waitFor(() =>
      expect(mocks.saveSubtreeBlocks).toHaveBeenCalledTimes(2)
    )
    expect(onClose).not.toHaveBeenCalled()
    await fireEvent.click(screen.getByRole('button', { name: 'Retry save' }))
    await vi.waitFor(() =>
      expect(mocks.saveSubtreeBlocks).toHaveBeenCalledTimes(3)
    )
    await flush()
    expect(screen.queryByText(/Save failed: disk locked/)).toBeNull()

    await fireEvent.click(screen.getAllByLabelText('Close sub-editor').at(-1)!)
    await flush()
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('allows explicitly discarding edits after a failed save', async () => {
    const onClose = vi.fn()
    mocks.saveSubtreeBlocks.mockRejectedValueOnce(new Error('disk locked'))
    render(TaskSubEditorModal, {
      ...BASE_PROPS,
      ctx: makeCtx(),
      onClose
    })
    await vi.waitFor(() =>
      expect(document.querySelector('.ProseMirror')).not.toBeNull()
    )
    const pm = document.querySelector('.ProseMirror') as HTMLElement
    pm.textContent = 'edited content'
    pm.dispatchEvent(new InputEvent('input', { bubbles: true }))
    await vi.waitFor(() =>
      expect(
        screen.getByRole('button', { name: 'Discard changes' })
      ).toBeInTheDocument()
    )

    await fireEvent.click(
      screen.getByRole('button', { name: 'Discard changes' })
    )
    expect(onClose).toHaveBeenCalledTimes(1)
    expect(mocks.saveSubtreeBlocks).toHaveBeenCalledTimes(1)
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
    expect(screen.getByTestId('task-planning-section')).toBeInTheDocument()
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
      ctx: makeCtx({ setTaskStartDate, today: '2026-07-09' }),
      onClose: () => {}
    })
    await vi.waitFor(() =>
      expect(document.querySelector('.ProseMirror')).not.toBeNull()
    )
    await vi.waitFor(() => expect(mocks.sqliteQuery).toHaveBeenCalled())
    await flush()

    await fireEvent.click(
      document.getElementById(
        'sub-editor-task-start-date-trigger'
      ) as HTMLElement
    )
    await fireEvent.click(screen.getByText('Today'))
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

describe('TaskSubEditorModal — pop-up single title (#1096)', () => {
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

  async function mountWithSidebar() {
    render(TaskSubEditorModal, {
      ...BASE_PROPS,
      ctx: makeCtx(),
      onClose: () => {}
    })
    await vi.waitFor(() => expect(mocks.sqliteQuery).toHaveBeenCalled())
    await flush()
  }

  function heading(): HTMLElement {
    const el = document.getElementById('sub-editor-title')
    expect(el, 'dialog heading must always render').not.toBeNull()
    return el as HTMLElement
  }

  it('shows exactly one editable title — the sidebar input — with the sidebar open', async () => {
    await mountWithSidebar()

    const titles = screen.getAllByLabelText('Task title')
    expect(titles).toHaveLength(1)
    expect(titles[0]).toHaveValue('Ship the feature')
    expect(document.getElementById('sub-editor-sidebar')).toContainElement(
      titles[0]
    )

    // The header copy is visually hidden, not deleted: it is the dialog name.
    expect(heading()).toHaveClass('sr-only')
    expect(heading()).toHaveTextContent('Ship the feature')
    expect(
      screen.getByRole('dialog', { name: 'Ship the feature' })
    ).toBeInTheDocument()
  })

  it('keeps the dialog named and the Details toggle usable with the sidebar shut', async () => {
    await mountWithSidebar()

    await fireEvent.click(screen.getByRole('button', { name: 'Hide details' }))
    await tick()
    expect(screen.queryAllByLabelText('Task title')).toHaveLength(0)
    expect(heading()).toHaveTextContent('Ship the feature')
    expect(
      screen.getByRole('dialog', { name: 'Ship the feature' })
    ).toBeInTheDocument()

    // The toggle still reopens the sidebar after a shut.
    await fireEvent.click(screen.getByRole('button', { name: 'Show details' }))
    await tick()
    expect(screen.getAllByLabelText('Task title')).toHaveLength(1)
  })

  it('names the dialog from the sr-only heading while the narrow sidebar is shut', async () => {
    mql.matches = true
    render(TaskSubEditorModal, {
      ...BASE_PROPS,
      ctx: makeCtx(),
      onClose: () => {}
    })
    await vi.waitFor(() => expect(mocks.sqliteQuery).toHaveBeenCalled())
    await flush()

    // Narrow defaults to the collapsed disclosure.
    expect(
      screen.getByRole('button', { name: 'Show details' })
    ).toHaveAttribute('aria-expanded', 'false')
    expect(screen.queryAllByLabelText('Task title')).toHaveLength(0)
    expect(heading()).toHaveTextContent('Ship the feature')
    expect(
      screen.getByRole('dialog', { name: 'Ship the feature' })
    ).toBeInTheDocument()
  })
})

describe('TaskSubEditorModal — cross-surface sidebar parity (D7)', () => {
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

  function textOf(el: Element | null): string {
    return (el?.textContent ?? '').replace(/\s+/g, ' ').trim()
  }

  // The sidebar is shared, so its DOM order is the parity contract both hosts
  // render; reading it per host catches any prop that changes layout.
  function readSidebarLayout(root: Element) {
    const labels = (scope: Element | null, selector: string) =>
      scope
        ? Array.from(scope.querySelectorAll(selector)).map((el) => textOf(el))
        : []
    return {
      primary: labels(
        root.querySelector('[data-testid="task-primary-header"]'),
        'h3'
      ),
      essentials: labels(
        root.querySelector(
          'section[aria-labelledby$="task-essentials-heading"]'
        ),
        'dt'
      ),
      planning: labels(
        root.querySelector('[data-testid="task-planning-section"]'),
        'h3, label[for$="task-estimate-input"]'
      ),
      activity: labels(
        root.querySelector('[data-testid="task-activity-section"]'),
        'h3'
      )
    }
  }

  // The modal opens over the still-mounted drawer, so both sidebars sit in the
  // document at once. Each id must belong to a single instance, and every
  // label/ARIA reference must resolve inside the sidebar that owns it.
  function expectScopedIds(...roots: Element[]) {
    const idsOf = (root: Element) =>
      Array.from(root.querySelectorAll('[id]'), (el) => el.id)
    const idSets = roots.map(idsOf)
    for (const ids of idSets) {
      expect(new Set(ids).size, 'ids must be unique within a sidebar').toBe(
        ids.length
      )
    }
    for (const ids of idSets.slice(1)) {
      expect(ids.filter((id) => idSets[0].includes(id))).toEqual([])
    }

    // aria-controls and aria-activedescendant name popover content that only
    // renders while open, so they must resolve only when the target is already
    // in the document.
    const required = ['for', 'aria-labelledby', 'aria-describedby']
    const optional = ['aria-controls', 'aria-activedescendant']
    const attrs = [...required, ...optional]
    for (const root of roots) {
      const selector = attrs.map((a) => `[${a}]`).join(',')
      for (const el of Array.from(root.querySelectorAll(selector))) {
        for (const attr of attrs) {
          const value = el.getAttribute(attr)
          if (!value) continue
          for (const id of value.split(/\s+/).filter(Boolean)) {
            const target = document.getElementById(id)
            if (!target) {
              expect(optional.includes(attr), `${attr}="${id}" resolves`).toBe(
                true
              )
              continue
            }
            // Popover bodies are portaled to <body>, so they resolve outside
            // every sidebar; the invariant is that they never land in a
            // *different* one.
            expect(
              roots.some((other) => other !== root && other.contains(target)),
              `${attr}="${id}" must not resolve inside another sidebar`
            ).toBe(false)
          }
        }
      }
    }
  }

  // The typeahead debounces its search; wait on the observable wiring (an
  // active option mounted in the portaled listbox) rather than on a constant
  // copied from the component's debounce, so a retune can't leave this
  // passing/failing for a timing reason unrelated to id scoping.
  async function openDependencyPopover(
    root: Element
  ): Promise<HTMLInputElement> {
    const input = root.querySelector<HTMLInputElement>(
      'input[aria-label="Search tasks to add as dependencies"]'
    )
    expect(input, 'dependency combobox must render').not.toBeNull()
    await fireEvent.input(input!, { target: { value: 'found' } })
    await vi.waitFor(() => {
      const optionId = input!.getAttribute('aria-activedescendant')
      expect(optionId, 'dependency popover must open').toBeTruthy()
      expect(
        document.getElementById(optionId!),
        'active option must be mounted'
      ).not.toBeNull()
    })
    await flush()
    return input!
  }

  function makeDetail(): TaskDetail {
    return {
      id: 'task-1',
      source: 'vault',
      notebook: 'Work',
      section: 'Journal',
      page: 'Daily',
      file_date: '2026-07-01',
      clean_content: 'Ship the feature',
      status: 'TODO',
      owner: '',
      start_date: '',
      due_date: '2026-07-15',
      priority: 2,
      pinned: false,
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
      shortlist_rank: null
    }
  }

  it('renders the same sidebar field order, grouping, and labels as the drawer', async () => {
    const searchTasks = async () => [
      {
        id: 'dep-found',
        clean_content: 'Found task',
        notebook: 'Work',
        section: 'Journal',
        page: 'Daily'
      }
    ]
    render(TaskSubEditorModal, {
      ...BASE_PROPS,
      ctx: makeCtx({ searchTasks }),
      onClose: () => {}
    })
    await vi.waitFor(() => expect(mocks.sqliteQuery).toHaveBeenCalled())
    await flush()
    const modalSidebar = document.querySelector(
      '#sub-editor-sidebar .task-metadata-sidebar'
    )
    expect(modalSidebar, 'modal sidebar must render').not.toBeNull()

    render(TaskEditDrawer, {
      props: {
        task: makeDetail(),
        ctx: makeCtx({ searchTasks }),
        onClose: () => {}
      }
    })
    const drawerSidebar = screen
      .getByTestId('task-edit-drawer')
      .querySelector('.task-metadata-sidebar')
    expect(drawerSidebar, 'drawer sidebar must render').not.toBeNull()

    const expected = {
      primary: ['Status', 'Due date', 'Start day', 'Priority'],
      essentials: ['Owner', 'Tags'],
      planning: [
        'Planning & tracking',
        'Progress',
        'Estimate',
        'Recurrence',
        'Dependencies'
      ],
      activity: ['Activity', 'Comments']
    }
    expect(readSidebarLayout(modalSidebar!)).toEqual(expected)
    expect(readSidebarLayout(drawerSidebar!)).toEqual(expected)
    expect(readSidebarLayout(modalSidebar!)).toEqual(
      readSidebarLayout(drawerSidebar!)
    )

    // Open both dependency popovers so their combobox wiring is on screen
    // while both sidebars share one document; "Open sub-editor" leaves the
    // drawer mounted, so coexistence is where a shared id can cross-wire.
    const modalCombobox = await openDependencyPopover(modalSidebar!)
    const drawerCombobox = await openDependencyPopover(drawerSidebar!)
    expectScopedIds(modalSidebar!, drawerSidebar!)

    // The popover body is portaled out of the sidebar, so assert the wiring
    // directly: each combobox must control the listbox that holds its own
    // active descendant, and those ids must be unique across both hosts.
    const modalOptionId = modalCombobox.getAttribute('aria-activedescendant')
    const drawerOptionId = drawerCombobox.getAttribute('aria-activedescendant')
    expect(modalOptionId).toBeTruthy()
    expect(drawerOptionId).toBeTruthy()
    expect(modalOptionId).not.toBe(drawerOptionId)
    for (const [combobox, optionId] of [
      [modalCombobox, modalOptionId],
      [drawerCombobox, drawerOptionId]
    ] as const) {
      const listboxId = combobox.getAttribute('aria-controls')
      const listbox = listboxId ? document.getElementById(listboxId) : null
      const option = optionId ? document.getElementById(optionId) : null
      expect(listboxId, 'combobox must control its listbox').toBeTruthy()
      expect(listbox?.getAttribute('role')).toBe('listbox')
      expect(option?.getAttribute('role')).toBe('option')
      expect(listbox?.contains(option), 'option must be in its listbox').toBe(
        true
      )
      expect(document.querySelectorAll(`[id="${optionId}"]`)).toHaveLength(1)
    }
  })
})
