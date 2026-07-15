// Agent tool #602 — get_related_notes.
//
// Semantic "more like this": given a source block, embed its content and rank
// other blocks by cosine similarity. The agent does not maintain a vec0 index
// today (Phase 4 noted this), so the primary path is on-demand: gather a
// candidate pool (recent + FTS-recalled), embed the misses via ctx.ai.embed,
// and rank by cosine similarity computed in JS. Each computed vector is
// cached in plugin.db keyed by (block_id, content_hash) so subsequent runs
// reuse the work — a real vec0 + vec_distance_cosine index can replace the
// cache table later without changing this tool's contract.
//
// Candidate pool: recent blocks (last CANDIDATE_LIMIT by line_number) plus
// FTS-recalled blocks using the source's keywords. Unioned + deduped. The
// source block_id is always excluded from results.

import type { PluginContext } from '../../../sdk'
import type { ToolResult } from '../tool-registry'
import { breadcrumb, clampInt } from './_util'

export const getRelatedNotesToolDef = {
  name: 'get_related_notes',
  description:
    'Find blocks semantically related to a source block. Embeds the source ' +
    'and ranks candidates by cosine similarity. Returns block_id, score, ' +
    'snippet, and location for each. The source block is excluded from ' +
    'results.',
  parameters: {
    type: 'object',
    required: ['block_id'],
    properties: {
      block_id: { type: 'string', description: 'Source block UUID.' },
      top_k: {
        type: 'integer',
        description: 'Max results to return (default 10).',
        minimum: 1,
        maximum: 50
      },
      min_score: {
        type: 'number',
        description:
          'Minimum cosine similarity in [0, 1] to include a result (default 0.5).',
        minimum: 0,
        maximum: 1
      }
    }
  }
}

/** Candidate pool cap. Bounds per-call embedding cost when the cache is cold. */
const CANDIDATE_LIMIT = 200
/** Cache table for on-demand embeddings (lazy-created on first miss). */
const CACHE_DDL =
  'CREATE TABLE IF NOT EXISTS block_vectors (' +
  '  block_id TEXT PRIMARY KEY,' +
  '  content_hash TEXT NOT NULL,' +
  '  model TEXT NOT NULL,' +
  '  dimensions INTEGER NOT NULL,' +
  '  vector TEXT NOT NULL,' +
  '  updated_at INTEGER NOT NULL' +
  ')'

interface CandidateBlock {
  id: string
  clean_content: string
  notebook: string
  section: string
  page: string
}

interface CachedVector {
  block_id: string
  content_hash: string
  vector: number[]
}

export async function handleGetRelatedNotes(
  ctx: PluginContext,
  args: Record<string, unknown>
): Promise<ToolResult> {
  const sourceId = String(args.block_id ?? '').trim()
  if (!sourceId) {
    return { content: '', error: 'block_id must not be empty' }
  }
  const topK = clampInt(args.top_k, 10, 1, 50)
  const minScore =
    typeof args.min_score === 'number' && Number.isFinite(args.min_score)
      ? Math.min(1, Math.max(0, args.min_score))
      : 0.5

  // 1. Read the source block's clean_content.
  const sourceRes = await fetchSourceContent(ctx, sourceId)
  if ('error' in sourceRes) {
    return { content: '', error: sourceRes.error }
  }
  const sourceText = sourceRes.text

  // 2. Embed the source as a retrieval query.
  const queryVec = await embedOne(ctx, sourceText, 'RETRIEVAL_QUERY')
  if (!queryVec) {
    return {
      content: '',
      error: 'embedding the source block failed (no vector returned)'
    }
  }

  // 3. Gather candidates (recent + FTS-recalled, source excluded).
  const candidates = await gatherCandidates(ctx, sourceId, sourceText)
  if (candidates.length === 0) {
    return { content: 'No other blocks found to compare against.' }
  }

  // 4. Resolve a vector per candidate: cache hit on (block_id, content_hash)
  //    or batch-embed the misses and cache them for future runs.
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

  // 5. Score every candidate, filter by min_score, sort, take top_k.
  const scored: Array<{ block: CandidateBlock; score: number }> = []
  const missById = new Map(misses.map((m, i) => [m.id, missVectors[i] ?? []]))
  for (const c of candidates) {
    const vec = missById.get(c.id) ?? cached.get(c.id)?.vector
    if (!vec || vec.length === 0) continue
    const score = cosine(queryVec, vec)
    if (score >= minScore) scored.push({ block: c, score })
  }
  scored.sort((a, b) => b.score - a.score)
  const top = scored.slice(0, topK)

  if (top.length === 0) {
    return {
      content: `No related blocks met the min_score=${minScore.toFixed(2)} threshold.`
    }
  }

  const lines = top.map((s, i) => {
    const snippet =
      s.block.clean_content.length > 200
        ? `${s.block.clean_content.slice(0, 200)}…`
        : s.block.clean_content
    return [
      `[${i + 1}] block ${s.block.id}`,
      `    score: ${s.score.toFixed(4)}`,
      `    location: ${breadcrumb(
        s.block.notebook,
        s.block.section,
        s.block.page
      )}`,
      `    ${snippet.replace(/\n/g, '\n    ')}`
    ].join('\n')
  })
  return {
    content: `${top.length} related block(s):\n\n${lines.join('\n\n')}`
  }
}

// --- helpers --------------------------------------------------------------

/** Fetch the source block's clean_content (single-row SELECT). */
async function fetchSourceContent(
  ctx: PluginContext,
  id: string
): Promise<{ error: string } | { text: string }> {
  const { rows } = await ctx.sqliteQuery(
    'SELECT clean_content FROM blocks WHERE id = ?',
    [id]
  )
  const row = rows[0]
  if (!row) {
    return { error: `block ${id} not found` }
  }
  const text = String(row.clean_content ?? '').trim()
  if (!text) {
    return { error: `block ${id} has no content to compare` }
  }
  return { text }
}

/** Embed a single text using the given task type; returns [] on failure. */
async function embedOne(
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
async function embedBatch(
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
 * Gather the candidate pool: recent blocks + FTS-recalled, source excluded
 * and deduped. Recency uses line_number DESC (a stable proxy for "fresh" —
 * the editor appends new blocks at the bottom of their file). FTS recall
 * uses a few simple keywords pulled from the source content.
 */
async function gatherCandidates(
  ctx: PluginContext,
  sourceId: string,
  sourceText: string
): Promise<CandidateBlock[]> {
  const byId = new Map<string, CandidateBlock>()

  const recent = await fetchCandidateRows(
    ctx,
    'SELECT id, clean_content, notebook, section, page FROM blocks ' +
      'WHERE id != ? ORDER BY line_number DESC LIMIT ?',
    [sourceId, CANDIDATE_LIMIT]
  )
  for (const c of recent) byId.set(c.id, c)

  const keywords = extractKeywords(sourceText)
  if (keywords.length > 0) {
    const ftsRows = await safeFts(ctx, keywords.join(' OR '))
    for (const r of ftsRows) {
      const id = String(r.id ?? '')
      if (!id || id === sourceId || byId.has(id)) continue
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

/** Pull a few alphanumeric keywords (length ≥ 3) from the source text. */
function extractKeywords(text: string): string[] {
  const seen = new Set<string>()
  const out: string[] = []
  for (const m of text.matchAll(/[A-Za-z][A-Za-z0-9_-]{2,}/g)) {
    const w = m[0]
    const lower = w.toLowerCase()
    // Skip stopwords that add no signal.
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

/** Parse a cached JSON-encoded vector; returns [] on any malformed row. */
function parseVector(raw: unknown): number[] {
  if (typeof raw !== 'string' || raw.length === 0) return []
  try {
    const parsed = JSON.parse(raw)
    if (!Array.isArray(parsed)) return []
    return parsed.map((n) => Number(n)).filter((n) => Number.isFinite(n))
  } catch {
    return []
  }
}

/**
 * Cheap stable hash for cache keys: FNV-1a over the UTF-8 bytes of the clean
 * content. Determinism (same content → same hash) is the only requirement —
 * collision resistance is irrelevant since the cache row is per-block_id and
 * a stale hash only causes a re-embed, never a wrong result.
 */
function hashOf(text: string): string {
  let h = 0x811c9dc5
  for (let i = 0; i < text.length; i++) {
    h ^= text.charCodeAt(i)
    // FNV prime (multiply by 16777619 mod 2^32).
    h = (h + ((h << 1) + (h << 4) + (h << 7) + (h << 8) + (h << 24))) >>> 0
  }
  return h.toString(16)
}

/** Cosine similarity; returns 0 for any zero-magnitude edge case. */
function cosine(a: number[], b: number[]): number {
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
