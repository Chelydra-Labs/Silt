import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest'
import { tick } from 'svelte'
import {
  render,
  screen,
  cleanup,
  fireEvent,
  within
} from '@testing-library/svelte'

// jsdom polyfills — CalendarView pulls in TaskEditDrawer/TaskSubEditorModal
// (transition:fly + TipTap), which need the Web Animations API + caret rects.
if (!Element.prototype.getAnimations) {
  Element.prototype.getAnimations = () => []
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

// Hoisted mock state — loadCalendarSubMode() reads from the settings module's
// slice (seeded via initTasksSettings in beforeEach), and every SDK setter is
// a spy so the reschedule dispatcher can be asserted.
const mocks = vi.hoisted(() => ({
  tasksSettings: {},
  sqliteQuery: vi.fn(),
  setTaskDueDate: vi.fn().mockResolvedValue(true),
  createTask: vi.fn().mockResolvedValue('new-task-id'),
  updatePluginSetting: vi.fn().mockResolvedValue(true),
  blockChangedCallbacks: [] as Array<() => void>
}))

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

import CalendarView from './CalendarView.svelte'
import type {
  PluginContext,
  PluginEventName,
  PluginEventPayload
} from '../../../sdk'
import { v2CtxStubs } from '../../../test-helpers'
import {
  getTaskHubState,
  resetTaskHubState,
  enterTaskPageRoute,
  clearTaskPageRoute,
  setGroupBy,
  setCalendarSubMode,
  setFilters,
  setWeekStart
} from '../state.svelte'
import { initTasksSettings } from '../settings'

const TODAY = '2026-07-06'

function makeCtx(overrides: Partial<PluginContext> = {}): PluginContext {
  return {
    ...v2CtxStubs,
    activeNotebook: '',
    activeSection: '',
    activePage: '',
    today: TODAY,
    sqliteQuery: mocks.sqliteQuery,
    mutateBlock: vi.fn(),
    updateBlockState: vi.fn(),
    updateTaskMeta: vi.fn(),
    setTaskDueDate: mocks.setTaskDueDate,
    createTask: mocks.createTask,
    getPluginSettings: vi.fn(() => Promise.resolve(mocks.tasksSettings)),
    updatePluginSetting: mocks.updatePluginSetting,
    on: <E extends PluginEventName>(
      event: E,
      cb: (payload: PluginEventPayload<E>) => void
    ) => {
      if (event === 'block:changed') {
        const cbAny = cb as unknown as () => void
        mocks.blockChangedCallbacks.push(cbAny)
        return () => {
          const i = mocks.blockChangedCallbacks.indexOf(cbAny)
          if (i >= 0) mocks.blockChangedCallbacks.splice(i, 1)
        }
      }
      return () => {}
    },
    ...overrides
  }
}

// A row factory covering every TaskDetail field the unified query projects
// (shortlist_rank included) so chip richness + drawer rendering don't hit
// coercion holes.
function row(p: Partial<Record<string, unknown>>): Record<string, unknown> {
  return {
    id: 'r',
    source: 'vault',
    notebook: 'Work',
    section: 'Journal',
    page: 'Daily',
    file_date: TODAY,
    clean_content: 'Task',
    status: 'TODO',
    owner: '',
    start_date: '',
    due_date: '',
    priority: 3,
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
    tags: '',
    is_blocked: 0,
    ...p
  }
}

async function flush() {
  await tick()
  await new Promise((r) => setTimeout(r, 0))
}

// The view issues three queries per reload — a windowed SELECT for the
// visible month/week, a separate undated SELECT, and an all-overdue-open
// SELECT (status != 'DONE' AND due_date < today). All go through buildQuery;
// the undated query is the one whose SQL contains the "due_date IS NULL"
// branch; the overdue query is the one whose SQL contains "t.status != 'DONE'"
// without a `due_date >=` window bound.
async function mockQueries(
  rows: Record<string, unknown>[],
  undatedRows: Record<string, unknown>[] = []
) {
  mocks.sqliteQuery.mockReset()
  mocks.sqliteQuery.mockImplementation(async (sql: string) => {
    if (sql.includes('due_date IS NULL')) {
      return { rows: undatedRows, truncated: false }
    }
    // The all-overdue-open query carries a "status != 'DONE'" filter the
    // windowed query lacks — mirror the WHERE clause so a DONE-overdue row
    // in the windowed result doesn't surface via the overdue path.
    if (sql.includes("status != 'DONE'") && !sql.includes('due_date >=')) {
      return {
        rows: rows.filter((r) => r.status !== 'DONE'),
        truncated: false
      }
    }
    return { rows, truncated: false }
  })
}

async function renderCalendar(opts: { ctx?: PluginContext } = {}) {
  resetTaskHubState()
  setCalendarSubMode('month')
  const ctx = opts.ctx ?? makeCtx()
  const onCountChange = vi.fn()
  render(CalendarView, { ctx, onCountChange })
  await flush()
  return { ctx, onCountChange }
}

function ymdForCell(dateNum: number): string {
  // Cursor is seeded from ctx.today (TODAY), not wall-clock — so the visible
  // month is always July 2026 in this suite. Do not use new Date() here or
  // the helper drifts after month-end and CI fails outside July.
  const [y, m] = TODAY.split('-').map(Number)
  const target = new Date(y, m - 1, dateNum)
  return `${target.getFullYear()}-${String(target.getMonth() + 1).padStart(
    2,
    '0'
  )}-${String(target.getDate()).padStart(2, '0')}`
}

describe('CalendarView — Calendar display mode (#425)', () => {
  beforeEach(async () => {
    mocks.tasksSettings = {}
    mocks.setTaskDueDate.mockReset().mockResolvedValue(true)
    mocks.createTask.mockReset().mockResolvedValue('new-task-id')
    mocks.updatePluginSetting.mockReset().mockResolvedValue(true)
    mocks.blockChangedCallbacks.length = 0
    // Seed the settings module so loadCalendarSubMode() + persistCalendarSubMode()
    // (saveFn) are wired to the mock slice.
    await initTasksSettings(makeCtx())
  })

  afterEach(() => {
    cleanup()
    clearTaskPageRoute()
  })

  it('queries the explicit page route instead of ambient navigation', async () => {
    resetTaskHubState()
    setCalendarSubMode('month')
    enterTaskPageRoute({
      source: 'linked:meetings',
      notebook: 'Work',
      section: 'Meetings',
      page: 'Sprint Review',
      nonce: 'calendar-route'
    })
    await mockQueries([])

    render(CalendarView, { ctx: makeCtx(), onCountChange: vi.fn() })
    await flush()

    expect(mocks.sqliteQuery.mock.calls.length).toBeGreaterThan(0)
    for (const [sql, params] of mocks.sqliteQuery.mock.calls) {
      expect(sql).toContain('b.source = ?')
      expect(params.slice(0, 4)).toEqual([
        'linked:meetings',
        'Work',
        'Meetings',
        'Sprint Review'
      ])
    }
  })

  it('re-queries through the shortlist lens when the hub filter turns on (#1092)', async () => {
    resetTaskHubState()
    setCalendarSubMode('month')
    await mockQueries([])
    render(CalendarView, { ctx: makeCtx(), onCountChange: vi.fn() })
    await flush()

    const baselineCalls = mocks.sqliteQuery.mock.calls.length
    expect(baselineCalls).toBeGreaterThan(0)
    for (const [sql] of mocks.sqliteQuery.mock.calls) {
      expect(sql).not.toContain('shortlist_rank IS NOT NULL')
    }

    setFilters({
      owners: [],
      priorities: [],
      dueDate: '',
      tags: [],
      shortlist: true
    })
    await flush()

    // The reload dependency block must track filters.shortlist: turning the
    // lens on re-runs buildQuery, whose fresh queries carry the predicate.
    const fresh = mocks.sqliteQuery.mock.calls.slice(baselineCalls)
    expect(fresh.length).toBeGreaterThan(0)
    for (const [sql] of fresh) {
      expect(sql).toContain('t.shortlist_rank IS NOT NULL')
    }
  })

  it('shows a user-visible notice when any calendar result is truncated', async () => {
    resetTaskHubState()
    setCalendarSubMode('month')
    mocks.sqliteQuery.mockReset().mockResolvedValue({
      rows: [row({ id: 'truncated-1', due_date: TODAY })],
      truncated: true
    })

    render(CalendarView, { ctx: makeCtx(), onCountChange: vi.fn() })
    await flush()

    expect(screen.getByTestId('calendar-truncated-notice')).toHaveTextContent(
      'Some tasks are hidden'
    )
  })

  // --- Grid rendering ----------------------------------------------------

  it('renders a 6×7 day-cell skeleton while loading (#458)', async () => {
    // Never-resolving query keeps `loading` true so the skeleton renders.
    mocks.sqliteQuery.mockReset()
    mocks.sqliteQuery.mockReturnValue(new Promise(() => {}))
    resetTaskHubState()
    setCalendarSubMode('month')
    render(CalendarView, { ctx: makeCtx(), onCountChange: vi.fn() })
    await tick() // initial mount render (loading starts true)
    expect(screen.getByTestId('tasks-calendar-loading')).toBeTruthy()
  })

  it('renders the month grid container', async () => {
    await mockQueries([])
    await renderCalendar()

    expect(screen.getByTestId('tasks-calendar')).toBeInTheDocument()
    // Day cells carry role="gridcell" + data-celldate for DnD/testing.
    expect(
      document.querySelectorAll('[role="gridcell"]').length
    ).toBeGreaterThan(0)
  })

  it('uses Monday as the first calendar column when configured', async () => {
    await mockQueries([])
    resetTaskHubState()
    setWeekStart('monday')
    setCalendarSubMode('month')
    render(CalendarView, { ctx: makeCtx(), onCountChange: vi.fn() })
    await flush()
    const headers = document.querySelectorAll(
      '[data-testid="tasks-calendar"] [role="columnheader"]'
    )
    expect(headers[0]?.textContent?.trim()).toBe('Mon')
    expect(document.querySelector('[data-celldate="2026-06-29"]')).toBeTruthy()
  })

  it('a task with due_date=today appears in today’s cell', async () => {
    await mockQueries([
      row({ id: 't1', clean_content: 'Standup meeting', due_date: TODAY })
    ])
    await renderCalendar()

    expect(screen.getByText('Standup meeting')).toBeInTheDocument()
    const todayCell = document.querySelector(`[data-celldate="${TODAY}"]`)
    expect(todayCell).toBeTruthy()
    expect(todayCell!.textContent).toContain('Standup meeting')
  })

  describe('month-cell overflow agenda', () => {
    const overflowRows = Array.from({ length: 5 }, (_, index) =>
      row({
        id: `overflow-${index + 1}`,
        clean_content: `Overflow task ${index + 1}`,
        due_date: TODAY
      })
    )

    it('exposes every capped task and returns focus to the trigger on Escape', async () => {
      await mockQueries(overflowRows)
      await renderCalendar()

      expect(screen.queryByText('Overflow task 4')).toBeNull()
      const trigger = screen.getByRole('button', {
        name: `Show 2 more tasks for ${TODAY}`
      })
      await fireEvent.click(trigger)
      await flush()

      const agenda = screen.getByRole('dialog', { name: 'July 6, 2026' })
      expect(agenda).toHaveAttribute('aria-modal', 'false')
      for (let index = 1; index <= 5; index++) {
        expect(within(agenda).getByText(`Overflow task ${index}`)).toBeTruthy()
      }

      const first = within(agenda).getByRole('button', {
        name: /Overflow task 1/i
      })
      const second = within(agenda).getByRole('button', {
        name: /Overflow task 2/i
      })
      expect(document.activeElement).toBe(first)
      await fireEvent.keyDown(first, { key: 'ArrowDown' })
      expect(document.activeElement).toBe(second)

      await fireEvent.keyDown(second, { key: 'Escape' })
      await flush()
      expect(screen.queryByTestId('calendar-day-agenda')).toBeNull()
      expect(document.activeElement).toBe(trigger)
    })

    it('opens a hidden task in the existing edit drawer', async () => {
      await mockQueries(overflowRows)
      await renderCalendar()

      await fireEvent.click(
        screen.getByRole('button', {
          name: `Show 2 more tasks for ${TODAY}`
        })
      )
      await flush()
      const agenda = screen.getByTestId('calendar-day-agenda')
      await fireEvent.click(
        within(agenda).getByRole('button', { name: /Overflow task 5/i })
      )
      await flush()

      expect(screen.queryByTestId('calendar-day-agenda')).toBeNull()
      const drawer = screen.getByRole('dialog', { name: /Overflow task 5/i })
      expect(drawer).toHaveAttribute('aria-modal', 'false')
    })

    it('keeps hidden agenda tasks draggable onto day cells', async () => {
      await mockQueries(overflowRows)
      await renderCalendar()

      await fireEvent.click(
        screen.getByRole('button', {
          name: `Show 2 more tasks for ${TODAY}`
        })
      )
      await flush()
      const hiddenTask = within(
        screen.getByTestId('calendar-day-agenda')
      ).getByRole('button', { name: /Overflow task 4/i })
      expect(hiddenTask).toHaveAttribute('draggable', 'true')

      await fireEvent.dragStart(hiddenTask)
      const targetDate = ymdForCell(7)
      const target = document.querySelector(`[data-celldate="${targetDate}"]`)!
      await fireEvent.dragOver(target)
      await fireEvent.drop(target)
      await flush()

      expect(mocks.setTaskDueDate).toHaveBeenCalledWith(
        'overflow-4',
        targetDate
      )
    })

    it('replaces capped overdue text with an agenda trigger', async () => {
      const overdueRows = Array.from({ length: 4 }, (_, index) =>
        row({
          id: `overdue-overflow-${index + 1}`,
          clean_content: `Hidden overdue ${index + 1}`,
          due_date: `2026-06-0${index + 1}`
        })
      )
      mocks.sqliteQuery.mockReset()
      mocks.sqliteQuery.mockImplementation(async (sql: string) => {
        if (sql.includes('due_date IS NULL')) {
          return { rows: [], truncated: false }
        }
        if (sql.includes("status != 'DONE'") && !sql.includes('due_date >=')) {
          return { rows: overdueRows, truncated: false }
        }
        return { rows: [], truncated: false }
      })
      await renderCalendar()

      const trigger = screen.getByRole('button', {
        name: `Show 2 more overdue tasks for ${TODAY}`
      })
      await fireEvent.click(trigger)
      await flush()

      const agenda = screen.getByTestId('calendar-day-agenda')
      expect(within(agenda).getAllByText(/Hidden overdue/)).toHaveLength(4)
      expect(within(agenda).getAllByText(/Overdue ·/)).toHaveLength(4)
    })
  })

  // --- #414 closure: single-click → drawer (no navigate-to-block) -------

  it('single-click on a chip opens the TaskEditDrawer (does NOT dispatch navigate-to-block)', async () => {
    await mockQueries([
      row({ id: 'c1', clean_content: 'Click me', due_date: TODAY })
    ])
    await renderCalendar()

    // navigate-to-block was the standalone Calendar's chip-click action; the
    // unified Calendar opens the drawer instead. This is the #414 closure.
    const navHandler = vi.fn()
    window.addEventListener('navigate-to-block', navHandler)

    const chip = screen.getByText('Click me')
    await fireEvent.click(chip)
    await flush()

    const dialog = screen.getByRole('dialog')
    expect(dialog).toHaveAttribute('aria-modal', 'false')
    expect(dialog.textContent).toContain('Click me')
    expect(navHandler).not.toHaveBeenCalled()
    expect(chip).toHaveAttribute('aria-current', 'true')
    expect(chip.hasAttribute('data-task-hit')).toBe(true)

    window.removeEventListener('navigate-to-block', navHandler)
  })

  it('Shift+Enter on a chip opens the TaskSubEditorModal', async () => {
    await mockQueries([
      row({ id: 'c1', clean_content: 'Edit subtree', due_date: TODAY })
    ])
    await renderCalendar()

    const chip = screen.getByText('Edit subtree')
    chip.focus()
    await fireEvent.keyDown(chip, { key: 'Enter', shiftKey: true })
    await flush()

    // Sub-editor modal is aria-modal="true" with the parent task text in its
    // labelledby title.
    const modal = screen.getByRole('dialog', { name: 'Edit subtree' })
    expect(modal).toHaveAttribute('aria-modal', 'true')
  })

  // --- Drag-reschedule (ported from Calendar.test.ts) -------------------

  it('dropping a chip on a day cell calls setTaskDueDate(id, thatDate) (#293)', async () => {
    await mockQueries([
      row({ id: 'drag-1', clean_content: 'Drop me', due_date: TODAY })
    ])
    await renderCalendar()

    const chip = screen.getByText('Drop me')
    await fireEvent.dragStart(chip)

    // Drop on the 15th of the visible month.
    const targetKey = ymdForCell(15)
    const cell = document.querySelector(`[data-celldate="${targetKey}"]`)
    expect(cell).toBeTruthy()
    await fireEvent.dragOver(cell!)
    await fireEvent.drop(cell!)
    await flush()

    expect(mocks.setTaskDueDate).toHaveBeenCalledWith('drag-1', targetKey)
  })

  it('Alt+ArrowRight on a focused chip reschedules +1 day (#294)', async () => {
    await mockQueries([
      row({ id: 'drag-1', clean_content: 'Keyboard task', due_date: TODAY })
    ])
    await renderCalendar()

    const chip = screen.getByText('Keyboard task')
    chip.focus()
    await fireEvent.keyDown(chip, { key: 'ArrowRight', altKey: true })
    await flush()

    expect(mocks.setTaskDueDate).toHaveBeenCalledTimes(1)
    const call = mocks.setTaskDueDate.mock.calls[0]
    expect(call[0]).toBe('drag-1')
    // +1 day from today.
    const [y, m, d] = TODAY.split('-').map(Number)
    const next = new Date(y, m - 1, d + 1)
    const expected = `${next.getFullYear()}-${String(
      next.getMonth() + 1
    ).padStart(2, '0')}-${String(next.getDate()).padStart(2, '0')}`
    expect(call[1]).toBe(expected)
  })

  it('dropping a chip on its own current cell is a no-op', async () => {
    await mockQueries([
      row({ id: 'self-1', clean_content: 'Self drop', due_date: TODAY })
    ])
    await renderCalendar()

    const chip = screen.getByText('Self drop')
    await fireEvent.dragStart(chip)
    const cell = document.querySelector(`[data-celldate="${TODAY}"]`)!
    await fireEvent.dragOver(cell)
    await fireEvent.drop(cell)
    await flush()

    expect(mocks.setTaskDueDate).not.toHaveBeenCalled()
  })

  // --- Day-cell quick-add (#368) ----------------------------------------

  it('day-cell + button opens quick-add prefilled with that day', async () => {
    await mockQueries([])
    await renderCalendar()

    // Each + button carries an aria-label keyed to its day's ISO date; pick
    // today's so the prefilled due date is deterministic.
    const todayAddBtn = screen.getByLabelText(`Add task for ${TODAY}`)
    await fireEvent.click(todayAddBtn)
    await flush()

    const input = await screen.findByTestId('quick-add-task-input')
    await fireEvent.input(input, { target: { value: 'New thing' } })
    await fireEvent.keyDown(input, { key: 'Enter' })
    await flush()

    expect(mocks.createTask).toHaveBeenCalledTimes(1)
    expect(mocks.createTask.mock.calls[0][0]).toMatchObject({
      title: 'New thing',
      dueDate: TODAY
    })
  })

  // --- "No Date" strip ---------------------------------------------------

  it('renders the No Date strip with undated tasks', async () => {
    await mockQueries(
      [],
      [
        row({ id: 'u1', clean_content: 'Undated one', due_date: '' }),
        row({ id: 'u2', clean_content: 'Undated two', due_date: '' })
      ]
    )
    await renderCalendar()

    const strip = screen.getByTestId('calendar-no-date-strip')
    expect(strip).toBeInTheDocument()
    expect(strip.textContent).toContain('Undated one')
    expect(strip.textContent).toContain('Undated two')
  })

  it('dragging a No Date chip onto a day cell sets the due date', async () => {
    await mockQueries(
      [],
      [row({ id: 'u1', clean_content: 'Schedule me', due_date: '' })]
    )
    await renderCalendar()

    const chip = screen.getByText('Schedule me')
    await fireEvent.dragStart(chip)

    const targetKey = ymdForCell(20)
    const cell = document.querySelector(`[data-celldate="${targetKey}"]`)!
    await fireEvent.dragOver(cell)
    await fireEvent.drop(cell)
    await flush()

    expect(mocks.setTaskDueDate).toHaveBeenCalledWith('u1', targetKey)
  })

  it('dropping a dated chip on the No Date strip clears the due date', async () => {
    await mockQueries(
      [row({ id: 'dated-1', clean_content: 'Clear me', due_date: TODAY })],
      // One undated row so the strip is visible.
      [row({ id: 'u1', clean_content: 'Undated', due_date: '' })]
    )
    await renderCalendar()

    const chip = screen.getByText('Clear me')
    await fireEvent.dragStart(chip)
    const strip = screen.getByTestId('calendar-no-date-strip')
    await fireEvent.dragOver(strip)
    await fireEvent.drop(strip)
    await flush()

    expect(mocks.setTaskDueDate).toHaveBeenCalledWith('dated-1', '')
  })

  // --- Overdue surfacing -------------------------------------------------

  it('an overdue open task surfaces in today’s cell with error-tone', async () => {
    await mockQueries([
      row({
        id: 'od1',
        clean_content: 'Past due',
        due_date: '2026-07-04',
        status: 'TODO'
      })
    ])
    await renderCalendar()

    const todayCell = document.querySelector(`[data-celldate="${TODAY}"]`)!
    expect(todayCell).toBeTruthy()
    const surfaced = todayCell.querySelector('[data-overdue-surfaced="true"]')
    expect(surfaced).toBeTruthy()
    expect(surfaced!.textContent).toContain('Past due')
    // Error-tone styling marks it as overdue, not a regular chip.
    expect(surfaced!.className).toMatch(/border-l-error|bg-error-bg/)
  })

  it('a DONE overdue task does NOT surface in today’s cell', async () => {
    await mockQueries([
      row({
        id: 'done1',
        clean_content: 'Finished late',
        due_date: '2026-07-04',
        status: 'DONE'
      })
    ])
    await renderCalendar()

    const todayCell = document.querySelector(`[data-celldate="${TODAY}"]`)!
    const surfaced = todayCell.querySelector('[data-overdue-surfaced="true"]')
    expect(surfaced).toBeNull()
  })

  // --- Month/Week sub-toggle --------------------------------------------

  it('clicking Week switches the grid and persists calendar_sub_mode', async () => {
    await mockQueries([])
    await renderCalendar()

    const weekBtn = screen.getByTestId('calendar-submode-week')
    await fireEvent.click(weekBtn)
    await flush()

    // setCalendarSubMode updates hub state synchronously, so the grid flips
    // immediately. The 200ms-debounced persist needs a real wait to land.
    expect(getTaskHubState().calendarSubMode).toBe('week')
    await new Promise((r) => setTimeout(r, 300))
    await flush()

    expect(mocks.updatePluginSetting).toHaveBeenCalledWith(
      'calendar_sub_mode',
      'week'
    )
    // The heading changes from "July 2026" to a date-range form.
    const heading = screen.getByRole('heading', { level: 2 })
    expect(heading.textContent).toMatch(/–/)
  })

  it('formats week headings from actual boundaries across months and years', async () => {
    await mockQueries([])
    await renderCalendar()
    await fireEvent.click(screen.getByTestId('calendar-submode-week'))
    await flush()

    window.dispatchEvent(
      new CustomEvent('calendar:focus-date', {
        detail: { date: '2026-07-01' }
      })
    )
    await flush()
    expect(screen.getByRole('heading', { level: 2 })).toHaveTextContent(
      'June 28–July 4, 2026'
    )

    window.dispatchEvent(
      new CustomEvent('calendar:focus-date', {
        detail: { date: '2026-01-01' }
      })
    )
    await flush()
    expect(screen.getByRole('heading', { level: 2 })).toHaveTextContent(
      'December 28, 2025–January 3, 2026'
    )
  })

  // --- Group-by ignored + one-time notice ------------------------------

  it('ignores groupBy and shows a one-time notice when groupBy is incompatible', async () => {
    await mockQueries([
      row({ id: 't1', owner: 'Alice', clean_content: 'Owned', due_date: TODAY })
    ])
    sessionStorage.clear()
    await renderCalendar()
    // Set groupBy AFTER render — renderCalendar resets hub state, which would
    // clobber a pre-render setGroupBy. The $effect re-runs on the change.
    setGroupBy('owner')
    await flush()

    // The grid renders without owner columns — Calendar ignores groupBy.
    expect(screen.getByTestId('tasks-calendar')).toBeInTheDocument()
    // The one-time notice appears.
    const notice = screen.getByTestId('calendar-groupby-notice')
    expect(notice.textContent).toMatch(/group-by is ignored/i)
  })

  it('does not show the group-by notice on the second mount (sessionStorage gate)', async () => {
    await mockQueries([])
    sessionStorage.setItem('silt-tasks:calendar-groupby-notice-shown', '1')
    await renderCalendar()
    setGroupBy('status')
    await flush()

    expect(screen.queryByTestId('calendar-groupby-notice')).toBeNull()
  })

  // --- Focus listener pans the grid ------------------------------------

  it('calendar:focus-date event pans the visible month to include the picked date', async () => {
    await mockQueries([])
    await renderCalendar()

    const before = screen.getByRole('heading', { level: 2 }).textContent
    window.dispatchEvent(
      new CustomEvent('calendar:focus-date', { detail: { date: '2026-09-15' } })
    )
    await flush()

    const after = screen.getByRole('heading', { level: 2 }).textContent
    expect(after).toBe('September 2026')
    expect(after).not.toBe(before)
  })

  // --- Count reporting --------------------------------------------------

  it('reports open/done counts to the hub via onCountChange', async () => {
    const { onCountChange } = await renderCalendarWithRows([
      row({ id: 'o1', status: 'TODO', due_date: TODAY, clean_content: 'open' }),
      row({ id: 'd1', status: 'DONE', due_date: TODAY, clean_content: 'done' })
    ])
    const last = onCountChange.mock.calls.at(-1)
    expect(last?.[0]).toBe(1) // open
    expect(last?.[1]).toBe(1) // done
  })

  it('includes overdue-surfaced tasks in the count (deduped against windowed rows)', async () => {
    // An open overdue task whose due_date falls OUTSIDE the visible window
    // (past month) would be missing from `win` but present in `overdueAll`.
    // The count effect must include it so the header matches Board's tally.
    await mockQueries([
      row({ id: 'o1', status: 'TODO', due_date: TODAY, clean_content: 'open' })
    ])
    // Override the mock to inject an overdue row from a past month that does
    // NOT appear in the windowed result (so it's not deduped out).
    mocks.sqliteQuery.mockImplementation(async (sql: string) => {
      if (sql.includes('due_date IS NULL')) {
        return { rows: [], truncated: false }
      }
      if (sql.includes("status != 'DONE'") && !sql.includes('due_date >=')) {
        return {
          rows: [
            row({
              id: 'od1',
              status: 'TODO',
              due_date: '2026-06-15',
              clean_content: 'past due'
            })
          ],
          truncated: false
        }
      }
      return {
        rows: [
          row({
            id: 'o1',
            status: 'TODO',
            due_date: TODAY,
            clean_content: 'open'
          })
        ],
        truncated: false
      }
    })
    const { onCountChange } = await renderCalendar()

    const last = onCountChange.mock.calls.at(-1)
    expect(last?.[0]).toBe(2) // o1 (windowed) + od1 (overdue-surfaced)
  })

  // --- Keyboard navigation (#425 a11y) ----------------------------------

  it('week view: ArrowRight moves focus to the next day cell', async () => {
    await mockQueries([])
    await renderCalendar()

    // Switch to week view so the grid is a single 7-day row.
    await fireEvent.click(screen.getByTestId('calendar-submode-week'))
    await flush()

    const todayCell = document.querySelector(
      `[data-celldate="${TODAY}"]`
    ) as HTMLElement
    expect(todayCell).toBeTruthy()
    todayCell.focus()
    await fireEvent.keyDown(todayCell, { key: 'ArrowRight' })
    await flush()

    const [y, m, d] = TODAY.split('-').map(Number)
    const next = new Date(y, m - 1, d + 1)
    const expectedKey = `${next.getFullYear()}-${String(
      next.getMonth() + 1
    ).padStart(2, '0')}-${String(next.getDate()).padStart(2, '0')}`
    const focused = document.activeElement as HTMLElement
    expect(focused.getAttribute('data-celldate')).toBe(expectedKey)
  })

  it('month view: ArrowDown moves focus to the cell one week below', async () => {
    await mockQueries([])
    await renderCalendar()

    const todayCell = document.querySelector(
      `[data-celldate="${TODAY}"]`
    ) as HTMLElement
    expect(todayCell).toBeTruthy()
    todayCell.focus()
    await fireEvent.keyDown(todayCell, { key: 'ArrowDown' })
    await flush()

    const [y, m, d] = TODAY.split('-').map(Number)
    const next = new Date(y, m - 1, d + 7)
    const expectedKey = `${next.getFullYear()}-${String(
      next.getMonth() + 1
    ).padStart(2, '0')}-${String(next.getDate()).padStart(2, '0')}`
    const focused = document.activeElement as HTMLElement
    expect(focused.getAttribute('data-celldate')).toBe(expectedKey)
  })

  async function renderCalendarWithRows(
    rows: Record<string, unknown>[],
    undatedRows: Record<string, unknown>[] = []
  ) {
    await mockQueries(rows, undatedRows)
    return renderCalendar()
  }
})

// --- Task context menu (#1116) -------------------------------------------
// Wiring-level coverage for calendar chips (incl. overdue-surfaced); the
// item matrix itself lives in TaskContextMenu.test.ts.

describe('CalendarView — task context menu (#1116)', () => {
  beforeEach(async () => {
    mocks.tasksSettings = {}
    mocks.setTaskDueDate.mockReset().mockResolvedValue(true)
    mocks.createTask.mockReset().mockResolvedValue('new-task-id')
    mocks.updatePluginSetting.mockReset().mockResolvedValue(true)
    mocks.blockChangedCallbacks.length = 0
    await initTasksSettings(makeCtx())
  })

  afterEach(() => {
    cleanup()
    clearTaskPageRoute()
  })

  async function renderCalendarWithMenu(
    rows: Record<string, unknown>[],
    undatedRows: Record<string, unknown>[] = []
  ) {
    await mockQueries(rows, undatedRows)
    const ctx = makeCtx({
      updateBlockState: vi.fn().mockResolvedValue({ ok: true }),
      setTaskPriority: vi.fn().mockResolvedValue(true),
      deleteBlock: vi.fn().mockResolvedValue(true)
    })
    await renderCalendar({ ctx })
    return ctx
  }

  function rootItemLabels(): string[] {
    return Array.from(
      screen
        .getByTestId('task-context-menu')
        .querySelectorAll<HTMLElement>(':scope > [role="menuitem"]')
    ).map((el) => {
      const span = el.querySelector<HTMLSpanElement>('span:not([aria-hidden])')
      return (span ?? el).textContent?.trim() ?? ''
    })
  }

  it('right-click on a chip opens the seven-item menu', async () => {
    await renderCalendarWithMenu([
      row({
        id: 'm1',
        clean_content: 'Menu chip',
        due_date: TODAY,
        priority: 2
      })
    ])
    await fireEvent.contextMenu(screen.getByText('Menu chip'), {
      clientX: 80,
      clientY: 80
    })
    expect(screen.getByTestId('task-context-menu')).toBeInTheDocument()
    expect(rootItemLabels()).toEqual([
      'Mark done',
      'Pin',
      'Due date',
      'Priority',
      'Open in sub-editor',
      'Edit task',
      'Delete task'
    ])
  })

  it('overdue-surfaced chips carry the same menu', async () => {
    const ctx = await renderCalendarWithMenu([
      row({
        id: 'od1',
        clean_content: 'Overdue chip',
        due_date: '2026-07-01',
        status: 'TODO'
      })
    ])
    const chip = document.querySelector(
      'button[data-overdue-surfaced]'
    ) as HTMLElement
    expect(chip).toBeTruthy()
    await fireEvent.contextMenu(chip, { clientX: 5, clientY: 5 })
    expect(screen.getByTestId('task-context-menu')).toBeInTheDocument()
    await fireEvent.click(screen.getByTestId('task-menu-toggle-status'))
    expect(ctx.updateBlockState).toHaveBeenCalledWith('od1', 'DONE')
  })

  it('is state-aware on DONE chips: Reopen label, no Pin', async () => {
    await renderCalendarWithMenu([
      row({
        id: 'd1',
        clean_content: 'Done chip',
        due_date: TODAY,
        status: 'DONE',
        pinned: 1
      })
    ])
    await fireEvent.contextMenu(screen.getByText('Done chip'), {
      clientX: 5,
      clientY: 5
    })
    const labels = rootItemLabels()
    expect(labels[0]).toBe('Reopen')
    expect(labels).not.toContain('Pin')
    expect(labels).not.toContain('Unpin')
  })

  it('Shift+F10 on a chip opens the menu (keyboard parity)', async () => {
    await renderCalendarWithMenu([
      row({ id: 'm1', clean_content: 'Menu chip', due_date: TODAY })
    ])
    const chip = screen.getByText('Menu chip')
    chip.focus()
    await fireEvent.keyDown(chip, { key: 'F10', shiftKey: true })
    expect(screen.getByTestId('task-context-menu')).toBeInTheDocument()
  })

  it('menu actions dispatch the SDK writes: status, due preset, delete', async () => {
    const ctx = await renderCalendarWithMenu([
      row({
        id: 'm1',
        clean_content: 'Menu chip',
        due_date: TODAY,
        priority: 2
      })
    ])
    const chip = screen.getByText('Menu chip')

    await fireEvent.contextMenu(chip, { clientX: 5, clientY: 5 })
    await fireEvent.click(screen.getByTestId('task-menu-toggle-status'))
    expect(ctx.updateBlockState).toHaveBeenCalledWith('m1', 'DONE')

    // The menu's due write rides the same setTaskDueDate mock the DnD path
    // uses, but through the journaled TaskContextMenu path.
    await fireEvent.contextMenu(chip, { clientX: 5, clientY: 5 })
    await fireEvent.click(screen.getByTestId(`task-menu-due-${TODAY}`))
    expect(mocks.setTaskDueDate).toHaveBeenCalledWith('m1', TODAY)

    await fireEvent.contextMenu(chip, { clientX: 5, clientY: 5 })
    expect(screen.getByTestId('task-menu-priority-2').textContent).toContain(
      '(current)'
    )
    await fireEvent.click(screen.getByTestId('task-menu-priority-3'))
    expect(ctx.setTaskPriority).toHaveBeenCalledWith('m1', 3)

    await fireEvent.contextMenu(chip, { clientX: 5, clientY: 5 })
    await fireEvent.click(screen.getByTestId('task-menu-delete'))
    expect(ctx.deleteBlock).not.toHaveBeenCalled()
    await fireEvent.click(screen.getByTestId('task-menu-delete-confirm-cancel'))
    expect(ctx.deleteBlock).not.toHaveBeenCalled()
  })

  it('menu Delete on the 3rd chip keeps focus when "+N more" vanishes', async () => {
    // A 4-task day renders 3 chips plus a "+1 more" overflow control that
    // also carries data-task-hit="overflow"; deleting the third chip used
    // to capture that button as the neighbor, and it unmounts once the
    // count drops to 3 — leaving the rescue focusing a detached node
    // (<body>).
    let deleted = false
    const deleteBlock = vi.fn(async () => {
      deleted = true
      return true
    })
    mocks.sqliteQuery.mockReset()
    mocks.sqliteQuery.mockImplementation(async (sql: string) => {
      if (sql.includes('due_date IS NULL')) {
        return { rows: [], truncated: false }
      }
      if (sql.includes("status != 'DONE'") && !sql.includes('due_date >=')) {
        return { rows: [], truncated: false }
      }
      return {
        rows: deleted
          ? [
              row({ id: 'c1', clean_content: 'First chip', due_date: TODAY }),
              row({
                id: 'c2',
                clean_content: 'Neighbor chip',
                due_date: TODAY
              }),
              row({ id: 'c4', clean_content: 'Hidden chip', due_date: TODAY })
            ]
          : [
              row({ id: 'c1', clean_content: 'First chip', due_date: TODAY }),
              row({
                id: 'c2',
                clean_content: 'Neighbor chip',
                due_date: TODAY
              }),
              row({ id: 'c3', clean_content: 'Menu chip', due_date: TODAY }),
              row({ id: 'c4', clean_content: 'Hidden chip', due_date: TODAY })
            ],
        truncated: false
      }
    })
    const ctx = makeCtx({ deleteBlock })
    await renderCalendar({ ctx })

    const more = screen.getByTestId(`calendar-more-${TODAY}`)
    expect(more).toBeInTheDocument()
    // The exclusion is the explicit overflow marker — not the button's
    // aria-haspopup, which a future task surface may legitimately carry.
    expect(more.getAttribute('data-task-hit')).toBe('overflow')
    await fireEvent.contextMenu(screen.getByText('Menu chip'), {
      clientX: 5,
      clientY: 5
    })
    await fireEvent.click(screen.getByTestId('task-menu-delete'))
    await fireEvent.click(
      screen.getByTestId('task-menu-delete-confirm-confirm')
    )
    expect(ctx.deleteBlock).toHaveBeenCalledWith('c3')

    // Production removal: the write emits block:changed, the debounced
    // reload re-buckets the day (now 3 tasks, no overflow), the rescue
    // polls past it.
    mocks.blockChangedCallbacks.forEach((cb) => cb())
    await new Promise((resolve) => setTimeout(resolve, 300))
    await flush()

    expect(screen.queryByTestId(`calendar-more-${TODAY}`)).toBeNull()
    const neighbor = screen.getByText('Neighbor chip').closest('button')!
    const active = document.activeElement as HTMLElement
    expect(active).not.toBe(document.body)
    expect(active).toBe(neighbor)
    expect(active.isConnected).toBe(true)
  })

  it('Open in sub-editor and Edit task reuse the chip affordance state', async () => {
    await renderCalendarWithMenu([
      row({ id: 'm1', clean_content: 'Menu chip', due_date: TODAY })
    ])
    const chip = screen.getByText('Menu chip')

    // Drawer first: the sub-editor modal renders a second metadata header,
    // so the single-element query must run while only the drawer is up.
    await fireEvent.contextMenu(chip, { clientX: 5, clientY: 5 })
    await fireEvent.click(screen.getByTestId('task-menu-edit'))
    expect(screen.getByTestId('task-primary-header')).toBeInTheDocument()

    await fireEvent.contextMenu(chip, { clientX: 5, clientY: 5 })
    await fireEvent.click(screen.getByTestId('task-menu-sub-editor'))
    expect(
      document.querySelector('[aria-labelledby="sub-editor-title"]')
    ).toBeInTheDocument()
  })

  it('Escape and backdrop dismiss; Alt+Arrow reschedule still works after', async () => {
    await renderCalendarWithMenu([
      row({ id: 'm1', clean_content: 'Menu chip', due_date: TODAY })
    ])
    const chip = screen.getByText('Menu chip')
    chip.focus()
    await fireEvent.keyDown(chip, { key: 'F10', shiftKey: true })
    await fireEvent.keyDown(window, { key: 'Escape' })
    expect(screen.queryByTestId('task-context-menu')).toBeNull()

    await fireEvent.contextMenu(chip, { clientX: 5, clientY: 5 })
    await fireEvent.click(screen.getByTestId('task-menu-backdrop'))
    expect(screen.queryByTestId('task-context-menu')).toBeNull()

    // No-regression: the chip's own Alt+Arrow reschedule is untouched.
    await fireEvent.keyDown(chip, { key: 'ArrowRight', altKey: true })
    expect(mocks.setTaskDueDate).toHaveBeenCalledWith('m1', '2026-07-07')
  })
})

// --- Day-cell context menu (#1119) ----------------------------------------
// The two-item day menu (add task / open agenda) over the existing quick-add
// and agenda paths; chip menus (#1116) and cell click/DnD/keyboard must be
// untouched.

describe('CalendarView — day-cell context menu (#1119)', () => {
  beforeEach(async () => {
    mocks.tasksSettings = {}
    mocks.setTaskDueDate.mockReset().mockResolvedValue(true)
    mocks.createTask.mockReset().mockResolvedValue('new-task-id')
    mocks.updatePluginSetting.mockReset().mockResolvedValue(true)
    mocks.blockChangedCallbacks.length = 0
    await initTasksSettings(makeCtx())
  })

  afterEach(() => {
    cleanup()
    clearTaskPageRoute()
  })

  function dayMenuLabels(): string[] {
    return Array.from(
      screen
        .getByTestId('calendar-day-menu')
        .querySelectorAll<HTMLElement>(':scope > [role="menuitem"]')
    ).map((el) => {
      const span = el.querySelector<HTMLSpanElement>('span:not([aria-hidden])')
      return (span ?? el).textContent?.trim() ?? ''
    })
  }

  async function openDayMenuOn(dateKey: string, x = 300, y = 300) {
    const cell = document.querySelector(
      `[data-celldate="${dateKey}"]`
    ) as HTMLElement
    expect(cell).toBeTruthy()
    await fireEvent.contextMenu(cell, { clientX: x, clientY: y })
    return cell
  }

  it('right-click a day cell opens the two-item menu at the pointer', async () => {
    await mockQueries([])
    await renderCalendar()
    expect(screen.queryByTestId('calendar-day-menu')).toBeNull()

    // 2026-07-15 within the default 1024×768 jsdom viewport: the primitive
    // measures the menu at 0×0 there, so clamping is identity and the menu
    // lands exactly at the dispatched pointer coordinates.
    await openDayMenuOn(ymdForCell(15))

    const menu = screen.getByTestId('calendar-day-menu')
    expect(menu).toHaveAttribute('role', 'menu')
    expect(menu.getAttribute('style')).toContain('left: 300px')
    expect(menu.getAttribute('style')).toContain('top: 300px')
    expect(dayMenuLabels()).toEqual([
      'Add task for July 15, 2026',
      'Open agenda'
    ])

    // A right-click is not a click: the cell's click-to-quick-add stays put.
    expect(screen.queryByTestId('quick-add-task-input')).toBeNull()
  })

  it('"Add task for <day>" opens quick-add prefilled with that day', async () => {
    await mockQueries([])
    await renderCalendar()
    const dateKey = ymdForCell(15)
    await openDayMenuOn(dateKey)

    await fireEvent.click(screen.getByTestId('calendar-day-menu-add'))

    expect(screen.queryByTestId('calendar-day-menu')).toBeNull()
    const input = screen.getByTestId('quick-add-task-input')
    // The quick-add lives inside the menu's day cell, exactly like the
    // calendar-day-add button's instance.
    expect(
      input.closest('[data-celldate]')?.getAttribute('data-celldate')
    ).toBe(dateKey)
    await fireEvent.input(input, { target: { value: 'From day menu' } })
    await fireEvent.keyDown(input, { key: 'Enter' })
    await flush()

    expect(mocks.createTask).toHaveBeenCalledTimes(1)
    expect(mocks.createTask.mock.calls[0][0]).toMatchObject({
      title: 'From day menu',
      dueDate: dateKey
    })
  })

  it('"Open agenda" opens the day agenda and returns focus to the cell', async () => {
    // Four tasks on TODAY: 3 chips + "+1 more" — the agenda is the only way
    // to see the fourth.
    await mockQueries(
      Array.from({ length: 4 }, (_, i) =>
        row({
          id: `a${i + 1}`,
          clean_content: `Agenda task ${i + 1}`,
          due_date: TODAY
        })
      )
    )
    await renderCalendar()
    await openDayMenuOn(TODAY)

    await fireEvent.click(screen.getByTestId('calendar-day-menu-agenda'))

    const agenda = screen.getByTestId('calendar-day-agenda')
    expect(agenda).toBeInTheDocument()
    for (let i = 1; i <= 4; i++) {
      expect(agenda.textContent).toContain(`Agenda task ${i}`)
    }
    // The agenda opens with its first entry focused (shared open path with
    // the "+N more" affordance).
    await flush()
    expect(document.activeElement?.textContent).toContain('Agenda task 1')

    // Escape closes the agenda and restores focus to the anchor — the day
    // cell that opened the menu, not a vanished "+N more" button.
    await fireEvent.keyDown(document.activeElement!, { key: 'Escape' })
    await flush()
    expect(screen.queryByTestId('calendar-day-agenda')).toBeNull()
    const cell = document.querySelector(`[data-celldate="${TODAY}"]`)!
    expect(document.activeElement).toBe(cell)
  })

  it('Shift+F10 and Menu key open the menu from a focused cell; focus returns on close', async () => {
    await mockQueries([])
    await renderCalendar()
    const cell = document.querySelector(
      `[data-celldate="${TODAY}"]`
    ) as HTMLElement
    cell.focus()

    await fireEvent.keyDown(cell, { key: 'F10', shiftKey: true })
    expect(screen.getByTestId('calendar-day-menu')).toBeInTheDocument()
    // The primitive focuses the first item on open.
    expect(document.activeElement).toBe(
      screen.getByTestId('calendar-day-menu-add')
    )

    await fireEvent.keyDown(window, { key: 'Escape' })
    await flush()
    expect(screen.queryByTestId('calendar-day-menu')).toBeNull()
    expect(document.activeElement).toBe(cell)

    // The Menu key takes the same branch.
    await fireEvent.keyDown(cell, { key: 'ContextMenu' })
    expect(screen.getByTestId('calendar-day-menu')).toBeInTheDocument()
  })

  it('dismisses on Escape, backdrop click, calendar scroll, and resize', async () => {
    await mockQueries([])
    await renderCalendar()
    await openDayMenuOn(TODAY)

    // jsdom computes no Tailwind overflow on the calendar's scroll container,
    // so the primitive's scoped scroll listener falls back to document.
    document.dispatchEvent(new Event('scroll', { bubbles: true }))
    await flush()
    expect(screen.queryByTestId('calendar-day-menu')).toBeNull()

    await openDayMenuOn(TODAY)
    window.dispatchEvent(new Event('resize'))
    await flush()
    expect(screen.queryByTestId('calendar-day-menu')).toBeNull()

    await openDayMenuOn(TODAY)
    await fireEvent.click(screen.getByTestId('day-menu-backdrop'))
    await flush()
    expect(screen.queryByTestId('calendar-day-menu')).toBeNull()

    await openDayMenuOn(TODAY)
    await fireEvent.keyDown(window, { key: 'Escape' })
    await flush()
    expect(screen.queryByTestId('calendar-day-menu')).toBeNull()
  })

  it('right-click on a chip or "+N more" keeps their own context behavior, not the day menu', async () => {
    await mockQueries(
      Array.from({ length: 4 }, (_, i) =>
        row({
          id: `c${i + 1}`,
          clean_content: `Chip task ${i + 1}`,
          due_date: TODAY
        })
      )
    )
    await renderCalendar()

    // The event bubbles from the chip to the cell — the cell handler must
    // yield to the chip's task menu (#1116).
    await fireEvent.contextMenu(screen.getByText('Chip task 1'), {
      clientX: 5,
      clientY: 5
    })
    expect(screen.getByTestId('task-context-menu')).toBeInTheDocument()
    expect(screen.queryByTestId('calendar-day-menu')).toBeNull()
    await fireEvent.keyDown(window, { key: 'Escape' })

    // Same guard for the overflow control (a control, not the cell surface).
    await fireEvent.contextMenu(screen.getByTestId(`calendar-more-${TODAY}`), {
      clientX: 5,
      clientY: 5
    })
    expect(screen.queryByTestId('calendar-day-menu')).toBeNull()
    expect(screen.queryByTestId('task-context-menu')).toBeNull()
  })

  it('week-view cells expose the same menu', async () => {
    await mockQueries([])
    await renderCalendar()
    await fireEvent.click(screen.getByTestId('calendar-submode-week'))
    await flush()

    await openDayMenuOn(TODAY)
    expect(dayMenuLabels()).toEqual([
      'Add task for July 6, 2026',
      'Open agenda'
    ])
    await fireEvent.click(screen.getByTestId('calendar-day-menu-add'))

    const input = screen.getByTestId('quick-add-task-input')
    expect(
      input.closest('[data-celldate]')?.getAttribute('data-celldate')
    ).toBe(TODAY)
  })

  it('no-regression: cell click quick-add, Enter quick-add, arrows, drop reschedule', async () => {
    await mockQueries([
      row({ id: 'd1', clean_content: 'Drag me', due_date: TODAY })
    ])
    await renderCalendar()

    // Plain click on a cell still opens quick-add for that day.
    const clickCell = document.querySelector(
      `[data-celldate="${ymdForCell(10)}"]`
    ) as HTMLElement
    await fireEvent.click(clickCell)
    expect(
      screen
        .getByTestId('quick-add-task-input')
        .closest('[data-celldate]')
        ?.getAttribute('data-celldate')
    ).toBe(ymdForCell(10))
    await fireEvent.keyDown(screen.getByTestId('quick-add-task-input'), {
      key: 'Escape'
    })
    await flush()
    expect(screen.queryByTestId('quick-add-task-input')).toBeNull()

    // Enter on a focused cell still opens quick-add (same path the menu
    // item takes).
    const enterCell = document.querySelector(
      `[data-celldate="${ymdForCell(11)}"]`
    ) as HTMLElement
    enterCell.focus()
    await fireEvent.keyDown(enterCell, { key: 'Enter' })
    expect(
      screen
        .getByTestId('quick-add-task-input')
        .closest('[data-celldate]')
        ?.getAttribute('data-celldate')
    ).toBe(ymdForCell(11))

    // Arrow navigation still moves cell focus (Enter left focus on the cell;
    // ArrowDown targets the cell one week below).
    const fromCell = document.querySelector(
      `[data-celldate="${ymdForCell(12)}"]`
    ) as HTMLElement
    fromCell.focus()
    await fireEvent.keyDown(fromCell, { key: 'ArrowDown' })
    await flush()
    expect(document.activeElement?.getAttribute('data-celldate')).toBe(
      ymdForCell(19)
    )

    // Drag-to-reschedule onto a day cell still writes the due date.
    const chip = screen.getByText('Drag me')
    await fireEvent.dragStart(chip)
    const target = document.querySelector(
      `[data-celldate="${ymdForCell(20)}"]`
    )!
    await fireEvent.dragOver(target)
    await fireEvent.drop(target)
    await flush()
    expect(mocks.setTaskDueDate).toHaveBeenCalledWith('d1', ymdForCell(20))
  })
})
