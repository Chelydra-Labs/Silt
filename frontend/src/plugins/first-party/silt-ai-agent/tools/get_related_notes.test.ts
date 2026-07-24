import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { PluginContext } from '../../../sdk'
import { asString } from '../../../../lib/asString'
import { clearTools } from '../tool-registry'
import {
  getRelatedNotesToolDef,
  handleGetRelatedNotes
} from './get_related_notes'

interface Cand {
  id: string
  clean_content: string
  notebook: string
  section: string
  page: string
}

/**
 * Build a ctx with controllable embeddings. Each candidate is mapped (in
 * fixture order) to a vector; the source block is mapped to sourceVec. FTS
 * and cache reads return empty by default so the on-demand path is exercised.
 */
function makeCtx(opts: {
  sourceId: string
  sourceContent: string
  sourceVec: number[]
  candidates: Cand[]
  candidateVecs: number[][]
  recentRows?: Cand[]
}): {
  ctx: PluginContext
  embed: ReturnType<typeof vi.fn>
  sqliteQuery: ReturnType<typeof vi.fn>
  pluginDbQuery: ReturnType<typeof vi.fn>
  pluginDbExec: ReturnType<typeof vi.fn>
} {
  const recent = opts.recentRows ?? opts.candidates

  const embed = vi.fn(async (req: { texts: string[]; taskType?: string }) => {
    if (req.taskType === 'RETRIEVAL_QUERY') {
      return {
        embeddings: [opts.sourceVec],
        model: 'm',
        dimensions: opts.sourceVec.length
      }
    }
    // RETRIEVAL_DOCUMENT batch — serve vectors in fixture order.
    return {
      embeddings: req.texts.map((_, i) => opts.candidateVecs[i] ?? [0, 0]),
      model: 'm',
      dimensions: 2
    }
  })

  const sqliteQuery = vi.fn(async (sql: string, params?: unknown[]) => {
    if (sql.includes('FROM blocks WHERE id = ?')) {
      const id = asString(params?.[0])
      // Source lookup: return a row only when there is content to compare.
      if (id === opts.sourceId && opts.sourceContent.length > 0) {
        return {
          rows: [{ clean_content: opts.sourceContent }],
          truncated: false
        }
      }
      return { rows: [], truncated: false }
    }
    if (sql.includes('ORDER BY line_number DESC')) {
      // The real SQL excludes the source via `WHERE id != ?` — the mock
      // honors the same filter so a fixture that includes the source for
      // readability does not surface it as a candidate.
      const excludeId = asString(params?.[0])
      const filtered = recent.filter((c) => c.id !== excludeId)
      return {
        rows: filtered as unknown as Record<string, unknown>[],
        truncated: false
      }
    }
    return { rows: [], truncated: false }
  })

  const pluginDbQuery = vi.fn(async () => ({ rows: [], truncated: false }))
  const pluginDbExec = vi.fn(async () => undefined)

  const ctx = {
    ai: { embed },
    sqliteQuery,
    pluginDb: { query: pluginDbQuery, exec: pluginDbExec, migrate: vi.fn() },
    fullTextSearch: vi.fn(async () => ({ rows: [], truncated: false }))
  } as unknown as PluginContext
  return { ctx, embed, sqliteQuery, pluginDbQuery, pluginDbExec }
}

beforeEach(() => clearTools())
afterEach(() => clearTools())

describe('get_related_notes', () => {
  it('excludes the source block from results', async () => {
    const { ctx } = makeCtx({
      sourceId: 'src',
      sourceContent: 'source note about databases',
      sourceVec: [1, 0],
      candidates: [
        {
          id: 'src',
          clean_content: 'source note about databases',
          notebook: 'N',
          section: 'S',
          page: 'P'
        },
        {
          id: 'a',
          clean_content: 'a note about databases',
          notebook: 'N',
          section: 'S',
          page: 'P'
        }
      ],
      candidateVecs: [[0.95, 0.05]]
    })
    const res = await handleGetRelatedNotes(ctx, { block_id: 'src' })
    expect(res.error).toBeUndefined()
    expect(res.content).not.toContain('block src')
    expect(res.content).toContain('block a')
  })

  it('filters out results below min_score', async () => {
    const { ctx } = makeCtx({
      sourceId: 'src',
      sourceContent: 'source note about databases',
      sourceVec: [1, 0],
      candidates: [
        {
          id: 'similar',
          clean_content: 'a note about databases',
          notebook: 'N',
          section: 'S',
          page: 'P'
        },
        {
          id: 'far',
          clean_content: 'a note about cooking recipes',
          notebook: 'N',
          section: 'S',
          page: 'P'
        }
      ],
      // similar → cosine([1,0],[0.9,0.1]) ≈ 0.99; far → cosine([1,0],[0.1,0.9]) ≈ 0.11
      candidateVecs: [
        [0.9, 0.1],
        [0.1, 0.9]
      ]
    })
    const res = await handleGetRelatedNotes(ctx, {
      block_id: 'src',
      min_score: 0.5
    })
    expect(res.content).toContain('block similar')
    expect(res.content).not.toContain('block far')
  })

  it('honours the top_k limit', async () => {
    const candidates: Cand[] = Array.from({ length: 5 }, (_, i) => ({
      id: `c${i}`,
      clean_content: `note about databases number ${i}`,
      notebook: 'N',
      section: 'S',
      page: 'P'
    }))
    const { ctx } = makeCtx({
      sourceId: 'src',
      sourceContent: 'note about databases',
      sourceVec: [1, 0],
      candidates,
      // All five candidates align with the source → all above min_score.
      candidateVecs: candidates.map(() => [0.95, 0.05])
    })
    const res = await handleGetRelatedNotes(ctx, {
      block_id: 'src',
      top_k: 2,
      min_score: 0
    })
    expect(res.content).toMatch(/2 related block/)
    // Exactly two block ids appear in the formatted output.
    const ids = res.content.match(/block c\d/g) ?? []
    expect(ids).toHaveLength(2)
  })

  it('embeds candidates on-demand when the cache is empty', async () => {
    const { ctx, embed, pluginDbQuery, pluginDbExec } = makeCtx({
      sourceId: 'src',
      sourceContent: 'source note about databases',
      sourceVec: [1, 0],
      candidates: [
        {
          id: 'a',
          clean_content: 'a note about databases',
          notebook: 'N',
          section: 'S',
          page: 'P'
        }
      ],
      candidateVecs: [[0.9, 0.1]]
    })
    await handleGetRelatedNotes(ctx, { block_id: 'src' })
    // Source embed (QUERY) + one candidate batch (DOCUMENT).
    expect(embed).toHaveBeenCalledTimes(2)
    // Cache read returned no rows.
    expect(pluginDbQuery).toHaveBeenCalled()
    // Cache write persisted the on-demand embedding.
    expect(pluginDbExec).toHaveBeenCalled()
    const writeCalls = pluginDbExec.mock.calls.map((c) => String(c[0]))
    expect(writeCalls.some((s) => s.includes('block_vectors'))).toBe(true)
  })

  it('reuses cached vectors and skips re-embedding', async () => {
    const candidate: Cand = {
      id: 'a',
      clean_content: 'a note about databases',
      notebook: 'N',
      section: 'S',
      page: 'P'
    }
    const cachedVec = [0.9, 0.1]

    // Lazy hash-of-content check: replicate the FNV-1a the tool uses so the
    // cache hit matches. (Compute via the same prime arithmetic.)
    function hashOf(text: string): string {
      let h = 0x811c9dc5
      for (let i = 0; i < text.length; i++) {
        h ^= text.charCodeAt(i)
        h = (h + ((h << 1) + (h << 4) + (h << 7) + (h << 8) + (h << 24))) >>> 0
      }
      return h.toString(16)
    }

    const embed = vi.fn(async (req: { texts: string[]; taskType?: string }) => {
      if (req.taskType === 'RETRIEVAL_QUERY') {
        return { embeddings: [[1, 0]], model: 'm', dimensions: 2 }
      }
      return {
        embeddings: req.texts.map(() => [0, 0]),
        model: 'm',
        dimensions: 2
      }
    })
    const sqliteQuery = vi.fn(async (sql: string, _params?: unknown[]) => {
      if (sql.includes('FROM blocks WHERE id = ?')) {
        return {
          rows: [{ clean_content: 'source note' }],
          truncated: false
        }
      }
      return {
        rows: [candidate] as unknown as Record<string, unknown>[],
        truncated: false
      }
    })
    const pluginDbQuery = vi.fn(async (_sql: string, params?: unknown[]) => {
      // Vector cache lookup returns a hit for candidate 'a'.
      const ids = (params ?? []) as string[]
      const rows: Record<string, unknown>[] = ids.map((id) => ({
        block_id: id,
        content_hash: hashOf(candidate.clean_content),
        vector: JSON.stringify(cachedVec)
      }))
      return { rows, truncated: false }
    })
    const pluginDbExec = vi.fn(async () => undefined)
    const ctx = {
      ai: { embed },
      sqliteQuery,
      pluginDb: { query: pluginDbQuery, exec: pluginDbExec, migrate: vi.fn() },
      fullTextSearch: vi.fn(async () => ({ rows: [], truncated: false }))
    } as unknown as PluginContext

    const res = await handleGetRelatedNotes(ctx, {
      block_id: 'src',
      min_score: 0
    })
    expect(res.error).toBeUndefined()
    expect(res.content).toContain('block a')
    // Only the source was embedded — the candidate came from the cache.
    expect(embed).toHaveBeenCalledTimes(1)
    // No cache write happened (no misses).
    expect(pluginDbExec).not.toHaveBeenCalled()
  })

  it('returns a clean message when source is not found', async () => {
    const { ctx } = makeCtx({
      sourceId: 'ghost',
      sourceContent: '',
      sourceVec: [1, 0],
      candidates: [],
      candidateVecs: []
    })
    const res = await handleGetRelatedNotes(ctx, { block_id: 'ghost' })
    expect(res.error).toMatch(/not found/)
  })

  it('returns a clean message when no candidates match', async () => {
    const { ctx } = makeCtx({
      sourceId: 'src',
      sourceContent: 'source note',
      sourceVec: [1, 0],
      candidates: [
        {
          id: 'a',
          clean_content: 'cooking note',
          notebook: 'N',
          section: 'S',
          page: 'P'
        }
      ],
      candidateVecs: [[0, 1]]
    })
    const res = await handleGetRelatedNotes(ctx, {
      block_id: 'src',
      min_score: 0.9
    })
    expect(res.content).toMatch(/no related blocks met/i)
  })

  it('exposes the tool def shape', () => {
    expect(getRelatedNotesToolDef.name).toBe('get_related_notes')
    expect(getRelatedNotesToolDef.parameters.required).toEqual(['block_id'])
  })
})
