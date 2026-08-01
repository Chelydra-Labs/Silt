import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createQAController } from './state.svelte'
import type { PluginContext } from '../../sdk'
import * as embedIndex from './embed_index'

vi.mock('../../../settings/ai-setup', () => ({
  aiProviderNeedsSetup: () => false,
  embeddingProviderNeedsSetup: () => false
}))

vi.mock('../../../settings/store.svelte', () => ({
  settings: {
    config: {
      ai: {
        chat: { provider_type: 'openai', model: 'm' },
        embedding: { provider_type: 'openai', model: 'emb' }
      }
    }
  },
  updatePluginSetting: vi.fn(async () => true)
}))

vi.mock('../../../notifications/store.svelte', () => ({
  pushNotification: vi.fn()
}))

describe('schedulePageIndex coalesce + drop', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    embedIndex.resetIndexState()
  })
  afterEach(() => {
    vi.useRealTimers()
    vi.restoreAllMocks()
  })

  function makeCtx(blockCounts: Record<string, number>): PluginContext {
    return {
      sqliteQuery: vi.fn(async (sql: string, params?: unknown[]) => {
        if (sql.includes('COUNT(*)') && sql.includes('FROM blocks')) {
          const p = params ?? []
          const key = [p[0], p[1], p[2]]
            .map((v) =>
              typeof v === 'string' || typeof v === 'number' ? String(v) : ''
            )
            .join('\0')
          return { rows: [{ n: blockCounts[key] ?? 0 }] }
        }
        if (sql.includes('DISTINCT notebook')) {
          return { rows: [] }
        }
        return { rows: [] }
      }),
      pluginDb: {
        migrate: vi.fn(async () => {}),
        query: vi.fn(async (sql: string) => {
          if (sql.includes('index_meta')) return { rows: [{ value: 'emb' }] }
          if (sql.includes('COUNT')) return { rows: [{ n: 5 }] }
          if (sql.includes('DISTINCT')) return { rows: [] }
          return { rows: [] }
        }),
        exec: vi.fn(async () => {})
      },
      getPluginSettings: vi.fn(async () => ({})),
      ai: {
        embed: vi.fn(async () => ({
          embeddings: [],
          model: 'emb',
          dimensions: 2
        }))
      }
    } as unknown as PluginContext
  }

  it('indexes every page queued within the debounce window', async () => {
    const indexPage = vi
      .spyOn(embedIndex, 'indexPage')
      .mockResolvedValue(undefined)
    vi.spyOn(embedIndex, 'needsFullRebuildForModel').mockResolvedValue(false)
    vi.spyOn(embedIndex, 'getIndexInfo').mockResolvedValue({
      model: 'emb',
      dimensions: 2,
      chunkCount: 5
    })
    vi.spyOn(embedIndex, 'dropPageIndex').mockResolvedValue(undefined)

    const ctl = createQAController()
    ctl.setSettings({
      ...ctl.settings,
      reindex_debounce_ms: 50
    })
    const ctx = makeCtx({
      'A\0S\0P1': 1,
      'A\0S\0P2': 1,
      'A\0S\0P3': 1
    })

    ctl.schedulePageIndex(ctx, 'A', 'S', 'P1')
    ctl.schedulePageIndex(ctx, 'A', 'S', 'P2')
    ctl.schedulePageIndex(ctx, 'A', 'S', 'P3')
    await vi.advanceTimersByTimeAsync(60)
    // Drain the serialized index chain.
    await vi.runAllTimersAsync()
    await Promise.resolve()
    await Promise.resolve()

    expect(indexPage).toHaveBeenCalledTimes(3)
    const pages = indexPage.mock.calls.map((c) => c[3]).sort()
    expect(pages).toEqual(['P1', 'P2', 'P3'])
    ctl.dispose()
  })

  it('drops vectors when the page has no blocks', async () => {
    const drop = vi
      .spyOn(embedIndex, 'dropPageIndex')
      .mockResolvedValue(undefined)
    vi.spyOn(embedIndex, 'needsFullRebuildForModel').mockResolvedValue(false)
    vi.spyOn(embedIndex, 'getIndexInfo').mockResolvedValue({
      model: 'emb',
      dimensions: 2,
      chunkCount: 1
    })
    const indexPage = vi.spyOn(embedIndex, 'indexPage')

    const ctl = createQAController()
    ctl.setSettings({ ...ctl.settings, reindex_debounce_ms: 20 })
    const ctx = makeCtx({ 'W\0\0Gone': 0 })

    ctl.schedulePageIndex(ctx, 'W', '', 'Gone')
    await vi.advanceTimersByTimeAsync(30)
    await vi.runAllTimersAsync()
    await Promise.resolve()
    await Promise.resolve()

    expect(drop).toHaveBeenCalledWith(ctx, 'W', '', 'Gone')
    expect(indexPage).not.toHaveBeenCalled()
    ctl.dispose()
  })
})
