import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest'
import { tick } from 'svelte'
import { render, screen, cleanup, fireEvent } from '@testing-library/svelte'

// Mock seam mirrors TasksHub.test.ts: the sidebar persists through the
// PluginContext SDK (ctx.updatePluginSetting) and reads its slice via
// ctx.getPluginSettings. Sidebar doesn't self-init the settings module, so
// each test seeds it via initTasksSettings(ctx) in beforeEach.
const mocks = vi.hoisted(() => ({
  sqliteQuery: vi.fn(),
  updatePluginSetting: vi.fn().mockResolvedValue(true),
  tasksSettings: {} as Record<string, unknown>,
  blockChangedCallbacks: [] as Array<() => void>
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
import { initTasksSettings } from './settings'

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
  beforeEach(async () => {
    mocks.sqliteQuery.mockReset()
    mocks.updatePluginSetting.mockReset().mockResolvedValue(true)
    mocks.tasksSettings = {}
    mocks.blockChangedCallbacks.length = 0
    resetTaskHubState()
    // Seed the settings module so persistSavedViews' saveFn is wired to
    // mocks.updatePluginSetting (Sidebar doesn't self-init).
    await initTasksSettings(makeCtx())
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

  it('deleting a USER view via manage menu → confirm modal removes + persists', async () => {
    const userView: SavedView = {
      id: 'u1',
      name: 'My View',
      displayMode: 'list',
      groupBy: 'owner',
      scope: 'vault',
      filters: { owners: [], priorities: [], dueDate: '', tags: [] }
    }
    seedSavedViews([userView])
    render(Sidebar, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()
    // Open the manage menu via the ⋯ button.
    await fireEvent.click(screen.getByTestId('manage-view-u1'))
    await flush()
    expect(screen.getByTestId('manage-view-menu')).toBeInTheDocument()
    // Click Delete → opens the confirm modal (NOT window.confirm).
    await fireEvent.click(screen.getByTestId('manage-delete-view'))
    await flush()
    expect(screen.getByTestId('delete-view-confirm')).toBeInTheDocument()
    expect(screen.queryByTestId('manage-view-menu')).toBeNull()
    // Confirm the deletion.
    await fireEvent.click(screen.getByTestId('delete-view-confirm-confirm'))
    await flush()
    expect(mocks.updatePluginSetting).toHaveBeenCalledWith(
      'saved_views',
      expect.not.arrayContaining([expect.objectContaining({ id: 'u1' })])
    )
    expect(screen.queryByTestId('view-u1')).toBeNull()
  })

  it('cancelling the delete confirm modal leaves the list unchanged', async () => {
    const userView: SavedView = {
      id: 'u1',
      name: 'My View',
      displayMode: 'list',
      filters: { owners: [], priorities: [], dueDate: '', tags: [] }
    }
    seedSavedViews([userView])
    render(Sidebar, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()
    await fireEvent.click(screen.getByTestId('manage-view-u1'))
    await flush()
    await fireEvent.click(screen.getByTestId('manage-delete-view'))
    await flush()
    await fireEvent.click(screen.getByTestId('delete-view-confirm-cancel'))
    await flush()
    expect(screen.queryByTestId('delete-view-confirm')).toBeNull()
    expect(screen.getByTestId('view-u1')).toBeInTheDocument()
    expect(mocks.updatePluginSetting).not.toHaveBeenCalled()
  })

  it('delete confirm modal shows the view name in its message', async () => {
    const userView: SavedView = {
      id: 'u1',
      name: 'Sprint 42',
      displayMode: 'list',
      filters: { owners: [], priorities: [], dueDate: '', tags: [] }
    }
    seedSavedViews([userView])
    render(Sidebar, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()
    await fireEvent.click(screen.getByTestId('manage-view-u1'))
    await flush()
    await fireEvent.click(screen.getByTestId('manage-delete-view'))
    await flush()
    const modal = screen.getByTestId('delete-view-confirm')
    expect(modal.textContent).toContain('Sprint 42')
  })

  it('SYSTEM views have no manage button and no grip (read-only)', async () => {
    seedSavedViews()
    render(Sidebar, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()
    expect(screen.queryByTestId('manage-view-sys-today-board')).toBeNull()
    expect(screen.queryByTestId('manage-view-sys-by-owner')).toBeNull()
    expect(screen.queryByTestId('grip-sys-today-board')).toBeNull()
    // The view name button still renders.
    expect(screen.getByTestId('view-sys-today-board')).toBeInTheDocument()
  })

  it('right-clicking a USER view opens the manage menu', async () => {
    const userView: SavedView = {
      id: 'u1',
      name: 'My View',
      displayMode: 'list',
      filters: { owners: [], priorities: [], dueDate: '', tags: [] }
    }
    seedSavedViews([userView])
    render(Sidebar, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()
    const row = screen.getByTestId('view-row-u1')
    await fireEvent.contextMenu(row)
    await flush()
    expect(screen.getByTestId('manage-view-menu')).toBeInTheDocument()
  })

  it('right-clicking a SYSTEM view does NOT open the manage menu', async () => {
    seedSavedViews()
    render(Sidebar, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()
    const row = screen.getByTestId('view-row-sys-today-board')
    await fireEvent.contextMenu(row)
    await flush()
    expect(screen.queryByTestId('manage-view-menu')).toBeNull()
  })

  it('manage menu backdrop click closes the menu', async () => {
    const userView: SavedView = {
      id: 'u1',
      name: 'My View',
      displayMode: 'list',
      filters: { owners: [], priorities: [], dueDate: '', tags: [] }
    }
    seedSavedViews([userView])
    render(Sidebar, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()
    await fireEvent.click(screen.getByTestId('manage-view-u1'))
    await flush()
    expect(screen.getByTestId('manage-view-menu')).toBeInTheDocument()
    await fireEvent.click(screen.getByTestId('manage-view-backdrop'))
    await flush()
    expect(screen.queryByTestId('manage-view-menu')).toBeNull()
  })

  it('manage menu has aria-haspopup and the button identifies the view by name', async () => {
    const userView: SavedView = {
      id: 'u1',
      name: 'My Special View',
      displayMode: 'list',
      filters: { owners: [], priorities: [], dueDate: '', tags: [] }
    }
    seedSavedViews([userView])
    render(Sidebar, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()
    const btn = screen.getByTestId('manage-view-u1')
    expect(btn.getAttribute('aria-haspopup')).toBe('menu')
    expect(btn.getAttribute('aria-label')).toContain('My Special View')
  })

  // --- Rename (#470) ----------------------------------------------------

  it('rename: inline editor opens prefilled, Enter confirms, persists via saveView', async () => {
    const userView: SavedView = {
      id: 'u1',
      name: 'Old Name',
      displayMode: 'list',
      filters: { owners: [], priorities: [], dueDate: '', tags: [] }
    }
    seedSavedViews([userView])
    render(Sidebar, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()
    await fireEvent.click(screen.getByTestId('manage-view-u1'))
    await flush()
    await fireEvent.click(screen.getByTestId('manage-rename-view'))
    await flush()
    const input = screen.getByTestId('rename-input-u1') as HTMLInputElement
    expect(input.value).toBe('Old Name')
    await fireEvent.input(input, { target: { value: 'New Name' } })
    await fireEvent.keyDown(input, { key: 'Enter' })
    await flush()
    expect(mocks.updatePluginSetting).toHaveBeenCalledWith(
      'saved_views',
      expect.arrayContaining([expect.objectContaining({ name: 'New Name' })])
    )
    // Rename mode exited.
    expect(screen.queryByTestId('rename-input-u1')).toBeNull()
  })

  it('rename: Escape cancels without persisting', async () => {
    const userView: SavedView = {
      id: 'u1',
      name: 'Original',
      displayMode: 'list',
      filters: { owners: [], priorities: [], dueDate: '', tags: [] }
    }
    seedSavedViews([userView])
    render(Sidebar, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()
    await fireEvent.click(screen.getByTestId('manage-view-u1'))
    await flush()
    await fireEvent.click(screen.getByTestId('manage-rename-view'))
    await flush()
    const input = screen.getByTestId('rename-input-u1') as HTMLInputElement
    await fireEvent.input(input, { target: { value: 'Changed' } })
    await fireEvent.keyDown(input, { key: 'Escape' })
    await flush()
    expect(screen.queryByTestId('rename-input-u1')).toBeNull()
    expect(mocks.updatePluginSetting).not.toHaveBeenCalled()
    // Original name survives.
    expect(screen.getByTestId('view-u1').textContent).toContain('Original')
  })

  it('rename: empty name does not persist', async () => {
    const userView: SavedView = {
      id: 'u1',
      name: 'Keep Me',
      displayMode: 'list',
      filters: { owners: [], priorities: [], dueDate: '', tags: [] }
    }
    seedSavedViews([userView])
    render(Sidebar, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()
    await fireEvent.click(screen.getByTestId('manage-view-u1'))
    await flush()
    await fireEvent.click(screen.getByTestId('manage-rename-view'))
    await flush()
    const input = screen.getByTestId('rename-input-u1') as HTMLInputElement
    await fireEvent.input(input, { target: { value: '   ' } })
    await fireEvent.keyDown(input, { key: 'Enter' })
    await flush()
    expect(mocks.updatePluginSetting).not.toHaveBeenCalled()
    // Still in rename mode.
    expect(screen.getByTestId('rename-input-u1')).toBeInTheDocument()
  })

  it('rename: blur with non-empty value commits (focus loss persists)', async () => {
    const userView: SavedView = {
      id: 'u1',
      name: 'Blur Me',
      displayMode: 'list',
      filters: { owners: [], priorities: [], dueDate: '', tags: [] }
    }
    seedSavedViews([userView])
    render(Sidebar, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()
    await fireEvent.click(screen.getByTestId('manage-view-u1'))
    await flush()
    await fireEvent.click(screen.getByTestId('manage-rename-view'))
    await flush()
    const input = screen.getByTestId('rename-input-u1') as HTMLInputElement
    await fireEvent.input(input, { target: { value: 'Blurred Commit' } })
    // Simulate focus loss (clicking elsewhere) — should commit the non-empty value.
    await fireEvent.blur(input)
    await flush()
    expect(mocks.updatePluginSetting).toHaveBeenCalledWith(
      'saved_views',
      expect.arrayContaining([
        expect.objectContaining({ name: 'Blurred Commit' })
      ])
    )
    expect(screen.queryByTestId('rename-input-u1')).toBeNull()
  })

  // --- Overwrite / Update (#470) ----------------------------------------

  it('update: dirty indicator clears + current state overwrites the view', async () => {
    const userView: SavedView = {
      id: 'u1',
      name: 'My View',
      displayMode: 'list',
      groupBy: 'owner',
      sort: 'priority',
      scope: 'vault',
      filters: { owners: [], priorities: [], dueDate: '', tags: [] },
      calendarSubMode: 'month',
      columns: ['TODO', 'DOING', 'DONE']
    }
    seedSavedViews([userView])
    // Make the view active + dirty, with the live state diverged to board.
    getTaskHubState().activeSavedViewId = 'u1'
    getTaskHubState().savedViewsDirty = true
    getTaskHubState().displayMode = 'board'
    render(Sidebar, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()
    await fireEvent.click(screen.getByTestId('manage-view-u1'))
    await flush()
    // Update option is present because the view is active + dirty.
    expect(screen.getByTestId('manage-update-view')).toBeInTheDocument()
    await fireEvent.click(screen.getByTestId('manage-update-view'))
    await flush()
    const s = getTaskHubState()
    expect(s.savedViewsDirty).toBe(false)
    expect(s.savedViews.find((v) => v.id === 'u1')?.displayMode).toBe('board')
    expect(mocks.updatePluginSetting).toHaveBeenCalledWith(
      'saved_views',
      expect.arrayContaining([
        expect.objectContaining({ id: 'u1', displayMode: 'board' })
      ])
    )
  })

  it('update option is absent when the view is not the active dirty view', async () => {
    const userView: SavedView = {
      id: 'u1',
      name: 'My View',
      displayMode: 'list',
      filters: { owners: [], priorities: [], dueDate: '', tags: [] }
    }
    seedSavedViews([userView])
    render(Sidebar, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()
    await fireEvent.click(screen.getByTestId('manage-view-u1'))
    await flush()
    expect(screen.queryByTestId('manage-update-view')).toBeNull()
  })

  // --- Reorder (#470) ---------------------------------------------------

  it('drag: drop persists the new user-view order (system views anchored)', async () => {
    const u1: SavedView = {
      id: 'u1',
      name: 'First',
      displayMode: 'list',
      filters: { owners: [], priorities: [], dueDate: '', tags: [] }
    }
    const u2: SavedView = {
      id: 'u2',
      name: 'Second',
      displayMode: 'list',
      filters: { owners: [], priorities: [], dueDate: '', tags: [] }
    }
    seedSavedViews([u1, u2])
    render(Sidebar, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()
    const grip = screen.getByTestId('grip-u1')
    const targetRow = screen.getByTestId('view-row-u2')
    // Simulate HTML5 drag: dragstart on grip, dragover + drop on target row.
    await fireEvent.dragStart(grip)
    await fireEvent.dragOver(targetRow, { clientY: 100 })
    await fireEvent.drop(targetRow)
    await flush()
    const written = mocks.updatePluginSetting.mock.calls.find(
      (c) => c[0] === 'saved_views'
    )?.[1] as Array<{ id: string }> | undefined
    expect(written).toBeDefined()
    // u2 should now come before u1 in the persisted user-view order.
    const userIds = written!.filter((v) => !v.id.startsWith('sys-'))
    expect(userIds.map((v) => v.id)).toEqual(['u2', 'u1'])
  })

  it('keyboard: Move up persists the reordered list', async () => {
    const u1: SavedView = {
      id: 'u1',
      name: 'First',
      displayMode: 'list',
      filters: { owners: [], priorities: [], dueDate: '', tags: [] }
    }
    const u2: SavedView = {
      id: 'u2',
      name: 'Second',
      displayMode: 'list',
      filters: { owners: [], priorities: [], dueDate: '', tags: [] }
    }
    seedSavedViews([u1, u2])
    render(Sidebar, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()
    // Move u2 up via the manage menu.
    await fireEvent.click(screen.getByTestId('manage-view-u2'))
    await flush()
    expect(screen.getByTestId('manage-move-up').hasAttribute('disabled')).toBe(
      false
    )
    await fireEvent.click(screen.getByTestId('manage-move-up'))
    await flush()
    const written = mocks.updatePluginSetting.mock.calls.find(
      (c) => c[0] === 'saved_views'
    )?.[1] as Array<{ id: string }> | undefined
    expect(written).toBeDefined()
    const userIds = written!.filter((v) => !v.id.startsWith('sys-'))
    expect(userIds.map((v) => v.id)).toEqual(['u2', 'u1'])
  })

  it('Move up is disabled for the first user view', async () => {
    const u1: SavedView = {
      id: 'u1',
      name: 'First',
      displayMode: 'list',
      filters: { owners: [], priorities: [], dueDate: '', tags: [] }
    }
    seedSavedViews([u1])
    render(Sidebar, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()
    await fireEvent.click(screen.getByTestId('manage-view-u1'))
    await flush()
    const moveUp = screen.getByTestId('manage-move-up') as HTMLButtonElement
    expect(moveUp.disabled).toBe(true)
  })

  it('keyboard: ArrowDown navigates the manage menu items', async () => {
    const u1: SavedView = {
      id: 'u1',
      name: 'My View',
      displayMode: 'list',
      filters: { owners: [], priorities: [], dueDate: '', tags: [] }
    }
    const u2: SavedView = {
      id: 'u2',
      name: 'Other',
      displayMode: 'list',
      filters: { owners: [], priorities: [], dueDate: '', tags: [] }
    }
    seedSavedViews([u1, u2])
    render(Sidebar, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()
    // Open u2's menu — u2 can move up (it's the 2nd user view).
    await fireEvent.click(screen.getByTestId('manage-view-u2'))
    await flush()
    const menu = screen.getByTestId('manage-view-menu')
    // Focus the first item (Rename).
    const renameItem = screen.getByTestId('manage-rename-view')
    renameItem.focus()
    await flush()
    expect(document.activeElement).toBe(renameItem)
    // ArrowDown should move to the next enabled item (Move up).
    await fireEvent.keyDown(menu, { key: 'ArrowDown' })
    await flush()
    const moveUp = screen.getByTestId('manage-move-up')
    expect(document.activeElement).toBe(moveUp)
  })

  it('keyboard: Escape closes the manage menu', async () => {
    const u1: SavedView = {
      id: 'u1',
      name: 'My View',
      displayMode: 'list',
      filters: { owners: [], priorities: [], dueDate: '', tags: [] }
    }
    seedSavedViews([u1])
    render(Sidebar, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()
    await fireEvent.click(screen.getByTestId('manage-view-u1'))
    await flush()
    expect(screen.getByTestId('manage-view-menu')).toBeInTheDocument()
    const menu = screen.getByTestId('manage-view-menu')
    await fireEvent.keyDown(menu, { key: 'Escape' })
    await flush()
    expect(screen.queryByTestId('manage-view-menu')).toBeNull()
  })

  // --- #489: dismiss the one-shot context menu on scroll / resize ----------
  it('dismisses the manage menu on scroll (#489)', async () => {
    const u1: SavedView = {
      id: 'u1',
      name: 'My View',
      displayMode: 'list',
      filters: { owners: [], priorities: [], dueDate: '', tags: [] }
    }
    seedSavedViews([u1])
    render(Sidebar, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()
    await fireEvent.click(screen.getByTestId('manage-view-u1'))
    await flush()
    expect(screen.getByTestId('manage-view-menu')).toBeInTheDocument()
    // A scroll in any container dismisses via the capture-phase listener —
    // the menu's anchor is a one-shot position, not a tracked element.
    document.dispatchEvent(new Event('scroll', { bubbles: true }))
    await flush()
    expect(screen.queryByTestId('manage-view-menu')).toBeNull()
  })

  it('dismisses the manage menu on window resize (#489)', async () => {
    const u1: SavedView = {
      id: 'u1',
      name: 'My View',
      displayMode: 'list',
      filters: { owners: [], priorities: [], dueDate: '', tags: [] }
    }
    seedSavedViews([u1])
    render(Sidebar, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()
    await fireEvent.click(screen.getByTestId('manage-view-u1'))
    await flush()
    expect(screen.getByTestId('manage-view-menu')).toBeInTheDocument()
    window.dispatchEvent(new Event('resize'))
    await flush()
    expect(screen.queryByTestId('manage-view-menu')).toBeNull()
  })

  // --- #492: scroll-scope — unrelated editor scroll keeps the menu open ----

  it('scroll-scope: unrelated editor scroll does not dismiss (#492)', async () => {
    const u1: SavedView = {
      id: 'u1',
      name: 'My View',
      displayMode: 'list',
      filters: { owners: [], priorities: [], dueDate: '', tags: [] }
    }
    seedSavedViews([u1])
    // Create a scrollable sidebar wrapper and an unrelated editor area.
    const sidebarWrapper = document.createElement('div')
    sidebarWrapper.style.overflowY = 'auto'
    sidebarWrapper.style.height = '300px'
    document.body.appendChild(sidebarWrapper)

    const editorArea = document.createElement('div')
    editorArea.style.overflowY = 'auto'
    editorArea.style.height = '300px'
    document.body.appendChild(editorArea)

    render(Sidebar, {
      target: sidebarWrapper,
      props: { ctx: makeCtx(), manifest: MANIFEST }
    })
    await flush()
    await fireEvent.click(screen.getByTestId('manage-view-u1'))
    await flush()
    expect(screen.getByTestId('manage-view-menu')).toBeInTheDocument()

    // Scrolling the unrelated editor should NOT dismiss.
    editorArea.dispatchEvent(new Event('scroll', { bubbles: true }))
    await flush()
    expect(screen.getByTestId('manage-view-menu')).toBeInTheDocument()

    // Scrolling the sidebar's own wrapper should dismiss.
    sidebarWrapper.dispatchEvent(new Event('scroll', { bubbles: true }))
    await flush()
    expect(screen.queryByTestId('manage-view-menu')).toBeNull()

    document.body.removeChild(sidebarWrapper)
    document.body.removeChild(editorArea)
  })

  it('active view shows a dirty dot when savedViewsDirty is true', async () => {
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
    // Dirty signal is now a colored dot (aria-label="modified") rather than
    // text — assert the dot is present on the active dirty row.
    const dot = viewChip?.querySelector('[aria-label="modified"]')
    expect(dot).toBeTruthy()
    expect(dot?.getAttribute('title')).toBe('This view has unsaved changes')
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

  it('toggle mini calendar collapses and expands the calendar grid', async () => {
    render(Sidebar, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()
    // By default, it is expanded, so mini-today is visible.
    expect(screen.getByTestId('mini-today')).toBeInTheDocument()

    // Click to collapse
    await fireEvent.click(screen.getByTestId('toggle-mini-calendar'))
    await flush()
    expect(screen.queryByTestId('mini-today')).toBeNull()

    // Click to expand again
    await fireEvent.click(screen.getByTestId('toggle-mini-calendar'))
    await flush()
    expect(screen.getByTestId('mini-today')).toBeInTheDocument()
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
