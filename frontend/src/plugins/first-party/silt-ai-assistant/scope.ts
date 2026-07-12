// Build action scope from active page / selection (#230–#231).

import type { PluginContext } from '../../sdk'
import { stripIdentityComments, truncateForPrompt } from './text'
import type { AssistantSettings, ScopeContext } from './types'

export async function fetchPageBlocks(
  ctx: PluginContext,
  notebook: string,
  section: string,
  page: string
): Promise<
  Array<{ id: string; clean_content: string; type?: string; text?: string }>
> {
  const { rows } = await ctx.sqliteQuery(
    `SELECT id, clean_content, type
       FROM blocks
      WHERE notebook = ? AND section = ? AND page = ?
      ORDER BY line_number`,
    [notebook, section, page]
  )
  return rows.map((r) => ({
    id: String(r.id ?? ''),
    clean_content: String(r.clean_content ?? ''),
    type: typeof r.type === 'string' ? r.type : undefined
  }))
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
  let existingTaskTitles: string[] = []

  if (opts.selectionText?.trim()) {
    inputText = opts.selectionText
  } else if (opts.instruction?.trim() && !opts.selectionText) {
    // Draft from instruction alone is fine; still attach page context if any.
    inputText = ''
  }

  if (notebook && page) {
    const blocks = await fetchPageBlocks(ctx, notebook, section, page)
    existingTaskTitles = blocks
      .filter((b) => b.type === 'TASK')
      .map((b) => b.clean_content)

    if (!inputText.trim()) {
      if (opts.blockId) {
        const b = blocks.find((x) => x.id === opts.blockId)
        if (b) {
          inputText = b.clean_content
          targetBlockId = b.id
        }
      }
      if (!inputText.trim()) {
        inputText = blocks.map((b) => b.clean_content).join('\n')
      }
    } else if (opts.blockId) {
      targetBlockId = opts.blockId
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
    blockId: opts.blockId,
    inputText: text,
    truncated,
    selectionText: opts.selectionText,
    existingTaskTitles,
    targetBlockId
  }
}
