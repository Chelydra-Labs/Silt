import { describe, expect, it, beforeEach, vi } from 'vitest'
import {
  render,
  screen,
  cleanup,
  fireEvent,
  within
} from '@testing-library/svelte'

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

// CommentThread (mounted inside the sidebar) reads the local_author pref via
// the settings store; mock it so its reads resolve to controlled empty values.
const sidebarMocks = vi.hoisted(() => ({
  settings: {
    config: {
      plugins: {
        active: [],
        disabled: [],
        plugin_settings: {} as Record<string, Record<string, unknown>>
      }
    }
  }
}))
vi.mock('../../../../settings/store.svelte', () => ({
  settings: sidebarMocks.settings,
  updatePluginSetting: vi.fn().mockResolvedValue(true)
}))

import TaskMetadataSidebar from './TaskMetadataSidebar.svelte'
import {
  initTasksUndo,
  resetTasksUndo,
  tasksUndoAvailable
} from '../undo.svelte'
import type { PluginContext } from '../../../sdk'
import type { TaskDetail } from '../types'
import { v2CtxStubs } from '../../../test-helpers'

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
    due_date: '2026-07-15',
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
    subtask_total: 0,
    subtask_done: 0,
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

describe('TaskMetadataSidebar', () => {
  beforeEach(() => {
    resetTasksUndo()
    cleanup()
  })

  it('keeps primary actions and essential metadata immediately visible', () => {
    const ctx = makeCtx()
    render(TaskMetadataSidebar, {
      props: { task: makeTask(), ctx }
    })
    const header = screen.getByTestId('task-primary-header')
    expect(within(header).getByLabelText('Task title')).toBeTruthy()
    expect(within(header).getByText('Status')).toBeTruthy()
    expect(
      within(header).getByRole('radiogroup', { name: 'Task status' })
    ).toBeTruthy()
    expect(within(header).getByText('Due date')).toBeTruthy()
    expect(within(header).getByText('Start day')).toBeTruthy()
    expect(
      within(header).getByRole('button', { name: 'Pin task' })
    ).toBeTruthy()
    // Priority moved out of Essentials into the primary group (D1).
    expect(within(header).getByText('Priority')).toBeTruthy()
    expect(
      within(header).getByRole('radiogroup', { name: 'Priority' })
    ).toBeTruthy()
    // Priority is a named region like its header siblings.
    expect(
      within(header).getByRole('region', { name: 'Priority' })
    ).toBeTruthy()

    // Essentials now carries only Owner + Tags as full-width rows (D2).
    const essentials = screen.getByRole('region', { name: 'Essentials' })
    expect(within(essentials).getByLabelText('Owner')).toBeTruthy()
    expect(within(essentials).getByLabelText('Add a tag')).toBeTruthy()
    expect(within(essentials).queryByText('Priority')).toBeNull()
  })

  it('exposes Start day as a popover trigger matching Due date', () => {
    render(TaskMetadataSidebar, {
      props: {
        task: makeTask({ start_date: '2026-07-08' }),
        ctx: makeCtx()
      }
    })

    const trigger = document.getElementById(
      'task-start-date-trigger'
    ) as HTMLButtonElement
    expect(trigger).toBeTruthy()
    expect(trigger.tagName).toBe('BUTTON')
    expect(trigger).toHaveAttribute('aria-haspopup', 'dialog')
    expect(trigger.textContent).toContain('2026-07-08')
    expect(document.getElementById('task-start-date-input')).toBeNull()
  })

  it('sets Start day without inferring due date or duration', async () => {
    const setTaskStartDate = vi.fn().mockResolvedValue(true)
    const setTaskDueDate = vi.fn().mockResolvedValue(true)
    const setTaskEstimate = vi.fn().mockResolvedValue(true)
    const onMetaChanged = vi.fn()
    render(TaskMetadataSidebar, {
      props: {
        task: makeTask(),
        ctx: makeCtx({
          setTaskStartDate,
          setTaskDueDate,
          setTaskEstimate,
          today: '2026-07-09'
        }),
        onMetaChanged
      }
    })

    await fireEvent.click(
      document.getElementById('task-start-date-trigger') as HTMLElement
    )
    await fireEvent.click(screen.getByText('Today'))
    await flush()

    expect(setTaskStartDate).toHaveBeenCalledWith('task-1', '2026-07-09')
    expect(setTaskDueDate).not.toHaveBeenCalled()
    expect(setTaskEstimate).not.toHaveBeenCalled()
    expect(onMetaChanged).toHaveBeenCalled()
  })

  it('holds undo until a field write settles, even after the drawer unmounts', async () => {
    const getTaskUndoState = vi.fn().mockResolvedValue({
      canUndo: true,
      remaining: 1,
      changedFields: ['startDate'],
      blockIds: ['task-1'],
      notebook: 'Work',
      section: 'Journal',
      page: 'Daily',
      source: 'vault'
    })
    let settle: (v: boolean) => void = () => {}
    const setTaskStartDate = vi.fn(
      () => new Promise<boolean>((resolve) => (settle = resolve))
    )
    const ctx = makeCtx({ getTaskUndoState, setTaskStartDate })
    initTasksUndo(ctx)
    await flush()
    expect(tasksUndoAvailable()).toBe(true)

    render(TaskMetadataSidebar, {
      props: { task: makeTask(), ctx }
    })

    await fireEvent.click(
      document.getElementById('task-start-date-trigger') as HTMLElement
    )
    await fireEvent.click(screen.getByText('Today'))
    await flush()

    // The optimistic value and the journal disagree until the write lands.
    expect(tasksUndoAvailable()).toBe(false)

    // Closing the drawer mid-write must not release the hold: the async write
    // closure outlives the component.
    cleanup()
    expect(tasksUndoAvailable()).toBe(false)

    settle(true)
    await flush()
    expect(tasksUndoAvailable()).toBe(true)
  })

  it('clears Start day via Clear start day', async () => {
    const setTaskStartDate = vi.fn().mockResolvedValue(true)
    render(TaskMetadataSidebar, {
      props: {
        task: makeTask({ start_date: '2026-07-08' }),
        ctx: makeCtx({ setTaskStartDate })
      }
    })

    await fireEvent.click(
      document.getElementById('task-start-date-trigger') as HTMLElement
    )
    await fireEvent.click(screen.getByText('Clear start day'))
    await flush()

    expect(setTaskStartDate).toHaveBeenCalledWith('task-1', '')
  })

  it('closes open date popovers when the task identity changes', async () => {
    // Host remounts the sidebar on task.id change ({#key task.id} in
    // TaskEditDrawer). Simulate that by unmount + mount with a new task.
    const first = render(TaskMetadataSidebar, {
      props: {
        task: makeTask({ id: 'task-1' }),
        ctx: makeCtx()
      }
    })

    const trigger = document.getElementById(
      'task-start-date-trigger'
    ) as HTMLButtonElement
    await fireEvent.click(trigger)
    await flush()
    expect(
      screen.getByRole('dialog', { name: 'Start day options' })
    ).toBeTruthy()

    first.unmount()
    cleanup()
    render(TaskMetadataSidebar, {
      props: {
        task: makeTask({ id: 'task-2', clean_content: 'Other task' }),
        ctx: makeCtx()
      }
    })
    await flush()

    const nextTrigger = document.getElementById(
      'task-start-date-trigger'
    ) as HTMLButtonElement
    expect(nextTrigger.getAttribute('aria-expanded')).toBe('false')
    expect(
      screen.queryByRole('dialog', { name: 'Start day options' })
    ).toBeNull()
  })

  it('rolls Start day back when the write is rejected', async () => {
    const setTaskStartDate = vi
      .fn()
      .mockRejectedValue(new Error('start date rejected'))
    render(TaskMetadataSidebar, {
      props: {
        task: makeTask({ start_date: '2026-07-08' }),
        ctx: makeCtx({ setTaskStartDate, today: '2026-07-09' })
      }
    })

    const trigger = document.getElementById(
      'task-start-date-trigger'
    ) as HTMLButtonElement
    await fireEvent.click(trigger)
    await fireEvent.click(screen.getByText('Today'))
    await flush()

    expect(setTaskStartDate).toHaveBeenCalledWith('task-1', '2026-07-09')
    expect(trigger.textContent).toContain('2026-07-08')
    expect(screen.getByTestId('task-meta-error')).toHaveTextContent(
      "Couldn't save: start date rejected"
    )
  })

  it('renders flat sections always open with no disclosure toggle', () => {
    const { container } = render(TaskMetadataSidebar, {
      props: { task: makeTask(), ctx: makeCtx() }
    })

    const planning = screen.getByTestId('task-planning-section')
    const activity = screen.getByTestId('task-activity-section')
    // Sections are always open: content renders regardless of task population,
    // with no native <details> to collapse it.
    expect(planning).toHaveTextContent('Progress')
    expect(planning).toHaveTextContent('Recurrence')
    expect(activity).toHaveTextContent('Comments')
    expect(container.querySelectorAll('details, summary')).toHaveLength(0)
  })

  it('renders flat section headers for planning and activity', () => {
    render(TaskMetadataSidebar, {
      props: {
        task: makeTask({
          progress: 35,
          estimate_minutes: 90,
          comments_count: 2
        }),
        ctx: makeCtx()
      }
    })

    // Content renders under real h3 section headers (SR section nav) with no
    // disclosure affordance — the old open/closed summaries are gone.
    expect(
      screen.getByRole('heading', { name: 'Planning & tracking', level: 3 })
    ).toBeInTheDocument()
    expect(
      screen.getByRole('heading', { name: 'Activity', level: 3 })
    ).toBeInTheDocument()
    expect(screen.queryAllByTestId('task-planning-disclosure')).toHaveLength(0)
    expect(screen.queryAllByTestId('task-activity-disclosure')).toHaveLength(0)
  })

  it('wires the responsive container + date field-pair so the narrow modal sidebar stacks to one column', () => {
    const { container } = render(TaskMetadataSidebar, {
      props: { task: makeTask(), ctx: makeCtx() }
    })

    // The sidebar root is a container (container-type: inline-size) and the
    // Due/Start pair carries .field-pair, which the @container (max-width: 360px)
    // rule collapses to one column inside the 320px sub-editor modal aside.
    // Essentials rows are single full-width rows now, so they need no collapse.
    // jsdom can't evaluate container queries, so this guards the mechanism's
    // presence rather than the computed geometry.
    expect(container.querySelector('.task-metadata-sidebar')).toBeTruthy()
    const pairs = container.querySelectorAll('.field-pair')
    expect(pairs).toHaveLength(1)
    expect(pairs[0].className).toContain('grid-cols-2')
  })

  it('an optimistic status edit calls ctx.updateBlockState', async () => {
    const updateBlockState = vi
      .fn()
      .mockResolvedValue({ ok: true, spawnedId: '' })
    const ctx = makeCtx({ updateBlockState })
    render(TaskMetadataSidebar, {
      props: { task: makeTask(), ctx }
    })
    await fireEvent.click(screen.getByRole('radio', { name: 'Done' }))
    expect(updateBlockState).toHaveBeenCalledWith('task-1', 'DONE')
  })

  it('an optimistic pin edit calls ctx.updateTaskMeta', async () => {
    const updateTaskMeta = vi.fn().mockResolvedValue(true)
    const ctx = makeCtx({ updateTaskMeta })
    render(TaskMetadataSidebar, {
      props: { task: makeTask(), ctx }
    })
    // Icon-only pin button: query by its accessible name, not text content.
    const pinBtn = screen.getByRole('button', { name: 'Pin task' })
    await fireEvent.click(pinBtn)
    await flush()
    expect(updateTaskMeta).toHaveBeenCalledWith('task-1', { pinned: true })
  })

  it('fires onMetaChanged after a successful metadata write', async () => {
    const onMetaChanged = vi.fn()
    const updateBlockState = vi
      .fn()
      .mockResolvedValue({ ok: true, spawnedId: '' })
    const ctx = makeCtx({ updateBlockState })
    render(TaskMetadataSidebar, {
      props: { task: makeTask(), ctx, onMetaChanged }
    })
    await fireEvent.click(screen.getByRole('radio', { name: 'Done' }))
    await flush()
    expect(onMetaChanged).toHaveBeenCalled()
  })

  it('fires onMetaChanged after a successful title write', async () => {
    const onMetaChanged = vi.fn()
    const setTaskTitle = vi.fn().mockResolvedValue(true)
    const ctx = makeCtx({ setTaskTitle })
    render(TaskMetadataSidebar, {
      props: { task: makeTask(), ctx, onMetaChanged }
    })
    const input = screen.getByLabelText('Task title')
    await fireEvent.input(input, { target: { value: 'Water the garden' } })
    await fireEvent.keyDown(input, { key: 'Enter' })
    await flush()
    expect(setTaskTitle).toHaveBeenCalledWith('task-1', 'Water the garden')
    expect(onMetaChanged).toHaveBeenCalled()
  })

  it('offers calendar-aware due-date presets with resolved local dates', async () => {
    const ctx = makeCtx({ today: '2026-07-06' })
    render(TaskMetadataSidebar, { props: { task: makeTask(), ctx } })
    await fireEvent.click(screen.getByRole('button', { name: /2026-07-15/ }))
    expect(screen.getByText('End of week')).toBeTruthy()
    expect(screen.getByText('2026-07-11')).toBeTruthy()
    expect(screen.getByText('End of next week')).toBeTruthy()
    expect(screen.getByText('2026-07-18')).toBeTruthy()
    expect(screen.queryByText('Next week')).toBeNull()
  })

  it('shows the full task title as a tooltip on the title input', () => {
    const longTitle = 'Water the plants in the greenhouse before the frost'
    render(TaskMetadataSidebar, {
      props: {
        task: makeTask({ clean_content: longTitle }),
        ctx: makeCtx()
      }
    })
    expect(screen.getByLabelText('Task title')).toHaveAttribute(
      'title',
      longTitle
    )
  })

  it('commits priority from the primary header via ctx.setTaskPriority', async () => {
    const setTaskPriority = vi.fn().mockResolvedValue(true)
    const ctx = makeCtx({ setTaskPriority })
    render(TaskMetadataSidebar, {
      props: { task: makeTask({ priority: 3 }), ctx }
    })

    const group = within(screen.getByTestId('task-primary-header')).getByRole(
      'radiogroup',
      { name: 'Priority' }
    )
    await fireEvent.click(within(group).getByRole('radio', { name: 'Normal' }))
    await flush()
    expect(setTaskPriority).toHaveBeenCalledWith('task-1', 2)
  })

  it('renders one progress slider with no duplicate progress bar', () => {
    render(TaskMetadataSidebar, {
      props: {
        task: makeTask({ progress: 40, subtask_total: 3, subtask_done: 1 }),
        ctx: makeCtx()
      }
    })

    const planning = screen.getByTestId('task-planning-section')
    expect(planning.querySelectorAll('input[type="range"]')).toHaveLength(1)
    // The native range is the only progress affordance — the old decorative
    // bar duplicated it (its inline width style is the tell).
    expect(planning.querySelector('[style*="width"]')).toBeNull()
    expect(screen.getByTestId('task-subtask-count')).toHaveTextContent('[1/3]')
  })

  it('renders every editable field from the no-capability-loss list', () => {
    render(TaskMetadataSidebar, {
      props: {
        task: makeTask({
          owner: 'Alice',
          tags: 'garden|spring',
          progress: 30,
          estimate_minutes: 90,
          recurrence: 'every week'
        }),
        ctx: makeCtx()
      }
    })

    // Primary group
    expect(screen.getByLabelText('Task title')).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Pin task' })).toBeTruthy()
    expect(screen.getByRole('radiogroup', { name: 'Task status' })).toBeTruthy()
    expect(document.getElementById('task-due-date-trigger')).toBeTruthy()
    expect(document.getElementById('task-start-date-trigger')).toBeTruthy()
    expect(screen.getByRole('radiogroup', { name: 'Priority' })).toBeTruthy()
    // Essentials
    expect(screen.getByLabelText('Owner')).toBeTruthy()
    expect(screen.getByLabelText('Add a tag')).toBeTruthy()
    // Planning & tracking
    expect(screen.getByLabelText('Task progress')).toBeTruthy()
    expect(screen.getByTestId('task-estimate-input')).toBeTruthy()
    expect(screen.getByRole('button', { name: /every week/ })).toBeTruthy()
    expect(screen.getByRole('heading', { name: 'Dependencies' })).toBeTruthy()
    // Activity
    expect(screen.getByRole('heading', { name: 'Activity' })).toBeTruthy()
    expect(screen.getByText('0 comments · 0 links · timestamps')).toBeTruthy()
    expect(screen.getByText('Comments')).toBeTruthy()
  })
})
