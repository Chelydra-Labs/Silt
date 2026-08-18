// Agent tool — list_page_versions.
//
// Lists retained page-history snapshots via PluginContext (not $silt-app).

import type { PluginContext } from '../../../sdk'
import { asString } from '../../../../lib/asString'
import type { ToolResult } from '../tool-registry'
import { breadcrumb } from './_util'

export const listPageVersionsToolDef = {
  name: 'list_page_versions',
  description:
    'List retained page-history snapshots for a page, newest first. ' +
    'Each row has id, timestamp, source (editor/source/mcp/plugin/restore/rename), and bytes. ' +
    'An empty list means no snapshots — not an error. Use get_page_version to preview a body.',
  parameters: {
    type: 'object',
    required: ['notebook', 'page'],
    properties: {
      notebook: { type: 'string', description: 'Notebook name.' },
      section: {
        type: 'string',
        description: 'Section path; empty or omit for a notebook-root page.'
      },
      page: { type: 'string', description: 'Page name without .md.' }
    }
  }
}

export async function handleListPageVersions(
  ctx: PluginContext,
  args: Record<string, unknown>
): Promise<ToolResult> {
  const notebook = asString(args.notebook).trim()
  const section = asString(args.section).trim()
  const page = asString(args.page).trim()
  if (!notebook || !page) {
    return { content: '', error: 'notebook and page are required' }
  }
  const rows = await ctx.listPageVersions(notebook, section, page)
  const loc = breadcrumb(notebook, section, page)
  if (!rows || rows.length === 0) {
    return { content: `No versions for ${loc}.` }
  }
  const lines = rows.map((r, i) => {
    const bytes = Number.isFinite(r.bytes) ? `${r.bytes} B` : ''
    return `${i + 1}. ${r.id}  ${r.timestamp}  ${r.source}${bytes ? `  ${bytes}` : ''}`
  })
  return {
    content: [`Versions for ${loc} (newest first):`, ...lines].join('\n')
  }
}
