// Apply accepted proposals via PluginContext mutators only (#230).
// Never called from stream completion — only from explicit Accept.

import type { PluginContext } from '../../../sdk'
import type { Proposal } from '../types'

export type ApplyResult = { ok: true } | { ok: false; error: string }

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

async function applyMarkdown(
  ctx: PluginContext,
  proposal: Proposal
): Promise<ApplyResult> {
  const md = proposal.proposedMarkdown.trim()
  if (!md) return { ok: false, error: 'Empty proposal' }

  const blockId = proposal.scope.targetBlockId
  if (proposal.kind === 'replace-selection' && blockId) {
    // Replace whole block clean content while preserving identity via mutateBlock.
    const ok = await ctx.mutateBlock(blockId, md)
    if (!ok) return { ok: false, error: 'mutateBlock failed' }
    return { ok: true }
  }

  // Insert as a new NOTE block on the active page.
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
  return { ok: true }
}

async function applyTasks(
  ctx: PluginContext,
  proposal: Proposal
): Promise<ApplyResult> {
  const tasks = (proposal.tasks ?? []).map((t) => t.trim()).filter(Boolean)
  if (tasks.length === 0) return { ok: false, error: 'No tasks to insert' }

  const { notebook, section, page } = proposal.scope
  for (const title of tasks) {
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
  }
  return { ok: true }
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
    // Merge with existing tags on a task when possible.
    try {
      await ctx.setTaskTags(blockId, tags)
      return { ok: true }
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
  return { ok: true }
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
  return { ok: true }
}
