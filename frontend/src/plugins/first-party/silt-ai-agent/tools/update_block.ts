// Agent tool #603 — update_block (identity-preserving prose edit).
//
// Replaces a block's body text via ctx.mutateBlock, which preserves the
// block's UUID and identity. For TASK blocks the new content must not strip
// any existing task-metadata tokens (status, owner, due, priority) or the
// checkbox state — those have dedicated tools (setTaskDueDate, setTaskOwner,
// setTaskPriority, updateBlockState) and silently dropping them via a prose
// rewrite would be data loss. NOTE/HEADER blocks may be rewritten freely.
//
// Single-block updates do NOT stage: the change is reversible prose and the
// mutation round-trips through the markdown file (source of truth). A future
// bulk variant that touches many blocks at once would route through the
// Phase 5 staging protocol (see staging.ts), but this single-block tool is
// non-staged.

import type { PluginContext } from '../../../sdk'
import type { ToolResult } from '../tool-registry'

export const updateBlockToolDef = {
  name: 'update_block',
  description:
    "Rewrite a block's prose body by UUID, preserving its identity and " +
    '(for TASK blocks) existing metadata tokens. For TASK blocks, the new ' +
    'content must retain any [status::], [owner::], [due::], [priority::] ' +
    'tokens and checkbox state present in the original — use the dedicated ' +
    'tools to change those. NOTE/HEADER blocks may be rewritten freely. ' +
    "Optional tags override the block's tags.",
  parameters: {
    type: 'object',
    required: ['block_id', 'content'],
    properties: {
      block_id: { type: 'string', description: 'Block UUID to update.' },
      content: {
        type: 'string',
        description: 'New prose body (replaces the existing text).'
      },
      tags: {
        type: 'array',
        description:
          'Optional tag override. For TASK blocks this is applied via ' +
          'setTaskTags; for NOTE/HEADER blocks tags are folded into the ' +
          'content as #hashtags before mutation.',
        items: { type: 'string' }
      }
    }
  }
}

/** Metadata tokens whose removal from a TASK block would be silent data loss. */
const TASK_TOKEN_KEYS = ['status', 'owner', 'due', 'priority'] as const

/** A captured metadata token: key + raw bracketed text + value. */
interface TaskToken {
  key: string
  raw: string
  value: string
}

export async function handleUpdateBlock(
  ctx: PluginContext,
  args: Record<string, unknown>
): Promise<ToolResult> {
  const blockId = String(args.block_id ?? '').trim()
  if (!blockId) {
    return { content: '', error: 'block_id must not be empty' }
  }
  const newContent = String(args.content ?? '')
  if (!newContent.trim()) {
    return { content: '', error: 'content must not be empty' }
  }
  const tagOverride = Array.isArray(args.tags) ? (args.tags as unknown[]) : null

  // 1. Fetch the current block.
  const { rows } = await ctx.sqliteQuery(
    'SELECT clean_content, type FROM blocks WHERE id = ?',
    [blockId]
  )
  const row = rows[0]
  if (!row) {
    return { content: '', error: `block ${blockId} not found` }
  }
  const currentContent = String(row.clean_content ?? '')
  const type = String(row.type ?? '').toUpperCase()

  // 2. TASK blocks: reject if the new content strips any existing token or
  //    the checkbox state.
  if (type === 'TASK') {
    const stripped = findStrippedTokens(currentContent, newContent)
    if (stripped.length > 0) {
      return {
        content: '',
        error:
          'Cannot remove task metadata (status/owner/due/priority). ' +
          'Use specific tools to change them. Stripped: ' +
          stripped.join(', ')
      }
    }
  }

  // 3. Compute the body to write. For non-task blocks with a tag override,
  //    fold the tags into the body as #hashtags before mutating. For TASK
  //    blocks, setTaskTags applies the override after the body rewrite.
  const isTask = type === 'TASK'
  const body =
    !isTask && tagOverride !== null
      ? applyInlineTags(newContent, tagOverride)
      : newContent

  // 4. Mutate the block body.
  const ok = await ctx.mutateBlock(blockId, body)
  if (!ok) {
    return { content: '', error: `mutateBlock failed for block ${blockId}` }
  }

  // 5. Apply the tag override for TASK blocks via the dedicated API.
  if (isTask && tagOverride !== null) {
    const tags = normalizeTags(tagOverride)
    const tagOk = await ctx.setTaskTags(blockId, tags)
    if (!tagOk) {
      return {
        content: '',
        error: `body updated but setTaskTags failed for block ${blockId}`
      }
    }
  }

  return {
    content: `Updated block ${blockId} (${type || 'BLOCK'}).`
  }
}

/**
 * Compare old vs new content and list which task-metadata tokens (or the
 * checkbox state) the rewrite would remove. Returns the raw removed tokens
 * (e.g. "[due:: 2026-07-20]", "[checkbox: - [x]]") so the error message is
 * actionable. An empty array means the rewrite is safe.
 */
export function findStrippedTokens(
  oldContent: string,
  newContent: string
): string[] {
  const stripped: string[] = []

  const oldTokens = new Map<string, TaskToken>()
  for (const t of extractTaskTokens(oldContent)) oldTokens.set(t.key, t)
  const newKeys = new Set(extractTaskTokens(newContent).map((t) => t.key))

  // Each metadata key in the old content must still be present in the new.
  for (const key of TASK_TOKEN_KEYS) {
    const oldTok = oldTokens.get(key)
    if (oldTok && !newKeys.has(key)) {
      stripped.push(oldTok.raw)
    }
  }

  // Checkbox state — a TASK's status is encoded as `- [ ]`/`- [x]`/`- [~]`.
  // Removing the checkbox would silently drop the task from the task index.
  const oldCheck = extractCheckbox(oldContent)
  const newCheck = extractCheckbox(newContent)
  if (oldCheck !== null && newCheck === null) {
    stripped.push(`checkbox: ${oldCheck.raw}`)
  }

  return stripped
}

/**
 * Extract the four supported metadata tokens from a task body. Token form is
 * case-insensitive `[key:: value]`. The regex tolerates surrounding whitespace
 * and arbitrary value content up to the closing bracket.
 */
function extractTaskTokens(text: string): TaskToken[] {
  const out: TaskToken[] = []
  const re = /\[(status|owner|due|priority)\s*::\s*([^\]]*)\]/gi
  for (const m of text.matchAll(re)) {
    out.push({
      key: m[1].toLowerCase(),
      raw: m[0],
      value: (m[2] ?? '').trim()
    })
  }
  return out
}

/**
 * Extract the GFM task-list checkbox at the start of the first line. Returns
 * the raw marker ("- [ ]", "- [x]", "- [~]") or null when none is present.
 */
function extractCheckbox(text: string): { raw: string } | null {
  const m = text.match(/^\s*[-*+]\s*\[([ xX~])\]/)
  if (!m) return null
  return { raw: m[0].trim() }
}

/** Append a tags array to a body as space-separated #hashtags. */
function applyInlineTags(body: string, tags: unknown[]): string {
  const tagPart = normalizeTags(tags)
    .map((t) => (t.startsWith('#') ? t : `#${t}`))
    .join(' ')
  return tagPart ? `${body} ${tagPart}` : body
}

/** Coerce a raw tags array to a clean string[] (drop empties, no leading #). */
function normalizeTags(tags: unknown[]): string[] {
  return tags.map((t) => String(t).trim()).filter((t) => t.length > 0)
}
