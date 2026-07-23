// Suggest related notes via on-demand embeddings (#232).
// Does not read silt-ai-qa plugin DB — ranks candidates with ctx.ai.embed.

import type { PluginContext } from '../../../sdk'
import { asString } from '../../../../lib/asString'
import { createProposal } from '../proposal/model'
import { cosineSimilarity, stripIdentityComments } from '../text'
import type {
  AssistantSettings,
  Proposal,
  RelatedSuggestion,
  ScopeContext
} from '../types'

interface Candidate {
  id: string
  text: string
  notebook?: string
  section?: string
  page?: string
}

export async function loadRelatedCandidates(
  ctx: PluginContext,
  scope: ScopeContext,
  limit: number
): Promise<Candidate[]> {
  const { rows } = await ctx.sqliteQuery(
    `SELECT id, clean_content, notebook, section, page
       FROM blocks
      WHERE clean_content IS NOT NULL
        AND length(clean_content) > 20
        AND NOT (notebook = ? AND section = ? AND page = ?)
      ORDER BY line_number DESC
      LIMIT ?`,
    [scope.notebook || '', scope.section || '', scope.page || '', limit * 3]
  )

  // Prefer FTS hits when query has content.
  let ftsRows: typeof rows = []
  const q = scope.inputText.slice(0, 200).replace(/"/g, ' ').trim()
  if (q.length >= 3) {
    try {
      const fts = await ctx.fullTextSearch(q)
      ftsRows = fts.rows ?? []
    } catch {
      ftsRows = []
    }
  }

  const byId = new Map<string, Candidate>()
  for (const r of [...ftsRows, ...rows]) {
    const id = asString(r.id)
    if (!id || byId.has(id)) continue
    if (id === scope.blockId || id === scope.targetBlockId) continue
    const text = stripIdentityComments(asString(r.clean_content)).slice(0, 800)
    if (text.length < 20) continue
    byId.set(id, {
      id,
      text,
      notebook: typeof r.notebook === 'string' ? r.notebook : undefined,
      section: typeof r.section === 'string' ? r.section : undefined,
      page: typeof r.page === 'string' ? r.page : undefined
    })
    if (byId.size >= limit) break
  }
  return [...byId.values()]
}

export function rankByEmbedding(
  queryVec: number[],
  candidates: Candidate[],
  embeddings: number[][],
  maxResults: number
): RelatedSuggestion[] {
  const scored: RelatedSuggestion[] = []
  for (let i = 0; i < candidates.length; i++) {
    const emb = embeddings[i]
    if (!emb) continue
    const score = cosineSimilarity(queryVec, emb)
    const c = candidates[i]
    scored.push({
      blockId: c.id,
      snippet: c.text.slice(0, 160).replace(/\s+/g, ' '),
      notebook: c.notebook,
      section: c.section,
      page: c.page,
      score
    })
  }
  scored.sort((a, b) => b.score - a.score)
  return scored.filter((s) => s.score > 0.15).slice(0, maxResults)
}

export async function runSuggestRelated(
  ctx: PluginContext,
  scope: ScopeContext,
  settings: AssistantSettings
): Promise<Proposal> {
  const queryText = scope.inputText.slice(0, 1500)
  if (!queryText.trim()) {
    return createProposal({
      actionId: 'suggest-related',
      kind: 'insert-links',
      scope,
      status: 'error',
      errorMessage: 'No text to match against related notes'
    })
  }

  const candidates = await loadRelatedCandidates(
    ctx,
    scope,
    settings.related_candidate_limit
  )
  if (candidates.length === 0) {
    return createProposal({
      actionId: 'suggest-related',
      kind: 'insert-links',
      scope,
      related: [],
      warning: 'No candidate notes found to compare.',
      status: 'ready'
    })
  }

  const queryEmb = await ctx.ai.embed({ texts: [queryText] })
  const qVec = queryEmb.embeddings[0]
  if (!qVec?.length) {
    return createProposal({
      actionId: 'suggest-related',
      kind: 'insert-links',
      scope,
      status: 'error',
      errorMessage: 'Embedding model returned an empty vector'
    })
  }

  // Batch embed candidates.
  const batchSize = 16
  const allEmb: number[][] = []
  for (let i = 0; i < candidates.length; i += batchSize) {
    const batch = candidates.slice(i, i + batchSize)
    const res = await ctx.ai.embed({ texts: batch.map((c) => c.text) })
    allEmb.push(...res.embeddings)
  }

  const related = rankByEmbedding(
    qVec,
    candidates,
    allEmb,
    settings.max_related_suggestions
  )
  const md = related.map((r) => `- ((${r.blockId})) — ${r.snippet}`).join('\n')

  return createProposal({
    actionId: 'suggest-related',
    kind: 'insert-links',
    scope,
    proposedMarkdown: md,
    related,
    warning:
      related.length === 0 ? 'No sufficiently similar notes found.' : undefined,
    status: 'ready'
  })
}
