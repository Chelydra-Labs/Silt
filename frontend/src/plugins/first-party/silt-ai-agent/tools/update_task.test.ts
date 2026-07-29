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
    recur: string | null
  }
  /** Sibling rows returned by the spawn lookup. */
  siblings?: Array<{ id: string }>
  /** Make every setter + updateBlockState reject (capability-denied). */
  rejectAll?: boolean
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
  const updateBlockState = mk()
  const setTaskDueDate = mk()
  const setTaskOwner = mk()
  const setTaskPriority = mk()
  const setTaskTags = mk()
  const setTaskRecurrence = mk()
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
    mutateBlock
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

  it('transitions status via updateBlockState', async () => {
    const c = makeCtx({ snap: { ...SNAP, recur: null } })
    const res = await handleUpdateTask(c.ctx, { task_id: 't1', status: 'DONE' })
    expect(res.error).toBeUndefined()
    expect(c.updateBlockState).toHaveBeenCalledWith('t1', 'DONE')
    // No recurrence ⇒ no spawn note.
    expect(res.content).not.toMatch(/spawn/i)
  })

  it('reports the spawned instance on a recurring DONE', async () => {
    const c = makeCtx({
      snap: { ...SNAP, recur: 'every week' },
      siblings: [{ id: 'spawned-1' }, { id: 't1' }]
    })
    const res = await handleUpdateTask(c.ctx, { task_id: 't1', status: 'DONE' })
    expect(c.updateBlockState).toHaveBeenCalledWith('t1', 'DONE')
    expect(res.content).toContain('spawned-1')
    expect(res.content).toMatch(/every week/)
  })

  it('falls back to a textual spawn note when the sibling is not found', async () => {
    const c = makeCtx({
      snap: { ...SNAP, recur: 'every week' },
      siblings: []
    })
    const res = await handleUpdateTask(c.ctx, { task_id: 't1', status: 'DONE' })
    expect(res.content).toMatch(/spawn/i)
    expect(res.content).toMatch(/query_tasks/)
    expect(res.content).not.toContain('spawned-1')
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

  it('exposes the tool def shape', () => {
    expect(updateTaskToolDef.name).toBe('update_task')
    expect(updateTaskToolDef.parameters.required).toEqual(['task_id'])
  })
})
