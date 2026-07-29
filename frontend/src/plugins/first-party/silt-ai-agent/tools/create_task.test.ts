import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { PluginContext } from '../../../sdk'
import { clearTools } from '../tool-registry'
import { createTaskToolDef, handleCreateTask } from './create_task'

interface CtxOpts {
  activeNotebook?: string
  blockId?: string
  /** Override a specific setter to reject (partial-failure test). */
  rejectSetter?: 'due' | 'owner' | 'priority' | 'tags'
  /** Make createTask / createBlock reject (capability-denied test). */
  rejectCreate?: boolean
}

function makeCtx(opts: CtxOpts = {}): {
  ctx: PluginContext
  createTask: ReturnType<typeof vi.fn>
  createBlock: ReturnType<typeof vi.fn>
  createPage: ReturnType<typeof vi.fn>
  setTaskDueDate: ReturnType<typeof vi.fn>
  setTaskOwner: ReturnType<typeof vi.fn>
  setTaskPriority: ReturnType<typeof vi.fn>
  setTaskTags: ReturnType<typeof vi.fn>
} {
  const ok = vi.fn(async () => true)
  const reject = (name: string) =>
    vi.fn(async () => {
      throw new Error(`${name} denied`)
    })
  const createTask = opts.rejectCreate
    ? reject('createTask')
    : vi.fn(async () => opts.blockId ?? 'task-uuid')
  const createBlock = opts.rejectCreate
    ? reject('createBlock')
    : vi.fn(async () => opts.blockId ?? 'task-uuid')
  const createPage = vi.fn(async () => 'page-uuid')
  const mk = (name: 'due' | 'owner' | 'priority' | 'tags') =>
    opts.rejectSetter === name ? reject(name) : ok
  const ctx = {
    activeNotebook: opts.activeNotebook ?? '',
    createTask,
    createBlock,
    createPage,
    setTaskDueDate: mk('due'),
    setTaskOwner: mk('owner'),
    setTaskPriority: mk('priority'),
    setTaskTags: mk('tags')
  } as unknown as PluginContext
  return {
    ctx,
    createTask,
    createBlock,
    createPage,
    setTaskDueDate: ctx.setTaskDueDate as ReturnType<typeof vi.fn>,
    setTaskOwner: ctx.setTaskOwner as ReturnType<typeof vi.fn>,
    setTaskPriority: ctx.setTaskPriority as ReturnType<typeof vi.fn>,
    setTaskTags: ctx.setTaskTags as ReturnType<typeof vi.fn>
  }
}

beforeEach(() => clearTools())
afterEach(() => clearTools())

describe('create_task', () => {
  it('creates a standalone task via ctx.createTask and returns the id', async () => {
    const { ctx, createTask, createBlock } = makeCtx({ blockId: 'blk-1' })
    const res = await handleCreateTask(ctx, { text: 'draft proposal' })
    expect(res.error).toBeUndefined()
    expect(createTask).toHaveBeenCalledWith({ title: 'draft proposal' })
    // Standalone path never touches createBlock.
    expect(createBlock).not.toHaveBeenCalled()
    expect(res.content).toContain('blk-1')
    expect(res.content).toContain('standalone tasks list')
  })

  it('creates the page (idempotent) before appending a page-scoped task', async () => {
    const { ctx, createPage, createBlock, createTask } = makeCtx({
      blockId: 'blk-2'
    })
    const res = await handleCreateTask(ctx, {
      text: 'ship feature',
      notebook: 'Work',
      section: 'Sprint 42',
      page: 'Plan',
      after: 'anchor-id'
    })
    expect(res.error).toBeUndefined()
    // SaveFileBlocks fails closed on a missing page file (#691), so the page
    // must exist before createBlock — createPage is the idempotent ensure.
    expect(createPage).toHaveBeenCalledWith('Work', 'Sprint 42', 'Plan')
    expect(createPage.mock.invocationCallOrder[0]).toBeLessThan(
      createBlock.mock.invocationCallOrder[0]
    )
    expect(createBlock).toHaveBeenCalledWith({
      type: 'TASK',
      text: 'ship feature',
      notebook: 'Work',
      section: 'Sprint 42',
      page: 'Plan',
      after: 'anchor-id'
    })
    expect(createTask).not.toHaveBeenCalled()
    expect(res.content).toContain('Work/Sprint 42/Plan')
  })

  it('errors when page-scoped anchors arrive without a page', async () => {
    // notebook/section/after are meaningless on the standalone path; failing
    // loudly lets the model self-correct instead of silently misplacing the task.
    const standalone = makeCtx({ blockId: 'blk-x' })
    const res = await handleCreateTask(standalone.ctx, {
      text: 't',
      after: 'anchor-id'
    })
    expect(res.error).toMatch(/require a page/)
    expect(standalone.createTask).not.toHaveBeenCalled()
    expect(standalone.createBlock).not.toHaveBeenCalled()

    const withNotebook = makeCtx({ blockId: 'blk-y' })
    const res2 = await handleCreateTask(withNotebook.ctx, {
      text: 't',
      notebook: 'Work'
    })
    expect(res2.error).toMatch(/require a page/)
    expect(withNotebook.createTask).not.toHaveBeenCalled()
  })

  it('falls back to the active notebook for a page-scoped task', async () => {
    const { ctx, createBlock } = makeCtx({
      activeNotebook: 'Personal',
      blockId: 'blk-3'
    })
    const res = await handleCreateTask(ctx, { text: 'x', page: 'Diary' })
    expect(res.error).toBeUndefined()
    expect(createBlock).toHaveBeenCalledWith(
      expect.objectContaining({ notebook: 'Personal', page: 'Diary' })
    )
  })

  it('applies metadata via the dedicated setters with the right args', async () => {
    const { ctx, setTaskDueDate, setTaskOwner, setTaskPriority, setTaskTags } =
      makeCtx({ blockId: 'blk-4' })
    const res = await handleCreateTask(ctx, {
      text: 't',
      due: '2026-08-01',
      owner: 'chris',
      priority: 2,
      tags: ['work', '#urgent']
    })
    expect(res.error).toBeUndefined()
    expect(setTaskDueDate).toHaveBeenCalledWith('blk-4', '2026-08-01')
    expect(setTaskOwner).toHaveBeenCalledWith('blk-4', 'chris')
    expect(setTaskPriority).toHaveBeenCalledWith('blk-4', 2)
    // Leading # is stripped — setTaskTags takes raw tag paths.
    expect(setTaskTags).toHaveBeenCalledWith('blk-4', ['work', 'urgent'])
  })

  it('omits setter calls for fields the caller did not supply', async () => {
    const { ctx, setTaskDueDate, setTaskOwner, setTaskPriority, setTaskTags } =
      makeCtx({ blockId: 'blk-5' })
    await handleCreateTask(ctx, { text: 'bare task' })
    expect(setTaskDueDate).not.toHaveBeenCalled()
    expect(setTaskOwner).not.toHaveBeenCalled()
    expect(setTaskPriority).not.toHaveBeenCalled()
    expect(setTaskTags).not.toHaveBeenCalled()
  })

  it('rejects empty text', async () => {
    const { ctx } = makeCtx()
    const res = await handleCreateTask(ctx, { text: '   ' })
    expect(res.error).toMatch(/text/)
  })

  it('rejects an out-of-range priority without creating anything', async () => {
    const { ctx, createTask } = makeCtx()
    const res = await handleCreateTask(ctx, { text: 't', priority: 5 })
    expect(res.error).toMatch(/priority/)
    expect(createTask).not.toHaveBeenCalled()
  })

  it('rejects a malformed or impossible due date without creating anything', async () => {
    const a = makeCtx()
    const malformed = await handleCreateTask(a.ctx, { text: 't', due: 'Aug 1' })
    expect(malformed.error).toMatch(/due/)
    expect(a.createTask).not.toHaveBeenCalled()

    // Well-formed but not a real calendar date (month 13 / day overflow).
    const b = makeCtx()
    const impossible = await handleCreateTask(b.ctx, {
      text: 't',
      due: '2026-13-40'
    })
    expect(impossible.error).toMatch(/due/)
    expect(b.createTask).not.toHaveBeenCalled()
  })

  it('errors when a page-scoped task has no notebook and no active notebook', async () => {
    const { ctx, createBlock } = makeCtx({ activeNotebook: '' })
    const res = await handleCreateTask(ctx, { text: 't', page: 'P' })
    expect(res.error).toMatch(/notebook/)
    expect(createBlock).not.toHaveBeenCalled()
  })

  it('propagates a capability-denied create failure (creates nothing)', async () => {
    const { ctx, createTask } = makeCtx({ rejectCreate: true })
    await expect(handleCreateTask(ctx, { text: 't' })).rejects.toThrow(
      /denied/i
    )
    expect(createTask).toHaveBeenCalledTimes(1)
  })

  it('reports the created id when a metadata setter fails (no duplicate risk)', async () => {
    const { ctx } = makeCtx({ blockId: 'blk-9', rejectSetter: 'priority' })
    const res = await handleCreateTask(ctx, {
      text: 't',
      owner: 'maya',
      priority: 2
    })
    expect(res.error).toMatch(/Task created/i)
    expect(res.error).toContain('blk-9')
    expect(res.error).toMatch(/priority/)
  })

  it('exposes the tool def shape', () => {
    expect(createTaskToolDef.name).toBe('create_task')
    expect(createTaskToolDef.parameters.required).toEqual(['text'])
  })
})
