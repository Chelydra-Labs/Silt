import { describe, expect, it, vi } from 'vitest'
import { v2CtxStubs } from '../../../test-helpers'
import type { PluginContext } from '../../../sdk'
import { createProposal } from './model'
import { applyProposal } from './apply'

function scope() {
  return {
    notebook: 'nb',
    section: 'sec',
    page: 'page',
    inputText: 'x',
    truncated: false,
    targetBlockId: 'block-1'
  }
}

describe('applyProposal', () => {
  it('refuses non-ready proposals', async () => {
    const ctx = { ...v2CtxStubs } as unknown as PluginContext
    const p = createProposal({
      actionId: 'improve-clarity',
      kind: 'replace-selection',
      scope: scope(),
      proposedMarkdown: 'hi',
      status: 'streaming'
    })
    const res = await applyProposal(ctx, p)
    expect(res.ok).toBe(false)
  })

  it('mutates block on replace accept', async () => {
    const mutateBlock = vi.fn(async () => true)
    const ctx = { ...v2CtxStubs, mutateBlock } as unknown as PluginContext
    const p = createProposal({
      actionId: 'improve-clarity',
      kind: 'replace-selection',
      scope: scope(),
      proposedMarkdown: 'Clearer',
      status: 'ready'
    })
    const res = await applyProposal(ctx, p)
    expect(res.ok).toBe(true)
    expect(mutateBlock).toHaveBeenCalledWith('block-1', 'Clearer')
  })

  it('creates tasks on insert-tasks accept', async () => {
    const createBlock = vi.fn(async () => 'new-id')
    const ctx = { ...v2CtxStubs, createBlock } as unknown as PluginContext
    const p = createProposal({
      actionId: 'extract-tasks',
      kind: 'insert-tasks',
      scope: scope(),
      tasks: ['Do thing'],
      status: 'ready'
    })
    const res = await applyProposal(ctx, p)
    expect(res.ok).toBe(true)
    expect(createBlock).toHaveBeenCalledWith(
      expect.objectContaining({ type: 'TASK', text: 'Do thing' })
    )
  })

  it('inserts related links on accept', async () => {
    const createBlock = vi.fn(async () => 'new-id')
    const ctx = { ...v2CtxStubs, createBlock } as unknown as PluginContext
    const p = createProposal({
      actionId: 'suggest-related',
      kind: 'insert-links',
      scope: scope(),
      related: [
        {
          blockId: 'uuid-1',
          snippet: 'snip',
          score: 0.9
        }
      ],
      status: 'ready'
    })
    const res = await applyProposal(ctx, p)
    expect(res.ok).toBe(true)
    expect(createBlock).toHaveBeenCalledWith(
      expect.objectContaining({
        type: 'NOTE',
        text: expect.stringContaining('((uuid-1))')
      })
    )
  })
})
