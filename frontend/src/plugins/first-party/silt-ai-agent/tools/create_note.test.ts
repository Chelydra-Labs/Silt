import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { PluginContext } from '../../../sdk'
import { clearTools } from '../tool-registry'
import { createNoteToolDef, handleCreateNote } from './create_note'

function makeCtx(opts: { activeNotebook?: string; blockId?: string }): {
  ctx: PluginContext
  createPage: ReturnType<typeof vi.fn>
  createBlock: ReturnType<typeof vi.fn>
} {
  const createPage = vi.fn(async () => 'page-uuid')
  const createBlock = vi.fn(async () => opts.blockId ?? 'new-block-id')
  const ctx = {
    activeNotebook: opts.activeNotebook ?? '',
    createPage,
    createBlock
  } as unknown as PluginContext
  return { ctx, createPage, createBlock }
}

beforeEach(() => clearTools())
afterEach(() => clearTools())

describe('create_note', () => {
  it('creates a page then a NOTE block and returns the block id', async () => {
    const { ctx, createPage, createBlock } = makeCtx({ blockId: 'blk-1' })
    const res = await handleCreateNote(ctx, {
      notebook: 'Work',
      section: 'Notes',
      page: 'Decisions',
      content: 'Use Postgres.'
    })
    expect(res.error).toBeUndefined()
    expect(createPage).toHaveBeenCalledWith('Work', 'Notes', 'Decisions')
    expect(createBlock).toHaveBeenCalledWith({
      type: 'NOTE',
      text: 'Use Postgres.',
      notebook: 'Work',
      section: 'Notes',
      page: 'Decisions'
    })
    expect(res.content).toContain('blk-1')
    expect(res.content).toContain('Work/Notes/Decisions')
  })

  it('falls back to the active notebook when notebook is omitted', async () => {
    const { ctx, createBlock } = makeCtx({
      activeNotebook: 'Personal',
      blockId: 'blk-2'
    })
    const res = await handleCreateNote(ctx, {
      page: 'Diary',
      content: 'Today was good.'
    })
    expect(res.error).toBeUndefined()
    expect(createBlock).toHaveBeenCalledWith(
      expect.objectContaining({ notebook: 'Personal', section: '' })
    )
    expect(res.content).toContain('Personal/Diary')
  })

  it('errors when no notebook and no active notebook', async () => {
    const { ctx } = makeCtx({ activeNotebook: '' })
    const res = await handleCreateNote(ctx, {
      page: 'P',
      content: 'x'
    })
    expect(res.error).toMatch(/no active notebook/i)
  })

  it('appends (does not overwrite): createPage is called and the block is added', async () => {
    // createPage is idempotent (no-op on existing) and createBlock appends.
    // The tool calls both unconditionally, so an existing page gains a block
    // rather than losing its prior content.
    const { ctx, createPage, createBlock } = makeCtx({ blockId: 'blk-3' })
    await handleCreateNote(ctx, {
      notebook: 'N',
      page: 'Existing',
      content: 'second note'
    })
    await handleCreateNote(ctx, {
      notebook: 'N',
      page: 'Existing',
      content: 'third note'
    })
    expect(createPage).toHaveBeenCalledTimes(2)
    expect(createBlock).toHaveBeenCalledTimes(2)
  })

  it('folds tags into the block text as hashtags', async () => {
    const { ctx, createBlock } = makeCtx({ blockId: 'blk-4' })
    await handleCreateNote(ctx, {
      notebook: 'N',
      page: 'P',
      content: 'tagged note',
      tags: ['work/urgent', 'bug']
    })
    const text = (createBlock.mock.calls[0][0] as { text: string }).text
    expect(text).toContain('tagged note')
    expect(text).toContain('#work/urgent')
    expect(text).toContain('#bug')
  })

  it('rejects empty page or content', async () => {
    const { ctx } = makeCtx({ activeNotebook: 'N' })
    const noPage = await handleCreateNote(ctx, { page: '', content: 'x' })
    expect(noPage.error).toMatch(/page/)
    const noContent = await handleCreateNote(ctx, { page: 'P', content: '' })
    expect(noContent.error).toMatch(/content/)
  })

  it('exposes the tool def shape', () => {
    expect(createNoteToolDef.name).toBe('create_note')
    expect(createNoteToolDef.parameters.required).toEqual(['page', 'content'])
  })
})
