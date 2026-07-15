import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { PluginContext } from '../../../sdk'
import { clearTools } from '../tool-registry'
import {
  getVaultStatisticsToolDef,
  handleGetVaultStatistics
} from './get_vault_statistics'

/**
 * Default row provider — distinct SQL fragments distinguish each aggregation
 * so a substring matcher never collides (e.g. 'as stale_tasks' is more
 * specific than the generic 'from tasks t'). Tests override individual
 * fragments to exercise specific scenarios.
 */
type RowProvider = (sql: string) => Record<string, unknown>[]

function defaultRows(): Record<string, Record<string, unknown>[]> {
  return {
    'group by type': [
      { type: 'NOTE', count: 12 },
      { type: 'TASK', count: 5 }
    ],
    'count(distinct notebook)': [{ notebooks: 2, sections: 4, pages: 9 }],
    total_files: [{ total_files: 9 }],
    'group by t.status': [
      { status: 'TODO', count: 3 },
      { status: 'DONE', count: 2 }
    ],
    'as stale_tasks': [{ stale_tasks: 0 }],
    'from page_links': [], // referenced pages (none for orphan count)
    'distinct notebook, section, page from blocks': [], // all pages for orphan
    'from tags': [
      { raw_path: 'work/urgent', count: 4 },
      { raw_path: 'bug', count: 2 }
    ],
    'order by indexed_at': [{ path: '/v/Work/Notes/a.md', size: 1024 }]
  }
}

/**
 * Build a ctx whose sqliteQuery dispatches on the longest matching fragment
 * (longest first so 'as stale_tasks' wins over 'from tasks'). Captures every
 * call so the read-only contract is assertable.
 */
function makeCtx(overrides: Record<string, Record<string, unknown>[]> = {}): {
  ctx: PluginContext
  calls: { sql: string; params: unknown[] }[]
  mutateFns: { name: string; args: unknown[] }[]
} {
  const table = { ...defaultRows(), ...overrides }
  const calls: { sql: string; params: unknown[] }[] = []
  const mutateFns: { name: string; args: unknown[] }[] = []

  const provider: RowProvider = (sql) => {
    const lower = sql.toLowerCase()
    // Longest-match-wins: more specific keys shadow generic ones.
    const keys = Object.keys(table).sort((a, b) => b.length - a.length)
    for (const k of keys) {
      if (lower.includes(k)) return table[k]
    }
    return []
  }

  const ctx = {
    today: '2026-07-15',
    sqliteQuery: vi.fn(async (sql: string, params?: unknown[]) => {
      calls.push({ sql, params: [...(params ?? [])] })
      return { rows: provider(sql), truncated: false }
    }),
    // Mutator stubs — the read-only contract requires NONE of these to fire.
    createBlock: vi.fn((...args: unknown[]) => {
      mutateFns.push({ name: 'createBlock', args })
      return 'should-not-happen'
    }),
    createPage: vi.fn((...args: unknown[]) => {
      mutateFns.push({ name: 'createPage', args })
      return 'should-not-happen'
    }),
    mutateBlock: vi.fn((...args: unknown[]) => {
      mutateFns.push({ name: 'mutateBlock', args })
      return true
    })
  } as unknown as PluginContext
  return { ctx, calls, mutateFns }
}

beforeEach(() => clearTools())
afterEach(() => clearTools())

describe('get_vault_statistics', () => {
  it('aggregates block + task counts, locations, and tags into text', async () => {
    const { ctx, mutateFns } = makeCtx()
    const res = await handleGetVaultStatistics(ctx, {})
    expect(res.error).toBeUndefined()

    expect(res.content).toMatch(/NOTE: 12/)
    expect(res.content).toMatch(/TASK: 5/)
    expect(res.content).toMatch(/TODO: 3/)
    expect(res.content).toMatch(/DONE: 2/)
    expect(res.content).toMatch(/2 notebook\(s\)/)
    expect(res.content).toMatch(/9 page\(s\)/)
    expect(res.content).toMatch(/9 indexed file\(s\)/)
    expect(res.content).toMatch(/work\/urgent: 4/)
    expect(res.content).toMatch(/Work\/Notes\/a\.md/)
    // Read-only: no mutator fired.
    expect(mutateFns).toHaveLength(0)
  })

  it('reports stale tasks when the overdue query finds rows', async () => {
    const { ctx } = makeCtx({ 'as stale_tasks': [{ stale_tasks: 3 }] })
    const res = await handleGetVaultStatistics(ctx, {})
    expect(res.content).toMatch(/3 stale task\(s\)/)
  })

  it('uses a WHERE clause before unscoped stale-task predicates', async () => {
    const { ctx, calls } = makeCtx()
    await handleGetVaultStatistics(ctx, {})

    const staleCall = calls.find((c) =>
      c.sql.toLowerCase().includes('as stale_tasks')
    )
    expect(staleCall?.sql).toMatch(
      /from tasks t\s+where\s+1\s*=\s*1\s+and\s+t\.due_date/i
    )
    expect(staleCall?.sql).not.toMatch(/from tasks t\s+and/i)
  })

  it('reports orphan pages: a page tuple with no inbound links', async () => {
    // All-pages query returns three pages; referenced-pages query returns one.
    // The two pages not in the referenced set are the orphans.
    const { ctx } = makeCtx({
      'from page_links': [{ notebook: 'N', section: 'S', page: 'Linked' }],
      'distinct notebook, section, page from blocks': [
        { notebook: 'N', section: 'S', page: 'Linked' },
        { notebook: 'N', section: 'S', page: 'Orphan1' },
        { notebook: 'N', section: 'S', page: 'Orphan2' }
      ]
    })
    const res = await handleGetVaultStatistics(ctx, {})
    expect(res.content).toMatch(/2 orphan page\(s\)/)
  })

  it('shows "no stale / no orphan" when nothing needs attention', async () => {
    const { ctx } = makeCtx()
    const res = await handleGetVaultStatistics(ctx, {})
    expect(res.content).toMatch(/Needs attention: none/)
  })

  it('scopes every metric to a notebook via parameterized WHERE', async () => {
    const { ctx, calls } = makeCtx()
    await handleGetVaultStatistics(ctx, { scope: 'Work' })

    // Every aggregation that supports a notebook scope binds 'Work' as a
    // parameter (never interpolates it with quotes — injection safety).
    const scoped = calls.filter(
      (c) => c.params.includes('Work') || c.params.includes('%/Work/%')
    )
    expect(scoped.length).toBeGreaterThan(0)
    for (const { sql } of calls) {
      // The only 'Work' literals allowed are the LIKE-pattern placeholder
      // for the path column — never a direct = 'Work' in the SQL text.
      expect(sql).not.toMatch(/=\s*'Work'/i)
    }
  })

  it('never mutates — every issued statement is a SELECT', async () => {
    const { ctx, calls, mutateFns } = makeCtx()
    await handleGetVaultStatistics(ctx, {})
    expect(mutateFns).toHaveLength(0)
    for (const { sql } of calls) {
      expect(sql.trim().toUpperCase().startsWith('SELECT')).toBe(true)
    }
  })

  it('exposes the tool def shape', () => {
    expect(getVaultStatisticsToolDef.name).toBe('get_vault_statistics')
    expect(
      (getVaultStatisticsToolDef.parameters as { required?: string[] })
        .required ?? []
    ).toEqual([])
  })
})
