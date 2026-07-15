// Shared hybrid retrieval primitives (#597).
//
// The fusion layer moved out of silt-ai-qa so the agent and any future consumer
// share ONE source of truth for weighted RRF + context-budget trimming. These
// functions are pure (no PluginContext, no plugin.db) — they operate on plain
// RankedHit / RetrievedPassage values. The vector index + embedding calls live
// in retrieve.ts and are injected by each plugin so a plugin's plugin.db stays
// its own storage.

/**
 * A fused retrieved passage. citeIndex is a 1-based citation marker ([1], [2])
 * assigned by the fusion/rerank stages. Defined here (next to fuseHybrid, the
 * producer) so the whole retrieval pipeline shares one type.
 */
export interface RetrievedPassage {
  blockId: string
  notebook: string
  section: string
  page: string
  lineNumber: number
  text: string
  score: number
  /** 1-based citation marker used in the prompt ([1], [2], …). */
  citeIndex: number
}

/** A single ranked hit from either the vector or FTS side, pre-fusion. */
export interface RankedHit {
  blockId: string
  notebook?: string
  section?: string
  page?: string
  lineNumber?: number
  text?: string
  /** Lower is better for vector distance; higher is better for FTS rank if provided. */
  score?: number
}

const RRF_K = 60

/**
 * Weighted Reciprocal Rank Fusion.
 * score(d) = α/(k+rank_vec) + (1−α)/(k+rank_fts)
 * α = hybridWeight (vector weight). Missing list ⇒ that side contributes 0.
 * Dedupes by blockId; keeps best fused score.
 */
export function fuseHybrid(
  vectorHits: RankedHit[],
  ftsHits: RankedHit[],
  opts: {
    hybridWeight: number
    topK: number
    minScore?: number
  }
): RetrievedPassage[] {
  const α = Math.min(1, Math.max(0, opts.hybridWeight))
  const scores = new Map<string, { score: number; hit: RankedHit }>()

  const addList = (list: RankedHit[], weight: number) => {
    list.forEach((hit, i) => {
      if (!hit.blockId) return
      const rrf = weight / (RRF_K + i + 1)
      const prev = scores.get(hit.blockId)
      if (prev) {
        prev.score += rrf
        // Prefer richer metadata from whichever hit has text.
        if (!prev.hit.text && hit.text) prev.hit = { ...prev.hit, ...hit }
      } else {
        scores.set(hit.blockId, { score: rrf, hit: { ...hit } })
      }
    })
  }

  // Skip a side entirely when its weight is 0 so pure-vector / pure-FTS modes
  // do not inject zero-score noise from the unused list.
  if (α > 0) addList(vectorHits, α)
  if (1 - α > 0) addList(ftsHits, 1 - α)

  const minScore = opts.minScore ?? 0
  const ranked = [...scores.entries()]
    .map(([blockId, v]) => ({ blockId, ...v }))
    .filter((v) => v.score >= minScore)
    .sort((a, b) => b.score - a.score)
    .slice(0, opts.topK)

  return ranked.map((r, i) => ({
    blockId: r.blockId,
    notebook: r.hit.notebook ?? '',
    section: r.hit.section ?? '',
    page: r.hit.page ?? '',
    lineNumber: r.hit.lineNumber ?? 0,
    text: (r.hit.text ?? '').trim(),
    score: r.score,
    citeIndex: i + 1
  }))
}

/** Drop lowest-score passages until total text length ≤ maxChars. */
export function trimToBudget(
  passages: RetrievedPassage[],
  maxChars: number
): RetrievedPassage[] {
  if (maxChars <= 0) return passages
  // Already sorted by score desc from fuse; drop from the end.
  const kept = [...passages]
  let total = kept.reduce((n, p) => n + p.text.length, 0)
  while (kept.length > 1 && total > maxChars) {
    const dropped = kept.pop()!
    total -= dropped.text.length
  }
  // Re-number cite indices after trim.
  return kept.map((p, i) => ({ ...p, citeIndex: i + 1 }))
}
