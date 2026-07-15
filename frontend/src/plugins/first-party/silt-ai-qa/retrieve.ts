// End-to-end hybrid retrieval for silt-ai-qa (#225, #620).

import type { PluginContext } from '../../sdk'
import { fuseHybrid, trimToBudget, type RankedHit } from './hybrid'
import { vectorSearch } from './embed_index'
import type { QASettings, RetrievedPassage } from './types'

/** Typed retrieval failure so the panel can show an error, not false no-results. */
export class RetrieveError extends Error {
  constructor(
    message: string,
    readonly cause?: unknown
  ) {
    super(message)
    this.name = 'RetrieveError'
  }
}

/** Map fullTextSearch rows to RankedHit (block id + snippet). */
export function ftsRowsToHits(rows: Record<string, unknown>[]): RankedHit[] {
  return rows.map((r) => {
    // Strip FTS highlight markers before RAG so the model sees plain text.
    const raw = String(r.snippet ?? r.clean_content ?? r.raw_content ?? '')
    const text = raw.replace(/<\/?mark>/gi, '')
    return {
      blockId: String(r.id ?? r.block_id ?? ''),
      notebook: String(r.notebook ?? ''),
      section: String(r.section ?? ''),
      page: String(r.page ?? ''),
      lineNumber: Number(r.line_number ?? r.lineNumber ?? 0),
      text,
      score: Number(r.rank ?? r.score ?? 0)
    }
  })
}

function cosine(a: number[], b: number[]): number {
  const n = Math.min(a.length, b.length)
  if (n === 0) return 0
  let dot = 0
  let na = 0
  let nb = 0
  for (let i = 0; i < n; i++) {
    dot += a[i] * b[i]
    na += a[i] * a[i]
    nb += b[i] * b[i]
  }
  const denom = Math.sqrt(na) * Math.sqrt(nb)
  return denom === 0 ? 0 : dot / denom
}

/**
 * Re-score fused candidates by query–passage cosine similarity.
 * Fail-open: returns original order on any embed error.
 * queryVec may be pre-computed by the caller to avoid a redundant embed.
 */
export async function rerankPassages(
  ctx: PluginContext,
  query: string,
  passages: RetrievedPassage[],
  queryVec?: number[]
): Promise<RetrievedPassage[]> {
  if (passages.length === 0) return passages
  try {
    const qVec =
      queryVec ??
      (
        await ctx.ai.embed({
          texts: [query],
          taskType: 'RETRIEVAL_QUERY'
        })
      ).embeddings[0]
    if (!qVec) return passages

    const BATCH = 16
    const scores: number[] = new Array(passages.length).fill(0)
    for (let i = 0; i < passages.length; i += BATCH) {
      const batch = passages.slice(i, i + BATCH)
      const res = await ctx.ai.embed({
        texts: batch.map((p) => p.text),
        taskType: 'RETRIEVAL_DOCUMENT'
      })
      for (let j = 0; j < batch.length; j++) {
        const vec = res.embeddings[j]
        scores[i + j] = vec ? cosine(qVec, vec) : 0
      }
    }

    const ranked = passages
      .map((p, i) => ({ p, score: scores[i] }))
      .sort((a, b) => b.score - a.score)
      .map((r, i) => ({
        ...r.p,
        score: r.score,
        citeIndex: i + 1
      }))
    return ranked
  } catch (e) {
    console.warn('silt-ai-qa: rerank failed, falling back to RRF order:', e)
    return passages
  }
}

export async function hybridRetrieve(
  ctx: PluginContext,
  question: string,
  settings: QASettings
): Promise<RetrievedPassage[]> {
  const k = Math.max(settings.top_k, 1)
  const fetchK = settings.rerank_enabled
    ? Math.max(k * 5, 50)
    : Math.max(k * 2, 10)

  // Embed the query once when reranking so the same vector feeds both
  // vectorSearch and rerankPassages (avoids a redundant API call).
  let queryVec: number[] | undefined
  if (settings.rerank_enabled) {
    try {
      queryVec = (
        await ctx.ai.embed({
          texts: [question],
          taskType: 'RETRIEVAL_QUERY'
        })
      ).embeddings[0]
    } catch {
      // vectorSearch will embed internally as fallback.
    }
  }

  let vecHits: RankedHit[] = []
  let ftsRows: Record<string, unknown>[] = []
  let vecErr: unknown = null
  let ftsErr: unknown = null

  const [vecSettled, ftsSettled] = await Promise.allSettled([
    vectorSearch(ctx, question, fetchK, queryVec),
    ctx.fullTextSearch(question)
  ])

  if (vecSettled.status === 'fulfilled') {
    vecHits = vecSettled.value
  } else {
    vecErr = vecSettled.reason
  }
  if (ftsSettled.status === 'fulfilled') {
    ftsRows = ftsSettled.value.rows ?? []
  } else {
    ftsErr = ftsSettled.reason
  }

  // Both sides failed → hard error (do not pretend "no results").
  if (vecErr && ftsErr) {
    throw new RetrieveError(
      `Search failed: ${errMsg(vecErr)}; ${errMsg(ftsErr)}`,
      vecErr
    )
  }
  // Vector failed and FTS empty → surface the vector error.
  if (vecErr && ftsRows.length === 0) {
    throw new RetrieveError(`Semantic search failed: ${errMsg(vecErr)}`, vecErr)
  }
  // FTS failed and vector empty → surface the FTS error.
  if (ftsErr && vecHits.length === 0) {
    throw new RetrieveError(`Keyword search failed: ${errMsg(ftsErr)}`, ftsErr)
  }

  const ftsHits = ftsRowsToHits(ftsRows).slice(0, fetchK)
  // When reranking, fuse a wider candidate set first; otherwise fuse to k.
  const fuseTopK = settings.rerank_enabled ? fetchK : k
  let fused = fuseHybrid(vecHits, ftsHits, {
    hybridWeight: settings.hybrid_weight,
    topK: fuseTopK,
    minScore: settings.min_score
  })

  if (settings.rerank_enabled) {
    fused = await rerankPassages(ctx, question, fused, queryVec)
    fused = fused.slice(0, k).map((p, i) => ({ ...p, citeIndex: i + 1 }))
  }

  fused = trimToBudget(fused, settings.max_context_chars)
  return fused.filter((p) => p.text.length > 0)
}

function errMsg(e: unknown): string {
  if (e instanceof Error) return e.message
  return String(e)
}
