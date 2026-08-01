// Agent tool #597 — search_notes.
//
// Hybrid retrieval exposed to the agent. Primary channel is FTS5 (via shared
// hybridRetrieve). When FTS returns nothing, a semantic fallback ranks recent
// + keyword-recalled candidates by cosine similarity (same path as
// get_related_notes) so a single empty FTS channel cannot zero the tool.

import type { PluginContext } from '../../../sdk'
import { asString } from '../../../../lib/asString'
import {
  hybridRetrieve,
  type RetrieveOptions,
  type VectorSearchFn
} from '../../../shared/retrieval/retrieve'
import type {
  RankedHit,
  RetrievedPassage
} from '../../../shared/retrieval/hybrid'
import type { ToolResult } from '../tool-registry'
import { breadcrumb, clampInt } from './_util'
import { embedOne, gatherCandidates, rankCandidates } from './_embedding'

export const searchNotesToolDef = {
  name: 'search_notes',
  description:
    'Search the note vault by keyword and meaning. Returns ranked blocks ' +
    'with block_id, location, snippet, and score. Use filters to narrow to a ' +
    'notebook, section, or block type.',
  parameters: {
    type: 'object',
    required: ['query'],
    properties: {
      query: {
        type: 'string',
        description: 'Natural-language or keyword query.'
      },
      top_k: {
        type: 'integer',
        description: 'Max results to return (default 10).',
        minimum: 1,
        maximum: 50
      },
      filters: {
        type: 'object',
        description: 'Optional narrowing filters.',
        properties: {
          notebook: { type: 'string' },
          section: { type: 'string' },
          type: {
            type: 'string',
            description: "Block type: 'TASK', 'NOTE', or 'HEADER'."
          }
        }
      }
    }
  }
}

interface SearchFilters {
  notebook?: string
  section?: string
  type?: string
}

/** No agent vec0 index — hybrid primary path is pure FTS (+ embed rerank). */
const emptyVectorSearch: VectorSearchFn = (): Promise<RankedHit[]> =>
  Promise.resolve([])

const SEMANTIC_MIN_SCORE = 0.5

export async function handleSearchNotes(
  ctx: PluginContext,
  args: Record<string, unknown>
): Promise<ToolResult> {
  const query = asString(args.query).trim()
  if (!query) {
    return { content: '', error: 'query must not be empty' }
  }
  const topK = clampInt(args.top_k, 10, 1, 50)
  const filters = normalizeFilters(args.filters)

  let degradedNote = ''
  const opts: RetrieveOptions = {
    top_k: topK,
    hybrid_weight: 0, // primary: pure FTS recall
    min_score: 0,
    // Bound tool→model context: agent multi-turn history grows fast; 100k was
    // a quiet chat-token burner. Align with a generous QA-scale budget.
    max_context_chars: 32_000,
    rerank_enabled: true,
    filterPassages:
      filters.notebook || filters.section || filters.type
        ? async (passages) => filterPassages(ctx, passages, filters)
        : undefined,
    onDegraded: (info) => {
      degradedNote = `Retrieval degraded (${info.side}): ${info.message}`
      void ctx.ai.auditEvent?.({
        kind: 'search_degraded',
        tool: 'search_notes',
        side: info.side,
        status: 'degraded'
      })
    }
  }

  let passages: RetrievedPassage[]
  try {
    passages = await hybridRetrieve(ctx, query, opts, emptyVectorSearch)
  } catch (e) {
    const msg = e instanceof Error ? e.message : String(e)
    return { content: '', error: `search failed: ${msg}` }
  }

  // Semantic fallback when FTS (and rerank) produced nothing.
  // Embed/provider failures degrade to no-results (not a tool error) so the
  // agent loop can continue — FTS already returned empty.
  if (passages.length === 0) {
    try {
      passages = await semanticFallback(ctx, query, topK, filters)
      if (passages.length > 0) {
        degradedNote =
          'Keyword search returned no matches; results are from semantic fallback.'
        void ctx.ai.auditEvent?.({
          kind: 'search_degraded',
          tool: 'search_notes',
          side: 'fts',
          status: 'degraded',
          message: 'fts_empty_semantic_fallback'
        })
      }
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e)
      degradedNote = `Semantic fallback unavailable: ${msg}`
      void ctx.ai.auditEvent?.({
        kind: 'search_degraded',
        tool: 'search_notes',
        side: 'vector',
        status: 'degraded',
        message: msg
      })
      passages = []
    }
  }

  if (passages.length === 0) {
    return {
      content: degradedNote
        ? `${degradedNote}\n\nNo matching notes found.`
        : 'No matching notes found.'
    }
  }

  const lines = passages.map((p) => {
    const loc = breadcrumb(p.notebook, p.section, p.page)
    const snippet = p.text.length > 280 ? `${p.text.slice(0, 280)}…` : p.text
    return [
      `[${p.citeIndex}] block ${p.blockId}`,
      `    location: ${loc}`,
      `    score: ${p.score.toFixed(4)}`,
      `    ${snippet.replace(/\n/g, '\n    ')}`
    ].join('\n')
  })
  const header = degradedNote
    ? `${degradedNote}\n\n${passages.length} result(s):\n\n`
    : `${passages.length} result(s):\n\n`
  return {
    content: `${header}${lines.join('\n\n')}`,
    evidence: passages.map((p) => ({
      citationIndex: p.citeIndex,
      blockId: p.blockId,
      notebook: p.notebook,
      section: p.section,
      page: p.page,
      lineNumber: p.lineNumber,
      snippet: p.text.slice(0, 200),
      title: breadcrumb(p.notebook, p.section, p.page)
    }))
  }
}

/**
 * Rank recent + FTS-keyword candidates by cosine similarity to the query.
 * Used only when the primary FTS channel returns no passages.
 */
async function semanticFallback(
  ctx: PluginContext,
  query: string,
  topK: number,
  filters: SearchFilters
): Promise<RetrievedPassage[]> {
  // Non-empty query already validated by the caller. Empty vector means the
  // embedding provider failed or returned unusable data — surface that so the
  // outer catch can emit search_degraded instead of a silent no-results.
  const queryVec = await embedOne(ctx, query, 'RETRIEVAL_QUERY')
  if (queryVec.length === 0) {
    throw new Error('embedding provider unavailable or returned empty vector')
  }

  const candidates = await gatherCandidates(ctx, new Set(), query)
  let scoped = candidates
  if (filters.notebook) {
    scoped = scoped.filter((c) => c.notebook === filters.notebook)
  }
  if (filters.section) {
    scoped = scoped.filter((c) => c.section === filters.section)
  }

  const ranked = await rankCandidates(ctx, queryVec, scoped, {
    minScore: SEMANTIC_MIN_SCORE,
    topK
  })

  let passages: RetrievedPassage[] = ranked.map((r, i) => ({
    blockId: r.block.id,
    notebook: r.block.notebook,
    section: r.block.section,
    page: r.block.page,
    lineNumber: 0,
    text: r.block.clean_content.trim(),
    score: r.score,
    citeIndex: i + 1
  }))

  if (filters.type && passages.length > 0) {
    passages = await filterPassages(ctx, passages, { type: filters.type })
    passages = passages.map((p, i) => ({ ...p, citeIndex: i + 1 }))
  }
  return passages
}

function normalizeFilters(raw: unknown): SearchFilters {
  if (typeof raw !== 'object' || raw === null) return {}
  const f = raw as Record<string, unknown>
  return {
    notebook: typeof f.notebook === 'string' ? f.notebook : undefined,
    section: typeof f.section === 'string' ? f.section : undefined,
    type: typeof f.type === 'string' ? f.type.toUpperCase() : undefined
  }
}

/**
 * Apply notebook/section (field) + type (resolved via one query) filters to the
 * fused candidate set. Runs before rerank so out-of-scope text is never embedded
 * and cannot displace in-scope hits from top_k.
 */
async function filterPassages(
  ctx: PluginContext,
  passages: RetrievedPassage[],
  filters: SearchFilters
): Promise<RetrievedPassage[]> {
  let filtered = passages
  if (filters.notebook) {
    filtered = filtered.filter((p) => p.notebook === filters.notebook)
  }
  if (filters.section) {
    filtered = filtered.filter((p) => p.section === filters.section)
  }
  if (filters.type && filtered.length > 0) {
    const ids = filtered.map((p) => p.blockId)
    const placeholders = ids.map(() => '?').join(',')
    const { rows } = await ctx.sqliteQuery(
      `SELECT id, type FROM blocks WHERE id IN (${placeholders})`,
      ids
    )
    const typeById = new Map(rows.map((r) => [String(r.id), String(r.type)]))
    filtered = filtered.filter((p) => typeById.get(p.blockId) === filters.type)
  }
  return filtered
}
