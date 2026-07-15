import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { PluginContext } from '../../../sdk'
import { clearTools } from '../tool-registry'
import { getBacklinksToolDef, handleGetBacklinks } from './get_backlinks'

const UUID = '11111111-1111-4111-8111-111111111111'

function makeCtx(opts: {
  backlinks?: Record<string, Record<string, unknown>[]>
  embeds?: Record<string, Record<string, unknown>[]>
  pageBlocks?: Record<string, string[]> // page path → block ids
}): PluginContext {
  return {
    getBacklinks: vi.fn(async (id: string) => ({
      rows: opts.backlinks?.[id] ?? [],
      truncated: false
    })),
    getEmbeds: vi.fn(async (id: string) => ({
      rows: opts.embeds?.[id] ?? [],
      truncated: false
    })),
    sqliteQuery: vi.fn(async (_sql: string, params?: unknown[]) => {
      // resolveTargetIds: SELECT DISTINCT id FROM blocks WHERE page = ? [AND ...]
      const page = String(params?.[0] ?? '')
      return {
        rows: (opts.pageBlocks?.[page] ?? []).map((id) => ({ id })),
        truncated: false
      }
    })
  } as unknown as PluginContext
}

beforeEach(() => clearTools())
afterEach(() => clearTools())

describe('get_backlinks', () => {
  it('returns backlinks formatted for a UUID target', async () => {
    const ctx = makeCtx({
      backlinks: {
        [UUID]: [
          {
            id: 'src1',
            page: 'Notes/PageA',
            snippet: 'see ((11111111-1111-4111-8111-111111111111)) for context'
          }
        ]
      }
    })
    const res = await handleGetBacklinks(ctx, { target: UUID })
    expect(res.error).toBeUndefined()
    expect(res.content).toContain('backlink')
    expect(res.content).toContain('src1')
    expect(res.content).toContain('Notes/PageA')
  })

  it('returns a clean empty list when nothing references the target', async () => {
    const ctx = makeCtx({})
    const res = await handleGetBacklinks(ctx, { target: UUID })
    expect(res.error).toBeUndefined()
    expect(res.content).toMatch(/no backlinks/i)
  })

  it('resolves a page path to block UUIDs before querying', async () => {
    const ctx = makeCtx({
      pageBlocks: { Decisions: [UUID] },
      backlinks: {
        [UUID]: [{ id: 'src9', page: 'Other', snippet: 'ref' }]
      }
    })
    const res = await handleGetBacklinks(ctx, { target: 'Decisions' })
    expect(res.content).toContain('src9')
    // sqliteQuery was used to resolve the page name.
    expect(ctx.sqliteQuery).toHaveBeenCalled()
  })

  it('errors when a page path resolves to no blocks', async () => {
    const ctx = makeCtx({ pageBlocks: {} })
    const res = await handleGetBacklinks(ctx, { target: 'Nowhere' })
    expect(res.error).toMatch(/could not resolve/)
  })

  it('includes embeds by default and excludes them when false', async () => {
    const ctx = makeCtx({
      backlinks: { [UUID]: [{ id: 'bl', page: 'P', snippet: 's' }] },
      embeds: { [UUID]: [{ id: 'em', page: 'P', snippet: 's' }] }
    })

    const withEmbeds = await handleGetBacklinks(ctx, { target: UUID })
    expect(withEmbeds.content).toContain('embed')
    expect(ctx.getEmbeds).toHaveBeenCalled()

    ;(ctx.getEmbeds as ReturnType<typeof vi.fn>).mockClear()
    const noEmbeds = await handleGetBacklinks(ctx, {
      target: UUID,
      include_embeds: false
    })
    expect(noEmbeds.content).not.toContain('embed')
    expect(ctx.getEmbeds).not.toHaveBeenCalled()
  })

  it('dedupes refs by source id across multiple page blocks', async () => {
    const second = '22222222-2222-4222-8222-222222222222'
    const ctx = makeCtx({
      pageBlocks: { Page: [UUID, second] },
      backlinks: {
        [UUID]: [{ id: 'dup', page: 'P', snippet: 'first' }],
        [second]: [{ id: 'dup', page: 'P', snippet: 'second' }]
      }
    })
    const res = await handleGetBacklinks(ctx, { target: 'Page' })
    // 'dup' appears only once despite two source ids both surfacing it.
    expect((res.content.match(/block dup/g) ?? []).length).toBe(1)
  })

  it('exposes the tool def shape', () => {
    expect(getBacklinksToolDef.name).toBe('get_backlinks')
    expect(getBacklinksToolDef.parameters.required).toContain('target')
  })
})
