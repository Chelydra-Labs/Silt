import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest'
import { tick } from 'svelte'
import {
  render,
  screen,
  cleanup,
  fireEvent,
  within
} from '@testing-library/svelte'

// jsdom polyfills — BoardView pulls in TaskEditDrawer/TaskSubEditorModal
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

// Hoisted mutable mock state — loadColumns() reads from the settings module's
// slice (seeded via initTasksSettings in beforeEach), and every SDK setter is
// a spy so the dimension-aware DnD dispatcher can be asserted per-dimension.
const mocks = vi.hoisted(() => ({
  tasksSettings: {},
  sqliteQuery: vi.fn(),
  updateBlockState: vi.fn(),
  setTaskOwner: vi.fn(),
  setTaskPriority: vi.fn(),
  setTaskDueDate: vi.fn(),
  setTaskTags: vi.fn(),
  setTaskOrder: vi.fn(),
  setTaskOrders: vi.fn(),
  createTask: vi.fn().mockResolvedValue('new-task-id'),
  getTaskBlockers: vi.fn().mockResolvedValue([]),
  updatePluginSetting: vi.fn().mockResolvedValue(true),
  notify: vi.fn().mockResolvedValue(true)
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

import BoardView from './BoardView.svelte'
import type { PluginContext } from '../../../sdk'
import { v2CtxStubs } from '../../../test-helpers'
import {
  getTaskHubState,
  resetTaskHubState,
  enterTaskPageRoute,
  clearTaskPageRoute,
  setGroupBy,
  setSort,
  setActiveFilter
} from '../state.svelte'
import { initTasksSettings } from '../settings'

const TODAY = '2026-07-06'

function makeCtx(overrides: Partial<PluginContext> = {}): PluginContext {
  return {
    ...v2CtxStubs,
    activeNotebook: 'Work',
    activeSection: 'Journal',
    activePage: 'Daily',
    today: TODAY,
    sqliteQuery: mocks.sqliteQuery,
    mutateBlock: vi.fn(),
    updateBlockState: mocks.updateBlockState,
    updateTaskMeta: vi.fn(),
    setTaskOwner: mocks.setTaskOwner,
    setTaskPriority: mocks.setTaskPriority,
    setTaskDueDate: mocks.setTaskDueDate,
    setTaskTags: mocks.setTaskTags,
    setTaskOrder: mocks.setTaskOrder,
    setTaskOrders: mocks.setTaskOrders,
    createTask: mocks.createTask,
    getTaskBlockers: mocks.getTaskBlockers,
    getPluginSettings: vi.fn(() => Promise.resolve(mocks.tasksSettings)),
    updatePluginSetting: mocks.updatePluginSetting,
    notify: mocks.notify,
    on: () => () => {},
    ...overrides
  }
}

// A row factory covering every TaskDetail field the query/projects so the
// card richness markup (priority stripe, owner badge, blocked indicator)
// renders without coercion holes.
function row(p: Partial<Record<string, unknown>>): Record<string, unknown> {
  return {
    id: 'r',
    source: 'vault',
    notebook: 'Work',
    section: 'Journal',
    page: 'Daily',
    file_date: '2026-07-01',
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

// Locate a rendered card by its clean_content. Within-column manual
// reorders reshuffle DOM order, so position-based lookups (cards[N]) are
// unstable across an optimistic update; text lookup stays correct.
function cardByText(text: string): HTMLElement {
  return Array.from(document.querySelectorAll<HTMLElement>('[data-card]')).find(
    (c) => c.textContent?.includes(text)
  )!
}

async function renderBoard(
  g: ReturnType<typeof setGroupBy> extends never
    ? never
    : Parameters<typeof setGroupBy>[0],
  rows: Record<string, unknown>[] = []
) {
  resetTaskHubState()
  setGroupBy(g)
  mocks.sqliteQuery.mockReset()
  mocks.sqliteQuery.mockResolvedValue({ rows, truncated: false })
  const ctx = makeCtx()
  const onCountChange = vi.fn()
  render(BoardView, { ctx, onCountChange })
  await flush()
  return { ctx, onCountChange }
}

// Variant that lets a test pin the sort mode before render (used by the
// manual-order tests so setTaskOrder fires on cross-column drops).
async function renderBoardWithSort(
  g: Parameters<typeof setGroupBy>[0],
  sort: Parameters<typeof setSort>[0],
  rows: Record<string, unknown>[] = []
) {
  resetTaskHubState()
  setGroupBy(g)
  setSort(sort)
  mocks.sqliteQuery.mockReset()
  mocks.sqliteQuery.mockResolvedValue({ rows, truncated: false })
  const ctx = makeCtx()
  const onCountChange = vi.fn()
  render(BoardView, { ctx, onCountChange })
  await flush()
  return { ctx, onCountChange }
}

describe('BoardView — dimension-aware Board (#421)', () => {
  beforeEach(async () => {
    mocks.tasksSettings = {}
    mocks.updateBlockState.mockReset().mockResolvedValue(true)
    mocks.setTaskOwner.mockReset().mockResolvedValue(true)
    mocks.setTaskPriority.mockReset().mockResolvedValue(true)
    mocks.setTaskDueDate.mockReset().mockResolvedValue(true)
    mocks.setTaskTags.mockReset().mockResolvedValue(true)
    mocks.setTaskOrder.mockReset().mockResolvedValue(true)
    mocks.setTaskOrders.mockReset().mockResolvedValue(true)
    mocks.createTask.mockReset().mockResolvedValue('new-task-id')
    mocks.getTaskBlockers.mockReset().mockResolvedValue([])
    mocks.updatePluginSetting.mockReset().mockResolvedValue(true)
    mocks.notify.mockReset().mockResolvedValue(true)
    // Seed the settings module AFTER resets so loadColumns() (read at
    // BoardView construction) and persistColumns() (saveFn) are wired to
    // the freshly-reset mock slice.
    await initTasksSettings(makeCtx())
  })

  afterEach(() => {
    cleanup()
    clearTaskPageRoute()
  })

  it('queries the explicit page route instead of ambient navigation', async () => {
    resetTaskHubState()
    setGroupBy('status')
    enterTaskPageRoute({
      source: 'linked:meetings',
      notebook: 'Work',
      section: 'Meetings',
      page: 'Sprint Review',
      nonce: 'board-route'
    })
    mocks.sqliteQuery.mockReset().mockResolvedValue({
      rows: [],
      truncated: false
    })

    render(BoardView, { ctx: makeCtx(), onCountChange: vi.fn() })
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

  // --- Column derivation per dimension ----------------------------------

  it('renders TODO/DOING/DONE columns for groupBy=status', async () => {
    await renderBoard('status', [
      row({ id: 't1', status: 'TODO', clean_content: 'A' }),
      row({ id: 't2', status: 'DOING', clean_content: 'B' }),
      row({ id: 't3', status: 'DONE', clean_content: 'C' })
    ])

    expect(screen.getByRole('group', { name: 'To Do' })).toBeInTheDocument()
    expect(
      screen.getByRole('group', { name: 'In Progress' })
    ).toBeInTheDocument()
    expect(screen.getByRole('group', { name: 'Done' })).toBeInTheDocument()
  })

  it('renders distinct owners + an Unassigned column on the far left (groupBy=owner)', async () => {
    await renderBoard('owner', [
      row({ id: 't1', owner: 'Alice', clean_content: 'A' }),
      row({ id: 't2', owner: 'Bob', clean_content: 'B' }),
      row({ id: 't3', owner: '', clean_content: 'C' })
    ])

    const groups = screen.getAllByRole('group')
    const labels = groups.map((g) => g.getAttribute('aria-label'))
    // Unassigned first, then owners alphabetically.
    expect(labels[0]).toBe('Unassigned')
    expect(labels).toContain('Alice')
    expect(labels).toContain('Bob')
  })

  it('renders P1/P2/P3 columns for groupBy=priority (legacy priority-0 joins P3)', async () => {
    await renderBoard('priority', [
      row({ id: 't1', priority: 1, clean_content: 'A' }),
      row({ id: 't2', priority: 2, clean_content: 'B' }),
      row({ id: 't3', priority: 3, clean_content: 'C' }),
      row({ id: 't4', priority: 0, clean_content: 'legacy' })
    ])

    expect(screen.getByRole('group', { name: 'Critical' })).toBeInTheDocument()
    expect(screen.getByRole('group', { name: 'Normal' })).toBeInTheDocument()
    expect(screen.getByRole('group', { name: 'Low' })).toBeInTheDocument()
    // The priority-0 row lands in the Low (P3) column alongside real P3s.
    const lowCards = screen
      .getByRole('group', { name: 'Low' })
      .querySelectorAll('[data-card]')
    const lowText = Array.from(lowCards)
      .map((c) => c.textContent)
      .join(' ')
    expect(lowText).toContain('legacy')
  })

  // --- Dimension-aware DnD dispatcher -----------------------------------

  it('status drop calls updateBlockState(id, status)', async () => {
    await renderBoard('status', [
      row({ id: 't1', status: 'TODO', clean_content: 'A' }),
      row({ id: 't2', status: 'DOING', clean_content: 'B' })
    ])

    const todoCard = screen
      .getByRole('group', { name: 'To Do' })
      .querySelector<HTMLElement>('[data-card]')!
    const doingCol = screen.getByRole('group', { name: 'In Progress' })

    await fireEvent.dragStart(todoCard)
    await fireEvent.drop(doingCol)
    await flush()

    expect(mocks.updateBlockState).toHaveBeenCalledWith('t1', 'DOING')
  })

  it('owner drop calls setTaskOwner(id, owner)', async () => {
    await renderBoard('owner', [
      row({ id: 't1', owner: 'Alice', clean_content: 'A' }),
      row({ id: 't2', owner: 'Bob', clean_content: 'B' })
    ])

    const aliceCard = screen
      .getByRole('group', { name: 'Alice' })
      .querySelector<HTMLElement>('[data-card]')!
    const bobCol = screen.getByRole('group', { name: 'Bob' })

    await fireEvent.dragStart(aliceCard)
    await fireEvent.drop(bobCol)
    await flush()

    expect(mocks.setTaskOwner).toHaveBeenCalledWith('t1', 'Bob')
    // status/priority/dueDate/tags setters are NOT touched.
    expect(mocks.updateBlockState).not.toHaveBeenCalled()
    expect(mocks.setTaskPriority).not.toHaveBeenCalled()
  })

  it('priority drop calls setTaskPriority(id, priority)', async () => {
    await renderBoard('priority', [
      row({ id: 't1', priority: 3, clean_content: 'low' }),
      row({ id: 't2', priority: 1, clean_content: 'crit' })
    ])

    const lowCard = screen
      .getByRole('group', { name: 'Low' })
      .querySelector<HTMLElement>('[data-card]')!
    const critCol = screen.getByRole('group', { name: 'Critical' })

    await fireEvent.dragStart(lowCard)
    await fireEvent.drop(critCol)
    await flush()

    expect(mocks.setTaskPriority).toHaveBeenCalledWith('t1', 1)
  })

  it('dueDate drop calls setTaskDueDate(id, anchor)', async () => {
    await renderBoard('dueDate', [
      row({ id: 't1', due_date: '2026-06-01', clean_content: 'overdue' }),
      row({ id: 't2', due_date: TODAY, clean_content: 'today' })
    ])

    const overdueCard = screen
      .getByRole('group', { name: 'Overdue' })
      .querySelector<HTMLElement>('[data-card]')!
    const todayCol = screen.getByRole('group', { name: 'Today' })

    await fireEvent.dragStart(overdueCard)
    await fireEvent.drop(todayCol)
    await flush()

    // Today bucket anchors on the local day.
    expect(mocks.setTaskDueDate).toHaveBeenCalledWith('t1', TODAY)
  })

  it('tag drop calls setTaskTags(id, unionOfExistingPlusNewTag)', async () => {
    await renderBoard('tag', [
      row({ id: 't1', tags: 'alpha', clean_content: 'A' }),
      row({ id: 't2', tags: 'beta', clean_content: 'B' })
    ])

    const alphaCard = screen
      .getByRole('group', { name: 'alpha' })
      .querySelector<HTMLElement>('[data-card]')!
    const betaCol = screen.getByRole('group', { name: 'beta' })

    await fireEvent.dragStart(alphaCard)
    await fireEvent.drop(betaCol)
    await flush()

    // Multi-membership: the existing 'alpha' tag is preserved, 'beta' added.
    expect(mocks.setTaskTags).toHaveBeenCalledWith('t1', ['alpha', 'beta'])
  })

  // --- DnD disabled for location dimensions -----------------------------

  it('cards are not draggable for groupBy=notebook (location is read-only)', async () => {
    await renderBoard('notebook', [
      row({ id: 't1', notebook: 'Work', clean_content: 'A' }),
      row({ id: 't2', notebook: 'Personal', clean_content: 'B' })
    ])

    const workCard = screen
      .getByRole('group', { name: 'Work' })
      .querySelector<HTMLElement>('[data-card]')!
    // draggable="false" — the card can't initiate a drag.
    expect(workCard.getAttribute('draggable')).toBe('false')

    // Even if a drop is dispatched, no setter fires (DnD is disabled).
    const personalCol = screen.getByRole('group', { name: 'Personal' })
    await fireEvent.dragStart(workCard)
    await fireEvent.drop(personalCol)
    await flush()
    expect(mocks.updateBlockState).not.toHaveBeenCalled()
    expect(mocks.setTaskOwner).not.toHaveBeenCalled()
  })

  // --- DONE guard --------------------------------------------------------

  it('dragging a blocked card to DONE opens the BlockedDoneDialog', async () => {
    mocks.getTaskBlockers.mockResolvedValue([
      { id: 'pre', clean_content: 'Prerequisite' }
    ])
    await renderBoard('status', [
      row({ id: 'tb', status: 'TODO', clean_content: 'blocked', is_blocked: 1 })
    ])

    const card = screen
      .getByRole('group', { name: 'To Do' })
      .querySelector<HTMLElement>('[data-card]')!
    const doneCol = screen.getByRole('group', { name: 'Done' })

    await fireEvent.dragStart(card)
    await fireEvent.drop(doneCol)
    await flush()

    // The guard pauses before persisting; the confirm dialog is shown.
    expect(
      await screen.findByRole('alertdialog', { name: 'Complete blocked task?' })
    ).toBeInTheDocument()
    expect(mocks.getTaskBlockers).toHaveBeenCalledWith('tb')
    // No DONE write until the user confirms.
    expect(mocks.updateBlockState).not.toHaveBeenCalled()
  })

  it('confirmBlockedDone writes DONE + cross-column manual order when sort=manual', async () => {
    mocks.getTaskBlockers.mockResolvedValue([
      { id: 'pre', clean_content: 'Prerequisite' }
    ])
    // DONE already has one card at manual_order=5 so max+1 = 6 (gap-tolerant).
    await renderBoardWithSort('status', 'manual', [
      row({
        id: 'tb',
        status: 'TODO',
        clean_content: 'blocked',
        is_blocked: 1
      }),
      row({
        id: 'd1',
        status: 'DONE',
        clean_content: 'existing done',
        manual_order: 5
      })
    ])

    const card = screen
      .getByRole('group', { name: 'To Do' })
      .querySelector<HTMLElement>('[data-card]')!
    const doneCol = screen.getByRole('group', { name: 'Done' })

    await fireEvent.dragStart(card)
    await fireEvent.drop(doneCol)
    await flush()

    const dialog = await screen.findByRole('alertdialog', {
      name: 'Complete blocked task?'
    })
    expect(dialog).toBeInTheDocument()
    expect(mocks.updateBlockState).not.toHaveBeenCalled()

    // Confirm the dialog → DONE persists AND setTaskOrder assigns the
    // destination's tail order (max([5]) + 1 = 6).
    await fireEvent.click(
      screen.getByRole('button', { name: 'Complete anyway' })
    )
    await flush()

    expect(mocks.updateBlockState).toHaveBeenCalledWith('tb', 'DONE')
    expect(mocks.setTaskOrder).toHaveBeenCalledWith('tb', 6)
  })

  it('cancelBlockedDone reverts card to source column without persisting', async () => {
    mocks.getTaskBlockers.mockResolvedValue([
      { id: 'pre', clean_content: 'Prerequisite' }
    ])
    await renderBoard('status', [
      row({ id: 'tb', status: 'TODO', clean_content: 'blocked', is_blocked: 1 })
    ])

    const card = screen
      .getByRole('group', { name: 'To Do' })
      .querySelector<HTMLElement>('[data-card]')!
    const doneCol = screen.getByRole('group', { name: 'Done' })

    await fireEvent.dragStart(card)
    await fireEvent.drop(doneCol)
    await flush()

    await screen.findByRole('alertdialog', { name: 'Complete blocked task?' })

    // Cancel → the optimistic DONE placement reverts; the card is back in
    // To Do and no DONE write fired.
    await fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    await flush()

    expect(mocks.updateBlockState).not.toHaveBeenCalled()
    const todoCards = screen
      .getByRole('group', { name: 'To Do' })
      .querySelectorAll('[data-card]')
    expect(todoCards).toHaveLength(1)
    expect(
      screen
        .getByRole('group', { name: 'Done' })
        .querySelectorAll('[data-card]')
    ).toHaveLength(0)
  })

  it('getTaskBlockers rejection proceeds with persist (no dialog)', async () => {
    mocks.getTaskBlockers.mockRejectedValue(new Error('blocker lookup failed'))
    await renderBoard('status', [
      row({ id: 'tb', status: 'TODO', clean_content: 'blocked', is_blocked: 1 })
    ])

    const card = screen
      .getByRole('group', { name: 'To Do' })
      .querySelector<HTMLElement>('[data-card]')!
    const doneCol = screen.getByRole('group', { name: 'Done' })

    await fireEvent.dragStart(card)
    await fireEvent.drop(doneCol)
    await flush()

    // Blocker lookup failed → the persist proceeds rather than stranding the
    // card in an un-committed optimistic state. No dialog opens.
    expect(
      screen.queryByRole('alertdialog', { name: 'Complete blocked task?' })
    ).toBeNull()
    expect(mocks.updateBlockState).toHaveBeenCalledWith('tb', 'DONE')
  })

  it('is_blocked=true but no blockers proceeds without dialog', async () => {
    // is_blocked is set but the blocker query returns empty — the task was
    // flagged stale (a prerequisite was deleted without clearing the flag).
    // The drop should proceed to DONE without surfacing the confirm dialog.
    mocks.getTaskBlockers.mockResolvedValue([])
    await renderBoard('status', [
      row({ id: 'tb', status: 'TODO', clean_content: 'blocked', is_blocked: 1 })
    ])

    const card = screen
      .getByRole('group', { name: 'To Do' })
      .querySelector<HTMLElement>('[data-card]')!
    const doneCol = screen.getByRole('group', { name: 'Done' })

    await fireEvent.dragStart(card)
    await fireEvent.drop(doneCol)
    await flush()

    expect(
      screen.queryByRole('alertdialog', { name: 'Complete blocked task?' })
    ).toBeNull()
    expect(mocks.updateBlockState).toHaveBeenCalledWith('tb', 'DONE')
  })

  it('second drop during blocker await does not strand first card in DONE', async () => {
    // Card A is blocked (enters the DONE-guard path and awaits
    // getTaskBlockers); card B is unblocked. While A's blocker lookup is
    // pending, dropping B to DONE bumps moveSeq — A's commitDrop must
    // return without stranding A in DONE (no optimistic placement, no
    // dialog, no persist). Was: applyOptimistic ran before the await, so
    // the early return left A visually in DONE with no revert/persist.
    let resolveBlockers!: (v: { id: string; clean_content: string }[]) => void
    const blockersPending = new Promise<
      {
        id: string
        clean_content: string
      }[]
    >((r) => {
      resolveBlockers = r
    })
    mocks.getTaskBlockers.mockReturnValue(blockersPending)
    await renderBoard('status', [
      row({
        id: 'a',
        status: 'TODO',
        clean_content: 'blocked A',
        is_blocked: 1
      }),
      row({ id: 'b', status: 'TODO', clean_content: 'free B', is_blocked: 0 })
    ])

    const todoCards = screen
      .getByRole('group', { name: 'To Do' })
      .querySelectorAll<HTMLElement>('[data-card]')
    const cardA = todoCards[0]
    const cardB = todoCards[1]
    const doneCol = screen.getByRole('group', { name: 'Done' })

    // Drop A to DONE — enters the blocked-guard and awaits getTaskBlockers.
    await fireEvent.dragStart(cardA)
    await fireEvent.drop(doneCol)
    await tick()

    // While A's blocker lookup is pending, drop B to DONE — bumps moveSeq.
    await fireEvent.dragStart(cardB)
    await fireEvent.drop(doneCol)
    await flush()

    // Resolve A's blocker lookup — A's commitDrop resumes, sees my !== moveSeq,
    // and returns without stranding A in DONE.
    resolveBlockers([{ id: 'pre', clean_content: 'Prerequisite' }])
    await flush()

    // A is NOT stranded in DONE — no DONE write for A.
    expect(mocks.updateBlockState).not.toHaveBeenCalledWith('a', 'DONE')
    // B's normal drop to DONE did proceed.
    expect(mocks.updateBlockState).toHaveBeenCalledWith('b', 'DONE')
    // No confirm dialog opened for A (nothing stranded).
    expect(
      screen.queryByRole('alertdialog', { name: 'Complete blocked task?' })
    ).toBeNull()
    // A is still in To Do, not Done.
    const doneTexts = Array.from(
      screen
        .getByRole('group', { name: 'Done' })
        .querySelectorAll('[data-card]')
    ).map((c) => c.textContent)
    expect(doneTexts.some((t) => t?.includes('blocked A'))).toBe(false)
  })

  // --- Keyboard parity ---------------------------------------------------

  it('ArrowRight on a focused TODO card moves it to DOING (status)', async () => {
    await renderBoard('status', [
      row({ id: 't1', status: 'TODO', clean_content: 'A' }),
      row({ id: 't2', status: 'DOING', clean_content: 'B' })
    ])

    const card = screen
      .getByRole('group', { name: 'To Do' })
      .querySelector<HTMLElement>('[data-card]')!
    card.focus()
    await fireEvent.keyDown(card, { key: 'ArrowRight' })

    expect(mocks.updateBlockState).toHaveBeenCalledWith('t1', 'DOING')
  })

  // --- High-cardinality guard -------------------------------------------

  it('falls back to List mode when >20 distinct columns', async () => {
    // 25 distinct owners → 25 columns → over the 20-column cap.
    const manyRows = Array.from({ length: 25 }, (_, i) =>
      row({ id: `t${i}`, owner: `Owner${i}`, clean_content: `T${i}` })
    )
    await renderBoard('owner', manyRows)

    // The guard auto-switches the hub to List mode.
    expect(getTaskHubState().displayMode).toBe('list')
  })

  // --- Per-column quick-add ---------------------------------------------

  it('quick-add in a status column creates a task with that status', async () => {
    await renderBoard('status', [
      row({ id: 't1', status: 'TODO', clean_content: 'A' })
    ])

    await fireEvent.click(screen.getByTestId('board-add-status-TODO'))
    const input = await screen.findByTestId('quick-add-task-input')
    await fireEvent.input(input, { target: { value: 'New thing' } })
    await fireEvent.keyDown(input, { key: 'Enter' })
    await flush()

    expect(mocks.createTask).toHaveBeenCalledTimes(1)
    expect(mocks.createTask.mock.calls[0][0]).toMatchObject({
      title: 'New thing',
      status: 'TODO'
    })
  })

  it('quick-add in an owner column creates a task then assigns the owner', async () => {
    await renderBoard('owner', [
      row({ id: 't1', owner: 'Alice', clean_content: 'A' })
    ])

    await fireEvent.click(screen.getByTestId('board-add-owner-Alice'))
    const input = await screen.findByTestId('quick-add-task-input')
    await fireEvent.input(input, { target: { value: 'Owned' } })
    await fireEvent.keyDown(input, { key: 'Enter' })
    await flush()

    expect(mocks.createTask).toHaveBeenCalledTimes(1)
    // After create, the owner is set via the onCreated hook (createTask's
    // SDK signature doesn't accept owner/priority/tags directly).
    expect(mocks.setTaskOwner).toHaveBeenCalledWith('new-task-id', 'Alice')
  })

  // --- Column management (status) ---------------------------------------

  it('add column prompts for a name and persists via updatePluginSetting', async () => {
    const promptSpy = vi.spyOn(window, 'prompt').mockReturnValue('Backlog')
    await renderBoard('status', [])

    await fireEvent.click(screen.getByRole('button', { name: /add column/i }))
    await flush()

    expect(promptSpy).toHaveBeenCalled()
    expect(screen.getByRole('group', { name: 'Backlog' })).toBeInTheDocument()
    expect(mocks.updatePluginSetting).toHaveBeenCalledWith(
      'columns',
      expect.arrayContaining([{ name: 'Backlog' }])
    )
    promptSpy.mockRestore()
  })

  it('remove column confirms and drops the lane', async () => {
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(true)
    await renderBoard('status', [])

    const menus = screen.getAllByRole('button', { name: 'Column actions' })
    await fireEvent.click(menus[0])
    await flush()
    await fireEvent.click(screen.getByRole('menuitem', { name: /remove/i }))
    await flush()

    expect(confirmSpy).toHaveBeenCalled()
    expect(
      screen.queryByRole('group', { name: 'To Do' })
    ).not.toBeInTheDocument()
    confirmSpy.mockRestore()
  })

  // --- Drawer integration ------------------------------------------------

  it('single-click on a card opens the TaskEditDrawer', async () => {
    await renderBoard('status', [
      row({ id: 't1', status: 'TODO', clean_content: 'Open me' })
    ])

    const card = screen
      .getByRole('group', { name: 'To Do' })
      .querySelector<HTMLElement>('[data-card]')!
    await fireEvent.click(card)
    await flush()

    const dialog = screen.getByRole('dialog')
    expect(dialog).toHaveAttribute('aria-modal', 'false')
    expect(dialog.textContent).toContain('Open me')
  })

  // --- Optimistic + revert ----------------------------------------------

  it('a failed status setter reverts the card and shows a role="alert" banner', async () => {
    mocks.updateBlockState.mockRejectedValue(new Error('lock held'))
    await renderBoard('status', [
      row({ id: 't1', status: 'TODO', clean_content: 'A' }),
      row({ id: 't2', status: 'DOING', clean_content: 'B' })
    ])

    const card = screen
      .getByRole('group', { name: 'To Do' })
      .querySelector<HTMLElement>('[data-card]')!
    card.focus()
    await fireEvent.keyDown(card, { key: 'ArrowRight' })
    await flush()

    const alert = screen.getByRole('alert')
    expect(alert.textContent).toContain("Couldn't move task")
    // Card is still in To Do (reverted).
    const todoCards = screen
      .getByRole('group', { name: 'To Do' })
      .querySelectorAll('[data-card]')
    expect(todoCards).toHaveLength(1)
  })

  // --- Count reporting ---------------------------------------------------

  it('reports open/done counts to the hub via onCountChange', async () => {
    const { onCountChange } = await renderBoard('status', [
      row({ id: 't1', status: 'TODO', clean_content: 'A' }),
      row({ id: 't2', status: 'DOING', clean_content: 'B' }),
      row({ id: 't3', status: 'DONE', clean_content: 'C' })
    ])

    const last = onCountChange.mock.calls.at(-1)
    expect(last?.[0]).toBe(2) // open
    expect(last?.[1]).toBe(1) // done
  })

  // --- Manual ordering (#426) ------------------------------------------

  it('sort=manual: dropping a card onto a sibling in the SAME column renumbers via setTaskOrders', async () => {
    await renderBoardWithSort('status', 'manual', [
      row({ id: 'a', status: 'TODO', clean_content: 'A', manual_order: 1 }),
      row({ id: 'b', status: 'TODO', clean_content: 'B', manual_order: 2 }),
      row({ id: 'c', status: 'TODO', clean_content: 'C', manual_order: 3 })
    ])

    const cards = screen
      .getByRole('group', { name: 'To Do' })
      .querySelectorAll<HTMLElement>('[data-card]')
    const cardA = cards[0]
    const cardC = cards[2]

    await fireEvent.dragStart(cardA)
    await fireEvent.drop(cardC)
    await flush()

    // Splice-dance "land BEFORE target": [a,b,c] → remove a → [b,c] →
    // insert a before c (idx 1) → [b, a, c] → new orders b=1 (was 2),
    // a=2 (was 1), c=3 (unchanged). The two changed rows are persisted in
    // ONE batched setTaskOrders call (one atomic write per file).
    expect(mocks.setTaskOrders).toHaveBeenCalledTimes(1)
    const batch = mocks.setTaskOrders.mock.calls[0][0] as {
      id: string
      order: number
    }[]
    expect(batch).toEqual(
      expect.arrayContaining([
        { id: 'b', order: 1 },
        { id: 'a', order: 2 }
      ])
    )
    // The unchanged row (c) is not persisted.
    expect(batch.some((x) => x.id === 'c')).toBe(false)
    // The dimension setter did NOT fire (within-column manual is order-only).
    expect(mocks.updateBlockState).not.toHaveBeenCalled()
  })

  it('sort=manual: dropping a card on itself is a no-op', async () => {
    await renderBoardWithSort('status', 'manual', [
      row({ id: 'a', status: 'TODO', clean_content: 'A', manual_order: 1 }),
      row({ id: 'b', status: 'TODO', clean_content: 'B', manual_order: 2 })
    ])

    const cardA = screen
      .getByRole('group', { name: 'To Do' })
      .querySelector<HTMLElement>('[data-card]')!

    await fireEvent.dragStart(cardA)
    await fireEvent.drop(cardA)
    await flush()

    expect(mocks.setTaskOrder).not.toHaveBeenCalled()
  })

  it('sort=manual: cross-column drop assigns the destination tail order (N+1)', async () => {
    await renderBoardWithSort('status', 'manual', [
      row({ id: 't1', status: 'TODO', clean_content: 'A', manual_order: 1 }),
      row({ id: 't2', status: 'TODO', clean_content: 'B', manual_order: 2 }),
      row({ id: 'd1', status: 'DOING', clean_content: 'C', manual_order: 1 })
    ])

    const todoCards = screen
      .getByRole('group', { name: 'To Do' })
      .querySelectorAll<HTMLElement>('[data-card]')
    const cardT1 = todoCards[0]
    const doingCol = screen.getByRole('group', { name: 'In Progress' })

    await fireEvent.dragStart(cardT1)
    await fireEvent.drop(doingCol)
    await flush()

    // The dimension setter (updateBlockState) fires for the cross-column
    // move, AND setTaskOrder assigns the moved card the destination's
    // tail order (1 card already in DOING → t1 gets order 2).
    expect(mocks.updateBlockState).toHaveBeenCalledWith('t1', 'DOING')
    expect(mocks.setTaskOrder).toHaveBeenCalledWith('t1', 2)
  })

  it('sort=manual: cross-column drop uses max(existing orders)+1 for gap-tolerant destinations', async () => {
    // Source column holds non-contiguous orders [1, 5] (gap-tolerant: the
    // source isn't renumbered when a card leaves). The destination has 1
    // card at order 1. A count-based destLen+1 would assign 2 — but if the
    // destination also had gaps (e.g. [1, 5]) destLen+1=3 would land mid-
    // column rather than at the tail. Verifies max+1 produces order 6.
    await renderBoardWithSort('status', 'manual', [
      row({ id: 't1', status: 'TODO', clean_content: 'A', manual_order: 1 }),
      row({
        id: 'd1',
        status: 'DOING',
        clean_content: 'B',
        manual_order: 1
      }),
      row({
        id: 'd2',
        status: 'DOING',
        clean_content: 'C',
        manual_order: 5
      })
    ])

    const todoCard = screen
      .getByRole('group', { name: 'To Do' })
      .querySelector<HTMLElement>('[data-card]')!
    const doingCol = screen.getByRole('group', { name: 'In Progress' })

    await fireEvent.dragStart(todoCard)
    await fireEvent.drop(doingCol)
    await flush()

    // max([1, 5]) + 1 = 6, not destLen(2)+1 = 3.
    expect(mocks.setTaskOrder).toHaveBeenCalledWith('t1', 6)
  })

  it('sort=manual: serializes rapid reorder IPCs so the newer write always lands last', async () => {
    // The Go side is last-writer-wins and IPC arrival order across near-
    // simultaneous calls isn't FIFO, so a stale older orderByID resolving
    // AFTER a newer reorder would clobber the user's last action. The
    // in-flight tracker makes the newer reorder await the older IPC,
    // guaranteeing the newer write lands last on disk.
    let resolveFirst!: (v: boolean) => void
    const firstPending = new Promise<boolean>((r) => (resolveFirst = r))
    mocks.setTaskOrders
      .mockReturnValueOnce(firstPending)
      .mockResolvedValueOnce(true)

    await renderBoardWithSort('status', 'manual', [
      row({ id: 'a', status: 'TODO', clean_content: 'A', manual_order: 1 }),
      row({ id: 'b', status: 'TODO', clean_content: 'B', manual_order: 2 }),
      row({ id: 'c', status: 'TODO', clean_content: 'C', manual_order: 3 })
    ])

    // First reorder (move A before C): IPC stays pending via the
    // controllable promise so the second reorder overlaps it.
    await fireEvent.dragStart(cardByText('A'))
    await fireEvent.drop(cardByText('C'))
    await tick()

    // Second reorder during the first IPC (move C before B).
    await fireEvent.dragStart(cardByText('C'))
    await fireEvent.drop(cardByText('B'))
    await tick()

    // Serialized: only the first IPC has fired — the second is parked on
    // reorderInFlight. Without the fix both would be in-flight here.
    expect(mocks.setTaskOrders).toHaveBeenCalledTimes(1)

    resolveFirst(true)
    await flush()

    // The second IPC fires now and carries fresh optimistic manual_order
    // values (the round-5 patch): [a,b,c] → [b,a,c] → [c,b,a].
    expect(mocks.setTaskOrders).toHaveBeenCalledTimes(2)
    const secondBatch = mocks.setTaskOrders.mock.calls[1][0] as {
      id: string
      order: number
    }[]
    expect(secondBatch).toEqual(
      expect.arrayContaining([
        { id: 'c', order: 1 },
        { id: 'b', order: 2 },
        { id: 'a', order: 3 }
      ])
    )
  })

  it('sort=manual: second rapid reorder receives fresh optimistic manual_order values', async () => {
    // Pins the round-5 optimistic patch (manual_order: i+1 on the reordered
    // card objects): a second reorder before the first IPC settles must read
    // the FIRST reorder's fresh orders. Without the patch, a card whose stale
    // order coincidentally equals its new position is skipped and dropped
    // from the batch, writing a colliding [order::] token to disk.
    mocks.setTaskOrders.mockResolvedValue(true)

    await renderBoardWithSort('status', 'manual', [
      row({ id: 'a', status: 'TODO', clean_content: 'A', manual_order: 1 }),
      row({ id: 'b', status: 'TODO', clean_content: 'B', manual_order: 2 }),
      row({ id: 'c', status: 'TODO', clean_content: 'C', manual_order: 3 })
    ])

    // First reorder (move A before C) → [b, a, c].
    await fireEvent.dragStart(cardByText('A'))
    await fireEvent.drop(cardByText('C'))
    await tick()

    // Second reorder before the first settles (move C before B) → [c, b, a].
    await fireEvent.dragStart(cardByText('C'))
    await fireEvent.drop(cardByText('B'))
    await flush()

    expect(mocks.setTaskOrders).toHaveBeenCalledTimes(2)
    const secondBatch = mocks.setTaskOrders.mock.calls[1][0] as {
      id: string
      order: number
    }[]
    // All three carry fresh orders. Without the round-5 patch card B
    // (stale 2 → new 2) would be skipped and absent from this batch.
    expect(secondBatch).toEqual(
      expect.arrayContaining([
        { id: 'c', order: 1 },
        { id: 'b', order: 2 },
        { id: 'a', order: 3 }
      ])
    )
  })

  // --- Smart-list activeFilter integration (#432) -----------------------

  it('activeFilter wires into buildQuery so clicking "Today" narrows the board', async () => {
    // The regression: Sidebar sets hubState.activeFilter on a smart-list
    // click but no renderer consumed it, so the click was decorative-only.
    // Verify BoardView forwards activeFilter to buildQuery by inspecting
    // the SQL the reload() fired.
    resetTaskHubState()
    setGroupBy('status')
    setActiveFilter('today')
    mocks.sqliteQuery.mockReset()
    mocks.sqliteQuery.mockResolvedValue({ rows: [], truncated: false })
    const ctx = makeCtx()
    render(BoardView, { ctx, onCountChange: vi.fn() })
    await flush()

    expect(mocks.sqliteQuery).toHaveBeenCalled()
    // Every fired query should carry the smart-list constraint — verify
    // the most recent one (the load effect may fire more than once during
    // mount as reactive deps settle).
    const lastCall = mocks.sqliteQuery.mock.calls.at(-1)!
    const sql = lastCall[0] as string
    const params = lastCall[1] as unknown[]
    // Smart-list today → status!=DONE AND due_date=today.
    expect(sql).toContain("t.status != 'DONE'")
    expect(sql).toContain('t.due_date = ?')
    expect(params).toContain(TODAY)
  })

  it('shows subtask badge and column estimate sum when present (#434/#439)', async () => {
    await renderBoard('status', [
      row({
        id: 't1',
        status: 'TODO',
        clean_content: 'Parent',
        subtask_total: 4,
        subtask_done: 2,
        estimate_minutes: 120
      }),
      row({
        id: 't2',
        status: 'TODO',
        clean_content: 'Sibling',
        estimate_minutes: 60
      })
    ])
    expect(screen.getByTestId('board-subtask-badge-t1').textContent).toContain(
      '[2/4]'
    )
    // 120 + 60 = 180m → 3h estimated on the TODO column.
    expect(
      screen.getByTestId('board-col-estimate-status-TODO').textContent
    ).toMatch(/3h estimated/)
  })

  // #458: a column-shell skeleton renders while the board is loading (replaces
  // the old bare "Loading board…" text). Keep loading=true by never resolving
  // the query, then assert the skeleton testid is present.
  it('renders a column skeleton while loading', async () => {
    resetTaskHubState()
    setGroupBy('status')
    mocks.sqliteQuery.mockReset()
    mocks.sqliteQuery.mockReturnValue(new Promise(() => {})) // never resolves
    const ctx = makeCtx()
    render(BoardView, { ctx, onCountChange: vi.fn() })
    await tick() // initial mount render (loading starts true)
    expect(screen.getByTestId('tasks-board-loading')).toBeTruthy()
  })

  // ── Soft WIP limits (#437) ───────────────────────────────────────────

  it('shows count/limit badge and over-limit styling when a column exceeds WIP', async () => {
    mocks.tasksSettings = {
      columns: [
        { name: 'TODO', wipLimit: 1 },
        { name: 'DOING' },
        { name: 'DONE' }
      ]
    }
    await initTasksSettings(makeCtx())
    await renderBoard('status', [
      row({ id: 't1', status: 'TODO', clean_content: 'A' }),
      row({ id: 't2', status: 'TODO', clean_content: 'B' })
    ])

    const badge = screen.getByTestId('board-wip-badge-status-TODO')
    expect(badge.textContent?.replace(/\s+/g, ' ').trim()).toBe('2 / 1')
    expect(badge.className).toContain('text-status-warn')
    expect(screen.getByTestId('board-wip-over-limit')).toBeInTheDocument()
    expect(
      screen.getByRole('group', { name: 'To Do' }).getAttribute('data-wip-over')
    ).toBe('true')
  })

  it('does not show WIP badge when groupBy is not status', async () => {
    mocks.tasksSettings = {
      columns: [
        { name: 'TODO', wipLimit: 1 },
        { name: 'DOING' },
        { name: 'DONE' }
      ]
    }
    await initTasksSettings(makeCtx())
    await renderBoard('owner', [
      row({ id: 't1', owner: 'Alice', clean_content: 'A' }),
      row({ id: 't2', owner: 'Alice', clean_content: 'B' })
    ])
    expect(screen.queryByTestId('board-wip-badge-status-TODO')).toBeNull()
    expect(screen.queryByTestId('board-wip-over-limit')).toBeNull()
  })

  it('prompts to confirm when a drop would exceed the WIP limit; cancel snaps back', async () => {
    mocks.tasksSettings = {
      columns: [
        { name: 'TODO' },
        { name: 'DOING', wipLimit: 1 },
        { name: 'DONE' }
      ]
    }
    await initTasksSettings(makeCtx())
    await renderBoard('status', [
      row({ id: 't1', status: 'TODO', clean_content: 'A' }),
      row({ id: 't2', status: 'DOING', clean_content: 'B' })
    ])

    const todoCard = screen
      .getByRole('group', { name: 'To Do' })
      .querySelector<HTMLElement>('[data-card]')!
    const doingCol = screen.getByRole('group', { name: 'In Progress' })

    await fireEvent.dragStart(todoCard)
    await fireEvent.drop(doingCol)
    await flush()

    // Soft limit: no persist yet; confirm dialog is open.
    expect(mocks.updateBlockState).not.toHaveBeenCalled()
    expect(screen.getByTestId('board-wip-confirm')).toBeInTheDocument()
    expect(screen.getByText(/over its WIP limit/i)).toBeInTheDocument()

    await fireEvent.click(screen.getByTestId('board-wip-confirm-cancel'))
    await flush()

    expect(mocks.updateBlockState).not.toHaveBeenCalled()
    expect(screen.queryByTestId('board-wip-confirm')).toBeNull()
    // Card snapped back to TODO.
    expect(
      screen.getByRole('group', { name: 'To Do' }).querySelector('[data-card]')
        ?.textContent
    ).toContain('A')
  })

  it('proceeds with the status change when WIP over-limit is confirmed', async () => {
    mocks.tasksSettings = {
      columns: [
        { name: 'TODO' },
        { name: 'DOING', wipLimit: 1 },
        { name: 'DONE' }
      ]
    }
    await initTasksSettings(makeCtx())
    await renderBoard('status', [
      row({ id: 't1', status: 'TODO', clean_content: 'A' }),
      row({ id: 't2', status: 'DOING', clean_content: 'B' })
    ])

    const todoCard = screen
      .getByRole('group', { name: 'To Do' })
      .querySelector<HTMLElement>('[data-card]')!
    const doingCol = screen.getByRole('group', { name: 'In Progress' })

    await fireEvent.dragStart(todoCard)
    await fireEvent.drop(doingCol)
    await flush()

    await fireEvent.click(screen.getByTestId('board-wip-confirm-confirm'))
    await flush()

    expect(mocks.updateBlockState).toHaveBeenCalledWith('t1', 'DOING')
    expect(screen.queryByTestId('board-wip-confirm')).toBeNull()
  })

  it('WIP over-limit + blocked DONE skips WIP dialog and opens blocked-DONE only', async () => {
    mocks.tasksSettings = {
      columns: [
        { name: 'TODO' },
        { name: 'DOING' },
        { name: 'DONE', wipLimit: 1 }
      ]
    }
    await initTasksSettings(makeCtx())
    mocks.getTaskBlockers.mockResolvedValue([
      { id: 'blocker-1', clean_content: 'Prerequisite' }
    ])
    await renderBoard('status', [
      row({
        id: 'tb',
        status: 'TODO',
        clean_content: 'blocked',
        is_blocked: 1
      }),
      row({ id: 'td', status: 'DONE', clean_content: 'already done' })
    ])

    const todoCard = screen
      .getByRole('group', { name: 'To Do' })
      .querySelector<HTMLElement>('[data-card]')!
    const doneCol = screen.getByRole('group', { name: 'Done' })

    await fireEvent.dragStart(todoCard)
    await fireEvent.drop(doneCol)
    await flush()

    // Single dialog: blocked-DONE, not WIP confirm first.
    expect(screen.queryByTestId('board-wip-confirm')).toBeNull()
    const dialog = await screen.findByRole('alertdialog', {
      name: 'Complete blocked task?'
    })
    expect(dialog).toBeInTheDocument()

    // Cancel reverts to TODO (use dialog-scoped Cancel — not the scrim).
    await fireEvent.click(
      within(dialog).getByRole('button', { name: 'Cancel' })
    )
    await flush()
    expect(
      screen.getByRole('group', { name: 'To Do' }).querySelector('[data-card]')
        ?.textContent
    ).toContain('blocked')
    expect(mocks.updateBlockState).not.toHaveBeenCalled()
  })

  it('prompts on quick-add into an over-WIP column; cancel does not create', async () => {
    mocks.tasksSettings = {
      columns: [
        { name: 'TODO', wipLimit: 1 },
        { name: 'DOING' },
        { name: 'DONE' }
      ]
    }
    await initTasksSettings(makeCtx())
    await renderBoard('status', [
      row({ id: 't1', status: 'TODO', clean_content: 'Already there' })
    ])

    await fireEvent.click(screen.getByTestId('board-add-status-TODO'))
    await flush()
    const input = screen.getByPlaceholderText(/Add to To Do/i)
    await fireEvent.input(input, { target: { value: 'New over limit' } })
    await fireEvent.keyDown(input, { key: 'Enter' })
    await flush()

    expect(mocks.createTask).not.toHaveBeenCalled()
    expect(screen.getByTestId('board-wip-quickadd-confirm')).toBeInTheDocument()

    await fireEvent.click(
      screen.getByTestId('board-wip-quickadd-confirm-cancel')
    )
    await flush()

    expect(mocks.createTask).not.toHaveBeenCalled()
    expect(screen.queryByTestId('board-wip-quickadd-confirm')).toBeNull()
  })

  it('creates via quick-add when over-WIP is confirmed', async () => {
    mocks.tasksSettings = {
      columns: [
        { name: 'TODO', wipLimit: 1 },
        { name: 'DOING' },
        { name: 'DONE' }
      ]
    }
    await initTasksSettings(makeCtx())
    await renderBoard('status', [
      row({ id: 't1', status: 'TODO', clean_content: 'Already there' })
    ])

    await fireEvent.click(screen.getByTestId('board-add-status-TODO'))
    await flush()
    const input = screen.getByPlaceholderText(/Add to To Do/i)
    await fireEvent.input(input, { target: { value: 'New over limit' } })
    await fireEvent.keyDown(input, { key: 'Enter' })
    await flush()

    await fireEvent.click(
      screen.getByTestId('board-wip-quickadd-confirm-confirm')
    )
    await flush()

    expect(mocks.createTask).toHaveBeenCalledWith(
      expect.objectContaining({ title: 'New over limit', status: 'TODO' })
    )
  })
})
