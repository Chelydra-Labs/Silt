import { describe, expect, it, beforeEach, vi } from 'vitest'
import { render, screen, cleanup, fireEvent } from '@testing-library/svelte'

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
  beforeEach(() => cleanup())

  it('keeps primary actions and essential metadata immediately visible', () => {
    const ctx = makeCtx()
    render(TaskMetadataSidebar, {
      props: { task: makeTask(), ctx }
    })
    expect(screen.getByLabelText('Task title')).toBeTruthy()
    expect(screen.getByText('Status')).toBeTruthy()
    expect(screen.getByText('Due date')).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Pin task' })).toBeTruthy()
    expect(screen.getByText('Essentials')).toBeTruthy()
    expect(screen.getByLabelText('Owner')).toBeTruthy()
    expect(screen.getByText('Priority')).toBeTruthy()
    expect(screen.getByLabelText('Start day')).toBeTruthy()
    expect(screen.getByLabelText('Add a tag')).toBeTruthy()
  })

  it('exposes Start day as a labelled local date input', () => {
    render(TaskMetadataSidebar, {
      props: {
        task: makeTask({ start_date: '2026-07-08' }),
        ctx: makeCtx()
      }
    })

    const input = screen.getByLabelText('Start day') as HTMLInputElement
    expect(input.type).toBe('date')
    expect(input.value).toBe('2026-07-08')
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
          setTaskEstimate
        }),
        onMetaChanged
      }
    })

    await fireEvent.change(screen.getByLabelText('Start day'), {
      target: { value: '2026-07-09' }
    })
    await flush()

    expect(setTaskStartDate).toHaveBeenCalledWith('task-1', '2026-07-09')
    expect(setTaskDueDate).not.toHaveBeenCalled()
    expect(setTaskEstimate).not.toHaveBeenCalled()
    expect(onMetaChanged).toHaveBeenCalled()
  })

  it('clears Start day with an empty local date', async () => {
    const setTaskStartDate = vi.fn().mockResolvedValue(true)
    render(TaskMetadataSidebar, {
      props: {
        task: makeTask({ start_date: '2026-07-08' }),
        ctx: makeCtx({ setTaskStartDate })
      }
    })

    await fireEvent.change(screen.getByLabelText('Start day'), {
      target: { value: '' }
    })
    await flush()

    expect(setTaskStartDate).toHaveBeenCalledWith('task-1', '')
  })

  it('rolls Start day back when the write is rejected', async () => {
    const setTaskStartDate = vi
      .fn()
      .mockRejectedValue(new Error('start date rejected'))
    render(TaskMetadataSidebar, {
      props: {
        task: makeTask({ start_date: '2026-07-08' }),
        ctx: makeCtx({ setTaskStartDate })
      }
    })

    const input = screen.getByLabelText('Start day') as HTMLInputElement
    await fireEvent.change(input, { target: { value: '2026-07-09' } })
    await flush()

    expect(setTaskStartDate).toHaveBeenCalledWith('task-1', '2026-07-09')
    expect(input.value).toBe('2026-07-08')
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
})
