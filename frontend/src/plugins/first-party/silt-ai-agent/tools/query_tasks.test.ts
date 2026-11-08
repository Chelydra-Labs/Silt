import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { PluginContext } from '../../../sdk'
import { clearTools } from '../tool-registry'
import { handleQueryTasks, queryTasksToolDef } from './query_tasks'

function makeCtx(rows: Record<string, unknown>[] = []): {
  ctx: PluginContext
  calls: { sql: string; params: unknown[] }[]
} {
  const calls: { sql: string; params: unknown[] }[] = []
  const ctx = {
    sqliteQuery: vi.fn(async (sql: string, params?: unknown[]) => {
      calls.push({ sql, params: [...(params ?? [])] })
      return { rows, truncated: false }
    })
  } as unknown as PluginContext
  return { ctx, calls }
}

beforeEach(() => clearTools())
afterEach(() => clearTools())

describe('query_tasks', () => {
  it('builds parameterized SQL for status + owner filters', async () => {
    const { ctx, calls } = makeCtx()
    await handleQueryTasks(ctx, { status: 'doing', owner: 'alice' })
    expect(calls).toHaveLength(1)
    const { sql, params } = calls[0]
    expect(sql).toContain('t.status = ?')
    expect(sql).toContain('t.owner = ?')
    expect(sql).toMatch(/LIMIT \?/)
    // Values bound as parameters (not interpolated) — injection-safe.
    expect(params).toContain('DOING')
    expect(params).toContain('alice')
    expect(params).not.toContain("'alice'")
    expect(params[params.length - 1]).toBe(20) // default limit
  })

  it('applies the is_blocked filter via task_dependencies EXISTS', async () => {
    const { ctx, calls } = makeCtx()
    await handleQueryTasks(ctx, { is_blocked: true })
    expect(calls[0].sql).toContain('task_dependencies td')
    expect(calls[0].sql).toContain("bt.status != 'DONE'")
    // SELECT column always carries the EXISTS too.
    expect(calls[0].sql).toMatch(/AS is_blocked/)

    const { ctx: ctx2, calls: calls2 } = makeCtx()
    await handleQueryTasks(ctx2, { is_blocked: false })
    expect(calls2[0].sql).toContain('NOT EXISTS')
  })

  it('clamps limit to 1–50', async () => {
    const low = makeCtx()
    await handleQueryTasks(low.ctx, { limit: 0 })
    expect(low.calls[0].params[low.calls[0].params.length - 1]).toBe(1)

    const high = makeCtx()
    await handleQueryTasks(high.ctx, { limit: 100 })
    expect(high.calls[0].params[high.calls[0].params.length - 1]).toBe(50)
  })

  it('binds tags via an EXISTS subquery with placeholders', async () => {
    const { ctx, calls } = makeCtx()
    await handleQueryTasks(ctx, { tags: ['work/urgent', 'bug'] })
    const { sql, params } = calls[0]
    expect(sql).toContain('FROM tags tg')
    expect(sql).toMatch(/tg.raw_path IN \(\?,\?\)/)
    expect(params).toContain('work/urgent')
    expect(params).toContain('bug')
  })

  it('applies priority_min and due-date range bounds', async () => {
    const { ctx, calls } = makeCtx()
    await handleQueryTasks(ctx, {
      priority_min: 2,
      due_after: '2026-01-01',
      due_before: '2026-12-31'
    })
    const { sql, params } = calls[0]
    expect(sql).toContain('t.priority <= ?')
    expect(sql).toContain('t.due_date <= ?')
    expect(sql).toContain('t.due_date >= ?')
    expect(params).toContain(2)
    expect(params).toContain('2026-01-01')
    expect(params).toContain('2026-12-31')
  })

  it('adds a parameterized FTS predicate for text', async () => {
    const { ctx, calls } = makeCtx()
    await handleQueryTasks(ctx, { text: 'postgres "Bob"', owner: 'Bob' })
    const { sql, params } = calls[0]
    expect(sql).toContain('JOIN blocks_fts ON blocks_fts.rowid = b.rowid')
    expect(sql).toContain('blocks_fts MATCH ?')
    // Exact order pins the WHERE/param alignment: a future push reorder that
    // binds a value to the wrong predicate must fail this test.
    expect(params).toEqual(['"postgres"* "Bob"*', 'Bob', 20])
  })

  it('binds every filter in WHERE order', async () => {
    const { ctx, calls } = makeCtx()
    await handleQueryTasks(ctx, {
      text: 'ship',
      status: 'doing',
      owner: 'alice',
      priority_min: 2,
      due_before: '2026-12-31',
      due_after: '2026-01-01',
      notebook: 'Work',
      tags: ['work', 'bug'],
      is_blocked: true,
      limit: 5
    })
    expect(calls[0].params).toEqual([
      '"ship"*',
      'DOING',
      'alice',
      2,
      '2026-12-31',
      '2026-01-01',
      'Work',
      'work',
      'bug',
      5
    ])
  })

  it('searches single multibyte characters (2-byte cutoff, not 2 chars)', async () => {
    const { ctx, calls } = makeCtx()
    await handleQueryTasks(ctx, { text: 'é' })
    expect(calls[0].sql).toContain('blocks_fts MATCH ?')
    expect(calls[0].params[0]).toBe('"é"*')
  })

  it('omits the FTS join when text has no usable terms', async () => {
    const { ctx, calls } = makeCtx()
    await handleQueryTasks(ctx, { text: 'a * b' })
    expect(calls[0].sql).not.toContain('blocks_fts')
    expect(calls[0].params).toEqual([20])
  })

  it('describes the real text-matching scope and cutoff', () => {
    const text = queryTasksToolDef.parameters.properties.text.description
    // blocks_fts indexes content + notebook + section, and the 2-char floor is
    // 2 UTF-8 bytes (so a single CJK/accented char is still searched).
    expect(text).toMatch(/notebook and section names/i)
    expect(text).toMatch(/shorter than 2 bytes/i)
  })

  it('rejects an invalid status value', async () => {
    const { ctx } = makeCtx()
    const res = await handleQueryTasks(ctx, { status: 'banana' })
    expect(res.error).toMatch(/status must be one of/)
  })

  it('formats returned tasks with id, status, due, owner, location, blocked', async () => {
    const { ctx } = makeCtx([
      {
        id: 't1',
        clean_content: 'Ship the agent tools',
        status: 'DOING',
        due_date: '2026-07-20',
        owner: 'chris',
        notebook: 'Work',
        section: 'Sprint',
        page: 'Sprint 41',
        priority: 1,
        is_blocked: 1
      }
    ])
    const res = await handleQueryTasks(ctx, {})
    expect(res.error).toBeUndefined()
    // Citation header must match assignEvidenceIndices shape (`[n] block <id>`).
    expect(res.content).toContain('[1] block t1')
    expect(res.content).toContain('Ship the agent tools')
    expect(res.content).toContain('DOING')
    expect(res.content).toContain('2026-07-20')
    expect(res.content).toContain('chris')
    expect(res.content).toContain('Work > Sprint > Sprint 41')
    expect(res.content).toContain('blocked: yes')
    expect(res.evidence?.[0]).toMatchObject({
      citationIndex: 1,
      blockId: 't1'
    })
  })

  it('reports a clean empty message when no tasks match', async () => {
    const { ctx } = makeCtx([])
    const res = await handleQueryTasks(ctx, { status: 'DONE' })
    expect(res.content).toMatch(/no tasks match/i)
  })

  it('exposes the tool def shape', () => {
    expect(queryTasksToolDef.name).toBe('query_tasks')
    expect(queryTasksToolDef.parameters.properties).toHaveProperty('is_blocked')
  })
})
