import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest'
import { tick } from 'svelte'
import { render, screen, cleanup, fireEvent } from '@testing-library/svelte'

// Mock seam mirrors TasksHub.test.ts: the sidebar persists via the settings
// store and reads the synchronous config snapshot, plus the wails runtime.
const mocks = vi.hoisted(() => ({
  sqliteQuery: vi.fn(),
  updatePluginSetting: vi.fn().mockResolvedValue(true),
  settings: {
    config: {
      plugins: {
        active: [],
        disabled: [],
        plugin_settings: {} as Record<string, Record<string, unknown>>
      }
    }
  },
  blockChangedCallbacks: [] as Array<() => void>
}))

vi.mock('../../../settings/store.svelte', () => ({
  settings: mocks.settings,
  updatePluginSetting: mocks.updatePluginSetting
}))

vi.mock('../../../../wailsjs/runtime/runtime.js', () => ({
  EventsOn: vi.fn(() => () => {})
}))

import Sidebar from './Sidebar.svelte'
import type {
  PluginContext,
  PluginManifest,
  PluginEventName,
  PluginEventPayload
} from '../../sdk'
import { v2CtxStubs } from '../../test-helpers'
import {
  getTaskHubState,
  resetTaskHubState,
  setFilters,
  applySavedView,
  type SavedView
} from './state.svelte'
import { SYSTEM_VIEWS } from './savedViews'

// jsdom polyfills: the sidebar itself doesn't need them, but keeping the
// pattern consistent with TasksHub.test.ts avoids breakage when the test
// suite adds wider coverage later.
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

function makeCtx(overrides: Partial<PluginContext> = {}): PluginContext {
  return {
    ...v2CtxStubs,
    activeNotebook: '',
    activeSection: '',
    activePage: '',
    today: '2026-07-06',
    sqliteQuery: mocks.sqliteQuery,
    mutateBlock: vi.fn(),
    updateBlockState: vi.fn(),
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
    ...overrides
  }
}

const MANIFEST: PluginManifest = {
  id: 'silt-tasks',
  name: 'Tasks',
  version: '1.0.0'
}

function mockCounts(
  today: number,
  upcoming: number,
  overdue: number,
  completed: number,
  all: number
) {
  return {
    rows: [{ today, upcoming, overdue, completed, all }],
    truncated: false
  }
}

function mockDayCounts(entries: Array<{ d: string; c: number }>) {
  return {
    rows: entries.map((e) => ({ d: e.d, c: e.c })),
    truncated: false
  }
}

function mockOwnersOwners(list: string[]) {
  return { rows: list.map((o) => ({ owner: o })), truncated: false }
}

function mockTagRoots(list: string[]) {
  return { rows: list.map((t) => ({ level_0: t })), truncated: false }
}

/** Seed the unified hub state with system views + an optional user view. */
function seedSavedViews(user: SavedView[] = []): void {
  getTaskHubState().savedViews = [...SYSTEM_VIEWS, ...user]
}

async function flush() {
  await tick()
  await new Promise((r) => setTimeout(r, 0))
}

describe('silt-tasks Sidebar (#432)', () => {
  beforeEach(() => {
    mocks.sqliteQuery.mockReset()
    mocks.updatePluginSetting.mockReset().mockResolvedValue(true)
    mocks.settings.config.plugins.plugin_settings = {}
    mocks.blockChangedCallbacks.length = 0
    resetTaskHubState()
    // Default sqlite behavior: empty counts + empty facets. Individual
    // tests override via mockImplementation when they need data.
    mocks.sqliteQuery.mockImplementation(async (sql: string) => {
      if (sql.includes('SUM(CASE')) return mockCounts(0, 0, 0, 0, 0)
      if (sql.includes('level_0')) return mockTagRoots([])
      if (sql.includes('DISTINCT owner')) return mockOwnersOwners([])
      return mockDayCounts([])
    })
  })

  afterEach(() => {
    cleanup()
  })

  // --- Section 1: Smart Lists --------------------------------------------

  it('renders all five smart-list rows', async () => {
    mocks.sqliteQuery.mockImplementation(async (sql: string) => {
      if (sql.includes('SUM(CASE')) return mockCounts(3, 12, 1, 0, 49)
      if (sql.includes('level_0')) return mockTagRoots([])
      if (sql.includes('DISTINCT owner')) return mockOwnersOwners([])
      return mockDayCounts([])
    })
    render(Sidebar, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()
    expect(screen.getByTestId('today')).toBeInTheDocument()
    expect(screen.getByTestId('upcoming')).toBeInTheDocument()
    expect(screen.getByTestId('overdue')).toBeInTheDocument()
    expect(screen.getByTestId('completed')).toBeInTheDocument()
    expect(screen.getByTestId('all')).toBeInTheDocument()
  })

  it('renders the count badges from the SQLite aggregate query', async () => {
    mocks.sqliteQuery.mockImplementation(async (sql: string) => {
      if (sql.includes('SUM(CASE')) return mockCounts(3, 12, 1, 0, 49)
      if (sql.includes('level_0')) return mockTagRoots([])
      if (sql.includes('DISTINCT owner')) return mockOwnersOwners([])
      return mockDayCounts([])
    })
    render(Sidebar, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()
    expect(screen.getByTestId('count-today').textContent?.trim()).toBe('3')
    expect(screen.getByTestId('count-upcoming').textContent?.trim()).toBe('12')
    expect(screen.getByTestId('count-overdue').textContent?.trim()).toBe('1')
    expect(screen.getByTestId('count-all').textContent?.trim()).toBe('49')
  })

  it('quotes the `all` aggregate alias — ALL is a SQLite keyword', async () => {
    // Regression guard: `AS all` (bare) is a syntax error because ALL is a
    // reserved word. The IPC-boundary mock never executes the SQL, so we
    // assert the quoted alias here so it can't silently regress.
    render(Sidebar, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()
    const countSql = mocks.sqliteQuery.mock.calls
      .map((c) => String(c[0]))
      .find((s) => s.includes('SUM(CASE'))
    expect(countSql).toBeDefined()
    expect(countSql).toContain('AS "all"')
    expect(countSql).not.toMatch(/AS all\b/)
  })

  it('shows empty-state hint when no active + no completed tasks', async () => {
    render(Sidebar, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()
    expect(screen.getByTestId('calendar-empty-state')).toBeInTheDocument()
    expect(screen.queryByTestId('today')).toBeNull()
    expect(screen.queryByTestId('all')).toBeNull()
  })

  it('keeps Completed smart list visible for completed-only vault', async () => {
    mocks.sqliteQuery.mockImplementation(async (sql: string) => {
      if (sql.includes('SUM(CASE')) return mockCounts(0, 0, 0, 5, 0)
      if (sql.includes('level_0')) return mockTagRoots([])
      if (sql.includes('DISTINCT owner')) return mockOwnersOwners([])
      return mockDayCounts([])
    })
    render(Sidebar, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()
    expect(screen.queryByTestId('calendar-empty-state')).toBeNull()
    expect(screen.getByTestId('completed')).toBeInTheDocument()
  })

  it('clicking Today sets getTaskHubState().activeFilter === "today"', async () => {
    mocks.sqliteQuery.mockImplementation(async (sql: string) => {
      if (sql.includes('SUM(CASE')) return mockCounts(3, 12, 1, 0, 49)
      if (sql.includes('level_0')) return mockTagRoots([])
      if (sql.includes('DISTINCT owner')) return mockOwnersOwners([])
      return mockDayCounts([])
    })
    render(Sidebar, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()
    await fireEvent.click(screen.getByTestId('today'))
    expect(getTaskHubState().activeFilter).toBe('today')
  })

  it('clicking All Tasks sets activeFilter === "all"', async () => {
    mocks.sqliteQuery.mockImplementation(async (sql: string) => {
      if (sql.includes('SUM(CASE')) return mockCounts(3, 12, 1, 0, 49)
      if (sql.includes('level_0')) return mockTagRoots([])
      if (sql.includes('DISTINCT owner')) return mockOwnersOwners([])
      return mockDayCounts([])
    })
    render(Sidebar, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()
    await fireEvent.click(screen.getByTestId('today'))
    expect(getTaskHubState().activeFilter).toBe('today')
    await fireEvent.click(screen.getByTestId('all'))
    expect(getTaskHubState().activeFilter).toBe('all')
  })

  it('smart-list keyboard nav: ArrowDown moves focus to next option', async () => {
    mocks.sqliteQuery.mockImplementation(async (sql: string) => {
      if (sql.includes('SUM(CASE')) return mockCounts(3, 12, 1, 0, 16)
      if (sql.includes('level_0')) return mockTagRoots([])
      if (sql.includes('DISTINCT owner')) return mockOwnersOwners([])
      return mockDayCounts([])
    })
    render(Sidebar, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()
    const first = screen.getByTestId('today')
    first.focus()
    await fireEvent.keyDown(first, { key: 'ArrowDown' })
    await flush()
    expect(document.activeElement).toBe(screen.getByTestId('upcoming'))
  })

  it('aria-live region announces count changes', async () => {
    mocks.sqliteQuery.mockImplementation(async (sql: string) => {
      if (sql.includes('SUM(CASE')) return mockCounts(3, 12, 1, 0, 16)
      if (sql.includes('level_0')) return mockTagRoots([])
      if (sql.includes('DISTINCT owner')) return mockOwnersOwners([])
      return mockDayCounts([])
    })
    render(Sidebar, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()
    const live = document.querySelector('[aria-live="polite"]')
    expect(live).toBeTruthy()
    expect(live?.textContent ?? '').toContain('3 today')
    expect(live?.textContent ?? '').toContain('16 total')
  })

  // --- Section 2: Saved Views --------------------------------------------

  it('renders the saved-views section + system views on first paint even with empty user settings', async () => {
    seedSavedViews()
    render(Sidebar, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()
    expect(screen.getByTestId('view-sys-today-board')).toBeInTheDocument()
    expect(screen.getByTestId('view-sys-by-owner')).toBeInTheDocument()
    expect(screen.getByTestId('view-sys-week-calendar')).toBeInTheDocument()
  })

  it('clicking a saved view calls applySavedView and the button gets aria-pressed="true" when its fingerprint matches state', async () => {
    // The fingerprint compares ALL hub dimensions, so the view must
    // snapshot calendarSubMode + columns too — system views omit those
    // and their applied state diverges from the fingerprint by design.
    const fullView: SavedView = {
      id: 'u-match',
      name: 'Match Me',
      displayMode: 'list',
      groupBy: 'owner',
      sort: 'priority',
      scope: 'vault',
      filters: { owners: [], priorities: [], dueDate: '', tags: [] },
      calendarSubMode: 'month',
      columns: ['TODO', 'DOING', 'DONE']
    }
    seedSavedViews([fullView])
    render(Sidebar, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()
    const activateBtn = document.querySelector<HTMLElement>(
      `[data-testid="view-${fullView.id}"] button`
    )
    expect(activateBtn).toBeTruthy()
    await fireEvent.click(activateBtn!)
    await flush()
    expect(getTaskHubState().activeSavedViewId).toBe(fullView.id)
    expect(activateBtn!.getAttribute('aria-pressed')).toBe('true')
  })

  it('clicking a SYSTEM saved view highlights it as active via viewMatchesState (#432)', async () => {
    // System views are partial templates — they omit calendarSubMode/columns.
    // viewMatchesState compares only the dims the view defines, so applying
    // sys-by-owner to default state highlights it (the previous fingerprint
    // check would fail because state has calendarSubMode=month but the view
    // doesn't define it).
    render(Sidebar, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()
    // Seed the system view as the active one (TasksHub does this on mount in
    // production via applySavedView; here we set it directly to isolate the
    // sidebar highlight check from the activation path).
    const state = getTaskHubState()
    state.savedViews = [...SYSTEM_VIEWS]
    applySavedView(SYSTEM_VIEWS[1]) // sys-by-owner: list/owner/priority
    await flush()
    const activateBtn = document.querySelector<HTMLElement>(
      '[data-testid="view-sys-by-owner"] button'
    )
    expect(activateBtn).toBeTruthy()
    expect(activateBtn!.getAttribute('aria-pressed')).toBe('true')
  })

  it('a system view whose defined dims do NOT match state is not highlighted', async () => {
    render(Sidebar, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()
    const state = getTaskHubState()
    state.savedViews = [...SYSTEM_VIEWS]
    // State is default: list/dueDate/dueDate — sys-today-board wants board/status/today.
    // The view's displayMode + groupBy don't match → not active.
    await flush()
    const activateBtn = document.querySelector<HTMLElement>(
      '[data-testid="view-sys-today-board"] button'
    )
    expect(activateBtn).toBeTruthy()
    expect(activateBtn!.getAttribute('aria-pressed')).toBe('false')
  })

  it('delete button on a USER view calls persistSavedViews with the view removed', async () => {
    const userView: SavedView = {
      id: 'u1',
      name: 'My View',
      displayMode: 'list',
      groupBy: 'owner',
      scope: 'vault',
      filters: { owners: [], priorities: [], dueDate: '', tags: [] }
    }
    seedSavedViews([userView])
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    render(Sidebar, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()
    await fireEvent.click(screen.getByTestId('delete-view-u1'))
    await flush()
    expect(mocks.updatePluginSetting).toHaveBeenCalledWith(
      'silt-tasks',
      'saved_views',
      expect.not.arrayContaining([expect.objectContaining({ id: 'u1' })])
    )
    expect(screen.queryByTestId('view-u1')).toBeNull()
  })

  it('delete button on a SYSTEM view is disabled (system views are read-only)', async () => {
    seedSavedViews()
    render(Sidebar, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()
    const del = screen.getByTestId(
      'delete-view-sys-today-board'
    ) as HTMLButtonElement
    expect(del.disabled).toBe(true)
    expect(del.getAttribute('aria-disabled')).toBe('true')
  })

  it('active view shows "(modified)" suffix when savedViewsDirty is true', async () => {
    seedSavedViews()
    render(Sidebar, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()
    const view = SYSTEM_VIEWS[1]
    // Pin the view active + dirty directly via state — the sidebar reads
    // hubState.activeSavedViewId + hubState.savedViewsDirty reactively.
    getTaskHubState().activeSavedViewId = view.id
    getTaskHubState().savedViewsDirty = true
    await flush()
    const viewChip = document.querySelector(`[data-testid="view-${view.id}"]`)
    expect(viewChip?.textContent ?? '').toMatch(/modified/i)
  })

  // --- Section 3: Mini-cal -----------------------------------------------

  it('mini calendar shows dot indicators on days with tasks', async () => {
    mocks.sqliteQuery.mockImplementation(async (sql: string) => {
      if (sql.includes('SUM(CASE')) return mockCounts(0, 0, 0, 0, 0)
      if (sql.includes('level_0')) return mockTagRoots([])
      if (sql.includes('DISTINCT owner')) return mockOwnersOwners([])
      return mockDayCounts([
        { d: '2026-07-06', c: 2 },
        { d: '2026-07-10', c: 1 }
      ])
    })
    render(Sidebar, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()
    const dayWithDots = document.querySelector(
      '[data-testid="mini-day-2026-07-06"] [aria-hidden="true"]'
    )
    expect(dayWithDots).toBeTruthy()
    const dayWithoutDots = document.querySelector(
      '[data-testid="mini-day-2026-07-07"] [aria-hidden="true"]'
    )
    expect(dayWithoutDots).toBeNull()
  })

  it('clicking a day sets getTaskHubState().focusDate and dispatches calendar:focus-date', async () => {
    render(Sidebar, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()
    const handler = vi.fn()
    window.addEventListener('calendar:focus-date', handler)
    const cell = document.querySelector<HTMLElement>(
      '[data-testid="mini-day-2026-07-06"]'
    )
    expect(cell).toBeTruthy()
    await fireEvent.click(cell!)
    expect(getTaskHubState().focusDate).toBe('2026-07-06')
    expect(handler).toHaveBeenCalled()
    const detail = (handler.mock.calls[0][0] as CustomEvent).detail
    expect(detail.date).toBe('2026-07-06')
    window.removeEventListener('calendar:focus-date', handler)
  })

  it('Today button clears focusDate', async () => {
    render(Sidebar, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()
    const cell = document.querySelector<HTMLElement>(
      '[data-testid="mini-day-2026-07-06"]'
    )
    await fireEvent.click(cell!)
    expect(getTaskHubState().focusDate).toBe('2026-07-06')
    await fireEvent.click(screen.getByTestId('mini-today'))
    expect(getTaskHubState().focusDate).toBe('')
  })

  it('clear-focus button shows when focusDate set, hides when cleared', async () => {
    render(Sidebar, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()
    expect(screen.queryByTestId('clear-focus')).toBeNull()
    const cell = document.querySelector<HTMLElement>(
      '[data-testid="mini-day-2026-07-06"]'
    )
    await fireEvent.click(cell!)
    expect(screen.queryByTestId('clear-focus')).toBeTruthy()
    await fireEvent.click(screen.getByTestId('clear-focus'))
    expect(getTaskHubState().focusDate).toBe('')
    expect(screen.queryByTestId('clear-focus')).toBeNull()
  })

  // --- Section 4: Active Filters -----------------------------------------

  it('toggle priority checkbox updates state filters + reflects in checkbox checked', async () => {
    render(Sidebar, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()
    await fireEvent.click(screen.getByTestId('priority-1'))
    expect(getTaskHubState().filters.priorities).toContain(1)
    const cb = screen.getByTestId('priority-1') as HTMLInputElement
    expect(cb.checked).toBe(true)
    await fireEvent.click(screen.getByTestId('priority-1'))
    expect(getTaskHubState().filters.priorities).not.toContain(1)
    expect(cb.checked).toBe(false)
  })

  it('toggle due-date quick-pick sets filters.dueDate', async () => {
    render(Sidebar, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()
    await fireEvent.click(screen.getByTestId('due-overdue'))
    expect(getTaskHubState().filters.dueDate).toBe('overdue')
    await fireEvent.click(screen.getByTestId('due-all'))
    expect(getTaskHubState().filters.dueDate).toBe('')
  })

  it('toggling a filter from outside the sidebar is reflected in the sidebar checkboxes (bidirectional sync)', async () => {
    render(Sidebar, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()
    setFilters({
      owners: [],
      priorities: [2],
      dueDate: 'today',
      tags: []
    })
    await flush()
    const checked = screen.getByTestId('priority-2') as HTMLInputElement
    expect(checked.checked).toBe(true)
    const dueToday = screen.getByTestId('due-today')
    expect(dueToday.getAttribute('aria-checked')).toBe('true')
  })

  it('Clear all filters clears state', async () => {
    render(Sidebar, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()
    await fireEvent.click(screen.getByTestId('priority-3'))
    await flush()
    expect(getTaskHubState().filters.priorities).toContain(3)
    await fireEvent.click(screen.getByTestId('clear-filters'))
    expect(getTaskHubState().filters.priorities).toEqual([])
    expect(getTaskHubState().filters.dueDate).toBe('')
  })

  it('owner checkboxes appear only when owner facet query returns owners', async () => {
    mocks.sqliteQuery.mockImplementation(async (sql: string) => {
      if (sql.includes('SUM(CASE')) return mockCounts(0, 0, 0, 0, 0)
      if (sql.includes('level_0')) return mockTagRoots([])
      if (sql.includes('DISTINCT owner'))
        return mockOwnersOwners(['alice', 'bob'])
      return mockDayCounts([])
    })
    render(Sidebar, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()
    expect(screen.getByTestId('owner-alice')).toBeInTheDocument()
    expect(screen.getByTestId('owner-bob')).toBeInTheDocument()
  })

  it('tag checkboxes appear only when tag facet query returns tags', async () => {
    mocks.sqliteQuery.mockImplementation(async (sql: string) => {
      if (sql.includes('SUM(CASE')) return mockCounts(0, 0, 0, 0, 0)
      if (sql.includes('level_0')) return mockTagRoots(['work', 'home'])
      if (sql.includes('DISTINCT owner')) return mockOwnersOwners([])
      return mockDayCounts([])
    })
    render(Sidebar, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()
    expect(screen.getByTestId('tag-work')).toBeInTheDocument()
    expect(screen.getByTestId('tag-home')).toBeInTheDocument()
  })

  // --- Misc: refresh + error paths ---------------------------------------

  it('registers a block:changed listener that triggers a reload', async () => {
    render(Sidebar, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()
    expect(mocks.blockChangedCallbacks.length).toBeGreaterThan(0)
    const before = mocks.sqliteQuery.mock.calls.length
    // The sidebar debounces block:changed by 200ms; flush the timer.
    for (const cb of mocks.blockChangedCallbacks) cb()
    await new Promise((r) => setTimeout(r, 250))
    expect(mocks.sqliteQuery.mock.calls.length).toBeGreaterThan(before)
  })

  it('refresh-navigation event triggers reload', async () => {
    render(Sidebar, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()
    const before = mocks.sqliteQuery.mock.calls.length
    window.dispatchEvent(new CustomEvent('refresh-navigation'))
    await flush()
    expect(mocks.sqliteQuery.mock.calls.length).toBeGreaterThan(before)
  })

  it('error path: when sqliteQuery rejects, role="alert" renders the error message', async () => {
    mocks.sqliteQuery.mockImplementation(async () => {
      throw new Error('boom')
    })
    render(Sidebar, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()
    const alert = document.querySelector('[role="alert"]')
    expect(alert).toBeTruthy()
    expect(alert?.textContent ?? '').toContain('boom')
  })
})
