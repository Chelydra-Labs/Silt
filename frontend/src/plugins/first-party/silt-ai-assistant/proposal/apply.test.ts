import { describe, expect, it, vi } from 'vitest'
import { v2CtxStubs } from '../../../test-helpers'
import type { PluginContext } from '../../../sdk'
import { createProposal } from './model'
import { applyProposal, buildReplacedBlockText } from './apply'

function scope(over: Record<string, unknown> = {}) {
  return {
    notebook: 'nb',
    section: 'sec',
    page: 'page',
    inputText: 'x',
    truncated: false,
    targetBlockId: 'block-1',
    targetBlockText: 'Hello world today',
    selectionText: 'world',
    replaceFullBlock: false,
    ...over
  }
}

describe('buildReplacedBlockText', () => {
  it('splices partial selection', () => {
    expect(
      buildReplacedBlockText('Hello world today', 'world', 'planet', false)
    ).toBe('Hello planet today')
  })
  it('full replace when replaceFullBlock', () => {
    expect(
      buildReplacedBlockText('Hello world', 'Hello world', 'Bye', true)
    ).toBe('Bye')
  })
})

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

  it('mutates block with spliced selection on replace accept', async () => {
    const mutateBlock = vi.fn(async () => true)
    const ctx = { ...v2CtxStubs, mutateBlock } as unknown as PluginContext
    const p = createProposal({
      actionId: 'improve-clarity',
      kind: 'replace-selection',
      scope: scope(),
      proposedMarkdown: 'planet',
      status: 'ready'
    })
    const res = await applyProposal(ctx, p)
    expect(res.ok).toBe(true)
    expect(mutateBlock).toHaveBeenCalledWith('block-1', 'Hello planet today')
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

  it('merges tags with existing on task accept', async () => {
    const setTaskTags = vi.fn(async () => true)
    const sqliteQuery = vi.fn(async () => ({
      rows: [{ tag: 'old/tag' }],
      truncated: false
    }))
    const ctx = {
      ...v2CtxStubs,
      setTaskTags,
      sqliteQuery
    } as unknown as PluginContext
    const p = createProposal({
      actionId: 'suggest-tags',
      kind: 'apply-tags',
      scope: scope({ targetBlockId: 'task-1' }),
      tags: [{ tag: 'new/tag', existing: false }],
      status: 'ready'
    })
    const res = await applyProposal(ctx, p)
    expect(res.ok).toBe(true)
    expect(setTaskTags).toHaveBeenCalledWith('task-1', ['old/tag', 'new/tag'])
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
