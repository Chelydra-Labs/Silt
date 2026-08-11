import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { PluginContext } from '../../../sdk'
import { clearTools } from '../tool-registry'
import {
  commitExtractAndSave,
  extractAndSaveToolDef,
  handleExtractAndSave,
  parseExtraction,
  previewDetails,
  targetPageExists
} from './extract_and_save'

interface SourceBlock {
  id: string
  clean_content: string
}

vi.mock('../staging', () => ({
  stageOperation: vi.fn(async () => 'tok-extract-1')
}))

import { stageOperation } from '../staging'

/**
 * Build a ctx whose sqliteQuery returns the canned source blocks for the
 * `WHERE id IN (...)` lookup, and whose ai.complete returns the configured
 * JSON content. createPage/applyBlocks/deletePage stubbed for commit path.
 */
function makeCtx(opts: {
  sources: SourceBlock[]
  completeContent?: string
  completeError?: Error
  auditEvent?: ReturnType<typeof vi.fn>
  applyBlocksOk?: boolean
  applyBlocksImpl?: ReturnType<typeof vi.fn>
  /** When true, target page already exists before commit (no cleanup delete). */
  pageExists?: boolean
}): {
  ctx: PluginContext
  complete: ReturnType<typeof vi.fn>
  createPage: ReturnType<typeof vi.fn>
  createBlock: ReturnType<typeof vi.fn>
  applyBlocks: ReturnType<typeof vi.fn>
  deletePage: ReturnType<typeof vi.fn>
  mutateBlock: ReturnType<typeof vi.fn>
  sqliteCalls: { sql: string; params: unknown[] }[]
} {
  const srcById = new Map(opts.sources.map((s) => [s.id, s]))
  const sqliteCalls: { sql: string; params: unknown[] }[] = []
  const sqliteQuery = vi.fn(async (sql: string, params?: unknown[]) => {
    sqliteCalls.push({ sql, params: [...(params ?? [])] })
    const lower = sql.toLowerCase()
    if (lower.includes('where id in (')) {
      const ids = (params ?? []) as string[]
      const rows = ids
        .map((id) => srcById.get(id))
        .filter((b): b is SourceBlock => b !== undefined)
        .map((b) => ({ id: b.id, clean_content: b.clean_content }))
      return { rows, truncated: false }
    }
    // targetPageExists probes blocks (then page_types) by notebook/section/page.
    if (
      (lower.includes('from blocks') || lower.includes('from page_types')) &&
      lower.includes('notebook = ?') &&
      lower.includes('page = ?')
    ) {
      return {
        rows: opts.pageExists ? [{ ok: 1 }] : [],
        truncated: false
      }
    }
    return { rows: [], truncated: false }
  })

  const complete = vi.fn(async () => {
    if (opts.completeError) throw opts.completeError
    return { content: opts.completeContent ?? '', model: 'm' }
  })

  const createPage = vi.fn(async () => 'page-uuid')
  const createBlock = vi.fn(async () => 'blk-should-not-use')
  const applyBlocks =
    opts.applyBlocksImpl ?? vi.fn(async () => opts.applyBlocksOk !== false)
  const deletePage = vi.fn(async () => true)
  const mutateBlock = vi.fn(async () => true)

  const ctx = {
    sqliteQuery,
    ai: {
      complete,
      embed: vi.fn(async () => ({ embeddings: [], model: 'm', dimensions: 0 })),
      ...(opts.auditEvent ? { auditEvent: opts.auditEvent } : {})
    },
    createPage,
    createBlock,
    applyBlocks,
    deletePage,
    mutateBlock
  } as unknown as PluginContext
  return {
    ctx,
    complete,
    createPage,
    createBlock,
    applyBlocks,
    deletePage,
    mutateBlock,
    sqliteCalls
  }
}

beforeEach(() => {
  clearTools()
  vi.mocked(stageOperation).mockClear()
  vi.mocked(stageOperation).mockResolvedValue('tok-extract-1')
})
afterEach(() => clearTools())

const SOURCES = [
  { id: 'src-1', clean_content: 'Postgres uses MVCC.' },
  { id: 'src-2', clean_content: 'Redis is single-threaded.' }
]
const TARGET = { notebook: 'Work', section: 'Notes', page: 'Distilled' }

describe('extract_and_save stage (handleExtractAndSave)', () => {
  it('summary mode: stages frozen content, no vault write', async () => {
    const { ctx, createPage, createBlock, applyBlocks, mutateBlock } = makeCtx({
      sources: SOURCES,
      completeContent: JSON.stringify({
        summary: 'Postgres uses MVCC; Redis is single-threaded.'
      })
    })
    const res = await handleExtractAndSave(ctx, {
      source_block_ids: ['src-1', 'src-2'],
      mode: 'summary',
      target: TARGET
    })
    expect(res.error).toBeUndefined()
    expect(res.isStaged).toBe(true)
    expect(res.stagedToken).toBe('tok-extract-1')
    expect(res.stagedPreview?.summary).toMatch(
      /Extract summary → Work\/Notes\/Distilled/
    )
    expect(res.stagedPreview?.details).toMatch(/Postgres uses MVCC/)
    expect(res.stagedPreview?.details).toMatch(
      /<!-- src: src-1,src-2 mode:summary -->/
    )
    expect(createPage).not.toHaveBeenCalled()
    expect(createBlock).not.toHaveBeenCalled()
    expect(applyBlocks).not.toHaveBeenCalled()
    expect(mutateBlock).not.toHaveBeenCalled()
    expect(stageOperation).toHaveBeenCalledTimes(1)
    const frozen = vi.mocked(stageOperation).mock.calls[0][2] as {
      mode: string
      blocks: string[]
      source_block_ids: string[]
    }
    expect(frozen.mode).toBe('summary')
    expect(frozen.blocks).toHaveLength(1)
    expect(frozen.blocks[0]).toMatch(/Postgres uses MVCC/)
    expect(frozen.source_block_ids).toEqual(['src-1', 'src-2'])
  })

  it('flashcards mode: freezes one body per item', async () => {
    const { ctx } = makeCtx({
      sources: SOURCES,
      completeContent: JSON.stringify({
        items: [
          { front: 'What concurrency model does Postgres use?', back: 'MVCC.' },
          { front: 'Is Redis multi-threaded?', back: 'No, single-threaded.' }
        ]
      })
    })
    const res = await handleExtractAndSave(ctx, {
      source_block_ids: ['src-1', 'src-2'],
      mode: 'flashcards',
      target: TARGET
    })
    expect(res.isStaged).toBe(true)
    const frozen = vi.mocked(stageOperation).mock.calls[0][2] as {
      blocks: string[]
    }
    expect(frozen.blocks).toHaveLength(2)
    expect(frozen.blocks[0]).toMatch(
      /Q1: What concurrency model does Postgres use\?/
    )
    expect(frozen.blocks[1]).toMatch(/Q2: Is Redis multi-threaded\?/)
    expect(res.stagedPreview?.affectedCount).toBe(2)
  })

  it('qa_pairs mode: freezes Q/A bodies', async () => {
    const { ctx } = makeCtx({
      sources: SOURCES,
      completeContent: JSON.stringify({
        items: [
          { question: 'Postgres MVCC?', answer: 'Multi-version concurrency.' },
          {
            question: 'Redis threading?',
            answer: 'Single-threaded event loop.'
          }
        ]
      })
    })
    await handleExtractAndSave(ctx, {
      source_block_ids: ['src-1'],
      mode: 'qa_pairs',
      target: TARGET
    })
    const frozen = vi.mocked(stageOperation).mock.calls[0][2] as {
      blocks: string[]
    }
    expect(frozen.blocks).toHaveLength(2)
    expect(frozen.blocks[0]).toMatch(/Q1: Postgres MVCC\?/)
  })

  it('action_items mode: freezes GFM checkboxes with optional due', async () => {
    const { ctx } = makeCtx({
      sources: SOURCES,
      completeContent: JSON.stringify({
        items: [
          { title: 'Set up Postgres replication', due_date: '2026-08-01' },
          { title: 'Document Redis failover' }
        ]
      })
    })
    await handleExtractAndSave(ctx, {
      source_block_ids: ['src-1'],
      mode: 'action_items',
      target: TARGET
    })
    const frozen = vi.mocked(stageOperation).mock.calls[0][2] as {
      blocks: string[]
    }
    expect(frozen.blocks[0]).toMatch(/- \[ \] Set up Postgres replication/)
    expect(frozen.blocks[0]).toMatch(/\[due:: 2026-08-01\]/)
    expect(frozen.blocks[1]).toMatch(/- \[ \] Document Redis failover/)
  })

  it('passes responseSchema + citation prompt to ctx.ai.complete', async () => {
    const { ctx, complete } = makeCtx({
      sources: SOURCES,
      completeContent: JSON.stringify({ summary: 'ok.' })
    })
    await handleExtractAndSave(ctx, {
      source_block_ids: ['src-1'],
      mode: 'summary',
      target: TARGET
    })
    expect(complete).toHaveBeenCalledTimes(1)
    const req = complete.mock.calls[0][0] as {
      messages: { role: string; content: string }[]
      responseSchema: { properties: Record<string, unknown> }
      temperature?: number
    }
    expect(req.messages[0].role).toBe('system')
    expect(req.responseSchema.properties.summary).toBeDefined()
    expect(req.temperature).toBeLessThan(0.5)
    const userMsg = req.messages.find((m) => m.role === 'user')?.content ?? ''
    expect(userMsg).toContain('src-1')
    expect(userMsg).toContain('Postgres uses MVCC')
  })

  it('prepends SECURITY preamble to nested system prompt', async () => {
    const { ctx, complete } = makeCtx({
      sources: SOURCES,
      completeContent: JSON.stringify({ summary: 'ok.' })
    })
    await handleExtractAndSave(ctx, {
      source_block_ids: ['src-1'],
      mode: 'summary',
      target: TARGET
    })
    const req = complete.mock.calls[0][0] as {
      messages: { role: string; content: string }[]
    }
    const sys = req.messages.find((m) => m.role === 'system')?.content ?? ''
    expect(sys).toContain('SECURITY:')
    expect(sys).toContain('untrusted DATA')
    const user = req.messages.find((m) => m.role === 'user')?.content ?? ''
    expect(user).toContain('<vault_data tool="extract_and_save">')
  })

  it('returns error and stages nothing when model returns non-JSON', async () => {
    const { ctx, createPage, applyBlocks } = makeCtx({
      sources: SOURCES,
      completeContent: 'Sorry, I could not extract anything.'
    })
    const res = await handleExtractAndSave(ctx, {
      source_block_ids: ['src-1'],
      mode: 'flashcards',
      target: TARGET
    })
    expect(res.error).toMatch(/not JSON|did not match/i)
    expect(res.isStaged).toBeFalsy()
    expect(stageOperation).not.toHaveBeenCalled()
    expect(createPage).not.toHaveBeenCalled()
    expect(applyBlocks).not.toHaveBeenCalled()
  })

  it('returns error when ctx.ai.complete throws', async () => {
    const { ctx, createPage } = makeCtx({
      sources: SOURCES,
      completeError: new Error('model unreachable')
    })
    const res = await handleExtractAndSave(ctx, {
      source_block_ids: ['src-1'],
      mode: 'summary',
      target: TARGET
    })
    expect(res.error).toMatch(/extraction call failed/)
    expect(res.error).toContain('model unreachable')
    expect(stageOperation).not.toHaveBeenCalled()
    expect(createPage).not.toHaveBeenCalled()
  })

  it('aborts before complete when signal already aborted', async () => {
    const { ctx, complete, createPage } = makeCtx({
      sources: SOURCES,
      completeContent: JSON.stringify({ summary: 'ok.' })
    })
    const ac = new AbortController()
    ac.abort()
    const res = await handleExtractAndSave(
      ctx,
      {
        source_block_ids: ['src-1'],
        mode: 'summary',
        target: TARGET
      },
      ac.signal
    )
    expect(res.error).toMatch(/Cancelled/)
    expect(complete).not.toHaveBeenCalled()
    expect(stageOperation).not.toHaveBeenCalled()
    expect(createPage).not.toHaveBeenCalled()
  })

  it('reads source blocks via parameterized IN', async () => {
    const { ctx, sqliteCalls } = makeCtx({
      sources: SOURCES,
      completeContent: JSON.stringify({ summary: 'ok.' })
    })
    await handleExtractAndSave(ctx, {
      source_block_ids: ['src-1', 'src-2'],
      mode: 'summary',
      target: TARGET
    })
    const read = sqliteCalls.find((c) =>
      c.sql.toLowerCase().includes('where id in (')
    )
    expect(read).toBeDefined()
    expect((read!.sql.match(/\?/g) ?? []).length).toBe(2)
    expect(read!.params).toEqual(['src-1', 'src-2'])
  })

  it('rejects too many source ids', async () => {
    const { ctx } = makeCtx({ sources: SOURCES })
    const res = await handleExtractAndSave(ctx, {
      source_block_ids: Array.from({ length: 21 }, (_, i) => `b${i}`),
      mode: 'summary',
      target: TARGET
    })
    expect(res.error).toMatch(/exceeds the 20-id limit/)
  })

  it('rejects unknown mode and empty target page', async () => {
    const { ctx } = makeCtx({ sources: SOURCES })
    expect(
      (
        await handleExtractAndSave(ctx, {
          source_block_ids: ['src-1'],
          mode: 'translation',
          target: TARGET
        })
      ).error
    ).toMatch(/mode must be one of/)
    expect(
      (
        await handleExtractAndSave(ctx, {
          source_block_ids: ['src-1'],
          mode: 'summary',
          target: { notebook: 'Work', page: '' }
        })
      ).error
    ).toMatch(/target\.page/)
  })

  it('does not stage on non-JSON (no salvage)', async () => {
    const { ctx } = makeCtx({
      sources: SOURCES,
      completeContent: 'not json ' + 'x'.repeat(20_000)
    })
    const res = await handleExtractAndSave(ctx, {
      source_block_ids: ['src-1'],
      mode: 'summary',
      target: TARGET
    })
    expect(res.error).toBeTruthy()
    expect(stageOperation).not.toHaveBeenCalled()
  })

  it('does not audit ok on stage (audit on commit)', async () => {
    const auditEvent = vi.fn(async (_payload: unknown) => {})
    const { ctx } = makeCtx({
      sources: SOURCES,
      completeContent: JSON.stringify({ summary: 'ok.' }),
      auditEvent
    })
    const res = await handleExtractAndSave(ctx, {
      source_block_ids: ['src-1'],
      mode: 'summary',
      target: TARGET
    })
    expect(res.isStaged).toBe(true)
    // Stage path should not emit success audit (write has not happened).
    expect(auditEvent).not.toHaveBeenCalledWith(
      expect.objectContaining({ status: 'ok' })
    )
  })

  it('exposes the tool def shape', () => {
    expect(extractAndSaveToolDef.name).toBe('extract_and_save')
    expect(extractAndSaveToolDef.parameters.required).toEqual([
      'source_block_ids',
      'mode',
      'target'
    ])
  })
})

describe('extract_and_save commit (commitExtractAndSave)', () => {
  const frozen = {
    mode: 'summary',
    target: TARGET,
    source_block_ids: ['src-1', 'src-2'],
    blocks: [
      'Postgres uses MVCC; Redis is single-threaded.\n\n<!-- src: src-1,src-2 mode:summary -->'
    ]
  }

  it('writes frozen blocks via applyBlocks; never calls complete', async () => {
    const { ctx, complete, createPage, applyBlocks, createBlock } = makeCtx({
      sources: SOURCES
    })
    const res = await commitExtractAndSave(ctx, frozen)
    expect(res.error).toBeUndefined()
    expect(complete).not.toHaveBeenCalled()
    expect(createPage).toHaveBeenCalledWith('Work', 'Notes', 'Distilled')
    expect(createBlock).not.toHaveBeenCalled()
    expect(applyBlocks).toHaveBeenCalledTimes(1)
    const ops = applyBlocks.mock.calls[0][0] as Array<{
      kind: string
      type: string
      text: string
      notebook: string
      page: string
    }>
    expect(ops).toHaveLength(1)
    expect(ops[0].kind).toBe('create')
    expect(ops[0].type).toBe('NOTE')
    expect(ops[0].text).toMatch(/Postgres uses MVCC/)
    expect(ops[0].notebook).toBe('Work')
    expect(res.content).toContain('Work/Notes/Distilled')
  })

  it('fail-closed on malformed frozen params (failed parse after confirm)', async () => {
    const { ctx, createPage, applyBlocks } = makeCtx({ sources: SOURCES })
    const bad = await commitExtractAndSave(ctx, {
      mode: 'summary',
      target: TARGET,
      blocks: []
    })
    expect(bad.error).toMatch(/malformed/)
    expect(createPage).not.toHaveBeenCalled()
    expect(applyBlocks).not.toHaveBeenCalled()

    const badMode = await commitExtractAndSave(ctx, {
      mode: 'nope',
      target: TARGET,
      blocks: ['x']
    })
    expect(badMode.error).toMatch(/malformed/)
  })

  it('aborts before write when signal already aborted', async () => {
    const { ctx, createPage, applyBlocks } = makeCtx({ sources: SOURCES })
    const ac = new AbortController()
    ac.abort()
    const res = await commitExtractAndSave(ctx, frozen, ac.signal)
    expect(res.error).toMatch(/Cancelled/)
    expect(createPage).not.toHaveBeenCalled()
    expect(applyBlocks).not.toHaveBeenCalled()
  })

  it('cleans up minted page when applyBlocks fails after createPage', async () => {
    const { ctx, createPage, deletePage, applyBlocks } = makeCtx({
      sources: SOURCES,
      applyBlocksOk: false,
      pageExists: false
    })
    const res = await commitExtractAndSave(ctx, frozen)
    expect(res.error).toMatch(/failed to write/)
    expect(createPage).toHaveBeenCalled()
    expect(applyBlocks).toHaveBeenCalled()
    expect(deletePage).toHaveBeenCalledWith('Work', 'Notes', 'Distilled')
  })

  it('cleans up minted page when applyBlocks throws mid-write', async () => {
    const applyBlocks = vi.fn(async () => {
      throw new Error('disk full')
    })
    const { ctx, deletePage } = makeCtx({
      sources: SOURCES,
      applyBlocksImpl: applyBlocks,
      pageExists: false
    })
    const res = await commitExtractAndSave(ctx, frozen)
    expect(res.error).toMatch(/disk full/)
    expect(deletePage).toHaveBeenCalledWith('Work', 'Notes', 'Distilled')
  })

  it('does not delete pre-existing target page when apply fails', async () => {
    const { ctx, deletePage, createPage } = makeCtx({
      sources: SOURCES,
      applyBlocksOk: false,
      pageExists: true
    })
    const res = await commitExtractAndSave(ctx, frozen)
    expect(res.error).toMatch(/failed to write/)
    expect(createPage).toHaveBeenCalled()
    expect(deletePage).not.toHaveBeenCalled()
  })

  it('aborts after createPage before apply and cleans up only minted pages', async () => {
    let applyCalled = false
    const applyBlocks = vi.fn(async () => {
      applyCalled = true
      return true
    })
    const deletePage = vi.fn(async () => true)
    const sqliteQuery = vi.fn(async () => ({ rows: [], truncated: false }))
    const ac = new AbortController()
    const ctx = {
      sqliteQuery,
      createPage: vi.fn(async () => {
        ac.abort()
        return 'page-uuid'
      }),
      createBlock: vi.fn(),
      applyBlocks,
      deletePage,
      ai: { complete: vi.fn(), embed: vi.fn() }
    } as unknown as PluginContext

    const res = await commitExtractAndSave(ctx, frozen, ac.signal)
    expect(res.error).toMatch(/Cancelled/)
    expect(applyCalled).toBe(false)
    expect(deletePage).toHaveBeenCalledWith('Work', 'Notes', 'Distilled')
  })

  it('does not delete pre-existing page on abort after createPage no-op', async () => {
    const deletePage = vi.fn(async () => true)
    const applyBlocks = vi.fn(async () => true)
    const ac = new AbortController()
    const ctx = {
      sqliteQuery: vi.fn(async (sql: string) => {
        if (sql.toLowerCase().includes('from blocks')) {
          return { rows: [{ ok: 1 }], truncated: false }
        }
        return { rows: [], truncated: false }
      }),
      createPage: vi.fn(async () => {
        ac.abort()
        return 'page-uuid'
      }),
      createBlock: vi.fn(),
      applyBlocks,
      deletePage,
      ai: { complete: vi.fn(), embed: vi.fn() }
    } as unknown as PluginContext

    const res = await commitExtractAndSave(ctx, frozen, ac.signal)
    expect(res.error).toMatch(/Cancelled/)
    expect(applyBlocks).not.toHaveBeenCalled()
    expect(deletePage).not.toHaveBeenCalled()
  })

  it('emits tool_result audit on successful commit', async () => {
    const auditEvent = vi.fn(async (_payload: unknown) => {})
    const { ctx } = makeCtx({ sources: SOURCES, auditEvent })
    const res = await commitExtractAndSave(ctx, frozen)
    expect(res.error).toBeUndefined()
    expect(auditEvent).toHaveBeenCalledWith(
      expect.objectContaining({
        kind: 'tool_result',
        tool: 'extract_and_save',
        status: 'ok'
      })
    )
  })
})

describe('previewDetails', () => {
  it('includes block count header and truncation footer when long', () => {
    const short = previewDetails(['hello'])
    expect(short).toMatch(/^1 block to write:/)
    expect(short).toContain('hello')

    const longBody = 'x'.repeat(2_000)
    const long = previewDetails([longBody, longBody])
    expect(long).toMatch(/^2 blocks to write:/)
    expect(long).toMatch(/preview truncated \(2 blocks total\)/)
  })
})

describe('targetPageExists', () => {
  it('uses blocks probe (not files notebook columns) and page_types fallback', async () => {
    const calls: string[] = []
    const ctx = {
      sqliteQuery: vi.fn(async (sql: string) => {
        calls.push(sql)
        const lower = sql.toLowerCase()
        // Simulate real schema: files has no notebook column — must not be queried that way.
        if (lower.includes('from files') && lower.includes('notebook')) {
          throw new Error('no such column: notebook')
        }
        if (lower.includes('from blocks')) {
          return { rows: [], truncated: false }
        }
        if (lower.includes('from page_types')) {
          return { rows: [{ ok: 1 }], truncated: false }
        }
        return { rows: [], truncated: false }
      })
    } as unknown as PluginContext

    await expect(
      targetPageExists(ctx, {
        notebook: 'Work',
        section: 'Notes',
        page: 'EmptyTyped'
      })
    ).resolves.toBe(true)
    expect(
      calls.some((s) => /from files/i.test(s) && /notebook/i.test(s))
    ).toBe(false)
    expect(calls.some((s) => /from blocks/i.test(s))).toBe(true)
    expect(calls.some((s) => /from page_types/i.test(s))).toBe(true)
  })

  it('returns false when blocks and page_types are empty', async () => {
    const ctx = {
      sqliteQuery: vi.fn(async () => ({ rows: [], truncated: false }))
    } as unknown as PluginContext
    await expect(
      targetPageExists(ctx, { notebook: 'W', section: '', page: 'New' })
    ).resolves.toBe(false)
  })

  it('fails closed (true) when blocks query throws', async () => {
    const ctx = {
      sqliteQuery: vi.fn(async () => {
        throw new Error('db locked')
      })
    } as unknown as PluginContext
    await expect(
      targetPageExists(ctx, { notebook: 'W', section: '', page: 'P' })
    ).resolves.toBe(true)
  })
})

describe('parseExtraction', () => {
  it('handles ```json fenced responses', () => {
    const parsed = parseExtraction('```json\n{"summary":"hi"}\n```', 'summary')
    expect(parsed.error).toBeUndefined()
    expect(parsed.blocks).toEqual(['hi'])
  })

  it('returns error on invalid JSON', () => {
    const parsed = parseExtraction('not json at all', 'summary')
    expect(parsed.blocks).toEqual([])
    expect(parsed.error).toMatch(/not JSON/i)
  })

  it('drops items missing required fields', () => {
    const parsed = parseExtraction(
      JSON.stringify({
        items: [
          { front: 'ok', back: 'yes' },
          { front: '', back: 'no front' },
          { front: 'no back' }
        ]
      }),
      'flashcards'
    )
    expect(parsed.blocks).toHaveLength(1)
  })

  it('caps extracted item count and field length', () => {
    const parsed = parseExtraction(
      JSON.stringify({
        items: Array.from({ length: 60 }, () => ({
          front: 'f'.repeat(3_000),
          back: 'b'.repeat(3_000)
        }))
      }),
      'flashcards'
    )
    expect(parsed.blocks).toHaveLength(50)
    expect(parsed.blocks.every((block) => block.length < 4_100)).toBe(true)
  })
})
