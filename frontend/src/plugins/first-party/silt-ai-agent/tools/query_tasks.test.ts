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
    expect(res.content).toContain('Ship the agent tools')
    expect(res.content).toContain('t1')
    expect(res.content).toContain('DOING')
    expect(res.content).toContain('2026-07-20')
    expect(res.content).toContain('chris')
    expect(res.content).toContain('Work > Sprint > Sprint 41')
    expect(res.content).toContain('blocked: yes')
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
