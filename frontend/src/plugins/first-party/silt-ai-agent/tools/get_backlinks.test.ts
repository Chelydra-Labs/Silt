import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { PluginContext } from '../../../sdk'
import { asString } from '../../../../lib/asString'
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
    sqliteQuery: vi.fn(async (sql: string, params?: unknown[]) => {
      if (sql.includes('raw_content LIKE')) {
        const rows: Record<string, unknown>[] = []
        for (const [id, refs] of Object.entries(opts.backlinks ?? {})) {
          for (const ref of refs) {
            rows.push({
              id: ref.id,
              page: ref.page,
              clean_content: ref.snippet,
              raw_content: `((` + id + `))`
            })
          }
        }
        for (const [id, refs] of Object.entries(opts.embeds ?? {})) {
          for (const ref of refs) {
            rows.push({
              id: ref.id,
              page: ref.page,
              clean_content: ref.snippet,
              raw_content: `{{embed:${id}}}`
            })
          }
        }
        return { rows, truncated: false }
      }
      // resolveTargetIds: SELECT DISTINCT id FROM blocks WHERE page = ? [AND ...]
      const page = asString(params?.[0])
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
    expect(ctx.sqliteQuery).toHaveBeenCalledWith(
      expect.stringContaining('raw_content LIKE'),
      expect.any(Array)
    )

    const noEmbeds = await handleGetBacklinks(ctx, {
      target: UUID,
      include_embeds: false
    })
    expect(noEmbeds.content).not.toContain('embed')
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

  it('caps results and clamps the requested limit', async () => {
    const refs = Array.from({ length: 30 }, (_, i) => ({
      id: `src-${i}`,
      page: 'P',
      snippet: 'ref'
    }))
    const ctx = makeCtx({ backlinks: { [UUID]: refs } })
    const res = await handleGetBacklinks(ctx, {
      target: UUID,
      include_embeds: false
    })
    expect((res.content.match(/^- \[\d+\] \[backlink\]/gm) ?? []).length).toBe(
      20
    )
    const batchCall = (
      ctx.sqliteQuery as ReturnType<typeof vi.fn>
    ).mock.calls.find((call) => String(call[0]).includes('raw_content LIKE'))
    expect(batchCall?.[1]?.at(-1)).toBe(20)
  })

  it('exposes the tool def shape', () => {
    expect(getBacklinksToolDef.name).toBe('get_backlinks')
    expect(getBacklinksToolDef.parameters.required).toContain('target')
  })
})
