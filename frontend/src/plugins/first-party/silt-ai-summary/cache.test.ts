import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { PluginContext } from '../../sdk'
import {
  computeContentHash,
  getCachedSummary,
  latestSummaryForPage,
  migrateCache,
  putCachedSummary,
  resetCacheState
} from './cache'

/** Build a mock PluginContext whose pluginDb captures exec calls and serves
 *  programmable query rows. queryRows lets a test stage the next read. */
function makeCtx(queryRows: Record<string, unknown>[] = []): {
  ctx: PluginContext
  execCalls: { sql: string; params: unknown[] }[]
  migrateCalls: { version: number; sql: string }[]
  setQueryRows: (rows: Record<string, unknown>[]) => void
} {
  const execCalls: { sql: string; params: unknown[] }[] = []
  const migrateCalls: { version: number; sql: string }[] = []
  let rows = queryRows
  const ctx = {
    pluginDb: {
      exec: vi.fn(async (sql: string, params: unknown[]) => {
        execCalls.push({ sql, params })
      }),
      query: vi.fn(async () => ({ rows, truncated: false })),
      migrate: vi.fn(async (version: number, sql: string) => {
        migrateCalls.push({ version, sql })
      })
    }
  } as unknown as { ctx: PluginContext }
  return {
    ctx: ctx as unknown as PluginContext,
    execCalls,
    migrateCalls,
    setQueryRows: (next) => {
      rows = next
    }
  }
}

describe('computeContentHash', () => {
  it('produces a stable sha256 hex', async () => {
    // Known sha256("abc") = ba7816bf...
    expect(await computeContentHash('abc')).toBe(
      'ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad'
    )
  })
  it('differs for different content', async () => {
    expect(await computeContentHash('a')).not.toBe(await computeContentHash('b'))
  })
})

describe('migrateCache', () => {
  beforeEach(() => resetCacheState())

  it('runs the migration once with version 2 + the summaries table + summary_length', async () => {
    const { ctx, migrateCalls } = makeCtx()
    await migrateCache(ctx)
    await migrateCache(ctx) // idempotent
    expect(migrateCalls).toHaveLength(1)
    expect(migrateCalls[0].version).toBe(2)
    expect(migrateCalls[0].sql).toContain('CREATE TABLE IF NOT EXISTS summaries')
    expect(migrateCalls[0].sql).toContain('content_hash')
    expect(migrateCalls[0].sql).toContain('ALTER TABLE summaries ADD COLUMN summary_length')
  })
})

describe('getCachedSummary', () => {
  beforeEach(() => resetCacheState())

  it('returns null when no row matches', async () => {
    const { ctx } = makeCtx([])
    expect(await getCachedSummary(ctx, 'p', 'h')).toBeNull()
  })
  it('deserializes a stored row (JSON facets + prior snapshot)', async () => {
    const row = {
      summary: 's',
      tasks: '["t1","t2"]',
      risks: '[]',
      decisions: '["d1"]',
      prior_snapshot: '{"summary":"","tasks":["t1"],"risks":[],"decisions":[]}',
      model: 'm',
      generated_at: '2026-07-06T10:00:00Z'
    }
    const { ctx } = makeCtx([row])
    const got = await getCachedSummary(ctx, 'p', 'h')
    expect(got).not.toBeNull()
    expect(got?.summary).toBe('s')
    expect(got?.tasks).toEqual(['t1', 't2'])
    expect(got?.decisions).toEqual(['d1'])
    expect(got?.prior_snapshot.tasks).toEqual(['t1'])
    expect(got?.model).toBe('m')
    expect(got?.page_id).toBe('p')
    expect(got?.content_hash).toBe('h')
  })
  it('tolerates a corrupt JSON cell by degrading to empty arrays', async () => {
    const { ctx } = makeCtx([
      {
        summary: 's',
        tasks: 'not json',
        risks: null,
        decisions: '[]',
        prior_snapshot: 'broken',
        model: 'm',
        generated_at: 't'
      }
    ])
    const got = await getCachedSummary(ctx, 'p', 'h')
    expect(got?.tasks).toEqual([])
    expect(got?.risks).toEqual([])
    expect(got?.prior_snapshot.tasks).toEqual([])
  })
})

describe('putCachedSummary', () => {
  beforeEach(() => resetCacheState())

  it('upserts with serialized JSON arrays + the (page_id, content_hash) key', async () => {
    const { ctx, execCalls } = makeCtx()
    await putCachedSummary(ctx, {
      page_id: 'p',
      content_hash: 'h',
      summary: 's',
      tasks: ['t'],
      risks: [],
      decisions: ['d'],
      prior_snapshot: { summary: '', tasks: ['t'], risks: [], decisions: [] },
      model: 'm',
      summary_length: 'medium',
      generated_at: '2026-07-06T10:00:00Z'
    })
    expect(execCalls).toHaveLength(1)
    const { sql, params } = execCalls[0]
    expect(sql).toContain('INSERT INTO summaries')
    expect(sql).toContain('ON CONFLICT(page_id, content_hash)')
    expect(params[0]).toBe('p')
    expect(params[1]).toBe('h')
    expect(params[2]).toBe('s') // summary
    expect(params[3]).toBe('["t"]') // tasks serialized
    expect(params[4]).toBe('[]') // risks serialized
    expect(params[5]).toBe('["d"]') // decisions serialized
    expect(params[7]).toBe('m') // model
    // prior_snapshot is JSON containing the prior tasks.
    expect(String(params[6])).toContain('"tasks":["t"]')
  })
})

describe('latestSummaryForPage', () => {
  beforeEach(() => resetCacheState())

  it('reads the single most-recent row for the page', async () => {
    const { ctx } = makeCtx([
      {
        content_hash: 'h2',
        summary: 'newer',
        tasks: '[]',
        risks: '[]',
        decisions: '[]',
        prior_snapshot: '{}',
        model: 'm',
        generated_at: '2026-07-06T12:00:00Z'
      }
    ])
    const got = await latestSummaryForPage(ctx, 'p')
    expect(got?.summary).toBe('newer')
    expect(got?.content_hash).toBe('h2')
  })
  it('returns null when the page has no rows', async () => {
    const { ctx } = makeCtx([])
    expect(await latestSummaryForPage(ctx, 'p')).toBeNull()
  })
})
