// Keyword ranker over the shipped product-help corpus.

import {
  loadProductDocCorpus,
  tokenize,
  type ProductDocSection
} from './loadCorpus'

export const NO_MATCH_MESSAGE = 'No matching Silt help topics.'

export interface ProductDocHit {
  docId: string
  title: string
  sectionHeading: string
  excerpt: string
  score: number
  helpId: string
  /** Display title for evidence cards. */
  displayTitle: string
}

const DEFAULT_TOP_K = 5
const MAX_TOP_K = 10
const EXCERPT_CHARS = 400

function clampTopK(n: unknown): number {
  const v =
    typeof n === 'number' && Number.isFinite(n) ? Math.floor(n) : DEFAULT_TOP_K
  if (v < 1) return 1
  if (v > MAX_TOP_K) return MAX_TOP_K
  return v
}

function excerptOf(body: string, queryTokens: string[]): string {
  const flat = body.replace(/\s+/g, ' ').trim()
  if (flat.length <= EXCERPT_CHARS) return flat
  const lower = flat.toLowerCase()
  let best = 0
  for (const t of queryTokens) {
    const i = lower.indexOf(t)
    if (i >= 0 && (best === 0 || i < best)) best = i
  }
  const start = Math.max(0, best - 40)
  let slice = flat.slice(start, start + EXCERPT_CHARS)
  if (start > 0) slice = `…${slice}`
  if (start + EXCERPT_CHARS < flat.length) slice = `${slice}…`
  return slice
}

function scoreSection(sec: ProductDocSection, tokens: string[]): number {
  if (tokens.length === 0) return 0
  const titleToks = new Set(tokenize(sec.title))
  const headToks = new Set(tokenize(sec.sectionHeading))
  const bodyToks = tokenize(sec.body)
  const bodySet = new Set(bodyToks)
  let score = 0
  let matched = 0
  for (const t of tokens) {
    let hit = false
    if (titleToks.has(t)) {
      score += 8
      hit = true
    }
    if (headToks.has(t)) {
      score += 5
      hit = true
    }
    if (bodySet.has(t)) {
      // Mild tf boost
      const tf = bodyToks.filter((x) => x === t).length
      score += 1 + Math.min(3, tf - 1) * 0.25
      hit = true
    }
    if (hit) matched += 1
  }
  // Prefer sections that cover more of the query
  score *= 0.5 + matched / tokens.length
  return score
}

/**
 * Rank product-help sections for a query. Empty/whitespace query → [].
 * Does not format the no-match message (caller/tool does).
 */
export function searchProductDocs(
  query: string,
  topK?: unknown,
  corpus?: ProductDocSection[]
): ProductDocHit[] {
  const q = query.trim()
  if (!q) return []
  const tokens = tokenize(q)
  if (tokens.length === 0) return []
  const limit = clampTopK(topK)
  const sections = corpus ?? loadProductDocCorpus()
  const scored: ProductDocHit[] = []
  for (const sec of sections) {
    const score = scoreSection(sec, tokens)
    if (score <= 0) continue
    const sectionLabel = sec.sectionHeading
      ? `${sec.title} › ${sec.sectionHeading}`
      : sec.title
    scored.push({
      docId: sec.docId,
      title: sec.title,
      sectionHeading: sec.sectionHeading,
      excerpt: excerptOf(sec.body, tokens),
      score,
      helpId: sec.helpId,
      displayTitle: `Silt help: ${sectionLabel}`
    })
  }
  scored.sort((a, b) => b.score - a.score || a.helpId.localeCompare(b.helpId))
  return scored.slice(0, limit)
}
