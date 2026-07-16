import { describe, expect, it, vi } from 'vitest'
import { ftsRowsToHits, hybridRetrieve, RetrieveError } from './retrieve'
import type { PluginContext } from '../../sdk'
import { DEFAULT_SETTINGS } from './settings'

describe('ftsRowsToHits', () => {
  it('strips mark tags from snippets', () => {
    const hits = ftsRowsToHits([
      {
        id: 'b1',
        notebook: 'N',
        section: 'S',
        page: 'P',
        snippet: 'hello <mark>world</mark>'
      }
    ])
    expect(hits[0].text).toBe('hello world')
    expect(hits[0].blockId).toBe('b1')
  })
})

describe('hybridRetrieve', () => {
  it('reports degrade when vector fails but FTS has hits', async () => {
    const onDegraded = vi.fn()
    // Exercise the shared pipeline with an injected vectorSearch that rejects
    // (QA's own vectorSearch often soft-fails to [] without setting vecErr).
    const { hybridRetrieve: sharedHybridRetrieve } =
      await import('../../shared/retrieval/retrieve')
    const ctx = {
      fullTextSearch: vi.fn(async () => ({
        rows: [
          {
            id: 'b1',
            notebook: 'N',
            section: 'S',
            page: 'P',
            clean_content: 'billing migration'
          }
        ]
      })),
      ai: { embed: vi.fn() }
    } as unknown as PluginContext

    const passages = await sharedHybridRetrieve(
      ctx,
      'billing',
      {
        hybrid_weight: 0.5,
        top_k: 5,
        min_score: 0,
        max_context_chars: 24000,
        rerank_enabled: false,
        onDegraded
      },
      async () => {
        throw new Error('vec index down')
      }
    )
    expect(passages.length).toBeGreaterThan(0)
    expect(onDegraded).toHaveBeenCalledWith(
      expect.objectContaining({ side: 'vector' })
    )
  })

  it('throws RetrieveError when both sides fail', async () => {
    const ctx = {
      fullTextSearch: vi.fn(async () => {
        throw new Error('fts down')
      }),
      ai: {
        embed: vi.fn(async () => {
          throw new Error('embed down')
        })
      },
      pluginDb: {
        migrate: vi.fn(async () => {}),
        query: vi.fn(async () => ({ rows: [] })),
        exec: vi.fn(async () => {})
      }
    } as unknown as PluginContext

    // vectorSearch will call migrate + meta + embed — force failure via embed
    await expect(
      hybridRetrieve(ctx, 'billing', DEFAULT_SETTINGS)
    ).rejects.toBeInstanceOf(RetrieveError)
  })

  it('returns fused hits when both sides succeed', async () => {
    const ctx = {
      fullTextSearch: vi.fn(async () => ({
        rows: [
          {
            id: 'b1',
            notebook: 'N',
            section: 'S',
            page: 'P',
            clean_content: 'billing migration'
          }
        ]
      })),
      ai: {
        embed: vi.fn(async () => ({
          embeddings: [[0.1, 0.2]],
          model: 'm',
          dimensions: 2
        }))
      },
      pluginDb: {
        migrate: vi.fn(async () => {}),
        query: vi.fn(async (sql: string) => {
          if (sql.includes('index_meta')) {
            return { rows: [{ value: '2' }] }
          }
          if (sql.includes('embeddings')) {
            return {
              rows: [
                {
                  block_id: 'b2',
                  notebook: 'N',
                  section: 'S',
                  page: 'P',
                  line_number: 1,
                  text: 'semantic hit',
                  distance: 0.1
                }
              ]
            }
          }
          if (sql.includes('COUNT')) {
            return { rows: [{ n: 1 }] }
          }
          return { rows: [] }
        }),
        exec: vi.fn(async () => {})
      }
    } as unknown as PluginContext

    const passages = await hybridRetrieve(ctx, 'billing', {
      ...DEFAULT_SETTINGS,
      hybrid_weight: 0.5,
      top_k: 5
    })
    expect(passages.length).toBeGreaterThan(0)
    const ids = passages.map((p) => p.blockId)
    expect(ids).toContain('b1')
  })

  it('reranks by cosine similarity when enabled', async () => {
    const embed = vi.fn(async ({ texts }: { texts: string[] }) => {
      // Query embed: unit vector on x. Passage embeds: b-low closer than b-high.
      if (texts.length === 1 && texts[0] === 'q') {
        return { embeddings: [[1, 0]], model: 'm', dimensions: 2 }
      }
      return {
        embeddings: texts.map((t) =>
          t.includes('orthogonal') ? [0.1, 0.9] : [0.99, 0.01]
        ),
        model: 'm',
        dimensions: 2
      }
    })
    const ctx = {
      fullTextSearch: vi.fn(async () => ({
        rows: [
          {
            id: 'b-high',
            notebook: 'N',
            section: 'S',
            page: 'P',
            clean_content: 'orthogonal rrf first'
          },
          {
            id: 'b-low',
            notebook: 'N',
            section: 'S',
            page: 'P',
            clean_content: 'aligned rrf second'
          }
        ]
      })),
      ai: { embed },
      pluginDb: {
        migrate: vi.fn(async () => {}),
        query: vi.fn(async (sql: string) => {
          if (sql.includes('index_meta')) return { rows: [{ value: '2' }] }
          if (sql.includes('embeddings')) return { rows: [] }
          return { rows: [] }
        }),
        exec: vi.fn(async () => {})
      }
    } as unknown as PluginContext

    const passages = await hybridRetrieve(ctx, 'q', {
      ...DEFAULT_SETTINGS,
      hybrid_weight: 0,
      top_k: 2,
      rerank_enabled: true
    })
    expect(passages[0].blockId).toBe('b-low')
  })

  it('falls back to RRF order when rerank embed fails', async () => {
    let call = 0
    const ctx = {
      fullTextSearch: vi.fn(async () => ({
        rows: [
          {
            id: 'b1',
            notebook: 'N',
            section: 'S',
            page: 'P',
            clean_content: 'first'
          }
        ]
      })),
      ai: {
        embed: vi.fn(async () => {
          call++
          // vectorSearch query embed succeeds once; rerank query fails.
          if (call === 1) {
            return { embeddings: [[0.1, 0.2]], model: 'm', dimensions: 2 }
          }
          throw new Error('rerank embed failed')
        })
      },
      pluginDb: {
        migrate: vi.fn(async () => {}),
        query: vi.fn(async (sql: string) => {
          if (sql.includes('index_meta')) return { rows: [{ value: '2' }] }
          return { rows: [] }
        }),
        exec: vi.fn(async () => {})
      }
    } as unknown as PluginContext

    const passages = await hybridRetrieve(ctx, 'q', {
      ...DEFAULT_SETTINGS,
      hybrid_weight: 0,
      top_k: 1,
      rerank_enabled: true
    })
    expect(passages[0].blockId).toBe('b1')
  })

  it('reuses the query embedding for both vector search and rerank', async () => {
    const embed = vi.fn(async ({ texts }: { texts: string[] }) => {
      if (texts.length === 1) {
        return { embeddings: [[1, 0]], model: 'm', dimensions: 2 }
      }
      return {
        embeddings: texts.map(() => [0.9, 0.1]),
        model: 'm',
        dimensions: 2
      }
    })
    const ctx = {
      fullTextSearch: vi.fn(async () => ({
        rows: [
          {
            id: 'b1',
            notebook: 'N',
            section: 'S',
            page: 'P',
            clean_content: 'a note'
          }
        ]
      })),
      ai: { embed },
      pluginDb: {
        migrate: vi.fn(async () => {}),
        query: vi.fn(async (sql: string) => {
          if (sql.includes('index_meta')) return { rows: [{ value: '2' }] }
          return { rows: [] }
        }),
        exec: vi.fn(async () => {})
      }
    } as unknown as PluginContext

    await hybridRetrieve(ctx, 'q', {
      ...DEFAULT_SETTINGS,
      hybrid_weight: 0,
      top_k: 1,
      rerank_enabled: true
    })

    // RETRIEVAL_QUERY embed calls should be exactly 1 (pre-computed once
    // and reused by both vectorSearch and rerankPassages — no duplicate).
    const queryEmbeds = embed.mock.calls.filter(
      (c: any[]) => c[0]?.taskType === 'RETRIEVAL_QUERY'
    )
    expect(queryEmbeds).toHaveLength(1)
  })
})
