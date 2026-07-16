// End-to-end hybrid retrieval for silt-ai-qa (#225, #620).
//
// The retrieval pipeline moved to the shared retrieval module (#597) so the AI
// agent shares one source of truth. QA re-exports the pure pieces and wraps
// hybridRetrieve to inject its own vectorSearch (bound to QA's plugin.db index)
// and pass its QASettings as RetrieveOptions.

import type { PluginContext } from '../../sdk'
import {
  ftsRowsToHits as sharedFtsRowsToHits,
  hybridRetrieve as sharedHybridRetrieve,
  rerankPassages as sharedRerankPassages,
  RetrieveError,
  type HybridDegradeInfo,
  type RetrieveOptions
} from '../../shared/retrieval/retrieve'
import type { RetrievedPassage } from '../../shared/retrieval/hybrid'
import { vectorSearch } from './embed_index'
import type { QASettings } from './types'

export { RetrieveError }
export type { RetrievedPassage, HybridDegradeInfo }

/** Map fullTextSearch rows to RankedHit (block id + snippet). */
export const ftsRowsToHits = sharedFtsRowsToHits

/** Re-score fused candidates by query–passage cosine similarity (QA entry). */
export const rerankPassages = sharedRerankPassages

/**
 * QA retrieval: shared pipeline + QA's own vector index. QASettings satisfies
 * RetrieveOptions structurally (hybrid_weight / top_k / min_score /
 * max_context_chars / rerank_enabled), so no mapping is needed.
 */
export async function hybridRetrieve(
  ctx: PluginContext,
  question: string,
  settings: QASettings,
  onDegraded?: (info: HybridDegradeInfo) => void
): Promise<RetrievedPassage[]> {
  const opts: RetrieveOptions = {
    hybrid_weight: settings.hybrid_weight,
    top_k: settings.top_k,
    min_score: settings.min_score,
    max_context_chars: settings.max_context_chars,
    rerank_enabled: settings.rerank_enabled,
    onDegraded
  }
  return sharedHybridRetrieve(ctx, question, opts, vectorSearch)
}
