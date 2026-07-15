// Shared embedding + cosine-similarity ranking helpers.
//
// Used by get_related_notes (#602) and suggest_link_targets (#607) — both
// tools embed a source, gather a candidate pool (recent + FTS-recalled),
// resolve per-candidate vectors (cache or on-demand embed), and rank by
// cosine similarity. The cache lives in the plugin's per-vault SQLite store
// (block_vectors), keyed by (block_id, content_hash) so a content change
// invalidates only that row.
//
// A real vec0 + vec_distance_cosine index can replace the cache table later
// without changing the contract of rankCandidates().

import type { PluginContext } from '../../../sdk'

/** Candidate pool cap. Bounds per-call embedding cost when the cache is cold. */
export const CANDIDATE_LIMIT = 200

/** Cache table for on-demand embeddings (lazy-created on first miss). */
export const CACHE_DDL =
  'CREATE TABLE IF NOT EXISTS block_vectors (' +
  '  block_id TEXT PRIMARY KEY,' +
  '  content_hash TEXT NOT NULL,' +
  '  model TEXT NOT NULL,' +
  '  dimensions INTEGER NOT NULL,' +
  '  vector TEXT NOT NULL,' +
  '  updated_at INTEGER NOT NULL' +
  ')'

export interface CandidateBlock {
  id: string
  clean_content: string
  notebook: string
  section: string
  page: string
}

export interface CachedVector {
  block_id: string
  content_hash: string
  vector: number[]
}

export interface ScoredCandidate {
  block: CandidateBlock
  score: number
}

/** Cosine similarity; returns 0 for any zero-magnitude or length-mismatch case. */
export function cosine(a: number[], b: number[]): number {
  if (a.length === 0 || a.length !== b.length) return 0
  let dot = 0
  let magA = 0
  let magB = 0
  for (let i = 0; i < a.length; i++) {
    dot += a[i] * b[i]
    magA += a[i] * a[i]
    magB += b[i] * b[i]
  }
  if (magA === 0 || magB === 0) return 0
  return dot / (Math.sqrt(magA) * Math.sqrt(magB))
}

/**
 * Cheap stable hash for cache keys: FNV-1a over the UTF-8 bytes of the clean
 * content. Determinism (same content → same hash) is the only requirement —
 * collision resistance is irrelevant since the cache row is per-block_id and
 * a stale hash only causes a re-embed, never a wrong result.
 */
export function hashOf(text: string): string {
  let h = 0x811c9dc5
  for (let i = 0; i < text.length; i++) {
    h ^= text.charCodeAt(i)
    // FNV prime (multiply by 16777619 mod 2^32).
    h = (h + ((h << 1) + (h << 4) + (h << 7) + (h << 8) + (h << 24))) >>> 0
  }
  return h.toString(16)
}

/** Parse a cached JSON-encoded vector; returns [] on any malformed row. */
export function parseVector(raw: unknown): number[] {
  if (typeof raw !== 'string' || raw.length === 0) return []
  try {
    const parsed = JSON.parse(raw)
    if (!Array.isArray(parsed)) return []
    return parsed.map((n) => Number(n)).filter((n) => Number.isFinite(n))
  } catch {
    return []
  }
}

/** Embed a single text using the given task type; returns [] on failure. */
export async function embedOne(
  ctx: PluginContext,
  text: string,
  taskType: string
): Promise<number[]> {
  try {
    const res = await ctx.ai.embed({ texts: [text], taskType })
    return res.embeddings[0] ?? []
  } catch {
    return []
  }
}

/** Embed a batch using the given task type; returns null on failure. */
export async function embedBatch(
  ctx: PluginContext,
  texts: string[],
  taskType: string
): Promise<number[][] | null> {
  try {
    const res = await ctx.ai.embed({ texts, taskType })
    return res.embeddings
  } catch {
    return null
  }
}

/**
 * Gather the candidate pool: recent blocks + FTS-recalled, with the
 * configured excludeIds removed and the result deduped. Recency uses
 * line_number DESC (a stable proxy for "fresh" — the editor appends new
 * blocks at the bottom of their file). FTS recall uses a few simple keywords
 * pulled from the source content. sourceText may be empty (no FTS recall);
 * excludeIds must always contain at least the source id.
 */
export async function gatherCandidates(
  ctx: PluginContext,
  excludeIds: Set<string>,
  sourceText: string
): Promise<CandidateBlock[]> {
  const byId = new Map<string, CandidateBlock>()

  // Build the "id != ? AND id != ? …" placeholder list for the excluded set.
  // The pool always excludes the source; suggest_link_targets additionally
  // excludes already-linked targets.
  const excludeList = [...excludeIds].filter((s) => s.length > 0)
  const recentSql =
    excludeList.length > 0
      ? `SELECT id, clean_content, notebook, section, page FROM blocks WHERE id NOT IN (${excludeList
          .map(() => '?')
          .join(',')}) ORDER BY line_number DESC LIMIT ?`
      : 'SELECT id, clean_content, notebook, section, page FROM blocks ORDER BY line_number DESC LIMIT ?'
  const recent = await fetchCandidateRows(ctx, recentSql, [
    ...excludeList,
    CANDIDATE_LIMIT
  ])
  for (const c of recent) byId.set(c.id, c)

  const keywords = extractKeywords(sourceText)
  if (keywords.length > 0) {
    const ftsRows = await safeFts(ctx, keywords.join(' OR '))
    for (const r of ftsRows) {
      const id = String(r.id ?? '')
      if (!id || excludeIds.has(id) || byId.has(id)) continue
      byId.set(id, {
        id,
        clean_content: String(r.clean_content ?? ''),
        notebook: String(r.notebook ?? ''),
        section: String(r.section ?? ''),
        page: String(r.page ?? '')
      })
    }
  }

  return [...byId.values()]
}

/**
 * Resolve a vector per candidate (cache hit on (block_id, content_hash) or
 * batch-embed the misses and cache them), score each by cosine similarity to
 * queryVec, filter by minScore, sort descending, and take top_k.
 *
 * Failures (embed service down, cache table missing) degrade gracefully: a
 * candidate with no resolvable vector is dropped rather than aborting the
 * ranking.
 */
export async function rankCandidates(
  ctx: PluginContext,
  queryVec: number[],
  candidates: CandidateBlock[],
  opts: { minScore?: number; topK?: number } = {}
): Promise<ScoredCandidate[]> {
  const minScore = opts.minScore ?? 0.5
  const topK = opts.topK ?? 10
  if (queryVec.length === 0 || candidates.length === 0) return []

  const cached = await readCachedVectors(ctx, candidates)
  const misses: CandidateBlock[] = []
  for (const c of candidates) {
    const hit = cached.get(c.id)
    if (!hit || hit.content_hash !== hashOf(c.clean_content)) {
      misses.push(c)
    }
  }
  let missVectors: number[][] = []
  if (misses.length > 0) {
    const texts = misses.map((m) => m.clean_content)
    const embedded = await embedBatch(ctx, texts, 'RETRIEVAL_DOCUMENT')
    if (embedded) {
      missVectors = embedded
      await writeCachedVectors(ctx, misses, embedded)
    }
  }

  const scored: ScoredCandidate[] = []
  const missById = new Map(misses.map((m, i) => [m.id, missVectors[i] ?? []]))
  for (const c of candidates) {
    const vec = missById.get(c.id) ?? cached.get(c.id)?.vector
    if (!vec || vec.length === 0) continue
    const score = cosine(queryVec, vec)
    if (score >= minScore) scored.push({ block: c, score })
  }
  scored.sort((a, b) => b.score - a.score)
  return scored.slice(0, topK)
}

/** Run a candidate SELECT and normalize rows to CandidateBlock. */
async function fetchCandidateRows(
  ctx: PluginContext,
  sql: string,
  params: unknown[]
): Promise<CandidateBlock[]> {
  const { rows } = await ctx.sqliteQuery(sql, params)
  return rows
    .filter((r) => {
      const text = String(r.clean_content ?? '').trim()
      return text.length > 0
    })
    .map((r) => ({
      id: String(r.id ?? ''),
      clean_content: String(r.clean_content ?? '').trim(),
      notebook: String(r.notebook ?? ''),
      section: String(r.section ?? ''),
      page: String(r.page ?? '')
    }))
}

/**
 * FTS recall with a soft failure mode: any MATCH syntax issue or empty index
 * yields an empty list rather than aborting the whole tool. The recent-blocks
 * pool is still useful on its own.
 */
async function safeFts(
  ctx: PluginContext,
  query: string
): Promise<Record<string, unknown>[]> {
  try {
    const res = await ctx.fullTextSearch(query)
    return res.rows
  } catch {
    return []
  }
}

/** Read cached vectors for a candidate set. Returns map keyed by block_id. */
async function readCachedVectors(
  ctx: PluginContext,
  candidates: CandidateBlock[]
): Promise<Map<string, CachedVector>> {
  if (candidates.length === 0) return new Map()
  const ids = candidates.map((c) => c.id)
  const placeholders = ids.map(() => '?').join(',')
  let rows: Record<string, unknown>[] = []
  try {
    const res = await ctx.pluginDb.query(
      `SELECT block_id, content_hash, vector FROM block_vectors ` +
        `WHERE block_id IN (${placeholders})`,
      ids
    )
    rows = res.rows
  } catch {
    // First-run: table doesn't exist yet. Treat as empty cache; the write
    // path below will CREATE TABLE IF NOT EXISTS before caching.
    return new Map()
  }
  const map = new Map<string, CachedVector>()
  for (const r of rows) {
    const id = String(r.block_id ?? '')
    if (!id) continue
    const vec = parseVector(r.vector)
    if (vec.length === 0) continue
    map.set(id, {
      block_id: id,
      content_hash: String(r.content_hash ?? ''),
      vector: vec
    })
  }
  return map
}

/** Persist on-demand embeddings for future runs. */
async function writeCachedVectors(
  ctx: PluginContext,
  blocks: CandidateBlock[],
  vectors: number[][]
): Promise<void> {
  try {
    await ctx.pluginDb.exec(CACHE_DDL)
  } catch {
    // If we cannot create the cache table (e.g. exec blocked), give up on
    // caching for this call — ranking still works with the freshly embedded
    // vectors in memory.
    return
  }
  const now = Date.now()
  // Pull the configured model name once for all rows; the embeddings result
  // does not carry it, so leave a stable placeholder the lookup ignores.
  const model = 'default'
  for (let i = 0; i < blocks.length; i++) {
    const b = blocks[i]
    const vec = vectors[i]
    if (!vec || vec.length === 0) continue
    try {
      await ctx.pluginDb.exec(
        `INSERT INTO block_vectors (block_id, content_hash, model, dimensions, vector, updated_at) ` +
          `VALUES (?, ?, ?, ?, ?, ?) ` +
          `ON CONFLICT(block_id) DO UPDATE SET ` +
          `content_hash=excluded.content_hash, model=excluded.model, ` +
          `dimensions=excluded.dimensions, vector=excluded.vector, ` +
          `updated_at=excluded.updated_at`,
        [
          b.id,
          hashOf(b.clean_content),
          model,
          vec.length,
          JSON.stringify(vec),
          now
        ]
      )
    } catch {
      // Per-row failures (e.g. shape mismatch on a schema bump) must not
      // fail the ranking — the cached value is a perf optimization only.
    }
  }
}

/** Pull a few alphanumeric keywords (length ≥ 3) from the source text. */
export function extractKeywords(text: string): string[] {
  const seen = new Set<string>()
  const out: string[] = []
  for (const m of text.matchAll(/[A-Za-z][A-Za-z0-9_-]{2,}/g)) {
    const w = m[0]
    const lower = w.toLowerCase()
    if (STOPWORDS.has(lower)) continue
    if (seen.has(lower)) continue
    seen.add(lower)
    out.push(w)
    if (out.length >= 6) break
  }
  return out
}

const STOPWORDS = new Set([
  'the',
  'and',
  'for',
  'are',
  'but',
  'not',
  'you',
  'all',
  'any',
  'can',
  'her',
  'was',
  'one',
  'our',
  'out',
  'has',
  'have',
  'from',
  'this',
  'that',
  'with',
  'your',
  'they',
  'will',
  'what',
  'when',
  'who',
  'whom',
  'them',
  'then'
])
