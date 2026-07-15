// Agent tool #597 — search_notes.
//
// Hybrid retrieval exposed to the agent. The retrieval pipeline is shared with
// silt-ai-qa (#597 extract); the agent injects its own vector search. The agent
// does not maintain a vector index today, so it relies on FTS5 recall refined
// by embedding-based rerank (ctx.ai.embed) — no index maintenance, still
// semantically ranked. Results are formatted as readable text the model can
// cite: block id, location, snippet, score.

import type { PluginContext } from '../../../sdk'
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

/**
 * The agent has no vector index, so its vector search contributes nothing and
 * hybrid retrieval falls open to FTS5. Returning [] (not throwing) keeps the
 * pipeline on its FTS-only path; rerank (via ctx.ai.embed) still refines the
 * keyword hits. If a future agent maintains its own index, swap this for a real
 * KNN implementation bound to ctx.pluginDb.
 */
const agentVectorSearch: VectorSearchFn = async (): Promise<RankedHit[]> => []

export async function handleSearchNotes(
  ctx: PluginContext,
  args: Record<string, unknown>
): Promise<ToolResult> {
  const query = String(args.query ?? '').trim()
  if (!query) {
    return { content: '', error: 'query must not be empty' }
  }
  const topK = clampInt(args.top_k, 10, 1, 50)
  const filters = normalizeFilters(args.filters)

  // Rerank on (embedding) so the model sees semantically ranked hits even
  // without a vector index. Generous char budget: the tool formats its own
  // output, so do not let the RAG trim stage drop results prematurely.
  const opts: RetrieveOptions = {
    top_k: topK,
    hybrid_weight: 0, // no agent vector index → pure FTS recall
    min_score: 0,
    max_context_chars: 100_000,
    rerank_enabled: true
  }

  let passages
  try {
    passages = await hybridRetrieve(ctx, query, opts, agentVectorSearch)
  } catch (e) {
    const msg = e instanceof Error ? e.message : String(e)
    return { content: '', error: `search failed: ${msg}` }
  }

  if (filters.type) {
    passages = await filterByType(ctx, passages, filters.type)
  }
  if (filters.notebook) {
    passages = passages.filter((p) => p.notebook === filters.notebook)
  }
  if (filters.section) {
    passages = passages.filter((p) => p.section === filters.section)
  }

  if (passages.length === 0) {
    return { content: 'No matching notes found.' }
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
  return { content: `${passages.length} result(s):\n\n${lines.join('\n\n')}` }
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

/** Resolve block types in one query and drop passages that do not match. */
async function filterByType(
  ctx: PluginContext,
  passages: RetrievedPassage[],
  type: string
): Promise<RetrievedPassage[]> {
  const ids = passages.map((p) => p.blockId)
  if (ids.length === 0) return passages
  const placeholders = ids.map(() => '?').join(',')
  const { rows } = await ctx.sqliteQuery(
    `SELECT id, type FROM blocks WHERE id IN (${placeholders})`,
    ids
  )
  const typeById = new Map(rows.map((r) => [String(r.id), String(r.type)]))
  return passages.filter((p) => typeById.get(p.blockId) === type)
}
