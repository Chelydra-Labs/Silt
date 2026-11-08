import { buildFtsMatch, normalizeFtsTerm, quoteFtsTerm } from '../lib/ftsQuery'

/**
 * Plugin fullTextSearch SQL. Join MUST use blocks.rowid (integer), never
 * blocks.id (TEXT UUID) — external-content FTS5 is keyed on content_rowid='rowid'
 * (backend/db/search.go). Exported so tests lock the join contract.
 */
export const PLUGIN_FULL_TEXT_SEARCH_SQL = `SELECT b.id, b.notebook, b.section, b.page, b.clean_content, snippet(blocks_fts, -1, '<mark>', '</mark>', '…', 12) as snippet
         FROM blocks_fts f JOIN blocks b ON b.rowid = f.rowid
         WHERE blocks_fts MATCH ? ORDER BY rank LIMIT 50`

/**
 * Sanitize one free-text token for FTS5 MATCH: keep Unicode letters/digits,
 * drop punctuation (so hyphens never become FTS5 NOT), require length ≥ 2,
 * quote it and append the prefix `*`. Returns '' when the token is unusable.
 *
 * Delegates to lib/ftsQuery (the builder global search mirrors) so
 * `ctx.fullTextSearch` cannot diverge from the hub/agent semantics.
 */
export function sanitizeFTSToken(token: string): string {
  const term = normalizeFtsTerm(token)
  return term ? quoteFtsTerm(term) : ''
}

/**
 * Build an FTS5 MATCH query from free-text user input. Mirrors
 * backend/db/search.go buildFTSQuery: strip non-alnum per token, drop tokens
 * shorter than 2 UTF-8 bytes, quote + prefix `*`, join with spaces (implicit
 * AND). Quoting prevents hyphens and bare `OR/AND/NOT` from becoming FTS5
 * operators.
 */
export function buildFTSQuery(query: string): string {
  return buildFtsMatch(query)
}

/**
 * Build an FTS5 MATCH that ORs sanitized prefix terms (keyword candidate
 * recall). Do not pass this through buildFTSQuery / fullTextSearch — those
 * treat "OR" as a literal term and collapse the union into an AND.
 */
export function buildFTSOrQuery(terms: string[]): string {
  const parts: string[] = []
  const seen = new Set<string>()
  for (const t of terms) {
    const term = sanitizeFTSToken(t)
    if (!term) continue
    // FTS5 matching is case-insensitive; keep first surface form only.
    const key = term.toLowerCase()
    if (seen.has(key)) continue
    seen.add(key)
    parts.push(term)
  }
  return parts.join(' OR ')
}
