import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { PluginContext } from '../../sdk'

vi.mock('../../shared/ai-chat/availability', () => ({
  getAIAvailability: vi.fn(() => ({
    ragEnabled: true,
    aiEnabled: true,
    chatReady: true,
    embedReady: true,
    drawerAvailable: true,
    features: { enabled: true, rag_enabled: true, summaries_enabled: false }
  }))
}))

vi.mock('../../../settings/ai-setup', () => ({
  embeddingProviderNeedsSetup: vi.fn(() => false),
  aiProviderNeedsSetup: vi.fn(() => false)
}))

vi.mock('../../../settings/store.svelte', () => ({
  settings: {
    config: {
      ai: {
        embedding: {
          provider_type: 'local',
          model: 'nomic-embed',
          has_key: true
        }
      }
    }
  }
}))

import { getAIAvailability } from '../../shared/ai-chat/availability'
import {
  getAgentVectorSearch,
  isAgentFullRebuildInProgress,
  isAgentIndexWarm,
  reconcileAgentEmbedIndex,
  resetAgentEmbedLifecycleForTests,
  setAgentIndexWarmForTests,
  setAgentVectorSearchForTests,
  startAgentEmbedIndex,
  stopAgentEmbedIndex
} from './embed_lifecycle'

function mockCtx(): PluginContext {
  return {
    on: vi.fn(() => () => {}),
    ai: {
      embed: vi.fn(async (req: { texts: string[] }) => ({
        embeddings: req.texts.map(() => [1, 0]),
        model: 'nomic-embed',
        dimensions: 2
      }))
    },
    sqliteQuery: vi.fn(async () => ({ rows: [], truncated: false })),
    pluginDb: {
      migrate: vi.fn(async () => {}),
      query: vi.fn(async (sql: string, params?: unknown[]) => {
        if (sql.includes('COUNT(*)')) return { rows: [{ n: 0 }] }
        if (sql.includes('index_meta') && params?.[0] === 'dimensions') {
          return { rows: [{ value: '2' }] }
        }
        if (sql.includes('index_meta') && params?.[0] === 'model') {
          return { rows: [{ value: 'nomic-embed' }] }
        }
        if (sql.includes('index_meta')) return { rows: [] }
        if (sql.includes('MATCH') || sql.includes('embeddings e')) {
          throw new Error('vec0 boom')
        }
        return { rows: [] }
      }),
      exec: vi.fn(async () => {})
    }
  } as unknown as PluginContext
}

beforeEach(() => {
  resetAgentEmbedLifecycleForTests()
  vi.mocked(getAIAvailability).mockReturnValue({
    ragEnabled: true,
    aiEnabled: true,
    chatReady: true,
    embedReady: true,
    drawerAvailable: true,
    features: { enabled: true, rag_enabled: true, summaries_enabled: false }
  } as ReturnType<typeof getAIAvailability>)
})

afterEach(() => {
  resetAgentEmbedLifecycleForTests()
})

describe('embed_lifecycle', () => {
  it('start/stop does not throw', async () => {
    const ctx = mockCtx()
    expect(() => startAgentEmbedIndex(ctx)).not.toThrow()
    await new Promise((r) => setTimeout(r, 40))
    expect(() => stopAgentEmbedIndex()).not.toThrow()
    expect(isAgentFullRebuildInProgress()).toBe(false)
  })

  it('getAgentVectorSearch propagates vector errors (does not swallow)', async () => {
    const ctx = mockCtx()
    const fn = getAgentVectorSearch()
    await expect(fn(ctx, 'q', 5)).rejects.toThrow(/vec0 boom/)
  })

  it('isAgentIndexWarm respects test override and defaults cold before start completes', async () => {
    const ctx = mockCtx()
    expect(await isAgentIndexWarm(ctx)).toBe(false)
    setAgentIndexWarmForTests(true)
    expect(await isAgentIndexWarm(ctx)).toBe(true)
    setAgentIndexWarmForTests(false)
    expect(await isAgentIndexWarm(ctx)).toBe(false)
  })

  it('reconcileAgentEmbedIndex stops when RAG off', () => {
    const ctx = mockCtx()
    startAgentEmbedIndex(ctx)
    vi.mocked(getAIAvailability).mockReturnValue({
      ragEnabled: false,
      aiEnabled: true,
      chatReady: true,
      embedReady: true,
      drawerAvailable: true,
      features: { enabled: true, rag_enabled: false, summaries_enabled: false }
    } as ReturnType<typeof getAIAvailability>)
    expect(() => reconcileAgentEmbedIndex(ctx)).not.toThrow()
  })

  it('uses only the agent ctx.pluginDb (no cross-plugin DB)', async () => {
    const ctx = mockCtx()
    const migrate = ctx.pluginDb.migrate as ReturnType<typeof vi.fn>
    startAgentEmbedIndex(ctx)
    await new Promise((r) => setTimeout(r, 40))
    expect(migrate).toHaveBeenCalled()
    stopAgentEmbedIndex()
  })

  it('setAgentVectorSearchForTests overrides production path', async () => {
    setAgentVectorSearchForTests(async () => [
      {
        blockId: 'x',
        notebook: 'N',
        section: '',
        page: 'P',
        lineNumber: 0,
        text: 't',
        score: 0.1
      }
    ])
    const hits = await getAgentVectorSearch()(mockCtx(), 'q', 3)
    expect(hits).toHaveLength(1)
    expect(hits[0].blockId).toBe('x')
  })
})
