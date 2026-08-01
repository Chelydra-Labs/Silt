import { describe, expect, it, vi } from 'vitest'
import type { PluginContext } from '../../../sdk'
import { PLUGIN_FULL_TEXT_SEARCH_SQL } from '../../../ftsQuery'
import {
  embedOne,
  extractKeywords,
  gatherCandidates,
  hashOf,
  rankCandidates
} from './_embedding'

const candidate = {
  id: 'candidate',
  clean_content: 'candidate note',
  notebook: 'N',
  section: 'S',
  page: 'P'
}

function makeCtx(opts: { cachedVector: number[]; cachedModel: string }): {
  ctx: PluginContext
  embed: ReturnType<typeof vi.fn>
} {
  const model = 'model-a'
  const embed = vi.fn(async (req: { texts: string[]; taskType?: string }) => ({
    embeddings: req.texts.map(() => [1, 0]),
    model,
    dimensions: 2
  }))
  const ctx = {
    ai: { embed },
    pluginDb: {
      query: vi.fn(async () => ({
        rows: [
          {
            block_id: candidate.id,
            content_hash: hashOf(candidate.clean_content),
            provider: 'local',
            model: opts.cachedModel,
            dimensions: 2,
            task_type: 'RETRIEVAL_DOCUMENT',
            vector: JSON.stringify(opts.cachedVector)
          }
        ],
        truncated: false
      })),
      exec: vi.fn(async () => undefined),
      migrate: vi.fn()
    }
  } as unknown as PluginContext
  return { ctx, embed }
}

describe('embedding cache identity', () => {
  it('misses the cache when the embedding model changes', async () => {
    const { ctx, embed } = makeCtx({
      cachedVector: [1, 0],
      cachedModel: 'model-a'
    })
    const queryA = await embedOne(ctx, 'source', 'RETRIEVAL_QUERY')
    await rankCandidates(ctx, queryA, [candidate], { minScore: 0 })

    // The same block is now queried under a different model. The model-a row
    // must not be accepted as a cache hit.
    ;(ctx.ai.embed as ReturnType<typeof vi.fn>).mockImplementation(
      (req: { texts: string[]; taskType?: string }) => ({
        embeddings: req.texts.map(() => [1, 0]),
        model: 'model-b',
        dimensions: 2
      })
    )
    const queryB = await embedOne(ctx, 'source', 'RETRIEVAL_QUERY')
    await rankCandidates(ctx, queryB, [candidate], { minScore: 0 })

    // The first model uses its cached document vector; switching models causes
    // a fresh document embedding instead of reusing model-a's vector.
    expect(embed).toHaveBeenCalledTimes(3)
  })

  it('discards malformed and short cached vectors', async () => {
    const { ctx, embed } = makeCtx({
      cachedVector: [1],
      cachedModel: 'model-a'
    })
    const query = await embedOne(ctx, 'source', 'RETRIEVAL_QUERY')
    await rankCandidates(ctx, query, [candidate], { minScore: 0 })
    expect(embed).toHaveBeenCalledTimes(2)
  })
})

describe('gatherCandidates FTS OR recall', () => {
  it('issues an OR MATCH via sqliteQuery, not fullTextSearch sanitization', async () => {
    const source = 'database durability'
    const expectedMatch = 'database* OR durability*'
    expect(extractKeywords(source)).toEqual(['database', 'durability'])

    const sqliteQuery = vi.fn(async (sql: string, params?: unknown[]) => {
      if (sql.includes('ORDER BY line_number')) {
        return { rows: [], truncated: false }
      }
      if (sql === PLUGIN_FULL_TEXT_SEARCH_SQL) {
        expect(params?.[0]).toBe(expectedMatch)
        return {
          rows: [
            {
              id: 'fts1',
              clean_content: 'Postgres durability notes',
              notebook: 'Work',
              section: 'Notes',
              page: 'DB'
            }
          ],
          truncated: false
        }
      }
      return { rows: [], truncated: false }
    })
    const fullTextSearch = vi.fn(async () => ({
      rows: [],
      truncated: false
    }))
    const ctx = {
      sqliteQuery,
      fullTextSearch
    } as unknown as PluginContext

    const got = await gatherCandidates(ctx, new Set(), source)
    expect(got.map((c) => c.id)).toContain('fts1')
    expect(fullTextSearch).not.toHaveBeenCalled()
    expect(sqliteQuery).toHaveBeenCalledWith(PLUGIN_FULL_TEXT_SEARCH_SQL, [
      expectedMatch
    ])
  })
})
