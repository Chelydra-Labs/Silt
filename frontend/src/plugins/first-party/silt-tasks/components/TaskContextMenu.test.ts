// TaskContextMenu (#1116): the shared per-view task context menu. These tests
// drive the component directly with menu state (the views' wiring tests live
// in ListView/BoardView/CalendarView.test.ts). PluginContext is mocked inline
// per the TaskMetadataSidebar.test.ts pattern; trackTaskUndoWrite is wrapped
// (not replaced) so the journaling contract can be asserted per write.
import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest'
import { tick } from 'svelte'
import {
  render,
  screen,
  cleanup,
  fireEvent,
  within
} from '@testing-library/svelte'

import TaskContextMenu from './TaskContextMenu.svelte'
import type { TaskContextMenuState } from './useTaskContextMenu.svelte'
import { buildDueDatePresets } from './dueDatePresets'
import { getTaskWeekStart } from '../../../../lib/taskWeekStart.svelte'
import type { PluginContext } from '../../../sdk'
import type { TaskDetail } from '../types'
import { v2CtxStubs } from '../../../test-helpers'

const undoMocks = vi.hoisted(() => ({
  trackTaskUndoWrite: vi.fn((p: Promise<unknown>) => p)
}))

// Wrap (not replace) so assertions can prove every SDK write is journaled
// while the real undo-pause behavior stays in place.
vi.mock('../undo.svelte', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../undo.svelte')>()
  return { ...actual, trackTaskUndoWrite: undoMocks.trackTaskUndoWrite }
})

const TODAY = '2026-07-06'

function makeTask(overrides: Partial<TaskDetail> = {}): TaskDetail {
  return {
    id: 'task-1',
    source: 'vault',
    notebook: 'Work',
    section: 'Journal',
    page: 'Daily',
    file_date: '2026-07-01',
    clean_content: 'Water plants',
    status: 'TODO',
    owner: '',
    start_date: '',
    due_date: '',
    priority: 3,
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
    shortlist_rank: null,
    subtask_total: 0,
    subtask_done: 0,
    ...overrides
  }
}

function makeCtx(overrides: Partial<PluginContext> = {}): PluginContext {
  return {
    ...v2CtxStubs,
    activeNotebook: 'Work',
    activeSection: 'Journal',
    activePage: 'Daily',
    today: TODAY,
    sqliteQuery: vi.fn(),
    updateBlockState: vi.fn().mockResolvedValue({ ok: true }),
    updateTaskMeta: vi.fn().mockResolvedValue(true),
    setTaskDueDate: vi.fn().mockResolvedValue(true),
    setTaskPriority: vi.fn().mockResolvedValue(true),
    deleteBlock: vi.fn().mockResolvedValue(true),
    getTaskBlockers: vi.fn().mockResolvedValue([]),
    getPluginSettings: vi.fn(() => Promise.resolve({})),
    on: () => () => {},
    ...overrides
  } as unknown as PluginContext
}

function menuState(task: TaskDetail): TaskContextMenuState {
  return { task, x: 10, y: 10, anchorEl: null }
}

function renderMenu(task: TaskDetail, ctx = makeCtx(), handlers = {}) {
  const onClose = vi.fn()
  const onEditTask = vi.fn()
  const onOpenSubEditor = vi.fn()
  render(TaskContextMenu, {
    props: {
      ctx,
      menu: menuState(task),
      onClose,
      onEditTask,
      onOpenSubEditor,
      ...handlers
    }
  })
  return { ctx, onClose, onEditTask, onOpenSubEditor }
}

/** Root-level items only (submenu entries live in nested [role=menu] flyouts). */
function rootItems(): HTMLElement[] {
  const menu = screen.getByTestId('task-context-menu')
  return Array.from(
    menu.querySelectorAll<HTMLElement>(':scope > [role="menuitem"]')
  )
}

async function flush() {
  await tick()
  await new Promise((r) => setTimeout(r, 0))
}

/** Item label = first non-icon text span (glyphs are aria-hidden text). */
function labelOf(el: HTMLElement): string {
  const span = el.querySelector<HTMLSpanElement>('span:not([aria-hidden])')
  return (span ?? el).textContent?.trim() ?? ''
}

describe('TaskContextMenu (#1116)', () => {
  beforeEach(() => {
    undoMocks.trackTaskUndoWrite.mockClear()
  })
  afterEach(cleanup)

  it('renders the seven root items for an open task', () => {
    renderMenu(makeTask())
    expect(rootItems().map(labelOf)).toEqual([
      'Mark done',
      'Pin',
      'Due date',
      'Priority',
      'Open in sub-editor',
      'Edit task',
      'Delete task'
    ])
  })

  it('is state-aware: DONE tasks reopen and drop the pin item (PinToggle parity)', () => {
    renderMenu(makeTask({ status: 'DONE', pinned: true }))
    const labels = rootItems().map(labelOf)
    expect(labels[0]).toBe('Reopen')
    expect(labels).not.toContain('Pin')
    expect(labels).not.toContain('Unpin')
  })

  it('Mark done writes updateBlockState DONE through the undo wrapper', async () => {
    const { ctx, onClose } = renderMenu(makeTask())
    await fireEvent.click(screen.getByTestId('task-menu-toggle-status'))
    expect(ctx.updateBlockState).toHaveBeenCalledWith('task-1', 'DONE')
    expect(undoMocks.trackTaskUndoWrite).toHaveBeenCalled()
    expect(onClose).toHaveBeenCalled()
  })

  it('Reopen writes updateBlockState TODO', async () => {
    const { ctx } = renderMenu(makeTask({ status: 'DONE' }))
    await fireEvent.click(screen.getByTestId('task-menu-toggle-status'))
    expect(ctx.updateBlockState).toHaveBeenCalledWith('task-1', 'TODO')
  })

  it('a blocked task confirms through the BlockedDoneDialog before DONE lands', async () => {
    const { ctx } = renderMenu(
      makeTask({ is_blocked: 1 }),
      makeCtx({
        getTaskBlockers: vi
          .fn()
          .mockResolvedValue([{ id: 'b1', clean_content: 'Prereq' }])
      })
    )
    await fireEvent.click(screen.getByTestId('task-menu-toggle-status'))
    expect(ctx.updateBlockState).not.toHaveBeenCalled()
    await fireEvent.click(screen.getByText('Complete anyway'))
    expect(ctx.updateBlockState).toHaveBeenCalledWith('task-1', 'DONE')
  })

  it('Pin / Unpin writes updateTaskMeta pinned through the wrapper', async () => {
    const pinned = makeTask({ pinned: true })
    const { ctx: ctx1 } = renderMenu(pinned)
    expect(screen.getByTestId('task-menu-pin').textContent).toContain('Unpin')
    await fireEvent.click(screen.getByTestId('task-menu-pin'))
    expect(ctx1.updateTaskMeta).toHaveBeenCalledWith('task-1', {
      pinned: false
    })

    cleanup()
    const { ctx: ctx2 } = renderMenu(makeTask({ pinned: false }))
    await fireEvent.click(screen.getByTestId('task-menu-pin'))
    expect(ctx2.updateTaskMeta).toHaveBeenCalledWith('task-1', { pinned: true })
  })

  it('due submenu mirrors the drawer presets and writes the YMD value', async () => {
    const { ctx } = renderMenu(makeTask())
    const menu = screen.getByTestId('task-context-menu')
    const dueSub = within(
      menu.querySelector('[role="menu"][aria-label="Due date presets"]')!
    )
    const presets = buildDueDatePresets(TODAY, getTaskWeekStart())
    for (const preset of presets) {
      expect(dueSub.getByTestId(`task-menu-due-${preset.value}`)).toBeTruthy()
    }
    // No due date → no "Clear due date" row (drawer behavior).
    expect(dueSub.queryByTestId('task-menu-due-clear')).toBeNull()

    await fireEvent.click(dueSub.getByTestId(`task-menu-due-${TODAY}`))
    expect(ctx.setTaskDueDate).toHaveBeenCalledWith('task-1', TODAY)
    expect(undoMocks.trackTaskUndoWrite).toHaveBeenCalled()
  })

  it('offers Clear due date only when a due date is set, and clears it', async () => {
    const { ctx } = renderMenu(makeTask({ due_date: TODAY }))
    const menu = screen.getByTestId('task-context-menu')
    const dueSub = within(
      menu.querySelector('[role="menu"][aria-label="Due date presets"]')!
    )
    const clear = dueSub.getByTestId('task-menu-due-clear')
    expect(clear.textContent).toContain('Clear due date')
    // A due date matching a preset is marked current (check + SR-only note).
    expect(dueSub.getByTestId(`task-menu-due-${TODAY}`).textContent).toContain(
      '(current)'
    )
    await fireEvent.click(clear)
    expect(ctx.setTaskDueDate).toHaveBeenCalledWith('task-1', '')
  })

  it('priority submenu marks the current value and writes the selection', async () => {
    const { ctx } = renderMenu(makeTask({ priority: 2 }))
    const menu = screen.getByTestId('task-context-menu')
    const priSub = within(
      menu.querySelector('[role="menu"][aria-label="Priority"]')!
    )
    const high = priSub.getByTestId('task-menu-priority-1')
    const normal = priSub.getByTestId('task-menu-priority-2')
    expect(normal.textContent).toContain('(current)')
    expect(high.textContent).not.toContain('(current)')
    expect(priSub.getByTestId('task-menu-priority-3').textContent).toContain(
      'Low'
    )

    await fireEvent.click(high)
    expect(ctx.setTaskPriority).toHaveBeenCalledWith('task-1', 1)
    expect(undoMocks.trackTaskUndoWrite).toHaveBeenCalled()
  })

  it('sub-editor and edit items delegate to the host callbacks', async () => {
    const task = makeTask()
    const { onEditTask, onOpenSubEditor } = renderMenu(task)
    await fireEvent.click(screen.getByTestId('task-menu-sub-editor'))
    expect(onOpenSubEditor).toHaveBeenCalledWith(task)
    await fireEvent.click(screen.getByTestId('task-menu-edit'))
    expect(onEditTask).toHaveBeenCalledWith(task)
  })

  it('delete confirms before writing; cancel leaves the task untouched', async () => {
    const { ctx } = renderMenu(makeTask())
    await fireEvent.click(screen.getByTestId('task-menu-delete'))
    expect(ctx.deleteBlock).not.toHaveBeenCalled()
    expect(screen.getByTestId('task-menu-delete-confirm')).toBeTruthy()

    await fireEvent.click(screen.getByTestId('task-menu-delete-confirm-cancel'))
    expect(ctx.deleteBlock).not.toHaveBeenCalled()
    expect(screen.queryByTestId('task-menu-delete-confirm')).toBeNull()

    await fireEvent.click(screen.getByTestId('task-menu-delete'))
    await fireEvent.click(
      screen.getByTestId('task-menu-delete-confirm-confirm')
    )
    expect(ctx.deleteBlock).toHaveBeenCalledWith('task-1')
    expect(undoMocks.trackTaskUndoWrite).toHaveBeenCalled()
  })

  it('dismisses on Escape and on backdrop click', async () => {
    const { onClose } = renderMenu(makeTask())
    await fireEvent.keyDown(window, { key: 'Escape' })
    expect(onClose).toHaveBeenCalledTimes(1)
    onClose.mockClear()
    await fireEvent.click(screen.getByTestId('task-menu-backdrop'))
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('surfaces a failed write in the error banner', async () => {
    renderMenu(
      makeTask(),
      makeCtx({
        setTaskPriority: vi.fn().mockRejectedValue(new Error('disk locked'))
      })
    )
    const menu = screen.getByTestId('task-context-menu')
    const priSub = within(
      menu.querySelector('[role="menu"][aria-label="Priority"]')!
    )
    await fireEvent.click(priSub.getByTestId('task-menu-priority-1'))
    await flush()
    expect(screen.getByTestId('task-menu-error').textContent).toContain(
      "Couldn't update task"
    )
  })

  it('reports write failures through onError instead of the in-flow banner', async () => {
    const onError = vi.fn()
    renderMenu(
      makeTask(),
      makeCtx({
        setTaskPriority: vi.fn().mockRejectedValue(new Error('disk locked'))
      }),
      { onError }
    )
    const menu = screen.getByTestId('task-context-menu')
    const priSub = within(
      menu.querySelector('[role="menu"][aria-label="Priority"]')!
    )
    await fireEvent.click(priSub.getByTestId('task-menu-priority-1'))
    await flush()
    expect(onError).toHaveBeenCalledWith(
      expect.stringContaining("Couldn't update task")
    )
    expect(screen.queryByTestId('task-menu-error')).toBeNull()
  })
})
