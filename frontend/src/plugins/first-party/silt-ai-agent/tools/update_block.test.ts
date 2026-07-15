import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { PluginContext } from '../../../sdk'
import { clearTools } from '../tool-registry'
import {
  findStrippedTokens,
  handleUpdateBlock,
  updateBlockToolDef
} from './update_block'

function makeCtx(opts: {
  row?: { clean_content: string; type: string }
  mutateOk?: boolean
  setTagsOk?: boolean
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
    sqliteQuery
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

  it('updates a TASK block when tokens are preserved', async () => {
    const original =
      '- [x] Ship the agent tools [status:: DONE] [due:: 2026-07-20]'
    const rewritten =
      '- [x] Ship the agent tools (revised) [status:: DONE] [due:: 2026-07-20]'
    const { ctx, mutateBlock } = makeCtx({
      row: { clean_content: original, type: 'TASK' }
    })
    const res = await handleUpdateBlock(ctx, {
      block_id: 't1',
      content: rewritten
    })
    expect(res.error).toBeUndefined()
    expect(mutateBlock).toHaveBeenCalledWith('t1', rewritten)
  })

  it('rejects a TASK rewrite that strips [status::]', async () => {
    const original = '- [x] Ship [status:: DONE] [due:: 2026-07-20]'
    const rewritten = '- [x] Ship (no metadata anymore)'
    const { ctx, mutateBlock } = makeCtx({
      row: { clean_content: original, type: 'TASK' }
    })
    const res = await handleUpdateBlock(ctx, {
      block_id: 't2',
      content: rewritten
    })
    expect(res.error).toMatch(/Cannot remove task metadata/)
    expect(res.error).toMatch(/status/i)
    expect(res.error).toMatch(/due/i)
    expect(mutateBlock).not.toHaveBeenCalled()
  })

  it('rejects a TASK rewrite that strips [due::] but keeps status', async () => {
    const original = '- [ ] Build feature [status:: TODO] [due:: 2026-08-01]'
    const rewritten = '- [ ] Build feature [status:: TODO]'
    const { ctx, mutateBlock } = makeCtx({
      row: { clean_content: original, type: 'TASK' }
    })
    const res = await handleUpdateBlock(ctx, {
      block_id: 't3',
      content: rewritten
    })
    expect(res.error).toMatch(/Cannot remove task metadata/)
    expect(res.error).toMatch(/due/i)
    expect(mutateBlock).not.toHaveBeenCalled()
  })

  it('rejects a TASK rewrite that strips the checkbox state', async () => {
    const original = '- [x] Done thing [status:: DONE]'
    // No checkbox at all → status encoding removed.
    const rewritten = 'Done thing [status:: DONE]'
    const { ctx, mutateBlock } = makeCtx({
      row: { clean_content: original, type: 'TASK' }
    })
    const res = await handleUpdateBlock(ctx, {
      block_id: 't4',
      content: rewritten
    })
    expect(res.error).toMatch(/Cannot remove task metadata/)
    expect(res.error).toMatch(/checkbox/)
    expect(mutateBlock).not.toHaveBeenCalled()
  })

  it('allows adding tokens that were not present before', async () => {
    const original = '- [ ] Open task'
    const rewritten = '- [ ] Open task [status:: TODO] [due:: 2026-09-01]'
    const { ctx, mutateBlock } = makeCtx({
      row: { clean_content: original, type: 'TASK' }
    })
    const res = await handleUpdateBlock(ctx, {
      block_id: 't5',
      content: rewritten
    })
    expect(res.error).toBeUndefined()
    expect(mutateBlock).toHaveBeenCalledWith('t5', rewritten)
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
      row: {
        clean_content: '- [ ] Task [status:: TODO]',
        type: 'TASK'
      }
    })
    const res = await handleUpdateBlock(ctx, {
      block_id: 't',
      content: '- [ ] Task [status:: TODO]',
      tags: ['work/urgent', 'bug']
    })
    expect(res.error).toBeUndefined()
    // Body is mutated first (unchanged here), then tags applied.
    expect(mutateBlock).toHaveBeenCalled()
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

  it('findStrippedTokens is exported and detects all four keys + checkbox', () => {
    expect(findStrippedTokens('[status:: TODO]', '[status:: TODO]')).toEqual([])
    expect(findStrippedTokens('[due:: X]', 'no tokens')).toEqual(['[due:: X]'])
    expect(
      findStrippedTokens(
        '[owner:: alice] [priority:: 2]',
        '[owner:: alice]'
      ).sort()
    ).toEqual(['[priority:: 2]'])
    // Checkbox stripping.
    expect(findStrippedTokens('- [x] done', 'no checkbox')).toEqual([
      'checkbox: - [x]'
    ])
  })

  it('exposes the tool def shape', () => {
    expect(updateBlockToolDef.name).toBe('update_block')
    expect(updateBlockToolDef.parameters.required).toEqual([
      'block_id',
      'content'
    ])
  })
})
