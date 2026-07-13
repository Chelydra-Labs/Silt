// Apply accepted proposals via PluginContext mutators only (#230).
// Never called from stream completion — only from explicit Accept.

import type { PluginContext } from '../../../sdk'
import type { Proposal } from '../types'

export type ApplyResult =
  { ok: true; detail?: string } | { ok: false; error: string }

/**
 * Apply a ready proposal. Mutates the vault only through SDK content-mutate APIs.
 */
export async function applyProposal(
  ctx: PluginContext,
  proposal: Proposal
): Promise<ApplyResult> {
  if (proposal.status !== 'ready') {
    return { ok: false, error: 'Proposal is not ready to accept' }
  }

  try {
    switch (proposal.kind) {
      case 'replace-selection':
      case 'insert-below':
        return await applyMarkdown(ctx, proposal)
      case 'insert-tasks':
        return await applyTasks(ctx, proposal)
      case 'apply-tags':
        return await applyTags(ctx, proposal)
      case 'insert-links':
        return await applyLinks(ctx, proposal)
      default:
        return { ok: false, error: `Unknown proposal kind: ${proposal.kind}` }
    }
  } catch (e) {
    const msg = e instanceof Error ? e.message : String(e)
    return { ok: false, error: msg }
  }
}

/** Splice proposed text into a block: partial selection replace or full replace. */
export function buildReplacedBlockText(
  blockText: string,
  selectionText: string | undefined,
  proposed: string,
  replaceFullBlock?: boolean
): string {
  if (replaceFullBlock || !selectionText?.trim()) {
    return proposed
  }
  const sel = selectionText
  const idx = blockText.indexOf(sel)
  if (idx < 0) {
    // Selection not found in stored block text — fall back to full replace.
    return proposed
  }
  return blockText.slice(0, idx) + proposed + blockText.slice(idx + sel.length)
}

async function applyMarkdown(
  ctx: PluginContext,
  proposal: Proposal
): Promise<ApplyResult> {
  const md = proposal.proposedMarkdown.trim()
  if (!md) return { ok: false, error: 'Empty proposal' }

  const blockId = proposal.scope.targetBlockId
  if (proposal.kind === 'replace-selection' && blockId) {
    const next = buildReplacedBlockText(
      proposal.scope.targetBlockText ?? md,
      proposal.scope.selectionText,
      md,
      proposal.scope.replaceFullBlock
    )
    const ok = await ctx.mutateBlock(blockId, next)
    if (!ok) return { ok: false, error: 'mutateBlock failed' }
    return { ok: true, detail: 'Block updated' }
  }

  const { notebook, section, page } = proposal.scope
  if (!notebook || !page) {
    return {
      ok: false,
      error: 'No active page — open a note before inserting'
    }
  }
  await ctx.createBlock({
    notebook,
    section: section || '',
    page,
    type: 'NOTE',
    text: md
  })
  return { ok: true, detail: 'Inserted into note' }
}

async function applyTasks(
  ctx: PluginContext,
  proposal: Proposal
): Promise<ApplyResult> {
  const tasks = (proposal.tasks ?? []).map((t) => t.trim()).filter(Boolean)
  if (tasks.length === 0) return { ok: false, error: 'No tasks to insert' }

  const { notebook, section, page } = proposal.scope
  let applied = 0
  const errors: string[] = []
  for (const title of tasks) {
    try {
      if (notebook && page) {
        await ctx.createBlock({
          notebook,
          section: section || '',
          page,
          type: 'TASK',
          text: title
        })
      } else {
        await ctx.createTask({ title, status: 'TODO' })
      }
      applied++
    } catch (e) {
      errors.push(e instanceof Error ? e.message : String(e))
    }
  }
  if (applied === 0) {
    return {
      ok: false,
      error: errors[0] || 'Failed to create tasks'
    }
  }
  if (errors.length) {
    return {
      ok: true,
      detail: `Inserted ${applied} of ${tasks.length} tasks (${errors.length} failed)`
    }
  }
  return { ok: true, detail: `Inserted ${applied} task(s)` }
}

async function loadExistingTags(
  ctx: PluginContext,
  blockId: string
): Promise<string[]> {
  try {
    const { rows } = await ctx.sqliteQuery(
      `SELECT raw_path AS tag FROM tags WHERE block_id = ?`,
      [blockId]
    )
    return rows
      .map((r) =>
        typeof r.tag === 'string' ? r.tag.replace(/^#/, '').trim() : ''
      )
      .filter(Boolean)
  } catch {
    return []
  }
}

async function applyTags(
  ctx: PluginContext,
  proposal: Proposal
): Promise<ApplyResult> {
  const tags = (proposal.selectedTags ?? proposal.tags?.map((t) => t.tag) ?? [])
    .map((t) => t.replace(/^#/, '').trim())
    .filter(Boolean)
  if (tags.length === 0) return { ok: false, error: 'No tags selected' }

  const blockId = proposal.scope.targetBlockId ?? proposal.scope.blockId
  if (blockId) {
    try {
      const existing = await loadExistingTags(ctx, blockId)
      const merged = [...new Set([...existing, ...tags])]
      await ctx.setTaskTags(blockId, merged)
      return { ok: true, detail: 'Tags merged onto task' }
    } catch {
      /* fall through to hashtag insert */
    }
  }

  const { notebook, section, page } = proposal.scope
  if (!notebook || !page) {
    return { ok: false, error: 'No active page for tag insert' }
  }
  const line = tags.map((t) => `#${t}`).join(' ')
  await ctx.createBlock({
    notebook,
    section: section || '',
    page,
    type: 'NOTE',
    text: line
  })
  return { ok: true, detail: 'Tags inserted' }
}

async function applyLinks(
  ctx: PluginContext,
  proposal: Proposal
): Promise<ApplyResult> {
  const ids =
    proposal.selectedRelatedIds ?? proposal.related?.map((r) => r.blockId) ?? []
  const unique = [...new Set(ids.filter(Boolean))]
  if (unique.length === 0) return { ok: false, error: 'No links selected' }

  const { notebook, section, page } = proposal.scope
  if (!notebook || !page) {
    return { ok: false, error: 'No active page for link insert' }
  }
  const lines = ['Related:', ...unique.map((id) => `- ((${id}))`)].join('\n')
  await ctx.createBlock({
    notebook,
    section: section || '',
    page,
    type: 'NOTE',
    text: lines
  })
  return { ok: true, detail: 'Related links inserted' }
}
