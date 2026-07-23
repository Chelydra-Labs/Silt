// Agent tool #604 — tag_management (three separate tools, not one god-tool).
//
// list_tags and find_untagged are read-only and run without staging. rename_tag
// is a destructive bulk operation: a single call rewrites the hashtag token in
// every matching block, so it routes through the Phase 5 staging protocol
// (stageOperation at request time, commit at confirm time). Splitting the
// surface into three tools lets the model call list/find freely while making
// rename's blast radius explicit.

import type { PluginContext } from '../../../sdk'
import { asString } from '../../../../lib/asString'
import type { AgentToolDef, ToolResult } from '../tool-registry'
import { stageOperation } from '../staging'
import { breadcrumb, clampInt } from './_util'

// --- list_tags ------------------------------------------------------------

const MAX_TAGS = 200

export const listTagsToolDef = {
  name: 'list_tags',
  description:
    'List all tags in the vault with the number of blocks carrying each. ' +
    'Sorted by usage descending. Useful for spotting typos or near-duplicate ' +
    'tag paths before a rename.',
  parameters: {
    type: 'object',
    properties: {}
  }
}

export async function handleListTags(
  ctx: PluginContext,
  _args: Record<string, unknown>
): Promise<ToolResult> {
  const { rows } = await ctx.sqliteQuery(
    'SELECT raw_path, COUNT(*) AS count FROM tags ' +
      'GROUP BY raw_path ORDER BY count DESC, raw_path ASC LIMIT ?',
    [MAX_TAGS]
  )
  if (rows.length === 0) {
    return { content: 'No tags found in the vault.' }
  }
  const { rows: totalRows } = await ctx.sqliteQuery(
    'SELECT COUNT(DISTINCT raw_path) AS total FROM tags'
  )
  const total = Math.max(
    rows.length,
    Number(totalRows[0]?.total ?? rows.length)
  )
  const visible = rows.slice(0, MAX_TAGS)
  const lines = visible.map((r, i) => {
    const tag = asString(r.raw_path)
    const count = Number(r.count ?? 0)
    return `[${i + 1}] #${tag} (${count} block${count === 1 ? '' : 's'})`
  })
  const more = total - visible.length
  return {
    content:
      `${total} tag(s):\n${lines.join('\n')}` +
      (more > 0 ? `\n…and ${more} more tag(s) not shown.` : '')
  }
}

// --- find_untagged --------------------------------------------------------

export const findUntaggedToolDef = {
  name: 'find_untagged',
  description:
    'List TASK blocks that have no tags. Useful for triage: tasks without ' +
    'tags fall out of tag-scoped Kanban views and dashboards. Optional scope ' +
    'restricts to a single notebook.',
  parameters: {
    type: 'object',
    properties: {
      scope: {
        type: 'string',
        description: 'Restrict results to a notebook (optional).'
      },
      limit: {
        type: 'integer',
        description: 'Max results (default 20, clamped to 1–100).',
        minimum: 1,
        maximum: 100
      }
    }
  }
}

export async function handleFindUntagged(
  ctx: PluginContext,
  args: Record<string, unknown>
): Promise<ToolResult> {
  const limit = clampInt(args.limit, 20, 1, 100)
  const scope =
    typeof args.scope === 'string' && args.scope.length > 0 ? args.scope : null

  const sql = scope
    ? 'SELECT b.id, b.clean_content, b.notebook, b.section, b.page ' +
      'FROM blocks b ' +
      "WHERE b.type = 'TASK' AND b.notebook = ? " +
      'AND b.id NOT IN (SELECT block_id FROM tags) ' +
      'ORDER BY b.notebook, b.section, b.page, b.line_number ' +
      'LIMIT ?'
    : 'SELECT b.id, b.clean_content, b.notebook, b.section, b.page ' +
      'FROM blocks b ' +
      "WHERE b.type = 'TASK' " +
      'AND b.id NOT IN (SELECT block_id FROM tags) ' +
      'ORDER BY b.notebook, b.section, b.page, b.line_number ' +
      'LIMIT ?'
  const params = scope ? [scope, limit] : [limit]

  const { rows } = await ctx.sqliteQuery(sql, params)
  if (rows.length === 0) {
    return { content: 'No untagged tasks found.' }
  }
  const lines = rows.map((r, i) => {
    const id = asString(r.id)
    const body = asString(r.clean_content).trim().replace(/\n/g, ' ')
    const snippet = body.length > 120 ? `${body.slice(0, 120)}…` : body
    return [
      `[${i + 1}] block ${id}`,
      `    location: ${breadcrumb(
        asString(r.notebook),
        asString(r.section),
        asString(r.page)
      )}`,
      `    ${snippet}`
    ].join('\n')
  })
  return {
    content: `${rows.length} untagged task(s):\n\n${lines.join('\n\n')}`
  }
}

// --- rename_tag (STAGED) --------------------------------------------------

export const renameTagToolDef = {
  name: 'rename_tag',
  description:
    'Rename a tag across every block that uses it. Destructive bulk ' +
    'operation: stages a preview for user confirmation; on confirm, rewrites ' +
    'the #hashtag token in every matching block via mutateBlock and reports ' +
    'the count actually renamed.',
  parameters: {
    type: 'object',
    required: ['old_tag', 'new_tag'],
    properties: {
      old_tag: {
        type: 'string',
        description: 'Existing tag path (no leading #).'
      },
      new_tag: {
        type: 'string',
        description: 'Replacement tag path (no leading #).'
      }
    }
  }
}

/**
 * Stage a rename_tag operation: count blocks carrying the exact tag, persist
 * the staged op, and return the token + preview. The actual rewrite happens in
 * commit() after the user confirms. Namespace descendants are separate tags.
 */
export async function handleRenameTag(
  ctx: PluginContext,
  args: Record<string, unknown>
): Promise<ToolResult> {
  const oldTag = stripLeadingHash(asString(args.old_tag).trim())
  const newTag = stripLeadingHash(asString(args.new_tag).trim())
  if (!oldTag) {
    return { content: '', error: 'old_tag must not be empty' }
  }
  if (!newTag) {
    return { content: '', error: 'new_tag must not be empty' }
  }
  if (oldTag === newTag) {
    return { content: '', error: 'old_tag and new_tag must differ' }
  }
  if (!isValidTagPath(oldTag) || !isValidTagPath(newTag)) {
    return {
      content: '',
      error: 'tags may only contain letters, numbers, "/", "_" and "-"'
    }
  }

  // Commit re-issues the same exact-tag query to get the live set at apply
  // time (a block may have been tagged/untagged between stage + confirm).
  const { rows } = await queryExactTag(ctx, oldTag)
  const affected = rows.length

  const token = await stageOperation(ctx, 'rename_tag', {
    old_tag: oldTag,
    new_tag: newTag
  })

  return {
    content: '',
    isStaged: true,
    stagedToken: token,
    stagedPreview: {
      kind: 'rename_tag',
      summary: `Rename tag #${oldTag} → #${newTag} across ${affected} block${affected === 1 ? '' : 's'}`,
      affectedCount: affected
    }
  }
}

/**
 * Commit half of rename_tag. Re-issues queryByTag to get the live set, then
 * rewrites the #oldtag token → #newtag in every matching block via mutateBlock
 * and reports the real count of blocks touched. A block that no longer
 * carries the tag (untagged between stage + confirm) is skipped — the count
 * reflects the actual rewrite, not the preview.
 *
 * `params` is the operation payload stored at stage time (NOT the model's
 * args), so the model cannot mutate old_tag/new_tag between staging and
 * confirmation.
 */
export async function commitRenameTag(
  ctx: PluginContext,
  params: Record<string, unknown>
): Promise<ToolResult> {
  const oldTag = stripLeadingHash(asString(params.old_tag).trim())
  const newTag = stripLeadingHash(asString(params.new_tag).trim())
  if (
    !oldTag ||
    !newTag ||
    !isValidTagPath(oldTag) ||
    !isValidTagPath(newTag)
  ) {
    return { content: '', error: 'staged rename_tag params were malformed' }
  }

  const { rows } = await queryExactTag(ctx, oldTag)
  if (rows.length === 0) {
    return {
      content: `No blocks carry tag #${oldTag}; nothing renamed.`
    }
  }

  const renameRe = buildTagRegex(oldTag)
  let renamed = 0
  const failed: string[] = []
  for (const r of rows) {
    const id = asString(r.id)
    const body = asString(r.clean_content)
    if (!id || !renameRe.test(body)) {
      // The block no longer carries the literal token (e.g. content changed
      // or the block changed after staging). Skip it — the count must reflect
      // actual rewrites.
      renameRe.lastIndex = 0
      continue
    }
    renameRe.lastIndex = 0
    // Function replacer: a string replacement would interpret $&, $', etc. in
    // newTag as match references and corrupt the body. The grammar check above
    // bounds newTag to tag chars, but the function form is the safe contract.
    const next = body.replace(renameRe, () => `#${newTag}`)
    const ok = await ctx.mutateBlock(id, next)
    if (ok) {
      renamed++
    } else {
      failed.push(id)
    }
  }

  const summary = `Renamed #${oldTag} → #${newTag} in ${renamed} block${renamed === 1 ? '' : 's'}.`
  if (failed.length > 0) {
    return {
      content: `${summary} ${failed.length} block(s) failed: ${failed.join(', ')}`,
      error: `mutateBlock failed for ${failed.length} block(s)`
    }
  }
  return { content: summary }
}

/** Query the same exact-tag set used by both preview and commit. */
async function queryExactTag(
  ctx: PluginContext,
  tag: string
): Promise<{ rows: Record<string, unknown>[] }> {
  return ctx.sqliteQuery(
    'SELECT DISTINCT b.id, b.clean_content FROM blocks b ' +
      'JOIN tags t ON t.block_id = b.id WHERE t.raw_path = ?',
    [tag]
  )
}

/**
 * Build the regex that matches `#<tag>` exactly (not as a prefix of a longer
 * tag). Tag chars are [A-Za-z0-9/_-]; the negative lookahead ensures the
 * match ends at a non-tag char so `#work` does not match `#workflow`.
 * Global flag so String.replace rewrites all occurrences in a block.
 */
function buildTagRegex(tag: string): RegExp {
  const escaped = tag.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  return new RegExp(`#${escaped}(?![A-Za-z0-9/_-])`, 'g')
}

/** Strip an optional leading '#' from a tag path (users often include it). */
function stripLeadingHash(s: string): string {
  return s.startsWith('#') ? s.slice(1) : s
}

/** Canonical tag grammar: letters, numbers, "/", "_", "-". Used to reject
 *  replacement-string metacharacters ($ &, spaces, newlines) in rename targets. */
const TAG_PATH_RE = /^[A-Za-z0-9/_-]+$/

function isValidTagPath(tag: string): boolean {
  return TAG_PATH_RE.test(tag)
}

// --- Tool defs with handlers wired (consumed by registerP1Tools) ---------

export const listTagsTool: AgentToolDef = {
  ...listTagsToolDef,
  handler: handleListTags
}

export const findUntaggedTool: AgentToolDef = {
  ...findUntaggedToolDef,
  handler: handleFindUntagged
}

export const renameTagTool: AgentToolDef = {
  ...renameTagToolDef,
  handler: handleRenameTag,
  commit: commitRenameTag
}
