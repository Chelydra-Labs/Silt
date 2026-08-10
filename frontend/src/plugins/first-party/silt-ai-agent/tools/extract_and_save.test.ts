import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { PluginContext } from '../../../sdk'
import { clearTools } from '../tool-registry'
import {
  extractAndSaveToolDef,
  handleExtractAndSave,
  parseExtraction
} from './extract_and_save'

interface SourceBlock {
  id: string
  clean_content: string
}

/**
 * Build a ctx whose sqliteQuery returns the canned source blocks for the
 * `WHERE id IN (...)` lookup, and whose ai.complete returns the configured
 * JSON content. createPage/createBlock are stubbed with sequenced ids so the
 * returned block list is deterministic.
 */
function makeCtx(opts: {
  sources: SourceBlock[]
  completeContent?: string
  completeError?: Error
  auditEvent?: ReturnType<typeof vi.fn>
}): {
  ctx: PluginContext
  complete: ReturnType<typeof vi.fn>
  createPage: ReturnType<typeof vi.fn>
  createBlock: ReturnType<typeof vi.fn>
  mutateBlock: ReturnType<typeof vi.fn>
  sqliteCalls: { sql: string; params: unknown[] }[]
} {
  const srcById = new Map(opts.sources.map((s) => [s.id, s]))
  const sqliteCalls: { sql: string; params: unknown[] }[] = []
  const sqliteQuery = vi.fn(async (sql: string, params?: unknown[]) => {
    sqliteCalls.push({ sql, params: [...(params ?? [])] })
    if (sql.toLowerCase().includes('where id in (')) {
      // The bound params are the requested ids; return matching sources.
      const ids = (params ?? []) as string[]
      const rows = ids
        .map((id) => srcById.get(id))
        .filter((b): b is SourceBlock => b !== undefined)
        .map((b) => ({ id: b.id, clean_content: b.clean_content }))
      return { rows, truncated: false }
    }
    return { rows: [], truncated: false }
  })

  const complete = vi.fn(async () => {
    if (opts.completeError) throw opts.completeError
    return { content: opts.completeContent ?? '', model: 'm' }
  })

  let blockSeq = 0
  const createPage = vi.fn(async () => 'page-uuid')
  const createBlock = vi.fn(async (_req: { type: string; text: string }) => {
    blockSeq += 1
    return `blk-${blockSeq}`
  })
  // mutateBlock would touch a source — must never be called.
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
    mutateBlock
  } as unknown as PluginContext
  return { ctx, complete, createPage, createBlock, mutateBlock, sqliteCalls }
}

beforeEach(() => clearTools())
afterEach(() => clearTools())

const SOURCES = [
  { id: 'src-1', clean_content: 'Postgres uses MVCC.' },
  { id: 'src-2', clean_content: 'Redis is single-threaded.' }
]
const TARGET = { notebook: 'Work', section: 'Notes', page: 'Distilled' }

describe('extract_and_save', () => {
  it('summary mode: parses JSON, writes one NOTE with citation, no source mutation', async () => {
    const { ctx, createPage, createBlock, mutateBlock } = makeCtx({
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
    expect(createPage).toHaveBeenCalledWith('Work', 'Notes', 'Distilled')
    expect(createBlock).toHaveBeenCalledTimes(1)
    const block = createBlock.mock.calls[0][0] as {
      type: string
      text: string
      notebook: string
      section: string
      page: string
    }
    expect(block.type).toBe('NOTE')
    expect(block.notebook).toBe('Work')
    expect(block.section).toBe('Notes')
    expect(block.page).toBe('Distilled')
    expect(block.text).toMatch(/Postgres uses MVCC/)
    // Citation comment carries source ids + mode.
    expect(block.text).toMatch(/<!-- src: src-1,src-2 mode:summary -->/)
    // Source blocks were never mutated.
    expect(mutateBlock).not.toHaveBeenCalled()
    // Returned content names the page + block id.
    expect(res.content).toContain('Work/Notes/Distilled')
    expect(res.content).toContain('blk-1')
  })

  it('flashcards mode: writes one NOTE per item with Q-front / A-back', async () => {
    const { ctx, createBlock } = makeCtx({
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
    expect(res.error).toBeUndefined()
    expect(createBlock).toHaveBeenCalledTimes(2)
    const text0 = (createBlock.mock.calls[0][0] as { text: string }).text
    const text1 = (createBlock.mock.calls[1][0] as { text: string }).text
    expect(text0).toMatch(/Q1: What concurrency model does Postgres use\?/)
    expect(text0).toMatch(/MVCC\./)
    expect(text1).toMatch(/Q2: Is Redis multi-threaded\?/)
    expect(text1).toMatch(/single-threaded/)
    expect(res.content).toContain('blk-1')
    expect(res.content).toContain('blk-2')
  })

  it('qa_pairs mode: writes one NOTE per Q/A pair', async () => {
    const { ctx, createBlock } = makeCtx({
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
    expect(createBlock).toHaveBeenCalledTimes(2)
    const text0 = (createBlock.mock.calls[0][0] as { text: string }).text
    expect(text0).toMatch(/Q1: Postgres MVCC\?/)
    expect(text0).toMatch(/Multi-version concurrency/)
  })

  it('action_items mode: renders GFM checkboxes with optional [due:: YYYY-MM-DD]', async () => {
    const { ctx, createBlock } = makeCtx({
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
    expect(createBlock).toHaveBeenCalledTimes(2)
    const text0 = (createBlock.mock.calls[0][0] as { text: string }).text
    const text1 = (createBlock.mock.calls[1][0] as { text: string }).text
    expect(text0).toMatch(/- \[ \] Set up Postgres replication/)
    expect(text0).toMatch(/\[due:: 2026-08-01\]/)
    expect(text1).toMatch(/- \[ \] Document Redis failover/)
    expect(text1).not.toMatch(/\[due::/)
  })

  it('passes responseSchema + a citation prompt to ctx.ai.complete', async () => {
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
    // System + user message present.
    expect(req.messages.length).toBeGreaterThanOrEqual(2)
    expect(req.messages[0].role).toBe('system')
    // responseSchema is the summary shape.
    expect(req.responseSchema.properties.summary).toBeDefined()
    // Low temperature for deterministic extraction.
    expect(req.temperature).toBeLessThan(0.5)
    // Source content + ids are cited in the user message.
    const userMsg = req.messages.find((m) => m.role === 'user')?.content ?? ''
    expect(userMsg).toContain('src-1')
    expect(userMsg).toContain('Postgres uses MVCC')
  })

  it('prepends the shared SECURITY preamble to the nested system prompt (#633)', async () => {
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
    // The nested complete processes the same untrusted vault text as the agent
    // loop, so the shared SECURITY framing must carry system-prompt priority
    // (not only the user-message caveat).
    expect(sys).toContain('SECURITY:')
    expect(sys).toContain('untrusted DATA')
    // Untrusted source text stays hard-delimited as DATA in the user message.
    const user = req.messages.find((m) => m.role === 'user')?.content ?? ''
    expect(user).toContain('<vault_data tool="extract_and_save">')
    expect(user).toContain('</vault_data>')
  })

  it('returns error and writes nothing when the model returns non-JSON', async () => {
    const { ctx, createBlock, createPage } = makeCtx({
      sources: SOURCES,
      completeContent: 'Sorry, I could not extract anything.'
    })
    const res = await handleExtractAndSave(ctx, {
      source_block_ids: ['src-1'],
      mode: 'flashcards',
      target: TARGET
    })
    expect(res.error).toMatch(/not JSON|did not match/i)
    expect(createBlock).not.toHaveBeenCalled()
    expect(createPage).not.toHaveBeenCalled()
  })

  it('returns error and writes nothing when ctx.ai.complete throws', async () => {
    const { ctx, createBlock, createPage } = makeCtx({
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
    expect(createBlock).not.toHaveBeenCalled()
    expect(createPage).not.toHaveBeenCalled()
  })

  it('aborts before write when signal is already aborted', async () => {
    const { ctx, createBlock, createPage } = makeCtx({
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
    expect(createBlock).not.toHaveBeenCalled()
    expect(createPage).not.toHaveBeenCalled()
  })

  it('reads source blocks via parameterized IN (no interpolation)', async () => {
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
    // Two placeholders for two ids, values bound (not interpolated).
    const placeholderCount = (read!.sql.match(/\?/g) ?? []).length
    expect(placeholderCount).toBe(2)
    expect(read!.params).toEqual(['src-1', 'src-2'])
    // No quoted literal in the SQL text.
    expect(read!.sql).not.toMatch(/'src-1'/)
  })

  it('rejects too many source ids (>20)', async () => {
    const { ctx } = makeCtx({ sources: SOURCES })
    const res = await handleExtractAndSave(ctx, {
      source_block_ids: Array.from({ length: 21 }, (_, i) => `b${i}`),
      mode: 'summary',
      target: TARGET
    })
    expect(res.error).toMatch(/exceeds the 20-id limit/)
  })

  it('rejects an unknown mode', async () => {
    const { ctx } = makeCtx({ sources: SOURCES })
    const res = await handleExtractAndSave(ctx, {
      source_block_ids: ['src-1'],
      mode: 'translation',
      target: TARGET
    })
    expect(res.error).toMatch(/mode must be one of/)
  })

  it('rejects an empty target page', async () => {
    const { ctx } = makeCtx({ sources: SOURCES })
    const res = await handleExtractAndSave(ctx, {
      source_block_ids: ['src-1'],
      mode: 'summary',
      target: { notebook: 'Work', page: '' }
    })
    expect(res.error).toMatch(/target\.page/)
  })

  it('parseExtraction handles ```json fenced responses', () => {
    const parsed = parseExtraction('```json\n{"summary":"hi"}\n```', 'summary')
    expect(parsed.error).toBeUndefined()
    expect(parsed.blocks).toEqual(['hi'])
  })

  it('parseExtraction returns error on invalid JSON', () => {
    const parsed = parseExtraction('not json at all', 'summary')
    expect(parsed.blocks).toEqual([])
    expect(parsed.error).toMatch(/not JSON/i)
  })

  it('parseExtraction drops items missing required fields', () => {
    const parsed = parseExtraction(
      JSON.stringify({
        items: [
          { front: 'ok', back: 'yes' },
          { front: '', back: 'no front' }, // dropped
          { front: 'no back' } // dropped
        ]
      }),
      'flashcards'
    )
    expect(parsed.blocks).toHaveLength(1)
    expect(parsed.blocks[0]).toMatch(/ok/)
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
    expect(parsed.blocks[0]).toContain('…[truncated]')
  })

  it('does not write when model output is non-JSON (no salvage)', async () => {
    const { ctx, createBlock, createPage } = makeCtx({
      sources: SOURCES,
      completeContent: 'not json ' + 'x'.repeat(20_000)
    })
    const res = await handleExtractAndSave(ctx, {
      source_block_ids: ['src-1'],
      mode: 'summary',
      target: TARGET
    })
    expect(res.error).toBeTruthy()
    expect(createBlock).not.toHaveBeenCalled()
    expect(createPage).not.toHaveBeenCalled()
  })

  it('emits a tool_result audit event on success', async () => {
    const auditEvent = vi.fn(async (_payload: unknown) => {})
    const { ctx } = makeCtx({
      sources: SOURCES,
      completeContent: JSON.stringify({
        summary: 'Postgres uses MVCC; Redis is single-threaded.'
      }),
      auditEvent
    })
    const res = await handleExtractAndSave(ctx, {
      source_block_ids: ['src-1', 'src-2'],
      mode: 'summary',
      target: TARGET
    })
    expect(res.error).toBeUndefined()
    expect(auditEvent).toHaveBeenCalledWith(
      expect.objectContaining({
        kind: 'tool_result',
        tool: 'extract_and_save',
        status: 'ok'
      })
    )
    // extract_and_save creates multiple blocks → no single block_id.
    expect(auditEvent.mock.calls[0][0]).not.toHaveProperty('block_id')
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
