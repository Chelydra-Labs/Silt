// Shared hybrid retrieval orchestration (#597).
//
// hybridRetrieve + rerank + FTS-row mapping moved out of silt-ai-qa. The
// fusion algorithm (fuseHybrid) is in hybrid.ts; the vector index is INJECTED
// via VectorSearchFn so each plugin owns its plugin.db storage and there is no
// process-global index state shared across plugins. silt-ai-qa injects its own
// vectorSearch; silt-ai-agent injects a fn over its agent-owned vec0 index
// (shared embed-index helpers, separate plugin.db instance) and fails open to
// FTS when the vector side is empty or errors.

import type { PluginContext } from '../../sdk'
import { asString } from '../../../lib/asString'
import {
  fuseHybrid,
  trimToBudget,
  type RankedHit,
  type RetrievedPassage
} from './hybrid'

/** Typed retrieval failure so callers can show an error, not false no-results. */
export class RetrieveError extends Error {
  constructor(
    message: string,
    readonly cause?: unknown
  ) {
    super(message)
    this.name = 'RetrieveError'
  }
}

/**
 * The subset of a plugin's settings that drives retrieval. QASettings satisfies
 * this structurally; the agent builds one inline from tool args.
 */
export interface RetrieveOptions {
  /** Vector weight α in weighted RRF (0 = pure FTS, 1 = pure vector). */
  hybrid_weight: number
  /** Max fused passages returned. */
  top_k: number
  /** Drop fused hits below this RRF score (0 disables). */
  min_score: number
  /** Approx char budget for retrieved context. */
  max_context_chars: number
  /** When true, re-score fused candidates by query–passage cosine similarity. */
  rerank_enabled: boolean
  /**
   * Optional filter applied to the FTS/vector-fused candidate set BEFORE
   * rerank + trim. Use this to scope results (notebook/section/type) so that
   * out-of-scope passage text is never sent to the embedding provider during
   * rerank, and out-of-scope hits cannot crowd in-scope hits out of top_k.
   */
  filterPassages?: (passages: RetrievedPassage[]) => Promise<RetrievedPassage[]>
  /**
   * Called when hybrid retrieve continues after one side (vector or FTS) fails
   * but the other succeeds (#630 degraded search signal).
   */
  onDegraded?: (info: HybridDegradeInfo) => void
}

/** One-sided hybrid failure while the other channel still returned hits. */
export interface HybridDegradeInfo {
  /** Which channel failed. */
  side: 'vector' | 'fts'
  /** Safe short reason for audit / UI (no note bodies). */
  message: string
}

/**
 * KNN vector search for a query. Each plugin supplies its own implementation
 * bound to its plugin.db; returning an empty list (no index) makes hybrid
 * retrieval fall open to the FTS side.
 */
export type VectorSearchFn = (
  ctx: PluginContext,
  query: string,
  topK: number,
  queryVec?: number[]
) => Promise<RankedHit[]>

/** Re-exports for convenience so consumers can import the full pipeline here. */
export { fuseHybrid, trimToBudget }
export type { RankedHit, RetrievedPassage }

/** Map fullTextSearch rows to RankedHit (block id + snippet). */
export function ftsRowsToHits(rows: Record<string, unknown>[]): RankedHit[] {
  return rows.map((r) => {
    // Strip FTS highlight markers before RAG so the model sees plain text.
    const raw = asString(r.snippet ?? r.clean_content ?? r.raw_content)
    const text = raw.replace(/<\/?mark>/gi, '')
    return {
      blockId: asString(r.id ?? r.block_id),
      notebook: asString(r.notebook),
      section: asString(r.section),
      page: asString(r.page),
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
    console.warn('silt-retrieval: rerank failed, falling back to RRF order:', e)
    return passages
  }
}

/**
 * End-to-end hybrid retrieval. Runs vector search (injected) and FTS5 in
 * parallel, fuses by weighted RRF, optionally reranks by cosine similarity,
 * and trims to the char budget. Throws RetrieveError when BOTH sides fail (or
 * one fails while the other is empty) so callers never mistake a hard failure
 * for "no results".
 */
export async function hybridRetrieve(
  ctx: PluginContext,
  question: string,
  settings: RetrieveOptions,
  vectorSearch: VectorSearchFn
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

  // One side failed but the other produced hits — continue with a degrade signal.
  if (vecErr && ftsRows.length > 0) {
    settings.onDegraded?.({
      side: 'vector',
      message: `Semantic search failed; keyword results only. ${errMsg(vecErr)}`
    })
  } else if (ftsErr && vecHits.length > 0) {
    settings.onDegraded?.({
      side: 'fts',
      message: `Keyword search failed; semantic results only. ${errMsg(ftsErr)}`
    })
  }

  const ftsHits = ftsRowsToHits(ftsRows).slice(0, fetchK)
  // When reranking, fuse a wider candidate set first; otherwise fuse to k.
  const fuseTopK = settings.rerank_enabled ? fetchK : k
  let fused = fuseHybrid(vecHits, ftsHits, {
    hybridWeight: settings.hybrid_weight,
    topK: fuseTopK,
    minScore: settings.min_score
  })

  // Scope candidates BEFORE rerank/trim so out-of-scope text is never embedded
  // and cannot displace in-scope hits from top_k.
  if (settings.filterPassages) {
    fused = await settings.filterPassages(fused)
  }

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
