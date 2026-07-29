import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { PluginContext } from '../../../sdk'
import { clearTools } from '../tool-registry'
import { handleUpdateTask, updateTaskToolDef } from './update_task'

interface CtxOpts {
  /** Snapshot row for the task (omit → task not found). */
  snap?: {
    notebook: string
    section: string
    page: string
    lineNumber?: number
    recur: string | null
  }
  /** Sibling rows returned by the spawn lookup. */
  siblings?: Array<{ id: string }>
  /** The spawned id the server returns from the status transition (#812).
   *  When set, the tool must use it directly and skip the index fallback. */
  spawnedId?: string
  /** Make every setter + updateBlockState reject (capability-denied). */
  rejectAll?: boolean
  /** Reject a single named setter (partial-failure tests). */
  rejectSetter?: 'recurrence'
  /** Optional AI facet so audit-event assertions can opt in. */
  ai?: { auditEvent: ReturnType<typeof vi.fn> }
}

function makeCtx(opts: CtxOpts = {}): {
  ctx: PluginContext
  sqliteQuery: ReturnType<typeof vi.fn>
  updateBlockState: ReturnType<typeof vi.fn>
  setTaskDueDate: ReturnType<typeof vi.fn>
  setTaskOwner: ReturnType<typeof vi.fn>
  setTaskPriority: ReturnType<typeof vi.fn>
  setTaskTags: ReturnType<typeof vi.fn>
  setTaskRecurrence: ReturnType<typeof vi.fn>
  setTaskEstimate: ReturnType<typeof vi.fn>
  setTaskBlockedBy: ReturnType<typeof vi.fn>
  setTaskTitle: ReturnType<typeof vi.fn>
  mutateBlock: ReturnType<typeof vi.fn>
} {
  const useDeny = !!opts.rejectAll
  // Each setter gets its own mock instance so per-setter call assertions work.
  const mk = () =>
    useDeny
      ? vi.fn(async () => {
          throw new Error('capability denied')
        })
      : vi.fn(async () => true)
  const reject = (name: string) =>
    vi.fn(async () => {
      throw new Error(`${name} failed`)
    })
  const sqliteQuery = vi.fn(async (sql: string) => {
    // readSnapshot query projects notebook/section/page + recur by block id.
    if (sql.includes('SELECT b.notebook')) {
      if (!opts.snap) return { rows: [], truncated: false }
      return {
        rows: [
          {
            notebook: opts.snap.notebook,
            section: opts.snap.section,
            page: opts.snap.page,
            line_number: opts.snap.lineNumber ?? 10,
            recur: opts.snap.recur
          }
        ] as unknown as Record<string, unknown>[],
        truncated: false
      }
    }
    // Spawn-lookup query projects b.id, ordered by line_number.
    return {
      rows: (opts.siblings ?? []).slice() as unknown as Record<
        string,
        unknown
      >[],
      truncated: false
    }
  })
  // updateBlockState returns the new {ok, spawnedId} struct (#812); the other
  // setters still resolve to a boolean.
  const updateBlockState = useDeny
    ? vi.fn(async () => {
        throw new Error('capability denied')
      })
    : vi.fn(async () => ({ ok: true, spawnedId: opts.spawnedId ?? '' }))
  const setTaskDueDate = mk()
  const setTaskOwner = mk()
  const setTaskPriority = mk()
  const setTaskTags = mk()
  const setTaskRecurrence =
    opts.rejectSetter === 'recurrence' ? reject('recurrence') : mk()
  const setTaskEstimate = mk()
  const setTaskBlockedBy = mk()
  const setTaskTitle = mk()
  const mutateBlock = vi.fn(async () => true)
  const ctx = {
    sqliteQuery,
    updateBlockState,
    setTaskDueDate,
    setTaskOwner,
    setTaskPriority,
    setTaskTags,
    setTaskRecurrence,
    setTaskEstimate,
    setTaskBlockedBy,
    setTaskTitle,
    mutateBlock,
    ...(opts.ai ? { ai: opts.ai } : {})
  } as unknown as PluginContext
  return {
    ctx,
    sqliteQuery,
    updateBlockState,
    setTaskDueDate,
    setTaskOwner,
    setTaskPriority,
    setTaskTags,
    setTaskRecurrence,
    setTaskEstimate,
    setTaskBlockedBy,
    setTaskTitle,
    mutateBlock
  }
}

beforeEach(() => clearTools())
afterEach(() => clearTools())

const SNAP = {
  notebook: 'Work',
  section: 'Sprint',
  page: 'Plan',
  recur: null as string | null
}

describe('update_task', () => {
  it('updates multiple metadata fields via the matching setters', async () => {
    const c = makeCtx({ snap: { ...SNAP } })
    const res = await handleUpdateTask(c.ctx, {
      task_id: 't1',
      due: '2026-08-01',
      priority: 2,
      owner: 'maya',
      tags: ['work', 'urgent']
    })
    expect(res.error).toBeUndefined()
    expect(c.setTaskDueDate).toHaveBeenCalledWith('t1', '2026-08-01')
    expect(c.setTaskPriority).toHaveBeenCalledWith('t1', 2)
    expect(c.setTaskOwner).toHaveBeenCalledWith('t1', 'maya')
    expect(c.setTaskTags).toHaveBeenCalledWith('t1', ['work', 'urgent'])
  })

  it('partial update touches only the supplied field', async () => {
    const c = makeCtx({ snap: { ...SNAP } })
    await handleUpdateTask(c.ctx, { task_id: 't1', due: '2026-08-01' })
    expect(c.setTaskDueDate).toHaveBeenCalledWith('t1', '2026-08-01')
    expect(c.setTaskOwner).not.toHaveBeenCalled()
    expect(c.setTaskPriority).not.toHaveBeenCalled()
    expect(c.setTaskTags).not.toHaveBeenCalled()
    expect(c.updateBlockState).not.toHaveBeenCalled()
  })

  it('clears a field on an explicit empty value', async () => {
    const c = makeCtx({ snap: { ...SNAP } })
    await handleUpdateTask(c.ctx, { task_id: 't1', due: '', tags: [] })
    expect(c.setTaskDueDate).toHaveBeenCalledWith('t1', '')
    expect(c.setTaskTags).toHaveBeenCalledWith('t1', [])
  })

  it('clears an array field on null (description contract: null clears)', async () => {
    const c = makeCtx({ snap: { ...SNAP } })
    await handleUpdateTask(c.ctx, {
      task_id: 't1',
      tags: null,
      blocked_by: null
    })
    expect(c.setTaskTags).toHaveBeenCalledWith('t1', [])
    expect(c.setTaskBlockedBy).toHaveBeenCalledWith('t1', [])
  })

  it('drops a tag that becomes empty after the #-strip', async () => {
    // '###' survives the leading-empty filter but strips to '' — without the
    // trailing filter this would write a malformed empty tag to the prose.
    const c = makeCtx({ snap: { ...SNAP } })
    await handleUpdateTask(c.ctx, { task_id: 't1', tags: ['###', 'work'] })
    expect(c.setTaskTags).toHaveBeenCalledWith('t1', ['work'])
  })

  it('rejects an impossible calendar date without writing', async () => {
    const c = makeCtx({ snap: { ...SNAP } })
    const res = await handleUpdateTask(c.ctx, {
      task_id: 't1',
      due: '2026-13-40'
    })
    expect(res.error).toMatch(/due/)
    expect(c.setTaskDueDate).not.toHaveBeenCalled()
  })

  it('transitions status via updateBlockState', async () => {
    const c = makeCtx({ snap: { ...SNAP, recur: null } })
    const res = await handleUpdateTask(c.ctx, { task_id: 't1', status: 'DONE' })
    expect(res.error).toBeUndefined()
    expect(c.updateBlockState).toHaveBeenCalledWith('t1', 'DONE')
    // No recurrence ⇒ no spawn note.
    expect(res.content).not.toMatch(/spawn/i)
  })

  it('uses the server-returned spawned id directly (no index lookup)', async () => {
    // #812: the status transition returns the spawned id from the atomic Go
    // write, so the tool must report it directly and skip the heuristic query.
    const c = makeCtx({
      snap: { ...SNAP, recur: 'every week', lineNumber: 10 },
      spawnedId: 'server-spawn',
      siblings: [{ id: 'should-not-be-used' }]
    })
    const res = await handleUpdateTask(c.ctx, { task_id: 't1', status: 'DONE' })
    expect(c.updateBlockState).toHaveBeenCalledWith('t1', 'DONE')
    expect(res.content).toContain('server-spawn')
    expect(res.content).not.toContain('should-not-be-used')
    // Only the readSnapshot query ran — the index fallback was skipped.
    expect(c.sqliteQuery).toHaveBeenCalledTimes(1)
  })

  it('reports the spawned instance via the index fallback when the server omits the id', async () => {
    // Defense-in-depth (#812): when the server returns no spawned id, fall back
    // to the index heuristic to locate the sibling spliced below the line.
    const c = makeCtx({
      snap: { ...SNAP, recur: 'every week', lineNumber: 10 },
      siblings: [{ id: 'spawned-1' }]
    })
    const res = await handleUpdateTask(c.ctx, { task_id: 't1', status: 'DONE' })
    expect(c.updateBlockState).toHaveBeenCalledWith('t1', 'DONE')
    expect(res.content).toContain('spawned-1')
    expect(res.content).toMatch(/every week/)
    // The spawn lookup is the second sqliteQuery call; it must localize to the
    // line directly below the completed task (D1 fix), not the first recurring
    // TODO anywhere in the file.
    const spawnCall = c.sqliteQuery.mock.calls[1]
    expect(spawnCall[0]).toMatch(/line_number >/)
    const spawnParams = spawnCall[1] as unknown[]
    expect(spawnParams[3]).toBe(10) // completed task's line number
    expect(spawnParams[4]).toBe('t1') // excludes the completed task itself
  })

  it('falls back to a textual spawn note (with page path) when not found', async () => {
    const c = makeCtx({
      snap: { ...SNAP, recur: 'every week' },
      siblings: []
    })
    const res = await handleUpdateTask(c.ctx, { task_id: 't1', status: 'DONE' })
    expect(res.content).toMatch(/spawn/i)
    expect(res.content).toMatch(/query_tasks/)
    expect(res.content).toContain('Work/Sprint/Plan')
    expect(res.content).not.toContain('spawned-1')
  })

  it('gates the spawn note on a just-set recurrence (not a failed one)', async () => {
    // recurrence set succeeds on a task with no prior rule → spawn note cites
    // the just-set rule.
    const ok = makeCtx({
      snap: { ...SNAP, recur: null, lineNumber: 10 },
      siblings: [{ id: 'spawned-2' }]
    })
    const res = await handleUpdateTask(ok.ctx, {
      task_id: 't1',
      recurrence: 'every month',
      status: 'DONE'
    })
    expect(res.error).toBeUndefined()
    expect(res.content).toContain('every month')
    expect(res.content).toContain('spawned-2')

    // recurrence set FAILS on a task with a prior 'every week' rule → spawn
    // note must cite the on-disk rule (every week), not the rejected value.
    const fail = makeCtx({
      snap: { ...SNAP, recur: 'every week', lineNumber: 10 },
      siblings: [{ id: 'spawned-3' }],
      rejectSetter: 'recurrence'
    })
    const failRes = await handleUpdateTask(fail.ctx, {
      task_id: 't1',
      recurrence: 'every month',
      status: 'DONE'
    })
    expect(failRes.error).toMatch(/recurrence/)
    expect(failRes.error).toContain('every week')
    expect(failRes.error).not.toMatch(/every month/)
  })

  it('returns a not-found error for an unknown task id', async () => {
    const c = makeCtx({ snap: undefined })
    const res = await handleUpdateTask(c.ctx, {
      task_id: 'nope',
      due: '2026-08-01'
    })
    expect(res.error).toMatch(/not found/)
    expect(c.setTaskDueDate).not.toHaveBeenCalled()
    expect(c.updateBlockState).not.toHaveBeenCalled()
  })

  it('never touches prose (mutateBlock is not called)', async () => {
    const c = makeCtx({ snap: { ...SNAP } })
    await handleUpdateTask(c.ctx, {
      task_id: 't1',
      due: '2026-08-01',
      status: 'DOING',
      owner: 'x'
    })
    expect(c.mutateBlock).not.toHaveBeenCalled()
  })

  it('rejects an invalid status without writing', async () => {
    const c = makeCtx({ snap: { ...SNAP } })
    const res = await handleUpdateTask(c.ctx, { task_id: 't1', status: 'WOOF' })
    expect(res.error).toMatch(/status/)
    expect(c.updateBlockState).not.toHaveBeenCalled()
  })

  it('rejects an out-of-range priority without writing', async () => {
    const c = makeCtx({ snap: { ...SNAP } })
    const res = await handleUpdateTask(c.ctx, { task_id: 't1', priority: 9 })
    expect(res.error).toMatch(/priority/)
    expect(c.setTaskPriority).not.toHaveBeenCalled()
  })

  it('rejects an empty title without writing', async () => {
    const c = makeCtx({ snap: { ...SNAP } })
    const res = await handleUpdateTask(c.ctx, { task_id: 't1', title: '   ' })
    expect(res.error).toMatch(/title/)
    expect(c.setTaskTitle).not.toHaveBeenCalled()
  })

  it('treats title:null as skip (not an empty-string error)', async () => {
    // title can't be cleared, so null means "no change" — consistent with how
    // every other nullable field treats null. Only an explicit empty/whitespace
    // string is an error.
    const c = makeCtx({ snap: { ...SNAP } })
    const res = await handleUpdateTask(c.ctx, {
      task_id: 't1',
      title: null,
      due: '2026-08-01'
    })
    expect(res.error).toBeUndefined()
    expect(c.setTaskTitle).not.toHaveBeenCalled()
    expect(c.setTaskDueDate).toHaveBeenCalledWith('t1', '2026-08-01')
  })

  it('trims surrounding whitespace from title before writing', async () => {
    // Mirrors create_task's asString(text).trim() — model-supplied padding
    // must not leak into the prose title.
    const c = makeCtx({ snap: { ...SNAP } })
    await handleUpdateTask(c.ctx, { task_id: 't1', title: '  Water plants  ' })
    expect(c.setTaskTitle).toHaveBeenCalledWith('t1', 'Water plants')
  })

  it('errors when no fields are supplied', async () => {
    const c = makeCtx({ snap: { ...SNAP } })
    const res = await handleUpdateTask(c.ctx, { task_id: 't1' })
    expect(res.error).toMatch(/no fields/i)
  })

  it('returns a permission error and mutates nothing when capability-denied', async () => {
    const c = makeCtx({ snap: { ...SNAP }, rejectAll: true })
    const res = await handleUpdateTask(c.ctx, {
      task_id: 't1',
      due: '2026-08-01',
      status: 'DOING'
    })
    expect(res.error).toMatch(/capability denied/)
    // Every write attempt rejected; read-only snapshot still ran.
    expect(c.sqliteQuery).toHaveBeenCalled()
  })

  it('emits a tool_result audit event on success', async () => {
    const auditEvent = vi.fn(async (_payload: unknown) => {})
    const c = makeCtx({ snap: { ...SNAP }, ai: { auditEvent } })
    const res = await handleUpdateTask(c.ctx, {
      task_id: 't1',
      due: '2026-08-01'
    })
    expect(res.error).toBeUndefined()
    expect(auditEvent).toHaveBeenCalledWith(
      expect.objectContaining({
        kind: 'tool_result',
        tool: 'update_task',
        status: 'ok'
      })
    )
    expect(auditEvent.mock.calls[0][0]).toMatchObject({ block_id: 't1' })
  })

  it('emits a tool_result audit event with status error on invalid args', async () => {
    const auditEvent = vi.fn(async (_payload: unknown) => {})
    const c = makeCtx({ snap: { ...SNAP }, ai: { auditEvent } })
    const res = await handleUpdateTask(c.ctx, { task_id: 't1', status: 'WOOF' })
    expect(res.error).toMatch(/status/)
    expect(auditEvent).toHaveBeenCalledWith(
      expect.objectContaining({
        kind: 'tool_result',
        tool: 'update_task',
        status: 'error'
      })
    )
  })

  it('exposes the tool def shape', () => {
    expect(updateTaskToolDef.name).toBe('update_task')
    expect(updateTaskToolDef.parameters.required).toEqual(['task_id'])
  })
})
