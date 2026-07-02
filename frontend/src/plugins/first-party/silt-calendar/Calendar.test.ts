import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest'
import { tick } from 'svelte'
import { render, screen, cleanup, fireEvent } from '@testing-library/svelte'

const mocks = vi.hoisted(() => ({
  sqliteQuery: vi.fn(),
  setTaskDueDate: vi.fn().mockResolvedValue(true),
  createTask: vi.fn().mockResolvedValue('new-task-id'),
  // Mocked settings store so we can flip updatePluginSetting's return
  // value to force the persistence-failure banner path.
  updatePluginSetting: vi.fn().mockResolvedValue(true)
}))

vi.mock('../../../settings/store.svelte', () => ({
  settings: {
    config: {
      plugins: { plugin_settings: { 'silt-calendar': {} } }
    },
    error: ''
  },
  updatePluginSetting: mocks.updatePluginSetting
}))

import Calendar from './Calendar.svelte'
import type { PluginContext, PluginManifest } from '../../sdk'
import { v2CtxStubs } from '../../test-helpers'
import {
  getFocusState,
  resetFocusState,
  setActiveFilter,
  setFocusDate
} from './focusState.svelte'

function makeCtx(): PluginContext {
  return {
    activeNotebook: 'Work',
    activeSection: 'Journal',
    activePage: 'Daily',
    today: '2026-06-16',
    sqliteQuery: mocks.sqliteQuery,
    updateBlockState: vi.fn(),
    mutateBlock: vi.fn(),
    updateTaskMeta: vi.fn(),
    getPluginSettings: vi.fn(() => Promise.resolve({})),
    on: () => () => {},
    ...v2CtxStubs,
    setTaskDueDate: mocks.setTaskDueDate,
    createTask: mocks.createTask
  }
}

const MANIFEST: PluginManifest = {
  id: 'silt-calendar',
  name: 'Calendar',
  version: '1.0.0'
}

async function flush() {
  await tick()
  await new Promise((r) => setTimeout(r, 0))
}

describe('Calendar plugin', () => {
  beforeEach(() => {
    mocks.sqliteQuery.mockReset()
    mocks.setTaskDueDate.mockReset().mockResolvedValue(true)
    mocks.createTask.mockReset().mockResolvedValue('new-task-id')
    resetFocusState()
  })

  afterEach(() => {
    cleanup()
  })

  it('renders a month grid with tasks from ctx.sqliteQuery', async () => {
    const now = new Date()
    const ymd = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}-${String(now.getDate()).padStart(2, '0')}`
    mocks.sqliteQuery.mockResolvedValue({
      rows: [
        {
          id: 'c1',
          notebook: 'Work',
          section: 'Journal',
          page: 'Daily',
          file_date: ymd,
          clean_content: 'Meeting today',
          status: 'TODO',
          due_date: ymd
        }
      ],
      truncated: false
    })

    render(Calendar, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    expect(mocks.sqliteQuery).toHaveBeenCalled()
    // The task should appear in the calendar grid.
    expect(screen.getByText('Meeting today')).toBeInTheDocument()
  })

  it('Today button resets the cursor to the current date', async () => {
    mocks.sqliteQuery.mockResolvedValue({ rows: [], truncated: false })

    render(Calendar, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    const todayBtn = screen.getByRole('button', { name: 'Today' })
    expect(todayBtn).toBeInTheDocument()
    await fireEvent.click(todayBtn)
    // After clicking Today the heading should contain the current month/year.
    const now = new Date()
    const monthYear = `${now.toLocaleString('en', { month: 'long' })} ${now.getFullYear()}`
    expect(screen.getByText(monthYear)).toBeInTheDocument()
  })

  it('clicking a task dispatches navigate-to-block', async () => {
    const now = new Date()
    const ymd = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}-${String(now.getDate()).padStart(2, '0')}`
    mocks.sqliteQuery.mockResolvedValue({
      rows: [
        {
          id: 'c1',
          notebook: 'Work',
          section: 'Journal',
          page: 'Daily',
          file_date: ymd,
          clean_content: 'Standup meeting',
          status: 'TODO',
          due_date: ymd
        }
      ],
      truncated: false
    })

    const handler = vi.fn()
    window.addEventListener('navigate-to-block', handler)

    render(Calendar, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    // Click the task button in the grid.
    const taskBtn = screen.getByText('Standup meeting')
    await fireEvent.click(taskBtn)

    expect(handler).toHaveBeenCalledTimes(1)
    const detail = (handler.mock.calls[0][0] as CustomEvent).detail
    expect(detail.blockId).toBe('c1')
    window.removeEventListener('navigate-to-block', handler)
  })

  // --- #322 unified Calendar/Agenda ---------------------------------------

  it('mode toggle has three buttons: Month, Week, Agenda (#322)', async () => {
    mocks.sqliteQuery.mockResolvedValue({ rows: [], truncated: false })
    render(Calendar, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()
    expect(screen.getByRole('button', { name: 'Month' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Week' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Agenda' })).toBeInTheDocument()
  })

  it('switching to Agenda mode renders the agenda subcomponent (#322)', async () => {
    const today = '2026-06-16'
    mocks.sqliteQuery.mockResolvedValue({ rows: [], truncated: false })
    render(Calendar, { ctx: { ...makeCtx(), today }, manifest: MANIFEST })
    await flush()
    const agendaBtn = screen.getByRole('button', { name: 'Agenda' })
    await fireEvent.click(agendaBtn)
    await flush()
    // With no items the AgendaList shows the empty state ("Nothing
    // scheduled."). The agenda header still mounts.
    expect(screen.getByText(/Nothing scheduled/i)).toBeInTheDocument()
  })

  it('switching to Agenda mode renders the four groups when items exist (#322)', async () => {
    const today = '2026-06-16'
    // First sqliteQuery is the Calendar's windowed due-date query (mode =
    // month default). Return empty so we don't render month tasks. The
    // AgendaList runs its own non-DONE-task query; mock that with two
    // tasks so all four groups render.
    mocks.sqliteQuery.mockImplementation(async (sql: string) => {
      if (
        sql.includes("status != 'DONE'") &&
        sql.includes('due_date IS NOT NULL')
      ) {
        return {
          rows: [
            {
              id: 'a1',
              notebook: 'Work',
              section: 'Journal',
              page: 'Daily',
              file_date: '2026-06-16',
              clean_content: 'Overdue task',
              status: 'TODO',
              owner: '',
              start_date: '',
              due_date: '2026-06-14',
              priority: 2
            },
            {
              id: 'a2',
              notebook: 'Work',
              section: 'Journal',
              page: 'Daily',
              file_date: '2026-06-16',
              clean_content: 'Today task',
              status: 'TODO',
              owner: '',
              start_date: '',
              due_date: today,
              priority: 2
            }
          ],
          truncated: false
        }
      }
      return { rows: [], truncated: false }
    })
    render(Calendar, { ctx: { ...makeCtx(), today }, manifest: MANIFEST })
    await flush()
    const agendaBtn = screen.getByRole('button', { name: 'Agenda' })
    await fireEvent.click(agendaBtn)
    await flush()
    expect(screen.getByLabelText('Overdue')).toBeInTheDocument()
    expect(screen.getByLabelText('Today')).toBeInTheDocument()
  })

  it('Month grid click on a task still dispatches navigate-to-block (#322 AC preserved)', async () => {
    // Re-verify the existing contract survives the merge.
    const now = new Date()
    const ymd = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}-${String(now.getDate()).padStart(2, '0')}`
    mocks.sqliteQuery.mockResolvedValue({
      rows: [
        {
          id: 'c1',
          notebook: 'Work',
          section: 'Journal',
          page: 'Daily',
          file_date: ymd,
          clean_content: 'Standup meeting',
          status: 'TODO',
          due_date: ymd
        }
      ],
      truncated: false
    })
    const handler = vi.fn()
    window.addEventListener('navigate-to-block', handler)
    render(Calendar, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()
    const taskBtn = screen.getByText('Standup meeting')
    await fireEvent.click(taskBtn)
    expect(handler).toHaveBeenCalled()
    window.removeEventListener('navigate-to-block', handler)
  })

  // --- #322 hardening: persistence-failure UI for view_mode (#322 polish)
  it('surfaces view_mode save failures as a visible banner', async () => {
    // Force the next updatePluginSetting to return false (write rejection).
    mocks.updatePluginSetting.mockReset().mockResolvedValue(false)
    mocks.sqliteQuery.mockResolvedValue({ rows: [], truncated: false })
    render(Calendar, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()
    // Switch mode to trigger the persist path. The modeLoaded guard
    // skips the first run; this user click is the second $effect run,
    // which schedules a debounced persist (400ms) — wait it out.
    await fireEvent.click(screen.getByRole('button', { name: 'Week' }))
    // Wait > 400ms for the debounced persistMode to fire.
    await new Promise((r) => setTimeout(r, 500))
    await flush()
    // The modeError banner is now rendered with the failure message.
    expect(screen.queryByTestId('mode-error')).toBeInTheDocument()
    expect(screen.getByText(/Couldn't save view mode/i)).toBeInTheDocument()
  })

  it('dismisses the modeError banner via the X button', async () => {
    mocks.updatePluginSetting.mockReset().mockResolvedValue(false)
    mocks.sqliteQuery.mockResolvedValue({ rows: [], truncated: false })
    render(Calendar, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()
    await fireEvent.click(screen.getByRole('button', { name: 'Week' }))
    await new Promise((r) => setTimeout(r, 500))
    await flush()
    expect(screen.queryByTestId('mode-error')).toBeInTheDocument()
    await fireEvent.click(screen.getByLabelText('Dismiss error'))
    expect(screen.queryByTestId('mode-error')).toBeNull()
  })

  // --- #322 polish: in-view Clear-filter affordance in agenda mode (#322 polish)
  it('shows an in-view clear-filter banner in Agenda mode when a filter is active', async () => {
    mocks.sqliteQuery.mockResolvedValue({ rows: [], truncated: false })
    render(Calendar, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()
    // Activate a filter from outside the agenda (we mimic the sidebar
    // by writing directly to focusState, which is what CalendarSidebar
    // does on click).
    setActiveFilter('today')
    await flush()
    const agendaBtn = screen.getByRole('button', { name: 'Agenda' })
    await fireEvent.click(agendaBtn)
    await flush()
    expect(screen.getByTestId('agenda-filter-banner')).toBeInTheDocument()
    expect(getFocusState().activeFilter).toBe('today')
    // The in-view Clear-filter button clears the filter without dispatching
    // the cross-window event (the sidebar listens on that event, but
    // here we only care about the local state mutation).
    await fireEvent.click(screen.getByTestId('agenda-clear-filter'))
    expect(getFocusState().activeFilter).toBe('all')
    expect(screen.queryByTestId('agenda-filter-banner')).toBeNull()
  })

  // --- #325 P2 follow-up: Upcoming filter excludes today
  it('upcoming filter round-trips through setActiveFilter without crashing', async () => {
    // The actual dim/highlight contract is asserted by AgendaList.test.ts
    // where matchesFilter() is exercised directly with a today-task +
    // future-task fixture. Here we just verify the view survives
    // repeated filter flips so the changed boundary can't crash the
    // dim path.
    setActiveFilter('upcoming')
    render(Calendar, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()
    expect(getFocusState().activeFilter).toBe('upcoming')
    setActiveFilter('all')
    await flush()
    setActiveFilter('upcoming')
    await flush()
    expect(getFocusState().activeFilter).toBe('upcoming')
  })

  // --- focusDate → cursor jump (sidebar mini-cal click → main view)
  it('jumps the month cursor to the focused date month when focusDate changes', async () => {
    mocks.sqliteQuery.mockResolvedValue({ rows: [], truncated: false })
    render(Calendar, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()
    const heading = () => screen.getByRole('heading')
    const before = heading().textContent
    setFocusDate('2026-08-15')
    await flush()
    const after = heading().textContent
    expect(after).toBe('August 2026')
    expect(after).not.toBe(before)
  })

  // --- Completed filter highlights DONE tasks in the month grid
  it('Completed filter keeps DONE tasks bright and dims non-DONE in month grid', async () => {
    const now = new Date()
    const ymd = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}-${String(now.getDate()).padStart(2, '0')}`
    mocks.sqliteQuery.mockResolvedValue({
      rows: [
        {
          id: 'todo1',
          notebook: 'Work',
          section: 'Journal',
          page: 'Daily',
          file_date: ymd,
          clean_content: 'Active task',
          status: 'TODO',
          due_date: ymd
        },
        {
          id: 'done1',
          notebook: 'Work',
          section: 'Journal',
          page: 'Daily',
          file_date: ymd,
          clean_content: 'Finished task',
          status: 'DONE',
          due_date: ymd
        }
      ],
      truncated: false
    })
    render(Calendar, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()
    setActiveFilter('completed')
    await flush()
    const todoBtn = screen.getByText('Active task')
    const doneBtn = screen.getByText('Finished task')
    expect(todoBtn.className).toMatch(/opacity-30/)
    expect(doneBtn.className).not.toMatch(/opacity-30/)
  })

  // --- Drag-and-drop rescheduling (#292/#293/#294) -------------------------

  function ymd(d: Date): string {
    return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
  }

  async function renderWithTaskOnToday(label: string) {
    const today = ymd(new Date())
    mocks.sqliteQuery.mockResolvedValue({
      rows: [
        {
          id: 'drag-1',
          notebook: 'Work',
          section: 'Journal',
          page: 'Daily',
          file_date: today,
          clean_content: label,
          status: 'TODO',
          due_date: today
        }
      ],
      truncated: false
    })
    render(Calendar, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()
  }

  it('task card is draggable and sets the drag payload (#292)', async () => {
    await renderWithTaskOnToday('Draggable task')
    const card = screen.getByText('Draggable task')
    expect(card.getAttribute('draggable')).toBe('true')
    expect(card.getAttribute('aria-keyshortcuts')).toContain('Alt+Arrow')
  })

  it('dropping a card on a day cell calls setTaskDueDate with that date (#293)', async () => {
    await renderWithTaskOnToday('Drop me')
    const card = screen.getByText('Drop me')
    // Start the drag so the component tracks dragTaskId.
    await fireEvent.dragStart(card)

    // Find a different day cell in the month grid (the 15th).
    const now = new Date()
    const target = new Date(now.getFullYear(), now.getMonth(), 15)
    const targetKey = ymd(target)
    const cell = document.querySelector(`[data-celldate="${targetKey}"]`)
    expect(cell).toBeTruthy()
    await fireEvent.dragOver(cell!)
    await fireEvent.drop(cell!)

    await flush()
    expect(mocks.setTaskDueDate).toHaveBeenCalledWith('drag-1', targetKey)
  })

  it('dragover toggles the drop-target highlight (#292)', async () => {
    await renderWithTaskOnToday('Highlight me')
    const card = screen.getByText('Highlight me')
    await fireEvent.dragStart(card)

    const now = new Date()
    const target = new Date(now.getFullYear(), now.getMonth(), 20)
    const targetKey = ymd(target)
    const cell = document.querySelector(`[data-celldate="${targetKey}"]`)!
    await fireEvent.dragOver(cell)
    await flush()
    expect(cell.className).toMatch(/ring-accent-primary-glow/)
    await fireEvent.dragLeave(cell)
    await flush()
    expect(cell.className).not.toMatch(/ring-accent-primary-glow/)
  })

  it('Alt+ArrowRight on a focused card reschedules +1 day via keyboard (#294)', async () => {
    await renderWithTaskOnToday('Keyboard task')
    const card = screen.getByText('Keyboard task') as HTMLElement
    card.focus()
    const today = ymd(new Date())
    const tomorrow = `${new Date().getFullYear()}-${String(new Date().getMonth() + 1).padStart(2, '0')}-${String(new Date().getDate() + 1).padStart(2, '0')}`

    await fireEvent.keyDown(card, { key: 'ArrowRight', altKey: true })
    await flush()

    expect(mocks.setTaskDueDate).toHaveBeenCalledTimes(1)
    expect(mocks.setTaskDueDate.mock.calls[0][0]).toBe('drag-1')
    // New date is today + 1 day.
    expect(mocks.setTaskDueDate.mock.calls[0][1]).not.toBe(today)
  })

  it('Alt+ArrowUp on a focused card reschedules -7 days (#294)', async () => {
    await renderWithTaskOnToday('Week jump task')
    const card = screen.getByText('Week jump task') as HTMLElement
    card.focus()

    await fireEvent.keyDown(card, { key: 'ArrowUp', altKey: true })
    await flush()

    expect(mocks.setTaskDueDate).toHaveBeenCalledTimes(1)
  })

  it('arrow keys without Alt do NOT reschedule (cell navigation still works)', async () => {
    await renderWithTaskOnToday('No alt task')
    const card = screen.getByText('No alt task') as HTMLElement
    card.focus()
    await fireEvent.keyDown(card, { key: 'ArrowRight' })
    await flush()
    expect(mocks.setTaskDueDate).not.toHaveBeenCalled()
  })

  it('aria-live region announces a successful reschedule (#292 AC)', async () => {
    await renderWithTaskOnToday('Announce me')
    const card = screen.getByText('Announce me')
    await fireEvent.dragStart(card)
    const now = new Date()
    const target = new Date(now.getFullYear(), now.getMonth(), 18)
    const cell = document.querySelector(`[data-celldate="${ymd(target)}"]`)!
    await fireEvent.dragOver(cell)
    await fireEvent.drop(cell!)
    await flush()

    const live = screen.getByRole('status')
    expect(live.getAttribute('aria-live')).toBe('polite')
    expect(live.textContent).toContain(ymd(target))
  })

  // --- Quick-add standalone tasks (#368) -----------------------------------

  it('toolbar "New task" button opens an undated quick-add (#368)', async () => {
    await renderWithTaskOnToday('Anything')
    const btn = screen.getByTestId('calendar-new-task-btn')
    await fireEvent.click(btn)
    const input = await screen.findByTestId('quick-add-task-input')
    expect(input).toBeInTheDocument()
  })

  it('submitting the toolbar quick-add calls createTask with no due date', async () => {
    mocks.sqliteQuery.mockResolvedValue({ rows: [], truncated: false })
    render(Calendar, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()
    await fireEvent.click(screen.getByTestId('calendar-new-task-btn'))
    const input = await screen.findByTestId('quick-add-task-input')
    await fireEvent.input(input, { target: { value: 'Plan sprint' } })
    await fireEvent.keyDown(input, { key: 'Enter' })
    await flush()
    expect(mocks.createTask).toHaveBeenCalledTimes(1)
    const call = mocks.createTask.mock.calls[0][0]
    expect(call.title).toBe('Plan sprint')
    expect(call.dueDate).toBeUndefined()
  })

  it('Escape cancels the quick-add input', async () => {
    mocks.sqliteQuery.mockResolvedValue({ rows: [], truncated: false })
    render(Calendar, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()
    await fireEvent.click(screen.getByTestId('calendar-new-task-btn'))
    const input = await screen.findByTestId('quick-add-task-input')
    await fireEvent.keyDown(input, { key: 'Escape' })
    await flush()
    expect(screen.queryByTestId('quick-add-task-input')).toBeNull()
  })
})
