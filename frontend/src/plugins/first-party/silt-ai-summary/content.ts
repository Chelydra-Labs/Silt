// Note-content fetcher for silt-ai-summary.
//
// The orchestrator needs the active page's clean text to hash + summarize.
// This queries the core index (read-only, SELECT-only) for the page's blocks
// in line order and concatenates their clean_content. Source is intentionally
// not filtered: a page name lives in one notebook+section, so collation by
// (notebook, section, page) is unambiguous in practice; the hash is over the
// resulting text, so a cross-source collision would just hash identically.

import type { PluginContext } from '../../sdk'

/** Fetch the concatenated clean_content of every block in a page, in line
 *  order. Returns '' when the page has no blocks (empty/new note). */
export async function fetchNoteContent(
  ctx: PluginContext,
  notebook: string,
  section: string,
  page: string
): Promise<string> {
  const { rows } = await ctx.sqliteQuery(
    `SELECT clean_content
       FROM blocks
      WHERE notebook = ? AND section = ? AND page = ?
      ORDER BY line_number`,
    [notebook, section, page]
  )
  return rows
    .map((r) => (typeof r.clean_content === 'string' ? r.clean_content : ''))
    .join('\n')
    .trim()
}
