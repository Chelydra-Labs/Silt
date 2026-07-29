// Agent tool #601 — create_note.
//
// Creates a NOTE block on a page, creating the page first if needed. Pages are
// append-only containers: createPage is a no-op when the page already exists
// (it never overwrites), and createBlock appends, so repeated calls accumulate
// notes rather than clobbering prior content. Tags are inline #hashtags in Silt,
// so they are folded into the block text. Returns the new block id so the model
// can immediately read_blocks or get_backlinks on it.

import type { PluginContext } from '../../../sdk'
import { asString } from '../../../../lib/asString'
import { auditWrite } from './_util'
import type { ToolResult } from '../tool-registry'

export const createNoteToolDef = {
  name: 'create_note',
  description:
    'Create a note (NOTE block) on a page, creating the page if it does not ' +
    'exist. Existing pages are appended to, never overwritten. Returns the ' +
    'page path and the new block id.',
  parameters: {
    type: 'object',
    required: ['page', 'content'],
    properties: {
      notebook: {
        type: 'string',
        description:
          'Target notebook. Omit to use the currently active notebook.'
      },
      section: { type: 'string', description: 'Target section (optional).' },
      page: { type: 'string', description: 'Target page name.' },
      content: { type: 'string', description: 'Note body text.' },
      tags: {
        type: 'array',
        description: 'Tags to attach (folded into the note as #hashtags).',
        items: { type: 'string' }
      }
    }
  }
}

export async function handleCreateNote(
  ctx: PluginContext,
  args: Record<string, unknown>
): Promise<ToolResult> {
  const page = asString(args.page).trim()
  if (!page) {
    auditWrite(ctx, 'create_note', 'error')
    return { content: '', error: 'page must not be empty' }
  }
  const body = asString(args.content)
  if (!body.trim()) {
    auditWrite(ctx, 'create_note', 'error')
    return { content: '', error: 'content must not be empty' }
  }

  const notebook =
    typeof args.notebook === 'string' && args.notebook.length > 0
      ? args.notebook
      : ctx.activeNotebook
  if (!notebook) {
    auditWrite(ctx, 'create_note', 'error')
    return {
      content: '',
      error:
        'no notebook specified and no active notebook is open; pass notebook explicitly.'
    }
  }
  const section = typeof args.section === 'string' ? args.section : ''

  const tagPart = formatTags(args.tags)
  const text = tagPart ? `${body} ${tagPart}` : body

  // createPage is idempotent: it creates the page when absent and no-ops (never
  // overwrites) when present. createBlock then appends the note.
  await ctx.createPage(notebook, section, page)
  const blockId = await ctx.createBlock({
    type: 'NOTE',
    text,
    notebook,
    section,
    page
  })

  const pagePath = [notebook, section, page]
    .filter((s) => s.length > 0)
    .join('/')
  auditWrite(ctx, 'create_note', 'ok', blockId)
  return {
    content: `Created note on ${pagePath} (block ${blockId}). Use read_blocks with this id to verify.`
  }
}

/** Render a tags array into a space-separated #hashtag string (empty if none). */
function formatTags(raw: unknown): string {
  if (!Array.isArray(raw)) return ''
  return raw
    .map((t) => asString(t).trim())
    .filter((t) => t.length > 0)
    .map((t) => (t.startsWith('#') ? t : `#${t}`))
    .join(' ')
}
