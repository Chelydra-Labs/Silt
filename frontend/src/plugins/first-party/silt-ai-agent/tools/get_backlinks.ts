// Agent tool #599 — get_backlinks.
//
// Returns the blocks that reference a target — either inbound backlinks
// ((uuid)) or transclusion embeds. The target may be a block UUID or a page
// path (notebook/section/page, section/page, or just page); a page path is
// resolved to its block UUIDs first, then backlinks/embeds are gathered across
// all of them. An empty reference set is a clean empty list, NOT an error.

import type { PluginContext } from '../../../sdk'
import type { ToolResult } from '../tool-registry'

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

export async function handleGetBacklinks(
  ctx: PluginContext,
  args: Record<string, unknown>
): Promise<ToolResult> {
  const target = String(args.target ?? '').trim()
  if (!target) {
    return { content: '', error: 'target must not be empty' }
  }
  const includeEmbeds =
    args.include_embeds === undefined ? true : Boolean(args.include_embeds)

  const ids = await resolveTargetIds(ctx, target)
  if (ids.length === 0) {
    return { content: '', error: `could not resolve target "${target}"` }
  }

  // Gather refs across all resolved block ids, deduped by source + type.
  const seen = new Set<string>()
  const refs: RefRow[] = []
  for (const id of ids) {
    for (const r of await toRefs(ctx.getBacklinks(id), 'backlink')) {
      const key = `${r.type}:${r.source_id}`
      if (r.source_id && !seen.has(key)) {
        seen.add(key)
        refs.push(r)
      }
    }
    if (includeEmbeds) {
      for (const r of await toRefs(ctx.getEmbeds(id), 'embed')) {
        const key = `${r.type}:${r.source_id}`
        if (r.source_id && !seen.has(key)) {
          seen.add(key)
          refs.push(r)
        }
      }
    }
  }

  if (refs.length === 0) {
    return { content: 'No backlinks or embeds found.' }
  }

  const lines = refs.map((r) => {
    const preview =
      r.snippet.length > 200 ? `${r.snippet.slice(0, 200)}…` : r.snippet
    return `- [${r.type}] block ${r.source_id} (${r.source_page}): ${preview}`
  })
  return {
    content: `${refs.length} reference(s):\n${lines.join('\n')}`
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
  return rows.map((r) => String(r.id)).filter((s) => s.length > 0)
}

/** Map a backlink/embed query result to source-ref rows. */
async function toRefs(
  res: PromiseLike<{ rows: Record<string, unknown>[] }>,
  type: 'backlink' | 'embed'
): Promise<RefRow[]> {
  const { rows } = await res
  return rows.map((r) => {
    const sourceId = String(r.id ?? r.block_id ?? r.source_id ?? '')
    const rawSnippet = String(
      r.snippet ?? r.clean_content ?? r.raw_content ?? ''
    )
    return {
      source_id: sourceId,
      source_page: String(r.page ?? ''),
      snippet: rawSnippet.replace(/<\/?mark>/gi, ''),
      type
    }
  })
}
