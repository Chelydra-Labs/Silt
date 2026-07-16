// Agent tool #598 — read_blocks.
//
// Fetches block contents by UUID so the agent can read what search_notes (or a
// backlink) surfaced. Capped at 20 ids and a fixed output budget; unknown ids
// are skipped with a warning line rather than failing the whole call (a
// stale id from the model should not abort a read of 19 good ones). With
// include_context (default), the parent block + immediate siblings are fetched
// so the model sees the surrounding prose, not an orphaned fragment.

import type { PluginContext } from '../../../sdk'
import type { ToolResult } from '../tool-registry'
import { breadcrumb } from './_util'

export const readBlocksToolDef = {
  name: 'read_blocks',
  description:
    'Read the full content of up to 20 blocks by UUID within a bounded output. Returns each block ' +
    'with its location breadcrumb and (optionally) its parent and sibling ' +
    'blocks for context. Unknown ids are skipped with a warning.',
  parameters: {
    type: 'object',
    required: ['block_ids'],
    properties: {
      block_ids: {
        type: 'array',
        description: 'Block UUIDs to read (max 20).',
        items: { type: 'string' }
      },
      include_context: {
        type: 'boolean',
        description: 'Fetch parent + sibling blocks for context (default true).'
      }
    }
  }
}

interface BlockRow {
  id: string
  clean_content: string | null
  notebook: string
  section: string
  page: string
  type: string
  parent_id: string | null
  depth: number
  line_number: number
}

const MAX_BLOCK_IDS = 20
const MAX_CONTEXT_SIBLINGS = 40
const MAX_OUTPUT_BYTES = 32_000
const OUTPUT_TRUNCATION_MARKER = '[output truncated: size limit reached]'

const BLOCK_SELECT =
  'SELECT id, clean_content, notebook, section, page, type, parent_id, ' +
  'depth, line_number FROM blocks'

export async function handleReadBlocks(
  ctx: PluginContext,
  args: Record<string, unknown>
): Promise<ToolResult> {
  const raw = args.block_ids
  if (!Array.isArray(raw)) {
    return { content: '', error: 'block_ids must be an array of UUIDs' }
  }
  const ids = raw.map((x) => String(x)).filter((s) => s.length > 0)
  if (ids.length === 0) {
    return { content: '', error: 'block_ids must contain at least one UUID' }
  }
  if (ids.length > MAX_BLOCK_IDS) {
    return {
      content: '',
      error: `block_ids exceeds the ${MAX_BLOCK_IDS}-id limit (got ${ids.length}); narrow your request.`
    }
  }
  const includeContext =
    args.include_context === undefined ? true : Boolean(args.include_context)

  // Fetch all requested blocks in one query.
  const found = await fetchByIds(ctx, ids)
  const warnings: string[] = []
  for (const id of ids) {
    if (!found.has(id)) {
      warnings.push(`- warning: block ${id} not found (skipped)`)
    }
  }

  // Context: parents + siblings, batched across the requested set.
  const parents = new Map<string, BlockRow>()
  const siblingsByParent = new Map<string, BlockRow[]>()
  if (includeContext) {
    const parentIds = unique(
      [...found.values()]
        .map((b) => b.parent_id)
        .filter((p): p is string => !!p && !found.has(p))
    )
    if (parentIds.length > 0) {
      for (const p of await fetchByIds(ctx, parentIds)) parents.set(p[0], p[1])
      // Siblings share the parent id; exclude the requested blocks themselves.
      const foundIds = [...found.keys()]
      for (const [pid, sibs] of await fetchByParentIds(
        ctx,
        parentIds,
        foundIds
      )) {
        siblingsByParent.set(pid, sibs)
      }
    }
  }

  const sections: string[] = []
  const evidence: NonNullable<ToolResult['evidence']> = []
  let i = 0
  for (const id of ids) {
    const block = found.get(id)
    if (!block) continue
    i++
    sections.push(formatBlock(i, block))
    evidence.push({
      citationIndex: i,
      blockId: block.id,
      notebook: block.notebook,
      section: block.section,
      page: block.page,
      lineNumber: block.line_number,
      snippet: (block.clean_content ?? '').slice(0, 200),
      title: breadcrumb(block.notebook, block.section, block.page)
    })
    if (includeContext) {
      const ctxLines = formatContext(block, parents, siblingsByParent)
      if (ctxLines.length > 0) {
        sections.push(
          `  Context:\n${ctxLines.map((l) => `    ${l}`).join('\n')}`
        )
      }
    }
  }

  const out = [...warnings, ...sections].filter((s) => s.length > 0)
  if (out.length === 0) {
    return { content: 'No blocks found for the given ids.' }
  }
  return { content: capOutput(out.join('\n\n')), evidence }
}

async function fetchByIds(
  ctx: PluginContext,
  ids: string[]
): Promise<Map<string, BlockRow>> {
  if (ids.length === 0) return new Map()
  const placeholders = ids.map(() => '?').join(',')
  const { rows } = await ctx.sqliteQuery(
    `${BLOCK_SELECT} WHERE id IN (${placeholders})`,
    ids
  )
  const map = new Map<string, BlockRow>()
  for (const r of rows) {
    map.set(String(r.id), rowToBlock(r))
  }
  return map
}

async function fetchByParentIds(
  ctx: PluginContext,
  parentIds: string[],
  excludeIds: string[]
): Promise<Map<string, BlockRow[]>> {
  if (parentIds.length === 0) return new Map()
  const parentPh = parentIds.map(() => '?').join(',')
  const excludePh = excludeIds.map(() => '?').join(',')
  const sql =
    excludeIds.length > 0
      ? `${BLOCK_SELECT} WHERE parent_id IN (${parentPh}) AND id NOT IN (${excludePh}) ORDER BY line_number LIMIT ?`
      : `${BLOCK_SELECT} WHERE parent_id IN (${parentPh}) ORDER BY line_number LIMIT ?`
  const params =
    excludeIds.length > 0
      ? [...parentIds, ...excludeIds, MAX_CONTEXT_SIBLINGS]
      : [...parentIds, MAX_CONTEXT_SIBLINGS]
  const { rows } = await ctx.sqliteQuery(sql, params)
  const map = new Map<string, BlockRow[]>()
  for (const r of rows) {
    const b = rowToBlock(r)
    const pid = b.parent_id ?? ''
    const list = map.get(pid) ?? []
    list.push(b)
    map.set(pid, list)
  }
  return map
}

function rowToBlock(r: Record<string, unknown>): BlockRow {
  return {
    id: String(r.id ?? ''),
    clean_content: r.clean_content == null ? null : String(r.clean_content),
    notebook: String(r.notebook ?? ''),
    section: String(r.section ?? ''),
    page: String(r.page ?? ''),
    type: String(r.type ?? ''),
    parent_id:
      r.parent_id == null || r.parent_id === '' ? null : String(r.parent_id),
    depth: Number(r.depth ?? 0),
    line_number: Number(r.line_number ?? 0)
  }
}

function formatBlock(index: number, b: BlockRow): string {
  const loc = breadcrumb(b.notebook, b.section, b.page)
  const body = (b.clean_content ?? '').trim()
  return [
    `[${index}] block ${b.id} (${b.type || 'BLOCK'})`,
    `    location: ${loc} (line ${b.line_number})`,
    `    ${body.replace(/\n/g, '\n    ')}`
  ].join('\n')
}

function formatContext(
  b: BlockRow,
  parents: Map<string, BlockRow>,
  siblingsByParent: Map<string, BlockRow[]>
): string[] {
  const lines: string[] = []
  const pid = b.parent_id
  const parent = pid ? parents.get(pid) : undefined
  if (parent) {
    lines.push(`parent [${parent.id}]: ${preview(parent.clean_content)}`)
  }
  const siblings = pid ? (siblingsByParent.get(pid) ?? []) : []
  for (const s of siblings.slice(0, MAX_CONTEXT_SIBLINGS)) {
    lines.push(`sibling [${s.id}]: ${preview(s.clean_content)}`)
  }
  return lines
}

function capOutput(text: string): string {
  const encoder = new TextEncoder()
  if (encoder.encode(text).length <= MAX_OUTPUT_BYTES) return text
  const suffix = `\n\n${OUTPUT_TRUNCATION_MARKER}`
  const available = Math.max(
    0,
    MAX_OUTPUT_BYTES - encoder.encode(suffix).length
  )
  let low = 0
  let high = text.length
  while (low < high) {
    const middle = Math.ceil((low + high) / 2)
    if (encoder.encode(text.slice(0, middle)).length <= available) low = middle
    else high = middle - 1
  }
  return `${text.slice(0, low).trimEnd()}${suffix}`
}

function preview(text: string | null, max = 160): string {
  const t = (text ?? '').trim().replace(/\n/g, ' ')
  return t.length > max ? `${t.slice(0, max)}…` : t
}

function unique(arr: string[]): string[] {
  return [...new Set(arr)]
}
