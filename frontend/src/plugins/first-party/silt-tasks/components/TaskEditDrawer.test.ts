import { describe, expect, it, beforeEach, vi } from 'vitest'
import { tick } from 'svelte'
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

// CommentThread (mounted inside the drawer) reads the local_author pref via
// the settings store; mock it so its reads resolve to controlled empty
// values without exercising the real store's reactive plumbing.
const drawerMocks = vi.hoisted(() => ({
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
  settings: drawerMocks.settings,
  updatePluginSetting: vi.fn().mockResolvedValue(true)
}))

import TaskEditDrawer from './TaskEditDrawer.svelte'
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
    // #417: new [created::]/[completed::]/[order::] fields default to
    // empty/0 (the pre-existing-task case).
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
    render(TaskEditDrawer, {
      props: { task: makeTask(), ctx, onClose: () => {} }
    })
    const header = screen.getByTestId('task-primary-header')
    expect(
      Array.from(header.querySelectorAll('.material-symbols-outlined')).some(
        (icon) => icon.textContent?.trim() === 'event_repeat'
      )
    ).toBe(false)
  })

  it('renders the repeat badge in the header when recurrence is set', () => {
    const ctx = makeCtx()
    render(TaskEditDrawer, {
      props: {
        task: makeTask({ recurrence: 'every week' }),
        ctx,
        onClose: () => {}
      }
    })
    const header = screen.getByTestId('task-primary-header')
    const badge = Array.from(
      header.querySelectorAll('.material-symbols-outlined')
    ).find((icon) => icon.textContent?.trim() === 'event_repeat')
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

  it('renders the six calendar-aware due-date presets', async () => {
    const ctx = makeCtx({ today: '2026-07-02' })
    const { container } = render(TaskEditDrawer, {
      props: { task: makeTask(), ctx, onClose: () => {} }
    })
    await fireEvent.click(
      container.querySelector('button[aria-haspopup="dialog"]') as HTMLElement
    )
    for (const label of [
      'Today',
      'Tomorrow',
      'End of week',
      'End of next week',
      'End of month',
      'End of next month'
    ]) {
      expect(screen.getByText(label)).toBeTruthy()
    }
    expect(screen.queryByText('Next week')).toBeNull()
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
    const updateBlockState = vi
      .fn()
      .mockResolvedValue({ ok: true, spawnedId: '' })
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
    const updateBlockState = vi
      .fn()
      .mockResolvedValue({ ok: true, spawnedId: '' })
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
    const updateBlockState = vi
      .fn()
      .mockResolvedValue({ ok: true, spawnedId: '' })
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
    const updateBlockState = vi
      .fn()
      .mockResolvedValue({ ok: true, spawnedId: '' })
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

  it('rapid arrow-key navigation commits only the final status once (#442)', async () => {
    const updateBlockState = vi
      .fn()
      .mockResolvedValue({ ok: true, spawnedId: '' })
    const ctx = makeCtx({ updateBlockState })
    const { container } = render(TaskEditDrawer, {
      // Unblocked so landing on DONE via arrow doesn't trigger the guard.
      props: { task: makeTask({ status: 'TODO' }), ctx, onClose: () => {} }
    })
    const rg = container.querySelector(
      '[aria-label="Task status"]'
    ) as HTMLElement
    await tick()
    // Two quick ArrowRights within the 200ms window: TODO → DOING → DONE.
    // tick() between presses lets statusState update so the next index is
    // computed correctly, but no 200ms elapses, so both collapse to ONE
    // trailing commit of the final selection (DONE).
    await fireEvent.keyDown(rg, { key: 'ArrowRight' }) // TODO → DOING
    await tick()
    await fireEvent.keyDown(rg, { key: 'ArrowRight' }) // DOING → DONE
    await tick()
    expect(updateBlockState).not.toHaveBeenCalled()
    await new Promise((r) => setTimeout(r, 250)) // past the debounce window
    expect(updateBlockState).toHaveBeenCalledTimes(1)
    expect(updateBlockState).toHaveBeenLastCalledWith('task-1', 'DONE')
  })
})

describe('TaskEditDrawer — source awareness + affordances', () => {
  beforeEach(() => cleanup())

  it('renders aria-pressed as a real boolean (pinned coercion, not INT 0/1)', () => {
    const ctx = makeCtx()
    render(TaskEditDrawer, {
      // SQL delivers pinned as INTEGER 1; cast simulates the wire shapes.
      props: {
        task: makeTask({ pinned: 1 as unknown as boolean }),
        ctx,
        onClose: () => {}
      }
    })
    // Icon-only pin button: query by its accessible name, not text content.
    const pinBtn = screen.getByRole('button', { name: /pin/i })
    // Coerced to a real boolean — never the out-of-spec "1".
    expect(pinBtn.getAttribute('aria-pressed')).toBe('true')
  })

  it('hides "Open source page" and omits breadcrumb for a .silt task', () => {
    const ctx = makeCtx()
    render(TaskEditDrawer, {
      props: {
        task: makeTask({ notebook: '.silt', section: '', page: 'tasks.md' }),
        ctx,
        onClose: () => {}
      }
    })
    expect(screen.queryByText('Standalone task')).toBeNull()
    expect(screen.queryByText('Open source page')).toBeNull()
    expect(screen.queryByText('.silt')).toBeNull()
    expect(screen.queryByText('tasks.md')).toBeNull()
  })

  it('shows "Open source page" for an in-page task', () => {
    const ctx = makeCtx()
    render(TaskEditDrawer, {
      props: { task: makeTask(), ctx, onClose: () => {} }
    })
    expect(screen.getByText('Open source page')).toBeTruthy()
  })

  it('retains the linked source in exact source-page navigation', async () => {
    const ctx = makeCtx()
    const handler = vi.fn()
    window.addEventListener('navigate-to-block', handler)
    render(TaskEditDrawer, {
      props: {
        task: makeTask({ source: 'linked:meetings' }),
        ctx,
        onClose: () => {}
      }
    })

    await fireEvent.click(screen.getByText('Open source page'))
    const detail = (handler.mock.calls[0][0] as CustomEvent).detail
    expect(detail).toEqual({
      notebook: 'Work',
      source: 'linked:meetings',
      section: 'Journal',
      page: 'Daily',
      date: '2026-07-01',
      blockId: 'task-1'
    })
    window.removeEventListener('navigate-to-block', handler)
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

describe('TaskEditDrawer — information architecture and keyboard flow', () => {
  beforeEach(() => cleanup())

  it('uses the full mobile width and widens to 480–540px on desktop', () => {
    render(TaskEditDrawer, {
      props: { task: makeTask(), ctx: makeCtx(), onClose: () => {} }
    })

    expect(screen.getByRole('dialog')).toHaveClass(
      'w-full',
      'sm:w-[480px]',
      'lg:w-[540px]',
      'lg:max-w-xl'
    )
  })

  it('is genuinely non-modal: outside input closes without blocking its target', async () => {
    const onClose = vi.fn()
    render(TaskEditDrawer, {
      props: { task: makeTask(), ctx: makeCtx(), onClose }
    })

    await fireEvent.click(screen.getByRole('dialog'))
    expect(onClose).not.toHaveBeenCalled()

    const outside = document.createElement('button')
    const outsideClick = vi.fn()
    outside.addEventListener('click', outsideClick)
    document.body.appendChild(outside)
    await fireEvent.mouseDown(outside)
    await fireEvent.click(outside)
    expect(onClose).toHaveBeenCalledTimes(1)
    expect(outsideClick).toHaveBeenCalledTimes(1)
    expect(screen.getByRole('dialog')).toHaveAttribute('aria-modal', 'false')
    expect(screen.queryByTestId('task-drawer-backdrop')).toBeNull()
    outside.remove()
  })

  it('does not treat nested popover interaction as an outside click', async () => {
    const onClose = vi.fn()
    const setTaskDueDate = vi.fn().mockResolvedValue(true)
    render(TaskEditDrawer, {
      props: {
        task: makeTask(),
        ctx: makeCtx({ setTaskDueDate }),
        onClose
      }
    })

    await fireEvent.click(screen.getByRole('button', { name: /2026-07-15/ }))
    await fireEvent.click(screen.getByText('Today'))

    expect(setTaskDueDate).toHaveBeenCalledWith('task-1', '2026-07-02')
    expect(onClose).not.toHaveBeenCalled()
  })

  it('keeps title, status, due date, start day, pin, and close in one sticky header', () => {
    render(TaskEditDrawer, {
      props: { task: makeTask(), ctx: makeCtx(), onClose: () => {} }
    })

    const header = screen.getByTestId('task-primary-header')
    expect(header).toHaveClass('sticky', 'top-0')
    expect(header).toContainElement(screen.getByLabelText('Task title'))
    expect(header).toContainElement(
      screen.getByRole('radiogroup', { name: 'Task status' })
    )
    expect(header).toContainElement(
      screen.getByRole('button', { name: /2026-07-15/ })
    )
    expect(header).toContainElement(screen.getByLabelText('Start day'))
    expect(header).toContainElement(
      screen.getByRole('button', { name: /Pin task/ })
    )
    expect(header).toContainElement(
      screen.getByRole('button', { name: 'Close detail panel' })
    )
  })

  it('uses content-aware defaults for planning and activity', () => {
    render(TaskEditDrawer, {
      props: {
        task: makeTask({
          recurrence: 'every week',
          comments_count: 3
        }),
        ctx: makeCtx(),
        onClose: () => {}
      }
    })

    expect(screen.getByTestId('task-planning-disclosure')).toHaveAttribute(
      'open'
    )
    expect(screen.getByTestId('task-activity-disclosure')).toHaveAttribute(
      'open'
    )
  })

  it('Escape closes a nested due-date popover before closing the drawer', async () => {
    const onClose = vi.fn()
    render(TaskEditDrawer, {
      props: { task: makeTask(), ctx: makeCtx(), onClose }
    })
    const dueTrigger = screen.getByRole('button', { name: /2026-07-15/ })
    await fireEvent.click(dueTrigger)
    await tick()
    expect(
      screen.getByRole('dialog', { name: 'Due date options' })
    ).toBeTruthy()

    dueTrigger.focus()
    await fireEvent.keyDown(dueTrigger, { key: 'Escape' })
    await tick()
    expect(dueTrigger).toHaveAttribute('aria-expanded', 'false')
    expect(onClose).not.toHaveBeenCalled()
    expect(document.activeElement).toBe(dueTrigger)

    await fireEvent.keyDown(window, { key: 'Escape' })
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('restores focus to the host trigger after the drawer unmounts', async () => {
    const trigger = document.createElement('button')
    trigger.textContent = 'Open task'
    document.body.appendChild(trigger)
    trigger.focus()
    const onClose = vi.fn()
    const ctx = makeCtx()
    const { rerender } = render(TaskEditDrawer, {
      props: { task: makeTask(), ctx, onClose }
    })
    await tick()

    await fireEvent.keyDown(window, { key: 'Escape' })
    expect(onClose).toHaveBeenCalledTimes(1)
    await rerender({ task: null, ctx, onClose })
    await tick()
    expect(document.activeElement).toBe(trigger)
    trigger.remove()
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
    await tick()
    // Start at Low (priority 3, index 2). ArrowLeft → Normal (priority 2).
    // Commit is debounced ~200ms (#442): wait past the trailing edge.
    await fireEvent.keyDown(rg, { key: 'ArrowLeft' })
    await tick()
    await new Promise((r) => setTimeout(r, 250))
    expect(setTaskPriority).toHaveBeenCalledWith('task-1', 2)
    // Home jumps to Critical (priority 1).
    await fireEvent.keyDown(rg, { key: 'Home' })
    await tick()
    await new Promise((r) => setTimeout(r, 250))
    expect(setTaskPriority).toHaveBeenLastCalledWith('task-1', 1)
  })

  it('rapid arrow-key navigation commits only the final selection once (#442)', async () => {
    const setTaskPriority = vi.fn().mockResolvedValue(true)
    const ctx = makeCtx({ setTaskPriority })
    const { container } = render(TaskEditDrawer, {
      props: { task: makeTask({ priority: 2 }), ctx, onClose: () => {} }
    })
    const rg = container.querySelector(
      '[aria-labelledby="task-priority-label"]'
    ) as HTMLElement
    await tick()
    // Two quick arrows within the 200ms debounce window: Normal(2) → Low(3)
    // → wrap to Critical(1). tick() between presses lets priorityState update
    // so the next index is computed correctly, but no 200ms elapses, so both
    // collapse to ONE trailing commit of the final selection.
    await fireEvent.keyDown(rg, { key: 'ArrowRight' }) // 2 → 3 (Low)
    await tick()
    await fireEvent.keyDown(rg, { key: 'ArrowRight' }) // 3 → 1 (Critical, wrap)
    await tick()
    expect(setTaskPriority).not.toHaveBeenCalled()
    await new Promise((r) => setTimeout(r, 250)) // past the debounce window
    // Exactly one commit, for the final selection (Critical, priority 1).
    expect(setTaskPriority).toHaveBeenCalledTimes(1)
    expect(setTaskPriority).toHaveBeenLastCalledWith('task-1', 1)
  })

  it('catches up a newer selection that landed during a slow in-flight commit (#442)', async () => {
    // Regression guard: before the fix, a debounced flush that fired while a
    // prior IPC was still pending early-returned on `priorityPending` and the
    // newer selection was silently dropped (drawer shows one value, the list
    // view another). The fix re-arms the debouncer from the commit's finally
    // when the local selection has diverged.
    let resolveFirst!: () => void
    const firstInFlight = new Promise<void>((r) => (resolveFirst = r))
    const setTaskPriority = vi
      .fn()
      .mockReturnValueOnce(firstInFlight.then(() => true)) // 1st call: slow
      .mockResolvedValue(true) // subsequent calls: instant
    const ctx = makeCtx({ setTaskPriority })
    const { container } = render(TaskEditDrawer, {
      props: { task: makeTask({ priority: 2 }), ctx, onClose: () => {} }
    })
    const rg = container.querySelector(
      '[aria-labelledby="task-priority-label"]'
    ) as HTMLElement
    await tick()

    // 1st arrow (2→3): debounce fires → commit starts, awaits the slow IPC.
    await fireEvent.keyDown(rg, { key: 'ArrowRight' })
    await tick()
    await new Promise((r) => setTimeout(r, 300)) // past the 200ms debounce
    expect(setTaskPriority).toHaveBeenCalledTimes(1)
    expect(setTaskPriority).toHaveBeenLastCalledWith('task-1', 3)

    // 2nd arrow (3→1) WHILE the 1st IPC is still in-flight. The debounce
    // flush fires but commitPriority early-returns on priorityPending — the
    // catch-up must re-arm it after the 1st commit resolves.
    await fireEvent.keyDown(rg, { key: 'ArrowRight' })
    await tick()
    await new Promise((r) => setTimeout(r, 300)) // flush during in-flight → dropped
    // Still only the 1st call; the 2nd was dropped (in-flight).
    expect(setTaskPriority).toHaveBeenCalledTimes(1)

    // 1st IPC resolves → finally re-arms the debouncer → catch-up commits 1.
    resolveFirst()
    await new Promise((r) => setTimeout(r, 300)) // past the catch-up debounce
    expect(setTaskPriority).toHaveBeenCalledTimes(2)
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

  it('strips a leading # from a typed tag before committing', async () => {
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
    await fireEvent.input(input, { target: { value: '#home' } })
    await fireEvent.keyDown(input, { key: 'Enter' })
    await flush()
    expect(setTaskTags).toHaveBeenCalledWith('task-1', [
      'work',
      'urgent',
      'home'
    ])
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

describe('TaskEditDrawer — estimate editor (#439)', () => {
  beforeEach(() => cleanup())

  it('shows empty estimate when estimate_minutes is null (not 0)', () => {
    const ctx = makeCtx()
    render(TaskEditDrawer, {
      props: {
        task: makeTask({ estimate_minutes: null }),
        ctx,
        onClose: () => {}
      }
    })
    const input = screen.getByTestId('task-estimate-input') as HTMLInputElement
    expect(input.value).toBe('')
  })

  it('formats minutes into the estimate draft (120 → 2h)', () => {
    const ctx = makeCtx()
    render(TaskEditDrawer, {
      props: {
        task: makeTask({ estimate_minutes: 120 }),
        ctx,
        onClose: () => {}
      }
    })
    const input = screen.getByTestId('task-estimate-input') as HTMLInputElement
    expect(input.value).toBe('2h')
  })

  it('formats fractional work-days as Nd not hours (1200 → 2.5d)', () => {
    const ctx = makeCtx()
    render(TaskEditDrawer, {
      props: {
        task: makeTask({ estimate_minutes: 1200 }),
        ctx,
        onClose: () => {}
      }
    })
    const input = screen.getByTestId('task-estimate-input') as HTMLInputElement
    expect(input.value).toBe('2.5d')
  })

  it('formats non-half-day hours as hours not fractional days (540 → 9h)', () => {
    const ctx = makeCtx()
    render(TaskEditDrawer, {
      props: {
        task: makeTask({ estimate_minutes: 540 }),
        ctx,
        onClose: () => {}
      }
    })
    const input = screen.getByTestId('task-estimate-input') as HTMLInputElement
    expect(input.value).toBe('9h')
  })

  it('typing + Enter commits via ctx.setTaskEstimate', async () => {
    const setTaskEstimate = vi.fn().mockResolvedValue(true)
    const ctx = makeCtx({ setTaskEstimate })
    render(TaskEditDrawer, {
      props: { task: makeTask(), ctx, onClose: () => {} }
    })
    const input = screen.getByTestId('task-estimate-input')
    await fireEvent.input(input, { target: { value: '30m' } })
    await fireEvent.keyDown(input, { key: 'Enter' })
    await flush()
    expect(setTaskEstimate).toHaveBeenCalledWith('task-1', '30m')
  })

  it('clearing the field commits empty string', async () => {
    const setTaskEstimate = vi.fn().mockResolvedValue(true)
    const ctx = makeCtx({ setTaskEstimate })
    render(TaskEditDrawer, {
      props: {
        task: makeTask({ estimate_minutes: 60 }),
        ctx,
        onClose: () => {}
      }
    })
    const input = screen.getByTestId('task-estimate-input')
    await fireEvent.input(input, { target: { value: '' } })
    await fireEvent.blur(input)
    await flush()
    expect(setTaskEstimate).toHaveBeenCalledWith('task-1', '')
  })

  it('shows subtask counts next to progress when subtask_total > 0', () => {
    const ctx = makeCtx()
    render(TaskEditDrawer, {
      props: {
        task: makeTask({ subtask_total: 3, subtask_done: 1 }),
        ctx,
        onClose: () => {}
      }
    })
    expect(screen.getByTestId('task-subtask-count').textContent).toBe('[1/3]')
  })

  it('hides subtask counts when subtask_total is 0', () => {
    const ctx = makeCtx()
    render(TaskEditDrawer, {
      props: { task: makeTask({ subtask_total: 0 }), ctx, onClose: () => {} }
    })
    expect(screen.queryByTestId('task-subtask-count')).toBeNull()
  })
})

describe('TaskEditDrawer — comment thread (#430)', () => {
  beforeEach(() => cleanup())

  it('renders the Comments section beneath the metadata', async () => {
    const ctx = makeCtx({
      fetchSubtree: vi.fn().mockResolvedValue([])
    })
    render(TaskEditDrawer, {
      props: { task: makeTask(), ctx, onClose: () => {} }
    })
    // CommentThread mounts as a child; its heading + empty state appear once
    // fetchSubtree resolves. The empty state confirms the wiring (taskId,
    // notebook/section/page, ctx) reaches the component.
    expect(await screen.findByText('Comments')).toBeInTheDocument()
    expect(await screen.findByTestId('comment-empty-state')).toBeInTheDocument()
  })
})
