import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { PluginContext } from '../../../sdk'
import { clearTools } from '../tool-registry'
import {
  handleUpdateBlock,
  stripTaskMetadata,
  updateBlockToolDef
} from './update_block'

function makeCtx(opts: {
  row?: { clean_content: string; type: string }
  mutateOk?: boolean
  setTagsOk?: boolean
  ai?: { auditEvent: ReturnType<typeof vi.fn> }
}): {
  ctx: PluginContext
  mutateBlock: ReturnType<typeof vi.fn>
  setTaskTags: ReturnType<typeof vi.fn>
  sqliteQuery: ReturnType<typeof vi.fn>
} {
  const mutateBlock = vi.fn(async () => opts.mutateOk ?? true)
  const setTaskTags = vi.fn(async () => opts.setTagsOk ?? true)
  const sqliteQuery = vi.fn(async () => {
    if (!opts.row) return { rows: [], truncated: false }
    return {
      rows: [opts.row] as unknown as Record<string, unknown>[],
      truncated: false
    }
  })
  const ctx = {
    mutateBlock,
    setTaskTags,
    sqliteQuery,
    ...(opts.ai ? { ai: opts.ai } : {})
  } as unknown as PluginContext
  return { ctx, mutateBlock, setTaskTags, sqliteQuery }
}

beforeEach(() => clearTools())
afterEach(() => clearTools())

describe('update_block', () => {
  it('updates a NOTE block by calling mutateBlock with the new content', async () => {
    const { ctx, mutateBlock } = makeCtx({
      row: { clean_content: 'old note text', type: 'NOTE' }
    })
    const res = await handleUpdateBlock(ctx, {
      block_id: 'b1',
      content: 'shiny new note text'
    })
    expect(res.error).toBeUndefined()
    expect(mutateBlock).toHaveBeenCalledWith('b1', 'shiny new note text')
    expect(res.content).toContain('b1')
  })

  it('updates a TASK block prose (clean_content is prose-only in storage)', async () => {
    // TASK clean_content holds the token-stripped description; metadata lives
    // in structured columns preserved by the backend.
    const { ctx, mutateBlock } = makeCtx({
      row: { clean_content: 'Ship the agent tools', type: 'TASK' }
    })
    const res = await handleUpdateBlock(ctx, {
      block_id: 't1',
      content: 'Ship the agent tools (revised)'
    })
    expect(res.error).toBeUndefined()
    expect(mutateBlock).toHaveBeenCalledWith(
      't1',
      'Ship the agent tools (revised)'
    )
  })

  it('strips a checkbox and injected metadata tokens from a TASK rewrite', async () => {
    const { ctx, mutateBlock } = makeCtx({
      row: { clean_content: 'Build feature', type: 'TASK' }
    })
    const res = await handleUpdateBlock(ctx, {
      block_id: 't2',
      content: '- [ ] Build feature [owner:: mallory] [due:: 2099-01-01]'
    })
    expect(res.error).toBeUndefined()
    // Only the prose reaches mutateBlock; structured metadata is untouched.
    expect(mutateBlock).toHaveBeenCalledWith('t2', 'Build feature')
  })

  it('rejects a TASK rewrite that is only tokens/checkbox (no prose)', async () => {
    const { ctx, mutateBlock } = makeCtx({
      row: { clean_content: 'whatever', type: 'TASK' }
    })
    const res = await handleUpdateBlock(ctx, {
      block_id: 't3',
      content: '- [x] [status:: DONE] [owner:: alice]'
    })
    expect(res.error).toMatch(/no prose after stripping task metadata/i)
    expect(mutateBlock).not.toHaveBeenCalled()
  })

  it('errors when the block is not found', async () => {
    const { ctx, mutateBlock } = makeCtx({ row: undefined })
    const res = await handleUpdateBlock(ctx, {
      block_id: 'ghost',
      content: 'x'
    })
    expect(res.error).toMatch(/not found/)
    expect(mutateBlock).not.toHaveBeenCalled()
  })

  it('reports an error when mutateBlock returns false', async () => {
    const { ctx } = makeCtx({
      row: { clean_content: 'old', type: 'NOTE' },
      mutateOk: false
    })
    const res = await handleUpdateBlock(ctx, {
      block_id: 'b',
      content: 'new'
    })
    expect(res.error).toMatch(/mutateBlock failed/)
  })

  it('applies a tag override for a TASK via setTaskTags', async () => {
    const { ctx, mutateBlock, setTaskTags } = makeCtx({
      row: { clean_content: 'Task', type: 'TASK' }
    })
    const res = await handleUpdateBlock(ctx, {
      block_id: 't',
      content: 'Task',
      tags: ['work/urgent', 'bug']
    })
    expect(res.error).toBeUndefined()
    // Body (prose) mutated first, then tags applied via the dedicated API.
    expect(mutateBlock).toHaveBeenCalledWith('t', 'Task')
    expect(setTaskTags).toHaveBeenCalledWith('t', ['work/urgent', 'bug'])
  })

  it('folds a tag override into a NOTE body as hashtags', async () => {
    const { ctx, mutateBlock, setTaskTags } = makeCtx({
      row: { clean_content: 'old note', type: 'NOTE' }
    })
    await handleUpdateBlock(ctx, {
      block_id: 'n',
      content: 'new note',
      tags: ['work/urgent']
    })
    const written = mutateBlock.mock.calls[0][1] as string
    expect(written).toContain('new note')
    expect(written).toContain('#work/urgent')
    // Non-task path must not call setTaskTags.
    expect(setTaskTags).not.toHaveBeenCalled()
  })

  it('rejects empty block_id or content', async () => {
    const { ctx } = makeCtx({ row: { clean_content: 'x', type: 'NOTE' } })
    const noId = await handleUpdateBlock(ctx, { block_id: '', content: 'x' })
    expect(noId.error).toMatch(/block_id/)
    const noBody = await handleUpdateBlock(ctx, {
      block_id: 'b',
      content: '   '
    })
    expect(noBody.error).toMatch(/content/)
  })

  it('emits a tool_result audit event on success', async () => {
    const auditEvent = vi.fn(async (_payload: unknown) => {})
    const { ctx } = makeCtx({
      row: { clean_content: 'old note', type: 'NOTE' },
      ai: { auditEvent }
    })
    const res = await handleUpdateBlock(ctx, {
      block_id: 'b1',
      content: 'shiny new note text'
    })
    expect(res.error).toBeUndefined()
    expect(auditEvent).toHaveBeenCalledWith(
      expect.objectContaining({
        kind: 'tool_result',
        tool: 'update_block',
        status: 'ok'
      })
    )
    expect(auditEvent.mock.calls[0][0]).toMatchObject({ block_id: 'b1' })
  })

  it('stripTaskMetadata removes checkbox + tokens and collapses whitespace', () => {
    expect(stripTaskMetadata('- [x] Do thing')).toBe('Do thing')
    expect(
      stripTaskMetadata('- [ ] Do thing [owner:: alice] [due:: 2026-08-01]')
    ).toBe('Do thing')
    expect(stripTaskMetadata('Plain prose')).toBe('Plain prose')
    expect(stripTaskMetadata('  - [~]   A   [p:: 2]   B  ')).toBe('A B')
    expect(stripTaskMetadata('- [x] [status:: DONE]')).toBe('')
  })

  it('exposes the tool def shape', () => {
    expect(updateBlockToolDef.name).toBe('update_block')
    expect(updateBlockToolDef.parameters.required).toEqual([
      'block_id',
      'content'
    ])
  })
})
