// End-to-end unit path: retrieve → stream → cite (mocked bindings).
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createQAController } from './state.svelte'
import type { PluginContext } from '../../sdk'

const { mockSettings } = vi.hoisted(() => ({
  mockSettings: {
    config: {
      ai: {
        chat: { model: 'chat-m', provider_type: 'local' },
        embedding: { model: 'emb-m', provider_type: 'local' }
      },
      plugins: { disabled: [], plugin_settings: { 'silt-ai-qa': {} } }
    }
  }
}))

vi.mock('../../../settings/store.svelte', () => ({
  settings: mockSettings
}))

describe('ask flow (mocked)', () => {
  beforeEach(() => {
    mockSettings.config.ai.chat.model = 'chat-m'
    mockSettings.config.ai.embedding.model = 'emb-m'
  })

  it('streams an answer with citations and navigable block ids', async () => {
    const deltas = ['We ', 'chose ', 'Postgres [1].']
    let deltaIdx = 0
    const stream = {
      streamId: 's1',
      cancel: vi.fn(async () => {}),
      result: async () => ({
        content: 'We chose Postgres [1].',
        model: 'chat-m'
      }),
      async *[Symbol.asyncIterator]() {
        while (deltaIdx < deltas.length) {
          yield deltas[deltaIdx++]
        }
      }
    }

    const ctx = {
      fullTextSearch: vi.fn(async () => ({
        rows: [
          {
            id: 'block-1',
            notebook: 'Work',
            section: 'Notes',
            page: 'Decisions',
            clean_content: 'We chose Postgres for billing.'
          }
        ]
      })),
      ai: {
        complete: vi.fn(async (req: { stream?: boolean }) => {
          if (req.stream) return stream
          return { content: 'We chose Postgres [1].', model: 'chat-m' }
        }),
        embed: vi.fn(async () => ({
          embeddings: [[0.1, 0.2, 0.3]],
          model: 'emb-m',
          dimensions: 3
        }))
      },
      pluginDb: {
        migrate: vi.fn(async () => {}),
        query: vi.fn(async (sql: string) => {
          if (sql.includes('index_meta')) {
            return { rows: [{ value: '3' }] }
          }
          if (sql.includes('embeddings')) {
            return {
              rows: [
                {
                  block_id: 'block-1',
                  notebook: 'Work',
                  section: 'Notes',
                  page: 'Decisions',
                  line_number: 1,
                  text: 'We chose Postgres for billing.',
                  distance: 0.05
                }
              ]
            }
          }
          if (sql.includes('COUNT')) return { rows: [{ n: 1 }] }
          return { rows: [] }
        }),
        exec: vi.fn(async () => {})
      },
      getPluginSettings: vi.fn(async () => ({}))
    } as unknown as PluginContext

    const ctl = createQAController()
    ctl.loadSettings(ctx)
    await ctl.ask(ctx, 'What database did we choose?')

    expect(ctl.panelStatus).toBe('idle')
    expect(ctl.answer).toContain('Postgres')
    expect(ctl.citations.length).toBeGreaterThan(0)
    expect(ctl.citations[0].blockId).toBe('block-1')
    expect(ctl.messages.some((m) => m.role === 'user')).toBe(true)
    expect(ctl.messages.filter((m) => m.role === 'assistant')).toHaveLength(1)
  })

  it('surfaces retrieve errors instead of false no-results', async () => {
    const ctx = {
      fullTextSearch: vi.fn(async () => {
        throw new Error('fts boom')
      }),
      ai: {
        complete: vi.fn(),
        embed: vi.fn(async () => {
          throw new Error('embed boom')
        })
      },
      pluginDb: {
        migrate: vi.fn(async () => {}),
        query: vi.fn(async () => ({ rows: [] })),
        exec: vi.fn(async () => {})
      }
    } as unknown as PluginContext

    const ctl = createQAController()
    ctl.loadSettings(ctx)
    await ctl.ask(ctx, 'anything')
    expect(ctl.panelStatus).toBe('error')
    expect(ctl.errorMessage.toLowerCase()).toMatch(/fail|search|boom/)
  })

  it('ignores concurrent second ask', async () => {
    let release!: () => void
    const gate = new Promise<void>((r) => {
      release = r
    })
    const stream = {
      streamId: 's2',
      cancel: vi.fn(async () => {}),
      result: async () => ({ content: 'done', model: 'm' }),
      async *[Symbol.asyncIterator]() {
        await gate
        yield 'x'
      }
    }
    const complete = vi.fn(async (req: { stream?: boolean }) => {
      if (req.stream) return stream
      return { content: 'done', model: 'm' }
    })
    const ctx = {
      fullTextSearch: vi.fn(async () => ({
        rows: [
          {
            id: 'b',
            notebook: 'n',
            section: 's',
            page: 'p',
            clean_content: 'text'
          }
        ]
      })),
      ai: {
        complete,
        embed: vi.fn(async () => ({
          embeddings: [[1, 0]],
          model: 'e',
          dimensions: 2
        }))
      },
      pluginDb: {
        migrate: vi.fn(async () => {}),
        query: vi.fn(async (sql: string) => {
          if (sql.includes('index_meta')) return { rows: [{ value: '2' }] }
          if (sql.includes('embeddings')) {
            return {
              rows: [
                {
                  block_id: 'b',
                  notebook: 'n',
                  section: 's',
                  page: 'p',
                  text: 'text',
                  distance: 0
                }
              ]
            }
          }
          if (sql.includes('COUNT')) return { rows: [{ n: 1 }] }
          return { rows: [] }
        }),
        exec: vi.fn(async () => {})
      }
    } as unknown as PluginContext

    const ctl = createQAController()
    ctl.loadSettings(ctx)
    const p1 = ctl.ask(ctx, 'first')
    // Let first ask reach streaming
    await Promise.resolve()
    await Promise.resolve()
    await ctl.ask(ctx, 'second')
    // Second ask is ignored while in flight.
    expect(
      complete.mock.calls.filter((c) => (c[0] as { stream?: boolean })?.stream)
        .length
    ).toBeLessThanOrEqual(1)
    release()
    await p1
  })
})
