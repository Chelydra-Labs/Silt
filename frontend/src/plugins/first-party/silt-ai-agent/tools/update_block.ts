// Agent tool #603 — update_block (identity-preserving prose edit).
//
// Replaces a block's body text via ctx.mutateBlock, which preserves the
// block's UUID and identity. TASK metadata (status, owner, due, priority, …)
// lives in STRUCTURED columns, not in clean_content — clean_content is the
// token-stripped prose, and MutateBlock re-renders the markdown from the
// preserved structured fields. So update_block is a prose-only edit: for TASK
// blocks we strip any leading checkbox and [key:: value] metadata tokens the
// model supplied, so it cannot inject conflicting tokens into the re-rendered
// markdown (which would ambiguity-resolve against the preserved fields on the
// next parse). Dedicated tools change metadata — use update_task for TASK
// status/owner/due/priority/tags/recurrence/etc.; NOTE/HEADER blocks rewrite
// freely. Single-block updates are not staged (reversible prose, one undo).

import type { PluginContext } from '../../../sdk'
import { asString } from '../../../../lib/asString'
import { auditWrite } from './_util'
import type { ToolResult } from '../tool-registry'

export const updateBlockToolDef = {
  name: 'update_block',
  description:
    "Rewrite a block's prose body by UUID, preserving its identity. " +
    'TASK metadata (status/owner/due/priority) is stored in structured ' +
    'fields, not the prose, so it is preserved automatically — for TASK ' +
    'blocks any checkbox or [key:: value] tokens in the supplied content ' +
    'are stripped before writing (use update_task to change metadata). ' +
    'NOTE/HEADER blocks may be rewritten freely. ' +
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

export async function handleUpdateBlock(
  ctx: PluginContext,
  args: Record<string, unknown>
): Promise<ToolResult> {
  const blockId = asString(args.block_id).trim()
  if (!blockId) {
    auditWrite(ctx, 'update_block', 'error', blockId)
    return { content: '', error: 'block_id must not be empty' }
  }
  const newContent = asString(args.content)
  if (!newContent.trim()) {
    auditWrite(ctx, 'update_block', 'error', blockId)
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
    auditWrite(ctx, 'update_block', 'error', blockId)
    return { content: '', error: `block ${blockId} not found` }
  }
  const type = asString(row.type).toUpperCase()
  const isTask = type === 'TASK'

  // 2. Compute the body to write. For TASK blocks, strip any checkbox and
  //    metadata tokens the model supplied so the prose cannot inject tokens
  //    that would conflict with the structured fields MutateBlock preserves.
  //    For non-task blocks with a tag override, fold tags in as #hashtags.
  let body: string
  if (isTask) {
    body = stripTaskMetadata(newContent)
    if (!body) {
      auditWrite(ctx, 'update_block', 'error', blockId)
      return {
        content: '',
        error:
          'content had no prose after stripping task metadata; supply a description'
      }
    }
  } else if (tagOverride !== null) {
    body = applyInlineTags(newContent, tagOverride)
  } else {
    body = newContent
  }

  // 3. Mutate the block body.
  const ok = await ctx.mutateBlock(blockId, body)
  if (!ok) {
    auditWrite(ctx, 'update_block', 'error', blockId)
    return { content: '', error: `mutateBlock failed for block ${blockId}` }
  }

  // 4. Apply the tag override for TASK blocks via the dedicated API.
  if (isTask && tagOverride !== null) {
    const tags = normalizeTags(tagOverride)
    const tagOk = await ctx.setTaskTags(blockId, tags)
    if (!tagOk) {
      auditWrite(ctx, 'update_block', 'error', blockId)
      return {
        content: '',
        error: `body updated but setTaskTags failed for block ${blockId}`
      }
    }
  }

  auditWrite(ctx, 'update_block', 'ok', blockId)
  return {
    content: `Updated block ${blockId} (${type || 'BLOCK'}).`
  }
}

// GFM task-list checkbox at the start of a line (`- [ ]`, `- [x]`, `- [/]`,
// `- [~]`). `mutateBlock` writes CleanText; the checkbox is re-emitted from
// the structured Status by the serializer, so a model-supplied checkbox must
// be stripped to avoid a doubled marker in the re-rendered markdown.
const TASK_CHECKBOX_RE = /^\s*[-*+]\s*\[[ xX/~]\]\s*/

// A [key:: value] metadata token (the parser's TaskTokenRegex shape). Any such
// token in the model's content is metadata, not prose, and is stripped so it
// cannot conflict with the structured fields the backend owns.
const TASK_TOKEN_RE = /\[[a-z][a-z0-9_]*\s*::\s*[^\]]*\]/gi

/**
 * Strip a leading task checkbox and every [key:: value] metadata token from a
 * TASK body, returning the pure prose. This keeps update_block a prose edit:
 * the backend preserves structured metadata, and the model cannot inject
 * tokens that would ambiguity-resolve against those fields on the next parse.
 */
export function stripTaskMetadata(text: string): string {
  return text
    .replace(TASK_CHECKBOX_RE, '')
    .replace(TASK_TOKEN_RE, '')
    .replace(/\s{2,}/g, ' ')
    .trim()
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
  return tags.map((t) => asString(t).trim()).filter((t) => t.length > 0)
}
