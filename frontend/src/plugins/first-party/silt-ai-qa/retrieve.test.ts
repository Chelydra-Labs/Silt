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
})
