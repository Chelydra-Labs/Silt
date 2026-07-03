import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest'
import { tick } from 'svelte'
import { render, screen, cleanup, fireEvent } from '@testing-library/svelte'

const mocks = vi.hoisted(() => ({
  sqliteQuery: vi.fn(),
  updateBlockState: vi.fn(),
  blockChangedCallbacks: [] as Array<() => void>
}))

import Tasks from './Tasks.svelte'
import type {
  PluginContext,
  PluginManifest,
  PluginEventName,
  PluginEventPayload
} from '../../sdk'
import { v2CtxStubs } from '../../test-helpers'

function makeCtx(): PluginContext {
  return {
    activeNotebook: '',
    activeSection: '',
    activePage: '',
    today: todayStr(),
    sqliteQuery: mocks.sqliteQuery,
    updateBlockState: mocks.updateBlockState,
    mutateBlock: vi.fn(),
    updateTaskMeta: vi.fn(),
    getPluginSettings: vi.fn(() => Promise.resolve({})),
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
    ...v2CtxStubs
  }
}

const MANIFEST: PluginManifest = {
  id: 'silt-tasks',
  name: 'Tasks',
  version: '1.0.0'
}

function todayStr() {
  const d = new Date()
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}

function yesterdayStr() {
  const d = new Date()
  d.setDate(d.getDate() - 1)
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}

function dateOffsetStr(days: number) {
  const d = new Date()
  d.setDate(d.getDate() + days)
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}

async function flush() {
  await tick()
  await new Promise((r) => setTimeout(r, 0))
}

interface TaskRow {
  id: string
  notebook: string
  section: string
  page: string
  file_date: string
  line_number?: number
  clean_content: string
  status: string
  owner: string
  start_date: string
  due_date: string
  priority: number
  pinned?: boolean
}

function task(
  id: string,
  content: string,
  overrides: Partial<TaskRow> = {}
): TaskRow {
  const today = todayStr()
  return {
    id,
    notebook: '.silt',
    section: '',
    page: 'tasks',
    file_date: today,
    line_number: 1,
    clean_content: content,
    status: 'TODO',
    owner: '',
    start_date: '',
    due_date: today,
    priority: 0,
    ...overrides
  }
}

describe('Tasks view', () => {
  beforeEach(() => {
    mocks.sqliteQuery.mockReset()
    mocks.updateBlockState.mockReset()
    mocks.updateBlockState.mockResolvedValue(true)
    mocks.blockChangedCallbacks.length = 0
  })

  afterEach(() => {
    cleanup()
  })

  it('renders undated tasks under a No Date group (#370 AC1)', async () => {
    mocks.sqliteQuery.mockImplementation(async (sql: string) => {
      if (sql.includes("status != 'DONE'")) {
        return {
          rows: [task('u1', 'undated task', { due_date: '' })],
          truncated: false
        }
      }
      return { rows: [], truncated: false }
    })

    render(Tasks, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    // The undated row's data-block-id should live inside a section
    // whose data-group="undated".
    const row = document.querySelector('[data-block-id="u1"]')
    expect(row).toBeInTheDocument()
    const section = row?.closest('[data-group]')
    expect(section?.getAttribute('data-group')).toBe('undated')
    expect(section?.getAttribute('aria-label')).toBe('No Date')
  })

  it('renders a dated task under Today and not under No Date (#370 AC2)', async () => {
    mocks.sqliteQuery.mockImplementation(async (sql: string) => {
      if (sql.includes("status != 'DONE'")) {
        return {
          rows: [
            task('t1', 'today task', {
              notebook: 'Work',
              section: 'Journal',
              page: 'Daily'
            })
          ],
          truncated: false
        }
      }
      return { rows: [], truncated: false }
    })

    render(Tasks, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    const row = document.querySelector('[data-block-id="t1"]')
    expect(row).toBeInTheDocument()
    const section = row?.closest('[data-group]')
    expect(section?.getAttribute('data-group')).toBe('today')
    expect(section?.getAttribute('aria-label')).toBe('Today')
    // The undated section is not rendered (no undated rows → no group).
    expect(document.querySelector('[data-group="undated"]')).toBeNull()
  })

  it('renders overdue tasks in a visually-distinct group (#370 AC3)', async () => {
    const ymd = yesterdayStr()
    mocks.sqliteQuery.mockImplementation(async (sql: string) => {
      if (sql.includes("status != 'DONE'")) {
        return {
          rows: [task('o1', 'overdue task', { due_date: ymd })],
          truncated: false
        }
      }
      return { rows: [], truncated: false }
    })

    render(Tasks, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    const row = document.querySelector('[data-block-id="o1"]')
    expect(row).toBeInTheDocument()
    const section = row?.closest('[data-group]')
    expect(section?.getAttribute('data-group')).toBe('overdue')
    // The Overdue section's h2 carries the error-tone class for
    // visual distinction from Today/Upcoming/No Date.
    const heading = section?.querySelector('h2')
    expect(heading?.className).toContain('text-error')
  })

  it('Completed group is collapsed by default and expands on click (#370 AC4)', async () => {
    mocks.sqliteQuery.mockImplementation(async (sql: string) => {
      if (sql.includes("status != 'DONE'")) {
        return { rows: [], truncated: false }
      }
      if (sql.includes("status = 'DONE'")) {
        return {
          rows: [
            {
              id: 'd1',
              notebook: '.silt',
              section: '',
              page: 'tasks',
              file_date: todayStr(),
              clean_content: 'a done task',
              status: 'DONE'
            }
          ],
          truncated: false
        }
      }
      return { rows: [], truncated: false }
    })

    render(Tasks, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    const toggle = screen.getByTestId('tasks-completed-toggle')
    expect(toggle).toBeInTheDocument()
    expect(toggle.getAttribute('aria-expanded')).toBe('false')
    expect(screen.queryByTestId('tasks-completed-list')).toBeNull()

    await fireEvent.click(toggle)
    await flush()
    expect(toggle.getAttribute('aria-expanded')).toBe('true')
    expect(screen.getByTestId('tasks-completed-list')).toBeInTheDocument()
    expect(document.querySelector('[data-block-id="d1"]')).toBeInTheDocument()
  })

  it('header count reflects open tasks only (#370 AC5)', async () => {
    mocks.sqliteQuery.mockImplementation(async (sql: string) => {
      if (sql.includes("status != 'DONE'")) {
        return {
          rows: [task('a', 'open a'), task('b', 'open b', { due_date: '' })],
          truncated: false
        }
      }
      if (sql.includes("status = 'DONE'")) {
        return {
          rows: [
            {
              id: 'c',
              notebook: '.silt',
              section: '',
              page: 'tasks',
              file_date: todayStr(),
              clean_content: 'done c',
              status: 'DONE'
            }
          ],
          truncated: false
        }
      }
      return { rows: [], truncated: false }
    })

    render(Tasks, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    expect(screen.getByTestId('tasks-open-count').textContent).toContain(
      '2 active tasks'
    )
    expect(screen.getByTestId('tasks-completed-toggle').textContent).toContain(
      '1'
    )
  })

  it('mark-done calls updateBlockState with DONE and removes the row (#370 AC6)', async () => {
    mocks.sqliteQuery.mockImplementation(async (sql: string) => {
      if (sql.includes("status != 'DONE'")) {
        return { rows: [task('m1', 'finish me')], truncated: false }
      }
      return { rows: [], truncated: false }
    })

    render(Tasks, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    expect(document.querySelector('[data-block-id="m1"]')).toBeInTheDocument()

    await fireEvent.click(screen.getByRole('button', { name: 'Mark done' }))
    await flush()

    expect(mocks.updateBlockState).toHaveBeenCalledWith('m1', 'DONE')
  })

  it('clicking an open row dispatches navigate-to-block', async () => {
    mocks.sqliteQuery.mockImplementation(async (sql: string) => {
      if (sql.includes("status != 'DONE'")) {
        return { rows: [task('nav1', 'clickable')], truncated: false }
      }
      return { rows: [], truncated: false }
    })

    const handler = vi.fn()
    window.addEventListener('navigate-to-block', handler)

    render(Tasks, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    const row = document.querySelector('[data-block-id="nav1"]')
    expect(row).toBeInTheDocument()
    await fireEvent.click(row!)

    expect(handler).toHaveBeenCalledTimes(1)
    const detail = (handler.mock.calls[0][0] as CustomEvent).detail
    expect(detail.blockId).toBe('nav1')
    expect(detail.notebook).toBe('.silt')
    window.removeEventListener('navigate-to-block', handler)
  })

  it('shows the empty state when no tasks exist', async () => {
    mocks.sqliteQuery.mockResolvedValue({ rows: [], truncated: false })

    render(Tasks, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    expect(screen.getByTestId('tasks-empty')).toBeInTheDocument()
    expect(screen.getByText(/Ctrl\+Shift\+N/)).toBeInTheDocument()
  })

  it('focusBlockId scrolls into view and highlights the targeted row (#374 AC4)', async () => {
    const targetId = 'focus-target-1'
    mocks.sqliteQuery.mockImplementation(async (sql: string) => {
      if (sql.includes("status != 'DONE'")) {
        return { rows: [task(targetId, 'jump here')], truncated: false }
      }
      return { rows: [], truncated: false }
    })

    const scrollIntoViewSpy = vi.fn()
    const originalQuery = HTMLElement.prototype.scrollIntoView
    HTMLElement.prototype.scrollIntoView = scrollIntoViewSpy

    try {
      render(Tasks, {
        ctx: makeCtx(),
        manifest: MANIFEST,
        focusBlockId: targetId
      })
      await flush()
      // Allow the queueMicrotask inside the $effect to run.
      await new Promise((r) => setTimeout(r, 10))
      await flush()

      expect(scrollIntoViewSpy).toHaveBeenCalled()
      const row = document.querySelector(
        `[data-block-id="${targetId}"]`
      ) as HTMLElement | null
      expect(row).toBeTruthy()
      expect(row?.classList.contains('tasks-focused')).toBe(true)
    } finally {
      HTMLElement.prototype.scrollIntoView = originalQuery
    }
  })

  it('SQL pushes undated tasks to the tail of the open list (#370 AC1 sort)', async () => {
    mocks.sqliteQuery.mockResolvedValue({ rows: [], truncated: false })

    render(Tasks, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    const openCall = mocks.sqliteQuery.mock.calls.find((c) =>
      String(c[0]).includes("status != 'DONE'")
    )
    expect(openCall).toBeTruthy()
    const sql = String(openCall![0])
    expect(sql).toMatch(/ORDER BY t\.due_date IS NULL/)
    expect(sql).toMatch(/status != 'DONE'/)
  })

  it('Upcoming group is capped at today+7 (#370 open question #3)', async () => {
    const today = todayStr()
    const today8 = dateOffsetStr(8)
    const today3 = dateOffsetStr(3)
    mocks.sqliteQuery.mockImplementation(async (sql: string) => {
      if (sql.includes("status != 'DONE'")) {
        return {
          rows: [
            task('in8', 'beyond-week', { due_date: today8 }),
            task('in3', 'inside-week', { due_date: today3 })
          ],
          truncated: false
        }
      }
      return { rows: [], truncated: false }
    })

    render(Tasks, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    const insideWeek = document.querySelector('[data-block-id="in3"]')
    expect(insideWeek).toBeInTheDocument()
    expect(
      insideWeek?.closest('[data-group]')?.getAttribute('data-group')
    ).toBe('upcoming')

    // beyond-week is filtered out by the WHERE due_date <= weekAhead
    // bucket → no DOM element with that block id.
    expect(document.querySelector('[data-block-id="in8"]')).toBeNull()
  })
})

describe('Tasks view — truncated footer (#372 hardening)', () => {
  it('renders the truncated footer when the SQLite cap truncated results', async () => {
    mocks.sqliteQuery.mockImplementation(async (sql: string) => {
      if (sql.includes("status != 'DONE'")) {
        // 500-row cap (defensive memory safeguard) hit — surface the
        // notice so the user knows there are hidden rows below.
        return { rows: [task('one', 'only rendered task')], truncated: true }
      }
      return { rows: [], truncated: false }
    })

    render(Tasks, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    expect(screen.getByTestId('tasks-truncated-notice')).toBeInTheDocument()
  })

  it('does not render the truncated footer when results fit the cap', async () => {
    mocks.sqliteQuery.mockResolvedValue({
      rows: [task('one', 'visible task')],
      truncated: false
    })

    render(Tasks, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    expect(screen.queryByTestId('tasks-truncated-notice')).toBeNull()
  })
})
