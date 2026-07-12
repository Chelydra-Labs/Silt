// End-to-end hybrid retrieval for silt-ai-qa (#225).

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

export async function hybridRetrieve(
  ctx: PluginContext,
  question: string,
  settings: QASettings
): Promise<RetrievedPassage[]> {
  const k = Math.max(settings.top_k, 1)
  const fetchK = Math.max(k * 2, 10)

  let vecHits: RankedHit[] = []
  let ftsRows: Record<string, unknown>[] = []
  let vecErr: unknown = null
  let ftsErr: unknown = null

  const [vecSettled, ftsSettled] = await Promise.allSettled([
    vectorSearch(ctx, question, fetchK),
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
  let fused = fuseHybrid(vecHits, ftsHits, {
    hybridWeight: settings.hybrid_weight,
    topK: k,
    minScore: settings.min_score
  })
  fused = trimToBudget(fused, settings.max_context_chars)
  return fused.filter((p) => p.text.length > 0)
}

function errMsg(e: unknown): string {
  if (e instanceof Error) return e.message
  return String(e)
}
