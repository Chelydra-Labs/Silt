// Build action scope from active page / selection (#230–#231).

import type { PluginContext } from '../../sdk'
import { asString } from '../../../lib/asString'
import { stripIdentityComments, truncateForPrompt } from './text'
import type { AssistantSettings, ScopeContext } from './types'

export type PageBlock = {
  id: string
  clean_content: string
  type?: string
}

export async function fetchPageBlocks(
  ctx: PluginContext,
  notebook: string,
  section: string,
  page: string
): Promise<PageBlock[]> {
  const { rows } = await ctx.sqliteQuery(
    `SELECT id, clean_content, type
       FROM blocks
      WHERE notebook = ? AND section = ? AND page = ?
      ORDER BY line_number`,
    [notebook, section, page]
  )
  return rows.map((r) => ({
    id: asString(r.id),
    clean_content: asString(r.clean_content),
    type: typeof r.type === 'string' ? r.type : undefined
  }))
}

/**
 * Resolve which block a selection belongs to.
 * Prefer exact full-block match, then unique containment, then longest match.
 */
export function resolveBlockForSelection(
  blocks: PageBlock[],
  selectionText: string
): { id: string; blockText: string; fullBlock: boolean } | null {
  const sel = selectionText.trim()
  if (!sel) return null

  const exact = blocks.find((b) => b.clean_content.trim() === sel)
  if (exact) {
    return { id: exact.id, blockText: exact.clean_content, fullBlock: true }
  }

  const containing = blocks.filter((b) => b.clean_content.includes(sel))
  if (containing.length === 1) {
    const b = containing[0]
    return {
      id: b.id,
      blockText: b.clean_content,
      fullBlock: b.clean_content.trim() === sel
    }
  }
  if (containing.length > 1) {
    // Prefer the shortest block that still contains the selection (tightest match).
    containing.sort((a, b) => a.clean_content.length - b.clean_content.length)
    const b = containing[0]
    return {
      id: b.id,
      blockText: b.clean_content,
      fullBlock: b.clean_content.trim() === sel
    }
  }
  return null
}

export async function buildScope(
  ctx: PluginContext,
  settings: AssistantSettings,
  opts: {
    selectionText?: string
    blockId?: string
    instruction?: string
  } = {}
): Promise<ScopeContext> {
  const notebook = ctx.activeNotebook || ''
  const section = ctx.activeSection || ''
  const page = ctx.activePage || ''

  let inputText = ''
  let targetBlockId: string | undefined
  let targetBlockText: string | undefined
  let existingTaskTitles: string[] = []
  let replaceFullBlock = false

  if (opts.selectionText?.trim()) {
    inputText = opts.selectionText
  }

  if (notebook && page) {
    const blocks = await fetchPageBlocks(ctx, notebook, section, page)
    existingTaskTitles = blocks
      .filter((b) => b.type === 'TASK')
      .map((b) => b.clean_content)

    if (opts.blockId) {
      const b = blocks.find((x) => x.id === opts.blockId)
      if (b) {
        targetBlockId = b.id
        targetBlockText = b.clean_content
        replaceFullBlock = true
        if (!inputText.trim()) inputText = b.clean_content
      }
    }

    if (opts.selectionText?.trim() && !targetBlockId) {
      const resolved = resolveBlockForSelection(blocks, opts.selectionText)
      if (resolved) {
        targetBlockId = resolved.id
        targetBlockText = resolved.blockText
        replaceFullBlock = resolved.fullBlock
      }
    }

    if (!inputText.trim()) {
      if (!targetBlockId) {
        inputText = blocks.map((b) => b.clean_content).join('\n')
      }
    }
  }

  const cleaned = stripIdentityComments(inputText)
  const { text, truncated } = truncateForPrompt(
    cleaned,
    settings.max_input_chars
  )

  return {
    notebook,
    section,
    page,
    blockId: opts.blockId ?? targetBlockId,
    inputText: text,
    truncated,
    selectionText: opts.selectionText?.trim() || undefined,
    existingTaskTitles,
    targetBlockId,
    targetBlockText,
    replaceFullBlock
  }
}
