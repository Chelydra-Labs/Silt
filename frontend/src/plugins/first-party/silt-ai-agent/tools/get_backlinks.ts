// Agent tool #599 — get_backlinks.
//
// Returns the blocks that reference a target — either inbound backlinks
// ((uuid)) or transclusion embeds. The target may be a block UUID or a page
// path (notebook/section/page, section/page, or just page); a page path is
// resolved to its block UUIDs first, then backlinks/embeds are gathered across
// all of them. An empty reference set is a clean empty list, NOT an error.

import type { PluginContext } from '../../../sdk'
import { asString } from '../../../../lib/asString'
import type { ToolResult } from '../tool-registry'
import { clampInt } from './_util'

export const getBacklinksToolDef = {
  name: 'get_backlinks',
  description:
    'List blocks that reference a target block or page. Returns backlinks ' +
    '(((uuid)) references) and, optionally, embed transclusions. Empty result ' +
    'means nothing references the target. target may be a UUID or a page path ' +
    '(e.g. "Work/Notes/Decisions").',
  parameters: {
    type: 'object',
    required: ['target'],
    properties: {
      target: {
        type: 'string',
        description: 'Block UUID or page path to find references to.'
      },
      include_embeds: {
        type: 'boolean',
        description: 'Also include transclusion embeds (default true).'
      },
      max_results: {
        type: 'integer',
        description: 'Maximum references to return (default 20, max 100).',
        minimum: 1,
        maximum: 100
      }
    }
  }
}

interface RefRow {
  source_id: string
  source_page: string
  snippet: string
  type: 'backlink' | 'embed'
}

const UUID_RE =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i
const DEFAULT_MAX_RESULTS = 20
const MAX_RESULTS = 100
const MAX_TARGET_IDS = 100

export async function handleGetBacklinks(
  ctx: PluginContext,
  args: Record<string, unknown>
): Promise<ToolResult> {
  const target = asString(args.target).trim()
  if (!target) {
    return { content: '', error: 'target must not be empty' }
  }
  const includeEmbeds =
    args.include_embeds === undefined ? true : Boolean(args.include_embeds)
  const maxResults = clampInt(
    args.max_results,
    DEFAULT_MAX_RESULTS,
    1,
    MAX_RESULTS
  )

  const ids = [...new Set(await resolveTargetIds(ctx, target))].slice(
    0,
    MAX_TARGET_IDS
  )
  if (ids.length === 0) {
    return { content: '', error: `could not resolve target "${target}"` }
  }

  // The core SDK exposes per-target helpers, but page targets can resolve to
  // many blocks. One parameterized raw-content query avoids an N+1 IPC storm;
  // exact token checks below prevent LIKE-prefix false positives.
  const refs = await fetchRefsBatch(ctx, ids, includeEmbeds, maxResults)

  if (refs.length === 0) {
    return { content: 'No backlinks or embeds found.' }
  }

  const lines = refs.map((r, i) => {
    const preview =
      r.snippet.length > 200 ? `${r.snippet.slice(0, 200)}…` : r.snippet
    return `- [${i + 1}] [${r.type}] block ${r.source_id} (${r.source_page}): ${preview}`
  })
  return {
    content: `${refs.length} reference(s):\n${lines.join('\n')}`,
    evidence: refs.map((r, i) => ({
      citationIndex: i + 1,
      blockId: r.source_id,
      snippet: r.snippet.slice(0, 200),
      title: r.source_page
    }))
  }
}

/** Resolve a UUID target directly, or a page path to its block UUIDs. */
async function resolveTargetIds(
  ctx: PluginContext,
  target: string
): Promise<string[]> {
  if (UUID_RE.test(target)) return [target.toLowerCase()]

  const parts = target
    .split('/')
    .map((s) => s.trim())
    .filter((s) => s.length > 0)
  if (parts.length === 0) return []

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
  const { rows } = await ctx.sqliteQuery(
    `SELECT DISTINCT id FROM blocks WHERE ${clauses.join(' AND ')}`,
    params
  )
  return rows.map((r) => asString(r.id)).filter((s) => s.length > 0)
}

async function fetchRefsBatch(
  ctx: PluginContext,
  ids: string[],
  includeEmbeds: boolean,
  maxResults: number
): Promise<RefRow[]> {
  const clauses: string[] = []
  const params: unknown[] = []
  for (const id of ids) {
    clauses.push('b.raw_content LIKE ?')
    params.push(`%((` + id + `)%`)
    if (includeEmbeds) {
      clauses.push('b.raw_content LIKE ?')
      params.push(`%{{embed:${id}}}%`)
    }
  }
  if (clauses.length === 0) return []
  const { rows } = await ctx.sqliteQuery(
    'SELECT b.id, b.page, b.clean_content, b.raw_content FROM blocks b ' +
      `WHERE ${clauses.join(' OR ')} ORDER BY b.notebook, b.section, b.page, b.line_number LIMIT ?`,
    [...params, maxResults]
  )
  const seen = new Set<string>()
  const refs: RefRow[] = []
  for (const r of rows) {
    const sourceId = asString(r.id)
    const raw = asString(r.raw_content).toLowerCase()
    const snippet = asString(r.clean_content).replace(/<\/?mark>/gi, '')
    for (const id of ids) {
      const normalizedId = id.toLowerCase()
      const backlink = raw.includes(`((${normalizedId}))`)
      if (backlink) addRef(refs, seen, sourceId, r, snippet, 'backlink')
      if (includeEmbeds && raw.includes(`{{embed:${normalizedId}}}`)) {
        addRef(refs, seen, sourceId, r, snippet, 'embed')
      }
      if (refs.length >= maxResults) return refs
    }
  }
  return refs
}

function addRef(
  refs: RefRow[],
  seen: Set<string>,
  sourceId: string,
  row: Record<string, unknown>,
  snippet: string,
  type: RefRow['type']
): void {
  const key = `${type}:${sourceId}`
  if (!sourceId || seen.has(key)) return
  seen.add(key)
  refs.push({
    source_id: sourceId,
    source_page: asString(row.page),
    snippet,
    type
  })
}
