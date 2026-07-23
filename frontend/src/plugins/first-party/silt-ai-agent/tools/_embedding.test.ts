import { describe, expect, it, vi } from 'vitest'
import type { PluginContext } from '../../../sdk'
import { embedOne, hashOf, rankCandidates } from './_embedding'

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
