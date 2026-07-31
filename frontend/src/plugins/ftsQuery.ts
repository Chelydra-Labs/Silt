/**
 * Plugin fullTextSearch SQL. Join MUST use blocks.rowid (integer), never
 * blocks.id (TEXT UUID) — external-content FTS5 is keyed on content_rowid='rowid'
 * (backend/db/search.go). Exported so tests lock the join contract.
 */
export const PLUGIN_FULL_TEXT_SEARCH_SQL = `SELECT b.id, b.notebook, b.section, b.page, b.clean_content, snippet(blocks_fts, -1, '<mark>', '</mark>', '…', 12) as snippet
         FROM blocks_fts f JOIN blocks b ON b.rowid = f.rowid
         WHERE blocks_fts MATCH ? ORDER BY rank LIMIT 50`

/**
 * Build an FTS5 MATCH query from free-text user input.
 * Mirrors backend/db/search.go buildFTSQuery: strip non-alnum per token,
 * drop tokens shorter than 2 chars, append prefix `*`, join with spaces
 * (implicit AND). Prevents hyphens from becoming FTS5 NOT operators.
 */
export function buildFTSQuery(query: string): string {
  const parts: string[] = []
  for (const w of query.trim().split(/\s+/)) {
    if (!w) continue
    let clean = ''
    for (const ch of w) {
      if (/[\p{L}\p{N}]/u.test(ch)) clean += ch
    }
    if (clean.length < 2) continue
    parts.push(`${clean}*`)
  }
  return parts.join(' ')
}
