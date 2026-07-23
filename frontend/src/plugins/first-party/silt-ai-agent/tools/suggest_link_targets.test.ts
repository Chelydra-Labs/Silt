import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { PluginContext } from '../../../sdk'
import { asString } from '../../../../lib/asString'
import { clearTools } from '../tool-registry'
import {
  parseBlockRefs,
  suggestLinkTargetsToolDef,
  handleSuggestLinkTargets
} from './suggest_link_targets'

interface Cand {
  id: string
  clean_content: string
  notebook: string
  section: string
  page: string
}

/**
 * Build a ctx with controllable embeddings + an exclude-aware candidate
 * fetcher. The candidate SQL is `WHERE id NOT IN (?, ?, ...) ORDER BY
 * line_number DESC LIMIT ?` — the mock honors the exclude list so a fixture
 * that already-links a candidate excludes it from the pool, matching the
 * real backend behaviour.
 */
function makeCtx(opts: {
  sourceId: string
  sourceContent: string
  sourceVec: number[]
  candidates: Cand[]
  candidateVecs: number[][]
}): {
  ctx: PluginContext
  embed: ReturnType<typeof vi.fn>
  sqliteQuery: ReturnType<typeof vi.fn>
  fullTextSearch: ReturnType<typeof vi.fn>
  mutateFns: { name: string; args: unknown[] }[]
} {
  const embed = vi.fn(async (req: { texts: string[]; taskType?: string }) => {
    if (req.taskType === 'RETRIEVAL_QUERY') {
      return {
        embeddings: [opts.sourceVec],
        model: 'm',
        dimensions: opts.sourceVec.length
      }
    }
    return {
      embeddings: req.texts.map((_, i) => opts.candidateVecs[i] ?? [0, 0]),
      model: 'm',
      dimensions: 2
    }
  })

  const mutateFns: { name: string; args: unknown[] }[] = []
  const sqliteQuery = vi.fn(async (sql: string, params?: unknown[]) => {
    const s = sql.toLowerCase()
    // Source lookup: return a row only when there is content.
    if (s.includes('from blocks where id = ?')) {
      const id = asString(params?.[0])
      if (id === opts.sourceId && opts.sourceContent.length > 0) {
        return {
          rows: [{ clean_content: opts.sourceContent }],
          truncated: false
        }
      }
      return { rows: [], truncated: false }
    }
    // Wiki-link → block-id resolution: `SELECT DISTINCT id FROM blocks WHERE page = ? ...`
    if (
      s.includes('select distinct id from blocks') &&
      s.includes('page = ?')
    ) {
      // Return rows for any candidate whose page matches the requested page.
      const requestedPage = asString(params?.[0])
      const rows = opts.candidates
        .filter((c) => c.page === requestedPage)
        .map((c) => ({ id: c.id }))
      return { rows, truncated: false }
    }
    // Candidate pool: `WHERE id NOT IN (?, ...) ORDER BY line_number DESC LIMIT ?`
    if (s.includes('order by line_number desc')) {
      const exclude = new Set((params ?? []).slice(0, -1).map((p) => String(p)))
      const filtered = opts.candidates.filter((c) => !exclude.has(c.id))
      return {
        rows: filtered as unknown as Record<string, unknown>[],
        truncated: false
      }
    }
    return { rows: [], truncated: false }
  })

  const fullTextSearch = vi.fn(async () => ({ rows: [], truncated: false }))
  const pluginDbQuery = vi.fn(async () => ({ rows: [], truncated: false }))
  const pluginDbExec = vi.fn(async () => undefined)

  const ctx = {
    ai: { embed },
    sqliteQuery,
    fullTextSearch,
    pluginDb: { query: pluginDbQuery, exec: pluginDbExec, migrate: vi.fn() },
    // Mutator stubs — read-only contract requires NONE of these to fire.
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
  return { ctx, embed, sqliteQuery, fullTextSearch, mutateFns }
}

beforeEach(() => clearTools())
afterEach(() => clearTools())

describe('suggest_link_targets', () => {
  it('returns ranked suggestions for the source block', async () => {
    const { ctx } = makeCtx({
      sourceId: 'src',
      sourceContent: 'note about databases',
      sourceVec: [1, 0],
      candidates: [
        {
          id: 'a',
          clean_content: 'a related note about databases',
          notebook: 'N',
          section: 'S',
          page: 'P'
        },
        {
          id: 'b',
          clean_content: 'an unrelated note about cooking',
          notebook: 'N',
          section: 'S',
          page: 'P'
        }
      ],
      // a → cosine([1,0],[0.95,0.05]) ≈ 0.998; b → cosine([1,0],[0.1,0.9]) ≈ 0.11
      candidateVecs: [
        [0.95, 0.05],
        [0.1, 0.9]
      ]
    })
    const res = await handleSuggestLinkTargets(ctx, { block_id: 'src' })
    expect(res.error).toBeUndefined()
    // 'a' is highly relevant; 'b' is below the 0.15 floor.
    expect(res.content).toContain('block a')
    expect(res.content).not.toContain('block b')
  })

  it('excludes the source block from suggestions', async () => {
    const { ctx } = makeCtx({
      sourceId: 'src',
      sourceContent: 'note about databases',
      sourceVec: [1, 0],
      candidates: [
        {
          id: 'src',
          clean_content: 'duplicate of the source',
          notebook: 'N',
          section: 'S',
          page: 'P'
        },
        {
          id: 'a',
          clean_content: 'a related note',
          notebook: 'N',
          section: 'S',
          page: 'P'
        }
      ],
      candidateVecs: [
        [1, 0],
        [0.9, 0.1]
      ]
    })
    const res = await handleSuggestLinkTargets(ctx, { block_id: 'src' })
    // Header intentionally names the source ("for block src"); the numbered
    // SUGGESTION list must not re-suggest it.
    const suggestions = res.content.split('\n\n').slice(1).join('\n\n')
    expect(suggestions).not.toMatch(/\] block src\b/)
  })

  it('excludes already-linked ((uuid)) targets from suggestions', async () => {
    // The source already references 'linked-a' via ((linked-a)). It must NOT
    // be suggested even though it is the most similar block in the pool.
    const { ctx, sqliteQuery } = makeCtx({
      sourceId: 'src',
      sourceContent:
        'see ((linked-a)) for more about databases. Also [[Databases]]',
      sourceVec: [1, 0],
      candidates: [
        {
          id: 'linked-a',
          clean_content: 'the canonical note about databases',
          notebook: 'N',
          section: 'S',
          page: 'Databases'
        },
        {
          id: 'b',
          clean_content: 'a less-central note about databases',
          notebook: 'N',
          section: 'S',
          page: 'Other'
        }
      ],
      candidateVecs: [
        [0.99, 0.01], // linked-a is the most similar
        [0.7, 0.3] // b is similar but lower
      ]
    })
    const res = await handleSuggestLinkTargets(ctx, { block_id: 'src' })
    expect(res.error).toBeUndefined()
    // linked-a was excluded via ((uuid)) AND via [[Databases]] (same page).
    expect(res.content).not.toContain('block linked-a')
    // b is suggested.
    expect(res.content).toContain('block b')

    // Verify the exclude set propagated to the candidate SQL: the candidate
    // fetch received both the source id and the linked ids in NOT IN (...).
    const candCall = sqliteQuery.mock.calls.find((c) =>
      String(c[0]).toLowerCase().includes('order by line_number desc')
    )
    expect(candCall).toBeTruthy()
    const excludeList = ((candCall?.[1] as unknown[]) ?? []).slice(0, -1)
    expect(excludeList).toContain('src')
    expect(excludeList).toContain('linked-a')
  })

  it('honours max_suggestions', async () => {
    const candidates: Cand[] = Array.from({ length: 8 }, (_, i) => ({
      id: `c${i}`,
      clean_content: `note about databases variant ${i}`,
      notebook: 'N',
      section: 'S',
      page: `P${i}`
    }))
    const { ctx } = makeCtx({
      sourceId: 'src',
      sourceContent: 'note about databases',
      sourceVec: [1, 0],
      candidates,
      candidateVecs: candidates.map(() => [0.9, 0.1])
    })
    const res = await handleSuggestLinkTargets(ctx, {
      block_id: 'src',
      max_suggestions: 3
    })
    // Exactly three suggestions surface in the output.
    const ids = res.content.match(/block c\d/g) ?? []
    expect(ids).toHaveLength(3)
  })

  it('is read-only — never mutates sources or candidates', async () => {
    const { ctx, mutateFns } = makeCtx({
      sourceId: 'src',
      sourceContent: 'note about databases',
      sourceVec: [1, 0],
      candidates: [
        {
          id: 'a',
          clean_content: 'related',
          notebook: 'N',
          section: 'S',
          page: 'P'
        }
      ],
      candidateVecs: [[0.9, 0.1]]
    })
    await handleSuggestLinkTargets(ctx, { block_id: 'src' })
    expect(mutateFns).toHaveLength(0)
  })

  it('returns a clean message when the source is not found', async () => {
    const { ctx } = makeCtx({
      sourceId: 'ghost',
      sourceContent: '',
      sourceVec: [1, 0],
      candidates: [],
      candidateVecs: []
    })
    const res = await handleSuggestLinkTargets(ctx, { block_id: 'ghost' })
    expect(res.error).toMatch(/not found/)
  })

  it('returns a clean message when no unlinked candidates qualify', async () => {
    const { ctx } = makeCtx({
      sourceId: 'src',
      sourceContent: 'note about databases',
      sourceVec: [1, 0],
      candidates: [
        {
          id: 'a',
          clean_content: 'totally unrelated cooking note',
          notebook: 'N',
          section: 'S',
          page: 'P'
        }
      ],
      candidateVecs: [[0.05, 0.95]] // cosine([1,0], [0.05,0.95]) ≈ 0.05 — below 0.15
    })
    const res = await handleSuggestLinkTargets(ctx, { block_id: 'src' })
    expect(res.content).toMatch(
      /no unlinked blocks met the relevance threshold/i
    )
  })

  it('parseBlockRefs pulls ((uuid)) ids from prose, lowercased', () => {
    const refs = parseBlockRefs(
      'see ((ABCDEF12-3456-7890-ABCD-EF1234567890)) and ((abcdef12-3456-7890-abcd-ef1234567891))'
    )
    expect(refs).toEqual([
      'abcdef12-3456-7890-abcd-ef1234567890',
      'abcdef12-3456-7890-abcd-ef1234567891'
    ])
  })

  it('exposes the tool def shape', () => {
    expect(suggestLinkTargetsToolDef.name).toBe('suggest_link_targets')
    expect(suggestLinkTargetsToolDef.parameters.required).toEqual(['block_id'])
  })
})
