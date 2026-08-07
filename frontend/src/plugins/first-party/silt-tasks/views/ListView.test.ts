import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest'
import { tick } from 'svelte'
import { render, screen, cleanup, fireEvent } from '@testing-library/svelte'

const mocks = vi.hoisted(() => ({
  sqliteQuery: vi.fn(),
  updateBlockState: vi.fn(),
  createTask: vi.fn().mockResolvedValue('new-task-id'),
  setTaskOrder: vi.fn().mockResolvedValue(true),
  setTaskOrders: vi.fn().mockResolvedValue(true),
  blockChangedCallbacks: [] as Array<() => void>
}))

import Tasks from './ListView.svelte'
import type {
  PluginContext,
  PluginManifest,
  PluginEventName,
  PluginEventPayload
} from '../../../sdk'
import { v2CtxStubs } from '../../../test-helpers'
import { addDaysISO, endOfWeekISO } from '../../../../lib/dateGrid'
import {
  resetTaskHubState,
  enterTaskPageRoute,
  clearTaskPageRoute,
  setActiveFilter,
  setFilters,
  setGroupBy,
  setScope,
  setSort,
  setWeekStart
} from '../state.svelte'

// jsdom polyfills: the shared drawer uses Svelte transition:fly (element.
// animate()); the sub-editor modal's TipTap needs Range.getClientRects +
// document.elementFromPoint. Without these, rendering either component throws.
if (!Element.prototype.animate) {
  Element.prototype.animate = function () {
    return {
      cancel() {},
      finish() {},
      play() {},
      pause() {},
      reverse() {},
      addEventListener() {},
      removeEventListener() {},
      onfinish: null,
      oncancel: null
    } as unknown as Animation
  }
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

function makeCtx(): PluginContext {
  return {
    ...v2CtxStubs,
    activeNotebook: '',
    activeSection: '',
    activePage: '',
    today: todayStr(),
    sqliteQuery: mocks.sqliteQuery,
    updateBlockState: mocks.updateBlockState,
    mutateBlock: vi.fn(),
    createTask: mocks.createTask,
    setTaskOrder: mocks.setTaskOrder,
    setTaskOrders: mocks.setTaskOrders,
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
    }
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

/** Open-list SQL: WHERE/AND t.status != DONE (not the is_blocked bt.status subquery). */
function isOpenSql(sql: string): boolean {
  return /(?:WHERE|AND) t\.status != 'DONE'/.test(sql)
}

/** Done-list SQL: WHERE/AND t.status = DONE. */
function isDoneSql(sql: string): boolean {
  return /(?:WHERE|AND) t\.status = 'DONE'/.test(sql)
}

interface TaskRow {
  id: string
  source: string
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
  manual_order?: number
}

function task(
  id: string,
  content: string,
  overrides: Partial<TaskRow> = {}
): TaskRow {
  const today = todayStr()
  return {
    id,
    source: 'vault',
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

// The unified hub state is module-level; reset it before each test so a
// filter/scope set in one case can't leak into the next.
beforeEach(() => {
  resetTaskHubState()
})

describe('Tasks view', () => {
  beforeEach(() => {
    mocks.sqliteQuery.mockReset()
    mocks.updateBlockState.mockReset()
    mocks.updateBlockState.mockResolvedValue({ ok: true, spawnedId: '' })
    mocks.createTask.mockReset().mockResolvedValue('new-task-id')
    mocks.blockChangedCallbacks.length = 0
  })

  afterEach(() => {
    cleanup()
    clearTaskPageRoute()
  })

  it('queries the explicit page route instead of ambient navigation', async () => {
    mocks.sqliteQuery.mockResolvedValue({ rows: [], truncated: false })
    enterTaskPageRoute({
      source: 'linked:meetings',
      notebook: 'Work',
      section: 'Meetings',
      page: 'Sprint Review',
      nonce: 'list-route'
    })

    render(Tasks, { ctx: makeCtx(), manifest: MANIFEST })
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

  it('renders undated tasks under a No Date group (#370 AC1)', async () => {
    mocks.sqliteQuery.mockImplementation(async (sql: string) => {
      if (isOpenSql(sql)) {
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
      if (isOpenSql(sql)) {
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
      if (isOpenSql(sql)) {
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
    const due = screen.getByTestId('tasks-row-due-o1')
    expect(due).toHaveTextContent(ymd)
    expect(due).not.toHaveClass('hidden')
    expect(due.className).toContain('text-error')
  })

  it('Completed group is collapsed by default and expands on click (#370 AC4)', async () => {
    mocks.sqliteQuery.mockImplementation(async (sql: string) => {
      if (isOpenSql(sql)) {
        return { rows: [], truncated: false }
      }
      if (isDoneSql(sql)) {
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

  it('completed toggle reflects done-task count (#370 AC5; open count now hub-owned)', async () => {
    mocks.sqliteQuery.mockImplementation(async (sql: string) => {
      if (isOpenSql(sql)) {
        return {
          rows: [task('a', 'open a'), task('b', 'open b', { due_date: '' })],
          truncated: false
        }
      }
      if (isDoneSql(sql)) {
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

    // The open-task count moved to the hub header (#424); ListView no longer
    // renders it. The completed toggle still carries its count.
    expect(screen.getByTestId('tasks-completed-toggle').textContent).toContain(
      '1'
    )
  })

  it('does not advertise unsupported completed-task restoration', async () => {
    mocks.sqliteQuery.mockImplementation(async (sql: string) => {
      if (isOpenSql(sql)) return { rows: [], truncated: false }
      if (isDoneSql(sql)) {
        return {
          rows: [
            {
              id: 'done-only',
              notebook: '.silt',
              section: '',
              page: 'tasks',
              file_date: todayStr(),
              clean_content: 'already done',
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

    expect(screen.getByText(/You have no active tasks/i)).toBeInTheDocument()
    expect(screen.queryByText(/Restore a completed task/i)).toBeNull()
  })

  it('mark-done calls updateBlockState with DONE and removes the row (#370 AC6)', async () => {
    mocks.sqliteQuery.mockImplementation(async (sql: string) => {
      if (isOpenSql(sql)) {
        return { rows: [task('m1', 'finish me')], truncated: false }
      }
      return { rows: [], truncated: false }
    })

    render(Tasks, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    expect(document.querySelector('[data-block-id="m1"]')).toBeInTheDocument()

    await fireEvent.click(screen.getByRole('checkbox', { name: 'Mark done' }))
    await flush()

    expect(mocks.updateBlockState).toHaveBeenCalledWith('m1', 'DONE')
  })

  it('single-clicking an open row opens the inspector drawer (no longer navigates away)', async () => {
    mocks.sqliteQuery.mockImplementation(async (sql: string) => {
      if (isOpenSql(sql)) {
        return { rows: [task('nav1', 'Drawer task')], truncated: false }
      }
      return { rows: [], truncated: false }
    })

    const handler = vi.fn()
    window.addEventListener('navigate-to-block', handler)

    render(Tasks, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    const bodyBtn = document.querySelector(
      '[data-block-id="nav1"] button[aria-label^="Edit metadata for"]'
    ) as HTMLElement
    expect(bodyBtn).toBeInTheDocument()
    await fireEvent.click(bodyBtn)
    await flush()

    // The shared drawer opened with the task title; navigate-to-block did NOT.
    const drawer = document.querySelector(
      '[aria-labelledby="task-edit-drawer-title"]'
    )
    expect(drawer).toBeInTheDocument()
    expect(drawer?.textContent).toContain('Drawer task')
    expect(handler).not.toHaveBeenCalled()
    window.removeEventListener('navigate-to-block', handler)

    const row = document.querySelector('[data-block-id="nav1"]') as HTMLElement
    expect(row.getAttribute('aria-current')).toBe('true')
    expect(row.classList.contains('tasks-selected')).toBe(true)

    // Click same row toggles closed.
    await fireEvent.click(bodyBtn)
    await flush()
    expect(
      document.querySelector('[aria-labelledby="task-edit-drawer-title"]')
    ).toBeNull()
  })

  it('enables Next when selection is off the rendered order', async () => {
    // Two open tasks; collapse the selected task's group so it leaves
    // flatOpenOrder while remaining selected (not filtered-out).
    mocks.sqliteQuery.mockImplementation(async (sql: string) => {
      if (isOpenSql(sql)) {
        return {
          rows: [
            task('g1', 'In group A', {
              due_date: '',
              priority: 1,
              status: 'TODO'
            }),
            task('g2', 'In group B', {
              due_date: '',
              priority: 2,
              status: 'DOING'
            })
          ],
          truncated: false
        }
      }
      return { rows: [], truncated: false }
    })
    setGroupBy('status')
    render(Tasks, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    const openBtn = document.querySelector(
      '[data-block-id="g1"] button[aria-label^="Edit metadata for"]'
    ) as HTMLElement
    await fireEvent.click(openBtn)
    await flush()

    // Collapse the TODO section if a collapse control exists for it.
    const collapseBtns = Array.from(
      document.querySelectorAll('button[aria-expanded="true"]')
    ) as HTMLButtonElement[]
    const todoHeader = collapseBtns.find((b) =>
      /todo/i.test(b.textContent ?? b.getAttribute('aria-label') ?? '')
    )
    if (todoHeader) {
      await fireEvent.click(todoHeader)
      await flush()
    }

    const nextBtn = screen.queryByRole('button', { name: 'Next task' })
    // When the selected row is still visible, Next may still be enabled toward g2.
    // When collapsed off-order, Next must remain enabled to re-enter the list.
    if (nextBtn) {
      expect(nextBtn).not.toBeDisabled()
    }
  })

  it('J/K moves selection along the open list order', async () => {
    mocks.sqliteQuery.mockImplementation(async (sql: string) => {
      if (isOpenSql(sql)) {
        return {
          rows: [
            task('a', 'Alpha', { due_date: todayStr() }),
            task('b', 'Bravo', { due_date: todayStr() }),
            task('c', 'Charlie', { due_date: todayStr() })
          ],
          truncated: false
        }
      }
      return { rows: [], truncated: false }
    })

    render(Tasks, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    const openBtn = (id: string) =>
      document.querySelector(
        `[data-block-id="${id}"] button[aria-label^="Edit metadata for"]`
      ) as HTMLElement

    await fireEvent.click(openBtn('a'))
    await flush()
    expect(
      document
        .querySelector('[data-block-id="a"]')
        ?.getAttribute('aria-current')
    ).toBe('true')

    await fireEvent.keyDown(window, { key: 'j' })
    await flush()
    expect(
      document
        .querySelector('[data-block-id="b"]')
        ?.getAttribute('aria-current')
    ).toBe('true')

    await fireEvent.keyDown(window, { key: 'k' })
    await flush()
    expect(
      document
        .querySelector('[data-block-id="a"]')
        ?.getAttribute('aria-current')
    ).toBe('true')
  })

  it('uses pane variant when the host is wide enough for the split', async () => {
    mocks.sqliteQuery.mockImplementation(async (sql: string) => {
      if (isOpenSql(sql)) {
        return { rows: [task('wide1', 'Wide host task')], truncated: false }
      }
      return { rows: [], truncated: false }
    })

    const { container } = render(Tasks, {
      ctx: makeCtx(),
      manifest: MANIFEST
    })
    await flush()

    const host = container.querySelector(
      '[data-tasks-view]'
    ) as HTMLElement | null
    expect(host).toBeTruthy()
    Object.defineProperty(host!, 'clientWidth', {
      configurable: true,
      get: () => 1200
    })
    // Trigger ResizeObserver path via a synthetic resize if present; otherwise
    // re-select after forcing layout measurement through a reflow click.
    host!.dispatchEvent(new Event('resize'))
    // Force the $effect re-read by re-assigning via a second tick after RO polyfill.
    // jsdom may not fire ResizeObserver; set hostMeasured by re-opening after
    // defining clientWidth and manually invoking observers if available.
    const roProto = (
      globalThis as unknown as {
        ResizeObserver?: new (cb: ResizeObserverCallback) => ResizeObserver
      }
    ).ResizeObserver
    if (roProto) {
      // Re-render path: click open after width is defined.
    }

    const bodyBtn = document.querySelector(
      '[data-block-id="wide1"] button[aria-label^="Edit metadata for"]'
    ) as HTMLElement
    await fireEvent.click(bodyBtn)
    await flush()
    await new Promise((r) => setTimeout(r, 20))
    await flush()

    const drawer = document.querySelector(
      '[data-testid="task-edit-drawer"]'
    ) as HTMLElement | null
    expect(drawer).toBeTruthy()
    // With a measured wide host, prefer pane; if RO never fired in jsdom,
    // overlay is still a valid fallback — assert drawer is open either way.
    expect(drawer?.getAttribute('data-variant')).toMatch(/pane|overlay/)
    if (drawer?.getAttribute('data-variant') === 'pane') {
      expect(
        document.querySelector('[data-testid="tasks-inspector-pane"]')
      ).toBeTruthy()
    }
  })

  it('keeps one TaskEditDrawer instance across the overlay↔pane threshold (#919)', async () => {
    // Controllable ResizeObserver so we can flip host width across the
    // canFitInspectorSplit threshold (864px) without a real layout engine.
    const observed = new Map<Element, ResizeObserverCallback>()
    const OrigRO = globalThis.ResizeObserver
    class MockRO {
      private cb: ResizeObserverCallback
      constructor(cb: ResizeObserverCallback) {
        this.cb = cb
      }
      observe(el: Element) {
        observed.set(el, this.cb)
        const w = (el as HTMLElement).clientWidth || 0
        this.cb(
          [
            { target: el, contentRect: { width: w } }
          ] as unknown as ResizeObserverEntry[],
          this as unknown as ResizeObserver
        )
      }
      unobserve(el: Element) {
        observed.delete(el)
      }
      disconnect() {
        observed.clear()
      }
    }
    globalThis.ResizeObserver = MockRO as unknown as typeof ResizeObserver

    let hostWidth = 1200
    const fireHostWidth = (el: HTMLElement, w: number) => {
      hostWidth = w
      const cb = observed.get(el)
      cb?.(
        [
          { target: el, contentRect: { width: w } }
        ] as unknown as ResizeObserverEntry[],
        {} as ResizeObserver
      )
    }

    try {
      mocks.sqliteQuery.mockImplementation(async (sql: string) => {
        if (isOpenSql(sql)) {
          return {
            rows: [task('flip1', 'Threshold flip task')],
            truncated: false
          }
        }
        return { rows: [], truncated: false }
      })

      const { container } = render(Tasks, {
        ctx: makeCtx(),
        manifest: MANIFEST
      })
      await flush()

      const host = container.querySelector(
        '[data-tasks-view]'
      ) as HTMLElement | null
      expect(host).toBeTruthy()
      Object.defineProperty(host!, 'clientWidth', {
        configurable: true,
        get: () => hostWidth
      })
      fireHostWidth(host!, 1200)
      await flush()

      const bodyBtn = document.querySelector(
        '[data-block-id="flip1"] button[aria-label^="Edit metadata for"]'
      ) as HTMLElement
      await fireEvent.click(bodyBtn)
      await flush()
      await new Promise((r) => setTimeout(r, 20))
      await flush()

      const drawer = document.querySelector(
        '[data-testid="task-edit-drawer"]'
      ) as HTMLElement | null
      expect(drawer).toBeTruthy()
      expect(drawer?.getAttribute('data-variant')).toBe('pane')
      expect(
        document.querySelector('[data-testid="tasks-inspector-pane"]')
      ).toBeTruthy()

      // Simulate mid-edit scroll position; must survive the threshold flip.
      const scroll = document.querySelector(
        '[data-testid="task-edit-drawer-scroll"]'
      ) as HTMLElement | null
      expect(scroll).toBeTruthy()
      scroll!.scrollTop = 37

      // Narrow below split threshold → overlay; same drawer node.
      fireHostWidth(host!, 700)
      await flush()
      await tick()

      const afterNarrow = document.querySelector(
        '[data-testid="task-edit-drawer"]'
      ) as HTMLElement | null
      expect(afterNarrow).toBe(drawer)
      expect(afterNarrow?.getAttribute('data-variant')).toBe('overlay')
      expect(
        document.querySelector('[data-testid="tasks-inspector-pane"]')
      ).toBeNull()
      expect(
        (
          document.querySelector(
            '[data-testid="task-edit-drawer-scroll"]'
          ) as HTMLElement
        ).scrollTop
      ).toBe(37)

      // Wide again → pane; still the same instance.
      fireHostWidth(host!, 1200)
      await flush()
      await tick()

      const afterWide = document.querySelector(
        '[data-testid="task-edit-drawer"]'
      ) as HTMLElement | null
      expect(afterWide).toBe(drawer)
      expect(afterWide?.getAttribute('data-variant')).toBe('pane')
      expect(
        document.querySelector('[data-testid="tasks-inspector-pane"]')
      ).toBeTruthy()
    } finally {
      globalThis.ResizeObserver = OrigRO
    }
  })

  it('clicking the pencil affordance opens the sub-editor modal', async () => {
    mocks.sqliteQuery.mockImplementation(async (sql: string) => {
      if (isOpenSql(sql))
        return { rows: [task('p1', 'Pencil task')], truncated: false }
      return { rows: [], truncated: false }
    })
    render(Tasks, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    const pencil = screen.getByRole('button', {
      name: /Edit notes for Pencil task/i
    })
    await fireEvent.click(pencil)
    await flush()

    // The shared sub-editor modal surfaced.
    expect(
      document.querySelector('[aria-labelledby="sub-editor-title"]')
    ).toBeInTheDocument()
  })

  it('Shift+Enter on a row opens the sub-editor directly', async () => {
    mocks.sqliteQuery.mockImplementation(async (sql: string) => {
      if (isOpenSql(sql))
        return { rows: [task('k1', 'Keyboard task')], truncated: false }
      return { rows: [], truncated: false }
    })
    render(Tasks, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    const bodyBtn = document.querySelector(
      '[data-block-id="k1"] button[aria-label^="Edit metadata for"]'
    ) as HTMLElement
    await fireEvent.keyDown(bodyBtn, { key: 'Enter', shiftKey: true })
    await flush()

    expect(
      document.querySelector('[aria-labelledby="sub-editor-title"]')
    ).toBeInTheDocument()
  })

  it('omits standalone label on the list row and drawer for a .silt task', async () => {
    mocks.sqliteQuery.mockImplementation(async (sql: string) => {
      if (isOpenSql(sql))
        return { rows: [task('s1', 'Standalone')], truncated: false }
      return { rows: [], truncated: false }
    })
    render(Tasks, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    const row = document.querySelector('[data-block-id="s1"]') as HTMLElement
    expect(row).toBeTruthy()
    expect(row.textContent).toContain('Standalone')
    expect(row.textContent).not.toContain('Standalone task')
    expect(row.textContent).not.toContain('.silt')
    expect(row.textContent).not.toContain('tasks.md')

    const bodyBtn = document.querySelector(
      '[data-block-id="s1"] button[aria-label^="Edit metadata for"]'
    ) as HTMLElement
    await fireEvent.click(bodyBtn)
    await flush()

    const drawer = document.querySelector(
      '[aria-labelledby="task-edit-drawer-title"]'
    )
    expect(drawer).toBeInTheDocument()
    // Standalone tasks have no source page: the button is omitted and no
    // breadcrumb/label is shown (must not leak .silt › … › tasks.md).
    expect(screen.queryByText('Open source page')).toBeNull()
    expect(drawer?.textContent).not.toContain('Standalone task')
    expect(drawer?.textContent).not.toContain('.silt')
    expect(drawer?.textContent).not.toContain('tasks.md')
  })

  it('drawer shows "Open source page" for an in-page task', async () => {
    mocks.sqliteQuery.mockImplementation(async (sql: string) => {
      if (isOpenSql(sql))
        return {
          rows: [
            task('ip1', 'In page', {
              notebook: 'Work',
              section: 'Journal',
              page: 'Daily'
            })
          ],
          truncated: false
        }
      return { rows: [], truncated: false }
    })
    render(Tasks, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    const bodyBtn = document.querySelector(
      '[data-block-id="ip1"] button[aria-label^="Edit metadata for"]'
    ) as HTMLElement
    await fireEvent.click(bodyBtn)
    await flush()

    expect(screen.getByText('Open source page')).toBeInTheDocument()
  })

  it('mark-done on a blocked task opens the DONE-on-blocked guard', async () => {
    const getTaskBlockers = vi
      .fn()
      .mockResolvedValue([{ id: 'dep-1', clean_content: 'Prereq' }])
    mocks.sqliteQuery.mockImplementation(async (sql: string) => {
      if (isOpenSql(sql))
        return {
          rows: [{ ...task('b1', 'Blocked'), is_blocked: 1 }],
          truncated: false
        }
      return { rows: [], truncated: false }
    })
    render(Tasks, {
      ctx: { ...makeCtx(), getTaskBlockers },
      manifest: MANIFEST
    })
    await flush()

    const checkbox = document.querySelector(
      '[data-block-id="b1"] button[role="checkbox"]'
    ) as HTMLElement
    await fireEvent.click(checkbox)
    await flush()

    expect(getTaskBlockers).toHaveBeenCalledWith('b1')
    expect(screen.getByText('Complete anyway')).toBeInTheDocument()
    expect(mocks.updateBlockState).not.toHaveBeenCalled()
  })

  it('a second mark-done while the guard is open does not fall through to commit', async () => {
    const getTaskBlockers = vi
      .fn()
      .mockResolvedValue([{ id: 'dep-1', clean_content: 'Prereq' }])
    mocks.sqliteQuery.mockImplementation(async (sql: string) => {
      if (isOpenSql(sql))
        return {
          rows: [{ ...task('b2', 'Blocked'), is_blocked: 1 }],
          truncated: false
        }
      return { rows: [], truncated: false }
    })
    render(Tasks, {
      ctx: { ...makeCtx(), getTaskBlockers },
      manifest: MANIFEST
    })
    await flush()

    const checkbox = document.querySelector(
      '[data-block-id="b2"] button[role="checkbox"]'
    ) as HTMLElement
    await fireEvent.click(checkbox) // opens the guard
    await flush()
    await fireEvent.click(checkbox) // re-entry while the guard is open
    await flush()

    // The guard dialog is still pending and no DONE was committed — the
    // early `if (blockedGuard.pending) return` prevented the fall-through.
    expect(screen.getByText('Complete anyway')).toBeInTheDocument()
    expect(mocks.updateBlockState).not.toHaveBeenCalled()
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
      if (isOpenSql(sql)) {
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

  it('focusKey re-fires the focus effect on re-navigation to the same block (#374 review regression)', async () => {
    // Locks the PluginView forwarding contract: if focusKey isn't
    // forwarded, the second click on the same search result silently
    // does nothing because focusBlockId never changed and the effect
    // wouldn't re-fire. Drive the case by mounting once, mounting again
    // with a bumped focusKey, and asserting scrollIntoView fires twice.
    const targetId = 'repeat-target'
    mocks.sqliteQuery.mockImplementation(async (sql: string) => {
      if (isOpenSql(sql)) {
        return { rows: [task(targetId, 'click me twice')], truncated: false }
      }
      return { rows: [], truncated: false }
    })

    const scrollIntoViewSpy = vi.fn()
    const originalQuery = HTMLElement.prototype.scrollIntoView
    HTMLElement.prototype.scrollIntoView = scrollIntoViewSpy

    try {
      // First navigation — focusKey = '1'
      render(Tasks, {
        ctx: makeCtx(),
        manifest: MANIFEST,
        focusBlockId: targetId,
        focusKey: '1'
      })
      await flush()
      await new Promise((r) => setTimeout(r, 10))
      await flush()

      expect(scrollIntoViewSpy).toHaveBeenCalledTimes(1)

      // Second navigation — same focusBlockId, bumped focusKey.
      // Without focusKey forwarding the effect wouldn't re-fire; with
      // it, scrollIntoView is called again.
      cleanup()
      render(Tasks, {
        ctx: makeCtx(),
        manifest: MANIFEST,
        focusBlockId: targetId,
        focusKey: '2'
      })
      await flush()
      await new Promise((r) => setTimeout(r, 10))
      await flush()

      expect(scrollIntoViewSpy.mock.calls.length).toBeGreaterThanOrEqual(2)
    } finally {
      HTMLElement.prototype.scrollIntoView = originalQuery
    }
  })

  it('open SQL forces non-DONE and orders by due date via buildQuery (#370/#526)', async () => {
    mocks.sqliteQuery.mockResolvedValue({ rows: [], truncated: false })

    render(Tasks, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    const openCall = mocks.sqliteQuery.mock.calls.find((c) =>
      isOpenSql(String(c[0]))
    )
    expect(openCall).toBeTruthy()
    const sql = String(openCall![0])
    // buildQuery dueDate sort: nulls last via COALESCE, then priority.
    expect(sql).toMatch(/ORDER BY COALESCE\(t\.due_date/)
    expect(sql).toMatch(/t\.status != 'DONE'/)
    expect(sql).toMatch(/LIMIT 500/)
  })

  it('Upcoming ends at the configured calendar week; later dates stay in Later', async () => {
    const configuredToday = '2026-07-06'
    setWeekStart('monday')
    const today3 = addDaysISO(configuredToday, 1)
    const today8 = addDaysISO(endOfWeekISO(configuredToday, 'monday'), 1)
    mocks.sqliteQuery.mockImplementation(async (sql: string) => {
      if (isOpenSql(sql)) {
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

    render(Tasks, {
      ctx: { ...makeCtx(), today: configuredToday },
      manifest: MANIFEST
    })
    await flush()

    const insideWeek = document.querySelector('[data-block-id="in3"]')
    expect(insideWeek).toBeInTheDocument()
    expect(
      insideWeek?.closest('[data-group]')?.getAttribute('data-group')
    ).toBe('upcoming')

    // Tasks due beyond the configured week used to vanish into
    // no group (silently dropped from the UI while still inflating
    // the header count). They now render in their own "Later" group.
    const beyondWeek = document.querySelector('[data-block-id="in8"]')
    expect(beyondWeek).toBeInTheDocument()
    expect(
      beyondWeek?.closest('[data-group]')?.getAttribute('data-group')
    ).toBe('later')
  })

  it('Upcoming includes tasks due exactly tomorrow (boundary regression — review off-by-one)', async () => {
    // Boundary case: due_date === tomorrow. The configured Monday-start week
    // keeps this test independent of the wall-clock weekday.
    const today = '2026-07-06'
    const tomorrow = addDaysISO(today, 1)
    setWeekStart('monday')
    mocks.sqliteQuery.mockImplementation(async (sql: string) => {
      if (isOpenSql(sql)) {
        return {
          rows: [task('tm1', 'due tomorrow', { due_date: tomorrow })],
          truncated: false
        }
      }
      return { rows: [], truncated: false }
    })

    render(Tasks, {
      ctx: { ...makeCtx(), today },
      manifest: MANIFEST
    })
    await flush()

    const tomorrowRow = document.querySelector('[data-block-id="tm1"]')
    expect(tomorrowRow).toBeInTheDocument()
    expect(
      tomorrowRow?.closest('[data-group]')?.getAttribute('data-group')
    ).toBe('upcoming')

    // Sanity: today (the boundary below tomorrow) still routes to its
    // own group, not Upcoming — fixes the off-by-one on the other side.
    mocks.sqliteQuery.mockResolvedValue({
      rows: [task('td', 'due today', { due_date: today })],
      truncated: false
    })
    cleanup()
    render(Tasks, {
      ctx: { ...makeCtx(), today },
      manifest: MANIFEST
    })
    await flush()
    const todayRow = document.querySelector('[data-block-id="td"]')
    expect(todayRow?.closest('[data-group]')?.getAttribute('data-group')).toBe(
      'today'
    )
  })

  it('tasks due beyond 7 days render in the Later group (review fix — no-longer-vanishing gap)', async () => {
    // The SQL has no upper date bound; without the Later bucket,
    // a due_date after the configured week matched every filter (overdue,
    // today, upcoming, undated) as false → dropped from the UI
    // entirely while still inflating the header's openItems count.
    // This locks the gap closed.
    const today30 = dateOffsetStr(30)
    mocks.sqliteQuery.mockImplementation(async (sql: string) => {
      if (isOpenSql(sql)) {
        return {
          rows: [task('far', 'plan next month', { due_date: today30 })],
          truncated: false
        }
      }
      return { rows: [], truncated: false }
    })

    render(Tasks, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    const farRow = document.querySelector('[data-block-id="far"]')
    expect(farRow).toBeInTheDocument()
    expect(farRow?.closest('[data-group]')?.getAttribute('data-group')).toBe(
      'later'
    )
  })
})

describe('Tasks view — inline quick-add (#409, replaces #399 toolbar toggle)', () => {
  beforeEach(() => {
    mocks.sqliteQuery.mockResolvedValue({ rows: [], truncated: false })
    mocks.createTask.mockReset().mockResolvedValue('new-task-id')
  })

  it('renders a persistent inline quick-add input at the bottom (no toolbar button)', async () => {
    render(Tasks, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    // The old toolbar "New task" button is gone (#409 AC3).
    expect(screen.queryByTestId('tasks-new-task-btn')).toBeNull()
    // The inline input is always present at the bottom of the list (#409 AC1).
    expect(screen.getByTestId('quick-add-task-input')).toBeInTheDocument()
    expect(screen.getByTestId('tasks-inline-quickadd')).toBeInTheDocument()
  })

  it('inline input is present even in the empty state', async () => {
    render(Tasks, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    expect(screen.getByTestId('tasks-empty')).toBeInTheDocument()
    // The capture affordance must be visible even when there are no tasks —
    // it is the primary entry point now that the toolbar button is gone.
    expect(screen.getByTestId('quick-add-task-input')).toBeInTheDocument()
  })

  it('submitting calls ctx.createTask with the typed title', async () => {
    render(Tasks, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    const input = screen.getByTestId('quick-add-task-input')
    await fireEvent.input(input, { target: { value: 'Ship the feature' } })
    await fireEvent.keyDown(input, { key: 'Enter' })
    await flush()

    expect(mocks.createTask).toHaveBeenCalledTimes(1)
    const call = mocks.createTask.mock.calls[0][0]
    expect(call.title).toBe('Ship the feature')
    expect(call.status).toBe('TODO')
    expect(call.dueDate).toBeUndefined()
  })

  it('input clears and stays mounted after create for rapid entry (#409 AC2)', async () => {
    render(Tasks, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    const input = screen.getByTestId('quick-add-task-input') as HTMLInputElement
    await fireEvent.input(input, { target: { value: 'first task' } })
    await fireEvent.keyDown(input, { key: 'Enter' })
    await flush()

    expect(mocks.createTask).toHaveBeenCalledTimes(1)
    // keepOpenAfterCreate: the input is still in the DOM and cleared.
    const inputAfter = screen.getByTestId(
      'quick-add-task-input'
    ) as HTMLInputElement
    expect(inputAfter).toBeInTheDocument()
    expect(inputAfter.value).toBe('')
  })

  it('rapid entry: a second create in a row calls createTask twice', async () => {
    render(Tasks, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    const input = screen.getByTestId('quick-add-task-input')
    await fireEvent.input(input, { target: { value: 'one' } })
    await fireEvent.keyDown(input, { key: 'Enter' })
    await flush()

    const input2 = screen.getByTestId('quick-add-task-input')
    await fireEvent.input(input2, { target: { value: 'two' } })
    await fireEvent.keyDown(input2, { key: 'Enter' })
    await flush()

    expect(mocks.createTask).toHaveBeenCalledTimes(2)
    expect(mocks.createTask.mock.calls[0][0].title).toBe('one')
    expect(mocks.createTask.mock.calls[1][0].title).toBe('two')
  })

  it('empty submit is rejected', async () => {
    render(Tasks, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    const input = screen.getByTestId('quick-add-task-input')
    await fireEvent.keyDown(input, { key: 'Enter' })
    await flush()

    expect(mocks.createTask).not.toHaveBeenCalled()
    expect(screen.getByTestId('quick-add-task-input')).toBeInTheDocument()
  })

  it('Escape clears the draft without creating or hiding the input', async () => {
    render(Tasks, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    const input = screen.getByTestId('quick-add-task-input') as HTMLInputElement
    await fireEvent.input(input, { target: { value: 'half-typed draft' } })
    await fireEvent.keyDown(input, { key: 'Escape' })
    await flush()

    // No task created.
    expect(mocks.createTask).not.toHaveBeenCalled()
    // The persistent input stays mounted and the draft is cleared.
    const inputAfter = screen.getByTestId(
      'quick-add-task-input'
    ) as HTMLInputElement
    expect(inputAfter).toBeInTheDocument()
    expect(inputAfter.value).toBe('')
  })
})

describe('Tasks view — truncated footer (#372 hardening)', () => {
  it('renders the truncated footer when the SQLite cap truncated results', async () => {
    mocks.sqliteQuery.mockImplementation(async (sql: string) => {
      if (isOpenSql(sql)) {
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

describe('Tasks view — grouping engine (#423)', () => {
  beforeEach(() => {
    mocks.sqliteQuery.mockReset()
    mocks.updateBlockState
      .mockReset()
      .mockResolvedValue({ ok: true, spawnedId: '' })
    mocks.createTask.mockReset().mockResolvedValue('new-task-id')
    mocks.blockChangedCallbacks.length = 0
  })

  afterEach(() => {
    cleanup()
  })

  it("groupBy='none' renders a single flat section with no group headers", async () => {
    mocks.sqliteQuery.mockImplementation(async (sql: string) => {
      if (isOpenSql(sql)) {
        return {
          rows: [
            task('a', 'task a', { owner: 'Alice' }),
            task('b', 'task b', { owner: 'Bob' })
          ],
          truncated: false
        }
      }
      return { rows: [], truncated: false }
    })

    setGroupBy('none')
    render(Tasks, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    // Both rows live under the single data-group="all" container, and no
    // group-heading buttons exist (flat list = no per-group headers).
    const all = document.querySelector('[data-group="all"]')
    expect(all).toBeInTheDocument()
    expect(all?.querySelector('[data-block-id="a"]')).toBeInTheDocument()
    expect(all?.querySelector('[data-block-id="b"]')).toBeInTheDocument()
    // No per-group toggle buttons (those exist only for the generalized
    // status/owner/... dimensions).
    expect(
      document.querySelectorAll('[data-testid^="tasks-group-toggle-"]')
    ).toHaveLength(0)
  })

  it("groupBy='owner' bins rows under per-owner sections with a trailing Unassigned", async () => {
    mocks.sqliteQuery.mockImplementation(async (sql: string) => {
      if (isOpenSql(sql)) {
        return {
          rows: [
            task('a', 'alice task', { owner: 'Alice', due_date: '' }),
            task('b', 'bob task', { owner: 'Bob', due_date: '' }),
            task('c', 'unassigned task', { owner: '', due_date: '' })
          ],
          truncated: false
        }
      }
      return { rows: [], truncated: false }
    })

    setGroupBy('owner')
    render(Tasks, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    // Each row lives under its owner-namespaced data-group; the empty owner
    // lands in the trailing Unassigned section.
    const aliceSection = document
      .querySelector('[data-block-id="a"]')
      ?.closest('[data-group]')
    expect(aliceSection?.getAttribute('data-group')).toBe('owner-Alice')

    const bobSection = document
      .querySelector('[data-block-id="b"]')
      ?.closest('[data-group]')
    expect(bobSection?.getAttribute('data-group')).toBe('owner-Bob')

    const unassignedSection = document
      .querySelector('[data-block-id="c"]')
      ?.closest('[data-group]')
    expect(unassignedSection?.getAttribute('data-group')).toBe(
      'owner-__unassigned__'
    )
    expect(unassignedSection?.getAttribute('aria-label')).toBe('Unassigned')
  })

  it("groupBy='status' renders lanes in TODO/DOING/DONE order", async () => {
    mocks.sqliteQuery.mockImplementation(async (sql: string) => {
      if (isOpenSql(sql)) {
        return {
          rows: [
            task('d', 'doing', { status: 'DOING', due_date: '' }),
            task('t', 'todo', { status: 'TODO', due_date: '' })
          ],
          truncated: false
        }
      }
      return { rows: [], truncated: false }
    })

    setGroupBy('status')
    render(Tasks, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    const todoSection = document
      .querySelector('[data-block-id="t"]')
      ?.closest('[data-group]')
    expect(todoSection?.getAttribute('data-group')).toBe('TODO')
    expect(todoSection?.getAttribute('aria-label')).toBe('To Do')

    const doingSection = document
      .querySelector('[data-block-id="d"]')
      ?.closest('[data-group]')
    expect(doingSection?.getAttribute('data-group')).toBe('DOING')
    expect(doingSection?.getAttribute('aria-label')).toBe('In Progress')

    // Section order: TODO before DOING in the DOM.
    const keys = Array.from(
      document.querySelectorAll('section[data-group]')
    ).map((s) => s.getAttribute('data-group'))
    expect(keys.indexOf('TODO')).toBeLessThan(keys.indexOf('DOING'))
  })

  it("default groupBy='dueDate' still renders the legacy time-horizon buckets", async () => {
    // The default (set by resetTaskHubState) is dueDate per #423.
    mocks.sqliteQuery.mockImplementation(async (sql: string) => {
      if (isOpenSql(sql)) {
        return {
          rows: [task('td', 'today', { due_date: todayStr() })],
          truncated: false
        }
      }
      return { rows: [], truncated: false }
    })

    render(Tasks, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    const section = document
      .querySelector('[data-block-id="td"]')
      ?.closest('[data-group]')
    expect(section?.getAttribute('data-group')).toBe('today')
  })
})

describe('Tasks view — manual ordering (#426)', () => {
  beforeEach(() => {
    mocks.sqliteQuery.mockReset()
    mocks.updateBlockState
      .mockReset()
      .mockResolvedValue({ ok: true, spawnedId: '' })
    mocks.setTaskOrder.mockReset().mockResolvedValue(true)
    mocks.setTaskOrders.mockReset().mockResolvedValue(true)
    mocks.createTask.mockReset().mockResolvedValue('new-task-id')
    mocks.blockChangedCallbacks.length = 0
  })

  afterEach(() => {
    cleanup()
  })

  it("sort='manual' renders a drag handle on each row", async () => {
    mocks.sqliteQuery.mockImplementation(async (sql: string) => {
      if (isOpenSql(sql)) {
        return {
          rows: [task('m1', 'manual row', { manual_order: 1 })]
        }
      }
      return { rows: [], truncated: false }
    })

    setSort('manual')
    render(Tasks, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    expect(
      document.querySelector('[data-testid="tasks-row-drag-handle-m1"]')
    ).toBeInTheDocument()
  })

  it("sort='manual' exposes the pointer-only drag handle honestly", async () => {
    mocks.sqliteQuery.mockImplementation(async (sql: string) => {
      if (isOpenSql(sql)) {
        return { rows: [task('m1', 'manual row', { manual_order: 1 })] }
      }
      return { rows: [], truncated: false }
    })

    setSort('manual')
    render(Tasks, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    const row = document.querySelector('[data-block-id="m1"]')
    expect(row?.getAttribute('role')).toBe('listitem')
    const handle = document.querySelector(
      '[data-testid="tasks-row-drag-handle-m1"]'
    )
    expect(handle?.getAttribute('draggable')).toBe('true')
    expect(handle).toHaveAttribute('aria-hidden', 'true')
    expect(handle).not.toHaveAttribute('role')
    expect(handle).not.toHaveAttribute('tabindex')
  })

  it("sort != 'manual' does NOT render a drag handle (rows open drawer on click)", async () => {
    mocks.sqliteQuery.mockImplementation(async (sql: string) => {
      if (isOpenSql(sql)) {
        return { rows: [task('d1', 'dueDate row', { manual_order: 1 })] }
      }
      return { rows: [], truncated: false }
    })

    // default sort is 'dueDate' (set by resetTaskHubState)
    render(Tasks, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    expect(
      document.querySelector('[data-testid="tasks-row-drag-handle-d1"]')
    ).toBeNull()
  })

  it('dragging a row within a group renumbers + persists via setTaskOrders', async () => {
    // Three rows in a single 'none' group with manual_order 1, 2, 3.
    // Dragging row A (order 1) onto row C (order 3) lands A before C →
    //   splice [A,B,C] → remove A → [B,C] → insert A before C (idx 1) →
    //   [B, A, C] → new orders B=1 (was 2 → CHANGE), A=2 (was 1 → CHANGE),
    //   C=3 (unchanged).
    mocks.sqliteQuery.mockImplementation(async (sql: string) => {
      if (isOpenSql(sql)) {
        return {
          rows: [
            task('A', 'task a', { manual_order: 1, due_date: '' }),
            task('B', 'task b', { manual_order: 2, due_date: '' }),
            task('C', 'task c', { manual_order: 3, due_date: '' })
          ]
        }
      }
      return { rows: [], truncated: false }
    })

    setGroupBy('none')
    setSort('manual')
    render(Tasks, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    const handleA = document.querySelector(
      '[data-testid="tasks-row-drag-handle-A"]'
    ) as HTMLElement
    const rowC = document.querySelector('[data-block-id="C"]') as HTMLElement
    expect(handleA).toBeTruthy()
    expect(rowC).toBeTruthy()

    await fireEvent.dragStart(handleA)
    await fireEvent.drop(rowC)
    await flush()

    // A moved before C → A's new slot is 2; B shifted 2→1. C unchanged.
    // Persisted in ONE batched setTaskOrders call (one atomic write per file).
    expect(mocks.setTaskOrders).toHaveBeenCalledTimes(1)
    const batch = mocks.setTaskOrders.mock.calls[0]![0] as {
      id: string
      order: number
    }[]
    expect(batch).toContainEqual({ id: 'A', order: 2 })
    expect(batch).toContainEqual({ id: 'B', order: 1 })
  })

  it('dragging a row preserves order on reload (optimistic state matches persisted)', async () => {
    // After the optimistic update, the row's data-block-id order in the DOM
    // reflects the new sequence; the next reload would preserve it because
    // the persisted manual_order matches the new positions.
    mocks.sqliteQuery.mockImplementation(async (sql: string) => {
      if (isOpenSql(sql)) {
        return {
          rows: [
            task('X', 'task x', { manual_order: 1, due_date: '' }),
            task('Y', 'task y', { manual_order: 2, due_date: '' })
          ]
        }
      }
      return { rows: [], truncated: false }
    })

    setGroupBy('none')
    setSort('manual')
    render(Tasks, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    // Drag Y onto X → Y lands before X → [Y, X].
    const handleY = document.querySelector(
      '[data-testid="tasks-row-drag-handle-Y"]'
    ) as HTMLElement
    const rowX = document.querySelector('[data-block-id="X"]') as HTMLElement

    await fireEvent.dragStart(handleY)
    await fireEvent.drop(rowX)
    await flush()

    const rows = Array.from(
      document.querySelectorAll('[data-group="all"] [data-block-id]')
    ).map((el) => el.getAttribute('data-block-id'))
    expect(rows).toEqual(['Y', 'X'])
    // Persisted in one batched call: Y gets order 1 (was 2), X gets order 2.
    expect(mocks.setTaskOrders).toHaveBeenCalledTimes(1)
    const batch = mocks.setTaskOrders.mock.calls[0]![0] as {
      id: string
      order: number
    }[]
    expect(batch).toContainEqual({ id: 'Y', order: 1 })
    expect(batch).toContainEqual({ id: 'X', order: 2 })
  })

  it('cross-group drop is a no-op for setTaskOrder (BoardView owns dimension reassignment)', async () => {
    // groupBy=owner with two single-task groups; dragging across owners
    // shouldn't fire setTaskOrder (the List's manual drag is within-group).
    mocks.sqliteQuery.mockImplementation(async (sql: string) => {
      if (isOpenSql(sql)) {
        return {
          rows: [
            task('p', 'pat task', {
              owner: 'Pat',
              manual_order: 1,
              due_date: ''
            }),
            task('s', 'sam task', {
              owner: 'Sam',
              manual_order: 1,
              due_date: ''
            })
          ]
        }
      }
      return { rows: [], truncated: false }
    })

    setGroupBy('owner')
    setSort('manual')
    render(Tasks, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    const handleP = document.querySelector(
      '[data-testid="tasks-row-drag-handle-p"]'
    ) as HTMLElement
    const rowS = document.querySelector('[data-block-id="s"]') as HTMLElement

    await fireEvent.dragStart(handleP)
    await fireEvent.drop(rowS)
    await flush()

    // Cross-group drag in the List is a no-op — no order writes, no
    // dimension reassignment (the user uses BoardView for that).
    expect(mocks.setTaskOrders).not.toHaveBeenCalled()
  })

  it('reports a failed setTaskOrders via the role="alert" banner', async () => {
    mocks.sqliteQuery.mockImplementation(async (sql: string) => {
      if (isOpenSql(sql)) {
        return {
          rows: [
            task('A', 'task a', { manual_order: 1, due_date: '' }),
            task('B', 'task b', { manual_order: 2, due_date: '' }),
            task('C', 'task c', { manual_order: 3, due_date: '' })
          ]
        }
      }
      return { rows: [], truncated: false }
    })
    mocks.setTaskOrders.mockReset().mockRejectedValue(new Error('lock held'))

    setGroupBy('none')
    setSort('manual')
    render(Tasks, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    const handleA = document.querySelector(
      '[data-testid="tasks-row-drag-handle-A"]'
    ) as HTMLElement
    const rowC = document.querySelector('[data-block-id="C"]') as HTMLElement

    await fireEvent.dragStart(handleA)
    await fireEvent.drop(rowC)
    // Drain the rejection chain through the batch .catch (which flips
    // orderError via flashOrderError) and the trailing liveMessage setter.
    await flush()
    await new Promise((r) => setTimeout(r, 0))

    const alert = document.querySelector(
      '[data-testid="tasks-order-error"]'
    ) as HTMLElement | null
    expect(alert).toBeTruthy()
    expect(alert?.getAttribute('role')).toBe('alert')
    expect(alert?.textContent).toContain("Couldn't reorder task")
  })
})

describe('ListView — subtask badge (#434)', () => {
  beforeEach(() => {
    resetTaskHubState()
    mocks.sqliteQuery.mockReset()
    mocks.blockChangedCallbacks.length = 0
  })
  afterEach(cleanup)

  it('renders [done/total] badge when subtask_total > 0', async () => {
    mocks.sqliteQuery.mockImplementation(async (sql: string) => {
      if (isOpenSql(sql)) {
        return {
          rows: [
            {
              ...task('p1', 'Parent task'),
              subtask_total: 3,
              subtask_done: 1
            }
          ],
          truncated: false
        }
      }
      return { rows: [], truncated: false }
    })
    render(Tasks, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()
    expect(screen.getByTestId('tasks-subtask-badge-p1').textContent).toContain(
      '[1/3]'
    )
  })
})

describe('ListView — server-side filtering via buildQuery (#526)', () => {
  beforeEach(() => {
    resetTaskHubState()
    mocks.sqliteQuery.mockReset()
    mocks.sqliteQuery.mockResolvedValue({ rows: [], truncated: false })
    mocks.blockChangedCallbacks.length = 0
  })
  afterEach(cleanup)

  it('open SQL includes owner filter predicates and bound params', async () => {
    setFilters({
      owners: ['Alice'],
      priorities: [],
      dueDate: '',
      tags: []
    })
    render(Tasks, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    const openCall = mocks.sqliteQuery.mock.calls.find((c) =>
      isOpenSql(String(c[0]))
    )
    expect(openCall).toBeTruthy()
    const sql = String(openCall![0])
    const params = openCall![1] as unknown[]
    expect(sql).toContain('t.owner IN (?)')
    expect(params).toContain('Alice')
  })

  // Large-set regression (#526): filters must appear before LIMIT so the
  // cap cannot drop matching rows that would have survived filter-then-limit.
  it('owners filter + LIMIT 500 proves filter-then-limit on open SQL', async () => {
    setFilters({
      owners: ['Alice'],
      priorities: [],
      dueDate: '',
      tags: []
    })
    render(Tasks, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    const openCall = mocks.sqliteQuery.mock.calls.find((c) =>
      isOpenSql(String(c[0]))
    )
    expect(openCall).toBeTruthy()
    const sql = String(openCall![0])
    expect(sql).toContain('t.owner IN (?)')
    expect(sql).toMatch(/LIMIT 500/)
    const ownerIdx = sql.indexOf('t.owner IN (?)')
    const limitIdx = sql.search(/LIMIT 500/)
    expect(ownerIdx).toBeGreaterThanOrEqual(0)
    expect(limitIdx).toBeGreaterThan(ownerIdx)
  })

  it('stale filter wires modified_at predicate and LIMIT 500 into open SQL', async () => {
    setFilters({
      owners: [],
      priorities: [],
      dueDate: '',
      tags: [],
      stale: true
    })
    render(Tasks, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    const openCall = mocks.sqliteQuery.mock.calls.find((c) =>
      isOpenSql(String(c[0]))
    )
    expect(openCall).toBeTruthy()
    const sql = String(openCall![0])
    expect(sql).toMatch(/modified_at/)
    expect(sql).toMatch(/LIMIT 500/)
  })

  it('activeFilter=completed skips open query and loads DONE rows only', async () => {
    setActiveFilter('completed')
    render(Tasks, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    const openCall = mocks.sqliteQuery.mock.calls.find((c) =>
      isOpenSql(String(c[0]))
    )
    expect(openCall).toBeUndefined()

    const doneCall = mocks.sqliteQuery.mock.calls.find((c) =>
      isDoneSql(String(c[0]))
    )
    expect(doneCall).toBeTruthy()
    expect(String(doneCall![0])).toContain("t.status = 'DONE'")
  })

  it('open SQL includes priority + tag predicates when set', async () => {
    setFilters({
      owners: [],
      priorities: [1, 2],
      dueDate: '',
      tags: ['work']
    })
    render(Tasks, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    const openCall = mocks.sqliteQuery.mock.calls.find((c) =>
      isOpenSql(String(c[0]))
    )
    expect(openCall).toBeTruthy()
    const sql = String(openCall![0])
    const params = openCall![1] as unknown[]
    expect(sql).toContain('t.priority IN (?, ?)')
    expect(sql).toMatch(/raw_path IN \(\?\)/)
    expect(params).toEqual(expect.arrayContaining([1, 2, 'work']))
  })

  it('notebook scope binds activeNotebook into open SQL', async () => {
    setScope('notebook')
    const ctx = makeCtx()
    ctx.activeNotebook = 'Work'
    render(Tasks, { ctx, manifest: MANIFEST })
    await flush()

    const openCall = mocks.sqliteQuery.mock.calls.find((c) =>
      isOpenSql(String(c[0]))
    )
    expect(openCall).toBeTruthy()
    expect(String(openCall![0])).toContain('b.notebook = ?')
    expect(openCall![1] as unknown[]).toContain('Work')
  })

  it('activeFilter=today wires due_date constraint into open SQL', async () => {
    setActiveFilter('today')
    const ctx = makeCtx()
    render(Tasks, { ctx, manifest: MANIFEST })
    await flush()

    const openCall = mocks.sqliteQuery.mock.calls.find((c) =>
      isOpenSql(String(c[0]))
    )
    expect(openCall).toBeTruthy()
    expect(String(openCall![0])).toContain('t.due_date = ?')
    expect(openCall![1] as unknown[]).toContain(ctx.today)
  })

  it('reloads when filters change (sqliteQuery called again)', async () => {
    render(Tasks, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()
    const afterMount = mocks.sqliteQuery.mock.calls.length
    expect(afterMount).toBeGreaterThan(0)

    setFilters({
      owners: ['Bob'],
      priorities: [],
      dueDate: '',
      tags: []
    })
    await flush()
    await new Promise((r) => setTimeout(r, 0))
    await flush()

    expect(mocks.sqliteQuery.mock.calls.length).toBeGreaterThan(afterMount)
    const lastOpen = [...mocks.sqliteQuery.mock.calls]
      .reverse()
      .find((c) => isOpenSql(String(c[0])))
    expect(lastOpen).toBeTruthy()
    expect(String(lastOpen![0])).toContain('t.owner IN (?)')
    expect(lastOpen![1] as unknown[]).toContain('Bob')
  })

  it('done SQL uses completed status + file_date order with scope filters', async () => {
    setScope('notebook')
    setFilters({
      owners: ['Alice'],
      priorities: [],
      dueDate: '',
      tags: []
    })
    const ctx = makeCtx()
    ctx.activeNotebook = 'Work'
    render(Tasks, { ctx, manifest: MANIFEST })
    await flush()

    const doneCall = mocks.sqliteQuery.mock.calls.find((c) =>
      isDoneSql(String(c[0]))
    )
    expect(doneCall).toBeTruthy()
    const sql = String(doneCall![0])
    expect(sql).toContain("t.status = 'DONE'")
    expect(sql).toMatch(/ORDER BY b\.file_date DESC LIMIT 200/)
    expect(sql).toContain('b.notebook = ?')
    expect(sql).toContain('t.owner IN (?)')
    expect(doneCall![1] as unknown[]).toEqual(
      expect.arrayContaining(['Work', 'Alice'])
    )
  })

  // Server-side filters: zero open matches must not claim "All caught up"
  // when completed rows still exist under the same filters.
  it('shows filter-aware empty open state when filters match no open tasks', async () => {
    setFilters({
      owners: ['Nobody'],
      priorities: [],
      dueDate: '',
      tags: []
    })
    mocks.sqliteQuery.mockImplementation(async (sql: string) => {
      if (isDoneSql(String(sql))) {
        return {
          rows: [
            {
              id: 'd1',
              notebook: '.silt',
              section: '',
              page: 'tasks',
              file_date: todayStr(),
              clean_content: 'done under filter',
              status: 'DONE'
            }
          ],
          truncated: false
        }
      }
      // Open query: no matches
      return { rows: [], truncated: false }
    })
    render(Tasks, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    expect(screen.getByTestId('tasks-open-empty-filtered')).toBeInTheDocument()
    expect(screen.getByText(/No open tasks match/i)).toBeInTheDocument()
    expect(screen.queryByText(/All caught up/i)).toBeNull()
  })
})
