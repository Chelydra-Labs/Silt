// Content-hash summary cache for silt-ai-summary (#222).
//
// Lives in the plugin-owned SQLite store (the §0 rule 4 carve-out — NOT the
// core index). Keyed by (page_id, content_hash) so a note is re-summarized
// only when its clean_content changes. The "new items" diff is computed
// against the prior extraction stored alongside each row, so the diff is
// stable across regenerations and model switches. Safe to clear: deleting the
// table just triggers re-summarization on the next open (documented in the
// README).

import type { PluginContext } from '../../sdk'
import type { SummaryExtraction } from './types'

const MIGRATION_VERSION = 1
const MIGRATION_SQL = `CREATE TABLE IF NOT EXISTS summaries (
  page_id        TEXT NOT NULL,
  content_hash   TEXT NOT NULL,
  summary        TEXT NOT NULL,
  tasks          TEXT NOT NULL,
  risks          TEXT NOT NULL,
  decisions      TEXT NOT NULL,
  prior_snapshot TEXT NOT NULL,
  model          TEXT NOT NULL,
  generated_at   TEXT NOT NULL,
  PRIMARY KEY (page_id, content_hash)
);`

/** One cached summary row. Array facets are stored as JSON strings. */
export interface CacheRow {
  page_id: string
  content_hash: string
  summary: string
  tasks: string[]
  risks: string[]
  decisions: string[]
  /** The extraction this row's newItems were diffed against (so a cache hit
   *  can re-derive the diff without a second row). */
  prior_snapshot: SummaryExtraction
  model: string
  generated_at: string
}

let migrated = false

/** Idempotently create the summaries table. The flag is per-session (a vault
 *  switch resets it via resetCacheState). ctx.pluginDb.migrate stamps
 *  PRAGMA user_version, so a future v2 migration is forward-only. */
export async function migrateCache(ctx: PluginContext): Promise<void> {
  if (migrated) return
  await ctx.pluginDb.migrate(MIGRATION_VERSION, MIGRATION_SQL)
  migrated = true
}

/** Test hook: forget the migrated flag so a fresh vault re-runs the migration. */
export function resetCacheState(): void {
  migrated = false
}

/** sha256 hex of the content. Async because it uses WebCrypto (SubtleCrypto),
 *  available in the main webview and in jsdom (Node ≥19). */
export async function computeContentHash(content: string): Promise<string> {
  const data = new TextEncoder().encode(content)
  const buf = await crypto.subtle.digest('SHA-256', data)
  return Array.from(new Uint8Array(buf))
    .map((b) => b.toString(16).padStart(2, '0'))
    .join('')
}

/** Read the cached row for an exact (page_id, content_hash) match, or null.
 *  Deserializes the JSON facet arrays + prior snapshot. */
export async function getCachedSummary(
  ctx: PluginContext,
  pageId: string,
  contentHash: string
): Promise<CacheRow | null> {
  const { rows } = await ctx.pluginDb.query(
    `SELECT summary, tasks, risks, decisions, prior_snapshot, model, generated_at
       FROM summaries
      WHERE page_id = ? AND content_hash = ?
      LIMIT 1`,
    [pageId, contentHash]
  )
  const r = rows[0]
  if (!r) return null
  return deserializeRow(r, pageId, contentHash)
}

/** The most-recently-generated row for a page (any content hash), used as the
 *  "prior snapshot" when a content change forces a regeneration. Ordered by
 *  generated_at DESC. null when the page has never been summarized. */
export async function latestSummaryForPage(
  ctx: PluginContext,
  pageId: string
): Promise<CacheRow | null> {
  const { rows } = await ctx.pluginDb.query(
    `SELECT content_hash, summary, tasks, risks, decisions, prior_snapshot, model, generated_at
       FROM summaries
      WHERE page_id = ?
      ORDER BY generated_at DESC
      LIMIT 1`,
    [pageId]
  )
  const r = rows[0]
  if (!r) return null
  return deserializeRow(r, pageId, String(r.content_hash))
}

/** Upsert a summary row keyed by (page_id, content_hash). The prior_snapshot
 *  is the extraction this row's newItems were diffed against (threaded from
 *  the previous latest row by the orchestrator). */
export async function putCachedSummary(ctx: PluginContext, row: CacheRow): Promise<void> {
  await ctx.pluginDb.exec(
    `INSERT INTO summaries
       (page_id, content_hash, summary, tasks, risks, decisions, prior_snapshot, model, generated_at)
     VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
     ON CONFLICT(page_id, content_hash) DO UPDATE SET
       summary = excluded.summary,
       tasks = excluded.tasks,
       risks = excluded.risks,
       decisions = excluded.decisions,
       prior_snapshot = excluded.prior_snapshot,
       model = excluded.model,
       generated_at = excluded.generated_at`,
    [
      row.page_id,
      row.content_hash,
      row.summary,
      JSON.stringify(row.tasks),
      JSON.stringify(row.risks),
      JSON.stringify(row.decisions),
      JSON.stringify(row.prior_snapshot),
      row.model,
      row.generated_at
    ]
  )
}

/** Deserialize a raw DB row into a CacheRow, tolerating malformed JSON in the
 *  facets/prior snapshot (a corrupt cell degrades to an empty array/snapshot
 *  rather than throwing — the cache is disposable per §0 rule 4). */
function deserializeRow(
  r: Record<string, unknown>,
  pageId: string,
  contentHash: string
): CacheRow {
  return {
    page_id: pageId,
    content_hash: contentHash,
    summary: stringOrEmpty(r.summary),
    // Top-level facets are stored as JSON strings → parseStringArray handles them.
    tasks: parseStringArray(r.tasks),
    risks: parseStringArray(r.risks),
    decisions: parseStringArray(r.decisions),
    // prior_snapshot is a JSON OBJECT whose facet fields are already arrays —
    // parse it once and read the arrays directly (parseStringArray expects a
    // JSON string, so feeding it an already-parsed array would yield []).
    prior_snapshot: parsePriorSnapshot(r.prior_snapshot),
    model: stringOrEmpty(r.model),
    generated_at: stringOrEmpty(r.generated_at)
  }
}

function parsePriorSnapshot(v: unknown): SummaryExtraction {
  const obj = (parseJSON(v) ?? {}) as Record<string, unknown>
  return {
    summary: stringOrEmpty(obj.summary),
    tasks: stringArrayValue(obj.tasks),
    risks: stringArrayValue(obj.risks),
    decisions: stringArrayValue(obj.decisions)
  }
}

/** Read a string[] from an already-parsed value (a facet inside prior_snapshot). */
function stringArrayValue(v: unknown): string[] {
  if (!Array.isArray(v)) return []
  return v.filter((x): x is string => typeof x === 'string')
}

function parseJSON(v: unknown): unknown {
  if (typeof v !== 'string') return null
  try {
    return JSON.parse(v)
  } catch {
    return null
  }
}

function parseStringArray(v: unknown): string[] {
  const parsed = parseJSON(v)
  if (!Array.isArray(parsed)) return []
  return parsed.filter((x): x is string => typeof x === 'string')
}

function stringOrEmpty(v: unknown): string {
  return typeof v === 'string' ? v : ''
}
