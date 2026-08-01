import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { PluginContext } from '../../../sdk'
import { clearTools, dispatchTool, registerTool } from '../tool-registry'
import { searchNotesToolDef, handleSearchNotes } from './search_notes'

function mockEmbed(queryVec: number[], passageVecs: number[][]) {
  let passageIdx = 0
  return vi.fn(async (req: { texts: string[]; taskType?: string }) => {
    // Query embeds may run more than once (hybrid rerank + semantic fallback).
    if (req.taskType === 'RETRIEVAL_QUERY') {
      return {
        embeddings: req.texts.map(() => queryVec),
        model: 'm',
        dimensions: 2
      }
    }
    // RETRIEVAL_DOCUMENT batches.
    const out = req.texts.map(() => {
      const v = passageVecs[passageIdx] ?? queryVec
      passageIdx++
      return v
    })
    return { embeddings: out, model: 'm', dimensions: 2 }
  })
}

function makeCtx(opts: {
  ftsRows: Record<string, unknown>[]
  embed: ReturnType<typeof vi.fn>
  typeRows?: Record<string, unknown>[]
}): PluginContext {
  return {
    fullTextSearch: vi.fn(async () => ({
      rows: opts.ftsRows,
      truncated: false
    })),
    ai: { embed: opts.embed },
    sqliteQuery: vi.fn(async (sql: string, params?: unknown[]) => {
      if (sql.includes('type FROM blocks')) {
        const ids = (params ?? []) as string[]
        const byId = new Map(
          (opts.typeRows ?? []).map((r) => [String(r.id), r])
        )
        return {
          rows: ids.filter((id) => byId.has(id)).map((id) => byId.get(id)!),
          truncated: false
        }
      }
      return { rows: [], truncated: false }
    })
  } as unknown as PluginContext
}

beforeEach(() => clearTools())
afterEach(() => clearTools())

describe('search_notes', () => {
  it('formats fused results with block id, location, snippet, score', async () => {
    const ctx = makeCtx({
      ftsRows: [
        {
          id: 'b1',
          notebook: 'Work',
          section: 'Notes',
          page: 'Decisions',
          clean_content: 'We chose Postgres for durability.'
        }
      ],
      // Query and passage both point the same way → rerank keeps it.
      embed: mockEmbed([1, 0], [[0.9, 0.1]])
    })

    const res = await handleSearchNotes(ctx, { query: 'database choice' })
    expect(res.error).toBeUndefined()
    expect(res.content).toContain('b1')
    expect(res.content).toContain('Work > Notes > Decisions')
    expect(res.content).toContain('Postgres')
    expect(res.content).toContain('score:')
  })

  it('honours top_k default and rejects empty query', async () => {
    const ctx = makeCtx({ ftsRows: [], embed: mockEmbed([1, 0], [[0, 0]]) })
    const empty = await handleSearchNotes(ctx, { query: '' })
    expect(empty.error).toMatch(/empty/)

    const ok = await handleSearchNotes(ctx, { query: 'x', top_k: 1 })
    expect(ok.error).toBeUndefined()
  })

  it('narrows by notebook and section filters', async () => {
    const ctx = makeCtx({
      ftsRows: [
        {
          id: 'b1',
          notebook: 'Work',
          section: 'Notes',
          page: 'P',
          clean_content: 'work note'
        },
        {
          id: 'b2',
          notebook: 'Personal',
          section: 'Diary',
          page: 'P',
          clean_content: 'personal note'
        }
      ],
      embed: mockEmbed(
        [1, 0],
        [
          [0.9, 0.1],
          [0.1, 0.9]
        ]
      )
    })

    const res = await handleSearchNotes(ctx, {
      query: 'note',
      filters: { notebook: 'Work' }
    })
    expect(res.content).toContain('b1')
    expect(res.content).not.toContain('b2')
  })

  it('filters by block type via sqliteQuery', async () => {
    const ctx = makeCtx({
      ftsRows: [
        {
          id: 'b1',
          notebook: 'N',
          section: 'S',
          page: 'P',
          clean_content: 'a note'
        },
        {
          id: 'b2',
          notebook: 'N',
          section: 'S',
          page: 'P',
          clean_content: 'a task'
        }
      ],
      typeRows: [
        { id: 'b1', type: 'NOTE' },
        { id: 'b2', type: 'TASK' }
      ],
      embed: mockEmbed(
        [1, 0],
        [
          [0.9, 0.1],
          [0.9, 0.1]
        ]
      )
    })

    const res = await handleSearchNotes(ctx, {
      query: 'x',
      filters: { type: 'task' }
    })
    expect(res.content).toContain('b2')
    expect(res.content).not.toContain('b1')
  })

  it('does not embed out-of-scope passages when a notebook filter is set', async () => {
    const embed = mockEmbed([1, 0], [[0.9, 0.1]])
    const ctx = makeCtx({
      ftsRows: [
        {
          id: 'b1',
          notebook: 'Work',
          section: 'S',
          page: 'P',
          clean_content: 'work secret'
        },
        {
          id: 'b2',
          notebook: 'Personal',
          section: 'D',
          page: 'P',
          clean_content: 'personal diary'
        }
      ],
      embed
    })
    await handleSearchNotes(ctx, {
      query: 'note',
      filters: { notebook: 'Work' }
    })
    const embeddedDocs = embed.mock.calls
      .filter(
        (c) =>
          (c[0] as { taskType?: string })?.taskType === 'RETRIEVAL_DOCUMENT'
      )
      .flatMap((c) => (c[0] as { texts?: string[] })?.texts ?? [])
    expect(embeddedDocs).toContain('work secret')
    expect(embeddedDocs).not.toContain('personal diary')
  })

  it('returns a clean no-results message when nothing matches', async () => {
    const ctx = makeCtx({ ftsRows: [], embed: mockEmbed([1, 0], []) })
    const res = await handleSearchNotes(ctx, { query: 'nothing' })
    expect(res.content).toMatch(/no matching notes/i)
  })

  it('falls back to semantic ranking when FTS is empty', async () => {
    const auditEvent = vi.fn()
    const embed = mockEmbed([1, 0], [[0.95, 0.05]])
    const ctx = {
      fullTextSearch: vi.fn(async () => ({ rows: [], truncated: false })),
      ai: { embed, auditEvent },
      sqliteQuery: vi.fn(async (sql: string) => {
        if (
          sql.includes('FROM blocks') &&
          sql.includes('ORDER BY line_number')
        ) {
          return {
            rows: [
              {
                id: 'sem1',
                clean_content:
                  'semantically related vault content about databases',
                notebook: 'Work',
                section: 'Notes',
                page: 'DB'
              }
            ],
            truncated: false
          }
        }
        return { rows: [], truncated: false }
      }),
      pluginDb: {
        migrate: vi.fn(async () => {}),
        query: vi.fn(async () => ({ rows: [] })),
        exec: vi.fn(async () => {})
      }
    } as unknown as PluginContext

    const res = await handleSearchNotes(ctx, { query: 'database durability' })
    expect(res.error).toBeUndefined()
    expect(res.content).toContain('sem1')
    expect(res.content).toMatch(/semantic fallback/i)
    expect(auditEvent).toHaveBeenCalled()
  })

  it('caps semantic-fallback passage text so long notes stay in budget', async () => {
    const longBody = `${'database durability word '.repeat(80)}END`
    expect(longBody.length).toBeGreaterThan(500)
    const embed = mockEmbed([1, 0], [[0.99, 0.01]])
    const ctx = {
      fullTextSearch: vi.fn(async () => ({ rows: [], truncated: false })),
      ai: { embed, auditEvent: vi.fn() },
      sqliteQuery: vi.fn(async (sql: string) => {
        if (
          sql.includes('FROM blocks') &&
          sql.includes('ORDER BY line_number')
        ) {
          return {
            rows: [
              {
                id: 'long1',
                clean_content: longBody,
                notebook: 'Work',
                section: 'Notes',
                page: 'Long'
              }
            ],
            truncated: false
          }
        }
        return { rows: [], truncated: false }
      }),
      pluginDb: {
        migrate: vi.fn(async () => {}),
        query: vi.fn(async () => ({ rows: [] })),
        exec: vi.fn(async () => {})
      }
    } as unknown as PluginContext

    const res = await handleSearchNotes(ctx, { query: 'database durability' })
    expect(res.error).toBeUndefined()
    expect(res.content).toContain('long1')
    // Full body must not appear; per-passage fallback cap is 500 chars.
    expect(res.content).not.toContain('END')
    expect(res.content).toMatch(/…/)
  })

  it('degrades with audit when semantic fallback embed fails', async () => {
    // embedOne swallows provider errors → empty vector → semanticFallback
    // throws so the outer path can emit search_degraded (not silent no-results).
    const auditEvent = vi.fn()
    const ctx = {
      fullTextSearch: vi.fn(async () => ({ rows: [], truncated: false })),
      ai: {
        embed: vi.fn(async () => {
          throw new Error('embed provider down')
        }),
        auditEvent
      },
      sqliteQuery: vi.fn(async () => ({ rows: [], truncated: false })),
      pluginDb: {
        migrate: vi.fn(async () => {}),
        query: vi.fn(async () => ({ rows: [] })),
        exec: vi.fn(async () => {})
      }
    } as unknown as PluginContext

    const res = await handleSearchNotes(ctx, { query: 'database durability' })
    expect(res.error).toBeUndefined()
    expect(res.content).toMatch(/no matching notes/i)
    expect(res.content).toMatch(/semantic fallback unavailable/i)
    expect(auditEvent).toHaveBeenCalledWith(
      expect.objectContaining({
        kind: 'search_degraded',
        tool: 'search_notes',
        side: 'vector',
        status: 'degraded'
      })
    )
  })

  it('degrades to no-results when semantic fallback throws', async () => {
    const auditEvent = vi.fn()
    const ctx = {
      fullTextSearch: vi.fn(async () => ({ rows: [], truncated: false })),
      ai: {
        embed: vi.fn(async () => ({
          embeddings: [[1, 0]],
          model: 'm',
          dimensions: 2
        })),
        auditEvent
      },
      // gatherCandidates recent-pool query throws → fallback catch path.
      sqliteQuery: vi.fn(async () => {
        throw new Error('sqlite unavailable')
      }),
      pluginDb: {
        migrate: vi.fn(async () => {}),
        query: vi.fn(async () => ({ rows: [] })),
        exec: vi.fn(async () => {})
      }
    } as unknown as PluginContext

    const res = await handleSearchNotes(ctx, { query: 'database durability' })
    expect(res.error).toBeUndefined()
    expect(res.content).toMatch(/no matching notes/i)
    expect(res.content).toMatch(/semantic fallback unavailable/i)
    expect(auditEvent).toHaveBeenCalledWith(
      expect.objectContaining({
        kind: 'search_degraded',
        tool: 'search_notes',
        side: 'vector',
        status: 'degraded'
      })
    )
  })

  it('dispatches via the tool registry', async () => {
    registerTool({ ...searchNotesToolDef, handler: handleSearchNotes })
    const ctx = makeCtx({
      ftsRows: [
        {
          id: 'b9',
          notebook: 'N',
          section: 'S',
          page: 'P',
          clean_content: 'hi'
        }
      ],
      embed: mockEmbed([1, 0], [[0.9, 0.1]])
    })
    const res = await dispatchTool(ctx, 'search_notes', { query: 'hi' })
    expect(res.error).toBeUndefined()
    expect(res.content).toContain('b9')
  })
})
