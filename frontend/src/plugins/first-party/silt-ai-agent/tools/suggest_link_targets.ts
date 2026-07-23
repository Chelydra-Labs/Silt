// Agent tool #607 — suggest_link_targets.
//
// Suggests blocks the source block might want to link to, ranked by cosine
// similarity (same embedding pipeline as get_related_notes #602), but
// EXCLUDING targets the source already references. References parsed from
// the source's clean_content: block refs `((uuid))` and wiki links
// `[[page]]` / `[[notebook/section/page]]` / `[[page#heading|alias]]`.
//
// Read-only: parses + queries + embeds + ranks, but writes nothing. The
// agent surfaces the suggestions to the user; an actual link insertion is a
// separate step (update_block).

import type { PluginContext } from '../../../sdk'
import { asString } from '../../../../lib/asString'
import type { ToolResult } from '../tool-registry'
import { breadcrumb, clampInt } from './_util'
import { embedOne, gatherCandidates, rankCandidates } from './_embedding'

export const suggestLinkTargetsToolDef = {
  name: 'suggest_link_targets',
  description:
    'Suggest blocks the source block might want to link to, ranked by ' +
    'semantic similarity and excluding already-linked targets (existing ' +
    '((uuid)) refs and [[page]] wiki links). Read-only — surfaces ' +
    'suggestions; the agent must call update_block to insert a link.',
  parameters: {
    type: 'object',
    required: ['block_id'],
    properties: {
      block_id: { type: 'string', description: 'Source block UUID.' },
      max_suggestions: {
        type: 'integer',
        description: 'Max results to return (default 5, max 20).',
        minimum: 1,
        maximum: 20
      }
    }
  }
}

/** GFM-style block ref `((uuid))`. UUID is 8-4-4-4-12 hex. */
const BLOCK_REF_RE =
  /\(\(([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})\)\)/gi

/** Wiki link `[[target]]`, `[[target#heading]]`, `[[target|alias]]`. */
const WIKI_LINK_RE = /\[\[([^\]]+)\]\]/g

export async function handleSuggestLinkTargets(
  ctx: PluginContext,
  args: Record<string, unknown>
): Promise<ToolResult> {
  const sourceId = asString(args.block_id).trim()
  if (!sourceId) {
    return { content: '', error: 'block_id must not be empty' }
  }
  const maxSuggestions = clampInt(args.max_suggestions, 5, 1, 20)

  // 1. Read the source block's clean_content.
  const { rows } = await ctx.sqliteQuery(
    'SELECT clean_content FROM blocks WHERE id = ?',
    [sourceId]
  )
  const row = rows[0]
  if (!row) {
    return { content: '', error: `block ${sourceId} not found` }
  }
  const sourceText = asString(row.clean_content).trim()
  if (!sourceText) {
    return {
      content: '',
      error: `block ${sourceId} has no content to compare`
    }
  }

  // 2. Resolve the set of target block_ids the source already links to, so
  //    they are excluded from suggestions. Block refs resolve directly;
  //    wiki links resolve through the page tuple (notebook/section/page).
  const excludeIds = new Set<string>([sourceId])
  for (const ref of parseBlockRefs(sourceText)) excludeIds.add(ref)
  for (const id of await resolveWikiLinkTargets(ctx, sourceText)) {
    excludeIds.add(id)
  }

  // 3. Embed the source as a retrieval query.
  const queryVec = await embedOne(ctx, sourceText, 'RETRIEVAL_QUERY')
  if (!queryVec || queryVec.length === 0) {
    return {
      content: '',
      error: 'embedding the source block failed (no vector returned)'
    }
  }

  // 4. Gather candidates with the full exclude set, then rank.
  const candidates = await gatherCandidates(ctx, excludeIds, sourceText)
  if (candidates.length === 0) {
    return { content: 'No candidate blocks found to suggest.' }
  }

  // min_score=0.15 — lower than get_related_notes's default 0.5 because the
  // point here is "interesting enough to maybe link", not "near-duplicate".
  // Anything below this is almost certainly noise.
  const top = await rankCandidates(ctx, queryVec, candidates, {
    minScore: 0.15,
    topK: maxSuggestions
  })

  if (top.length === 0) {
    return {
      content:
        `No unlinked blocks met the relevance threshold for block ${sourceId}. ` +
        '(Existing references were excluded.)'
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
    content:
      `${top.length} link suggestion(s) for block ${sourceId} ` +
      `(existing references excluded):\n\n${lines.join('\n\n')}`
  }
}

// --- helpers --------------------------------------------------------------

/** Extract all `((uuid))` block refs from the source text (lowercase). */
export function parseBlockRefs(text: string): string[] {
  const out: string[] = []
  for (const m of text.matchAll(BLOCK_REF_RE)) {
    const id = (m[1] ?? '').toLowerCase()
    if (id) out.push(id)
  }
  return out
}

/**
 * Resolve `[[page]]` / `[[notebook/section/page]]` / `[[page#heading|alias]]`
 * wiki links to the block_ids of their target page. Each wiki link yields a
 * set of block ids belonging to that page (a page is many blocks); each of
 * those ids is excluded from suggestions so the source does not get told to
 * re-link to a page it already names.
 *
 * Slashes split the path from the right (page is always last; section and
 * notebook are optional prefixes). Heading + alias are stripped before the
 * lookup — they affect scroll position, not target identity.
 */
export async function resolveWikiLinkTargets(
  ctx: PluginContext,
  text: string
): Promise<string[]> {
  const targets = parseWikiLinkTargets(text)
  if (targets.length === 0) return []

  const out: string[] = []
  for (const t of targets) {
    const parts = t
      .split('/')
      .map((s) => s.trim())
      .filter((s) => s.length > 0)
    if (parts.length === 0) continue

    const page = parts[parts.length - 1]
    const section = parts.length >= 2 ? parts[parts.length - 2] : null
    const notebook = parts.length >= 3 ? parts[parts.length - 3] : null

    const clauses = ['page = ?']
    const params: unknown[] = [page]
    if (section) {
      clauses.push('section = ?')
      params.push(section)
    }
    if (notebook) {
      clauses.push('notebook = ?')
      params.push(notebook)
    }
    const res = await ctx.sqliteQuery(
      `SELECT DISTINCT id FROM blocks WHERE ${clauses.join(' AND ')}`,
      params
    )
    for (const r of res.rows) {
      const id = asString(r.id)
      if (id) out.push(id)
    }
  }
  return out
}

/** Pull the raw target strings (heading + alias stripped) from `[[…]]`. */
function parseWikiLinkTargets(text: string): string[] {
  const out: string[] = []
  for (const m of text.matchAll(WIKI_LINK_RE)) {
    const raw = (m[1] ?? '').trim()
    if (!raw) continue
    // Strip `|alias` then `#heading`. Both are positional hints that do not
    // affect which page is the target.
    const noAlias = raw.split('|')[0] ?? ''
    const noHeading = (noAlias.split('#')[0] ?? '').trim()
    if (noHeading) out.push(noHeading)
  }
  return out
}
