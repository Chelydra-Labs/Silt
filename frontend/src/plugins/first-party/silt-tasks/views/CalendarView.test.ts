import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest'
import { tick } from 'svelte'
import { render, screen, cleanup, fireEvent } from '@testing-library/svelte'

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
  setCalendarSubMode
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
// so chip richness + drawer rendering don't hit coercion holes.
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
  // The cursor anchors on the real "today" (July 6 2026 in this env), so the
  // visible month is July 2026. Cells use local-date arithmetic → Y-M-D where
  // M and the year come from the env's clock.
  const d = new Date()
  const y = d.getFullYear()
  const m = d.getMonth()
  const target = new Date(y, m, dateNum)
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
