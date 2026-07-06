import { describe, expect, it, beforeEach, vi } from 'vitest'
import { render, screen, cleanup, fireEvent } from '@testing-library/svelte'

// jsdom polyfill: Svelte 5 transition:fly calls element.animate().
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

vi.mock('../../../../wailsjs/runtime/runtime.js', () => ({
  EventsOn: vi.fn(() => () => {})
}))

import TaskEditDrawer from './TaskEditDrawer.svelte'
import type { PluginContext } from '../../sdk'
import type { TaskDetail } from './types'
import { v2CtxStubs } from '../../test-helpers'

function makeTask(overrides: Partial<TaskDetail> = {}): TaskDetail {
  return {
    id: 'task-1',
    notebook: 'Work',
    section: 'Journal',
    page: 'Daily',
    file_date: '2026-07-01',
    clean_content: 'Water plants',
    status: 'TODO',
    owner: '',
    start_date: '',
    due_date: '2026-07-15',
    priority: 3,
    pinned: false,
    progress: 0,
    recurrence: '',
    comments_count: 0,
    links_count: 0,
    // #417: new [created::]/[completed::]/[order::] fields default to
    // empty/0 (the pre-existing-task case).
    created_at: '',
    completed_at: '',
    manual_order: 0,
    ...overrides
  }
}

function makeCtx(overrides: Partial<PluginContext> = {}): PluginContext {
  return {
    activeNotebook: 'Work',
    activeSection: 'Journal',
    activePage: 'Daily',
    today: '2026-07-02',
    sqliteQuery: vi.fn(),
    mutateBlock: vi.fn(),
    updateBlockState: vi.fn(),
    updateTaskMeta: vi.fn(),
    getPluginSettings: vi.fn(() => Promise.resolve({})),
    on: () => () => {},
    ...v2CtxStubs,
    ...overrides
  } as unknown as PluginContext
}

async function flush() {
  await new Promise((r) => setTimeout(r, 0))
}

describe('TaskEditDrawer — recurrence', () => {
  beforeEach(() => cleanup())

  it('does not render the repeat badge when recurrence is empty', () => {
    const ctx = makeCtx()
    const { container } = render(TaskEditDrawer, {
      props: { task: makeTask(), ctx, onClose: () => {} }
    })
    // The header h2 should carry no event_repeat icon for a non-recurring task.
    const title = container.querySelector('#task-edit-drawer-title')
    expect(title?.querySelector('.material-symbols-outlined')).toBeNull()
    void container
  })

  it('renders the repeat badge in the header when recurrence is set', () => {
    const ctx = makeCtx()
    const { container } = render(TaskEditDrawer, {
      props: {
        task: makeTask({ recurrence: 'every week' }),
        ctx,
        onClose: () => {}
      }
    })
    const title = container.querySelector('#task-edit-drawer-title')
    const badge = title?.querySelector('.material-symbols-outlined')
    expect(badge).toBeTruthy()
    expect(badge?.textContent?.trim()).toBe('event_repeat')
  })

  it('shows the recurrence value in the editor trigger', () => {
    const ctx = makeCtx()
    render(TaskEditDrawer, {
      props: {
        task: makeTask({ recurrence: 'every month' }),
        ctx,
        onClose: () => {}
      }
    })
    // The trigger button (only rendered when a due date is set) shows the
    // current recurrence value.
    expect(screen.getByText('every month')).toBeTruthy()
  })

  it('opens the dropdown and selects an interval calling setTaskRecurrence', async () => {
    const setTaskRecurrence = vi.fn().mockResolvedValue(true)
    const ctx = makeCtx({ setTaskRecurrence })
    const { container } = render(TaskEditDrawer, {
      props: { task: makeTask(), ctx, onClose: () => {} }
    })
    const trigger = container.querySelector('button[aria-haspopup="listbox"]')
    expect(trigger).toBeTruthy()
    await fireEvent.click(trigger as HTMLElement)
    const option = screen.getByRole('option', { name: 'every week' })
    await fireEvent.click(option)
    expect(setTaskRecurrence).toHaveBeenCalledWith('task-1', 'every week')
  })

  it('shows Stop recurring when a recurrence is already set', async () => {
    const setTaskRecurrence = vi.fn().mockResolvedValue(true)
    const ctx = makeCtx({ setTaskRecurrence })
    const { container } = render(TaskEditDrawer, {
      props: {
        task: makeTask({ recurrence: 'every week' }),
        ctx,
        onClose: () => {}
      }
    })
    const trigger = container.querySelector('button[aria-haspopup="listbox"]')
    expect(trigger).toBeTruthy()
    await fireEvent.click(trigger as HTMLElement)
    const stopBtn = screen.getByText('Stop recurring')
    await fireEvent.click(stopBtn)
    expect(setTaskRecurrence).toHaveBeenCalledWith('task-1', '')
  })

  it('reverts local state on error', async () => {
    const setTaskRecurrence = vi
      .fn()
      .mockRejectedValue(new Error('disk locked'))
    const ctx = makeCtx({ setTaskRecurrence })
    render(TaskEditDrawer, {
      props: { task: makeTask(), ctx, onClose: () => {} }
    })
    const trigger = screen.getByText('Set recurrence…')
    await fireEvent.click(trigger)
    await fireEvent.click(screen.getByText('every day'))
    await new Promise((r) => setTimeout(r, 10))
    expect(screen.getByText(/Couldn't save/)).toBeTruthy()
  })

  it('disables the recurrence editor when no due date', () => {
    const ctx = makeCtx()
    render(TaskEditDrawer, {
      props: {
        task: makeTask({ due_date: '' }),
        ctx,
        onClose: () => {}
      }
    })
    expect(screen.getByText(/Set a due date first/)).toBeTruthy()
    expect(screen.queryByText('Set recurrence…')).toBeNull()
  })

  it('portals the recurrence listbox out of the scroll container so it is not clipped (#376)', async () => {
    const ctx = makeCtx()
    const { container } = render(TaskEditDrawer, {
      props: { task: makeTask(), ctx, onClose: () => {} }
    })
    const trigger = container.querySelector(
      'button[aria-haspopup="listbox"]'
    ) as HTMLElement
    await fireEvent.click(trigger)
    const option = await screen.findByRole('option', { name: 'every week' })
    expect(document.body.contains(option)).toBe(true)
    expect(container.contains(option)).toBe(false)
    expect(screen.queryByText('Stop recurring')).toBeNull()
  })
})

describe('TaskEditDrawer — due-date editor', () => {
  beforeEach(() => cleanup())

  it('a preset commits via ctx.setTaskDueDate', async () => {
    const setTaskDueDate = vi.fn().mockResolvedValue(true)
    const ctx = makeCtx({ setTaskDueDate, today: '2026-07-02' })
    const { container } = render(TaskEditDrawer, {
      props: { task: makeTask(), ctx, onClose: () => {} }
    })
    const trigger = container.querySelector(
      'button[aria-haspopup="dialog"]'
    ) as HTMLElement
    await fireEvent.click(trigger)
    // The "Today" preset carries ctx.today as its value.
    const today = screen.getByText('Today')
    await fireEvent.click(today)
    expect(setTaskDueDate).toHaveBeenCalledWith('task-1', '2026-07-02')
  })

  it('Clear due date commits an empty string', async () => {
    const setTaskDueDate = vi.fn().mockResolvedValue(true)
    const ctx = makeCtx({ setTaskDueDate })
    const { container } = render(TaskEditDrawer, {
      props: {
        task: makeTask({ due_date: '2026-07-15' }),
        ctx,
        onClose: () => {}
      }
    })
    const trigger = container.querySelector(
      'button[aria-haspopup="dialog"]'
    ) as HTMLElement
    await fireEvent.click(trigger)
    const clear = screen.getByText('Clear due date')
    await fireEvent.click(clear)
    expect(setTaskDueDate).toHaveBeenCalledWith('task-1', '')
  })
})

describe('TaskEditDrawer — status radiogroup', () => {
  beforeEach(() => cleanup())

  it('picking DONE commits via ctx.updateBlockState', async () => {
    const updateBlockState = vi.fn().mockResolvedValue(true)
    const ctx = makeCtx({ updateBlockState })
    const { container } = render(TaskEditDrawer, {
      props: { task: makeTask(), ctx, onClose: () => {} }
    })
    const rg = container.querySelector('[role="radiogroup"]')
    expect(rg).toBeTruthy()
    const done = screen.getByRole('radio', { name: 'Done' })
    await fireEvent.click(done)
    expect(updateBlockState).toHaveBeenCalledWith('task-1', 'DONE')
  })

  it('pauses with the shared BlockedDoneDialog when DONE is picked on a blocked task', async () => {
    const updateBlockState = vi.fn().mockResolvedValue(true)
    const getTaskBlockers = vi
      .fn()
      .mockResolvedValue([{ id: 'dep-1', clean_content: 'Prerequisite' }])
    const ctx = makeCtx({ updateBlockState, getTaskBlockers })
    render(TaskEditDrawer, {
      props: {
        task: makeTask({ status: 'DOING', is_blocked: 1 }),
        ctx,
        onClose: () => {}
      }
    })
    await fireEvent.click(screen.getByRole('radio', { name: 'Done' }))
    // The guard awaited getTaskBlockers before deciding.
    await flush()
    expect(getTaskBlockers).toHaveBeenCalledWith('task-1')
    // The shared dialog surfaced, and the status was NOT persisted yet.
    expect(screen.getByText('Complete anyway')).toBeTruthy()
    expect(updateBlockState).not.toHaveBeenCalled()
  })

  it('confirming the guard completes the DONE transition', async () => {
    const updateBlockState = vi.fn().mockResolvedValue(true)
    const getTaskBlockers = vi
      .fn()
      .mockResolvedValue([{ id: 'dep-1', clean_content: 'Prerequisite' }])
    const ctx = makeCtx({ updateBlockState, getTaskBlockers })
    render(TaskEditDrawer, {
      props: {
        task: makeTask({ status: 'DOING', is_blocked: 1 }),
        ctx,
        onClose: () => {}
      }
    })
    await fireEvent.click(screen.getByRole('radio', { name: 'Done' }))
    await flush()
    await fireEvent.click(screen.getByText('Complete anyway'))
    await flush()
    expect(updateBlockState).toHaveBeenCalledWith('task-1', 'DONE')
  })

  it('cancelling the guard leaves status unchanged (no write)', async () => {
    const updateBlockState = vi.fn().mockResolvedValue(true)
    const getTaskBlockers = vi
      .fn()
      .mockResolvedValue([{ id: 'dep-1', clean_content: 'Prerequisite' }])
    const ctx = makeCtx({ updateBlockState, getTaskBlockers })
    render(TaskEditDrawer, {
      props: {
        task: makeTask({ status: 'DOING', is_blocked: 1 }),
        ctx,
        onClose: () => {}
      }
    })
    await fireEvent.click(screen.getByRole('radio', { name: 'Done' }))
    await flush()
    await fireEvent.click(screen.getByText('Cancel'))
    await flush()
    await flush()
    expect(updateBlockState).not.toHaveBeenCalled()
    // Roving-tabindex stays consistent after the cancel: focus returns to
    // the still-checked radio (In Progress = DOING), not the canceled DONE
    // radio that briefly held focus.
    const doingRadio = screen.getByRole('radio', { name: 'In Progress' })
    expect(document.activeElement).toBe(doingRadio)
  })
})

describe('TaskEditDrawer — source awareness + affordances', () => {
  beforeEach(() => cleanup())

  it('renders aria-pressed as a real boolean (pinned coercion, not INT 0/1)', () => {
    const ctx = makeCtx()
    const { container } = render(TaskEditDrawer, {
      // SQL delivers pinned as INTEGER 1; cast simulates the wire shape.
      props: {
        task: makeTask({ pinned: 1 as unknown as boolean }),
        ctx,
        onClose: () => {}
      }
    })
    const pinBtn = Array.from(
      container.querySelectorAll('button[aria-pressed]')
    ).find((b) => b.textContent?.includes('Pin')) as HTMLElement | undefined
    expect(pinBtn).toBeTruthy()
    // Coerced to a real boolean — never the out-of-spec "1".
    expect(pinBtn?.getAttribute('aria-pressed')).toBe('true')
  })

  it('hides "Open source page" and shows "Standalone task" for a .silt task', () => {
    const ctx = makeCtx()
    render(TaskEditDrawer, {
      props: {
        task: makeTask({ notebook: '.silt', section: '', page: 'tasks.md' }),
        ctx,
        onClose: () => {}
      }
    })
    expect(screen.getByText('Standalone task')).toBeTruthy()
    expect(screen.queryByText('Open source page')).toBeNull()
  })

  it('shows "Open source page" for an in-page task', () => {
    const ctx = makeCtx()
    render(TaskEditDrawer, {
      props: { task: makeTask(), ctx, onClose: () => {} }
    })
    expect(screen.getByText('Open source page')).toBeTruthy()
  })

  it('renders "Open sub-editor" only when onOpenSubEditor is provided', () => {
    const ctx = makeCtx()
    const onOpenSubEditor = vi.fn()
    const { unmount } = render(TaskEditDrawer, {
      props: { task: makeTask(), ctx, onClose: () => {}, onOpenSubEditor }
    })
    const btn = screen.getByText('Open sub-editor')
    fireEvent.click(btn)
    expect(onOpenSubEditor).toHaveBeenCalled()
    unmount()
    // Without the callback the button is absent.
    render(TaskEditDrawer, {
      props: { task: makeTask(), ctx, onClose: () => {} }
    })
    expect(screen.queryByText('Open sub-editor')).toBeNull()
  })
})

describe('TaskEditDrawer — created/completed metadata line (#417)', () => {
  beforeEach(() => cleanup())

  it('hides the Created/Completed line when both timestamps are empty', () => {
    const ctx = makeCtx()
    render(TaskEditDrawer, {
      props: { task: makeTask(), ctx, onClose: () => {} }
    })
    // The common case for pre-existing tasks — no Created dt in the Details dl.
    expect(screen.queryByText('Created')).toBeNull()
    expect(screen.queryByText(/Completed/)).toBeNull()
  })

  it('renders the Created timestamp when created_at is populated', () => {
    const ctx = makeCtx()
    render(TaskEditDrawer, {
      props: {
        task: makeTask({ created_at: '2026-07-06T15:30:00' }),
        ctx,
        onClose: () => {}
      }
    })
    // The dt label is present, and the formatted timestamp renders (the
    // exact localized string varies by TZ/locale, so assert the label is
    // associated with a dd that contains "Jul" + "6").
    const createdDt = screen.getByText('Created')
    const dd = createdDt.parentElement?.querySelector('dd')
    expect(dd).toBeTruthy()
    expect(dd?.textContent).toMatch(/Jul/)
    expect(dd?.textContent).toMatch(/6/)
  })

  it('renders the Completed line when completed_at is populated', () => {
    const ctx = makeCtx()
    render(TaskEditDrawer, {
      props: {
        task: makeTask({
          status: 'DONE',
          created_at: '2026-07-01T09:00:00',
          completed_at: '2026-07-06T15:30:00'
        }),
        ctx,
        onClose: () => {}
      }
    })
    expect(screen.getByText(/Completed/)).toBeTruthy()
    // Completed renders as a block-level line inside the dd.
    const completedLine = screen.getByText(/Completed/).parentElement
    expect(completedLine?.textContent).toMatch(/Jul/)
    expect(completedLine?.textContent).toMatch(/6/)
  })
})

describe('TaskEditDrawer — owner editor (#412)', () => {
  beforeEach(() => cleanup())

  it('typing + Enter commits via ctx.setTaskOwner', async () => {
    const setTaskOwner = vi.fn().mockResolvedValue(true)
    const ctx = makeCtx({ setTaskOwner })
    render(TaskEditDrawer, {
      props: { task: makeTask(), ctx, onClose: () => {} }
    })
    const input = screen.getByLabelText('Owner')
    await fireEvent.input(input, { target: { value: 'Alice' } })
    await fireEvent.keyDown(input, { key: 'Enter' })
    await flush()
    expect(setTaskOwner).toHaveBeenCalledWith('task-1', 'Alice')
  })

  it('blur commits the owner', async () => {
    const setTaskOwner = vi.fn().mockResolvedValue(true)
    const ctx = makeCtx({ setTaskOwner })
    render(TaskEditDrawer, {
      props: { task: makeTask(), ctx, onClose: () => {} }
    })
    const input = screen.getByLabelText('Owner')
    await fireEvent.input(input, { target: { value: 'Bob' } })
    await fireEvent.blur(input)
    await flush()
    expect(setTaskOwner).toHaveBeenCalledWith('task-1', 'Bob')
  })

  it('empty string clears the owner', async () => {
    const setTaskOwner = vi.fn().mockResolvedValue(true)
    const ctx = makeCtx({ setTaskOwner })
    render(TaskEditDrawer, {
      props: { task: makeTask({ owner: 'Alice' }), ctx, onClose: () => {} }
    })
    const input = screen.getByLabelText('Owner')
    await fireEvent.input(input, { target: { value: '' } })
    await fireEvent.blur(input)
    await flush()
    expect(setTaskOwner).toHaveBeenCalledWith('task-1', '')
  })

  it('reverts local state and shows the error banner on failure', async () => {
    const setTaskOwner = vi.fn().mockRejectedValue(new Error('disk locked'))
    const ctx = makeCtx({ setTaskOwner })
    render(TaskEditDrawer, {
      props: { task: makeTask({ owner: 'Alice' }), ctx, onClose: () => {} }
    })
    const input = screen.getByLabelText('Owner') as HTMLInputElement
    await fireEvent.input(input, { target: { value: 'Bob' } })
    await fireEvent.blur(input)
    await new Promise((r) => setTimeout(r, 10))
    expect(screen.getByText(/Couldn't save/)).toBeTruthy()
    // Reverted to the original committed value.
    expect(input.value).toBe('Alice')
  })
})

describe('TaskEditDrawer — priority editor (#412)', () => {
  beforeEach(() => cleanup())

  it('clicking an option commits via ctx.setTaskPriority', async () => {
    const setTaskPriority = vi.fn().mockResolvedValue(true)
    const ctx = makeCtx({ setTaskPriority })
    render(TaskEditDrawer, {
      props: { task: makeTask({ priority: 3 }), ctx, onClose: () => {} }
    })
    await fireEvent.click(screen.getByRole('radio', { name: 'Critical' }))
    expect(setTaskPriority).toHaveBeenCalledWith('task-1', 1)
  })

  it('arrow-key navigation changes the selection (WCAG 2.1.1)', async () => {
    const setTaskPriority = vi.fn().mockResolvedValue(true)
    const ctx = makeCtx({ setTaskPriority })
    const { container } = render(TaskEditDrawer, {
      props: { task: makeTask({ priority: 3 }), ctx, onClose: () => {} }
    })
    const rg = container.querySelector(
      '[aria-labelledby="task-priority-label"]'
    ) as HTMLElement
    // Start at Low (priority 3, index 2). ArrowLeft → Normal (priority 2).
    await fireEvent.keyDown(rg, { key: 'ArrowLeft' })
    expect(setTaskPriority).toHaveBeenCalledWith('task-1', 2)
    // ArrowRight from Normal wraps forward; here test Home jumps to Critical.
    await fireEvent.keyDown(rg, { key: 'Home' })
    expect(setTaskPriority).toHaveBeenLastCalledWith('task-1', 1)
  })

  it('reverts local state and shows the error banner on failure', async () => {
    const setTaskPriority = vi.fn().mockRejectedValue(new Error('disk locked'))
    const ctx = makeCtx({ setTaskPriority })
    render(TaskEditDrawer, {
      props: { task: makeTask({ priority: 3 }), ctx, onClose: () => {} }
    })
    await fireEvent.click(screen.getByRole('radio', { name: 'Critical' }))
    await new Promise((r) => setTimeout(r, 10))
    expect(screen.getByText(/Couldn't save/)).toBeTruthy()
  })
})

describe('TaskEditDrawer — tags editor (#412)', () => {
  beforeEach(() => cleanup())

  it('adding a chip commits the full new set via ctx.setTaskTags', async () => {
    const setTaskTags = vi.fn().mockResolvedValue(true)
    const ctx = makeCtx({ setTaskTags })
    render(TaskEditDrawer, {
      props: {
        task: makeTask({ tags: 'work|urgent' }),
        ctx,
        onClose: () => {}
      }
    })
    const input = screen.getByLabelText('Add a tag')
    await fireEvent.input(input, { target: { value: 'home' } })
    await fireEvent.keyDown(input, { key: 'Enter' })
    await flush()
    expect(setTaskTags).toHaveBeenCalledWith('task-1', [
      'work',
      'urgent',
      'home'
    ])
  })

  it('removing a chip commits the updated set', async () => {
    const setTaskTags = vi.fn().mockResolvedValue(true)
    const ctx = makeCtx({ setTaskTags })
    render(TaskEditDrawer, {
      props: {
        task: makeTask({ tags: 'work|urgent' }),
        ctx,
        onClose: () => {}
      }
    })
    await fireEvent.click(screen.getByLabelText('Remove tag urgent'))
    expect(setTaskTags).toHaveBeenCalledWith('task-1', ['work'])
  })

  it('reverts local state and shows the error banner on failure', async () => {
    const setTaskTags = vi.fn().mockRejectedValue(new Error('disk locked'))
    const ctx = makeCtx({ setTaskTags })
    render(TaskEditDrawer, {
      props: { task: makeTask({ tags: 'work' }), ctx, onClose: () => {} }
    })
    const input = screen.getByLabelText('Add a tag')
    await fireEvent.input(input, { target: { value: 'home' } })
    await fireEvent.keyDown(input, { key: 'Enter' })
    await new Promise((r) => setTimeout(r, 10))
    expect(screen.getByText(/Couldn't save/)).toBeTruthy()
  })
})

describe('TaskEditDrawer — title editor (#412)', () => {
  beforeEach(() => cleanup())

  it('editing + Enter commits via ctx.setTaskTitle', async () => {
    const setTaskTitle = vi.fn().mockResolvedValue(true)
    const ctx = makeCtx({ setTaskTitle })
    render(TaskEditDrawer, {
      props: { task: makeTask(), ctx, onClose: () => {} }
    })
    const input = screen.getByLabelText('Task title')
    await fireEvent.input(input, { target: { value: 'Water the garden' } })
    await fireEvent.keyDown(input, { key: 'Enter' })
    await flush()
    expect(setTaskTitle).toHaveBeenCalledWith('task-1', 'Water the garden')
  })

  it('blur commits the title', async () => {
    const setTaskTitle = vi.fn().mockResolvedValue(true)
    const ctx = makeCtx({ setTaskTitle })
    render(TaskEditDrawer, {
      props: { task: makeTask(), ctx, onClose: () => {} }
    })
    const input = screen.getByLabelText('Task title')
    await fireEvent.input(input, { target: { value: 'New title' } })
    await fireEvent.blur(input)
    await flush()
    expect(setTaskTitle).toHaveBeenCalledWith('task-1', 'New title')
  })

  it('reverts local state and shows the error banner on failure', async () => {
    const setTaskTitle = vi.fn().mockRejectedValue(new Error('disk locked'))
    const ctx = makeCtx({ setTaskTitle })
    render(TaskEditDrawer, {
      props: { task: makeTask(), ctx, onClose: () => {} }
    })
    const input = screen.getByLabelText('Task title') as HTMLInputElement
    await fireEvent.input(input, { target: { value: 'New title' } })
    await fireEvent.blur(input)
    await new Promise((r) => setTimeout(r, 10))
    expect(screen.getByText(/Couldn't save/)).toBeTruthy()
    // Reverted to the original committed title.
    expect(input.value).toBe('Water plants')
  })
})
