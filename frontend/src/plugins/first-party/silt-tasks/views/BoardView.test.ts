import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest'
import { tick } from 'svelte'
import { render, screen, cleanup, fireEvent } from '@testing-library/svelte'

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

// Hoisted mutable mock state — the settings store is mocked so loadColumns()
// reads from the synchronous snapshot, and every SDK setter is a spy so the
// dimension-aware DnD dispatcher can be asserted per-dimension.
const mocks = vi.hoisted(() => ({
  settings: {
    config: {
      plugins: {
        active: [],
        disabled: [],
        plugin_settings: {} as Record<string, Record<string, unknown>>
      }
    },
    error: ''
  },
  sqliteQuery: vi.fn(),
  updateBlockState: vi.fn(),
  setTaskOwner: vi.fn(),
  setTaskPriority: vi.fn(),
  setTaskDueDate: vi.fn(),
  setTaskTags: vi.fn(),
  setTaskOrder: vi.fn(),
  createTask: vi.fn().mockResolvedValue('new-task-id'),
  getTaskBlockers: vi.fn().mockResolvedValue([]),
  updatePluginSetting: vi.fn().mockResolvedValue(true),
  notify: vi.fn().mockResolvedValue(true)
}))

vi.mock('../../../../settings/store.svelte', () => ({
  settings: mocks.settings,
  updatePluginSetting: mocks.updatePluginSetting
}))

vi.mock('../../../../wailsjs/runtime/runtime.js', () => ({
  EventsOn: vi.fn(() => () => {})
}))

import BoardView from './BoardView.svelte'
import type { PluginContext } from '../../../sdk'
import { v2CtxStubs } from '../../../test-helpers'
import {
  getTaskHubState,
  resetTaskHubState,
  setGroupBy,
  setDisplayMode,
  setSort
} from '../state.svelte'

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
    createTask: mocks.createTask,
    getTaskBlockers: mocks.getTaskBlockers,
    getPluginSettings: vi.fn(() => Promise.resolve({})),
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
    tags: '',
    is_blocked: 0,
    ...p
  }
}

async function flush() {
  await tick()
  await new Promise((r) => setTimeout(r, 0))
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
  beforeEach(() => {
    mocks.settings.config.plugins.plugin_settings = {}
    mocks.updateBlockState.mockReset().mockResolvedValue(true)
    mocks.setTaskOwner.mockReset().mockResolvedValue(true)
    mocks.setTaskPriority.mockReset().mockResolvedValue(true)
    mocks.setTaskDueDate.mockReset().mockResolvedValue(true)
    mocks.setTaskTags.mockReset().mockResolvedValue(true)
    mocks.setTaskOrder.mockReset().mockResolvedValue(true)
    mocks.createTask.mockReset().mockResolvedValue('new-task-id')
    mocks.getTaskBlockers.mockReset().mockResolvedValue([])
    mocks.updatePluginSetting.mockReset().mockResolvedValue(true)
    mocks.notify.mockReset().mockResolvedValue(true)
  })

  afterEach(() => {
    cleanup()
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
      'silt-tasks',
      'columns',
      expect.arrayContaining(['Backlog'])
    )
    promptSpy.mockRestore()
  })

  it('remove column confirms and drops the lane', async () => {
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(true)
    await renderBoard('status', [])

    const menus = screen.getAllByRole('button', { name: 'Column actions' })
    await fireEvent.click(menus[0]!)
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

  it('sort=manual: dropping a card onto a sibling in the SAME column renumbers via setTaskOrder', async () => {
    await renderBoardWithSort('status', 'manual', [
      row({ id: 'a', status: 'TODO', clean_content: 'A', manual_order: 1 }),
      row({ id: 'b', status: 'TODO', clean_content: 'B', manual_order: 2 }),
      row({ id: 'c', status: 'TODO', clean_content: 'C', manual_order: 3 })
    ])

    const cards = screen
      .getByRole('group', { name: 'To Do' })
      .querySelectorAll<HTMLElement>('[data-card]')
    const cardA = cards[0]!
    const cardC = cards[2]!

    await fireEvent.dragStart(cardA)
    await fireEvent.drop(cardC)
    await flush()

    // Splice-dance "land BEFORE target": [a,b,c] → remove a → [b,c] →
    // insert a before c (idx 1) → [b, a, c] → new orders b=1 (was 2),
    // a=2 (was 1), c=3 (unchanged). The two changed rows are persisted.
    expect(mocks.setTaskOrder).toHaveBeenCalledTimes(2)
    expect(mocks.setTaskOrder).toHaveBeenCalledWith('b', 1)
    expect(mocks.setTaskOrder).toHaveBeenCalledWith('a', 2)
    // The unchanged row (c) is not persisted.
    expect(mocks.setTaskOrder.mock.calls.some(([id]) => id === 'c')).toBe(false)
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
    const cardT1 = todoCards[0]!
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
})
