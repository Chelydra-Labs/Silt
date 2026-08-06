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

  it('renders all primary metadata controls', () => {
    const ctx = makeCtx()
    render(TaskMetadataSidebar, {
      props: { task: makeTask(), ctx }
    })
    // Title, Status, Due date, Pin, Progress, Estimate, Recurrence, Details.
    expect(screen.getByLabelText('Task title')).toBeTruthy()
    expect(screen.getByText('Status')).toBeTruthy()
    expect(screen.getByText('Due date')).toBeTruthy()
    expect(screen.getByText('Pin')).toBeTruthy()
    expect(screen.getByText('Progress')).toBeTruthy()
    expect(screen.getByText('Estimate')).toBeTruthy()
    expect(screen.getByText('Recurrence')).toBeTruthy()
    expect(screen.getByText('Details')).toBeTruthy()
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
    // The Pin button is the one with aria-pressed containing "Pin" text.
    const pinBtn = Array.from(
      document.querySelectorAll('button[aria-pressed]')
    ).find((b) => b.textContent?.includes('Pin')) as HTMLElement
    expect(pinBtn).toBeTruthy()
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
