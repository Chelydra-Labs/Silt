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
  getAgentEmbedGeneration,
  getAgentVectorSearch,
  isAgentFullRebuildInProgress,
  isAgentIndexWarm,
  reconcileAgentEmbedIndex,
  resetAgentEmbedLifecycleForTests,
  setAgentFullRebuildInProgressForTests,
  setAgentIndexWarmForTests,
  setAgentVectorSearchForTests,
  startAgentEmbedIndex,
  stopAgentEmbedIndex
} from './embed_lifecycle'

function mockCtx(): PluginContext {
  const meta = new Map<string, string>()
  return {
    on: vi.fn(() => () => {}),
    ai: {
      embed: vi.fn(async (req: { texts: string[] }) => ({
        embeddings: req.texts.map(() => [1, 0]),
        model: 'nomic-embed',
        dimensions: 2
      }))
    },
    sqliteQuery: vi.fn(async (sql: string) => {
      if (sql.includes('COUNT(*)'))
        return { rows: [{ n: 0 }], truncated: false }
      return { rows: [], truncated: false }
    }),
    pluginDb: {
      migrate: vi.fn(async () => {}),
      query: vi.fn(async (sql: string, params?: unknown[]) => {
        if (sql.includes('COUNT(*)')) return { rows: [{ n: 0 }] }
        if (sql.includes('index_meta') && typeof params?.[0] === 'string') {
          const v = meta.get(params[0])
          return { rows: v != null ? [{ value: v }] : [] }
        }
        if (sql.includes('MATCH') || sql.includes('embeddings e')) {
          throw new Error('vec0 boom')
        }
        return { rows: [] }
      }),
      exec: vi.fn(async (sql: string, params?: unknown[]) => {
        // Capture metaSet INSERT ... ON CONFLICT
        if (
          typeof sql === 'string' &&
          sql.includes('index_meta') &&
          typeof params?.[0] === 'string' &&
          typeof params?.[1] === 'string'
        ) {
          meta.set(params[0], params[1])
        }
      })
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
  it('start/stop does not throw and bumps generation', async () => {
    const ctx = mockCtx()
    const gen0 = getAgentEmbedGeneration()
    expect(() => startAgentEmbedIndex(ctx)).not.toThrow()
    await new Promise((r) => setTimeout(r, 40))
    expect(() => stopAgentEmbedIndex()).not.toThrow()
    expect(getAgentEmbedGeneration()).toBeGreaterThan(gen0)
    expect(isAgentFullRebuildInProgress()).toBe(false)
  })

  it('second start with a new ctx does not restart (idempotent)', async () => {
    const ctx1 = mockCtx()
    const ctx2 = mockCtx()
    startAgentEmbedIndex(ctx1)
    await new Promise((r) => setTimeout(r, 20))
    const gen = getAgentEmbedGeneration()
    const migrate1 = ctx1.pluginDb.migrate as ReturnType<typeof vi.fn>
    const callsBefore = migrate1.mock.calls.length
    startAgentEmbedIndex(ctx2)
    // Same generation — no stop/restart
    expect(getAgentEmbedGeneration()).toBe(gen)
    // ctx1 migrate was from first start; second start should not re-migrate immediately
    expect(migrate1.mock.calls.length).toBe(callsBefore)
    // Fresh ctx re-binds event subscriptions (ctx.on called on both)
    expect(ctx1.on).toHaveBeenCalled()
    expect(ctx2.on).toHaveBeenCalled()
    stopAgentEmbedIndex()
  })

  it('getAgentVectorSearch propagates vector errors (does not swallow)', async () => {
    const ctx = mockCtx()
    // Seed dimensions so vectorSearch reaches the KNN query (not early []).
    const meta = new Map<string, string>([['dimensions', '2']])
    ;(ctx.pluginDb.query as ReturnType<typeof vi.fn>).mockImplementation(
      async (sql: string, params?: unknown[]) => {
        if (sql.includes('index_meta') && typeof params?.[0] === 'string') {
          const v = meta.get(params[0])
          return { rows: v != null ? [{ value: v }] : [] }
        }
        if (sql.includes('MATCH') || sql.includes('embeddings')) {
          throw new Error('vec0 boom')
        }
        return { rows: [] }
      }
    )
    ;(ctx.ai.embed as ReturnType<typeof vi.fn>).mockResolvedValue({
      embeddings: [[1, 0]],
      model: 'nomic-embed',
      dimensions: 2
    })
    const fn = getAgentVectorSearch()
    await expect(fn(ctx, 'q', 5)).rejects.toThrow(/vec0 boom/)
  })

  it('getAgentVectorSearch returns [] while full rebuild in progress', async () => {
    setAgentVectorSearchForTests(null)
    setAgentFullRebuildInProgressForTests(true)
    expect(isAgentFullRebuildInProgress()).toBe(true)
    const ctx = mockCtx()
    // Production path (no override): mid-rebuild must not hit vec0 / half-built index.
    const hits = await getAgentVectorSearch()(ctx, 'q', 3)
    expect(hits).toEqual([])
    // pluginDb must not have been queried for KNN while rebuild flag is set
    const query = ctx.pluginDb.query as ReturnType<typeof vi.fn>
    expect(
      query.mock.calls.some(
        (c) =>
          typeof c[0] === 'string' &&
          (c[0].includes('MATCH') || c[0].includes('embeddings'))
      )
    ).toBe(false)
    setAgentFullRebuildInProgressForTests(null)
    expect(isAgentFullRebuildInProgress()).toBe(false)
  })

  it('isAgentIndexWarm respects test override', async () => {
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
