import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { PluginContext } from '../../../sdk'
import { clearTools } from '../tool-registry'
import {
  commitRenameTag,
  findUntaggedToolDef,
  handleFindUntagged,
  handleListTags,
  handleRenameTag,
  listTagsToolDef,
  renameTagToolDef
} from './tag_management'

// --- list_tags ------------------------------------------------------------

function makeListCtx(rows: Record<string, unknown>[]): {
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

describe('list_tags', () => {
  beforeEach(() => clearTools())
  afterEach(() => clearTools())

  it('aggregates tag counts ordered by usage desc', async () => {
    const { ctx, calls } = makeListCtx([
      { raw_path: 'work/urgent', count: 5 },
      { raw_path: 'bug', count: 2 }
    ])
    const res = await handleListTags(ctx, {})
    expect(res.error).toBeUndefined()
    expect(calls[0].sql).toMatch(/GROUP BY raw_path/)
    expect(calls[0].sql).toMatch(/ORDER BY count DESC/)
    expect(res.content).toContain('#work/urgent')
    expect(res.content).toContain('5 blocks')
    expect(res.content).toContain('#bug')
    expect(res.content).toContain('2 blocks')
  })

  it('uses singular "block" for count === 1', async () => {
    const { ctx } = makeListCtx([{ raw_path: 'solo', count: 1 }])
    const res = await handleListTags(ctx, {})
    expect(res.content).toContain('1 block')
    expect(res.content).not.toContain('1 blocks')
  })

  it('reports a clean empty message when there are no tags', async () => {
    const { ctx } = makeListCtx([])
    const res = await handleListTags(ctx, {})
    expect(res.content).toMatch(/no tags found/i)
  })

  it('caps the tag list and reports omitted tags', async () => {
    const rows = Array.from({ length: 200 }, (_, i) => ({
      raw_path: `tag/${i}`,
      count: 1
    }))
    const calls: { sql: string; params: unknown[] }[] = []
    const ctx = {
      sqliteQuery: vi.fn(async (sql: string, params?: unknown[]) => {
        calls.push({ sql, params: [...(params ?? [])] })
        return sql.includes('COUNT(DISTINCT')
          ? { rows: [{ total: 250 }], truncated: false }
          : { rows, truncated: false }
      })
    } as unknown as PluginContext
    const res = await handleListTags(ctx, {})
    expect(calls[0].sql).toMatch(/LIMIT \?/i)
    expect(calls[0].params).toEqual([200])
    expect(res.content).toContain('250 tag(s)')
    expect(res.content).toContain('…and 50 more tag(s)')
  })
})

// --- find_untagged --------------------------------------------------------

interface UntaggedRow {
  id: string
  clean_content: string
  notebook: string
  section: string
  page: string
}

function makeUntaggedCtx(
  rows: UntaggedRow[],
  expectNotebook?: string
): {
  ctx: PluginContext
  calls: { sql: string; params: unknown[] }[]
} {
  const calls: { sql: string; params: unknown[] }[] = []
  const ctx = {
    sqliteQuery: vi.fn(async (sql: string, params?: unknown[]) => {
      calls.push({ sql, params: [...(params ?? [])] })
      if (expectNotebook && !sql.includes('notebook = ?')) {
        throw new Error('expected notebook clause missing')
      }
      return {
        rows: rows as unknown as Record<string, unknown>[],
        truncated: false
      }
    })
  } as unknown as PluginContext
  return { ctx, calls }
}

describe('find_untagged', () => {
  beforeEach(() => clearTools())
  afterEach(() => clearTools())

  it('lists untagged tasks with breadcrumb + snippet', async () => {
    const { ctx, calls } = makeUntaggedCtx([
      {
        id: 't1',
        clean_content: 'untriaged bug',
        notebook: 'Work',
        section: 'Sprint',
        page: 'Backlog'
      }
    ])
    const res = await handleFindUntagged(ctx, {})
    expect(res.error).toBeUndefined()
    expect(calls[0].sql).toMatch(/type = 'TASK'/)
    expect(calls[0].sql).toMatch(/NOT IN \(SELECT block_id FROM tags\)/)
    expect(calls[0].params[calls[0].params.length - 1]).toBe(20)
    expect(res.content).toContain('t1')
    expect(res.content).toContain('Work > Sprint > Backlog')
    expect(res.content).toContain('untriaged bug')
  })

  it('scopes by notebook when scope is provided', async () => {
    const { ctx, calls } = makeUntaggedCtx([], 'Work')
    await handleFindUntagged(ctx, { scope: 'Work' })
    expect(calls[0].sql).toMatch(/b\.notebook = \?/)
    expect(calls[0].params).toContain('Work')
  })

  it('clamps limit to 1–100', async () => {
    const high = makeUntaggedCtx([])
    await handleFindUntagged(high.ctx, { limit: 500 })
    expect(high.calls[0].params[high.calls[0].params.length - 1]).toBe(100)

    const low = makeUntaggedCtx([])
    await handleFindUntagged(low.ctx, { limit: 0 })
    expect(low.calls[0].params[low.calls[0].params.length - 1]).toBe(1)
  })

  it('reports a clean empty message when all tasks are tagged', async () => {
    const { ctx } = makeUntaggedCtx([])
    const res = await handleFindUntagged(ctx, {})
    expect(res.content).toMatch(/no untagged tasks/i)
  })
})

// --- rename_tag (STAGED) --------------------------------------------------

function makeRenameCtx(opts: { queryByTagRows?: Record<string, unknown>[] }): {
  ctx: PluginContext
  queryByTag: ReturnType<typeof vi.fn>
  sqliteQuery: ReturnType<typeof vi.fn>
  pluginDbExec: ReturnType<typeof vi.fn>
  mutateBlock: ReturnType<typeof vi.fn>
} {
  const queryByTag = vi.fn(async () => ({
    rows: opts.queryByTagRows ?? [],
    truncated: false
  }))
  const sqliteQuery = vi.fn(async () => ({
    rows: opts.queryByTagRows ?? [],
    truncated: false
  }))
  const pluginDbExec = vi.fn(async () => undefined)
  const mutateBlock = vi.fn(async () => true)
  const ctx = {
    queryByTag,
    sqliteQuery,
    pluginDb: {
      exec: pluginDbExec,
      query: vi.fn(async () => ({ rows: [], truncated: false })),
      migrate: vi.fn()
    },
    mutateBlock
  } as unknown as PluginContext
  return { ctx, queryByTag, sqliteQuery, pluginDbExec, mutateBlock }
}

describe('rename_tag (handler — staging)', () => {
  beforeEach(() => clearTools())
  afterEach(() => clearTools())

  it('stages the op and returns a token + preview without executing', async () => {
    const { ctx, queryByTag, pluginDbExec, mutateBlock } = makeRenameCtx({
      queryByTagRows: [
        { id: 'b1', clean_content: 'note #work' },
        { id: 'b2', clean_content: 'note #work too' }
      ]
    })
    const res = await handleRenameTag(ctx, {
      old_tag: 'work',
      new_tag: 'work/urgent'
    })
    expect(res.error).toBeUndefined()
    expect(res.isStaged).toBe(true)
    expect(res.stagedToken).toMatch(/^[0-9a-f]{32}$/)
    expect(res.stagedPreview?.kind).toBe('rename_tag')
    expect(res.stagedPreview?.affectedCount).toBe(2)
    expect(res.stagedPreview?.summary).toContain('#work')
    expect(res.stagedPreview?.summary).toContain('#work/urgent')
    expect(res.stagedPreview?.summary).toContain('2 blocks')
    // Counted via the exact-tag SQL lookup.
    expect(queryByTag).not.toHaveBeenCalled()
    expect(ctx.sqliteQuery).toHaveBeenCalledWith(
      expect.stringContaining('t.raw_path = ?'),
      ['work']
    )
    // Persisted the staged op via pluginDb.exec (INSERT INTO staging_tokens).
    expect(pluginDbExec).toHaveBeenCalled()
    // Did NOT rewrite any block yet — that happens on confirm.
    expect(mutateBlock).not.toHaveBeenCalled()
  })

  it('strips a leading # from input tag paths', async () => {
    const { ctx } = makeRenameCtx({ queryByTagRows: [] })
    await handleRenameTag(ctx, { old_tag: '#work', new_tag: '#biz' })
    // The exact-tag query receives the de-hashed form.
    expect(ctx.sqliteQuery).toHaveBeenCalledWith(
      expect.stringContaining('t.raw_path = ?'),
      ['work']
    )
  })

  it('rejects identical old_tag/new_tag', async () => {
    const { ctx } = makeRenameCtx({})
    const res = await handleRenameTag(ctx, {
      old_tag: 'work',
      new_tag: 'work'
    })
    expect(res.error).toMatch(/must differ/)
  })

  it('rejects empty tag values', async () => {
    const { ctx } = makeRenameCtx({})
    const a = await handleRenameTag(ctx, { old_tag: '', new_tag: 'x' })
    expect(a.error).toMatch(/old_tag/)
    const b = await handleRenameTag(ctx, { old_tag: 'x', new_tag: '' })
    expect(b.error).toMatch(/new_tag/)
  })
})

describe('rename_tag (commit — after confirm)', () => {
  beforeEach(() => clearTools())
  afterEach(() => clearTools())

  it('rewrites the hashtag in every matching block and reports the count', async () => {
    const { ctx, mutateBlock } = makeRenameCtx({
      queryByTagRows: [
        { id: 'b1', clean_content: '- [ ] ship #work today' },
        { id: 'b2', clean_content: 'see #work and #other' }
      ]
    })
    const res = await commitRenameTag(ctx, {
      old_tag: 'work',
      new_tag: 'work/urgent'
    })
    expect(res.error).toBeUndefined()
    expect(mutateBlock).toHaveBeenCalledTimes(2)
    expect(mutateBlock).toHaveBeenCalledWith(
      'b1',
      expect.stringContaining('#work/urgent')
    )
    // b2 has two tags; only #work is renamed, #other survives.
    const b2Call = mutateBlock.mock.calls.find((c) => c[0] === 'b2')
    expect(b2Call?.[1]).toContain('#work/urgent')
    expect(b2Call?.[1]).toContain('#other')
    expect(res.content).toMatch(/Renamed #work → #work\/urgent in 2 blocks/)
  })

  it('does not match #work as a prefix of #workflow', async () => {
    const { ctx, mutateBlock } = makeRenameCtx({
      queryByTagRows: [
        // queryByTag matches via raw_path = ? OR LIKE '<path>/%' so this row
        // may carry a longer tag whose prefix is `work`. The commit must
        // leave it untouched.
        { id: 'b1', clean_content: 'longer tag #workflow' }
      ]
    })
    const res = await commitRenameTag(ctx, {
      old_tag: 'work',
      new_tag: 'biz'
    })
    expect(res.error).toBeUndefined()
    // No literal #work token → no rewrite, count 0.
    expect(mutateBlock).not.toHaveBeenCalled()
    expect(res.content).toMatch(/0 blocks|nothing renamed/)
  })

  it('honours params stored at stage time, not a later model call', async () => {
    // commit reads old_tag/new_tag from the params record, so the model
    // cannot mutate the staged op by issuing another handler call.
    const { ctx, sqliteQuery } = makeRenameCtx({ queryByTagRows: [] })
    await commitRenameTag(ctx, { old_tag: 'staged-old', new_tag: 'staged-new' })
    expect(sqliteQuery).toHaveBeenCalledWith(
      expect.stringContaining('t.raw_path = ?'),
      ['staged-old']
    )
  })

  it('reports the real count when nothing matches at apply time', async () => {
    const { ctx, mutateBlock } = makeRenameCtx({ queryByTagRows: [] })
    const res = await commitRenameTag(ctx, {
      old_tag: 'gone',
      new_tag: 'biz'
    })
    expect(res.error).toBeUndefined()
    expect(mutateBlock).not.toHaveBeenCalled()
    expect(res.content).toMatch(/nothing renamed/i)
  })

  it('keeps exact-tag preview and commit counts aligned', async () => {
    const exactRows = [{ id: 'b1', clean_content: 'a #work' }]
    const queryByTag = vi.fn(async () => ({
      rows: [...exactRows, { id: 'b2', clean_content: 'a #work/child' }],
      truncated: false
    }))
    const sqliteQuery = vi.fn(async () => ({
      rows: exactRows,
      truncated: false
    }))
    const mutateBlock = vi.fn(async () => true)
    const ctx = {
      queryByTag,
      sqliteQuery,
      pluginDb: {
        exec: vi.fn(async () => undefined),
        query: vi.fn(async () => ({ rows: [], truncated: false })),
        migrate: vi.fn()
      },
      mutateBlock
    } as unknown as PluginContext

    const preview = await handleRenameTag(ctx, {
      old_tag: 'work',
      new_tag: 'biz'
    })
    const committed = await commitRenameTag(ctx, {
      old_tag: 'work',
      new_tag: 'biz'
    })
    expect(preview.stagedPreview?.affectedCount).toBe(1)
    expect(committed.content).toMatch(/in 1 block/)
    expect(mutateBlock).toHaveBeenCalledTimes(1)
  })

  it('errors on malformed staged params', async () => {
    const { ctx } = makeRenameCtx({})
    const res = await commitRenameTag(ctx, { old_tag: '', new_tag: '' })
    expect(res.error).toMatch(/malformed/)
  })

  it('surfaces partial failure when mutateBlock rejects a block', async () => {
    const queryByTagRows = [
      { id: 'b1', clean_content: 'a #work' },
      { id: 'b2', clean_content: 'b #work' }
    ]
    const ctx = {
      queryByTag: vi.fn(async () => ({
        rows: queryByTagRows,
        truncated: false
      })),
      sqliteQuery: vi.fn(async () => ({
        rows: queryByTagRows,
        truncated: false
      })),
      pluginDb: {
        exec: vi.fn(async () => undefined),
        query: vi.fn(async () => ({ rows: [], truncated: false })),
        migrate: vi.fn()
      },
      mutateBlock: vi.fn(async (id: string) => id === 'b1')
    } as unknown as PluginContext
    const res = await commitRenameTag(ctx, { old_tag: 'work', new_tag: 'biz' })
    expect(res.content).toContain('1 block') // b1 succeeded
    expect(res.error).toMatch(/mutateBlock failed/)
  })

  it('rejects replacement-string metacharacters / invalid grammar at stage', async () => {
    const { ctx } = makeRenameCtx({})
    const dollar = await handleRenameTag(ctx, {
      old_tag: 'work',
      new_tag: 'ev$&il'
    })
    expect(dollar.error).toMatch(/letters, numbers/)
    const spaces = await handleRenameTag(ctx, {
      old_tag: 'work',
      new_tag: 'two words'
    })
    expect(spaces.error).toMatch(/letters, numbers/)
  })

  it('does not interpret $ patterns in the replacement (function replacer)', async () => {
    // A clean newTag that happens to contain '$' would be rejected by the
    // grammar guard; this asserts the commit path itself is safe by using a
    // valid tag and confirming no match-text leaks into the rewritten body.
    const { ctx, mutateBlock } = makeRenameCtx({
      queryByTagRows: [{ id: 'b1', clean_content: 'do #work now' }]
    })
    const res = await commitRenameTag(ctx, {
      old_tag: 'work',
      new_tag: 'done'
    })
    expect(res.error).toBeUndefined()
    const written = mutateBlock.mock.calls[0]?.[1] as string
    expect(written).toBe('do #done now')
    expect(written).not.toContain('work')
  })

  it('rejects malformed staged params with invalid grammar', async () => {
    const { ctx } = makeRenameCtx({})
    const res = await commitRenameTag(ctx, {
      old_tag: 'work',
      new_tag: 'bad$tag'
    })
    expect(res.error).toMatch(/malformed/)
  })
})

describe('tool def shapes', () => {
  it('exposes names + required params', () => {
    expect(listTagsToolDef.name).toBe('list_tags')
    expect(findUntaggedToolDef.name).toBe('find_untagged')
    expect(renameTagToolDef.name).toBe('rename_tag')
    expect(renameTagToolDef.parameters.required).toEqual(['old_tag', 'new_tag'])
    // list_tags and find_untagged are read-only — no required params.
    expect(listTagsToolDef.parameters.properties).toEqual({})
    expect(findUntaggedToolDef.parameters.properties).toHaveProperty('scope')
    expect(findUntaggedToolDef.parameters.properties).toHaveProperty('limit')
  })
})
