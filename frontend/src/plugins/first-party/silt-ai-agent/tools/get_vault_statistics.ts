// Agent tool #606 — get_vault_statistics.
//
// Read-only vault health summary: block counts by type, task counts by
// status, distinct notebook/section/page counts, file count, orphan-page
// estimate, stale (overdue) task count, top tag usage, and recently edited
// pages. Every query is a SELECT — no mutation, no staging.
//
// When `scope` is supplied (a notebook name) every aggregation is restricted
// to that notebook via a parameterized WHERE notebook=? clause (the tasks
// table JOINs back to blocks for the scope, since tasks carry no notebook
// column of its own). The output is plain text formatted for the model —
// sections the agent can cite when answering "how big is my vault?" or
// "what needs attention?".

import type { PluginContext } from '../../../sdk'
import type { ToolResult } from '../tool-registry'

export const getVaultStatisticsToolDef = {
  name: 'get_vault_statistics',
  description:
    'Return a read-only summary of vault contents and health: block counts ' +
    'by type, task counts by status, notebook/section/page/file totals, ' +
    'orphan pages (no incoming references), stale (overdue) tasks, top ' +
    'tags, and recently edited pages. Optionally scope every metric to one ' +
    'notebook. No mutation — pure SELECT aggregation.',
  parameters: {
    type: 'object',
    properties: {
      scope: {
        type: 'string',
        description:
          'Optional notebook name to restrict every metric to. Omit for ' +
          'the whole vault (vault + linked notebooks).'
      }
    }
  }
}

/** Upper bound on top-tags + recent-edits lists (each is a "top N" view). */
const TOP_N = 20
const RECENT_N = 5

interface CountRow {
  key: string
  count: number
}

export async function handleGetVaultStatistics(
  ctx: PluginContext,
  args: Record<string, unknown>
): Promise<ToolResult> {
  const scope = typeof args.scope === 'string' ? args.scope.trim() : ''
  const scoped = scope.length > 0

  // blocks/type — always scoped to a real notebook/section/page tuple (the
  // synthetic .silt/tasks.md rows surface as notebook='.silt' and would skew
  // the "what does the user actually have?" answer).
  const blockWhere = scoped ? 'WHERE notebook = ?' : ''
  const blockParams: unknown[] = scoped ? [scope] : []

  const blocksByType = await runCounts(
    ctx,
    `SELECT type, COUNT(*) as count FROM blocks ${blockWhere} GROUP BY type`,
    blockParams,
    'type'
  )
  const locationCounts = await runSingle(
    ctx,
    `SELECT COUNT(DISTINCT notebook) as notebooks, ` +
      `COUNT(DISTINCT section) as sections, ` +
      `COUNT(DISTINCT page) as pages ` +
      `FROM blocks ${blockWhere}`,
    blockParams
  )
  const fileCount = await runSingle(
    ctx,
    scoped
      ? `SELECT COUNT(DISTINCT notebook || char(31) || section || char(31) || page) ` +
          'as total_files FROM blocks WHERE notebook = ?'
      : 'SELECT COUNT(*) as total_files FROM files',
    scoped ? [scope] : []
  )

  // tasks/status — the tasks table has no notebook column; JOIN blocks to
  // apply the scope. Only TODO/DOING/DONE are valid statuses, but a stray
  // value would surface here rather than be silently dropped.
  const taskFrom = scoped
    ? 'FROM tasks t JOIN blocks b ON b.id = t.block_id'
    : 'FROM tasks t'
  const taskWhere = scoped ? 'WHERE b.notebook = ?' : ''
  const taskParams: unknown[] = scoped ? [scope] : []
  const tasksByStatus = await runCounts(
    ctx,
    `SELECT t.status, COUNT(*) as count ${taskFrom} ${taskWhere} GROUP BY t.status`,
    taskParams,
    'status'
  )
  const staleWhere = scoped
    ? 'WHERE b.notebook = ? AND t.due_date IS NOT NULL AND t.due_date < ? AND t.status != ?'
    : 'WHERE t.due_date IS NOT NULL AND t.due_date < ? AND t.status != ?'
  const staleTaskCount = await runSingle(
    ctx,
    `SELECT COUNT(*) as stale_tasks ${taskFrom} ${staleWhere}`,
    scoped ? [scope, ctx.today, 'DONE'] : [ctx.today, 'DONE']
  )

  // Orphan pages: a (notebook, section, page) tuple with no inbound
  // block refs and no inbound wiki links. A page can host many blocks; one
  // inbound reference anywhere rescues the whole page.
  const orphanCount = await runOrphanPageCount(ctx, scoped, scope)

  // Top tags + recently indexed logical pages. Absolute file paths never cross
  // the model boundary; notebook/page metadata is the index's safe identity.
  const tagUsage = await runCounts(
    ctx,
    scoped
      ? 'SELECT t.raw_path, COUNT(*) as count FROM tags t ' +
          'JOIN blocks b ON b.id = t.block_id WHERE b.notebook = ? ' +
          'GROUP BY t.raw_path ORDER BY count DESC LIMIT ?'
      : 'SELECT raw_path, COUNT(*) as count FROM tags ' +
          'GROUP BY raw_path ORDER BY count DESC LIMIT ?',
    scoped ? [scope, TOP_N] : [TOP_N],
    'raw_path'
  )
  const recentEdits = await runRecentEdits(ctx, scoped, scope)

  // --- Compose the summary text the agent consumes. ------------------------
  const sections: string[] = []
  const head = scoped
    ? `vault statistics (scope: ${scope})`
    : 'vault statistics'
  sections.push(head)

  sections.push(formatKv('Blocks by type', blocksByType, '0'))
  sections.push(formatKv('Tasks by status', tasksByStatus, '0'))

  const notebooks = Number(locationCounts.get('notebooks') ?? 0)
  const sectionsCount = Number(locationCounts.get('sections') ?? 0)
  const pagesCount = Number(locationCounts.get('pages') ?? 0)
  const filesTotal = Number(fileCount.get('total_files') ?? 0)
  sections.push(
    'Locations: ' +
      `${notebooks} notebook(s), ${sectionsCount} section(s), ` +
      `${pagesCount} page(s), ${filesTotal} indexed file(s).`
  )

  const stale = Number(staleTaskCount.get('stale_tasks') ?? 0)
  const orphans = Number(orphanCount.get('orphan_pages') ?? 0)
  const attention: string[] = []
  if (stale > 0) attention.push(`${stale} stale task(s) (overdue, not DONE)`)
  if (orphans > 0)
    attention.push(`${orphans} orphan page(s) (no inbound links)`)
  sections.push(
    attention.length > 0
      ? `Needs attention: ${attention.join('; ')}.`
      : 'Needs attention: none — no stale tasks or orphan pages.'
  )

  if (tagUsage.size > 0) {
    sections.push(formatKv('Top tags', tagUsage, String(TOP_N)))
  } else {
    sections.push('Top tags: (none)')
  }

  if (recentEdits.length > 0) {
    const lines = recentEdits.map((r) =>
      r.fileDate ? `  - ${r.path} (file date ${r.fileDate})` : `  - ${r.path}`
    )
    sections.push(`Recently indexed pages (${RECENT_N}): \n${lines.join('\n')}`)
  } else {
    sections.push('Recently indexed pages: (none)')
  }

  return { content: sections.join('\n\n') }
}

// --- helpers --------------------------------------------------------------

/** Run a SELECT that returns { key, count } rows; collapse to a Map. */
async function runCounts(
  ctx: PluginContext,
  sql: string,
  params: unknown[],
  keyCol: string
): Promise<Map<string, number>> {
  const { rows } = await ctx.sqliteQuery(sql, params)
  const map = new Map<string, number>()
  for (const r of rows) {
    const key = String(r[keyCol] ?? r.key ?? '(none)')
    const count = Number(r.count ?? 0)
    map.set(key, (map.get(key) ?? 0) + count)
  }
  return map
}

/** Run a SELECT that returns a single aggregate row; collapse to a Map. */
async function runSingle(
  ctx: PluginContext,
  sql: string,
  params: unknown[]
): Promise<Map<string, unknown>> {
  const { rows } = await ctx.sqliteQuery(sql, params)
  const row = rows[0] ?? {}
  return new Map(Object.entries(row))
}

/** Format a key→count map as "Label:\n  - key: count" lines. */
function formatKv(
  label: string,
  counts: Map<string, number>,
  fallback: string
): string {
  if (counts.size === 0) return `${label}: (none)`
  const entries = [...counts.entries()]
  const lines = entries.map(([k, n]) => `  - ${k}: ${n}`)
  return `${label} (${entries.length}${fallback === '0' ? '' : ` of ${fallback}`}): \n${lines.join('\n')}`
}

/**
 * Approximate orphan-page count: distinct (notebook, section, page) tuples
 * that have NO inbound block reference and NO inbound wiki link. We collect
 * referenced pages from `page_links` (resolved targets) and from
 * `((uuid))` backlinks via the blocks table's existing refs projection; the
 * result is a count of pages NOT in that referenced set.
 *
 * "Approximate" because unresolved or ambiguous wiki links cannot be assigned
 * to a page tuple; this is fine for a stats summary, not a hard guarantee.
 */
async function runOrphanPageCount(
  ctx: PluginContext,
  scoped: boolean,
  scope: string
): Promise<Map<string, unknown>> {
  // Collect target pages from resolved wiki links. The source page is not the
  // page being referenced; using source columns here under-counts inbound
  // links whenever a page links out to a different page.
  const targetClauses: string[] = []
  const targetParams: unknown[] = []
  if (scoped) {
    // Keep unresolved target_raw rows for the JS resolver below; resolved
    // rows can be filtered in SQL without losing deferred links.
    targetClauses.push('(target_notebook = ? OR target_notebook IS NULL)')
    targetParams.push(scope)
  }
  const referenced = await ctx.sqliteQuery(
    `SELECT DISTINCT target_notebook, target_section, target_page, target_raw ` +
      `FROM page_links${
        targetClauses.length > 0 ? ` WHERE ${targetClauses.join(' AND ')}` : ''
      }`,
    targetParams
  )
  const referencedKeys = new Set(
    referenced.rows
      .filter(
        (r) =>
          r.target_notebook != null &&
          r.target_section != null &&
          r.target_page != null
      )
      .map((r) =>
        pageKey(
          String(r.target_notebook),
          String(r.target_section),
          String(r.target_page)
        )
      )
  )

  // All pages in scope.
  const pageWhere = scoped ? 'WHERE notebook = ?' : ''
  const pageParams: unknown[] = scoped ? [scope] : []
  const allPages = await ctx.sqliteQuery(
    `SELECT DISTINCT notebook, section, page FROM blocks ${pageWhere}`,
    pageParams
  )

  // ((uuid)) references are indexed in blocks.raw_content rather than
  // page_links. Resolve those block ids to their target page tuples in JS so
  // this remains correct even when a ref points across notebooks.
  const blockRefs = await ctx.sqliteQuery(
    'SELECT id, notebook, section, page, raw_content FROM blocks',
    []
  )
  const pageByBlock = new Map<string, string>()
  const pageLocations: PageLocation[] = []
  const pageLocationKeys = new Set<string>()
  for (const r of blockRefs.rows) {
    const id = String(r.id ?? '')
      .trim()
      .toLowerCase()
    const location = {
      notebook: String(r.notebook ?? ''),
      section: String(r.section ?? ''),
      page: String(r.page ?? '')
    }
    const locationKey = pageKey(
      location.notebook,
      location.section,
      location.page
    )
    if (!pageLocationKeys.has(locationKey)) {
      pageLocationKeys.add(locationKey)
      pageLocations.push(location)
    }
    if (!id) continue
    pageByBlock.set(id, locationKey)
  }
  // Current page_links rows may retain target_raw while target_* is NULL
  // because resolution is intentionally deferred by the indexer. Resolve the
  // raw path against the indexed page inventory as an equivalent fallback.
  for (const r of referenced.rows) {
    if (r.target_notebook != null) continue
    const target = (String(r.target_raw ?? '').split('|')[0] ?? '')
      .split('#')[0]
      .trim()
    if (!target) continue
    const parts = target
      .split('/')
      .map((part) => part.trim())
      .filter((part) => part.length > 0)
    const page = parts.at(-1) ?? ''
    const section = parts.length >= 2 ? parts.at(-2) : undefined
    const notebook = parts.length >= 3 ? parts.at(-3) : undefined
    const matches = pageLocations.filter(
      (location) =>
        location.page === page &&
        (section === undefined || location.section === section) &&
        (notebook === undefined || location.notebook === notebook) &&
        (!scoped || location.notebook === scope)
    )
    if (matches.length === 1) {
      referencedKeys.add(
        pageKey(matches[0].notebook, matches[0].section, matches[0].page)
      )
    }
  }
  for (const r of blockRefs.rows) {
    const raw = String(r.raw_content ?? '')
    for (const match of raw.matchAll(/\(\(([^()\s]+)\)\)/g)) {
      const targetKey = pageByBlock.get((match[1] ?? '').toLowerCase())
      if (!targetKey) continue
      if (!scoped || targetKey.startsWith(`${scope}\u{1f}/`)) {
        referencedKeys.add(targetKey)
      }
    }
  }

  let orphanCount = 0
  for (const r of allPages.rows) {
    const key = pageKey(
      String(r.notebook ?? ''),
      String(r.section ?? ''),
      String(r.page ?? '')
    )
    if (!referencedKeys.has(key)) orphanCount++
  }
  const map = new Map<string, unknown>()
  map.set('orphan_pages', orphanCount)
  return map
}

interface RecentEditRow {
  path: string
  fileDate: string
}

interface PageLocation {
  notebook: string
  section: string
  page: string
}

/** Most-recently-indexed logical pages (by file_date desc). */
async function runRecentEdits(
  ctx: PluginContext,
  scoped: boolean,
  scope: string
): Promise<RecentEditRow[]> {
  const { rows } = await ctx.sqliteQuery(
    scoped
      ? 'SELECT notebook, section, page, MAX(file_date) as file_date FROM blocks WHERE notebook = ? GROUP BY notebook, section, page ORDER BY file_date DESC LIMIT ?'
      : 'SELECT notebook, section, page, MAX(file_date) as file_date FROM blocks GROUP BY notebook, section, page ORDER BY file_date DESC LIMIT ?',
    scoped ? [scope, RECENT_N] : [RECENT_N]
  )
  return rows.map((r) => ({
    path: [r.notebook, r.section, r.page]
      .map((value) => String(value ?? '').trim())
      .filter((value) => value.length > 0)
      .join('/'),
    fileDate: String(r.file_date ?? '')
  }))
}

/** Stable composite key for a page tuple (Set membership). */
function pageKey(notebook: string, section: string, page: string): string {
  return `${notebook}\u{1f}/${section}\u{1f}/${page}`
}
