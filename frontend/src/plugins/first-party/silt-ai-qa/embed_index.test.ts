import { beforeEach, describe, expect, it, vi } from 'vitest'
import {
  needsFullRebuildForModel,
  resetIndexState,
  getIndexInfo,
  vectorSearch,
  dropPageIndex,
  DEFAULT_MIN_COSINE_SIMILARITY
} from './embed_index'
import type { PluginContext } from '../../sdk'
import { asString } from '../../../lib/asString'

function mockCtx(meta: Record<string, string>, chunkCount = 0): PluginContext {
  return {
    pluginDb: {
      migrate: vi.fn(async () => {}),
      query: vi.fn(async (sql: string, params?: unknown[]) => {
        if (sql.includes('index_meta') && sql.includes('SELECT')) {
          const key = asString(params?.[0])
          const v = meta[key]
          return { rows: v != null ? [{ value: v }] : [] }
        }
        if (sql.includes('COUNT')) {
          return { rows: [{ n: chunkCount }] }
        }
        return { rows: [] }
      }),
      exec: vi.fn(async () => {})
    }
  } as unknown as PluginContext
}

describe('needsFullRebuildForModel', () => {
  beforeEach(() => resetIndexState())

  it('true when index empty', async () => {
    const ctx = mockCtx({ model: 'm', dimensions: '8' }, 0)
    expect(await needsFullRebuildForModel(ctx, 'm')).toBe(true)
  })

  it('true when model changed', async () => {
    const ctx = mockCtx({ model: 'old-model', dimensions: '8' }, 10)
    expect(await needsFullRebuildForModel(ctx, 'new-model')).toBe(true)
  })

  it('false when model matches and chunks exist', async () => {
    const ctx = mockCtx({ model: 'same', dimensions: '8' }, 10)
    expect(await needsFullRebuildForModel(ctx, 'same')).toBe(false)
  })
})

describe('getIndexInfo', () => {
  beforeEach(() => resetIndexState())

  it('reads durable meta', async () => {
    const ctx = mockCtx({ model: 'nomic', dimensions: '768' }, 42)
    const info = await getIndexInfo(ctx)
    expect(info).toEqual({
      model: 'nomic',
      dimensions: 768,
      chunkCount: 42
    })
  })
})

describe('dropPageIndex', () => {
  beforeEach(() => resetIndexState())

  it('deletes chunks and embeddings for the page', async () => {
    const exec = vi.fn(async () => {})
    const query = vi.fn(async (sql: string) => {
      if (sql.includes('chunk_id FROM chunks')) {
        return { rows: [{ chunk_id: 'c1' }, { chunk_id: 'c2' }] }
      }
      return { rows: [] }
    })
    const ctx = {
      pluginDb: {
        migrate: vi.fn(async () => {}),
        query,
        exec
      }
    } as unknown as PluginContext

    await dropPageIndex(ctx, 'Work', 'Notes', 'Gone')
    expect(exec).toHaveBeenCalledWith(`DELETE FROM chunks WHERE chunk_id = ?`, [
      'c1'
    ])
    expect(exec).toHaveBeenCalledWith(`DELETE FROM chunks WHERE chunk_id = ?`, [
      'c2'
    ])
    // Embeddings delete is unconditional (not gated on embedTableReady).
    expect(exec).toHaveBeenCalledWith(
      `DELETE FROM embeddings WHERE chunk_id = ?`,
      ['c1']
    )
    expect(exec).toHaveBeenCalledWith(
      `DELETE FROM embeddings WHERE chunk_id = ?`,
      ['c2']
    )
  })

  it('still deletes embeddings when embedTableReady is false after restart', async () => {
    resetIndexState()
    const exec = vi.fn(async (sql: string) => {
      if (sql.includes('embeddings')) {
        // Simulate vec0 present even though in-memory flag is cold.
        return
      }
    })
    const ctx = {
      pluginDb: {
        migrate: vi.fn(async () => {}),
        query: vi.fn(async (sql: string) => {
          if (sql.includes('chunk_id FROM chunks')) {
            return { rows: [{ chunk_id: 'orphan' }] }
          }
          return { rows: [] }
        }),
        exec
      }
    } as unknown as PluginContext

    await dropPageIndex(ctx, 'N', 'S', 'P')
    expect(exec).toHaveBeenCalledWith(
      `DELETE FROM embeddings WHERE chunk_id = ?`,
      ['orphan']
    )
  })
})

describe('vectorSearch similarity floor', () => {
  beforeEach(() => resetIndexState())

  it('excludes chunks below the cosine similarity floor', async () => {
    const maxKeep = 1 - DEFAULT_MIN_COSINE_SIMILARITY
    const ctx = {
      pluginDb: {
        migrate: vi.fn(async () => {}),
        query: vi.fn(async (sql: string) => {
          if (sql.includes('index_meta') && sql.includes('SELECT')) {
            return { rows: [{ value: '2' }] }
          }
          if (sql.includes('embeddings')) {
            return {
              rows: [
                {
                  chunk_id: 'c-near',
                  block_id: 'b-near',
                  notebook: 'N',
                  section: 'S',
                  page: 'P',
                  line_number: 1,
                  text: 'relevant',
                  distance: 0.1 // similarity ~0.9
                },
                {
                  chunk_id: 'c-far',
                  block_id: 'b-far',
                  notebook: 'N',
                  section: 'S',
                  page: 'P',
                  line_number: 2,
                  text: 'noise',
                  distance: maxKeep + 0.2 // below floor
                }
              ]
            }
          }
          return { rows: [] }
        }),
        exec: vi.fn(async () => {})
      },
      ai: {
        embed: vi.fn(async () => ({
          embeddings: [[1, 0]],
          model: 'm',
          dimensions: 2
        }))
      }
    } as unknown as PluginContext

    const hits = await vectorSearch(ctx, 'q', 10, [1, 0])
    expect(hits.map((h) => h.blockId)).toEqual(['b-near'])
  })

  it('keeps chunks at or above the floor', async () => {
    const ctx = {
      pluginDb: {
        migrate: vi.fn(async () => {}),
        query: vi.fn(async (sql: string) => {
          if (sql.includes('index_meta') && sql.includes('SELECT')) {
            return { rows: [{ value: '2' }] }
          }
          if (sql.includes('embeddings')) {
            return {
              rows: [
                {
                  chunk_id: 'c1',
                  block_id: 'b1',
                  notebook: 'N',
                  section: 'S',
                  page: 'P',
                  line_number: 1,
                  text: 'hit',
                  distance: 0.5 // similarity 0.5 — on the floor
                }
              ]
            }
          }
          return { rows: [] }
        }),
        exec: vi.fn(async () => {})
      },
      ai: {
        embed: vi.fn(async () => ({
          embeddings: [[1, 0]],
          model: 'm',
          dimensions: 2
        }))
      }
    } as unknown as PluginContext

    const hits = await vectorSearch(ctx, 'q', 5, [1, 0])
    expect(hits).toHaveLength(1)
    expect(hits[0].blockId).toBe('b1')
  })

  it('over-fetches k so the floor can still fill topK', async () => {
    const query = vi.fn(async (sql: string, params?: unknown[]) => {
      if (sql.includes('index_meta') && sql.includes('SELECT')) {
        return { rows: [{ value: '2' }] }
      }
      if (sql.includes('embeddings')) {
        // Second param is k — must be topK * 10.
        expect(params?.[1]).toBe(50)
        return {
          rows: Array.from({ length: 50 }, (_, i) => ({
            chunk_id: `c${i}`,
            block_id: `b${i}`,
            notebook: 'N',
            section: 'S',
            page: 'P',
            line_number: i,
            text: `t${i}`,
            // First 40 below floor, last 10 above.
            distance: i < 40 ? 0.9 : 0.1
          }))
        }
      }
      return { rows: [] }
    })
    const ctx = {
      pluginDb: {
        migrate: vi.fn(async () => {}),
        query,
        exec: vi.fn(async () => {})
      },
      ai: {
        embed: vi.fn(async () => ({
          embeddings: [[1, 0]],
          model: 'm',
          dimensions: 2
        }))
      }
    } as unknown as PluginContext

    const hits = await vectorSearch(ctx, 'q', 5, [1, 0])
    expect(hits).toHaveLength(5)
    expect(hits.every((h) => (h.score ?? 1) <= 0.5)).toBe(true)
  })
})
