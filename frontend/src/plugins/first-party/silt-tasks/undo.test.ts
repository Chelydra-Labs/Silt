import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { PluginContext, TaskUndoResult, TaskUndoState } from '../../sdk'
import {
  beginTaskUndoWrite,
  clearTasksUndoError,
  getTasksUndoView,
  initTasksUndo,
  refreshTasksUndo,
  resetTasksUndo,
  runTaskUndo,
  tasksUndoAvailable,
  trackTaskUndoWrite
} from './undo.svelte'
import {
  describeChangedFields,
  undoButtonLabel,
  undoPageLabel,
  undoRemainingClause,
  undoResultAnnouncement
} from './undoCopy'

function undoResult(overrides: Partial<TaskUndoResult> = {}): TaskUndoResult {
  return {
    undone: true,
    skipped: false,
    blockIds: ['b1'],
    skippedBlockIds: [],
    notebook: 'Work',
    section: 'Journal',
    page: 'Daily',
    removedSpawnedId: '',
    changedFields: ['status'],
    remaining: 0,
    ...overrides
  }
}

function undoState(overrides: Partial<TaskUndoState> = {}): TaskUndoState {
  return {
    canUndo: true,
    remaining: 2,
    changedFields: ['status'],
    blockIds: ['b1'],
    notebook: 'Work',
    section: 'Journal',
    page: 'Daily',
    source: 'vault',
    ...overrides
  }
}

function makeCtx(overrides: Partial<PluginContext> = {}) {
  const getTaskUndoState = vi.fn().mockResolvedValue(undoState())
  const undoLastTaskMutation = vi.fn().mockResolvedValue(undoResult())
  const blockChanged: Array<() => void> = []
  const ctx = {
    getTaskUndoState,
    undoLastTaskMutation,
    on: vi.fn((event: string, cb: () => void) => {
      if (event === 'block:changed') blockChanged.push(cb)
      return () => {
        const i = blockChanged.indexOf(cb)
        if (i >= 0) blockChanged.splice(i, 1)
      }
    }),
    ...overrides
  } as unknown as PluginContext
  return { ctx, getTaskUndoState, undoLastTaskMutation, blockChanged }
}

async function flush() {
  await Promise.resolve()
  await Promise.resolve()
}

describe('undo store (#1011)', () => {
  beforeEach(() => {
    resetTasksUndo()
  })

  it('mirrors the journal peek into the affordance state', async () => {
    const { ctx } = makeCtx()
    initTasksUndo(ctx)
    await flush()

    const view = getTasksUndoView()
    expect(view.canUndo).toBe(true)
    expect(view.available).toBe(true)
    expect(view.remaining).toBe(2)
    expect(view.pageLabel).toBe('Work › Journal › Daily')
    expect(tasksUndoAvailable()).toBe(true)
  })

  it('refreshes the peek on block:changed', async () => {
    const { ctx, getTaskUndoState, blockChanged } = makeCtx()
    initTasksUndo(ctx)
    await flush()
    getTaskUndoState.mockClear()
    getTaskUndoState.mockResolvedValue(
      undoState({ changedFields: ['dueDate'], remaining: 1 })
    )

    vi.useFakeTimers()
    try {
      blockChanged[0]()
      vi.advanceTimersByTime(100)
      await flush()
    } finally {
      vi.useRealTimers()
    }

    expect(getTaskUndoState).toHaveBeenCalledTimes(1)
    expect(getTasksUndoView().changedFields).toEqual(['dueDate'])
    expect(getTasksUndoView().remaining).toBe(1)
  })

  it('undoes through the ctx binding and announces what was restored', async () => {
    const { ctx, undoLastTaskMutation, getTaskUndoState } = makeCtx()
    initTasksUndo(ctx)
    await flush()
    getTaskUndoState.mockResolvedValue(
      undoState({ remaining: 0, canUndo: false })
    )
    undoLastTaskMutation.mockResolvedValue(
      undoResult({ changedFields: ['status', 'completed'], remaining: 0 })
    )

    const result = await runTaskUndo()

    expect(undoLastTaskMutation).toHaveBeenCalledTimes(1)
    expect(result?.undone).toBe(true)
    expect(getTasksUndoView().announcement).toBe(
      'Undid status and completion changes in Work › Journal › Daily. No more changes to undo.'
    )
    expect(getTasksUndoView().available).toBe(false)
    expect(getTasksUndoView().error).toBe('')
  })

  it('communicates remaining depth after a multi-entry undo', async () => {
    const { ctx, undoLastTaskMutation, getTaskUndoState } = makeCtx()
    initTasksUndo(ctx)
    await flush()
    getTaskUndoState.mockResolvedValue(undoState({ remaining: 3 }))
    undoLastTaskMutation.mockResolvedValue(undoResult({ remaining: 3 }))

    await runTaskUndo()

    expect(getTasksUndoView().announcement).toBe(
      'Undid status change in Work › Journal › Daily. 3 changes left.'
    )
    expect(getTasksUndoView().available).toBe(true)
  })

  it('reports a skipped entry without claiming the journal is empty', async () => {
    const { ctx, undoLastTaskMutation, getTaskUndoState } = makeCtx()
    initTasksUndo(ctx)
    await flush()
    getTaskUndoState.mockResolvedValue(undoState({ remaining: 1 }))
    undoLastTaskMutation.mockResolvedValue(
      undoResult({
        undone: false,
        skipped: true,
        skippedBlockIds: ['gone'],
        changedFields: [],
        remaining: 1
      })
    )

    await runTaskUndo()

    expect(getTasksUndoView().announcement).toBe(
      "Undo skipped — that change couldn't be restored and was dropped from the undo history. 1 change left."
    )
    expect(getTasksUndoView().available).toBe(true)
  })

  it('disables undo while a tracked write is in flight', async () => {
    const { ctx, undoLastTaskMutation } = makeCtx()
    initTasksUndo(ctx)
    await flush()

    let settle: (v: boolean) => void = () => {}
    const write = trackTaskUndoWrite(
      new Promise<boolean>((resolve) => {
        settle = resolve
      })
    )
    await flush()
    expect(getTasksUndoView().available).toBe(false)
    await expect(runTaskUndo()).resolves.toBeNull()
    expect(undoLastTaskMutation).not.toHaveBeenCalled()

    settle(true)
    await write
    await flush()
    expect(getTasksUndoView().available).toBe(true)
  })

  it('re-enables undo only after the last overlapping write releases', async () => {
    const { ctx } = makeCtx()
    initTasksUndo(ctx)
    await flush()

    const releaseOne = beginTaskUndoWrite()
    const releaseTwo = beginTaskUndoWrite()
    expect(getTasksUndoView().available).toBe(false)
    releaseOne()
    expect(getTasksUndoView().available).toBe(false)
    releaseTwo()
    expect(getTasksUndoView().available).toBe(true)
  })

  it('surfaces a rejected undo and clears the success announcement', async () => {
    const { ctx, undoLastTaskMutation } = makeCtx()
    initTasksUndo(ctx)
    await flush()
    undoLastTaskMutation.mockRejectedValue(new Error('focus lock held'))

    const result = await runTaskUndo()

    expect(result).toBeNull()
    expect(getTasksUndoView().error).toBe("Couldn't undo: focus lock held")
    expect(getTasksUndoView().announcement).toBe('')
    clearTasksUndoError()
    expect(getTasksUndoView().error).toBe('')
  })

  it('stays disabled with an empty journal', async () => {
    const { ctx, getTaskUndoState } = makeCtx()
    getTaskUndoState.mockResolvedValue(
      undoState({ canUndo: false, remaining: 0, changedFields: [] })
    )
    initTasksUndo(ctx)
    await flush()

    expect(getTasksUndoView().available).toBe(false)
    expect(
      undoButtonLabel({ ...getTasksUndoView(), writePending: false })
    ).toBe('Nothing to undo')
  })

  it('drops a stale peek that resolves after a newer one', async () => {
    const { ctx, getTaskUndoState } = makeCtx()
    let resolveSlow: (state: TaskUndoState) => void = () => {}
    // initTasksUndo fires peek A; hold it open so peek B can land first.
    getTaskUndoState
      .mockImplementationOnce(
        () => new Promise<TaskUndoState>((resolve) => (resolveSlow = resolve))
      )
      .mockResolvedValueOnce(undoState({ remaining: 1, changedFields: [] }))

    initTasksUndo(ctx)
    await flush()
    await refreshTasksUndo()
    await flush()
    expect(getTasksUndoView().remaining).toBe(1)

    // The journal moved on after B; A's late answer is history now.
    resolveSlow(undoState({ remaining: 5, changedFields: ['status'] }))
    await flush()

    expect(getTasksUndoView().remaining).toBe(1)
    expect(getTasksUndoView().changedFields).toEqual([])
  })

  it('reset drops the bound context and the mirror', async () => {
    const { ctx } = makeCtx()
    initTasksUndo(ctx)
    await flush()
    expect(tasksUndoAvailable()).toBe(true)

    resetTasksUndo()
    expect(tasksUndoAvailable()).toBe(false)
    expect(getTasksUndoView().remaining).toBe(0)
  })

  it('keeps the last known state when a peek fails', async () => {
    const { ctx, getTaskUndoState } = makeCtx()
    initTasksUndo(ctx)
    await flush()
    getTaskUndoState.mockRejectedValue(new Error('ipc down'))

    await refreshTasksUndo()

    expect(getTasksUndoView().canUndo).toBe(true)
    expect(getTasksUndoView().remaining).toBe(2)
  })
})

describe('undo copy', () => {
  it('names one or many restored attributes', () => {
    expect(describeChangedFields([])).toBe('change')
    expect(describeChangedFields(['status'])).toBe('status change')
    expect(describeChangedFields(['status', 'completed'])).toBe(
      'status and completion changes'
    )
    expect(describeChangedFields(['status', 'status'])).toBe('status change')
  })

  it('keeps the synthetic standalone notebook out of the locator', () => {
    expect(undoPageLabel('.silt', '', 'tasks')).toBe('your standalone tasks')
    expect(undoPageLabel('Work', 'Journal', 'Daily')).toBe(
      'Work › Journal › Daily'
    )
    expect(undoPageLabel('', '', '')).toBe('your vault')
  })

  it('spells out the remaining depth', () => {
    expect(undoRemainingClause(0)).toBe('No more changes to undo.')
    expect(undoRemainingClause(1)).toBe('1 change left.')
    expect(undoRemainingClause(4)).toBe('4 changes left.')
  })

  it('describes the journal head and the disabled reasons', () => {
    const base = {
      canUndo: true,
      pending: false,
      writePending: false,
      remaining: 3,
      changedFields: ['order'],
      pageLabel: 'Work › Journal › Daily'
    } as const
    expect(undoButtonLabel(base)).toBe(
      'Undo order change in Work › Journal › Daily — 3 changes left'
    )
    expect(undoButtonLabel({ ...base, pending: true })).toBe(
      'Undoing last change…'
    )
    expect(undoButtonLabel({ ...base, writePending: true })).toBe(
      'Undo unavailable while a change is saving'
    )
    expect(undoButtonLabel({ ...base, canUndo: false })).toBe('Nothing to undo')
  })

  it('announces a clean empty-journal no-op', () => {
    expect(undoResultAnnouncement(undoResult({ undone: false }))).toBe(
      'Nothing to undo.'
    )
  })
})
