import { beforeEach, describe, expect, it, vi } from 'vitest'
import {
  needsFullRebuildForModel,
  resetIndexState,
  getIndexInfo
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
