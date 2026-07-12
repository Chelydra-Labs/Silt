// End-to-end hybrid retrieval for silt-ai-qa (#225).

import type { PluginContext } from '../../sdk'
import { fuseHybrid, trimToBudget, type RankedHit } from './hybrid'
import { vectorSearch } from './embed_index'
import type { QASettings, RetrievedPassage } from './types'

/** Map fullTextSearch rows to RankedHit (block id + snippet). */
export function ftsRowsToHits(rows: Record<string, unknown>[]): RankedHit[] {
  return rows.map((r) => ({
    blockId: String(r.id ?? r.block_id ?? ''),
    notebook: String(r.notebook ?? ''),
    section: String(r.section ?? ''),
    page: String(r.page ?? ''),
    lineNumber: Number(r.line_number ?? r.lineNumber ?? 0),
    text: String(r.snippet ?? r.clean_content ?? r.raw_content ?? ''),
    score: Number(r.rank ?? r.score ?? 0)
  }))
}

export async function hybridRetrieve(
  ctx: PluginContext,
  question: string,
  settings: QASettings
): Promise<RetrievedPassage[]> {
  const k = Math.max(settings.top_k, 1)
  const fetchK = Math.max(k * 2, 10)

  const [vecHits, ftsResult] = await Promise.all([
    vectorSearch(ctx, question, fetchK).catch(() => [] as RankedHit[]),
    ctx
      .fullTextSearch(question)
      .catch(() => ({ rows: [] as Record<string, unknown>[] }))
  ])

  const ftsHits = ftsRowsToHits(ftsResult.rows ?? []).slice(0, fetchK)
  let fused = fuseHybrid(vecHits, ftsHits, {
    hybridWeight: settings.hybrid_weight,
    topK: k,
    minScore: settings.min_score
  })
  fused = trimToBudget(fused, settings.max_context_chars)
  // Drop empty text passages.
  return fused.filter((p) => p.text.length > 0)
}
