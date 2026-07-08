import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { PluginContext } from '../../sdk'
import { stripReasoningContent } from '../../stripReasoning'
import { resetCacheState } from './cache'
import { summarize } from './summarize'
import { DEFAULT_SETTINGS } from './settings'
import type { SummarySettings } from './types'

/** Build a mock ctx with controllable pluginDb.query rows + ai.complete. The
 *  cache is driven entirely through these two surfaces, so the orchestrator
 *  test does not touch SQLite or the network. */
function makeCtx(opts: {
  cachedRow?: Record<string, unknown> | null
  latestRow?: Record<string, unknown> | null
  complete?: ReturnType<typeof vi.fn>
}): { ctx: PluginContext; execCalls: { sql: string; params: unknown[] }[] } {
  const execCalls: { sql: string; params: unknown[] }[] = []
  const queryImpl = vi.fn(async (sql: string, _params: unknown[]) => {
    // Route by the query's intent: a WHERE content_hash clause → exact cache
    // read; an ORDER BY generated_at → latest-for-page read.
    if (/content_hash\s*=\s*\?/.test(sql)) {
      return { rows: opts.cachedRow ? [opts.cachedRow] : [], truncated: false }
    }
    if (/ORDER BY generated_at/.test(sql)) {
      return { rows: opts.latestRow ? [opts.latestRow] : [], truncated: false }
    }
    return { rows: [], truncated: false }
  })
  const ctx = {
    pluginDb: {
      exec: vi.fn(async (sql: string, params: unknown[]) => {
        execCalls.push({ sql, params })
      }),
      query: queryImpl,
      migrate: vi.fn(async () => {})
    },
    ai: {
      complete:
        opts.complete ??
        vi.fn(async () => ({ content: '', model: 'm', usage: undefined }))
    }
  } as unknown as PluginContext
  return { ctx, execCalls }
}

const settings: SummarySettings = {
  ...DEFAULT_SETTINGS,
  facets: { ...DEFAULT_SETTINGS.facets }
}
const configured = { configuredModel: 'qwen3:30b', isConfigured: true }

describe('summarize — unconfigured gate (#220 no-network-until-configured)', () => {
  beforeEach(() => resetCacheState())
  it('returns an unconfigured error WITHOUT calling ai.complete', async () => {
    const complete = vi.fn()
    const { ctx } = makeCtx({ complete })
    const out = await summarize(ctx, {
      pageId: 'p',
      cleanContent: 'some note',
      settings,
      configuredModel: '',
      isConfigured: false
    })
    expect(out.ok).toBe(false)
    if (!out.ok) expect(out.error.code).toBe('unconfigured')
    expect(complete).not.toHaveBeenCalled()
  })
})

describe('summarize — empty note', () => {
  beforeEach(() => resetCacheState())
  it('serves a muted empty result without an LLM call', async () => {
    const complete = vi.fn()
    const { ctx } = makeCtx({ complete })
    const out = await summarize(ctx, {
      pageId: 'p',
      cleanContent: '   ',
      settings,
      ...configured
    })
    expect(out.ok).toBe(true)
    if (out.ok) {
      expect(out.result.summary).toBe('')
      expect(out.result.fromCache).toBe(true)
    }
    expect(complete).not.toHaveBeenCalled()
  })
})

describe('summarize — oversized note is skipped (#220)', () => {
  beforeEach(() => resetCacheState())
  it('returns an oversized outcome WITHOUT calling the LLM', async () => {
    const complete = vi.fn()
    const { ctx } = makeCtx({ complete })
    const big = 'x'.repeat(settings.max_note_chars + 1)
    const out = await summarize(ctx, {
      pageId: 'p',
      cleanContent: big,
      settings,
      ...configured
    })
    expect(out.ok).toBe(false)
    if (!out.ok) expect(out.error.code).toBe('oversized')
    expect(complete).not.toHaveBeenCalled()
  })
  it('does not skip a note exactly at the limit', async () => {
    const complete = vi
      .fn()
      .mockResolvedValue({ content: '{"summary":"s"}', model: 'qwen3:30b' })
    const { ctx } = makeCtx({ complete })
    const atLimit = 'y'.repeat(settings.max_note_chars)
    const out = await summarize(ctx, {
      pageId: 'p',
      cleanContent: atLimit,
      settings,
      ...configured
    })
    expect(out.ok).toBe(true)
    expect(complete).toHaveBeenCalled()
  })
})

describe('summarize — cache hit', () => {
  beforeEach(() => resetCacheState())
  it('serves a cached extraction with a matching model and skips the LLM', async () => {
    const complete = vi.fn()
    const cachedRow = {
      summary: 'cached summary',
      tasks: '["t1"]',
      risks: '[]',
      decisions: '["d1"]',
      prior_snapshot: '{"summary":"","tasks":[],"risks":[],"decisions":["d1"]}',
      model: 'qwen3:30b',
      generated_at: '2026-07-06T10:00:00Z'
    }
    const { ctx } = makeCtx({ cachedRow, complete })
    const out = await summarize(ctx, {
      pageId: 'p',
      cleanContent: 'a note',
      settings,
      ...configured
    })
    expect(out.ok).toBe(true)
    if (out.ok) {
      expect(out.result.fromCache).toBe(true)
      expect(out.result.summary).toBe('cached summary')
      expect(out.result.model).toBe('qwen3:30b')
      // newItems re-derived from the stored prior snapshot: d1 was prior ⇒ not new.
      expect(out.result.newItems.decisions).toEqual([])
      expect(out.result.newItems.tasks).toEqual(['t1']) // t1 wasn't in prior
    }
    expect(complete).not.toHaveBeenCalled()
  })
})

describe('summarize — cache miss generates, diffs against prior, stores', () => {
  beforeEach(() => resetCacheState())
  it('calls the LLM, parses, writes the cache row, and flags new items', async () => {
    const complete = vi.fn().mockResolvedValue({
      content: '{"summary":"s","tasks":["new task"]}',
      model: 'qwen3:30b'
    })
    const { ctx, execCalls } = makeCtx({ complete, latestRow: null })
    const out = await summarize(ctx, {
      pageId: 'p',
      cleanContent: 'a note',
      settings,
      ...configured
    })
    expect(out.ok).toBe(true)
    if (out.ok) {
      expect(out.result.fromCache).toBe(false)
      expect(out.result.summary).toBe('s')
      expect(out.result.newItems.tasks).toEqual(['new task']) // empty prior ⇒ new
    }
    expect(complete).toHaveBeenCalledTimes(1)
    // The cache row was written.
    const put = execCalls.find((e) => /INSERT INTO summaries/.test(e.sql))
    expect(put).toBeDefined()
    // prior_snapshot stored as empty extraction (no latest row).
    expect(String(put?.params[6])).toBe(
      '{"summary":"","tasks":[],"risks":[],"decisions":[]}'
    )
  })

  it('threads the latest extraction as the prior snapshot across regenerations', async () => {
    const complete = vi.fn().mockResolvedValue({
      content: '{"summary":"s","tasks":["second"]}',
      model: 'qwen3:30b'
    })
    const latestRow = {
      content_hash: 'oldhash',
      summary: 'first',
      tasks: '["first"]',
      risks: '[]',
      decisions: '[]',
      prior_snapshot: '{"summary":"","tasks":[],"risks":[],"decisions":[]}',
      model: 'qwen3:30b',
      generated_at: '2026-07-06T09:00:00Z'
    }
    const { ctx, execCalls } = makeCtx({ complete, latestRow })
    const out = await summarize(ctx, {
      pageId: 'p',
      cleanContent: 'changed note',
      settings,
      ...configured
    })
    expect(out.ok).toBe(true)
    if (out.ok) {
      // "second" is new vs prior ["first"]; "first" is gone (not flagged).
      expect(out.result.newItems.tasks).toEqual(['second'])
    }
    const put = execCalls.find((e) => /INSERT INTO summaries/.test(e.sql))
    // The stored prior_snapshot carries the latest extraction's tasks.
    expect(String(put?.params[6])).toContain('"tasks":["first"]')
  })
})

describe('summarize — invalidation', () => {
  beforeEach(() => resetCacheState())
  it('regenerates when the cached row was produced by a different model', async () => {
    const complete = vi
      .fn()
      .mockResolvedValue({ content: '{"summary":"fresh"}', model: 'qwen3:30b' })
    const cachedRow = {
      summary: 'stale',
      tasks: '[]',
      risks: '[]',
      decisions: '[]',
      prior_snapshot: '{}',
      model: 'old-model',
      generated_at: '2026-07-06T10:00:00Z'
    }
    const { ctx } = makeCtx({ cachedRow, complete })
    const out = await summarize(ctx, {
      pageId: 'p',
      cleanContent: 'note',
      settings,
      configuredModel: 'qwen3:30b',
      isConfigured: true
    })
    expect(out.ok).toBe(true)
    if (out.ok) {
      expect(out.result.fromCache).toBe(false)
      expect(out.result.summary).toBe('fresh')
    }
    expect(complete).toHaveBeenCalled()
  })

  it('regenerates when summary_length changed even if content + model match', async () => {
    // summary_length changes the prompt + maxTokens, so a cached row generated
    // at a different length is stale. Without this invalidation, changing
    // medium→short and reopening an unchanged note serves the old summary.
    const complete = vi.fn().mockResolvedValue({
      content: '{"summary":"fresh at medium"}',
      model: 'qwen3:30b'
    })
    const cachedRow = {
      summary: 'stale short summary',
      tasks: '[]',
      risks: '[]',
      decisions: '[]',
      prior_snapshot: '{}',
      model: 'qwen3:30b',
      summary_length: 'short',
      generated_at: '2026-07-06T10:00:00Z'
    }
    const { ctx } = makeCtx({ cachedRow, complete })
    const out = await summarize(ctx, {
      pageId: 'p',
      cleanContent: 'note',
      settings: { ...settings, summary_length: 'medium' },
      configuredModel: 'qwen3:30b',
      isConfigured: true
    })
    expect(out.ok).toBe(true)
    if (out.ok) {
      expect(out.result.fromCache).toBe(false)
      expect(out.result.summary).toBe('fresh at medium')
    }
    expect(complete).toHaveBeenCalled()
  })

  it('force=true bypasses the cache even on an exact hash + model match', async () => {
    const complete = vi
      .fn()
      .mockResolvedValue({ content: '{"summary":"regen"}', model: 'qwen3:30b' })
    const cachedRow = {
      summary: 'cached',
      tasks: '[]',
      risks: '[]',
      decisions: '[]',
      prior_snapshot: '{}',
      model: 'qwen3:30b',
      generated_at: '2026-07-06T10:00:00Z'
    }
    const { ctx } = makeCtx({ cachedRow, complete })
    const out = await summarize(ctx, {
      pageId: 'p',
      cleanContent: 'note',
      settings,
      ...configured,
      force: true
    })
    expect(out.ok).toBe(true)
    if (out.ok) expect(out.result.fromCache).toBe(false)
    expect(complete).toHaveBeenCalledTimes(1)
  })
})

describe('summarize — provider error', () => {
  beforeEach(() => resetCacheState())
  it('surfaces a provider-error outcome without throwing', async () => {
    const complete = vi
      .fn()
      .mockRejectedValue({ code: 'server', message: 'down' })
    const { ctx } = makeCtx({ complete })
    const out = await summarize(ctx, {
      pageId: 'p',
      cleanContent: 'note',
      settings,
      ...configured
    })
    expect(out.ok).toBe(false)
    if (!out.ok) expect(out.error.code).toBe('provider-error')
  })
})

// AC2 regression (#483): a leaky provider's <thought> reasoning must never
// reach the persisted cache. ctx.ai.complete (context.ts) strips it in
// production; this test mirrors that by routing the raw leaky output through
// stripReasoningContent in the mock, then asserts BOTH the rendered result and
// the cache row written by summarize are clean.
describe('summarize — reasoning tags never persist (#483)', () => {
  beforeEach(() => resetCacheState())
  it('stores a clean summary + facets when the provider leaks a <thought> block', async () => {
    const complete = vi.fn().mockResolvedValue({
      content: stripReasoningContent(
        '<thought>* Input: … * Draft 1: …</thought>{"summary":"clean summary","tasks":["new task"]}'
      ),
      model: 'qwen3:30b'
    })
    const { ctx, execCalls } = makeCtx({ complete, latestRow: null })
    const out = await summarize(ctx, {
      pageId: 'p',
      cleanContent: 'a note',
      settings,
      ...configured
    })
    // Rendered result is clean.
    expect(out.ok).toBe(true)
    if (out.ok) {
      expect(out.result.summary).toBe('clean summary')
      expect(out.result.summary).not.toMatch(/<thought|<think|<reasoning/i)
      expect(out.result.tasks).toEqual(['new task'])
    }
    // Persisted cache row is clean (AC2). params[2] is the summary column.
    const put = execCalls.find((e) => /INSERT INTO summaries/.test(e.sql))
    expect(put).toBeDefined()
    expect(put?.params[2]).toBe('clean summary')
    expect(String(put?.params[2])).not.toMatch(/<thought|<think|<reasoning/i)
  })

  it('serves a clean summary from the cache on a second open (no regression after persist)', async () => {
    // First call: generate + persist a clean row derived from a leaky response.
    const leaky = vi.fn().mockResolvedValue({
      content: stripReasoningContent(
        '<think>planning</think>{"summary":"cached clean","tasks":[]}'
      ),
      model: 'qwen3:30b'
    })
    const { ctx } = makeCtx({ complete: leaky, latestRow: null })
    const first = await summarize(ctx, {
      pageId: 'p',
      cleanContent: 'a note',
      settings,
      ...configured
    })
    expect(first.ok && first.result.summary).toBe('cached clean')
  })
})
